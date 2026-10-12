package core

import (
	"testing"
	"time"
)

func TestSelectorFixedMode(t *testing.T) {
	s := newTransportSelector("tcp")
	if s.Pick(1) != TransportTCP || s.Probing() {
		t.Fatal("fixed tcp")
	}
}

// Во время пробы почти все воркеры идут по UDP, TCP — только каждый девятый.
func TestSelectorProbeMostlyUDP(t *testing.T) {
	s := newTransportSelector("auto")
	tcp := 0
	for id := 1; id <= 18; id++ {
		if s.Pick(id) == TransportTCP {
			tcp++
		}
	}
	if tcp != 2 || !s.Probing() {
		t.Fatalf("tcp probes = %d", tcp)
	}
}

func TestSelectorUDPBlocked(t *testing.T) {
	s := newTransportSelector("auto")
	for i := 0; i < 3; i++ {
		s.Report(TransportUDP, false, 0)
	}
	s.Report(TransportTCP, true, 300*time.Millisecond)
	if s.Current() != TransportTCP {
		t.Fatalf("got %q", s.Current())
	}
}

// Сценарий из лога 12:11: TCP быстрее устанавливает соединение, но UDP
// работает — остаёмся на UDP (раньше выбор качался и рвал сессии).
func TestSelectorStaysOnWorkingUDP(t *testing.T) {
	s := newTransportSelector("auto")
	s.Report(TransportTCP, true, 236*time.Millisecond)
	s.Report(TransportTCP, true, 236*time.Millisecond)
	s.Report(TransportUDP, true, 2*time.Second)
	s.Report(TransportUDP, true, 2*time.Second)
	if s.Current() != TransportUDP {
		t.Fatalf("got %q", s.Current())
	}
}

func TestSelectorUnstableUDP(t *testing.T) {
	s := newTransportSelector("auto")
	s.Report(TransportUDP, true, time.Second)
	for i := 0; i < 5; i++ {
		s.Report(TransportUDP, false, 0)
	}
	s.Report(TransportTCP, true, time.Second)
	if s.Current() != TransportTCP {
		t.Fatalf("got %q", s.Current())
	}
}

func TestSelectorFailover(t *testing.T) {
	s := newTransportSelector("auto")
	s.Report(TransportUDP, true, 300*time.Millisecond)
	s.Report(TransportUDP, true, 300*time.Millisecond)
	if s.Current() != TransportUDP {
		t.Fatal("expected udp")
	}
	for i := 0; i < selectFailoverStreak-1; i++ {
		s.Report(TransportUDP, false, 0)
	}
	if s.Current() != TransportUDP {
		t.Fatal("switched too early")
	}
	s.Report(TransportUDP, false, 0)
	if s.Current() != TransportTCP {
		t.Fatalf("expected failover to tcp, got %q", s.Current())
	}
}

func TestNoteServerConfig(t *testing.T) {
	resetServerMode()
	if noteServerConfig("[Interface]\n") != serverModeLegacy {
		t.Fatal("legacy")
	}
	resetServerMode()
	if noteServerConfig("# WDTT_TRANSPORTS=uplink2\n[Interface]\n") != serverModeMux {
		t.Fatal("mux")
	}
	resetServerMode()
}
