//go:build windows

package main

import (
	"context"
	"embed"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/energye/systray"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"pwdtt/backend"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed assets/icons/icon.png
var appIcon []byte

//go:embed assets/icons/icon.ico
var trayIcon []byte

//go:embed assets/wintun.dll
var wintunDLL []byte

// Клиент WDTT Plus (собирается scripts/build-engine.sh из WDTT-Plus/go_client).
//go:embed assets/wdtt-client.exe
var wdttClientExe []byte

var (
	trayReady    atomic.Bool
	trayStopping atomic.Bool
	trayStop     = make(chan struct{})
	trayExited   = make(chan struct{})
	trayStopOnce sync.Once
	trayExitOnce sync.Once
)

func startTray(ctx context.Context) {
	go systray.Run(func() {
		if trayStopping.Load() {
			systray.Quit()
			return
		}

		systray.SetIcon(trayIcon)
		systray.SetTooltip("RWDTT")
		systray.SetOnClick(func(systray.IMenu) { showWindow(ctx) })
		systray.SetOnDClick(func(systray.IMenu) { showWindow(ctx) })

		show := systray.AddMenuItem("Show RWDTT", "Restore RWDTT")
		show.Click(func() { showWindow(ctx) })
		systray.AddSeparator()
		exit := systray.AddMenuItem("Exit", "Exit RWDTT")
		exit.Click(func() { wailsruntime.Quit(ctx) })

		trayReady.Store(true)
		go hideMinimisedWindow(ctx)
	}, func() {
		trayReady.Store(false)
		trayExitOnce.Do(func() { close(trayExited) })
		if !trayStopping.Load() {
			showWindow(ctx)
		}
	})
}

// windowShown будит наблюдатель, когда окно снова показали.
var windowShown = make(chan struct{}, 1)

// showWindow — единственный путь показать окно (трей, второй экземпляр,
// ссылка wdtt://): наблюдатель за сворачиванием и анимации включаются снова.
func showWindow(ctx context.Context) {
	wailsruntime.WindowUnminimise(ctx)
	wailsruntime.WindowShow(ctx)
	select {
	case windowShown <- struct{}{}:
	default:
	}
	wailsruntime.EventsEmit(ctx, "window_visible", true)
}

// hideMinimisedWindow прячет свёрнутое окно в трей. Пока окно в трее,
// опрос не идёт вовсе (раньше — 5 раз в секунду, всё время работы).
func hideMinimisedWindow(ctx context.Context) {
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-trayStop:
			return
		case <-ticker.C:
		}
		if !trayReady.Load() || !wailsruntime.WindowIsMinimised(ctx) {
			continue
		}
		wailsruntime.WindowHide(ctx)
		wailsruntime.EventsEmit(ctx, "window_visible", false)
		ticker.Stop()
		select {
		case <-trayStop:
			return
		case <-windowShown:
		}
		ticker.Reset(300 * time.Millisecond)
	}
}

func stopTray() {
	trayStopping.Store(true)
	trayStopOnce.Do(func() { close(trayStop) })
	if trayReady.Load() {
		systray.Quit()
		select {
		case <-trayExited:
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func main() {
	backend.WaitAfterUpdate(os.Args[1:])
	backend.InitWintun(wintunDLL)
	backend.InitEngine(wdttClientExe)
	app := backend.NewApp()
	app.HandleLaunchArgs(os.Args[1:])
	secondInstanceLaunch := make(chan struct{}, 1)

	err := wails.Run(&options.App{
		Title:     "RWDTT",
		Width:     900,
		Height:    600,
		MinWidth:  800,
		MinHeight: 550,
		Frameless: false,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 255, G: 255, B: 255, A: 1},
		OnStartup: func(ctx context.Context) {
			app.Startup(ctx)
			backend.ShowWindowHook = func() { showWindow(ctx) }
			startTray(ctx)
			go func() {
				for range secondInstanceLaunch {
					showWindow(ctx)
				}
			}()
		},
		OnShutdown: func(ctx context.Context) {
			stopTray()
			app.Shutdown(ctx)
		},
		Bind: []interface{}{app},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "pwdtt-plus-windows-client",
			OnSecondInstanceLaunch: func(data options.SecondInstanceData) {
				app.HandleLaunchArgs(data.Args)
				select {
				case secondInstanceLaunch <- struct{}{}:
				default:
				}
			},
		},
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
		},
	})
	if err != nil {
		panic(err)
	}
}
