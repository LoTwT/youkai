package ui

import (
	"fmt"
	"image"
	"image/color"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/record"
	"github.com/LoTwT/youkai/internal/scan"
	"github.com/egoist/mygo/ui"
)

// TestNavRailIsFixedWidth checks the 64-DIP rail with its 52-DIP items at
// the default and minimum window sizes: the three destinations exist, no
// wide sidebar remains, and settings stays pinned to the bottom.
func TestNavRailIsFixedWidth(t *testing.T) {
	for _, tc := range []struct {
		width, height int
	}{
		{1180, 760},
		{960, 640},
	} {
		m, q := newTestModel(t, Config{Service: &fakeService{}})
		m.SetContentSize(tc.width, tc.height)
		tt := ui.NewTester(m.View, tc.width, tc.height)
		settle(t, m, q, tt)

		for _, label := range []string{"扫描", "记录", "设置"} {
			rect, ok := tt.Find(label)
			if !ok {
				t.Fatalf("at %d: the %q item is missing", tc.width, label)
			}
			if rect.X < 0 || rect.X+rect.W > navRailWidth {
				t.Errorf("at %d: %q at %+v does not fit the %d-DIP rail", tc.width, label, rect, navRailWidth)
			}
			if rect.W < navRailItemWidth-1 {
				t.Errorf("at %d: %q is %v wide, want the %d-DIP rail item", tc.width, label, rect.W, navRailItemWidth)
			}
		}
		if _, ok := tt.Find("历史"); ok {
			t.Fatal("the old wide sidebar's history destination is still rendered")
		}
		scan, _ := tt.Find("扫描")
		records, _ := tt.Find("记录")
		settings, _ := tt.Find("设置")
		if !(scan.Y < records.Y && records.Y < settings.Y) {
			t.Errorf("at %d: the rail order is scan %v, records %v, settings %v",
				tc.width, scan.Y, records.Y, settings.Y)
		}
		if settings.Y+settings.H < float32(tc.height)*0.8 {
			t.Errorf("at %d: settings at %+v is not pinned to the bottom", tc.width, settings)
		}
	}
}

// TestNavRailIsTheFirstTabGroup checks the design's focus order: Tab
// reaches the rail before any page content, in the rail's own order
// (design 09 §4.4).
func TestNavRailIsTheFirstTabGroup(t *testing.T) {
	m, q := newTestModel(t, Config{Service: &fakeService{},
		ScanSource: ScanSourceInfo{Available: true}})
	tt := ui.NewTester(m.View, 1180, 760)
	settle(t, m, q, tt)

	for _, label := range []string{"扫描", "记录", "设置"} {
		tt.Key(0, ui.KeyTab)
		if !tt.Focused(label) {
			t.Fatalf("Tab did not focus the %q destination next", label)
		}
	}
	// The rail is the first group; the scan page's action follows it.
	tt.Key(0, ui.KeyTab)
	if !tt.Focused("开始扫描") {
		t.Fatalf("Tab after the rail did not reach the scan action: %q", tt.Texts())
	}
}

// countColorInRect counts the pixels of a rendered frame within a small
// tolerance of c inside one region.
func countColorInRect(img *image.RGBA, c color.RGBA, r image.Rectangle) int {
	n := 0
	rect := r.Intersect(img.Bounds())
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
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

// TestNavRailActiveState checks that the current destination has the
// design's accent-soft face and the others do not, and that an opened
// record's workspace keeps 记录 highlighted.
func TestNavRailActiveState(t *testing.T) {
	m, q := newTestModel(t, Config{Service: &fakeService{}})
	tt := ui.NewTester(m.View, 1180, 760)
	settle(t, m, q, tt)

	soft := tokensFor(false).AccentSoft
	active := color.RGBA{R: soft.R, G: soft.G, B: soft.B, A: 255}
	rectImage := func(r ui.Rect) image.Rectangle {
		return image.Rect(int(r.X), int(r.Y), int(r.X+r.W), int(r.Y+r.H))
	}
	faceIsActive := func(label string) bool {
		rect, ok := tt.Find(label)
		if !ok {
			t.Fatalf("the %q item is missing", label)
		}
		want := int(rect.W * rect.H / 2)
		return countColorInRect(tt.Image(), active, rectImage(rect)) >= want
	}

	if m.displayPage() != pageScan {
		t.Fatalf("the initial page is %v, want scan", m.displayPage())
	}
	if !faceIsActive("扫描") || faceIsActive("记录") || faceIsActive("设置") {
		t.Fatal("the scan destination alone is not active at startup")
	}

	if err := tt.Click("记录"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if m.displayPage() != pageHistory {
		t.Fatalf("the page is %v after clicking records, want the list", m.displayPage())
	}
	if faceIsActive("扫描") || !faceIsActive("记录") || faceIsActive("设置") {
		t.Fatal("the records destination alone is not active")
	}

	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if m.displayPage() != pageSettings {
		t.Fatalf("the page is %v after clicking settings, want settings", m.displayPage())
	}
	if faceIsActive("扫描") || faceIsActive("记录") || !faceIsActive("设置") {
		t.Fatal("the settings destination alone is not active")
	}
}

// TestShellStartsOnScanWithSavedRecords checks the startup landing: a
// history full of records does not pull the page away from scan (design
// 07 §4).
func TestShellStartsOnScanWithSavedRecords(t *testing.T) {
	svc := &fakeService{
		list: []record.RecordInfo{{
			ID: "r1", LatestRevision: 1,
			SavedAt: time.Date(2026, 10, 17, 12, 30, 0, 0, time.UTC),
		}},
		loads: map[string]*record.Recovery{"r1": testRecord()},
	}
	m, q := newTestModel(t, Config{Service: svc})
	tt := ui.NewTester(m.View, 1180, 760)
	settle(t, m, q, tt)
	drainAll(t, m, q)
	tt.Frame()

	if m.displayPage() != pageScan || !tt.HasText(scanHint) {
		t.Fatalf("startup lands on page %v, want the scan page", m.displayPage())
	}
	if tt.HasText("全部记录") {
		t.Fatal("startup shows the records list instead of the scan page")
	}
}

// TestRecordsDestinationRoutesWithoutStageSwitch checks the records
// destination's two levels: the whole list opens a record's workspace
// (starting on 摘要), only the workspace's own back action returns to the
// list, the scan destination is prepare, and the rail restores the opened
// record. There is no in-page stage switch.
func TestRecordsDestinationRoutesWithoutStageSwitch(t *testing.T) {
	svc := &fakeService{
		list: []record.RecordInfo{{
			ID: "r1", LatestRevision: 1,
			SavedAt: time.Date(2026, 10, 17, 12, 30, 0, 0, time.UTC),
		}},
		loads: map[string]*record.Recovery{"r1": testRecord()},
	}
	m, q := newTestModel(t, Config{Service: svc})
	tt := ui.NewTester(m.View, 1180, 760)
	settle(t, m, q, tt)
	m.page = pageScan
	tt.Frame()

	if !tt.HasText(scanHint) {
		t.Fatal("the scan page does not show the scan hint")
	}
	if tt.HasText("结果与校对") {
		t.Fatal("the scan page still shows the in-page stage switch")
	}

	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("关闭窗口时停止扫描并退出应用") {
		t.Fatal("the settings page does not show the close preference")
	}
	if err := tt.Click("记录"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("搜索日期") || !tt.HasText("全部记录") {
		t.Fatalf("the records list does not show its records: %q", tt.Texts())
	}
	if err := tt.Click("打开"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if m.displayPage() != pageResults || m.recordTab != recordTabSummary {
		t.Fatalf("opening a record shows page %v tab %v, want the summary", m.displayPage(), m.recordTab)
	}
	if !tt.HasText("‹ 全部记录") {
		t.Fatalf("the workspace header does not offer its way back: %q", tt.Texts())
	}
	if !tt.HasText("摘要") || !tt.HasText("逐件") || !tt.HasText("导出…") {
		t.Fatalf("the workspace title bar is incomplete: %q", tt.Texts())
	}
	if tt.HasText("+ 准备新扫描") {
		t.Fatal("the workspace header still offers the scan destination's old action")
	}
	if err := tt.Click("‹ 全部记录"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if m.displayPage() != pageHistory {
		t.Fatalf("the workspace's back action shows page %v, want the list", m.displayPage())
	}
	if err := tt.Click("扫描"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if m.displayPage() != pageScan || !tt.HasText(scanHint) {
		t.Fatalf("scan shows page %v, want prepare", m.displayPage())
	}
	if m.rec == nil {
		t.Fatal("preparing a new scan discarded the loaded record")
	}
	if err := tt.Click("记录"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if m.displayPage() != pageResults || m.rec == nil || m.rec.ID != "r1" {
		t.Fatalf("records after scan = page %v record %+v, want the opened record",
			m.displayPage(), m.rec)
	}
	if m.recordTab != recordTabSummary {
		t.Fatalf("the restored record opens on tab %v, want the summary", m.recordTab)
	}
}

// TestRecordWorkspaceSwitchesViews checks the segmented control: 逐件
// carries the existing results surface and 摘要 the conclusion, and the
// records destination keeps 记录 highlighted in both.
func TestRecordWorkspaceSwitchesViews(t *testing.T) {
	m, q, tt, _ := openReviewRecordPage(t, reviewRowItem(t, 1, 12))
	tok := tokensFor(false)
	active := color.RGBA{R: tok.AccentSoft.R, G: tok.AccentSoft.G, B: tok.AccentSoft.B, A: 255}
	rect, ok := tt.Find("记录")
	if !ok {
		t.Fatal("the records destination is missing")
	}
	rectImage := image.Rect(int(rect.X), int(rect.Y), int(rect.X+rect.W), int(rect.Y+rect.H))
	if countColorInRect(tt.Image(), active, rectImage) < int(rect.W*rect.H/2) {
		t.Fatal("the records destination is not active in the workspace")
	}

	if err := tt.Click("逐件"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if m.recordTab != recordTabItems {
		t.Fatalf("clicking 逐件 left tab %v", m.recordTab)
	}
	if err := tt.Click("摘要"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if m.recordTab != recordTabSummary {
		t.Fatalf("clicking 摘要 left tab %v", m.recordTab)
	}
	if !tt.HasText("待校对") {
		t.Fatalf("the summary does not state the record's conclusion: %q", tt.Texts())
	}
}

// TestRecordWorkspaceShortcuts checks the two registered skeletons:
// Ctrl+E brings the export controls into view, Shift+F6 returns the
// keyboard to the rail, and F6 moves it into the workspace's first region.
func TestRecordWorkspaceShortcuts(t *testing.T) {
	svc := &fakeService{
		list: []record.RecordInfo{{
			ID: "r1", LatestRevision: 1,
			SavedAt: time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC),
		}},
		loads: map[string]*record.Recovery{
			"r1": recovery(uiStructuredItem(t, 1, record.QualityAutoAccepted)),
		},
	}
	m, q := newTestModel(t, Config{
		Service:           svc,
		DirectExportSaver: &fakeDirectExportSaver{},
	})
	m.SetContentSize(1180, 760)
	tt := ui.NewTester(m.View, 1180, 760)
	settle(t, m, q, tt)
	m.histChoice = slotHistoryLabel(t, m, "r1")
	m.OpenHistory()
	settle(t, m, q, tt)

	if m.recordTab != recordTabSummary || !tt.HasText("导出 JSON") {
		t.Fatalf("the summary does not hold the export controls: tab %v %q", m.recordTab, tt.Texts())
	}
	if err := tt.Click("逐件"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if m.recordTab != recordTabItems {
		t.Fatalf("clicking 逐件 left tab %v", m.recordTab)
	}

	tt.Key(ui.Cmd, ui.KeyE)
	settle(t, m, q, tt)
	if m.recordTab != recordTabSummary || !tt.HasText("导出 JSON") {
		t.Fatalf("Ctrl+E did not reveal the export controls: tab %v %q", m.recordTab, tt.Texts())
	}

	tt.Key(ui.Shift, ui.KeyF6)
	settle(t, m, q, tt)
	if !tt.Focused("记录") {
		t.Fatalf("Shift+F6 did not return the keyboard to the rail: %q", tt.Texts())
	}
	tt.Key(0, ui.KeyF6)
	settle(t, m, q, tt)
	if !tt.Focused("摘要") {
		t.Fatalf("F6 did not enter the workspace's first region: %q", tt.Texts())
	}

	// A record with no exportable item blocks the export entry, so Ctrl+E
	// is not registered: the item view stays where it is (L-2's reverse
	// path).
	blockedSvc := &fakeService{
		list: []record.RecordInfo{{
			ID: "r2", LatestRevision: 1,
			SavedAt: time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC),
		}},
		loads: map[string]*record.Recovery{
			"r2": recovery(reviewRowItem(t, 1, 12)),
		},
	}
	blockedModel, blockedQueue := newTestModel(t, Config{
		Service:           blockedSvc,
		DirectExportSaver: &fakeDirectExportSaver{},
	})
	blockedModel.SetContentSize(1180, 760)
	blockedTester := ui.NewTester(blockedModel.View, 1180, 760)
	settle(t, blockedModel, blockedQueue, blockedTester)
	blockedModel.histChoice = slotHistoryLabel(t, blockedModel, "r2")
	blockedModel.OpenHistory()
	settle(t, blockedModel, blockedQueue, blockedTester)
	showRecordItems(blockedModel)
	blockedTester.Frame()
	if blockedModel.recordTab != recordTabItems {
		t.Fatalf("the blocked record did not show 逐件: tab %v", blockedModel.recordTab)
	}
	blockedTester.Key(ui.Cmd, ui.KeyE)
	settle(t, blockedModel, blockedQueue, blockedTester)
	if blockedModel.recordTab != recordTabItems {
		t.Fatalf("Ctrl+E moved a blocked export to tab %v", blockedModel.recordTab)
	}
}

// TestWorkspaceShowsTheUnsavedPromptFromTheSummary checks H-1: switching
// to 摘要 keeps the item editor's draft alive, so leaving the record or
// quitting must still show the save/discard prompt from that view.
func TestWorkspaceShowsTheUnsavedPromptFromTheSummary(t *testing.T) {
	m, q, tt, _ := openReviewRecordPage(t, reviewRowItem(t, 1, 12))
	if err := tt.Click("编辑"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	m.edit.draft.Level = "13"
	if err := tt.Click("摘要"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if m.recordTab != recordTabSummary || m.edit == nil {
		t.Fatalf("the summary tab did not keep the draft: tab %v edit %+v", m.recordTab, m.edit)
	}

	// Leaving for settings pauses behind the prompt, visible on 摘要.
	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if m.displayPage() != pageResults || m.navPrompt == nil ||
		m.navPrompt.kind != navPage || m.navPrompt.page != pageSettings {
		t.Fatalf("leaving the summary did not prompt: page %v prompt %+v", m.displayPage(), m.navPrompt)
	}
	if !tt.HasText("当前条目有未保存的修改。") || !tt.HasText("保存修改并继续") {
		t.Fatalf("the summary does not show the unsaved prompt: %q", tt.Texts())
	}
	if !tt.HasText("留在当前项") {
		t.Fatalf("the summary prompt lost its stay action: %q", tt.Texts())
	}

	// A quit requested from the tray shows the same prompt with exit
	// wording while the summary stays on screen.
	m.promptStay()
	tt.Frame()
	if m.AllowClose() {
		t.Fatal("the dirty summary allowed the close")
	}
	tt.Frame()
	if m.navPrompt == nil || m.navPrompt.kind != navExit {
		t.Fatalf("the close did not hold behind the exit prompt: %+v", m.navPrompt)
	}
	if !tt.HasText("当前条目有未保存的修改，退出前请处理。") || !tt.HasText("保存修改并退出") {
		t.Fatalf("the summary does not show the exit prompt: %q", tt.Texts())
	}
	m.promptStay()
}

// TestWorkspaceCoverageMatchesTheConclusion checks M-1: the title bar and
// the summary body read one coverage conclusion. A record that claims
// complete coverage without a verified sort is downgraded by the export
// guard, and both places must say partial.
func TestWorkspaceCoverageMatchesTheConclusion(t *testing.T) {
	rec := recovery(item(1, record.StatusProduced, "", "", "一"))
	rec.Checkpoint.Session.SessionID = "r1"
	rec.Checkpoint.Session.Coverage.Status = record.CoverageComplete
	svc := &fakeService{
		list: []record.RecordInfo{{
			ID: "r1", LatestRevision: 1,
			SavedAt: time.Date(2026, 11, 5, 21, 40, 0, 0, time.UTC),
		}},
		loads: map[string]*record.Recovery{"r1": rec},
	}
	m, q := newTestModel(t, Config{
		Service:           svc,
		DirectExportSaver: &fakeDirectExportSaver{},
	})
	m.SetContentSize(1180, 760)
	tt := ui.NewTester(m.View, 1180, 760)
	settle(t, m, q, tt)
	m.histChoice = slotHistoryLabel(t, m, "r1")
	m.OpenHistory()
	settle(t, m, q, tt)

	if m.recordTab != recordTabSummary {
		t.Fatalf("the record opened on tab %v, want the summary", m.recordTab)
	}
	// HasText is a substring match, so the body's "覆盖：部分覆盖…" would
	// also satisfy it; the title bar's slot must be the exact status.
	coverageHeader := 0
	for _, text := range tt.Texts() {
		if text == "覆盖：部分" {
			coverageHeader++
		}
	}
	if coverageHeader != 1 {
		t.Fatalf("the title bar does not show the export guard's coverage exactly once: %d in %q",
			coverageHeader, tt.Texts())
	}
	if !tt.HasText("覆盖：部分覆盖（导出文件会标注“部分快照”）") {
		t.Fatalf("the summary does not show the same coverage: %q", tt.Texts())
	}
	if tt.HasText("覆盖：完整") {
		t.Fatalf("a coverage the export cannot confirm is shown as complete: %q", tt.Texts())
	}
}

// TestScanActionsStayOnTheScanPage checks the scan page's bottom action
// bar and that the rail, not a workspace header action, returns to a
// running scan.
func TestScanActionsStayOnTheScanPage(t *testing.T) {
	svc := &fakeService{}
	m, q := newTestModel(t, Config{Service: svc})
	tt := ui.NewTester(m.View, 1180, 760)
	settle(t, m, q, tt)

	if err := tt.Click("开始扫描"); err != nil {
		t.Fatal(err)
	}
	drainAll(t, m, q)
	if len(svc.starts) != 1 {
		t.Fatalf("the action bar issued %d starts, want 1", len(svc.starts))
	}

	applyAndSettle(t, m, q, tt, snap("s1", 1, true, scan.PhaseCapturing,
		scan.Counts{Captured: 2, Produced: 1}, 0, false, ""))
	if m.displayPage() != pageScan {
		t.Fatalf("a running scan shows page %v, want the scan page", m.displayPage())
	}
	if !tt.HasText("停止扫描") {
		t.Fatalf("the running scan has no bottom stop action: %q", tt.Texts())
	}
	if err := tt.Click("停止扫描"); err != nil {
		t.Fatal(err)
	}
	drainAll(t, m, q)
	if svc.stops != 1 {
		t.Fatalf("the stop action issued %d stops, want 1", svc.stops)
	}

	// The workspace opened during the scan returns to the progress page
	// through the rail, not a header action.
	m.page = pageResults
	tt.Frame()
	if tt.HasText("返回扫描中") {
		t.Fatalf("the workspace header still offers the return-to-progress action: %q", tt.Texts())
	}
	if err := tt.Click("扫描"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if m.displayPage() != pageScan {
		t.Fatalf("the rail shows page %v, want the running scan", m.displayPage())
	}

	// A finished session lands on the new record's summary.
	ended := snap("s1", 2, false, scan.PhaseEnded,
		scan.Counts{Captured: 2, Produced: 1}, 1, false, "")
	applyAndSettle(t, m, q, tt, ended)
	if m.displayPage() != pageResults || m.recordTab != recordTabSummary {
		t.Fatalf("the ended scan shows page %v tab %v, want the summary",
			m.displayPage(), m.recordTab)
	}
}

// TestNavRailRestoresOpenedRecords checks the rail as a set of
// destinations: history from settings returns to the opened snapshot,
// the history list is reachable from results, and scan restores the live
// session slot without loading it again. Clicking the active destination
// is a no-op.
func TestNavRailRestoresOpenedRecords(t *testing.T) {
	svc := &fakeService{
		list: []record.RecordInfo{{
			ID:      "old",
			SavedAt: time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC),
		}},
		loads: map[string]*record.Recovery{
			"s1":  recovery(item(1, record.StatusProduced, "", "", "本次"), item(2, record.StatusProduced, "", "", "本次二")),
			"old": recovery(item(1, record.StatusProduced, "", "", "旧一")),
		},
	}
	m, q := newTestModel(t, Config{Service: svc})
	tt := ui.NewTester(m.View, 1024, 700)
	settle(t, m, q, tt)
	m.Apply(snap("s1", 1, true, scan.PhaseCapturing,
		scan.Counts{Captured: 2, Produced: 2}, 1, true, ""))
	drainAll(t, m, q)

	// Give the session slot its own view state, then open history.
	m.filter = resultFilterAll
	m.search = "未评价"
	m.selected = 1
	m.histChoice = slotHistoryLabel(t, m, "old")
	m.OpenHistory()
	settle(t, m, q, tt)
	if m.activeSlot != resultSlotHistory || m.rec.ID != "old" {
		t.Fatalf("history did not open: slot %v record %+v", m.activeSlot, m.rec)
	}
	historyLoads, sessionLoads := slotLoadCount(svc, "old"), slotLoadCount(svc, "s1")

	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if m.displayPage() != pageSettings {
		t.Fatalf("settings shows page %v", m.displayPage())
	}
	if err := tt.Click("记录"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if m.displayPage() != pageResults || m.activeSlot != resultSlotHistory ||
		m.rec == nil || m.rec.ID != "old" {
		t.Fatalf("history from settings = page %v slot %v record %+v",
			m.displayPage(), m.activeSlot, m.rec)
	}
	if slotLoadCount(svc, "old") != historyLoads {
		t.Fatal("restoring the opened history reloaded it")
	}

	// The history destination restores the opened snapshot; only its own
	// back action opens the list. Clicking the active destination is a
	// no-op.
	if err := tt.Click("记录"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if m.displayPage() != pageResults || m.activeSlot != resultSlotHistory {
		t.Fatalf("history from results = page %v slot %v, want the opened record",
			m.displayPage(), m.activeSlot)
	}
	if err := tt.Click("‹ 全部记录"); err != nil {
		t.Fatalf("click 返回历史: %v", err)
	}
	tt.Frame()
	if m.displayPage() != pageHistory {
		t.Fatalf("returning to history shows page %v, want the list", m.displayPage())
	}
	if err := tt.Click("记录"); err != nil { // active destination: no-op
		t.Fatal(err)
	}
	tt.Frame()
	if m.displayPage() != pageHistory {
		t.Fatalf("clicking the active records destination moved to page %v", m.displayPage())
	}

	// Scan shows the scan destination, never the live record's workspace;
	// the session slot keeps its stored view for later.
	if err := tt.Click("扫描"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if m.displayPage() != pageScan {
		t.Fatalf("scan from records = page %v, want the scan page", m.displayPage())
	}
	if m.sessionSlot.recordID != "s1" || m.sessionSlot.state.rec == nil ||
		m.sessionSlot.state.rec.ID != "s1" {
		t.Fatalf("the session slot lost its record: %+v", m.sessionSlot)
	}
	if m.sessionSlot.state.filter != resultFilterAll || m.sessionSlot.state.search != "未评价" ||
		m.sessionSlot.state.selected != 1 {
		t.Fatalf("the session slot lost its view: filter %v search %q selected %d",
			m.sessionSlot.state.filter, m.sessionSlot.state.search, m.sessionSlot.state.selected)
	}
	if slotLoadCount(svc, "s1") != sessionLoads {
		t.Fatal("leaving the live session reloaded it")
	}

	// The records destination restores the opened snapshot from the scan
	// page.
	if err := tt.Click("记录"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if m.displayPage() != pageResults || m.activeSlot != resultSlotHistory ||
		m.rec == nil || m.rec.ID != "old" {
		t.Fatalf("records from scan = page %v slot %v record %+v",
			m.displayPage(), m.activeSlot, m.rec)
	}

	// The active settings destination ignores its own clicks.
	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if m.displayPage() != pageSettings {
		t.Fatalf("clicking the active settings destination moved to page %v", m.displayPage())
	}
}

// TestNavRailRecordsRoundTripRestoresViewState checks the history
// destination's two levels: the rail restores the opened snapshot with its
// filter, search, selection and scroll after a scan or settings visit, and
// only the snapshot's back action opens the list. Neither round trip
// reloads the record.
func TestNavRailRecordsRoundTripRestoresViewState(t *testing.T) {
	old := manyPendingRecord(t, 60)
	old.Checkpoint.Session.SessionID = "old"
	svc := &fakeService{
		list: []record.RecordInfo{{
			ID:      "old",
			SavedAt: time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC),
		}},
		loads: map[string]*record.Recovery{
			"s1": recovery(
				item(1, record.StatusProduced, "", "", "本次一"),
				item(2, record.StatusProduced, "", "", "本次二"),
			),
			"old": old,
		},
	}
	m, q := newTestModel(t, Config{Service: svc})
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)
	m.Apply(snap("s1", 1, true, scan.PhaseCapturing,
		scan.Counts{Captured: 2, Produced: 2}, 1, true, ""))
	drainAll(t, m, q)

	m.histChoice = slotHistoryLabel(t, m, "old")
	m.OpenHistory()
	settle(t, m, q, tt)
	showRecordItems(m)
	tt.Frame()
	if m.activeSlot != resultSlotHistory || m.rec == nil || m.rec.ID != "old" {
		t.Fatalf("history did not open: slot %v record %+v", m.activeSlot, m.rec)
	}
	loads := slotLoadCount(svc, "old")

	// A segment, a search that keeps every row, a set filter, a chosen row
	// and a first visible row well down the list.
	m.filter = resultFilterAll
	m.search = "待校对"
	m.setFilter = "31100"
	m.selected = 32
	m.selectedSequence = m.rec.Rows[32].Seq
	m.scroll = 30
	m.list.ScrollTo(30, ui.Start)
	tt.Frame()
	if first, _ := m.list.Visible(); first != 30 {
		t.Fatalf("the history list starts at row %d, want 30", first)
	}
	wantSeq := m.rec.Rows[32].Seq

	checkRestored := func(stage string) {
		t.Helper()
		if m.displayPage() != pageResults || m.activeSlot != resultSlotHistory ||
			m.rec == nil || m.rec.ID != "old" {
			t.Fatalf("%s: page %v slot %v record %+v", stage,
				m.displayPage(), m.activeSlot, m.rec)
		}
		if m.filter != resultFilterAll || m.search != "待校对" || m.setFilter != "31100" {
			t.Fatalf("%s: view state = filter %v search %q set %q",
				stage, m.filter, m.search, m.setFilter)
		}
		if m.selected != 32 || m.selectedSeq() != wantSeq {
			t.Fatalf("%s: selection = %d (seq %d), want 32 (seq %d)",
				stage, m.selected, m.selectedSeq(), wantSeq)
		}
		tt.Frame()
		if first, _ := m.list.Visible(); first != 30 {
			t.Fatalf("%s: the list starts at row %d, want 30", stage, first)
		}
		if got := slotLoadCount(svc, "old"); got != loads {
			t.Fatalf("%s: restoring reloaded the record (%d loads, want %d)", stage, got, loads)
		}
	}

	// History → scan (the live session) → history.
	if err := tt.Click("扫描"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if m.displayPage() != pageScan {
		t.Fatalf("scan from records = page %v, want the scan page", m.displayPage())
	}
	if err := tt.Click("记录"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	checkRestored("records after scan")

	// Records → settings → records.
	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if m.displayPage() != pageSettings {
		t.Fatalf("settings shows page %v", m.displayPage())
	}
	if err := tt.Click("记录"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	checkRestored("records after settings")

	// The snapshot's own back action opens the list; the rail then
	// returns to the opened record rather than the list.
	if err := tt.Click("‹ 全部记录"); err != nil {
		t.Fatalf("click the workspace back action: %v", err)
	}
	tt.Frame()
	if m.displayPage() != pageHistory {
		t.Fatalf("returning to the list shows page %v", m.displayPage())
	}
	if err := tt.Click("扫描"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if m.displayPage() != pageScan {
		t.Fatalf("scan from the records list shows page %v", m.displayPage())
	}
	if err := tt.Click("记录"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	checkRestored("records from the list")
}

// TestNavRailHighlightsRecordsForAnOpenedRecord checks that a reopened
// record, rendered by the workspace, belongs to the records destination in
// the rail: records is active and scan is not.
func TestNavRailHighlightsRecordsForAnOpenedRecord(t *testing.T) {
	m, _, tt, _ := openReviewRecordPage(t, reviewRowItem(t, 1, 12))
	if m.displayPage() != pageResults || m.activeSlot != resultSlotHistory {
		t.Fatalf("the record is not shown by the history slot: page %v slot %v",
			m.displayPage(), m.activeSlot)
	}
	soft := tokensFor(false).AccentSoft
	active := color.RGBA{R: soft.R, G: soft.G, B: soft.B, A: 255}
	rectImage := func(r ui.Rect) image.Rectangle {
		return image.Rect(int(r.X), int(r.Y), int(r.X+r.W), int(r.Y+r.H))
	}
	faceIsActive := func(label string) bool {
		rect, ok := tt.Find(label)
		if !ok {
			t.Fatalf("the %q item is missing", label)
		}
		want := int(rect.W * rect.H / 2)
		return countColorInRect(tt.Image(), active, rectImage(rect)) >= want
	}
	if faceIsActive("扫描") {
		t.Fatal("the scan destination is active for an opened record")
	}
	if !faceIsActive("记录") {
		t.Fatal("the records destination is not active for its opened record")
	}
	if faceIsActive("设置") {
		t.Fatal("the settings destination is active for an opened record")
	}
}

// TestNavRailRecordsRevalidatesConfirmedStaySelection checks the same-slot
// revisit after "confirm and stay": leaving the results page drops the
// temporary marker of the confirmed row, so returning through settings
// must move the selection to the first row the pending segment still
// shows instead of keeping the hidden row in the detail.
func TestNavRailRecordsRevalidatesConfirmedStaySelection(t *testing.T) {
	m, q, tt, _ := openReviewRecordPage(t,
		reviewRowItem(t, 1, 12),
		reviewRowItem(t, 2, 10),
	)

	if err := tt.Click("确认"); err != nil {
		t.Fatalf("click 确认: %v", err)
	}
	settle(t, m, q, tt)
	if visible := m.visibleResultRows(); m.pendingCount() != 1 || len(visible) != 2 ||
		m.selected != 0 || m.selectedSeq() != 1 {
		t.Fatalf("confirm-stay setup: pending %d selected %d (seq %d) visible %v",
			m.pendingCount(), m.selected, m.selectedSeq(), visible)
	}

	if err := tt.Click("设置"); err != nil {
		t.Fatalf("click 设置: %v", err)
	}
	settle(t, m, q, tt)
	if err := tt.Click("记录"); err != nil {
		t.Fatalf("click 记录: %v", err)
	}
	settle(t, m, q, tt)

	visible := m.visibleResultRows()
	if len(visible) != 1 || visible[0] != 1 {
		t.Fatalf("the pending segment shows %v, want only row 1", visible)
	}
	if m.selected != visible[0] || m.selectedSeq() != 2 {
		t.Fatalf("the detail keeps the hidden row: selected %d (seq %d), visible %v",
			m.selected, m.selectedSeq(), visible)
	}
	if m.scroll != 0 {
		t.Fatalf("the restored scroll is %d, want the first visible row", m.scroll)
	}
	// The rendered detail follows the settled selection.
	if !tt.HasText("2 · ") {
		t.Fatalf("the detail does not show row 2: %q", tt.Texts())
	}

	// A selection that the filter hides also falls back on a plain
	// same-slot activation, not only on the navigation path above.
	m.selected = 0
	m.selectedSequence = 1
	m.activateResultSlot(resultSlotHistory)
	if m.selected != 1 || m.selectedSeq() != 2 {
		t.Fatalf("an out-of-filter selection was kept: selected %d (seq %d)",
			m.selected, m.selectedSeq())
	}
}

// TestHistoryConfirmedStayListReopenRevalidatesSelection checks the
// "confirm and stay → back to the history list → open the same record"
// path: the list drops the temporary marker, so the reopen must settle the
// selection against the pending segment instead of leaving the confirmed
// row's detail under a list that no longer shows it.
func TestHistoryConfirmedStayListReopenRevalidatesSelection(t *testing.T) {
	for _, width := range []int{1440, 1024} {
		for _, rows := range []int{2, 1} {
			t.Run(fmt.Sprintf("%d/%drows", width, rows), func(t *testing.T) {
				items := []record.ItemRecord{
					reviewRowItem(t, 1, 12),
					reviewRowItem(t, 2, 10),
				}[:rows]
				rec := recovery(items...)
				rec.Checkpoint.Session.SessionID = "r1"
				svc := &fakeService{
					list: []record.RecordInfo{{
						ID:             "r1",
						SavedAt:        time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC),
						LatestRevision: 1,
					}},
					loads: map[string]*record.Recovery{"r1": rec},
				}
				installReviewSave(svc)
				m, q := newTestModel(t, Config{Service: svc})
				height := 700
				if width == 1440 {
					height = 900
				}
				m.SetContentSize(width, height)
				tt := ui.NewTester(m.View, width, height)
				settle(t, m, q, tt)
				m.histChoice = slotHistoryLabel(t, m, "r1")
				m.OpenHistory()
				settle(t, m, q, tt)
				showRecordItems(m)
				tt.Frame()
				if m.activeSlot != resultSlotHistory || m.rec == nil || m.rec.ID != "r1" {
					t.Fatalf("the history record did not open: slot %v record %+v", m.activeSlot, m.rec)
				}
				loads := slotLoadCount(svc, "r1")
				rowLabel := historyTimeText(m.history[0])

				if err := tt.Click("确认"); err != nil {
					t.Fatalf("click 确认: %v", err)
				}
				settle(t, m, q, tt)
				wantVisible := 0
				if rows == 2 {
					// The confirmed row lingers in this visit's pending
					// segment only while the results page stays open.
					wantVisible = 2
				}
				if visible := m.visibleResultRows(); m.pendingCount() != rows-1 ||
					len(visible) != wantVisible || m.selected != 0 || m.selectedSeq() != 1 {
					t.Fatalf("confirm-stay setup: pending %d selected %d (seq %d) visible %v",
						m.pendingCount(), m.selected, m.selectedSeq(), visible)
				}

				if err := tt.Click("‹ 全部记录"); err != nil {
					t.Fatalf("click 返回历史: %v", err)
				}
				settle(t, m, q, tt)
				if m.displayPage() != pageHistory {
					t.Fatalf("returning to history shows page %v, want the list", m.displayPage())
				}
				if err := tt.Click(rowLabel); err != nil {
					t.Fatalf("click the history row %q: %v", rowLabel, err)
				}
				tt.Key(0, ui.KeyEnter)
				settle(t, m, q, tt)
				if m.displayPage() != pageResults || m.activeSlot != resultSlotHistory ||
					m.rec == nil || m.rec.ID != "r1" {
					t.Fatalf("reopening = page %v slot %v record %+v",
						m.displayPage(), m.activeSlot, m.rec)
				}
				visible := m.visibleResultRows()
				want := -1
				if len(visible) > 0 {
					want = visible[0]
				}
				if m.selected != want {
					t.Fatalf("the detail keeps a row outside the segment: selected %d, want %d (visible %v)",
						m.selected, want, visible)
				}
				if m.selected >= 0 && m.selectedSeq() != m.rec.Rows[m.selected].Seq {
					t.Fatalf("the selection sequence %d does not match row %d",
						m.selectedSeq(), m.selected)
				}
				if m.scroll != 0 {
					t.Fatalf("the fallback scroll is %d, want the first visible row", m.scroll)
				}
				switch rows {
				case 2:
					if len(visible) != 1 || visible[0] != 1 || m.selectedSeq() != 2 {
						t.Fatalf("the reopened list shows %v selected %d, want only row 2",
							visible, m.selected)
					}
					if !tt.HasText("2 · ") || tt.HasText("1 · ") {
						t.Fatalf("the detail does not follow row 2: %q", tt.Texts())
					}
				case 1:
					if len(visible) != 0 || m.selected != -1 {
						t.Fatalf("the completed segment shows %v selected %d",
							visible, m.selected)
					}
					if tt.HasText("1 · ") {
						t.Fatalf("the empty list still shows the confirmed row's detail: %q", tt.Texts())
					}
					if !tt.HasText("结果已保存，没有待校对的条目。") {
						t.Fatalf("the completed segment has no empty state: %q", tt.Texts())
					}
				}
				if got := slotLoadCount(svc, "r1"); got != loads {
					t.Fatalf("reopening from the list reloaded the record (%d loads, want %d)", got, loads)
				}
			})
		}
	}
}

// TestHistoryListReopenSettlesSelectionAgainstFilters checks that opening
// the record already on screen from the list keeps its segment, set/slot
// choices, selection and scroll, and falls back to the first visible row
// when the chosen one no longer matches the filters.
func TestHistoryListReopenSettlesSelectionAgainstFilters(t *testing.T) {
	rec := manyPendingRecord(t, 30)
	rec.Checkpoint.Session.SessionID = "r1"
	svc := &fakeService{
		list: []record.RecordInfo{{
			ID:             "r1",
			SavedAt:        time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC),
			LatestRevision: 1,
		}},
		loads: map[string]*record.Recovery{"r1": rec},
	}
	m, q := newTestModel(t, Config{Service: svc})
	m.SetContentSize(1024, 700)
	tt := ui.NewTester(m.View, 1024, 700)
	settle(t, m, q, tt)
	m.histChoice = slotHistoryLabel(t, m, "r1")
	m.OpenHistory()
	settle(t, m, q, tt)
	showRecordItems(m)
	tt.Frame()
	loads := slotLoadCount(svc, "r1")

	m.filter = resultFilterAll
	m.selected = 13
	m.selectedSequence = m.rec.Rows[13].Seq
	m.scroll = 5
	m.list.ScrollTo(5, ui.Start)
	tt.Frame()
	rowLabel := historyTimeText(m.history[0])

	backToRecord := func() {
		t.Helper()
		if err := tt.Click("‹ 全部记录"); err != nil {
			t.Fatalf("click 返回历史: %v", err)
		}
		settle(t, m, q, tt)
		if m.displayPage() != pageHistory {
			t.Fatalf("returning to history shows page %v, want the list", m.displayPage())
		}
		if err := tt.Click(rowLabel); err != nil {
			t.Fatalf("click the history row %q: %v", rowLabel, err)
		}
		tt.Key(0, ui.KeyEnter)
		settle(t, m, q, tt)
	}

	backToRecord()
	if m.displayPage() != pageResults || m.activeSlot != resultSlotHistory {
		t.Fatalf("reopening = page %v slot %v", m.displayPage(), m.activeSlot)
	}
	if m.filter != resultFilterAll || m.search != "" {
		t.Fatalf("the reopen reset the filters: filter %v search %q", m.filter, m.search)
	}
	if m.selected != 13 || m.selectedSeq() != 14 {
		t.Fatalf("the reopen moved the visible selection: selected %d (seq %d)",
			m.selected, m.selectedSeq())
	}
	tt.Frame()
	if first, _ := m.list.Visible(); first != 5 {
		t.Fatalf("the reopen reset the scroll: first %d, want 5", first)
	}

	// A selection the segment no longer shows falls back to its first row.
	m.slotFilter = "2"
	m.selected = 0
	m.selectedSequence = 1
	backToRecord()
	visible := m.visibleResultRows()
	if len(visible) == 0 || m.selected != visible[0] || m.selectedSeq() != 2 || m.scroll != 0 {
		t.Fatalf("the hidden selection was kept: selected %d (seq %d) scroll %d visible %v",
			m.selected, m.selectedSeq(), m.scroll, visible)
	}
	if m.slotFilter != "2" {
		t.Fatalf("the fallback dropped the set/slot choice: %q", m.slotFilter)
	}
	if !tt.HasText("2 · ") {
		t.Fatalf("the detail does not follow the fallback row: %q", tt.Texts())
	}
	if got := slotLoadCount(svc, "r1"); got != loads {
		t.Fatalf("reopening from the list reloaded the record (%d loads, want %d)", got, loads)
	}
}

// TestNavRailActiveRecordsDestinationIsNoOp checks that the rail's active
// records destination ignores its own click: an unchanged editor stays
// open, a dirty draft gets no save/discard prompt, and the record is not
// reopened.
func TestNavRailActiveRecordsDestinationIsNoOp(t *testing.T) {
	m, _, tt, svc := openReviewRecordPage(t, reviewRowItem(t, 1, 12), reviewRowItem(t, 2, 10))
	loads := slotLoadCount(svc, "r1")

	m.startEdit()
	if m.edit == nil || m.navPrompt != nil {
		t.Fatalf("the clean editor did not open: edit %+v prompt %+v", m.edit, m.navPrompt)
	}
	if err := tt.Click("记录"); err != nil {
		t.Fatalf("click 记录: %v", err)
	}
	tt.Frame()
	if m.edit == nil || m.navPrompt != nil {
		t.Fatalf("clicking the active records destination closed the editor: edit %+v prompt %+v",
			m.edit, m.navPrompt)
	}
	if m.displayPage() != pageResults || m.activeSlot != resultSlotHistory {
		t.Fatalf("the active destination moved: page %v slot %v", m.displayPage(), m.activeSlot)
	}

	m.edit.draft.Level = "13"
	if err := tt.Click("记录"); err != nil {
		t.Fatalf("click 记录 with a dirty draft: %v", err)
	}
	tt.Frame()
	if m.navPrompt != nil {
		t.Fatalf("clicking the active records destination prompted for the same record: %+v",
			m.navPrompt)
	}
	if m.edit == nil || m.edit.draft.Level != "13" {
		t.Fatal("clicking the active records destination lost the dirty draft")
	}
	if got := slotLoadCount(svc, "r1"); got != loads {
		t.Fatalf("clicking the active records destination reloaded the record: %d loads, want %d",
			got, loads)
	}
}

// TestNavRailDestinationsGuardDirtyDrafts checks that switching to the
// scan, history or settings destination never drops an unsaved item: the
// move waits behind the save/discard/stay prompt, and staying keeps the
// draft and the current slot.
func TestNavRailDestinationsGuardDirtyDrafts(t *testing.T) {
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
	tt := ui.NewTester(m.View, 1024, 700)
	settle(t, m, q, tt)
	m.Apply(snap("s1", 1, true, scan.PhaseCapturing,
		scan.Counts{Captured: 1, Produced: 1}, 1, true, ""))
	drainAll(t, m, q)
	m.histChoice = slotHistoryLabel(t, m, "old")
	m.OpenHistory()
	settle(t, m, q, tt)
	showRecordItems(m)
	tt.Frame()

	m.startEdit()
	m.edit.draft.Level = "13"
	if err := tt.Click("扫描"); err != nil {
		t.Fatal(err)
	}
	if m.navPrompt == nil || m.navPrompt.kind != navPage || m.navPrompt.page != pageScan {
		t.Fatalf("dirty history did not prompt before scan: %+v", m.navPrompt)
	}
	if m.displayPage() != pageResults || m.activeSlot != resultSlotHistory {
		t.Fatal("the dirty history slot switched before the prompt settled")
	}
	m.promptStay()
	if m.edit == nil || m.edit.draft.Level != "13" {
		t.Fatal("staying lost the dirty history draft")
	}

	// Discarding the draft opens the scan page.
	if err := tt.Click("扫描"); err != nil {
		t.Fatal(err)
	}
	if m.navPrompt == nil || m.navPrompt.kind != navPage || m.navPrompt.page != pageScan {
		t.Fatalf("the second scan click did not prompt: %+v", m.navPrompt)
	}
	m.promptDiscardAndContinue()
	drainAll(t, m, q)
	if m.displayPage() != pageScan || m.edit != nil {
		t.Fatalf("discarding did not open the scan page: page %v edit %v", m.displayPage(), m.edit)
	}

	// The records destination restores the opened record from scan.
	if err := tt.Click("记录"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if m.activeSlot != resultSlotHistory || m.rec == nil || m.rec.ID != "old" {
		t.Fatalf("records from scan did not restore the opened record: slot %v record %+v",
			m.activeSlot, m.rec)
	}

	// A dirty record prompts before settings.
	m.startEdit()
	m.edit.draft.Level = "14"
	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	if m.navPrompt == nil || m.navPrompt.kind != navPage || m.navPrompt.page != pageSettings {
		t.Fatalf("dirty history did not prompt before settings: %+v", m.navPrompt)
	}
	if m.displayPage() != pageResults {
		t.Fatal("settings opened before the dirty prompt settled")
	}
	m.promptStay()
	if m.edit == nil || m.edit.draft.Level != "14" {
		t.Fatal("staying lost the dirty record draft")
	}
}

// TestNavRailRecordsRestoresActualScroll scrolls the history table with
// pointer input, leaves through settings or the prepare page, and returns
// to the same snapshot. The destination must keep the list's real place,
// not just the last explicitly captured one.
func TestNavRailRecordsRestoresActualScroll(t *testing.T) {
	for _, width := range []int{1440, 1024, 1020, 860} {
		height := 700
		switch width {
		case 1440:
			height = 900
		case 860:
			height = 560
		}
		for _, route := range []string{"设置", "扫描"} {
			t.Run(fmt.Sprintf("%d/%s", width, route), func(t *testing.T) {
				old := manyPendingRecord(t, 30)
				old.Checkpoint.Session.SessionID = "old"
				svc := &fakeService{
					list: []record.RecordInfo{{
						ID:      "old",
						SavedAt: time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC),
					}},
					loads: map[string]*record.Recovery{"old": old},
				}
				m, q := newTestModel(t, Config{Service: svc})
				m.SetContentSize(width, height)
				tt := ui.NewTester(m.View, width, height)
				settle(t, m, q, tt)
				m.histChoice = slotHistoryLabel(t, m, "old")
				m.OpenHistory()
				settle(t, m, q, tt)
				showRecordItems(m)
				tt.Frame()
				if m.activeSlot != resultSlotHistory || m.rec == nil || m.rec.ID != "old" {
					t.Fatalf("history did not open: slot %v record %+v", m.activeSlot, m.rec)
				}

				// Actual pointer scrolling: no direct m.scroll or
				// List.ScrollTo writes.
				tt.Scroll(350, 350, 0, 240)
				tt.Advance(time.Second)
				settle(t, m, q, tt)
				first, _ := m.list.Visible()
				if first <= 0 {
					t.Fatalf("pointer input did not scroll the list: first %d", first)
				}
				selected, filter := m.selected, m.filter
				loads := slotLoadCount(svc, "old")

				if err := tt.Click(route); err != nil {
					t.Fatalf("click %s: %v", route, err)
				}
				settle(t, m, q, tt)
				if route == "扫描" && m.displayPage() != pageScan {
					t.Fatalf("scan without a session shows page %v", m.displayPage())
				}
				if err := tt.Click("记录"); err != nil {
					t.Fatalf("click 记录: %v", err)
				}
				settle(t, m, q, tt)

				after, _ := m.list.Visible()
				if m.displayPage() != pageResults || m.activeSlot != resultSlotHistory ||
					m.rec == nil || m.rec.ID != "old" {
					t.Fatalf("returning from %s = page %v slot %v record %+v",
						route, m.displayPage(), m.activeSlot, m.rec)
				}
				if m.filter != filter || m.selected != selected {
					t.Fatalf("returning from %s changed the view: filter %v→%v selected %d→%d",
						route, filter, m.filter, selected, m.selected)
				}
				if after != first {
					t.Fatalf("returning from %s reset the list: first %d→%d",
						route, first, after)
				}
				if got := slotLoadCount(svc, "old"); got != loads {
					t.Fatalf("returning from %s reloaded the record (%d loads, want %d)",
						route, got, loads)
				}
			})
		}
	}
}

// TestNavRailSessionConfirmedStayRevisitRevalidates returns to the live
// session after "confirm and stay" through settings. The confirmed row has
// left the pending segment, so the revisit must revalidate the selection
// against the visible rows instead of keeping its hidden detail.
func TestNavRailSessionConfirmedStayRevisitRevalidates(t *testing.T) {
	for _, width := range []int{1440, 1024, 1020, 860} {
		for _, rows := range []int{2, 1} {
			t.Run(fmt.Sprintf("%d/%drows", width, rows), func(t *testing.T) {
				items := []record.ItemRecord{
					reviewRowItem(t, 1, 12),
					reviewRowItem(t, 2, 10),
				}[:rows]
				rec := recovery(items...)
				rec.Checkpoint.Session.SessionID = "s1"
				svc := &fakeService{loads: map[string]*record.Recovery{"s1": rec}}
				installReviewSave(svc)
				m, q := newTestModel(t, Config{Service: svc})
				height := 700
				if width == 1440 {
					height = 900
				}
				if width == 860 {
					height = 560
				}
				m.SetContentSize(width, height)
				tt := ui.NewTester(m.View, width, height)
				settle(t, m, q, tt)
				m.Apply(snap("s1", 1, false, scan.PhaseEnded,
					scan.Counts{Captured: rows, Produced: rows}, rows, false, ""))
				settle(t, m, q, tt)
				m.ShowLive()
				showRecordItems(m)
				if m.narrowResults() {
					m.openResultDetail()
				}
				settle(t, m, q, tt)
				if m.activeSlot != resultSlotSession || m.rec == nil || m.rec.ID != "s1" {
					t.Fatalf("the live session is not active: slot %v record %+v", m.activeSlot, m.rec)
				}

				if err := tt.Click("确认"); err != nil {
					t.Fatalf("click 确认: %v", err)
				}
				settle(t, m, q, tt)
				if m.pendingCount() != rows-1 {
					t.Fatalf("confirm-stay left %d pending rows, want %d", m.pendingCount(), rows-1)
				}

				if err := tt.Click("设置"); err != nil {
					t.Fatalf("click 设置: %v", err)
				}
				settle(t, m, q, tt)
				if err := tt.Click("记录"); err != nil {
					t.Fatalf("click 记录: %v", err)
				}
				settle(t, m, q, tt)

				visible := m.visibleResultRows()
				want := -1
				if len(visible) > 0 {
					want = visible[0]
				}
				if m.displayPage() != pageResults || m.activeSlot != resultSlotSession {
					t.Fatalf("returning through settings = page %v slot %v",
						m.displayPage(), m.activeSlot)
				}
				if m.selected != want {
					t.Fatalf("the detail keeps a row outside the filter: selected %d, want %d (visible %v)",
						m.selected, want, visible)
				}
				if m.selected >= 0 && m.selectedSeq() != m.rec.Rows[m.selected].Seq {
					t.Fatalf("the selection sequence %d does not match row %d",
						m.selectedSeq(), m.selected)
				}
				if m.scroll != 0 {
					t.Fatalf("the fallback scroll is %d, want the first visible row", m.scroll)
				}
				switch rows {
				case 2:
					if len(visible) != 1 || visible[0] != 1 || m.selectedSeq() != 2 {
						t.Fatalf("the revisit shows %v selected %d, want only row 2",
							visible, m.selected)
					}
				case 1:
					if len(visible) != 0 || m.selected != -1 {
						t.Fatalf("the completed pending segment shows %v selected %d",
							visible, m.selected)
					}
				}
			})
		}
	}
}
