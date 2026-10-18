package windows

import (
	"errors"
	"testing"
	"time"
)

func TestMeasureClientHappyPath(t *testing.T) {
	geo := newFakeGeometry()
	geo.toScreen = func(x, y int) (PixelPoint, error) {
		return PixelPoint{100 + x, 200 + y}, nil
	}
	m, fault := MeasureClient(geo, 7)
	if fault != nil {
		t.Fatalf("measure: %v", fault)
	}
	if m.ClientSize != (PixelPoint{1920, 1080}) {
		t.Fatalf("client size: %+v", m.ClientSize)
	}
	if m.ClientOrigin != (PixelPoint{100, 200}) || m.ClientFarCorner != (PixelPoint{2020, 1280}) {
		t.Fatalf("corners: %+v %+v", m.ClientOrigin, m.ClientFarCorner)
	}
	if m.WindowDPI != 96 || m.MonitorDPI != 96 {
		t.Fatalf("dpi: %d/%d", m.WindowDPI, m.MonitorDPI)
	}
}

func TestMeasureClientNegativeOrigin(t *testing.T) {
	// A display left of the primary gives negative screen coordinates;
	// the measurement must carry them through unchanged.
	geo := newFakeGeometry()
	geo.toScreen = func(x, y int) (PixelPoint, error) {
		return PixelPoint{-1920 + x, -1080 + y}, nil
	}
	m, fault := MeasureClient(geo, 7)
	if fault != nil {
		t.Fatalf("measure: %v", fault)
	}
	if m.ClientOrigin != (PixelPoint{-1920, -1080}) {
		t.Fatalf("origin: %+v", m.ClientOrigin)
	}
}

func TestMeasureClientWindowMovedMidMeasurement(t *testing.T) {
	// The two ClientToScreen calls must agree with GetClientRect; if the
	// window moved between them the whole pass is void.
	geo := newFakeGeometry()
	calls := 0
	geo.toScreen = func(x, y int) (PixelPoint, error) {
		calls++
		if calls == 1 {
			return PixelPoint{x, y}, nil
		}
		return PixelPoint{x + 1, y}, nil // moved one pixel between the calls
	}
	_, fault := MeasureClient(geo, 7)
	if fault == nil || fault.Code != CodeGeometryUnavailable {
		t.Fatalf("inconsistent measurement must be a geometry fault, got %v", fault)
	}
}

func TestMeasureClientEmptyClient(t *testing.T) {
	geo := newFakeGeometry()
	geo.clientSize = func() (int, int, error) { return 0, 1080, nil }
	_, fault := MeasureClient(geo, 7)
	if fault == nil || fault.Code != CodeGeometryUnavailable {
		t.Fatalf("empty client must be a geometry fault, got %v", fault)
	}
}

func TestMeasureClientDPIQueriesFail(t *testing.T) {
	geo := newFakeGeometry()
	geo.windowDPI = func() (uint32, error) { return 0, errors.New("GetDpiForWindow unavailable") }
	if _, fault := MeasureClient(geo, 7); fault == nil || fault.Code != CodeDpiCheckUnavailable {
		t.Fatalf("window DPI failure must be dpiCheckUnavailable, got %v", fault)
	}

	geo = newFakeGeometry()
	geo.monitorDPI = func() (uint32, error) { return 0, errors.New("GetDpiForMonitor unavailable") }
	if _, fault := MeasureClient(geo, 7); fault == nil || fault.Code != CodeDpiCheckUnavailable {
		t.Fatalf("monitor DPI failure must be dpiCheckUnavailable, got %v", fault)
	}
}

func TestMeasureClientMonitorRectFails(t *testing.T) {
	geo := newFakeGeometry()
	geo.monitorRect = func() (PixelRect, error) { return PixelRect{}, errors.New("GetMonitorInfoW failed") }
	_, fault := MeasureClient(geo, 7)
	if fault == nil || fault.Code != CodeGeometryUnavailable {
		t.Fatalf("monitor rect failure must be geometryUnavailable, got %v", fault)
	}
}

func TestVerifyDpiSetup(t *testing.T) {
	cases := []struct {
		name       string
		self       DpiAwareness
		windowDPI  uint32
		monitorDPI uint32
		wantCode   string
	}{
		{"pmv2 equal 96", DpiAwarenessPerMonitorV2, 96, 96, ""},
		{"pmv2 equal 144", DpiAwarenessPerMonitorV2, 144, 144, ""},
		{"unaware window on 144 monitor", DpiAwarenessPerMonitorV2, 96, 144, CodeDpiVirtualized},
		{"144 window on 96 monitor", DpiAwarenessPerMonitorV2, 144, 96, CodeDpiVirtualized},
		{"self not pmv2", DpiAwarenessSystem, 96, 96, CodeDpiAwarenessMismatch},
		{"self per-monitor v1", DpiAwarenessPerMonitor, 96, 96, CodeDpiAwarenessMismatch},
		{"window dpi unknown", DpiAwarenessPerMonitorV2, 0, 96, CodeDpiCheckUnavailable},
		{"monitor dpi unknown", DpiAwarenessPerMonitorV2, 96, 0, CodeDpiCheckUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fault := VerifyDpiSetup(tc.self, tc.windowDPI, tc.monitorDPI)
			if tc.wantCode == "" {
				if fault != nil {
					t.Fatalf("want clean, got %v", fault)
				}
				return
			}
			if fault == nil || fault.Code != tc.wantCode {
				t.Fatalf("want fault %s, got %v", tc.wantCode, fault)
			}
		})
	}
}

func TestGeometryTrackerVersions(t *testing.T) {
	tr := &GeometryTracker{}
	if _, ok := tr.Current(); ok {
		t.Fatal("tracker must start without a geometry")
	}
	if tr.Ready() {
		t.Fatal("tracker must not be ready before the first measurement")
	}

	at := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	base := GeometryMeasurement{
		ClientSize:      PixelPoint{1920, 1080},
		ClientOrigin:    PixelPoint{0, 0},
		ClientFarCorner: PixelPoint{1920, 1080},
		Monitor:         PixelRect{PixelPoint{0, 0}, PixelPoint{1920, 1080}},
		WindowDPI:       96,
		MonitorDPI:      96,
	}

	g1, changed := tr.Update(base, at)
	if g1.Version != 1 || !changed {
		t.Fatalf("first update: version %d, changed %v", g1.Version, changed)
	}

	// Same facts, later time: same version, refreshed observation.
	g2, changed := tr.Update(base, at.Add(time.Second))
	if g2.Version != 1 || changed {
		t.Fatalf("unchanged update: version %d, changed %v", g2.Version, changed)
	}
	if !g2.ObservedAt.After(g1.ObservedAt) {
		t.Fatalf("observation time should refresh: %v", g2.ObservedAt)
	}

	// Window moved: version bump.
	moved := base
	moved.ClientOrigin = PixelPoint{100, 100}
	moved.ClientFarCorner = PixelPoint{2020, 1180}
	g3, changed := tr.Update(moved, at.Add(2*time.Second))
	if g3.Version != 2 || !changed {
		t.Fatalf("moved update: version %d, changed %v", g3.Version, changed)
	}

	// DPI change: version bump (old coordinates die with it).
	dpiChange := base
	dpiChange.WindowDPI = 144
	dpiChange.MonitorDPI = 144
	g4, changed := tr.Update(dpiChange, at.Add(3*time.Second))
	if g4.Version != 3 || !changed {
		t.Fatalf("dpi update: version %d, changed %v", g4.Version, changed)
	}

	// Monitor change: version bump.
	monitorChange := base
	monitorChange.Monitor = PixelRect{PixelPoint{-1920, 0}, PixelPoint{0, 1080}}
	g5, changed := tr.Update(monitorChange, at.Add(4*time.Second))
	if g5.Version != 4 || !changed {
		t.Fatalf("monitor update: version %d, changed %v", g5.Version, changed)
	}
}

func TestImagePointToScreen(t *testing.T) {
	geo := ClientGeometry{
		Version: 1,
		Client:  PixelRect{PixelPoint{-1920, -1080}, PixelPoint{0, 0}}, // secondary display left of/above primary
	}
	got, err := ImagePointToScreen(geo, 1920, 1080, PixelPoint{0, 0})
	if err != nil || got != (PixelPoint{-1920, -1080}) {
		t.Fatalf("image origin should map to client origin: %v, %v", got, err)
	}
	got, err = ImagePointToScreen(geo, 1920, 1080, PixelPoint{1919, 1079})
	if err != nil || got != (PixelPoint{-1, -1}) {
		t.Fatalf("last image pixel should map to last client pixel: %v, %v", got, err)
	}

	if _, err := ImagePointToScreen(geo, 1920, 1080, PixelPoint{1920, 0}); err == nil {
		t.Fatal("point outside the image must be refused")
	}
	if _, err := ImagePointToScreen(geo, 1920, 1080, PixelPoint{-1, 0}); err == nil {
		t.Fatal("negative point must be refused")
	}
	// The image may be smaller than the client (a later integration may
	// capture a sub-region), but never larger.
	if _, err := ImagePointToScreen(geo, 2000, 1080, PixelPoint{1999, 0}); err == nil {
		t.Fatal("image larger than the client must be refused")
	}
	if _, err := ImagePointToScreen(ClientGeometry{Version: 1, Client: PixelRect{PixelPoint{0, 0}, PixelPoint{100, 100}}}, 50, 50, PixelPoint{0, 0}); err != nil {
		t.Fatalf("smaller image must map fine: %v", err)
	}
}

func TestImageRectToScreenCropInverse(t *testing.T) {
	geo := ClientGeometry{
		Version: 1,
		Client:  PixelRect{PixelPoint{100, 200}, PixelPoint{2020, 1280}},
	}
	// An OCR crop in image pixels maps back to the screen rectangle for
	// the input that targets it.
	got, err := ImageRectToScreen(geo, 1920, 1080, PixelRect{PixelPoint{10, 20}, PixelPoint{110, 220}})
	if err != nil {
		t.Fatalf("crop mapping: %v", err)
	}
	want := PixelRect{PixelPoint{110, 220}, PixelPoint{210, 420}}
	if got != want {
		t.Fatalf("crop mapping: got %v, want %v", got, want)
	}
	if _, err := ImageRectToScreen(geo, 1920, 1080, PixelRect{PixelPoint{10, 20}, PixelPoint{1921, 220}}); err == nil {
		t.Fatal("crop beyond the image must be refused")
	}
	if _, err := ImageRectToScreen(geo, 1920, 1080, PixelRect{PixelPoint{10, 20}, PixelPoint{10, 220}}); err == nil {
		t.Fatal("empty crop must be refused")
	}
}
