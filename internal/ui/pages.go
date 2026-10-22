package ui

import (
	"fmt"

	"github.com/egoist/mygo/ui"
)

// Page is the main window's current task page. Scan and review are two
// stages of one workflow; history and settings are separate destinations.
type Page int

const (
	pageAuto Page = iota
	pageScan
	pageResults
	pageHistory
	pageSettings
)

var (
	scanIcon     = ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M4 8V5a1 1 0 0 1 1-1h3M16 4h3a1 1 0 0 1 1 1v3M20 16v3a1 1 0 0 1-1 1h-3M8 20H5a1 1 0 0 1-1-1v-3"/><circle cx="12" cy="12" r="3"/></svg>`))
	historyIcon  = ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M3 12a9 9 0 1 0 3-6.7"/><path d="M3 4v5h5"/><path d="M12 7v5l3 2"/></svg>`))
	settingsIcon = ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M4 7h16M4 12h16M4 17h16"/><circle cx="9" cy="7" r="2"/><circle cx="15" cy="12" r="2"/><circle cx="10" cy="17" r="2"/></svg>`))
)

func (m *Model) displayPage() Page {
	if m.page != pageAuto {
		return m.page
	}
	switch {
	case m.rec != nil:
		return pageResults
	case len(m.history) > 0:
		return pageHistory
	default:
		return pageScan
	}
}

// viewPages is the sidebar shell used by production builds. The legacy
// single-column view remains available for the existing interaction tests
// while the pages are migrated incrementally.
func (m *Model) viewPages(c *ui.Context) {
	ApplyTheme(c)
	t := c.Theme()

	ui.Row(c).Fill().Children(func() {
		m.sidebar(c, t)
		ui.Divider(c)
		ui.Column(c).Grow(1).Children(func() {
			m.pageBody(c, t)
			ui.Divider(c)
			m.pageStatusBar(c, t)
		})
	})
}

// sidebar is a compact navigation rail: icon and short label, no counts.
// Settings stays pinned to the bottom; the upper items are destinations.
func (m *Model) sidebar(c *ui.Context, t *ui.Theme) {
	ui.Column(c).Width(t.Space(16)).FillHeight().Background(t.Surface).
		Padding(t.Space(2), t.Space(1)).Gap(t.Space(1)).Children(func() {
		m.sidebarButton(c, t, "扫描与校对", "扫描", scanIcon,
			m.displayPage() == pageScan || m.displayPage() == pageResults, func() {
				if m.rec != nil {
					m.page = pageResults
				} else {
					m.page = pageScan
				}
			})
		m.sidebarButton(c, t, "历史记录", "历史", historyIcon,
			m.displayPage() == pageHistory, func() {
				m.page = pageHistory
			})
		ui.Box(c).Grow(1)
		ui.Divider(c)
		m.sidebarButton(c, t, "设置", "设置", settingsIcon,
			m.displayPage() == pageSettings, func() {
				m.page = pageSettings
			})
	})
}

func (m *Model) sidebarButton(c *ui.Context, t *ui.Theme, label, short string, icon *ui.SVG, active bool, action func()) {
	bg, fg := t.Surface, t.TextMuted
	if active {
		bg, fg = t.SurfaceHover, t.Text
	}
	button := ui.Button(c, "").Label(label).Background(bg).TextColor(fg).
		Height(t.Space(14)).Width(t.Space(14))
	if button.Clicked() {
		action()
	}
	button.Children(func() {
		ui.Column(c).AlignItems(ui.Center).Gap(t.Space(0.5)).Children(func() {
			ui.Icon(c, icon).Size(t.Space(4), t.Space(4)).TextColor(fg)
			ui.Text(c, short).FontSize(t.FontSize * 0.68).TextColor(fg).SingleLine()
		})
	})
}

func (m *Model) workflowLabel() string {
	return "扫描与校对"
}

func (m *Model) pageResultsLabel() string {
	if n := m.pendingCount(); n > 0 {
		return fmt.Sprintf("结果与校对 %d", n)
	}
	return "结果与校对"
}

func (m *Model) pageBody(c *ui.Context, t *ui.Theme) {
	page := m.displayPage()
	if page == pageScan || page == pageResults {
		ui.Column(c).Grow(1).Padding(t.Space(3), t.Space(3)).Gap(t.Space(2)).Children(func() {
			m.workflowStage(c, t)
			if page == pageResults {
				m.resultsArea(c, t)
				return
			}
			ui.Scroll(c).Grow(1).Children(func() {
				ui.Column(c).Gap(t.Space(2)).Children(func() {
					m.prepareArea(c, t)
					if m.snap.SessionID != "" {
						ui.Divider(c)
						m.progressArea(c, t)
					}
					if m.exitState != exitIdle {
						ui.Divider(c)
						m.exitArea(c, t)
					}
				})
			})
		})
		return
	}

	ui.Scroll(c).Grow(1).Children(func() {
		ui.Column(c).Gap(t.Space(2)).Padding(t.Space(3), t.Space(3)).Children(func() {
			switch page {
			case pageHistory:
				m.historyPage(c, t)
			case pageSettings:
				m.settingsPage(c, t)
			}
		})
	})
}

func (m *Model) workflowStage(c *ui.Context, t *ui.Theme) {
	stage := 0
	if m.displayPage() == pageResults {
		stage = 1
	}
	seg := ui.Segmented(c, &stage, "扫描", m.pageResultsLabel())
	if seg.Changed() {
		if stage == 0 {
			m.page = pageScan
		} else {
			m.page = pageResults
		}
	}
}

func (m *Model) pageStatusBar(c *ui.Context, t *ui.Theme) {
	ui.Row(c).Background(t.Surface).Padding(t.Space(2), t.Space(3)).Children(func() {
		ui.Text(c, m.pageStatusText()).TextColor(t.TextMuted).FontSize(t.FontSize * 0.85)
		ui.Box(c).Grow(1)
		ui.Text(c, "⌘↵ 确认并下一项  ·  ⌘S 保存修改  ·  ⌃⌥⌘S 停止扫描").
			TextColor(t.TextMuted).FontSize(t.FontSize * 0.85)
	})
}

func (m *Model) pageStatusText() string {
	switch m.displayPage() {
	case pageResults:
		return m.pendingText()
	case pageHistory:
		return fmt.Sprintf("已加载 %d 条历史记录", len(m.history))
	case pageSettings:
		return "设置更改会立即保存"
	default:
		if m.snap.SessionID != "" {
			return fmt.Sprintf("扫描会话 %s · %s", m.snap.SessionID, phaseText(m.snap.Scan.Phase))
		}
		return "就绪 · 自动保存已开启"
	}
}
