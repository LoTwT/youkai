package windows

import (
	"fmt"
	"time"
)

// Phase names the stage a fault occurred in, so the kernel can tell
// precheck failures from capture, dispatch and shutdown failures without
// parsing message text.
type Phase string

const (
	PhasePrecheck Phase = "precheck"
	PhaseObserve  Phase = "observe"
	PhaseCapture  Phase = "capture"
	PhaseDispatch Phase = "dispatch"
	PhaseShutdown Phase = "shutdown"
)

// Diagnostic codes of platform faults. They are stable identifiers for
// the kernel's stop triggers, not user-facing text.
const (
	// CodeTargetLost: the locked window was destroyed or replaced, or its
	// process exited or restarted. The old HWND is never reused; a new
	// target requires a fresh precheck.
	CodeTargetLost = "targetLost"
	// CodeGeometryUnavailable: the client geometry could not be measured
	// consistently.
	CodeGeometryUnavailable = "geometryUnavailable"
	// CodeDpiAwarenessMismatch: the scanner process is not per-monitor-v2
	// aware, so its screen coordinates may be DPI-virtualized. The
	// package never changes the process DPI mode after windows exist; it
	// only verifies and reports, so the caller (MyGo) must configure
	// PMv2 before the UI creates its windows.
	CodeDpiAwarenessMismatch = "dpiAwarenessMismatch"
	// CodeDpiVirtualized: the target window's own DPI differs from its
	// monitor's effective DPI, so the window is DPI-virtualized and the
	// measured client rect is not the physical region on screen. The
	// image is never scaled by a single DPI factor to "fix" this; the
	// configuration is refused instead.
	CodeDpiVirtualized = "dpiVirtualized"
	// CodeDpiCheckUnavailable: a DPI fact could not be observed (API
	// missing or query failed); virtualization status is unknown, which
	// is treated as unverified, not as fine.
	CodeDpiCheckUnavailable = "dpiCheckUnavailable"
	// CodeCaptureFailed: the GDI capture path failed.
	CodeCaptureFailed = "captureFailed"
	// CodeInputBlocked: SendInput inserted no events and the system
	// reported access denial — the shape a UIPI permission mismatch
	// takes. SendInput's own error text does not name UIPI, so this is
	// the platform's classification and is reported as such.
	CodeInputBlocked = "inputBlocked"
	// CodeInputFailed: the input path failed for another reason.
	CodeInputFailed = "inputFailed"
	// CodeHotkeyUnavailable: the stop hotkey could not be registered
	// (including a conflict with an existing registration elsewhere).
	CodeHotkeyUnavailable = "hotkeyUnavailable"
)

// PlatformFault is the "fault or unavailable" handover category of T2
// §4.2: a reason category, the raw system error text when there is one,
// the stage it occurred in, the observation time, and the geometry
// version that was still valid. A fault invalidates the scan premises;
// stopping and wrapping up is the kernel's job — the package never
// starts recovery work of its own.
type PlatformFault struct {
	Code    string
	Message string
	// RawErr is the raw system error text, empty when there is none.
	RawErr string
	Phase  Phase
	At     time.Time
	// LastGeometryVersion is the geometry version still known valid when
	// the fault occurred; 0 when none was ever valid.
	LastGeometryVersion GeometryVersion
}

// NewFault builds a PlatformFault; rawErr may be nil.
func NewFault(code, message string, rawErr error, phase Phase, at time.Time, lastGeo GeometryVersion) *PlatformFault {
	f := &PlatformFault{
		Code:                code,
		Message:             message,
		Phase:               phase,
		At:                  at,
		LastGeometryVersion: lastGeo,
	}
	if rawErr != nil {
		f.RawErr = rawErr.Error()
	}
	return f
}

func (f *PlatformFault) Error() string {
	if f.RawErr != "" {
		return fmt.Sprintf("%s: %s: %s", f.Code, f.Message, f.RawErr)
	}
	return fmt.Sprintf("%s: %s", f.Code, f.Message)
}
