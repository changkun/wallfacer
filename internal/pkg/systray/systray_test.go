package systray

import "testing"

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

func TestMenuItemClickedDropsWhenFull(t *testing.T) {
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

func TestMenuItemClickedInvalidID(t *testing.T) {
	// Should not panic.
	menuItemClicked(999999)
}

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

func TestTrayTappedNil(t *testing.T) {
	tappedMu.Lock()
	onTapped = nil
	tappedMu.Unlock()
	// Should not panic.
	trayTapped()
}
