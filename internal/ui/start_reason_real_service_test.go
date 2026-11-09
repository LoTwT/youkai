package ui

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/egoist/mygo/ui"
)

// refusingStartSource refuses every start inside the service's preparation,
// like a target that vanished or a scan lock held by another process.
type refusingStartSource struct{}

func (refusingStartSource) NewScan() (app.ScanSpec, error) {
	return app.ScanSpec{}, errors.New("the test source refused the start")
}

// readyRealService keeps the real service behind the model while the
// read-only checklist reports satisfied rows. The start refusal, the quit
// wrap-up and every snapshot remain the service's own.
type readyRealService struct{ *app.Service }

func (readyRealService) CheckReadiness(*app.TargetCandidate) app.ReadinessReport {
	return fakeReadyReport(nil, 0, 0)
}

// TestRealServiceFailedExitPublishesTheQuittingBlock is the quitting-block
// regression: Quit turned s.quitting on under the lock without a revision
// bump, so the model discarded the authoritative snapshot at the exit
// decision as stale. The past refusal then stayed on screen through the
// exit, and after a teardown failure the page showed no current reason and
// let cleanup open a preview.
func TestRealServiceFailedExitPublishesTheQuittingBlock(t *testing.T) {
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
	teardownGate := make(chan struct{})
	var teardownOnce sync.Once
	releaseTeardown := func() { teardownOnce.Do(func() { close(teardownGate) }) }
	svc, err := app.NewService(app.Config{
		Paths:    paths,
		DataLock: lock,
		Source:   refusingStartSource{},
		ShutdownOCR: func() error {
			<-teardownGate
			return errors.New("the test teardown failed")
		},
	})
	if err != nil {
		lock.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		releaseTeardown()
		_ = svc.Quit()
	})

	m, q := newTestModel(t, Config{
		Service:           readyRealService{svc},
		DirectExportSaver: &fakeDirectExportSaver{},
	})
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)
	sub := svc.Subscribe()
	q.Update(func() { m.Apply(sub.Initial) })
	go func() {
		for range sub.Changes {
			snap := svc.Snapshot()
			q.Update(func() { m.Apply(snap) })
		}
	}()
	t.Cleanup(sub.Close)

	// The Start call and its teardown run asynchronously; drain the queue
	// without Idle so a blocked teardown cannot hang the wait.
	drain := func() {
		for _, fn := range q.take() {
			fn()
		}
	}
	wait := func(what string, pred func() bool) {
		t.Helper()
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			drain()
			if pred() {
				drain()
				return
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatalf("timed out waiting for %s", what)
	}

	// A real refused start leaves the past-event line.
	m.Start()
	wait("the refused start", func() bool { return m.startReqID == "" && m.startErr != "" })
	tt.Frame()
	const refusal = "上次开始失败：app: start: the test source refused the start"
	if !tt.HasText(refusal) {
		t.Fatalf("the refused start left no past event: %q", tt.Texts())
	}
	if got := m.snap.StartBlocked; got != app.StartBlockedNone {
		t.Fatalf("the refused start left the service block %q", got)
	}

	// Quit begins; the teardown stays blocked until the gate opens.
	m.AllowClose()
	wait("the quitting snapshot", func() bool {
		return m.exitState == exitRunning && m.snap.StartBlocked == app.StartBlockedQuitting
	})
	tt.Frame()
	if got := m.scanStartBlockedReason(); got != "正在退出，不能再开始扫描。" {
		t.Fatalf("the running exit reason = %q, want the running copy", got)
	}
	if m.startErr != "" || tt.HasText("上次开始失败") {
		t.Fatalf("the quitting block kept the past event: %q %q", m.startErr, tt.Texts())
	}

	// The teardown fails; the process stays running and cannot clean up.
	releaseTeardown()
	wait("the failed exit", func() bool { return m.exitState == exitFailed })
	m.Idle()
	drain()
	tt.Frame()
	if got := m.snap.StartBlocked; got != app.StartBlockedQuitting {
		t.Fatalf("the failed exit snapshot block = %q, want %q", got, app.StartBlockedQuitting)
	}
	const incomplete = "退出清理没有完成，不能再开始扫描；请退出 youkai 后重新打开。"
	if got := m.scanStartBlockedReason(); got != incomplete {
		t.Fatalf("the failed exit reason = %q, want %q", got, incomplete)
	}
	if m.startErr != "" || tt.HasText("上次开始失败") {
		t.Fatalf("the failed exit kept the past event: %q %q", m.startErr, tt.Texts())
	}
	if !tt.HasText(incomplete) {
		t.Fatalf("the failed exit does not explain the start block: %q", tt.Texts())
	}

	const cleanupReason = "退出清理没有完成，不能清理；请退出 youkai 后重新打开。"
	m.RequestRecordCleanup("none")
	tt.Frame()
	if m.cleanup != nil {
		t.Fatalf("the failed exit opened a cleanup preview: %+v", m.cleanup)
	}
	if m.cleanupNotice != cleanupReason {
		t.Fatalf("the cleanup notice = %q, want %q", m.cleanupNotice, cleanupReason)
	}
}
