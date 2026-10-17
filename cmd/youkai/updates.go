// The wiring of MyGo's updater behind the UI's update seam
// (ui.UpdateSource). `mygo build` embeds the update feed and the public
// key into the binary when mygo.json carries updates; builds without them
// — development runs (`mygo dev`), test packages the Package workflow
// builds without the updates configuration — report disabled, which the
// update area explains instead of treating as an error. Nothing here runs
// at process start: the UI asks, and the checks run in its background
// work.
package main

import (
	"context"

	"github.com/LoTwT/youkai/internal/ui"
	"github.com/egoist/mygo"
)

// mygoUpdates adapts the mygo.Updater singleton to ui.UpdateSource.
type mygoUpdates struct{}

// Enabled reports whether this build can update itself: built with an
// update feed and key, and able to write where it is installed.
func (mygoUpdates) Enabled() bool { return mygo.Updater.Enabled() }

// Check returns the newer version MyGo finds, nil when this build is up
// to date.
func (mygoUpdates) Check(ctx context.Context) (ui.UpdateHandle, error) {
	u, err := mygo.Updater.Check(ctx)
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

// Install follows MyGo's download, signature verification and replacement
// flow; the running app is not affected until the entry point relaunches.
func (h mygoUpdate) Install(ctx context.Context, progress func(downloaded, total int64)) error {
	return h.u.Install(ctx, progress)
}
