package app

import (
	"fmt"
	"time"

	"github.com/LoTwT/youkai/internal/record"
)

// HistoryRecord is the history list's metadata. It deliberately comes
// from Peek rather than Load: listing many records must not hash every
// evidence file, and a record whose metadata is readable should still be
// listed when opening it would later fail on missing evidence.
type HistoryRecord struct {
	ID            string
	SavedAt       time.Time
	Client        record.Client
	CapturedCount int64
	Coverage      record.CoverageStatus
	// CoverageExpected is the scope's expected item count; 0 when the
	// record does not know it. The history table only shows a percentage
	// when it can divide by a real expected count.
	CoverageExpected int64
	PendingReview    int
	// ExportableCount, FailedCount, UnprocessedCount and OtherCount are the
	// mutually exclusive parts of CapturedCount after review: exportable
	// items, items waiting for a person, failed and unprocessed
	// observations, and everything else (an unreadable structure or a
	// status the export cannot use).
	ExportableCount  int64
	FailedCount      int64
	UnprocessedCount int64
	OtherCount       int64
	TotalBytes       int64
	EvidenceBytes    int64
	EvidenceFiles    int
	Unreadable       bool
}

// HistoryRecords lists saved records newest first with the metadata the
// history area shows. A record whose newest structurally valid
// checkpoint cannot be read is still listed, marked Unreadable; this is
// the same "show it rather than hide it" rule as the record listing.
func (s *Service) HistoryRecords() ([]HistoryRecord, error) {
	infos, err := s.store.ListRecords()
	if err != nil {
		return nil, fmt.Errorf("app: history records: %w", err)
	}
	out := make([]HistoryRecord, 0, len(infos))
	for _, info := range infos {
		entry := HistoryRecord{
			ID:            info.ID,
			SavedAt:       info.SavedAt,
			TotalBytes:    info.TotalBytes,
			EvidenceBytes: info.EvidenceBytes,
			EvidenceFiles: info.EvidenceFiles,
		}
		cp, err := s.store.Peek(info.ID)
		if err != nil {
			entry.Unreadable = true
			out = append(out, entry)
			continue
		}
		entry.Client = cp.Session.Client
		entry.CapturedCount = cp.Session.CapturedCount
		entry.Coverage = cp.Session.Coverage.Status
		if cp.Session.Coverage.ExpectedCount != nil {
			entry.CoverageExpected = *cp.Session.Coverage.ExpectedCount
		}
		entry.PendingReview = pendingReviewCount(cp.Items)
		entry.ExportableCount, entry.FailedCount, entry.UnprocessedCount, entry.OtherCount =
			historyItemCounts(cp.Items)
		out = append(out, entry)
	}
	return out, nil
}

// historyItemCounts splits a record's items into the four states the
// history table shows beside the pending count. Pending is separate: an
// item waiting for a person is not exportable yet, and failed, unprocessed
// or structurally unusable observations never are. The parts and
// PendingReview add up to the captured count, so the table can state the
// same accounting the results page does.
func historyItemCounts(items []record.ItemRecord) (exportable, failed, unprocessed, other int64) {
	for _, item := range items {
		switch {
		case item.Status == record.StatusFailed:
			failed++
		case item.Status == record.StatusUnprocessed:
			unprocessed++
		case item.Status != record.StatusProduced:
			other++
		case !hasStructured(item):
			other++
		case itemPendingReview(item):
			// Pending is reported separately; it is not exportable yet.
		default:
			// The remaining produced items are exportable only when the
			// current validation and catalog accept them. The history table
			// must not promise a record the export would drop.
			if _, ok := usableItem(&item); ok {
				exportable++
			} else {
				other++
			}
		}
	}
	return exportable, failed, unprocessed, other
}
