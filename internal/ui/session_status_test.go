package ui

import (
	"strings"
	"testing"

	"github.com/LoTwT/youkai/internal/record"
	"github.com/LoTwT/youkai/internal/scan"
)

func TestSessionTerminationNotice(t *testing.T) {
	cases := []struct {
		name        string
		term        *record.Termination
		interrupted bool
		rows        int
		params      map[string]string
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
			got, kind, ok := sessionTerminationNotice(tc.term, tc.params, tc.interrupted, tc.rows)
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

// TestFailureTextLocalizesEveryPrecheckCode is the Chinese-mapping gate:
// every fault code this PR introduces has a user-facing explanation, and
// the explanation never leaks the raw diagnostic code.
func TestFailureTextLocalizesEveryPrecheckCode(t *testing.T) {
	cases := []struct {
		code   string
		params map[string]string
		want   string
	}{
		{"clientTierNotOpened", map[string]string{"current": "2560x1440"}, "2560×1440"},
		{"clientTierUnsupported", map[string]string{"current": "800x600"}, "800×600"},
		{"windowOutsideWorkArea", map[string]string{"client": "0,20 1920x1080", "workArea": "0,0 1920x1040"}, "任务栏"},
		{"windowOccluded", map[string]string{"probe": "bottom-button-bar", "hitClass": "Shell_TrayWnd"}, "Shell_TrayWnd"},
		{"frameContentBlank", map[string]string{"edge": "bottom"}, "纯黑"},
		{"exclusiveFullscreen", nil, "独占全屏"},
		{"integrityMismatch", map[string]string{"target": "high", "self": "medium"}, "以管理员身份运行"},
		{"integrityUnverifiable", map[string]string{"target": "high", "self": "unknown"}, "以管理员身份运行"},
		{"targetOffscreen", map[string]string{"window": "1800,100 1164x762"}, "完整放在一块屏幕内"},
		{"windowSizeUnsupported", map[string]string{"current": "1200x800", "expected": "1164x762"}, "1164×762"},
		{"frameConfigMismatch", map[string]string{"client": "macosCloud"}, "1164×762"},
		{"frameConfigMismatch", map[string]string{"client": "windowsLocal"}, "1920×1080"},
		{"anchorsMissing", nil, "布局"},
	}
	for _, tc := range cases {
		got := failureText(tc.code, tc.params)
		if got == "" {
			t.Fatalf("%s has no Chinese explanation", tc.code)
		}
		if !strings.Contains(got, tc.want) {
			t.Fatalf("%s = %q, want it to contain %q", tc.code, got, tc.want)
		}
		if strings.Contains(got, tc.code) {
			t.Fatalf("%s explanation leaks the raw code: %q", tc.code, got)
		}
	}
	if got := failureText("someUnknownCode", nil); got != "" {
		t.Fatalf("unknown codes must fall back to the diagnostic, got %q", got)
	}
}

func TestSessionTerminationNoticeUsesFailureParams(t *testing.T) {
	term := &record.Termination{Kind: record.TerminationFailed, Code: "clientTierNotOpened", Phase: record.PhasePreparing}
	got, kind, ok := sessionTerminationNotice(term, map[string]string{"current": "2560x1440"}, false, 0)
	if !ok || kind != record.TerminationFailed {
		t.Fatalf("notice = %q/%q/%v", got, kind, ok)
	}
	for _, want := range []string{"扫描失败：", "2560×1440", "1920×1080", "准备阶段", "未保留任何条目"} {
		if !strings.Contains(got, want) {
			t.Fatalf("notice %q missing %q", got, want)
		}
	}
}

func TestTriggerTextLocalizesKnownFault(t *testing.T) {
	trigger := scan.Trigger{
		Kind: scan.TriggerFault, Code: "integrityMismatch", Phase: scan.PhasePreparing,
		Params: map[string]string{"target": "high", "self": "medium"},
	}
	got := triggerText(trigger)
	if !strings.Contains(got, "以管理员身份运行") {
		t.Fatalf("trigger text = %q, want the Chinese permission prompt", got)
	}
	if strings.Contains(got, "integrityMismatch") {
		t.Fatalf("trigger text leaks the raw code: %q", got)
	}
}
