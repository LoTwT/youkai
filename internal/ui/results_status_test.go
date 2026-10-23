package ui

import (
	"fmt"
	"testing"

	"github.com/LoTwT/youkai/internal/game/zzz"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/egoist/mygo/ui"
)

func pendingBadgeRow() *Row {
	return &Row{
		Status:     record.StatusProduced,
		Quality:    record.QualityNeedsReview,
		Structured: &zzz.StructuredDisc{},
	}
}

func autoBadgeRow() *Row {
	return &Row{
		Status:     record.StatusProduced,
		Quality:    record.QualityAutoAccepted,
		Structured: &zzz.StructuredDisc{},
	}
}

func confirmedBadgeRow() *Row {
	return &Row{
		Status:     record.StatusProduced,
		Quality:    record.QualityHumanConfirmed,
		Structured: &zzz.StructuredDisc{},
	}
}

func optionalEquippedBadgeRow() *Row {
	return &Row{
		Status:  record.StatusProduced,
		Quality: record.QualityNeedsReview,
		Structured: &zzz.StructuredDisc{
			Validation: zzz.Validation{
				Status: zzz.ValidationUnavailable,
				Issues: []zzz.Issue{{Code: zzz.CodeEquippedStateUnavailable}},
			},
		},
	}
}

func neutralBadgeRow() *Row {
	return &Row{Status: record.StatusProduced, Quality: record.QualityNotEvaluated}
}

func TestResultBadgeKindForStatuses(t *testing.T) {
	cases := []struct {
		name              string
		row               *Row
		recentlyConfirmed bool
		want              resultBadgeKind
	}{
		{"pending review", pendingBadgeRow(), false, resultBadgePending},
		{"recently confirmed", pendingBadgeRow(), true, resultBadgeConfirmed},
		{"failed", &Row{Status: record.StatusFailed, Failure: "识别超时"}, false, resultBadgeFailed},
		{"unprocessed", &Row{Status: record.StatusUnprocessed, Reason: record.ReasonInterrupted}, false, resultBadgeUnprocessed},
		{"auto accepted", autoBadgeRow(), false, resultBadgeAuto},
		{"optional equipped only", optionalEquippedBadgeRow(), false, resultBadgeAuto},
		{"human confirmed", confirmedBadgeRow(), false, resultBadgeConfirmed},
		{"other produced quality", neutralBadgeRow(), false, resultBadgeNeutral},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resultBadgeKindFor(tc.row, tc.recentlyConfirmed); got != tc.want {
				t.Fatalf("badge kind = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestResultBadgeStyles(t *testing.T) {
	tok := tokensFor(true)
	theme := &ui.Theme{AccentText: ui.Hex("#FFFFFF")}
	cases := []struct {
		name       string
		kind       resultBadgeKind
		glyph      string
		background ui.Color
		border     ui.Color
		text       ui.Color
	}{
		{"pending", resultBadgePending, "◐", tok.WarningSoft, tok.WarningBorder, tok.WarningFG},
		{"confirmed", resultBadgeConfirmed, "✓", tok.SuccessSoft, tok.SuccessBorder, tok.SuccessFG},
		{"auto", resultBadgeAuto, "○", ui.Transparent, tok.BorderDefault, tok.TextSecondary},
		{"failed", resultBadgeFailed, "✕", tok.DangerSoft, tok.DangerBorder, tok.DangerFG},
		{"unprocessed", resultBadgeUnprocessed, "⊘", ui.Transparent, tok.BorderDefault, tok.TextMuted},
		{"neutral", resultBadgeNeutral, "⧉", tok.SurfaceControl, tok.BorderDefault, tok.TextSecondary},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			style := resultBadgeStyleFor(tc.kind, tok, theme, false)
			if style.Glyph != tc.glyph {
				t.Errorf("glyph = %q, want %q", style.Glyph, tc.glyph)
			}
			if !sameColor(style.Background, tc.background) {
				t.Errorf("background = %v, want %v", style.Background, tc.background)
			}
			if !sameColor(style.Border, tc.border) {
				t.Errorf("border = %v, want %v", style.Border, tc.border)
			}
			if !sameColor(style.Text, tc.text) {
				t.Errorf("text = %v, want %v", style.Text, tc.text)
			}
			if style.BorderWidth != 1 {
				t.Errorf("border width = %v, want 1", style.BorderWidth)
			}

			selected := resultBadgeStyleFor(tc.kind, tok, theme, true)
			if selected.Glyph != tc.glyph {
				t.Errorf("selected glyph = %q, want %q", selected.Glyph, tc.glyph)
			}
			if !sameColor(selected.Background, ui.Transparent) {
				t.Errorf("selected background = %v, want transparent", selected.Background)
			}
			if want := theme.AccentText.Alpha(0.75); !sameColor(selected.Border, want) {
				t.Errorf("selected border = %v, want %v", selected.Border, want)
			}
			if !sameColor(selected.Text, theme.AccentText) {
				t.Errorf("selected text = %v, want %v", selected.Text, theme.AccentText)
			}
			if selected.BorderWidth != 1 {
				t.Errorf("selected border width = %v, want 1", selected.BorderWidth)
			}
		})
	}
}

// badgeResultRecord covers every badge kind the table can show: pending,
// human-confirmed, auto-accepted, failed, unprocessed and the neutral
// produced fallback without a structured payload.
func badgeResultRecord(t *testing.T) *record.Recovery {
	t.Helper()
	sets := []int{31000, 31100, 31200, 31300, 31400, 31500}
	items := make([]record.ItemRecord, 0, len(sets))
	for i, setID := range sets {
		disc := zzz.DiscRecord{
			SetID: intp(setID), Slot: i + 1, Level: intp(15),
			MainStat: zzz.Stat{Attribute: zzz.AttrHealth, Value: "2200", Unit: zzz.UnitFlat},
			Substats: []zzz.Substat{{
				Stat:             zzz.Stat{Attribute: zzz.AttrCritDamage, Value: "19.2", Unit: zzz.UnitPercent},
				EnhancementRolls: intp(3),
			}},
		}
		items = append(items, structuredItem(int64(i+1), disc, fmt.Sprintf("证据 %d", i+1)))
	}
	items[0].Quality = record.QualityNeedsReview
	items[1].Quality = record.QualityHumanConfirmed
	items[2].Quality = record.QualityAutoAccepted
	items[3] = item(4, record.StatusFailed, "", "识别超时", "失败")
	items[4] = item(5, record.StatusUnprocessed, record.ReasonCancelled, "", "未处理")
	items[5] = item(6, record.StatusProduced, "", "", "未解析")
	return recovery(items...)
}

func TestResultStatusBadgesAt1440And860(t *testing.T) {
	for _, tc := range []struct{ width, height int }{
		{width: 1440, height: 900},
		{width: 860, height: 560},
	} {
		t.Run(fmt.Sprintf("width-%d", tc.width), func(t *testing.T) {
			m, _, tt := openResponsiveResults(t, badgeResultRecord(t), tc.width, tc.height)
			if err := tt.Click("全部 6"); err != nil {
				t.Fatal(err)
			}
			tt.Frame()

			badges := []struct{ glyph, label string }{
				{"◐", "待校对"},
				{"✓", "人工确认"},
				{"○", "自动接受"},
				{"✕", "失败"},
				{"⊘", "未处理"},
				{"⧉", "未评价"},
			}
			for _, badge := range badges {
				glyph, ok := tt.Find(badge.glyph)
				if !ok {
					t.Fatalf("the %s badge shape is missing: %q", badge.label, tt.Texts())
				}
				label, ok := tt.Find(badge.label)
				if !ok {
					t.Fatalf("the %s badge label is missing: %q", badge.label, tt.Texts())
				}
				if glyph.X >= label.X || absFloat(glyph.Y-label.Y) > 3 {
					t.Fatalf("the %s badge does not pair its shape and label: %+v / %+v",
						badge.label, glyph, label)
				}
			}

			// The badges must not grow the table's 32-DIP rows.
			first, ok := tt.Find("待校对")
			if !ok {
				t.Fatalf("the first row's badge is missing: %q", tt.Texts())
			}
			last, ok := tt.Find("未评价")
			if !ok {
				t.Fatalf("the last row's badge is missing: %q", tt.Texts())
			}
			const rowHeight = 32
			if got, want := last.Y-first.Y, float32(5*rowHeight); got != want {
				t.Fatalf("badges changed the table row height: first-to-last = %v, want %v", got, want)
			}

			if tc.width == 860 {
				// A click on the badge itself still opens the detail
				// page, whose header carries the same badge and keeps
				// the problem subtitle.
				if err := tt.Click("人工确认"); err != nil {
					t.Fatal(err)
				}
				tt.Frame()
				if !m.resultsDetail {
					t.Fatal("clicking a badge row did not open the 860 detail page")
				}
				if !tt.HasText("← 结果列表") || !tt.HasText("✓") || !tt.HasText("人工确认") {
					t.Fatalf("the detail header does not show the confirmed badge: %q", tt.Texts())
				}
				if !tt.HasText("人工确认，未发现需核对项") {
					t.Fatalf("the detail subtitle is missing: %q", tt.Texts())
				}
			}
		})
	}
}

func absFloat(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
