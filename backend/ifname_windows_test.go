//go:build windows

package backend

import "testing"

func TestRealIfNameFallback(t *testing.T) {
	if got := realIfName(nil, "pwdtt-wg"); got != "pwdtt-wg" {
		t.Fatal(got)
	}
}
