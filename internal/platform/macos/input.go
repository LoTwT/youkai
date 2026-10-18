package macos

import (
	"fmt"
	"sync"
	"time"
)

// The serial short-input dispatcher (T2 §2.2, §4.2, §4.3 停止能力的实
// 际边界): one dispatcher per capture session delivers bounded single
// operations — one mouse move, one click, one short key press or one
// small scroll — through an injected InputPoster. Input permission
// starts closed, opens once for the session and never reopens after
// closing; every dispatch re-checks permission, session, geometry
// version and an injected readiness hook immediately before posting,
// and the dispatcher itself serializes operations so a close can never
// race past a check into a post. Release of the inputs the dispatcher
// itself pressed is accounted separately, and only those inputs are
// ever released — never the user's keys.

// InputPoster posts the individual events of one operation via the
// native Quartz layer (CGEvent creation + CGEventPost). Every method
// reports only creation failures: a nil error means the event was
// created and posted, and because CGEventPost has no confirmation
// return value, delivery to the game remains unconfirmed by
// construction (T2 §2.2 输入 row). Positions are in global display
// coordinates (points).
type InputPoster interface {
	// PostMouseMoved moves the cursor to the position.
	PostMouseMoved(p Point) error
	// PostMouseButton presses (down) or releases (up) one button at a
	// position.
	PostMouseButton(button MouseButton, down bool, at Point) error
	// PostKey presses (down) or releases (up) one key.
	PostKey(code KeyCode, down bool) error
	// PostScroll scrolls by a vertical pixel delta.
	PostScroll(deltaY float64) error
}

// HeldInput is one input the dispatcher itself pressed and has not
// released yet. The ledger only ever contains the dispatcher's own
// presses; the user's keys and buttons are never recorded and never
// bulk-released (T2 §4.3).
type HeldInput struct {
	// Kind is "key" or "button".
	Kind string
	// KeyCode identifies a held key; Button a held button.
	KeyCode KeyCode
	Button  MouseButton
	// Position is the press position of a held button, replayed on
	// release attempts.
	Position Point
	// Since is when the press was posted.
	Since time.Time
	// ReleaseAttempts counts release attempts so far.
	ReleaseAttempts int
	// LastReleaseErr is the error of the last release attempt, if any.
	LastReleaseErr error
}

// ReleaseOutcome accounts one release attempt of a held input. Because
// CGEventPost is unconfirmable, Released means the release event was
// posted, not that the game observed it; a failure keeps the input in
// the ledger for another attempt and is reported as-is.
type ReleaseOutcome struct {
	Input HeldInput
	// Released reports that the release event was posted.
	Released bool
	// Err is the creation failure of the release event, if any.
	Err error
	// At is when the attempt ran.
	At time.Time
}

// InputDispatcher serializes short input delivery for one capture
// session. Construct one per session; OpenFor opens it for that
// session's token; Close closes it forever and attempts to release
// everything the dispatcher still holds.
type InputDispatcher struct {
	mu sync.Mutex
	// poster delivers the raw events.
	poster InputPoster
	// now supplies timestamps.
	now func() time.Time
	// readiness is an optional hook re-checked immediately before
	// posting; it typically runs the foreground/visibility verdict.
	readiness func(req ShortInputRequest) error
	// open/stream: the dispatcher opens for exactly one session token.
	open   bool
	stream StreamToken
	// geometry is the latest geometry version; requests carrying
	// another version are rejected.
	geometry GeometryVersion
	// closed is set by Close; once true, nothing is ever dispatched.
	closed       bool
	closedReason string
	// held is the press ledger.
	held []HeldInput
}

// NewInputDispatcher builds a dispatcher around poster. now defaults
// to time.Now.
func NewInputDispatcher(poster InputPoster, now func() time.Time) *InputDispatcher {
	if now == nil {
		now = time.Now
	}
	return &InputDispatcher{poster: poster, now: now}
}

// SetReadiness installs the hook re-checked immediately before every
// posting; returning an error blocks the dispatch with the readiness
// denial. The hook must be fast and must not call back into the
// dispatcher.
func (d *InputDispatcher) SetReadiness(fn func(req ShortInputRequest) error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.readiness = fn
}

// OpenFor opens the dispatcher for exactly one capture session. It
// fails when already opened or closed. Until opened, dispatches are
// blocked; after opening, only that session's requests are accepted.
func (d *InputDispatcher) OpenFor(stream StreamToken) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return fmt.Errorf("macos: input dispatcher closed (%s)", d.closedReason)
	}
	if d.open {
		return fmt.Errorf("macos: input dispatcher already open for stream %d", d.stream)
	}
	d.open = true
	d.stream = stream
	return nil
}

// SetGeometry advances the geometry version the dispatcher compares
// requests against. Stale versions are ignored (versions only
// increase); the first call after opening sets the baseline.
func (d *InputDispatcher) SetGeometry(v GeometryVersion) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if v > d.geometry {
		d.geometry = v
	}
}

// Dispatch delivers one bounded operation. The whole call is
// serialized: checks and posting happen under the dispatcher lock, so
// a concurrent Close can only take effect between operations, never
// between an operation's own events.
func (d *InputDispatcher) Dispatch(req ShortInputRequest) InputResult {
	d.mu.Lock()
	defer d.mu.Unlock()
	blocked := func(code BlockedCode, reason string) InputResult {
		return InputResult{Status: DispatchBlocked, BlockedCode: code, BlockedReason: reason, At: d.now()}
	}
	switch {
	case d.closed:
		return blocked(BlockedClosed, d.closedReason)
	case !d.open:
		return blocked(BlockedNotOpen, "input permission not open yet")
	case req.Stream != d.stream:
		return blocked(BlockedWrongSession, fmt.Sprintf("request stream %d, dispatcher stream %d", req.Stream, d.stream))
	case req.Geometry != d.geometry:
		return blocked(BlockedStaleGeometry, fmt.Sprintf("request geometry %d, current %d", req.Geometry, d.geometry))
	}
	if err := req.Op.validate(); err != nil {
		return blocked(BlockedInvalidOperation, err.Error())
	}
	if d.readiness != nil {
		if err := d.readiness(req); err != nil {
			return blocked(BlockedReadiness, err.Error())
		}
	}

	res := InputResult{At: d.now()}
	post := func(fail func() error) bool {
		if err := fail(); err != nil {
			res.EventsFailed++
			return false
		}
		res.EventsPosted++
		return true
	}
	switch req.Op.Kind {
	case OpMouseMoved:
		post(func() error { return d.poster.PostMouseMoved(req.Op.Position) })
	case OpMouseClick:
		// The press and its release are one atomic operation: when the
		// press fails to create, the release is never posted on its
		// own — a stray release could undo a press the user made.
		if !post(func() error { return d.poster.PostMouseButton(req.Op.Button, true, req.Op.Position) }) {
			break
		}
		d.held = append(d.held, HeldInput{Kind: "button", Button: req.Op.Button, Position: req.Op.Position, Since: res.At})
		if post(func() error { return d.poster.PostMouseButton(req.Op.Button, false, req.Op.Position) }) {
			d.unholdButton(req.Op.Button)
		}
	case OpKeyPress:
		// Same atomicity as the click: no release without its press.
		if !post(func() error { return d.poster.PostKey(req.Op.KeyCode, true) }) {
			break
		}
		d.held = append(d.held, HeldInput{Kind: "key", KeyCode: req.Op.KeyCode, Since: res.At})
		if post(func() error { return d.poster.PostKey(req.Op.KeyCode, false) }) {
			d.unholdKey(req.Op.KeyCode)
		}
	case OpScroll:
		post(func() error { return d.poster.PostScroll(req.Op.ScrollDeltaY) })
	}

	switch {
	case res.EventsFailed == 0:
		res.Status = DispatchPosted
	case res.EventsPosted == 0:
		res.Status = DispatchBlocked
		res.BlockedCode = BlockedInvalidOperation
		res.BlockedReason = "event creation failed before anything was posted"
	default:
		res.Status = DispatchPartial
	}
	return res
}

// unholdKey/unholdButton remove one released input from the ledger;
// the release event was posted, which is the strongest confirmation
// available.
func (d *InputDispatcher) unholdKey(code KeyCode) {
	for i, h := range d.held {
		if h.Kind == "key" && h.KeyCode == code {
			d.held = append(d.held[:i], d.held[i+1:]...)
			return
		}
	}
}

func (d *InputDispatcher) unholdButton(button MouseButton) {
	for i, h := range d.held {
		if h.Kind == "button" && h.Button == button {
			d.held = append(d.held[:i], d.held[i+1:]...)
			return
		}
	}
}

// ReleaseHeld attempts to release every held input once and returns
// the outcomes. Failed releases keep their ledger entries (with the
// attempt count and error) so a later attempt can retry; successful
// ones are removed. Only the dispatcher's own presses are touched.
func (d *InputDispatcher) ReleaseHeld() []ReleaseOutcome {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.releaseAllLocked()
}

func (d *InputDispatcher) releaseAllLocked() []ReleaseOutcome {
	out := make([]ReleaseOutcome, 0, len(d.held))
	kept := d.held[:0]
	for _, h := range d.held {
		o := ReleaseOutcome{Input: h, At: d.now()}
		var err error
		switch h.Kind {
		case "key":
			err = d.poster.PostKey(h.KeyCode, false)
		case "button":
			err = d.poster.PostMouseButton(h.Button, false, h.Position)
		default:
			err = fmt.Errorf("macos: unknown held input kind %q", h.Kind)
		}
		if err != nil {
			o.Err = err
			h.ReleaseAttempts++
			h.LastReleaseErr = err
			kept = append(kept, h)
		} else {
			o.Released = true
		}
		out = append(out, o)
	}
	clear(d.held[len(kept):])
	d.held = kept
	return out
}

// Close closes the input permission forever and attempts to release
// every held input, returning the release outcomes. Close is idempotent
// and racing it with Dispatch is safe: dispatches are serialized
// through the same lock, so a dispatch either fully ran before the
// close or is fully blocked by it — a queued next operation can never
// slip past the cancellation.
func (d *InputDispatcher) Close(reason string) []ReleaseOutcome {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil
	}
	d.closed = true
	if reason == "" {
		reason = "input permission closed"
	}
	d.closedReason = reason
	return d.releaseAllLocked()
}

// DispatcherState is the dispatcher's coarse state for diagnostics.
type DispatcherState struct {
	Open     bool
	Stream   StreamToken
	Closed   bool
	Reason   string
	Geometry GeometryVersion
	Held     []HeldInput
}

// State reports the dispatcher's coarse state, including a copy of the
// held-input ledger.
func (d *InputDispatcher) State() DispatcherState {
	d.mu.Lock()
	defer d.mu.Unlock()
	return DispatcherState{
		Open:     d.open,
		Stream:   d.stream,
		Closed:   d.closed,
		Reason:   d.closedReason,
		Geometry: d.geometry,
		Held:     append([]HeldInput(nil), d.held...),
	}
}
