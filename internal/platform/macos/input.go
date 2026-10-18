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
	// sleep waits out a click's hold or a drag's step interval; tests
	// inject a recorder. The default is time.Sleep.
	sleep func(time.Duration)
	// readiness is an optional hook re-checked immediately before
	// posting; it typically runs the foreground/visibility verdict.
	readiness func(req ShortInputRequest) error
	// stepGate is an optional hook checked between a drag's steps and
	// honored before a click's release: when it reports false the drag
	// stops, the press is released and the result is marked Aborted. It
	// reads the kernel's input permission; the dispatcher never keeps
	// permission truth of its own.
	stepGate func() bool
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
	// lastPostAt is when the last event post succeeded; zero when
	// nothing was ever posted. Session close uses it to dwell before
	// the process may exit, because CGEventPost has no confirmation
	// semantics (实测轮 ①).
	lastPostAt time.Time
}

// NewInputDispatcher builds a dispatcher around poster. now defaults
// to time.Now.
func NewInputDispatcher(poster InputPoster, now func() time.Time) *InputDispatcher {
	if now == nil {
		now = time.Now
	}
	return &InputDispatcher{poster: poster, now: now, sleep: time.Sleep}
}

// SetSleep installs the sleep used between a click's press and release
// and between a drag's steps. It exists for tests; the default is
// time.Sleep.
func (d *InputDispatcher) SetSleep(sleep func(time.Duration)) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if sleep != nil {
		d.sleep = sleep
	}
}

// SetStepGate installs the gate honored between a drag's steps: when it
// reports false, the drag aborts — the release is posted immediately
// and the result is marked Aborted — so a stop never waits for more
// than one step interval. The hook must be fast and must not call back
// into the dispatcher.
func (d *InputDispatcher) SetStepGate(gate func() bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.stepGate = gate
}

// LastPostAt returns when the last event post succeeded; the zero time
// when nothing was ever posted. It backs the session-close dwell.
func (d *InputDispatcher) LastPostAt() time.Time {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.lastPostAt
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
// between an operation's own events. A click holds its press for the
// op's Hold interval (the stop path waits at most one hold); a drag
// checks the step gate between steps and aborts — release posted,
// Aborted marked — the moment the gate closes.
func (d *InputDispatcher) Dispatch(req ShortInputRequest) InputResult {
	d.mu.Lock()
	defer d.mu.Unlock()
	started := d.now()
	blocked := func(code BlockedCode, reason string) InputResult {
		return InputResult{Status: DispatchBlocked, BlockedCode: code, BlockedReason: reason, StartedAt: started, At: d.now()}
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
	// A click's hold and a drag's steps and duration take their defaults
	// here and are bounded here; an out-of-bounds request never reaches
	// the poster.
	hold := req.Op.Hold
	if hold == 0 {
		hold = DefaultHold
	}
	if hold < 0 || hold > MaxHold {
		return blocked(BlockedInvalidOperation, fmt.Sprintf("click hold %s outside 0..%s", req.Op.Hold, MaxHold))
	}
	steps := req.Op.Steps
	if steps == 0 {
		steps = DefaultDragSteps
	}
	if steps < 0 || steps > MaxDragSteps {
		return blocked(BlockedInvalidOperation, fmt.Sprintf("drag steps %d outside 0..%d", req.Op.Steps, MaxDragSteps))
	}
	dur := req.Op.Duration
	if dur == 0 {
		dur = DefaultDragDuration
	}
	if dur < 0 || dur > MaxDragDuration {
		return blocked(BlockedInvalidOperation, fmt.Sprintf("drag duration %s outside 0..%s", req.Op.Duration, MaxDragDuration))
	}
	if d.readiness != nil {
		if err := d.readiness(req); err != nil {
			return blocked(BlockedReadiness, err.Error())
		}
	}

	res := InputResult{StartedAt: started}
	post := func(fail func() error) bool {
		if err := fail(); err != nil {
			res.EventsFailed++
			return false
		}
		res.EventsPosted++
		d.lastPostAt = d.now()
		return true
	}
	switch req.Op.Kind {
	case OpMouseMoved:
		post(func() error { return d.poster.PostMouseMoved(req.Op.Position) })
	case OpMouseClick:
		// The press and its release are one atomic operation: when the
		// press fails to create, the release is never posted on its
		// own — a stray release could undo a press the user made. The
		// hold keeps the press observable by a game sampling input once
		// per frame; the stop path may wait at most this one hold.
		if !post(func() error { return d.poster.PostMouseButton(req.Op.Button, true, req.Op.Position) }) {
			break
		}
		d.held = append(d.held, HeldInput{Kind: "button", Button: req.Op.Button, Position: req.Op.Position, Since: started})
		d.sleep(hold)
		if post(func() error { return d.poster.PostMouseButton(req.Op.Button, false, req.Op.Position) }) {
			d.unholdButton(req.Op.Button)
		}
	case OpKeyPress:
		// Same atomicity as the click: no release without its press.
		if !post(func() error { return d.poster.PostKey(req.Op.KeyCode, true) }) {
			break
		}
		d.held = append(d.held, HeldInput{Kind: "key", KeyCode: req.Op.KeyCode, Since: started})
		if post(func() error { return d.poster.PostKey(req.Op.KeyCode, false) }) {
			d.unholdKey(req.Op.KeyCode)
		}
	case OpScroll:
		post(func() error { return d.poster.PostScroll(req.Op.ScrollDeltaY) })
	case OpDrag:
		// Press, interpolate moves towards To, release — the path 实测轮
		// ① verified against the cloud client. The step gate is honored
		// between steps: closing it aborts the drag immediately with the
		// release posted, so a stop never waits for more than one step
		// interval.
		if !post(func() error { return d.poster.PostMouseButton(req.Op.Button, true, req.Op.Position) }) {
			break
		}
		d.held = append(d.held, HeldInput{Kind: "button", Button: req.Op.Button, Position: req.Op.Position, Since: started})
		last := req.Op.Position
		interval := dur / time.Duration(steps)
		for i := 1; i <= steps; i++ {
			d.sleep(interval)
			if d.stepGate != nil && !d.stepGate() {
				res.Aborted = true
				break
			}
			p := lerpPoint(req.Op.Position, req.Op.To, float64(i)/float64(steps))
			if !post(func() error { return d.poster.PostMouseMoved(p) }) {
				break
			}
			last = p
		}
		if !res.Aborted {
			// The full span has passed; the release lands on the
			// destination.
			d.sleep(interval)
			last = req.Op.To
		}
		if post(func() error { return d.poster.PostMouseButton(req.Op.Button, false, last) }) {
			d.unholdButton(req.Op.Button)
		}
	}

	res.At = d.now()
	switch {
	case res.Aborted:
		// An aborted drag stopped short of its destination; the events
		// that ran all posted, but the operation as requested did not
		// complete, so partial — never posted — is the honest status.
		res.Status = DispatchPartial
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

// lerpPoint interpolates between a and b by frac (0→a, 1→b).
func lerpPoint(a, b Point, frac float64) Point {
	return Point{X: a.X + (b.X-a.X)*frac, Y: a.Y + (b.Y-a.Y)*frac}
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
