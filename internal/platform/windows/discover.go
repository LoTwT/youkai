package windows

import (
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

// DiscoverySpec selects the candidate windows of one game client by the
// owning process's image file name (not its full path: installations live
// in user-chosen directories).
type DiscoverySpec struct {
	// ImageName is the executable's file name, compared case-insensitively
	// like Windows compares paths.
	ImageName string
	// ClassNameHint narrows several remaining candidates to this exact
	// window class. A hint that matches nothing leaves the candidates
	// alone: it must not turn a matchable set into "not found".
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

// DiscoverTargets enumerates the visible top-level windows whose process
// image name is spec.ImageName. Windows of that executable that are
// hidden or DWM-cloaked are skipped — they cannot be the window the user
// sees — and windows whose process vanished during the walk are skipped
// rather than failing the whole enumeration. An enumeration error fails
// the call. The result is ordered by PID and handle so the selection list
// is stable between checks.
func DiscoverTargets(win WindowAPI, procs ProcessAPI, spec DiscoverySpec) ([]TargetCandidate, error) {
	if strings.TrimSpace(spec.ImageName) == "" {
		return nil, fmt.Errorf("discover game windows: the process image name is empty")
	}
	var handles []HWND
	if err := win.EnumTopLevel(func(h HWND) { handles = append(handles, h) }); err != nil {
		return nil, fmt.Errorf("enumerate top-level windows: %w", err)
	}
	candidates := make([]TargetCandidate, 0, len(handles))
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
			continue
		}
		if !strings.EqualFold(imageBaseName(path), spec.ImageName) {
			continue
		}
		className, _ := win.ClassName(h)
		candidates = append(candidates, TargetCandidate{
			HWND:             h,
			PID:              pid,
			ImagePath:        path,
			ProcessStartedAt: started,
			ClassName:        className,
			Minimized:        win.IsIconic(h),
		})
	}
	if len(candidates) > 1 && spec.ClassNameHint != "" {
		narrowed := make([]TargetCandidate, 0, len(candidates))
		for _, c := range candidates {
			if c.ClassName == spec.ClassNameHint {
				narrowed = append(narrowed, c)
			}
		}
		if len(narrowed) > 0 {
			candidates = narrowed
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].PID != candidates[j].PID {
			return candidates[i].PID < candidates[j].PID
		}
		return candidates[i].HWND < candidates[j].HWND
	})
	return candidates, nil
}

// imageBaseName returns the file name of a Windows path without relying
// on the host OS's path separator.
func imageBaseName(path string) string {
	if i := strings.LastIndexAny(path, `\/`); i >= 0 {
		return path[i+1:]
	}
	return path
}
