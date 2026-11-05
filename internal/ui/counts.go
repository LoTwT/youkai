package ui

import (
	"fmt"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/record"
)

// recordCounts is the mutually exclusive breakdown shown above the results
// and in the export summary. Captured is the sum of the other five fields.
//
// Pending review and exportable are separate: accepting a record is what
// moves it into the export. Failed and Unprocessed here mean only the
// unbackfilled observations; an interpreted backfilled item follows its
// current class, and other-excluded records are not reviewable.
type recordCounts struct {
	Captured      int
	Exportable    int
	Pending       int
	Failed        int
	Unprocessed   int
	OtherExcluded int
	ExportKnown   bool
}

// countRows classifies built rows for display from their four-class value,
// never from the raw processing Status: a backfilled failed item belongs to
// pending review or an exportable class. included is the export snapshot's
// Included count; it only reconciles the remaining excluded count, so a
// legacy optional-equipped reclassification never makes the sum negative.
func countRows(rows []Row, included int) recordCounts {
	return countRowsWithClasses(rows, rowClassCounts(rows), included, true)
}

func countRowsWithClasses(rows []Row, classes app.ItemClassCounts, included int, exportKnown bool) recordCounts {
	c := recordCounts{
		Captured:    int(classes.Total()),
		Exportable:  int(classes.Exportable()),
		Pending:     int(classes.PendingReview),
		ExportKnown: exportKnown,
	}
	splitBackfill := 0
	for i := range rows {
		row := &rows[i]
		if row.ItemClass != app.ItemClassPendingBackfill {
			continue
		}
		splitBackfill++
		switch row.Status {
		case record.StatusFailed:
			c.Failed++
		case record.StatusUnprocessed:
			c.Unprocessed++
		default:
			c.OtherExcluded++
		}
	}
	// Classes.PendingBackfill is split above when rows exist. A fallback
	// class count without rows keeps the total honest as other-excluded.
	if missing := int(classes.PendingBackfill) - splitBackfill; missing > 0 {
		c.OtherExcluded += missing
	}
	if exportKnown {
		if other := c.Captured - included - c.Pending - c.Failed - c.Unprocessed; other > c.OtherExcluded {
			c.OtherExcluded = other
		}
	}
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
	included := 0
	known := m.exportData != nil
	if known {
		included = m.exportData.Summary.Included
	}
	return countRowsWithClasses(m.rec.Rows, m.recordClassCounts(), included, known)
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
