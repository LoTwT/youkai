package ui

import (
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
// switch and the chosen view. The summary is the designed conclusion; the
// item browser keeps its existing content until PR-14b replaces it.
func (m *Model) recordWorkspace(c *ui.Context, t *ui.Theme) {
	_, tok := themeWithTokens(t, c.Preferences())
	ui.Column(c).Grow(1).FillHeight().Children(func() {
		m.recordWorkspaceHeader(c, t, tok)
		ui.Divider(c)
		switch m.recordTab {
		case recordTabItems:
			ui.Column(c).Grow(1).Padding(t.Space(3), t.Space(3)).Gap(t.Space(2)).Children(func() {
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
	ui.Row(c).Gap(t.Space(2)).Padding(t.Space(2), t.Space(3)).
		Background(tok.SurfaceHeader).AlignItems(ui.Center).Children(func() {
		if ui.Button(c, "‹ 全部记录").Clicked() {
			m.navigatePage(pageHistory)
		}
		ui.Text(c, m.resultsTitle()).FontWeight(600).SingleLine().Shrink(1)
		if m.rec != nil && m.rec.ID != string(m.appliedSession) &&
			m.appliedSession != "" && m.exitState != exitRunning {
			if ui.Button(c, "查看本次扫描").Clicked() {
				m.ShowLive()
			}
		}
		tab := int(m.recordTab)
		ui.Segmented(c, &tab, "摘要", "逐件").Bind(&m.workspaceTabsRef)
		if tab >= 0 && tab < int(recordTabCount) && recordTab(tab) != m.recordTab {
			m.recordTab = recordTab(tab)
			if m.recordTab == recordTabItems {
				m.enterReviewView()
			}
		}
		ui.Box(c).Grow(1)
		coverage, pending, saved := m.workspaceStatusTexts()
		for _, status := range []string{coverage, pending, saved} {
			ui.Text(c, status).TextColor(tok.TextMuted).
				FontSize(t.FontSize * 0.9).SingleLine()
		}
		// The title bar owns the record's one export entry; it opens the
		// modal dialog. A build without a saver still shows it, disabled.
		m.exportEntryArea(c)
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

// openRecordWorkspace shows the opened record's summary. It switches only
// the view: it never activates the window, so a scan that finished in the
// background stays in the background (C-09).
func (m *Model) openRecordWorkspace() {
	m.page = pageResults
	m.recordTab = recordTabSummary
}

// enterReviewView starts a review visit on the item tab: the form region
// and its first problem field take the focus, as 08 §3 requires.
func (m *Model) enterReviewView() {
	m.reviewRegion = reviewRegionForm
	m.reviewField = ""
	m.requestReviewFieldFocus("")
	m.scrollToCrop = false
}
