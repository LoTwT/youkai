package ui

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/app"
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
