//go:build page_unknown_trace

package collect

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/platform"
	"github.com/LoTwT/youkai/internal/platform/platformfake"
)

func TestPageUnknownTraceWeakSameDetailReferenceSucceeds(t *testing.T) {
	t.Setenv(pageUnknownTraceEnv, "1")
	for _, reused := range []bool{false, true} {
		t.Run(fmt.Sprintf("reused=%t", reused), func(t *testing.T) {
			l := StorageLayout()
			m := NewScrollEvidenceMatcher(NewPixelMatcher(l, StorageCalibration()))
			makeFrame := func(id uint64, slot int, weak bool) platform.Frame {
				f := weakTraceFrame(m, l, id, 300, slot, weak)
				if slot == 10 {
					muted := image.NewUniform(color.RGBA{145, 145, 145, 255})
					for _, line := range l.DetailLines {
						draw.Draw(f.Image, line.Rect, muted, image.Point{}, draw.Src)
					}
				}
				return f
			}
			fresh := makeFrame(6, 9, false)
			frames := []platform.Frame{
				makeFrame(1, 0, false), makeFrame(2, 0, false),
				makeFrame(3, 9, false), makeFrame(4, 9, true),
				makeFrame(5, 9, false), fresh,
			}
			if reused {
				idle := fresh
				idle.Reused = true
				frames = append(frames, idle)
			}
			// The zero-valued entry returns the budget expiry for the
			// already settled but visually identical target detail.
			frames = append(frames, platform.Frame{}, makeFrame(7, 9, false),
				makeFrame(8, 10, false), makeFrame(9, 10, false),
				makeFrame(10, 9, false), makeFrame(11, 9, false))
			if m.CompareDetail(frames[0], fresh) != DetailSame || m.CompareDetail(fresh, frames[len(frames)-3]) != DetailDifferent {
				t.Fatal("bad same-detail fixture")
			}
			reads := 0
			var ops []platform.InputOp
			view := &platformfake.Session{
				ObserveFn: func() (platform.Observation, error) { return platform.Observation{Ready: true, Geometry: 1}, nil },
				FrameFn: func(context.Context, platform.FrameRequest) (platform.Frame, error) {
					if reads >= len(frames) {
						return platform.Frame{}, context.DeadlineExceeded
					}
					f := frames[reads]
					reads++
					if f.ID == 0 {
						return f, context.DeadlineExceeded
					}
					return f, nil
				},
				DispatchFn: func(op platform.InputOp) platform.InputResult {
					ops = append(ops, op)
					now := time.Now()
					return platform.InputResult{Status: platform.InputPosted, EventsPlanned: 3, EventsPosted: 3, StartedAt: now, FinishedAt: now}
				},
			}
			dir := t.TempDir()
			inv := &Inventory{Layout: l, Matcher: m, StableFrames: 2, DetailStableSpan: -1, ActionBudget: 2 * time.Second, DiagnosticDir: dir}
			var obs []Observation
			inv.Project = func(o Observation) (any, error) { obs = append(obs, o); return o, nil }
			run := &inventoryRun{inv: inv, geo: 1, position: 3, view: view, filterActive: true, rangeProof: positionRange{Verified: true}}
			run.ledger = run.newStorageLedger(m.Match(frames[0]), 1)
			err := runScriptedSession(t, run, func(ctx context.Context, r *inventoryRun) error { return r.visitSlot(ctx, 9) })
			manifest := pageUnknownTestManifests(t, dir)
			if len(manifest) != 0 {
				t.Fatalf("reference success wrote %d trace manifests: %+v", len(manifest), manifest)
			}
			if err != nil || len(ops) != 3 || len(obs) != 1 || obs[0].Association != AssociationReference || obs[0].FrameID != 11 {
				t.Fatalf("fresh reference did not admit frame 11: reused=%t err=%v ops=%d observations=%d", reused, err, len(ops), len(obs))
			}
		})
	}
}
