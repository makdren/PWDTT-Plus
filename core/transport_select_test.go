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

func TestSelectorProbeAlternates(t *testing.T) {
	s := newTransportSelector("auto")
	if s.Pick(1) != TransportUDP || s.Pick(2) != TransportTCP || !s.Probing() {
		t.Fatal("probe should alternate")
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

func TestSelectorPrefersUDPWhenClose(t *testing.T) {
	s := newTransportSelector("auto")
	for i := 0; i < 2; i++ {
		s.Report(TransportUDP, true, 500*time.Millisecond)
		s.Report(TransportTCP, true, 450*time.Millisecond)
	}
	if s.Current() != TransportUDP {
		t.Fatalf("got %q", s.Current())
	}
}

func TestSelectorTCPMuchFaster(t *testing.T) {
	s := newTransportSelector("auto")
	for i := 0; i < 2; i++ {
		s.Report(TransportUDP, true, 2*time.Second)
		s.Report(TransportTCP, true, 400*time.Millisecond)
	}
	if s.Current() != TransportTCP {
		t.Fatalf("got %q", s.Current())
	}
}

func TestSelectorFailover(t *testing.T) {
	s := newTransportSelector("auto")
	for i := 0; i < 2; i++ {
		s.Report(TransportUDP, true, 300*time.Millisecond)
		s.Report(TransportTCP, true, 900*time.Millisecond)
	}
	if s.Current() != TransportUDP {
		t.Fatal("expected udp")
	}
	for i := 0; i < selectFailoverStreak; i++ {
		s.Report(TransportUDP, false, 0)
	}
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
