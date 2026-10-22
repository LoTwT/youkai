package ui

import (
	"image"
	"image/color"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/record"
	"github.com/LoTwT/youkai/internal/scan"
	"github.com/egoist/mygo/ui"
)

// TestPageLayoutSidebarWidths guards the design's two rail widths: 168
// DIP on wide windows and 144 DIP from 1024 down to 860.
func TestPageLayoutSidebarWidths(t *testing.T) {
	for _, tc := range []struct {
		width int
		want  float32
	}{
		{1440, sidebarWideWidth},
		{1024, sidebarNarrowWidth},
		{1020, sidebarNarrowWidth},
		{860, sidebarNarrowWidth},
	} {
		m, _ := newTestModel(t, Config{Service: &fakeService{}, PageLayout: true})
		m.SetContentSize(tc.width, 700)
		if got := m.sidebarWidth(); got != tc.want {
			t.Errorf("sidebar width at %d = %v, want %v", tc.width, got, tc.want)
		}
	}
}

// TestPageLayoutSidebarItemsFillTheRail checks the rectangular items and
// the bottom-anchored settings entry at both rail widths.
func TestPageLayoutSidebarItemsFillTheRail(t *testing.T) {
	for _, tc := range []struct {
		width int
		rail  float32
	}{
		{1440, sidebarWideWidth},
		{1020, sidebarNarrowWidth},
		{860, sidebarNarrowWidth},
	} {
		m, q := newTestModel(t, Config{Service: &fakeService{}, PageLayout: true})
		m.SetContentSize(tc.width, 700)
		height := 700
		if tc.width == 860 {
			height = 560
		}
		tt := ui.NewTester(m.View, tc.width, height)
		settle(t, m, q, tt)

		for _, label := range []string{"扫描", "历史", "设置"} {
			rect, ok := tt.Find(label)
			if !ok {
				t.Fatalf("at %d: the %q item is missing", tc.width, label)
			}
			if rect.X < 0 || rect.X+rect.W > tc.rail {
				t.Errorf("at %d: %q at %+v does not fit the %v-DIP rail", tc.width, label, rect, tc.rail)
			}
			if rect.H != 38 {
				t.Errorf("at %d: %q is %v high, want the 38-DIP rectangular item", tc.width, label, rect.H)
			}
		}
		scan, _ := tt.Find("扫描")
		settings, _ := tt.Find("设置")
		if settings.Y <= scan.Y {
			t.Errorf("at %d: settings at %v is not below scan at %v", tc.width, settings.Y, scan.Y)
		}
		if settings.Y+settings.H < float32(height)*0.8 {
			t.Errorf("at %d: settings at %+v is not pinned to the bottom", tc.width, settings)
		}
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

// TestPageLayoutSidebarActiveState checks that the current destination has
// the design's accent-soft face and the other items do not.
func TestPageLayoutSidebarActiveState(t *testing.T) {
	m, q := newTestModel(t, Config{Service: &fakeService{}, PageLayout: true})
	tt := ui.NewTester(m.View, 1020, 700)
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
	if !faceIsActive("扫描") || faceIsActive("历史") || faceIsActive("设置") {
		t.Fatal("the scan destination alone is not active at startup")
	}

	if err := tt.Click("历史"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if m.displayPage() != pageHistory {
		t.Fatalf("the page is %v after clicking history, want history", m.displayPage())
	}
	if faceIsActive("扫描") || !faceIsActive("历史") || faceIsActive("设置") {
		t.Fatal("the history destination alone is not active")
	}

	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if m.displayPage() != pageSettings {
		t.Fatalf("the page is %v after clicking settings, want settings", m.displayPage())
	}
	if faceIsActive("扫描") || faceIsActive("历史") || !faceIsActive("设置") {
		t.Fatal("the settings destination alone is not active")
	}
}

// TestPageLayoutPagesRouteWithoutStageSwitch checks the shell's page
// model: there is no in-page stage switch, a history snapshot returns to
// the list, and the scan destination is prepare without a live session
// while the history destination keeps the loaded record.
func TestPageLayoutPagesRouteWithoutStageSwitch(t *testing.T) {
	svc := &fakeService{
		list: []record.RecordInfo{{
			ID: "r1", LatestRevision: 1,
			SavedAt: time.Date(2026, 10, 17, 12, 30, 0, 0, time.UTC),
		}},
		loads: map[string]*record.Recovery{"r1": testRecord()},
	}
	m, q := newTestModel(t, Config{Service: svc, PageLayout: true})
	tt := ui.NewTester(m.View, 1020, 700)
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
	if err := tt.Click("历史"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("搜索日期") {
		t.Fatalf("the history page does not show its records: %q", tt.Texts())
	}
	if err := tt.Click("打开"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if m.displayPage() != pageResults {
		t.Fatalf("opening a record shows page %v, want results", m.displayPage())
	}
	if !tt.HasText("← 返回历史") {
		t.Fatalf("the history snapshot header does not offer its way back: %q", tt.Texts())
	}
	if tt.HasText("+ 准备新扫描") {
		t.Fatal("the history snapshot header offers the scan destination's action")
	}
	if err := tt.Click("← 返回历史"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if m.displayPage() != pageHistory {
		t.Fatalf("the history snapshot's back action shows page %v, want the list", m.displayPage())
	}
	if err := tt.Click("扫描"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if m.displayPage() != pageScan || !tt.HasText(scanHint) {
		t.Fatalf("scan without a live session shows page %v, want prepare", m.displayPage())
	}
	if m.rec == nil {
		t.Fatal("preparing a new scan discarded the loaded record")
	}
	if err := tt.Click("历史"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if m.displayPage() != pageResults || m.rec == nil || m.rec.ID != "r1" {
		t.Fatalf("history after prepare = page %v record %+v, want the opened record",
			m.displayPage(), m.rec)
	}
}

// TestPageLayoutScanActions checks the scan page's bottom action bar: it
// starts while idle, stops while a session runs, and the results header
// offers the return-to-progress action instead of a new scan.
func TestPageLayoutScanActions(t *testing.T) {
	svc := &fakeService{}
	m, q := newTestModel(t, Config{Service: svc, PageLayout: true})
	tt := ui.NewTester(m.View, 1020, 700)
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

	// The results page opened during the scan returns to the progress
	// page rather than starting another scan.
	m.page = pageResults
	tt.Frame()
	if !tt.HasText("返回扫描中") {
		t.Fatalf("the results header during a scan does not offer the return action: %q", tt.Texts())
	}
	if err := tt.Click("返回扫描中"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if m.displayPage() != pageScan {
		t.Fatalf("returning to the running scan shows page %v, want scan", m.displayPage())
	}

	// A finished session lands on the results page with the new-scan
	// action; that action only returns to prepare.
	ended := snap("s1", 2, false, scan.PhaseEnded,
		scan.Counts{Captured: 2, Produced: 1}, 1, false, "")
	applyAndSettle(t, m, q, tt, ended)
	if m.displayPage() != pageResults {
		t.Fatalf("the ended scan shows page %v, want results", m.displayPage())
	}
	if !tt.HasText("+ 准备新扫描") {
		t.Fatalf("the results header does not offer a new scan: %q", tt.Texts())
	}
	if err := tt.Click("+ 准备新扫描"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if m.displayPage() != pageScan || !tt.HasText(scanHint) {
		t.Fatalf("the new-scan action shows page %v, want prepare", m.displayPage())
	}
}

// TestSidebarRestoresSessionAndOpenedHistory checks the rail as a set of
// destinations: history from settings returns to the opened snapshot,
// the history list is reachable from results, and scan restores the live
// session slot without loading it again. Clicking the active destination
// is a no-op.
func TestSidebarRestoresSessionAndOpenedHistory(t *testing.T) {
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
	m, q := newTestModel(t, Config{Service: svc, PageLayout: true})
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
	if err := tt.Click("历史"); err != nil {
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
	if err := tt.Click("历史"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if m.displayPage() != pageResults || m.activeSlot != resultSlotHistory {
		t.Fatalf("history from results = page %v slot %v, want the opened record",
			m.displayPage(), m.activeSlot)
	}
	if err := tt.Click("← 返回历史"); err != nil {
		t.Fatalf("click 返回历史: %v", err)
	}
	tt.Frame()
	if m.displayPage() != pageHistory {
		t.Fatalf("returning to history shows page %v, want the list", m.displayPage())
	}
	if err := tt.Click("历史"); err != nil { // active destination: no-op
		t.Fatal(err)
	}
	tt.Frame()
	if m.displayPage() != pageHistory {
		t.Fatalf("clicking the active history destination moved to page %v", m.displayPage())
	}

	if err := tt.Click("扫描"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if m.displayPage() != pageResults || m.activeSlot != resultSlotSession ||
		m.rec == nil || m.rec.ID != "s1" || !m.rec.Live {
		t.Fatalf("scan from history = page %v slot %v record %+v",
			m.displayPage(), m.activeSlot, m.rec)
	}
	if m.filter != resultFilterAll || m.search != "未评价" || m.selected != 1 {
		t.Fatalf("the session slot lost its view: filter %v search %q selected %d",
			m.filter, m.search, m.selected)
	}
	if slotLoadCount(svc, "s1") != sessionLoads {
		t.Fatal("returning to the live session reloaded it")
	}

	// The active scan destination and the active settings destination
	// ignore their own clicks.
	if err := tt.Click("扫描"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if m.displayPage() != pageResults || m.activeSlot != resultSlotSession {
		t.Fatal("clicking the active scan destination moved away")
	}
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

// TestSidebarHistoryRoundTripRestoresViewState checks the history
// destination's two levels: the rail restores the opened snapshot with its
// filter, search, selection and scroll after a scan or settings visit, and
// only the snapshot's back action opens the list. Neither round trip
// reloads the record.
func TestSidebarHistoryRoundTripRestoresViewState(t *testing.T) {
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
	m, q := newTestModel(t, Config{Service: svc, PageLayout: true})
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)
	m.Apply(snap("s1", 1, true, scan.PhaseCapturing,
		scan.Counts{Captured: 2, Produced: 2}, 1, true, ""))
	drainAll(t, m, q)

	m.histChoice = slotHistoryLabel(t, m, "old")
	m.OpenHistory()
	settle(t, m, q, tt)
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
	if m.displayPage() != pageResults || m.activeSlot != resultSlotSession {
		t.Fatalf("scan from history = page %v slot %v", m.displayPage(), m.activeSlot)
	}
	if err := tt.Click("历史"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	checkRestored("history after scan")

	// History → settings → history.
	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if m.displayPage() != pageSettings {
		t.Fatalf("settings shows page %v", m.displayPage())
	}
	if err := tt.Click("历史"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	checkRestored("history after settings")

	// The snapshot's own back action opens the list; the rail then
	// returns to the opened record rather than the list.
	if err := tt.Click("← 返回历史"); err != nil {
		t.Fatalf("click 返回历史: %v", err)
	}
	tt.Frame()
	if m.displayPage() != pageHistory {
		t.Fatalf("returning to history shows page %v, want the list", m.displayPage())
	}
	if err := tt.Click("扫描"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if m.activeSlot != resultSlotSession {
		t.Fatalf("scan from the history list did not open the live session: slot %v", m.activeSlot)
	}
	if err := tt.Click("历史"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	checkRestored("history from the list")
}

// TestSidebarDestinationsGuardDirtyDrafts checks that switching to the
// scan, history or settings destination never drops an unsaved item: the
// move waits behind the save/discard/stay prompt, and staying keeps the
// draft and the current slot.
func TestSidebarDestinationsGuardDirtyDrafts(t *testing.T) {
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
	m, q := newTestModel(t, Config{Service: svc, PageLayout: true})
	tt := ui.NewTester(m.View, 1024, 700)
	settle(t, m, q, tt)
	m.Apply(snap("s1", 1, true, scan.PhaseCapturing,
		scan.Counts{Captured: 1, Produced: 1}, 1, true, ""))
	drainAll(t, m, q)
	m.histChoice = slotHistoryLabel(t, m, "old")
	m.OpenHistory()
	settle(t, m, q, tt)

	m.startEdit()
	m.edit.draft.Level = "13"
	if err := tt.Click("扫描"); err != nil {
		t.Fatal(err)
	}
	if m.navPrompt == nil || m.navPrompt.kind != navShowLive {
		t.Fatalf("dirty history did not prompt before scan: %+v", m.navPrompt)
	}
	if m.displayPage() != pageResults || m.activeSlot != resultSlotHistory {
		t.Fatal("the dirty history slot switched before the prompt settled")
	}
	m.promptStay()
	if m.edit == nil || m.edit.draft.Level != "13" {
		t.Fatal("staying lost the dirty history draft")
	}

	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	if m.navPrompt == nil || m.navPrompt.kind != navPage || m.navPrompt.page != pageSettings {
		t.Fatalf("dirty history did not prompt before settings: %+v", m.navPrompt)
	}
	if m.displayPage() != pageResults {
		t.Fatal("settings opened before the dirty prompt settled")
	}
	m.promptDiscardAndContinue()
	drainAll(t, m, q)
	if m.displayPage() != pageSettings || m.edit != nil {
		t.Fatalf("discarding did not open settings: page %v edit %v", m.displayPage(), m.edit)
	}

	if err := tt.Click("扫描"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if m.activeSlot != resultSlotSession || m.rec == nil || m.rec.ID != "s1" {
		t.Fatalf("scan from settings did not restore the session: slot %v record %+v",
			m.activeSlot, m.rec)
	}
	m.startEdit()
	m.edit.draft.Level = "14"
	if err := tt.Click("历史"); err != nil {
		t.Fatal(err)
	}
	if m.navPrompt == nil || m.navPrompt.kind != navOpenHistory || m.navPrompt.recordID != "old" {
		t.Fatalf("dirty session did not prompt before the opened history: %+v", m.navPrompt)
	}
	if m.displayPage() != pageResults || m.activeSlot != resultSlotSession {
		t.Fatal("the dirty session switched before the prompt settled")
	}
	m.promptStay()
	if m.edit == nil || m.edit.draft.Level != "14" {
		t.Fatal("staying lost the dirty session draft")
	}
}
