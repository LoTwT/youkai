package ui

import (
	"fmt"

	"github.com/LoTwT/youkai/internal/record"
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

// resultColumns is the results table's layout: the capture sequence, the
// raw processing state and a one-line content summary.
var resultColumns = []ui.TableColumn{
	{Title: "序号", Width: 64, Align: ui.End},
	{Title: "状态", Width: 130},
	{Title: "内容"},
}

// View builds the main window's three areas — prepare, progress and
// results. It runs on the UI thread whenever the window needs a frame and
// only reads the model; every interaction schedules its work through the
// model's actions.
func (m *Model) View(c *ui.Context) {
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
			if ui.Button(c, "停止").Disabled(!snap.Busy).Clicked() {
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

// resultsArea is the results browser: the current record's items in
// capture order, the chosen item's evidence and recognized texts, and the
// history list to reopen saved records.
func (m *Model) resultsArea(c *ui.Context, t *ui.Theme) {
	ui.Column(c).Grow(1).Gap(t.Space(1)).Children(func() {
		ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
			ui.Text(c, m.resultsTitle()).FontWeight(600)
			if m.rec != nil && !m.rec.Live && m.appliedSession != "" && m.exitState != exitRunning {
				if ui.Button(c, "查看本次扫描").Clicked() {
					m.ShowLive()
				}
			}
			ui.Box(c).Grow(1)
			if labels := m.HistoryLabels(); len(labels) > 0 {
				ui.Text(c, "历史记录").TextColor(t.TextMuted)
				ui.Select(c, &m.histChoice, labels).Width(220)
				if ui.Button(c, "打开记录").Disabled(m.historyID(m.histChoice) == "").Clicked() {
					m.OpenHistory()
				}
			} else {
				ui.Text(c, "历史记录："+m.historyEmptyText()).TextColor(t.TextMuted)
			}
		})
		if m.histErr != "" {
			ui.Text(c, "历史记录读取失败："+m.histErr).TextColor(t.Danger)
		}
		if m.recErr != "" {
			ui.Text(c, "记录读取失败："+m.recErr).TextColor(t.Danger)
		} else if m.rec == nil || len(m.rec.Rows) == 0 {
			ui.Text(c, m.emptyResultsText()).TextColor(t.TextMuted)
		} else {
			ui.Split(c, &m.split,
				func() { m.tablePane(c) },
				func() { m.detailPane(c) },
			).Fill()
		}
	})
}

func (m *Model) historyEmptyText() string {
	if m.histErr != "" {
		return "读取失败。"
	}
	return "暂无已保存的记录。"
}

// resultsTitle names what the results area shows: the live session's saved
// record or a reopened history record, with the recovery mark.
func (m *Model) resultsTitle() string {
	if m.rec == nil {
		return "结果"
	}
	if m.rec.Live {
		return fmt.Sprintf("结果：本次扫描（已保存 %d 条）", len(m.rec.Rows))
	}
	mark := ""
	if m.rec.Interrupted {
		mark = fmt.Sprintf("，中断恢复 %d 条", m.rec.InterruptedCount)
	}
	return fmt.Sprintf("结果：历史记录 %s（%d 条%s）", m.rec.ID, len(m.rec.Rows), mark)
}

func (m *Model) emptyResultsText() string {
	if m.rec == nil {
		return "还没有扫描结果。开始一次扫描，或从历史记录打开一条已保存的记录。"
	}
	if m.rec.Live {
		return "本次扫描还没有已保存的条目；扫描过程中条目按批次保存，收尾后全部可见。"
	}
	return "该记录没有条目。"
}

// tablePane is the results list. Every captured item stays in it whatever
// its state — failed and unprocessed items never disappear.
func (m *Model) tablePane(c *ui.Context) {
	rows := m.rec.Rows
	ui.Table(c, &m.list, resultColumns, len(rows), func(row, col int) {
		r := rows[row]
		switch col {
		case 0:
			ui.Textf(c, "%d", r.Seq)
		case 1:
			ui.Text(c, r.StatusText()).SingleLine()
		case 2:
			ui.Text(c, r.Summary()).SingleLine()
		}
	}).Fill()
}

// detailPane shows the chosen item: its outcome and quality, the failure
// or unprocessed reason, and every observed line with its evidence image,
// recognized text and raw model score.
func (m *Model) detailPane(c *ui.Context) {
	t := c.Theme()
	var row *Row
	if m.rec != nil && m.selected >= 0 && m.selected < len(m.rec.Rows) {
		row = &m.rec.Rows[m.selected]
	}
	ui.ScrollBoth(c).Fill().Padding(t.Space(2)).Children(func() {
		if row == nil {
			ui.Text(c, "选择一条记录查看详情。").TextColor(t.TextMuted)
			return
		}
		ui.Column(c).Gap(t.Space(2)).Children(func() {
			ui.Textf(c, "序号 %d · %s · 质量：%s", row.Seq, row.StatusText(), row.QualityText()).FontWeight(600)
			if row.Status == record.StatusFailed {
				ui.Text(c, "识别失败："+row.Failure).TextColor(t.Danger)
			}
			if row.Status == record.StatusUnprocessed {
				ui.Text(c, "未处理原因："+unprocessedReasonText(row.Reason)).TextColor(t.Danger)
			}
			for i := range row.Lines {
				m.lineDetail(c, t, row, i)
			}
			// Evidence beyond the observed lines (a line without metadata
			// still has its image).
			for i := len(row.Lines); i < len(row.EvidenceIDs); i++ {
				m.lineDetail(c, t, row, i)
			}
			if len(row.Lines) == 0 && len(row.EvidenceIDs) == 0 {
				ui.Text(c, "该条目没有证据行。").TextColor(t.TextMuted)
			}
		})
	})
}

func unprocessedReasonText(reason record.UnprocessedReason) string {
	switch reason {
	case record.ReasonCancelled:
		return "用户取消（识别未运行）"
	case record.ReasonInterrupted:
		return "进程中断（识别未完成）"
	default:
		return string(reason)
	}
}

// lineDetail shows one observed line: its role and provenance, its
// evidence image and its recognized text with the raw model score. The
// image belongs to this item's evidence identity — a load that completes
// after a switch to another item lands in the cache under its own key and
// can never attach here.
func (m *Model) lineDetail(c *ui.Context, t *ui.Theme, row *Row, i int) {
	var line Line
	if i < len(row.Lines) {
		line = row.Lines[i]
	}
	ui.Row(c).Gap(t.Space(3)).AlignItems(ui.Start).Children(func() {
		ui.Column(c).Width(96).Gap(t.Space(0.5)).Children(func() {
			role := line.Role
			if role == "" {
				role = fmt.Sprintf("行 %d", i+1)
			}
			ui.Text(c, role).SingleLine()
			if line.Source != "" {
				ui.Text(c, line.Source).FontSize(t.FontSize * 0.8).TextColor(t.TextMuted).SingleLine()
			}
		})
		if i < len(row.EvidenceIDs) {
			m.evidenceImage(c, t, m.rec.ID, row.EvidenceIDs[i])
		}
		ui.Column(c).Grow(1).Gap(t.Space(0.5)).Children(func() {
			switch {
			case line.Scored:
				ui.Text(c, line.Text)
				ui.Textf(c, "模型分数 %.3f", line.Score).
					FontSize(t.FontSize * 0.85).TextColor(t.TextMuted)
			case row.Status == record.StatusProduced:
				ui.Text(c, "（该行未识别出文本）").TextColor(t.TextMuted)
			default:
				ui.Text(c, "（未运行识别）").TextColor(t.TextMuted)
			}
		})
	})
}

// evidenceImage shows one evidence image at its own size, loading it on
// demand: absent shows a placeholder, a failed load shows the failure, and
// a completed load never attaches to another item (the cache is keyed by
// the image's own identity).
func (m *Model) evidenceImage(c *ui.Context, t *ui.Theme, recordID, id string) {
	if id == "" || m.cache.load == nil {
		return
	}
	entry := m.evidenceFor(recordID, id)
	switch {
	case entry == nil:
		ui.Text(c, "证据图片加载中……").TextColor(t.TextMuted)
	case entry.err != nil:
		ui.Textf(c, "证据图片不可用：%v", entry.err).TextColor(t.Danger)
	default:
		ui.Image(c, entry.bitmap)
	}
}
