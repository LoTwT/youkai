package collect

import (
	"fmt"
	"image"

	"github.com/LoTwT/youkai/internal/platform"
)

// storageMatch is the drive-storage page's page verdict. The page has no
// wheel; the list mixes all six slots and each occupied card must name its
// slot through the calibrated badge. A card whose badge matches no
// template (or more than one) stays unreadable in the verdict so the
// traversal can stop rather than guess; it does not make the whole page
// unknown, because the list/detail evidence is still needed to report the
// failure.
func (m *PixelMatcher) storageMatch(f platform.Frame) PageMatch {
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
	for i := range m.L.Slots {
		hits := countSelectionPixels(f.Image, m.L.BorderRect(i))
		switch {
		case hits >= m.C.SelectionHitMin:
			if selected >= 0 {
				return m.unknown("frame %d: drive-storage slots %d and %d both carry the selection border", f.ID, selected, i)
			}
			selected = i
		case hits >= m.C.SelectionClearMax:
			return m.unknown("frame %d: drive-storage slot %d's selection border has %d hits, between the clear and selected tiers",
				f.ID, i, hits)
		}
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
	if detail != nil {
		evidence += fmt.Sprintf(", detail quality %q, substats %d", detail.Quality, detail.Substats)
		if detail.Position.known() {
			evidence += fmt.Sprintf(", detail slot %d", detail.Position.Position)
		}
	} else {
		evidence += ", detail not populated"
	}
	evidence += ")"
	return PageMatch{
		Page: PageDriveStorage,
		List: &ListEvidence{
			Occupied:      occupied,
			Selected:      selected,
			Management:    management,
			Avatars:       avatars,
			Positions:     positions,
			TopManagement: topManagement,
			Top:           top,
			Bottom:        bottom,
		},
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

// positionVerdict classifies one slot badge against the six calibrated
// glyph sets. The window may be a few pixels larger than the mask so a
// one-row scroll's sub-pixel shift does not turn a readable badge into an
// unknown. Exactly one set must clear its foreground-F1 floor and lead the
// runner-up by its margin; two credible sets closer than that are a
// conflict, and neither is guessed.
func (m *PixelMatcher) positionVerdict(img *image.RGBA, window image.Rectangle, templates [6]glyphSet) PositionVerdict {
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
