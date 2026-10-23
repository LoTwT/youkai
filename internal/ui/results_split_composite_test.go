package ui

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// resultHeaderRects returns the fixed results header's box and its five
// column title positions, in column order.
func resultHeaderRects(t *testing.T, tt *ui.Tester) (ui.Rect, []float32) {
	t.Helper()
	header, ok := tt.Find("序号")
	if !ok {
		t.Fatalf("the results header is missing: %q", tt.Texts())
	}
	var columns []float32
	for _, title := range []string{"序号", "套装", "槽位·等级", "主词条", "状态"} {
		rect, ok := tt.Find(title)
		if !ok {
			t.Fatalf("the %q header is missing: %q", title, tt.Texts())
		}
		columns = append(columns, rect.X)
	}
	return header, columns
}

// checkFixedResultHeader verifies that the header and its columns stayed at
// their recorded place while the list scrolled.
func checkFixedResultHeader(t *testing.T, tt *ui.Tester, stage string, header ui.Rect, columns []float32) {
	t.Helper()
	after, ok := tt.Find("序号")
	if !ok || after.Y != header.Y {
		t.Fatalf("%s: the header moved: %+v -> %+v", stage, header, after)
	}
	for i, title := range []string{"序号", "套装", "槽位·等级", "主词条", "状态"} {
		rect, ok := tt.Find(title)
		if !ok || rect.X != columns[i] {
			t.Fatalf("%s: the %q column moved: %v -> %+v", stage, title, columns[i], rect)
		}
	}
}

// checkWholeResultRows verifies a snapped composite view: the tracked
// offset and the list's first row match, every row the viewport shows is a
// whole 32-DIP row in consecutive order, and no clipped row peeks in past
// the last one.
func checkWholeResultRows(t *testing.T, m *Model, tt *ui.Tester, visible []int, stage string, wantOffset float32, wantFirst int) {
	t.Helper()
	if got := m.resultsScroll.Y; got != wantOffset {
		t.Fatalf("%s: scroll offset = %v, want %v", stage, got, wantOffset)
	}
	first, last := m.list.Visible()
	if first != wantFirst || last < first {
		t.Fatalf("%s: visible rows = %d..%d, want the first %d", stage, first, last, wantFirst)
	}
	var top float32
	for row := first; row <= last; row++ {
		if row < 0 || row >= len(visible) {
			t.Fatalf("%s: visible row %d exceeds %d filtered rows", stage, row, len(visible))
		}
		rect, ok := tt.Find(m.rec.Rows[visible[row]].Label())
		if !ok {
			t.Fatalf("%s: row %d is not rendered", stage, row)
		}
		if rect.H != resultsRowHeight {
			t.Fatalf("%s: row %d height = %.3f, want %d", stage, row, rect.H, resultsRowHeight)
		}
		if row == first {
			top = rect.Y
		} else if math.Abs(float64(rect.Y-(top+float32(row-first)*resultsRowHeight))) > 0.01 {
			t.Fatalf("%s: row %d starts at %.2f, want %.2f",
				stage, row, rect.Y, top+float32(row-first)*resultsRowHeight)
		}
	}
	if last+1 < len(visible) {
		if rect, ok := tt.Find(m.rec.Rows[visible[last+1]].Label()); ok && rect.H > 0 {
			t.Fatalf("%s: row %d below the viewport is clipped into view: %+v",
				stage, last+1, rect)
		}
	}
}

// TestSplitResultsCompositeSnapsPreciseScrollToRows checks the 1024 and
// 1440 split tables: the same fixed header and 32-DIP composite as the
// narrow list snaps a precise 17-DIP or 26-DIP move to whole rows, keeps
// small moves accumulating, and reaches the top and bottom on boundaries.
func TestSplitResultsCompositeSnapsPreciseScrollToRows(t *testing.T) {
	for _, tc := range []struct {
		width  int
		height int
	}{
		{width: 1024, height: 700},
		{width: 1440, height: 900},
	} {
		t.Run(fmt.Sprintf("width-%d", tc.width), func(t *testing.T) {
			m, _, tt := openResponsiveResults(t, manyPendingRecord(t, 200), tc.width, tc.height)
			if m.narrowResults() || !resultsSideBySide(m.contentWidth) {
				t.Fatalf("%d is not the side-by-side results split", tc.width)
			}
			visible := m.visibleResultRows()
			header, columns := resultHeaderRects(t, tt)
			rest := func() {
				tt.Advance(resultsSnapDelay + 40*time.Millisecond)
				tt.Frame()
			}

			checkWholeResultRows(t, m, tt, visible, "top", 0, 0)
			checkFixedResultHeader(t, tt, "top", header, columns)

			// A precise 17-DIP move stays where the input put it until the
			// motion stops, then aligns with the next whole row. The split
			// must not fall back to the native table's half-row offset.
			tt.Scroll(300, 300, 0, 17)
			tt.Frame()
			if got := m.resultsScroll.Y; got != 17 {
				t.Fatalf("the 17-DIP move was snapped mid-input: offset %v", got)
			}
			rest()
			checkWholeResultRows(t, m, tt, visible, "after 17 DIP", resultsRowHeight, 1)
			checkFixedResultHeader(t, tt, "after 17 DIP", header, columns)

			// 26 DIPs continue from the snapped row instead of accumulating
			// a half-row offset.
			tt.Scroll(300, 300, 0, 26)
			rest()
			checkWholeResultRows(t, m, tt, visible, "after 26 DIP", 2*resultsRowHeight, 2)
			checkFixedResultHeader(t, tt, "after 26 DIP", header, columns)

			// Moves below half a row accumulate before the rest snap.
			tt.Scroll(300, 300, 0, 10)
			tt.Frame()
			if got := m.resultsScroll.Y; got != 2*resultsRowHeight+10 {
				t.Fatalf("the small move did not accumulate: offset %v", got)
			}
			tt.Scroll(300, 300, 0, 10)
			tt.Frame()
			rest()
			checkWholeResultRows(t, m, tt, visible, "after two small moves", 3*resultsRowHeight, 3)

			// The full travel reaches the end on a row boundary.
			tt.Scroll(300, 300, 0, 100000)
			rest()
			first, last := m.list.Visible()
			if got := m.resultsScroll.Y; math.Mod(float64(got), resultsRowHeight) != 0 {
				t.Fatalf("the bottom offset %v is not on a row boundary", got)
			}
			if first != int(math.Round(float64(m.resultsScroll.Y)/resultsRowHeight)) || last != len(visible)-1 {
				t.Fatalf("the end shows rows %d..%d at offset %v", first, last, m.resultsScroll.Y)
			}
			checkWholeResultRows(t, m, tt, visible, "bottom", float32(first)*resultsRowHeight, first)
			checkFixedResultHeader(t, tt, "bottom", header, columns)

			// Scrolling back up restores the top without a partial row.
			tt.Scroll(300, 300, 0, -100000)
			rest()
			checkWholeResultRows(t, m, tt, visible, "back at top", 0, 0)
			checkFixedResultHeader(t, tt, "back at top", header, columns)
			if !tt.HasText("◐") {
				t.Fatalf("the pending status badge is missing: %q", tt.Texts())
			}
		})
	}
}

// TestSplitResultsChooseRowsWithoutDetailPage checks that a click or the
// keyboard in the 1024/1440 split only chooses the row and updates the
// visible detail: it must not open the narrow detail page, and Return must
// not navigate there either.
func TestSplitResultsChooseRowsWithoutDetailPage(t *testing.T) {
	for _, tc := range []struct {
		width  int
		height int
	}{
		{width: 1024, height: 700},
		{width: 1440, height: 900},
	} {
		t.Run(fmt.Sprintf("width-%d", tc.width), func(t *testing.T) {
			m, _, tt := openResponsiveResults(t, manyPendingRecord(t, 40), tc.width, tc.height)
			visible := m.visibleResultRows()
			if len(visible) < 10 {
				t.Fatalf("the split table shows only %d rows", len(visible))
			}

			chosenDisplay := 3
			chosen := visible[chosenDisplay]
			rect, ok := tt.Find(m.rec.Rows[chosen].Label())
			if !ok {
				t.Fatalf("row %d is not rendered", chosenDisplay)
			}
			tt.ClickAt(rect.X+rect.W/2, rect.Y+rect.H/2)
			tt.Frame()
			if m.resultsDetail {
				t.Fatal("clicking a split row opened the narrow detail page")
			}
			if m.selected != chosen || m.tableSelected != chosenDisplay {
				t.Fatalf("the click chose row %d (table %d), want %d (table %d)",
					m.selected, m.tableSelected, chosen, chosenDisplay)
			}
			if !tt.HasText(resultDetailTitle(&m.rec.Rows[chosen])) {
				t.Fatalf("the split detail does not show the clicked row: %q", tt.Texts())
			}
			if tt.HasText("← 结果列表") {
				t.Fatal("the split shows the narrow detail's back action")
			}

			// Escape returns the keyboard to the table; Down chooses the
			// next row and the side-by-side detail follows it.
			if err := tt.Click("全部 40"); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			tt.Key(0, ui.KeyEscape)
			tt.Frame()
			tt.Frame()
			tt.Key(0, ui.KeyDown)
			tt.Frame()
			if m.resultsDetail {
				t.Fatal("the keyboard opened the narrow detail page")
			}
			want := visible[chosenDisplay+1]
			if m.selected != want || m.tableSelected != chosenDisplay+1 {
				t.Fatalf("Down chose row %d (table %d), want %d (table %d)",
					m.selected, m.tableSelected, want, chosenDisplay+1)
			}
			if !tt.HasText(resultDetailTitle(&m.rec.Rows[want])) {
				t.Fatalf("the split detail does not follow the keyboard: %q", tt.Texts())
			}
			tt.Key(0, ui.KeyEnter)
			tt.Frame()
			if m.resultsDetail {
				t.Fatal("Return in the split opened the narrow detail page")
			}
			if m.displayPage() != pageResults {
				t.Fatalf("choosing a split row left the results page: %v", m.displayPage())
			}
		})
	}
}

// TestSplitResultsRestoreFiltersSelectionAndScroll checks the split table's
// visit state: segment choices, search, the chosen row and the snapped
// offset survive leaving the results page. Returning while at the end must
// keep a whole-row offset too, not clamp it to a transient larger viewport.
func TestSplitResultsRestoreFiltersSelectionAndScroll(t *testing.T) {
	for _, tc := range []struct {
		width  int
		height int
	}{
		{width: 1024, height: 700},
		{width: 1440, height: 900},
	} {
		t.Run(fmt.Sprintf("width-%d", tc.width), func(t *testing.T) {
			m, _, tt := openResponsiveResults(t, manyPendingRecord(t, 200), tc.width, tc.height)
			m.filter = resultFilterAll
			m.slotFilter = "2"
			m.setFilter = "31100"
			m.search = "2 号位"
			m.settleResultSelection()
			tt.Frame()
			visible := m.visibleResultRows()
			if len(visible) < 20 {
				t.Fatalf("the filters leave %d rows, want enough to scroll", len(visible))
			}
			header, columns := resultHeaderRects(t, tt)
			rest := func() {
				tt.Advance(resultsSnapDelay + 40*time.Millisecond)
				tt.Frame()
			}
			tt.Scroll(300, 300, 0, 17)
			rest()
			checkWholeResultRows(t, m, tt, visible, "after 17 DIP", resultsRowHeight, 1)
			chosenDisplay := 3
			chosen := visible[chosenDisplay]
			rect, ok := tt.Find(m.rec.Rows[chosen].Label())
			if !ok {
				t.Fatalf("row %d is not rendered", chosenDisplay)
			}
			tt.ClickAt(rect.X+rect.W/2, rect.Y+rect.H/2)
			tt.Frame()
			if m.selected != chosen || m.tableSelected != chosenDisplay {
				t.Fatalf("the click chose row %d, want %d", m.selected, chosen)
			}

			if err := tt.Click("设置"); err != nil {
				t.Fatalf("click 设置: %v", err)
			}
			tt.Frame()
			if m.displayPage() != pageSettings {
				t.Fatalf("settings did not open: %v", m.displayPage())
			}
			if err := tt.Click("历史"); err != nil {
				t.Fatalf("click 历史: %v", err)
			}
			tt.Frame()
			tt.Frame()
			if m.displayPage() != pageResults || m.activeSlot != resultSlotHistory {
				t.Fatalf("returning = page %v slot %v", m.displayPage(), m.activeSlot)
			}
			if m.filter != resultFilterAll || m.slotFilter != "2" || m.setFilter != "31100" || m.search != "2 号位" {
				t.Fatalf("the return lost the filters: segment %v slot %q set %q search %q",
					m.filter, m.slotFilter, m.setFilter, m.search)
			}
			if m.selected != chosen || m.tableSelected != chosenDisplay {
				t.Fatalf("the return lost the selection: row %d (table %d), want %d (table %d)",
					m.selected, m.tableSelected, chosen, chosenDisplay)
			}
			checkWholeResultRows(t, m, tt, visible, "returned", resultsRowHeight, 1)
			checkFixedResultHeader(t, tt, "returned", header, columns)

			// Leaving at the end used to let a transient taller viewport
			// clamp the offset to a fractional row. The return must keep
			// the same whole-row end.
			tt.Scroll(300, 300, 0, 100000)
			rest()
			first, last := m.list.Visible()
			bottom := m.resultsScroll.Y
			if math.Mod(float64(bottom), resultsRowHeight) != 0 || last != len(visible)-1 {
				t.Fatalf("the end is not a whole row: offset %v rows %d..%d", bottom, first, last)
			}
			if err := tt.Click("设置"); err != nil {
				t.Fatalf("click 设置: %v", err)
			}
			tt.Frame()
			if err := tt.Click("历史"); err != nil {
				t.Fatalf("click 历史: %v", err)
			}
			tt.Frame()
			tt.Frame()
			afterFirst, afterLast := m.list.Visible()
			if got := m.resultsScroll.Y; got != bottom {
				t.Fatalf("the end moved on return: offset %v -> %v", bottom, got)
			}
			if afterFirst != first || afterLast != last {
				t.Fatalf("the end rows moved on return: %d..%d -> %d..%d", first, last, afterFirst, afterLast)
			}
			checkWholeResultRows(t, m, tt, visible, "returned at end", bottom, first)
			checkFixedResultHeader(t, tt, "returned at end", header, columns)
		})
	}
}

// TestResultsCompositeResizeAcrossSplitKeepsWholeRows resizes a filtered
// results list through 1020, 1024 and 1440: every layout stays on whole
// 32-DIP rows, keeps the selection and filters, and the 1020 list still
// opens its detail page from a row click.
func TestResultsCompositeResizeAcrossSplitKeepsWholeRows(t *testing.T) {
	m, _, tt := openResponsiveResults(t, manyPendingRecord(t, 200), 1020, 700)
	m.filter = resultFilterAll
	m.slotFilter = "2"
	m.setFilter = "31100"
	m.search = "2 号位"
	m.settleResultSelection()
	tt.Frame()
	visible := m.visibleResultRows()
	if len(visible) < 20 {
		t.Fatalf("the filters leave %d rows, want enough to scroll", len(visible))
	}
	rest := func() {
		tt.Advance(resultsSnapDelay + 40*time.Millisecond)
		tt.Frame()
	}
	tt.Scroll(400, 300, 0, 17)
	rest()
	checkWholeResultRows(t, m, tt, visible, "1020", resultsRowHeight, 1)
	chosenDisplay := 3
	chosen := visible[chosenDisplay]
	rect, ok := tt.Find(m.rec.Rows[chosen].Label())
	if !ok {
		t.Fatalf("row %d is not rendered", chosenDisplay)
	}
	tt.ClickAt(rect.X+rect.W/2, rect.Y+rect.H/2)
	tt.Frame()
	if m.selected != chosen || !m.resultsDetail {
		t.Fatalf("the 1020 click chose row %d (detail %v), want %d on its detail page",
			m.selected, m.resultsDetail, chosen)
	}
	if err := tt.Click("← 结果列表"); err != nil {
		t.Fatalf("click 结果列表: %v", err)
	}
	tt.Frame()
	if m.resultsDetail {
		t.Fatal("the 1020 list did not return from its detail page")
	}

	stages := []struct {
		name          string
		width, height int
		split         float32
	}{
		{name: "1024", width: 1024, height: 700, split: 420},
		{name: "1440", width: 1440, height: 900, split: 560},
		{name: "back-1020", width: 1020, height: 700, split: 420},
	}
	for _, stage := range stages {
		m.SetContentSize(stage.width, stage.height)
		tt.SetSize(stage.width, stage.height)
		tt.Frame()
		tt.Frame()
		resultHeaderRects(t, tt)
		if m.split != stage.split {
			t.Fatalf("%s: the table pane is %v DIPs, want %v", stage.name, m.split, stage.split)
		}
		if got := m.resultsScroll.Y; math.Mod(float64(got), resultsRowHeight) != 0 {
			t.Fatalf("%s: the resize left the stale offset %v", stage.name, got)
		}
		first, _ := m.list.Visible()
		checkWholeResultRows(t, m, tt, visible, stage.name, float32(first)*resultsRowHeight, first)
		if m.selected != chosen || m.tableSelected != resultDisplayIndex(visible, chosen) {
			t.Fatalf("%s: the resize moved the selection: row %d (table %d), want %d",
				stage.name, m.selected, m.tableSelected, chosen)
		}
		if m.resultsDetail {
			t.Fatalf("%s: the resize opened the narrow detail page", stage.name)
		}
		if m.filter != resultFilterAll || m.slotFilter != "2" || m.setFilter != "31100" || m.search != "2 号位" {
			t.Fatalf("%s: the resize lost the filters", stage.name)
		}
	}

	// Back at 1020 the list page still opens the detail from a row click.
	rect, ok = tt.Find(m.rec.Rows[chosen].Label())
	if !ok {
		t.Fatalf("row %d is not rendered at 1020", chosenDisplay)
	}
	tt.ClickAt(rect.X+rect.W/2, rect.Y+rect.H/2)
	tt.Frame()
	if !m.resultsDetail {
		t.Fatal("the 1020 list no longer opens its detail page")
	}
}
