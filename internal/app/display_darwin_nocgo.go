//go:build darwin && !cgo

package app

// currentDisplay cannot query CoreGraphics without cgo; the diagnostic
// text reports the display as unknown instead of guessing.
func currentDisplay() (width, height int, scale float64) {
	return 0, 0, 0
}
