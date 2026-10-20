package ui

import (
	"testing"

	"github.com/LoTwT/youkai/internal/game/zzz"
	"github.com/LoTwT/youkai/internal/record"
)

// TestOptionalEquippedOnlyRowIsSettled pins the v2 presentation of a v1
// revision-0 record whose only gap was the old required equipped field.
func TestOptionalEquippedOnlyRowIsSettled(t *testing.T) {
	row := Row{
		Status:   record.StatusProduced,
		Quality:  record.QualityNeedsReview,
		Revision: 0,
		Structured: &zzz.StructuredDisc{
			Validation: zzz.Validation{
				Status: zzz.ValidationUnavailable,
				Issues: []zzz.Issue{{
					Field:    "equipped",
					Code:     zzz.CodeEquippedStateUnavailable,
					Severity: zzz.SeverityError,
					Message:  "legacy required equipped gap",
				}},
			},
		},
	}
	if row.PendingReview() {
		t.Fatal("legacy optional-equipped row still pending")
	}
	if got := row.QualityText(); got != "自动接受（装备字段警告）" {
		t.Fatalf("quality = %q", got)
	}
	if got := row.ValidationText(); got != "通过（装备字段警告）" {
		t.Fatalf("validation = %q", got)
	}
	if got := row.IssueSummaryText(); got != "1 个警告" {
		t.Fatalf("issues = %q, want one warning", got)
	}
}
