package systray

import "testing"

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
