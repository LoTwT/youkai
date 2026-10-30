package collect_test

import (
	"image"
	"image/color"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/game/zzz/collect"
	"github.com/LoTwT/youkai/internal/platform"
)

// The stage 2 scroll evidence matcher's tests: the checked-in masked
// fixtures replay the recorded M2 field scrolls through the new judgment
// (the historical success samples), the synthetic canvases exercise each
// decision branch with content the fixtures do not contain, and the ablation
// run pins that the new evidence changes none of the recorded verdicts.

// newScrollEvidenceMatcher builds the stage 2 matcher over the calibrated
// macOS cloud anchors.
func newScrollEvidenceMatcher() *collect.ScrollEvidenceMatcher {
	return collect.NewScrollEvidenceMatcher(macCloudMatcher())
}

// paintScrollFrame paints one synthetic warehouse frame for the scroll
// evidence matcher: a per-global-row pattern inside the list viewport with
// strong per-cell variation within each row, the selection border on one
// slot, and a scrollbar thumb segment. The row and cell bases are nonlinear
// hashes of the global row and column (an arithmetic base would differ by a
// constant between rows, and normalized correlation cannot see constants),
// so the true whole-row alignment correlates while every rival decorrelates
// — including inside the single surviving row of a four-row overlap, where
// only the within-row cell pattern can still identify the content.
// Everything outside the declared probes stays black: MatchScroll only
// reads the viewport, the border probes and the track. offset is the first
// visible global row; selection is a slot index or -1 for none; noise adds
// a deterministic ±10 codec-like dither so identical content still differs
// by a mean absolute error above the stationary threshold, the cloud
// condition the normalized path exists for.
func paintScrollFrame(l collect.Layout, id uint64, offset, selection, thumbStart, thumbEnd int, noise bool) platform.Frame {
	img := image.NewRGBA(image.Rectangle{Max: l.Image})
	gray := func(x, y int) int {
		g := offset + (y-l.ListViewport.Min.Y)/l.RowPitch
		c := (x - l.ListViewport.Min.X) / (l.ListViewport.Dx() / l.Columns)
		v := 60 + (137*g*g+29*g)%120 + ((53*g+97*c)*(11*g+3*c+1))%90
		if (y-l.ListViewport.Min.Y)%49 >= 24 {
			v += 12
		}
		if noise {
			v += ((x*7 + y*13) % 21) - 10
		}
		if v < 0 {
			v = 0
		}
		if v > 255 {
			v = 255
		}
		return v
	}
	for y := l.ListViewport.Min.Y; y < l.ListViewport.Max.Y; y++ {
		for x := l.ListViewport.Min.X; x < l.ListViewport.Max.X; x++ {
			v := uint8(gray(x, y))
			img.SetRGBA(x, y, color.RGBA{R: v, G: v, B: v, A: 255})
		}
	}
	paintSelectionBorder(img, l, selection)
	for y := thumbStart; y <= thumbEnd; y++ {
		if y >= l.ScrollTrack.Min.Y && y < l.ScrollTrack.Max.Y {
			img.SetRGBA(l.ScrollTrack.Min.X, y, color.RGBA{R: 200, G: 200, B: 200, A: 255})
		}
	}
	return platform.Frame{ID: id, Geometry: 1, Image: img, Content: l.Content}
}

// paintSelectionBorder paints the selection border strip on one slot, or
// nothing for a negative slot.
func paintSelectionBorder(img *image.RGBA, l collect.Layout, selection int) {
	if selection < 0 {
		return
	}
	r := l.BorderRect(selection).Intersect(img.Bounds())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 224, G: 208, B: 67, A: 255})
		}
	}
}

// TestScrollEvidenceMatcherFixtures replays every recorded M2 field scroll
// pair through the stage 2 matcher. The table is the historical expectation
// the pre-stage-2 matcher is pinned against (TestMacCloudScrollDisplacements):
// stationary pairs stay stationary, the displaced pairs keep their verified
// row counts and correspondences, and the off-pitch negative sample stays
// lost — the new evidence model must not change any recorded verdict. The
// displaced fixtures carry no selection anchor (selected=-1), so they also
// exercise the feature-only path with the scrollbar corroboration.
func TestScrollEvidenceMatcherFixtures(t *testing.T) {
	m := newScrollEvidenceMatcher()
	for _, tc := range scrollCases {
		tc := tc
		t.Run(tc.from+"->"+tc.to, func(t *testing.T) {
			assertScrollCase(t, m, loadMaskedFrame(t, tc.from), loadMaskedFrame(t, tc.to), tc)
			sm := m.MatchScroll(loadMaskedFrame(t, tc.from), loadMaskedFrame(t, tc.to))
			if tc.kind == "displaced" && (sm.Metrics == nil || !sm.Metrics.NCCMeasured ||
				sm.Metrics.BestNCCRow == 0 || sm.Metrics.RowNCC == nil) {
				t.Fatalf("displaced metrics %+v lack the normalized evidence", sm.Metrics)
			}
		})
	}
	if sm := m.MatchScroll(loadMaskedFrame(t, "s2-22-s1-step-1"), loadMaskedFrame(t, "s2-22-s1-step-1")); !sm.Stationary {
		t.Fatalf("a frame compared with itself must be stationary: %+v", sm)
	}
}

// TestScrollEvidenceAblationFallsBackToLegacyVerdicts pins the ablation
// requirement: with the stage 2 evidence disabled (the bare PixelMatcher the
// YOUKAI_SCROLL_MATCHER=legacy wiring installs), every recorded fixture pair
// keeps the pre-stage-2 verdict, and the stage 2 matcher agrees with it on
// the same pixels. Turning the new evidence off therefore restores the old
// judgment exactly; the sequence-level counterpart (the 2026-10-30 field
// failures replaying as unsettled under the legacy matcher) lives in the
// internal replay test.
func TestScrollEvidenceAblationFallsBackToLegacyVerdicts(t *testing.T) {
	legacy := macCloudMatcher()
	evidence := newScrollEvidenceMatcher()
	for _, tc := range scrollCases {
		before, after := loadMaskedFrame(t, tc.from), loadMaskedFrame(t, tc.to)
		old := legacy.MatchScroll(before, after)
		new := evidence.MatchScroll(before, after)
		if scrollVerdictOf(old) != scrollVerdictOf(new) || old.Rows != new.Rows || old.Stationary != new.Stationary ||
			old.Displaced != new.Displaced || old.Lost != new.Lost {
			t.Fatalf("%s->%s: legacy %+v, stage 2 %+v — the ablation must not change recorded verdicts",
				tc.from, tc.to, old, new)
		}
	}
}

func scrollVerdictOf(sm collect.ScrollMatch) string {
	switch {
	case sm.Stationary:
		return "stationary"
	case sm.Displaced:
		return "displaced"
	case sm.Lost:
		return "lost"
	case sm.Undecided:
		return "undecided"
	default:
		return "none"
	}
}

// TestScrollEvidenceSelectionDisplacementIsPrimary pins the primary
// evidence: the drag anchor's selection box moves from the last row to the
// first (r4c3 → r0c3, the F5 anchor), the normalized feature at that row
// corroborates it, and the grayscale MAD — well past the legacy error cap on
// the noisy frame — may not veto the displacement.
func TestScrollEvidenceSelectionDisplacementIsPrimary(t *testing.T) {
	l := collect.MacCloudLayout()
	m := newScrollEvidenceMatcher()
	before := paintScrollFrame(l, 1, 0, l.Rows*l.Columns-1, 300, 430, false)
	after := paintScrollFrame(l, 2, 4, l.Columns-1, 400, 530, true)
	sm := m.MatchScroll(before, after)
	if !sm.Displaced || sm.Rows != 4 || sm.Stationary || sm.Lost {
		t.Fatalf("verdict %+v, want displaced 4 rows (%s)", sm, sm.Evidence)
	}
	if len(sm.Matches) != (l.Rows-4)*l.Columns {
		t.Fatalf("%d correspondences, want %d", len(sm.Matches), (l.Rows-4)*l.Columns)
	}
	if sm.Metrics == nil || sm.Metrics.SelectionBefore != l.Rows*l.Columns-1 || sm.Metrics.SelectionAfter != l.Columns-1 {
		t.Fatalf("metrics %+v do not record the selection anchors", sm.Metrics)
	}
	if sm.Metrics.BestNCCRow != 4 || sm.Metrics.BestNCC < 0.9 {
		t.Fatalf("metrics %+v do not record the corroborating +4 alignment", sm.Metrics)
	}
}

// TestScrollEvidenceSelectionScrolledOutOfViewFallsBack pins the K1
// fallback: the anchor's selection scrolls out of the viewport (the
// before-selection sits above the last row, so four rows carry it out), the
// selection evidence is gone, and the feature-only path — stronger
// normalized threshold plus the scrollbar's corroboration — verifies the
// displacement instead.
func TestScrollEvidenceSelectionScrolledOutOfViewFallsBack(t *testing.T) {
	l := collect.MacCloudLayout()
	m := newScrollEvidenceMatcher()
	// The selection on r2c3 scrolls out after a four-row advance.
	before := paintScrollFrame(l, 1, 0, 2*l.Columns+3, 300, 430, false)
	after := paintScrollFrame(l, 2, 4, -1, 400, 530, true)
	sm := m.MatchScroll(before, after)
	if !sm.Displaced || sm.Rows != 4 {
		t.Fatalf("verdict %+v, want the feature-only path to verify 4 rows (%s)", sm, sm.Evidence)
	}
	if sm.Metrics == nil || sm.Metrics.SelectionAfter != -1 {
		t.Fatalf("metrics %+v do not record the missing after-selection", sm.Metrics)
	}
}

// TestScrollEvidenceFeatureNeedsScrollbarCorroboration pins the fail-closed
// fallback: without a selection anchor, a normalized alignment the
// scrollbar does not corroborate stays unverified — the thumb is the
// independent cross-check that a content correlation alone cannot replace.
func TestScrollEvidenceFeatureNeedsScrollbarCorroboration(t *testing.T) {
	l := collect.MacCloudLayout()
	m := newScrollEvidenceMatcher()
	before := paintScrollFrame(l, 1, 0, 2*l.Columns+3, 300, 430, false)
	after := paintScrollFrame(l, 2, 4, -1, 300, 430, true)
	sm := m.MatchScroll(before, after)
	if sm.Displaced || sm.Stationary || sm.Lost || !sm.Undecided {
		t.Fatalf("verdict %+v, want undecided without the scrollbar's corroboration (%s)", sm, sm.Evidence)
	}
}

// TestScrollEvidenceCompetitionRejectsPeriodicContent pins the
// best-versus-runner-up rule: a three-row periodic list makes the +1 and +4
// alignments equally perfect, so even the selection box's four-row claim
// cannot be corroborated uniquely and the verdict stays undecided — the
// machine does not pick between indistinguishable candidates.
func TestScrollEvidenceCompetitionRejectsPeriodicContent(t *testing.T) {
	l := collect.MacCloudLayout()
	m := newScrollEvidenceMatcher()
	periodic := func(id uint64, offset, selection, thumbStart, thumbEnd int) platform.Frame {
		f := paintScrollFrame(l, id, offset, -1, thumbStart, thumbEnd, false)
		img := f.Image
		// Overwrite the row bases with a three-row period: the pattern
		// repeats every three global rows (row base and within-row cell
		// pattern alike), so +1 and +4 align equally well.
		for y := l.ListViewport.Min.Y; y < l.ListViewport.Max.Y; y++ {
			g := offset + (y-l.ListViewport.Min.Y)/l.RowPitch
			for x := l.ListViewport.Min.X; x < l.ListViewport.Max.X; x++ {
				c := (x - l.ListViewport.Min.X) / (l.ListViewport.Dx() / l.Columns)
				i := img.PixOffset(x, y)
				v := uint8(60 + (g%3)*40 + ((97*c)*((g%3)*11+3*c+1))%90)
				if (y-l.ListViewport.Min.Y)%49 >= 24 {
					v += 12
				}
				img.Pix[i], img.Pix[i+1], img.Pix[i+2] = v, v, v
			}
		}
		paintSelectionBorder(img, l, selection)
		return f
	}
	before := periodic(1, 0, l.Rows*l.Columns-1, 300, 430)
	after := periodic(2, 4, l.Columns-1, 400, 530)
	sm := m.MatchScroll(before, after)
	if sm.Displaced || sm.Stationary || sm.Lost || !sm.Undecided {
		t.Fatalf("verdict %+v, want the periodic candidates to stay undecided (%s)", sm, sm.Evidence)
	}
}

// TestScrollEvidenceUniformContentRefusesNormalizedVerdicts pins the
// information gate: a uniform viewport whose brightness changed has no
// correlation to measure, so the normalized path must not turn the change
// into a stationary or displaced verdict; below the lost threshold the
// verdict is undecided, and pixel-identical uniform frames still prove
// stillness the legacy way.
func TestScrollEvidenceUniformContentRefusesNormalizedVerdicts(t *testing.T) {
	l := collect.MacCloudLayout()
	m := newScrollEvidenceMatcher()
	uniform := func(id uint64, value int, thumbStart, thumbEnd int) platform.Frame {
		f := paintScrollFrame(l, id, 0, -1, thumbStart, thumbEnd, false)
		img := f.Image
		for y := l.ListViewport.Min.Y; y < l.ListViewport.Max.Y; y++ {
			for x := l.ListViewport.Min.X; x < l.ListViewport.Max.X; x++ {
				i := img.PixOffset(x, y)
				img.Pix[i], img.Pix[i+1], img.Pix[i+2] = uint8(value), uint8(value), uint8(value)
			}
		}
		return f
	}
	changed := m.MatchScroll(uniform(1, 100, 300, 430), uniform(2, 106, 300, 430))
	if changed.Stationary || changed.Displaced || changed.Lost || !changed.Undecided {
		t.Fatalf("uniform brightness change = %+v, want undecided (%s)", changed, changed.Evidence)
	}
	identical := m.MatchScroll(uniform(1, 100, 300, 430), uniform(2, 100, 300, 430))
	if !identical.Stationary {
		t.Fatalf("pixel-identical uniform frames = %+v, want stationary (%s)", identical, identical.Evidence)
	}
}

// TestScrollEvidenceNoisyStillnessIsStationary pins the fix the stage 2
// model exists for: identical list content behind codec-like noise fails
// the legacy zero-shift MAD but proves stillness by normalized correlation,
// so a settled list no longer waits out the budget as undecided.
func TestScrollEvidenceNoisyStillnessIsStationary(t *testing.T) {
	l := collect.MacCloudLayout()
	m := newScrollEvidenceMatcher()
	before := paintScrollFrame(l, 1, 4, l.Columns-1, 400, 530, false)
	after := paintScrollFrame(l, 2, 4, l.Columns-1, 400, 530, true)
	sm := m.MatchScroll(before, after)
	if !sm.Stationary || sm.Displaced || sm.Lost {
		t.Fatalf("verdict %+v, want stationary by normalized correlation (%s)", sm, sm.Evidence)
	}
	if sm.Metrics == nil || sm.Metrics.Err0 < 4 {
		t.Fatalf("metrics %+v: the fixture must fail the legacy MAD gate to prove the point", sm.Metrics)
	}
	if ncc0 := sm.Metrics.RowNCC[0]; ncc0 < 0.92 {
		t.Fatalf("ncc0 %.3f below the stationary threshold on identical content", ncc0)
	}
}

// TestScrollEvidenceSameSlotSelectionBlocksFeatureDisplacement pins the
// primary evidence's negative side: the content changed but the selection
// box stayed on the same slot, so the change is not list movement and a
// feature-only displacement claim is refused — contradictory evidence fails
// closed instead of committing.
func TestScrollEvidenceSameSlotSelectionBlocksFeatureDisplacement(t *testing.T) {
	l := collect.MacCloudLayout()
	m := newScrollEvidenceMatcher()
	before := paintScrollFrame(l, 1, 0, l.Columns-1, 300, 430, false)
	after := paintScrollFrame(l, 2, 4, l.Columns-1, 400, 530, true)
	sm := m.MatchScroll(before, after)
	if sm.Displaced || sm.Stationary || sm.Lost || !sm.Undecided {
		t.Fatalf("verdict %+v, want undecided on the selection contradiction (%s)", sm, sm.Evidence)
	}
}

// TestScrollEvidenceUnreadableSelectionUsesFeaturePath pins the misread
// fallback: two bordered slots make the selection anchor unreadable, and
// the judgment falls back to the feature path with the scrollbar
// corroboration instead of guessing which border is real.
func TestScrollEvidenceUnreadableSelectionUsesFeaturePath(t *testing.T) {
	l := collect.MacCloudLayout()
	m := newScrollEvidenceMatcher()
	before := paintScrollFrame(l, 1, 0, l.Columns-1, 300, 430, false)
	// A second border strip on another slot makes the reading ambiguous.
	ambiguous := paintScrollFrame(l, 2, 4, -1, 400, 530, true)
	paintSelectionBorder(ambiguous.Image, l, 2*l.Columns)
	paintSelectionBorder(ambiguous.Image, l, 3*l.Columns+1)
	sm := m.MatchScroll(before, ambiguous)
	if !sm.Displaced || sm.Rows != 4 {
		t.Fatalf("verdict %+v, want the feature path to verify 4 rows (%s)", sm, sm.Evidence)
	}
	if sm.Metrics == nil || sm.Metrics.SelectionAfter != -2 {
		t.Fatalf("metrics %+v do not record the ambiguous after-selection", sm.Metrics)
	}
}

// TestScrollEvidenceOffPitchChangeIsLost pins the out-of-model rule through
// the new matcher: a large change no whole-row alignment explains is lost,
// never rounded into a displacement (the recorded totop-1→totop-2 negative
// sample class, reproduced synthetically with sub-row shifted noise-free
// content).
func TestScrollEvidenceOffPitchChangeIsLost(t *testing.T) {
	l := collect.MacCloudLayout()
	m := newScrollEvidenceMatcher()
	before := paintScrollFrame(l, 1, 0, -1, 300, 430, false)
	after := paintScrollFrame(l, 2, 0, -1, 300, 430, false)
	// Shift the after content by half a row pitch: no whole-row candidate
	// aligns it.
	img := after.Image
	src := paintScrollFrame(l, 3, 0, -1, 300, 430, false).Image
	for y := l.ListViewport.Min.Y; y < l.ListViewport.Max.Y; y++ {
		for x := l.ListViewport.Min.X; x < l.ListViewport.Max.X; x++ {
			i := img.PixOffset(x, y)
			j := src.PixOffset(x, y-l.RowPitch/2)
			if y-l.RowPitch/2 >= 0 {
				img.Pix[i], img.Pix[i+1], img.Pix[i+2] = src.Pix[j], src.Pix[j+1], src.Pix[j+2]
			}
		}
	}
	sm := m.MatchScroll(before, after)
	if !sm.Lost || sm.Displaced || sm.Stationary {
		t.Fatalf("verdict %+v, want lost on the off-pitch change (%s)", sm, sm.Evidence)
	}
	if sm.Metrics == nil || !sm.Metrics.Measured {
		t.Fatalf("metrics %+v lack the MAD search the lost rule rests on", sm.Metrics)
	}
}

// TestScrollEvidenceThresholdReporter pins the optional capability the
// diagnostic margins read.
func TestScrollEvidenceThresholdReporter(t *testing.T) {
	m := newScrollEvidenceMatcher()
	stationary, selection, displaced, margin := m.ScrollFeatureThresholds()
	c := collect.MacCloudCalibration()
	if stationary != c.ScrollStationaryNCC || selection != c.ScrollSelectionNCC ||
		displaced != c.ScrollDisplacedNCC || margin != c.ScrollNCCMargin {
		t.Fatalf("thresholds (%.2f, %.2f, %.2f, %.2f), want the calibration's", stationary, selection, displaced, margin)
	}
	if s, d := m.ScrollThresholds(); s != c.ScrollStationaryMax || d != c.ScrollDisplacedMax {
		t.Fatalf("wrapped thresholds (%.1f, %.1f), want the calibration's", s, d)
	}
}

// TestMacCloudScrollTimingProfile pins the macOS cloud timing profile's
// shape: the budgets cover F7's measured late starts and the settle needs
// more than one consecutive observation. The Inventory defaulting and the
// invalid-override fallback live in the internal tests.
func TestMacCloudScrollTimingProfile(t *testing.T) {
	timing := collect.MacCloudScrollTiming()
	if timing.MotionStartBudget < 2*time.Second {
		t.Fatalf("motion start budget %s does not cover F7's ~2s late starts", timing.MotionStartBudget)
	}
	if timing.SettleBudget < 2*time.Second {
		t.Fatalf("settle budget %s is shorter than a settling animation", timing.SettleBudget)
	}
	if timing.SettleObservations < 2 {
		t.Fatalf("settle observations %d: one observation is not a consecutive stability proof", timing.SettleObservations)
	}
}
