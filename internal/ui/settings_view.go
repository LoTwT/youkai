package ui

import (
	"github.com/LoTwT/youkai/internal/app"
	"github.com/egoist/mygo/ui"
)

// settingsPage is the settings destination: storage management and the
// update area when updates are configured.
func (m *Model) settingsPage(c *ui.Context, t *ui.Theme) {
	m.storageArea(c, t)
	if m.updates != nil {
		ui.Divider(c)
		m.updateArea(c, t)
	}
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
