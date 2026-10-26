package app

import (
	"context"
	"encoding/json"
	"errors"
	"image"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/game/zzz/collect"
	"github.com/LoTwT/youkai/internal/platform"
	"github.com/LoTwT/youkai/internal/platform/platformfake"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/LoTwT/youkai/internal/scan"
)

// Termination and recovery tests: the application-level contracts that a
// fault keeps the admitted results, that repeated stop entries do not run
// the wrap-up twice, that an explicit exit refuses to report success while
// the final save is missing, and that opening a recovered record never
// resumes scanning on its own.

// admitThenHoldTraversal admits exactly one observation and then holds the
// session open until the scan context ends — the seam that lets a test put
// a watcher fault squarely into the capturing phase after a result exists.
// It records whether input permission was still open when the stop
// unwound, so the "no input after the stop" rule is observable.
type admitThenHoldTraversal struct {
	admitted chan struct{}
	admits   atomic.Int64

	permitAfterStop atomic.Bool
	permitChecked   atomic.Bool
}

func (t *admitThenHoldTraversal) Precheck(context.Context, platform.Session) error { return nil }

func (t *admitThenHoldTraversal) Collect(ctx context.Context, _ platform.Session, h *scan.ControllerHandle) error {
	slot := h.Acquire(ctx)
	if slot == nil {
		return ctx.Err()
	}
	obs := Observation{Lines: []LineCrop{{
		Role:   "name",
		Source: "termination.png",
		Image:  image.NewNRGBA(image.Rect(0, 0, 2, 2)),
	}}}
	if _, err := slot.Admit(obs); err != nil {
		slot.Discard()
		return err
	}
	t.admits.Add(1)
	close(t.admitted)
	<-ctx.Done()
	// The stop must have closed the permission before the controller
	// unwinds; a still-open permission here would mean a new dispatch
	// could follow the fault.
	t.permitAfterStop.Store(h.InputPermitted())
	t.permitChecked.Store(true)
	return ctx.Err()
}

// oneResultRecognizer submits one valid result per task immediately, so a
// fault that follows admission leaves a produced item behind instead of an
// in-flight one.
func oneResultRecognizer() Recognizer {
	return &scriptedRecognizer{recognize: func(ctx context.Context, task scan.Task, submit scan.Submit) error {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		return submit(task.SessionID, task.Seq, scan.Recognition{Result: RecognitionResult{
			Lines: []RecognizedLine{{Role: "name", Text: "disc", Score: 0.9}},
		}})
	}}
}

// TestWatcherFaultKeepsAdmittedResultsAndFinalizesOnce covers focusLost
// and captureStopped: both stop input, keep the accepted result in the
// final checkpoint with the right termination, and release the controller
// exactly once. Repeated stop/fault entries afterwards change nothing.
func TestWatcherFaultKeepsAdmittedResultsAndFinalizesOnce(t *testing.T) {
	for _, tc := range []struct {
		name string
		code string
	}{
		{name: "focusLost", code: platform.CodeFocusLost},
		{name: "captureStopped", code: platform.CodeCaptureStopped},
	} {
		t.Run(tc.name, func(t *testing.T) {
			paths := newTestPaths(t)
			sess := platformfake.NewSession()
			trav := &admitThenHoldTraversal{admitted: make(chan struct{})}
			source := &GameScanSource{
				Port:   &platformfake.Port{NewSession: func() *platformfake.Session { return sess }},
				Target: collect.MacCloudTarget,
				Hotkey: platform.HotkeySpec{Key: platform.KeyS},
				Client: record.ClientMacOSCloud,
				Traversal: func() collect.Traversal {
					return trav
				},
			}
			svc, _ := newTestService(t, paths, source, func() (Recognizer, error) {
				return oneResultRecognizer(), nil
			})

			out, err := svc.Start(StartRequest{ID: "fault-" + tc.name})
			if err != nil {
				t.Fatalf("start: %v", err)
			}
			waitSnapshot(t, svc, scan.PhaseCapturing, "capturing")
			select {
			case <-trav.admitted:
			case <-time.After(10 * time.Second):
				t.Fatal("the traversal never admitted its observation")
			}
			waitFor(t, 10*time.Second, "the admitted observation to produce", func() bool {
				snap := svc.Snapshot().Scan
				return snap.Counts.Captured == 1 && snap.Counts.Produced == 1
			})

			sess.TriggerFault(platform.NewFault(tc.code, "injected "+tc.code))
			waitWrapped(t, svc)

			final := svc.Snapshot()
			if final.Scan.End != scan.EndFailed {
				t.Fatalf("end = %s, want failed after %s", final.Scan.End, tc.code)
			}
			if final.Scan.Trigger == nil || final.Scan.Trigger.Kind != scan.TriggerFault || final.Scan.Trigger.Code != tc.code {
				t.Fatalf("trigger = %+v, want fault %s", final.Scan.Trigger, tc.code)
			}
			if final.Scan.InputPermitted {
				t.Fatal("input permission stayed open after the fault")
			}
			if !trav.permitChecked.Load() || trav.permitAfterStop.Load() {
				t.Fatalf("controller saw input permitted during unwind: checked=%v permitted=%v",
					trav.permitChecked.Load(), trav.permitAfterStop.Load())
			}
			if n := trav.admits.Load(); n != 1 {
				t.Fatalf("admits = %d, want exactly the one before the fault", n)
			}
			_, _, _, dispatched, closes := sess.Counters()
			if dispatched != 0 {
				t.Fatalf("dispatched %d inputs, want none", dispatched)
			}
			if closes != 1 {
				t.Fatalf("platform session closed %d times, want exactly once", closes)
			}

			rec, err := svc.LoadRecord(string(out.SessionID))
			if err != nil {
				t.Fatalf("LoadRecord: %v", err)
			}
			if len(rec.Checkpoint.Items) != 1 || rec.Checkpoint.Items[0].Status != record.StatusProduced {
				t.Fatalf("recovered items = %+v, want the one produced result", rec.Checkpoint.Items)
			}
			if rec.Checkpoint.Session.CapturedCount != 1 {
				t.Fatalf("captured count = %d, want 1", rec.Checkpoint.Session.CapturedCount)
			}
			term := rec.Checkpoint.Session.Termination
			if term == nil || term.Kind != record.TerminationFailed || term.Code != tc.code || term.Phase != record.PhaseCapturing {
				t.Fatalf("termination = %+v, want failed/%s/capturing", term, tc.code)
			}

			// Duplicate stop and fault entries are absorbed: the record
			// revision and the platform close count do not change.
			infos, err := svc.ListRecords()
			if err != nil || len(infos) != 1 {
				t.Fatalf("records = %+v err %v, want one", infos, err)
			}
			revision := infos[0].LatestRevision
			svc.Stop()
			svc.Stop()
			sess.TriggerFault(platform.NewFault(tc.code, "late duplicate"))
			infos, err = svc.ListRecords()
			if err != nil || len(infos) != 1 || infos[0].LatestRevision != revision {
				t.Fatalf("record revision after duplicate stops = %+v (err %v), want %d", infos, err, revision)
			}
			if _, _, _, _, closes := sess.Counters(); closes != 1 {
				t.Fatalf("platform session closed %d times after duplicate stops, want 1", closes)
			}
		})
	}
}

// TestQuitRefusesToFinishWhileTheFinalSaveIsMissing checks requirement 5
// and the explicit-exit rule together: a failed final save is not a
// completed exit. Quit reports the unsaved result and leaves the
// recognizer and data-root occupancy alone; a retry persists the final
// checkpoint, after which the same quit sequence completes. Repeating Quit
// joins the completed teardown.
func TestQuitRefusesToFinishWhileTheFinalSaveIsMissing(t *testing.T) {
	paths := newTestPaths(t)
	source := devFixtureSource(t, 2*checkpointBatchSize, 5*time.Millisecond)
	store := newFlakyStore(t, paths.Data)
	store.failOn = func(call int) bool { return call >= 2 }
	rec := &recordingRecognizer{delegate: newFakeRecognizer()}
	svc := newServiceWithStore(t, paths, store, source, func() (Recognizer, error) { return rec, nil })
	rec.wire(svc)

	if _, err := svc.Start(StartRequest{ID: "quit-save-failure"}); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitFor(t, 30*time.Second, "the ended session with its failed final save", func() bool {
		snap := svc.Snapshot()
		return snap.Scan.Phase == scan.PhaseEnded && snap.Busy && snap.SavePending && snap.SaveError != ""
	})

	if err := svc.Quit(); err == nil || !strings.Contains(err.Error(), "unsaved") {
		t.Fatalf("Quit = %v, want the unsaved-result refusal", err)
	}
	// A repeated quit joins the same refusal; no teardown runs.
	if err := svc.Quit(); err == nil || !strings.Contains(err.Error(), "unsaved") {
		t.Fatalf("repeated Quit = %v, want the same unsaved-result refusal", err)
	}
	if rec.closeCount != 0 {
		t.Fatalf("recognizer closed %d times on a refused quit, want none", rec.closeCount)
	}
	snap := svc.Snapshot()
	if !snap.Busy || snap.SaveError == "" {
		t.Fatalf("snapshot after refused quit = %+v, want busy with the save error", snap)
	}
	if _, err := AcquireDataRootLock(paths.Data); !errors.Is(err, ErrLockHeld) {
		t.Fatalf("data-root occupancy after a refused quit: %v, want ErrLockHeld", err)
	}

	// The durable state is the previous generation: no termination, every
	// item terminal after recovery, counts reconciled.
	infos, err := svc.ListRecords()
	if err != nil || len(infos) != 1 {
		t.Fatalf("records = %+v err %v, want one", infos, err)
	}
	previous, err := svc.LoadRecord(infos[0].ID)
	if err != nil {
		t.Fatalf("LoadRecord of the previous generation: %v", err)
	}
	if previous.Checkpoint.Session.Termination != nil {
		t.Fatalf("previous generation carries termination %+v, want none", previous.Checkpoint.Session.Termination)
	}
	if len(previous.Checkpoint.Items) == 0 {
		t.Fatal("previous generation has no admitted items")
	}
	if int64(len(previous.Checkpoint.Items)) != previous.Checkpoint.Session.CapturedCount {
		t.Fatalf("previous generation captured %d over %d items", previous.Checkpoint.Session.CapturedCount, len(previous.Checkpoint.Items))
	}
	for _, item := range previous.Checkpoint.Items {
		if item.Status == record.StatusQueued || item.Status == record.StatusRunning {
			t.Fatalf("previous generation item %d is %s after recovery", item.Sequence, item.Status)
		}
	}

	store.heal()
	if err := svc.RetrySave(); err != nil {
		t.Fatalf("RetrySave: %v", err)
	}
	snap = waitNotBusy(t, svc)
	if snap.SavedCount != snap.Scan.Counts.Captured {
		t.Fatalf("saved %d of %d captured after retry", snap.SavedCount, snap.Scan.Counts.Captured)
	}

	// The retry wrote the termination the failed wrap-up could not.
	final, err := svc.LoadRecord(infos[0].ID)
	if err != nil {
		t.Fatalf("LoadRecord after retry: %v", err)
	}
	term := final.Checkpoint.Session.Termination
	if term == nil || term.Kind != record.TerminationFailed || term.Code != scan.CodeStorageFailure {
		t.Fatalf("termination after retry = %+v, want failed/storageFailure", term)
	}
	if len(final.Checkpoint.Items) != snap.Scan.Counts.Captured {
		t.Fatalf("final record has %d items, kernel captured %d", len(final.Checkpoint.Items), snap.Scan.Counts.Captured)
	}

	if err := svc.Quit(); err != nil {
		t.Fatalf("Quit after retry: %v", err)
	}
	if rec.closeCount != 1 {
		t.Fatalf("recognizer closed %d times, want exactly once", rec.closeCount)
	}
	taken, err := AcquireDataRootLock(paths.Data)
	if err != nil {
		t.Fatalf("data root after quit: %v", err)
	}
	taken.Close()
	if err := svc.Quit(); err != nil {
		t.Fatalf("repeated Quit: %v", err)
	}
	if rec.closeCount != 1 {
		t.Fatalf("recognizer closed %d times after a repeated quit, want 1", rec.closeCount)
	}
}

// noopDeliverer is a minimal scan.Deliverer for kernels whose records are
// irrelevant to the test.
type noopDeliverer struct{}

func (noopDeliverer) Deliver(scan.Record) error { return nil }

// TestWrapUpDoesNotReviveAStaleSaveFailureAfterRetry pins the race between
// a failed final save and a concurrent retry that already succeeded: the
// wrap-up must not overwrite the newer success with its stale error. A
// revived error would keep the slot busy and make every later Quit refuse
// an exit whose final generation is on disk.
func TestWrapUpDoesNotReviveAStaleSaveFailureAfterRetry(t *testing.T) {
	paths := newTestPaths(t)
	svc, dataLock := newTestService(t, paths, nil, func() (Recognizer, error) {
		return newFakeRecognizer(), nil
	})

	kernel, err := scan.Start(scan.Config{
		ID:         "finished-session",
		Capacity:   1,
		Controller: &scriptedController{},
		Recognizer: newFakeRecognizer(),
		Deliverer:  noopDeliverer{},
	})
	if err != nil {
		t.Fatalf("scan.Start: %v", err)
	}
	select {
	case <-kernel.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("the helper session did not finish")
	}

	store := newFlakyStore(t, paths.Data)
	store.failOn = func(int) bool { return true }
	const id = "wrap-race"
	if err := store.CreateRecord(id); err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}
	deliverer := newRecordDeliverer(store, record.SessionShell{
		SessionID: id,
		Game:      "zzz",
		Entity:    "driveDisc",
		Client:    record.ClientMacOSCloud,
		Locale:    "zh-CN",
		Scope:     json.RawMessage(`{"kind":"wrap-race"}`),
		StartedAt: nowUTC(),
		Coverage:  record.Coverage{Status: record.CoverageUnknown},
	}, nil)
	if err := deliverer.finish(kernel.Snapshot()); err == nil {
		t.Fatal("the injected final-save failure did not fail finish")
	}

	scanLock, err := dataLock.AcquireScanLock()
	if err != nil {
		t.Fatalf("AcquireScanLock: %v", err)
	}
	a := &activeSession{
		id:        scan.ID(id),
		kernel:    kernel,
		deliverer: deliverer,
		scanLock:  scanLock,
		// wrapped simulates the retry that already persisted the final
		// generation before this wrap-up's failed attempt recorded its
		// outcome.
		wrapped:   true,
		wrapDone:  make(chan struct{}),
		kernelSub: kernel.Subscribe(),
	}
	svc.wrapUp(a)

	select {
	case <-a.wrapDone:
	default:
		t.Fatal("wrapUp did not close its done channel")
	}
	if a.saveErr != nil {
		t.Fatalf("stale save error revived after a successful retry: %v", a.saveErr)
	}
	if !a.wrapped {
		t.Fatal("wrapUp unset the retry's completed wrap-up state")
	}
}

// TestFinishRefusesNonTerminalItemsAndRetrySettlesInterrupted pins the
// broken-invariant path: the kernel sealed without a terminal delivery,
// so finish refuses to save and the failure is visible. A retry cannot
// invent the missing outcome; it settles the entry as interrupted
// instead of persisting a completed session with a mid-flight row.
func TestFinishRefusesNonTerminalItemsAndRetrySettlesInterrupted(t *testing.T) {
	dir := t.TempDir()
	store, err := record.Open(dir)
	if err != nil {
		t.Fatalf("record.Open: %v", err)
	}
	const id = "finish-invariant"
	if err := store.CreateRecord(id); err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}
	deliverer := newRecordDeliverer(store, record.SessionShell{
		SessionID: id,
		Game:      "zzz",
		Entity:    "driveDisc",
		Client:    record.ClientMacOSCloud,
		Locale:    "zh-CN",
		Scope:     json.RawMessage(`{"kind":"finish-invariant"}`),
		StartedAt: nowUTC(),
		Coverage:  record.Coverage{Status: record.CoverageComplete},
	}, nil)
	if err := deliverer.Admitted(1, "observation"); err != nil {
		t.Fatalf("Admitted: %v", err)
	}

	err = deliverer.finish(scan.Snapshot{End: scan.EndCompleted})
	if err == nil || !strings.Contains(err.Error(), "still queued") {
		t.Fatalf("finish = %v, want the non-terminal refusal", err)
	}
	if !deliverer.failed() {
		t.Fatal("failed() is false after the refused final generation")
	}
	if st := deliverer.status(); !st.pending || st.err == nil {
		t.Fatalf("status = %+v, want a visible pending failure", st)
	}
	if _, err := store.Peek(id); !errors.Is(err, record.ErrNoValidCheckpoint) {
		t.Fatalf("Peek after the refusal = %v, want no saved generation", err)
	}

	if err := deliverer.retry(); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if deliverer.failed() || !deliverer.settled() {
		t.Fatalf("after retry failed=%v settled=%v, want a settled record",
			deliverer.failed(), deliverer.settled())
	}
	rec, err := store.Load(id)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if rec.Checkpoint.SavedRevision != 1 {
		t.Errorf("saved revision = %d, want 1", rec.Checkpoint.SavedRevision)
	}
	term := rec.Checkpoint.Session.Termination
	if term == nil || term.Kind != record.TerminationInterrupted {
		t.Fatalf("termination = %+v, want interrupted", term)
	}
	item := rec.Checkpoint.Items[0]
	if item.Status != record.StatusUnprocessed || item.Reason != record.ReasonInterrupted {
		t.Fatalf("item = %s/%s, want unprocessed/interrupted", item.Status, item.Reason)
	}
	if rec.Checkpoint.Session.Coverage.Status != record.CoveragePartial {
		t.Errorf("coverage = %q, want the complete claim downgraded", rec.Checkpoint.Session.Coverage.Status)
	}
}

// countingSource counts NewScan calls; LoadRecord must never trigger one.
type countingSource struct {
	mu    sync.Mutex
	calls int
}

func (s *countingSource) NewScan() (ScanSpec, error) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	return ScanSpec{}, errors.New("recovery must not start a scan")
}

func (s *countingSource) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// TestLoadRecordRecoversWithoutResumingTheScan loads a crashed record the
// way the reopen flow does: the newest valid checkpoint comes back with
// the interrupted item marked, the service stays idle, the source is never
// asked for a scan and the scan occupancy stays free. Loading is read-only
// too — no new generation is written and a second load returns the same
// recovery.
func TestLoadRecordRecoversWithoutResumingTheScan(t *testing.T) {
	paths := newTestPaths(t)
	const id = "11111111-1111-4111-8111-111111111111"

	store, err := record.Open(paths.Data)
	if err != nil {
		t.Fatalf("record.Open: %v", err)
	}
	if err := store.CreateRecord(id); err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}
	shell := record.SessionShell{
		SessionID: id,
		Game:      "zzz",
		Entity:    "driveDisc",
		Client:    record.ClientMacOSCloud,
		Locale:    "zh-CN",
		Scope:     json.RawMessage(`{"kind":"recovery"}`),
		StartedAt: nowUTC(),
		Coverage:  record.Coverage{Status: record.CoverageUnknown},
	}
	cp := record.Checkpoint{
		Session: shell,
		Items: []record.ItemRecord{
			{Sequence: 1, Status: record.StatusProduced, Quality: record.QualityNotEvaluated,
				Data: json.RawMessage(`{"name":"disc"}`)},
			{Sequence: 2, Status: record.StatusRunning, Quality: record.QualityNotEvaluated},
		},
	}
	cp.Session.CapturedCount = 2
	if _, err := store.SaveCheckpoint(id, cp); err != nil {
		t.Fatalf("SaveCheckpoint: %v", err)
	}

	source := &countingSource{}
	svc, dataLock := newTestService(t, paths, source, func() (Recognizer, error) {
		return newFakeRecognizer(), nil
	})
	rec, err := svc.LoadRecord(id)
	if err != nil {
		t.Fatalf("LoadRecord: %v", err)
	}
	if len(rec.Interrupted) != 1 || rec.Interrupted[0] != 2 {
		t.Fatalf("interrupted = %v, want [2]", rec.Interrupted)
	}
	if items := rec.Checkpoint.Items; items[0].Status != record.StatusProduced ||
		items[1].Status != record.StatusUnprocessed || items[1].Reason != record.ReasonInterrupted {
		t.Fatalf("recovered items = %+v, want produced then interrupted", items)
	}
	if rec.Checkpoint.Session.Termination != nil {
		t.Fatalf("recovered termination = %+v, want none (the interruption mark)", rec.Checkpoint.Session.Termination)
	}

	if snap := svc.Snapshot(); snap.SessionID != "" || snap.Busy || snap.Scan.Phase != "" {
		t.Fatalf("service after LoadRecord = %+v, want idle with no session", snap)
	}
	if got := source.count(); got != 0 {
		t.Fatalf("NewScan calls after LoadRecord = %d, want none", got)
	}
	scanLock, err := dataLock.AcquireScanLock()
	if err != nil {
		t.Fatalf("scan occupancy after LoadRecord: %v, want it free", err)
	}
	scanLock.Close()

	// Loading is read-only: the newest generation stays the one on disk
	// and the second load returns the same recovery view.
	infos, err := svc.ListRecords()
	if err != nil || len(infos) != 1 || infos[0].LatestRevision != 1 {
		t.Fatalf("records after load = %+v err %v, want revision 1 untouched", infos, err)
	}
	again, err := svc.LoadRecord(id)
	if err != nil {
		t.Fatalf("second LoadRecord: %v", err)
	}
	if again.Checkpoint.SavedRevision != rec.Checkpoint.SavedRevision {
		t.Fatalf("second load revision = %d, want %d", again.Checkpoint.SavedRevision, rec.Checkpoint.SavedRevision)
	}
	// Loading is read-only: the interrupted mark lives in the returned
	// view, not on disk, so every load derives it again and no generation
	// is appended.
	if len(again.Interrupted) != 1 || again.Interrupted[0] != 2 {
		t.Fatalf("second load interrupted = %v, want [2] derived again from the same generation", again.Interrupted)
	}
	if got := source.count(); got != 0 {
		t.Fatalf("NewScan calls after the second load = %d, want none", got)
	}
}
