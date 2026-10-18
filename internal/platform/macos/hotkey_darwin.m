// youkai macOS bridge — the global stop hotkey via Carbon's
// RegisterEventHotKey.
//
// Design (verified against the last Carbon-based release of the
// golang-design/hotkey reference and the HIToolbox headers):
//   - The handler is installed on GetApplicationEventTarget for
//     {kEventClassKeyboard, kEventHotKeyPressed} and the hotkey is
//     registered on the same target; hotkey events are delivered by
//     the host process's main run loop (the MyGo application provides
//     one). A plain Carbon hotkey needs no Accessibility or Input
//     Monitoring permission.
//   - RegisterEventHotKey and InstallEventHandler are documented "not
//     thread safe", so registration and unregistration run on the main
//     thread; calls from other threads marshal through the main queue
//     with a bounded wait and fail fast when no main run loop serves
//     it (e.g. bare test binaries).
//   - Triggering a registered hotkey notifies the process without
//     activating it, so the trigger does not steal focus from the
//     game.
//
// The Carbon event-manager declarations below are declared here
// because recent SDKs removed the HIToolbox headers that carried them
// while HIToolbox still exports the symbols. The prototypes and struct
// layouts are copied verbatim from the last SDKs that shipped them
// (verified against the MacOSX 10.15 SDK headers and the local Xcode
// SDK); the modifier bit values are declared on the Go side
// (hotkey.go) and passed through as plain numbers, so no Carbon header
// is included here at all.

#import "bridge.h"
#import "_cgo_export.h"

#import <Foundation/Foundation.h>
#import <MacTypes.h>
#import <dispatch/dispatch.h>

/* HIToolbox event-manager types, prototypes and constants (see the
 * file comment). Layouts match the SDK declarations byte for byte. */

typedef struct YKEventTypeSpec {
	OSType eventClass;
	UInt32 eventKind;
} YKEventTypeSpec;

typedef struct YKEventHotKeyID {
	OSType signature;
	UInt32 id;
} YKEventHotKeyID;

typedef OSStatus (*YKEventHandlerProcPtr)(void *inHandlerCallRef, void *inEvent, void *inUserData);

extern OSStatus InstallEventHandler(void *inTarget, YKEventHandlerProcPtr inHandler,
                                    unsigned long inNumTypes, const YKEventTypeSpec *inList,
                                    void *inUserData, void **outRef);
extern void *GetApplicationEventTarget(void);
extern OSStatus GetEventParameter(void *inEvent, UInt32 inName, UInt32 inType,
                                  UInt32 *outActualType, unsigned long inBufferSize,
                                  unsigned long *outActualSize, void *outData);
extern OSStatus RegisterEventHotKey(UInt32 inHotKeyCode, UInt32 inHotKeyModifiers,
                                    YKEventHotKeyID inHotKeyID, void *inTarget,
                                    unsigned long inOptions, void **outRef);
extern OSStatus UnregisterEventHotKey(void *inHotKey);

static const OSType YKEventClassKeyboard = 'keyb';
static const UInt32 YKEventHotKeyPressed = 5;
static const UInt32 YKParamDirectObject = '----';
static const UInt32 YKTypeEventHotKeyID = 'hkid';
static const OSType YKHotKeySignature = 'ykai';

/* The handler runs on the main thread; it only forwards the hotkey id
 * to Go, and the Go trigger must not block the main thread. */
static OSStatus yk_hotkey_handler(void *inHandlerCallRef, void *inEvent, void *inUserData) {
	(void)inHandlerCallRef;
	(void)inUserData;
	YKEventHotKeyID hotKeyID;
	UInt32 actualType = 0;
	unsigned long actualSize = 0;
	OSStatus status = GetEventParameter(inEvent, YKParamDirectObject, YKTypeEventHotKeyID,
	                                    &actualType, sizeof hotKeyID, &actualSize, &hotKeyID);
	if (status == noErr) {
		youkaiHotKeyFired((int64_t)hotKeyID.id);
	}
	return noErr;
}

/* The handler install status; guarded by running on the main thread. */
static OSStatus g_handlerStatus = -1; /* not installed yet */

/* yk_run_on_main marshals block onto the main thread with a bounded
 * wait; 0 ran, -1 timed out (no main run loop served the queue). */
static int yk_run_on_main(double timeoutSec, dispatch_block_t block) {
	if ([NSThread isMainThread]) {
		block();
		return 0;
	}
	__block dispatch_semaphore_t done = dispatch_semaphore_create(0);
	dispatch_async(dispatch_get_main_queue(), ^{
		block();
		dispatch_semaphore_signal(done);
	});
	dispatch_time_t deadline =
	    dispatch_time(DISPATCH_TIME_NOW, (int64_t)(timeoutSec * NSEC_PER_SEC));
	if (dispatch_semaphore_wait(done, deadline) != 0) {
		return -1;
	}
	return 0;
}

int yk_hotkey_register(uint32_t keyCode, uint32_t modifiers, int64_t handle,
                       double timeoutSec, void **outRef) {
	*outRef = NULL;
	__block OSStatus status = noErr;
	__block void *ref = NULL;
	int rc = yk_run_on_main(timeoutSec, ^{
		if (g_handlerStatus != noErr) {
			g_handlerStatus =
			    InstallEventHandler(GetApplicationEventTarget(), yk_hotkey_handler, 1,
			                        &(YKEventTypeSpec){YKEventClassKeyboard, YKEventHotKeyPressed},
			                        NULL, NULL);
			if (g_handlerStatus != noErr) {
				status = g_handlerStatus;
				return;
			}
		}
		YKEventHotKeyID hotKeyID = {YKHotKeySignature, (UInt32)handle};
		status = RegisterEventHotKey(keyCode, modifiers, hotKeyID,
		                             GetApplicationEventTarget(), 0, &ref);
	});
	if (rc != 0) return -2; /* main run loop unavailable */
	if (status != noErr) return (int)status;
	*outRef = ref;
	return 0;
}

int yk_hotkey_unregister(void *ref, double timeoutSec) {
	if (ref == NULL) return -1;
	__block OSStatus status = noErr;
	int rc = yk_run_on_main(timeoutSec, ^{
		status = UnregisterEventHotKey(ref);
	});
	if (rc != 0) return -2; /* main run loop unavailable */
	if (status != noErr) return (int)status;
	return 0;
}
