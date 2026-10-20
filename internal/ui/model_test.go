package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/LoTwT/youkai/internal/scan"
)

func availableSource() ScanSourceInfo {
	return ScanSourceInfo{Available: true, Label: "开发数据源"}
}

// newTestModel attaches a queued updater, so the model's background work
// is observable and confined to the test goroutine.
func newTestModel(t *testing.T, cfg Config) (*Model, *queueUpdater) {
	t.Helper()
	if cfg.ScanSource == (ScanSourceInfo{}) {
		cfg.ScanSource = availableSource()
	}
	m := New(cfg)
	q := &queueUpdater{}
	m.Attach(q)
	return m, q
}

// TestApplyDiscardsStaleRevisions checks the subscription pump's discard
// rule: an update of the shown session applies only at a higher revision;
// one of another session always applies.
func TestApplyDiscardsStaleRevisions(t *testing.T) {
	m, q := newTestModel(t, Config{Service: &fakeService{}})
	busy := snap("s1", 5, true, scan.PhaseCapturing,
		scan.Counts{Captured: 10, Produced: 7, Queued: 3}, 0, true, "")
	m.Apply(busy)

	stale := snap("s1", 3, false, scan.PhaseEnded,
		scan.Counts{Captured: 2, Produced: 2}, 2, false, "")
	m.Apply(stale)
	if m.snap.Revision != 5 || m.snap.Scan.Counts.Captured != 10 || !m.snap.Busy {
		t.Fatalf("a stale revision was applied: %+v", m.snap)
	}
	same := snap("s1", 5, false, scan.PhaseEnded,
		scan.Counts{Captured: 1}, 1, false, "")
	m.Apply(same)
	if m.snap.Revision != 5 {
		t.Fatalf("an equal revision was applied: %+v", m.snap)
	}

	newer := snap("s1", 6, false, scan.PhaseEnded,
		scan.Counts{Captured: 10, Produced: 10}, 10, false, "")
	m.Apply(newer)
	if m.snap.Revision != 6 || m.snap.Scan.Phase != scan.PhaseEnded {
		t.Fatalf("a newer revision was discarded: %+v", m.snap)
	}

	// A refused start rolls the service back to the previous session: the
	// snapshot names another session and applies even at a lower revision.
	other := snap("s0", 2, false, scan.PhaseEnded, scan.Counts{Captured: 1}, 1, false, "")
	m.Apply(other)
	if m.appliedSession != scan.ID("s0") {
		t.Fatalf("a snapshot of another session was discarded: %+v", m.snap)
	}
	drainAll(t, m, q) // the wrap history reload settles
}

// TestApplySwitchesSessionAndReloadsSavedGrowth checks the live record's
// lifecycle: a new session becomes the results record, and each growth of
// the saved count reloads it from disk, keeping the selection on the same
// capture sequence.
func TestApplySwitchesSessionAndReloadsSavedGrowth(t *testing.T) {
	svc := &fakeService{loads: map[string]*record.Recovery{
		"s1": recovery(item(1, record.StatusProduced, "", "")),
	}}
	m, q := newTestModel(t, Config{Service: svc})

	// The live session becomes the results record before any history is
	// opened.
	m.Apply(snap("s1", 1, true, scan.PhaseCapturing, scan.Counts{Captured: 1, Queued: 1}, 0, true, ""))
	drainAll(t, m, q)
	if m.rec == nil || !m.rec.Live || m.rec.ID != "s1" {
		t.Fatalf("a new session did not become the results record: %+v", m.rec)
	}
	if len(svc.loadLog) != 0 {
		t.Fatalf("a session with nothing saved reloaded from disk: %v", svc.loadLog)
	}

	m.Apply(snap("s1", 2, true, scan.PhaseCapturing,
		scan.Counts{Captured: 3, Produced: 1, Queued: 2}, 1, true, ""))
	drainAll(t, m, q)
	if len(svc.loadLog) != 1 || svc.loadLog[0] != "s1" {
		t.Fatalf("saved growth did not reload the record: %v", svc.loadLog)
	}
	if m.selected != 0 {
		t.Fatalf("the first record was not selected by default: %d", m.selected)
	}

	// The same saved count does not reload; a growth does, and the
	// selection follows the capture sequence.
	m.Apply(snap("s1", 3, true, scan.PhaseCapturing,
		scan.Counts{Captured: 3, Produced: 3}, 1, true, ""))
	drainAll(t, m, q)
	if len(svc.loadLog) != 1 {
		t.Fatalf("an unchanged saved count reloaded: %v", svc.loadLog)
	}
	svc.loads["s1"] = recovery(
		item(1, record.StatusProduced, "", ""),
		item(2, record.StatusFailed, "", "磁盘识别失败"),
		item(3, record.StatusProduced, "", ""),
	)
	m.selected = 1
	m.Apply(snap("s1", 4, false, scan.PhaseEnded,
		scan.Counts{Captured: 3, Produced: 2, Failed: 1}, 3, false, ""))
	drainAll(t, m, q)
	if len(svc.loadLog) != 2 {
		t.Fatalf("the final save did not reload the record: %v", svc.loadLog)
	}
	if m.selected != 1 || m.selectedSeq() != 2 {
		t.Fatalf("the selection did not follow its capture sequence: %d", m.selected)
	}
}

// TestStartRequestsAreIdempotentPerClickBurst checks the request identity
// rule: one identity per attempt, held until the service answered, so
// repeated starts cannot open a second session; the next attempt after an
// answer gets a fresh identity.
func TestStartRequestsAreIdempotentPerClickBurst(t *testing.T) {
	svc := &fakeService{}
	m, q := newTestModel(t, Config{Service: svc})

	m.Start()
	m.Start() // the first request is still in flight
	if m.startReqID == "" {
		t.Fatal("the in-flight request lost its identity")
	}
	drainAll(t, m, q)
	if len(svc.starts) != 1 {
		t.Fatalf("an in-flight start was issued twice: %+v", svc.starts)
	}
	if m.startReqID != "" {
		t.Fatalf("the answered request kept its identity: %q", m.startReqID)
	}

	m.Start()
	drainAll(t, m, q)
	if len(svc.starts) != 2 || svc.starts[0].ID == svc.starts[1].ID {
		t.Fatalf("a new attempt did not get a fresh identity: %+v", svc.starts)
	}
}

// TestStartRefusalIsShown checks that a refused start's reason stays
// visible until the next attempt.
func TestStartRefusalIsShown(t *testing.T) {
	svc := &fakeService{startFn: func(app.StartRequest) (app.StartOutcome, error) {
		return app.StartOutcome{}, errors.New("no scan source is wired")
	}}
	m, q := newTestModel(t, Config{Service: svc})
	m.Start()
	drainAll(t, m, q)
	if m.startErr == "" {
		t.Fatal("the refusal was not recorded")
	}
	m.Start()
	if m.startErr != "" {
		t.Fatal("the next attempt kept the old refusal")
	}
	drainAll(t, m, q)
}

// TestCloseBehaviorPreference checks the default, the change and its
// persistence: hide is the default, changing to exit saves once, and a
// failed save keeps the effective preference with a visible error.
func TestCloseBehaviorPreference(t *testing.T) {
	var saved []app.CloseBehavior
	m, q := newTestModel(t, Config{Service: &fakeService{}, SaveCloseBehavior: func(behavior app.CloseBehavior) error {
		saved = append(saved, behavior)
		return nil
	}})
	if m.CloseBehavior() != app.CloseBehaviorHide {
		t.Fatalf("default close behavior = %q", m.CloseBehavior())
	}
	// A startup with the persisted exit preference applies it directly.
	persisted, pq := newTestModel(t, Config{Service: &fakeService{}, CloseBehavior: app.CloseBehaviorExit})
	if persisted.CloseBehavior() != app.CloseBehaviorExit {
		t.Fatalf("startup close behavior = %q", persisted.CloseBehavior())
	}
	drainAll(t, persisted, pq)

	m.SetCloseBehavior(app.CloseBehaviorExit)
	drainAll(t, m, q)
	if m.CloseBehavior() != app.CloseBehaviorExit {
		t.Fatalf("close behavior = %q after the change", m.CloseBehavior())
	}
	if len(saved) != 1 || saved[0] != app.CloseBehaviorExit {
		t.Fatalf("saved preferences = %v", saved)
	}
	m.SetCloseBehavior(app.CloseBehaviorExit)
	drainAll(t, m, q)
	if len(saved) != 1 {
		t.Fatalf("an unchanged preference was saved again: %v", saved)
	}

	failing, fq := newTestModel(t, Config{Service: &fakeService{}, SaveCloseBehavior: func(app.CloseBehavior) error {
		return errors.New("disk full")
	}})
	failing.SetCloseBehavior(app.CloseBehaviorExit)
	drainAll(t, failing, fq)
	if failing.CloseBehavior() != app.CloseBehaviorExit {
		t.Fatal("a failed save reverted the effective preference")
	}
	if !strings.Contains(failing.closeBehaviorErr, "disk full") {
		t.Fatalf("save error = %q", failing.closeBehaviorErr)
	}
}

// TestLifecycleStatusRendersTrayState checks the four tray states and the
// stoppable flag.
func TestLifecycleStatusRendersTrayState(t *testing.T) {
	svc := &fakeService{}
	m, q := newTestModel(t, Config{Service: svc})
	if status := m.LifecycleStatus(); status.Text != "就绪" || status.Busy {
		t.Fatalf("idle status = %+v", status)
	}
	m.Apply(snap("s1", 1, true, scan.PhaseCapturing, scan.Counts{Captured: 1}, 0, true, ""))
	drainAll(t, m, q)
	if status := m.LifecycleStatus(); status.Text != "扫描中" || !status.Busy {
		t.Fatalf("scanning status = %+v", status)
	}
	m.Apply(snap("s1", 2, true, scan.PhaseFinalizing, scan.Counts{Captured: 1}, 1, true, "disk full"))
	drainAll(t, m, q)
	if status := m.LifecycleStatus(); status.Text != "保存失败" {
		t.Fatalf("save-failure status = %+v", status)
	}
	if m.AllowClose() {
		t.Fatal("the first close was allowed before the exit wrap-up")
	}
	if status := m.LifecycleStatus(); status.Text != "收尾中" || status.Busy {
		t.Fatalf("exit status = %+v", status)
	}
	drainAll(t, m, q)
}

// TestPrepareRestoreStopsWithoutStarting checks the unified restore
// contract: an active session is stopped, nothing is started, and an idle
// model is left alone.
func TestPrepareRestoreStopsWithoutStarting(t *testing.T) {
	svc := &fakeService{}
	m, q := newTestModel(t, Config{Service: svc})
	m.Apply(snap("s1", 1, true, scan.PhaseCapturing, scan.Counts{Captured: 1}, 0, true, ""))
	drainAll(t, m, q)
	m.PrepareRestore()
	drainAll(t, m, q)
	if svc.stops != 1 {
		t.Fatalf("restore stops = %d, want 1", svc.stops)
	}
	if len(svc.starts) != 0 {
		t.Fatalf("restore started a scan: %v", svc.starts)
	}

	m.Apply(snap("s1", 2, false, scan.PhaseEnded, scan.Counts{Captured: 1}, 1, false, ""))
	drainAll(t, m, q)
	m.PrepareRestore()
	drainAll(t, m, q)
	if svc.stops != 1 {
		t.Fatalf("an idle restore stopped again: %d", svc.stops)
	}

	// A start request that the service has not answered yet has no Busy
	// snapshot; the restore still stops it so it cannot begin collecting
	// behind the restored window.
	m.startReqID = "ui-in-flight"
	m.PrepareRestore()
	if svc.stops != 2 {
		t.Fatalf("an in-flight start was not stopped: %d stops", svc.stops)
	}
	m.startReqID = ""
}

// Stop must also reach a start request that has no Busy snapshot yet: the
// service has already placed its placeholder, so the user must be able to
// cancel preparation instead of waiting for the first snapshot.
func TestStopStopsInFlightStart(t *testing.T) {
	svc := &fakeService{}
	m, q := newTestModel(t, Config{Service: svc})
	m.startReqID = "ui-in-flight"
	m.Stop()
	drainAll(t, m, q)
	if svc.stops != 1 {
		t.Fatalf("stop calls = %d, want 1", svc.stops)
	}
}

func TestLifecycleStatusIncludesInFlightStart(t *testing.T) {
	m, _ := newTestModel(t, Config{Service: &fakeService{}})
	m.startReqID = "ui-in-flight"
	status := m.LifecycleStatus()
	if !status.Busy || status.Text != "扫描中" {
		t.Fatalf("in-flight status = %+v, want busy scan", status)
	}
}

// TestExitFailureCallback checks that a failed wrap-up reaches the entry
// point, which restores the window for the retry.
func TestExitFailureCallback(t *testing.T) {
	failures := 0
	svc := &fakeService{quitFn: func() error { return errors.New("disk full") }}
	m, q := newTestModel(t, Config{
		Service:      svc,
		OnExitFailed: func() { failures++ },
	})
	if m.AllowClose() {
		t.Fatal("the close was allowed before the wrap-up")
	}
	drainAll(t, m, q)
	if failures != 1 {
		t.Fatalf("exit failures reported = %d, want 1", failures)
	}
}

// TestExitFlow checks the explicit-exit flow: the first close starts the
// wrap-up and refuses; repeats join it; a failed wrap-up keeps the window
// and runs again after the save settled; a successful one allows every
// later close and settles the application once.
func TestExitFlow(t *testing.T) {
	settled := 0
	release := make(chan struct{})
	svc := &fakeService{quitFn: func() error {
		<-release
		return nil
	}}
	m, q := newTestModel(t, Config{Service: svc, OnExitSettled: func() { settled++ }})

	if m.AllowClose() {
		t.Fatal("the first close was allowed before the wrap-up")
	}
	if m.AllowClose() {
		t.Fatal("a repeated close was allowed while the wrap-up runs")
	}
	close(release)
	drainAll(t, m, q)
	if !m.AllowClose() {
		t.Fatal("a settled wrap-up did not allow the close")
	}
	if settled != 1 || svc.quits != 1 {
		t.Fatalf("the wrap-up settled %d times with %d quits", settled, svc.quits)
	}

	// A failed wrap-up (an unsaved result) keeps the window; once the save
	// settled, the next close runs the wrap-up again and succeeds.
	failed := &fakeService{quitFn: func() error {
		return errors.New("app: quit: scan s1 still has an unsaved result: disk full")
	}}
	m2, q2 := newTestModel(t, Config{Service: failed, OnExitSettled: func() { settled++ }})
	if m2.AllowClose() {
		t.Fatal("the close was allowed before the wrap-up")
	}
	drainAll(t, m2, q2)
	if m2.exitErr == "" || m2.exitState != exitFailed {
		t.Fatalf("the wrap-up failure was not kept: %+v", m2)
	}
	failed.quitFn = nil // the retry succeeded
	if m2.AllowClose() {
		t.Fatal("a failed wrap-up allowed the close")
	}
	drainAll(t, m2, q2)
	if !m2.AllowClose() || settled != 2 {
		t.Fatalf("the second wrap-up did not settle: state %d, settled %d", m2.exitState, settled)
	}
}

// TestOpenHistoryAndReturnToLive checks the reopen flow: the chosen
// history record loads with its recovery marks and becomes the results
// record, and the live scan is reachable again.
func TestOpenHistoryAndReturnToLive(t *testing.T) {
	svc := &fakeService{
		list: []record.RecordInfo{{
			ID: "old", LatestRevision: 4,
			SavedAt: time.Date(2026, 10, 17, 12, 30, 0, 0, time.UTC),
		}},
		loads: map[string]*record.Recovery{
			"old": recovery(
				item(1, record.StatusProduced, "", ""),
				item(2, record.StatusUnprocessed, record.ReasonInterrupted, ""),
			),
			"s1": recovery(item(1, record.StatusProduced, "", "")),
		},
	}
	m, q := newTestModel(t, Config{Service: svc})
	drainAll(t, m, q)
	if len(m.history) != 1 || m.histChoice == "" {
		t.Fatalf("the history list did not load: %+v", m.history)
	}

	// The live session runs and becomes the results record before any
	// history is opened.
	m.Apply(snap("s1", 1, true, scan.PhaseCapturing, scan.Counts{Captured: 1, Queued: 1}, 0, true, ""))
	drainAll(t, m, q)
	if m.rec == nil || !m.rec.Live || m.rec.ID != "s1" {
		t.Fatalf("the live session did not become the results record: %+v", m.rec)
	}

	m.OpenHistory()
	drainAll(t, m, q)
	if m.rec == nil || m.rec.Live || m.rec.ID != "old" || !m.rec.Interrupted || m.rec.InterruptedCount != 1 {
		t.Fatalf("the history record did not open with its marks: %+v", m.rec)
	}

	// The live session's saved growth does not overwrite an open history
	// record; returning to the live scan reloads it.
	m.Apply(snap("s1", 2, true, scan.PhaseCapturing, scan.Counts{Captured: 2}, 1, true, ""))
	drainAll(t, m, q)
	if m.rec.ID != "old" || m.rec.Live {
		t.Fatalf("a live update overwrote the open history record: %+v", m.rec)
	}
	m.ShowLive()
	drainAll(t, m, q)
	if !m.rec.Live || m.rec.ID != "s1" || len(svc.loadLog) == 0 || svc.loadLog[len(svc.loadLog)-1] != "s1" {
		t.Fatalf("returning to the live scan did not reload it: %+v", svc.loadLog)
	}

	// A new session takes the results area back from a history record.
	m.OpenHistory()
	drainAll(t, m, q)
	m.Apply(snap("s2", 1, true, scan.PhasePreparing, scan.Counts{}, 0, true, ""))
	drainAll(t, m, q)
	if !m.rec.Live || m.rec.ID != "s2" {
		t.Fatalf("a new session did not replace the history record: %+v", m.rec)
	}
}

// TestRecordLoadFailureIsShown checks that an unloadable record — one
// without a verifiable checkpoint — surfaces its error instead of empty
// results.
func TestRecordLoadFailureIsShown(t *testing.T) {
	svc := &fakeService{
		loads: map[string]*record.Recovery{},
	}
	m, q := newTestModel(t, Config{Service: svc})
	m.Apply(snap("s1", 2, true, scan.PhaseCapturing, scan.Counts{Captured: 2}, 1, true, ""))
	drainAll(t, m, q)
	if m.recErr == "" {
		t.Fatalf("the load failure was not shown: recErr=%q", m.recErr)
	}
}
