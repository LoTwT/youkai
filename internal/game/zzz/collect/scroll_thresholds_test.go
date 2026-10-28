package collect_test

import (
	"image"
	"testing"

	"github.com/LoTwT/youkai/internal/game/zzz/collect"
	"github.com/LoTwT/youkai/internal/platform"
)

// The boundary tests below pin the rejecting side of each calibrated
// scroll tier; TestMacCloudScrollDisplacements already pins the accepting
// side with recorded pairs (stationary 1.5–2.0, four-row 6.9–7.2 and 9.7).
// Each frame below starts from a recorded scroll fixture's list viewport
// and adds one controlled difference, so the verdict is decided by the
// calibrated threshold rather than by a wholly invented image. T5 §5 records
// the thresholds and the field evidence behind them.

const scrollThresholdFixture = "s2-22-s1-step-1"

func loadScrollThresholdFrame(t *testing.T) platform.Frame {
	t.Helper()
	return loadMaskedFrame(t, scrollThresholdFixture)
}

func cloneScrollFrame(f platform.Frame) platform.Frame {
	img := image.NewRGBA(f.Image.Bounds())
	copy(img.Pix, f.Image.Pix)
	f.Image = img
	return f
}

// scrollGray reads a pixel's grayscale value; the synthetic frames write
// the same value into all three channels, so this is their exact sample.
func scrollGray(img *image.RGBA, x, y int) int {
	i := img.PixOffset(x, y)
	return (int(img.Pix[i]) + int(img.Pix[i+1]) + int(img.Pix[i+2])) / 3
}

func setScrollGray(img *image.RGBA, x, y, value int) {
	if value < 0 {
		value = 0
	}
	if value > 255 {
		value = 255
	}
	i := img.PixOffset(x, y)
	img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = uint8(value), uint8(value), uint8(value), 255
}

// boundedScrollDelta picks a signed offset that keeps value+delta inside
// the 8-bit range, so the controlled error stays exactly |delta|.
func boundedScrollDelta(value, delta int) int {
	if value <= 127 {
		return delta
	}
	return -delta
}

func fillScrollViewport(img *image.RGBA, vp image.Rectangle, fill func(value int) int) {
	for y := vp.Min.Y; y < vp.Max.Y; y++ {
		for x := vp.Min.X; x < vp.Max.X; x++ {
			setScrollGray(img, x, y, fill(scrollGray(img, x, y)))
		}
	}
}

// TestMatchScrollStationaryThresholdBoundary separates list motion from
// live-UI noise: a viewport difference just below the calibrated noise floor
// is Stationary, while a larger one that still falls between the stationary
// and displaced tiers must stay Undecided until another observation proves
// what happened. Treating the larger difference as Stationary would let a
// moving list be confirmed as still.
func TestMatchScrollStationaryThresholdBoundary(t *testing.T) {
	m := macCloudMatcher()
	base := loadScrollThresholdFrame(t)
	vp := collect.MacCloudLayout().ListViewport

	cases := []struct {
		name           string
		delta          int
		wantStationary bool
	}{
		{"below_the_stationary_floor", 3, true},
		{"above_the_stationary_floor", 5, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			after := cloneScrollFrame(base)
			fillScrollViewport(after.Image, vp, func(value int) int {
				return value + boundedScrollDelta(value, tc.delta)
			})
			sm := m.MatchScroll(base, after)
			if tc.wantStationary {
				if !sm.Stationary || sm.Displaced || sm.Lost || sm.Undecided {
					t.Fatalf("verdict %+v, want Stationary (%s)", sm, sm.Evidence)
				}
				return
			}
			if sm.Stationary {
				t.Fatalf("a viewport difference above the noise floor was called Stationary: %s", sm.Evidence)
			}
			if !sm.Undecided || sm.Displaced || sm.Lost {
				t.Fatalf("verdict %+v, want Undecided (%s)", sm, sm.Evidence)
			}
		})
	}
}

// TestMatchScrollDisplacedErrorCapBoundary pins the absolute error cap on
// a four-row alignment. The top window is an exact copy of the recorded
// viewport four rows below plus a bounded per-pixel difference, so the row
// correspondence itself is real. A best error inside the cap is a verified
// displacement; one above it is not, even when it improves on the
// zero-displacement error by more than the margin.
func TestMatchScrollDisplacedErrorCapBoundary(t *testing.T) {
	l := collect.MacCloudLayout()
	m := macCloudMatcher()
	base := loadScrollThresholdFrame(t)
	vp := l.ListViewport
	shift := 4 * l.RowPitch

	cases := []struct {
		name          string
		delta         int
		wantRows      int
		wantDisplaced bool
	}{
		{"inside_the_error_cap", 9, 4, true},
		{"above_the_error_cap", 11, 0, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			after := cloneScrollFrame(base)
			// The rows outside the compared overlap are unrelated to the
			// four-row window; leaving them empty keeps the zero-shift
			// difference clearly above the margin.
			fillScrollViewport(after.Image, vp, func(_ int) int { return 0 })
			for y := vp.Min.Y; y < vp.Max.Y-shift; y++ {
				for x := vp.Min.X; x < vp.Max.X; x++ {
					value := scrollGray(base.Image, x, y+shift)
					setScrollGray(after.Image, x, y, value+boundedScrollDelta(value, tc.delta))
				}
			}
			sm := m.MatchScroll(base, after)
			if !tc.wantDisplaced {
				if sm.Displaced {
					t.Fatalf("a four-row alignment above the error cap was accepted as displaced: %s", sm.Evidence)
				}
				if !sm.Undecided || sm.Stationary || sm.Lost {
					t.Fatalf("verdict %+v, want Undecided (%s)", sm, sm.Evidence)
				}
				return
			}
			if !sm.Displaced || sm.Rows != tc.wantRows || sm.Stationary || sm.Lost || sm.Undecided {
				t.Fatalf("verdict %+v, want displaced %d rows (%s)", sm, tc.wantRows, sm.Evidence)
			}
			wantMatches := (l.Rows - tc.wantRows) * l.Columns
			if len(sm.Matches) != wantMatches {
				t.Fatalf("%d correspondences, want %d (%s)", len(sm.Matches), wantMatches, sm.Evidence)
			}
		})
	}
}

// TestMatchScrollDisplacedMarginBoundary pins the required improvement over
// the zero-displacement error. The recorded viewport is made
// four-row-periodic over the comparison by copying its top grid row (147
// pixels) to four rows below, so an exact four-row alignment exists. The
// controlled difference is then split between the compared overlap and the
// remaining rows: with a 9.6 zero-shift error the 8.0 alignment is only 1.6
// better and must stay Undecided; with a near-12 error it is almost 4 better
// and is a verified displacement.
func TestMatchScrollDisplacedMarginBoundary(t *testing.T) {
	l := collect.MacCloudLayout()
	m := macCloudMatcher()
	base := loadScrollThresholdFrame(t)
	vp := l.ListViewport
	shift := 4 * l.RowPitch
	overlapEnd := vp.Min.Y + l.RowPitch

	for y := vp.Min.Y; y < overlapEnd; y++ {
		for x := vp.Min.X; x < vp.Max.X; x++ {
			setScrollGray(base.Image, x, y+shift, scrollGray(base.Image, x, y))
		}
	}

	cases := []struct {
		name          string
		outsideDelta  int
		wantDisplaced bool
	}{
		{"short_of_the_margin", 10, false},
		{"past_the_margin", 13, true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			after := cloneScrollFrame(base)
			for y := vp.Min.Y; y < vp.Max.Y; y++ {
				delta := 8
				if y >= overlapEnd {
					delta = tc.outsideDelta
				}
				for x := vp.Min.X; x < vp.Max.X; x++ {
					value := scrollGray(base.Image, x, y)
					setScrollGray(after.Image, x, y, value+boundedScrollDelta(value, delta))
				}
			}
			sm := m.MatchScroll(base, after)
			if !tc.wantDisplaced {
				if sm.Displaced {
					t.Fatalf("a four-row alignment short of the improvement margin was accepted: %s", sm.Evidence)
				}
				if !sm.Undecided || sm.Stationary || sm.Lost {
					t.Fatalf("verdict %+v, want Undecided (%s)", sm, sm.Evidence)
				}
				return
			}
			if !sm.Displaced || sm.Rows != 4 || sm.Stationary || sm.Lost || sm.Undecided {
				t.Fatalf("verdict %+v, want displaced 4 rows (%s)", sm, sm.Evidence)
			}
		})
	}
}

// TestMatchScrollRejectsUniformEdgeSliver covers the field case recorded in
// T5 §5: a 27-pixel uniform edge strip of the recorded list matches a
// far-away shift at error 0. The four-row window and the strip's far edge
// are both blanked so that the bogus 708-pixel alignment is exact, while the
// real four-row window keeps a bounded difference. The matcher must still
// report the real displacement; accepting the strip would replace a verified
// position with an invented one.
func TestMatchScrollRejectsUniformEdgeSliver(t *testing.T) {
	l := collect.MacCloudLayout()
	m := macCloudMatcher()
	before := loadScrollThresholdFrame(t)
	vp := l.ListViewport
	shift := 4 * l.RowPitch
	const sliver = 27

	// The strip at the top of the four-row comparison and the strip at the
	// far edge of the viewport are identical, so a 708-pixel shift compares
	// only uniform pixels and reaches zero error.
	for y := vp.Min.Y + shift; y < vp.Min.Y+shift+sliver; y++ {
		for x := vp.Min.X; x < vp.Max.X; x++ {
			setScrollGray(before.Image, x, y, 0)
		}
	}
	for y := vp.Max.Y - sliver; y < vp.Max.Y; y++ {
		for x := vp.Min.X; x < vp.Max.X; x++ {
			setScrollGray(before.Image, x, y, 0)
		}
	}

	after := cloneScrollFrame(before)
	fillScrollViewport(after.Image, vp, func(_ int) int { return 0 })
	for y := vp.Min.Y; y < vp.Max.Y-shift; y++ {
		for x := vp.Min.X; x < vp.Max.X; x++ {
			value := scrollGray(before.Image, x, y+shift)
			// The zero strip stays an exact zero match; the rest of the
			// four-row window carries a bounded difference, so the real
			// alignment has a nonzero error and the bogus sliver would win
			// without the at-least-one-row overlap floor.
			if y >= vp.Min.Y+sliver {
				value += boundedScrollDelta(value, 6)
			}
			setScrollGray(after.Image, x, y, value)
		}
	}

	sm := m.MatchScroll(before, after)
	if !sm.Displaced || sm.Rows != 4 || sm.Stationary || sm.Lost || sm.Undecided {
		t.Fatalf("verdict %+v, want displaced 4 rows (%s)", sm, sm.Evidence)
	}
	wantMatches := (l.Rows - 4) * l.Columns
	if len(sm.Matches) != wantMatches {
		t.Fatalf("%d correspondences, want %d (%s)", len(sm.Matches), wantMatches, sm.Evidence)
	}
}
