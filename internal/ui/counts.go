package ui

import (
	"fmt"

	"github.com/LoTwT/youkai/internal/record"
)

// recordCounts is the mutually exclusive breakdown shown above the results
// and in the export summary. Captured is the sum of the other five fields.
//
// Pending review and exportable are separate: accepting a record is what
// moves it into the export. Failed, unprocessed and other-excluded records
// are not reviewable in this revision.
type recordCounts struct {
	Captured      int
	Exportable    int
	Pending       int
	Failed        int
	Unprocessed   int
	OtherExcluded int
	ExportKnown   bool
}

// countRows classifies rows for display. included is the export snapshot's
// Included count; it may reclassify legacy optional-equipped rows that the
// row list still calls pending, so the remainder is clamped rather than
// allowed to go negative.
func countRows(rows []Row, included int) recordCounts {
	c := recordCounts{Captured: len(rows), Exportable: included, ExportKnown: true}
	for _, row := range rows {
		switch {
		case row.Status == record.StatusFailed:
			c.Failed++
		case row.Status == record.StatusUnprocessed:
			c.Unprocessed++
		case row.PendingReview():
			c.Pending++
		}
	}
	c.OtherExcluded = c.Captured - c.Exportable - c.Pending - c.Failed - c.Unprocessed
	if c.OtherExcluded < 0 {
		c.OtherExcluded = 0
	}
	return c
}

// pendingText is the results header's review summary. It never presents
// pending-review zero as "everything is complete": a failed, unprocessed or
// otherwise excluded record still cannot be exported.
func (m *Model) pendingText() string {
	return reviewSummaryText(m.recordCounts())
}

func (m *Model) recordCounts() recordCounts {
	if m.rec == nil {
		return recordCounts{}
	}
	if m.exportData == nil {
		// Without an export snapshot the UI cannot distinguish a produced
		// row that is exportable from one that is not. Keep the summary
		// honest about pending/failed/unprocessed and do not invent an
		// "other excluded" count.
		c := countRows(m.rec.Rows, 0)
		c.Exportable = c.Captured - c.Pending - c.Failed - c.Unprocessed
		if c.Exportable < 0 {
			c.Exportable = 0
		}
		c.OtherExcluded = 0
		c.ExportKnown = false
		return c
	}
	return countRows(m.rec.Rows, m.exportData.Summary.Included)
}

func reviewSummaryText(c recordCounts) string {
	if c.Captured == 0 && c.Pending+c.Failed+c.Unprocessed+c.OtherExcluded == 0 {
		if c.ExportKnown {
			return "没有可导出的条目"
		}
		return "没有条目"
	}
	blocked := c.Failed + c.Unprocessed + c.OtherExcluded
	if c.Pending > 0 {
		if blocked > 0 {
			return fmt.Sprintf("待校对 %d 件 · 另有 %d 件无法导出", c.Pending, blocked)
		}
		return fmt.Sprintf("待校对 %d 件", c.Pending)
	}
	if blocked > 0 {
		return fmt.Sprintf("待校对 0 · 另有 %d 件无法导出", blocked)
	}
	if !c.ExportKnown {
		return "待校对 0"
	}
	return "待校对 0 · 全部可导出"
}
