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

// slotHistoryLabel returns the picker label of one loaded history record.
func slotHistoryLabel(t *testing.T, m *Model, id string) string {
	t.Helper()
	for _, entry := range m.history {
		if entry.ID == id {
			return entry.Label
		}
	}
	t.Fatalf("history record %s is not loaded: %+v", id, m.history)
	return ""
}

// slotLoadCount counts the loads of one record.
func slotLoadCount(svc *fakeService, id string) int {
	svc.mu.Lock()
	defer svc.mu.Unlock()
	count := 0
	for _, loaded := range svc.loadLog {
		if loaded == id {
			count++
		}
	}
	return count
}

// TestOpenHistoryKeepsSessionAndShowLiveRestoresIt checks the two result
// destinations: opening history leaves the session slot intact, and
// returning to the live session restores its own filters, selection,
// scroll and export snapshot without another load.
func TestOpenHistoryKeepsSessionAndShowLiveRestoresIt(t *testing.T) {
	svc := &fakeService{
		list: []record.RecordInfo{{
			ID:      "old",
			SavedAt: time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC),
		}},
		loads: map[string]*record.Recovery{
			"s1": recovery(
				item(1, record.StatusProduced, "", "", "一号"),
				item(2, record.StatusProduced, "", "", "二号"),
			),
			"old": recovery(
				item(1, record.StatusProduced, "", "", "旧一"),
				item(2, record.StatusProduced, "", "", "旧二"),
			),
		},
	}
	m, q := newTestModel(t, Config{Service: svc})
	drainAll(t, m, q)
	m.Apply(snap("s1", 1, true, scan.PhaseCapturing, scan.Counts{Captured: 2, Produced: 2}, 1, true, ""))
	drainAll(t, m, q)
	if m.rec == nil || m.rec.ID != "s1" || !m.rec.Live {
		t.Fatalf("the live session did not load: %+v", m.rec)
	}

	sessionExport := &app.UsableExport{Summary: app.ExportSummary{Included: 2, Captured: 2}}
	m.filter = resultFilterAll
	m.search = "crit"
	m.setFilter = "31100"
	m.slotFilter = "4"
	m.selected = 1
	m.scroll = 1
	m.exportData = sessionExport
	m.exportNotice = "session export"

	m.histChoice = slotHistoryLabel(t, m, "old")
	m.OpenHistory()
	drainAll(t, m, q)
	if m.activeSlot != resultSlotHistory || m.rec == nil || m.rec.ID != "old" || m.rec.Live {
		t.Fatalf("history did not become the active destination: %+v", m.rec)
	}
	if m.sessionSlot.recordID != "s1" || m.sessionSlot.state.rec == nil || m.sessionSlot.state.rec.ID != "s1" {
		t.Fatalf("opening history cleared the session slot: %+v", m.sessionSlot)
	}
	if m.sessionSlot.state.recData == nil || m.sessionSlot.state.selected != 1 || m.sessionSlot.state.scroll != 1 ||
		m.sessionSlot.state.filter != resultFilterAll || m.sessionSlot.state.search != "crit" ||
		m.sessionSlot.state.setFilter != "31100" || m.sessionSlot.state.slotFilter != "4" ||
		m.sessionSlot.state.exportData != sessionExport {
		t.Fatalf("the session slot lost its view state: %+v", m.sessionSlot.state)
	}

	historyExport := &app.UsableExport{Summary: app.ExportSummary{Included: 1, Captured: 2}}
	m.filter = resultFilterUnexportable
	m.search = "hist"
	m.setFilter = "99999"
	m.slotFilter = "2"
	m.selected = 1
	m.scroll = 1
	m.exportData = historyExport

	loadsBefore := slotLoadCount(svc, "s1")
	m.ShowLive()
	drainAll(t, m, q)
	if m.activeSlot != resultSlotSession || m.rec == nil || m.rec.ID != "s1" || !m.rec.Live {
		t.Fatalf("ShowLive did not return to the session: %+v", m.rec)
	}
	if m.filter != resultFilterAll || m.search != "crit" || m.setFilter != "31100" ||
		m.slotFilter != "4" || m.selected != 1 || m.scroll != 1 || m.exportData != sessionExport {
		t.Fatalf("the session state was not restored: filter %v search %q set %q slot %q selected %d scroll %d",
			m.filter, m.search, m.setFilter, m.slotFilter, m.selected, m.scroll)
	}
	if slotLoadCount(svc, "s1") != loadsBefore {
		t.Fatal("ShowLive reloaded the cached session")
	}
	if m.historySlot.state.rec == nil || m.historySlot.state.filter != resultFilterUnexportable ||
		m.historySlot.state.search != "hist" || m.historySlot.state.selected != 1 ||
		m.historySlot.state.exportData != historyExport {
		t.Fatalf("leaving history lost its state: %+v", m.historySlot.state)
	}

	m.OpenHistory() // the picker still names the same record
	drainAll(t, m, q)
	if m.activeSlot != resultSlotHistory || m.rec.ID != "old" {
		t.Fatalf("reopening history did not restore the slot: %+v", m.rec)
	}
	if m.filter != resultFilterUnexportable || m.search != "hist" || m.setFilter != "99999" ||
		m.slotFilter != "2" || m.selected != 1 || m.scroll != 1 || m.exportData != historyExport {
		t.Fatalf("the history state was not restored: filter %v search %q selected %d scroll %d",
			m.filter, m.search, m.selected, m.scroll)
	}
	if slotLoadCount(svc, "old") != 1 {
		t.Fatalf("revisiting history reloaded it: %v", svc.loadLog)
	}
}

// TestResultSlotDraftMustSettleBeforeSwitch checks that a slot switch waits
// for the active draft and that only the active slot can be dirty.
func TestResultSlotDraftMustSettleBeforeSwitch(t *testing.T) {
	newDirtyModel := func(t *testing.T) (*Model, *queueUpdater, *fakeService) {
		t.Helper()
		svc := &fakeService{
			list: []record.RecordInfo{{
				ID:      "old",
				SavedAt: time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC),
			}},
			loads: map[string]*record.Recovery{
				"s1":  recovery(reviewRowItem(t, 1, 12)),
				"old": recovery(reviewRowItem(t, 1, 12)),
			},
		}
		installReviewSave(svc)
		m, q := newTestModel(t, Config{Service: svc})
		drainAll(t, m, q)
		m.Apply(snap("s1", 1, true, scan.PhaseCapturing, scan.Counts{Captured: 1, Produced: 1}, 1, true, ""))
		drainAll(t, m, q)
		m.startEdit()
		m.edit.draft.Level = "13"
		m.histChoice = slotHistoryLabel(t, m, "old")
		return m, q, svc
	}

	t.Run("stay", func(t *testing.T) {
		m, q, svc := newDirtyModel(t)
		m.OpenHistory()
		if m.navPrompt == nil || m.navPrompt.kind != navOpenHistory {
			t.Fatalf("a dirty switch did not prompt: %+v", m.navPrompt)
		}
		if m.activeSlot != resultSlotSession || m.historySlot.recordID != "" {
			t.Fatal("the destination switched before the draft settled")
		}
		if slotLoadCount(svc, "old") != 0 {
			t.Fatal("the history record loaded behind the prompt")
		}
		m.promptStay()
		drainAll(t, m, q)
		if m.navPrompt != nil || m.edit == nil || m.edit.draft.Level != "13" {
			t.Fatalf("staying lost the draft: prompt %+v edit %+v", m.navPrompt, m.edit)
		}
		if m.activeSlot != resultSlotSession {
			t.Fatal("staying moved the destination")
		}
	})

	t.Run("discard", func(t *testing.T) {
		m, q, _ := newDirtyModel(t)
		m.OpenHistory()
		m.promptDiscardAndContinue()
		drainAll(t, m, q)
		if m.edit != nil || m.navPrompt != nil || m.activeSlot != resultSlotHistory {
			t.Fatalf("discard did not switch: edit %v prompt %+v slot %v",
				m.edit, m.navPrompt, m.activeSlot)
		}
		if m.sessionSlot.state.rec == nil || m.sessionSlot.state.rec.ID != "s1" {
			t.Fatal("discarding cleared the session slot")
		}

		// The history slot may now hold its own draft; the session slot is
		// not edited at the same time.
		m.startEdit()
		m.edit.draft.Level = "14"
		m.ShowLive()
		if m.navPrompt == nil || m.navPrompt.kind != navShowLive {
			t.Fatalf("a dirty return did not prompt: %+v", m.navPrompt)
		}
		if m.activeSlot != resultSlotHistory {
			t.Fatal("the dirty history slot was left behind without a prompt")
		}
		m.promptDiscardAndContinue()
		drainAll(t, m, q)
		if m.activeSlot != resultSlotSession || m.edit != nil {
			t.Fatalf("discarding did not return to the session: slot %v edit %v", m.activeSlot, m.edit)
		}
	})

	t.Run("save", func(t *testing.T) {
		m, q, svc := newDirtyModel(t)
		m.OpenHistory()
		m.promptSaveAndContinue()
		drainAll(t, m, q)
		if m.edit != nil || m.navPrompt != nil || m.activeSlot != resultSlotHistory {
			t.Fatalf("save did not switch: edit %v prompt %+v slot %v",
				m.edit, m.navPrompt, m.activeSlot)
		}
		if svc.saveCount() != 1 {
			t.Fatalf("save-and-continue issued %d saves", svc.saveCount())
		}
		if m.sessionSlot.state.rec == nil || m.sessionSlot.state.rec.Rows[0].Revision != 1 {
			t.Fatalf("the saved revision did not reach the session slot: %+v", m.sessionSlot.state.rec)
		}
	})

	t.Run("save failure", func(t *testing.T) {
		m, q, svc := newDirtyModel(t)
		svc.mu.Lock()
		svc.saveFn = func(string, int64, int64, app.ItemRevisionEdit) (app.ItemRevisionResult, error) {
			return app.ItemRevisionResult{}, errors.New("磁盘已满")
		}
		svc.mu.Unlock()
		m.OpenHistory()
		m.promptSaveAndContinue()
		drainAll(t, m, q)
		if m.activeSlot != resultSlotSession || m.edit == nil || m.navPrompt == nil {
			t.Fatalf("a failed save left the draft: slot %v edit %v prompt %+v",
				m.activeSlot, m.edit, m.navPrompt)
		}
		if !strings.Contains(m.reviewErr, "磁盘已满") {
			t.Fatalf("the save failure was not shown: %q", m.reviewErr)
		}
		if slotLoadCount(svc, "old") != 0 {
			t.Fatal("a failed save switched to history")
		}
	})
}

// TestStaleResultLoadsStayInTheirSlot checks the slot + version guard:
// a history load that lands after the user returned to the live session
// updates only the history slot, and a superseded history load never
// replaces a newer history record.
func TestStaleResultLoadsStayInTheirSlot(t *testing.T) {
	svc := &fakeService{
		list: []record.RecordInfo{
			{ID: "old1", SavedAt: time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)},
			{ID: "old2", SavedAt: time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)},
		},
		loads: map[string]*record.Recovery{
			"s1":   recovery(item(1, record.StatusProduced, "", "", "本次")),
			"old1": recovery(item(1, record.StatusProduced, "", "", "旧一")),
			"old2": recovery(item(1, record.StatusProduced, "", "", "旧二")),
		},
	}
	m, q := newTestModel(t, Config{Service: svc})
	drainAll(t, m, q)
	m.Apply(snap("s1", 1, true, scan.PhaseCapturing, scan.Counts{Captured: 1, Produced: 1}, 1, true, ""))
	drainAll(t, m, q)

	type loadGate struct {
		started chan struct{}
		release chan struct{}
	}
	gates := map[string]*loadGate{
		"old1": {started: make(chan struct{}, 1), release: make(chan struct{})},
		"old2": {started: make(chan struct{}, 1), release: make(chan struct{})},
	}
	svc.mu.Lock()
	svc.loadFn = func(id string) (*record.Recovery, error) {
		gate := gates[id]
		if gate == nil {
			return svc.loads[id], nil
		}
		gate.started <- struct{}{}
		<-gate.release
		return svc.loads[id], nil
	}
	svc.mu.Unlock()

	// A history load still in flight must not replace the session after
	// ShowLive; it lands in the inactive history slot.
	m.histChoice = slotHistoryLabel(t, m, "old1")
	m.OpenHistory()
	<-gates["old1"].started
	m.ShowLive()
	if m.activeSlot != resultSlotSession || m.rec.ID != "s1" {
		t.Fatalf("ShowLive did not switch while history loaded: %+v", m.rec)
	}
	close(gates["old1"].release)
	drainAll(t, m, q)
	if m.rec == nil || m.rec.ID != "s1" || !m.rec.Live {
		t.Fatalf("a stale history load overwrote the session: %+v", m.rec)
	}
	if m.historySlot.state.rec == nil || m.historySlot.state.rec.ID != "old1" {
		t.Fatalf("the history load did not settle in its own slot: %+v", m.historySlot.state.rec)
	}

	// Consecutive history opens: the first response is superseded even if
	// it lands, and the last chosen record wins.
	gates["old2"] = &loadGate{started: make(chan struct{}, 1), release: make(chan struct{})}
	gates["old1"] = &loadGate{started: make(chan struct{}, 1), release: make(chan struct{})}
	m.histChoice = slotHistoryLabel(t, m, "old2")
	m.OpenHistory()
	<-gates["old2"].started
	m.histChoice = slotHistoryLabel(t, m, "old1")
	m.OpenHistory()
	<-gates["old1"].started
	close(gates["old2"].release)
	close(gates["old1"].release)
	drainAll(t, m, q)
	if m.activeSlot != resultSlotHistory || m.rec == nil || m.rec.ID != "old1" {
		t.Fatalf("a superseded history load replaced the newer record: %+v", m.rec)
	}
	if m.historySlot.state.rec == nil || m.historySlot.state.rec.ID != "old1" {
		t.Fatalf("the history slot holds %+v", m.historySlot.state.rec)
	}
	if m.sessionSlot.state.rec == nil || m.sessionSlot.state.rec.ID != "s1" {
		t.Fatal("history switching changed the session slot")
	}
}

// TestHistoryPendingZeroUsesItsOwnRecord checks that a finished history
// snapshot takes its completion count and export summary from its own
// checkpoint, never from the live session.
func TestHistoryPendingZeroUsesItsOwnRecord(t *testing.T) {
	finished := reviewRowItem(t, 1, 12)
	finished.Quality = record.QualityHumanConfirmed
	svc := &fakeService{
		list: []record.RecordInfo{{
			ID:      "old",
			SavedAt: time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC),
		}},
		loads: map[string]*record.Recovery{
			"s1":  recovery(reviewRowItem(t, 1, 12), reviewRowItem(t, 2, 10)),
			"old": recovery(finished),
		},
	}
	m, q := newTestModel(t, Config{Service: svc})
	drainAll(t, m, q)
	m.Apply(snap("s1", 1, true, scan.PhaseCapturing, scan.Counts{Captured: 2, Produced: 2}, 1, true, ""))
	drainAll(t, m, q)
	if m.pendingCount() != 2 || m.exportData == nil || m.exportData.Summary.Included != 0 {
		t.Fatalf("the session baseline is wrong: pending %d export %+v", m.pendingCount(), m.exportData)
	}

	m.histChoice = slotHistoryLabel(t, m, "old")
	m.OpenHistory()
	drainAll(t, m, q)
	if m.rec == nil || m.rec.ID != "old" || m.rec.Live {
		t.Fatalf("history did not open: %+v", m.rec)
	}
	if m.pendingCount() != 0 {
		t.Fatalf("the finished history snapshot reports %d pending", m.pendingCount())
	}
	if m.exportData == nil {
		t.Fatalf("the history export snapshot is missing: %q", m.exportBuildErr)
	}
	if m.exportData.Summary.Included != 1 || m.exportData.Summary.Captured != 1 {
		t.Fatalf("history used another record's export summary: %+v", m.exportData.Summary)
	}
	if m.sessionSlot.state.exportData == nil || m.sessionSlot.state.exportData.Summary.Included != 0 {
		t.Fatalf("opening history changed the session export snapshot: %+v", m.sessionSlot.state.exportData)
	}
}
