package main

/*
#cgo LDFLAGS: -framework AppKit
void fieldprobe_init_gui(void);
void fieldprobe_serve_main_loop(void);
int fieldprobe_cursor_pos(double timeoutSec, double *x, double *y);
*/
import "C"

import "time"

// initGUI initializes AppKit/CGS in the process (NSApplicationLoad) on
// the main thread. It must run before any native call that touches
// CoreGraphics Services — SCStream creation aborts with a
// CGS_REQUIRE_INIT assertion in a plain command-line process.
func initGUI() { C.fieldprobe_init_gui() }

// serveMainLoop parks the main thread in the main NSRunLoop forever. It
// services the GCD main queue (which the native layer marshals
// NSWorkspace reads and Carbon hotkey registration through) and the
// AppKit/Carbon event sources that deliver hotkey triggers. It never
// returns; the work goroutine exits the process.
func serveMainLoop() { C.fieldprobe_serve_main_loop() }

// cursorPos returns the global cursor position in screen points with a
// top-left origin (the space SCWindow.frame and CGEvent mouse events
// use). ok is false when the main run loop did not serve the query
// within the bound.
func cursorPos(timeout time.Duration) (x, y float64, ok bool) {
	xc := C.double(0)
	yc := C.double(0)
	r := C.fieldprobe_cursor_pos(C.double(timeout.Seconds()), &xc, &yc)
	if r == 0 {
		return 0, 0, false
	}
	return float64(xc), float64(yc), true
}
