package collect_test

import (
	"image"
	"image/color"
	"math"
	"strings"
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
// strong per-row and within-row identity, the selection border on one
// slot, and a scrollbar thumb segment. Three pattern components serve
// three judgments: a nonlinear per-row base identifies rows across
// multi-row overlaps (an arithmetic base would differ by a constant and
// normalized correlation cannot see constants); a per-(row, x) hash gives
// a one-row overlap strip its identity, so the true alignment correlates
// while the mirrored strip at the opposite end decorrelates; and a fine
// y-texture makes sub-row creeps visible at zero shift. Everything outside
// the declared probes stays black: MatchScroll only reads the viewport,
// the border probes and the track. offset is the first visible global row;
// selection is a slot index or -1 for none; noise adds a deterministic
// ±10 codec-like dither so identical content still differs by a mean
// absolute error above the stationary threshold, the cloud condition the
// normalized path exists for.
func paintScrollFrame(l collect.Layout, id uint64, offset, selection, thumbStart, thumbEnd int, noise bool) platform.Frame {
	img := image.NewRGBA(image.Rectangle{Max: l.Image})
	gray := func(x, y int) int {
		g := offset + (y-l.ListViewport.Min.Y)/l.RowPitch
		v := 60 + (137*g*g+29*g)%120
		h := uint32(g)*1000003 + uint32(x)*97
		h ^= h >> 16
		h *= 0x45d9f3b
		h ^= h >> 16
		v += int(h % 120)
		if (y-l.ListViewport.Min.Y)%49 >= 24 {
			v += 12
		}
		if (y-l.ListViewport.Min.Y)%4 == 0 {
			v += 45
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

// paintRowSimilarFrame paints a list whose rows share one texture and
// differ only by a low-contrast stripe whose position follows the global
// row. A four-row scroll therefore aligns the same row exactly, while a
// three-row (or one-row) candidate compares adjacent rows with only a small
// mismatch: the shape the 2026-10-31 field frames showed, where a four-row
// step leaves one row of true overlap and a homogeneous list makes a wrong
// neighbouring row competitive. The stripe is wide and shallow enough to
// keep the zero-shift error above the stationary threshold while the
// adjacent-row correlation stays within the competition margin.
func paintRowSimilarFrame(l collect.Layout, id uint64, offset, selection, thumbStart, thumbEnd int) platform.Frame {
	f := paintScrollFrame(l, id, offset, -1, thumbStart, thumbEnd, false)
	img := f.Image
	min, max := l.ListViewport.Min, l.ListViewport.Max
	for y := min.Y; y < max.Y; y++ {
		g := offset + (y-min.Y)/l.RowPitch
		yy := (y - min.Y) % l.RowPitch
		marker := 80 + (g*53)%280
		for x := min.X; x < max.X; x++ {
			h := uint32(x*97 + yy*1000003)
			h ^= h >> 16
			h *= 0x45d9f3b
			h ^= h >> 16
			v := int(40 + h%160)
			if x >= marker && x < marker+77 {
				if v += 30; v > 255 {
					v = 255
				}
			}
			img.SetRGBA(x, y, color.RGBA{R: uint8(v), G: uint8(v), B: uint8(v), A: 255})
		}
	}
	paintSelectionBorder(img, l, selection)
	return f
}

// ditherSpec parameterizes a frame's codec-like dither:
// ((x*ax + y*ay) % mod) - mod/2. A nil spec paints clean pixels.
type ditherSpec struct{ mod, ax, ay int }

// paintUniformRowFrame paints a list whose every row shares one within-row
// texture, so no whole-row alignment can be told from another on content: the
// only difference between candidates is where the after frame's dither falls.
// It is the degenerate end of the homogeneous top segment the 2026-10-31 field
// frames showed, used to pin what the model may decide when the features
// cannot choose a row at all.
func paintUniformRowFrame(l collect.Layout, id uint64, selection, thumbStart, thumbEnd int, dither *ditherSpec) platform.Frame {
	f := paintScrollFrame(l, id, 0, -1, thumbStart, thumbEnd, false)
	img := f.Image
	for y := l.ListViewport.Min.Y; y < l.ListViewport.Max.Y; y++ {
		yy := (y - l.ListViewport.Min.Y) % l.RowPitch
		for x := l.ListViewport.Min.X; x < l.ListViewport.Max.X; x++ {
			i := img.PixOffset(x, y)
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
			if dither != nil {
				v += ((x*dither.ax + y*dither.ay) % dither.mod) - dither.mod/2
			}
			if v < 0 {
				v = 0
			}
			if v > 255 {
				v = 255
			}
			img.Pix[i], img.Pix[i+1], img.Pix[i+2] = uint8(v), uint8(v), uint8(v)
		}
	}
	paintSelectionBorder(img, l, selection)
	return f
}

// strongestRival returns the strongest whole-row candidate other than chosen.
func strongestRival(rows map[int]float64, chosen int) (row int, ncc float64) {
	ncc = math.Inf(-1)
	for r, v := range rows {
		if r == chosen {
			continue
		}
		if v > ncc {
			row, ncc = r, v
		}
	}
	return row, ncc
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
	if sm.Basis != collect.ScrollBasisSelectionFeature {
		t.Fatalf("basis %q, want the ordinary selection+feature pass", sm.Basis)
	}
	if !strings.Contains(sm.Evidence, "runner-up gap") || !strings.Contains(sm.Evidence, "≥") {
		t.Fatalf("evidence %q should state the sufficient runner-up gap", sm.Evidence)
	}
}

// TestScrollEvidenceSelectionGapBelowMarginNeedsScrollbar pins the
// 2026-10-31 gap-limited case: the selection box and the normalized
// feature agree on the row and clear the correlation and information
// floors, but a homogeneous neighbouring row keeps the runner-up gap below
// 0.05. The scrollbar's independently derived travel is the extra witness;
// the gap threshold itself is unchanged, and the verdict names the
// gap-limited basis so field triage can tell it apart from the ordinary
// pass.
func TestScrollEvidenceSelectionGapBelowMarginNeedsScrollbar(t *testing.T) {
	l := collect.MacCloudLayout()
	m := newScrollEvidenceMatcher()
	c := collect.MacCloudCalibration()
	before := paintRowSimilarFrame(l, 1, 0, l.Rows*l.Columns-1, 300, 430)
	after := paintRowSimilarFrame(l, 2, 4, l.Columns-1, 405, 535)
	sm := m.MatchScroll(before, after)
	if !sm.Displaced || sm.Rows != 4 || sm.Stationary || sm.Lost {
		t.Fatalf("verdict %+v, want the four-row displacement accepted with the scrollbar (%s)", sm, sm.Evidence)
	}
	if sm.Basis != collect.ScrollBasisSelectionFeatureThumb {
		t.Fatalf("basis %q, want the gap-limited selection+feature+scrollbar basis", sm.Basis)
	}
	if sm.Metrics == nil || sm.Metrics.BestNCCRow != 4 {
		t.Fatalf("metrics %+v, want the selection's +4 row", sm.Metrics)
	}
	rival := math.Inf(-1)
	for row, ncc := range sm.Metrics.RowNCC {
		if row != sm.Metrics.BestNCCRow && ncc > rival {
			rival = ncc
		}
	}
	gap := sm.Metrics.BestNCC - rival
	if gap < 0 || gap >= c.ScrollNCCMargin {
		t.Fatalf("runner-up gap %.3f, want a gap-limited match (0 ≤ gap < %.2f)", gap, c.ScrollNCCMargin)
	}
	if !strings.Contains(sm.Evidence, "below the") || !strings.Contains(sm.Evidence, "scrollbar") ||
		!strings.Contains(sm.Evidence, "corroborated") {
		t.Fatalf("evidence %q should state the short gap and the scrollbar's independent corroboration", sm.Evidence)
	}
}

// TestScrollEvidenceSelectionGapBelowMarginRejectsUncorroboratedScrollbar
// pins the fail-closed side of the gap-limited pass: the same near-tie
// stays undecided when the scrollbar does not corroborate the selection's
// four-row direction or magnitude.
func TestScrollEvidenceSelectionGapBelowMarginRejectsUncorroboratedScrollbar(t *testing.T) {
	l := collect.MacCloudLayout()
	m := newScrollEvidenceMatcher()
	before := paintRowSimilarFrame(l, 1, 0, l.Rows*l.Columns-1, 300, 430)
	for name, after := range map[string]platform.Frame{
		// Three rows of travel: the magnitude check rejects the off-by-one
		// claim.
		"short travel": paintRowSimilarFrame(l, 2, 4, l.Columns-1, 380, 510),
		// The thumb moves the wrong way for a content-up displacement.
		"backward travel": paintRowSimilarFrame(l, 3, 4, l.Columns-1, 225, 355),
	} {
		t.Run(name, func(t *testing.T) {
			sm := m.MatchScroll(before, after)
			if sm.Displaced || sm.Stationary || sm.Lost || !sm.Undecided {
				t.Fatalf("verdict %+v, want undecided without the scrollbar's corroboration (%s)", sm, sm.Evidence)
			}
			if sm.Basis != "" {
				t.Fatalf("basis %q, want no accepted-displacement basis", sm.Basis)
			}
			if !strings.Contains(sm.Evidence, "scrollbar does not corroborate") {
				t.Fatalf("evidence %q should name the failed scrollbar corroboration", sm.Evidence)
			}
		})
	}
}

// TestScrollEvidenceExactTieDefersToScrollbar pins the float-tie rule: the
// rows are content-identical, so two whole-row candidates correlate exactly
// the same, and the exact comparison the old rule used could only report a
// tie by coincidence. The named row must be recorded deterministically and the
// scrollbar must decide; the same content with an uncorroborated thumb stays
// undecided.
func TestScrollEvidenceExactTieDefersToScrollbar(t *testing.T) {
	l := collect.MacCloudLayout()
	m := newScrollEvidenceMatcher()
	before := paintUniformRowFrame(l, 1, l.Rows*l.Columns-1, 300, 430, nil)
	after := paintUniformRowFrame(l, 2, l.Columns-1, 405, 535, &ditherSpec{mod: 21, ax: 3, ay: 13})
	sm := m.MatchScroll(before, after)
	if !sm.Displaced || sm.Rows != 4 || sm.Stationary || sm.Lost {
		t.Fatalf("verdict %+v, want the exact tie decided by the scrollbar (%s)", sm, sm.Evidence)
	}
	if sm.Basis != collect.ScrollBasisSelectionFeatureThumb {
		t.Fatalf("basis %q, want the scrollbar-corroborated basis", sm.Basis)
	}
	if sm.Metrics == nil {
		t.Fatal("no metrics recorded")
	}
	rivalRow, rival := strongestRival(sm.Metrics.RowNCC, 4)
	if rival > sm.Metrics.RowNCC[4] {
		t.Fatalf("the fixture must not feature-prefer another row: +4 %.6f, %+d %.6f", sm.Metrics.RowNCC[4], rivalRow, rival)
	}
	if sm.Metrics.BestNCCRow != 4 || sm.Metrics.BestNCC != sm.Metrics.RowNCC[4] {
		t.Fatalf("metrics name %+d:%.6f, want the tied selection row +4:%.6f",
			sm.Metrics.BestNCCRow, sm.Metrics.BestNCC, sm.Metrics.RowNCC[4])
	}
	if !strings.Contains(sm.Evidence, "tie epsilon") || !strings.Contains(sm.Evidence, "scrollbar") {
		t.Fatalf("evidence %q should state the tie band and the scrollbar adjudication", sm.Evidence)
	}

	// A three-row travel cannot break the tie.
	uncorroborated := paintUniformRowFrame(l, 3, l.Columns-1, 380, 510, &ditherSpec{mod: 21, ax: 3, ay: 13})
	sm = m.MatchScroll(before, uncorroborated)
	if sm.Displaced || sm.Stationary || sm.Lost || !sm.Undecided {
		t.Fatalf("verdict %+v, want undecided without a corroborating scrollbar (%s)", sm, sm.Evidence)
	}
}

// TestScrollEvidenceNegligibleRivalGapUsesScrollbar pins the near-tie side of
// the same rule: on row-identical content the after frame's dither can put the
// mirrored -4 candidate a few 10^-5 above the selection's +4. That is below
// the tie epsilon, so the features cannot call it a contradiction; the
// direction of the scrollbar's travel is the independent evidence that rules
// the mirrored row out, and the displacement is accepted. The previous exact
// comparison rejected this same fixture because the rounding fell on the other
// side of zero.
func TestScrollEvidenceNegligibleRivalGapUsesScrollbar(t *testing.T) {
	l := collect.MacCloudLayout()
	m := newScrollEvidenceMatcher()
	before := paintUniformRowFrame(l, 1, l.Rows*l.Columns-1, 300, 430, nil)
	after := paintUniformRowFrame(l, 2, l.Columns-1, 405, 535, &ditherSpec{mod: 23, ax: 7, ay: 13})
	sm := m.MatchScroll(before, after)
	if sm.Metrics == nil {
		t.Fatal("no metrics recorded")
	}
	rivalRow, rival := strongestRival(sm.Metrics.RowNCC, 4)
	gap := sm.Metrics.RowNCC[4] - rival
	if gap >= 0 {
		t.Fatalf("the fixture must place a rival above the selection's row: gap %+.9f", gap)
	}
	if gap <= -0.005 {
		t.Fatalf("the rival's lead %.9f is outside the tie epsilon", gap)
	}
	if rivalRow != -4 {
		t.Fatalf("strongest rival %+d, want the mirrored -4 candidate", rivalRow)
	}
	if !sm.Displaced || sm.Rows != 4 || sm.Basis != collect.ScrollBasisSelectionFeatureThumb {
		t.Fatalf("verdict %+v, want the near tie decided by the scrollbar (%s)", sm, sm.Evidence)
	}
	if sm.Metrics.BestNCCRow != 4 {
		t.Fatalf("metrics name %+d, want the selection's +4", sm.Metrics.BestNCCRow)
	}
	if !strings.Contains(sm.Evidence, "tie epsilon") || !strings.Contains(sm.Evidence, "corroborated") {
		t.Fatalf("evidence %q should state the tie band and the corroboration", sm.Evidence)
	}
}

// TestScrollEvidenceScrollbarMustDistinguishCompetingRows pins the tightening
// the tie rule needs: the same near-tie content with a thumb that travelled
// 88px fits the claimed four rows (~105px) inside ±20% but also fits the
// competing +3 row (~79px). A thumb that cannot tell the rows apart may not
// decide between them, so the verdict stays undecided; the identical content
// with an unambiguous four-row travel is still accepted.
func TestScrollEvidenceScrollbarMustDistinguishCompetingRows(t *testing.T) {
	l := collect.MacCloudLayout()
	m := newScrollEvidenceMatcher()
	before := paintUniformRowFrame(l, 1, l.Rows*l.Columns-1, 300, 430, nil)
	ambiguous := paintUniformRowFrame(l, 2, l.Columns-1, 388, 518, &ditherSpec{mod: 23, ax: 7, ay: 13})
	sm := m.MatchScroll(before, ambiguous)
	if sm.Displaced || sm.Stationary || sm.Lost || !sm.Undecided {
		t.Fatalf("verdict %+v, want undecided when the thumb also fits another row (%s)", sm, sm.Evidence)
	}
	if sm.Basis != "" {
		t.Fatalf("basis %q, want no accepted-displacement basis", sm.Basis)
	}
	if !strings.Contains(sm.Evidence, "also the competing +3-row candidate") {
		t.Fatalf("evidence %q should name the competing row the thumb also fits", sm.Evidence)
	}

	unambiguous := paintUniformRowFrame(l, 3, l.Columns-1, 405, 535, &ditherSpec{mod: 23, ax: 7, ay: 13})
	if sm := m.MatchScroll(before, unambiguous); !sm.Displaced || sm.Rows != 4 {
		t.Fatalf("verdict %+v, want the four-row travel that no other candidate fits to corroborate 4 rows (%s)", sm, sm.Evidence)
	}
}

// TestScrollEvidenceSelectionContradictsFeatureRow pins the other hard
// constraint: when the normalized feature prefers a different row than the
// selection box, an agreeing scrollbar may not override the contradiction.
// The after content is shifted three rows while the selection box claims
// four; the thumb travels four rows' worth.
func TestScrollEvidenceSelectionContradictsFeatureRow(t *testing.T) {
	l := collect.MacCloudLayout()
	m := newScrollEvidenceMatcher()
	before := paintRowSimilarFrame(l, 1, 0, l.Rows*l.Columns-1, 300, 430)
	after := paintRowSimilarFrame(l, 2, 3, l.Columns-1, 405, 535)
	sm := m.MatchScroll(before, after)
	if sm.Displaced || sm.Stationary || sm.Lost || !sm.Undecided {
		t.Fatalf("verdict %+v, want undecided on the selection/feature contradiction (%s)", sm, sm.Evidence)
	}
	if sm.Metrics == nil || sm.Metrics.BestNCCRow != 3 {
		t.Fatalf("metrics %+v, want the feature's +3 row", sm.Metrics)
	}
	lead := sm.Metrics.RowNCC[3] - sm.Metrics.RowNCC[4]
	if lead <= 0.005 {
		t.Fatalf("the feature's lead %.4f must exceed the 0.005 tie epsilon to count as a contradiction", lead)
	}
	if !strings.Contains(sm.Evidence, "prefers another row") || !strings.Contains(sm.Evidence, "contradict") ||
		!strings.Contains(sm.Evidence, "tie epsilon") {
		t.Fatalf("evidence %q should state the selection/feature contradiction beyond the tie epsilon", sm.Evidence)
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
// best-versus-runner-up rule and its scrollbar tiebreak: a three-row
// periodic list makes the +1 and +4 alignments equally perfect, so the
// feature competition alone cannot distinguish them. The selection box's
// four-row claim plus the scrollbar's independent four-row travel now
// accept the displacement on the gap-limited basis; without a
// corroborating scrollbar the same frames stay undecided — the machine
// does not pick between indistinguishable candidates on features alone.
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
	if !sm.Displaced || sm.Rows != 4 || sm.Basis != collect.ScrollBasisSelectionFeatureThumb {
		t.Fatalf("verdict %+v, want the four-row displacement accepted on the scrollbar tiebreak (%s)", sm, sm.Evidence)
	}
	if sm.Metrics == nil || sm.Metrics.BestNCCRow != 4 || sm.Metrics.BestNCC < 0.99 {
		t.Fatalf("metrics %+v, want the selection's +4 row named among the perfect ties", sm.Metrics)
	}
	// The periodic content makes +1 and +4 correlate exactly the same: the
	// features are tied, so the verdict must rest on the scrollbar, and the
	// recorded best row must be the selection's own rather than whichever row
	// the map iteration happened to visit last.
	if got := sm.Metrics.RowNCC[1]; got != sm.Metrics.RowNCC[4] {
		t.Fatalf("periodic fixture: +1 %.9f and +4 %.9f are not the intended exact tie", got, sm.Metrics.RowNCC[4])
	}
	// A thumb travelling only three rows' worth cannot break the tie; the
	// periodic features stay undecided.
	uncorroborated := periodic(3, 4, l.Columns-1, 380, 510)
	sm = m.MatchScroll(before, uncorroborated)
	if sm.Displaced || sm.Stationary || sm.Lost || !sm.Undecided {
		t.Fatalf("verdict %+v, want undecided without a corroborating scrollbar (%s)", sm, sm.Evidence)
	}
	if sm.Basis != "" {
		t.Fatalf("basis %q, want no accepted-displacement basis", sm.Basis)
	}
	if !strings.Contains(sm.Evidence, "scrollbar does not corroborate") {
		t.Fatalf("evidence %q should name the failed scrollbar corroboration", sm.Evidence)
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

// TestScrollEvidenceSubRowShiftIsNotStationary pins the zero candidate's
// exact dy=0 evaluation: a two-pixel vertical creep (a settling animation
// mid-snap) must not borrow the stationary verdict from a neighbouring
// shift inside a tolerance window — stillness admits no window, so the
// verdict stays fail-closed until the creep stops.
func TestScrollEvidenceSubRowShiftIsNotStationary(t *testing.T) {
	l := collect.MacCloudLayout()
	m := newScrollEvidenceMatcher()
	before := paintScrollFrame(l, 1, 4, l.Columns-1, 400, 530, false)
	after := paintScrollFrame(l, 2, 4, -1, 400, 530, false)
	// Shift the viewport content down by two pixels: every row's fine
	// texture misaligns at dy=0 and realigns only at dy=+2, which the zero
	// candidate must not consult. The selection and thumb are re-painted at
	// their calibrated places so only the list content moved.
	src := before.Image
	for y := l.ListViewport.Max.Y - 1; y >= l.ListViewport.Min.Y+2; y-- {
		for x := l.ListViewport.Min.X; x < l.ListViewport.Max.X; x++ {
			i := after.Image.PixOffset(x, y)
			j := src.PixOffset(x, y-2)
			after.Image.Pix[i], after.Image.Pix[i+1], after.Image.Pix[i+2] = src.Pix[j], src.Pix[j+1], src.Pix[j+2]
		}
	}
	paintSelectionBorder(after.Image, l, l.Columns-1)
	sm := m.MatchScroll(before, after)
	if sm.Stationary || sm.Displaced {
		t.Fatalf("verdict %+v, want fail-closed on the sub-row creep (%s)", sm, sm.Evidence)
	}
	if sm.Metrics == nil || sm.Metrics.Err0 < 4 {
		t.Fatalf("metrics %+v: the creep must fail the zero-shift MAD too", sm.Metrics)
	}
}

// TestScrollEvidenceRejectsOffByRowThumbTravel pins the scrollbar's
// magnitude corroboration: without a selection anchor, a four-row feature
// claim whose thumb travelled only three rows' worth stays unverified —
// direction alone cannot catch an off-by-one candidate.
func TestScrollEvidenceRejectsOffByRowThumbTravel(t *testing.T) {
	l := collect.MacCloudLayout()
	m := newScrollEvidenceMatcher()
	before := paintScrollFrame(l, 1, 0, -1, 300, 430, false)
	// The content advanced four rows, but the thumb moved only ~78px —
	// three rows' worth at the ~26px/row this thumb geometry implies.
	after := paintScrollFrame(l, 2, 4, -1, 378, 508, true)
	sm := m.MatchScroll(before, after)
	if sm.Displaced || sm.Stationary || sm.Lost || !sm.Undecided {
		t.Fatalf("verdict %+v, want undecided on the off-by-one thumb travel (%s)", sm, sm.Evidence)
	}
	if !strings.Contains(sm.Evidence, "expected ~") {
		t.Fatalf("evidence %q should name the expected travel", sm.Evidence)
	}
	// The same content with a consistent four-row thumb travel verifies.
	consistent := paintScrollFrame(l, 3, 4, -1, 400, 530, true)
	if sm := m.MatchScroll(before, consistent); !sm.Displaced || sm.Rows != 4 {
		t.Fatalf("verdict %+v, want the consistent thumb to corroborate 4 rows (%s)", sm, sm.Evidence)
	}
}

// TestScrollEvidenceCorroboratesSixRowListScroll pins the thumb-geometry
// boundary: a list with exactly one row below the viewport (six total,
// five visible) leaves the derived scrollable-step count at ~1.0, and its
// genuine one-row scroll — the thumb travelling the whole remaining range
// — must still corroborate. Fullness is judged by the thumb's own geometry,
// not by the step count crossing 1.
func TestScrollEvidenceCorroboratesSixRowListScroll(t *testing.T) {
	l := collect.MacCloudLayout()
	m := newScrollEvidenceMatcher()
	// Thumb ~571px of the 685px track ≈ 5/6 of the list: one scrollable
	// row, moving the thumb from the track's top to its bottom.
	before := paintScrollFrame(l, 1, 0, -1, l.ScrollTrack.Min.Y, l.ScrollTrack.Min.Y+570, false)
	after := paintScrollFrame(l, 2, 1, -1, l.ScrollTrack.Min.Y+114, l.ScrollTrack.Max.Y-1, true)
	sm := m.MatchScroll(before, after)
	if !sm.Displaced || sm.Rows != 1 || sm.Stationary || sm.Lost {
		t.Fatalf("verdict %+v, want the one-row displacement corroborated (%s)", sm, sm.Evidence)
	}
}

// TestScrollEvidenceRejectsFullTrackThumb pins the other side of the same
// boundary: a thumb that genuinely fills the track names a list with
// nothing below the viewport, and no displacement claim can be corroborated
// against it.
func TestScrollEvidenceRejectsFullTrackThumb(t *testing.T) {
	l := collect.MacCloudLayout()
	m := newScrollEvidenceMatcher()
	before := paintScrollFrame(l, 1, 0, -1, l.ScrollTrack.Min.Y, l.ScrollTrack.Max.Y-1, false)
	after := paintScrollFrame(l, 2, 1, -1, l.ScrollTrack.Min.Y, l.ScrollTrack.Max.Y-1, true)
	sm := m.MatchScroll(before, after)
	if sm.Displaced || sm.Stationary || sm.Lost || !sm.Undecided {
		t.Fatalf("verdict %+v, want undecided against a full-track thumb (%s)", sm, sm.Evidence)
	}
	if !strings.Contains(sm.Evidence, "fills the track") {
		t.Fatalf("evidence %q should name the full-track rejection", sm.Evidence)
	}
}

// TestScrollEvidenceThresholdReporter pins the optional capability the
// diagnostic margins and the motion detection read.
func TestScrollEvidenceThresholdReporter(t *testing.T) {
	m := newScrollEvidenceMatcher()
	stationary, selection, displaced, margin, infoMin := m.ScrollFeatureThresholds()
	c := collect.MacCloudCalibration()
	if stationary != c.ScrollStationaryNCC || selection != c.ScrollSelectionNCC ||
		displaced != c.ScrollDisplacedNCC || margin != c.ScrollNCCMargin || infoMin != c.ScrollInfoStdMin {
		t.Fatalf("thresholds (%.2f, %.2f, %.2f, %.2f, %.1f), want the calibration's", stationary, selection, displaced, margin, infoMin)
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
