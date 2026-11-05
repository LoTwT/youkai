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
//
// CGDisplayPixelsWide/High and CGDisplayBounds both report points, so
// the pixel size has to come from the current display mode. On a Retina
// display the mode's pixel width is twice the bounds width, and that
// ratio is the effective scale.
func currentDisplay() (width, height int, scale float64) {
	display := C.CGMainDisplayID()
	mode := C.CGDisplayCopyDisplayMode(display)
	if mode == 0 {
		return 0, 0, 0
	}
	defer C.CGDisplayModeRelease(mode)

	return displayMetrics(
		int(C.CGDisplayModeGetPixelWidth(mode)),
		int(C.CGDisplayModeGetPixelHeight(mode)),
		float64(C.CGDisplayBounds(display).size.width),
	)
}

// displayMetrics converts a display mode's pixel size and its bounds
// width in points into the reported size and scale. The ratio is what
// makes a Retina display report 3024×1964 at 200% instead of its
// 1512×982 points at 100%.
func displayMetrics(pixelWidth, pixelHeight int, pointWidth float64) (width, height int, scale float64) {
	if pixelWidth <= 0 || pixelHeight <= 0 || pointWidth <= 0 {
		return 0, 0, 0
	}
	return pixelWidth, pixelHeight, float64(pixelWidth) / pointWidth
}
