package platform

import (
	"errors"
	"image"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/platform/windows"
)

// The read-only preparation check reuses the stage-1 decisions and the
// PR-05 candidate enumeration, but must never open a session. Every test
// below also guards the absence of the session's side effects.

func readinessChecks(t *testing.T, report ReadinessReport) map[ReadinessItem]ReadinessCheck {
	t.Helper()
	checks := make(map[ReadinessItem]ReadinessCheck, len(report.Checks))
	for _, check := range report.Checks {
		checks[check.Item] = check
	}
	return checks
}

func assertReadOnly(t *testing.T, w *winWorld) {
	t.Helper()
	if w.hotkey.registered() {
		t.Fatal("the readiness check registered the stop hotkey")
	}
	if calls := w.send.batches(); len(calls) != 0 {
		t.Fatalf("the readiness check dispatched %d input batches", len(calls))
	}
	if w.gdi.captures != 0 {
		t.Fatalf("the readiness check captured %d frames", w.gdi.captures)
	}
	if requests := w.fg.requested(); len(requests) != 0 {
		t.Fatalf("the readiness check requested the foreground %d times", len(requests))
	}
	w.win.mu.Lock()
	titles := w.win.titleReads
	w.win.mu.Unlock()
	if titles != 0 {
		t.Fatalf("the readiness check read %d window titles", titles)
	}
}

func TestWinReadinessSatisfiedAndReadOnly(t *testing.T) {
	w := newWinWorld(t)
	report, err := w.adapter.CheckReadiness(ReadinessRequest{Display: testDisplayPolicy()})
	if err != nil {
		t.Fatalf("check readiness: %v", err)
	}
	if len(report.Candidates) != 1 || report.Candidates[0].Handle != uintptr(gameHwnd) {
		t.Fatalf("candidates = %+v, want the game window", report.Candidates)
	}
	checks := readinessChecks(t, report)
	for _, item := range []ReadinessItem{
		ReadinessTargetClient,
		ReadinessAdministrator,
		ReadinessGameResolution,
		ReadinessWindowPosition,
		ReadinessDisplayScale,
	} {
		check, ok := checks[item]
		if !ok {
			t.Fatalf("missing readiness item %s in %+v", item, report.Checks)
		}
		if check.Verdict != ReadinessSatisfied {
			t.Fatalf("%s = %s (%+v), want satisfied", item, check.Verdict, check)
		}
		if check.At.IsZero() {
			t.Fatalf("%s has no check time", item)
		}
	}
	if checks[ReadinessGameResolution].Current != "1920x1080" {
		t.Fatalf("resolution current = %q", checks[ReadinessGameResolution].Current)
	}
	assertReadOnly(t, w)
}

func TestWinReadinessTargetNotFound(t *testing.T) {
	w := newWinWorld(t)
	w.win.mu.Lock()
	w.win.wnds = nil
	w.win.mu.Unlock()
	report, err := w.adapter.CheckReadiness(ReadinessRequest{Display: testDisplayPolicy()})
	if err != nil {
		t.Fatalf("check readiness: %v", err)
	}
	checks := readinessChecks(t, report)
	target := checks[ReadinessTargetClient]
	if target.Verdict != ReadinessUnsatisfied || target.Fault == nil || target.Fault.Code != CodeTargetNotFound {
		t.Fatalf("target = %+v, want unsatisfied targetNotFound", target)
	}
	if checks[ReadinessAdministrator].Verdict != ReadinessUnknown {
		t.Fatalf("administrator without a target = %s, want unknown", checks[ReadinessAdministrator].Verdict)
	}
	for _, item := range []ReadinessItem{ReadinessGameResolution, ReadinessWindowPosition, ReadinessDisplayScale} {
		if got := checks[item].Verdict; got != ReadinessUnknown {
			t.Fatalf("%s without a target = %s, want unknown", item, got)
		}
	}
	assertReadOnly(t, w)
}

func TestWinReadinessTargetAmbiguousNeedsAChoice(t *testing.T) {
	w := newWinWorld(t)
	w.win.mu.Lock()
	w.win.wnds = append(w.win.wnds, &fakeWnd{
		hwnd: 8, pid: 77, visible: true, className: "UnityWndClass",
	})
	w.win.mu.Unlock()
	w.procs.mu.Lock()
	w.procs.byPID[77] = fakeProcInfo{path: `D:\Other\ZenlessZoneZero.exe`, started: time.Now()}
	w.procs.mu.Unlock()
	report, err := w.adapter.CheckReadiness(ReadinessRequest{Display: testDisplayPolicy()})
	if err != nil {
		t.Fatalf("check readiness: %v", err)
	}
	if len(report.Candidates) != 2 {
		t.Fatalf("candidates = %+v, want two", report.Candidates)
	}
	target := readinessChecks(t, report)[ReadinessTargetClient]
	if target.Verdict != ReadinessUnsatisfied || target.Fault == nil || target.Fault.Code != CodeTargetAmbiguous {
		t.Fatalf("target = %+v, want unsatisfied targetAmbiguous", target)
	}
	if got := readinessChecks(t, report)[ReadinessGameResolution].Verdict; got != ReadinessUnknown {
		t.Fatalf("resolution with an ambiguous target = %s, want unknown", got)
	}
	assertReadOnly(t, w)
}

func TestWinReadinessConfirmedTargetUsesThatWindow(t *testing.T) {
	w := newWinWorld(t)
	w.win.mu.Lock()
	w.win.wnds = append(w.win.wnds, &fakeWnd{
		hwnd: 8, pid: 77, visible: true, className: "UnityWndClass",
	})
	w.win.mu.Unlock()
	w.procs.mu.Lock()
	w.procs.byPID[77] = fakeProcInfo{path: `D:\Other\ZenlessZoneZero.exe`, started: time.Now()}
	w.procs.mu.Unlock()
	report, err := w.adapter.CheckReadiness(ReadinessRequest{
		Target: TargetSpec{
			HandleHint:           uintptr(gameHwnd),
			PIDHint:              uint32(gamePID),
			ProcessStartedAtHint: w.started,
		},
		Display: testDisplayPolicy(),
	})
	if err != nil {
		t.Fatalf("check readiness: %v", err)
	}
	target := readinessChecks(t, report)[ReadinessTargetClient]
	if target.Verdict != ReadinessSatisfied {
		t.Fatalf("confirmed target = %+v, want satisfied", target)
	}
	if got := readinessChecks(t, report)[ReadinessGameResolution]; got.Verdict != ReadinessSatisfied {
		t.Fatalf("resolution for the confirmed window = %+v", got)
	}
	assertReadOnly(t, w)
}

func TestWinReadinessAccessDeniedLeavesTargetUnknownAndAdminUnsatisfied(t *testing.T) {
	w := newWinWorld(t)
	w.integrity.self = windows.IntegrityMedium
	w.procs.mu.Lock()
	w.procs.byPID[gamePID] = fakeProcInfo{
		path: gameExe, started: w.started,
		imageName: windows.DefaultGameImageName,
		err:       windows.ErrProcessAccessDenied,
	}
	w.procs.mu.Unlock()
	report, err := w.adapter.CheckReadiness(ReadinessRequest{Display: testDisplayPolicy()})
	if err != nil {
		t.Fatalf("check readiness: %v", err)
	}
	if len(report.Candidates) != 0 || report.AccessDeniedWindows != 1 {
		t.Fatalf("discovery = candidates %+v denied %d", report.Candidates, report.AccessDeniedWindows)
	}
	checks := readinessChecks(t, report)
	target := checks[ReadinessTargetClient]
	if target.Verdict != ReadinessUnknown {
		t.Fatalf("target = %+v, want unknown while the window cannot be opened", target)
	}
	admin := checks[ReadinessAdministrator]
	if admin.Verdict != ReadinessUnsatisfied || admin.Fault == nil || admin.Fault.Code != CodeIntegrityMismatch {
		t.Fatalf("administrator = %+v, want unsatisfied integrityMismatch", admin)
	}
	if admin.Fault.Params["target"] != "unreadable" || admin.Fault.Params["self"] != "medium" {
		t.Fatalf("administrator params = %+v", admin.Fault.Params)
	}
	assertReadOnly(t, w)
}

func TestWinReadinessAdministrator(t *testing.T) {
	t.Run("game runs elevated", func(t *testing.T) {
		w := newWinWorld(t)
		w.integrity.target = windows.IntegrityHigh
		w.integrity.self = windows.IntegrityMedium
		report, err := w.adapter.CheckReadiness(ReadinessRequest{Display: testDisplayPolicy()})
		if err != nil {
			t.Fatalf("check readiness: %v", err)
		}
		admin := readinessChecks(t, report)[ReadinessAdministrator]
		if admin.Verdict != ReadinessUnsatisfied || admin.Fault == nil || admin.Fault.Code != CodeIntegrityMismatch {
			t.Fatalf("administrator = %+v, want unsatisfied integrityMismatch", admin)
		}
		if admin.Fault.Params["target"] != "high" || admin.Fault.Params["self"] != "medium" {
			t.Fatalf("integrity params = %+v", admin.Fault.Params)
		}
	})
	t.Run("levels unverifiable", func(t *testing.T) {
		w := newWinWorld(t)
		w.integrity.targetErr = errors.New("query denied")
		report, err := w.adapter.CheckReadiness(ReadinessRequest{Display: testDisplayPolicy()})
		if err != nil {
			t.Fatalf("check readiness: %v", err)
		}
		admin := readinessChecks(t, report)[ReadinessAdministrator]
		if admin.Verdict != ReadinessUnknown || admin.Fault == nil || admin.Fault.Code != CodeIntegrityUnverifiable {
			t.Fatalf("administrator = %+v, want unknown integrityUnverifiable", admin)
		}
	})
}

func TestWinReadinessResolutionAndPosition(t *testing.T) {
	t.Run("known but not opened tier", func(t *testing.T) {
		w := newWinWorld(t)
		w.geo.mu.Lock()
		w.geo.clientSize = windows.PixelPoint{X: 2560, Y: 1440}
		w.geo.mu.Unlock()
		policy := testDisplayPolicy()
		policy.KnownTiers = append(policy.KnownTiers, ClientTier{Name: "2560x1440", Client: image.Pt(2560, 1440)})
		report, err := w.adapter.CheckReadiness(ReadinessRequest{Display: policy})
		if err != nil {
			t.Fatalf("check readiness: %v", err)
		}
		resolution := readinessChecks(t, report)[ReadinessGameResolution]
		if resolution.Verdict != ReadinessUnsatisfied || resolution.Fault == nil || resolution.Fault.Code != CodeClientTierNotOpened {
			t.Fatalf("resolution = %+v, want unsatisfied clientTierNotOpened", resolution)
		}
		if resolution.Current != "2560x1440" || resolution.Required != "1920x1080" {
			t.Fatalf("resolution values = %q -> %q", resolution.Current, resolution.Required)
		}
	})
	t.Run("unknown tier", func(t *testing.T) {
		w := newWinWorld(t)
		w.geo.mu.Lock()
		w.geo.clientSize = windows.PixelPoint{X: 1280, Y: 1024}
		w.geo.mu.Unlock()
		report, err := w.adapter.CheckReadiness(ReadinessRequest{Display: testDisplayPolicy()})
		if err != nil {
			t.Fatalf("check readiness: %v", err)
		}
		resolution := readinessChecks(t, report)[ReadinessGameResolution]
		if resolution.Verdict != ReadinessUnsatisfied || resolution.Fault == nil || resolution.Fault.Code != CodeClientTierUnsupported {
			t.Fatalf("resolution = %+v, want unsatisfied clientTierUnsupported", resolution)
		}
	})
	t.Run("window outside the work area", func(t *testing.T) {
		w := newWinWorld(t)
		w.geo.mu.Lock()
		w.geo.monitor = windows.PixelRect{Min: windows.PixelPoint{X: 0, Y: 0}, Max: windows.PixelPoint{X: 1920, Y: 1200}}
		w.geo.workArea = windows.PixelRect{Min: windows.PixelPoint{X: 0, Y: 0}, Max: windows.PixelPoint{X: 1920, Y: 1040}}
		w.geo.mu.Unlock()
		report, err := w.adapter.CheckReadiness(ReadinessRequest{Display: testDisplayPolicy()})
		if err != nil {
			t.Fatalf("check readiness: %v", err)
		}
		position := readinessChecks(t, report)[ReadinessWindowPosition]
		if position.Verdict != ReadinessUnsatisfied || position.Fault == nil || position.Fault.Code != CodeWindowOutsideWorkArea {
			t.Fatalf("position = %+v, want unsatisfied windowOutsideWorkArea", position)
		}
		if got := readinessChecks(t, report)[ReadinessGameResolution].Verdict; got != ReadinessSatisfied {
			t.Fatalf("resolution = %s, want satisfied independent of the position", got)
		}
	})
}

func TestWinReadinessDisplayScale(t *testing.T) {
	t.Run("virtualized target", func(t *testing.T) {
		w := newWinWorld(t)
		w.geo.mu.Lock()
		w.geo.windowDPI = 96
		w.geo.monitorDPI = 144
		w.geo.mu.Unlock()
		report, err := w.adapter.CheckReadiness(ReadinessRequest{Display: testDisplayPolicy()})
		if err != nil {
			t.Fatalf("check readiness: %v", err)
		}
		dpi := readinessChecks(t, report)[ReadinessDisplayScale]
		if dpi.Verdict != ReadinessUnsatisfied || dpi.Fault == nil || dpi.Fault.Code != CodeDpiVirtualized {
			t.Fatalf("display scale = %+v, want unsatisfied dpiVirtualized", dpi)
		}
	})
	t.Run("youkai is not PMv2", func(t *testing.T) {
		w := newWinWorld(t)
		w.geo.mu.Lock()
		w.geo.self = windows.DpiAwarenessSystem
		w.geo.mu.Unlock()
		report, err := w.adapter.CheckReadiness(ReadinessRequest{Display: testDisplayPolicy()})
		if err != nil {
			t.Fatalf("check readiness: %v", err)
		}
		dpi := readinessChecks(t, report)[ReadinessDisplayScale]
		if dpi.Verdict != ReadinessUnsatisfied || dpi.Fault == nil || dpi.Fault.Code != CodeDpiAwarenessMismatch {
			t.Fatalf("display scale = %+v, want unsatisfied dpiAwarenessMismatch", dpi)
		}
	})
	t.Run("DPI fact unreadable", func(t *testing.T) {
		w := newWinWorld(t)
		w.geo.mu.Lock()
		w.geo.windowDPI = 0
		w.geo.mu.Unlock()
		report, err := w.adapter.CheckReadiness(ReadinessRequest{Display: testDisplayPolicy()})
		if err != nil {
			t.Fatalf("check readiness: %v", err)
		}
		dpi := readinessChecks(t, report)[ReadinessDisplayScale]
		if dpi.Verdict != ReadinessUnknown || dpi.Fault == nil || dpi.Fault.Code != CodeDpiCheckUnavailable {
			t.Fatalf("display scale = %+v, want unknown dpiCheckUnavailable", dpi)
		}
	})
}

// F1: the start-time preflight and the read-only checklist name the same
// cause with the same code when youkai itself is known not PMv2.
func TestWinDpiAwarenessCodeMatchesReadiness(t *testing.T) {
	w := newWinWorld(t)
	w.geo.mu.Lock()
	w.geo.self = windows.DpiAwarenessSystem
	w.geo.mu.Unlock()

	start := w.adapter.Preflight().Fault()
	if start == nil || start.Code != CodeDpiAwarenessMismatch {
		t.Fatalf("preflight = %+v, want dpiAwarenessMismatch", start)
	}
	report, err := w.adapter.CheckReadiness(ReadinessRequest{Display: testDisplayPolicy()})
	if err != nil {
		t.Fatalf("check readiness: %v", err)
	}
	checklist := readinessChecks(t, report)[ReadinessDisplayScale]
	if checklist.Fault == nil || checklist.Fault.Code != start.Code {
		t.Fatalf("checklist display scale = %+v, want code %q", checklist, start.Code)
	}
}

func TestWinReadinessFixedTarget(t *testing.T) {
	w := newWinWorld(t)
	cfg := w.config()
	report, err := w.adapter.CheckReadiness(ReadinessRequest{
		Target:  cfg.Target,
		Display: cfg.Display,
	})
	if err != nil {
		t.Fatalf("check readiness: %v", err)
	}
	checks := readinessChecks(t, report)
	for _, item := range []ReadinessItem{
		ReadinessTargetClient,
		ReadinessAdministrator,
		ReadinessGameResolution,
		ReadinessWindowPosition,
		ReadinessDisplayScale,
	} {
		if got := checks[item].Verdict; got != ReadinessSatisfied {
			t.Fatalf("%s = %s, want satisfied", item, got)
		}
	}
	assertReadOnly(t, w)
}

func TestWinReadinessWithoutDisplayPolicyLeavesResolutionUnknown(t *testing.T) {
	w := newWinWorld(t)
	report, err := w.adapter.CheckReadiness(ReadinessRequest{})
	if err != nil {
		t.Fatalf("check readiness: %v", err)
	}
	resolution := readinessChecks(t, report)[ReadinessGameResolution]
	if resolution.Verdict != ReadinessUnknown {
		t.Fatalf("resolution without a policy = %+v, want unknown", resolution)
	}
	if resolution.Current != "1920x1080" {
		t.Fatalf("resolution current = %q, want the measured size", resolution.Current)
	}
}

// A failed measurement leaves the geometry rows unknown, never
// unsatisfied: the checklist cannot judge a window it could not measure,
// and unsatisfied rows would disable the start for a fact nobody
// established (#140).
func TestWinReadinessMeasurementFailureLeavesGeometryRowsUnknown(t *testing.T) {
	w := newWinWorld(t)
	w.geo.mu.Lock()
	w.geo.clientSize = windows.PixelPoint{}
	w.geo.mu.Unlock()

	report, err := w.adapter.CheckReadiness(ReadinessRequest{Display: testDisplayPolicy()})
	if err != nil {
		t.Fatalf("check readiness: %v", err)
	}
	checks := readinessChecks(t, report)
	for _, item := range []ReadinessItem{
		ReadinessGameResolution,
		ReadinessWindowPosition,
		ReadinessDisplayScale,
	} {
		check := checks[item]
		if check.Verdict != ReadinessUnknown {
			t.Fatalf("%s after a failed measurement = %s (%+v), want unknown",
				item, check.Verdict, check)
		}
		if check.Fault == nil {
			t.Fatalf("%s after a failed measurement carries no fault", item)
		}
	}
	if got := checks[ReadinessGameResolution].Fault.Code; got != CodeGeometryInvalid {
		t.Fatalf("resolution fault = %s, want %s", got, CodeGeometryInvalid)
	}
	if got := checks[ReadinessDisplayScale].Fault.Code; got != CodeDpiCheckUnavailable {
		t.Fatalf("display scale fault = %s, want %s", got, CodeDpiCheckUnavailable)
	}
}
