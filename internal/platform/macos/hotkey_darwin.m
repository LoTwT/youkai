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
#include <errno.h>
#include <pthread.h>
#include <time.h>

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
	if (yk_system_terminating()) return noErr;
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

static pthread_mutex_t g_mainMutex = PTHREAD_MUTEX_INITIALIZER;
static pthread_cond_t g_mainChanged = PTHREAD_COND_INITIALIZER;
static bool g_systemTerminating = false;

bool yk_system_terminating(void) {
	pthread_mutex_lock(&g_mainMutex);
	bool terminating = g_systemTerminating;
	pthread_mutex_unlock(&g_mainMutex);
	return terminating;
}

void yk_prepare_system_termination(void) {
	pthread_mutex_lock(&g_mainMutex);
	g_systemTerminating = true;
	pthread_cond_broadcast(&g_mainChanged);
	pthread_mutex_unlock(&g_mainMutex);
}

/* Pending blocks retain their captures until the main queue or OS
 * disposes of them. Termination wakes waiters without running a nested
 * loop, touching Carbon off-main, or freeing a still-referenced handle. */
int yk_run_on_main(double timeoutSec, dispatch_block_t block) {
	pthread_mutex_lock(&g_mainMutex);
	if (g_systemTerminating) {
		pthread_mutex_unlock(&g_mainMutex);
		return 1;
	}
	if ([NSThread isMainThread]) {
		pthread_mutex_unlock(&g_mainMutex);
		block();
		return 0;
	}
	__block bool ran = false;
	dispatch_async(dispatch_get_main_queue(), ^{
		pthread_mutex_lock(&g_mainMutex);
		if (!g_systemTerminating) {
			pthread_mutex_unlock(&g_mainMutex);
			block();
			pthread_mutex_lock(&g_mainMutex);
		}
		ran = true;
		pthread_cond_broadcast(&g_mainChanged);
		pthread_mutex_unlock(&g_mainMutex);
	});
	struct timespec deadline;
	clock_gettime(CLOCK_MONOTONIC, &deadline);
	int64_t nanos = (int64_t)(timeoutSec * NSEC_PER_SEC);
	deadline.tv_sec += nanos / NSEC_PER_SEC;
	deadline.tv_nsec += nanos % NSEC_PER_SEC;
	if (deadline.tv_nsec >= NSEC_PER_SEC) {
		deadline.tv_sec++;
		deadline.tv_nsec -= NSEC_PER_SEC;
	}
	int rc = 0;
	while (!ran && !g_systemTerminating && rc == 0) {
		struct timespec now, remaining;
		clock_gettime(CLOCK_MONOTONIC, &now);
		remaining.tv_sec = deadline.tv_sec - now.tv_sec;
		remaining.tv_nsec = deadline.tv_nsec - now.tv_nsec;
		if (remaining.tv_nsec < 0) {
			remaining.tv_sec--;
			remaining.tv_nsec += NSEC_PER_SEC;
		}
		if (remaining.tv_sec < 0) { rc = ETIMEDOUT; break; }
		rc = pthread_cond_timedwait_relative_np(&g_mainChanged, &g_mainMutex, &remaining);
	}
	int result = g_systemTerminating ? 1 : (ran ? 0 : -1);
	pthread_mutex_unlock(&g_mainMutex);
	return result;
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
	if (rc == 1) return -3; /* system termination refuses registration */
	if (rc != 0) return -2; /* main run loop unavailable */
	if (status != noErr) return (int)status;
	*outRef = ref;
	return 0;
}

int yk_hotkey_unregister(void *ref, double timeoutSec) {
	if (ref == NULL) return -1;
	if (yk_system_terminating()) return 0; /* OS reclaims the Carbon ref */
	__block OSStatus status = noErr;
	int rc = yk_run_on_main(timeoutSec, ^{
		status = UnregisterEventHotKey(ref);
	});
	if (rc == 1) return 0; /* OS reclaims the Carbon ref */
	if (rc != 0) return -2; /* main run loop unavailable */
	if (status != noErr) return (int)status;
	return 0;
}
