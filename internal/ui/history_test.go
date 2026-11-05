package ui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/LoTwT/youkai/internal/scan"
	"github.com/egoist/mygo/ui"
)

// openRecordsList opens the records destination's list. Startup always
// lands on the scan page, so tests that read the list navigate to it.
func openRecordsList(t *testing.T, m *Model, q *queueUpdater, tt *ui.Tester) {
	t.Helper()
	if err := tt.Click("记录"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
}

// historyTableRecords builds n saved records with distinct times, each
// completed with complete coverage and a rotating pending count. The
// newest is 10-21 14:02; every later row steps back 25 hours.
func historyTableRecords(n int) []app.HistoryRecord {
	base := time.Date(2026, 10, 21, 14, 2, 0, 0, time.Local)
	out := make([]app.HistoryRecord, 0, n)
	for i := 0; i < n; i++ {
		captured := int64(1008 - i)
		pending := int64(i % 3)
		out = append(out, app.HistoryRecord{
			ID:            fmt.Sprintf("r%02d", i+1),
			SavedAt:       base.Add(-time.Duration(i) * 25 * time.Hour),
			CapturedCount: captured,
			Coverage:      record.CoverageComplete,
			Termination: &record.Termination{
				Kind: record.TerminationCompleted, Code: "verifiedEnd", Phase: record.PhaseCapturing,
			},
			Counts:     app.ItemClassCounts{Passed: captured - pending, PendingReview: pending},
			TotalBytes: 1024 * 1024,
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

// TestHistoryListNewestFirst checks the list's order and that the newest
// row carries its conclusion: the view sorts by save time even when the
// service hands the records back shuffled, and the first row answers
// "what is this record" without reading counts (07 §4, 08 §5).
func TestHistoryListNewestFirst(t *testing.T) {
	records := historyTableRecords(3)
	records[0], records[2] = records[2], records[0]
	svc := &fakeService{history: records}
	m, q := newTestModel(t, Config{Service: svc})
	tt := ui.NewTester(m.View, 1180, 760)
	settle(t, m, q, tt)
	openRecordsList(t, m, q, tt)

	if m.history[0].ID != "r01" || m.history[2].ID != "r03" {
		t.Fatalf("history order = %q, %q, %q, want r01, r02, r03",
			m.history[0].ID, m.history[1].ID, m.history[2].ID)
	}
	newest, ok := tt.Find(historyTimeText(m.history[0]))
	if !ok {
		t.Fatalf("the newest row %q does not show: %q", historyTimeText(m.history[0]), tt.Texts())
	}
	second, ok := tt.Find(historyTimeText(m.history[1]))
	if !ok {
		t.Fatalf("the second row %q does not show", historyTimeText(m.history[1]))
	}
	if newest.Y >= second.Y {
		t.Fatalf("the newest row is not first: newest Y %.1f, second Y %.1f", newest.Y, second.Y)
	}
	if !tt.HasText("扫全了，可以直接导出") {
		t.Fatalf("the newest record's conclusion does not show: %q", tt.Texts())
	}
	if !tt.HasText("扫全了，1 件待校对") {
		t.Fatalf("the second record's conclusion does not show: %q", tt.Texts())
	}
}

// TestHistoryEmptyState checks the first-run empty list and its way to
// start a scan (08 §5).
func TestHistoryEmptyState(t *testing.T) {
	m, q := newTestModel(t, Config{Service: &fakeService{}})
	tt := ui.NewTester(m.View, 1180, 760)
	settle(t, m, q, tt)
	openRecordsList(t, m, q, tt)

	if !tt.HasText("还没有扫描记录") || !tt.HasText("去扫描页开始扫描") {
		t.Fatalf("the empty list does not explain itself: %q", tt.Texts())
	}
	if err := tt.Click("去扫描页开始扫描"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if m.displayPage() != pageScan {
		t.Fatalf("the empty list's action shows page %v, want scan", m.displayPage())
	}
}

// TestHistoryLoadingState checks the asynchronous list's loading state:
// the page answers before HistoryRecords returns and replaces the state
// with the rows when it does.
func TestHistoryLoadingState(t *testing.T) {
	release := make(chan struct{})
	svc := &fakeService{historyFn: func() ([]app.HistoryRecord, error) {
		<-release
		return historyTableRecords(2), nil
	}}
	m, q := newTestModel(t, Config{Service: svc})
	tt := ui.NewTester(m.View, 1180, 760)
	tt.Frame()
	if err := tt.Click("记录"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !m.historyLoading {
		t.Fatal("the list did not record its loading state")
	}
	if !tt.HasText("正在读取记录……") {
		t.Fatalf("the loading list shows no state: %q", tt.Texts())
	}
	if tt.HasText("扫全了") {
		t.Fatalf("the loading list shows rows before HistoryRecords returned: %q", tt.Texts())
	}

	close(release)
	settle(t, m, q, tt)
	if m.historyLoading || len(m.history) != 2 {
		t.Fatalf("the loaded list = loading %v, %d rows", m.historyLoading, len(m.history))
	}
	if !tt.HasText(historyTimeText(m.history[0])) || tt.HasText("正在读取记录……") {
		t.Fatalf("the loaded list does not replace its loading state: %q", tt.Texts())
	}
}

// TestHistoryDateFilter covers the DateInput filter's range and its
// no-results state, including the single 清除筛选 action the design asks
// for (08 §5).
func TestHistoryDateFilter(t *testing.T) {
	svc := &fakeService{history: historyTableRecords(4)}
	m, q := newTestModel(t, Config{Service: svc})
	tt := ui.NewTester(m.View, 1180, 760)
	settle(t, m, q, tt)
	openRecordsList(t, m, q, tt)

	if _, ok := tt.Find("开始日期"); !ok {
		t.Fatalf("the date filter is not a DateInput: %q", tt.Texts())
	}
	if tt.HasText("YYYY-MM-DD") || tt.HasText("格式应为") {
		t.Fatalf("the typed date filter is still on screen: %q", tt.Texts())
	}
	if len(m.visibleHistory()) != 4 || m.historyFilterActive {
		t.Fatalf("an untouched filter hides rows: active %v, %d visible",
			m.historyFilterActive, len(m.visibleHistory()))
	}

	m.historyFilterActive = true
	m.historyFilterStart = time.Date(2026, 12, 1, 0, 0, 0, 0, time.Local)
	m.historyFilterEnd = m.historyFilterStart
	tt.Frame()
	if len(m.visibleHistory()) != 0 {
		t.Fatalf("the gap-day filter kept %d rows", len(m.visibleHistory()))
	}
	if !tt.HasText("没有符合条件的记录") || !tt.HasText("清除筛选") {
		t.Fatalf("the no-results state does not show: %q", tt.Texts())
	}
	if tt.HasText(historyTimeText(m.history[0])) {
		t.Fatal("the filtered-out row still shows")
	}
	if count := countText(tt.Texts(), "清除筛选"); count != 1 {
		t.Fatalf("the clear action appears %d times, want once", count)
	}
	if err := tt.Click("清除筛选"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if m.historyFilterActive || len(m.visibleHistory()) != 4 {
		t.Fatalf("clearing the filter = active %v, %d visible", m.historyFilterActive, len(m.visibleHistory()))
	}
	if !tt.HasText(historyTimeText(m.history[0])) {
		t.Fatalf("clearing the filter did not restore the rows: %q", tt.Texts())
	}
}

// TestHistoryDateInputChangesFilter drives the DateInput itself: picking a
// later start day narrows the range without a typed date (08 §5, M6).
func TestHistoryDateInputChangesFilter(t *testing.T) {
	svc := &fakeService{history: historyTableRecords(4)}
	m, q := newTestModel(t, Config{Service: svc})
	tt := ui.NewTester(m.View, 1180, 760)
	settle(t, m, q, tt)
	openRecordsList(t, m, q, tt)

	if err := tt.Click("开始日期"); err != nil {
		t.Fatalf("open the start date calendar: %v", err)
	}
	tt.Frame()
	tt.Key(0, ui.KeyRight)
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if !m.historyFilterActive {
		t.Fatal("choosing a date did not activate the filter")
	}
	if want := historyDay(m.history[0].SavedAt).AddDate(0, 0, -2); !m.historyFilterStart.Equal(want) {
		t.Fatalf("the start date = %v, want %v", m.historyFilterStart, want)
	}
	if got := len(m.visibleHistory()); got != 3 {
		t.Fatalf("the chosen range shows %d records, want 3", got)
	}
	if tt.HasText(historyTimeText(m.history[3])) {
		t.Fatalf("the excluded record still shows: %q", tt.Texts())
	}
	if countText(tt.Texts(), "清除筛选") != 1 {
		t.Fatalf("the active filter shows no single clear action: %q", tt.Texts())
	}
	if err := tt.Click("清除筛选"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if m.historyFilterActive || len(m.visibleHistory()) != 4 {
		t.Fatalf("clearing from the header = active %v, %d visible",
			m.historyFilterActive, len(m.visibleHistory()))
	}
}

func countText(texts []string, want string) int {
	n := 0
	for _, text := range texts {
		if text == want {
			n++
		}
	}
	return n
}

// TestHistoryDamagedRowOnlyDeletes checks a record whose newest
// checkpoint is unreadable: it stays listed with its reason, cannot be
// opened or exported, and its menu deletes it (F-25).
func TestHistoryDamagedRowOnlyDeletes(t *testing.T) {
	svc := &fakeService{
		history: []app.HistoryRecord{{
			ID: "bad", SavedAt: time.Date(2026, 11, 1, 23, 40, 0, 0, time.Local),
			Unreadable: true, TotalBytes: 88 * 1024 * 1024,
		}},
		previewFn: func(kind, recordID string) (app.CleanupPreview, error) {
			return app.CleanupPreview{Records: 1, TotalBytes: 88 * 1024 * 1024}, nil
		},
	}
	m, q := newTestModel(t, Config{Service: svc, DirectExportSaver: &fakeDirectExportSaver{}})
	tt := ui.NewTester(m.View, 1180, 760)
	settle(t, m, q, tt)
	openRecordsList(t, m, q, tt)

	if !tt.HasText("无法打开：记录不可读") || !tt.HasText("88.0 MB") {
		t.Fatalf("the damaged row does not state its reason and size: %q", tt.Texts())
	}
	tt.Key(0, ui.KeyEnter)
	settle(t, m, q, tt)
	if m.displayPage() != pageHistory {
		t.Fatalf("Enter opened the damaged record: page %v", m.displayPage())
	}

	m.openHistoryMenu("bad")
	tt.Frame()
	for _, text := range []string{"打开", "导出…", "删除截图…"} {
		if countText(tt.Texts(), text) != 0 {
			t.Errorf("the damaged row's menu offers %q: %q", text, tt.Texts())
		}
	}
	if !tt.HasText("记录不可读，只能删除。") || !tt.HasText("删除记录…") {
		t.Fatalf("the damaged row's menu is incomplete: %q", tt.Texts())
	}
	if err := tt.Click("删除记录…"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if m.cleanup == nil {
		t.Fatal("the damaged row's delete action did not open the measured cleanup")
	}
	if !tt.HasText("将永久删除记录 bad") {
		t.Fatalf("the deletion preview does not show: %q", tt.Texts())
	}
}

// TestHistoryDeleteBlockedReasons checks that a blocked deletion says
// why, both for the live session's row and while a scan owns the app
// (C-28, 08 §5).
func TestHistoryDeleteBlockedReasons(t *testing.T) {
	svc := &fakeService{history: historyTableRecords(2)}
	m, q := newTestModel(t, Config{Service: svc, DirectExportSaver: &fakeDirectExportSaver{}})
	m.Apply(snap("r01", 1, false, scan.PhaseEnded, scan.Counts{Captured: 1}, 1, false, ""))
	m.page = pageHistory
	tt := ui.NewTester(m.View, 1180, 760)
	settle(t, m, q, tt)

	m.openHistoryMenu("r01")
	tt.Frame()
	if !tt.HasText("当前会话记录，扫描中或未转移前不能删除。") {
		t.Fatalf("the live row's deletion reason does not show: %q", tt.Texts())
	}
	if err := tt.Click("删除记录…"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if m.cleanup != nil {
		t.Fatal("the disabled live-row deletion opened a cleanup prompt")
	}

	m.closeHistoryMenu()
	m.Apply(snap("live", 1, true, scan.PhaseCapturing, scan.Counts{Captured: 1}, 0, false, ""))
	tt.Frame()
	m.openHistoryMenu("r02")
	tt.Frame()
	if !tt.HasText("扫描会话进行中，结束后才能清理。") {
		t.Fatalf("the active scan's deletion reason does not show: %q", tt.Texts())
	}
	if err := tt.Click("删除截图…"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if m.cleanup != nil {
		t.Fatal("a deletion while the scan runs opened a prompt")
	}
}

// TestHistoryKeyboard checks the list's focus order: entering puts the
// keyboard on the first row, arrows move the choice, Enter opens it, and
// Shift+F10 opens the chosen row's menu (08 §5, 09 §4.4).
func TestHistoryKeyboard(t *testing.T) {
	svc := &fakeService{
		history: historyTableRecords(3),
		loads:   map[string]*record.Recovery{"r01": testRecord()},
	}
	m, q := newTestModel(t, Config{Service: svc, DirectExportSaver: &fakeDirectExportSaver{}})
	tt := ui.NewTester(m.View, 1180, 760)
	settle(t, m, q, tt)
	openRecordsList(t, m, q, tt)

	if m.historyRow != 0 || !tt.Focused(historyTimeText(m.history[0])) {
		t.Fatalf("entering the list did not focus its first row (row %d)", m.historyRow)
	}
	tt.Key(0, ui.KeyDown)
	if m.historyRow != 1 {
		t.Fatalf("Down moved the selection to %d", m.historyRow)
	}
	tt.Key(0, ui.KeyUp)
	if m.historyRow != 0 {
		t.Fatalf("Up moved the selection to %d", m.historyRow)
	}
	tt.Key(0, ui.KeyEnter)
	settle(t, m, q, tt)
	if m.displayPage() != pageResults || m.historySlot.recordID != "r01" {
		t.Fatalf("Enter opened page %v, record %q", m.displayPage(), m.historySlot.recordID)
	}

	if err := tt.Click("‹ 全部记录"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if m.displayPage() != pageHistory || !tt.Focused(historyTimeText(m.history[0])) {
		t.Fatalf("returning to the list did not restore the table's focus")
	}
	tt.Key(ui.Shift, ui.KeyF10)
	tt.Frame()
	if !m.historyMenuOpen || m.historyMenuID != "r01" {
		t.Fatalf("Shift+F10 did not open row r01's menu: open %v, id %q",
			m.historyMenuOpen, m.historyMenuID)
	}
	if !tt.Focused("打开") {
		t.Fatal("the row menu does not take the keyboard focus")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if m.historyMenuOpen {
		t.Fatal("Escape did not close the row menu")
	}
}

// TestHistoryColumnsByWidth guards the two tiers of 09 §2: the standard
// list shows 占用 and separate 待处理 / 可导出 columns; the compact list
// merges the counts and drops 占用.
func TestHistoryColumnsByWidth(t *testing.T) {
	standard := historyHeaderTitles(historyColumnsFor(1180))
	wantStandard := []string{"时间", "结果", "件数", "待处理", "可导出", "占用", ""}
	if strings.Join(standard, "/") != strings.Join(wantStandard, "/") {
		t.Fatalf("1180 columns = %v, want %v", standard, wantStandard)
	}
	compact := historyHeaderTitles(historyColumnsFor(960))
	wantCompact := []string{"时间", "结果", "件数", "待处理 / 可导出", ""}
	if strings.Join(compact, "/") != strings.Join(wantCompact, "/") {
		t.Fatalf("960 columns = %v, want %v", compact, wantCompact)
	}
	if !historyCompact(960) || historyCompact(1180) {
		t.Fatalf("compact tiers: 960 %v, 1180 %v", historyCompact(960), historyCompact(1180))
	}
}

// TestHistoryCompactMovesStorageIntoTheMenu checks the compact tier's
// trade: 占用 leaves the table and its number appears in the screenshot
// deletion entry, while the standard tier keeps both (09 §2).
func TestHistoryCompactMovesStorageIntoTheMenu(t *testing.T) {
	for _, tc := range []struct {
		name    string
		width   int
		storage bool
	}{
		{"standard", 1180, true},
		{"compact", 960, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &fakeService{history: historyTableRecords(1)}
			m, q := newTestModel(t, Config{Service: svc, DirectExportSaver: &fakeDirectExportSaver{}})
			m.SetContentSize(tc.width, 760)
			tt := ui.NewTester(m.View, tc.width, 760)
			settle(t, m, q, tt)
			openRecordsList(t, m, q, tt)

			if got := tt.HasText("占用"); got != tc.storage {
				t.Fatalf("占用 shown = %v, want %v", got, tc.storage)
			}
			if tt.HasText("待处理 / 可导出") == tc.storage {
				t.Fatalf("merged counts shown = %v at %d", tt.HasText("待处理 / 可导出"), tc.width)
			}
			m.openHistoryMenu("r01")
			tt.Frame()
			want := "删除截图…"
			if !tc.storage {
				want += "（1.0 MB）"
			}
			if !tt.HasText(want) {
				t.Fatalf("the delete entry does not show %q: %q", want, tt.Texts())
			}
		})
	}
}

// TestHistoryResultConclusions covers the 结果 column's conclusion texts:
// they use the four-class counts from PR-02 and localize the termination
// without leaking a fault code (08 §5, 09 §4.3).
func TestHistoryResultConclusions(t *testing.T) {
	completed := &record.Termination{
		Kind: record.TerminationCompleted, Code: "verifiedEnd", Phase: record.PhaseCapturing,
	}
	cases := []struct {
		name  string
		entry HistoryEntry
		tag   string
		text  string
	}{
		{
			name:  "unreadable",
			entry: HistoryEntry{Unreadable: true},
			tag:   "!",
			text:  "无法打开：记录不可读",
		},
		{
			name:  "interrupted",
			entry: HistoryEntry{Interrupted: true, CapturedCount: 3},
			tag:   "中断",
			text:  "已恢复已保存部分",
		},
		{
			name: "all pass",
			entry: HistoryEntry{
				Termination: completed, Coverage: record.CoverageComplete,
				CapturedCount: 1_017, Counts: app.ItemClassCounts{Passed: 1_017},
			},
			text: "扫全了，可以直接导出",
		},
		{
			name: "one pending review",
			entry: HistoryEntry{
				Termination: completed, Coverage: record.CoverageComplete,
				CapturedCount: 1_017,
				Counts:        app.ItemClassCounts{Passed: 1_016, PendingReview: 1},
			},
			text: "扫全了，1 件待校对",
		},
		{
			name: "pending backfill and review",
			entry: HistoryEntry{
				Termination: completed, Coverage: record.CoverageComplete,
				CapturedCount: 8,
				Counts:        app.ItemClassCounts{Passed: 6, PendingBackfill: 1, PendingReview: 1},
			},
			text: "扫全了，2 件待处理",
		},
		{
			name: "warnings stay exportable",
			entry: HistoryEntry{
				Termination: completed, Coverage: record.CoverageComplete,
				CapturedCount: 8, Counts: app.ItemClassCounts{Passed: 6, Warning: 2},
			},
			text: "扫全了，2 件有警告",
		},
		{
			name: "partial coverage",
			entry: HistoryEntry{
				Termination: completed, Coverage: record.CoveragePartial,
				CapturedCount: 6, Counts: app.ItemClassCounts{Passed: 6},
			},
			text: "没有扫全 · 覆盖：部分",
		},
		{
			name: "partial coverage with known positions",
			entry: HistoryEntry{
				Termination: completed, Coverage: record.CoveragePartial,
				CapturedCount: 6, Counts: app.ItemClassCounts{Passed: 6},
				IncompletePositions: 2, IncompletePositionsKnown: true,
			},
			text: "没有扫全 · 2 个号位不完整",
		},
		{
			name: "manual stop",
			entry: HistoryEntry{
				Termination: &record.Termination{
					Kind: record.TerminationCancelled, Code: "userCancelled", Phase: record.PhaseCapturing,
				},
				Coverage: record.CoveragePartial, CapturedCount: 2,
				Counts: app.ItemClassCounts{Passed: 2},
			},
			text: "手动停止 · 覆盖：部分",
		},
		{
			name: "fault while capturing",
			entry: HistoryEntry{
				Termination: &record.Termination{
					Kind: record.TerminationFailed, Code: "focusLost", Phase: record.PhaseCapturing,
				},
				Coverage: record.CoveragePartial, CapturedCount: 431,
				Counts: app.ItemClassCounts{Passed: 429, PendingReview: 2},
			},
			tag:  "失败",
			text: "中途停止：游戏失去了焦点",
		},
		{
			name: "stopped before starting",
			entry: HistoryEntry{
				Termination: &record.Termination{
					Kind: record.TerminationFailed, Code: "prepareTimeout", Phase: record.PhasePreparing,
				},
				Coverage: record.CoverageUnknown,
			},
			tag:  "失败",
			text: "没有开始：30 秒内游戏没有就绪",
		},
	}
	m := New(Config{Service: &fakeService{}})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := m.historyResultFor(tc.entry)
			if got.tag != tc.tag || got.text != tc.text {
				t.Fatalf("result = %q/%q, want %q/%q", got.tag, got.text, tc.tag, tc.text)
			}
		})
	}
}

// TestHistoryRowMenu checks the normal row's menu, its disabled deletion
// reason and the existing export entry behind 导出… (PR-15 replaces it
// with the dialog).
func TestHistoryRowMenu(t *testing.T) {
	svc := &fakeService{
		history: historyTableRecords(2),
		loads:   map[string]*record.Recovery{"r01": testRecord()},
		previewFn: func(kind, recordID string) (app.CleanupPreview, error) {
			return app.CleanupPreview{Records: 1, EvidenceFiles: 2, EvidenceBytes: 1024, TotalBytes: 2048}, nil
		},
	}
	m, q := newTestModel(t, Config{Service: svc, DirectExportSaver: &fakeDirectExportSaver{}})
	tt := ui.NewTester(m.View, 1180, 760)
	settle(t, m, q, tt)
	openRecordsList(t, m, q, tt)

	m.openHistoryMenu("r02")
	tt.Frame()
	for _, text := range []string{"打开", "导出…", "删除截图…", "删除记录…"} {
		if !tt.HasText(text) {
			t.Errorf("the row menu does not show %q: %q", text, tt.Texts())
		}
	}
	if tt.HasText("当前会话记录") {
		t.Fatal("a normal record's menu kept a live-session reason")
	}
	if got := m.historyDeleteBlockedReason(m.history[1]); got != "" {
		t.Fatalf("a normal record's delete entries are blocked: %q", got)
	}
	if err := tt.Click("删除记录…"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if m.cleanup == nil {
		t.Fatal("the row's delete entry did not open the measured cleanup")
	}

	m.closeHistoryMenu()
	m.cleanup = nil
	m.openHistoryMenu("r01")
	tt.Frame()
	if err := tt.Click("导出…"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if m.displayPage() != pageResults || m.historySlot.recordID != "r01" {
		t.Fatalf("导出… = page %v, record %q, want the record's existing export entry",
			m.displayPage(), m.historySlot.recordID)
	}
	if !tt.HasText("导出 JSON") || !tt.HasText("导出 CSV") {
		t.Fatalf("the existing export controls do not show: %q", tt.Texts())
	}
}

// TestHistoryCurrentRowOpensLiveSession checks that the 本次 row opens the
// session slot and never creates a second history slot.
func TestHistoryCurrentRowOpensLiveSession(t *testing.T) {
	svc := &fakeService{
		history: historyTableRecords(3),
		loads:   map[string]*record.Recovery{"r01": testRecord()},
	}
	m, q := newTestModel(t, Config{Service: svc, DirectExportSaver: &fakeDirectExportSaver{}})
	m.Apply(snap("r01", 1, false, scan.PhaseEnded, scan.Counts{Captured: 1}, 1, false, ""))
	m.page = pageHistory
	tt := ui.NewTester(m.View, 1180, 760)
	settle(t, m, q, tt)

	if !tt.HasText("本次") {
		t.Fatalf("the current session row has no 本次 badge: %q", tt.Texts())
	}
	if err := tt.Click(historyTimeText(m.history[0])); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyEnter)
	settle(t, m, q, tt)
	if m.displayPage() != pageResults || m.activeSlot != resultSlotSession {
		t.Fatalf("the current row opened page %v, slot %v", m.displayPage(), m.activeSlot)
	}
	if m.historySlot.recordID != "" {
		t.Fatalf("the current row created a history slot for %q", m.historySlot.recordID)
	}
}

// TestHistoryOpenAndReturn checks row selection, Enter opening the chosen
// record, and returning to the same selection from the workspace.
func TestHistoryOpenAndReturn(t *testing.T) {
	svc := &fakeService{
		history: historyTableRecords(4),
		loads:   map[string]*record.Recovery{"r02": testRecord()},
	}
	m, q := newTestModel(t, Config{Service: svc, DirectExportSaver: &fakeDirectExportSaver{}})
	tt := ui.NewTester(m.View, 1180, 760)
	settle(t, m, q, tt)
	openRecordsList(t, m, q, tt)

	if err := tt.Click(historyTimeText(m.history[1])); err != nil {
		t.Fatal(err)
	}
	if m.historyRow != 1 {
		t.Fatalf("clicking the second row selected %d", m.historyRow)
	}
	tt.Key(0, ui.KeyEnter)
	settle(t, m, q, tt)
	if m.displayPage() != pageResults || m.historySlot.recordID != "r02" {
		t.Fatalf("Enter opened page %v, record %q", m.displayPage(), m.historySlot.recordID)
	}

	if err := tt.Click("‹ 全部记录"); err != nil {
		t.Fatalf("click the workspace back action: %v", err)
	}
	settle(t, m, q, tt)
	if m.displayPage() != pageHistory || m.historyRow != 1 {
		t.Fatalf("returning to history = page %v, row %d", m.displayPage(), m.historyRow)
	}
	if !tt.HasText("已选中 " + historyTimeText(m.history[1])) {
		t.Fatalf("the status bar lost the selection: %q", tt.Texts())
	}
}

// TestHistoryExportFailure keeps the per-row export's failure state: the
// in-flight marker clears and the error is reported globally.
func TestHistoryExportFailure(t *testing.T) {
	svc := &fakeService{
		history: historyTableRecords(1),
		loads: map[string]*record.Recovery{
			"r01": recovery(uiStructuredItem(t, 1, record.QualityAutoAccepted)),
		},
	}
	saver := &fakeDirectExportSaver{}
	m, q := newTestModel(t, Config{Service: svc, DirectExportSaver: saver, AppVersion: "test"})
	tt := ui.NewTester(m.View, 1180, 760)
	settle(t, m, q, tt)

	saver.saveFn = func(app.ExportFormat, string, []byte) (string, error) {
		return "", errors.New("no space left on device")
	}
	m.ExportHistory("r01", app.ExportJSON)
	settle(t, m, q, tt)
	if m.historyExporting != "" || m.exportOutcome == nil || m.exportOutcome.err == nil ||
		!strings.Contains(m.exportOutcome.text(), "no space left on device") {
		t.Fatalf("failure state = exporting %q, outcome %+v", m.historyExporting, m.exportOutcome)
	}
}

// TestHistoryTableFitsWidths guards the two tiers' geometry: every header
// and the row action stay inside the page at the default and minimum
// window sizes, without horizontal scrolling.
func TestHistoryTableFitsWidths(t *testing.T) {
	for _, width := range []int{1180, 960} {
		t.Run(fmt.Sprintf("%d", width), func(t *testing.T) {
			svc := &fakeService{history: historyTableRecords(3)}
			m, q := newTestModel(t, Config{Service: svc, DirectExportSaver: &fakeDirectExportSaver{}})
			m.SetContentSize(width, 760)
			tt := ui.NewTester(m.View, width, 760)
			settle(t, m, q, tt)
			openRecordsList(t, m, q, tt)

			for _, title := range historyHeaderTitles(historyColumnsFor(width)) {
				if title == "" {
					continue
				}
				rect, ok := tt.Find(title)
				if !ok {
					t.Fatalf("%q is missing at %d", title, width)
				}
				if rect.X < navRailWidth+12 || rect.X+rect.W > float32(width)-12 {
					t.Errorf("%q at %+v is outside the %d page", title, rect, width)
				}
			}
			action, ok := tt.Find("⋯")
			if !ok || action.X+action.W > float32(width)-12 {
				t.Fatalf("the row action at %+v does not fit %d", action, width)
			}
			if width >= historyStandardMinWidth && tt.HasText("待处理 / 可导出") {
				t.Fatal("the wide table still merges the counts")
			}
			if width < historyStandardMinWidth && tt.HasText("占用") {
				t.Fatal("the compact table still shows the storage column")
			}
		})
	}
}

// TestHistoryPerRowExportUsesThatRecord keeps
// the existing per-record export path working from the list.
func TestHistoryPerRowExportUsesThatRecord(t *testing.T) {
	svc := &fakeService{
		history: historyTableRecords(2),
		loads: map[string]*record.Recovery{
			"r01": recovery(uiStructuredItem(t, 1, record.QualityAutoAccepted)),
		},
	}
	saver := &fakeDirectExportSaver{}
	m, q := newTestModel(t, Config{Service: svc, DirectExportSaver: saver, AppVersion: "test"})
	tt := ui.NewTester(m.View, 1180, 760)
	settle(t, m, q, tt)

	m.ExportHistory("r01", app.ExportJSON)
	settle(t, m, q, tt)
	calls := saver.snapshot()
	if len(calls) != 1 || calls[0].format != app.ExportJSON {
		t.Fatalf("export calls = %+v, want one JSON export", calls)
	}
	if m.historyExporting != "" || m.exportOutcome == nil || m.exportOutcome.err != nil {
		t.Fatalf("export state = %q / %+v", m.historyExporting, m.exportOutcome)
	}
	if !tt.HasText("已导出 JSON（1 件，历史记录 r01）") {
		t.Fatalf("history export result does not show: %q", tt.Texts())
	}
}
