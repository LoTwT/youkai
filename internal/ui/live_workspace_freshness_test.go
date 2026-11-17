package ui

import (
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/record"
)

// Use Apply and actual navigation throughout: an older workspace refresh
// and a list read coexist, finalization is observed, then the list open is
// cancelled. Its suppressed refresh must not be forgotten on explicit return.
func TestLiveWorkspaceRefreshSurvivesCancelledHistoryOpen(t *testing.T) {
	assertLiveWorkspaceRefreshAfterOpenExit(t, "cancel", true)
}

func TestLiveWorkspaceRefreshSurvivesOtherOpenExits(t *testing.T) {
	for _, tc := range []struct {
		name, exit string
		growth     bool
	}{
		{"cancel-same-count", "cancel", false}, {"leave-growth", "leave", true},
		{"leave-same-count", "leave", false}, {"failure-growth", "failure", true},
		{"failure-same-count", "failure", false},
	} {
		t.Run(tc.name, func(t *testing.T) { assertLiveWorkspaceRefreshAfterOpenExit(t, tc.exit, tc.growth) })
	}
}

func assertLiveWorkspaceRefreshAfterOpenExit(t *testing.T, exit string, growth bool) {
	t.Helper()
	workspace := &historyReadGate{started: make(chan struct{}, 1), release: make(chan struct{})}
	list := &historyReadGate{started: make(chan struct{}, 1), release: make(chan struct{})}
	t.Cleanup(workspace.unblock)
	t.Cleanup(list.unblock)
	old := recovery(reviewRowItem(t, 1, 12))
	old.Checkpoint.Session.Termination = nil
	items := []record.ItemRecord{reviewRowItem(t, 1, 12)}
	if growth {
		items = append(items, reviewRowItem(t, 2, 12))
	}
	latest := recovery(items...)
	latest.Checkpoint.SavedRevision = 2
	var reads atomic.Int32
	svc := &fakeService{history: historyTableRecords(1), loadFn: func(string) (*record.Recovery, error) {
		switch reads.Add(1) {
		case 1:
			workspace.started <- struct{}{}
			<-workspace.release
			return old, nil
		case 2:
			list.started <- struct{}{}
			<-list.release
			if exit == "failure" {
				return nil, errors.New("pending read failed")
			}
			return old, nil
		default:
			return latest, nil
		}
	}}
	m, q := newTestModel(t, Config{Service: svc, Notify: func(Notification) {}, DirectExportSaver: &fakeDirectExportSaver{}})
	drainAll(t, m, q)
	m.Apply(openCapturing("r01", 1, 1))
	<-workspace.started
	m.navigatePage(pageHistory)
	m.openHistoryExport(m.history[0])
	<-list.started
	m.Apply(endedSession("r01", 2, len(items), len(items)))
	switch exit {
	case "cancel":
		m.cancelHistoryOpen()
	case "leave":
		m.navigatePage(pageSettings)
	}
	workspace.unblock()
	list.unblock()
	drainAll(t, m, q)
	wantPage := pageHistory
	if exit == "leave" {
		wantPage = pageSettings
	}
	if m.displayPage() != wantPage || m.historyOpen != nil || m.exportDialog.open {
		t.Fatal("revoked list intent navigated or consumed its export")
	}
	if exit == "failure" && m.historyOpenError == "" {
		t.Fatal("pending failure disappeared")
	}
	// Exercise the records destination as a user returning from Settings.
	m.navigatePage(pageSettings)
	m.showRecordsDestination()
	drainAll(t, m, q)
	raw, err := m.exportData.JSON(time.Now(), "test")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Session struct{ SavedRevision, CapturedCount int64 }
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	t.Logf("page=%v rows=%d revision=%d final=%v loading=%v saved=%d shown=%d reads=%d exportRevision=%d exportCaptured=%d exportBlock=%q", m.displayPage(), len(m.rec.Rows), m.recData.Checkpoint.SavedRevision, m.recData.Checkpoint.Session.Termination, m.loading, m.snap.SavedCount, m.liveSavedShown, reads.Load(), doc.Session.SavedRevision, doc.Session.CapturedCount, m.exportBlockedReason())
	if !m.recordIsReady("r01") || len(m.rec.Rows) != len(items) || m.recData.Checkpoint.SavedRevision != 2 || m.recData.Checkpoint.Session.Termination == nil || doc.Session.SavedRevision != 2 || doc.Session.CapturedCount != int64(len(items)) || m.exportBlockedReason() != "" {
		t.Fatal("cancelled list intent left the real live workspace and enabled export on the old generation after the accepted final snapshot")
	}
}

func TestLiveWorkspaceRejectsOlderReadAfterFinalRefresh(t *testing.T) {
	workspace := &historyReadGate{started: make(chan struct{}, 1), release: make(chan struct{})}
	list := &historyReadGate{started: make(chan struct{}, 1), release: make(chan struct{})}
	t.Cleanup(workspace.unblock)
	t.Cleanup(list.unblock)
	old := recovery(reviewRowItem(t, 1, 12))
	old.Checkpoint.Session.Termination = nil
	latest := recovery(reviewRowItem(t, 1, 12), reviewRowItem(t, 2, 12))
	latest.Checkpoint.SavedRevision = 2
	var reads atomic.Int32
	svc := &fakeService{history: historyTableRecords(1), loadFn: func(string) (*record.Recovery, error) {
		switch reads.Add(1) {
		case 1:
			workspace.started <- struct{}{}
			<-workspace.release
			return old, nil
		case 2:
			list.started <- struct{}{}
			<-list.release
			return old, nil
		default:
			return latest, nil
		}
	}}
	m, q := newTestModel(t, Config{Service: svc, DirectExportSaver: &fakeDirectExportSaver{}})
	drainAll(t, m, q)
	m.Apply(openCapturing("r01", 1, 1))
	<-workspace.started
	m.navigatePage(pageHistory)
	m.openHistoryExport(m.history[0])
	<-list.started
	m.Apply(endedSession("r01", 2, 2, 2))
	m.cancelHistoryOpen()
	for !m.recordIsReady("r01") {
		waitForHistoryUpdate(t, q)
	}
	if m.recData.Checkpoint.SavedRevision != 2 {
		t.Fatal("workspace became ready on an older checkpoint")
	}
	m.recordTab = recordTabItems
	workspace.unblock()
	list.unblock()
	drainAll(t, m, q)
	if !m.recordIsReady("r01") || len(m.rec.Rows) != 2 || m.recData.Checkpoint.SavedRevision != 2 || m.recData.Checkpoint.Session.Termination == nil || m.recordTab != recordTabItems || m.displayPage() != pageHistory || m.exportDialog.open || len(m.recordReads) != 0 {
		t.Fatal("late old workspace/list read replaced the final checkpoint or navigation")
	}
}

func TestLiveWorkspaceFreshnessSurvivesDirtyFinalSnapshot(t *testing.T) {
	old := recovery(reviewRowItem(t, 1, 12))
	old.Checkpoint.Session.Termination = nil
	latest := recovery(reviewRowItem(t, 1, 12))
	latest.Checkpoint.SavedRevision = 2
	var reads atomic.Int32
	svc := &fakeService{loadFn: func(string) (*record.Recovery, error) {
		if reads.Add(1) == 1 {
			return old, nil
		}
		return latest, nil
	}}
	m, q := newTestModel(t, Config{Service: svc, DirectExportSaver: &fakeDirectExportSaver{}})
	drainAll(t, m, q)
	m.Apply(openCapturing("r01", 1, 1))
	drainAll(t, m, q)
	showRecordItems(m)
	m.startEdit()
	if m.edit == nil {
		t.Fatal("editor did not open")
	}
	m.edit.draft.Level = "13"
	m.Apply(endedSession("r01", 2, 1, 1))
	if m.recordIsReady("r01") || m.exportBlockedReasonWithDraft(false) == "" || m.edit.draft.Level != "13" {
		t.Fatal("stale live data became ready/exportable or lost its draft")
	}
	m.discardEdit()
	drainAll(t, m, q)
	if !m.recordIsReady("r01") || m.recData.Checkpoint.SavedRevision != 2 || m.recData.Checkpoint.Session.Termination == nil || m.exportBlockedReason() != "" {
		t.Fatal("same-count final checkpoint was not refreshed after the draft settled")
	}
}
