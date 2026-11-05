package ui

import (
	"testing"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/game/zzz"
	"github.com/LoTwT/youkai/internal/record"
)

func TestCountRowsIsMutuallyExclusive(t *testing.T) {
	rows := []Row{
		{Status: record.StatusFailed, ItemClass: app.ItemClassPendingBackfill},
		{Status: record.StatusUnprocessed, ItemClass: app.ItemClassPendingBackfill},
		{Status: record.StatusProduced, Quality: record.QualityNeedsReview, ItemClass: app.ItemClassPendingReview, Structured: &zzz.StructuredDisc{}},
		{Status: record.StatusProduced, Quality: record.QualityAutoAccepted, ItemClass: app.ItemClassPassed, Structured: &zzz.StructuredDisc{}},
		{Status: record.StatusProduced, Quality: record.QualityHumanConfirmed, ItemClass: app.ItemClassPassed, Structured: &zzz.StructuredDisc{}},
	}
	got := countRows(rows, 2)
	want := recordCounts{
		Captured:    5,
		Exportable:  2,
		Pending:     1,
		Failed:      1,
		Unprocessed: 1,
		ExportKnown: true,
	}
	if got != want {
		t.Fatalf("counts = %+v, want %+v", got, want)
	}
	if got.Captured != got.Exportable+got.Pending+got.Failed+got.Unprocessed+got.OtherExcluded {
		t.Fatalf("counts do not add up: %+v", got)
	}
}

func TestCountRowsClampsOtherExcluded(t *testing.T) {
	rows := []Row{{Status: record.StatusProduced, Quality: record.QualityAutoAccepted, ItemClass: app.ItemClassPassed, Structured: &zzz.StructuredDisc{}}}
	got := countRows(rows, 1)
	if got.OtherExcluded != 0 {
		t.Fatalf("OtherExcluded = %d, want 0", got.OtherExcluded)
	}
}

func TestReviewSummaryText(t *testing.T) {
	cases := []struct {
		name  string
		count recordCounts
		want  string
	}{
		{"pending only", recordCounts{Pending: 2}, "待校对 2 件"},
		{"empty known export", recordCounts{Captured: 0, ExportKnown: true}, "没有可导出的条目"},
		{"empty unknown export", recordCounts{Captured: 0}, "没有条目"},
		{"pending with blocked", recordCounts{Pending: 1, Failed: 1, Unprocessed: 1}, "待校对 1 件 · 另有 2 件无法导出"},
		{"zero pending with blocked", recordCounts{Failed: 2, OtherExcluded: 1}, "待校对 0 · 另有 3 件无法导出"},
		{"all exportable", recordCounts{Captured: 2, Exportable: 2, ExportKnown: true}, "待校对 0 · 全部可导出"},
		{"export unknown", recordCounts{Captured: 2}, "待校对 0"},
	}
	for _, tc := range cases {
		if got := reviewSummaryText(tc.count); got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestCountRowsUsesItemClassForBackfilledItems pins the UI count to the
// four-class decision: a failed item after a manual revision is pending
// review or exportable, never "failed" in the status line.
func TestCountRowsUsesItemClassForBackfilledItems(t *testing.T) {
	rows := []Row{
		{Status: record.StatusFailed, Failure: "识别超时", ItemClass: app.ItemClassPendingReview},
		{Status: record.StatusFailed, Failure: "识别超时", ItemClass: app.ItemClassPassed},
		{Status: record.StatusUnprocessed, Reason: record.ReasonInterrupted, ItemClass: app.ItemClassWarning},
	}
	got := countRows(rows, 2)
	if got.Failed != 0 || got.Unprocessed != 0 || got.Pending != 1 || got.Exportable != 2 {
		t.Fatalf("backfilled counts = %+v, want no failed/unprocessed and pending/exportable 1/2", got)
	}
	if got.Captured != got.Exportable+got.Pending+got.Failed+got.Unprocessed+got.OtherExcluded {
		t.Fatalf("counts do not add up: %+v", got)
	}
}

// TestCountRowsQueuedRowCountsOnce checks a queued/running pending-backfill
// row is not counted both by the row loop and by the class remainder.
func TestCountRowsQueuedRowCountsOnce(t *testing.T) {
	rows := []Row{{Seq: 1, Status: record.StatusQueued, ItemClass: app.ItemClassPendingBackfill}}
	got := countRows(rows, 0)
	if got.OtherExcluded != 1 {
		t.Fatalf("one queued row counts as %d other-excluded: %+v", got.OtherExcluded, got)
	}
}
