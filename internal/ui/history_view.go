package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

// The records list has two width tiers (09 §2): standard from a 1120-DIP
// window, compact from the 960-DIP minimum. Compact hides the storage
// column and merges 待处理 and 可导出 into one. The model's content width is
// the window's client area, so the tier compares it with the window width.
const (
	historyStandardMinWidth = 1120
	historyTimeWidth        = 168
	historyActionWidth      = 48
	historyCountWidth       = 88
	historyCompactCount     = 80
	historyMergedWidth      = 132
)

// historyColumnsStandard is the standard tier: 时间 / 结果 / 件数 /
// 待处理 / 可导出 / 占用 / ⋯. 结果 takes the room the fixed columns leave.
var historyColumnsStandard = []ui.TableColumn{
	{Title: "时间", Width: historyTimeWidth, Fixed: true},
	{Title: "结果", MinWidth: 220, Fixed: true},
	{Title: "件数", Width: historyCountWidth, Align: ui.End, Fixed: true},
	{Title: "待处理", Width: historyCountWidth, Align: ui.End, Fixed: true},
	{Title: "可导出", Width: historyCountWidth, Align: ui.End, Fixed: true},
	{Title: "占用", Width: 96, Align: ui.End, Fixed: true},
	{ID: "actions", Title: "", Width: historyActionWidth, Align: ui.End, Fixed: true},
}

// historyColumnsCompact is the compact tier: 占用 moves into the row
// menu's delete entry and 待处理 / 可导出 share one column.
var historyColumnsCompact = []ui.TableColumn{
	{Title: "时间", Width: historyTimeWidth, Fixed: true},
	{Title: "结果", MinWidth: 200, Fixed: true},
	{Title: "件数", Width: historyCompactCount, Align: ui.End, Fixed: true},
	{Title: "待处理 / 可导出", Width: historyMergedWidth, Align: ui.End, Fixed: true},
	{ID: "actions", Title: "", Width: historyActionWidth, Align: ui.End, Fixed: true},
}

// historyCompact reports the compact tier from the window width (09 §2).
func historyCompact(contentWidth int) bool {
	return contentWidth < historyStandardMinWidth
}

// historyColumnsFor returns the table columns for the current window.
func historyColumnsFor(contentWidth int) []ui.TableColumn {
	if historyCompact(contentWidth) {
		return historyColumnsCompact
	}
	return historyColumnsStandard
}

// historyPage is the records destination: the title, the date filter, the
// record table and its row menu. The newest record is first and carries
// its conclusion in the 结果 column, so a returning user finds last time's
// scan without reading counts (07 §4, 08 §5). The live session appears as
// the first row with the 本次 badge; it opens the same record workspace as
// any other row, so one record never has two editable copies.
func (m *Model) historyPage(c *ui.Context, t *ui.Theme) {
	_, tok := themeWithTokens(t, c.Preferences())
	m.historyHeader(c, t, tok)
	m.historyNotices(c, t)

	switch {
	case m.historyLoading && len(m.history) == 0:
		ui.Text(c, "正在读取记录……").TextColor(tok.TextMuted)
		return
	case m.histErr != "":
		ui.Text(c, "历史记录读取失败："+m.histErr).TextColor(t.Danger)
		return
	case len(m.history) == 0:
		m.historyEmptyState(c, t, tok, false)
		return
	}

	entries := m.visibleHistory()
	if len(entries) == 0 {
		m.settleHistoryRow(0)
		m.historyEmptyState(c, t, tok, true)
		return
	}
	m.settleHistoryRow(len(entries))
	m.prepareHistoryList(entries)

	ui.Column(c).Grow(1).MinWidth(0).Background(tok.SurfaceCard).
		Border(1, tok.TableBorder).Radius(t.Space(1.5)).Clip().Children(func() {
		columns := historyColumnsFor(m.contentWidth)
		table := ui.Table(c, &m.historyList, columns, len(entries), func(row, col int) {
			if row < 0 || row >= len(entries) || col < 0 || col >= len(columns) {
				return
			}
			m.historyCell(c, t, tok, entries[row], row, columns[col])
		}).Fill().MinHeight(0)
		if table.Submitted() {
			m.openHistoryRow(m.historyRow)
		}
		// Shift+F10 and the menu key open the chosen row's menu while the
		// table or one of its rows holds the keyboard (08 §5).
		if m.historyList.Shortcut(c, ui.Shift, ui.KeyF10) ||
			m.historyList.Shortcut(c, 0, ui.KeyContextMenu) {
			m.openSelectedHistoryMenu()
		}
		if m.historyFocusPending && m.historyList.Focus(c) {
			m.historyFocusPending = false
		}
	})
}

// historyHeader is the page title and its date filter. The filter uses
// DateInput rather than a typed date (M6); 清除筛选 returns the range to
// every record.
func (m *Model) historyHeader(c *ui.Context, t *ui.Theme, tok *Tokens) {
	m.settleHistoryFilterRange()
	ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "记录").FontWeight(600).FontSize(t.FontSize * 1.15)
		if len(m.history) > 0 {
			count := fmt.Sprintf("%d 条", len(m.history))
			if m.historyLoading {
				count += " · 正在读取"
			}
			ui.Text(c, count).TextColor(tok.TextMuted).FontSize(t.FontSize * 0.9)
		}
		ui.Box(c).Grow(1)
		if len(m.history) == 0 {
			return
		}
		ui.Text(c, "日期").TextColor(tok.TextMuted)
		start := ui.DateInput(c, &m.historyFilterStart).Label("开始日期").Width(150)
		if start.Changed() {
			m.historyFilterActive = true
			m.clampHistoryFilter(true)
		}
		ui.Text(c, "–").TextColor(tok.TextMuted)
		end := ui.DateInput(c, &m.historyFilterEnd).Label("结束日期").Width(150)
		if end.Changed() {
			m.historyFilterActive = true
			m.clampHistoryFilter(false)
		}
		if m.historyFilterActive && len(m.visibleHistory()) > 0 {
			if ui.Button(c, "清除筛选").Clicked() {
				m.resetHistoryFilter()
			}
		}
	})
}

// historyEmptyState answers the two empty outcomes of 08 §5: no records at
// all, and a date filter that matches none. The clear action sits in the
// state rather than the header so it appears once.
func (m *Model) historyEmptyState(c *ui.Context, t *ui.Theme, tok *Tokens, filtered bool) {
	ui.Column(c).Grow(1).Justify(ui.Center).AlignItems(ui.Center).Gap(t.Space(2)).Children(func() {
		if filtered {
			ui.Text(c, "没有符合条件的记录").TextColor(tok.TextMuted)
			if ui.Button(c, "清除筛选").Clicked() {
				m.resetHistoryFilter()
			}
			return
		}
		ui.Text(c, "还没有扫描记录").TextColor(tok.TextMuted)
		if ui.Button(c, "去扫描页开始扫描").Clicked() {
			m.showScanDestination()
		}
	})
}

// prepareHistoryList gives the table its stable identity and the row's
// assistive-technology name. The key is the record ID, so a reload or a
// filter change keeps the scroll anchored to a record rather than a row.
func (m *Model) prepareHistoryList(entries []HistoryEntry) {
	m.historyList.Selected = &m.historyRow
	m.historyList.Key = func(row int) any {
		if row < 0 || row >= len(entries) {
			return row
		}
		return entries[row].ID
	}
	m.historyList.Label = func(row int) string {
		if row < 0 || row >= len(entries) {
			return ""
		}
		return historyRowLabel(m, entries[row])
	}
}

// settleHistoryRow keeps the choice inside the visible rows: after a date
// filter narrows the list, or the first frame has no choice yet.
func (m *Model) settleHistoryRow(count int) {
	switch {
	case count == 0:
		m.historyRow = -1
	case m.historyRow < 0 || m.historyRow >= count:
		m.historyRow = 0
	}
}

// historyCell builds one table cell of the records list.
func (m *Model) historyCell(c *ui.Context, t *ui.Theme, tok *Tokens, entry HistoryEntry, row int, column ui.TableColumn) {
	chosen := m.historyRow == row
	latest := row == 0
	id := column.ID
	if id == "" {
		id = column.Title
	}
	switch id {
	case "时间":
		m.historyTimeCell(c, t, tok, entry, latest, chosen)
	case "结果":
		m.historyResultCell(c, t, tok, entry, latest, chosen)
	case "件数":
		m.historyNumberCell(c, t, tok, entry, entry.CapturedCount, entry.Unreadable, latest, chosen)
	case "待处理":
		m.historyPendingCell(c, t, tok, entry, chosen)
	case "可导出":
		m.historyNumberCell(c, t, tok, entry, entry.Counts.Exportable(), entry.Unreadable, latest, chosen)
	case "占用":
		m.historySizeCell(c, t, tok, entry, chosen)
	case "待处理 / 可导出":
		m.historyMergedCountsCell(c, t, tok, entry, latest, chosen)
	case "actions":
		m.historyActionCell(c, t, tok, entry, row)
	}
}

// historyTimeCell is the row's first cell. Its height keeps the design's
// comfortable rows instead of the toolkit's minimum.
func (m *Model) historyTimeCell(c *ui.Context, t *ui.Theme, tok *Tokens, entry HistoryEntry, latest, chosen bool) {
	ui.Row(c).Height(t.Space(8)).AlignItems(ui.Center).Children(func() {
		ui.Row(c).Gap(t.Space(1)).AlignItems(ui.Center).Children(func() {
			text := ui.Text(c, historyTimeText(entry)).SingleLine()
			if latest && !entry.Unreadable {
				text.FontWeight(600)
			}
			if entry.Unreadable {
				// 线框 10 keeps a damaged row's time muted: its reason, not
				// its date, is what the user needs from the row.
				text.TextColor(colorWhenChosen(chosen, tok.TextMuted, t.AccentText))
			}
			if entry.ID != "" && entry.ID == string(m.appliedSession) {
				ui.Text(c, "本次").FontSize(t.FontSize*0.75).TextColor(tok.AccentFG).
					Background(tok.AccentSoft).Padding(0, t.Space(0.5)).Radius(t.Space(0.75))
			}
		})
	})
}

// historyConclusionInput adapts one list entry to the shared conclusion.
// ScanNotWrapped and SaveFailed are model state: the record data alone
// cannot say whether the service still owns the session, including its
// wrap-up save.
func (m *Model) historyConclusionInput(entry HistoryEntry) recordConclusionInput {
	active := entry.ID != "" && entry.ID == string(m.appliedSession)
	return recordConclusionInput{
		CapturedCount:            entry.CapturedCount,
		Coverage:                 entry.Coverage,
		Termination:              entry.Termination,
		Counts:                   entry.Counts,
		Interrupted:              recordConclusionInterrupted(entry.Termination, entry.Interrupted, false),
		IncompletePositions:      entry.IncompletePositions,
		IncompletePositionsKnown: entry.IncompletePositionsKnown,
		Unreadable:               entry.Unreadable,
		ScanNotWrapped:           active && m.snap.Busy,
		SaveFailed:               active && m.snap.SaveError != "",
	}
}

// historyResultFor states one list entry's conclusion.
func (m *Model) historyResultFor(entry HistoryEntry) recordConclusion {
	return recordConclusionFor(m.historyConclusionInput(entry))
}

// historyResultCell draws the tag and the conclusion.
func (m *Model) historyResultCell(c *ui.Context, t *ui.Theme, tok *Tokens, entry HistoryEntry, latest, chosen bool) {
	result := m.historyResultFor(entry)
	ui.Row(c).Gap(t.Space(1)).AlignItems(ui.Center).Children(func() {
		if result.tag != "" {
			bg, fg := tok.WarningSoft, tok.WarningFG
			if result.tone == recordToneDanger {
				bg, fg = tok.DangerSoft, tok.DangerFG
			}
			if chosen {
				// The accent-filled selected row would swallow the soft
				// badge colors; the tag keeps its text and loses the face.
				bg, fg = ui.Color{}, t.AccentText
			}
			ui.Text(c, result.tag).SingleLine().FontSize(t.FontSize*0.75).FontWeight(600).
				TextColor(fg).Background(bg).Padding(0, t.Space(0.75)).Radius(t.Space(0.75))
		}
		text := ui.Text(c, result.text).SingleLine()
		if latest {
			text.FontWeight(600)
		}
		switch result.tone {
		case recordToneMuted:
			text.TextColor(colorWhenChosen(chosen, tok.TextMuted, t.AccentText))
		case recordToneWarning:
			text.TextColor(colorWhenChosen(chosen, tok.WarningFG, t.AccentText))
		case recordToneDanger:
			text.TextColor(colorWhenChosen(chosen, t.Danger, t.AccentText))
		}
	})
}

// historyNumberCell draws one integer column. unreadable renders the dash
// a damaged record needs; strong marks the latest row.
func (m *Model) historyNumberCell(c *ui.Context, t *ui.Theme, tok *Tokens, entry HistoryEntry, count int64, unreadable, strong, chosen bool) {
	if unreadable {
		ui.Text(c, "—").TextColor(colorWhenChosen(chosen, tok.TextMuted, t.AccentText))
		return
	}
	text := ui.Text(c, formatCount(count)).SingleLine()
	if strong && count != 0 {
		text.FontWeight(600)
	}
}

// historyPendingCell is 待处理: backfill plus review. A scan that captured
// nothing has no pending work to count, so it shows the design's dash.
func (m *Model) historyPendingCell(c *ui.Context, t *ui.Theme, tok *Tokens, entry HistoryEntry, chosen bool) {
	if entry.Unreadable || entry.CapturedCount == 0 {
		ui.Text(c, "—").TextColor(colorWhenChosen(chosen, tok.TextMuted, t.AccentText))
		return
	}
	pending := entry.Counts.Pending()
	text := ui.Text(c, formatCount(pending)).SingleLine()
	if pending > 0 {
		text.TextColor(colorWhenChosen(chosen, tok.WarningFG, t.AccentText)).FontWeight(600)
	}
}

// historyMergedCountsCell is the compact tier's "待处理 / 可导出".
func (m *Model) historyMergedCountsCell(c *ui.Context, t *ui.Theme, tok *Tokens, entry HistoryEntry, latest, chosen bool) {
	if entry.Unreadable {
		ui.Text(c, "—").TextColor(colorWhenChosen(chosen, tok.TextMuted, t.AccentText))
		return
	}
	pending := "—"
	if entry.CapturedCount > 0 {
		pending = formatCount(entry.Counts.Pending())
	}
	text := ui.Text(c, pending+" / "+formatCount(entry.Counts.Exportable())).SingleLine()
	if latest {
		text.FontWeight(600)
	}
}

// historySizeCell is 占用. A record stays measurable even when its
// checkpoint cannot be read, which the wireframe's damaged row shows.
func (m *Model) historySizeCell(c *ui.Context, t *ui.Theme, tok *Tokens, entry HistoryEntry, chosen bool) {
	ui.Text(c, formatBytes(entry.TotalBytes)).SingleLine().
		TextColor(colorWhenChosen(chosen, tok.TextMuted, t.AccentText))
}

// historyActionCell is the row's ⋯ trigger. 打开 is not an inline button:
// the row itself opens on Enter or a double click (08 §5).
func (m *Model) historyActionCell(c *ui.Context, t *ui.Theme, tok *Tokens, entry HistoryEntry, row int) {
	menu := ui.ButtonBase(c).Label("更多操作 "+historyTimeText(entry)).
		Padding(t.Space(0.5), t.Space(1)).Height(t.Space(6))
	if menu.Hovered() {
		menu.Background(t.SurfaceHover)
	}
	menu.Children(func() { ui.Text(c, "⋯").SingleLine() })
	if menu.Clicked() {
		m.historyRow = row
		m.toggleHistoryMenu(entry.ID)
	}
	if m.historyMenuID == entry.ID && m.historyMenuOpen {
		m.historyRowMenu(c, t, tok, menu, entry)
	}
}

// toggleHistoryMenu opens the row menu, or closes it when its trigger is
// pressed again.
func (m *Model) toggleHistoryMenu(id string) {
	if m.historyMenuID == id && m.historyMenuOpen {
		m.closeHistoryMenu()
		return
	}
	m.openHistoryMenu(id)
}

// historyRowMenu is the in-window row menu. It is drawn in the app rather
// than as a native menu so destructive items use the danger color and a
// disabled item can explain why. PopoverBase closes on Escape or a press
// outside and gives the focus back to ⋯.
func (m *Model) historyRowMenu(c *ui.Context, t *ui.Theme, tok *Tokens, anchor ui.Element, entry HistoryEntry) {
	ui.PopoverBase(c, anchor, &m.historyMenuOpen, func(panel ui.Element) {
		panel.MinWidth(t.Space(45)).Background(tok.SurfaceOverlay).Border(1, tok.BorderDefault).
			Radius(t.Space(1.5)).Padding(t.Space(1)).Gap(t.Space(0.5)).
			Shadow(0, 8, 24, 0, tok.Scrim)
		// The menu opens at the row's right edge so the last column keeps
		// it inside the window.
		panel.AttachTo(anchor, ui.AnchorBottomEnd, ui.AnchorTopEnd)
		panel.Children(func() {
			if entry.Unreadable {
				// A damaged record has no readable checkpoint to open or
				// export: deletion is its only action (F-25).
				ui.Text(c, "记录不可读，只能删除。").TextColor(tok.TextMuted).FontSize(t.FontSize * 0.9)
				ui.Divider(c)
				reason := m.historyDeleteBlockedReason(entry)
				color := t.Danger
				if reason != "" {
					color = tok.TextMuted
				}
				m.historyMenuItem(c, t, "删除记录…", color, reason != "", true, func() {
					m.RequestRecordCleanup(entry.ID)
				})
				if reason != "" {
					ui.Text(c, reason).MaxWidth(t.Space(60)).FontSize(t.FontSize * 0.8).
						TextColor(tok.TextMuted)
				}
				return
			}
			m.historyMenuItem(c, t, "打开", tok.TextPrimary, false, true, func() {
				m.openHistoryEntry(entry)
			})
			exportReason := m.historyExportBlockedReason(entry)
			exportColor := tok.TextPrimary
			if exportReason != "" {
				exportColor = tok.TextMuted
			}
			m.historyMenuItem(c, t, "导出…", exportColor, exportReason != "", false, func() {
				m.openHistoryExport(entry)
			})
			ui.Divider(c)
			screenshotLabel := "删除截图…"
			if historyCompact(m.contentWidth) {
				// Compact hides 占用; its number stays in the delete item.
				screenshotLabel += "（" + formatBytes(entry.TotalBytes) + "）"
			}
			reason := m.historyDeleteBlockedReason(entry)
			deleteColor := t.Danger
			if reason != "" {
				deleteColor = tok.TextMuted
			}
			m.historyMenuItem(c, t, screenshotLabel, deleteColor, reason != "", false, func() {
				m.RequestScreenshotCleanup(entry.ID)
			})
			m.historyMenuItem(c, t, "删除记录…", deleteColor, reason != "", false, func() {
				m.RequestRecordCleanup(entry.ID)
			})
			if exportReason != "" {
				ui.Text(c, exportReason).MaxWidth(t.Space(60)).FontSize(t.FontSize * 0.8).
					TextColor(tok.TextMuted)
			}
			if reason != "" {
				ui.Text(c, reason).MaxWidth(t.Space(60)).FontSize(t.FontSize * 0.8).
					TextColor(tok.TextMuted)
			}
		})
	})
}

// historyMenuItem is one flat menu row. first puts the keyboard focus on
// the first item when the menu opens, so Tab/Enter can drive the menu.
func (m *Model) historyMenuItem(c *ui.Context, t *ui.Theme, label string, color ui.Color, disabled, first bool, action func()) {
	item := ui.ButtonBase(c).Label(label).Justify(ui.Start).
		Padding(t.Space(1.5), t.Space(2)).Radius(t.Radius).Shrink(0)
	if item.Hovered() && !disabled {
		item.Background(t.SurfaceHover)
	}
	item.Disabled(disabled)
	item.Children(func() {
		ui.Text(c, label).SingleLine().TextColor(color)
	})
	if first {
		item.AutoFocus()
	}
	if item.Clicked() && !disabled {
		m.closeHistoryMenu()
		m.historyMenuID = ""
		action()
	}
}

// openHistoryMenu opens the row menu for one record.
func (m *Model) openHistoryMenu(id string) {
	if id == "" {
		return
	}
	m.historyMenuID = id
	m.historyMenuOpen = true
}

func (m *Model) closeHistoryMenu() {
	m.historyMenuOpen = false
}

// openSelectedHistoryMenu opens the menu of the chosen row, for the
// keyboard's Shift+F10 and menu key.
func (m *Model) openSelectedHistoryMenu() {
	entries := m.visibleHistory()
	if m.historyRow < 0 || m.historyRow >= len(entries) {
		return
	}
	m.openHistoryMenu(entries[m.historyRow].ID)
}

// historyDeleteBlockedReason explains why a row's deletion entries are
// disabled. The live session's record is never deletable while it is the
// current session, even after the scan finished: the current session owns
// it until a new session replaces it.
func (m *Model) historyDeleteBlockedReason(entry HistoryEntry) string {
	if entry.ID != "" && entry.ID == string(m.appliedSession) {
		return "当前会话记录，扫描中或未转移前不能删除。"
	}
	return m.cleanupBlockedReason()
}

// historyExportBlockedReason explains why the row's 导出… entry is
// disabled. It mirrors historyExportsBlocked's shared guards.
func (m *Model) historyExportBlockedReason(entry HistoryEntry) string {
	switch {
	case entry.Unreadable:
		return "记录不可读，不能导出。"
	case !m.exportAvailable():
		return "当前构建未启用导出。"
	case m.historyExporting != "":
		return "正在导出历史记录，完成前不能导出。"
	case m.exporting:
		return "正在导出，完成前不能导出。"
	case m.exitState == exitRunning || m.exitState == exitDone:
		return "正在退出应用，不能导出。"
	case m.reviewSaving:
		return "正在保存修改，完成前不能导出。"
	case m.editingDirty():
		return "有未保存的修改，保存或放弃后可导出。"
	default:
		return ""
	}
}

// openHistoryExport is the existing export entry this PR calls. The export
// dialog arrives with PR-15; until then 导出… opens the record's summary,
// where the current export controls live.
func (m *Model) openHistoryExport(entry HistoryEntry) {
	if entry.Unreadable || m.historyExportBlockedReason(entry) != "" {
		return
	}
	m.openHistoryEntry(entry)
	m.revealExports()
}

// historyNotices shows the cleanup outcome and a pending per-record
// cleanup confirmation.
func (m *Model) historyNotices(c *ui.Context, t *ui.Theme) {
	if m.cleanupNotice != "" {
		ui.Text(c, m.cleanupNotice)
	}
	if m.cleanup != nil && !m.cleanup.target.all() {
		m.cleanupPromptArea(c, t)
	}
}

// historyStatusText is the status bar's line on the history destination.
func (m *Model) historyStatusText() string {
	entries := m.visibleHistory()
	text := fmt.Sprintf("%d 条记录", len(entries))
	if m.historyRow >= 0 && m.historyRow < len(entries) {
		text += " · 已选中 " + historyTimeText(entries[m.historyRow])
	}
	return text
}

// historyRowLabel is the assistive-technology name of one row: the same
// conclusion and accounting the visible cells carry.
func historyRowLabel(m *Model, entry HistoryEntry) string {
	if entry.Unreadable {
		return historyTimeText(entry) + " · 记录不可读"
	}
	result := m.historyResultFor(entry)
	label := historyTimeText(entry) + " · " + result.text
	if entry.CapturedCount > 0 {
		label += fmt.Sprintf(" · 件数 %s · 待处理 %s · 可导出 %s",
			formatCount(entry.CapturedCount), formatCount(entry.Counts.Pending()),
			formatCount(entry.Counts.Exportable()))
	}
	return label
}

// historyTimeText is the table's local time in the design's wording
// (09 §5.3): 11 月 3 日 21:40.
func historyTimeText(entry HistoryEntry) string {
	if entry.SavedAt.IsZero() {
		return "日期未知"
	}
	return entry.SavedAt.In(time.Local).Format("1 月 2 日 15:04")
}

// colorWhenChosen keeps secondary text readable on the accent-filled
// selected row: the design requires pure white there, not the muted role.
func colorWhenChosen(chosen bool, muted, onAccent ui.Color) ui.Color {
	if chosen {
		return onAccent
	}
	return muted
}

// formatCount writes an integer with thousands separators, as the design's
// 1,008.
func formatCount(n int64) string {
	sign := ""
	if n < 0 {
		sign, n = "-", -n
	}
	digits := strconv.FormatInt(n, 10)
	var b strings.Builder
	for i, r := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	return sign + b.String()
}

// openHistoryRow opens the visible row the table selected.
func (m *Model) openHistoryRow(row int) {
	entries := m.visibleHistory()
	if row < 0 || row >= len(entries) {
		return
	}
	m.historyRow = row
	m.openHistoryEntry(entries[row])
}

// openHistoryEntry routes the row's 打开 action through the merged record
// workspace: "本次" and every other row open the same place, and a record
// already open keeps its state. A damaged record cannot be opened; its row
// menu only deletes it.
func (m *Model) openHistoryEntry(entry HistoryEntry) {
	m.closeHistoryMenu()
	m.historyMenuID = ""
	if entry.Unreadable {
		return
	}
	m.histChoice = entry.Label
	m.OpenHistory()
}
