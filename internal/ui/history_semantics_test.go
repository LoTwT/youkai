package ui

import (
	"fmt"
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

// A short status and the unchanged explanation occupy distinct text columns.
// Keep the existing outcome boundaries, including active and zero-item cases.
func TestHistoryTextStatuses(t *testing.T) {
	cases := []struct {
		name, label string
		change      func(*app.HistoryRecord)
		live, save  bool
	}{
		{name: "complete", label: "完成"},
		{name: "pending", label: "待处理", change: func(r *app.HistoryRecord) { r.Counts = app.ItemClassCounts{PendingReview: r.CapturedCount} }},
		{name: "warning", label: "警告", change: func(r *app.HistoryRecord) { r.Counts = app.ItemClassCounts{Warning: r.CapturedCount} }},
		{name: "partial", label: "部分覆盖", change: func(r *app.HistoryRecord) { r.Coverage = record.CoveragePartial }},
		{name: "interrupted", label: "中断", change: func(r *app.HistoryRecord) { r.Termination.Kind = record.TerminationInterrupted }},
		{name: "failed", label: "失败", change: func(r *app.HistoryRecord) {
			r.Termination.Kind = record.TerminationFailed
			r.Termination.Code = "focusLost"
		}},
		{name: "unreadable", label: "不可读", change: func(r *app.HistoryRecord) { r.Unreadable = true }},
		{name: "cancelled", label: "手动停止", change: func(r *app.HistoryRecord) { r.Termination.Kind = record.TerminationCancelled }},
		{name: "empty", label: "无采集", change: func(r *app.HistoryRecord) { r.CapturedCount = 0; r.Counts = app.ItemClassCounts{} }},
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
		{name: "empty-failed", label: "失败", change: func(r *app.HistoryRecord) {
			r.CapturedCount = 0
			r.Counts = app.ItemClassCounts{}
			r.Termination.Kind = record.TerminationFailed
		}},
		{name: "unknown", label: "未知", change: func(r *app.HistoryRecord) { r.Termination.Kind = "future" }},
		{name: "coverage-unknown", label: "覆盖未知", change: func(r *app.HistoryRecord) { r.Coverage = "unknown" }},
		{name: "legacy-counts", label: "完成", change: func(r *app.HistoryRecord) { r.Counts = app.ItemClassCounts{} }},
		{name: "running", label: "进行中", live: true},
		{name: "save-failed", label: "保存失败", live: true, save: true},
		{name: "prepare-failed", label: "准备失败", change: func(r *app.HistoryRecord) {
			r.CapturedCount = 0
			r.Counts = app.ItemClassCounts{}
			r.Termination = &record.Termination{Kind: record.TerminationFailed, Phase: record.PhasePreparing, Code: "focusLost"}
		}},
		{name: "prepare-cancelled", label: "未开始", change: func(r *app.HistoryRecord) {
			r.CapturedCount = 0
			r.Counts = app.ItemClassCounts{}
			r.Termination = &record.Termination{Kind: record.TerminationCancelled, Phase: record.PhasePreparing}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			records := historyTableRecords(1)
			records[0].Counts = app.ItemClassCounts{Passed: records[0].CapturedCount}
			if tc.change != nil {
				tc.change(&records[0])
			}
			m, q := newTestModel(t, Config{Service: &fakeService{history: records}})
			m.SetContentSize(1440, 900)
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
			statusColumn, ok := tt.AccessByRole(ui.RoleColumnHeader, "状态")
			if !ok {
				t.Fatal("the standard table lacks its status column")
			}
			resultColumn, ok := tt.AccessByRole(ui.RoleColumnHeader, "结果说明")
			if !ok {
				t.Fatal("the standard table lacks its explanation column")
			}
			var statusFound, explanationFound bool
			for _, node := range tt.AccessAll() {
				if node.Role != ui.RoleText {
					continue
				}
				if node.Name == tc.label && node.Bounds.X >= statusColumn.Bounds.X && node.Bounds.X+node.Bounds.W <= statusColumn.Bounds.X+statusColumn.Bounds.W {
					statusFound = true
				}
				if node.Name == m.historyResultFor(m.history[0]).text && node.Bounds.X >= resultColumn.Bounds.X && node.Bounds.X+node.Bounds.W <= resultColumn.Bounds.X+resultColumn.Bounds.W {
					explanationFound = true
				}
			}
			if !statusFound || !explanationFound {
				t.Errorf("status %q or its unchanged explanation is missing from its column", tc.label)
			}
		})
	}
}

// Both column tiers retain neutral text and the native selected face while
// the keyboard moves between ordinary, partial and failed records.
func TestHistoryTextResultsSurviveSelection(t *testing.T) {
	for _, width := range []int{1440, 960} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m, q := newTestModel(t, Config{Service: &fakeService{history: historySemanticRecords()}})
			m.SetContentSize(width, 800)
			tt := ui.NewTesterWith(m.View, width, 800, ui.TesterOptions{Preferences: ui.Preferences{TextScale: 1.2, ReduceMotion: true}})
			settle(t, m, q, tt)
			openRecordsList(t, m, q, tt)
			if width == 960 && !tt.HasText("状态 / 结果说明") {
				t.Fatal("compact table lost its merged status/explanation column")
			}
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
					r, ok := tt.Find(result.text)
					if !ok {
						t.Fatalf("result %q is missing", result.text)
					}
					region := image.Rect(int(r.X), int(r.Y), int(r.X+r.W), int(r.Y+r.H))
					if pixels := countColorInRect(img, rgba(tok.TextPrimary), region); pixels < 8 {
						t.Errorf("selected row %d: result %q is not ordinary body text (%d pixels)", selected, result.text, pixels)
					}
				}
				if !tt.HasText("部分覆盖") || !tt.HasText("失败") {
					t.Error("short statuses were lost")
				}
				for _, node := range tt.AccessAll() {
					if node.Role == ui.RoleImage && node.Bounds.X > m.railWidth()+m.pageLeftPadding() {
						t.Errorf("result decoration remains: %q", node.Name)
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
