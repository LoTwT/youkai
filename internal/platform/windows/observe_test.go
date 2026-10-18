package windows

import (
	"testing"
	"time"
)

// observeWorld wires a coherent fake target for the observation tests.
type observeWorld struct {
	win     *fakeWindows
	procs   *fakeProcesses
	geo     *fakeGeometry
	fg      *fakeForeground
	tracker *GeometryTracker
	locked  WindowIdentity
}

func newObserveWorld(t *testing.T) *observeWorld {
	t.Helper()
	started := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	w := &observeWorld{
		win:     newFakeWindows(),
		procs:   newFakeProcesses(),
		geo:     newFakeGeometry(),
		fg:      &fakeForeground{},
		tracker: &GeometryTracker{},
	}
	w.win.add(7, &fakeWnd{pid: 42, visible: true})
	w.procs.byPID[42] = fakeProc{path: `C:\game.exe`, started: started}
	w.locked = WindowIdentity{HWND: 7, PID: 42, ImagePath: `C:\game.exe`, ProcessStartedAt: started}
	w.fg.foreground = 7
	return w
}

func (w *observeWorld) observe(at time.Time) TargetObservation {
	return ObserveTarget(w.win, w.procs, w.geo, w.fg, w.locked, w.tracker, at)
}

func TestObserveTargetHealthy(t *testing.T) {
	w := newObserveWorld(t)
	at := time.Date(2026, 10, 18, 12, 0, 1, 0, time.UTC)

	obs := w.observe(at)
	if obs.Fault != nil {
		t.Fatalf("healthy observation must not fault: %v", obs.Fault)
	}
	if !obs.Matched || obs.Mismatch != nil {
		t.Fatalf("identity should verify: %+v", obs)
	}
	if !obs.Foreground || !obs.Visible || obs.Minimized {
		t.Fatalf("state flags: %+v", obs)
	}
	if obs.Occlusion != OcclusionNotChecked {
		t.Fatalf("passive observation does not check occlusion: %s", obs.Occlusion)
	}
	// The first observation establishes version 1 — a change from the
	// "no geometry" state, so GeometryChanged is true.
	if obs.Geometry.Version != 1 || !obs.GeometryChanged {
		t.Fatalf("first observation seeds version 1: %+v", obs.Geometry)
	}

	// An unchanged second observation keeps the version.
	obs = w.observe(at.Add(time.Second))
	if obs.Geometry.Version != 1 || obs.GeometryChanged {
		t.Fatalf("unchanged observation must keep version 1: %+v", obs.Geometry)
	}

	// A window move bumps the version: coordinates from version 1 are
	// invalid from here on.
	w.geo.toScreen = func(x, y int) (PixelPoint, error) {
		return PixelPoint{500 + x, 500 + y}, nil
	}
	obs = w.observe(at.Add(2 * time.Second))
	if obs.Fault != nil {
		t.Fatalf("a move is a version bump, not a fault: %v", obs.Fault)
	}
	if obs.Geometry.Version != 2 || !obs.GeometryChanged {
		t.Fatalf("the move should bump to version 2: %+v", obs.Geometry)
	}
	if obs.Geometry.Client.Min != (PixelPoint{500, 500}) {
		t.Fatalf("new client origin: %+v", obs.Geometry.Client)
	}
}

func TestObserveTargetWindowGone(t *testing.T) {
	w := newObserveWorld(t)
	w.observe(time.Now()) // seed version 1
	w.win.byHandle[7].gone = true

	obs := w.observe(time.Now())
	if obs.Matched {
		t.Fatal("a destroyed window cannot match")
	}
	if obs.Mismatch == nil || obs.Mismatch.Reason != MismatchWindowGone {
		t.Fatalf("want windowGone, got %+v", obs.Mismatch)
	}
	if obs.Fault == nil || obs.Fault.Code != CodeTargetLost || obs.Fault.LastGeometryVersion != 1 {
		t.Fatalf("want targetLost carrying the last geometry version, got %v", obs.Fault)
	}
}

func TestObserveTargetHandleReusedByAnotherProcess(t *testing.T) {
	w := newObserveWorld(t)
	w.observe(time.Now())
	// The handle now belongs to another process instance.
	w.win.byHandle[7].pid = 43
	w.procs.byPID[43] = fakeProc{
		path:    `C:\game.exe`,
		started: w.locked.ProcessStartedAt.Add(time.Hour),
	}

	obs := w.observe(time.Now())
	if obs.Matched || obs.Mismatch == nil || obs.Mismatch.Reason != MismatchWindowReplaced {
		t.Fatalf("want windowReplaced, got %+v", obs.Mismatch)
	}
	if obs.Fault == nil || obs.Fault.Code != CodeTargetLost {
		t.Fatalf("replaced handle is target lost, got %v", obs.Fault)
	}
}

func TestObserveTargetProcessRestartedReusingPID(t *testing.T) {
	w := newObserveWorld(t)
	w.observe(time.Now())
	w.procs.byPID[42] = fakeProc{
		path:    `C:\game.exe`,
		started: w.locked.ProcessStartedAt.Add(2 * time.Hour), // same PID, new instance
	}

	obs := w.observe(time.Now())
	if obs.Matched || obs.Mismatch == nil || obs.Mismatch.Reason != MismatchProcessRestarted {
		t.Fatalf("want processRestarted, got %+v", obs.Mismatch)
	}
	if obs.Fault == nil || obs.Fault.Code != CodeTargetLost {
		t.Fatalf("restarted process is target lost, got %v", obs.Fault)
	}
}

func TestObserveTargetProcessUnresolvable(t *testing.T) {
	w := newObserveWorld(t)
	w.observe(time.Now())
	w.procs.byPID[42] = fakeProc{err: errProcessGone}

	obs := w.observe(time.Now())
	if obs.Matched {
		t.Fatal("an unresolvable owner cannot be verified")
	}
	if obs.Fault == nil || obs.Fault.Code != CodeTargetLost {
		t.Fatalf("want targetLost, got %v", obs.Fault)
	}
}

func TestObserveTargetDpiVirtualizedKeepsLastGeometry(t *testing.T) {
	w := newObserveWorld(t)
	w.observe(time.Now()) // version 1

	// The target reports 96 (DPI-unaware) on a 144 monitor: its client
	// rect is virtualized, the coordinates cannot be trusted.
	w.geo.windowDPI = func() (uint32, error) { return 96, nil }
	w.geo.monitorDPI = func() (uint32, error) { return 144, nil }

	obs := w.observe(time.Now())
	if obs.Fault == nil || obs.Fault.Code != CodeDpiVirtualized {
		t.Fatalf("want dpiVirtualized, got %v", obs.Fault)
	}
	// The fault keeps the last valid geometry and the tracker keeps
	// version 1: nothing was scaled to "fix" the mismatch.
	if obs.Geometry.Version != 1 {
		t.Fatalf("last valid geometry should survive the fault: %+v", obs.Geometry)
	}
	if geo, ok := w.tracker.Current(); !ok || geo.Version != 1 {
		t.Fatalf("tracker must not fold a virtualized measurement, got %+v ok=%v", geo, ok)
	}
}

func TestObserveTargetSelfAwarenessMismatch(t *testing.T) {
	w := newObserveWorld(t)
	w.geo.self = func() (DpiAwareness, error) { return DpiAwarenessSystem, nil }

	obs := w.observe(time.Now())
	if obs.Fault == nil || obs.Fault.Code != CodeDpiAwarenessMismatch {
		t.Fatalf("want dpiAwarenessMismatch, got %v", obs.Fault)
	}
	if obs.Geometry.Version != 0 {
		t.Fatalf("no geometry should be seeded on a DPI mismatch: %+v", obs.Geometry)
	}
}

func TestObserveTargetSelfAwarenessUnverifiable(t *testing.T) {
	w := newObserveWorld(t)
	w.geo.self = func() (DpiAwareness, error) { return 0, errDpiApiMissing }

	obs := w.observe(time.Now())
	if obs.Fault == nil || obs.Fault.Code != CodeDpiCheckUnavailable {
		t.Fatalf("want dpiCheckUnavailable, got %v", obs.Fault)
	}
}

func TestObserveTargetMeasurementFailsKeepsLastGeometry(t *testing.T) {
	w := newObserveWorld(t)
	w.observe(time.Now()) // version 1

	calls := 0
	w.geo.toScreen = func(x, y int) (PixelPoint, error) {
		calls++
		if calls >= 2 {
			return PixelPoint{}, errMeasureBroken
		}
		return PixelPoint{x, y}, nil
	}
	obs := w.observe(time.Now())
	if obs.Fault == nil || obs.Fault.Code != CodeGeometryUnavailable {
		t.Fatalf("want geometryUnavailable, got %v", obs.Fault)
	}
	if obs.Geometry.Version != 1 {
		t.Fatalf("last valid geometry must survive: %+v", obs.Geometry)
	}
}

func TestObserveTargetFocusLossIsObservationNotFault(t *testing.T) {
	w := newObserveWorld(t)
	w.observe(time.Now())
	// Something else (or the scanner itself) is foreground.
	w.fg.foreground = 555

	obs := w.observe(time.Now())
	if obs.Fault != nil {
		t.Fatalf("focus loss is an observation, not a fault: %v", obs.Fault)
	}
	if obs.Foreground {
		t.Fatal("foreground should be false")
	}
	if !obs.Matched {
		t.Fatal("the target itself is still fine")
	}
	if obs.GeometryChanged {
		t.Fatal("focus loss must not touch the geometry version")
	}
}
