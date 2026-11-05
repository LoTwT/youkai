package ui

import (
	"errors"
	"fmt"
	"image"
	"strings"
	"sync"
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

// waitForHistoryUpdate waits until the model queued one update, then runs
// it. Tests that keep a load blocked cannot use settle: Idle would wait for
// the blocked load.
func waitForHistoryUpdate(t *testing.T, q *queueUpdater) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for q.empty() {
		if time.Now().After(deadline) {
			t.Fatal("the model did not queue a history update")
		}
		time.Sleep(time.Millisecond)
	}
	for _, fn := range q.take() {
		fn()
	}
}

// TestHistoryLoadsDoNotApplyOutOfOrder starts two loads, lets the older
// one finish first, and checks that it neither replaces the newer list nor
// clears the newer load's loading state.
func TestHistoryLoadsDoNotApplyOutOfOrder(t *testing.T) {
	initial := historyTableRecords(1)
	stale := historyTableRecords(2)
	fresh := historyTableRecords(3)

	var mu sync.Mutex
	calls := 0
	started := make(chan int, 4)
	releaseStale := make(chan struct{})
	releaseFresh := make(chan struct{})
	svc := &fakeService{}
	svc.historyFn = func() ([]app.HistoryRecord, error) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		started <- n
		switch n {
		case 1:
			return initial, nil
		case 2:
			<-releaseStale
			return stale, nil
		default:
			<-releaseFresh
			return fresh, nil
		}
	}
	m, q := newTestModel(t, Config{Service: svc})
	tt := ui.NewTester(m.View, 1180, 760)
	settle(t, m, q, tt)
	if n := <-started; n != 1 {
		t.Fatalf("the startup load is call %d, want 1", n)
	}
	if len(m.history) != 1 {
		t.Fatalf("the startup load shows %d records", len(m.history))
	}

	m.loadHistory()
	if n := <-started; n != 2 {
		t.Fatalf("the slow load is call %d, want 2", n)
	}
	m.loadHistory()
	if n := <-started; n != 3 {
		t.Fatalf("the fresh load is call %d, want 3", n)
	}
	if !m.historyLoading {
		t.Fatal("two in-flight loads left the list not loading")
	}

	// The older load lands first. Its result and its loading-state change
	// both belong to a load that no longer owns the list.
	close(releaseStale)
	waitForHistoryUpdate(t, q)
	if len(m.history) != 1 {
		t.Fatalf("the stale load replaced the list: %d records", len(m.history))
	}
	if !m.historyLoading {
		t.Fatal("the stale load cleared the newer load's loading state")
	}

	close(releaseFresh)
	waitForHistoryUpdate(t, q)
	if len(m.history) != 3 {
		t.Fatalf("the fresh load shows %d records, want 3", len(m.history))
	}
	if m.historyLoading {
		t.Fatal("the fresh load did not clear the loading state")
	}
	m.Idle()
}

// TestHistoryNumberColumnsShowReadableValues checks the standard tier's
// visible 件数 and 可导出 cells, not only the row's accessibility label. The
// two numbers differ on purpose: a column swap must fail the order check.
func TestHistoryNumberColumnsShowReadableValues(t *testing.T) {
	completed := &record.Termination{
		Kind: record.TerminationCompleted, Code: "verifiedEnd", Phase: record.PhaseCapturing,
	}
	svc := &fakeService{history: []app.HistoryRecord{{
		ID: "r01", SavedAt: time.Date(2026, 10, 21, 14, 2, 0, 0, time.Local),
		CapturedCount: 1_007, Coverage: record.CoverageComplete, Termination: completed,
		Counts: app.ItemClassCounts{Passed: 1_006, PendingReview: 1}, TotalBytes: 1024,
	}}}
	m, q := newTestModel(t, Config{Service: svc})
	m.SetContentSize(1180, 760)
	tt := ui.NewTester(m.View, 1180, 760)
	settle(t, m, q, tt)
	openRecordsList(t, m, q, tt)

	counts, countsOK := tt.Find("1,007")
	exported, exportedOK := tt.Find("1,006")
	if !countsOK || !exportedOK {
		t.Fatalf("visible 件数/可导出 cells are missing: %q", tt.Texts())
	}
	if countText(tt.Texts(), "1,007") != 1 || countText(tt.Texts(), "1,006") != 1 {
		t.Fatalf("visible 件数/可导出 cells are duplicated or missing: %q", tt.Texts())
	}
	if counts.X >= exported.X {
		t.Fatalf("件数 and 可导出 are swapped: 1,007 at %.1f, 1,006 at %.1f",
			counts.X, exported.X)
	}
}

// TestHistoryStaleLoadErrorIsDropped checks the error branch of the load
// sequence: an older load's error must not reach the list after a newer
// load owns it.
func TestHistoryStaleLoadErrorIsDropped(t *testing.T) {
	initial := historyTableRecords(1)
	fresh := historyTableRecords(3)

	var mu sync.Mutex
	calls := 0
	started := make(chan int, 4)
	releaseStale := make(chan struct{})
	releaseFresh := make(chan struct{})
	svc := &fakeService{}
	svc.historyFn = func() ([]app.HistoryRecord, error) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		started <- n
		switch n {
		case 1:
			return initial, nil
		case 2:
			<-releaseStale
			return nil, errors.New("stale history read failed")
		default:
			<-releaseFresh
			return fresh, nil
		}
	}
	m, q := newTestModel(t, Config{Service: svc})
	tt := ui.NewTester(m.View, 1180, 760)
	settle(t, m, q, tt)
	if n := <-started; n != 1 {
		t.Fatalf("the startup load is call %d, want 1", n)
	}

	m.loadHistory()
	if n := <-started; n != 2 {
		t.Fatalf("the slow load is call %d, want 2", n)
	}
	m.loadHistory()
	if n := <-started; n != 3 {
		t.Fatalf("the fresh load is call %d, want 3", n)
	}

	close(releaseStale)
	waitForHistoryUpdate(t, q)
	if m.histErr != "" {
		t.Fatalf("the stale load's error reached the list: %q", m.histErr)
	}
	if len(m.history) != 1 {
		t.Fatalf("the stale load replaced the list: %d records", len(m.history))
	}
	if !m.historyLoading {
		t.Fatal("the stale load cleared the newer load's loading state")
	}

	close(releaseFresh)
	waitForHistoryUpdate(t, q)
	if m.histErr != "" {
		t.Fatalf("the fresh load reports the stale error: %q", m.histErr)
	}
	if len(m.history) != 3 || m.historyLoading {
		t.Fatalf("the fresh load = %d records, loading %v", len(m.history), m.historyLoading)
	}
	m.Idle()
}

// minLuminanceInRect is the darkest text pixel in a rect, used to tell a
// primary-color time from a muted one.
func minLuminanceInRect(img *image.RGBA, r ui.Rect) int {
	rect := image.Rect(int(r.X), int(r.Y), int(r.X+r.W), int(r.Y+r.H)).Intersect(img.Bounds())
	darkest := 255
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			p := img.RGBAAt(x, y)
			lum := (299*int(p.R) + 587*int(p.G) + 114*int(p.B)) / 1000
			if lum < darkest {
				darkest = lum
			}
		}
	}
	return darkest
}

// TestHistoryDamagedRowTimeIsMuted guards the wireframe's muted time on a
// damaged row: its date recedes beside its reason (线框 10).
func TestHistoryDamagedRowTimeIsMuted(t *testing.T) {
	records := historyTableRecords(2)
	records = append(records, app.HistoryRecord{
		ID: "bad", SavedAt: time.Date(2026, 10, 19, 12, 2, 0, 0, time.Local),
		Unreadable: true, TotalBytes: 88 * 1024 * 1024,
	})
	svc := &fakeService{history: records}
	m, q := newTestModel(t, Config{Service: svc})
	m.SetContentSize(1180, 760)
	tt := ui.NewTester(m.View, 1180, 760)
	settle(t, m, q, tt)
	openRecordsList(t, m, q, tt)

	normal, ok := tt.Find(historyTimeText(m.history[1]))
	if !ok {
		t.Fatalf("the normal row's time is missing: %q", tt.Texts())
	}
	damaged, ok := tt.Find(historyTimeText(m.history[2]))
	if !ok {
		t.Fatalf("the damaged row's time is missing: %q", tt.Texts())
	}
	img := tt.Image()
	normalMin := minLuminanceInRect(img, normal)
	damagedMin := minLuminanceInRect(img, damaged)
	if damagedMin < normalMin+40 {
		t.Fatalf("the damaged row's time is not muted: normal min %d, damaged min %d",
			normalMin, damagedMin)
	}
}

// TestHistoryNumberColumnsDashDamagedRow checks that a damaged row shows
// the design's dash in 件数, 待处理 and 可导出, and invents no zero.
func TestHistoryNumberColumnsDashDamagedRow(t *testing.T) {
	svc := &fakeService{history: []app.HistoryRecord{{
		ID: "bad", SavedAt: time.Date(2026, 11, 1, 23, 40, 0, 0, time.Local),
		Unreadable: true, TotalBytes: 88 * 1024 * 1024,
	}}}
	m, q := newTestModel(t, Config{Service: svc})
	m.SetContentSize(1180, 760)
	tt := ui.NewTester(m.View, 1180, 760)
	settle(t, m, q, tt)
	openRecordsList(t, m, q, tt)

	if got := countText(tt.Texts(), "—"); got != 3 {
		t.Fatalf("the damaged row shows %d dashes, want 3 (件数/待处理/可导出): %q", got, tt.Texts())
	}
	if got := countText(tt.Texts(), "0"); got != 0 {
		t.Fatalf("the damaged row invents a zero: %q", tt.Texts())
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
	if m.displayPage() != pageResults || m.rec == nil || m.rec.ID != "r01" {
		t.Fatalf("Enter opened page %v, record %+v", m.displayPage(), m.rec)
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

// TestRecordConclusion covers the shared conclusion texts: they use the
// four-class counts and localize the termination without leaking a fault
// code (08 §5, 09 §4.3). The records list and the summary share this
// function.
func TestRecordConclusion(t *testing.T) {
	completed := &record.Termination{
		Kind: record.TerminationCompleted, Code: "verifiedEnd", Phase: record.PhaseCapturing,
	}
	knownPartial := []app.PositionOutcome{
		{Position: 1, State: app.PositionOutcomeDegraded, Stage: "filter", ReasonCode: "navigationMismatch"},
		{Position: 2, State: app.PositionOutcomeTruncated},
		{Position: 3, State: app.PositionOutcomeComplete},
		{Position: 4, State: app.PositionOutcomeComplete},
		{Position: 5, State: app.PositionOutcomeComplete},
		{Position: 6, State: app.PositionOutcomeComplete},
	}
	cases := []struct {
		name              string
		input             recordConclusionInput
		wantOutcome       recordOutcomeKind
		tag               string
		text              string
		summaryEyebrow    string
		summaryTitle      string
		summarySupplement string
		summaryNotes      string
	}{
		{
			name:         "unreadable",
			input:        recordConclusionInput{Unreadable: true},
			wantOutcome:  recordOutcomeUnreadable,
			tag:          "!",
			text:         "无法打开：记录不可读",
			summaryTitle: "记录读取失败",
		},
		{
			name:         "interrupted",
			input:        recordConclusionInput{Interrupted: true, CapturedCount: 3},
			wantOutcome:  recordOutcomeInterrupted,
			tag:          "中断",
			text:         "已恢复已保存部分",
			summaryTitle: "扫描中断，只保存了部分结果",
		},
		{
			name: "all pass",
			input: recordConclusionInput{
				Termination: completed, Coverage: record.CoverageComplete,
				CapturedCount: 1_017, Counts: app.ItemClassCounts{Passed: 1_017},
			},
			wantOutcome:    recordOutcomeCompleted,
			summaryEyebrow: "正常结束",
			text:           "扫全了，可以直接导出",
			summaryTitle:   "扫全了，可以直接导出",
		},
		{
			name: "one pending review",
			input: recordConclusionInput{
				Termination: completed, Coverage: record.CoverageComplete,
				CapturedCount: 1_017,
				Counts:        app.ItemClassCounts{Passed: 1_016, PendingReview: 1},
			},
			wantOutcome:    recordOutcomeCompleted,
			summaryEyebrow: "正常结束",
			text:           "扫全了，1 件待校对",
			summaryTitle:   "扫全了，还有 1 件待校对",
		},
		{
			name: "pending backfill and review",
			input: recordConclusionInput{
				Termination: completed, Coverage: record.CoverageComplete,
				CapturedCount: 8,
				Counts:        app.ItemClassCounts{Passed: 6, PendingBackfill: 1, PendingReview: 1},
			},
			wantOutcome:    recordOutcomeCompleted,
			summaryEyebrow: "正常结束",
			text:           "扫全了，2 件待处理",
			summaryTitle:   "扫全了，还有 1 件待校对、1 件待补录",
		},
		{
			name: "warnings stay exportable",
			input: recordConclusionInput{
				Termination: completed, Coverage: record.CoverageComplete,
				CapturedCount: 8, Counts: app.ItemClassCounts{Passed: 6, Warning: 2},
			},
			wantOutcome:    recordOutcomeCompleted,
			summaryEyebrow: "正常结束",
			text:           "扫全了，2 件有警告",
			summaryTitle:   "扫全了，可以直接导出",
		},
		{
			name: "partial coverage",
			input: recordConclusionInput{
				Termination: completed, Coverage: record.CoveragePartial,
				CapturedCount: 6, Counts: app.ItemClassCounts{Passed: 6},
			},
			wantOutcome:    recordOutcomeCompleted,
			summaryEyebrow: "正常结束",
			text:           "没有扫全 · 覆盖：部分",
			summaryTitle:   "没有扫全：覆盖不完整",
		},
		{
			name: "partial coverage with known positions",
			input: recordConclusionInput{
				Termination: completed, Coverage: record.CoveragePartial,
				CapturedCount: 6, Counts: app.ItemClassCounts{Passed: 6},
				IncompletePositions: 2, IncompletePositionsKnown: true,
				PositionOutcomes: knownPartial, PositionOutcomesKnown: true,
			},
			wantOutcome:    recordOutcomeCompleted,
			summaryEyebrow: "正常结束",
			text:           "没有扫全 · 2 个号位不完整",
			summaryTitle:   "没有扫全：1 号位、2 号位不完整",
			summaryNotes:   "其余 4 个号位完整",
		},
		{
			name: "manual stop",
			input: recordConclusionInput{
				Termination: &record.Termination{
					Kind: record.TerminationCancelled, Code: "userCancelled", Phase: record.PhaseCapturing,
				},
				Coverage: record.CoveragePartial, CapturedCount: 2,
				Counts: app.ItemClassCounts{Passed: 2},
			},
			wantOutcome:  recordOutcomeCancelled,
			text:         "手动停止 · 覆盖：部分",
			summaryTitle: "已手动停止",
		},
		{
			name: "manual stop with complete coverage",
			input: recordConclusionInput{
				Termination: &record.Termination{
					Kind: record.TerminationCancelled, Code: "userCancelled", Phase: record.PhaseCapturing,
				},
				Coverage: record.CoverageComplete, CapturedCount: 2,
				Counts: app.ItemClassCounts{Passed: 2},
			},
			wantOutcome:  recordOutcomeCancelled,
			text:         "手动停止 · 覆盖：全部",
			summaryTitle: "已手动停止",
		},
		{
			name: "completed record without four-class counts",
			input: recordConclusionInput{
				Termination: completed, Coverage: record.CoverageComplete,
				CapturedCount: 5,
			},
			wantOutcome:    recordOutcomeCompleted,
			text:           "正常结束 · 覆盖：全部",
			summaryEyebrow: "正常结束",
			summaryTitle:   "扫全了，可以直接导出",
		},
		{
			name: "fault while capturing",
			input: recordConclusionInput{
				Termination: &record.Termination{
					Kind: record.TerminationFailed, Code: "focusLost", Phase: record.PhaseCapturing,
				},
				Coverage: record.CoveragePartial, CapturedCount: 431,
				Counts: app.ItemClassCounts{Passed: 429, PendingReview: 2},
			},
			wantOutcome:       recordOutcomeFault,
			tag:               "失败",
			text:              "中途停止：游戏失去了焦点",
			summaryTitle:      "扫描中途停止：游戏失去了焦点",
			summarySupplement: "怎么做：扫描时请不要切换应用。",
			summaryNotes:      "已保存：431 件",
		},
		{
			name: "stopped before starting",
			input: recordConclusionInput{
				Termination: &record.Termination{
					Kind: record.TerminationFailed, Code: "prepareTimeout", Phase: record.PhasePreparing,
				},
				Coverage: record.CoverageUnknown,
			},
			wantOutcome:       recordOutcomeEmpty,
			tag:               "失败",
			text:              "没有开始：30 秒内游戏没有就绪",
			summaryTitle:      "没有采集到任何驱动盘：30 秒内游戏没有就绪",
			summarySupplement: "怎么做：请让游戏保持在前台并停在驱动盘仓库页，然后重新开始扫描。",
		},
		{
			name: "zero captured after a normal end",
			input: recordConclusionInput{
				Termination: completed, Coverage: record.CoverageComplete,
			},
			wantOutcome:       recordOutcomeEmpty,
			text:              "正常结束 · 没有采集到驱动盘",
			summaryTitle:      "没有采集到任何驱动盘",
			summarySupplement: "回到准备检查后重新开始。",
		},
		{
			name: "zero captured after a manual stop",
			input: recordConclusionInput{
				Termination: &record.Termination{
					Kind: record.TerminationCancelled, Code: "userCancelled", Phase: record.PhaseCapturing,
				},
				Coverage: record.CoveragePartial,
			},
			wantOutcome:       recordOutcomeEmpty,
			text:              "手动停止 · 覆盖：部分",
			summaryTitle:      "已手动停止，没有采集到任何驱动盘",
			summarySupplement: "回到准备检查后重新开始。",
		},
		{
			name: "zero captured with an interrupted termination",
			input: recordConclusionInput{
				Termination: &record.Termination{Kind: record.TerminationInterrupted},
				Interrupted: true,
			},
			wantOutcome:       recordOutcomeEmpty,
			tag:               "中断",
			text:              "已恢复已保存部分",
			summaryTitle:      "扫描中断，没有采集到任何驱动盘",
			summarySupplement: "请重新开始扫描。",
		},
		{
			name: "zero captured without a termination",
			input: recordConclusionInput{
				Interrupted: true,
			},
			wantOutcome:       recordOutcomeEmpty,
			tag:               "中断",
			text:              "已恢复已保存部分",
			summaryTitle:      "没有采集到任何驱动盘",
			summarySupplement: "回到准备检查后重新开始。",
		},
		{
			name: "save failure outranks running and zero items",
			input: recordConclusionInput{
				SaveFailed: true, ScanNotWrapped: true, CapturedCount: 0,
			},
			wantOutcome:       recordOutcomeSaveFailed,
			tag:               "!",
			text:              "保存失败",
			summaryTitle:      "扫描结果还没有保存",
			summarySupplement: "保存失败，请重试。若仍然失败，请检查磁盘空间和权限。",
		},
	}
	// Running is model state rather than record data; the shared function
	// still owns the wording.
	cases = append(cases, struct {
		name              string
		input             recordConclusionInput
		wantOutcome       recordOutcomeKind
		tag               string
		text              string
		summaryEyebrow    string
		summaryTitle      string
		summarySupplement string
		summaryNotes      string
	}{
		name:              "scan still running",
		input:             recordConclusionInput{ScanNotWrapped: true},
		wantOutcome:       recordOutcomeRunning,
		text:              "扫描进行中",
		summaryTitle:      "扫描还没有结束",
		summarySupplement: "结束后这里会给出覆盖结论和可导出数量。",
	})

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := recordOutcomeFor(tc.input)
			if out.Kind != tc.wantOutcome {
				t.Fatalf("outcome = %v, want %v", out.Kind, tc.wantOutcome)
			}
			got := recordConclusionFor(tc.input)
			if got.tag != tc.tag || got.text != tc.text {
				t.Fatalf("list = %q/%q, want %q/%q", got.tag, got.text, tc.tag, tc.text)
			}
			summary := recordSummaryWording(out)
			if summary.Eyebrow != tc.summaryEyebrow {
				t.Fatalf("summary eyebrow = %q, want %q", summary.Eyebrow, tc.summaryEyebrow)
			}
			if summary.Title != tc.summaryTitle {
				t.Fatalf("summary title = %q, want %q", summary.Title, tc.summaryTitle)
			}
			if summary.Supplement != tc.summarySupplement {
				t.Fatalf("summary supplement = %q, want %q", summary.Supplement, tc.summarySupplement)
			}
			if tc.summaryNotes != "" && !strings.Contains(strings.Join(summary.Notes, "\n"), tc.summaryNotes) {
				t.Fatalf("summary notes = %q, want them to contain %q", summary.Notes, tc.summaryNotes)
			}
		})
	}
}

// TestInterruptedRecordEndingIsShared checks the two adapters classify the
// same crashed record as interrupted, even when recovery marked no item.
func TestInterruptedRecordEndingIsShared(t *testing.T) {
	rec := recovery(record.ItemRecord{Sequence: 1, Status: record.StatusProduced})
	rec.Checkpoint.Session.Termination = nil
	rec.Checkpoint.Session.Coverage = record.Coverage{Status: record.CoveragePartial}
	rec.Checkpoint.Session.CapturedCount = 1
	view := BuildRecordView("rec", rec, false)
	m := summaryModel(view, app.Snapshot{})
	history := m.historyConclusionInput(HistoryEntry{
		ID:            "rec",
		CapturedCount: 1,
		Coverage:      record.CoveragePartial,
		Counts:        app.ItemClassCounts{Passed: 1},
	})

	summaryOutcome := recordOutcomeFor(m.summaryConclusionInput())
	listOutcome := recordOutcomeFor(history)
	if summaryOutcome.Kind != recordOutcomeInterrupted || listOutcome.Kind != recordOutcomeInterrupted {
		t.Fatalf("outcomes = summary %v, list %v; want interrupted", summaryOutcome.Kind, listOutcome.Kind)
	}
	if got := recordConclusionFor(history).text; got != "已恢复已保存部分" {
		t.Fatalf("list wording = %q", got)
	}
	if got := recordSummaryWording(summaryOutcome).Title; got != "扫描中断，只保存了部分结果" {
		t.Fatalf("summary wording = %q", got)
	}
}

// TestHistorySaveFailureUsesTheSummaryEnding keeps the list and summary on
// the same save-failure ending for the current session.
func TestHistorySaveFailureUsesTheSummaryEnding(t *testing.T) {
	m := new(Model)
	m.snap = app.Snapshot{Busy: true, SaveError: "磁盘已满"}
	m.appliedSession = scan.ID("rec")
	out := recordOutcomeFor(m.historyConclusionInput(HistoryEntry{ID: "rec"}))
	if out.Kind != recordOutcomeSaveFailed {
		t.Fatalf("history outcome = %v, want save failure", out.Kind)
	}
	if got := recordListConclusion(out).text; got != "保存失败" {
		t.Fatalf("history wording = %q", got)
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
	if m.displayPage() != pageResults || m.rec == nil || m.rec.ID != "r01" {
		t.Fatalf("导出… = page %v, record %+v, want the record's existing export entry",
			m.displayPage(), m.rec)
	}
	if !tt.HasText("导出 JSON") || !tt.HasText("导出 CSV") {
		t.Fatalf("the existing export controls do not show: %q", tt.Texts())
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
	if m.displayPage() != pageResults || m.rec == nil || m.rec.ID != "r02" ||
		m.rec.Live {
		t.Fatalf("Enter opened page %v, record %+v", m.displayPage(), m.rec)
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

// TestHistoryCurrentRowOpensLiveRecord checks that the 本次 row opens the
// current session's live record in the one record workspace, with no
// second editable copy.
func TestHistoryCurrentRowOpensLiveRecord(t *testing.T) {
	svc := &fakeService{
		history: historyTableRecords(3),
		loads: map[string]*record.Recovery{
			"r01": testRecord(),
		},
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
	if m.displayPage() != pageResults {
		t.Fatalf("the current row opened page %v, want results", m.displayPage())
	}
	if m.rec == nil || m.rec.ID != "r01" || !m.rec.Live {
		t.Fatalf("the current row opened %+v, want the live record", m.rec)
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
