package backend

// Транспорт WDTT Plus: RWDTT запускает официальный клиент WDTT Plus
// (go_client из github.com/Ivan4537/WDTT-Plus) отдельным процессом — так же,
// как это делает приложение WDTT Plus на Android. Соединение, потоки, капча
// и скорость — ровно как в WDTT Plus; RWDTT отвечает за окно, туннель
// WireGuard в Windows, маршруты и исключения.
//
// Протокол с процессом (как у Android-приложения):
//   stdin:  START_CONFIG|<base64url JSON с хешами и паролем>, CAPTCHA_RESULT|id|значение, STOP
//   stdout: рамка «WireGuard Конфиг» с конфигом, CAPTCHA_SOLVE|id|mode|uri|token, строки лога

import (
	"bufio"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	core "wg-turn-client"
)

var wdttEngineExe []byte

// InitEngine передаёт встроенный в приложение клиент WDTT Plus.
func InitEngine(exe []byte) { wdttEngineExe = exe }

// engineOptions — параметры запуска клиента WDTT Plus.
type engineOptions struct {
	Peer        string
	Password    string
	Hashes      []string
	Workers     int
	Listen      string
	DeviceID    string
	DeviceInfo  string
	StreamOnly  bool // «Подключение: TCP/TLS»
	TurnSNI     string
	Fingerprint string
}

func (o engineOptions) args(deviceID string) []string {
	args := []string{
		"-peer", o.Peer,
		"-startup-config-stdin=true",
		"-n", strconv.Itoa(o.Workers),
		"-listen", o.Listen,
		"-mode", "vpn",
		"-vkcalls-preflight=true",
		"-turn-stream-first=" + strconv.FormatBool(o.StreamOnly),
		"-rt-masque=false",
		"-device-id", deviceID,
		"-config-first-start=true",
		"-captcha-mode", "auto",
	}
	if o.DeviceInfo != "" {
		args = append(args, "-device-info", o.DeviceInfo)
	}
	if o.TurnSNI != "" {
		args = append(args, "-turn-sni", o.TurnSNI)
	}
	if o.Fingerprint != "" {
		args = append(args, "-fingerprint", o.Fingerprint)
	}
	if len(o.Hashes) > 1 {
		args = append(args, "-hash-fallback=true")
	}
	return args
}

func (o engineOptions) startupLine() string {
	data, _ := json.Marshal(map[string]string{
		"vk_hashes":               strings.Join(o.Hashes, ","),
		"connection_password":     o.Password,
		"custom_vk_client_id":     "",
		"custom_vk_client_secret": "",
	})
	return "START_CONFIG|" + base64.RawURLEncoding.EncodeToString(data) + "\n"
}

// wdttEngine — запущенный клиент WDTT Plus.
type wdttEngine struct {
	opts    engineOptions
	exePath string
	events  chan core.Event
	excl    func([]string)

	mu       sync.Mutex
	stdin    io.WriteCloser
	proc     *os.Process
	stopped  bool
	seenTURN map[string]bool
	fatalMsg string // причина остановки (отказ сервера)
	mismatch bool   // сервер ответил device_mismatch
	stopCh   chan struct{}
	restarts int       // перезапусков подряд без устойчивой работы
	started  time.Time // запуск текущего процесса
}

// Как приложение WDTT Plus: клиент, вышедший сам (не получил креды, потерял
// все пути), перезапускается. Туннель Windows при этом остаётся поднятым.
const (
	engineMaxRestarts = 10
	engineStableRun   = 2 * time.Minute
)

func startWDTTEngine(dir string, opts engineOptions, excludeTURN func([]string)) (*wdttEngine, <-chan core.Event, error) {
	exePath, err := ensureEngineExe(dir)
	if err != nil {
		return nil, nil, err
	}
	e := &wdttEngine{
		opts:    opts,
		exePath: exePath,
		events:  make(chan core.Event, 8192),
		excl:    excludeTURN,
		stopCh:  make(chan struct{}),
	}
	if err := e.launch(opts.DeviceID); err != nil {
		return nil, nil, err
	}
	return e, e.events, nil
}

// ensureEngineExe раскладывает встроенный клиент в папку настроек. Имя с
// хешем: обновлённое приложение не упрётся в старый файл, занятый процессом.
func ensureEngineExe(dir string) (string, error) {
	if len(wdttEngineExe) == 0 {
		return "", fmt.Errorf("клиент WDTT Plus не встроен в сборку")
	}
	sum := sha256.Sum256(wdttEngineExe)
	engineDir := filepath.Join(dir, "engine")
	if err := os.MkdirAll(engineDir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(engineDir, "wdtt-client-"+hex.EncodeToString(sum[:4])+".exe")
	if data, err := os.ReadFile(path); err == nil && sha256.Sum256(data) == sum {
		return path, nil
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, wdttEngineExe, 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return "", err
	}
	// Старые версии клиента больше не нужны.
	if old, _ := filepath.Glob(filepath.Join(engineDir, "wdtt-client-*.exe")); len(old) > 0 {
		for _, p := range old {
			if p != path {
				os.Remove(p)
			}
		}
	}
	return path, nil
}

func (e *wdttEngine) launch(deviceID string) error {
	cmd := exec.Command(e.exePath, e.opts.args(deviceID)...)
	cmd.Dir = filepath.Dir(e.exePath)
	hideWindow(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("запуск клиента WDTT Plus: %w", err)
	}
	if _, err := io.WriteString(stdin, e.opts.startupLine()); err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		return fmt.Errorf("передача настроек клиенту WDTT Plus: %w", err)
	}
	e.mu.Lock()
	e.stdin = stdin
	e.proc = cmd.Process
	e.mismatch = false
	e.started = time.Now()
	raisePriority(cmd.Process)
	stoppedNow := e.stopped
	e.mu.Unlock()
	if stoppedNow {
		// «Отключить» пришло, пока процесс запускался.
		io.WriteString(stdin, "STOP\n")
		go func() {
			time.Sleep(3 * time.Second)
			cmd.Process.Kill()
		}()
	}

	e.emit(core.Event{Type: core.EventState, Status: "connecting"})
	done := make(chan struct{})
	go func() {
		e.readOutput(pr)
		close(done)
	}()
	go func() {
		err := cmd.Wait()
		pw.Close()
		<-done
		e.exited(deviceID, err)
	}()
	return nil
}

// exited: процесс завершился. Пароль, привязанный старыми версиями PWDTT к
// общему ID "unknown", пробуем один раз с этим ID. Отказ сервера или «Отключить»
// — конец сессии; любой другой выход — перезапуск клиента.
func (e *wdttEngine) exited(deviceID string, err error) {
	e.mu.Lock()
	stopped, mismatch, msg := e.stopped, e.mismatch, e.fatalMsg
	if time.Since(e.started) >= engineStableRun {
		e.restarts = 0
	}
	e.mu.Unlock()
	if stopped {
		close(e.events)
		return
	}
	if mismatch && deviceID != legacyDeviceID {
		e.logLine("INFO", "[ЯДРО] Пароль привязан к другому ID устройства — пробую ID старых версий приложения")
		e.mu.Lock()
		e.fatalMsg = ""
		e.mu.Unlock()
		if e.launch(legacyDeviceID) == nil {
			return
		}
	}
	if msg != "" {
		e.emit(core.Event{Type: core.EventError, Message: msg})
		close(e.events)
		return
	}

	e.mu.Lock()
	e.restarts++
	n := e.restarts
	e.mu.Unlock()
	if n > engineMaxRestarts {
		e.emit(core.Event{Type: core.EventError, Message: "Не удалось подключиться: клиент WDTT Plus завершился " +
			strconv.Itoa(engineMaxRestarts) + " раз подряд. Проверьте ссылку и интернет и подключитесь снова."})
		close(e.events)
		return
	}
	delay := time.Duration(min(n*2, 30)) * time.Second
	reason := "без ошибки"
	if err != nil {
		reason = err.Error()
	}
	e.logLine("WARN", fmt.Sprintf("[ЯДРО] Клиент WDTT Plus завершился (%s), перезапуск через %v", reason, delay))
	go func() {
		select {
		case <-time.After(delay):
		case <-e.stopCh:
			close(e.events)
			return
		}
		if launchErr := e.launch(deviceID); launchErr != nil {
			e.emit(core.Event{Type: core.EventError, Message: launchErr.Error()})
			close(e.events)
		}
	}()
}

const legacyDeviceID = "unknown"

func (e *wdttEngine) Stop() {
	e.mu.Lock()
	if e.stopped {
		e.mu.Unlock()
		return
	}
	e.stopped = true
	close(e.stopCh)
	stdin, proc := e.stdin, e.proc
	e.mu.Unlock()
	if stdin != nil {
		io.WriteString(stdin, "STOP\n")
	}
	if proc != nil {
		// Клиент сам удаляет TURN-аллокации и выходит; не дождались — гасим.
		go func() {
			time.Sleep(3 * time.Second)
			proc.Kill()
		}()
	}
}

// NetworkChanged — как у Android-приложения WDTT Plus: после сна клиент
// сразу проверяет каналы (DEVICE_WAKE), после смены сети заново ищет путь,
// снова пробуя UDP (CONNECTION_NETWORK). Живые потоки не трогаются.
func (e *wdttEngine) NetworkChanged(resumed bool) {
	if resumed {
		e.send("DEVICE_WAKE")
	}
	e.send("CONNECTION_NETWORK")
}

// SolveCaptcha нужен для интерфейса движка: окна капчи в RWDTT нет, ответ
// на запрос клиента отправляется сразу (см. handleLine).
func (e *wdttEngine) SolveCaptcha(string) {}

func (e *wdttEngine) send(line string) {
	e.mu.Lock()
	stdin := e.stdin
	e.mu.Unlock()
	if stdin != nil {
		io.WriteString(stdin, line+"\n")
	}
}

func (e *wdttEngine) emit(ev core.Event) {
	select {
	case e.events <- ev:
	default:
	}
}

func (e *wdttEngine) logLine(level, msg string) {
	e.emit(core.Event{Type: core.EventLog, Level: level, Message: msg})
}

func (e *wdttEngine) readOutput(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	var conf []string
	inConf := false
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.Contains(line, "╔") && strings.Contains(line, "WireGuard"):
			inConf, conf = true, nil
			continue
		case inConf && strings.Contains(line, "╚"):
			inConf = false
			cfg := strings.Join(conf, "\n")
			e.logLine("INFO", "[ЯДРО] Конфиг WireGuard получен")
			e.emit(core.Event{Type: core.EventEvent, Name: "wg_config", Data: cfg})
			continue
		case inConf:
			if s := strings.TrimSpace(strings.Trim(strings.TrimSpace(line), "║")); s != "" {
				conf = append(conf, s)
			}
			continue
		}
		e.handleLine(stripLogPrefix(line))
	}
}

var logPrefixRe = regexp.MustCompile(`^\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}(\.\d+)? `)

func stripLogPrefix(s string) string {
	return logPrefixRe.ReplaceAllString(strings.TrimSpace(s), "")
}

var statsRe = regexp.MustCompile(`\[СТАТИСТИКА\] Активных: (\d+) \| ↓([\d.]+) МБ / ↑([\d.]+) МБ`)

func (e *wdttEngine) handleLine(line string) {
	if line == "" {
		return
	}
	if rest, ok := strings.CutPrefix(line, "CAPTCHA_SOLVE|"); ok {
		// Окна капчи нет: отказываемся сразу, клиент перейдёт к встроенному
		// решателю (Go v2), не теряя 25 секунд на ожидание окна.
		id, _, _ := strings.Cut(rest, "|")
		e.send("CAPTCHA_RESULT|" + id + "|error:unsupported")
		e.logLine("SKIP", "[КАПЧА] Запрос окна капчи — решаем встроенным способом")
		return
	}
	if strings.HasPrefix(line, "TRANSPORT_METRIC|") || strings.HasPrefix(line, "TRANSPORT_ORDER|") {
		return
	}
	if m := statsRe.FindStringSubmatch(line); m != nil {
		active, _ := strconv.Atoi(m[1])
		down, _ := strconv.ParseFloat(m[2], 64)
		up, _ := strconv.ParseFloat(m[3], 64)
		e.emit(core.Event{Type: core.EventStats, Active: int32(active),
			BytesDown: int64(down * 1024 * 1024), BytesUp: int64(up * 1024 * 1024)})
		e.logLine("SKIP", line)
		return
	}
	if strings.Contains(line, "TURN") || strings.Contains(line, "Relay:") {
		e.excludeNew(publicIPv4s(line))
	}
	if i := strings.Index(line, "FATAL_AUTH"); i >= 0 &&
		!strings.Contains(line, "DTLS timeout") && !strings.Contains(line, "WRAP_AUTH_TIMEOUT") {
		msg := strings.TrimPrefix(line[i:], "FATAL_AUTH: ")
		if j := strings.Index(msg, " (адрес="); j > 0 {
			msg = msg[:j]
		}
		e.mu.Lock()
		first := e.fatalMsg == ""
		if first {
			e.fatalMsg = friendlyDenied(msg)
			e.mismatch = strings.Contains(msg, "device_mismatch") || strings.Contains(msg, "другому устройству")
		}
		e.mu.Unlock()
		if first {
			// Отказ сервера повторами не лечится: останавливаем клиент сразу.
			e.send("STOP")
		}
	}
	e.logLine(classifyEngineLine(line), line)
}

// excludeNew выводит новые адреса TURN из туннеля. Маршруты добавляются в
// фоне: чтение вывода не должно ждать netsh, иначе клиент упрётся в полный
// канал вывода и притормозит.
func (e *wdttEngine) excludeNew(ips []string) {
	if len(ips) == 0 || e.excl == nil {
		return
	}
	e.mu.Lock()
	if e.seenTURN == nil {
		e.seenTURN = make(map[string]bool)
	}
	var fresh []string
	for _, ip := range ips {
		if !e.seenTURN[ip] {
			e.seenTURN[ip] = true
			fresh = append(fresh, ip)
		}
	}
	e.mu.Unlock()
	if len(fresh) > 0 {
		go e.excl(fresh)
	}
}

func friendlyDenied(msg string) string {
	if strings.Contains(msg, "device_mismatch") || strings.Contains(msg, "другому устройству") {
		return "Пароль привязан к другому устройству — он уже использовался, например, в WDTT Plus на телефоне или на другом компьютере. " +
			"Каждому устройству нужен свой пароль: попросите владельца сервера выдать отдельный пароль или сбросить привязку (WDTT Plus → Деплой → Клиенты)."
	}
	return "Сервер отказал в доступе: " + msg
}

// classifyEngineLine: что показывать в окне (в файл лога пишется всё).
func classifyEngineLine(line string) string {
	low := strings.ToLower(line)
	switch {
	case strings.Contains(line, "FATAL") || strings.Contains(low, "ошибка") && !strings.Contains(line, "[FAILOVER]"):
		return "ERROR"
	case strings.Contains(line, "[КЛИЕНТ]") || strings.Contains(line, "[ПОДКЛЮЧЕНИЕ]") ||
		strings.Contains(line, "Креды OK") || strings.Contains(line, "[ЯДРО]") ||
		strings.Contains(line, "AUTO: стадия") || strings.Contains(line, "решил капчу") ||
		strings.Contains(line, "[CONNECTION] READY") || strings.Contains(line, "Все воркеры завершены"):
		return "INFO"
	default:
		return "SKIP"
	}
}

var ipv4Re = regexp.MustCompile(`\b(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3})\b`)

func publicIPv4s(line string) []string {
	var out []string
	for _, m := range ipv4Re.FindAllStringSubmatch(line, -1) {
		ip := net.ParseIP(m[1]).To4()
		if ip == nil || ip.IsPrivate() || ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() ||
			ip[0] == 100 && ip[1]&0xC0 == 64 {
			continue
		}
		out = append(out, ip.String())
	}
	return out
}

// deviceInfoJSON — сведения об устройстве для панели WDTT Plus (Деплой → Клиенты).
func deviceInfoJSON(appVersion string) string {
	host, _ := os.Hostname()
	if host == "" {
		host = "Windows PC"
	}
	data, _ := json.Marshal(map[string]any{
		"name":            host + " (RWDTT)",
		"manufacturer":    "PC",
		"model":           "Windows",
		"android_version": "",
		"app_version":     "RWDTT " + appVersion,
	})
	return string(data)
}
