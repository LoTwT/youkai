package app

import (
	"testing"

	"github.com/LoTwT/youkai/internal/record"
)

// TestSaveItemRevisionCompletesFailedItem checks the backfill contract in
// the application layer: a confirmed revision of a failed item becomes a
// produced, human-confirmed item the export includes.
func TestSaveItemRevisionCompletesFailedItem(t *testing.T) {
	failed := record.ItemRecord{
		Sequence: 1,
		Status:   record.StatusFailed,
		Failure:  "识别超时",
		Quality:  record.QualityNotEvaluated,
	}
	svc, id := reviewService(t, nil, failed)

	result, err := svc.SaveItemRevision(id, 1, 0, ItemRevisionEdit{Data: reviewData(), Confirmed: true})
	if err != nil {
		t.Fatalf("complete failed item: %v", err)
	}
	if result.Item.Status != record.StatusProduced || result.Item.Failure != "" ||
		result.Item.Quality != record.QualityHumanConfirmed {
		t.Fatalf("completed item = status %q failure %q quality %q",
			result.Item.Status, result.Item.Failure, result.Item.Quality)
	}
	if class := ClassifyItem(result.Item); class != ItemClassPassed {
		t.Fatalf("completed item class = %q, want passed", class)
	}
	exported, usable := usableItem(&result.Item)
	if !usable || exported.Data.SetID == nil {
		t.Fatalf("completed item is not exportable: usable %v item %+v", usable, exported)
	}
}
