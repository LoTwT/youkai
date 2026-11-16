package ui

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/LoTwT/youkai/internal/record"
	"github.com/egoist/mygo/ui"
	"github.com/lobehub/lobe-mygo/pkg/lucide"
)

// Keep native Table's shared header/cell padding aligned with the v5 grid.
// MyGo 0.9.0 supplies 10 horizontal / 6 vertical DIP per cell; the body
// adds 2 and the header sets its horizontal padding to 12.
const (
	historyPanelInset = 24
	historyCellInset  = 12
	historyCellExtra  = 2
)

var (
	historyWarningIcon     = lucide.AlertTriangle()
	historyErrorIcon       = lucide.CircleX()
	historyCompletedIcon   = lucide.CircleCheck()
	historyRunningIcon     = lucide.LoaderCircle()
	historyStoppedIcon     = lucide.Square()
	historyPendingIcon     = lucide.ClipboardList()
	historyInterruptedIcon = lucide.CirclePause()
	historyUnreadableIcon  = lucide.FileX2()
	historySaveFailedIcon  = lucide.SaveOff()
	historyEmptyIcon       = lucide.PackageOpen()
	historyUnknownIcon     = lucide.CircleHelp()
)

// historyColumns measures the effective system font, not the unscaled
// window width. Only a few representative strings are shaped, even in a
// large history. The result uses the remaining width and may wrap, while
// timestamps and numbers keep their complete values.
func (m *Model) historyColumns(c *ui.Context, entries []HistoryEntry) []ui.TableColumn {
	t := c.Theme()
	measure := func(text string, size float32) float32 {
		w, _ := c.MeasureText(0, ui.Span{Text: text, Size: size, Weight: 600, Features: "tnum"})
		return w
	}
	round := func(v float32) float32 { return float32(math.Ceil(float64(v)/4)) * 4 }
	columnWidth := func(minimum float32, labels ...string) float32 {
		w := minimum
		for _, label := range labels {
			w = max(w, round(measure(label, t.FontSize)+2*historyCellInset))
		}
		return w
	}
	var largestCount, largestBytes int64
	for _, entry := range entries {
		largestCount = max(largestCount, entry.CapturedCount, entry.Counts.Pending(), entry.Counts.Exportable())
		largestBytes = max(largestBytes, entry.TotalBytes)
	}
	// Reserve the widest tabular digits for the largest displayed count.
	digits := strings.Repeat("8", len(strconv.FormatInt(largestCount, 10)))
	number, _ := strconv.ParseInt(digits, 10, 64)
	count := columnWidth(72, "待处理", formatCount(number))
	timeWidth := round(max(200, measure("12 月 28 日 23:59", t.FontSize)+8+measure("本次", t.FontSize*12/14)+2*historyCellInset))
	resultWidth := columnWidth(240, "正常结束 · 没有采集到驱动盘")
	// A smaller value just below a unit boundary can be wider than the
	// largest value (1024.0 MB versus 2.0 GB after decimal rounding).
	size := columnWidth(88, "1024.0 KB", "1024.0 MB", "1024.0 GB", "1024.0 TB", formatBytes(largestBytes))
	const action = 48
	available := float32(m.contentWidth) - m.railWidth() - m.pageLeftPadding() - m.pagePadding() - 2*(historyPanelInset+1)
	m.historyCompactLayout = m.contentWidth < 1120 || available < timeWidth+resultWidth+3*count+size+action
	columns := []ui.TableColumn{
		{Title: "时间", Width: timeWidth, Fixed: true},
		{Title: "结果", MinWidth: resultWidth, Fixed: true},
		{Title: "件数", Width: count, Align: ui.End, Fixed: true},
	}
	if m.historyCompactLayout {
		merged := columnWidth(132, "待处理 / 可导出", formatCount(number)+" / "+formatCount(number))
		columns = append(columns, ui.TableColumn{Title: "待处理 / 可导出", Width: merged, Align: ui.End, Fixed: true})
	} else {
		columns = append(columns,
			ui.TableColumn{Title: "待处理", Width: count, Align: ui.End, Fixed: true},
			ui.TableColumn{Title: "可导出", Width: count, Align: ui.End, Fixed: true},
			ui.TableColumn{Title: "占用", Width: size, Align: ui.End, Fixed: true})
	}
	return append(columns, ui.TableColumn{ID: "actions", Width: action, Align: ui.End, Fixed: true})
}

// historyPage is the records destination: the title, the date filter, the
// record table and its row menu. The newest record is first and carries
// its conclusion in the 结果 column, so a returning user finds last time's
// scan without reading counts (07 §4, 08 §5). The live session appears as
// the first row with the 本次 badge; it opens the same record workspace as
// any other row, so one record never has two editable copies.
func (m *Model) historyPage(c *ui.Context, t *ui.Theme) {
	tok := m.effectiveTokens()
	m.historyHeader(c, t, tok)
	m.historyNotices(c, t)
	contentPanel(c, tok).Padding(historyPanelInset).Children(func() {
		m.historyContent(c, t, tok)
	})
}

func (m *Model) historyContent(c *ui.Context, t *ui.Theme, tok *Tokens) {
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

	columns := m.historyColumns(c, entries)
	var table ui.Element
	ui.Themed(c, m.theme.table.Theme(t.Dark)).Background(ui.Transparent).Fill().MinHeight(0).Children(func() {
		table = ui.Table(c, &m.historyList, columns, len(entries), func(row, col int) {
			if row < 0 || row >= len(entries) || col < 0 || col >= len(columns) {
				return
			}
			column := columns[col]
			ui.Row(c).FillWidth().MinHeight(36).Padding(historyCellExtra).AlignItems(ui.Center).Justify(column.Align).Children(func() {
				m.historyCell(c, c.Theme(), tok, entries[row], row, column)
			})
		}).Fill().MinHeight(0)
	})
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
}

// historyHeader is the page title and its date filter. The filter uses
// DateInput rather than a typed date (M6); 清除筛选 returns the range to
// every record.
func (m *Model) historyHeader(c *ui.Context, t *ui.Theme, tok *Tokens) {
	m.settleHistoryFilterRange()
	pageHeader(c).Children(func() {
		pageTitle(c, "记录")
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
			if button(c, "清除筛选").Clicked() {
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
			if button(c, "清除筛选").Clicked() {
				m.resetHistoryFilter()
			}
			return
		}
		ui.Text(c, "还没有扫描记录").TextColor(tok.TextMuted)
		if button(c, "去扫描页开始扫描").Clicked() {
			m.showScanDestination()
		}
	})
}

// prepareHistoryList gives the table its stable identity and the row's
// assistive-technology name. The key is the record ID, so a reload or a
// filter change keeps the scroll anchored to a record rather than a row.
func (m *Model) prepareHistoryList(entries []HistoryEntry) {
	m.historyList.Selected = &m.historyRow
	m.historyList.HeaderCell = func(cell ui.Element, _ int) { cell.Padding(4, historyCellInset) }
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
	cell := ui.Row(c).Height(t.Space(8)).AlignItems(ui.Center)
	if chosen {
		cell.DrawOver(func(p *ui.Painter, r ui.Rect) {
			p.Fill(ui.Rect{X: r.X - historyCellInset, Y: r.Y, W: 4, H: r.H}, tok.RowSelectedBorder, 0)
		})
	}
	cell.Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			text := ui.Text(c, historyTimeText(entry)).SingleLine().Shrink(0).FontFeatures("tnum")
			if latest && !entry.Unreadable {
				text.FontWeight(600)
			}
			if entry.Unreadable {
				// 线框 10 keeps a damaged row's time muted: its reason, not
				// its date, is what the user needs from the row.
				text.TextColor(colorWhenChosen(chosen, tok.TextMuted, tok.TextPrimary))
			}
			if entry.ID != "" && entry.ID == string(m.appliedSession) {
				ui.Text(c, "本次").SingleLine().Shrink(0).FontSize(t.FontSize * 12 / 14).TextColor(tok.TextSecondary)
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
		Interrupted:              recordConclusionInterrupted(entry.Termination, entry.Interrupted, active),
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

// The list needs a gold/red hue distinction in addition to contrast. Keep
// this pair local: other screens still consume the shared semantic tokens.
func historyResultInks(tok *Tokens, dark bool) (warning, danger ui.Color) {
	if dark {
		return tok.WarningStrong, tok.DangerStrong
	}
	return hex("#856500"), hex("#C5221F")
}

// Icons consume the existing outcome and facts, never localized wording.
// Empty records retain their real termination, including manual stops.
func historyResultIcon(out recordOutcome, result recordConclusion) (*ui.SVG, string) {
	kind, in := out.Kind, out.Input
	if kind == recordOutcomeEmpty {
		switch {
		case in.Interrupted:
			kind = recordOutcomeInterrupted
		case in.Termination == nil:
			kind = recordOutcomeUnknown
		default:
			switch in.Termination.Kind {
			case record.TerminationInterrupted:
				kind = recordOutcomeInterrupted
			case record.TerminationCancelled:
				kind = recordOutcomeCancelled
			case record.TerminationFailed:
				kind = recordOutcomeFault
			case record.TerminationCompleted:
				return historyEmptyIcon, "未采集"
			default:
				kind = recordOutcomeUnknown
			}
		}
	}
	switch kind {
	case recordOutcomeUnreadable:
		return historyUnreadableIcon, "记录不可读"
	case recordOutcomeSaveFailed:
		return historySaveFailedIcon, "保存失败"
	case recordOutcomeRunning:
		return historyRunningIcon, "进行中"
	case recordOutcomeInterrupted:
		return historyInterruptedIcon, "中断"
	case recordOutcomeFault:
		return historyErrorIcon, "错误"
	case recordOutcomeCancelled:
		return historyStoppedIcon, "手动停止"
	case recordOutcomeCompleted:
		switch {
		case result.tone == recordToneWarning:
			return historyWarningIcon, "警告"
		case result.tone == recordToneMuted:
			return historyUnknownIcon, "覆盖未知"
		case in.Counts.Pending() > 0:
			return historyPendingIcon, "待处理"
		default:
			return historyCompletedIcon, "完成"
		}
	default:
		return historyUnknownIcon, "状态未知"
	}
}

// historyResultCell draws the tag and the conclusion.
func (m *Model) historyResultCell(c *ui.Context, t *ui.Theme, tok *Tokens, entry HistoryEntry, latest, chosen bool) {
	out := recordOutcomeFor(m.historyConclusionInput(entry))
	result := recordListConclusion(out)
	warning, danger := historyResultInks(tok, t.Dark)
	fg, bg := warning, tok.WarningSoft
	if result.tone == recordToneDanger {
		fg, bg = danger, tok.DangerSoft
	}
	semantic := result.tone == recordToneWarning || result.tone == recordToneDanger
	ink := colorWhenChosen(chosen, tok.TextSecondary, tok.TextPrimary)
	if semantic {
		ink = fg
	} else if result.tone == recordToneMuted {
		ink = colorWhenChosen(chosen, tok.TextMuted, tok.TextPrimary)
	}
	icon, label := historyResultIcon(out, result)
	ui.Row(c).FillWidth().MinWidth(0).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, icon).Size(20, 20).Shrink(0).TextColor(ink).Role(ui.RoleImage).Label(label)
		text := ui.Text(c, result.text).MinWidth(0)
		if latest || semantic {
			text.FontWeight(600)
		}
		switch result.tone {
		case recordToneMuted:
			text.TextColor(colorWhenChosen(chosen, tok.TextMuted, tok.TextPrimary))
		case recordToneWarning, recordToneDanger:
			text.TextColor(fg)
		}
		if result.tag != "" {
			ui.Text(c, result.tag).SingleLine().Shrink(0).FontSize(t.FontSize*12/14).FontWeight(600).
				TextColor(fg).Background(bg).Padding(0, 4).Radius(4)
		}
	})
}

// historyNumberCell draws one integer column. unreadable renders the dash
// a damaged record needs; strong marks the latest row.
func (m *Model) historyNumberCell(c *ui.Context, t *ui.Theme, tok *Tokens, entry HistoryEntry, count int64, unreadable, strong, chosen bool) {
	if unreadable {
		ui.Text(c, "—").TextColor(colorWhenChosen(chosen, tok.TextMuted, tok.TextPrimary))
		return
	}
	text := ui.Text(c, formatCount(count)).SingleLine().FontFeatures("tnum")
	if strong && count != 0 {
		text.FontWeight(600)
	}
}

// historyPendingCell is 待处理: backfill plus review. A scan that captured
// nothing has no pending work to count, so it shows the design's dash.
func (m *Model) historyPendingCell(c *ui.Context, t *ui.Theme, tok *Tokens, entry HistoryEntry, chosen bool) {
	if entry.Unreadable || entry.CapturedCount == 0 {
		ui.Text(c, "—").TextColor(colorWhenChosen(chosen, tok.TextMuted, tok.TextPrimary))
		return
	}
	pending := entry.Counts.Pending()
	text := ui.Text(c, formatCount(pending)).SingleLine().FontFeatures("tnum")
	if pending > 0 {
		warning, _ := historyResultInks(tok, t.Dark)
		text.TextColor(warning).FontWeight(600)
	}
}

// historyMergedCountsCell is the compact tier's "待处理 / 可导出".
func (m *Model) historyMergedCountsCell(c *ui.Context, t *ui.Theme, tok *Tokens, entry HistoryEntry, latest, chosen bool) {
	if entry.Unreadable {
		ui.Text(c, "—").TextColor(colorWhenChosen(chosen, tok.TextMuted, tok.TextPrimary))
		return
	}
	pending := "—"
	if entry.CapturedCount > 0 {
		pending = formatCount(entry.Counts.Pending())
	}
	text := ui.Text(c, pending+" / "+formatCount(entry.Counts.Exportable())).SingleLine().FontFeatures("tnum")
	if latest {
		text.FontWeight(600)
	}
}

// historySizeCell is 占用. A record stays measurable even when its
// checkpoint cannot be read, which the wireframe's damaged row shows.
func (m *Model) historySizeCell(c *ui.Context, t *ui.Theme, tok *Tokens, entry HistoryEntry, chosen bool) {
	ui.Text(c, formatBytes(entry.TotalBytes)).SingleLine().FontFeatures("tnum").
		TextColor(colorWhenChosen(chosen, tok.TextMuted, tok.TextPrimary))
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
				m.historyMenuItem(c, t, "删除记录", color, reason != "", true, func() {
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
			m.historyMenuItem(c, t, "导出", exportColor, exportReason != "", false, func() {
				m.openHistoryExport(entry)
			})
			ui.Divider(c)
			screenshotLabel := "删除截图"
			if m.historyCompactLayout {
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
			m.historyMenuItem(c, t, "删除记录", deleteColor, reason != "", false, func() {
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
// disabled. A dirty review draft is not a blocker here: choosing 导出…
// first opens the target record and lets the existing save/discard prompt
// settle that draft, just as opening the record would.
func (m *Model) historyExportBlockedReason(entry HistoryEntry) string {
	switch {
	case entry.Unreadable:
		return "记录不可读，不能导出。"
	case !m.exportAvailable():
		return "当前构建未启用导出。"
	case m.exporting:
		return "正在导出，完成前不能导出。"
	case m.exitState == exitRunning || m.exitState == exitDone:
		return "正在退出应用，不能导出。"
	case m.reviewSaving:
		return "正在保存修改，完成前不能导出。"
	case m.cleanupRunning():
		return "正在清理，完成前不能导出。"
	default:
		return ""
	}
}

// openHistoryExport opens the target record and then its one export
// dialog; a different record's export never writes from the list row.
// Opening the record through requestNavigation keeps the existing
// save/discard prompt in front of a dirty draft.
func (m *Model) openHistoryExport(entry HistoryEntry) {
	if entry.Unreadable || m.historyExportBlockedReason(entry) != "" {
		return
	}
	m.closeHistoryMenu()
	m.historyMenuID = ""
	m.histChoice = entry.Label
	if m.recordIsOpen(entry.ID) && m.recErr == "" {
		m.page = pageResults
		// The target is already the open record; the title bar's own
		// export entry handles its draft and blocked states.
		m.clearRecentlyConfirmed()
		m.settleResultSelection()
		m.requestExport()
		return
	}
	m.requestNavigation(navigationRequest{
		kind:            navOpenRecord,
		recordID:        entry.ID,
		exportAfterLoad: true,
	})
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

// colorWhenChosen keeps special cell text readable on the selected object.
func colorWhenChosen(chosen bool, normal, selected ui.Color) ui.Color {
	if chosen {
		return selected
	}
	return normal
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
