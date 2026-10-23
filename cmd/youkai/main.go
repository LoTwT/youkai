// Command youkai is the entry point of the Youkai application. It only
// assembles the application service and the main window: the service owns
// sessions, saving and the exit wrap-up, the window's model (internal/ui)
// renders its snapshots, and this file wires the two — the subscription
// pump, the evidence loader, the start availability and the window
// lifecycle (single instance, close-to-tray or close-to-exit, tray menu and
// deep links).
package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/game/zzz/collect"
	"github.com/LoTwT/youkai/internal/platform"
	"github.com/LoTwT/youkai/internal/recognition"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/LoTwT/youkai/internal/ui"
	"github.com/egoist/mygo"
	mgui "github.com/egoist/mygo/ui"
)

func main() {
	// The install-time recognition self-check: an explicit environment
	// switch (any non-empty YOUKAI_SELFTEST) turns the process into a
	// one-shot check of the artifacts this build actually ships — load the
	// bundled ONNX Runtime library and model from the resolved resources
	// root, recognize one embedded sample, then exit. It runs before the
	// application service, the locks and any user data, so verifying an
	// installed app (DMG copy, Windows install directory) never touches
	// them. This is a development and packaging switch, not a user-facing
	// image import feature.
	if os.Getenv("YOUKAI_SELFTEST") != "" {
		selfTest(resourcesRoot())
		return
	}

	paths, err := app.Resolve(app.Options{
		Name:      mygo.App.Name(),
		Resources: resourcesRoot(),
	})
	if err != nil {
		log.Fatal(err)
	}
	// The overrides must be in place before Run: as Run starts, the
	// framework already reads its locale file and update state from
	// PathUserData, and its Windows defaults would put them in %APPDATA%
	// instead of the local app data youkai uses.
	mygo.App.SetPath(mygo.PathUserData, paths.Data)
	mygo.App.SetPath(mygo.PathCache, paths.Cache)
	mygo.App.SetPath(mygo.PathLogs, paths.Logs)
	log.Printf("youkai: data=%q cache=%q logs=%q resources=%q",
		paths.Data, paths.Cache, paths.Logs, paths.Resources)

	// A launch that reaches the single-instance socket hands its command
	// line to the running process and exits there, before it can touch the
	// data root: otherwise the occupancy below would reject it first and no
	// restore or deep link could be forwarded. The data-root occupancy must
	// then be held before Run and everything after it: the framework reads
	// from PathUserData as Run starts, and the service reads settings,
	// checkpoints and history under the same root. A second youkai process
	// on the same data root — under any path alias — fails the occupancy and
	// exits; MyGo's single-instance lock does not cover this occupancy. The
	// service takes over the occupancy for the explicit-exit wrap-up; the
	// deferred release stays as the crash fallback (Close is idempotent).
	dataLock, err := acquireInstanceLocks(paths.Data, instanceLockOps{
		requestSingleInstance: mygo.App.RequestSingleInstanceLock,
		acquireDataRoot:       app.AcquireDataRootLock,
	})
	if errors.Is(err, errSecondInstance) {
		log.Printf("youkai: another instance is running; this launch was handed over to it")
		return
	}
	if err != nil {
		log.Fatalf("youkai: %v", err)
	}
	defer dataLock.Close()
	log.Printf("youkai: data-root lock acquired on %q", paths.Data)

	// The single-instance handlers must be registered before Run so the
	// URLs of the launch reach the existing entry points. The lifecycle is
	// created with the model below; callbacks run on the UI thread after
	// this function's setup, so the deferred assignment is safe.
	var life *windowLifecycle
	mygo.App.OnSecondInstance(func(args []string, workingDir string) {
		if life != nil {
			life.OnSecondInstance(args, workingDir)
		}
	})
	mygo.App.OnOpenURL(func(raw string) {
		if life != nil {
			life.OnOpenURL(raw)
		}
	})
	mygo.App.OnActivate(func(hasVisibleWindows bool) {
		if life != nil {
			life.OnActivate(hasVisibleWindows)
		}
	})

	// The diagnostic log file is opened only now, under the data-root
	// occupancy: a second instance exits at the single-instance lock above
	// (or, under another app name, at the data-root acquire) before touching
	// the log directory, so two processes never interleave or truncate each
	// other's lines. The alternative — per-process file
	// names — would leave one file per start in the directory and make
	// "attach the log" mean picking among many. The cost is that the lines
	// above, and lock failures, reach stderr only. The sink becomes the
	// logger's whole output: it writes the file first and mirrors to
	// stderr afterwards, discarding the mirror's errors — an installed
	// Windows process runs on the GUI subsystem with no console, where
	// the stderr handle is invalid, and a stderr-first MultiWriter would
	// stop at that failure and keep every line out of the file. stderr
	// keeps working for development and redirection.
	logFile, err := app.OpenDiagnosticLog(paths.Logs)
	if err != nil {
		// Not fatal: the app runs without a retrievable log, stderr still
		// carries everything, and one notice explains why there is none.
		log.Printf("youkai: diagnostic log unavailable, logging to stderr only: %v", err)
	} else {
		defer logFile.Close()
		log.SetOutput(logFile)
	}
	// The first file line of every run identifies the build a user-attached
	// log came from (T7's problem-report requirement): the version, the VCS
	// revision the binary was built from when the build carries it, the
	// platform and the data root.
	commit := app.BuildCommit()
	if commit == "" {
		commit = "unknown"
	}
	log.Printf("youkai: version=%s commit=%s platform=%s/%s data=%q",
		mygo.App.Version(), commit, runtime.GOOS, runtime.GOARCH, paths.Data)

	// The persisted preferences. A missing file is the default; a broken
	// one is logged and the defaults apply, so a bad preference can never
	// keep the application from starting.
	settings, err := app.LoadSettings(paths.Data)
	if err != nil {
		log.Printf("youkai: settings: %v (using defaults)", err)
	}
	log.Printf("youkai: close behavior=%s", settings.CloseBehavior)

	// "应用和功能" lists the app from the NSIS-written uninstall entry, and
	// the in-place updater never refreshes that registry value (MyGo
	// replaces the app's files only) — keep DisplayVersion at the running
	// version so the list does not lag an update behind. Only the
	// production build owns the entry: development builds ("Youkai Dev")
	// and rehearsal builds under other names must not rewrite it. The NSIS
	// uninstaller never reads the value (it deletes the whole key), so this
	// cannot disturb an uninstall; failures are harmless and logged. On
	// macOS there is no such entry and the call is a no-op.
	if mygo.App.Name() == "Youkai" && !mygo.IsDev() {
		if updated, err := app.RefreshUninstallDisplayVersion(mygo.App.Version()); err != nil {
			log.Printf("youkai: refresh the uninstall entry's version: %v", err)
		} else if updated {
			log.Printf("youkai: uninstall entry version refreshed to %s", mygo.App.Version())
		}
	}

	source, sourceLabel := scanSource()
	svc, err := app.NewService(app.Config{
		Paths:    paths,
		DataLock: dataLock,
		Source:   source,
	})
	if err != nil {
		log.Fatalf("youkai: %v", err)
	}

	// The installed update's restart. MyGo's Relaunch cancels itself when
	// anything refuses the quit — "Nothing happens when OnBeforeQuit … or
	// a window cancels the quit" (login.go) — and youkai's close
	// interception refuses the first quit of every exit so the wrap-up can
	// run (AllowClose starts it and says no). The relaunch the install
	// asks for is therefore cancelled by design, so the restart is re-
	// issued once the wrap-up settled: the quit is allowed then, and the
	// data-root occupancy is already released by the wrap-up's teardown,
	// so the relaunched process can take it. Both callbacks run on the UI
	// thread, as does everything they touch.
	updateInstalled := false

	model := ui.New(ui.Config{
		Service:    svc,
		PageLayout: true,
		ScanSource: ui.ScanSourceInfo{
			Available: source != nil,
			Label:     sourceLabel,
			Reason:    "当前构建没有接入游戏扫描源（M2 接入游戏适配）；开发构建可通过环境变量 YOUKAI_DEV_SOURCE 启用开发数据源。",
		},
		Evidence: ui.FileEvidence{Root: paths.Data},
		// Usable-data exports: the page shell writes straight into the
		// system downloads directory; the legacy layout keeps the save
		// dialog. Both write atomically outside the managed data root.
		ExportSaver:       fileExportSaver{dataRoot: paths.Data},
		DirectExportSaver: fileExportSaver{dataRoot: paths.Data},
		OpenDownloadsDirectory: func() error {
			dir, err := mygo.App.Path(mygo.PathDownloads)
			if err != nil {
				return err
			}
			return mygo.Shell.OpenPath(dir)
		},
		OpenDataDirectory: func() error { return mygo.Shell.OpenPath(paths.Data) },
		// The auto-update corner: MyGo's updater behind the UI's seam. The
		// running version shows in the area ("当前版本 …"); DevAutoInstall
		// is the rehearsal switch (YOUKAI_DEV_AUTOUPDATE) that installs a
		// found update on its own, for driving the real install and
		// restart flow end to end (docs/development.md 自动更新的本地演
		// 练) — development only, never a user setting.
		Updates:        mygoUpdates{},
		AppVersion:     mygo.App.Version(),
		DevAutoInstall: os.Getenv("YOUKAI_DEV_AUTOUPDATE") != "",
		// The close preference is read at startup and persisted in the data
		// root; the window's setting row changes it. Closing hides by
		// default; quitting always runs the explicit-exit wrap-up.
		CloseBehavior: settings.CloseBehavior,
		SaveCloseBehavior: func(behavior app.CloseBehavior) error {
			err := app.SaveSettings(paths.Data, app.Settings{CloseBehavior: behavior})
			if err != nil {
				log.Printf("youkai: save settings: %v", err)
			}
			return err
		},
		// After a successful install the application restarts into the new
		// version: this first Relaunch only starts the exit — the close
		// interception cancels it (see updateInstalled above) after firing
		// the wrap-up — and the re-issued one below, with the wrap-up
		// settled, is the restart itself. A session whose final save keeps
		// failing still refuses the exit, the window stays with its retry,
		// and the installed update runs at the next launch.
		OnUpdateInstalled: func() {
			updateInstalled = true
			log.Printf("youkai: update installed, restarting the application")
			mygo.App.Relaunch()
		},
		// A settled explicit exit closes the application: the model's
		// quit goroutine already waited for the wrap-up, so quitting re-
		// enters the allowed path (the model allows the close) and the
		// event loop ends — relaunching instead when an update is
		// installed. A failed wrap-up never reaches here — the window
		// stays with the save failure instead.
		OnExitSettled: func() {
			life.Refresh()
			if updateInstalled {
				log.Printf("youkai: exit settled, relaunching into the installed update")
				mygo.App.Relaunch()
				return
			}
			mygo.App.Quit()
		},
		// A failed final save leaves the process alive. The restore entry
		// must become visible again even when the exit was requested from
		// the tray while the window was hidden.
		OnExitFailed: func() {
			life.OnExitFailed()
		},
	})
	life = newWindowLifecycle(model)

	// The explicit-exit wrap-up. The quit event only triggers it
	// asynchronously — a quit callback must not block the main thread on
	// session finalization, recognition or disk writes — and main joins
	// it after the event loop stopped, so the wrap-up always completes
	// before the process exits.
	var shutdownOnce sync.Once
	shutdown := func() {
		shutdownOnce.Do(func() {
			if err := svc.Quit(); err != nil {
				// A failed save stops the wrap-up short of the teardown;
				// with the window gone nothing retries it, so the failure
				// is reported and the process exits with the last durable
				// checkpoint standing.
				log.Printf("youkai: exit wrap-up incomplete: %v", err)
			}
		})
	}
	// Every explicit quit — menu, ⌘Q, the tray's exit and the updater's
	// restart — runs the one wrap-up regardless of the close preference.
	// The window's close handler applies the preference separately; a quit
	// already in progress is never turned into a hide.
	mygo.App.OnBeforeQuit(func(e *mygo.QuitEvent) {
		life.OnBeforeQuit(e)
	})

	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:     "Youkai",
			Width:     1020,
			Height:    700,
			MinWidth:  860,
			MinHeight: 560,
			Content:   mgui.View(model.View),
		})
		win.OnClose(func(e *mygo.CloseEvent) {
			life.OnWindowClose(e)
		})
		model.Attach(win)
		if width, height := win.ContentSize(); width > 0 && height > 0 {
			model.SetContentSize(width, height)
		}
		win.OnResize(func() {
			if width, height := win.ContentSize(); width > 0 && height > 0 {
				model.SetContentSize(width, height)
			}
		})
		// The window exists now: replay any restore that a second instance
		// or a launch-time deep link requested before ready, then add the
		// tray/menu bar entry (the fallback to close-to-exit is handled by
		// the lifecycle if creation fails).
		life.AttachWindow(win)
		life.CreateTray()

		// The update-check schedule (2026-10-17 决定): the startup check
		// stays, and a running application re-checks every
		// updateCheckInterval. It neither blocks the ready callback — the
		// checks run in the model's own background work — nor activates the
		// window; results land through Update, which only redraws. Builds
		// without updates start nothing (the area explains that state
		// instead).
		model.StartUpdateChecks()

		// The subscription pump: a notification only means "something
		// changed", so the pump re-fetches the authoritative snapshot and
		// hands it to the model on the UI thread, which discards stale
		// session revisions itself. Background progress never activates
		// the window — Update only redraws.
		sub := svc.Subscribe()
		initial := sub.Initial
		win.Update(func() {
			model.Apply(initial)
			life.Refresh()
		})
		go func() {
			for range sub.Changes {
				snap := svc.Snapshot()
				win.Update(func() {
					model.Apply(snap)
					life.Refresh()
				})
			}
		}()
	})
	if os.Getenv("YOUKAI_DEV_AUTOSTART") != "" {
		// Development-only auto start, for full runs without a click. The
		// ready callback stays fast — the start, and with it the first
		// model load, runs on its own goroutine.
		mygo.App.WhenReady(func() {
			go func() {
				req := app.StartRequest{ID: fmt.Sprintf("dev-autostart-%d", os.Getpid())}
				out, err := svc.Start(req)
				if err != nil {
					log.Printf("youkai: dev autostart: %v", err)
					return
				}
				log.Printf("youkai: dev autostart: scan %s (busy=%v replayed=%v)",
					out.SessionID, out.Busy, out.Replayed)
			}()
		})
	}
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
	shutdown()
}

// selfTest runs the install-time recognition check against root, the
// resolved resources root of this executable (Contents/Resources of a copied
// .app, the EXE's install directory on Windows, the staged development app
// under mygo dev). It logs the outcome with the loaded paths and exits the
// process non-zero on failure; the log goes to stderr, so run the executable
// from a terminal (or with redirection on Windows, whose GUI subsystem has
// no console) to see it.
func selfTest(root string) {
	// The updater state of this build, before anything else: both
	// packaging workflows assert it — the Package workflow builds without
	// the updates configuration (updates=disabled), the Release workflow
	// with the signing key (updates=enabled). Reporting it here keeps the
	// assertion on the very artifact that ships, not on the build log.
	if mygo.Updater.Enabled() {
		log.Printf("youkai: self-test: updates=enabled")
	} else {
		log.Printf("youkai: self-test: updates=disabled")
	}
	res, err := app.OCRResources(root)
	if err != nil {
		log.Fatalf("youkai: self-test: %v", err)
	}
	log.Printf("youkai: self-test: library=%q model=%q", res.LibraryPath, res.ModelPath)
	got, err := recognition.SelfCheck(res)
	if err != nil {
		log.Fatalf("youkai: self-test failed: %v", err)
	}
	if err := recognition.Shutdown(); err != nil {
		log.Fatalf("youkai: self-test: %v", err)
	}
	log.Printf("youkai: self-test passed: text=%q score=%.3f", got.Text, got.Score)
}

// scanSource wires the scan source the environment selects, and returns
// its UI label:
//
//   - YOUKAI_DEV_SOURCE names the development fixture directory (the
//     development and testing stand-in of M1);
//   - YOUKAI_GAME_SOURCE=observe wires the observe-only game rehearsal
//     through the unified platform port — a session opens the platform,
//     observes the target and waits; no input is dispatched;
//   - YOUKAI_GAME_SOURCE=traverse wires the calibrated macOS cloud
//     inventory traversal (darwin only; Windows has no calibrated
//     layout);
//   - YOUKAI_INDEPENDENT_CLEAR (any non-empty value) makes the traverse
//     preparation verify a clear funnel between reset and S selection —
//     the acceptance-only independent Clear check, which costs one extra
//     panel open/close per wheel position; unset keeps the daily path;
//   - YOUKAI_DEV_SOURCE and YOUKAI_GAME_SOURCE set at once is a wiring
//     error the entry point refuses;
//   - neither leaves the production state: no source, starts refused
//     with the documented reason.
func scanSource() (app.ScanSource, string) {
	source, label, err := scanSourceFrom(os.Getenv, runtime.GOOS, platform.Host)
	if err != nil {
		log.Fatalf("youkai: %v", err)
	}
	return source, label
}

// scanSourceFrom is scanSource with its environment, platform and host
// resolution injected, so the wiring and the platform split are testable
// without reading the process environment or touching a real display.
func scanSourceFrom(getenv func(string) string, goos string, host func() (platform.Port, error)) (app.ScanSource, string, error) {
	devDir := getenv("YOUKAI_DEV_SOURCE")
	gameMode := getenv("YOUKAI_GAME_SOURCE")
	if devDir != "" && gameMode != "" {
		return nil, "", errors.New("YOUKAI_DEV_SOURCE and YOUKAI_GAME_SOURCE are both set; pick one scan source")
	}
	if devDir != "" {
		return devScanSourceFrom(devDir, getenv)
	}
	if gameMode != "" {
		return gameScanSourceFrom(gameMode, goos, getenv, host)
	}
	return nil, "开发数据源", nil
}

// gameScanSourceFrom wires the game source for one mode and platform. The
// traverse mode is refused outside darwin: only the macOS cloud client has
// a calibrated layout, and guessing anchors would act on the wrong pixels.
func gameScanSourceFrom(mode, goos string, getenv func(string) string, host func() (platform.Port, error)) (app.ScanSource, string, error) {
	switch mode {
	case "observe", "traverse":
	default:
		return nil, "", fmt.Errorf("YOUKAI_GAME_SOURCE=%q is not supported (use \"observe\" or \"traverse\")", mode)
	}
	if mode == "traverse" && goos != "darwin" {
		return nil, "", fmt.Errorf("YOUKAI_GAME_SOURCE=traverse needs a calibrated layout; only the macOS cloud client has one (%s has none yet)", goos)
	}
	port, err := host()
	if err != nil {
		return nil, "", fmt.Errorf("platform: %w", err)
	}
	var (
		target platform.TargetSpec
		client record.Client
		hotkey platform.HotkeySpec
	)
	switch goos {
	case "darwin":
		target = collect.MacCloudTarget
		client = record.ClientMacOSCloud
		hotkey = platform.HotkeySpec{Key: platform.KeyS, Ctrl: true, Alt: true, Meta: true}
	case "windows":
		exe := getenv("YOUKAI_GAME_EXE")
		kind := getenv("YOUKAI_GAME_CLIENT")
		if exe == "" || (kind != "local" && kind != "cloud") {
			return nil, "", fmt.Errorf("the Windows game target needs YOUKAI_GAME_EXE=<game exe path> and YOUKAI_GAME_CLIENT=local|cloud (the real window identity is measured in field round ②; refusing to guess), got exe=%q client=%q", exe, kind)
		}
		target = platform.TargetSpec{ExecutablePath: exe}
		client = record.ClientWindowsCloud
		if kind == "local" {
			client = record.ClientWindowsLocal
		}
		hotkey = platform.HotkeySpec{Key: platform.KeyS, Ctrl: true, Alt: true, Shift: true}
	default:
		return nil, "", fmt.Errorf("no game platform on %s", goos)
	}
	src := &app.GameScanSource{
		Port:   port,
		Target: target,
		Hotkey: hotkey,
		Client: client,
	}
	if mode == "observe" {
		src.Mode = "observeOnly"
		log.Printf("youkai: game scan source wired (observe-only): client=%s target=%+v", client, target)
		return src, "游戏窗口（仅观测演练，不发送输入）", nil
	}
	// The calibrated traverse: one immutable layout shared by the session
	// traversals; each session gets its own matcher and Inventory.
	layout := collect.MacCloudLayout()
	independentClear := getenv("YOUKAI_INDEPENDENT_CLEAR") != ""
	src.Mode = "traverse"
	src.Traversal = func() collect.Traversal {
		return &collect.Inventory{
			Layout:           layout,
			Matcher:          collect.NewPixelMatcher(layout, collect.MacCloudCalibration()),
			Project:          app.ProjectDiscObservation,
			IndependentClear: independentClear,
		}
	}
	log.Printf("youkai: game scan source wired (traverse): client=%s target=%+v independentClear=%v",
		client, target, independentClear)
	return src, "游戏窗口（自动遍历）", nil
}

// devScanSourceFrom wires the development data source when the explicit
// environment switch names an observations directory (its lines.tsv and
// the crop images it lists). It exists so a scan can be driven before the
// game adapters of M2 exist — development and tests only; installed
// builds run without it and the service then refuses starts with a clear
// error. It is not a user-facing image import feature.
func devScanSourceFrom(dir string, getenv func(string) string) (app.ScanSource, string, error) {
	src, err := app.LoadDevScanSource(dir)
	if err != nil {
		return nil, "", fmt.Errorf("dev scan source %q: %w", dir, err)
	}
	// A development-only pace override (a Go duration like "3s"), for
	// manual runs that need the scan to last long enough to stop it
	// midway.
	if raw := getenv("YOUKAI_DEV_PACE"); raw != "" {
		pace, err := time.ParseDuration(raw)
		if err != nil || pace < 0 {
			return nil, "", fmt.Errorf("dev pace %q: %v", raw, err)
		}
		src.Pace = pace
	}
	log.Printf("youkai: dev scan source wired to %q (pace %s)", dir, src.Pace)
	return src, "开发数据源", nil
}

// resourcesRoot returns the app's shipped-resources directory as the
// framework resolves it: Contents/Resources of an app bundle, the
// executable's directory of an installed build, and the working
// directory's resources under go run and go test — which is why tests
// inject roots instead of relying on where they run.
func resourcesRoot() string {
	dir, err := mygo.App.Path(mygo.PathResources)
	if err != nil {
		log.Fatalf("youkai: resolve resources: %v", err)
	}
	return dir
}
