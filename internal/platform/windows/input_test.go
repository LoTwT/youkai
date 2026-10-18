package windows

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestValidateOp(t *testing.T) {
	cases := []struct {
		name string
		op   InputOp
		ok   bool
	}{
		{"click left", InputOp{Kind: OpClick, Point: PixelPoint{5, 5}, Button: MouseButtonLeft}, true},
		{"click bad button", InputOp{Kind: OpClick, Button: MouseButton(9)}, false},
		{"key press", InputOp{Kind: OpKeyPress, Key: 0x41}, true},
		{"key press no key", InputOp{Kind: OpKeyPress}, false},
		{"scroll up", InputOp{Kind: OpScroll, Point: PixelPoint{1, 1}, WheelSteps: 2}, true},
		{"scroll down", InputOp{Kind: OpScroll, WheelSteps: -3}, true},
		{"scroll zero steps", InputOp{Kind: OpScroll, WheelSteps: 0}, false},
		{"scroll too many steps", InputOp{Kind: OpScroll, WheelSteps: 4}, false},
		{"unknown kind", InputOp{Kind: OpKind("wiggle")}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.op.Validate()
			if tc.ok && err != nil {
				t.Fatalf("should validate: %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatal("should be rejected")
			}
		})
	}
}

func TestExpandOp(t *testing.T) {
	events, err := ExpandOp(InputOp{Kind: OpClick, Point: PixelPoint{100, 200}, Button: MouseButtonRight})
	if err != nil {
		t.Fatalf("expand click: %v", err)
	}
	want := []InputEvent{
		{Kind: EventMouseMove, Point: PixelPoint{100, 200}},
		{Kind: EventMouseDown, Button: MouseButtonRight},
		{Kind: EventMouseUp, Button: MouseButtonRight},
	}
	if len(events) != len(want) {
		t.Fatalf("click should be exactly 3 events, got %d: %+v", len(events), events)
	}
	for i := range want {
		if events[i] != want[i] {
			t.Fatalf("click event %d: got %+v, want %+v", i, events[i], want[i])
		}
	}

	events, err = ExpandOp(InputOp{Kind: OpKeyPress, Key: 0x20})
	if err != nil || len(events) != 2 || events[0].Kind != EventKeyDown || events[1].Kind != EventKeyUp || events[0].Key != 0x20 {
		t.Fatalf("key press expansion: %v, %+v", err, events)
	}

	events, err = ExpandOp(InputOp{Kind: OpScroll, WheelSteps: -3})
	if err != nil {
		t.Fatalf("expand scroll: %v", err)
	}
	if len(events) != 4 || events[0].Kind != EventMouseMove {
		t.Fatalf("scroll of 3 steps should be move + 3 wheel events, got %+v", events)
	}
	for _, ev := range events[1:] {
		if ev.Kind != EventWheel || ev.WheelSteps != -1 {
			t.Fatalf("each scroll step is one detent event: %+v", ev)
		}
	}
}

func clickRequest(version GeometryVersion, p PixelPoint) InputRequest {
	return InputRequest{
		GeometryVersion: version,
		FrameRef:        "frame-1",
		Op:              InputOp{Kind: OpClick, Point: p, Button: MouseButtonLeft},
	}
}

func TestActorDeliversFullOperation(t *testing.T) {
	sender := newFakeSender()
	actor := NewInputActor(sender, nil)
	sleeps := make(chan time.Duration, 4)
	actor.SetSleep(func(d time.Duration) { sleeps <- d })
	actor.Permit()

	res := actor.Dispatch(clickRequest(1, PixelPoint{10, 10}))
	if res.State != StateDelivered {
		t.Fatalf("want delivered, got %s: %v", res.State, res.Err)
	}
	if res.EventsPlanned != 3 || res.EventsInserted != 3 {
		t.Fatalf("planned %d, inserted %d", res.EventsPlanned, res.EventsInserted)
	}
	if !res.EffectUnverified {
		t.Fatal("delivery must never claim a verified game effect")
	}
	if len(res.HeldAfter) != 0 {
		t.Fatalf("a full delivery leaves nothing held: %+v", res.HeldAfter)
	}
	// A click is two SendInput batches — [move, down], the hold, [up] —
	// so the press stays observable by a game sampling once per frame.
	if sender.callCount() != 2 {
		t.Fatalf("two SendInput calls (press batch, release batch), got %d", sender.callCount())
	}
	if len(sender.calls[0]) != 2 || len(sender.calls[1]) != 1 || sender.calls[1][0].Kind != EventMouseUp {
		t.Fatalf("batches: %+v %+v", sender.calls[0], sender.calls[1])
	}
	// The default hold separates them.
	select {
	case d := <-sleeps:
		if d != DefaultHold {
			t.Fatalf("default hold %v, want %v", d, DefaultHold)
		}
	default:
		t.Fatal("the hold never slept")
	}
	// The last successful insertion feeds the post-time bookkeeping.
	if actor.LastPostAt().IsZero() {
		t.Fatal("a delivered click must record LastPostAt")
	}
}

func TestActorRefusesWithoutPermission(t *testing.T) {
	actor := NewInputActor(newFakeSender(), nil)
	res := actor.Dispatch(clickRequest(1, PixelPoint{0, 0}))
	if res.State != StateNotDispatched || res.Err == nil {
		t.Fatalf("want notDispatched with a reason, got %s: %v", res.State, res.Err)
	}
}

func TestActorRefusesAfterClose(t *testing.T) {
	actor := NewInputActor(newFakeSender(), nil)
	actor.Permit()
	actor.ClosePermission("user stop")
	res := actor.Dispatch(clickRequest(1, PixelPoint{0, 0}))
	if res.State != StateNotDispatched {
		t.Fatalf("want notDispatched after close, got %s", res.State)
	}
	if res.Err == nil || res.Err.Error() != "input permission closed: user stop" {
		t.Fatalf("close reason should be reported: %v", res.Err)
	}
	// Closing is one-way: a late or duplicated Permit cannot reopen
	// dispatching behind a stop.
	actor.Permit()
	if res := actor.Dispatch(clickRequest(1, PixelPoint{0, 0})); res.State != StateNotDispatched {
		t.Fatalf("permission must never reopen after a close, got %s", res.State)
	}
	if !actor.Closed() {
		t.Fatal("actor must report closed")
	}
}

func TestActorRefusesOnPrecheckFailure(t *testing.T) {
	stale := &PrecheckError{Code: PrecheckStaleGeometry, Detail: "request geometry version 1, current version 2"}
	actor := NewInputActor(newFakeSender(), func(req InputRequest) error { return stale })
	actor.Permit()

	res := actor.Dispatch(clickRequest(1, PixelPoint{0, 0}))
	if res.State != StateNotDispatched {
		t.Fatalf("want notDispatched, got %s", res.State)
	}
	var pe *PrecheckError
	if !errors.As(res.Err, &pe) || pe.Code != PrecheckStaleGeometry {
		t.Fatalf("precheck error should survive into the result: %v", res.Err)
	}
	if res.EventsPlanned != 0 {
		t.Fatalf("a refused request plans no events: %d", res.EventsPlanned)
	}
}

func TestActorPartialDeliveryLedger(t *testing.T) {
	// The press batch ([move, down]) is inserted, the release batch ([up])
	// is not: the button stays in the ledger and the result reports the
	// count.
	sender := newFakeSender()
	sender.respond = func(call int, events []InputEvent) (int, bool, error) {
		if call == 0 {
			return 2, false, nil
		}
		if call == 1 {
			return 0, false, errors.New("SendInput: internal error")
		}
		return len(events), false, nil
	}
	actor := NewInputActor(sender, nil)
	actor.SetSleep(func(time.Duration) {})
	actor.Permit()

	res := actor.Dispatch(clickRequest(1, PixelPoint{3, 3}))
	if res.State != StatePartialDelivered || res.EventsInserted != 2 || res.EventsPlanned != 3 {
		t.Fatalf("want partialDelivered 2/3, got %s %d/%d", res.State, res.EventsInserted, res.EventsPlanned)
	}
	if len(res.HeldAfter) != 1 || res.HeldAfter[0] != (HeldKey{IsButton: true, Button: MouseButtonLeft}) {
		t.Fatalf("button down without up must be ledgered: %+v", res.HeldAfter)
	}

	// Release at stop time succeeds and clears the ledger.
	sender.respond = func(call int, events []InputEvent) (int, bool, error) {
		if call != 2 {
			t.Fatalf("release should be the third call, got %d", call)
		}
		if len(events) != 1 || events[0].Kind != EventMouseUp {
			t.Fatalf("release should be one mouse-up event: %+v", events)
		}
		return 1, false, nil
	}
	records := actor.ReleaseHeld()
	if len(records) != 1 || !records[0].Released || records[0].Err != nil {
		t.Fatalf("release should succeed: %+v", records)
	}
	if held := actor.Held(); len(held) != 0 {
		t.Fatalf("ledger should be empty after release: %+v", held)
	}
}

func TestActorPartialMoveOnlyHoldsNothing(t *testing.T) {
	// Only the move event inserted: nothing is down, nothing is held.
	sender := newFakeSender()
	sender.respond = func(call int, events []InputEvent) (int, bool, error) {
		return 1, false, nil
	}
	actor := NewInputActor(sender, nil)
	actor.Permit()
	res := actor.Dispatch(clickRequest(1, PixelPoint{0, 0}))
	if res.State != StatePartialDelivered || len(res.HeldAfter) != 0 {
		t.Fatalf("move-only partial delivery holds nothing: %s %+v", res.State, res.HeldAfter)
	}
}

func TestActorHeldKeyRelease(t *testing.T) {
	// A key press whose down is inserted but not its up.
	sender := newFakeSender()
	sender.respond = func(call int, events []InputEvent) (int, bool, error) {
		return 1, false, nil
	}
	actor := NewInputActor(sender, nil)
	actor.Permit()
	res := actor.Dispatch(InputRequest{GeometryVersion: 1, Op: InputOp{Kind: OpKeyPress, Key: 0x42}})
	if res.State != StatePartialDelivered {
		t.Fatalf("want partialDelivered, got %s", res.State)
	}
	held := actor.Held()
	if len(held) != 1 || held[0] != (HeldKey{Key: 0x42}) {
		t.Fatalf("key 0x42 should be ledgered: %+v", held)
	}
	if records := actor.ReleaseHeld(); len(records) != 1 || !records[0].Released {
		t.Fatalf("release should succeed: %+v", records)
	}
	if len(actor.Held()) != 0 {
		t.Fatal("ledger should be empty")
	}
}

func TestActorBlockedSticky(t *testing.T) {
	// Zero delivery with access denial: the UIPI shape. The actor stops
	// accepting operations and never blindly re-sends.
	sender := newFakeSender()
	sender.respond = func(call int, events []InputEvent) (int, bool, error) {
		return 0, true, nil
	}
	actor := NewInputActor(sender, nil)
	actor.Permit()

	res := actor.Dispatch(clickRequest(1, PixelPoint{0, 0}))
	if res.State != StateBlocked || res.Err == nil {
		t.Fatalf("want blocked with an explanation, got %s: %v", res.State, res.Err)
	}

	res = actor.Dispatch(clickRequest(1, PixelPoint{0, 0}))
	if res.State != StateNotDispatched {
		t.Fatalf("blocked actor must refuse further operations, got %s", res.State)
	}
	if res.Err == nil || res.Err.Error() == "" {
		t.Fatal("the refusal must explain itself")
	}
	if sender.callCount() != 1 {
		t.Fatalf("no second SendInput call after a block, got %d", sender.callCount())
	}
}

func TestActorFailureNotSticky(t *testing.T) {
	// A plain failure (not the UIPI shape) is reported; whether to retry
	// is the kernel's decision, not a permanent actor state.
	sender := newFakeSender()
	failing := true
	sender.respond = func(call int, events []InputEvent) (int, bool, error) {
		if failing {
			return 0, false, errors.New("SendInput: internal error")
		}
		return len(events), false, nil
	}
	actor := NewInputActor(sender, nil)
	actor.Permit()

	res := actor.Dispatch(clickRequest(1, PixelPoint{0, 0}))
	if res.State != StateFailed || res.Err == nil {
		t.Fatalf("want failed with the raw error, got %s: %v", res.State, res.Err)
	}
	failing = false
	res = actor.Dispatch(clickRequest(1, PixelPoint{0, 0}))
	if res.State != StateDelivered {
		t.Fatalf("a non-UIPI failure is not sticky, got %s: %v", res.State, res.Err)
	}
}

func TestActorZeroDeliveryWithoutError(t *testing.T) {
	sender := newFakeSender()
	sender.respond = func(call int, events []InputEvent) (int, bool, error) {
		return 0, false, nil
	}
	actor := NewInputActor(sender, nil)
	actor.Permit()
	res := actor.Dispatch(clickRequest(1, PixelPoint{0, 0}))
	if res.State != StateFailed {
		t.Fatalf("zero delivery without the blocked shape is a failure, got %s", res.State)
	}
	if res.Err == nil || res.Err.Error() == "" {
		t.Fatal("the failure must say something even without an error code")
	}
}

func TestClosePermissionWaitsForInFlightOperation(t *testing.T) {
	// The dispatch entry and the permission close synchronize: an
	// in-flight bounded operation finishes first, the close takes
	// effect, and the next operation cannot slip past it. A click now
	// spans several SendInput batches; every one of them must finish
	// before the close.
	sender := newFakeSender()
	entered := make(chan struct{})
	gate := make(chan struct{})
	var enterOnce sync.Once
	sender.onEnter = func() { enterOnce.Do(func() { close(entered) }) }
	sender.gate = gate
	actor := NewInputActor(sender, nil)
	actor.SetSleep(func(time.Duration) {})
	actor.Permit()

	dispatched := make(chan InputResult, 1)
	go func() { dispatched <- actor.Dispatch(clickRequest(1, PixelPoint{0, 0})) }()

	// The operation is now inside SendInput, holding the actor's serial
	// section.
	<-entered

	closed := make(chan struct{})
	go func() {
		actor.ClosePermission("user stop")
		close(closed)
	}()

	// The close must not complete while the bounded operation is still
	// in flight. The negative wait is a race guard only: the dispatch
	// cannot finish before the gate opens.
	select {
	case <-closed:
		t.Fatal("close returned while an operation was in flight")
	case <-time.After(100 * time.Millisecond):
	}

	close(gate)
	res := <-dispatched
	if res.State != StateDelivered {
		t.Fatalf("in-flight operation should complete normally: %s: %v", res.State, res.Err)
	}
	<-closed

	// The next operation cannot slip past the close.
	if res := actor.Dispatch(clickRequest(1, PixelPoint{0, 0})); res.State != StateNotDispatched {
		t.Fatalf("post-close dispatch must be refused, got %s", res.State)
	}
}

func TestReleaseHeldRecordsFailuresHonestly(t *testing.T) {
	// A click whose release batch fails leaves the button down; the
	// stop-time release is then refused once — the record must say so,
	// and the key stays in the ledger because it may still be down.
	sender := newFakeSender()
	upAttempts := 0
	sender.respond = func(call int, events []InputEvent) (int, bool, error) {
		if len(events) == 2 {
			return 2, false, nil // press batch [move, down] fully inserted
		}
		if len(events) == 1 && events[0].Kind == EventMouseUp {
			upAttempts++
			if upAttempts <= 2 { // the click's release and the first stop-time release
				return 0, false, errors.New("release blocked by another thread")
			}
		}
		return len(events), false, nil
	}
	actor := NewInputActor(sender, nil)
	actor.SetSleep(func(time.Duration) {})
	actor.Permit()
	if res := actor.Dispatch(clickRequest(1, PixelPoint{0, 0})); res.State != StatePartialDelivered {
		t.Fatalf("setup: want partial delivery, got %s", res.State)
	}

	records := actor.ReleaseHeld()
	if len(records) != 1 {
		t.Fatalf("one record per held key: %+v", records)
	}
	if records[0].Released {
		t.Fatal("a failed release must not be reported as released")
	}
	if records[0].Err == nil {
		t.Fatal("a failed release must carry its error")
	}
	if held := actor.Held(); len(held) != 1 {
		t.Fatalf("the unreleased key stays in the ledger: %+v", held)
	}

	records = actor.ReleaseHeld()
	if len(records) != 1 || !records[0].Released {
		t.Fatalf("a later successful release clears the key: %+v", records)
	}
	if held := actor.Held(); len(held) != 0 {
		t.Fatalf("ledger empty after successful release: %+v", held)
	}
}

func TestReleaseHeldBlockedRecordsBlocked(t *testing.T) {
	sender := newFakeSender()
	sender.respond = func(call int, events []InputEvent) (int, bool, error) {
		if call == 0 {
			return 2, false, nil
		}
		return 0, true, nil // release blocked the UIPI way
	}
	actor := NewInputActor(sender, nil)
	actor.Permit()
	actor.Dispatch(clickRequest(1, PixelPoint{0, 0}))

	records := actor.ReleaseHeld()
	if len(records) != 1 || records[0].Released || records[0].Err == nil {
		t.Fatalf("blocked release must be recorded as not released with a reason: %+v", records)
	}
	if held := actor.Held(); len(held) != 1 {
		t.Fatalf("the key may still be down and stays ledgered: %+v", held)
	}
}

func TestReleaseHeldNothingHeldSendsNothing(t *testing.T) {
	sender := newFakeSender()
	actor := NewInputActor(sender, nil)
	actor.SetSleep(func(time.Duration) {})
	actor.Permit()
	actor.Dispatch(clickRequest(1, PixelPoint{0, 0})) // full delivery, nothing held
	if records := actor.ReleaseHeld(); records != nil {
		t.Fatalf("no held keys, no release records: %+v", records)
	}
	if sender.callCount() != 2 {
		t.Fatalf("release must not send events when nothing is held (2 click batches): %d calls", sender.callCount())
	}
}

func TestReleaseHeldChunksLargeLedgers(t *testing.T) {
	// Ten partial key presses leave ten keys down; the release sends
	// them in batches within the sender bound and accounts every key.
	sender := newFakeSender()
	sender.respond = func(call int, events []InputEvent) (int, bool, error) {
		if call < 10 {
			return 1, false, nil // each key press: down inserted, up not
		}
		return len(events), false, nil // release batches
	}
	actor := NewInputActor(sender, nil)
	actor.Permit()
	for i := 0; i < 10; i++ {
		res := actor.Dispatch(InputRequest{GeometryVersion: 1, Op: InputOp{Kind: OpKeyPress, Key: VirtualKey(0x30 + i)}})
		if res.State != StatePartialDelivered {
			t.Fatalf("key %d: want partial delivery (down only), got %s", i, res.State)
		}
	}
	if held := actor.Held(); len(held) != 10 {
		t.Fatalf("ten keys should be ledgered: %+v", held)
	}

	records := actor.ReleaseHeld()
	if len(records) != 10 {
		t.Fatalf("one record per key: %d", len(records))
	}
	for i, rec := range records {
		if !rec.Released || rec.Err != nil {
			t.Fatalf("record %d should be released: %+v", i, rec)
		}
	}
	// 10 dispatches + 2 release batches (8 + 2).
	if got := sender.callCount(); got != 12 {
		t.Fatalf("release should chunk to two batches, got %d calls total", got)
	}
	if last := len(sender.calls[len(sender.calls)-1]); last != 2 {
		t.Fatalf("the second batch holds the remaining 2 events, got %d", last)
	}
	if held := actor.Held(); len(held) != 0 {
		t.Fatalf("ledger empty after release: %+v", held)
	}
}

// precheckWorld wires a coherent fake target for the precheck tests.
type precheckWorld struct {
	win     *fakeWindows
	procs   *fakeProcesses
	geo     *fakeGeometry
	fg      *fakeForeground
	tracker *GeometryTracker
	locked  WindowIdentity
}

func newPrecheckWorld(t *testing.T) *precheckWorld {
	t.Helper()
	started := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	w := &precheckWorld{
		win:     newFakeWindows(),
		procs:   newFakeProcesses(),
		geo:     newFakeGeometry(),
		fg:      &fakeForeground{},
		tracker: &GeometryTracker{},
	}
	w.win.add(7, &fakeWnd{pid: 42, visible: true})
	w.procs.byPID[42] = fakeProc{path: `C:\game.exe`, started: started}
	w.locked = WindowIdentity{HWND: 7, PID: 42, ImagePath: `C:\game.exe`, ProcessStartedAt: started}
	// A first valid observation seeds geometry version 1.
	m, fault := MeasureClient(w.geo, 7)
	if fault != nil {
		t.Fatalf("seed measurement: %v", fault)
	}
	w.tracker.Update(m, started)
	w.fg.foreground = 7
	w.fg.hitDefault = 7
	return w
}

func (w *precheckWorld) precheck() PrecheckFunc {
	return InputPrecheck(w.win, w.fg, w.tracker, w.locked)
}

func precheckCode(err error) string {
	var pe *PrecheckError
	if errors.As(err, &pe) {
		return pe.Code
	}
	if err == nil {
		return ""
	}
	return fmt.Sprintf("unexpected error: %v", err)
}

func TestInputPrecheck(t *testing.T) {
	t.Run("no geometry yet refuses", func(t *testing.T) {
		w := &precheckWorld{win: newFakeWindows(), procs: newFakeProcesses(), geo: newFakeGeometry(), fg: &fakeForeground{}, tracker: &GeometryTracker{}}
		if code := precheckCode(w.precheck()(clickRequest(1, PixelPoint{10, 10}))); code != PrecheckNoGeometry {
			t.Fatalf("want noValidGeometry, got %v", code)
		}
	})

	t.Run("stale geometry version refuses", func(t *testing.T) {
		w := newPrecheckWorld(t)
		if code := precheckCode(w.precheck()(clickRequest(99, PixelPoint{10, 10}))); code != PrecheckStaleGeometry {
			t.Fatalf("want staleGeometry, got %v", code)
		}
	})

	t.Run("window gone refuses", func(t *testing.T) {
		w := newPrecheckWorld(t)
		w.win.byHandle[7].gone = true
		if code := precheckCode(w.precheck()(clickRequest(1, PixelPoint{10, 10}))); code != PrecheckTargetGone {
			t.Fatalf("want targetGone, got %v", code)
		}
	})

	t.Run("replaced pid refuses", func(t *testing.T) {
		w := newPrecheckWorld(t)
		w.win.byHandle[7].pid = 43
		if code := precheckCode(w.precheck()(clickRequest(1, PixelPoint{10, 10}))); code != PrecheckTargetReplaced {
			t.Fatalf("want targetReplaced, got %v", code)
		}
	})

	t.Run("focus lost refuses, including the scanner itself", func(t *testing.T) {
		w := newPrecheckWorld(t)
		w.fg.foreground = 999 // e.g. the scanner's own restored window
		if code := precheckCode(w.precheck()(clickRequest(1, PixelPoint{10, 10}))); code != PrecheckFocusLost {
			t.Fatalf("want focusLost without self-filtering, got %v", code)
		}
	})

	t.Run("minimized refuses", func(t *testing.T) {
		w := newPrecheckWorld(t)
		w.win.byHandle[7].iconic = true
		if code := precheckCode(w.precheck()(clickRequest(1, PixelPoint{10, 10}))); code != PrecheckMinimized {
			t.Fatalf("want minimized, got %v", code)
		}
	})

	t.Run("point outside client refuses", func(t *testing.T) {
		w := newPrecheckWorld(t)
		if code := precheckCode(w.precheck()(clickRequest(1, PixelPoint{5000, 5000}))); code != PrecheckPointOutside {
			t.Fatalf("want pointOutsideClient, got %v", code)
		}
	})

	t.Run("occluded point refuses", func(t *testing.T) {
		w := newPrecheckWorld(t)
		w.fg.hits = map[PixelPoint]HWND{{10, 10}: 55}
		if code := precheckCode(w.precheck()(clickRequest(1, PixelPoint{10, 10}))); code != PrecheckOccluded {
			t.Fatalf("want occluded, got %v", code)
		}
	})

	t.Run("unknown hit test refuses", func(t *testing.T) {
		w := newPrecheckWorld(t)
		w.fg.hitDefault = 0
		if code := precheckCode(w.precheck()(clickRequest(1, PixelPoint{10, 10}))); code != PrecheckOccluded {
			t.Fatalf("want occluded for an indeterminate hit test, got %v", code)
		}
	})

	t.Run("child window hit counts as the target", func(t *testing.T) {
		w := newPrecheckWorld(t)
		w.fg.hits = map[PixelPoint]HWND{{10, 10}: 77} // a child render surface
		w.fg.roots = map[HWND]HWND{77: 7}
		if err := w.precheck()(clickRequest(1, PixelPoint{10, 10})); err != nil {
			t.Fatalf("a hit on the target's child window is the target: %v", err)
		}
	})

	t.Run("healthy click passes", func(t *testing.T) {
		w := newPrecheckWorld(t)
		if err := w.precheck()(clickRequest(1, PixelPoint{10, 10})); err != nil {
			t.Fatalf("healthy click should pass: %v", err)
		}
	})

	t.Run("key press skips point checks", func(t *testing.T) {
		w := newPrecheckWorld(t)
		w.fg.hitDefault = 0 // no hit test needed for keys
		req := InputRequest{GeometryVersion: 1, Op: InputOp{Kind: OpKeyPress, Key: 0x41}}
		if err := w.precheck()(req); err != nil {
			t.Fatalf("key press needs no point checks: %v", err)
		}
	})

	t.Run("geometry change makes older requests stale", func(t *testing.T) {
		w := newPrecheckWorld(t)
		// The window moves: the observation bumps the version to 2.
		w.geo.toScreen = func(x, y int) (PixelPoint, error) {
			return PixelPoint{50 + x, 60 + y}, nil
		}
		m, fault := MeasureClient(w.geo, 7)
		if fault != nil {
			t.Fatalf("re-measure: %v", fault)
		}
		if _, changed := w.tracker.Update(m, time.Now()); !changed {
			t.Fatal("the move should bump the geometry version")
		}
		// A request built on version-1 coordinates is now stale, even
		// though its point happens to lie inside the new rect.
		if code := precheckCode(w.precheck()(clickRequest(1, PixelPoint{60, 70}))); code != PrecheckStaleGeometry {
			t.Fatalf("want staleGeometry after the move, got %v", code)
		}
		if err := w.precheck()(clickRequest(2, PixelPoint{60, 70})); err != nil {
			t.Fatalf("a request naming the new version passes: %v", err)
		}
	})
}
