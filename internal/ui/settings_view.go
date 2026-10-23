package ui

import (
	"runtime"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/egoist/mygo/ui"
)

// settingsTwoColumnMinWidth starts the round-3 two-column settings grid:
// window, updates, storage and shortcuts sit side by side from 1024 up.
// Below it the cards stack in reading order.
const settingsTwoColumnMinWidth = 1024

// settingsPage is the settings destination: window behavior, updates,
// storage, the keys this build registers, and the destructive entries
// across the bottom. The page owns no stage switch.
func (m *Model) settingsPage(c *ui.Context, t *ui.Theme) {
	_, tok := themeWithTokens(t, c.Preferences())
	ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "设置").FontSize(t.FontSize * 1.3).FontWeight(700)
		ui.Text(c, "窗口、存储、更新与快捷键").TextColor(tok.TextMuted).
			FontSize(t.FontSize * 0.9)
	})
	if m.settingsTwoColumns() {
		ui.Row(c).Gap(t.Space(3.5)).AlignItems(ui.Start).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(t.Space(2)).Children(func() {
				m.settingsWindowCard(c, t, tok)
				m.settingsUpdateCard(c, t, tok)
			})
			ui.Column(c).Grow(1).MinWidth(0).Gap(t.Space(2)).Children(func() {
				m.settingsStorageCard(c, t, tok)
				m.settingsShortcutsCard(c, t, tok)
			})
		})
	} else {
		ui.Column(c).Gap(t.Space(2)).Children(func() {
			m.settingsWindowCard(c, t, tok)
			m.settingsUpdateCard(c, t, tok)
			m.settingsStorageCard(c, t, tok)
			m.settingsShortcutsCard(c, t, tok)
		})
	}
	m.settingsDangerZone(c, t, tok)
}

// settingsTwoColumns reports whether the grid has room for both cards.
// Before the window reports its content width, the wide design applies.
func (m *Model) settingsTwoColumns() bool {
	return m.contentWidth == 0 || m.contentWidth >= settingsTwoColumnMinWidth
}

// settingsCard is one titled group. The width comes from its column; the
// card never grows vertically on its own.
func settingsCard(c *ui.Context, t *ui.Theme, tok *Tokens, title string, body func()) {
	ui.Column(c).Background(tok.SurfaceCard).Border(1, tok.TableBorder).
		Radius(t.Space(1.5)).Padding(t.Space(2.5), t.Space(3)).
		Gap(t.Space(1.5)).Children(func() {
		ui.Text(c, title).FontWeight(600)
		body()
	})
}

func (m *Model) settingsWindowCard(c *ui.Context, t *ui.Theme, tok *Tokens) {
	settingsCard(c, t, tok, "窗口与启动", func() {
		m.closeBehaviorArea(c, t)
	})
}

func (m *Model) settingsUpdateCard(c *ui.Context, t *ui.Theme, tok *Tokens) {
	settingsCard(c, t, tok, "软件更新", func() {
		if m.updates == nil {
			if m.appVersion != "" {
				ui.Text(c, "当前版本 "+m.appVersion).TextColor(t.TextMuted)
			}
			ui.Text(c, "当前构建未启用自动更新。").TextColor(t.TextMuted)
			return
		}
		m.updateArea(c, t)
	})
}

func (m *Model) settingsStorageCard(c *ui.Context, t *ui.Theme, tok *Tokens) {
	settingsCard(c, t, tok, "存储与清理", func() {
		m.storageSummaryLine(c, t)
		m.storageHousekeepingRow(c, t, true)
		if m.cleanupNotice != "" {
			ui.Text(c, m.cleanupNotice)
		}
		m.storagePromptArea(c, t)
	})
}

// settingsShortcut is one row of the settings key list. Only keys this
// build actually registers appear.
type settingsShortcut struct {
	action  string
	context string
	keys    string
}

func (m *Model) settingsShortcutsCard(c *ui.Context, t *ui.Theme, tok *Tokens) {
	settingsCard(c, t, tok, "快捷键", func() {
		for i, shortcut := range settingsShortcutRows() {
			if i > 0 {
				ui.Divider(c)
			}
			ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
				ui.Text(c, shortcut.action).TextColor(tok.TextSecondary)
				ui.Text(c, shortcut.context).TextColor(tok.TextMuted).
					FontSize(t.FontSize * 0.85)
				ui.Box(c).Grow(1)
				ui.Text(c, shortcut.keys).FontWeight(600)
			})
		}
	})
}

// settingsShortcutRows lists the registered keys in the design's order.
// The global stop key follows the platform binding the entry point
// registers for a scan session; every other row is a key the page shell
// itself handles.
func settingsShortcutRows() []settingsShortcut {
	return []settingsShortcut{
		{action: "开始扫描", context: "扫描准备", keys: "⌘R"},
		{action: "停止扫描", context: "扫描中 · 全局", keys: stopHotkeyLabel()},
		{action: "确认并下一项", context: "结果与校对", keys: "⌘↵"},
		{action: "下一个待校对", context: "结果与校对", keys: "⌘J"},
		{action: "保存修改", context: "编辑中", keys: "⌘S"},
		{action: "打开记录", context: "历史", keys: "↵"},
		{action: "关闭浮层/返回表格", context: "任意", keys: "Esc"},
	}
}

func stopHotkeyLabel() string {
	if runtime.GOOS == "windows" {
		return "Ctrl+Alt+Shift+S"
	}
	return "⌃⌥⌘S"
}

// settingsDangerZone is the full-width destructive group. The 3-DIP red
// edge follows the design; the two entries keep the measured confirmation
// and share its disabled reason.
func (m *Model) settingsDangerZone(c *ui.Context, t *ui.Theme, tok *Tokens) {
	ui.Row(c).AlignItems(ui.Stretch).Background(tok.DangerSoft).
		Border(1, tok.DangerBorder).Radius(t.Space(1.5)).Clip().Children(func() {
		ui.Box(c).Width(3).Background(tok.DangerSolid)
		ui.Column(c).Grow(1).MinWidth(0).Padding(t.Space(2.5), t.Space(3)).
			Gap(t.Space(1)).Children(func() {
			ui.Text(c, "危险区").TextColor(tok.DangerFG).FontWeight(700)
			ui.Text(c, "删除后无法恢复。确认前会列出影响的记录数与占用空间，默认焦点在“取消”；扫描进行中不可用。").
				TextColor(tok.TextSecondary).FontSize(t.FontSize * 0.9)
			m.bulkCleanupRow(c, t)
			if m.cleanup != nil && m.cleanup.target.all() {
				m.cleanupPromptArea(c, t)
			}
		})
	})
}

// closeBehaviorArea exposes the persisted close-window preference. The
// default hides to the tray/menu bar and keeps the process alive; the
// explicit choice runs the same stop/save/release wrap-up as a quit.
func (m *Model) closeBehaviorArea(c *ui.Context, t *ui.Theme) {
	exitOnClose := m.CloseBehavior() == app.CloseBehaviorExit
	ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
		ui.Checkbox(c, &exitOnClose, "关闭窗口时停止扫描并退出应用")
		if exitOnClose != (m.CloseBehavior() == app.CloseBehaviorExit) {
			behavior := app.CloseBehaviorHide
			if exitOnClose {
				behavior = app.CloseBehaviorExit
			}
			m.SetCloseBehavior(behavior)
		}
		ui.Text(c, "默认：关闭窗口隐藏到托盘/菜单栏，应用继续运行。").
			TextColor(t.TextMuted).FontSize(t.FontSize * 0.9)
	})
	if m.closeBehaviorErr != "" {
		ui.Text(c, m.closeBehaviorErr).TextColor(t.Danger).FontSize(t.FontSize * 0.9)
	}
	if m.windowNotice != "" {
		ui.Text(c, m.windowNotice).TextColor(t.TextMuted).FontSize(t.FontSize * 0.9)
	}
}
