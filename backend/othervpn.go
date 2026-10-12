package backend

import (
	"net"
	"os/exec"
	"runtime"
	"sort"
	"strings"
)

// Другой VPN, поднятый одновременно с PWDTT Plus (Happ/Xray, Outline,
// WireGuard и т.п.), ставит свой маршрут по умолчанию. Если его метрика
// ниже, весь трафик уходит в чужой туннель: потоки PWDTT подключены, а через
// сам туннель не проходит ни байта. Находим такие маршруты, чтобы сразу
// сказать пользователю, что мешает.

type defaultRoute struct {
	gateway string // IP шлюза или "On-link"
	iface   string // IP интерфейса
}

// parseDefaultRoutes разбирает вывод "route print -4 0.0.0.0".
func parseDefaultRoutes(out string) []defaultRoute {
	var routes []defaultRoute
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) >= 5 && f[0] == "0.0.0.0" && f[1] == "0.0.0.0" && net.ParseIP(f[3]) != nil {
			routes = append(routes, defaultRoute{gateway: f[2], iface: f[3]})
		}
	}
	return routes
}

// foreignDefaultRoutes — маршруты по умолчанию, не принадлежащие ни
// физическому шлюзу, ни туннелю PWDTT Plus (ownIP).
func foreignDefaultRoutes(routes []defaultRoute, physGW, ownIP string) []defaultRoute {
	var out []defaultRoute
	for _, r := range routes {
		if r.iface == ownIP || (physGW != "" && r.gateway == physGW) {
			continue
		}
		out = append(out, r)
	}
	return out
}

// interfaceNameByIP — имя сетевого интерфейса с этим адресом.
func interfaceNameByIP(ip string) string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, ifc := range ifaces {
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok && ipnet.IP.String() == ip {
				return ifc.Name
			}
		}
	}
	return ""
}

// OtherVPNs возвращает имена интерфейсов чужих VPN с маршрутом по умолчанию.
// ownCIDR — адрес туннеля PWDTT Plus из конфига ("10.66.66.4/32").
func OtherVPNs(physGW, ownCIDR string) []string {
	if runtime.GOOS != "windows" {
		return nil
	}
	cmd := exec.Command("route", "print", "-4", "0.0.0.0")
	hideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	ownIP := ownCIDR
	if ip, _, err := net.ParseCIDR(ownCIDR); err == nil {
		ownIP = ip.String()
	}
	seen := map[string]bool{}
	var names []string
	for _, r := range foreignDefaultRoutes(parseDefaultRoutes(string(out)), physGW, ownIP) {
		name := interfaceNameByIP(r.iface)
		if name == "" {
			name = r.iface
		}
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}
