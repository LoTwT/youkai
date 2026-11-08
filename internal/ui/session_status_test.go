package ui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/LoTwT/youkai/internal/record"
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
			want:     "扫描失败：点击或拖拽后页面没有按预期变化。请重新开始扫描；反复出现时复制诊断信息。（采集阶段），未保留任何条目",
			wantKind: record.TerminationFailed,
			wantOK:   true,
		},
		{
			name:     "failed preparing",
			term:     &record.Termination{Kind: record.TerminationFailed, Code: "pageUnverified", Phase: record.PhasePreparing},
			rows:     0,
			want:     "没有开始：游戏页面不是驱动盘仓库页。请打开驱动盘仓库页后重新开始扫描。",
			wantKind: record.TerminationFailed,
			wantOK:   true,
		},
		{
			name:     "cancelled preparing",
			term:     &record.Termination{Kind: record.TerminationCancelled, Code: "userCancelled", Phase: record.PhasePreparing},
			want:     "没有开始：已手动停止",
			wantKind: record.TerminationCancelled,
			wantOK:   true,
		},
		{
			name:     "failed without a code",
			term:     &record.Termination{Kind: record.TerminationFailed, Phase: record.PhaseCapturing},
			rows:     1,
			want:     "扫描失败：原因未知（采集阶段）",
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
			got, kind, ok := sessionTerminationNotice(tc.term, tc.params, tc.interrupted, false, tc.rows)
			if got != tc.want || kind != tc.wantKind || ok != tc.wantOK {
				t.Fatalf("notice = %q/%q/%v, want %q/%q/%v", got, kind, ok, tc.want, tc.wantKind, tc.wantOK)
			}
		})
	}
}

func TestNotStartedEmptyResultsText(t *testing.T) {
	m, _ := newTestModel(t, Config{Service: &fakeService{}})
	m.rec = &RecordView{Shell: record.SessionShell{Termination: &record.Termination{
		Kind: record.TerminationFailed, Code: "integrityMismatch", Phase: record.PhasePreparing,
	}}}
	got := m.emptyResultsTextForTermination()
	if got != "没有已保存的条目。" {
		t.Fatalf("empty results text = %q, want the no-saved-items wording", got)
	}
}

func TestInterruptedEndingSurfacesAgree(t *testing.T) {
	cases := []struct {
		name        string
		term        *record.Termination
		interrupted bool
		live        bool
		summary     string
		notice      string
		empty       string
	}{
		{
			name:        "cancelled ending wins over an item flag",
			term:        &record.Termination{Kind: record.TerminationCancelled, Code: "userCancelled", Phase: record.PhaseCapturing},
			interrupted: true,
			summary:     "结束：手动停止",
			notice:      "扫描已停止：结果只覆盖已采集的条目",
			empty:       "本次扫描已停止，没有已保存的条目。",
		},
		{
			name:        "failed ending wins over an item flag",
			term:        &record.Termination{Kind: record.TerminationFailed, Code: "navigationMismatch", Phase: record.PhaseCapturing},
			interrupted: true,
			summary:     "结束：中途停止",
			notice:      "扫描失败：",
			empty:       "本次扫描失败，没有已保存的条目。",
		},
		{
			name:    "live interrupted termination is still an interruption",
			term:    &record.Termination{Kind: record.TerminationInterrupted, Code: "interrupted", Phase: record.PhaseCapturing},
			live:    true,
			summary: "结束：中断恢复",
			notice:  "扫描中断：仅保留已完成条目",
			empty:   "本次扫描中断，没有已保存的条目。",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := newTestModel(t, Config{Service: &fakeService{}})
			m.rec = &RecordView{
				ID: "rec", Live: tc.live, Interrupted: tc.interrupted,
				Shell: record.SessionShell{Termination: tc.term},
				Rows:  []Row{{Seq: 1}},
			}
			if got := m.summaryTerminationLine(); got != tc.summary {
				t.Errorf("summary line = %q, want %q", got, tc.summary)
			}
			if got := m.emptyResultsTextForTermination(); got != tc.empty {
				t.Errorf("empty text = %q, want %q", got, tc.empty)
			}
			notice, _, ok := sessionTerminationNotice(tc.term, nil, tc.interrupted, tc.live, 1)
			if !ok || !strings.HasPrefix(notice, tc.notice) {
				t.Errorf("notice = %q/%v, want prefix %q", notice, ok, tc.notice)
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
		{"failed", &record.Termination{Kind: record.TerminationFailed, Code: "navigationMismatch"}, "失败"},
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
		{"clientTierNotOpened", map[string]string{"current": "2560x1440", "allowed": "1920x1080"}, "2560×1440"},
		{"clientTierUnsupported", map[string]string{"current": "800x600", "allowed": "1920x1080"}, "800×600"},
		{"windowOutsideWorkArea", map[string]string{"client": "0,20 1920x1080", "workArea": "0,0 1920x1040", "allowed": "1920x1080"}, "任务栏"},
		{"windowOccluded", map[string]string{"probe": "bottom-button-bar", "hitClass": "Shell_TrayWnd", "hitProcess": "explorer.exe", "hitTitle": "不应显示"}, "explorer.exe"},
		{"frameContentBlank", map[string]string{"edge": "bottom"}, "纯黑"},
		{"exclusiveFullscreen", nil, "独占全屏"},
		{"integrityMismatch", map[string]string{"target": "high", "self": "medium"}, "以管理员身份运行"},
		{"integrityUnverifiable", map[string]string{"target": "high", "self": "unknown"}, "以管理员身份运行"},
		{"targetOffscreen", map[string]string{"window": "1800,100 1164x762"}, "完整放在一块屏幕内"},
		{"windowSizeUnsupported", map[string]string{"current": "1200x800", "expected": "1164x762"}, "1164×762"},
		{"frameConfigMismatch", map[string]string{"client": "macosCloud"}, "1164×762"},
		{"frameConfigMismatch", map[string]string{"client": "windowsLocal", "allowed": "1920x1080"}, "1920×1080"},
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
	if got := failureText("windowOccluded", map[string]string{
		"probe": "detail", "hitClass": "Shell_TrayWnd", "hitProcess": "explorer.exe", "hitTitle": "隐私标题",
	}); strings.Contains(got, "隐私标题") {
		t.Fatalf("the covered window's title must not be shown: %q", got)
	}
	if got := failureText("someUnknownCode", nil); got != "" {
		t.Fatalf("unknown codes must fall back to the diagnostic, got %q", got)
	}
}

func TestFailureTextUsesAllowedTiersAndChineseIntegrityLevels(t *testing.T) {
	got := failureText("clientTierNotOpened", map[string]string{"current": "1280x720", "allowed": "2560x1440"})
	if !strings.Contains(got, "2560×1440") || strings.Contains(got, "1920×1080") {
		t.Fatalf("the prompt must render the allowed tier, not a hard-coded 1920: %q", got)
	}
	got = failureText("windowOutsideWorkArea", map[string]string{"allowed": "2560x1440"})
	if !strings.Contains(got, "2560×1440") || strings.Contains(got, "1920×1080") {
		t.Fatalf("the containment prompt must render the allowed tier: %q", got)
	}
	got = failureText("integrityUnverifiable", map[string]string{"target": "high", "self": "unknown"})
	for _, want := range []string{"游戏 高", "youkai 未知", "不加载驱动", "不注入", "不读取游戏内存"} {
		if !strings.Contains(got, want) {
			t.Fatalf("integrity prompt %q missing %q", got, want)
		}
	}
	if strings.Contains(got, "high") || strings.Contains(got, "unknown") {
		t.Fatalf("integrity levels must be Chinese: %q", got)
	}
}

func TestSessionTerminationNoticeUsesFailureParams(t *testing.T) {
	term := &record.Termination{Kind: record.TerminationFailed, Code: "clientTierNotOpened", Phase: record.PhasePreparing}
	got, kind, ok := sessionTerminationNotice(term, map[string]string{"current": "2560x1440", "allowed": "1920x1080"}, false, false, 0)
	if !ok || kind != record.TerminationFailed {
		t.Fatalf("notice = %q/%q/%v", got, kind, ok)
	}
	for _, want := range []string{"没有开始：", "2560×1440", "1920×1080"} {
		if !strings.Contains(got, want) {
			t.Fatalf("notice %q missing %q", got, want)
		}
	}
}

func TestIntegrityLevelText(t *testing.T) {
	cases := map[string]string{
		"untrusted":  "不受信任",
		"low":        "低",
		"medium":     "中",
		"mediumPlus": "中+",
		"high":       "高",
		"system":     "系统",
		"protected":  "受保护",
		"unknown":    "未知",
		"":           "未知",
	}
	for level, want := range cases {
		if got := integrityLevelText(level); got != want {
			t.Errorf("%q = %q, want %q", level, got, want)
		}
	}
}

// TestIncompletePositionsTextNamesEveryPosition pins the note's wording
// against the typed position ledger #129 added: a known stage carries the
// localized cause, an unknown stage still names the position as unfinished,
// and the raw code never reaches the surface.
func TestIncompletePositionsTextNamesEveryPosition(t *testing.T) {
	shell := record.SessionShell{PositionLedger: &record.PositionLedger{
		Sort: record.SortVerificationVerified,
		Positions: []record.PositionConclusion{
			{Position: 2, Stage: "filter", ReasonCode: "navigationMismatch"},
			{Position: 5, Stage: "unexpected"},
		},
	}}
	got := incompletePositionsText(shell)
	want := "不完整号位：2 号位 筛选准备失败：点击或拖拽后页面没有按预期变化；5 号位 未完成"
	if got != want {
		t.Fatalf("incomplete positions = %q, want %q", got, want)
	}
	if strings.Contains(got, "navigationMismatch") {
		t.Fatalf("the note leaks English diagnostics: %q", got)
	}
}

// TestIncompletePositionsTextSkipsTruncatedAndNamesCodeOnly covers the two
// predicate edges through the typed ledger: a position cut by a
// session-fatal fault has no position-level cause and must not appear, while
// a position that carries only a reason code is degraded and keeps the
// generic "unfinished" stage.
func TestIncompletePositionsTextSkipsTruncatedAndNamesCodeOnly(t *testing.T) {
	truncated := record.SessionShell{PositionLedger: &record.PositionLedger{
		Sort:      record.SortVerificationUnverified,
		Positions: []record.PositionConclusion{{Position: 3}},
	}}
	if got := incompletePositionsText(truncated); got != "" {
		t.Fatalf("truncated position produced a note: %q", got)
	}
	codeOnly := record.SessionShell{PositionLedger: &record.PositionLedger{
		Sort: record.SortVerificationVerified,
		Positions: []record.PositionConclusion{
			{Position: 4, ReasonCode: "navigationMismatch"},
		},
	}}
	want := "不完整号位：4 号位 未完成：点击或拖拽后页面没有按预期变化"
	if got := incompletePositionsText(codeOnly); got != want {
		t.Fatalf("code-only position = %q, want %q", got, want)
	}
}

// TestIncompletePositionsTextReadsLegacyObservedScope keeps the one-time
// compatibility path visible at this layer too: a record written before the
// ledger existed still names its degraded positions, without the UI parsing
// the raw scope itself.
func TestIncompletePositionsTextReadsLegacyObservedScope(t *testing.T) {
	shell := record.SessionShell{ObservedScope: []byte(`{"positions":[
		{"position":2,"stage":"filter","reasonCode":"navigationMismatch","reason":"the filter panel never closed"}
	]}`)}
	got := incompletePositionsText(shell)
	want := "不完整号位：2 号位 筛选准备失败：点击或拖拽后页面没有按预期变化"
	if got != want {
		t.Fatalf("legacy scope note = %q, want %q", got, want)
	}
}

// TestFaultGuidanceCoversEveryCode is the table's completeness gate. It
// reads the diagnostic-code constants from every package that defines
// faults, so adding a code without a guidance row fails here instead of
// leaking an English code to the user.
func TestFaultGuidanceCoversEveryCode(t *testing.T) {
	codes := map[string]string{"unknownEnd": "internal/app/deliverer.go"}
	// Keep this list aligned with every package that can emit a terminal
	// trigger or a position reason code; a new package stays invisible
	// until it is added here. internal/app also writes codes as plain
	// strings (unknownEnd in deliverer.go), so it needs an explicit entry
	// instead of a source scan. internal/game/zzz/disc.go is deliberately
	// absent: its Code* constants are per-item review codes, not faults.
	for _, dir := range []string{
		"../platform",
		"../platform/windows",
		"../game/zzz/collect",
		"../scan",
	} {
		collectFaultCodeConstants(t, dir, codes)
	}
	for _, want := range []string{"focusLost", "dpiVirtualized", "prepareTimeout", "storageFailure", "unknownEnd"} {
		if _, ok := codes[want]; !ok {
			t.Fatalf("fault-code collection missed %s; the source scan found only %v", want, codes)
		}
	}

	// Normal completion and a user stop have their own termination
	// surfaces; they are not failures and never ask for a recovery action.
	nonFailure := map[string]bool{"verifiedEnd": true, "userCancelled": true}
	for code, source := range codes {
		if nonFailure[code] {
			continue
		}
		guidance, ok := faultGuidanceFor(code, nil)
		if !ok {
			t.Errorf("%s (%s) has no fault-guidance entry", code, source)
			continue
		}
		checkFaultGuidance(t, code, guidance)
	}
	for code := range faultGuidanceTable {
		if _, ok := codes[code]; !ok {
			t.Errorf("fault-guidance entry %q does not match any defined fault code", code)
		}
	}
}

// checkFaultGuidance enforces the wording contract of 09 §5: every entry
// has a cause and an action, the action is a complete sentence, the row
// names an exit, and no English diagnostic code reaches the UI.
func checkFaultGuidance(t *testing.T, code string, guidance faultGuidance) {
	t.Helper()
	if guidance.cause == "" {
		t.Errorf("%s has no cause", code)
	}
	if guidance.action == "" {
		t.Errorf("%s has no action", code)
	}
	if guidance.exit == faultExitNone {
		t.Errorf("%s has no exit", code)
	}
	if strings.HasSuffix(guidance.cause, "。") {
		t.Errorf("%s cause must not end with a full stop: %q", code, guidance.cause)
	}
	if !strings.HasSuffix(guidance.action, "。") {
		t.Errorf("%s action must be a complete sentence: %q", code, guidance.action)
	}
	for _, part := range []string{guidance.cause, guidance.action} {
		if strings.Contains(part, code) {
			t.Errorf("%s text leaks the raw diagnostic code: %q", code, part)
		}
		if strings.Contains(part, "\n") {
			t.Errorf("%s text must be one line: %q", code, part)
		}
	}
	text := failureText(code, nil)
	if text == "" || !strings.HasSuffix(text, "。") {
		t.Errorf("%s failureText = %q, want a complete sentence", code, text)
	}
}

// collectFaultCodeConstants adds every `Code… = "…"` constant under dir to
// out. It parses source instead of keeping a second hand-written list, so
// a new code in the platform layers extends the gate automatically. The
// windows package's Precheck* values are internal refusal details that the
// adapter translates to Code* before the UI sees them, so the Code prefix
// is the right boundary.
func collectFaultCodeConstants(t *testing.T, dir string, out map[string]string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read fault-code directory %s: %v", dir, err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(dir, name)
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				valueSpec, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, ident := range valueSpec.Names {
					if !strings.HasPrefix(ident.Name, "Code") || i >= len(valueSpec.Values) {
						continue
					}
					lit, ok := valueSpec.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					value, err := strconv.Unquote(lit.Value)
					if err != nil {
						t.Fatalf("unquote %s in %s: %v", ident.Name, path, err)
					}
					out[value] = path
				}
			}
		}
	}
}

// TestFaultGuidanceDPIWording pins the three S3 DPI rows whose treatment
// was undecided when the design was written. The wording is the contract
// the checklist and diagnostics reuse.
func TestFaultGuidanceDPIWording(t *testing.T) {
	checkFaultGuidanceRows(t, []faultGuidanceCase{
		{
			code:       "dpiAwarenessMismatch",
			wantCause:  "youkai 的显示缩放支持不完整",
			wantAction: "这是 youkai 自身的问题，请复制诊断信息反馈。",
			wantExit:   faultExitDiagnostics,
			diag:       true,
		},
		{
			code:       "dpiVirtualized",
			wantCause:  "游戏窗口的显示缩放与显示器不一致",
			wantAction: "请右键游戏程序 › 属性 › 兼容性 › 更改高 DPI 设置，关闭“替代高 DPI 缩放行为”，重新打开游戏后再试；仍不通过时复制诊断信息。",
			wantExit:   faultExitDisplayScale,
			diag:       true,
		},
		{
			code:       "dpiCheckUnavailable",
			wantCause:  "无法读取游戏窗口或显示器的显示缩放",
			wantAction: "请重新打开游戏和 youkai 后再试；仍无法读取时复制诊断信息。",
			wantExit:   faultExitDisplayScale,
			diag:       true,
		},
	})
}

// TestFaultGuidanceRecoveryWording pins the rows whose recovery path is
// easy to get wrong: the Windows capability check, UIPI blocking, a lost
// scroll position, an unsaved screenshot, and the platform notice.
func TestFaultGuidanceRecoveryWording(t *testing.T) {
	checkFaultGuidanceRows(t, []faultGuidanceCase{
		{
			code:       "permissionMissing",
			wantCause:  "youkai 缺少运行所需的系统能力",
			wantAction: "这通常是 youkai 自身的显示缩放支持不完整，请复制诊断信息反馈。",
			wantExit:   faultExitDiagnostics,
			diag:       true,
		},
		{
			code:       "inputBlocked",
			wantCause:  "系统拦截了 youkai 的鼠标操作",
			wantAction: "请先从托盘退出 youkai，再右键 youkai 选择“以管理员身份运行”重新打开，然后重新开始扫描。youkai 只向游戏发送鼠标操作，不加载驱动、不注入、不读取游戏内存。",
			wantExit:   faultExitAdmin,
		},
		{
			code:       "evidenceFailed",
			wantCause:  "采集到的截图没有保存成功",
			wantAction: "请重新开始扫描；反复出现时复制诊断信息。",
			wantExit:   faultExitRestart,
			diag:       true,
		},
		{
			code:       "positionLost",
			wantCause:  "列表滚动后无法确认滚到了哪里",
			wantAction: "请重新开始扫描；反复出现时复制诊断信息。",
			wantExit:   faultExitRestart,
			diag:       true,
		},
		{
			code:       "unsupportedPlatform",
			wantCause:  "当前系统不支持扫描",
			wantAction: "当前版本只支持 Windows 本地客户端。",
			wantExit:   faultExitDiagnostics,
			diag:       true,
		},
	})
}

type faultGuidanceCase struct {
	code       string
	wantCause  string
	wantAction string
	wantExit   faultExit
	diag       bool
}

func checkFaultGuidanceRows(t *testing.T, cases []faultGuidanceCase) {
	t.Helper()
	for _, tc := range cases {
		guidance, ok := faultGuidanceFor(tc.code, nil)
		if !ok {
			t.Fatalf("%s has no guidance", tc.code)
		}
		if guidance.cause != tc.wantCause || guidance.action != tc.wantAction {
			t.Errorf("%s = %q / %q, want %q / %q", tc.code, guidance.cause, guidance.action, tc.wantCause, tc.wantAction)
		}
		if guidance.exit != tc.wantExit || guidance.diagnostics != tc.diag {
			t.Errorf("%s exit = %v/%v, want %v/%v", tc.code, guidance.exit, guidance.diagnostics, tc.wantExit, tc.diag)
		}
	}
}
