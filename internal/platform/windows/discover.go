package windows

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// The Windows local client's window identity, as recorded by the
// 2026-11-02 Windows field acceptance (docs/reports/
// 2026-11-02-windows-local-field-acceptance.md): the process image name
// is the stable fact across install paths, and the game's top-level
// window class screens out unrelated helper windows of the same process.
//
// The class name is a screening hint, not identity: a client update could
// rename the class, and the image name alone still lists the windows for
// the user to confirm rather than reporting "not found".
const (
	DefaultGameImageName   = "ZenlessZoneZero.exe"
	DefaultGameWindowClass = "UnityWndClass"
)

// ErrProcessAccessDenied marks a process query the caller was not allowed
// to complete (OpenProcess returned ERROR_ACCESS_DENIED). Discovery uses
// it to tell "the game runs elevated and youkai does not" apart from "the
// process vanished"; the real Win32 layer wraps the OS error with it.
var ErrProcessAccessDenied = errors.New("the process cannot be opened without permission")

// DiscoverySpec selects the candidate windows of one game client by the
// owning process's image file name (not its full path: installations live
// in user-chosen directories).
type DiscoverySpec struct {
	// ImageName is the executable's file name, compared case-insensitively
	// like Windows compares paths.
	ImageName string
	// ClassNameHint narrows several remaining candidates to this exact
	// window class. A hint that matches nothing leaves the candidates
	// alone: it must not turn a matchable set into "not found". It is also
	// the only signal available for a window whose process cannot be
	// opened, so the access-denied count is limited to this class.
	ClassNameHint string
}

// TargetCandidate is one visible top-level window of the game client that
// the user can confirm as the scan target. The title is deliberately not
// part of it: discovery never reads another process's window caption.
type TargetCandidate struct {
	HWND             HWND
	PID              ProcessID
	ImagePath        string
	ProcessStartedAt time.Time
	ClassName        string
	// Minimized is a candidate fact only: a minimized game window is still
	// the user's target, and the precheck reports targetHidden when a
	// session opens on it.
	Minimized bool
}

// DiscoveryResult is one enumeration pass. AccessDeniedWindows counts
// visible windows of the game's class whose owning process could not be
// opened for lack of permission. They cannot become candidates because
// their executable is unknown, but the count lets the caller suggest
// running youkai with the game's privileges (D-1) instead of claiming the
// game is not running.
type DiscoveryResult struct {
	Candidates          []TargetCandidate
	AccessDeniedWindows int
}

// DiscoverTargets enumerates the visible top-level windows whose process
// image name is spec.ImageName. Windows of that executable that are
// hidden or DWM-cloaked are skipped — they cannot be the window the user
// sees — and windows whose process vanished during the walk are skipped
// rather than failing the whole enumeration. An enumeration error fails
// the call. The result is ordered by PID and handle so the selection list
// is stable between checks.
func DiscoverTargets(win WindowAPI, procs ProcessAPI, spec DiscoverySpec) (DiscoveryResult, error) {
	if strings.TrimSpace(spec.ImageName) == "" {
		return DiscoveryResult{}, fmt.Errorf("discover game windows: the process image name is empty")
	}
	var handles []HWND
	if err := win.EnumTopLevel(func(h HWND) { handles = append(handles, h) }); err != nil {
		return DiscoveryResult{}, fmt.Errorf("enumerate top-level windows: %w", err)
	}
	result := DiscoveryResult{Candidates: make([]TargetCandidate, 0, len(handles))}
	for _, h := range handles {
		if !win.IsWindowVisible(h) {
			continue
		}
		if cloaked, err := win.Cloaked(h); err == nil && cloaked {
			continue
		}
		pid, err := win.ThreadProcessID(h)
		if err != nil || pid == 0 {
			continue
		}
		path, started, err := procs.Query(pid)
		if err != nil {
			// A window with the game's class whose process we may not open
			// is the shape of "the game runs elevated, youkai does not".
			// Without the image path it cannot be a candidate.
			if errors.Is(err, ErrProcessAccessDenied) && spec.ClassNameHint != "" {
				if className, classErr := win.ClassName(h); classErr == nil && className == spec.ClassNameHint {
					result.AccessDeniedWindows++
				}
			}
			continue
		}
		if !strings.EqualFold(imageBaseName(path), spec.ImageName) {
			continue
		}
		className, _ := win.ClassName(h)
		result.Candidates = append(result.Candidates, TargetCandidate{
			HWND:             h,
			PID:              pid,
			ImagePath:        path,
			ProcessStartedAt: started,
			ClassName:        className,
			Minimized:        win.IsIconic(h),
		})
	}
	if len(result.Candidates) > 1 && spec.ClassNameHint != "" {
		narrowed := make([]TargetCandidate, 0, len(result.Candidates))
		for _, c := range result.Candidates {
			if c.ClassName == spec.ClassNameHint {
				narrowed = append(narrowed, c)
			}
		}
		if len(narrowed) > 0 {
			result.Candidates = narrowed
		}
	}
	sort.Slice(result.Candidates, func(i, j int) bool {
		if result.Candidates[i].PID != result.Candidates[j].PID {
			return result.Candidates[i].PID < result.Candidates[j].PID
		}
		return result.Candidates[i].HWND < result.Candidates[j].HWND
	})
	return result, nil
}

// imageBaseName returns the file name of a Windows path without relying
// on the host OS's path separator.
func imageBaseName(path string) string {
	if i := strings.LastIndexAny(path, `\/`); i >= 0 {
		return path[i+1:]
	}
	return path
}
