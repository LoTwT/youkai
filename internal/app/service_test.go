package app

import (
	"context"
	"errors"
	"fmt"
	"image"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/record"
	"github.com/LoTwT/youkai/internal/scan"
)

// slowPace keeps a session open for the whole test; every test that uses
// it stops the session before it ends on its own.
const slowPace = time.Hour

func TestStartReplayBusyAndFreeSlot(t *testing.T) {
	paths := newTestPaths(t)
	source := devFixtureSource(t, 2, slowPace)
	svc, _ := newTestService(t, paths, source, func() (Recognizer, error) { return newFakeRecognizer(), nil })

	first, err := svc.Start(StartRequest{ID: "r1"})
	if err != nil {
		t.Fatalf("start r1: %v", err)
	}
	if first.SessionID == "" || first.Busy || first.Replayed {
		t.Fatalf("first start = %+v, want a fresh accepted session", first)
	}

	// The same request again: its session, still active, marked busy.
	replay, err := svc.Start(StartRequest{ID: "r1"})
	if err != nil {
		t.Fatalf("replay r1: %v", err)
	}
	if replay.SessionID != first.SessionID || !replay.Replayed || !replay.Busy {
		t.Fatalf("replay = %+v, want %s replayed busy", replay, first.SessionID)
	}

	// A different request while the slot is occupied: the active session
	// and a busy marker, not an error.
	rival, err := svc.Start(StartRequest{ID: "r2"})
	if err != nil {
		t.Fatalf("start r2 while busy: %v", err)
	}
	if rival.SessionID != first.SessionID || rival.Replayed || !rival.Busy {
		t.Fatalf("busy start = %+v, want %s busy", rival, first.SessionID)
	}

	svc.Stop()
	snap := waitNotBusy(t, svc)
	if snap.Scan.End != scan.EndCancelled {
		t.Fatalf("end = %s, want cancelled", snap.Scan.End)
	}
	if snap.Scan.InputPermitted {
		t.Fatalf("input still permitted after a stop; stop never reopens")
	}

	// After the wrap-up, the same request returns the ended session —
	// a stop never turns a replay into a new start.
	afterStop, err := svc.Start(StartRequest{ID: "r1"})
	if err != nil {
		t.Fatalf("replay r1 after stop: %v", err)
	}
	if afterStop.SessionID != first.SessionID || !afterStop.Replayed || afterStop.Busy {
		t.Fatalf("replay after stop = %+v, want %s replayed and free", afterStop, first.SessionID)
	}
	records, err := svc.ListRecords()
	if err != nil {
		t.Fatalf("ListRecords: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("records after replay = %+v, want the one stopped session", records)
	}

	// The freed slot accepts a different request and starts a new one.
	next, err := svc.Start(StartRequest{ID: "r2"})
	if err != nil {
		t.Fatalf("start r2 after wrap: %v", err)
	}
	if next.SessionID == first.SessionID || next.Busy || next.Replayed {
		t.Fatalf("start r2 after wrap = %+v, want a fresh session", next)
	}
	svc.Stop()
	waitNotBusy(t, svc)
}

func TestStartInvalidRequests(t *testing.T) {
	paths := newTestPaths(t)
	// No source wired: the M1 production state.
	svc, _ := newTestService(t, paths, nil, func() (Recognizer, error) { return newFakeRecognizer(), nil })
	if _, err := svc.Start(StartRequest{ID: "r1"}); err == nil || !strings.Contains(err.Error(), "no scan source") {
		t.Fatalf("start without a source: err = %v, want the no-source refusal", err)
	}
	if _, err := svc.Start(StartRequest{}); err == nil || !strings.Contains(err.Error(), "request id is empty") {
		t.Fatalf("start without a request id: err = %v, want the empty-id refusal", err)
	}

	// A broken spec is refused before anything is persisted.
	brokenSpec := specOf(newGatedController())
	brokenSpec.Scope = nil
	broken := staticSource{spec: brokenSpec}
	svc2, _ := newTestService(t, newTestPaths(t), broken, func() (Recognizer, error) { return newFakeRecognizer(), nil })
	if _, err := svc2.Start(StartRequest{ID: "r1"}); err == nil || !strings.Contains(err.Error(), "scope") {
		t.Fatalf("start with a broken spec: err = %v, want the spec refusal", err)
	}
	snap := svc2.Snapshot()
	if snap.SessionID != "" {
		t.Fatalf("refused start left session %s in the slot", snap.SessionID)
	}
	if records, err := svc2.ListRecords(); err != nil || len(records) != 0 {
		t.Fatalf("refused start persisted %v (err %v), want nothing", records, err)
	}
}

// TestStartShowsPreparingWhileRecognizerLoads pins the placeholder
// semantics: acceptance occupies the slot at once and the snapshot shows
// preparing while the slow recognizer factory has not returned yet.
func TestStartShowsPreparingWhileRecognizerLoads(t *testing.T) {
	paths := newTestPaths(t)
	source := devFixtureSource(t, 1, 0)
	loaded := make(chan struct{})
	var once sync.Once
	svc, _ := newTestService(t, paths, source, func() (Recognizer, error) {
		<-loaded // stand in for the model load
		return newFakeRecognizer(), nil
	})

	started := make(chan StartOutcome, 1)
	go func() {
		out, err := svc.Start(StartRequest{ID: "r1"})
		if err != nil {
			t.Errorf("start: %v", err)
		}
		started <- out
	}()
	waitFor(t, 10*time.Second, "the accepted placeholder", func() bool {
		return svc.Snapshot().SessionID != ""
	})
	snap := svc.Snapshot()
	if snap.Busy != true || snap.Scan.Phase != scan.PhasePreparing || snap.Scan.SessionID == "" {
		t.Fatalf("snapshot while preparing = %+v, want busy preparing with the session id", snap)
	}
	// A second request meets the occupied slot.
	rival, err := svc.Start(StartRequest{ID: "r2"})
	if err != nil || !rival.Busy {
		t.Fatalf("start while preparing = %+v err %v, want busy", rival, err)
	}
	once.Do(func() { close(loaded) })
	out := <-started
	if out.SessionID != snap.SessionID {
		t.Fatalf("committed session = %s, want the placeholder %s", out.SessionID, snap.SessionID)
	}
	waitNotBusy(t, svc)
}

// TestDelivererBatchesAndSavesFinalCheckpoint drives 42 observations
// through the service: batches of 16 produce mid-session generations and
// the wrap-up writes the final one with the termination.
func TestDelivererBatchesAndSavesFinalCheckpoint(t *testing.T) {
	paths := newTestPaths(t)
	source := devFixtureSource(t, 42, 0)
	svc, _ := newTestService(t, paths, source, func() (Recognizer, error) { return newFakeRecognizer(), nil })

	out, err := svc.Start(StartRequest{ID: "r1"})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	snap := waitNotBusy(t, svc)
	if snap.Scan.End != scan.EndCompleted || snap.Scan.Counts.Captured != 42 || snap.Scan.Counts.Produced != 42 {
		t.Fatalf("snapshot = end %s counts %+v, want completed with 42 produced", snap.Scan.End, snap.Scan.Counts)
	}
	if snap.SavedCount != 42 {
		t.Fatalf("saved count = %d, want 42", snap.SavedCount)
	}

	records, err := svc.ListRecords()
	if err != nil {
		t.Fatalf("ListRecords: %v", err)
	}
	if len(records) != 1 || records[0].ID != string(out.SessionID) {
		t.Fatalf("records = %+v, want %s", records, out.SessionID)
	}
	// 42 items at batch size 16: generations at 16 and 32 mid-session,
	// plus the final one — the batching is visible in the revision.
	if want := int64(3); records[0].LatestRevision != want {
		t.Fatalf("latest revision = %d, want %d (two batches plus the final save)", records[0].LatestRevision, want)
	}

	rec, err := svc.LoadRecord(string(out.SessionID))
	if err != nil {
		t.Fatalf("LoadRecord: %v", err)
	}
	sh := rec.Checkpoint.Session
	if len(rec.Checkpoint.Items) != 42 || sh.CapturedCount != 42 {
		t.Fatalf("record has %d items and captured count %d, want 42", len(rec.Checkpoint.Items), sh.CapturedCount)
	}
	if sh.Termination == nil || sh.Termination.Kind != record.TerminationCompleted ||
		sh.Termination.Code != scan.CodeVerifiedEnd || sh.Termination.Phase != record.PhaseCapturing {
		t.Fatalf("termination = %+v, want completed/verifiedEnd/capturing", sh.Termination)
	}
	if sh.EndedAt == nil {
		t.Fatalf("record has no end time")
	}
	for i, item := range rec.Checkpoint.Items {
		if item.Sequence != int64(i+1) || item.Status != record.StatusProduced {
			t.Fatalf("item %d = seq %d status %s, want %d produced", i, item.Sequence, item.Status, i+1)
		}
		if len(item.EvidenceIDs) != 1 || item.EvidenceIDs[0] != evidenceID(scan.Seq(i+1), 0) {
			t.Fatalf("item %d evidence = %v, want its one line crop", i, item.EvidenceIDs)
		}
		if len(item.Observed) == 0 || len(item.Data) == 0 {
			t.Fatalf("item %d has no observed or data payload", i)
		}
	}
}

// TestSaveFailureStopsCollectionAndFinalRetryReconciles injects one
// failing save (the first batch): the kernel stops collection with a
// storage fault, the wrap-up retries, and the final checkpoint accounts
// for every admitted sequence.
func TestSaveFailureStopsCollectionAndFinalRetryReconciles(t *testing.T) {
	paths := newTestPaths(t)
	// The paced source keeps the session capturing while recognition
	// keeps up, so the injected save fault cuts the scan instead of
	// landing in the drain.
	source := devFixtureSource(t, 42, 5*time.Millisecond)
	store := newFlakyStore(t, paths.Data)
	store.failOn = func(call int) bool { return call == 1 }
	svc := newServiceWithStore(t, paths, store, source, func() (Recognizer, error) { return newFakeRecognizer(), nil })

	if _, err := svc.Start(StartRequest{ID: "r1"}); err != nil {
		t.Fatalf("start: %v", err)
	}
	snap := waitNotBusy(t, svc)
	if snap.Scan.End != scan.EndFailed {
		t.Fatalf("end = %s, want failed after the storage fault", snap.Scan.End)
	}
	if !hasFaultCode(snap.Scan, scan.CodeStorageFailure) {
		t.Fatalf("faults = %+v, want a storage failure recorded", snap.Scan.Faults)
	}
	// Every admitted sequence is accounted for: the saved items equal
	// the kernel's captured count.
	records, err := svc.ListRecords()
	if err != nil || len(records) != 1 {
		t.Fatalf("records = %+v err %v, want the one session", records, err)
	}
	rec, err := svc.LoadRecord(records[0].ID)
	if err != nil {
		t.Fatalf("LoadRecord: %v", err)
	}
	if len(rec.Checkpoint.Items) != snap.Scan.Counts.Captured {
		t.Fatalf("record has %d items, kernel captured %d; the reconciliation broke",
			len(rec.Checkpoint.Items), snap.Scan.Counts.Captured)
	}
	if rec.Checkpoint.Session.Termination == nil ||
		rec.Checkpoint.Session.Termination.Kind != record.TerminationFailed ||
		rec.Checkpoint.Session.Termination.Code != scan.CodeStorageFailure {
		t.Fatalf("termination = %+v, want failed/storageFailure", rec.Checkpoint.Session.Termination)
	}
}

// TestSaveFailureVisibleRetryableKeepsPreviousGeneration fails every
// save after the first batch: the failure is visible in the snapshot,
// the previous generation stands, the slot stays occupied, and a retry
// after healing persists everything.
func TestSaveFailureVisibleRetryableKeepsPreviousGeneration(t *testing.T) {
	paths := newTestPaths(t)
	source := devFixtureSource(t, 42, 5*time.Millisecond)
	store := newFlakyStore(t, paths.Data)
	store.failOn = func(call int) bool { return call >= 2 }
	svc := newServiceWithStore(t, paths, store, source, func() (Recognizer, error) { return newFakeRecognizer(), nil })

	if _, err := svc.Start(StartRequest{ID: "r1"}); err != nil {
		t.Fatalf("start: %v", err)
	}
	// The wrap-up's final save failed: the session ended, but the slot
	// stays occupied and the failure is visible.
	waitFor(t, 30*time.Second, "the ended session with its failed save", func() bool {
		snap := svc.Snapshot()
		return snap.Scan.Phase == scan.PhaseEnded && snap.Busy && snap.SavePending && snap.SaveError != ""
	})
	snap := svc.Snapshot()
	if snap.Scan.End != scan.EndFailed {
		t.Fatalf("end = %s, want failed (the storage fault cut the scan)", snap.Scan.End)
	}
	// The previous valid generation is the last durable state: 16 items
	// from the first batch, no termination yet.
	records, err := svc.ListRecords()
	if err != nil || len(records) != 1 {
		t.Fatalf("records = %+v err %v", records, err)
	}
	rec, err := svc.LoadRecord(records[0].ID)
	if err != nil {
		t.Fatalf("LoadRecord: %v", err)
	}
	if len(rec.Checkpoint.Items) != checkpointBatchSize || rec.Checkpoint.Session.Termination != nil {
		t.Fatalf("durable state = %d items, termination %+v; want the first batch's 16 items without termination",
			len(rec.Checkpoint.Items), rec.Checkpoint.Session.Termination)
	}
	// A new start is refused while the result is unsaved.
	if rival, err := svc.Start(StartRequest{ID: "r2"}); err != nil || !rival.Busy {
		t.Fatalf("start while a save is pending = %+v err %v, want busy", rival, err)
	}

	store.heal()
	if err := svc.RetrySave(); err != nil {
		t.Fatalf("RetrySave: %v", err)
	}
	snap = waitNotBusy(t, svc)
	if snap.SavePending || snap.SaveError != "" || snap.SavedCount != snap.Scan.Counts.Captured {
		t.Fatalf("after retry = %+v, want everything saved and the slot free", snap)
	}
	rec, err = svc.LoadRecord(records[0].ID)
	if err != nil {
		t.Fatalf("LoadRecord after retry: %v", err)
	}
	if len(rec.Checkpoint.Items) != snap.Scan.Counts.Captured || rec.Checkpoint.Session.Termination == nil {
		t.Fatalf("record after retry = %d items termination %+v, want everything with the termination",
			len(rec.Checkpoint.Items), rec.Checkpoint.Session.Termination)
	}
	// With nothing pending, a retry is a no-op and a new start works.
	if err := svc.RetrySave(); err != nil {
		t.Fatalf("idle RetrySave: %v", err)
	}
	if next, err := svc.Start(StartRequest{ID: "r2"}); err != nil || next.Busy {
		t.Fatalf("start after retry = %+v err %v, want a fresh session", next, err)
	}
	svc.Stop()
	waitNotBusy(t, svc)
}

// TestEvidenceFailureIsRetriedAtWrapUp injects one failing evidence
// write: the affected item cannot be persisted during the scan, the
// session fails, and the wrap-up's retry writes the evidence and saves
// the complete record.
func TestEvidenceFailureIsRetriedAtWrapUp(t *testing.T) {
	paths := newTestPaths(t)
	// One admitted observation, then the controller holds the session
	// open: the evidence failure of that one delivery lands squarely in
	// the capturing phase and cuts the scan.
	gate := newAdmitThenGateController()
	store := newFlakyStore(t, paths.Data)
	store.failEvidence = func(call int) bool { return call == 1 }
	svc := newServiceWithStore(t, paths, store, staticSource{spec: specOf(gate)}, func() (Recognizer, error) { return newFakeRecognizer(), nil })

	if _, err := svc.Start(StartRequest{ID: "r1"}); err != nil {
		t.Fatalf("start: %v", err)
	}
	snap := waitNotBusy(t, svc)
	if snap.Scan.End != scan.EndFailed || !hasFaultCode(snap.Scan, scan.CodeStorageFailure) {
		t.Fatalf("snapshot = end %s faults %+v, want failed with the storage fault", snap.Scan.End, snap.Scan.Faults)
	}
	records, err := svc.ListRecords()
	if err != nil || len(records) != 1 {
		t.Fatalf("records = %+v err %v", records, err)
	}
	rec, err := svc.LoadRecord(records[0].ID)
	if err != nil {
		t.Fatalf("LoadRecord: %v", err)
	}
	if len(rec.Checkpoint.Items) != snap.Scan.Counts.Captured {
		t.Fatalf("record has %d items, kernel captured %d", len(rec.Checkpoint.Items), snap.Scan.Counts.Captured)
	}
	// The item whose evidence failed first is present and produced,
	// with its evidence written by the retry.
	first := rec.Checkpoint.Items[0]
	if first.Status != record.StatusProduced || len(first.EvidenceIDs) != 1 {
		t.Fatalf("first item = %s with evidence %v, want produced with its crop", first.Status, first.EvidenceIDs)
	}
}

// TestCrashedSessionReopensAsInterrupted simulates a crash between two
// saves at the deliverer level: the kernel delivers everything, but the
// wrap-up's final checkpoint never runs. Reopening the record shows the
// interruption mark — a session shell without a termination.
func TestCrashedSessionReopensAsInterrupted(t *testing.T) {
	paths := newTestPaths(t)
	setScanLockBase(t, t.TempDir())
	store, err := record.Open(paths.Data)
	if err != nil {
		t.Fatalf("record.Open: %v", err)
	}
	id := "crash-sim"
	if err := store.CreateRecord(id); err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}
	shell := record.SessionShell{
		SessionID: id,
		Game:      "zzz",
		Entity:    "driveDisc",
		Client:    record.ClientWindowsLocal,
		Locale:    "zh-CN",
		Scope:     []byte(`{"kind":"test"}`),
		StartedAt: nowUTC(),
		Coverage:  record.Coverage{Status: record.CoverageUnknown},
	}
	deliverer := newRecordDeliverer(store, shell, nil)
	session, err := scan.Start(scan.Config{
		ID:         scan.ID(id),
		Capacity:   2,
		Controller: admitNController(20),
		Recognizer: newFakeRecognizer(),
		Deliverer:  deliverer,
	})
	if err != nil {
		t.Fatalf("scan.Start: %v", err)
	}
	// Wait for the first batch's save, then for the kernel to finish —
	// and never call finish: the process "died" with the document at 16
	// saved and the rest only in memory.
	waitFor(t, 30*time.Second, "the first batch save", func() bool {
		return deliverer.status().saved == checkpointBatchSize
	})
	<-session.Done()

	// Reopen with a fresh store on the same root, the way a restarted
	// process does.
	reopened, err := record.Open(paths.Data)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	rec, err := reopened.Load(id)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(rec.Checkpoint.Items) != checkpointBatchSize {
		t.Fatalf("recovered %d items, want the %d the crash had saved", len(rec.Checkpoint.Items), checkpointBatchSize)
	}
	// The interruption mark: the shell has no termination, so the
	// record reads as a session that never finished. The writer
	// checkpoints only terminal states, so nothing arrives queued or
	// running; the coverage stays unknown and is never upgraded.
	if rec.Checkpoint.Session.Termination != nil {
		t.Fatalf("recovered termination %+v, want none (the interruption mark)", rec.Checkpoint.Session.Termination)
	}
	if rec.Checkpoint.Session.Coverage.Status != record.CoverageUnknown || rec.CoverageDowngraded {
		t.Fatalf("coverage = %q downgraded %v, want unknown untouched",
			rec.Checkpoint.Session.Coverage.Status, rec.CoverageDowngraded)
	}
	if len(rec.Interrupted) != 0 {
		t.Fatalf("interrupted = %v; this writer checkpoints only terminal states", rec.Interrupted)
	}
	infos, err := reopened.ListRecords()
	if err != nil || len(infos) != 1 || infos[0].ID != id || infos[0].LatestRevision != 1 {
		t.Fatalf("listing = %+v err %v, want the interrupted record at revision 1", infos, err)
	}
}

// TestQuitStopsSessionThenClosesRecognizerThenReleasesOccupancy checks
// the explicit-exit order: the recognizer closes only after the session
// ended (with no recognition in flight), the data-root occupancy frees
// only after the teardown, and repeated quits join the same sequence.
func TestQuitStopsSessionThenClosesRecognizerThenReleasesOccupancy(t *testing.T) {
	paths := newTestPaths(t)
	gate := newGatedController()
	source := staticSource{spec: specOf(gate)}
	rec := &recordingRecognizer{delegate: newFakeRecognizer()}
	svc, dataLock := newTestService(t, paths, source, func() (Recognizer, error) { return rec, nil })
	rec.wire(svc)

	if _, err := svc.Start(StartRequest{ID: "r1"}); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitFor(t, 10*time.Second, "the capturing phase", func() bool {
		return svc.Snapshot().Scan.Phase == scan.PhaseCapturing
	})
	if _, err := dataLock.AcquireScanLock(); !errors.Is(err, ErrLockHeld) {
		t.Fatalf("scan occupancy during the session: err = %v, want ErrLockHeld", err)
	}

	quitErr := make(chan error, 1)
	go func() { quitErr <- svc.Quit() }()
	waitFor(t, 30*time.Second, "the quit to finish", func() bool {
		return len(quitErr) > 0 // peek, do not consume the result
	})
	if err := <-quitErr; err != nil {
		t.Fatalf("Quit: %v", err)
	}

	snap := svc.Snapshot()
	if snap.Scan.Phase != scan.PhaseEnded || snap.Busy {
		t.Fatalf("snapshot after quit = phase %s busy %v, want ended and free", snap.Scan.Phase, snap.Busy)
	}
	if rec.closeCount != 1 {
		t.Fatalf("recognizer closed %d times, want exactly once", rec.closeCount)
	}
	if rec.inFlightAtCls != 0 {
		t.Fatalf("recognizer closed with %d calls in flight; the session must end first", rec.inFlightAtCls)
	}
	if rec.phaseAtClose != string(scan.PhaseEnded) {
		t.Fatalf("session phase at recognizer close = %q, want %q", rec.phaseAtClose, scan.PhaseEnded)
	}
	// The data-root occupancy freed with the teardown: this process can
	// take the same root again.
	taken, err := AcquireDataRootLock(paths.Data)
	if err != nil {
		t.Fatalf("data-root occupancy after quit: %v", err)
	}
	taken.Close()

	// A repeated quit returns the same outcome without redoing work.
	if err := svc.Quit(); err != nil {
		t.Fatalf("second Quit: %v", err)
	}
	if rec.closeCount != 1 {
		t.Fatalf("recognizer closed %d times after a repeated quit, want 1", rec.closeCount)
	}
	// Starts are refused once the service is quitting.
	if _, err := svc.Start(StartRequest{ID: "r2"}); err == nil || !strings.Contains(err.Error(), "quitting") {
		t.Fatalf("start after quit: err = %v, want the quitting refusal", err)
	}
}

// TestScanLockReleasedAfterSessionEnd pins the scan occupancy's
// lifetime: held from the accepted start, released when the session
// wraps up — not at process exit.
func TestScanLockReleasedAfterSessionEnd(t *testing.T) {
	paths := newTestPaths(t)
	gate := newGatedController()
	svc, dataLock := newTestService(t, paths, staticSource{spec: specOf(gate)}, func() (Recognizer, error) { return newFakeRecognizer(), nil })

	if _, err := svc.Start(StartRequest{ID: "r1"}); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitFor(t, 10*time.Second, "the capturing phase", func() bool {
		return svc.Snapshot().Scan.Phase == scan.PhaseCapturing
	})
	if _, err := dataLock.AcquireScanLock(); !errors.Is(err, ErrLockHeld) {
		t.Fatalf("scan occupancy during the session: err = %v, want ErrLockHeld", err)
	}
	svc.Stop()
	waitNotBusy(t, svc)
	scanLock, err := dataLock.AcquireScanLock()
	if err != nil {
		t.Fatalf("scan occupancy after the wrap-up: %v (it must release with the session)", err)
	}
	scanLock.Close()
}

func TestSnapshotAndSubscriptionForwarding(t *testing.T) {
	paths := newTestPaths(t)
	source := devFixtureSource(t, 2, 0)
	svc, _ := newTestService(t, paths, source, func() (Recognizer, error) { return newFakeRecognizer(), nil })

	sub := svc.Subscribe()
	if sub.Initial.SessionID != "" || sub.Initial.Scan.Phase != "" {
		t.Fatalf("initial snapshot = %+v, want the idle service", sub.Initial)
	}
	rev := sub.Initial.Revision

	out, err := svc.Start(StartRequest{ID: "r1"})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	// At least one notification arrives for the session, carrying its
	// identity and a revision above the idle one.
	gotSession, gotRev := false, false
	for deadline := time.Now().Add(10 * time.Second); !gotSession || !gotRev; {
		select {
		case n := <-sub.Changes:
			if n.SessionID == out.SessionID {
				gotSession = true
			}
			if n.Revision > rev {
				gotRev = true
			}
		case <-time.After(time.Until(deadline)):
			t.Fatalf("notification forwarding: session %v revision %v after %v", gotSession, gotRev, time.Since(deadline.Add(-10*time.Second)))
		}
	}
	snap := waitNotBusy(t, svc)
	if snap.Revision <= rev {
		t.Fatalf("revision %d after the session, want it above %d", snap.Revision, rev)
	}
	if snap.SessionID != out.SessionID || snap.Scan.Phase != scan.PhaseEnded {
		t.Fatalf("snapshot = %s %s, want the ended session", snap.SessionID, snap.Scan.Phase)
	}
	sub.Close()
	sub.Close() // idempotent

	// A second session's notifications reach a fresh subscription too.
	sub2 := svc.Subscribe()
	if _, err := svc.Start(StartRequest{ID: "r2"}); err != nil {
		t.Fatalf("start r2: %v", err)
	}
	select {
	case n := <-sub2.Changes:
		if n.SessionID == "" {
			t.Fatalf("notification without a session id: %+v", n)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("no notification for the second session")
	}
	sub2.Close()
	svc.Stop()
	waitNotBusy(t, svc)
}

// TestRestartDoesNotReplayStartRequest proves the request identifiers
// live in memory only: a restarted service on the same data root starts
// a new session for the same request id.
func TestRestartDoesNotReplayStartRequest(t *testing.T) {
	paths := newTestPaths(t)
	source := devFixtureSource(t, 1, 0)
	svc, _ := newTestService(t, paths, source, func() (Recognizer, error) { return newFakeRecognizer(), nil })
	first, err := svc.Start(StartRequest{ID: "r1"})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitNotBusy(t, svc)
	if err := svc.Quit(); err != nil {
		t.Fatalf("quit: %v", err)
	}

	// The restarted process: a fresh data-root occupancy and service on
	// the same root — the quit released it.
	dataLock2, err := AcquireDataRootLock(paths.Data)
	if err != nil {
		t.Fatalf("reacquire data root after quit: %v", err)
	}
	defer dataLock2.Close()
	svc2, err := NewService(Config{
		Paths:         paths,
		DataLock:      dataLock2,
		Source:        devFixtureSource(t, 1, 0),
		NewRecognizer: func() (Recognizer, error) { return newFakeRecognizer(), nil },
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	second, err := svc2.Start(StartRequest{ID: "r1"})
	if err != nil {
		t.Fatalf("start after restart: %v", err)
	}
	if second.SessionID == first.SessionID || second.Replayed {
		t.Fatalf("start after restart = %+v, want a fresh session (no replay across restarts)", second)
	}
	waitNotBusy(t, svc2)
	records, err := svc2.ListRecords()
	if err != nil || len(records) != 2 {
		t.Fatalf("records after restart = %+v err %v, want both sessions", records, err)
	}
}

// TestQuitWithoutSessionTearsDownImmediately quits an idle service: the
// recognizer (created but unused) closes, the teardown runs and the
// occupancy frees.
func TestQuitWithoutSessionTearsDownImmediately(t *testing.T) {
	paths := newTestPaths(t)
	rec := &recordingRecognizer{delegate: newFakeRecognizer()}
	svc, _ := newTestService(t, paths, nil, func() (Recognizer, error) { return rec, nil })
	if err := svc.Quit(); err != nil {
		t.Fatalf("Quit: %v", err)
	}
	// The recognizer is created on the first start, never in advance: an
	// idle service quit closes nothing and still tears the occupancy down.
	if rec.closeCount != 0 {
		t.Fatalf("recognizer closed %d times without any session, want none", rec.closeCount)
	}
	taken, err := AcquireDataRootLock(paths.Data)
	if err != nil {
		t.Fatalf("data root after quit: %v", err)
	}
	taken.Close()
}

// --- small helpers -------------------------------------------------------

// newFlakyStore opens a real store over root inside a failure-injecting
// wrapper.
func newFlakyStore(t *testing.T, root string) *flakyStore {
	t.Helper()
	store, err := record.Open(root)
	if err != nil {
		t.Fatalf("record.Open: %v", err)
	}
	return &flakyStore{Store: store}
}

// newServiceWithStore assembles a service whose store is the injected
// one.
func newServiceWithStore(t *testing.T, paths Paths, store recordStore, source ScanSource, newRecognizer func() (Recognizer, error)) *Service {
	t.Helper()
	setScanLockBase(t, t.TempDir())
	dataLock, err := AcquireDataRootLock(paths.Data)
	if err != nil {
		t.Fatalf("acquire data-root lock: %v", err)
	}
	t.Cleanup(func() { dataLock.Close() })
	svc, err := NewService(Config{
		Paths:         paths,
		DataLock:      dataLock,
		Source:        source,
		Store:         store,
		NewRecognizer: newRecognizer,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return svc
}

// hasFaultCode reports whether the snapshot lists a fault with the code.
func hasFaultCode(snap scan.Snapshot, code string) bool {
	for _, f := range snap.Faults {
		if f.Code == code {
			return true
		}
	}
	// The first trigger carries its code too; if the fault cut the scan
	// it is not in the fault list.
	return snap.Trigger != nil && snap.Trigger.Code == code
}

// admitNController is a collection loop admitting n string observations.
func admitNController(n int) scan.Controller {
	return &scriptedController{
		collect: func(ctx context.Context, h *scan.ControllerHandle) error {
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
		},
	}
}

// admitThenGateController admits one observation and then blocks in
// Collect until the gate closes or the context ends, holding the session
// in the capturing phase.
type admitThenGateController struct {
	gate chan struct{}
}

func newAdmitThenGateController() *admitThenGateController {
	return &admitThenGateController{gate: make(chan struct{})}
}

func (c *admitThenGateController) Prepare(ctx context.Context, h *scan.ControllerHandle) error {
	return nil
}

func (c *admitThenGateController) Collect(ctx context.Context, h *scan.ControllerHandle) error {
	slot := h.Acquire(ctx)
	if slot == nil {
		return ctx.Err()
	}
	crop := LineCrop{Role: "name", Source: "gate.png", Image: image.NewNRGBA(image.Rect(0, 0, 2, 2))}
	if _, err := slot.Admit(Observation{Lines: []LineCrop{crop}}); err != nil {
		slot.Discard()
		return ctx.Err()
	}
	select {
	case <-c.gate:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// scriptedController delegates each controller phase to a function.
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
