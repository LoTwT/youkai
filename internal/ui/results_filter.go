package ui

import (
	"fmt"
	"slices"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/egoist/mygo/ui"
)

// resultFilterOrder is the item list's segment order (08 §3.1): the items a
// person still has to handle, the exportable items carrying a warning, and
// every captured item.
var resultFilterOrder = []resultFilter{
	resultFilterPending,
	resultFilterWarning,
	resultFilterAll,
}

func (f resultFilter) label() string {
	switch f {
	case resultFilterPending:
		return "待处理"
	case resultFilterWarning:
		return "警告"
	case resultFilterAll:
		return "全部"
	default:
		return ""
	}
}

// resultRowInFilter reports whether one row belongs to a segment. The
// pending segment holds both pending classes (backfill and review). The
// warning segment is exactly the exportable warning class.
func resultRowInFilter(row *Row, filter resultFilter) bool {
	switch filter {
	case resultFilterPending:
		return row.ItemClass == app.ItemClassPendingBackfill ||
			row.ItemClass == app.ItemClassPendingReview
	case resultFilterWarning:
		return row.ItemClass == app.ItemClassWarning
	default:
		return true
	}
}

// visibleResultRows maps the item list to the record's rows, narrowed by
// the open record's segment.
func (m *Model) visibleResultRows() []int {
	if m.rec == nil {
		return nil
	}
	rows := make([]int, 0, len(m.rec.Rows))
	for i := range m.rec.Rows {
		if m.resultRowMatches(&m.rec.Rows[i]) {
			rows = append(rows, i)
		}
	}
	return rows
}

// resultRowMatches reports whether one row is in the open record's current
// segment. A just-confirmed item stays visible in the pending segment for
// the rest of this visit, so the selection never jumps out of the list.
func (m *Model) resultRowMatches(row *Row) bool {
	if m.filter == resultFilterPending && m.recentlyConfirmedNow(row) {
		return true
	}
	return resultRowInFilter(row, m.filter)
}

// resultFilterCount is the record-wide count of one segment.
func (m *Model) resultFilterCount(filter resultFilter) int {
	if m.rec == nil {
		return 0
	}
	counts := m.classCounts()
	switch filter {
	case resultFilterPending:
		return int(counts.Pending())
	case resultFilterWarning:
		return int(counts.Warning)
	default:
		return len(m.rec.Rows)
	}
}

// setResultFilter chooses a segment. Switching segments ends the visit's
// just-confirmed markers: the confirmed items leave the pending segment as
// the design requires.
func (m *Model) setResultFilter(filter resultFilter) {
	if m.filter == filter {
		return
	}
	m.clearRecentlyConfirmed()
	m.filter = filter
	m.settleResultSelection()
}

// settleResultSelection keeps the selection inside the current filter.
func (m *Model) settleResultSelection() {
	if m.rec == nil {
		m.selected = -1
		m.scroll = 0
		return
	}
	visible := m.visibleResultRows()
	if len(visible) == 0 {
		m.selected = -1
		m.scroll = 0
		return
	}
	if slices.Contains(visible, m.selected) {
		// The chosen row is still shown: keep the list where the user left
		// it, including a scroll only the ListState knows about.
		return
	}
	m.selected = visible[0]
	m.selectedSequence = m.rec.Rows[m.selected].Seq
	m.scroll = 0
	m.clampResultScroll()
	m.pointFirstReviewField()
}

// clampResultScroll keeps the list's remembered place inside the filtered
// content, never in the blank area past its last row.
func (m *Model) clampResultScroll() {
	visible := m.visibleResultRows()
	if len(visible) == 0 {
		m.scroll = 0
		return
	}
	if m.scroll < 0 {
		m.scroll = 0
	}
	if m.scroll >= len(visible) {
		m.scroll = len(visible) - 1
	}
	m.list.ScrollTo(m.scroll, ui.Start)
}

// filteredEmptyText explains an empty segment and the way back to the
// whole record.
func (m *Model) filteredEmptyText() string {
	switch m.filter {
	case resultFilterPending:
		if m.pendingItemsCount() == 0 {
			return "待处理 0 件，都处理完了。"
		}
		return "没有待处理的条目。"
	case resultFilterWarning:
		return "没有“有警告但可导出”的条目。"
	default:
		return "没有条目。"
	}
}

// resultDisplayIndex maps a full row index to its display row.
func resultDisplayIndex(visible []int, full int) int {
	for i, index := range visible {
		if index == full {
			return i
		}
	}
	return -1
}

// afterResultFilterChange settles a segment choice made in the compact
// strip or the list.
func (m *Model) afterResultFilterChange() {
	m.settleResultSelection()
	m.tableSelected = resultDisplayIndex(m.visibleResultRows(), m.selected)
}

// resultsFilterBar is the item list's segment control. The compact strip
// reuses it in its top bar.
func (m *Model) resultsFilterBar(c *ui.Context, t *ui.Theme) {
	labels := make([]string, len(resultFilterOrder))
	for i, filter := range resultFilterOrder {
		labels[i] = filter.label()
	}
	choice := m.reviewFilterChoice()
	ui.Segmented(c, choice, labels...).Bind(&m.reviewFilterRef)
	if *choice >= 0 && *choice < len(resultFilterOrder) && resultFilterOrder[*choice] != m.filter {
		m.setResultFilter(resultFilterOrder[*choice])
		m.afterResultFilterChange()
	}
}

// reviewFilterChoice is the segment control's index for the current
// filter. Segmented writes into an int, so the frame resolves it before
// building the control.
func (m *Model) reviewFilterChoice() *int {
	choice := 0
	for i, filter := range resultFilterOrder {
		if filter == m.filter {
			choice = i
		}
	}
	return &choice
}

// selectNextPendingResult moves the selection to the next item a person
// still has to handle (待处理), wrapping around. The move goes through
// requestNavigation, so an open editor with unsaved input holds it behind
// the save/discard/stay prompt instead of dropping the draft.
func (m *Model) selectNextPendingResult() {
	m.selectPendingResult(1)
}

// selectPreviousPendingResult is Ctrl+Shift+J: the same walk backwards.
func (m *Model) selectPreviousPendingResult() {
	m.selectPendingResult(-1)
}

func (m *Model) selectPendingResult(direction int) {
	visible := m.visibleResultRows()
	if len(visible) == 0 {
		return
	}
	start := resultDisplayIndex(visible, m.selected)
	for step := 1; step <= len(visible); step++ {
		next := (start + direction*step) % len(visible)
		if next < 0 {
			next += len(visible)
		}
		if start < 0 && direction > 0 && step == 1 {
			next = 0
		}
		row := &m.rec.Rows[visible[next]]
		if !resultRowInFilter(row, resultFilterPending) {
			continue
		}
		m.requestNavigation(navigationRequest{kind: navSelect, seq: row.Seq})
		if m.navPrompt == nil && m.selected == visible[next] {
			m.list.ScrollIntoView(next)
			m.tableSelected = next
		}
		return
	}
}

// selectAdjacentVisible selects the previous or next visible item, the
// compact strip's ‹ and › buttons.
func (m *Model) selectAdjacentVisible(direction int) {
	visible := m.visibleResultRows()
	if len(visible) == 0 {
		return
	}
	current := resultDisplayIndex(visible, m.selected)
	next := current + direction
	if next < 0 || next >= len(visible) {
		return
	}
	m.requestNavigation(navigationRequest{kind: navSelect, seq: m.rec.Rows[visible[next]].Seq})
	if m.navPrompt == nil && m.selected == visible[next] {
		m.list.ScrollIntoView(next)
		m.tableSelected = next
	}
}

// resultsRowHeight is the design's 32-DIP results row at 100% text size.
const resultsRowHeight = 32

// emptyResultsText explains a record with no items.
func (m *Model) emptyResultsText() string {
	if m.rec == nil {
		return "还没有扫描结果。开始一次扫描，或从历史记录打开一条已保存的记录。"
	}
	if text := m.emptyResultsTextForTermination(); text != "" {
		return text
	}
	if m.rec.Live {
		return "本次扫描还没有已保存的条目；扫描过程中条目按批次保存，收尾后全部可见。"
	}
	return "该记录没有条目。"
}

// wholeRowTableHeight is a MyGo table's header and divider plus as many
// whole rows as available leaves. A leftover strip shorter than a row stays
// blank instead of showing a clipped last row.
func wholeRowTableHeight(available, row, chrome float32) (float32, bool) {
	if row <= 0 || chrome <= 0 || available <= chrome {
		return 0, false
	}
	rows := int((available - chrome) / row)
	return chrome + float32(rows)*row, true
}

// registerResultsShortcuts wires the review view's keys. Ctrl+Enter saves
// the draft and confirms it; Ctrl+S saves without confirming; both keep
// the editor open on failure. Ctrl+J / Ctrl+Shift+J walk the pending
// items through requestNavigation, so a dirty draft holds the move behind
// the unsaved prompt. Ctrl+= / Ctrl+- / Ctrl+0 zoom the screenshots.
func (m *Model) registerResultsShortcuts(c *ui.Context) {
	if m.reviewSaving || m.loading || m.recErr != "" || m.exportBusy() {
		return
	}
	if m.edit != nil {
		c.OnShortcut(ui.Cmd, ui.KeyEnter, func() {
			// The editor's draft is what Ctrl+Enter confirms; confirming
			// the persisted row here would silently drop the input.
			m.saveEdit(reviewConfirmNext, nil)
		})
		c.OnShortcut(ui.Cmd, ui.KeyS, func() {
			m.saveEdit(reviewSave, nil)
		})
	} else {
		c.OnShortcut(ui.Cmd, ui.KeyEnter, func() {
			m.confirmCurrent()
		})
	}
	c.OnShortcut(ui.Cmd, ui.KeyJ, func() {
		m.selectNextPendingResult()
	})
	c.OnShortcut(ui.Cmd|ui.Shift, ui.KeyJ, func() {
		m.selectPreviousPendingResult()
	})
	// Zoom belongs to the review view only and not to a text field: a
	// focused field keeps its own Ctrl+= undo-like input handling.
	if !m.textInputFocused {
		c.OnShortcut(ui.Cmd, ui.KeyEqual, func() { m.zoomCrop(0.25) })
		c.OnShortcut(ui.Cmd|ui.Shift, ui.KeyEqual, func() { m.zoomCrop(0.25) })
		c.OnShortcut(ui.Cmd, ui.KeyMinus, func() { m.zoomCrop(-0.25) })
		c.OnShortcut(ui.Cmd, ui.Key0, func() { m.zoomCrop(0) })
	}
}

// resultShortcutText is the review page's status-bar keys.
func (m *Model) resultShortcutText() string {
	if m.reviewSaving || m.loading || m.recErr != "" || m.exportBusy() {
		return ""
	}
	zoom := "  ·  " + shortcutZoomIn + " 放大  ·  " + shortcutZoomOut + " 缩小"
	if m.edit != nil {
		return shortcutConfirmNext + " 保存并确认并下一项  ·  " + shortcutSave + " 保存修改" + zoom
	}
	if m.rec == nil || len(m.rec.Rows) == 0 {
		return ""
	}
	return shortcutConfirmNext + " 确认并下一项  ·  " + shortcutNextPending + " 下一个待处理" +
		"  ·  " + shortcutPreviousPending + " 上一个待处理" + zoom
}

// compactPositionText is the compact strip's "待处理 1 / 3" label: the
// current item's place in the chosen segment.
func (m *Model) compactPositionText(visible []int) string {
	if len(visible) == 0 {
		return m.filter.label() + " 0 / 0"
	}
	index := resultDisplayIndex(visible, m.selected)
	if index < 0 {
		index = 0
	}
	return fmt.Sprintf("%s %d / %d", m.filter.label(), index+1, len(visible))
}

// listFooterText is the standard list's footer, the same position label.
func (m *Model) listFooterText(visible []int) string {
	return m.compactPositionText(visible)
}
