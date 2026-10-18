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

// DpiAwareness mirrors the process/thread DPI awareness values of
// GetCurrentDpiAwarenessContext.
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
	// ClassName and Title are screening hints recorded at lock time. They
	// are not identity: titles change freely and class names are not
	// unique, so verification deliberately ignores them.
	ClassName string
	Title     string
}

// WindowProbe is one enumerated top-level window with the facts used by
// selection and verification.
type WindowProbe struct {
	HWND             HWND
	PID              ProcessID
	ImagePath        string
	ProcessStartedAt time.Time
	ClassName        string
	Title            string
	Visible          bool
	Minimized        bool
	Cloaked          bool
}

// TargetSpec selects the game window from enumerated probes. The
// executable path is the authoritative identity; class and title are
// screening hints applied only when the executable alone leaves several
// candidates (launcher, login or secondary windows of the same process).
type TargetSpec struct {
	// ExecutablePath is the full path of the game executable, compared
	// case-insensitively.
	ExecutablePath string
	// ClassNameHint, when set, must equal the candidate's class name.
	ClassNameHint string
	// TitleHint, when set, must be contained in the candidate's title.
	TitleHint string
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
	default:
		return fmt.Sprintf("%d candidate windows of %q remain (launcher, login or same-name windows must be told apart)", len(e.Candidates), e.Executable)
	}
}

// SelectTarget picks the game window from enumerated probes. The
// algorithm is deliberately conservative:
//
//  1. keep the probes whose process image path matches the spec
//     (case-insensitive);
//  2. if several remain and hints are set, narrow by exact class name,
//     then by title substring — hints refine identity, they never pick a
//     window the executable does not own;
//  3. if several remain, prefer the single window that is visible and
//     not cloaked;
//  4. more than one at that point is an ambiguity error — never a guess;
//  5. the single candidate fails not-ready when it is hidden or cloaked
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
	if len(candidates) > 1 && spec.TitleHint != "" {
		narrowed := candidates[:0]
		for _, p := range candidates {
			if strings.Contains(p.Title, spec.TitleHint) {
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
		if len(visible) == 1 {
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
		Title:            p.Title,
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
	// Title reads the window title. Reading the caption of another
	// process's top-level window does not send it a message, so a hung
	// target cannot hang the reader.
	Title(hwnd HWND) (string, error)
}

// ProcessAPI resolves the identity of a process instance.
type ProcessAPI interface {
	// Query reports the full executable path and creation time of pid.
	// The handle the query opens is closed within the call.
	Query(pid ProcessID) (path string, started time.Time, err error)
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
		title, _ := win.Title(h)
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
			Title:            title,
			Visible:          win.IsWindowVisible(h),
			Minimized:        win.IsIconic(h),
			Cloaked:          cloaked,
		})
	}
	return probes, nil
}
