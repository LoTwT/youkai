package app

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/game/zzz"
	"github.com/LoTwT/youkai/internal/record"
)

// exportableHistoryItem builds one settled, valid S-rarity item the export
// accepts, so the history counts can be checked against a real usable row.
func exportableHistoryItem(t *testing.T, seq int64) record.ItemRecord {
	t.Helper()
	data := reviewData()
	rarity := "S"
	data.Rarity = &rarity
	item := reviewItem(t, seq, data)
	item.Quality = record.QualityAutoAccepted
	structured := zzz.StructuredDisc{
		Data:       data,
		Issues:     []zzz.Issue{},
		Validation: zzz.Validation{Status: zzz.ValidationPassed, Issues: []zzz.Issue{}},
	}
	raw, err := json.Marshal(structured)
	if err != nil {
		t.Fatalf("marshal structured: %v", err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(item.Data, &doc); err != nil {
		t.Fatalf("unmarshal item payload: %v", err)
	}
	doc["structured"] = raw
	item.Data, err = json.Marshal(doc)
	if err != nil {
		t.Fatalf("remarshal item payload: %v", err)
	}
	return item
}

// TestHistoryItemCountsSplitsCapturedItems checks the four export states
// behind the history table's columns: exportable (warning or passed),
// failed, unprocessed and the queued remainder of pending backfill;
// pending review is counted separately and is not exportable yet.
func TestHistoryItemCountsSplitsCapturedItems(t *testing.T) {
	exportable := exportableHistoryItem(t, 1)
	pending := reviewItem(t, 2, reviewData())
	other := record.ItemRecord{Sequence: 3, Status: record.StatusQueued, Quality: record.QualityNotEvaluated}
	failed := record.ItemRecord{Sequence: 4, Status: record.StatusFailed, Failure: "识别超时", Quality: record.QualityNotEvaluated}
	unprocessed := record.ItemRecord{Sequence: 5, Status: record.StatusUnprocessed, Reason: record.ReasonCancelled, Quality: record.QualityNotEvaluated}

	items := []record.ItemRecord{exportable, pending, other, failed, unprocessed}
	counts, failedCount, unprocessedCount := historyItemCounts(items)
	if exported := counts.Exportable(); exported != 1 || failedCount != 1 || unprocessedCount != 1 {
		t.Fatalf("counts = exportable %d, failed %d, unprocessed %d; want 1/1/1",
			exported, failedCount, unprocessedCount)
	}
	if otherCount := counts.PendingBackfill - failedCount - unprocessedCount; otherCount != 1 {
		t.Fatalf("other = %d, want 1", otherCount)
	}
	if counts.PendingReview != 1 {
		t.Fatalf("pending review = %d, want 1", counts.PendingReview)
	}
	if counts.Total() != int64(len(items)) {
		t.Fatalf("class total = %d, want %d", counts.Total(), len(items))
	}
	if counts.Warning != 0 || counts.Passed != 1 {
		t.Fatalf("warning/passed = %d/%d, want 0/1", counts.Warning, counts.Passed)
	}
}

// TestServiceHistoryRecordsIncludesExportBreakdown checks that the history
// metadata carries the same counts and coverage denominator the table
// renders, without loading the record.
func TestServiceHistoryRecordsIncludesExportBreakdown(t *testing.T) {
	store, paths := cleanupStore(t)
	const id = "history-breakdown"
	if err := store.CreateRecord(id); err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}
	exportable := exportableHistoryItem(t, 1)
	pending := reviewItem(t, 2, reviewData())
	failed := record.ItemRecord{Sequence: 3, Status: record.StatusFailed, Failure: "识别超时", Quality: record.QualityNotEvaluated}
	unprocessed := record.ItemRecord{Sequence: 4, Status: record.StatusUnprocessed, Reason: record.ReasonCancelled, Quality: record.QualityNotEvaluated}
	other := record.ItemRecord{Sequence: 5, Status: record.StatusQueued, Quality: record.QualityNotEvaluated}
	expected := int64(6)
	started := time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)
	termination := &record.Termination{Kind: record.TerminationFailed, Code: "navigationMismatch", Phase: record.PhaseCapturing}
	cp := record.Checkpoint{
		Session: record.SessionShell{
			SessionID:     id,
			Game:          "zzz",
			Entity:        "driveDisc",
			Client:        record.ClientWindowsLocal,
			Locale:        "zh-CN",
			Scope:         json.RawMessage(`{"rarity":"S"}`),
			StartedAt:     started,
			Termination:   termination,
			Coverage:      record.Coverage{Status: record.CoveragePartial, ExpectedCount: &expected},
			CapturedCount: 5,
		},
		Items: []record.ItemRecord{exportable, pending, failed, unprocessed, other},
	}
	if _, err := store.SaveCheckpoint(id, cp); err != nil {
		t.Fatalf("SaveCheckpoint: %v", err)
	}
	svc := newServiceWithStore(t, paths, store, nil, func() (Recognizer, error) { return newFakeRecognizer(), nil })

	records, err := svc.HistoryRecords()
	if err != nil {
		t.Fatalf("HistoryRecords: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %+v, want one", records)
	}
	rec := records[0]
	if rec.CapturedCount != 5 || rec.CoverageExpected != 6 || rec.PendingReview != 1 {
		t.Fatalf("captured/expected/pending = %d/%d/%d, want 5/6/1",
			rec.CapturedCount, rec.CoverageExpected, rec.PendingReview)
	}
	if rec.ExportableCount != 1 || rec.FailedCount != 1 || rec.UnprocessedCount != 1 || rec.OtherCount != 1 {
		t.Fatalf("counts = exportable %d, failed %d, unprocessed %d, other %d; want 1/1/1/1",
			rec.ExportableCount, rec.FailedCount, rec.UnprocessedCount, rec.OtherCount)
	}
	if rec.ExportableCount+int64(rec.PendingReview)+rec.FailedCount+rec.UnprocessedCount+rec.OtherCount != rec.CapturedCount {
		t.Fatal("the history breakdown does not add up to the captured count")
	}
	wantCounts := ItemClassCounts{PendingBackfill: 3, PendingReview: 1, Passed: 1}
	if rec.Counts != wantCounts {
		t.Fatalf("four-class counts = %+v, want %+v", rec.Counts, wantCounts)
	}
	if rec.StartedAt != started {
		t.Fatalf("started at = %s, want %s", rec.StartedAt, started)
	}
	if rec.EndedAt != nil {
		t.Fatalf("ended at = %s, want unknown", rec.EndedAt)
	}
	if _, ok := rec.Duration(); ok {
		t.Fatal("duration is known without an end time")
	}
	if rec.Interrupted {
		t.Fatal("a failed session with a termination is marked interrupted")
	}
	if rec.IncompletePositions != 0 || rec.IncompletePositionsKnown {
		t.Fatalf("incomplete positions = %d known %v, want 0/unknown before PR-03",
			rec.IncompletePositions, rec.IncompletePositionsKnown)
	}
	if rec.Termination == nil || *rec.Termination != *termination {
		t.Fatalf("termination = %+v, want %+v", rec.Termination, termination)
	}
}

// TestHistoryRecordsSummaryAndInterruption checks the summary metadata on
// a completed record and the interruption marker on a record whose
// checkpoint never recorded a termination.
func TestHistoryRecordsSummaryAndInterruption(t *testing.T) {
	store, paths := cleanupStore(t)
	const completedID = "history-completed"
	const interruptedID = "history-interrupted"
	started := time.Date(2026, 11, 5, 8, 0, 0, 0, time.UTC)
	ended := started.Add(14 * time.Minute)
	for _, id := range []string{completedID, interruptedID} {
		if err := store.CreateRecord(id); err != nil {
			t.Fatalf("CreateRecord %s: %v", id, err)
		}
	}
	completed := record.Checkpoint{
		Session: record.SessionShell{
			SessionID: completedID, Game: "zzz", Entity: "driveDisc",
			Client: record.ClientWindowsLocal, Locale: "zh-CN",
			Scope:     json.RawMessage(`{"rarity":"S"}`),
			StartedAt: started, EndedAt: &ended,
			Termination:   &record.Termination{Kind: record.TerminationCompleted, Code: "verifiedEnd", Phase: record.PhaseCapturing},
			Coverage:      record.Coverage{Status: record.CoverageComplete},
			CapturedCount: 1,
		},
		Items: []record.ItemRecord{warningExportableItem(t, 1)},
	}
	if _, err := store.SaveCheckpoint(completedID, completed); err != nil {
		t.Fatalf("SaveCheckpoint completed: %v", err)
	}
	interrupted := record.Checkpoint{
		Session: record.SessionShell{
			SessionID: interruptedID, Game: "zzz", Entity: "driveDisc",
			Client: record.ClientWindowsLocal, Locale: "zh-CN",
			Scope:         json.RawMessage(`{"rarity":"S"}`),
			StartedAt:     started.Add(time.Hour),
			Coverage:      record.Coverage{Status: record.CoverageUnknown},
			CapturedCount: 1,
		},
		Items: []record.ItemRecord{{Sequence: 1, Status: record.StatusFailed, Failure: "识别失败", Quality: record.QualityNotEvaluated}},
	}
	if _, err := store.SaveCheckpoint(interruptedID, interrupted); err != nil {
		t.Fatalf("SaveCheckpoint interrupted: %v", err)
	}
	svc := newServiceWithStore(t, paths, store, nil, func() (Recognizer, error) { return newFakeRecognizer(), nil })

	records, err := svc.HistoryRecords()
	if err != nil {
		t.Fatalf("HistoryRecords: %v", err)
	}
	byID := make(map[string]HistoryRecord, len(records))
	for _, rec := range records {
		byID[rec.ID] = rec
	}
	done := byID[completedID]
	if !done.StartedAt.Equal(started) || done.EndedAt == nil || !done.EndedAt.Equal(ended) {
		t.Fatalf("completed times = %s..%v, want %s..%s", done.StartedAt, done.EndedAt, started, ended)
	}
	if duration, ok := done.Duration(); !ok || duration != 14*time.Minute {
		t.Fatalf("duration = %s/%v, want 14m/true", duration, ok)
	}
	if done.Interrupted {
		t.Fatal("completed record is marked interrupted")
	}
	if done.Counts != (ItemClassCounts{Warning: 1}) {
		t.Fatalf("completed counts = %+v, want one warning", done.Counts)
	}
	broken := byID[interruptedID]
	if !broken.Interrupted {
		t.Fatal("record without a termination is not marked interrupted")
	}
	if broken.EndedAt != nil {
		t.Fatalf("interrupted ended at = %s, want unknown", broken.EndedAt)
	}
	if _, ok := broken.Duration(); ok {
		t.Fatal("interrupted record reports a duration")
	}
	if broken.Counts != (ItemClassCounts{PendingBackfill: 1}) {
		t.Fatalf("interrupted counts = %+v, want one pending backfill", broken.Counts)
	}

	estimate, err := svc.ScanDurationEstimate()
	if err != nil {
		t.Fatalf("ScanDurationEstimate: %v", err)
	}
	if !estimate.Known() || estimate.Samples != 1 || estimate.Median != 14*time.Minute {
		t.Fatalf("estimate = %+v, want one 14m sample", estimate)
	}
}

// completedHistory is one normally completed full scan for the estimator
// table.
func completedHistory(t *testing.T, start time.Time, duration time.Duration) HistoryRecord {
	t.Helper()
	ended := start.Add(duration)
	return HistoryRecord{
		ID:        start.Format(time.RFC3339),
		StartedAt: start,
		EndedAt:   &ended,
		Coverage:  record.CoverageComplete,
		Termination: &record.Termination{
			Kind: record.TerminationCompleted, Code: "verifiedEnd", Phase: record.PhaseCapturing,
		},
	}
}

// TestEstimateScanDuration covers the median and sample count at 0, 1, 2,
// 3 and 5 histories, and checks that a manual stop, a failure, partial
// coverage or a missing end never becomes a zero-length sample.
func TestEstimateScanDuration(t *testing.T) {
	base := time.Date(2026, 11, 5, 8, 0, 0, 0, time.UTC)
	at := func(hours int) time.Time { return base.Add(time.Duration(hours) * time.Hour) }

	t.Run("no history", func(t *testing.T) {
		estimate := EstimateScanDuration(nil)
		if estimate.Known() || estimate.Samples != 0 || estimate.Median != 0 {
			t.Fatalf("estimate = %+v, want unknown", estimate)
		}
	})
	t.Run("one history", func(t *testing.T) {
		estimate := EstimateScanDuration([]HistoryRecord{completedHistory(t, at(0), 10*time.Minute)})
		if estimate.Samples != 1 || estimate.Median != 10*time.Minute {
			t.Fatalf("estimate = %+v, want one 10m sample", estimate)
		}
	})
	t.Run("two histories use the mean", func(t *testing.T) {
		estimate := EstimateScanDuration([]HistoryRecord{
			completedHistory(t, at(0), 10*time.Minute),
			completedHistory(t, at(1), 20*time.Minute),
		})
		if estimate.Samples != 2 || estimate.Median != 15*time.Minute {
			t.Fatalf("estimate = %+v, want two samples with a 15m median", estimate)
		}
	})
	t.Run("three histories use the middle", func(t *testing.T) {
		estimate := EstimateScanDuration([]HistoryRecord{
			completedHistory(t, at(0), 30*time.Minute),
			completedHistory(t, at(1), 10*time.Minute),
			completedHistory(t, at(2), 20*time.Minute),
		})
		if estimate.Samples != 3 || estimate.Median != 20*time.Minute {
			t.Fatalf("estimate = %+v, want three samples with a 20m median", estimate)
		}
	})
	t.Run("five histories use the newest three", func(t *testing.T) {
		estimate := EstimateScanDuration([]HistoryRecord{
			completedHistory(t, at(4), 14*time.Minute), // newest
			completedHistory(t, at(0), 100*time.Minute),
			completedHistory(t, at(3), 18*time.Minute),
			completedHistory(t, at(1), 50*time.Minute),
			completedHistory(t, at(2), 12*time.Minute),
		})
		if estimate.Samples != 3 || estimate.Median != 14*time.Minute {
			t.Fatalf("estimate = %+v, want three newest samples with a 14m median", estimate)
		}
	})
	t.Run("ineligible histories are unknown", func(t *testing.T) {
		cancelled := completedHistory(t, at(0), 10*time.Minute)
		cancelled.Termination = &record.Termination{Kind: record.TerminationCancelled, Code: "hotkey", Phase: record.PhaseCapturing}
		failed := completedHistory(t, at(1), 10*time.Minute)
		failed.Termination = &record.Termination{Kind: record.TerminationFailed, Code: "targetLost", Phase: record.PhaseCapturing}
		partial := completedHistory(t, at(2), 10*time.Minute)
		partial.Coverage = record.CoveragePartial
		unknownCoverage := completedHistory(t, at(3), 10*time.Minute)
		unknownCoverage.Coverage = record.CoverageUnknown
		missingEnd := completedHistory(t, at(4), 10*time.Minute)
		missingEnd.EndedAt = nil
		zeroLength := completedHistory(t, at(5), 0)
		unreadable := completedHistory(t, at(6), 10*time.Minute)
		unreadable.Unreadable = true
		interrupted := completedHistory(t, at(7), 10*time.Minute)
		interrupted.Interrupted = true

		estimate := EstimateScanDuration([]HistoryRecord{
			cancelled, failed, partial, unknownCoverage, missingEnd, zeroLength, unreadable, interrupted,
		})
		if estimate.Known() || estimate.Samples != 0 {
			t.Fatalf("estimate = %+v, want unknown from ineligible histories", estimate)
		}
	})
	t.Run("ineligible histories do not change an eligible median", func(t *testing.T) {
		cancelled := completedHistory(t, at(3), 5*time.Minute)
		cancelled.Termination = &record.Termination{Kind: record.TerminationCancelled, Code: "hotkey", Phase: record.PhaseCapturing}
		missingEnd := completedHistory(t, at(4), 5*time.Minute)
		missingEnd.EndedAt = nil
		estimate := EstimateScanDuration([]HistoryRecord{
			cancelled,
			completedHistory(t, at(0), 10*time.Minute),
			missingEnd,
			completedHistory(t, at(1), 20*time.Minute),
			completedHistory(t, at(2), 30*time.Minute),
		})
		if estimate.Samples != 3 || estimate.Median != 20*time.Minute {
			t.Fatalf("estimate = %+v, want three samples with a 20m median", estimate)
		}
	})
}
