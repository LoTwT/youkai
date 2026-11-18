package ui

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/LoTwT/youkai/internal/scan"
)

// TestExitCallbackDoesNotWaitForServiceDiskWork pins the callback's
// non-blocking contract: a record cleanup holds the service lock while the
// store works, and the close callback must still return at once. The
// background exit then stops admission, waits for the pending work, and
// quits only after the store released the lock.
func TestExitCallbackDoesNotWaitForServiceDiskWork(t *testing.T) {
	root := t.TempDir()
	paths := app.Paths{
		Data:      filepath.Join(root, "data"),
		Cache:     filepath.Join(root, "cache"),
		Logs:      filepath.Join(root, "logs"),
		Resources: filepath.Join(root, "resources"),
	}
	base, err := record.Open(paths.Data)
	if err != nil {
		t.Fatal(err)
	}
	store := &blockingDeleteStore{
		Store:   base,
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	lock, err := app.AcquireDataRootLock(paths.Data)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := app.NewService(app.Config{
		Paths:       paths,
		DataLock:    lock,
		Store:       store,
		ShutdownOCR: func() error { return nil },
	})
	if err != nil {
		lock.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		store.releaseDelete()
		_ = svc.Quit()
		_ = lock.Close()
	})
	m, q := newTestModel(t, Config{Service: readyRealService{svc}})

	deleteDone := make(chan struct{})
	go func() {
		_, _ = svc.DeleteAllRecords()
		close(deleteDone)
	}()
	select {
	case <-store.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the record deletion did not reach the store")
	}

	// The timer releases the store if the callback still waits for the
	// service lock, so the old behavior fails on the measurement instead
	// of hanging the test.
	timer := time.AfterFunc(250*time.Millisecond, store.releaseDelete)
	started := time.Now()
	if m.AllowClose() {
		t.Fatal("the close was allowed without an exit")
	}
	elapsed := time.Since(started)
	timer.Stop()
	if elapsed >= 200*time.Millisecond {
		t.Fatalf("the exit callback waited %s for the service lock", elapsed)
	}
	if m.exitState != exitRunning {
		t.Fatalf("the exit did not start: %v", m.exitState)
	}
	time.Sleep(20 * time.Millisecond)
	if m.exitState != exitRunning {
		t.Fatalf("the exit settled while the store still held the lock: %v", m.exitState)
	}

	store.releaseDelete()
	<-deleteDone
	drainQueued(t, q, func() bool { return m.exitState == exitDone })
	m.Idle()
}

// blockingDeleteStore holds the first DeleteAllRecords inside the store,
// the window in which the service holds its state lock through disk work.
type blockingDeleteStore struct {
	*record.Store
	entered     chan struct{}
	release     chan struct{}
	enterOnce   sync.Once
	releaseOnce sync.Once
}

func (s *blockingDeleteStore) DeleteAllRecords() (record.RecordDeletionBatch, error) {
	s.enterOnce.Do(func() {
		close(s.entered)
		<-s.release
	})
	return s.Store.DeleteAllRecords()
}

func (s *blockingDeleteStore) releaseDelete() { s.releaseOnce.Do(func() { close(s.release) }) }

// drainQueued runs the updater's queued closures until done holds. It exists
// for tests whose exit goroutine waits for a closure that is itself queued:
// draining through Idle would wait for that goroutine before running the
// closure that releases it, which the real UI thread never does because it
// runs the closure independently.
func drainQueued(t *testing.T, q *queueUpdater, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		for _, fn := range q.take() {
			fn()
		}
		if done() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the queued completion did not settle")
		}
		time.Sleep(time.Millisecond)
	}
}

// TestExitStopsScanWhileWaitingForHistoryExport is the exit-ordering
// regression: the pending-work wait must not leave the scan operating the
// game. A real service holds a live session; a blocking export stands in
// for a slow history export. Once the exit is accepted, the session's
// input and admission close promptly, no further observation is admitted
// while the export stays blocked, and only then does the wrap-up join the
// export and quit. The wait runs off the UI thread.
func TestExitStopsScanWhileWaitingForHistoryExport(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := t.TempDir()
	paths := app.Paths{
		Data:      filepath.Join(root, "data"),
		Cache:     filepath.Join(root, "cache"),
		Logs:      filepath.Join(root, "logs"),
		Resources: filepath.Join(root, "resources"),
	}
	lock, err := app.AcquireDataRootLock(paths.Data)
	if err != nil {
		t.Fatal(err)
	}
	controller := newAdmissionProbeController()
	svc, err := app.NewService(app.Config{
		Paths:    paths,
		DataLock: lock,
		Source:   probeScanSource{controller: controller},
		NewRecognizer: func() (app.Recognizer, error) {
			return passRecognizer{}, nil
		},
		ShutdownOCR: func() error { return nil },
	})
	if err != nil {
		lock.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = svc.Quit()
		_ = lock.Close()
	})

	started := make(chan struct{})
	release := make(chan struct{})
	var startOnce, releaseOnce sync.Once
	releaseExport := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseExport)
	var finished atomic.Bool
	saver := &fakeDirectExportSaver{}
	saver.saveCtxFn = func(context.Context, app.ExportFormat, string, []byte) (string, error) {
		startOnce.Do(func() { close(started) })
		<-release
		finished.Store(true)
		return "/tmp/finished.youkai.json", nil
	}
	m, q := newTestModel(t, Config{
		Service:           readyRealService{svc},
		DirectExportSaver: saver,
		AppVersion:        "test-1",
	})

	if _, err := svc.Start(app.StartRequest{ID: "exit-probe"}); err != nil {
		if errors.Is(err, app.ErrLockHeld) {
			// The per-user scan occupancy is intentionally process-wide;
			// a real youkai running on this machine owns it. The test is
			// about exit ordering, not lock contention.
			t.Skipf("another youkai holds the per-user scan occupancy: %v", err)
		}
		t.Fatalf("start: %v", err)
	}
	waitUntil(t, "the first admitted observation", func() bool {
		return controller.admitted.Load() > 0
	})

	// The export is a saved history record, so it stays available while
	// the live scan occupies the service slot.
	rec := testRecord()
	m.recData = rec
	m.rec = &RecordView{ID: "history-export"}
	m.exportData, err = app.BuildUsableExport(rec)
	if err != nil {
		t.Fatalf("build the export: %v", err)
	}
	if reason := m.exportBlockedReason(); reason != "" {
		t.Fatalf("the history export is blocked: %s", reason)
	}
	m.startExport(app.ExportJSON)
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("the history export did not start")
	}

	if m.AllowClose() {
		t.Fatal("the close was allowed while the export was writing")
	}
	select {
	case <-controller.stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("the scan did not stop when the exit was accepted")
	}
	if finished.Load() {
		t.Fatal("the export finished before the test released it")
	}
	if m.exitState != exitRunning {
		t.Fatalf("the exit settled before the export: %v", m.exitState)
	}
	admitted := controller.admitted.Load()
	time.Sleep(30 * time.Millisecond)
	if later := controller.admitted.Load(); later != admitted {
		t.Fatalf("the scan admitted %d more observations during the export wait", later-admitted)
	}

	releaseExport()
	drainQueued(t, q, func() bool { return m.exitState == exitDone })
	m.Idle()
	if !finished.Load() {
		t.Fatal("the export did not finish after release")
	}
	snap := svc.Snapshot()
	if snap.Scan.InputPermitted || snap.Scan.Phase != scan.PhaseEnded {
		t.Fatalf("the scan after the exit = phase %s permitted %v, want ended and closed",
			snap.Scan.Phase, snap.Scan.InputPermitted)
	}
}

// admissionProbeController admits observations continuously until input
// closes, then reports that stop through stopped. The continuous loop is
// what makes a delayed stop visible as more admissions.
type admissionProbeController struct {
	admitted atomic.Int64
	stopped  chan struct{}
	stopOnce sync.Once
}

func newAdmissionProbeController() *admissionProbeController {
	return &admissionProbeController{stopped: make(chan struct{})}
}

func (c *admissionProbeController) Prepare(context.Context, *scan.ControllerHandle) error {
	return nil
}

func (c *admissionProbeController) Collect(ctx context.Context, h *scan.ControllerHandle) error {
	defer c.stopOnce.Do(func() { close(c.stopped) })
	for {
		if !h.InputPermitted() {
			return ctx.Err()
		}
		slot := h.Acquire(ctx)
		if slot == nil {
			return ctx.Err()
		}
		if _, err := slot.Admit(app.Observation{}); err != nil {
			slot.Discard()
			return err
		}
		c.admitted.Add(1)
		select {
		case <-time.After(time.Millisecond):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// probeScanSource supplies the admission probe as a valid scan.
type probeScanSource struct{ controller scan.Controller }

func (s probeScanSource) NewScan() (app.ScanSpec, error) {
	return app.ScanSpec{
		Controller: s.controller,
		Game:       "zzz",
		Entity:     "driveDisc",
		Client:     record.ClientWindowsLocal,
		Locale:     "zh-CN",
		Scope:      json.RawMessage(`{"kind":"exitProbe"}`),
	}, nil
}

// passRecognizer ends every task with its admitted observation, which is
// enough to keep the pipeline moving without OCR.
type passRecognizer struct{}

func (passRecognizer) Recognize(ctx context.Context, task scan.Task, submit scan.Submit) error {
	select {
	case <-ctx.Done():
		return nil
	default:
	}
	return submit(task.SessionID, task.Seq, scan.Recognition{Result: task.Observation})
}

func (passRecognizer) Close() error { return nil }

// TestExitWaitsForRunningExport pins the export half of the exit's pending
// work: an explicit exit requested while the direct export writes must wait
// for the worker to publish the write's outcome before Service.Quit runs.
// The wait stays off the UI thread; the queued completion closure still
// lands.
func TestExitWaitsForRunningExport(t *testing.T) {
	f := newExportDialogFixture(t, testRecord(), false)
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	var finished atomic.Bool
	f.saver.saveCtxFn = func(context.Context, app.ExportFormat, string, []byte) (string, error) {
		once.Do(func() { close(started) })
		<-release
		finished.Store(true)
		return "/tmp/finished.youkai.json", nil
	}
	f.svc.quitFn = func() error {
		if !finished.Load() {
			t.Error("Service.Quit ran while the export was still writing")
		}
		return nil
	}
	f.open(t)
	if err := f.tt.Click("导出 JSON"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("the export write did not start")
	}
	settled := 0
	f.m.cfg.OnExitSettled = func() { settled++ }

	if f.m.AllowClose() {
		t.Fatal("the close was allowed while the export was writing")
	}
	if f.m.exitState != exitRunning {
		t.Fatalf("the exit did not start: %s", probeReviewState(f.m))
	}
	if got := f.svc.quitCount(); got != 0 {
		t.Fatalf("Service.Quit ran %d times before the export settled", got)
	}

	close(release)
	drainQueued(t, f.q, func() bool { return f.m.exitState == exitDone })
	f.m.Idle()
	drainAll(t, f.m, f.q)
	if got := f.svc.quitCount(); got != 1 {
		t.Fatalf("Service.Quit ran %d times, want once after the export", got)
	}
	if f.m.exitState != exitDone || settled != 1 {
		t.Fatalf("the exit settled state %v callbacks %d, want done and 1", f.m.exitState, settled)
	}
	if got := f.svc.exitPreparationCount(); got != 1 {
		t.Fatalf("the exit prepared the service %d times, want 1", got)
	}
	if calls := f.saver.snapshot(); len(calls) != 1 {
		t.Fatalf("the export ran %d writes, want 1", len(calls))
	}
}

// TestExitWaitsForRunningUpdateInstall pins the update half: MyGo's Windows
// install replaces the executable in process, so the exit must wait for the
// install to publish its outcome before Service.Quit. A successful install
// still asks for the one restart, and that request precedes the quit so the
// handoff re-issues it after the wrap-up.
func TestExitWaitsForRunningUpdateInstall(t *testing.T) {
	found := &fakeUpdateHandle{version: "0.0.2"}
	started := make(chan struct{})
	release := make(chan struct{})
	var startOnce sync.Once
	found.install = func(func(downloaded, total int64)) error {
		startOnce.Do(func() { close(started) })
		<-release
		return nil
	}
	src := &fakeUpdates{
		enabled: true,
		check: func(context.Context) (UpdateHandle, error) {
			return found, nil
		},
	}
	svc := &fakeService{}
	var mu sync.Mutex
	var events []string
	record := func(event string) {
		mu.Lock()
		events = append(events, event)
		mu.Unlock()
	}
	m, q := newTestModel(t, Config{
		Service:           svc,
		Updates:           src,
		OnUpdateInstalled: func() { record("installed") },
		OnExitSettled:     func() { record("settled") },
	})
	tt := openUpdateSettings(t, m, q)
	m.CheckUpdates()
	settle(t, m, q, tt)
	m.InstallUpdate()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("the update install did not start")
	}
	svc.quitFn = func() error {
		record("quit")
		return nil
	}

	if m.AllowClose() {
		t.Fatal("the close was allowed while the update was installing")
	}
	if m.exitState != exitRunning {
		t.Fatalf("the exit did not start: %s", probeReviewState(m))
	}
	if got := svc.quitCount(); got != 0 {
		t.Fatalf("Service.Quit ran %d times before the install settled", got)
	}

	close(release)
	drainQueued(t, q, func() bool { return m.exitState == exitDone })
	m.Idle()
	drainAll(t, m, q)
	if got := svc.quitCount(); got != 1 {
		t.Fatalf("Service.Quit ran %d times, want once after the install", got)
	}
	if found.installs != 1 {
		t.Fatalf("the install ran %d times, want 1", found.installs)
	}
	if m.exitState != exitDone {
		t.Fatalf("the exit settled in state %v, want done", m.exitState)
	}
	mu.Lock()
	gotEvents := append([]string(nil), events...)
	mu.Unlock()
	want := []string{"installed", "quit", "settled"}
	if len(gotEvents) != len(want) {
		t.Fatalf("events = %v, want %v", gotEvents, want)
	}
	for i := range want {
		if gotEvents[i] != want[i] {
			t.Fatalf("events = %v, want %v", gotEvents, want)
		}
	}
	if calls := svc.updateInstallingCalls(); len(calls) != 1 || !calls[0] {
		t.Fatalf("the service saw %v, want the successful install left admitted", calls)
	}
}

// TestExitWaitsForFailedUpdateInstall covers the failure side: the install
// releases the service's admission and the exit still proceeds without
// asking for a restart.
func TestExitWaitsForFailedUpdateInstall(t *testing.T) {
	found := &fakeUpdateHandle{version: "0.0.2"}
	started := make(chan struct{})
	release := make(chan struct{})
	var startOnce sync.Once
	found.install = func(func(downloaded, total int64)) error {
		startOnce.Do(func() { close(started) })
		<-release
		return errors.New("signature mismatch")
	}
	src := &fakeUpdates{
		enabled: true,
		check: func(context.Context) (UpdateHandle, error) {
			return found, nil
		},
	}
	relaunched := 0
	svc := &fakeService{}
	m, q := newTestModel(t, Config{
		Service:           svc,
		Updates:           src,
		OnUpdateInstalled: func() { relaunched++ },
	})
	tt := openUpdateSettings(t, m, q)
	m.CheckUpdates()
	settle(t, m, q, tt)
	m.InstallUpdate()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("the update install did not start")
	}

	if m.AllowClose() {
		t.Fatal("the close was allowed while the update was installing")
	}
	if got := svc.quitCount(); got != 0 {
		t.Fatalf("Service.Quit ran %d times before the install failed", got)
	}

	close(release)
	drainQueued(t, q, func() bool { return m.exitState == exitDone })
	m.Idle()
	drainAll(t, m, q)
	if got := svc.quitCount(); got != 1 {
		t.Fatalf("Service.Quit ran %d times, want once after the failed install", got)
	}
	if m.exitState != exitDone {
		t.Fatalf("the exit settled in state %v, want done", m.exitState)
	}
	if relaunched != 0 {
		t.Fatalf("a failed install asked for %d restarts", relaunched)
	}
	if calls := svc.updateInstallingCalls(); len(calls) != 2 || !calls[0] || calls[1] {
		t.Fatalf("the service saw %v, want the install admitted then released", calls)
	}
	if m.updatesState != updateAvailable {
		t.Fatalf("the failed install left update state %v, want available", m.updatesState)
	}
}

// TestInstallRefusedWhenServiceAlreadyHoldsTheScanSlot is the stale-snapshot
// regression: the UI still shows an idle scan slot, but the service already
// accepted a scan. The click must not start an install beside it; the
// refreshed snapshot then explains the real reason.
func TestInstallRefusedWhenServiceAlreadyHoldsTheScanSlot(t *testing.T) {
	found := &fakeUpdateHandle{version: "0.0.2"}
	src := &fakeUpdates{
		enabled: true,
		check: func(context.Context) (UpdateHandle, error) {
			return found, nil
		},
	}
	svc := &fakeService{
		beginUpdateErr: errors.New("app: update install: the active scan session has not finished saving"),
		snapshot: app.Snapshot{
			SessionID: "s1",
			Revision:  2,
			Busy:      true,
			Scan:      scan.Snapshot{SessionID: "s1", Revision: 1, Phase: scan.PhaseCapturing},
		},
	}
	m, q := newTestModel(t, Config{Service: svc, Updates: src})
	tt := openUpdateSettings(t, m, q)
	m.CheckUpdates()
	settle(t, m, q, tt)
	if m.snap.Busy {
		t.Fatalf("the fixture's snapshot is already busy: %+v", m.snap)
	}

	m.InstallUpdate()
	settle(t, m, q, tt)
	if found.installs != 0 {
		t.Fatalf("the install ran beside the service's accepted scan: %d", found.installs)
	}
	if m.updatesState != updateAvailable {
		t.Fatalf("the refused install left update state %v, want available", m.updatesState)
	}
	if m.updatesInstallErr != "" {
		t.Fatalf("the refusal was reported as an install failure: %q", m.updatesInstallErr)
	}
	if !tt.HasText("扫描会话进行中，等其结束并保存完成后再安装更新。") {
		t.Fatalf("the refreshed reason does not explain the refusal: %q", tt.Texts())
	}
}
