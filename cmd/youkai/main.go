// Command youkai is the entry point of the Youkai application. It only
// assembles the app and opens the main window: the application service
// owns sessions, saving and the exit wrap-up, and the MyGo views of the
// next milestone call into it.
package main

import (
	"fmt"
	"log"
	"os"
	"sync"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
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

	svc, err := app.NewService(app.Config{
		Paths:    paths,
		DataLock: dataLock,
		Source:   devScanSource(),
	})
	if err != nil {
		log.Fatalf("youkai: %v", err)
	}

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
				// nothing retries it in this headless milestone, so the
				// failure is reported and the process exits with the
				// last durable checkpoint standing.
				log.Printf("youkai: exit wrap-up incomplete: %v", err)
			}
		})
	}
	mygo.App.OnBeforeQuit(func(e *mygo.QuitEvent) { go shutdown() })

	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{
			Title:   "Youkai",
			Width:   480,
			Height:  320,
			Content: ui.View(view),
		})
	})
	if os.Getenv("YOUKAI_DEV_AUTOSTART") != "" {
		// Development-only auto start: the interface that starts scans
		// arrives with the next milestone, so a full run is driven by an
		// explicit environment switch. The ready callback stays fast —
		// the start, and with it the first model load, runs on its own
		// goroutine.
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
// the crop images it lists). It exists so a scan can be driven before
// the game adapters of M2 exist — development and tests only; installed
// builds run without it and the service then refuses starts with a
// clear error. It is not a user-facing image import feature.
func devScanSource() app.ScanSource {
	dir := os.Getenv("YOUKAI_DEV_SOURCE")
	if dir == "" {
		return nil
	}
	src, err := app.LoadDevScanSource(dir)
	if err != nil {
		log.Fatalf("youkai: dev scan source %q: %v", dir, err)
	}
	log.Printf("youkai: dev scan source wired to %q", dir)
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

// view is the placeholder content of the main window until the real
// interface is built.
func view(c *ui.Context) {
	ui.Column(c).Fill().Center().Children(func() {
		ui.Text(c, "Youkai")
	})
}
