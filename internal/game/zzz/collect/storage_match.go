package collect

import (
	"fmt"
	"image"

	"github.com/LoTwT/youkai/internal/platform"
)

// storageMatch is the public drive-storage page verdict. The page has no
// wheel; the list mixes all six slots and each occupied card must name its
// slot through the calibrated badge. A card whose badge matches no
// template (or more than one) stays unreadable in the verdict so the
// traversal can stop rather than guess; it does not make the whole page
// unknown, because the list/detail evidence is still needed to report the
// failure.
//
// A single between-tier selection border is still the original
// PageUnknown/List=nil failure here. storageMatchDetailed keeps the
// structured observation only for the one normal target detail wait that
// may wait through it; no public list or precheck path can select it.
func (m *PixelMatcher) storageMatch(f platform.Frame) PageMatch {
	return m.publicStorageMatch(f, m.storageMatchDetailed(f))
}

func (m *PixelMatcher) publicStorageMatch(f platform.Frame, p PageMatch) PageMatch {
	if p.List != nil && p.List.weakSelection != nil {
		weak := p.List.weakSelection
		return m.unknown("frame %d: drive-storage slot %d's selection border has %d hits, between the clear and selected tiers",
			f.ID, weak.Slot, weak.Hits)
	}
	return p
}

// storageHeader verifies the page anchors that do not move with the grid.
func (m *PixelMatcher) storageHeader(f platform.Frame) PageMatch {
	// This entry is shared by Match and the private weak-selection hint.
	// Validate the frame before any probe so an invalid image cannot panic
	// in a path that is only meant to wait for a fresh frame.
	if m == nil {
		return PageMatch{Page: PageUnknown, Evidence: "pixel matcher: the layout or calibration is incomplete"}
	}
	if !m.valid || !m.frameFits(f) {
		return m.unknown("frame %d is not the calibrated display configuration (image %v, content %v, want %dx%d and %v)",
			f.ID, boundsOrNil(f.Image), f.Content, m.L.Image.X, m.L.Image.Y, m.L.Content)
	}
	open, closed := 0, 0
	for _, p := range m.L.PanelOpen {
		switch m.classify(f.Image, p) {
		case "open":
			open++
		case "closed":
			closed++
		}
	}
	if open == len(m.L.PanelOpen) {
		panel := &PanelEvidence{Range: m.panelRangeEvidence(f.Image)}
		if m.sortLabelMatches(f.Image) {
			panel.SortLabel = sortLabelRarity
			panel.SortVerified = true
		}
		evidence := fmt.Sprintf("pixel matcher: frame %d is the drive-storage filter panel (both probes report open", f.ID)
		if panel.SortVerified {
			evidence += fmt.Sprintf(", sort %q verified", panel.SortLabel)
		} else {
			evidence += ", sort label unverified"
		}
		evidence += ")"
		return PageMatch{Page: PageFilterPanel, Panel: panel, Evidence: evidence}
	}
	if closed != len(m.L.PanelOpen) {
		return m.unknown("frame %d: the drive-storage filter-panel probes disagree or fall between tiers", f.ID)
	}

	if m.L.PageTitleStencil != nil && !m.L.PageTitleStencil.matchesExact(f.Image, m.L.PageTitle) {
		return m.unknown("frame %d: the drive-storage page title does not match the calibrated header", f.ID)
	}

	var filter FilterState
	switch m.classify(f.Image, m.L.Funnel) {
	case "clear":
		filter = FilterClear
	case "active":
		filter = FilterActive
	default:
		return m.unknown("frame %d: the drive-storage funnel is outside the clear and active tiers", f.ID)
	}

	return PageMatch{Page: PageDriveStorage, Filter: &FilterEvidence{State: filter}}
}

// weakSelectionEvidence is the private hint read by
// storageWeakSelectionPending. It reports one between-tier target border
// inside an otherwise confirmed storage frame without changing Match's
// public verdict.
func (m *PixelMatcher) weakSelectionEvidence(f platform.Frame) (PageMatch, *weakSelectionEvidence) {
	return weakStorageMatch(m.storageMatchDetailed(f))
}

func weakStorageMatch(p PageMatch) (PageMatch, *weakSelectionEvidence) {
	if p.List == nil || p.List.weakSelection == nil {
		return PageMatch{}, nil
	}
	return p, p.List.weakSelection
}

// storageMatchDetailed builds the structured storage observation used by
// the public verdict and by the private weak-selection wait. A weak
// between-tier border is recorded in ListEvidence.weakSelection with
// Selected=-1; storageMatch hides that structure from every public caller.
func (m *PixelMatcher) storageMatchDetailed(f platform.Frame) PageMatch {
	header := m.storageHeader(f)
	if header.Page != PageDriveStorage {
		return header
	}
	filter := header.Filter.State
	occupied := make([]bool, len(m.L.Slots))
	positions := make([]PositionVerdict, len(m.L.Slots))
	for i := range occupied {
		bar := m.maxChannel(m.meanColor(f.Image, m.L.BarRect(i)))
		switch {
		case bar <= m.C.BarEmptyMax:
			occupied[i] = false
		case bar >= m.C.BarOccupiedMin:
			occupied[i] = true
			positions[i] = m.positionVerdict(f.Image, m.L.CardPositionRect(i), m.L.CardPositions)
		default:
			return m.unknown("frame %d: drive-storage slot %d's occupancy bar mean %d is between %d and %d",
				f.ID, i, bar, m.C.BarEmptyMax, m.C.BarOccupiedMin)
		}
	}
	management := make([]ManagementState, len(m.L.Slots))
	avatars := make([]AvatarState, len(m.L.Slots))
	for i := range m.L.Slots {
		management[i] = m.managementState(f.Image, m.L.ManagementRect(i))
		avatars[i] = m.avatarState(f.Image, m.L.AvatarRect(i))
	}
	topManagement := m.topManagement(f.Image)
	selected := -1
	weak := -1
	weakHits := 0
	for i := range m.L.Slots {
		hits := countSelectionPixels(f.Image, m.L.BorderRect(i))
		switch {
		case hits >= m.C.SelectionHitMin:
			if selected >= 0 {
				return m.unknown("frame %d: drive-storage slots %d and %d both carry the selection border", f.ID, selected, i)
			}
			selected = i
		case hits >= m.C.SelectionClearMax:
			if weak >= 0 {
				return m.unknown("frame %d: drive-storage slots %d and %d both have between-tier selection borders", f.ID, weak, i)
			}
			weak, weakHits = i, hits
		}
	}
	if selected >= 0 && weak >= 0 {
		return m.unknown("frame %d: drive-storage slot %d has a selection border while slot %d is between tiers; the selection is ambiguous", f.ID, selected, weak)
	}
	top, bottom := m.scrollEnds(f.Image)
	detail := m.detailEvidence(f.Image)

	evidence := fmt.Sprintf("pixel matcher: frame %d is the drive-storage list (selected %d, filter %s, top %v, bottom %v, %d slots",
		f.ID, selected, filter, top, bottom, len(occupied))
	knownPositions := 0
	for i, p := range positions {
		if !occupied[i] {
			continue
		}
		if p.known() {
			knownPositions++
		}
	}
	evidence += fmt.Sprintf("; %d/%d occupied cards have a slot badge", knownPositions, countOccupied(occupied))
	if weak >= 0 {
		evidence += fmt.Sprintf("; slot %d has a between-tier selection border (%d hits), so selection is unconfirmed", weak, weakHits)
	}
	if detail != nil {
		evidence += fmt.Sprintf(", detail quality %q, substats %d", detail.Quality, detail.Substats)
		if detail.Position.known() {
			evidence += fmt.Sprintf(", detail slot %d", detail.Position.Position)
		}
	} else {
		evidence += ", detail not populated"
	}
	evidence += ")"
	list := &ListEvidence{
		Occupied:      occupied,
		Selected:      selected,
		Management:    management,
		Avatars:       avatars,
		Positions:     positions,
		TopManagement: topManagement,
		Top:           top,
		Bottom:        bottom,
	}
	if weak >= 0 {
		list.weakSelection = &weakSelectionEvidence{Slot: weak, Hits: weakHits}
	}
	return PageMatch{
		Page:     PageDriveStorage,
		List:     list,
		Detail:   detail,
		Filter:   &FilterEvidence{State: filter},
		Evidence: evidence,
	}
}

func countOccupied(occupied []bool) int {
	n := 0
	for _, present := range occupied {
		if present {
			n++
		}
	}
	return n
}

// positionVerdictDetail keeps the pre-C1 detail-pane rule. The same-offset
// runner-up recheck was calibrated only on card badges; details use a
// different glyph size and margin and remain unchanged until their own
// evidence exists.
func (m *PixelMatcher) positionVerdictDetail(img *image.RGBA, window image.Rectangle, templates [6]glyphSet) PositionVerdict {
	if img == nil || window.Empty() || !window.In(img.Bounds()) {
		return PositionVerdict{}
	}
	bestPosition, best, second := 0, -1.0, -1.0
	var bestSet glyphSet
	for i, set := range templates {
		if !set.valid() {
			continue
		}
		score, ok := set.bestScore(img, window)
		if !ok {
			continue
		}
		switch {
		case best < 0 || score > best:
			second = best
			best, bestPosition, bestSet = score, i+1, set
		case second < 0 || score > second:
			second = score
		}
	}
	if bestPosition == 0 || best < 0 || best < bestSet.minScore {
		return PositionVerdict{}
	}
	if second >= 0 && best-second < bestSet.minMargin {
		return PositionVerdict{Conflict: true}
	}
	return PositionVerdict{Position: bestPosition}
}

// positionVerdict classifies a card badge. A conflict is rechecked with the
// runner-up fixed at the winner's offset; this is the C1 card-only rule.
func (m *PixelMatcher) positionVerdict(img *image.RGBA, window image.Rectangle, templates [6]glyphSet) PositionVerdict {
	if img == nil || window.Empty() || !window.In(img.Bounds()) {
		return PositionVerdict{}
	}
	fast := m.fastCardBadges && window.Dx() <= 64 && window.Dy() <= 64
	pixels := cardBadgePixels{image: img, window: window}
	var scores [6]float64
	var available [6]bool
	bestPosition, secondPosition, best, second := 0, 0, -1.0, -1.0
	var bestSet glyphSet
	tied := false
	for i, set := range templates {
		if !set.valid() {
			continue
		}
		var score float64
		var ok bool
		if fast {
			score, ok = pixels.best(set, nil)
			scores[i], available[i] = score, ok
		} else {
			score, _, ok = set.bestMatch(img, window)
		}
		if !ok {
			continue
		}
		switch {
		case best < 0 || score > best:
			second, secondPosition, tied = best, bestPosition, false
			best, bestPosition, bestSet = score, i+1, set
		case score == best && i+1 != bestPosition:
			tied = true
			second, secondPosition = score, i+1
		case second < 0 || score > second:
			second, secondPosition = score, i+1
		}
	}
	// The saved partition-1 end viewport has two genuine 1 glyphs at
	// F1=.8772, both more than .23 ahead of every other digit. Keep this
	// narrow allowance inside an independently verified partition 1.
	partitionOneClear := m.partitionOneBadge && bestPosition == 1 && !tied &&
		best >= .875 && second >= 0 && best-second >= .20
	if bestPosition == 0 || best < 0 || (best < bestSet.minScore && !partitionOneClear) {
		return PositionVerdict{}
	}
	if tied {
		// Equal-quality offsets are genuinely ambiguous; do not let the
		// template iteration order choose one.
		return PositionVerdict{Conflict: true}
	}
	if second >= 0 && best-second < bestSet.minMargin {
		// The winner can itself have several equally good offsets. Every one
		// must hold the margin against every competing set before the cell is
		// admitted; otherwise the equal-quality placement is ambiguous.
		var offsets []image.Point
		var ok bool
		if fast {
			_, ok = pixels.best(bestSet, &offsets)
		} else {
			offsets, ok = bestSet.bestOffsets(img, window)
		}
		if !ok {
			return PositionVerdict{Conflict: true}
		}
		missing, third, allPass := false, false, true
		// A third class can be credible at an offset other than the winner's
		// equal placements. Check its global best score as well as every
		// winner-offset competitor before allowing LDA to confirm.
		for i, set := range templates {
			if i+1 == bestPosition || i+1 == secondPosition || !set.valid() {
				continue
			}
			score, ok := scores[i], available[i]
			if !fast {
				score, _, ok = set.bestMatch(img, window)
			}
			if ok && (score >= set.minScore || best-score < bestSet.minMargin) {
				third = true
			}
		}
		for _, at := range offsets {
			alt := -1.0
			scored := 0
			for i, set := range templates {
				if i+1 == bestPosition || !set.valid() {
					continue
				}
				var score float64
				var ok bool
				if fast {
					score, ok = pixels.bestAt(set, at)
				} else {
					score, ok = set.bestAt(img, at)
				}
				if !ok {
					missing = true
					continue
				}
				scored++
				if score > alt {
					alt = score
				}
				// A third winner class must not be quietly promoted by the
				// LDA confirmation: its score must be non-competitive at every
				// equal winner placement.
				if i+1 != secondPosition &&
					(score >= set.minScore || best-score < bestSet.minMargin) {
					third = true
				}
			}
			if scored < len(templates)-1 {
				missing = true
			}
			if alt < 0 || best-alt < bestSet.minMargin {
				allPass = false
			}
		}
		if allPass {
			return PositionVerdict{Position: bestPosition}
		}
		if !missing && !third && (bestPosition == 5 || bestPosition == 6) &&
			(secondPosition == 5 || secondPosition == 6) && secondPosition != bestPosition &&
			m.confirmCardGlyph(img, window, bestPosition) {
			return PositionVerdict{Position: bestPosition}
		}
		return PositionVerdict{Conflict: true}
	}
	return PositionVerdict{Position: bestPosition}
}
