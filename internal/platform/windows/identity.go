package windows

import (
	"fmt"
	"strings"
	"time"
)

// HWND is an opaque window handle value as observed through the Win32
// API. Zero means "no window". The system may reuse a handle value after
// the window is destroyed, so numeric equality alone is never identity
// proof; WindowIdentity adds the owning process instance, and
// verification re-checks it on every observation.
type HWND uintptr

// ProcessID identifies a process instance to the OS. PID values are
// reused after processes exit, so a PID is only meaningful together with
// the process creation time.
type ProcessID uint32

// DpiAwareness mirrors the DPI_AWARENESS value of the calling thread's
// DPI_AWARENESS_CONTEXT (GetThreadDpiAwarenessContext +
// GetAwarenessFromDpiAwarenessContext). PerMonitorV2 refines per-monitor
// awareness by comparing the context with
// DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 through
// AreDpiAwarenessContextsEqual.
type DpiAwareness int

const (
	DpiAwarenessUnaware      DpiAwareness = 0
	DpiAwarenessSystem       DpiAwareness = 1
	DpiAwarenessPerMonitor   DpiAwareness = 2
	DpiAwarenessPerMonitorV2 DpiAwareness = 3
)

// WindowIdentity is the locked identity of the game target: the window
// handle plus the process instance (PID, executable, creation time) that
// owned it at verification time. If the window is replaced or the
// process restarts, this identity is void and a new precheck must lock a
// new target; the old handle is never carried over.
type WindowIdentity struct {
	HWND HWND
	PID  ProcessID
	// ImagePath is the full path of the owning process executable,
	// compared case-insensitively like Windows itself compares paths.
	ImagePath string
	// ProcessStartedAt distinguishes a restarted process that happened to
	// reuse the same PID.
	ProcessStartedAt time.Time
	// ClassName is a screening hint recorded at lock time. It is not
	// identity: class names are not unique, so verification deliberately
	// ignores them.
	ClassName string
}

// WindowProbe is one enumerated top-level window with the facts used by
// selection and verification. It deliberately carries no window title:
// probing another process's caption is user data the scanner never needs
// (T2 §3.2).
type WindowProbe struct {
	HWND             HWND
	PID              ProcessID
	ImagePath        string
	ProcessStartedAt time.Time
	ClassName        string
	Visible          bool
	Minimized        bool
	Cloaked          bool
}

// TargetSpec selects the game window from enumerated probes. The
// executable path is the authoritative identity; the class name is a
// screening hint applied only when the executable alone leaves several
// candidates (launcher, login or secondary windows of the same process).
type TargetSpec struct {
	// ExecutablePath is the full path of the game executable, compared
	// case-insensitively.
	ExecutablePath string
	// HWNDHint, when set, restricts selection to exactly this window: the
	// user confirmed it from the discovered candidates. A hint that
	// matches no window is a stale selection (SelectionHintGone) — it
	// never lets the selector fall back to another window of the same
	// executable. Handles are recycled, so the path and process instance
	// remain the identity checks.
	HWNDHint HWND
	// PIDHint and ProcessStartedAtHint, when set, must match the window's
	// process instance inside the confirmed-handle branch: a handle the
	// OS recycled into a restarted process is stale, not the target.
	PIDHint              ProcessID
	ProcessStartedAtHint time.Time
	// ClassNameHint, when set, must equal the candidate's class name.
	ClassNameHint string
	// RequireVisible requires the selected window to be visible and not
	// cloaked; a hidden or suspended candidate yields a not-ready result
	// instead of a lock.
	RequireVisible bool
}

// SelectionErrorKind classifies why selection did not produce one
// window.
type SelectionErrorKind string

const (
	// SelectionNoWindow: no top-level window belongs to the executable.
	SelectionNoWindow SelectionErrorKind = "noWindow"
	// SelectionNotReady: the window is the only candidate but is hidden
	// or cloaked, so it cannot be locked as a visible target yet.
	SelectionNotReady SelectionErrorKind = "notReady"
	// SelectionAmbiguous: several windows remain candidates; locking any
	// of them could act on the wrong window (launcher, login window,
	// second instance), so the caller must disambiguate.
	SelectionAmbiguous SelectionErrorKind = "ambiguous"
	// SelectionHintGone: the caller confirmed a specific window handle
	// that no longer exists among the executable's windows (closed or
	// replaced). The user must enumerate and confirm again; the selector
	// never substitutes another window.
	SelectionHintGone SelectionErrorKind = "hintGone"
)

// SelectionError reports that selection did not yield exactly one
// lockable window.
type SelectionError struct {
	Kind       SelectionErrorKind
	Executable string
	Candidates []WindowProbe // remaining candidates, for ambiguous
	HintDetail string
}

func (e *SelectionError) Error() string {
	switch e.Kind {
	case SelectionNoWindow:
		return fmt.Sprintf("no top-level window of %q", e.Executable)
	case SelectionNotReady:
		return fmt.Sprintf("target window of %q is %s", e.Executable, e.HintDetail)
	case SelectionHintGone:
		return fmt.Sprintf("the confirmed window %s of %q is gone or was replaced", e.HintDetail, e.Executable)
	default:
		return fmt.Sprintf("%d candidate windows of %q remain (launcher, login or same-name windows must be told apart)", len(e.Candidates), e.Executable)
	}
}

// SelectTarget picks the game window from enumerated probes. The
// algorithm is deliberately conservative:
//
//  1. keep the probes whose process image path matches the spec
//     (case-insensitive);
//  2. if the caller confirmed a specific handle, keep exactly that window
//     and require the confirmed process instance (PID and start time when
//     set) to match: a confirmed window that vanished or was recycled is
//     a stale selection, never a cue to pick another window of the same
//     executable;
//  3. if several remain and hints are set, narrow by exact class name —
//     hints refine identity, they never pick a window the executable does
//     not own;
//  4. if several remain, drop hidden and cloaked windows whenever at
//     least one visible candidate exists: one visible candidate is
//     selected, several visible candidates are an ambiguity error whose
//     list contains only the visible ones, and with no visible candidate
//     the set is left unchanged;
//  5. more than one candidate at that point is an ambiguity error —
//     never a guess;
//  6. the single candidate fails not-ready when it is hidden or cloaked
//     and RequireVisible is set; a minimized window is still selected
//     (minimization is an observed state, input is refused while it
//     lasts).
func SelectTarget(probes []WindowProbe, spec TargetSpec) (WindowProbe, error) {
	candidates := make([]WindowProbe, 0, len(probes))
	for _, p := range probes {
		if strings.EqualFold(p.ImagePath, spec.ExecutablePath) {
			candidates = append(candidates, p)
		}
	}
	if len(candidates) == 0 {
		return WindowProbe{}, &SelectionError{Kind: SelectionNoWindow, Executable: spec.ExecutablePath}
	}
	if spec.HWNDHint != 0 {
		confirmed := WindowProbe{}
		found := false
		for _, p := range candidates {
			if p.HWND == spec.HWNDHint {
				confirmed = p
				found = true
				break
			}
		}
		if !found {
			return WindowProbe{}, &SelectionError{Kind: SelectionHintGone, Executable: spec.ExecutablePath,
				HintDetail: fmt.Sprintf("%d", spec.HWNDHint)}
		}
		if (spec.PIDHint != 0 && confirmed.PID != spec.PIDHint) ||
			(!spec.ProcessStartedAtHint.IsZero() && !confirmed.ProcessStartedAt.Equal(spec.ProcessStartedAtHint)) {
			return WindowProbe{}, &SelectionError{Kind: SelectionHintGone, Executable: spec.ExecutablePath,
				HintDetail: fmt.Sprintf("%d (process instance changed)", spec.HWNDHint)}
		}
		candidates = []WindowProbe{confirmed}
	}
	if len(candidates) > 1 && spec.ClassNameHint != "" {
		narrowed := candidates[:0]
		for _, p := range candidates {
			if p.ClassName == spec.ClassNameHint {
				narrowed = append(narrowed, p)
			}
		}
		if len(narrowed) > 0 {
			candidates = narrowed
		}
	}
	if len(candidates) > 1 {
		visible := make([]WindowProbe, 0, len(candidates))
		for _, p := range candidates {
			if p.Visible && !p.Cloaked {
				visible = append(visible, p)
			}
		}
		// Hidden IME/helper windows must not pollute the remaining set: when
		// any visible candidate exists, only those are real choices. A single
		// one is selected; several remain ambiguous (still fail-closed).
		if len(visible) > 0 {
			candidates = visible
		}
	}
	if len(candidates) > 1 {
		return WindowProbe{}, &SelectionError{Kind: SelectionAmbiguous, Executable: spec.ExecutablePath, Candidates: candidates}
	}
	cand := candidates[0]
	if spec.RequireVisible && (!cand.Visible || cand.Cloaked) {
		detail := "hidden"
		if cand.Cloaked {
			detail = "cloaked"
		}
		return WindowProbe{}, &SelectionError{Kind: SelectionNotReady, Executable: spec.ExecutablePath, HintDetail: detail}
	}
	return cand, nil
}

// LockIdentity turns a selected probe into the locked target identity.
func LockIdentity(p WindowProbe) WindowIdentity {
	return WindowIdentity{
		HWND:             p.HWND,
		PID:              p.PID,
		ImagePath:        p.ImagePath,
		ProcessStartedAt: p.ProcessStartedAt,
		ClassName:        p.ClassName,
	}
}

// MismatchReason classifies why a fresh probe no longer matches the
// locked target.
type MismatchReason string

const (
	// MismatchWindowGone: the handle no longer names a window.
	MismatchWindowGone MismatchReason = "windowGone"
	// MismatchWindowReplaced: the handle value now belongs to another
	// window or process (HWND values are recycled).
	MismatchWindowReplaced MismatchReason = "windowReplaced"
	// MismatchProcessRestarted: same PID, different process instance.
	MismatchProcessRestarted MismatchReason = "processRestarted"
	// MismatchExecutableChanged: same process instance but a different
	// executable path (defensive; should not occur).
	MismatchExecutableChanged MismatchReason = "executableChanged"
)

// MismatchError is the identity verdict of one verification pass.
type MismatchError struct {
	Reason MismatchReason
	Detail string
}

func (e *MismatchError) Error() string {
	return fmt.Sprintf("target mismatch: %s: %s", e.Reason, e.Detail)
}

// VerifyLocked reports whether observed still is the locked target. The
// observed probe must name the same handle; class names and titles of
// the observed window are deliberately ignored — they are hints, not
// identity, and games change their titles freely.
func VerifyLocked(locked WindowIdentity, observed WindowProbe) error {
	if observed.HWND != locked.HWND {
		return &MismatchError{Reason: MismatchWindowReplaced, Detail: fmt.Sprintf("observed handle %d, locked handle %d", observed.HWND, locked.HWND)}
	}
	if observed.PID != locked.PID {
		return &MismatchError{Reason: MismatchWindowReplaced, Detail: fmt.Sprintf("handle %d now owned by pid %d, locked pid %d", locked.HWND, observed.PID, locked.PID)}
	}
	if !observed.ProcessStartedAt.Equal(locked.ProcessStartedAt) {
		return &MismatchError{Reason: MismatchProcessRestarted, Detail: fmt.Sprintf("pid %d restarted: started %s, locked start %s", observed.PID, observed.ProcessStartedAt.Format(time.RFC3339Nano), locked.ProcessStartedAt.Format(time.RFC3339Nano))}
	}
	if !strings.EqualFold(observed.ImagePath, locked.ImagePath) {
		return &MismatchError{Reason: MismatchExecutableChanged, Detail: fmt.Sprintf("pid %d image %q, locked image %q", observed.PID, observed.ImagePath, locked.ImagePath)}
	}
	return nil
}

// WindowAPI is the user32 surface the identity logic runs on. The real
// implementation is the Win32 path; pure-logic tests inject fakes.
type WindowAPI interface {
	// EnumTopLevel enumerates the current top-level windows, invoking
	// visit once per window. It returns the error of the enumeration
	// itself; visit cannot abort the walk.
	EnumTopLevel(visit func(HWND)) error
	// ThreadProcessID reports the PID that currently owns hwnd. A zero
	// PID means the window has no owner (typically: gone between
	// enumeration and query).
	ThreadProcessID(hwnd HWND) (ProcessID, error)
	IsWindow(hwnd HWND) bool
	IsWindowVisible(hwnd HWND) bool
	IsIconic(hwnd HWND) bool
	// Cloaked reports the DWM cloaked state of hwnd; a query error is
	// best-effort treated as "not cloaked" by the collector.
	Cloaked(hwnd HWND) (bool, error)
	ClassName(hwnd HWND) (string, error)
	// Title reads the window title. Probe collection deliberately does not
	// call it: another process's caption is user data the scan never needs.
	// It remains for explicit diagnostic tools (tools/winprobe), which
	// report titles on purpose.
	Title(hwnd HWND) (string, error)
}

// ProcessAPI resolves the identity of a process instance.
type ProcessAPI interface {
	// Query reports the full executable path and creation time of pid.
	// The handle the query opens is closed within the call.
	Query(pid ProcessID) (path string, started time.Time, err error)
	// ImageName reports the executable file name of pid from a process
	// snapshot (CreateToolhelp32Snapshot), which does not open the target
	// process and works across integrity levels. It is the fallback for
	// naming a window whose Query was denied, so a denied window can be
	// checked against the game's image name without being opened.
	ImageName(pid ProcessID) (string, error)
}

// CollectProbes enumerates all top-level windows and resolves their
// process identity. Windows whose PID is zero or whose process cannot be
// queried (typically: vanished during the walk) are skipped rather than
// failing the collection; an enumeration error fails it. The cloak check
// is best-effort and degrades to "not cloaked" when DWM reports nothing.
func CollectProbes(win WindowAPI, procs ProcessAPI) ([]WindowProbe, error) {
	var handles []HWND
	if err := win.EnumTopLevel(func(h HWND) { handles = append(handles, h) }); err != nil {
		return nil, fmt.Errorf("enumerate top-level windows: %w", err)
	}
	probes := make([]WindowProbe, 0, len(handles))
	for _, h := range handles {
		pid, err := win.ThreadProcessID(h)
		if err != nil || pid == 0 {
			continue
		}
		path, started, err := procs.Query(pid)
		if err != nil {
			continue
		}
		className, _ := win.ClassName(h)
		cloaked := false
		if c, err := win.Cloaked(h); err == nil {
			cloaked = c
		}
		probes = append(probes, WindowProbe{
			HWND:             h,
			PID:              pid,
			ImagePath:        path,
			ProcessStartedAt: started,
			ClassName:        className,
			Visible:          win.IsWindowVisible(h),
			Minimized:        win.IsIconic(h),
			Cloaked:          cloaked,
		})
	}
	return probes, nil
}
