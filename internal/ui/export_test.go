package ui

import (
	"bytes"
	"context"
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

// fakeDirectExportSaver records direct writes. saveFn may hold the write
// open or return an actionable directory failure; saveCtxFn drives the
// dialog's cancellation path.
type fakeDirectExportSaver struct {
	mu        sync.Mutex
	calls     []exportCall
	saveFn    func(format app.ExportFormat, defaultName string, data []byte) (string, error)
	saveCtxFn func(ctx context.Context, format app.ExportFormat, defaultName string, data []byte) (string, error)
}

func (f *fakeDirectExportSaver) SaveDirect(format app.ExportFormat, defaultName string, data []byte) (string, error) {
	return f.save(nil, format, defaultName, data)
}

// SaveDirectContext gives the dialog a cancelable writer. A canceled save
// returns the context error and no path, as the production saver does.
func (f *fakeDirectExportSaver) SaveDirectContext(ctx context.Context, format app.ExportFormat, defaultName string, data []byte) (string, error) {
	return f.save(ctx, format, defaultName, data)
}

func (f *fakeDirectExportSaver) save(ctx context.Context, format app.ExportFormat, defaultName string, data []byte) (string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, exportCall{format: format, name: defaultName, data: append([]byte(nil), data...)})
	fn, ctxFn := f.saveFn, f.saveCtxFn
	f.mu.Unlock()
	if ctx != nil && ctxFn != nil {
		return ctxFn(ctx, format, defaultName, data)
	}
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

// exportDialogFixture loads one saved record with a direct saver and a
// tester at the design's default window size.
type exportDialogFixture struct {
	m     *Model
	q     *queueUpdater
	tt    *ui.Tester
	svc   *fakeService
	saver *fakeDirectExportSaver
}

func newExportDialogFixture(t *testing.T, rec *record.Recovery, live bool) *exportDialogFixture {
	t.Helper()
	saver := &fakeDirectExportSaver{}
	id := rec.Checkpoint.Session.SessionID
	svc := &fakeService{loads: map[string]*record.Recovery{id: rec}}
	m, q := newTestModel(t, Config{
		Service:           svc,
		DirectExportSaver: saver,
		AppVersion:        "test-1",
	})
	m.SetContentSize(1180, 760)
	m.showRecord(id, live)
	m.reloadRecord(id, live)
	drainAll(t, m, q)
	m.page = pageResults
	tt := ui.NewTester(m.View, 1180, 760)
	settle(t, m, q, tt)
	if m.exportData == nil {
		t.Fatalf("the export snapshot did not load: %v", m.exportBuildErr)
	}
	return &exportDialogFixture{m: m, q: q, tt: tt, svc: svc, saver: saver}
}

// open shows the dialog through the model's entry and redraws.
func (f *exportDialogFixture) open(t *testing.T) {
	t.Helper()
	f.m.requestExport()
	f.tt.Frame()
	if !f.m.exportDialog.open {
		t.Fatalf("the export dialog did not open: %q", f.tt.Texts())
	}
}

// TestExportDialogShowsCountsFormatDifferenceAndSuccess covers S8's
// 可导出 and 成功 states, the equal-weight format buttons, the opening
// focus, and the print/download actions of the success state.
func TestExportDialogShowsCountsFormatDifferenceAndSuccess(t *testing.T) {
	rec := recovery(
		uiStructuredItem(t, 1, record.QualityAutoAccepted),
		uiStructuredItem(t, 2, record.QualityAutoAccepted),
		uiStructuredItem(t, 3, record.QualityNeedsReview),
	)
	f := newExportDialogFixture(t, rec, false)
	opened := 0
	f.m.cfg.OpenDownloadsDirectory = func() error {
		opened++
		return nil
	}
	if err := f.tt.Click("导出…"); err != nil {
		t.Fatalf("click the title-bar entry: %v", err)
	}
	f.tt.Frame()
	if !f.m.exportDialog.open {
		t.Fatalf("the title-bar entry did not open the dialog: %q", f.tt.Texts())
	}

	if !f.tt.HasText("导出这条记录") {
		t.Fatalf("the dialog title is missing: %q", f.tt.Texts())
	}
	for _, want := range []string{
		"纳入", "2 件", "排除", "1 件", "待处理 1",
		"覆盖", "无法确认", "不含截图",
		"写入位置", "系统下载目录",
		"JSON：带来源扫描、覆盖结论、筛选方式、排除数量和目录。CSV：一行一件的表格，不含这些信息。两者来自同一已保存修订。",
		"导出 JSON", "导出 CSV", "去校对",
	} {
		if !f.tt.HasText(want) {
			t.Fatalf("the dialog is missing %q: %q", want, f.tt.Texts())
		}
	}
	if !f.tt.Focused("导出 JSON") {
		t.Fatalf("the opening focus is not 导出 JSON: %q", f.tt.Texts())
	}
	if calls := f.saver.snapshot(); len(calls) != 0 {
		t.Fatalf("opening the dialog exported: %+v", calls)
	}

	if err := f.tt.Click("导出 JSON"); err != nil {
		t.Fatalf("click 导出 JSON: %v", err)
	}
	settle(t, f.m, f.q, f.tt)
	calls := f.saver.snapshot()
	if len(calls) != 1 || calls[0].format != app.ExportJSON {
		t.Fatalf("JSON export calls = %+v", calls)
	}
	date := f.m.recData.Checkpoint.Session.StartedAt.Local().Format("2006-01-02")
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
	if doc.Format != "youkai.scan" || doc.Session.SavedRevision != 1 || len(doc.Items) != 2 {
		t.Fatalf("exported JSON = format %q revision %d items %d", doc.Format, doc.Session.SavedRevision, len(doc.Items))
	}
	if !f.tt.HasText("已导出 2 件") || !f.tt.HasText("打开下载目录") ||
		!f.tt.HasText("完成") || !f.tt.HasText("rec.youkai.json") {
		t.Fatalf("the success state is incomplete: %q", f.tt.Texts())
	}
	if f.tt.HasText("删除这条记录的截图…") {
		t.Fatal("a record with a pending item offered screenshot deletion")
	}
	if err := f.tt.Click("打开下载目录"); err != nil {
		t.Fatalf("click 打开下载目录: %v", err)
	}
	if opened != 1 {
		t.Fatalf("open downloads calls = %d, want 1", opened)
	}
	if err := f.tt.Click("完成"); err != nil {
		t.Fatalf("click 完成: %v", err)
	}
	f.tt.Frame()
	if f.m.exportDialog.open {
		t.Fatal("完成 did not close the dialog")
	}
}

// TestExportDialogTabOrder checks 08 §4's forward Tab order: the two format
// buttons, then the review link, then close. MyGo builds the close and
// review controls last (attached to their visible places), so the layout
// stays on the wireframe while the focus order follows the spec.
func TestExportDialogTabOrder(t *testing.T) {
	rec := recovery(
		uiStructuredItem(t, 1, record.QualityAutoAccepted),
		uiStructuredItem(t, 2, record.QualityNeedsReview),
	)
	f := newExportDialogFixture(t, rec, false)
	f.open(t)

	if !f.tt.Focused("导出 JSON") {
		t.Fatalf("the dialog did not open on 导出 JSON: %q", f.tt.Texts())
	}
	for _, want := range []string{"导出 CSV", "去校对", "关闭导出对话框"} {
		f.tt.Key(0, ui.KeyTab)
		if !f.tt.Focused(want) {
			t.Fatalf("Tab did not reach %q: %q", want, f.tt.Texts())
		}
	}
	f.tt.Key(0, ui.KeyEnter)
	f.tt.Frame()
	if f.m.exportDialog.open {
		t.Fatal("the attached close button did not close the dialog")
	}
}

// TestExportDialogExcludedReasonsExpandOnDemand covers 08 §4's on-demand
// exclusion breakdown: the reasons stay out of the way until asked for.
func TestExportDialogExcludedReasonsExpandOnDemand(t *testing.T) {
	rec := recovery(
		uiStructuredItem(t, 1, record.QualityAutoAccepted),
		uiStructuredItem(t, 2, record.QualityNeedsReview),
	)
	f := newExportDialogFixture(t, rec, false)
	f.open(t)

	if f.tt.HasText("待校对：1 件") {
		t.Fatalf("the exclusion reasons start expanded: %q", f.tt.Texts())
	}
	if err := f.tt.Click("排除原因明细"); err != nil {
		t.Fatal(err)
	}
	f.tt.Frame()
	if !f.tt.HasText("待校对：1 件") {
		t.Fatalf("the exclusion reasons did not expand: %q", f.tt.Texts())
	}
	if err := f.tt.Click("排除原因明细"); err != nil {
		t.Fatal(err)
	}
	f.tt.Advance(250 * time.Millisecond)
	if f.tt.HasText("待校对：1 件") {
		t.Fatalf("the exclusion reasons did not collapse: %q", f.tt.Texts())
	}
}

// TestExportDialogFitsTheNarrowWindow checks that the whole pre-export
// content stays inside the design's minimum window, so the format choice
// is never clipped at 960×640.
func TestExportDialogFitsTheNarrowWindow(t *testing.T) {
	rec := recovery(
		uiStructuredItem(t, 1, record.QualityAutoAccepted),
		uiStructuredItem(t, 2, record.QualityNeedsReview),
	)
	f := newExportDialogFixture(t, rec, false)
	f.tt.SetSize(960, 640)
	f.m.SetContentSize(960, 640)
	f.tt.Frame()
	f.open(t)
	for _, label := range []string{
		"导出这条记录", "纳入", "排除", "覆盖", "不含截图",
		"写入位置", "去校对", "排除原因明细", "导出 JSON", "导出 CSV",
		"关闭导出对话框",
		"JSON：带来源扫描、覆盖结论、筛选方式、排除数量和目录。" +
			"CSV：一行一件的表格，不含这些信息。两者来自同一已保存修订。",
	} {
		rect, ok := f.tt.Find(label)
		if !ok {
			t.Fatalf("%q is not visible at 960×640: %q", label, f.tt.Texts())
		}
		if rect.X < 0 || rect.Y < 0 || rect.X+rect.W > 960 || rect.Y+rect.H > 640 {
			t.Fatalf("%q lies outside 960×640: %+v", label, rect)
		}
	}
}

// TestExportDialogEscClosesWhenIdle checks the ordinary Escape dismissal.
func TestExportDialogEscClosesWhenIdle(t *testing.T) {
	rec := recovery(uiStructuredItem(t, 1, record.QualityAutoAccepted))
	f := newExportDialogFixture(t, rec, false)
	f.open(t)
	f.tt.Key(0, ui.KeyEscape)
	settle(t, f.m, f.q, f.tt)
	if f.m.exportDialog.open {
		t.Fatalf("Esc did not close the idle dialog: %q", f.tt.Texts())
	}
	if f.tt.HasText("导出这条记录") {
		t.Fatalf("the closed dialog still renders: %q", f.tt.Texts())
	}
}

// TestExportDialogAttachedControlsStayInPlace guards the placeholder and
// AttachTo layout: dropping either attachment moves the control to the
// bottom of the dialog instead of its wireframe row.
func TestExportDialogAttachedControlsStayInPlace(t *testing.T) {
	for _, size := range []struct {
		name string
		w, h int
	}{{"1180x760", 1180, 760}, {"960x640", 960, 640}} {
		t.Run(size.name, func(t *testing.T) {
			rec := recovery(
				uiStructuredItem(t, 1, record.QualityAutoAccepted),
				uiStructuredItem(t, 2, record.QualityNeedsReview),
			)
			f := newExportDialogFixture(t, rec, false)
			f.tt.SetSize(size.w, size.h)
			f.m.SetContentSize(size.w, size.h)
			f.tt.Frame()
			f.open(t)

			review, ok := f.tt.Find("去校对")
			if !ok {
				t.Fatalf("去校对 is missing: %q", f.tt.Texts())
			}
			excluded, ok := f.tt.Find("排除")
			if !ok {
				t.Fatalf("排除 is missing: %q", f.tt.Texts())
			}
			if centerY(review)-centerY(excluded) > 2 || centerY(excluded)-centerY(review) > 2 {
				t.Fatalf("去校对 is not on the exclusion row: review %+v, 排除 %+v", review, excluded)
			}
			closeRect, ok := f.tt.Find("关闭导出对话框")
			if !ok {
				t.Fatalf("关闭导出对话框 is missing: %q", f.tt.Texts())
			}
			title, ok := f.tt.Find("导出这条记录")
			if !ok {
				t.Fatalf("导出这条记录 is missing: %q", f.tt.Texts())
			}
			if centerY(closeRect)-centerY(title) > 2 || centerY(title)-centerY(closeRect) > 2 {
				t.Fatalf("close is not on the title row: close %+v, title %+v", closeRect, title)
			}
			if review.X <= excluded.X || closeRect.X <= title.X {
				t.Fatalf("attached controls are not to the right of their rows: review %+v, close %+v",
					review, closeRect)
			}
		})
	}
}

func centerY(r ui.Rect) float32 { return r.Y + r.H/2 }

// TestExportDialogCloseButtonWorks checks the attached close button is
// clickable where the wireframe draws it.
func TestExportDialogCloseButtonWorks(t *testing.T) {
	f := newExportDialogFixture(t, recovery(uiStructuredItem(t, 1, record.QualityAutoAccepted)), false)
	f.open(t)
	if err := f.tt.Click("关闭导出对话框"); err != nil {
		t.Fatal(err)
	}
	f.tt.Frame()
	if f.m.exportDialog.open {
		t.Fatal("the clickable close button did not close the dialog")
	}
}

// TestRowExportDoesNotFollowAPageSwitch checks that a record load started
// by the records list opens its dialog only while the workspace is still
// the destination.
func TestRowExportDoesNotFollowAPageSwitch(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	loads := map[string]*record.Recovery{
		"r01": recovery(uiStructuredItem(t, 1, record.QualityAutoAccepted)),
	}
	svc := &fakeService{history: historyTableRecords(1), loads: loads}
	svc.loadFn = func(id string) (*record.Recovery, error) {
		if id == "r01" {
			started <- struct{}{}
			<-release
		}
		return loads[id], nil
	}
	m, q := newTestModel(t, Config{
		Service:           svc,
		DirectExportSaver: &fakeDirectExportSaver{},
		AppVersion:        "test",
	})
	tt := ui.NewTester(m.View, 1180, 760)
	settle(t, m, q, tt)
	openRecordsList(t, m, q, tt)

	m.openHistoryMenu("r01")
	tt.Frame()
	if err := tt.Click("导出…"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("the row export did not start loading")
	}
	m.navigatePage(pageSettings)
	if m.exportAfterLoadID != "" {
		t.Fatalf("leaving the workspace kept the deferred export: %q", m.exportAfterLoadID)
	}
	close(release)
	settle(t, m, q, tt)
	if m.page != pageSettings || m.exportDialog.open {
		t.Fatalf("the load opened its dialog over another page: page %v open=%t",
			m.page, m.exportDialog.open)
	}
}

// TestExportDialogBlocksPageShortcuts checks that the modal owns the
// keyboard while it shows: F6 must not move focus to the page behind it.
func TestExportDialogBlocksPageShortcuts(t *testing.T) {
	rec := recovery(
		uiStructuredItem(t, 1, record.QualityAutoAccepted),
		uiStructuredItem(t, 2, record.QualityNeedsReview),
	)
	f := newExportDialogFixture(t, rec, false)
	f.open(t)
	if !f.tt.Focused("导出 JSON") {
		t.Fatalf("the dialog did not open on 导出 JSON: %q", f.tt.Texts())
	}
	f.tt.Key(0, ui.KeyF6)
	if !f.tt.Focused("导出 JSON") {
		t.Fatalf("F6 moved the focus behind the dialog: %q", f.tt.Texts())
	}
	selected := f.m.selected
	f.tt.Key(ui.Cmd, ui.KeyJ)
	if f.m.selected != selected {
		t.Fatalf("Ctrl+J moved the result selection behind the dialog: %d -> %d",
			selected, f.m.selected)
	}
}

// TestCoverageTextUsesTheDesignVocabulary keeps complete coverage called
// 全部, never 完整 (09 §5.2).
func TestCoverageTextUsesTheDesignVocabulary(t *testing.T) {
	if got := coverageText(record.CoverageComplete); got != "全部" {
		t.Fatalf("complete coverage = %q, want 全部", got)
	}
	if got := coverageText(record.CoveragePartial); got != "部分" {
		t.Fatalf("partial coverage = %q, want 部分", got)
	}
	if got := coverageText(record.CoverageUnknown); got != "无法确认" {
		t.Fatalf("unknown coverage = %q, want 无法确认", got)
	}
}

// TestExportDialogShowsPartialCoverageWarning covers the C-27 notice.
func TestExportDialogShowsPartialCoverageWarning(t *testing.T) {
	rec := recovery(uiStructuredItem(t, 1, record.QualityAutoAccepted))
	rec.Checkpoint.Session.Coverage = record.Coverage{Status: record.CoveragePartial}
	f := newExportDialogFixture(t, rec, false)
	f.open(t)
	want := "这是部分扫描的结果，不是完整库存，不能据此删除没出现的驱动盘。"
	if !f.tt.HasText(want) {
		t.Fatalf("the C-27 notice is missing: %q", f.tt.Texts())
	}

	complete := recovery(uiStructuredItem(t, 1, record.QualityAutoAccepted))
	complete.Checkpoint.Session.Coverage = record.Coverage{Status: record.CoverageComplete}
	complete.Checkpoint.Session.ObservedScope = []byte(`{"kind":"zzzInventory","sort":"verified","coverage":"complete"}`)
	g := newExportDialogFixture(t, complete, false)
	g.open(t)
	if g.tt.HasText(want) {
		t.Fatalf("a complete record showed the partial notice: %q", g.tt.Texts())
	}
}

// TestExportDialogEmptyStateFocusesReviewAndWritesAnEmptyFile covers
// S8's 可导出数为零 state: the title states the reason, focus starts on
// 去校对, and both formats still write an empty file (F-22).
func TestExportDialogEmptyStateFocusesReviewAndWritesAnEmptyFile(t *testing.T) {
	rec := recovery(uiStructuredItem(t, 1, record.QualityNeedsReview))
	f := newExportDialogFixture(t, rec, false)
	f.open(t)

	if !f.tt.HasText("没有可导出的件：还有 1 件待处理") {
		t.Fatalf("the empty-state title does not state the reason: %q", f.tt.Texts())
	}
	if !f.tt.Focused("去校对") {
		t.Fatalf("the zero-exportable state did not start on 去校对: %q", f.tt.Texts())
	}
	if !f.tt.HasText("导出 JSON") || !f.tt.HasText("导出 CSV") {
		t.Fatalf("the empty state hid the format buttons: %q", f.tt.Texts())
	}

	if err := f.tt.Click("导出 JSON"); err != nil {
		t.Fatalf("click 导出 JSON: %v", err)
	}
	settle(t, f.m, f.q, f.tt)
	calls := f.saver.snapshot()
	if len(calls) != 1 {
		t.Fatalf("empty export calls = %+v", calls)
	}
	var doc struct {
		Items []app.ExportItem `json:"items"`
	}
	if err := json.Unmarshal(calls[0].data, &doc); err != nil {
		t.Fatalf("decode the empty export: %v", err)
	}
	if len(doc.Items) != 0 {
		t.Fatalf("the empty export wrote %d items", len(doc.Items))
	}
	if !f.tt.HasText("已导出 0 件") {
		t.Fatalf("the empty success state is missing: %q", f.tt.Texts())
	}
	if f.tt.HasText("删除这条记录的截图…") {
		t.Fatal("a record with a pending item offered screenshot deletion")
	}

	f.tt.Click("完成")
	f.tt.Frame()
	f.open(t)
	if err := f.tt.Click("去校对"); err != nil {
		t.Fatalf("click 去校对: %v", err)
	}
	settle(t, f.m, f.q, f.tt)
	if f.m.exportDialog.open || f.m.recordTab != recordTabItems ||
		f.m.filter != resultFilterPending {
		t.Fatalf("去校对 left dialog=%t tab=%v filter=%v",
			f.m.exportDialog.open, f.m.recordTab, f.m.filter)
	}
}

// TestExportDialogCountsComeFromTheExportClassification pins the warning
// count that #123's four-class partition provides.
func TestExportDialogCountsComeFromTheExportClassification(t *testing.T) {
	rec := recovery(uiStructuredItem(t, 1, record.QualityAutoAccepted))
	f := newExportDialogFixture(t, rec, false)
	f.m.exportData.Summary.Counts.Warning = 3
	f.open(t)
	if !f.tt.HasText("（含 3 件有警告）") {
		t.Fatalf("the warning count is missing: %q", f.tt.Texts())
	}
}

// TestExportDialogAsksForUnsavedChangesFirst checks that a dirty review
// draft keeps the dialog closed until the ordinary prompt settles it.
func TestExportDialogAsksForUnsavedChangesFirst(t *testing.T) {
	rec := recovery(
		uiStructuredItem(t, 1, record.QualityAutoAccepted),
		uiStructuredItem(t, 2, record.QualityNeedsReview),
	)
	f := newExportDialogFixture(t, rec, false)
	showRecordItems(f.m)
	f.tt.Frame()
	if f.m.edit == nil {
		t.Fatal("the editor did not open")
	}
	f.m.edit.draft.Level = "14"
	f.tt.Frame()

	// The click path is covered by the clean fixture above; requestExport
	// is the one entry the title bar and Ctrl+E share.
	f.m.requestExport()
	f.tt.Frame()
	if f.m.exportDialog.open {
		t.Fatal("the export dialog opened over a dirty draft")
	}
	if f.m.navPrompt == nil || !f.tt.HasText("第 2 件有未保存的修改") {
		t.Fatalf("the unsaved-changes prompt did not appear: %q", f.tt.Texts())
	}
	if err := f.tt.Click("放弃修改"); err != nil {
		t.Fatal(err)
	}
	f.tt.Frame()
	if !f.m.exportDialog.open {
		t.Fatalf("the dialog did not open after discarding the draft: %q", f.tt.Texts())
	}
}

// TestExportDialogShowsTheBlockedReason covers S8's 被阻止 state.
func TestExportDialogShowsTheBlockedReason(t *testing.T) {
	rec := recovery(uiStructuredItem(t, 1, record.QualityAutoAccepted))
	f := newExportDialogFixture(t, rec, true)
	f.m.snap.Busy = true
	f.open(t)
	if !f.tt.HasText("扫描尚未收尾，结束后可导出已保存的固定修订。") {
		t.Fatalf("the blocked reason is missing: %q", f.tt.Texts())
	}
	if err := f.tt.Click("导出 JSON"); err != nil {
		t.Fatalf("click blocked 导出 JSON: %v", err)
	}
	if calls := f.saver.snapshot(); len(calls) != 0 {
		t.Fatalf("a blocked dialog exported: %+v", calls)
	}

	f.m.snap.Busy = false
	f.tt.Frame()
	if err := f.tt.Click("导出 JSON"); err != nil {
		t.Fatalf("click 导出 JSON after unblocking: %v", err)
	}
	settle(t, f.m, f.q, f.tt)
	if calls := f.saver.snapshot(); len(calls) != 1 {
		t.Fatalf("the unblocked dialog exported %d files", len(calls))
	}

	// An unsaved live session is the other S8 blocked case.
	g := newExportDialogFixture(t, recovery(uiStructuredItem(t, 1, record.QualityAutoAccepted)), true)
	g.m.snap.SavePending = true
	g.open(t)
	if !g.tt.HasText("扫描结果尚未保存，保存完成后才能导出。") {
		t.Fatalf("the unsaved-session block is missing: %q", g.tt.Texts())
	}
	if err := g.tt.Click("导出 JSON"); err != nil {
		t.Fatalf("click blocked 导出 JSON: %v", err)
	}
	if calls := g.saver.snapshot(); len(calls) != 0 {
		t.Fatalf("an unsaved session exported: %+v", calls)
	}

	// A review save in flight is the third S8 blocked case.
	h := newExportDialogFixture(t, recovery(uiStructuredItem(t, 1, record.QualityAutoAccepted)), false)
	h.m.reviewSaving = true
	h.open(t)
	if !h.tt.HasText("正在保存修改，完成前不能导出。") {
		t.Fatalf("the review-save block is missing: %q", h.tt.Texts())
	}
	if err := h.tt.Click("导出 CSV"); err != nil {
		t.Fatalf("click blocked 导出 CSV: %v", err)
	}
	if calls := h.saver.snapshot(); len(calls) != 0 {
		t.Fatalf("a review save in flight exported: %+v", calls)
	}
}

// TestExportDialogRunsCancelsAndReexports covers S8's 导出中 and 已取消
// states: progress replaces the formats, Esc cannot close, 取消 leaves no
// file, and the dialog returns to its pre-export content.
func TestExportDialogRunsCancelsAndReexports(t *testing.T) {
	rec := recovery(uiStructuredItem(t, 1, record.QualityAutoAccepted))
	f := newExportDialogFixture(t, rec, false)
	started := make(chan struct{})
	var once sync.Once
	f.saver.saveCtxFn = func(ctx context.Context, format app.ExportFormat, name string, data []byte) (string, error) {
		once.Do(func() { close(started) })
		<-ctx.Done()
		return "", ctx.Err()
	}
	f.open(t)
	if err := f.tt.Click("导出 JSON"); err != nil {
		t.Fatalf("click 导出 JSON: %v", err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("the export write did not start")
	}
	f.tt.Frame()
	if !f.tt.HasText("正在导出 JSON……") || !f.tt.HasText("取消") {
		t.Fatalf("the export progress is missing: %q", f.tt.Texts())
	}
	if reason := f.m.cleanupBlockedReason(); !strings.Contains(reason, "正在导出") {
		t.Fatalf("cleanup during export = %q", reason)
	}

	// Esc is ignored while the write is in flight.
	f.tt.Key(0, ui.KeyEscape)
	f.tt.Frame()
	if !f.m.exportDialog.open || !f.tt.HasText("正在导出 JSON……") {
		t.Fatalf("Esc closed the exporting dialog: %q", f.tt.Texts())
	}

	if err := f.tt.Click("取消"); err != nil {
		t.Fatalf("click 取消: %v", err)
	}
	settle(t, f.m, f.q, f.tt)
	if f.m.exporting {
		t.Fatal("the canceled export stayed busy")
	}
	if f.m.exportOutcome != nil {
		t.Fatalf("a canceled export left an outcome: %+v", f.m.exportOutcome)
	}
	if !f.tt.HasText("已取消，没有写入文件。") {
		t.Fatalf("the canceled state is missing: %q", f.tt.Texts())
	}
	if !f.tt.HasText("导出 JSON") || !f.tt.HasText("导出 CSV") {
		t.Fatalf("the canceled dialog did not return to its formats: %q", f.tt.Texts())
	}

	// The same dialog starts over from the pre-export content.
	f.saver.saveCtxFn = nil
	if err := f.tt.Click("导出 CSV"); err != nil {
		t.Fatalf("click 导出 CSV: %v", err)
	}
	settle(t, f.m, f.q, f.tt)
	calls := f.saver.snapshot()
	if len(calls) != 2 || calls[1].format != app.ExportCSV {
		t.Fatalf("re-export calls = %+v", calls)
	}
	if !f.tt.HasText("已导出 1 件") {
		t.Fatalf("the re-export did not succeed: %q", f.tt.Texts())
	}
}

// TestExportDialogProgressLeavesNoReviewSeparator checks that the review
// link and its separator do not leave a dangling dot while the write runs.
func TestExportDialogProgressLeavesNoReviewSeparator(t *testing.T) {
	rec := recovery(
		uiStructuredItem(t, 1, record.QualityAutoAccepted),
		uiStructuredItem(t, 2, record.QualityNeedsReview),
	)
	f := newExportDialogFixture(t, rec, false)
	started := make(chan struct{})
	var once sync.Once
	f.saver.saveCtxFn = func(ctx context.Context, _ app.ExportFormat, _ string, _ []byte) (string, error) {
		once.Do(func() { close(started) })
		<-ctx.Done()
		return "", ctx.Err()
	}
	f.open(t)
	if err := f.tt.Click("导出 JSON"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("the export write did not start")
	}
	f.tt.Frame()
	if _, ok := f.tt.Find("待处理 1 ·"); ok {
		t.Fatalf("the exporting dialog left a dangling separator: %q", f.tt.Texts())
	}
	if _, ok := f.tt.Find("待处理 1"); !ok {
		t.Fatalf("the exporting dialog lost the pending count: %q", f.tt.Texts())
	}
	if _, ok := f.tt.Find("去校对"); ok {
		t.Fatalf("the exporting dialog still shows 去校对: %q", f.tt.Texts())
	}
	if err := f.tt.Click("取消"); err != nil {
		t.Fatal(err)
	}
	settle(t, f.m, f.q, f.tt)
	if _, ok := f.tt.Find("待处理 1 ·"); !ok {
		t.Fatalf("the canceled dialog did not restore the review link: %q", f.tt.Texts())
	}
}

// TestExportDialogCancelRacingAWriteShowsSuccess covers the cancel/write
// race: once the saver returned a path, the file exists and the dialog must
// report success even though 取消 was pressed.
func TestExportDialogCancelRacingAWriteShowsSuccess(t *testing.T) {
	f := newExportDialogFixture(t, recovery(uiStructuredItem(t, 1, record.QualityAutoAccepted)), false)
	started := make(chan struct{})
	var once sync.Once
	f.saver.saveCtxFn = func(ctx context.Context, _ app.ExportFormat, _ string, _ []byte) (string, error) {
		once.Do(func() { close(started) })
		<-ctx.Done()
		// The write committed before the cancellation landed.
		return "/tmp/raced.youkai.json", nil
	}
	f.open(t)
	if err := f.tt.Click("导出 JSON"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("the export write did not start")
	}
	if err := f.tt.Click("取消"); err != nil {
		t.Fatal(err)
	}
	settle(t, f.m, f.q, f.tt)
	// Canceling was requested, but the committed file owns the result.
	if f.m.exportDialog.canceled {
		t.Fatal("a committed write kept the canceled state")
	}
	if f.m.exportDialogSuccess() == nil || f.m.exportOutcome.path != "/tmp/raced.youkai.json" {
		t.Fatalf("a committed race was not shown as success: %+v", f.m.exportOutcome)
	}
	if !f.tt.HasText("已导出 1 件") || f.tt.HasText("已取消，没有写入文件。") {
		t.Fatalf("the raced result says the wrong thing: %q", f.tt.Texts())
	}
}

// TestExportDialogCleanupFailureIsNotCancellation keeps the dialog honest
// when a canceled write cannot remove the file it made.
func TestExportDialogCleanupFailureIsNotCancellation(t *testing.T) {
	f := newExportDialogFixture(t, recovery(uiStructuredItem(t, 1, record.QualityAutoAccepted)), false)
	started := make(chan struct{})
	var once sync.Once
	f.saver.saveCtxFn = func(ctx context.Context, _ app.ExportFormat, _ string, _ []byte) (string, error) {
		once.Do(func() { close(started) })
		<-ctx.Done()
		return "", &ExportCleanupError{
			Detail: "临时文件未删除",
			Err:    context.Canceled,
		}
	}
	f.open(t)
	if err := f.tt.Click("导出 JSON"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("the export write did not start")
	}
	if err := f.tt.Click("取消"); err != nil {
		t.Fatal(err)
	}
	settle(t, f.m, f.q, f.tt)
	if f.m.exportDialog.canceled || f.m.exportDialogFailure() == nil {
		t.Fatalf("a cleanup failure was called a cancellation: %+v", f.m.exportOutcome)
	}
	if !f.tt.HasText("导出失败：导出文件清理失败：临时文件未删除") ||
		f.tt.HasText("context canceled") ||
		f.tt.HasText("已取消，没有写入文件。") {
		t.Fatalf("the cleanup failure is hidden or leaks English: %q", f.tt.Texts())
	}
}

// TestExportDialogCleanupFailureKeepsWriteCause checks 08 §4's failure
// reason survives beside the file-left-behind detail.
func TestExportDialogCleanupFailureKeepsWriteCause(t *testing.T) {
	f := newExportDialogFixture(t, recovery(uiStructuredItem(t, 1, record.QualityAutoAccepted)), false)
	f.saver.saveFn = func(app.ExportFormat, string, []byte) (string, error) {
		return "", &ExportCleanupError{
			Detail: "临时文件未删除",
			Err:    errors.New("no space left on device"),
		}
	}
	f.open(t)
	if err := f.tt.Click("导出 JSON"); err != nil {
		t.Fatal(err)
	}
	settle(t, f.m, f.q, f.tt)
	if !f.tt.HasText("临时文件未删除") || !f.tt.HasText("no space left on device") {
		t.Fatalf("the cleanup failure lost the write cause: %q", f.tt.Texts())
	}
}

// TestExportDialogFailureRetriesWithoutChangingTheRecord covers S8's
// 失败 state and the retry action.
func TestExportDialogFailureRetriesWithoutChangingTheRecord(t *testing.T) {
	rec := recovery(uiStructuredItem(t, 1, record.QualityAutoAccepted))
	f := newExportDialogFixture(t, rec, false)
	before := f.m.recData
	beforeItems := len(before.Checkpoint.Items)
	beforeRevision := before.Checkpoint.SavedRevision
	attempts := 0
	f.saver.saveFn = func(app.ExportFormat, string, []byte) (string, error) {
		attempts++
		if attempts == 1 {
			return "", errors.New("磁盘已满")
		}
		return "/tmp/retry.youkai.json", nil
	}
	f.open(t)
	if err := f.tt.Click("导出 JSON"); err != nil {
		t.Fatalf("click 导出 JSON: %v", err)
	}
	settle(t, f.m, f.q, f.tt)
	if !f.tt.HasText("导出失败：磁盘已满") || !f.tt.HasText("内部记录未受影响。") ||
		!f.tt.HasText("重试") {
		t.Fatalf("the failure state is incomplete: %q", f.tt.Texts())
	}
	if f.m.recData != before || len(f.m.recData.Checkpoint.Items) != beforeItems ||
		f.m.recData.Checkpoint.SavedRevision != beforeRevision {
		t.Fatal("a failed export changed the internal record")
	}
	if err := f.tt.Click("重试"); err != nil {
		t.Fatalf("click 重试: %v", err)
	}
	settle(t, f.m, f.q, f.tt)
	if attempts != 2 {
		t.Fatalf("retry attempts = %d, want 2", attempts)
	}
	if !f.tt.HasText("已导出 1 件") || f.tt.HasText("导出失败：磁盘已满") {
		t.Fatalf("the retry did not replace the failure: %q", f.tt.Texts())
	}
}

// TestExportDialogOffersScreenshotDeletionWhenNothingIsPending covers the
// P-13 entry shown after a successful export of a fully reviewed record.
func TestExportDialogOffersScreenshotDeletionWhenNothingIsPending(t *testing.T) {
	rec := recovery(uiStructuredItem(t, 1, record.QualityAutoAccepted))
	f := newExportDialogFixture(t, rec, false)
	f.svc.previewFn = func(kind, recordID string) (app.CleanupPreview, error) {
		return app.CleanupPreview{Records: 1, EvidenceFiles: 3, EvidenceBytes: 1024, PendingReview: 0}, nil
	}
	f.open(t)
	if err := f.tt.Click("导出 JSON"); err != nil {
		t.Fatalf("click 导出 JSON: %v", err)
	}
	settle(t, f.m, f.q, f.tt)
	if !f.tt.HasText("删除这条记录的截图…") {
		t.Fatalf("the screenshot-deletion link is missing: %q", f.tt.Texts())
	}
	if err := f.tt.Click("删除这条记录的截图…"); err != nil {
		t.Fatalf("click 删除这条记录的截图…: %v", err)
	}
	settle(t, f.m, f.q, f.tt)
	if f.m.exportDialog.open {
		t.Fatal("the screenshot link left the export dialog open")
	}
	if f.m.page != pageResults || f.m.recordTab != recordTabSummary {
		t.Fatalf("the screenshot link landed on page %v tab %v", f.m.page, f.m.recordTab)
	}
	if f.m.cleanup == nil || f.m.cleanup.recordID != "rec" || !f.m.cleanup.ready {
		t.Fatalf("the screenshot confirmation did not open: %+v", f.m.cleanup)
	}
	if !f.tt.HasText("将删除记录 rec 的 3 张截图") {
		t.Fatalf("the screenshot confirmation is missing: %q", f.tt.Texts())
	}
}

// TestExportKeepsTheSnapshotWhenTheRecordChanges keeps the frozen
// revision contract of the export snapshot.
func TestExportKeepsTheSnapshotWhenTheRecordChanges(t *testing.T) {
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
		DirectExportSaver: saver,
		AppVersion:        "test-1",
	})
	m.SetContentSize(1024, 700)
	m.showRecord("s1", true)
	m.reloadRecord("s1", true)
	drainAll(t, m, q)
	if m.exportData == nil || m.exportData.Summary.Included != 2 {
		t.Fatalf("session snapshot = %+v", m.exportData)
	}

	m.startExport(app.ExportJSON)
	<-started
	m.showRecord("old", false)
	m.applyLoadedRecord("old", historyRec, false)
	if m.rec == nil || m.rec.ID != "old" {
		t.Fatalf("the history record did not replace the live one: %+v", m.rec)
	}
	close(release)
	drainAll(t, m, q)

	calls := saver.snapshot()
	if len(calls) != 1 {
		t.Fatalf("export calls = %+v", calls)
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
		m.exportOutcome.recordID != "s1" {
		t.Fatalf("the switched export lost its identity: %+v", m.exportOutcome)
	}
}

// TestExportBlocksWhileEditingOrScanning keeps the shared guard the dialog
// and the direct actions use.
func TestExportBlocksWhileEditingOrScanning(t *testing.T) {
	rec := recovery(
		uiStructuredItem(t, 1, record.QualityAutoAccepted),
		uiStructuredItem(t, 2, record.QualityNeedsReview),
	)
	f := newExportDialogFixture(t, rec, false)
	showRecordItems(f.m)

	f.m.selected = 0
	f.m.startEdit()
	if f.m.edit == nil {
		t.Fatal("the editor did not open")
	}
	f.m.edit.draft.Level = "14"
	if reason := f.m.exportBlockedReason(); !strings.Contains(reason, "未保存的修改") {
		t.Fatalf("dirty editor reason = %q", reason)
	}
	f.m.startExport(app.ExportJSON)
	drainAll(t, f.m, f.q)
	if calls := f.saver.snapshot(); len(calls) != 0 {
		t.Fatalf("a dirty editor exported: %+v", calls)
	}
	f.m.edit = nil

	f.m.rec.Live = true
	f.m.snap.Busy = true
	if reason := f.m.exportBlockedReason(); !strings.Contains(reason, "扫描尚未收尾") {
		t.Fatalf("busy scan reason = %q", reason)
	}
	f.m.startExport(app.ExportCSV)
	drainAll(t, f.m, f.q)
	if calls := f.saver.snapshot(); len(calls) != 0 {
		t.Fatalf("a running scan exported: %+v", calls)
	}
	f.m.snap.Busy = false
	if reason := f.m.exportBlockedReason(); reason != "" {
		t.Fatalf("settled record still blocked: %q", reason)
	}
}

// TestExportUsesTheOpenRecordsOwnSnapshot keeps the history/live
// revision separation of the export snapshot.
func TestExportUsesTheOpenRecordsOwnSnapshot(t *testing.T) {
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
		DirectExportSaver: saver,
		AppVersion:        "test-1",
	})
	m.SetContentSize(1024, 700)
	m.showRecord("s1", true)
	m.reloadRecord("s1", true)
	drainAll(t, m, q)
	if m.exportData == nil || m.exportData.Summary.Included != 3 {
		t.Fatalf("session snapshot = %+v", m.exportData)
	}

	m.showRecord("old", false)
	m.applyLoadedRecord("old", historyRec, false)
	if m.exportData == nil || m.exportData.Summary.Included != 1 {
		t.Fatalf("history snapshot = %+v", m.exportData)
	}
	m.startExport(app.ExportJSON)
	drainAll(t, m, q)

	calls := saver.snapshot()
	if len(calls) != 1 {
		t.Fatalf("export calls = %+v", calls)
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
	if m.exportData == nil || m.exportData.Summary.Included != 1 {
		t.Fatalf("the export changed the open record's snapshot: %+v", m.exportData)
	}
}

// TestExportSnapshotReloadsTheFinalCheckpoint keeps the final-checkpoint
// refresh contract for the export snapshot.
func TestExportSnapshotReloadsTheFinalCheckpoint(t *testing.T) {
	saver := &fakeDirectExportSaver{}
	item := uiStructuredItem(t, 1, record.QualityAutoAccepted)
	pre := recovery(item)
	pre.Checkpoint.Session.Termination = nil
	pre.Checkpoint.Session.EndedAt = nil
	svc := &fakeService{loads: map[string]*record.Recovery{"rec": pre}}
	m, q := newTestModel(t, Config{Service: svc, DirectExportSaver: saver, AppVersion: "test-1"})

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

// TestExportFollowsASavedRevision keeps the saved-revision contract.
func TestExportFollowsASavedRevision(t *testing.T) {
	rec := recovery(
		uiStructuredItem(t, 1, record.QualityAutoAccepted),
		uiStructuredItem(t, 2, record.QualityNeedsReview),
	)
	f := newExportDialogFixture(t, rec, false)
	confirmed := uiStructuredItem(t, 2, record.QualityHumanConfirmed)
	confirmed.Revision = 1
	f.m.applySavedItem(confirmed, 2)
	if f.m.exportData == nil || f.m.exportData.Summary.Included != 2 || f.m.exportData.Summary.Excluded != 0 {
		t.Fatalf("export snapshot after save = %+v", f.m.exportData)
	}
	if f.m.recData.Checkpoint.SavedRevision != 2 {
		t.Fatalf("saved revision = %d, want 2", f.m.recData.Checkpoint.SavedRevision)
	}
	f.open(t)
	if err := f.tt.Click("导出 JSON"); err != nil {
		t.Fatalf("click 导出 JSON: %v", err)
	}
	settle(t, f.m, f.q, f.tt)
	calls := f.saver.snapshot()
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

// TestExportIsNotLimitedByTheVisibleFilter keeps the filter-
// independent export set.
func TestExportIsNotLimitedByTheVisibleFilter(t *testing.T) {
	rec := recovery(
		uiStructuredItem(t, 1, record.QualityAutoAccepted),
		uiStructuredItem(t, 2, record.QualityAutoAccepted),
		uiStructuredItem(t, 3, record.QualityNeedsReview),
	)
	f := newExportDialogFixture(t, rec, false)
	showRecordItems(f.m)
	f.tt.Frame()
	if f.m.filter != resultFilterPending {
		t.Fatalf("default filter = %v, want pending", f.m.filter)
	}
	if visible := len(f.m.visibleResultRows()); visible != 1 {
		t.Fatalf("visible pending rows = %d, want 1", visible)
	}
	if f.m.exportData.Summary.Included != 2 {
		t.Fatalf("exportable set = %d, want 2", f.m.exportData.Summary.Included)
	}

	f.open(t)
	if err := f.tt.Click("导出 CSV"); err != nil {
		t.Fatalf("click 导出 CSV: %v", err)
	}
	settle(t, f.m, f.q, f.tt)
	calls := f.saver.snapshot()
	if len(calls) != 1 {
		t.Fatalf("export calls = %+v", calls)
	}
	rows, err := csv.NewReader(bytes.NewReader(calls[0].data[3:])).ReadAll()
	if err != nil {
		t.Fatalf("read the filtered export: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("filtered CSV rows = %d, want header + 2 exportable items", len(rows))
	}
	if f.m.exportOutcome == nil || f.m.exportOutcome.included != 2 {
		t.Fatalf("filtered export outcome = %+v", f.m.exportOutcome)
	}
}

// TestExportDialogDisablesReviewActions keeps the review lock while the
// dialog writes.
func TestExportDialogDisablesReviewActions(t *testing.T) {
	rec := recovery(
		uiStructuredItem(t, 1, record.QualityAutoAccepted),
		uiStructuredItem(t, 2, record.QualityNeedsReview),
	)
	f := newExportDialogFixture(t, rec, false)
	showRecordItems(f.m)
	started := make(chan struct{})
	release := make(chan struct{})
	f.saver.saveFn = func(app.ExportFormat, string, []byte) (string, error) {
		close(started)
		<-release
		return "/tmp/out.youkai.json", nil
	}
	f.m.selected = 0
	f.open(t)

	if err := f.tt.Click("导出 JSON"); err != nil {
		t.Fatalf("click 导出 JSON: %v", err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("the export write did not start")
	}
	f.m.startEdit()
	if f.m.edit != nil {
		t.Fatal("an export in flight allowed the editor to open")
	}
	f.m.confirmCurrent()
	if !strings.Contains(f.m.reviewErr, "正在导出") {
		t.Fatalf("confirmation during export = %q", f.m.reviewErr)
	}
	f.tt.Frame()
	if !f.tt.HasText("正在导出 JSON……") {
		t.Fatalf("the export busy state is not shown: %q", f.tt.Texts())
	}

	close(release)
	drainAll(t, f.m, f.q)
	if f.m.exporting {
		t.Fatal("the export did not settle")
	}
}

// TestExportDialogDirectoryFailureOffersRetry checks that a directory
// failure stays inside the dialog with its reason, the untouched-record
// note and 重试.
func TestExportDialogDirectoryFailureOffersRetry(t *testing.T) {
	rec := recovery(uiStructuredItem(t, 1, record.QualityAutoAccepted))
	f := newExportDialogFixture(t, rec, false)
	f.saver.saveFn = func(app.ExportFormat, string, []byte) (string, error) {
		return "", &ExportDirectoryError{Directory: "/missing/Downloads", Err: errors.New("permission denied")}
	}
	f.open(t)
	if err := f.tt.Click("导出 CSV"); err != nil {
		t.Fatalf("click 导出 CSV: %v", err)
	}
	settle(t, f.m, f.q, f.tt)
	if f.m.exportOutcome == nil || f.m.exportOutcome.err == nil {
		t.Fatalf("the directory failure was not recorded: %+v", f.m.exportOutcome)
	}
	var dirErr *ExportDirectoryError
	if !errors.As(f.m.exportOutcome.err, &dirErr) {
		t.Fatalf("the directory failure was not classified: %v", f.m.exportOutcome.err)
	}
	if !f.tt.HasText("导出失败：无法写入下载目录 /missing/Downloads") ||
		!f.tt.HasText("内部记录未受影响。") || !f.tt.HasText("重试") {
		t.Fatalf("the dialog failure is incomplete: %q", f.tt.Texts())
	}
	if f.m.recData == nil || len(f.m.recData.Checkpoint.Items) != 1 {
		t.Fatal("a failed export changed the record")
	}
}

// TestExportDialogEscDoesNotCloseDuringExport is part of the running-state
// contract; it also checks that a second Ctrl+E cannot open another
// dialog while this one is in flight.
func TestExportDialogEscDoesNotCloseDuringExport(t *testing.T) {
	rec := recovery(uiStructuredItem(t, 1, record.QualityAutoAccepted))
	f := newExportDialogFixture(t, rec, false)
	started := make(chan struct{})
	release := make(chan struct{})
	f.saver.saveFn = func(app.ExportFormat, string, []byte) (string, error) {
		close(started)
		<-release
		return "/tmp/out.youkai.json", nil
	}
	f.open(t)
	if err := f.tt.Click("导出 JSON"); err != nil {
		t.Fatalf("click 导出 JSON: %v", err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("the export write did not start")
	}
	f.tt.Key(ui.Cmd, ui.KeyE)
	f.tt.Key(0, ui.KeyEscape)
	f.tt.ClickAt(5, 5)
	f.tt.Frame()
	if !f.m.exportDialog.open || !f.tt.HasText("正在导出 JSON……") {
		t.Fatalf("a second entry, Esc or the backdrop closed the running dialog: %q", f.tt.Texts())
	}
	close(release)
	settle(t, f.m, f.q, f.tt)
	if !f.tt.HasText("已导出 1 件") {
		t.Fatalf("the held export did not finish: %q", f.tt.Texts())
	}
}
