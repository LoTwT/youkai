package main

import (
	"errors"
	"log"
	"net/url"
	"runtime"
	"strings"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/ui"
	"github.com/egoist/mygo"
)

// openURLScheme is the only URL scheme this build handles; openURLRoute is
// the only route. Deep links never start a scan or carry a local path.
const (
	openURLScheme = "youkai"
	openURLRoute  = "open"
)

// errSecondInstance means another process owns the single-instance lock and
// received this launch's arguments. It is not an error to report as a
// failure: the launcher exits quietly, exactly as the second launch should.
var errSecondInstance = errors.New("another youkai instance is already running")

// instanceLockOps are the two process-wide acquisitions the entry point
// makes, in order: MyGo's single-instance socket first, so a second launch
// forwards its command line and exits before it can touch the data root,
// then the data-root occupancy itself. The data-root lock alone rejects a
// second process without giving it a way to wake the first one.
type instanceLockOps struct {
	requestSingleInstance func() bool
	acquireDataRoot       func(string) (*app.DataRootLock, error)
}

// acquireInstanceLocks runs the acquisitions in their required order and
// returns errSecondInstance when the launch was handed to the running
// process.
func acquireInstanceLocks(dataRoot string, ops instanceLockOps) (*app.DataRootLock, error) {
	if !ops.requestSingleInstance() {
		return nil, errSecondInstance
	}
	return ops.acquireDataRoot(dataRoot)
}

// windowSurface is the main-window surface the lifecycle drives; *mygo.Window
// implements it. Tests substitute a recorder.
type windowSurface interface {
	Show()
	Hide()
	Focus()
	Restore()
}

// traySurface is the tray/menu bar surface the lifecycle updates;
// *mygo.Tray implements it. Tests substitute a recorder.
type traySurface interface {
	SetMenu(*mygo.Menu)
	SetToolTip(string)
}

// preventableEvent is a native event whose default action can be cancelled;
// *mygo.CloseEvent and *mygo.QuitEvent both satisfy it.
type preventableEvent interface {
	PreventDefault()
}

// lifecycleModel is the model surface the window lifecycle reads and
// drives; *ui.Model implements it. Tests substitute a recorder.
type lifecycleModel interface {
	AllowClose() bool
	Stop()
	PrepareRestore()
	Exiting() bool
	CloseBehavior() app.CloseBehavior
	LifecycleStatus() ui.LifecycleStatus
	SetWindowNotice(string)
}

// windowLifecycle owns the window lifecycle decisions the entry point
// wires to MyGo: close-to-tray versus close-to-exit, explicit quit, restore
// from the tray/Dock/second instance/deep links, and the tray menu. It is
// confined to the UI thread, like the model it drives; the single-instance
// and URL callbacks arrive on that thread.
type windowLifecycle struct {
	model  lifecycleModel
	window windowSurface
	tray   traySurface

	// newTray and quitApp are seams: production uses MyGo's tray and quit,
	// tests record the calls without native objects.
	newTray func(mygo.TrayOptions) (traySurface, error)
	quitApp func()

	// pendingRestore remembers a restore that arrived before the window
	// was created (a launch-time deep link is delivered after ready, but a
	// second-instance message can queue earlier).
	pendingRestore bool

	// status is the last state rendered into the tray menu, to avoid
	// rebuilding the menu for every unchanged snapshot.
	status      ui.LifecycleStatus
	statusKnown bool
}

func newWindowLifecycle(model lifecycleModel) *windowLifecycle {
	return &windowLifecycle{
		model: model,
		newTray: func(opts mygo.TrayOptions) (traySurface, error) {
			return mygo.NewTray(opts)
		},
		quitApp: mygo.App.Quit,
	}
}

// AttachWindow connects the main window once the ready callback created it
// and replays a restore that arrived earlier.
func (l *windowLifecycle) AttachWindow(win windowSurface) {
	l.window = win
	if l.pendingRestore {
		l.Restore()
	}
}

// CreateTray adds the tray/menu bar entry. Called from the ready callback.
// A failed creation is not fatal: the close-to-tray preference would hide
// the only way back into the app, so closing falls back to the explicit-exit
// wrap-up and the window states why.
func (l *windowLifecycle) CreateTray() {
	if l.tray != nil {
		return
	}
	tray, err := l.newTray(mygo.TrayOptions{
		Icon: trayIconPNG,
		// macOS tints a template image to match the menu bar; elsewhere the
		// icon keeps its own colors.
		IconIsTemplate: runtime.GOOS == "darwin",
		ToolTip:        "Youkai · " + l.model.LifecycleStatus().Text,
		Menu:           l.trayMenu(),
	})
	if err != nil {
		log.Printf("youkai: tray unavailable: %v; closing the window will stop the scan and quit the application", err)
		l.model.SetWindowNotice("托盘/菜单栏入口不可用（" + err.Error() + "）。关闭窗口将停止扫描并退出应用。")
		return
	}
	l.tray = tray
	l.status = l.model.LifecycleStatus()
	l.statusKnown = true
	l.Refresh()
}

// Restore brings the main window back for every entry point: tray, Dock,
// second instance and deep link. An active session is stopped first, so the
// restored window shows the wrap-up progress instead of accepting input
// during collection. It never starts a scan.
func (l *windowLifecycle) Restore() {
	if l.window == nil {
		l.pendingRestore = true
		return
	}
	l.pendingRestore = false
	l.model.PrepareRestore()
	l.window.Restore()
	l.window.Show()
	l.window.Focus()
	l.Refresh()
}

// OnWindowClose applies the close preference. The default hides to the
// tray and keeps the process running; close-to-exit runs the wrap-up; an
// explicit exit already in progress is never turned into a hide.
func (l *windowLifecycle) OnWindowClose(e preventableEvent) {
	if l.model.Exiting() || l.model.CloseBehavior() == app.CloseBehaviorExit || l.tray == nil {
		// AllowClose starts or joins the explicit-exit wrap-up and says no
		// until the final save settled. A completed wrap-up allows the close.
		if !l.model.AllowClose() {
			e.PreventDefault()
		}
		l.Refresh()
		return
	}
	if l.window == nil {
		// No window to hide: do not let the close become a second kind of
		// exit with no wrap-up.
		log.Printf("youkai: close requested without a window; keeping the process")
		e.PreventDefault()
		return
	}
	l.window.Hide()
	e.PreventDefault()
}

// OnBeforeQuit begins the explicit-exit wrap-up. It always runs, regardless
// of the close preference: a menu quit, ⌘Q or the tray's exit must never
// turn into a hide. The quit is prevented until the wrap-up settled; then
// OnExitSettled asks MyGo to quit again.
func (l *windowLifecycle) OnBeforeQuit(e preventableEvent) {
	if !l.model.AllowClose() {
		e.PreventDefault()
		// A refusal that did not start an exit is a prompt the user must
		// see: an unsaved editor or an in-flight review save. A tray quit
		// may have arrived while the window was hidden, so restore it.
		// A running wrap-up is not forced into view.
		if !l.model.Exiting() {
			l.Restore()
		}
	}
	l.Refresh()
}

// OnSecondInstance wakes the existing instance for a forwarded command
// line. MyGo also delivers any URL argument through OnOpenURL, so this
// handler only has to restore the window; it never starts a scan.
func (l *windowLifecycle) OnSecondInstance(args []string, _ string) {
	// The forwarded command line is not logged: a URL argument may carry
	// query data that does not belong in a diagnostic file.
	log.Printf("youkai: second instance handed over %d argument(s); restoring the main window", len(args))
	l.Restore()
}

// OnOpenURL handles a deep link. Only youkai://open (with any query) is
// recognized; unknown schemes and routes are logged and ignored, and no URL
// can start a scan. The log keeps the scheme, host and path but never the
// query, which may carry data the app does not need or store.
func (l *windowLifecycle) OnOpenURL(raw string) {
	if !isOpenURL(raw) {
		log.Printf("youkai: ignoring unsupported deep link %s", redactedURL(raw))
		return
	}
	l.Restore()
}

// redactedURL renders the route parts of a URL for a diagnostic line,
// dropping the query, fragment and user info.
func redactedURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		return "(unparseable)"
	}
	out := u.Scheme + "://" + u.Host + u.Path
	if u.Host == "" {
		out = u.Scheme + ":" + u.Opaque
	}
	return out
}

// OnActivate restores the window when the app is activated (the macOS Dock
// icon) with nothing visible; a visible window is left alone.
func (l *windowLifecycle) OnActivate(hasVisibleWindows bool) {
	if !hasVisibleWindows {
		l.Restore()
	}
}

// OnExitFailed keeps a failed save visible: an exit requested while the
// window was hidden restores it with the retry.
func (l *windowLifecycle) OnExitFailed() {
	l.Restore()
}

// Refresh reconciles the tray menu with the model's current state. The
// subscription pump calls it after every applied snapshot; event handlers
// call it after exit-state changes.
func (l *windowLifecycle) Refresh() {
	if l.tray == nil {
		return
	}
	status := l.model.LifecycleStatus()
	if l.statusKnown && status == l.status {
		return
	}
	l.status = status
	l.statusKnown = true
	l.tray.SetMenu(l.trayMenu())
	l.tray.SetToolTip("Youkai · " + status.Text)
}

// trayMenu builds the menu the icon shows: status, open, stop and exit.
// Stopping is disabled when no session is active and is only ever the scan
// stop — it does not quit the application.
func (l *windowLifecycle) trayMenu() *mygo.Menu {
	status := l.model.LifecycleStatus()
	return mygo.NewMenu([]*mygo.MenuItem{
		{Label: "Youkai · " + status.Text, Disabled: true},
		mygo.Separator(),
		{Label: "打开主窗口", Click: func(*mygo.MenuItem, *mygo.Window) { l.Restore() }},
		{Label: "停止扫描", Disabled: !status.Busy, Click: func(*mygo.MenuItem, *mygo.Window) { l.model.Stop() }},
		mygo.Separator(),
		{Label: "退出", Click: func(*mygo.MenuItem, *mygo.Window) { l.quitApp() }},
	})
}

// isOpenURL reports whether raw is the supported youkai://open link. Query
// parameters are ignored: the route carries no commands or paths.
func isOpenURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if !strings.EqualFold(u.Scheme, openURLScheme) || !strings.EqualFold(u.Host, openURLRoute) {
		return false
	}
	return u.Path == "" || u.Path == "/"
}
