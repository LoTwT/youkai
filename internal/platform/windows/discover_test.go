package windows

import (
	"errors"
	"testing"
	"time"
)

// titleGuard fails the test when discovery reads a window title.
type titleGuard struct {
	*fakeWindows
	t *testing.T
}

func (g titleGuard) Title(hwnd HWND) (string, error) {
	g.t.Errorf("discovery read the title of hwnd %d", hwnd)
	return "", nil
}

func TestDiscoverTargetsEmpty(t *testing.T) {
	procs := newFakeProcesses()
	procs.byPID[10] = fakeProc{path: `C:\Games\Other\other.exe`, started: time.Now()}
	win := newFakeWindows()
	win.add(1, &fakeWnd{pid: 10, visible: true, class: "OtherWnd"})

	result, err := DiscoverTargets(win, procs, DiscoverySpec{
		ImageName: DefaultGameImageName, ClassNameHint: DefaultGameWindowClass,
	})
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(result.Candidates) != 0 || result.AccessDeniedWindows != 0 {
		t.Fatalf("a window of another executable must not be a candidate: %+v", result)
	}
}

func TestDiscoverTargetsSingleVisibleGameWindow(t *testing.T) {
	started := time.Date(2026, 11, 2, 8, 0, 0, 0, time.UTC)
	win := newFakeWindows()
	win.add(1, &fakeWnd{pid: 100, visible: true, class: DefaultGameWindowClass, title: "绝区零"})
	// The field run found the game process also owns hidden IME helper
	// windows; they cannot be the window the user sees.
	win.add(2, &fakeWnd{pid: 100, visible: false, class: "MSCTFIME UI", title: "Default IME"})
	procs := newFakeProcesses()
	procs.byPID[100] = fakeProc{path: `C:\Games\ZZZ\ZenlessZoneZero.exe`, started: started}

	result, err := DiscoverTargets(titleGuard{win, t}, procs, DiscoverySpec{
		ImageName: DefaultGameImageName, ClassNameHint: DefaultGameWindowClass,
	})
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(result.Candidates) != 1 {
		t.Fatalf("want one visible candidate, got %+v", result)
	}
	got := result.Candidates[0]
	if got.HWND != 1 || got.PID != 100 || got.ImagePath != `C:\Games\ZZZ\ZenlessZoneZero.exe` ||
		!got.ProcessStartedAt.Equal(started) || got.ClassName != DefaultGameWindowClass {
		t.Fatalf("candidate facts: %+v", got)
	}
}

func TestDiscoverTargetsSameImageDifferentPaths(t *testing.T) {
	started := time.Date(2026, 11, 2, 8, 0, 0, 0, time.UTC)
	win := newFakeWindows()
	win.add(7, &fakeWnd{pid: 200, visible: true, class: DefaultGameWindowClass})
	win.add(3, &fakeWnd{pid: 100, visible: true, class: DefaultGameWindowClass})
	procs := newFakeProcesses()
	procs.byPID[100] = fakeProc{path: `C:\Games\ZZZ\ZenlessZoneZero.exe`, started: started}
	procs.byPID[200] = fakeProc{path: `D:\Other\ZenlessZoneZero.exe`, started: started}

	result, err := DiscoverTargets(win, procs, DiscoverySpec{
		ImageName: DefaultGameImageName, ClassNameHint: DefaultGameWindowClass,
	})
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(result.Candidates) != 2 {
		t.Fatalf("two installations with the same image name are two candidates: %+v", result)
	}
	// Stable order by PID, then handle, so repeated checks list the same
	// rows.
	if result.Candidates[0].PID != 100 || result.Candidates[0].HWND != 3 ||
		result.Candidates[1].PID != 200 || result.Candidates[1].HWND != 7 {
		t.Fatalf("candidate order: %+v", result.Candidates)
	}
	if result.Candidates[0].ImagePath == result.Candidates[1].ImagePath {
		t.Fatalf("the two candidates must keep their own full paths: %+v", result.Candidates)
	}
}

func TestDiscoverTargetsClassHintNarrows(t *testing.T) {
	started := time.Date(2026, 11, 2, 8, 0, 0, 0, time.UTC)
	win := newFakeWindows()
	win.add(1, &fakeWnd{pid: 100, visible: true, class: "LauncherWnd"})
	win.add(2, &fakeWnd{pid: 100, visible: true, class: DefaultGameWindowClass})
	procs := newFakeProcesses()
	procs.byPID[100] = fakeProc{path: `C:\Games\ZZZ\ZenlessZoneZero.exe`, started: started}

	result, err := DiscoverTargets(win, procs, DiscoverySpec{
		ImageName: DefaultGameImageName, ClassNameHint: DefaultGameWindowClass,
	})
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(result.Candidates) != 1 || result.Candidates[0].HWND != 2 {
		t.Fatalf("class hint must narrow to the game window: %+v", result)
	}

	// A hint that matches nothing (a client update renamed the class) must
	// keep the image-name candidates rather than report "not found".
	result, err = DiscoverTargets(win, procs, DiscoverySpec{
		ImageName: DefaultGameImageName, ClassNameHint: "NewClass",
	})
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(result.Candidates) != 2 {
		t.Fatalf("a non-matching class hint must leave both candidates: %+v", result)
	}
}

func TestDiscoverTargetsSkipsCloakedAndVanishedProcesses(t *testing.T) {
	started := time.Date(2026, 11, 2, 8, 0, 0, 0, time.UTC)
	win := newFakeWindows()
	win.add(1, &fakeWnd{pid: 100, visible: true, cloaked: true, class: DefaultGameWindowClass})
	win.add(2, &fakeWnd{pid: 200, visible: true, class: DefaultGameWindowClass})
	procs := newFakeProcesses()
	procs.byPID[100] = fakeProc{path: `C:\Games\ZZZ\ZenlessZoneZero.exe`, started: started}
	procs.byPID[200] = fakeProc{err: errors.New("process gone")}

	result, err := DiscoverTargets(win, procs, DiscoverySpec{ImageName: DefaultGameImageName})
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(result.Candidates) != 0 || result.AccessDeniedWindows != 0 {
		t.Fatalf("cloaked windows and vanished processes are not candidates: %+v", result)
	}
}

// M1: a visible game-class window whose process cannot be opened is the
// shape of "the game runs elevated, youkai does not". It is counted, not a
// candidate, and it must not be confused with a vanished process or a
// window of another class.
func TestDiscoverTargetsCountsAccessDeniedGameWindows(t *testing.T) {
	win := newFakeWindows()
	win.add(1, &fakeWnd{pid: 100, visible: true, class: DefaultGameWindowClass})
	win.add(2, &fakeWnd{pid: 200, visible: true, class: "OtherClass"})
	win.add(3, &fakeWnd{pid: 300, visible: true, class: DefaultGameWindowClass})
	procs := newFakeProcesses()
	procs.byPID[100] = fakeProc{err: ErrProcessAccessDenied}
	procs.byPID[200] = fakeProc{err: ErrProcessAccessDenied}
	procs.byPID[300] = fakeProc{err: errors.New("process gone")}

	result, err := DiscoverTargets(win, procs, DiscoverySpec{
		ImageName: DefaultGameImageName, ClassNameHint: DefaultGameWindowClass,
	})
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(result.Candidates) != 0 {
		t.Fatalf("an unreadable process cannot be a candidate: %+v", result)
	}
	if result.AccessDeniedWindows != 1 {
		t.Fatalf("access-denied windows = %d, want only the game-class one: %+v", result.AccessDeniedWindows, result)
	}
}

func TestDiscoverTargetsEnumerationError(t *testing.T) {
	win := newFakeWindows()
	win.enumErr = errors.New("enum failed")
	if _, err := DiscoverTargets(win, newFakeProcesses(), DiscoverySpec{ImageName: DefaultGameImageName}); err == nil {
		t.Fatal("enumeration error must fail discovery")
	}
}

func TestDiscoverTargetsEmptyImageName(t *testing.T) {
	if _, err := DiscoverTargets(newFakeWindows(), newFakeProcesses(), DiscoverySpec{}); err == nil {
		t.Fatal("an empty image name is a wiring error")
	}
}
