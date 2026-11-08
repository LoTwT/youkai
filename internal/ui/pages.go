package ui

import (
	"github.com/LoTwT/youkai/internal/app"
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
	page := m.displayPage()
	if page != m.lastPage {
		if page == pageHistory {
			// Entering the list puts the keyboard on its first row (08 §5).
			m.historyFocusPending = true
		}
		if page == pageSettings {
			// Entering settings puts the keyboard on the in-page directory,
			// unless the disk-space entry asked for the storage section.
			m.settingsFocusPending = true
		}
		if m.lastPage == pageSettings && page != pageSettings {
			m.leaveSettingsPage()
		}
		m.lastPage = page
	}
	m.registerPageShortcuts(c)
	if page == pageScan {
		// Entering the scan page applies the ten-second freshness rule; a
		// focus or timer event also triggers it while the page stays open.
		m.refreshReadinessIfStale()
	}
	if page == pageResults && m.recordTab == recordTabItems {
		m.registerResultsShortcuts(c)
	} else {
		// The pending segment's just-confirmed markers belong to the item
		// browser's current visit; the summary and other destinations
		// refresh it. The browse guard protects only that bar's reused
		// position, so leaving the item browser ends it too.
		m.clearRecentlyConfirmed()
		m.clearReviewBrowseGuard()
	}
	if page != pageHistory {
		// The row menu only exists on the records list; leaving it must not
		// leave an open menu to reappear on the next visit.
		m.historyMenuOpen = false
	}

	ui.Row(c).Fill().Children(func() {
		m.navRail(c, t, tok)
		ui.Divider(c)
		ui.Column(c).Grow(1).FillHeight().Children(func() {
			m.pageBody(c, t, tok)
			m.pageActions(c, t, tok)
			m.reviewNoticePage(c, t)
			ui.Divider(c)
			m.pageStatusBar(c, t)
		})
	})
	// The export modal and the unsaved-changes dialog both cover the
	// whole window; only one of them is open at a time.
	m.exportDialogView(c, t)
	m.navigationPrompt(c, t)
	m.trayHintDialogView(c, t)
	m.exitDialogView(c, t)
}

// navRail is the three-destination rail: icon above label, the active
// destination with the accent-soft face, and settings pinned to the
// bottom. The rail is built first, so Tab reaches it before any page
// content; inside it Up and Down move the focus between the destinations
// and Enter visits them (design 09 §4.4).
func (m *Model) navRail(c *ui.Context, t *ui.Theme, tok *Tokens) {
	page := m.displayPage()
	onRecords := page == pageResults || page == pageHistory
	ui.Column(c).Width(navRailWidth).FillHeight().Background(tok.SurfaceWindow).
		Padding(t.Space(2.5), 0).Gap(t.Space(1.5)).AlignItems(ui.Center).FocusGroup(ui.Vertical).Children(func() {
		m.navRailItem(c, t, tok, "扫描", scanIcon, page == pageScan, false, m.showScanDestination)
		m.navRailItem(c, t, tok, "记录", recordsIcon, onRecords, false, m.showRecordsDestination)
		ui.Box(c).Grow(1)
		m.navRailItem(c, t, tok, "设置", settingsIcon, page == pageSettings, m.updateDotVisible(), m.showSettingsDestination)
	})
}

// navRailItem is one rail destination. The active item carries the
// accent-soft face and the accent foreground, and its ref lets F6 return
// the keyboard to the rail.
func (m *Model) navRailItem(c *ui.Context, t *ui.Theme, tok *Tokens, label string, icon *ui.SVG, active, showDot bool, action func()) {
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
			ui.Row(c).Gap(t.Space(0.75)).AlignItems(ui.Center).Children(func() {
				ui.Text(c, label).FontSize(t.FontSize * 0.8).TextColor(fg).SingleLine()
				if showDot {
					updateDot(c, t, tok)
				}
			})
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

func (m *Model) pageBody(c *ui.Context, t *ui.Theme, tok *Tokens) {
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
		ui.Column(c).Grow(1).FillHeight().Padding(t.Space(3), t.Space(3)).Gap(t.Space(2)).Children(func() {
			m.settingsPage(c, t)
		})
	default:
		ui.Column(c).Grow(1).FillHeight().Padding(t.Space(3), t.Space(3)).Children(func() {
			ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
				ui.Column(c).Gap(t.Space(2)).Children(func() {
					m.scanArea(c, t, tok)
					if m.exitState != exitIdle {
						ui.Divider(c)
						m.exitArea(c, t)
					}
				})
			})
		})
	}
}

// pageActions is the scan page's bottom action bar: the start facts on the
// left, the disabled reason and 开始扫描 on the right, or 停止扫描 while a
// session runs. The bar takes its content height instead of a fixed one so
// the minimum window keeps every line and the button visible (08 §1.3;
// T-08 may only wrap or drop the estimate).
func (m *Model) pageActions(c *ui.Context, t *ui.Theme, tok *Tokens) {
	if m.displayPage() != pageScan {
		return
	}
	ui.Divider(c)
	ui.Row(c).Background(tok.SurfaceHeader).Padding(t.Space(1.5), t.Space(3)).
		AlignItems(ui.Center).Gap(t.Space(3)).Children(func() {
		ui.Column(c).Grow(1).Gap(t.Space(0.5)).Children(func() {
			ui.Text(c, "中途停止 "+shortcutStopScan).FontSize(t.FontSize * 0.85).TextColor(t.TextMuted)
			ui.Text(c, "点开始后 30 秒内保持游戏在前台，扫描期间不要操作游戏").
				FontSize(t.FontSize * 0.85).TextColor(t.TextMuted)
			ui.Text(c, m.estimateText()).FontSize(t.FontSize * 0.85).TextColor(t.TextMuted)
		})
		if m.snap.Busy || m.startReqID != "" {
			button := ui.Button(c, "停止扫描").TextColor(t.Danger).Bind(&m.scanActionRef)
			if button.Clicked() {
				m.Stop()
			}
			return
		}
		if reason := m.scanStartBlockedReason(); reason != "" {
			ui.Text(c, reason).TextColor(t.TextMuted).FontSize(t.FontSize * 0.9).Shrink(1)
		}
		button := ui.PrimaryButton(c, "开始扫描").Disabled(!m.scanStartReady()).Bind(&m.scanActionRef)
		if button.Clicked() {
			m.requestScanStart()
		}
		if m.scanStartReady() && m.readinessFocus && m.readinessFocusStart {
			button.Focus()
			m.readinessFocus = false
			m.readinessFocusStart = false
		}
	})
}

// reviewNoticePage keeps the last review outcome visible when the item
// action bar is not on screen: the summary, the records list and settings
// must not hide that a saved modification went back to review (08 §3.4,
// 09 §4.1). The item browser shows the same line in its action bar.
func (m *Model) reviewNoticePage(c *ui.Context, t *ui.Theme) {
	if m.reviewNotice == "" || m.displayPage() == pageScan {
		return
	}
	// The item browser shows the same line in its action bar, which needs a
	// row and not the finished-review empty state. A failed record load
	// replaces that bar with the error, so the notice must move to the page
	// level instead of being suppressed (PR-19b round 2).
	if m.displayPage() == pageResults && m.recordTab == recordTabItems && m.recErr == "" &&
		m.selectedRow() != nil && !(m.filter == resultFilterPending && m.reviewSettled()) {
		return
	}
	ui.Row(c).Padding(t.Space(1.5), t.Space(3)).Children(func() {
		ui.Text(c, m.reviewNotice).FontSize(t.FontSize * 0.85).TextColor(t.TextMuted)
	})
}

// pageStatusBar is the window's bottom status line: the page's conclusion
// on the left and the keys that work there on the right.
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
		// The bottom action bar already names the stop hotkey and the
		// estimate; 09 §5.3 keeps further shortcut hints out of the page.
		return ""
	}
	// The records list names no keys: 09 §5.3 keeps shortcut hints out of
	// the interface text.
	return ""
}

// scanStartBlockedReason names why the start button is unavailable, in the
// order the user meets the conditions: exit and update flows, the service's
// slot state, page-owned work, the source and target, then the checklist.
// The text sits left of the button (08 §1, R-14); a busy session shows the
// stop button instead and never reaches this copy.
func (m *Model) scanStartBlockedReason() string {
	switch {
	case m.exitState == exitRunning || m.exitState == exitDone:
		return "正在退出，不能再开始扫描。"
	case m.startErr != "":
		if m.source.TargetSelection {
			return m.startErr
		}
		return "开始失败：" + m.startErr
	case m.snap.StartBlocked == app.StartBlockedSaveRetry:
		return "上次扫描的结果还没有保存，请先重试保存。"
	case m.snap.StartBlocked == app.StartBlockedUpdateInstall:
		return "更新正在安装，应用重启前不能开始扫描。"
	case m.snap.StartBlocked == app.StartBlockedQuitting:
		return "正在退出，不能再开始扫描。"
	case m.snap.StartBlocked == app.StartBlockedOccupied:
		return "上次扫描仍在收尾，等它结束或点“停止扫描”。"
	case m.updateRestartImminent():
		return "正在安装更新，应用稍后将自动重启；暂不能开始扫描。"
	case m.reviewSaving:
		return "正在保存校对修改，完成前不能开始扫描。"
	case m.exportBusy():
		return "正在导出，完成前不能开始扫描。"
	case !m.source.Available:
		return "这个版本还不能扫描。"
	case !m.readinessLoaded:
		return "正在检查……"
	case m.startReqID != "":
		return "正在开始扫描……"
	default:
		if reason := m.readinessStartReason(); reason != "" {
			return reason
		}
		if !m.targetReady() {
			// A target row without its own blocking conclusion must still
			// explain why the button is disabled.
			return "请先确认要扫描的游戏窗口。"
		}
		return ""
	}
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
		switch {
		case m.snap.SaveError != "":
			return "扫描结果未保存 · 需要重试"
		case m.snap.Busy:
			return "扫描" + phaseText(m.snap.Scan.Phase)
		default:
			return "就绪 · 自动保存已开启"
		}
	}
}
