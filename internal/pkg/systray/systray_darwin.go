package systray

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa

#include <stdlib.h>

void tray_init(void);
void tray_set_icon(const void *data, int length, int isTemplate);
void tray_set_tooltip(const char *s);
void tray_add_item(int itemID, const char *title, const char *tooltip,
                   int checkable, int checked);
void tray_add_separator(void);
void tray_set_item_title(int itemID, const char *title);
void tray_set_item_enabled(int itemID, int enabled);
void tray_set_item_checked(int itemID, int checked);
void tray_quit(void);
*/
import "C"
import "unsafe"

// goMenuItemClicked is the cgo-exported callback invoked by the Objective-C
// NSMenu action handler when a menu item is selected.
//
//export goMenuItemClicked
func goMenuItemClicked(itemID C.int) {
	menuItemClicked(uint32(itemID))
}

// nativeStart initializes the NSStatusItem via Objective-C and fires readyCb
// on a separate goroutine so the caller is not blocked by AppKit setup.
func nativeStart() {
	C.tray_init()
	if readyCb != nil {
		go readyCb()
	}
}

// nativeEnd is a no-op on macOS; the NSStatusItem is released by tray_quit.
func nativeEnd() {}

func nativeSetIcon(data []byte, isTemplate bool) {
	if len(data) == 0 {
		return
	}
	t := C.int(0)
	if isTemplate {
		t = 1
	}
	C.tray_set_icon(unsafe.Pointer(&data[0]), C.int(len(data)), t)
}

func nativeSetTooltip(s string) {
	cs := C.CString(s)
	defer C.free(unsafe.Pointer(cs))
	C.tray_set_tooltip(cs)
}

func nativeAddMenuItem(id uint32, title, tooltip string, checkable, checked bool) {
	ct := C.CString(title)
	defer C.free(unsafe.Pointer(ct))
	ctt := C.CString(tooltip)
	defer C.free(unsafe.Pointer(ctt))
	ck, cc := C.int(0), C.int(0)
	if checkable {
		ck = 1
	}
	if checked {
		cc = 1
	}
	C.tray_add_item(C.int(id), ct, ctt, ck, cc)
}

func nativeAddSeparator(_ uint32) {
	C.tray_add_separator()
}

func nativeSetItemTitle(id uint32, title string) {
	ct := C.CString(title)
	defer C.free(unsafe.Pointer(ct))
	C.tray_set_item_title(C.int(id), ct)
}

func nativeSetItemEnabled(id uint32, enabled bool) {
	e := C.int(0)
	if enabled {
		e = 1
	}
	C.tray_set_item_enabled(C.int(id), e)
}

func nativeSetItemChecked(id uint32, checked bool) {
	c := C.int(0)
	if checked {
		c = 1
	}
	C.tray_set_item_checked(C.int(id), c)
}

func nativeQuit() {
	C.tray_quit()
}

// nativeSetOnTapped is a no-op on macOS; left-click always opens the menu
// because NSStatusItem does not distinguish left/right click natively.
func nativeSetOnTapped(_ bool) {}
