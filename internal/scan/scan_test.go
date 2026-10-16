package scan_test

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/scan"
)

// scriptedController delegates each controller phase to a function the
// test provides.
type scriptedController struct {
	prepare func(ctx context.Context, h *scan.ControllerHandle) error
	collect func(ctx context.Context, h *scan.ControllerHandle) error
}

func (c *scriptedController) Prepare(ctx context.Context, h *scan.ControllerHandle) error {
	if c.prepare == nil {
		return nil
	}
	return c.prepare(ctx, h)
}

func (c *scriptedController) Collect(ctx context.Context, h *scan.ControllerHandle) error {
	if c.collect == nil {
		return nil
	}
	return c.collect(ctx, h)
}

// scriptedRecognizer delegates recognition to a function the test provides.
type scriptedRecognizer struct {
	recognize func(ctx context.Context, task scan.Task, submit scan.Submit) error
}

func (r *scriptedRecognizer) Recognize(ctx context.Context, task scan.Task, submit scan.Submit) error {
	return r.recognize(ctx, task, submit)
}

// recordSink collects every delivered record, optionally failing the
// delivery through fail.
type recordSink struct {
	mu      sync.Mutex
	records []scan.Record
	fail    func(scan.Record) error
}

func (s *recordSink) Deliver(rec scan.Record) error {
	s.mu.Lock()
	s.records = append(s.records, rec)
	fail := s.fail
	s.mu.Unlock()
	if fail != nil {
		return fail(rec)
	}
	return nil
}

func (s *recordSink) all() []scan.Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]scan.Record(nil), s.records...)
}

func (s *recordSink) len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.records)
}

// newSession starts a session with the given pieces and fails the test on
// construction errors.
func newSession(t *testing.T, id scan.ID, capacity, workers int, ctrl scan.Controller, rec scan.Recognizer, sink scan.Deliverer) *scan.Session {
	t.Helper()
	s, err := scan.Start(scan.Config{
		ID:         id,
		Capacity:   capacity,
		Workers:    workers,
		Controller: ctrl,
		Recognizer: rec,
		Deliverer:  sink,
	})
	if err != nil {
		t.Fatalf("scan.Start: %v", err)
	}
	return s
}

// admitN is a collection loop that admits n observations, one slot at a
// time, and unwinds with the context error once the session stops.
func admitN(n int) func(context.Context, *scan.ControllerHandle) error {
	return func(ctx context.Context, h *scan.ControllerHandle) error {
		for i := 0; i < n; i++ {
			if !h.InputPermitted() {
				return ctx.Err()
			}
			slot := h.Acquire(ctx)
			if slot == nil {
				return ctx.Err()
			}
			if _, err := slot.Admit(fmt.Sprintf("obs-%d", i)); err != nil {
				slot.Discard()
				return ctx.Err()
			}
		}
		return nil
	}
}

// echoRecognizer submits the observation back as the result, honouring
// cancellation by returning without an outcome.
func echoRecognizer() *scriptedRecognizer {
	return &scriptedRecognizer{recognize: func(ctx context.Context, task scan.Task, submit scan.Submit) error {
		select {
		case <-ctx.Done():
			return nil // terminated; the entry ends unprocessed
		default:
		}
		return submit(task.SessionID, task.Seq, scan.Recognition{Result: "rec:" + fmt.Sprint(task.Observation)})
	}}
}

// waitEnded waits for the session to seal and returns the ended snapshot.
// The timeout is a hang guard, not an ordering device.
func waitEnded(t *testing.T, s *scan.Session) scan.Snapshot {
	t.Helper()
	select {
	case <-s.Done():
	case <-time.After(30 * time.Second):
		t.Fatal("session did not end")
	}
	return s.Snapshot()
}

// waitPhase polls the authoritative snapshot until the phase is reached.
func waitPhase(t *testing.T, s *scan.Session, want scan.Phase) scan.Snapshot {
	t.Helper()
	for i := 0; i < 500_000; i++ {
		snap := s.Snapshot()
		if snap.Phase == want {
			return snap
		}
		if snap.Phase == scan.PhaseEnded {
			t.Fatalf("session ended while waiting for phase %s", want)
		}
		runtime.Gosched()
	}
	t.Fatalf("phase %s was never reached", want)
	return scan.Snapshot{}
}

// assertTrigger checks the first stop trigger.
func assertTrigger(t *testing.T, snap scan.Snapshot, kind scan.TriggerKind, code string, phase scan.Phase) {
	t.Helper()
	if snap.Trigger == nil {
		t.Fatalf("want %s trigger %s in phase %s, got none", kind, code, phase)
	}
	got := snap.Trigger
	if got.Kind != kind || got.Code != code || got.Phase != phase {
		t.Fatalf("want trigger %s/%s in phase %s, got %s/%s in phase %s", kind, code, phase, got.Kind, got.Code, got.Phase)
	}
}

// hasFault reports whether the snapshot lists a fault with the code.
func hasFault(snap scan.Snapshot, code string) bool {
	for _, f := range snap.Faults {
		if f.Code == code {
			return true
		}
	}
	return false
}

// reconcile checks the ended-session invariants: the five-state accounting
// sums to the captured count, nothing is queued or running anymore, every
// captured sequence has exactly one delivered terminal record, and the
// delivered states match the counts. Every lifecycle test ends with it.
func reconcile(t *testing.T, s *scan.Session, sink *recordSink) {
	t.Helper()
	snap := s.Snapshot()
	if snap.Phase != scan.PhaseEnded {
		t.Fatalf("session is %s, not ended", snap.Phase)
	}
	if snap.End == "" {
		t.Fatal("ended snapshot carries no end kind")
	}
	if snap.InputPermitted {
		t.Fatal("input permission is still open after the session ended")
	}
	if snap.Counts.Captured != snap.Counts.Sum() {
		t.Fatalf("captured %d does not equal the sum of the five states %+v", snap.Counts.Captured, snap.Counts)
	}
	if snap.Counts.Queued != 0 || snap.Counts.Running != 0 {
		t.Fatalf("ended session still has queued or running entries: %+v", snap.Counts)
	}
	records := sink.all()
	if len(records) != snap.Counts.Captured {
		t.Fatalf("got %d delivered records for %d captured sequences", len(records), snap.Counts.Captured)
	}
	seen := make(map[scan.Seq]bool, len(records))
	byState := make(map[scan.State]int)
	for _, rec := range records {
		if rec.SessionID != s.ID() {
			t.Fatalf("record for seq %d names session %q, want %q", rec.Seq, rec.SessionID, s.ID())
		}
		if seen[rec.Seq] {
			t.Fatalf("sequence %d was delivered twice", rec.Seq)
		}
		seen[rec.Seq] = true
		if !rec.State.IsTerminal() {
			t.Fatalf("delivered record for seq %d is in non-terminal state %s", rec.Seq, rec.State)
		}
		byState[rec.State]++
	}
	for st, n := range byState {
		var want int
		switch st {
		case scan.StateProduced:
			want = snap.Counts.Produced
		case scan.StateFailed:
			want = snap.Counts.Failed
		case scan.StateUnprocessed:
			want = snap.Counts.Unprocessed
		}
		if n != want {
			t.Fatalf("delivered %d records in state %s, snapshot counts %d", n, st, want)
		}
	}
}

func TestStartValidation(t *testing.T) {
	base := scan.Config{
		ID:         "cfg",
		Capacity:   1,
		Controller: &scriptedController{},
		Recognizer: echoRecognizer(),
		Deliverer:  &recordSink{},
	}
	bad := map[string]func(scan.Config) scan.Config{
		"empty id":          func(c scan.Config) scan.Config { c.ID = ""; return c },
		"zero capacity":     func(c scan.Config) scan.Config { c.Capacity = 0; return c },
		"negative capacity": func(c scan.Config) scan.Config { c.Capacity = -1; return c },
		"negative workers":  func(c scan.Config) scan.Config { c.Workers = -1; return c },
		"nil controller":    func(c scan.Config) scan.Config { c.Controller = nil; return c },
		"nil recognizer":    func(c scan.Config) scan.Config { c.Recognizer = nil; return c },
		"nil deliverer":     func(c scan.Config) scan.Config { c.Deliverer = nil; return c },
	}
	for name, mutate := range bad {
		if _, err := scan.Start(mutate(base)); err == nil {
			t.Errorf("%s: want a start error", name)
		}
	}
	// Zero workers defaults to a single worker and the session runs.
	s := newSession(t, "cfg-ok", 1, 0, &scriptedController{collect: admitN(2)}, echoRecognizer(), &recordSink{})
	<-s.Done()
	if got := s.Snapshot(); got.Counts.Produced != 2 {
		t.Fatalf("produced %d, want 2", got.Counts.Produced)
	}
}

func TestImmediateCancelDuringPrepare(t *testing.T) {
	prepareStarted := make(chan struct{})
	prepareReturned := make(chan struct{})
	collectRan := false
	ctrl := &scriptedController{
		prepare: func(ctx context.Context, h *scan.ControllerHandle) error {
			close(prepareStarted)
			<-ctx.Done() // precheck work observes the stop and unwinds
			close(prepareReturned)
			return ctx.Err()
		},
		collect: func(context.Context, *scan.ControllerHandle) error {
			collectRan = true
			return nil
		},
	}
	sink := &recordSink{}
	s := newSession(t, "immediate", 2, 1, ctrl, echoRecognizer(), sink)
	<-prepareStarted
	if snap := s.Snapshot(); snap.Phase != scan.PhasePreparing || snap.InputPermitted {
		t.Fatalf("during preparation: phase %s, permitted %v", snap.Phase, snap.InputPermitted)
	}
	s.Cancel()
	<-prepareReturned
	snap := waitEnded(t, s)
	if collectRan {
		t.Fatal("Collect ran although the session was cancelled during preparation")
	}
	if snap.End != scan.EndCancelled {
		t.Fatalf("end %s, want cancelled", snap.End)
	}
	assertTrigger(t, snap, scan.TriggerUser, scan.CodeUserCancelled, scan.PhasePreparing)
	if snap.InputPermitted {
		t.Fatal("input permission open after a cancel during preparation")
	}
	if snap.Counts != (scan.Counts{}) {
		t.Fatalf("counts %+v, want all zero", snap.Counts)
	}
	if sink.len() != 0 {
		t.Fatalf("sink got %d records, want none", sink.len())
	}
	reconcile(t, s, sink)
}

func TestCancelDuringPrepareWithLatePrepareSuccess(t *testing.T) {
	prepareStarted := make(chan struct{})
	release := make(chan struct{})
	collectRan := false
	ctrl := &scriptedController{
		prepare: func(ctx context.Context, h *scan.ControllerHandle) error {
			close(prepareStarted)
			<-release // prechecks keep running until the test lets go
			return nil
		},
		collect: func(context.Context, *scan.ControllerHandle) error {
			collectRan = true
			return nil
		},
	}
	sink := &recordSink{}
	s := newSession(t, "late-prepare", 2, 1, ctrl, echoRecognizer(), sink)
	<-prepareStarted
	s.Cancel()
	close(release)
	snap := waitEnded(t, s)
	if collectRan {
		t.Fatal("Collect ran although the session was cancelled during preparation")
	}
	if snap.End != scan.EndCancelled {
		t.Fatalf("end %s, want cancelled", snap.End)
	}
	assertTrigger(t, snap, scan.TriggerUser, scan.CodeUserCancelled, scan.PhasePreparing)
	reconcile(t, s, sink)
}

func TestPrepareFailureFailsSession(t *testing.T) {
	collectRan := false
	ctrl := &scriptedController{
		prepare: func(context.Context, *scan.ControllerHandle) error {
			return scan.NewFault("permissionRevoked", errors.New("screen recording denied"))
		},
		collect: func(context.Context, *scan.ControllerHandle) error {
			collectRan = true
			return nil
		},
	}
	sink := &recordSink{}
	s := newSession(t, "precheck", 2, 1, ctrl, echoRecognizer(), sink)
	snap := waitEnded(t, s)
	if collectRan {
		t.Fatal("Collect ran although preparation failed")
	}
	if snap.End != scan.EndFailed {
		t.Fatalf("end %s, want failed", snap.End)
	}
	assertTrigger(t, snap, scan.TriggerFault, "permissionRevoked", scan.PhasePreparing)
	if snap.Trigger.Message == "" {
		t.Fatal("trigger carries no message")
	}
	reconcile(t, s, sink)
}

func TestPrepareFaultAfterUserCancelIsRecorded(t *testing.T) {
	prepareStarted := make(chan struct{})
	release := make(chan struct{})
	ctrl := &scriptedController{
		prepare: func(ctx context.Context, h *scan.ControllerHandle) error {
			close(prepareStarted)
			<-release
			// A real fault surfacing after the user cancel must not be
			// masked by it.
			return scan.NewFault("focusLost", errors.New("target window lost"))
		},
	}
	sink := &recordSink{}
	s := newSession(t, "masked", 2, 1, ctrl, echoRecognizer(), sink)
	<-prepareStarted
	s.Cancel()
	close(release)
	snap := waitEnded(t, s)
	if snap.End != scan.EndCancelled {
		t.Fatalf("end %s, want cancelled: the user stop was first", snap.End)
	}
	assertTrigger(t, snap, scan.TriggerUser, scan.CodeUserCancelled, scan.PhasePreparing)
	if !hasFault(snap, "focusLost") {
		t.Fatalf("later fault focusLost is not recorded: %+v", snap.Faults)
	}
	reconcile(t, s, sink)
}

func TestNormalCompletionDrainsQueue(t *testing.T) {
	permitSeen := false
	ctrl := &scriptedController{
		collect: func(ctx context.Context, h *scan.ControllerHandle) error {
			if !h.InputPermitted() {
				return errors.New("input permission never opened for capturing")
			}
			permitSeen = true
			return admitN(5)(ctx, h)
		},
	}
	sink := &recordSink{}
	s := newSession(t, "normal", 2, 1, ctrl, echoRecognizer(), sink)
	snap := waitEnded(t, s)
	if !permitSeen {
		t.Fatal("controller never observed an open input permission")
	}
	if snap.End != scan.EndCompleted {
		t.Fatalf("end %s, want completed", snap.End)
	}
	if snap.Trigger != nil {
		t.Fatalf("completed session carries a trigger: %+v", snap.Trigger)
	}
	if snap.Counts != (scan.Counts{Captured: 5, Produced: 5}) {
		t.Fatalf("counts %+v, want 5 captured and 5 produced", snap.Counts)
	}
	records := sink.all()
	if len(records) != 5 {
		t.Fatalf("sink got %d records, want 5", len(records))
	}
	for i, rec := range records {
		if rec.Seq != scan.Seq(i+1) {
			t.Fatalf("record %d has seq %d, want %d", i, rec.Seq, i+1)
		}
		if rec.State != scan.StateProduced || rec.Result != "rec:obs-"+fmt.Sprint(i) {
			t.Fatalf("unexpected record %+v", rec)
		}
		if rec.Observation != fmt.Sprintf("obs-%d", i) {
			t.Fatalf("record for seq %d lost its observation: %+v", rec.Seq, rec.Observation)
		}
	}
	if snap.InputPermitted {
		t.Fatal("input permission open after the session ended")
	}
	reconcile(t, s, sink)
}

func TestUnadmittedAttemptsConsumeNoSequence(t *testing.T) {
	firstSeq := make(chan scan.Seq, 1)
	ctrl := &scriptedController{
		collect: func(ctx context.Context, h *scan.ControllerHandle) error {
			// Three untrusted attempts: acquired, looked at, discarded.
			for i := 0; i < 3; i++ {
				slot := h.Acquire(ctx)
				if slot == nil {
					return ctx.Err()
				}
				slot.Discard()
			}
			slot := h.Acquire(ctx)
			if slot == nil {
				return ctx.Err()
			}
			seq, err := slot.Admit("real observation")
			if err != nil {
				slot.Discard()
				return err
			}
			firstSeq <- seq
			return nil
		},
	}
	sink := &recordSink{}
	s := newSession(t, "seq", 2, 1, ctrl, echoRecognizer(), sink)
	if seq := <-firstSeq; seq != 1 {
		t.Fatalf("first admitted sequence is %d, want 1", seq)
	}
	snap := waitEnded(t, s)
	if snap.End != scan.EndCompleted || snap.Counts.Captured != 1 || snap.Counts.Produced != 1 {
		t.Fatalf("end %s with counts %+v, want completed with one produced entry", snap.End, snap.Counts)
	}
	records := sink.all()
	if len(records) != 1 || records[0].Seq != 1 {
		t.Fatalf("sink records %+v, want exactly seq 1", records)
	}
	reconcile(t, s, sink)
}

func TestPermitLifecycle(t *testing.T) {
	admitted := make(chan struct{})
	release := make(chan struct{})
	var unwindSawPermit bool
	ctrl := &scriptedController{
		prepare: func(ctx context.Context, h *scan.ControllerHandle) error {
			if h.InputPermitted() {
				t.Error("input permission open during preparation")
			}
			return nil
		},
		collect: func(ctx context.Context, h *scan.ControllerHandle) error {
			if !h.InputPermitted() {
				return errors.New("input permission did not open for capturing")
			}
			if err := admitN(2)(ctx, h); err != nil {
				return err
			}
			close(admitted)
			<-release
			// The unwind after the stop must see the permission closed and
			// must not be able to reopen it.
			unwindSawPermit = h.InputPermitted()
			return ctx.Err()
		},
	}
	sink := &recordSink{}
	s := newSession(t, "permit", 4, 1, ctrl, echoRecognizer(), sink)
	<-admitted
	waitCounts(t, s, func(c scan.Counts) bool { return c.Produced == 2 })
	s.Cancel()
	// Cancel closes the permission synchronously before it returns.
	if snap := s.Snapshot(); snap.InputPermitted || snap.Phase != scan.PhaseFinalizing {
		t.Fatalf("after Cancel: permitted %v, phase %s", snap.InputPermitted, snap.Phase)
	}
	close(release)
	snap := waitEnded(t, s)
	if unwindSawPermit {
		t.Fatal("input permission was still open while the controller unwound")
	}
	if snap.End != scan.EndCancelled {
		t.Fatalf("end %s, want cancelled", snap.End)
	}
	assertTrigger(t, snap, scan.TriggerUser, scan.CodeUserCancelled, scan.PhaseCapturing)
	if snap.InputPermitted {
		t.Fatal("input permission open after the session ended")
	}
	// Both admitted tasks completed before the stop: produced, not lost.
	if snap.Counts.Produced != 2 {
		t.Fatalf("counts %+v, want 2 produced", snap.Counts)
	}
	reconcile(t, s, sink)
}

// TestOnAdmitPrecedesWorkersAndSeesEveryAdmission pins the admission
// hook's contract: it is called for every admitted sequence, before the
// recognition workers can see the task, and never for discarded
// attempts.
func TestOnAdmitPrecedesWorkersAndSeesEveryAdmission(t *testing.T) {
	var mu sync.Mutex
	admitted := make(map[scan.Seq]string)
	hook := func(seq scan.Seq, observation any) error {
		mu.Lock()
		admitted[seq] = fmt.Sprint(observation)
		mu.Unlock()
		return nil
	}
	recognizer := &scriptedRecognizer{recognize: func(ctx context.Context, task scan.Task, submit scan.Submit) error {
		mu.Lock()
		_, seen := admitted[task.Seq]
		mu.Unlock()
		if !seen {
			t.Errorf("worker saw task %d before the admission hook ran", task.Seq)
		}
		return submit(task.SessionID, task.Seq, scan.Recognition{Result: "done"})
	}}
	ctrl := &scriptedController{collect: func(ctx context.Context, h *scan.ControllerHandle) error {
		for i := 0; i < 3; i++ {
			slot := h.Acquire(ctx)
			if slot == nil {
				return ctx.Err()
			}
			if _, err := slot.Admit(fmt.Sprintf("obs-%d", i)); err != nil {
				slot.Discard()
				return ctx.Err()
			}
		}
		// A discarded attempt consumes no number and must not reach the
		// hook either.
		slot := h.Acquire(ctx)
		if slot == nil {
			return ctx.Err()
		}
		slot.Discard()
		return nil
	}}
	sink := &recordSink{}
	s, err := scan.Start(scan.Config{
		ID:         "on-admit",
		Capacity:   2,
		Controller: ctrl,
		Recognizer: recognizer,
		Deliverer:  sink,
		OnAdmit:    hook,
	})
	if err != nil {
		t.Fatalf("scan.Start: %v", err)
	}
	snap := waitEnded(t, s)
	if snap.End != scan.EndCompleted {
		t.Fatalf("end %s, want completed", snap.End)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(admitted) != 3 {
		t.Fatalf("hook saw %d admissions, want exactly the 3 admitted ones", len(admitted))
	}
	for seq, want := range map[scan.Seq]string{1: "obs-0", 2: "obs-1", 3: "obs-2"} {
		if got := admitted[seq]; got != want {
			t.Fatalf("hook saw %q for sequence %d, want %q", got, seq, want)
		}
	}
}

// TestOnAdmitErrorIsAStorageFault checks the hook's failure path: the
// error cuts collection as a storage fault, the admission it failed for
// is still counted, and the accounting still reconciles.
func TestOnAdmitErrorIsAStorageFault(t *testing.T) {
	ctrl := &scriptedController{collect: admitN(4)}
	sink := &recordSink{}
	s, err := scan.Start(scan.Config{
		ID:         "on-admit-fault",
		Capacity:   4,
		Controller: ctrl,
		Recognizer: echoRecognizer(),
		Deliverer:  sink,
		OnAdmit: func(seq scan.Seq, observation any) error {
			if seq == 2 {
				return errors.New("disk full")
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("scan.Start: %v", err)
	}
	snap := waitEnded(t, s)
	if snap.End != scan.EndFailed {
		t.Fatalf("end %s, want failed", snap.End)
	}
	assertTrigger(t, snap, scan.TriggerFault, scan.CodeStorageFailure, scan.PhaseCapturing)
	if snap.Counts.Captured == 0 {
		t.Fatalf("counts %+v, want the admission to count even though its hook failed", snap.Counts)
	}
	reconcile(t, s, sink)
}
