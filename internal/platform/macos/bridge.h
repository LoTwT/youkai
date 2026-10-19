/* youkai macOS native bridge — the C ABI between the Go package and
 * the Objective-C/C implementations in the *_darwin.m files.
 *
 * Conventions:
 *   - Functions returning int return 0 on success and a nonzero
 *     platform code otherwise; fallible synchronous calls take an
 *     errBuf/errBufLen pair and write a UTF-8 message into it.
 *   - Go callbacks are the //export functions in bridge_darwin.go;
 *     the .m files call them through _cgo_export.h.
 *   - C never holds Go pointers: callbacks are associated through
 *     int64 handles owned by the Go registry in bridge_darwin.go.
 */
#ifndef YOUKAI_MACOS_BRIDGE_H
#define YOUKAI_MACOS_BRIDGE_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

/* ---- Window and display enumeration (SCShareableContent) ---- */

typedef struct YKWindow {
	uint32_t windowID;
	int32_t pid;
	int32_t layer;
	bool onScreen;
	bool active;
	double x, y, w, h;
	char bundleID[256];
	char title[256];
	char appName[128];
} YKWindow;

typedef struct YKDisplay {
	uint32_t displayID;
	int32_t width, height; /* display size in points */
	double x, y, w, h;     /* frame in screen points */
} YKDisplay;

int yk_enumerate_windows(double timeoutSec,
                         YKWindow **outWindows, int *outWindowCount,
                         YKDisplay **outDisplays, int *outDisplayCount,
                         char *errBuf, int errBufLen);
void yk_free_enumeration(YKWindow *windows, YKDisplay *displays);

/* ---- Single-window capture stream (SCStream) ----
 *
 * Ownership: the stream context lives in the native registry and the
 * void* handle is a non-owning lookup token. yk_stream_release removes
 * the context from the registry and drops the last reference (ARC);
 * the caller must only release after the stop completed and the queue
 * barrier (yk_stream_drain) passed, so no callback can race the free.
 */

typedef struct YKFrameGeometry {
	int32_t status; /* SCFrameStatus, -1 when the attachment is missing */
	bool hasDisplayTime;
	uint64_t displayTime; /* raw mach ticks */
	bool hasContentRect;
	double contentX, contentY, contentW, contentH;
	bool hasContentScale;
	double contentScale;
	bool hasScaleFactor;
	double scaleFactor;
	bool hasScreenRect;
	double screenX, screenY, screenW, screenH;
	int32_t surfaceWidth, surfaceHeight, bytesPerRow;
} YKFrameGeometry;

/* Stream lifecycle events reported through youkaiStreamEvent. */
enum {
	YK_STREAM_EVENT_STARTED = 1,
	YK_STREAM_EVENT_STOPPED = 2,
	YK_STREAM_EVENT_DRAINED = 3,
};

int yk_stream_start(uint32_t windowID, int64_t handle, void **outStream,
                    char *errBuf, int errBufLen);
int yk_stream_request_stop(void *stream);
int yk_stream_drain(void *stream);
void yk_stream_release(void *stream);

/* ---- Foreground, accessibility, permissions ---- */

typedef struct YKAXFocus {
	int32_t pid;
	double x, y, w, h;
	char role[64];
	int32_t axError; /* AXError code, 0 on success */
	bool timedOut;
} YKAXFocus;

/* Returns the frontmost application's pid, 0 when there is none, and
 * -1 when the main run loop did not serve the query within timeoutSec
 * (the query is routed through the application's main thread). */
int32_t yk_frontmost_pid(double timeoutSec);

/* Brings the application with pid to the foreground. Returns 0 when the
 * activation was accepted, 1 when the application is gone, 2 when the
 * application refused activation, and -1 when the main run loop did not
 * serve the request within timeoutSec. */
int32_t yk_activate_pid(int32_t pid, double timeoutSec);

/* Queries the system-wide focused window with a bounded messaging
 * timeout. Returns 0 on success; on failure out->axError carries the
 * AXError code and the remaining fields are void. */
int32_t yk_ax_focused_window(double timeoutSec, YKAXFocus *out);

/* AXIsProcessTrustedWithOptions with the prompt option disabled: the
 * query never pops an authorization dialog. */
bool yk_ax_trusted(void);

/* CGPreflightPostEventAccess. */
bool yk_event_post_allowed(void);

/* ---- Quartz event synthesis ----
 *
 * Return 0 when the event was created and posted; nonzero means event
 * creation failed and nothing was posted. CGEventPost itself has no
 * confirmation return value, so "posted" is the strongest claim. */

int yk_post_mouse_moved(double x, double y);
int yk_post_mouse_button(int32_t button, int down, double x, double y);
int yk_post_key(int32_t keycode, int down);
int yk_post_scroll(double deltaY);

/* ---- Global hotkey (Carbon RegisterEventHotKey) ----
 *
 * Registration and the event handler run on the application's main
 * thread; hotkey events are delivered by the host's main run loop.
 * The registrar fails fast (instead of hanging) when no main run loop
 * serves the main queue within timeoutSec. Triggering does not
 * activate the registering process. */

int yk_hotkey_register(uint32_t keyCode, uint32_t modifiers, int64_t handle,
                       double timeoutSec, void **outRef);
int yk_hotkey_unregister(void *ref, double timeoutSec);

/* ---- Mach clock ---- */

uint64_t yk_mach_ticks(void);
uint32_t yk_mach_timebase_numer(void);
uint32_t yk_mach_timebase_denom(void);

#endif /* YOUKAI_MACOS_BRIDGE_H */
