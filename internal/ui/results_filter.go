package ui

import (
	"fmt"
	"slices"
	"strconv"

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
// segment and matches its set/slot facets. A just-confirmed item stays
// visible in the pending segment for the rest of this visit, so the
// selection never jumps out of the list.
func (m *Model) resultRowMatches(row *Row) bool {
	if !(m.filter == resultFilterPending && m.recentlyConfirmedNow(row)) {
		if !resultRowInFilter(row, m.filter) {
			return false
		}
	}
	if m.setFilter != "" {
		if row.Structured == nil || row.Structured.Data.SetID == nil ||
			strconv.Itoa(*row.Structured.Data.SetID) != m.setFilter {
			return false
		}
	}
	if m.slotFilter != "" {
		if row.Structured == nil || row.Structured.Data.Slot == nil ||
			strconv.Itoa(*row.Structured.Data.Slot) != m.slotFilter {
			return false
		}
	}
	return true
}

// hasResultFacets reports whether a set or slot facet narrows the current
// segment.
func (m *Model) hasResultFacets() bool {
	return m.setFilter != "" || m.slotFilter != ""
}

// clearResultFacets removes the set/slot narrowing and settles the list.
func (m *Model) clearResultFacets() {
	m.setFilter = ""
	m.slotFilter = ""
	m.afterResultFilterChange()
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
	if m.hasResultFacets() {
		return "没有符合筛选条件的条目。"
	}
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
	// A facet change is a new visit to the filtered list: a confirmed item
	// that only lingered in the pending segment no longer belongs here.
	m.clearRecentlyConfirmed()
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

// resultsFacetFilters are the set and slot narrowing controls. They sit
// under the three segments in the standard list and at the top of the
// compact popover; they never replace the three segments themselves.
func (m *Model) resultsFacetFilters(c *ui.Context, t *ui.Theme, width float32) {
	changed := false
	ui.Row(c).Gap(t.Space(1)).AlignItems(ui.Center).Children(func() {
		setLabel := setFilterLabel(m.setFilter)
		before := setLabel
		ui.Select(c, &setLabel, setFilterChoices(m.rec.Rows, m.setFilter)).
			Width(width).Shrink(1).Label("套装筛选")
		if setLabel != before {
			if setLabel == "全部套装" {
				m.setFilter = ""
			} else {
				m.setFilter = optionValue(setLabel)
			}
			changed = true
		}
		slotLabel := slotFilterLabel(m.slotFilter)
		before = slotLabel
		ui.Select(c, &slotLabel, slotFilterChoices(m.slotFilter)).
			Width(width).Shrink(1).Label("槽位筛选")
		if slotLabel != before {
			if slotLabel == "全部槽位" {
				m.slotFilter = ""
			} else {
				m.slotFilter = optionValue(slotLabel)
			}
			changed = true
		}
	})
	if changed {
		m.afterResultFilterChange()
	}
}

// setFilterLabel is the picker's label for the current facet id.
func parseDecimal(text string) int {
	value, ok := parseDecimalInt(text)
	if !ok {
		return -1
	}
	return value
}

func setFilterLabel(id string) string {
	if id == "" || parseDecimal(id) < 0 {
		return "全部套装"
	}
	return describeSet(parseDecimal(id))
}

// setFilterChoices lists "all" followed by the sets the open record
// actually carries, so the filter never offers a catalog-wide list.
func setFilterChoices(rows []Row, current string) []string {
	options := []string{"全部套装"}
	ids := make(map[int]bool)
	for i := range rows {
		structured := rows[i].Structured
		if structured == nil || structured.Data.SetID == nil {
			continue
		}
		ids[*structured.Data.SetID] = true
	}
	sorted := make([]int, 0, len(ids))
	for id := range ids {
		sorted = append(sorted, id)
	}
	slices.Sort(sorted)
	known := false
	for _, id := range sorted {
		label := describeSet(id)
		options = append(options, label)
		if current == strconv.Itoa(id) {
			known = true
		}
	}
	if current != "" && !known && parseDecimal(current) >= 0 {
		options = append(options, describeSet(parseDecimal(current)))
	}
	return options
}

// slotFilterLabel is the picker's label for the current facet id.
func slotFilterLabel(id string) string {
	if id == "" || parseDecimal(id) < 0 {
		return "全部槽位"
	}
	return id + " 号位"
}

// slotFilterChoices lists all six slots plus an unknown current id, so a
// legacy value stays visible for correction.
func slotFilterChoices(current string) []string {
	options := []string{"全部槽位"}
	known := false
	for slot := 1; slot <= 6; slot++ {
		id := strconv.Itoa(slot)
		options = append(options, id+" 号位")
		if current == id {
			known = true
		}
	}
	if current != "" && !known {
		options = append(options, current+" 号位")
	}
	return options
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
		return shortcutConfirmNext + " 确认并下一项  ·  " + shortcutSave + " 保存修改" + zoom
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
