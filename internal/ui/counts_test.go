package ui

import (
	"testing"

	"github.com/LoTwT/youkai/internal/game/zzz"
	"github.com/LoTwT/youkai/internal/record"
)

func TestCountRowsIsMutuallyExclusive(t *testing.T) {
	rows := []Row{
		{Status: record.StatusFailed},
		{Status: record.StatusUnprocessed},
		{Status: record.StatusProduced, Quality: record.QualityNeedsReview, Structured: &zzz.StructuredDisc{}},
		{Status: record.StatusProduced, Quality: record.QualityAutoAccepted, Structured: &zzz.StructuredDisc{}},
		{Status: record.StatusProduced, Quality: record.QualityHumanConfirmed, Structured: &zzz.StructuredDisc{}},
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
	rows := []Row{{Status: record.StatusProduced, Quality: record.QualityAutoAccepted, Structured: &zzz.StructuredDisc{}}}
	got := countRows(rows, 2)
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
