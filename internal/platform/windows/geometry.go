package windows

import (
	"fmt"
	"sync"
	"time"
)

// PixelPoint is a point in screen physical pixels. Screen coordinates
// can be negative: displays placed left of or above the primary monitor
// have negative origins.
type PixelPoint struct {
	X, Y int
}

// PixelRect is a rectangle in screen physical pixels. Min is inclusive
// and Max exclusive, like image.Rectangle.
type PixelRect struct {
	Min, Max PixelPoint
}

func (r PixelRect) Dx() int { return r.Max.X - r.Min.X }
func (r PixelRect) Dy() int { return r.Max.Y - r.Min.Y }

// Empty reports whether the rectangle covers no pixel.
func (r PixelRect) Empty() bool { return r.Dx() <= 0 || r.Dy() <= 0 }

// Contains reports whether p is a pixel inside r.
func (r PixelRect) Contains(p PixelPoint) bool {
	return p.X >= r.Min.X && p.X < r.Max.X && p.Y >= r.Min.Y && p.Y < r.Max.Y
}

// ContainsRect reports whether inner lies inside r.
func (r PixelRect) ContainsRect(inner PixelRect) bool {
	return inner.Min.X >= r.Min.X && inner.Min.Y >= r.Min.Y && inner.Max.X <= r.Max.X && inner.Max.Y <= r.Max.Y
}

// GeometryVersion numbers successive client-geometry measurements of the
// locked target. It starts at 1 with the first valid measurement and
// increases whenever any fact the input path depends on changes: the
// client rect, the monitor, or either DPI value. Input requests name the
// version their coordinates were derived from; a request naming an older
// version is rejected, because window moves, size changes, DPI or
// display changes make old coordinates invalid.
type GeometryVersion uint64

// ClientGeometry is one verified client-area measurement of the target,
// bound to a version. All rectangles are screen physical pixels as
// observed by a per-monitor-v2 aware process.
type ClientGeometry struct {
	Version GeometryVersion
	// Client is the target's client area on screen. Min is the
	// screen-space client origin, Max the exclusive far corner.
	Client PixelRect
	// Monitor is the monitor the window currently sits on; its origin
	// may be negative.
	Monitor PixelRect
	// WindowDPI is GetDpiForWindow of the target. It reflects the
	// target window's own DPI awareness — a DPI-unaware window reports
	// 96 regardless of the monitor — so it is recorded as an
	// observation, never used to scale an image.
	WindowDPI uint32
	// MonitorDPI is the effective DPI of the monitor.
	MonitorDPI uint32
	ObservedAt time.Time
}

// GeometryMeasurement is the raw outcome of one measurement pass over
// GeometryAPI, before versioning.
type GeometryMeasurement struct {
	// ClientSize is the client size from GetClientRect.
	ClientSize PixelPoint // X = width, Y = height
	// ClientOrigin is ClientToScreen of (0,0).
	ClientOrigin PixelPoint
	// ClientFarCorner is ClientToScreen of (width,height) — one past the
	// last client pixel, the exclusive far corner.
	ClientFarCorner PixelPoint
	Monitor         PixelRect
	WindowDPI       uint32
	MonitorDPI      uint32
}

// GeometryAPI is the measurement surface for client geometry and DPI
// facts.
type GeometryAPI interface {
	// ClientSize reports the client-area size of hwnd in the window's
	// own coordinate space (physical pixels for a per-monitor-aware
	// target).
	ClientSize(hwnd HWND) (w, h int, err error)
	// ClientPointToScreen maps a client-area point of hwnd to screen
	// physical pixels.
	ClientPointToScreen(hwnd HWND, x, y int) (PixelPoint, error)
	// WindowDPI reports GetDpiForWindow of hwnd, which depends on the
	// target window's own DPI awareness.
	WindowDPI(hwnd HWND) (uint32, error)
	// MonitorDPI reports the effective DPI of the monitor hwnd is on.
	MonitorDPI(hwnd HWND) (uint32, error)
	// MonitorRect reports the monitor rectangle of hwnd in screen
	// physical pixels.
	MonitorRect(hwnd HWND) (PixelRect, error)
	// SelfDpiAwareness reports the DPI awareness of the calling thread,
	// which governs what the other measurements mean. The package never
	// changes it.
	SelfDpiAwareness() (DpiAwareness, error)
}

// MeasureClient performs one measurement pass and validates its internal
// consistency: the two mapped corners must reproduce the client size, or
// the window moved between the calls and the whole pass is void (the
// caller re-measures; it must not use half a measurement).
func MeasureClient(api GeometryAPI, hwnd HWND) (GeometryMeasurement, *PlatformFault) {
	var m GeometryMeasurement
	w, h, err := api.ClientSize(hwnd)
	if err != nil {
		return m, NewFault(CodeGeometryUnavailable, "client size unavailable", err, PhaseObserve, time.Now(), 0)
	}
	if w <= 0 || h <= 0 {
		return m, NewFault(CodeGeometryUnavailable, fmt.Sprintf("client area is empty (%d×%d)", w, h), nil, PhaseObserve, time.Now(), 0)
	}
	origin, err := api.ClientPointToScreen(hwnd, 0, 0)
	if err != nil {
		return m, NewFault(CodeGeometryUnavailable, "client origin unavailable", err, PhaseObserve, time.Now(), 0)
	}
	far, err := api.ClientPointToScreen(hwnd, w, h)
	if err != nil {
		return m, NewFault(CodeGeometryUnavailable, "client far corner unavailable", err, PhaseObserve, time.Now(), 0)
	}
	if far.X-origin.X != w || far.Y-origin.Y != h {
		return m, NewFault(CodeGeometryUnavailable, fmt.Sprintf("window moved during measurement: corners %v/%v vs client size %d×%d", origin, far, w, h), nil, PhaseObserve, time.Now(), 0)
	}
	m.ClientSize = PixelPoint{w, h}
	m.ClientOrigin = origin
	m.ClientFarCorner = far
	if m.Monitor, err = api.MonitorRect(hwnd); err != nil {
		return m, NewFault(CodeGeometryUnavailable, "monitor rectangle unavailable", err, PhaseObserve, time.Now(), 0)
	}
	if m.WindowDPI, err = api.WindowDPI(hwnd); err != nil {
		return m, NewFault(CodeDpiCheckUnavailable, "window DPI unavailable", err, PhaseObserve, time.Now(), 0)
	}
	if m.MonitorDPI, err = api.MonitorDPI(hwnd); err != nil {
		return m, NewFault(CodeDpiCheckUnavailable, "monitor DPI unavailable", err, PhaseObserve, time.Now(), 0)
	}
	return m, nil
}

// VerifyDpiSetup checks the measured DPI facts for coordinate
// virtualization and returns a fault when the physical-pixel premise of
// the whole platform path does not hold.
//
// Both checks are refusals, never corrections: the package does not
// scale the image by any single DPI factor, and it does not change the
// process DPI mode after the fact — MyGo must configure per-monitor-v2
// awareness before creating windows.
func VerifyDpiSetup(self DpiAwareness, windowDPI, monitorDPI uint32) *PlatformFault {
	if self != DpiAwarenessPerMonitorV2 {
		return NewFault(CodeDpiAwarenessMismatch,
			fmt.Sprintf("scanner thread DPI awareness is %d, must be per-monitor-v2 (3) or screen coordinates are virtualized", int(self)),
			nil, PhasePrecheck, time.Now(), 0)
	}
	if windowDPI == 0 || monitorDPI == 0 {
		return NewFault(CodeDpiCheckUnavailable, "DPI fact unknown; virtualization status cannot be verified", nil, PhasePrecheck, time.Now(), 0)
	}
	if windowDPI != monitorDPI {
		return NewFault(CodeDpiVirtualized,
			fmt.Sprintf("target window DPI %d differs from monitor DPI %d: the window is DPI-virtualized and its measured client rect is not the physical region", windowDPI, monitorDPI),
			nil, PhasePrecheck, time.Now(), 0)
	}
	return nil
}

// BuildGeometry turns a consistent measurement into a ClientGeometry.
func BuildGeometry(m GeometryMeasurement, version GeometryVersion, at time.Time) ClientGeometry {
	return ClientGeometry{
		Version:    version,
		Client:     PixelRect{m.ClientOrigin, m.ClientFarCorner},
		Monitor:    m.Monitor,
		WindowDPI:  m.WindowDPI,
		MonitorDPI: m.MonitorDPI,
		ObservedAt: at,
	}
}

// GeometryChanged reports whether two geometries differ in any fact the
// input path depends on: the client rect, the monitor, or either DPI
// value. ObservedAt is deliberately not compared.
func GeometryChanged(a, b ClientGeometry) bool {
	return a.Client != b.Client || a.Monitor != b.Monitor || a.WindowDPI != b.WindowDPI || a.MonitorDPI != b.MonitorDPI
}

// GeometryTracker owns the versioned geometry of one locked target. It
// is safe for concurrent use; the input path reads the current version
// while observations refresh it.
type GeometryTracker struct {
	mu      sync.Mutex
	current ClientGeometry
	valid   bool
}

// Update folds a fresh measurement into the tracker. The first valid
// measurement becomes version 1; a changed one bumps the version (old
// coordinates are invalid from that point); an unchanged one keeps the
// version and only refreshes the observation time. It reports the
// current geometry and whether this update changed it.
func (t *GeometryTracker) Update(m GeometryMeasurement, at time.Time) (ClientGeometry, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	fresh := BuildGeometry(m, t.current.Version, at)
	if !t.valid {
		t.valid = true
		t.current = fresh
		t.current.Version = 1
		return t.current, true
	}
	if GeometryChanged(t.current, fresh) {
		fresh.Version = t.current.Version + 1
		t.current = fresh
		return t.current, true
	}
	t.current = fresh
	return t.current, false
}

// Current returns the current geometry; ok is false before the first
// valid measurement. With no valid geometry the platform must not
// produce input requests at all.
func (t *GeometryTracker) Current() (ClientGeometry, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.current, t.valid
}

// Ready reports whether a valid geometry exists.
func (t *GeometryTracker) Ready() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.valid
}

// ImagePointToScreen maps a captured-image pixel point to screen
// physical pixels under geometry g. The capture is 1:1 from the client
// rect, so the mapping is a pure translation; any scaling an OCR or UI
// layer applied to a crop must be undone by its own inverse transform
// before calling here. The point must lie inside the image, and the
// image must not exceed the client area.
func ImagePointToScreen(g ClientGeometry, imageW, imageH int, p PixelPoint) (PixelPoint, error) {
	if imageW <= 0 || imageH <= 0 {
		return PixelPoint{}, fmt.Errorf("invalid image size %d×%d", imageW, imageH)
	}
	if imageW > g.Client.Dx() || imageH > g.Client.Dy() {
		return PixelPoint{}, fmt.Errorf("image %d×%d exceeds client area %d×%d", imageW, imageH, g.Client.Dx(), g.Client.Dy())
	}
	if p.X < 0 || p.X >= imageW || p.Y < 0 || p.Y >= imageH {
		return PixelPoint{}, fmt.Errorf("point (%d,%d) outside the %d×%d image", p.X, p.Y, imageW, imageH)
	}
	return PixelPoint{g.Client.Min.X + p.X, g.Client.Min.Y + p.Y}, nil
}

// ImageRectToScreen maps a rectangle in captured-image pixels (for
// example an OCR crop whose coordinates must be clicked) to a screen
// physical pixel rectangle under geometry g. The same bounds rules as
// ImagePointToScreen apply.
func ImageRectToScreen(g ClientGeometry, imageW, imageH int, r PixelRect) (PixelRect, error) {
	if imageW <= 0 || imageH <= 0 {
		return PixelRect{}, fmt.Errorf("invalid image size %d×%d", imageW, imageH)
	}
	if imageW > g.Client.Dx() || imageH > g.Client.Dy() {
		return PixelRect{}, fmt.Errorf("image %d×%d exceeds client area %d×%d", imageW, imageH, g.Client.Dx(), g.Client.Dy())
	}
	if r.Min.X < 0 || r.Min.Y < 0 || r.Max.X > imageW || r.Max.Y > imageH || r.Empty() {
		return PixelRect{}, fmt.Errorf("rect %v outside the %d×%d image", r, imageW, imageH)
	}
	return PixelRect{
		Min: PixelPoint{g.Client.Min.X + r.Min.X, g.Client.Min.Y + r.Min.Y},
		Max: PixelPoint{g.Client.Min.X + r.Max.X, g.Client.Min.Y + r.Max.Y},
	}, nil
}
