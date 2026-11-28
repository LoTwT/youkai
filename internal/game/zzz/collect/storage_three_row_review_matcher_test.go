package collect

import (
	"context"
	"fmt"
	"testing"

	"github.com/LoTwT/youkai/internal/platform"
	"github.com/LoTwT/youkai/internal/platform/platformfake"
)

func TestReviewThreeRowRealMatcherSettlesAndCommitsActualRows(t *testing.T) {
	l := StorageLayout()
	m := NewScrollEvidenceMatcher(NewPixelMatcher(l, StorageCalibration()))
	for _, rows := range []int{1, 2, 3} {
		for _, anchor := range []bool{true, false} {
			t.Run(fmt.Sprintf("%d/anchor=%t", rows, anchor), func(t *testing.T) {
				beforeSlot, afterSlot := -1, -1
				if anchor {
					beforeSlot, afterSlot = 35, 35-rows*l.Columns
				}
				thumb := 300 + 6*rows
				before := storageSelectedPatternFrame(l, 1, 0, beforeSlot, 300, 323)
				after := storageSelectedPatternFrame(l, 2, rows, afterSlot, thumb, thumb+23)
				c1 := storageSelectedPatternFrame(l, 3, rows, afterSlot, thumb, thumb+23)
				c2 := storageSelectedPatternFrame(l, 4, rows, afterSlot, thumb, thumb+23)
				out, viewport := storageWaitAndCommit(t, l, m, before, after, c1, c2)
				if !out.settled || out.match.Rows != rows || len(out.match.Matches) != (l.Rows-rows)*l.Columns || viewport != 1 {
					t.Fatalf("did not settle/commit exactly %d rows: settled=%t rows=%d overlap=%d viewport=%d evidence=%s", rows, out.settled, out.match.Rows, len(out.match.Matches), viewport, out.match.Evidence)
				}
			})
		}
	}
}

func TestReviewThreeRowRealMatcherProgressiveMovement(t *testing.T) {
	l := StorageLayout()
	m := NewScrollEvidenceMatcher(NewPixelMatcher(l, StorageCalibration()))
	before := storageSelectedPatternFrame(l, 1, 0, 35, 300, 323)
	f1 := storageSelectedPatternFrame(l, 2, 1, 26, 306, 329)
	f2 := storageSelectedPatternFrame(l, 3, 2, 17, 312, 335)
	f3 := storageSelectedPatternFrame(l, 4, 3, 8, 318, 341)
	s1 := storageSelectedPatternFrame(l, 5, 3, 8, 318, 341)
	s2 := storageSelectedPatternFrame(l, 6, 3, 8, 318, 341)
	out, viewport := storageWaitAndCommit(t, l, m, before, f1, f2, f3, s1, s2)
	if !out.settled || viewport != 1 || out.match.Rows != 3 || len(out.match.Matches) != 9 {
		t.Fatalf("progressive movement did not commit final 3-row displacement: settled=%t viewport=%d row=%d overlap=%d evidence=%s",
			out.settled, viewport, out.match.Rows, len(out.match.Matches), out.match.Evidence)
	}
}

func TestReviewThreeRowRealMatcherBottomMovedThenReturned(t *testing.T) {
	l := StorageLayout()
	m := NewScrollEvidenceMatcher(NewPixelMatcher(l, StorageCalibration()))
	before := storageSelectedPatternFrame(l, 1, 0, 27, 842, 865)
	moved := storageSelectedPatternFrame(l, 2, 1, 18, 848, 871)
	returned := storageSelectedPatternFrame(l, 3, 0, 27, 842, 865)
	if mm := m.Match(before); mm.List == nil || !mm.List.Bottom {
		t.Fatalf("bad starting end frame: %s", mm.Evidence)
	}
	if verdict := m.MatchScroll(before, moved); !verdict.Displaced || verdict.Rows != 1 {
		t.Fatalf("should verify real movement in pixel fixture: %s", verdict.Evidence)
	}
	run := storageSettleRun(l, m, before, moved, returned)
	run.lastFrame, run.listMatch = before, m.Match(before)
	replay := run.view
	var inputs []platform.InputOp
	run.view = &platformfake.Session{
		ObserveFn: func() (platform.Observation, error) {
			return platform.Observation{Ready: true, Geometry: 1}, nil
		},
		FrameFn: replay.Frame,
		DispatchFn: func(op platform.InputOp) platform.InputResult {
			inputs = append(inputs, op)
			return platform.InputResult{Status: platform.InputPosted}
		},
	}
	err := runScriptedSession(t, run, func(ctx context.Context, run *inventoryRun) error { return run.confirmBottomStep(ctx) })
	if len(inputs) != 1 || inputs[0].Kind != platform.InputScroll || inputs[0].WheelSteps != -1 {
		t.Fatalf("expected sole -1 confirmation, got %+v", inputs)
	}
	if err == nil {
		t.Fatal("production matcher confirmed bottom despite witnessed motion during the same action")
	}
}
