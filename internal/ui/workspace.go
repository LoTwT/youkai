package ui

import (
	"fmt"

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
// switch and the chosen view. This PR builds only the frame around the
// existing surfaces; the summary and the item browser keep their content
// until their own PRs replace it.
func (m *Model) recordWorkspace(c *ui.Context, t *ui.Theme) {
	_, tok := themeWithTokens(t, c.Preferences())
	ui.Column(c).Grow(1).FillHeight().Children(func() {
		m.recordWorkspaceHeader(c, t, tok)
		ui.Divider(c)
		// The unsaved-input prompt belongs to the whole workspace: leaving
		// the record, switching destinations or quitting must show it from
		// 摘要 as well as 逐件 (H-1).
		if m.navPrompt != nil {
			ui.Row(c).Padding(t.Space(2), t.Space(3)).Children(func() {
				m.navigationPrompt(c, t)
			})
			ui.Divider(c)
		}
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
		if tab >= 0 && tab < int(recordTabCount) {
			m.recordTab = recordTab(tab)
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

// workspaceStatusTexts fills the header's three status slots. They are
// placeholders for the summary work of PR-13: coverage and pending come
// from the loaded record, and the save slot names the live record's state
// rather than claiming a save that has not happened.
func (m *Model) workspaceStatusTexts() (coverage, pending, saved string) {
	coverage = "覆盖：—"
	if m.recData != nil {
		coverage = "覆盖：" + coverageText(m.recordCoverage())
	}
	pending = "待处理 —"
	if m.rec != nil {
		pending = fmt.Sprintf("待处理 %d", m.pendingCount())
	}
	saved = "已保存"
	switch {
	case m.recErr != "":
		saved = "记录不可读"
	case m.loading:
		saved = "读取中…"
	case m.rec == nil:
		saved = "未打开"
	case m.rec.Live && m.snap.SaveError != "":
		saved = "保存失败"
	case m.rec.Live && (m.snap.Busy || m.snap.SavePending):
		saved = "未保存"
	}
	return coverage, pending, saved
}

// recordSummary is the summary view's first version: the existing
// conclusion and export controls, without the item browser. PR-13 replaces
// it with the designed summary.
func (m *Model) recordSummary(c *ui.Context, t *ui.Theme) {
	ui.Scroll(c).Grow(1).Children(func() {
		ui.Column(c).Gap(t.Space(2)).Padding(t.Space(3)).Children(func() {
			m.sessionTerminationArea(c, t)
			m.savePromptArea(c, t)
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
				if m.rec != nil && m.completionVisible() {
					m.completionCard(c, t)
					return
				}
				ui.Text(c, m.emptyResultsText()).TextColor(t.TextMuted)
				return
			}
			if m.completionVisible() {
				m.completionCard(c, t)
				return
			}
			m.resultConclusion(c, t)
		})
	})
}

// resultConclusion states the record's conclusion from the data the
// results header already shows: the pending count, the exportable and
// captured counts, the coverage conclusion and an incomplete-position
// note that the termination notice does not already carry.
func (m *Model) resultConclusion(c *ui.Context, t *ui.Theme) {
	counts := m.recordCounts()
	ui.Column(c).Gap(t.Space(1)).Padding(t.Space(3)).
		Background(t.Surface).Border(1, t.Border).Radius(t.Radius).
		Children(func() {
			ui.Text(c, m.pendingText()).FontWeight(600)
			if counts.ExportKnown {
				ui.Textf(c, "可导出 %d 件 · 共采集 %d 件", counts.Exportable, counts.Captured)
			}
			ui.Text(c, m.completionCoverageText()).TextColor(t.TextMuted)
			if note := m.incompletePositionsNote(); note != "" && !m.sessionTerminationShown() {
				ui.Text(c, note).TextColor(t.Warning)
			}
		})
}

// openRecordWorkspace shows the opened record's summary. It switches only
// the view: it never activates the window, so a scan that finished in the
// background stays in the background (C-09).
func (m *Model) openRecordWorkspace() {
	m.page = pageResults
	m.recordTab = recordTabSummary
}
