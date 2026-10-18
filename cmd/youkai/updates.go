// The wiring of MyGo's updater behind the UI's update seam
// (ui.UpdateSource). `mygo build` embeds the update feed and the public
// key into the binary when mygo.json carries updates; builds without them
// — development runs (`mygo dev`), test packages the Package workflow
// builds without the updates configuration — report disabled, which the
// update area explains instead of treating as an error. Nothing here runs
// at process start: the UI asks, and the checks run in its background
// work. Every stage the adapter drives is logged (see mygoUpdates), so an
// upgrade rehearsal can be followed in the diagnostic log.
package main

import (
	"context"
	"log"

	"github.com/LoTwT/youkai/internal/ui"
	"github.com/egoist/mygo"
)

// mygoUpdates adapts the mygo.Updater singleton to ui.UpdateSource. The
// update flow itself writes no log of its own, so every stage MyGo drives
// is logged here (check outcomes, install start, each completed download,
// the outcome), making an upgrade rehearsal followable line by line in the
// diagnostic log (docs/release.md 升级实测清单).
type mygoUpdates struct{}

// Enabled reports whether this build can update itself: built with an
// update feed and key, and able to write where it is installed.
func (mygoUpdates) Enabled() bool { return mygo.Updater.Enabled() }

// Check returns the newer version MyGo finds, nil when this build is up
// to date. The outcome — found, up to date, or the failure as it is — is
// logged before it reaches the UI.
func (mygoUpdates) Check(ctx context.Context) (ui.UpdateHandle, error) {
	current := mygo.App.Version()
	u, err := mygo.Updater.Check(ctx)
	switch {
	case err != nil:
		log.Printf("youkai: update check failed: %v", err)
	case u == nil:
		log.Printf("youkai: update check: no version newer than %s", current)
	default:
		log.Printf("youkai: update check: found %s (running %s)", u.Version, current)
	}
	if err != nil || u == nil {
		return nil, err
	}
	return mygoUpdate{u}, nil
}

// mygoUpdate adapts *mygo.Update: its fields become the accessors of
// ui.UpdateHandle.
type mygoUpdate struct{ u *mygo.Update }

func (h mygoUpdate) Version() string { return h.u.Version }
func (h mygoUpdate) Notes() string   { return h.u.Notes }

// downloadLog turns MyGo's raw progress reports into one log line per
// completed download. The progress of one download ends with the finished
// total reported twice — once when the last bytes arrive and once more on
// EOF — and a failed delta update makes MyGo download the whole archive
// with the progress starting over, so a completion is remembered until an
// in-progress report (below the total) means another download has begun.
type downloadLog struct {
	logged bool // the current download's completion already has its line
}

// report takes one progress report of the current download: the completion
// line is written at the first report at or past the total, and only a
// report below the total (a download in progress) arms the next one.
func (d *downloadLog) report(downloaded, total int64) {
	if total > 0 && downloaded >= total {
		if !d.logged {
			log.Printf("youkai: update install: downloaded %d bytes", downloaded)
			d.logged = true
		}
		return
	}
	// No size known, or the download is still running: the next completion
	// belongs to a download that has no line yet.
	d.logged = false
}

// Install follows MyGo's download, signature verification and replacement
// flow; the running app is not affected until the entry point relaunches
// it. The start, each completed download with its bytes — once per
// download, so a failed delta update that falls back to the whole archive
// leaves one line per download — and the outcome are logged; the progress
// reaches the UI unchanged.
func (h mygoUpdate) Install(ctx context.Context, progress func(downloaded, total int64)) error {
	log.Printf("youkai: update install: downloading and verifying %s", h.u.Version)
	var downloads downloadLog
	err := h.u.Install(ctx, func(downloaded, total int64) {
		downloads.report(downloaded, total)
		if progress != nil {
			progress(downloaded, total)
		}
	})
	if err != nil {
		log.Printf("youkai: update install failed: %v", err)
		return err
	}
	log.Printf("youkai: update install: %s installed", h.u.Version)
	return nil
}
