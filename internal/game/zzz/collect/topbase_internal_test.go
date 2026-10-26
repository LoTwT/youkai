package collect

import (
	"context"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/platform"
)

// TestAwaitTopBaseWaitsForStationaryPair pins the pacing fix: a fresh
// frame that still disagrees with the top-claim frame is not used as the
// confirmation base; the next stationary pair is.
func TestAwaitTopBaseWaitsForStationaryPair(t *testing.T) {
	inv := &Inventory{
		Layout: MacCloudLayout(),
		Matcher: &scrollScriptMatcher{
			rows:    5,
			columns: 4,
			verdict: map[scrollPair]ScrollMatch{
				{before: 1, after: 2}: {Lost: true, Evidence: "still settling"},
				{before: 2, after: 3}: {Stationary: true, Evidence: "settled"},
			},
		},
		ActionBudget: time.Second,
		RecheckEvery: time.Millisecond,
	}
	view := &scrollScriptView{frames: []platform.Frame{
		{ID: 2, Geometry: 1, ArrivedAt: time.Now()},
		{ID: 3, Geometry: 1, ArrivedAt: time.Now()},
	}}
	run := &inventoryRun{
		inv:       inv,
		view:      view,
		geo:       1,
		position:  1,
		lastFrame: platform.Frame{ID: 1, Geometry: 1, ArrivedAt: time.Now()},
	}

	if err := run.awaitTopBase(context.Background()); err != nil {
		t.Fatalf("awaitTopBase: %v", err)
	}
	if view.calls != 2 {
		t.Fatalf("awaitTopBase consumed %d frames, want two before a stationary pair", view.calls)
	}
	if run.lastFrame.ID != 3 {
		t.Fatalf("top base frame = %d, want the settled frame 3", run.lastFrame.ID)
	}
}
