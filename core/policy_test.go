package core

import "testing"

func TestParseWorkerPolicy(t *testing.T) {
	cases := map[string]int{
		"POLICY:max_workers=18":  18,
		"POLICY:max_workers= 9 ": 9,
		"POLICY:max_workers=0":   0,
		"POLICY:max_workers=999": 0,
		"POLICY:max_workers=x":   0,
		"[Interface]":            0,
	}
	for in, want := range cases {
		got, ok := parseWorkerPolicy(in)
		if ok != (want > 0) || got != want {
			t.Errorf("%q: got %d,%v want %d", in, got, ok, want)
		}
	}
}

func TestWorkerOverServerLimit(t *testing.T) {
	resetServerWorkerLimit()
	if workerOverServerLimit(100) {
		t.Fatal("no limit set")
	}
	if !noteWorkerPolicy(18) || noteWorkerPolicy(18) {
		t.Fatal("noteWorkerPolicy change detection")
	}
	if workerOverServerLimit(18) || !workerOverServerLimit(19) {
		t.Fatal("limit boundary")
	}
	resetServerWorkerLimit()
}

func TestStripServiceConfigLines(t *testing.T) {
	in := "# WDTT_TRANSPORTS=uplink2\n[Interface]\nPrivateKey = x\n# comment\n"
	want := "[Interface]\nPrivateKey = x\n# comment\n"
	if got := stripServiceConfigLines(in); got != want {
		t.Fatalf("got %q", got)
	}
}

func TestIsDeniedReason(t *testing.T) {
	if !isDeniedReason("expired") || !isDeniedReason("wrong_password") {
		t.Fatal("valid reasons")
	}
	if isDeniedReason("") || isDeniedReason("X y") {
		t.Fatal("invalid reasons")
	}
}
