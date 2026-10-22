package ui

import (
	"fmt"

	"github.com/LoTwT/youkai/internal/scan"
	"github.com/egoist/mygo/ui"
)

// scanHint is the confirmed foreground-scan notice (T1 §2), shown in the
// prepare area at all times.
const scanHint = "扫描期间会自动操作游戏。请保持游戏窗口在前台，不要切换应用或手动操作游戏。扫描完成或停止后，即可校对结果。"

// stopMeans is how a scan can be stopped in this build: the window's stop
// button (a global shortcut arrives with M2's platform work, and nothing
// unregistered is claimed).
const stopMeans = "停止方式：点击本窗口的“停止”按钮。"

// View builds the main window's three areas — prepare, progress and
// results. It runs on the UI thread whenever the window needs a frame and
// only reads the model; every interaction schedules its work through the
// model's actions.
func (m *Model) View(c *ui.Context) {
	if m.cfg.PageLayout {
		m.viewPages(c)
		return
	}
	ApplyTheme(c)
	t := c.Theme()
	m.list.Key = func(row int) any {
		if m.rec == nil || row < 0 || row >= len(m.rec.Rows) {
			return row
		}
		return m.rec.Rows[row].Seq
	}
	m.list.Label = func(row int) string {
		if m.rec == nil || row < 0 || row >= len(m.rec.Rows) {
			return ""
		}
		return m.rec.Rows[row].Label()
	}
	ui.Column(c).Fill().Gap(t.Space(2)).Padding(t.Space(3), t.Space(3)).Children(func() {
		m.prepareArea(c, t)
		if m.snap.SessionID != "" {
			ui.Divider(c)
			m.progressArea(c, t)
		}
		ui.Divider(c)
		m.resultsArea(c, t)
		ui.Divider(c)
		m.storageArea(c, t)
		ui.Divider(c)
		m.updateArea(c, t)
		if m.exitState != exitIdle {
			ui.Divider(c)
			m.exitArea(c, t)
		}
	})
}

// prepareArea is the always-present start area: the confirmed hint, the
// stop means and the start button with its state.
func (m *Model) prepareArea(c *ui.Context, t *ui.Theme) {
	ui.Column(c).Gap(t.Space(1)).Children(func() {
		ui.Text(c, scanHint)
		ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
			if ui.PrimaryButton(c, "开始扫描").Disabled(!m.canStart()).Clicked() {
				m.Start()
			}
			if note := m.startNote(); note != "" {
				ui.Text(c, note).TextColor(t.TextMuted)
			}
		})
		ui.Text(c, stopMeans).TextColor(t.TextMuted).FontSize(t.FontSize * 0.9)
	})
}

// startNote explains the start button's state beside it: the wired source,
// the refusal while a session holds the active slot, the failure of the
// last attempt, or the exit in progress.
func (m *Model) startNote() string {
	switch {
	case m.exitState == exitRunning:
		return "正在退出，不能再开始扫描。"
	case m.startErr != "":
		return "开始失败：" + m.startErr
	case m.snap.Busy:
		return fmt.Sprintf("扫描进行中（会话 %s），结束后可再次开始。", m.snap.SessionID)
	case m.updateRestartImminent():
		return "正在安装更新，应用稍后将自动重启；暂不能开始扫描。"
	case m.startReqID != "":
		return "正在开始扫描……"
	case !m.source.Available:
		return "暂无可用扫描源：" + m.source.Reason
	case m.source.Label != "":
		return "扫描源：" + m.source.Label
	default:
		return ""
	}
}

// progressArea shows the session's phase, the provable counts, how it
// ended and the save state, with the idempotent stop button.
func (m *Model) progressArea(c *ui.Context, t *ui.Theme) {
	snap := m.snap
	ui.Column(c).Gap(t.Space(1)).Children(func() {
		ui.Row(c).Gap(t.Space(3)).AlignItems(ui.Center).Children(func() {
			ui.Textf(c, "会话 %s · 阶段：%s", snap.SessionID, phaseText(snap.Scan.Phase))
			if end := endText(snap.Scan); end != "" {
				ui.Text(c, "结束："+end)
			}
			ui.Box(c).Grow(1)
			if ui.Button(c, "停止").Disabled(!snap.Busy && m.startReqID == "").Clicked() {
				m.Stop()
			}
		})
		ui.Text(c, countsText(snap.Scan.Counts))
		ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
			if snap.SaveError != "" {
				ui.Text(c, "保存失败："+snap.SaveError+"（结果尚未保存）").TextColor(t.Danger)
				if ui.Button(c, "重试保存").Clicked() {
					m.RetrySave()
				}
			} else if snap.SavePending {
				ui.Textf(c, "已保存 %d 条 · 有内容待写入", snap.SavedCount)
			} else {
				ui.Textf(c, "已保存 %d 条", snap.SavedCount)
			}
		})
	})
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

// countsText renders the captured count and the five processing states.
func countsText(counts scan.Counts) string {
	return fmt.Sprintf("已采集 %d · 已产出 %d · 失败 %d · 未处理 %d · 排队 %d · 识别中 %d",
		counts.Captured, counts.Produced, counts.Failed, counts.Unprocessed,
		counts.Queued, counts.Running)
}

// endText renders how a session ended, from the kernel's first trigger;
// empty while it still runs.
func endText(s scan.Snapshot) string {
	if s.Phase != scan.PhaseEnded || s.End == "" {
		return ""
	}
	trigger := ""
	if s.Trigger != nil {
		trigger = "（" + triggerText(*s.Trigger) + "）"
	}
	switch s.End {
	case scan.EndCompleted:
		return "正常结束" + trigger
	case scan.EndCancelled:
		return "用户取消" + trigger
	case scan.EndFailed:
		return "故障结束" + trigger
	default:
		return string(s.End) + trigger
	}
}

func triggerText(trigger scan.Trigger) string {
	kind := "故障"
	if trigger.Kind == scan.TriggerUser {
		kind = "用户"
	}
	text := fmt.Sprintf("%s停止，阶段：%s", kind, phaseText(trigger.Phase))
	if trigger.Message != "" {
		text += "，" + trigger.Message
	}
	if trigger.Code != "" {
		text += "（" + trigger.Code + "）"
	}
	return text
}

// exitArea shows the explicit-exit wrap-up's progress, or its failure with
// the way forward; a completed exit never shows here (the window closed).
func (m *Model) exitArea(c *ui.Context, t *ui.Theme) {
	switch m.exitState {
	case exitRunning:
		ui.Text(c, "正在退出：停止扫描 → 保存结果 → 释放识别资源……").TextColor(t.TextMuted)
	case exitFailed:
		ui.Text(c, "退出未完成："+m.exitErr+"。请先“重试保存”，成功后再关闭窗口退出。").TextColor(t.Danger)
	}
}
