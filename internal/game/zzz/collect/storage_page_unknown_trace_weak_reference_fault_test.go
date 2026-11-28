//go:build page_unknown_trace

package collect

import (
	"context"
	"image"
	"image/draw"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/platform"
	"github.com/LoTwT/youkai/internal/platform/platformfake"
)

func TestPageUnknownTraceWeakReferenceFaultIsNotTimeout(t *testing.T) {
	t.Setenv(pageUnknownTraceEnv, "1")
	l := StorageLayout()
	m := NewScrollEvidenceMatcher(NewPixelMatcher(l, StorageCalibration()))
	makeFrame := func(id uint64, selected int, weak bool) platform.Frame {
		f := weakTraceFrame(m, l, id, 300, selected, weak)
		for i := range l.Slots {
			if i != 9 {
				drawBarBlack(l, f, i)
			}
		}
		return f
	}
	frames := []platform.Frame{
		makeFrame(1, 0, false),
		makeFrame(2, 0, false),
		makeFrame(3, 9, false),
		makeFrame(4, 9, true),
		makeFrame(5, 9, false),
		makeFrame(6, 9, false),
		{},
	}
	baseline := frames[0]
	var ops []platform.InputOp
	sess := &platformfake.Session{
		ObserveFn: func() (platform.Observation, error) { return platform.Observation{Ready: true, Geometry: 1}, nil },
		FrameFn: func(ctx context.Context, _ platform.FrameRequest) (platform.Frame, error) {
			if len(frames) == 0 {
				return platform.Frame{}, context.DeadlineExceeded
			}
			f := frames[0]
			frames = frames[1:]
			if f.ID == 0 {
				return f, context.DeadlineExceeded
			}
			return f, nil
		},
		DispatchFn: func(op platform.InputOp) platform.InputResult {
			ops = append(ops, op)
			return platform.InputResult{Status: platform.InputPosted, EventsPlanned: 3, EventsPosted: 3}
		},
	}
	dir := t.TempDir()
	run := &inventoryRun{inv: &Inventory{Layout: l, Matcher: m, StableFrames: 2, DetailStableSpan: -1,
		ActionBudget: 2 * time.Second, DiagnosticDir: dir}, geo: 1, position: 3,
		filterActive: true, rangeProof: positionRange{Verified: true}, view: sess}
	run.ledger = run.newStorageLedger(m.Match(baseline), baseline.ID)
	err := runScriptedSession(t, run, func(ctx context.Context, r *inventoryRun) error { return r.visitSlot(ctx, 9) })
	if err == nil || len(ops) != 1 {
		t.Fatalf("reference failure err=%v ops=%d", err, len(ops))
	}
	manifests := pageUnknownTestManifests(t, dir)
	if len(manifests) != 1 {
		t.Fatalf("reference failure manifests=%d, want one", len(manifests))
	}
	got := manifests[0]
	if got.Kind == "detail-timeout" || got.Cause != "weak-selection-reference" || got.TimedOut ||
		got.WeakFrame != 4 || got.Input == nil || got.Input.FrameID != 2 {
		t.Fatalf("reference failure was mislabeled: %+v", got)
	}
}

func drawBarBlack(l Layout, f platform.Frame, slot int) {
	draw.Draw(f.Image, l.BarRect(slot), image.Black, image.Point{}, draw.Src)
}
