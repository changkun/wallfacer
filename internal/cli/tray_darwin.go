//go:build desktop && darwin

package cli

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>
#import <objc/runtime.h>

extern void goDockReopenCallback(void);

// Swizzle the Wails AppDelegate to handle dock icon clicks.
// Called from OnStartup after the delegate is set.
static void installDockHandler(void) {
    // Delay to the next main-loop iteration so the Wails AppDelegate
    // is fully installed.
    dispatch_async(dispatch_get_main_queue(), ^{
        NSApplication *app = [NSApplication sharedApplication];
        id delegate = [app delegate];
        if (!delegate) return;

        Class cls = object_getClass(delegate);
        SEL sel = @selector(applicationShouldHandleReopen:hasVisibleWindows:);

        // Add (or replace) the reopen handler on the delegate's class.
        class_replaceMethod(cls, sel,
            imp_implementationWithBlock(^BOOL(id self, NSApplication *sender, BOOL hasVisibleWindows) {
                if (!hasVisibleWindows) {
                    goDockReopenCallback();
                }
                return YES;
            }), "B@:@B");
    });
}
*/
import "C"

var dockReopenFn func()

//export goDockReopenCallback
func goDockReopenCallback() {
	if dockReopenFn != nil {
		dockReopenFn()
	}
}

// installDockReopenHandler installs a handler on the Wails AppDelegate so
// that clicking the dock icon with no visible windows shows the main window.
func installDockReopenHandler(showWindow func()) {
	dockReopenFn = showWindow
	C.installDockHandler()
}

// platformTraySetup performs macOS-specific tray initialization.
// On macOS, both left-click and right-click open the menu (default behavior).
// We do not register SetOnTapped so that left-click shows the menu
// instead of directly opening the window.
func platformTraySetup(showWindow func()) {
	// No SetOnTapped — left-click should show the tray menu, not the window.
}

// macOS tray behavior notes:
//
// - Template icons: SetTemplateIcon is used for all icon states (idle,
//   active, attention) so macOS auto-adapts the icon for light/dark menu bar.
// - Close hides: Cmd+W hides the window via explicit menu item; HideWindowOnClose
//   is also set to true in desktop.go for darwin.
// - Cmd+Q: Overridden via custom app menu to route through doShutdown(),
//   showing the shutdown overlay while draining tasks.
// - Dock icon click: applicationShouldHandleReopen:hasVisibleWindows: is
//   installed on the Wails AppDelegate to show the window on dock click.
