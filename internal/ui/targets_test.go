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

	if !tt.HasText("未找到《绝区零》窗口 → 启动游戏并打开驱动仓库页（旧入口为驱动盘仓库页）") {
		t.Fatalf("the not-found state does not show: %q", tt.Texts())
	}
	if !tt.HasText("未找到可扫描的游戏窗口。") {
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

// A unique window is selected without input; only an explicit start sends it.
func TestViewSingleCandidateSelected(t *testing.T) {
	cand := testTarget(0xB0ABE, 42, `C:\Games\ZZZ\ZenlessZoneZero.exe`)
	svc := &fakeService{targets: []app.TargetCandidate{cand}}
	m, q := newTestModel(t, Config{Service: svc, ScanSource: targetSource()})
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)

	label := targetLabel(cand)
	if !tt.HasText(label) {
		t.Fatalf("the unique candidate is not shown: %q", tt.Texts())
	}
	if len(svc.starts) != 0 {
		t.Fatalf("selecting the unique candidate started a scan: %+v", svc.starts)
	}
	if !m.scanStartReady() {
		t.Fatalf("unique candidate is not ready: %s", m.scanStartBlockedReason())
	}
	if err := tt.Click("开始扫描"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if len(svc.starts) != 1 {
		t.Fatalf("starts = %d, want one: %+v", len(svc.starts), svc.starts)
	}
	req := svc.lastStart()
	if req.Target == nil || *req.Target != cand {
		t.Fatalf("the start did not carry the full selected identity: %+v", req.Target)
	}
}

// Process start time, PID, HWND and executable are all part of the choice.
// Once invalidated, repeated refreshes must not silently select a replacement.
func TestSelectedTargetIdentityChangeRequiresChoice(t *testing.T) {
	original := testTarget(1, 42, `C:\Games\ZZZ\ZenlessZoneZero.exe`)
	for _, field := range []string{"process start", "PID", "HWND", "executable", "disappeared"} {
		t.Run(field, func(t *testing.T) {
			replacement := original
			switch field {
			case "process start":
				replacement.ProcessStartedAt = original.ProcessStartedAt.Add(time.Second)
			case "PID":
				replacement.PID++
			case "HWND":
				replacement.Handle++
			case "executable":
				replacement.ExecutablePath = `D:\Games\ZZZ\ZenlessZoneZero.exe`
			}
			svc := &fakeService{targets: []app.TargetCandidate{original}}
			m, q := newTestModel(t, Config{Service: svc, ScanSource: targetSource()})
			tt := ui.NewTester(m.View, 1020, 700)
			settle(t, m, q, tt)
			// Select on the old implementation too, so this pins invalidation independently.
			m.targetChoice = 0
			if field == "disappeared" {
				svc.targets = nil
				m.refreshReadiness()
				settle(t, m, q, tt)
				if m.targetReady() {
					t.Fatal("vanished target remains ready")
				}
			}
			svc.targets = []app.TargetCandidate{replacement}
			for i := 0; i < 2; i++ {
				m.refreshReadiness()
				settle(t, m, q, tt)
				if m.targetReady() {
					t.Fatal("refresh silently selected a replacement instance")
				}
			}
			if !tt.HasText("为什么：原目标已失效，请重新选择游戏窗口。") {
				t.Fatal("replacement requires a choice without explaining the invalidation")
			}
			if err := tt.Click(targetLabel(replacement)); err != nil {
				t.Fatal(err)
			}
			settle(t, m, q, tt)
			if !m.scanStartReady() {
				t.Fatal("explicit replacement selection is not ready")
			}
		})
	}
}

func TestUniqueTargetSelectionKeepsReadinessGuards(t *testing.T) {
	cand := testTarget(1, 42, `C:\Games\ZZZ\ZenlessZoneZero.exe`)
	for _, check := range []app.ReadinessCheck{
		{Item: app.ReadinessAdministrator, Verdict: app.ReadinessUnsatisfied,
			Current: "target=high self=medium", FaultCode: "integrityMismatch"},
		{Item: app.ReadinessGameResolution, Verdict: app.ReadinessUnsatisfied,
			Current: "2560x1440", Required: "1920x1080", FaultCode: "clientTierNotOpened"},
		{Item: app.ReadinessTargetClient, Verdict: app.ReadinessUnsatisfied,
			Current: "minimized=true", FaultCode: "targetHidden"},
	} {
		t.Run(string(check.Item), func(t *testing.T) {
			svc := &fakeService{targets: []app.TargetCandidate{cand}, readiness: readinessWith(check)}
			m, q := newTestModel(t, Config{Service: svc, ScanSource: targetSource()})
			tt := ui.NewTester(m.View, 1020, 700)
			settle(t, m, q, tt)
			if m.chosenTarget() == nil {
				t.Fatal("unique candidate not selected")
			}
			if m.scanStartReady() {
				t.Fatal("selection bypassed a readiness failure")
			}
			if err := tt.Click("开始扫描"); err != nil {
				t.Fatal(err)
			}
			settle(t, m, q, tt)
			if len(svc.starts) != 0 {
				t.Fatal("disabled start issued a request")
			}
			if check.FaultCode == "targetHidden" {
				if !tt.HasText("游戏窗口被最小化或隐藏。") {
					t.Fatal("the minimized window is not named in the start refusal")
				}
				applyReadinessReport(t, m, q, tt, svc, readinessWith(app.ReadinessCheck{
					Item: app.ReadinessTargetClient, Verdict: app.ReadinessSatisfied,
				}))
				if !m.scanStartReady() || m.chosenTarget() == nil || m.chosenTarget().Handle != cand.Handle {
					t.Fatal("restoring the same window should unblock start without changing selection")
				}
			}
		})
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

	if !tt.HasText("找到 2 个窗口 → 请选择要扫描的一个") {
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
	if !tt.HasText("已找到 1 个窗口 → 请确认要扫描的一个") {
		t.Fatalf("the refreshed count does not show: %q", tt.Texts())
	}
}

// TestViewTargetDiscoveryFailure shows the user-facing failure, keeps the
// raw internal error out of the UI, and keeps the start disabled until a
// check succeeds.
func TestViewTargetDiscoveryFailure(t *testing.T) {
	svc := &fakeService{readiness: readinessWith(app.ReadinessCheck{
		Item: app.ReadinessTargetClient, Verdict: app.ReadinessUnknown,
		Current: "enumerationFailed=true", Required: "confirmed=1",
		FaultCode: "observationFailed",
	})}
	m, q := newTestModel(t, Config{Service: svc, ScanSource: targetSource()})
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)

	if !tt.HasText("查找游戏窗口失败") || !tt.HasText("怎么做："+targetDiscoveryFailedAction) {
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

// TestViewElevatedGameSuggestsAdmin is the M1 shape: a visible game-class
// window whose process cannot be opened (the game runs elevated) must ask
// for the same privileges, never claim the game is not running.
func TestViewElevatedGameSuggestsAdmin(t *testing.T) {
	svc := &fakeService{targetsAccessDenied: 1, readiness: readinessWith(
		app.ReadinessCheck{Item: app.ReadinessTargetClient, Verdict: app.ReadinessUnknown, Current: "candidates=0 denied=1 unreadable=0", Required: "confirmed=1"},
		app.ReadinessCheck{Item: app.ReadinessAdministrator, Verdict: app.ReadinessUnsatisfied,
			Current: "target=unreadable self=medium", Required: "target<=self",
			FaultCode: "integrityMismatch", FaultParams: map[string]string{"target": "unreadable", "self": "medium"}},
	)}
	m, q := newTestModel(t, Config{Service: svc, ScanSource: targetSource()})
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)

	if !tt.HasText("无法读取游戏窗口（游戏以管理员身份运行）") {
		t.Fatalf("the elevated-game hint does not show: %q", tt.Texts())
	}
	if tt.HasText("未找到《绝区零》窗口 → 启动游戏并打开驱动仓库页（旧入口为驱动盘仓库页）") {
		t.Fatalf("an unreadable elevated window was reported as not found: %q", tt.Texts())
	}
	if !tt.HasText("为什么："+faultCauseText("integrityMismatch", nil)) || !tt.HasText("退出 youkai") {
		t.Fatalf("the permission row does not carry the administrator action: %q", tt.Texts())
	}
	if err := tt.Click("开始扫描"); err != nil {
		t.Fatal(err)
	}
	drainAll(t, m, q)
	if len(svc.starts) != 0 {
		t.Fatalf("the disabled start issued a request: %+v", svc.starts)
	}
}

// M1′: an already-elevated youkai that still cannot read the game process
// gets the generic diagnostic copy, not the administrator hint and not
// "not found".
func TestViewUnreadableGameShowsGenericCopy(t *testing.T) {
	svc := &fakeService{targetsUnreadable: 1}
	m, q := newTestModel(t, Config{Service: svc, ScanSource: targetSource()})
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)

	if !tt.HasText("无法读取游戏进程信息") || !tt.HasText("怎么做："+targetUnreadableText) {
		t.Fatalf("the unreadable-game copy does not show: %q", tt.Texts())
	}
	if tt.HasText("可能以管理员身份运行") {
		t.Fatalf("an elevated youkai was told to run as administrator: %q", tt.Texts())
	}
	if tt.HasText("未找到《绝区零》窗口 → 启动游戏并打开驱动仓库页（旧入口为驱动盘仓库页）") {
		t.Fatalf("an unreadable game window was reported as not found: %q", tt.Texts())
	}
	if err := tt.Click("开始扫描"); err != nil {
		t.Fatal(err)
	}
	drainAll(t, m, q)
	if len(svc.starts) != 0 {
		t.Fatalf("the disabled start issued a request: %+v", svc.starts)
	}
}

// A readable candidate wins over the access-denied count: the list is
// shown, not the elevated hint.
func TestViewReadableCandidateWinsOverAccessDenied(t *testing.T) {
	cand := testTarget(0xB0ABE, 42, `C:\Games\ZZZ\ZenlessZoneZero.exe`)
	svc := &fakeService{targets: []app.TargetCandidate{cand}, targetsAccessDenied: 1}
	m, q := newTestModel(t, Config{Service: svc, ScanSource: targetSource()})
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)

	if !tt.HasText(targetLabel(cand)) {
		t.Fatalf("the readable candidate is not listed: %q", tt.Texts())
	}
	if tt.HasText("无法读取游戏窗口") {
		t.Fatalf("the denied hint replaced a usable candidate list: %q", tt.Texts())
	}
}

// TestViewStartTargetGoneMapsAndRefreshes covers two review gaps at once:
// the vanished-window failure leaves Chinese copy in the UI—the refreshed
// checklist's current reason, never the English internal error—and the
// failed start refreshes the candidate list automatically.
func TestViewStartTargetGoneMapsAndRefreshes(t *testing.T) {
	cand := testTarget(0xB0ABE, 42, `C:\Games\ZZZ\ZenlessZoneZero.exe`)
	svc := &fakeService{
		targets: []app.TargetCandidate{cand},
		startFn: func(app.StartRequest) (app.StartOutcome, error) {
			return app.StartOutcome{}, app.ErrTargetGone
		},
	}
	m, q := newTestModel(t, Config{Service: svc, ScanSource: targetSource()})
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)
	if err := tt.Click(targetLabel(cand)); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)

	// The window disappears while the start is in flight: the service
	// refuses and the model re-enumerates.
	svc.mu.Lock()
	svc.targets = nil
	svc.mu.Unlock()
	if err := tt.Click("开始扫描"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)

	if !tt.HasText("未找到可扫描的游戏窗口。") {
		t.Fatalf("the current target state does not explain the start: %q", tt.Texts())
	}
	if !tt.HasText("上次开始失败：" + targetGoneText) {
		t.Fatalf("the refused start is not visible as a past event: %q", tt.Texts())
	}
	if tt.HasText("请点“重新检查”后重新选择") {
		t.Fatalf("the past event kept stale action guidance: %q", tt.Texts())
	}
	if tt.HasText("gone or was replaced") {
		t.Fatalf("the raw target-gone error reached the UI: %q", tt.Texts())
	}
	if !tt.HasText("未找到《绝区零》窗口 → 启动游戏并打开驱动仓库页（旧入口为驱动盘仓库页）") {
		t.Fatalf("the candidate list was not refreshed: %q", tt.Texts())
	}
	if m.targetReady() {
		t.Fatal("the start must stay disabled after the confirmed window vanished")
	}
	if svc.readinessCount() < 2 {
		t.Fatalf("readiness checks = %d, want a refresh after the refused start", svc.readinessCount())
	}
}

// TestViewStartDiscoveryFailureKeepsCause: when the start is refused because
// discovery failed and the following re-check finds the confirmed window
// again, the target row reads normally and the current reason is empty. The
// refusal must still show as a past event with its cause, not only the
// recovery action or nothing at all.
func TestViewStartDiscoveryFailureKeepsCause(t *testing.T) {
	cand := testTarget(0xB0ABE, 42, `C:\Games\ZZZ\ZenlessZoneZero.exe`)
	svc := &fakeService{
		targets: []app.TargetCandidate{cand},
		startFn: func(app.StartRequest) (app.StartOutcome, error) {
			return app.StartOutcome{}, app.ErrTargetDiscoveryFailed
		},
	}
	m, q := newTestModel(t, Config{Service: svc, ScanSource: targetSource()})
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)
	if err := tt.Click(targetLabel(cand)); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if err := tt.Click("开始扫描"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)

	// The re-check after the refusal found the same window; the row itself
	// no longer shows a failure.
	if !m.targetReady() {
		t.Fatal("the re-check lost the confirmed window")
	}
	if got := m.scanStartBlockedReason(); got != "" {
		t.Fatalf("a past refusal still explains the usable start: %q", got)
	}
	if got := m.scanStartFailureLine(); got != "上次开始失败："+targetDiscoveryFailedText {
		t.Fatalf("start failure = %q, want the past discovery failure", got)
	}
	if !tt.HasText("上次开始失败：" + targetDiscoveryFailedText) {
		t.Fatalf("the discovery failure's cause is missing beside the start: %q", tt.Texts())
	}
	if !tt.HasText("全部满足") {
		t.Fatalf("the recovered checklist does not read as satisfied: %q", tt.Texts())
	}
}

// TestRefusedStartReasonClearsAfterARecheck pins the two slots: the reason
// beside the start describes the current state, while the refused start is
// a past event that survives the automatic re-check it launched. The past
// event disappears when the user confirms a window again or runs 重新检查.
func TestRefusedStartReasonClearsAfterARecheck(t *testing.T) {
	first := testTarget(0xB0ABE, 42, `C:\Games\ZZZ\ZenlessZoneZero.exe`)
	second := testTarget(0xB0ABF, 42, `C:\Games\ZZZ\ZenlessZoneZero.exe`)

	t.Run("confirm another window", func(t *testing.T) {
		svc := &fakeService{
			targets: []app.TargetCandidate{first},
			startFn: func(app.StartRequest) (app.StartOutcome, error) {
				return app.StartOutcome{}, app.ErrTargetGone
			},
		}
		m, q := newTestModel(t, Config{Service: svc, ScanSource: targetSource()})
		tt := ui.NewTester(m.View, 1020, 700)
		settle(t, m, q, tt)
		if err := tt.Click(targetLabel(first)); err != nil {
			t.Fatal(err)
		}
		settle(t, m, q, tt)

		// The confirmed window disappears while the start is in flight.
		svc.mu.Lock()
		svc.targets = []app.TargetCandidate{second}
		svc.mu.Unlock()
		if err := tt.Click("开始扫描"); err != nil {
			t.Fatal(err)
		}
		settle(t, m, q, tt)
		// The automatic re-check now finds the confirmed window gone, so the
		// checklist's current reason takes the current line; the refusal stays
		// visible as a separate past event.
		if got := m.scanStartBlockedReason(); got != "请先确认要扫描的游戏窗口。" {
			t.Fatalf("the current target state did not take precedence: %q", got)
		}
		if m.startErr != targetGoneText {
			t.Fatalf("the refusal was not remembered: %q", m.startErr)
		}
		if !tt.HasText("上次开始失败：" + targetGoneText) {
			t.Fatalf("the re-check hid the refused start: %q", tt.Texts())
		}

		if err := tt.Click(targetLabel(second)); err != nil {
			t.Fatal(err)
		}
		settle(t, m, q, tt)
		if m.startErr != "" {
			t.Fatalf("the old refusal survived the new confirmation: %q", m.startErr)
		}
		if tt.HasText("上次开始失败") {
			t.Fatalf("the old refusal still shows after the new confirmation: %q", tt.Texts())
		}
		if !m.scanStartReady() {
			t.Fatalf("the start is not offered after the new confirmation: %q", m.scanStartBlockedReason())
		}
	})

	t.Run("change target", func(t *testing.T) {
		cand := testTarget(0xB0ABE, 42, `C:\Games\ZZZ\ZenlessZoneZero.exe`)
		svc := &fakeService{
			targets: []app.TargetCandidate{cand},
			startFn: func(app.StartRequest) (app.StartOutcome, error) {
				return app.StartOutcome{}, app.ErrTargetDiscoveryFailed
			},
		}
		m, q := newTestModel(t, Config{Service: svc, ScanSource: targetSource()})
		tt := ui.NewTester(m.View, 1020, 700)
		settle(t, m, q, tt)
		if err := tt.Click(targetLabel(cand)); err != nil {
			t.Fatal(err)
		}
		settle(t, m, q, tt)
		if err := tt.Click("开始扫描"); err != nil {
			t.Fatal(err)
		}
		settle(t, m, q, tt)
		if !tt.HasText("上次开始失败：" + targetDiscoveryFailedText) {
			t.Fatalf("the refusal did not stay beside the start: %q", tt.Texts())
		}

		if err := tt.Click("更换目标"); err != nil {
			t.Fatal(err)
		}
		settle(t, m, q, tt)
		if m.startErr != "" || tt.HasText("上次开始失败") {
			t.Fatalf("更换目标 did not clear the refusal: %q %q", m.startErr, tt.Texts())
		}
		if got := m.scanStartBlockedReason(); got != "请先确认要扫描的游戏窗口。" {
			t.Fatalf("the changed target did not explain the disabled start: %q", got)
		}
	})

	t.Run("re-check", func(t *testing.T) {
		cand := testTarget(0xB0ABE, 42, `C:\Games\ZZZ\ZenlessZoneZero.exe`)
		svc := &fakeService{
			targets: []app.TargetCandidate{cand},
			startFn: func(app.StartRequest) (app.StartOutcome, error) {
				return app.StartOutcome{}, app.ErrTargetDiscoveryFailed
			},
		}
		m, q := newTestModel(t, Config{Service: svc, ScanSource: targetSource()})
		tt := ui.NewTester(m.View, 1020, 700)
		settle(t, m, q, tt)
		if err := tt.Click(targetLabel(cand)); err != nil {
			t.Fatal(err)
		}
		settle(t, m, q, tt)
		if err := tt.Click("开始扫描"); err != nil {
			t.Fatal(err)
		}
		settle(t, m, q, tt)
		if got := m.scanStartBlockedReason(); got != "" {
			t.Fatalf("a past refusal still explains the usable start: %q", got)
		}
		if !tt.HasText("上次开始失败：" + targetDiscoveryFailedText) {
			t.Fatalf("the refusal did not stay beside the start: %q", tt.Texts())
		}

		if err := tt.Click("重新检查"); err != nil {
			t.Fatal(err)
		}
		settle(t, m, q, tt)
		if m.startErr != "" {
			t.Fatalf("the old refusal survived 重新检查: %q", m.startErr)
		}
		if tt.HasText("上次开始失败") {
			t.Fatalf("the old refusal still shows after 重新检查: %q", tt.Texts())
		}
	})
}

// TestRefusedStartReasonSurvivesAutomaticRechecks pins the complement of
// the user-action rule: focus and the ten-second timer re-judge the state
// but never clear the last refusal, so the user can leave and return
// without losing the explanation. A checklist blocker found by such a pass
// is shown while it lasts, without discarding the refusal.
func TestRefusedStartReasonSurvivesAutomaticRechecks(t *testing.T) {
	clock := &testClock{t: readinessClock()}
	timers := &timerRecorder{}
	cand := testTarget(0xB0ABE, 42, `C:\Games\ZZZ\ZenlessZoneZero.exe`)
	ready := func() app.ReadinessReport {
		report := fakeReadyReport([]app.TargetCandidate{cand}, 0, 0)
		report.CheckedAt = clock.now()
		return report
	}
	svc := &fakeService{
		targets:     []app.TargetCandidate{cand},
		readinessFn: func(*app.TargetCandidate) app.ReadinessReport { return ready() },
		startFn: func(app.StartRequest) (app.StartOutcome, error) {
			return app.StartOutcome{}, app.ErrTargetDiscoveryFailed
		},
	}
	m, q := newTestModel(t, Config{
		Service: svc, ScanSource: targetSource(), Now: clock.now, After: timers.after,
	})
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)
	if err := tt.Click(targetLabel(cand)); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if err := tt.Click("开始扫描"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if got := m.scanStartBlockedReason(); got != "" {
		t.Fatalf("a past refusal still explains the usable start: %q", got)
	}
	if !tt.HasText("上次开始失败：" + targetDiscoveryFailedText) {
		t.Fatalf("the refusal did not stay beside the start: %q", tt.Texts())
	}

	m.WindowFocused()
	settle(t, m, q, tt)
	if !tt.HasText("上次开始失败：" + targetDiscoveryFailedText) {
		t.Fatalf("focus cleared the past failure: %q", tt.Texts())
	}

	clock.add(11 * time.Second)
	timers.fire(t, 0)
	settle(t, m, q, tt)
	if !tt.HasText("上次开始失败：" + targetDiscoveryFailedText) {
		t.Fatalf("the timer cleared the past failure: %q", tt.Texts())
	}

	// The automatic pass found a new checklist blocker: it owns the current
	// line while it lasts, and the past failure stays visible separately.
	svc.mu.Lock()
	svc.readinessFn = func(*app.TargetCandidate) app.ReadinessReport {
		report := ready()
		for i := range report.Checks {
			if report.Checks[i].Item == app.ReadinessDiskSpace {
				report.Checks[i].Verdict = app.ReadinessUnsatisfied
				report.Checks[i].Current = "100000000"
				report.Checks[i].Required = "1073741824"
			}
		}
		return report
	}
	svc.mu.Unlock()
	m.WindowFocused()
	settle(t, m, q, tt)
	if got := m.scanStartBlockedReason(); got != "磁盘空间不足。" {
		t.Fatalf("the checklist blocker did not take precedence: %q", got)
	}
	if m.startErr != targetDiscoveryFailedText ||
		!tt.HasText("上次开始失败："+targetDiscoveryFailedText) {
		t.Fatalf("the checklist blocker discarded the past failure: %q %q", m.startErr, tt.Texts())
	}

	// With the checklist satisfied again, only a user action may clear the
	// remembered refusal.
	svc.mu.Lock()
	svc.readinessFn = func(*app.TargetCandidate) app.ReadinessReport { return ready() }
	svc.mu.Unlock()
	m.WindowFocused()
	settle(t, m, q, tt)
	if got := m.scanStartBlockedReason(); got != "" {
		t.Fatalf("the past refusal still explains the usable start: %q", got)
	}
	if !tt.HasText("上次开始失败：" + targetDiscoveryFailedText) {
		t.Fatalf("the past failure vanished after the checklist cleared: %q", tt.Texts())
	}

	if err := tt.Click("重新检查"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if m.startErr != "" || tt.HasText("上次开始失败") {
		t.Fatalf("重新检查 did not clear the refusal: %q %q", m.startErr, tt.Texts())
	}
}

// Any other start refusal in the installed UI gets the generic Chinese
// past-event fallback; the raw detail stays out of the UI.
func TestViewStartRefusalGenericFallback(t *testing.T) {
	cand := testTarget(0xB0ABE, 42, `C:\Games\ZZZ\ZenlessZoneZero.exe`)
	svc := &fakeService{
		targets: []app.TargetCandidate{cand},
		startFn: func(app.StartRequest) (app.StartOutcome, error) {
			return app.StartOutcome{}, errString("internal detail")
		},
	}
	m, q := newTestModel(t, Config{Service: svc, ScanSource: targetSource()})
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)
	if err := tt.Click(targetLabel(cand)); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if err := tt.Click("开始扫描"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)

	const want = "上次开始失败：详细原因已写入诊断日志；可在“设置 › 诊断”打开诊断目录查看。"
	if !tt.HasText(want) {
		t.Fatalf("the generic start copy does not show: %q", tt.Texts())
	}
	if tt.HasText("internal detail") {
		t.Fatalf("the raw start error reached the UI: %q", tt.Texts())
	}
}

// A readiness pass freezes the confirmed target on the UI thread. Changing
// target while the worker runs must not make the worker read the new index,
// and the late report must not select a window for the choice the user just
// retired: a fresh pass for the current selection owns that state.
func TestReadinessChangeTargetRetiresInFlightPass(t *testing.T) {
	first := testTarget(1, 42, `C:\Games\A\ZenlessZoneZero.exe`)
	second := testTarget(2, 77, `D:\Games\B\ZenlessZoneZero.exe`)
	report := fakeReadyReport([]app.TargetCandidate{first, second}, 0, 0)
	svc := &fakeService{targets: []app.TargetCandidate{first, second}}
	m, q := newTestModel(t, Config{Service: svc, ScanSource: targetSource()})
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)
	if err := tt.Click(targetLabel(first)); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if m.chosenTarget() == nil || m.chosenTarget().Handle != first.Handle {
		t.Fatalf("the first window was not chosen: %+v", m.chosenTarget())
	}

	entered := make(chan *app.TargetCandidate, 4)
	release := make(chan struct{})
	svc.mu.Lock()
	svc.readinessFn = func(target *app.TargetCandidate) app.ReadinessReport {
		entered <- target
		if target != nil && sameTarget(*target, first) {
			<-release
		}
		return report
	}
	svc.mu.Unlock()

	m.refreshReadiness()
	var frozen *app.TargetCandidate
	select {
	case frozen = <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("the readiness pass did not start")
	}
	if frozen == nil || !sameTarget(*frozen, first) {
		t.Fatalf("the worker did not receive the frozen target: %+v", frozen)
	}

	// The user retires the choice while that pass is still in flight.
	if err := tt.Click("更换目标"); err != nil {
		t.Fatal(err)
	}
	if m.targetChoice != -1 || m.chosenTarget() != nil || !m.targetNeedsChoice {
		t.Fatalf("更换目标 did not retire the choice: choice=%d chosen=%+v needs=%v",
			m.targetChoice, m.chosenTarget(), m.targetNeedsChoice)
	}
	close(release)
	settle(t, m, q, tt)

	if m.targetChoice != -1 || m.chosenTarget() != nil {
		t.Fatalf("the late pass restored a retired choice: choice=%d chosen=%+v", m.targetChoice, m.chosenTarget())
	}
	if !m.targetNeedsChoice {
		t.Fatal("the late pass cleared the fresh-choice requirement")
	}
	// The landing pass judged the retired target, so it started a fresh
	// pass with no target instead of applying the stale report.
	select {
	case target := <-entered:
		if target != nil {
			t.Fatalf("the fresh pass received %+v, want no target after 更换目标", target)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the stale landing did not start a pass for the current selection")
	}
}

// A pass that started for the old window must not undo a new confirmation:
// the user's actual 更换目标 and second choice win over the late result.
func TestReadinessLatePassKeepsNewlyConfirmedTarget(t *testing.T) {
	first := testTarget(1, 42, `C:\Games\A\ZenlessZoneZero.exe`)
	second := testTarget(2, 77, `D:\Games\B\ZenlessZoneZero.exe`)
	report := fakeReadyReport([]app.TargetCandidate{first, second}, 0, 0)
	svc := &fakeService{targets: []app.TargetCandidate{first, second}}
	m, q := newTestModel(t, Config{Service: svc, ScanSource: targetSource()})
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)
	if err := tt.Click(targetLabel(first)); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)

	entered := make(chan *app.TargetCandidate, 4)
	release := make(chan struct{})
	svc.mu.Lock()
	svc.readinessFn = func(target *app.TargetCandidate) app.ReadinessReport {
		entered <- target
		if target != nil && sameTarget(*target, first) {
			<-release
		}
		return report
	}
	svc.mu.Unlock()

	m.refreshReadiness()
	select {
	case target := <-entered:
		if target == nil || !sameTarget(*target, first) {
			t.Fatalf("the worker did not receive the frozen first target: %+v", target)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the readiness pass did not start")
	}

	if err := tt.Click("更换目标"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click(targetLabel(second)); err != nil {
		t.Fatalf("choose the second window: %v", err)
	}
	close(release)
	settle(t, m, q, tt)

	if got := m.chosenTarget(); got == nil || !sameTarget(*got, second) {
		t.Fatalf("the late pass replaced the new confirmation: %+v", got)
	}
	if m.targetChoice != 1 || m.targetNeedsChoice {
		t.Fatalf("target choice = %d needsChoice=%v, want the second row", m.targetChoice, m.targetNeedsChoice)
	}
	select {
	case target := <-entered:
		if target == nil || !sameTarget(*target, second) {
			t.Fatalf("the fresh pass received %+v, want the newly confirmed window", target)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the new confirmation did not start a pass for its window")
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
	if strings.Contains(strings.Join(tt.Texts(), "\n"), "请确认要扫描的一个") ||
		strings.Contains(strings.Join(tt.Texts(), "\n"), "请选择要扫描的一个") {
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
