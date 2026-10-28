package collect

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/platform"
	"github.com/LoTwT/youkai/internal/scan"
)

// TestConfirmBottomLostStopsInsteadOfCommittingIt pins the bottom
// confirmation's Lost branch: a drag that loses the overlap proves neither
// the end nor stillness, so the traversal must stop with navigationMismatch
// and keep the last verified frame. Accepting it as the stationary bottom
// confirmation would declare a mid-list position complete and drop the rest
// of the position's items.
func TestConfirmBottomLostStopsInsteadOfCommittingIt(t *testing.T) {
	l := MacCloudLayout()
	view := &scrollScriptView{
		frames:     []platform.Frame{scrollFrame(2)},
		blockAfter: true,
	}
	m := &scrollScriptMatcher{rows: l.Rows, columns: l.Columns, verdict: map[scrollPair]ScrollMatch{
		{1, 2}: {Lost: true, Evidence: "scripted bottom drag lost the overlap"},
	}}
	run := newScriptedScrollRun(l, view, m, "", -1, 40*time.Millisecond)
	run.lastFrame = scrollFrame(1)

	err := runScriptedSession(t, run, func(ctx context.Context, run *inventoryRun) error {
		_, err := run.confirmBottom(ctx)
		return err
	})
	if err == nil {
		t.Fatal("a bottom confirmation that lost the overlap returned as a confirmed end")
	}
	var fault *scan.FaultError
	if !errors.As(err, &fault) || fault.Code != CodeNavigationMismatch {
		t.Fatalf("bottom confirmation error %v, want navigationMismatch", err)
	}
	if run.lastFrame.ID != 1 {
		t.Fatalf("the lost confirmation committed frame %d, want the last verified frame 1", run.lastFrame.ID)
	}
	if run.listMatch.List != nil {
		t.Fatalf("the lost confirmation committed list evidence: %+v", run.listMatch.List)
	}
}
