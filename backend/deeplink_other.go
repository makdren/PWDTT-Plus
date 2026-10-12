//go:build !windows

package backend

import "errors"

func registerLinkHandler() error {
	return errors.New("регистрация ссылок поддерживается только в Windows")
}

func unregisterLinkHandler() error { return nil }

func linkHandlerRegistered() bool { return false }
