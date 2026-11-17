package ui

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/LoTwT/youkai/internal/record"
	"github.com/LoTwT/youkai/internal/scan"
	"github.com/egoist/mygo/ui"
)

type historyReadGate struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (g *historyReadGate) unblock() { g.once.Do(func() { close(g.release) }) }

func gatedHistoryModel(t *testing.T) (*Model, *queueUpdater, *ui.Tester, *fakeService, map[string]*historyReadGate) {
	t.Helper()
	gates := map[string]*historyReadGate{}
	for _, id := range []string{"r01", "r02"} {
		g := &historyReadGate{started: make(chan struct{}, 4), release: make(chan struct{})}
		gates[id] = g
		t.Cleanup(g.unblock)
	}
	svc := &fakeService{history: historyTableRecords(3), loadFn: func(id string) (*record.Recovery, error) {
		g := gates[id]
		g.started <- struct{}{}
		<-g.release
		return testRecord(), nil
	}}
	m, q := newTestModel(t, Config{Service: svc, DirectExportSaver: &fakeDirectExportSaver{}})
	drainAll(t, m, q)
	m.page = pageHistory
	tt := ui.NewTester(m.View, 1180, 760)
	return m, q, tt, svc, gates
}

func TestHistoryOpenBlocksOtherActivationAndCancels(t *testing.T) {
	m, q, tt, svc, gates := gatedHistoryModel(t)
	m.historyRow = 1
	m.openHistoryRow(1)
	<-gates["r02"].started
	tt.Frame()
	if !tt.HasText("正在读取记录…") || !tt.HasText(historyTimeText(m.history[1])) {
		t.Fatalf("loading did not retain the table: %q", tt.Texts())
	}
	m.openRecord("r02")
	m.openHistoryRow(0)
	m.openHistoryExport(m.history[0])
	m.openHistoryMenu("r01")
	tt.Key(0, ui.KeyEnter)
	tt.Key(ui.Shift, ui.KeyF10)
	if recordLoadCount(svc, "r02") != 1 || recordLoadCount(svc, "r01") != 0 || m.historyMenuOpen || m.historyRow != 1 {
		t.Fatalf("loading activation: A=%d B=%d menu=%v row=%d intent=%+v", recordLoadCount(svc, "r01"), recordLoadCount(svc, "r02"), m.historyMenuOpen, m.historyRow, m.historyOpen)
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if m.historyOpen != nil || m.displayPage() != pageHistory || m.historyRow != 1 {
		t.Fatal("Escape did not cancel in place")
	}
	gates["r02"].unblock()
	drainAll(t, m, q)
	if m.rec != nil || len(m.recordReads) != 0 || m.displayPage() != pageHistory {
		t.Fatal("cancelled read committed or was not cleaned up")
	}
}

func TestHistoryOpenCancelledResponsesNeverTakeNavigation(t *testing.T) {
	for _, action := range []string{"cancel", "leave-return", "exit"} {
		t.Run(action, func(t *testing.T) {
			m, q, tt, _, gates := gatedHistoryModel(t)
			m.openHistoryExport(m.history[0])
			<-gates["r01"].started
			tt.Frame()
			switch action {
			case "cancel":
				if err := tt.Click("取消打开"); err != nil {
					t.Fatal(err)
				}
			case "leave-return":
				if err := tt.Click("设置"); err != nil {
					t.Fatal(err)
				}
				if m.displayPage() != pageSettings {
					t.Fatal("sidebar was blocked")
				}
				m.navigatePage(pageHistory)
			case "exit":
				m.requestExit(false)
			}
			if m.historyOpen != nil {
				t.Fatal("navigation still owns the cancelled intent")
			}
			if action != "exit" {
				m.openHistoryRow(1)
				<-gates["r02"].started
				gates["r02"].unblock()
				for m.historyOpen != nil {
					waitForHistoryUpdate(t, q)
				}
				if !m.recordIsReady("r02") || m.displayPage() != pageResults {
					t.Fatal("new intent did not commit")
				}
			}
			gates["r01"].unblock()
			drainAll(t, m, q)
			if m.exportDialog.open || m.historyOpen != nil || len(m.recordReads) != 0 {
				t.Fatal("stale response retained an intent, dialog or read")
			}
			if action == "exit" {
				if m.rec != nil {
					t.Fatal("exited model installed the late record")
				}
			} else if !m.recordIsReady("r02") {
				t.Fatal("late A replaced B")
			}
		})
	}
}

func TestHistoryOpenFailurePreservesWorkspaceAndRetries(t *testing.T) {
	svc := &fakeService{history: historyTableRecords(2), loadErr: map[string]error{"r01": errors.New("temporary read failure")}, loads: map[string]*record.Recovery{"r01": testRecord()}}
	m, q := newTestModel(t, Config{Service: svc})
	drainAll(t, m, q)
	m.showRecord("old", false)
	m.applyLoadedRecord("old", testRecord(), false)
	old := m.rec
	m.page = pageHistory
	m.historyRow = 1
	m.openRecord("r01")
	drainAll(t, m, q)
	tt := ui.NewTester(m.View, 1180, 760)
	if m.displayPage() != pageHistory || m.rec != old || m.histErr != "" || m.history[0].Unreadable || m.historyRow != 1 || !tt.HasText("重试打开") {
		t.Fatalf("failed open destroyed list/workspace: %q", tt.Texts())
	}
	svc.mu.Lock()
	delete(svc.loadErr, "r01")
	svc.mu.Unlock()
	if err := tt.Click("重试打开"); err != nil {
		t.Fatal(err)
	}
	drainAll(t, m, q)
	if !m.recordIsReady("r01") || m.historyOpenError != "" || recordLoadCount(svc, "r01") != 2 {
		t.Fatal("retry did not recover")
	}
}

func TestHistoryOpenZeroAndExportPreparationError(t *testing.T) {
	for _, invalidExport := range []bool{false, true} {
		rec := recovery()
		if invalidExport {
			rec.Checkpoint.Items = append(rec.Checkpoint.Items, reviewRowItem(t, 1, 12))
			rec.Checkpoint.Session.Game = "unsupported"
		}
		svc := &fakeService{history: historyTableRecords(1), loads: map[string]*record.Recovery{"r01": rec}}
		m, q := newTestModel(t, Config{Service: svc, DirectExportSaver: &fakeDirectExportSaver{}})
		drainAll(t, m, q)
		m.page = pageHistory
		m.openHistoryExport(m.history[0])
		drainAll(t, m, q)
		if !m.recordIsReady("r01") || m.displayPage() != pageResults || m.historyOpen != nil || !m.exportDialog.open {
			t.Fatal("prepared record did not enter with its export intent")
		}
		if invalidExport && (m.exportBuildErr == "" || m.exportData != nil) {
			t.Fatal("invalid export was not distinguished from a read error")
		}
		if !invalidExport && len(m.rec.Rows) != 0 {
			t.Fatal("zero-item record changed")
		}
		m.exportDialog.open = false
		m.page = pageHistory
		m.openRecord("r01")
		if m.historyOpen != nil || recordLoadCount(svc, "r01") != 1 || m.exportDialog.open {
			t.Fatal("ready reuse reloaded or repeated the export")
		}
	}
}

func TestHistoryOpenReadDoneSurvivesCancellation(t *testing.T) {
	m, q, _, _, gates := gatedHistoryModel(t)
	var got []Notification
	m.cfg.Notify = func(n Notification) { got = append(got, n) }
	m.openRecord("r01")
	<-gates["r01"].started
	version := -m.historyOpen.token
	load := m.recordReads[version]
	m.sessionNotificationState(scan.ID("r01")).pending = &pendingResult{snap: endedSession("r01", 2, 4, 4), loadVersion: version, load: load}
	m.cancelHistoryOpen()
	gates["r01"].unblock()
	<-load.done
	if rec, err := load.result(); err != nil || rec == nil {
		t.Fatal("cancelled read did not publish its result")
	}
	m.Idle()
	if len(m.recordReads) != 1 {
		t.Fatal("read was cleaned outside the UI queue")
	}
	// Exit can consume done before navigation/preparation's queued commit.
	m.flushResultsBeforeExit()
	if len(resultNotifications(got)) != 1 {
		t.Fatal("cancelled navigation prevented the exit notification flush")
	}
	drainAll(t, m, q)
	if len(m.recordReads) != 0 || m.rec != nil || len(resultNotifications(got)) != 1 {
		t.Fatal("cancelled read was installed or leaked")
	}
}

func TestHistoryOpenCancellationKeepsOldWorkspaceRefresh(t *testing.T) {
	m, q, _, _, gates := gatedHistoryModel(t)
	m.showRecord("r02", false)
	m.applyLoadedRecord("r02", testRecord(), false)
	old := m.rec
	m.reloadRecord("r02", false)
	<-gates["r02"].started
	version := m.recLoad
	m.openRecord("r01")
	<-gates["r01"].started
	if m.rec != old || m.recLoad != version || !m.loading {
		t.Fatal("pending navigation replaced or invalidated the old refresh")
	}
	m.cancelHistoryOpen()
	gates["r02"].unblock()
	gates["r01"].unblock()
	drainAll(t, m, q)
	if !m.recordIsReady("r02") || m.displayPage() != pageHistory {
		t.Fatal("cancelled navigation lost the old workspace's refresh")
	}
}

func TestHistoryOpenLiveProgressDoesNotRestartRead(t *testing.T) {
	m, q, _, svc, gates := gatedHistoryModel(t)
	m.appliedSession = scan.ID("r01")
	m.snap = openCapturing("r01", 1, 1)
	m.openRecord("r01")
	<-gates["r01"].started
	// Ordinary progress at the same saved checkpoint does not restart a
	// pending read. Only saved growth/finalization requires catch-up.
	m.Apply(openCapturing("r01", 2, 1))
	gates["r01"].unblock()
	drainAll(t, m, q)
	if !m.recordIsReady("r01") || !m.rec.Live || m.liveSavedShown != 1 {
		t.Fatal("prepared live read lost its saved generation")
	}
	if recordLoadCount(svc, "r01") != 1 {
		t.Fatal("unchanged checkpoint caused an unnecessary reread")
	}
}

func TestHistoryReadyReuseKeepsDirtyDraftAndPlace(t *testing.T) {
	svc := &fakeService{history: historyTableRecords(1), loads: map[string]*record.Recovery{"r01": recovery(reviewRowItem(t, 1, 12))}}
	m, q := newTestModel(t, Config{Service: svc})
	drainAll(t, m, q)
	m.page = pageHistory
	m.openRecord("r01")
	drainAll(t, m, q)
	showRecordItems(m)
	m.startEdit()
	m.edit.draft.Level = "13"
	m.page = pageHistory
	m.openRecord("r01")
	if m.edit == nil || !strings.Contains(m.edit.draft.Level, "13") || m.historyOpen != nil || m.recordTab != recordTabItems || recordLoadCount(svc, "r01") != 1 {
		t.Fatal("ready reuse lost the tab/draft or reloaded")
	}
}
