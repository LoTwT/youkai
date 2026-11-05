package app

import (
	"encoding/json"
	"testing"

	"github.com/LoTwT/youkai/internal/game/zzz"
	"github.com/LoTwT/youkai/internal/record"
)

// warningExportableItem is an auto-accepted S-rarity item whose only
// problem is an unresolved agent identity. v2 keeps that as a warning and
// the export still includes it.
func warningExportableItem(t *testing.T, seq int64) record.ItemRecord {
	t.Helper()
	return exportStructuredItem(t, seq, record.QualityAutoAccepted,
		testDisc(31100, 4, "critRate", "19.2", zzz.UnitPercent),
		zzz.CaptureState{ManagementState: zzz.ManagementNone, EquippedState: zzz.EquippedStateEquipped})
}

// legacyEquippedOnlyItem is the revision-0 v1 shape whose only persisted
// gap was the then-required equipped field. Export reclassifies it as
// auto-accepted with a warning; the display class must follow the export,
// not the stale needsReview quality.
func legacyEquippedOnlyItem(t *testing.T, seq int64) record.ItemRecord {
	t.Helper()
	structured := zzz.ToStructuredWithState(
		testDisc(31100, 4, "critRate", "24", zzz.UnitPercent),
		zzz.RarityS,
		zzz.CaptureState{ManagementState: zzz.ManagementLocked},
		nil,
	)
	structured.Validation = zzz.Validation{
		Status: zzz.ValidationUnavailable,
		Issues: []zzz.Issue{{
			Field:    "equipped",
			Code:     zzz.CodeEquippedStateUnavailable,
			Severity: zzz.SeverityError,
			Message:  "legacy required equipped gap",
		}},
	}
	return exportItemWithStructured(t, seq, record.StatusProduced, record.QualityNeedsReview, structured)
}

func unstructuredItem(seq int64) record.ItemRecord {
	return record.ItemRecord{
		Sequence: seq,
		Status:   record.StatusProduced,
		Quality:  record.QualityAutoAccepted,
		Data:     json.RawMessage(`{"lines":[]}`),
	}
}

// TestClassifyItemFollowsFixedOrder covers every class and the boundaries
// between them. The order is processing outcome, validation, quality,
// warning; a warning that also has a data gap is pending review, and only
// a usable item can be warning or passed.
func TestClassifyItemFollowsFixedOrder(t *testing.T) {
	none := zzz.CaptureState{ManagementState: zzz.ManagementNone, EquippedState: zzz.EquippedStateUnequipped}

	passedAuto := exportStructuredItem(t, 1, record.QualityAutoAccepted,
		testDisc(31100, 4, "critRate", "19.2", zzz.UnitPercent), none)
	passedConfirmed := exportStructuredItem(t, 2, record.QualityHumanConfirmed,
		testDisc(31000, 6, "impact", "18", zzz.UnitPercent), none)
	needsReview := reviewItem(t, 3, reviewData())
	needsReviewWithWarning := exportStructuredItem(t, 4, record.QualityNeedsReview,
		testDisc(31100, 4, "critRate", "19.2", zzz.UnitPercent),
		zzz.CaptureState{ManagementState: zzz.ManagementNone, EquippedState: zzz.EquippedStateEquipped})
	invalidValue := exportStructuredItem(t, 5, record.QualityAutoAccepted,
		testDisc(31300, 2, "attack", "abc", zzz.UnitFlat), none)
	missingManagement := exportStructuredItem(t, 6, record.QualityAutoAccepted,
		testDisc(31600, 5, "attack", "19.2", zzz.UnitPercent),
		zzz.CaptureState{EquippedState: zzz.EquippedStateUnequipped})
	// warningWithGap carries both an unresolved agent (warning) and a
	// missing management state (validation gap): the gap wins.
	warningWithGap := exportStructuredItem(t, 7, record.QualityAutoAccepted,
		testDisc(31600, 5, "attack", "19.2", zzz.UnitPercent),
		zzz.CaptureState{EquippedState: zzz.EquippedStateEquipped})
	notEvaluated := exportStructuredItem(t, 8, record.QualityNotEvaluated,
		testDisc(31500, 4, "health", "2200", zzz.UnitFlat), none)
	backfilledReview := reviewItem(t, 16, reviewData())
	backfilledReview.Status = record.StatusFailed
	backfilledReview.Failure = "识别超时"
	backfilledReview.Revision = 1
	backfilledPassed := exportStructuredItem(t, 17, record.QualityHumanConfirmed,
		testDisc(31100, 4, "critRate", "19.2", zzz.UnitPercent), none)
	backfilledPassed.Status = record.StatusFailed
	backfilledPassed.Failure = "识别超时"
	backfilledPassed.Revision = 1
	backfilledRawOnly := backfilledReview
	backfilledRawOnly.Data = json.RawMessage(`{"lines":[]}`)
	failedAtRevisionZero := reviewItem(t, 18, reviewData())
	failedAtRevisionZero.Status = record.StatusFailed
	failedAtRevisionZero.Failure = "识别超时"
	failedAtRevisionZero.Revision = 0

	withPersistedError := zzz.ToStructuredWithState(
		testDisc(31400, 3, "defense", "46", zzz.UnitPercent), zzz.RarityS, none, nil)
	withPersistedError.Issues = []zzz.Issue{{
		Field: "mainStat", Code: "rawMismatch", Severity: zzz.SeverityError, Message: "原始识别不一致",
	}}
	persistedError := exportItemWithStructured(t, 8, record.StatusProduced,
		record.QualityHumanConfirmed, withPersistedError)

	tests := []struct {
		name string
		item record.ItemRecord
		want ItemClass
	}{
		{"failed observation is pending backfill", record.ItemRecord{
			Sequence: 10, Status: record.StatusFailed, Failure: "识别失败", Quality: record.QualityNotEvaluated,
		}, ItemClassPendingBackfill},
		{"unprocessed observation is pending backfill", record.ItemRecord{
			Sequence: 11, Status: record.StatusUnprocessed, Reason: record.ReasonInterrupted, Quality: record.QualityNotEvaluated,
		}, ItemClassPendingBackfill},
		{"queued observation is pending backfill", record.ItemRecord{
			Sequence: 12, Status: record.StatusQueued, Quality: record.QualityNotEvaluated,
		}, ItemClassPendingBackfill},
		{"auto accepted without warnings is passed", passedAuto, ItemClassPassed},
		{"human confirmed without warnings is passed", passedConfirmed, ItemClassPassed},
		{"needs review is pending review", needsReview, ItemClassPendingReview},
		{"warning without a gap is warning", warningExportableItem(t, 13), ItemClassWarning},
		{"warning with a data gap is pending review", warningWithGap, ItemClassPendingReview},
		{"data gap without a warning is pending review", missingManagement, ItemClassPendingReview},
		{"needs review with a warning is pending review", needsReviewWithWarning, ItemClassPendingReview},
		{"persisted error issue is pending review", persistedError, ItemClassPendingReview},
		{"failed current validation is pending review", invalidValue, ItemClassPendingReview},
		{"unstructured payload is pending review", unstructuredItem(14), ItemClassPendingReview},
		{"unevaluated quality is pending review", notEvaluated, ItemClassPendingReview},
		{"legacy equipped-only record is warning", legacyEquippedOnlyItem(t, 15), ItemClassWarning},
		{"backfilled needs-review item is pending review", backfilledReview, ItemClassPendingReview},
		{"backfilled confirmed item is passed", backfilledPassed, ItemClassPassed},
		{"failed item with revision but no structured payload is still backfill", backfilledRawOnly, ItemClassPendingBackfill},
		{"failed item with structured data at revision zero is still backfill", failedAtRevisionZero, ItemClassPendingBackfill},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyItem(tt.item); got != tt.want {
				t.Fatalf("ClassifyItem(%d) = %q, want %q", tt.item.Sequence, got, tt.want)
			}
		})
	}
}

// TestClassifyItemsPartitionsCaptured checks that the four classes cover
// every captured item exactly once and that "pending" is backfill plus
// review.
func TestClassifyItemsPartitionsCaptured(t *testing.T) {
	items := []record.ItemRecord{
		exportStructuredItem(t, 1, record.QualityAutoAccepted,
			testDisc(31100, 4, "critRate", "19.2", zzz.UnitPercent),
			zzz.CaptureState{ManagementState: zzz.ManagementNone, EquippedState: zzz.EquippedStateUnequipped}),
		warningExportableItem(t, 2),
		reviewItem(t, 3, reviewData()),
		{Sequence: 4, Status: record.StatusFailed, Failure: "识别失败", Quality: record.QualityNotEvaluated},
	}
	counts := ClassifyItems(items)
	if counts.Total() != int64(len(items)) {
		t.Fatalf("total = %d, want %d", counts.Total(), len(items))
	}
	if counts.Passed != 1 || counts.Warning != 1 || counts.PendingReview != 1 || counts.PendingBackfill != 1 {
		t.Fatalf("counts = %+v, want one of each class", counts)
	}
	if counts.Pending() != 2 || counts.Exportable() != 2 {
		t.Fatalf("pending/exportable = %d/%d, want 2/2", counts.Pending(), counts.Exportable())
	}
}

// TestExportCountsUseItemClassification checks the export summary against
// the same per-item decision: warning + passed is exactly what the export
// writes, and the four classes add up to the captured count.
func TestExportCountsUseItemClassification(t *testing.T) {
	rec := exportFixtureRecord(t)
	export, err := BuildUsableExport(rec)
	if err != nil {
		t.Fatalf("BuildUsableExport: %v", err)
	}
	want := ItemClassCounts{PendingBackfill: 2, PendingReview: 6, Warning: 0, Passed: 2}
	if export.Summary.Counts != want {
		t.Fatalf("counts = %+v, want %+v", export.Summary.Counts, want)
	}
	if export.Summary.Counts.Total() != export.Summary.Captured {
		t.Fatalf("class total = %d, captured = %d", export.Summary.Counts.Total(), export.Summary.Captured)
	}
	if export.Summary.Counts.Exportable() != int64(export.Summary.Included) {
		t.Fatalf("warning+passed = %d, included = %d",
			export.Summary.Counts.Exportable(), export.Summary.Included)
	}
	if export.Summary.Counts.Pending() != int64(export.Summary.Excluded) {
		t.Fatalf("backfill+review = %d, excluded = %d",
			export.Summary.Counts.Pending(), export.Summary.Excluded)
	}
	for i := range rec.Checkpoint.Items {
		item := &rec.Checkpoint.Items[i]
		class := ClassifyItem(*item)
		_, included := usableItem(item)
		classIncluded := class == ItemClassWarning || class == ItemClassPassed
		if included != classIncluded {
			t.Fatalf("item %d class %q, export included %v", item.Sequence, class, included)
		}
	}
}

// TestExportWarningCountsStayIncluded checks the warning class through
// the legacy reclassification and a current warning item: both warnings
// are exported, and the class count names them.
func TestExportWarningCountsStayIncluded(t *testing.T) {
	export, err := BuildUsableExport(exportTestRecovery(
		warningExportableItem(t, 1),
		legacyEquippedOnlyItem(t, 2),
	))
	if err != nil {
		t.Fatalf("BuildUsableExport: %v", err)
	}
	want := ItemClassCounts{Warning: 2}
	if export.Summary.Counts != want {
		t.Fatalf("counts = %+v, want %+v", export.Summary.Counts, want)
	}
	if export.Summary.Included != 2 || export.Summary.Excluded != 0 {
		t.Fatalf("summary = %+v, want 2 included / 0 excluded", export.Summary)
	}
}

// TestBackfilledItemExportsWithoutChangingOutcome pins the export rule: a
// failed item becomes usable only through an interpreted, confirmed
// revision, and the original status stays failed.
func TestBackfilledItemExportsWithoutChangingOutcome(t *testing.T) {
	backfilled := exportStructuredItem(t, 20, record.QualityHumanConfirmed,
		testDisc(31100, 4, "critRate", "19.2", zzz.UnitPercent),
		zzz.CaptureState{ManagementState: zzz.ManagementNone, EquippedState: zzz.EquippedStateUnequipped})
	backfilled.Status = record.StatusFailed
	backfilled.Failure = "识别超时"
	backfilled.Revision = 1
	exported, usable := usableItem(&backfilled)
	if !usable || exported.Sequence != 20 || exported.Revision != 1 {
		t.Fatalf("backfilled export = %+v usable %v", exported, usable)
	}
	if backfilled.Status != record.StatusFailed || backfilled.Failure != "识别超时" {
		t.Fatalf("export rewrote the outcome: %+v", backfilled)
	}
}
