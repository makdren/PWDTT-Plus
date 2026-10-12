package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Многопоточный режим сервера WDTT-Plus (WDTT_MUX1).
//
// В старом режиме каждый DTLS-воркер на сервере открывает свой UDP-сокет к
// WireGuard. У WireGuard-пира один «роумингующий» адрес, поэтому весь входящий
// трафик уходит через воркер, писавший последним, — скорость скачивания
// упирается в один TURN-релей. Сервер WDTT-Plus умеет держать один сокет на
// устройство и раздавать входящие пакеты по всем воркерам по кругу; для этого
// каждый воркер авторизует устройство через GETCONF и шлёт WDTT_MUX1 перед
// первым пакетом WireGuard. Признак поддержки — строка "# WDTT_TRANSPORTS=" в
// ответе на GETCONF; старым серверам шлём прежний AUTH.

const (
	muxHello           = "WDTT_MUX1"
	serverModeUnknown  = int32(0)
	serverModeLegacy   = int32(1)
	serverModeMux      = int32(2)
	serverModeWaitTime = 20 * time.Second
)

var (
	serverModeMu    sync.Mutex
	serverModeValue atomic.Int32
	serverModeReady chan struct{}

)

func init() { resetServerMode() }

// resetServerMode вызывается при каждом старте ядра.
func resetServerMode() {
	serverModeMu.Lock()
	defer serverModeMu.Unlock()
	serverModeValue.Store(serverModeUnknown)
	serverModeReady = make(chan struct{})
}

// noteServerConfig определяет режим сервера по ответу на GETCONF.
func noteServerConfig(conf string) int32 {
	mode := serverModeLegacy
	if strings.Contains(conf, "# WDTT_TRANSPORTS=") {
		mode = serverModeMux
	}
	serverModeMu.Lock()
	defer serverModeMu.Unlock()
	if serverModeValue.CompareAndSwap(serverModeUnknown, mode) {
		close(serverModeReady)
	}
	return serverModeValue.Load()
}

func currentServerMode() int32 { return serverModeValue.Load() }

// waitServerMode ждёт, пока первый воркер получит конфиг и режим станет известен.
func waitServerMode(ctx context.Context, timeout time.Duration) int32 {
	serverModeMu.Lock()
	ready := serverModeReady
	serverModeMu.Unlock()
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case <-ready:
	case <-ctx.Done():
	case <-t.C:
	}
	return currentServerMode()
}

// muxDeviceInfo — описание устройства для панели владельца сервера WDTT-Plus.
func muxDeviceInfo() string {
	info := map[string]string{
		"name":        "PWDTT Plus (" + runtime.GOOS + ")",
		"app_version": "pwdtt-plus",
		"abi":         runtime.GOARCH,
	}
	data, _ := json.Marshal(info)
	return strings.ReplaceAll(string(data), "|", " ")
}

// requestMuxConfig — GETCONF с описанием устройства для панели владельца.
//
// Поколение транспорта и номер воркера (как у Android-клиента) намеренно не
// отправляются: сервер WDTT Plus хранит одно поколение на пароль, и новое
// поколение закрывает все потоки прежнего. Если тем же паролем одновременно
// пользуются два устройства (два ПК, ПК и телефон), они бесконечно вышибали
// бы потоки друг друга. Без поколения сервер просто добавляет поток к
// текущему, а многопоточный режим работает по deviceID.
func requestMuxConfig(conn net.Conn, localPort, deviceID, password string, _ int) (string, error) {
	payload := fmt.Sprintf("GETCONF:%s|%s|%s|%s", localPort, deviceID, password, muxDeviceInfo())
	return requestConfigPayload(conn, payload)
}

// startMux переводит сессию в многопоточный режим.
func startMux(conn net.Conn) error {
	_, err := conn.Write([]byte(muxHello))
	return err
}
