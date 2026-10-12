//go:build !windows

package backend

import "golang.zx2c4.com/wireguard/tun"

func realIfName(_ tun.Device, fallback string) string { return fallback }

func createTUN(name string, mtu int, _ wgLogFunc) (tun.Device, error) { return tun.CreateTUN(name, mtu) }
