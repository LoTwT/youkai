package platform

import (
	"image"
	"time"
)

// GeometryVersion numbers one consistent geometry state of the capture
// target within a session: any change — window move or resize, content
// rect or scale changes, display configuration changes — produces a new
// version. Input requests name the version their coordinates were
// derived from; requests carrying an older version are refused, because
// old coordinates must not be dispatched after the geometry changed.
type GeometryVersion uint64

// TargetSpec selects the game target window. One platform reads the
// macOS fields, the other the Windows fields; the unused fields are
// ignored.
type TargetSpec struct {
	// BundleID is the macOS target's bundle identifier (the App Store
	// cloud client: com.miHoYo.cloudgames.Nap, per 实测轮 ①).
	BundleID string
	// ExecutablePath is the Windows target's full executable path, the
	// authoritative identity; class and title are screening hints only.
	ExecutablePath string
	ClassNameHint  string
	TitleHint      string
}

// Key is a neutral key name of the port's vocabulary: KeyA…KeyZ,
// KeyF1…KeyF12 and KeyEscape. The platform bases map it to their native
// key codes (macOS Carbon/CGEvent kVK_*, Windows VK_*); keys outside the
// vocabulary are refused, never guessed.
type Key string

const (
	KeyA Key = "A"
	KeyB Key = "B"
	KeyC Key = "C"
	KeyD Key = "D"
	KeyE Key = "E"
	KeyF Key = "F"
	KeyG Key = "G"
	KeyH Key = "H"
	KeyI Key = "I"
	KeyJ Key = "J"
	KeyK Key = "K"
	KeyL Key = "L"
	KeyM Key = "M"
	KeyN Key = "N"
	KeyO Key = "O"
	KeyP Key = "P"
	KeyQ Key = "Q"
	KeyR Key = "R"
	KeyS Key = "S"
	KeyT Key = "T"
	KeyU Key = "U"
	KeyV Key = "V"
	KeyW Key = "W"
	KeyX Key = "X"
	KeyY Key = "Y"
	KeyZ Key = "Z"

	KeyF1  Key = "F1"
	KeyF2  Key = "F2"
	KeyF3  Key = "F3"
	KeyF4  Key = "F4"
	KeyF5  Key = "F5"
	KeyF6  Key = "F6"
	KeyF7  Key = "F7"
	KeyF8  Key = "F8"
	KeyF9  Key = "F9"
	KeyF10 Key = "F10"
	KeyF11 Key = "F11"
	KeyF12 Key = "F12"

	KeyEscape Key = "Escape"
)

// HotkeySpec is one global stop hotkey: a key plus modifiers. Meta is ⌘
// on macOS and the Win key on Windows; the default combinations avoid
// the Win key on Windows (registered system shortcuts) — whether a
// combination actually registers is the native registration's verdict,
// reported as hotkeyUnavailable.
type HotkeySpec struct {
	Key                    Key
	Ctrl, Alt, Shift, Meta bool
}

// FrameRequest asks a session for one frame.
type FrameRequest struct {
	// After requires a complete frame that arrived after it. An idle
	// status observed after it also satisfies the request with the
	// latest complete frame and Reused=true: unchanged content is still
	// current content. A caller passes the previous action's finish time
	// so the frame it inspects cannot predate the action.
	After time.Time
}

// Frame is one complete frame of the capture target.
type Frame struct {
	// ID is session-monotonic; frames of different sessions never share
	// the numbering.
	ID uint64
	// Geometry is the geometry version the frame was captured under.
	Geometry GeometryVersion
	// Image is the frame in Go-owned RGBA; nothing aliases it after the
	// return, so the caller may crop and hold it as evidence.
	Image *image.RGBA
	// Content is the valid video content region inside Image, in image
	// pixels: stream padding (capture 留边, right-side black bars) lies
	// outside it. Input coordinates derive from Image pixels and must
	// stay inside Content.
	Content image.Rectangle
	// ArrivedAt is when the frame reached the platform: the callback
	// time on macOS, the capture completion on Windows. It is local
	// machine time, never game time.
	ArrivedAt time.Time
	// DisplayTime is the system frame time on macOS (WindowServer time,
	// converted to comparable monotonic time); nil on Windows and on
	// frames without the attachment. It says when the system showed the
	// frame — never when the game or cloud produced the content.
	DisplayTime *time.Time
	// Reused reports the frame satisfied an After request through an
	// idle observation: the content is unchanged since this frame and
	// therefore still current.
	Reused bool
}

// InputKind is the kind of one bounded input operation. Scroll is
// deliberately not part of the vocabulary: the macOS cloud client
// ignores synthesized wheel events (实测轮 ①), so traversal scrolling is
// expressed as a drag on both platforms.
type InputKind string

const (
	InputClick    InputKind = "click"
	InputKeyPress InputKind = "keyPress"
	InputDrag     InputKind = "drag"
)

// InputOp is one bounded input operation. Positions are Image pixels of
// a frame captured under Geometry — the platform maps them to screen
// coordinates through the baseline geometry; a request whose Geometry is
// not the current version is refused.
type InputOp struct {
	Kind     InputKind
	Geometry GeometryVersion
	// FrameID is the frame the coordinates were derived from; carried
	// for provenance only, never verified for freshness — that evidence
	// problem belongs to the caller.
	FrameID uint64
	// At is the operation position in Image pixels; To is the drag
	// destination (drag only).
	At, To image.Point
	// Key is the key of a key press.
	Key Key
	// Hold is the press-to-release interval of a click; zero takes the
	// default (60 ms), above the bound (200 ms) is refused.
	Hold time.Duration
	// Steps is the interpolated move count of a drag; zero takes the
	// default (10), above the bound (32) is refused.
	Steps int
	// Duration is the press-to-release span of a drag; zero takes the
	// default (400 ms), above the platform's maximum is refused.
	Duration time.Duration
}

// InputStatus is what happened to one input operation.
type InputStatus string

const (
	// InputNotDispatched: nothing was dispatched; Denial says why.
	InputNotDispatched InputStatus = "notDispatched"
	// InputPosted: every event entered the platform's event path
	// (CGEventPost / SendInput). Delivery to the game remains unverified
	// by construction — only the caller's frame evidence can confirm an
	// effect.
	InputPosted InputStatus = "posted"
	// InputPartial: some events were dispatched; EventsPosted counts
	// what ran. An aborted drag also reports partial with Aborted set.
	InputPartial InputStatus = "partial"
	// InputFailed: the dispatch failed (UIPI block shape, event path
	// failure); Denial carries the code. The platform does not retry.
	InputFailed InputStatus = "failed"
)

// InputResult is the outcome of one input operation.
type InputResult struct {
	Status InputStatus
	// Denial is set when Status is notDispatched or failed: the fault
	// that refused the operation (focusLost, staleGeometry,
	// permitClosed, inputBlocked, …).
	Denial *Fault
	// Aborted reports that a drag stopped mid-way because the kernel's
	// input permission closed: the release was dispatched and the
	// platform's own presses left the ledger, but the operation did not
	// reach its destination.
	Aborted bool
	// EventsPlanned and EventsPosted count the events of the operation:
	// planned before dispatch, posted what actually ran.
	EventsPlanned, EventsPosted int
	// StartedAt and FinishedAt bound the operation: the dispatch entry
	// and the last event. A caller requesting the next frame passes
	// FinishedAt as After, so the frame cannot predate the action.
	StartedAt, FinishedAt time.Time
}

// Observation is one fresh observation of the capture target. Problems
// carry the premises that are not met (Premise class), the conditions
// that broke (Broken) and pending stream faults observed before the
// baseline was locked; Ready means no problem at all. The error return
// is reserved for the observation itself failing (Class observation) —
// it never means "the target is fine" or "the target is broken".
type Observation struct {
	At       time.Time
	Ready    bool
	Problems []*Fault
	// Geometry is the geometry version in force at observation time.
	Geometry GeometryVersion
}

// CapabilityCheck is one process-capability preflight result: a name,
// whether it passed and the raw query error when the query itself
// failed (a failed query does not count as a pass).
type CapabilityCheck struct {
	Name string
	OK   bool
	Err  error
}

// Preflight is the outcome of the port's capability preflight. It
// checks process capabilities only, without a target and without ever
// prompting: macOS accessibility trust and event synthesis; Windows
// per-monitor-v2 DPI awareness. Screen recording stays unknown here —
// Open judges it by the actual enumeration, stream start and first
// frame.
type Preflight struct {
	Checks []CapabilityCheck
}

// Fault returns the first missing capability as a permissionMissing
// fault — the check that failed names the missing capability in the
// message — or nil when every check passed. A failed query counts as
// missing: without the fact, permission cannot be claimed.
func (p Preflight) Fault() *Fault {
	for _, c := range p.Checks {
		if c.OK {
			continue
		}
		msg := "capability " + c.Name + " missing"
		if c.Err != nil {
			msg = "capability " + c.Name + " unverifiable: " + c.Err.Error()
		}
		return NewFault(CodePermissionMissing, msg)
	}
	return nil
}

// ReleaseOutcome accounts one stop-time release attempt of an input the
// platform itself pressed. Released means the release event entered the
// platform's event path — delivery to the game remains unverified; a
// failure keeps its error here.
type ReleaseOutcome struct {
	// Input names what was released (e.g. "button 0", "key 6").
	Input string
	// Released reports the release was dispatched.
	Released bool
	// Err is the release failure, when set.
	Err error
}

// CloseReport is the session's resource teardown report. Each stage is
// reported as the fact it is — a completed stop request does not imply
// the stream drained or the native objects were released — and Err
// joins the failures that matter (a teardown timeout leaves Err set and
// the native objects deliberately alive).
type CloseReport struct {
	// Releases accounts every stop-time release attempt of the
	// platform's own presses.
	Releases []ReleaseOutcome
	// CaptureStopped: the native capture stop completed (macOS; Windows
	// has no stream and reports true).
	CaptureStopped bool
	// Drained: the frame queue drained after the stop.
	Drained bool
	// Released: the native objects were released — only true when the
	// stop completed and the drain passed.
	Released bool
	// HotkeyErr is the stop hotkey's unregistration error, if any.
	HotkeyErr error
	// Dwell is the exit dwell the close slept through (macOS only:
	// ~50 ms after the last event post, because CGEventPost has no
	// confirmation semantics; zero when nothing was ever posted).
	Dwell time.Duration
	// Err joins the teardown failures.
	Err error
}

// TargetInfo is the locked target identity, for diagnostics and logs
// only; it never participates in input decisions (the observation
// re-verifies identity every time).
type TargetInfo struct {
	// macOS identity: bundle, process and window id.
	BundleID string
	PID      int32
	WindowID uint32
	// Windows identity: handle and process instance.
	HWND           uintptr
	ExecutablePath string
	ClassName      string
	Title          string
}
