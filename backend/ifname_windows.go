//go:build windows

package backend

import (
	"net"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wireguard/tun"
)

var procConvertLuidToIndex = windows.NewLazySystemDLL("iphlpapi.dll").NewProc("ConvertInterfaceLuidToIndex")

// realIfName — настоящее имя адаптера Wintun в Windows (по LUID). tun.Name()
// возвращает лишь запрошенное имя, а Windows может назвать адаптер иначе.
func realIfName(dev tun.Device, fallback string) string {
	nt, ok := dev.(*tun.NativeTun)
	if !ok {
		return fallback
	}
	luid := nt.LUID()
	if luid == 0 {
		return fallback
	}
	var index uint32
	if r, _, _ := procConvertLuidToIndex.Call(uintptr(unsafe.Pointer(&luid)), uintptr(unsafe.Pointer(&index))); r != 0 || index == 0 {
		return fallback
	}
	if ifc, err := net.InterfaceByIndex(int(index)); err == nil && ifc.Name != "" {
		return ifc.Name
	}
	return fallback
}
