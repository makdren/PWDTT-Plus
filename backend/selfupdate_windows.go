//go:build windows

package backend

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

const afterUpdateFlag = "--after-update="

// replaceAndRestart: запущенный exe нельзя перезаписать, но можно
// переименовать. Старый → .old, новый → на его место, запускаем новый.
// При любой ошибке возвращаем всё как было.
func replaceAndRestart(exe, downloaded string) error {
	old := exe + ".old"
	_ = os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		return fmt.Errorf("не удалось заменить программу: %w", err)
	}
	if err := os.Rename(downloaded, exe); err != nil {
		_ = os.Rename(old, exe)
		return fmt.Errorf("не удалось установить новую версию: %w", err)
	}
	cmd := exec.Command(exe, afterUpdateFlag+strconv.Itoa(os.Getpid()))
	if err := cmd.Start(); err != nil {
		_ = os.Remove(exe)
		_ = os.Rename(old, exe)
		return fmt.Errorf("не удалось запустить новую версию: %w", err)
	}
	_ = cmd.Process.Release()
	return nil
}

// WaitAfterUpdate вызывается в main до wails.Run: новая версия ждёт, пока
// старая освободит блокировку единственного экземпляра, и чистит .old.
func WaitAfterUpdate(args []string) {
	for _, a := range args {
		v, ok := strings.CutPrefix(a, afterUpdateFlag)
		if !ok {
			continue
		}
		if pid, err := strconv.Atoi(v); err == nil && pid > 0 {
			if h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid)); err == nil {
				_, _ = windows.WaitForSingleObject(h, 20000)
				_ = windows.CloseHandle(h)
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	go CleanupAfterUpdate()
}
