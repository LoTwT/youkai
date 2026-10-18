package windows

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"
)

// VirtualKey is a Win32 virtual-key code. The platform never invents key
// meanings; the game adapter supplies concrete codes and interprets
// their effects.
type VirtualKey uint16

// MouseButton names a mouse button of a bounded operation.
type MouseButton int

const (
	MouseButtonLeft   MouseButton = 1
	MouseButtonRight  MouseButton = 2
	MouseButtonMiddle MouseButton = 3
)

// OpKind is the kind of one bounded short operation.
type OpKind string

const (
	// OpClick: move the cursor to the op point (absolute), then one
	// button press and release.
	OpClick OpKind = "click"
	// OpKeyPress: one key press and release, no cursor motion.
	OpKeyPress OpKind = "keyPress"
	// OpScroll: move the cursor to the op point (absolute), then a small
	// number of wheel steps in one direction.
	OpScroll OpKind = "scroll"
)

// MaxWheelSteps bounds one scroll operation. One step is one wheel
// detent; the bound keeps a partial delivery a small, countable event.
const MaxWheelSteps = 3

// MaxInputEventsPerOp is the largest event array one operation expands
// to: one cursor move plus MaxWheelSteps wheel steps.
const MaxInputEventsPerOp = 1 + MaxWheelSteps

// InputOp is one bounded short operation. Only the fields named by Kind
// are meaningful; Validate rejects everything else.
type InputOp struct {
	Kind   OpKind
	Point  PixelPoint  // screen physical pixels; click and scroll
	Button MouseButton // click
	Key    VirtualKey  // keyPress
	// WheelSteps is the scroll amount in detents, 1..MaxWheelSteps by
	// magnitude; the sign is the direction (positive scrolls away from
	// the user, like a standard wheel up).
	WheelSteps int
}

// Validate checks the op is one well-formed bounded operation.
func (op InputOp) Validate() error {
	switch op.Kind {
	case OpClick:
		if op.Button < MouseButtonLeft || op.Button > MouseButtonMiddle {
			return fmt.Errorf("click: invalid mouse button %d", op.Button)
		}
		return nil
	case OpKeyPress:
		if op.Key == 0 {
			return errors.New("keyPress: empty virtual-key code")
		}
		return nil
	case OpScroll:
		n := op.WheelSteps
		if n < 0 {
			n = -n
		}
		if n == 0 || n > MaxWheelSteps {
			return fmt.Errorf("scroll: %d wheel steps outside 1..%d", op.WheelSteps, MaxWheelSteps)
		}
		return nil
	default:
		return fmt.Errorf("unknown input op kind %q", op.Kind)
	}
}

// HasPoint reports whether the op targets a screen point and therefore
// requires the point checks of the precheck (click and scroll do; a key
// press does not).
func (op InputOp) HasPoint() bool {
	return op.Kind == OpClick || op.Kind == OpScroll
}

// InputRequest is the "short input request" handover category of T2
// §4.2: the geometry version and the source frame the coordinates were
// derived from, plus one bounded operation. Session and target identity
// are enforced by the kernel side that builds the request; the platform
// correlates through the geometry version and the opaque frame
// reference. The platform does not interpret "next page" or any field
// semantics — it dispatches one bounded operation.
type InputRequest struct {
	// GeometryVersion is the version of the geometry the point was
	// computed under. The actor re-checks it against the current
	// version right before dispatch; a stale version is rejected because
	// window moves, size, DPI or display changes made the old
	// coordinates invalid.
	GeometryVersion GeometryVersion
	// FrameRef is the caller's opaque reference to the observation the
	// coordinates came from; it is carried into the result unchanged.
	FrameRef string
	Op       InputOp
}

// InputEventKind is one event kind of the system input stream.
type InputEventKind int

const (
	EventMouseMove InputEventKind = iota + 1 // absolute cursor move
	EventMouseDown                           // button press
	EventMouseUp                             // button release
	EventWheel                               // one wheel step
	EventKeyDown                             // key press
	EventKeyUp                               // key release
)

// InputEvent is one event of the system input stream in the platform's
// own terms. The Win32 implementation marshals the list into a single
// SendInput call.
type InputEvent struct {
	Kind   InputEventKind
	Button MouseButton // mouse button events
	Key    VirtualKey  // keyboard events
	// Point is the target in screen physical pixels for move events; the
	// sender converts it into the normalized absolute domain at dispatch
	// time, with a freshly measured desktop space.
	Point PixelPoint
	// WheelSteps is ±1 for a wheel event: one detent in one direction.
	WheelSteps int
}

// ExpandOp turns a validated op into its bounded event list. Click and
// scroll first move the cursor to the op point with an absolute move —
// relative motion is never used for positioning because pointer speed
// settings make it unrepeatable. No inter-event delay is inserted; if a
// game needs a perceptible hold that is an integration-time decision
// backed by field evidence, not a platform default.
func ExpandOp(op InputOp) ([]InputEvent, error) {
	if err := op.Validate(); err != nil {
		return nil, err
	}
	switch op.Kind {
	case OpClick:
		return []InputEvent{
			{Kind: EventMouseMove, Point: op.Point},
			{Kind: EventMouseDown, Button: op.Button},
			{Kind: EventMouseUp, Button: op.Button},
		}, nil
	case OpKeyPress:
		return []InputEvent{
			{Kind: EventKeyDown, Key: op.Key},
			{Kind: EventKeyUp, Key: op.Key},
		}, nil
	case OpScroll:
		step := 1
		if op.WheelSteps < 0 {
			step = -1
		}
		n := op.WheelSteps
		if n < 0 {
			n = -n
		}
		events := make([]InputEvent, 0, MaxInputEventsPerOp)
		events = append(events, InputEvent{Kind: EventMouseMove, Point: op.Point})
		for i := 0; i < n; i++ {
			events = append(events, InputEvent{Kind: EventWheel, WheelSteps: step})
		}
		return events, nil
	default:
		return nil, fmt.Errorf("unknown input op kind %q", op.Kind)
	}
}

// InputState is the dispatch state of one input request.
type InputState string

const (
	// StateNotDispatched: refused before any event was inserted
	// (permission closed, earlier UIPI block, stale geometry, failed
	// preconditions, malformed op).
	StateNotDispatched InputState = "notDispatched"
	// StateDelivered: every event of the operation was inserted into the
	// system input stream. Insertion is not game action success; the
	// effect remains unverified.
	StateDelivered InputState = "delivered"
	// StatePartialDelivered: some but not all events were inserted; the
	// held ledger records whatever is still down.
	StatePartialDelivered InputState = "partialDelivered"
	// StateBlocked: SendInput inserted nothing and the system reported
	// access denial — the shape a UIPI permission mismatch takes. The
	// actor refuses every further normal operation; matching privileges
	// require a new precheck, the platform never elevates itself and
	// never blindly re-sends.
	StateBlocked InputState = "blocked"
	// StateFailed: dispatch failed for another reason.
	StateFailed InputState = "failed"
)

// HeldKey is a key or button the platform pressed down and has not
// verified released. Only platform-pressed keys are ever recorded; user
// keys are never bulk-released.
type HeldKey struct {
	IsButton bool
	Button   MouseButton
	Key      VirtualKey
}

func (h HeldKey) String() string {
	if h.IsButton {
		return fmt.Sprintf("mouse button %d", h.Button)
	}
	return fmt.Sprintf("key 0x%02x", h.Key)
}

// ReleaseRecord is the outcome of one stop-time release attempt.
type ReleaseRecord struct {
	Key      HeldKey
	Released bool
	// Err is the failure reason when the release was not inserted.
	Err error
}

// InputResult is the "input result" handover category of T2 §4.2: it
// distinguishes not-dispatched, full delivery, partial delivery with
// counts, the UIPI-blocked shape, and failure — and it never claims game
// action success.
type InputResult struct {
	Request        InputRequest
	State          InputState
	EventsPlanned  int
	EventsInserted int
	// EffectUnverified is always true: SendInput counts insertion into
	// the system stream, never what the game did with the events.
	EffectUnverified bool
	// HeldAfter lists the keys/buttons still down after this request.
	HeldAfter []HeldKey
	Err       error
}

// Sender inserts events into the system input stream. The Win32
// implementation marshals the events into one SendInput call.
type Sender interface {
	// SendInput inserts events and returns how many were accepted, in
	// order — a partial count means the remaining events were not
	// inserted. blocked=true marks the zero-acceptance access-denied
	// outcome, the shape a UIPI permission mismatch takes; SendInput's
	// own error text does not name UIPI, so this classification is the
	// sender's and is reported as such. An error is returned when the
	// call failed; accepted events still count.
	SendInput(events []InputEvent) (accepted int, blocked bool, err error)
}

// PrecheckFunc re-verifies the premises of one request immediately
// before dispatch: geometry version freshness, target identity
// (cheaply), foreground and minimized state, and the op point's client
// membership and hit test. It runs inside the actor's serial section,
// as close to insertion as the API allows. The residual race — the
// foreground changing between the check and the insertion — cannot be
// closed from user mode; the platform minimizes the window instead of
// pretending atomicity.
type PrecheckFunc func(req InputRequest) error

// InputActor is the serial executor of bounded short operations: one
// operation runs at a time, permission is closed in sync with the
// dispatch entry so a queued next operation can never slip past a
// cancel, and every key or button the platform itself pressed is ledgered
// and released at stop time.
//
// Permission is one-way: it starts closed, Permit opens it once, and
// after ClosePermission it never reopens — the kernel's closed-permission
// semantics, enforced by the platform gate itself.
//
// The actor is pure logic over the injected Sender and PrecheckFunc; the
// Win32 wiring is in the //go:build windows files.
type InputActor struct {
	mu          sync.Mutex
	send        Sender
	precheck    PrecheckFunc
	permitted   bool
	closed      bool
	closeReason string
	// blocked is sticky after a UIPI-shaped zero delivery: no normal
	// operation is accepted again, and the actor never elevates or
	// blindly re-sends.
	blocked bool
	held    []HeldKey
}

// NewInputActor builds an actor in the closed, unpermitted state.
func NewInputActor(send Sender, precheck PrecheckFunc) *InputActor {
	return &InputActor{send: send, precheck: precheck}
}

// Permit opens input permission for dispatching normal operations. It is
// ignored once permission has been closed: closing is a one-way state,
// so a late or duplicated Permit cannot reopen dispatching behind a
// stop.
func (a *InputActor) Permit() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return
	}
	a.permitted = true
	a.closeReason = ""
}

// ClosePermission closes input permission with a reason. It synchronizes
// with the dispatch entry: an in-flight operation finishes first
// (operations are bounded), and once ClosePermission returns, no new
// normal operation can start. Events already inserted into the system
// stream cannot be recalled. Permission never reopens.
func (a *InputActor) ClosePermission(reason string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.permitted = false
	a.closed = true
	if a.closeReason == "" {
		a.closeReason = reason
	}
}

// Closed reports whether permission is currently closed.
func (a *InputActor) Closed() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return !a.permitted
}

// Dispatch executes one bounded operation. The result distinguishes
// not-dispatched, delivered, partial delivery with counts, the blocked
// shape and failure; it never claims a game action succeeded.
func (a *InputActor) Dispatch(req InputRequest) InputResult {
	a.mu.Lock()
	defer a.mu.Unlock()

	res := InputResult{Request: req, EffectUnverified: true}
	if err := req.Op.Validate(); err != nil {
		res.State = StateNotDispatched
		res.Err = err
		return res
	}
	if a.blocked {
		res.State = StateNotDispatched
		res.Err = errors.New("input refused after an access-denied zero delivery (likely UIPI privilege mismatch); rerun the precheck with matching privileges")
		return res
	}
	if !a.permitted {
		res.State = StateNotDispatched
		if a.closeReason != "" {
			res.Err = fmt.Errorf("input permission closed: %s", a.closeReason)
		} else {
			res.Err = errors.New("input permission not granted")
		}
		return res
	}
	if a.precheck != nil {
		if err := a.precheck(req); err != nil {
			res.State = StateNotDispatched
			res.Err = err
			return res
		}
	}
	events, err := ExpandOp(req.Op)
	if err != nil {
		res.State = StateNotDispatched
		res.Err = err
		return res
	}
	res.EventsPlanned = len(events)

	accepted, blocked, err := a.send.SendInput(events)
	res.EventsInserted = accepted
	switch {
	case blocked:
		// Zero events, access denied: the UIPI shape. Refuse everything
		// further instead of retrying or elevating.
		a.blocked = true
		res.State = StateBlocked
		res.Err = NewFault(CodeInputBlocked,
			"SendInput inserted no events and the system reported access denial; the target likely runs at a higher privilege level (UIPI)",
			nil, PhaseDispatch, time.Now(), 0)
	case accepted == 0:
		res.State = StateFailed
		if err == nil {
			res.Err = NewFault(CodeInputFailed,
				"SendInput inserted no events and reported no error; input may be blocked by UIPI or another thread",
				nil, PhaseDispatch, time.Now(), 0)
		} else {
			res.Err = NewFault(CodeInputFailed, "SendInput failed", err, PhaseDispatch, time.Now(), 0)
		}
	case accepted >= len(events):
		res.State = StateDelivered
		// A full delivery contains each down and its up, so the ledger is
		// unchanged.
	default:
		// 0 < accepted < len: a prefix of the events was inserted; ledger
		// every down of the prefix that has no up in it. A sender note
		// (when there is one) survives into the result.
		res.State = StatePartialDelivered
		if err != nil {
			res.Err = err
		}
		a.applyHeld(events[:accepted])
	}
	res.HeldAfter = slices.Clone(a.held)
	return res
}

// applyHeld folds a prefix of inserted events into the held ledger: a
// down whose up is not in the prefix leaves the key or button held.
func (a *InputActor) applyHeld(prefix []InputEvent) {
	for _, ev := range prefix {
		switch ev.Kind {
		case EventMouseDown:
			a.addHeld(HeldKey{IsButton: true, Button: ev.Button})
		case EventMouseUp:
			a.removeHeld(HeldKey{IsButton: true, Button: ev.Button})
		case EventKeyDown:
			a.addHeld(HeldKey{Key: ev.Key})
		case EventKeyUp:
			a.removeHeld(HeldKey{Key: ev.Key})
		}
	}
}

func (a *InputActor) addHeld(k HeldKey) {
	if !slices.Contains(a.held, k) {
		a.held = append(a.held, k)
	}
}

func (a *InputActor) removeHeld(k HeldKey) {
	a.held = slices.DeleteFunc(a.held, func(h HeldKey) bool { return h == k })
}

// Held snapshots the currently ledgered keys/buttons.
func (a *InputActor) Held() []HeldKey {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.held)
}

// ReleaseHeld attempts to release every platform-pressed key or button
// still down and reports each outcome. It deliberately runs after
// permission close — release is not a normal operation — and only ever
// releases what the ledger says the platform itself pressed; a failed
// release is recorded as such and the key stays in the ledger, because
// it may still be down. Release is attempted once per stop; the platform
// does not steal game focus or bulk-release user keys to "clean up".
// senderBatchLimit is the largest event array one SendInput call carries.
// Bounded operations stay far below it; the stop-time release chunks to
// it so a larger ledger still releases everything.
const senderBatchLimit = 8

func (a *InputActor) ReleaseHeld() []ReleaseRecord {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.held) == 0 {
		return nil
	}
	events := make([]InputEvent, 0, len(a.held))
	for _, h := range a.held {
		if h.IsButton {
			events = append(events, InputEvent{Kind: EventMouseUp, Button: h.Button})
		} else {
			events = append(events, InputEvent{Kind: EventKeyUp, Key: h.Key})
		}
	}
	records := make([]ReleaseRecord, len(a.held))
	for start := 0; start < len(events); start += senderBatchLimit {
		end := start + senderBatchLimit
		if end > len(events) {
			end = len(events)
		}
		accepted, blocked, err := a.send.SendInput(events[start:end])
		for i := start; i < end; i++ {
			h := a.held[i]
			rec := ReleaseRecord{Key: h}
			switch {
			case i-start < accepted:
				rec.Released = true
			case blocked:
				rec.Err = errors.New("release not inserted: access denied (the target may run at a higher privilege level)")
			case err != nil:
				rec.Err = err
			default:
				rec.Err = errors.New("release not inserted")
			}
			records[i] = rec
		}
	}
	// Keys that were not verifiably released stay in the ledger: they may
	// still be down.
	var still []HeldKey
	for i, h := range a.held {
		if !records[i].Released {
			still = append(still, h)
		}
	}
	a.held = still
	return records
}

// Precheck diagnostic codes of InputPrecheck refusals.
const (
	PrecheckNoGeometry     = "noValidGeometry"
	PrecheckStaleGeometry  = "staleGeometry"
	PrecheckTargetGone     = "targetGone"
	PrecheckTargetReplaced = "targetReplaced"
	PrecheckFocusLost      = "focusLost"
	PrecheckMinimized      = "minimized"
	PrecheckPointOutside   = "pointOutsideClient"
	PrecheckOccluded       = "occluded"
)

// PrecheckError is one precheck refusal with its diagnostic code.
type PrecheckError struct {
	Code   string
	Detail string
}

func (e *PrecheckError) Error() string {
	return fmt.Sprintf("precheck %s: %s", e.Code, e.Detail)
}

// InputPrecheck builds the platform-side PrecheckFunc for a locked
// target: no input request exists without a valid geometry; the request
// must name the current geometry version; the window must still be the
// locked one (cheap identity: handle alive and same PID — the full
// executable verification runs on the observation cadence); the target
// must be the actual foreground window and not minimized; and an op
// point must lie inside the client area and hit the target in a
// WindowFromPoint test. The foreground check does not filter the
// scanner's own windows: the scanner itself being foreground is focus
// lost, exactly like any other window. The hit test ignores hidden and
// disabled windows, so a clear result is not proof of no occlusion — it
// is the best available user-mode evidence, and unknown occlusion at the
// target point refuses the input.
func InputPrecheck(win WindowAPI, fg ForegroundAPI, tracker *GeometryTracker, locked WindowIdentity) PrecheckFunc {
	return func(req InputRequest) error {
		geo, ok := tracker.Current()
		if !ok {
			return &PrecheckError{Code: PrecheckNoGeometry, Detail: "no verified client geometry"}
		}
		if req.GeometryVersion != geo.Version {
			return &PrecheckError{Code: PrecheckStaleGeometry, Detail: fmt.Sprintf("request geometry version %d, current version %d; window moved, resized or display changed", req.GeometryVersion, geo.Version)}
		}
		hwnd := locked.HWND
		if !win.IsWindow(hwnd) {
			return &PrecheckError{Code: PrecheckTargetGone, Detail: fmt.Sprintf("window handle %d gone", hwnd)}
		}
		pid, err := win.ThreadProcessID(hwnd)
		if err != nil || pid == 0 {
			return &PrecheckError{Code: PrecheckTargetGone, Detail: fmt.Sprintf("window handle %d has no owning process", hwnd)}
		}
		if pid != locked.PID {
			return &PrecheckError{Code: PrecheckTargetReplaced, Detail: fmt.Sprintf("handle %d now owned by pid %d, locked pid %d", hwnd, pid, locked.PID)}
		}
		if fg.ForegroundWindow() != hwnd {
			return &PrecheckError{Code: PrecheckFocusLost, Detail: fmt.Sprintf("foreground window is %d, target is %d", fg.ForegroundWindow(), hwnd)}
		}
		if win.IsIconic(hwnd) {
			return &PrecheckError{Code: PrecheckMinimized, Detail: "target window is minimized"}
		}
		if req.Op.HasPoint() {
			if !geo.Client.Contains(req.Op.Point) {
				return &PrecheckError{Code: PrecheckPointOutside, Detail: fmt.Sprintf("op point %v outside client rect %v", req.Op.Point, geo.Client)}
			}
			hit := fg.HitWindow(req.Op.Point)
			if hit == 0 {
				return &PrecheckError{Code: PrecheckOccluded, Detail: fmt.Sprintf("hit test at %v returned no window", req.Op.Point)}
			}
			if root := fg.RootOf(hit); root != hwnd {
				return &PrecheckError{Code: PrecheckOccluded, Detail: fmt.Sprintf("point %v is covered by window %d (root %d), not target %d", req.Op.Point, hit, root, hwnd)}
			}
		}
		return nil
	}
}
