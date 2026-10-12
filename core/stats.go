package core

import (
	"sync/atomic"
	"time"
)

type Stats struct {
	TotalBytesUp      atomic.Int64
	TotalBytesDown    atomic.Int64
	ActiveConnections atomic.Int32
}

func NewStats() *Stats {
	return &Stats{}
}

// RunLoop отправляет статистику каждые 3 секунды.
// onLog вызывается для лог-сообщений, onStats — для статистики.
func (s *Stats) RunLoop(shutdown <-chan struct{},
	onLog func(level, msg string),
	onStats func(rx, tx int64, workers int32)) {

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	logTick := 0

	for {
		select {
		case <-shutdown:
			return
		case <-ticker.C:
			active := s.ActiveConnections.Load()
			up := s.TotalBytesUp.Load()
			down := s.TotalBytesDown.Load()
			totalMB := float64(up+down) / (1024.0 * 1024.0)

			// Строка в лог — раз в 30 с (раньше каждые 3 с забивала файл
			// лога); счётчики в окно по-прежнему уходят каждые 3 с.
			logTick++
			if onLog != nil && logTick%10 == 1 {
				onLog("INFO", "[STATS] Активных: "+itoa(int(active))+", Трафик: "+ftoa(totalMB)+" МБ")
			}
			if onStats != nil {
				onStats(up, down, active)
			}
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}

func ftoa(f float64) string {
	// Две цифры после точки с ведущим нулём: раньше 1.05 печаталось как "1.5".
	if f < 0 {
		f = 0
	}
	hundredths := int(f*100 + 0.5)
	frac := hundredths % 100
	pad := ""
	if frac < 10 {
		pad = "0"
	}
	return itoa(hundredths/100) + "." + pad + itoa(frac)
}
