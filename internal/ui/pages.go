package ui

import (
	"fmt"

	"github.com/LoTwT/youkai/internal/scan"
	"github.com/egoist/mygo/ui"
)

// Page is the shell's destination. The navigation rail has exactly three:
// scan, records and settings (design 07 §3.1). Scan is the only place a
// scan starts and where its live progress shows; record results are the
// records destination's drill-down level, not a fourth destination.
//
// pageResults is the opened record workspace and pageHistory its parent
// list; the rail highlights the records destination for both.
type Page int

const (
	pageScan Page = iota
	pageResults
	pageHistory
	pageSettings
)

// The navigation rail's fixed metrics: 64-DIP rail with 52-DIP items and a
// 20-DIP icon, settings pinned to the bottom (design 07 §3.1, wireframe 01).
const (
	navRailWidth     = 64
	navRailItemWidth = 52
)

var (
	scanIcon     = ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M4 8V5a1 1 0 0 1 1-1h3M16 4h3a1 1 0 0 1 1 1v3M20 16v3a1 1 0 0 1-1 1h-3M8 20H5a1 1 0 0 1-1-1v-3"/><circle cx="12" cy="12" r="3"/></svg>`))
	recordsIcon  = ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M5 4h14a1 1 0 0 1 1 1v14a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V5a1 1 0 0 1 1-1z"/><path d="M8 9h8M8 13h8M8 17h5"/></svg>`))
	settingsIcon = ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M4 7h16M4 12h16M4 17h16"/><circle cx="9" cy="7" r="2"/><circle cx="15" cy="12" r="2"/><circle cx="10" cy="17" r="2"/></svg>`))
)

// displayPage is the destination the shell renders. The page starts on
// scan (the zero value), never on a record or the records list: startup
// always lands on the scan page (design 07 §4).
func (m *Model) displayPage() Page {
	switch m.page {
	case pageScan, pageResults, pageHistory, pageSettings:
		return m.page
	default:
		return pageScan
	}
}

// viewPages is the shell: the 64-DIP navigation rail, the current page's
// body and actions, and the shared status bar.
func (m *Model) viewPages(c *ui.Context) {
	tok := ApplyTheme(c)
	t := c.Theme()
	m.registerPageShortcuts(c)
	if m.displayPage() == pageResults && m.recordTab == recordTabItems {
		m.registerResultsShortcuts(c)
	} else {
		// The pending segment's just-confirmed markers belong to the item
		// browser's current visit; the summary and other destinations
		// refresh it.
		m.clearRecentlyConfirmed()
	}
	if m.displayPage() != pageHistory {
		// The row menu only exists on the records list; leaving it must not
		// leave an open menu to reappear on the next visit.
		m.historyMenuOpen = false
	}

	ui.Row(c).Fill().Children(func() {
		m.navRail(c, t, tok)
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
	// The unsaved-changes dialog covers the whole window, whichever page
	// triggered it (08 §9).
	m.navigationPrompt(c, t)
}

// navRail is the three-destination rail: icon above label, the active
// destination with the accent-soft face, and settings pinned to the
// bottom. The rail is built first, so Tab reaches it before any page
// content (design 09 §4.4).
func (m *Model) navRail(c *ui.Context, t *ui.Theme, tok *Tokens) {
	page := m.displayPage()
	onRecords := page == pageResults || page == pageHistory
	ui.Column(c).Width(navRailWidth).FillHeight().Background(tok.SurfaceWindow).
		Padding(t.Space(2.5), 0).Gap(t.Space(1.5)).AlignItems(ui.Center).Children(func() {
		m.navRailItem(c, t, tok, "扫描", scanIcon, page == pageScan, m.showScanDestination)
		m.navRailItem(c, t, tok, "记录", recordsIcon, onRecords, m.showRecordsDestination)
		ui.Box(c).Grow(1)
		m.navRailItem(c, t, tok, "设置", settingsIcon, page == pageSettings, m.showSettingsDestination)
	})
}

// navRailItem is one rail destination. The active item carries the
// accent-soft face and the accent foreground, and its ref lets F6 return
// the keyboard to the rail.
func (m *Model) navRailItem(c *ui.Context, t *ui.Theme, tok *Tokens, label string, icon *ui.SVG, active bool, action func()) {
	bg, fg := tok.SurfaceWindow, tok.TextSecondary
	if active {
		bg, fg = tok.AccentSoft, tok.AccentFG
	}
	item := ui.Button(c, "").Label(label).Background(bg).TextColor(fg).
		Border(0, ui.Color{}).Radius(t.Space(2)).
		Width(navRailItemWidth).Padding(t.Space(1.75), 0)
	if active {
		item = item.Bind(&m.railActiveRef)
	}
	if item.Clicked() {
		action()
	}
	item.Children(func() {
		ui.Column(c).Gap(t.Space(0.5)).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icon).Size(t.Space(5), t.Space(5)).TextColor(fg)
			ui.Text(c, label).FontSize(t.FontSize * 0.8).TextColor(fg).SingleLine()
		})
	})
}

// showScanDestination opens the scan page. Scan is the only destination
// that starts a scan and shows its live progress; a record, live or
// reopened, is read in the records destination.
func (m *Model) showScanDestination() {
	if m.displayPage() == pageScan {
		return
	}
	m.navigatePage(pageScan)
}

// showRecordsDestination opens the records destination. An already open
// workspace stays where it is; from another destination, the record the
// user was reading comes back with its view state, and otherwise the whole
// list opens.
func (m *Model) showRecordsDestination() {
	switch m.displayPage() {
	case pageHistory, pageResults:
		return
	}
	if m.rec != nil && m.rec.ID != "" {
		if !m.rec.Live {
			// A reopened history record returns to its workspace.
			m.openRecord(m.rec.ID)
			return
		}
		if !m.snap.Busy && m.snap.Scan.Phase == scan.PhaseEnded &&
			m.appliedSession != "" && m.exitState != exitRunning && m.exitState != exitDone {
			// A finished session is a saved record; a running one belongs to
			// the scan page, so the records destination opens the list.
			m.ShowLive()
			return
		}
	}
	m.navigatePage(pageHistory)
}

// showSettingsDestination routes settings through the same draft guard as
// the record destinations, so leaving a dirty item prompts instead of
// dropping its input.
func (m *Model) showSettingsDestination() {
	m.navigatePage(pageSettings)
}

// navigatePage switches destinations after the active editor settles.
func (m *Model) navigatePage(page Page) {
	if m.displayPage() == page {
		return
	}
	m.requestNavigation(navigationRequest{kind: navPage, page: page})
}

func (m *Model) pageBody(c *ui.Context, t *ui.Theme) {
	switch m.displayPage() {
	case pageResults:
		m.recordWorkspace(c, t)
	case pageHistory:
		// The records table owns its vertical scrolling; an outer scroll
		// would give the List an unbounded height and defeat row snapping.
		ui.Column(c).Grow(1).FillHeight().Padding(t.Space(3), t.Space(3)).Children(func() {
			m.historyPage(c, t)
		})
	case pageSettings:
		ui.Scroll(c).Grow(1).Children(func() {
			ui.Column(c).Gap(t.Space(2)).Padding(t.Space(3), t.Space(3)).Children(func() {
				m.settingsPage(c, t)
			})
		})
	default:
		ui.Column(c).Grow(1).Padding(t.Space(3), t.Space(3)).Gap(t.Space(2)).Children(func() {
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
	}
}

// pageActions is the scan page's bottom action bar: start while idle, stop
// while a session is running. The records workspace keeps its actions in
// its title bar instead.
func (m *Model) pageActions(c *ui.Context, t *ui.Theme, tok *Tokens) {
	if m.displayPage() != pageScan {
		return
	}
	ui.Divider(c)
	ui.Row(c).Height(t.Space(13)).Background(tok.SurfaceHeader).
		Padding(0, t.Space(3)).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Grow(1)
		if m.snap.Busy || m.startReqID != "" {
			button := ui.Button(c, "停止扫描").TextColor(t.Danger).Bind(&m.scanActionRef)
			if button.Clicked() {
				m.Stop()
			}
			return
		}
		button := ui.PrimaryButton(c, "开始扫描").Disabled(!m.scanStartReady()).Bind(&m.scanActionRef)
		if button.Clicked() {
			m.requestScanStart()
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
// current page. Every label comes from shortcut_text.go, so the interface
// shows one platform's notation.
func (m *Model) statusShortcutText() string {
	switch m.displayPage() {
	case pageResults:
		// The review keys belong to the item browser; the summary has no
		// list and names none of them.
		if m.recordTab == recordTabItems {
			return m.resultShortcutText()
		}
	case pageScan:
		if m.snap.Busy || m.startReqID != "" {
			return shortcutStopScan + " 停止扫描"
		}
		if m.scanStartReady() {
			return shortcutStartScan + " 开始扫描"
		}
	case pageHistory:
		if len(m.visibleHistory()) > 0 {
			return shortcutOpenRecord + " 打开记录"
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
