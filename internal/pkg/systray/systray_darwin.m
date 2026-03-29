#import <Cocoa/Cocoa.h>

extern void goMenuItemClicked(int itemID);

// WFTrayDelegate handles menu item actions and routes them to Go.
@interface WFTrayDelegate : NSObject
@end

static NSStatusItem *_statusItem = nil;
static NSMenu *_menu = nil;
static WFTrayDelegate *_delegate = nil;

@implementation WFTrayDelegate
- (void)menuItemAction:(id)sender {
	NSMenuItem *item = (NSMenuItem *)sender;
	goMenuItemClicked((int)item.tag);
}
@end

void tray_init(void) {
	void (^block)(void) = ^{
		_delegate = [[WFTrayDelegate alloc] init];
		_statusItem = [[NSStatusBar systemStatusBar]
			statusItemWithLength:NSVariableStatusItemLength];
		_menu = [[NSMenu alloc] init];
		[_menu setAutoenablesItems:NO];
		_statusItem.menu = _menu;
	};
	if ([NSThread isMainThread]) {
		block();
	} else {
		dispatch_sync(dispatch_get_main_queue(), block);
	}
}

void tray_set_icon(const void *data, int length, int isTemplate) {
	NSData *imgData = [NSData dataWithBytes:data length:length];
	BOOL tmpl = (isTemplate != 0);
	dispatch_async(dispatch_get_main_queue(), ^{
		if (!_statusItem) return;
		NSImage *img = [[NSImage alloc] initWithData:imgData];
		[img setSize:NSMakeSize(16, 16)];
		[img setTemplate:tmpl];
		_statusItem.button.image = img;
	});
}

void tray_set_tooltip(const char *s) {
	NSString *str = [NSString stringWithUTF8String:s];
	dispatch_async(dispatch_get_main_queue(), ^{
		if (!_statusItem) return;
		_statusItem.button.toolTip = str;
	});
}

void tray_add_item(int itemID, const char *title, const char *tooltip,
				   int checkable, int checked) {
	NSString *t = [NSString stringWithUTF8String:title];
	NSString *tt = (tooltip && tooltip[0])
		? [NSString stringWithUTF8String:tooltip] : @"";
	BOOL ck = (checkable && checked);
	dispatch_async(dispatch_get_main_queue(), ^{
		if (!_menu) return;
		NSMenuItem *item = [[NSMenuItem alloc]
			initWithTitle:t
				   action:@selector(menuItemAction:)
			keyEquivalent:@""];
		item.target = _delegate;
		item.tag = itemID;
		if (tt.length > 0) {
			item.toolTip = tt;
		}
		if (ck) {
			[item setState:NSControlStateValueOn];
		}
		[_menu addItem:item];
	});
}

void tray_add_separator(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		if (!_menu) return;
		[_menu addItem:[NSMenuItem separatorItem]];
	});
}

static NSMenuItem* _find_item(int itemID) {
	if (!_menu) return nil;
	for (NSMenuItem *item in _menu.itemArray) {
		if (item.tag == itemID) return item;
	}
	return nil;
}

void tray_set_item_title(int itemID, const char *title) {
	NSString *t = [NSString stringWithUTF8String:title];
	dispatch_async(dispatch_get_main_queue(), ^{
		NSMenuItem *item = _find_item(itemID);
		if (item) item.title = t;
	});
}

void tray_set_item_enabled(int itemID, int enabled) {
	BOOL en = (enabled != 0);
	dispatch_async(dispatch_get_main_queue(), ^{
		NSMenuItem *item = _find_item(itemID);
		if (item) [item setEnabled:en];
	});
}

void tray_set_item_checked(int itemID, int checked) {
	NSInteger state = checked ? NSControlStateValueOn : NSControlStateValueOff;
	dispatch_async(dispatch_get_main_queue(), ^{
		NSMenuItem *item = _find_item(itemID);
		if (item) [item setState:state];
	});
}

void tray_quit(void) {
	void (^block)(void) = ^{
		if (_statusItem) {
			[[NSStatusBar systemStatusBar] removeStatusItem:_statusItem];
			_statusItem = nil;
		}
		_menu = nil;
		_delegate = nil;
	};
	if ([NSThread isMainThread]) {
		block();
	} else {
		dispatch_sync(dispatch_get_main_queue(), block);
	}
}
