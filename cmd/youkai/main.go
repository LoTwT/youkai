// Command youkai is the entry point of the Youkai application. It only
// assembles the app and opens the main window; feature packages are wired
// in here as later milestones add them.
package main

import (
	"log"

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
	// later milestones read settings, checkpoints and history under the
	// same root. A second youkai process on the same data root — under
	// any path alias — fails here and exits; MyGo's own single-instance
	// lock does not cover this occupancy, and the friendly failure UI and
	// the hand-off to the running instance come with the application
	// service of a later milestone.
	dataLock, err := app.AcquireDataRootLock(paths.Data)
	if err != nil {
		log.Fatalf("youkai: %v", err)
	}
	defer dataLock.Close()
	log.Printf("youkai: data-root lock acquired on %q", paths.Data)

	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{
			Title:   "Youkai",
			Width:   480,
			Height:  320,
			Content: ui.View(view),
		})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
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
