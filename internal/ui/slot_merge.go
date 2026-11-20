package ui

import (
	"errors"
	"fmt"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/egoist/mygo/ui"
)

type slotMergeDialog struct {
	supplementID string
	preview      app.SlotMergePreview
	loading      bool
	running      bool
	err          string
	chosen       [record.PositionCount + 1]bool
}

func (m *Model) hasMergedSlots() bool {
	return m.recData != nil && len(m.recData.Checkpoint.SlotMerges) != 0
}

func (m *Model) slotMergeCandidate() bool {
	if m.rec == nil || m.recData == nil || m.loading || m.recErr != "" {
		return false
	}
	shell := m.rec.Shell
	if shell.Client != record.ClientWindowsLocal || shell.Termination == nil {
		return false
	}
	plan, ok := app.DeclaredScanPlan(shell.Scope)
	if !ok || plan.Full() {
		return false
	}
	ledger, ok := shell.PositionConclusions()
	if !ok {
		return false
	}
	for _, position := range ledger.Positions {
		if position.Complete && plan.Includes(position.Position) {
			return true
		}
	}
	return false
}

func (m *Model) slotMergeBlockedReason() string {
	switch {
	case m.exitIntent != exitIntentNone || m.exitState == exitRunning || m.exitState == exitDone:
		return "应用正在退出。"
	case m.snap.Busy || m.snap.SavePending || m.snap.SaveError != "":
		return "扫描结束并保存后才能合并。"
	case m.exporting || m.exportDialog.open || m.cleanup != nil || m.reviewSaving || m.editingDirty():
		return "请先完成当前的导出、清理或校对。"
	default:
		return ""
	}
}

func (m *Model) openSlotMerge() {
	if m.slotMerge != nil || !m.slotMergeCandidate() || m.slotMergeBlockedReason() != "" {
		return
	}
	dialog := &slotMergeDialog{supplementID: m.rec.ID, loading: true}
	m.slotMerge = dialog
	m.goAsync(func() {
		preview, err := m.cfg.Service.PreviewSlotMerge(dialog.supplementID)
		m.update(func() {
			if m.slotMerge != dialog {
				return
			}
			dialog.loading = false
			if err != nil {
				dialog.err = slotMergeError(err)
				return
			}
			dialog.preview = preview
			for _, choice := range preview.Choices {
				dialog.chosen[choice.Slot] = true
			}
		})
	})
}

func (m *Model) confirmSlotMerge(dialog *slotMergeDialog, slots []int) {
	if m.slotMerge != dialog || dialog.loading || dialog.running || dialog.err != "" || len(slots) == 0 || m.slotMergeBlockedReason() != "" {
		return
	}
	preview := dialog.preview
	req := app.SlotMergeRequest{
		RecordID: preview.RecordID, RecordRevision: preview.RecordRevision,
		SupplementID: preview.SupplementID, SupplementRevision: preview.SupplementRevision,
		Slots: slots,
	}
	dialog.running = true
	m.goAsync(func() {
		_, err := m.cfg.Service.MergeSlots(req)
		m.update(func() {
			if m.slotMerge != dialog {
				return
			}
			dialog.running = false
			if err != nil {
				dialog.err = slotMergeError(err)
				return
			}
			m.slotMerge = nil
			m.loadHistory()
			if m.displayPage() == pageResults && m.recordIsOpen(dialog.supplementID) {
				m.requestNavigation(navigationRequest{kind: navOpenRecord, recordID: req.RecordID})
				m.summaryDetailsOpen = true
			}
			m.setReviewNoticeAcrossPages(fmt.Sprintf("已将 %s 号位合并回原记录。", summaryPositionRanges(slots)))
		})
	})
}

func slotMergeError(err error) string {
	switch {
	case errors.Is(err, app.ErrSlotMergeUnavailable):
		return "没有可合并的号位。补扫号位须已扫完整，且在原记录中仍然不完整。"
	case errors.Is(err, app.ErrSlotMergeUnassigned):
		return "有观测缺少可靠的采集号位，无法确认应替换哪些观测。"
	case errors.Is(err, record.ErrRevisionConflict):
		return "原记录或补扫记录已更新。关闭对话框后重新预览。"
	case errors.Is(err, app.ErrSlotMergeBusy), errors.Is(err, app.ErrServiceClosing):
		return "扫描、保存或退出尚未完成，暂时无法合并。"
	default:
		return "合并失败。请检查两条记录是否可读、磁盘空间是否充足，再重新预览。"
	}
}

func (m *Model) slotMergeDialogView(c *ui.Context, t *ui.Theme) {
	dialog := m.slotMerge
	if dialog == nil {
		return
	}
	if !dialog.running && (m.displayPage() != pageResults || !m.recordIsOpen(dialog.supplementID)) {
		m.slotMerge = nil
		return
	}
	open := true
	m.modal(c, &open, func() {
		ui.Column(c).Width(t.Space(112)).Gap(t.Space(2)).Children(func() {
			ui.Text(c, "合并回原记录").FontWeight(600)
			switch {
			case dialog.err != "":
				ui.Text(c, dialog.err).TextColor(t.Danger)
			case dialog.running:
				ui.Text(c, "正在保存合并结果……")
			case dialog.loading:
				ui.Text(c, "正在核对两条记录……")
			default:
				m.slotMergeChoices(c, t, dialog)
			}
			if !dialog.running && (dialog.err != "" || dialog.loading) {
				actionRow(c).Children(func() {
					if button(c, "关闭").Clicked() {
						open = false
					}
				})
			}
		})
	})
	if !open && !dialog.running {
		m.slotMerge = nil
	}
}

func (m *Model) slotMergeChoices(c *ui.Context, t *ui.Theme, dialog *slotMergeDialog) {
	preview := dialog.preview
	ui.Textf(c, "原记录 %s · 修订 %d    补扫修订 %d",
		preview.RecordStartedAt.Local().Format("2006-01-02 15:04"), preview.RecordRevision, preview.SupplementRevision)
	ui.Text(c, "选中的号位将整位替换。原来的观测、校对和截图会归档，补扫记录仍可单独打开。").
		TextColor(t.TextMuted)
	var slots []int
	for _, choice := range preview.Choices {
		ui.Checkbox(c, &dialog.chosen[choice.Slot],
			fmt.Sprintf("%d 号位：%d 件 → %d 件（归档 %d 件有校对修订的观测）",
				choice.Slot, choice.PreviousCount, choice.SupplementCount, choice.PreviousReviewed))
		if dialog.chosen[choice.Slot] {
			slots = append(slots, choice.Slot)
		}
	}
	reason := m.slotMergeBlockedReason()
	if reason != "" {
		ui.Text(c, reason).TextColor(t.TextMuted)
	}
	actionRow(c).Wrap().Children(func() {
		if button(c, "取消").Clicked() {
			m.slotMerge = nil
		}
		if primaryButton(c, "确认合并").Disabled(len(slots) == 0 || reason != "").Clicked() {
			m.confirmSlotMerge(dialog, slots)
		}
	})
}

func (m *Model) summarySlotSources(c *ui.Context, t *ui.Theme) {
	cp := &m.recData.Checkpoint
	ui.Text(c, "号位采集来源").FontWeight(600)
	for _, src := range cp.SlotSources() {
		window := src.StartedAt.Local().Format("2006-01-02 15:04")
		if src.EndedAt != nil {
			window += " 至 " + src.EndedAt.Local().Format("2006-01-02 15:04")
		}
		ui.Textf(c, "%d 号位 · %s · 会话 %s · 修订 %d", src.Slot, window, src.SessionID, src.SavedRevision).
			FontSize(t.FontSize * 0.92).TextColor(t.TextMuted)
	}
	for _, merge := range cp.SlotMerges {
		var slots []int
		for _, src := range merge.Sources {
			slots = append(slots, src.Slot)
		}
		ui.Textf(c, "%s 号位 · 已归档修订 %d 的 %d 件原观测", summaryPositionRanges(slots), merge.BaseRevision, len(merge.Replaced)).
			FontSize(t.FontSize * 0.92).TextColor(t.TextMuted)
	}
}
