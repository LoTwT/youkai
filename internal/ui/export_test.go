package ui

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/game/zzz"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/LoTwT/youkai/internal/scan"
	"github.com/egoist/mygo/ui"
)

// exportCall is one recorded save request of the export saver double.
type exportCall struct {
	format app.ExportFormat
	name   string
	data   []byte
}

// fakeExportSaver records every Save call; saveFn may hold the call open
// or return a failure, so the tests can drive duplicate clicks, cancel
// and write errors.
type fakeExportSaver struct {
	mu     sync.Mutex
	calls  []exportCall
	saveFn func(format app.ExportFormat, defaultName string, data []byte) (string, error)
}

func (f *fakeExportSaver) Save(format app.ExportFormat, defaultName string, data []byte) (string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, exportCall{format: format, name: defaultName, data: append([]byte(nil), data...)})
	fn := f.saveFn
	f.mu.Unlock()
	if fn != nil {
		return fn(format, defaultName, data)
	}
	return "/tmp/" + defaultName, nil
}

func (f *fakeExportSaver) snapshot() []exportCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]exportCall(nil), f.calls...)
}

// fakeDirectExportSaver records direct writes. saveFn may hold the write
// open or return an actionable directory failure.
type fakeDirectExportSaver struct {
	mu     sync.Mutex
	calls  []exportCall
	saveFn func(format app.ExportFormat, defaultName string, data []byte) (string, error)
}

func (f *fakeDirectExportSaver) SaveDirect(format app.ExportFormat, defaultName string, data []byte) (string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, exportCall{format: format, name: defaultName, data: append([]byte(nil), data...)})
	fn := f.saveFn
	f.mu.Unlock()
	if fn != nil {
		return fn(format, defaultName, data)
	}
	return "/tmp/" + defaultName, nil
}

func (f *fakeDirectExportSaver) snapshot() []exportCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]exportCall(nil), f.calls...)
}

// uiStructuredItem builds one produced disc with a passed current
// validation and a chosen quality, so the export filter has both usable
// and excluded cases.
func uiStructuredItem(t *testing.T, seq int64, quality record.Quality) record.ItemRecord {
	t.Helper()
	level := 15
	disc := zzz.DiscRecord{
		SetID: intp(31100),
		Slot:  1,
		Level: &level,
		MainStat: zzz.Stat{
			Attribute: zzz.AttrHealth,
			Value:     "2200",
			Unit:      zzz.UnitFlat,
		},
		Substats: []zzz.Substat{
			{Stat: zzz.Stat{Attribute: zzz.AttrCritDamage, Value: "19.2", Unit: zzz.UnitPercent}, EnhancementRolls: intp(3)},
			{Stat: zzz.Stat{Attribute: zzz.AttrAttack, Value: "38", Unit: zzz.UnitFlat}, EnhancementRolls: intp(1)},
		},
	}
	structured := zzz.ToStructuredWithState(disc, zzz.RarityS,
		zzz.CaptureState{ManagementState: zzz.ManagementLocked, EquippedState: zzz.EquippedStateUnequipped}, nil)
	raw, err := json.Marshal(structured)
	if err != nil {
		t.Fatalf("marshal structured: %v", err)
	}
	payload, err := json.Marshal(map[string]json.RawMessage{"lines": json.RawMessage("[]"), "structured": raw})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return record.ItemRecord{Sequence: seq, Status: record.StatusProduced, Quality: quality, Data: payload}
}

// exportModel builds a model showing one saved history record with two
// items: one usable and one still pending review.
func exportModel(t *testing.T, saver ExportSaver) (*Model, *queueUpdater, *fakeService) {
	t.Helper()
	svc := &fakeService{loads: map[string]*record.Recovery{
		"rec": recovery(uiStructuredItem(t, 1, record.QualityAutoAccepted),
			uiStructuredItem(t, 2, record.QualityNeedsReview)),
	}}
	m, q := newTestModel(t, Config{Service: svc, ExportSaver: saver, AppVersion: "test-1"})
	m.reloadRecord("rec", false)
	drainAll(t, m, q)
	if m.rec == nil || m.exportData == nil {
		t.Fatalf("the export snapshot did not load: rec=%v err=%q", m.rec, m.exportBuildErr)
	}
	return m, q, svc
}

func TestExportControlsShowCountsAndSaveBothFormats(t *testing.T) {
	saver := &fakeExportSaver{}
	m, q, _ := exportModel(t, saver)
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)

	want := "已采集 2 件 · 可导出 1 件 · 待校对 1 件 · 失败 0 件 · 未处理 0 件 · 覆盖：无法确认 · 不含截图"
	if !tt.HasText(want) {
		t.Fatalf("export summary missing %q: %v", want, tt.Texts())
	}
	if !tt.HasText("导出 JSON") || !tt.HasText("导出 CSV") {
		t.Fatalf("the two export buttons do not show: %v", tt.Texts())
	}

	if err := tt.Click("导出 JSON"); err != nil {
		t.Fatalf("click 导出 JSON: %v", err)
	}
	drainAll(t, m, q)
	tt.Frame()
	calls := saver.snapshot()
	if len(calls) != 1 || calls[0].format != app.ExportJSON {
		t.Fatalf("JSON export calls = %+v", calls)
	}
	date := m.recData.Checkpoint.Session.StartedAt.Local().Format("2006-01-02")
	if want := "youkai-" + date + "-rec.youkai.json"; calls[0].name != want {
		t.Fatalf("default JSON name = %q, want %q", calls[0].name, want)
	}
	var doc struct {
		Format  string `json:"format"`
		Session struct {
			SavedRevision int64 `json:"savedRevision"`
		} `json:"session"`
		Items []app.ExportItem `json:"items"`
	}
	if err := json.Unmarshal(calls[0].data, &doc); err != nil {
		t.Fatalf("decode exported JSON: %v", err)
	}
	if doc.Format != "youkai.scan" || doc.Session.SavedRevision != 1 || len(doc.Items) != 1 {
		t.Fatalf("exported JSON = format %q revision %d items %d", doc.Format, doc.Session.SavedRevision, len(doc.Items))
	}
	if !tt.HasText("已导出 JSON：/tmp/") {
		t.Fatalf("success notice missing: %v", tt.Texts())
	}

	if err := tt.Click("导出 CSV"); err != nil {
		t.Fatalf("click 导出 CSV: %v", err)
	}
	drainAll(t, m, q)
	calls = saver.snapshot()
	if len(calls) != 2 || calls[1].format != app.ExportCSV {
		t.Fatalf("CSV export calls = %+v", calls)
	}
	if want := "youkai-" + date + "-rec.youkai.csv"; calls[1].name != want {
		t.Fatalf("default CSV name = %q, want %q", calls[1].name, want)
	}
	if !bytes.HasPrefix(calls[1].data, []byte{0xEF, 0xBB, 0xBF}) {
		t.Fatal("exported CSV has no BOM")
	}
	rows, err := csv.NewReader(bytes.NewReader(calls[1].data[3:])).ReadAll()
	if err != nil {
		t.Fatalf("read exported CSV: %v", err)
	}
	if len(rows) != 2 || len(rows[1]) != len(rows[0]) {
		t.Fatalf("exported CSV rows = %d, width = %d", len(rows), len(rows[1]))
	}
}

func TestDirectExportKeepsTheSnapshotWhenTheSlotChanges(t *testing.T) {
	sessionRec := recovery(
		uiStructuredItem(t, 1, record.QualityAutoAccepted),
		uiStructuredItem(t, 2, record.QualityAutoAccepted),
	)
	sessionRec.Checkpoint.Session.SessionID = "s1"
	historyRec := recovery(uiStructuredItem(t, 1, record.QualityAutoAccepted))
	historyRec.Checkpoint.Session.SessionID = "old"

	saver := &fakeDirectExportSaver{}
	started := make(chan struct{})
	release := make(chan struct{})
	saver.saveFn = func(app.ExportFormat, string, []byte) (string, error) {
		close(started)
		<-release
		return "/tmp/out.youkai.json", nil
	}
	svc := &fakeService{loads: map[string]*record.Recovery{"s1": sessionRec}}
	m, q := newTestModel(t, Config{
		Service:           svc,
		PageLayout:        true,
		DirectExportSaver: saver,
		AppVersion:        "test-1",
	})
	m.SetContentSize(1024, 700)
	m.reloadRecord("s1", true)
	drainAll(t, m, q)
	if m.exportData == nil || m.exportData.Summary.Included != 2 {
		t.Fatalf("session snapshot = %+v", m.exportData)
	}

	m.ExportJSON()
	<-started
	m.historySlot.recordID = "old"
	m.applyLoadedRecord(&m.historySlot, "old", historyRec, false)
	m.activateResultSlot(resultSlotHistory)
	if m.rec == nil || m.rec.ID != "old" {
		t.Fatalf("the history slot did not become active: %+v", m.rec)
	}
	close(release)
	drainAll(t, m, q)

	calls := saver.snapshot()
	if len(calls) != 1 {
		t.Fatalf("direct export calls = %+v", calls)
	}
	var doc struct {
		Items []app.ExportItem `json:"items"`
	}
	if err := json.Unmarshal(calls[0].data, &doc); err != nil {
		t.Fatalf("decode the in-flight export: %v", err)
	}
	if len(doc.Items) != 2 {
		t.Fatalf("the switched export wrote %d items, want the session's 2", len(doc.Items))
	}
	if want := app.DefaultExportName(sessionRec.Checkpoint.Session, app.ExportJSON); calls[0].name != want {
		t.Fatalf("in-flight name = %q, want %q", calls[0].name, want)
	}
	if m.exportOutcome == nil || m.exportOutcome.included != 2 ||
		m.exportOutcome.recordID != "s1" || !strings.Contains(m.exportOutcome.text(), "本次扫描") {
		t.Fatalf("the switched export lost its identity: %+v", m.exportOutcome)
	}
}

func TestDirectExportUsesTheHistorySlotsOwnSnapshot(t *testing.T) {
	sessionRec := recovery(
		uiStructuredItem(t, 1, record.QualityAutoAccepted),
		uiStructuredItem(t, 2, record.QualityAutoAccepted),
		uiStructuredItem(t, 3, record.QualityAutoAccepted),
	)
	sessionRec.Checkpoint.Session.SessionID = "s1"
	historyRec := recovery(uiStructuredItem(t, 1, record.QualityAutoAccepted))
	historyRec.Checkpoint.Session.SessionID = "old"
	historyRec.Checkpoint.Session.StartedAt = sessionRec.Checkpoint.Session.StartedAt.Add(-24 * time.Hour)

	saver := &fakeDirectExportSaver{}
	svc := &fakeService{loads: map[string]*record.Recovery{"s1": sessionRec, "old": historyRec}}
	m, q := newTestModel(t, Config{
		Service:           svc,
		PageLayout:        true,
		DirectExportSaver: saver,
		AppVersion:        "test-1",
	})
	m.SetContentSize(1024, 700)
	m.reloadRecord("s1", true)
	drainAll(t, m, q)
	if m.exportData == nil || m.exportData.Summary.Included != 3 {
		t.Fatalf("session snapshot = %+v", m.exportData)
	}

	m.historySlot.recordID = "old"
	m.applyLoadedRecord(&m.historySlot, "old", historyRec, false)
	m.activateResultSlot(resultSlotHistory)
	if m.exportData == nil || m.exportData.Summary.Included != 1 {
		t.Fatalf("history snapshot = %+v", m.exportData)
	}
	m.ExportJSON()
	drainAll(t, m, q)

	calls := saver.snapshot()
	if len(calls) != 1 {
		t.Fatalf("direct history export calls = %+v", calls)
	}
	var doc struct {
		Items []app.ExportItem `json:"items"`
	}
	if err := json.Unmarshal(calls[0].data, &doc); err != nil {
		t.Fatalf("decode the history export: %v", err)
	}
	if len(doc.Items) != 1 {
		t.Fatalf("history export wrote %d items, want 1", len(doc.Items))
	}
	if want := app.DefaultExportName(historyRec.Checkpoint.Session, app.ExportJSON); calls[0].name != want {
		t.Fatalf("history export name = %q, want %q", calls[0].name, want)
	}
	if m.exportOutcome == nil || m.exportOutcome.included != 1 || m.exportOutcome.recordID != "old" {
		t.Fatalf("history export outcome = %+v", m.exportOutcome)
	}
	if m.sessionSlot.state.exportData == nil || m.sessionSlot.state.exportData.Summary.Included != 3 {
		t.Fatalf("the session slot lost its snapshot: %+v", m.sessionSlot.state.exportData)
	}
}

func TestExportBlocksWhileEditingOrScanning(t *testing.T) {
	saver := &fakeExportSaver{}
	m, q, _ := exportModel(t, saver)

	m.selected = 0
	m.startEdit()
	if m.edit == nil {
		t.Fatal("the editor did not open")
	}
	m.edit.draft.Level = "14"
	if reason := m.exportBlockedReason(); !strings.Contains(reason, "未保存的修改") {
		t.Fatalf("dirty editor reason = %q", reason)
	}
	m.ExportJSON()
	drainAll(t, m, q)
	if calls := saver.snapshot(); len(calls) != 0 {
		t.Fatalf("a dirty editor exported: %+v", calls)
	}
	m.edit = nil

	m.rec.Live = true
	m.snap.Busy = true
	if reason := m.exportBlockedReason(); !strings.Contains(reason, "扫描尚未收尾") {
		t.Fatalf("busy scan reason = %q", reason)
	}
	m.ExportCSV()
	drainAll(t, m, q)
	if calls := saver.snapshot(); len(calls) != 0 {
		t.Fatalf("a running scan exported: %+v", calls)
	}
	m.snap.Busy = false
	if reason := m.exportBlockedReason(); reason != "" {
		t.Fatalf("settled record still blocked: %q", reason)
	}
}

func TestExportCancelAndWriteFailureDoNotChangeTheRecord(t *testing.T) {
	saver := &fakeExportSaver{}
	m, q, _ := exportModel(t, saver)
	before := m.recData
	beforeItems := len(before.Checkpoint.Items)
	beforeRevision := before.Checkpoint.SavedRevision

	saver.saveFn = func(app.ExportFormat, string, []byte) (string, error) { return "", nil }
	m.ExportJSON()
	drainAll(t, m, q)
	if !m.exportNoticeContains("已取消导出") {
		t.Fatalf("cancel notice = %q", m.exportNotice)
	}
	if m.recData != before || len(m.recData.Checkpoint.Items) != beforeItems ||
		m.recData.Checkpoint.SavedRevision != beforeRevision {
		t.Fatal("cancel changed the internal record")
	}

	saver.saveFn = func(app.ExportFormat, string, []byte) (string, error) {
		return "", errors.New("磁盘已满")
	}
	m.ExportCSV()
	drainAll(t, m, q)
	if !strings.Contains(m.exportError, "磁盘已满") {
		t.Fatalf("write failure = %q", m.exportError)
	}
	if m.recData != before || len(m.recData.Checkpoint.Items) != beforeItems ||
		m.recData.Checkpoint.SavedRevision != beforeRevision {
		t.Fatal("write failure changed the internal record")
	}
}

func TestExportDisablesDuplicateClicks(t *testing.T) {
	saver := &fakeExportSaver{}
	m, q, _ := exportModel(t, saver)
	started := make(chan struct{})
	release := make(chan struct{})
	saver.saveFn = func(app.ExportFormat, string, []byte) (string, error) {
		close(started)
		<-release
		return "/tmp/out.youkai.json", nil
	}
	m.ExportJSON()
	<-started
	m.ExportJSON()
	m.ExportCSV()
	close(release)
	drainAll(t, m, q)
	if calls := saver.snapshot(); len(calls) != 1 {
		t.Fatalf("duplicate clicks issued %d saves", len(calls))
	}
}

func TestExportSnapshotReloadsTheFinalCheckpoint(t *testing.T) {
	saver := &fakeExportSaver{}
	item := uiStructuredItem(t, 1, record.QualityAutoAccepted)
	pre := recovery(item)
	pre.Checkpoint.Session.Termination = nil
	pre.Checkpoint.Session.EndedAt = nil
	svc := &fakeService{loads: map[string]*record.Recovery{"rec": pre}}
	m, q := newTestModel(t, Config{Service: svc, ExportSaver: saver, AppVersion: "test-1"})

	m.Apply(snap("rec", 1, true, scan.PhaseCapturing, scan.Counts{Captured: 1}, 1, true, ""))
	drainAll(t, m, q)
	if m.recData == nil || m.recData.Checkpoint.Session.Termination != nil {
		t.Fatalf("the pre-final record did not load: %+v", m.recData)
	}

	final := recovery(item)
	svc.loads["rec"] = final
	m.Apply(snap("rec", 2, false, scan.PhaseEnded, scan.Counts{Captured: 1}, 1, false, ""))
	drainAll(t, m, q)
	if m.recData == nil || m.recData.Checkpoint.Session.Termination == nil {
		t.Fatalf("the final checkpoint was not reloaded: %+v", m.recData)
	}
	if m.exportData == nil {
		t.Fatalf("the final export snapshot is missing: %q", m.exportBuildErr)
	}
	data, err := m.exportData.JSON(time.Now().UTC(), "test-1")
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	var doc struct {
		Session struct {
			Termination struct {
				Kind string `json:"kind"`
			} `json:"termination"`
		} `json:"session"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	if doc.Session.Termination.Kind != string(record.TerminationCompleted) {
		t.Fatalf("export termination = %q, want completed", doc.Session.Termination.Kind)
	}
}

func TestExportFollowsASavedRevision(t *testing.T) {
	saver := &fakeExportSaver{}
	m, q, _ := exportModel(t, saver)

	confirmed := uiStructuredItem(t, 2, record.QualityHumanConfirmed)
	confirmed.Revision = 1
	m.applySavedItem(confirmed, 2)
	if m.exportData == nil || m.exportData.Summary.Included != 2 || m.exportData.Summary.Excluded != 0 {
		t.Fatalf("export snapshot after save = %+v", m.exportData)
	}
	if m.recData.Checkpoint.SavedRevision != 2 {
		t.Fatalf("saved revision = %d, want 2", m.recData.Checkpoint.SavedRevision)
	}
	m.ExportJSON()
	drainAll(t, m, q)
	calls := saver.snapshot()
	if len(calls) != 1 {
		t.Fatalf("export calls = %+v", calls)
	}
	var doc struct {
		Session struct {
			SavedRevision int64 `json:"savedRevision"`
		} `json:"session"`
		Items []app.ExportItem `json:"items"`
	}
	if err := json.Unmarshal(calls[0].data, &doc); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	if doc.Session.SavedRevision != 2 || len(doc.Items) != 2 {
		t.Fatalf("exported revision %d with %d items", doc.Session.SavedRevision, len(doc.Items))
	}
}

func TestDirectExportIsNotLimitedByTheVisibleFilter(t *testing.T) {
	saver := &fakeDirectExportSaver{}
	m, q := directExportModel(t, saver, recovery(
		uiStructuredItem(t, 1, record.QualityAutoAccepted),
		uiStructuredItem(t, 2, record.QualityAutoAccepted),
		uiStructuredItem(t, 3, record.QualityNeedsReview),
	), false)
	if m.filter != resultFilterPending {
		t.Fatalf("default filter = %v, want pending", m.filter)
	}
	if visible := len(m.visibleResultRows()); visible != 1 {
		t.Fatalf("visible pending rows = %d, want 1", visible)
	}
	if m.exportData.Summary.Included != 2 {
		t.Fatalf("exportable set = %d, want 2", m.exportData.Summary.Included)
	}

	m.ExportCSV()
	drainAll(t, m, q)
	calls := saver.snapshot()
	if len(calls) != 1 {
		t.Fatalf("direct export calls = %+v", calls)
	}
	rows, err := csv.NewReader(bytes.NewReader(calls[0].data[3:])).ReadAll()
	if err != nil {
		t.Fatalf("read the filtered export: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("filtered CSV rows = %d, want header + 2 exportable items", len(rows))
	}
	if m.exportOutcome == nil || m.exportOutcome.included != 2 {
		t.Fatalf("filtered export outcome = %+v", m.exportOutcome)
	}
}

func TestDirectExportDisablesReviewActions(t *testing.T) {
	saver := &fakeDirectExportSaver{}
	started := make(chan struct{})
	release := make(chan struct{})
	saver.saveFn = func(app.ExportFormat, string, []byte) (string, error) {
		close(started)
		<-release
		return "/tmp/out.youkai.json", nil
	}
	m, q := directExportModel(t, saver, recovery(
		uiStructuredItem(t, 1, record.QualityAutoAccepted),
		uiStructuredItem(t, 2, record.QualityNeedsReview),
	), true)
	m.selected = 0

	m.ExportJSON()
	<-started
	m.startEdit()
	if m.edit != nil {
		t.Fatal("an export in flight allowed the editor to open")
	}
	m.confirmCurrent()
	if !strings.Contains(m.reviewErr, "正在导出") {
		t.Fatalf("confirmation during export = %q", m.reviewErr)
	}
	tt := ui.NewTester(m.View, 1024, 700)
	tt.Frame()
	if !tt.HasText("正在导出……") || !tt.HasText("正在导出，完成前不能编辑或确认。") {
		t.Fatalf("the export busy state is not shown: %q", tt.Texts())
	}

	close(release)
	drainAll(t, m, q)
	if m.exporting {
		t.Fatal("the export did not settle")
	}
}

// exportNoticeContains is a small readability helper for the cancellation
// assertion.
func (m *Model) exportNoticeContains(part string) bool {
	return strings.Contains(m.exportNotice, part)
}

// directExportModel loads one record into the page shell with a direct
// saver, ready to export.
func directExportModel(t *testing.T, saver *fakeDirectExportSaver, rec *record.Recovery, live bool) (*Model, *queueUpdater) {
	t.Helper()
	id := rec.Checkpoint.Session.SessionID
	svc := &fakeService{loads: map[string]*record.Recovery{id: rec}}
	m, q := newTestModel(t, Config{
		Service:           svc,
		PageLayout:        true,
		DirectExportSaver: saver,
		AppVersion:        "test-1",
	})
	m.SetContentSize(1024, 700)
	m.reloadRecord(id, live)
	drainAll(t, m, q)
	if m.exportData == nil {
		t.Fatalf("the direct export snapshot did not load: %v", m.exportBuildErr)
	}
	return m, q
}

func TestDirectExportDirectoryFailureOffersActions(t *testing.T) {
	saver := &fakeDirectExportSaver{}
	saver.saveFn = func(app.ExportFormat, string, []byte) (string, error) {
		return "", &ExportDirectoryError{Directory: "/missing/Downloads", Err: errors.New("permission denied")}
	}
	opened := 0
	svc := &fakeService{loads: map[string]*record.Recovery{
		"rec": recovery(uiStructuredItem(t, 1, record.QualityAutoAccepted)),
	}}
	m, q := newTestModel(t, Config{
		Service:           svc,
		PageLayout:        true,
		DirectExportSaver: saver,
		OpenDownloadsDirectory: func() error {
			opened++
			return nil
		},
	})
	m.SetContentSize(1024, 700)
	m.reloadRecord("rec", false)
	drainAll(t, m, q)
	tt := ui.NewTester(m.View, 1024, 700)
	settle(t, m, q, tt)

	if err := tt.Click("导出 CSV"); err != nil {
		t.Fatalf("click 导出 CSV: %v", err)
	}
	drainAll(t, m, q)
	tt.Frame()
	if m.exportOutcome == nil || m.exportOutcome.dirErr == nil {
		t.Fatalf("the directory failure was not classified: %+v", m.exportOutcome)
	}
	if !tt.HasText("导出失败（CSV，历史记录 rec）") || !tt.HasText("打开目录") || !tt.HasText("去设置") {
		t.Fatalf("actionable export failure missing: %q", tt.Texts())
	}
	if err := tt.Click("打开目录"); err != nil {
		t.Fatalf("click 打开目录: %v", err)
	}
	if opened != 1 {
		t.Fatalf("open directory calls = %d, want 1", opened)
	}
	if err := tt.Click("去设置"); err != nil {
		t.Fatalf("click 去设置: %v", err)
	}
	if m.page != pageSettings {
		t.Fatalf("page after 去设置 = %v, want settings", m.page)
	}
	if m.recData == nil || len(m.recData.Checkpoint.Items) != 1 {
		t.Fatal("a failed direct export changed the record")
	}
}

func TestDirectExportWritesBothFormatsToTheFrozenSnapshot(t *testing.T) {
	saver := &fakeDirectExportSaver{}
	m, q := directExportModel(t, saver, recovery(
		uiStructuredItem(t, 1, record.QualityAutoAccepted),
		uiStructuredItem(t, 2, record.QualityAutoAccepted),
	), true)
	tt := ui.NewTester(m.View, 1024, 700)
	settle(t, m, q, tt)

	if err := tt.Click("导出 JSON"); err != nil {
		t.Fatalf("click 导出 JSON: %v", err)
	}
	drainAll(t, m, q)
	tt.Frame()
	calls := saver.snapshot()
	if len(calls) != 1 || calls[0].format != app.ExportJSON {
		t.Fatalf("JSON direct export calls = %+v", calls)
	}
	if want := app.DefaultExportName(m.recData.Checkpoint.Session, app.ExportJSON); calls[0].name != want {
		t.Fatalf("default JSON name = %q, want %q", calls[0].name, want)
	}
	var doc struct {
		Items []app.ExportItem `json:"items"`
	}
	if err := json.Unmarshal(calls[0].data, &doc); err != nil {
		t.Fatalf("decode exported JSON: %v", err)
	}
	if len(doc.Items) != 2 {
		t.Fatalf("exported JSON items = %d, want 2", len(doc.Items))
	}
	if m.exportOutcome == nil || m.exportOutcome.err != nil ||
		m.exportOutcome.included != 2 || m.exportOutcome.recordID != "rec" {
		t.Fatalf("direct JSON outcome = %+v", m.exportOutcome)
	}
	if !tt.HasText("已导出 JSON（2 件，本次扫描）") {
		t.Fatalf("direct export status missing: %q", tt.Texts())
	}

	if err := tt.Click("导出 CSV"); err != nil {
		t.Fatalf("click 导出 CSV: %v", err)
	}
	drainAll(t, m, q)
	calls = saver.snapshot()
	if len(calls) != 2 || calls[1].format != app.ExportCSV {
		t.Fatalf("CSV direct export calls = %+v", calls)
	}
	if want := app.DefaultExportName(m.recData.Checkpoint.Session, app.ExportCSV); calls[1].name != want {
		t.Fatalf("default CSV name = %q, want %q", calls[1].name, want)
	}
	if !bytes.HasPrefix(calls[1].data, []byte{0xEF, 0xBB, 0xBF}) {
		t.Fatal("directly exported CSV has no BOM")
	}
	rows, err := csv.NewReader(bytes.NewReader(calls[1].data[3:])).ReadAll()
	if err != nil {
		t.Fatalf("read directly exported CSV: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("direct CSV rows = %d, want header + 2 items", len(rows))
	}
}
