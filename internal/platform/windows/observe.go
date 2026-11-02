package windows

import (
	"errors"
	"fmt"
	"time"
)

// ForegroundAPI queries foreground and hit-testing facts. None of these
// calls changes any state the scan depends on; RequestForeground is the
// only mutating call and its outcome is verified by observation, never
// assumed.
type ForegroundAPI interface {
	// ForegroundWindow returns the actual foreground window
	// (GetForegroundWindow). The scanner's own windows are not filtered
	// out: the scanner being foreground is focus lost like any other
	// window.
	ForegroundWindow() HWND
	// RequestForeground asks the system to give the target the foreground
	// and reports whether the target actually is the foreground window
	// afterwards. SetForegroundWindow can be refused by the system even
	// when its documented conditions hold, and its BOOL result alone is
	// not trustworthy, so the result is the observed foreground, and a
	// false outcome leaves the session in preparing — never "ready".
	RequestForeground(hwnd HWND) bool
	// HitWindow returns the window visible at a screen point
	// (WindowFromPoint). It ignores hidden and disabled windows, so a
	// clear result is evidence about the checked point only, not proof
	// of no occlusion anywhere.
	HitWindow(p PixelPoint) HWND
	// RootOf returns the root window of hwnd (GetAncestor GA_ROOT); a
	// hit on a child window of the target counts as the target.
	RootOf(hwnd HWND) HWND
}

const (
	// foregroundSettlePollEvery is the foreground observation cadence while
	// a requested switch is in flight. GetForegroundWindow is a cheap,
	// side-effect-free read; 25 ms keeps the success path short without
	// producing a meaningful CPU cost for the bounded window.
	foregroundSettlePollEvery = 25 * time.Millisecond
	// foregroundSettleLimit bounds the settle wait at roughly twice the
	// ~0.24 s observed on 2026-11-02 (see the Windows local field report).
	// A longer window would delay the unchanged readiness fallback without
	// evidence that the switch takes longer.
	foregroundSettleLimit = 500 * time.Millisecond
)

// ForegroundSettle bounds how long ActivateTarget watches the observed
// foreground after requesting a switch. Zero fields take the defaults;
// tests inject Sleep and Now so no test waits on a real clock.
type ForegroundSettle struct {
	// PollEvery is the observation cadence; zero means 25 ms.
	PollEvery time.Duration
	// Limit is the total observation window; zero means 500 ms.
	Limit time.Duration
	// Sleep waits between observations; nil means time.Sleep.
	Sleep func(time.Duration)
	// Now supplies the observation clock; nil means time.Now.
	Now func() time.Time
}

func (s ForegroundSettle) withDefaults() ForegroundSettle {
	if s.PollEvery <= 0 {
		s.PollEvery = foregroundSettlePollEvery
	}
	if s.Limit <= 0 {
		s.Limit = foregroundSettleLimit
	}
	if s.Sleep == nil {
		s.Sleep = time.Sleep
	}
	if s.Now == nil {
		s.Now = time.Now
	}
	return s
}

// ActivateTarget asks the system to bring hwnd to the foreground and
// verifies the outcome through the actual foreground window. Windows can
// refuse SetForegroundWindow even when its documented conditions appear
// to hold, and an accepted switch can land asynchronously, so a false
// result right after the call is not final: the observed foreground is
// polled for a bounded settle window. The returned duration is the time
// from the request to the observation that found the target (zero when it
// was already foreground); an error means the target was still not
// foreground at the limit, and the caller must keep waiting for the user.
func ActivateTarget(fg ForegroundAPI, hwnd HWND, settle ForegroundSettle) (time.Duration, error) {
	settle = settle.withDefaults()
	start := settle.Now()
	if fg.RequestForeground(hwnd) {
		return 0, nil
	}
	deadline := start.Add(settle.Limit)
	// The iteration cap keeps the wait bounded even if an injected clock
	// fails to advance.
	maxPolls := int((settle.Limit + settle.PollEvery - 1) / settle.PollEvery)
	for poll := 0; poll < maxPolls; poll++ {
		remaining := deadline.Sub(settle.Now())
		if remaining <= 0 {
			break
		}
		wait := settle.PollEvery
		if wait > remaining {
			wait = remaining
		}
		settle.Sleep(wait)
		if fg.ForegroundWindow() == hwnd {
			return settle.Now().Sub(start), nil
		}
	}
	return 0, fmt.Errorf("target hwnd %d not foreground after %d ms (SetForegroundWindow refused or focus moved)", hwnd, settle.Limit.Milliseconds())
}

// OcclusionState is what the platform knows about occlusion of the
// target at the checked points.
type OcclusionState string

const (
	// OcclusionNotChecked: passive observation, no points checked.
	OcclusionNotChecked OcclusionState = "notChecked"
	// OcclusionClear: every checked point hit the target window tree.
	OcclusionClear OcclusionState = "clear"
	// OcclusionOccluded: a checked point is covered by another window,
	// or the hit test is indeterminate.
	OcclusionOccluded OcclusionState = "occluded"
)

// TargetObservation is the "target observation" handover category of T2
// §4.2: one dated snapshot of the locked target — identity verdict,
// foreground/visibility/minimized state, occlusion state of the checked
// points, geometry with version, and the fault when the premises are
// broken. The title of the target is not part of identity and is not
// repeated here. Observations are reports only: nothing here opens or
// closes input permission by itself.
type TargetObservation struct {
	ObservedAt time.Time
	Identity   WindowIdentity
	// Matched: the re-verified window still is the locked target.
	Matched  bool
	Mismatch *MismatchError
	// Foreground is true when the target is the actual foreground window.
	Foreground bool
	Visible    bool
	Minimized  bool
	// Occlusion is the checked-point state; passive observations leave
	// it notChecked.
	Occlusion OcclusionState
	// Geometry is the current geometry; before the first valid
	// measurement it is the zero value. On a fault it is the last
	// geometry that was still valid.
	Geometry ClientGeometry
	// GeometryChanged: this observation bumped the geometry version, so
	// coordinates derived from older versions are invalid.
	GeometryChanged bool
	// Fault is set when the premises are broken (target lost, geometry
	// unavailable, DPI virtualized or unverifiable). Focus loss or
	// minimization are observations, not faults: they are premises the
	// kernel weighs, and a regained focus is just another observation.
	Fault *PlatformFault
}

// ObserveTarget re-verifies the locked target and refreshes its
// geometry: window alive, owner PID, process identity (full executable
// verification on this cadence), client measurement with consistency
// and DPI checks, and the foreground/visibility/minimized facts. It
// reports; it never dispatches input, never changes permission and
// never attempts recovery — a lost target requires a fresh precheck with
// a new lock.
func ObserveTarget(win WindowAPI, procs ProcessAPI, geo GeometryAPI, fg ForegroundAPI, locked WindowIdentity, tracker *GeometryTracker, at time.Time) TargetObservation {
	obs := TargetObservation{
		ObservedAt: at,
		Identity:   locked,
		Occlusion:  OcclusionNotChecked,
	}
	lastVersion := GeometryVersion(0)
	if current, ok := tracker.Current(); ok {
		lastVersion = current.Version
		obs.Geometry = current
	}

	if !win.IsWindow(locked.HWND) {
		obs.Mismatch = &MismatchError{Reason: MismatchWindowGone, Detail: "window handle no longer names a window"}
		obs.Fault = NewFault(CodeTargetLost, "target window gone", nil, PhaseObserve, at, lastVersion)
		return obs
	}
	pid, err := win.ThreadProcessID(locked.HWND)
	if err != nil || pid == 0 {
		detail := "no owning process"
		if err != nil {
			detail = err.Error()
		}
		obs.Mismatch = &MismatchError{Reason: MismatchWindowGone, Detail: detail}
		obs.Fault = NewFault(CodeTargetLost, "target window has no owning process", err, PhaseObserve, at, lastVersion)
		return obs
	}
	path, started, err := procs.Query(pid)
	if err != nil {
		obs.Mismatch = &MismatchError{Reason: MismatchWindowGone, Detail: "process identity unresolvable: " + err.Error()}
		obs.Fault = NewFault(CodeTargetLost, "owning process cannot be verified", err, PhaseObserve, at, lastVersion)
		return obs
	}
	if mismatch := VerifyLocked(locked, WindowProbe{
		HWND:             locked.HWND,
		PID:              pid,
		ImagePath:        path,
		ProcessStartedAt: started,
	}); mismatch != nil {
		var me *MismatchError
		errors.As(mismatch, &me)
		obs.Mismatch = me
		obs.Fault = NewFault(CodeTargetLost, "target identity mismatch", mismatch, PhaseObserve, at, lastVersion)
		return obs
	}
	obs.Matched = true
	obs.Visible = win.IsWindowVisible(locked.HWND)
	obs.Minimized = win.IsIconic(locked.HWND)
	obs.Foreground = fg.ForegroundWindow() == locked.HWND

	measurement, fault := MeasureClient(geo, locked.HWND)
	if fault == nil {
		if self, selfErr := geo.SelfDpiAwareness(); selfErr != nil {
			fault = NewFault(CodeDpiCheckUnavailable, "scanner DPI awareness unverifiable", selfErr, PhaseObserve, at, lastVersion)
		} else if dpiFault := VerifyDpiSetup(self, measurement.WindowDPI, measurement.MonitorDPI); dpiFault != nil {
			dpiFault.At = at
			fault = dpiFault
		}
	}
	if fault != nil {
		fault.At = at
		fault.LastGeometryVersion = lastVersion
		obs.Fault = fault
		// obs.Geometry already holds the last valid geometry from above.
		return obs
	}
	current, changed := tracker.Update(measurement, at)
	obs.Geometry = current
	obs.GeometryChanged = changed
	return obs
}
