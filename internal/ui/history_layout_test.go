package ui

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// A larger system text size must preserve both the complete timestamp and
// the current-session marker in the native table, including narrow windows.
func TestHistoryCurrentTimestampAtLargerText(t *testing.T) {
	for _, width := range []int{1440, 1200, 960} {
		records := historyTableRecords(1)
		records[0].SavedAt = time.Date(2026, 11, 16, 21, 48, 0, 0, time.Local)
		m, q := newTestModel(t, Config{Service: &fakeService{history: records}})
		m.appliedSession = "r01"
		m.SetContentSize(width, 800)
		var wantTime, markerHeight float32
		tt := ui.NewTesterWith(func(c *ui.Context) {
			m.View(c)
			wantTime, _ = c.MeasureText(0, ui.Span{Text: historyTimeText(HistoryEntry{SavedAt: records[0].SavedAt}), Weight: 600, Features: "tnum"})
			_, markerHeight = c.MeasureText(0, ui.Span{Text: "本次", Size: c.Theme().FontSize * 12 / 14})
		}, width, 800, ui.TesterOptions{Preferences: ui.Preferences{TextScale: 1.2, ReduceMotion: true}})
		settle(t, m, q, tt)
		openRecordsList(t, m, q, tt)
		timestamp, ok := tt.Find(historyTimeText(m.history[0]))
		if !ok || timestamp.W+0.5 < wantTime {
			t.Errorf("width %d timestamp = %+v, needs %.1f DIP for the full time", width, timestamp, wantTime)
		}
		marker, ok := tt.Find("本次")
		if !ok || marker.H > markerHeight+0.5 || marker.X < timestamp.X+timestamp.W {
			t.Errorf("width %d current-session marker = %+v, needs one line beside time %+v", width, marker, timestamp)
		}
	}
}

// Byte formatting gets shorter at a unit boundary, so the largest byte
// count alone must not determine the width of every storage value.
func TestHistoryStorageBeforeUnitBoundaryStaysVisible(t *testing.T) {
	records := historyTableRecords(2)
	records[0].TotalBytes = 2 << 30
	records[1].TotalBytes = (1 << 30) - 1
	m, q := newTestModel(t, Config{Service: &fakeService{history: records}})
	label := formatBytes(records[1].TotalBytes)
	var wanted float32
	tt := ui.NewTesterWith(func(c *ui.Context) {
		m.View(c)
		wanted, _ = c.MeasureText(0, ui.Span{Text: label, Features: "tnum"})
	}, 1440, 900, ui.TesterOptions{Preferences: ui.Preferences{TextScale: 1.2, ReduceMotion: true}})
	settle(t, m, q, tt)
	openRecordsList(t, m, q, tt)
	got, ok := tt.Find(label)
	if !ok || got.W+0.5 < wanted {
		t.Fatalf("storage %q = %+v, needs %.1f DIP", label, got, wanted)
	}
}
