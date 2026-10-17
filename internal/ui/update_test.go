package ui

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/scan"
	"github.com/egoist/mygo/ui"
)

// errInstallBoom is the canned install failure.
var errInstallBoom = errors.New("install boom")

// fakeUpdates is the update-source double: Enabled is canned, Check comes
// from a function the test swaps per step. asked counts the checks that
// reached the source, so tests can tell "never asked" from "asked and
// answered".
type fakeUpdates struct {
	enabled bool
	asked   int
	check   func(ctx context.Context) (UpdateHandle, error)
}

func (f *fakeUpdates) Enabled() bool { return f.enabled }

func (f *fakeUpdates) Check(ctx context.Context) (UpdateHandle, error) {
	f.asked++
	if f.check == nil {
		return nil, nil
	}
	return f.check(ctx)
}

// fakeUpdateHandle is one found update: how often Install ran, and what
// one run does (nil installs cleanly). The model's install runs on its
// background work, so the counts are read only after settle.
type fakeUpdateHandle struct {
	version, notes string
	install        func(progress func(downloaded, total int64)) error
	installs       int
}

func (h *fakeUpdateHandle) Version() string { return h.version }
func (h *fakeUpdateHandle) Notes() string   { return h.notes }

func (h *fakeUpdateHandle) Install(ctx context.Context, progress func(downloaded, total int64)) error {
	h.installs++
	if h.install != nil {
		return h.install(progress)
	}
	return nil
}

// TestViewUpdateDisabledBuild covers the builds without updates — the
// Package workflow's test packages, development runs: the area explains
// the state in plain words, treats it as no error, and offers no check.
func TestViewUpdateDisabledBuild(t *testing.T) {
	src := &fakeUpdates{enabled: false}
	m, q := newTestModel(t, Config{Service: &fakeService{}, Updates: src})
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)

	if !tt.HasText("当前构建未启用自动更新。") {
		t.Error("a build without updates does not say so")
	}
	if err := tt.Click("检查更新"); err == nil {
		t.Error("a build without updates still offers a check")
	}
	// The startup path asks anyway; the disabled source must ignore it
	// rather than fail.
	m.CheckUpdates()
	settle(t, m, q, tt)
	if src.check != nil {
		t.Error("the disabled source was asked to check")
	}
	if !tt.HasText("当前构建未启用自动更新。") {
		t.Error("the disabled note changed after the ignored check")
	}
}

// TestViewUpdateCheckFlow drives the check states: the found version with
// its notes, the up-to-date case, and the honest failure with the button
// still offering a re-check.
func TestViewUpdateCheckFlow(t *testing.T) {
	found := &fakeUpdateHandle{version: "0.0.2", notes: "修复若干问题。"}
	src := &fakeUpdates{
		enabled: true,
		check: func(ctx context.Context) (UpdateHandle, error) {
			return found, nil
		},
	}
	m, q := newTestModel(t, Config{Service: &fakeService{}, Updates: src})
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)

	// The startup check the entry point triggers.
	m.CheckUpdates()
	settle(t, m, q, tt)
	if !tt.HasText("发现新版本 0.0.2。") {
		t.Error("the found version does not show")
	}
	if !tt.HasText("修复若干问题。") {
		t.Error("the found version's notes do not show")
	}

	// Up to date on the next check.
	src.check = func(ctx context.Context) (UpdateHandle, error) { return nil, nil }
	if err := tt.Click("检查更新"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if !tt.HasText("已是最新版本。") {
		t.Error("the up-to-date state does not show")
	}

	// A failed check shows the error as it is and keeps the button.
	src.check = func(ctx context.Context) (UpdateHandle, error) {
		return nil, context.DeadlineExceeded
	}
	if err := tt.Click("检查更新"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if !tt.HasText("检查更新失败：context deadline exceeded") {
		t.Error("the check failure does not show as it is")
	}
	if err := tt.Click("检查更新"); err != nil {
		t.Error("the check button disappeared after a failure")
	}
}

// TestViewUpdateShowsCurrentVersion covers the update area's running
// version line: it names the build in every state — updates enabled or
// not — because that is where a user checks what they run.
func TestViewUpdateShowsCurrentVersion(t *testing.T) {
	for _, tc := range []struct {
		name    string
		enabled bool
	}{
		{name: "enabled", enabled: true},
		{name: "disabled", enabled: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := &fakeUpdates{enabled: tc.enabled}
			m, q := newTestModel(t, Config{
				Service:    &fakeService{},
				Updates:    src,
				AppVersion: "0.0.2",
			})
			tt := ui.NewTester(m.View, 1020, 700)
			settle(t, m, q, tt)
			if !tt.HasText("当前版本 0.0.2") {
				t.Error("the running version does not show in the update area")
			}
			m.CheckUpdates()
			settle(t, m, q, tt)
			if !tt.HasText("当前版本 0.0.2") {
				t.Error("the running version disappeared after a check")
			}
		})
	}
}

// TestStartUpdateChecksSchedule drives the update-check schedule with an
// injected wait: the startup check runs at once, the first periodic wait
// asks for the 24-hour interval, and firing it runs the next check
// through the UI thread like every background result. Builds without
// updates start nothing at all.
func TestStartUpdateChecksSchedule(t *testing.T) {
	checks := 0
	src := &fakeUpdates{
		enabled: true,
		check: func(ctx context.Context) (UpdateHandle, error) {
			checks++
			return nil, nil
		},
	}
	// The controllable wait: every call records the interval it was asked
	// for and hands back a channel only the test fires.
	type tick struct {
		interval time.Duration
		ch       chan time.Time
	}
	ticks := make(chan tick, 4)
	wait := func(interval time.Duration) <-chan time.Time {
		ch := make(chan time.Time)
		ticks <- tick{interval: interval, ch: ch}
		return ch
	}
	m, q := newTestModel(t, Config{
		Service:         &fakeService{},
		Updates:         src,
		UpdateCheckWait: wait,
	})
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)

	m.StartUpdateChecks()
	settle(t, m, q, tt)
	if checks != 1 {
		t.Fatalf("the startup check did not run: %d checks", checks)
	}
	select {
	case tk := <-ticks:
		if tk.interval != updateCheckInterval {
			t.Fatalf("the periodic wait asked for %s, want %s", tk.interval, updateCheckInterval)
		}
		// Fire the tick: the check it schedules must land through the
		// queued updater, on the UI thread, not from the ticking
		// goroutine.
		tk.ch <- time.Now()
	default:
		t.Fatal("no periodic wait was scheduled")
	}
	// The send above only proves the scheduling goroutine received the
	// tick; it queues the check right after, then asks for its next wait.
	// Waiting for that next wait therefore proves the check is queued, so
	// settling cannot run ahead of it. Taking it here also leaves ticks
	// empty for the disabled model below.
	select {
	case <-ticks:
	case <-time.After(5 * time.Second):
		t.Fatal("the schedule did not ask for its next wait after a tick")
	}
	settle(t, m, q, tt)
	if checks != 2 {
		t.Fatalf("the periodic check did not run: %d checks", checks)
	}

	// A build without updates starts nothing: no check, no wait.
	off := &fakeUpdates{enabled: false}
	m2, q2 := newTestModel(t, Config{
		Service:         &fakeService{},
		Updates:         off,
		UpdateCheckWait: wait,
	})
	tt2 := ui.NewTester(m2.View, 1020, 700)
	settle(t, m2, q2, tt2)
	m2.StartUpdateChecks()
	settle(t, m2, q2, tt2)
	if off.asked != 0 {
		t.Fatalf("a disabled build asked the source %d times", off.asked)
	}
	select {
	case <-ticks:
		t.Error("a disabled build scheduled a periodic wait")
	default:
	}
}

// TestDevAutoInstall drives the rehearsal switch: an update a check found
// installs on its own and fires the restart hook; the install guards
// still apply — a scan holding the slot keeps the update waiting until the
// session settles.
func TestDevAutoInstall(t *testing.T) {
	found := &fakeUpdateHandle{version: "0.0.2"}
	src := &fakeUpdates{
		enabled: true,
		check: func(ctx context.Context) (UpdateHandle, error) {
			return found, nil
		},
	}
	relaunched := 0
	m, q := newTestModel(t, Config{
		Service:        &fakeService{},
		Updates:        src,
		DevAutoInstall: true,
		OnUpdateInstalled: func() {
			relaunched++
		},
	})
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)

	// A running session forbids the automatic install no less than the
	// button: the update is found and waits.
	applyAndSettle(t, m, q, tt, snap("s1", 1, true, scan.PhaseCapturing,
		scan.Counts{Captured: 3}, 3, false, ""))
	m.CheckUpdates()
	settle(t, m, q, tt)
	if found.installs != 0 {
		t.Fatalf("the automatic install ran during a scan: %d", found.installs)
	}
	if !tt.HasText("发现新版本 0.0.2。") {
		t.Error("the found update does not show while it waits")
	}

	// The session settled: the next check installs on its own and fires
	// the restart hook once.
	applyAndSettle(t, m, q, tt, snap("s1", 2, false, scan.PhaseEnded,
		scan.Counts{Captured: 3}, 3, false, ""))
	m.CheckUpdates()
	settle(t, m, q, tt)
	if found.installs != 1 {
		t.Fatalf("the automatic install did not run once: %d", found.installs)
	}
	if relaunched != 1 {
		t.Fatalf("the restart hook fired %d times, want 1", relaunched)
	}
	if !tt.HasText("更新已安装，正在重启应用……") {
		t.Error("the installed state does not show")
	}
}

// TestViewUpdateInstallForbiddenWhileScanning is the 2026-10-17 决定：
// a scan session — running, wrapping up, or waiting for a save retry —
// forbids installing the found update, the area says why, and a click on
// the disabled button installs nothing. Once the session settles, the
// install runs and the entry point's restart hook fires.
func TestViewUpdateInstallForbiddenWhileScanning(t *testing.T) {
	found := &fakeUpdateHandle{version: "0.0.2", notes: "修复若干问题。"}
	src := &fakeUpdates{
		enabled: true,
		check: func(ctx context.Context) (UpdateHandle, error) {
			return found, nil
		},
	}
	relaunched := 0
	m, q := newTestModel(t, Config{
		Service:           &fakeService{},
		Updates:           src,
		OnUpdateInstalled: func() { relaunched++ },
	})
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)
	m.CheckUpdates()
	settle(t, m, q, tt)

	// A session in the capturing phase: the install is forbidden with its
	// reason, and the disabled button installs nothing.
	applyAndSettle(t, m, q, tt, snap("s1", 1, true, scan.PhaseCapturing,
		scan.Counts{Captured: 3, Produced: 2}, 2, true, ""))
	if !tt.HasText("扫描会话进行中，等其结束并保存完成后再安装更新。") {
		t.Error("the running session does not explain the refused install")
	}
	if err := tt.Click("安装更新"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if found.installs != 0 {
		t.Fatalf("the install ran during a scan: %d", found.installs)
	}

	// The wrap-up phase says so.
	applyAndSettle(t, m, q, tt, snap("s1", 2, true, scan.PhaseFinalizing,
		scan.Counts{Captured: 5, Produced: 5}, 5, true, ""))
	if !tt.HasText("扫描正在收尾保存，等保存完成后再安装更新。") {
		t.Error("the wrap-up does not explain the refused install")
	}

	// A failed save awaiting its retry keeps the session active.
	applyAndSettle(t, m, q, tt, snap("s1", 3, true, scan.PhaseEnded,
		scan.Counts{Captured: 5, Produced: 5}, 5, true, "disk full"))
	if !tt.HasText("有保存失败的扫描结果待重试保存，先完成保存再安装更新。") {
		t.Error("the pending save retry does not explain the refused install")
	}
	if err := tt.Click("安装更新"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if found.installs != 0 {
		t.Fatalf("the install ran while a save retry was due: %d", found.installs)
	}

	// The session settled and saved: the install is offered, runs with its
	// progress, and the restart hook fires once.
	applyAndSettle(t, m, q, tt, snap("s1", 4, false, scan.PhaseEnded,
		scan.Counts{Captured: 5, Produced: 5}, 5, false, ""))
	if tt.HasText("等保存完成后再安装更新。") {
		t.Error("a settled session still blocks the install")
	}
	if err := tt.Click("安装更新"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if found.installs != 1 {
		t.Fatalf("the install did not run once on the settled session: %d", found.installs)
	}
	if relaunched != 1 {
		t.Fatalf("the restart hook fired %d times, want 1", relaunched)
	}
	if !tt.HasText("更新已安装，正在重启应用……") {
		t.Error("the installed state does not show")
	}
	// While the restart is imminent, no scan may begin.
	if err := tt.Click("开始扫描"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if !tt.HasText("正在安装更新，应用稍后将自动重启；暂不能开始扫描。") {
		t.Error("a scan is still offered while the update restart is imminent")
	}
}

// TestViewUpdateInstallFailure shows the install error as it is, keeps the
// application on its current version, and offers the install again; a
// retry that succeeds restarts.
func TestViewUpdateInstallFailure(t *testing.T) {
	found := &fakeUpdateHandle{version: "0.0.2", notes: ""}
	found.install = func(progress func(downloaded, total int64)) error {
		progress(128, 256)
		return errInstallBoom
	}
	src := &fakeUpdates{
		enabled: true,
		check: func(ctx context.Context) (UpdateHandle, error) {
			return found, nil
		},
	}
	relaunched := 0
	m, q := newTestModel(t, Config{
		Service:           &fakeService{},
		Updates:           src,
		OnUpdateInstalled: func() { relaunched++ },
	})
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)
	m.CheckUpdates()
	settle(t, m, q, tt)

	if err := tt.Click("安装更新"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if !tt.HasText("安装更新失败：install boom。应用继续运行当前版本，可重试安装。") {
		t.Error("the install failure does not show as it is")
	}
	if relaunched != 0 {
		t.Error("a failed install still restarted the application")
	}

	// The retry succeeds: the error clears and the restart fires.
	found.install = func(progress func(downloaded, total int64)) error {
		progress(256, 256)
		return nil
	}
	if err := tt.Click("安装更新"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if found.installs != 2 {
		t.Fatalf("the install retry did not run: %d", found.installs)
	}
	if relaunched != 1 {
		t.Error("the successful retry did not restart the application")
	}
	if tt.HasText("安装更新失败") {
		t.Error("the earlier install failure outlived the successful retry")
	}
}
