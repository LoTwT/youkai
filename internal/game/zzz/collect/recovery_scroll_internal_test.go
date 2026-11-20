package collect

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/platform"
)

type recoveryWheelSequence struct {
	*scrollScriptMatcher
	finalWheel int
}

func (m recoveryWheelSequence) Match(f platform.Frame) PageMatch {
	page := m.scrollScriptMatcher.Match(f)
	if f.ID == 3 {
		page.Wheel.Position = m.finalWheel
	}
	return page
}

// The displacement candidate belongs to position 1. Its correspondence
// never settles. Only the last warehouse frame distinguishes these cases.
func TestRecoveryReplayLostCandidateRequiresLatestWheel(t *testing.T) {
	for _, tc := range []struct {
		name           string
		wheel          int
		positionScoped bool
	}{
		{"same position", 1, true},
		{"different position", 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			layout := sixRowScrollLayout()
			m := recoveryWheelSequence{&scrollScriptMatcher{
				rows: layout.Rows, columns: layout.Columns,
				verdict: map[scrollPair]ScrollMatch{
					{1, 2}: displacedScan(layout, 2),
					{2, 3}: {Undecided: true},
					{1, 3}: {Undecided: true},
				},
			}, tc.wheel}
			view := &scrollScriptView{frames: []platform.Frame{scrollFrame(1), scrollFrame(2), scrollFrame(3)}, blockAfter: true}
			run := newScriptedScrollRun(layout, view, m, "", 0, 40*time.Millisecond)
			err := runScriptedScroll(t, run)
			_, scoped := asPositionIncomplete(err)
			if view.calls != 3 {
				t.Fatalf("read %d frames, want the final wheel observation on frame 3", view.calls)
			}
			if tc.positionScoped && faultDiagnosticCode(err) != CodePositionLost {
				t.Fatalf("same-wheel proof loss returned %v", err)
			}
			if err == nil || scoped != tc.positionScoped || view.dispatches != 1 {
				t.Fatalf("last wheel=%d: error=%v positionScoped=%t drags=%d, want positionScoped=%t and one drag",
					tc.wheel, err, scoped, view.dispatches, tc.positionScoped)
			}
			if run.ledger.rowBase != 0 || run.viewport != 3 {
				t.Fatalf("the unsettled candidate advanced the ledger to row %d viewport %d", run.ledger.rowBase, run.viewport)
			}
		})
	}
}

func TestRecoveryReplayWrongWheelCannotSettleCandidate(t *testing.T) {
	for _, tc := range []struct {
		name string
		step func(context.Context, *inventoryRun) error
	}{
		{"scroll", func(ctx context.Context, run *inventoryRun) error { return run.scroll(ctx) }},
		{"bottom", func(ctx context.Context, run *inventoryRun) error {
			_, err := run.confirmBottom(ctx)
			return err
		}},
		{"top fling", func(ctx context.Context, run *inventoryRun) error {
			_, err := run.scrollToTop(ctx)
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			layout := sixRowScrollLayout()
			m := recoveryWheelSequence{&scrollScriptMatcher{
				rows: layout.Rows, columns: layout.Columns,
				verdict: map[scrollPair]ScrollMatch{
					{1, 2}: displacedScan(layout, 2),
					{2, 3}: {Stationary: true}, // Same-looking list on position 2.
					{2, 4}: {Stationary: true}, // Back to position 1.
				},
			}, 2}
			frames := []platform.Frame{scrollFrame(1), scrollFrame(2), scrollFrame(3), scrollFrame(4)}
			view := &scrollScriptView{frames: frames, blockAfter: true}
			run := newScriptedScrollRun(layout, view, m, "", 0, 40*time.Millisecond)
			run.lastFrame, run.listMatch = frames[0], m.Match(frames[0])
			if tc.name != "scroll" {
				view.frames = frames[1:]
			}
			err := runScriptedSession(t, run, tc.step)
			if _, scoped := asPositionIncomplete(err); scoped || faultDiagnosticCode(err) != CodeNavigationMismatch ||
				!strings.Contains(err.Error(), "changed the wheel position to 2") {
				t.Fatalf("observing a different wheel returned %v, want a session-fatal wheel fault", err)
			}
			if len(view.frames)-view.calls != 1 || view.dispatches != 1 || run.lastFrame.ID != 1 || run.ledger.rowBase != 0 || run.viewport != 3 {
				t.Fatalf("after the wheel change: reads=%d drags=%d last verified=%d row=%d viewport=%d",
					view.calls, view.dispatches, run.lastFrame.ID, run.ledger.rowBase, run.viewport)
			}
		})
	}
}
