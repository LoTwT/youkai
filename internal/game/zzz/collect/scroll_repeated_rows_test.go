package collect_test

import (
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/LoTwT/youkai/internal/game/zzz/collect"
	"github.com/LoTwT/youkai/internal/platform"
)

// The Windows 1920×1080 repeated-row regression: a list whose visible rows
// are pixel-identical makes a whole-row move nearly invisible to the
// zero-shift MAD. The selection border and the scrollbar thumb are then the
// only position evidence, and a Stationary verdict from the MAD alone would
// let the traversal retry a drag that actually moved the list — the P1
// scroll slip that skipped a row of items.

// newWindowsScrollEvidenceMatcher builds the stage 2 matcher over the
// Windows local 1920×1080 profile.
func newWindowsScrollEvidenceMatcher(t *testing.T) (*collect.ScrollEvidenceMatcher, collect.Layout, collect.Calibration) {
	t.Helper()
	profile, err := collect.WindowsLocalProfileFor(image.Pt(1920, 1080))
	if err != nil {
		t.Fatalf("WindowsLocalProfileFor: %v", err)
	}
	return collect.NewScrollEvidenceMatcher(collect.NewPixelMatcher(profile.Layout, profile.Calibration)),
		profile.Layout, profile.Calibration
}

// repeatedRowThumb returns the thumb segment for a list of total rows with
// offset rows above the viewport, matching the geometry thumbTravelOf
// derives: thumb length / track length ≈ visible rows / total rows.
func repeatedRowThumb(l collect.Layout, offset, total int) (start, end int) {
	trackLen := l.ScrollTrack.Dy()
	thumbLen := int(math.Round(float64(trackLen) * float64(l.Rows) / float64(total)))
	if thumbLen < 1 {
		thumbLen = 1
	}
	perRow := float64(trackLen-thumbLen) / float64(total-l.Rows)
	start = l.ScrollTrack.Min.Y + int(math.Round(perRow*float64(offset)))
	return start, start + thumbLen - 1
}

// paintRepeatedRowFrame paints a warehouse list whose every visible row
// shares one within-row texture, so any whole-row displacement is
// pixel-identical inside the list viewport. Only the selection border and
// the scrollbar thumb name the position. The row texture keeps enough
// within-row detail and contrast for the normalized feature to report a
// real support (std ≥ ScrollInfoStdMin).
func paintRepeatedRowFrame(l collect.Layout, id uint64, offset, selection, thumbStart, thumbEnd int) platform.Frame {
	img := image.NewRGBA(image.Rectangle{Max: l.Image})
	for y := l.ListViewport.Min.Y; y < l.ListViewport.Max.Y; y++ {
		yy := (y - l.ListViewport.Min.Y) % l.RowPitch
		for x := l.ListViewport.Min.X; x < l.ListViewport.Max.X; x++ {
			h := uint32(x*97 + yy*1000003)
			h ^= h >> 16
			h *= 0x45d9f3b
			h ^= h >> 16
			v := int(40 + h%160)
			if yy >= 24 && yy < 36 {
				v += 12
			}
			if yy%4 == 0 {
				v += 45
			}
			if v > 255 {
				v = 255
			}
			i := img.PixOffset(x, y)
			img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = uint8(v), uint8(v), uint8(v), 255
		}
	}
	paintSelectionBorder(img, l, selection)
	for y := thumbStart; y <= thumbEnd; y++ {
		if y >= l.ScrollTrack.Min.Y && y < l.ScrollTrack.Max.Y {
			img.SetRGBA(l.ScrollTrack.Min.X, y, color.RGBA{R: 200, G: 200, B: 200, A: 255})
		}
	}
	return platform.Frame{
		ID:       id,
		Geometry: 1,
		Image:    img,
		Content:  l.Content,
	}
}

// TestScrollEvidenceRepeatedRowsOneRowMoveIsNotStationary pins the P1 defect
// and its fix in the matcher: the first real one-row move of a repeated-row
// Windows list changes only the selection border (and thumb), so the
// zero-shift MAD stays below the stationary threshold. The legacy matcher
// calls that Stationary; the evidence matcher must let the selection box veto
// stillness and verify the one-row displacement with the scrollbar's
// corroboration. This is the before/after contrast: the same frames that
// prove the legacy misjudgment must not reproduce it in the evidence model.
func TestScrollEvidenceRepeatedRowsOneRowMoveIsNotStationary(t *testing.T) {
	m, l, c := newWindowsScrollEvidenceMatcher(t)
	beforeStart, beforeEnd := repeatedRowThumb(l, 3, 30)
	afterStart, afterEnd := repeatedRowThumb(l, 4, 30)
	// The selected item's border follows its global row: row 4 of the
	// viewport (slot 16) before the advance, row 3 (slot 12) after it.
	before := paintRepeatedRowFrame(l, 1, 3, 16, beforeStart, beforeEnd)
	after := paintRepeatedRowFrame(l, 2, 4, 12, afterStart, afterEnd)

	legacy := m.PixelMatcher.MatchScroll(before, after)
	if !legacy.Stationary || legacy.Metrics == nil || legacy.Metrics.Err0 >= c.ScrollStationaryMax {
		t.Fatalf("legacy verdict %+v (err0 %+v): the fixture must reproduce the MAD misjudgment (err0 < %.1f)",
			legacy, legacy.Metrics, c.ScrollStationaryMax)
	}

	sm := m.MatchScroll(before, after)
	if sm.Stationary || !sm.Displaced || sm.Rows != 1 || sm.Lost {
		t.Fatalf("verdict %+v, want the selection box's one-row displacement, not stillness (%s)", sm, sm.Evidence)
	}
	if len(sm.Matches) != (l.Rows-1)*l.Columns {
		t.Fatalf("%d correspondences, want %d", len(sm.Matches), (l.Rows-1)*l.Columns)
	}
	if sm.Metrics == nil || sm.Metrics.SelectionBefore != 16 || sm.Metrics.SelectionAfter != 12 {
		t.Fatalf("metrics %+v do not record the selection anchors", sm.Metrics)
	}
	if sm.Basis != collect.ScrollBasisSelectionFeatureThumb {
		t.Fatalf("basis %q, want the scrollbar-corroborated selection path", sm.Basis)
	}
}

// TestScrollEvidenceRepeatedRowsStillnessIsStationary pins the other half of
// the rule: genuinely identical frames of the same repeated-row list, with
// the selection box and thumb unchanged, are still Stationary. The
// movement veto must not turn a true stillness into an undecidable wait.
func TestScrollEvidenceRepeatedRowsStillnessIsStationary(t *testing.T) {
	m, l, _ := newWindowsScrollEvidenceMatcher(t)
	thumbStart, thumbEnd := repeatedRowThumb(l, 3, 30)
	before := paintRepeatedRowFrame(l, 1, 3, 16, thumbStart, thumbEnd)
	after := paintRepeatedRowFrame(l, 2, 3, 16, thumbStart, thumbEnd)

	sm := m.MatchScroll(before, after)
	if !sm.Stationary || sm.Displaced || sm.Lost || sm.Undecided {
		t.Fatalf("verdict %+v, want Stationary for unchanged repeated rows (%s)", sm, sm.Evidence)
	}
	if sm.Metrics == nil || sm.Metrics.Err0 != 0 {
		t.Fatalf("metrics %+v, want the pixel-identical zero-shift measurement", sm.Metrics)
	}
}

// TestScrollEvidenceRepeatedRowsThumbMoveVetoesStillness pins the independent
// witness: the list pixels and the selection are unchanged, but the thumb
// travelled the calibrated minimum. A scrollbar movement that measurable
// contradicts stillness, so the verdict must stay fail-closed instead of
// proving the list did not move.
func TestScrollEvidenceRepeatedRowsThumbMoveVetoesStillness(t *testing.T) {
	m, l, c := newWindowsScrollEvidenceMatcher(t)
	if c.ScrollbarMoveMin > 2 {
		t.Fatalf("ScrollbarMoveMin %d: the fixture assumes a 2px movement floor", c.ScrollbarMoveMin)
	}
	thumbStart, thumbEnd := repeatedRowThumb(l, 3, 30)
	before := paintRepeatedRowFrame(l, 1, 3, 16, thumbStart, thumbEnd)
	after := paintRepeatedRowFrame(l, 2, 3, 16, thumbStart+c.ScrollbarMoveMin, thumbEnd+c.ScrollbarMoveMin)

	sm := m.MatchScroll(before, after)
	if sm.Stationary || sm.Displaced {
		t.Fatalf("verdict %+v, want fail-closed when a measurable thumb move contradicts stillness (%s)", sm, sm.Evidence)
	}
}
