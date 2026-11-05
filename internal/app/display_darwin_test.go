//go:build darwin && cgo

package app

import "testing"

// TestDisplayMetricsUsesPixelWidthOverPointWidth pins the Retina case
// from the field: 3024×1964 pixels in 1512×982 points must report 200%,
// not the 100% the old point-based reading produced.
func TestDisplayMetricsUsesPixelWidthOverPointWidth(t *testing.T) {
	width, height, scale := displayMetrics(3024, 1964, 1512)
	if width != 3024 || height != 1964 {
		t.Fatalf("displayMetrics = %d×%d, want 3024×1964 pixels", width, height)
	}
	if want := 2.0; scale != want {
		t.Fatalf("displayMetrics scale = %v, want pixel width 3024 / point width 1512 = %v", scale, want)
	}

	if width, height, scale := displayMetrics(0, 1964, 1512); width != 0 || height != 0 || scale != 0 {
		t.Fatalf("displayMetrics without a pixel size = %d×%d scale %v, want all zero", width, height, scale)
	}
	if width, height, scale := displayMetrics(3024, 1964, 0); width != 0 || height != 0 || scale != 0 {
		t.Fatalf("displayMetrics without a point width = %d×%d scale %v, want all zero", width, height, scale)
	}
}

// TestCurrentDisplayReportsRealMetrics exercises the CoreGraphics read on
// the machine running the test. A headless host may have no main display;
// skip there instead of inventing numbers.
func TestCurrentDisplayReportsRealMetrics(t *testing.T) {
	width, height, scale := currentDisplay()
	if width <= 0 || height <= 0 {
		t.Skip("the platform reported no main display")
	}
	if scale <= 0 {
		t.Fatalf("display %d×%d has no scale", width, height)
	}
	t.Logf("main display %d×%d px, scale %v", width, height, scale)
}
