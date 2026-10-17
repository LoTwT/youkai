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
	"sync"
	"time"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/ui"
	"github.com/egoist/mygo"
	mgui "github.com/egoist/mygo/ui"
)

func main() {
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
