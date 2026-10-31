package ui

import (
	"fmt"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/LoTwT/youkai/internal/scan"
	"github.com/egoist/mygo/ui"
)

// reviewSettled reports whether the active destination has reached the
// saved, review-free state the completion card describes. The live session
// must be the applied session, ended and fully saved; a history record is
// persisted by definition. A failed export snapshot does not hide the
// state: the card then explains that completion cannot be confirmed.
func (m *Model) reviewSettled() bool {
	if m.rec == nil || m.recData == nil || m.recErr != "" || m.loading {
		return false
	}
	if m.pendingCount() != 0 || m.reviewSaving || m.editingDirty() {
		return false
	}
	if m.activeSlot == resultSlotHistory {
		return true
	}
	if !m.rec.Live || m.snap.SessionID == "" || string(m.snap.SessionID) != m.rec.ID {
		return false
	}
	if m.snap.Busy || m.snap.Scan.Phase != scan.PhaseEnded ||
		m.snap.SavePending || m.snap.SaveError != "" {
		return false
	}
	return true
}

// completionVisible reports whether the pending-zero completion surface
// replaces the results table. The card belongs to the pending segment
// only; "全部" and "无法导出" keep their table even after review is done.
func (m *Model) completionVisible() bool {
	return m.filter == resultFilterPending && m.reviewSettled()
}

// completionArea renders the finished review: the empty pending queue and
// the completion card that owns the export entry. In the split layout the
// left pane stays the empty queue, matching the results table's place.
func (m *Model) completionArea(c *ui.Context, t *ui.Theme) {
	if resultsSideBySide(m.contentWidth) {
		ui.Split(c, &m.split,
			func() { m.completionEmptyPane(c, t) },
			func() { m.completionCard(c, t) },
		).Fill()
		return
	}
	m.completionCard(c, t)
}

// completionEmptyPane is the pending segment's empty state beside the
// completion card.
func (m *Model) completionEmptyPane(c *ui.Context, t *ui.Theme) {
	ui.Column(c).Fill().Padding(t.Space(3)).Gap(t.Space(1)).Children(func() {
		ui.Text(c, "待校对 0 件").FontWeight(600)
		ui.Text(c, "结果已保存，没有待校对的条目。").TextColor(t.TextMuted)
	})
}

// completionCard states the result of the review: the exportable count,
// the excluded groups, the coverage conclusion and the export actions.
// A zero-exportable or unknown export is never presented as a successful
// completion.
func (m *Model) completionCard(c *ui.Context, t *ui.Theme) {
	counts := m.recordCounts()
	ui.ScrollBoth(c).Fill().Children(func() {
		ui.Column(c).Gap(t.Space(2)).Padding(t.Space(3)).Children(func() {
			ui.Column(c).Gap(t.Space(2)).Padding(t.Space(3)).
				Background(t.Surface).Border(1, t.Border).Radius(t.Radius).
				Children(func() {
					title, titleColor := "校对完成", t.Text
					switch {
					case !counts.ExportKnown:
						title, titleColor = "不可确认完成", t.Warning
					case counts.Exportable == 0:
						title, titleColor = "无可导出条目", t.Warning
					}
					ui.Text(c, title).FontWeight(600).TextColor(titleColor)
					if counts.ExportKnown {
						ui.Textf(c, "可导出 %d 件 · 共采集 %d 件", counts.Exportable, counts.Captured)
						if counts.Exportable == 0 {
							ui.Text(c, "不可确认：无可导出的条目。").TextColor(t.Warning)
						}
					} else {
						ui.Text(c, "不可确认：导出清单尚未就绪。").TextColor(t.Warning)
					}
					ui.Text(c, completionExclusionText(counts)).TextColor(t.TextMuted)
					ui.Text(c, m.completionCoverageText()).TextColor(t.TextMuted)
					if note := m.incompletePositionsNote(); note != "" && !m.sessionTerminationShown() {
						ui.Text(c, note).TextColor(t.Warning)
					}
					if !m.exportAvailable() {
						return
					}
					blocked := m.exportBlockedReason()
					ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
						if ui.Button(c, "导出 JSON").Disabled(blocked != "").Clicked() {
							m.ExportJSON()
						}
						if ui.Button(c, "导出 CSV").Disabled(blocked != "").Clicked() {
							m.ExportCSV()
						}
					})
					if blocked != "" {
						ui.Text(c, blocked).TextColor(t.TextMuted).FontSize(t.FontSize * 0.9)
					}
				})
		})
	})
}

// completionExclusionText is the mutually exclusive exclusion breakdown
// the card shows: failed, unprocessed and otherwise excluded items.
func completionExclusionText(counts recordCounts) string {
	return fmt.Sprintf("排除：失败 %d 件 · 未处理 %d 件 · 其他 %d 件",
		counts.Failed, counts.Unprocessed, counts.OtherExcluded)
}

// completionCoverageText states the coverage conclusion of the card's own
// record. A partial coverage is never rounded up and names the snapshot
// mark the exported file will carry.
func (m *Model) completionCoverageText() string {
	status := record.CoverageUnknown
	var summary *app.ExportSummary
	if m.exportData != nil {
		summary = &m.exportData.Summary
		status = summary.Coverage
	} else if m.recData != nil {
		status = m.recData.Checkpoint.Session.Coverage.Status
	}
	switch status {
	case record.CoverageComplete:
		return "覆盖：完整"
	case record.CoveragePartial:
		text := "覆盖：部分覆盖"
		if summary != nil && summary.ExpectedCount != nil && *summary.ExpectedCount > 0 {
			percent := float64(summary.Included) * 100 / float64(*summary.ExpectedCount)
			text = fmt.Sprintf("覆盖：部分覆盖 %.0f%%", percent)
		}
		return text + "（导出文件会标注“部分快照”）"
	default:
		return "覆盖：无法确认"
	}
}

// completionStatusText is the results page's status line while the
// completion card shows. It names the saved state and the exportable
// count without claiming a count the export snapshot cannot prove.
func (m *Model) completionStatusText() string {
	counts := m.recordCounts()
	switch {
	case !counts.ExportKnown:
		return "已保存 · 待校对 0 · 导出清单尚未就绪"
	case counts.Exportable == 0:
		return "已保存 · 待校对 0 · 无可导出条目"
	default:
		return fmt.Sprintf("已保存 · 待校对 0 · 可导出 %d 件", counts.Exportable)
	}
}
