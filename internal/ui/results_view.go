package ui

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/game/zzz"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/egoist/mygo/ui"
)

// The review layout's fixed metrics (08 §3.1, 09 §2): a 256-DIP item list,
// a 488/400-DIP form and screenshots between, never under 360 DIP. The
// standard profile shows the list as a column; the compact profile folds it
// into the top strip. Both keep screenshots and form side by side.
const (
	reviewListWidth     = 256
	reviewFormWidth     = 488
	reviewMinCropsWidth = 360
	reviewStandardWidth = 1376
	reviewListRowHeight = 52
	reviewPagePadding   = 32
	reviewPanePaddingX  = 16
	reviewPanePaddingY  = 16
	reviewCropImageMin  = 120
	reviewZoomStep      = 0.25
	reviewZoomMax       = 4
	// Main-stat controls share one row inside the form; keep their total
	// under the inner width at 960 (L6).
	reviewMainAttributeWidth = 132
	reviewMainValueWidth     = 76
	reviewMainUnitWidth      = 100
	reviewHeaderSizeRatio    = 0.9
)

// reviewLayoutWidths is the laid-out width of each review region, recorded
// for the acceptance checks of the three profiles.
type reviewLayoutWidths struct {
	List        float32
	Crops       float32
	Form        float32
	Standard    bool
	ListFocused bool
}

// reviewStandard reports the standard width profile (client area ≥ 1376 DIP):
// the item list is its own column. Below it the compact profile puts the
// list into the top strip.
func (m *Model) reviewStandard() bool {
	return m.contentWidth >= reviewStandardWidth
}

// reviewInnerWidth is the width the page leaves for its panes: the window
// less the navigation rail and the page padding.
func (m *Model) reviewInnerWidth() float32 {
	width := float32(m.contentWidth) - m.railWidth() - m.pageLeftPadding() - m.pagePadding()
	if width < 0 {
		return 0
	}
	return width
}

// reviewCropsWidth is the width the screenshot pane gets from the layout
// arithmetic. The pane's real bounds are recorded in reviewLayout; this
// value sizes the crops before the first frame commits.
func (m *Model) reviewCropsWidth() float32 {
	width := m.reviewInnerWidth()
	if m.reviewStandard() {
		width -= reviewListWidth + columnGap
	}
	width -= m.reviewFormWidth() + columnGap
	if width < reviewMinCropsWidth {
		width = reviewMinCropsWidth

	}
	return width
}

// reviewCropImageWidth is the crop's fit width inside the screenshot pane.
func (m *Model) reviewCropImageWidth() float32 {
	width := m.reviewCropsWidth() - 2*reviewPanePaddingX - 14 // scrollbar
	if width < reviewCropImageMin {
		return reviewCropImageMin
	}
	return width
}

// reviewCropWidth applies zoom to the effective fit size, including the
// profile's default scale and the available width. Every crop therefore
// grows or shrinks from the size the user is actually looking at.
func (m *Model) reviewCropWidth(imageWidth int) float32 {
	fit := min(m.reviewCropImageWidth(), float32(imageWidth)*m.reviewDefaultZoom())
	return fit * m.cropZoomFactor()
}

// resultsTitle names what the review area shows: the live session's saved
// record or a reopened history record, with the recovery mark.
func (m *Model) resultsTitle() string {
	if m.rec == nil {
		return "结果"
	}
	evidence := ""
	if m.rec.EvidenceRemoved {
		evidence = "，截图已删除或缺失"
	}
	notStarted := terminationNotStarted(m.rec.Shell.Termination)
	if m.rec.Live && notStarted {
		return fmt.Sprintf("结果：没有开始（已保存 %d 条%s）", len(m.rec.Rows), evidence)
	}
	if m.rec.Live {
		return fmt.Sprintf("结果：本次扫描（已保存 %d 条%s）", len(m.rec.Rows), evidence)
	}
	mark := ""
	if m.rec.Interrupted {
		mark = fmt.Sprintf("，中断恢复 %d 条", m.rec.InterruptedCount)
	}
	return fmt.Sprintf("结果：历史记录 %s（%d 条%s%s）", m.rec.ID, len(m.rec.Rows), mark, evidence)
}

// resultsArea is the review workspace: the record's items, their
// screenshots and the editable form. The summary is a separate view of the
// same record, so this area never falls back to a whole-page list/detail
// switch (P-11): screenshots and the form are always side by side.
func (m *Model) resultsArea(c *ui.Context, t *ui.Theme) {
	ui.Column(c).Grow(1).FillHeight().MinHeight(0).Gap(t.Space(1)).Children(func() {
		m.sessionTerminationArea(c, t)
		if m.histErr != "" {
			ui.Text(c, "历史记录读取失败："+m.histErr).TextColor(t.Danger)
		}
		if m.cleanupNotice != "" {
			ui.Text(c, m.cleanupNotice)
		}
		if m.cleanup != nil && !m.cleanup.target.all() {
			m.cleanupPromptArea(c, t)
		}
		if m.recErr != "" {
			ui.Text(c, "记录读取失败："+m.recErr).TextColor(t.Danger)
			return
		}
		if m.rec == nil || len(m.rec.Rows) == 0 {
			ui.Text(c, m.emptyResultsText()).TextColor(t.TextMuted)
			if m.completionVisible() {
				ui.Text(c, m.completionStatusText()).TextColor(t.TextMuted)
			}
			return
		}
		m.reviewArea(c, t)
	})
}

// reviewArea is the three-region review. The list is a column at standard
// widths and a top strip at compact widths; the screenshot pane and the
// 488/400-DIP form keep their places at every width.
func (m *Model) reviewArea(c *ui.Context, t *ui.Theme) {
	visible := m.visibleResultRows()
	m.settleResultSelection()
	if row := m.selectedRow(); row != nil {
		m.ensureReviewEdit(row)
	}
	m.preparePairedHeights(m.selectedRow())
	if m.reviewField == "" {
		if row := m.selectedRow(); row != nil && m.reviewRowEditable(row) {
			// Entering the review (or returning to it) starts on the first
			// problem field (08 §3). A settled item keeps its read-only
			// focus request, which enterReviewView already placed.
			m.requestReviewFieldFocus("")
		}
	}
	ui.Column(c).Grow(1).FillHeight().MinHeight(0).Gap(16).Children(func() {
		m.reviewFormNotices(c, t)
		if !m.reviewStandard() {
			m.reviewCompactStrip(c, t, visible)
		}
		ui.Row(c).Grow(1).FillHeight().MinHeight(0).Gap(columnGap).Children(func() {
			if m.reviewStandard() {
				m.reviewListPane(c, t, visible)
			}
			m.reviewCropsPane(c, t, visible)
			m.reviewFormPane(c, t, visible)
		})
	})
	m.recordReviewLayout(c)
	if !m.pairedMeasureReady {
		m.pairedMeasureReady = true
		c.AnimationFrame()
	}
}

// recordReviewLayout stores the pane widths the frame laid out, for the
// profile checks.
func (m *Model) recordReviewLayout(c *ui.Context) {
	layout := reviewLayoutWidths{Standard: m.reviewStandard()}
	if layout.Standard {
		layout.List = m.reviewListRef.Bounds(c).W
		layout.ListFocused = m.list.Focused(c)
	}
	layout.Crops = m.reviewCropsRef.Bounds(c).W + 2*reviewPanePaddingX + 2
	layout.Form = m.reviewFormRef.Bounds(c).W
	if layout.List == 0 && layout.Crops == 0 && layout.Form == 0 {
		return
	}
	m.reviewLayout = layout
}

// reviewListPane is the standard profile's item list column.
func (m *Model) reviewListPane(c *ui.Context, t *ui.Theme, visible []int) {
	tok := m.effectiveTokens()
	panel(c, tok).Width(reviewListWidth).Shrink(0).FillHeight().MinHeight(0).Padding(16).Gap(12).Bind(&m.reviewListRef).Children(func() {
		ui.Column(c).Gap(12).Children(func() {
			ui.Row(c).MinHeight(panelHeaderHeight).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "驱动盘").FontWeight(600)
				ui.Box(c).Grow(1)
				ui.Textf(c, "%d 件", len(visible)).TextColor(t.TextMuted)
			})
			m.resultsFilterBar(c, t)
			m.resultsFacetFilters(c, t, reviewListWidth-2*reviewPanePaddingX)
		})
		ui.Divider(c)
		if len(visible) == 0 {
			ui.Column(c).Grow(1).Padding(t.Space(2), reviewPanePaddingX).Gap(t.Space(1)).
				Children(func() {
					ui.Text(c, m.filteredEmptyText()).TextColor(t.TextMuted)
					if m.hasResultFacets() && button(c, "清除筛选").Clicked() {
						m.clearResultFacets()
					}
				})
			return
		}
		m.reviewItemList(c, t, tok, visible)
		ui.Divider(c)
		ui.Row(c).Padding(t.Space(1.5), reviewPanePaddingX).Children(func() {
			ui.Text(c, m.listFooterText(visible)).FontSize(t.FontSize * reviewHeaderSizeRatio).
				TextColor(t.TextMuted).SingleLine()
		})
	})
}

// reviewItemList is the keyboard-driven item list. The ListState owns the
// chosen display row; the frame maps it back to the workspace's full-row
// selection through the same editor guard as before.
func (m *Model) reviewItemList(c *ui.Context, t *ui.Theme, tok *Tokens, visible []int) {
	previous := m.selected
	previousDisplay := resultDisplayIndex(visible, previous)
	if previousDisplay >= 0 {
		m.tableSelected = previousDisplay
	}
	m.list.Key = func(row int) any {
		if row < 0 || row >= len(visible) {
			return row
		}
		return m.rec.Rows[visible[row]].Seq
	}
	m.list.Label = func(row int) string {
		if row < 0 || row >= len(visible) {
			return ""
		}
		r := &m.rec.Rows[visible[row]]
		return fmt.Sprintf("#%04d %s %s", r.Seq, r.ClassText(), r.labelSetSlot())
	}
	list := ui.List(c, &m.list, len(visible), func(row int) {
		if row < 0 || row >= len(visible) {
			return
		}
		m.reviewItemRow(c, t, tok, visible, row)
	}).Grow(1).MinHeight(0)
	if m.focusListRequested && m.list.Focus(c) {
		m.focusListRequested = false
	}
	_ = list
	chosen := m.tableSelected
	if chosen != previousDisplay && chosen >= 0 && chosen < len(visible) {
		m.selected = visible[chosen]
		m.applyTableSelection(previous)
		m.tableSelected = resultDisplayIndex(visible, m.selected)
	}
}

// reviewItemRow renders one item: its capture number and category on the
// first line, set and slot on the second. The category is text plus a
// glyph and a border shape, never color alone.
func (m *Model) reviewItemRow(c *ui.Context, t *ui.Theme, tok *Tokens, visible []int, row int) {
	if row < 0 || row >= len(visible) {
		return
	}
	r := &m.rec.Rows[visible[row]]
	selected := row == m.tableSelected
	background := ui.Transparent
	border := ui.Transparent
	if selected {
		background, border = tok.RowSelected, tok.RowSelectedBorder
	}
	height := float32(reviewListRowHeight)
	if m.textScale > 0 {
		height *= m.textScale
	}
	ui.Column(c).Height(height).Shrink(0).Padding(t.Space(1.5), reviewPanePaddingX).
		Gap(t.Space(0.5)).Background(background).
		BorderWidth(0, 0, 0, 3).BorderColor(border).Children(func() {
		ui.Row(c).Gap(t.Space(1)).AlignItems(ui.Center).Children(func() {
			ui.Textf(c, "#%04d", r.Seq).FontWeight(600).TextColor(tok.TextPrimary).SingleLine().Shrink(1)
			ui.Box(c).Grow(1)
			m.reviewCategoryTag(c, t, tok, r)
		})
		ui.Text(c, r.labelSetSlot()).FontSize(t.FontSize * reviewHeaderSizeRatio).
			TextColor(selectedMuted(t, tok, selected)).SingleLine()
	})
}

// labelSetSlot is the item list's second line: set and slot, with the
// explicit "未识别" wording for an unparsed item.
func (r Row) labelSetSlot() string {
	set := r.SetColumnText()
	slot := r.SlotText()
	switch {
	case set == "" || set == "—":
		set = "套装未识别"
	case slot == "":
		slot = "号位未识别"
	}
	if slot == "" {
		return set
	}
	return set + " · " + slot
}

// reviewCategoryTag is the four-class label as text plus shape.
func (m *Model) reviewCategoryTag(c *ui.Context, t *ui.Theme, tok *Tokens, row *Row) {
	glyph, label := reviewCategoryParts(row)
	if m.recentlyConfirmedNow(row) {
		glyph, label = "✓", "已确认"
	}
	background, border, text := tok.SurfaceControl, tok.BorderDefault, tok.TextSecondary
	dashed := false
	switch {
	case m.recentlyConfirmedNow(row):
		background, border, text = tok.SuccessSoft, tok.SuccessBorder, tok.SuccessFG
	case row.ItemClass == app.ItemClassPendingBackfill:
		background, border, text = tok.WarningSoft, tok.WarningBorder, tok.WarningFG
		dashed = true
	case row.ItemClass == app.ItemClassPendingReview:
		background, border, text = tok.WarningSoft, tok.WarningBorder, tok.WarningFG
	case row.ItemClass == app.ItemClassWarning:
		background, border, text = tok.WarningSoft, tok.WarningBorder, tok.WarningFG
		dashed = true
	case row.ItemClass == app.ItemClassPassed:
		background, border, text = tok.SuccessSoft, tok.SuccessBorder, tok.SuccessFG
	default:
		switch row.Status {
		case record.StatusFailed:
			background, border, text = tok.DangerSoft, tok.DangerBorder, tok.DangerFG
		case record.StatusUnprocessed:
			dashed = true
		}
	}
	tag := ui.Row(c).Shrink(0).Padding(t.Space(0.25), t.Space(1)).Gap(t.Space(0.5)).
		AlignItems(ui.Center).Background(background).Border(1, border).
		BorderStyle(ui.BorderSolid).Radius(t.Space(2)).Label("状态：" + label)
	if dashed {
		tag = tag.BorderStyle(ui.BorderDashed)
	}
	tag.Children(func() {
		ui.Text(c, glyph).FontSize(t.FontSize * 0.8).FontWeight(600).
			TextColor(text).SingleLine().Role(ui.RoleNone)
		ui.Text(c, label).FontSize(t.FontSize * 0.8).FontWeight(600).
			TextColor(text).SingleLine().Role(ui.RoleNone)
	})
}

// reviewCategoryParts maps a row to its category glyph and label.
func reviewCategoryParts(row *Row) (glyph, label string) {
	label = row.ClassText()
	switch row.ItemClass {
	case app.ItemClassPendingBackfill:
		if row.Status == record.StatusUnprocessed {
			return "⊘", label
		}
		return "✕", label
	case app.ItemClassPendingReview:
		return "◐", label
	case app.ItemClassWarning:
		return "!", label
	case app.ItemClassPassed:
		return "✓", label
	}
	switch row.Status {
	case record.StatusFailed:
		return "✕", label
	case record.StatusUnprocessed:
		return "⊘", label
	}
	return "◐", label
}

// reviewCompactStrip is the compact profile's top bar: previous / next,
// the pending position, the filter segments and the "全部件" popover list.
func (m *Model) reviewCompactStrip(c *ui.Context, t *ui.Theme, visible []int) {
	tok := m.effectiveTokens()
	actionRow(c).Background(tok.SurfaceHeader).Children(func() {
		position := resultDisplayIndex(visible, m.selected)
		back := button(c, "上一件").Disabled(position <= 0)
		if back.Clicked() {
			m.selectAdjacentVisible(-1)
		}
		ui.Text(c, m.compactPositionText(visible)).FontWeight(600).SingleLine().Shrink(1)
		forward := button(c, "下一件").Disabled(position < 0 || position >= len(visible)-1)
		if forward.Clicked() {
			m.selectAdjacentVisible(1)
		}
		m.resultsFilterBar(c, t)
		m.compactItemPopover(c, t, visible)
	})
}

// compactItemPopover is the "全部件" popover: the same rows as the standard
// list, without the 256-DIP column. It carries the set/slot facets and uses
// a virtualized List, so 10,000 rows build only the visible frame.
func (m *Model) compactItemPopover(c *ui.Context, t *ui.Theme, visible []int) {
	anchor := button(c, "全部件 ▾").Bind(&m.reviewCompactRef)
	if anchor.Clicked() {
		m.compactListOpen = !m.compactListOpen
		if m.compactListOpen {
			m.compactListFocusRequested = true
		}
	}
	height := float32(m.contentHeight - 160)
	if height < 260 {
		height = 260
	}
	if height > 480 {
		height = 480
	}
	popover := ui.Popover(c, anchor, &m.compactListOpen, func() {
		ui.Column(c).Width(300).Height(height).Gap(t.Space(1)).
			Padding(t.Space(1)).Children(func() {
			m.resultsFacetFilters(c, t, 276)
			ui.Divider(c)
			if len(visible) == 0 {
				ui.Text(c, m.filteredEmptyText()).TextColor(t.TextMuted)
				return
			}
			previous := m.selected
			previousDisplay := resultDisplayIndex(visible, previous)
			if previousDisplay >= 0 {
				m.tableSelected = previousDisplay
			}
			m.list.Key = func(row int) any {
				if row < 0 || row >= len(visible) {
					return row
				}
				return m.rec.Rows[visible[row]].Seq
			}
			m.list.Label = func(row int) string {
				if row < 0 || row >= len(visible) {
					return ""
				}
				r := &m.rec.Rows[visible[row]]
				return fmt.Sprintf("#%04d %s %s", r.Seq, r.ClassText(), r.labelSetSlot())
			}
			list := ui.List(c, &m.list, len(visible), func(row int) {
				m.compactItemRow(c, t, visible, row)
			}).Grow(1).MinHeight(0)
			if m.compactListFocusRequested {
				m.list.Focus(c)
				m.compactListFocusRequested = false
			}
			_ = list
			chosen := m.tableSelected
			if chosen != previousDisplay && chosen >= 0 && chosen < len(visible) {
				m.selected = visible[chosen]
				m.applyTableSelection(previous)
				m.tableSelected = resultDisplayIndex(visible, m.selected)
				m.compactListOpen = false
			}
		})
	})
	if popover.Dismissed() {
		m.compactListOpen = false
	}
}

// compactItemRow is one row of the compact popover's virtualized list.
func (m *Model) compactItemRow(c *ui.Context, t *ui.Theme, visible []int, row int) {
	if row < 0 || row >= len(visible) {
		return
	}
	tok := m.effectiveTokens()
	r := &m.rec.Rows[visible[row]]
	selected := row == m.tableSelected
	background, border := ui.Transparent, ui.Transparent
	if selected {
		background, border = tok.RowSelected, tok.RowSelectedBorder
	}
	height := float32(reviewListRowHeight)
	if m.textScale > 0 {
		height *= m.textScale
	}
	ui.Column(c).Height(height).Shrink(0).Padding(t.Space(1), t.Space(1.5)).
		Gap(t.Space(0.5)).Background(background).
		BorderWidth(0, 0, 0, 3).BorderColor(border).Children(func() {
		ui.Row(c).Gap(t.Space(1)).AlignItems(ui.Center).Children(func() {
			ui.Textf(c, "#%04d", r.Seq).FontWeight(600).TextColor(tok.TextPrimary).
				SingleLine().Shrink(1)
			ui.Box(c).Grow(1)
			m.reviewCategoryTag(c, t, tok, r)
		})
		ui.Text(c, r.labelSetSlot()).FontSize(t.FontSize * reviewHeaderSizeRatio).
			TextColor(selectedMuted(t, tok, selected)).SingleLine()
	})
}

// reviewCropsPane is the screenshots: one crop per observed line in the
// game panel's reading order, then the context crops. Selecting a form
// field highlights its crops and scrolls the first into view; a record
// without line roles falls back to capture order without a highlight.
func (m *Model) reviewCropsPane(c *ui.Context, t *ui.Theme, visible []int) {
	tok := m.effectiveTokens()
	row := m.selectedRow()
	panel(c, tok).Grow(1).Shrink(1).MinWidth(reviewMinCropsWidth).FillHeight().MinHeight(0).
		Padding(16).Gap(12).Background(tok.SurfaceSunken).Children(func() {
		ui.Row(c).MinHeight(panelHeaderHeight).Gap(12).
			AlignItems(ui.Center).Children(func() {
			ui.Text(c, "截图").FontWeight(600).SingleLine()
			ui.Box(c).Grow(1)
			if row != nil {
				m.reviewZoomControls(c, t)
			}
		})
		scroll := ui.ScrollBoth(c).Grow(1).FillHeight().MinHeight(0).Bind(&m.reviewCropsRef)
		if m.reviewRegion == reviewRegionCrops {
			scroll.Focus()
		}
		scroll.Children(func() {
			ui.Column(c).Gap(8).Children(func() {
				m.reviewCropList(c, t, row)
			})
		})
	})
	_ = visible
}

// reviewZoomControls are the screenshot zoom actions: enlarge, shrink and
// fit the area. The current scale is named so the user can tell the fit
// state from a zoomed one.
func (m *Model) reviewZoomControls(c *ui.Context, t *ui.Theme) {
	label := "适合"
	if m.cropZoom > 0 {
		label = fmt.Sprintf("适合 ×%g", m.cropZoom)
	}
	if button(c, "缩小").Disabled(m.cropZoomFactor() <= reviewZoomStep).Clicked() {
		m.zoomCrop(-reviewZoomStep)
	}
	if button(c, "放大").Disabled(m.cropZoomFactor() >= reviewZoomMax).Clicked() {
		m.zoomCrop(reviewZoomStep)
	}
	if button(c, label).Clicked() {
		m.zoomCrop(0)
	}
}

// reviewCropList builds the crops of one item, with the highlight and the
// one-time scroll-to-crop of the selected form field.
func (m *Model) reviewCropList(c *ui.Context, t *ui.Theme, row *Row) {
	if m.rec != nil && m.rec.EvidenceRemoved {
		ui.Text(c, "该记录的截图已删除或缺失，无法显示图像；识别文本与字段数据仍保留。").
			TextColor(t.TextMuted)
	}
	if row == nil {
		ui.Text(c, "选择一件查看截图。").TextColor(t.TextMuted)
		m.scrollToCrop = false
		return
	}
	crops, rolesComplete := reviewCropsForRow(row)
	highlight := m.highlightedCropRoles(row, rolesComplete)
	if len(crops) == 0 {
		if row.MissingEvidence() {
			ui.Text(c, "这件缺少截图。").TextColor(t.TextMuted)
		} else {
			ui.Text(c, "这件没有可显示的截图。").TextColor(t.TextMuted)
		}
		m.scrollToCrop = false
		return
	}
	if !rolesComplete {
		ui.Text(c, "这件缺少行角色，截图按采集顺序列出。").
			FontSize(t.FontSize * reviewHeaderSizeRatio).TextColor(t.TextMuted)
	}
	if m.reviewField != "" && len(EvidenceRolesForField(m.reviewField)) == 0 && rolesComplete {
		ui.Text(c, "这个字段没有截图。").
			FontSize(t.FontSize * reviewHeaderSizeRatio).TextColor(t.TextMuted)
	}
	if highlight != nil && m.reviewField != "" {
		// The field maps to evidence, but this item may not carry that
		// crop (for example an equipped agent without an avatar image).
		present := false
		for i := range crops {
			if highlight[crops[i].role()] {
				present = true
				break
			}
		}
		if !present {
			ui.Text(c, "这个字段的截图缺失。").
				FontSize(t.FontSize * reviewHeaderSizeRatio).TextColor(t.TextMuted)
		}
	}
	scrolled := false
	lastContext := false
	for i := range crops {
		crop := &crops[i]
		if crop.context != lastContext {
			if crop.context {
				ui.Text(c, "其他截图").FontSize(t.FontSize * reviewHeaderSizeRatio).
					TextColor(t.TextMuted)
			}
			lastContext = crop.context
		}
		role := crop.role()
		highlighted := highlight != nil && highlight[role]
		target := highlighted && m.scrollToCrop && !scrolled
		m.reviewCrop(c, t, row, crop, highlighted, target)
		if target {
			scrolled = true
		}
	}
	if scrolled || m.scrollToCrop {
		m.scrollToCrop = false
	}
}

// reviewCrop is one crop block: its Chinese role label and the image, or
// the missing-image note that belongs to this row alone.
func (m *Model) reviewCrop(c *ui.Context, t *ui.Theme, row *Row, crop *reviewCropRow, highlighted, scrollIntoView bool) {
	tok := m.effectiveTokens()
	background := tok.SurfaceCard
	border := tok.BorderSubtle
	if highlighted {
		background, border = tok.AccentSoft, tok.AccentBorder
	}
	block := ui.Column(c).Gap(8).Padding(8).MinHeight(m.reviewRowHeight(row, crop.role())).
		Background(background).Border(1, border).Radius(controlRadius)
	if scrollIntoView {
		block = block.ScrollIntoView()
	}
	block.Children(func() {
		label := crop.roleLabel()
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			if number := reviewRoleNumber(crop.role()); number != "" {
				fieldNumber(c, number, highlighted, tok)
			}
			ui.Text(c, label).FontSize(t.FontSize * 12 / 14).TextColor(selectedMuted(t, tok, highlighted))
		})
		if crop.evidence == nil {
			ui.Text(c, "这一行缺少截图。").FontSize(t.FontSize * reviewHeaderSizeRatio).
				TextColor(selectedMuted(t, tok, highlighted))
			return
		}
		m.reviewCropImage(c, t, crop.evidence)
	})
}

// reviewCropImage draws one crop at the screenshot area's width, scaled up
// by the zoom with pixel-art edges. The image belongs to its own evidence
// identity, so a late load can never attach to another item.
func (m *Model) reviewCropImage(c *ui.Context, t *ui.Theme, evidence *Evidence) {
	if m.rec == nil || evidence == nil || evidence.ID == "" || m.cache.load == nil {
		return
	}
	entry := m.evidenceFor(m.rec.ID, evidence.ID)
	switch {
	case entry == nil:
		ui.Text(c, "截图加载中…").TextColor(t.TextMuted)
	case entry.err != nil:
		ui.Textf(c, "截图不可用：%v", entry.err).TextColor(t.Danger)
	default:
		imageWidth, imageHeight := entry.bitmap.Size()
		width := m.reviewCropWidth(imageWidth)
		if m.cropZoom > 0 {
			entry.bitmap.Pixelated()
		}
		if imageWidth <= 0 || imageHeight <= 0 {
			return
		}
		height := width * float32(imageHeight) / float32(imageWidth)
		ui.Image(c, entry.bitmap).Size(width, height).Fit(ui.Contain)
	}
}

// reviewCropRow is one screenshot block: an observed line with (or
// without) its crop, or a context crop that carries no text.
type reviewCropRow struct {
	line     *Line
	evidence *Evidence
	context  bool
	index    int
}

func (c *reviewCropRow) role() string {
	if c.evidence != nil && c.evidence.Role != "" {
		return c.evidence.Role
	}
	if c.line != nil {
		return c.line.Role
	}
	return ""
}

func (c *reviewCropRow) roleLabel() string {
	if c.context {
		return contextRoleText(c.role(), c.index)
	}
	return lineRoleText(c.role(), c.index)
}

// reviewCropsForRow builds the screenshot list and reports whether every
// line crop carries a locatable role. When a role is missing the list
// follows capture order and nothing is highlighted (R-07a's fallback).
func reviewCropsForRow(row *Row) ([]reviewCropRow, bool) {
	if row == nil {
		return nil, false
	}
	lineCount := len(row.Lines)
	for i := range row.Evidence {
		ev := &row.Evidence[i]
		if ev.Kind == EvidenceLine && ev.Index >= lineCount {
			lineCount = ev.Index + 1
		}
	}
	crops := make([]reviewCropRow, 0, lineCount+len(row.Evidence))
	complete := true
	for i := 0; i < lineCount; i++ {
		crop := reviewCropRow{index: i}
		if i < len(row.Lines) {
			crop.line = &row.Lines[i]
		}
		crop.evidence = row.LineEvidence(i)
		role := crop.role()
		if _, ok := lineRoleRank(role); !ok {
			complete = false
		}
		crops = append(crops, crop)
	}
	if complete {
		// The game panel's reading order: set, level, main stat, then
		// substats 1–4.
		sortCropsByRole(crops)
	}
	for i := range row.Evidence {
		ev := &row.Evidence[i]
		if ev.Kind != EvidenceContext {
			continue
		}
		crops = append(crops, reviewCropRow{evidence: ev, context: true, index: ev.Index})
	}
	return crops, complete
}

func sortCropsByRole(crops []reviewCropRow) {
	for i := 1; i < len(crops); i++ {
		current := crops[i]
		currentRank, _ := lineRoleRank(current.role())
		j := i - 1
		for j >= 0 {
			rank, _ := lineRoleRank(crops[j].role())
			if rank <= currentRank {
				break
			}
			crops[j+1] = crops[j]
			j--
		}
		crops[j+1] = current
	}
}

// highlightedCropRoles maps the selected form field to the crop roles it
// locates. A record without complete line roles gets no highlight.
func (m *Model) highlightedCropRoles(row *Row, rolesComplete bool) map[string]bool {
	if row == nil || !rolesComplete || m.reviewField == "" {
		return nil
	}
	roles := EvidenceRolesForField(m.reviewField)
	if len(roles) == 0 {
		return nil
	}
	set := make(map[string]bool, len(roles))
	for _, role := range roles {
		set[role] = true
	}
	return set
}

// reviewFormPane is the 488/400-DIP form. Its content scrolls inside the pane;
// the action bar stays fixed at the bottom at every height.
func (m *Model) reviewFormPane(c *ui.Context, t *ui.Theme, visible []int) {
	tok := m.effectiveTokens()
	row := m.selectedRow()
	// A click inside this frame may request a new focus (修改 opens the
	// editor with its first field focused). Only the requests that were
	// already pending when the frame started are consumed now; a new one
	// survives for the frame that builds its control.
	fieldSeq, modifySeq := m.focusReviewFieldSeq, m.focusModifySeq
	// A zero pending queue is the finished review even when the list still
	// shows the last confirmed row for this visit. The list keeps its place;
	// the form states the conclusion and offers the export.
	if m.filter == resultFilterPending && m.reviewSettled() {
		row = nil
	}
	panel(c, tok).Width(m.reviewFormWidth()).Shrink(0).FillHeight().MinHeight(0).Padding(16).Gap(12).Bind(&m.reviewFormRef).Children(func() {
		if row != nil {
			m.ensureReviewEdit(row)
			m.reviewFormHeader(c, t, row, m.reviewEditing(row))
		}
		ui.ScrollBoth(c).Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).Width(m.reviewFormWidth() - 2*reviewPanePaddingX - 2).Gap(8).Children(func() {
				if row == nil {
					m.reviewEmptyForm(c, t, visible)
					return
				}
				m.ensureReviewEdit(row)
				editing := m.reviewEditing(row)
				if editing {
					m.reviewEditForm(c, t, row)
				} else {
					m.reviewReadOnlyForm(c, t, row)
				}
			})
		})
		// The pending one-shot focus requests belong to the frame that
		// built the control they name; a request made during this frame
		// (by the 修改 click) waits for the next frame. Later Tab moves are
		// the user's.
		if m.focusReviewFieldSeq == fieldSeq {
			m.focusReviewField = ""
		}
		if m.focusModifySeq == modifySeq {
			m.focusModifyRequested = false
		}
		if row != nil {
			m.reviewActionBar(c, t, row)
		}
	})
}

// reviewEmptyForm is the form pane with no chosen item. Following the
// design, an empty pending queue is a finished review, not a blank page.
func (m *Model) reviewEmptyForm(c *ui.Context, t *ui.Theme, visible []int) {
	if m.filter == resultFilterPending && m.reviewSettled() {
		counts := m.recordCounts()
		switch {
		case !counts.ExportKnown:
			ui.Text(c, "不可确认完成").FontWeight(600).TextColor(t.Warning)
			ui.Text(c, "不可确认：导出清单尚未就绪。").TextColor(t.Warning)
		case counts.Exportable == 0:
			ui.Text(c, "无可导出条目").FontWeight(600).TextColor(t.Warning)
			ui.Text(c, "不可确认：无可导出的条目。").TextColor(t.Warning)
		default:
			ui.Text(c, "都处理完了").FontWeight(600)
			ui.Textf(c, "可导出 %d 件 · 排除 %d 件", counts.Exportable, counts.Captured-counts.Exportable)
		}
		blocked := m.exportEntryBlocked()
		ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
			if primaryButton(c, "导出").Disabled(blocked).Clicked() {
				m.requestExport()
			}
			if blocked {
				if reason := m.exportEntryBlockedReason(); reason != "" {
					ui.Text(c, reason).FontSize(t.FontSize * 0.85).
						TextColor(t.TextMuted).SingleLine()
				}
			}
		})
		if button(c, "删除这条记录的截图").Disabled(m.cleanupRunning()).Clicked() {
			m.RequestScreenshotCleanup(m.rec.ID)
		}
		return
	}
	ui.Text(c, m.filteredEmptyText()).TextColor(t.TextMuted)
	if m.hasResultFacets() {
		if button(c, "清除筛选").Clicked() {
			m.clearResultFacets()
		}
	}
	if button(c, "显示全部").Clicked() {
		m.setResultFilter(resultFilterAll)
	}
	_ = visible
}

// reviewFormHeader names the item and its category, then the form's state
// bands: backfill, exportable warning, save failure, loading and export.
// A settled item's 修改 action sits at its top right (08 §3.4); the
// passed row's status tag names whether the revision was auto accepted or
// human confirmed.
func (m *Model) reviewFormHeader(c *ui.Context, t *ui.Theme, row *Row, editing bool) {
	tok := m.effectiveTokens()
	status := passedStatusLabel(row)
	ui.Row(c).MinHeight(panelHeaderHeight).Gap(t.Space(1)).AlignItems(ui.Center).Children(func() {
		ui.Text(c, strings.TrimSuffix(resultDetailTitle(row), " · "+row.LevelText())).FontWeight(600).SingleLine().Shrink(1)
		ui.Box(c).Grow(1)
		// A settled item's state is its status tag or warning band; only a
		// pending item needs the category tag here (wireframes 07, 13, 14).
		if status == "" && row.ItemClass != app.ItemClassWarning {
			m.reviewCategoryTag(c, t, tok, row)
		}
		if !editing && m.canModifyRow(row) {
			modify := button(c, "修改").Key("review-modify").Shrink(0).Disabled(m.reviewActionsBlocked())
			if m.focusModifyRequested {
				modify.Focus()
			}
			if modify.Clicked() {
				m.startModify(row)
			}
		}
	})

}

// passedStatusLabel names a passed item's confirmation quality (08 §3.4).
// It is empty for every other class; a warning item's state is its notice
// band instead.
func passedStatusLabel(row *Row) string {
	if row == nil || row.ItemClass != app.ItemClassPassed {
		return ""
	}
	if row.Quality == record.QualityHumanConfirmed {
		return "已通过 · 已人工确认"
	}
	return "已通过 · 自动通过"
}

// reviewPassedTag is the passed status label: text plus a success shape,
// never color alone.
func (m *Model) reviewPassedTag(c *ui.Context, t *ui.Theme, tok *Tokens, label string) {
	ui.Row(c).Shrink(0).AlignSelf(ui.Start).Padding(t.Space(0.25), t.Space(1)).Gap(t.Space(0.5)).
		AlignItems(ui.Center).Border(1, tok.SuccessBorder).
		BorderStyle(ui.BorderSolid).Radius(t.Space(2)).Label("状态：" + label).Children(func() {
		ui.Text(c, "✓").FontSize(t.FontSize * 0.8).FontWeight(600).
			TextColor(tok.SuccessFG).SingleLine().Role(ui.RoleNone)
		ui.Text(c, label).FontSize(t.FontSize * 0.8).FontWeight(600).
			TextColor(tok.SuccessFG).SingleLine().Role(ui.RoleNone)
	})
}

// retryActionLabel names the failed action the error band retries.
func (m *Model) retryActionLabel() string {
	switch m.retryAction() {
	case reviewConfirmStay:
		return "重试确认"
	case reviewConfirmNext:
		return "重试确认并下一项"
	case reviewConfirmModify:
		return "重试确认修改"
	default:
		return "重试保存"
	}
}

// reviewEditForm keeps the full form in stable evidence order. Problems
// stay with their fields while strings, candidates and revisions retain
// their original editor semantics.
func (m *Model) reviewEditForm(c *ui.Context, t *ui.Theme, row *Row) {
	if m.edit == nil {
		return
	}
	m.edit.refresh()
	problems := m.reviewProblems(row)
	for _, field := range pairedFormFields {
		if field == FormFieldSubstats {
			m.editSubstats(c, t, m.edit)
			continue
		}
		m.reviewEditField(c, t, row, field, problems)
	}
	m.rawTextArea(c, t, row)
}

// reviewFieldInLeading reports whether field is rendered in the frozen
// leading group. The substat group renders every row field, so a row field
// belongs to the group's position.
func reviewFieldInLeading(first []FormField, field FormField) bool {
	if slices.Contains(first, field) {
		return true
	}
	return isSubstatField(field) && slices.Contains(first, FormFieldSubstats)
}

// reviewEditField is one editable field: its label, the control, and the
// reason under it. The whole block is a click target for the screenshots.
func (m *Model) reviewEditField(c *ui.Context, t *ui.Theme, row *Row, field FormField, problems []reviewProblem) {
	if m.edit == nil {
		return
	}
	problem, hasProblem := reviewProblemKindFor(problems, field)
	block := m.reviewFieldCard(c, t, row, field)
	block.OnClick(func() { m.selectReviewField(field) })
	block.Children(func() {
		ui.Row(c).Gap(t.Space(1)).AlignItems(ui.Center).Children(func() {
			if roles := EvidenceRolesForField(field); len(roles) > 0 {
				if number := reviewRoleNumber(roles[0]); number != "" {
					fieldNumber(c, number, reviewFieldShows(field, m.reviewField), m.effectiveTokens())
				}
			}
			ui.Text(c, formFieldLabel(field)).TextColor(selectedMuted(t, m.effectiveTokens(), reviewFieldShows(field, m.reviewField)))
			if hasProblem {
				m.reviewProblemTag(c, t, problem)
			}
		})
		m.reviewFieldControl(c, t, row, field)
		if hasProblem {
			text := reviewProblemText(problems, field)
			color := t.Warning
			if problem == reviewProblemBlocking {
				color = t.Danger
			}
			ui.Text(c, text).FontSize(t.FontSize * 0.85).TextColor(color)
		}
	})
}

// reviewProblemTag is the small "需确认" / "!" marker beside a field label.
func (m *Model) reviewProblemTag(c *ui.Context, t *ui.Theme, kind reviewProblemKind) {
	tok := m.effectiveTokens()
	if kind == reviewProblemWarning {
		ui.Text(c, "!").FontSize(t.FontSize*0.85).FontWeight(600).TextColor(tok.WarningFG).
			Border(1, tok.WarningBorder).Padding(0, t.Space(0.75)).BorderStyle(ui.BorderDashed).
			Radius(t.Space(1)).SingleLine()
		return
	}
	label := "需确认"
	if kind == reviewProblemGap {
		label = "需补全"
	}
	ui.Text(c, label).FontSize(t.FontSize*0.8).FontWeight(600).TextColor(tok.WarningFG).
		Background(tok.WarningSoft).Border(1, tok.WarningBorder).Padding(0, t.Space(1)).
		Radius(t.Space(1)).SingleLine()
}

// reviewFieldControl builds one field's editable control. The control is
// focused once when the form asks for this field, and any focus inside it
// selects the field for the screenshot highlight.
func (m *Model) reviewFieldControl(c *ui.Context, t *ui.Theme, row *Row, field FormField) {
	if m.edit == nil {
		return
	}
	draft := &m.edit.draft
	width := m.reviewFormWidth() - 2*reviewPanePaddingX - 40
	switch field {
	case FormFieldSetID:
		input := ui.Select(c, &draft.SetID, setChoices(currentID(draft.SetID))).
			Width(width).Disabled(m.reviewActionsBlocked()).
			Label("套装：" + draft.SetID)
		input = m.reviewFocus(field, input)
		m.noteReviewControlFocus(field, input.FocusWithin())
	case FormFieldRarity:
		if row.Structured != nil {
			if m.focusReviewField == field {
				m.selectReviewField(field)
			}
			ui.Text(c, row.RarityText())
			return
		}
		input := ui.Select(c, &draft.Rarity, rarityChoices(draft.Rarity)).
			Width(width).Disabled(m.reviewActionsBlocked()).
			Label("稀有度：" + draft.Rarity)
		input = m.reviewFocus(field, input)
		m.noteReviewControlFocus(field, input.FocusWithin())
	case FormFieldSlot:
		input := ui.Select(c, &draft.Slot, slotChoices(currentID(draft.Slot))).
			Width(width).Disabled(m.reviewActionsBlocked()).
			Label("槽位：" + draft.Slot)
		input = m.reviewFocus(field, input)
		m.noteReviewControlFocus(field, input.FocusWithin())
	case FormFieldLevel:
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			input := m.noteTextInputFocus(m.reviewTextInput(c, &draft.Level, 110).
				Placeholder("0–15").Disabled(m.reviewActionsBlocked()).
				Label("等级"))
			input = m.reviewFocus(field, input)
			m.noteReviewControlFocus(field, input.FocusWithin())
			ui.Text(c, "级").TextColor(selectedMuted(t, m.effectiveTokens(), reviewFieldShows(field, m.reviewField)))
		})
	case FormFieldMainStat:
		ui.Row(c).Gap(t.Space(1)).AlignItems(ui.Center).Children(func() {
			attribute := ui.Select(c, &draft.MainAttribute,
				attributeChoices(attributeTextValue(draft.MainAttribute))).
				Width(reviewMainAttributeWidth).Shrink(1).Disabled(m.reviewActionsBlocked()).Label("主词条类型")
			m.noteTextInputFocus(attribute)
			value := m.noteTextInputFocus(m.reviewTextInput(c, &draft.MainValue, reviewMainValueWidth).Placeholder("数值").Disabled(m.reviewActionsBlocked()).Label("主词条数值"))
			// The value control is the main-stat field's focus target; the
			// one-shot request also highlights its field so the form does not
			// ask for it again next frame.
			value = m.reviewFocus(field, value)
			unit := ui.Select(c, &draft.MainUnit,
				unitChoices(attributeTextValue(draft.MainAttribute), draft.MainUnit)).
				Width(reviewMainUnitWidth).Shrink(1).Disabled(m.reviewActionsBlocked()).Label("主词条单位")
			m.mainUnitBounds = unit.Bounds()
			m.noteReviewControlFocus(field, attribute.FocusWithin() || value.FocusWithin() || unit.FocusWithin())
		})
	case FormFieldSubstats, FormFieldSubstat1, FormFieldSubstat2, FormFieldSubstat3, FormFieldSubstat4:
		if field != FormFieldSubstats {
			return // the group control renders every row
		}
		m.editSubstats(c, t, m.edit)
	case FormFieldManagementState:
		input := ui.Select(c, &draft.ManagementState,
			managementChoices(attributeTextValue(draft.ManagementState))).
			Width(width).Disabled(m.reviewActionsBlocked()).Label("管理状态")
		input = m.reviewFocus(field, input)
		m.noteReviewControlFocus(field, input.FocusWithin())
	case FormFieldEquipped:
		ui.Row(c).Gap(8).Wrap().AlignItems(ui.Center).Children(func() {
			state := ui.Select(c, &draft.EquippedState,
				equippedChoices(optionValue(draft.EquippedState))).
				Width(150).Disabled(m.reviewActionsBlocked()).Label("装备状态")
			disabled := optionValue(draft.EquippedState) != string(zzz.EquippedStateEquipped)
			agent := ui.Select(c, &draft.AgentID, agentChoices(currentID(draft.AgentID))).
				Width(170).Disabled(disabled || m.reviewSaving).Label("代理人")
			if m.focusReviewField == field {
				m.selectReviewField(field)
				state.Focus()
			}
			m.noteReviewControlFocus(field, state.FocusWithin() || agent.FocusWithin())
		})
		if optionValue(draft.EquippedState) == "" {
			ui.Text(c, "未识别（可留空导出）").FontSize(t.FontSize * 0.85).TextColor(t.TextMuted)
		}
	}
}

// noteReviewControlFocus follows the keyboard between form controls: a
// newly focused control selects its field, while a click-selected field is
// not overwritten by a control that merely kept the focus.
func (m *Model) noteReviewControlFocus(field FormField, focused bool) {
	if focused {
		if m.focusedReviewField != field {
			m.focusedReviewField = field
			m.selectReviewField(field)
		}
		return
	}
	if m.focusedReviewField == field {
		m.focusedReviewField = ""
	}
}

// reviewFocus applies the one-shot focus request for a field.
func (m *Model) reviewFocus(field FormField, el ui.Element) ui.Element {
	if m.focusReviewField == field {
		// A Select's wrapper has no focus state of its own, so the
		// request also selects the field here instead of waiting for
		// FocusWithin to report it.
		m.selectReviewField(field)
		return el.Focus()
	}
	return el
}

// rarityChoices is the backfill rarity picker: the only assertable rarity
// or the explicit unread choice.
func rarityChoices(current string) []string {
	options := []string{unreadOption, zzz.RarityS}
	if current == "" || current == unreadOption {
		return options
	}
	if !containsString(options, current) {
		options = append(options, current)
	}
	return options
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// reviewReadOnlyForm is a settled item's form. Every field is text; the
// header's 修改 action opens the editor. Warning fields keep their stable
// position, their "!" marker and their export explanation (08 §3.3, §3.4).
func (m *Model) reviewReadOnlyForm(c *ui.Context, t *ui.Theme, row *Row) {
	problems := m.reviewProblems(row)
	if !m.canModifyRow(row) {
		// Queued and running items have no settled revision to correct.
		// Their placeholders are not "gaps" to fix (L2).
		problems = nil
	}
	ui.Column(c).Gap(t.Space(1.5)).Children(func() {
		for _, field := range readOnlyFormFields(problems) {
			if field == FormFieldSubstats {
				texts, known := row.SubstatTexts()
				if known && len(texts) > 0 {
					for index, value := range texts {
						m.reviewReadOnlySubstat(c, t, row, index, value)
					}
					continue
				}
			}
			m.reviewReadOnlyField(c, t, row, field, problems)
		}
		m.rawTextArea(c, t, row)
	})
}

// readOnlyFormFields keeps the v5 evidence reading order, regardless of
// warnings. Each field appears once.
func readOnlyFormFields(problems []reviewProblem) []FormField {
	return append([]FormField(nil), pairedFormFields...)
}

// reviewReadOnlyField is one label/value line of the read-only form. The
// whole block selects the field, which highlights its screenshot crop.
func (m *Model) reviewReadOnlyField(c *ui.Context, t *ui.Theme, row *Row, field FormField, problems []reviewProblem) {
	kind, hasProblem := reviewProblemKindFor(problems, field)
	block := m.reviewFieldCard(c, t, row, field)
	block.OnClick(func() { m.selectReviewField(field) })
	block.Children(func() {
		ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Start).Children(func() {
			if roles := EvidenceRolesForField(field); len(roles) > 0 {
				if number := reviewRoleNumber(roles[0]); number != "" {
					fieldNumber(c, number, reviewFieldShows(field, m.reviewField), m.effectiveTokens())
				}
			}
			ui.Text(c, formFieldLabel(field)).Width(72).
				TextColor(selectedMuted(t, m.effectiveTokens(), reviewFieldShows(field, m.reviewField)))
			if hasProblem {
				m.reviewProblemTag(c, t, kind)
			}
			if field == FormFieldSubstats {
				m.substatValue(c, t, row)
				return
			}
			value := readOnlyFieldValue(row, field, kind, hasProblem)
			if value == "" {
				// A row without structured data (queued or running) has
				// no field text yet; keep the old placeholder instead of
				// an unexplained blank line (L2).
				value = "—"
			}
			ui.Text(c, value).Grow(1).MinWidth(0)
		})
	})
}

// readOnlyFieldValue renders one persisted field as text. The equipped
// field names the optional warning the way the design does: the item is
// exported without an agent (08 §3.3, JSON/CSV v2 §3).
func readOnlyFieldValue(row *Row, field FormField, kind reviewProblemKind, hasProblem bool) string {
	switch field {
	case FormFieldSetID:
		return row.SetFormText()
	case FormFieldRarity:
		return row.RarityText()
	case FormFieldSlot:
		return row.SlotText()
	case FormFieldLevel:
		return row.LevelText()
	case FormFieldMainStat:
		return row.MainStatText()
	case FormFieldManagementState:
		return row.ManagementStateText()
	case FormFieldEquipped:
		if hasProblem && kind == reviewProblemWarning {
			return "未识别（可留空导出）"
		}
		return row.EquippedText()
	default:
		return ""
	}
}

// reviewActionBar is the fixed bottom bar. A read-only item offers only
// browsing (上一件 / 下一件); 修改 then switches to 确认修改 / 取消修改
// (08 §3.4). A pending item's editor keeps 确认并下一项 / 保存修改.
func (m *Model) reviewActionBar(c *ui.Context, t *ui.Theme, row *Row) {
	ui.Column(c).Gap(8).Children(func() {
		if row == nil {
			return
		}
		switch {
		case m.reviewEditing(row) && m.edit.modify:
			m.reviewModifyActions(c, t, row)
		case m.reviewEditing(row):
			m.reviewEditActions(c, t, row)
		default:
			m.reviewBrowseActions(c, t)
		}
		if m.reviewNotice != "" {
			ui.Text(c, m.reviewNotice).FontSize(t.FontSize * 0.85).TextColor(t.TextMuted)
		}
	})
}

// reviewEditActions is a pending item's editor bar: confirm and advance on
// the left, a plain save next to it, and the reason a disabled action
// cannot run. The reason line is always laid out, so a guard or a new
// reason never moves the buttons under the pointer (L9).
func (m *Model) reviewEditActions(c *ui.Context, t *ui.Theme, row *Row) {
	guarded := m.reviewBrowseGuarded()
	blocked := m.reviewConfirmBlockedReason(row)
	reason := blocked
	if guarded && blocked == "" {
		reason = reviewBrowseGuardReason
	}
	// The reason sits above the action row. The action bar is bottom-anchored,
	// so a reason that wraps at a larger text size grows upward instead of
	// moving the buttons (PR-19b, 09 §1).
	reviewActionReason(c, t, reason)
	actionRow(c).Children(func() {
		confirmLabel := "确认并下一项"
		if m.reviewErr != "" && m.lastAction == reviewConfirmNext {
			confirmLabel = "重试确认并下一项"
		}
		confirm := m.reviewWriteButton(primaryButton(c, confirmLabel).Key("review-confirm-next").
			Disabled(blocked != "" || m.reviewActionsBlocked()), guarded)
		if confirm.Clicked() && !guarded {
			m.saveEdit(reviewConfirmNext, nil)
		}
		saveLabel := "保存修改"
		if m.reviewErr != "" && m.lastAction == reviewSave {
			saveLabel = "重试保存"
		}
		save := m.reviewWriteButton(button(c, saveLabel).Key("review-save").
			Disabled(!m.edit.canSave() || m.reviewActionsBlocked()), guarded)
		if save.Clicked() && !guarded {
			m.saveEdit(reviewSave, nil)
		}
	})
}

// reviewModifyActions is the modify flow of a settled item: 确认修改
// re-confirms the revision (F-20), 取消修改 returns to the text form
// without saving (08 §3.4). A clean draft cannot be confirmed, and says
// why beside the disabled button (09 §4.1).
func (m *Model) reviewModifyActions(c *ui.Context, t *ui.Theme, row *Row) {
	guarded := m.reviewBrowseGuarded()
	reason := m.reviewConfirmBlockedReason(row)
	if reason == "" && !m.edit.dirty() {
		reason = "没有修改，确认修改不可用。"
	}
	disabled := reason != "" || !m.edit.dirty() || m.reviewActionsBlocked()
	if guarded && reason == "" {
		reason = reviewBrowseGuardReason
	}
	reviewActionReason(c, t, reason)
	actionRow(c).Children(func() {
		confirmLabel := "确认修改"
		if m.reviewErr != "" && m.lastAction == reviewConfirmModify {
			confirmLabel = "重试确认修改"
		}
		confirm := m.reviewWriteButton(primaryButton(c, confirmLabel).Key("review-confirm-modify").
			Disabled(disabled), guarded)
		if confirm.Clicked() && !guarded {
			m.saveEdit(reviewConfirmModify, nil)
		}
		cancel := button(c, "取消修改").Key("review-cancel-modify").
			Disabled(m.reviewActionsBlocked())
		if cancel.Clicked() {
			m.cancelEdit()
		}
	})
}

// reviewActionReason draws the action bar's reason line, or the empty
// space it keeps when there is none. The line sits above the bottom-anchored
// buttons, so a reason appearing, clearing or wrapping does not move them.
func reviewActionReason(c *ui.Context, t *ui.Theme, reason string) {
	if reason == "" {
		// The invisible line keeps the bar's height stable while no reason is
		// shown. Its exact height may differ from a real line by a sub-pixel
		// amount; the source of that difference is not identified.
		ui.Text(c, "\u00a0").FontSize(t.FontSize * 0.85).Role(ui.RoleNone)
		return
	}
	ui.Text(c, reason).FontSize(t.FontSize * 0.85).TextColor(t.TextMuted)
}

// reviewWriteButton makes a write button inert to the pointer while the
// browse guard is active, without disabling it: a key pressed on the
// focused button is deliberate and ends the guard, and the button then
// reports the click. The pointer press is consumed so a repeated click at
// the browse position neither writes nor moves the keyboard to the write
// button.
func (m *Model) reviewWriteButton(button ui.Element, guarded bool) ui.Element {
	if !guarded {
		return button
	}
	// The committed box locates a pointer event within the window without
	// waiting for a frame: a move and click that arrive together must still
	// count as leaving the old point.
	bounds := button.Bounds()
	return button.Opacity(0.55).HandleInput(func(ev ui.InputEvent) bool {
		switch ev.Kind {
		case ui.InputKeyDown:
			// Only the button's own activation keys are deliberate. Tab and
			// other navigation keys must not retire the guard while the
			// pointer still sits on the reused position (L-C).
			if ev.Key == ui.KeySpace || ev.Key == ui.KeyEnter {
				m.clearReviewBrowseGuard()
			}
			return false
		case ui.InputPointerDown:
			if !m.reviewBrowseGuardRect.Contains(bounds.X+ev.X, bounds.Y+ev.Y) {
				m.clearReviewBrowseGuard()
				return false
			}
			// The press moved the keyboard to the muted button before this
			// handler ran. Because the click is ignored, put it back on the
			// form: a following Space or Enter then edits instead of
			// confirming an item the user has only just reached.
			field := m.focusedReviewField
			if field == "" {
				field = m.firstReviewField()
			}
			m.requestReviewFieldFocus(field)
			return true
		case ui.InputPointerUp:
			return true
		}
		return false
	})
}

// reviewBrowseActions is the read-only item's only action row: previous
// and next in the current segment, both secondary (08 §3.4). Each button
// has its own identity. A read-only item keeps the keyboard on the browse
// button, so repeated Space / Enter continues browsing instead of opening
// the next item's 修改; arriving at an editor moves it to the first
// problem field (08 §3.4).
func (m *Model) reviewBrowseActions(c *ui.Context, t *ui.Theme) {
	visible := m.visibleResultRows()
	index := resultDisplayIndex(visible, m.selected)
	// Keep the browse and editor bars the same height so their first
	// buttons share the position the guard protects (08 §3.4). The reserved
	// reason line is above the buttons for the same bottom-anchored layout
	// as the editor bars.
	reviewActionReason(c, t, "")
	actionRow(c).Children(func() {
		previous := button(c, "‹ 上一件").Key("review-prev").
			Disabled(m.reviewSaving || index <= 0)
		if previous.Clicked() && m.selectAdjacentVisible(-1) {
			m.markReviewBrowse(previous)
		}
		next := button(c, "下一件 ›").Key("review-next").
			Disabled(m.reviewSaving || index < 0 || index+1 >= len(visible))
		if next.Clicked() && m.selectAdjacentVisible(1) {
			m.markReviewBrowse(next)
		}
	})
}

// rawTextArea is the per-item disclosure of the recognized text and the
// revision history. The form owns it, so the screenshot pane stays a crop
// list.
func (m *Model) rawTextArea(c *ui.Context, t *ui.Theme, row *Row) {
	ui.Divider(c)
	label := fmt.Sprintf("原始识别文字与修订（%d 条）", len(row.Lines))
	ui.Collapsible(c, label, &m.formRawOpen, func() {
		ui.Column(c).Gap(t.Space(0.5)).Children(func() {
			if row.Backfilled {
				ui.Text(c, "原始处理结局："+row.OriginalOutcomeText()).TextColor(t.TextMuted)
			}
			if len(row.Lines) == 0 {
				ui.Text(c, "这件没有识别文字。").TextColor(t.TextMuted)
			}
			for i := range row.Lines {
				line := row.Lines[i]
				text := line.Text
				if text == "" {
					text = "（该行未识别出文本）"
				}
				ui.Textf(c, "%s：%s", lineRoleText(line.Role, i), text).
					FontSize(t.FontSize * 0.85).TextColor(t.TextMuted)
			}
			ui.Textf(c, "当前修订：%d", row.Revision).
				FontSize(t.FontSize * 0.85).TextColor(t.TextMuted)
		})
	})
}

// navigationPrompt is the unsaved-changes dialog (08 §9). It is a modal
// over the whole window; Save and Continue is primary, Discard and Stay
// stay secondary, and Stay keeps the default focus and Escape.
func (m *Model) navigationPrompt(c *ui.Context, t *ui.Theme) {
	if m.navPrompt == nil {
		return
	}
	open := true
	m.modal(c, &open, func() {
		ui.Column(c).Width(420).Gap(16).Children(func() {
			seq := m.editSeqForPrompt()
			_, failedConfirm := m.failedConfirmationAction()
			title := "有未保存的修改"
			if failedConfirm {
				title = "确认没有完成"
			}
			if seq > 0 {
				if failedConfirm {
					title = fmt.Sprintf("第 %d 件还没有确认", seq)
				} else {
					title = fmt.Sprintf("第 %d 件有未保存的修改", seq)
				}
			}
			ui.Text(c, title).FontWeight(600)
			if failedConfirm {
				ui.Text(c, "上次确认没有完成。重试确认后继续，或放弃这次确认。").TextColor(t.TextMuted).
					FontSize(t.FontSize * 0.9)
			} else {
				ui.Text(c, "保存后继续，或放弃这次修改。").TextColor(t.TextMuted).
					FontSize(t.FontSize * 0.9)
			}
			if !failedConfirm && m.edit != nil && m.edit.modify {
				// A confirmed item may only leave through a new
				// confirmation; a plain save drops it back into the
				// review queue. Say so before the user chooses
				// (08 §3.4, F-20, JSON/CSV v2 §3).
				ui.Text(c, "保存后这件退回待校对，确认前不会导出。").TextColor(t.Warning).
					FontSize(t.FontSize * 0.9)
			}
			actionRow(c).Children(func() {
				if primaryButton(c, "保存并继续").Disabled(m.reviewActionsBlocked()).Clicked() {
					m.promptSaveAndContinue()
				}
				if button(c, "放弃修改").Disabled(m.reviewSaving).Clicked() {
					m.promptDiscardAndContinue()
				}
				stay := button(c, "留在这里").Disabled(m.reviewSaving).AutoFocus()
				if stay.Clicked() {
					m.promptStay()
				}
			})
			if m.reviewSaving {
				ui.Text(c, "正在保存……").TextColor(t.TextMuted)
			}
			if m.reviewErr != "" {
				ui.Text(c, m.reviewErr).TextColor(t.Danger)
			}
		})
	})
	if !open && m.navPrompt != nil {
		m.promptStay()
	}
}

// editSeqForPrompt names the item in the unsaved dialog.
func (m *Model) editSeqForPrompt() int64 {
	if m.edit != nil {
		return m.edit.seq
	}
	if m.rec != nil && m.selected >= 0 && m.selected < len(m.rec.Rows) {
		return m.rec.Rows[m.selected].Seq
	}
	return 0
}

// resultDetailTitle is the item identity the form names: sequence, set,
// slot and level, never a fabricated value for a row without a structured
// interpretation.
func resultDetailTitle(row *Row) string {
	parts := []string{fmt.Sprintf("#%04d", row.Seq)}
	if set := row.SetText(); set != "" {
		parts = append(parts, set)
	}
	if slot := row.SlotText(); slot != "" {
		parts = append(parts, slot)
	}
	if level := row.LevelText(); level != "" {
		parts = append(parts, level)
	}
	if len(parts) == 1 {
		parts = append(parts, row.StatusText())
	}
	return strings.Join(parts, " · ")
}

// resultIssueColor is the problem subtitle's color.
func resultIssueColor(t *ui.Theme, row *Row) ui.Color {
	if row.Status == record.StatusFailed {
		return t.Danger
	}
	return t.TextMuted
}

// issueSummaryText is the compact count of an issue list.
func issueSummaryText(issues []zzz.Issue) string {
	if len(issues) == 0 {
		return "无问题"
	}
	errors, warnings := 0, 0
	for _, issue := range issues {
		if issue.Severity == zzz.SeverityError {
			errors++
		} else {
			warnings++
		}
	}
	switch {
	case errors == 0:
		return fmt.Sprintf("%d 个警告", warnings)
	case warnings == 0:
		return fmt.Sprintf("%d 个问题（%d 个错误）", len(issues), errors)
	default:
		return fmt.Sprintf("%d 个问题（%d 个错误，%d 个警告）", len(issues), errors, warnings)
	}
}

// firstCheckProblem names the first reason a row cannot be confirmed.
func firstCheckProblem(check app.DataCheck) string {
	if len(check.Blocking) > 0 {
		return check.Blocking[0].Message
	}
	if len(check.Gaps) > 0 {
		return check.Gaps[0].Message
	}
	return ""
}

// editSubstats renders the substat list and its null/confirmed-empty
// distinction.
func (m *Model) editSubstats(c *ui.Context, t *ui.Theme, edit *editState) {
	draft := &edit.draft
	row := m.selectedRow()
	if draft.SubstatsKnown {
		for index := range draft.Substats {
			field := substatFormField(index)
			remove := false
			block := m.reviewFieldCard(c, t, row, field)
			block.OnClick(func() { m.selectReviewField(field) })
			block.Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					fieldNumber(c, fmt.Sprintf("%02d", index+4), reviewFieldShows(field, m.reviewField), m.effectiveTokens())
					ui.Textf(c, "副词条 %d", index+1).TextColor(selectedMuted(t, m.effectiveTokens(), reviewFieldShows(substatFormField(index), m.reviewField)))
				})
				remove = m.editSubstatRow(c, t, index)
			})
			if remove {
				draft.Substats = append(draft.Substats[:index], draft.Substats[index+1:]...)
				break
			}
		}
	}
	ui.Column(c).Gap(8).Padding(8, 0).Children(func() {
		unknown := !draft.SubstatsKnown
		box := ui.Checkbox(c, &unknown, "数量未读取").Disabled(m.reviewActionsBlocked())
		if m.focusReviewField == FormFieldSubstats {
			m.selectReviewField(FormFieldSubstats)
			box.Focus()
		}
		if unknown != !draft.SubstatsKnown {
			draft.SubstatsKnown = !unknown
			if unknown {
				draft.Substats = nil
			}
		}
		if draft.SubstatsKnown {
			if len(draft.Substats) == 0 {
				ui.Text(c, "无（确认没有可见副词条）").TextColor(t.TextMuted)
			}
			if button(c, "添加副词条").Disabled(len(draft.Substats) >= 4 || m.reviewActionsBlocked()).Clicked() {
				draft.Substats = append(draft.Substats, substatDraft{Attribute: unreadOption, Unit: unreadOption})
			}
		}
		if text := reviewProblemText(m.reviewProblems(row), FormFieldSubstats); text != "" {
			ui.Text(c, text).FontSize(t.FontSize * 12 / 14).TextColor(t.Warning)
		}
	})
}

// editSubstatRow renders one substat's pickers and decimal boxes. It
// reports whether the row's delete button was clicked.
func (m *Model) editSubstatRow(c *ui.Context, t *ui.Theme, index int) bool {
	sub := &m.edit.draft.Substats[index]
	field := substatFormField(index)
	remove := false
	if m.focusReviewField == field {
		m.selectReviewField(field)
	}
	ui.Column(c).Gap(8).MinWidth(0).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {

			attribute := ui.Select(c, &sub.Attribute, attributeChoices(attributeTextValue(sub.Attribute))).Grow(1).MinWidth(0).Disabled(m.reviewActionsBlocked()).Label("副词条类型")
			if m.focusReviewField == field {
				attribute.Focus()
			}
			m.noteReviewControlFocus(field, attribute.FocusWithin())
			if button(c, "×").Width(32).Disabled(m.reviewActionsBlocked()).Clicked() {
				remove = true
			}
		})
		ui.Row(c).Gap(8).Wrap().AlignItems(ui.Center).Children(func() {
			value := m.noteTextInputFocus(m.reviewTextInput(c, &sub.Value, 90).Placeholder("数值").Disabled(m.reviewActionsBlocked()).Label("副词条数值"))
			unit := ui.Select(c, &sub.Unit, unitChoices(attributeTextValue(sub.Attribute), sub.Unit)).Width(120).Disabled(m.reviewActionsBlocked()).Label("副词条单位")
			ui.Text(c, "强化").TextColor(selectedMuted(t, m.effectiveTokens(), reviewFieldShows(field, m.reviewField)))
			rolls := m.noteTextInputFocus(m.reviewTextInput(c, &sub.Rolls, 60).Placeholder("档位").Disabled(m.reviewActionsBlocked()).Label("强化档位"))
			m.noteReviewControlFocus(field, value.FocusWithin() || unit.FocusWithin() || rolls.FocusWithin())
		})
	})
	return remove
}

// currentID reads a numeric picker label back to its id.
func currentID(label string) *int {
	value := optionValue(label)
	if value == "" {
		return nil
	}
	id, ok := parseDecimalInt(value)
	if !ok {
		return nil
	}
	return &id
}

// attributeTextValue reads a non-numeric picker label back to its value.
func attributeTextValue(label string) *string {
	value := optionValue(label)
	if value == "" {
		return nil
	}
	return &value
}

// reviewFieldLabel renders one data field path for error lists.
func reviewFieldLabel(field string) string {
	if field == "" {
		return "整件"
	}
	if form := canonicalFormField(field); form != "" {
		return formFieldLabel(form)
	}
	if strings.HasPrefix(field, "substats[") {
		index := strings.TrimSuffix(strings.TrimPrefix(field, "substats["), "]")
		if n, err := strconv.Atoi(index); err == nil {
			return fmt.Sprintf("副词条 %d", n+1)
		}
	}
	return field
}

// substatValue is the substat group's value column of the read-only form;
// a null list (unknown line count) stays distinct from a confirmed empty
// one.
func (m *Model) substatValue(c *ui.Context, t *ui.Theme, row *Row) {
	ui.Column(c).Grow(1).Gap(t.Space(0.5)).Children(func() {
		texts, known := row.SubstatTexts()
		switch {
		case !known:
			ui.Text(c, "未读取")
		case len(texts) == 0:
			ui.Text(c, "无（确认没有可见副词条）")
		default:
			for i, text := range texts {
				ui.Textf(c, "%d. %s", i+1, text)
			}
		}
	})
}

func (m *Model) reviewFormNotices(c *ui.Context, t *ui.Theme) {
	row := m.selectedRow()
	if row == nil {
		return
	}
	tok := m.effectiveTokens()
	status := passedStatusLabel(row)

	if status != "" {
		m.reviewPassedTag(c, t, tok, status)
	}
	if notice := backfillNotice(row); notice != "" {
		ui.Text(c, notice).FontSize(t.FontSize * reviewHeaderSizeRatio).TextColor(t.Warning)
	}
	if notice := warningNotice(row); notice != "" {
		ui.Column(c).Padding(t.Space(1.5), t.Space(2)).Gap(t.Space(0.5)).
			Background(tok.WarningSoft).Border(1, tok.WarningBorder).Radius(controlRadius).Children(func() {
			ui.Text(c, notice).TextColor(tok.WarningFG).FontSize(t.FontSize * reviewHeaderSizeRatio)
		})
	}
	if m.reviewErr != "" {
		ui.Column(c).Padding(t.Space(1.5), t.Space(2)).Gap(t.Space(1)).
			Background(tok.DangerSoft).Border(1, tok.DangerBorder).Radius(t.Radius).Children(func() {
			ui.Row(c).Gap(t.Space(1)).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "没有保存："+m.reviewErr).TextColor(t.Danger).Shrink(1)
				retry := button(c, m.retryActionLabel()).Key("review-retry").
					Disabled(m.reviewSaving)
				if retry.Clicked() {
					m.saveEdit(m.retryAction(), nil)
				}
			})
		})
	}
	if m.loading {
		ui.Text(c, "正在加载最新检查点，加载完成前不能编辑、保存或确认。").TextColor(t.TextMuted)
	}
	if m.exportBusy() {
		ui.Text(c, "正在导出，完成前不能编辑或确认。").TextColor(t.TextMuted)
	}
}

func (m *Model) reviewFormWidth() float32 {
	if m.reviewStandard() {
		return reviewFormWidth
	}
	return 400
}
func (m *Model) reviewDefaultZoom() float32 {
	if m.reviewStandard() {
		return 1.5
	}
	return 1.35
}
func widthForSubstat(m *Model) float32 { return m.reviewFormWidth() - 2*reviewPanePaddingX - 24 }
func reviewRoleNumber(role string) string {
	switch role {
	case "name":
		return "01"
	case "level":
		return "02"
	case "main":
		return "03"
	case "sub1":
		return "04"
	case "sub2":
		return "05"
	case "sub3":
		return "06"
	case "sub4":
		return "07"
	}
	return ""
}
func (m *Model) reviewRowHeight(row *Row, role string) float32 {
	height := float32(72) * max(1, m.textScale)
	if row != nil && m.rec != nil {
		for _, ev := range row.Evidence {
			if ev.Role != role {
				continue
			}
			entry := m.cache.entries[evidenceKey{record: m.rec.ID, id: ev.ID}]
			if entry == nil || entry.bitmap == nil {
				continue
			}
			w, h := entry.bitmap.Size()
			if w > 0 {
				width := m.reviewCropWidth(w)
				height = max(height, width*float32(h)/float32(w)+44)
			}
		}
	}
	return float32(math.Ceil(float64(max(height, m.pairedHeights[role]))/4) * 4)
}
func (m *Model) reviewFieldCard(c *ui.Context, t *ui.Theme, row *Row, field FormField) ui.Element {
	tok := m.effectiveTokens()
	selected := reviewFieldShows(field, m.reviewField)
	bg, border := tok.SurfaceCard, tok.BorderSubtle
	if selected {
		bg, border = tok.AccentSoft, tok.AccentFG
	}
	role := ""
	if roles := EvidenceRolesForField(field); len(roles) > 0 {
		role = roles[0]
	}
	identity := m.pairedHeightIdentity
	block := ui.Column(c).Key(reviewFieldKey{identity.record, identity.seq, field, identity.editing}).FillWidth().MinWidth(0).Gap(8).Padding(12).MinHeight(m.reviewRowHeight(row, role)).Background(bg).Border(1, border).Radius(controlRadius)
	if bounds := block.Bounds(); m.pairedMeasureReady && role != "" && bounds.H > m.pairedHeights[role]+.5 {
		m.pairedHeights[role] = float32(math.Ceil(float64(bounds.H)/4) * 4)
		c.AnimationFrame()
	}
	return block
}

// Paired evidence fields lead both forms. Less frequent identity and state
// fields remain fully editable below them, with their original candidates.
var pairedFormFields = []FormField{FormFieldSetID, FormFieldLevel, FormFieldMainStat, FormFieldSubstats, FormFieldRarity, FormFieldSlot, FormFieldManagementState, FormFieldEquipped}

type reviewHeightIdentity struct {
	record      string
	seq         int64
	width       int
	scale, zoom float32
	editing     bool
}
type reviewFieldKey struct {
	record   string
	sequence int64
	field    FormField
	editing  bool
}

func (m *Model) preparePairedHeights(row *Row) {
	identity := reviewHeightIdentity{width: m.contentWidth, scale: m.textScale, zoom: m.cropZoom}
	if row != nil && m.rec != nil {
		identity.record = m.rec.ID
		identity.seq = row.Seq
		identity.editing = m.reviewEditing(row)
	}
	if identity != m.pairedHeightIdentity || m.pairedHeights == nil {
		m.pairedHeightIdentity = identity
		m.pairedHeights = make(map[string]float32, 7)
		m.pairedMeasureReady = false
	}
}
func (m *Model) reviewReadOnlySubstat(c *ui.Context, t *ui.Theme, row *Row, index int, value string) {
	field := substatFormField(index)
	block := m.reviewFieldCard(c, t, row, field)
	block.OnClick(func() { m.selectReviewField(field) })
	block.Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Start).Children(func() {
			fieldNumber(c, fmt.Sprintf("%02d", index+4), reviewFieldShows(field, m.reviewField), m.effectiveTokens())
			ui.Textf(c, "副词条 %d", index+1).TextColor(selectedMuted(t, m.effectiveTokens(), reviewFieldShows(substatFormField(index), m.reviewField))).Shrink(0)
			ui.Text(c, value).Grow(1).MinWidth(0)
		})
	})
}
