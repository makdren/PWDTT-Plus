package core

import (
	"net"
	"testing"
	"time"
)

func TestTurnAddressFromURL(t *testing.T) {
	cases := map[string]string{
		"turn:1.2.3.4:19302?transport=udp":    "1.2.3.4:19302",
		"turn:1.2.3.4:19302?transport=tcp":    "1.2.3.4:19302",
		"turns:turn.vk.com:443?transport=tcp": "tls://turn.vk.com:443",
		"turns:turn.vk.com":                   "tls://turn.vk.com:5349",
		"1.2.3.4:19302":                       "1.2.3.4:19302",
	}
	for in, want := range cases {
		if got := turnAddressFromURL(in); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeConnectionMode(t *testing.T) {
	cases := []struct {
		mode   string
		legacy bool
		want   string
	}{
		{"", false, ConnUDP}, {"auto", false, ConnUDP}, {"udp", false, ConnUDP},
		{"tcp", false, ConnStream}, {"stream", false, ConnStream}, {"", true, ConnStream},
	}
	for _, c := range cases {
		if got := NormalizeConnectionMode(c.mode, c.legacy); got != c.want {
			t.Errorf("%q/%v → %q", c.mode, c.legacy, got)
		}
	}
}

func TestFilterURLs(t *testing.T) {
	urls := []string{"1.1.1.1:19302", "tls://turn.vk.com:443", "2.2.2.2:19302"}
	none := func(string) bool { return false }

	udp := newConnectionPolicy(ConnUDP, "")
	if got := udp.filterURLs(urls, none); len(got) != 2 || got[0] != "1.1.1.1:19302" {
		t.Fatalf("udp: %v", got)
	}
	if udp.transportFor("1.1.1.1:19302") != TransportUDP {
		t.Fatal("udp transport")
	}

	stream := newConnectionPolicy(ConnStream, "")
	if got := stream.filterURLs(urls, none); len(got) != 1 || got[0] != "tls://turn.vk.com:443" {
		t.Fatalf("stream prefers TLS: %v", got)
	}
	banned := func(u string) bool { return u == "tls://turn.vk.com:443" }
	if got := stream.filterURLs(urls, banned); len(got) != 2 {
		t.Fatalf("stream falls back to TCP: %v", got)
	}
	if stream.transportFor("1.1.1.1:19302") != TransportTCP || stream.transportFor("tls://turn.vk.com:443") != TransportTLS {
		t.Fatal("stream transports")
	}
}

// Пока работает хоть одна UDP-сессия, сбои отдельных каналов не уводят на TCP.
func TestUDPFallbackOnlyWithoutWorkingUDP(t *testing.T) {
	p := newConnectionPolicy(ConnUDP, "")
	p.sessionUp(TransportUDP)
	for i := 0; i < 20; i++ {
		p.udpFailed()
	}
	if p.streamOnly() {
		t.Fatal("must stay on UDP while a UDP session works")
	}
	p.sessionDown(TransportUDP)
	for i := 0; i < udpFailThreshold-1; i++ {
		p.udpFailed()
	}
	if p.streamOnly() {
		t.Fatal("switched too early")
	}
	p.udpFailed()
	if !p.streamOnly() || p.transportFor("1.1.1.1:19302") != TransportTCP {
		t.Fatal("expected TCP/TLS fallback")
	}
	p.mu.Lock()
	p.retryAfter = time.Now().Add(-time.Second)
	p.mu.Unlock()
	if p.streamOnly() {
		t.Fatal("UDP must be retried after backoff")
	}
}

func TestStreamModeIgnoresUDPAccounting(t *testing.T) {
	p := newConnectionPolicy(ConnStream, "")
	p.udpFailed()
	if !p.streamOnly() || !p.splitFirstWrite() {
		t.Fatal("stream mode")
	}
}

func TestNormalizeFrontSNI(t *testing.T) {
	if v, err := NormalizeFrontSNI(" Ya.RU "); err != nil || v != "ya.ru" {
		t.Fatalf("%q %v", v, err)
	}
	for _, bad := range []string{"1.2.3.4", "localhost", "a..b", "кириллица.рф"} {
		if _, err := NormalizeFrontSNI(bad); err == nil {
			t.Errorf("%q must be rejected", bad)
		}
	}
}

func TestTLSConfigFrontSNI(t *testing.T) {
	c := turnTLSConfig("turn.vk.com", "ya.ru")
	if c.ServerName != "ya.ru" || !c.InsecureSkipVerify || c.VerifyConnection == nil {
		t.Fatal("front SNI must verify the real TURN name")
	}
	c = turnTLSConfig("turn.vk.com", "")
	if c.ServerName != "turn.vk.com" || c.InsecureSkipVerify {
		t.Fatal("plain TLS")
	}
}

func TestSplitFirstWrite(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	conn := &splitFirstWriteConn{Conn: a, splitAt: 6, delay: time.Millisecond}
	go func() { _, _ = conn.Write([]byte("0123456789")) }()
	buf := make([]byte, 16)
	n, _ := b.Read(buf)
	if n != 6 {
		t.Fatalf("first segment %d bytes, want 6", n)
	}
	n, _ = b.Read(buf)
	if n != 4 {
		t.Fatalf("second segment %d bytes, want 4", n)
	}
}
