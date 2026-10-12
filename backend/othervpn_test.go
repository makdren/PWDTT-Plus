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

func TestParseNativeIPv6Default(t *testing.T) {
	teredo := `Publish  Type      Met  Prefix                    Idx  Gateway/Interface Name
-------  --------  ---  ------------------------  ---  ------------------------
No       Manual    256  ::/0                       12  Teredo Tunneling Pseudo-Interface
No       System    256  ::1/128                     1  Loopback Pseudo-Interface 1`
	if parseNativeIPv6Default(teredo) {
		t.Fatal("Teredo must not count as native IPv6")
	}
	native := `No       Manual    256  ::/0                       10  fe80::1`
	if !parseNativeIPv6Default(native) {
		t.Fatal("native IPv6 default route not detected")
	}
}

func TestParseWGDNS(t *testing.T) {
	conf := "[Interface]\nPrivateKey = x\nAddress = 10.66.66.4/32\nDNS = 1.1.1.1, 8.8.8.8, ::1\n[Peer]\n"
	got := parseWGDNS(conf)
	if len(got) != 2 || got[0] != "1.1.1.1" || got[1] != "8.8.8.8" {
		t.Fatalf("got %v", got)
	}
}
