package ui

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/egoist/mygo/ui"
)

// The history table's responsive rules. The wide table keeps the design's
// eight columns; the narrow one drops storage size. Below the panel width
// the 1020 layout needs, the fixed action column is composed outside the
// native table, because MyGo's Table scrolls every column together (W1).
const (
	historyWideMinWidth     = 1440
	historyMinPanelWidth    = 810
	historyMinTableWidth860 = 776
	historyRowHeight860     = 52
	historyActionWidth860   = 84
	historyCompositePad     = 12
	historyCompositeGap     = 8
	historyCompositeHeader  = 36
	historyEmptyCell        = "—"
)

// historyColumnsWide is the 1440 table: 时间 / 采集 / 可导出 / 待校对 /
// 无法导出 / 覆盖 / 大小 / 操作. Its fixed widths add up to the design's
// 994-DIP minimum; 覆盖 takes the room left.
var historyColumnsWide = []ui.TableColumn{
	{Title: "时间", Width: 124, Fixed: true},
	{Title: "采集", Width: 120, Align: ui.End, Fixed: true},
	{Title: "可导出", Width: 100, Align: ui.End, Fixed: true},
	{Title: "待校对", Width: 100, Align: ui.End, Fixed: true},
	{Title: "无法导出", Width: 210, Align: ui.End, Fixed: true},
	{Title: "覆盖", MinWidth: 160, Fixed: true},
	{Title: "大小", Width: 80, Align: ui.End, Fixed: true},
	{Title: "操作", Width: 100, Align: ui.End, Fixed: true},
}

// historyColumnsNarrow is the 1020 and 860 table. Storage size belongs to
// the settings page at these widths; the source column is not shown at any
// width. "无法导出" stays a first-class column.
var historyColumnsNarrow = []ui.TableColumn{
	{Title: "时间", Width: 120, Fixed: true},
	{Title: "采集", Width: 90, Align: ui.End, Fixed: true},
	{Title: "可导出", Width: 76, Align: ui.End, Fixed: true},
	{Title: "待校对", Width: 76, Align: ui.End, Fixed: true},
	{Title: "无法导出", Width: 160, Align: ui.End, Fixed: true},
	{Title: "覆盖", MinWidth: 120, Fixed: true},
	{Title: "操作", Width: 84, Align: ui.End, Fixed: true},
}

// historyColumns860 mirrors the 860 mockup's data columns and the fixed
// action column. The composite lays these out itself so the action column
// can stay at the right edge while the data columns scroll.
var historyColumns860 = []ui.TableColumn{
	{Title: "时间", Width: 116, Fixed: true},
	{Title: "采集", Width: 88, Align: ui.End, Fixed: true},
	{Title: "可导出", Width: 72, Align: ui.End, Fixed: true},
	{Title: "待校对", Width: 72, Align: ui.End, Fixed: true},
	{Title: "无法导出", Width: 152, Align: ui.End, Fixed: true},
	{Title: "覆盖", Width: 120, Fixed: true},
	{Title: "操作", Width: historyActionWidth860, Align: ui.End, Fixed: true},
}

// historyColumnsFor returns the native table columns for a content width:
// eight at 1440 and above, seven below it.
func historyColumnsFor(contentWidth int) []ui.TableColumn {
	if contentWidth >= historyWideMinWidth {
		return historyColumnsWide
	}
	return historyColumnsNarrow
}

// historyPanelWidth estimates the room the page body gives the table: the
// content width minus the rail, its divider and the page padding. It is
// the viewport the composite pins its action column against.
func historyPanelWidth(contentWidth int) float32 {
	sidebar := float32(sidebarWideWidth)
	if contentWidth < sidebarNarrowMaxSize {
		sidebar = sidebarNarrowWidth
	}
	width := float32(contentWidth) - sidebar - 1 - 2*historyCompositePad
	if width < 1 {
		width = 1
	}
	return width
}

// historyUsesComposite reports whether the panel is too narrow for the
// native seven-column table. The native table scrolls all columns together,
// so the fixed action column needs the composite at 860.
func historyUsesComposite(contentWidth int) bool {
	return historyPanelWidth(contentWidth) < historyMinPanelWidth
}

// historyPage is the saved-records destination: the record table, its date
// search and the per-row actions. The live session appears as the first
// row with the 本次 badge; it opens back into the session slot instead of
// creating a second editable copy.
func (m *Model) historyPage(c *ui.Context, t *ui.Theme) {
	_, tok := themeWithTokens(t, c.Preferences())
	entries := m.visibleHistory()
	m.settleHistoryRow(len(entries))
	m.prepareHistoryList(entries)

	ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "历史记录").FontWeight(600)
		subtitle := fmt.Sprintf("%d 条记录", len(m.history))
		if len(m.history) > 0 {
			subtitle += " · 最近 " + historyTimeText(m.history[0])
		}
		ui.Text(c, subtitle).TextColor(tok.TextMuted).FontSize(t.FontSize * 0.9)
		ui.Box(c).Grow(1)
		if m.cfg.OpenDataDirectory != nil {
			if ui.Button(c, "打开数据目录").Clicked() {
				m.OpenDataDirectory()
			}
		}
	})
	ui.Text(c, "已保存的扫描修订；打开记录后回到结果与校对。").TextColor(tok.TextMuted).
		FontSize(t.FontSize * 0.9)

	ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "搜索日期").TextColor(tok.TextMuted)
		m.noteTextInputFocus(ui.TextInput(c, &m.historyFilter).Width(160).Placeholder("YYYY-MM-DD"))
		if m.historyFilterInvalid() {
			ui.Text(c, "格式应为 YYYY-MM-DD。").TextColor(t.Danger).FontSize(t.FontSize * 0.9)
		}
	})
	m.historyNotices(c, t, tok)

	switch {
	case m.histErr != "":
		ui.Text(c, "历史记录读取失败："+m.histErr).TextColor(t.Danger)
		return
	case len(m.history) == 0:
		ui.Text(c, "暂无已保存的记录。").TextColor(tok.TextMuted)
		return
	case len(entries) == 0:
		ui.Text(c, m.historyEmptyText()).TextColor(tok.TextMuted)
		return
	}

	ui.Column(c).Grow(1).Width(historyPanelWidth(m.contentWidth)).MinWidth(0).
		Background(tok.SurfaceCard).Border(1, tok.TableBorder).
		Radius(t.Space(1.5)).Clip().Bind(&m.historyPanel).Children(func() {
		if historyUsesComposite(m.contentWidth) {
			m.historyCompositeTable(c, t, tok, entries)
		} else {
			m.historyNativeTable(c, t, tok, entries)
		}
		m.historyTableFooter(c, t, tok, entries)
	})
}

// prepareHistoryList gives the table its stable identity, accessibility
// labels and single-row choice. The key is the record ID, so a reload or a
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
		return historyRowLabel(entries[row])
	}
}

// settleHistoryRow keeps the choice inside the visible rows: after a date
// search narrows the list, or the first frame has no choice yet.
func (m *Model) settleHistoryRow(count int) {
	switch {
	case count == 0:
		m.historyRow = -1
	case m.historyRow < 0 || m.historyRow >= count:
		m.historyRow = 0
	}
}

// historyNativeTable is the 1440/1020 table. Every column, including the
// action column, fits the panel, so the toolkit's native horizontal
// behaviour is not exercised at these widths.
func (m *Model) historyNativeTable(c *ui.Context, t *ui.Theme, tok *Tokens, entries []HistoryEntry) {
	columns := historyColumnsFor(m.contentWidth)
	table := ui.Table(c, &m.historyList, columns, len(entries), func(row, col int) {
		if row < 0 || row >= len(entries) {
			return
		}
		entry := entries[row]
		chosen := m.historyRow == row
		switch columns[col].Title {
		case "时间":
			m.historyTimeCell(c, t, tok, entry, chosen)
		case "采集":
			m.historyNumberCell(c, entry.CapturedCount, entry.Unreadable, false, chosen)
		case "可导出":
			m.historyNumberCell(c, entry.ExportableCount, entry.Unreadable, true, chosen)
		case "待校对":
			m.historyPendingCell(c, t, entry, chosen)
		case "无法导出":
			m.historyBlockedCell(c, t, tok, entry, chosen)
		case "覆盖":
			m.historyCoverageCell(c, t, tok, entry, chosen)
		case "大小":
			m.historySizeCell(c, t, tok, entry, chosen)
		case "操作":
			m.historyActionCell(c, t, tok, entry, row)
		}
	}).Fill().MinHeight(0)
	if table.Submitted() {
		m.openHistoryRow(m.historyRow)
	}
}

// historyCompositeTable is the 860 table from W1's go report: a header and
// row list that scroll sideways together, with the action column offset
// back to the right edge on every row. The list keeps its own vertical
// scrolling, so rows snap and the header stays put.
func (m *Model) historyCompositeTable(c *ui.Context, t *ui.Theme, tok *Tokens, entries []HistoryEntry) {
	viewport := historyPanelWidth(m.contentWidth) - 2
	if viewport < 1 {
		viewport = 1
	}
	maxX := historyMinTableWidth860 - viewport
	if maxX < 0 {
		maxX = 0
	}
	actionLeft := m.historyScroll.X - maxX

	head := ui.Row(c).Height(historyCompositeHeader).Background(tok.TableHeader).BorderWidth(0, 0, 1, 0).
		BorderColor(tok.TableBorder).Clip().Shrink(0).MinWidth(0).Padding(0, historyCompositePad).
		Gap(historyCompositeGap).AlignItems(ui.Stretch).Bind(&m.historyHead).Children(func() {
		for _, column := range historyColumns860[:len(historyColumns860)-1] {
			m.historyCompositeCell(c, t, tok, HistoryEntry{}, column, true, false)
		}
		m.historyCompositeActionCell(c, t, tok, HistoryEntry{}, -1, actionLeft, true)
	})
	list := ui.List(c, &m.historyList, len(entries), func(row int) {
		if row < 0 || row >= len(entries) {
			return
		}
		entry := entries[row]
		chosen := m.historyRow == row
		ui.Row(c).Height(historyRowHeight860).AlignItems(ui.Stretch).
			Padding(0, historyCompositePad).Gap(historyCompositeGap).
			BorderWidth(0, 0, 1, 0).BorderColor(tok.TableBorder).Children(func() {
			for _, column := range historyColumns860[:len(historyColumns860)-1] {
				m.historyCompositeCell(c, t, tok, entry, column, false, chosen)
			}
			m.historyCompositeActionCell(c, t, tok, entry, row, actionLeft, false)
		})
	}).MinWidth(0).RowWidth(historyMinTableWidth860).TrackScroll(&m.historyScroll)
	m.snapHistoryScroll(c, len(entries))
	if height, measured := m.historyCompositeListHeight(c); measured {
		// Rows are fixed 52-DIP boxes. An exact whole-row height keeps
		// the viewport from clipping the last row; the spacer holds the
		// footer at the panel's bottom.
		list.Height(height)
		ui.Box(c).Grow(1)
	} else {
		// Measure the panel/header/footer once with the table filling the
		// rest, then rebuild with the snapped viewport.
		list.Grow(1)
		c.Invalidate()
	}
	head.ScrollWith(list)
	if list.Submitted() {
		m.openHistoryRow(m.historyRow)
	}
}

// historySnapDelay is how long the composite waits after the last precise
// scroll move before snapping a small offset to the nearest row.
const historySnapDelay = 120 * time.Millisecond

// snapHistoryScroll aligns the composite's vertical offset with the
// 52-DIP row boundary a precise scroll can leave between rows. A trackpad
// delivers small moves, and a wheel glides through the offset over a few
// frames; in both cases the offset belongs to the input until the motion
// stops. Writing it mid-glide makes MyGo treat the app as taking over and
// cancels the rest of the wheel's travel, so snapping waits for the
// debounce and then moves to the nearest row. The header and the fixed
// action column follow only the horizontal offset and stay put.
func (m *Model) snapHistoryScroll(c *ui.Context, rows int) {
	if rows == 0 {
		return
	}
	y := m.historyScroll.Y
	if y < 0 {
		m.historyScroll.Y = 0
		y = 0
	}
	now := c.Now()
	if y != m.historyScrollSeen {
		// The offset is still moving: keep the full travel and snap only
		// after it has been still for the whole debounce.
		m.historyScrollSeen = y
		m.historyScrollMovedAt = now
		if m.historyScrollSnapAt.IsZero() || !now.Before(m.historyScrollSnapAt) {
			m.historyScrollSnapAt = now.Add(historySnapDelay)
		}
		c.After(historySnapDelay)
		return
	}
	if m.historyScrollSnapAt.IsZero() || now.Before(m.historyScrollSnapAt) {
		return
	}
	if wait := m.historyScrollMovedAt.Add(historySnapDelay).Sub(now); wait > 0 {
		m.historyScrollSnapAt = now.Add(wait)
		c.After(wait)
		return
	}
	m.historyScrollSnapAt = time.Time{}
	m.applyHistoryScrollSnap(historyRowOffset(y))
}

// historyRowOffset is the nearest 52-DIP boundary to y.
func historyRowOffset(y float32) float32 {
	return float32(math.Round(float64(y)/historyRowHeight860)) * historyRowHeight860
}

// applyHistoryScrollSnap moves the tracked offset and the list to one row
// boundary. MaxY keeps the end of the content in reach.
func (m *Model) applyHistoryScrollSnap(target float32) {
	if maxY := m.historyScroll.MaxY; maxY > 0 && target > maxY {
		target = maxY
	}
	if target < 0 {
		target = 0
	}
	m.historyScrollSeen = target
	if target == m.historyScroll.Y {
		return
	}
	m.historyScroll.Y = target
	m.historyList.ScrollTo(int(math.Round(float64(target)/historyRowHeight860)), ui.Start)
}

// historyCompositeListHeight returns the largest whole number of 52-DIP
// rows the composite's panel can show, after its header and footer. The
// measured flag is false until all three have a committed frame.
func (m *Model) historyCompositeListHeight(c *ui.Context) (height float32, measured bool) {
	panel := m.historyPanel.Bounds(c)
	head := m.historyHead.Bounds(c)
	footer := m.historyFooter.Bounds(c)
	if panel.H <= 0 || head.H <= 0 || footer.H <= 0 {
		return 0, false
	}
	// The panel's own 1-DIP border sits outside the children's frame.
	available := panel.H - 2 - head.H - footer.H
	if available < 0 {
		available = 0
	}
	rows := int(available / historyRowHeight860)
	return float32(rows) * historyRowHeight860, true
}

// historyCompositeCell is one fixed-width data cell of the 860 composite.
// The header and the rows share it so their columns can never drift apart.
func (m *Model) historyCompositeCell(c *ui.Context, t *ui.Theme, tok *Tokens, entry HistoryEntry, column ui.TableColumn, header, chosen bool) {
	cell := ui.Row(c).Width(column.Width).Shrink(0).Padding(t.Space(1), t.Space(1.5)).
		AlignItems(ui.Center)
	switch column.Align {
	case ui.End:
		cell.Justify(ui.End)
	case ui.Center:
		cell.Justify(ui.Center)
	}
	cell.Children(func() {
		if header {
			ui.Text(c, column.Title).SingleLine().FontWeight(600).
				TextColor(tok.TableHeaderText).FontSize(t.FontSize * 0.9)
			return
		}
		switch column.Title {
		case "时间":
			m.historyTimeContent(c, t, tok, entry, chosen)
		case "采集":
			m.historyNumberContent(c, entry.CapturedCount, entry.Unreadable, false, chosen)
		case "可导出":
			m.historyNumberContent(c, entry.ExportableCount, entry.Unreadable, true, chosen)
		case "待校对":
			m.historyPendingContent(c, t, entry, chosen)
		case "无法导出":
			m.historyBlockedContent(c, t, tok, entry, chosen)
		case "覆盖":
			m.historyCoverageContent(c, t, tok, entry, chosen)
		}
	})
}

// historyCompositeActionCell is the 860 fixed action column. The left
// offset cancels the list's horizontal scroll (and, at the first frame,
// the distance the action column naturally sits past the viewport), as the
// design's sticky right column. Its background is opaque so the data
// columns scroll underneath it.
func (m *Model) historyCompositeActionCell(c *ui.Context, t *ui.Theme, tok *Tokens, entry HistoryEntry, row int, left float32, header bool) {
	bg := tok.SurfaceCard
	divider := tok.TableBorder
	if header {
		bg = tok.TableHeader
	} else if row == m.historyRow {
		bg = t.Accent
		divider = t.AccentText
	}
	cell := ui.Row(c).Width(historyActionWidth860).Shrink(0).
		Padding(t.Space(1), t.Space(1)).AlignItems(ui.Center).Justify(ui.End).
		Background(bg).BorderWidth(0, 0, 0, 1).BorderColor(divider).Left(left)
	cell.Children(func() {
		if header {
			ui.Text(c, "操作").SingleLine().FontWeight(600).
				TextColor(tok.TableHeaderText).FontSize(t.FontSize * 0.9)
			return
		}
		m.historyActionContent(c, t, tok, entry, row)
	})
}

// historyTimeCell is the native table's first cell. It carries the row
// height the design asks for: 44 DIP at 1440 and 50 DIP at 1020.
func (m *Model) historyTimeCell(c *ui.Context, t *ui.Theme, tok *Tokens, entry HistoryEntry, chosen bool) {
	height := t.Space(8)
	if m.contentWidth < historyWideMinWidth {
		height = t.Space(9.5)
	}
	ui.Row(c).Height(height).AlignItems(ui.Center).Children(func() {
		m.historyTimeContent(c, t, tok, entry, chosen)
	})
}

func (m *Model) historyTimeContent(c *ui.Context, t *ui.Theme, tok *Tokens, entry HistoryEntry, chosen bool) {
	ui.Row(c).Gap(t.Space(1)).AlignItems(ui.Center).Children(func() {
		ui.Text(c, historyTimeText(entry)).SingleLine()
		if entry.ID != "" && entry.ID == string(m.appliedSession) {
			ui.Text(c, "本次").FontSize(t.FontSize*0.75).TextColor(tok.AccentFG).
				Background(tok.AccentSoft).Padding(0, t.Space(0.5)).Radius(t.Space(0.75))
		}
	})
}

func (m *Model) historyNumberCell(c *ui.Context, count int64, unreadable, strong, chosen bool) {
	m.historyNumberContent(c, count, unreadable, strong, chosen)
}

func (m *Model) historyNumberContent(c *ui.Context, count int64, unreadable, strong, chosen bool) {
	if unreadable {
		ui.Text(c, historyEmptyCell).TextColor(colorWhenChosen(chosen, c.Theme().TextMuted, c.Theme().AccentText))
		return
	}
	text := ui.Text(c, formatCount(count)).SingleLine()
	if strong {
		text.FontWeight(600)
	}
}

func (m *Model) historyPendingCell(c *ui.Context, t *ui.Theme, entry HistoryEntry, chosen bool) {
	m.historyPendingContent(c, t, entry, chosen)
}

func (m *Model) historyPendingContent(c *ui.Context, t *ui.Theme, entry HistoryEntry, chosen bool) {
	if entry.Unreadable {
		ui.Text(c, historyEmptyCell).TextColor(colorWhenChosen(chosen, t.TextMuted, t.AccentText))
		return
	}
	text := ui.Text(c, formatCount(int64(entry.PendingReview))).SingleLine()
	if entry.PendingReview > 0 {
		text.TextColor(colorWhenChosen(chosen, t.Warning, t.AccentText)).FontWeight(600)
	}
}

func (m *Model) historyBlockedCell(c *ui.Context, t *ui.Theme, tok *Tokens, entry HistoryEntry, chosen bool) {
	m.historyBlockedContent(c, t, tok, entry, chosen)
}

// historyBlockedContent is "无法导出": the total and the failed,
// unprocessed and other split, right aligned. It stays one column at every
// width.
func (m *Model) historyBlockedContent(c *ui.Context, t *ui.Theme, tok *Tokens, entry HistoryEntry, chosen bool) {
	subColor := colorWhenChosen(chosen, tok.TextMuted, t.AccentText)
	if entry.Unreadable {
		ui.Column(c).AlignItems(ui.End).Gap(t.Space(0.25)).Children(func() {
			ui.Text(c, historyEmptyCell).SingleLine()
			ui.Text(c, "记录不可读").SingleLine().FontSize(t.FontSize * 0.8).TextColor(subColor)
		})
		return
	}
	total := entry.FailedCount + entry.UnprocessedCount + entry.OtherCount
	ui.Column(c).AlignItems(ui.End).Gap(t.Space(0.25)).Children(func() {
		ui.Text(c, formatCount(total)).SingleLine()
		ui.Textf(c, "失败 %s · 未处理 %s · 其他 %s",
			formatCount(entry.FailedCount), formatCount(entry.UnprocessedCount), formatCount(entry.OtherCount)).
			SingleLine().FontSize(t.FontSize * 0.8).TextColor(subColor)
	})
}

func (m *Model) historyCoverageCell(c *ui.Context, t *ui.Theme, tok *Tokens, entry HistoryEntry, chosen bool) {
	m.historyCoverageContent(c, t, tok, entry, chosen)
}

// historyCoverageContent is the coverage bar and its number. A percentage
// appears only when the record knows the expected count (or states the
// coverage is complete); otherwise the status word is shown rather than a
// fabricated percentage.
func (m *Model) historyCoverageContent(c *ui.Context, t *ui.Theme, tok *Tokens, entry HistoryEntry, chosen bool) {
	if entry.Unreadable {
		ui.Text(c, "无法确认").TextColor(colorWhenChosen(chosen, tok.TextMuted, t.AccentText))
		return
	}
	percent, ok := historyCoveragePercent(entry)
	if !ok {
		ui.Text(c, coverageText(entry.Coverage)).TextColor(colorWhenChosen(chosen, tok.TextMuted, t.AccentText))
		return
	}
	partial := percent < 100
	bar := tok.BorderStrong
	textColor := tok.TextMuted
	if partial {
		bar = tok.WarningStrong
		textColor = tok.WarningFG
	}
	if chosen {
		bar = t.AccentText
		textColor = t.AccentText
	}
	label := fmt.Sprintf("%d%%", percent)
	if partial {
		label += " 部分"
	}
	ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Width(t.Space(12)).Height(t.Space(1.25)).Background(tok.SurfaceSunken).
			Radius(t.Space(0.75)).Draw(func(p *ui.Painter, r ui.Rect) {
			p.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W * float32(percent) / 100, H: r.H}, bar, t.Space(0.75))
		})
		ui.Text(c, label).SingleLine().TextColor(textColor)
	})
}

func (m *Model) historySizeCell(c *ui.Context, t *ui.Theme, tok *Tokens, entry HistoryEntry, chosen bool) {
	if entry.Unreadable {
		ui.Text(c, historyEmptyCell).TextColor(colorWhenChosen(chosen, tok.TextMuted, t.AccentText))
		return
	}
	ui.Text(c, formatBytes(entry.TotalBytes)).SingleLine().
		TextColor(colorWhenChosen(chosen, tok.TextMuted, t.AccentText))
}

func (m *Model) historyActionCell(c *ui.Context, t *ui.Theme, tok *Tokens, entry HistoryEntry, row int) {
	ui.Row(c).Gap(t.Space(1)).AlignItems(ui.Center).Justify(ui.End).Children(func() {
		m.historyActionContent(c, t, tok, entry, row)
	})
}

// historyActionContent is the per-row 打开 button and the row menu trigger.
// Both stay clickable in the 860 fixed column whatever the horizontal
// scroll position.
func (m *Model) historyActionContent(c *ui.Context, t *ui.Theme, tok *Tokens, entry HistoryEntry, row int) {
	open := ui.Button(c, "打开").Padding(t.Space(0.5), t.Space(1)).Height(t.Space(6)).
		Label("打开 " + entry.Label)
	if open.Clicked() || open.Shortcut(0, ui.KeyEnter) {
		m.historyRow = row
		m.openHistoryEntry(entry)
	}
	menu := ui.Button(c, "⋯").Padding(t.Space(0.5), t.Space(1)).Height(t.Space(6)).
		Label("更多操作 " + entry.Label)
	if menu.Clicked() || menu.Shortcut(0, ui.KeyEnter) {
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
// than as a native menu so the destructive items can use the danger text
// the design asks for and disabled items can explain why. PopoverBase
// closes on Escape or a press outside and gives the focus back to ⋯.
func (m *Model) historyRowMenu(c *ui.Context, t *ui.Theme, tok *Tokens, anchor ui.Element, entry HistoryEntry) {
	ui.PopoverBase(c, anchor, &m.historyMenuOpen, func(panel ui.Element) {
		panel.MinWidth(t.Space(45)).Background(tok.SurfaceOverlay).Border(1, tok.BorderDefault).
			Radius(t.Space(1.5)).Padding(t.Space(1)).Gap(t.Space(0.5)).
			Shadow(0, 8, 24, 0, tok.Scrim)
		// The menu opens at the row's right edge so the fixed action
		// column keeps it inside the 860 window.
		panel.AttachTo(anchor, ui.AnchorBottomEnd, ui.AnchorTopEnd)
		panel.Children(func() {
			m.historyMenuItem(c, t, "打开", tok.TextPrimary, false, true, func() {
				m.openHistoryEntry(entry)
			})
			exportBlocked := m.historyExportsBlocked()
			m.historyMenuItem(c, t, "导出 CSV", tok.TextPrimary, exportBlocked, false, func() {
				m.ExportHistory(entry.ID, app.ExportCSV)
			})
			m.historyMenuItem(c, t, "导出 JSON", tok.TextPrimary, exportBlocked, false, func() {
				m.ExportHistory(entry.ID, app.ExportJSON)
			})
			ui.Divider(c)
			reason := m.historyDeleteBlockedReason(entry)
			m.historyMenuItem(c, t, "删除截图", t.Danger, reason != "", false, func() {
				m.RequestScreenshotCleanup(entry.ID)
			})
			m.historyMenuItem(c, t, "删除记录", t.Danger, reason != "", false, func() {
				m.RequestRecordCleanup(entry.ID)
			})
			if reason != "" {
				ui.Text(c, reason).MaxWidth(t.Space(60)).FontSize(t.FontSize * 0.8).
					TextColor(tok.TextMuted)
			}
		})
	})
}

// historyMenuItem is one flat menu row. first puts the keyboard focus on
// 打开 when the menu opens, so Tab/Enter can drive the whole menu.
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

// historyDeleteBlockedReason explains why a row's deletion entries are
// disabled. The live session's record is never deletable while it is the
// current session, even after the scan finished: the scan slot owns it
// until a new session replaces it.
func (m *Model) historyDeleteBlockedReason(entry HistoryEntry) string {
	if entry.ID != "" && entry.ID == string(m.appliedSession) {
		return "当前会话记录，扫描中或未转移前不能删除。"
	}
	return m.cleanupBlockedReason()
}

// historyTableFooter states the accounting and, at the scrolling width,
// how to reach the hidden data columns.
func (m *Model) historyTableFooter(c *ui.Context, t *ui.Theme, tok *Tokens, entries []HistoryEntry) {
	text := fmt.Sprintf("共 %d 条 · ", len(entries))
	if first, last := m.historyList.Visible(); last >= first {
		text += fmt.Sprintf("显示 %d 条，向下滚动查看更多", last-first+1)
	} else {
		text += "向下滚动查看更多"
	}
	if historyUsesComposite(m.contentWidth) {
		text += "，左右滚动查看全部列"
	}
	text += " · 采集 = 可导出 + 待校对 + 失败 + 未处理 + 其他"
	ui.Text(c, text).TextColor(tok.TextMuted).FontSize(t.FontSize * 0.8).
		Bind(&m.historyFooter)
}

// historyNotices shows export/cleanup outcomes, the load error and a
// pending per-record cleanup confirmation.
func (m *Model) historyNotices(c *ui.Context, t *ui.Theme, tok *Tokens) {
	if m.cleanupNotice != "" {
		ui.Text(c, m.cleanupNotice).TextColor(tok.TextMuted)
	}
	if m.historyNotice != "" {
		ui.Text(c, m.historyNotice)
	}
	if m.historyError != "" {
		ui.Text(c, m.historyError).TextColor(t.Danger)
	}
	if m.cleanup != nil && !m.cleanup.target.all() {
		m.cleanupPromptArea(c, t)
	}
}

// historyStatusText is the status bar's line on the history destination.
func (m *Model) historyStatusText() string {
	entries := m.visibleHistory()
	text := fmt.Sprintf("%d 条历史记录", len(entries))
	if m.historyRow >= 0 && m.historyRow < len(entries) {
		text += " · 已选中 " + historyTimeText(entries[m.historyRow])
	}
	return text
}

// historyRowLabel is the assistive-technology name of one row: the same
// accounting the visible cells carry.
func historyRowLabel(entry HistoryEntry) string {
	if entry.Unreadable {
		return historyTimeText(entry) + " · 记录不可读"
	}
	return fmt.Sprintf("%s · 采集 %s · 可导出 %s · 待校对 %s · 无法导出 %s",
		historyTimeText(entry), formatCount(entry.CapturedCount), formatCount(entry.ExportableCount),
		formatCount(int64(entry.PendingReview)),
		formatCount(entry.FailedCount+entry.UnprocessedCount+entry.OtherCount))
}

// historyTimeText is the table's compact local time. The year is omitted as
// in the design; the picker label keeps the full date.
func historyTimeText(entry HistoryEntry) string {
	if entry.SavedAt.IsZero() {
		return "日期未知"
	}
	return entry.SavedAt.In(time.Local).Format("01-02 15:04")
}

// historyCoveragePercent returns the percentage only when the record has a
// denominator or explicitly says the coverage is complete.
func historyCoveragePercent(entry HistoryEntry) (int, bool) {
	switch {
	case entry.CoverageExpected > 0:
		percent := int(entry.CapturedCount * 100 / entry.CoverageExpected)
		return min(100, max(0, percent)), true
	case entry.Coverage == record.CoverageComplete:
		return 100, true
	default:
		return 0, false
	}
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

// openHistoryEntry routes the row's 打开 action. "本次" goes back to the
// live session slot through ShowLive; every other row opens the history
// slot.
func (m *Model) openHistoryEntry(entry HistoryEntry) {
	m.closeHistoryMenu()
	m.historyMenuID = ""
	m.histChoice = entry.Label
	m.OpenHistory()
}

// historyPageLegacy is the old single-column results area's history list.
// The page shell uses the table above; the legacy list stays for the
// interaction tests that still drive the pre-shell layout.
func (m *Model) legacyHistoryActions(c *ui.Context, t *ui.Theme) {
	blocked := m.cleanupBlockedReason()
	selected := m.selectedHistoryIDs()
	ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "日期筛选").TextColor(t.TextMuted)
		m.noteTextInputFocus(ui.TextInput(c, &m.historyFilter).Width(110).Placeholder("YYYY-MM-DD"))
		if ui.Button(c, "清除筛选").Clicked() {
			m.historyFilter = ""
		}
		if m.historyFilterInvalid() {
			ui.Text(c, "格式应为 YYYY-MM-DD。").TextColor(t.Danger).FontSize(t.FontSize * 0.9)
		}
		if len(selected) > 0 {
			ui.Textf(c, "已选 %d 条", len(selected)).TextColor(t.TextMuted)
			if ui.Button(c, "删除选中截图").Disabled(blocked != "").Clicked() {
				m.RequestSelectedScreenshotCleanup()
			}
			if ui.Button(c, "删除选中记录").Disabled(blocked != "").Clicked() {
				m.RequestSelectedRecordCleanup()
			}
			if ui.Button(c, "清除选择").Clicked() {
				m.ClearHistorySelection()
			}
		}
	})
	ui.ScrollBoth(c).MaxHeight(180).Children(func() {
		for _, entry := range m.visibleHistory() {
			m.legacyHistoryRow(c, t, entry, blocked)
		}
	})
	if m.historyBulk != nil {
		m.historyBulkArea(c, t)
	}
	if m.historyNotice != "" {
		ui.Text(c, m.historyNotice)
	}
	if m.historyError != "" {
		ui.Text(c, m.historyError).TextColor(t.Danger)
	}
	if blocked != "" {
		ui.Text(c, blocked).TextColor(t.TextMuted).FontSize(t.FontSize * 0.9)
	}
}

func (m *Model) legacyHistoryRow(c *ui.Context, t *ui.Theme, entry HistoryEntry, blocked string) {
	ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
		selected := m.historySelected[entry.ID]
		if ui.Checkbox(c, &selected, "").Changed() {
			m.setHistorySelected(entry.ID, selected)
		}
		text := m.historyRowText(entry)
		if m.rec != nil && m.rec.ID == entry.ID {
			text += "（当前）"
		}
		ui.Text(c, text).TextColor(t.TextMuted).SingleLine().Grow(1)
		if ui.Button(c, "打开").Clicked() {
			m.histChoice = entry.Label
			m.OpenHistory()
		}
		if !m.cfg.PageLayout {
			if m.cfg.ExportSaver != nil {
				if ui.Button(c, "导出 JSON").Disabled(m.historyExportsBlocked()).Clicked() {
					m.ExportHistory(entry.ID, app.ExportJSON)
				}
				if ui.Button(c, "导出 CSV").Disabled(m.historyExportsBlocked()).Clicked() {
					m.ExportHistory(entry.ID, app.ExportCSV)
				}
			}
			if ui.Button(c, "删除截图").Disabled(blocked != "").Clicked() {
				m.RequestScreenshotCleanup(entry.ID)
			}
			if ui.Button(c, "删除当前记录").Disabled(blocked != "").Clicked() {
				m.RequestRecordCleanup(entry.ID)
			}
		}
	})
}

func (m *Model) historyEmptyText() string {
	if m.histErr != "" {
		return "读取失败。"
	}
	if m.historyFilterInvalid() {
		return "日期格式应为 YYYY-MM-DD。"
	}
	if len(m.history) > 0 && len(m.visibleHistory()) == 0 {
		return "没有匹配筛选条件的记录。"
	}
	return "暂无已保存的记录。"
}
