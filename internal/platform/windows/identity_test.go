package windows

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSelectTargetExactExecutable(t *testing.T) {
	started := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	probes := []WindowProbe{
		{HWND: 11, PID: 100, ImagePath: `C:\Games\Other\launcher.exe`, ProcessStartedAt: started, Visible: true},
		{HWND: 12, PID: 200, ImagePath: `C:\Games\ZZZ\ZenlessZoneZero.exe`, ProcessStartedAt: started, Visible: true},
	}
	got, err := SelectTarget(probes, TargetSpec{ExecutablePath: `C:\Games\ZZZ\ZenlessZoneZero.exe`, RequireVisible: true})
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if got.HWND != 12 || got.PID != 200 {
		t.Fatalf("selected wrong window: %+v", got)
	}
}

func TestSelectTargetPathCaseInsensitive(t *testing.T) {
	probes := []WindowProbe{
		{HWND: 3, PID: 7, ImagePath: `c:\games\zzz\zenlesszonezero.exe`, Visible: true},
	}
	got, err := SelectTarget(probes, TargetSpec{ExecutablePath: `C:\GAMES\ZZZ\ZENLESSZONEZERO.EXE`})
	if err != nil || got.HWND != 3 {
		t.Fatalf("case-insensitive path match failed: %v, %+v", err, got)
	}
}

func TestSelectTargetNoWindow(t *testing.T) {
	probes := []WindowProbe{{HWND: 1, PID: 2, ImagePath: `C:\other.exe`, Visible: true}}
	_, err := SelectTarget(probes, TargetSpec{ExecutablePath: `C:\game.exe`})
	var selErr *SelectionError
	if !errors.As(err, &selErr) || selErr.Kind != SelectionNoWindow {
		t.Fatalf("want noWindow selection error, got %v", err)
	}
}

func TestSelectTargetHintsNarrow(t *testing.T) {
	started := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	probes := []WindowProbe{
		// Same process: a launcher-style top-level window and the game
		// window.
		{HWND: 20, PID: 300, ImagePath: `C:\game.exe`, ProcessStartedAt: started, Visible: true, ClassName: "LaunchWnd", Title: "Launcher"},
		{HWND: 21, PID: 300, ImagePath: `C:\game.exe`, ProcessStartedAt: started, Visible: true, ClassName: "GameWnd", Title: "ZZZ"},
	}
	got, err := SelectTarget(probes, TargetSpec{ExecutablePath: `C:\game.exe`, ClassNameHint: "GameWnd"})
	if err != nil {
		t.Fatalf("select with class hint: %v", err)
	}
	if got.HWND != 21 {
		t.Fatalf("class hint picked the wrong window: %+v", got)
	}
	// A hint that matches nothing leaves the candidates alone (it must
	// not turn a matchable set into nothing).
	_, err = SelectTarget(probes, TargetSpec{ExecutablePath: `C:\game.exe`, ClassNameHint: "NoSuchClass"})
	var selErr *SelectionError
	if !errors.As(err, &selErr) || selErr.Kind != SelectionAmbiguous {
		t.Fatalf("non-matching hint should leave ambiguity, got %v", err)
	}
}

func TestSelectTargetTitleHintNarrows(t *testing.T) {
	probes := []WindowProbe{
		{HWND: 30, PID: 1, ImagePath: `C:\game.exe`, Visible: true, ClassName: "GameWnd", Title: "ZZZ Login"},
		{HWND: 31, PID: 1, ImagePath: `C:\game.exe`, Visible: true, ClassName: "GameWnd", Title: "Zenless Zone Zero"},
	}
	got, err := SelectTarget(probes, TargetSpec{ExecutablePath: `C:\game.exe`, ClassNameHint: "GameWnd", TitleHint: "Zenless"})
	if err != nil || got.HWND != 31 {
		t.Fatalf("title hint should narrow to 31: %v, %+v", err, got)
	}
}

func TestSelectTargetAmbiguousRefuses(t *testing.T) {
	probes := []WindowProbe{
		{HWND: 40, PID: 1, ImagePath: `C:\game.exe`, Visible: true},
		{HWND: 41, PID: 1, ImagePath: `C:\game.exe`, Visible: true},
	}
	_, err := SelectTarget(probes, TargetSpec{ExecutablePath: `C:\game.exe`})
	var selErr *SelectionError
	if !errors.As(err, &selErr) || selErr.Kind != SelectionAmbiguous || len(selErr.Candidates) != 2 {
		t.Fatalf("want ambiguous error with both candidates, got %v", err)
	}
}

func TestSelectTargetPrefersUniqueVisible(t *testing.T) {
	probes := []WindowProbe{
		{HWND: 50, PID: 1, ImagePath: `C:\game.exe`, Visible: false}, // hidden helper window
		{HWND: 51, PID: 1, ImagePath: `C:\game.exe`, Visible: true},
	}
	got, err := SelectTarget(probes, TargetSpec{ExecutablePath: `C:\game.exe`, RequireVisible: true})
	if err != nil || got.HWND != 51 {
		t.Fatalf("unique visible window should win: %v, %+v", err, got)
	}
}

func TestSelectTargetNotReadyHiddenAndCloaked(t *testing.T) {
	hidden := []WindowProbe{{HWND: 60, PID: 1, ImagePath: `C:\game.exe`, Visible: false}}
	_, err := SelectTarget(hidden, TargetSpec{ExecutablePath: `C:\game.exe`, RequireVisible: true})
	var selErr *SelectionError
	if !errors.As(err, &selErr) || selErr.Kind != SelectionNotReady {
		t.Fatalf("hidden single candidate should be notReady, got %v", err)
	}
	if !strings.Contains(err.Error(), "hidden") {
		t.Fatalf("notReady error should say hidden: %v", err)
	}

	cloaked := []WindowProbe{{HWND: 61, PID: 1, ImagePath: `C:\game.exe`, Visible: true, Cloaked: true}}
	_, err = SelectTarget(cloaked, TargetSpec{ExecutablePath: `C:\game.exe`, RequireVisible: true})
	if !errors.As(err, &selErr) || selErr.Kind != SelectionNotReady {
		t.Fatalf("cloaked single candidate should be notReady, got %v", err)
	}
}

func TestSelectTargetMinimizedStillSelected(t *testing.T) {
	// A minimized window is a valid lock: minimization is an observed
	// state, and the input precheck refuses while it lasts — selection
	// must not silently drop the target.
	probes := []WindowProbe{{HWND: 70, PID: 1, ImagePath: `C:\game.exe`, Visible: true, Minimized: true}}
	got, err := SelectTarget(probes, TargetSpec{ExecutablePath: `C:\game.exe`, RequireVisible: true})
	if err != nil || got.HWND != 70 || !got.Minimized {
		t.Fatalf("minimized window should still be selected: %v, %+v", err, got)
	}
}

func TestVerifyLockedIgnoresClassAndTitle(t *testing.T) {
	locked := WindowIdentity{
		HWND: 1, PID: 2, ImagePath: `C:\game.exe`,
		ProcessStartedAt: time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC),
		ClassName:        "GameWnd", Title: "Old Title",
	}
	sameWindowNewTitles := WindowProbe{
		HWND: 1, PID: 2, ImagePath: `c:\GAME.exe`,
		ProcessStartedAt: locked.ProcessStartedAt,
		ClassName:        "SomethingElse", Title: "Totally Different",
	}
	if err := VerifyLocked(locked, sameWindowNewTitles); err != nil {
		t.Fatalf("hints are not identity; changed class/title must still match: %v", err)
	}
}

func TestVerifyLockedMismatches(t *testing.T) {
	started := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	restarted := started.Add(time.Hour)
	locked := WindowIdentity{HWND: 9, PID: 5, ImagePath: `C:\game.exe`, ProcessStartedAt: started}

	cases := []struct {
		name     string
		observed WindowProbe
		reason   MismatchReason
	}{
		{
			name:     "different handle",
			observed: WindowProbe{HWND: 10, PID: 5, ImagePath: `C:\game.exe`, ProcessStartedAt: started},
			reason:   MismatchWindowReplaced,
		},
		{
			name:     "handle reused by another process",
			observed: WindowProbe{HWND: 9, PID: 6, ImagePath: `C:\game.exe`, ProcessStartedAt: started},
			reason:   MismatchWindowReplaced,
		},
		{
			name:     "process restarted reusing the pid",
			observed: WindowProbe{HWND: 9, PID: 5, ImagePath: `C:\game.exe`, ProcessStartedAt: restarted},
			reason:   MismatchProcessRestarted,
		},
		{
			name:     "different executable",
			observed: WindowProbe{HWND: 9, PID: 5, ImagePath: `C:\other.exe`, ProcessStartedAt: started},
			reason:   MismatchExecutableChanged,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := VerifyLocked(locked, tc.observed)
			var mismatch *MismatchError
			if !errors.As(err, &mismatch) || mismatch.Reason != tc.reason {
				t.Fatalf("want mismatch %s, got %v", tc.reason, err)
			}
		})
	}
}

func TestCollectProbes(t *testing.T) {
	started := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	win := newFakeWindows()
	win.add(1, &fakeWnd{pid: 100, visible: true, iconic: false, class: "A", title: "One"})
	win.add(2, &fakeWnd{pid: 0})                                       // no owner: skipped
	win.add(3, &fakeWnd{pid: 300, cloaked: true})                      // cloaked: still collected, flagged
	win.add(4, &fakeWnd{pid: 400, pidErr: errors.New("query failed")}) // pid query fails: skipped
	win.add(5, &fakeWnd{pid: 500, visible: false, iconic: true, class: "B", title: "Minimized"})

	procs := newFakeProcesses()
	procs.byPID[100] = fakeProc{path: `C:\a.exe`, started: started}
	procs.byPID[300] = fakeProc{path: `C:\c.exe`, started: started}
	procs.byPID[400] = fakeProc{path: `C:\d.exe`, started: started}
	procs.byPID[500] = fakeProc{path: `C:\e.exe`, started: started}

	probes, err := CollectProbes(win, procs)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	// Handles 2 (no pid) and 4 (pid query error) are skipped; 1, 3, 5
	// are collected with their facts.
	if len(probes) != 3 {
		t.Fatalf("want 3 probes, got %d: %+v", len(probes), probes)
	}
	var byHandle = map[HWND]WindowProbe{}
	for _, p := range probes {
		byHandle[p.HWND] = p
	}
	if p := byHandle[3]; !p.Cloaked {
		t.Fatalf("handle 3 should carry the cloaked flag: %+v", p)
	}
	if p := byHandle[5]; !p.Minimized || p.Visible {
		t.Fatalf("handle 5 should be minimized and not visible: %+v", p)
	}
	if p := byHandle[1]; p.ImagePath != `C:\a.exe` || !p.ProcessStartedAt.Equal(started) {
		t.Fatalf("handle 1 should carry the process identity: %+v", p)
	}
}

func TestCollectProbesEnumerationErrorFails(t *testing.T) {
	win := newFakeWindows()
	win.enumErr = errors.New("enum failed")
	if _, err := CollectProbes(win, newFakeProcesses()); err == nil {
		t.Fatal("enumeration error must fail the collection")
	}
}
