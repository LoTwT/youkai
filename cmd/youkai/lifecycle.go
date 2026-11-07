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
	FlashFrame(bool)
	// Update posts a UI-thread task that also renders a frame. The close
	// callback and a notification click change model state after the last
	// input event, so without it the dialog they opened would wait for the
	// next mouse move or keystroke.
	Update(func())
}

// notificationSurface is one native notification; *mygo.Notification
// implements it. Tests substitute a recorder so the two-stage order is
// observable without a notification area.
type notificationSurface interface {
	Show() error
	Close()
}

// traySurface is the tray/menu bar surface the lifecycle updates;
// *mygo.Tray implements it. Tests substitute a recorder.
type traySurface interface {
	SetMenu(*mygo.Menu)
	SetToolTip(string)
	// OnClick registers the callback for a click that the platform does not
	// turn into a menu. Windows fires it on every left-button release, so a
	// double click fires it twice; macOS shows the menu instead and does not
	// fire it while a menu is installed.
	OnClick(func()) (off func())
	// Bounds reports where the platform put the icon, empty when it cannot
	// locate it. It is diagnostic only: on Windows an icon in the
	// notification area's overflow has no rectangle (Shell_NotifyIconGetRect
	// fails), so an empty rectangle does not prove the entry is unusable.
	Bounds() mygo.Rectangle
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
	ExitAnyway()
	Stop()
	PrepareRestore()
	Exiting() bool
	CloseBehavior() app.CloseBehavior
	LifecycleStatus() ui.LifecycleStatus
	SetTrayUnavailable(string)
	FirstHideHintDue() bool
	ShowFirstHideHint()
	OpenNotification(string)
}

// windowLifecycle owns the window lifecycle decisions the entry point
// wires to MyGo: close-to-tray versus close-to-exit, the first-hide hint,
// explicit quit, restore from the tray/Dock/second instance/deep links, the
// tray menu, and the two-stage scan notifications. It is confined to the UI
// thread, like the model it drives; the single-instance, notification-click
// and URL callbacks arrive on that thread.
type windowLifecycle struct {
	model  lifecycleModel
	window windowSurface
	tray   traySurface

	// newTray, newNotification and quitApp are seams: production uses
	// MyGo's tray, notification and quit, tests record the calls without
	// native objects.
	newTray         func(mygo.TrayOptions) (traySurface, error)
	newNotification func(mygo.NotificationOptions) notificationSurface
	quitApp         func()

	// firstNotification is the input-stop notification a result replaces.
	// MyGo's Windows backend does not replace an ID, so the second segment
	// must close this one before it shows.
	firstNotification notificationSurface

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
		newNotification: func(opts mygo.NotificationOptions) notificationSurface {
			return mygo.NewNotification(opts)
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
//
// The outcome is always logged. On Windows, a returned tray is not proof of
// a visible icon: MyGo v0.6.2's NewTray only records the options and lets a
// later posted message call Shell_NotifyIcon, whose result it discards
// (internal/windows/tray.go:83-124). Asking where the icon is forces that
// add and records whether the shell can locate the icon; the answer is a
// diagnostic, not a usable/unusable decision.
func (l *windowLifecycle) CreateTray() {
	if l.tray != nil {
		return
	}
	tray, err := l.newTray(mygo.TrayOptions{
		Icon: trayIconPNG,
		// macOS tints a template image to match the menu bar; elsewhere the
		// icon keeps its own colors.
		IconIsTemplate: runtime.GOOS == "darwin",
		ToolTip:        l.model.LifecycleStatus().TrayMenu().Status,
		Menu:           l.trayMenu(),
	})
	if err == nil && tray == nil {
		err = errors.New("the platform returned no tray")
	}
	if err != nil {
		l.trayUnavailable(err)
		return
	}
	l.tray = tray
	// Windows delivers a left-button release as a tray click even when the
	// icon has a menu, and the platform has no separate double-click event:
	// a double click is two clicks. Restore is safe to repeat — PrepareRestore
	// only asks the same idempotent stop, and re-showing the visible window
	// has no lasting effect — and every other restore route uses it unchanged.
	// macOS shows the installed menu on a click instead of invoking the
	// callback, so its behavior does not change.
	tray.OnClick(l.Restore)
	l.status = l.model.LifecycleStatus()
	l.statusKnown = true
	l.Refresh()
	l.logTrayBounds("created")
}

// trayUnavailable records that the close-to-tray preference has no entry to
// return through: closing runs the explicit-exit wrap-up and the window says
// why.
func (l *windowLifecycle) trayUnavailable(err error) {
	log.Printf("youkai: tray unavailable (%s): %v; closing the window will stop the scan and quit the application", runtime.GOOS, err)
	l.model.SetTrayUnavailable(err.Error())
}

// logTrayBounds writes the icon's location, or why it has none, for the
// event that needs it. It never changes the close behavior: on Windows an
// icon in the notification area's overflow has no rectangle, and hiding must
// still work for it.
func (l *windowLifecycle) logTrayBounds(event string) {
	bounds := l.tray.Bounds()
	if bounds.Width > 0 && bounds.Height > 0 {
		log.Printf("youkai: tray %s: icon at %d,%d %dx%d", event, bounds.X, bounds.Y, bounds.Width, bounds.Height)
		return
	}
	if runtime.GOOS == "windows" {
		log.Printf("youkai: tray %s: no icon rectangle; Windows 11 keeps new icons in the notification area's overflow, where Shell_NotifyIconGetRect fails, so this alone is not a failure", event)
		return
	}
	log.Printf("youkai: tray %s: no screen rectangle reported yet", event)
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
		if l.tray == nil {
			// The log says which decision the close took: a report of a
			// hidden window with no tray starts here.
			log.Printf("youkai: close requested without a tray entry; running the exit wrap-up instead of hiding")
		}
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
	if l.model.FirstHideHintDue() {
		// The hint must be acknowledged before the first hide: once the
		// window is gone, a message box could not be reliably shown. The
		// close callback renders no frame of its own, so ask for one.
		e.PreventDefault()
		l.model.ShowFirstHideHint()
		l.redraw()
		return
	}
	l.HideToTray()
	e.PreventDefault()
}

// HideToTray hides the window through the tray entry. It is the same action
// the first-hide hint's acknowledgement performs and keeps the tray location
// diagnostic at every hide.
func (l *windowLifecycle) HideToTray() {
	if l.window == nil || l.tray == nil {
		return
	}
	l.logTrayBounds("at close")
	l.window.Hide()
	l.Refresh()
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

// Notify sends one two-stage scan notification. The result segment closes
// the input-stop notification first: MyGo's Windows backend never replaces
// an existing notification with the same ID, it adds a temporary tray icon
// and leaves the old bubble. The second segment also flashes the taskbar
// button as the fallback when Windows suppresses the banner.
func (l *windowLifecycle) Notify(n ui.Notification) {
	if n.Flash && l.window != nil {
		l.window.FlashFrame(true)
	}
	if n.Segment == ui.NotificationResult && l.firstNotification != nil {
		l.firstNotification.Close()
		l.firstNotification = nil
	}
	note := l.newNotification(mygo.NotificationOptions{
		ID:    n.ID,
		Title: n.Title,
		Body:  n.Body,
	})
	if n.Segment == ui.NotificationInputStopped {
		// Register before Show: on macOS Show waits on the main thread and
		// keeps processing events, so the result segment can arrive while
		// this call is still running and must find the first one here.
		l.firstNotification = note
	}
	if err := note.Show(); err != nil {
		log.Printf("youkai: show notification %q: %v", n.ID, err)
	}
}

// OnNotificationClick opens the record a notification names. It restores
// the window through the one restore route (which stops a live scan, never
// starts one) and lets the model choose between the scan page's wrap-up
// state and the record summary.
func (l *windowLifecycle) OnNotificationClick(id string) {
	recordID, _, ok := ui.ParseNotificationID(id)
	if !ok {
		log.Printf("youkai: ignoring a notification click outside this application")
		return
	}
	l.Restore()
	l.model.OpenNotification(recordID)
	// A click on an already visible window changes the page or opens a
	// prompt after the last native event; request the frame that shows it.
	l.redraw()
}

// redraw requests one frame after model state changed outside an input
// event. The close callback and a notification click both arrive with no
// follow-up event, so the dialog or page they chose would otherwise remain
// invisible until the user moved the mouse.
func (l *windowLifecycle) redraw() {
	if l.window != nil {
		l.window.Update(func() {})
	}
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
	l.tray.SetToolTip(status.TrayMenu().Status)
}

// trayMenu builds the menu the icon shows: the disabled status line, open,
// stop and exit. The labels and the stop item's enabled state follow the
// lifecycle states in 08 §7; stopping is only ever the scan stop and never
// quits the application.
func (l *windowLifecycle) trayMenu() *mygo.Menu {
	labels := l.model.LifecycleStatus().TrayMenu()
	exit := func(*mygo.MenuItem, *mygo.Window) { l.quitApp() }
	if labels.Exit == "仍然退出" {
		// Only this tray item takes the forced settled-exit path. Ctrl+Q and
		// the window close keep reopening the failure dialog (PR-19b,
		// decision 2). A required unsaved prompt must be visible even when
		// the window is hidden.
		exit = func(*mygo.MenuItem, *mygo.Window) {
			l.model.ExitAnyway()
			if !l.model.Exiting() {
				l.Restore()
			}
			l.redraw()
		}
	}
	return mygo.NewMenu([]*mygo.MenuItem{
		{Label: labels.Status, Disabled: true},
		mygo.Separator(),
		{Label: labels.Open, Click: func(*mygo.MenuItem, *mygo.Window) { l.Restore() }},
		{Label: labels.Stop, Disabled: !labels.StopEnabled, Click: func(*mygo.MenuItem, *mygo.Window) { l.model.Stop() }},
		mygo.Separator(),
		{Label: labels.Exit, Click: exit},
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
