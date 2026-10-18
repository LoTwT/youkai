package macos_test

import (
	"errors"
	"testing"
	"time"

	macos "github.com/LoTwT/youkai/internal/platform/macos"
)

// Input readiness tests (T2 §2.2 前台 row, §3.1, §5): a frontmost PID
// match alone never unlocks input, visibility is verified against the
// display bounds, an AX query failure or timeout fails closed, and the
// requested position must lie inside both the window and the displays.

func readyAssessment() macos.TargetAssessment {
	obs := &macos.TargetObservation{
		Selector: macos.WindowSelector{BundleID: "com.example.game", PID: 200, WindowID: 42},
		Frame:    macos.PointRect{X: 100, Y: 50, W: 1280, H: 720},
		OnScreen: true,
		Active:   true,
	}
	return macos.TargetAssessment{Status: macos.TargetUnique, Observation: obs, ObservedAt: time.Unix(0, 0)}
}

func mainDisplay() []macos.PointRect {
	return []macos.PointRect{{X: 0, Y: 0, W: 1920, H: 1080}}
}

func readyForeground() macos.ForegroundState {
	return macos.ForegroundState{FrontmostPID: 200, ObservedAt: time.Unix(0, 0)}
}

func windowCenter() macos.Point {
	return macos.Point{X: 740, Y: 410}
}

func TestReadinessAllChecksPass(t *testing.T) {
	pos := windowCenter()
	v := macos.AssessInputReadiness(readyAssessment(), readyForeground(), nil, mainDisplay(), &pos, macos.ReadinessOptions{}, time.Unix(0, 0))
	if !v.Ready {
		t.Fatalf("verdict not ready: %+v", v.Denials)
	}
}

func TestReadinessFailsWithoutTarget(t *testing.T) {
	pos := windowCenter()
	cases := map[macos.TargetStatus]macos.ReadinessDenialCode{
		macos.TargetMissing:   macos.DenialTargetMissing,
		macos.TargetReplaced:  macos.DenialTargetReplaced,
		macos.TargetAmbiguous: macos.DenialTargetAmbiguous,
	}
	for status, code := range cases {
		a := macos.TargetAssessment{Status: status, ObservedAt: time.Unix(0, 0)}
		v := macos.AssessInputReadiness(a, readyForeground(), nil, mainDisplay(), &pos, macos.ReadinessOptions{}, time.Unix(0, 0))
		if v.Ready {
			t.Fatalf("status %s ready", status)
		}
		if v.Denials[0].Code != code {
			t.Fatalf("status %s denial %s", status, v.Denials[0].Code)
		}
	}
}

func TestReadinessDeniesOffScreenWindow(t *testing.T) {
	a := readyAssessment()
	a.Observation.OnScreen = false
	pos := windowCenter()
	v := macos.AssessInputReadiness(a, readyForeground(), nil, mainDisplay(), &pos, macos.ReadinessOptions{}, time.Unix(0, 0))
	if v.Ready {
		t.Fatal("off-screen window ready")
	}
	if v.Denials[0].Code != macos.DenialWindowOffScreen {
		t.Fatalf("denial %s", v.Denials[0].Code)
	}
}

func TestReadinessDeniesUnknownDisplays(t *testing.T) {
	pos := windowCenter()
	v := macos.AssessInputReadiness(readyAssessment(), readyForeground(), nil, nil, &pos, macos.ReadinessOptions{}, time.Unix(0, 0))
	if v.Ready {
		t.Fatal("no display bounds ready")
	}
	if v.Denials[0].Code != macos.DenialDisplayBoundsUnknown {
		t.Fatalf("denial %s", v.Denials[0].Code)
	}
}

func TestReadinessDeniesPartiallyVisibleWindow(t *testing.T) {
	// The window is mostly off the only display: below the default
	// fraction, input is refused.
	a := readyAssessment()
	a.Observation.Frame = macos.PointRect{X: 1728, Y: 50, W: 1280, H: 720}
	pos := macos.Point{X: 1900, Y: 400}
	v := macos.AssessInputReadiness(a, readyForeground(), nil, mainDisplay(), &pos, macos.ReadinessOptions{}, time.Unix(0, 0))
	if v.Ready {
		t.Fatal("mostly off-display window ready")
	}
	found := false
	for _, d := range v.Denials {
		if d.Code == macos.DenialWindowNotVisibleEnough {
			found = true
		}
	}
	if !found {
		t.Fatalf("denials %+v", v.Denials)
	}
	// About 15% of the window is visible (192 of 1280 points); raising
	// the caller's tolerance above that accepts it — the fraction is a
	// caller policy, not a verdict constant.
	v = macos.AssessInputReadiness(a, readyForeground(), nil, mainDisplay(), &pos, macos.ReadinessOptions{MinVisibleFraction: 0.1}, time.Unix(0, 0))
	if !v.Ready {
		t.Fatalf("relaxed fraction still denied: %+v", v.Denials)
	}
}

func TestReadinessDeniesFrontmostMismatchAndQueryFailure(t *testing.T) {
	pos := windowCenter()
	fg := macos.ForegroundState{FrontmostPID: 300, ObservedAt: time.Unix(0, 0)}
	v := macos.AssessInputReadiness(readyAssessment(), fg, nil, mainDisplay(), &pos, macos.ReadinessOptions{}, time.Unix(0, 0))
	if v.Ready || v.Denials[0].Code != macos.DenialNotFrontmost {
		t.Fatalf("frontmost mismatch verdict %+v", v.Denials)
	}
	// A failed frontmost query denies input: unknown is not allowed.
	fg = macos.ForegroundState{Err: errors.New("workspace unavailable"), ObservedAt: time.Unix(0, 0)}
	v = macos.AssessInputReadiness(readyAssessment(), fg, nil, mainDisplay(), &pos, macos.ReadinessOptions{}, time.Unix(0, 0))
	if v.Ready || v.Denials[0].Code != macos.DenialFrontmostQueryFailed {
		t.Fatalf("frontmost query failure verdict %+v", v.Denials)
	}
}

func TestReadinessAXReportFailClosed(t *testing.T) {
	pos := windowCenter()
	// A timed-out AX query fails closed even with everything else
	// green.
	ax := &macos.AXFocusReport{Err: errors.New("ax message timeout"), TimedOut: true, ObservedAt: time.Unix(0, 0)}
	v := macos.AssessInputReadiness(readyAssessment(), readyForeground(), ax, mainDisplay(), &pos, macos.ReadinessOptions{}, time.Unix(0, 0))
	if v.Ready {
		t.Fatal("timed-out AX query ready")
	}
	if v.Denials[0].Code != macos.DenialAXQueryFailed {
		t.Fatalf("denial %s", v.Denials[0].Code)
	}
	// A successful AX report for another process denies.
	ax = &macos.AXFocusReport{PID: 300, Frame: macos.PointRect{X: 100, Y: 50, W: 1280, H: 720}, Role: "AXWindow", ObservedAt: time.Unix(0, 0)}
	v = macos.AssessInputReadiness(readyAssessment(), readyForeground(), ax, mainDisplay(), &pos, macos.ReadinessOptions{}, time.Unix(0, 0))
	if v.Ready {
		t.Fatal("AX focus on another process ready")
	}
	if v.Denials[0].Code != macos.DenialAXFocusOtherProcess {
		t.Fatalf("denial %s", v.Denials[0].Code)
	}
	// A frame disagreement beyond the tolerance denies.
	ax = &macos.AXFocusReport{PID: 200, Frame: macos.PointRect{X: 100, Y: 50, W: 1000, H: 600}, Role: "AXWindow", ObservedAt: time.Unix(0, 0)}
	v = macos.AssessInputReadiness(readyAssessment(), readyForeground(), ax, mainDisplay(), &pos, macos.ReadinessOptions{}, time.Unix(0, 0))
	if v.Ready {
		t.Fatal("AX frame mismatch ready")
	}
	if v.Denials[0].Code != macos.DenialAXFrameMismatch {
		t.Fatalf("denial %s", v.Denials[0].Code)
	}
	// A corroborating report keeps the verdict ready.
	ax = &macos.AXFocusReport{PID: 200, Frame: macos.PointRect{X: 100.5, Y: 50, W: 1280, H: 720}, Role: "AXWindow", ObservedAt: time.Unix(0, 0)}
	v = macos.AssessInputReadiness(readyAssessment(), readyForeground(), ax, mainDisplay(), &pos, macos.ReadinessOptions{}, time.Unix(0, 0))
	if !v.Ready {
		t.Fatalf("corroborating AX denied: %+v", v.Denials)
	}
}

func TestReadinessPositionChecks(t *testing.T) {
	// Outside the window frame: denied for the window, even though the
	// position is on a display.
	outside := macos.Point{X: 50, Y: 400}
	v := macos.AssessInputReadiness(readyAssessment(), readyForeground(), nil, mainDisplay(), &outside, macos.ReadinessOptions{}, time.Unix(0, 0))
	if v.Ready {
		t.Fatal("position outside window ready")
	}
	if v.Denials[0].Code != macos.DenialPositionOutsideTarget {
		t.Fatalf("denial %s", v.Denials[0].Code)
	}
	// Inside the window but off every display: denied for the display.
	// A window reaching into a second, unknown display area provides
	// this combination.
	a := readyAssessment()
	a.Observation.Frame = macos.PointRect{X: 1700, Y: 0, W: 1280, H: 720}
	offDisplay := macos.Point{X: 2500, Y: 100}
	v = macos.AssessInputReadiness(a, readyForeground(), nil, mainDisplay(), &offDisplay, macos.ReadinessOptions{MinVisibleFraction: 0.1}, time.Unix(0, 0))
	if v.Ready {
		t.Fatal("position off displays ready")
	}
	found := false
	for _, d := range v.Denials {
		if d.Code == macos.DenialPositionOffScreen {
			found = true
		}
	}
	if !found {
		t.Fatalf("denials %+v", v.Denials)
	}
	// A key press carries no position and skips the position checks.
	v = macos.AssessInputReadiness(readyAssessment(), readyForeground(), nil, mainDisplay(), nil, macos.ReadinessOptions{}, time.Unix(0, 0))
	if !v.Ready {
		t.Fatalf("positionless operation denied: %+v", v.Denials)
	}
}

func TestReadinessIgnoresIsActive(t *testing.T) {
	// isActive is true but the window is off screen and another app is
	// frontmost (the Stage Manager case the SDK documents): the verdict
	// must come from the real checks, never from the flag.
	a := readyAssessment()
	a.Observation.OnScreen = false
	pos := windowCenter()
	fg := macos.ForegroundState{FrontmostPID: 300, ObservedAt: time.Unix(0, 0)}
	v := macos.AssessInputReadiness(a, fg, nil, mainDisplay(), &pos, macos.ReadinessOptions{}, time.Unix(0, 0))
	if v.Ready {
		t.Fatal("isActive-style flags unlocked input")
	}
	if len(v.Denials) < 2 {
		t.Fatalf("expected multiple denials, got %+v", v.Denials)
	}
}
