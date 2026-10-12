package core

import (
	"reflect"
	"testing"
)

func TestTurnIPsFor(t *testing.T) {
	got := turnIPsFor([]string{"193.203.43.16:19302", "tls://91.231.135.129:443", "193.203.43.16:3478"}, &TurnParams{})
	want := []string{"193.203.43.16", "91.231.135.129"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if got := turnIPsFor([]string{"1.2.3.4:1"}, &TurnParams{Host: "5.6.7.8"}); !reflect.DeepEqual(got, []string{"5.6.7.8"}) {
		t.Fatalf("host override: %v", got)
	}
}
