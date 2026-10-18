package macos

import "time"

// Window identity (T2 §2.2): the target is locked by bundle
// identifier + process id + window id; titles are display-only. The
// assessment below distinguishes a unique lock from a missing, replaced
// or ambiguous target, and the ambiguous case always refuses input
// rather than guessing which window is the game.

// NativeWindow is one window as reported by the SCShareableContent
// enumeration. It is a plain data record crossing the native boundary;
// the field meanings are the SCWindow/SCRunningApplication properties
// of the same names.
type NativeWindow struct {
	// WindowID is the CGWindowID of the window.
	WindowID uint32
	// PID is the owning process id.
	PID int32
	// BundleID is the owning application's bundle identifier; empty for
	// processes without a bundle.
	BundleID string
	// AppName is the owning application's localized name; display only.
	AppName string
	// Title is the window title, possibly empty; display only, never
	// part of identity.
	Title string
	// Frame is the window frame in screen points (top-left origin).
	Frame PointRect
	// OnScreen is SCWindow.isOnScreen.
	OnScreen bool
	// Active is SCWindow.isActive (macOS 13.1+); observability only —
	// it can be true while the window is off screen, so it never
	// unlocks input.
	Active bool
	// Layer is the window layer; 0 is the normal layer, popups and
	// system overlays use other layers. Recorded for diagnostics.
	Layer int
}

// NativeDisplay is one display as reported by the enumeration
// (SCDisplay). Frame is in screen points, top-left origin, and may
// extend into negative coordinates for secondary displays.
type NativeDisplay struct {
	// DisplayID is the CGDirectDisplayID.
	DisplayID uint32
	// Width and Height are the display size in points.
	Width, Height int
	// Frame is the display's rect in the global screen point space.
	Frame PointRect
}

// WindowSelector is the locked target identity: bundle id + process
// instance + window id, all three together. A process restart or a
// window replacement breaks the lock and requires a new precheck; the
// selector never re-binds to a same-named new window.
type WindowSelector struct {
	BundleID string
	PID      int32
	WindowID uint32
}

// Matches reports whether the window is exactly the selected one: same
// window id, same owning process instance and same bundle.
func (s WindowSelector) Matches(w NativeWindow) bool {
	return s.WindowID == w.WindowID && s.PID == w.PID && s.BundleID == w.BundleID
}

// SameProcess reports whether the window belongs to the selected
// process instance (bundle + pid, any window id).
func (s WindowSelector) SameProcess(w NativeWindow) bool {
	return s.PID == w.PID && s.BundleID == w.BundleID
}

// TargetStatus classifies the state of the locked target against one
// enumeration snapshot.
type TargetStatus string

const (
	// TargetUnique: the selected window is present and is the only
	// window of its process; input preconditions may proceed to the
	// foreground and visibility checks.
	TargetUnique TargetStatus = "unique"
	// TargetMissing: no window of the selected process exists any more
	// (process exit or all windows closed). The target is invalid;
	// re-precheck is required.
	TargetMissing TargetStatus = "missing"
	// TargetReplaced: the process has window(s), but not the selected
	// window id — the window was replaced. The target is invalid even
	// though the process lives on; re-precheck is required.
	TargetReplaced TargetStatus = "replaced"
	// TargetAmbiguous: the selected window exists, but the same process
	// also owns other windows (login popups, secondary windows). Input
	// is refused until the ambiguity is resolved; the first version
	// does not try to disambiguate by title (T2 §2.2: 无法排除登录弹
	// 窗、同进程其他窗口时停止或拒绝).
	TargetAmbiguous TargetStatus = "ambiguous"
)

// TargetAssessment is the result of assessing a locked selector against
// one enumeration snapshot. It feeds both the capture setup and the
// input readiness verdict.
type TargetAssessment struct {
	// Status classifies the target's state.
	Status TargetStatus
	// Observation is the selected window's observation; set exactly
	// when Status is unique or ambiguous.
	Observation *TargetObservation
	// Candidates lists the windows of the same process instance,
	// including the selected one, when they exist; it backs the
	// replaced and ambiguous reports.
	Candidates []NativeWindow
	// ObservedAt is the enumeration time.
	ObservedAt time.Time
}

// AssessTarget assesses selector against one enumeration snapshot.
// now stamps the observation. The rules, in order:
//
//  1. the selected window (id + pid + bundle) present:
//     no other same-process window → unique; otherwise → ambiguous;
//  2. the selected window absent:
//     same-process windows exist → replaced; none → missing.
func AssessTarget(selector WindowSelector, windows []NativeWindow, now time.Time) TargetAssessment {
	a := TargetAssessment{ObservedAt: now}
	var selected *NativeWindow
	var siblings []NativeWindow
	for i := range windows {
		w := windows[i]
		if !selector.SameProcess(w) {
			continue
		}
		if selector.Matches(w) {
			if selected == nil {
				selected = &windows[i]
			}
			continue
		}
		siblings = append(siblings, w)
	}
	if len(siblings) > 0 {
		a.Candidates = append(a.Candidates, siblings...)
	}
	if selected != nil {
		if len(siblings) > 0 {
			a.Candidates = append(a.Candidates, *selected)
		}
		a.Observation = &TargetObservation{
			Selector:   selector,
			Title:      selected.Title,
			AppName:    selected.AppName,
			Frame:      selected.Frame,
			OnScreen:   selected.OnScreen,
			Active:     selected.Active,
			Layer:      selected.Layer,
			ObservedAt: now,
		}
		if len(siblings) > 0 {
			a.Status = TargetAmbiguous
		} else {
			a.Status = TargetUnique
		}
		return a
	}
	if len(a.Candidates) > 0 {
		a.Status = TargetReplaced
	} else {
		a.Status = TargetMissing
	}
	return a
}

// SelectProcessWindows returns the windows of one process instance
// (bundle + pid) from an enumeration snapshot, for the initial precheck
// where the caller decides whether a process exposes exactly one
// candidate window to lock.
func SelectProcessWindows(selector WindowSelector, windows []NativeWindow) []NativeWindow {
	var out []NativeWindow
	for _, w := range windows {
		if selector.SameProcess(w) {
			out = append(out, w)
		}
	}
	return out
}
