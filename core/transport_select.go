package core

import (
	"log"
	"sort"
	"strings"
	"sync"
	"time"
)

// Выбор транспорта до TURN-серверов VK: UDP, TCP или автоматически.
//
// Режим "auto" — это UDP с запасным TCP. Через TCP туннель почти всегда
// медленнее (блокировка очереди), поэтому TCP пробует лишь каждый девятый
// воркер, а основная масса идёт по UDP. Успехом считается сессия, по которой
// от VPS реально пришли данные, а не только DTLS-рукопожатие. На TCP клиент
// переходит, только если UDP не работает (сессии не поднимаются или почти
// все падают), а TCP — работает.
//
// Решение касается только НОВЫХ сессий: живые сессии никогда не рвутся из-за
// смены выбора. Иначе десятки воркеров разом идут за новыми TURN-сессиями,
// VK отвечает на такой всплеск отказами — и выбор начинает качаться.

const (
	TransportAuto = "auto"
	TransportUDP  = "udp"
	TransportTCP  = "tcp"

	selectMinSamples      = 2                // успешных UDP-сессий, чтобы закрепить UDP
	selectProbeTimeout    = 30 * time.Second // дольше не пробуем — решаем по тому, что есть
	selectFailoverStreak  = 8                // подряд неудач выбранного транспорта для смены
	selectTCPProbeEvery   = 9                // во время пробы TCP получает каждый N-й воркер
	selectRecheckInterval = 10 * time.Minute // после ухода на TCP — снова пробуем UDP
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
	if s.chosen == TransportTCP && time.Since(s.decided) > selectRecheckInterval {
		// Сеть могла смениться — снова пробуем UDP, не трогая живые сессии.
		s.restartProbeLocked()
	}
	if s.chosen != "" {
		return s.chosen
	}
	s.maybeDecideLocked()
	if s.chosen != "" {
		return s.chosen
	}
	if workerID%selectTCPProbeEvery == 0 {
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
	udpDead := u.ok == 0 && u.fail >= 3
	uRate := rate(u)

	switch {
	case udpDead && t.ok > 0:
		s.decideLocked(TransportTCP, "UDP не проходит, TCP работает")
	case u.ok >= selectMinSamples && uRate >= 0.5:
		s.decideLocked(TransportUDP, "UDP работает: "+u.median().Round(time.Millisecond).String())
	case u.ok+u.fail >= 6 && uRate < 0.5 && t.ok > 0 && rate(t) > uRate+0.25:
		s.decideLocked(TransportTCP, "UDP нестабилен, TCP надёжнее")
	case timedOut && u.ok > 0:
		s.decideLocked(TransportUDP, "UDP работает")
	case timedOut && t.ok > 0:
		s.decideLocked(TransportTCP, "UDP не дал ни одной сессии")
	}
}

func rate(st *transportStat) float64 {
	if st.ok+st.fail == 0 {
		return 0
	}
	return float64(st.ok) / float64(st.ok+st.fail)
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

func otherTransport(t string) string {
	if t == TransportTCP {
		return TransportUDP
	}
	return TransportTCP
}
