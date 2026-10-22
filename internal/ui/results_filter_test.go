package ui

import (
	"fmt"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/game/zzz"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/LoTwT/youkai/internal/scan"
	"github.com/egoist/mygo/ui"
)

// responsiveResultRecord builds a saved record with two pending items and
// one failed and one unprocessed item, so every segment has a distinct
// population. The two pending items differ by set and slot.
func responsiveResultRecord(t *testing.T) *record.Recovery {
	t.Helper()
	first := zzz.DiscRecord{
		SetID: intp(31100), Slot: 1, Level: intp(15),
		MainStat: zzz.Stat{Attribute: zzz.AttrHealth, Value: "2200", Unit: zzz.UnitFlat},
		Substats: []zzz.Substat{{
			Stat:             zzz.Stat{Attribute: zzz.AttrCritDamage, Value: "19.2", Unit: zzz.UnitPercent},
			EnhancementRolls: intp(3),
		}},
	}
	second := first
	second.SetID = intp(31200)
	second.Slot = 2
	return recovery(
		structuredItem(1, first, "待一"),
		structuredItem(2, second, "待二"),
		item(3, record.StatusFailed, "", "识别超时", "失败"),
		item(4, record.StatusUnprocessed, record.ReasonCancelled, "", "未处理"),
	)
}

func manyPendingRecord(t *testing.T, n int) *record.Recovery {
	t.Helper()
	items := make([]record.ItemRecord, 0, n)
	for seq := 1; seq <= n; seq++ {
		disc := zzz.DiscRecord{
			SetID: intp(31100), Slot: (seq-1)%6 + 1, Level: intp(15),
			MainStat: zzz.Stat{Attribute: zzz.AttrHealth, Value: "2200", Unit: zzz.UnitFlat},
			Substats: []zzz.Substat{{
				Stat:             zzz.Stat{Attribute: zzz.AttrCritDamage, Value: "19.2", Unit: zzz.UnitPercent},
				EnhancementRolls: intp(3),
			}},
		}
		items = append(items, structuredItem(int64(seq), disc, fmt.Sprintf("证据 %d", seq)))
	}
	return recovery(items...)
}

// openResponsiveResults opens one saved record on the four-page results
// destination at the requested window size.
func openResponsiveResults(t *testing.T, rec *record.Recovery, width, height int) (*Model, *queueUpdater, *ui.Tester) {
	t.Helper()
	svc := &fakeService{
		list: []record.RecordInfo{{
			ID: "rec", LatestRevision: 1,
			SavedAt: time.Date(2026, 10, 21, 14, 2, 0, 0, time.UTC),
		}},
		loads: map[string]*record.Recovery{"rec": rec},
	}
	m, q := newTestModel(t, Config{Service: svc, PageLayout: true})
	m.SetContentSize(width, height)
	tt := ui.NewTester(m.View, width, height)
	settle(t, m, q, tt)
	m.histChoice = slotHistoryLabel(t, m, "rec")
	m.OpenHistory()
	settle(t, m, q, tt)
	return m, q, tt
}

// TestResultsResponsiveListDetail checks the confirmed breakpoint: 1024
// and wider show the table beside the detail, while 1020 and 860 are whole
// list and detail pages. Returning from the detail restores the filter,
// selection and list place.
func TestResultsResponsiveListDetail(t *testing.T) {
	for _, tc := range []struct {
		width  int
		height int
	}{
		{width: 1020, height: 700},
		{width: 860, height: 560},
	} {
		t.Run(fmt.Sprintf("width-%d", tc.width), func(t *testing.T) {
			m, _, tt := openResponsiveResults(t, responsiveResultRecord(t), tc.width, tc.height)
			if m.filter != resultFilterPending {
				t.Fatalf("the results page starts on filter %v, want pending", m.filter)
			}
			if tt.HasText("← 结果列表") {
				t.Fatal("the list page shows the detail's back action")
			}
			if !tt.HasText("全部 4") || !tt.HasText("待校对 2") || !tt.HasText("无法导出 2") {
				t.Fatalf("the filter bar is missing: %q", tt.Texts())
			}
			for _, label := range []string{"全部 4", "无法导出 2", "全部套装", "全部槽位"} {
				if rect, ok := tt.Find(label); ok && rect.X+rect.W > float32(tc.width) {
					t.Fatalf("the %q control at %v overflows the %d window", label, rect, tc.width)
				}
			}
			if tt.HasText("字段（只读）") {
				t.Fatal("the narrow list already shows the detail page")
			}

			if err := tt.Click("河豚电音"); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			if !m.resultsDetail {
				t.Fatal("clicking a row did not open the detail page")
			}
			if !tt.HasText("← 结果列表") {
				t.Fatalf("the detail page has no way back: %q", tt.Texts())
			}
			if tt.HasText("全部 4") {
				t.Fatal("the detail page kept the list filters")
			}
			if !tt.HasText("字段（只读）") {
				t.Fatalf("the detail page does not show the chosen item: %q", tt.Texts())
			}

			if err := tt.Click("← 结果列表"); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			if m.resultsDetail {
				t.Fatal("the back action did not return to the list")
			}
			if m.filter != resultFilterPending || m.selectedSeq() != 1 {
				t.Fatalf("returning lost the filter/selection: filter %v seq %d", m.filter, m.selectedSeq())
			}
			if !tt.HasText("全部 4") {
				t.Fatal("the list page did not return")
			}
		})
	}
}

// TestResultsSplitAt1024 checks that 1024 is the side-by-side layout: the
// table takes its 420-DIP pane and the detail stays visible beside it.
func TestResultsSplitAt1024(t *testing.T) {
	m, _, tt := openResponsiveResults(t, responsiveResultRecord(t), 1024, 700)
	if !resultsSideBySide(m.contentWidth) {
		t.Fatal("1024 did not use the side-by-side results split")
	}
	if m.split != 420 {
		t.Fatalf("the results table is %v DIPs wide, want 420", m.split)
	}
	if tt.HasText("← 结果列表") {
		t.Fatal("the side-by-side layout shows the narrow detail's back action")
	}
	table, ok := tt.Find("主词条")
	if !ok {
		t.Fatalf("the split table header is missing: %q", tt.Texts())
	}
	detail, ok := tt.Find("字段（只读）")
	if !ok {
		t.Fatalf("the detail form is missing: %q", tt.Texts())
	}
	if detail.X <= table.X {
		t.Fatalf("the detail at %v is not right of the table at %v", detail, table)
	}
}

// TestResultsFilterOrderAndSelection checks the confirmed order, the
// pending default, the counts and the rule that a filter keeps a still
// visible selection but falls back to the first visible row otherwise.
func TestResultsFilterOrderAndSelection(t *testing.T) {
	m, _, tt := openResponsiveResults(t, responsiveResultRecord(t), 1024, 700)
	texts := tt.Texts()
	positions := make(map[string]int, 3)
	for i, text := range texts {
		switch text {
		case "全部 4", "待校对 2", "无法导出 2":
			positions[text] = i
		}
	}
	for _, label := range []string{"全部 4", "待校对 2", "无法导出 2"} {
		if _, ok := positions[label]; !ok {
			t.Fatalf("the %q segment is missing: %q", label, texts)
		}
	}
	if !(positions["全部 4"] < positions["待校对 2"] &&
		positions["待校对 2"] < positions["无法导出 2"]) {
		t.Fatalf("the filter order is %v, want all/pending/unexportable", positions)
	}
	if m.filter != resultFilterPending {
		t.Fatalf("the default filter is %v, want pending", m.filter)
	}
	if visible := m.visibleResultRows(); len(visible) != 2 || visible[0] != 0 || visible[1] != 1 {
		t.Fatalf("the pending filter shows rows %v", visible)
	}

	m.selected = 1
	m.selectedSequence = 2
	tt.Frame()
	if err := tt.Click("全部 4"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if m.filter != resultFilterAll || m.selectedSeq() != 2 {
		t.Fatalf("switching to all changed the selection: filter %v seq %d", m.filter, m.selectedSeq())
	}

	if err := tt.Click("无法导出 2"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if m.filter != resultFilterUnexportable {
		t.Fatalf("the filter is %v, want unexportable", m.filter)
	}
	if got := m.selectedSeq(); got != 3 {
		t.Fatalf("an out-of-filter selection stayed at seq %d, want the first unexportable row 3", got)
	}

	if err := tt.Click("待校对 2"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := m.selectedSeq(); got != 1 {
		t.Fatalf("returning to pending chose seq %d, want the first pending row 1", got)
	}
}

// TestResultsSearchSetAndSlotFilters checks the three narrowing controls
// and that clearing them returns the whole segment.
func TestResultsSearchSetAndSlotFilters(t *testing.T) {
	m, _, tt := openResponsiveResults(t, responsiveResultRecord(t), 1024, 700)
	m.search = "2 号位"
	m.afterResultFilterChange()
	tt.Frame()
	if visible := m.visibleResultRows(); len(visible) != 1 || visible[0] != 1 {
		t.Fatalf("searching the second slot shows rows %v, want the second pending row", visible)
	}
	if got := m.selectedSeq(); got != 2 {
		t.Fatalf("the search left the selection at seq %d", got)
	}

	m.search = ""
	m.setFilter = "31200"
	m.afterResultFilterChange()
	tt.Frame()
	if visible := m.visibleResultRows(); len(visible) != 1 || visible[0] != 1 {
		t.Fatalf("the set filter shows rows %v", visible)
	}

	m.setFilter = ""
	m.slotFilter = "1"
	m.afterResultFilterChange()
	tt.Frame()
	if visible := m.visibleResultRows(); len(visible) != 1 || visible[0] != 0 {
		t.Fatalf("the slot filter shows rows %v", visible)
	}

	m.search = ""
	m.setFilter = ""
	m.slotFilter = ""
	m.afterResultFilterChange()
	if visible := m.visibleResultRows(); len(visible) != 2 {
		t.Fatalf("clearing the filters shows %d rows, want the two pending rows", len(visible))
	}
	tt.Frame()

	// The set picker stores the id while showing its human label.
	if err := tt.Click("全部套装"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click(describeSet(31200)); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if m.setFilter != "31200" {
		t.Fatalf("the set picker stored %q, want 31200", m.setFilter)
	}
	if visible := m.visibleResultRows(); len(visible) != 1 || visible[0] != 1 {
		t.Fatalf("the set picker shows rows %v", visible)
	}
}

// TestResultsFilterRestoresPerSlot checks that each result destination
// keeps its own segment and narrowing values when the other is reopened.
func TestResultsFilterRestoresPerSlot(t *testing.T) {
	svc := &fakeService{
		list: []record.RecordInfo{{
			ID: "old", LatestRevision: 1,
			SavedAt: time.Date(2026, 10, 20, 22, 18, 0, 0, time.UTC),
		}},
		loads: map[string]*record.Recovery{"old": responsiveResultRecord(t), "s1": responsiveResultRecord(t)},
	}
	m, q := newTestModel(t, Config{Service: svc, PageLayout: true})
	m.SetContentSize(1024, 700)
	tt := ui.NewTester(m.View, 1024, 700)
	settle(t, m, q, tt)
	m.Apply(snap("s1", 1, false, scan.PhaseEnded, scan.Counts{Captured: 4, Produced: 2}, 4, false, ""))
	settle(t, m, q, tt)

	m.filter = resultFilterAll
	m.search = "2 号位"
	m.setFilter = "31200"
	m.slotFilter = "2"
	m.selected = 1
	m.selectedSequence = 2
	m.histChoice = slotHistoryLabel(t, m, "old")
	m.OpenHistory()
	settle(t, m, q, tt)

	m.filter = resultFilterUnexportable
	m.search = ""
	m.setFilter = ""
	m.slotFilter = ""
	m.selected = 3
	m.selectedSequence = 4
	m.ShowLive()
	settle(t, m, q, tt)
	if m.filter != resultFilterAll || m.search != "2 号位" || m.setFilter != "31200" || m.slotFilter != "2" {
		t.Fatalf("the session filters were not restored: filter %v search %q set %q slot %q",
			m.filter, m.search, m.setFilter, m.slotFilter)
	}
	if m.selectedSeq() != 2 {
		t.Fatalf("the session selection is seq %d, want 2", m.selectedSeq())
	}
}

// TestResultsNarrowScrollRestore checks that a narrow detail return
// restores the list's first visible row as well as the selection.
func TestResultsNarrowScrollRestore(t *testing.T) {
	m, _, tt := openResponsiveResults(t, manyPendingRecord(t, 200), 860, 560)
	m.selected = 120
	m.selectedSequence = 121
	m.list.ScrollTo(50, ui.Start)
	tt.Frame()
	first, last := m.list.Visible()
	if first != 50 || last < first {
		t.Fatalf("the list shows rows %d..%d after scrolling to 50", first, last)
	}

	m.openResultDetail()
	tt.Frame()
	if !m.resultsDetail {
		t.Fatal("the narrow results page did not open the detail")
	}
	m.closeResultDetail()
	tt.Frame()
	if m.resultsDetail {
		t.Fatal("the narrow detail did not close")
	}
	if gotFirst, _ := m.list.Visible(); gotFirst != first {
		t.Fatalf("the returned list starts at row %d, want %d", gotFirst, first)
	}
	if m.selectedSeq() != 121 || m.filter != resultFilterPending {
		t.Fatalf("the return lost the selection/filter: seq %d filter %v", m.selectedSeq(), m.filter)
	}
}

// TestResultsStatusBarShortcuts checks that the status bar names the keys
// registered for the current results layout and drops the old, unwired
// save shortcut.
func TestResultsStatusBarShortcuts(t *testing.T) {
	m, _, tt := openResponsiveResults(t, responsiveResultRecord(t), 860, 560)
	if !tt.HasText("↵ 查看详情") || !tt.HasText("⌘J 下一个待校对") {
		t.Fatalf("the 860 list shortcuts are missing: %q", tt.Texts())
	}
	if tt.HasText("⌘S 保存修改") || tt.HasText("⌃⌥⌘S 停止扫描") {
		t.Fatalf("the results list names an unregistered key: %q", tt.Texts())
	}
	m.openResultDetail()
	tt.Frame()
	if !tt.HasText("⌘↵ 确认并下一项") || !tt.HasText("⌘J 下一个待校对") {
		t.Fatalf("the 860 detail shortcuts are missing: %q", tt.Texts())
	}

	_, _, tt2 := openResponsiveResults(t, responsiveResultRecord(t), 1024, 700)
	if !tt2.HasText("⌘↵ 确认并下一项") || !tt2.HasText("⌘J 下一个待校对") {
		t.Fatalf("the split shortcuts are missing: %q", tt2.Texts())
	}
}

// TestResultsEvidenceSizes checks the evidence display sizes by layout.
func TestResultsEvidenceSizes(t *testing.T) {
	for _, tc := range []struct {
		width  int
		height int
		wantW  float32
		wantH  float32
	}{
		{width: 1440, height: 900, wantW: 340, wantH: 170},
		{width: 1024, height: 700, wantW: 224, wantH: 112},
		{width: 1020, height: 700, wantW: 196, wantH: 98},
		{width: 860, height: 560, wantW: 196, wantH: 98},
	} {
		m := New(Config{Service: &fakeService{}, PageLayout: true})
		m.SetContentSize(tc.width, tc.height)
		if w, h := m.evidenceDisplaySize(); w != tc.wantW || h != tc.wantH {
			t.Errorf("evidence at %d is %v×%v, want %v×%v", tc.width, w, h, tc.wantW, tc.wantH)
		}
	}
}

// TestResultShortcutsMoveSelection checks ⌘J in the rendered page: it
// moves to the next pending row and stays on the detail page.
func TestResultShortcutsMoveSelection(t *testing.T) {
	m, _, tt := openResponsiveResults(t, manyPendingRecord(t, 5), 860, 560)
	m.openResultDetail()
	tt.Frame()
	if m.selectedSeq() != 1 {
		t.Fatalf("the detail starts on seq %d, want 1", m.selectedSeq())
	}
	tt.Key(ui.Cmd, ui.KeyJ)
	tt.Frame()
	if m.selectedSeq() != 2 || !m.resultsDetail {
		t.Fatalf("⌘J chose seq %d (detail %v), want seq 2 on the detail", m.selectedSeq(), m.resultsDetail)
	}
}
