package app

import (
	"fmt"
	"sort"
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
	StartedAt     time.Time
	EndedAt       *time.Time
	Client        record.Client
	CapturedCount int64
	Coverage      record.CoverageStatus
	// CoverageExpected is the scope's expected item count; 0 when the
	// record does not know it. The history table only shows a percentage
	// when it can divide by a real expected count.
	CoverageExpected int64
	PendingReview    int
	// Termination is the session's first termination trigger, when the
	// checkpoint recorded one. The list shows its kind and code so a
	// zero-item failure is not summarized only as item-level "失败 0 件".
	Termination *record.Termination
	// Counts is the four-class partition of the captured items; see
	// ItemClass. It is the source of the exportable and pending numbers,
	// and the legacy split fields below keep the current history table
	// compiling until its UI switches to Counts.
	Counts ItemClassCounts
	// IncompletePositions is the number of inventory positions whose
	// traversal conclusion was incomplete. IncompletePositionsKnown is
	// false until the checkpoint carries per-position conclusions, so a
	// zero count currently means "unknown", not "all six complete".
	IncompletePositions      int
	IncompletePositionsKnown bool
	// Interrupted reports that the newest checkpoint never recorded a
	// termination (or recorded the interrupted kind): the session ended by
	// a crash or a lost process and was recovered as interrupted.
	Interrupted bool
	// ExportableCount, FailedCount, UnprocessedCount and OtherCount are
	// the mutually exclusive parts of CapturedCount after review:
	// exportable items, failed observations, unprocessed observations, and
	// the remaining not-yet-processed observations (queued or running in a
	// checkpoint saved mid-session).
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
		entry.StartedAt = cp.Session.StartedAt
		if cp.Session.EndedAt != nil {
			ended := *cp.Session.EndedAt
			entry.EndedAt = &ended
		}
		entry.Interrupted = interruptedSession(cp.Session.Termination)
		entry.Client = cp.Session.Client
		entry.CapturedCount = cp.Session.CapturedCount
		entry.Coverage = cp.Session.Coverage.Status
		if cp.Session.Termination != nil {
			term := *cp.Session.Termination
			entry.Termination = &term
		}
		if cp.Session.Coverage.ExpectedCount != nil {
			entry.CoverageExpected = *cp.Session.Coverage.ExpectedCount
		}
		entry.Counts, entry.FailedCount, entry.UnprocessedCount = historyItemCounts(cp.Items)
		entry.PendingReview = int(entry.Counts.PendingReview)
		entry.ExportableCount = entry.Counts.Exportable()
		entry.OtherCount = entry.Counts.PendingBackfill - entry.FailedCount - entry.UnprocessedCount
		out = append(out, entry)
	}
	return out, nil
}

// interruptedSession reports whether a checkpoint represents a session
// that never recorded a clean end. An absent termination is exactly what
// recovery marks interrupted; an explicit interrupted termination is the
// same state written down.
func interruptedSession(termination *record.Termination) bool {
	return termination == nil || termination.Kind == record.TerminationInterrupted
}

// historyItemCounts classifies a record's items once for the history
// table. It also returns the failed and unprocessed split the current
// table shows inside the backfill class; the remainder of that class is
// the queued/running "other" column.
func historyItemCounts(items []record.ItemRecord) (counts ItemClassCounts, failed, unprocessed int64) {
	for i := range items {
		item := &items[i]
		switch item.Status {
		case record.StatusFailed:
			failed++
		case record.StatusUnprocessed:
			unprocessed++
		}
		counts.add(ClassifyItem(*item))
	}
	return counts, failed, unprocessed
}

// Duration returns the recorded scan's wall-clock duration. ok is false
// when the checkpoint has no end time or the times are not ordered, which
// an interrupted session and an older record can both produce. Callers
// must then show the duration as unknown rather than as zero.
func (r HistoryRecord) Duration() (time.Duration, bool) {
	if r.StartedAt.IsZero() || r.EndedAt == nil || !r.EndedAt.After(r.StartedAt) {
		return 0, false
	}
	return r.EndedAt.Sub(r.StartedAt), true
}

// ScanDurationEstimate is the expected duration of one full scan, as the
// scan page shows it: the median of the newest eligible scans and how
// many scans back the number. Samples 0 means unknown; Median is then not
// a duration to display.
type ScanDurationEstimate struct {
	Median  time.Duration
	Samples int
}

// Known reports whether at least one eligible scan backs the estimate.
func (e ScanDurationEstimate) Known() bool { return e.Samples > 0 }

// scanDurationSamples is how many recent full scans the estimate uses.
const scanDurationSamples = 3

// EstimateScanDuration returns the median duration of the newest
// eligible full scans. Eligible means a normally completed session whose
// recorded coverage is complete and whose start and end are both known;
// a manual stop, a failure, a partial coverage or a missing end is
// unknown, never a zero-length sample.
func EstimateScanDuration(records []HistoryRecord) ScanDurationEstimate {
	type sample struct {
		started  time.Time
		duration time.Duration
	}
	samples := make([]sample, 0, len(records))
	for _, record := range records {
		duration, ok := record.fullScanDuration()
		if !ok {
			continue
		}
		samples = append(samples, sample{started: record.StartedAt, duration: duration})
	}
	// The history list is ordered by save time, not scan start; choose the
	// newest scans by their own start so a late save of an old record
	// cannot push a newer scan out of the window.
	sort.Slice(samples, func(i, j int) bool {
		if !samples[i].started.Equal(samples[j].started) {
			return samples[i].started.After(samples[j].started)
		}
		return samples[i].duration > samples[j].duration
	})
	if len(samples) > scanDurationSamples {
		samples = samples[:scanDurationSamples]
	}
	if len(samples) == 0 {
		return ScanDurationEstimate{}
	}
	durations := make([]time.Duration, len(samples))
	for i := range samples {
		durations[i] = samples[i].duration
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	middle := len(durations) / 2
	median := durations[middle]
	if len(durations)%2 == 0 {
		median = (durations[middle-1] + durations[middle]) / 2
	}
	return ScanDurationEstimate{Median: median, Samples: len(durations)}
}

// fullScanDuration reports the duration of a scan that ended normally
// with complete coverage and known ends.
func (r HistoryRecord) fullScanDuration() (time.Duration, bool) {
	if r.Unreadable || r.Interrupted {
		return 0, false
	}
	if r.Termination == nil || r.Termination.Kind != record.TerminationCompleted {
		return 0, false
	}
	if r.Coverage != record.CoverageComplete {
		return 0, false
	}
	return r.Duration()
}

// ScanDurationEstimate is the same estimate over the saved records. The
// scan page calls it before a scan; like HistoryRecords it reads every
// listed checkpoint, so callers run it off the UI thread.
func (s *Service) ScanDurationEstimate() (ScanDurationEstimate, error) {
	records, err := s.HistoryRecords()
	if err != nil {
		return ScanDurationEstimate{}, err
	}
	return EstimateScanDuration(records), nil
}
