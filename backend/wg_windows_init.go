//go:build windows

package backend

import (
	"fmt"
	"sync"
	"time"

	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wireguard/tun"
)

// Фиксированный GUID, чтобы Windows не плодила новые сетевые профили.
var staticTunGUID, _ = windows.GUIDFromString("{59B518DA-F85C-4DF7-BEE7-4B29E3063C43}")

var (
	tunGUIDMu  sync.Mutex
	tunGUIDBad bool // адаптер с фиксированным GUID застрял — до конца работы берём случайные
)

// createTUN создаёт адаптер Wintun. Если прошлый адаптер с тем же GUID
// ещё не удалён Windows (быстрое переподключение, аварийное завершение),
// создание падает с «The system cannot find the file specified» —
// тогда повторяем с новым случайным GUID.
func createTUN(name string, mtu int, logf wgLogFunc) (tun.Device, error) {
	tunGUIDMu.Lock()
	useStatic := !tunGUIDBad
	tunGUIDMu.Unlock()

	if useStatic {
		guid := staticTunGUID
		dev, err := tun.CreateTUNWithRequestedGUID(name, &guid, mtu)
		if err == nil {
			return dev, nil
		}
		logf(fmt.Sprintf("Адаптер не создан (%v), пробую с новым GUID", err))
		tunGUIDMu.Lock()
		tunGUIDBad = true
		tunGUIDMu.Unlock()
		time.Sleep(time.Second)
	}

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		guid, err := windows.GenerateGUID()
		if err != nil {
			return nil, err
		}
		dev, err := tun.CreateTUNWithRequestedGUID(name, &guid, mtu)
		if err == nil {
			return dev, nil
		}
		lastErr = err
		logf(fmt.Sprintf("Адаптер не создан (попытка %d): %v", attempt+2, err))
		time.Sleep(2 * time.Second)
	}
	return nil, lastErr
}
