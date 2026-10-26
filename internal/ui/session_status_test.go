package ui

import (
	"testing"

	"github.com/LoTwT/youkai/internal/record"
)

func TestSessionTerminationNotice(t *testing.T) {
	cases := []struct {
		name        string
		term        *record.Termination
		interrupted bool
		rows        int
		want        string
		wantKind    record.TerminationKind
		wantOK      bool
	}{
		{
			name: "completed",
			term: &record.Termination{Kind: record.TerminationCompleted, Code: "verifiedEnd", Phase: record.PhaseCapturing},
		},
		{
			name:     "failed without rows",
			term:     &record.Termination{Kind: record.TerminationFailed, Code: "navigationMismatch", Phase: record.PhaseCapturing},
			want:     "扫描失败：navigationMismatch（采集阶段），未保留任何条目",
			wantKind: record.TerminationFailed,
			wantOK:   true,
		},
		{
			name:     "failed preparing",
			term:     &record.Termination{Kind: record.TerminationFailed, Code: "pageUnverified", Phase: record.PhasePreparing},
			rows:     3,
			want:     "扫描失败：pageUnverified（准备阶段）",
			wantKind: record.TerminationFailed,
			wantOK:   true,
		},
		{
			name:     "cancelled without rows",
			term:     &record.Termination{Kind: record.TerminationCancelled, Code: "userCancelled", Phase: record.PhaseCapturing},
			want:     "扫描已停止：结果只覆盖已采集的条目，未保留任何条目",
			wantKind: record.TerminationCancelled,
			wantOK:   true,
		},
		{
			name:     "interrupted termination",
			term:     &record.Termination{Kind: record.TerminationInterrupted, Code: "interrupted", Phase: record.PhaseCapturing},
			want:     "扫描中断：仅保留已完成条目",
			wantKind: record.TerminationInterrupted,
			wantOK:   true,
		},
		{
			name:        "nil termination with interrupted rows",
			interrupted: true,
			want:        "扫描中断：仅保留已完成条目",
			wantKind:    record.TerminationInterrupted,
			wantOK:      true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, kind, ok := sessionTerminationNotice(tc.term, tc.interrupted, tc.rows)
			if got != tc.want || kind != tc.wantKind || ok != tc.wantOK {
				t.Fatalf("notice = %q/%q/%v, want %q/%q/%v", got, kind, ok, tc.want, tc.wantKind, tc.wantOK)
			}
		})
	}
}

func TestHistoryTerminationLabel(t *testing.T) {
	cases := []struct {
		name string
		term *record.Termination
		want string
	}{
		{"nil", nil, ""},
		{"completed", &record.Termination{Kind: record.TerminationCompleted, Code: "verifiedEnd"}, ""},
		{"failed", &record.Termination{Kind: record.TerminationFailed, Code: "navigationMismatch"}, "失败：navigationMismatch"},
		{"failed without code", &record.Termination{Kind: record.TerminationFailed}, "失败"},
		{"cancelled", &record.Termination{Kind: record.TerminationCancelled, Code: "userCancelled"}, "已停止"},
		{"interrupted", &record.Termination{Kind: record.TerminationInterrupted, Code: "interrupted"}, "中断"},
	}
	for _, tc := range cases {
		if got := historyTerminationLabel(tc.term); got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, got, tc.want)
		}
	}
}
