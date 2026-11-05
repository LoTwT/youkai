package ui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/egoist/mygo/ui"
)

// summaryWideMinWidth is the width from which the four count cells share one
// row. Below it (960–1119 DIP) they form two rows of two (09 §2).
const summaryWideMinWidth = 1120

// summaryFixedActionsHeight is the height below which the summary's action
// row is pinned below the scrolling body instead of following the
// conclusion (09 §2's 矮窗口 rule).
const summaryFixedActionsHeight = 720

// summaryActionKind identifies what a conclusion action does. Labels are
// computed with the counts they act on so the button and its destination
// cannot disagree.
type summaryActionKind int

const (
	summaryActionNone summaryActionKind = iota
	summaryActionExport
	summaryActionRetrySave
	summaryActionPrepare
	summaryActionRescan
	summaryActionReview
	summaryActionBackfill
	summaryActionViewItems
	summaryActionCopyDiagnostics
	summaryActionDeleteRecord
)

// summaryAction is one button of the conclusion's action row. A non-empty
// DisabledReason renders the button disabled with that reason beside it
// (09 §4.1) instead of hiding it.
type summaryAction struct {
	Kind           summaryActionKind
	Label          string
	DisabledReason string
}

// disabled reports whether the action must be shown disabled.
func (a summaryAction) disabled() bool { return a.DisabledReason != "" }

// summaryConclusion is the summary's one-sentence conclusion and its action
// row: the title answers "did the scan cover everything / can I export", the
// supplement adds a cause or a recovery step the counts and buttons do not
// carry, and Primary is the first hit of 08 §2's ①–⑤ priority.
type summaryConclusion struct {
	// Eyebrow is the small ending label above the title (wireframe 05's
	// "正常结束"); it stays empty when the title already names the ending.
	Eyebrow    string
	Title      string
	Supplement string
	// Notes carry facts the title and counts do not: the incomplete slots
	// for a partial coverage, or how much a fault saved.
	Notes     []string
	Primary   summaryAction
	Secondary []summaryAction
}

// recordSummary renders the opened record's summary view. The conclusion,
// four-count grid and collapsed detail area answer the screen's two
// questions without the item browser; a finished scan never asks whether to
// keep its screenshots (P-13).
func (m *Model) recordSummary(c *ui.Context, t *ui.Theme) {
	if m.recErr != "" {
		m.summaryUnreadable(c, t)
		return
	}
	if m.rec == nil {
		ui.Scroll(c).Grow(1).Children(func() {
			ui.Column(c).Gap(t.Space(2)).Padding(t.Space(3)).Children(func() {
				m.summaryNotices(c, t)
				ui.Text(c, m.emptyResultsText()).TextColor(t.TextMuted)
			})
		})
		return
	}
	conclusion := m.recordSummaryConclusion()
	body := func(withActions bool) {
		m.summaryNotices(c, t)
		m.summaryConclusionArea(c, t, conclusion, withActions)
		m.summaryCountsGrid(c, t)
		m.summaryDetailsArea(c, t)
		if len(m.rec.Rows) > 0 {
			m.summaryExportArea(c, t)
		}
	}

	if m.contentHeight > 0 && m.contentHeight < summaryFixedActionsHeight {
		ui.Column(c).Grow(1).Children(func() {
			ui.Scroll(c).Grow(1).Children(func() {
				ui.Column(c).Gap(t.Space(2)).Padding(t.Space(3)).Children(func() {
					body(false)
				})
			})
			ui.Row(c).Padding(t.Space(2), t.Space(3)).AlignItems(ui.Center).Children(func() {
				m.summaryActions(c, t, conclusion)
			})
		})
		return
	}
	ui.Scroll(c).Grow(1).Children(func() {
		ui.Column(c).Gap(t.Space(2)).Padding(t.Space(3)).Children(func() {
			body(true)
		})
	})
}

// summaryNotices renders the cleanup feedback and any measured deletion
// confirmation that belongs to the record workspace. It sits above the
// conclusion so a "管理截图" click is answered at the top of the view.
func (m *Model) summaryNotices(c *ui.Context, t *ui.Theme) {
	if m.cleanupNotice != "" {
		ui.Text(c, m.cleanupNotice).TextColor(t.TextMuted)
	}
	if m.cleanup != nil {
		m.cleanupPromptArea(c, t)
	}
	if m.exportBuildErr != "" {
		ui.Text(c, "导出尚未就绪："+m.exportBuildErr).TextColor(t.Danger)
	}
}

// summaryConclusionArea renders the conclusion title, its optional
// supplement and (at full height) the action row.
func (m *Model) summaryConclusionArea(c *ui.Context, t *ui.Theme, conclusion summaryConclusion, withActions bool) {
	ui.Column(c).Gap(t.Space(1)).Children(func() {
		if conclusion.Eyebrow != "" {
			ui.Text(c, conclusion.Eyebrow).TextColor(t.TextMuted).FontSize(t.FontSize * 0.8)
		}
		ui.Text(c, conclusion.Title).FontSize(t.FontSize * 1.7).FontWeight(600)
		if conclusion.Supplement != "" {
			ui.Text(c, conclusion.Supplement).TextColor(t.TextMuted)
		}
		for _, note := range conclusion.Notes {
			ui.Text(c, note).TextColor(t.TextMuted)
		}
		if withActions {
			m.summaryActions(c, t, conclusion)
		}
	})
}

// summaryActions renders the primary action first (leftmost and first in tab
// order) and the secondary actions after it. A disabled action keeps its
// place and explains why.
func (m *Model) summaryActions(c *ui.Context, t *ui.Theme, conclusion summaryConclusion) {
	ui.Column(c).Gap(t.Space(1)).Children(func() {
		ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
			if conclusion.Primary.Kind != summaryActionNone {
				primary := ui.PrimaryButton(c, conclusion.Primary.Label).
					Disabled(conclusion.Primary.disabled())
				if primary.Clicked() {
					m.performSummaryAction(c, conclusion.Primary.Kind)
				}
			}
			for _, action := range conclusion.Secondary {
				button := ui.Button(c, action.Label).Disabled(action.disabled())
				if button.Clicked() {
					m.performSummaryAction(c, action.Kind)
				}
			}
		})
		for _, action := range append([]summaryAction{conclusion.Primary}, conclusion.Secondary...) {
			if action.disabled() {
				ui.Text(c, action.DisabledReason).TextColor(t.TextMuted).
					FontSize(t.FontSize * 0.9)
			}
		}
	})
}

// performSummaryAction routes one conclusion action to the existing surface
// it names. The export dialog and the item browser's four-class filters
// arrive in later PRs; until then the actions use the existing export entry
// and the closest existing result filters.
func (m *Model) performSummaryAction(c *ui.Context, kind summaryActionKind) {
	switch kind {
	case summaryActionExport:
		m.revealExports()
	case summaryActionRetrySave:
		m.RetrySave()
	case summaryActionPrepare, summaryActionRescan:
		m.navigatePage(pageScan)
	case summaryActionReview:
		m.openSummaryClass(app.ItemClassPendingReview)
	case summaryActionBackfill:
		m.openSummaryClass(app.ItemClassPendingBackfill)
	case summaryActionViewItems:
		m.openSummaryClass("")
	case summaryActionCopyDiagnostics:
		m.copySummaryDiagnostics(c)
	case summaryActionDeleteRecord:
		if m.rec != nil && m.rec.ID != "" {
			m.RequestRecordCleanup(m.rec.ID)
		}
	}
}

// openSummaryClass opens the item browser on the rows of one four-class
// bucket. The empty class means "every item". Until PR-14b adds the
// four-class segments, 待补录 maps to the existing 无法导出 segment (failed
// and unprocessed rows), 待校对 to 待校对, and warning/passed to 全部; in
// every case the first row of the clicked class becomes the selection so the
// count and the opened item agree.
func (m *Model) openSummaryClass(class app.ItemClass) {
	if m.rec == nil {
		return
	}
	filter := resultFilterAll
	switch class {
	case app.ItemClassPendingBackfill:
		filter = resultFilterUnexportable
	case app.ItemClassPendingReview:
		filter = resultFilterPending
	}
	m.recordTab = recordTabItems
	m.resultsDetail = false
	// The item browser remembers search and set/slot filters across visits.
	// A count cell promises the class's first item, so those additional
	// filters must not hide it.
	m.search = ""
	m.setFilter = ""
	m.slotFilter = ""
	m.setResultFilter(filter)
	if class == "" {
		m.settleResultSelection()
		return
	}
	if m.selectSummaryClass(class) {
		return
	}
	// The existing pending segment only shows quality-pending rows; a class
	// the segment cannot represent (for example a produced item the export
	// refuses for validation) still opens in 全部 so the count and its item
	// stay connected until PR-14b adds the four-class filters.
	if filter != resultFilterAll {
		m.setResultFilter(resultFilterAll)
		m.selectSummaryClass(class)
	}
}

// selectSummaryClass chooses the first row of one class among the rows the
// current segment shows. It reports whether such a row exists. The class of
// each candidate is classified from the loaded checkpoint when the user
// clicks, so the summary does not need a second per-row copy of the
// classification in the item browser's state.
func (m *Model) selectSummaryClass(class app.ItemClass) bool {
	if m.rec == nil || m.recData == nil {
		return false
	}
	classes := make(map[int64]app.ItemClass, len(m.recData.Checkpoint.Items))
	for i := range m.recData.Checkpoint.Items {
		item := &m.recData.Checkpoint.Items[i]
		classes[item.Sequence] = app.ClassifyItem(*item)
	}
	for _, index := range m.visibleResultRows() {
		if classes[m.rec.Rows[index].Seq] != class {
			continue
		}
		m.selected = index
		m.selectedSequence = m.rec.Rows[index].Seq
		if display := slices.Index(m.visibleResultRows(), index); display >= 0 {
			m.scroll = display
			m.clampResultScroll()
		}
		return true
	}
	return false
}

// summaryCounts is the shared four-class partition for the summary's count
// cells, the workspace header and the pending action labels. The export
// snapshot is authoritative for the current revision. A record whose export
// cannot be built falls back to classifying the loaded checkpoint, so that
// pass runs only while the export snapshot is missing. PR-14b's per-item
// segments must read this function instead of adding a second counting rule.
func (m *Model) summaryCounts() app.ItemClassCounts {
	if m.exportData != nil {
		return m.exportData.Summary.Counts
	}
	if m.recData != nil {
		return app.ClassifyItems(m.recData.Checkpoint.Items)
	}
	if m.rec != nil {
		return m.rec.Classes
	}
	return app.ItemClassCounts{}
}

// summaryCountsGrid lays the four count cells on one row at 1120 DIP and
// above, or two rows of two in the compact width (09 §2). Every cell is a
// button: it opens the item browser filtered and selected as documented in
// openSummaryClass.
func (m *Model) summaryCountsGrid(c *ui.Context, t *ui.Theme) {
	counts := m.summaryCounts()
	cells := []struct {
		label string
		count int64
		class app.ItemClass
	}{
		{"待补录", counts.PendingBackfill, app.ItemClassPendingBackfill},
		{"待校对", counts.PendingReview, app.ItemClassPendingReview},
		{"有警告但可导出", counts.Warning, app.ItemClassWarning},
		{"已通过", counts.Passed, app.ItemClassPassed},
	}
	if m.contentWidth >= summaryWideMinWidth {
		width := m.summaryCountCellWidth(t, 4)
		ui.Row(c).Gap(t.Space(2)).Children(func() {
			for _, cell := range cells {
				m.summaryCountCell(c, t, width, cell.label, cell.count, cell.class, m.summaryCellAttention(cell.class, counts))
			}
		})
		return
	}
	width := m.summaryCountCellWidth(t, 2)
	for i := 0; i < len(cells); i += 2 {
		ui.Row(c).Gap(t.Space(2)).Children(func() {
			for _, cell := range cells[i:min(i+2, len(cells))] {
				m.summaryCountCell(c, t, width, cell.label, cell.count, cell.class, m.summaryCellAttention(cell.class, counts))
			}
		})
	}
}

// summaryCountCellWidth gives every cell the same share of the summary's
// inner width, so the four cells read as one row of equal cards (or two by
// two in the compact layout). The inner width is the window minus the rail,
// its divider and the summary padding.
func (m *Model) summaryCountCellWidth(t *ui.Theme, columns int) float32 {
	inner := float32(m.contentWidth) - navRailWidth - 1 - 2*t.Space(3)
	gaps := t.Space(2) * float32(columns-1)
	width := (inner - gaps) / float32(columns)
	if width < 1 {
		return 1
	}
	return width
}

// summaryCountCell is one clickable count: the number over its class name. A
// non-zero count carries "›" so the cell reads as a way into the item
// browser even without color; a zero count stays plain but still opens the
// filtered empty view.
func (m *Model) summaryCountCell(c *ui.Context, t *ui.Theme, width float32, label string, count int64, class app.ItemClass, attention bool) {
	_, tok := themeWithTokens(t, c.Preferences())
	display := label
	if count > 0 {
		display += " ›"
	}
	border, borderWidth := tok.BorderDefault, float32(1)
	if attention {
		border, borderWidth = tok.BorderStrong, 2
	}
	cell := ui.Button(c, "").Label(fmt.Sprintf("%s，%s 件", label, formatCount(count))).
		Width(width).Padding(t.Space(2), t.Space(2.5)).
		Background(tok.SurfaceCard).Border(borderWidth, border).Radius(t.Space(1.5))
	cell.Children(func() {
		ui.Column(c).FillWidth().AlignItems(ui.Start).Gap(t.Space(0.5)).Children(func() {
			ui.Text(c, formatCount(count)).FontSize(t.FontSize * 1.5).FontWeight(600)
			ui.Text(c, display).TextColor(tok.TextMuted).FontSize(t.FontSize * 0.9)
		})
	})
	if cell.Clicked() {
		m.openSummaryClass(class)
	}
}

// summaryCellAttention marks the classes that still need a person (待校对
// and 待补录) with the stronger border from wireframe 05, so the distinction
// does not rely on color alone.
func (m *Model) summaryCellAttention(class app.ItemClass, counts app.ItemClassCounts) bool {
	switch class {
	case app.ItemClassPendingReview:
		return counts.PendingReview > 0
	case app.ItemClassPendingBackfill:
		return counts.PendingBackfill > 0
	default:
		return false
	}
}

// summaryDetailsArea is the collapsed "详情" disclosure: coverage and
// position conclusions, count breakdown, session ending, duration,
// screenshot footprint and management entry, and the fault diagnostics.
func (m *Model) summaryDetailsArea(c *ui.Context, t *ui.Theme) {
	_, tok := themeWithTokens(t, c.Preferences())
	ui.Column(c).Background(tok.SurfaceCard).Border(1, tok.BorderSubtle).
		Radius(t.Space(1.5)).Padding(t.Space(1.5), t.Space(2)).Children(func() {
		ui.Collapsible(c, "详情", &m.summaryDetailsOpen, func() {
			ui.Column(c).Gap(t.Space(1)).Padding(t.Space(1), t.Space(0.5)).Children(func() {
				ui.Text(c, m.summaryCoverageLine()).TextColor(tok.TextSecondary)
				m.summaryPositions(c, t)
				ui.Text(c, m.summaryCountLine()).TextColor(tok.TextSecondary)
				ui.Text(c, m.summaryTerminationLine()).TextColor(tok.TextSecondary)
				ui.Text(c, m.summaryTriggerLine()).TextColor(tok.TextSecondary)
				ui.Text(c, m.summaryPhaseLine()).TextColor(tok.TextSecondary)
				ui.Text(c, m.summaryDurationLine()).TextColor(tok.TextSecondary)
				m.summaryScreenshotRow(c, t)
				m.summaryDiagnosticsArea(c, t)
			})
		})
	})
}

// summaryCoverageLine restates the conclusion the header and export share.
// "全部" is a traversal conclusion, not a total reconciliation, so the line
// says so (architecture §5 完整性与身份).
func (m *Model) summaryCoverageLine() string {
	switch m.recordCoverage() {
	case record.CoverageComplete:
		return "覆盖：全部 · 未做总数对账"
	case record.CoveragePartial:
		return "覆盖：部分"
	default:
		return "覆盖：无法确认"
	}
}

// summaryPositions lists all six slots whenever the record can report them:
// each state, a degraded slot's stage and cause, and the sort verdict. A
// record without the typed ledger or the legacy observed scope says it
// cannot list the slots instead of inventing six complete ones.
func (m *Model) summaryPositions(c *ui.Context, t *ui.Theme) {
	_, tok := themeWithTokens(t, c.Preferences())
	outcomes, known := app.PositionOutcomesOf(m.rec.Shell)
	if !known {
		ui.Text(c, "号位：记录没有保存号位结论，无法列出").TextColor(tok.TextMuted)
		return
	}
	for _, outcome := range outcomes {
		text, color := summaryPositionText(outcome), tok.TextSecondary
		if outcome.State != app.PositionOutcomeComplete {
			color = tok.WarningFG
		}
		ui.Text(c, text).TextColor(color).FontSize(t.FontSize * 0.92)
	}
	sort, known := m.rec.Shell.SortVerification()
	switch {
	case known && sort == record.SortVerificationVerified:
		ui.Text(c, "排序：已验证").TextColor(tok.TextSecondary).FontSize(t.FontSize * 0.92)
	case known:
		ui.Text(c, "排序：未验证").TextColor(tok.WarningFG).FontSize(t.FontSize * 0.92)
	default:
		ui.Text(c, "排序：未知").TextColor(tok.TextMuted).FontSize(t.FontSize * 0.92)
	}
}

// summaryPositionText writes one slot's state without a raw diagnostic code:
// a degraded slot uses the shared stage and cause wording, a slot cut by a
// session-fatal fault is "未完成", and one never started is "未扫描".
func summaryPositionText(outcome app.PositionOutcome) string {
	switch outcome.State {
	case app.PositionOutcomeComplete:
		return fmt.Sprintf("%d 号位：完整", outcome.Position)
	case app.PositionOutcomeDegraded:
		detail := positionStageText(outcome.Stage)
		if cause := faultCauseText(outcome.ReasonCode, nil); cause != "" {
			detail += "：" + cause
		}
		return fmt.Sprintf("%d 号位：不完整，%s", outcome.Position, detail)
	case app.PositionOutcomeTruncated:
		return fmt.Sprintf("%d 号位：未完成，扫描在这里停止", outcome.Position)
	default:
		return fmt.Sprintf("%d 号位：未扫描", outcome.Position)
	}
}

// summaryCountLine is the detail area's count breakdown: captured,
// exportable and excluded, with the excluded total split into the two
// person-facing classes. The four classes partition the captured items, so
// this adds up without a second counting rule.
func (m *Model) summaryCountLine() string {
	counts := m.summaryCounts()
	captured := counts.Total()
	if captured == 0 && m.rec != nil {
		captured = int64(len(m.rec.Rows))
	}
	excluded := captured - counts.Exportable()
	if excluded < 0 {
		excluded = 0
	}
	text := fmt.Sprintf("计数：采集 %s 件 · 可导出 %s 件 · 排除 %s 件",
		formatCount(captured), formatCount(counts.Exportable()), formatCount(excluded))
	if excluded > 0 {
		text += fmt.Sprintf("（待补录 %s · 待校对 %s）",
			formatCount(counts.PendingBackfill), formatCount(counts.PendingReview))
	}
	return text
}

// summaryTerminationLine states the ending type without repeating the
// title's cause, so the detail area answers "how did it end" even when the
// title was about coverage.
func (m *Model) summaryTerminationLine() string {
	term := m.rec.Shell.Termination
	if term == nil {
		if m.rec.Interrupted {
			return "结束：中断恢复"
		}
		if m.rec.Live && m.snap.Busy {
			return "结束：进行中"
		}
		return "结束：未知"
	}
	switch term.Kind {
	case record.TerminationCompleted:
		return "结束：正常结束"
	case record.TerminationCancelled:
		return "结束：手动停止"
	case record.TerminationFailed:
		return "结束：中途停止"
	case record.TerminationInterrupted:
		return "结束：中断恢复"
	default:
		return "结束：未知"
	}
}

// summaryTriggerLine names the first trigger. A raw diagnostic code stays in
// the diagnostics section, never on this surface; an unknown or absent
// trigger is "无".
func (m *Model) summaryTriggerLine() string {
	term := m.rec.Shell.Termination
	if term == nil {
		return "首个触发：无"
	}
	switch term.Kind {
	case record.TerminationCompleted, record.TerminationInterrupted:
		return "首个触发：无"
	case record.TerminationCancelled:
		return "首个触发：手动停止"
	}
	if cause := faultCauseText(term.Code, m.rec.Shell.FailureParams); cause != "" {
		return "首个触发：" + cause
	}
	return "首个触发：原因未知（见诊断信息）"
}

// summaryPhaseLine names the stage the termination occurred in, or a dash
// when the record has no trigger stage.
func (m *Model) summaryPhaseLine() string {
	term := m.rec.Shell.Termination
	if term == nil || term.Phase == "" {
		return "阶段：—"
	}
	return "阶段：" + terminationPhaseText(term.Phase)
}

// summaryDurationLine reports the wall-clock scan time. A missing end time
// is unknown, never zero.
func (m *Model) summaryDurationLine() string {
	shell := m.rec.Shell
	if shell.StartedAt.IsZero() {
		return "耗时：未知"
	}
	if shell.EndedAt == nil {
		if m.rec.Live && m.snap.Busy {
			return "耗时：进行中"
		}
		return "耗时：未知"
	}
	return "耗时：" + summaryDurationText(shell.EndedAt.Sub(shell.StartedAt))
}

// summaryDurationText rounds a scan duration to the wording 09 §5.3 uses:
// about n minutes, or hours and minutes for long sessions.
func summaryDurationText(d time.Duration) string {
	if d < time.Minute {
		return "不到 1 分钟"
	}
	minutes := int(d.Round(time.Minute) / time.Minute)
	if minutes < 60 {
		return fmt.Sprintf("约 %d 分钟", minutes)
	}
	hours, rest := minutes/60, minutes%60
	if rest == 0 {
		return fmt.Sprintf("约 %d 小时", hours)
	}
	return fmt.Sprintf("约 %d 小时 %d 分钟", hours, rest)
}

// summaryScreenshotRow shows the screenshot footprint and the management
// entry. Deletion is never offered on this screen before review/export
// (P-13): "管理截图" opens the measured confirmation, which keeps the record,
// revisions and review state.
func (m *Model) summaryScreenshotRow(c *ui.Context, t *ui.Theme) {
	_, tok := themeWithTokens(t, c.Preferences())
	ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
		ui.Text(c, m.summaryScreenshotLine()).TextColor(tok.TextSecondary)
		if !m.summaryCanManageScreenshots() {
			return
		}
		reason := m.cleanupBlockedReason()
		button := ui.Button(c, "管理截图").Disabled(reason != "")
		if button.Clicked() {
			m.RequestScreenshotCleanup(m.rec.ID)
		}
		if reason != "" {
			ui.Text(c, reason).TextColor(tok.TextMuted).FontSize(t.FontSize * 0.9)
		}
	})
}

// summaryScreenshotLine names the record's screenshot footprint. A record
// whose evidence is already missing says so instead of showing a stale size;
// a live record whose history entry has not loaded yet says unknown.
func (m *Model) summaryScreenshotLine() string {
	if m.rec.EvidenceRemoved {
		return "截图：已删除或缺失"
	}
	entry, ok := m.historyEntry(m.rec.ID)
	if !ok || entry.EvidenceFiles <= 0 {
		return "截图：占用未知"
	}
	return fmt.Sprintf("截图：%d 张 · %s", entry.EvidenceFiles, formatBytes(entry.EvidenceBytes))
}

// summaryCanManageScreenshots reports whether the detail area offers the
// measured screenshot confirmation.
func (m *Model) summaryCanManageScreenshots() bool {
	return m.rec != nil && m.rec.ID != "" && !m.rec.EvidenceRemoved && m.exportAvailable()
}

// historyEntry returns one loaded history entry by record id.
func (m *Model) historyEntry(recordID string) (HistoryEntry, bool) {
	for _, entry := range m.history {
		if entry.ID == recordID {
			return entry, true
		}
	}
	return HistoryEntry{}, false
}

// summaryHasDiagnostics reports whether this record's ending is a fault
// whose diagnostic section belongs in the summary (08 §2, 09 §4.6). An
// interrupted session has no fault trigger to report.
func (m *Model) summaryHasDiagnostics() bool {
	term := m.rec.Shell.Termination
	return term != nil && term.Kind == record.TerminationFailed
}

// summaryDiagnosticsArea is the collapsed "诊断信息" section inside 详情.
// The expanded line names the fault code, its stage, time and youkai
// version; the copy carries the privacy-bounded app.DiagnosticsText.
func (m *Model) summaryDiagnosticsArea(c *ui.Context, t *ui.Theme) {
	if !m.summaryHasDiagnostics() {
		return
	}
	_, tok := themeWithTokens(t, c.Preferences())
	ui.Collapsible(c, "诊断信息", &m.summaryDiagnosticsOpen, func() {
		ui.Column(c).Gap(t.Space(1)).Children(func() {
			ui.Text(c, m.summaryDiagnosticsLine()).TextColor(tok.TextMuted).
				FontSize(t.FontSize * 0.9)
			ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
				if ui.Button(c, "复制诊断信息").Clicked() {
					m.copySummaryDiagnostics(c)
				}
				if m.cfg.OpenDiagnosticsDirectory != nil {
					if ui.Button(c, "打开诊断目录").Clicked() {
						if err := m.cfg.OpenDiagnosticsDirectory(); err != nil {
							m.summaryDiagnosticsErr = "打开诊断目录失败：" + err.Error()
						} else {
							m.summaryDiagnosticsErr = ""
						}
					}
				}
			})
			if m.summaryDiagnosticsErr != "" {
				ui.Text(c, m.summaryDiagnosticsErr).TextColor(t.Danger).
					FontSize(t.FontSize * 0.9)
			}
		})
	})
}

// summaryDiagnosticsLine is the short on-screen diagnostic identity: fault
// code, stage, end time and version. The raw code appears only inside this
// collapsed section, never in the conclusion or detail prose.
func (m *Model) summaryDiagnosticsLine() string {
	term := m.rec.Shell.Termination
	if term == nil {
		return "故障码：无"
	}
	code := term.Code
	if code == "" {
		code = "未知"
	}
	parts := []string{"故障码：" + code, "阶段：" + terminationPhaseText(term.Phase)}
	if m.rec.Shell.EndedAt != nil {
		parts = append(parts, "时间："+summaryTimeText(*m.rec.Shell.EndedAt))
	}
	version := m.cfg.AppVersion
	if version == "" {
		version = "未知"
	}
	parts = append(parts, "youkai "+version)
	return strings.Join(parts, " · ")
}

// summaryTimeText writes a local time in the design's 09 §5.3 form.
func summaryTimeText(at time.Time) string {
	local := at.In(time.Local)
	return fmt.Sprintf("%d 月 %d 日 %02d:%02d", local.Month(), local.Day(), local.Hour(), local.Minute())
}

// copySummaryDiagnostics writes the copyable diagnostic summary for the
// record's fault and confirms the light action with a toast (09 §4.3, §4.6).
func (m *Model) copySummaryDiagnostics(c *ui.Context) {
	c.WriteClipboard(m.summaryDiagnosticsText())
	c.Toast("已复制诊断信息")
}

// summaryDiagnosticsText assembles the copyable text for the opened record.
// It delegates the shared version, system and allowlisted assembly to
// diagnosticsText; this record only supplies its termination.
func (m *Model) summaryDiagnosticsText() string {
	term := m.rec.Shell.Termination
	scan := app.DiagnosticsScanFromTermination(term)
	if scan == nil && m.rec.Interrupted {
		scan = &app.DiagnosticsScan{End: string(record.TerminationInterrupted)}
	}
	return m.diagnosticsText(scan)
}

// summaryUnreadable is the record's page-level failure state: the reason and
// a deletion entry. It never shows counts or a conclusion for a record that
// cannot be read.
func (m *Model) summaryUnreadable(c *ui.Context, t *ui.Theme) {
	ui.Scroll(c).Grow(1).Children(func() {
		ui.Column(c).Gap(t.Space(2)).Padding(t.Space(3)).Children(func() {
			m.summaryNotices(c, t)
			ui.Text(c, "记录读取失败").FontSize(t.FontSize * 1.7).FontWeight(600).TextColor(t.Danger)
			ui.Text(c, m.recErr).TextColor(t.TextMuted)
			if m.rec == nil || m.rec.ID == "" {
				return
			}
			reason := m.cleanupBlockedReason()
			button := ui.Button(c, "删除记录…").Disabled(reason != "")
			if button.Clicked() {
				m.performSummaryAction(c, summaryActionDeleteRecord)
			}
			if reason != "" {
				ui.Text(c, reason).TextColor(t.TextMuted).FontSize(t.FontSize * 0.9)
			}
		})
	})
}

// summaryWording is the summary's text before its model-specific actions are
// attached. recordSummaryWording is pure so one shared table can check the
// list and summary wording from the same classification.
type summaryWording struct {
	Eyebrow    string
	Title      string
	Supplement string
	Notes      []string
}

// summaryConclusionInput adapts the open record to the shared conclusion
// input. CapturedCount falls back to the rows so an older record without the
// four-class counts still classifies as empty or non-empty correctly.
func (m *Model) summaryConclusionInput() recordConclusionInput {
	counts := m.summaryCounts()
	captured := counts.Total()
	if captured == 0 {
		captured = int64(len(m.rec.Rows))
	}
	outcomes, outcomesKnown := app.PositionOutcomesOf(m.rec.Shell)
	incomplete := 0
	if outcomesKnown {
		for _, outcome := range outcomes {
			if outcome.State == app.PositionOutcomeDegraded {
				incomplete++
			}
		}
	}
	sort, sortKnown := m.rec.Shell.SortVerification()
	return recordConclusionInput{
		CapturedCount:            captured,
		Coverage:                 m.recordCoverage(),
		Termination:              m.rec.Shell.Termination,
		FailureParams:            m.rec.Shell.FailureParams,
		Counts:                   counts,
		Interrupted:              recordConclusionInterrupted(m.rec.Shell.Termination, m.rec.Interrupted, m.rec.Live),
		IncompletePositions:      incomplete,
		IncompletePositionsKnown: outcomesKnown,
		PositionOutcomes:         outcomes,
		PositionOutcomesKnown:    outcomesKnown,
		Sort:                     sort,
		SortKnown:                sortKnown,
		ScanNotWrapped:           m.rec.Live && m.snap.Busy,
		SaveFailed:               m.summarySaveFailed(),
	}
}

// recordSummaryConclusion composes the summary from the shared ending
// classification: recordOutcomeFor decides the facts, recordSummaryWording
// writes this surface's text, and summaryConclusionActions maps the facts to
// buttons. The order is 08 §2: save failure, zero captured items, fault,
// then the pending queue or export.
func (m *Model) recordSummaryConclusion() summaryConclusion {
	out := recordOutcomeFor(m.summaryConclusionInput())
	wording := recordSummaryWording(out)
	primary, secondary := m.summaryConclusionActions(out)
	return summaryConclusion{
		Eyebrow:    wording.Eyebrow,
		Title:      wording.Title,
		Supplement: wording.Supplement,
		Notes:      wording.Notes,
		Primary:    primary,
		Secondary:  secondary,
	}
}

// recordSummaryWording writes the summary's long-form conclusion from the
// shared outcome. The records list keeps its short wording in
// recordListConclusion.
func recordSummaryWording(out recordOutcome) summaryWording {
	in := out.Input
	switch out.Kind {
	case recordOutcomeUnreadable:
		return summaryWording{Title: "记录读取失败"}
	case recordOutcomeSaveFailed:
		return summaryWording{
			Title:      "扫描结果还没有保存",
			Supplement: "保存失败，请重试。若仍然失败，请检查磁盘空间和权限。",
		}
	case recordOutcomeRunning:
		return summaryWording{
			Title:      "扫描还没有结束",
			Supplement: "结束后这里会给出覆盖结论和可导出数量。",
		}
	case recordOutcomeEmpty:
		return emptyRecordWording(in)
	case recordOutcomeInterrupted:
		return summaryWording{Title: "扫描中断，只保存了部分结果"}
	case recordOutcomeFault:
		return faultWording(in)
	case recordOutcomeCancelled:
		return manualStopWording(in)
	case recordOutcomeCompleted:
		switch in.Coverage {
		case record.CoveragePartial:
			wording := partialCoverageWording(in)
			wording.Eyebrow = "正常结束"
			return wording
		case record.CoverageComplete:
			wording := completeCoverageWording(in)
			wording.Eyebrow = "正常结束"
			return wording
		default:
			wording := summaryWording{Title: "扫描结束，无法确认是否扫全"}
			wording.Eyebrow = "正常结束"
			return wording
		}
	case recordOutcomeUnknown:
		switch in.Coverage {
		case record.CoveragePartial:
			return partialCoverageWording(in)
		case record.CoverageComplete:
			return completeCoverageWording(in)
		default:
			return summaryWording{Title: "扫描结束，无法确认是否扫全"}
		}
	default:
		return summaryWording{Title: "结束状态未知"}
	}
}

// summarySaveFailed reports the live record's failed final save. A history
// record cannot be in this state even when the idle snapshot still carries
// the previous session's error.
func (m *Model) summarySaveFailed() bool {
	return m.rec != nil && m.rec.Live && m.snap.SaveError != ""
}

// completeCoverageWording is the normal, fully covered ending: "扫全了"
// with either export or the pending queue as the main action.
func completeCoverageWording(in recordConclusionInput) summaryWording {
	if in.Counts.Pending() == 0 {
		return summaryWording{Title: "扫全了，可以直接导出"}
	}
	return summaryWording{Title: summaryCompletePendingTitle(in.Counts)}
}

// summaryCompletePendingTitle names the queue that keeps a fully covered
// record from being exportable. 待校对 and 待补录 are separate jobs, so a
// record with both says both; a single class uses the design's wording.
func summaryCompletePendingTitle(counts app.ItemClassCounts) string {
	switch {
	case counts.PendingReview > 0 && counts.PendingBackfill > 0:
		return fmt.Sprintf("扫全了，还有 %s 件待校对、%s 件待补录",
			formatCount(counts.PendingReview), formatCount(counts.PendingBackfill))
	case counts.PendingReview > 0:
		return fmt.Sprintf("扫全了，还有 %s 件待校对", formatCount(counts.PendingReview))
	default:
		return fmt.Sprintf("扫全了，还有 %s 件待补录", formatCount(counts.PendingBackfill))
	}
}

// partialCoverageWording names the slots that did not complete and, when
// known, the unverified sort. The incomplete slots and their reasons stay
// under the title (08 §2.1, wireframe 06).
func partialCoverageWording(in recordConclusionInput) summaryWording {
	title, notes := partialCoverageTitleAndNotes(in)
	return summaryWording{Title: title, Notes: notes}
}

// partialCoverageTitleAndNotes builds "没有扫全：3 号位、5 号位不完整" plus
// "；排序未验证" when the sort was known and not verified. A record whose
// slots cannot be listed says "覆盖不完整" instead of inventing names. It
// never says "排序不一致".
func partialCoverageTitleAndNotes(in recordConclusionInput) (string, []string) {
	var incomplete []int
	var notes []string
	complete := 0
	if in.PositionOutcomesKnown {
		for _, outcome := range in.PositionOutcomes {
			if outcome.State == app.PositionOutcomeComplete {
				complete++
				continue
			}
			incomplete = append(incomplete, outcome.Position)
			notes = append(notes, summaryPositionText(outcome))
		}
		if len(incomplete) > 0 && complete > 0 {
			notes = append(notes, fmt.Sprintf("其余 %d 个号位完整", complete))
		}
	}
	title := "没有扫全：覆盖不完整"
	if len(incomplete) > 0 {
		names := make([]string, len(incomplete))
		for i, position := range incomplete {
			names[i] = fmt.Sprintf("%d 号位", position)
		}
		title = "没有扫全：" + strings.Join(names, "、") + "不完整"
	}
	if in.SortKnown && in.Sort != record.SortVerificationVerified {
		if len(incomplete) > 0 {
			title += "；排序未验证"
		} else {
			title = "没有扫全：排序未验证"
		}
	}
	return title, notes
}

// manualStopWording is the user-stopped ending. The count is the number of
// slots the traversal completed, not the slots it tried; a record without a
// position ledger omits the number rather than claiming zero.
func manualStopWording(in recordConclusionInput) summaryWording {
	title := "已手动停止"
	if in.PositionOutcomesKnown {
		complete := 0
		for _, outcome := range in.PositionOutcomes {
			if outcome.State == app.PositionOutcomeComplete {
				complete++
			}
		}
		title = fmt.Sprintf("已手动停止，扫描了 %d 个号位", complete)
	}
	return summaryWording{Title: title}
}

// faultWording names the fault's Chinese cause and its recovery action.
// The code itself stays in the diagnostics section; the saved count and slot
// coverage sit under the title (08 §2.1, wireframe 06 B).
func faultWording(in recordConclusionInput) summaryWording {
	term := in.Termination
	title := "扫描中途停止：原因未知"
	supplement := ""
	if term != nil {
		if cause := faultCauseText(term.Code, in.FailureParams); cause != "" {
			title = "扫描中途停止：" + cause
		}
		if guidance, ok := faultGuidanceFor(term.Code, in.FailureParams); ok && guidance.action != "" {
			supplement = "怎么做：" + guidance.action
		}
	}
	var notes []string
	if saved := summarySavedLine(in); saved != "" {
		notes = append(notes, saved)
	}
	return summaryWording{Title: title, Supplement: supplement, Notes: notes}
}

// summarySavedLine states how much a faulted record saved and which slots
// the saved result covers, using the same position expansion as the details.
func summarySavedLine(in recordConclusionInput) string {
	if in.CapturedCount == 0 {
		return ""
	}
	saved := "已保存：" + formatCount(in.CapturedCount) + " 件"
	if !in.PositionOutcomesKnown {
		return saved
	}
	var complete, partial []int
	for _, outcome := range in.PositionOutcomes {
		switch outcome.State {
		case app.PositionOutcomeComplete:
			complete = append(complete, outcome.Position)
		case app.PositionOutcomeDegraded, app.PositionOutcomeTruncated:
			partial = append(partial, outcome.Position)
		}
	}
	var parts []string
	if len(complete) > 0 {
		parts = append(parts, summaryPositionRanges(complete)+" 号位完整")
	}
	if len(partial) > 0 {
		names := make([]string, len(partial))
		for i, position := range partial {
			names[i] = fmt.Sprintf("%d", position)
		}
		parts = append(parts, strings.Join(names, "、")+" 号位部分")
	}
	if len(parts) > 0 {
		saved += "（" + strings.Join(parts, "，") + "）"
	}
	return saved
}

// summaryPositionRanges compresses sorted positions into "1–3、5" form.
func summaryPositionRanges(positions []int) string {
	var parts []string
	for i := 0; i < len(positions); {
		j := i
		for j+1 < len(positions) && positions[j+1] == positions[j]+1 {
			j++
		}
		if i == j {
			parts = append(parts, fmt.Sprintf("%d", positions[i]))
		} else {
			parts = append(parts, fmt.Sprintf("%d–%d", positions[i], positions[j]))
		}
		i = j + 1
	}
	return strings.Join(parts, "、")
}

// emptyRecordWording is the zero-captured-item ending. Its main action is
// always "回到准备检查" (08 §2 ②), even when the session also failed: there is
// nothing to review or export yet.
func emptyRecordWording(in recordConclusionInput) summaryWording {
	term := in.Termination
	cause, action := "", ""
	if term != nil && term.Kind == record.TerminationFailed {
		cause = faultCauseText(term.Code, in.FailureParams)
		if guidance, ok := faultGuidanceFor(term.Code, in.FailureParams); ok {
			action = guidance.action
		}
	}
	title := "没有采集到任何驱动盘"
	supplement := ""
	switch {
	case term != nil && term.Kind == record.TerminationFailed && cause != "":
		title += "：" + cause
		if action != "" {
			supplement = "怎么做：" + action
		}
	case term != nil && term.Kind == record.TerminationCancelled:
		title = "已手动停止，没有采集到任何驱动盘"
		supplement = "回到准备检查后重新开始。"
	case term != nil && term.Kind == record.TerminationInterrupted:
		title = "扫描中断，没有采集到任何驱动盘"
		supplement = "请重新开始扫描。"
	default:
		supplement = "回到准备检查后重新开始。"
	}
	return summaryWording{Title: title, Supplement: supplement}
}

// summaryConclusionActions maps the shared outcome to the summary's buttons.
// The labels are computed from the same facts as the wording, so a button
// never lands on a different count.
func (m *Model) summaryConclusionActions(out recordOutcome) (summaryAction, []summaryAction) {
	in := out.Input
	counts := in.Counts
	switch out.Kind {
	case recordOutcomeSaveFailed:
		secondary := []summaryAction{}
		if in.CapturedCount > 0 {
			secondary = append(secondary, summaryAction{
				Kind:  summaryActionViewItems,
				Label: fmt.Sprintf("查看本次采集的 %s 件", formatCount(in.CapturedCount)),
			})
		}
		return summaryAction{Kind: summaryActionRetrySave, Label: "重试保存"}, secondary
	case recordOutcomeRunning:
		return summaryAction{Kind: summaryActionPrepare, Label: "返回扫描页"}, nil
	case recordOutcomeEmpty:
		secondary := []summaryAction{}
		if in.Termination != nil && in.Termination.Kind == record.TerminationFailed && in.Termination.Code != "" {
			secondary = append(secondary, summaryAction{Kind: summaryActionCopyDiagnostics, Label: "复制诊断信息"})
		}
		return summaryAction{Kind: summaryActionPrepare, Label: "回到准备检查"}, secondary
	case recordOutcomeFault:
		secondary := []summaryAction{}
		if counts.Pending() > 0 {
			secondary = append(secondary, m.pendingPrimary(counts))
		}
		if in.CapturedCount > 0 {
			secondary = append(secondary, m.viewItemsAction())
		}
		if in.Termination != nil && in.Termination.Code != "" {
			secondary = append(secondary, summaryAction{Kind: summaryActionCopyDiagnostics, Label: "复制诊断信息"})
		}
		return summaryAction{Kind: summaryActionRescan, Label: "修正后重新扫描"}, secondary
	case recordOutcomeInterrupted:
		secondary := m.pendingSecondary(counts, false)
		if in.CapturedCount > 0 {
			secondary = append(secondary, m.viewItemsAction())
		}
		secondary = append(secondary, m.rescanAction())
		return m.pendingPrimary(counts), secondary
	case recordOutcomeCancelled:
		return m.pendingPrimary(counts), append(m.pendingSecondary(counts, false), m.rescanAction())
	case recordOutcomeCompleted, recordOutcomeUnknown:
		switch in.Coverage {
		case record.CoveragePartial:
			return m.pendingPrimary(counts), append(m.pendingSecondary(counts, false), m.rescanAction())
		case record.CoverageComplete:
			if counts.Pending() == 0 {
				secondary := []summaryAction{}
				if in.CapturedCount > 0 {
					secondary = append(secondary, summaryAction{Kind: summaryActionViewItems, Label: "查看全部件"})
				}
				return m.exportAction(), secondary
			}
			return m.pendingPrimary(counts), m.pendingSecondary(counts, true)
		default:
			return m.pendingPrimary(counts), m.pendingSecondary(counts, false)
		}
	default:
		return summaryAction{Kind: summaryActionNone, Label: ""}, nil
	}
}

// pendingPrimary is the main action when a person still has work: review
// first when any pending-review item exists, otherwise backfill. The label's
// count matches the class the action opens, so "校对 3 件" cannot land on a
// different number.
func (m *Model) pendingPrimary(counts app.ItemClassCounts) summaryAction {
	switch {
	case counts.PendingReview > 0:
		return summaryAction{
			Kind:  summaryActionReview,
			Label: fmt.Sprintf("校对 %s 件", formatCount(counts.PendingReview)),
		}
	case counts.PendingBackfill > 0:
		return summaryAction{
			Kind:  summaryActionBackfill,
			Label: fmt.Sprintf("处理 %s 件", formatCount(counts.PendingBackfill)),
		}
	default:
		return m.exportAction()
	}
}

// pendingSecondary is the action row after the pending primary. Export is
// offered only when the design names it (a normal, fully covered ending) and
// there is something to write; backfill remains a secondary path when review
// is primary.
func (m *Model) pendingSecondary(counts app.ItemClassCounts, offerExport bool) []summaryAction {
	secondary := []summaryAction{}
	if counts.PendingReview > 0 && counts.PendingBackfill > 0 {
		secondary = append(secondary, summaryAction{
			Kind:  summaryActionBackfill,
			Label: fmt.Sprintf("处理 %s 件", formatCount(counts.PendingBackfill)),
		})
	}
	if offerExport && counts.Exportable() > 0 {
		action := m.exportAction()
		action.Label = fmt.Sprintf("先导出可导出的 %s 件…", formatCount(counts.Exportable()))
		secondary = append(secondary, action)
	}
	return secondary
}

// exportAction is the shared export conclusion action. It carries the
// existing entry's disabled reason, so the summary and the title-bar button
// cannot disagree about whether export is possible.
func (m *Model) exportAction() summaryAction {
	reason := m.exportBlockedReason()
	if !m.exportAvailable() {
		reason = "当前构建未启用导出。"
	}
	return summaryAction{Kind: summaryActionExport, Label: "导出…", DisabledReason: reason}
}

// rescanAction returns to the scan page's preparation checklist. The design's
// duration estimate (P-14) is not wired yet; the label names only what it
// does. The P-12 "rescan incomplete positions" entry will sit beside this
// action once that contract exists; the first release reserves its place and
// does not show it.
func (m *Model) rescanAction() summaryAction {
	return summaryAction{Kind: summaryActionRescan, Label: "重新扫描"}
}

// viewItemsAction opens the item browser on every captured item.
func (m *Model) viewItemsAction() summaryAction {
	return summaryAction{
		Kind:  summaryActionViewItems,
		Label: fmt.Sprintf("查看已采集的 %s 件", formatCount(int64(len(m.rec.Rows)))),
	}
}
