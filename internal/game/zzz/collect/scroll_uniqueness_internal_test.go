package collect

import (
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/LoTwT/youkai/internal/platform"
)

// TestMatchScrollRejectsCloseSameDirectionRow covers the end-to-end
// guard: a slowly changing vertical gradient breaks the reverse-direction
// tie, while a one-row and a four-row alignment remain close enough to be
// ambiguous. MatchScroll must refuse both rather than commit the
// four-row candidate.
func TestMatchScrollRejectsCloseSameDirectionRow(t *testing.T) {
	l := MacCloudLayout()
	m := NewPixelMatcher(l, MacCloudCalibration())
	before := gradientListFrame(l, 1, 0)
	after := gradientListFrame(l, 2, 4*l.RowPitch)

	sm := m.MatchScroll(before, after)
	if !sm.Lost || sm.Displaced || sm.Stationary {
		t.Fatalf("close row alignments = %+v, want Lost", sm)
	}
	if !strings.Contains(sm.Evidence, "do not uniquely identify") {
		t.Fatalf("ambiguous evidence = %q, want the competing alignments named", sm.Evidence)
	}
}

// gradientListFrame paints a three-row periodic pattern plus a slow
// vertical gradient. Shifting by four row pitches is exact; one row
// differs only by the gradient, and the reverse-direction match differs
// by twice as much.
func gradientListFrame(l Layout, id uint64, offset int) platform.Frame {
	img := image.NewRGBA(image.Rectangle{Max: l.Image})
	grays := []int{20, 120, 220}
	for y := 0; y < l.Image.Y; y++ {
		row := (y + offset) / l.RowPitch
		gray := grays[row%len(grays)] + (y+offset)/300
		if gray > 255 {
			gray = 255
		}
		c := color.RGBA{R: uint8(gray), G: uint8(gray), B: uint8(gray), A: 255}
		for x := 0; x < l.Image.X; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	return platform.Frame{ID: id, Geometry: 1, Image: img, Content: l.Content}
}

// TestRivalShiftRejectsCloseSameDirectionAlignment pins the repeated-row
// guard: another whole-row alignment in the same movement direction that
// is too close to the chosen one makes the correspondence ambiguous. The
// matcher must report Lost rather than choose the first minimum, which
// would silently shift the traversal's visited map.
func TestRivalShiftRejectsCloseSameDirectionAlignment(t *testing.T) {
	m := NewPixelMatcher(MacCloudLayout(), MacCloudCalibration())
	for _, tc := range []struct {
		name   string
		chosen int
		rows   map[int]float64
	}{
		{"forward", 4, map[int]float64{4: 7.0, 1: 8.0}},
		{"backward", -4, map[int]float64{-4: 7.0, -1: 8.0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rival, err, ambiguous := m.rivalShift(tc.chosen, 7.0, tc.rows)
			if !ambiguous {
				t.Fatalf("rivalShift(%d) accepted a close alignment: %v", tc.chosen, tc.rows)
			}
			if rival == 0 || err != 8.0 {
				t.Fatalf("rival = %d, error = %.2f, want the competing row", rival, err)
			}
		})
	}
}

func TestRivalShiftAllowsDistinctRows(t *testing.T) {
	m := NewPixelMatcher(MacCloudLayout(), MacCloudCalibration())
	rows := map[int]float64{
		4:  7.0,
		1:  9.5, // exactly the configured margin away
		-4: 7.0, // opposite direction is not competing
	}
	if rival, err, ambiguous := m.rivalShift(4, 7.0, rows); ambiguous {
		t.Fatalf("rivalShift accepted ambiguity with rival %d at %.2f", rival, err)
	}
}
