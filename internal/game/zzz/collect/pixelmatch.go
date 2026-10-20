package collect

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"sort"

	"github.com/LoTwT/youkai/internal/platform"
)

// PixelMatcher is the calibrated evidence engine for one display
// configuration. It reads only the regions declared by L and reports
// "cannot determine" whenever a probe falls between calibrated tiers; it
// never guesses a page, a slot attribution, a rarity or a line count.
type PixelMatcher struct {
	L Layout
	C Calibration

	// valid is false when the layout or the threshold set is structurally
	// unusable; every verdict is then "cannot determine".
	valid bool
}

// NewPixelMatcher returns a matcher over the given anchors and thresholds.
// A structurally invalid layout or an unordered threshold set makes the
// matcher report undecidable verdicts instead of panicking or guessing.
func NewPixelMatcher(l Layout, c Calibration) *PixelMatcher {
	return &PixelMatcher{L: l, C: c, valid: l.Validate() == nil && l.FitsContent(l.Content) && c.validate() == nil}
}

// frameFits reports whether the frame is the calibrated display
// configuration: the image must be exactly the calibrated size and the
// content region must be exactly equal, not merely contained.
func (m *PixelMatcher) frameFits(f platform.Frame) bool {
	return f.Image != nil && f.Image.Bounds() == image.Rectangle{Max: m.L.Image} && f.Content == m.L.Content
}

func (m *PixelMatcher) unknown(format string, args ...any) PageMatch {
	return PageMatch{Page: PageUnknown, Evidence: "pixel matcher: " + fmt.Sprintf(format, args...)}
}

// Match implements Matcher: one frame's page, list, detail, wheel and
// funnel evidence (design §3's decision table).
func (m *PixelMatcher) Match(f platform.Frame) PageMatch {
	if m == nil || !m.valid {
		return m.unknown("the layout or calibration is incomplete")
	}
	if !m.frameFits(f) {
		return m.unknown("frame %d is not the calibrated display configuration (image %v, content %v, want %dx%d and %v)",
			f.ID, boundsOrNil(f.Image), f.Content, m.L.Image.X, m.L.Image.Y, m.L.Content)
	}

	// The filter panel overlays the warehouse; its two probes must agree
	// on the tier before either page can be judged.
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
		panel := &PanelEvidence{}
		if m.sortLabelMatches(f.Image) {
			panel.SortLabel = sortLabelRarity
			panel.SortVerified = true
		}
		evidence := fmt.Sprintf("pixel matcher: frame %d is the filter panel (both probes report open", f.ID)
		if panel.SortVerified {
			evidence += fmt.Sprintf(", sort %q verified", panel.SortLabel)
		} else {
			evidence += ", sort label unverified"
		}
		evidence += ")"
		return PageMatch{Page: PageFilterPanel, Panel: panel, Evidence: evidence}
	}
	if closed != len(m.L.PanelOpen) {
		return m.unknown("frame %d: the filter-panel probes disagree or fall between tiers", f.ID)
	}

	// Warehouse page: the funnel must be in one of its two tiers, exactly
	// one wheel position must carry the selection ring, and every grid slot
	// must have a decidable occupancy bar (design §3).
	var filter FilterState
	switch m.classify(f.Image, m.L.Funnel) {
	case "clear":
		filter = FilterClear
	case "active":
		filter = FilterActive
	default:
		return m.unknown("frame %d: the funnel icon is outside the clear and active tiers", f.ID)
	}
	position := m.wheelPosition(f.Image)
	if position == 0 {
		return m.unknown("frame %d: no unique wheel position carries the selection ring", f.ID)
	}
	occupied := make([]bool, len(m.L.Slots))
	for i := range occupied {
		bar := m.maxChannel(m.meanColor(f.Image, m.L.BarRect(i)))
		switch {
		case bar <= m.C.BarEmptyMax:
			occupied[i] = false
		case bar >= m.C.BarOccupiedMin:
			occupied[i] = true
		default:
			return m.unknown("frame %d: slot %d's occupancy bar mean %d is between %d and %d",
				f.ID, i, bar, m.C.BarEmptyMax, m.C.BarOccupiedMin)
		}
	}
	// Management badges and equipped-agent avatars are per-cell evidence.
	// An unreadable badge or avatar stays unknown; it never becomes none or
	// absent.
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
				return m.unknown("frame %d: slots %d and %d both carry the selection border", f.ID, selected, i)
			}
			selected = i
		case hits >= m.C.SelectionClearMax:
			return m.unknown("frame %d: slot %d's selection border has %d hits, between the clear and selected tiers",
				f.ID, i, hits)
		}
	}
	top, bottom := m.scrollEnds(f.Image)
	detail := m.detailEvidence(f.Image)

	evidence := fmt.Sprintf("pixel matcher: frame %d is the warehouse list (selected %d, wheel %d, filter %s, top %v, bottom %v",
		f.ID, selected, position, filter, top, bottom)
	if detail != nil {
		evidence += fmt.Sprintf(", quality %q, substats %d", detail.Quality, detail.Substats)
	} else {
		evidence += ", detail not populated"
	}
	evidence += ")"
	return PageMatch{
		Page: PageWarehouse,
		List: &ListEvidence{
			Occupied:      occupied,
			Selected:      selected,
			Management:    management,
			Avatars:       avatars,
			TopManagement: topManagement,
			Top:           top,
			Bottom:        bottom,
		},
		Detail:   detail,
		Wheel:    &WheelEvidence{Position: position},
		Filter:   &FilterEvidence{State: filter},
		Evidence: evidence,
	}
}

// detailEvidence reads the detail pane's rarity and line count. It returns
// nil when the name line carries no text (no populated pane); a populated
// pane whose badge or fourth row cannot be assigned a calibrated tier
// reports QualityUnknown or Substats 0, which the state machine turns into
// detailUnresolved.
func (m *PixelMatcher) detailEvidence(img *image.RGBA) *DetailEvidence {
	name := m.detailLine(LineName)
	if name.Empty() || m.inkCount(img, name, m.C.InkThreshold) < m.C.DetailPopulatedInk {
		return nil
	}
	ev := &DetailEvidence{Quality: QualityUnknown}
	if m.goldCount(img, m.L.QualityBadge) >= m.C.QualityGoldMin {
		ev.Quality = QualityS
	}
	ev.Avatar = m.avatarState(img, m.L.DetailAvatar)
	switch ink := m.inkCount(img, m.L.Substat4Probe, m.C.InkThreshold); {
	case ink >= m.C.Substat4InkMin:
		ev.Substats = 4
	case ink <= m.C.Substat3InkMax:
		ev.Substats = 3
	}
	return ev
}

// sortLabelMatches compares the panel's sort region against the
// layout's reference template. It requires the exact calibrated size and
// position; it never searches a neighbourhood, because a moved label is a
// different display configuration, not the same fact.
func (m *PixelMatcher) sortLabelMatches(img *image.RGBA) bool {
	t := m.L.SortTemplate
	r := m.L.SortLabel
	if t == nil || r.Empty() || img == nil {
		return false
	}
	if !r.In(img.Bounds()) || t.Bounds().Size() != r.Size() {
		return false
	}
	limit := m.C.SortLabelMADMax
	if limit <= 0 {
		limit = 2.0
	}
	var sum float64
	for y := 0; y < r.Dy(); y++ {
		for x := 0; x < r.Dx(); x++ {
			d := grayAt(img, r.Min.X+x, r.Min.Y+y) - grayAt(t, t.Bounds().Min.X+x, t.Bounds().Min.Y+y)
			if d < 0 {
				d = -d
			}
			sum += float64(d)
		}
	}
	return sum/float64(r.Dx()*r.Dy()) <= limit
}

// detailLine returns the calibrated region for a role, or the zero
// rectangle when the layout does not declare it.
func (m *PixelMatcher) detailLine(role string) image.Rectangle {
	for _, line := range m.L.DetailLines {
		if line.Role == role {
			return line.Rect
		}
	}
	return image.Rectangle{}
}

// managementState classifies one cell's bottom-right management badge.
// A red trash glyph is discarded; a bright neutral annulus around a
// mostly hollow center is the lock; neither is none. Counts that fall
// between the calibrated tiers, or a region that shows both a lock ring
// and red trash pixels, are unknown. The lock's flash means one frame can
// lose the annulus; callers combine several frames rather than treating a
// single dim frame as none.
func (m *PixelMatcher) managementState(img *image.RGBA, r image.Rectangle) ManagementState {
	c := rectCenter(r)
	red, ring, core := 0, 0, 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			i := img.PixOffset(x, y)
			redValue, green, blue := int(img.Pix[i]), int(img.Pix[i+1]), int(img.Pix[i+2])
			if redValue > 90 && redValue > green+40 && redValue > blue+40 && green < 80 {
				red++
			}
			dx, dy := x-c.X, y-c.Y
			d2 := dx*dx + dy*dy
			if neutral(redValue, green, blue, 110, 50) {
				if d2 >= 49 && d2 <= 169 {
					ring++
				}
				if d2 <= 36 {
					core++
				}
			}
		}
	}
	switch {
	case red >= m.C.ManagementDiscardRedMin && ring < m.C.ManagementLockRingMin:
		return ManagementDiscarded
	case red <= m.C.ManagementClearRedMax && ring >= m.C.ManagementLockRingMin &&
		core <= m.C.ManagementLockCoreMax:
		return ManagementLocked
	case red <= m.C.ManagementClearRedMax && ring <= m.C.ManagementClearRingMax:
		return ManagementNone
	default:
		return ManagementUnknown
	}
}

// avatarState classifies an equipped-agent avatar probe. Present requires
// a bright neutral annulus covering most directions around a portrait-like
// core (bright enough and with enough internal range); absent requires
// both a missing ring and an empty core. Everything else is unknown: a
// selection corner glow or a partially covered avatar must not become
// either answer.
func (m *PixelMatcher) avatarState(img *image.RGBA, r image.Rectangle) AvatarState {
	c := rectCenter(r)
	var sectors [8]int
	ring := 0
	coreN, coreSum, coreMin, coreMax := 0, 0, 255, 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			i := img.PixOffset(x, y)
			red, green, blue := int(img.Pix[i]), int(img.Pix[i+1]), int(img.Pix[i+2])
			dx, dy := x-c.X, y-c.Y
			d2 := dx*dx + dy*dy
			if d2 >= 81 && d2 <= 225 && neutral(red, green, blue, 100, 65) {
				ring++
				angle := math.Atan2(float64(dy), float64(dx)) + math.Pi
				sector := int(angle / (math.Pi / 4))
				if sector > 7 {
					sector = 7
				}
				sectors[sector]++
			}
			if d2 <= 64 {
				value := (red + green + blue) / 3
				coreN++
				coreSum += value
				if value < coreMin {
					coreMin = value
				}
				if value > coreMax {
					coreMax = value
				}
			}
		}
	}
	if coreN == 0 {
		return AvatarUnknown
	}
	mean := coreSum / coreN
	covered := 0
	for _, n := range sectors {
		if n >= 4 {
			covered++
		}
	}
	switch {
	case ring >= m.C.AvatarRingMin && covered >= m.C.AvatarRingSectorsMin &&
		mean >= m.C.AvatarCoreMeanMin && coreMax-coreMin >= m.C.AvatarCoreRangeMin:
		return AvatarPresent
	case ring <= m.C.AvatarAbsentRingMax && mean <= m.C.AvatarAbsentCoreMeanMax:
		return AvatarAbsent
	default:
		return AvatarUnknown
	}
}

// topManagement classifies the detail page's lock indicator: a filled
// dim circle means locked, an open circle with a bright glyph means
// unlocked. The indicator only confirms the locked dimension; the list
// badge remains the source of discarded versus none.
func (m *PixelMatcher) topManagement(img *image.RGBA) TopManagementState {
	r := m.L.TopManagement
	c := rectCenter(r)
	radius := r.Dx()
	if r.Dy() < radius {
		radius = r.Dy()
	}
	radius = radius/2 - 2
	if radius <= 0 {
		return TopManagementUnknown
	}
	dim, bright := 0, 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			dx, dy := x-c.X, y-c.Y
			if dx*dx+dy*dy > radius*radius {
				continue
			}
			i := img.PixOffset(x, y)
			red, green, blue := int(img.Pix[i]), int(img.Pix[i+1]), int(img.Pix[i+2])
			if neutral(red, green, blue, 150, 50) {
				bright++
			}
			if neutral(red, green, blue, 80, 60) {
				dim++
			}
		}
	}
	switch {
	case dim >= m.C.TopLockedDimMin:
		return TopManagementLocked
	case bright >= m.C.TopUnlockedBrightMin && dim <= m.C.TopUnlockedDimMax:
		return TopManagementUnlocked
	default:
		return TopManagementUnknown
	}
}

// neutral reports whether a pixel reaches a brightness floor with little
// channel spread, the property shared by the lock annulus, avatar ring and
// top indicator's neutral UI.
func neutral(r, g, b, minBrightness, maxSpread int) bool {
	max := r
	if g > max {
		max = g
	}
	if b > max {
		max = b
	}
	min := r
	if g < min {
		min = g
	}
	if b < min {
		min = b
	}
	return max >= minBrightness && max-min <= maxSpread
}

// wheelPosition counts selection-colored pixels in the four arc probes of
// each wheel position. Exactly one position at the selected tier and all
// others at the clear tier yields 1..6; every other combination is 0.
func (m *PixelMatcher) wheelPosition(img *image.RGBA) int {
	selected := 0
	for i, center := range m.L.Wheel {
		hits := 0
		for _, arc := range m.L.WheelArcs {
			hits += countSelectionPixels(img, arc.Add(center))
		}
		switch {
		case hits >= m.C.WheelSelectedMin:
			if selected != 0 {
				return 0
			}
			selected = i + 1
		case hits > m.C.WheelOtherMax:
			return 0
		}
	}
	return selected
}

// scrollEnds classifies the scrollbar thumb's resting position. Top and
// Bottom are single-frame supporting evidence, not end evidence on their
// own (T5 §5); a missing thumb reports neither.
func (m *PixelMatcher) scrollEnds(img *image.RGBA) (top, bottom bool) {
	start, end, ok := m.thumbSegment(img)
	if !ok {
		return false, false
	}
	return start <= m.C.ScrollbarTopStartMax, end >= m.C.ScrollbarBottomEndMin
}

// ThumbStart reports the scrollbar thumb's first row in a frame. The
// traversal uses it to prove that a fling toward the top actually moved
// the thumb upward; a missing or unusable frame reports false.
func (m *PixelMatcher) ThumbStart(f platform.Frame) (int, bool) {
	if m == nil || !m.valid || !m.frameFits(f) {
		return 0, false
	}
	start, _, ok := m.thumbSegment(f.Image)
	return start, ok
}

// thumbSegment finds the longest bright run in the scrollbar's thumb
// column. The track rect excludes the two end caps.
func (m *PixelMatcher) thumbSegment(img *image.RGBA) (start, end int, ok bool) {
	track := m.L.ScrollTrack
	x := track.Min.X
	bestStart, bestEnd := 0, -1
	runStart := -1
	commit := func(stop int) {
		if runStart < 0 {
			return
		}
		if stop-runStart > bestEnd-bestStart {
			bestStart, bestEnd = runStart, stop
		}
		runStart = -1
	}
	for y := track.Min.Y; y < track.Max.Y; y++ {
		if grayAt(img, x, y) > m.C.ScrollbarBrightMin {
			if runStart < 0 {
				runStart = y
			}
		} else {
			commit(y - 1)
		}
	}
	commit(track.Max.Y - 1)
	if bestEnd < bestStart {
		return 0, 0, false
	}
	return bestStart, bestEnd, true
}

// MatchScroll implements Matcher: grayscale overlap matching over the list
// viewport. err0 below the stationary threshold is proof the list did not
// move; a best alignment at a whole row pitch (within tolerance) is a
// verified displacement with the correspondences the traversal carries
// across; a large zero-displacement error with no aligned best shift is a
// lost position (a fling overshot the overlap), not a guess.
func (m *PixelMatcher) MatchScroll(before, after platform.Frame) ScrollMatch {
	if m == nil || !m.valid {
		return ScrollMatch{Evidence: "pixel matcher: the layout or calibration is incomplete"}
	}
	if !m.frameFits(before) || !m.frameFits(after) {
		return ScrollMatch{Evidence: fmt.Sprintf("pixel matcher: frames %d → %d are not the calibrated display configuration",
			before.ID, after.ID)}
	}
	err0, ok := m.zeroShiftError(before.Image, after.Image)
	if !ok {
		return ScrollMatch{Evidence: fmt.Sprintf("pixel matcher: frames %d → %d have too few comparable list samples",
			before.ID, after.ID)}
	}
	if err0 < m.C.ScrollStationaryMax {
		return ScrollMatch{Stationary: true, Evidence: fmt.Sprintf(
			"frames %d → %d: list viewport error at zero displacement %.2f < %.1f — the list did not move",
			before.ID, after.ID, err0, m.C.ScrollStationaryMax)}
	}
	dy, bestErr, ok := m.bestShift(before.Image, after.Image)
	if !ok {
		return ScrollMatch{Evidence: fmt.Sprintf("pixel matcher: frames %d → %d have too few comparable list samples",
			before.ID, after.ID)}
	}
	direction, rows := 0, 0
	switch {
	case dy > 0:
		direction = 1
		rows = int(math.Round(float64(dy) / float64(m.L.RowPitch)))
	case dy < 0:
		direction = -1
		rows = int(math.Round(float64(-dy) / float64(m.L.RowPitch)))
	}
	aligned := rows >= 1 && rows <= m.C.ScrollMaxRows &&
		absInt(absInt(dy)-rows*m.L.RowPitch) <= m.C.ScrollRowTolerance
	if aligned && bestErr <= m.C.ScrollDisplacedMax && bestErr <= err0-m.C.ScrollDisplacedMargin {
		matches := make([]PositionPair, 0, (m.L.Rows-rows)*m.L.Columns)
		for r := 0; r < m.L.Rows-rows; r++ {
			for c := 0; c < m.L.Columns; c++ {
				if direction > 0 {
					// The item at before-row r+rows sits at after-row r.
					matches = append(matches, PositionPair{FromRow: r + rows, FromColumn: c, ToRow: r, ToColumn: c})
				} else {
					matches = append(matches, PositionPair{FromRow: r, FromColumn: c, ToRow: r + rows, ToColumn: c})
				}
			}
		}
		return ScrollMatch{Displaced: true, Rows: rows, Matches: matches, Evidence: fmt.Sprintf(
			"frames %d → %d: list displaced %d row(s) (dy %d, error %.2f ≤ %.1f and ≥ %.1f below the zero-displacement error %.2f)",
			before.ID, after.ID, rows, dy, bestErr, m.C.ScrollDisplacedMax, m.C.ScrollDisplacedMargin, err0)}
	}
	if err0 >= m.C.ScrollLostMin && !aligned {
		return ScrollMatch{Lost: true, Evidence: fmt.Sprintf(
			"frames %d → %d: the list changed (zero-displacement error %.2f ≥ %.1f) but the best shift %d is not a whole row (error %.2f) — the overlap was lost",
			before.ID, after.ID, err0, m.C.ScrollLostMin, dy, bestErr)}
	}
	return ScrollMatch{Evidence: fmt.Sprintf(
		"frames %d → %d: undecided (zero-displacement error %.2f, best shift %d at error %.2f)",
		before.ID, after.ID, err0, dy, bestErr)}
}

// zeroShiftError is the mean absolute grayscale difference of the list
// viewport at zero displacement.
func (m *PixelMatcher) zeroShiftError(before, after *image.RGBA) (float64, bool) {
	r := m.L.ListViewport
	var sum float64
	var n int
	for y := r.Min.Y; y < r.Max.Y; y += m.C.ScrollStepY {
		for x := r.Min.X; x < r.Max.X; x += m.C.ScrollStepX {
			d := grayAt(before, x, y) - grayAt(after, x, y)
			if d < 0 {
				d = -d
			}
			sum += float64(d)
			n++
		}
	}
	if n < m.C.ScrollMinSamples {
		return 0, false
	}
	return sum / float64(n), true
}

// bestShift finds the vertical displacement with the smallest error over
// the overlap, sampling the list viewport at the calibrated steps.
func (m *PixelMatcher) bestShift(before, after *image.RGBA) (int, float64, bool) {
	r := m.L.ListViewport
	bestDy, bestErr := 0, math.Inf(1)
	// A shift that leaves less than one row of overlap cannot establish a
	// correspondence: the surviving sliver may be identical background in
	// both frames (the field samples show 27-pixel edge slivers matching at
	// error 0), so accepting it would invent a displacement. T5 §5 requires
	// at least one usable row of overlap.
	minOverlap := m.L.RowPitch - m.C.ScrollRowTolerance
	for dy := -m.C.ScrollSearchRadius; dy <= m.C.ScrollSearchRadius; dy++ {
		if r.Dy()-absInt(dy) < minOverlap {
			continue
		}
		var sum float64
		var n int
		for y := r.Min.Y; y < r.Max.Y; y += m.C.ScrollStepY {
			beforeY := y + dy
			if beforeY < r.Min.Y || beforeY >= r.Max.Y {
				continue
			}
			for x := r.Min.X; x < r.Max.X; x += m.C.ScrollStepX {
				d := grayAt(before, x, beforeY) - grayAt(after, x, y)
				if d < 0 {
					d = -d
				}
				sum += float64(d)
				n++
			}
		}
		if n < m.C.ScrollMinSamples {
			continue
		}
		if err := sum / float64(n); err < bestErr {
			bestDy, bestErr = dy, err
		}
	}
	if math.IsInf(bestErr, 1) {
		return 0, 0, false
	}
	return bestDy, bestErr, true
}

// CompareDetail implements Matcher: the mean of the seven per-line
// grayscale MADs, with the calibrated same/different bands. The caller
// must not read an Undecided verdict as either answer.
func (m *PixelMatcher) CompareDetail(a, b platform.Frame) DetailComparison {
	mean, ok := m.DetailMAD(a, b)
	if !ok {
		return DetailUndecided
	}
	switch {
	case mean < m.C.DetailSameMax:
		return DetailSame
	case mean > m.C.DetailDifferentMin:
		return DetailDifferent
	default:
		return DetailUndecided
	}
}

// DetailMAD returns the mean of the seven per-line grayscale MADs and
// whether the comparison is possible under this matcher's calibration.
// It is the numeric evidence behind CompareDetail; the field replay uses
// it to report the measured same-content and adjacent-pair distributions.
func (m *PixelMatcher) DetailMAD(a, b platform.Frame) (float64, bool) {
	if m == nil || !m.valid || !m.frameFits(a) || !m.frameFits(b) || len(m.L.DetailLines) == 0 {
		return 0, false
	}
	var sum float64
	for _, line := range m.L.DetailLines {
		sum += lineMAD(a.Image, b.Image, line.Rect)
	}
	return sum / float64(len(m.L.DetailLines)), true
}

// classify returns the color tier whose mean is within the probe's
// tolerance. A mean within two tiers (only possible with overlapping
// tolerances) or outside every tier is unclassified.
func (m *PixelMatcher) classify(img *image.RGBA, probe ColorProbe) string {
	mean := m.meanColor(img, probe.At)
	names := make([]string, 0, len(probe.Colors))
	for name := range probe.Colors {
		names = append(names, name)
	}
	sort.Strings(names)
	matched := ""
	for _, name := range names {
		tier := probe.Colors[name]
		if maxChannelDiff(mean, tier) <= probe.Tol {
			if matched != "" {
				return ""
			}
			matched = name
		}
	}
	return matched
}

// meanColor returns the rounded mean RGB of a region.
func (m *PixelMatcher) meanColor(img *image.RGBA, r image.Rectangle) color.RGBA {
	var sr, sg, sb, n int
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			i := img.PixOffset(x, y)
			sr += int(img.Pix[i])
			sg += int(img.Pix[i+1])
			sb += int(img.Pix[i+2])
			n++
		}
	}
	if n == 0 {
		return color.RGBA{}
	}
	return color.RGBA{R: uint8(sr / n), G: uint8(sg / n), B: uint8(sb / n), A: 255}
}

func (m *PixelMatcher) maxChannel(c color.RGBA) int {
	max := int(c.R)
	if int(c.G) > max {
		max = int(c.G)
	}
	if int(c.B) > max {
		max = int(c.B)
	}
	return max
}

// inkCount counts pixels whose grayscale value exceeds threshold.
func (m *PixelMatcher) inkCount(img *image.RGBA, r image.Rectangle, threshold int) int {
	n := 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if grayAt(img, x, y) > threshold {
				n++
			}
		}
	}
	return n
}

// goldCount counts badge pixels in the calibrated gold tier; every S
// sample counted 251–256, dimmed panel frames 0.
func (m *PixelMatcher) goldCount(img *image.RGBA, r image.Rectangle) int {
	n := 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			i := img.PixOffset(x, y)
			red, green, blue := int(img.Pix[i]), int(img.Pix[i+1]), int(img.Pix[i+2])
			if red > 150 && green > 110 && blue < 110 && red > blue+60 {
				n++
			}
		}
	}
	return n
}

// countSelectionPixels counts the border/ring color: the yellow-green
// selection color measured at (224,208,67) and (155,188,58).
func countSelectionPixels(img *image.RGBA, r image.Rectangle) int {
	n := 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			i := img.PixOffset(x, y)
			red, green, blue := int(img.Pix[i]), int(img.Pix[i+1]), int(img.Pix[i+2])
			if green > 170 && blue < 100 && red > 130 {
				n++
			}
		}
	}
	return n
}

// lineMAD is one region's mean absolute grayscale difference.
func lineMAD(a, b *image.RGBA, r image.Rectangle) float64 {
	var sum float64
	n := 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			d := grayAt(a, x, y) - grayAt(b, x, y)
			if d < 0 {
				d = -d
			}
			sum += float64(d)
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

func grayAt(img *image.RGBA, x, y int) int {
	i := img.PixOffset(x, y)
	return (int(img.Pix[i]) + int(img.Pix[i+1]) + int(img.Pix[i+2])) / 3
}

func maxChannelDiff(a, b color.RGBA) int {
	d := absInt(int(a.R) - int(b.R))
	if v := absInt(int(a.G) - int(b.G)); v > d {
		d = v
	}
	if v := absInt(int(a.B) - int(b.B)); v > d {
		d = v
	}
	return d
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func boundsOrNil(img *image.RGBA) image.Rectangle {
	if img == nil {
		return image.Rectangle{}
	}
	return img.Bounds()
}
