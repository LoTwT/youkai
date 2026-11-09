package ui

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/LoTwT/youkai/internal/scan"
	"github.com/egoist/mygo/ui"
)

// TestTrayHintAcknowledgementKeepsUnsavedExitPrompt covers PR-18: a quit
// request arriving while the first-hide hint is open leaves the unsaved
// dialog waiting; acknowledging the hint must not hide that dialog.
func TestTrayHintAcknowledgementKeepsUnsavedExitPrompt(t *testing.T) {
	hides := 0
	m, _, tt, _ := openReviewableResults(t, recovery(reviewRowItem(t, 1, 12)), 1180, 760)
	m.cfg.HideToTray = func() { hides++ }
	tt.Frame()
	m.edit.draft.Level = "13"
	m.edit.refresh()
	tt.Frame()

	m.ShowFirstHideHint()
	m.AllowClose()
	tt.Frame()
	if m.navPrompt == nil {
		t.Fatalf("AllowClose did not keep the unsaved prompt: %s", probeReviewState(m))
	}
	if err := tt.Click("知道了"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if hides != 0 {
		t.Fatalf("acknowledging the hint hid the window with an unsaved prompt open")
	}
	if m.navPrompt == nil {
		t.Fatal("acknowledging the hint discarded the pending exit prompt")
	}
	if m.exitState != exitIdle {
		t.Fatalf("the exit state = %v, want idle until the prompt settles", m.exitState)
	}
	if !tt.HasText("有未保存的修改") {
		t.Fatalf("the unsaved prompt is not visible: %q", tt.Texts())
	}
}

// TestFirstHideHintOnceAndReplay checks the first close's hint, its
// persistence, the second close's silence, and the settings replay.
func TestFirstHideHintOnceAndReplay(t *testing.T) {
	var saved []bool
	hides := 0
	m, q := newTestModel(t, Config{
		Service:          &fakeService{},
		HideToTray:       func() { hides++ },
		SaveTrayHintSeen: func(seen bool) error { saved = append(saved, seen); return nil },
	})
	m.page = pageSettings
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)

	if !m.FirstHideHintDue() {
		t.Fatal("a fresh model already considers the hint seen")
	}
	if !tt.HasText("再看一次托盘图标位置提示") {
		t.Fatal("settings has no tray-hint replay entry")
	}

	m.ShowFirstHideHint()
	tt.Frame()
	for _, want := range []string{
		"youkai 会继续在后台运行",
		"图标在任务栏右侧的通知区域",
		"之后可在 设置 › 常规 重看",
		"知道了",
	} {
		if !tt.HasText(want) {
			t.Fatalf("the first-hide hint does not show %q: %q", want, tt.Texts())
		}
	}
	if err := tt.Click("知道了"); err != nil {
		t.Fatal(err)
	}
	drainAll(t, m, q)
	tt.Frame()
	if m.FirstHideHintDue() {
		t.Fatal("acknowledging did not mark the hint seen")
	}
	if hides != 1 {
		t.Fatalf("hides = %d, want the close path to hide after acknowledgement", hides)
	}
	if len(saved) != 1 || !saved[0] {
		t.Fatalf("saved hint state = %v, want true", saved)
	}
	if tt.HasText("youkai 会继续在后台运行") {
		t.Fatal("the dismissed hint stayed visible")
	}

	// The settings entry reopens the hint without hiding the window.
	if err := tt.Click("再看一次托盘图标位置提示"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("youkai 会继续在后台运行") {
		t.Fatal("the settings replay did not open the hint")
	}
	if err := tt.Click("知道了"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if hides != 1 {
		t.Fatalf("the replay hid the window: hides=%d", hides)
	}
	if tt.HasText("youkai 会继续在后台运行") {
		t.Fatal("the replayed hint stayed visible")
	}
}

// TestFirstHideHintHidesAfterCleanupFailure covers the close-to-tray
// preference after a failed cleanup: acknowledging the hint must hide the
// window even though the exit state is failed, because closing did not
// reopen the failure dialog.
func TestFirstHideHintHidesAfterCleanupFailure(t *testing.T) {
	hides := 0
	m, q := newTestModel(t, Config{
		Service:    &fakeService{quitFn: func() error { return errors.New("close recognizer: busy") }},
		HideToTray: func() { hides++ },
	})
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)
	m.AllowClose()
	drainAll(t, m, q)
	tt.Frame()
	if m.exitState != exitFailed {
		t.Fatalf("the fixture did not fail the exit: %v", m.exitState)
	}
	m.exitFailureDialogOpen = false
	tt.Frame()

	m.ShowFirstHideHint()
	tt.Frame()
	if err := tt.Click("知道了"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if hides != 1 {
		t.Fatalf("acknowledging the hint after a failed cleanup hid %d times", hides)
	}
	if m.navPrompt != nil {
		t.Fatalf("hiding left a prompt behind the window: %s", probeReviewState(m))
	}
}

// TestFirstHideHintDismissalKeepsItDue checks Escape/backdrop: closing the
// dialog without its button neither acknowledges nor hides.
func TestFirstHideHintDismissalKeepsItDue(t *testing.T) {
	hides := 0
	m, _ := newTestModel(t, Config{Service: &fakeService{}, HideToTray: func() { hides++ }})
	m.ShowFirstHideHint()
	m.trayHintDismissed()
	if !m.FirstHideHintDue() {
		t.Fatal("dismissing acknowledged the hint")
	}
	if hides != 0 {
		t.Fatalf("dismissing hid the window: %d", hides)
	}
}

// TestBeginExitClosesAnIdleExportDialog covers exit-keeps-export: an open
// export dialog must not stay under the exit progress. A write in flight
// keeps its dialog by the export's own rule.
func TestBeginExitClosesAnIdleExportDialog(t *testing.T) {
	f := newExportDialogFixture(t, testRecord(), false)
	f.open(t)
	f.m.beginExit()
	if f.m.exportDialog.open {
		t.Fatal("the exit left the export dialog open")
	}
	drainAll(t, f.m, f.q)
}

// TestBeginExitClosesTheFirstHideHint checks L-3: the exit progress owns
// the window instead of stacking under the still-open hint.
func TestBeginExitClosesTheFirstHideHint(t *testing.T) {
	m, q := newTestModel(t, Config{Service: &fakeService{}})
	m.ShowFirstHideHint()
	m.beginExit()
	if m.trayHintOpen || m.trayHintHideOnDone {
		t.Fatalf("the exit left the hint open=%v hideOnDone=%v",
			m.trayHintOpen, m.trayHintHideOnDone)
	}
	drainAll(t, m, q)
}

// TestExitFailureStatusFollowsTheRetriedSave checks M-3: a successful
// retry clears the save failure, and the tray must stop claiming the
// result is unsaved just because the exit attempt failed earlier.
func TestExitFailureStatusFollowsTheRetriedSave(t *testing.T) {
	m, q := newTestModel(t, Config{Service: &fakeService{}})
	m.exitState = exitFailed
	m.exitFailureWasSave = true
	failed := snap("s1", 2, true, scan.PhaseEnded,
		scan.Counts{Captured: 3}, 2, true, "磁盘已满")
	failed.FinalSaveFailures = 1
	m.Apply(failed)
	if got := m.LifecycleStatus().Text; got != "保存失败" {
		t.Fatalf("tray status with an unsaved result = %q, want 保存失败", got)
	}

	retried := snap("s1", 3, false, scan.PhaseEnded,
		scan.Counts{Captured: 3}, 3, false, "")
	m.Apply(retried)
	drainAll(t, m, q)
	if got := m.LifecycleStatus().Text; got != "就绪" {
		t.Fatalf("tray status after the retried save = %q, want 就绪", got)
	}
	labels := m.LifecycleStatus().TrayMenu()
	if labels.Open != "打开 youkai" || labels.Exit != "退出 youkai" {
		t.Fatalf("stale failed-exit menu after the retried save: %+v", labels)
	}
}

// TestExitFailureWithoutSaveErrorKeepsAnExitFailedTrayState covers N-3: a
// teardown error unrelated to the final save must not leave the tray
// claiming the application is ready.
func TestExitFailureWithoutSaveErrorKeepsAnExitFailedTrayState(t *testing.T) {
	m, q := newTestModel(t, Config{Service: &fakeService{
		quitFn: func() error { return errors.New("close recognizer: busy") },
	}})
	m.AllowClose()
	drainAll(t, m, q)
	if m.exitState != exitFailed {
		t.Fatalf("the failed exit settled as %v", m.exitState)
	}
	status := m.LifecycleStatus()
	if status.Text != "退出失败" || !status.ExitCleanupFailed {
		t.Fatalf("tray status after a non-save exit failure = %+v", status)
	}
	labels := status.TrayMenu()
	if labels.Status != "youkai · 退出清理未完成" || labels.Open != "打开 youkai" ||
		labels.Exit != "仍然退出" {
		t.Fatalf("tray menu after a non-save exit failure = %+v", labels)
	}
}

// TestExitCleanupFailureGenericAndForcedExits covers decision 2: a generic
// quit reopens the failure dialog without another doomed Quit, while the
// tray's 仍然退出 settles directly.
func TestExitCleanupFailureGenericAndForcedExits(t *testing.T) {
	attempts := 0
	settled := 0
	m, q := newTestModel(t, Config{
		Service: &fakeService{quitFn: func() error {
			attempts++
			return errors.New("close recognizer: busy")
		}},
		OnExitSettled: func() { settled++ },
	})
	m.AllowClose()
	drainAll(t, m, q)
	if attempts != 1 || m.exitState != exitFailed {
		t.Fatalf("first exit: attempts %d state %v", attempts, m.exitState)
	}
	if !m.exitFailureDialogOpen {
		t.Fatal("the first failure did not open its dialog")
	}
	m.AllowClose()
	drainAll(t, m, q)
	if attempts != 1 || m.exitState != exitFailed || !m.exitFailureDialogOpen {
		t.Fatalf("generic exit with the dialog open: attempts %d state %v dialog %v",
			attempts, m.exitState, m.exitFailureDialogOpen)
	}
	m.exitFailureDialogOpen = false
	m.AllowClose()
	drainAll(t, m, q)
	if attempts != 1 || m.exitState != exitFailed || !m.exitFailureDialogOpen {
		t.Fatalf("generic exit: attempts %d state %v dialog %v",
			attempts, m.exitState, m.exitFailureDialogOpen)
	}
	m.ExitAnyway()
	drainAll(t, m, q)
	if attempts != 1 {
		t.Fatalf("仍然退出 retried cleanup %d times", attempts)
	}
	if m.exitState != exitDone || settled != 1 {
		t.Fatalf("仍然退出 did not settle: state %v settled %d", m.exitState, settled)
	}
}

// TestExitCleanupFailureOffersExitAnyway covers the dialog: only a non-save
// failure offers 仍然退出, and the button uses the settled exit path.
func TestExitCleanupFailureOffersExitAnyway(t *testing.T) {
	settled := 0
	m, q := newTestModel(t, Config{
		Service:       &fakeService{quitFn: func() error { return errors.New("close recognizer: busy") }},
		OnExitSettled: func() { settled++ },
	})
	m.snap.SessionID = "s1"
	m.snap.SavedCount = 1
	m.SetContentSize(1020, 700)
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)
	m.AllowClose()
	drainAll(t, m, q)
	tt.Frame()
	if !tt.HasText("仍然退出") || !tt.HasText("扫描结果已保存") {
		t.Fatalf("the cleanup failure dialog is incomplete: %q", tt.Texts())
	}
	if labels := m.LifecycleStatus().TrayMenu(); labels.Status != "youkai · 退出清理未完成，结果已保存" {
		t.Fatalf("the session tray status = %q", labels.Status)
	}
	if err := tt.Click("仍然退出"); err != nil {
		t.Fatal(err)
	}
	drainAll(t, m, q)
	if m.exitState != exitDone || settled != 1 {
		t.Fatalf("仍然退出 did not settle: state %v settled %d", m.exitState, settled)
	}
}

// TestExitSaveFailureCannotExitAnyway covers the other side: a failed final
// save keeps its retry path and never offers a bypass.
func TestExitSaveFailureCannotExitAnyway(t *testing.T) {
	settled := 0
	m := &Model{snap: app.Snapshot{SaveError: "磁盘已满"}}
	m.exitState = exitFailed
	m.exitFailureWasSave = true
	m.exitFailureDialogOpen = true
	m.cfg.OnExitSettled = func() { settled++ }
	m.SetContentSize(1020, 700)
	tt := ui.NewTester(m.View, 1020, 700)
	tt.Frame()
	if tt.HasText("仍然退出") {
		t.Fatalf("a save failure offered 仍然退出: %q", tt.Texts())
	}
	if !tt.HasText("重试保存") {
		t.Fatalf("a save failure lost its retry path: %q", tt.Texts())
	}
	m.exitAnyway()
	if m.exitState != exitFailed || settled != 0 {
		t.Fatalf("exitAnyway bypassed the save failure: state %v settled %d",
			m.exitState, settled)
	}
}

// TestExitFailureDialogEscThenExitReopens covers decision 2 with a real
// key: Escape dismisses the dialog, and the next generic exit reopens it
// without another Quit attempt.
func TestExitFailureDialogEscThenExitReopens(t *testing.T) {
	attempts := 0
	m, q := newTestModel(t, Config{Service: &fakeService{quitFn: func() error {
		attempts++
		return errors.New("close recognizer: busy")
	}}})
	m.SetContentSize(1020, 700)
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)
	m.AllowClose()
	drainAll(t, m, q)
	tt.Frame()
	if !m.exitFailureDialogOpen || !tt.HasText("仍然退出") {
		t.Fatalf("the failure dialog did not open: %s", probeReviewState(m))
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if m.exitFailureDialogOpen {
		t.Fatal("Escape did not close the failure dialog")
	}
	if m.AllowClose() {
		t.Fatal("reopening the exit failure dialog allowed the close")
	}
	tt.Frame()
	if !m.exitFailureDialogOpen {
		t.Fatalf("the next exit did not reopen the failure dialog: %s", probeReviewState(m))
	}
	if !tt.HasText("youkai 没有退出") {
		t.Fatalf("the reopened failure dialog is incomplete: %q", tt.Texts())
	}
	if attempts != 1 {
		t.Fatalf("reopening retried cleanup: attempts %d", attempts)
	}
}

// TestExitCleanupFailureGenericExitPromptsDirty covers the held generic
// exit: after 放弃修改 it reopens the dialog instead of force-exiting.
func TestExitCleanupFailureGenericExitPromptsDirty(t *testing.T) {
	settled := 0
	m, q := newTestModel(t, Config{
		Service:       &fakeService{quitFn: func() error { return errors.New("close recognizer: busy") }},
		OnExitSettled: func() { settled++ },
	})
	m.AllowClose()
	drainAll(t, m, q)
	m.edit = &editState{
		recordID: "rec",
		seq:      1,
		original: editDraft{Level: "15"},
		draft:    editDraft{Level: "14"},
	}
	m.AllowClose()
	if m.navPrompt == nil || m.exitState != exitFailed {
		t.Fatalf("generic exit bypassed the dirty draft: %s", probeReviewState(m))
	}
	m.promptDiscardAndContinue()
	drainAll(t, m, q)
	if m.exitState != exitFailed || !m.exitFailureDialogOpen || settled != 0 {
		t.Fatalf("after 放弃修改: state %v dialog %v settled %d",
			m.exitState, m.exitFailureDialogOpen, settled)
	}
}

// TestExitCleanupFailureExitAnywayPromptsDirty covers the tray/dialog
// 仍然退出 path: it waits for the unsaved decision and exits after 放弃.
func TestExitCleanupFailureExitAnywayPromptsDirty(t *testing.T) {
	settled := 0
	m, q := newTestModel(t, Config{
		Service:       &fakeService{quitFn: func() error { return errors.New("close recognizer: busy") }},
		OnExitSettled: func() { settled++ },
	})
	m.AllowClose()
	drainAll(t, m, q)
	m.edit = &editState{
		recordID: "rec",
		seq:      1,
		original: editDraft{Level: "15"},
		draft:    editDraft{Level: "14"},
	}
	m.ExitAnyway()
	if m.navPrompt == nil || m.exitState != exitFailed || settled != 0 {
		t.Fatalf("仍然退出 bypassed the dirty draft: %s", probeReviewState(m))
	}
	m.promptDiscardAndContinue()
	drainAll(t, m, q)
	if m.exitState != exitDone || settled != 1 {
		t.Fatalf("放弃修改 did not finish the forced exit: state %v settled %d",
			m.exitState, settled)
	}
}

// TestExitCleanupFailureExitAnywayWaitsForSave covers the in-flight save
// through the real save callback: no exit may settle until the save
// finishes, and its success then continues the forced exit.
func TestExitCleanupFailureExitAnywayWaitsForSave(t *testing.T) {
	f := newExitMatrixFixture(t)
	f.dirty(t)
	release := f.blockedSave(t, nil)

	f.m.ExitAnyway()
	f.tt.Frame()
	if f.m.navPrompt == nil || f.m.exitState != exitFailed || f.settled != 0 {
		t.Fatalf("仍然退出 settled during a save: %s", probeReviewState(f.m))
	}
	release()
	drainAll(t, f.m, f.q)
	if f.m.exitState != exitDone || f.settled != 1 {
		t.Fatalf("the settled save did not allow the forced exit: state %v settled %d",
			f.m.exitState, f.settled)
	}
}

// TestExitCleanupDialogExitAnywayCannotBypassDirty covers the button: the
// dialog's 仍然退出 shows the ordinary unsaved prompt before exiting.
func TestExitCleanupDialogExitAnywayCannotBypassDirty(t *testing.T) {
	settled := 0
	m, q := newTestModel(t, Config{
		Service:       &fakeService{quitFn: func() error { return errors.New("close recognizer: busy") }},
		OnExitSettled: func() { settled++ },
	})
	m.SetContentSize(1020, 700)
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)
	m.AllowClose()
	drainAll(t, m, q)
	tt.Frame()
	m.edit = &editState{
		recordID: "rec",
		seq:      1,
		original: editDraft{Level: "15"},
		draft:    editDraft{Level: "14"},
	}
	tt.Frame()
	if err := tt.Click("仍然退出"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if m.navPrompt == nil || !tt.HasText("有未保存的修改") || settled != 0 {
		t.Fatalf("the dialog bypassed the dirty draft: %s", probeReviewState(m))
	}
	if err := tt.Click("放弃修改"); err != nil {
		t.Fatal(err)
	}
	drainAll(t, m, q)
	if m.exitState != exitDone || settled != 1 {
		t.Fatalf("放弃修改 did not finish the dialog's exit: state %v settled %d",
			m.exitState, settled)
	}
}

// TestExitFailureClassifiesSaveFromTheLatestSnapshot covers
// was-save-always: the failure classification must follow the
// authoritative snapshot, not the last delivered one.
func TestExitFailureClassifiesSaveFromTheLatestSnapshot(t *testing.T) {
	failed := endedSession("s1", 3, 2, 2)
	failed.SaveError = "disk full"
	failed.FinalSaveFailures = 1
	fake := &fakeService{
		quitFn: func() error { return errors.New("scan 1 still has an unsaved result") },
		loads:  map[string]*record.Recovery{"s1": notificationRecord(failedItems(2), record.CoverageComplete, completePositions())},
	}
	m, q := newTestModel(t, Config{Service: &snapshotService{fakeService: fake, snapshot: failed}})
	m.AllowClose()
	drainAll(t, m, q)
	if !m.exitFailureWasSave {
		t.Fatal("a save-blocked exit was classified as a non-save failure")
	}
	if got := m.LifecycleStatus().Text; got != "保存失败" {
		t.Fatalf("tray status = %q, want 保存失败", got)
	}
}

// TestExitFailureClassifiesNonSaveFromTheLatestSnapshot covers
// was-save-never: an exit without an unsaved result stays a non-save
// failure even if an earlier snapshot had no save error yet.
func TestExitFailureClassifiesNonSaveFromTheLatestSnapshot(t *testing.T) {
	fake := &fakeService{quitFn: func() error { return errors.New("close recognizer: busy") }}
	m, q := newTestModel(t, Config{Service: &snapshotService{
		fakeService: fake,
		snapshot:    endedSession("s1", 3, 2, 2),
	}})
	m.AllowClose()
	drainAll(t, m, q)
	if m.exitFailureWasSave {
		t.Fatal("a non-save exit failure was classified as a save failure")
	}
	if got := m.LifecycleStatus().Text; got != "退出失败" {
		t.Fatalf("tray status = %q, want 退出失败", got)
	}
}

// TestExitFailureClassifiesALateSaveSnapshot covers the delivery order the
// exit error can beat: the save failure arrives while the exit is already
// failed, upgrades the classification, and a successful retry returns the
// tray to idle.
func TestExitFailureClassifiesALateSaveSnapshot(t *testing.T) {
	fake := &fakeService{quitFn: func() error { return errors.New("scan 1 still has an unsaved result") }}
	m, q := newTestModel(t, Config{Service: fake})
	m.AllowClose()
	drainAll(t, m, q)
	if m.exitFailureWasSave {
		t.Fatal("the empty service snapshot classified the failure as save")
	}

	late := endedSession("s1", 3, 2, 2)
	late.SaveError = "disk full"
	late.FinalSaveFailures = 1
	m.Apply(late)
	drainAll(t, m, q)
	if !m.exitFailureWasSave {
		t.Fatal("the late save snapshot did not upgrade the exit failure")
	}
	if got := m.LifecycleStatus().Text; got != "保存失败" {
		t.Fatalf("tray status after the late save snapshot = %q", got)
	}

	retried := late
	retried.Revision = 4
	retried.SaveError = ""
	m.Apply(retried)
	drainAll(t, m, q)
	if m.exitState != exitIdle {
		t.Fatalf("the successful retry left the exit in state %v", m.exitState)
	}
	if got := m.LifecycleStatus().Text; got != "就绪" {
		t.Fatalf("tray status after the retried save = %q", got)
	}
}

// TestFailedExitDoesNotFlushPendingResult covers flush-on-failed-exit: a
// failed exit must leave the pending result to the normal settle path, not
// spend the exit budget or send a premature conservative notification.
func TestFailedExitDoesNotFlushPendingResult(t *testing.T) {
	gate := make(chan struct{})
	var mu sync.Mutex
	block := false
	recordRead := notificationRecord(failedItems(2), record.CoverageComplete, completePositions())
	svc := &fakeService{
		quitFn: func() error { return errors.New("close recognizer: busy") },
		loadFn: func(string) (*record.Recovery, error) {
			mu.Lock()
			held := block
			mu.Unlock()
			if held {
				<-gate
			}
			return recordRead, nil
		},
	}
	var got []Notification
	m := New(Config{
		Service:    svc,
		ScanSource: availableSource(),
		Notify:     func(n Notification) { got = append(got, n) },
	})
	u := &fifoUpdater{}
	m.Attach(u)
	m.Apply(openCapturing("s1", 1, 2))
	for i := 0; i < 4; i++ {
		m.Idle()
		for u.step() {
		}
	}
	mu.Lock()
	block = true
	mu.Unlock()
	m.Apply(stoppingSession("s1", 2, 2))
	m.Apply(endedSession("s1", 3, 2, 2))
	start := time.Now()
	u.Update(func() { m.AllowClose() })
	stepFIFOUntil(t, u, "the failed exit", func() bool { return m.exitState == exitFailed })
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("the failed exit waited for the result read: %v", elapsed)
	}
	if results := resultNotifications(got); len(results) != 0 {
		t.Fatalf("the failed exit flushed a result early: %+v", results)
	}

	close(gate)
	m.Idle()
	for u.step() {
	}
	if results := resultNotifications(got); len(results) != 1 {
		t.Fatalf("the normal settle path did not send the result: %+v", results)
	}
}

// TestNonSaveExitFailureCopyDoesNotClaimAnUnsavedResult covers R3-3: a
// teardown failure that did not involve the final save must not tell the
// user the result is unsaved or to retry a save that does not exist.
func TestNonSaveExitFailureCopyDoesNotClaimAnUnsavedResult(t *testing.T) {
	m, q := newTestModel(t, Config{Service: &fakeService{
		quitFn: func() error { return errors.New("close recognizer: busy") },
	}})
	m.page = pageScan
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)
	m.AllowClose()
	drainAll(t, m, q)
	tt.Frame()

	for _, bad := range []string{
		"结果还没有保存，youkai 没有退出",
		"请先重试保存",
		"重试保存",
	} {
		if tt.HasText(bad) {
			t.Fatalf("non-save exit failure shows %q: %q", bad, tt.Texts())
		}
	}
	for _, want := range []string{
		"youkai 没有退出",
		"当前没有待保存的扫描结果",
		"复制诊断信息",
	} {
		if !tt.HasText(want) {
			t.Fatalf("non-save exit failure does not show %q", want)
		}
	}
	if tt.HasText("扫描结果已保存") {
		t.Fatalf("an empty session claims a saved scan result: %q", tt.Texts())
	}

	m.exitFailureDialogOpen = false
	tt.Frame()
	if tt.HasText("请先重试保存") {
		t.Fatal("the inline exit failure still asks for a save retry")
	}
	if !tt.HasText("当前没有待保存的扫描结果") {
		t.Fatal("the inline exit failure does not state that no result is pending")
	}
	if !tt.HasText("再次退出可重新打开退出失败对话框") {
		t.Fatalf("the inline exit failure does not explain how to reopen the dialog: %q", tt.Texts())
	}
}

// TestSnapshotHasSavedResultsUsesNotificationFacts covers R5-3: the exit
// copy reuses the notification's saved-result decision, including its
// saved-to-captured fallback for a lagging snapshot.
func TestSnapshotHasSavedResultsUsesNotificationFacts(t *testing.T) {
	cases := []struct {
		name string
		snap app.Snapshot
		rec  *record.Recovery
		want bool
	}{
		{"no session", snap("", 1, false, scan.PhaseEnded, scan.Counts{}, 0, false, ""), nil, false},
		{"session without a result", snap("s1", 1, false, scan.PhaseEnded, scan.Counts{}, 0, false, ""), nil, false},
		{"saved result", snap("s1", 1, false, scan.PhaseEnded, scan.Counts{Captured: 1, Produced: 1}, 1, false, ""), nil, true},
		{"captured fallback", snap("s1", 1, false, scan.PhaseEnded, scan.Counts{Captured: 2}, 0, false, ""), nil, true},
		{"record fallback", snap("s1", 1, false, scan.PhaseEnded, scan.Counts{}, 0, false, ""), recovery(reviewRowItem(t, 1, 12)), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := snapshotHasSavedResults(tc.snap, tc.rec); got != tc.want {
				t.Fatalf("snapshotHasSavedResults = %v, want %v for %+v", got, tc.want, tc.snap)
			}
		})
	}
}

// TestExitFailureWithSessionButNoResultDoesNotClaimSavedResults covers the
// preparation-failure and stopped-before-capture case: a session identity
// alone is not a saved scan result.
func TestExitFailureWithSessionButNoResultDoesNotClaimSavedResults(t *testing.T) {
	m, q := newTestModel(t, Config{Service: &fakeService{
		quitFn: func() error { return errors.New("close recognizer: busy") },
	}})
	m.snap = snap("s1", 1, false, scan.PhaseEnded, scan.Counts{}, 0, false, "")
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)
	m.AllowClose()
	drainAll(t, m, q)
	tt.Frame()

	if tt.HasText("扫描结果已保存") {
		t.Fatalf("a session without results claims a saved scan result: %q", tt.Texts())
	}
	if !tt.HasText("当前没有待保存的扫描结果") {
		t.Fatalf("the failure does not state that no result is pending: %q", tt.Texts())
	}
	if got := m.LifecycleStatus().TrayMenu().Status; got != "youkai · 退出清理未完成" {
		t.Fatalf("tray status for a session without results = %q", got)
	}
}

// TestExitDialogs checks the running and failed wrap-up dialogs.
func TestExitDialogs(t *testing.T) {
	release := make(chan struct{})
	svc := &fakeService{quitFn: func() error {
		<-release
		return nil
	}}
	m, q := newTestModel(t, Config{Service: svc})
	m.page = pageScan
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)
	if m.AllowClose() {
		t.Fatal("the first close was allowed before the wrap-up")
	}
	tt.Frame()
	if !tt.HasText("正在退出：保存结果……") {
		t.Fatalf("the running-exit dialog is missing: %q", tt.Texts())
	}
	close(release)
	drainAll(t, m, q)

	failed := &fakeService{quitFn: func() error { return errors.New("disk full") }}
	m2, q2 := newTestModel(t, Config{Service: failed})
	m2.page = pageScan
	tt2 := ui.NewTester(m2.View, 1020, 700)
	settle(t, m2, q2, tt2)
	if m2.AllowClose() {
		t.Fatal("the failed close was allowed")
	}
	drainAll(t, m2, q2)
	// The failure stays visible with the unsaved result, so the retry can
	// actually retry the save.
	m2.Apply(snap("s1", 2, false, scan.PhaseEnded,
		scan.Counts{Captured: 1, Produced: 1}, 1, false, "磁盘已满"))
	drainAll(t, m2, q2)
	tt2.Frame()
	for _, want := range []string{
		"结果还没有保存，youkai 没有退出",
		"重试保存",
		"复制诊断信息",
	} {
		if !tt2.HasText(want) {
			t.Fatalf("the exit-failure dialog does not show %q: %q", want, tt2.Texts())
		}
	}
	if err := tt2.Click("重试保存"); err != nil {
		t.Fatal(err)
	}
	drainAll(t, m2, q2)
	tt2.Frame()
	if failed.retries != 1 {
		t.Fatalf("retries = %d, want 1", failed.retries)
	}
	if m2.exitFailureDialogOpen {
		t.Fatal("the retry left the dialog open")
	}
}
