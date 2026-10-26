package ui

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/LoTwT/youkai/internal/scan"
	"github.com/egoist/mygo/ui"
)

func historyMetadata(r1, r2 time.Time) []app.HistoryRecord {
	return []app.HistoryRecord{
		{
			ID:            "r1",
			SavedAt:       r1,
			Client:        record.ClientMacOSCloud,
			CapturedCount: 995,
			Coverage:      record.CoveragePartial,
			PendingReview: 12,
			TotalBytes:    1024,
			EvidenceFiles: 4,
		},
		{
			ID:         "r2",
			SavedAt:    r2,
			Unreadable: true,
		},
	}
}

func TestViewHistoryShowsMetadataAndDateFilter(t *testing.T) {
	svc := &fakeService{history: historyMetadata(
		time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC),
		time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC),
	)}
	m, q := newTestModel(t, Config{Service: svc})
	m.SetContentSize(1440, 700)
	tt := ui.NewTester(m.View, 1440, 1000)
	settle(t, m, q, tt)

	if !tt.HasText("995") || !tt.HasText("12") || !tt.HasText("1.0 KB") || !tt.HasText("部分") {
		t.Fatalf("history metadata does not show: %q", tt.Texts())
	}
	if !tt.HasText("记录不可读") || !tt.HasText("无法确认") {
		t.Fatalf("unreadable history state does not show: %q", tt.Texts())
	}

	m.historyFilter = "2026-10-17"
	tt.Frame()
	if !tt.HasText("995") {
		t.Fatalf("matching record disappeared: %q", tt.Texts())
	}
	if tt.HasText("记录不可读") {
		t.Fatalf("date filter kept the unreadable record: %q", tt.Texts())
	}

	m.historyFilter = "2026-13-40"
	tt.Frame()
	if !tt.HasText("格式应为 YYYY-MM-DD。") {
		t.Fatalf("invalid date hint does not show: %q", tt.Texts())
	}

	m.historyFilter = ""
	tt.Frame()
	if !tt.HasText("记录不可读") {
		t.Fatalf("clearing the filter did not restore all records: %q", tt.Texts())
	}
}

func TestViewHistoryShowsSessionTermination(t *testing.T) {
	history := historyMetadata(
		time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC),
		time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC),
	)
	history[0].Termination = &record.Termination{
		Kind: record.TerminationFailed, Code: "navigationMismatch", Phase: record.PhaseCapturing,
	}
	svc := &fakeService{history: history}
	m, q := newTestModel(t, Config{Service: svc})
	m.SetContentSize(1440, 700)
	tt := ui.NewTester(m.View, 1440, 1000)
	settle(t, m, q, tt)

	if !tt.HasText("失败：navigationMismatch") {
		t.Fatalf("history does not show the failed session code: %q", tt.Texts())
	}
}

func TestHistoryExportFailure(t *testing.T) {
	svc := &fakeService{
		history: historyMetadata(time.Now(), time.Now().Add(-time.Hour)),
		loads: map[string]*record.Recovery{
			"r1": recovery(uiStructuredItem(t, 1, record.QualityAutoAccepted)),
		},
	}
	saver := &fakeDirectExportSaver{}
	m, q := newTestModel(t, Config{Service: svc, DirectExportSaver: saver, AppVersion: "test"})
	tt := ui.NewTester(m.View, 1020, 1000)
	settle(t, m, q, tt)

	saver.saveFn = func(app.ExportFormat, string, []byte) (string, error) {
		return "", errors.New("no space left on device")
	}
	m.ExportHistory("r1", app.ExportJSON)
	settle(t, m, q, tt)
	if m.historyExporting != "" || m.exportOutcome == nil || m.exportOutcome.err == nil ||
		!strings.Contains(m.exportOutcome.text(), "no space left on device") {
		t.Fatalf("failure state = exporting %q, outcome %+v", m.historyExporting, m.exportOutcome)
	}
}

func TestHistoryPerRowExportUsesThatRecord(t *testing.T) {
	svc := &fakeService{
		history: historyMetadata(
			time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC),
			time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC),
		),
		loads: map[string]*record.Recovery{
			"r1": recovery(uiStructuredItem(t, 1, record.QualityAutoAccepted)),
		},
	}
	saver := &fakeDirectExportSaver{}
	m, q := newTestModel(t, Config{Service: svc, DirectExportSaver: saver, AppVersion: "test"})
	tt := ui.NewTester(m.View, 1020, 1000)
	settle(t, m, q, tt)

	m.ExportHistory("r1", app.ExportJSON)
	settle(t, m, q, tt)
	calls := saver.snapshot()
	if len(calls) != 1 || calls[0].format != app.ExportJSON {
		t.Fatalf("export calls = %+v, want one JSON export", calls)
	}
	if m.historyExporting != "" || m.exportOutcome == nil || m.exportOutcome.err != nil {
		t.Fatalf("export state = %q / %+v", m.historyExporting, m.exportOutcome)
	}
	if !tt.HasText("已导出 JSON（1 件，历史记录 r1）") {
		t.Fatalf("history export result does not show: %q", tt.Texts())
	}
}

// historyTableRecords builds n saved records with distinct times, each
// carrying the four export states the table shows. The newest is 10-21
// 14:02; every later row steps back an hour.
func historyTableRecords(n int) []app.HistoryRecord {
	base := time.Date(2026, 10, 21, 14, 2, 0, 0, time.Local)
	out := make([]app.HistoryRecord, 0, n)
	for i := 0; i < n; i++ {
		captured := int64(1008 - i)
		out = append(out, app.HistoryRecord{
			ID:               fmt.Sprintf("r%02d", i+1),
			SavedAt:          base.Add(-time.Duration(i) * 25 * time.Hour),
			Client:           record.ClientMacOSCloud,
			CapturedCount:    captured,
			Coverage:         record.CoveragePartial,
			CoverageExpected: captured + 2,
			PendingReview:    i % 3,
			ExportableCount:  int64(1005 - i),
			FailedCount:      int64(i % 2),
			UnprocessedCount: int64(i % 3),
			OtherCount:       0,
			TotalBytes:       1024 * 1024,
			EvidenceFiles:    4,
		})
	}
	return out
}

func historyHeaderTitles(columns []ui.TableColumn) []string {
	out := make([]string, len(columns))
	for i, column := range columns {
		out[i] = column.Title
	}
	return out
}

// TestHistoryColumnsByWidth guards the 8/7/7 split and the fact that
// "无法导出" is a first-class column at every width while storage size and
// source are not shown on the narrow tables.
func TestHistoryColumnsByWidth(t *testing.T) {
	wide := historyHeaderTitles(historyColumnsFor(1440))
	wantWide := []string{"时间", "采集", "可导出", "待校对", "无法导出", "覆盖", "大小", "操作"}
	if strings.Join(wide, "/") != strings.Join(wantWide, "/") {
		t.Fatalf("1440 columns = %v, want %v", wide, wantWide)
	}
	var wideWidth float32
	for _, column := range historyColumnsWide {
		wideWidth += max(column.Width, column.MinWidth)
	}
	if wideWidth != 994 {
		t.Errorf("1440 fixed width = %v, want the design's 994 minimum", wideWidth)
	}

	narrow := historyHeaderTitles(historyColumnsFor(1020))
	wantNarrow := []string{"时间", "采集", "可导出", "待校对", "无法导出", "覆盖", "操作"}
	if strings.Join(narrow, "/") != strings.Join(wantNarrow, "/") {
		t.Fatalf("1020 columns = %v, want %v", narrow, wantNarrow)
	}
	if got := historyHeaderTitles(historyColumnsFor(860)); strings.Join(got, "/") != strings.Join(wantNarrow, "/") {
		t.Fatalf("860 columns = %v, want %v", got, wantNarrow)
	}
	for _, title := range append(wide, narrow...) {
		if title == "来源" {
			t.Fatal("history columns still show the source")
		}
	}
}

// TestHistoryPageWideTable checks the 1440 table's eight columns, the
// per-record breakdown and that the current content needs no horizontal
// scrolling.
func TestHistoryPageWideTable(t *testing.T) {
	svc := &fakeService{history: historyTableRecords(12)}
	m, q := newTestModel(t, Config{Service: svc, DirectExportSaver: &fakeDirectExportSaver{}})
	m.SetContentSize(1440, 900)
	tt := ui.NewTester(m.View, 1440, 900)
	settle(t, m, q, tt)

	for _, title := range []string{"时间", "采集", "可导出", "待校对", "无法导出", "覆盖", "大小", "操作"} {
		rect, ok := tt.Find(title)
		if !ok {
			t.Fatalf("the wide table does not show %q", title)
		}
		if rect.X < 168 || rect.X+rect.W > 1440-12 {
			t.Errorf("%q at %+v is outside the page", title, rect)
		}
	}
	if tt.HasText("来源") {
		t.Fatal("the history table shows a source column")
	}
	for _, text := range []string{"1,008", "1,005", "失败 1 · 未处理 1 · 其他 0", "99% 部分", "历史记录"} {
		if !tt.HasText(text) {
			t.Errorf("the wide table does not show %q: %q", text, tt.Texts())
		}
	}

	before, _ := tt.Find("操作")
	tt.Scroll(720, 400, 240, 0)
	after, _ := tt.Find("操作")
	if after.X != before.X {
		t.Errorf("1440 scrolled horizontally: 操作 %.1f -> %.1f", before.X, after.X)
	}
}

// TestHistoryPageNarrowTableFits checks the 1020 seven-column layout: all
// headers stay inside the panel and a horizontal wheel does not move them.
func TestHistoryPageNarrowTableFits(t *testing.T) {
	svc := &fakeService{history: historyTableRecords(12)}
	m, q := newTestModel(t, Config{Service: svc, DirectExportSaver: &fakeDirectExportSaver{}})
	m.SetContentSize(1020, 700)
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)

	if m.contentWidth != 1020 || historyUsesComposite(m.contentWidth) {
		t.Fatalf("1020 must use the native table (width %d, composite %v)", m.contentWidth, historyUsesComposite(m.contentWidth))
	}
	for _, title := range []string{"时间", "采集", "可导出", "待校对", "无法导出", "覆盖", "操作"} {
		rect, ok := tt.Find(title)
		if !ok {
			t.Fatalf("the 1020 table does not show %q", title)
		}
		if rect.X < 144+12 || rect.X+rect.W > 1020-12 {
			t.Errorf("%q at %+v is outside the 1020 page", title, rect)
		}
	}
	if tt.HasText("大小") {
		t.Fatal("the 1020 table still shows the storage-size column")
	}

	before, _ := tt.Find("操作")
	tt.Scroll(600, 350, 240, 0)
	after, _ := tt.Find("操作")
	if after.X != before.X {
		t.Errorf("1020 scrolled horizontally: 操作 %.1f -> %.1f", before.X, after.X)
	}
}

// TestHistoryMinFixedActionColumn checks the 860 composite: the data
// header and rows scroll sideways together, the action column stays at the
// right edge and remains clickable, and vertical row scrolling leaves the
// header where it is.
func TestHistoryMinFixedActionColumn(t *testing.T) {
	svc := &fakeService{
		history: historyTableRecords(12),
		loads: map[string]*record.Recovery{
			"r01": testRecord(),
		},
	}
	m, q := newTestModel(t, Config{Service: svc, DirectExportSaver: &fakeDirectExportSaver{}})
	m.SetContentSize(860, 560)
	tt := ui.NewTester(m.View, 860, 560)
	settle(t, m, q, tt)

	if !historyUsesComposite(860) {
		t.Fatal("860 must use the composite table")
	}
	if historyMinTableWidth860 != 776 {
		t.Fatalf("860 table minimum = %v, want 776", historyMinTableWidth860)
	}
	panelLeft, panelRight := float32(144+12), float32(860-12)
	for _, title := range []string{"时间", "采集", "可导出", "待校对", "无法导出", "覆盖", "操作"} {
		rect, ok := tt.Find(title)
		if !ok {
			t.Fatalf("the 860 table does not show %q", title)
		}
		if rect.X < panelLeft || rect.X+rect.W > panelRight {
			t.Errorf("%q at %+v is outside the 860 panel", title, rect)
		}
	}
	if tt.HasText("大小") {
		t.Fatal("the 860 table still shows the storage-size column")
	}

	// The data columns scroll, the action column does not.
	dataBefore, _ := tt.Find("无法导出")
	cellBefore, _ := tt.Find("失败 0 · 未处理 0 · 其他 0")
	actionBefore, _ := tt.Find("操作")
	openBefore, _ := tt.Find("打开")
	tt.Scroll(400, 300, 200, 0)
	dataAfter, _ := tt.Find("无法导出")
	cellAfter, _ := tt.Find("失败 0 · 未处理 0 · 其他 0")
	actionAfter, _ := tt.Find("操作")
	openAfter, _ := tt.Find("打开")
	if !(dataAfter.X < dataBefore.X-1) {
		t.Errorf("860 data columns did not scroll: %.1f -> %.1f", dataBefore.X, dataAfter.X)
	}
	if dHeader, dCell := dataBefore.X-dataAfter.X, cellBefore.X-cellAfter.X; dHeader-dCell > 1 || dCell-dHeader > 1 {
		t.Errorf("860 header and row scroll differently: header %.1f, cell %.1f", dHeader, dCell)
	}
	if actionAfter.X != actionBefore.X {
		t.Errorf("860 action header moved with the scroll: %.1f -> %.1f", actionBefore.X, actionAfter.X)
	}
	if openAfter.X != openBefore.X || openAfter.W < 1 {
		t.Errorf("860 action button moved or was clipped: %+v -> %+v", openBefore, openAfter)
	}

	// A click on the fixed column still opens the record.
	if err := tt.Click("打开"); err != nil {
		t.Fatalf("the fixed action button is not clickable: %v", err)
	}
	settle(t, m, q, tt)
	if m.displayPage() != pageResults || m.activeSlot != resultSlotHistory || m.historySlot.recordID != "r01" {
		t.Fatalf("opening from the fixed column = page %v, slot %v, record %q",
			m.displayPage(), m.activeSlot, m.historySlot.recordID)
	}
	if err := tt.Click("← 返回历史"); err != nil {
		t.Fatalf("click 返回历史: %v", err)
	}
	settle(t, m, q, tt)

	// Vertical row scrolling is independent of the header.
	headerBefore, _ := tt.Find("时间")
	firstBefore, _ := m.historyList.Visible()
	tt.Scroll(400, 300, 0, 300)
	firstAfter, _ := m.historyList.Visible()
	headerAfter, _ := tt.Find("时间")
	if firstAfter == firstBefore {
		t.Errorf("860 rows did not scroll vertically: first row %d -> %d", firstBefore, firstAfter)
	}
	if headerAfter.Y != headerBefore.Y {
		t.Errorf("860 header moved vertically: %.1f -> %.1f", headerBefore.Y, headerAfter.Y)
	}

	// The fixed column is still clickable after the vertical scroll: a
	// press there opens that visible row's menu.
	visibleTime := historyTimeText(HistoryEntry{SavedAt: historyTableTime(firstAfter)})
	rowRect, ok := tt.Find(visibleTime)
	if !ok {
		t.Fatalf("the first visible row %q is missing", visibleTime)
	}
	actionRect, _ := tt.Find("操作")
	tt.ClickAt(actionRect.X+actionRect.W/2, rowRect.Y+rowRect.H/2)
	tt.Frame()
	if !m.historyMenuOpen || m.historyMenuID != fmt.Sprintf("r%02d", firstAfter+1) {
		t.Fatalf("the fixed column did not open row %d's menu: open=%v id=%q",
			firstAfter, m.historyMenuOpen, m.historyMenuID)
	}
	if rect, ok := tt.Find("删除记录"); !ok || rect.X < 0 || rect.X+rect.W > 860 {
		t.Fatalf("the row menu does not fit the 860 window: %+v (found %v)", rect, ok)
	}
}

func historyTableTime(index int) time.Time {
	return time.Date(2026, 10, 21, 14, 2, 0, 0, time.Local).Add(-time.Duration(index) * 25 * time.Hour)
}

// TestHistoryOpenAndReturn checks row selection, Enter opening the chosen
// record, and returning to the same selection from the solution rail.
func TestHistoryOpenAndReturn(t *testing.T) {
	svc := &fakeService{
		history: historyTableRecords(4),
		loads: map[string]*record.Recovery{
			"r02": testRecord(),
		},
	}
	m, q := newTestModel(t, Config{Service: svc, DirectExportSaver: &fakeDirectExportSaver{}})
	m.SetContentSize(1020, 700)
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)

	if err := tt.Click("10-20 13:02"); err != nil {
		t.Fatal(err)
	}
	if m.historyRow != 1 {
		t.Fatalf("clicking the second row selected %d", m.historyRow)
	}
	tt.Key(0, ui.KeyEnter)
	settle(t, m, q, tt)
	if m.displayPage() != pageResults || m.activeSlot != resultSlotHistory || m.historySlot.recordID != "r02" {
		t.Fatalf("Enter opened page %v, slot %v, record %q", m.displayPage(), m.activeSlot, m.historySlot.recordID)
	}

	if err := tt.Click("← 返回历史"); err != nil {
		t.Fatalf("click 返回历史: %v", err)
	}
	settle(t, m, q, tt)
	if m.displayPage() != pageHistory || m.historyRow != 1 {
		t.Fatalf("returning to history = page %v, row %d", m.displayPage(), m.historyRow)
	}
	if !tt.HasText("已选中 10-20 13:02") {
		t.Fatalf("the status bar lost the selection: %q", tt.Texts())
	}
}

// TestHistoryCurrentRowOpensLiveSession checks that the 本次 row opens the
// session slot and never creates a second history slot.
func TestHistoryCurrentRowOpensLiveSession(t *testing.T) {
	svc := &fakeService{
		history: historyTableRecords(3),
		loads: map[string]*record.Recovery{
			"r01": testRecord(),
		},
	}
	m, q := newTestModel(t, Config{Service: svc, DirectExportSaver: &fakeDirectExportSaver{}})
	m.Apply(snap("r01", 1, false, scan.PhaseEnded, scan.Counts{Captured: 1}, 1, false, ""))
	m.page = pageHistory
	m.SetContentSize(1020, 700)
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)

	if !tt.HasText("本次") {
		t.Fatalf("the current session row has no 本次 badge: %q", tt.Texts())
	}
	if err := tt.Click("10-21 14:02"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyEnter)
	settle(t, m, q, tt)
	if m.displayPage() != pageResults {
		t.Fatalf("the current row opened page %v, want results", m.displayPage())
	}
	if m.activeSlot != resultSlotSession {
		t.Fatalf("the current row opened slot %v, want session", m.activeSlot)
	}
	if m.historySlot.recordID != "" {
		t.Fatalf("the current row created a history slot for %q", m.historySlot.recordID)
	}
}

// TestHistoryRowMenu checks the single-row menu: the current session's
// delete entries are disabled with a reason, a normal record's delete
// entry works, and the menu offers no source or multi-select actions.
func TestHistoryRowMenu(t *testing.T) {
	svc := &fakeService{
		history: historyTableRecords(3),
		loads: map[string]*record.Recovery{
			"r01": testRecord(),
		},
		previewFn: func(kind, recordID string) (app.CleanupPreview, error) {
			return app.CleanupPreview{Records: 1, EvidenceFiles: 2, EvidenceBytes: 1024, TotalBytes: 2048}, nil
		},
	}
	m, q := newTestModel(t, Config{Service: svc, DirectExportSaver: &fakeDirectExportSaver{}})
	m.Apply(snap("r01", 1, false, scan.PhaseEnded, scan.Counts{Captured: 1}, 1, false, ""))
	m.page = pageHistory
	m.SetContentSize(1020, 700)
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)

	m.openHistoryMenu("r01")
	tt.Frame()
	for _, text := range []string{"打开", "导出 CSV", "导出 JSON", "删除截图", "删除记录",
		"当前会话记录，扫描中或未转移前不能删除。"} {
		if !tt.HasText(text) {
			t.Errorf("the current session menu does not show %q: %q", text, tt.Texts())
		}
	}
	if got := m.historyDeleteBlockedReason(m.history[0]); got == "" {
		t.Fatal("the current session row has no deletion reason")
	}
	if err := tt.Click("删除记录"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if m.cleanup != nil {
		t.Fatal("the disabled delete entry opened a cleanup prompt")
	}

	m.closeHistoryMenu()
	tt.Frame()
	m.openHistoryMenu("r02")
	tt.Frame()
	if tt.HasText("当前会话记录，扫描中或未转移前不能删除。") {
		t.Fatal("a normal record's menu kept the current-session reason")
	}
	if got := m.historyDeleteBlockedReason(m.history[1]); got != "" {
		t.Fatalf("a normal record's delete entries are blocked: %q", got)
	}
	if err := tt.Click("删除记录"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if m.cleanup == nil {
		t.Fatal("a normal record's delete entry did not open the measured cleanup")
	}
	if !tt.HasText("将永久删除记录 r02") {
		t.Fatalf("the cleanup preview does not show: %q", tt.Texts())
	}
}

// TestHistoryKeyboardSelectionAndMenu checks the keyboard path: Tab
// reaches the row's ⋯ button, Enter opens the menu, Tab walks its items,
// and arrows keep moving the table selection.
func TestHistoryKeyboardSelectionAndMenu(t *testing.T) {
	svc := &fakeService{history: historyTableRecords(4)}
	m, q := newTestModel(t, Config{Service: svc, DirectExportSaver: &fakeDirectExportSaver{}})
	m.SetContentSize(1020, 700)
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)

	if err := tt.Click("10-21 14:02"); err != nil {
		t.Fatal(err)
	}
	// Arrow keys move the table's choice before any menu takes the focus.
	tt.Key(0, ui.KeyDown)
	if m.historyRow != 1 {
		t.Fatalf("Down moved the selection to %d", m.historyRow)
	}
	tt.Key(0, ui.KeyUp)
	if m.historyRow != 0 {
		t.Fatalf("Up moved the selection to %d", m.historyRow)
	}
	reached := false
	for i := 0; i < 6; i++ {
		tt.Key(0, ui.KeyTab)
		tt.Frame()
		if tt.Focused("⋯") {
			reached = true
			break
		}
	}
	if !reached {
		t.Fatal("Tab never reached the row menu button")
	}
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if !m.historyMenuOpen {
		t.Fatal("Enter on the row menu button did not open the menu")
	}
	if !tt.Focused("打开") {
		t.Fatal("the first menu item does not take the keyboard focus")
	}
	tt.Key(0, ui.KeyTab)
	tt.Frame()
	if !tt.Focused("导出 CSV") {
		t.Fatal("Tab does not move through the menu items")
	}
	tt.Key(0, ui.KeyEnter)
	settle(t, m, q, tt)
	if m.historyMenuOpen {
		t.Fatal("Enter on a menu item did not close the menu")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if m.historyMenuOpen {
		t.Fatal("Escape did not close the row menu")
	}
}

// TestHistoryDateSearch filters by the date search, in the design's wording
// and without a source filter.
func TestHistoryDateSearch(t *testing.T) {
	svc := &fakeService{history: historyTableRecords(3)}
	m, q := newTestModel(t, Config{Service: svc, DirectExportSaver: &fakeDirectExportSaver{}})
	m.SetContentSize(1020, 700)
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)

	if !tt.HasText("搜索日期") {
		t.Fatal("the history search is not labelled 搜索日期")
	}
	m.historyFilter = "2026-10-21"
	tt.Frame()
	if !tt.HasText("10-21 14:02") || tt.HasText("10-20 13:02") {
		t.Fatalf("the date search did not filter the rows: %q", tt.Texts())
	}
	if tt.HasText("来源") {
		t.Fatal("the history page added a source filter")
	}
}

// TestHistoryCompositeShowsWholeRowsAt860 checks the 860 table's vertical
// viewport: its list shows only complete 52-DIP rows, the footer never
// overlaps the last row, and the same holds at the bottom of the scroll.
func TestHistoryCompositeShowsWholeRowsAt860(t *testing.T) {
	svc := &fakeService{history: historyTableRecords(12)}
	m, q := newTestModel(t, Config{Service: svc, DirectExportSaver: &fakeDirectExportSaver{}})
	m.SetContentSize(860, 560)
	tt := ui.NewTester(m.View, 860, 560)
	settle(t, m, q, tt)
	if !historyUsesComposite(860) {
		t.Fatal("860 must use the composite table")
	}
	entries := m.visibleHistory()

	footerRect := func() (ui.Rect, string) {
		for _, text := range tt.Texts() {
			if strings.HasPrefix(text, "共 ") {
				if rect, ok := tt.Find(text); ok {
					return rect, text
				}
			}
		}
		t.Fatalf("the table footer is missing: %q", tt.Texts())
		return ui.Rect{}, ""
	}
	checkWholeRows := func(stage string) {
		t.Helper()
		first, last := m.historyList.Visible()
		if first < 0 || last < first {
			t.Fatalf("%s: the table shows no rows (%d..%d)", stage, first, last)
		}
		if count := last - first + 1; count != 7 {
			t.Fatalf("%s: visible rows = %d, want 7 whole rows", stage, count)
		}
		footer, text := footerRect()
		if !strings.Contains(text, fmt.Sprintf("显示 %d 条", last-first+1)) {
			t.Fatalf("%s: footer %q disagrees with rows %d..%d", stage, text, first, last)
		}
		for row := first; row <= last; row++ {
			if row >= len(entries) {
				t.Fatalf("%s: visible row %d exceeds %d entries", stage, row, len(entries))
			}
			rect, ok := tt.Find(historyRowLabel(entries[row]))
			if !ok {
				t.Fatalf("%s: row %d is not rendered", stage, row)
			}
			if rect.H < historyRowHeight860-0.01 || rect.H > historyRowHeight860+0.01 {
				t.Fatalf("%s: row %d height = %.3f, want %d", stage, row, rect.H, historyRowHeight860)
			}
			if rect.Y+rect.H > footer.Y+0.01 {
				t.Fatalf("%s: row %d bottom %.1f overlaps footer top %.1f",
					stage, row, rect.Y+rect.H, footer.Y)
			}
		}
		if last+1 < len(entries) {
			if rect, ok := tt.Find(historyRowLabel(entries[last+1])); ok && rect.H > 0 {
				t.Fatalf("%s: row %d is partially visible (height %.1f)",
					stage, last+1, rect.H)
			}
		}
	}

	checkWholeRows("top")
	tt.Scroll(400, 300, 0, 1000)
	settle(t, m, q, tt)
	checkWholeRows("bottom")
}

// TestHistoryNativeTableWholeRows checks the native 1440/1024/1020 table:
// its panel leaves whole rows, so the footer never follows a clipped row.
func TestHistoryNativeTableWholeRows(t *testing.T) {
	for _, tc := range []struct {
		width  int
		height int
	}{
		{width: 1440, height: 900},
		{width: 1024, height: 700},
		{width: 1020, height: 700},
	} {
		t.Run(fmt.Sprintf("width-%d", tc.width), func(t *testing.T) {
			svc := &fakeService{history: historyTableRecords(12)}
			m, q := newTestModel(t, Config{Service: svc, DirectExportSaver: &fakeDirectExportSaver{}})
			m.SetContentSize(tc.width, tc.height)
			tt := ui.NewTester(m.View, tc.width, tc.height)
			settle(t, m, q, tt)
			if historyUsesComposite(m.contentWidth) {
				t.Fatalf("%d must use the native history table", tc.width)
			}
			entries := m.visibleHistory()
			first, last := m.historyList.Visible()
			if first < 0 || last < first || last >= len(entries) {
				t.Fatalf("visible rows %d..%d of %d", first, last, len(entries))
			}
			var rowHeight float32
			for row := first; row <= last; row++ {
				rect, ok := tt.Find(historyRowLabel(entries[row]))
				if !ok {
					t.Fatalf("row %d is not rendered", row)
				}
				if row == first {
					rowHeight = rect.H
				} else if math.Abs(float64(rect.H-rowHeight)) > 0.01 {
					t.Fatalf("row %d height = %.2f, other rows are %.2f",
						row, rect.H, rowHeight)
				}
			}
			if rowHeight < 40 {
				t.Fatalf("the visible row height is %.2f, want the table's row height", rowHeight)
			}
			if last+1 < len(entries) {
				if rect, ok := tt.Find(historyRowLabel(entries[last+1])); ok && rect.H > 0 {
					t.Fatalf("row %d below the viewport is clipped into view: %+v", last+1, rect)
				}
			}
		})
	}
}

// TestHistoryCompositeSnapsPreciseScrollToRowsAt860 checks a touchpad's
// 26-DIP vertical move: the offset may not stop between two 52-DIP rows,
// so the table shows whole rows instead of half of the first and last one.
func TestHistoryCompositeSnapsPreciseScrollToRowsAt860(t *testing.T) {
	svc := &fakeService{history: historyTableRecords(12)}
	m, q := newTestModel(t, Config{Service: svc, DirectExportSaver: &fakeDirectExportSaver{}})
	m.SetContentSize(860, 560)
	tt := ui.NewTester(m.View, 860, 560)
	settle(t, m, q, tt)
	entries := m.visibleHistory()
	header, _ := tt.Find("时间")
	action, _ := tt.Find("操作")

	footerTop := func() float32 {
		for _, text := range tt.Texts() {
			if strings.HasPrefix(text, "共 ") {
				if rect, ok := tt.Find(text); ok {
					return rect.Y
				}
			}
		}
		t.Fatalf("the table footer is missing: %q", tt.Texts())
		return 0
	}
	rest := func() {
		tt.Advance(historySnapDelay + 40*time.Millisecond)
		tt.Frame()
	}
	check := func(stage string, wantOffset, wantFirst int) {
		t.Helper()
		if got := m.historyScroll.Y; got != float32(wantOffset) {
			t.Fatalf("%s: scroll offset = %v, want the %d-DIP row boundary",
				stage, got, wantOffset)
		}
		first, last := m.historyList.Visible()
		if first != wantFirst || last-first+1 != 7 {
			t.Fatalf("%s: visible rows = %d..%d, want %d..%d",
				stage, first, last, wantFirst, wantFirst+6)
		}
		footer := footerTop()
		headerAfter, _ := tt.Find("时间")
		actionAfter, _ := tt.Find("操作")
		if headerAfter.Y != header.Y || actionAfter.X != action.X {
			t.Fatalf("%s: header/action moved: %+v -> %+v, %+v -> %+v",
				stage, header, headerAfter, action, actionAfter)
		}
		if actionAfter.W < 1 {
			t.Fatalf("%s: the fixed action column is clipped: %+v", stage, actionAfter)
		}
		for row := first; row <= last; row++ {
			rect, ok := tt.Find(historyRowLabel(entries[row]))
			if !ok {
				t.Fatalf("%s: row %d is not rendered", stage, row)
			}
			if rect.H < historyRowHeight860-0.01 || rect.H > historyRowHeight860+0.01 {
				t.Fatalf("%s: row %d height = %.3f, want %d", stage, row, rect.H, historyRowHeight860)
			}
			if rect.Y+rect.H > footer+0.01 {
				t.Fatalf("%s: row %d overlaps the footer", stage, row)
			}
		}
	}

	// A precise 26-DIP move leaves the tracked offset at 26 until the
	// motion stops; the rest snap then aligns it with the next row.
	tt.Scroll(400, 300, 0, 26)
	tt.Frame()
	if got := m.historyScroll.Y; got != 26 {
		t.Fatalf("the move was snapped mid-input: offset %v", got)
	}
	rest()
	check("after 26 DIP", historyRowHeight860, 1)

	// Repeated moves keep snapping forward one whole row instead of
	// accumulating half rows.
	tt.Scroll(400, 300, 0, 26)
	rest()
	check("after another 26 DIP", 2*historyRowHeight860, 2)

	// A single small move below half a row stays where it is, so the next
	// one accumulates instead of being rounded away.
	tt.Scroll(400, 300, 0, 20)
	tt.Frame()
	if got := m.historyScroll.Y; got != 2*historyRowHeight860+20 {
		t.Fatalf("a small move did not accumulate: offset %v", got)
	}
	tt.Scroll(400, 300, 0, 20)
	tt.Frame()
	rest()
	check("after two small moves", 3*historyRowHeight860, 3)

	// Stopping between rows snaps to the nearest one once the debounce
	// elapses, so a short final move cannot leave half a row showing.
	tt.Scroll(400, 300, 0, 10)
	tt.Frame()
	if got := m.historyScroll.Y; got != 3*historyRowHeight860+10 {
		t.Fatalf("the short move did not stay for the rest snap: offset %v", got)
	}
	rest()
	check("after the rest snap", 3*historyRowHeight860, 3)
}

// TestHistorySnapStepRearmsAfterRepaint checks the rest debounce asks for
// the time left after a repaint between a precise scroll and the deadline.
// MyGo keeps only the timer the last frame armed: a frame that does not ask
// again stops the pending After, which left the native 860 table on a half
// row after the wheel had stopped.
func TestHistorySnapStepRearmsAfterRepaint(t *testing.T) {
	m := &Model{}
	m.historyScroll.Y = 26
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.Local)

	wait, snap := m.historyScrollSnapStep(now)
	if snap || wait != historySnapDelay {
		t.Fatalf("the first move: wait %v snap %v, want %v and no snap",
			wait, snap, historySnapDelay)
	}
	// The native window built another frame before the deadline. That frame
	// must ask for the time left, or MyGo stops the only pending timer and
	// the half row stays.
	wait, snap = m.historyScrollSnapStep(now.Add(20 * time.Millisecond))
	if snap {
		t.Fatal("a repaint snapped the move mid-input")
	}
	if want := historySnapDelay - 20*time.Millisecond; wait != want {
		t.Fatalf("the repaint asks for %v, want the remaining %v", wait, want)
	}

	wait, snap = m.historyScrollSnapStep(now.Add(historySnapDelay))
	if !snap || wait != 0 {
		t.Fatalf("at the deadline: wait %v snap %v, want the snap", wait, snap)
	}
	if got := historyRowOffset(m.historyScroll.Y); got != historyRowHeight860 {
		t.Fatalf("the snapped offset is %v, want %d", got, historyRowHeight860)
	}
}

// TestHistoryCompositeSnapSurvivesRepaintAt860 checks the same rest snap in
// the rendered 860 table: a repaint before the deadline must not leave the
// first and last rows cut in half.
func TestHistoryCompositeSnapSurvivesRepaintAt860(t *testing.T) {
	svc := &fakeService{history: historyTableRecords(12)}
	m, q := newTestModel(t, Config{Service: svc, DirectExportSaver: &fakeDirectExportSaver{}})
	m.SetContentSize(860, 560)
	tt := ui.NewTester(m.View, 860, 560)
	settle(t, m, q, tt)

	tt.Scroll(400, 300, 0, 26)
	tt.Frame()
	if got := m.historyScroll.Y; got != 26 {
		t.Fatalf("the move was snapped mid-input: offset %v", got)
	}

	// A repaint before the 120-ms deadline must re-arm the wait, not drop it.
	tt.Advance(20 * time.Millisecond)
	tt.Frame()
	if got := m.historyScroll.Y; got != 26 {
		t.Fatalf("the repaint moved the offset before the deadline: %v", got)
	}
	tt.Advance(historySnapDelay + 40*time.Millisecond)
	tt.Frame()

	if got := m.historyScroll.Y; got != historyRowHeight860 {
		t.Fatalf("after the repaint the rest snap left offset %v, want %d",
			got, historyRowHeight860)
	}
	first, last := m.historyList.Visible()
	if first != 1 || last-first+1 != 7 {
		t.Fatalf("visible rows = %d..%d, want 1..7 whole rows", first, last)
	}
}

// TestHistoryCompositeWheelKeepsFullTravelAt860 checks a mouse wheel's
// animating scroll: the composite may only snap after the glide stops, so
// a 120-DIP wheel moves at least two whole rows instead of being cut to
// the first row by a mid-animation offset write. The header and the fixed
// action column stay in place.
func TestHistoryCompositeWheelKeepsFullTravelAt860(t *testing.T) {
	for _, delta := range []float32{120, 600} {
		t.Run(fmt.Sprintf("delta%d", int(delta)), func(t *testing.T) {
			svc := &fakeService{history: historyTableRecords(40)}
			m, q := newTestModel(t, Config{Service: svc})
			m.SetContentSize(860, 560)
			tt := ui.NewTester(m.View, 860, 560)
			settle(t, m, q, tt)
			header, _ := tt.Find("时间")
			action, _ := tt.Find("操作")
			if m.historyScroll.Y != 0 {
				t.Fatalf("the table does not start at the top: Y=%v", m.historyScroll.Y)
			}

			tt.Wheel(450, 300, 0, delta)
			tt.Advance(time.Second)
			settle(t, m, q, tt)

			got := m.historyScroll.Y
			if math.Abs(float64(got-delta)) > historyRowHeight860/2 {
				t.Fatalf("wheel travel truncated beyond nearest-row quantization: requested=%v actual=%v",
					delta, got)
			}
			if math.Mod(float64(got), historyRowHeight860) != 0 {
				t.Fatalf("the wheel stopped between rows: Y=%v", got)
			}
			if first, _ := m.historyList.Visible(); first != int(math.Round(float64(got/historyRowHeight860))) {
				t.Fatalf("the list is not at the snapped row: Y=%v first=%d", got, first)
			}
			headerAfter, _ := tt.Find("时间")
			actionAfter, _ := tt.Find("操作")
			if headerAfter.Y != header.Y || actionAfter.X != action.X {
				t.Fatalf("the header/action column moved: %+v -> %+v, %+v -> %+v",
					header, headerAfter, action, actionAfter)
			}
		})
	}
}
