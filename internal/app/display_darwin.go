//go:build darwin && cgo

package app

/*
#cgo LDFLAGS: -framework CoreGraphics
#include <CoreGraphics/CoreGraphics.h>
*/
import "C"

// currentDisplay reports the main display's pixel size and its scale in
// pixels per point. CoreGraphics is used instead of AppKit because this
// is a diagnostic read: it needs no window, main thread or screen
// recording permission.
func currentDisplay() (width, height int, scale float64) {
	display := C.CGMainDisplayID()
	width = int(C.CGDisplayPixelsWide(display))
	height = int(C.CGDisplayPixelsHigh(display))
	bounds := C.CGDisplayBounds(display)
	if width <= 0 || height <= 0 || bounds.size.width <= 0 || bounds.size.height <= 0 {
		return 0, 0, 0
	}
	return width, height, float64(width) / float64(bounds.size.width)
}
