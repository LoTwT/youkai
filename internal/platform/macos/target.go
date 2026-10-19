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

// Target candidate filtering (实测轮 ①): the cloud client's process owns
// several small layer-0 windows besides the real one (four 33 pt strips
// and a 500×500 untitled overlay), so "any same-process window means
// ambiguity" — the rule AssessTarget applies — would misjudge every
// observation. Resolution instead passes each window through a predicate
// and demands that exactly one survivor remains; the same predicate, with
// the selected window itself excluded, decides which siblings count as
// ambiguity sources on later observations.

// candidateEdgeEpsilon is the slack, in points, when testing that a
// window frame lies fully inside a display: window and display frames
// come from the same enumeration snapshot but may differ by sub-point
// rounding between the two sources.
const candidateEdgeEpsilon = 0.01

// WindowFullyInsideDisplays reports whether frame lies completely inside
// one of the displays (allowing the epsilon above on every edge). A
// window that only overlaps a display, or straddles two, fails.
func WindowFullyInsideDisplays(frame PointRect, displays []NativeDisplay) bool {
	for _, d := range displays {
		if frame.X >= d.Frame.X-candidateEdgeEpsilon &&
			frame.Y >= d.Frame.Y-candidateEdgeEpsilon &&
			frame.MaxX() <= d.Frame.MaxX()+candidateEdgeEpsilon &&
			frame.MaxY() <= d.Frame.MaxY()+candidateEdgeEpsilon {
			return true
		}
	}
	return false
}

// Minimum size of a target candidate, in screen points. The cloud
// client creates transient same-process status chips (field round 2 saw
// a 66x20 chip at the window's top-left while the game was frontmost);
// such a window cannot carry a game surface and must not turn a valid
// target into an ambiguous one. The bound is far below any usable game
// window and far above these chips.
const (
	minCandidateWidth  = 320
	minCandidateHeight = 240
)

// IsTargetCandidate reports whether w passes the target-resolution
// predicate: it belongs to bundleID, sits in the normal window layer
// (0), is on screen, is at least minCandidateWidth × minCandidateHeight,
// and lies fully inside one display. Layer, onScreen, size and in-display
// each exclude a class of windows the field rounds observed around the
// cloud client (system overlays, status chips, minimized or parked
// windows, partially dragged-off windows); the predicate is a resolution
// rule, not a health report — a healthy window that fails it is simply
// not a resolvable target right now.
func IsTargetCandidate(bundleID string, w NativeWindow, displays []NativeDisplay) bool {
	if w.BundleID != bundleID || w.Layer != 0 || !w.OnScreen {
		return false
	}
	if w.Frame.W < minCandidateWidth || w.Frame.H < minCandidateHeight {
		return false
	}
	return WindowFullyInsideDisplays(w.Frame, displays)
}

// FilterTargetCandidates returns the windows of bundleID that pass the
// candidate predicate, in enumeration order. A fresh session must find
// exactly one survivor to lock; the caller reports zero and multi-window
// outcomes (it owns the diagnostic vocabulary).
func FilterTargetCandidates(bundleID string, windows []NativeWindow, displays []NativeDisplay) []NativeWindow {
	var out []NativeWindow
	for _, w := range windows {
		if IsTargetCandidate(bundleID, w, displays) {
			out = append(out, w)
		}
	}
	return out
}

// FilterAmbiguitySiblings returns the windows of the locked process that
// pass the candidate predicate, excluding the selected window itself.
// Only these count as ambiguity sources when re-assessing a locked
// target: the selected window's own state (minimized, dragged half off a
// display) is the target's problem, not ambiguity, and same-process
// windows that fail the predicate are not resolvable targets and must
// not block input to the locked one.
func FilterAmbiguitySiblings(selector WindowSelector, windows []NativeWindow, displays []NativeDisplay) []NativeWindow {
	var out []NativeWindow
	for _, w := range windows {
		if !selector.SameProcess(w) || selector.Matches(w) {
			continue
		}
		if IsTargetCandidate(selector.BundleID, w, displays) {
			out = append(out, w)
		}
	}
	return out
}
