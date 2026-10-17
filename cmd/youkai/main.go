// Command youkai is the entry point of the Youkai application. It only
// assembles the application service and the main window: the service owns
// sessions, saving and the exit wrap-up, the window's model (internal/ui)
// renders its snapshots, and this file wires the two — the subscription
// pump, the evidence loader, the start availability and the close-as-
// explicit-exit behavior.
package main

import (
	"fmt"
	"log"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/recognition"
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

	// The data-root occupancy must be held before Run and everything
	// after it: the framework reads from PathUserData as Run starts, and
	// the service reads settings, checkpoints and history under the same
	// root. A second youkai process on the same data root — under any
	// path alias — fails here and exits; MyGo's own single-instance lock
	// does not cover this occupancy. The service takes over the lock for
	// the explicit-exit wrap-up; the deferred release stays as the crash
	// fallback (Close is idempotent).
	dataLock, err := app.AcquireDataRootLock(paths.Data)
	if err != nil {
		log.Fatalf("youkai: %v", err)
	}
	defer dataLock.Close()
	log.Printf("youkai: data-root lock acquired on %q", paths.Data)

	// The diagnostic log file is opened only now, under the data-root
	// occupancy: a second instance fails the acquire above and exits
	// before touching the log directory, so two processes never interleave
	// or truncate each other's lines. The alternative — per-process file
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

	source := devScanSource()
	svc, err := app.NewService(app.Config{
		Paths:    paths,
		DataLock: dataLock,
		Source:   source,
	})
	if err != nil {
		log.Fatalf("youkai: %v", err)
	}

	model := ui.New(ui.Config{
		Service: svc,
		ScanSource: ui.ScanSourceInfo{
			Available: source != nil,
			Label:     "开发数据源",
			Reason:    "当前构建没有接入游戏扫描源（M2 接入游戏适配）；开发构建可通过环境变量 YOUKAI_DEV_SOURCE 启用开发数据源。",
		},
		Evidence: ui.FileEvidence{Root: paths.Data},
		// The auto-update corner: MyGo's updater behind the UI's seam, and
		// the restart after a successful install. Relaunch runs the normal
		// quit sequence — a session whose final save keeps failing refuses
		// it, the window stays with its retry, and the installed update
		// runs at the next launch.
		Updates:           mygoUpdates{},
		OnUpdateInstalled: func() { mygo.App.Relaunch() },
		// A settled explicit exit closes the application: the model's
		// quit goroutine already waited for the wrap-up, so this Quit
		// re-enters the allowed path (the model allows the close) and the
		// event loop ends. A failed wrap-up never reaches here — the
		// window stays with the save failure instead.
		OnExitSettled: func() { mygo.App.Quit() },
	})

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
	// M1 treats closing the main window as the explicit exit (the two
	// close modes, the tray and deep links are M3): every close and quit
	// attempt joins the one wrap-up, which shows its progress in the
	// window and may refuse to close until the final save settled.
	mygo.App.OnBeforeQuit(func(e *mygo.QuitEvent) {
		if !model.AllowClose() {
			e.PreventDefault()
		}
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
			if !model.AllowClose() {
				e.PreventDefault()
			}
		})
		model.Attach(win)

		// One update check in the background after startup (2026-10-17
		// 决定): it neither blocks the ready callback — the check runs in
		// the model's own background work — nor activates the window; its
		// result lands through Update, which only redraws. Builds without
		// updates ignore it (the area explains that state instead).
		model.CheckUpdates()

		// The subscription pump: a notification only means "something
		// changed", so the pump re-fetches the authoritative snapshot and
		// hands it to the model on the UI thread, which discards stale
		// session revisions itself. Background progress never activates
		// the window — Update only redraws.
		sub := svc.Subscribe()
		initial := sub.Initial
		win.Update(func() { model.Apply(initial) })
		go func() {
			for range sub.Changes {
				snap := svc.Snapshot()
				win.Update(func() { model.Apply(snap) })
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

// devScanSource wires the development data source when the explicit
// environment switch names an observations directory (its lines.tsv and
// the crop images it lists). It exists so a scan can be driven before the
// game adapters of M2 exist — development and tests only; installed
// builds run without it and the service then refuses starts with a clear
// error. It is not a user-facing image import feature.
func devScanSource() app.ScanSource {
	dir := os.Getenv("YOUKAI_DEV_SOURCE")
	if dir == "" {
		return nil
	}
	src, err := app.LoadDevScanSource(dir)
	if err != nil {
		log.Fatalf("youkai: dev scan source %q: %v", dir, err)
	}
	// A development-only pace override (a Go duration like "3s"), for
	// manual runs that need the scan to last long enough to stop it
	// midway.
	if raw := os.Getenv("YOUKAI_DEV_PACE"); raw != "" {
		pace, err := time.ParseDuration(raw)
		if err != nil || pace < 0 {
			log.Fatalf("youkai: dev pace %q: %v", raw, err)
		}
		src.Pace = pace
	}
	log.Printf("youkai: dev scan source wired to %q (pace %s)", dir, src.Pace)
	return src
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
