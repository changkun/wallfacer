//go:build desktop && darwin

package cli

import "fyne.io/systray"

// platformTraySetup performs macOS-specific tray initialization.
// On macOS, left-click on the tray icon opens the menu (default behavior).
// SetOnTapped is used to show the window as well, providing a quick shortcut.
func platformTraySetup(showWindow func()) {
	systray.SetOnTapped(showWindow)
}

// macOS tray behavior notes:
//
// - Template icons: SetTemplateIcon is called in tray.go:onReady() which
//   correctly makes macOS auto-adapt the icon for light/dark menu bar.
// - Close hides: HideWindowOnClose is set to true in desktop.go for darwin.
// - Cmd+Q: Wails handles Cmd+Q natively, triggering OnShutdown.
// - Dock icon click: Wails v2 does not expose a public API for dock icon
//   reopen events. The user can use "Open Dashboard" from the tray menu
//   or left-click the tray icon.
