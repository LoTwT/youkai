package ui

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/game/zzz"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/LoTwT/youkai/internal/scan"
	"github.com/egoist/mygo/ui"
)

// applyAndSettle applies a snapshot as the subscription pump would and
// settles the frames it drives.
func applyAndSettle(t *testing.T, m *Model, q *queueUpdater, tt *ui.Tester, s app.Snapshot) {
	t.Helper()
	m.Apply(s)
	settle(t, m, q, tt)
}

// countColor counts the pixels of a rendered frame within a small
// tolerance of c, telling the detail pane's evidence images apart.
func countColor(img *image.RGBA, c color.RGBA) int {
	n := 0
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			p := img.RGBAAt(x, y)
			dr, dg, db := int(p.R)-int(c.R), int(p.G)-int(c.G), int(p.B)-int(c.B)
			if dr < 0 {
				dr = -dr
			}
			if dg < 0 {
				dg = -dg
			}
			if db < 0 {
				db = -db
			}
			if dr+dg+db <= 12 {
				n++
			}
		}
	}
	return n
}

// evidencePaneRect returns the screenshot pane below its header, where crop
// bitmaps draw. Color checks stay inside this pane so ClearType fringes in the
// form or item list cannot masquerade as a retained evidence image.
func evidencePaneRect(t *testing.T, tt *ui.Tester, m *Model, img *image.RGBA) image.Rectangle {
	t.Helper()
	header, ok := tt.Find("截图（逐行）")
	if !ok {
		t.Fatalf("the screenshot pane header is missing: %q", tt.Texts())
	}
	width := m.reviewLayout.Crops
	if width == 0 {
		width = m.reviewCropsWidth()
	}
	x0 := int(header.X) - reviewPanePaddingX
	y0 := int(header.Y + header.H)
	return image.Rect(x0, y0, x0+int(width), img.Bounds().Max.Y)
}

// TestViewCloseBehaviorPreference checks that the close preference is
// visible, toggles both ways and persists each change, and that a window
// notice explains a missing tray entry.
func TestViewCloseBehaviorPreference(t *testing.T) {
	var saved []app.CloseBehavior
	m, q := newTestModel(t, Config{Service: &fakeService{}, SaveCloseBehavior: func(behavior app.CloseBehavior) error {
		saved = append(saved, behavior)
		return nil
	}})
	m.page = pageSettings
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)

	const exitLabel = "停止扫描并退出"
	const hideLabel = "隐藏到通知区域（默认）"
	if !tt.HasText(exitLabel) || !tt.HasText(hideLabel) {
		t.Fatal("the close preference does not show")
	}
	if err := tt.Click(exitLabel); err != nil {
		t.Fatal(err)
	}
	drainAll(t, m, q)
	if m.CloseBehavior() != app.CloseBehaviorExit {
		t.Fatalf("close behavior = %q after the click", m.CloseBehavior())
	}
	if len(saved) != 1 || saved[0] != app.CloseBehaviorExit {
		t.Fatalf("saved preferences = %v", saved)
	}

	tt.Frame()
	if err := tt.Click(hideLabel); err != nil {
		t.Fatal(err)
	}
	drainAll(t, m, q)
	if m.CloseBehavior() != app.CloseBehaviorHide {
		t.Fatalf("close behavior = %q after the second click", m.CloseBehavior())
	}
	if len(saved) != 2 || saved[1] != app.CloseBehaviorHide {
		t.Fatalf("saved preferences = %v", saved)
	}

	m.SetTrayUnavailable("test")
	tt.Frame()
	if !tt.HasText(exitLabel) {
		t.Fatal("the fixed close option does not show")
	}
	if tt.HasText(hideLabel) {
		t.Fatal("the hide option remains selectable without a tray")
	}
	if !tt.HasText("通知区域图标不可用") {
		t.Fatal("the fixed close option does not state why")
	}
	if tt.HasText("再看一次托盘图标位置提示") {
		t.Fatal("the tray-hint replay is offered without a tray")
	}
}

// TestViewStartAndStopByPhase drives the scan page through the stages:
// the checklist replaces the old prepare hint, a double click cannot issue
// two starts, the stop button follows the busy state, and the stage block
// shows only provable facts.
func TestViewStartAndStopByPhase(t *testing.T) {
	now := time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC)
	svc := &fakeService{}
	m, q := newTestModel(t, Config{Service: svc, Now: func() time.Time { return now }})
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)

	if !tt.HasText("准备检查") {
		t.Fatalf("the preparation checklist does not show: %q", tt.Texts())
	}
	if !tt.HasText("中途停止 " + shortcutStopScan) {
		t.Fatalf("the bottom bar does not name the stop hotkey: %q", tt.Texts())
	}

	// Idle with a source: a second start while the request is in flight
	// issues nothing — the action bar swaps to stop and the guard refuses.
	if err := tt.Click("开始扫描"); err != nil {
		t.Fatal(err)
	}
	m.Start()
	drainAll(t, m, q)
	if len(svc.starts) != 1 {
		t.Fatalf("the start issued %d requests for one click burst", len(svc.starts))
	}

	// Preparing: countdown, waiting copy, activation result, stop works.
	preparing := snap("s1", 1, true, scan.PhasePreparing, scan.Counts{}, 0, true, "")
	preparing.Scan.PrepareDeadline = now.Add(30 * time.Second)
	applyAndSettle(t, m, q, tt, preparing)
	for _, want := range []string{"30 秒", "请在倒计时结束前让游戏保持在前台", "正在等待游戏就绪", "请确保 youkai 没有盖住游戏窗口"} {
		if !tt.HasText(want) {
			t.Errorf("the preparing stage does not show %q: %q", want, tt.Texts())
		}
	}
	if m.scanStartReady() {
		t.Error("the start is offered during preparation")
	}
	if err := tt.Click("停止扫描"); err != nil {
		t.Fatal(err)
	}
	drainAll(t, m, q)
	if len(svc.starts) != 1 || svc.stops != 1 {
		t.Fatalf("during preparation: %d starts, %d stops", len(svc.starts), svc.stops)
	}

	// Capture: one provable count and no percentage (R-11).
	applyAndSettle(t, m, q, tt, snap("s1", 2, true, scan.PhaseCapturing,
		scan.Counts{Captured: 5, Produced: 2, Failed: 1, Unprocessed: 1, Queued: 1}, 0, true, ""))
	if !tt.HasText("正在扫描") || !tt.HasText("已采集 5 件") {
		t.Errorf("the capture stage does not show its count: %q", tt.Texts())
	}
	if tt.HasText("已产出 2") || tt.HasText("排队 1") {
		t.Errorf("the capture stage leaks internal progress: %q", tt.Texts())
	}
	if err := tt.Click("停止扫描"); err != nil {
		t.Fatal(err)
	}
	drainAll(t, m, q)
	if svc.stops != 2 {
		t.Fatalf("the stop button stopped working while busy: %d", svc.stops)
	}

	// Finalizing: the stop is over, recognition and saving remain.
	applyAndSettle(t, m, q, tt, snap("s1", 3, true, scan.PhaseFinalizing,
		scan.Counts{Captured: 5, Produced: 3, Queued: 2}, 3, true, ""))
	if !tt.HasText("已停止操作游戏，正在识别剩余 2 件并保存") {
		t.Errorf("the finalizing stage does not show: %q", tt.Texts())
	}

	// Ended after input opened: the page returns to the checklist and the
	// summary opens; a new attempt gets a fresh request identity.
	ended := snap("s1", 4, false, scan.PhaseEnded,
		scan.Counts{Captured: 5, Produced: 3, Failed: 1, Unprocessed: 1}, 5, false, "")
	ended.Scan.End = scan.EndCancelled
	ended.Scan.Trigger = &scan.Trigger{
		Kind: scan.TriggerUser, Code: scan.CodeUserCancelled, Phase: scan.PhaseCapturing,
	}
	applyAndSettle(t, m, q, tt, ended)
	if m.displayPage() != pageResults || m.recordTab != recordTabSummary {
		t.Fatalf("the ended scan shows page %v tab %v, want the summary", m.displayPage(), m.recordTab)
	}
	m.page = pageScan
	tt.Frame()
	if !tt.HasText("准备检查") {
		t.Fatalf("the scan page did not return to the checklist: %q", tt.Texts())
	}
	if err := tt.Click("开始扫描"); err != nil {
		t.Fatal(err)
	}
	drainAll(t, m, q)
	if svc.stops != 2 {
		t.Fatalf("the idle page still stopped a session: %d", svc.stops)
	}
	if len(svc.starts) != 2 || svc.starts[0].ID == svc.starts[1].ID {
		t.Fatalf("a new attempt after an ended session: %+v", svc.starts)
	}
}

// TestViewStartWithoutSource checks the production M1 state: no scan
// source is wired, the start button is disabled with its reason, and
// clicking it issues nothing.
func TestViewStartWithoutSource(t *testing.T) {
	svc := &fakeService{}
	reason := "当前构建没有接入游戏扫描源"
	m, q := newTestModel(t, Config{Service: svc, ScanSource: ScanSourceInfo{Reason: reason}})
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)
	if !tt.HasText("这个版本还不能扫描") || !tt.HasText(reason) {
		t.Errorf("the no-source state does not show its reason: %q", tt.Texts())
	}
	if !tt.HasText("这个版本还不能扫描。") {
		t.Errorf("the start's refusal reason does not show: %q", tt.Texts())
	}
	if err := tt.Click("开始扫描"); err != nil {
		t.Fatal(err)
	}
	drainAll(t, m, q)
	if len(svc.starts) != 0 {
		t.Fatalf("the disabled start issued a request: %+v", svc.starts)
	}
}

// TestViewStartRefusalIsShown checks that a refused start's reason shows
// beside the button.
func TestViewStartRefusalIsShown(t *testing.T) {
	svc := &fakeService{startFn: func(app.StartRequest) (app.StartOutcome, error) {
		return app.StartOutcome{}, errString("no scan source is wired")
	}}
	m, q := newTestModel(t, Config{Service: svc})
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)
	if err := tt.Click("开始扫描"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if !tt.HasText("开始失败：no scan source is wired") {
		t.Errorf("the start refusal does not show: %q", tt.Texts())
	}
}

type errString string

func (e errString) Error() string { return string(e) }

// testRecord is one record covering the outcome variety: produced, failed,
// user-cancelled and interrupted items, the interrupted one marked by the
// recovery.
func testRecord() *record.Recovery {
	disc := zzz.DiscRecord{
		SetID: intp(31100),
		Slot:  1,
		Level: intp(15),
		MainStat: zzz.Stat{
			Attribute: zzz.AttrHealth, Value: "2200", Unit: zzz.UnitFlat,
		},
		Substats: []zzz.Substat{{
			Stat:             zzz.Stat{Attribute: zzz.AttrCritDamage, Value: "19.2", Unit: zzz.UnitPercent},
			EnhancementRolls: intp(3),
		}},
	}
	return recovery(
		structuredItem(1, disc, "河豚电音", "+15"),
		item(2, record.StatusFailed, "", "识别超时", "名", "级"),
		item(3, record.StatusUnprocessed, record.ReasonCancelled, "", "名"),
		item(4, record.StatusUnprocessed, record.ReasonInterrupted, "", "名"),
	)
}

// openTestRecord attaches a model whose history holds the test record and
// reopens it, ready for view assertions.
func openTestRecord(t *testing.T, ev EvidenceSource, cacheLimit int) (*Model, *queueUpdater, *ui.Tester) {
	t.Helper()
	svc := &fakeService{
		list: []record.RecordInfo{{
			ID: "r1", LatestRevision: 2,
			SavedAt: time.Date(2026, 10, 17, 12, 30, 0, 0, time.UTC),
		}},
		loads: map[string]*record.Recovery{"r1": testRecord()},
	}
	m, q := newTestModel(t, Config{Service: svc, Evidence: ev, EvidenceCacheLimit: cacheLimit})
	// The standard width keeps the 240-DIP item list beside the
	// screenshots and form. The all segment keeps every outcome in the
	// list, as the review flow sees them.
	m.SetContentSize(1180, 900)
	tt := ui.NewTester(m.View, 1180, 900)
	settle(t, m, q, tt)
	m.OpenHistory()
	settle(t, m, q, tt)
	showRecordItems(m)
	tt.Frame()
	m.setResultFilter(resultFilterAll)
	settle(t, m, q, tt)
	return m, q, tt
}

// TestViewDetailMatchesSelection checks the results list and the details
// staying on the same item: every outcome stays in the list, selecting a
// row shows that row's evidence images (by their pixels), texts and
// scores, and failed or unprocessed rows show their reasons.
func TestViewDetailMatchesSelection(t *testing.T) {
	item1A := color.RGBA{R: 200, G: 10, B: 210, A: 255}
	item1B := color.RGBA{R: 10, G: 200, B: 210, A: 255}
	item2A := color.RGBA{R: 210, G: 120, B: 10, A: 255}
	item2B := color.RGBA{R: 10, G: 210, B: 120, A: 255}
	item3A := color.RGBA{R: 120, G: 10, B: 30, A: 255}
	ev := &fakeEvidence{images: map[string]*ui.Bitmap{
		"r1/000001-line0": solidBitmap(item1A),
		"r1/000001-line1": solidBitmap(item1B),
		"r1/000002-line0": solidBitmap(item2A),
		"r1/000002-line1": solidBitmap(item2B),
		"r1/000003-line0": solidBitmap(item3A),
	}}
	m, q, tt := openTestRecord(t, ev, 0)
	pane := evidencePaneRect(t, tt, m, tt.Image())

	// Every outcome stays in the list, and the recovery mark shows.
	for _, want := range []string{
		"结果：历史记录 r1（4 条，中断恢复 1 条）",
		"河豚电音", "待补录 · 识别失败", "待补录 · 未处理",
	} {
		if !tt.HasText(want) {
			t.Fatalf("the results list does not show %q: %q", want, tt.Texts())
		}
	}

	// The first record is chosen by default; its screenshots are always
	// visible beside the form.
	if !tt.HasText("#0001 · 河豚电音 · 1 号位 · 15 级") {
		t.Fatalf("the default selection does not show: %q", tt.Texts())
	}
	if n := countColorInRect(tt.Image(), item1A, pane); n < 100 {
		t.Fatalf("the first item's first image does not show (%d px, pane %v)", n, pane)
	}

	// The failed row: its reason shows and only its images render.
	m.selected = 1
	m.tableSelected = 1
	tt.Frame()
	settle(t, m, q, tt)
	if !tt.HasText("#0002 · 失败") || !tt.HasText("补录：这件识别失败，补全必填字段后才能导出。") {
		t.Fatalf("the failed row's details do not show: %q", tt.Texts())
	}
	if n := countColorInRect(tt.Image(), item2A, pane); n < 100 {
		t.Fatalf("the failed item's image does not show (%d px, pane %v)", n, pane)
	}
	if n := countColorInRect(tt.Image(), item1A, pane); n != 0 {
		t.Fatalf("another item's image shows for the failed item (%d px, pane %v)", n, pane)
	}

	// The user-cancelled row shows its backfill notice and text.
	m.selected = 2
	m.tableSelected = 2
	tt.Frame()
	settle(t, m, q, tt)
	if !tt.HasText("补录：这件没有处理，补全必填字段后才能导出。") {
		t.Fatalf("the cancelled reason does not show: %q", tt.Texts())
	}
	if n := countColorInRect(tt.Image(), item1A, pane); n != 0 {
		t.Fatalf("another item's image shows for the cancelled item (%d px, pane %v)", n, pane)
	}
}

// TestViewStructuredFormAndScreenshots checks that the item form shows
// the parsed fields and the screenshots are always visible beside it.
func TestViewStructuredFormAndScreenshots(t *testing.T) {
	img := color.RGBA{R: 171, G: 31, B: 91, A: 255}
	ev := &fakeEvidence{images: map[string]*ui.Bitmap{
		"r1/000001-line0": solidBitmap(img),
	}}
	_, _, tt := openTestRecord(t, ev, 0)
	if err := tt.Click("其余 6 个字段"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, want := range []string{
		"套装", "31100 河豚电音", "稀有度", "S", "槽位", "1 号位",
		"等级", "15 级", "主词条", "health 生命值", "flat 固定值",
		"副词条", "critDamage 暴击伤害", "percent 百分比",
		"管理状态", "装备",
	} {
		if !tt.HasText(want) {
			t.Fatalf("the structured form does not show %q: %q", want, tt.Texts())
		}
	}
	if n := countColor(tt.Image(), img); n < 100 {
		t.Fatalf("the screenshot is not beside the form (%d px)", n)
	}
}

// TestViewFastSelectionSwitch checks that a load completing for an item
// the user already switched away from cannot attach to the newly chosen
// one: the details always render the selected item's own images, and a
// switch back shows the earlier item's images without loading again.
func TestViewFastSelectionSwitch(t *testing.T) {
	item1A := color.RGBA{R: 201, G: 11, B: 211, A: 255}
	item1B := color.RGBA{R: 11, G: 201, B: 211, A: 255}
	item2A := color.RGBA{R: 211, G: 121, B: 11, A: 255}
	item2B := color.RGBA{R: 11, G: 211, B: 121, A: 255}
	ev := &fakeEvidence{images: map[string]*ui.Bitmap{
		"r1/000001-line0": solidBitmap(item1A),
		"r1/000001-line1": solidBitmap(item1B),
		"r1/000002-line0": solidBitmap(item2A),
		"r1/000002-line1": solidBitmap(item2B),
	}}
	m, q, tt := openTestRecord(t, ev, 0)
	pane := evidencePaneRect(t, tt, m, tt.Image())

	// The first item is chosen and its loads are scheduled. Switch to the
	// second item before the loads land, then settle: the second item's
	// own images show, the first item's do not.
	m.selected = 1
	m.tableSelected = 1
	tt.Frame()
	settle(t, m, q, tt)
	if !tt.HasText("#0002 · 失败") {
		t.Fatalf("the switch did not choose the second item: %q", tt.Texts())
	}
	if n := countColorInRect(tt.Image(), item2A, pane); n < 100 {
		t.Fatalf("the second item's image does not show after the switch (%d px, pane %v)", n, pane)
	}
	if n := countColorInRect(tt.Image(), item1A, pane); n != 0 {
		t.Fatalf("the first item's image shows for the second item (%d px, pane %v)", n, pane)
	}
	loaded := ev.callsCount()
	if loaded < 4 {
		t.Fatalf("both items' evidence should have loaded, got %d calls", loaded)
	}

	// Switching back shows the first item's images from the cache without
	// loading anything again.
	m.selected = 0
	m.tableSelected = 0
	tt.Frame()
	settle(t, m, q, tt)
	if n := countColorInRect(tt.Image(), item1A, pane); n < 100 {
		t.Fatalf("the first item's image does not show after switching back (%d px, pane %v)", n, pane)
	}
	if n := countColorInRect(tt.Image(), item2A, pane); n != 0 {
		t.Fatalf("the second item's image still shows after switching back (%d px, pane %v)", n, pane)
	}
	if ev.callsCount() != loaded {
		t.Fatalf("a cached image reloaded: %d then %d calls", loaded, ev.callsCount())
	}
}

// TestViewEvidenceFailureShows checks the unavailable evidence image: the
// failure text shows and stays without a reload loop.
func TestViewEvidenceFailureShows(t *testing.T) {
	ev := &fakeEvidence{images: map[string]*ui.Bitmap{}}
	m, q, tt := openTestRecord(t, ev, 0)
	settle(t, m, q, tt)
	if !tt.HasText("截图不可用：missing evidence 000001-line0") {
		t.Fatalf("the evidence failure does not show: %q", tt.Texts())
	}
	loaded := ev.callsCount()
	settle(t, m, q, tt)
	if ev.callsCount() != loaded {
		t.Fatalf("the failed evidence reloaded every frame: %d then %d", loaded, ev.callsCount())
	}
}

// TestViewSaveFailureShowsRetry checks the save state: a failed save shows
// its reason with the retry and never as saved; a clean save state hides
// the retry and shows the saved count.
func TestViewSaveFailureShowsRetry(t *testing.T) {
	m, q := newTestModel(t, Config{Service: &fakeService{}})
	m.page = pageScan
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)

	applyAndSettle(t, m, q, tt, snap("s1", 3, false, scan.PhaseEnded,
		scan.Counts{Captured: 4, Produced: 4}, 2, true, "磁盘已满"))
	if !tt.HasText("扫描结果还没有保存") || !tt.HasText("磁盘已满") {
		t.Fatalf("the save failure does not show: %q", tt.Texts())
	}
	if !tt.HasText("已保存 2 条；其余已采集结果尚未保存。") {
		t.Fatalf("the save failure does not name what is unsaved: %q", tt.Texts())
	}
	if tt.HasText("已保存 2 条 · 有内容待写入") {
		t.Fatal("a failed save showed as merely pending")
	}

	// The retried save succeeded: the snapshot is clean and the retry
	// disappears.
	applyAndSettle(t, m, q, tt, snap("s1", 4, false, scan.PhaseEnded,
		scan.Counts{Captured: 4, Produced: 4}, 4, false, ""))
	m.page = pageScan
	tt.Frame()
	if !tt.HasText("准备检查") {
		t.Fatalf("the scan page did not return to the checklist: %q", tt.Texts())
	}
	if tt.HasText("重试保存") || tt.HasText("扫描结果还没有保存") {
		t.Fatalf("the retry still shows after a clean save: %q", tt.Texts())
	}
}

// TestViewSaveRetryIsIssued checks the retry button reaching the service
// and disappearing once the save settled.
func TestViewSaveRetryIsIssued(t *testing.T) {
	svc := &fakeService{}
	m, q := newTestModel(t, Config{Service: svc})
	m.page = pageScan
	tt := ui.NewTester(m.View, 1020, 700)
	applyAndSettle(t, m, q, tt, snap("s1", 3, false, scan.PhaseEnded,
		scan.Counts{Captured: 4, Produced: 4}, 2, true, "磁盘已满"))
	if err := tt.Click("重试保存"); err != nil {
		t.Fatal(err)
	}
	drainAll(t, m, q)
	if svc.retries != 1 {
		t.Fatalf("the retry button issued %d retries", svc.retries)
	}
	applyAndSettle(t, m, q, tt, snap("s1", 4, false, scan.PhaseEnded,
		scan.Counts{Captured: 4, Produced: 4}, 4, false, ""))
	if err := tt.Click("重试保存"); err == nil {
		t.Fatal("the retry button still exists after the save settled")
	}
}

// TestViewExitShowsWrapUp checks the close-as-exit behavior: the wrap-up
// shows its progress, refuses the close while it runs, and a failed
// wrap-up keeps the window with the save retry; a settled one allows the
// close and settles the application once.
func TestViewExitShowsWrapUp(t *testing.T) {
	release := make(chan struct{})
	svc := &fakeService{quitFn: func() error {
		<-release
		return nil
	}}
	settled := 0
	m, q := newTestModel(t, Config{Service: svc, OnExitSettled: func() { settled++ }})
	m.page = pageScan
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)

	// The first close starts the wrap-up and refuses; the window shows the
	// wrap-up while it runs. (No draining while it blocks on release.)
	if m.AllowClose() {
		t.Fatal("the first close was allowed before the wrap-up")
	}
	tt.Frame()
	if !tt.HasText("正在退出：停止扫描 → 保存结果 → 释放识别资源……") {
		t.Fatalf("the wrap-up progress does not show: %q", tt.Texts())
	}
	if m.AllowClose() {
		t.Fatal("a repeated close was allowed while the wrap-up runs")
	}
	close(release)
	drainAll(t, m, q)
	if !m.AllowClose() || settled != 1 {
		t.Fatalf("the settled wrap-up did not allow the close (settled %d)", settled)
	}

	// A failed wrap-up keeps the window: the failure shows together with
	// the save retry once the snapshot carries the save error.
	svc2 := &fakeService{quitFn: func() error {
		return errString("app: quit: scan s1 still has an unsaved result: 磁盘已满")
	}}
	m2, q2 := newTestModel(t, Config{Service: svc2, OnExitSettled: func() { settled++ }})
	m2.page = pageScan
	tt2 := ui.NewTester(m2.View, 1020, 700)
	settle(t, m2, q2, tt2)
	if m2.AllowClose() {
		t.Fatal("the close was allowed before the wrap-up")
	}
	drainAll(t, m2, q2)
	applyAndSettle(t, m2, q2, tt2, snap("s1", 3, false, scan.PhaseEnded,
		scan.Counts{Captured: 4, Produced: 4}, 2, true, "磁盘已满"))
	m2.page = pageScan
	tt2.Frame()
	if !tt2.HasText("退出未完成：app: quit: scan s1 still has an unsaved result: 磁盘已满") {
		t.Fatalf("the wrap-up failure does not show: %q", tt2.Texts())
	}
	if !tt2.HasText("重试保存") {
		t.Fatal("the failed exit offers no save retry")
	}
	svc2.quitFn = nil
	if m2.AllowClose() {
		t.Fatal("the close was allowed before the second wrap-up")
	}
	drainAll(t, m2, q2)
	if !m2.AllowClose() || settled != 2 {
		t.Fatalf("the second wrap-up did not settle (state %d, settled %d)", m2.exitState, settled)
	}
}

// TestViewHistoryReopen checks the history area: the saved records show,
// opening one loads it, and after a scan the live record is reachable
// again.
func TestViewHistoryReopen(t *testing.T) {
	svc := &fakeService{
		list: []record.RecordInfo{
			{ID: "new", LatestRevision: 2, SavedAt: time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)},
			{ID: "old", LatestRevision: 1, SavedAt: time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)},
		},
		loads: map[string]*record.Recovery{
			"new":  testRecord(),
			"old":  recovery(structuredItem(1, zzz.DiscRecord{SetID: intp(31000), Slot: 2, Level: intp(0)}, "啄木鸟电音")),
			"live": recovery(structuredItem(1, zzz.DiscRecord{SetID: intp(32800), Slot: 1, Level: intp(15)}, "静听嘉音")),
		},
	}
	m, q := newTestModel(t, Config{Service: svc})
	m.SetContentSize(1020, 700)
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)
	openRecordsList(t, m, q, tt)
	if !tt.HasText(historyTimeText(m.history[0])) {
		t.Fatalf("the history entries do not show: %q", tt.Texts())
	}

	// Open the default entry, then the older one through the picker.
	if err := tt.Click(historyTimeText(m.history[0])); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyEnter)
	settle(t, m, q, tt)
	showRecordItems(m)
	tt.Frame()
	if !tt.HasText("结果：历史记录 new（4 条，中断恢复 1 条）") {
		t.Fatalf("the reopened record does not show: %q", tt.Texts())
	}
	m.histChoice = "2026-10-16 09:00 · old"
	m.OpenHistory()
	settle(t, m, q, tt)
	showRecordItems(m)
	tt.Frame()
	if !tt.HasText("啄木鸟电音") || tt.HasText("中断恢复") {
		t.Fatalf("the older record did not open: %q", tt.Texts())
	}

	// A scan happened before the history was opened: its record is
	// reachable again.
	applyAndSettle(t, m, q, tt, snap("live", 1, false, scan.PhaseEnded,
		scan.Counts{Captured: 1, Produced: 1}, 1, false, ""))
	m.OpenHistory()
	settle(t, m, q, tt)
	showRecordItems(m)
	tt.Frame()
	if !tt.HasText("啄木鸟电音") {
		t.Fatalf("the history record did not reopen: %q", tt.Texts())
	}
	if err := tt.Click("查看本次扫描"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	showRecordItems(m)
	tt.Frame()
	if !tt.HasText("静听嘉音") {
		t.Fatalf("the live record did not reopen: %q", tt.Texts())
	}
}

// TestEvidenceCacheIsBounded checks the decoded-image bound: eviction
// trims to the limit but keeps the images on screen (evicting those would
// only churn reloads), and an evicted image reloads when it shows again.
func TestEvidenceCacheIsBounded(t *testing.T) {
	svc := &fakeService{
		list: []record.RecordInfo{{ID: "r1", LatestRevision: 1, SavedAt: time.Now()}},
		loads: map[string]*record.Recovery{
			"r1": recovery(
				structuredItem(1, zzz.DiscRecord{SetID: intp(31100), Slot: 1, Level: intp(15)},
					"一行一", "一行二", "一行三", "一行四", "一行五", "一行六"),
				structuredItem(2, zzz.DiscRecord{SetID: intp(31200), Slot: 2, Level: intp(15)},
					"二行一", "二行二", "二行三", "二行四", "二行五", "二行六"),
			),
		},
	}
	ev := &fakeEvidence{images: map[string]*ui.Bitmap{}}
	for _, seq := range []int{1, 2} {
		for i := range 6 {
			ev.images[fmt.Sprintf("r1/%06d-line%d", seq, i)] =
				solidBitmap(color.RGBA{R: uint8(20 + 10*seq + i), G: 210, B: 90, A: 255})
		}
	}
	m, q := newTestModel(t, Config{Service: svc, Evidence: ev, EvidenceCacheLimit: 8})
	m.SetContentSize(1180, 760)
	tt := ui.NewTester(m.View, 1180, 760)
	settle(t, m, q, tt)
	m.OpenHistory()
	settle(t, m, q, tt)
	showRecordItems(m)
	tt.Frame()
	settle(t, m, q, tt)

	// The first item's six images load and stay (they are on screen).
	if ev.callsCount() != 6 {
		t.Fatalf("the first item's six lines loaded %d times", ev.callsCount())
	}
	if len(m.cache.entries) != 6 {
		t.Fatalf("the cache holds %d entries, want the six on screen", len(m.cache.entries))
	}

	// Switching to the second item loads its six; eviction trims the
	// first item's down to the limit while keeping what is on screen.
	m.selected = 1
	m.tableSelected = 1
	tt.Frame()
	settle(t, m, q, tt)
	if ev.callsCount() != 12 {
		t.Fatalf("the second item's six lines did not load: %d calls", ev.callsCount())
	}
	if len(m.cache.entries) != 8 {
		t.Fatalf("the cache holds %d entries, want the limit 8", len(m.cache.entries))
	}

	// Switching back reloads only what was evicted.
	m.selected = 0
	m.tableSelected = 0
	tt.Frame()
	settle(t, m, q, tt)
	if ev.callsCount() != 16 {
		t.Fatalf("the evicted images did not reload once: %d calls", ev.callsCount())
	}
	if len(m.cache.entries) != 8 {
		t.Fatalf("the cache holds %d entries after reloading, want the limit 8", len(m.cache.entries))
	}
}

// TestFileEvidence checks the file-backed evidence source against the
// record store's own layout.
func TestFileEvidence(t *testing.T) {
	root := t.TempDir()
	img := image.NewRGBA(image.Rect(0, 0, 8, 6))
	for y := range 6 {
		for x := range 8 {
			img.SetRGBA(x, y, color.RGBA{R: 9, G: 200, B: 130, A: 255})
		}
	}
	path := record.EvidencePath(root, "r1", "000001-line0")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	src := FileEvidence{Root: root}
	bm, err := src.Evidence("r1", "000001-line0")
	if err != nil {
		t.Fatalf("loading a written evidence image: %v", err)
	}
	if w, h := bm.Size(); w != 8 || h != 6 {
		t.Fatalf("the loaded image is %d×%d, want 8×6", w, h)
	}
	if _, err := src.Evidence("r1", "000002-line0"); err == nil {
		t.Fatal("loading a missing evidence image succeeded")
	}
}

// confirmedReviewItem is a produced item already settled by a human, so a
// record built from it starts with zero pending rows.
func confirmedReviewItem(t *testing.T, seq int64, level int) record.ItemRecord {
	t.Helper()
	it := reviewRowItem(t, seq, level)
	it.Quality = record.QualityHumanConfirmed
	return it
}

// excludedReviewItem is a produced item the export must leave out even
// though it carries structured fields, so the completion card can show an
// "other excluded" group.
func excludedReviewItem(t *testing.T, seq int64) record.ItemRecord {
	t.Helper()
	it := reviewRowItem(t, seq, 12)
	it.Quality = record.QualityAutoAccepted
	return withStructured(t, it, func(structured *zzz.StructuredDisc) {
		structured.Data.ManagementState = nil
	})
}

// TestFailedSessionExplainsTermination checks that a zero-item failure is
// not summarized only as item-level "失败 0 件": the results page states
// the session's termination cause and phase, and the empty state says no
// item was saved.
func TestFailedSessionExplainsTermination(t *testing.T) {
	rec := recovery()
	rec.Checkpoint.Session.Termination = &record.Termination{
		Kind: record.TerminationFailed, Code: "navigationMismatch", Phase: record.PhaseCapturing,
	}
	rec.Checkpoint.Session.Coverage = record.Coverage{Status: record.CoveragePartial}
	m, _, tt := openResponsiveResults(t, rec, 1024, 700)

	if !tt.HasText("扫描失败：点击或拖拽后页面没有按预期变化。请重新开始扫描；反复出现时复制诊断信息。（采集阶段），未保留任何条目") {
		t.Fatalf("the failed session does not name its termination: %q", tt.Texts())
	}
	if !tt.HasText("无可导出条目") {
		t.Fatalf("the zero-item completion state is missing: %q", tt.Texts())
	}
	m.filter = resultFilterAll
	tt.Frame()
	if !tt.HasText("本次扫描失败，没有已保存的条目。") {
		t.Fatalf("the failed empty state still reads as a running session: %q", tt.Texts())
	}
	if tt.HasText("该记录没有条目。") {
		t.Fatalf("the failed empty state uses the generic history text: %q", tt.Texts())
	}
}

// TestCompletionCardEmptyRecord checks that a saved record with no rows
// still reaches the pending-zero surface and does not offer an export.

// TestCompletionCardShowsCountsExclusionsAndExports checks the positive
// completion card: its own exportable count, the mutually exclusive
// exclusion groups and the export actions inside the card.

// TestCompletionCardBlocksZeroExportable checks that a record with no
// usable item states "无可导出" and disables both export actions.

// TestCompletionCardBlocksUnknownExport checks that an unknown export
// list is never presented as a confirmed completion and cannot export.
func TestCompletionCardBlocksUnknownExport(t *testing.T) {
	rec := recovery(confirmedReviewItem(t, 1, 12))
	m, q, tt := openResponsiveResults(t, rec, 1024, 700)
	saver := &fakeDirectExportSaver{}
	m.cfg.DirectExportSaver = saver
	m.exportData = nil
	m.exportBuildErr = ""
	tt.Frame()

	if !m.completionVisible() {
		t.Fatal("the pending-zero record without an export snapshot does not show its blocked state")
	}
	if !tt.HasText("不可确认完成") || !tt.HasText("不可确认：导出清单尚未就绪。") {
		t.Fatalf("the unknown export state is not explained: %q", tt.Texts())
	}
	if tt.HasText("都处理完了") {
		t.Fatal("an unknown export claims completion")
	}
	if err := tt.Click("导出…"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !m.exportDialog.open || !tt.HasText("导出清单尚未就绪，不能导出。") {
		t.Fatalf("the unknown export dialog did not name its block: %q", tt.Texts())
	}
	if err := tt.Click("导出 CSV"); err != nil {
		t.Fatal(err)
	}
	drainAll(t, m, q)
	if calls := saver.snapshot(); len(calls) != 0 {
		t.Fatalf("an unknown export wrote %+v", calls)
	}
}

// TestCompletionListDoesNotClaimDoneWithoutExportSnapshot covers PR-14b:
// at the standard width the item list and the form must give the same
// conclusion when the export snapshot is unknown or failed.
func TestCompletionListDoesNotClaimDoneWithoutExportSnapshot(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(*Model)
	}{
		{name: "unknown", set: func(m *Model) { m.exportData = nil; m.exportBuildErr = "" }},
		{name: "failed", set: func(m *Model) { m.exportData = nil; m.exportBuildErr = "boom" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := recovery(confirmedReviewItem(t, 1, 12))
			m, q, tt := openResponsiveResults(t, rec, 1180, 760)
			m.cfg.DirectExportSaver = &fakeDirectExportSaver{}
			tc.set(m)
			settle(t, m, q, tt)
			if tt.HasText("都处理完了") {
				t.Fatalf("the list claims completion without an export snapshot: %q", tt.Texts())
			}
			if !tt.HasText("待处理 0 件，导出清单尚未就绪。") {
				t.Fatalf("the list does not state the export state: %q", tt.Texts())
			}
			if !tt.HasText("不可确认完成") {
				t.Fatalf("the form does not state the blocked completion: %q", tt.Texts())
			}
		})
	}
}

// TestCompletionCardShowsPartialCoverage checks the partial coverage
// warning and the snapshot mark the exported file will carry.

// TestCompletionCardNamesIncompletePositions checks that a degraded
// position's stage and localized cause reach the results page beside the
// partial coverage conclusion without leaking the diagnostic code.

// TestSessionTerminationNoticeNamesIncompletePositions checks the failed
// session surface: the termination cause and the degraded positions appear
// together, because a limit stop is diagnosed by both.
func TestSessionTerminationNoticeNamesIncompletePositions(t *testing.T) {
	rec := recovery(confirmedReviewItem(t, 1, 12))
	rec.Checkpoint.Session.Coverage = record.Coverage{Status: record.CoveragePartial}
	rec.Checkpoint.Session.Termination = &record.Termination{
		Kind: record.TerminationFailed, Code: "positionFailureLimit", Phase: record.PhaseCapturing,
	}
	rec.Checkpoint.Session.ObservedScope = []byte(`{"positions":[{"position":1,"complete":false,"stage":"filter","reasonCode":"navigationMismatch","reason":"three preparations failed"}]}`)
	_, _, tt := openResponsiveResults(t, rec, 1024, 700)
	if !tt.HasText("扫描失败：多个号位没有完成") || !tt.HasText("不完整号位：1 号位 筛选准备失败：点击或拖拽后页面没有按预期变化") {
		t.Fatalf("the failed-session surface omits the degraded positions: %q", tt.Texts())
	}
}

// TestCompletionCardOnlyInPendingSegment checks the confirmed switch
// rule: "全部" shows the full table, and returning to "待校对 0" restores
// the completion card.

// TestCompletionRequiresEndedAndSavedSession checks that a live session
// only reaches completion after the scan ends and every result is saved.

// TestCompletionUsesHistoryRecordCounts checks that a history snapshot
// with pending=0 takes its completion card and exportable count from its
// own record, never from the live session.

// TestUnprocessedIssueColorsAreMuted checks the detail subtitle's issue
// color: an unprocessed item means recognition did not run, so it uses
// the muted role rather than the failure's danger color. Failures stay
// danger.
func TestUnprocessedIssueColorsAreMuted(t *testing.T) {
	theme := &ui.Theme{
		Danger:    ui.RGB(220, 0, 0),
		TextMuted: ui.RGB(120, 120, 120),
	}
	unprocessed := &Row{Status: record.StatusUnprocessed, Reason: record.ReasonInterrupted}
	if got := resultIssueColor(theme, unprocessed); got != theme.TextMuted {
		t.Fatalf("unprocessed issue color = %+v, want muted %+v", got, theme.TextMuted)
	}
	failed := &Row{Status: record.StatusFailed, Failure: "识别超时"}
	if got := resultIssueColor(theme, failed); got != theme.Danger {
		t.Fatalf("failed issue color = %+v, want danger %+v", got, theme.Danger)
	}
}
