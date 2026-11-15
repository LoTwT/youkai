package ui

import (
	"github.com/LoTwT/youkai/internal/record"
	"github.com/egoist/mygo/ui"
)

// recordTab selects one view of an opened record. The workspace is the
// records destination's drill-down level, not a fourth destination: the
// rail keeps 记录 highlighted in both views (design 07 §3.1).
type recordTab int

const (
	recordTabSummary recordTab = iota
	recordTabItems
	recordTabCount
)

// recordWorkspace is an opened record: its title bar, the 摘要 / 逐件
// switch and the chosen view. Coverage, quality and saving stay independent.
func (m *Model) recordWorkspace(c *ui.Context, t *ui.Theme) {
	tok := m.effectiveTokens()
	ui.Column(c).Grow(1).FillHeight().MinHeight(0).Padding(m.pagePadding()).Gap(16).Children(func() {
		m.recordWorkspaceHeader(c, t, tok)
		switch m.recordTab {
		case recordTabItems:
			ui.Column(c).Grow(1).MinHeight(0).Gap(16).Children(func() {
				m.resultsArea(c, t)
			})
		default:
			m.recordSummary(c, t)
		}
	})
}

// recordWorkspaceHeader is the record's title bar: back to the whole list,
// the record title, the 摘要 / 逐件 switch, the three status slots (coverage,
// pending, saved) and the export entry that opens the modal dialog.
func (m *Model) recordWorkspaceHeader(c *ui.Context, t *ui.Theme, tok *Tokens) {
	ui.Column(c).Gap(16).Children(func() {
		pageHeader(c).Children(func() {
			if button(c, "‹ 全部记录").Clicked() {
				m.navigatePage(pageHistory)
			}
			title := m.resultsTitle()
			if m.rec != nil && !m.rec.Shell.StartedAt.IsZero() {
				title = m.rec.Shell.StartedAt.Local().Format("1 月 2 日 15:04")
			}
			pageTitle(c, title).Shrink(1)
			ui.Box(c).Grow(1)
			if m.rec != nil && m.rec.ID != string(m.appliedSession) && m.appliedSession != "" && m.exitState != exitRunning {
				if button(c, "查看本次扫描").Clicked() {
					m.ShowLive()
				}
			}
			m.exportEntryArea(c)
		})
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			tab := int(m.recordTab)
			viewSegments(c, &tab, tok, "摘要", "逐件").Bind(&m.workspaceTabsRef)
			if tab >= 0 && tab < int(recordTabCount) && recordTab(tab) != m.recordTab {
				m.requestNavigation(navigationRequest{kind: navRecordTab, recordTab: recordTab(tab)})
			}
			ui.Box(c).Grow(1)
			coverage, pending, saved := m.workspaceStatusTexts()
			if m.rec != nil && m.rec.Interrupted {
				ui.Textf(c, "中断恢复 %d 条", m.rec.InterruptedCount).TextColor(tok.WarningFG).FontSize(t.FontSize * 12 / 14)
			}
			fg, bg := tok.TextSecondary, tok.SurfaceSunken
			if m.recordCoverage() == record.CoveragePartial {
				fg, bg = tok.WarningFG, tok.WarningSoft
			} else if m.recordCoverage() == record.CoverageComplete {
				fg, bg = tok.SuccessFG, tok.SuccessSoft
			}
			ui.Text(c, coverage).FontSize(t.FontSize*12/14).TextColor(fg).Background(bg).Padding(4, 12).Radius(4)
			ui.Text(c, pending).SingleLine()
			prefix := ""
			if saved == "已保存" {
				prefix = "✓ "
			}
			fg, bg = tok.TextSecondary, tok.SurfaceSunken
			switch saved {
			case "保存失败", "记录不可读":
				fg, bg = tok.DangerFG, tok.DangerSoft
			case "正在保存…", "读取中…":
				fg, bg = tok.AccentFG, tok.AccentSoft
			case "未保存修改", "未保存":
				fg, bg = tok.WarningFG, tok.WarningSoft
			}
			ui.Text(c, prefix+saved).FontSize(t.FontSize*12/14).TextColor(fg).Background(bg).Padding(4, 12).Radius(4)
		})
	})
}

// workspaceStatusTexts fills the header's three status slots. The pending
// slot is the four-class total (backfill plus review) from summaryCounts,
// the same source the summary and item list use; the save slot names the
// live record's or revision's state rather than claiming a save that has
// not happened.
func (m *Model) workspaceStatusTexts() (coverage, pending, saved string) {
	coverage = "覆盖：—"
	if m.recData != nil {
		coverage = "覆盖：" + coverageText(m.recordCoverage())
	}
	pending = "待处理 —"
	if m.rec != nil {
		pending = "待处理 " + formatCount(m.summaryCounts().Pending())
	}
	saved = "已保存"
	switch {
	case m.recErr != "":
		saved = "记录不可读"
	case m.loading:
		saved = "读取中…"
	case m.rec == nil:
		saved = "未打开"
	case m.reviewSaving:
		saved = "正在保存…"
	case m.reviewSaveFailed:
		saved = "保存失败"
	case m.editingDirty():
		saved = "未保存修改"
	case m.rec.Live && m.snap.SaveError != "":
		saved = "保存失败"
	case m.rec.Live && (m.snap.Busy || m.snap.SavePending):
		saved = "未保存"
	}
	return coverage, pending, saved
}

// showRecordTab applies a settled 摘要 / 逐件 switch. 摘要 and 逐件 are
// separate screens (08 §2, §3), so an ordinary action result from the item
// browser does not follow the user into the summary (L-D). The modify
// flow's "back to review" consequence is the one result that intentionally
// stays across screens. Callers that bypass requestNavigation use this too,
// so the invariant does not depend on which entry point switched the tab.
func (m *Model) showRecordTab(tab recordTab) {
	if tab != m.recordTab && !m.reviewNoticeAcrossPages {
		m.clearReviewNotice()
	}
	m.recordTab = tab
	if tab == recordTabItems {
		m.enterReviewView()
	}
}

// openRecordWorkspace shows the opened record's summary. It switches only
// the view: it never activates the window, so a scan that finished in the
// background stays in the background (C-09).
func (m *Model) openRecordWorkspace() {
	m.page = pageResults
	m.showRecordTab(recordTabSummary)
}

// enterReviewView starts a review visit on the item tab: the form region
// and its first control take the focus — 修改 for a read-only item, the
// first problem field otherwise (08 §3, §3.4).
func (m *Model) enterReviewView() {
	m.reviewRegion = reviewRegionForm
	m.reviewField = ""
	m.scrollToCrop = false
	m.requestReviewFormEntryFocus()
}
