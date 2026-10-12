//go:build windows

package backend

import (
	"os"

	"golang.org/x/sys/windows"
)

// raisePriority — клиент WDTT Plus с приоритетом выше обычного: когда ПК
// нагружен (игра, сборка, браузер), пакеты туннеля не ждут своей очереди.
func raisePriority(p *os.Process) {
	if p == nil {
		return
	}
	h, err := windows.OpenProcess(windows.PROCESS_SET_INFORMATION, false, uint32(p.Pid))
	if err != nil {
		return
	}
	defer windows.CloseHandle(h)
	_ = windows.SetPriorityClass(h, windows.ABOVE_NORMAL_PRIORITY_CLASS)
}
