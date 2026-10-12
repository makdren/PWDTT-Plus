//go:build windows

package backend

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// Регистрация протоколов в HKCU\Software\Classes — без прав администратора.
var linkSchemes = []string{"wdtt", "qwdtt"}

func exePath() (string, error) {
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	return p, nil
}

func openCommand(exe string) string {
	return `"` + exe + `" "%1"`
}

func registerLinkHandler() error {
	exe, err := exePath()
	if err != nil {
		return err
	}
	for _, scheme := range linkSchemes {
		base := `Software\Classes\` + scheme
		k, _, err := registry.CreateKey(registry.CURRENT_USER, base, registry.SET_VALUE)
		if err != nil {
			return err
		}
		_ = k.SetStringValue("", "URL:"+strings.ToUpper(scheme)+" Protocol")
		_ = k.SetStringValue("URL Protocol", "")
		k.Close()

		if ik, _, err := registry.CreateKey(registry.CURRENT_USER, base+`\DefaultIcon`, registry.SET_VALUE); err == nil {
			_ = ik.SetStringValue("", `"`+exe+`",0`)
			ik.Close()
		}
		ck, _, err := registry.CreateKey(registry.CURRENT_USER, base+`\shell\open\command`, registry.SET_VALUE)
		if err != nil {
			return err
		}
		err = ck.SetStringValue("", openCommand(exe))
		ck.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func unregisterLinkHandler() error {
	for _, scheme := range linkSchemes {
		if !ownedByUs(scheme) {
			continue
		}
		base := `Software\Classes\` + scheme
		for _, sub := range []string{`\shell\open\command`, `\shell\open`, `\shell`, `\DefaultIcon`, ``} {
			_ = registry.DeleteKey(registry.CURRENT_USER, base+sub)
		}
	}
	return nil
}

// ownedByUs — ключ протокола указывает на текущий exe.
func ownedByUs(scheme string) bool {
	exe, err := exePath()
	if err != nil {
		return false
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Classes\`+scheme+`\shell\open\command`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetStringValue("")
	return err == nil && strings.EqualFold(v, openCommand(exe))
}

func linkHandlerRegistered() bool {
	for _, scheme := range linkSchemes {
		if !ownedByUs(scheme) {
			return false
		}
	}
	return true
}
