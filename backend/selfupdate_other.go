//go:build !windows

package backend

import (
	"errors"
	"os"
)

func replaceAndRestart(exe, downloaded string) error {
	return errors.New("обновление из приложения поддерживается только в Windows")
}

func exePath() (string, error) { return os.Executable() }

func WaitAfterUpdate(args []string) {}
