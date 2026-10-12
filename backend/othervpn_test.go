package backend

import "testing"

// Вывод route print при запущенном Happ (happ-xray) и PWDTT Plus.
const routePrintSample = `
===========================================================================
Active Routes:
Network Destination        Netmask          Gateway       Interface  Metric
          0.0.0.0          0.0.0.0      192.168.0.1    192.168.0.102   9999
          0.0.0.0          0.0.0.0         On-link        172.19.0.1      0
          0.0.0.0          0.0.0.0         On-link       10.66.66.4      1
===========================================================================
Persistent Routes:
  None
`

func TestForeignDefaultRoutes(t *testing.T) {
	routes := parseDefaultRoutes(routePrintSample)
	if len(routes) != 3 {
		t.Fatalf("parsed %d routes", len(routes))
	}
	foreign := foreignDefaultRoutes(routes, "192.168.0.1", "10.66.66.4")
	if len(foreign) != 1 || foreign[0].iface != "172.19.0.1" {
		t.Fatalf("foreign = %+v", foreign)
	}
	if got := foreignDefaultRoutes(routes[:1], "192.168.0.1", "10.66.66.4"); len(got) != 0 {
		t.Fatalf("only physical gateway must not warn: %+v", got)
	}
}

func TestExpandDefaultRoute(t *testing.T) {
	got := expandDefaultRoute([]string{"0.0.0.0/0"})
	if len(got) != 3 || got[1] != "0.0.0.0/1" || got[2] != "128.0.0.0/1" {
		t.Fatalf("got %v", got)
	}
	if got := expandDefaultRoute([]string{"10.66.66.0/24"}); len(got) != 1 {
		t.Fatalf("split AllowedIPs must stay as is: %v", got)
	}
}
