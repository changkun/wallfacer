//go:build desktop && linux

package cli

import "fyne.io/systray"

// platformTraySetup performs Linux-specific tray initialization.
// On Linux, left-click typically opens the menu (same as right-click),
// which is the default fyne.io/systray behavior — no override needed.
func platformTraySetup(showWindow func()) {
	// On Linux, left-click on the tray icon shows the window for
	// consistency with macOS/Windows behavior.
	systray.SetOnTapped(showWindow)
}

// installDockReopenHandler is a no-op on Linux.
func installDockReopenHandler(func()) {}

// Linux tray behavior notes:
//
// - GNOME: System tray icons require the AppIndicator/KStatusNotifierItem
//   GNOME Shell extension (e.g., "AppIndicator and KStatusNotifierItem Support").
//   Without it, the tray icon will not appear. KDE, XFCE, and other desktops
//   support tray icons natively.
// - Icons: The 22x22 PNG icons created in Task 3 are the correct size for
//   Linux system trays.
