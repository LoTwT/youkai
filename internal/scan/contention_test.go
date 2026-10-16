package scan_test

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"testing"

	"github.com/LoTwT/youkai/internal/scan"
)

// The tests in this file drive the kernel with fakes and controllable
// events to cover the session-and-contention behaviours of the
// architecture's acceptance table: immediate cancel, cancel while the
// pipeline is full, out-of-order and duplicate outcomes, late outcomes,
// slow subscribers, concurrent stops, single termination per sequence,
// one-way input permission and count reconciliation. No test relies on
// sleeping for ordering; channels and the kernel's own synchronisation
// decide.

// waitCounts polls the snapshot until want is satisfied.
func waitCounts(t *testing.T, s *scan.Session, want func(scan.Counts) bool) {
	t.Helper()
	for i := 0; i < 500_000; i++ {
		snap := s.Snapshot()
		if want(snap.Counts) {
			return
		}
		if snap.Phase == scan.PhaseEnded {
			t.Fatalf("session ended before the counts were reached: %+v", snap.Counts)
		}
		runtime.Gosched()
	}
	t.Fatal("counts condition was never reached")
}

// assertDoneNotClosed fails when the session already sealed; used to prove
// that cancellation waits instead of tearing things down.
func assertDoneNotClosed(t *testing.T, s *scan.Session, why string) {
	t.Helper()
	select {
	case <-s.Done():
		t.Fatalf("session sealed although %s", why)
	default:
	}
}

func TestQueueFullWaitsForCapacityAndCancelUnblocks(t *testing.T) {
	inRec := make(chan struct{})
	release := make(chan struct{})
	admitted := make(chan struct{})
	collectReturned := make(chan error, 1)
	ctrl := &scriptedController{
		collect: func(ctx context.Context, h *scan.ControllerHandle) error {
			if err := admitN(2)(ctx, h); err != nil {
				return err
			}
			close(admitted)
			// Both slots are held (one task running, one queued), so this
			// acquisition waits for capacity and must respond to cancel.
			slot := h.Acquire(ctx)
			if slot != nil {
				// A freed slot may race the cancelled context in select;
				// admission after the stop must fail either way.
				if _, err := slot.Admit("must not be admitted"); err == nil {
					collectReturned <- errors.New("admission succeeded after the stop")
					return nil
				}
				slot.Discard()
			}
			collectReturned <- nil
			return ctx.Err()
		},
	}
	rec := &scriptedRecognizer{recognize: func(ctx context.Context, task scan.Task, submit scan.Submit) error {
		close(inRec) // task 1 is running now
		<-release    // hold recognition until the test lets go
		return nil   // terminated without an outcome
	}}
	sink := &recordSink{}
	s := newSession(t, "queue-full", 2, 1, ctrl, rec, sink)
	<-inRec
	<-admitted
	s.Cancel()
	close(release)
	if err := <-collectReturned; err != nil {
		t.Fatal(err)
	}
	snap := waitEnded(t, s)
	if snap.End != scan.EndCancelled {
		t.Fatalf("end %s, want cancelled", snap.End)
	}
	assertTrigger(t, snap, scan.TriggerUser, scan.CodeUserCancelled, scan.PhaseCapturing)
	// Two captured sequences, both cancelled-unprocessed; the blocked
	// acquisition let nothing new in.
	if snap.Counts != (scan.Counts{Captured: 2, Unprocessed: 2}) {
		t.Fatalf("counts %+v, want 2 captured and 2 unprocessed", snap.Counts)
	}
	for _, rec := range sink.all() {
		if rec.State != scan.StateUnprocessed || rec.Cause != scan.CauseCancelled {
			t.Fatalf("record %+v, want cancelled unprocessed", rec)
		}
		if rec.Reason != scan.CodeUserCancelled {
			t.Fatalf("unprocessed reason %q, want %q", rec.Reason, scan.CodeUserCancelled)
		}
	}
	reconcile(t, s, sink)
}

func TestCancelWaitsForProducerAndWorkers(t *testing.T) {
	admitted := make(chan struct{})
	producerRelease := make(chan struct{})
	workerRelease := make(chan struct{})
	inRec := make(chan struct{}, 2)
	ctrl := &scriptedController{
		collect: func(ctx context.Context, h *scan.ControllerHandle) error {
			if err := admitN(2)(ctx, h); err != nil {
				return err
			}
			close(admitted)
			<-producerRelease // the controller is slow to unwind
			return ctx.Err()
		},
	}
	rec := &scriptedRecognizer{recognize: func(ctx context.Context, task scan.Task, submit scan.Submit) error {
		inRec <- struct{}{} // both workers are inside recognition now
		<-workerRelease     // recognition is slow to return
		return nil          // terminated without an outcome
	}}
	sink := &recordSink{}
	s := newSession(t, "wait-all", 4, 2, ctrl, rec, sink)
	<-admitted
	<-inRec
	<-inRec
	s.Cancel()
	// The fast part is done, but neither the producer nor the workers have
	// returned: the session must stay in finalizing, not pretend to end.
	assertDoneNotClosed(t, s, "the producer and workers are still running")
	if snap := s.Snapshot(); snap.Phase != scan.PhaseFinalizing {
		t.Fatalf("phase %s, want finalizing", snap.Phase)
	}
	close(workerRelease)
	assertDoneNotClosed(t, s, "the producer is still running")
	close(producerRelease)
	snap := waitEnded(t, s)
	if snap.End != scan.EndCancelled {
		t.Fatalf("end %s, want cancelled", snap.End)
	}
	if snap.Counts != (scan.Counts{Captured: 2, Unprocessed: 2}) {
		t.Fatalf("counts %+v, want 2 captured and 2 unprocessed", snap.Counts)
	}
	reconcile(t, s, sink)
}

func TestAcquireCancelledBetweenAcquireAndAdmit(t *testing.T) {
	acquired := make(chan struct{})
	goAhead := make(chan struct{})
	admitErr := make(chan error, 1)
	ctrl := &scriptedController{
		collect: func(ctx context.Context, h *scan.ControllerHandle) error {
			slot := h.Acquire(ctx)
			if slot == nil {
				close(acquired)
				admitErr <- errors.New("acquire already refused")
				return ctx.Err()
			}
			close(acquired)
			<-goAhead // the stop lands between acquire and admit
			_, err := slot.Admit("never admitted")
			admitErr <- err
			slot.Discard()
			return ctx.Err()
		},
	}
	sink := &recordSink{}
	s := newSession(t, "acquire-gap", 4, 1, ctrl, echoRecognizer(), sink)
	<-acquired
	s.Cancel()
	close(goAhead)
	if err := <-admitErr; err == nil {
		t.Fatal("admission succeeded although the session was stopped")
	}
	snap := waitEnded(t, s)
	if snap.End != scan.EndCancelled || snap.Counts != (scan.Counts{}) {
		t.Fatalf("end %s with counts %+v, want cancelled with nothing admitted", snap.End, snap.Counts)
	}
	reconcile(t, s, sink)
}

func TestOutcomesArriveUnordered(t *testing.T) {
	seq2Done := make(chan struct{})
	ctrl := &scriptedController{collect: admitN(2)}
	rec := &scriptedRecognizer{recognize: func(ctx context.Context, task scan.Task, submit scan.Submit) error {
		if task.Seq == 1 {
			// Sequence 1 waits until sequence 2 has been submitted and
			// delivered, so the outcomes arrive out of order.
			<-seq2Done
			return submit(task.SessionID, task.Seq, scan.Recognition{Result: "one"})
		}
		if err := submit(task.SessionID, task.Seq, scan.Recognition{Result: "two"}); err != nil {
			return err
		}
		close(seq2Done)
		return nil
	}}
	sink := &recordSink{}
	s := newSession(t, "unordered", 4, 2, ctrl, rec, sink)
	snap := waitEnded(t, s)
	if snap.End != scan.EndCompleted || snap.Counts.Produced != 2 {
		t.Fatalf("end %s with counts %+v, want completed with 2 produced", snap.End, snap.Counts)
	}
	records := sink.all()
	if len(records) != 2 || records[0].Seq != 2 || records[1].Seq != 1 {
		t.Fatalf("records %+v, want seq 2 delivered before seq 1", records)
	}
	reconcile(t, s, sink)
}

func TestDuplicateOutcomeRejected(t *testing.T) {
	dupErrs := make(chan error, 2)
	ctrl := &scriptedController{collect: admitN(2)}
	rec := &scriptedRecognizer{recognize: func(ctx context.Context, task scan.Task, submit scan.Submit) error {
		if task.Seq == 1 {
			if err := submit(task.SessionID, task.Seq, scan.Recognition{Result: "first"}); err != nil {
				return err
			}
			dupErrs <- submit(task.SessionID, task.Seq, scan.Recognition{Result: "first again"})
			return nil
		}
		if err := submit(task.SessionID, task.Seq, scan.Recognition{Failed: errors.New("unreadable")}); err != nil {
			return err
		}
		dupErrs <- submit(task.SessionID, task.Seq, scan.Recognition{Result: "too late"})
		return nil
	}}
	sink := &recordSink{}
	s := newSession(t, "dup", 4, 1, ctrl, rec, sink)
	snap := waitEnded(t, s)
	for i := 0; i < 2; i++ {
		if err := <-dupErrs; err == nil {
			t.Fatal("a duplicate outcome was accepted")
		}
	}
	if snap.End != scan.EndCompleted {
		t.Fatalf("end %s, want completed", snap.End)
	}
	if snap.Counts != (scan.Counts{Captured: 2, Produced: 1, Failed: 1}) {
		t.Fatalf("counts %+v, want one produced and one failed", snap.Counts)
	}
	records := sink.all()
	if len(records) != 2 || records[0].Result != "first" || records[1].Reason != "unreadable" {
		t.Fatalf("records %+v, want the first outcome of each sequence kept", records)
	}
	reconcile(t, s, sink)
}

func TestLateOutcomeAfterSealRejected(t *testing.T) {
	sealed := make(chan struct{})
	lateErr := make(chan error, 1)
	ctrl := &scriptedController{collect: admitN(1)}
	rec := &scriptedRecognizer{recognize: func(ctx context.Context, task scan.Task, submit scan.Submit) error {
		id, seq := task.SessionID, task.Seq
		go func() {
			<-sealed
			// A stale callback from a leaked goroutine after the session
			// sealed: rejected, and nothing it carries changes the record.
			lateErr <- submit(id, seq, scan.Recognition{Result: "late"})
		}()
		return nil // returns without an outcome: the task fails
	}}
	sink := &recordSink{}
	s := newSession(t, "late", 2, 1, ctrl, rec, sink)
	go func() {
		<-s.Done()
		close(sealed)
	}()
	snap := waitEnded(t, s)
	if err := <-lateErr; err == nil {
		t.Fatal("an outcome submitted after sealing was accepted")
	}
	if snap.Counts != (scan.Counts{Captured: 1, Failed: 1}) {
		t.Fatalf("counts %+v, want one failed entry", snap.Counts)
	}
	records := sink.all()
	if len(records) != 1 || records[0].State != scan.StateFailed || records[0].Result != nil {
		t.Fatalf("records %+v, want the original failure kept", records)
	}
	reconcile(t, s, sink)
}

func TestForeignAndUnknownSubmissionsRejected(t *testing.T) {
	rejects := make(chan error, 3)
	ctrl := &scriptedController{collect: admitN(1)}
	rec := &scriptedRecognizer{recognize: func(ctx context.Context, task scan.Task, submit scan.Submit) error {
		// A stale callback naming another session.
		rejects <- submit("other-session", task.Seq, scan.Recognition{Result: "x"})
		// An outcome for a sequence that was never admitted.
		rejects <- submit(task.SessionID, 999, scan.Recognition{Result: "x"})
		// A malformed outcome: neither a result nor a failure.
		rejects <- submit(task.SessionID, task.Seq, scan.Recognition{})
		return submit(task.SessionID, task.Seq, scan.Recognition{Result: "valid"})
	}}
	sink := &recordSink{}
	s := newSession(t, "foreign", 2, 1, ctrl, rec, sink)
	snap := waitEnded(t, s)
	for i := 0; i < 3; i++ {
		if err := <-rejects; err == nil {
			t.Fatal("an invalid submission was accepted")
		}
	}
	if snap.End != scan.EndCompleted || snap.Counts.Produced != 1 {
		t.Fatalf("end %s with counts %+v, want one produced entry", snap.End, snap.Counts)
	}
	reconcile(t, s, sink)
}

func TestTaskFailureDoesNotBlockTheRest(t *testing.T) {
	ctrl := &scriptedController{collect: admitN(4)}
	rec := &scriptedRecognizer{recognize: func(ctx context.Context, task scan.Task, submit scan.Submit) error {
		if task.Seq == 2 {
			return submit(task.SessionID, task.Seq, scan.Recognition{Failed: errors.New("crop unreadable")})
		}
		return submit(task.SessionID, task.Seq, scan.Recognition{Result: fmt.Sprintf("rec-%d", task.Seq)})
	}}
	sink := &recordSink{}
	s := newSession(t, "one-fails", 4, 1, ctrl, rec, sink)
	snap := waitEnded(t, s)
	if snap.End != scan.EndCompleted {
		t.Fatalf("end %s, want completed: a single failed task must not stop the session", snap.End)
	}
	if snap.Counts != (scan.Counts{Captured: 4, Produced: 3, Failed: 1}) {
		t.Fatalf("counts %+v, want 3 produced and 1 failed", snap.Counts)
	}
	for _, rec := range sink.all() {
		if rec.Seq == 2 && (rec.State != scan.StateFailed || rec.Reason != "crop unreadable") {
			t.Fatalf("seq 2 record %+v, want failed with the reason", rec)
		}
	}
	reconcile(t, s, sink)
}

func TestStorageFailureStopsCapturing(t *testing.T) {
	inRec := make(chan struct{})
	hold := make(chan struct{})
	admitted := make(chan struct{})
	collectRelease := make(chan struct{})
	ctrl := &scriptedController{
		collect: func(ctx context.Context, h *scan.ControllerHandle) error {
			if err := admitN(3)(ctx, h); err != nil {
				return err
			}
			close(admitted)
			<-collectRelease // stay in capturing until the write failure lands
			return ctx.Err()
		},
	}
	rec := &scriptedRecognizer{recognize: func(ctx context.Context, task scan.Task, submit scan.Submit) error {
		if task.Seq == 1 {
			close(inRec)
			<-hold
		}
		return submit(task.SessionID, task.Seq, scan.Recognition{Result: "rec-1"})
	}}
	sink := &recordSink{fail: func(scan.Record) error { return errors.New("disk full") }}
	s := newSession(t, "storage", 3, 1, ctrl, rec, sink)
	<-inRec
	<-admitted
	close(hold) // seq 1 submits; its delivery fails and cuts the session
	waitPhase(t, s, scan.PhaseFinalizing)
	close(collectRelease)
	snap := waitEnded(t, s)
	if snap.End != scan.EndFailed {
		t.Fatalf("end %s, want failed: a write failure must stop capturing", snap.End)
	}
	assertTrigger(t, snap, scan.TriggerFault, scan.CodeStorageFailure, scan.PhaseCapturing)
	// Seq 1 produced (its processing succeeded; the save failed), the two
	// queued entries interrupted.
	if snap.Counts != (scan.Counts{Captured: 3, Produced: 1, Unprocessed: 2}) {
		t.Fatalf("counts %+v, want 1 produced and 2 interrupted", snap.Counts)
	}
	for _, rec := range sink.all() {
		if rec.Seq != 1 && (rec.State != scan.StateUnprocessed || rec.Cause != scan.CauseInterrupted) {
			t.Fatalf("record %+v, want interrupted unprocessed", rec)
		}
	}
	if !hasFault(snap, scan.CodeStorageFailure) {
		t.Fatalf("later storage failures are not recorded: %+v", snap.Faults)
	}
	reconcile(t, s, sink)
}

func TestCancelDoesNotMaskLaterStorageFailure(t *testing.T) {
	inRec := make(chan struct{})
	release := make(chan struct{})
	admitted := make(chan struct{})
	collectRelease := make(chan struct{})
	ctrl := &scriptedController{
		collect: func(ctx context.Context, h *scan.ControllerHandle) error {
			if err := admitN(2)(ctx, h); err != nil {
				return err
			}
			close(admitted)
			<-collectRelease
			return ctx.Err()
		},
	}
	rec := &scriptedRecognizer{recognize: func(ctx context.Context, task scan.Task, submit scan.Submit) error {
		if task.Seq == 1 {
			close(inRec)
			<-release
		}
		return nil // terminated without an outcome
	}}
	// Saving fails exactly for the cancelled marks that the stop produces.
	sink := &recordSink{fail: func(rec scan.Record) error {
		if rec.Cause == scan.CauseCancelled {
			return errors.New("checkpoint write failed")
		}
		return nil
	}}
	s := newSession(t, "mask", 2, 1, ctrl, rec, sink)
	<-inRec
	<-admitted
	s.Cancel()
	close(release)        // the running task returns and is marked cancelled
	close(collectRelease) // the producer unwinds
	snap := waitEnded(t, s)
	if snap.End != scan.EndCancelled {
		t.Fatalf("end %s, want cancelled: the user stop stays the reason", snap.End)
	}
	assertTrigger(t, snap, scan.TriggerUser, scan.CodeUserCancelled, scan.PhaseCapturing)
	if !hasFault(snap, scan.CodeStorageFailure) {
		t.Fatalf("the later save failure is masked: %+v", snap.Faults)
	}
	if snap.Counts != (scan.Counts{Captured: 2, Unprocessed: 2}) {
		t.Fatalf("counts %+v, want 2 cancelled unprocessed", snap.Counts)
	}
	reconcile(t, s, sink)
}

func TestUserStopDuringNormalFinalizingIsAbsorbed(t *testing.T) {
	inRec := make(chan struct{})
	release := make(chan struct{})
	ctrl := &scriptedController{collect: admitN(2)}
	rec := &scriptedRecognizer{recognize: func(ctx context.Context, task scan.Task, submit scan.Submit) error {
		if task.Seq == 1 {
			close(inRec)
			<-release // hold the first task to keep the drain open
		}
		return submit(task.SessionID, task.Seq, scan.Recognition{Result: fmt.Sprintf("rec-%d", task.Seq)})
	}}
	sink := &recordSink{}
	s := newSession(t, "absorb", 4, 1, ctrl, rec, sink)
	<-inRec
	waitPhase(t, s, scan.PhaseFinalizing)
	s.Cancel() // the range already completed; the stop must not cancel the drain
	if snap := s.Snapshot(); snap.Phase != scan.PhaseFinalizing || snap.Trigger != nil {
		t.Fatalf("after the absorbed stop: phase %s, trigger %+v", snap.Phase, snap.Trigger)
	}
	close(release)
	snap := waitEnded(t, s)
	if snap.End != scan.EndCompleted {
		t.Fatalf("end %s, want completed: a stop during finalizing changes nothing", snap.End)
	}
	if snap.Trigger != nil {
		t.Fatalf("completed session carries a trigger: %+v", snap.Trigger)
	}
	if snap.Counts != (scan.Counts{Captured: 2, Produced: 2}) {
		t.Fatalf("counts %+v, want 2 produced", snap.Counts)
	}
	reconcile(t, s, sink)
}

func TestRecognizerRuntimeFailureCutsDrain(t *testing.T) {
	inRec := make(chan struct{})
	release := make(chan struct{})
	ctrl := &scriptedController{collect: admitN(3)}
	rec := &scriptedRecognizer{recognize: func(ctx context.Context, task scan.Task, submit scan.Submit) error {
		switch task.Seq {
		case 1:
			close(inRec)
			<-release
			return submit(task.SessionID, task.Seq, scan.Recognition{Result: "rec-1"})
		case 2:
			// A recognizer runtime failure, not a per-task one: the
			// remaining queue cannot run, so the drain is cut.
			return errors.New("ort session died")
		default:
			return submit(task.SessionID, task.Seq, scan.Recognition{Result: "rec-3"})
		}
	}}
	sink := &recordSink{}
	s := newSession(t, "runtime-cut", 4, 1, ctrl, rec, sink)
	<-inRec
	waitPhase(t, s, scan.PhaseFinalizing)
	close(release)
	snap := waitEnded(t, s)
	if snap.End != scan.EndFailed {
		t.Fatalf("end %s, want failed: the runtime failure cut the drain", snap.End)
	}
	assertTrigger(t, snap, scan.TriggerFault, scan.CodeRuntimeFailure, scan.PhaseFinalizing)
	if snap.Counts != (scan.Counts{Captured: 3, Produced: 1, Unprocessed: 2}) {
		t.Fatalf("counts %+v, want 1 produced and 2 interrupted", snap.Counts)
	}
	for _, rec := range sink.all() {
		if rec.Seq == 2 && (rec.State != scan.StateUnprocessed || rec.Cause != scan.CauseInterrupted || rec.Reason != "ort session died") {
			t.Fatalf("seq 2 record %+v, want interrupted with the runtime failure", rec)
		}
		if rec.Seq == 3 && (rec.State != scan.StateUnprocessed || rec.Cause != scan.CauseInterrupted) {
			t.Fatalf("seq 3 record %+v, want interrupted unprocessed", rec)
		}
	}
	reconcile(t, s, sink)
}

func TestConcurrentStopsAndFaults(t *testing.T) {
	inRec1 := make(chan struct{})
	faultGate := make(chan struct{})
	ctrl := &scriptedController{
		collect: func(ctx context.Context, h *scan.ControllerHandle) error {
			if err := admitN(12)(ctx, h); err != nil {
				return err
			}
			<-ctx.Done()
			return ctx.Err()
		},
	}
	rec := &scriptedRecognizer{recognize: func(ctx context.Context, task scan.Task, submit scan.Submit) error {
		if task.Seq == 1 {
			close(inRec1)
			<-faultGate
			// The runtime fault races with the concurrent user stops.
			return errors.New("ort crashed")
		}
		return submit(task.SessionID, task.Seq, scan.Recognition{Result: fmt.Sprintf("rec-%d", task.Seq)})
	}}
	sink := &recordSink{}
	s := newSession(t, "race", 16, 3, ctrl, rec, sink)
	<-inRec1 // task 1 is in recognition before any stop source fires
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Cancel()
		}()
	}
	close(faultGate)
	snap := waitEnded(t, s)
	wg.Wait()
	switch snap.End {
	case scan.EndCancelled:
		// The user stop won the race; the runtime fault must still be on
		// record, not masked by the cancel.
		assertTrigger(t, snap, scan.TriggerUser, scan.CodeUserCancelled, scan.PhaseCapturing)
		if !hasFault(snap, scan.CodeRuntimeFailure) {
			t.Fatalf("the runtime failure is masked by the cancel: %+v", snap.Faults)
		}
	case scan.EndFailed:
		assertTrigger(t, snap, scan.TriggerFault, scan.CodeRuntimeFailure, scan.PhaseCapturing)
	default:
		t.Fatalf("end %s, want cancelled or failed", snap.End)
	}
	if snap.InputPermitted {
		t.Fatal("input permission open after the session ended")
	}
	reconcile(t, s, sink)
}

func TestCountInvariantRevisionMonotonicPermitOneWay(t *testing.T) {
	ctrl := &scriptedController{collect: admitN(30)}
	sink := &recordSink{}
	s := newSession(t, "invariant", 4, 2, ctrl, echoRecognizer(), sink)
	sub := s.Subscribe()

	var readerWG sync.WaitGroup
	readerWG.Add(1)
	go func() { // notification reader: revisions strictly increase
		defer readerWG.Done()
		last := sub.Initial.Revision
		for {
			select {
			case n := <-sub.Changes:
				if n.SessionID != s.ID() {
					t.Errorf("notification names session %q, want %q", n.SessionID, s.ID())
					return
				}
				if n.Revision <= last {
					t.Errorf("notification revision %d did not increase past %d", n.Revision, last)
					return
				}
				last = n.Revision
			case <-s.Done():
				return
			default:
				runtime.Gosched()
			}
		}
	}()

	observerErr := make(chan error, 1)
	go func() { // snapshot observer: invariants at every instant
		lastRev := uint64(0)
		opened, closedAfterOpen := false, false
		for {
			snap := s.Snapshot()
			if snap.SessionID != s.ID() {
				observerErr <- fmt.Errorf("snapshot names session %q", snap.SessionID)
				return
			}
			if snap.Revision < lastRev {
				observerErr <- fmt.Errorf("revision %d went below %d", snap.Revision, lastRev)
				return
			}
			lastRev = snap.Revision
			if snap.Counts.Captured != snap.Counts.Sum() {
				observerErr <- fmt.Errorf("captured %d does not equal the state sum %+v", snap.Counts.Captured, snap.Counts)
				return
			}
			if snap.InputPermitted {
				opened = true
			} else if opened {
				closedAfterOpen = true
			}
			if closedAfterOpen && snap.InputPermitted {
				observerErr <- errors.New("input permission reopened after closing")
				return
			}
			if snap.Phase == scan.PhaseEnded {
				observerErr <- nil
				return
			}
			runtime.Gosched()
		}
	}()

	waitEnded(t, s)
	if err := <-observerErr; err != nil {
		t.Fatal(err)
	}
	readerWG.Wait()
	reconcile(t, s, sink)
}

func TestSlowSubscriberDoesNotBlockKernel(t *testing.T) {
	ctrl := &scriptedController{collect: admitN(50)}
	sink := &recordSink{}
	s := newSession(t, "slow-sub", 4, 2, ctrl, echoRecognizer(), sink)
	// This subscriber never reads its coalesced notification channel.
	sub := s.Subscribe()
	snap := waitEnded(t, s)
	if snap.End != scan.EndCompleted || snap.Counts.Produced != 50 {
		t.Fatalf("end %s with counts %+v, want 50 produced despite the slow subscriber", snap.End, snap.Counts)
	}
	// At most one coalesced notification is buffered; draining it must be
	// possible without blocking.
	notifications := 0
	for {
		select {
		case n := <-sub.Changes:
			if n.Revision > snap.Revision {
				t.Fatalf("notification revision %d beyond the final revision %d", n.Revision, snap.Revision)
			}
			notifications++
		default:
			goto closed
		}
	}
closed:
	if notifications > 1 {
		t.Fatalf("buffered %d notifications, want at most one coalesced", notifications)
	}
	sub.Close()
	sub.Close() // idempotent
	reconcile(t, s, sink)
}

func TestSubscriptionContract(t *testing.T) {
	prepareStarted := make(chan struct{})
	release := make(chan struct{})
	ctrl := &scriptedController{
		prepare: func(ctx context.Context, h *scan.ControllerHandle) error {
			close(prepareStarted)
			<-release
			return nil
		},
		collect: admitN(2),
	}
	sink := &recordSink{}
	s := newSession(t, "contract", 4, 1, ctrl, echoRecognizer(), sink)
	<-prepareStarted
	sub := s.Subscribe()
	if sub.Initial.SessionID != s.ID() || sub.Initial.Phase != scan.PhasePreparing {
		t.Fatalf("initial snapshot %+v, want the preparing phase of this session", sub.Initial)
	}
	if sub.Initial.Revision == 0 {
		t.Fatal("initial snapshot revision is zero")
	}
	close(release)
	waitEnded(t, s)
	got := 0
	last := uint64(0)
	for {
		select {
		case n := <-sub.Changes:
			if n.SessionID != s.ID() {
				t.Fatalf("notification names session %q, want %q", n.SessionID, s.ID())
			}
			if n.Revision <= last {
				t.Fatalf("notification revision %d did not increase past %d", n.Revision, last)
			}
			last = n.Revision
			got++
		default:
			goto done
		}
	}
done:
	if got == 0 {
		t.Fatal("subscriber received no notification although the session ran to the end")
	}
	if last > s.Snapshot().Revision {
		t.Fatalf("notification revision %d beyond the final revision", last)
	}
	sub.Close()
	reconcile(t, s, sink)
}
