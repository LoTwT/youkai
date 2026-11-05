package ui

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/game/zzz"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/LoTwT/youkai/internal/scan"
	"github.com/egoist/mygo/ui"
)

// historyLabel returns the picker label of one loaded history record.
func historyLabel(t *testing.T, m *Model, id string) string {
	t.Helper()
	for _, entry := range m.history {
		if entry.ID == id {
			return entry.Label
		}
	}
	t.Fatalf("history record %s is not loaded: %+v", id, m.history)
	return ""
}

// recordLoadCount counts the loads of one record.
func recordLoadCount(svc *fakeService, id string) int {
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

// TestFinishedScanAndListShareOneRecordWorkspace checks the merge: the
// record of a just-finished scan and the same record reopened from the
// records list are one workspace with one edit buffer. Reopening it keeps
// the unsaved edit; switching to another record asks what to do with it.
func TestFinishedScanAndListShareOneRecordWorkspace(t *testing.T) {
	svc := &fakeService{
		list: []record.RecordInfo{
			{ID: "s1", SavedAt: time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)},
			{ID: "old", SavedAt: time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)},
		},
		loads: map[string]*record.Recovery{
			"s1":  recovery(reviewRowItem(t, 1, 12)),
			"old": recovery(reviewRowItem(t, 1, 12)),
		},
	}
	installReviewSave(svc)
	m, q := newTestModel(t, Config{Service: svc})
	drainAll(t, m, q)
	m.Apply(snap("s1", 1, true, scan.PhaseCapturing,
		scan.Counts{Captured: 1, Produced: 1}, 0, true, ""))
	drainAll(t, m, q)
	m.Apply(snap("s1", 2, false, scan.PhaseEnded,
		scan.Counts{Captured: 1, Produced: 1}, 1, false, ""))
	drainAll(t, m, q)
	if m.displayPage() != pageResults || m.recordTab != recordTabSummary {
		t.Fatalf("the finished scan did not enter the new record's summary: page %v tab %v",
			m.displayPage(), m.recordTab)
	}
	if m.rec == nil || m.rec.ID != "s1" || !m.rec.Live {
		t.Fatalf("the finished scan's record is not open: %+v", m.rec)
	}

	showRecordItems(m)
	m.startEdit()
	if m.edit == nil {
		t.Fatal("the editor did not open")
	}
	m.edit.draft.Level = "13"
	loads := recordLoadCount(svc, "s1")

	m.histChoice = historyLabel(t, m, "s1")
	m.OpenHistory()
	drainAll(t, m, q)
	if m.navPrompt != nil {
		t.Fatalf("reopening the same record prompted for its own draft: %+v", m.navPrompt)
	}
	if m.rec == nil || m.rec.ID != "s1" || m.edit == nil || m.edit.draft.Level != "13" {
		t.Fatalf("the same record lost its edit buffer: record %+v edit %+v", m.rec, m.edit)
	}
	if got := recordLoadCount(svc, "s1"); got != loads {
		t.Fatalf("reopening the same record reloaded it from disk (%d loads, want %d)", got, loads)
	}

	// Another record waits for the draft through the existing prompt.
	m.histChoice = historyLabel(t, m, "old")
	m.OpenHistory()
	if m.navPrompt == nil || m.navPrompt.kind != navOpenRecord || m.navPrompt.recordID != "old" {
		t.Fatalf("a dirty switch did not prompt: %+v", m.navPrompt)
	}
	if m.rec == nil || m.rec.ID != "s1" {
		t.Fatal("the record switched before the draft settled")
	}
	if recordLoadCount(svc, "old") != 0 {
		t.Fatal("the other record loaded behind the prompt")
	}
	m.promptStay()
	if m.edit == nil || m.edit.draft.Level != "13" {
		t.Fatal("staying lost the draft")
	}
	m.OpenHistory()
	m.promptDiscardAndContinue()
	drainAll(t, m, q)
	if m.rec == nil || m.rec.ID != "old" || m.rec.Live || m.edit != nil {
		t.Fatalf("discarding did not open the other record cleanly: record %+v edit %v",
			m.rec, m.edit)
	}
	if got := recordLoadCount(svc, "old"); got != 1 {
		t.Fatalf("the other record loaded %d times, want 1", got)
	}
}

// TestUnsavedPromptSavesOrStaysForAnotherRecord checks the prompt's save
// action when the draft belongs to the just-finished scan: a successful
// save reaches that record and then opens the other one cleanly; a failed
// save keeps the draft and never loads the other record.
func TestUnsavedPromptSavesOrStaysForAnotherRecord(t *testing.T) {
	newDirtyModel := func(t *testing.T) (*Model, *queueUpdater, *fakeService) {
		t.Helper()
		live := recovery(reviewRowItem(t, 1, 12))
		live.Checkpoint.Session.SessionID = "s1"
		other := recovery(structuredItem(1,
			zzz.DiscRecord{SetID: intp(31200), Slot: 2, Level: intp(15)}, "另一条"))
		other.Checkpoint.Session.SessionID = "old"
		svc := &fakeService{
			list: []record.RecordInfo{
				{ID: "s1", SavedAt: time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)},
				{ID: "old", SavedAt: time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)},
			},
			loads: map[string]*record.Recovery{"s1": live, "old": other},
		}
		installReviewSave(svc)
		m, q := newTestModel(t, Config{Service: svc})
		drainAll(t, m, q)
		m.Apply(snap("s1", 1, true, scan.PhaseCapturing,
			scan.Counts{Captured: 1, Produced: 1}, 1, true, ""))
		drainAll(t, m, q)
		showRecordItems(m)
		m.startEdit()
		if m.edit == nil {
			t.Fatal("the editor did not open")
		}
		m.edit.draft.Level = "13"
		m.histChoice = historyLabel(t, m, "old")
		return m, q, svc
	}

	t.Run("save", func(t *testing.T) {
		m, q, svc := newDirtyModel(t)
		m.OpenHistory()
		if m.navPrompt == nil || m.navPrompt.kind != navOpenRecord || m.navPrompt.recordID != "old" {
			t.Fatalf("the dirty switch did not prompt: %+v", m.navPrompt)
		}
		m.promptSaveAndContinue()
		drainAll(t, m, q)
		svc.mu.Lock()
		saves := append([]saveCall(nil), svc.saves...)
		svc.mu.Unlock()
		if len(saves) != 1 || saves[0].recordID != "s1" {
			t.Fatalf("the save went to %+v, want the draft's record s1", saves)
		}
		if m.rec == nil || m.rec.ID != "old" || m.rec.Live || m.edit != nil || m.navPrompt != nil {
			t.Fatalf("save-and-continue did not switch cleanly: record %+v edit %v prompt %+v",
				m.rec, m.edit, m.navPrompt)
		}
		row := m.rec.Rows[0]
		if row.Structured == nil || row.Structured.Data.Slot == nil || *row.Structured.Data.Slot != 2 {
			t.Fatalf("the saved item leaked into the other record: %+v", row)
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
		if m.rec == nil || m.rec.ID != "s1" || !m.rec.Live {
			t.Fatalf("a failed save switched records: %+v", m.rec)
		}
		if m.edit == nil || m.edit.draft.Level != "13" {
			t.Fatalf("the failed save lost the draft: %+v", m.edit)
		}
		if m.navPrompt == nil {
			t.Fatal("the failed save dropped the held navigation")
		}
		if !strings.Contains(m.reviewErr, "磁盘已满") {
			t.Fatalf("the save failure was not shown: %q", m.reviewErr)
		}
		if got := recordLoadCount(svc, "old"); got != 0 {
			t.Fatalf("the other record loaded behind the failed save (%d loads)", got)
		}
	})
}

// TestShowLivePromptsForAHistoryDraft checks the current-scan entry:
// showing the live record while the reopened record holds unsaved input
// goes through the same prompt instead of discarding it.
func TestShowLivePromptsForAHistoryDraft(t *testing.T) {
	live := recovery(reviewRowItem(t, 1, 12))
	live.Checkpoint.Session.SessionID = "s1"
	old := recovery(structuredItem(1,
		zzz.DiscRecord{SetID: intp(31200), Slot: 2, Level: intp(15)}, "另一条"))
	old.Checkpoint.Session.SessionID = "old"
	svc := &fakeService{
		list: []record.RecordInfo{
			{ID: "s1", SavedAt: time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)},
			{ID: "old", SavedAt: time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)},
		},
		loads: map[string]*record.Recovery{"s1": live, "old": old},
	}
	installReviewSave(svc)
	m, q := newTestModel(t, Config{Service: svc})
	drainAll(t, m, q)
	m.Apply(snap("s1", 1, true, scan.PhaseCapturing,
		scan.Counts{Captured: 1, Produced: 1}, 1, true, ""))
	drainAll(t, m, q)
	m.histChoice = historyLabel(t, m, "old")
	m.OpenHistory()
	drainAll(t, m, q)
	showRecordItems(m)
	m.startEdit()
	if m.edit == nil {
		t.Fatal("the editor did not open on the history record")
	}
	m.edit.draft.Level = "13"
	loads := recordLoadCount(svc, "s1")

	m.ShowLive()
	if m.navPrompt == nil || m.navPrompt.kind != navOpenRecord || m.navPrompt.recordID != "s1" {
		t.Fatalf("ShowLive with a dirty history record did not prompt: %+v", m.navPrompt)
	}
	if m.rec == nil || m.rec.ID != "old" || m.edit == nil || m.edit.draft.Level != "13" {
		t.Fatalf("ShowLive moved before the prompt settled: record %+v edit %+v", m.rec, m.edit)
	}
	if got := recordLoadCount(svc, "s1"); got != loads {
		t.Fatalf("ShowLive loaded the live record behind the prompt (%d loads, want %d)", got, loads)
	}
}

// TestSavedRevisionIsTheOpenRecordWhenReopenedFromList checks the other
// direction of the merge: a revision saved from the just-finished scan is
// already in the one workspace, so reopening the record from the list
// shows it without a stale buffer or a reload.
func TestSavedRevisionIsTheOpenRecordWhenReopenedFromList(t *testing.T) {
	svc := &fakeService{
		list: []record.RecordInfo{{
			ID: "s1", SavedAt: time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC),
		}},
		loads: map[string]*record.Recovery{"s1": recovery(reviewRowItem(t, 1, 12))},
	}
	installReviewSave(svc)
	m, q := newTestModel(t, Config{Service: svc})
	drainAll(t, m, q)
	m.Apply(snap("s1", 1, false, scan.PhaseEnded,
		scan.Counts{Captured: 1, Produced: 1}, 1, false, ""))
	drainAll(t, m, q)
	if m.rec == nil || m.rec.ID != "s1" || len(m.rec.Rows) != 1 {
		t.Fatalf("the finished record did not load: %+v", m.rec)
	}
	showRecordItems(m)
	selected := m.selected
	before := m.rec.Rows[selected].Revision
	m.startEdit()
	m.edit.draft.Level = "13"
	m.saveEdit(reviewSave, nil)
	drainAll(t, m, q)
	if m.edit != nil {
		t.Fatalf("the save left the editor open: %+v", m.edit)
	}
	saved := m.rec.Rows[selected].Revision
	if saved != before+1 {
		t.Fatalf("the open record shows revision %d, want %d", saved, before+1)
	}
	loads := recordLoadCount(svc, "s1")

	m.histChoice = historyLabel(t, m, "s1")
	m.OpenHistory()
	drainAll(t, m, q)
	if m.rec == nil || m.rec.ID != "s1" || m.rec.Rows[selected].Revision != saved {
		t.Fatalf("the reopened record does not show the saved revision: %+v", m.rec)
	}
	if m.edit != nil {
		t.Fatal("the reopened record brought back an edit buffer")
	}
	if got := recordLoadCount(svc, "s1"); got != loads {
		t.Fatalf("reopening the record reloaded it (%d loads, want %d)", got, loads)
	}
}

// TestFinishedScanLeavesAnotherOpenRecordAlone checks the boundary: when
// the user is reading a different record while a scan finishes in the
// background, the new record does not steal the workspace or its tab.
func TestFinishedScanLeavesAnotherOpenRecordAlone(t *testing.T) {
	svc := &fakeService{
		list: []record.RecordInfo{{
			ID: "old", SavedAt: time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC),
		}},
		loads: map[string]*record.Recovery{
			"s1":  recovery(reviewRowItem(t, 1, 12)),
			"old": recovery(reviewRowItem(t, 1, 12)),
		},
	}
	installReviewSave(svc)
	m, q := newTestModel(t, Config{Service: svc})
	drainAll(t, m, q)
	m.Apply(snap("s1", 1, true, scan.PhaseCapturing,
		scan.Counts{Captured: 1, Produced: 1}, 1, true, ""))
	drainAll(t, m, q)

	m.histChoice = historyLabel(t, m, "old")
	m.OpenHistory()
	drainAll(t, m, q)
	showRecordItems(m)
	if m.rec == nil || m.rec.ID != "old" || m.rec.Live {
		t.Fatalf("the other record did not open: %+v", m.rec)
	}
	loads := recordLoadCount(svc, "old")

	m.Apply(snap("s1", 2, false, scan.PhaseEnded,
		scan.Counts{Captured: 1, Produced: 1}, 1, false, ""))
	drainAll(t, m, q)
	if m.rec == nil || m.rec.ID != "old" || m.rec.Live {
		t.Fatalf("the finished scan replaced the open record: %+v", m.rec)
	}
	if m.displayPage() != pageResults || m.recordTab != recordTabItems {
		t.Fatalf("the finished scan stole the page: page %v tab %v",
			m.displayPage(), m.recordTab)
	}
	if got := recordLoadCount(svc, "old"); got != loads {
		t.Fatalf("the finished scan reloaded the open record (%d loads, want %d)", got, loads)
	}
}

// TestRefusedStartKeepsTheDraftOnTheReturningRecord checks a start the
// service refuses: the snapshot falls back to the session the draft
// belongs to, so that record keeps its editor and becomes live again
// instead of being replaced by an empty view.
func TestRefusedStartKeepsTheDraftOnTheReturningRecord(t *testing.T) {
	rec := recovery(reviewRowItem(t, 1, 12))
	rec.Checkpoint.Session.SessionID = "s1"
	svc := &fakeService{loads: map[string]*record.Recovery{"s1": rec}}
	installReviewSave(svc)
	m, q := newTestModel(t, Config{Service: svc})
	drainAll(t, m, q)
	m.Apply(snap("s1", 1, true, scan.PhaseCapturing,
		scan.Counts{Captured: 1, Produced: 1}, 1, true, ""))
	drainAll(t, m, q)
	m.Apply(snap("s1", 2, false, scan.PhaseEnded,
		scan.Counts{Captured: 1, Produced: 1}, 1, false, ""))
	drainAll(t, m, q)
	showRecordItems(m)
	m.startEdit()
	if m.edit == nil {
		t.Fatal("the editor did not open")
	}
	m.edit.draft.Level = "13"
	loads := recordLoadCount(svc, "s1")

	// Another scan starts behind the draft: the open record is history for
	// now, but the editor stays.
	m.Apply(snap("s2", 3, true, scan.PhasePreparing, scan.Counts{}, 1, true, ""))
	drainAll(t, m, q)
	if m.rec == nil || m.rec.ID != "s1" || m.rec.Live || m.edit == nil ||
		m.edit.draft.Level != "13" {
		t.Fatalf("the background scan disturbed the draft: record %+v edit %+v", m.rec, m.edit)
	}

	// The service refuses s2 and rolls back to s1's ended snapshot.
	m.Apply(snap("s1", 3, false, scan.PhaseEnded,
		scan.Counts{Captured: 1, Produced: 1}, 1, false, ""))
	drainAll(t, m, q)
	if m.rec == nil || m.rec.ID != "s1" || !m.rec.Live {
		t.Fatalf("the returning record is not live again: %+v", m.rec)
	}
	if m.edit == nil || m.edit.draft.Level != "13" {
		t.Fatalf("the rollback dropped the draft: %+v", m.edit)
	}
	if m.reviewNotice != "" {
		t.Fatalf("the rollback kept the replaced-scanner notice: %q", m.reviewNotice)
	}
	if got := recordLoadCount(svc, "s1"); got != loads {
		t.Fatalf("the rollback reloaded the record (%d loads, want %d)", got, loads)
	}
}

// TestRefusedStartClearsTheNoticeOverAHistoryDraft checks the same rollback
// while the user edits a different record: the history record and its draft
// stay, and the stale "a new scan has started" notice goes away because no
// scan is running.
func TestRefusedStartClearsTheNoticeOverAHistoryDraft(t *testing.T) {
	live := recovery(structuredItem(1,
		zzz.DiscRecord{SetID: intp(31100), Slot: 1, Level: intp(15)}, "本次"))
	live.Checkpoint.Session.SessionID = "s1"
	old := recovery(reviewRowItem(t, 1, 12))
	old.Checkpoint.Session.SessionID = "old"
	svc := &fakeService{
		list: []record.RecordInfo{
			{ID: "s1", SavedAt: time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)},
			{ID: "old", SavedAt: time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)},
		},
		loads: map[string]*record.Recovery{"s1": live, "old": old},
	}
	installReviewSave(svc)
	m, q := newTestModel(t, Config{Service: svc})
	drainAll(t, m, q)
	m.Apply(snap("s1", 1, true, scan.PhaseCapturing,
		scan.Counts{Captured: 1, Produced: 1}, 1, true, ""))
	drainAll(t, m, q)
	m.Apply(snap("s1", 2, false, scan.PhaseEnded,
		scan.Counts{Captured: 1, Produced: 1}, 1, false, ""))
	drainAll(t, m, q)

	m.histChoice = historyLabel(t, m, "old")
	m.OpenHistory()
	drainAll(t, m, q)
	showRecordItems(m)
	m.startEdit()
	if m.edit == nil {
		t.Fatal("the editor did not open on the history record")
	}
	m.edit.draft.Level = "13"

	m.Apply(snap("s2", 3, true, scan.PhasePreparing, scan.Counts{}, 1, true, ""))
	drainAll(t, m, q)
	if !strings.Contains(m.reviewNotice, "新的扫描已开始") {
		t.Fatalf("the started scan gave no notice: %q", m.reviewNotice)
	}
	if m.rec == nil || m.rec.ID != "old" || m.edit == nil || m.edit.draft.Level != "13" {
		t.Fatalf("the started scan disturbed the history draft: record %+v edit %+v",
			m.rec, m.edit)
	}

	// The service refuses s2 and returns to s1's ended snapshot.
	m.Apply(snap("s1", 3, false, scan.PhaseEnded,
		scan.Counts{Captured: 1, Produced: 1}, 1, false, ""))
	drainAll(t, m, q)
	if m.reviewNotice != "" {
		t.Fatalf("the refused start kept the stale notice: %q", m.reviewNotice)
	}
	if m.rec == nil || m.rec.ID != "old" || m.rec.Live || m.edit == nil ||
		m.edit.draft.Level != "13" {
		t.Fatalf("the rollback disturbed the history draft: record %+v edit %+v",
			m.rec, m.edit)
	}
}

// TestReopeningAFailedRecordRetriesTheLoad checks the records entry after a
// failed load: opening the record again clears the stale view and loads it,
// so the workspace never stays stuck on the error.
func TestReopeningAFailedRecordRetriesTheLoad(t *testing.T) {
	rec := recovery(reviewRowItem(t, 1, 12))
	rec.Checkpoint.Session.SessionID = "r1"
	svc := &fakeService{
		list: []record.RecordInfo{{
			ID: "r1", SavedAt: time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC),
		}},
		loads: map[string]*record.Recovery{"r1": rec},
	}
	m, q := newTestModel(t, Config{Service: svc})
	drainAll(t, m, q)
	m.histChoice = historyLabel(t, m, "r1")
	svc.mu.Lock()
	svc.loadErr = map[string]error{"r1": errors.New("磁盘读取失败")}
	svc.mu.Unlock()
	m.OpenHistory()
	drainAll(t, m, q)
	if m.recErr == "" || m.rec == nil || m.rec.ID != "r1" {
		t.Fatalf("the failed load did not leave its error: record %+v err %q", m.rec, m.recErr)
	}

	svc.mu.Lock()
	svc.loadErr = nil
	svc.mu.Unlock()
	m.OpenHistory()
	drainAll(t, m, q)
	if m.recErr != "" || m.rec == nil || m.rec.ID != "r1" || len(m.rec.Rows) != 1 {
		t.Fatalf("reopening did not retry the load: record %+v err %q", m.rec, m.recErr)
	}
}

// TestLateSaveResponseDoesNotTouchAnotherRecord checks the save identity
// guard: a response for the record replaced by a later one is discarded
// instead of writing its item into the new record's row with the same
// capture sequence.
func TestLateSaveResponseDoesNotTouchAnotherRecord(t *testing.T) {
	first := recovery(reviewRowItem(t, 1, 12))
	first.Checkpoint.Session.SessionID = "s1"
	second := recovery(reviewRowItem(t, 1, 10))
	second.Checkpoint.Session.SessionID = "old"
	svc := &fakeService{loads: map[string]*record.Recovery{"s1": first, "old": second}}

	started := make(chan struct{}, 1)
	release := make(chan struct{})
	svc.saveFn = func(recordID string, sequence, expectedRevision int64, edit app.ItemRevisionEdit) (app.ItemRevisionResult, error) {
		started <- struct{}{}
		<-release
		item := first.Checkpoint.Items[0]
		item.Revision = expectedRevision + 1
		return app.ItemRevisionResult{Item: item, Generation: 2}, nil
	}
	m, q := newTestModel(t, Config{Service: svc})
	drainAll(t, m, q)
	m.Apply(snap("s1", 1, false, scan.PhaseEnded,
		scan.Counts{Captured: 1, Produced: 1}, 1, false, ""))
	drainAll(t, m, q)
	showRecordItems(m)
	m.startEdit()
	m.edit.draft.Level = "13"
	m.saveEdit(reviewSave, nil)
	<-started

	// The real UI cannot switch while a save runs; the navigation guard
	// rejects the attempt and leaves the workspace alone.
	m.openRecord("old")
	if m.navPrompt != nil || m.rec == nil || m.rec.ID != "s1" {
		t.Fatalf("a navigation during the save moved the workspace: record %+v prompt %+v",
			m.rec, m.navPrompt)
	}
	if !strings.Contains(m.reviewErr, "正在保存") {
		t.Fatalf("the blocked navigation gave no reason: %q", m.reviewErr)
	}
	if got := recordLoadCount(svc, "old"); got != 0 {
		t.Fatalf("the blocked navigation loaded the other record (%d loads)", got)
	}

	// The identity check is still the last line of defence: replace the
	// record directly, as a state change that bypassed the guard would,
	// and let the late response land.
	m.showRecord("old", false)
	m.applyLoadedRecord("old", second, false)
	if m.rec == nil || m.rec.ID != "old" {
		t.Fatalf("the other record did not open: %+v", m.rec)
	}
	close(release)
	drainAll(t, m, q)

	if m.rec == nil || m.rec.ID != "old" || len(m.rec.Rows) != 1 {
		t.Fatalf("the late save replaced the workspace: %+v", m.rec)
	}
	row := m.rec.Rows[0]
	if row.Revision != second.Checkpoint.Items[0].Revision || row.Structured == nil ||
		row.Structured.Data.Level == nil || *row.Structured.Data.Level != 10 {
		t.Fatalf("the late save wrote into the other record: %+v", row)
	}
	if m.edit != nil {
		t.Fatalf("the late save left an edit buffer: %+v", m.edit)
	}
}

// TestStaleResultLoadsNeverReplaceTheOpenRecord checks the load version
// and identity guard: a record load that lands after the user opened another
// record is discarded, and a superseded load never replaces a newer one.
func TestStaleResultLoadsNeverReplaceTheOpenRecord(t *testing.T) {
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

	// A record load still in flight must not replace the live record after
	// the user returned to it.
	m.histChoice = historyLabel(t, m, "old1")
	m.OpenHistory()
	<-gates["old1"].started
	m.ShowLive()
	if m.rec == nil || m.rec.ID != "s1" {
		t.Fatalf("ShowLive did not open the live record while history loaded: %+v", m.rec)
	}
	close(gates["old1"].release)
	drainAll(t, m, q)
	if m.rec == nil || m.rec.ID != "s1" || !m.rec.Live {
		t.Fatalf("a stale history load overwrote the live record: %+v", m.rec)
	}

	// Consecutive history opens: the first response is superseded even if
	// it lands, and the last chosen record wins.
	gates["old2"] = &loadGate{started: make(chan struct{}, 1), release: make(chan struct{})}
	gates["old1"] = &loadGate{started: make(chan struct{}, 1), release: make(chan struct{})}
	m.histChoice = historyLabel(t, m, "old2")
	m.OpenHistory()
	<-gates["old2"].started
	m.histChoice = historyLabel(t, m, "old1")
	m.OpenHistory()
	<-gates["old1"].started
	close(gates["old2"].release)
	close(gates["old1"].release)
	drainAll(t, m, q)
	if m.rec == nil || m.rec.ID != "old1" || m.rec.Live {
		t.Fatalf("a superseded history load replaced the newer record: %+v", m.rec)
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

	m.histChoice = historyLabel(t, m, "old")
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
}

// TestFinalCheckpointLoadingGatesCompletionAndExport checks the window
// between a saved-count growth and its final checkpoint landing: the old
// snapshot must not satisfy the completion state or an export.
func TestFinalCheckpointLoadingGatesCompletionAndExport(t *testing.T) {
	initial := recovery(confirmedReviewItem(t, 1, 12))
	final := recovery(confirmedReviewItem(t, 1, 12), confirmedReviewItem(t, 2, 10))
	svc := &fakeService{loads: map[string]*record.Recovery{"s1": initial}}
	saver := &fakeDirectExportSaver{}
	m, q := newTestModel(t, Config{Service: svc, DirectExportSaver: saver})

	m.Apply(snap("s1", 1, true, scan.PhaseCapturing,
		scan.Counts{Captured: 1, Produced: 1}, 1, true, ""))
	drainAll(t, m, q)
	if m.rec == nil || len(m.rec.Rows) != 1 || !m.rec.Live {
		t.Fatalf("the initial checkpoint did not load: %+v", m.rec)
	}
	if m.exportData == nil || m.exportData.Summary.Included != 1 {
		t.Fatalf("the initial export snapshot is wrong: %+v (%q)", m.exportData, m.exportBuildErr)
	}

	started := make(chan struct{}, 1)
	release := make(chan struct{})
	svc.mu.Lock()
	svc.loadFn = func(id string) (*record.Recovery, error) {
		if id == "s1" {
			select {
			case started <- struct{}{}:
			default:
			}
			<-release
			return final, nil
		}
		return svc.loads[id], nil
	}
	svc.mu.Unlock()

	// The final checkpoint grew to two items, but its load has not
	// returned. The on-screen record is still the one-item snapshot.
	m.Apply(snap("s1", 2, false, scan.PhaseEnded,
		scan.Counts{Captured: 2, Produced: 2}, 2, false, ""))
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("the final checkpoint load did not start")
	}
	if !m.loading {
		t.Fatal("the session slot does not mark its final load as pending")
	}
	if m.completionVisible() {
		t.Fatal("the completion state shows during the final checkpoint load")
	}
	if reason := m.exportBlockedReason(); reason == "" {
		t.Fatal("export is not blocked during the final checkpoint load")
	}
	if m.exportData == nil || m.exportData.Summary.Included != 1 {
		t.Fatalf("the stale export snapshot changed before the load landed: %+v", m.exportData)
	}
	m.ExportJSON()
	if calls := saver.snapshot(); len(calls) != 0 {
		t.Fatalf("an export started from the stale snapshot: %+v", calls)
	}

	close(release)
	drainAll(t, m, q)
	if m.loading {
		t.Fatal("the landed final checkpoint left the slot loading")
	}
	if m.pendingCount() != 0 || len(m.rec.Rows) != 2 {
		t.Fatalf("the final checkpoint is not applied: pending %d rows %d",
			m.pendingCount(), len(m.rec.Rows))
	}
	if !m.completionVisible() {
		t.Fatal("the completion state does not show after the final checkpoint landed")
	}
	if reason := m.exportBlockedReason(); reason != "" {
		t.Fatalf("export stays blocked after the final checkpoint landed: %q", reason)
	}
	if m.exportData == nil || m.exportData.Summary.Included != 2 {
		t.Fatalf("the export snapshot did not advance: %+v", m.exportData)
	}

	// A failed reload clears loading through its own current version, but
	// the latest checkpoint never became ready: completion stays hidden
	// and the old snapshot stays unexportable.
	svc.mu.Lock()
	svc.loadFn = func(string) (*record.Recovery, error) {
		return nil, errors.New("磁盘读取失败")
	}
	svc.mu.Unlock()
	m.reloadRecord("s1", true)
	drainAll(t, m, q)
	if m.loading || m.recErr == "" {
		t.Fatalf("the failed reload state = loading %v err %q", m.loading, m.recErr)
	}
	if m.completionVisible() {
		t.Fatal("a failed checkpoint load still shows completion")
	}
	if reason := m.exportBlockedReason(); reason == "" {
		t.Fatal("a failed checkpoint load still allows exporting the old snapshot")
	}
}

// TestSupersededResultLoadDoesNotClearLoading checks that only the current
// load's response clears the workspace's loading state; an older response
// that lands while a newer load is in flight must leave the guard in place.
func TestSupersededResultLoadDoesNotClearLoading(t *testing.T) {
	svc := &fakeService{loads: map[string]*record.Recovery{
		"s1": recovery(confirmedReviewItem(t, 1, 12)),
	}}
	m, q := newTestModel(t, Config{Service: svc})
	m.showRecord("s1", true)
	releases := []chan struct{}{make(chan struct{}), make(chan struct{})}
	started := []chan struct{}{make(chan struct{}, 1), make(chan struct{}, 1)}
	responses := []*record.Recovery{
		recovery(confirmedReviewItem(t, 1, 12), confirmedReviewItem(t, 2, 10)),
		recovery(confirmedReviewItem(t, 1, 12), confirmedReviewItem(t, 2, 10), confirmedReviewItem(t, 3, 14)),
	}
	call := 0
	svc.mu.Lock()
	svc.loadFn = func(string) (*record.Recovery, error) {
		index := call
		call++
		started[index] <- struct{}{}
		<-releases[index]
		return responses[index], nil
	}
	svc.mu.Unlock()

	m.reloadRecord("s1", true)
	<-started[0]
	m.reloadRecord("s1", true)
	<-started[1]
	if !m.loading {
		t.Fatal("the newer load did not mark the workspace loading")
	}

	// Let the superseded response land while the newer one is still
	// blocked.
	close(releases[0])
	deadline := time.Now().Add(2 * time.Second)
	for q.count() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if q.count() == 0 {
		t.Fatal("the superseded load response did not arrive")
	}
	for _, fn := range q.take() {
		fn()
	}
	if !m.loading {
		t.Fatal("a superseded response cleared the loading guard")
	}
	if len(m.rec.Rows) != 0 {
		t.Fatalf("a superseded response applied its record: %+v", m.rec)
	}

	close(releases[1])
	drainAll(t, m, q)
	if m.loading {
		t.Fatal("the current response did not clear the loading guard")
	}
	if m.rec == nil || len(m.rec.Rows) != 3 {
		t.Fatalf("the current response did not apply: %+v", m.rec)
	}
}

// TestFinalCheckpointLoadingBlocksConfirmation checks the final checkpoint
// gap from the review side: while the newer checkpoint is in flight the
// stale rows must not be confirmable or exportable, and a confirmation may
// not clear the loading guard. The landed checkpoint then provides both
// rows to the view and to an export.
func TestFinalCheckpointLoadingBlocksConfirmation(t *testing.T) {
	initial := recovery(reviewRowItem(t, 1, 12))
	initial.Checkpoint.Session.SessionID = "s1"
	final := recovery(
		uiStructuredItem(t, 1, record.QualityHumanConfirmed),
		uiStructuredItem(t, 2, record.QualityHumanConfirmed),
	)
	final.Checkpoint.Session.SessionID = "s1"
	svc := &fakeService{loads: map[string]*record.Recovery{"s1": initial}}
	installReviewSave(svc)
	saver := &fakeDirectExportSaver{}
	m, q := newTestModel(t, Config{
		Service:           svc,
		DirectExportSaver: saver,
		AppVersion:        "test-1",
	})
	m.SetContentSize(1024, 700)
	tt := ui.NewTester(m.View, 1024, 700)
	settle(t, m, q, tt)

	m.Apply(snap("s1", 1, true, scan.PhaseCapturing,
		scan.Counts{Captured: 1, Produced: 1}, 1, true, ""))
	drainAll(t, m, q)
	m.page = pageResults
	showRecordItems(m)
	tt.Frame()
	if m.rec == nil || len(m.rec.Rows) != 1 {
		t.Fatalf("the initial checkpoint did not load: %+v", m.rec)
	}

	started := make(chan struct{}, 1)
	release := make(chan struct{})
	svc.mu.Lock()
	svc.loadFn = func(id string) (*record.Recovery, error) {
		if id == "s1" {
			select {
			case started <- struct{}{}:
			default:
			}
			<-release
			return final, nil
		}
		return svc.loads[id], nil
	}
	svc.mu.Unlock()

	m.Apply(snap("s1", 2, false, scan.PhaseEnded,
		scan.Counts{Captured: 2, Produced: 2}, 2, false, ""))
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("the final checkpoint load did not start")
	}
	if !m.loading {
		t.Fatal("the session slot does not mark its final load as pending")
	}

	// The UI frame shows the stale row and names the reason review actions
	// are unavailable.
	showRecordItems(m)
	tt.Frame()
	if !tt.HasText("正在加载最新检查点") {
		t.Fatalf("the results page does not explain the loading guard: %q", tt.Texts())
	}

	// A button click and the ⌘Enter shortcut must both wait: neither may
	// save the stale row, and a save must not clear the loading guard.
	if err := tt.Click("确认并下一项"); err != nil {
		t.Fatalf("click 确认并下一项: %v", err)
	}
	tt.Key(ui.Cmd, ui.KeyEnter)
	m.confirmCurrentStay()
	if calls := svc.saveCount(); calls != 0 {
		t.Fatalf("a review action saved the stale snapshot: %+v", svc.saves)
	}
	if !m.loading {
		t.Fatal("a blocked confirmation cleared the loading guard")
	}
	if !strings.Contains(m.reviewErr, "加载最新检查点") {
		t.Fatalf("a blocked confirmation gave no reason: %q", m.reviewErr)
	}

	// Export stays blocked too: writing the stale one-row snapshot would
	// disagree with the checkpoint on disk.
	m.ExportJSON()
	if calls := saver.snapshot(); len(calls) != 0 {
		t.Fatalf("an export started from the stale snapshot: %+v", calls)
	}

	close(release)
	drainAll(t, m, q)
	tt.Frame()
	if m.loading {
		t.Fatal("the landed final checkpoint left the slot loading")
	}
	if len(m.rec.Rows) != 2 || m.pendingCount() != 0 {
		t.Fatalf("the final checkpoint is not applied: rows %d pending %d",
			len(m.rec.Rows), m.pendingCount())
	}
	if m.exportData == nil || m.exportData.Summary.Included != 2 {
		t.Fatalf("the export snapshot did not advance: %+v", m.exportData)
	}

	if err := tt.Click("导出 JSON"); err != nil {
		t.Fatalf("click 导出 JSON: %v", err)
	}
	drainAll(t, m, q)
	calls := saver.snapshot()
	if len(calls) != 1 {
		t.Fatalf("direct export calls = %+v", calls)
	}
	var doc struct {
		Items []app.ExportItem `json:"items"`
	}
	if err := json.Unmarshal(calls[0].data, &doc); err != nil {
		t.Fatalf("decode the export: %v", err)
	}
	if len(doc.Items) != 2 {
		t.Fatalf("the export wrote %d rows, want the latest checkpoint's 2", len(doc.Items))
	}
}

// TestFinalCheckpointLoadingBlocksEditorSave checks the editor while the
// latest checkpoint loads: its save and confirm actions are disabled, the
// model refuses a direct save too, and the input survives the rejection.
func TestFinalCheckpointLoadingBlocksEditorSave(t *testing.T) {
	initial := recovery(reviewRowItem(t, 1, 12))
	initial.Checkpoint.Session.SessionID = "s1"
	final := recovery(reviewRowItem(t, 1, 12), reviewRowItem(t, 2, 10))
	final.Checkpoint.Session.SessionID = "s1"
	svc := &fakeService{loads: map[string]*record.Recovery{"s1": initial}}
	installReviewSave(svc)
	m, q := newTestModel(t, Config{Service: svc})
	m.SetContentSize(1024, 700)
	tt := ui.NewTester(m.View, 1024, 700)
	settle(t, m, q, tt)

	m.Apply(snap("s1", 1, true, scan.PhaseCapturing,
		scan.Counts{Captured: 1, Produced: 1}, 1, true, ""))
	drainAll(t, m, q)
	m.page = pageResults
	showRecordItems(m)
	tt.Frame()
	m.startEdit()
	if m.edit == nil {
		t.Fatal("the editor did not open on the initial checkpoint")
	}

	started := make(chan struct{}, 1)
	release := make(chan struct{})
	svc.mu.Lock()
	svc.loadFn = func(id string) (*record.Recovery, error) {
		if id == "s1" {
			select {
			case started <- struct{}{}:
			default:
			}
			<-release
			return final, nil
		}
		return svc.loads[id], nil
	}
	svc.mu.Unlock()

	// The clean editor does not block the refresh; the load starts behind
	// it.
	m.Apply(snap("s1", 2, false, scan.PhaseEnded,
		scan.Counts{Captured: 2, Produced: 2}, 2, false, ""))
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("the final checkpoint load did not start")
	}
	if !m.loading {
		t.Fatal("the session slot does not mark its final load as pending")
	}

	m.edit.draft.Level = "13"
	showRecordItems(m)
	tt.Frame()
	if err := tt.Click("保存"); err != nil {
		t.Fatalf("click 保存: %v", err)
	}
	tt.Key(ui.Cmd, ui.KeyS)
	m.saveEdit(reviewSave, nil)
	if calls := svc.saveCount(); calls != 0 {
		t.Fatalf("the editor saved over the loading checkpoint: %+v", svc.saves)
	}
	if m.edit == nil || m.edit.draft.Level != "13" {
		t.Fatal("the refused save dropped the unsaved input")
	}
	if !strings.Contains(m.reviewErr, "加载最新检查点") {
		t.Fatalf("the refused save gave no loading reason: %q", m.reviewErr)
	}

	close(release)
	drainAll(t, m, q)
	if m.loading {
		t.Fatal("the landed final checkpoint left the slot loading")
	}
	if len(m.rec.Rows) != 2 {
		t.Fatalf("the final checkpoint is not applied: %+v", m.rec)
	}
	m.discardEdit()
	if m.rec == nil || len(m.rec.Rows) != 2 || m.pendingCount() != 2 {
		t.Fatalf("discarding the rejected draft lost the checkpoint: rows %d pending %d",
			len(m.rec.Rows), m.pendingCount())
	}
}

// TestFailedRecordLoadBlocksReviewActions checks that a failed final
// checkpoint load disables every way of writing the stale rows it leaves
// on screen: the button guard, ⌘Enter and a direct confirmation must not
// reach SaveItemRevision, and the failure stays visible.
func TestFailedRecordLoadBlocksReviewActions(t *testing.T) {
	svc := &fakeService{loads: map[string]*record.Recovery{
		"s1": recovery(reviewRowItem(t, 1, 12)),
	}}
	installReviewSave(svc)
	m, q := newTestModel(t, Config{Service: svc})
	tt := ui.NewTester(m.View, 1024, 700)
	settle(t, m, q, tt)
	m.Apply(snap("s1", 1, true, scan.PhaseCapturing,
		scan.Counts{Captured: 1, Produced: 1}, 1, true, ""))
	drainAll(t, m, q)
	m.page = pageResults
	showRecordItems(m)
	settle(t, m, q, tt)
	if m.rec == nil || len(m.rec.Rows) != 1 || m.recErr != "" {
		t.Fatalf("the record did not load: %+v err %q", m.rec, m.recErr)
	}

	// The next load fails and leaves the last good row on screen.
	svc.mu.Lock()
	svc.loadErr = map[string]error{"s1": errors.New("磁盘读取失败")}
	svc.mu.Unlock()
	m.reloadRecord("s1", true)
	drainAll(t, m, q)
	if m.recErr == "" || m.rec == nil || len(m.rec.Rows) != 1 {
		t.Fatalf("the failed load did not leave its error and stale row: rec %+v err %q",
			m.rec, m.recErr)
	}
	if !m.reviewActionsBlocked() {
		t.Fatal("a failed load does not block review actions")
	}
	m.startEdit()
	if m.edit != nil {
		t.Fatal("a failed load still opens the editor")
	}

	tt.Frame()
	if !tt.HasText("记录读取失败：磁盘读取失败") {
		t.Fatalf("the load failure is not visible: %q", tt.Texts())
	}
	tt.Key(ui.Cmd, ui.KeyEnter)
	m.confirmCurrent()
	m.confirmCurrentStay()
	m.confirmCurrentAction(reviewConfirmNext)
	drainAll(t, m, q)
	if calls := svc.saveCount(); calls != 0 {
		t.Fatalf("a failed load still confirmed the stale row: %+v", svc.saves)
	}
	if !strings.Contains(m.reviewErr, "读取失败") {
		t.Fatalf("a blocked confirmation gave no record-error reason: %q", m.reviewErr)
	}
}
