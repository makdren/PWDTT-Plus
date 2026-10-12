package core

import (
	"errors"
	"testing"
)

func TestAuthControlFallsBackToLegacyIDOnce(t *testing.T) {
	var fatal []error
	a := newAuthControl("pwdtt-abc", func(err error) { fatal = append(fatal, err) })
	mismatch := errors.New("FATAL_AUTH: пароль привязан к другому устройству — ...")

	if !a.handleFatal(mismatch, "pwdtt-abc") {
		t.Fatal("first mismatch must retry with the legacy ID")
	}
	if got := a.DeviceID(); got != legacyDeviceID {
		t.Fatalf("DeviceID = %q, want %q", got, legacyDeviceID)
	}
	// Другие воркеры получили отказ ещё со старым ID — тоже повторяют.
	if !a.handleFatal(mismatch, "pwdtt-abc") {
		t.Fatal("worker that used the previous ID must retry")
	}
	if len(fatal) != 0 {
		t.Fatalf("no fatal expected yet, got %v", fatal)
	}
	// Старый ID тоже отклонён — останавливаемся ровно один раз.
	if a.handleFatal(mismatch, legacyDeviceID) || a.handleFatal(mismatch, legacyDeviceID) {
		t.Fatal("mismatch with the legacy ID must stop")
	}
	if len(fatal) != 1 {
		t.Fatalf("onFatal called %d times, want 1", len(fatal))
	}
}

func TestAuthControlStopsOnWrongPassword(t *testing.T) {
	stopped := 0
	a := newAuthControl("pwdtt-abc", func(error) { stopped++ })
	if a.handleFatal(errors.New("FATAL_AUTH: неверный пароль подключения"), "pwdtt-abc") {
		t.Fatal("wrong password must not retry")
	}
	if stopped != 1 || a.DeviceID() != "pwdtt-abc" {
		t.Fatalf("stopped=%d id=%q", stopped, a.DeviceID())
	}
}
