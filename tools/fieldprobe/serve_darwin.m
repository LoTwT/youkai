// serve_darwin.m — GUI bootstrap for the field-test driver.
//
// SCStream creation asserts in CGS_REQUIRE_INIT (CoreGraphics
// Services) unless the process has initialized AppKit/CGS first — a
// plain command-line binary has not. The real youkai app is an MyGo
// NSApplication process, so this is a driver-side requirement, not a
// platform-package one. NSApplicationLoad() performs the process-wide
// initialization; a running main NSRunLoop then services both the GCD
// main queue (NSWorkspace reads, Carbon hotkey registration) and the
// AppKit/Carbon event sources (hotkey delivery).
#import <AppKit/AppKit.h>
#import <CoreGraphics/CoreGraphics.h>
#import <dispatch/dispatch.h>

void fieldprobe_init_gui(void) {
	NSApplicationLoad();
}

void fieldprobe_serve_main_loop(void) {
	// Mirror the host-app loop MyGo provides (mygo.go: nextEventMatchingMask
	// + sendEvent on the main thread). A bare -[NSRunLoop runMode:] loop is
	// not guaranteed to dispatch Carbon hotkey events, and this loop also
	// drains the GCD main queue in common modes (frontmost queries). The
	// accessory activation policy keeps the probe out of the Dock and,
	// critically, stops each invocation from stealing focus from the game.
	[NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
	while (true) {
		NSEvent *event = [NSApp nextEventMatchingMask:NSEventMaskAny
		                                    untilDate:[NSDate distantFuture]
		                                       inMode:NSDefaultRunLoopMode
		                                       dequeue:YES];
		if (event != nil) {
			[NSApp sendEvent:event];
		}
	}
}

// Cursor-position diagnostic. NSEvent.mouseLocation is AppKit's global
// cursor position with a bottom-left origin; flip it into the
// top-left-origin screen-point space that SCWindow.frame and CGEvent
// mouse coordinates use (relative to the primary display height).
static void fieldprobe_cursor_pos_on_main(double *x, double *y) {
	NSPoint p = [NSEvent mouseLocation];
	CGRect main = CGDisplayBounds(CGMainDisplayID());
	*x = p.x;
	*y = main.size.height - p.y;
}

int fieldprobe_cursor_pos(double timeoutSec, double *x, double *y) {
	*x = 0;
	*y = 0;
	if ([NSThread isMainThread]) {
		fieldprobe_cursor_pos_on_main(x, y);
		return 1;
	}
	dispatch_semaphore_t done = dispatch_semaphore_create(0);
	dispatch_async(dispatch_get_main_queue(), ^{
		fieldprobe_cursor_pos_on_main(x, y);
		dispatch_semaphore_signal(done);
	});
	dispatch_time_t deadline =
	    dispatch_time(DISPATCH_TIME_NOW, (int64_t)(timeoutSec * NSEC_PER_SEC));
	if (dispatch_semaphore_wait(done, deadline) != 0) {
		// No main run loop served the query within the bound.
		return 0;
	}
	return 1;
}
