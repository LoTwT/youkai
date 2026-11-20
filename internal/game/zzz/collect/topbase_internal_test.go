package collect

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/platform"
)

type topBasePageMatcher struct {
	*scrollScriptMatcher
	panelFrame uint64
}

func (m topBasePageMatcher) Match(f platform.Frame) PageMatch {
	if f.ID == m.panelFrame {
		return PageMatch{Page: PageFilterPanel, Panel: &PanelEvidence{}}
	}
	match := m.scrollScriptMatcher.Match(f)
	if match.List != nil {
		match.List.Top = f.ID > 1
	}
	return match
}

func TestTopConfirmationRequiresCurrentWarehouse(t *testing.T) {
	layout := sixRowScrollLayout()
	for _, tc := range []struct {
		name       string
		afterFling []uint64
		panel      uint64
		unreadable map[uint64]bool
		reads      int
		drags      int
		failure    string
		baseMoving bool
	}{
		{
			name: "identified filter panel", afterFling: []uint64{5, 6, 7}, panel: 5, reads: 4, drags: 1,
			failure: "filterPanel",
		},
		{
			name: "unreadable through the deadline", afterFling: []uint64{5, 6}, unreadable: map[uint64]bool{6: true},
			reads: 5, drags: 1, failure: "no current verifiable warehouse list", baseMoving: true,
		},
		{name: "no observation after the fling", reads: 3, drags: 1, failure: "no frame received"},
		{
			name: "warehouse returns before the deadline", afterFling: []uint64{5, 6, 7, 8},
			unreadable: map[uint64]bool{5: true}, reads: 7, drags: 2,
		},
		{
			name: "panel appears during a confirmation", afterFling: []uint64{5, 6, 7}, panel: 6,
			reads: 5, drags: 2, failure: "filterPanel",
		},
		{
			name: "retry must also recheck its page", afterFling: []uint64{5, 6, 7, 8, 9, 10}, panel: 9,
			reads: 8, drags: 2, failure: "filterPanel",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := topBasePageMatcher{
				scrollScriptMatcher: &scrollScriptMatcher{
					rows: layout.Rows, columns: layout.Columns, nonListFrames: tc.unreadable,
					verdict: map[scrollPair]ScrollMatch{
						{1, 2}: displacedScan(layout, 2), {2, 3}: {Stationary: true}, {2, 4}: {Stationary: true},
						{2, 5}: {Stationary: true}, {5, 6}: displacedScan(layout, 2),
						{6, 7}: {Stationary: true}, {6, 8}: {Stationary: true}, {7, 8}: {Stationary: true},
					},
				},
				panelFrame: tc.panel,
			}
			// For the unreadable-deadline case the first observation after
			// the fling is readable but has not yet settled. Its following
			// unreadable frame must still revoke the old base at the deadline.
			if tc.baseMoving {
				delete(m.verdict, scrollPair{2, 5})
			}
			frames := []platform.Frame{scrollFrame(2), scrollFrame(3), scrollFrame(4)}
			for _, id := range tc.afterFling {
				frames = append(frames, scrollFrame(id))
			}
			view := &scrollScriptView{frames: frames, blockAfter: true}
			run := newScriptedScrollRun(layout, view, m, "", 0, 30*time.Millisecond)
			run.inv.ActionBudget = 15 * time.Millisecond
			run.lastFrame = scrollFrame(1)
			run.listMatch = m.Match(run.lastFrame)
			err := runScriptedSession(t, run, func(ctx context.Context, run *inventoryRun) error {
				_, err := run.scrollToTop(ctx)
				return err
			})
			if view.dispatches != tc.drags || view.calls != tc.reads {
				t.Fatalf("got %d drags and %d reads, want %d and %d: %v", view.dispatches, view.calls, tc.drags, tc.reads, err)
			}
			if tc.failure == "" {
				if err != nil || run.lastFrame.ID != 8 {
					t.Fatalf("current warehouse confirmation: frame=%d err=%v", run.lastFrame.ID, err)
				}
				return
			}
			if _, scoped := asPositionIncomplete(err); scoped || faultDiagnosticCode(err) != CodeNavigationMismatch ||
				!strings.Contains(err.Error(), tc.failure) {
				t.Fatalf("expected a session-level %q fault: %v", tc.failure, err)
			}
		})
	}
}

// TestAwaitTopBaseWaitsForStationaryPair pins the pacing fix: a fresh
// frame that still disagrees with the top-claim frame is not used as the
// confirmation base; the next stationary pair is.
func TestAwaitTopBaseWaitsForStationaryPair(t *testing.T) {
	for _, tc := range []struct {
		name     string
		lastPair ScrollMatch
	}{
		{"stationary pair", ScrollMatch{Stationary: true, Evidence: "settled"}},
		{"readable warehouse at deadline", ScrollMatch{Lost: true, Evidence: "still settling"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inv := &Inventory{
				Layout: MacCloudLayout(),
				Matcher: &scrollScriptMatcher{
					rows: 5, columns: 4,
					verdict: map[scrollPair]ScrollMatch{
						{1, 2}: {Lost: true, Evidence: "still settling"},
						{2, 3}: tc.lastPair,
					},
				},
				ActionBudget: time.Second,
			}
			view := &scrollScriptView{frames: []platform.Frame{scrollFrame(2), scrollFrame(3)}}
			run := &inventoryRun{inv: inv, view: view, geo: 1, position: 1, lastFrame: scrollFrame(1)}
			if err := run.awaitTopBase(context.Background()); err != nil {
				t.Fatalf("awaitTopBase: %v", err)
			}
			if view.calls != 2 || run.lastFrame.ID != 3 {
				t.Fatalf("top base after %d reads was frame %d, want two reads and frame 3", view.calls, run.lastFrame.ID)
			}
		})
	}
}
