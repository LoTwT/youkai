package macos

import (
	"errors"
	"fmt"
	"time"
)

// This file expresses the six handoff semantics of T2 §4.2 as named Go
// types: target observation, image observation, short input request,
// input result, fault & unavailability, and resource teardown. They
// carry what the platform layer can honestly report; the scan kernel
// (integration PR) composes them into session state. Window-side and
// process-level permission probes live in permission.go because they
// are process capabilities, not per-window observations; input
// bookkeeping types (HeldInput, ReleaseOutcome) live in input.go.

// StreamToken identifies one capture session instance within the
// process. Tokens come from a process-wide monotonic counter, so every
// started stream has a fresh token and callbacks or requests naming an
// old token are rejected instead of leaking into a new session.
type StreamToken uint64

// FrameStatus is the semantic status of one capture frame callback,
// mapped from the native SCFrameStatus values. The names follow the
// semantics T2 §4.1 requires the platform to preserve: complete means
// the system generated a new frame; idle means the displayed content
// did not change; blank, suspended and stopped each mean no new frame
// for their own reason. Unknown keeps the raw value when the mapping
// does not recognize it.
type FrameStatus string

const (
	// FrameComplete: WindowServer generated a new frame; the sample
	// usually carries pixel data.
	FrameComplete FrameStatus = "complete"
	// FrameIdle: no new frame because the displayed content did not
	// change; not evidence of anything about the game.
	FrameIdle FrameStatus = "idle"
	// FrameBlank: no new frame because the display went blank.
	FrameBlank FrameStatus = "blank"
	// FrameSuspended: no new frame because updates were suspended.
	FrameSuspended FrameStatus = "suspended"
	// FrameStarted: the first callback after the stream started; not a
	// content observation by itself.
	FrameStarted FrameStatus = "started"
	// FrameStopped: the stream stopped; no further frames are expected.
	FrameStopped FrameStatus = "stopped"
	// FrameUnknown: the native status did not map to a known value; the
	// raw number is preserved in the observation.
	FrameUnknown FrameStatus = "unknown"
)

// TargetObservation is the platform's observation of the capture
// target window at one point in time (T2 §4.2 目标观测). Identity is
// the bundle identifier plus process id plus window id; the title is
// display-only and never part of identity. Frontmost and permission
// checks are reported separately (foreground.go, permission.go) because
// they are process-level facts, not window properties.
type TargetObservation struct {
	// Selector is the locked identity this observation was matched
	// against: bundle id, process id and window id together.
	Selector WindowSelector
	// Title is the window title at observation time; display only.
	Title string
	// AppName is the owning application's localized name; display only.
	AppName string
	// Frame is the window frame in screen points, origin at the top
	// left of the main display (the same space as SCWindow.frame and
	// Quartz event coordinates, not AppKit's bottom-left origin).
	Frame PointRect
	// OnScreen reports SCWindow.isOnScreen. A false value covers
	// minimized and fully off-screen windows; the exact coverage of the
	// flag for every state is a real-device question (实测轮 ①).
	OnScreen bool
	// Active reports SCWindow.isActive, recorded for observability
	// only. The SDK documents that it can stay true while the window is
	// not on screen (Stage Manager), so it is never used to allow
	// input; see foreground.go.
	Active bool
	// Layer is the window layer (0 is the normal window layer).
	Layer int
	// ObservedAt is when the enumeration backing this observation was
	// taken.
	ObservedAt time.Time
}

// ImageObservation is one frame handed to the caller (T2 §4.2 图像观测).
// Pixels are a Go-owned copy in a fixed byte layout; nothing in the
// observation aliases a native buffer. There is no source video
// timestamp: for a cloud client the platform cannot see inside the
// video stream, so any "video time" stays unknown by design and only
// the system frame time is reported.
type ImageObservation struct {
	// Stream identifies the capture session that produced the frame.
	Stream StreamToken
	// FrameID is the per-session monotonic id assigned when the
	// callback arrived; every callback consumes one id, admitted or
	// not, so a denial never reuses a frame's identity.
	FrameID uint64
	// Status is the semantic frame status of this callback.
	Status FrameStatus
	// RawStatus is the native SCFrameStatus value, preserved verbatim
	// (−1 when the attachment was missing).
	RawStatus int32
	// Width and Height are the output surface size in pixels. They are
	// the captured image's pixels, not the window's backing pixels: the
	// stream configuration may scale or pad, and the geometry model in
	// geometry.go relates the two.
	Width, Height int
	// BytesPerRow is the row stride of Pixels in bytes.
	BytesPerRow int
	// PixelFormat names the color convention; the first version fixes
	// "BGRA" (byte order B,G,R,A), matching the configured
	// kCVPixelFormatType_32BGRA.
	PixelFormat string
	// ValidRect is the content rectangle inside the surface, in image
	// pixels; nil when the frame's geometry attachments were missing or
	// inconsistent (Pixels are still valid evidence, but coordinates
	// must not be derived from such a frame).
	ValidRect *PixelRect
	// Geometry is the geometry version this frame was observed under;
	// zero when the geometry was invalid.
	Geometry GeometryVersion
	// GeometryValid reports whether the frame's geometry passed the
	// consistency checks and a transform exists.
	GeometryValid bool
	// DisplayTime is the system frame time converted to comparable Go
	// monotonic time (see MachClock); nil when the frame carried no
	// display time. It says when WindowServer showed the frame — never
	// when the game or the cloud produced the content.
	DisplayTime *time.Time
	// DisplayTimeMach is the raw mach tick count of DisplayTime, kept
	// for diagnostics; zero when absent.
	DisplayTimeMach uint64
	// ArrivedAt is when the frame callback reached Go.
	ArrivedAt time.Time
	// Pixels is the copied image data (Width×Height BGRA with
	// BytesPerRow stride); nil when the sample carried no pixel buffer.
	// The slice is owned by this observation and is never mutated.
	Pixels []byte
}

// KeyCode is a macOS virtual key code (the shared Carbon/CGEvent
// numbering, HIToolbox Events.h kVK_* values).
type KeyCode uint32

// MouseButton identifies a mouse button using the CGEvent numbering:
// 0 left, 1 right, 2 center, higher numbers are additional buttons.
type MouseButton int32

const (
	MouseButtonLeft   MouseButton = 0
	MouseButtonRight  MouseButton = 1
	MouseButtonCenter MouseButton = 2
)

// OperationKind is the kind of one bounded input operation.
type OperationKind string

const (
	// OpMouseMoved moves the cursor to a position without pressing.
	OpMouseMoved OperationKind = "mouseMoved"
	// OpMouseClick presses and releases one button at a position.
	OpMouseClick OperationKind = "mouseClick"
	// OpKeyPress presses and releases one key.
	OpKeyPress OperationKind = "keyPress"
	// OpScroll scrolls by a bounded pixel delta along the vertical
	// axis. The sign convention follows CGEventCreateScrollWheelEvent
	// (positive scrolls up); whether the game interprets it that way is
	// a real-device question (实测轮 ①), and the platform does not
	// assume a Windows wheel notch equals any fixed pixel count.
	OpScroll OperationKind = "scroll"
)

// InputOperation is one bounded single operation. Position and Button
// are used by the mouse kinds, KeyCode by the key press, ScrollDeltaY
// by the scroll; the unused fields are ignored.
type InputOperation struct {
	Kind OperationKind
	// Position is the target position in screen points (top-left
	// origin, the Quartz global display coordinate space).
	Position Point
	// Button is the mouse button for a click.
	Button MouseButton
	// KeyCode is the key for a key press.
	KeyCode KeyCode
	// ScrollDeltaY is the vertical scroll delta in pixels.
	ScrollDeltaY float64
}

// validate reports whether the operation carries the data its kind
// needs.
func (op InputOperation) validate() error {
	switch op.Kind {
	case OpMouseMoved:
		return nil
	case OpMouseClick:
		if op.Button < 0 {
			return fmt.Errorf("macos: invalid mouse button %d", op.Button)
		}
	case OpKeyPress:
		if op.KeyCode == 0 {
			return errors.New("macos: key press requires a non-zero key code")
		}
	case OpScroll:
		if op.ScrollDeltaY == 0 {
			return errors.New("macos: scroll requires a non-zero delta")
		}
	default:
		return fmt.Errorf("macos: unknown operation kind %q", op.Kind)
	}
	return nil
}

// ShortInputRequest is one bounded single operation the caller asks the
// platform to deliver (T2 §4.2 短输入请求). The platform does not
// interpret the operation ("next page" is not its vocabulary); it
// re-checks permission, session, geometry and window conditions
// immediately before dispatch and reports the outcome via InputResult.
type ShortInputRequest struct {
	// Stream identifies the capture session the request belongs to;
	// requests naming another session are rejected.
	Stream StreamToken
	// Geometry is the geometry version the coordinates were derived
	// under. Requests with a stale version are rejected: window moves,
	// resizes or display changes make old coordinates invalid (T2 §3.1).
	Geometry GeometryVersion
	// Frame is the source frame id the coordinates came from. The
	// platform carries it for accounting only; it does not verify frame
	// freshness, which is the caller's evidence problem.
	Frame uint64
	// Op is the single bounded operation to deliver.
	Op InputOperation
}

// DispatchStatus classifies what actually happened to a short input
// request (T2 §4.2 输入结果). The platform distinguishes not
// dispatched, partially dispatched, and posted; it never reports game
// success — CGEventPost has no confirmation return value, so even
// "posted" means "entered the system event stream, effect unknown".
type DispatchStatus string

const (
	// DispatchBlocked: nothing was dispatched; BlockedCode and
	// BlockedReason say why (permission closed, stale geometry, target
	// not ready, invalid operation, …).
	DispatchBlocked DispatchStatus = "blocked"
	// DispatchPartial: some events of the operation were created and
	// posted but a later one failed to create; EventsPosted and
	// EventsFailed count them. Held inputs may remain.
	DispatchPartial DispatchStatus = "partial"
	// DispatchPosted: every event was created and posted via
	// CGEventPost. Delivery to the game is unconfirmed by construction;
	// only the game-side evidence of the caller can confirm an effect.
	DispatchPosted DispatchStatus = "posted"
)

// BlockedCode says why a short input request was not dispatched.
type BlockedCode string

const (
	// BlockedNotOpen: the dispatcher has not been opened for a session.
	BlockedNotOpen BlockedCode = "notOpen"
	// BlockedClosed: input permission is closed; it never reopens.
	BlockedClosed BlockedCode = "closed"
	// BlockedWrongSession: the request names another capture session.
	BlockedWrongSession BlockedCode = "wrongSession"
	// BlockedStaleGeometry: the request carries an outdated geometry
	// version.
	BlockedStaleGeometry BlockedCode = "staleGeometry"
	// BlockedReadiness: the readiness check (foreground, visibility,
	// ambiguity) denied the operation.
	BlockedReadiness BlockedCode = "readiness"
	// BlockedInvalidOperation: the operation itself is not deliverable
	// (unknown kind, missing position or key code).
	BlockedInvalidOperation BlockedCode = "invalidOperation"
)

// InputResult is the outcome of one short input request (T2 §4.2 输入
// 结果). It separates what the platform did from what the game did:
// EventsPosted counts CGEventPost calls that returned (the call itself
// has no return value, so "posted" is the strongest claim available),
// and any attempted release of held inputs is accounted separately in
// input.go's release outcomes.
type InputResult struct {
	// Status is one of blocked, partial or posted.
	Status DispatchStatus
	// BlockedCode and BlockedReason are set when Status is blocked.
	BlockedCode   BlockedCode
	BlockedReason string
	// EventsPosted is the number of events created and posted.
	EventsPosted int
	// EventsFailed is the number of events that failed to create.
	EventsFailed int
	// At is when the dispatcher finished handling the request.
	At time.Time
}

// FaultCategory classifies platform faults and unavailability for the
// kernel (T2 §4.2 故障与不可用). Categories say what broke; the raw
// error and the stage say how it was observed.
type FaultCategory string

const (
	// FaultTargetInvalid: the target window is gone, replaced or
	// ambiguous; identity can no longer be trusted.
	FaultTargetInvalid FaultCategory = "targetInvalid"
	// FaultPermission: a permission precheck or a live permission
	// signal failed (screen recording, accessibility, event synthesis).
	FaultPermission FaultCategory = "permission"
	// FaultStream: the capture stream failed or stopped unexpectedly.
	FaultStream FaultCategory = "stream"
	// FaultGeometry: the frame geometry was missing or inconsistent, so
	// coordinates cannot be derived from the affected frames.
	FaultGeometry FaultCategory = "geometry"
	// FaultUnexpected: any other unexpected platform error.
	FaultUnexpected FaultCategory = "unexpected"
)

// PlatformFault reports one platform fault or unavailability event to
// the kernel (T2 §4.2 故障与不可用). The kernel decides to close input
// permission, stop capture and finalize; the platform does not open its
// own recovery session.
type PlatformFault struct {
	// Category classifies the fault.
	Category FaultCategory
	// Stage names where it was observed, e.g. "enumeration",
	// "streamStart", "frameCallback", "input", "foreground".
	Stage string
	// Err is the raw platform error when one exists; may be nil.
	Err error
	// At is the observation time.
	At time.Time
	// LastGeometry is the last geometry version known good before the
	// fault; zero when none.
	LastGeometry GeometryVersion
	// LastFrame is the last frame id accepted before the fault; zero
	// when none.
	LastFrame uint64
	// Stream is the capture session the fault belongs to.
	Stream StreamToken
}

// Error implements error so a PlatformFault can travel as one.
func (f PlatformFault) Error() string {
	if f.Err == nil {
		return fmt.Sprintf("macos platform fault %s at %s", f.Category, f.Stage)
	}
	return fmt.Sprintf("macos platform fault %s at %s: %v", f.Category, f.Stage, f.Err)
}

// Unwrap exposes the raw platform error to errors.Is/As.
func (f PlatformFault) Unwrap() error { return f.Err }

// TeardownReport is the resource finalization report of a capture
// session (T2 §4.2 资源收尾). Requesting the stop and the native stream
// actually stopping are reported separately, and so is the drain of
// in-flight callbacks; the native objects are released only after both
// completed. Late callbacks after the intake closed are counted, not
// delivered.
type TeardownReport struct {
	// StopRequestedAt is when Stop was first called; zero when the
	// session never received a user stop (for example it failed on its
	// own before starting).
	StopRequestedAt time.Time
	// IntakeClosedAt is when new frame callbacks started being rejected.
	IntakeClosedAt time.Time
	// NativeStopRequested reports whether the native stop was requested.
	NativeStopRequested bool
	// NativeStoppedAt is when the native stop completion arrived; zero
	// value means it did not arrive within the stop timeout.
	NativeStoppedAt time.Time
	// NativeStopErr is the error reported by the native stop, if any.
	NativeStopErr error
	// NativeStopTimedOut reports that the stop completion never arrived.
	NativeStopTimedOut bool
	// Drained reports that the frame queue barrier completed after the
	// native stop; DrainedAt is when.
	Drained   bool
	DrainedAt time.Time
	// ReleasedAt is when the native objects were released; zero value
	// means they were deliberately kept (stop or drain timed out —
	// releasing while callbacks may still be in flight is unsafe).
	ReleasedAt time.Time
	// UnexpectedStopErr is the error from an unplanned stream stop
	// (SCStreamDelegate stream:didStopWithError:), nil for a normal
	// stop.
	UnexpectedStopErr error
	// FramesReceived counts all frame callbacks that arrived while the
	// intake was open.
	FramesReceived int
	// FramesComplete counts complete frames with pixel data that were
	// offered to the cache.
	FramesComplete int
	// FramesRejectedLate counts callbacks rejected after the intake
	// closed (late frames from the stopped stream).
	FramesRejectedLate int
	// FramesGeometryInvalid counts complete frames whose geometry
	// attachments were missing or inconsistent.
	FramesGeometryInvalid int
	// FirstCompleteFrameID is the first complete frame with pixels;
	// zero when none arrived. It is the frame evidence behind the
	// screen recording assessment.
	FirstCompleteFrameID uint64
	// FinalGeometry is the last geometry version observed.
	FinalGeometry GeometryVersion
	// FinalFrame is the last frame id received before teardown.
	FinalFrame uint64
}

// ErrStaleGeometry reports that a request carried a geometry version
// that is not the current one; old coordinates must not be dispatched.
var ErrStaleGeometry = errors.New("macos: stale geometry version")
