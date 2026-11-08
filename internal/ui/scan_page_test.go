package ui

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/LoTwT/youkai/internal/scan"
	"github.com/egoist/mygo/ui"
)

// readinessClock is the fixed check time the fake report carries; tests
// compare the header against the same instant instead of a hard-coded zone.
func readinessClock() time.Time {
	return time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC)
}

func readinessCheckTimeText() string {
	return readinessClock().Local().Format("15:04") + " 检查"
}

// applyReadinessReport pushes a service report through the model and
// settles the frame it drives, as the asynchronous check does.
func applyReadinessReport(t *testing.T, m *Model, q *queueUpdater, tt *ui.Tester, svc *fakeService, report app.ReadinessReport) {
	t.Helper()
	svc.mu.Lock()
	svc.readiness = report
	svc.mu.Unlock()
	m.refreshReadiness()
	settle(t, m, q, tt)
}

// TestScanF6SkipsDisabledStart pins the PR-05 follow-up: F6 must not park
// the keyboard on the disabled start button.
func TestScanF6SkipsDisabledStart(t *testing.T) {
	svc := &fakeService{readiness: readinessWith(app.ReadinessCheck{
		Item: app.ReadinessAdministrator, Verdict: app.ReadinessUnsatisfied,
		Current: "target=high self=medium", Required: "target<=self",
		FaultCode: "integrityMismatch",
	})}
	m, q := newTestModel(t, Config{Service: svc})
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)
	tt.Key(0, ui.KeyF6)
	tt.Frame()
	if !tt.Focused("退出 youkai") {
		t.Fatalf("F6 left the first action: %q", tt.Texts())
	}
	if tt.Focused("开始扫描") {
		t.Fatal("F6 focused the disabled start button")
	}
}

// TestScanActionBarEstimate shows P-14's known copy and the cleanup
// action reaching the storage section.
func TestScanActionBarEstimate(t *testing.T) {
	svc := &fakeService{estimate: app.ScanDurationEstimate{Median: 14 * time.Minute, Samples: 3}}
	m, q := newTestModel(t, Config{Service: svc})
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)
	if !tt.HasText("预计：约 14 分钟（依据最近 3 次扫描）") {
		t.Fatalf("the estimate does not show its basis: %q", tt.Texts())
	}
}

// TestReadinessCleanupNavigatesToStorage: 磁盘不足's 去清理 opens settings
// with the storage actions focused.
func TestReadinessCleanupNavigatesToStorage(t *testing.T) {
	svc := &fakeService{
		readiness: readinessWith(app.ReadinessCheck{
			Item: app.ReadinessDiskSpace, Verdict: app.ReadinessUnsatisfied,
			Current: "100000000", Required: "1073741824",
		}),
	}
	m, q := newTestModel(t, Config{
		Service:           svc,
		OpenDataDirectory: func() error { return nil },
	})
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)
	if err := tt.Click("去清理"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if m.displayPage() != pageSettings {
		t.Fatalf("去清理 opened page %v, want settings", m.displayPage())
	}
	if !tt.Focused("打开数据目录") {
		t.Fatalf("去清理 does not land on the storage actions: %q", tt.Texts())
	}
}

// TestReadinessUnsatisfiedCopy covers 08 §1.1's unsatisfied wording for
// every row the read-only check can fail. Each case changes one conclusion
// on an otherwise satisfied report, so a failure names the row that broke.
func TestReadinessUnsatisfiedCopy(t *testing.T) {
	admin := app.ReadinessCheck{
		Item: app.ReadinessAdministrator, Verdict: app.ReadinessUnsatisfied,
		Current: "target=high self=medium", Required: "target<=self",
		FaultCode:   "integrityMismatch",
		FaultParams: map[string]string{"target": "high", "self": "medium"},
	}
	cases := []struct {
		name    string
		source  ScanSourceInfo
		report  app.ReadinessReport
		want    []string
		actions []string
	}{
		{
			name:   "target not found",
			source: targetSource(),
			report: readinessWith(app.ReadinessCheck{
				Item: app.ReadinessTargetClient, Verdict: app.ReadinessUnsatisfied,
				Current: "candidates=0 denied=0 unreadable=0", Required: "confirmed=1",
				FaultCode: "targetNotFound",
			}),
			want: []string{
				"未找到《绝区零》窗口 → 启动游戏并打开驱动盘仓库页",
				"等待找到游戏",
			},
		},
		{
			name:   "administrator mismatch",
			source: availableSource(),
			report: readinessWith(admin),
			want: []string{
				"youkai：普通权限 → 需要：与游戏相同（管理员）",
				"为什么：" + faultCauseText("integrityMismatch", nil),
				"怎么做：" + faultActionText("integrityMismatch", nil),
			},
			actions: []string{"退出 youkai"},
		},
		{
			name:   "resolution not opened",
			source: availableSource(),
			report: readinessWith(app.ReadinessCheck{
				Item: app.ReadinessGameResolution, Verdict: app.ReadinessUnsatisfied,
				Current: "2560x1440", Required: "1920x1080",
				FaultCode: "clientTierNotOpened",
				FaultParams: map[string]string{
					"current": "2560x1440", "allowed": "1920x1080",
				},
			}),
			want: []string{
				"2560×1440 → 1920×1080（窗口或无边框）",
				"当前游戏分辨率 2560×1440 是已知但尚未开放的档位",
			},
		},
		{
			name:   "window outside work area",
			source: availableSource(),
			report: readinessWith(app.ReadinessCheck{
				Item: app.ReadinessWindowPosition, Verdict: app.ReadinessUnsatisfied,
				Current: "0,700 2560x1440", Required: "workArea=0,0 2560x1080 monitor=0,0 2560x1440",
				FaultCode: "windowOutsideWorkArea",
				FaultParams: map[string]string{
					"client": "0,700 2560x1440", "workArea": "0,0 2560x1080",
					"allowed": "1920x1080",
				},
			}),
			want: []string{
				"客户区 (0, 700)，2560×1440 → 完整位于屏幕工作区内",
				"游戏窗口超出屏幕或被任务栏遮挡",
			},
		},
		{
			name:   "display scale mismatch",
			source: availableSource(),
			report: readinessWith(app.ReadinessCheck{
				Item: app.ReadinessDisplayScale, Verdict: app.ReadinessUnsatisfied,
				Current: "self=system", Required: "self=perMonitorV2 window=monitor",
				FaultCode: "dpiAwarenessMismatch",
			}),
			want: []string{
				"youkai 的显示缩放支持不完整 → 这是 youkai 自身的问题",
				"这是 youkai 自身的问题，请复制诊断信息反馈。",
			},
			actions: []string{"复制诊断信息"},
		},
		{
			name:   "disk space low",
			source: availableSource(),
			report: readinessWith(app.ReadinessCheck{
				Item: app.ReadinessDiskSpace, Verdict: app.ReadinessUnsatisfied,
				Current: "400000000", Required: "1073741824",
			}),
			want:    []string{"可用 381.5 MB → 至少 1.0 GB", "清理磁盘空间后重新检查。"},
			actions: []string{"去清理"},
		},
		{
			name:   "recognition resources missing",
			source: availableSource(),
			report: readinessWith(app.ReadinessCheck{
				Item: app.ReadinessRecognitionResources, Verdict: app.ReadinessUnsatisfied,
				Current: "missing=model", Required: "files=runtime,model",
			}),
			want: []string{
				"安装不完整 → 需要完整的识别资源",
				"识别资源缺失或损坏",
				"请重新安装 youkai。",
			},
			actions: []string{"复制诊断信息"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &fakeService{}
			m, q := newTestModel(t, Config{Service: svc, ScanSource: tc.source})
			tt := ui.NewTester(m.View, 1020, 700)
			settle(t, m, q, tt)
			applyReadinessReport(t, m, q, tt, svc, tc.report)
			if !tt.HasText(readinessCheckTimeText()) {
				t.Fatalf("the check time does not show: %q", tt.Texts())
			}
			for _, want := range tc.want {
				if !tt.HasText(want) {
					t.Fatalf("the row does not show %q: %q", want, tt.Texts())
				}
			}
			for _, label := range tc.actions {
				// Find matches the whole label, so an explanation that merely
				// mentions the action does not satisfy the test.
				if _, ok := tt.Find(label); !ok {
					t.Fatalf("the row's action %q is not a button: %q", label, tt.Texts())
				}
			}
			if m.scanStartReady() {
				t.Fatal("an unsatisfied checklist still offers the start")
			}
		})
	}
}

// faultActionText exposes the shared guidance action for assertions.
func faultActionText(code string, params map[string]string) string {
	guidance, ok := faultGuidanceFor(code, params)
	if !ok {
		return ""
	}
	return guidance.action
}

// TestReadinessUnknownRowsAreNotAPass: a report without checks (the
// macOS development port) shows unknown rows, never 全部满足. Unknown does
// not disable the start, because only the session's own precheck can judge
// those facts, but the page never calls them satisfied.
func TestReadinessUnknownRowsAreNotAPass(t *testing.T) {
	svc := &fakeService{readiness: app.ReadinessReport{CheckedAt: readinessClock()}}
	m, q := newTestModel(t, Config{Service: svc})
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)
	if !tt.HasText("7 项无法判断") {
		t.Fatalf("unknown rows are not counted: %q", tt.Texts())
	}
	if tt.HasText("全部满足") {
		t.Fatalf("unknown rows were called satisfied: %q", tt.Texts())
	}
}

// TestReadinessOrderAndRepairChecklistPutsFailuresFirst checks that the
// unsatisfied row moves above satisfied rows and the header counts them.
func TestReadinessOrderAndRepairChecklistPutsFailuresFirst(t *testing.T) {
	svc := &fakeService{}
	m, q := newTestModel(t, Config{Service: svc})
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)
	applyReadinessReport(t, m, q, tt, svc, readinessWith(app.ReadinessCheck{
		Item: app.ReadinessDiskSpace, Verdict: app.ReadinessUnsatisfied,
		Current: "100000000", Required: "1073741824",
	}))
	if !tt.HasText("! 1 项需要处理") {
		t.Fatalf("the header does not count the failure: %q", tt.Texts())
	}
	disk, ok := tt.Find("磁盘空间")
	if !ok {
		t.Fatalf("the disk row is missing: %q", tt.Texts())
	}
	target, ok := tt.Find("目标客户端")
	if !ok {
		t.Fatalf("the target row is missing: %q", tt.Texts())
	}
	if disk.Y >= target.Y {
		t.Fatalf("the unsatisfied row at %v is not above the satisfied row at %v", disk, target)
	}
}

// TestReadinessFocusRules covers the first-entry focus: the first
// unsatisfied row's own control, the start button when everything is
// satisfied, and 重新检查 when no row has a primary control.
func TestReadinessFocusRules(t *testing.T) {
	t.Run("first unsatisfied action", func(t *testing.T) {
		svc := &fakeService{readiness: readinessWith(app.ReadinessCheck{
			Item: app.ReadinessAdministrator, Verdict: app.ReadinessUnsatisfied,
			Current: "target=high self=medium", Required: "target<=self",
			FaultCode: "integrityMismatch",
		})}
		m, q := newTestModel(t, Config{Service: svc})
		tt := ui.NewTester(m.View, 1020, 700)
		settle(t, m, q, tt)
		if !tt.Focused("退出 youkai") {
			t.Fatalf("the first actionable row does not take the keyboard: %q", tt.Texts())
		}
	})
	t.Run("all satisfied starts", func(t *testing.T) {
		svc := &fakeService{}
		m, q := newTestModel(t, Config{Service: svc})
		tt := ui.NewTester(m.View, 1020, 700)
		settle(t, m, q, tt)
		if !m.scanStartReady() {
			t.Fatal("a fully satisfied checklist does not offer the start")
		}
		if !tt.Focused("开始扫描") {
			t.Fatalf("the start button does not take the first-entry focus: %q", tt.Texts())
		}
	})
	t.Run("display scale diagnostics before a later action", func(t *testing.T) {
		// The first unsatisfied row owns the focus even when its only control
		// is copying diagnostics; a later cleanup action must not take it
		// (08 §1's first-unsatisfied rule).
		svc := &fakeService{readiness: readinessWith(
			app.ReadinessCheck{
				Item: app.ReadinessDisplayScale, Verdict: app.ReadinessUnsatisfied,
				Current: "self=system", Required: "self=perMonitorV2 window=monitor",
				FaultCode: "dpiAwarenessMismatch",
			},
			app.ReadinessCheck{
				Item: app.ReadinessDiskSpace, Verdict: app.ReadinessUnsatisfied,
				Current: "100000000", Required: "1073741824",
			},
		)}
		m, q := newTestModel(t, Config{Service: svc})
		tt := ui.NewTester(m.View, 1020, 700)
		settle(t, m, q, tt)
		if !tt.Focused("复制诊断信息") {
			t.Fatalf("the display-scale row's diagnostics does not take the focus: %q", tt.Texts())
		}
		if tt.Focused("去清理") {
			t.Fatalf("the focus skipped to the disk row: %q", tt.Texts())
		}
	})

	t.Run("unreadable target before a later action", func(t *testing.T) {
		// The target row has a diagnostics control, but copying it cannot
		// resolve an unreadable process. The row still owns the first-entry
		// focus, so the keyboard falls back to 重新检查 instead of skipping
		// to a later row's cleanup action (08 §1's first-unsatisfied rule).
		svc := &fakeService{
			targetsUnreadable: 1,
			readiness: readinessWith(app.ReadinessCheck{
				Item: app.ReadinessDiskSpace, Verdict: app.ReadinessUnsatisfied,
				Current: "100000000", Required: "1073741824",
			}),
		}
		m, q := newTestModel(t, Config{Service: svc, ScanSource: targetSource()})
		tt := ui.NewTester(m.View, 1020, 700)
		settle(t, m, q, tt)
		if _, ok := tt.Find("去清理"); !ok {
			t.Fatalf("the later cleanup action is missing: %q", tt.Texts())
		}
		if !tt.Focused("重新检查") {
			t.Fatalf("the unreadable target does not keep the focus on refresh: %q", tt.Texts())
		}
		if tt.Focused("去清理") {
			t.Fatalf("the focus skipped to the later cleanup action: %q", tt.Texts())
		}
	})

	t.Run("display scale diagnostics over refresh", func(t *testing.T) {
		// A DPI mismatch is youkai's own problem; re-checking cannot fix it,
		// so the row's diagnostics entry is the place to land.
		svc := &fakeService{readiness: readinessWith(app.ReadinessCheck{
			Item: app.ReadinessDisplayScale, Verdict: app.ReadinessUnsatisfied,
			Current: "self=system", Required: "self=perMonitorV2 window=monitor",
			FaultCode: "dpiAwarenessMismatch",
		})}
		m, q := newTestModel(t, Config{Service: svc})
		tt := ui.NewTester(m.View, 1020, 700)
		settle(t, m, q, tt)
		if !tt.Focused("复制诊断信息") {
			t.Fatalf("the display-scale row's diagnostics does not take the focus: %q", tt.Texts())
		}
		if tt.Focused("重新检查") {
			t.Fatalf("the focus fell back to refresh over the row's diagnostics: %q", tt.Texts())
		}
	})

	t.Run("no action falls back to refresh", func(t *testing.T) {
		svc := &fakeService{readiness: readinessWith(app.ReadinessCheck{
			Item: app.ReadinessGameResolution, Verdict: app.ReadinessUnsatisfied,
			Current: "2560x1440", Required: "1920x1080",
			FaultCode: "clientTierUnsupported",
		})}
		m, q := newTestModel(t, Config{Service: svc})
		tt := ui.NewTester(m.View, 1020, 700)
		settle(t, m, q, tt)
		// The row has no button; the page's only action is the re-check.
		if !tt.Focused("重新检查") {
			t.Fatalf("the refresh entry does not take the fallback focus: %q", tt.Texts())
		}
	})
}

// TestReadinessTargetConfirmationFocus: a unique candidate still needs the
// user's confirmation, and the radio receives the first-entry focus.
func TestReadinessTargetConfirmationFocus(t *testing.T) {
	cand := testTarget(0xB0ABE, 42, `C:\Games\ZZZ\ZenlessZoneZero.exe`)
	svc := &fakeService{targets: []app.TargetCandidate{cand}}
	m, q := newTestModel(t, Config{Service: svc, ScanSource: targetSource()})
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)
	if !tt.HasText("已找到 1 个窗口 → 请确认要扫描的一个") {
		t.Fatalf("the unique candidate is not offered for confirmation: %q", tt.Texts())
	}
	if !tt.Focused(targetLabel(cand)) {
		t.Fatalf("the candidate radio does not take the first-entry focus: %q", tt.Texts())
	}
	if m.scanStartReady() {
		t.Fatal("the start is offered before the candidate is confirmed")
	}
	if !tt.HasText("请先确认要扫描的游戏窗口。") {
		t.Fatalf("the start does not explain the missing confirmation: %q", tt.Texts())
	}
}

// TestScanStartBlockedReasons covers the page-owned and service-owned
// reasons written left of the start button (08 §1.1, R-14).
func TestScanStartBlockedReasons(t *testing.T) {
	t.Run("service save retry", func(t *testing.T) {
		svc := &fakeService{}
		m, q := newTestModel(t, Config{Service: svc})
		tt := ui.NewTester(m.View, 1020, 700)
		settle(t, m, q, tt)
		s := snap("s1", 1, false, scan.PhaseEnded, scan.Counts{}, 0, true, "磁盘已满")
		s.StartBlocked = app.StartBlockedSaveRetry
		applyAndSettle(t, m, q, tt, s)
		if !tt.HasText("上次扫描的结果还没有保存，请先重试保存。") {
			t.Fatalf("the save-retry block does not show: %q", tt.Texts())
		}
		if m.scanStartReady() {
			t.Fatal("the start is offered while a result waits for its retry")
		}
	})
	t.Run("update install", func(t *testing.T) {
		svc := &fakeService{}
		m, q := newTestModel(t, Config{Service: svc})
		tt := ui.NewTester(m.View, 1020, 700)
		settle(t, m, q, tt)
		s := snap("s1", 1, false, scan.PhaseEnded, scan.Counts{}, 0, false, "")
		s.StartBlocked = app.StartBlockedUpdateInstall
		applyAndSettle(t, m, q, tt, s)
		if !tt.HasText("更新正在安装，应用重启前不能开始扫描。") {
			t.Fatalf("the update-install block does not show: %q", tt.Texts())
		}
	})
	t.Run("review save", func(t *testing.T) {
		svc := &fakeService{}
		m, q := newTestModel(t, Config{Service: svc})
		tt := ui.NewTester(m.View, 1020, 700)
		settle(t, m, q, tt)
		m.reviewSaving = true
		tt.Frame()
		if !tt.HasText("正在保存校对修改，完成前不能开始扫描。") {
			t.Fatalf("the review-save block does not show: %q", tt.Texts())
		}
	})
	t.Run("export", func(t *testing.T) {
		svc := &fakeService{}
		m, q := newTestModel(t, Config{Service: svc})
		tt := ui.NewTester(m.View, 1020, 700)
		settle(t, m, q, tt)
		m.exporting = true
		tt.Frame()
		if !tt.HasText("正在导出，完成前不能开始扫描。") {
			t.Fatalf("the export block does not show: %q", tt.Texts())
		}
	})
	t.Run("several rows", func(t *testing.T) {
		svc := &fakeService{}
		m, q := newTestModel(t, Config{Service: svc})
		tt := ui.NewTester(m.View, 1020, 700)
		settle(t, m, q, tt)
		applyReadinessReport(t, m, q, tt, svc, readinessWith(
			app.ReadinessCheck{Item: app.ReadinessAdministrator, Verdict: app.ReadinessUnsatisfied,
				Current: "target=high self=medium", Required: "target<=self", FaultCode: "integrityMismatch"},
			app.ReadinessCheck{Item: app.ReadinessDiskSpace, Verdict: app.ReadinessUnsatisfied,
				Current: "100000000", Required: "1073741824"},
		))
		if !tt.HasText("还有 2 项需要处理") {
			t.Fatalf("the multi-row reason does not count both: %q", tt.Texts())
		}
	})
}

// timerRecorder captures the one-shot timers the page arms.
type timerRecorder struct {
	mu    sync.Mutex
	waits []chan time.Time
}

func (r *timerRecorder) after(time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	r.mu.Lock()
	r.waits = append(r.waits, ch)
	r.mu.Unlock()
	return ch
}

func (r *timerRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.waits)
}

func (r *timerRecorder) fire(t *testing.T, index int) {
	t.Helper()
	r.mu.Lock()
	if index >= len(r.waits) {
		r.mu.Unlock()
		t.Fatalf("timer %d was never armed (%d armed)", index, len(r.waits))
	}
	ch := r.waits[index]
	r.mu.Unlock()
	ch <- time.Now()
}

// testClock is a mutable clock shared by the model and the fake's
// background check; the mutex keeps -race clean.
type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *testClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *testClock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// TestReadinessAutoRecheck covers both triggers: regaining focus and the
// ten-second timer. A second focus while a check runs must not stack one.
func TestReadinessAutoRecheck(t *testing.T) {
	clock := &testClock{t: time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC)}
	timers := &timerRecorder{}
	svc := &fakeService{}
	svc.readinessFn = func(*app.TargetCandidate) app.ReadinessReport {
		report := fakeReadyReport(nil, 0, 0)
		report.CheckedAt = clock.now()
		return report
	}
	m, q := newTestModel(t, Config{Service: svc, Now: clock.now, After: timers.after})
	tt := ui.NewTester(m.View, 700, 700)
	settle(t, m, q, tt)
	if got := svc.readinessCount(); got != 1 {
		t.Fatalf("initial readiness checks = %d, want 1", got)
	}
	// The check completes before the ten-second timer fires.
	if timers.count() == 0 {
		t.Fatal("the readiness timer was not armed")
	}
	clock.add(11 * time.Second)
	timers.fire(t, 0)
	settle(t, m, q, tt)
	if got := svc.readinessCount(); got != 2 {
		t.Fatalf("stale timer checks = %d, want 2", got)
	}

	// Focus re-checks immediately; a second focus during the same pass does
	// not start a third call.
	m.WindowFocused()
	m.WindowFocused()
	settle(t, m, q, tt)
	if got := svc.readinessCount(); got != 3 {
		t.Fatalf("focus checks = %d, want one new check", got)
	}
}

// TestScanStageActivationAndCountdown covers the preparing stage's
// activation results and the countdown text.
func TestScanStageActivationAndCountdown(t *testing.T) {
	now := time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC)
	svc := &fakeService{}
	m, q := newTestModel(t, Config{Service: svc, Now: func() time.Time { return now }})
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)
	preparing := snap("s1", 1, true, scan.PhasePreparing, scan.Counts{}, 0, true, "")
	preparing.Scan.PrepareDeadline = now.Add(30 * time.Second)
	preparing.Scan.Activation = scan.ActivationActivated
	applyAndSettle(t, m, q, tt, preparing)
	if !tt.HasText("30 秒") || !tt.HasText("已把游戏切到前台") {
		t.Fatalf("the activated preparing stage does not show: %q", tt.Texts())
	}
	refused := preparing
	refused.Revision = 2
	refused.Scan.Revision = 2
	refused.Scan.Activation = scan.ActivationRefused
	applyAndSettle(t, m, q, tt, refused)
	if !tt.HasText("未能切换，请自己切换到游戏") {
		t.Fatalf("the refused activation does not show: %q", tt.Texts())
	}
}

// TestScanPageActionBarFitsMinimumWindow measures the 960×640 contract:
// the action bar's start button is fully above the window's bottom edge at
// 100%, 125% and 150% text. The last two are the T-08 shapes; a real
// Windows round still confirms the platform's own text metrics.
func TestScanPageActionBarFitsMinimumWindow(t *testing.T) {
	for _, scale := range []float32{1, 1.25, 1.5} {
		t.Run(fmt.Sprintf("scale_%g", scale), func(t *testing.T) {
			svc := &fakeService{}
			m, q := newTestModel(t, Config{Service: svc})
			tt := ui.NewTester(m.View, 960, 640)
			tt.SetPreferences(ui.Preferences{TextScale: scale, ReduceMotion: true})
			settle(t, m, q, tt)
			for _, want := range []string{"中途停止 " + shortcutStopScan, "点开始后 30 秒内保持游戏在前台，扫描期间不要操作游戏"} {
				if !tt.HasText(want) {
					t.Fatalf("the bar lost %q at scale %g: %q", want, scale, tt.Texts())
				}
			}
			button, ok := tt.Find("开始扫描")
			if !ok {
				t.Fatalf("the start button is missing at scale %g", scale)
			}
			bottom := button.Y + button.H
			t.Logf("scale %g: start button bottom = %.1f DIP, margin %.1f", scale, bottom, 640-bottom)
			if bottom > 640 {
				t.Fatalf("the start button bottom %.1f exceeds the 960×640 window at scale %g", bottom, scale)
			}
		})
	}
}

// TestScanPreparationFailureReturnsToChecklist is the plan-outside fix: a
// session that ended before input opened stays on the scan page and says
// 没有开始 instead of posing as a zero-item scan.
func TestScanPreparationFailureReturnsToChecklist(t *testing.T) {
	svc := &fakeService{}
	m, q := newTestModel(t, Config{Service: svc})
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)
	applyAndSettle(t, m, q, tt, snap("s1", 1, true, scan.PhasePreparing, scan.Counts{}, 0, true, ""))
	failed := snap("s1", 2, false, scan.PhaseEnded, scan.Counts{}, 0, false, "")
	failed.Scan.InputOpened = false
	failed.Scan.End = scan.EndFailed
	failed.Scan.Trigger = &scan.Trigger{
		Kind: scan.TriggerFault, Code: "integrityMismatch", Phase: scan.PhasePreparing,
		Params: map[string]string{"target": "high", "self": "medium"},
	}
	applyAndSettle(t, m, q, tt, failed)
	if m.displayPage() != pageScan {
		t.Fatalf("a preparation refusal opened page %v, want the checklist", m.displayPage())
	}
	if !tt.HasText("没有开始：" + faultCauseText("integrityMismatch", failed.Scan.Trigger.Params)) {
		t.Fatalf("the refusal does not stay on the checklist: %q", tt.Texts())
	}
	if !tt.HasText("管理员权限") {
		t.Fatalf("the failed row is missing: %q", tt.Texts())
	}
	// The refusal triggers a read-only pass; that pass replaces the
	// presentation marker on rows it can judge, so a now-satisfied row
	// must not keep the start disabled (round-1 decision).
	if !m.scanStartReady() {
		t.Fatal("the re-checked row still blocks the next start")
	}
}

// TestScanHotkeyFailurePromotesTheHotkeyRow is Q-B: the hotkey is only
// tested on start, and a failure returns to the checklist with the row on
// top.
func TestScanHotkeyFailurePromotesTheHotkeyRow(t *testing.T) {
	// Another unsatisfied row sorts before the hotkey in the checklist
	// order; only the failure pin puts the hotkey above it.
	svc := &fakeService{readiness: readinessWith(app.ReadinessCheck{
		Item: app.ReadinessAdministrator, Verdict: app.ReadinessUnsatisfied,
		Current: "target=high self=medium", Required: "target<=self",
		FaultCode: "integrityMismatch",
	})}
	m, q := newTestModel(t, Config{Service: svc})
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)
	applyAndSettle(t, m, q, tt, snap("s1", 1, true, scan.PhasePreparing, scan.Counts{}, 0, true, ""))
	failed := snap("s1", 2, false, scan.PhaseEnded, scan.Counts{}, 0, false, "")
	failed.Scan.InputOpened = false
	failed.Scan.End = scan.EndFailed
	failed.Scan.Trigger = &scan.Trigger{
		Kind: scan.TriggerFault, Code: "hotkeyUnavailable", Phase: scan.PhasePreparing,
	}
	applyAndSettle(t, m, q, tt, failed)
	if !tt.HasText("没有开始：停止热键已被其他程序占用") {
		t.Fatalf("the hotkey failure does not explain the start refusal: %q", tt.Texts())
	}
	hotkey, ok := tt.Find("停止热键")
	if !ok {
		t.Fatalf("the hotkey row is missing: %q", tt.Texts())
	}
	// Compare against a satisfied row, not the other post-start row: the two
	// post-start rows are already in this order without the promotion.
	target, ok := tt.Find("目标客户端")
	if !ok || hotkey.Y >= target.Y {
		t.Fatalf("the hotkey row is not promoted: hotkey %v target %v", hotkey, target)
	}
	// An unsatisfied row earlier in the checklist order must not outrank the
	// failure; this is what the pin mapping itself decides.
	admin, ok := tt.Find("管理员权限")
	if !ok || hotkey.Y >= admin.Y {
		t.Fatalf("another failure outranks the hotkey: hotkey %v admin %v", hotkey, admin)
	}
}

// applyPreparationFailure drives one preparing session to a refusal before
// input opened, as the service reports a preparation-stage fault.
func applyPreparationFailure(t *testing.T, m *Model, q *queueUpdater, tt *ui.Tester, code string, params map[string]string) {
	t.Helper()
	applyAndSettle(t, m, q, tt, snap("s1", 1, true, scan.PhasePreparing, scan.Counts{}, 0, true, ""))
	failed := snap("s1", 2, false, scan.PhaseEnded, scan.Counts{}, 0, false, "")
	failed.Scan.InputOpened = false
	failed.Scan.End = scan.EndFailed
	failed.Scan.Trigger = &scan.Trigger{
		Kind: scan.TriggerFault, Code: code, Phase: scan.PhasePreparing, Params: params,
	}
	applyAndSettle(t, m, q, tt, failed)
}

// TestPreparationFailureDoesNotBlockRestart pins the round-1 fix: the
// failure record only pins and explains the row; it never keeps 开始扫描
// disabled after the read-only check says the row holds. The hotkey and
// game page rows stay pinned until the next start because only a start can
// check them.
func TestPreparationFailureDoesNotBlockRestart(t *testing.T) {
	t.Run("hotkey", func(t *testing.T) {
		svc := &fakeService{}
		m, q := newTestModel(t, Config{Service: svc})
		tt := ui.NewTester(m.View, 1020, 700)
		settle(t, m, q, tt)
		applyPreparationFailure(t, m, q, tt, "hotkeyUnavailable", nil)
		if !m.scanStartReady() {
			t.Fatal("the hotkey failure still blocks the next start")
		}
		if !tt.HasText("没有开始：停止热键已被其他程序占用") {
			t.Fatalf("the hotkey failure lost its explanation: %q", tt.Texts())
		}
		if !tt.HasText("停止热键已被其他程序占用") {
			t.Fatalf("the hotkey row lost its failure value: %q", tt.Texts())
		}
	})

	t.Run("integrity", func(t *testing.T) {
		svc := &fakeService{}
		m, q := newTestModel(t, Config{Service: svc})
		tt := ui.NewTester(m.View, 1020, 700)
		settle(t, m, q, tt)
		params := map[string]string{"target": "high", "self": "medium"}
		applyPreparationFailure(t, m, q, tt, "integrityMismatch", params)
		if !m.scanStartReady() {
			t.Fatal("the integrity failure still blocks the next start after the re-check passed")
		}
		if !tt.HasText("没有开始：" + faultCauseText("integrityMismatch", params)) {
			t.Fatalf("the integrity failure lost its explanation: %q", tt.Texts())
		}
	})

	t.Run("prepare timeout", func(t *testing.T) {
		svc := &fakeService{}
		m, q := newTestModel(t, Config{Service: svc})
		tt := ui.NewTester(m.View, 1020, 700)
		settle(t, m, q, tt)
		applyPreparationFailure(t, m, q, tt, "prepareTimeout", nil)
		if !m.scanStartReady() {
			t.Fatal("the game-page failure still blocks the next start")
		}
		if !tt.HasText("没有开始：30 秒内游戏没有就绪") {
			t.Fatalf("the timeout lost its explanation: %q", tt.Texts())
		}
	})

	t.Run("target lost", func(t *testing.T) {
		original := testTarget(0xB0ABE, 42, `C:\Games\ZZZ\ZenlessZoneZero.exe`)
		replacement := testTarget(0xB0ABF, 43, `C:\Games\ZZZ\ZenlessZoneZero.exe`)
		svc := &fakeService{targets: []app.TargetCandidate{original}}
		m, q := newTestModel(t, Config{Service: svc, ScanSource: targetSource()})
		tt := ui.NewTester(m.View, 1020, 700)
		settle(t, m, q, tt)
		if err := tt.Click(targetLabel(original)); err != nil {
			t.Fatal(err)
		}
		settle(t, m, q, tt)

		// The confirmed window is gone; the re-check after the refusal finds
		// the restarted game and must offer it again.
		svc.mu.Lock()
		svc.targets = []app.TargetCandidate{replacement}
		svc.mu.Unlock()
		applyPreparationFailure(t, m, q, tt, "targetLost", nil)
		if !tt.HasText("已找到 1 个窗口 → 请确认要扫描的一个") {
			t.Fatalf("the candidate list did not return: %q", tt.Texts())
		}
		if !tt.HasText(targetLabel(replacement)) {
			t.Fatalf("the replacement window is not listed: %q", tt.Texts())
		}
		if m.scanStartReady() {
			t.Fatal("the start is offered before the replacement is confirmed")
		}
		if !tt.HasText("请先确认要扫描的游戏窗口。") {
			t.Fatalf("the start does not explain the missing confirmation: %q", tt.Texts())
		}
		if err := tt.Click(targetLabel(replacement)); err != nil {
			t.Fatal(err)
		}
		settle(t, m, q, tt)
		if !m.scanStartReady() {
			t.Fatal("confirming the replacement does not allow a restart")
		}
	})
}

// TestReadinessLaterRecheckKeepsFocus: only the first completed pass may
// move the keyboard. A background pass that reorders rows and builds a new
// control must not take the focus away from 重新检查.
func TestReadinessLaterRecheckKeepsFocus(t *testing.T) {
	svc := &fakeService{readiness: readinessWith(app.ReadinessCheck{
		Item: app.ReadinessGameResolution, Verdict: app.ReadinessUnsatisfied,
		Current: "2560x1440", Required: "1920x1080",
		FaultCode: "clientTierUnsupported",
	})}
	m, q := newTestModel(t, Config{Service: svc})
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)
	if !tt.Focused("重新检查") {
		t.Fatalf("the first pass did not focus the fallback action: %q", tt.Texts())
	}

	applyReadinessReport(t, m, q, tt, svc, readinessWith(app.ReadinessCheck{
		Item: app.ReadinessDiskSpace, Verdict: app.ReadinessUnsatisfied,
		Current: "100000000", Required: "1073741824",
	}))
	if !tt.Focused("重新检查") {
		t.Fatalf("a later re-check moved the focus away from refresh: %q", tt.Texts())
	}
	if tt.Focused("去清理") {
		t.Fatalf("the re-check's new row took the focus: %q", tt.Texts())
	}
}

// TestTargetUnavailableStatesBlockStart covers the P-05 and M1′ target
// states: each one counts as needing handling, disables the start with a
// non-empty reason, and leaves the focus on an actionable control.
func TestTargetUnavailableStatesBlockStart(t *testing.T) {
	t.Run("access denied without elevation", func(t *testing.T) {
		svc := &fakeService{
			targetsAccessDenied: 1,
			readiness: readinessWith(app.ReadinessCheck{
				Item: app.ReadinessAdministrator, Verdict: app.ReadinessUnsatisfied,
				Current: "target=unreadable self=medium", Required: "target<=self",
				FaultCode:   "integrityMismatch",
				FaultParams: map[string]string{"target": "unreadable", "self": "medium"},
			}),
		}
		m, q := newTestModel(t, Config{Service: svc, ScanSource: targetSource()})
		tt := ui.NewTester(m.View, 1020, 700)
		settle(t, m, q, tt)
		if m.scanStartReady() {
			t.Fatal("the access-denied target still offers the start")
		}
		if got := m.scanStartBlockedReason(); got != "youkai 需要与游戏相同的管理员权限。" {
			t.Fatalf("start reason = %q, want the elevation cause", got)
		}
		if !tt.HasText("无法读取游戏窗口（游戏以管理员身份运行）") {
			t.Fatalf("the target row does not explain the elevation case: %q", tt.Texts())
		}
		if !tt.Focused("退出 youkai") {
			t.Fatalf("the elevation action does not take the focus: %q", tt.Texts())
		}
		// The full elevation passage belongs to the permission row only.
		action := faultActionText("integrityMismatch", nil)
		mentions := 0
		for _, text := range tt.Texts() {
			if strings.Contains(text, action) {
				mentions++
			}
		}
		if mentions != 1 {
			t.Fatalf("the elevation passage appears %d times, want once: %q", mentions, tt.Texts())
		}
	})

	t.Run("access denied while elevated", func(t *testing.T) {
		// The game denies its process while youkai already matches the
		// game's level (the default report says medium/medium), so this is
		// not the elevation mismatch: the row needs handling, a short
		// reason, and a diagnostics action.
		svc := &fakeService{targetsAccessDenied: 1}
		m, q := newTestModel(t, Config{Service: svc, ScanSource: targetSource()})
		tt := ui.NewTester(m.View, 1020, 700)
		settle(t, m, q, tt)
		if m.scanStartReady() {
			t.Fatal("the access-denied target still offers the start")
		}
		if got := m.scanStartBlockedReason(); got != "无法读取游戏窗口。" {
			t.Fatalf("start reason = %q, want the denied target", got)
		}
		if !tt.HasText("无法读取游戏窗口") || !tt.HasText("怎么做："+targetUnreadableText) {
			t.Fatalf("the denied target does not explain the failure: %q", tt.Texts())
		}
		if tt.HasText("无法读取游戏窗口（游戏以管理员身份运行）") {
			t.Fatalf("an elevated youkai was told the game is elevated: %q", tt.Texts())
		}
		if _, ok := tt.Find("复制诊断信息"); !ok {
			t.Fatalf("the denied target has no diagnostics action: %q", tt.Texts())
		}
		if !tt.Focused("重新检查") {
			t.Fatalf("the re-check action does not take the focus: %q", tt.Texts())
		}
	})

	t.Run("process unreadable", func(t *testing.T) {
		svc := &fakeService{targetsUnreadable: 1}
		m, q := newTestModel(t, Config{Service: svc, ScanSource: targetSource()})
		tt := ui.NewTester(m.View, 1020, 700)
		settle(t, m, q, tt)
		if m.scanStartReady() {
			t.Fatal("the unreadable target still offers the start")
		}
		if got := m.scanStartBlockedReason(); got != "无法读取游戏进程信息。" {
			t.Fatalf("start reason = %q, want the unreadable target", got)
		}
		if !tt.HasText("! 1 项需要处理") {
			t.Fatalf("the unreadable target is not counted: %q", tt.Texts())
		}
		if _, ok := tt.Find("复制诊断信息"); !ok {
			t.Fatalf("the unreadable target has no diagnostics action: %q", tt.Texts())
		}
		if !tt.Focused("重新检查") {
			t.Fatalf("the re-check action does not take the focus: %q", tt.Texts())
		}
	})

	t.Run("discovery failed", func(t *testing.T) {
		svc := &fakeService{readiness: readinessWith(app.ReadinessCheck{
			Item: app.ReadinessTargetClient, Verdict: app.ReadinessUnknown,
			Current: "enumerationFailed=true", Required: "confirmed=1",
			FaultCode: "observationFailed",
		})}
		m, q := newTestModel(t, Config{Service: svc, ScanSource: targetSource()})
		tt := ui.NewTester(m.View, 1020, 700)
		settle(t, m, q, tt)
		if m.scanStartReady() {
			t.Fatal("the failed discovery still offers the start")
		}
		if got := m.scanStartBlockedReason(); got != "查找游戏窗口失败。" {
			t.Fatalf("start reason = %q, want the discovery failure", got)
		}
		if !tt.HasText("! 1 项需要处理") {
			t.Fatalf("the failed discovery is not counted: %q", tt.Texts())
		}
		if _, ok := tt.Find("复制诊断信息"); !ok {
			t.Fatalf("the failed discovery has no diagnostics action: %q", tt.Texts())
		}
		if !tt.Focused("重新检查") {
			t.Fatalf("the re-check action does not take the focus: %q", tt.Texts())
		}
	})
}

// TestScanStageCopy covers 08 §1.2's capturing and wrap-up copy, the
// safety note about clicking the window, and the save-failure block.
func TestScanStageCopy(t *testing.T) {
	t.Run("capturing", func(t *testing.T) {
		svc := &fakeService{}
		m, q := newTestModel(t, Config{Service: svc})
		tt := ui.NewTester(m.View, 1020, 700)
		settle(t, m, q, tt)
		applyAndSettle(t, m, q, tt, snap("s1", 1, true, scan.PhaseCapturing,
			scan.Counts{Captured: 12}, 0, true, ""))
		if !tt.HasText("正在扫描 · 已采集 12 件") {
			t.Fatalf("the capturing stage does not show its count: %q", tt.Texts())
		}
		if !tt.HasText("点击窗口本身也会让游戏失焦而停止") {
			t.Fatalf("the capturing stage lost the safety note: %q", tt.Texts())
		}
	})

	t.Run("finalizing remaining", func(t *testing.T) {
		svc := &fakeService{}
		m, q := newTestModel(t, Config{Service: svc})
		tt := ui.NewTester(m.View, 1020, 700)
		settle(t, m, q, tt)
		applyAndSettle(t, m, q, tt, snap("s1", 1, true, scan.PhaseFinalizing,
			scan.Counts{Queued: 3, Running: 1}, 0, true, ""))
		if !tt.HasText("已停止操作游戏，正在识别剩余 4 件并保存") {
			t.Fatalf("the finalizing stage does not show the remaining count: %q", tt.Texts())
		}
	})

	t.Run("finalizing saving", func(t *testing.T) {
		svc := &fakeService{}
		m, q := newTestModel(t, Config{Service: svc})
		tt := ui.NewTester(m.View, 1020, 700)
		settle(t, m, q, tt)
		applyAndSettle(t, m, q, tt, snap("s1", 1, true, scan.PhaseFinalizing,
			scan.Counts{}, 0, true, ""))
		if !tt.HasText("已停止操作游戏，正在保存结果") {
			t.Fatalf("the saving stage does not show its copy: %q", tt.Texts())
		}
	})

	t.Run("save failure", func(t *testing.T) {
		svc := &fakeService{}
		m, q := newTestModel(t, Config{Service: svc})
		tt := ui.NewTester(m.View, 1020, 700)
		settle(t, m, q, tt)
		applyAndSettle(t, m, q, tt, snap("s1", 1, false, scan.PhaseFinalizing,
			scan.Counts{Captured: 10}, 2, false, "磁盘已满"))
		for _, want := range []string{
			"扫描结果还没有保存",
			"磁盘已满",
			"此时不能开始新扫描、不能安装更新，退出会被拒绝。",
			"重试保存",
		} {
			if !tt.HasText(want) {
				t.Fatalf("the save-failure block lost %q: %q", want, tt.Texts())
			}
		}
	})
}

// TestResultsTitleForRefusedStart: a history or live record that never
// opened input says 没有开始 in the results title too.
func TestResultsTitleForRefusedStart(t *testing.T) {
	view := BuildRecordView("s1", &record.Recovery{
		Checkpoint: record.Checkpoint{
			Session: record.SessionShell{
				SessionID: "s1",
				Termination: &record.Termination{
					Kind: record.TerminationFailed, Code: "integrityMismatch",
					Phase: record.PhasePreparing,
				},
			},
		},
	}, true)
	m := &Model{rec: view}
	if got := m.resultsTitle(); got != "结果：没有开始（已保存 0 条）" {
		t.Fatalf("results title = %q", got)
	}
}
