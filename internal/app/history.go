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
	PendingReview int
	TotalBytes    int64
	EvidenceBytes int64
	EvidenceFiles int
	Unreadable    bool
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
		entry.PendingReview = pendingReviewCount(cp.Items)
		out = append(out, entry)
	}
	return out, nil
}
