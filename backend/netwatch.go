package backend

// Сон компьютера и смена сети. Android-приложение WDTT Plus сообщает клиенту
// о таких событиях (CONNECTION_NETWORK, DEVICE_WAKE): он сразу проверяет
// каналы и заново ищет рабочий путь, а не ждёт тайм-аутов. RWDTT делает то же
// самое на Windows, а при смене шлюза ещё и прокладывает маршруты мимо
// туннеля через новый шлюз — старые вели бы в никуда.

import (
	"net"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"time"
)

// networkNotifier — движок, которому можно сообщить о сне и смене сети.
type networkNotifier interface {
	NetworkChanged(resumed bool)
}

const netWatchInterval = 10 * time.Second

// watchNetwork работает, пока жива сессия sessID.
func (b *Bridge) watchNetwork(sessID uint64, logf func(string)) {
	if runtime.GOOS != "windows" {
		return
	}
	alive := func() bool {
		b.mu.Lock()
		defer b.mu.Unlock()
		return b.session == sessID && b.running
	}
	_, own := wg.TunnelAddrs()
	fp := networkFingerprint(own)
	last := time.Now().Round(0) // настенное время: монотонные часы Windows не идут во сне
	t := time.NewTicker(netWatchInterval)
	defer t.Stop()
	for range t.C {
		if !alive() {
			return
		}
		now := time.Now().Round(0)
		resumed := now.Sub(last) > 3*netWatchInterval
		last = now
		cur := networkFingerprint(own)
		changed := cur != "" && fp != "" && cur != fp
		if cur != "" {
			fp = cur
		}
		if !resumed && !changed {
			continue
		}

		if changed {
			logf("Сеть изменилась — прокладываю маршруты заново")
			b.mu.Lock()
			conf := b.wgConf
			b.mu.Unlock()
			if conf != "" && alive() {
				if err := wg.Apply(conf, wg.TurnExtraIPs(), logf); err != nil {
					logf("Не удалось перестроить туннель: " + err.Error())
				}
			}
		} else {
			logf("Компьютер вышел из сна — проверяю каналы")
		}
		b.mu.Lock()
		eng := b.core
		b.mu.Unlock()
		if n, ok := eng.(networkNotifier); ok && alive() {
			n.NetworkChanged(resumed)
		}
	}
}

// networkFingerprint — IPv4-адреса физических интерфейсов и шлюзы по
// умолчанию. Интерфейс туннеля (с адресом ownCIDR) не учитывается.
func networkFingerprint(ownCIDR string) string {
	ownIP := ownCIDR
	if ip, _, err := net.ParseCIDR(ownCIDR); err == nil {
		ownIP = ip.String()
	}
	var parts []string
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		var v4 []string
		tunnel := false
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok || ipn.IP.To4() == nil || ipn.IP.IsLinkLocalUnicast() {
				continue
			}
			if ipn.IP.String() == ownIP {
				tunnel = true
			}
			v4 = append(v4, ipn.IP.String())
		}
		if !tunnel {
			parts = append(parts, v4...)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	cmd := exec.Command("route", "print", "-4", "0.0.0.0")
	hideWindow(cmd)
	if out, err := cmd.Output(); err == nil {
		parts = append(parts, defaultGateways(string(out))...)
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

// defaultGateways — шлюзы маршрутов 0.0.0.0/0 из вывода route print
// (маршруты туннеля "On-link" не в счёт).
func defaultGateways(routePrint string) []string {
	var out []string
	seen := map[string]bool{}
	for _, line := range strings.Split(routePrint, "\n") {
		f := strings.Fields(line)
		if len(f) >= 3 && f[0] == "0.0.0.0" && f[1] == "0.0.0.0" && net.ParseIP(f[2]) != nil && !seen[f[2]] {
			seen[f[2]] = true
			out = append(out, "gw="+f[2])
		}
	}
	return out
}
