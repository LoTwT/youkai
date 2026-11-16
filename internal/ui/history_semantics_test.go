package ui

import (
	"image"
	"math"
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

// Every result reserves the same vector-icon slot, including ordinary and
// zero-item endings. Tags must not shift the conclusion's starting edge.
func TestHistoryResultsHaveAlignedAccessibleIcons(t *testing.T) {
	cases := []struct {
		name, label string
		change      func(*app.HistoryRecord)
		live, save  bool
	}{
		{name: "complete", label: "完成"},
		{name: "pending", label: "待处理", change: func(r *app.HistoryRecord) { r.Counts = app.ItemClassCounts{PendingReview: r.CapturedCount} }},
		{name: "warning", label: "警告", change: func(r *app.HistoryRecord) { r.Counts = app.ItemClassCounts{Warning: r.CapturedCount} }},
		{name: "partial", label: "警告", change: func(r *app.HistoryRecord) { r.Coverage = record.CoveragePartial }},
		{name: "interrupted", label: "中断", change: func(r *app.HistoryRecord) { r.Termination.Kind = record.TerminationInterrupted }},
		{name: "failed", label: "错误", change: func(r *app.HistoryRecord) {
			r.Termination.Kind = record.TerminationFailed
			r.Termination.Code = "focusLost"
		}},
		{name: "unreadable", label: "记录不可读", change: func(r *app.HistoryRecord) { r.Unreadable = true }},
		{name: "cancelled", label: "手动停止", change: func(r *app.HistoryRecord) { r.Termination.Kind = record.TerminationCancelled }},
		{name: "empty", label: "未采集", change: func(r *app.HistoryRecord) { r.CapturedCount = 0; r.Counts = app.ItemClassCounts{} }},
		{name: "empty-interrupted", label: "中断", change: func(r *app.HistoryRecord) {
			r.CapturedCount = 0
			r.Counts = app.ItemClassCounts{}
			r.Termination.Kind = record.TerminationInterrupted
		}},
		{name: "empty-cancelled", label: "手动停止", change: func(r *app.HistoryRecord) {
			r.CapturedCount = 0
			r.Counts = app.ItemClassCounts{}
			r.Termination.Kind = record.TerminationCancelled
		}},
		{name: "empty-failed", label: "错误", change: func(r *app.HistoryRecord) {
			r.CapturedCount = 0
			r.Counts = app.ItemClassCounts{}
			r.Termination.Kind = record.TerminationFailed
		}},
		{name: "unknown", label: "状态未知", change: func(r *app.HistoryRecord) { r.Termination.Kind = "future" }},
		{name: "coverage-unknown", label: "覆盖未知", change: func(r *app.HistoryRecord) { r.Coverage = "unknown" }},
		{name: "legacy-counts", label: "完成", change: func(r *app.HistoryRecord) { r.Counts = app.ItemClassCounts{} }},
		{name: "running", label: "进行中", live: true},
		{name: "save-failed", label: "保存失败", live: true, save: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			records := historyTableRecords(1)
			records[0].Counts = app.ItemClassCounts{Passed: records[0].CapturedCount}
			if tc.change != nil {
				tc.change(&records[0])
			}
			m, q := newTestModel(t, Config{Service: &fakeService{history: records}})
			tt := ui.NewTesterWith(m.View, 1440, 900, ui.TesterOptions{Preferences: ui.Preferences{TextScale: 1.2, ReduceMotion: true}})
			settle(t, m, q, tt)
			openRecordsList(t, m, q, tt)
			if tc.live {
				m.appliedSession = "r01"
				m.snap.Busy = true
			}
			if tc.save {
				m.snap.SaveError = "synthetic save failure"
			}
			tt.Frame()
			icon, ok := tt.AccessByRole(ui.RoleImage, tc.label)
			if !ok {
				t.Fatalf("result lacks accessible %q icon", tc.label)
			}
			conclusion, ok := tt.AccessByRole(ui.RoleText, m.historyResultFor(m.history[0]).text)
			if !ok {
				t.Fatal("conclusion is missing")
			}
			text := conclusion.Bounds
			if icon.Bounds.W != 20 || icon.Bounds.H != 20 || math.Abs(float64(text.X-icon.Bounds.X-icon.Bounds.W-8)) > 0.5 {
				t.Errorf("icon slot %+v and text %+v do not preserve a common start", icon.Bounds, text)
			}
		})
	}
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
					warning, danger := historyResultInks(tok, dark)
					fg, label := warning, "警告"
					if row == 2 {
						fg, label = danger, "错误"
					}
					r, ok := tt.Find(result.text)
					if !ok {
						t.Fatalf("result %q is missing", result.text)
					}
					region := image.Rect(int(r.X), int(r.Y), int(r.X+r.W), int(r.Y+r.H))
					if pixels := countColorInRect(img, rgba(fg), region); pixels < 8 {
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
