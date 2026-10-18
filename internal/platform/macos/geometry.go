package macos

import (
	"errors"
	"fmt"
	"math"
	"sync"
)

// Geometry and coordinate units (T2 §3.1). Every layer keeps its own
// unit: screen points (top-left origin, the space of SCWindow.frame and
// Quartz event coordinates), surface points (the point grid inside the
// captured output surface) and image pixels (the output surface's
// actual pixels). contentRect, contentScale and scaleFactor model the
// relations between them and are not interchangeable ratios.

// GeometryVersion numbers one consistent geometry state. Any change —
// window move or resize, content rect or scale changes, display
// configuration changes — produces a new version, and input requests
// carrying an old version are rejected (T2 §3.1: old coordinates must
// not be dispatched after the geometry changed).
type GeometryVersion uint64

// Point is a position in screen points. The origin is the top-left
// corner of the main display; secondary displays may extend into
// negative coordinates. This matches SCWindow.frame and Quartz global
// display coordinates, and deliberately not AppKit's bottom-left
// origin.
type Point struct{ X, Y float64 }

// PointRect is a rectangle in points with top-left origin (X, Y) and
// size (W, H).
type PointRect struct{ X, Y, W, H float64 }

// PixelPoint is a position in image pixels of the capture output
// surface.
type PixelPoint struct{ X, Y int }

// PixelRect is a rectangle in image pixels with origin (X, Y) and size
// (W, H).
type PixelRect struct{ X, Y, W, H int }

// MinX/MinY/MaxX/MaxY return the rectangle's edges.
func (r PointRect) MinX() float64 { return r.X }
func (r PointRect) MinY() float64 { return r.Y }
func (r PointRect) MaxX() float64 { return r.X + r.W }
func (r PointRect) MaxY() float64 { return r.Y + r.H }

// MinX/MinY/MaxX/MaxY return the rectangle's edges.
func (r PixelRect) MinX() int { return r.X }
func (r PixelRect) MinY() int { return r.Y }
func (r PixelRect) MaxX() int { return r.X + r.W }
func (r PixelRect) MaxY() int { return r.Y + r.H }

// Empty reports whether the rectangle has no area.
func (r PointRect) Empty() bool { return r.W <= 0 || r.H <= 0 }

// Empty reports whether the rectangle has no area.
func (r PixelRect) Empty() bool { return r.W <= 0 || r.H <= 0 }

// Contains reports whether p lies inside r, allowing eps of slack on
// every edge to absorb floating-point noise between two sources of
// window geometry.
func (r PointRect) Contains(p Point, eps float64) bool {
	return p.X >= r.X-eps && p.X <= r.MaxX()+eps && p.Y >= r.Y-eps && p.Y <= r.MaxY()+eps
}

// Intersect returns the intersection of r and o; an empty result means
// the rectangles do not overlap.
func (r PointRect) Intersect(o PointRect) PointRect {
	x := math.Max(r.X, o.X)
	y := math.Max(r.Y, o.Y)
	w := math.Min(r.MaxX(), o.MaxX()) - x
	h := math.Min(r.MaxY(), o.MaxY()) - y
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	return PointRect{X: x, Y: y, W: w, H: h}
}

// pointNearlyEqual reports whether a and b differ by at most eps.
func pointNearlyEqual(a, b, eps float64) bool { return math.Abs(a-b) <= eps }

// rectNearlyEqual reports whether both rectangles agree within eps on
// every edge.
func rectNearlyEqual(a, b PointRect, eps float64) bool {
	return pointNearlyEqual(a.X, b.X, eps) && pointNearlyEqual(a.Y, b.Y, eps) &&
		pointNearlyEqual(a.W, b.W, eps) && pointNearlyEqual(a.H, b.H, eps)
}

// Comparison tolerances. Scales come from frame attachments as exact
// doubles (1, 2, 0.5, …) so they use a tight epsilon; point geometry
// can differ by sub-point rounding between sources, so it gets one
// thousandth of a point. The cross-check between window size and
// content size allows two points because the two values come from
// independent systems (WindowServer frame versus stream attachments);
// the constants are candidates until real frames confirm them (实测轮
// ①).
const (
	scaleEpsilon    = 1e-9
	pointEpsilon    = 1e-3
	windowSizeLimit = 2.0
)

// FrameGeometry is the per-frame geometry record, combining the frame
// attachments with the window's screen rect (T2 §3.1):
//
//   - ScaleFactor is pixels per point: it relates the surface's point
//     grid to its pixels (contentRect × scaleFactor gives the content
//     area in surface pixels).
//   - ContentRect is the content's position and size in points within
//     the surface; the surface may pad around it (capture 留边).
//   - ContentScale is the scale from the original content to the
//     surface (1 when the stream captures at native size, which is this
//     package's configuration; other values are handled but only
//     validated by the consistency checks below until real frames are
//     observed).
//   - WindowFrame is the window's rect in screen points, from the
//     frame's screenRect attachment when present (macOS 13.1+) or from
//     the enumeration-time observation otherwise.
//
// The three ratios are distinct: ScaleFactor converts units inside the
// surface, ContentScale converts surface content back to the original
// content, and neither may be replaced by the scanner's own display
// scale.
type FrameGeometry struct {
	// SurfaceWidth and SurfaceHeight are the output surface size in
	// pixels (CVPixelBuffer dimensions).
	SurfaceWidth, SurfaceHeight int
	// BytesPerRow is the surface's row stride in bytes (BGRA).
	BytesPerRow int
	// ContentRect is the content rectangle in points within the
	// surface.
	ContentRect PointRect
	// ContentScale is the original-content-to-surface scale.
	ContentScale float64
	// ScaleFactor is pixels per point within the surface.
	ScaleFactor float64
	// WindowFrame is the window rect in screen points.
	WindowFrame PointRect
}

// GeometryTransform maps coordinates between image pixels of one frame
// and screen points, per T2 §3.1. Construction validates that the
// frame's attachments are mutually consistent; frames that fail
// validation produce no transform, and their pixels must not be used to
// derive input coordinates.
//
// Composition (documented per API unit, not by guessing a single
// ratio):
//
//	pixel → surface point: divide by ScaleFactor
//	surface point → content point: subtract ContentRect origin
//	content point → original content point: divide by ContentScale
//	original content point → screen point: add WindowFrame origin
//
// The inverse runs the chain backwards. Rounding happens exactly once,
// at the pixel step of the inverse, with half-away-from-zero rounding;
// a screen point is therefore never off by more than half a surface
// pixel plus ContentScale magnification from its exact image position.
// These relations still need confirmation against marked points on
// real frames (实测轮 ①); the consistency checks refuse frames whose
// numbers contradict each other instead of guessing.
type GeometryTransform struct {
	geo           FrameGeometry
	valid         PixelRect
	surfacePoints PointRect // the surface expressed in points
}

// NewGeometryTransform validates g and returns its transform. It
// returns an error when any attachment is missing (manifesting as zero
// scales), when the content rect lies outside the surface, or when the
// window size contradicts the content size: in those cases the frame's
// coordinates cannot be trusted and must not produce input.
func NewGeometryTransform(g FrameGeometry) (*GeometryTransform, error) {
	if g.SurfaceWidth <= 0 || g.SurfaceHeight <= 0 {
		return nil, fmt.Errorf("macos: invalid surface size %dx%d", g.SurfaceWidth, g.SurfaceHeight)
	}
	if g.BytesPerRow < g.SurfaceWidth*4 {
		return nil, fmt.Errorf("macos: bytes per row %d smaller than BGRA row %d", g.BytesPerRow, g.SurfaceWidth*4)
	}
	if g.ScaleFactor <= 0 {
		return nil, errors.New("macos: missing or invalid frame scaleFactor")
	}
	if g.ContentScale <= 0 {
		return nil, errors.New("macos: missing or invalid frame contentScale")
	}
	if g.ContentRect.Empty() {
		return nil, errors.New("macos: missing or empty frame contentRect")
	}
	if g.WindowFrame.Empty() {
		return nil, errors.New("macos: missing or empty window frame")
	}
	t := &GeometryTransform{geo: g}
	t.surfacePoints = PointRect{X: 0, Y: 0, W: float64(g.SurfaceWidth) / g.ScaleFactor, H: float64(g.SurfaceHeight) / g.ScaleFactor}
	// The content rect, in points, must lie inside the surface's point
	// bounds (a small negative origin or slight overflow is tolerated
	// as rounding noise and clamped by the valid-rect computation).
	const boundSlack = 0.01
	if g.ContentRect.X < -boundSlack || g.ContentRect.Y < -boundSlack ||
		g.ContentRect.MaxX() > t.surfacePoints.MaxX()+boundSlack ||
		g.ContentRect.MaxY() > t.surfacePoints.MaxY()+boundSlack {
		return nil, fmt.Errorf("macos: content rect %v outside surface point bounds %v", g.ContentRect, t.surfacePoints)
	}
	// The window size (screen points) must match the original content
	// size (content rect scaled back by contentScale); a mismatch means
	// the attachments disagree about what is captured.
	if math.Abs(g.WindowFrame.W-g.ContentRect.W/g.ContentScale) > windowSizeLimit ||
		math.Abs(g.WindowFrame.H-g.ContentRect.H/g.ContentScale) > windowSizeLimit {
		return nil, fmt.Errorf("macos: window frame size %v contradicts content size %v (contentScale %v)",
			Point{g.WindowFrame.W, g.WindowFrame.H}, Point{g.ContentRect.W, g.ContentRect.H}, g.ContentScale)
	}
	t.valid = t.pixelRectFromPoints(g.ContentRect)
	if t.valid.Empty() {
		return nil, fmt.Errorf("macos: content rect %v rounds to an empty pixel rect", g.ContentRect)
	}
	return t, nil
}

// Geometry returns the frame geometry the transform was built from.
func (t *GeometryTransform) Geometry() FrameGeometry { return t.geo }

// ValidRect returns the content area of the surface in image pixels.
// Pixels outside it are stream padding, not window content; OCR crops
// must lie inside it and input coordinates must derive from it.
func (t *GeometryTransform) ValidRect() PixelRect { return t.valid }

// pixelRectFromPoints scales a point rect by ScaleFactor and rounds to
// the nearest pixel, clamped to the surface bounds.
func (t *GeometryTransform) pixelRectFromPoints(r PointRect) PixelRect {
	x0 := clampInt(roundPoint(r.X*t.geo.ScaleFactor), 0, t.geo.SurfaceWidth)
	y0 := clampInt(roundPoint(r.Y*t.geo.ScaleFactor), 0, t.geo.SurfaceHeight)
	x1 := clampInt(roundPoint(r.MaxX()*t.geo.ScaleFactor), 0, t.geo.SurfaceWidth)
	y1 := clampInt(roundPoint(r.MaxY()*t.geo.ScaleFactor), 0, t.geo.SurfaceHeight)
	return PixelRect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
}

// roundPoint rounds half away from zero.
func roundPoint(v float64) int {
	if v < 0 {
		return int(math.Floor(v - 0.5))
	}
	return int(math.Floor(v + 0.5))
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// PixelToScreen maps a point in image pixels to screen points. It does
// not check whether the pixel lies in the valid content area; use
// ValidRect for that when the point originates from outside a verified
// crop.
func (t *GeometryTransform) PixelToScreen(p PixelPoint) Point {
	return t.pixelToScreen(float64(p.X), float64(p.Y))
}

func (t *GeometryTransform) pixelToScreen(px, py float64) Point {
	g := t.geo
	sx := px / g.ScaleFactor
	sy := py / g.ScaleFactor
	cx := (sx - g.ContentRect.X) / g.ContentScale
	cy := (sy - g.ContentRect.Y) / g.ContentScale
	return Point{X: g.WindowFrame.X + cx, Y: g.WindowFrame.Y + cy}
}

// PixelRectToScreen maps a pixel rectangle to the screen point
// rectangle spanning its corners. This is the path an OCR crop takes
// back to the screen: the crop must first be inverse-mapped to source
// frame pixels (see CropTransform), then mapped here; crop coordinates
// are never dispatched directly.
func (t *GeometryTransform) PixelRectToScreen(r PixelRect) PointRect {
	near := t.pixelToScreen(float64(r.MinX()), float64(r.MinY()))
	far := t.pixelToScreen(float64(r.MaxX()), float64(r.MaxY()))
	x0, y0 := near.X, near.Y
	x1, y1 := far.X, far.Y
	if x1 < x0 {
		x0, x1 = x1, x0
	}
	if y1 < y0 {
		y0, y1 = y1, y0
	}
	return PointRect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
}

// ScreenToPixel maps a screen point to the nearest image pixel. It
// returns an error when the point does not lie in the frame's valid
// content area: coordinates outside the verified content are rejected,
// not silently clamped to the edge, because a clamped click could hit
// the wrong place (T2: 未支持的配置必须避免错点).
func (t *GeometryTransform) ScreenToPixel(p Point) (PixelPoint, error) {
	g := t.geo
	cx := (p.X - g.WindowFrame.X) * g.ContentScale
	cy := (p.Y - g.WindowFrame.Y) * g.ContentScale
	sx := (cx + g.ContentRect.X) * g.ScaleFactor
	sy := (cy + g.ContentRect.Y) * g.ScaleFactor
	px := PixelPoint{X: roundPoint(sx), Y: roundPoint(sy)}
	if px.X < t.valid.MinX() || px.X > t.valid.MaxX() || px.Y < t.valid.MinY() || px.Y > t.valid.MaxY() {
		return PixelPoint{}, fmt.Errorf("macos: screen point %v maps outside the valid content rect %v", p, t.valid)
	}
	return px, nil
}

// CropTransform maps coordinates of a processed crop (for example an
// OCR crop that was additionally scaled) back to source frame pixels
// (T2 §3.1: 裁剪/放大后的图需要先逆变换回源帧像素). The crop's
// destination coordinates are meaningless for input until mapped
// through this and then through GeometryTransform.
type CropTransform struct {
	src        PixelRect
	dstW, dstH int
}

// NewCropTransform builds the inverse mapping for a crop taken from
// src (in frame pixels) and rendered or resized to dstW×dstH. Either
// dimension must be positive; the crop itself may be any subrect of the
// frame (callers verify it against the frame's valid rect).
func NewCropTransform(src PixelRect, dstW, dstH int) (*CropTransform, error) {
	if src.Empty() {
		return nil, fmt.Errorf("macos: empty crop source rect %v", src)
	}
	if dstW <= 0 || dstH <= 0 {
		return nil, fmt.Errorf("macos: invalid crop destination size %dx%d", dstW, dstH)
	}
	return &CropTransform{src: src, dstW: dstW, dstH: dstH}, nil
}

// MapPoint maps a point in crop destination pixels back to source frame
// pixels, rounding half away from zero.
func (c *CropTransform) MapPoint(p PixelPoint) PixelPoint {
	fx := float64(p.X) * float64(c.src.W) / float64(c.dstW)
	fy := float64(p.Y) * float64(c.src.H) / float64(c.dstH)
	return PixelPoint{X: c.src.X + roundPoint(fx), Y: c.src.Y + roundPoint(fy)}
}

// MapRect maps a rectangle in crop destination pixels back to source
// frame pixels, covering all destination pixels it contains.
func (c *CropTransform) MapRect(r PixelRect) PixelRect {
	p0 := c.MapPoint(PixelPoint{X: r.MinX(), Y: r.MinY()})
	p1 := c.MapPoint(PixelPoint{X: r.MaxX(), Y: r.MaxY()})
	return PixelRect{X: p0.X, Y: p0.Y, W: p1.X - p0.X, H: p1.Y - p0.Y}
}

// GeometryTracker assigns GeometryVersion values to geometry states.
// The first observation is version 1; every later observation that
// differs beyond the comparison tolerances bumps the version. Display
// configuration changes are versioned too. Versions only increase;
// they never go back, so "old version" always means "before the last
// observed change".
type GeometryTracker struct {
	mu       sync.Mutex
	version  GeometryVersion
	current  FrameGeometry
	hasGeo   bool
	displays []PointRect
	hasDisp  bool
}

// NewGeometryTracker returns a tracker at version zero (no geometry
// observed yet).
func NewGeometryTracker() *GeometryTracker { return &GeometryTracker{} }

// Observe records a frame geometry and returns the version in force
// after the observation.
func (t *GeometryTracker) Observe(g FrameGeometry) GeometryVersion {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.hasGeo || !t.geoEqualLocked(g) {
		t.version++
		t.current = g
		t.hasGeo = true
	}
	return t.version
}

func (t *GeometryTracker) geoEqualLocked(g FrameGeometry) bool {
	return t.current.SurfaceWidth == g.SurfaceWidth &&
		t.current.SurfaceHeight == g.SurfaceHeight &&
		rectNearlyEqual(t.current.ContentRect, g.ContentRect, pointEpsilon) &&
		pointNearlyEqual(t.current.ContentScale, g.ContentScale, scaleEpsilon) &&
		pointNearlyEqual(t.current.ScaleFactor, g.ScaleFactor, scaleEpsilon) &&
		rectNearlyEqual(t.current.WindowFrame, g.WindowFrame, pointEpsilon)
}

// ObserveDisplays records the display configuration (frames in screen
// points) and returns the version in force after the observation.
func (t *GeometryTracker) ObserveDisplays(displays []PointRect) GeometryVersion {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.hasDisp || !displaysEqualLocked(t.displays, displays) {
		t.version++
		t.displays = append([]PointRect(nil), displays...)
		t.hasDisp = true
	}
	return t.version
}

func displaysEqualLocked(a, b []PointRect) bool {
	if len(a) != len(b) {
		return false
	}
	used := make([]bool, len(b))
	for _, da := range a {
		found := false
		for i, db := range b {
			if !used[i] && rectNearlyEqual(da, db, pointEpsilon) {
				used[i] = true
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// Version returns the current geometry version (zero before the first
// observation).
func (t *GeometryTracker) Version() GeometryVersion {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.version
}

// Current returns the current frame geometry and whether one was
// observed.
func (t *GeometryTracker) Current() (FrameGeometry, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.current, t.hasGeo
}

// Displays returns the current display configuration and whether one
// was observed.
func (t *GeometryTracker) Displays() ([]PointRect, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.hasDisp {
		return nil, false
	}
	return append([]PointRect(nil), t.displays...), true
}

// Check validates that v is the current version; it returns
// ErrStaleGeometry otherwise. Input requests must pass this check
// before dispatch.
func (t *GeometryTracker) Check(v GeometryVersion) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if v == t.version {
		return nil
	}
	return fmt.Errorf("%w: request has %d, current is %d", ErrStaleGeometry, v, t.version)
}
