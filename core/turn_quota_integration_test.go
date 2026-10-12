package core

import (
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pion/turn/v5"
)

// Настоящий TURN-сервер pion с квотой «одна сессия на учётные данные» (как
// 486 у VK). Сервер теряет первый запрос на удаление сессии — так бывает с
// одиночным UDP-пакетом. Проверяем, что после закрытия сессии новая
// сессия с теми же учётными данными создаётся.

type dropFirstDeletion struct {
	net.PacketConn
	dropped atomic.Bool
}

func (c *dropFirstDeletion) ReadFrom(p []byte) (int, net.Addr, error) {
	for {
		n, addr, err := c.PacketConn.ReadFrom(p)
		if err == nil {
			if _, ok := deletionTransaction(p[:n], 0x0004); ok && c.dropped.CompareAndSwap(false, true) {
				continue // первый запрос на удаление «потерялся»
			}
		}
		return n, addr, err
	}
}

func startQuotaTURN(t *testing.T) (addr string, active func() int32) {
	t.Helper()
	raw, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var count atomic.Int32
	var mu sync.Mutex
	srv, err := turn.NewServer(turn.ServerConfig{
		Realm: "vk",
		AuthHandler: func(ra *turn.RequestAttributes) (string, []byte, bool) {
			return ra.Username, turn.GenerateAuthKey(ra.Username, ra.Realm, "pass"), true
		},
		QuotaHandler: func(_, _ string, _ net.Addr) bool {
			mu.Lock()
			defer mu.Unlock()
			return count.Load() < 1
		},
		EventHandler: turn.EventHandler{
			OnAllocationCreated: func(_, _ net.Addr, _, _, _ string, _ net.Addr, _ int) { count.Add(1) },
			OnAllocationDeleted: func(_, _ net.Addr, _, _, _ string) { count.Add(-1) },
		},
		PacketConnConfigs: []turn.PacketConnConfig{{
			PacketConn: &dropFirstDeletion{PacketConn: raw},
			RelayAddressGenerator: &turn.RelayAddressGeneratorStatic{
				RelayAddress: net.ParseIP("127.0.0.1"), Address: "127.0.0.1",
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	return raw.LocalAddr().String(), count.Load
}

func allocate(t *testing.T, addr string) (*turn.Client, net.PacketConn, *turnDeletionTransport, net.PacketConn, error) {
	t.Helper()
	conn, err := dialTURNConn(addr, TransportUDP, "127.0.0.1", "", false)
	if err != nil {
		t.Fatal(err)
	}
	deletion := newTurnDeletionTransport(conn)
	tc, err := turn.NewClient(&turn.ClientConfig{
		STUNServerAddr: addr, TURNServerAddr: addr, Conn: deletion,
		Username: "user", Password: "pass", LoggerFactory: &NullLoggerFactory{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := tc.Listen(); err != nil {
		t.Fatal(err)
	}
	relay, err := tc.Allocate()
	return tc, relay, deletion, conn, err
}

func TestTurnQuotaFreedAfterClose(t *testing.T) {
	addr, active := startQuotaTURN(t)

	tc, relay, deletion, conn, err := allocate(t, addr)
	if err != nil {
		t.Fatal(err)
	}
	// Порядок, как в RunSession: удаление → закрытие клиента → сокета.
	closeRelay(relay, deletion)
	tc.Close()
	_ = conn.Close()

	deadline := time.Now().Add(2 * time.Second)
	for active() != 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if n := active(); n != 0 {
		t.Fatalf("сессия осталась занятой на сервере (активных: %d)", n)
	}

	tc2, relay2, deletion2, conn2, err := allocate(t, addr)
	if err != nil {
		if strings.Contains(err.Error(), "486") {
			t.Fatalf("квота не освободилась: %v", err)
		}
		t.Fatal(err)
	}
	closeRelay(relay2, deletion2)
	tc2.Close()
	_ = conn2.Close()
}
