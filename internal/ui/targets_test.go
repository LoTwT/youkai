package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/egoist/mygo/ui"
)

// targetSource is the installed Windows source's prepare-area contract:
// it discovers windows and starts only a confirmed one (P-01).
func targetSource() ScanSourceInfo {
	return ScanSourceInfo{Available: true, Label: "游戏窗口（自动遍历）", TargetSelection: true}
}

func testTarget(handle uintptr, pid uint32, path string) app.TargetCandidate {
	return app.TargetCandidate{
		Handle:           handle,
		PID:              pid,
		ExecutablePath:   path,
		ProcessStartedAt: time.Date(2026, 11, 2, 8, 0, 0, 0, time.UTC),
		ClassName:        "UnityWndClass",
	}
}

// TestTargetLabelsDistinguishWindowsOfOneProcess: two windows of the same
// process have the same path and PID, so the handle and class must tell
// them apart without a window title.
func TestTargetLabelsDistinguishWindowsOfOneProcess(t *testing.T) {
	first := testTarget(1, 42, `C:\Games\ZZZ\ZenlessZoneZero.exe`)
	second := testTarget(2, 42, `C:\Games\ZZZ\ZenlessZoneZero.exe`)
	if targetLabel(first) == targetLabel(second) {
		t.Fatalf("two windows of one process share a label: %q", targetLabel(first))
	}
	if !strings.Contains(targetLabel(first), "0x1") {
		t.Fatalf("the label should carry the handle: %q", targetLabel(first))
	}
}

// TestViewTargetNotFound: no running game shows "not found" and the start
// is disabled; the discovery details stay out of the way otherwise.
func TestViewTargetNotFound(t *testing.T) {
	svc := &fakeService{}
	m, q := newTestModel(t, Config{Service: svc, ScanSource: targetSource()})
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)

	if !tt.HasText("未找到《绝区零》窗口。请启动游戏并保持窗口可见，然后点“重新检查”。") {
		t.Fatalf("the not-found state does not show: %q", tt.Texts())
	}
	if !tt.HasText("未找到《绝区零》窗口，请启动游戏后点“重新检查”。") {
		t.Fatalf("the start's refusal reason does not show: %q", tt.Texts())
	}
	if err := tt.Click("开始扫描"); err != nil {
		t.Fatal(err)
	}
	drainAll(t, m, q)
	if len(svc.starts) != 0 {
		t.Fatalf("the disabled start issued a request: %+v", svc.starts)
	}
}

// TestViewSingleCandidateNeedsConfirmation: the unique window is shown but
// not chosen; the user confirms it before the start carries it.
func TestViewSingleCandidateNeedsConfirmation(t *testing.T) {
	cand := testTarget(0xB0ABE, 42, `C:\Games\ZZZ\ZenlessZoneZero.exe`)
	svc := &fakeService{targets: []app.TargetCandidate{cand}}
	m, q := newTestModel(t, Config{Service: svc, ScanSource: targetSource()})
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)

	if !tt.HasText("已找到 1 个窗口，请选择要扫描的一个。") {
		t.Fatalf("the candidate count does not show: %q", tt.Texts())
	}
	label := targetLabel(cand)
	if !tt.HasText(label) {
		t.Fatalf("the unique candidate is not shown: %q", tt.Texts())
	}
	if !tt.HasText("请选择要扫描的游戏窗口。") {
		t.Fatalf("the unconfirmed-target reason does not show: %q", tt.Texts())
	}
	if err := tt.Click("开始扫描"); err != nil {
		t.Fatal(err)
	}
	drainAll(t, m, q)
	if len(svc.starts) != 0 {
		t.Fatalf("an unconfirmed start was issued: %+v", svc.starts)
	}

	if err := tt.Click(label); err != nil {
		t.Fatalf("click the candidate: %v", err)
	}
	settle(t, m, q, tt)
	if m.targetChoice != 0 {
		t.Fatalf("target choice = %d, want 0", m.targetChoice)
	}
	if err := tt.Click("开始扫描"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if len(svc.starts) != 1 {
		t.Fatalf("starts = %d, want one: %+v", len(svc.starts), svc.starts)
	}
	req := svc.lastStart()
	if req.Target == nil || req.Target.Handle != cand.Handle || req.Target.PID != cand.PID {
		t.Fatalf("the start did not carry the confirmed candidate: %+v", req.Target)
	}
}

// TestViewTwoCandidatesRequireAChoice: several windows are listed and only
// the one the user picks reaches the start request.
func TestViewTwoCandidatesRequireAChoice(t *testing.T) {
	first := testTarget(1, 42, `C:\Games\A\ZenlessZoneZero.exe`)
	second := testTarget(2, 77, `D:\Games\B\ZenlessZoneZero.exe`)
	svc := &fakeService{targets: []app.TargetCandidate{first, second}}
	m, q := newTestModel(t, Config{Service: svc, ScanSource: targetSource()})
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)

	if !tt.HasText("已找到 2 个窗口，请选择要扫描的一个。") {
		t.Fatalf("the candidate count does not show: %q", tt.Texts())
	}
	if !tt.HasText(targetLabel(first)) || !tt.HasText(targetLabel(second)) {
		t.Fatalf("both candidates must be listed: %q", tt.Texts())
	}
	if err := tt.Click(targetLabel(second)); err != nil {
		t.Fatalf("click the second candidate: %v", err)
	}
	settle(t, m, q, tt)
	if m.targetChoice != 1 {
		t.Fatalf("target choice = %d, want the second row", m.targetChoice)
	}
	if err := tt.Click("开始扫描"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if len(svc.starts) != 1 {
		t.Fatalf("starts = %d, want one", len(svc.starts))
	}
	if req := svc.lastStart(); req.Target == nil || req.Target.Handle != second.Handle {
		t.Fatalf("the start did not carry the chosen window: %+v", req.Target)
	}
}

// TestViewTargetRefreshClearsVanishedChoice: a refresh that no longer
// lists the confirmed window clears the choice and disables the start —
// the old window is never started silently.
func TestViewTargetRefreshClearsVanishedChoice(t *testing.T) {
	first := testTarget(1, 42, `C:\Games\A\ZenlessZoneZero.exe`)
	second := testTarget(2, 77, `D:\Games\B\ZenlessZoneZero.exe`)
	svc := &fakeService{targets: []app.TargetCandidate{first, second}}
	m, q := newTestModel(t, Config{Service: svc, ScanSource: targetSource()})
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)

	if err := tt.Click(targetLabel(first)); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if m.targetChoice != 0 {
		t.Fatalf("target choice = %d, want 0", m.targetChoice)
	}

	svc.mu.Lock()
	svc.targets = []app.TargetCandidate{second}
	svc.mu.Unlock()
	if err := tt.Click("重新检查"); err != nil {
		t.Fatalf("click refresh: %v", err)
	}
	settle(t, m, q, tt)
	if m.targetChoice != -1 {
		t.Fatalf("a vanished candidate kept its choice: %d", m.targetChoice)
	}
	if m.targetReady() {
		t.Fatal("the start must stay disabled after the confirmed window vanished")
	}
	if !tt.HasText("已找到 1 个窗口，请选择要扫描的一个。") {
		t.Fatalf("the refreshed count does not show: %q", tt.Texts())
	}
}

// TestViewTargetDiscoveryFailure shows the enumeration failure and keeps
// the start disabled until a check succeeds.
func TestViewTargetDiscoveryFailure(t *testing.T) {
	svc := &fakeService{targetsErr: errString("枚举失败")}
	m, q := newTestModel(t, Config{Service: svc, ScanSource: targetSource()})
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)

	if !tt.HasText("查找游戏窗口失败：枚举失败") {
		t.Fatalf("the discovery failure does not show: %q", tt.Texts())
	}
	if err := tt.Click("开始扫描"); err != nil {
		t.Fatal(err)
	}
	drainAll(t, m, q)
	if len(svc.starts) != 0 {
		t.Fatalf("a failed discovery still allowed a start: %+v", svc.starts)
	}
}

// A fixed-target source (macOS, the environment override) shows no target
// list and starts directly.
func TestViewFixedTargetSourceSkipsSelection(t *testing.T) {
	svc := &fakeService{}
	m, q := newTestModel(t, Config{Service: svc, ScanSource: availableSource()})
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)

	if svc.discoveries != 0 {
		t.Fatalf("a fixed-target source discovered %d times", svc.discoveries)
	}
	if strings.Contains(strings.Join(tt.Texts(), "\n"), "目标客户端") {
		t.Fatalf("a fixed-target source shows the target list: %q", tt.Texts())
	}
	if err := tt.Click("开始扫描"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if len(svc.starts) != 1 || svc.lastStart().Target != nil {
		t.Fatalf("the fixed-target start: %+v", svc.starts)
	}
}
