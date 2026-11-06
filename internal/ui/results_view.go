package ui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/game/zzz"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/egoist/mygo/ui"
)

// The review layout's fixed metrics (08 §3.1, 09 §2): a 240-DIP item list,
// a 400-DIP form and the screenshots in between, never under 360 DIP. The
// standard profile shows the list as a column; the compact profile folds it
// into the top strip. Both keep screenshots and form side by side.
const (
	reviewListWidth     = 240
	reviewFormWidth     = 400
	reviewMinCropsWidth = 360
	reviewStandardWidth = 1120
	reviewListRowHeight = 52
	reviewPagePadding   = 12
	reviewPanePaddingX  = 12
	reviewPanePaddingY  = 10
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

// reviewStandard reports the standard width profile (window ≥ 1120 DIP):
// the item list is its own column. Below it the compact profile puts the
// list into the top strip.
func (m *Model) reviewStandard() bool {
	return m.contentWidth >= reviewStandardWidth
}

// reviewInnerWidth is the width the page leaves for its panes: the window
// less the navigation rail and the page padding.
func (m *Model) reviewInnerWidth() float32 {
	width := float32(m.contentWidth) - navRailWidth - 2*reviewPagePadding
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
		width -= reviewListWidth + 1
	}
	width -= reviewFormWidth + 1
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
			return
		}
		m.reviewArea(c, t)
	})
}

// reviewArea is the three-region review. The list is a column at standard
// widths and a top strip at compact widths; the screenshot pane and the
// 400-DIP form keep their places at every width.
func (m *Model) reviewArea(c *ui.Context, t *ui.Theme) {
	visible := m.visibleResultRows()
	m.settleResultSelection()
	if m.reviewField == "" && m.selectedRow() != nil {
		// Entering the review (or returning to it) starts on the first
		// problem field (08 §3).
		m.requestReviewFieldFocus("")
	}
	ui.Column(c).Grow(1).FillHeight().MinHeight(0).Children(func() {
		if !m.reviewStandard() {
			m.reviewCompactStrip(c, t, visible)
			ui.Divider(c)
		}
		ui.Row(c).Grow(1).FillHeight().MinHeight(0).Children(func() {
			if m.reviewStandard() {
				m.reviewListPane(c, t, visible)
				ui.Divider(c)
			}
			m.reviewCropsPane(c, t, visible)
			ui.Divider(c)
			m.reviewFormPane(c, t, visible)
		})
	})
	m.recordReviewLayout(c)
}

// recordReviewLayout stores the pane widths the frame laid out, for the
// profile checks.
func (m *Model) recordReviewLayout(c *ui.Context) {
	layout := reviewLayoutWidths{Standard: m.reviewStandard()}
	if layout.Standard {
		layout.List = m.reviewListRef.Bounds(c).W
		layout.ListFocused = m.list.Focused(c)
	}
	layout.Crops = m.reviewCropsRef.Bounds(c).W
	layout.Form = m.reviewFormRef.Bounds(c).W
	if layout.List == 0 && layout.Crops == 0 && layout.Form == 0 {
		return
	}
	m.reviewLayout = layout
}

// reviewListPane is the standard profile's item list column.
func (m *Model) reviewListPane(c *ui.Context, t *ui.Theme, visible []int) {
	_, tok := themeWithTokens(t, c.Preferences())
	ui.Column(c).Width(reviewListWidth).Shrink(0).FillHeight().MinHeight(0).
		Background(tok.SurfaceContent).Bind(&m.reviewListRef).Children(func() {
		ui.Column(c).Padding(reviewPanePaddingY, reviewPanePaddingX).Gap(t.Space(1)).
			Children(func() {
				m.resultsFilterBar(c, t)
				m.resultsFacetFilters(c, t, reviewListWidth-2*reviewPanePaddingX)
			})
		ui.Divider(c)
		if len(visible) == 0 {
			ui.Column(c).Grow(1).Padding(t.Space(2), reviewPanePaddingX).Gap(t.Space(1)).
				Children(func() {
					ui.Text(c, m.filteredEmptyText()).TextColor(t.TextMuted)
					if m.hasResultFacets() && ui.Button(c, "清除筛选").Clicked() {
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
			m.reviewCategoryTag(c, t, tok, r, selected)
		})
		ui.Text(c, r.labelSetSlot()).FontSize(t.FontSize * reviewHeaderSizeRatio).
			TextColor(t.TextMuted).SingleLine()
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
func (m *Model) reviewCategoryTag(c *ui.Context, t *ui.Theme, tok *Tokens, row *Row, selected bool) {
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
		background, border, text = ui.Transparent, tok.WarningBorder, tok.WarningFG
		dashed = true
	case row.ItemClass == app.ItemClassPassed:
		background, border, text = ui.Transparent, tok.SuccessBorder, tok.SuccessFG
	default:
		switch row.Status {
		case record.StatusFailed:
			background, border, text = tok.DangerSoft, tok.DangerBorder, tok.DangerFG
		case record.StatusUnprocessed:
			dashed = true
		}
	}
	if selected {
		background, border, text = tok.AccentSoft, tok.AccentBorder, tok.AccentFG
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
	_, tok := themeWithTokens(t, c.Preferences())
	ui.Row(c).Padding(t.Space(1.5), reviewPanePaddingX).Gap(t.Space(1.5)).
		AlignItems(ui.Center).Background(tok.SurfaceHeader).Children(func() {
		position := resultDisplayIndex(visible, m.selected)
		back := ui.Button(c, "‹").Disabled(position <= 0)
		if back.Clicked() {
			m.selectAdjacentVisible(-1)
		}
		ui.Text(c, m.compactPositionText(visible)).FontWeight(600).SingleLine().Shrink(1)
		forward := ui.Button(c, "›").Disabled(position < 0 || position >= len(visible)-1)
		if forward.Clicked() {
			m.selectAdjacentVisible(1)
		}
		ui.Box(c).Grow(1)
		m.resultsFilterBar(c, t)
		m.compactItemPopover(c, t, visible)
	})
}

// compactItemPopover is the "全部件" popover: the same rows as the standard
// list, without the 240-DIP column. It carries the set/slot facets and uses
// a virtualized List, so 10,000 rows build only the visible frame.
func (m *Model) compactItemPopover(c *ui.Context, t *ui.Theme, visible []int) {
	anchor := ui.Button(c, "全部件 ▾").Bind(&m.reviewCompactRef)
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
	_, tok := themeWithTokens(t, c.Preferences())
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
			m.reviewCategoryTag(c, t, tok, r, selected)
		})
		ui.Text(c, r.labelSetSlot()).FontSize(t.FontSize * reviewHeaderSizeRatio).
			TextColor(t.TextMuted).SingleLine()
	})
}

// reviewCropsPane is the screenshots: one crop per observed line in the
// game panel's reading order, then the context crops. Selecting a form
// field highlights its crops and scrolls the first into view; a record
// without line roles falls back to capture order without a highlight.
func (m *Model) reviewCropsPane(c *ui.Context, t *ui.Theme, visible []int) {
	_, tok := themeWithTokens(t, c.Preferences())
	row := m.selectedRow()
	ui.Column(c).Grow(1).Shrink(1).MinWidth(reviewMinCropsWidth).FillHeight().MinHeight(0).
		Background(tok.SurfaceContent).Children(func() {
		ui.Row(c).Padding(t.Space(2), reviewPanePaddingX).Gap(t.Space(2)).
			AlignItems(ui.Center).Children(func() {
			ui.Text(c, "截图（逐行）").FontWeight(600).SingleLine()
			ui.Box(c).Grow(1)
			if row != nil {
				m.reviewZoomControls(c, t)
			}
		})
		ui.Divider(c)
		scroll := ui.ScrollBoth(c).Grow(1).FillHeight().MinHeight(0).Bind(&m.reviewCropsRef)
		if m.reviewRegion == reviewRegionCrops {
			scroll.Focus()
		}
		scroll.Children(func() {
			ui.Column(c).Padding(reviewPanePaddingY, reviewPanePaddingX).Gap(t.Space(2)).Children(func() {
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
		label = fmt.Sprintf("%d%%", int(m.cropZoom*100+0.5))
	}
	if ui.Button(c, "缩小").Disabled(m.cropZoom == 0).Clicked() {
		m.zoomCrop(-reviewZoomStep)
	}
	if ui.Button(c, "放大").Clicked() {
		m.zoomCrop(reviewZoomStep)
	}
	if ui.Button(c, label).Clicked() {
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
	_, tok := themeWithTokens(t, c.Preferences())
	background := tok.SurfaceContent
	border := ui.Transparent
	if highlighted {
		background, border = tok.AccentSoft, tok.AccentBorder
	}
	block := ui.Column(c).Gap(t.Space(1)).Padding(t.Space(1)).
		Background(background).Border(1, border).Radius(t.Space(1))
	if scrollIntoView {
		block = block.ScrollIntoView()
	}
	block.Children(func() {
		label := crop.roleLabel()
		text := ui.Text(c, label).FontSize(t.FontSize * reviewHeaderSizeRatio).SingleLine()
		if highlighted {
			text.TextColor(tok.AccentFG).FontWeight(600)
		} else {
			text.TextColor(t.TextMuted)
		}
		if crop.evidence == nil {
			ui.Text(c, "这一行缺少截图。").FontSize(t.FontSize * reviewHeaderSizeRatio).
				TextColor(t.TextMuted)
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
		ui.Text(c, "截图加载中……").TextColor(t.TextMuted)
	case entry.err != nil:
		ui.Textf(c, "截图不可用：%v", entry.err).TextColor(t.Danger)
	default:
		width := m.reviewCropImageWidth()
		if m.cropZoom > 0 {
			width *= m.cropZoom
			entry.bitmap.Pixelated()
		}
		imageWidth, imageHeight := entry.bitmap.Size()
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

// reviewFormPane is the 400-DIP form. Its content scrolls inside the pane;
// the action bar stays fixed at the bottom at every height.
func (m *Model) reviewFormPane(c *ui.Context, t *ui.Theme, visible []int) {
	_, tok := themeWithTokens(t, c.Preferences())
	row := m.selectedRow()
	// A zero pending queue is the finished review even when the list still
	// shows the last confirmed row for this visit. The list keeps its place;
	// the form states the conclusion and offers the export.
	if m.filter == resultFilterPending && m.reviewSettled() {
		row = nil
	}
	ui.Column(c).Width(reviewFormWidth).Shrink(0).FillHeight().MinHeight(0).
		Background(tok.SurfaceContent).Bind(&m.reviewFormRef).Children(func() {
		ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).Padding(reviewPanePaddingY, t.Space(4)).Gap(t.Space(2.5)).Children(func() {
				if row == nil {
					m.reviewEmptyForm(c, t, visible)
					return
				}
				m.ensureReviewEdit(row)
				m.reviewFormHeader(c, t, row)
				if m.edit != nil && m.rec != nil &&
					m.edit.recordID == m.rec.ID && m.edit.seq == row.Seq {
					m.reviewEditForm(c, t, row)
				} else {
					m.reviewReadOnlyForm(c, t, row)
				}
			})
		})
		// The one-shot focus request belongs to the frame that built the
		// field it names; later Tab moves are the user's.
		m.focusReviewField = ""
		if row != nil {
			ui.Divider(c)
			m.reviewActionBar(c, t, row)
		}
	})
}

// reviewEmptyForm is the form pane with no chosen item. Following the
// design, an empty pending queue is a finished review, not a blank page.
func (m *Model) reviewEmptyForm(c *ui.Context, t *ui.Theme, visible []int) {
	if m.filter == resultFilterPending && m.reviewSettled() {
		counts := m.recordCounts()
		ui.Text(c, "都处理完了").FontWeight(600)
		if counts.ExportKnown {
			ui.Textf(c, "可导出 %d 件 · 排除 %d 件", counts.Exportable, counts.Captured-counts.Exportable)
		}
		if ui.PrimaryButton(c, "导出…").Disabled(m.exportEntryBlocked()).Clicked() {
			m.requestExport()
		}
		if ui.Button(c, "删除这条记录的截图…").Clicked() {
			m.RequestScreenshotCleanup(m.rec.ID)
		}
		return
	}
	ui.Text(c, m.filteredEmptyText()).TextColor(t.TextMuted)
	if m.hasResultFacets() {
		if ui.Button(c, "清除筛选").Clicked() {
			m.clearResultFacets()
		}
	}
	if ui.Button(c, "显示全部").Clicked() {
		m.setResultFilter(resultFilterAll)
	}
	_ = visible
}

// reviewFormHeader names the item and its category, then the form's state
// bands: backfill, exportable warning, save failure, loading and export.
func (m *Model) reviewFormHeader(c *ui.Context, t *ui.Theme, row *Row) {
	_, tok := themeWithTokens(t, c.Preferences())
	ui.Row(c).Gap(t.Space(1)).AlignItems(ui.Center).Children(func() {
		ui.Text(c, resultDetailTitle(row)).FontWeight(600).SingleLine().Shrink(1)
		ui.Box(c).Grow(1)
		m.reviewCategoryTag(c, t, tok, row, false)
	})
	if notice := backfillNotice(row); notice != "" {
		ui.Text(c, notice).FontSize(t.FontSize * reviewHeaderSizeRatio).TextColor(t.Warning)
	}
	if notice := warningNotice(row); notice != "" {
		ui.Column(c).Padding(t.Space(1.5), t.Space(2)).Gap(t.Space(0.5)).
			Background(t.Surface).Radius(t.Radius).Children(func() {
			ui.Text(c, notice).FontSize(t.FontSize * reviewHeaderSizeRatio)
		})
	}
	if m.reviewErr != "" {
		ui.Column(c).Padding(t.Space(1.5), t.Space(2)).Gap(t.Space(1)).
			Background(tok.DangerSoft).Border(1, tok.DangerBorder).Radius(t.Radius).Children(func() {
			ui.Row(c).Gap(t.Space(1)).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "没有保存："+m.reviewErr).TextColor(t.Danger).Shrink(1)
				if ui.Button(c, m.retryActionLabel()).Disabled(m.reviewSaving).Clicked() {
					m.saveEdit(m.lastAction, nil)
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

// retryActionLabel names the failed action the error band retries.
func (m *Model) retryActionLabel() string {
	switch m.lastAction {
	case reviewConfirmStay:
		return "重试确认"
	case reviewConfirmNext:
		return "重试确认并下一项"
	default:
		return "重试保存"
	}
}

// reviewEditForm is the editable form: problem fields first with their
// reasons, the rest folded away, then the raw text and revision history.
func (m *Model) reviewEditForm(c *ui.Context, t *ui.Theme, row *Row) {
	if m.edit == nil {
		return
	}
	m.edit.refresh()
	draft := &m.edit.draft
	problems := m.reviewProblems(row)
	first, rest := reviewFormFields(problems)
	for _, field := range first {
		m.reviewEditField(c, t, row, field, problems)
	}
	if len(rest) > 0 {
		if len(first) == 0 {
			// Nothing is wrong with this item: with no problem field to
			// lead with, the rest of the form is the form.
			for _, field := range rest {
				m.reviewEditField(c, t, row, field, problems)
			}
		} else {
			label := fmt.Sprintf("其余 %d 个字段", len(rest))
			ui.Collapsible(c, label, &m.formExtrasOpen, func() {
				ui.Column(c).Gap(t.Space(2)).Children(func() {
					for _, field := range rest {
						m.reviewEditField(c, t, row, field, problems)
					}
				})
			})
		}
	}
	m.rawTextArea(c, t, row)
	_ = draft
}

// reviewEditField is one editable field: its label, the control, and the
// reason under it. The whole block is a click target for the screenshots.
func (m *Model) reviewEditField(c *ui.Context, t *ui.Theme, row *Row, field FormField, problems []reviewProblem) {
	if m.edit == nil {
		return
	}
	problem, hasProblem := reviewProblemKindFor(problems, field)
	block := ui.Column(c).Gap(t.Space(1)).Padding(t.Space(0.5), 0)
	if reviewFieldShows(field, m.reviewField) {
		block = block.Padding(t.Space(1)).Background(t.Surface).Radius(t.Radius)
	}
	block.OnClick(func() { m.selectReviewField(field) })
	block.Children(func() {
		ui.Row(c).Gap(t.Space(1)).AlignItems(ui.Center).Children(func() {
			ui.Text(c, formFieldLabel(field)).Width(100).TextColor(t.TextMuted)
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
	_, tok := themeWithTokens(t, c.Preferences())
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
	width := float32(reviewFormWidth - 2*reviewPanePaddingX - 14)
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
		input := m.noteTextInputFocus(ui.TextInput(c, &draft.Level).Width(110).
			Placeholder("0–15").Disabled(m.reviewActionsBlocked()).
			Label("等级"))
		input = m.reviewFocus(field, input)
		m.noteReviewControlFocus(field, input.FocusWithin())
	case FormFieldMainStat:
		ui.Row(c).Gap(t.Space(1)).AlignItems(ui.Center).Children(func() {
			attribute := ui.Select(c, &draft.MainAttribute,
				attributeChoices(attributeTextValue(draft.MainAttribute))).
				Width(reviewMainAttributeWidth).Shrink(1).Disabled(m.reviewActionsBlocked()).Label("主词条类型")
			m.noteTextInputFocus(attribute)
			value := m.noteTextInputFocus(ui.TextInput(c, &draft.MainValue).
				Width(reviewMainValueWidth).Shrink(1).Placeholder("数值").Disabled(m.reviewActionsBlocked()).Label("主词条数值"))
			if m.focusReviewField == field {
				value.Focus()
			}
			unit := ui.Select(c, &draft.MainUnit,
				unitChoices(attributeTextValue(draft.MainAttribute), attributeTextValue(draft.MainUnit))).
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
		ui.Row(c).Gap(t.Space(1)).AlignItems(ui.Center).Children(func() {
			state := ui.Select(c, &draft.EquippedState,
				equippedChoices(optionValue(draft.EquippedState))).
				Width(150).Disabled(m.reviewActionsBlocked()).Label("装备状态")
			disabled := optionValue(draft.EquippedState) != string(zzz.EquippedStateEquipped)
			agent := ui.Select(c, &draft.AgentID, agentChoices(currentID(draft.AgentID))).
				Width(170).Disabled(disabled || m.reviewSaving).Label("代理人")
			if m.focusReviewField == field {
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
// header's 修改 action opens the editor. The full read-only design for
// passed and warning items (08 §3.3, §3.4) is PR-19a.
func (m *Model) reviewReadOnlyForm(c *ui.Context, t *ui.Theme, row *Row) {
	ui.Column(c).Gap(t.Space(1.5)).Children(func() {
		fields := []struct {
			field FormField
			build func()
		}{
			{FormFieldSetID, func() { formField(c, t, "套装", row.SetFormText()) }},
			{FormFieldRarity, func() { formField(c, t, "稀有度", row.RarityText()) }},
			{FormFieldSlot, func() { formField(c, t, "槽位", row.SlotText()) }},
			{FormFieldLevel, func() { formField(c, t, "等级", row.LevelText()) }},
			{FormFieldMainStat, func() { formField(c, t, "主词条", row.MainStatText()) }},
			{FormFieldSubstats, func() { m.substatField(c, t, row) }},
			{FormFieldManagementState, func() { formField(c, t, "管理状态", row.ManagementStateText()) }},
			{FormFieldEquipped, func() { formField(c, t, "装备", row.EquippedText()) }},
		}
		for _, entry := range fields {
			field := entry.field
			block := ui.Column(c).Gap(t.Space(0.5))
			block.OnClick(func() { m.selectReviewField(field) })
			block.Children(entry.build)
		}
		m.rawTextArea(c, t, row)
	})
}

// reviewActionBar is the fixed bottom bar: the primary "确认并下一项" on the
// left, the secondary save, and the reason a disabled action cannot run.
func (m *Model) reviewActionBar(c *ui.Context, t *ui.Theme, row *Row) {
	ui.Column(c).Padding(t.Space(2), t.Space(4)).Gap(t.Space(1)).Children(func() {
		if row == nil {
			return
		}
		editing := m.edit != nil && m.rec != nil &&
			m.edit.recordID == m.rec.ID && m.edit.seq == row.Seq
		if editing {
			reason := m.reviewConfirmBlockedReason(row)
			ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
				confirmLabel := "确认并下一项"
				if m.reviewErr != "" && m.lastAction == reviewConfirmNext {
					confirmLabel = "重试确认并下一项"
				}
				confirm := ui.PrimaryButton(c, confirmLabel).Disabled(reason != "" || m.reviewActionsBlocked())
				if confirm.Clicked() {
					m.saveEdit(reviewConfirmNext, nil)
				}
				saveLabel := "保存修改"
				if m.reviewErr != "" && m.lastAction == reviewSave {
					saveLabel = "重试保存"
				}
				save := ui.Button(c, saveLabel).Disabled(!m.edit.canSave() || m.reviewActionsBlocked())
				if save.Clicked() {
					m.saveEdit(reviewSave, nil)
				}
			})
			if reason != "" {
				ui.Text(c, reason).FontSize(t.FontSize * 0.85).TextColor(t.TextMuted)
			}
		} else {
			ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
				if ui.Button(c, "修改").Disabled(m.reviewActionsBlocked()).Clicked() {
					m.startEditRow(row)
				}
				if row.PendingReview() && ui.PrimaryButton(c, "确认并下一项").Disabled(m.reviewActionsBlocked()).Clicked() {
					m.confirmCurrent()
				}
			})
		}
		if m.reviewNotice != "" {
			ui.Text(c, m.reviewNotice).FontSize(t.FontSize * 0.85).TextColor(t.TextMuted)
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
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(420).Gap(t.Space(2)).Padding(t.Space(4)).
			Background(t.Background).Border(1, t.Border).Radius(t.Radius).Children(func() {
			seq := m.editSeqForPrompt()
			title := "有未保存的修改"
			if seq > 0 {
				title = fmt.Sprintf("第 %d 件有未保存的修改", seq)
			}
			ui.Text(c, title).FontWeight(600)
			ui.Text(c, "保存后继续，或放弃这次修改。").TextColor(t.TextMuted).
				FontSize(t.FontSize * 0.9)
			ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
				if ui.PrimaryButton(c, "保存并继续").Disabled(m.reviewActionsBlocked()).Clicked() {
					m.promptSaveAndContinue()
				}
				if ui.Button(c, "放弃修改").Disabled(m.reviewSaving).Clicked() {
					m.promptDiscardAndContinue()
				}
				stay := ui.Button(c, "留在这里").Disabled(m.reviewSaving).AutoFocus()
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
	ui.Column(c).Grow(1).Gap(t.Space(1)).Children(func() {
		unknown := !draft.SubstatsKnown
		unknownBox := ui.Checkbox(c, &unknown, "数量未读取").Disabled(m.reviewActionsBlocked())
		if m.focusReviewField == FormFieldSubstats {
			m.selectReviewField(FormFieldSubstats)
			unknownBox.Focus()
		}
		if unknown != !draft.SubstatsKnown {
			draft.SubstatsKnown = !unknown
			if !draft.SubstatsKnown {
				draft.Substats = nil
			}
		}
		if !draft.SubstatsKnown {
			return
		}
		for i := range draft.Substats {
			if m.editSubstatRow(c, t, i) {
				draft.Substats = append(draft.Substats[:i], draft.Substats[i+1:]...)
				break
			}
		}
		if len(draft.Substats) == 0 {
			ui.Text(c, "无（确认没有可见副词条）").TextColor(t.TextMuted)
		}
		if ui.Button(c, "添加副词条").Disabled(len(draft.Substats) >= 4 || m.reviewSaving).Clicked() {
			draft.Substats = append(draft.Substats, substatDraft{
				Attribute: unreadOption,
				Unit:      unreadOption,
			})
		}
	})
}

// editSubstatRow renders one substat's pickers and decimal boxes. It
// reports whether the row's delete button was clicked.
func (m *Model) editSubstatRow(c *ui.Context, t *ui.Theme, index int) bool {
	draft := &m.edit.draft
	sub := &draft.Substats[index]
	field := substatFormField(index)
	remove := false
	if m.focusReviewField == field {
		m.selectReviewField(field)
	}
	ui.Row(c).Gap(t.Space(1)).AlignItems(ui.Center).Children(func() {
		ui.Textf(c, "%d", index+1).Width(16).TextColor(t.TextMuted)
		attribute := ui.Select(c, &sub.Attribute, attributeChoices(attributeTextValue(sub.Attribute))).
			Width(116).Disabled(m.reviewActionsBlocked()).Label("副词条类型")
		if m.focusReviewField == field {
			attribute.Focus()
		}
		value := m.noteTextInputFocus(ui.TextInput(c, &sub.Value).Width(60).Placeholder("数值").
			Disabled(m.reviewActionsBlocked()).Label("副词条数值"))
		unit := ui.Select(c, &sub.Unit, unitChoices(attributeTextValue(sub.Attribute), attributeTextValue(sub.Unit))).
			Width(96).Disabled(m.reviewActionsBlocked()).Label("副词条单位")
		rolls := m.noteTextInputFocus(ui.TextInput(c, &sub.Rolls).Width(34).Placeholder("强化").
			Disabled(m.reviewActionsBlocked()).Label("强化档位"))
		m.noteReviewControlFocus(field, attribute.FocusWithin() || value.FocusWithin() ||
			unit.FocusWithin() || rolls.FocusWithin())
		if ui.Button(c, "×").Width(24).Disabled(m.reviewActionsBlocked()).Clicked() {
			remove = true
		}
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

// formField is one label/value line of the read-only form.
func formField(c *ui.Context, t *ui.Theme, label, value string) {
	if value == "" {
		value = "—"
	}
	ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Start).Children(func() {
		ui.Text(c, label).Width(100).TextColor(t.TextMuted)
		ui.Text(c, value)
	})
}

// substatField lists the substats in display order; a null list (unknown
// line count) stays distinct from a confirmed empty one.
func (m *Model) substatField(c *ui.Context, t *ui.Theme, row *Row) {
	ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Start).Children(func() {
		ui.Text(c, "副词条").Width(100).TextColor(t.TextMuted)
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
	})
}
