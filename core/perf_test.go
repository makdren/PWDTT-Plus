package core

import (
	"bytes"
	"net"
	"testing"
)

func TestWrapIntoRoundtripAndKeyChange(t *testing.T) {
	cfg, st := NewObfsConfig("audio"), NewObfsState()
	buf := make([]byte, 0, 2048)
	for _, pw := range []string{"one", "two", "one"} {
		key, _ := deriveWrapKey(pw)
		payload := []byte("wireguard-packet-" + pw)
		wire, err := obfsWrapPacketInto(buf, key, payload, cfg, st)
		if err != nil {
			t.Fatal(err)
		}
		dst := make([]byte, 2048)
		n, err := obfsUnwrapPacket(key, wire, dst)
		if err != nil || !bytes.Equal(dst[:n], payload) {
			t.Fatalf("roundtrip %s: %v", pw, err)
		}
		other, _ := deriveWrapKey(pw + "x")
		if _, err := obfsUnwrapPacket(other, wire, dst); err == nil {
			t.Fatal("wrong key must fail")
		}
	}
}

func TestSameUDPAddr(t *testing.T) {
	a := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}
	b := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1}
	c := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 2}
	if !sameUDPAddr(a, b) || sameUDPAddr(a, c) {
		t.Fatal("sameUDPAddr")
	}
}

func BenchmarkWrapInto(b *testing.B) {
	key, _ := deriveWrapKey("bench")
	cfg, st := NewObfsConfig("audio"), NewObfsState()
	buf := make([]byte, 0, 2048)
	p := make([]byte, 1280)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = obfsWrapPacketInto(buf, key, p, cfg, st)
	}
}
