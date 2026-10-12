//go:build !windows

package backend

import "golang.zx2c4.com/wireguard/tun"

func realIfName(_ tun.Device, fallback string) string { return fallback }
