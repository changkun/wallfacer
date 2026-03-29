package systray

import (
	"fmt"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	pRegisterClassEx       = user32.NewProc("RegisterClassExW")
	pCreateWindowEx        = user32.NewProc("CreateWindowExW")
	pDefWindowProc         = user32.NewProc("DefWindowProcW")
	pGetMessage            = user32.NewProc("GetMessageW")
	pTranslateMessage      = user32.NewProc("TranslateMessage")
	pDispatchMessage       = user32.NewProc("DispatchMessageW")
	pPostMessage           = user32.NewProc("PostMessageW")
	pPostQuitMessage       = user32.NewProc("PostQuitMessage")
	pCreatePopupMenu       = user32.NewProc("CreatePopupMenu")
	pInsertMenuItem        = user32.NewProc("InsertMenuItemW")
	pGetMenuItemInfo       = user32.NewProc("GetMenuItemInfoW")
	pSetMenuItemInfo       = user32.NewProc("SetMenuItemInfoW")
	pTrackPopupMenuEx      = user32.NewProc("TrackPopupMenuEx")
	pDestroyMenu           = user32.NewProc("DestroyMenu")
	pSetForegroundWindow   = user32.NewProc("SetForegroundWindow")
	pGetCursorPos          = user32.NewProc("GetCursorPos")
	pLoadImage             = user32.NewProc("LoadImageW")
	pDestroyIcon           = user32.NewProc("DestroyIcon")
	pRegisterWindowMessage = user32.NewProc("RegisterWindowMessageW")

	pShellNotifyIcon = shell32.NewProc("Shell_NotifyIconW")
	pGetModuleHandle = kernel32.NewProc("GetModuleHandleW")
)

const (
	// Standard Windows messages.
	wmClose   = 0x0010
	wmDestroy = 0x0002
	wmCommand = 0x0111

	// Application-defined messages. wmTray receives Shell_NotifyIcon callbacks;
	// wmRunOp is posted to drain the deferred operation queue on the UI thread.
	wmApp   = 0x8000
	wmTray  = wmApp + 1
	wmRunOp = wmApp + 2

	wmLButtonUp = 0x0202
	wmRButtonUp = 0x0205

	nimAdd    = 0
	nimModify = 1
	nimDelete = 2

	nifMessage = 0x01
	nifIcon    = 0x02
	nifTip     = 0x04

	imageIcon      = 1
	lrLoadFromFile = 0x0010
	lrDefaultSize  = 0x0040

	miimState  = 0x0001
	miimID     = 0x0002
	miimString = 0x0040
	miimFType  = 0x0100

	mftString    = 0x0000
	mftSeparator = 0x0800

	mfsEnabled  = 0x0000
	mfsDisabled = 0x0003
	mfsChecked  = 0x0008

	tpmLeftAlign   = 0x0000
	tpmBottomAlign = 0x0020
)

type point struct{ x, y int32 }

type winMsg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      point
}

type notifyIconData struct {
	cbSize           uint32
	hWnd             uintptr
	uID              uint32
	uFlags           uint32
	uCallbackMessage uint32
	hIcon            uintptr
	szTip            [128]uint16
	dwState          uint32
	dwStateMask      uint32
	szInfo           [256]uint16
	uVersion         uint32
	szInfoTitle      [64]uint16
	dwInfoFlags      uint32
	guidItem         [16]byte
	hBalloonIcon     uintptr
}

type menuItemInfoW struct {
	cbSize        uint32
	fMask         uint32
	fType         uint32
	fState        uint32
	wID           uint32
	hSubMenu      uintptr
	hbmpChecked   uintptr
	hbmpUnchecked uintptr
	dwItemData    uintptr
	dwTypeData    *uint16
	cch           uint32
	hbmpItem      uintptr
}

type wndClassExW struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  uintptr
	lpszClassName *uint16
	hIconSm       uintptr
}

var (
	winHWND          uintptr
	winMenu          uintptr
	winNID           notifyIconData
	winIcon          uintptr
	winReady         = make(chan struct{})
	winReadyFlag     atomic.Uint32
	winOpsMu         sync.Mutex
	winOps           []func()
	winItemCount     uint32
	wmTaskbarCreated uintptr
)

// nativeStart creates a hidden message-only window on a dedicated OS thread
// (required by Win32) and registers the Shell_NotifyIcon. All subsequent
// native operations are marshalled to this thread via postOp + wmRunOp.
func nativeStart() {
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		hInstance, _, _ := pGetModuleHandle.Call(0)
		className, _ := windows.UTF16PtrFromString("WallfacerSystray")

		wc := wndClassExW{
			lpfnWndProc:   syscall.NewCallback(wndProc),
			hInstance:     hInstance,
			lpszClassName: className,
		}
		wc.cbSize = uint32(unsafe.Sizeof(wc))
		pRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc)))

		windowName, _ := windows.UTF16PtrFromString("WallfacerTray")
		winHWND, _, _ = pCreateWindowEx.Call(
			0,
			uintptr(unsafe.Pointer(className)),
			uintptr(unsafe.Pointer(windowName)),
			0, 0, 0, 0, 0, 0, 0, hInstance, 0,
		)

		// Register TaskbarCreated message for explorer.exe restart recovery.
		tbcName, _ := windows.UTF16PtrFromString("TaskbarCreated")
		wmTaskbarCreated, _, _ = pRegisterWindowMessage.Call(uintptr(unsafe.Pointer(tbcName)))

		winMenu, _, _ = pCreatePopupMenu.Call()

		winNID = notifyIconData{
			hWnd:             winHWND,
			uID:              1,
			uFlags:           nifMessage | nifTip,
			uCallbackMessage: wmTray,
		}
		winNID.cbSize = uint32(unsafe.Sizeof(winNID))
		copy(winNID.szTip[:], windows.StringToUTF16("Wallfacer"))
		pShellNotifyIcon.Call(nimAdd, uintptr(unsafe.Pointer(&winNID)))

		close(winReady)
		winReadyFlag.Store(1)

		// Flush any ops queued before the window was ready.
		winOpsMu.Lock()
		ops := winOps
		winOps = nil
		winOpsMu.Unlock()
		for _, op := range ops {
			op()
		}

		if readyCb != nil {
			go readyCb()
		}

		var m winMsg
		for {
			ret, _, _ := pGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
			if ret == 0 || int32(ret) == -1 {
				break
			}
			pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
			pDispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
		}
	}()
}

// wndProc is the Win32 window procedure for the hidden tray window.
// It handles tray icon callbacks (wmTray), menu item clicks (wmCommand),
// deferred operation dispatch (wmRunOp), and explorer.exe restart recovery.
func wndProc(hwnd, message, wParam, lParam uintptr) uintptr {
	switch message {
	case wmTray:
		// lParam low word contains the mouse message that triggered the callback.
		switch lParam & 0xFFFF {
		case wmLButtonUp:
			tappedMu.Lock()
			fn := onTapped
			tappedMu.Unlock()
			if fn != nil {
				go trayTapped()
			} else {
				showPopupMenu()
			}
		case wmRButtonUp:
			showPopupMenu()
		}
		return 0
	case wmCommand:
		id := uint32(wParam & 0xFFFF)
		go menuItemClicked(id)
		return 0
	case wmRunOp:
		winOpsMu.Lock()
		ops := winOps
		winOps = nil
		winOpsMu.Unlock()
		for _, op := range ops {
			op()
		}
		return 0
	case wmDestroy:
		pShellNotifyIcon.Call(nimDelete, uintptr(unsafe.Pointer(&winNID)))
		if winMenu != 0 {
			pDestroyMenu.Call(winMenu)
		}
		if winIcon != 0 {
			pDestroyIcon.Call(winIcon)
		}
		pPostQuitMessage.Call(0)
		return 0
	default:
		// When explorer.exe restarts (e.g. after a crash), it broadcasts
		// "TaskbarCreated". Re-add our icon so it reappears in the tray.
		if message == wmTaskbarCreated && wmTaskbarCreated != 0 {
			pShellNotifyIcon.Call(nimAdd, uintptr(unsafe.Pointer(&winNID)))
			return 0
		}
	}
	ret, _, _ := pDefWindowProc.Call(hwnd, message, wParam, lParam)
	return ret
}

func showPopupMenu() {
	var pt point
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	pSetForegroundWindow.Call(winHWND)
	pTrackPopupMenuEx.Call(winMenu, tpmLeftAlign|tpmBottomAlign,
		uintptr(pt.x), uintptr(pt.y), winHWND, 0)
}

// postOp enqueues fn for execution on the Windows message loop thread.
// If the window is already created, it posts a wmRunOp message to wake the
// loop. Otherwise the ops accumulate and are flushed once nativeStart
// finishes creating the hidden window.
func postOp(fn func()) {
	winOpsMu.Lock()
	winOps = append(winOps, fn)
	winOpsMu.Unlock()
	if winReadyFlag.Load() == 1 {
		pPostMessage.Call(winHWND, wmRunOp, 0, 0)
	}
}

func nativeEnd() {}

func nativeSetIcon(data []byte, _ bool) {
	if len(data) == 0 {
		return
	}
	// Only handle ICO format (magic: 00 00 01 00).
	if len(data) < 4 || data[0] != 0 || data[1] != 0 || data[2] != 1 || data[3] != 0 {
		return
	}
	postOp(func() {
		icon, err := loadICO(data)
		if err != nil || icon == 0 {
			return
		}
		if winIcon != 0 {
			pDestroyIcon.Call(winIcon)
		}
		winIcon = icon
		winNID.hIcon = icon
		winNID.uFlags |= nifIcon
		pShellNotifyIcon.Call(nimModify, uintptr(unsafe.Pointer(&winNID)))
	})
}

// loadICO writes ICO data to a temp file and loads it via LoadImage.
// Win32 LoadImage requires a file path; there is no in-memory alternative.
func loadICO(data []byte) (uintptr, error) {
	f, err := os.CreateTemp("", "wallfacer-*.ico")
	if err != nil {
		return 0, err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err := f.Write(data); err != nil {
		f.Close()
		return 0, err
	}
	f.Close()

	path, _ := windows.UTF16PtrFromString(name)
	h, _, e := pLoadImage.Call(0, uintptr(unsafe.Pointer(path)),
		imageIcon, 0, 0, lrLoadFromFile|lrDefaultSize)
	if h == 0 {
		return 0, fmt.Errorf("LoadImage: %w", e)
	}
	return h, nil
}

func nativeSetTooltip(s string) {
	postOp(func() {
		tip := windows.StringToUTF16(s)
		copy(winNID.szTip[:], tip)
		winNID.uFlags |= nifTip
		pShellNotifyIcon.Call(nimModify, uintptr(unsafe.Pointer(&winNID)))
	})
}

func nativeAddMenuItem(id uint32, title, _ string, checkable, checked bool) {
	postOp(func() {
		tp, _ := windows.UTF16PtrFromString(title)
		mii := menuItemInfoW{
			fMask:      miimID | miimFType | miimString | miimState,
			fType:      mftString,
			wID:        id,
			dwTypeData: tp,
			cch:        uint32(len(title)),
		}
		mii.cbSize = uint32(unsafe.Sizeof(mii))
		if checkable && checked {
			mii.fState |= mfsChecked
		}
		winItemCount++
		pInsertMenuItem.Call(winMenu, uintptr(winItemCount-1), 1,
			uintptr(unsafe.Pointer(&mii)))
	})
}

func nativeAddSeparator(_ uint32) {
	postOp(func() {
		mii := menuItemInfoW{
			fMask: miimFType,
			fType: mftSeparator,
		}
		mii.cbSize = uint32(unsafe.Sizeof(mii))
		winItemCount++
		pInsertMenuItem.Call(winMenu, uintptr(winItemCount-1), 1,
			uintptr(unsafe.Pointer(&mii)))
	})
}

func nativeSetItemTitle(id uint32, title string) {
	postOp(func() {
		tp, _ := windows.UTF16PtrFromString(title)
		mii := menuItemInfoW{
			fMask:      miimString,
			dwTypeData: tp,
			cch:        uint32(len(title)),
		}
		mii.cbSize = uint32(unsafe.Sizeof(mii))
		pSetMenuItemInfo.Call(winMenu, uintptr(id), 0, uintptr(unsafe.Pointer(&mii)))
	})
}

func nativeSetItemEnabled(id uint32, enabled bool) {
	postOp(func() {
		var mii menuItemInfoW
		mii.cbSize = uint32(unsafe.Sizeof(mii))
		mii.fMask = miimState
		pGetMenuItemInfo.Call(winMenu, uintptr(id), 0, uintptr(unsafe.Pointer(&mii)))
		if enabled {
			mii.fState &^= mfsDisabled
		} else {
			mii.fState |= mfsDisabled
		}
		pSetMenuItemInfo.Call(winMenu, uintptr(id), 0, uintptr(unsafe.Pointer(&mii)))
	})
}

func nativeSetItemChecked(id uint32, checked bool) {
	postOp(func() {
		var mii menuItemInfoW
		mii.cbSize = uint32(unsafe.Sizeof(mii))
		mii.fMask = miimState
		pGetMenuItemInfo.Call(winMenu, uintptr(id), 0, uintptr(unsafe.Pointer(&mii)))
		if checked {
			mii.fState |= mfsChecked
		} else {
			mii.fState &^= mfsChecked
		}
		pSetMenuItemInfo.Call(winMenu, uintptr(id), 0, uintptr(unsafe.Pointer(&mii)))
	})
}

func nativeQuit() {
	if winReadyFlag.Load() == 1 {
		pPostMessage.Call(winHWND, wmClose, 0, 0)
	}
}

func nativeSetOnTapped(_ bool) {}
