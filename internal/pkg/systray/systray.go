// Package systray provides a cross-platform system tray (notification area)
// icon with a dropdown menu. It supports macOS (NSStatusItem), Linux (D-Bus
// StatusNotifierItem), and Windows (Shell_NotifyIcon).
package systray

import (
	"sync"
	"sync/atomic"
)

// MenuItem represents a menu item in the system tray menu.
type MenuItem struct {
	// ClickedCh receives a value when the menu item is clicked.
	ClickedCh chan struct{}

	id        uint32
	title     string
	tooltip   string
	disabled  bool
	checked   bool
	checkable bool
	mu        sync.Mutex
}

var (
	menuItems     = make(map[uint32]*MenuItem)
	menuItemsLock sync.RWMutex
	nextID        atomic.Uint32

	readyCb func()
	exitCb  func()

	onTapped func()
	tappedMu sync.Mutex
)

// RunWithExternalLoop initializes the system tray for use with an external
// event loop (e.g., Wails). It returns start and end functions. Call start()
// to create the tray icon and trigger onReady. Call end() to tear down.
func RunWithExternalLoop(onReady, onExit func()) (start, end func()) {
	readyCb = onReady
	exitCb = onExit
	return nativeStart, func() {
		nativeEnd()
		if exitCb != nil {
			exitCb()
		}
	}
}

// SetIcon sets the tray icon from raw image bytes (PNG or ICO).
func SetIcon(iconBytes []byte) {
	nativeSetIcon(iconBytes, false)
}

// SetTemplateIcon sets a macOS template icon that adapts to the menu bar
// appearance. On other platforms this behaves the same as SetIcon.
func SetTemplateIcon(templateIconBytes, regularIconBytes []byte) {
	nativeSetIcon(templateIconBytes, true)
}

// SetTooltip sets the tray icon tooltip text.
func SetTooltip(tooltip string) {
	nativeSetTooltip(tooltip)
}

// SetOnTapped sets a callback for left-click on the tray icon.
// On macOS this is ignored (left-click always opens the menu).
func SetOnTapped(fn func()) {
	tappedMu.Lock()
	onTapped = fn
	tappedMu.Unlock()
	nativeSetOnTapped(fn != nil)
}

// AddMenuItem adds a regular menu item and returns it.
func AddMenuItem(title, tooltip string) *MenuItem {
	return addItem(title, tooltip, false, false)
}

// AddMenuItemCheckbox adds a checkable menu item and returns it.
func AddMenuItemCheckbox(title, tooltip string, checked bool) *MenuItem {
	return addItem(title, tooltip, true, checked)
}

// AddSeparator adds a visual separator line to the menu.
func AddSeparator() {
	id := nextID.Add(1)
	nativeAddSeparator(id)
}

// Quit signals the system tray to shut down and remove its icon.
func Quit() {
	nativeQuit()
}

func addItem(title, tooltip string, checkable, checked bool) *MenuItem {
	id := nextID.Add(1)
	item := &MenuItem{
		ClickedCh: make(chan struct{}, 1),
		id:        id,
		title:     title,
		tooltip:   tooltip,
		checkable: checkable,
		checked:   checked,
	}
	menuItemsLock.Lock()
	menuItems[id] = item
	menuItemsLock.Unlock()

	nativeAddMenuItem(id, title, tooltip, checkable, checked)
	return item
}

// SetTitle updates the menu item's display text.
func (item *MenuItem) SetTitle(title string) {
	item.mu.Lock()
	item.title = title
	item.mu.Unlock()
	nativeSetItemTitle(item.id, title)
}

// Disable grays out the menu item so it cannot be clicked.
func (item *MenuItem) Disable() {
	item.mu.Lock()
	item.disabled = true
	item.mu.Unlock()
	nativeSetItemEnabled(item.id, false)
}

// Enable makes the menu item clickable.
func (item *MenuItem) Enable() {
	item.mu.Lock()
	item.disabled = false
	item.mu.Unlock()
	nativeSetItemEnabled(item.id, true)
}

// Check marks the menu item as checked.
func (item *MenuItem) Check() {
	item.mu.Lock()
	item.checked = true
	item.mu.Unlock()
	nativeSetItemChecked(item.id, true)
}

// Uncheck removes the check mark from the menu item.
func (item *MenuItem) Uncheck() {
	item.mu.Lock()
	item.checked = false
	item.mu.Unlock()
	nativeSetItemChecked(item.id, false)
}

// Checked returns whether the menu item is currently checked.
func (item *MenuItem) Checked() bool {
	item.mu.Lock()
	defer item.mu.Unlock()
	return item.checked
}

// menuItemClicked is called by platform code when a menu item is clicked.
func menuItemClicked(id uint32) {
	menuItemsLock.RLock()
	item, ok := menuItems[id]
	menuItemsLock.RUnlock()
	if !ok {
		return
	}
	select {
	case item.ClickedCh <- struct{}{}:
	default:
	}
}

// trayTapped is called by platform code on left-click of the tray icon.
func trayTapped() {
	tappedMu.Lock()
	fn := onTapped
	tappedMu.Unlock()
	if fn != nil {
		fn()
	}
}
