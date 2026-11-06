package ui

import (
	"errors"
	"testing"

	"github.com/LoTwT/youkai/internal/scan"
	"github.com/egoist/mygo/ui"
)

// TestFirstHideHintOnceAndReplay checks the first close's hint, its
// persistence, the second close's silence, and the settings replay.
func TestFirstHideHintOnceAndReplay(t *testing.T) {
	var saved []bool
	hides := 0
	m, q := newTestModel(t, Config{
		Service:          &fakeService{},
		HideToTray:       func() { hides++ },
		SaveTrayHintSeen: func(seen bool) error { saved = append(saved, seen); return nil },
	})
	m.page = pageSettings
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)

	if !m.FirstHideHintDue() {
		t.Fatal("a fresh model already considers the hint seen")
	}
	if !tt.HasText("再看一次托盘图标位置提示") {
		t.Fatal("settings has no tray-hint replay entry")
	}

	m.ShowFirstHideHint()
	tt.Frame()
	for _, want := range []string{
		"youkai 会继续在后台运行",
		"图标在任务栏右侧的通知区域",
		"之后可在 设置 › 常规 重看",
		"知道了",
	} {
		if !tt.HasText(want) {
			t.Fatalf("the first-hide hint does not show %q: %q", want, tt.Texts())
		}
	}
	if err := tt.Click("知道了"); err != nil {
		t.Fatal(err)
	}
	drainAll(t, m, q)
	tt.Frame()
	if m.FirstHideHintDue() {
		t.Fatal("acknowledging did not mark the hint seen")
	}
	if hides != 1 {
		t.Fatalf("hides = %d, want the close path to hide after acknowledgement", hides)
	}
	if len(saved) != 1 || !saved[0] {
		t.Fatalf("saved hint state = %v, want true", saved)
	}
	if tt.HasText("youkai 会继续在后台运行") {
		t.Fatal("the dismissed hint stayed visible")
	}

	// The settings entry reopens the hint without hiding the window.
	if err := tt.Click("再看一次托盘图标位置提示"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("youkai 会继续在后台运行") {
		t.Fatal("the settings replay did not open the hint")
	}
	if err := tt.Click("知道了"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if hides != 1 {
		t.Fatalf("the replay hid the window: hides=%d", hides)
	}
	if tt.HasText("youkai 会继续在后台运行") {
		t.Fatal("the replayed hint stayed visible")
	}
}

// TestFirstHideHintDismissalKeepsItDue checks Escape/backdrop: closing the
// dialog without its button neither acknowledges nor hides.
func TestFirstHideHintDismissalKeepsItDue(t *testing.T) {
	hides := 0
	m, _ := newTestModel(t, Config{Service: &fakeService{}, HideToTray: func() { hides++ }})
	m.ShowFirstHideHint()
	m.trayHintDismissed()
	if !m.FirstHideHintDue() {
		t.Fatal("dismissing acknowledged the hint")
	}
	if hides != 0 {
		t.Fatalf("dismissing hid the window: %d", hides)
	}
}

// TestExitDialogs checks the running and failed wrap-up dialogs.
func TestExitDialogs(t *testing.T) {
	release := make(chan struct{})
	svc := &fakeService{quitFn: func() error {
		<-release
		return nil
	}}
	m, q := newTestModel(t, Config{Service: svc})
	m.page = pageScan
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)
	if m.AllowClose() {
		t.Fatal("the first close was allowed before the wrap-up")
	}
	tt.Frame()
	if !tt.HasText("正在退出：保存结果……") {
		t.Fatalf("the running-exit dialog is missing: %q", tt.Texts())
	}
	close(release)
	drainAll(t, m, q)

	failed := &fakeService{quitFn: func() error { return errors.New("disk full") }}
	m2, q2 := newTestModel(t, Config{Service: failed})
	m2.page = pageScan
	tt2 := ui.NewTester(m2.View, 1020, 700)
	settle(t, m2, q2, tt2)
	if m2.AllowClose() {
		t.Fatal("the failed close was allowed")
	}
	drainAll(t, m2, q2)
	// The failure stays visible with the unsaved result, so the retry can
	// actually retry the save.
	m2.Apply(snap("s1", 2, false, scan.PhaseEnded,
		scan.Counts{Captured: 1, Produced: 1}, 1, false, "磁盘已满"))
	drainAll(t, m2, q2)
	tt2.Frame()
	for _, want := range []string{
		"结果还没有保存，youkai 没有退出",
		"重试保存",
		"复制诊断信息",
	} {
		if !tt2.HasText(want) {
			t.Fatalf("the exit-failure dialog does not show %q: %q", want, tt2.Texts())
		}
	}
	if err := tt2.Click("重试保存"); err != nil {
		t.Fatal(err)
	}
	drainAll(t, m2, q2)
	tt2.Frame()
	if failed.retries != 1 {
		t.Fatalf("retries = %d, want 1", failed.retries)
	}
	if m2.exitFailureDialogOpen {
		t.Fatal("the retry left the dialog open")
	}
}
