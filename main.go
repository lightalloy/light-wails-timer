package main

import (
	"embed"
	"flag"
	"fmt"
	"log"
	"runtime"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"

	"github.com/wailsapp/wails/v3/pkg/events"

	"light-wails-timer/assets"
	"light-wails-timer/internal/i18n"
	"light-wails-timer/internal/service"
	"light-wails-timer/internal/store"
	"light-wails-timer/internal/timer"
)

//go:embed all:frontend/dist
var frontendAssets embed.FS

func init() {
	application.RegisterEvent[service.State]("timer:state")
}

func main() {
	showAtStart := flag.Bool("show", false, "показать окно сразу при запуске")
	flag.Parse()

	notifier := notifications.New()

	var showMainWindow func()

	app := application.New(application.Options{
		Name:        i18n.AppName,
		Description: "Минималистичный таймер обратного отсчёта с треем и уведомлениями",
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(frontendAssets),
		},
		Services: []application.Service{
			application.NewService(notifier),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "app.lightwailstimer.timer",
			OnSecondInstanceLaunch: func(application.SecondInstanceData) {
				application.InvokeAsync(func() {
					if showMainWindow != nil {
						showMainWindow()
					}
				})
			},
		},
	})

	stateStore, err := store.New()
	if err != nil {
		log.Fatalf("resolve timer state path: %v", err)
	}

	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            i18n.AppName,
		Width:            300,
		Height:           380,
		Frameless:        false,
		AlwaysOnTop:      true,
		DisableResize:    true,
		HideOnFocusLost:  true,
		HideOnEscape:     true,
		Hidden:           !*showAtStart,
		BackgroundColour: application.NewRGB(24, 26, 27),
		URL:              "/",
	})
	showMainWindow = func() {
		window.Show()
		window.Focus()
	}

	// The title bar close button must hide the popup, not terminate the app:
	// the timer keeps running in the tray.
	window.OnWindowEvent(events.Common.WindowClosing, func(event *application.WindowEvent) {
		event.Cancel()
		window.Hide()
	})

	var (
		tray          *application.SystemTray
		autostartItem *application.MenuItem
		lastStatus    timer.Status
		lastPreset    int
		lastTooltip   string
		buildMenu     func(status timer.Status, minutes int)
	)

	readAutostart := func() bool {
		enabled, err := app.Autostart.IsEnabled()
		if err != nil {
			log.Printf("read autostart registration: %v", err)
			return false
		}
		return enabled
	}

	applyState := func(state service.State) {
		status := timer.Status(state.Status)

		if tray != nil {
			if status != lastStatus {
				if status == timer.Running {
					tray.SetIcon(assets.IconActive)
				} else {
					tray.SetIcon(assets.IconIdle)
				}
			}

			// The XFCE Status Tray plugin caches the SNI title once and does not
			// pick up later NewTitle updates, so a live countdown in the tooltip
			// is not possible there. Keep the text state-neutral instead of
			// showing a stale "inactive": the remaining time lives in the window
			// and in the menu status line.
			if lastTooltip == "" {
				tray.SetTooltip(i18n.AppName)
				if runtime.GOOS == "linux" {
					tray.SetLabel(i18n.AppName)
				}
				lastTooltip = i18n.AppName
			}
		}

		if buildMenu != nil && (status != lastStatus || state.PresetMinutes != lastPreset) {
			buildMenu(status, state.PresetMinutes)
		}

		lastStatus = status
		lastPreset = state.PresetMinutes
	}

	core := service.New(
		timer.SystemClock(),
		stateStore,
		wailsNotifier{service: notifier},
		app.Autostart,
		func(state service.State) {
			application.InvokeAsync(func() {
				app.Event.Emit("timer:state", state)
				applyState(state)
			})
		},
		log.Printf,
	)
	app.RegisterService(application.NewService(&TimerService{core: core}))

	tray = app.SystemTray.New()
	tray.SetIcon(assets.IconIdle)
	tray.SetLabel(i18n.AppName)
	tray.SetTooltip(i18n.TooltipIdleText())

	// The menu is rebuilt per state: idle offers the three presets, running
	// offers "Стоп". Items are created fresh so no stale handler survives.
	buildMenu = func(status timer.Status, minutes int) {
		rebuilt := app.NewMenu()

		if status == timer.Running {
			rebuilt.Add(i18n.MenuStatusRunning(minutes)).SetEnabled(false)
			rebuilt.AddSeparator()
			rebuilt.Add(i18n.MenuStop).OnClick(func(*application.Context) {
				if err := core.Stop(); err != nil {
					log.Printf("stop timer: %v", err)
				}
			})
		} else {
			rebuilt.Add(i18n.MenuStatusIdle).SetEnabled(false)
			rebuilt.AddSeparator()
			for _, preset := range timer.Presets {
				preset := preset
				rebuilt.Add(i18n.MenuStart(preset)).OnClick(func(*application.Context) {
					if err := core.Start(preset); err != nil {
						log.Printf("start timer: %v", err)
					}
				})
			}
		}

		rebuilt.AddSeparator()
		rebuilt.Add(i18n.MenuShow).OnClick(func(*application.Context) {
			showMainWindow()
		})

		autostartItem = rebuilt.AddCheckbox(i18n.MenuAutostart, readAutostart())
		autostartItem.OnClick(func(ctx *application.Context) {
			wanted := ctx.ClickedMenuItem().Checked()
			if err := core.SetAutostart(wanted); err != nil {
				log.Printf("change autostart registration: %v", err)
			}
			autostartItem.SetChecked(readAutostart())
			rebuilt.Update()
		})

		rebuilt.AddSeparator()
		rebuilt.Add(i18n.MenuQuit).OnClick(func(*application.Context) {
			app.Quit()
		})

		tray.SetMenu(rebuilt)
	}
	buildMenu(timer.Idle, 0)
	// AttachWindow is called after the tray exists, so the smart default that
	// binds a primary click to the window is not installed; do it explicitly.
	tray.AttachWindow(window)
	tray.WindowOffset(8)
	tray.OnClick(func() {
		tray.ToggleWindow()
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

// wailsNotifier adapts the Wails notifications service to the service.Notifier
// interface, keeping notifications silent as specified.
type wailsNotifier struct {
	service *notifications.NotificationService
}

func (n wailsNotifier) Send(title, body string) error {
	return n.service.SendNotification(notifications.NotificationOptions{
		ID:    "light-wails-timer.finished",
		Title: title,
		Body:  body,
		Sound: &notifications.NotificationSound{Silent: true},
	})
}

func formatRemaining(seconds int) string {
	if seconds < 0 {
		seconds = 0
	}
	return fmt.Sprintf("%02d:%02d", seconds/60, seconds%60)
}
