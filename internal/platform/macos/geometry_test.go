package macos_test

import (
	"math"
	"testing"

	macos "github.com/LoTwT/youkai/internal/platform/macos"
)

// Geometry and coordinate tests (T2 §3.1): every layer keeps its unit,
// the three frame ratios are not interchangeable, capture padding
// stays outside the valid rect, non-1:1 scales and negative secondary
// display coordinates round-trip within the documented error bound,
// and inconsistent geometry produces no transform at all.

func fullSurfaceGeometry(scale float64) macos.FrameGeometry {
	// A 1280×720 point window on a 2× display, captured at native
	// size: content covers the whole surface.
	return macos.FrameGeometry{
		SurfaceWidth:  int(1280 * scale),
		SurfaceHeight: int(720 * scale),
		BytesPerRow:   int(1280*scale) * 4,
		ContentRect:   macos.PointRect{X: 0, Y: 0, W: 1280, H: 720},
		ContentScale:  1,
		ScaleFactor:   scale,
		WindowFrame:   macos.PointRect{X: 100, Y: 50, W: 1280, H: 720},
	}
}

func TestGeometryIdentityScaleMapsPixelsToScreenPoints(t *testing.T) {
	tr, err := macos.NewGeometryTransform(fullSurfaceGeometry(1))
	if err != nil {
		t.Fatalf("identity transform: %v", err)
	}
	// Pixel (0,0) is the window's top-left; the far corner is its
	// bottom-right.
	if got := tr.PixelToScreen(macos.PixelPoint{0, 0}); got != (macos.Point{X: 100, Y: 50}) {
		t.Fatalf("top-left mapped to %v", got)
	}
	if got := tr.PixelToScreen(macos.PixelPoint{1279, 719}); got != (macos.Point{X: 100 + 1279, Y: 50 + 719}) {
		t.Fatalf("far corner mapped to %v", got)
	}
	if got := tr.ValidRect(); got != (macos.PixelRect{X: 0, Y: 0, W: 1280, H: 720}) {
		t.Fatalf("valid rect %v", got)
	}
}

func TestGeometryRetinaScaleHalvesPixels(t *testing.T) {
	tr, err := macos.NewGeometryTransform(fullSurfaceGeometry(2))
	if err != nil {
		t.Fatalf("retina transform: %v", err)
	}
	// Two pixels per point: the pixel at (2,2) is one point inside the
	// window.
	if got := tr.PixelToScreen(macos.PixelPoint{2, 2}); got != (macos.Point{X: 101, Y: 51}) {
		t.Fatalf("retina pixel mapped to %v", got)
	}
	if got := tr.ValidRect(); got != (macos.PixelRect{X: 0, Y: 0, W: 2560, H: 1440}) {
		t.Fatalf("retina valid rect %v", got)
	}
}

func TestGeometryNegativeSecondaryDisplayCoordinates(t *testing.T) {
	g := fullSurfaceGeometry(2)
	// The window lives entirely on the secondary display to the left
	// of the main one, so every coordinate is negative.
	g.WindowFrame = macos.PointRect{X: -1280, Y: -720, W: 1280, H: 720}
	tr, err := macos.NewGeometryTransform(g)
	if err != nil {
		t.Fatalf("negative origin transform: %v", err)
	}
	if got := tr.PixelToScreen(macos.PixelPoint{0, 0}); got != (macos.Point{X: -1280, Y: -720}) {
		t.Fatalf("negative top-left mapped to %v", got)
	}
	p, err := tr.ScreenToPixel(macos.Point{X: -1280 + 640, Y: -720 + 360})
	if err != nil {
		t.Fatalf("negative screen point: %v", err)
	}
	if p != (macos.PixelPoint{X: 1280, Y: 720}) {
		t.Fatalf("negative screen point mapped to %v", p)
	}
}

func TestGeometryRoundTripErrorBound(t *testing.T) {
	for _, scale := range []float64{1, 2} {
		g := fullSurfaceGeometry(scale)
		tr, err := macos.NewGeometryTransform(g)
		if err != nil {
			t.Fatalf("scale %v: %v", scale, err)
		}
		// Every interior pixel round-trips to itself: the inverse maps
		// to the nearest pixel, and a pixel center is at most half a
		// pixel away from its own rounded image.
		w, h := tr.ValidRect().MaxX(), tr.ValidRect().MaxY()
		for px := 1; px < w; px += 137 {
			for py := 1; py < h; py += 89 {
				p := macos.PixelPoint{px, py}
				back, err := tr.ScreenToPixel(tr.PixelToScreen(p))
				if err != nil {
					t.Fatalf("scale %v pixel %v: %v", scale, p, err)
				}
				dx := math.Abs(float64(back.X - p.X))
				dy := math.Abs(float64(back.Y - p.Y))
				if dx > 1 || dy > 1 {
					t.Fatalf("scale %v pixel %v round-tripped to %v", scale, p, back)
				}
			}
		}
	}
}

func TestGeometryCapturePaddingStaysOutsideValidRect(t *testing.T) {
	// The stream pads 20 points left and 12 points top around the
	// content (capture 留边): padding pixels must not be part of the
	// valid rect, and content coordinates are shifted accordingly.
	g := fullSurfaceGeometry(2)
	g.SurfaceWidth = 2640
	g.SurfaceHeight = 1488
	g.BytesPerRow = 2640 * 4
	g.ContentRect = macos.PointRect{X: 20, Y: 12, W: 1280, H: 720}
	tr, err := macos.NewGeometryTransform(g)
	if err != nil {
		t.Fatalf("padded transform: %v", err)
	}
	if got := tr.ValidRect(); got != (macos.PixelRect{X: 40, Y: 24, W: 2560, H: 1440}) {
		t.Fatalf("padded valid rect %v", got)
	}
	// The content's first pixel (40,24) maps to the window origin.
	if got := tr.PixelToScreen(macos.PixelPoint{40, 24}); got != (macos.Point{X: 100, Y: 50}) {
		t.Fatalf("padded content origin mapped to %v", got)
	}
	// A screen point in the padding area of the surface has no valid
	// pixel: it is outside the window frame anyway, so the inverse
	// refuses it.
	if _, err := tr.ScreenToPixel(macos.Point{X: 95, Y: 50}); err == nil {
		t.Fatal("screen point left of the window mapped to a pixel")
	}
}

func TestGeometryNonIdentityContentScale(t *testing.T) {
	// A stream that scaled the original content to half size into the
	// surface: contentScale 0.5 with a 2× surface scale factor. The
	// surface is 1280×720 pixels for a 1280×720 point window.
	g := macos.FrameGeometry{
		SurfaceWidth:  1280,
		SurfaceHeight: 720,
		BytesPerRow:   1280 * 4,
		ContentRect:   macos.PointRect{X: 0, Y: 0, W: 640, H: 360},
		ContentScale:  0.5,
		ScaleFactor:   2,
		WindowFrame:   macos.PointRect{X: 0, Y: 0, W: 1280, H: 720},
	}
	tr, err := macos.NewGeometryTransform(g)
	if err != nil {
		t.Fatalf("contentScale transform: %v", err)
	}
	// One content point covers two surface points here (contentScale
	// 0.5 halves the 1280×720 point window into a 640×360 point
	// surface, and scaleFactor 2 turns that into 1280×720 pixels), so
	// one output pixel is one original content point: pixel (2,2) is
	// screen (2,2).
	if got := tr.PixelToScreen(macos.PixelPoint{2, 2}); got != (macos.Point{X: 2, Y: 2}) {
		t.Fatalf("scaled pixel mapped to %v", got)
	}
	p, err := tr.ScreenToPixel(macos.Point{X: 640, Y: 360})
	if err != nil {
		t.Fatalf("scaled screen point: %v", err)
	}
	if p != (macos.PixelPoint{X: 640, Y: 360}) {
		t.Fatalf("window center mapped to %v", p)
	}
}

func TestGeometryRejectsInconsistentFrames(t *testing.T) {
	cases := map[string]func(g *macos.FrameGeometry){
		"zero surface":          func(g *macos.FrameGeometry) { g.SurfaceWidth = 0 },
		"missing scale factor":  func(g *macos.FrameGeometry) { g.ScaleFactor = 0 },
		"missing content scale": func(g *macos.FrameGeometry) { g.ContentScale = 0 },
		"empty content rect":    func(g *macos.FrameGeometry) { g.ContentRect = macos.PointRect{X: 0, Y: 0, W: 0, H: 720} },
		"empty window frame":    func(g *macos.FrameGeometry) { g.WindowFrame = macos.PointRect{X: 0, Y: 0, W: 0, H: 720} },
		"content outside surface": func(g *macos.FrameGeometry) {
			g.ContentRect = macos.PointRect{X: 0, Y: 0, W: 2000, H: 720}
		},
		"window contradicts content": func(g *macos.FrameGeometry) {
			g.WindowFrame = macos.PointRect{X: 0, Y: 0, W: 640, H: 360}
		},
		"row stride too small": func(g *macos.FrameGeometry) { g.BytesPerRow = 4 },
	}
	for name, mutate := range cases {
		g := fullSurfaceGeometry(2)
		mutate(&g)
		if _, err := macos.NewGeometryTransform(g); err == nil {
			t.Fatalf("%s produced a transform", name)
		}
	}
}

func TestGeometryScreenToPixelRefusesOutsideValidRect(t *testing.T) {
	tr, err := macos.NewGeometryTransform(fullSurfaceGeometry(2))
	if err != nil {
		t.Fatalf("transform: %v", err)
	}
	// A point half a window outside must be refused, never clamped to
	// the edge: a clamped click could hit the wrong place.
	for _, p := range []macos.Point{
		{X: 99, Y: 400},   // left of the window
		{X: 1381, Y: 400}, // right of the window
		{X: 400, Y: 49},   // above the window
		{X: 400, Y: 771},  // below the window
	} {
		if _, err := tr.ScreenToPixel(p); err == nil {
			t.Fatalf("point %v outside the window was accepted", p)
		}
	}
}

func TestCropTransformMapsBackToSourcePixels(t *testing.T) {
	// An OCR crop takes the left half of the frame at Retina scale and
	// downsamples it to 640×360: crop coordinates must come back to
	// source frame pixels before any screen mapping.
	src := macos.PixelRect{X: 0, Y: 0, W: 1280, H: 720}
	crop, err := macos.NewCropTransform(src, 640, 360)
	if err != nil {
		t.Fatalf("crop transform: %v", err)
	}
	if got := crop.MapPoint(macos.PixelPoint{0, 0}); got != (macos.PixelPoint{0, 0}) {
		t.Fatalf("crop origin mapped to %v", got)
	}
	if got := crop.MapPoint(macos.PixelPoint{640, 360}); got != (macos.PixelPoint{1280, 720}) {
		t.Fatalf("crop far corner mapped to %v", got)
	}
	// The crop's center pixel maps to the source center.
	if got := crop.MapPoint(macos.PixelPoint{320, 180}); got != (macos.PixelPoint{640, 360}) {
		t.Fatalf("crop center mapped to %v", got)
	}
	if got := crop.MapRect(macos.PixelRect{X: 0, Y: 0, W: 64, H: 36}); got != (macos.PixelRect{X: 0, Y: 0, W: 128, H: 72}) {
		t.Fatalf("crop rect mapped to %v", got)
	}
	if _, err := macos.NewCropTransform(macos.PixelRect{X: 0, Y: 0, W: 0, H: 10}, 10, 10); err == nil {
		t.Fatal("empty crop source accepted")
	}
	if _, err := macos.NewCropTransform(src, 0, 10); err == nil {
		t.Fatal("zero crop destination accepted")
	}
}

func TestCropRectRoundTripsToScreen(t *testing.T) {
	// The documented pipeline: crop pixel → source frame pixel →
	// screen point. The composed result equals a direct mapping.
	tr, err := macos.NewGeometryTransform(fullSurfaceGeometry(2))
	if err != nil {
		t.Fatalf("transform: %v", err)
	}
	crop, err := macos.NewCropTransform(macos.PixelRect{X: 100, Y: 60, W: 500, H: 300}, 250, 150)
	if err != nil {
		t.Fatalf("crop: %v", err)
	}
	cropRect := macos.PixelRect{X: 10, Y: 10, W: 100, H: 50}
	srcRect := crop.MapRect(cropRect)
	screenRect := tr.PixelRectToScreen(srcRect)
	// The same corner mapped directly through the crop point mapping.
	direct := tr.PixelToScreen(crop.MapPoint(macos.PixelPoint{10, 10}))
	if screenRect.X != direct.X || screenRect.Y != direct.Y {
		t.Fatalf("screen rect %v does not start at %v", screenRect, direct)
	}
}

func TestGeometryTrackerBumpsOnMeaningfulChange(t *testing.T) {
	tracker := macos.NewGeometryTracker()
	if got := tracker.Version(); got != 0 {
		t.Fatalf("fresh tracker version %d", got)
	}
	g := fullSurfaceGeometry(2)
	v1 := tracker.Observe(g)
	if v1 != 1 {
		t.Fatalf("first observation version %d", v1)
	}
	// The same geometry observed again keeps the version.
	if got := tracker.Observe(g); got != v1 {
		t.Fatalf("unchanged geometry bumped to %d", got)
	}
	// A window move is a meaningful change.
	g.WindowFrame = macos.PointRect{X: 140, Y: 60, W: 1280, H: 720}
	v2 := tracker.Observe(g)
	if v2 != v1+1 {
		t.Fatalf("window move did not bump: %d then %d", v1, v2)
	}
	// A resize is a meaningful change.
	g.WindowFrame = macos.PointRect{X: 140, Y: 60, W: 1000, H: 600}
	g.ContentRect = macos.PointRect{X: 0, Y: 0, W: 1000, H: 600}
	g.SurfaceWidth = 2000
	g.SurfaceHeight = 1200
	g.BytesPerRow = 2000 * 4
	v3 := tracker.Observe(g)
	if v3 != v2+1 {
		t.Fatalf("resize did not bump: %d then %d", v2, v3)
	}
	// Sub-point jitter below the tolerance keeps the version.
	jitter := g
	jitter.WindowFrame = macos.PointRect{X: 140.0002, Y: 60.0002, W: 1000, H: 600}
	if got := tracker.Observe(jitter); got != v3 {
		t.Fatalf("sub-point jitter bumped to %d", got)
	}
	// A display configuration change bumps too.
	displays := []macos.PointRect{{X: 0, Y: 0, W: 1920, H: 1080}}
	v4 := tracker.ObserveDisplays(displays)
	if v4 != v3+1 {
		t.Fatalf("display change did not bump: %d then %d", v3, v4)
	}
	if got := tracker.ObserveDisplays(displays); got != v4 {
		t.Fatalf("unchanged displays bumped to %d", got)
	}
	reordered := []macos.PointRect{{X: -1280, Y: 0, W: 1280, H: 800}, {X: 0, Y: 0, W: 1920, H: 1080}}
	if got := tracker.ObserveDisplays(reordered); got != v4+1 {
		t.Fatalf("added display did not bump: %d then %d", v4, got)
	}
	// Stale versions are rejected; the current one passes.
	if err := tracker.Check(v1); err == nil {
		t.Fatal("stale geometry version accepted")
	}
	if err := tracker.Check(tracker.Version()); err != nil {
		t.Fatalf("current version rejected: %v", err)
	}
}
