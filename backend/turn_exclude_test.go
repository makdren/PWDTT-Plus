package backend

import (
	"runtime"
	"testing"
)

// Адреса TURN, полученные до подъёма туннеля, запоминаются (маршруты
// добавит Apply); IPv6 и мусор игнорируются.
func TestExcludeTURNPendingBeforeTunnel(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("маршруты исключений — только Windows")
	}
	w := &WG{}
	w.ExcludeTURN([]string{"193.203.43.16", "91.231.135.129", "193.203.43.16", "::1", "bad"}, nil)
	if len(w.turnExtra) != 2 || !w.turnExtra["193.203.43.16"] || !w.turnExtra["91.231.135.129"] {
		t.Fatalf("turnExtra = %v", w.turnExtra)
	}
	if len(w.activeExcludeRoutes) != 0 {
		t.Fatal("без туннеля маршруты не добавляются")
	}
}
