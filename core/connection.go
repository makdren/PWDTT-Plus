package core

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Режим «Подключение» — как в WDTT Plus.
//
//   - ConnUDP ("UDP, при недоступности — TCP/TLS"): все сессии идут к TURN по
//     UDP. На TCP/TLS клиент переходит, только когда рабочих UDP-каналов не
//     осталось совсем и новые UDP-сессии подряд не поднимаются; тогда новые
//     проверки UDP откладываются на udpBackoff. Отказ отдельного канала
//     остальные не трогает, живые сессии никогда не рвутся.
//   - ConnStream ("TCP/TLS", без внешнего UDP): сначала TURN/TLS (если VK
//     выдал адрес turns:), затем TCP к тем же адресам. Первая запись в TCP
//     делится на две части — так делает WDTT Plus для сетей с DPI.
const (
	ConnUDP    = "udp"
	ConnStream = "stream"

	TransportUDP = "udp"
	TransportTCP = "tcp"
	TransportTLS = "tls"

	udpFailThreshold = 4               // подряд неудачных UDP-сессий без рабочих UDP
	udpBackoff       = 5 * time.Minute // пауза новых проверок UDP

	tlsURLPrefix = "tls://" // так в списке адресов помечаются turns:-адреса VK
)

// NormalizeConnectionMode переводит настройки в режим. Старые значения:
// auto/udp → UDP, tcp → TCP/TLS.
func NormalizeConnectionMode(mode string, legacyTCP bool) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case ConnStream, "tcp", "tls", "tcp/tls":
		return ConnStream
	case ConnUDP, "auto":
		return ConnUDP
	}
	if legacyTCP {
		return ConnStream
	}
	return ConnUDP
}

// turnAddressFromURL превращает URL из ответа VK в адрес для воркеров:
// "turn:1.2.3.4:19302?transport=udp" → "1.2.3.4:19302",
// "turns:turn.vk.com:443?transport=tcp" → "tls://turn.vk.com:443".
func turnAddressFromURL(raw string) string {
	raw = strings.TrimSpace(raw)
	lower := strings.ToLower(raw)
	clean := strings.Split(raw, "?")[0]
	switch {
	case strings.HasPrefix(lower, "turns:"):
		addr := strings.TrimPrefix(clean[len("turns:"):], "//")
		if _, _, err := net.SplitHostPort(addr); err != nil {
			addr = net.JoinHostPort(strings.Trim(addr, "[]"), "5349")
		}
		return tlsURLPrefix + addr
	case strings.HasPrefix(lower, "turn:"):
		return strings.TrimPrefix(clean[len("turn:"):], "//")
	}
	return clean
}

// splitTURNAddress: адрес без пометки и признак TLS.
func splitTURNAddress(addr string) (string, bool) {
	if rest, ok := strings.CutPrefix(addr, tlsURLPrefix); ok {
		return rest, true
	}
	return addr, false
}

type connectionPolicy struct {
	mode string
	sni  string

	mu         sync.Mutex
	udpActive  int
	udpStreak  int
	retryAfter time.Time
}

func newConnectionPolicy(mode, frontSNI string) *connectionPolicy {
	p := &connectionPolicy{mode: NormalizeConnectionMode(mode, false), sni: frontSNI}
	if p.mode == ConnStream {
		log.Printf("[ПОДКЛЮЧЕНИЕ] TCP/TLS: внешний UDP не используется")
	} else {
		log.Printf("[ПОДКЛЮЧЕНИЕ] UDP: сначала UDP, при недоступности — TCP/TLS")
	}
	if frontSNI != "" {
		log.Printf("[TURN] Внешний TLS SNI=%s", frontSNI)
	}
	return p
}

func (p *connectionPolicy) frontSNI() string {
	if p == nil {
		return ""
	}
	return p.sni
}

// splitFirstWrite — в режиме TCP/TLS первая запись в TCP делится (как в WDTT Plus).
func (p *connectionPolicy) splitFirstWrite() bool { return p != nil && p.mode == ConnStream }

// streamOnly — новые сессии должны идти по TCP/TLS.
func (p *connectionPolicy) streamOnly() bool {
	if p == nil {
		return false
	}
	if p.mode == ConnStream {
		return true
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return time.Now().Before(p.retryAfter)
}

// filterURLs выбирает адреса под текущий режим. UDP — только обычные
// адреса; TCP/TLS — сначала незабаненные TLS-адреса, иначе TCP к обычным.
func (p *connectionPolicy) filterURLs(urls []string, isBanned func(string) bool) []string {
	var plain, tlsURLs []string
	for _, u := range urls {
		if _, isTLS := splitTURNAddress(u); isTLS {
			tlsURLs = append(tlsURLs, u)
		} else {
			plain = append(plain, u)
		}
	}
	if !p.streamOnly() {
		if len(plain) > 0 {
			return plain
		}
		return tlsURLs
	}
	for _, u := range tlsURLs {
		if !isBanned(u) {
			return tlsURLs
		}
	}
	if len(plain) > 0 {
		return plain
	}
	return tlsURLs
}

// transportFor — транспорт для сессии к выбранному адресу.
func (p *connectionPolicy) transportFor(addr string) string {
	if _, isTLS := splitTURNAddress(addr); isTLS {
		return TransportTLS
	}
	if p.streamOnly() {
		return TransportTCP
	}
	return TransportUDP
}

// sessionUp/sessionDown — UDP-сессия, по которой пришли данные с VPS.
func (p *connectionPolicy) sessionUp(transport string) {
	if p == nil || transport != TransportUDP {
		return
	}
	p.mu.Lock()
	p.udpActive++
	p.udpStreak = 0
	p.mu.Unlock()
}

func (p *connectionPolicy) sessionDown(transport string) {
	if p == nil || transport != TransportUDP {
		return
	}
	p.mu.Lock()
	if p.udpActive > 0 {
		p.udpActive--
	}
	p.mu.Unlock()
}

// udpFailed — UDP-сессия не дошла до данных с VPS. Пока работает хоть одна
// UDP-сессия, это сбой канала (часто — VK не успевает за всплеском запросов),
// а не недоступность UDP.
func (p *connectionPolicy) udpFailed() {
	if p == nil || p.mode != ConnUDP {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.udpActive > 0 || time.Now().Before(p.retryAfter) {
		p.udpStreak = 0
		return
	}
	p.udpStreak++
	if p.udpStreak >= udpFailThreshold {
		p.udpStreak = 0
		p.retryAfter = time.Now().Add(udpBackoff)
		log.Printf("[ПОДКЛЮЧЕНИЕ] Рабочих UDP-каналов нет. Пробуем TCP/TLS; новые проверки UDP отложены на %v", udpBackoff)
	}
}

// ── TCP/TLS до TURN ───────────────────────────────────────────────

// splitFirstWriteConn делит первую запись (STUN Allocate) на две части с
// паузой — DPI, ищущий STUN в первом сегменте TCP, его не видит.
type splitFirstWriteConn struct {
	net.Conn
	splitAt int
	delay   time.Duration
	done    atomic.Bool
}

func (c *splitFirstWriteConn) Write(b []byte) (int, error) {
	if !c.done.CompareAndSwap(false, true) || len(b) <= c.splitAt {
		return c.Conn.Write(b)
	}
	n1, err := writeFull(c.Conn, b[:c.splitAt])
	if err != nil {
		return n1, err
	}
	time.Sleep(c.delay)
	n2, err := writeFull(c.Conn, b[c.splitAt:])
	return n1 + n2, err
}

func writeFull(conn net.Conn, b []byte) (int, error) {
	total := 0
	for len(b) > 0 {
		n, err := conn.Write(b)
		total += n
		b = b[n:]
		if err != nil {
			return total, err
		}
		if n == 0 {
			return total, io.ErrShortWrite
		}
	}
	return total, nil
}

// NormalizeFrontSNI проверяет внешний SNI для TURN/TLS ("" — без подмены).
func NormalizeFrontSNI(value string) (string, error) {
	host := strings.ToLower(strings.TrimSpace(value))
	if host == "" {
		return "", nil
	}
	if len(host) > 253 || net.ParseIP(host) != nil || strings.HasPrefix(host, ".") ||
		strings.HasSuffix(host, ".") || strings.Contains(host, "..") {
		return "", fmt.Errorf("ожидалось доменное имя длиной до 253 символов")
	}
	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		return "", fmt.Errorf("ожидалось доменное имя как ya.ru")
	}
	for _, label := range labels {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", fmt.Errorf("некорректная метка домена")
		}
		for _, ch := range label {
			if (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') && ch != '-' {
				return "", fmt.Errorf("разрешены только латинские буквы, цифры, дефис и точки")
			}
		}
	}
	return host, nil
}

// turnTLSConfig — как в WDTT Plus: при подмене SNI сертификат всё равно
// проверяется по настоящему имени TURN-узла из ответа VK.
func turnTLSConfig(host, frontSNI string) *tls.Config {
	config := &tls.Config{MinVersion: tls.VersionTLS12}
	serverName := host
	if frontSNI != "" {
		serverName = frontSNI
	}
	if net.ParseIP(serverName) == nil {
		config.ServerName = serverName
	}
	if frontSNI != "" && !strings.EqualFold(frontSNI, host) {
		config.InsecureSkipVerify = true // проверка перенесена в VerifyConnection
		config.VerifyConnection = func(state tls.ConnectionState) error {
			return verifyTURNPeerCertificate(state, host)
		}
	}
	return config
}

func verifyTURNPeerCertificate(state tls.ConnectionState, hostname string) error {
	if len(state.PeerCertificates) == 0 {
		return fmt.Errorf("TURN TLS не прислал сертификат")
	}
	intermediates := x509.NewCertPool()
	for _, cert := range state.PeerCertificates[1:] {
		intermediates.AddCert(cert)
	}
	_, err := state.PeerCertificates[0].Verify(x509.VerifyOptions{
		Intermediates: intermediates,
		DNSName:       hostname,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	})
	if err != nil {
		return fmt.Errorf("проверка цепочки сертификата TURN TLS: %w", err)
	}
	return nil
}
