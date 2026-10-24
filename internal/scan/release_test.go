package scan_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/scan"
)

// Release-path tests (PR 4a): the kernel calls the controller's Release
// exactly once, on the producer goroutine, after the controller returned,
// with input permission already closed and before the finalizer waits for
// the workers and seals. An ended session must never leave a controller's
// platform resources running.

// releaseProbe is a scripted controller that also implements Releaser and
// records what the session looked like at release time. The session is
// assigned right after Start and published over wired, so Release — which
// already runs on the producer goroutine — waits for the assignment
// instead of racing it.
type releaseProbe struct {
	prepare func(ctx context.Context, h *scan.ControllerHandle) error
	collect func(ctx context.Context, h *scan.ControllerHandle) error
	release func() error

	wired chan struct{}
	s     *scan.Session

	mu               sync.Mutex
	releases         int
	collectRuns      int
	releaseSnapshots []scan.Snapshot
	releaseDoneOpen  []bool
}

func (c *releaseProbe) Prepare(ctx context.Context, h *scan.ControllerHandle) error {
	if c.prepare != nil {
		return c.prepare(ctx, h)
	}
	return nil
}

func (c *releaseProbe) Collect(ctx context.Context, h *scan.ControllerHandle) error {
	c.mu.Lock()
	c.collectRuns++
	c.mu.Unlock()
	if c.collect != nil {
		return c.collect(ctx, h)
	}
	return nil
}

func (c *releaseProbe) Release() error {
	<-c.wired
	s := c.s
	snap := s.Snapshot()
	c.mu.Lock()
	c.releases++
	c.releaseSnapshots = append(c.releaseSnapshots, snap)
	select {
	case <-s.Done():
		c.releaseDoneOpen = append(c.releaseDoneOpen, false)
	default:
		c.releaseDoneOpen = append(c.releaseDoneOpen, true)
	}
	rel := c.release
	c.mu.Unlock()
	if rel != nil {
		return rel()
	}
	return nil
}

// counts reports the recorded release facts.
func (c *releaseProbe) counts() (releases, collectRuns int, snaps []scan.Snapshot, doneOpen []bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.releases, c.collectRuns, append([]scan.Snapshot(nil), c.releaseSnapshots...), append([]bool(nil), c.releaseDoneOpen...)
}

// startReleaseSession starts a session around ctrl with a recognizer that
// never produces an outcome (the release path must not depend on
// recognition) and a sink that only counts. The controller's session
// reference is published before the test proceeds.
func startReleaseSession(t *testing.T, id scan.ID, capacity int, ctrl *releaseProbe) (*scan.Session, *recordSink) {
	t.Helper()
	ctrl.wired = make(chan struct{})
	sink := &recordSink{}
	rec := &scriptedRecognizer{recognize: func(ctx context.Context, task scan.Task, submit scan.Submit) error {
		<-ctx.Done() // in-flight recognition ends only on the stop path
		return nil
	}}
	s, err := scan.Start(scan.Config{
		ID:         id,
		Capacity:   capacity,
		Workers:    1,
		Controller: ctrl,
		Recognizer: rec,
		Deliverer:  sink,
	})
	if err != nil {
		t.Fatalf("scan.Start: %v", err)
	}
	ctrl.s = s
	close(ctrl.wired)
	return s, sink
}

// waitEnd waits for the session to seal.
func waitEnd(t *testing.T, s *scan.Session) scan.Snapshot {
	t.Helper()
	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the session to end")
	}
	return s.Snapshot()
}

// K1: a Prepare error still releases exactly once, before Done, with
// input permission closed.
func TestReleaseAfterPrepareError(t *testing.T) {
	ctrl := &releaseProbe{prepare: func(context.Context, *scan.ControllerHandle) error {
		return scan.NewFault("permissionMissing", nil)
	}}
	s, _ := startReleaseSession(t, "rel-prepare-err", 1, ctrl)
	snap := waitEnd(t, s)

	releases, collectRuns, snaps, doneOpen := ctrl.counts()
	if releases != 1 {
		t.Fatalf("Release calls: %d, want 1", releases)
	}
	if collectRuns != 0 {
		t.Fatalf("Collect ran %d times after a failed Prepare", collectRuns)
	}
	if len(snaps) != 1 || snaps[0].InputPermitted {
		t.Fatalf("at release time input must be closed: %+v", snaps)
	}
	if len(doneOpen) != 1 || !doneOpen[0] {
		t.Fatal("Release must run before Done closed")
	}
	if snap.End != scan.EndFailed || snap.Trigger == nil || snap.Trigger.Code != "permissionMissing" {
		t.Fatalf("session should fail with the controller's code: %+v", snap)
	}
}

// K2: a stop accepted while preparing releases exactly once and the
// session ends cancelled, even though the controller unwinds with the
// context error.
func TestReleaseAfterStopDuringPrepare(t *testing.T) {
	inPrepare := make(chan struct{})
	unblock := make(chan struct{})
	ctrl := &releaseProbe{prepare: func(ctx context.Context, _ *scan.ControllerHandle) error {
		close(inPrepare)
		<-unblock
		return ctx.Err()
	}}
	s, _ := startReleaseSession(t, "rel-stop-prepare", 1, ctrl)
	<-inPrepare
	s.Cancel()
	close(unblock)
	snap := waitEnd(t, s)

	releases, collectRuns, _, doneOpen := ctrl.counts()
	if releases != 1 {
		t.Fatalf("Release calls: %d, want 1", releases)
	}
	if collectRuns != 0 {
		t.Fatalf("Collect must not run after a stop during Prepare, ran %d", collectRuns)
	}
	if len(doneOpen) != 1 || !doneOpen[0] {
		t.Fatal("Release must run before Done closed")
	}
	if snap.End != scan.EndCancelled {
		t.Fatalf("end %s, want cancelled", snap.End)
	}
}

// K3: Prepare returns nil but a stop raced ahead of enterCapturing:
// Collect is never called, and Release still runs exactly once.
func TestReleaseWhenStopWonEnterCapturingRace(t *testing.T) {
	inPrepare := make(chan struct{})
	unblock := make(chan struct{})
	ctrl := &releaseProbe{prepare: func(context.Context, *scan.ControllerHandle) error {
		close(inPrepare)
		<-unblock
		return nil // the controller finished its prechecks happily
	}}
	s, _ := startReleaseSession(t, "rel-race-capturing", 1, ctrl)
	<-inPrepare
	s.Cancel()
	close(unblock)
	snap := waitEnd(t, s)

	releases, collectRuns, _, _ := ctrl.counts()
	if releases != 1 {
		t.Fatalf("Release calls: %d, want 1", releases)
	}
	if collectRuns != 0 {
		t.Fatalf("Collect must not run when the stop won the race, ran %d", collectRuns)
	}
	if snap.End != scan.EndCancelled {
		t.Fatalf("end %s, want cancelled", snap.End)
	}
}

// K4: a Collect error releases exactly once and the diagnostic code
// survives.
func TestReleaseAfterCollectError(t *testing.T) {
	ctrl := &releaseProbe{collect: func(context.Context, *scan.ControllerHandle) error {
		return scan.NewFault("targetLost", errors.New("window gone"))
	}}
	s, _ := startReleaseSession(t, "rel-collect-err", 1, ctrl)
	snap := waitEnd(t, s)

	releases, collectRuns, snaps, doneOpen := ctrl.counts()
	if releases != 1 {
		t.Fatalf("Release calls: %d, want 1", releases)
	}
	if collectRuns != 1 {
		t.Fatalf("Collect runs: %d, want 1", collectRuns)
	}
	if len(snaps) != 1 || snaps[0].InputPermitted {
		t.Fatalf("at release time input must be closed: %+v", snaps)
	}
	if len(doneOpen) != 1 || !doneOpen[0] {
		t.Fatal("Release must run before Done closed")
	}
	if snap.End != scan.EndFailed || snap.Trigger == nil || snap.Trigger.Code != "targetLost" {
		t.Fatalf("session should fail with targetLost: %+v", snap)
	}
}

// K5: a normal completion releases after the outcome was decided
// (permit closed, phase finalizing) and before Done.
func TestReleaseBeforeFinalizingSealsCompletion(t *testing.T) {
	ctrl := &releaseProbe{}
	s, _ := startReleaseSession(t, "rel-complete", 1, ctrl)
	snap := waitEnd(t, s)

	releases, collectRuns, snaps, doneOpen := ctrl.counts()
	if releases != 1 {
		t.Fatalf("Release calls: %d, want 1", releases)
	}
	if collectRuns != 1 {
		t.Fatalf("Collect runs: %d, want 1", collectRuns)
	}
	if len(snaps) != 1 {
		t.Fatalf("release snapshots: %+v", snaps)
	}
	rs := snaps[0]
	if rs.InputPermitted || rs.Phase != scan.PhaseFinalizing {
		t.Fatalf("at release time the outcome must be decided (permit closed, finalizing): %+v", rs)
	}
	if len(doneOpen) != 1 || !doneOpen[0] {
		t.Fatal("Release must run before Done closed")
	}
	if snap.End != scan.EndCompleted {
		t.Fatalf("end %s, want completed", snap.End)
	}
}

// K6: a Release error is recorded as a releaseFailed fault and never
// changes the outcome — completed stays completed, failed stays failed.
func TestReleaseErrorRecordedNotOutcomeChanging(t *testing.T) {
	t.Run("completed", func(t *testing.T) {
		ctrl := &releaseProbe{release: func() error {
			return errors.New("teardown timeout")
		}}
		s, _ := startReleaseSession(t, "rel-err-complete", 1, ctrl)
		snap := waitEnd(t, s)
		if snap.End != scan.EndCompleted {
			t.Fatalf("end %s, want completed despite the release error", snap.End)
		}
		found := false
		for _, f := range snap.Faults {
			if f.Code == scan.CodeReleaseFailed {
				found = true
			}
		}
		if !found {
			t.Fatalf("releaseFailed fault missing: %+v", snap.Faults)
		}
	})
	t.Run("failed keeps its code", func(t *testing.T) {
		ctrl := &releaseProbe{
			collect: func(context.Context, *scan.ControllerHandle) error {
				return scan.NewFault("captureStopped", nil)
			},
			release: func() error { return errors.New("unregister failed") },
		}
		s, _ := startReleaseSession(t, "rel-err-failed", 1, ctrl)
		snap := waitEnd(t, s)
		if snap.End != scan.EndFailed || snap.Trigger == nil || snap.Trigger.Code != "captureStopped" {
			t.Fatalf("the release error must not mask the original fault: %+v", snap)
		}
		found := false
		for _, f := range snap.Faults {
			if f.Code == scan.CodeReleaseFailed {
				found = true
			}
		}
		if !found {
			t.Fatalf("releaseFailed fault missing: %+v", snap.Faults)
		}
	})
}

// K8: the producer blocked in Acquire while another goroutine reports a
// fault through Fail: Acquire returns nil, the session ends failed with
// the reported code. Fail and InputPermitted are safe from any goroutine;
// this test runs under -race in the acceptance suite.
func TestFailFromOtherGoroutineUnblocksAcquire(t *testing.T) {
	blocked := make(chan struct{})
	ctrl := &releaseProbe{}
	ctrl.prepare = func(ctx context.Context, h *scan.ControllerHandle) error {
		// A watcher-style reporter on its own goroutine, as the platform
		// session's watcher will do.
		go func() {
			<-blocked
			if h.InputPermitted() {
				h.Fail(scan.NewFault("focusLost", nil))
			}
		}()
		return nil
	}
	ctrl.collect = func(ctx context.Context, h *scan.ControllerHandle) error {
		// Fill the pipeline (capacity 1) with an observation whose
		// recognition never finishes, so the next Acquire must wait.
		slot := h.Acquire(ctx)
		if slot == nil {
			return ctx.Err()
		}
		if _, err := slot.Admit("obs-1"); err != nil {
			slot.Discard()
			return err
		}
		close(blocked)
		next := h.Acquire(ctx)
		if next != nil {
			next.Discard()
			t.Error("Acquire must return nil once the session stopped")
		}
		return ctx.Err()
	}
	s, _ := startReleaseSession(t, "rel-fail-unblock", 1, ctrl)
	snap := waitEnd(t, s)

	if snap.End != scan.EndFailed || snap.Trigger == nil || snap.Trigger.Code != "focusLost" {
		t.Fatalf("session should fail with focusLost: %+v", snap)
	}
	if snap.Counts.Captured != 1 || snap.Counts.Sum() != snap.Counts.Captured {
		t.Fatalf("accounting broken: %+v", snap.Counts)
	}
	releases, _, _, _ := ctrl.counts()
	if releases != 1 {
		t.Fatalf("Release calls: %d, want 1", releases)
	}
}

// K9: a watcher fault reported while a range-complete session is releasing
// its controller is recorded as an additional fault. It must not turn the
// completed traversal into a failed session or interrupt the final drain.
func TestFaultDuringReleaseKeepsCompletion(t *testing.T) {
	var handle *scan.ControllerHandle
	ctrl := &releaseProbe{
		collect: func(_ context.Context, h *scan.ControllerHandle) error {
			handle = h
			return nil
		},
		release: func() error {
			handle.Fail(scan.NewFault("focusLost", nil))
			return nil
		},
	}
	s, _ := startReleaseSession(t, "rel-fault-complete", 1, ctrl)
	snap := waitEnd(t, s)

	if snap.End != scan.EndCompleted {
		t.Fatalf("end %s, want completed despite a release-time watcher fault", snap.End)
	}
	if snap.Trigger != nil {
		t.Fatalf("trigger %+v, want none for a completed session", snap.Trigger)
	}
	found := false
	for _, f := range snap.Faults {
		if f.Code == "focusLost" {
			found = true
		}
	}
	if !found {
		t.Fatalf("release-time fault missing from the additional faults: %+v", snap.Faults)
	}
}
