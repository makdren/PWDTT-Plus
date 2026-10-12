package core

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cbeuw/connutil"
	"github.com/pion/dtls/v3"
	"github.com/pion/dtls/v3/pkg/crypto/selfsign"
	"github.com/pion/turn/v5"
)

const (
	workerSendBuf      = 128
	sessionReadTimeout = 30 * time.Minute
	readBufSize        = 1600
	socketBufSize      = 625 * 1024
	keepaliveByte      = 0xFF
	keepaliveInterval  = 15 * time.Second
	// Сервер WDTT-Plus отвечает на keepalive. Если ответы шли, а потом от
	// сервера ничего не приходит дольше этого времени — путь «молча» умер:
	// перезапускаем сессию, иначе её доля трафика уходит в никуда.
	keepalivePongTimeout = 45 * time.Second
	// Дедлайны DTLS (pion) — мьютекс и перезапуск таймера на каждый вызов.
	// Они нужны только против вечной блокировки, поэтому обновляем их
	// не на каждом пакете, а не чаще раза в deadlineRefresh.
	deadlineRefresh = time.Minute
)

var handshakeSem = make(chan struct{}, 3)

type connectedUDPConn struct{ *net.UDPConn }

func (c *connectedUDPConn) WriteTo(p []byte, _ net.Addr) (int, error) { return c.Write(p) }

func dialTURNConn(turnAddr string, tcp bool) (net.PacketConn, error) {
	if !tcp {
		resolved, err := net.ResolveUDPAddr("udp", turnAddr)
		if err != nil {
			return nil, fmt.Errorf("резолв TURN: %w", err)
		}
		c, err := net.DialUDP("udp", nil, resolved)
		if err != nil {
			return nil, fmt.Errorf("подключение TURN UDP: %w", err)
		}
		_ = c.SetReadBuffer(socketBufSize)
		_ = c.SetWriteBuffer(socketBufSize)
		return &connectedUDPConn{c}, nil
	}

	d := net.Dialer{Timeout: 10 * time.Second}
	c, err := d.Dial("tcp", turnAddr)
	if err != nil {
		return nil, fmt.Errorf("подключение TURN TCP: %w", err)
	}
	return turn.NewSTUNConn(c), nil
}

// SessionErrorType — тип ошибки сессии
type SessionErrorType string

const (
	// ADDRESS_DEAD — этот конкретный TURN-адрес не работает (квота, unreachable, timeout)
	SessionErrorAddressDead SessionErrorType = "ADDRESS_DEAD"
	// WRAP_TIMEOUT — DTLS-рукопожатие не прошло, нужно сменить обфускацию
	SessionErrorWrapTimeout SessionErrorType = "WRAP_TIMEOUT"
	// AUTH — TURN отклонил креды (401/протухшие); нужно обновить креды и повторить
	SessionErrorAuth SessionErrorType = "AUTH"
	// FATAL — невосстановимая ошибка (FATAL_AUTH, хеш мёртв и т.д.)
	SessionErrorFatal SessionErrorType = "FATAL"
	// POLICY — сервер WDTT-Plus ограничил число потоков доступа (POLICY:max_workers=N)
	SessionErrorPolicy SessionErrorType = "POLICY"
	// RETRY — временный сбой на стороне VPS; адрес TURN исправен, повторяем позже
	SessionErrorRetry SessionErrorType = "RETRY"
)

// SessionError — структурированная ошибка сессии
type SessionError struct {
	Type    SessionErrorType
	Address string // TURN-адрес, на котором произошла ошибка (если применимо)
	Err     error
	Limit   int // для POLICY: лимит потоков, присланный сервером
}

func (e *SessionError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v (addr=%s)", e.Type, e.Err, e.Address)
	}
	return fmt.Sprintf("%s (addr=%s)", e.Type, e.Address)
}

func (e *SessionError) Unwrap() error { return e.Err }

// RunSession устанавливает TURN-сессию с указанным адресом.
// В отличие от предыдущей версии, адрес передаётся явно, а не выбирается по индексу.
func RunSession(
	ctx context.Context,
	tp *TurnParams,
	peer *net.UDPAddr,
	d *Dispatcher,
	localPort string,
	getConfig bool,
	configCh chan<- string,
	sessionID int,
	turnAddr string, // конкретный TURN-адрес
	turnUser string, // username для TURN
	turnPass string, // password для TURN
	obfsMode string, // режим обфускации (пер-воркерский, может меняться на WRAP_TIMEOUT)
	cacheStreamID int, // для handleAuthError
	deviceID, password string,
	stats *Stats,
) (_ bool, sessErr *SessionError) {
	configDelivered := false

	// Транспорт до TURN выбирается на каждую сессию (UDP/TCP/авто).
	transport := tp.selector.Pick(sessionID)
	useTCP := transport == TransportTCP
	setupStart := time.Now()
	// Успех транспорта — первые данные от VPS по этой сессии, а не голое
	// DTLS-рукопожатие: по TCP рукопожатие может пройти, а данные — нет.
	var serverReplied atomic.Bool
	markReplied := func() {
		if serverReplied.CompareAndSwap(false, true) {
			tp.selector.Report(transport, true, time.Since(setupStart))
		}
	}
	defer func() {
		if ctx.Err() != nil || serverReplied.Load() || sessErr == nil {
			return
		}
		switch sessErr.Type {
		case SessionErrorAddressDead, SessionErrorWrapTimeout, SessionErrorRetry:
			tp.selector.Report(transport, false, 0)
		}
	}()
	var firstWireRead atomic.Bool

	if turnAddr == "" {
		return false, &SessionError{
			Type: SessionErrorFatal,
			Err:  fmt.Errorf("пустой TURN-адрес"),
		}
	}

	urlhost, urlport, err := net.SplitHostPort(turnAddr)
	if err != nil {
		return false, &SessionError{
			Type:    SessionErrorAddressDead,
			Address: turnAddr,
			Err:     fmt.Errorf("разбор TURN-адреса: %w", err),
		}
	}
	if tp.Host != "" {
		urlhost = tp.Host
	}
	if tp.Port != "" {
		urlport = tp.Port
	}
	turnAddrResolved := net.JoinHostPort(urlhost, urlport)

	turnConn, err := dialTURNConn(turnAddrResolved, useTCP)
	if err != nil {
		return false, &SessionError{
			Type:    SessionErrorAddressDead,
			Address: turnAddr,
			Err:     err,
		}
	}
	defer turnConn.Close()

	if useTCP {
		log.Printf("[СЕССИЯ #%d] TURN TCP (%s)", sessionID, turnAddr)
	} else {
		log.Printf("[СЕССИЯ #%d] TURN UDP (%s)", sessionID, turnAddr)
	}

	var addrFamily turn.RequestedAddressFamily
	if peer.IP.To4() != nil {
		addrFamily = turn.RequestedAddressFamilyIPv4
	} else {
		addrFamily = turn.RequestedAddressFamilyIPv6
	}

	tc, err := turn.NewClient(&turn.ClientConfig{
		STUNServerAddr:         turnAddrResolved,
		TURNServerAddr:         turnAddrResolved,
		Conn:                   turnConn,
		Username:               turnUser,
		Password:               turnPass,
		RequestedAddressFamily: addrFamily,
		LoggerFactory:          &NullLoggerFactory{},
	})
	if err != nil {
		return false, &SessionError{
			Type:    SessionErrorAddressDead,
			Address: turnAddr,
			Err:     fmt.Errorf("TURN клиент: %w", err),
		}
	}
	defer tc.Close()

	if err = tc.Listen(); err != nil {
		return false, &SessionError{
			Type:    SessionErrorAddressDead,
			Address: turnAddr,
			Err:     fmt.Errorf("TURN Listen: %w", err),
		}
	}

	if tp.allocateGate != nil {
		if err = tp.allocateGate.Wait(ctx); err != nil {
			return false, &SessionError{
				Type:    SessionErrorFatal,
				Address: turnAddr,
				Err:     fmt.Errorf("ожидание TURN Allocate: %w", err),
			}
		}
	}

	relay, err := tc.Allocate()
	if err != nil {
		errStr := err.Error()
		errLower := strings.ToLower(errStr)

		// Проверяем на ошибки авторизации (кеш кредов)
		if isAuthError(err) {
			handleAuthError(cacheStreamID)
			// Креды протухли — это НЕ фатальная ошибка: воркер обновит
			// креды через GetCreds (кеш уже инвалидирован выше) и повторит
			return false, &SessionError{
				Type:    SessionErrorAuth,
				Address: turnAddr,
				Err:     fmt.Errorf("TURN Allocate: %w", err),
			}
		}

		// VK не ответил на Allocate (часто — всплеск запросов): не фатально,
		// воркер повторит позже. Раньше воркер останавливался навсегда.
		if strings.Contains(errLower, "retransmission") {
			return false, &SessionError{
				Type:    SessionErrorRetry,
				Address: turnAddr,
				Err:     fmt.Errorf("TURN Allocate: %w", err),
			}
		}

		// Квота, unreachable, timeout — всё это "адрес мёртв"
		if strings.Contains(errLower, "quota") ||
			strings.Contains(errLower, "486") ||
			strings.Contains(errLower, "unreachable") ||
			strings.Contains(errLower, "timeout") ||
			strings.Contains(errLower, "connection refused") ||
			strings.Contains(errLower, "no route to host") {
			return false, &SessionError{
				Type:    SessionErrorAddressDead,
				Address: turnAddr,
				Err:     fmt.Errorf("TURN Allocate: %w", err),
			}
		}

		// Остальные ошибки — фатальные
		return false, &SessionError{
			Type:    SessionErrorFatal,
			Address: turnAddr,
			Err:     fmt.Errorf("TURN Allocate: %w", err),
		}
	}
	defer relay.Close()

	getStreamCache(cacheStreamID).errorCount.Store(0)

	log.Printf("[СЕССИЯ #%d] Relay: %s; peer: %s", sessionID, relay.LocalAddr(), peer)

	// Establish and verify the TURN permission before DTLS starts. Otherwise a
	// failure is hidden inside the relay-writing goroutine and looks like a DTLS
	// or password timeout.
	if err = tc.CreatePermission(peer); err != nil {
		return false, &SessionError{
			Type:    SessionErrorAddressDead,
			Address: turnAddr,
			Err:     fmt.Errorf("TURN CreatePermission для %s: %w", peer, err),
		}
	}
	log.Printf("[СЕССИЯ #%d] TURN permission OK для %s", sessionID, peer)

	pipeA, pipeB := connutil.AsyncPacketPipe()

	sessCtx, sessCancel := context.WithCancel(ctx)
	// ВАЖНО: defer sessCancel() регистрируется НИЖЕ, рядом с defer stopRelay().
	// Defers работают LIFO, и sessCancel обязан выполниться первым — иначе
	// stopRelay снимет AfterFunc до его срабатывания, дедлайны не выставятся,
	// и горутины релея навсегда зависнут в ReadFrom (утечка на каждом
	// error-пути сессии).

	var sessionWg sync.WaitGroup
	sessionWg.Add(1)
	go func() {
		defer sessionWg.Done()
		t := time.NewTicker(10 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-sessCtx.Done():
				return
			case <-t.C:
				tc.SendBindingRequest()
			}
		}
	}()

	var relayWg sync.WaitGroup
	relayWg.Add(2)

	useWrap := len(tp.WrapKey) == wrapKeyLen

	var obfsCfg *ObfsConfig
	var obfsWriteState *ObfsState
	if useWrap {
		obfsCfg = NewObfsConfig(obfsMode)
		obfsWriteState = NewObfsState()
	}

	stopRelay := context.AfterFunc(sessCtx, func() {
		_ = relay.SetDeadline(time.Now())
		_ = pipeA.SetDeadline(time.Now())
	})
	defer stopRelay()
	defer sessCancel()

	go func() {
		defer relayWg.Done()
		defer sessCancel()

		readBufLen := readBufSize + 80
		buf := make([]byte, readBufLen)
		plain := make([]byte, readBufSize)
		for {
			n, _, readErr := relay.ReadFrom(buf)
			if readErr != nil {
				return
			}
			if firstWireRead.CompareAndSwap(false, true) {
				log.Printf("[СЕССИЯ #%d] Первый пакет получен через TURN от %s (%d байт)", sessionID, peer, n)
			}
			payload := buf[:n]
			if useWrap {
				if !obfsIsRTPPacket(payload) {
					log.Printf("[СЕССИЯ #%d] OBFS unwrap: unexpected packet (n=%d)", sessionID, n)
					continue
				}
				m, wrapErr := obfsUnwrapPacket(tp.WrapKey, payload, plain)
				if wrapErr != nil {
					log.Printf("[СЕССИЯ #%d] OBFS unwrap: %v (n=%d)", sessionID, wrapErr, n)
					continue
				}
				payload = plain[:m]
			}
			if _, writeErr := pipeA.WriteTo(payload, peer); writeErr != nil {
				return
			}
		}
	}()

	go func() {
		defer relayWg.Done()
		defer sessCancel()
		b := make([]byte, readBufSize)
		wrapBuf := make([]byte, 0, readBufSize+128)
		firstWrite := true
		for {
			n, _, readErr := pipeA.ReadFrom(b)
			if readErr != nil {
				return
			}
			out := b[:n]
			if useWrap {
				if obfsCfg != nil && obfsWriteState != nil {
					wrapped, wrapErr := obfsWrapPacketInto(wrapBuf, tp.WrapKey, out, obfsCfg, obfsWriteState)
					if wrapErr != nil {
						log.Printf("[СЕССИЯ #%d] OBFS wrap: %v", sessionID, wrapErr)
						return
					}
					out = wrapped
				}
			}
			written, writeErr := relay.WriteTo(out, peer)
			if writeErr != nil {
				log.Printf("[СЕССИЯ #%d] TURN relay WriteTo %s: %v", sessionID, peer, writeErr)
				return
			}
			if firstWrite {
				log.Printf("[СЕССИЯ #%d] Первый пакет отправлен через TURN к %s (%d/%d байт)", sessionID, peer, written, len(out))
				firstWrite = false
			}
		}
	}()

	cert, err := selfsign.GenerateSelfSigned()
	if err != nil {
		return false, &SessionError{
			Type:    SessionErrorFatal,
			Address: turnAddr,
			Err:     fmt.Errorf("генерация сертификата: %w", err),
		}
	}

	select {
	case handshakeSem <- struct{}{}:
	case <-sessCtx.Done():
		errType := SessionErrorRetry // релей TURN закрылся, пока ждали очередь
		if ctx.Err() != nil {
			errType = SessionErrorFatal // общее отключение
		}
		return false, &SessionError{
			Type:    errType,
			Address: turnAddr,
			Err:     sessCtx.Err(),
		}
	}

	dtlsCfg := &dtls.Config{
		Certificates:          []tls.Certificate{cert},
		InsecureSkipVerify:    true,
		ExtendedMasterSecret:  dtls.RequireExtendedMasterSecret,
		CipherSuites:          []dtls.CipherSuiteID{dtls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256},
		ConnectionIDGenerator: dtls.OnlySendCIDGenerator(),
	}

	dtlsConn, err := dtls.Client(pipeB, peer, dtlsCfg)
	if err != nil {
		<-handshakeSem
		return false, &SessionError{
			Type:    SessionErrorAddressDead,
			Address: turnAddr,
			Err:     fmt.Errorf("DTLS клиент: %w", err),
		}
	}
	defer dtlsConn.Close()

	hctx, hcancel := context.WithTimeout(sessCtx, 20*time.Second)
	log.Printf("[ВОРКЕР #%d] [DTLS] Рукопожатие (Handshake)...", sessionID)
	err = dtlsConn.HandshakeContext(hctx)
	hcancel()
	<-handshakeSem

	if err != nil {
		errStr := strings.ToLower(err.Error())
		if strings.Contains(errStr, "deadline") || strings.Contains(errStr, "timeout") {
			if !firstWireRead.Load() {
				return false, &SessionError{
					Type:    SessionErrorAddressDead,
					Address: turnAddr,
					Err:     fmt.Errorf("TURN relay молчит: ни один пакет от peer не вернулся"),
				}
			}
			if useWrap {
				return false, &SessionError{
					Type:    SessionErrorWrapTimeout,
					Address: turnAddr,
					Err:     fmt.Errorf("peer ответил, но WRAP/DTLS не подтверждён"),
				}
			}
		}
		if errors.Is(err, context.Canceled) || sessCtx.Err() != nil {
			// Сессию отменили (закрылся релей TURN или идёт отключение):
			// адрес исправен — не баним, просто повторяем.
			return false, &SessionError{
				Type:    SessionErrorRetry,
				Address: turnAddr,
				Err:     fmt.Errorf("DTLS хендшейк прерван: %w", err),
			}
		}
		return false, &SessionError{
			Type:    SessionErrorAddressDead,
			Address: turnAddr,
			Err:     fmt.Errorf("DTLS хендшейк: %w", err),
		}
	}
	log.Printf("[ВОРКЕР #%d] [DTLS] Соединение установлено ✓", sessionID)

	stats.ActiveConnections.Add(1)
	defer stats.ActiveConnections.Add(-1)

	// Режим сервера: первые воркеры узнают его из ответа на GETCONF,
	// остальные ждут (WDTT-Plus требует GETCONF от каждого воркера).
	mode := currentServerMode()
	if !(getConfig && configCh != nil) && mode == serverModeUnknown {
		mode = waitServerMode(sessCtx, serverModeWaitTime)
	}
	needConfig := (getConfig && configCh != nil) || mode != serverModeLegacy

	if needConfig {
		var conf string
		var confErr error
		if mode == serverModeMux {
			conf, confErr = requestMuxConfig(dtlsConn, localPort, deviceID, password, sessionID)
		} else {
			conf, confErr = RequestConfig(dtlsConn, localPort, deviceID, password)
		}
		if confErr != nil {
			var policyErr *workerPolicyError
			if errors.As(confErr, &policyErr) {
				return false, &SessionError{
					Type:    SessionErrorPolicy,
					Address: turnAddr,
					Err:     confErr,
					Limit:   policyErr.maxWorkers,
				}
			}
			errStr := confErr.Error()
			if strings.Contains(errStr, "FATAL_AUTH") {
				return false, &SessionError{
					Type:    SessionErrorFatal,
					Address: turnAddr,
					Err:     confErr,
				}
			}
			log.Printf("[ВОРКЕР #%d] Ошибка конфига: %v", sessionID, confErr)
			if mode == serverModeMux {
				// Без авторизации сервер WDTT-Plus не подключит воркер к общему каналу.
				return false, &SessionError{Type: SessionErrorRetry, Address: turnAddr, Err: confErr}
			}
		} else if conf != "" {
			markReplied()
			if noteServerConfig(conf) == serverModeMux {
				if mode != serverModeMux {
					log.Printf("[ЯДРО] Сервер WDTT-Plus: включён многопоточный режим (WDTT_MUX1)")
				}
				mode = serverModeMux
			}
			if getConfig && configCh != nil {
				select {
				case configCh <- conf:
					configDelivered = true
					log.Printf("[ВОРКЕР #%d] Конфиг получен", sessionID)
				default:
					configDelivered = true
				}
			}
		} else {
			log.Printf("[ВОРКЕР #%d] Сервер ещё не выдал WireGuard-конфиг, повторим позже", sessionID)
		}
		if mode == serverModeMux {
			if err := startMux(dtlsConn); err != nil {
				return false, &SessionError{Type: SessionErrorRetry, Address: turnAddr, Err: fmt.Errorf("WDTT_MUX1: %w", err)}
			}
		}
	} else {
		if authErr := SendAuth(dtlsConn, deviceID, password); authErr != nil {
			log.Printf("[ВОРКЕР #%d] Ошибка авторизации: %v", sessionID, authErr)
		}
	}

	emitReady()

	var serverPolicy atomic.Int32
	var serverDenied atomic.Value

	slot := &WorkerSlot{
		ID:     sessionID,
		SendCh: make(chan []byte, workerSendBuf),
	}
	d.Register(slot)
	defer d.Unregister(slot)

	var proxyWg sync.WaitGroup
	proxyWg.Add(3)

	var lastServerRx atomic.Int64
	lastServerRx.Store(time.Now().UnixNano())
	var pongSeen atomic.Bool
	// Когда пора продлить длинный дедлайн записи (unix nano).
	var writeDeadlineDue atomic.Int64

	stopDTLS := context.AfterFunc(sessCtx, func() {
		_ = dtlsConn.SetDeadline(time.Now())
	})
	defer stopDTLS()

	go func() {
		defer proxyWg.Done()
		t := time.NewTicker(keepaliveInterval)
		defer t.Stop()
		ping := []byte{keepaliveByte}
		for {
			select {
			case <-sessCtx.Done():
				return
			case <-t.C:
				if pongSeen.Load() {
					silent := time.Since(time.Unix(0, lastServerRx.Load()))
					if silent > keepalivePongTimeout {
						log.Printf("[ВОРКЕР #%d] Сервер молчит %v — перезапуск зависшего пути", sessionID, silent.Round(time.Second))
						sessCancel()
						return
					}
				}
				now := time.Now()
				_ = dtlsConn.SetWriteDeadline(now.Add(5 * time.Second))
				if _, err := dtlsConn.Write(ping); err != nil {
					return
				}
				// Возвращаем длинный дедлайн для потока данных.
				_ = dtlsConn.SetWriteDeadline(now.Add(sessionReadTimeout))
				writeDeadlineDue.Store(now.Add(deadlineRefresh).UnixNano())
			}
		}
	}()

	go func() {
		defer proxyWg.Done()
		defer sessCancel()
		for {
			select {
			case <-sessCtx.Done():
				return
			case pkt, ok := <-slot.SendCh:
				if !ok {
					return
				}
				if now := time.Now(); now.UnixNano() >= writeDeadlineDue.Load() {
					_ = dtlsConn.SetWriteDeadline(now.Add(sessionReadTimeout))
					writeDeadlineDue.Store(now.Add(deadlineRefresh).UnixNano())
				}
				_, writeErr := dtlsConn.Write(pkt)
				putPktBuf(pkt)
				if writeErr != nil {
					log.Printf("[ВОРКЕР #%d] Ошибка Writer: %v", sessionID, writeErr)
					return
				}
			}
		}
	}()

	go func() {
		defer proxyWg.Done()
		defer sessCancel()
		var readDeadlineDue time.Time
		for {
			pkt := getPktBuf(2048)
			if now := time.Now(); !now.Before(readDeadlineDue) {
				_ = dtlsConn.SetReadDeadline(now.Add(sessionReadTimeout))
				readDeadlineDue = now.Add(deadlineRefresh)
			}
			n, readErr := dtlsConn.Read(pkt)
			if readErr != nil {
				putPktBuf(pkt)
				if sessCtx.Err() != nil {
					return
				}
				if ne, ok := readErr.(net.Error); ok && ne.Timeout() {
					continue
				}
				log.Printf("[ВОРКЕР #%d] [RECONNECT] Reader завершён: %v", sessionID, readErr)
				return
			}

			lastServerRx.Store(time.Now().UnixNano())
			markReplied()
			if n == 1 && pkt[0] == keepaliveByte {
				pongSeen.Store(true)
				putPktBuf(pkt)
				continue
			}

			// Служебные ответы сервера WDTT-Plus не являются WireGuard-пакетами.
			if n > 7 && (pkt[0] == 'P' || pkt[0] == 'D') {
				if limit, ok := parseWorkerPolicy(string(pkt[:n])); ok {
					putPktBuf(pkt)
					serverPolicy.Store(int32(limit))
					return
				}
				if reason, ok := strings.CutPrefix(string(pkt[:n]), "DENIED:"); ok && isDeniedReason(reason) {
					putPktBuf(pkt)
					serverDenied.Store(reason)
					return
				}
			}

			pkt = pkt[:n]
			select {
			case d.ReturnCh <- pkt:
			case <-sessCtx.Done():
				putPktBuf(pkt)
				return
			}
		}
	}()

	proxyWg.Wait()
	sessCancel()
	relayWg.Wait()
	sessionWg.Wait()
	_ = pipeA.Close()
	_ = pipeB.Close()
	log.Printf("[СЕССИЯ #%d] Завершена", sessionID)
	if limit := int(serverPolicy.Load()); limit > 0 {
		return configDelivered, &SessionError{
			Type:    SessionErrorPolicy,
			Address: turnAddr,
			Err:     &workerPolicyError{maxWorkers: limit},
			Limit:   limit,
		}
	}
	if reason, _ := serverDenied.Load().(string); reason != "" {
		return configDelivered, &SessionError{
			Type:    SessionErrorFatal,
			Address: turnAddr,
			Err:     fmt.Errorf("FATAL_AUTH: сервер отказал в доступе (%s)", reason),
		}
	}
	return configDelivered, nil
}

func isDeniedReason(reason string) bool {
	if reason == "" || len(reason) > 32 {
		return false
	}
	for _, r := range reason {
		if !(r == '_' || (r >= 'a' && r <= 'z')) {
			return false
		}
	}
	return true
}
