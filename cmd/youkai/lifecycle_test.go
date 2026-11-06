package main

import (
	"bytes"
	"errors"
	"image/png"
	"strings"
	"testing"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/ui"
	"github.com/egoist/mygo"
)

// fakeLifecycleModel records the lifecycle surface the entry point drives
// and answers with fixed state. It keeps the window/tray tests free of the
// real service and window.
type fakeLifecycleModel struct {
	behavior app.CloseBehavior
	exiting  bool
	busy     bool
	status   ui.LifecycleStatus

	// allowResults is consumed by AllowClose; empty falls back to allow.
	allowResults []bool
	allow        bool
	allowCalls   int
	stopCalls    int
	notice       string
	// hintDue answers FirstHideHintDue; showHintCalls counts the prompt
	// requests.
	hintDue       bool
	showHintCalls int
	// openedNotifications records the record IDs a notification click
	// opened.
	openedNotifications []string
	// events records "stop" and "prepare-restore" in call order; tests
	// share it with the fake window to assert the restore ordering.
	events *[]string
}

func (f *fakeLifecycleModel) record(event string) {
	if f.events != nil {
		*f.events = append(*f.events, event)
	}
}

func (f *fakeLifecycleModel) AllowClose() bool {
	f.allowCalls++
	if len(f.allowResults) > 0 {
		result := f.allowResults[0]
		f.allowResults = f.allowResults[1:]
		return result
	}
	return f.allow
}

func (f *fakeLifecycleModel) Stop() {
	f.stopCalls++
	f.record("stop")
}

func (f *fakeLifecycleModel) PrepareRestore() {
	f.record("prepare-restore")
	if f.busy {
		f.Stop()
	}
}

func (f *fakeLifecycleModel) Exiting() bool { return f.exiting }

func (f *fakeLifecycleModel) CloseBehavior() app.CloseBehavior { return f.behavior }

func (f *fakeLifecycleModel) LifecycleStatus() ui.LifecycleStatus {
	if f.status.Text == "" && !f.status.Busy {
		if f.busy {
			return ui.LifecycleStatus{Text: "扫描中", Busy: true}
		}
		return ui.LifecycleStatus{Text: "就绪"}
	}
	return f.status
}

func (f *fakeLifecycleModel) SetTrayUnavailable(reason string) {
	f.notice = "托盘/菜单栏入口不可用（" + reason + "）。关闭窗口将停止扫描并退出应用。"
}

func (f *fakeLifecycleModel) FirstHideHintDue() bool { return f.hintDue }

func (f *fakeLifecycleModel) ShowFirstHideHint() { f.showHintCalls++ }

func (f *fakeLifecycleModel) OpenNotification(recordID string) {
	f.openedNotifications = append(f.openedNotifications, recordID)
}

// fakeWindow records the window calls in order; "stop" from the model is
// recorded through events as well, so restore order is observable.
type fakeWindow struct {
	events  *[]string
	shows   int
	hides   int
	flashes int
}

func (w *fakeWindow) record(event string) {
	if w.events != nil {
		*w.events = append(*w.events, event)
	}
}
func (w *fakeWindow) Show()    { w.shows++; w.record("show") }
func (w *fakeWindow) Hide()    { w.hides++; w.record("hide") }
func (w *fakeWindow) Focus()   { w.record("focus") }
func (w *fakeWindow) Restore() { w.record("restore") }
func (w *fakeWindow) FlashFrame(flash bool) {
	if flash {
		w.flashes++
	}
}

// fakeTray records the menus and tooltips the lifecycle installs and the
// click callbacks it registers.
type fakeTray struct {
	menus    []*mygo.Menu
	tooltips []string
	menu     *mygo.Menu
	clicks   []func()
	bounds   mygo.Rectangle
	// boundsCalls counts the location checks: creation and every close.
	boundsCalls int
}

func (t *fakeTray) SetMenu(menu *mygo.Menu) {
	t.menu = menu
	t.menus = append(t.menus, menu)
}
func (t *fakeTray) SetToolTip(tip string) { t.tooltips = append(t.tooltips, tip) }
func (t *fakeTray) OnClick(fn func()) func() {
	t.clicks = append(t.clicks, fn)
	return func() {}
}
func (t *fakeTray) Bounds() mygo.Rectangle {
	t.boundsCalls++
	return t.bounds
}

// fakeNotification records the two-stage order, the options and the close.
type fakeNotification struct {
	opts   mygo.NotificationOptions
	events *[]string
	shows  int
	closes int
	err    error
}

func (n *fakeNotification) Show() error {
	n.shows++
	if n.events != nil {
		*n.events = append(*n.events, "show:"+n.opts.ID)
	}
	return n.err
}

func (n *fakeNotification) Close() {
	n.closes++
	if n.events != nil {
		*n.events = append(*n.events, "close:"+n.opts.ID)
	}
}

// installFakeNotifications replaces the native notification factory and
// returns the created notifications in order.
func installFakeNotifications(l *windowLifecycle) (*[]*fakeNotification, *[]string) {
	var created []*fakeNotification
	var events []string
	l.newNotification = func(opts mygo.NotificationOptions) notificationSurface {
		n := &fakeNotification{opts: opts, events: &events}
		created = append(created, n)
		return n
	}
	return &created, &events
}

// fakeEvent is a preventable close/quit event.
type fakeEvent struct{ prevented bool }

func (e *fakeEvent) PreventDefault() { e.prevented = true }

// installFakeTray creates the lifecycle's tray through the injected factory
// and returns the recorder and the options the lifecycle passed.
func installFakeTray(t *testing.T, l *windowLifecycle) (*fakeTray, *mygo.TrayOptions) {
	t.Helper()
	tray := &fakeTray{}
	var got mygo.TrayOptions
	l.newTray = func(opts mygo.TrayOptions) (traySurface, error) {
		got = opts
		// NewTray installs the options' menu directly; record it as the
		// first menu the physical tray shows.
		tray.menu = opts.Menu
		tray.menus = append(tray.menus, opts.Menu)
		if opts.ToolTip != "" {
			tray.tooltips = append(tray.tooltips, opts.ToolTip)
		}
		return tray, nil
	}
	l.CreateTray()
	return tray, &got
}

func menuItem(t *testing.T, menu *mygo.Menu, label string) *mygo.MenuItem {
	t.Helper()
	for _, item := range menu.Items() {
		if item.Label == label {
			return item
		}
	}
	t.Fatalf("menu has no item %q", label)
	return nil
}

// The first close shows the hint instead of hiding. Once acknowledged, the
// next close hides without asking again.
func TestFirstCloseShowsHideHint(t *testing.T) {
	model := &fakeLifecycleModel{behavior: app.CloseBehaviorHide, hintDue: true}
	life := newWindowLifecycle(model)
	installFakeTray(t, life)
	win := &fakeWindow{}
	life.AttachWindow(win)

	first := &fakeEvent{}
	life.OnWindowClose(first)
	if !first.prevented || win.hides != 0 {
		t.Fatalf("the first close: prevented=%v hides=%d, want the prompt", first.prevented, win.hides)
	}
	if model.showHintCalls != 1 {
		t.Fatalf("first-hide prompts = %d, want 1", model.showHintCalls)
	}

	// The hint's acknowledgement marks it seen; a later close hides
	// directly.
	model.hintDue = false
	second := &fakeEvent{}
	life.OnWindowClose(second)
	if !second.prevented || win.hides != 1 {
		t.Fatalf("the acknowledged close: prevented=%v hides=%d, want one hide", second.prevented, win.hides)
	}
	if model.showHintCalls != 1 {
		t.Fatalf("the hint showed again: %d prompts", model.showHintCalls)
	}
}

// A missing tray never shows the hint: the close falls back to the exit
// wrap-up, where the window is still visible.
func TestFirstHideHintSkippedWithoutTray(t *testing.T) {
	model := &fakeLifecycleModel{behavior: app.CloseBehaviorHide, hintDue: true, allow: false}
	life := newWindowLifecycle(model)
	life.newTray = func(mygo.TrayOptions) (traySurface, error) {
		return nil, errors.New("no notification area")
	}
	life.CreateTray()
	win := &fakeWindow{}
	life.AttachWindow(win)

	event := &fakeEvent{}
	life.OnWindowClose(event)
	if model.showHintCalls != 0 || win.hides != 0 || model.allowCalls != 1 {
		t.Fatalf("tray-less first close: prompts=%d hides=%d allow=%d",
			model.showHintCalls, win.hides, model.allowCalls)
	}
}

// The default close hides to the tray without starting the exit wrap-up.
func TestWindowCloseHidesByDefault(t *testing.T) {
	model := &fakeLifecycleModel{behavior: app.CloseBehaviorHide, allow: false}
	life := newWindowLifecycle(model)
	tray, opts := installFakeTray(t, life)
	if len(tray.menus) != 1 {
		t.Fatalf("tray menu installed %d times, want 1", len(tray.menus))
	}
	if len(opts.Icon) == 0 {
		t.Fatal("the tray was created without an icon")
	}
	win := &fakeWindow{}
	life.AttachWindow(win)

	event := &fakeEvent{}
	life.OnWindowClose(event)
	if !event.prevented {
		t.Fatal("hiding did not prevent the native window close")
	}
	if win.hides != 1 || win.shows != 0 {
		t.Fatalf("window hides=%d shows=%d, want one hide", win.hides, win.shows)
	}
	if model.allowCalls != 0 || model.stopCalls != 0 {
		t.Fatalf("hiding started the exit path: allowCalls=%d stopCalls=%d", model.allowCalls, model.stopCalls)
	}
}

// A left click restores the window on Windows, and a double click is the
// same callback twice: the second restore only re-shows the visible window
// and rebuilds nothing.
func TestTrayClickRestoresWindow(t *testing.T) {
	model := &fakeLifecycleModel{behavior: app.CloseBehaviorHide}
	life := newWindowLifecycle(model)
	tray, _ := installFakeTray(t, life)
	win := &fakeWindow{}
	life.AttachWindow(win)

	if len(tray.clicks) != 1 {
		t.Fatalf("tray click callbacks = %d, want 1", len(tray.clicks))
	}
	menusAfterCreation := len(tray.menus)

	tray.clicks[0]()
	if win.shows != 1 || win.hides != 0 {
		t.Fatalf("first click shows=%d hides=%d, want one restore", win.shows, win.hides)
	}
	if model.stopCalls != 0 {
		t.Fatalf("an idle click stopped a scan: %d stops", model.stopCalls)
	}

	// The platform has no double-click event; both releases reach the same
	// callback. Restore is idempotent for an idle window.
	tray.clicks[0]()
	if win.shows != 2 || win.hides != 0 {
		t.Fatalf("double click shows=%d hides=%d, want two restores", win.shows, win.hides)
	}
	if model.stopCalls != 0 {
		t.Fatalf("a double click stopped a scan: %d stops", model.stopCalls)
	}
	if len(tray.menus) != menusAfterCreation {
		t.Fatalf("a repeated restore rebuilt the tray menu: %d menus, want %d", len(tray.menus), menusAfterCreation)
	}
}

// A click during the explicit-exit wrap-up follows Restore's existing
// contract: it shows the wrap-up progress and never turns the exit into a
// hide or a second exit.
func TestTrayClickDuringExitUsesRestore(t *testing.T) {
	model := &fakeLifecycleModel{behavior: app.CloseBehaviorHide, exiting: true}
	life := newWindowLifecycle(model)
	tray, _ := installFakeTray(t, life)
	win := &fakeWindow{}
	life.AttachWindow(win)

	tray.clicks[0]()
	if win.shows != 1 || win.hides != 0 {
		t.Fatalf("click during exit shows=%d hides=%d, want the wrap-up window", win.shows, win.hides)
	}
	if model.allowCalls != 0 {
		t.Fatalf("the click asked to close %d times, want none", model.allowCalls)
	}
}

// Close-to-exit runs the wrap-up through AllowClose and keeps the window
// until it settles.
func TestWindowCloseExitsWhenPreferenceSaysSo(t *testing.T) {
	model := &fakeLifecycleModel{
		behavior:     app.CloseBehaviorExit,
		allowResults: []bool{false},
	}
	life := newWindowLifecycle(model)
	installFakeTray(t, life)
	win := &fakeWindow{}
	life.AttachWindow(win)

	event := &fakeEvent{}
	life.OnWindowClose(event)
	if !event.prevented {
		t.Fatal("the first close was allowed before the wrap-up settled")
	}
	if model.allowCalls != 1 {
		t.Fatalf("AllowClose calls = %d, want 1", model.allowCalls)
	}
	if win.hides != 0 {
		t.Fatal("close-to-exit hid the window")
	}
}

// A failed tray must not leave close-to-tray hiding the only entry: the
// close falls back to the explicit-exit wrap-up and the reason is visible.
func TestTrayFailureFallsBackToExitOnClose(t *testing.T) {
	model := &fakeLifecycleModel{behavior: app.CloseBehaviorHide}
	life := newWindowLifecycle(model)
	life.newTray = func(mygo.TrayOptions) (traySurface, error) {
		return nil, errors.New("no notification area")
	}
	out := captureLog(life.CreateTray)
	if !strings.Contains(out, "youkai: tray unavailable") || !strings.Contains(out, "no notification area") {
		t.Fatalf("tray failure log = %q, want the unavailable line", out)
	}
	if !strings.Contains(model.notice, "托盘/菜单栏入口不可用") {
		t.Fatalf("window notice = %q, want the tray failure", model.notice)
	}
	win := &fakeWindow{}
	life.AttachWindow(win)

	event := &fakeEvent{}
	life.OnWindowClose(event)
	if !event.prevented {
		t.Fatal("the fallback close was allowed before the wrap-up settled")
	}
	if model.allowCalls != 1 {
		t.Fatalf("AllowClose calls = %d, want the fallback exit", model.allowCalls)
	}
	if win.hides != 0 {
		t.Fatal("the fallback hid the only entry")
	}
}

// A platform that returns neither a tray nor an error is a failure too: a
// nil entry must not enable close-to-tray.
func TestNilTrayFallsBackToExitOnClose(t *testing.T) {
	model := &fakeLifecycleModel{behavior: app.CloseBehaviorHide}
	life := newWindowLifecycle(model)
	life.newTray = func(mygo.TrayOptions) (traySurface, error) { return nil, nil }
	out := captureLog(life.CreateTray)
	if !strings.Contains(out, "youkai: tray unavailable") {
		t.Fatalf("nil tray log = %q, want the unavailable line", out)
	}
	if !strings.Contains(model.notice, "托盘/菜单栏入口不可用") {
		t.Fatalf("window notice = %q, want the tray failure", model.notice)
	}
	win := &fakeWindow{}
	life.AttachWindow(win)

	event := &fakeEvent{}
	life.OnWindowClose(event)
	if !event.prevented || model.allowCalls != 1 {
		t.Fatalf("nil tray close: prevented=%v allowCalls=%d, want the exit wrap-up", event.prevented, model.allowCalls)
	}
	if win.hides != 0 {
		t.Fatal("a nil tray hid the only entry")
	}
}

// Creating the tray logs where the icon is. The bounds are diagnostic only.
func TestTrayCreationLogsItsIconLocation(t *testing.T) {
	model := &fakeLifecycleModel{behavior: app.CloseBehaviorHide}
	life := newWindowLifecycle(model)
	life.newTray = func(mygo.TrayOptions) (traySurface, error) {
		return &fakeTray{bounds: mygo.Rectangle{X: 100, Y: 20, Width: 20, Height: 20}}, nil
	}
	out := captureLog(life.CreateTray)
	if !strings.Contains(out, "youkai: tray created: icon at 100,20 20x20") {
		t.Fatalf("tray creation log = %q, want the icon location", out)
	}
}

// An icon the platform cannot locate (on Windows 11, one in the notification
// area's overflow) does not disable close-to-tray: the user can still reach
// the overflow, and Shell_NotifyIconGetRect cannot tell the two cases apart.
func TestTrayWithoutBoundsStillHidesOnClose(t *testing.T) {
	model := &fakeLifecycleModel{behavior: app.CloseBehaviorHide}
	life := newWindowLifecycle(model)
	tray := &fakeTray{}
	life.newTray = func(mygo.TrayOptions) (traySurface, error) {
		return tray, nil // no rectangle
	}
	life.CreateTray()
	win := &fakeWindow{}
	life.AttachWindow(win)

	event := &fakeEvent{}
	out := captureLog(func() { life.OnWindowClose(event) })
	if !event.prevented {
		t.Fatal("hiding did not prevent the native window close")
	}
	if win.hides != 1 || model.allowCalls != 0 {
		t.Fatalf("window hides=%d allowCalls=%d, want a hide without the exit path", win.hides, model.allowCalls)
	}
	if !strings.Contains(out, "youkai: tray at close:") || !strings.Contains(out, "rectangle") {
		t.Fatalf("close log = %q, want the tray location check", out)
	}
	if tray.boundsCalls != 2 {
		t.Fatalf("bounds checks = %d, want creation and close", tray.boundsCalls)
	}
}

// A close without a tray logs the decision that produced the exit wrap-up.
func TestCloseWithoutTrayLogsExitFallback(t *testing.T) {
	model := &fakeLifecycleModel{behavior: app.CloseBehaviorHide, allow: false}
	life := newWindowLifecycle(model)
	life.newTray = func(mygo.TrayOptions) (traySurface, error) {
		return nil, errors.New("no notification area")
	}
	life.CreateTray()
	win := &fakeWindow{}
	life.AttachWindow(win)

	event := &fakeEvent{}
	out := captureLog(func() { life.OnWindowClose(event) })
	if !strings.Contains(out, "close requested without a tray entry; running the exit wrap-up instead of hiding") {
		t.Fatalf("close log = %q, want the fallback decision", out)
	}
	if !event.prevented || win.hides != 0 || model.allowCalls != 1 {
		t.Fatalf("close with a running wrap-up: prevented=%v hides=%d allowCalls=%d", event.prevented, win.hides, model.allowCalls)
	}
}

// Explicit quit always runs the wrap-up, even with the hide preference, and
// the second asking (after the wrap-up settled) lets the quit through.
func TestExplicitQuitAlwaysWrapsUp(t *testing.T) {
	model := &fakeLifecycleModel{
		behavior:     app.CloseBehaviorHide,
		allowResults: []bool{false, true},
	}
	life := newWindowLifecycle(model)
	installFakeTray(t, life)
	win := &fakeWindow{}
	life.AttachWindow(win)

	first := &fakeEvent{}
	life.OnBeforeQuit(first)
	if !first.prevented {
		t.Fatal("the first quit was allowed before the wrap-up settled")
	}
	if win.hides != 0 {
		t.Fatal("an explicit quit hid the window")
	}
	second := &fakeEvent{}
	life.OnBeforeQuit(second)
	if second.prevented {
		t.Fatal("the settled wrap-up did not allow the quit")
	}
	if model.allowCalls != 2 {
		t.Fatalf("AllowClose calls = %d, want 2", model.allowCalls)
	}
}

// A refused quit that did not start the exit (an unsaved editor) restores
// the window so the prompt is visible; a refused quit during the running
// wrap-up does not force the window up.
func TestQuitRefusalRestoresOnlyWithoutRunningExit(t *testing.T) {
	model := &fakeLifecycleModel{behavior: app.CloseBehaviorHide, allow: false, exiting: false}
	life := newWindowLifecycle(model)
	win := &fakeWindow{}
	life.AttachWindow(win)
	event := &fakeEvent{}
	life.OnBeforeQuit(event)
	if !event.prevented {
		t.Fatal("the quit was allowed")
	}
	if win.shows != 1 {
		t.Fatalf("the prompt was left hidden: shows=%d", win.shows)
	}

	running := &fakeLifecycleModel{behavior: app.CloseBehaviorHide, allow: false, exiting: true}
	runningLife := newWindowLifecycle(running)
	runningWin := &fakeWindow{}
	runningLife.AttachWindow(runningWin)
	second := &fakeEvent{}
	runningLife.OnBeforeQuit(second)
	if !second.prevented {
		t.Fatal("the running exit allowed the quit early")
	}
	if runningWin.shows != 0 {
		t.Fatal("the running wrap-up forced the window up")
	}
}

// A second instance restores the window, stops an active scan first and
// never starts a scan.
func TestSecondInstanceRestoresAndStopsFirst(t *testing.T) {
	var events []string
	model := &fakeLifecycleModel{behavior: app.CloseBehaviorHide, busy: true, events: &events}
	life := newWindowLifecycle(model)
	installFakeTray(t, life)
	win := &fakeWindow{events: &events}
	life.AttachWindow(win)

	life.OnSecondInstance([]string{"youkai://open"}, "/tmp")
	if model.stopCalls != 1 {
		t.Fatalf("stops = %d, want the active session stopped", model.stopCalls)
	}
	if win.shows != 1 || win.hides != 0 {
		t.Fatalf("window shows=%d hides=%d, want one restore", win.shows, win.hides)
	}
	want := []string{"prepare-restore", "stop", "restore", "show", "focus"}
	if strings.Join(events, ",") != strings.Join(want, ",") {
		t.Fatalf("restore call order = %v, want stop before show (%v)", events, want)
	}
}

// Restore replays a request that arrived before the window existed.
func TestRestoreBeforeWindowIsReplayed(t *testing.T) {
	model := &fakeLifecycleModel{behavior: app.CloseBehaviorHide}
	life := newWindowLifecycle(model)
	life.Restore()
	if life.window != nil {
		t.Fatal("restore created a window")
	}
	win := &fakeWindow{}
	life.AttachWindow(win)
	if win.shows != 1 {
		t.Fatalf("pending restore shows = %d, want 1", win.shows)
	}
}

// Only youkai://open — with an optional query — restores the window;
// unknown schemes and routes do nothing and never scan.
func TestOpenURLRecognizesOnlyOpenRoute(t *testing.T) {
	model := &fakeLifecycleModel{behavior: app.CloseBehaviorHide}
	life := newWindowLifecycle(model)
	win := &fakeWindow{}
	life.AttachWindow(win)

	for _, raw := range []string{
		"youkai://open",
		"youkai://open?source=browser",
		"YOUKAI://OPEN/",
	} {
		before := win.shows
		life.OnOpenURL(raw)
		if win.shows != before+1 {
			t.Errorf("%q did not restore the window", raw)
		}
	}
	for _, raw := range []string{
		"youkai://scan",
		"youkai://open/extra",
		"youkai:open",
		"https://open",
		"not a url",
		"",
	} {
		before := win.shows
		life.OnOpenURL(raw)
		if win.shows != before {
			t.Errorf("%q restored the window", raw)
		}
	}
	if model.stopCalls != 0 {
		t.Fatalf("an idle deep link stopped a scan: %d stops", model.stopCalls)
	}
}

// A deep link during collection stops the scan before showing the window.
func TestOpenURLDuringScanStopsFirst(t *testing.T) {
	var events []string
	model := &fakeLifecycleModel{behavior: app.CloseBehaviorHide, busy: true, events: &events}
	life := newWindowLifecycle(model)
	installFakeTray(t, life)
	win := &fakeWindow{events: &events}
	life.AttachWindow(win)

	life.OnOpenURL("youkai://open")
	if model.stopCalls != 1 || win.shows != 1 {
		t.Fatalf("stops=%d shows=%d, want one stop and one show", model.stopCalls, win.shows)
	}
	want := []string{"prepare-restore", "stop", "restore", "show", "focus"}
	if strings.Join(events, ",") != strings.Join(want, ",") {
		t.Fatalf("restore call order = %v, want stop before show (%v)", events, want)
	}
}

// Ignored links must not put query data into the diagnostic log.
func TestRedactedURLDropsQuery(t *testing.T) {
	got := redactedURL("youkai://scan?token=secret#fragment")
	if strings.Contains(got, "secret") || strings.Contains(got, "fragment") {
		t.Fatalf("redacted URL = %q", got)
	}
	if got != "youkai://scan" {
		t.Fatalf("redacted URL = %q, want youkai://scan", got)
	}
}

// The tray menu follows the four lifecycle states, including the fixed
// labels and the stop item's enabled state (08 §7).
func TestTrayMenuStates(t *testing.T) {
	cases := []struct {
		status      ui.LifecycleStatus
		statusLabel string
		open        string
		stopEnabled bool
		exit        string
	}{
		{
			status:      ui.LifecycleStatus{Text: "就绪"},
			statusLabel: "youkai · 就绪",
			open:        "打开 youkai",
			exit:        "退出 youkai",
		},
		{
			status:      ui.LifecycleStatus{Text: "扫描中", Busy: true},
			statusLabel: "youkai · 正在扫描（停止：Ctrl+Alt+Shift+S）",
			open:        "停止扫描并打开",
			stopEnabled: true,
			exit:        "停止扫描并退出",
		},
		{
			status:      ui.LifecycleStatus{Text: "收尾中"},
			statusLabel: "youkai · 正在保存结果",
			open:        "打开 youkai（不取消剩余识别）",
			exit:        "退出 youkai（等保存完成）",
		},
		{
			status:      ui.LifecycleStatus{Text: "保存失败"},
			statusLabel: "youkai · 结果未保存，需要重试",
			open:        "打开 youkai",
			exit:        "退出 youkai（会被拒绝并打开窗口）",
		},
	}
	for _, tc := range cases {
		t.Run(tc.status.Text, func(t *testing.T) {
			model := &fakeLifecycleModel{behavior: app.CloseBehaviorHide, status: tc.status}
			life := newWindowLifecycle(model)
			menu := life.trayMenu()
			status := menuItem(t, menu, tc.statusLabel)
			if !status.Disabled {
				t.Fatal("the status line accepts clicks")
			}
			menuItem(t, menu, tc.open)
			stop := menuItem(t, menu, "停止扫描")
			if stop.Disabled != !tc.stopEnabled {
				t.Fatalf("stop disabled = %v, want enabled %v", stop.Disabled, tc.stopEnabled)
			}
			menuItem(t, menu, tc.exit)
		})
	}
}

// The result notification closes the input-stop segment before showing,
// then flashes the taskbar button.
func TestNotificationTwoStageOrder(t *testing.T) {
	model := &fakeLifecycleModel{behavior: app.CloseBehaviorHide}
	life := newWindowLifecycle(model)
	created, events := installFakeNotifications(life)
	win := &fakeWindow{}
	life.AttachWindow(win)

	first := ui.Notification{
		ID:       ui.NotificationID("rec-1", ui.NotificationInputStopped),
		Segment:  ui.NotificationInputStopped,
		RecordID: "rec-1",
		Title:    "youkai 已停止操作游戏",
		Body:     "正在识别和保存结果。",
	}
	life.Notify(first)
	if len(*created) != 1 || (*created)[0].shows != 1 {
		t.Fatalf("first notification = %d created, want one shown", len(*created))
	}
	if win.flashes != 0 {
		t.Fatal("the input-stop notification flashed the taskbar")
	}

	second := ui.Notification{
		ID:       ui.NotificationID("rec-1", ui.NotificationResult),
		Segment:  ui.NotificationResult,
		RecordID: "rec-1",
		Title:    "扫描完成：扫全了",
		Body:     "3 件可以导出。",
		Flash:    true,
	}
	life.Notify(second)
	want := "show:" + first.ID + ",close:" + first.ID + ",show:" + second.ID
	if got := strings.Join(*events, ","); got != want {
		t.Fatalf("notification order = %q, want %q", got, want)
	}
	if win.flashes != 1 {
		t.Fatalf("taskbar flashes = %d, want 1", win.flashes)
	}
	if len(*created) != 2 || (*created)[1].opts.ID != second.ID ||
		(*created)[1].opts.Title != second.Title || (*created)[1].opts.Body != second.Body {
		t.Fatalf("second notification options = %+v", (*created)[1].opts)
	}
}

// A session that never sent the first segment (prepare timeout) shows only
// the result and has nothing to close.
func TestResultNotificationWithoutFirstSegment(t *testing.T) {
	model := &fakeLifecycleModel{behavior: app.CloseBehaviorHide}
	life := newWindowLifecycle(model)
	created, events := installFakeNotifications(life)
	win := &fakeWindow{}
	life.AttachWindow(win)

	result := ui.Notification{
		ID:       ui.NotificationID("rec-2", ui.NotificationResult),
		Segment:  ui.NotificationResult,
		RecordID: "rec-2",
		Title:    "没有开始扫描",
		Body:     "30 秒内游戏没有就绪。点击查看。",
		Flash:    true,
	}
	life.Notify(result)
	if len(*created) != 1 || strings.Join(*events, ",") != "show:"+result.ID {
		t.Fatalf("prepare-timeout notification = %d created, events %v", len(*created), *events)
	}
	if win.flashes != 1 {
		t.Fatalf("taskbar flashes = %d, want 1", win.flashes)
	}
}

// A notification click restores the window and asks the model to open the
// record; it never starts a scan.
func TestNotificationClickOpensTheRecord(t *testing.T) {
	model := &fakeLifecycleModel{behavior: app.CloseBehaviorHide}
	life := newWindowLifecycle(model)
	win := &fakeWindow{}
	life.AttachWindow(win)

	life.OnNotificationClick(ui.NotificationID("rec-3", ui.NotificationResult))
	if win.shows != 1 {
		t.Fatalf("notification click shows = %d, want 1", win.shows)
	}
	if len(model.openedNotifications) != 1 || model.openedNotifications[0] != "rec-3" {
		t.Fatalf("opened notifications = %v", model.openedNotifications)
	}
	if model.stopCalls != 0 {
		t.Fatalf("an idle notification click stopped a scan: %d", model.stopCalls)
	}

	life.OnNotificationClick("someone-elses-notification")
	if win.shows != 1 || len(model.openedNotifications) != 1 {
		t.Fatalf("a foreign notification click was handled: shows=%d opened=%v",
			win.shows, model.openedNotifications)
	}
}

// The tray menu carries the status, opens the window, stops only the scan
// and quits through the explicit-exit path.
func TestTrayMenuActions(t *testing.T) {
	model := &fakeLifecycleModel{
		behavior: app.CloseBehaviorHide,
		busy:     true,
		status:   ui.LifecycleStatus{Text: "扫描中", Busy: true},
	}
	life := newWindowLifecycle(model)
	quits := 0
	life.quitApp = func() { quits++ }
	tray, _ := installFakeTray(t, life)
	win := &fakeWindow{}
	life.AttachWindow(win)

	menu := tray.menu
	status := menuItem(t, menu, "youkai · 正在扫描（停止：Ctrl+Alt+Shift+S）")
	if !status.Disabled {
		t.Fatal("the status item accepts clicks")
	}
	stop := menuItem(t, menu, "停止扫描")
	if stop.Disabled {
		t.Fatal("stop is disabled during a scan")
	}
	stop.Click(stop, nil)
	if model.stopCalls != 1 || quits != 0 {
		t.Fatalf("tray stop: stops=%d quits=%d, want one stop and no quit", model.stopCalls, quits)
	}

	open := menuItem(t, menu, "停止扫描并打开")
	open.Click(open, nil)
	if win.shows != 1 || model.stopCalls != 2 {
		// Restore also stops the still-busy session: the restore contract.
		t.Fatalf("tray open: shows=%d stops=%d", win.shows, model.stopCalls)
	}

	quit := menuItem(t, menu, "停止扫描并退出")
	quit.Click(quit, nil)
	if quits != 1 {
		t.Fatalf("tray quit calls = %d, want 1", quits)
	}

	// A settled session disables stop and renames the status; unchanged
	// states do not rebuild the menu.
	model.busy = false
	model.status = ui.LifecycleStatus{Text: "就绪"}
	before := len(tray.menus)
	life.Refresh()
	if len(tray.menus) != before+1 {
		t.Fatalf("status change rebuilt %d menus, want 1", len(tray.menus)-before)
	}
	if !menuItem(t, tray.menu, "停止扫描").Disabled {
		t.Fatal("stop stays enabled without a session")
	}
	menuItem(t, tray.menu, "youkai · 就绪")
	menuItem(t, tray.menu, "打开 youkai")
	menuItem(t, tray.menu, "退出 youkai")
	if len(tray.tooltips) == 0 || tray.tooltips[len(tray.tooltips)-1] != "youkai · 就绪" {
		t.Fatalf("tooltip = %q, want the ready status", tray.tooltips)
	}
	life.Refresh()
	if len(tray.menus) != before+1 {
		t.Fatal("an unchanged status rebuilt the tray menu")
	}
}

// Dock activation restores only when nothing is visible.
func TestActivationRestoresOnlyHiddenWindows(t *testing.T) {
	model := &fakeLifecycleModel{behavior: app.CloseBehaviorHide}
	life := newWindowLifecycle(model)
	win := &fakeWindow{}
	life.AttachWindow(win)

	life.OnActivate(true)
	if win.shows != 0 {
		t.Fatal("activation disturbed a visible window")
	}
	life.OnActivate(false)
	if win.shows != 1 {
		t.Fatal("activation did not restore a hidden window")
	}
}

// A failed exit restores the window so the retry is visible.
func TestExitFailureRestoresWindow(t *testing.T) {
	model := &fakeLifecycleModel{behavior: app.CloseBehaviorHide, busy: true}
	life := newWindowLifecycle(model)
	win := &fakeWindow{}
	life.AttachWindow(win)

	life.OnExitFailed()
	if win.shows != 1 {
		t.Fatalf("exit failure shows = %d, want 1", win.shows)
	}
	if model.stopCalls != 1 {
		t.Fatalf("exit failure restores without stopping the busy session: %d", model.stopCalls)
	}
}

// The single-instance request happens before the data-root occupancy, so a
// second launch can hand over its command line instead of being refused by
// the data root first.
func TestAcquireInstanceLocksOrdersSingleInstanceFirst(t *testing.T) {
	var order []string
	var gotRoot string
	expected := &app.DataRootLock{}
	lock, err := acquireInstanceLocks("/data/Youkai", instanceLockOps{
		requestSingleInstance: func() bool {
			order = append(order, "single")
			return true
		},
		acquireDataRoot: func(root string) (*app.DataRootLock, error) {
			order = append(order, "data")
			gotRoot = root
			return expected, nil
		},
	})
	if err != nil {
		t.Fatalf("acquireInstanceLocks: %v", err)
	}
	if lock != expected {
		t.Fatal("acquireInstanceLocks returned another lock")
	}
	if strings.Join(order, ",") != "single,data" {
		t.Fatalf("acquisition order = %v, want single-instance first", order)
	}
	if gotRoot != "/data/Youkai" {
		t.Fatalf("data root = %q", gotRoot)
	}
}

// A second instance stops at the single-instance lock: the data root is
// never touched.
func TestAcquireInstanceLocksSecondInstanceSkipsDataRoot(t *testing.T) {
	lock, err := acquireInstanceLocks("/data/Youkai", instanceLockOps{
		requestSingleInstance: func() bool { return false },
		acquireDataRoot: func(string) (*app.DataRootLock, error) {
			t.Fatal("the second instance acquired the data root")
			return nil, nil
		},
	})
	if !errors.Is(err, errSecondInstance) {
		t.Fatalf("err = %v, want errSecondInstance", err)
	}
	if lock != nil {
		t.Fatal("the second instance kept a lock")
	}
}

// A failed data-root occupancy is returned as-is after the single-instance
// request succeeded.
func TestAcquireInstanceLocksReturnsDataRootFailure(t *testing.T) {
	rootErr := errors.New("held")
	_, err := acquireInstanceLocks("/data/Youkai", instanceLockOps{
		requestSingleInstance: func() bool { return true },
		acquireDataRoot:       func(string) (*app.DataRootLock, error) { return nil, rootErr },
	})
	if !errors.Is(err, rootErr) {
		t.Fatalf("err = %v, want the data-root failure", err)
	}
}

// The embedded tray icon is a real PNG the size the tray expects.
func TestTrayIconIsUsablePNG(t *testing.T) {
	if len(trayIconPNG) == 0 {
		t.Fatal("no embedded tray icon")
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(trayIconPNG))
	if err != nil {
		t.Fatalf("decode tray icon: %v", err)
	}
	if cfg.Width != 32 || cfg.Height != 32 {
		t.Fatalf("tray icon = %dx%d, want 32x32", cfg.Width, cfg.Height)
	}
}
