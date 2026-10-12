package backend

import (
	"context"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	core "wg-turn-client"
)

// Bridge — мост между App.go и ядром.
type Bridge struct {
	ctx     context.Context
	store   *Store
	onEvent func(name string, args ...any)
	mu      sync.Mutex
	core    *core.Core
	running bool
	logFile *LogFile // полный лог сессии
	session uint64   // номер сессии; stale forwardEvents не должен трогать новую

	// Логи в окно уходят пачками: каждый EventsEmit — это вызов скрипта в
	// WebView2, а при переподключении десятков воркеров строк сотни.
	logMu    sync.Mutex
	logBatch [][2]string
	logTimer *time.Timer
}

const logFlushDelay = 250 * time.Millisecond

func (b *Bridge) queueLog(level, msg string) {
	b.logMu.Lock()
	defer b.logMu.Unlock()
	if len(b.logBatch) >= 500 {
		b.logBatch = b.logBatch[1:] // окно всё равно хранит последние 500
	}
	b.logBatch = append(b.logBatch, [2]string{level, msg})
	if b.logTimer == nil {
		b.logTimer = time.AfterFunc(logFlushDelay, b.flushLogs)
	}
}

// flushLogs отправляет накопленные строки одним событием.
func (b *Bridge) flushLogs() {
	b.logMu.Lock()
	batch := b.logBatch
	b.logBatch = nil
	if b.logTimer != nil {
		b.logTimer.Stop()
		b.logTimer = nil
	}
	b.logMu.Unlock()
	if len(batch) > 0 {
		b.onEvent("log_batch", batch)
	}
}

func NewBridge(ctx context.Context, store *Store, onEvent func(string, ...any)) *Bridge {
	return &Bridge{ctx: ctx, store: store, onEvent: onEvent}
}

// Connect подключается к серверу.
func (b *Bridge) Connect(params ConnectParams) error {
	b.mu.Lock()
	if b.running {
		b.mu.Unlock()
		log.Printf("[BRIDGE] Connect rejected: already running")
		return fmt.Errorf("already running")
	}
	// Сразу помечаем как running чтобы заблокировать параллельные вызовы
	b.running = true
	b.session++
	sessID := b.session
	b.mu.Unlock()

	// resetRunning сбрасывает флаг при любой ошибке до запуска forwardEvents
	resetRunning := func() {
		b.mu.Lock()
		b.running = false
		b.mu.Unlock()
	}

	hashes := params.Hashes
	if len(hashes) == 0 {
		resetRunning()
		return fmt.Errorf("нет хешей VK")
	}

	workers := params.Workers
	if workers <= 0 {
		workers = 24
	}
	if params.MaxWorkers > 0 && workers > params.MaxWorkers {
		workers = params.MaxWorkers
	}

	deviceID := params.DeviceID
	if deviceID == "" && b.store != nil {
		deviceID = b.store.MachineDeviceID()
	}
	if deviceID == "" {
		deviceID = "unknown"
	}
	log.Printf("[BRIDGE] ID устройства: %s", deviceID)

	cfg := core.Config{
		PeerAddr:    params.PeerAddr,
		Password:    params.Password,
		Hashes:      hashes,
		DeviceID:    deviceID,
		Workers:     workers,
		CaptchaMode: params.CaptchaMode,
		ObfsMode:    params.ObfsMode,
		Fingerprint: params.Fingerprint,
		TurnTCP:     params.TurnTCP,
		Listen:      normalizeListen(params.Listen),
		TurnMode:    core.NormalizeConnectionMode(params.TurnMode, params.TurnTCP),
		TurnSNI:     normalizeSNI(params.TurnSNI),
		ExcludeTURN: func(ips []string) {
			wg.ExcludeTURN(ips, func(msg string) { b.onEvent("log", "INFO", "[WG] "+msg) })
		},
	}
	SetExclusions(params.Exclusions)

	c := core.New(cfg)
	
	b.mu.Lock()
	b.core = c
	b.logFile = b.store.OpenLogFile()
	b.mu.Unlock()

	events, err := c.Start()
	if err != nil {
		log.Printf("[BRIDGE] core start failed: %v", err)
		c.Stop() // на всякий случай гасим контекст ядра
		b.mu.Lock()
		b.core = nil
		if b.logFile != nil {
			b.logFile.Close()
			b.logFile = nil
		}
		b.running = false
		b.mu.Unlock()
		return fmt.Errorf("core start: %w", err)
	}

	go b.forwardEvents(events, sessID)
	return nil
}

func (b *Bridge) Disconnect() {
	b.mu.Lock()
	c := b.core
	b.mu.Unlock()

	// Tear down WireGuard interface immediately — don't wait for core shutdown
	wg.Teardown()
	log.Printf("[BRIDGE] WG интерфейс снят по запросу пользователя")

	if c != nil {
		c.Stop()
	}
}

func (b *Bridge) IsRunning() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.running
}

func (b *Bridge) SendCaptchaResult(token string) {
	b.mu.Lock()
	c := b.core
	b.mu.Unlock()
	if c != nil {
		c.SolveCaptcha(token)
	}
}

// forwardEvents читает канал событий ядра и пробрасывает в Wails.
// sessID — поколение сессии, на которую запущен этот горутин: если к моменту
// его завершения уже начата новая сессия (быстрый Disconnect → Connect),
// устаревший горутин не имеет права трогать состояние Bridge.
func (b *Bridge) forwardEvents(events <-chan core.Event, sessID uint64) {
	for ev := range events {
		b.mu.Lock()
		stale := b.session != sessID
		b.mu.Unlock()
		if stale {
			continue // только осушаем канал, без побочных эффектов
		}

		switch ev.Type {
		case core.EventState:
			b.flushLogs() // порядок: сначала логи, потом смена состояния
			b.onEvent("state_changed", ev.Status)

		case core.EventLog:
			// Пишем ВСЁ в файл (включая отфильтрованное для UI)
			b.mu.Lock()
			if b.logFile != nil {
				b.logFile.Write(ev.Level, ev.Message)
			}
			b.mu.Unlock()
			// В UI — только отфильтрованное
			if ev.Level != "SKIP" {
				b.queueLog(ev.Level, ev.Message)
			}

		case core.EventStats:
			b.onEvent("stats", map[string]any{
				"active":     ev.Active,
				"bytes_up":   ev.BytesUp,
				"bytes_down": ev.BytesDown,
			})

		case core.EventError:
			b.flushLogs()
			b.onEvent("error", ev.Message)

		case core.EventEvent:
			b.flushLogs()
			switch ev.Name {
			case "wg_config":
				b.onEvent("log", "INFO", "[WG] Применение конфига...")
				wgLogf := func(msg string) {
					b.onEvent("log", "INFO", "[WG] "+msg)
					b.mu.Lock()
					if b.logFile != nil {
						b.logFile.Write("INFO", "[WG] "+msg)
					}
					b.mu.Unlock()
				}
				if err := wg.Apply(ev.Data, ev.TurnIPs, wgLogf); err != nil {
					b.onEvent("error", fmt.Sprintf("[WG] Ошибка: %v", err))
				} else {
					b.onEvent("log", "INFO", "[WG] Конфиг применён, туннель активен")
					b.onEvent("state_changed", "connected")
					// Через 5 и 20 секунд: диагностика в лог, чужой VPN, рукопожатие.
					go b.watchTunnel(sessID, wgLogf)
				}
			case "captcha_required":
				b.onEvent("captcha_required", ev.Data)
			case "ready":
				b.onEvent("log", "INFO", "[ЯДРО] Туннель готов к работе")
			default:
				b.onEvent("event", ev.Name)
			}
		}
	}

	b.mu.Lock()
	current := b.session == sessID
	curSession := b.session
	if current {
		b.running = false
		b.core = nil
		if b.logFile != nil {
			b.logFile.Close()
			b.logFile = nil
		}
	}
	b.mu.Unlock()

	if !current {
		log.Printf("[BRIDGE] Завершение устаревшей сессии проигнорировано (актуальна #%d)", curSession)
		return
	}

	// Ядро остановилось (в том числе само — отказ сервера, все воркеры
	// завершились): снимаем туннель, иначе его маршруты держат весь трафик в
	// мёртвом интерфейсе и интернет пропадает, хотя приложение «отключено».
	wg.Teardown()

	b.flushLogs()
	b.onEvent("state_changed", "disconnected")
	log.Printf("[BRIDGE] Ядро завершилось")
}

// normalizeListen принимает "порт" или "127.0.0.1:порт"; слушаем только loopback.
func normalizeListen(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if !strings.Contains(v, ":") {
		v = "127.0.0.1:" + v
	}
	host, port, err := net.SplitHostPort(v)
	if err != nil || (host != "127.0.0.1" && host != "localhost") {
		return ""
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return ""
	}
	return "127.0.0.1:" + port
}

// normalizeSNI — неверный SNI не ломает подключение: идём без подмены.
func normalizeSNI(v string) string {
	sni, err := core.NormalizeFrontSNI(v)
	if err != nil {
		log.Printf("[TURN] Внешний SNI %q неверен (%v) — без подмены SNI", v, err)
		return ""
	}
	return sni
}

// watchTunnel проверяет туннель после подключения: пишет диагностику, ищет
// чужой VPN (его маршрут мог появиться не сразу) и предупреждает, если
// WireGuard так и не договорился с сервером.
func (b *Bridge) watchTunnel(sessID uint64, logf func(string)) {
	alive := func() bool {
		b.mu.Lock()
		defer b.mu.Unlock()
		return b.session == sessID && b.running
	}
	warnedVPN := false
	for i, d := range []time.Duration{5 * time.Second, 15 * time.Second} {
		time.Sleep(d)
		if !alive() {
			return
		}
		wg.Diagnose(logf)
		if others := OtherVPNs(wg.TunnelAddrs()); len(others) > 0 && !warnedVPN {
			warnedVPN = true
			logf("Обнаружен другой VPN: " + strings.Join(others, ", "))
			b.onEvent("error", "Работает другой VPN ("+strings.Join(others, ", ")+
				") — он перехватывает трафик, и интернет через PWDTT Plus не пойдёт. Отключите его и переподключитесь.")
		}
		if i == 1 && !wg.Handshaked() {
			logf("WireGuard не получил ответа сервера за 20 с")
			b.onEvent("error", "Сервер не отвечает на рукопожатие WireGuard: потоки подключены, но трафик не идёт. "+
				"Проверьте, что этот пароль не используется сейчас на другом компьютере с PWDTT, и переподключитесь.")
		}
	}
}
