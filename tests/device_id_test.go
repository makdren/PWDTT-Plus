package backend_test

import (
	"strings"
	"testing"

	"pwdtt/backend"
)

// У компьютера постоянный собственный ID (не общий "unknown").
func TestMachineDeviceIDStable(t *testing.T) {
	s := newTestStore(t)
	id := s.MachineDeviceID()
	if !strings.HasPrefix(id, "pwdtt-") || len(id) < 20 {
		t.Fatalf("id = %q", id)
	}
	if again := backend.NewStore().MachineDeviceID(); again != id {
		t.Fatalf("id changed: %q -> %q", id, again)
	}
}
