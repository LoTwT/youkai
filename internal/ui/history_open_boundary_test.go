package ui

import (
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/record"
	"github.com/LoTwT/youkai/internal/scan"
)

// The accepted final snapshot is the last event. The first read must not
// install an older generation or let row export consume its frozen data.
func TestHistoryPendingLiveOpenCatchesFinalSnapshot(t *testing.T) {
	for _, tc := range []struct {
		name                string
		growth, placeholder bool
	}{
		{"growth", true, false}, {"same-count-final", false, false},
		{"growth-from-placeholder", true, true}, {"same-count-from-placeholder", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) { assertPendingLiveFinal(t, tc.growth, tc.placeholder) })
	}
}

func assertPendingLiveFinal(t *testing.T, growth, placeholder bool) {
	t.Helper()
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	old := recovery(reviewRowItem(t, 1, 12))
	old.Checkpoint.Session.Termination = nil
	old.Checkpoint.Session.Coverage.Status = record.CoveragePartial
	items := []record.ItemRecord{reviewRowItem(t, 1, 12)}
	if growth {
		items = append(items, reviewRowItem(t, 2, 12))
	}
	latest := recovery(items...)
	latest.Checkpoint.SavedRevision = 2
	latest.Checkpoint.Session.Coverage.Status = record.CoverageComplete
	var calls atomic.Int32
	svc := &fakeService{history: historyTableRecords(1), loadFn: func(string) (*record.Recovery, error) {
		if calls.Add(1) == 1 {
			close(started)
			<-release
			return old, nil
		}
		return latest, nil
	}}
	m, q := newTestModel(t, Config{Service: svc, Notify: func(Notification) {}, DirectExportSaver: &fakeDirectExportSaver{}})
	drainAll(t, m, q)
	m.appliedSession = scan.ID("r01")
	m.appliedRevision = 1
	m.snap = openCapturing("r01", 1, 1)
	m.prevBusy = true
	m.notifyPrev, m.notifyPrevKnown = m.snap, true
	if placeholder {
		m.showRecord("r01", true)
	}
	m.page = pageHistory
	m.openHistoryExport(m.history[0])
	<-started
	m.Apply(endedSession("r01", 2, len(items), len(items)))
	if m.displayPage() != pageHistory || m.historyOpen == nil || m.exportDialog.open {
		t.Fatal("final event navigated before the pending record was prepared")
	}
	unblock()
	drainAll(t, m, q)
	if !m.recordIsReady("r01") || len(m.rec.Rows) != len(items) || m.liveSavedShown != len(items) || m.recData.Checkpoint.SavedRevision != 2 || m.recData.Checkpoint.Session.Termination == nil || m.recData.Checkpoint.Session.Termination.Kind != record.TerminationCompleted {
		t.Fatalf("final snapshot lost: rows=%d revision=%d termination=%v shown=%d reads=%d", len(m.rec.Rows), m.recData.Checkpoint.SavedRevision, m.recData.Checkpoint.Session.Termination, m.liveSavedShown, calls.Load())
	}
	if !m.exportDialog.open || m.exportData == nil {
		t.Fatal("row export did not consume the final prepared generation")
	}
	raw, err := m.exportData.JSON(time.Now(), "test")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Session struct {
			SavedRevision int64
			CapturedCount int64
			Termination   record.Termination
		}
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Session.SavedRevision != 2 || doc.Session.CapturedCount != int64(len(items)) || doc.Session.Termination.Kind != record.TerminationCompleted {
		t.Fatalf("stale export: %s", raw)
	}
}

func TestNotificationSupersedesPendingHistoryOpen(t *testing.T) {
	m, q, _, svc, gates := gatedHistoryModel(t)
	m.openHistoryExport(m.history[0])
	<-gates["r01"].started
	m.OpenNotification("r02")
	if m.historyOpen == nil || m.historyOpen.id != "r02" {
		t.Fatal("notification did not take navigation ownership")
	}
	<-gates["r02"].started
	gates["r02"].unblock()
	for m.historyOpen != nil {
		waitForHistoryUpdate(t, q)
	}
	m.recordTab = recordTabItems
	gates["r01"].unblock()
	drainAll(t, m, q)
	if !m.recordIsReady("r02") || recordLoadCount(svc, "r02") != 1 || m.displayPage() != pageResults || m.recordTab != recordTabItems || m.exportDialog.open {
		t.Fatalf("notification lost: record=%s A=%d B=%d page=%v", m.rec.ID, recordLoadCount(svc, "r01"), recordLoadCount(svc, "r02"), m.displayPage())
	}
}

func TestHistoryPendingLiveCatchupStaysCancellable(t *testing.T) {
	for _, failure := range []bool{false, true} {
		first, second := &historyReadGate{started: make(chan struct{}, 1), release: make(chan struct{})}, &historyReadGate{started: make(chan struct{}, 1), release: make(chan struct{})}
		t.Cleanup(first.unblock)
		t.Cleanup(second.unblock)
		var calls atomic.Int32
		svc := &fakeService{history: historyTableRecords(1), loadFn: func(string) (*record.Recovery, error) {
			gate := first
			if calls.Add(1) > 1 {
				gate = second
			}
			gate.started <- struct{}{}
			<-gate.release
			if gate == second && failure {
				return nil, errors.New("catch-up read failed")
			}
			return testRecord(), nil
		}}
		m, q := newTestModel(t, Config{Service: svc, DirectExportSaver: &fakeDirectExportSaver{}})
		drainAll(t, m, q)
		m.appliedSession = scan.ID("r01")
		m.snap = openCapturing("r01", 1, 1)
		m.page = pageHistory
		m.openHistoryExport(m.history[0])
		<-first.started
		m.Apply(openCapturing("r01", 2, 2))
		first.unblock()
		waitForHistoryUpdate(t, q)
		<-second.started
		if m.displayPage() != pageHistory || m.historyOpen == nil || m.rec != nil || m.exportDialog.open {
			t.Fatal("catch-up exposed stale details")
		}
		if !failure {
			m.cancelHistoryOpen()
		}
		second.unblock()
		drainAll(t, m, q)
		if calls.Load() != 2 || m.displayPage() != pageHistory || m.rec != nil || m.historyOpen != nil || m.exportDialog.open || len(m.recordReads) != 0 {
			t.Fatal("catch-up cancellation/failure installed data or leaked a read")
		}
		if failure && m.historyOpenError == "" {
			t.Fatal("catch-up error was not retryable in the list")
		}
	}
}

func TestNotificationBusyTargetCancelsPendingHistoryOpen(t *testing.T) {
	m, q, _, svc, gates := gatedHistoryModel(t)
	m.appliedSession = scan.ID("r02")
	m.snap = openCapturing("r02", 1, 1)
	m.openRecord("r01")
	<-gates["r01"].started
	m.OpenNotification("r02")
	if m.historyOpen != nil || m.displayPage() != pageScan {
		t.Fatal("busy notification did not supersede the list intent")
	}
	gates["r01"].unblock()
	drainAll(t, m, q)
	if m.rec != nil || m.displayPage() != pageScan || recordLoadCount(svc, "r02") != 0 {
		t.Fatal("late list read overrode busy notification routing")
	}
}
