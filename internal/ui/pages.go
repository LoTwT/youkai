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

// The page shell's navigation rail. The design fixes 168 DIP at 1440 and
// 144 DIP at 1020/1024 and 860; folding below 1200 keeps that room for the
// page as the window narrows while leaving the wide screenshots untouched.
const (
	sidebarWideWidth     = 168
	sidebarNarrowWidth   = 144
	sidebarNarrowMaxSize = 1200
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
	tok := ApplyTheme(c)
	t := c.Theme()
	m.registerPageShortcuts(c)
	if m.displayPage() == pageResults {
		m.registerResultsShortcuts(c)
	} else {
		// Confirmed rows linger in the pending segment only for the
		// current results visit; leaving the destination refreshes it.
		m.clearRecentlyConfirmed()
	}
	if m.displayPage() != pageHistory {
		// The row menu only exists on the history page; leaving it must not
		// leave an open menu to reappear on the next visit.
		m.historyMenuOpen = false
	}

	ui.Row(c).Fill().Children(func() {
		m.sidebar(c, t, tok)
		ui.Divider(c)
		ui.Column(c).Grow(1).FillHeight().Children(func() {
			m.pageBody(c, t)
			m.pageActions(c, t, tok)
			if m.exportStatusVisible() {
				ui.Divider(c)
				m.exportStatusArea(c, t)
			}
			ui.Divider(c)
			m.pageStatusBar(c, t)
		})
	})
}

// sidebarWidth folds the rail to the design's narrow width when the window
// is compact. The window reports its content size through SetContentSize,
// so a resize redraws with the other width without any extra state.
func (m *Model) sidebarWidth() float32 {
	if m.contentWidth > 0 && m.contentWidth < sidebarNarrowMaxSize {
		return sidebarNarrowWidth
	}
	return sidebarWideWidth
}

// sidebar is the navigation rail: rectangular items with an icon and a
// label. Settings stays pinned to the bottom; the upper items are
// destinations.
func (m *Model) sidebar(c *ui.Context, t *ui.Theme, tok *Tokens) {
	width := m.sidebarWidth()
	horizontal := t.Space(2.5)
	vertical := t.Space(3.5)
	if width == sidebarNarrowWidth {
		horizontal = t.Space(2)
		vertical = t.Space(3)
	}
	ui.Column(c).Width(width).FillHeight().Background(tok.SurfaceWindow).
		Padding(vertical, horizontal).Gap(t.Space(0.5)).Children(func() {
		m.sidebarButton(c, t, tok, "扫描", scanIcon,
			m.displayPage() == pageScan || m.displayPage() == pageResults, func() {
				if m.rec != nil {
					m.page = pageResults
				} else {
					m.page = pageScan
				}
			})
		m.sidebarButton(c, t, tok, "历史", historyIcon,
			m.displayPage() == pageHistory, func() {
				m.page = pageHistory
			})
		ui.Box(c).Grow(1)
		ui.Divider(c)
		m.sidebarButton(c, t, tok, "设置", settingsIcon,
			m.displayPage() == pageSettings, func() {
				m.page = pageSettings
			})
	})
}

// sidebarButton is one rectangular navigation item. The active item uses
// the accent-soft face and accent foreground, as the design's wide sidebar.
func (m *Model) sidebarButton(c *ui.Context, t *ui.Theme, tok *Tokens, label string, icon *ui.SVG, active bool, action func()) {
	bg, fg := tok.SurfaceWindow, tok.TextSecondary
	if active {
		bg, fg = tok.AccentSoft, tok.AccentFG
	}
	button := ui.Button(c, "").Label(label).Background(bg).TextColor(fg).
		Border(0, ui.Color{}).Radius(t.Space(2)).
		Height(t.Space(9.5)).Padding(0, t.Space(2.5)).Gap(t.Space(2.5))
	if button.Clicked() {
		action()
	}
	button.Children(func() {
		ui.Icon(c, icon).Size(t.Space(4.5), t.Space(4.5)).TextColor(fg)
		ui.Text(c, label).FontSize(t.FontSize).TextColor(fg).SingleLine()
	})
}

func (m *Model) pageBody(c *ui.Context, t *ui.Theme) {
	page := m.displayPage()
	if page == pageScan || page == pageResults {
		ui.Column(c).Grow(1).Padding(t.Space(3), t.Space(3)).Gap(t.Space(2)).Children(func() {
			if page == pageResults {
				m.resultsArea(c, t)
				return
			}
			ui.Scroll(c).Grow(1).Children(func() {
				ui.Column(c).Gap(t.Space(2)).Children(func() {
					m.navigationPrompt(c, t)
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

	if page == pageHistory {
		// The history table owns its vertical scrolling; an outer scroll
		// would give the List an unbounded height and defeat row snapping.
		ui.Column(c).Grow(1).FillHeight().Padding(t.Space(3), t.Space(3)).Children(func() {
			m.historyPage(c, t)
		})
		return
	}

	ui.Scroll(c).Grow(1).Children(func() {
		ui.Column(c).Gap(t.Space(2)).Padding(t.Space(3), t.Space(3)).Children(func() {
			switch page {
			case pageSettings:
				m.settingsPage(c, t)
			}
		})
	})
}

// pageActions is the scan page's bottom action bar: start while idle, stop
// while a session is running. The results page keeps its actions in the
// page header instead, so this bar renders only on the scan destination.
func (m *Model) pageActions(c *ui.Context, t *ui.Theme, tok *Tokens) {
	if m.displayPage() != pageScan {
		return
	}
	ui.Divider(c)
	ui.Row(c).Height(t.Space(13)).Background(tok.SurfaceHeader).
		Padding(0, t.Space(3)).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Grow(1)
		if m.snap.Busy || m.startReqID != "" {
			if ui.Button(c, "停止扫描").TextColor(t.Danger).Clicked() {
				m.Stop()
			}
			return
		}
		if ui.PrimaryButton(c, "开始扫描").Disabled(!m.canStart()).Clicked() {
			m.Start()
		}
	})
}

func (m *Model) pageStatusBar(c *ui.Context, t *ui.Theme) {
	ui.Row(c).Background(t.Surface).Padding(t.Space(2), t.Space(3)).Children(func() {
		ui.Text(c, m.pageStatusText()).TextColor(t.TextMuted).FontSize(t.FontSize * 0.85)
		ui.Box(c).Grow(1)
		if shortcuts := m.statusShortcutText(); shortcuts != "" {
			ui.Text(c, shortcuts).TextColor(t.TextMuted).FontSize(t.FontSize * 0.85)
		}
	})
}

// statusShortcutText names only the keys this build registers for the
// current page. The results page's keys follow its list/detail/edit
// layout; the scan page's stop key is the platform hotkey registered by
// the entry point, and its start key exists only when ⌘R is registered.
func (m *Model) statusShortcutText() string {
	switch m.displayPage() {
	case pageResults:
		return m.resultShortcutText()
	case pageScan:
		if m.snap.Busy || m.startReqID != "" {
			return "⌃⌥⌘S 停止扫描"
		}
		if m.scanStartShortcutAvailable() {
			return "⌘R 开始扫描"
		}
	case pageHistory:
		if len(m.visibleHistory()) > 0 {
			return "↵ 打开记录"
		}
	}
	return ""
}

func (m *Model) pageStatusText() string {
	switch m.displayPage() {
	case pageResults:
		if m.completionVisible() {
			return m.completionStatusText()
		}
		return m.pendingText()
	case pageHistory:
		return m.historyStatusText()
	case pageSettings:
		return "设置更改会立即保存"
	default:
		if m.snap.SessionID != "" {
			return fmt.Sprintf("扫描会话 %s · %s", m.snap.SessionID, phaseText(m.snap.Scan.Phase))
		}
		return "就绪 · 自动保存已开启"
	}
}
