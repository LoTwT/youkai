// youkai macOS bridge — Quartz event synthesis, AX and NSWorkspace
// queries, permission preflights and the mach clock source.
//
// Notes:
//   - Event posting uses CGEventCreate* + CGEventPost on the HID event
//     tap in global display coordinates (points, top-left origin).
//     CGEventPost has no confirmation return value, so the wrapper can
//     only report creation failures; "posted" is the strongest claim.
//   - The NSWorkspace frontmost read is AppKit, so it is routed through
//     the application's main thread with a bounded wait (T2 §2.2);
//     without a serving main run loop it fails fast.
//   - The AX focused-window query uses a fresh system-wide element per
//     call with a bounded messaging timeout; a timeout fails the query
//     and the caller denies input (fail closed).

#import "bridge.h"

#import <AppKit/AppKit.h>
#import <ApplicationServices/ApplicationServices.h>
#import <CoreGraphics/CoreGraphics.h>
#import <Foundation/Foundation.h>
#import <dispatch/dispatch.h>
#import <mach/mach_time.h>
#import <math.h>
#import <string.h>

/* ---- Foreground (NSWorkspace) ---- */

static int32_t yk_frontmost_pid_on_main(void) {
	NSRunningApplication *app = [NSWorkspace sharedWorkspace].frontmostApplication;
	if (app == nil) return 0;
	return (int32_t)app.processIdentifier;
}

int32_t yk_frontmost_pid(double timeoutSec) {
	if ([NSThread isMainThread]) {
		return yk_frontmost_pid_on_main();
	}
	__block int32_t pid = -1;
	dispatch_semaphore_t done = dispatch_semaphore_create(0);
	dispatch_async(dispatch_get_main_queue(), ^{
		pid = yk_frontmost_pid_on_main();
		dispatch_semaphore_signal(done);
	});
	dispatch_time_t deadline =
	    dispatch_time(DISPATCH_TIME_NOW, (int64_t)(timeoutSec * NSEC_PER_SEC));
	if (dispatch_semaphore_wait(done, deadline) != 0) {
		// No main run loop served the query within the bound; report the
		// unavailability instead of hanging the caller.
		return -1;
	}
	return pid;
}

/* ---- AX focused window ---- */

int32_t yk_ax_focused_window(double timeoutSec, YKAXFocus *out) {
	memset(out, 0, sizeof *out);
	AXUIElementRef systemWide = AXUIElementCreateSystemWide();
	if (systemWide == NULL) {
		out->axError = (int32_t)kAXErrorFailure;
		return -1;
	}
	if (timeoutSec > 0) {
		AXUIElementSetMessagingTimeout(systemWide, timeoutSec);
	}
	CFTypeRef focused = NULL;
	AXError err = AXUIElementCopyAttributeValue(systemWide, kAXFocusedWindowAttribute, &focused);
	if (err != kAXErrorSuccess) {
		out->axError = (int32_t)err;
		// kAXErrorCannotComplete is the bounded-timeout failure the AX
		// API reports when a message cannot complete in time.
		out->timedOut = (err == kAXErrorCannotComplete);
		CFRelease(systemWide);
		return -1;
	}
	pid_t pid = 0;
	if (AXUIElementGetPid((AXUIElementRef)focused, &pid) == kAXErrorSuccess) {
		out->pid = (int32_t)pid;
	}
	CFTypeRef positionValue = NULL;
	if (AXUIElementCopyAttributeValue((AXUIElementRef)focused, kAXPositionAttribute, &positionValue) == kAXErrorSuccess &&
	    positionValue != NULL) {
		CGPoint position = CGPointZero;
		if (AXValueGetValue((AXValueRef)positionValue, kAXValueCGPointType, &position)) {
			out->x = position.x;
			out->y = position.y;
		}
		CFRelease(positionValue);
	}
	CFTypeRef sizeValue = NULL;
	if (AXUIElementCopyAttributeValue((AXUIElementRef)focused, kAXSizeAttribute, &sizeValue) == kAXErrorSuccess &&
	    sizeValue != NULL) {
		CGSize size = CGSizeZero;
		if (AXValueGetValue((AXValueRef)sizeValue, kAXValueCGSizeType, &size)) {
			out->w = size.width;
			out->h = size.height;
		}
		CFRelease(sizeValue);
	}
	CFTypeRef roleValue = NULL;
	if (AXUIElementCopyAttributeValue((AXUIElementRef)focused, kAXRoleAttribute, &roleValue) == kAXErrorSuccess &&
	    roleValue != NULL) {
		CFStringGetCString((CFStringRef)roleValue, out->role, sizeof out->role, kCFStringEncodingUTF8);
		CFRelease(roleValue);
	}
	CFRelease(focused);
	CFRelease(systemWide);
	return 0;
}

/* ---- Permission preflights ---- */

bool yk_ax_trusted(void) {
	// Query only: the prompt option is explicitly off, so the call
	// never pops an authorization dialog.
	CFStringRef keys[1] = {kAXTrustedCheckOptionPrompt};
	CFBooleanRef values[1] = {kCFBooleanFalse};
	CFDictionaryRef options = CFDictionaryCreate(
	    kCFAllocatorDefault, (const void **)keys, (const void **)values, 1,
	    &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
	Boolean trusted = AXIsProcessTrustedWithOptions(options);
	if (options != NULL) CFRelease(options);
	return trusted != false;
}

bool yk_event_post_allowed(void) {
	return CGPreflightPostEventAccess();
}

/* ---- Quartz event synthesis ---- */

static int yk_post_event(CGEventRef event) {
	if (event == NULL) return -1;
	// CGEventPost returns void: there is no delivery confirmation to
	// check. The event tap chosen is the HID tap, the standard point
	// for synthesized input.
	CGEventPost(kCGHIDEventTap, event);
	CFRelease(event);
	return 0;
}

int yk_post_mouse_moved(double x, double y) {
	CGEventRef event = CGEventCreateMouseEvent(
	    NULL, kCGEventMouseMoved, CGPointMake((CGFloat)x, (CGFloat)y), kCGMouseButtonLeft);
	return yk_post_event(event);
}

int yk_post_mouse_button(int32_t button, int down, double x, double y) {
	CGEventType type;
	switch (button) {
	case 0:
		type = down ? kCGEventLeftMouseDown : kCGEventLeftMouseUp;
		break;
	case 1:
		type = down ? kCGEventRightMouseDown : kCGEventRightMouseUp;
		break;
	default:
		type = down ? kCGEventOtherMouseDown : kCGEventOtherMouseUp;
		break;
	}
	CGEventRef event = CGEventCreateMouseEvent(
	    NULL, type, CGPointMake((CGFloat)x, (CGFloat)y), (CGMouseButton)button);
	if (event == NULL) return -1;
	if (button > 2) {
		CGEventSetIntegerValueField(event, kCGMouseEventButtonNumber, (int64_t)button);
	}
	return yk_post_event(event);
}

int yk_post_key(int32_t keycode, int down) {
	CGEventRef event = CGEventCreateKeyboardEvent(NULL, (CGKeyCode)keycode, down != 0);
	return yk_post_event(event);
}

int yk_post_scroll(double deltaY) {
	// Pixel units on the vertical wheel; the sign convention follows
	// CGEventCreateScrollWheelEvent (positive scrolls up). Whether the
	// target interprets it that way is a real-device question.
	int32_t wheel = (int32_t)lround(deltaY);
	if (wheel == 0) return -1;
	CGEventRef event =
	    CGEventCreateScrollWheelEvent(NULL, kCGScrollEventUnitPixel, 1, wheel);
	return yk_post_event(event);
}

/* ---- Mach clock ---- */

uint64_t yk_mach_ticks(void) { return mach_absolute_time(); }

uint32_t yk_mach_timebase_numer(void) {
	mach_timebase_info_data_t info;
	(void)mach_timebase_info(&info);
	return info.numer;
}

uint32_t yk_mach_timebase_denom(void) {
	mach_timebase_info_data_t info;
	(void)mach_timebase_info(&info);
	return info.denom;
}
