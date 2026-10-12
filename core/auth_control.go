package core

import (
	"log"
	"strings"
	"sync"
)

// legacyDeviceID — ID, который до 1.7.3 отправляли все копии PWDTT.
// Пароли WDTT-Plus, впервые использованные в старых версиях, привязаны к нему.
const legacyDeviceID = "unknown"

// deviceMismatchMarker — часть текста ошибки DENIED:device_mismatch.
const deviceMismatchMarker = "привязан к другому устройству"

// authControl — общий для всех воркеров ID устройства и реакция на отказ
// сервера в доступе.
type authControl struct {
	mu          sync.Mutex
	id          string
	legacyTried bool
	fatalOnce   sync.Once
	onFatal     func(error)
}

func newAuthControl(deviceID string, onFatal func(error)) *authControl {
	if deviceID == "" {
		deviceID = legacyDeviceID
	}
	return &authControl{id: deviceID, onFatal: onFatal}
}

func (a *authControl) DeviceID() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.id
}

// handleFatal решает, что делать с фатальной ошибкой сессии, полученной с ID
// usedID. true — воркер повторяет попытку, false — подключение остановлено.
//
// Если пароль привязан к другому устройству, один раз пробуем старый общий ID:
// так продолжают работать пароли, привязанные старыми версиями PWDTT.
func (a *authControl) handleFatal(err error, usedID string) bool {
	a.mu.Lock()
	if usedID != a.id {
		// Пока сессия шла, другой воркер уже сменил ID — пробуем с новым.
		a.mu.Unlock()
		return true
	}
	if strings.Contains(err.Error(), deviceMismatchMarker) && !a.legacyTried && a.id != legacyDeviceID {
		a.legacyTried = true
		a.id = legacyDeviceID
		a.mu.Unlock()
		log.Printf("[ЯДРО] Пароль привязан к другому ID устройства — пробую ID старых версий приложения")
		return true
	}
	a.mu.Unlock()
	a.fatalOnce.Do(func() {
		if a.onFatal != nil {
			a.onFatal(err)
		}
	})
	return false
}
