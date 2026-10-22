package ui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/game/zzz"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/egoist/mygo/ui"
)

// resultFilterOrder is the results toolbar's segment order: everything,
// the review queue the page starts on, and the items a direct export must
// leave out.
var resultFilterOrder = []resultFilter{
	resultFilterAll,
	resultFilterPending,
	resultFilterUnexportable,
}

func (f resultFilter) label() string {
	switch f {
	case resultFilterAll:
		return "全部"
	case resultFilterPending:
		return "待校对"
	case resultFilterUnexportable:
		return "无法导出"
	default:
		return ""
	}
}

// resultStatusColumnText is the results table's compact status: the
// processing outcome for failed or unprocessed rows and just the quality
// for produced ones. Problems themselves belong to the detail subtitle.
func resultStatusColumnText(row *Row) string {
	switch row.Status {
	case record.StatusProduced:
		if row.PendingReview() {
			return "待校对"
		}
		if row.OptionalEquippedOnly() {
			return "自动接受"
		}
		return row.QualityText()
	case record.StatusFailed:
		return "失败"
	case record.StatusUnprocessed:
		return "未处理"
	default:
		return row.StatusText()
	}
}

// resultStatusText renders a table row's status, naming the current
// visit's just-confirmed item as such while it lingers in the pending
// segment.
func (m *Model) resultStatusText(row *Row) string {
	if m.recentlyConfirmedNow(row) {
		return "已确认"
	}
	return resultStatusColumnText(row)
}

// visibleResultRows maps the rendered table to the record's rows. The
// four-page shell filters by the slot's segment, search text and set/slot
// choices; the legacy single-column view keeps showing every row.
func (m *Model) visibleResultRows() []int {
	if m.rec == nil {
		return nil
	}
	rows := make([]int, 0, len(m.rec.Rows))
	for i := range m.rec.Rows {
		if !m.cfg.PageLayout || m.resultRowMatches(&m.rec.Rows[i]) {
			rows = append(rows, i)
		}
	}
	return rows
}

// resultRowMatches reports whether one row is in the slot's current
// segment and matches its search text and set/slot choices.
func (m *Model) resultRowMatches(row *Row) bool {
	switch m.filter {
	case resultFilterPending:
		if !row.PendingReview() && !m.recentlyConfirmedNow(row) {
			return false
		}
	case resultFilterUnexportable:
		if !resultRowUnexportable(row) {
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
	if query := strings.ToLower(strings.TrimSpace(m.search)); query != "" {
		if !strings.Contains(resultRowSearchText(row), query) {
			return false
		}
	}
	return true
}

// resultRowSearchText is the searchable text of one row: identity,
// structured values, state and the failure/quality reason.
func resultRowSearchText(row *Row) string {
	parts := []string{
		strconv.FormatInt(row.Seq, 10),
		row.SetColumnText(),
		row.SlotLevelColumnText(),
		row.MainStatText(),
		resultStatusColumnText(row),
		row.StatusText(),
	}
	if row.Failure != "" {
		parts = append(parts, "识别失败："+row.Failure)
	}
	return strings.ToLower(strings.Join(parts, " "))
}

// resultRowUnexportable mirrors the export's usable-item rule for one
// rendered row: failed, unprocessed and produced-but-excluded items.
// Pending rows belong to the review segment, not this one. The export
// snapshot remains authoritative for the summary counts.
func resultRowUnexportable(row *Row) bool {
	if row.PendingReview() {
		return false
	}
	if row.Status != record.StatusProduced || row.Structured == nil {
		return true
	}
	optionalEquipped := row.Quality == record.QualityNeedsReview && row.OptionalEquippedOnly()
	switch row.Quality {
	case record.QualityAutoAccepted, record.QualityHumanConfirmed:
		if row.Structured.Validation.Status != zzz.ValidationPassed {
			return true
		}
	case record.QualityNeedsReview:
		if !optionalEquipped {
			return true
		}
	default:
		return true
	}
	check := app.CheckDiscData(row.Structured.Data)
	if check.Validation.Status != zzz.ValidationPassed {
		return true
	}
	for _, issue := range check.Validation.Issues {
		if issue.Severity == zzz.SeverityError {
			return true
		}
	}
	for _, issue := range row.Issues() {
		if issue.Severity == zzz.SeverityError {
			return true
		}
	}
	data := row.Structured.Data
	if data.SetID == nil || data.Rarity == nil || *data.Rarity != zzz.RarityS ||
		data.Slot == nil || data.Level == nil || data.MainStat == nil ||
		data.Substats == nil || data.ManagementState == nil {
		return true
	}
	if _, ok := zzz.LookupSetByID(*data.SetID); !ok {
		return true
	}
	return false
}

// resultFilterCount is the record-wide count of one segment, before the
// search and set/slot choices narrow it.
func (m *Model) resultFilterCount(filter resultFilter) int {
	if m.rec == nil {
		return 0
	}
	n := 0
	for i := range m.rec.Rows {
		row := &m.rec.Rows[i]
		switch filter {
		case resultFilterAll:
			n++
		case resultFilterPending:
			if row.PendingReview() {
				n++
			}
		case resultFilterUnexportable:
			if resultRowUnexportable(row) {
				n++
			}
		}
	}
	return n
}

// setResultFilter chooses a segment without replacing the chosen item's
// data: the selection stays while its row remains visible, otherwise the
// first visible row is chosen or none at all.
func (m *Model) setResultFilter(filter resultFilter) {
	if m.filter == filter {
		return
	}
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
		m.clampResultScroll()
		return
	}
	m.selected = visible[0]
	m.selectedSequence = m.rec.Rows[m.selected].Seq
	m.scroll = 0
	m.clampResultScroll()
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

// afterResultFilterChange settles a search or set/slot choice the widgets
// wrote directly into the slot state.
func (m *Model) afterResultFilterChange() {
	m.settleResultSelection()
}

// hasResultFilters reports whether the search or set/slot choices narrow
// the chosen segment.
func (m *Model) hasResultFilters() bool {
	return strings.TrimSpace(m.search) != "" || m.setFilter != "" || m.slotFilter != ""
}

// filteredEmptyText separates an empty filter result from the completion
// state: a pending queue of zero is done, while other empty views are
// merely filtered out.
func (m *Model) filteredEmptyText() string {
	if m.hasResultFilters() {
		return "没有符合搜索或筛选条件的条目。"
	}
	switch m.filter {
	case resultFilterPending:
		if m.pendingCount() == 0 {
			return "待校对 0 件，校对已完成。"
		}
		return "没有待校对的条目。"
	case resultFilterUnexportable:
		return "没有无法导出的条目。"
	default:
		return "没有条目。"
	}
}

// resultFilterOption is one set/slot choice: the stored id and its label.
type resultFilterOption struct {
	id    string
	label string
}

func setFilterOptions(rows []Row, current string) []resultFilterOption {
	options := []resultFilterOption{{id: "", label: "全部套装"}}
	ids := make(map[int]bool)
	for i := range rows {
		data := rows[i].Structured
		if data == nil || data.Data.SetID == nil {
			continue
		}
		ids[*data.Data.SetID] = true
	}
	sorted := make([]int, 0, len(ids))
	for id := range ids {
		sorted = append(sorted, id)
	}
	slices.Sort(sorted)
	for _, id := range sorted {
		options = append(options, resultFilterOption{id: strconv.Itoa(id), label: describeSet(id)})
	}
	if current != "" && parseDecimal(current) >= 0 {
		found := false
		for _, option := range options {
			if option.id == current {
				found = true
				break
			}
		}
		if !found {
			id, _ := strconv.Atoi(current)
			options = append(options, resultFilterOption{id: current, label: describeSet(id)})
		}
	}
	return options
}

func slotFilterOptions(current string) []resultFilterOption {
	options := []resultFilterOption{{id: "", label: "全部槽位"}}
	for slot := 1; slot <= 6; slot++ {
		id := strconv.Itoa(slot)
		options = append(options, resultFilterOption{id: id, label: id + " 号位"})
	}
	known := false
	for _, option := range options {
		if option.id == current {
			known = true
			break
		}
	}
	if current != "" && !known {
		options = append(options, resultFilterOption{id: current, label: current + " 号位"})
	}
	return options
}

// parseDecimal reports the value of a non-empty decimal string, -1 for
// anything else.
func parseDecimal(text string) int {
	value, err := strconv.Atoi(text)
	if err != nil || value < 0 {
		return -1
	}
	return value
}

// resultsFilterBar is the results page's toolbar: the segments in their
// confirmed order, the search field and the set/slot choices. All values
// live in the active slot, so leaving and returning restores them.
func (m *Model) resultsFilterBar(c *ui.Context, t *ui.Theme) {
	changed := false
	ui.Row(c).Gap(t.Space(1)).AlignItems(ui.Center).Children(func() {
		for _, filter := range resultFilterOrder {
			label := fmt.Sprintf("%s %d", filter.label(), m.resultFilterCount(filter))
			button := ui.Button(c, label)
			if m.filter == filter {
				button.Background(t.SurfacePressed)
			}
			if button.Clicked() {
				m.setResultFilter(filter)
			}
		}
		search := m.noteTextInputFocus(ui.SearchField(c, &m.search).Width(t.Space(48)).
			Placeholder("搜索序号、套装、主词条"))
		if search.Changed() {
			changed = true
		}
		if m.resultFilterSelect(c, t, &m.setFilter, "全部套装",
			setFilterOptions(m.rec.Rows, m.setFilter), t.Space(34)) {
			changed = true
		}
		if m.resultFilterSelect(c, t, &m.slotFilter, "全部槽位",
			slotFilterOptions(m.slotFilter), t.Space(26)) {
			changed = true
		}
	})
	if changed {
		m.afterResultFilterChange()
	}
}

// resultFilterSelect is a set/slot picker whose stored value is the id
// while the trigger and popup show its human label.
func (m *Model) resultFilterSelect(c *ui.Context, t *ui.Theme, value *string, allLabel string, options []resultFilterOption, width float32) bool {
	selected := ui.SelectBase(c, value)
	selected.Trigger.Width(width).Padding(t.Space(1.5), t.Space(2)).
		Border(1, t.Border).Radius(t.Radius).Background(t.Surface).Children(func() {
		ui.Text(c, resultFilterLabel(*value, allLabel, options)).SingleLine()
	})
	selected.Popup(func(panel ui.Element) {
		panel.Padding(t.Space(1)).Background(t.Background).
			Border(1, t.Border).Radius(t.Radius).Children(func() {
			for _, option := range options {
				item := selected.Item(option.id).Padding(t.Space(1), t.Space(2)).Radius(t.Radius)
				if option.id == *value {
					item.Background(t.Surface)
				}
				item.Children(func() {
					ui.Text(c, option.label).SingleLine()
				})
			}
		})
	})
	return selected.Trigger.Changed()
}

func resultFilterLabel(value, allLabel string, options []resultFilterOption) string {
	if value == "" {
		return allLabel
	}
	for _, option := range options {
		if option.id == value {
			return option.label
		}
	}
	return value
}

// registerResultsShortcuts wires the results page's keys. In the editor
// ⌘↵ saves the draft and confirms it, and ⌘S saves it without confirming;
// both keep the editor open on failure. ⌘J moves to the next pending item
// through requestNavigation, so a dirty draft holds the move behind the
// unsaved prompt. The status bar names only the keys that are registered
// and usable in the current layout.
func (m *Model) registerResultsShortcuts(c *ui.Context) {
	if !m.cfg.PageLayout {
		return
	}
	if m.reviewSaving || m.exportBusy() {
		return
	}
	if m.edit != nil {
		c.OnShortcut(ui.Cmd, ui.KeyEnter, func() {
			// The editor's draft is what ⌘↵ confirms; confirming the
			// persisted row here would silently drop the input.
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
}

// selectNextPendingResult moves the selection to the next pending row in
// the current filtered view, wrapping around. It never leaves the view for
// another slot. The move goes through requestNavigation, so an open editor
// with unsaved input holds it behind the save/discard/stay prompt instead
// of dropping the draft.
func (m *Model) selectNextPendingResult() {
	visible := m.visibleResultRows()
	if len(visible) == 0 {
		return
	}
	start := resultDisplayIndex(visible, m.selected)
	for step := 1; step <= len(visible); step++ {
		next := (start + step) % len(visible)
		if start < 0 && step == 1 {
			next = 0
		}
		row := &m.rec.Rows[visible[next]]
		if !row.PendingReview() {
			continue
		}
		m.requestNavigation(navigationRequest{kind: navSelect, seq: row.Seq})
		if m.navPrompt == nil && m.selected == visible[next] {
			m.list.ScrollIntoView(next)
		}
		return
	}
}

func resultDisplayIndex(visible []int, full int) int {
	for i, index := range visible {
		if index == full {
			return i
		}
	}
	return -1
}

// narrowResults reports the list ↔ detail layout below the 1024 split.
func (m *Model) narrowResults() bool {
	return m.cfg.PageLayout && !resultsSideBySide(m.contentWidth)
}

// openResultDetail moves a narrow results page from its list to the
// chosen item, keeping the list's filter, selection and place.
func (m *Model) openResultDetail() {
	if !m.narrowResults() || m.resultsDetail || m.rec == nil {
		return
	}
	if m.selected < 0 || m.selected >= len(m.rec.Rows) {
		return
	}
	m.captureActiveResult()
	m.resultsDetail = true
}

// openResultDetailAt opens the detail page for a row clicked in the list,
// choosing it first when the click was not a selection change (a click on
// the already chosen row).
func (m *Model) openResultDetailAt(full int) {
	if !m.narrowResults() || m.resultsDetail || m.rec == nil {
		return
	}
	if full < 0 || full >= len(m.rec.Rows) {
		return
	}
	if full != m.selected {
		if m.reviewSaving {
			return
		}
		m.selected = full
		m.selectedSequence = m.rec.Rows[full].Seq
		m.tableSelected = resultDisplayIndex(m.visibleResultRows(), full)
	}
	m.openResultDetail()
}

// closeResultDetail returns to the list and restores its filter, chosen
// row and scroll position.
func (m *Model) closeResultDetail() {
	if !m.resultsDetail {
		return
	}
	m.resultsDetail = false
	m.resetResultList()
	m.settleResultSelection()
}

// evidenceDisplaySize is the evidence image's displayed size by layout:
// 1440, the 1024 split and the narrow list/detail pages. The source crop is
// never enlarged; every size stays at or below the 412×206 capture.
func (m *Model) evidenceDisplaySize() (float32, float32) {
	if !m.cfg.PageLayout {
		return 0, 0
	}
	switch {
	case m.contentWidth >= 1440:
		return 340, 170
	case resultsSideBySide(m.contentWidth):
		return 224, 112
	default:
		return 196, 98
	}
}

// resultsDetailWidth estimates the detail pane's inner width for evidence
// layout: the window less the rail and page padding, then the table split
// when both panes show.
func (m *Model) resultsDetailWidth() float32 {
	width := float32(m.contentWidth) - m.sidebarWidth() - 24
	if resultsSideBySide(m.contentWidth) {
		width -= m.split
	}
	if width < 0 {
		return 0
	}
	return width
}

// evidenceStacked lays a narrow detail pane's evidence out vertically, so
// the 224-DIP image and its recognized text fit the 408-DIP pane.
func (m *Model) evidenceStacked() bool {
	return m.cfg.PageLayout && m.resultsDetailWidth() < 600
}

// resultShortcutText is the results page's status-bar keys. An open
// editor names its save-and-confirm and save keys; the list and detail
// otherwise name the keys their layout registers. Narrow list pages open a
// row with Return.
func (m *Model) resultShortcutText() string {
	if m.reviewSaving || m.exportBusy() {
		return ""
	}
	if m.edit != nil {
		return "⌘↵ 保存并确认并下一项  ·  ⌘S 保存修改"
	}
	if m.rec == nil || len(m.rec.Rows) == 0 || m.completionVisible() {
		return ""
	}
	if m.narrowResults() && !m.resultsDetail {
		return "↵ 查看详情  ·  ⌘J 下一个待校对"
	}
	return "⌘↵ 确认并下一项  ·  ⌘J 下一个待校对"
}
