package macos

import "time"

// The narrow native-API surfaces the pure logic drives. Each interface
// covers one native path so tests inject exactly the doubles they
// need; the darwin cgo bridge (bridge_darwin.go) provides the
// implementations behind //go:build darwin. The pure files reference
// only these interfaces and plain data types, which is what keeps the
// package compiling on GOOS=windows.

// WindowEnumerator performs one SCShareableContent enumeration. The
// single call returns both the window list and the display list of the
// same snapshot. An error means the enumeration itself failed; an
// empty other-process window list is a valid result the screen
// recording assessment interprets (see permission.go).
//
// The native implementation bounds the asynchronous
// getShareableContent call with a timeout instead of waiting forever.
type WindowEnumerator interface {
	EnumerateWindows() (windows []NativeWindow, displays []NativeDisplay, err error)
}

// ForegroundProbe reports the frontmost application via
// NSWorkspace.frontmostApplication: the process that would receive key
// events. A PID match is necessary but not sufficient for input — the
// readiness verdict combines it with visibility and, when queried, the
// AX focused window.
type ForegroundProbe interface {
	FrontmostPID() (int32, error)
}

// AXFocusProbe queries the system-wide AXFocusedWindow with a bounded
// messaging timeout. The timeout is a per-call bound: an AX query that
// exceeds it fails (TimedOut), and a failed query denies input rather
// than falling back to a weaker check (T2 §2.2).
type AXFocusProbe interface {
	FocusedWindow(timeout time.Duration) (AXFocusReport, error)
}

// PermissionProbe queries the two preflight-able capabilities:
// accessibility trust (AXIsProcessTrustedWithOptions with the prompt
// option disabled — the query never pops an authorization dialog; the
// request for authorization is a user-initiated action the caller
// owns) and event synthesis (CGPreflightPostEventAccess). Screen
// recording is deliberately not preflighted; see permission.go.
type PermissionProbe interface {
	// AccessibilityTrusted reports whether the process currently passes
	// the accessibility trust check.
	AccessibilityTrusted() (bool, error)
	// EventPostAllowed reports whether the process may synthesize and
	// post events.
	EventPostAllowed() (bool, error)
}

// EnumerateAndAssess runs one enumeration and assesses the locked
// target against it; it is the common precheck/monitoring path.
func EnumerateAndAssessment(enum WindowEnumerator, selector WindowSelector, now time.Time) ([]NativeDisplay, TargetAssessment, error) {
	windows, displays, err := enum.EnumerateWindows()
	if err != nil {
		return nil, TargetAssessment{ObservedAt: now}, err
	}
	return displays, AssessTarget(selector, windows, now), nil
}

// ScreenRecordingEvidenceFrom collects the screen recording evidence
// for a completed precheck: the enumeration snapshot (windows minus
// the caller's own process) plus the capture session's stream outcome.
// ownPID partitions the window list; the session fields map to the
// evidence struct verbatim.
func ScreenRecordingEvidenceFrom(enumErr error, windows []NativeWindow, ownPID int32, streamStartErr error, streamStarted bool, session *CaptureSession) ScreenRecordingEvidence {
	other := 0
	for _, w := range windows {
		if w.PID != ownPID {
			other++
		}
	}
	e := ScreenRecordingEvidence{
		EnumerationErr:      enumErr,
		OtherProcessWindows: other,
		StreamStartErr:      streamStartErr,
		StreamStarted:       streamStarted,
	}
	if session != nil && session.FirstCompleteFrameID() != 0 {
		e.FirstFrameComplete = true
		e.FirstFrameHasPixels = true
	}
	return e
}
