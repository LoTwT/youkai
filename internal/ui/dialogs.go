package ui

import (
	"github.com/LoTwT/youkai/internal/app"
	"github.com/egoist/mygo/ui"
)

// trayHintDialogView is the first-hide hint (08 §9, P-10/R-18). It appears
// before the first close hides the window and on the settings page's replay
// entry. Escape closes it without acknowledging, so the next close asks
// again.
func (m *Model) trayHintDialogView(c *ui.Context, t *ui.Theme) {
	if !m.trayHintOpen {
		return
	}
	open := true
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(t.Space(115)).Gap(t.Space(2)).Children(func() {
			ui.Text(c, "youkai 会继续在后台运行").FontWeight(600)
			ui.Text(c, "关闭窗口后，youkai 的图标在任务栏右侧的通知区域；Windows 11 可能把它收在 ^ 里面。点图标可以重新打开，右键图标可以退出。").
				TextColor(t.TextMuted)
			ui.Row(c).Gap(t.Space(1.5)).AlignItems(ui.Center).
				Background(t.Surface).Radius(t.Radius).Padding(t.Space(1.5), t.Space(2)).Children(func() {
				ui.Text(c, "任务栏示意").TextColor(t.TextMuted).FontSize(t.FontSize * 0.9)
				ui.Box(c).Grow(1)
				ui.Text(c, "^").FontWeight(600)
				ui.Text(c, "youkai").FontWeight(600)
			})
			if m.trayHintErr != "" {
				ui.Text(c, m.trayHintErr).TextColor(t.Danger).FontSize(t.FontSize * 0.9)
			}
			ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "之后可在 设置 › 常规 重看").TextColor(t.TextMuted).
					FontSize(t.FontSize * 0.9)
				ui.Box(c).Grow(1)
				if ui.PrimaryButton(c, "知道了").Clicked() {
					m.acknowledgeTrayHint()
				}
			})
		})
	})
	if !open {
		m.trayHintDismissed()
	}
}

// exitDialogView shows the explicit-exit wrap-up and its failure (08 §9).
// The running dialog has no buttons; the failure keeps the save retry and
// the diagnostic copy. Dismissing the failure leaves the scan page's inline
// retry visible.
func (m *Model) exitDialogView(c *ui.Context, t *ui.Theme) {
	switch m.exitState {
	case exitRunning:
		open := true
		ui.Modal(c, &open, func() {
			ui.Column(c).Width(t.Space(95)).Gap(t.Space(2)).Children(func() {
				ui.Text(c, "正在退出：保存结果……").FontWeight(600)
				ui.Text(c, "退出会先停止扫描并保存结果；重复退出会连到同一次收尾。").
					TextColor(t.TextMuted)
			})
		})
	case exitFailed:
		if !m.exitFailureDialogOpen {
			return
		}
		open := true
		saveFailure := m.exitFailedFromSave()
		title := "youkai 没有退出"
		if saveFailure {
			title = "结果还没有保存，youkai 没有退出"
		}
		ui.Modal(c, &open, func() {
			ui.Column(c).Width(t.Space(110)).Gap(t.Space(2)).Children(func() {
				ui.Text(c, title).FontWeight(600)
				ui.Text(c, m.exitFailureDetail()).TextColor(t.TextMuted)
				ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
					if saveFailure {
						retry := ui.PrimaryButton(c, "重试保存").
							Disabled(m.snap.SaveError == "")
						if retry.Clicked() {
							m.exitFailureDialogOpen = false
							m.RetrySave()
						}
					} else {
						// The saved result is safe; only the teardown failed
						// and Service.Quit caches that error, so this is the
						// explicit way out (PR-19b, item 13).
						if ui.PrimaryButton(c, "仍然退出").Clicked() {
							m.exitAnyway()
						}
					}
					if ui.Button(c, "复制诊断信息").Clicked() {
						m.copyCurrentDiagnostics(c)
					}
				})
				if saveFailure && m.snap.SaveError == "" {
					ui.Text(c, "当前没有可重试的保存；请查看诊断信息。").
						TextColor(t.TextMuted).FontSize(t.FontSize * 0.9)
				}
				if !saveFailure {
					ui.Text(c, "扫描结果已保存；退出清理没有完成，重试不会改变结果。可以仍然退出，或关闭对话框继续查看结果。").
						TextColor(t.TextMuted).FontSize(t.FontSize * 0.9)
				}
			})
		})
		if !open {
			m.exitFailureDialogOpen = false
		}
	}
}

// exitFailedFromSave reports whether the failed exit was blocked by the
// final save. The snapshot is authoritative; exitFailureWasSave carries the
// classification while a late snapshot is still arriving.
func (m *Model) exitFailedFromSave() bool {
	return m.snap.SaveError != "" || m.exitFailureWasSave
}

func (m *Model) exitFailureDetail() string {
	if m.exitFailedFromSave() {
		if m.snap.SaveError != "" {
			return "扫描结果还没有保存：" + m.snap.SaveError
		}
		return "退出收尾没有完成：扫描结果还没有保存。"
	}
	if m.exitErr != "" {
		return "扫描结果已保存，退出清理没有完成：" + m.exitErr + "。"
	}
	return "扫描结果已保存，退出清理没有完成。"
}

// copyCurrentDiagnostics copies the same privacy-bounded summary the record
// summary offers, without requiring an open record.
func (m *Model) copyCurrentDiagnostics(c *ui.Context) {
	if m.rec != nil {
		c.WriteClipboard(m.summaryDiagnosticsText())
		c.Toast("已复制诊断信息")
		return
	}
	var system app.SystemInfo
	if m.cfg.SystemInfo != nil {
		system = m.cfg.SystemInfo()
	}
	c.WriteClipboard(app.DiagnosticsText(app.DiagnosticsInput{
		Version: m.cfg.AppVersion,
		System:  system,
	}))
	c.Toast("已复制诊断信息")
}
