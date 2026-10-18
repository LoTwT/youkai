package macos_test

import (
	"testing"
	"time"

	macos "github.com/LoTwT/youkai/internal/platform/macos"
)

// Window identity tests (T2 §2.2): identity is bundle + process
// instance + window id, titles never matter, and the assessment
// distinguishes a unique lock from missing, replaced and ambiguous
// targets — the last two must refuse input rather than guess.

func testWindows() []macos.NativeWindow {
	return []macos.NativeWindow{
		{WindowID: 11, PID: 100, BundleID: "com.example.game", Title: "启动器", Frame: macos.PointRect{X: 0, Y: 0, W: 400, H: 300}},
		{WindowID: 42, PID: 200, BundleID: "com.example.game", Title: "云·绝区零", Frame: macos.PointRect{X: 100, Y: 50, W: 1280, H: 720}, OnScreen: true},
		{WindowID: 43, PID: 200, BundleID: "com.example.game", Title: "登录弹窗", Frame: macos.PointRect{X: 300, Y: 200, W: 300, H: 200}, OnScreen: true},
		{WindowID: 77, PID: 300, BundleID: "com.example.finder", Title: "Finder", Frame: macos.PointRect{X: 0, Y: 0, W: 800, H: 600}, OnScreen: true},
	}
}

func selectorFor(w macos.NativeWindow) macos.WindowSelector {
	return macos.WindowSelector{BundleID: w.BundleID, PID: w.PID, WindowID: w.WindowID}
}

func TestSelectorMatchesOnlyTheExactWindow(t *testing.T) {
	windows := testWindows()
	sel := selectorFor(windows[1])
	if !sel.Matches(windows[1]) {
		t.Fatal("selector does not match its own window")
	}
	// Same bundle and pid, different window id.
	if sel.Matches(windows[2]) {
		t.Fatal("selector matched a sibling window")
	}
	// Same window id under another process.
	other := windows[1]
	other.PID = 999
	if sel.Matches(other) {
		t.Fatal("selector matched a window id reuse by another process")
	}
	// Same window id, different bundle.
	other = windows[1]
	other.BundleID = "com.example.other"
	if sel.Matches(other) {
		t.Fatal("selector matched a window id reuse with another bundle")
	}
	if !sel.SameProcess(windows[2]) {
		t.Fatal("sibling window is not the same process")
	}
	if sel.SameProcess(windows[3]) {
		t.Fatal("unrelated window counted as same process")
	}
}

func TestAssessTargetUnique(t *testing.T) {
	// The launcher process owns exactly one window: locking it is
	// unique.
	sel := selectorFor(testWindows()[0])
	a := macos.AssessTarget(sel, testWindows(), time.Unix(0, 0))
	if a.Status != macos.TargetUnique {
		t.Fatalf("status %s", a.Status)
	}
	if a.Observation == nil {
		t.Fatal("unique assessment carries no observation")
	}
	if a.Observation.Selector != sel {
		t.Fatalf("observation selector %+v", a.Observation.Selector)
	}
	if a.Observation.Title != "启动器" {
		t.Fatalf("observation title %q", a.Observation.Title)
	}
	if a.Observation.Frame.W != 400 {
		t.Fatalf("observation frame %+v", a.Observation.Frame)
	}
}

func TestAssessTargetAmbiguousWithSiblingWindow(t *testing.T) {
	// The game process owns the game window and a login popup: locking
	// the game window is ambiguous and must refuse.
	sel := selectorFor(testWindows()[1])
	a := macos.AssessTarget(sel, testWindows(), time.Unix(0, 0))
	if a.Status != macos.TargetAmbiguous {
		t.Fatalf("status %s", a.Status)
	}
	if a.Observation == nil {
		t.Fatal("ambiguous assessment should still carry the observation")
	}
	if len(a.Candidates) != 2 {
		t.Fatalf("candidates %+v", a.Candidates)
	}
}

func TestAssessTargetReplacedByNewWindow(t *testing.T) {
	// The selected window id is gone but the process still owns other
	// windows: replaced, never re-bound silently.
	windows := []macos.NativeWindow{
		testWindows()[0], // pid 100 launcher
		testWindows()[2], // pid 200 login popup
		testWindows()[3], // pid 300 finder
		{WindowID: 99, PID: 200, BundleID: "com.example.game", Title: "新窗口"},
	}
	sel := selectorFor(testWindows()[1])
	a := macos.AssessTarget(sel, windows, time.Unix(0, 0))
	if a.Status != macos.TargetReplaced {
		t.Fatalf("status %s", a.Status)
	}
	if a.Observation != nil {
		t.Fatal("replaced assessment must not carry an observation")
	}
	if len(a.Candidates) != 2 {
		t.Fatalf("candidates %+v", a.Candidates)
	}
}

func TestAssessTargetMissingWhenProcessIsGone(t *testing.T) {
	sel := macos.WindowSelector{BundleID: "com.example.gone", PID: 1234, WindowID: 5}
	a := macos.AssessTarget(sel, testWindows(), time.Unix(0, 0))
	if a.Status != macos.TargetMissing {
		t.Fatalf("status %s", a.Status)
	}
	if a.Observation != nil || len(a.Candidates) != 0 {
		t.Fatalf("missing assessment carries data: %+v", a)
	}
}

func TestSelectProcessWindowsFiltersByProcessInstance(t *testing.T) {
	windows := testWindows()
	sel := selectorFor(windows[1])
	got := macos.SelectProcessWindows(sel, windows)
	if len(got) != 2 {
		t.Fatalf("process windows %+v", got)
	}
	for _, w := range got {
		if w.PID != sel.PID || w.BundleID != sel.BundleID {
			t.Fatalf("window %+v does not belong to the process", w)
		}
	}
}
