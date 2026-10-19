package collect

import (
	"fmt"
	"image"

	"github.com/LoTwT/youkai/internal/platform"
)

// Layout is one display configuration's calibrated anchor set (T5 §3
// 可操作位置): every region is in frame-image pixels — the pixels of a
// frame captured under the geometry the calibration was taken for — and
// the traversal binds them to the session's baseline geometry version
// during Precheck, naming that version in every input it derives from
// them.
//
// PR 4c defines the structure with deliberately empty defaults: the
// values arrive with 4b's field calibration (real sampled coordinates
// per supported display configuration), and an empty or inconsistent
// Layout makes Precheck fail with anchorsMissing — the traversal never
// guesses a coordinate or treats "somewhere in the frame" as an anchor.
type Layout struct {
	// Rows and Columns describe the visible warehouse grid: the grid has
	// Rows*Columns slots and Slots lists them in reading order
	// (row-major). A scroll advances the list by whole rows; the overlap
	// between two viewports is at least one row (T5 §5).
	Rows, Columns int
	// Slots are the clickable grid regions in reading order; the
	// traversal clicks a slot's center and the matcher reports per-slot
	// occupancy against the same order. len(Slots) == Rows*Columns.
	Slots []image.Rectangle
	// Close is the detail page's close control: the region whose click
	// returns from the disc detail to the warehouse list.
	Close image.Rectangle
	// ScrollFrom and ScrollTo are the drag's endpoints for one small
	// scroll step (the synthesized wheel is not an option on the macOS
	// cloud client, so scrolling is a drag on both platforms). One step
	// advances the list by whole rows; how many is the correspondence
	// evidence's verdict, not this pair's business.
	ScrollFrom, ScrollTo image.Point
}

// validate checks the layout's structural sanity. The pixel values
// themselves are calibration data this check cannot judge; what it
// refuses is a layout the traversal could only use by guessing: a
// missing grid, slots that do not add up, empty regions, or a drag that
// goes nowhere.
// Validate checks the layout's structural sanity; see the Layout doc.
// Exported because the calibration side (4b) validates a layout before
// injecting it, and the traversal's precheck runs the same check.
func (l Layout) Validate() error {
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
	if l.Close.Empty() {
		return fmt.Errorf("collect: layout: the close anchor is empty; every anchor region must be a real region")
	}
	if l.ScrollFrom.Eq(l.ScrollTo) {
		return fmt.Errorf("collect: layout: the scroll drag goes nowhere; ScrollFrom and ScrollTo must differ")
	}
	return nil
}

// fitsContent reports whether every anchor lies inside the frame's
// valid content region (image pixels): input derived from an anchor
// outside the content region would be refused by the platform, and a
// calibration that does not fit the captured content is not this
// geometry's calibration.
// FitsContent reports whether every anchor lies inside a frame's valid
// content region; see the Layout doc. Exported for the same reason as
// Validate.
func (l Layout) FitsContent(content image.Rectangle) bool {
	inside := func(r image.Rectangle) bool { return r.In(content) }
	for _, s := range l.Slots {
		if !inside(s) {
			return false
		}
	}
	return inside(l.Close) &&
		ptIn(content, l.ScrollFrom) && ptIn(content, l.ScrollTo)
}

func ptIn(r image.Rectangle, p image.Point) bool { return p.In(r) }

// slotCenter is the click point of slot i: the region's center. The
// calibration encodes any "avoid this part of the cell" knowledge in the
// region itself; the traversal does not second-guess it.
func (l Layout) slotCenter(i int) image.Point {
	return rectCenter(l.Slots[i])
}

// closeCenter is the click point of the close control.
func (l Layout) closeCenter() image.Point {
	return rectCenter(l.Close)
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
