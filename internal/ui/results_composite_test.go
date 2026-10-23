package ui

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// TestResultsCompositeSnapsPreciseScrollToRows checks the narrow results
// composite at 860 and 1020: a touchpad's 17-DIP or 26-DIP move may not stop
// between two 32-DIP rows, the snapped view shows only whole rows, and the
// header stays put while the offset travels to the end and back.
func TestResultsCompositeSnapsPreciseScrollToRows(t *testing.T) {
	for _, tc := range []struct {
		width  int
		height int
	}{
		{width: 860, height: 560},
		{width: 1020, height: 700},
	} {
		t.Run(fmt.Sprintf("width-%d", tc.width), func(t *testing.T) {
			m, _, tt := openResponsiveResults(t, manyPendingRecord(t, 200), tc.width, tc.height)
			if !m.narrowResults() {
				t.Fatalf("%d is not the narrow results layout", tc.width)
			}
			visible := m.visibleResultRows()
			header, ok := tt.Find("序号")
			if !ok {
				t.Fatalf("the table header is missing: %q", tt.Texts())
			}
			columns := []float32{}
			for _, title := range []string{"序号", "套装", "槽位·等级", "主词条", "状态"} {
				rect, ok := tt.Find(title)
				if !ok {
					t.Fatalf("the %q header is missing: %q", title, tt.Texts())
				}
				columns = append(columns, rect.X)
			}
			rest := func() {
				tt.Advance(resultsSnapDelay + 40*time.Millisecond)
				tt.Frame()
			}
			checkWholeRows := func(stage string, wantOffset float32, wantFirst int) {
				t.Helper()
				if got := m.resultsScroll.Y; got != wantOffset {
					t.Fatalf("%s: scroll offset = %v, want %v", stage, got, wantOffset)
				}
				first, last := m.list.Visible()
				if first != wantFirst || last < first {
					t.Fatalf("%s: visible rows = %d..%d, want the first %d", stage, first, last, wantFirst)
				}
				if count := last - first + 1; count < 10 {
					t.Fatalf("%s: only %d whole rows are visible", stage, count)
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
				headerAfter, ok := tt.Find("序号")
				if !ok || headerAfter.Y != header.Y {
					t.Fatalf("%s: the header moved: %+v -> %+v", stage, header, headerAfter)
				}
				for i, title := range []string{"序号", "套装", "槽位·等级", "主词条", "状态"} {
					rect, ok := tt.Find(title)
					if !ok || rect.X != columns[i] {
						t.Fatalf("%s: the %q column moved: %v -> %+v", stage, title, columns[i], rect)
					}
				}
				if !tt.HasText("◐") {
					t.Fatalf("%s: the pending status badge is missing: %q", stage, tt.Texts())
				}
			}

			checkWholeRows("top", 0, 0)

			// A precise 17-DIP move stays where the input put it until the
			// motion stops, then aligns with the next whole row.
			tt.Scroll(400, 300, 0, 17)
			tt.Frame()
			if got := m.resultsScroll.Y; got != 17 {
				t.Fatalf("the 17-DIP move was snapped mid-input: offset %v", got)
			}
			rest()
			checkWholeRows("after 17 DIP", resultsRowHeight, 1)

			// A 26-DIP move continues from the snapped row instead of
			// accumulating a half-row offset.
			tt.Scroll(400, 300, 0, 26)
			rest()
			checkWholeRows("after 26 DIP", 2*resultsRowHeight, 2)

			// Moves below half a row accumulate before the rest snap, so
			// repeated small events reach the next row.
			tt.Scroll(400, 300, 0, 10)
			tt.Frame()
			if got := m.resultsScroll.Y; got != 2*resultsRowHeight+10 {
				t.Fatalf("the small move did not accumulate: offset %v", got)
			}
			tt.Scroll(400, 300, 0, 10)
			tt.Frame()
			rest()
			checkWholeRows("after two small moves", 3*resultsRowHeight, 3)

			// The full travel still reaches the end, on a row boundary.
			tt.Scroll(400, 300, 0, 100000)
			rest()
			first, last := m.list.Visible()
			if got := m.resultsScroll.Y; math.Mod(float64(got), resultsRowHeight) != 0 {
				t.Fatalf("the bottom offset %v is not on a row boundary", got)
			}
			if first != int(math.Round(float64(m.resultsScroll.Y)/resultsRowHeight)) || last != len(visible)-1 {
				t.Fatalf("the end shows rows %d..%d at offset %v", first, last, m.resultsScroll.Y)
			}
			checkWholeRows("bottom", float32(first)*resultsRowHeight, first)

			// Scrolling back up restores the top without a partial row.
			tt.Scroll(400, 300, 0, -100000)
			rest()
			checkWholeRows("back at top", 0, 0)
		})
	}
}

// TestResultsCompositeWheelKeepsFullTravel checks a mouse wheel's animating
// scroll: the composite may snap only after the glide stops, so a 120-DIP or
// 600-DIP wheel is not cut back to its first row. The header stays put.
func TestResultsCompositeWheelKeepsFullTravel(t *testing.T) {
	for _, delta := range []float32{120, 600} {
		t.Run(fmt.Sprintf("delta%d", int(delta)), func(t *testing.T) {
			m, q, tt := openResponsiveResults(t, manyPendingRecord(t, 200), 860, 560)
			header, _ := tt.Find("序号")
			if m.resultsScroll.Y != 0 {
				t.Fatalf("the table does not start at the top: Y=%v", m.resultsScroll.Y)
			}
			tt.Wheel(450, 300, 0, delta)
			tt.Advance(time.Second)
			settle(t, m, q, tt)

			got := m.resultsScroll.Y
			if math.Abs(float64(got-delta)) > resultsRowHeight/2 {
				t.Fatalf("wheel travel truncated beyond nearest-row quantization: requested=%v actual=%v",
					delta, got)
			}
			if math.Mod(float64(got), resultsRowHeight) != 0 {
				t.Fatalf("the wheel stopped between rows: Y=%v", got)
			}
			first, _ := m.list.Visible()
			if first != int(math.Round(float64(got/resultsRowHeight))) {
				t.Fatalf("the list is not at the snapped row: Y=%v first=%d", got, first)
			}
			headerAfter, _ := tt.Find("序号")
			if headerAfter.Y != header.Y {
				t.Fatalf("the header moved: %+v -> %+v", header, headerAfter)
			}
		})
	}
}

// TestResultsCompositeResizeReentersOnWholeRow checks a resize across the
// 1024 split: the native table's place replaces the narrow composite's
// tracked offset, so returning to 860 cannot adopt a stale offset and show
// half rows.
func TestResultsCompositeResizeReentersOnWholeRow(t *testing.T) {
	m, _, tt := openResponsiveResults(t, manyPendingRecord(t, 200), 860, 560)
	m.list.ScrollTo(7, ui.Start)
	tt.Frame()
	if got := m.resultsScroll.Y; got != 7*resultsRowHeight {
		t.Fatalf("the narrow list starts at offset %v, want %d", got, 7*resultsRowHeight)
	}

	m.SetContentSize(1024, 700)
	tt.SetSize(1024, 700)
	tt.Frame()
	tt.Scroll(500, 300, 0, 17)
	tt.Frame()

	m.SetContentSize(860, 560)
	tt.SetSize(860, 560)
	tt.Frame()
	if got := m.resultsScroll.Y; math.Mod(float64(got), resultsRowHeight) != 0 {
		t.Fatalf("returning to 860 entered at the stale offset %v", got)
	}
	first, last := m.list.Visible()
	for row := first; row <= last; row++ {
		rect, ok := tt.Find(m.rec.Rows[m.visibleResultRows()[row]].Label())
		if !ok {
			t.Fatalf("row %d is not rendered after the resize", row)
		}
		if rect.H != resultsRowHeight {
			t.Fatalf("row %d height after the resize = %.3f, want %d", row, rect.H, resultsRowHeight)
		}
	}
}

// TestResultsCompositeClickOpensDetailAndRestoresList checks the narrow
// composite's row action and return: clicking anywhere on a row selects it
// and opens its detail, and going back restores the filter, selection and
// snapped offset.
func TestResultsCompositeClickOpensDetailAndRestoresList(t *testing.T) {
	m, _, tt := openResponsiveResults(t, manyPendingRecord(t, 200), 860, 560)
	m.slotFilter = "2"
	m.settleResultSelection()
	tt.Frame()
	visible := m.visibleResultRows()
	if len(visible) < 20 {
		t.Fatalf("the slot filter leaves %d rows, want enough to scroll", len(visible))
	}
	m.list.ScrollTo(5, ui.Start)
	tt.Frame()
	first, _ := m.list.Visible()
	if first != 5 || m.resultsScroll.Y != 5*resultsRowHeight {
		t.Fatalf("the list starts at row %d and offset %v", first, m.resultsScroll.Y)
	}
	offset := m.resultsScroll.Y
	chosenDisplay := first + 2
	chosen := visible[chosenDisplay]
	chosenSeq := m.rec.Rows[chosen].Seq

	// The row's center is empty space beside the status badge; the cell and
	// the row carry the detail action, not just the text.
	rect, ok := tt.Find(m.rec.Rows[chosen].Label())
	if !ok {
		t.Fatalf("row %d is not rendered", chosenDisplay)
	}
	tt.ClickAt(rect.X+rect.W/2, rect.Y+rect.H/2)
	tt.Frame()
	if !m.resultsDetail {
		t.Fatal("clicking the row did not open its detail")
	}
	if m.selected != chosen || m.tableSelected != chosenDisplay {
		t.Fatalf("the click chose row %d (table %d), want %d (table %d)",
			m.selected, m.tableSelected, chosen, chosenDisplay)
	}
	if !tt.HasText(resultDetailTitle(&m.rec.Rows[chosen])) {
		t.Fatalf("the detail does not show the clicked row: %q", tt.Texts())
	}

	if err := tt.Click("← 结果列表"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if m.resultsDetail {
		t.Fatal("the back action did not return to the list")
	}
	if m.slotFilter != "2" || m.filter != resultFilterPending {
		t.Fatalf("the return lost the filter: slot %q filter %v", m.slotFilter, m.filter)
	}
	if m.selectedSeq() != chosenSeq {
		t.Fatalf("the return lost the selection: seq %d, want %d", m.selectedSeq(), chosenSeq)
	}
	if firstAfter, _ := m.list.Visible(); firstAfter != first {
		t.Fatalf("the returned list starts at row %d, want %d", firstAfter, first)
	}
	if got := m.resultsScroll.Y; got != offset {
		t.Fatalf("the returned offset = %v, want %v", got, offset)
	}
}

// TestResultsCompositeKeyboardSelection checks the composite's keyboard
// path: Escape returns the keyboard from a page control, Down moves the
// chosen table row, and Enter opens its detail.
func TestResultsCompositeKeyboardSelection(t *testing.T) {
	m, _, tt := openResponsiveResults(t, manyPendingRecord(t, 40), 860, 560)
	visible := m.visibleResultRows()
	if err := tt.Click("全部 40"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	tt.Frame()
	first, _ := m.list.Visible()
	if !tt.Focused(m.rec.Rows[visible[first]].Label()) {
		t.Fatalf("Escape did not give the results list the focus: %q", tt.Texts())
	}
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	if m.selected != visible[first+1] || m.tableSelected != first+1 {
		t.Fatalf("Down chose row %d (table %d), want %d (table %d)",
			m.selected, m.tableSelected, visible[first+1], first+1)
	}
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if !m.resultsDetail {
		t.Fatal("Enter on the chosen row did not open its detail")
	}
	if m.selectedSeq() != m.rec.Rows[visible[first+1]].Seq {
		t.Fatalf("the detail shows seq %d, want %d", m.selectedSeq(), m.rec.Rows[visible[first+1]].Seq)
	}
}

// TestResultsSnapStepRearmsAfterRepaint checks the rest debounce asks for the
// time left after a repaint between a precise scroll and the deadline. MyGo
// keeps only the timer the last frame armed; a frame that does not ask again
// would drop the pending snap.
func TestResultsSnapStepRearmsAfterRepaint(t *testing.T) {
	m := &Model{}
	m.resultsScroll.Y = 17
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.Local)

	wait, snap := m.resultsScrollSnapStep(now)
	if snap || wait != resultsSnapDelay {
		t.Fatalf("the first move: wait %v snap %v, want %v and no snap",
			wait, snap, resultsSnapDelay)
	}
	wait, snap = m.resultsScrollSnapStep(now.Add(20 * time.Millisecond))
	if snap {
		t.Fatal("a repaint snapped the move mid-input")
	}
	if want := resultsSnapDelay - 20*time.Millisecond; wait != want {
		t.Fatalf("the repaint asks for %v, want the remaining %v", wait, want)
	}
	wait, snap = m.resultsScrollSnapStep(now.Add(resultsSnapDelay))
	if !snap || wait != 0 {
		t.Fatalf("at the deadline: wait %v snap %v, want the snap", wait, snap)
	}
	if got := resultRowOffset(m.resultsScroll.Y); got != resultsRowHeight {
		t.Fatalf("the snapped offset is %v, want %d", got, resultsRowHeight)
	}
}
