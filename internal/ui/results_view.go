package ui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/game/zzz"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/egoist/mygo/ui"
)

// resultColumns is the legacy single-column results table: the capture
// sequence, the structured set/slot/level, the nullable management and
// equipment states and the quality with its problems.
var resultColumns = []ui.TableColumn{
	{Title: "序号", Width: 40, Align: ui.End, Fixed: true},
	{Title: "套装", Width: 90, Fixed: true},
	{Title: "槽位/等级", Width: 84, Fixed: true},
	{Title: "管理状态", Width: 72, Fixed: true},
	{Title: "装备", Width: 110, Fixed: true},
	{Title: "质量/问题", MinWidth: 280},
}

// splitResultColumns is the four-page shell's results table at both the
// 1024 split and the narrow list: the capture sequence, set, slot/level,
// main stat and state. Problems stay out of the table and appear in the
// detail subtitle and evidence description.
var splitResultColumns = []ui.TableColumn{
	{Title: "序号", Width: 40, Align: ui.End, Fixed: true},
	{Title: "套装", Width: 90, Fixed: true},
	{Title: "槽位·等级", Width: 84, Fixed: true},
	{Title: "主词条", Width: 96},
	{Title: "状态", MinWidth: 110},
}

// wideSplitResultColumns fills the 1440 layout's 560-DIP table: the set
// and main-stat columns take the extra room and the state column absorbs
// what is left, so the table never leaves an empty right edge.
var wideSplitResultColumns = []ui.TableColumn{
	{Title: "序号", Width: 40, Align: ui.End, Fixed: true},
	{Title: "套装", Width: 120, Fixed: true},
	{Title: "槽位·等级", Width: 84, Fixed: true},
	{Title: "主词条", Width: 140},
	{Title: "状态", MinWidth: 110},
}

// resultsSideBySideMinWidth starts the results page's side-by-side split.
// At 1024 the page keeps the 144-DIP rail and 24 DIP of padding; the table
// takes 420 DIPs, leaving the detail pane about 408 DIPs after its own
// padding. Below it the list and the detail are separate full-width pages.
const resultsSideBySideMinWidth = 1024

// resultsWideSplitMinWidth starts the 1440 layout's wider table.
const resultsWideSplitMinWidth = 1440

// resultsSplitTableWidth is the table pane's design width: 560 DIPs on
// 1440 screens and 420 DIPs from 1024 through 1439. The split divider and
// the detail take the rest.
func resultsSplitTableWidth(contentWidth int) float32 {
	if contentWidth >= resultsWideSplitMinWidth {
		return 560
	}
	return 420
}

// resultsColumnsFor picks the column widths that fill the current split
// table.
func resultsColumnsFor(contentWidth int) []ui.TableColumn {
	if contentWidth >= resultsWideSplitMinWidth {
		return wideSplitResultColumns
	}
	return splitResultColumns
}

func resultsSideBySide(contentWidth int) bool {
	return contentWidth >= resultsSideBySideMinWidth
}

// resultsArea is the results browser: the current record's items in
// capture order, the chosen item's evidence and recognized texts, and the
// history list to reopen saved records. The four-page shell starts its
// side-by-side split at 1024 and switches whole pages below it.
func (m *Model) resultsArea(c *ui.Context, t *ui.Theme) {
	ui.Column(c).Grow(1).Gap(t.Space(1)).Children(func() {
		if m.narrowResults() && m.resultsDetail {
			if m.completionVisible() {
				// The last confirmation finishes the review; the narrow
				// detail page gives way to the completion card.
				m.resultsDetail = false
			} else {
				m.resultsDetailArea(c, t)
				return
			}
		}
		m.resultsHeader(c, t)
		if !m.cfg.PageLayout {
			m.savePromptArea(c, t)
			if len(m.history) > 0 {
				m.legacyHistoryActions(c, t)
			}
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
			return
		}
		if m.rec == nil || len(m.rec.Rows) == 0 {
			if m.rec != nil && m.completionVisible() {
				m.completionArea(c, t)
				return
			}
			if m.rec != nil {
				m.exportArea(c, t)
			}
			ui.Text(c, m.emptyResultsText()).TextColor(t.TextMuted)
			return
		}
		if m.cfg.PageLayout {
			m.resultsFilterBar(c, t)
		}
		if m.completionVisible() {
			m.completionArea(c, t)
			return
		}
		m.exportArea(c, t)
		visible := m.visibleResultRows()
		if len(visible) == 0 {
			ui.Text(c, m.filteredEmptyText()).TextColor(t.TextMuted)
			return
		}
		if m.cfg.PageLayout && !resultsSideBySide(m.contentWidth) {
			m.tablePane(c)
			return
		}
		ui.Split(c, &m.split,
			func() { m.tablePane(c) },
			func() { m.detailPane(c) },
		).Fill()
	})
}

// resultsHeader is the results destination's title, review summary and
// page actions. A narrow detail page replaces it with its own header.
func (m *Model) resultsHeader(c *ui.Context, t *ui.Theme) {
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
		if !m.cfg.PageLayout && m.rec != nil && m.rec.ID != m.historyID(m.histChoice) {
			blocked := m.cleanupBlockedReason()
			if ui.Button(c, "删除当前记录").Disabled(blocked != "").Clicked() {
				m.RequestRecordCleanup(m.rec.ID)
			}
		}
		ui.Box(c).Grow(1)
		if m.cfg.PageLayout {
			action := "+ 准备新扫描"
			if m.snap.Busy || m.startReqID != "" {
				action = "返回扫描中"
			}
			if ui.Button(c, action).Clicked() {
				m.page = pageScan
			}
			return
		}
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
}

// resultsDetailArea is a narrow page's full-width detail: its own header
// returns to the list, and the page keeps no filters or exports.
func (m *Model) resultsDetailArea(c *ui.Context, t *ui.Theme) {
	if m.recErr != "" {
		ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
			if ui.Button(c, "← 结果列表").Clicked() {
				m.closeResultDetail()
			}
		})
		ui.Text(c, "记录读取失败："+m.recErr).TextColor(t.Danger)
		return
	}
	if m.cleanupNotice != "" {
		ui.Text(c, m.cleanupNotice)
	}
	if m.rec == nil || len(m.rec.Rows) == 0 {
		ui.Text(c, m.emptyResultsText()).TextColor(t.TextMuted)
		return
	}
	m.detailPane(c)
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

// tablePane is the results list. Every captured item stays in the record
// whatever its state; the page shell's segments, search and set/slot
// choices decide which rows the table shows, and a narrow list opens the
// chosen row's detail page.
func (m *Model) tablePane(c *ui.Context) {
	if m.rec == nil {
		return
	}
	visible := m.visibleResultRows()
	previous := m.selected
	previousDisplay := resultDisplayIndex(visible, previous)
	m.tableSelected = previousDisplay

	columns := resultColumns
	if m.cfg.PageLayout {
		columns = resultsColumnsFor(m.contentWidth)
	}
	m.list.Key = func(row int) any {
		if row < 0 || row >= len(visible) {
			return row
		}
		return m.rec.Rows[visible[row]].Seq
	}
	m.list.Label = func(row int) string {
		if row < 0 || row >= len(visible) {
			return ""
		}
		return m.rec.Rows[visible[row]].Label()
	}
	cellText := func(row int, text string) {
		cell := ui.Text(c, text).SingleLine()
		if m.narrowResults() {
			full := visible[row]
			cell.OnClick(func() { m.openResultDetailAt(full) })
		}
	}
	table := ui.Table(c, &m.list, columns, len(visible), func(row, col int) {
		r := &m.rec.Rows[visible[row]]
		switch col {
		case 0:
			cellText(row, fmt.Sprintf("%d", r.Seq))
		case 1:
			cellText(row, r.SetColumnText())
		case 2:
			cellText(row, r.SlotLevelColumnText())
		case 3:
			if m.cfg.PageLayout {
				text := r.MainStatText()
				if text == "" {
					text = "—"
				}
				cellText(row, text)
				return
			}
			cellText(row, r.ManagementColumnText())
		case 4:
			if m.cfg.PageLayout {
				cellText(row, m.resultStatusText(r))
				return
			}
			cellText(row, r.EquippedColumnText())
		case 5:
			cellText(row, r.ResultColumnText())
		}
	}).Fill().MinHeight(0)
	if m.narrowResults() {
		table.OnClick(func() { m.openResultDetail() })
	}

	chosen := m.tableSelected
	m.tableSelected = previousDisplay
	if chosen != previousDisplay && chosen >= 0 && chosen < len(visible) {
		// The table writes the chosen display row directly; settle it
		// through the editor guard so a dirty item is never replaced by a
		// plain click.
		m.selected = visible[chosen]
		m.applyTableSelection(previous)
		m.tableSelected = resultDisplayIndex(visible, m.selected)
	}
	if m.narrowResults() && table.Submitted() {
		m.openResultDetail()
	}
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
			if !m.cfg.PageLayout {
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
			} else if row.Status == record.StatusProduced {
				if row.Structured == nil {
					ui.Text(c, "该条目没有结构化字段，原始识别文本见下方证据。").TextColor(t.TextMuted)
				} else {
					m.structuredForm(c, t, row)
				}
			}
			m.evidenceArea(c, t, row)
		})
	})
}

// detailHeader shows the item's identity and outcome, with the evidence
// disclosure control. The page shell puts the problem in the subtitle
// (the table no longer has a problem column) and keeps evidence folded so
// the structured form is the main view. A narrow detail page starts with
// its way back to the list.
func (m *Model) detailHeader(c *ui.Context, t *ui.Theme, row *Row) {
	if !m.cfg.PageLayout {
		ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
			ui.Textf(c, "序号 %d · %s · 质量：%s", row.Seq, row.StatusText(), row.QualityText()).FontWeight(600)
			if row.MissingEvidence() {
				ui.Text(c, "截图已删除或缺失").TextColor(t.TextMuted).FontSize(t.FontSize * 0.9)
			}
			ui.Box(c).Grow(1)
			m.evidenceToggle(c, row)
		})
		return
	}
	if m.narrowResults() && m.resultsDetail {
		ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
			back := ui.Button(c, "← 结果列表").Disabled(m.reviewSaving || m.editingDirty())
			if back.Clicked() {
				m.closeResultDetail()
			}
			if m.reviewSaving || m.editingDirty() {
				ui.Text(c, "有未保存修改，先保存或放弃。").TextColor(t.TextMuted).
					FontSize(t.FontSize * 0.9)
			}
		})
	}
	ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
		ui.Text(c, resultDetailTitle(row)).FontWeight(600).SingleLine()
		ui.Box(c).Grow(1)
		m.evidenceToggle(c, row)
	})
	ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
		ui.Text(c, m.resultStatusText(row)).TextColor(m.resultStatusColor(t, row)).SingleLine()
		ui.Text(c, resultIssueText(row)).TextColor(resultIssueColor(t, row)).SingleLine()
	})
}

func (m *Model) evidenceToggle(c *ui.Context, row *Row) {
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
}

func (m *Model) resultStatusColor(t *ui.Theme, row *Row) ui.Color {
	if m.recentlyConfirmedNow(row) {
		return t.Success
	}
	return resultStatusColor(t, row)
}

func resultStatusColor(t *ui.Theme, row *Row) ui.Color {
	switch row.Status {
	case record.StatusFailed, record.StatusUnprocessed:
		return t.Danger
	}
	if row.PendingReview() {
		return t.Warning
	}
	return t.TextMuted
}

func resultIssueColor(t *ui.Theme, row *Row) ui.Color {
	if row.Status == record.StatusFailed || row.Status == record.StatusUnprocessed {
		return t.Danger
	}
	return t.TextMuted
}

// resultDetailTitle is the item identity the detail page names: sequence,
// set, slot and level, never a fabricated value for a row without a
// structured interpretation.
func resultDetailTitle(row *Row) string {
	parts := []string{fmt.Sprintf("%d", row.Seq)}
	if set := row.SetText(); set != "" {
		parts = append(parts, set)
	}
	if slot := row.SlotText(); slot != "" {
		parts = append(parts, slot)
	}
	if level := row.LevelText(); level != "" {
		parts = append(parts, level)
	}
	if len(parts) == 1 {
		parts = append(parts, row.StatusText())
	}
	return strings.Join(parts, " · ")
}

// resultIssueText is the detail subtitle the table's problem column used
// to carry: the failure, the unprocessed reason, the first review problem
// or the settled conclusion.
func resultIssueText(row *Row) string {
	switch row.Status {
	case record.StatusFailed:
		return "识别失败：" + row.Failure
	case record.StatusUnprocessed:
		return "未处理原因：" + unprocessedReasonText(row.Reason)
	case record.StatusProduced:
		if row.Structured == nil {
			return "未解析：没有结构化字段，原始识别文本见证据。"
		}
		if row.PendingReview() {
			check := app.CheckDiscData(row.Structured.Data)
			if problem := firstCheckProblem(check); problem != "" {
				return "需核对：" + problem
			}
			if issues := row.Issues(); len(issues) > 0 {
				return "需核对：" + issues[0].Message
			}
			return "需核对：该条目尚未确认。"
		}
		if row.Quality == record.QualityAutoAccepted {
			return "自动校验通过，未发现需核对项"
		}
		return "人工确认，未发现需核对项"
	default:
		return "识别尚未完成，暂无结构化字段。"
	}
}

// resultEvidenceNote explains what the evidence is for, carrying the same
// problem the detail subtitle names.
func resultEvidenceNote(row *Row) string {
	switch row.Status {
	case record.StatusFailed:
		return "识别失败；证据保留用于复核失败原因。"
	case record.StatusUnprocessed:
		return "识别未运行；证据保留用于确认未处理原因。"
	case record.StatusProduced:
		if row.PendingReview() {
			return "待核对；证据用于核对副标题中的问题。"
		}
		return "自动校验通过；证据供复核。"
	default:
		return "识别尚未完成。"
	}
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
		// An item that cannot be exported offers no edit or confirmation
		// actions; the read-only fields still explain why it is excluded.
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
	errors, warnings := 0, 0
	for _, issue := range issues {
		if issue.Severity == zzz.SeverityError {
			errors++
		} else {
			warnings++
		}
	}
	switch {
	case errors == 0:
		return fmt.Sprintf("%d 个警告", warnings)
	case warnings == 0:
		return fmt.Sprintf("%d 个问题（%d 个错误）", len(issues), errors)
	default:
		return fmt.Sprintf("%d 个问题（%d 个错误，%d 个警告）", len(issues), errors, warnings)
	}
}

// reviewActions offers the per-item review controls in the read-only
// view: entering the editor, confirming the item where it stands, or
// confirming it and moving on. An already-settled item has no confirmation
// action; only a pending row offers the two confirmation buttons.
func (m *Model) reviewActions(c *ui.Context, t *ui.Theme, row *Row, check app.DataCheck) {
	if row.Structured == nil {
		return
	}
	ui.Divider(c)
	unexportable := resultRowUnexportable(row)
	pending := row.PendingReview()
	editButton := func() {
		if ui.Button(c, "编辑").Disabled(m.reviewActionsBlocked()).Clicked() {
			m.startEdit()
		}
	}
	stayLabel := "确认"
	if m.reviewErr != "" && m.lastAction == reviewConfirmStay {
		stayLabel = "重试确认"
	}
	stayButton := func() {
		confirm := ui.Button(c, stayLabel).Disabled(!check.Confirmable() || m.reviewSaving)
		if confirm.Clicked() {
			m.confirmCurrentStay()
		}
	}
	nextLabel := "确认并下一项"
	if m.reviewErr != "" && m.lastAction == reviewConfirmNext {
		nextLabel = "重试确认并下一项"
	}
	nextButton := func() {
		if ui.PrimaryButton(c, nextLabel).Disabled(!check.Confirmable() || m.reviewSaving).Clicked() {
			m.confirmCurrent()
		}
	}
	selectNext := func() {
		if ui.Button(c, "下一个待校对").Disabled(m.reviewActionsBlocked()).Clicked() {
			m.selectNextPendingResult()
		}
	}
	if m.cfg.PageLayout && !m.narrowResults() {
		ui.Column(c).Gap(t.Space(1)).Children(func() {
			if !unexportable || pending {
				ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
					if !unexportable {
						editButton()
					}
					if pending {
						stayButton()
					}
				})
			}
			ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
				selectNext()
				if pending {
					nextButton()
				}
			})
		})
	} else {
		ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
			if !unexportable {
				editButton()
			}
			if pending {
				stayButton()
			}
			if m.cfg.PageLayout {
				selectNext()
			}
			if pending {
				nextButton()
			}
		})
	}
	if pending {
		if problem := firstCheckProblem(check); problem != "" {
			ui.Text(c, "确认前还需："+problem).TextColor(t.TextMuted)
		}
	}
	if m.exportBusy() {
		ui.Text(c, "正在导出，完成前不能编辑或确认。").TextColor(t.TextMuted)
	}
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
			ui.Select(c, &draft.SetID, setChoices(currentID(draft.SetID))).Width(240).Disabled(m.reviewActionsBlocked())
		})
		labeledRow(c, t, "槽位", func() {
			ui.Select(c, &draft.Slot, slotChoices(currentID(draft.Slot))).Width(120).Disabled(m.reviewActionsBlocked())
		})
		labeledRow(c, t, "等级", func() {
			m.noteTextInputFocus(ui.TextInput(c, &draft.Level).Width(80).Disabled(m.reviewActionsBlocked()))
		})

		labeledRow(c, t, "主词条", func() {
			ui.Row(c).Gap(t.Space(1)).AlignItems(ui.Center).Children(func() {
				ui.Select(c, &draft.MainAttribute, attributeChoices(attributeTextValue(draft.MainAttribute))).Width(170).Disabled(m.reviewActionsBlocked())
				m.noteTextInputFocus(ui.TextInput(c, &draft.MainValue).Width(100).Disabled(m.reviewActionsBlocked()))
				ui.Select(c, &draft.MainUnit, unitChoices(attributeTextValue(draft.MainAttribute), attributeTextValue(draft.MainUnit))).Width(150).Disabled(m.reviewActionsBlocked())
			})
		})

		m.editSubstats(c, t, edit)

		labeledRow(c, t, "管理状态", func() {
			ui.Select(c, &draft.ManagementState, managementChoices(attributeTextValue(draft.ManagementState))).Width(170).Disabled(m.reviewActionsBlocked())
		})
		labeledRow(c, t, "装备", func() {
			ui.Select(c, &draft.EquippedState, equippedChoices(optionValue(draft.EquippedState))).Width(170).Disabled(m.reviewActionsBlocked())
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

		saveLabel := "保存"
		if m.reviewErr != "" && m.lastAction == reviewSave {
			saveLabel = "重试保存"
		}
		confirmStayLabel := "保存并确认"
		if m.reviewErr != "" && m.lastAction == reviewConfirmStay {
			confirmStayLabel = "重试保存并确认"
		}
		confirmNextLabel := "保存并确认并下一项"
		if m.reviewErr != "" && m.lastAction == reviewConfirmNext {
			confirmNextLabel = "重试保存并确认并下一项"
		}
		discardButton := func() {
			if ui.Button(c, "放弃修改").Disabled(m.reviewSaving).Clicked() {
				m.discardEdit()
			}
		}
		saveButton := func() {
			if ui.Button(c, saveLabel).Disabled(!edit.canSave() || m.reviewSaving).Clicked() {
				m.saveEdit(reviewSave, nil)
			}
		}
		confirmStayButton := func() {
			if ui.Button(c, confirmStayLabel).Disabled(!edit.canConfirm() || m.reviewSaving).Clicked() {
				m.saveEdit(reviewConfirmStay, nil)
			}
		}
		confirmNextButton := func() {
			if ui.PrimaryButton(c, confirmNextLabel).Disabled(!edit.canConfirm() || m.reviewSaving).Clicked() {
				m.saveEdit(reviewConfirmNext, nil)
			}
		}
		if m.cfg.PageLayout && !m.narrowResults() {
			ui.Column(c).Gap(t.Space(1)).Children(func() {
				ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
					discardButton()
					saveButton()
					confirmStayButton()
				})
				ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
					confirmNextButton()
				})
			})
		} else {
			ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
				discardButton()
				saveButton()
				confirmStayButton()
				confirmNextButton()
			})
		}
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
			ui.Checkbox(c, &unknown, "数量未读取（保存为 null）").Disabled(m.reviewActionsBlocked())
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
		ui.Select(c, &sub.Attribute, attributeChoices(attributeTextValue(sub.Attribute))).Width(160).Disabled(m.reviewActionsBlocked())
		m.noteTextInputFocus(ui.TextInput(c, &sub.Value).Width(90).Disabled(m.reviewActionsBlocked()))
		ui.Select(c, &sub.Unit, unitChoices(attributeTextValue(sub.Attribute), attributeTextValue(sub.Unit))).Width(140).Disabled(m.reviewActionsBlocked())
		ui.Text(c, "强化").TextColor(t.TextMuted)
		m.noteTextInputFocus(ui.TextInput(c, &sub.Rolls).Width(50).Disabled(m.reviewActionsBlocked()))
		if ui.Button(c, "删除").Disabled(m.reviewActionsBlocked()).Clicked() {
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
			if ui.Button(c, saveLabel).Disabled(m.reviewActionsBlocked()).Clicked() {
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
		if m.cfg.PageLayout {
			ui.Text(c, "证据说明："+resultEvidenceNote(row)).TextColor(t.TextMuted).FontSize(t.FontSize * 0.9)
		}
		return
	}
	ui.Divider(c)
	ui.Text(c, "证据（原始 OCR）").FontWeight(600)
	if m.cfg.PageLayout {
		ui.Text(c, "证据说明："+resultEvidenceNote(row)).TextColor(t.TextMuted).
			FontSize(t.FontSize * 0.9)
	}
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
	if m.evidenceStacked() {
		ui.Column(c).Gap(t.Space(1)).Children(func() {
			m.lineRole(c, t, line, i)
			m.lineEvidence(c, t, row, i)
			m.lineText(c, t, row, line)
		})
		return
	}
	ui.Row(c).Gap(t.Space(3)).AlignItems(ui.Start).Children(func() {
		ui.Column(c).Width(96).Gap(t.Space(0.5)).Children(func() {
			m.lineRole(c, t, line, i)
		})
		m.lineEvidence(c, t, row, i)
		m.lineText(c, t, row, line)
	})
}

func (m *Model) lineRole(c *ui.Context, t *ui.Theme, line Line, i int) {
	role := line.Role
	if role == "" {
		role = fmt.Sprintf("行 %d", i+1)
	}
	ui.Text(c, role).SingleLine()
	if line.Source != "" {
		ui.Text(c, line.Source).FontSize(t.FontSize * 0.8).TextColor(t.TextMuted).SingleLine()
	}
}

func (m *Model) lineEvidence(c *ui.Context, t *ui.Theme, row *Row, i int) {
	if i < len(row.EvidenceIDs) {
		m.evidenceImage(c, t, m.rec.ID, row.EvidenceIDs[i])
	} else if row.MissingEvidence() {
		ui.Text(c, "截图已删除或缺失").TextColor(t.TextMuted).FontSize(t.FontSize * 0.9)
	}
}

func (m *Model) lineText(c *ui.Context, t *ui.Theme, row *Row, line Line) {
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
		image := ui.Image(c, entry.bitmap)
		if width, height := m.evidenceDisplaySize(); width > 0 && height > 0 {
			image.Size(width, height).Fit(ui.ScaleDown)
		}
	}
}
