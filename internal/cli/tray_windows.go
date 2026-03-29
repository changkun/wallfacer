//go:build desktop && windows

package cli

import (
	"changkun.de/x/wallfacer/assets/icons"

	"fyne.io/systray"
)

// platformTraySetup performs Windows-specific tray initialization.
// It sets the tray icon to the .ico format (required for Windows system tray)
// and registers a left-click handler to show the main window.
func platformTraySetup(showWindow func()) {
	systray.SetIcon(icons.TrayICO)

	// On Windows, left-click on the tray icon should show/focus the window.
	// Right-click opens the menu (default behavior).
	systray.SetOnTapped(showWindow)
}

// installDockReopenHandler is a no-op on Windows (no dock icon reopen concept).
func installDockReopenHandler(func()) {}

// Windows tray behavior notes:
//
// - Balloon notification: fyne.io/systray does not support Windows balloon
//   (toast) notifications. Users see the tray icon appear which is sufficient
//   indication that the app is running in the background.
// - Close minimizes to tray: HideWindowOnClose is set in desktop.go for windows.
