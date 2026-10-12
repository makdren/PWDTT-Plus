package core

import (
	"encoding/binary"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Удаление TURN-сессии при закрытии — как в WDTT Plus.
//
// pion при закрытии relay шлёт Refresh с lifetime=0 и не ждёт ответа
// (dontWait). Если сразу закрыть сокет, единственный UDP-пакет может
// потеряться без повтора, и сессия остаётся занятой на VK до истечения срока
// (до 10 минут). Такие «висящие» сессии съедают квоту учётных данных —
// новые получают 486 Allocation Quota Reached. Поэтому при закрытии держим
// сокет живым до deletionWait и ждём подтверждения удаления.

const deletionWait = 750 * time.Millisecond

type turnDeletionTransport struct {
	net.PacketConn
	closing     atomic.Bool
	mu          sync.Mutex
	transaction [12]byte
	pending     bool
	ack         chan struct{}
	once        sync.Once
}

func newTurnDeletionTransport(conn net.PacketConn) *turnDeletionTransport {
	return &turnDeletionTransport{PacketConn: conn, ack: make(chan struct{})}
}

// deletionTransaction распознаёт Refresh (0x0004) / Refresh success (0x0104)
// с LIFETIME=0 и возвращает ID транзакции.
func deletionTransaction(p []byte, kind uint16) ([12]byte, bool) {
	var id [12]byte
	if len(p) < 20 || binary.BigEndian.Uint16(p[:2]) != kind || binary.BigEndian.Uint32(p[4:8]) != 0x2112a442 {
		return id, false
	}
	n := 20 + int(binary.BigEndian.Uint16(p[2:4]))
	if n != len(p) {
		return id, false
	}
	for at := 20; at+4 <= n; {
		attr, size := binary.BigEndian.Uint16(p[at:at+2]), int(binary.BigEndian.Uint16(p[at+2:at+4]))
		at += 4
		if at+size > n {
			return id, false
		}
		if attr == 0x000d && size == 4 && binary.BigEndian.Uint32(p[at:at+4]) == 0 {
			copy(id[:], p[8:20])
			return id, true
		}
		at += (size + 3) &^ 3
	}
	return id, false
}

func (c *turnDeletionTransport) WriteTo(p []byte, addr net.Addr) (int, error) {
	if c.closing.Load() {
		if id, ok := deletionTransaction(p, 0x0004); ok {
			c.mu.Lock()
			c.transaction = id
			c.pending = true
			c.mu.Unlock()
		}
	}
	return c.PacketConn.WriteTo(p, addr)
}

func (c *turnDeletionTransport) ReadFrom(p []byte) (int, net.Addr, error) {
	n, addr, err := c.PacketConn.ReadFrom(p)
	if c.closing.Load() && n > 0 {
		if id, ok := deletionTransaction(p[:n], 0x0104); ok {
			c.mu.Lock()
			match := c.pending && c.transaction == id
			c.mu.Unlock()
			if match {
				c.once.Do(func() { close(c.ack) })
			}
		}
	}
	return n, addr, err
}

func (c *turnDeletionTransport) wait(timeout time.Duration) {
	c.mu.Lock()
	pending := c.pending
	c.mu.Unlock()
	if !pending {
		return
	}
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case <-c.ack:
	case <-t.C:
	}
}

// closeRelay удаляет TURN-сессию и ждёт подтверждения, пока клиент pion
// и сокет ещё открыты (их закрывают defer'ы, зарегистрированные раньше).
func closeRelay(relay net.PacketConn, deletion *turnDeletionTransport) {
	deletion.closing.Store(true)
	if err := relay.Close(); err == nil {
		deletion.wait(deletionWait)
	}
}
