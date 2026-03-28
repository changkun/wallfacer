//go:build desktop

package cli

import (
	"changkun.de/x/wallfacer/assets/icons"
	"changkun.de/x/wallfacer/internal/logger"

	"fyne.io/systray"
)

// TrayManager manages the system tray icon and menu for the desktop app.
type TrayManager struct {
	showWindow func()
	quit       func()
	stopLoop   func() // returned by systray.RunWithExternalLoop
}

// NewTrayManager creates a TrayManager. showWindow is called when the user
// clicks "Open Dashboard"; quit is called when the user clicks "Quit".
func NewTrayManager(showWindow, quit func()) *TrayManager {
	return &TrayManager{
		showWindow: showWindow,
		quit:       quit,
	}
}

// Start initializes the system tray icon and menu. It must be called from
// the main goroutine on macOS.
func (tm *TrayManager) Start() {
	start, end := systray.RunWithExternalLoop(tm.onReady, func() {
		logger.Main.Info("system tray exited")
	})
	tm.stopLoop = end
	start()
}

// Stop tears down the system tray.
func (tm *TrayManager) Stop() {
	systray.Quit()
	if tm.stopLoop != nil {
		tm.stopLoop()
	}
}

// onReady is called by systray when the tray is ready to receive menu items.
func (tm *TrayManager) onReady() {
	systray.SetTemplateIcon(icons.Tray, icons.Tray)
	systray.SetTooltip("Wallfacer")

	mOpen := systray.AddMenuItem("Open Dashboard", "Show the Wallfacer window")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quit", "Quit Wallfacer")

	go func() {
		for {
			select {
			case <-mOpen.ClickedCh:
				tm.showWindow()
			case <-mQuit.ClickedCh:
				tm.quit()
				return
			}
		}
	}()
}
