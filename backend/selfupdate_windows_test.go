//go:build windows

package backend

import (
	"os"
	"path/filepath"
	"testing"
)

// Подмена «запущенного» exe: старый уходит в .old, новый встаёт на место и запускается.
func TestReplaceAndRestart(t *testing.T) {
	sys := filepath.Join(os.Getenv("SystemRoot"), "System32")
	src, err := os.ReadFile(filepath.Join(sys, "whoami.exe"))
	if err != nil {
		t.Skip("whoami.exe недоступен")
	}
	dir := t.TempDir()
	exe := filepath.Join(dir, "app.exe")
	newer := filepath.Join(dir, "app.exe.download")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newer, src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := replaceAndRestart(exe, newer); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe + ".old"); string(b) != "old" {
		t.Fatal("старая версия не сохранена в .old")
	}
	if b, _ := os.ReadFile(exe); len(b) != len(src) {
		t.Fatal("новая версия не установлена")
	}
	if _, err := os.Stat(newer); !os.IsNotExist(err) {
		t.Fatal("временный файл остался")
	}
}

func TestReplaceRollbackOnBadExe(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "app.exe")
	newer := filepath.Join(dir, "app.exe.download")
	_ = os.WriteFile(exe, []byte("old"), 0o755)
	_ = os.WriteFile(newer, []byte("not a program"), 0o755)
	if err := replaceAndRestart(exe, newer); err == nil {
		t.Fatal("запуск не-программы должен провалиться")
	}
	if b, _ := os.ReadFile(exe); string(b) != "old" {
		t.Fatal("откат не восстановил старую версию")
	}
}
