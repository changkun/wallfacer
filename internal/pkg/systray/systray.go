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
	menuItems     = make(map[uint32]*MenuItem) // maps menu item IDs to their MenuItem structs
	menuItemsLock sync.RWMutex                 // guards menuItems
	nextID        atomic.Uint32                // monotonic counter for unique menu item IDs

	readyCb func() // called once the tray icon is initialized
	exitCb  func() // called when the tray is torn down

	onTapped func()     // left-click callback; nil means open menu instead
	tappedMu sync.Mutex // guards onTapped
)

// Hooks for native platform calls; tests swap these to avoid cgo/GUI dependencies.
var (
	callNativeStart        = func() { nativeStart() }
	callNativeEnd          = func() { nativeEnd() }
	callNativeSetIcon      = func(d []byte, t bool) { nativeSetIcon(d, t) }
	callNativeSetTooltip   = func(s string) { nativeSetTooltip(s) }
	callNativeAddMenuItem  = func(id uint32, title, tooltip string, ck, ch bool) { nativeAddMenuItem(id, title, tooltip, ck, ch) }
	callNativeAddSeparator = func(id uint32) { nativeAddSeparator(id) }
	callNativeSetItemTitle = func(id uint32, t string) { nativeSetItemTitle(id, t) }
	callNativeSetEnabled   = func(id uint32, e bool) { nativeSetItemEnabled(id, e) }
	callNativeSetChecked   = func(id uint32, c bool) { nativeSetItemChecked(id, c) }
	callNativeQuit         = func() { nativeQuit() }
	callNativeSetOnTapped  = func(e bool) { nativeSetOnTapped(e) }
)

// RunWithExternalLoop initializes the system tray for use with an external
// event loop (e.g., Wails). It returns start and end functions. Call start()
// to create the tray icon and trigger onReady. Call end() to tear down.
func RunWithExternalLoop(onReady, onExit func()) (start, end func()) {
	readyCb = onReady
	exitCb = onExit
	return func() { callNativeStart() }, func() {
		callNativeEnd()
		if exitCb != nil {
			exitCb()
		}
	}
}

// SetIcon sets the tray icon from raw image bytes (PNG or ICO).
func SetIcon(iconBytes []byte) {
	callNativeSetIcon(iconBytes, false)
}

// SetTemplateIcon sets a macOS template icon that adapts to the menu bar
// appearance. On other platforms this behaves the same as SetIcon.
func SetTemplateIcon(templateIconBytes, _ []byte) {
	callNativeSetIcon(templateIconBytes, true)
}

// SetTooltip sets the tray icon tooltip text.
func SetTooltip(tooltip string) {
	callNativeSetTooltip(tooltip)
}

// SetOnTapped sets a callback for left-click on the tray icon.
// On macOS this is ignored (left-click always opens the menu).
func SetOnTapped(fn func()) {
	tappedMu.Lock()
	onTapped = fn
	tappedMu.Unlock()
	callNativeSetOnTapped(fn != nil)
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
	callNativeAddSeparator(id)
}

// Quit signals the system tray to shut down and remove its icon.
func Quit() {
	callNativeQuit()
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

	callNativeAddMenuItem(id, title, tooltip, checkable, checked)
	return item
}

// SetTitle updates the menu item's display text.
func (item *MenuItem) SetTitle(title string) {
	item.mu.Lock()
	item.title = title
	item.mu.Unlock()
	callNativeSetItemTitle(item.id, title)
}

// Disable grays out the menu item so it cannot be clicked.
func (item *MenuItem) Disable() {
	item.mu.Lock()
	item.disabled = true
	item.mu.Unlock()
	callNativeSetEnabled(item.id, false)
}

// Enable makes the menu item clickable.
func (item *MenuItem) Enable() {
	item.mu.Lock()
	item.disabled = false
	item.mu.Unlock()
	callNativeSetEnabled(item.id, true)
}

// Check marks the menu item as checked.
func (item *MenuItem) Check() {
	item.mu.Lock()
	item.checked = true
	item.mu.Unlock()
	callNativeSetChecked(item.id, true)
}

// Uncheck removes the check mark from the menu item.
func (item *MenuItem) Uncheck() {
	item.mu.Lock()
	item.checked = false
	item.mu.Unlock()
	callNativeSetChecked(item.id, false)
}

// Checked returns whether the menu item is currently checked.
func (item *MenuItem) Checked() bool {
	item.mu.Lock()
	defer item.mu.Unlock()
	return item.checked
}

// menuItemClicked is called by platform code when a menu item is clicked.
// It sends a non-blocking signal on the item's ClickedCh. If the channel
// is already full (capacity 1), the click is silently dropped to avoid
// blocking the platform event thread.
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
