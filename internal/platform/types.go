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
	// HandleHint is the window handle the user confirmed when the
	// installed build enumerated candidates (P-01). It narrows selection
	// to exactly that window; a session is never opened on another window
	// of the same executable after the confirmed one disappeared. It is a
	// hint, not identity: handles are recycled, so the path and process
	// instance are still verified on every observation.
	HandleHint uintptr
	// PIDHint and ProcessStartedAtHint are the confirmed candidate's
	// process instance. They are compared together with HandleHint so a
	// handle the OS recycled into another process cannot be mistaken for
	// the confirmed window; zero values leave the check out.
	PIDHint              uint32
	ProcessStartedAtHint time.Time
	ClassNameHint        string
}

// TargetCandidate identifies one running game window a user can confirm
// as the scan target (P-01). Candidate enumeration reads only the owning
// process's image path, process instance and window class — never a
// window title.
type TargetCandidate struct {
	// Handle is the platform window handle (the Windows HWND). It is a
	// selection hint; the executable path stays the authoritative
	// identity and handles are recycled.
	Handle uintptr
	// PID is the owning process instance at enumeration time.
	PID uint32
	// ExecutablePath is the owning process's full executable path.
	ExecutablePath string
	// ProcessStartedAt distinguishes a restarted process that reused the
	// PID.
	ProcessStartedAt time.Time
	// ClassName is the window class recorded for display and screening
	// only.
	ClassName string
}

// TargetDiscoveryResult is one candidate enumeration pass. Candidates is
// ordered stably for display. AccessDeniedWindows counts visible windows of
// the game (window class and image name verified) whose process could not
// be opened because youkai runs at lower integrity; the window suggests
// running youkai as administrator. UnreadableGameWindows counts the same
// shape when youkai is already elevated or its own integrity could not be
// read: the window shows a generic diagnostic message instead. Neither
// count is a candidate — their full identity is unknown.
type TargetDiscoveryResult struct {
	Candidates            []TargetCandidate
	AccessDeniedWindows   int
	UnreadableGameWindows int
}

// TargetDiscovery is the optional Port capability: enumerate the game
// windows the user can confirm as the scan target. Ports without it
// (macOS) scan a target selected by configuration instead.
type TargetDiscovery interface {
	DiscoverTargets() (TargetDiscoveryResult, error)
}

// ReadinessItem names one platform-owned row of the read-only preparation
// check (R-01). The names are stable identifiers shared with the
// application layer, never user-facing text.
type ReadinessItem string

const (
	// ReadinessTargetClient is the game window candidate row.
	ReadinessTargetClient ReadinessItem = "targetClient"
	// ReadinessAdministrator is the mandatory-integrity comparison between
	// youkai and the game.
	ReadinessAdministrator ReadinessItem = "administrator"
	// ReadinessGameResolution is the client-area tier row.
	ReadinessGameResolution ReadinessItem = "gameResolution"
	// ReadinessWindowPosition is the work-area containment row.
	ReadinessWindowPosition ReadinessItem = "windowPosition"
	// ReadinessDisplayScale is the DPI awareness and virtualization row.
	ReadinessDisplayScale ReadinessItem = "displayScale"
)

// ReadinessVerdict is one row's read-only conclusion. "After start" is not
// a platform verdict: the application layer adds it for rows the platform
// deliberately never probes before a session exists.
type ReadinessVerdict string

const (
	// ReadinessSatisfied: the row's premise holds now.
	ReadinessSatisfied ReadinessVerdict = "satisfied"
	// ReadinessUnsatisfied: the row's premise is known not to hold.
	ReadinessUnsatisfied ReadinessVerdict = "unsatisfied"
	// ReadinessUnknown: the premise could not be judged; never treat this
	// as a pass.
	ReadinessUnknown ReadinessVerdict = "unknown"
)

// ReadinessCheck is one platform row of the read-only preparation check.
// Current and Required are stable diagnostic values (tier sizes,
// integrity levels, rectangles, counts), never localized prose.
type ReadinessCheck struct {
	Item     ReadinessItem
	Verdict  ReadinessVerdict
	Current  string
	Required string
	// Fault explains an unsatisfied or unknown verdict; nil when the
	// verdict needs no cause.
	Fault *Fault
	// At is when this row was checked.
	At time.Time
}

// ReadinessReport is one read-only preparation-check pass of a platform.
// It never establishes a session, takes the scan occupancy, registers the
// stop hotkey, dispatches input or captures a frame.
type ReadinessReport struct {
	At                    time.Time
	Candidates            []TargetCandidate
	AccessDeniedWindows   int
	UnreadableGameWindows int
	Checks                []ReadinessCheck
}

// ReadinessRequest configures one read-only check. Target narrows the
// checks to the window the user confirmed; an empty Target asks a
// discovery source to use its candidates. Display is the game adapter's
// tier contract; a Windows local-client check needs it for the resolution
// row, while a nil policy leaves that row unknown.
type ReadinessRequest struct {
	Target  TargetSpec
	Display *WindowDisplayPolicy
}

// ReadinessChecker is the optional Port capability for the read-only
// preparation check (R-01). Ports without it (macOS) leave the
// platform-owned rows unknown in the application layer rather than
// pretending they hold.
type ReadinessChecker interface {
	CheckReadiness(req ReadinessRequest) (ReadinessReport, error)
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
// failed (a failed query does not count as a pass). Code optionally names
// the diagnostic code a failure reports; empty means permissionMissing.
// A platform uses it when the same condition already has a more precise
// code elsewhere, so a start refusal and the read-only preparation check
// report the same cause.
type CapabilityCheck struct {
	Name string
	OK   bool
	Err  error
	Code string
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

// Fault returns the first missing capability as a fault — the check that
// failed names the missing capability in the message — or nil when every
// check passed. The code is the check's own Code when set, otherwise
// permissionMissing. A failed query counts as missing: without the fact,
// permission cannot be claimed.
func (p Preflight) Fault() *Fault {
	for _, c := range p.Checks {
		if c.OK {
			continue
		}
		msg := "capability " + c.Name + " missing"
		if c.Err != nil {
			msg = "capability " + c.Name + " unverifiable: " + c.Err.Error()
		}
		code := c.Code
		if code == "" {
			code = CodePermissionMissing
		}
		return NewFault(code, msg)
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
	// Windows identity: handle and process instance. The window title is
	// deliberately absent: it is user data the scanner never reads.
	HWND           uintptr
	ExecutablePath string
	ClassName      string
}
