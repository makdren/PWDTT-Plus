package backend

import (
	"strings"
	"sync"

	wails "github.com/wailsapp/wails/v2/pkg/runtime"
)

// Ссылки wdtt:// и qwdtt://, переданные приложению системой (клик по ссылке
// в браузере/Telegram) через аргументы командной строки. Разбор и проверка —
// во frontend (lib/utils/wdttLink.ts), здесь только доставка.

const maxLinkLen = 8 * 1024

var (
	pendingLinkMu sync.Mutex
	pendingLink   string
)

// ExtractLinkArg ищет в аргументах запуска ссылку подключения.
func ExtractLinkArg(args []string) string {
	for _, a := range args {
		a = strings.Trim(strings.TrimSpace(a), `"`)
		lower := strings.ToLower(a)
		if (strings.HasPrefix(lower, "wdtt://") || strings.HasPrefix(lower, "qwdtt://")) && len(a) <= maxLinkLen {
			return a
		}
	}
	return ""
}

// HandleLaunchArgs вызывается при старте и при запуске второго экземпляра.
func (a *App) HandleLaunchArgs(args []string) {
	link := ExtractLinkArg(args)
	if link == "" {
		return
	}
	pendingLinkMu.Lock()
	pendingLink = link
	pendingLinkMu.Unlock()
	if a.ctx != nil {
		wails.EventsEmit(a.ctx, "wdtt_link")
	}
}

// TakePendingLink отдаёт frontend'у ожидающую ссылку (один раз).
func (a *App) TakePendingLink() string {
	pendingLinkMu.Lock()
	defer pendingLinkMu.Unlock()
	link := pendingLink
	pendingLink = ""
	return link
}

// IsLinkHandlerRegistered — открываются ли ссылки wdtt:// этим приложением.
func (a *App) IsLinkHandlerRegistered() bool {
	return linkHandlerRegistered()
}

// SetLinkHandler включает/выключает открытие ссылок wdtt:// и qwdtt:// в RWDTT.
func (a *App) SetLinkHandler(enabled bool) error {
	settings := a.store.LoadSettings()
	settings.LinkHandlerOff = !enabled
	_ = a.store.SaveSettings(settings)
	if enabled {
		return registerLinkHandler()
	}
	return unregisterLinkHandler()
}

// ensureLinkHandler при запуске (пере)регистрирует протокол на текущий exe —
// после перемещения или обновления exe ссылки продолжат открываться.
func (a *App) ensureLinkHandler() {
	if a.store.LoadSettings().LinkHandlerOff || linkHandlerRegistered() {
		return
	}
	_ = registerLinkHandler()
}
