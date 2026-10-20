package ui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/game/zzz"
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
// structured set/slot/level, the two explicitly nullable states and the
// quality with its problems. Failed and unprocessed items keep their row
// and explain themselves in the last column.
var resultColumns = []ui.TableColumn{
	{Title: "序号", Width: 40, Align: ui.End, Fixed: true},
	{Title: "套装", Width: 90, Fixed: true},
	{Title: "槽位/等级", Width: 84, Fixed: true},
	{Title: "管理状态", Width: 72, Fixed: true},
	{Title: "装备", Width: 110, Fixed: true},
	{Title: "质量/问题"},
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
			if m.rec != nil && len(m.rec.Rows) > 0 {
				ui.Text(c, m.pendingText()).TextColor(t.TextMuted)
			}
			if m.rec != nil && m.rec.ID != string(m.appliedSession) && m.appliedSession != "" && m.exitState != exitRunning {
				if ui.Button(c, "查看本次扫描").Clicked() {
					m.ShowLive()
				}
			}
			if m.rec != nil && m.rec.ID != m.historyID(m.histChoice) {
				blocked := m.cleanupBlockedReason()
				if ui.Button(c, "删除当前记录").Disabled(blocked != "").Clicked() {
					m.RequestRecordCleanup(m.rec.ID)
				}
			}
			ui.Box(c).Grow(1)
			if m.histChoice != "" && !m.hasHistoryLabel(m.histChoice) {
				m.histChoice = ""
			}
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
		m.savePromptArea(c, t)
		if len(m.history) > 0 {
			m.historyActions(c, t)
		}
		if m.histErr != "" {
			ui.Text(c, "历史记录读取失败："+m.histErr).TextColor(t.Danger)
		}
		if m.cleanupNotice != "" {
			ui.Text(c, m.cleanupNotice)
		}
		if m.cleanup != nil && !m.cleanup.target.all() {
			m.cleanupPromptArea(c, t)
		}
		if m.recErr != "" {
			ui.Text(c, "记录读取失败："+m.recErr).TextColor(t.Danger)
		} else if m.rec == nil || len(m.rec.Rows) == 0 {
			if m.rec != nil {
				m.exportArea(c, t)
			}
			ui.Text(c, m.emptyResultsText()).TextColor(t.TextMuted)
		} else {
			m.exportArea(c, t)
			ui.Split(c, &m.split,
				func() { m.tablePane(c) },
				func() { m.detailPane(c) },
			).Fill()
		}
	})
}

// historyActions lists the visible saved records with their metadata and
// per-record actions. The picker above stays for keyboard selection; this
// list is the per-record entry point T6 requires, kept compact with a
// bounded scroll.
func (m *Model) historyActions(c *ui.Context, t *ui.Theme) {
	blocked := m.cleanupBlockedReason()
	selected := m.selectedHistoryIDs()
	ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "日期筛选").TextColor(t.TextMuted)
		ui.TextInput(c, &m.historyFilter).Width(110).Placeholder("YYYY-MM-DD")
		if ui.Button(c, "清除筛选").Clicked() {
			m.historyFilter = ""
		}
		if m.historyFilterInvalid() {
			ui.Text(c, "格式应为 YYYY-MM-DD。").TextColor(t.Danger).FontSize(t.FontSize * 0.9)
		}
		if len(selected) > 0 {
			ui.Textf(c, "已选 %d 条", len(selected)).TextColor(t.TextMuted)
			if ui.Button(c, "删除选中截图").Disabled(blocked != "").Clicked() {
				m.RequestSelectedScreenshotCleanup()
			}
			if ui.Button(c, "删除选中记录").Disabled(blocked != "").Clicked() {
				m.RequestSelectedRecordCleanup()
			}
			if ui.Button(c, "清除选择").Clicked() {
				m.ClearHistorySelection()
			}
		}
	})
	ui.ScrollBoth(c).MaxHeight(180).Children(func() {
		for _, entry := range m.visibleHistory() {
			m.historyRow(c, t, entry, blocked)
		}
	})
	if m.historyBulk != nil {
		m.historyBulkArea(c, t)
	}
	if m.historyNotice != "" {
		ui.Text(c, m.historyNotice)
	}
	if m.historyError != "" {
		ui.Text(c, m.historyError).TextColor(t.Danger)
	}
	if blocked != "" {
		ui.Text(c, blocked).TextColor(t.TextMuted).FontSize(t.FontSize * 0.9)
	}
}

func (m *Model) historyRow(c *ui.Context, t *ui.Theme, entry HistoryEntry, blocked string) {
	ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
		selected := m.historySelected[entry.ID]
		if ui.Checkbox(c, &selected, "").Changed() {
			m.setHistorySelected(entry.ID, selected)
		}
		text := m.historyRowText(entry)
		if m.rec != nil && m.rec.ID == entry.ID {
			text += "（当前）"
		}
		ui.Text(c, text).TextColor(t.TextMuted).SingleLine().Grow(1)
		if ui.Button(c, "打开").Clicked() {
			m.histChoice = entry.Label
			m.OpenHistory()
		}
		if m.cfg.ExportSaver != nil {
			if ui.Button(c, "导出 JSON").Disabled(m.historyExportsBlocked()).Clicked() {
				m.ExportHistory(entry.ID, app.ExportJSON)
			}
			if ui.Button(c, "导出 CSV").Disabled(m.historyExportsBlocked()).Clicked() {
				m.ExportHistory(entry.ID, app.ExportCSV)
			}
		}
		if ui.Button(c, "删除截图").Disabled(blocked != "").Clicked() {
			m.RequestScreenshotCleanup(entry.ID)
		}
		if ui.Button(c, "删除当前记录").Disabled(blocked != "").Clicked() {
			m.RequestRecordCleanup(entry.ID)
		}
	})
}

func (m *Model) historyEmptyText() string {
	if m.histErr != "" {
		return "读取失败。"
	}
	if m.historyFilterInvalid() {
		return "日期格式应为 YYYY-MM-DD。"
	}
	if len(m.history) > 0 && len(m.visibleHistory()) == 0 {
		return "没有匹配筛选条件的记录。"
	}
	return "暂无已保存的记录。"
}

// resultsTitle names what the results area shows: the live session's saved
// record or a reopened history record, with the recovery mark.
func (m *Model) resultsTitle() string {
	if m.rec == nil {
		return "结果"
	}
	evidence := ""
	if m.rec.EvidenceRemoved {
		evidence = "，截图已删除或缺失"
	}
	if m.rec.Live {
		return fmt.Sprintf("结果：本次扫描（已保存 %d 条%s）", len(m.rec.Rows), evidence)
	}
	mark := ""
	if m.rec.Interrupted {
		mark = fmt.Sprintf("，中断恢复 %d 条", m.rec.InterruptedCount)
	}
	return fmt.Sprintf("结果：历史记录 %s（%d 条%s%s）", m.rec.ID, len(m.rec.Rows), mark, evidence)
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
// its state — failed and unprocessed items never disappear. Produced items
// with a structured interpretation summarize their fields; raw-only and
// unfinished items say so instead of inventing values.
func (m *Model) tablePane(c *ui.Context) {
	rows := m.rec.Rows
	previous := m.selected
	ui.Table(c, &m.list, resultColumns, len(rows), func(row, col int) {
		r := rows[row]
		switch col {
		case 0:
			ui.Textf(c, "%d", r.Seq)
		case 1:
			ui.Text(c, r.SetColumnText()).SingleLine()
		case 2:
			ui.Text(c, r.SlotLevelColumnText()).SingleLine()
		case 3:
			ui.Text(c, r.ManagementColumnText()).SingleLine()
		case 4:
			ui.Text(c, r.EquippedColumnText()).SingleLine()
		case 5:
			ui.Text(c, r.ResultColumnText()).SingleLine()
		}
	}).Fill()
	// The table writes the chosen row directly; settle it through the
	// editor guard so a dirty item is never replaced by a plain click.
	m.applyTableSelection(previous)
}

// detailPane shows the chosen item: its outcome and quality, the failure
// or unprocessed reason, the read-only structured form of a produced disc
// and, folded away by default, its evidence. The evidence area keeps every
// observed line with its image, recognized text and raw model score.
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
			m.navigationPrompt(c, t)
			m.detailHeader(c, t, row)
			switch row.Status {
			case record.StatusFailed:
				ui.Text(c, "识别失败："+row.Failure).TextColor(t.Danger)
				ui.Text(c, "该条目没有结构化字段（识别失败）。").TextColor(t.TextMuted)
			case record.StatusUnprocessed:
				ui.Text(c, "未处理原因："+unprocessedReasonText(row.Reason)).TextColor(t.Danger)
				ui.Text(c, "该条目没有结构化字段（识别未运行）。").TextColor(t.TextMuted)
			case record.StatusProduced:
				if row.Structured == nil {
					ui.Text(c, "该条目没有结构化字段，原始识别文本见下方证据。").TextColor(t.TextMuted)
				} else {
					m.structuredForm(c, t, row)
				}
			default:
				ui.Text(c, "识别尚未完成，暂无结构化字段。").TextColor(t.TextMuted)
			}
			m.evidenceArea(c, t, row)
		})
	})
}

// detailHeader shows the item's identity and outcome, with the evidence
// disclosure control. Evidence stays folded by default so the structured
// form is the main view.
func (m *Model) detailHeader(c *ui.Context, t *ui.Theme, row *Row) {
	ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
		ui.Textf(c, "序号 %d · %s · 质量：%s", row.Seq, row.StatusText(), row.QualityText()).FontWeight(600)
		if row.MissingEvidence() {
			ui.Text(c, "截图已删除或缺失").TextColor(t.TextMuted).FontSize(t.FontSize * 0.9)
		}
		ui.Box(c).Grow(1)
		if len(row.Lines) == 0 && len(row.EvidenceIDs) == 0 {
			return
		}
		label := "显示证据"
		if m.showEvidence {
			label = "隐藏证据"
		}
		if ui.Button(c, label).Clicked() {
			m.showEvidence = !m.showEvidence
		}
	})
}

// structuredForm renders the produced item's fields: read-only by
// default, editable while this item's editor is open. Every field the
// payload can leave unread says "未读取", and management state and the
// equipper keep null distinct from none/unequipped. The validation shown
// here is recomputed from the current data and rules, so it can never
// outlive the values it describes.
func (m *Model) structuredForm(c *ui.Context, t *ui.Theme, row *Row) {
	if m.edit != nil && m.rec != nil && m.edit.recordID == m.rec.ID && m.edit.seq == row.Seq {
		m.editForm(c, t, row)
		return
	}
	check := app.CheckDiscData(row.Structured.Data)
	issues := currentIssues(row, check)
	ui.Text(c, "字段（只读）").FontWeight(600)
	ui.Column(c).Gap(t.Space(1)).Children(func() {
		formField(c, t, "套装", row.SetFormText())
		formField(c, t, "稀有度", row.RarityText())
		formField(c, t, "槽位", row.SlotText())
		formField(c, t, "等级", row.LevelText())
		formField(c, t, "主词条", row.MainStatText())
		m.substatField(c, t, row)
		formField(c, t, "管理状态", row.ManagementStateText())
		formField(c, t, "装备", row.EquippedText())
		formField(c, t, "校验状态", validationStatusText(check.Validation.Status))
		formField(c, t, "问题", issueSummaryText(issues))
		for _, issue := range issues {
			field := issue.Field
			if field == "" {
				field = "整件"
			}
			ui.Textf(c, "· %s · %s（%s）：%s", field, issue.Code, issue.Severity, issue.Message).
				MaxWidth(360).FontSize(t.FontSize * 0.9).TextColor(t.TextMuted)
		}
		m.reviewActions(c, t, row, check)
	})
}

// currentIssues merges the persisted recognition issues with the
// validation recomputed for the current values, showing an exact
// duplicate once.
func currentIssues(row *Row, check app.DataCheck) []zzz.Issue {
	out := append([]zzz.Issue(nil), row.Structured.Issues...)
	for _, issue := range check.Issues {
		present := false
		for _, have := range out {
			if have == issue {
				present = true
				break
			}
		}
		if !present {
			out = append(out, issue)
		}
	}
	return out
}

// issueSummaryText is the compact count of an issue list.
func issueSummaryText(issues []zzz.Issue) string {
	if len(issues) == 0 {
		return "无问题"
	}
	errors := 0
	for _, issue := range issues {
		if issue.Severity == zzz.SeverityError {
			errors++
		}
	}
	if errors == 0 {
		return fmt.Sprintf("%d 个问题", len(issues))
	}
	return fmt.Sprintf("%d 个问题（%d 个错误）", len(issues), errors)
}

// reviewActions offers the per-item review controls in the read-only
// view: entering the editor or confirming the item as it stands.
func (m *Model) reviewActions(c *ui.Context, t *ui.Theme, row *Row, check app.DataCheck) {
	if row.Structured == nil {
		return
	}
	ui.Divider(c)
	ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
		if ui.Button(c, "编辑").Disabled(m.reviewSaving).Clicked() {
			m.startEdit()
		}
		confirmLabel := "确认并下一项"
		if m.reviewErr != "" && m.lastConfirm {
			confirmLabel = "重试确认并下一项"
		}
		confirm := ui.Button(c, confirmLabel).Disabled(!check.Confirmable() || m.reviewSaving)
		if confirm.Clicked() {
			m.confirmCurrent()
		}
		if problem := firstCheckProblem(check); problem != "" {
			ui.Text(c, "确认前还需："+problem).TextColor(t.TextMuted)
		}
	})
	if m.reviewNotice != "" {
		ui.Text(c, m.reviewNotice)
	}
	if m.reviewErr != "" {
		ui.Text(c, m.reviewErr).TextColor(t.Danger)
	}
}

// firstCheckProblem names the first reason a row cannot be confirmed.
func firstCheckProblem(check app.DataCheck) string {
	if len(check.Blocking) > 0 {
		return check.Blocking[0].Message
	}
	if len(check.Gaps) > 0 {
		return check.Gaps[0].Message
	}
	return ""
}

// editForm is the editable structured form. It shows fixed catalog
// pickers, decimal text boxes and the current revision's validation
// preview; saving is refused while a present value is malformed, and
// confirmation additionally needs every required field.
func (m *Model) editForm(c *ui.Context, t *ui.Theme, row *Row) {
	edit := m.edit
	edit.refresh()
	draft := &edit.draft

	ui.Text(c, "字段（编辑中）").FontWeight(600)
	ui.Column(c).Gap(t.Space(1)).Children(func() {
		ui.Text(c, "稀有度："+row.RarityText()+"（本修订不可编辑）").TextColor(t.TextMuted)

		labeledRow(c, t, "套装", func() {
			ui.Select(c, &draft.SetID, setChoices(currentID(draft.SetID))).Width(240).Disabled(m.reviewSaving)
		})
		labeledRow(c, t, "槽位", func() {
			ui.Select(c, &draft.Slot, slotChoices(currentID(draft.Slot))).Width(120).Disabled(m.reviewSaving)
		})
		labeledRow(c, t, "等级", func() {
			ui.TextInput(c, &draft.Level).Width(80).Disabled(m.reviewSaving)
		})

		labeledRow(c, t, "主词条", func() {
			ui.Row(c).Gap(t.Space(1)).AlignItems(ui.Center).Children(func() {
				ui.Select(c, &draft.MainAttribute, attributeChoices(attributeTextValue(draft.MainAttribute))).Width(170).Disabled(m.reviewSaving)
				ui.TextInput(c, &draft.MainValue).Width(100).Disabled(m.reviewSaving)
				ui.Select(c, &draft.MainUnit, unitChoices(attributeTextValue(draft.MainAttribute), attributeTextValue(draft.MainUnit))).Width(150).Disabled(m.reviewSaving)
			})
		})

		m.editSubstats(c, t, edit)

		labeledRow(c, t, "管理状态", func() {
			ui.Select(c, &draft.ManagementState, managementChoices(attributeTextValue(draft.ManagementState))).Width(170).Disabled(m.reviewSaving)
		})
		labeledRow(c, t, "装备", func() {
			ui.Select(c, &draft.EquippedState, equippedChoices(optionValue(draft.EquippedState))).Width(170).Disabled(m.reviewSaving)
		})
		labeledRow(c, t, "代理人", func() {
			disabled := optionValue(draft.EquippedState) != string(zzz.EquippedStateEquipped)
			ui.Select(c, &draft.AgentID, agentChoices(currentID(draft.AgentID))).Width(220).Disabled(disabled || m.reviewSaving)
		})

		ui.Divider(c)
		ui.Text(c, "校验预览："+validationStatusText(edit.check.Validation.Status)).FontWeight(600)
		for _, problem := range edit.fieldErrors {
			ui.Textf(c, "· %s：%s", reviewFieldLabel(problem.Field), problem.Message).
				MaxWidth(380).FontSize(t.FontSize * 0.9).TextColor(t.Danger)
		}
		for _, issue := range edit.check.Blocking {
			ui.Textf(c, "· %s：%s", reviewFieldLabel(issue.Field), issue.Message).
				MaxWidth(380).FontSize(t.FontSize * 0.9).TextColor(t.Danger)
		}
		for _, gap := range edit.check.Gaps {
			ui.Textf(c, "· 待补全 %s：%s", reviewFieldLabel(gap.Field), gap.Message).
				MaxWidth(380).FontSize(t.FontSize * 0.9).TextColor(t.TextMuted)
		}

		ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
			saveLabel := "保存修改"
			if m.reviewErr != "" && !m.lastConfirm {
				saveLabel = "重试保存"
			}
			if ui.Button(c, saveLabel).Disabled(!edit.canSave() || m.reviewSaving).Clicked() {
				m.saveEdit(false, nil)
			}
			confirmLabel := "确认并下一项"
			if m.reviewErr != "" && m.lastConfirm {
				confirmLabel = "重试确认并下一项"
			}
			if ui.Button(c, confirmLabel).Disabled(!edit.canConfirm() || m.reviewSaving).Clicked() {
				m.saveEdit(true, nil)
			}
			if ui.Button(c, "放弃修改").Disabled(m.reviewSaving).Clicked() {
				m.discardEdit()
			}
		})
		if m.reviewSaving {
			ui.Text(c, "正在保存……").TextColor(t.TextMuted)
		}
		if m.reviewErr != "" {
			ui.Text(c, m.reviewErr).TextColor(t.Danger)
		}
		if m.reviewNotice != "" {
			ui.Text(c, m.reviewNotice)
		}
	})
}

// editSubstats renders the substat list and its null/confirmed-empty
// distinction.
func (m *Model) editSubstats(c *ui.Context, t *ui.Theme, edit *editState) {
	draft := &edit.draft
	ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Start).Children(func() {
		ui.Text(c, "副词条").Width(72).TextColor(t.TextMuted)
		ui.Column(c).Grow(1).Gap(t.Space(1)).Children(func() {
			unknown := !draft.SubstatsKnown
			ui.Checkbox(c, &unknown, "数量未读取（保存为 null）").Disabled(m.reviewSaving)
			if unknown != !draft.SubstatsKnown {
				draft.SubstatsKnown = !unknown
				if !draft.SubstatsKnown {
					draft.Substats = nil
				}
			}
			if !draft.SubstatsKnown {
				return
			}
			for i := range draft.Substats {
				if m.editSubstatRow(c, t, i) {
					draft.Substats = append(draft.Substats[:i], draft.Substats[i+1:]...)
					break
				}
			}
			if len(draft.Substats) == 0 {
				ui.Text(c, "无（确认没有可见副词条）").TextColor(t.TextMuted)
			}
			if ui.Button(c, "添加副词条").Disabled(len(draft.Substats) >= 4 || m.reviewSaving).Clicked() {
				draft.Substats = append(draft.Substats, substatDraft{
					Attribute: unreadOption,
					Unit:      unreadOption,
				})
			}
		})
	})
}

// editSubstatRow renders one substat's pickers and decimal boxes. It
// reports whether the row's delete button was clicked.
func (m *Model) editSubstatRow(c *ui.Context, t *ui.Theme, index int) bool {
	draft := &m.edit.draft
	sub := &draft.Substats[index]
	remove := false
	ui.Row(c).Gap(t.Space(1)).AlignItems(ui.Center).Children(func() {
		ui.Textf(c, "%d", index+1).Width(16).TextColor(t.TextMuted)
		ui.Select(c, &sub.Attribute, attributeChoices(attributeTextValue(sub.Attribute))).Width(160).Disabled(m.reviewSaving)
		ui.TextInput(c, &sub.Value).Width(90).Disabled(m.reviewSaving)
		ui.Select(c, &sub.Unit, unitChoices(attributeTextValue(sub.Attribute), attributeTextValue(sub.Unit))).Width(140).Disabled(m.reviewSaving)
		ui.Text(c, "强化").TextColor(t.TextMuted)
		ui.TextInput(c, &sub.Rolls).Width(50).Disabled(m.reviewSaving)
		if ui.Button(c, "删除").Disabled(m.reviewSaving).Clicked() {
			remove = true
		}
	})
	return remove
}

// labeledRow is one editor label followed by its controls.
func labeledRow(c *ui.Context, t *ui.Theme, label string, build func()) {
	ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
		ui.Text(c, label).Width(72).TextColor(t.TextMuted)
		build()
	})
}

// navigationPrompt asks what to do with unsaved input before a record
// switch or exit. Normal browsing never shows it.
func (m *Model) navigationPrompt(c *ui.Context, t *ui.Theme) {
	if m.navPrompt == nil {
		return
	}
	exit := m.navPrompt.kind == navExit
	text := "当前条目有未保存的修改。"
	if exit {
		text = "当前条目有未保存的修改，退出前请处理。"
	}
	ui.Column(c).Gap(t.Space(1)).Children(func() {
		ui.Text(c, text).FontWeight(600)
		ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
			saveLabel, discardLabel := "保存修改并继续", "放弃修改"
			if exit {
				saveLabel, discardLabel = "保存修改并退出", "放弃修改并退出"
			}
			if ui.Button(c, saveLabel).Disabled(m.reviewSaving).Clicked() {
				m.promptSaveAndContinue()
			}
			if ui.Button(c, discardLabel).Disabled(m.reviewSaving).Clicked() {
				m.promptDiscardAndContinue()
			}
			if ui.Button(c, "留在当前项").Disabled(m.reviewSaving).Clicked() {
				m.promptStay()
			}
		})
		if m.reviewSaving {
			ui.Text(c, "正在保存……").TextColor(t.TextMuted)
		}
		if m.reviewErr != "" {
			ui.Text(c, m.reviewErr).TextColor(t.Danger)
		}
	})
}

// currentID reads a numeric picker label back to its id.
func currentID(label string) *int {
	value := optionValue(label)
	if value == "" {
		return nil
	}
	id, ok := parseDecimalInt(value)
	if !ok {
		return nil
	}
	return &id
}

// attributeTextValue reads a non-numeric picker label back to its value.
func attributeTextValue(label string) *string {
	value := optionValue(label)
	if value == "" {
		return nil
	}
	return &value
}

// reviewFieldLabel renders one data field path for the editor's error
// list.
func reviewFieldLabel(field string) string {
	switch field {
	case "":
		return "整件"
	case "setId":
		return "套装"
	case "rarity":
		return "稀有度"
	case "slot":
		return "槽位"
	case "level":
		return "等级"
	case "mainStat":
		return "主词条"
	case "substats":
		return "副词条"
	case "managementState":
		return "管理状态"
	case "equipped":
		return "装备"
	}
	if strings.HasPrefix(field, "substats[") {
		index := strings.TrimSuffix(strings.TrimPrefix(field, "substats["), "]")
		if n, err := strconv.Atoi(index); err == nil {
			return fmt.Sprintf("副词条 %d", n+1)
		}
	}
	return field
}

// formField is one label/value line of the read-only form.
func formField(c *ui.Context, t *ui.Theme, label, value string) {
	if value == "" {
		value = "—"
	}
	ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Start).Children(func() {
		ui.Text(c, label).Width(72).TextColor(t.TextMuted)
		ui.Text(c, value)
	})
}

// substatField lists the substats in display order; a null list (unknown
// line count) stays distinct from a confirmed empty one.
func (m *Model) substatField(c *ui.Context, t *ui.Theme, row *Row) {
	ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Start).Children(func() {
		ui.Text(c, "副词条").Width(72).TextColor(t.TextMuted)
		ui.Column(c).Grow(1).Gap(t.Space(0.5)).Children(func() {
			texts, known := row.SubstatTexts()
			switch {
			case !known:
				ui.Text(c, "未读取")
			case len(texts) == 0:
				ui.Text(c, "无（确认没有可见副词条）")
			default:
				for i, text := range texts {
					ui.Textf(c, "%d. %s", i+1, text)
				}
			}
		})
	})
}

// evidenceArea keeps the raw OCR lines, model scores, screenshots and
// their provenance out of the main view until the user asks for them.
func (m *Model) evidenceArea(c *ui.Context, t *ui.Theme, row *Row) {
	if len(row.Lines) == 0 && len(row.EvidenceIDs) == 0 {
		ui.Text(c, "该条目没有证据行。").TextColor(t.TextMuted)
		return
	}
	if !m.showEvidence {
		ui.Text(c, "截图与原始识别文本已折叠。").TextColor(t.TextMuted).FontSize(t.FontSize * 0.9)
		return
	}
	ui.Divider(c)
	ui.Text(c, "证据（原始 OCR）").FontWeight(600)
	if m.rec != nil && m.rec.EvidenceRemoved {
		ui.Text(c, "该记录的截图已删除或缺失，无法显示图像；识别文本与字段数据仍保留。").
			TextColor(t.TextMuted)
	}
	for i := range row.Lines {
		m.lineDetail(c, t, row, i)
	}
	// Evidence beyond the observed lines (a line without metadata still
	// has its image).
	for i := len(row.Lines); i < len(row.EvidenceIDs); i++ {
		m.lineDetail(c, t, row, i)
	}
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
		} else if row.MissingEvidence() {
			ui.Text(c, "截图已删除或缺失").TextColor(t.TextMuted).FontSize(t.FontSize * 0.9)
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
