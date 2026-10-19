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

// TestViewStartAndStopByPhase drives the prepare and progress areas
// through the phases: the confirmed hint shows, the start button follows
// availability, a double click cannot issue two starts, the stop button
// follows the busy state, and only provable counts and the end reason
// appear.
func TestViewStartAndStopByPhase(t *testing.T) {
	svc := &fakeService{}
	m, q := newTestModel(t, Config{Service: svc})
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)

	if !tt.HasText(scanHint) {
		t.Error("the confirmed scan hint does not show")
	}
	if !tt.HasText("停止方式：点击本窗口的“停止”按钮。") {
		t.Error("the stop means do not show")
	}

	// Idle with a source: a double click while the request is in flight
	// issues one request — the button disables itself until the service
	// answered.
	if err := tt.Click("开始扫描"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("开始扫描"); err != nil {
		t.Fatal(err)
	}
	drainAll(t, m, q)
	if len(svc.starts) != 1 {
		t.Fatalf("the start issued %d requests for one click burst", len(svc.starts))
	}

	// Preparing: the start is disabled, the stop works.
	applyAndSettle(t, m, q, tt, snap("s1", 1, true, scan.PhasePreparing,
		scan.Counts{}, 0, true, ""))
	if !tt.HasText("阶段：准备中") {
		t.Error("the preparing phase does not show")
	}
	if err := tt.Click("开始扫描"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("停止"); err != nil {
		t.Fatal(err)
	}
	drainAll(t, m, q)
	if len(svc.starts) != 1 || svc.stops != 1 {
		t.Fatalf("during preparation: %d starts, %d stops", len(svc.starts), svc.stops)
	}

	// Capturing: the counts show as proven facts, no percentage.
	applyAndSettle(t, m, q, tt, snap("s1", 2, true, scan.PhaseCapturing,
		scan.Counts{Captured: 5, Produced: 2, Failed: 1, Unprocessed: 1, Queued: 1}, 0, true, ""))
	if !tt.HasText("阶段：采集中") || !tt.HasText("已采集 5 · 已产出 2 · 失败 1 · 未处理 1 · 排队 1 · 识别中 0") {
		t.Errorf("the provable counts do not show: %q", tt.Texts())
	}
	if err := tt.Click("停止"); err != nil {
		t.Fatal(err)
	}
	drainAll(t, m, q)
	if svc.stops != 2 {
		t.Fatalf("the stop button stopped working while busy: %d", svc.stops)
	}

	// Ended: the end reason with its first trigger shows, the stop is
	// disabled, and a new start is possible again with a fresh request
	// identity.
	ended := snap("s1", 3, false, scan.PhaseEnded,
		scan.Counts{Captured: 5, Produced: 3, Failed: 1, Unprocessed: 1}, 5, false, "")
	ended.Scan.End = scan.EndCancelled
	ended.Scan.Trigger = &scan.Trigger{
		Kind: scan.TriggerUser, Code: scan.CodeUserCancelled, Phase: scan.PhaseCapturing,
	}
	applyAndSettle(t, m, q, tt, ended)
	if !tt.HasText("结束：用户取消（用户停止，阶段：采集中（userCancelled））") {
		t.Errorf("the end reason does not show: %q", tt.Texts())
	}
	if err := tt.Click("停止"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("开始扫描"); err != nil {
		t.Fatal(err)
	}
	drainAll(t, m, q)
	if svc.stops != 2 {
		t.Fatalf("the disabled stop button still stopped: %d", svc.stops)
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
	if !tt.HasText("暂无可用扫描源：" + reason) {
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
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)
	m.OpenHistory()
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

	// Every outcome stays in the list, and the recovery mark shows.
	for _, want := range []string{
		"结果：历史记录 r1（4 条，中断恢复 1 条）",
		"河豚电音", "识别失败：识别超时", "未处理（用户取消）", "未处理（中断）",
	} {
		if !tt.HasText(want) {
			t.Fatalf("the results list does not show %q: %q", want, tt.Texts())
		}
	}

	// The first record is chosen by default. Its structured form shows
	// while the evidence stays folded; revealing the evidence adds the
	// raw texts and scores with their images.
	if !tt.HasText("序号 1 · 已产出 · 质量：未评价") {
		t.Fatalf("the default selection does not show: %q", tt.Texts())
	}
	if tt.HasText("模型分数 0.900") {
		t.Fatalf("the evidence showed before it was asked for: %q", tt.Texts())
	}
	if err := tt.Click("显示证据"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if !tt.HasText("模型分数 0.900") || !tt.HasText("+15") {
		t.Fatalf("the produced texts or scores do not show: %q", tt.Texts())
	}
	if n := countColor(tt.Image(), item1A); n < 100 {
		t.Fatalf("the first item's first image does not show (%d px)", n)
	}

	// The failed row: its reason shows and only its images render.
	if err := tt.Click("识别失败：识别超时"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if !tt.HasText("序号 2 · 失败") {
		t.Fatalf("the failed row's details do not show: %q", tt.Texts())
	}
	if n := countColor(tt.Image(), item2A); n < 100 {
		t.Fatalf("the failed item's image does not show (%d px)", n)
	}
	if n := countColor(tt.Image(), item1A); n != 0 {
		t.Fatalf("another item's image shows for the failed item (%d px)", n)
	}

	// The user-cancelled row shows its unprocessed reason.
	if err := tt.Click("未处理（用户取消）"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if !tt.HasText("未处理原因：用户取消（识别未运行）") {
		t.Fatalf("the cancelled reason does not show: %q", tt.Texts())
	}
	if !tt.HasText("（未运行识别）") {
		t.Fatalf("the unrun recognition does not show: %q", tt.Texts())
	}
	if n := countColor(tt.Image(), item1A); n != 0 {
		t.Fatalf("another item's image shows for the cancelled item (%d px)", n)
	}
}

// TestViewStructuredFormAndEvidenceToggle checks the results detail's
// default shape: the read-only structured form is visible, the evidence
// stays collapsed, and the toggle reveals and hides the raw OCR evidence
// without losing the selected item's images.
func TestViewStructuredFormAndEvidenceToggle(t *testing.T) {
	img := color.RGBA{R: 171, G: 31, B: 91, A: 255}
	ev := &fakeEvidence{images: map[string]*ui.Bitmap{
		"r1/000001-line0": solidBitmap(img),
	}}
	m, q, tt := openTestRecord(t, ev, 0)

	for _, want := range []string{
		"套装", "河豚电音（31100）", "稀有度", "S", "槽位", "1 号位",
		"等级", "15 级", "主词条", "生命值 · 2200 · 固定值",
		"副词条", "暴击伤害 · 19.2 · 百分比 · 强化 +3",
		"管理状态", "装备", "校验状态", "不可用",
		"截图与原始识别文本已折叠。",
	} {
		if !tt.HasText(want) {
			t.Fatalf("the structured form does not show %q: %q", want, tt.Texts())
		}
	}
	if tt.HasText("无标记") || tt.HasText("未装备") {
		t.Fatalf("a null management/equipped rendered as a read state: %q", tt.Texts())
	}
	if tt.HasText("模型分数") || tt.HasText("证据（原始 OCR）") {
		t.Fatalf("the evidence occupied the main view: %q", tt.Texts())
	}
	if n := countColor(tt.Image(), img); n != 0 {
		t.Fatalf("the collapsed evidence still showed its image (%d px)", n)
	}

	if err := tt.Click("显示证据"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if !tt.HasText("隐藏证据") || !tt.HasText("证据（原始 OCR）") || !tt.HasText("模型分数 0.900") {
		t.Fatalf("the evidence did not reveal: %q", tt.Texts())
	}
	if n := countColor(tt.Image(), img); n < 100 {
		t.Fatalf("the revealed evidence image does not show (%d px)", n)
	}

	if err := tt.Click("隐藏证据"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if !tt.HasText("显示证据") || tt.HasText("模型分数") || tt.HasText("证据（原始 OCR）") {
		t.Fatalf("the evidence did not fold back: %q", tt.Texts())
	}
	if n := countColor(tt.Image(), img); n != 0 {
		t.Fatalf("the folded evidence still showed its image (%d px)", n)
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

	// Reveal the evidence area before checking image loading and switching.
	if err := tt.Click("显示证据"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)

	// The first item is chosen and its loads are scheduled. Switch to the
	// second item before the loads land, then settle: the second item's
	// own images show, the first item's do not.
	if err := tt.Click("识别失败：识别超时"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if !tt.HasText("序号 2 · 失败") {
		t.Fatalf("the switch did not choose the second item: %q", tt.Texts())
	}
	if n := countColor(tt.Image(), item2A); n < 100 {
		t.Fatalf("the second item's image does not show after the switch (%d px)", n)
	}
	if n := countColor(tt.Image(), item1A); n != 0 {
		t.Fatalf("the first item's image shows for the second item (%d px)", n)
	}
	loaded := ev.callsCount()
	if loaded < 4 {
		t.Fatalf("both items' evidence should have loaded, got %d calls", loaded)
	}

	// Switching back shows the first item's images from the cache without
	// loading anything again.
	if err := tt.Click("河豚电音"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if n := countColor(tt.Image(), item1A); n < 100 {
		t.Fatalf("the first item's image does not show after switching back (%d px)", n)
	}
	if n := countColor(tt.Image(), item2A); n != 0 {
		t.Fatalf("the second item's image still shows after switching back (%d px)", n)
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
	if err := tt.Click("显示证据"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if !tt.HasText("证据图片不可用：missing evidence 000001-line0") {
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
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)

	applyAndSettle(t, m, q, tt, snap("s1", 3, false, scan.PhaseEnded,
		scan.Counts{Captured: 4, Produced: 4}, 2, true, "磁盘已满"))
	if !tt.HasText("保存失败：磁盘已满（结果尚未保存）") {
		t.Fatalf("the save failure does not show: %q", tt.Texts())
	}
	if tt.HasText("已保存 2 条 · 有内容待写入") {
		t.Fatal("a failed save showed as merely pending")
	}

	// The retried save succeeded: the snapshot is clean and the retry
	// disappears.
	applyAndSettle(t, m, q, tt, snap("s1", 4, false, scan.PhaseEnded,
		scan.Counts{Captured: 4, Produced: 4}, 4, false, ""))
	if !tt.HasText("已保存 4 条") {
		t.Fatalf("the saved count does not show: %q", tt.Texts())
	}
	if tt.HasText("重试保存") || tt.HasText("保存失败") {
		t.Fatalf("the retry still shows after a clean save: %q", tt.Texts())
	}
}

// TestViewSaveRetryIsIssued checks the retry button reaching the service
// and disappearing once the save settled.
func TestViewSaveRetryIsIssued(t *testing.T) {
	svc := &fakeService{}
	m, q := newTestModel(t, Config{Service: svc})
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
	tt2 := ui.NewTester(m2.View, 1020, 700)
	settle(t, m2, q2, tt2)
	if m2.AllowClose() {
		t.Fatal("the close was allowed before the wrap-up")
	}
	drainAll(t, m2, q2)
	applyAndSettle(t, m2, q2, tt2, snap("s1", 3, false, scan.PhaseEnded,
		scan.Counts{Captured: 4, Produced: 4}, 2, true, "磁盘已满"))
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
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)
	if !tt.HasText("2026-10-17 09:00 · new") {
		t.Fatalf("the history entries do not show: %q", tt.Texts())
	}

	// Open the default entry, then the older one through the picker.
	if err := tt.Click("打开记录"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if !tt.HasText("结果：历史记录 new（4 条，中断恢复 1 条）") {
		t.Fatalf("the reopened record does not show: %q", tt.Texts())
	}
	m.histChoice = "2026-10-16 09:00 · old"
	m.OpenHistory()
	settle(t, m, q, tt)
	if !tt.HasText("啄木鸟电音") || tt.HasText("中断恢复") {
		t.Fatalf("the older record did not open: %q", tt.Texts())
	}

	// A scan happened before the history was opened: its record is
	// reachable again.
	applyAndSettle(t, m, q, tt, snap("live", 1, false, scan.PhaseEnded,
		scan.Counts{Captured: 1, Produced: 1}, 1, false, ""))
	m.OpenHistory()
	settle(t, m, q, tt)
	if !tt.HasText("啄木鸟电音") {
		t.Fatalf("the history record did not reopen: %q", tt.Texts())
	}
	if err := tt.Click("查看本次扫描"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
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
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)
	m.OpenHistory()
	settle(t, m, q, tt)
	if err := tt.Click("显示证据"); err != nil {
		t.Fatal(err)
	}
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
	if err := tt.Click("震星迪斯科"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if ev.callsCount() != 12 {
		t.Fatalf("the second item's six lines did not load: %d calls", ev.callsCount())
	}
	if len(m.cache.entries) != 8 {
		t.Fatalf("the cache holds %d entries, want the limit 8", len(m.cache.entries))
	}

	// Switching back reloads only what was evicted.
	if err := tt.Click("河豚电音"); err != nil {
		t.Fatal(err)
	}
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
