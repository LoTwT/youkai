package macos

import "time"

// Foreground and visibility verification before input (T2 §2.2, §3.1,
// §5): the frontmost application PID must match the target process,
// the window must be on screen and sufficiently visible inside the
// known display bounds, and — when the caller queries it — the AX
// focused window must corroborate process and geometry. isActive is
// never accepted as proof, and an AX query failure or timeout rejects
// input instead of falling back to a weaker check.

// ForegroundState is one NSWorkspace.frontmostApplication observation:
// the pid of the application that would receive key events, or the
// error the query produced. A frontmost PID match alone does not make
// the window ready — the verdict combines it with the target
// assessment and, when provided, the AX report.
type ForegroundState struct {
	// FrontmostPID is the frontmost application's process id.
	FrontmostPID int32
	// Err is the query failure; when set, FrontmostPID is void.
	Err error
	// ObservedAt is when the query ran.
	ObservedAt time.Time
}

// AXFocusReport is one system-wide AXFocusedWindow query with a
// bounded messaging timeout: the focused window's owning process,
// frame (screen points, top-left origin — the same space as window
// frames) and role. Err (including a timeout) voids the data; a failed
// or mismatching report denies input (T2 §2.2: AX 查询失败/超时或无法
// 唯一对应目标时拒绝输入).
type AXFocusReport struct {
	// PID is the process owning the focused window.
	PID int32
	// Frame is the focused window's frame in screen points.
	Frame PointRect
	// Role is the focused element's AXRole, e.g. "AXWindow"; empty when
	// the element reports none.
	Role string
	// Err is the query failure (including timeout); when set, the
	// fields above are void.
	Err error
	// TimedOut reports that the bounded AX message timeout elapsed.
	TimedOut bool
	// ObservedAt is when the query completed.
	ObservedAt time.Time
}

// ReadinessDenialCode names one reason input was denied.
type ReadinessDenialCode string

const (
	// DenialTargetMissing: the target window's process has no windows.
	DenialTargetMissing ReadinessDenialCode = "targetMissing"
	// DenialTargetReplaced: the selected window was replaced.
	DenialTargetReplaced ReadinessDenialCode = "targetReplaced"
	// DenialTargetAmbiguous: the process owns other windows besides the
	// target.
	DenialTargetAmbiguous ReadinessDenialCode = "targetAmbiguous"
	// DenialWindowOffScreen: the target window reports isOnScreen ==
	// false (covers minimized and fully off-screen windows).
	DenialWindowOffScreen ReadinessDenialCode = "windowOffScreen"
	// DenialDisplayBoundsUnknown: no display bounds were supplied, so
	// visibility cannot be verified.
	DenialDisplayBoundsUnknown ReadinessDenialCode = "displayBoundsUnknown"
	// DenialWindowNotVisibleEnough: the target window is visible only
	// below the configured fraction of its area inside the displays.
	DenialWindowNotVisibleEnough ReadinessDenialCode = "windowNotVisibleEnough"
	// DenialFrontmostQueryFailed: the frontmost application query
	// failed; input is not allowed on unknown foreground.
	DenialFrontmostQueryFailed ReadinessDenialCode = "frontmostQueryFailed"
	// DenialNotFrontmost: another application is frontmost.
	DenialNotFrontmost ReadinessDenialCode = "notFrontmost"
	// DenialAXQueryFailed: the AX focused window query failed or timed
	// out; fail closed.
	DenialAXQueryFailed ReadinessDenialCode = "axQueryFailed"
	// DenialAXFocusOtherProcess: the AX focused window belongs to
	// another process.
	DenialAXFocusOtherProcess ReadinessDenialCode = "axFocusOtherProcess"
	// DenialAXFrameMismatch: the AX focused window's frame disagrees
	// with the target window frame beyond the tolerance.
	DenialAXFrameMismatch ReadinessDenialCode = "axFrameMismatch"
	// DenialPositionOutsideTarget: the requested position lies outside
	// the target window frame.
	DenialPositionOutsideTarget ReadinessDenialCode = "positionOutsideTarget"
	// DenialPositionOffScreen: the requested position lies outside all
	// known displays.
	DenialPositionOffScreen ReadinessDenialCode = "positionOffScreen"
)

// ReadinessDenial is one failed precondition with its detail.
type ReadinessDenial struct {
	Code   ReadinessDenialCode
	Detail string
}

// ReadinessVerdict is the combined input-precondition verdict. Ready
// requires that no denial fired.
type ReadinessVerdict struct {
	// Ready reports whether every precondition passed.
	Ready bool
	// Denials lists every failed precondition, in check order.
	Denials []ReadinessDenial
	// AssessedAt is the verdict time.
	AssessedAt time.Time
}

// ReadinessOptions tunes the verdict. The defaults are candidates
// pending real-device tuning (实测轮 ①): the visible fraction requires
// the window to be almost fully on screen (partial windows with the
// click point still visible are rejected conservatively), and the AX
// frame tolerance absorbs small disagreements between the AX geometry
// and the window frame.
type ReadinessOptions struct {
	// MinVisibleFraction is the minimum fraction of the window's area
	// that must intersect the display bounds; zero means the default.
	MinVisibleFraction float64
	// AXFrameTolerance is the allowed per-edge disagreement, in points,
	// between the AX focused window frame and the target window frame;
	// zero means the default.
	AXFrameTolerance float64
	// PositionTolerance is the slack, in points, when testing that a
	// requested position lies inside the window frame and the displays;
	// zero means the default.
	PositionTolerance float64
}

const (
	defaultMinVisibleFraction = 0.9
	defaultAXFrameTolerance   = 2.0
	defaultPositionTolerance  = 0.5
)

func (o ReadinessOptions) withDefaults() ReadinessOptions {
	if o.MinVisibleFraction <= 0 {
		o.MinVisibleFraction = defaultMinVisibleFraction
	}
	if o.AXFrameTolerance <= 0 {
		o.AXFrameTolerance = defaultAXFrameTolerance
	}
	if o.PositionTolerance <= 0 {
		o.PositionTolerance = defaultPositionTolerance
	}
	return o
}

// AssessInputReadiness combines the target assessment, the frontmost
// observation, an optional AX focused window report and the display
// bounds into one verdict. pos is the intended input position in
// screen points (nil when the operation has no position, e.g. a key
// press). Every check that has the data to run must pass; a query
// failure denies input (fail closed), and SCWindow.isActive is not
// consulted at all.
func AssessInputReadiness(
	assessment TargetAssessment,
	foreground ForegroundState,
	ax *AXFocusReport,
	displays []PointRect,
	pos *Point,
	opts ReadinessOptions,
	now time.Time,
) ReadinessVerdict {
	o := opts.withDefaults()
	v := ReadinessVerdict{AssessedAt: now}
	deny := func(code ReadinessDenialCode, detail string) {
		v.Denials = append(v.Denials, ReadinessDenial{Code: code, Detail: detail})
	}

	switch assessment.Status {
	case TargetUnique:
	case TargetMissing:
		deny(DenialTargetMissing, "no window of the selected process remains")
	case TargetReplaced:
		deny(DenialTargetReplaced, "the selected window was replaced by another window of the same process")
	case TargetAmbiguous:
		deny(DenialTargetAmbiguous, "the same process owns other windows besides the target")
	}
	obs := assessment.Observation
	if obs == nil {
		v.Ready = len(v.Denials) == 0
		return v
	}

	if !obs.OnScreen {
		deny(DenialWindowOffScreen, "SCWindow.isOnScreen is false")
	}
	if len(displays) == 0 {
		deny(DenialDisplayBoundsUnknown, "no display bounds available")
	} else {
		visible := 0.0
		for _, d := range displays {
			inter := obs.Frame.Intersect(d)
			visible += inter.W * inter.H
		}
		total := obs.Frame.W * obs.Frame.H
		if total > 0 && visible/total < o.MinVisibleFraction {
			deny(DenialWindowNotVisibleEnough, "window visibility below the configured fraction")
		}
	}

	if foreground.Err != nil {
		deny(DenialFrontmostQueryFailed, foreground.Err.Error())
	} else if foreground.FrontmostPID != obs.Selector.PID {
		deny(DenialNotFrontmost, "another application is frontmost")
	}

	if ax != nil {
		if ax.Err != nil {
			detail := ax.Err.Error()
			if ax.TimedOut {
				detail = "ax query timed out: " + detail
			}
			deny(DenialAXQueryFailed, detail)
		} else {
			if ax.PID != obs.Selector.PID {
				deny(DenialAXFocusOtherProcess, "focused window belongs to another process")
			}
			if !rectNearlyEqual(ax.Frame, obs.Frame, o.AXFrameTolerance) {
				deny(DenialAXFrameMismatch, "focused window frame disagrees with the target frame")
			}
		}
	}

	if pos != nil {
		if !obs.Frame.Contains(*pos, o.PositionTolerance) {
			deny(DenialPositionOutsideTarget, "position lies outside the target window frame")
		}
		onScreen := false
		for _, d := range displays {
			if d.Contains(*pos, o.PositionTolerance) {
				onScreen = true
				break
			}
		}
		if !onScreen && len(displays) > 0 {
			deny(DenialPositionOffScreen, "position lies outside all known displays")
		}
	}

	v.Ready = len(v.Denials) == 0
	return v
}
