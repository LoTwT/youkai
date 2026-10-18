package windows

import (
	"errors"
	"testing"
	"time"
)

// Hold and drag behavior (PR 4a), mirroring the macOS dispatcher: a
// click's press is held between two SendInput batches; a drag delivers
// one batch per interpolated move with the step gate between steps; the
// last insertion time feeds stop-time bookkeeping. Sleep is recorded,
// never waited.

type sleepLog struct {
	spans []time.Duration
}

func holdActor(sender Sender) (*InputActor, *sleepLog) {
	sleep := &sleepLog{}
	actor := NewInputActor(sender, nil)
	actor.SetSleep(func(d time.Duration) { sleep.spans = append(sleep.spans, d) })
	actor.Permit()
	return actor, sleep
}

func TestClickHoldSplitsBatches(t *testing.T) {
	sender := newFakeSender()
	actor, sleep := holdActor(sender)

	// Zero hold takes the default.
	res := actor.Dispatch(clickRequest(1, PixelPoint{5, 5}))
	if res.State != StateDelivered || res.EventsPlanned != 3 || res.EventsInserted != 3 {
		t.Fatalf("default-hold click: %+v", res)
	}
	if len(sender.calls) != 2 || len(sender.calls[0]) != 2 || len(sender.calls[1]) != 1 {
		t.Fatalf("click batches: %+v", sender.calls)
	}
	if len(sleep.spans) != 1 || sleep.spans[0] != DefaultHold {
		t.Fatalf("default hold not applied: %v", sleep.spans)
	}

	// An explicit hold is honored; above the bound the click is refused
	// before any event.
	res = actor.Dispatch(InputRequest{GeometryVersion: 1, Op: InputOp{
		Kind: OpClick, Point: PixelPoint{5, 5}, Button: MouseButtonLeft, Hold: 25 * time.Millisecond}})
	if res.State != StateDelivered || len(sleep.spans) != 2 || sleep.spans[1] != 25*time.Millisecond {
		t.Fatalf("explicit hold: %+v / %v", res, sleep.spans)
	}
	res = actor.Dispatch(InputRequest{GeometryVersion: 1, Op: InputOp{
		Kind: OpClick, Point: PixelPoint{5, 5}, Button: MouseButtonLeft, Hold: MaxHold + time.Millisecond}})
	if res.State != StateNotDispatched || res.EventsPlanned != 0 || sender.callCount() != 4 {
		t.Fatalf("over-bound hold must be refused before any event: %+v (%d calls)", res, sender.callCount())
	}
}

func dragRequest(version GeometryVersion, from, to PixelPoint, steps int, dur time.Duration) InputRequest {
	return InputRequest{GeometryVersion: version, Op: InputOp{
		Kind: OpDrag, Point: from, To: to, Button: MouseButtonLeft, Steps: steps, Duration: dur}}
}

func TestDragDeliversBatchedInterpolation(t *testing.T) {
	sender := newFakeSender()
	actor, sleep := holdActor(sender)

	res := actor.Dispatch(dragRequest(1, PixelPoint{100, 100}, PixelPoint{100, 200}, 4, 40*time.Millisecond))
	if res.State != StateDelivered || res.Aborted {
		t.Fatalf("drag result: %+v", res)
	}
	// press batch + 4 move batches + release batch
	if sender.callCount() != 6 {
		t.Fatalf("drag batches: %d calls: %+v", sender.callCount(), sender.calls)
	}
	if res.EventsPlanned != 7 || res.EventsInserted != 7 { // press(2) + 4 moves + release(1)
		t.Fatalf("drag accounting: %+v", res)
	}
	first := sender.calls[0]
	if len(first) != 2 || first[0].Kind != EventMouseMove || first[1].Kind != EventMouseDown {
		t.Fatalf("press batch: %+v", first)
	}
	for i := 1; i <= 4; i++ {
		batch := sender.calls[i]
		if len(batch) != 1 || batch[0].Kind != EventMouseMove {
			t.Fatalf("step batch %d: %+v", i, batch)
		}
	}
	want := PixelPoint{100, 200} // the last of 4 steps lands on the destination
	if got := sender.calls[4][0].Point; got != want {
		t.Fatalf("last step point %v, want %v", got, want)
	}
	last := sender.calls[5]
	if len(last) != 1 || last[0].Kind != EventMouseUp {
		t.Fatalf("release batch: %+v", last)
	}
	// One interval before every step and before the release: 10 ms each.
	if len(sleep.spans) != 5 {
		t.Fatalf("drag sleeps: %v", sleep.spans)
	}
	for _, span := range sleep.spans {
		if span != 10*time.Millisecond {
			t.Fatalf("drag interval %v, want 10ms", span)
		}
	}
	if len(res.HeldAfter) != 0 || len(actor.Held()) != 0 {
		t.Fatalf("a delivered drag leaves nothing held: %+v", res.HeldAfter)
	}
	if actor.LastPostAt().IsZero() {
		t.Fatal("the drag inserted events, so LastPostAt must be set")
	}

	// Defaults: zero steps take DefaultDragSteps, zero duration
	// DefaultDragDuration; over-bound values are refused before any
	// event.
	res = actor.Dispatch(dragRequest(1, PixelPoint{0, 0}, PixelPoint{0, 30}, 0, 0))
	if res.State != StateDelivered || res.EventsPlanned != DefaultDragSteps+3 {
		t.Fatalf("default drag: %+v (planned %d)", res, res.EventsPlanned)
	}
	before := sender.callCount()
	res = actor.Dispatch(dragRequest(1, PixelPoint{0, 0}, PixelPoint{0, 30}, MaxDragSteps+1, 0))
	if res.State != StateNotDispatched || sender.callCount() != before {
		t.Fatalf("over-bound steps must be refused before any event: %+v", res)
	}
	res = actor.Dispatch(dragRequest(1, PixelPoint{0, 0}, PixelPoint{0, 30}, 2, MaxDragDuration+time.Millisecond))
	if res.State != StateNotDispatched || sender.callCount() != before {
		t.Fatalf("over-bound duration must be refused before any event: %+v", res)
	}
}

func TestDragAbortsWhenStepGateCloses(t *testing.T) {
	sender := newFakeSender()
	actor, sleep := holdActor(sender)

	permit := true
	actor.SetStepGate(func() bool { return permit })
	// The gate closes while the second step sleeps.
	step := 0
	actor.SetSleep(func(d time.Duration) {
		sleep.spans = append(sleep.spans, d)
		step++
		if step == 2 {
			permit = false
		}
	})

	res := actor.Dispatch(dragRequest(1, PixelPoint{0, 0}, PixelPoint{0, 100}, 5, 50*time.Millisecond))
	if !res.Aborted || res.State != StatePartialDelivered {
		t.Fatalf("aborted drag result: %+v", res)
	}
	// press batch + one move batch + the abort release batch
	if sender.callCount() != 3 {
		t.Fatalf("abort batches: %d calls: %+v", sender.callCount(), sender.calls)
	}
	if len(sender.calls[2]) != 1 || sender.calls[2][0].Kind != EventMouseUp {
		t.Fatalf("the abort must insert the release: %+v", sender.calls[2])
	}
	if res.EventsInserted != 4 || res.EventsPlanned != 4 {
		t.Fatalf("abort accounting: %+v", res)
	}
	if len(actor.Held()) != 0 {
		t.Fatalf("ledger must be empty after the abort release: %+v", actor.Held())
	}

	// A gate that stays open never aborts.
	permit = true
	res = actor.Dispatch(dragRequest(1, PixelPoint{0, 0}, PixelPoint{0, 100}, 3, 30*time.Millisecond))
	if res.Aborted || res.State != StateDelivered {
		t.Fatalf("open-gate drag: %+v", res)
	}
}

func TestDragBlockedMidWayIsStickyAndHolds(t *testing.T) {
	// The press batch inserts; the first move batch is refused the UIPI
	// way: the actor turns blocked, the release attempt also fails, and
	// the button stays ledgered for the stop-time release.
	sender := newFakeSender()
	calls := 0
	sender.respond = func(call int, events []InputEvent) (int, bool, error) {
		calls++
		if call == 0 {
			return 2, false, nil
		}
		return 0, true, nil
	}
	actor, _ := holdActor(sender)

	res := actor.Dispatch(dragRequest(1, PixelPoint{0, 0}, PixelPoint{0, 100}, 3, 30*time.Millisecond))
	if res.State != StateBlocked {
		t.Fatalf("blocked drag: %+v", res)
	}
	if len(actor.Held()) != 1 {
		t.Fatalf("the button may still be down and stays ledgered: %+v", actor.Held())
	}
	// Sticky: every further operation is refused without a send.
	if res := actor.Dispatch(clickRequest(1, PixelPoint{0, 0})); res.State != StateNotDispatched || sender.callCount() != calls {
		t.Fatalf("blocked actor must refuse further operations: %+v (%d calls)", res, sender.callCount())
	}
	// The stop-time release still runs and is accounted honestly.
	sender.respond = func(call int, events []InputEvent) (int, bool, error) {
		return 0, true, nil // the release is refused the UIPI way too
	}
	records := actor.ReleaseHeld()
	if len(records) != 1 || records[0].Released {
		t.Fatalf("the refused release must be recorded as not released: %+v", records)
	}
	if len(actor.Held()) != 1 {
		t.Fatalf("the button may still be down and stays ledgered: %+v", actor.Held())
	}
}

func TestLastPostAtTracksSuccessfulInsertionsOnly(t *testing.T) {
	sender := newFakeSender()
	sender.respond = func(call int, events []InputEvent) (int, bool, error) {
		return 0, false, errors.New("SendInput: internal error")
	}
	actor, _ := holdActor(sender)
	before := actor.LastPostAt()
	if res := actor.Dispatch(clickRequest(1, PixelPoint{0, 0})); res.State != StateFailed {
		t.Fatalf("zero insertion: %+v", res)
	}
	if !actor.LastPostAt().Equal(before) {
		t.Fatal("failed insertions must not move LastPostAt")
	}
}
