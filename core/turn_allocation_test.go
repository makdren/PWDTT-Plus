package core

import (
	"encoding/binary"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// stunRefresh собирает Refresh (0x0004) или Refresh success (0x0104) с LIFETIME.
func stunRefresh(kind uint16, id [12]byte, lifetime uint32) []byte {
	msg := make([]byte, 20+8)
	binary.BigEndian.PutUint16(msg[0:2], kind)
	binary.BigEndian.PutUint16(msg[2:4], 8)
	binary.BigEndian.PutUint32(msg[4:8], 0x2112a442)
	copy(msg[8:20], id[:])
	binary.BigEndian.PutUint16(msg[20:22], 0x000d)
	binary.BigEndian.PutUint16(msg[22:24], 4)
	binary.BigEndian.PutUint32(msg[24:28], lifetime)
	return msg
}

func TestDeletionTransaction(t *testing.T) {
	id := [12]byte{1, 2, 3}
	if got, ok := deletionTransaction(stunRefresh(0x0004, id, 0), 0x0004); !ok || got != id {
		t.Fatal("zero-lifetime Refresh must be recognised")
	}
	if _, ok := deletionTransaction(stunRefresh(0x0004, id, 600), 0x0004); ok {
		t.Fatal("normal Refresh is not a deletion")
	}
	if _, ok := deletionTransaction([]byte("wireguard data packet......"), 0x0004); ok {
		t.Fatal("non-STUN data")
	}
}

// fakeTURN отвечает на удаление сессии через задержку.
type fakeTURN struct {
	mu      sync.Mutex
	replies chan []byte
	closed  bool
	delay   time.Duration
}

func (f *fakeTURN) WriteTo(p []byte, _ net.Addr) (int, error) {
	if id, ok := deletionTransaction(p, 0x0004); ok {
		reply := stunRefresh(0x0104, id, 0)
		go func() { time.Sleep(f.delay); f.replies <- reply }()
	}
	return len(p), nil
}
func (f *fakeTURN) ReadFrom(p []byte) (int, net.Addr, error) {
	b, ok := <-f.replies
	if !ok {
		return 0, nil, errors.New("closed")
	}
	return copy(p, b), nil, nil
}
func (f *fakeTURN) Close() error                     { return nil }
func (f *fakeTURN) LocalAddr() net.Addr              { return nil }
func (f *fakeTURN) SetDeadline(time.Time) error      { return nil }
func (f *fakeTURN) SetReadDeadline(time.Time) error  { return nil }
func (f *fakeTURN) SetWriteDeadline(time.Time) error { return nil }

// fakeRelay при закрытии шлёт удаление через транспорт, как pion.
type fakeRelay struct {
	fakeTURN
	transport net.PacketConn
}

func (r *fakeRelay) Close() error {
	_, err := r.transport.WriteTo(stunRefresh(0x0004, [12]byte{9}, 0), nil)
	return err
}

func TestCloseRelayWaitsForDeletionAck(t *testing.T) {
	turnSrv := &fakeTURN{replies: make(chan []byte, 1), delay: 100 * time.Millisecond}
	deletion := newTurnDeletionTransport(turnSrv)
	go func() { // читатель pion
		buf := make([]byte, 64)
		for {
			if _, _, err := deletion.ReadFrom(buf); err != nil {
				return
			}
		}
	}()
	relay := &fakeRelay{transport: deletion}
	start := time.Now()
	closeRelay(relay, deletion)
	took := time.Since(start)
	if took < 90*time.Millisecond || took > deletionWait {
		t.Fatalf("must wait for the ack (~100ms), waited %v", took)
	}
	close(turnSrv.replies)
}

func TestCloseRelayGivesUpAfterTimeout(t *testing.T) {
	turnSrv := &fakeTURN{replies: make(chan []byte, 1), delay: time.Hour}
	deletion := newTurnDeletionTransport(turnSrv)
	start := time.Now()
	closeRelay(&fakeRelay{transport: deletion}, deletion)
	if took := time.Since(start); took < deletionWait || took > deletionWait+300*time.Millisecond {
		t.Fatalf("waited %v", took)
	}
}

// GETCONF без поколения транспорта: несколько устройств с одним паролем
// не вышибают потоки друг друга на сервере WDTT Plus.
func TestMuxGetconfHasNoTransportSession(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	got := make(chan string, 1)
	go func() {
		buf := make([]byte, 4096)
		n, _ := server.Read(buf)
		got <- string(buf[:n])
		_, _ = server.Write([]byte("# WDTT_TRANSPORTS=uplink2\n[Interface]\n"))
	}()
	conf, err := requestMuxConfig(client, "9000", "dev-1", "secret", 7)
	if err != nil || !strings.Contains(conf, "[Interface]") {
		t.Fatalf("conf=%q err=%v", conf, err)
	}
	parts := strings.Split(strings.TrimPrefix(<-got, "GETCONF:"), "|")
	if len(parts) != 4 || parts[0] != "9000" || parts[1] != "dev-1" || parts[2] != "secret" || !strings.HasPrefix(parts[3], "{") {
		t.Fatalf("payload parts: %q", parts)
	}
}
