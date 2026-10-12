package backend

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// Исключения из туннеля: сайты (домены), IP-адреса и подсети IPv4, трафик к
// которым идёт напрямую через физический шлюз, минуя VPN.
//
// Домены резолвятся в IPv4 и получают маршруты /32. Сайты меняют адреса
// (CDN), поэтому список перерезолвится каждые splitRefresh; однажды
// найденные адреса домена сохраняются до отключения, чтобы не рвать уже
// открытые соединения. Поддомены резолвятся вместе с "www.".

const (
	splitRefresh    = 2 * time.Minute
	maxExclusions   = 200
	maxRoutesPerDom = 64
)

var domainRe = regexp.MustCompile(`^(?i)([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)

// ExclusionKind — тип записи: "domain", "ip", "cidr" или "" (неверная).
type ExclusionEntry struct {
	Value string `json:"value"`
	Kind  string `json:"kind"`
}

// NormalizeExclusion приводит ввод пользователя к домену, IP или подсети.
// Принимает ссылки (https://site.ru/path), "*.site.ru", "site.ru:443".
func NormalizeExclusion(raw string) ExclusionEntry {
	v := strings.TrimSpace(raw)
	if v == "" || strings.HasPrefix(v, "#") {
		return ExclusionEntry{}
	}
	if strings.Contains(v, "://") {
		if u, err := url.Parse(v); err == nil && u.Host != "" {
			v = u.Host
		}
	}
	// "site.ru/path" → site.ru; подсеть "10.0.0.0/8" не трогаем
	if i := strings.IndexAny(v, "/?#"); i >= 0 {
		if _, _, err := net.ParseCIDR(v); err != nil {
			v = v[:i]
		}
	}
	v = strings.TrimPrefix(v, "*.")
	v = strings.TrimSuffix(strings.ToLower(v), ".")

	if _, n, err := net.ParseCIDR(v); err == nil {
		if n.IP.To4() == nil {
			return ExclusionEntry{}
		}
		return ExclusionEntry{Value: n.String(), Kind: "cidr"}
	}
	if host, _, err := net.SplitHostPort(v); err == nil {
		v = host
	}
	if ip := net.ParseIP(v); ip != nil {
		if ip.To4() == nil {
			return ExclusionEntry{}
		}
		return ExclusionEntry{Value: ip.To4().String(), Kind: "ip"}
	}
	if domainRe.MatchString(v) {
		return ExclusionEntry{Value: v, Kind: "domain"}
	}
	return ExclusionEntry{}
}

// NormalizeExclusions чистит список: без дублей, неверных записей и с лимитом.
func NormalizeExclusions(list []string) []ExclusionEntry {
	seen := map[string]bool{}
	var out []ExclusionEntry
	for _, raw := range list {
		e := NormalizeExclusion(raw)
		if e.Kind == "" || seen[e.Value] {
			continue
		}
		seen[e.Value] = true
		out = append(out, e)
		if len(out) >= maxExclusions {
			break
		}
	}
	return out
}

// splitTunnel держит маршруты-исключения, пока туннель поднят.
type splitTunnel struct {
	mu      sync.Mutex
	gw      string
	entries []ExclusionEntry
	routes  map[string]string          // cidr → запись-источник
	sticky  map[string]map[string]bool // домен → когда-либо найденные IP
	logf    wgLogFunc
	ctx     context.Context
	cancel  context.CancelFunc
	kick    chan struct{}
}

var split struct {
	mu      sync.Mutex
	entries []ExclusionEntry
	active  *splitTunnel
}

// SetExclusions задаёт список; если туннель поднят — применяется сразу.
func SetExclusions(list []string) []ExclusionEntry {
	entries := NormalizeExclusions(list)
	split.mu.Lock()
	split.entries = entries
	active := split.active
	split.mu.Unlock()
	if active != nil {
		active.mu.Lock()
		active.entries = entries
		active.mu.Unlock()
		select {
		case active.kick <- struct{}{}:
		default:
		}
	}
	return entries
}

// startSplitTunnel запускается после подъёма туннеля (gw — физический шлюз).
func startSplitTunnel(gw string, logf wgLogFunc) {
	stopSplitTunnel()
	if runtime.GOOS != "windows" {
		return
	}
	split.mu.Lock()
	defer split.mu.Unlock()
	if gw == "" {
		if len(split.entries) > 0 {
			logf("Исключения: физический шлюз не найден — сайты пойдут через туннель")
		}
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	st := &splitTunnel{
		gw:      gw,
		entries: split.entries,
		routes:  map[string]string{},
		sticky:  map[string]map[string]bool{},
		logf:    logf,
		ctx:     ctx,
		cancel:  cancel,
		kick:    make(chan struct{}, 1),
	}
	split.active = st
	go st.run()
}

// stopSplitTunnel снимает все маршруты-исключения (вызывается при teardown).
func stopSplitTunnel() {
	split.mu.Lock()
	st := split.active
	split.active = nil
	split.mu.Unlock()
	if st == nil {
		return
	}
	st.cancel()
	st.mu.Lock()
	defer st.mu.Unlock()
	for cidr := range st.routes {
		deleteExcludeRoute(cidr, st.gw)
	}
	st.routes = map[string]string{}
}

func (st *splitTunnel) run() {
	t := time.NewTicker(splitRefresh)
	defer t.Stop()
	for {
		st.refresh()
		select {
		case <-st.ctx.Done():
			return
		case <-t.C:
		case <-st.kick:
		}
	}
}

func (st *splitTunnel) refresh() {
	st.mu.Lock()
	entries := append([]ExclusionEntry(nil), st.entries...)
	st.mu.Unlock()

	// Резолв без блокировки: DNS может отвечать долго.
	resolved := map[string][]string{}
	for _, e := range entries {
		if e.Kind != "domain" {
			continue
		}
		resolved[e.Value] = resolveIPv4(st.ctx, e.Value)
	}
	if st.ctx.Err() != nil {
		return
	}

	st.mu.Lock()
	defer st.mu.Unlock()
	if st.ctx.Err() != nil {
		return
	}
	desired := map[string]string{}
	for _, e := range entries {
		switch e.Kind {
		case "ip":
			desired[e.Value+"/32"] = e.Value
		case "cidr":
			desired[e.Value] = e.Value
		case "domain":
			set := st.sticky[e.Value]
			if set == nil {
				set = map[string]bool{}
				st.sticky[e.Value] = set
			}
			for _, ip := range resolved[e.Value] {
				if len(set) < maxRoutesPerDom {
					set[ip] = true
				}
			}
			for ip := range set {
				desired[ip+"/32"] = e.Value
			}
		}
	}
	for domain := range st.sticky {
		if !hasDomain(entries, domain) {
			delete(st.sticky, domain)
		}
	}

	added, removed := 0, 0
	for cidr := range st.routes {
		if _, keep := desired[cidr]; !keep {
			deleteExcludeRoute(cidr, st.gw)
			delete(st.routes, cidr)
			removed++
		}
	}
	keys := make([]string, 0, len(desired))
	for cidr := range desired {
		keys = append(keys, cidr)
	}
	sort.Strings(keys)
	for _, cidr := range keys {
		if _, ok := st.routes[cidr]; ok {
			continue
		}
		if err := addExcludeRoute(cidr, st.gw); err != nil {
			st.logf(fmt.Sprintf("Исключение %s (%s) не применено: %v", cidr, desired[cidr], err))
			continue
		}
		st.routes[cidr] = desired[cidr]
		added++
	}
	if added > 0 || removed > 0 {
		st.logf(fmt.Sprintf("Исключения из туннеля: +%d / -%d маршрутов (всего %d)", added, removed, len(st.routes)))
	}
}

func hasDomain(entries []ExclusionEntry, domain string) bool {
	for _, e := range entries {
		if e.Kind == "domain" && e.Value == domain {
			return true
		}
	}
	return false
}

func resolveIPv4(ctx context.Context, domain string) []string {
	var out []string
	seen := map[string]bool{}
	for _, host := range []string{domain, "www." + domain} {
		rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		ips, err := net.DefaultResolver.LookupIP(rctx, "ip4", host)
		cancel()
		if err != nil {
			continue
		}
		for _, ip := range ips {
			s := ip.To4().String()
			if ip.To4() != nil && !seen[s] && !ip.IsLoopback() && !ip.IsUnspecified() {
				seen[s] = true
				out = append(out, s)
			}
		}
	}
	return out
}

func addExcludeRoute(cidr, gw string) error {
	ip, mask, err := parseCIDR(cidr)
	if err != nil {
		return err
	}
	return runCmdWindows("route", "add", ip, "mask", mask, gw, "metric", "1")
}

func deleteExcludeRoute(cidr, gw string) {
	ip, mask, err := parseCIDR(cidr)
	if err != nil {
		return
	}
	_ = runCmdWindows("route", "delete", ip, "mask", mask, gw)
}
