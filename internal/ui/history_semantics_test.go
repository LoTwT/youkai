package ui

import (
	"image"
	"testing"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/egoist/mygo/ui"
)

func historySemanticRecords() []app.HistoryRecord {
	records := historyTableRecords(5)
	records[0].Counts = app.ItemClassCounts{Passed: records[0].CapturedCount}
	records[1].Coverage = record.CoveragePartial
	records[2].Coverage = record.CoveragePartial
	records[2].Termination = &record.Termination{Kind: record.TerminationFailed, Code: "focusLost", Phase: record.PhaseCapturing}
	records[3].Coverage = record.CoveragePartial
	records[3].Termination = &record.Termination{Kind: record.TerminationInterrupted, Phase: record.PhaseCapturing}
	records[4].Unreadable = true
	return records
}

// Choosing a row must preserve the warning/error foreground in the real
// table, alongside the selected face and a non-color severity cue.
func TestHistoryResultSemanticsSurviveSelection(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "light"
		if dark {
			name = "dark"
		}
		t.Run(name, func(t *testing.T) {
			m, q := newTestModel(t, Config{Service: &fakeService{history: historySemanticRecords()}})
			tt := ui.NewTesterWith(m.View, 1200, 800, ui.TesterOptions{Dark: dark, Preferences: ui.Preferences{TextScale: 1.2, ReduceMotion: true}})
			settle(t, m, q, tt)
			openRecordsList(t, m, q, tt)
			for selected := 0; selected < 3; selected++ {
				if selected > 0 {
					tt.Key(0, ui.KeyDown)
					tt.Frame()
				}
				if m.historyRow != selected {
					t.Fatalf("keyboard selected %d, want %d", m.historyRow, selected)
				}
				img := tt.Image()
				tok := m.effectiveTokens()
				for _, row := range []int{1, 2} {
					result := m.historyResultFor(m.history[row])
					fg, label := tok.WarningFG, "警告"
					strong := tok.WarningStrong
					if row == 2 {
						fg, label = tok.DangerFG, "错误"
						strong = tok.DangerStrong
					}
					r, ok := tt.Find(result.text)
					if !ok {
						t.Fatalf("result %q is missing", result.text)
					}
					region := image.Rect(int(r.X), int(r.Y), int(r.X+r.W), int(r.Y+r.H))
					if pixels := countColorInRect(img, rgba(fg), region) + countColorInRect(img, rgba(strong), region); pixels < 8 {
						t.Errorf("selected row %d: %s result %q lost its semantic foreground (%d pixels)", selected, label, result.text, pixels)
					}
					if _, ok := tt.AccessByRole(ui.RoleImage, label); !ok {
						t.Errorf("selected row %d: %s lacks an accessible shape cue", selected, label)
					}
				}
				r, ok := tt.Find(historyRowLabel(m, m.history[selected]))
				if !ok || countColorInRect(img, rgba(tok.RowSelected), image.Rect(int(r.X), int(r.Y), int(r.X+r.W), int(r.Y+r.H))) < 100 {
					t.Errorf("row %d lost its selected background", selected)
				}
			}
		})
	}
}
