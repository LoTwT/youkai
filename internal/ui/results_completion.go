package ui

import (
	"fmt"

	"github.com/LoTwT/youkai/internal/record"
	"github.com/LoTwT/youkai/internal/scan"
)

// reviewSettled reports whether the active destination has reached the
// saved, review-free state the completion surface describes. The live session
// must be the applied session, ended and fully saved; a history record is
// persisted by definition. A failed export snapshot does not hide the
// state: the surface then explains that completion cannot be confirmed.
func (m *Model) reviewSettled() bool {
	if m.rec == nil || m.recData == nil || m.recErr != "" || m.loading {
		return false
	}
	if m.pendingItemsCount() != 0 || m.reviewSaving || m.editingDirty() {
		return false
	}
	if !m.rec.Live {
		return true
	}
	return !m.liveScanInProgress()
}

// liveScanInProgress reports a live record whose scan has not ended and
// saved. A history record is never in progress, even with a dirty editor.
func (m *Model) liveScanInProgress() bool {
	if m.rec == nil || !m.rec.Live {
		return false
	}
	if m.snap.SessionID == "" || string(m.snap.SessionID) != m.rec.ID {
		return true
	}
	return m.snap.Busy || m.snap.Scan.Phase != scan.PhaseEnded ||
		m.snap.SavePending || m.snap.SaveError != ""
}

// completionVisible reports whether the pending-zero completion surface
// replaces the results table. It belongs to the pending segment only; "全部"
// and "无法导出" keep their table even after review is done.
func (m *Model) completionVisible() bool {
	return m.filter == resultFilterPending && m.reviewSettled()
}

// recordCoverage is the record's single coverage conclusion: the frozen
// export snapshot when one exists, else the loaded checkpoint, else the
// view's shell (a view built without its recovery, as in tests). The
// workspace title bar and the summary body both read it, so they cannot
// disagree (M-1).
func (m *Model) recordCoverage() record.CoverageStatus {
	if m.exportData != nil {
		return m.exportData.Summary.Coverage
	}
	if m.recData != nil {
		return m.recData.Checkpoint.Session.Coverage.Status
	}
	if m.rec != nil {
		return m.rec.Shell.Coverage.Status
	}
	return record.CoverageUnknown
}

// completionStatusText is the results page's status line while the
// completion surface shows. It names the saved state and the exportable
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
