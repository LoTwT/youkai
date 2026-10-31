package collect

import (
	"fmt"
	"image"
	"math"
	"sort"

	"github.com/LoTwT/youkai/internal/platform"
)

// ScrollEvidenceMatcher is the stage 2 scroll judgment (decision §4.3 K2):
// it keeps the wrapped PixelMatcher's page verdicts, detail comparisons and
// scrollbar reads, and upgrades MatchScroll to the layered evidence model.
//
// The selection box's viewport displacement is the primary displacement
// evidence: the drag anchor leaves the last visited slot selected, and F5
// proved the border follows the same item through a drag. The normalized
// correlation of the list viewport over comparable support regions is the
// mandatory auxiliary check, together with a best-versus-runner-up
// competition among whole-row candidates and the scrollbar thumb. The
// grayscale MAD of the wrapped matcher stays as a quality gate — it still
// proves stillness for pixel-identical frames (including uniform ones) and
// still names the out-of-model Lost case — but it may no longer veto a
// displacement the selection box and the scrollbar agree on.
//
// Every feature of one verdict comes from the same frame: the selection
// anchor, the viewport samples and the thumb segment are read once per
// frame and only combined pairwise.
type ScrollEvidenceMatcher struct {
	*PixelMatcher
}

// NewScrollEvidenceMatcher wraps a calibrated pixel matcher with the stage 2
// scroll evidence model. A nil or uncalibrated wrapper reports undecidable
// verdicts, exactly like the wrapped matcher.
func NewScrollEvidenceMatcher(m *PixelMatcher) *ScrollEvidenceMatcher {
	return &ScrollEvidenceMatcher{PixelMatcher: m}
}

// selection states for one frame's border-probe reading.
const (
	selectionNone       = -1
	selectionUnreadable = -2
)

// selectionAnchor reads which grid slot carries the selection border, using
// the same tier thresholds as the page verdict. Exactly one bordered slot is
// a reading; none is a legitimate "the selection scrolled out of the
// viewport"; two carriers or a slot between the tiers is unreadable, and the
// caller must not use the selection as evidence.
func (m *ScrollEvidenceMatcher) selectionAnchor(img *image.RGBA) int {
	if img == nil {
		return selectionUnreadable
	}
	selected := selectionNone
	for i := range m.L.Slots {
		hits := countSelectionPixels(img, m.L.BorderRect(i))
		switch {
		case hits >= m.C.SelectionHitMin:
			if selected != selectionNone {
				return selectionUnreadable
			}
			selected = i
		case hits > m.C.SelectionClearMax:
			// Between the clear and selected tiers: the border's glow flash
			// or an animation frame. The reading proves nothing either way.
			return selectionUnreadable
		}
	}
	return selected
}

// selectionRows converts two unique selection anchors into the signed
// vertical displacement they name, in rows: positive means the content moved
// up (the viewport advanced toward the list's end), matching the shift
// search's dy sign. ok is false when the pair names no vertical
// displacement: same slot, a column change (a drag never moves the
// selection sideways — that reading is garbage), or an unreadable side.
func (m *ScrollEvidenceMatcher) selectionRows(before, after int) (rows int, ok bool) {
	if before < 0 || after < 0 || before == after {
		return 0, false
	}
	r1, c1 := m.L.slotPosition(before)
	r2, c2 := m.L.slotPosition(after)
	if c1 != c2 {
		return 0, false
	}
	return r1 - r2, true
}

// nccCandidate is one whole-row candidate's normalized evidence: the best
// correlation inside the row-tolerance window and the support region's
// grayscale std at the candidate's centre shift.
type nccCandidate struct {
	ncc float64
	std float64
}

// rowCorrelations computes the zero-mean normalized correlation of the two
// frames' list viewports at every whole-row candidate, sampling at the
// calibrated steps. The dynamic selection-border strips of both frames are
// masked out of the comparison: the border's glow animates independently of
// the list and must not count as content change. Each non-zero candidate
// takes the best correlation within its tolerance window, so a one-pixel
// snap does not deflate a true alignment; the zero candidate is evaluated
// at exactly dy=0 without a window — stillness means zero displacement, and
// a windowed best would let a sub-row creep (a settling animation) borrow
// the stationary verdict of a neighbouring shift.
func (m *ScrollEvidenceMatcher) rowCorrelations(before, after *image.RGBA, mask []image.Rectangle) map[int]nccCandidate {
	r := m.L.ListViewport
	out := make(map[int]nccCandidate, 2*m.C.ScrollMaxRows+1)
	inMask := func(x, y int) bool {
		for _, rect := range mask {
			if x >= rect.Min.X && x < rect.Max.X && y >= rect.Min.Y && y < rect.Max.Y {
				return true
			}
		}
		return false
	}
	for row := -m.C.ScrollMaxRows; row <= m.C.ScrollMaxRows; row++ {
		window := m.C.ScrollRowTolerance
		if row == 0 {
			window = 0
		}
		best := nccCandidate{ncc: math.Inf(-1)}
		for dy := row*m.L.RowPitch - window; dy <= row*m.L.RowPitch+window; dy++ {
			var n int
			var sumA, sumB, sumAA, sumBB, sumAB float64
			for y := r.Min.Y; y < r.Max.Y; y += m.C.ScrollStepY {
				beforeY := y + dy
				if beforeY < r.Min.Y || beforeY >= r.Max.Y {
					continue
				}
				for x := r.Min.X; x < r.Max.X; x += m.C.ScrollStepX {
					if inMask(x, y) || inMask(x, beforeY) {
						continue
					}
					a := float64(grayAt(before, x, beforeY))
					b := float64(grayAt(after, x, y))
					n++
					sumA += a
					sumB += b
					sumAA += a * a
					sumBB += b * b
					sumAB += a * b
				}
			}
			if n < m.C.ScrollMinSamples {
				continue
			}
			// Zero-mean normalized correlation over the n samples.
			varA := float64(n)*sumAA - sumA*sumA
			varB := float64(n)*sumBB - sumB*sumB
			if varA <= 0 || varB <= 0 {
				// A uniform support has no correlation to measure; leave the
				// candidate at -inf so the information gate rejects it.
				continue
			}
			ncc := (float64(n)*sumAB - sumA*sumB) / math.Sqrt(varA*varB)
			std := math.Sqrt(varB / float64(n*n))
			if ncc > best.ncc {
				best = nccCandidate{ncc: ncc, std: std}
			}
		}
		if !math.IsInf(best.ncc, -1) {
			out[row] = best
		}
	}
	return out
}

// nccVerdict names the strongest non-zero candidate and the runner-up gap
// against every other candidate (zero included). A missing candidate map
// entry or a non-positive gap means the competition cannot distinguish.
func nccVerdict(cands map[int]nccCandidate, chosen int) (best float64, gap float64, ok bool) {
	chosenCand, have := cands[chosen]
	if !have {
		return 0, 0, false
	}
	rival := math.Inf(-1)
	for row, cand := range cands {
		if row == chosen {
			continue
		}
		if cand.ncc > rival {
			rival = cand.ncc
		}
	}
	if math.IsInf(rival, -1) {
		return chosenCand.ncc, math.Inf(1), true
	}
	return chosenCand.ncc, chosenCand.ncc - rival, true
}

// MatchScroll implements Matcher with the stage 2 evidence model. The
// verdict order is:
//
//  1. Pixel-identical frames are stationary (the wrapped matcher's
//     zero-shift MAD; it also covers uniform content the normalized path
//     must refuse).
//  2. Stillness by normalized correlation, gated by the information floor
//     and the candidate competition, unless the selection box verifiably
//     moved (a moved selection contradicts stillness).
//  3. The selection box's displacement, corroborated by the normalized
//     feature at the named row. A selection pair that names no displacement
//     (same slot) blocks a feature-only displacement claim: the primary
//     evidence contradicts it.
//  4. A feature-only displacement, when no usable selection pair exists:
//     it needs the stronger feature threshold, the competition margin and
//     the scrollbar thumb's corroboration.
//  5. Otherwise the wrapped matcher's MAD search runs for the metrics and
//     the out-of-model Lost rule: content that changed past the lost
//     threshold with no whole-row alignment is Lost; everything else is
//     Undecided.
//
// The absolute MAD thresholds never reject a displacement the selection box
// and the features agree on; they only appear in the evidence prose.
func (m *ScrollEvidenceMatcher) MatchScroll(before, after platform.Frame) ScrollMatch {
	if m == nil || m.PixelMatcher == nil || !m.valid {
		return ScrollMatch{Evidence: "scroll evidence matcher: the layout or calibration is incomplete"}
	}
	if !m.frameFits(before) || !m.frameFits(after) {
		return ScrollMatch{Evidence: fmt.Sprintf(
			"scroll evidence matcher: frames %d → %d are not the calibrated display configuration",
			before.ID, after.ID)}
	}

	err0, ok := m.zeroShiftError(before.Image, after.Image)
	if !ok {
		return ScrollMatch{Evidence: fmt.Sprintf(
			"scroll evidence matcher: frames %d → %d have too few comparable list samples",
			before.ID, after.ID)}
	}

	// Same-frame evidence: selection anchors and thumb segments, each read
	// from one frame only.
	selB := m.selectionAnchor(before.Image)
	selA := m.selectionAnchor(after.Image)
	thumbBStart, thumbBEnd, thumbBOK := m.thumbSegment(before.Image)
	thumbAStart, thumbAEnd, thumbAOK := m.thumbSegment(after.Image)

	metrics := &ScrollMetrics{
		Err0:            err0,
		SelectionBefore: selB,
		SelectionAfter:  selA,
	}

	// 1) Pixel-identical frames prove stillness directly, uniform content
	// included (the normalized path refuses it by design).
	if err0 < m.C.ScrollStationaryMax {
		return ScrollMatch{Stationary: true, Metrics: metrics, Evidence: fmt.Sprintf(
			"frames %d → %d: list viewport error at zero displacement %.2f < %.1f — the list did not move",
			before.ID, after.ID, err0, m.C.ScrollStationaryMax)}
	}

	// The selection pair's displacement claim.
	selRows, selRowsOK := 0, false
	if selB >= 0 && selA >= 0 {
		selRows, selRowsOK = m.selectionRows(selB, selA)
	}
	selectionStill := selB >= 0 && selA >= 0 && selB == selA
	selectionMoved := selRowsOK && selRows != 0

	// 2) and the shared normalized-feature search.
	var mask []image.Rectangle
	for _, slot := range []int{selB, selA} {
		if slot >= 0 {
			mask = append(mask, m.L.BorderRect(slot))
		}
	}
	cands := m.rowCorrelations(before.Image, after.Image, mask)
	metrics.NCCMeasured = true
	metrics.RowNCC = make(map[int]float64, len(cands))
	for row, cand := range cands {
		metrics.RowNCC[row] = cand.ncc
	}
	if cand, have := cands[0]; have {
		metrics.InfoStd = cand.std
	}
	bestRow, bestNCC := 0, math.Inf(-1)
	for row, cand := range cands {
		if row != 0 && cand.ncc > bestNCC {
			bestRow, bestNCC = row, cand.ncc
		}
	}
	if bestRow != 0 {
		metrics.BestNCCRow, metrics.BestNCC = bestRow, bestNCC
	}

	thumbNote := fmt.Sprintf("thumb %d–%d → %d–%d", thumbBStart, thumbBEnd, thumbAStart, thumbAEnd)
	if !thumbBOK || !thumbAOK {
		thumbNote = "thumb unreadable"
	}

	// 2) Stillness by normalized correlation. A selection pair that verifiably
	// moved contradicts stillness; a same-slot pair is consistent with it.
	if cand, have := cands[0]; have && !selectionMoved {
		if _, gap, ok := nccVerdict(cands, 0); ok &&
			cand.ncc >= m.C.ScrollStationaryNCC && gap >= m.C.ScrollNCCMargin && cand.std >= m.C.ScrollInfoStdMin {
			return ScrollMatch{Stationary: true, Metrics: metrics, Evidence: fmt.Sprintf(
				"frames %d → %d: same content by normalized correlation (ncc %.3f ≥ %.2f, runner-up gap %.3f ≥ %.2f, info std %.1f) with zero-shift error %.2f — the list did not move; %s",
				before.ID, after.ID, cand.ncc, m.C.ScrollStationaryNCC, gap, m.C.ScrollNCCMargin, cand.std, err0, thumbNote)}
		}
	}

	// 3) The selection box's displacement: the primary evidence, corroborated
	// by the normalized feature at the named row.
	if selectionMoved {
		if absInt(selRows) >= 1 && absInt(selRows) <= m.C.ScrollMaxRows {
			best, gap, ok := nccVerdict(cands, selRows)
			if _, have := cands[selRows]; have {
				metrics.BestNCCRow, metrics.BestNCC = selRows, best
			}
			if ok && best >= m.C.ScrollSelectionNCC && gap >= m.C.ScrollNCCMargin &&
				cands[selRows].std >= m.C.ScrollInfoStdMin {
				// InfoStd names the support the verdict rests on; a
				// non-accepting verdict keeps the zero-shift std, which the
				// wait's motion detection gates on.
				metrics.InfoStd = cands[selRows].std
				return m.displaced(before.ID, after.ID, selRows, metrics,
					fmt.Sprintf("the selection box moved %s by %d row(s) (slot %d → %d) and the normalized feature at that row agrees (ncc %.3f ≥ %.2f, runner-up gap %.3f ≥ %.2f, info std %.1f; zero-shift error %.2f, best MAD alignment not a gate); %s",
						directionName(selRows), absInt(selRows), selB, selA, best, m.C.ScrollSelectionNCC,
						gap, m.C.ScrollNCCMargin, cands[selRows].std, err0, thumbNote))
			}
			return ScrollMatch{Undecided: true, Metrics: metrics, Evidence: fmt.Sprintf(
				"frames %d → %d: the selection box names a %d-row displacement (slot %d → %d) but the normalized feature does not corroborate it (ncc %.3f vs ≥ %.2f, runner-up gap %.3f vs ≥ %.2f); %s",
				before.ID, after.ID, absInt(selRows), selB, selA, best, m.C.ScrollSelectionNCC, gap, m.C.ScrollNCCMargin, thumbNote)}
		}
		// A selection displacement outside the modelled row range cannot be
		// committed; the features may not silently pick a different row.
		return ScrollMatch{Undecided: true, Metrics: metrics, Evidence: fmt.Sprintf(
			"frames %d → %d: the selection box moved outside the modelled row range (slot %d → %d); %s",
			before.ID, after.ID, selB, selA, thumbNote)}
	}

	// A same-slot selection pair blocks a feature-only displacement: the
	// primary evidence says the selection did not move, and no scroll moves
	// the list without moving the selection with it.
	if selectionStill {
		return ScrollMatch{Undecided: true, Metrics: metrics, Evidence: fmt.Sprintf(
			"frames %d → %d: the content changed (zero-shift error %.2f) but the selection box stayed on slot %d; the changed pixels are not list movement (ncc0 %.3f); %s",
			before.ID, after.ID, err0, selB, metrics.RowNCC[0], thumbNote)}
	}

	// 4) A feature-only displacement: the stronger threshold, the
	// competition margin and the scrollbar's corroboration.
	if bestRow != 0 && absInt(bestRow) <= m.C.ScrollMaxRows {
		_, gap, ok := nccVerdict(cands, bestRow)
		if ok && bestNCC >= m.C.ScrollDisplacedNCC && gap >= m.C.ScrollNCCMargin &&
			cands[bestRow].std >= m.C.ScrollInfoStdMin {
			// The thumb must corroborate both the direction and the
			// magnitude: without a selection anchor the scrollbar is the
			// independent cross-check (K1: fall back to pixels, features or
			// the scrollbar; still insufficient is a lost position). The
			// expected travel is the claimed rows times the per-row thumb
			// travel, derived from the observed thumb geometry itself —
			// thumb length / track length ≈ visible rows / total rows, so
			// per-row travel = (track − thumb) / (total − visible). A fixed
			// per-row constant would be wrong across positions (a 24-row
			// list moves the thumb ~28px per row, a 200-row list ~3px). The
			// tolerance stays below 1/ScrollMaxRows so an off-by-one row
			// claim (at least one row's travel off) cannot pass.
			corroborated, why := m.thumbCorroborates(thumbBStart, thumbBEnd, thumbBOK, thumbAStart, thumbAEnd, thumbAOK, bestRow)
			if corroborated {
				metrics.InfoStd = cands[bestRow].std
				return m.displaced(before.ID, after.ID, bestRow, metrics,
					fmt.Sprintf("the normalized feature alone names a %d-row displacement (ncc %.3f ≥ %.2f, runner-up gap %.3f ≥ %.2f, info std %.1f; no selection anchor: before %d, after %d) and the scrollbar travelled with it (%s); zero-shift error %.2f, best MAD alignment not a gate",
						absInt(bestRow), bestNCC, m.C.ScrollDisplacedNCC, gap, m.C.ScrollNCCMargin, cands[bestRow].std, selB, selA, why, err0))
			}
			return ScrollMatch{Undecided: true, Metrics: metrics, Evidence: fmt.Sprintf(
				"frames %d → %d: the normalized feature names a %d-row displacement (ncc %.3f) but the scrollbar does not corroborate it (%s); without a selection anchor the displacement stays unverified",
				before.ID, after.ID, absInt(bestRow), bestNCC, why)}
		}
	}

	// 5) No layered verdict: run the wrapped matcher's MAD search for the
	// metrics and the out-of-model Lost rule.
	dy, bestErr, rowErrors, ok := m.bestShift(before.Image, after.Image)
	if !ok {
		return ScrollMatch{Undecided: true, Metrics: metrics, Evidence: fmt.Sprintf(
			"frames %d → %d: undecided (zero-shift error %.2f, normalized candidates %s)",
			before.ID, after.ID, err0, formatRowNCC(metrics.RowNCC))}
	}
	metrics.Measured = true
	metrics.BestDy, metrics.BestErr, metrics.RowErrors = dy, bestErr, rowErrors
	// A whole-row alignment in either direction: the MAD search's best
	// shift within the row tolerance and the modelled row range.
	rows, whole := m.wholeRow(dy)
	aligned := whole && absInt(rows) >= 1 && absInt(rows) <= m.C.ScrollMaxRows
	if err0 >= m.C.ScrollLostMin && !aligned {
		return ScrollMatch{Lost: true, Metrics: metrics, Evidence: fmt.Sprintf(
			"frames %d → %d: the list changed (zero-displacement error %.2f ≥ %.1f) but the best shift %d is not a whole row (error %.2f) — the overlap was lost; normalized candidates %s; row candidates %s",
			before.ID, after.ID, err0, m.C.ScrollLostMin, dy, bestErr, formatRowNCC(metrics.RowNCC), formatRowErrors(rowErrors))}
	}
	return ScrollMatch{Undecided: true, Metrics: metrics, Evidence: fmt.Sprintf(
		"frames %d → %d: undecided (zero-shift error %.2f, best shift %d at error %.2f, normalized candidates %s)",
		before.ID, after.ID, err0, dy, bestErr, formatRowNCC(metrics.RowNCC))}
}

// displaced builds the Displaced verdict for a verified signed row count:
// the correspondence set is the uniform whole-row overlap, exactly as the
// wrapped matcher reports it, so applyTransition's validation is unchanged.
func (m *ScrollEvidenceMatcher) displaced(beforeID, afterID uint64, rows int, metrics *ScrollMetrics, why string) ScrollMatch {
	matches := make([]PositionPair, 0, (m.L.Rows-absInt(rows))*m.L.Columns)
	for r := 0; r < m.L.Rows-absInt(rows); r++ {
		for c := 0; c < m.L.Columns; c++ {
			if rows > 0 {
				// The item at before-row r+rows sits at after-row r.
				matches = append(matches, PositionPair{FromRow: r + rows, FromColumn: c, ToRow: r, ToColumn: c})
			} else {
				matches = append(matches, PositionPair{FromRow: r, FromColumn: c, ToRow: r - rows, ToColumn: c})
			}
		}
	}
	return ScrollMatch{
		Displaced: true,
		Rows:      absInt(rows),
		Matches:   matches,
		Metrics:   metrics,
		Evidence:  fmt.Sprintf("frames %d → %d: %s", beforeID, afterID, why),
	}
}

// thumbFullSlack is how far a thumb may fall short of the track's full
// length and still count as filling it. A genuinely full thumb spans the
// whole track (a list with nothing below the viewport); the slack only
// absorbs the segment measurement's couple of pixels of jitter. The
// smallest real non-full thumb — a list with exactly one row below the
// viewport, ~571px of the 685px macOS cloud track — is far outside it.
const thumbFullSlack = 8

// thumbCorroborates checks the scrollbar's independent agreement with a
// feature-only displacement claim, in direction and magnitude. The expected
// travel is the claimed rows times the per-row thumb travel derived from the
// observed thumb geometry itself: thumb length / track length ≈ visible
// rows / total rows, so per-row travel = (track − thumb) / (total −
// visible). A fixed per-row constant would be wrong across positions (a
// 24-row list moves the thumb ~28px per row, a much longer list a few
// pixels), and the derived estimate keeps the check self-contained per
// frame pair. A thumb that fills the track names a list with nothing below
// the viewport, where no displacement claim can be corroborated; fullness is
// judged by the thumb's own geometry, not by the derived step count — a
// six-row list's thumb leaves the step count at ~1.0 despite measurement
// jitter, and its genuine one-row scroll must still corroborate.
func (m *ScrollEvidenceMatcher) thumbCorroborates(beforeStart, beforeEnd int, beforeOK bool, afterStart, afterEnd int, afterOK bool, rows int) (bool, string) {
	if !beforeOK || !afterOK {
		return false, "thumb unreadable"
	}
	trackLen := m.L.ScrollTrack.Dy()
	thumbLen := float64(beforeEnd-beforeStart+1+afterEnd-afterStart+1) / 2
	if thumbLen >= float64(trackLen)-thumbFullSlack {
		return false, fmt.Sprintf("thumb %d–%d → %d–%d fills the track: no scrollable content below the viewport",
			beforeStart, beforeEnd, afterStart, afterEnd)
	}
	// Sign-corrected travel: positive rows mean the content moved up and
	// the thumb moved down (its start increased); negative rows the reverse.
	travel := afterStart - beforeStart
	if rows < 0 {
		travel = -travel
	}
	totalRows := float64(m.L.Rows) * float64(trackLen) / thumbLen
	steps := totalRows - float64(m.L.Rows)
	perRow := (float64(trackLen) - thumbLen) / steps
	expected := perRow * float64(absInt(rows))
	if float64(travel) < float64(m.C.ScrollbarMoveMin) ||
		float64(travel) < expected*(1-m.C.ScrollbarTravelTolerance) ||
		float64(travel) > expected*(1+m.C.ScrollbarTravelTolerance) {
		return false, fmt.Sprintf("thumb %d → %d travelled %dpx, expected ~%.0fpx (≈%.1fpx per row × %d) within ±%.0f%%",
			beforeStart, afterStart, travel, expected, perRow, absInt(rows), m.C.ScrollbarTravelTolerance*100)
	}
	return true, fmt.Sprintf("thumb %d → %d travelled %dpx ≈ %.1fpx per row × %d",
		beforeStart, afterStart, travel, perRow, absInt(rows))
}

// directionName renders a signed row displacement's direction: positive rows
// mean the content moved up (the viewport advanced toward the list's end).
func directionName(rows int) string {
	if rows > 0 {
		return "up"
	}
	return "down"
}

// formatRowNCC renders every whole-row candidate's normalized correlation in
// row order, the normalized counterpart of the MAD row candidates.
func formatRowNCC(rowNCC map[int]float64) string {
	if len(rowNCC) == 0 {
		return "none"
	}
	rows := make([]int, 0, len(rowNCC))
	for row := range rowNCC {
		rows = append(rows, row)
	}
	sort.Ints(rows)
	out := ""
	for i, row := range rows {
		if i > 0 {
			out += " "
		}
		out += fmt.Sprintf("%+d=%.3f", row, rowNCC[row])
	}
	return out
}

// ScrollFeatureThresholds reports the normalized-feature thresholds
// MatchScroll applies, for the diagnostic logs' decision margins and the
// wait's motion detection: the stationary, selection-corroborated and
// feature-only correlation floors, the competition margin, and the
// information floor below which a normalized verdict proves nothing.
func (m *ScrollEvidenceMatcher) ScrollFeatureThresholds() (stationaryNCC, selectionNCC, displacedNCC, nccMargin, infoStdMin float64) {
	if m == nil {
		return 0, 0, 0, 0, 0
	}
	return m.C.ScrollStationaryNCC, m.C.ScrollSelectionNCC, m.C.ScrollDisplacedNCC, m.C.ScrollNCCMargin, m.C.ScrollInfoStdMin
}
