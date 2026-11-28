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
	storagePartitions uint8
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

// correlationAt computes the zero-mean normalized correlation of the two
// frames' list viewports and the after-support's grayscale std at one exact
// displacement dy, sampling at the calibrated steps. The dynamic
// selection-border strips of both frames are masked out of the comparison:
// the border's glow animates independently of the list and must not count as
// content change. A uniform support has no correlation to measure and
// reports ok=false, so the information gate can reject it.
func (m *ScrollEvidenceMatcher) correlationAt(before, after *image.RGBA, dy int, mask []image.Rectangle) (ncc, std float64, ok bool) {
	return m.correlationIn(before, after, m.L.ListViewport, dy, mask)
}

func (m *ScrollEvidenceMatcher) correlationIn(before, after *image.RGBA, r image.Rectangle, dy int, mask []image.Rectangle) (ncc, std float64, ok bool) {
	inMask := func(x, y int) bool {
		for _, rect := range mask {
			if x >= rect.Min.X && x < rect.Max.X && y >= rect.Min.Y && y < rect.Max.Y {
				return true
			}
		}
		return false
	}
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
		return 0, 0, false
	}
	varA := float64(n)*sumAA - sumA*sumA
	varB := float64(n)*sumBB - sumB*sumB
	if varA <= 0 || varB <= 0 {
		return 0, 0, false
	}
	return (float64(n)*sumAB - sumA*sumB) / math.Sqrt(varA*varB),
		math.Sqrt(varB / float64(n*n)), true
}

// zeroShiftCounterEvidence reports whether the pair's zero-shift correlation
// is below the stationary floor on a support informative enough for that
// correlation to be movement evidence. It is the signal the MAD shortcut
// cannot see and is shared by the stillness proof and the wait's motion clock.
func (m *ScrollEvidenceMatcher) zeroShiftCounterEvidence(before, after *image.RGBA, mask []image.Rectangle) (counter bool, ncc, std float64) {
	ncc, std, ok := m.correlationAt(before, after, 0, mask)
	if !ok {
		return false, 0, 0
	}
	return std >= m.C.ScrollInfoStdMin && ncc < m.C.ScrollStationaryNCC, ncc, std
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
	out := make(map[int]nccCandidate, 2*m.C.ScrollMaxRows+1)
	for row := -m.C.ScrollMaxRows; row <= m.C.ScrollMaxRows; row++ {
		window := m.C.ScrollRowTolerance
		if row == 0 {
			window = 0
		}
		best := nccCandidate{ncc: math.Inf(-1)}
		for dy := row*m.L.RowPitch - window; dy <= row*m.L.RowPitch+window; dy++ {
			ncc, std, ok := m.correlationAt(before, after, dy, mask)
			if !ok {
				continue
			}
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

// nccTieEpsilon is the correlation difference below which two whole-row
// candidates are numerically indistinguishable, so the selection box's row is
// not "significantly beaten" and the disagreement goes to the scrollbar's
// independent adjudication instead of being read as a contradiction.
//
// The band is derived from the measurement, not tuned to a case. The
// normalized correlation is accumulated over the calibrated support (at least
// ScrollMinSamples, about 5.5×10⁴ samples on the macOS cloud viewport) of
// 8-bit grayscale pixels. Perturbing a fraction φ of the samples by δ gray
// levels moves the correlation by roughly 2δ√φ/(σ√n); with the information
// floor's σ = 12 that is ≲ 7×10⁻⁴ for a whole-support one-level change and
// ≲ 2×10⁻³ for the few-level codec dither seen on field frames. Float
// round-off over the same accumulation is of order 10⁻¹². A 0.005 band sits
// above that measurement resolution and an order of magnitude below the 0.05
// competition margin, the smallest difference the model treats as a row
// preference; the field's true-versus-neighbouring-row gaps measured
// 0.029–0.038 on 2026-10-31, so no measured preference is absorbed here. On
// genuinely periodic content the old exact comparison made the verdict depend
// on which side of zero the rounding noise fell; this band makes that
// boundary explicit.
const nccTieEpsilon = 0.005

// nccCompetition is the chosen row's normalized evidence against every other
// measured whole-row candidate. rivalRow and rival name the strongest other
// candidate; ties among rivals resolve to the smallest row so the recorded
// evidence does not depend on map iteration order. gap is the chosen
// correlation minus the rival's; it is +Inf when no rival was measured.
type nccCompetition struct {
	chosen   float64
	rivalRow int
	rival    float64
	gap      float64
}

// significantlyBeaten reports whether a rival beats the chosen row by more
// than the tie epsilon. Only then does the feature evidence contradict the
// selection box; at or inside the band the two are indistinguishable and the
// scrollbar may decide.
func (c nccCompetition) significantlyBeaten() bool {
	return c.rival-c.chosen > nccTieEpsilon
}

// nccCompetitionAt measures the chosen row against the strongest other
// candidate. ok is false when the chosen row has no measured candidate.
func nccCompetitionAt(cands map[int]nccCandidate, chosen int) (nccCompetition, bool) {
	chosenCand, have := cands[chosen]
	if !have {
		return nccCompetition{}, false
	}
	rows := make([]int, 0, len(cands)-1)
	for row := range cands {
		if row != chosen {
			rows = append(rows, row)
		}
	}
	sort.Ints(rows)
	comp := nccCompetition{chosen: chosenCand.ncc, rival: math.Inf(-1)}
	for _, row := range rows {
		if cands[row].ncc > comp.rival {
			comp.rivalRow, comp.rival = row, cands[row].ncc
		}
	}
	if math.IsInf(comp.rival, -1) {
		comp.gap = math.Inf(1)
	} else {
		comp.gap = comp.chosen - comp.rival
	}
	return comp, true
}

// nccVerdict names the chosen candidate's correlation and its gap against the
// strongest rival, for the diagnostics and the stillness competition. The
// competition's deterministic rival choice also makes this value independent
// of map iteration order.
func nccVerdict(cands map[int]nccCandidate, chosen int) (best float64, gap float64, ok bool) {
	comp, ok := nccCompetitionAt(cands, chosen)
	if !ok {
		return 0, 0, false
	}
	return comp.chosen, comp.gap, true
}

// stationaryEvidence reports whether the zero-shift candidate passes the
// stationary correlation, competition-margin and information gates. It is the
// single definition of normalized stillness shared by MatchScroll's Stationary
// verdict and the traversal's zero-displacement proofs; a repeated-row list
// can tie the zero candidate with a neighbouring row, and that tie (gap below
// the margin) is not proof of stillness.
func (m *ScrollEvidenceMatcher) stationaryEvidence(cands map[int]nccCandidate) (nccCandidate, nccCompetition, bool) {
	cand, have := cands[0]
	if !have {
		return nccCandidate{}, nccCompetition{}, false
	}
	comp, ok := nccCompetitionAt(cands, 0)
	if !ok {
		return nccCandidate{}, nccCompetition{}, false
	}
	if cand.ncc < m.C.ScrollStationaryNCC || comp.gap < m.C.ScrollNCCMargin || cand.std < m.C.ScrollInfoStdMin {
		return cand, comp, false
	}
	return cand, comp, true
}

// MatchScroll implements Matcher with the stage 2 evidence model. The
// verdict order is:
//
//  1. Pixel-identical frames are stationary (the wrapped matcher's
//     zero-shift MAD; it also covers uniform content the normalized path
//     must refuse), unless the selection box or the scrollbar verifiably
//     moved. Repeated rows can make a whole-row move almost invisible to
//     this MAD, so the primary evidence must be allowed to veto it.
//  2. Stillness by normalized correlation, gated by the information floor
//     and the candidate competition, unless the selection box or the
//     scrollbar verifiably moved (either contradicts stillness).
//  3. The selection box's displacement, corroborated by the normalized
//     feature at the named row. The named row must clear the correlation
//     and information floors, and no candidate may beat it by more than the
//     tie epsilon. A runner-up gap at or above the competition margin
//     accepts directly; a gap inside the margin — a tie within the epsilon
//     included — is accepted only when the scrollbar's independently derived
//     travel agrees in direction and magnitude and also rules out the travel
//     of every same-direction row the features cannot exclude. The margin
//     therefore still governs the feature evidence itself, while the
//     homogeneous-row case gets a second, independent witness; the
//     distinguishability requirement stops a thumb that is equally consistent
//     with a competing row count from deciding. A selection pair that names
//     no displacement (same slot) blocks a feature-only displacement claim:
//     the primary evidence contradicts it.
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
	// The method value belongs to the same PixelMatcher used by this pair.
	// The pair may reject the geometry before ever invoking it.
	var page func(platform.Frame) PageMatch
	if m != nil && m.PixelMatcher != nil {
		page = m.PixelMatcher.Match
	}
	return m.matchScrollWithPage(before, after, page)
}

func (m *ScrollEvidenceMatcher) matchScrollWithPage(before, after platform.Frame, page func(platform.Frame) PageMatch) ScrollMatch {
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

	// The selection pair's displacement claim and the scrollbar's movement
	// evidence are needed before the MAD stillness shortcut: on a list whose
	// rows repeat, a real whole-row move can leave the zero-shift MAD far
	// below the stationary threshold, and only the anchors name it.
	selRows, selRowsOK := 0, false
	if selB >= 0 && selA >= 0 {
		selRows, selRowsOK = m.selectionRows(selB, selA)
	}
	selectionKnown := selB >= 0 && selA >= 0
	selectionStill := selectionKnown && selB == selA
	selectionMoved := selRowsOK && selRows != 0
	// A readable pair that is neither still nor a vertical row change is a
	// conflict, not a missing anchor. Treating it as missing would let the
	// no-selection path accept a scroll that the observed selection
	// contradicts.
	selectionConflict := selectionKnown && !selectionStill && !selectionMoved
	thumbMoved := m.thumbMoved(
		thumbReading{start: thumbBStart, end: thumbBEnd, ok: thumbBOK},
		thumbReading{start: thumbAStart, end: thumbAEnd, ok: thumbAOK})

	metrics := &ScrollMetrics{
		Err0:            err0,
		SelectionBefore: selB,
		SelectionAfter:  selA,
	}
	if m.L.Page == PageDriveStorage && selectionConflict {
		// Two readable anchors that did not move along a column are a
		// positive contradiction of a vertical list scroll. This must veto
		// even a MAD-level Stationary/interprets-as-still path; it must not
		// be relabelled as "no anchor".
		return ScrollMatch{Undecided: true, SelectionConflict: true, Metrics: metrics, Evidence: fmt.Sprintf(
			"frames %d → %d: both selection anchors are readable but the selection changed from slot %d to %d without a vertical row correspondence; the observed selection contradicts a column scroll",
			before.ID, after.ID, selB, selA)}
	}

	// 1) Pixel-identical frames prove stillness directly, uniform content
	// included (the normalized path refuses it by design). A definite
	// selection or thumb movement vetoes that proof: repeated rows make a
	// whole-row move nearly invisible to this MAD, so the primary evidence
	// must decide instead of being skipped here.
	if err0 < m.C.ScrollStationaryMax && !selectionMoved && !thumbMoved {
		return ScrollMatch{Stationary: true, Metrics: metrics, Evidence: fmt.Sprintf(
			"frames %d → %d: list viewport error at zero displacement %.2f < %.1f — the list did not move",
			before.ID, after.ID, err0, m.C.ScrollStationaryMax)}
	}

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
	for row := -m.C.ScrollMaxRows; row <= m.C.ScrollMaxRows; row++ {
		cand, have := cands[row]
		if !have || row == 0 {
			continue
		}
		if cand.ncc > bestNCC {
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
	// moved contradicts stillness; a measurable thumb movement is the
	// independent contradiction. A same-slot pair is consistent with it.
	if cand, comp, ok := m.stationaryEvidence(cands); ok && !selectionMoved && !thumbMoved {
		return ScrollMatch{Stationary: true, Metrics: metrics, Evidence: fmt.Sprintf(
			"frames %d → %d: same content by normalized correlation (ncc %.3f ≥ %.2f, runner-up gap %.3f ≥ %.2f, info std %.1f) with zero-shift error %.2f — the list did not move; %s",
			before.ID, after.ID, cand.ncc, m.C.ScrollStationaryNCC, comp.gap, m.C.ScrollNCCMargin, cand.std, err0, thumbNote)}
	}

	// Drive storage with a visible selection uses the same independent
	// witness as the no-selection path: a physical-direction thumb travel
	// plus the card-badge overlap. The selected card is not allowed to
	// bypass that witness, and the repeated-grid zero-shift correlation is
	// not allowed to overrule a selection whose direction the thumb and
	// overlap already prove.
	if m.L.Page == PageDriveStorage && selectionMoved {
		beforeThumb := thumbReading{start: thumbBStart, end: thumbBEnd, ok: thumbBOK}
		afterThumb := thumbReading{start: thumbAStart, end: thumbAEnd, ok: thumbAOK}
		match, ok := m.storageScrollEvidence(before, after, beforeThumb, afterThumb, metrics, false, page)
		if !ok {
			return ScrollMatch{Undecided: true, Metrics: metrics, Evidence: fmt.Sprintf(
				"frames %d → %d: the selection box and the card overlap did not produce one consistent displacement (zero-shift error %.2f, ncc0 %.3f); %s",
				before.ID, after.ID, err0, metrics.RowNCC[0], thumbNote)}
		}
		storageRows := match.Rows
		if len(match.Matches) > 0 && match.Matches[0].FromRow < match.Matches[0].ToRow {
			storageRows = -storageRows
		}
		if storageRows != selRows {
			return ScrollMatch{Undecided: true, SelectionConflict: true, Metrics: metrics, Evidence: fmt.Sprintf(
				"frames %d → %d: the selection box names %+d row(s) but the thumb and card overlap name %+d; the witnesses contradict each other; %s",
				before.ID, after.ID, selRows, storageRows, thumbNote)}
		}
		return match
	}

	// 3) The selection box's displacement: the primary evidence, corroborated
	// by the normalized feature at the named row.
	if selectionMoved {
		if absInt(selRows) >= 1 && absInt(selRows) <= m.C.ScrollMaxRows {
			comp, ok := nccCompetitionAt(cands, selRows)
			if !ok {
				return ScrollMatch{Undecided: true, Metrics: metrics, Evidence: fmt.Sprintf(
					"frames %d → %d: the selection box names a %d-row displacement (slot %d → %d) but no whole-row candidate could be measured at that displacement; %s",
					before.ID, after.ID, absInt(selRows), selB, selA, thumbNote)}
			}
			std := cands[selRows].std
			// Diagnostics name the row the evidence supports: the selection's
			// row unless a candidate beats it by more than the tie epsilon.
			// The epsilon is what makes that choice independent of map order
			// and of which side of zero the rounding noise falls on.
			if comp.significantlyBeaten() {
				metrics.BestNCCRow, metrics.BestNCC = comp.rivalRow, comp.rival
			} else {
				metrics.BestNCCRow, metrics.BestNCC = selRows, comp.chosen
			}
			switch {
			case comp.chosen < m.C.ScrollSelectionNCC:
				return ScrollMatch{Undecided: true, Metrics: metrics, Evidence: fmt.Sprintf(
					"frames %d → %d: the selection box names a %d-row displacement (slot %d → %d) but the normalized feature at that row is below the selection floor (ncc %.3f < %.2f); %s",
					before.ID, after.ID, absInt(selRows), selB, selA, comp.chosen, m.C.ScrollSelectionNCC, thumbNote)}
			case std < m.C.ScrollInfoStdMin:
				return ScrollMatch{Undecided: true, Metrics: metrics, Evidence: fmt.Sprintf(
					"frames %d → %d: the selection box names a %d-row displacement (slot %d → %d) but the support at that row is too uniform for a normalized verdict (info std %.1f < %.1f); %s",
					before.ID, after.ID, absInt(selRows), selB, selA, std, m.C.ScrollInfoStdMin, thumbNote)}
			case comp.significantlyBeaten():
				// Another row beats the selection's row by more than the tie
				// epsilon: the two primary feature evidence sources contradict
				// each other, and the scrollbar may not break that
				// contradiction.
				return ScrollMatch{Undecided: true, Metrics: metrics, Evidence: fmt.Sprintf(
					"frames %d → %d: the selection box names a %d-row displacement (slot %d → %d) but the normalized feature prefers another row (best %+d:%.3f, %.3f above the named row's %.3f, more than the %.3f tie epsilon); the selection and features contradict each other; %s",
					before.ID, after.ID, absInt(selRows), selB, selA, comp.rivalRow, comp.rival, comp.rival-comp.chosen, comp.chosen, nccTieEpsilon, thumbNote)}
			case comp.gap >= m.C.ScrollNCCMargin:
				// InfoStd names the support the verdict rests on; a
				// non-accepting verdict keeps the zero-shift std, which the
				// wait's motion detection gates on.
				metrics.InfoStd = std
				return m.displaced(before.ID, after.ID, selRows, ScrollBasisSelectionFeature, metrics,
					fmt.Sprintf("the selection box moved %s by %d row(s) (slot %d → %d) and the normalized feature at that row agrees (ncc %.3f ≥ %.2f, runner-up gap %.3f ≥ %.2f, info std %.1f; zero-shift error %.2f, best MAD alignment not a gate); %s",
						directionName(selRows), absInt(selRows), selB, selA, comp.chosen, m.C.ScrollSelectionNCC,
						comp.gap, m.C.ScrollNCCMargin, std, err0, thumbNote))
			}
			// The named row is not significantly beaten and clears the
			// correlation and information floors, but the competition margin
			// is short. A four-row step leaves only one row of overlap, while a
			// smaller shift can compare two adjacent rows; in a homogeneous
			// list the runner-up therefore needs an independent tiebreaker.
			// The scrollbar's travel is that witness; without it the
			// gap-limited match stays unverified (the 0.05 margin itself is
			// unchanged).
			beforeThumb := thumbReading{start: thumbBStart, end: thumbBEnd, ok: thumbBOK}
			afterThumb := thumbReading{start: thumbAStart, end: thumbAEnd, ok: thumbAOK}
			travel, corroborated, why := m.thumbTravelOf(beforeThumb, afterThumb, selRows)
			if corroborated {
				// A thumb that also fits a competing row count cannot choose
				// between them: accepting the selection's row there would rest
				// on a coincidence rather than on independent evidence. Only
				// same-direction rivals matter — the thumb's direction already
				// agrees with the selection, and an opposite displacement would
				// need the thumb to move the other way.
				if rivalRow, rivalTravel, ambiguous := m.thumbAmbiguousRival(travel, cands, selRows, comp.chosen); ambiguous {
					return ScrollMatch{Undecided: true, Metrics: metrics, Evidence: fmt.Sprintf(
						"frames %d → %d: the selection box names a %d-row displacement (slot %d → %d) and the runner-up gap %.3f is below the %.2f competition margin; the scrollbar travelled %dpx, which fits the named row (~%.0fpx) but also the competing %+d-row candidate (~%.0fpx) within ±%.0f%%, so it cannot choose between them",
						before.ID, after.ID, absInt(selRows), selB, selA, comp.gap, m.C.ScrollNCCMargin,
						travel.moved, travel.expected(selRows), rivalRow, rivalTravel, m.C.ScrollbarTravelTolerance*100)}
				}
				metrics.InfoStd = std
				return m.displaced(before.ID, after.ID, selRows, ScrollBasisSelectionFeatureThumb, metrics,
					fmt.Sprintf("the selection box moved %s by %d row(s) (slot %d → %d); no whole-row candidate beats the named row by more than the %.3f tie epsilon and it clears the correlation and information floors (ncc %.3f ≥ %.2f, info std %.1f), but the runner-up gap %.3f is below the %.2f competition margin; the scrollbar independently corroborated the displacement in direction and magnitude, distinguishable from every competing row count (%s), so the gap-limited match is accepted on that independent evidence; zero-shift error %.2f, best MAD alignment not a gate",
						directionName(selRows), absInt(selRows), selB, selA, nccTieEpsilon, comp.chosen, m.C.ScrollSelectionNCC, std,
						comp.gap, m.C.ScrollNCCMargin, why, err0))
			}
			return ScrollMatch{Undecided: true, Metrics: metrics, Evidence: fmt.Sprintf(
				"frames %d → %d: the selection box names a %d-row displacement (slot %d → %d) and the normalized feature at that row clears its floors (ncc %.3f ≥ %.2f, info std %.1f), but the runner-up gap %.3f is below the %.2f competition margin and the scrollbar does not corroborate the displacement (%s); the gap alone cannot verify the row",
				before.ID, after.ID, absInt(selRows), selB, selA, comp.chosen, m.C.ScrollSelectionNCC, std,
				comp.gap, m.C.ScrollNCCMargin, why)}
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
	// The drive-storage grid repeats the same card art and level text, so
	// the normalized feature can prefer zero shift after a real one-row
	// move (the 2026-11-22 handoff: ncc0 0.945 vs +1 0.922). When the
	// selected card has scrolled out there is no selection anchor either.
	// The scrollbar's geometry-derived travel and the card slot badges'
	// overlap are independent evidence; use them only when both name one
	// unique row count, and never guess when two counts fit.
	if m.L.Page == PageDriveStorage && !selectionMoved {
		beforeThumb := thumbReading{start: thumbBStart, end: thumbBEnd, ok: thumbBOK}
		afterThumb := thumbReading{start: thumbAStart, end: thumbAEnd, ok: thumbAOK}
		if m.storagePartitions != 0 {
			if match, ok := m.filteredStorageScroll(before, after, beforeThumb, afterThumb, metrics); ok {
				return match
			}
		}
		if match, ok := m.storageScrollEvidence(before, after, beforeThumb, afterThumb, metrics, true, page); ok {
			return match
		}
		// Falling through to the feature-only path would let the repeated
		// card grid's normalized best row choose a displacement without the
		// independent badge witness. Stay undecided instead; the wait will
		// stop safely rather than skip or double-count a row.
		return ScrollMatch{Undecided: true, Metrics: metrics, Evidence: fmt.Sprintf(
			"frames %d → %d: no unique scrollbar-travel and card-slot overlap witness for the drive-storage displacement (zero-shift error %.2f, ncc0 %.3f); refusing to fall back to the repeated-grid normalized feature; %s",
			before.ID, after.ID, err0, metrics.RowNCC[0], thumbNote)}
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
			beforeThumb := thumbReading{start: thumbBStart, end: thumbBEnd, ok: thumbBOK}
			afterThumb := thumbReading{start: thumbAStart, end: thumbAEnd, ok: thumbAOK}
			_, corroborated, why := m.thumbTravelOf(beforeThumb, afterThumb, bestRow)
			if corroborated {
				metrics.InfoStd = cands[bestRow].std
				return m.displaced(before.ID, after.ID, bestRow, ScrollBasisFeatureThumb, metrics,
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

// storageScrollEvidence resolves a drive-storage scroll from two
// independent facts: the thumb's geometry-derived travel and the card
// slot badges visible on both sides of the overlap. The normalized pixel
// feature is deliberately not used as the decider here: the 9×4 cards
// carry the same art and level text, so a real one-row move can leave the
// zero-shift correlation higher than the shifted rows. Exactly one row
// count must fit both witnesses; zero or multiple fits stay unverified.
// requireChange is true for the no-selection branch, where the destination
// badges must prove the grid moved; a consistent selection supplies that
// independent instance evidence and may sit on a same-column repeated
// badge pattern.
func (m *ScrollEvidenceMatcher) storageScrollEvidence(before, after platform.Frame, thumbB, thumbA thumbReading, metrics *ScrollMetrics, requireChange bool, page func(platform.Frame) PageMatch) (ScrollMatch, bool) {
	beforePage := page(before)
	afterPage := page(after)
	if beforePage.Page != PageDriveStorage || afterPage.Page != PageDriveStorage ||
		beforePage.List == nil || afterPage.List == nil ||
		len(beforePage.List.Positions) != len(m.L.Slots) ||
		len(afterPage.List.Positions) != len(m.L.Slots) ||
		!thumbB.ok || !thumbA.ok {
		return ScrollMatch{}, false
	}
	type candidate struct {
		rows int
	}
	var fits []candidate
	// A wheel advances the list down; returning to the top scrolls it up.
	// Both directions use the same badge-overlap witness, with the signed
	// row count also checked against the thumb's independent travel.
	for _, direction := range []int{1, -1} {
		for rows := 1; rows <= m.L.Rows-1; rows++ {
			signed := rows * direction
			if _, ok, _ := m.thumbTravelOfStrict(thumbB, thumbA, signed); !ok {
				continue
			}
			occupied, matched, changed := 0, 0, 0
			valid := true
			for r := 0; r < m.L.Rows-rows && valid; r++ {
				for c := 0; c < m.L.Columns; c++ {
					fromRow, toRow := r+rows, r
					if direction < 0 {
						fromRow, toRow = r, r+rows
					}
					from := fromRow*m.L.Columns + c
					to := toRow*m.L.Columns + c
					beforeOccupied := beforePage.List.Occupied[from]
					afterOccupied := afterPage.List.Occupied[to]
					if beforeOccupied != afterOccupied {
						valid = false
						break
					}
					// A repeated slot number is not an instance identity.
					// Require the destination cell itself to show a change
					// from the before frame; a static same-slot grid must not
					// satisfy a displacement the list never made.
					bd := beforePage.List.Positions[to]
					ad := afterPage.List.Positions[to]
					if (!bd.known() && ad.known()) ||
						(bd.known() && ad.known() && bd.Position != ad.Position) {
						changed++
					}
					if !beforeOccupied {
						continue
					}
					occupied++
					bp := beforePage.List.Positions[from]
					ap := afterPage.List.Positions[to]
					if !bp.known() || !ap.known() || bp.Position != ap.Position {
						valid = false
						break
					}
					matched++
				}
			}
			// At least one occupied overlap cell must carry the same known
			// slot badge on both sides. Without a readable selection the
			// destination grid must also change somewhere; with an already
			// proven selection, a same-column repeated badge pattern is
			// allowed because the selection, thumb and every occupied
			// overlap cell agree on the row. An all-empty overlap still
			// leaves only the scrollbar as evidence and is rejected.
			if valid && occupied > 0 && matched == occupied && (!requireChange || changed > 0) {
				fits = append(fits, candidate{rows: signed})
			}
		}
	}
	if len(fits) != 1 {
		return ScrollMatch{}, false
	}
	rows := fits[0].rows
	return m.displaced(before.ID, after.ID, rows, ScrollBasisStoragePositions, metrics,
		fmt.Sprintf("the scrollbar travel and the card slot badges' overlap both name the same %d-row displacement", rows)), true
}

// displaced builds the Displaced verdict for a verified signed row count:
// the correspondence set is the uniform whole-row overlap, exactly as the
// wrapped matcher reports it, so applyTransition's validation is unchanged.
// basis names the evidence combination that accepted the displacement.
func (m *ScrollEvidenceMatcher) displaced(beforeID, afterID uint64, rows int, basis string, metrics *ScrollMetrics, why string) ScrollMatch {
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
		Basis:     basis,
		Metrics:   metrics,
		Evidence:  fmt.Sprintf("frames %d → %d: %s", beforeID, afterID, why),
	}
}

// zeroDisplacementProven reports whether before → after proves the list
// stayed at the same position strongly enough to rebase a scroll retry or to
// accept a settling confirmation. It is the shared zero-displacement
// semantics behind those decisions:
//
//   - the same immutable frame content proves it directly;
//   - a definite selection-box displacement or scrollbar translation vetoes
//     it (the scrollbar must move at both edges by the same measurable
//     travel; a single edge changing while the other stays is a boundary
//     measurement artifact, such as the Windows bottom capture reading the
//     thumb's leading edge two pixels low);
//   - a readable selection box on the same slot with a zero-shift error
//     below the stationary threshold, no definite thumb translation and no
//     information-rich normalized movement counter-evidence proves it: the
//     border follows its item through a drag, so a whole-row move would move
//     the border's slot (or make the border unreadable); the normalized
//     zero-shift check still rejects a sub-row creep that stays inside the
//     probe and under the MAD threshold;
//   - otherwise a unique zero-shift normalized candidate must pass the
//     stationary correlation, competition-margin and information floors. A
//     repeated-row tie leaves the zero candidate indistinguishable from a
//     whole-row move and proves nothing.
//
// A zero-shift MAD alone — even exactly zero — never proves stillness here:
// on repeated rows a real whole-row move can keep it at (or near) zero. That
// also makes the MAD-only Stationary verdict insufficient for a retry
// baseline, even though it remains a valid deadline verdict.
func (m *ScrollEvidenceMatcher) zeroDisplacementProven(before, after platform.Frame) (bool, string) {
	if sameFrameContent(before, after) {
		return true, "the same immutable frame content"
	}
	if m == nil || m.PixelMatcher == nil || !m.valid {
		return false, "scroll evidence matcher: the layout or calibration is incomplete"
	}
	if !m.frameFits(before) || !m.frameFits(after) {
		return false, fmt.Sprintf("frames %d → %d are not the calibrated display configuration", before.ID, after.ID)
	}
	err0, ok := m.zeroShiftError(before.Image, after.Image)
	if !ok {
		return false, fmt.Sprintf("frames %d → %d have too few comparable list samples", before.ID, after.ID)
	}

	selB := m.selectionAnchor(before.Image)
	selA := m.selectionAnchor(after.Image)
	thumbBStart, thumbBEnd, thumbBOK := m.thumbSegment(before.Image)
	thumbAStart, thumbAEnd, thumbAOK := m.thumbSegment(after.Image)
	beforeThumb := thumbReading{start: thumbBStart, end: thumbBEnd, ok: thumbBOK}
	afterThumb := thumbReading{start: thumbAStart, end: thumbAEnd, ok: thumbAOK}

	selectionMoved := false
	if selB >= 0 && selA >= 0 {
		if rows, ok := m.selectionRows(selB, selA); ok && rows != 0 {
			selectionMoved = true
		}
	}
	if m.L.Page == PageDriveStorage && selB >= 0 && selA >= 0 && selB != selA {
		if _, ok := m.selectionRows(selB, selA); !ok {
			// A readable cross-column/non-vertical selection pair is a
			// positive conflict, not an absent anchor. It cannot prove
			// stillness, and it must not let the caller settle a pending
			// candidate as if the confirmation pane had stayed put.
			return false, fmt.Sprintf(
				"frames %d → %d: both selection anchors are readable but the selection changed from slot %d to %d without a vertical row correspondence; a cross-column confirmation cannot prove stillness",
				before.ID, after.ID, selB, selA)
		}
	}
	if selectionMoved {
		return false, fmt.Sprintf(
			"frames %d → %d: the selection box moved (slot %d → %d), so the list did not stay at one position (zero-shift error %.2f)",
			before.ID, after.ID, selB, selA, err0)
	}
	if m.L.Page == PageDriveStorage && !m.thumbStationary(beforeThumb, afterThumb) {
		return false, fmt.Sprintf(
			"frames %d → %d: the scrollbar thumb moved at an edge (%d–%d → %d–%d), so boundary stillness is not proven (zero-shift error %.2f)",
			before.ID, after.ID, thumbBStart, thumbBEnd, thumbAStart, thumbAEnd, err0)
	}
	if m.thumbMoved(beforeThumb, afterThumb) {
		return false, fmt.Sprintf(
			"frames %d → %d: the scrollbar thumb verifiably travelled (%d–%d → %d–%d), so the list may have moved (zero-shift error %.2f)",
			before.ID, after.ID, thumbBStart, thumbBEnd, thumbAStart, thumbAEnd, err0)
	}
	// The masked border strips are shared by the same-slot check's movement
	// counter-evidence and the full normalized candidates below.
	var mask []image.Rectangle
	for _, slot := range []int{selB, selA} {
		if slot >= 0 {
			mask = append(mask, m.L.BorderRect(slot))
		}
	}
	if selB >= 0 && selA == selB && err0 < m.C.ScrollStationaryMax {
		// A same-slot border with a low MAD is not enough on its own: a
		// sub-row creep (8/16px of a settling animation) can stay inside the
		// border probe and below the MAD threshold while the zero-shift
		// correlation collapses on an information-rich support. That
		// correlation is the movement counter-evidence the wait already uses
		// to start its motion clock; the proof must honour it too.
		if counter, ncc, std := m.zeroShiftCounterEvidence(before.Image, after.Image, mask); counter {
			return false, fmt.Sprintf(
				"frames %d → %d: the selection box stayed on slot %d and the zero-shift MAD is below %.1f, but the information-rich zero-shift correlation %.3f < %.2f (info std %.1f) is movement counter-evidence — the list moved sub-row",
				before.ID, after.ID, selB, m.C.ScrollStationaryMax, ncc, m.C.ScrollStationaryNCC, std)
		}
		return true, fmt.Sprintf(
			"frames %d → %d: the selection box stayed on slot %d with no measurable thumb travel and a zero-shift error below the stationary threshold; the border follows its item, so a whole-row move would have changed the slot (zero-shift error %.2f)",
			before.ID, after.ID, selB, err0)
	}

	cands := m.rowCorrelations(before.Image, after.Image, mask)
	if cand, comp, ok := m.stationaryEvidence(cands); ok {
		return true, fmt.Sprintf(
			"frames %d → %d: the unique zero-shift candidate passes the stationary gates (ncc %.3f ≥ %.2f, runner-up gap %.3f ≥ %.2f, info std %.1f ≥ %.1f; zero-shift error %.2f)",
			before.ID, after.ID, cand.ncc, m.C.ScrollStationaryNCC, comp.gap, m.C.ScrollNCCMargin,
			cand.std, m.C.ScrollInfoStdMin, err0)
	}
	if cand, have := cands[0]; have {
		comp, _ := nccCompetitionAt(cands, 0)
		return false, fmt.Sprintf(
			"frames %d → %d: no zero-displacement proof (zero-shift error %.2f; ncc %.3f vs %.2f, runner-up gap %.3f vs %.2f, info std %.1f vs %.1f; selection before %d after %d)",
			before.ID, after.ID, err0, cand.ncc, m.C.ScrollStationaryNCC, comp.gap, m.C.ScrollNCCMargin,
			cand.std, m.C.ScrollInfoStdMin, selB, selA)
	}
	return false, fmt.Sprintf(
		"frames %d → %d: no zero-displacement proof (zero-shift error %.2f; no normalized candidate could be measured; selection before %d after %d)",
		before.ID, after.ID, err0, selB, selA)
}

// zeroShiftMovement reports whether before → after carries observable
// movement evidence that the MAD-level Stationary shortcut cannot see: a
// definite selection or thumb translation, or an information-rich zero-shift
// correlation below the stationary floor. The wait keeps its "unmoved" fact
// false after this returns true; a later frame returning to the baseline does
// not erase the observation.
func (m *ScrollEvidenceMatcher) zeroShiftMovement(before, after platform.Frame) (bool, string) {
	if m == nil || m.PixelMatcher == nil || !m.valid {
		return false, "scroll evidence matcher: the layout or calibration is incomplete"
	}
	if !m.frameFits(before) || !m.frameFits(after) {
		return false, fmt.Sprintf("frames %d → %d are not the calibrated display configuration", before.ID, after.ID)
	}
	selB := m.selectionAnchor(before.Image)
	selA := m.selectionAnchor(after.Image)
	if selB >= 0 && selA >= 0 {
		if rows, ok := m.selectionRows(selB, selA); ok && rows != 0 {
			return true, fmt.Sprintf("frames %d → %d: the selection box moved (slot %d → %d)",
				before.ID, after.ID, selB, selA)
		}
	}
	thumbBStart, thumbBEnd, thumbBOK := m.thumbSegment(before.Image)
	thumbAStart, thumbAEnd, thumbAOK := m.thumbSegment(after.Image)
	if m.thumbMoved(
		thumbReading{start: thumbBStart, end: thumbBEnd, ok: thumbBOK},
		thumbReading{start: thumbAStart, end: thumbAEnd, ok: thumbAOK}) {
		return true, fmt.Sprintf("frames %d → %d: the scrollbar thumb verifiably travelled (%d–%d → %d–%d)",
			before.ID, after.ID, thumbBStart, thumbBEnd, thumbAStart, thumbAEnd)
	}
	var mask []image.Rectangle
	for _, slot := range []int{selB, selA} {
		if slot >= 0 {
			mask = append(mask, m.L.BorderRect(slot))
		}
	}
	if counter, ncc, std := m.zeroShiftCounterEvidence(before.Image, after.Image, mask); counter {
		return true, fmt.Sprintf(
			"frames %d → %d: the information-rich zero-shift correlation %.3f < %.2f (info std %.1f) contradicts stillness",
			before.ID, after.ID, ncc, m.C.ScrollStationaryNCC, std)
	}
	return false, fmt.Sprintf("frames %d → %d: no observable movement counter-evidence", before.ID, after.ID)
}

// thumbFullSlack is how far a thumb may fall short of the track's full
// length and still count as filling it. A genuinely full thumb spans the
// whole track (a list with nothing below the viewport); the slack only
// absorbs the segment measurement's couple of pixels of jitter. The
// smallest real non-full thumb — a list with exactly one row below the
// viewport, ~571px of the 685px macOS cloud track — is far outside it.
const thumbFullSlack = 8

// thumbReading is one frame's scrollbar thumb segment on the track, in frame
// pixels; ok is false when no segment could be read.
type thumbReading struct {
	start, end int
	ok         bool
}

// thumbTranslation returns the thumb's signed travel when both segment edges
// moved in the same direction; the smaller magnitude is the travel both edges
// agree on. A single edge changing while the other stays is a boundary or
// measurement artifact, not travel: the Windows bottom capture reads the
// thumb's leading edge two pixels low while its trailing edge and the list
// pixels stay put (368–931 → 370–931). A real translation moves both edges
// together, so only that shape is definite movement evidence.
func thumbTranslation(before, after thumbReading) (travel int, ok bool) {
	if !before.ok || !after.ok {
		return 0, false
	}
	startTravel := after.start - before.start
	endTravel := after.end - before.end
	if startTravel == 0 || endTravel == 0 {
		return 0, false
	}
	if (startTravel < 0) != (endTravel < 0) {
		return 0, false
	}
	if absInt(endTravel) < absInt(startTravel) {
		return endTravel, true
	}
	return startTravel, true
}

// thumbMoved reports whether two readable thumb segments prove the thumb
// travelled along the track by at least the calibrated movement floor. It is
// direction-independent: any measurable travel contradicts a claim that the
// list stayed put. The travel must show at both edges (see thumbTranslation);
// a lone edge change is not definite movement. A thumb that fills the track
// has nothing to travel over, so its jitter proves nothing and is ignored.
func (m *ScrollEvidenceMatcher) thumbMoved(before, after thumbReading) bool {
	if !before.ok || !after.ok {
		return false
	}
	trackLen := m.L.ScrollTrack.Dy()
	thumbLen := float64(before.end-before.start+1+after.end-after.start+1) / 2
	if thumbLen >= float64(trackLen)-thumbFullSlack {
		return false
	}
	travel, translated := thumbTranslation(before, after)
	return translated && absInt(travel) >= m.C.ScrollbarMoveMin
}

// thumbStationary reports that neither thumb edge moved by more than the
// calibrated movement floor. It is the boundary-stillness predicate: a
// single moving edge is a measurement artifact for proving a row, but it is
// still a contradiction of "the thumb did not move" and must not be ignored
// when top/bottom stillness is being confirmed. The floor absorbs the 2 px
// jitter seen at the Windows bottom capture; a 6 px edge move is real motion.
func (m *ScrollEvidenceMatcher) thumbStationary(before, after thumbReading) bool {
	if !before.ok || !after.ok {
		return false
	}
	return absInt(after.start-before.start) <= m.C.ScrollbarMoveMin &&
		absInt(after.end-before.end) <= m.C.ScrollbarMoveMin
}

// thumbTravel is the scrollbar's independently measured travel for a claimed
// row count: the signed pixels the thumb moved and the per-row travel the
// observed thumb geometry implies.
type thumbTravel struct {
	// moved is positive when the thumb moved down the track, sign-corrected
	// for the claimed direction (a content-up displacement moves it down).
	moved int
	// perRow is the track pixels per list row derived from the observed thumb
	// geometry: (track − thumb) / (total rows − visible rows).
	perRow float64
}

// expected is the travel this geometry predicts for rows.
func (t thumbTravel) expected(rows int) float64 {
	return t.perRow * float64(absInt(rows))
}

// thumbTravelOf measures the scrollbar's travel and checks it against the
// claimed rows in direction and magnitude. The expected travel is the
// claimed rows times the per-row thumb travel derived from the observed
// thumb geometry itself: thumb length / track length ≈ visible rows / total
// rows, so per-row travel = (track − thumb) / (total − visible). A fixed
// per-row constant would be wrong across positions (a 24-row list moves the
// thumb ~28px per row, a much longer list a few pixels), and the derived
// estimate keeps the check self-contained per frame pair. A thumb that fills
// the track names a list with nothing below the viewport, where no
// displacement claim can be corroborated; fullness is judged by the thumb's
// own geometry, not by the derived step count — a six-row list's thumb leaves
// the step count at ~1.0 despite measurement jitter, and its genuine one-row
// scroll must still corroborate.
func (m *ScrollEvidenceMatcher) thumbTravelOf(before, after thumbReading, rows int) (thumbTravel, bool, string) {
	return m.thumbTravelCheck(before, after, rows, false)
}

// thumbTravelOfStrict is the drive-storage variant: both thumb edges must
// move in the claimed direction; a lone leading-edge move is not enough.
// The old warehouse keeps thumbTravelOf's original contract.
func (m *ScrollEvidenceMatcher) thumbTravelOfStrict(before, after thumbReading, rows int) (thumbTravel, bool, string) {
	return m.thumbTravelCheck(before, after, rows, true)
}

func (m *ScrollEvidenceMatcher) thumbTravelCheck(before, after thumbReading, rows int, strict bool) (thumbTravel, bool, string) {
	if !before.ok || !after.ok {
		return thumbTravel{}, false, "thumb unreadable"
	}
	trackLen := m.L.ScrollTrack.Dy()
	thumbLen := float64(before.end-before.start+1+after.end-after.start+1) / 2
	if thumbLen >= float64(trackLen)-thumbFullSlack {
		return thumbTravel{}, false, fmt.Sprintf("thumb %d–%d → %d–%d fills the track: no scrollable content below the viewport",
			before.start, before.end, after.start, after.end)
	}
	startTravel := after.start - before.start
	endTravel := after.end - before.end
	travel := startTravel
	if strict {
		translated, both := thumbTranslation(before, after)
		physical := translated
		if !both {
			// The trailing edge may be pinned at a track end while the leading
			// edge still moves for the final row. A synthetic one-edge move
			// (leading edge 300→306, trailing edge 323→323) is not a track-end
			// clamp and must not prove a row. Down: the bottom edge is pinned;
			// up: the top edge is pinned.
			pinnedDown := rows > 0 && endTravel == 0 && startTravel > 0 &&
				after.end >= m.C.ScrollbarBottomEndMin
			pinnedUp := rows < 0 && startTravel == 0 && endTravel < 0 &&
				after.start <= m.C.ScrollbarTopStartMax
			if !pinnedDown && !pinnedUp {
				return thumbTravel{}, false, fmt.Sprintf(
					"thumb %d–%d → %d–%d did not move both edges together (start %+d, end %+d)",
					before.start, before.end, after.start, after.end, startTravel, endTravel)
			}
			if pinnedUp {
				physical = endTravel
			} else {
				physical = startTravel
			}
		}
		// Positive rows mean the content moved up and the thumb moved down.
		// Keep the physical sign through the row test; taking the magnitude
		// first would let an upward thumb corroborate a forward row.
		if rows == 0 || physical*rows <= 0 {
			return thumbTravel{}, false, fmt.Sprintf(
				"thumb %d–%d → %d–%d moved %+dpx, opposite to the claimed %+d-row direction",
				before.start, before.end, after.start, after.end, physical, rows)
		}
		travel = absInt(physical)
	}
	if rows < 0 && !strict {
		travel = -travel
	}
	totalRows := float64(m.L.Rows) * float64(trackLen) / thumbLen
	steps := totalRows - float64(m.L.Rows)
	t := thumbTravel{moved: travel, perRow: (float64(trackLen) - thumbLen) / steps}
	if float64(travel) < float64(m.C.ScrollbarMoveMin) || !m.thumbTravelFits(t, rows) {
		if strict {
			return t, false, fmt.Sprintf("thumb %d–%d → %d–%d travelled %dpx, expected ~%.0fpx (≈%.1fpx per row × %d) within ±%.0f%%",
				before.start, before.end, after.start, after.end, travel, t.expected(rows), t.perRow, absInt(rows), m.C.ScrollbarTravelTolerance*100)
		}
		return t, false, fmt.Sprintf("thumb %d → %d travelled %dpx, expected ~%.0fpx (≈%.1fpx per row × %d) within ±%.0f%%",
			before.start, after.start, travel, t.expected(rows), t.perRow, absInt(rows), m.C.ScrollbarTravelTolerance*100)
	}
	return t, true, fmt.Sprintf("thumb %d → %d travelled %dpx ≈ %.1fpx per row × %d",
		before.start, after.start, travel, t.perRow, absInt(rows))
}

// thumbTravelFits reports whether the measured travel is within the
// calibrated relative tolerance of the travel rows predicts. The tolerance
// stays below 1/ScrollMaxRows, so a claim off by one whole row cannot pass on
// an exact travel measurement.
func (m *ScrollEvidenceMatcher) thumbTravelFits(t thumbTravel, rows int) bool {
	expected := t.expected(rows)
	if expected <= 0 {
		return false
	}
	return float64(t.moved) >= expected*(1-m.C.ScrollbarTravelTolerance) &&
		float64(t.moved) <= expected*(1+m.C.ScrollbarTravelTolerance)
}

// thumbAmbiguousRival reports a competing row count whose expected travel the
// same measured travel also fits. rivals are the same-direction non-zero whole
// row candidates the feature competition could not rule out; a thumb that also
// fits one of them cannot discriminate between the selection's row and that
// row, so the gap-limited verdict must stay undecided rather than be decided
// by a coincidence. The smallest rival row is reported for a deterministic
// message.
func (m *ScrollEvidenceMatcher) thumbAmbiguousRival(t thumbTravel, cands map[int]nccCandidate, chosen int, chosenNCC float64) (rivalRow int, rivalTravel float64, ambiguous bool) {
	rows := make([]int, 0, len(cands))
	for row, cand := range cands {
		if row == 0 || row == chosen || row*chosen < 0 {
			continue
		}
		if cand.ncc >= chosenNCC-m.C.ScrollNCCMargin {
			rows = append(rows, row)
		}
	}
	sort.Ints(rows)
	for _, row := range rows {
		if m.thumbTravelFits(t, row) {
			return row, t.expected(row), true
		}
	}
	return 0, 0, false
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
