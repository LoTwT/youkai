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

	candidates, err := DiscoverTargets(win, procs, DiscoverySpec{
		ImageName: DefaultGameImageName, ClassNameHint: DefaultGameWindowClass,
	})
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(candidates) != 0 {
		t.Fatalf("a window of another executable must not be a candidate: %+v", candidates)
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

	candidates, err := DiscoverTargets(titleGuard{win, t}, procs, DiscoverySpec{
		ImageName: DefaultGameImageName, ClassNameHint: DefaultGameWindowClass,
	})
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("want one visible candidate, got %+v", candidates)
	}
	got := candidates[0]
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

	candidates, err := DiscoverTargets(win, procs, DiscoverySpec{
		ImageName: DefaultGameImageName, ClassNameHint: DefaultGameWindowClass,
	})
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("two installations with the same image name are two candidates: %+v", candidates)
	}
	// Stable order by PID, then handle, so repeated checks list the same
	// rows.
	if candidates[0].PID != 100 || candidates[0].HWND != 3 ||
		candidates[1].PID != 200 || candidates[1].HWND != 7 {
		t.Fatalf("candidate order: %+v", candidates)
	}
	if candidates[0].ImagePath == candidates[1].ImagePath {
		t.Fatalf("the two candidates must keep their own full paths: %+v", candidates)
	}
}

func TestDiscoverTargetsClassHintNarrows(t *testing.T) {
	started := time.Date(2026, 11, 2, 8, 0, 0, 0, time.UTC)
	win := newFakeWindows()
	win.add(1, &fakeWnd{pid: 100, visible: true, class: "LauncherWnd"})
	win.add(2, &fakeWnd{pid: 100, visible: true, class: DefaultGameWindowClass})
	procs := newFakeProcesses()
	procs.byPID[100] = fakeProc{path: `C:\Games\ZZZ\ZenlessZoneZero.exe`, started: started}

	candidates, err := DiscoverTargets(win, procs, DiscoverySpec{
		ImageName: DefaultGameImageName, ClassNameHint: DefaultGameWindowClass,
	})
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(candidates) != 1 || candidates[0].HWND != 2 {
		t.Fatalf("class hint must narrow to the game window: %+v", candidates)
	}

	// A hint that matches nothing (a client update renamed the class) must
	// keep the image-name candidates rather than report "not found".
	candidates, err = DiscoverTargets(win, procs, DiscoverySpec{
		ImageName: DefaultGameImageName, ClassNameHint: "NewClass",
	})
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("a non-matching class hint must leave both candidates: %+v", candidates)
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

	candidates, err := DiscoverTargets(win, procs, DiscoverySpec{ImageName: DefaultGameImageName})
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(candidates) != 0 {
		t.Fatalf("cloaked windows and vanished processes are not candidates: %+v", candidates)
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
