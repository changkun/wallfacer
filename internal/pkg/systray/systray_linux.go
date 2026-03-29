package systray

import (
	"bytes"
	"fmt"
	"image"
	_ "image/png" // register PNG decoder for pngToARGB
	"os"
	"sync"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
)

// instance holds all mutable state for the Linux D-Bus system tray.
// All fields are protected by mu except conn (set once during nativeStart).
var instance struct {
	conn      *dbus.Conn
	busName   string // unique D-Bus name, e.g. org.kde.StatusNotifierItem-<pid>-1
	iconW     int32
	iconH     int32
	iconARGB  []byte // ARGB pixel data for IconPixmap property
	tooltip   string
	items     []linuxMenuItem // ordered menu entries
	menuRev   uint32          // incremented on every menu mutation; signals LayoutUpdated
	mu        sync.Mutex
	hasTapped bool // true when a left-click callback is registered
}

// linuxMenuItem is the Go-side representation of a single menu entry,
// mirrored to D-Bus clients via GetLayout.
type linuxMenuItem struct {
	id        int32
	label     string
	enabled   bool
	separator bool
	checkable bool
	checked   bool
}

// --- StatusNotifierItem D-Bus interface ---
// sniHandler implements the org.kde.StatusNotifierItem D-Bus methods.

type sniHandler struct{}

func (sniHandler) Activate(_, _ int32) *dbus.Error {
	go trayTapped()
	return nil
}

func (sniHandler) SecondaryActivate(_, _ int32) *dbus.Error { return nil }
func (sniHandler) Scroll(_ int32, _ string) *dbus.Error {
	return nil
}

// --- StatusNotifierItem Properties ---
// sniPropsHandler implements org.freedesktop.DBus.Properties for the SNI object.

type sniPropsHandler struct{}

// iconPX matches the D-Bus struct (iiay) expected by the IconPixmap property.
type iconPX struct {
	W, H int32
	Data []byte // ARGB pixel data, network byte order
}

// toolTipVal matches the D-Bus struct (sa(iiay)ss) for the ToolTip property.
type toolTipVal struct {
	IconName string
	IconPix  []iconPX
	Title    string
	Desc     string
}

func (sniPropsHandler) Get(_, prop string) (dbus.Variant, *dbus.Error) {
	instance.mu.Lock()
	defer instance.mu.Unlock()
	v, ok := sniAllProps()[prop]
	if !ok {
		return dbus.Variant{}, nil
	}
	return v, nil
}

func (sniPropsHandler) GetAll(_ string) (map[string]dbus.Variant, *dbus.Error) {
	instance.mu.Lock()
	defer instance.mu.Unlock()
	return sniAllProps(), nil
}

func (sniPropsHandler) Set(string, string, dbus.Variant) *dbus.Error {
	return dbus.NewError("org.freedesktop.DBus.Error.PropertyReadOnly", nil)
}

func sniAllProps() map[string]dbus.Variant {
	props := map[string]dbus.Variant{
		"Category":      dbus.MakeVariant("ApplicationStatus"),
		"Id":            dbus.MakeVariant("wallfacer"),
		"Title":         dbus.MakeVariant("Wallfacer"),
		"Status":        dbus.MakeVariant("Active"),
		"Menu":          dbus.MakeVariant(dbus.ObjectPath("/StatusNotifierItem/menu")),
		"ItemIsMenu":    dbus.MakeVariant(!instance.hasTapped),
		"IconName":      dbus.MakeVariant(""),
		"IconThemePath": dbus.MakeVariant(""),
	}
	if len(instance.iconARGB) > 0 {
		props["IconPixmap"] = dbus.MakeVariant([]iconPX{{instance.iconW, instance.iconH, instance.iconARGB}})
	} else {
		props["IconPixmap"] = dbus.MakeVariant([]iconPX{})
	}
	props["ToolTip"] = dbus.MakeVariant(toolTipVal{Title: instance.tooltip})
	return props
}

// --- DBusMenu interface ---
// dbusMenuHandler implements com.canonical.dbusmenu for the tray's dropdown menu.

type dbusMenuHandler struct{}

// menuLayout matches the DBusMenu layout struct (ia{sv}av): item ID,
// properties map, and child items.
type menuLayout struct {
	V0 int32
	V1 map[string]dbus.Variant
	V2 []dbus.Variant
}

func (dbusMenuHandler) GetLayout(parentID, _ int32, _ []string) (uint32, menuLayout, *dbus.Error) {
	instance.mu.Lock()
	defer instance.mu.Unlock()

	// Only the root (parentID=0) has children; submenus are not supported.
	if parentID != 0 {
		return instance.menuRev, menuLayout{V0: parentID, V1: map[string]dbus.Variant{}, V2: []dbus.Variant{}}, nil
	}

	children := make([]dbus.Variant, 0, len(instance.items))
	for _, item := range instance.items {
		props := make(map[string]dbus.Variant)
		if item.separator {
			props["type"] = dbus.MakeVariant("separator")
		} else {
			props["label"] = dbus.MakeVariant(item.label)
			props["enabled"] = dbus.MakeVariant(item.enabled)
			if item.checkable {
				props["toggle-type"] = dbus.MakeVariant("checkmark")
				state := int32(0)
				if item.checked {
					state = 1
				}
				props["toggle-state"] = dbus.MakeVariant(state)
			}
		}
		children = append(children, dbus.MakeVariant(menuLayout{
			V0: item.id,
			V1: props,
			V2: []dbus.Variant{},
		}))
	}

	return instance.menuRev, menuLayout{
		V0: 0,
		V1: map[string]dbus.Variant{},
		V2: children,
	}, nil
}

type dbusMenuEvent struct {
	ID        int32
	EventID   string
	Data      dbus.Variant
	Timestamp uint32
}

func (dbusMenuHandler) Event(id int32, eventID string, _ dbus.Variant, _ uint32) *dbus.Error {
	if eventID == "clicked" {
		go menuItemClicked(uint32(id))
	}
	return nil
}

func (dbusMenuHandler) EventGroup(events []dbusMenuEvent) ([]int32, *dbus.Error) {
	for _, e := range events {
		if e.EventID == "clicked" {
			go menuItemClicked(uint32(e.ID))
		}
	}
	return nil, nil
}

func (dbusMenuHandler) AboutToShow(int32) (bool, *dbus.Error) { return false, nil }
func (dbusMenuHandler) AboutToShowGroup(_ []int32) ([]int32, []int32, *dbus.Error) {
	return nil, nil, nil
}

func (dbusMenuHandler) GetGroupProperties(_ []int32, _ []string) ([]struct {
	V0 int32
	V1 map[string]dbus.Variant
}, *dbus.Error) {
	return nil, nil
}

func (dbusMenuHandler) GetProperty(_ int32, _ string) (dbus.Variant, *dbus.Error) {
	return dbus.Variant{}, nil
}

// --- DBusMenu Properties ---

type menuPropsHandler struct{}

func (menuPropsHandler) Get(_, prop string) (dbus.Variant, *dbus.Error) {
	switch prop {
	case "Version":
		return dbus.MakeVariant(uint32(3)), nil
	case "TextDirection":
		return dbus.MakeVariant("ltr"), nil
	case "Status":
		return dbus.MakeVariant("normal"), nil
	}
	return dbus.Variant{}, nil
}

func (menuPropsHandler) GetAll(string) (map[string]dbus.Variant, *dbus.Error) {
	return map[string]dbus.Variant{
		"Version":       dbus.MakeVariant(uint32(3)),
		"TextDirection": dbus.MakeVariant("ltr"),
		"Status":        dbus.MakeVariant("normal"),
	}, nil
}

func (menuPropsHandler) Set(string, string, dbus.Variant) *dbus.Error {
	return dbus.NewError("org.freedesktop.DBus.Error.PropertyReadOnly", nil)
}

// --- Native functions ---

func nativeStart() {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		// No D-Bus available; tray won't show but app still works.
		if readyCb != nil {
			go readyCb()
		}
		return
	}
	instance.conn = conn
	instance.busName = fmt.Sprintf("org.kde.StatusNotifierItem-%d-1", os.Getpid())

	_, _ = conn.RequestName(instance.busName, dbus.NameFlagDoNotQueue)

	// Export StatusNotifierItem interface and properties.
	_ = conn.Export(sniHandler{}, "/StatusNotifierItem", "org.kde.StatusNotifierItem")
	_ = conn.Export(sniPropsHandler{}, "/StatusNotifierItem", "org.freedesktop.DBus.Properties")

	// Export DBusMenu interface and properties.
	_ = conn.Export(dbusMenuHandler{}, "/StatusNotifierItem/menu", "com.canonical.dbusmenu")
	_ = conn.Export(menuPropsHandler{}, "/StatusNotifierItem/menu", "org.freedesktop.DBus.Properties")

	// Introspection for StatusNotifierItem.
	sniIntro := introspect.Node{
		Interfaces: []introspect.Interface{
			{
				Name: "org.kde.StatusNotifierItem",
				Methods: []introspect.Method{
					{Name: "Activate", Args: []introspect.Arg{
						{Name: "x", Type: "i", Direction: "in"},
						{Name: "y", Type: "i", Direction: "in"},
					}},
					{Name: "SecondaryActivate", Args: []introspect.Arg{
						{Name: "x", Type: "i", Direction: "in"},
						{Name: "y", Type: "i", Direction: "in"},
					}},
					{Name: "Scroll", Args: []introspect.Arg{
						{Name: "delta", Type: "i", Direction: "in"},
						{Name: "orientation", Type: "s", Direction: "in"},
					}},
				},
				Signals: []introspect.Signal{
					{Name: "NewIcon"},
					{Name: "NewToolTip"},
					{Name: "NewStatus", Args: []introspect.Arg{{Name: "status", Type: "s"}}},
				},
			},
			introspect.IntrospectData,
		},
	}
	conn.Export(introspect.NewIntrospectable(&sniIntro), "/StatusNotifierItem",
		"org.freedesktop.DBus.Introspectable")

	// Introspection for DBusMenu.
	menuIntro := introspect.Node{
		Interfaces: []introspect.Interface{
			{
				Name: "com.canonical.dbusmenu",
				Methods: []introspect.Method{
					{Name: "GetLayout", Args: []introspect.Arg{
						{Name: "parentId", Type: "i", Direction: "in"},
						{Name: "recursionDepth", Type: "i", Direction: "in"},
						{Name: "propertyNames", Type: "as", Direction: "in"},
						{Name: "revision", Type: "u", Direction: "out"},
						{Name: "layout", Type: "(ia{sv}av)", Direction: "out"},
					}},
					{Name: "Event", Args: []introspect.Arg{
						{Name: "id", Type: "i", Direction: "in"},
						{Name: "eventId", Type: "s", Direction: "in"},
						{Name: "data", Type: "v", Direction: "in"},
						{Name: "timestamp", Type: "u", Direction: "in"},
					}},
					{Name: "AboutToShow", Args: []introspect.Arg{
						{Name: "id", Type: "i", Direction: "in"},
						{Name: "needUpdate", Type: "b", Direction: "out"},
					}},
				},
				Signals: []introspect.Signal{
					{Name: "LayoutUpdated", Args: []introspect.Arg{
						{Name: "revision", Type: "u"},
						{Name: "parent", Type: "i"},
					}},
				},
			},
			introspect.IntrospectData,
		},
	}
	conn.Export(introspect.NewIntrospectable(&menuIntro), "/StatusNotifierItem/menu",
		"org.freedesktop.DBus.Introspectable")

	// Register with the StatusNotifierWatcher.
	obj := conn.Object("org.kde.StatusNotifierWatcher", "/StatusNotifierWatcher")
	obj.Call("org.kde.StatusNotifierWatcher.RegisterStatusNotifierItem", 0, instance.busName)

	if readyCb != nil {
		go readyCb()
	}
}

func nativeEnd() {}

// nativeSetIcon converts PNG data to ARGB pixel format and updates the
// StatusNotifierItem icon. The isTemplate parameter is ignored on Linux.
func nativeSetIcon(data []byte, _ bool) {
	if len(data) == 0 {
		return
	}
	w, h, argb, err := pngToARGB(data)
	if err != nil {
		return
	}
	instance.mu.Lock()
	instance.iconW = w
	instance.iconH = h
	instance.iconARGB = argb
	conn := instance.conn
	instance.mu.Unlock()

	if conn != nil {
		_ = conn.Emit("/StatusNotifierItem", "org.kde.StatusNotifierItem.NewIcon")
	}
}

func nativeSetTooltip(s string) {
	instance.mu.Lock()
	instance.tooltip = s
	conn := instance.conn
	instance.mu.Unlock()

	if conn != nil {
		_ = conn.Emit("/StatusNotifierItem", "org.kde.StatusNotifierItem.NewToolTip")
	}
}

func nativeAddMenuItem(id uint32, title, _ string, checkable, checked bool) {
	instance.mu.Lock()
	instance.items = append(instance.items, linuxMenuItem{
		id:        int32(id),
		label:     title,
		enabled:   true,
		checkable: checkable,
		checked:   checked,
	})
	instance.menuRev++
	instance.mu.Unlock()
	emitMenuUpdate()
}

func nativeAddSeparator(id uint32) {
	instance.mu.Lock()
	instance.items = append(instance.items, linuxMenuItem{
		id:        int32(id),
		separator: true,
	})
	instance.menuRev++
	instance.mu.Unlock()
	emitMenuUpdate()
}

func nativeSetItemTitle(id uint32, title string) {
	instance.mu.Lock()
	for i := range instance.items {
		if instance.items[i].id == int32(id) {
			instance.items[i].label = title
			break
		}
	}
	instance.menuRev++
	instance.mu.Unlock()
	emitMenuUpdate()
}

func nativeSetItemEnabled(id uint32, enabled bool) {
	instance.mu.Lock()
	for i := range instance.items {
		if instance.items[i].id == int32(id) {
			instance.items[i].enabled = enabled
			break
		}
	}
	instance.menuRev++
	instance.mu.Unlock()
	emitMenuUpdate()
}

func nativeSetItemChecked(id uint32, checked bool) {
	instance.mu.Lock()
	for i := range instance.items {
		if instance.items[i].id == int32(id) {
			instance.items[i].checked = checked
			break
		}
	}
	instance.menuRev++
	instance.mu.Unlock()
	emitMenuUpdate()
}

func nativeQuit() {
	instance.mu.Lock()
	conn := instance.conn
	instance.conn = nil
	instance.mu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
}

func nativeSetOnTapped(hasTapped bool) {
	instance.mu.Lock()
	instance.hasTapped = hasTapped
	instance.mu.Unlock()
}

// emitMenuUpdate emits the com.canonical.dbusmenu.LayoutUpdated signal so
// desktop environments (KDE, GNOME+AppIndicator) refresh the popup menu.
func emitMenuUpdate() {
	instance.mu.Lock()
	conn := instance.conn
	rev := instance.menuRev
	instance.mu.Unlock()
	if conn != nil {
		_ = conn.Emit("/StatusNotifierItem/menu", "com.canonical.dbusmenu.LayoutUpdated", rev, int32(0))
	}
}

// pngToARGB decodes a PNG image into ARGB pixel format for the
// StatusNotifierItem IconPixmap property. The output byte order is
// ARGB (alpha first), which the D-Bus spec requires in network byte order.
func pngToARGB(data []byte) (w, h int32, argb []byte, err error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return 0, 0, nil, err
	}
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	argb = make([]byte, width*height*4)
	for y := range height {
		for x := range width {
			// RGBA() returns pre-multiplied 16-bit values; shift to 8-bit.
			r, g, b, a := img.At(x+bounds.Min.X, y+bounds.Min.Y).RGBA()
			off := (y*width + x) * 4
			argb[off+0] = byte(a >> 8)
			argb[off+1] = byte(r >> 8)
			argb[off+2] = byte(g >> 8)
			argb[off+3] = byte(b >> 8)
		}
	}
	return int32(width), int32(height), argb, nil
}
