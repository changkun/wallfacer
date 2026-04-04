package systray

import (
	"testing"
)

func swapHook[T any](t *testing.T, ptr *T, val T) {
	t.Helper()
	orig := *ptr
	*ptr = val
	t.Cleanup(func() { *ptr = orig })
}

// stubAllNative replaces all native hooks with no-ops for the duration of the test.
func stubAllNative(t *testing.T) {
	t.Helper()
	swapHook(t, &callNativeStart, func() {})
	swapHook(t, &callNativeEnd, func() {})
	swapHook(t, &callNativeSetIcon, func([]byte, bool) {})
	swapHook(t, &callNativeSetTooltip, func(string) {})
	swapHook(t, &callNativeAddMenuItem, func(uint32, string, string, bool, bool) {})
	swapHook(t, &callNativeAddSeparator, func(uint32) {})
	swapHook(t, &callNativeSetItemTitle, func(uint32, string) {})
	swapHook(t, &callNativeSetEnabled, func(uint32, bool) {})
	swapHook(t, &callNativeSetChecked, func(uint32, bool) {})
	swapHook(t, &callNativeQuit, func() {})
	swapHook(t, &callNativeSetOnTapped, func(bool) {})
}

// TestMenuItemCheckedState verifies that the Checked() accessor reflects
// direct mutations of the checked field (bypassing native calls).
func TestMenuItemCheckedState(t *testing.T) {
	item := &MenuItem{ClickedCh: make(chan struct{}, 1)}
	if item.Checked() {
		t.Error("new item should not be checked")
	}
	item.mu.Lock()
	item.checked = true
	item.mu.Unlock()
	if !item.Checked() {
		t.Error("item should be checked after setting")
	}
	item.mu.Lock()
	item.checked = false
	item.mu.Unlock()
	if item.Checked() {
		t.Error("item should not be checked after unsetting")
	}
}

// TestMenuItemClicked verifies that menuItemClicked dispatches a signal
// to the correct item's ClickedCh channel.
func TestMenuItemClicked(t *testing.T) {
	id := nextID.Add(1)
	item := &MenuItem{ClickedCh: make(chan struct{}, 1), id: id}
	menuItemsLock.Lock()
	menuItems[id] = item
	menuItemsLock.Unlock()
	defer func() {
		menuItemsLock.Lock()
		delete(menuItems, id)
		menuItemsLock.Unlock()
	}()

	menuItemClicked(id)
	select {
	case <-item.ClickedCh:
	default:
		t.Fatal("expected click event on ClickedCh")
	}
}

// TestMenuItemClickedDropsWhenFull verifies that a second click does not
// block when the channel buffer (capacity 1) is already occupied.
func TestMenuItemClickedDropsWhenFull(_ *testing.T) {
	id := nextID.Add(1)
	item := &MenuItem{ClickedCh: make(chan struct{}, 1), id: id}
	menuItemsLock.Lock()
	menuItems[id] = item
	menuItemsLock.Unlock()
	defer func() {
		menuItemsLock.Lock()
		delete(menuItems, id)
		menuItemsLock.Unlock()
	}()

	// Fill the channel.
	item.ClickedCh <- struct{}{}
	// Second click should not block.
	menuItemClicked(id)
}

// TestMenuItemClickedInvalidID verifies that clicking a non-existent menu
// item ID is silently ignored (no panic, no channel send).
func TestMenuItemClickedInvalidID(_ *testing.T) {
	menuItemClicked(999999)
}

// TestTrayTapped verifies that trayTapped invokes the registered onTapped callback.
func TestTrayTapped(t *testing.T) {
	called := false
	tappedMu.Lock()
	onTapped = func() { called = true }
	tappedMu.Unlock()
	defer func() {
		tappedMu.Lock()
		onTapped = nil
		tappedMu.Unlock()
	}()

	trayTapped()
	if !called {
		t.Fatal("expected tapped callback to be called")
	}
}

// TestTrayTappedNil verifies that trayTapped is a no-op when no callback
// is registered (does not panic on nil function).
func TestTrayTappedNil(_ *testing.T) {
	tappedMu.Lock()
	onTapped = nil
	tappedMu.Unlock()
	trayTapped()
}

// TestRunWithExternalLoop verifies callback wiring and start/end functions.
func TestRunWithExternalLoop(t *testing.T) {
	stubAllNative(t)

	exitCalled := false
	start, end := RunWithExternalLoop(nil, func() { exitCalled = true })

	// start() should invoke the native start hook.
	startCalled := false
	swapHook(t, &callNativeStart, func() { startCalled = true })
	start()
	if !startCalled {
		t.Fatal("start() should call nativeStart")
	}

	// end() should invoke native end and exitCb.
	end()
	if !exitCalled {
		t.Fatal("end() should call exitCb")
	}
}

// TestRunWithExternalLoop_NilExitCb verifies end() doesn't panic with nil exitCb.
func TestRunWithExternalLoop_NilExitCb(t *testing.T) {
	stubAllNative(t)
	_, end := RunWithExternalLoop(nil, nil)
	end() // must not panic
}

// TestSetIcon verifies that SetIcon forwards to the native icon hook.
func TestSetIcon(t *testing.T) {
	stubAllNative(t)
	var gotData []byte
	var gotTemplate bool
	swapHook(t, &callNativeSetIcon, func(d []byte, tmpl bool) {
		gotData = d
		gotTemplate = tmpl
	})
	SetIcon([]byte{0x89, 0x50})
	if len(gotData) != 2 || gotTemplate {
		t.Fatalf("SetIcon: data=%v, template=%v", gotData, gotTemplate)
	}
}

// TestSetTemplateIcon verifies template flag is set.
func TestSetTemplateIcon(t *testing.T) {
	stubAllNative(t)
	var gotTemplate bool
	swapHook(t, &callNativeSetIcon, func(_ []byte, tmpl bool) { gotTemplate = tmpl })
	SetTemplateIcon([]byte{1}, nil)
	if !gotTemplate {
		t.Fatal("SetTemplateIcon should set template=true")
	}
}

// TestSetTooltip verifies forwarding to native tooltip.
func TestSetTooltip(t *testing.T) {
	stubAllNative(t)
	var got string
	swapHook(t, &callNativeSetTooltip, func(s string) { got = s })
	SetTooltip("hello")
	if got != "hello" {
		t.Fatalf("SetTooltip: got %q, want %q", got, "hello")
	}
}

// TestSetOnTapped verifies callback registration and native forwarding.
func TestSetOnTapped(t *testing.T) {
	stubAllNative(t)
	defer func() {
		tappedMu.Lock()
		onTapped = nil
		tappedMu.Unlock()
	}()

	var gotEnabled bool
	swapHook(t, &callNativeSetOnTapped, func(e bool) { gotEnabled = e })

	SetOnTapped(func() {})
	if !gotEnabled {
		t.Fatal("SetOnTapped(fn) should call native with true")
	}

	SetOnTapped(nil)
	if gotEnabled {
		t.Fatal("SetOnTapped(nil) should call native with false")
	}
}

// TestAddMenuItem verifies menu item creation and registration.
func TestAddMenuItem(t *testing.T) {
	stubAllNative(t)
	var addedID uint32
	swapHook(t, &callNativeAddMenuItem, func(id uint32, _, _ string, _, _ bool) { addedID = id })

	item := AddMenuItem("Open", "Open the app")
	if item == nil || item.title != "Open" || item.tooltip != "Open the app" {
		t.Fatalf("AddMenuItem returned unexpected item: %+v", item)
	}
	if item.checkable || item.checked {
		t.Fatal("regular menu item should not be checkable or checked")
	}
	if addedID != item.id {
		t.Fatalf("native called with id=%d, want %d", addedID, item.id)
	}

	// Cleanup
	menuItemsLock.Lock()
	delete(menuItems, item.id)
	menuItemsLock.Unlock()
}

// TestAddMenuItemCheckbox verifies checkbox menu item creation.
func TestAddMenuItemCheckbox(t *testing.T) {
	stubAllNative(t)
	item := AddMenuItemCheckbox("Dark Mode", "Toggle dark mode", true)
	if !item.checkable || !item.checked {
		t.Fatal("checkbox item should be checkable and checked")
	}
	menuItemsLock.Lock()
	delete(menuItems, item.id)
	menuItemsLock.Unlock()
}

// TestAddSeparator verifies the separator native call is made.
func TestAddSeparator(t *testing.T) {
	stubAllNative(t)
	called := false
	swapHook(t, &callNativeAddSeparator, func(_ uint32) { called = true })
	AddSeparator()
	if !called {
		t.Fatal("AddSeparator should call native")
	}
}

// TestQuit verifies native quit is called.
func TestQuit(t *testing.T) {
	stubAllNative(t)
	called := false
	swapHook(t, &callNativeQuit, func() { called = true })
	Quit()
	if !called {
		t.Fatal("Quit should call native")
	}
}

// TestMenuItem_SetTitle verifies title update and native forwarding.
func TestMenuItem_SetTitle(t *testing.T) {
	stubAllNative(t)
	item := AddMenuItem("Old", "")
	defer func() {
		menuItemsLock.Lock()
		delete(menuItems, item.id)
		menuItemsLock.Unlock()
	}()

	var gotTitle string
	swapHook(t, &callNativeSetItemTitle, func(_ uint32, title string) { gotTitle = title })
	item.SetTitle("New")
	if item.title != "New" || gotTitle != "New" {
		t.Fatalf("SetTitle: field=%q, native=%q", item.title, gotTitle)
	}
}

// TestMenuItem_DisableEnable verifies disable/enable state and native calls.
func TestMenuItem_DisableEnable(t *testing.T) {
	stubAllNative(t)
	item := AddMenuItem("Item", "")
	defer func() {
		menuItemsLock.Lock()
		delete(menuItems, item.id)
		menuItemsLock.Unlock()
	}()

	var gotEnabled bool
	swapHook(t, &callNativeSetEnabled, func(_ uint32, e bool) { gotEnabled = e })

	item.Disable()
	if !item.disabled || gotEnabled {
		t.Fatal("Disable should set disabled=true, native enabled=false")
	}

	item.Enable()
	if item.disabled || !gotEnabled {
		t.Fatal("Enable should set disabled=false, native enabled=true")
	}
}

// TestMenuItem_CheckUncheck verifies check/uncheck state and native calls.
func TestMenuItem_CheckUncheck(t *testing.T) {
	stubAllNative(t)
	item := AddMenuItem("Item", "")
	defer func() {
		menuItemsLock.Lock()
		delete(menuItems, item.id)
		menuItemsLock.Unlock()
	}()

	var gotChecked bool
	swapHook(t, &callNativeSetChecked, func(_ uint32, c bool) { gotChecked = c })

	item.Check()
	if !item.Checked() || !gotChecked {
		t.Fatal("Check should set checked=true")
	}

	item.Uncheck()
	if item.Checked() || gotChecked {
		t.Fatal("Uncheck should set checked=false")
	}
}
