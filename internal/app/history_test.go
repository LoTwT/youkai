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
// behind the history table's columns: exportable, failed, unprocessed and
// everything else; pending review is counted separately and is not
// exportable yet.
func TestHistoryItemCountsSplitsCapturedItems(t *testing.T) {
	exportable := exportableHistoryItem(t, 1)
	pending := reviewItem(t, 2, reviewData())
	other := record.ItemRecord{Sequence: 3, Status: record.StatusQueued, Quality: record.QualityNotEvaluated}
	failed := record.ItemRecord{Sequence: 4, Status: record.StatusFailed, Failure: "识别超时", Quality: record.QualityNotEvaluated}
	unprocessed := record.ItemRecord{Sequence: 5, Status: record.StatusUnprocessed, Reason: record.ReasonCancelled, Quality: record.QualityNotEvaluated}

	items := []record.ItemRecord{exportable, pending, other, failed, unprocessed}
	exported, failedCount, unprocessedCount, otherCount := historyItemCounts(items)
	if exported != 1 || failedCount != 1 || unprocessedCount != 1 || otherCount != 1 {
		t.Fatalf("counts = exportable %d, failed %d, unprocessed %d, other %d; want 1/1/1/1",
			exported, failedCount, unprocessedCount, otherCount)
	}
	if pendingCount := pendingReviewCount(items); pendingCount != 1 {
		t.Fatalf("pending review = %d, want 1", pendingCount)
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
	termination := &record.Termination{Kind: record.TerminationFailed, Code: "navigationMismatch", Phase: record.PhaseCapturing}
	cp := record.Checkpoint{
		Session: record.SessionShell{
			SessionID:     id,
			Game:          "zzz",
			Entity:        "driveDisc",
			Client:        record.ClientWindowsLocal,
			Locale:        "zh-CN",
			Scope:         json.RawMessage(`{"rarity":"S"}`),
			StartedAt:     time.Now().UTC(),
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
	if rec.Termination == nil || *rec.Termination != *termination {
		t.Fatalf("termination = %+v, want %+v", rec.Termination, termination)
	}
}
