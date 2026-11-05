package app

import (
	"testing"

	"github.com/LoTwT/youkai/internal/record"
)

// TestSaveItemRevisionBackfillsFailedItem checks the backfill contract in
// the application layer. A failed item accepts the manual interpretation
// and becomes usable after confirmation, while its original Status,
// Failure and Reason stay untouched across both saves.
func TestSaveItemRevisionBackfillsFailedItem(t *testing.T) {
	failed := record.ItemRecord{
		Sequence: 1,
		Status:   record.StatusFailed,
		Failure:  "识别超时",
		Quality:  record.QualityNotEvaluated,
	}
	svc, id := reviewService(t, nil, failed)

	first, err := svc.SaveItemRevision(id, 1, 0, ItemRevisionEdit{Data: reviewData()})
	if err != nil {
		t.Fatalf("save failed item: %v", err)
	}
	if first.Item.Status != record.StatusFailed || first.Item.Failure != "识别超时" ||
		first.Item.Quality != record.QualityNeedsReview || first.Item.Revision != 1 {
		t.Fatalf("first backfill = status %q failure %q quality %q revision %d",
			first.Item.Status, first.Item.Failure, first.Item.Quality, first.Item.Revision)
	}
	if !ItemInterpreted(first.Item) || ClassifyItem(first.Item) != ItemClassPendingReview {
		t.Fatalf("first backfill interpreted/class = %v/%q, want true/pending review",
			ItemInterpreted(first.Item), ClassifyItem(first.Item))
	}
	if _, usable := usableItem(&first.Item); usable {
		t.Fatal("an unconfirmed backfill was exportable")
	}

	second, err := svc.SaveItemRevision(id, 1, 1, ItemRevisionEdit{Data: reviewData(), Confirmed: true})
	if err != nil {
		t.Fatalf("confirm failed item: %v", err)
	}
	if second.Item.Status != record.StatusFailed || second.Item.Failure != "识别超时" ||
		second.Item.Quality != record.QualityHumanConfirmed || second.Item.Revision != 2 {
		t.Fatalf("confirmed backfill = status %q failure %q quality %q revision %d",
			second.Item.Status, second.Item.Failure, second.Item.Quality, second.Item.Revision)
	}
	if ClassifyItem(second.Item) != ItemClassPassed {
		t.Fatalf("confirmed backfill class = %q, want passed", ClassifyItem(second.Item))
	}
	exported, usable := usableItem(&second.Item)
	if !usable || exported.Data.SetID == nil {
		t.Fatalf("confirmed backfill is not exportable: usable %v item %+v", usable, exported)
	}
}

// TestSaveItemRevisionBackfillsUnprocessedItem covers the other original
// outcome. Its reason and status survive the revision too.
func TestSaveItemRevisionBackfillsUnprocessedItem(t *testing.T) {
	unprocessed := record.ItemRecord{
		Sequence: 1,
		Status:   record.StatusUnprocessed,
		Reason:   record.ReasonInterrupted,
		Quality:  record.QualityNotEvaluated,
	}
	svc, id := reviewService(t, nil, unprocessed)
	result, err := svc.SaveItemRevision(id, 1, 0, ItemRevisionEdit{Data: reviewData(), Confirmed: true})
	if err != nil {
		t.Fatalf("backfill unprocessed item: %v", err)
	}
	if result.Item.Status != record.StatusUnprocessed || result.Item.Reason != record.ReasonInterrupted ||
		result.Item.Quality != record.QualityHumanConfirmed {
		t.Fatalf("backfilled item = status %q reason %q quality %q",
			result.Item.Status, result.Item.Reason, result.Item.Quality)
	}
	if !ItemInterpreted(result.Item) || ClassifyItem(result.Item) != ItemClassPassed {
		t.Fatalf("interpreted/class = %v/%q, want true/passed",
			ItemInterpreted(result.Item), ClassifyItem(result.Item))
	}
}
