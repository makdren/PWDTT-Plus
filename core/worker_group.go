package core

import (
	"context"
	"log"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	workersPerGroup  = 9
	defaultCycleSecs = 36000

	// maxAttemptsPerAddress — сколько раз пробуем один адрес, прежде чем забанить
	maxAttemptsPerAddress = 3

	// sleepWhenAllBanned — сколько спим, если все адреса забанены
	sleepWhenAllBanned = 15 * time.Second

	// sleepAfterAddressDead — спим после того как адрес умер (перед переходом к следующему)
	sleepAfterAddressDead = 500 * time.Millisecond

	// workerStartDelay — задержка между запуском воркеров
	workerStartDelay = 200 * time.Millisecond

	// workerBatchDelay — задержка между первой и второй волной воркеров (5 секунд)
	workerBatchDelay = 5 * time.Second

	// firstBatchSize — сколько воркеров запускаем в первой волне
	firstBatchSize = 5

	// configRacerLimit bounds concurrent initial config requests. The first
	// racers are assigned different TURN URLs whenever enough URLs exist.
	configRacerLimit = 3
)

func WorkerGroup(
	ctx context.Context,
	groupID int,
	hashIndex int,
	tp *TurnParams,
	peer *net.UDPAddr,
	d *Dispatcher,
	localPort string,
	getConfig bool,
	configCh chan<- string,
	turnIPsCh chan<- []string,
	workerIDs []int,
	pauseFlag *int32,
	deviceID, password string,
	stats *Stats,
	waitCreds <-chan struct{},
	signalCreds chan<- struct{},
	waitSpawn <-chan struct{},
	signalSpawn chan<- struct{},
) {

	// signalSpawnOnce гарантирует ровно один close(signalSpawn) на любом пути выхода
	spawnSignaled := false
	signalSpawnOnce := func() {
		if signalSpawn != nil && !spawnSignaled {
			spawnSignaled = true
			close(signalSpawn)
		}
	}

	if waitCreds != nil {
		select {
		case <-waitCreds:
		case <-ctx.Done():
			signalSpawnOnce()
			return
		}
	}

	// Сигнал следующей группе не должен зависеть от успешности GetCreds ниже:
	// иначе один transient-фейл VK API навсегда блокирует все последующие группы
	if signalCreds != nil {
		go func() {
			time.Sleep(2 * time.Second)
			close(signalCreds)
		}()
	}

	var configSent int32
	if !getConfig {
		configSent = 1
	}

	for atomic.LoadInt32(pauseFlag) != 0 {
		if ctx.Err() != nil {
			signalSpawnOnce()
			return
		}
		time.Sleep(1 * time.Second)
	}

	if waitSpawn != nil {
		select {
		case <-waitSpawn:
		case <-ctx.Done():
			signalSpawnOnce()
			return
		}
	}

	hash := tp.Hashes[hashIndex%len(tp.Hashes)]
	shortHash := hash
	if len(shortHash) > 8 {
		shortHash = shortHash[:8]
	}
	log.Printf("[ГРУППА #%d] Запрос кредов (хеш: %s...)", groupID, shortHash)

	credStreamID := groupID * 100

	var credsMu sync.RWMutex
	var refreshMu sync.Mutex
	var curUser, curPass string
	var curURLs []string
	var credGen uint64
	var lastQuotaRefresh time.Time // под refreshMu

	fetchCreds := func() error {
		u, p, urls, err := GetCreds(ctx, hash, credStreamID)
		if err != nil {
			return err
		}
		// Адреса TURN этой группы выводим мимо туннеля ДО первой сессии:
		// иначе после подъёма VPN пакеты к ним уйдут в сам туннель (петля).
		if tp.excludeTURN != nil {
			tp.excludeTURN(turnIPsFor(urls, tp))
		}
		credsMu.Lock()
		curUser, curPass, curURLs = u, p, urls
		credsMu.Unlock()
		return nil
	}

	// Первичное получение кредов — с ретраями, а не один фейл навсегда
	for attempt := 1; ; attempt++ {
		if err := fetchCreds(); err == nil {
			break
		} else {
			log.Printf("[ГРУППА #%d] Ошибка кредов (попытка %d): %v", groupID, attempt, err)
		}
		select {
		case <-time.After(5 * time.Second):
		case <-ctx.Done():
			signalSpawnOnce()
			return
		}
	}

	credsMu.RLock()
	log.Printf("[ГРУППА #%d] Креды OK, TURN: %v, %d воркеров", groupID, curURLs, len(workerIDs))
	credsMu.RUnlock()

	// TURN IP первой группы уходят в Apply вместе с конфигом (exclude routes).
	if groupID == 1 && turnIPsCh != nil {
		credsMu.RLock()
		unique := turnIPsFor(curURLs, tp)
		credsMu.RUnlock()
		if len(unique) > 0 {
			select {
			case turnIPsCh <- unique:
			default:
			}
		}
	}

	var wg sync.WaitGroup

	// Запускаем воркеры с поэтапной задержкой
	for idx, wid := range workerIDs {
		wg.Add(1)

		// Определяем задержку перед запуском этого воркера
		var startDelay time.Duration
		if idx < firstBatchSize {
			// Первая волна: 5 воркеров с задержкой 200ms
			startDelay = time.Duration(idx) * workerStartDelay
		} else {
			// Вторая волна: задержка = (первая волна) + 5 секунд + (индекс во второй волне) * 200ms
			secondWaveIdx := idx - firstBatchSize
			startDelay = time.Duration(firstBatchSize)*workerStartDelay + workerBatchDelay + time.Duration(secondWaveIdx)*workerStartDelay
		}

		go func(wid, workerIndex int, delay time.Duration) {
			// Ждём свою задержку перед стартом
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				wg.Done()
				return
			}

			defer wg.Done()

			shouldGetConfig := getConfig
			// Retry state is local to this worker, avoiding synchronized storms.
			addressAttempts := 0
			relayOffset := 0
			lastTurnAddr := ""
			// Храним текущий ObfsMode для этого воркера (может меняться при WRAP_TIMEOUT)
			workerObfsMode := tp.ObfsMode

			for {
				if ctx.Err() != nil {
					return
				}
				if workerOverServerLimit(wid) {
					return
				}

				// Проверяем паузу
				for atomic.LoadInt32(pauseFlag) != 0 {
					if ctx.Err() != nil {
						return
					}
					time.Sleep(1 * time.Second)
				}

				// Берём свежие креды (могут быть обновлены после AUTH-ошибки)
				credsMu.RLock()
				urls := cloneStringSlice(curURLs)
				wUser, wPass := curUser, curPass
				gen := credGen
				credsMu.RUnlock()

				if len(urls) == 0 {
					log.Printf("[ВОРКЕР #%d] Нет TURN-адресов в кредах", wid)
					select {
					case <-time.After(5 * time.Second):
					case <-ctx.Done():
					}
					continue
				}

				// Spread workers deterministically over relays and advance after a
				// failed relay instead of converging on urls[0].
				// UDP-режим — обычные адреса; TCP/TLS — сначала адреса turns:.
				urls = tp.conn.filterURLs(urls, GlobalBlacklist.IsBanned)
				turnAddr, ok := selectTurnURL(urls, wid, relayOffset, GlobalBlacklist.IsBanned)
				if !ok {
					log.Printf("[ВОРКЕР #%d] Все TURN-адреса забанены (всего %d), спим %v",
						wid, len(urls), sleepWhenAllBanned)
					select {
					case <-time.After(sleepWhenAllBanned):
					case <-ctx.Done():
					}
					continue
				}

				if turnAddr != lastTurnAddr {
					addressAttempts = 0
					lastTurnAddr = turnAddr
				}

				// A few workers race for config on round-robin-selected relays. This
				// prevents one silent relay from blocking startup without starting a
				// config request from every worker.
				getConf := shouldRaceForConfig(shouldGetConfig, workerIndex, atomic.LoadInt32(&configSent))
				var cc chan<- string
				if getConf {
					cc = configCh
				}

				// Передаём в RunSession конкретный адрес и креды
				configDelivered, sessErr := RunSession(
					ctx, tp, peer, d, localPort,
					getConf, cc, wid,
					turnAddr, // конкретный TURN-адрес
					wUser,    // username из кредов
					wPass,    // password из кредов
					workerObfsMode,
					credStreamID, // для handleAuthError
					deviceID, password, stats,
				)

				if getConf && configDelivered {
					atomic.StoreInt32(&configSent, 1)
				}

				// Обработка ошибок
				if sessErr != nil {
					if ctx.Err() != nil {
						return
					}

					log.Printf("[ВОРКЕР #%d] [FAILOVER] Сбой сессии: %v (адрес=%s, тип=%s)",
						wid, sessErr.Err, sessErr.Address, sessErr.Type)

					switch sessErr.Type {

					case SessionErrorAddressDead:
						// Адрес мёртв — баним и пробуем следующий
						if sessErr.Address != "" {
							GlobalBlacklist.Ban(sessErr.Address)
							log.Printf("[ВОРКЕР #%d] TURN-адрес %s забанен на 5 минут", wid, sessErr.Address)
						}
						relayOffset++
						addressAttempts = 0
						// Небольшая пауза перед переходом к следующему адресу
						select {
						case <-time.After(sleepAfterAddressDead):
						case <-ctx.Done():
						}
						// Продолжаем цикл — возьмём следующий доступный адрес
						continue

					case SessionErrorWrapTimeout:
						// DTLS-таймаут — пробуем сменить обфускацию.
						// Режим пер-воркерский: глобальная перезапись tp.ObfsMode
						// была гонкой и меняла режим у всех живых сессий
						log.Printf("[ВОРКЕР #%d] WRAP_TIMEOUT на адресе %s, пробуем сменить обфускацию",
							wid, sessErr.Address)

						// Меняем режим обфускации
						if workerObfsMode == "audio" {
							workerObfsMode = "video"
						} else {
							workerObfsMode = "audio"
						}
						log.Printf("[ВОРКЕР #%d] Режим обфускации изменён на %s", wid, workerObfsMode)

						// Увеличиваем счётчик попыток на этом адресе
						addressAttempts++

						// Если слишком много попыток на одном адресе — баним его
						if addressAttempts >= maxAttemptsPerAddress {
							if sessErr.Address != "" {
								GlobalBlacklist.Ban(sessErr.Address)
								log.Printf("[ВОРКЕР #%d] TURN-адрес %s забанен после %d попыток",
									wid, sessErr.Address, addressAttempts)
							}
							addressAttempts = 0
							relayOffset++
							// Продолжаем цикл — возьмём следующий адрес
							continue
						}

						// Пробуем тот же адрес с новой обфускацией
						continue

					case SessionErrorAuth:
						// Креды протухли (401) — обновляем и продолжаем.
						// Обновляет только один воркер: остальные увидят новый credGen
						log.Printf("[ВОРКЕР #%d] Ошибка авторизации TURN, обновляю креды", wid)
						refreshMu.Lock()
						credsMu.RLock()
						currentGen := credGen
						credsMu.RUnlock()
						if currentGen == gen {
							if err := fetchCreds(); err == nil {
								credsMu.Lock()
								credGen++
								credsMu.Unlock()
								log.Printf("[ВОРКЕР #%d] Креды обновлены", wid)
							} else {
								log.Printf("[ВОРКЕР #%d] Не удалось обновить креды: %v", wid, err)
							}
						}
						refreshMu.Unlock()
						select {
						case <-time.After(2 * time.Second):
						case <-ctx.Done():
							return
						}
						continue

					case SessionErrorQuota:
						// Квота сессий на учётные данные: новые учётные данные дают
						// новую квоту. Обновляет один воркер и не чаще раза в минуту.
						refreshMu.Lock()
						credsMu.RLock()
						currentGen := credGen
						credsMu.RUnlock()
						if currentGen == gen && time.Since(lastQuotaRefresh) > time.Minute {
							lastQuotaRefresh = time.Now()
							log.Printf("[ГРУППА #%d] Квота TURN-сессий VK исчерпана — беру новые учётные данные", groupID)
							getStreamCache(credStreamID).invalidate(credStreamID)
							if err := fetchCreds(); err == nil {
								credsMu.Lock()
								credGen++
								credsMu.Unlock()
							} else {
								log.Printf("[ВОРКЕР #%d] Не удалось обновить креды: %v", wid, err)
							}
						}
						refreshMu.Unlock()
						select {
						case <-time.After(5*time.Second + time.Duration(wid%10)*500*time.Millisecond):
						case <-ctx.Done():
							return
						}
						continue

					case SessionErrorRetry:
						// Временный сбой: адрес не баним, берём следующий и ждём
						// с разбросом, чтобы воркеры не стучались в VK разом.
						relayOffset++
						select {
						case <-time.After(2*time.Second + time.Duration(wid%10)*300*time.Millisecond):
						case <-ctx.Done():
							return
						}
						continue

					case SessionErrorPolicy:
						if noteWorkerPolicy(sessErr.Limit) {
							log.Printf("[ЯДРО] Сервер WDTT-Plus ограничил доступ до %d потоков — лишние воркеры будут остановлены", sessErr.Limit)
						}
						if workerOverServerLimit(wid) {
							log.Printf("[ВОРКЕР #%d] Превышает лимит сервера (%d) — остановлен", wid, sessErr.Limit)
							return
						}
						select {
						case <-time.After(3 * time.Second):
						case <-ctx.Done():
							return
						}
						continue

					case SessionErrorFatal:
						// Фатальная ошибка — выходим
						log.Printf("[ВОРКЕР #%d] Фатальная ошибка: %v", wid, sessErr.Err)
						return

					default:
						// Неизвестный тип ошибки — спим и пробуем заново
						log.Printf("[ВОРКЕР #%d] Неизвестная ошибка: %v", wid, sessErr)
						select {
						case <-time.After(5 * time.Second):
						case <-ctx.Done():
						}
						continue
					}
				}

				// Успех — сбрасываем счётчики
				addressAttempts = 0

				// После успешной сессии — небольшая пауза перед следующим циклом
				select {
				case <-time.After(100 * time.Millisecond):
				case <-ctx.Done():
					return
				}
			}
		}(wid, idx, startDelay)
	}

	signalSpawnOnce()

	wg.Wait()
	log.Printf("[ГРУППА #%d] Все воркеры группы завершились.", groupID)
}

func shouldRaceForConfig(enabled bool, workerIndex int, configSent int32) bool {
	return enabled && configSent == 0 && workerIndex < configRacerLimit
}

func normalizeWorkers(n int) int {
	if n > 108 {
		n = 108
	}
	if n < workersPerGroup {
		n = workersPerGroup
	}
	return (n / workersPerGroup) * workersPerGroup
}

// selectTurnURL assigns workers round-robin and skips currently banned relays.
func selectTurnURL(urls []string, workerID, offset int, isBanned func(string) bool) (string, bool) {
	if len(urls) == 0 {
		return "", false
	}
	start := (workerID - 1 + offset) % len(urls)
	if start < 0 {
		start += len(urls)
	}
	for i := 0; i < len(urls); i++ {
		u := urls[(start+i)%len(urls)]
		if !isBanned(u) {
			return u, true
		}
	}
	return "", false
}

func ParseHashes(raw string) []string {
	var result []string
	seen := make(map[string]struct{})
	for _, h := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r' || r == '\t' || r == ' '
	}) {
		h = normalizeVKJoinHash(h)
		if h != "" {
			if _, exists := seen[h]; exists {
				continue
			}
			seen[h] = struct{}{}
			result = append(result, h)
		}
	}
	return result
}

func normalizeVKJoinHash(input string) string {
	s := strings.Trim(strings.TrimSpace(input), "<>\"'")
	if s == "" {
		return ""
	}

	lower := strings.ToLower(s)
	if idx := strings.Index(lower, "/call/join/"); idx >= 0 {
		s = s[idx+len("/call/join/"):]
	} else if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return ""
	}

	if idx := strings.IndexAny(s, "?#/"); idx != -1 {
		s = s[:idx]
	}
	return strings.Trim(strings.TrimSpace(s), "/")
}

type TurnParams struct {
	Host         string
	Port         string
	Hashes       []string
	WrapKey      []byte
	ObfsMode     string
	TCPTransport bool
	conn         *connectionPolicy
	excludeTURN  func(ips []string) // вывести IP TURN мимо туннеля (синхронно)
	allocateGate *allocationGate
}

// turnIPsFor резолвит адреса TURN в IP (без повторов).
func turnIPsFor(urls []string, tp *TurnParams) []string {
	seen := make(map[string]bool)
	var out []string
	for _, url := range urls {
		plain, _ := splitTURNAddress(url)
		host, _, err := net.SplitHostPort(plain)
		if err != nil {
			host = plain
		}
		if tp != nil && tp.Host != "" {
			host = tp.Host
		}
		var ips []string
		if ip := net.ParseIP(host); ip != nil {
			ips = []string{ip.String()}
		} else if resolved, err := net.LookupIP(host); err == nil {
			for _, ip := range resolved {
				ips = append(ips, ip.String())
			}
		} else {
			ips = []string{host}
		}
		for _, ip := range ips {
			if !seen[ip] {
				seen[ip] = true
				out = append(out, ip)
			}
		}
	}
	return out
}
