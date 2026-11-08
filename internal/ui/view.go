package ui

import (
	"github.com/LoTwT/youkai/internal/scan"
	"github.com/egoist/mygo/ui"
)

// View builds the main window's page shell. It runs on the UI thread
// whenever the window needs a frame and only reads the model; every
// interaction schedules its work through the model's actions.
func (m *Model) View(c *ui.Context) {
	// A browse click leaves the pointer on the position the editor's write
	// buttons reuse. End that guard as soon as the pointer leaves it, before
	// this frame reads any click (08 §3.4).
	m.updateReviewBrowseGuard(c)
	// Each build pass notes the text field that holds the keyboard, so the
	// shell's Escape handler can leave the key to it.
	m.textInputFocused = false
	// The results rows and badges scale with the desktop's text size.
	// Recording it here lets the scroll/reset handlers, which run from
	// update paths without a Context, use the same row height as this
	// frame's layout.
	m.textScale = c.Preferences().TextScale
	m.viewPages(c)
}

// phaseText renders a session phase. Nothing beyond the phase itself is
// claimed — no percentage the scan cannot prove.
func phaseText(p scan.Phase) string {
	switch p {
	case scan.PhasePreparing:
		return "准备中"
	case scan.PhaseCapturing:
		return "采集中"
	case scan.PhaseFinalizing:
		return "收尾中"
	case scan.PhaseEnded:
		return "已结束"
	default:
		return string(p)
	}
}

// exitArea shows the explicit-exit wrap-up's progress, or its failure with
// the way forward; a completed exit never shows here (the window closed).
func (m *Model) exitArea(c *ui.Context, t *ui.Theme) {
	switch m.exitState {
	case exitRunning:
		ui.Text(c, "正在退出：停止扫描 → 保存结果 → 释放识别资源……").TextColor(t.TextMuted)
	case exitFailed:
		if m.exitFailedFromSave() {
			ui.Text(c, "退出未完成："+m.exitErr+"。请先“重试保存”，成功后再关闭窗口退出。").TextColor(t.Danger)
			return
		}
		if m.savedExitResults() {
			ui.Text(c, "退出未完成："+m.exitErr+"。扫描结果已保存，退出清理没有完成；再次退出可重新打开退出失败对话框，在那里仍然退出或复制诊断信息。").TextColor(t.Danger)
			return
		}
		ui.Text(c, "退出未完成："+m.exitErr+"。当前没有待保存的扫描结果；再次退出可重新打开退出失败对话框，在那里仍然退出或复制诊断信息。").TextColor(t.Danger)
	}
}
