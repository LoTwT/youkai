package collect

import (
	"fmt"
	"image"
	"image/color"
	"time"

	"github.com/LoTwT/youkai/internal/platform"
)

// LineRegion is one calibrated text line of the detail pane: the role the
// line plays (name, level, main, sub1..sub4) and the region the matcher
// reads. Roles are stable identifiers for evidence and for the
// recognition pipeline's later projection; they are not UI labels.
type LineRegion struct {
	Role string          `json:"role"`
	Rect image.Rectangle `json:"rect"`
}

// Detail line roles. The first six always exist on a populated pane; sub4
// only exists for four-substat discs, but its region is calibrated for
// every layout because the matcher must be able to tell the two line
// counts apart (T5 §4).
const (
	LineName  = "name"
	LineLevel = "level"
	LineMain  = "main"
	LineSub1  = "sub1"
	LineSub2  = "sub2"
	LineSub3  = "sub3"
	LineSub4  = "sub4"
)

// Non-recognition context roles: the rarity badge, the selected cell's
// border and the two equipped-agent avatars. The recognizer never reads
// these regions; the projection writes them as admission evidence. The
// detail avatar is the primary equipper evidence, the list avatar confirms
// it, and the identity matcher consumes both crops.
const (
	LineQualityBadge = "qualityBadge"
	LineSelectedCell = "selectedCell"
	LineDetailAvatar = "detailAvatar"
	LineListAvatar   = "listAvatar"
)

// detailLines returns the detail pane's line regions actually present on
// a disc with the given substat count: a three-substat disc has no fourth
// substat row (the band holds the set-effect label instead).
func (l Layout) detailLines(substats int) []LineRegion {
	out := make([]LineRegion, 0, len(l.DetailLines))
	for _, line := range l.DetailLines {
		if line.Role == LineSub4 && substats < 4 {
			continue
		}
		out = append(out, line)
	}
	return out
}

// contextLines returns the admission's non-recognition evidence regions
// for a selected slot: the rarity badge, the slot's selection border and
// the detail/list equipped-agent avatars. The avatars are cropped for the
// identity matcher and persisted as evidence; they never reach the
// recognizer.
func (l Layout) contextLines(slot int) []LineRegion {
	return []LineRegion{
		{Role: LineQualityBadge, Rect: l.QualityBadge},
		{Role: LineSelectedCell, Rect: l.BorderRect(slot)},
		{Role: LineDetailAvatar, Rect: l.DetailAvatar},
		{Role: LineListAvatar, Rect: l.AvatarRect(slot)},
	}
}

// ColorProbe is one small region classified by its mean color: the colors
// name the calibrated tiers (for example "clear"/"active" on the funnel
// and "closed"/"open" on the panel probes) and Tol is the per-channel
// tolerance. A mean outside every tier is "cannot determine", never the
// nearest tier.
type ColorProbe struct {
	At     image.Rectangle
	Colors map[string]color.RGBA
	Tol    int
}

// CellProbes are the per-cell evidence regions, relative to the cell's
// center: the selection border immediately left of the cell, the rarity
// bar along its bottom edge, the management badge in the bottom-right
// corner and the equipped-agent avatar in the top-right corner.
type CellProbes struct {
	Border     image.Rectangle
	Bar        image.Rectangle
	Management image.Rectangle
	Avatar     image.Rectangle
}

// Layout is one display configuration's calibrated anchor set (T5 §3
// 可操作位置): every region is in frame-image pixels — the pixels of a
// frame captured under the geometry the calibration was taken for — and
// the traversal binds them to the session's baseline geometry version
// during Precheck, naming that version in every input it derives from
// them.
//
// The layout is one page: the same frame carries the list, the detail pane
// and the wheel (T5 §1). Image and Content identify the display
// configuration exactly: a frame whose image size or content region
// differs is not this calibration's frame. The pixel matcher only reads
// the regions declared here.
type Layout struct {
	// Name identifies the display configuration in diagnostics and the
	// observed-range report.
	Name string
	// Image is the full frame size and Content the valid video region
	// inside it. Both must match a frame exactly for the layout to apply;
	// a scaled window changes Content and invalidates the calibration.
	Image   image.Point
	Content image.Rectangle

	// Rows and Columns describe the visible warehouse grid: the grid has
	// Rows*Columns slots and Slots lists them in reading order
	// (row-major). A scroll advances the list by whole rows; the overlap
	// between two viewports is at least one row (T5 §5).
	Rows, Columns int
	// Slots are the clickable grid regions in reading order; the traversal
	// clicks a slot's center and the matcher reports per-slot evidence
	// against the same order. len(Slots) == Rows*Columns.
	Slots []image.Rectangle
	// Cell holds the per-cell evidence regions, relative to a slot center.
	Cell CellProbes
	// RowPitch is the list's row pitch in frame pixels. The scroll
	// matcher accepts a displacement only when it is a whole number of
	// row pitches within tolerance, and the traversal applies only
	// displacements that keep at least one row of overlap.
	RowPitch int

	// ListViewport is the list area compared across a scroll: grayscale
	// overlap matching establishes the displacement and the unique
	// correspondences between two viewports (T5 §5).
	ListViewport image.Rectangle
	// ScrollTrack is the scrollbar's thumb column. Top and Bottom rest on
	// the thumb resting at the track's extremes; the two end caps are not
	// part of the track.
	ScrollTrack image.Rectangle
	// ScrollFrom and ScrollTo are the drag's endpoints for one small
	// scroll step (the synthesized wheel is not an option on the macOS
	// cloud client, so scrolling is a drag on both platforms). One step
	// advances the list by whole rows; how many is the correspondence
	// evidence's verdict, not this pair's business.
	ScrollFrom, ScrollTo image.Point
	ScrollDuration       time.Duration
	// TopFrom and TopTo are the drag's endpoints for a fling toward the
	// list's beginning. The traversal uses them only to confirm the top
	// with a stationary verdict in this round.
	TopFrom, TopTo image.Point
	TopDuration    time.Duration

	// FilterButton is the funnel button's click point.
	FilterButton image.Point
	// Funnel is the funnel icon probe: clear (dark) and active (blue)
	// tiers (report §4).
	Funnel ColorProbe
	// PanelOpen are the probes that recognize the filter panel overlaying
	// the warehouse. Both must report "open" for PageFilterPanel and both
	// "closed" before the warehouse checks run.
	PanelOpen []ColorProbe
	// PanelReset, PanelClose and PanelS are the panel's click points. The
	// pixel matcher never reads them. PanelS selects the S rarity filter;
	// the recorded frames contain no selected-S sample, so no selected-state
	// probe is calibrated. The effect is verified through the active funnel
	// after the single panel visit closes (T5 §3).
	PanelReset, PanelClose, PanelS image.Point
	// SortLabel is the filter panel's sort dropdown region and the
	// reference image it must match. The label names the active order
	// ("稀有度" in every recorded sample); a mismatch means the matcher
	// cannot state the order, so the traversal records it as unverified.
	// Both fields are optional together: an uncalibrated layout leaves
	// them empty and every panel verdict carries no sort fact.
	SortLabel    image.Rectangle
	SortTemplate *image.RGBA

	// Wheel are the six wheel position centers, numbered from the
	// top-left counter-clockwise 1..6.
	Wheel [6]image.Point
	// WheelArcs are the selection-color probe regions around each wheel
	// center, relative to the center. They are the narrowed form of the
	// draft's ±95 ring: the fixture keeps four small arc blocks instead of
	// six full ring boxes (design §2).
	WheelArcs []image.Rectangle

	// DetailLines are the detail pane's calibrated line regions in reading
	// order (name, level, main, sub1..sub4). The comparison averages the
	// per-line grayscale MADs.
	DetailLines []LineRegion
	// QualityBadge is the rarity badge next to the level line. Only S is
	// calibrated; every sample of the macOS cloud client was S.
	QualityBadge image.Rectangle
	// DetailAvatar is the equipped-agent avatar immediately right of the
	// level line. Its presence is the primary equipment evidence; the
	// list cell's top-right avatar confirms it.
	DetailAvatar image.Rectangle
	// TopManagement is the lock/state indicator immediately left of the
	// detail page's dismantle control. It confirms the list badge's
	// locked/not-locked dimension; the icon cannot distinguish discarded
	// from none, so the list badge remains the source of that distinction.
	TopManagement image.Rectangle
	// Substat4Probe is the value column of the fourth substat row. A
	// three-substat disc shows the set-effect label there instead, so this
	// probe distinguishes 3 from 4 substats (design §2).
	Substat4Probe image.Rectangle
}

// Validate checks the layout's structural sanity. The pixel values
// themselves are calibration data this check cannot judge; what it
// refuses is a layout the matcher or traversal could only use by
// guessing: a missing grid, slots or probes that do not add up, empty
// regions, or drags that go nowhere. PixelMatcher construction and the
// traversal's precheck both run this check.
func (l Layout) Validate() error {
	if l.Image.X <= 0 || l.Image.Y <= 0 {
		return fmt.Errorf("collect: layout: image size is %dx%d; a calibrated layout needs a positive frame size", l.Image.X, l.Image.Y)
	}
	if l.Content.Empty() || !l.Content.In(image.Rectangle{Max: l.Image}) {
		return fmt.Errorf("collect: layout: content region %v is empty or outside the %dx%d frame", l.Content, l.Image.X, l.Image.Y)
	}
	if l.Rows <= 0 || l.Columns <= 0 {
		return fmt.Errorf("collect: layout: grid is %d×%d; the calibrated grid must be positive", l.Rows, l.Columns)
	}
	if len(l.Slots) != l.Rows*l.Columns {
		return fmt.Errorf("collect: layout: %d slots for a %d×%d grid; slots must number Rows×Columns in reading order",
			len(l.Slots), l.Rows, l.Columns)
	}
	for i, r := range l.Slots {
		if r.Empty() {
			return fmt.Errorf("collect: layout: slot %d is empty; every anchor region must be a real region", i)
		}
	}
	if l.Cell.Border.Empty() {
		return fmt.Errorf("collect: layout: the selection-border probe is empty")
	}
	if l.Cell.Bar.Empty() {
		return fmt.Errorf("collect: layout: the occupancy-bar probe is empty")
	}
	if l.Cell.Management.Empty() {
		return fmt.Errorf("collect: layout: the management-badge probe is empty")
	}
	if l.Cell.Avatar.Empty() {
		return fmt.Errorf("collect: layout: the cell-avatar probe is empty")
	}
	if l.RowPitch <= 0 {
		return fmt.Errorf("collect: layout: row pitch is %d; it must be positive", l.RowPitch)
	}
	if l.ListViewport.Empty() {
		return fmt.Errorf("collect: layout: the list viewport is empty")
	}
	if l.ScrollTrack.Empty() {
		return fmt.Errorf("collect: layout: the scrollbar track is empty")
	}
	if l.ScrollFrom.Eq(l.ScrollTo) {
		return fmt.Errorf("collect: layout: the scroll drag goes nowhere; ScrollFrom and ScrollTo must differ")
	}
	if l.ScrollDuration <= 0 {
		return fmt.Errorf("collect: layout: the scroll duration is not positive")
	}
	if l.TopFrom.Eq(l.TopTo) {
		return fmt.Errorf("collect: layout: the top drag goes nowhere; TopFrom and TopTo must differ")
	}
	if l.TopDuration <= 0 {
		return fmt.Errorf("collect: layout: the top duration is not positive")
	}
	if err := l.Funnel.validate("funnel"); err != nil {
		return err
	}
	if len(l.PanelOpen) < 2 {
		return fmt.Errorf("collect: layout: %d panel probes; the panel needs both calibrated probes", len(l.PanelOpen))
	}
	for i, p := range l.PanelOpen {
		if err := p.validate(fmt.Sprintf("panel probe %d", i)); err != nil {
			return err
		}
	}
	if l.PanelS == (image.Point{}) || !ptIn(l.Content, l.PanelS) {
		return fmt.Errorf("collect: layout: the S rarity control point %v is empty or outside the content region %v",
			l.PanelS, l.Content)
	}
	switch {
	case l.SortLabel.Empty() && l.SortTemplate == nil:
		// The layout does not claim a sort read; panel verdicts simply
		// carry no sort fact.
	case l.SortLabel.Empty() || l.SortTemplate == nil:
		return fmt.Errorf("collect: layout: the sort label region and its reference template must be calibrated together")
	case !l.SortLabel.In(l.Content):
		return fmt.Errorf("collect: layout: the sort label region %v is outside the content region %v",
			l.SortLabel, l.Content)
	case l.SortTemplate.Bounds().Size() != l.SortLabel.Size():
		return fmt.Errorf("collect: layout: the sort template is %v but the sort label region is %v",
			l.SortTemplate.Bounds().Size(), l.SortLabel.Size())
	}
	if l.Wheel == [6]image.Point{} {
		return fmt.Errorf("collect: layout: the wheel positions are not calibrated")
	}
	if len(l.WheelArcs) == 0 {
		return fmt.Errorf("collect: layout: the wheel probes are empty")
	}
	for i, r := range l.WheelArcs {
		if r.Empty() {
			return fmt.Errorf("collect: layout: wheel probe %d is empty", i)
		}
	}
	if len(l.DetailLines) == 0 {
		return fmt.Errorf("collect: layout: the detail lines are not calibrated")
	}
	for i, line := range l.DetailLines {
		if line.Role == "" || line.Rect.Empty() {
			return fmt.Errorf("collect: layout: detail line %d is empty or has no role", i)
		}
	}
	if l.QualityBadge.Empty() {
		return fmt.Errorf("collect: layout: the quality badge probe is empty")
	}
	if l.DetailAvatar.Empty() {
		return fmt.Errorf("collect: layout: the detail-avatar probe is empty")
	}
	if l.TopManagement.Empty() {
		return fmt.Errorf("collect: layout: the top management-indicator probe is empty")
	}
	if l.Substat4Probe.Empty() {
		return fmt.Errorf("collect: layout: the substat-4 probe is empty")
	}
	return nil
}

func (p ColorProbe) validate(what string) error {
	if p.At.Empty() {
		return fmt.Errorf("collect: layout: %s region is empty", what)
	}
	if len(p.Colors) == 0 {
		return fmt.Errorf("collect: layout: %s has no calibrated colors", what)
	}
	if p.Tol < 0 {
		return fmt.Errorf("collect: layout: %s tolerance is negative", what)
	}
	return nil
}

// MatchesFrame reports whether a frame's image size and content region are
// exactly the calibration's. Content is compared for equality, not
// containment: a scaled window makes the pixel anchors a different
// display configuration's calibration.
func (l Layout) MatchesFrame(imageSize image.Point, content image.Rectangle) bool {
	return l.Image == imageSize && l.Content == content
}

// FitsContent reports whether every anchor lies inside the frame's valid
// content region (image pixels): input derived from an anchor outside the
// content region would be refused by the platform, and a calibration that
// does not fit the captured content is not this geometry's calibration.
func (l Layout) FitsContent(content image.Rectangle) bool {
	inside := func(r image.Rectangle) bool { return r != image.Rectangle{} && r.In(content) }
	for _, s := range l.Slots {
		if !inside(s) {
			return false
		}
	}
	for i := range l.Slots {
		if !inside(l.BorderRect(i)) || !inside(l.BarRect(i)) ||
			!inside(l.ManagementRect(i)) || !inside(l.AvatarRect(i)) {
			return false
		}
	}
	if !inside(l.ListViewport) || !inside(l.ScrollTrack) || !inside(l.Funnel.At) {
		return false
	}
	if !inside(l.QualityBadge) || !inside(l.DetailAvatar) || !inside(l.TopManagement) || !inside(l.Substat4Probe) {
		return false
	}
	for _, p := range l.PanelOpen {
		if !inside(p.At) {
			return false
		}
	}
	for _, line := range l.DetailLines {
		if !inside(line.Rect) {
			return false
		}
	}
	for _, c := range l.Wheel {
		if !ptIn(content, c) {
			return false
		}
	}
	for i := range l.Wheel {
		for _, rel := range l.WheelArcs {
			if !inside(rel.Add(l.Wheel[i])) {
				return false
			}
		}
	}
	return ptIn(content, l.ScrollFrom) && ptIn(content, l.ScrollTo) &&
		ptIn(content, l.TopFrom) && ptIn(content, l.TopTo) &&
		ptIn(content, l.FilterButton) && ptIn(content, l.PanelReset) &&
		ptIn(content, l.PanelClose) && ptIn(content, l.PanelS)
}

// ReadRegions returns every region the pixel matcher reads, in frame
// pixels. The fixture tool blacks out everything else; the test that
// guards the "only declared regions" constraint corrupts everything else
// and expects the same verdict. MatchScroll additionally reads
// ListViewport, which is a declared layout field and is included here.
func (l Layout) ReadRegions() []image.Rectangle {
	regions := make([]image.Rectangle, 0, len(l.Slots)*4+len(l.DetailLines)+len(l.PanelOpen)+len(l.Wheel)*len(l.WheelArcs)+9)
	for i := range l.Slots {
		regions = append(regions, l.BorderRect(i), l.BarRect(i), l.ManagementRect(i), l.AvatarRect(i))
	}
	regions = append(regions, l.ListViewport, l.ScrollTrack, l.Funnel.At)
	for _, p := range l.PanelOpen {
		regions = append(regions, p.At)
	}
	if !l.SortLabel.Empty() {
		regions = append(regions, l.SortLabel)
	}
	for _, c := range l.Wheel {
		for _, rel := range l.WheelArcs {
			regions = append(regions, rel.Add(c))
		}
	}
	for _, line := range l.DetailLines {
		regions = append(regions, line.Rect)
	}
	return append(regions, l.QualityBadge, l.DetailAvatar, l.TopManagement, l.Substat4Probe)
}

// BorderRect is slot i's selection-border probe in frame pixels.
func (l Layout) BorderRect(i int) image.Rectangle {
	return l.Cell.Border.Add(slotCenterOf(l.Slots[i]))
}

// BarRect is slot i's occupancy-bar probe in frame pixels.
func (l Layout) BarRect(i int) image.Rectangle {
	return l.Cell.Bar.Add(slotCenterOf(l.Slots[i]))
}

// ManagementRect is slot i's management-badge probe in frame pixels.
func (l Layout) ManagementRect(i int) image.Rectangle {
	return l.Cell.Management.Add(slotCenterOf(l.Slots[i]))
}

// AvatarRect is slot i's equipped-agent avatar probe in frame pixels.
func (l Layout) AvatarRect(i int) image.Rectangle {
	return l.Cell.Avatar.Add(slotCenterOf(l.Slots[i]))
}

// slotCenter is the click point of slot i: the region's center. The
// calibration encodes any "avoid this part of the cell" knowledge in the
// region itself; the traversal does not second-guess it.
func (l Layout) slotCenter(i int) image.Point {
	return slotCenterOf(l.Slots[i])
}

func slotCenterOf(r image.Rectangle) image.Point {
	return r.Min.Add(r.Size().Div(2))
}

// rectCenter is one region's center point, rounded towards Min.
func rectCenter(r image.Rectangle) image.Point {
	return r.Min.Add(r.Size().Div(2))
}

// slotPosition maps a slot index to its grid position (row, column).
func (l Layout) slotPosition(i int) (row, column int) {
	return i / l.Columns, i % l.Columns
}

// positionSlot maps a grid position back to its slot index.
func (l Layout) positionSlot(row, column int) int {
	return row*l.Columns + column
}

func ptIn(r image.Rectangle, p image.Point) bool { return p.In(r) }

// bindGeometry returns the geometry version the anchors are bound to
// for this session: the observed baseline version (Precheck runs after
// the controller locked the baseline, so the observation carries it).
// Every input op derived from the anchors names this version, and every
// frame consumed as evidence must carry it.
func bindGeometry(obs platform.Observation) (platform.GeometryVersion, error) {
	if obs.Geometry == 0 {
		return 0, fmt.Errorf("collect: layout: the observation carries no geometry version to bind the anchors to")
	}
	return obs.Geometry, nil
}
