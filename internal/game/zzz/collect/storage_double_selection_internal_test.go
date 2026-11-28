package collect

import (
	"image"
	"image/color"
	"image/draw"
	"strings"
	"testing"
)

// TestStorageMatcherDoubleSelectionIsUnknown pins the production matcher's
// double-selection field: a frame with two calibrated selection borders must
// be rejected as PageUnknown and name both candidate slots. The last
// PageUnknown trace must use the same border probes when List is nil.
func TestStorageMatcherDoubleSelectionIsUnknown(t *testing.T) {
	l := StorageLayout()
	matcher := NewPixelMatcher(l, StorageCalibration())
	frame := storageSelectedPatternFrame(l, 1, 0, -1, 300, 323)
	accent := image.NewUniform(color.RGBA{R: 224, G: 208, B: 67, A: 255})
	draw.Draw(frame.Image, l.BorderRect(19), accent, image.Point{}, draw.Src)
	draw.Draw(frame.Image, l.BorderRect(20), accent, image.Point{}, draw.Src)

	match := matcher.Match(frame)
	if match.Page != PageUnknown || match.List != nil ||
		!strings.Contains(match.Evidence, "slots 19 and 20") {
		t.Fatalf("double selection = page %q list=%+v evidence=%q, want PageUnknown naming slots 19/20",
			match.Page, match.List, match.Evidence)
	}
}
