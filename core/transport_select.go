package core

import (
	"context"
	"log"
	"sort"
	"strings"
	"sync"
	"time"
)

// Выбор транспорта до TURN-серверов VK: UDP, TCP или автоматически.
//
// В режиме "auto" первые сессии пробуют UDP и TCP поровну. Для каждого
// транспорта копится успешность и время установки сессии (TURN Allocate +
// DTLS-рукопожатие до VPS). Победитель — транспорт, который работает, а при
// равной успешности — с меньшим временем установки; UDP получает фору, потому
// что поверх TCP поток туннеля страдает от блокировки очереди. Сессии на
// проигравшем транспорте переподключаются. Если выбранный транспорт подряд
// много раз не поднимается, выбор пересматривается.

const (
	TransportAuto = "auto"
	TransportUDP  = "udp"
	TransportTCP  = "tcp"

	selectMinSamples      = 2                // успешных установок на транспорт для сравнения
	selectProbeTimeout    = 25 * time.Second // дольше не пробуем — решаем по тому, что есть
	selectFailoverStreak  = 6                // подряд неудач выбранного транспорта для смены
	selectUDPPreference   = 1.3              // TCP должен быть заметно быстрее, чтобы победить
	selectRecheckInterval = 10 * time.Minute // повторная проба после смены сети и т.п.
)

func NormalizeTransportMode(mode string, legacyTCP bool) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case TransportUDP:
		return TransportUDP
	case TransportTCP:
		return TransportTCP
	case TransportAuto:
		return TransportAuto
	}
	if legacyTCP {
		return TransportTCP
	}
	return TransportAuto
}

type transportStat struct {
	ok      int
	fail    int
	streak  int // неудачи подряд
	samples []time.Duration
}

func (s *transportStat) median() time.Duration {
	if len(s.samples) == 0 {
		return 0
	}
	c := append([]time.Duration(nil), s.samples...)
	sort.Slice(c, func(i, j int) bool { return c[i] < c[j] })
	return c[len(c)/2]
}

type transportSelector struct {
	mode    string
	mu      sync.Mutex
	chosen  string
	started time.Time
	decided time.Time
	stats   map[string]*transportStat
	changed chan struct{} // закрывается при каждой смене выбора
}

func newTransportSelector(mode string) *transportSelector {
	s := &transportSelector{
		mode:    NormalizeTransportMode(mode, false),
		started: time.Now(),
		stats:   map[string]*transportStat{TransportUDP: {}, TransportTCP: {}},
		changed: make(chan struct{}),
	}
	if s.mode != TransportAuto {
		s.chosen = s.mode
	}
	return s
}

// Pick — транспорт для новой сессии воркера.
func (s *transportSelector) Pick(workerID int) string {
	if s == nil {
		return TransportUDP
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.mode != TransportAuto {
		return s.mode
	}
	if s.chosen != "" && time.Since(s.decided) > selectRecheckInterval {
		// Сеть могла смениться — заново сравниваем, не трогая живые сессии.
		s.restartProbeLocked()
	}
	if s.chosen != "" {
		return s.chosen
	}
	s.maybeDecideLocked()
	if s.chosen != "" {
		return s.chosen
	}
	if workerID%2 == 0 {
		return TransportTCP
	}
	return TransportUDP
}

// Current — текущий выбор ("" пока идёт проба).
func (s *transportSelector) Current() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.chosen
}

// Probing — идёт сравнение транспортов: сбой адреса может быть сбоем
// транспорта, поэтому адрес TURN не баним.
func (s *transportSelector) Probing() bool {
	if s == nil || s.mode != TransportAuto {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.chosen == ""
}

// Report — итог установки сессии на транспорте.
func (s *transportSelector) Report(transport string, ok bool, setup time.Duration) {
	if s == nil || s.mode != TransportAuto {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.stats[transport]
	if st == nil {
		return
	}
	if ok {
		st.ok++
		st.streak = 0
		st.samples = append(st.samples, setup)
		if len(st.samples) > 16 {
			st.samples = st.samples[len(st.samples)-16:]
		}
	} else {
		st.fail++
		st.streak++
	}

	if s.chosen == "" {
		s.maybeDecideLocked()
		return
	}
	if transport == s.chosen && st.streak >= selectFailoverStreak {
		other := otherTransport(transport)
		if ost := s.stats[other]; ost.streak < selectFailoverStreak {
			log.Printf("[ТРАНСПОРТ] %s не поднимается %d раз подряд — переключаюсь на %s",
				strings.ToUpper(transport), st.streak, strings.ToUpper(other))
			st.streak = 0
			s.setChosenLocked(other)
		}
	}
}

func (s *transportSelector) restartProbeLocked() {
	s.chosen = ""
	s.started = time.Now()
	s.stats = map[string]*transportStat{TransportUDP: {}, TransportTCP: {}}
}

func (s *transportSelector) maybeDecideLocked() {
	u, t := s.stats[TransportUDP], s.stats[TransportTCP]
	timedOut := time.Since(s.started) > selectProbeTimeout
	enough := u.ok >= selectMinSamples && t.ok >= selectMinSamples
	udpDead := u.ok == 0 && u.fail >= 3
	tcpDead := t.ok == 0 && t.fail >= 3

	switch {
	case udpDead && t.ok > 0:
		s.decideLocked(TransportTCP, "UDP не проходит")
	case tcpDead && u.ok > 0:
		s.decideLocked(TransportUDP, "TCP не проходит")
	case enough || (timedOut && (u.ok > 0 || t.ok > 0)):
		if u.ok == 0 {
			s.decideLocked(TransportTCP, "UDP не дал ни одной сессии")
			return
		}
		if t.ok == 0 {
			s.decideLocked(TransportUDP, "TCP не дал ни одной сессии")
			return
		}
		um, tm := u.median(), t.median()
		uRate := float64(u.ok) / float64(u.ok+u.fail)
		tRate := float64(t.ok) / float64(t.ok+t.fail)
		switch {
		case tRate > uRate+0.25:
			s.decideLocked(TransportTCP, "TCP стабильнее")
		case uRate > tRate+0.25:
			s.decideLocked(TransportUDP, "UDP стабильнее")
		case float64(tm)*selectUDPPreference < float64(um):
			s.decideLocked(TransportTCP, "TCP быстрее: "+tm.Round(time.Millisecond).String()+" против "+um.Round(time.Millisecond).String())
		default:
			s.decideLocked(TransportUDP, "UDP: "+um.Round(time.Millisecond).String()+", TCP: "+tm.Round(time.Millisecond).String())
		}
	}
}

func (s *transportSelector) decideLocked(transport, why string) {
	log.Printf("[ТРАНСПОРТ] Автовыбор: %s (%s)", strings.ToUpper(transport), why)
	s.setChosenLocked(transport)
}

func (s *transportSelector) setChosenLocked(transport string) {
	s.chosen = transport
	s.decided = time.Now()
	close(s.changed)
	s.changed = make(chan struct{})
}

// Watch завершает сессию, если выбор транспорта ушёл от её транспорта.
func (s *transportSelector) Watch(ctx context.Context, transport string, cancel context.CancelFunc) {
	if s == nil || s.mode != TransportAuto {
		return
	}
	for {
		s.mu.Lock()
		ch := s.changed
		chosen := s.chosen
		s.mu.Unlock()
		if chosen != "" && chosen != transport {
			log.Printf("[ТРАНСПОРТ] Сессия на %s переподключается на %s", strings.ToUpper(transport), strings.ToUpper(chosen))
			cancel()
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ch:
		}
	}
}

func otherTransport(t string) string {
	if t == TransportTCP {
		return TransportUDP
	}
	return TransportTCP
}
