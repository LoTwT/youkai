package collect

import (
	"image"
	"slices"
	"time"

	"github.com/LoTwT/youkai/internal/platform"
)

const bottomReboundWindow = 800 * time.Millisecond

type bottomReboundProof struct {
	matcher    *ScrollEvidenceMatcher
	before     platform.Frame
	list       *ListEvidence
	started    time.Time
	filter     FilterState
	invalid    bool
	rejected   uint64
	reason     string
	seen       bool
	freshStill int
	lastFresh  uint64
}

func (b *bottomReboundProof) reject(f platform.Frame, reason string) {
	if !b.invalid {
		b.invalid, b.rejected, b.reason = true, f.ID, reason
	}
}

// observe follows only the independent downward wheel tick at the bottom.
// Any unexplained frame permanently disqualifies this proof. In particular,
// settling back at the original offset cannot erase a detected row advance.
func (b *bottomReboundProof) observe(f platform.Frame, page PageMatch, sm ScrollMatch) {
	if b.matcher == nil || b.invalid {
		return
	}
	list := page.List
	filter := page.Filter
	selected, bottom := -1, false
	switch {
	case page.Page == PageDriveStorage && list != nil:
		selected, bottom = list.Selected, list.Bottom
	case page.Page == PageUnknown && list == nil:
		// A shifted rarity bar can make the fixed grid probe undecidable.
		// Recheck the stationary page anchors on this exact frame. Even
		// with those anchors, it cannot count as a fresh still frame.
		filter = nil
		header := b.matcher.storageHeader(f)
		if header.Page == PageDriveStorage {
			filter = header.Filter
			selected = b.matcher.selectionAnchor(f.Image)
			_, bottom = b.matcher.scrollEnds(f.Image)
			sm = b.matcher.MatchScroll(b.before, f)
		}
	default:
		b.reject(f, "unverified-page")
	}
	switch {
	case filter == nil:
		b.reject(f, "unverified-page")
	case !bottom:
		b.reject(f, "bottom-indicator-missing")
	case filter.State != b.filter:
		b.reject(f, "filter-changed")
	case selected != b.list.Selected:
		b.reject(f, "selection-changed")
	case sm.Displaced || sm.Lost || sm.SelectionConflict:
		b.reject(f, "scroll-counterevidence")
	}
	if b.invalid {
		return
	}
	start, end, ok := b.matcher.thumbSegment(b.before.Image)
	nextStart, nextEnd, nextOK := b.matcher.thumbSegment(f.Image)
	if !ok || !nextOK || start != nextStart || end != nextEnd {
		b.reject(f, "thumb-missing-or-changed")
		return
	}
	if still, _ := b.matcher.zeroDisplacementProven(b.before, f); list != nil && still &&
		slices.Equal(b.list.Occupied, list.Occupied) && slices.Equal(b.list.Positions, list.Positions) {
		if !f.Reused && f.ID != b.lastFresh && f.ID != b.before.ID {
			b.lastFresh = f.ID
			b.freshStill++
		}
		return
	}
	if f.ArrivedAt.IsZero() || f.ArrivedAt.Before(b.started) ||
		f.ArrivedAt.Sub(b.started) > bottomReboundWindow {
		b.reject(f, "rebound-outside-observation-window")
		return
	}
	if !b.matcher.bottomReboundAligned(b.before, f, b.list) {
		b.reject(f, "pixel-alignment-or-aligned-grid-unproven")
		return
	}
	b.seen = true
	b.freshStill = 0
}

func (b *bottomReboundProof) verified() bool {
	return b.seen && !b.invalid && b.freshStill >= 2
}

// bottomReboundAligned proves that independent upper and lower list areas
// have a small upward translation, to within one pixel of each other. Compare nearby pixel translations
// and whole-row rivals, then re-read every bar and badge at that alignment.
func (m *ScrollEvidenceMatcher) bottomReboundAligned(before, after platform.Frame, list *ListEvidence) bool {
	if !m.frameFits(before) || !m.frameFits(after) ||
		len(list.Occupied) != len(m.L.Slots) || len(list.Positions) != len(m.L.Slots) {
		return false
	}
	// Keep the translated list well within the same row. A quarter of a
	// calibrated row leaves room for the elastic animation and separate
	// search support between this bound and the neighbouring row.
	maxOffset := m.L.RowPitch / 4
	border := m.L.BorderRect(list.Selected)
	border.Min.Y -= maxOffset
	border.Max.Y += maxOffset
	mask := []image.Rectangle{border}
	viewport := m.L.ListViewport
	mid := (viewport.Min.Y + viewport.Max.Y) / 2
	upper, lower := viewport, viewport
	upper.Max.Y, lower.Min.Y = mid, mid

	upperBand, upperOK := m.bottomReboundOffsetBand(before, after, upper, mask)
	lowerBand, lowerOK := m.bottomReboundOffsetBand(before, after, lower, mask)
	if !upperOK || !lowerOK {
		return false
	}
	first := min(upperBand.first, lowerBand.first)
	last := max(upperBand.last, lowerBand.last)
	if first <= 0 || last > maxOffset || last-first > 1 {
		return false
	}
	floor := min(upperBand.floor, lowerBand.floor)
	for rows, candidate := range m.rowCorrelations(before.Image, after.Image, mask) {
		if rows != 0 && candidate.ncc > floor-m.C.ScrollNCCMargin {
			return false
		}
	}

	// Fractional translations can support two neighbouring offsets.
	// Verify each against the complete grid without selecting one by score.
	for dy := first; dy <= last; dy++ {
		if !m.bottomGridAt(after.Image, list, dy) {
			return false
		}
	}
	return true
}

func (m *ScrollEvidenceMatcher) bottomGridAt(img *image.RGBA, list *ListEvidence, dy int) bool {
	shift := image.Pt(0, -dy)
	for i, occupied := range list.Occupied {
		bar := m.maxChannel(m.meanColor(img, m.L.BarRect(i).Add(shift)))
		if !occupied {
			if bar > m.C.BarEmptyMax {
				return false
			}
			continue
		}
		if bar < m.C.BarOccupiedMin || !list.Positions[i].known() {
			return false
		}
		position := m.positionVerdict(img, m.L.CardPositionRect(i).Add(shift), m.L.CardPositions)
		if !position.known() || position.Position != list.Positions[i].Position {
			return false
		}
	}
	return true
}

type bottomOffsetBand struct {
	first, last int
	peakOffset  int
	peak, floor float64
}

func (m *ScrollEvidenceMatcher) bottomReboundOffset(before, after platform.Frame, region image.Rectangle, mask []image.Rectangle) (int, float64, bool) {
	band, ok := m.bottomReboundOffsetBand(before, after, region, mask)
	return band.peakOffset, band.peak, ok && band.first == band.last
}

func (m *ScrollEvidenceMatcher) bottomReboundOffsetBand(before, after platform.Frame, region image.Rectangle, mask []image.Rectangle) (bottomOffsetBand, bool) {
	// Search beyond the allowed excursion, so a larger shift cannot
	// masquerade as an in-range match at the edge of the search.
	search := m.L.RowPitch / 2
	scores := make([]float64, 2*search+1)
	band := bottomOffsetBand{peak: -1}
	for i := range scores {
		dy := i - search
		score, std, ok := m.correlationIn(before.Image, after.Image, region, dy, mask)
		if !ok || std < m.C.ScrollInfoStdMin {
			scores[i] = -1
			continue
		}
		scores[i] = score
		if score > band.peak {
			band.peakOffset, band.peak = dy, score
		}
	}
	if band.peak < m.C.ScrollStationaryNCC {
		return band, false
	}

	band.first, band.last = band.peakOffset, band.peakOffset
	band.floor = band.peak
	for i, score := range scores {
		if band.peak-score > nccTieEpsilon {
			continue
		}
		if score < m.C.ScrollStationaryNCC {
			return band, false
		}
		dy := i - search
		band.first = min(band.first, dy)
		band.last = max(band.last, dy)
		band.floor = min(band.floor, score)
	}
	return band, band.last-band.first <= 1
}
