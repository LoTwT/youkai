package ui

import (
	"context"
	"errors"
	"testing"

	"github.com/LoTwT/youkai/internal/scan"
	"github.com/egoist/mygo/ui"
)

// errInstallBoom is the canned install failure.
var errInstallBoom = errors.New("install boom")

// fakeUpdates is the update-source double: Enabled is canned, Check comes
// from a function the test swaps per step.
type fakeUpdates struct {
	enabled bool
	check   func(ctx context.Context) (UpdateHandle, error)
}

func (f *fakeUpdates) Enabled() bool { return f.enabled }

func (f *fakeUpdates) Check(ctx context.Context) (UpdateHandle, error) {
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
