//go:build page_unknown_trace

package collect

import (
	"context"
	"image"
	"image/color"
	"image/draw"
	"os"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/platform"
	"github.com/LoTwT/youkai/internal/platform/platformfake"
)

func weakTraceRun(t *testing.T, frames []platform.Frame, budget time.Duration) (*inventoryRun, platform.Frame, string) {
	t.Helper()
	l := StorageLayout()
	m := NewScrollEvidenceMatcher(NewPixelMatcher(l, StorageCalibration()))
	baseline := storageReviewPixelFrame(l, 1, 10, 300, 323)
	dir := t.TempDir()
	run := &inventoryRun{
		inv: &Inventory{Layout: l, Matcher: m, StableFrames: 2, DetailStableSpan: -1,
			ActionBudget: budget, DiagnosticDir: dir},
		geo: 1, position: 3, filterActive: true, rangeProof: positionRange{Verified: true},
		view: &storageReviewFrames{frames: frames},
	}
	run.ledger = run.newStorageLedger(m.Match(baseline), baseline.ID)
	tracePageUnknownInput(run, 9, baseline.ID, platform.InputResult{
		Status: platform.InputPosted, EventsPlanned: 3, EventsPosted: 3, StartedAt: time.Now(), FinishedAt: time.Now(),
	})
	return run, baseline, dir
}

func weakTraceFrame(m *ScrollEvidenceMatcher, l Layout, id uint64, thumb int, selected int, weak bool) platform.Frame {
	f := storageReviewPixelFrame(l, id, 10, thumb, thumb+23)
	f.ArrivedAt = time.Now().Add(time.Duration(id) * time.Millisecond)
	if selected >= 0 {
		if weak {
			weakSelectionReviewBorder(l, f, selected, 70)
		} else {
			draw.Draw(f.Image, l.BorderRect(selected), image.NewUniform(color.RGBA{224, 208, 67, 255}), image.Point{}, draw.Src)
		}
	}
	weakSelectionReviewDetail(m.PixelMatcher, f, 3)
	return f
}

func TestPageUnknownTraceWeakTerminalReceipts(t *testing.T) {
	t.Setenv(pageUnknownTraceEnv, "1")
	for _, scenario := range []string{"wrong-target", "double", "second-weak", "moved-after-weak", "thumb-missing"} {
		t.Run(scenario, func(t *testing.T) {
			l := StorageLayout()
			m := NewScrollEvidenceMatcher(NewPixelMatcher(l, StorageCalibration()))
			frames := []platform.Frame{
				weakTraceFrame(m, l, 2, 300, 9, false),
				weakTraceFrame(m, l, 3, 300, 9, true),
			}
			switch scenario {
			case "wrong-target":
				frames = append(frames, weakTraceFrame(m, l, 4, 300, 10, true))
			case "double":
				f := weakTraceFrame(m, l, 4, 300, 9, true)
				weakSelectionReviewBorder(l, f, 10, 70)
				frames = append(frames, f)
			case "second-weak":
				frames = append(frames, weakTraceFrame(m, l, 4, 300, 9, true))
			case "moved-after-weak":
				frames = append(frames, weakTraceFrame(m, l, 4, 330, 9, false))
			case "thumb-missing":
				draw.Draw(frames[1].Image, l.ScrollTrack, image.Black, image.Point{}, draw.Src)
			}
			run, baseline, dir := weakTraceRun(t, frames, time.Second)
			_, err := run.awaitNormalTargetDetail(context.Background(), baseline, nil, 9)
			if err == nil {
				t.Fatalf("%s did not terminate", scenario)
			}
			manifests := pageUnknownTestManifests(t, dir)
			if len(manifests) != 1 {
				t.Fatalf("%s manifests=%d, want one terminal receipt", scenario, len(manifests))
			}
			got := manifests[0]
			if got.Input == nil || got.Input.FrameID != 1 || got.Error == "" {
				t.Fatalf("%s lost input receipt: %+v", scenario, got)
			}
			if got.FailureFrame == 0 || got.TerminalFrame != got.FailureFrame ||
				got.TerminalAt.IsZero() || got.LastFrame == 0 || got.LastAt.IsZero() {
				t.Fatalf("%s lost actual terminal frame/time: %+v", scenario, got)
			}
			if len(got.Frames) > 3 || got.CropCount == 0 {
				t.Fatalf("%s ring/crops not bounded: frames=%d crops=%d", scenario, len(got.Frames), got.CropCount)
			}
			for _, fr := range got.Frames {
				if len(fr.Crops) > 64+len(StorageLayout().Slots)+1 {
					t.Fatalf("%s frame %d crops=%d exceeds bound", scenario, fr.ID, len(fr.Crops))
				}
			}
			if scenario == "second-weak" {
				if got.Kind != "weak-selection-terminal" || got.WeakFrame != 3 || got.WeakHits != 70 ||
					got.FailureFrame != 4 || got.TerminalFrame != 4 {
					t.Fatalf("second weak receipt = %+v", got)
				}
			}
			if scenario == "moved-after-weak" || scenario == "thumb-missing" {
				if got.Kind != "weak-selection-terminal" || got.WeakFrame != 3 {
					t.Fatalf("weak viewport receipt = %+v", got)
				}
			}
			if scenario == "wrong-target" || scenario == "double" {
				if got.Kind != "page-unknown" || got.Cause != "storageBadgeDisposition-page-unknown" ||
					got.WeakFrame != 3 || got.WeakHits != 70 {
					t.Fatalf("original page-unknown capture changed: %+v", got)
				}
			}
		})
	}
}

func TestPageUnknownTraceWeakTimeoutWaitsForVisit(t *testing.T) {
	t.Setenv(pageUnknownTraceEnv, "1")
	l := StorageLayout()
	m := NewScrollEvidenceMatcher(NewPixelMatcher(l, StorageCalibration()))
	run, baseline, dir := weakTraceRun(t, []platform.Frame{
		weakTraceFrame(m, l, 2, 300, 9, false),
		weakTraceFrame(m, l, 3, 300, 9, true),
	}, 400*time.Millisecond)
	out, err := run.awaitNormalTargetDetail(context.Background(), baseline, nil, 9)
	if err != nil || out.class != awaitDeadline {
		t.Fatalf("weak timeout class=%v err=%v", out.class, err)
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Fatalf("direct await wrote %d entries err=%v; visit termination was not observed", len(entries), err)
	}
}

func TestPageUnknownTraceWeakTimeoutAfterVisit(t *testing.T) {
	t.Setenv(pageUnknownTraceEnv, "1")
	l := StorageLayout()
	m := NewScrollEvidenceMatcher(NewPixelMatcher(l, StorageCalibration()))
	frames := []platform.Frame{
		weakTraceFrame(m, l, 1, 300, 0, false),
		weakTraceFrame(m, l, 2, 300, 0, false),
		weakTraceFrame(m, l, 3, 300, 9, false),
		weakTraceFrame(m, l, 4, 300, 9, true),
	}
	baseline := frames[0]
	var ops []platform.InputOp
	sess := &platformfake.Session{
		ObserveFn: func() (platform.Observation, error) {
			return platform.Observation{Ready: true, Geometry: 1}, nil
		},
		FrameFn: func(ctx context.Context, _ platform.FrameRequest) (platform.Frame, error) {
			if len(frames) == 0 {
				return platform.Frame{}, context.DeadlineExceeded
			}
			f := frames[0]
			frames = frames[1:]
			return f, nil
		},
		DispatchFn: func(op platform.InputOp) platform.InputResult {
			ops = append(ops, op)
			return platform.InputResult{Status: platform.InputPosted, EventsPlanned: 3, EventsPosted: 3}
		},
	}
	dir := t.TempDir()
	run := &inventoryRun{
		inv: &Inventory{Layout: l, Matcher: m, StableFrames: 2, DetailStableSpan: -1,
			ActionBudget: 400 * time.Millisecond, DiagnosticDir: dir},
		geo: 1, position: 3, filterActive: true, rangeProof: positionRange{Verified: true}, view: sess,
	}
	run.ledger = run.newStorageLedger(m.Match(baseline), baseline.ID)
	err := runScriptedSession(t, run, func(ctx context.Context, r *inventoryRun) error { return r.visitSlot(ctx, 9) })
	if err == nil || len(ops) != 1 {
		t.Fatalf("visit timeout err=%v ops=%d", err, len(ops))
	}
	manifests := pageUnknownTestManifests(t, dir)
	if len(manifests) != 1 {
		t.Fatalf("timeout manifests=%d, want one after visit termination", len(manifests))
	}
	got := manifests[0]
	if !got.TimedOut || got.Kind != "detail-timeout" || got.FailureFrame != 0 || got.TerminalFrame != 0 ||
		got.Cause != "weak-selection-deadline" || got.WeakFrame != 4 || got.LastFrame != 4 || got.LastAt.IsZero() {
		t.Fatalf("weak timeout receipt = %+v", got)
	}
	if got.Error == "" || got.Cause == "storageBadgeDisposition-page-unknown" || got.Kind == "page-unknown" {
		t.Fatalf("timeout falsely claimed a PageUnknown rejection: %+v", got)
	}
	if len(got.Frames) > 3 || got.CropCount == 0 {
		t.Fatalf("timeout ring/crops not bounded: frames=%d crops=%d", len(got.Frames), got.CropCount)
	}
}

func TestPageUnknownTraceWeakFreshRecoveryDoesNotWrite(t *testing.T) {
	t.Setenv(pageUnknownTraceEnv, "1")
	l := StorageLayout()
	m := NewScrollEvidenceMatcher(NewPixelMatcher(l, StorageCalibration()))
	run, baseline, dir := weakTraceRun(t, []platform.Frame{
		weakTraceFrame(m, l, 2, 300, 9, false),
		weakTraceFrame(m, l, 3, 300, 9, true),
		weakTraceFrame(m, l, 4, 300, 9, false),
		weakTraceFrame(m, l, 5, 300, 9, false),
	}, time.Second)
	out, err := run.awaitNormalTargetDetail(context.Background(), baseline, nil, 9)
	if err != nil || out.class != awaitStable || out.frame.ID != 5 {
		t.Fatalf("fresh recovery class=%v frame=%d err=%v", out.class, out.frame.ID, err)
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Fatalf("recovery wrote %d diagnostic entries err=%v", len(entries), err)
	}
}

func TestPageUnknownTraceWeakReusedStrongDoesNotRecover(t *testing.T) {
	t.Setenv(pageUnknownTraceEnv, "1")
	l := StorageLayout()
	m := NewScrollEvidenceMatcher(NewPixelMatcher(l, StorageCalibration()))
	fresh := weakTraceFrame(m, l, 4, 300, 9, false)
	reused := fresh
	reused.Reused = true
	run, baseline, dir := weakTraceRun(t, []platform.Frame{
		weakTraceFrame(m, l, 2, 300, 9, false),
		weakTraceFrame(m, l, 3, 300, 9, true),
		fresh, reused,
	}, 80*time.Millisecond)
	out, err := run.awaitNormalTargetDetail(context.Background(), baseline, nil, 9)
	if err != nil && out.class == awaitStable {
		t.Fatalf("reused strong recovered: %+v", out)
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Fatalf("direct weak wait wrote %d entries err=%v; visit termination was not observed", len(entries), err)
	}
}
