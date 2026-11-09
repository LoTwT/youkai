package ui

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/egoist/mygo/ui"
)

// exitMatrixFixture is a results page whose first explicit exit stopped on a
// non-save teardown error. Every matrix state starts here, so the tray and
// dialog 仍然退出 entries exist in the same conditions as production.
type exitMatrixFixture struct {
	m       *Model
	q       *queueUpdater
	tt      *ui.Tester
	svc     *fakeService
	settled int
}

func newExitMatrixFixture(t *testing.T) *exitMatrixFixture {
	t.Helper()
	m, q, tt, svc := openReviewRecordPage(t, reviewRowItem(t, 1, 12))
	f := &exitMatrixFixture{m: m, q: q, tt: tt, svc: svc}
	m.cfg.OnExitSettled = func() { f.settled++ }
	svc.quitFn = func() error { return errors.New("close recognizer: busy") }
	m.AllowClose()
	drainAll(t, m, q)
	tt.Frame()
	if m.exitState != exitFailed || !m.exitFailureDialogOpen {
		t.Fatalf("the fixture did not reach a failed exit: %s", probeReviewState(m))
	}
	m.exitFailureDialogOpen = false
	tt.Frame()
	return f
}

func (f *exitMatrixFixture) dirty(t *testing.T) {
	t.Helper()
	f.m.startEdit()
	if f.m.edit == nil {
		t.Fatalf("the review editor did not open: %s", probeReviewState(f.m))
	}
	f.m.edit.draft.Level = "13"
	f.m.edit.refresh()
	f.tt.Frame()
}

func (f *exitMatrixFixture) blockedSave(t *testing.T, saveErr error) func() {
	t.Helper()
	orig := f.svc.saveFn
	entered := make(chan struct{})
	var once sync.Once
	release := make(chan struct{})
	f.svc.saveFn = func(recordID string, sequence, expectedRevision int64, edit app.ItemRevisionEdit) (app.ItemRevisionResult, error) {
		once.Do(func() { close(entered) })
		<-release
		if saveErr != nil {
			return app.ItemRevisionResult{}, saveErr
		}
		return orig(recordID, sequence, expectedRevision, edit)
	}
	f.tt.Frame()
	if err := f.tt.Click("保存修改"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the review save did not start")
	}
	return func() { close(release) }
}

type exitMatrixEntry struct {
	name   string
	force  bool
	invoke func(*Model)
}

type exitMatrixStateKind int

const (
	exitMatrixDirect exitMatrixStateKind = iota
	exitMatrixPrompt
	exitMatrixSaveSuccess
	exitMatrixSaveFailure
	exitMatrixFinalSaveFailure
)

type exitMatrixState struct {
	name  string
	kind  exitMatrixStateKind
	setup func(*testing.T, *exitMatrixFixture) func()
}

func TestExitIntentMatrix(t *testing.T) {
	entries := []exitMatrixEntry{
		{"tray 仍然退出", true, func(m *Model) { m.ExitAnyway() }},
		{"Ctrl+Q", false, func(m *Model) { m.AllowClose() }},
		{"关窗即退出", false, func(m *Model) { m.AllowClose() }},
		{"对话框仍然退出", true, func(m *Model) { m.ExitAnyway() }},
	}
	states := []exitMatrixState{
		{"无提示", exitMatrixDirect, func(*testing.T, *exitMatrixFixture) func() { return nil }},
		{"未保存", exitMatrixPrompt, func(t *testing.T, f *exitMatrixFixture) func() {
			f.dirty(t)
			return nil
		}},
		{"导航提示打开", exitMatrixPrompt, func(t *testing.T, f *exitMatrixFixture) func() {
			f.dirty(t)
			f.m.navigatePage(pageSettings)
			f.tt.Frame()
			if f.m.navPrompt == nil || f.m.navPrompt.kind != navPage {
				t.Fatalf("the navigation prompt did not open: %s", probeReviewState(f.m))
			}
			return nil
		}},
		{"已有退出提示", exitMatrixPrompt, func(t *testing.T, f *exitMatrixFixture) func() {
			f.dirty(t)
			f.m.requestExit(false)
			f.tt.Frame()
			if f.m.navPrompt == nil || f.m.navPrompt.kind != navExit || f.m.exitIntent != exitIntentNormal {
				t.Fatalf("the exit prompt did not open: %s", probeReviewState(f.m))
			}
			return nil
		}},
		{"保存中后成功", exitMatrixSaveSuccess, func(t *testing.T, f *exitMatrixFixture) func() {
			f.dirty(t)
			return f.blockedSave(t, nil)
		}},
		{"保存中后失败", exitMatrixSaveFailure, func(t *testing.T, f *exitMatrixFixture) func() {
			f.dirty(t)
			return f.blockedSave(t, errors.New("磁盘已满"))
		}},
		{"保存失败（最终保存）", exitMatrixFinalSaveFailure, func(t *testing.T, f *exitMatrixFixture) func() {
			f.m.exitState = exitFailed
			f.m.exitFailureWasSave = true
			f.m.snap.SaveError = "磁盘已满"
			f.m.exitFailureDialogOpen = true
			f.tt.Frame()
			return nil
		}},
	}

	for _, state := range states {
		state := state
		t.Run(state.name, func(t *testing.T) {
			for _, entry := range entries {
				entry := entry
				t.Run(entry.name, func(t *testing.T) {
					f := newExitMatrixFixture(t)
					release := state.setup(t, f)
					beforeQuits := f.svc.quits

					entry.invoke(f.m)

					switch state.kind {
					case exitMatrixPrompt:
						f.tt.Frame()
						if f.m.navPrompt == nil || f.m.navPrompt.kind != navExit {
							t.Fatalf("the exit request did not own the prompt: %s", probeReviewState(f.m))
						}
						if f.m.edit == nil || f.m.edit.draft.Level != "13" {
							t.Fatalf("the prompt lost the unsaved edit: %s", probeReviewState(f.m))
						}
						if f.m.exitIntent != exitIntentNormal && f.m.exitIntent != exitIntentForce {
							t.Fatalf("the exit request was not recorded: %s", probeReviewState(f.m))
						}
						if err := f.tt.Click("放弃修改"); err != nil {
							t.Fatal(err)
						}
						drainAll(t, f.m, f.q)
					case exitMatrixSaveSuccess, exitMatrixSaveFailure:
						f.tt.Frame()
						if f.m.navPrompt == nil || f.m.navPrompt.kind != navExit {
							t.Fatalf("the in-flight save did not show the exit prompt: %s", probeReviewState(f.m))
						}
						release()
						drainAll(t, f.m, f.q)
					case exitMatrixDirect, exitMatrixFinalSaveFailure:
						drainAll(t, f.m, f.q)
					}

					if state.kind == exitMatrixSaveFailure {
						if f.settled != 0 || f.m.exitState == exitDone {
							t.Fatalf("a failed save settled the exit: state %v settled %d", f.m.exitState, f.settled)
						}
						if f.m.edit == nil || f.m.edit.draft.Level != "13" {
							t.Fatalf("the failed save lost the edit: %s", probeReviewState(f.m))
						}
						if f.m.navPrompt == nil || !strings.Contains(f.m.reviewErr, "保存失败") {
							t.Fatalf("the failed save did not leave a truthful prompt: %s", probeReviewState(f.m))
						}
						return
					}
					if state.kind == exitMatrixFinalSaveFailure {
						if f.settled != 0 || f.m.exitState == exitDone {
							t.Fatalf("a final-save failure was bypassed: state %v settled %d", f.m.exitState, f.settled)
						}
						if !f.m.exitFailureDialogOpen {
							t.Fatalf("the retry path lost the failure dialog: %s", probeReviewState(f.m))
						}
						return
					}
					if entry.force {
						if f.m.exitState != exitDone || f.settled != 1 {
							t.Fatalf("仍然退出 did not settle: state %v settled %d", f.m.exitState, f.settled)
						}
						if f.m.exitFailureDialogOpen {
							t.Fatalf("仍然退出 left the failure dialog open: %s", probeReviewState(f.m))
						}
						if f.svc.quits != beforeQuits {
							t.Fatalf("仍然退出 retried cleanup: quits %d -> %d", beforeQuits, f.svc.quits)
						}
						return
					}
					if f.m.exitState != exitFailed || f.settled != 0 || !f.m.exitFailureDialogOpen {
						t.Fatalf("generic exit after cleanup failure: state %v settled %d dialog %v",
							f.m.exitState, f.settled, f.m.exitFailureDialogOpen)
					}
					if f.svc.quits != beforeQuits {
						t.Fatalf("generic exit retried cleanup: quits %d -> %d", beforeQuits, f.svc.quits)
					}
				})
			}
		})
	}
}

// TestExitReplacesANavigationPromptAtIdle covers R5-1 on the first exit: a
// settings navigation prompt must not swallow ⌘Q or a close, and the old
// destination must not run after 放弃修改.
func TestExitReplacesANavigationPromptAtIdle(t *testing.T) {
	m, q, tt, svc := openReviewRecordPage(t, reviewRowItem(t, 1, 12))
	tt.Frame()
	m.edit.draft.Level = "13"
	m.edit.refresh()
	tt.Frame()
	m.navigatePage(pageSettings)
	tt.Frame()
	if m.navPrompt == nil || m.navPrompt.kind != navPage {
		t.Fatalf("the navigation prompt did not open: %s", probeReviewState(m))
	}

	if m.AllowClose() {
		t.Fatal("the close was allowed with an unsaved edit")
	}
	tt.Frame()
	if m.navPrompt == nil || m.navPrompt.kind != navExit {
		t.Fatalf("the quit did not replace the navigation prompt: %s", probeReviewState(m))
	}
	if err := tt.Click("放弃修改"); err != nil {
		t.Fatal(err)
	}
	drainAll(t, m, q)
	if svc.quits != 1 || m.exitState != exitDone {
		t.Fatalf("the replaced prompt did not exit: quits %d state %v", svc.quits, m.exitState)
	}
	if m.displayPage() == pageSettings {
		t.Fatal("the discarded settings navigation still ran")
	}
}

// TestExitSurvivesARealSaveAtIdle covers R5-2 without mutating model state:
// the save callback itself must continue the quit request.
func TestExitSurvivesARealSaveAtIdle(t *testing.T) {
	f := &exitMatrixFixture{}
	m, q, tt, svc := openReviewRecordPage(t, reviewRowItem(t, 1, 12))
	f.m, f.q, f.tt, f.svc = m, q, tt, svc
	m.cfg.OnExitSettled = func() { f.settled++ }
	m.edit.draft.Level = "13"
	m.edit.refresh()
	release := f.blockedSave(t, nil)

	m.AllowClose()
	tt.Frame()
	if m.navPrompt == nil || m.navPrompt.kind != navExit || m.exitIntent != exitIntentNormal {
		t.Fatalf("the quit during a save was not recorded: %s", probeReviewState(m))
	}
	release()
	drainAll(t, m, q)
	if m.exitState != exitDone || f.settled != 1 || svc.quits != 1 {
		t.Fatalf("the save completion did not settle the quit: state %v settled %d quits %d",
			m.exitState, f.settled, svc.quits)
	}
	if m.edit != nil || m.navPrompt != nil {
		t.Fatalf("the settled exit left editor or prompt state: %s", probeReviewState(m))
	}
}

// TestGenericExitDoesNotDowngradeForcedPrompt covers R5-1's upgrade rule:
// a force request already waiting must survive a later generic quit.
func TestGenericExitDoesNotDowngradeForcedPrompt(t *testing.T) {
	f := newExitMatrixFixture(t)
	f.dirty(t)
	f.m.ExitAnyway()
	f.tt.Frame()
	if f.m.exitIntent != exitIntentForce {
		t.Fatalf("the tray did not record a forced intent: %s", probeReviewState(f.m))
	}
	f.m.AllowClose()
	if f.m.exitIntent != exitIntentForce {
		t.Fatalf("the generic quit downgraded 仍然退出: %s", probeReviewState(f.m))
	}
	if err := f.tt.Click("放弃修改"); err != nil {
		t.Fatal(err)
	}
	drainAll(t, f.m, f.q)
	if f.m.exitState != exitDone || f.settled != 1 {
		t.Fatalf("the upgraded prompt did not settle: state %v settled %d", f.m.exitState, f.settled)
	}
}

// TestExitPromptRetriesAFailedConfirmation covers R6-1: a clean editor can
// still be waiting for a failed 确认并下一项. 保存并继续 must repeat that
// confirmed write before the exit intent continues.
func TestExitPromptRetriesAFailedConfirmation(t *testing.T) {
	settled := 0
	m, q, tt, svc := openReviewRecordPage(t,
		reviewRowItem(t, 1, 12),
		reviewRowItem(t, 2, 12),
	)
	m.cfg.OnExitSettled = func() { settled++ }
	orig := svc.saveFn
	entered := make(chan struct{})
	var once sync.Once
	release := make(chan struct{})
	var calls atomic.Int32
	svc.saveFn = func(recordID string, sequence, expectedRevision int64, edit app.ItemRevisionEdit) (app.ItemRevisionResult, error) {
		if calls.Add(1) == 1 {
			once.Do(func() { close(entered) })
			<-release
			return app.ItemRevisionResult{}, errors.New("磁盘已满")
		}
		return orig(recordID, sequence, expectedRevision, edit)
	}
	tt.Frame()
	if err := tt.Click("确认并下一项"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the confirmation did not start")
	}
	m.AllowClose()
	tt.Frame()
	if m.navPrompt == nil || m.navPrompt.kind != navExit || m.exitIntent != exitIntentNormal {
		t.Fatalf("the quit during confirmation was not recorded: %s", probeReviewState(m))
	}
	close(release)
	drainAll(t, m, q)
	tt.Frame()
	if calls.Load() != 1 || m.edit == nil || m.edit.dirty() {
		t.Fatalf("first confirmation: calls=%d edit=%+v", calls.Load(), m.edit)
	}
	if pending := m.pendingItemsCount(); pending != 2 {
		t.Fatalf("the failed confirmation settled %d items, want 2 pending", 2-pending)
	}
	if m.navPrompt == nil || !strings.Contains(m.reviewErr, "保存失败") {
		t.Fatalf("the failed confirmation lost its prompt: %s", probeReviewState(m))
	}
	if !tt.HasText("第 1 件还没有确认") ||
		!tt.HasText("上次确认没有完成。重试确认后继续，或放弃这次确认。") {
		t.Fatalf("the prompt still describes an unsaved edit: %q", tt.Texts())
	}
	if tt.HasText("第 1 件有未保存的修改") {
		t.Fatalf("the prompt claims an edit that does not exist: %q", tt.Texts())
	}

	if err := tt.Click("保存并继续"); err != nil {
		t.Fatal(err)
	}
	drainAll(t, m, q)
	if calls.Load() != 2 {
		t.Fatalf("保存并继续 ran %d saves, want the failed confirmation retried", calls.Load())
	}
	if len(svc.saves) != 2 || !svc.saves[1].edit.Confirmed {
		t.Fatalf("the retried action lost its confirmation semantics: %+v", svc.saves)
	}
	if m.exitState != exitDone || settled != 1 {
		t.Fatalf("the retried confirmation did not continue the exit: state %v settled %d",
			m.exitState, settled)
	}
}

// TestStayCancelsExitIntent covers M1: 留在这里 must drop the exit intent so
// a later ordinary save cannot quit the application.
func TestStayCancelsExitIntent(t *testing.T) {
	m, q, tt, svc := openReviewRecordPage(t, reviewRowItem(t, 1, 12))
	tt.Frame()
	m.edit.draft.Level = "13"
	m.edit.refresh()
	tt.Frame()
	m.AllowClose()
	tt.Frame()
	if m.navPrompt == nil || m.navPrompt.kind != navExit || m.exitIntent != exitIntentNormal {
		t.Fatalf("the exit prompt did not open: %s", probeReviewState(m))
	}
	if err := tt.Click("留在这里"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if m.navPrompt != nil || m.exitIntent != exitIntentNone {
		t.Fatalf("留在这里 kept the exit intent: %s", probeReviewState(m))
	}
	if err := tt.Click("保存修改"); err != nil {
		t.Fatal(err)
	}
	drainAll(t, m, q)
	if svc.quits != 0 || m.exitState != exitIdle {
		t.Fatalf("a later save exited after 留在这里: quits=%d state=%v", svc.quits, m.exitState)
	}
}

// TestFailedReloadCancelsExitIntent covers CL-2: when a record reload fails
// while the exit prompt is open, the prompt disappears and the quit must
// disappear with it. The test constructs the prompt over a dirty draft so it
// can also observe that navigation reaches its ordinary unsaved-input prompt
// instead of being silently refused; users cannot start a reload from that
// editor. The reachable form of this state is a failed confirmation's prompt
// over a clean editor followed by a failed live reload.
func TestFailedReloadCancelsExitIntent(t *testing.T) {
	m, q, tt, svc := openReviewRecordPage(t, reviewRowItem(t, 1, 12))
	tt.Frame()
	m.edit.draft.Level = "13"
	m.edit.refresh()
	tt.Frame()
	id := m.rec.ID
	// A reload is already in flight when the user quits; the read then
	// fails, so the exit prompt can no longer offer its save.
	svc.loadErr = map[string]error{id: errors.New("read failed")}
	m.reloadRecord(id, m.rec.Live)
	if m.AllowClose() {
		t.Fatal("the close was allowed with an unsaved edit")
	}
	drainAll(t, m, q)
	tt.Frame()
	if m.recErr == "" || !m.editingDirty() {
		t.Fatalf("the failed load did not keep a blocked draft: %s", probeReviewState(m))
	}
	if m.exitIntent != exitIntentNone || m.navPrompt != nil {
		t.Fatalf("the failed load kept the exit intent: %s", probeReviewState(m))
	}

	m.navigatePage(pageSettings)
	tt.Frame()
	if m.navPrompt == nil || m.navPrompt.kind != navPage {
		t.Fatalf("navigation after the failed load was silently refused: %s", probeReviewState(m))
	}
	if err := tt.Click("留在这里"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()

	// The read recovers; the draft can be saved, but that save must not
	// resume the quit whose prompt was already gone.
	svc.loadErr = nil
	m.reloadRecord(id, m.rec.Live)
	drainAll(t, m, q)
	tt.Frame()
	if m.recErr != "" || m.edit == nil {
		t.Fatalf("the recovered load lost the draft: %s", probeReviewState(m))
	}
	m.saveEdit(reviewSave, nil)
	drainAll(t, m, q)
	tt.Frame()
	if svc.quits != 0 || m.exitState != exitIdle {
		t.Fatalf("a save after the failed load exited without asking: quits=%d state=%v",
			svc.quits, m.exitState)
	}
}

// TestRevisionConflictCancelsExitIntent covers M4: a conflict during the
// prompt's save must cancel the quit; a later successful save must not exit.
func TestRevisionConflictCancelsExitIntent(t *testing.T) {
	m, q, tt, svc := openReviewRecordPage(t, reviewRowItem(t, 1, 12))
	tt.Frame()
	m.edit.draft.Level = "13"
	m.edit.refresh()
	tt.Frame()
	m.AllowClose()
	tt.Frame()
	if m.navPrompt == nil || m.exitIntent != exitIntentNormal {
		t.Fatalf("the exit prompt did not open: %s", probeReviewState(m))
	}

	svc.mu.Lock()
	svc.loads["r1"].Checkpoint.Items[0].Revision = 2
	svc.mu.Unlock()
	if err := tt.Click("保存并继续"); err != nil {
		t.Fatal(err)
	}
	drainAll(t, m, q)
	tt.Frame()
	if m.exitIntent != exitIntentNone || m.navPrompt != nil || m.exitState != exitIdle {
		t.Fatalf("the conflict kept the exit intent: %s", probeReviewState(m))
	}
	if svc.quits != 0 {
		t.Fatalf("the conflict quit the application: %d", svc.quits)
	}

	m.startEdit()
	if m.edit == nil {
		t.Fatalf("the reloaded item did not open an editor: %s", probeReviewState(m))
	}
	m.edit.draft.Level = "14"
	m.edit.refresh()
	tt.Frame()
	if err := tt.Click("保存修改"); err != nil {
		t.Fatal(err)
	}
	drainAll(t, m, q)
	if svc.quits != 0 || m.exitState != exitIdle {
		t.Fatalf("a save after the conflict exited: quits=%d state=%v", svc.quits, m.exitState)
	}
}

// TestExitIntentSurvivesSaveFailureRetry covers M10: the failed save keeps
// the exit intent, so retrying through 保存并继续 exits only after success.
func TestExitIntentSurvivesSaveFailureRetry(t *testing.T) {
	f := newExitMatrixFixture(t)
	f.dirty(t)
	orig := f.svc.saveFn
	release := f.blockedSave(t, errors.New("磁盘已满"))
	f.m.ExitAnyway()
	f.tt.Frame()
	if f.m.exitIntent != exitIntentForce || f.m.navPrompt == nil {
		t.Fatalf("the forced exit was not recorded: %s", probeReviewState(f.m))
	}
	release()
	drainAll(t, f.m, f.q)
	f.tt.Frame()
	if f.settled != 0 || f.m.exitState == exitDone || f.m.edit == nil {
		t.Fatalf("the failed save did not keep the exit pending: %s", probeReviewState(f.m))
	}
	if f.m.exitIntent != exitIntentForce {
		t.Fatalf("the failed save cleared the exit intent: %s", probeReviewState(f.m))
	}

	f.svc.saveFn = orig
	if err := f.tt.Click("保存并继续"); err != nil {
		t.Fatal(err)
	}
	drainAll(t, f.m, f.q)
	if f.m.exitState != exitDone || f.settled != 1 {
		t.Fatalf("the retried save did not continue the exit: state %v settled %d",
			f.m.exitState, f.settled)
	}
}

// TestExitPromptRetryFailureKeepsTheExitPending covers the other side of
// R6-1: if 保存并继续 retries the confirmation and it fails again, the
// prompt stays, the edit is intact and no exit settles.
func TestExitPromptRetryFailureKeepsTheExitPending(t *testing.T) {
	settled := 0
	m, q, tt, svc := openReviewRecordPage(t,
		reviewRowItem(t, 1, 12),
		reviewRowItem(t, 2, 12),
	)
	m.cfg.OnExitSettled = func() { settled++ }
	entered := make(chan struct{})
	var once sync.Once
	release := make(chan struct{})
	var calls atomic.Int32
	svc.saveFn = func(string, int64, int64, app.ItemRevisionEdit) (app.ItemRevisionResult, error) {
		n := calls.Add(1)
		if n == 1 {
			once.Do(func() { close(entered) })
			<-release
		}
		return app.ItemRevisionResult{}, errors.New("磁盘已满")
	}
	tt.Frame()
	if err := tt.Click("确认并下一项"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the confirmation did not start")
	}
	m.AllowClose()
	tt.Frame()
	close(release)
	drainAll(t, m, q)
	tt.Frame()
	if err := tt.Click("保存并继续"); err != nil {
		t.Fatal(err)
	}
	drainAll(t, m, q)
	tt.Frame()
	if calls.Load() != 2 {
		t.Fatalf("保存并继续 issued %d saves, want a second confirmed write", calls.Load())
	}
	if len(svc.saves) != 2 || !svc.saves[1].edit.Confirmed {
		t.Fatalf("the retry lost its confirmation semantics: %+v", svc.saves)
	}
	if settled != 0 || m.exitState == exitDone {
		t.Fatalf("a failed retry settled the exit: state %v settled %d", m.exitState, settled)
	}
	if m.navPrompt == nil || !strings.Contains(m.reviewErr, "保存失败") {
		t.Fatalf("the failed retry lost its prompt: %s", probeReviewState(m))
	}
	if m.edit == nil || m.edit.dirty() {
		t.Fatalf("the failed retry lost the confirmation target: %s", probeReviewState(m))
	}
	if pending := m.pendingItemsCount(); pending != 2 {
		t.Fatalf("the failed retry settled %d items, want 2 pending", 2-pending)
	}
}

// failedConfirmPrompt reaches the production state where an exit prompt stays
// open over a clean editor: the quit arrived while a confirmation was saving,
// and that write then failed. 保存并继续 remains the way to retry the
// confirmation, so the prompt cannot be treated as an unsaved draft.
func failedConfirmPrompt(t *testing.T, items ...record.ItemRecord) (*Model, *queueUpdater, *ui.Tester, *fakeService) {
	t.Helper()
	m, q, tt, svc := openReviewRecordPage(t, items...)
	orig := svc.saveFn
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	first := true
	svc.saveFn = func(recordID string, sequence, expectedRevision int64, edit app.ItemRevisionEdit) (app.ItemRevisionResult, error) {
		if first {
			first = false
			once.Do(func() { close(entered) })
			<-release
			return app.ItemRevisionResult{}, errors.New("磁盘已满")
		}
		return orig(recordID, sequence, expectedRevision, edit)
	}
	tt.Frame()
	if err := tt.Click("确认并下一项"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the confirmation did not start")
	}
	m.AllowClose()
	tt.Frame()
	close(release)
	drainAll(t, m, q)
	tt.Frame()
	if m.navPrompt == nil || m.navPrompt.kind != navExit || m.exitIntent == exitIntentNone || m.editingDirty() {
		t.Fatalf("fixture: %s", probeReviewState(m))
	}
	return m, q, tt, svc
}

// TestShowRecordCancelsExitIntent covers CL-2's showRecord path: a new scan
// session replaces the workspace while a failed confirmation's exit prompt is
// open. The prompt and the quit it carried must both disappear, or every
// later navigation is silently refused.
func TestShowRecordCancelsExitIntent(t *testing.T) {
	m, q, tt, _ := failedConfirmPrompt(t, reviewRowItem(t, 1, 12), reviewRowItem(t, 2, 12))
	m.Apply(app.Snapshot{SessionID: "new-session"})
	drainAll(t, m, q)
	tt.Frame()
	if m.exitIntent != exitIntentNone || m.navPrompt != nil {
		t.Fatalf("the new session kept the exit intent: %s", probeReviewState(m))
	}
	m.navigatePage(pageSettings)
	tt.Frame()
	if m.displayPage() != pageSettings {
		t.Fatalf("navigation after the new session was silently refused: page %v", m.displayPage())
	}
}

// TestClearResultsLockedCancelsExitIntent keeps the same invariant on the
// deletion path: clearing the results area abandons its prompt and the exit
// intent it carried. No ordering was found that reaches deletion with the
// exit prompt still open, so this asserts the state directly.
func TestClearResultsLockedCancelsExitIntent(t *testing.T) {
	m, _, _, _ := failedConfirmPrompt(t, reviewRowItem(t, 1, 12), reviewRowItem(t, 2, 12))
	m.clearResultsLocked()
	if m.navPrompt != nil || m.exitIntent != exitIntentNone {
		t.Fatalf("clearResultsLocked kept the exit intent: %s", probeReviewState(m))
	}
}

// TestStartEditRowCancelsExitIntent covers the remaining CL-2 residue: a
// confirmation is durable but reports an error, so the exit prompt stays over
// a clean editor; a later successful live reload removes the confirmed row
// from the pending segment and the renderer opens the next row through
// startEditRow. That transition must abandon the prompt together with the
// quit, or navigation is silently refused and a later save exits.
func TestStartEditRowCancelsExitIntent(t *testing.T) {
	m, q, tt, svc := openReviewRecordPage(t, reviewRowItem(t, 1, 12), reviewRowItem(t, 2, 12))
	m.requestResultFilter(resultFilterPending, "", "")
	m.rec.Live = true
	m.appliedSession = "r1"
	drainAll(t, m, q)
	m.rec.Live = true
	tt.Frame()
	orig := svc.saveFn
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	first := true
	svc.saveFn = func(recordID string, sequence, expectedRevision int64, edit app.ItemRevisionEdit) (app.ItemRevisionResult, error) {
		if first {
			first = false
			once.Do(func() { close(entered) })
			<-release
			if _, err := orig(recordID, sequence, expectedRevision, edit); err != nil {
				t.Errorf("orig: %v", err)
			}
			return app.ItemRevisionResult{}, errors.New("post-commit failure")
		}
		return orig(recordID, sequence, expectedRevision, edit)
	}
	tt.Frame()
	if err := tt.Click("确认并下一项"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the confirmation did not start")
	}
	m.AllowClose()
	tt.Frame()
	close(release)
	drainAll(t, m, q)
	tt.Frame()
	if m.navPrompt == nil || m.exitIntent == exitIntentNone {
		t.Fatalf("fixture: %s", probeReviewState(m))
	}

	m.Apply(app.Snapshot{SessionID: "r1", Revision: 2, SavedCount: m.liveSavedShown + 1})
	drainAll(t, m, q)
	tt.Frame()
	tt.Frame()
	if m.navPrompt != nil || m.exitIntent != exitIntentNone {
		t.Fatalf("startEditRow left the exit intent without its prompt: %s", probeReviewState(m))
	}
	if m.edit == nil || m.edit.seq != 2 {
		t.Fatalf("the renderer did not open the next pending row: %s", probeReviewState(m))
	}
	m.navigatePage(pageSettings)
	tt.Frame()
	if m.displayPage() != pageSettings {
		t.Fatalf("navigation after the live reload was silently refused: page %v", m.displayPage())
	}
}
