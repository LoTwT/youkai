package collect

import (
	"context"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/platform"
)

// detailSpanMatcher serves one stable transient detail for the first
// transientUntil frames and a settled target detail afterwards. The two
// panes compare as different, so only the stability run decides which one
// the traversal would admit.
type detailSpanMatcher struct {
	rows, columns  int
	transientUntil uint64
}

func (m *detailSpanMatcher) Match(f platform.Frame) PageMatch {
	occupied := make([]bool, m.rows*m.columns)
	for i := range occupied {
		occupied[i] = true
	}
	return PageMatch{
		Page:   PageWarehouse,
		List:   &ListEvidence{Occupied: occupied, Selected: 0},
		Detail: &DetailEvidence{Quality: QualityS, Substats: 4},
		Wheel:  &WheelEvidence{Position: 1},
		Filter: &FilterEvidence{State: FilterActive},
	}
}

func (m *detailSpanMatcher) MatchScroll(_, _ platform.Frame) ScrollMatch {
	return ScrollMatch{}
}

func (m *detailSpanMatcher) CompareDetail(a, b platform.Frame) DetailComparison {
	if m.transient(a.ID) == m.transient(b.ID) {
		return DetailSame
	}
	return DetailDifferent
}

func (m *detailSpanMatcher) transient(id uint64) bool {
	return id <= m.transientUntil
}

// pacedFrameView returns a fresh frame per request at a stream-like pace,
// so a wall-clock stability span has time to elapse between observations.
type pacedFrameView struct {
	next     uint64
	interval time.Duration
}

func (v *pacedFrameView) Observe() (platform.Observation, error) {
	return platform.Observation{Ready: true, Geometry: 1}, nil
}

func (v *pacedFrameView) Dispatch(platform.InputOp) platform.InputResult {
	return platform.InputResult{Status: platform.InputPosted}
}

func (v *pacedFrameView) Frame(ctx context.Context, _ platform.FrameRequest) (platform.Frame, error) {
	if v.interval > 0 {
		timer := time.NewTimer(v.interval)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return platform.Frame{}, ctx.Err()
		case <-timer.C:
		}
	}
	if err := ctx.Err(); err != nil {
		return platform.Frame{}, err
	}
	v.next++
	return platform.Frame{ID: v.next, Geometry: 1, ArrivedAt: time.Now()}, nil
}

// TestAwaitDetailRequiresWallClockStability pins the span's independent
// role beside StableFrames. The cloud stream runs at roughly 56fps, so three
// identical observations can arrive in about 54ms while a click's detail
// settles over 374–415ms (T5 §4). A crossfade can therefore hold a wrong
// pane for StableFrames samples without being stable. The test scripts that
// pane for the first three frames and the target pane afterwards; the
// traversal must wait out DetailStableSpan and admit the target, never settle
// on the three-frame transient.
func TestAwaitDetailRequiresWallClockStability(t *testing.T) {
	l := MacCloudLayout()
	inv := &Inventory{
		Layout:           l,
		Matcher:          &detailSpanMatcher{rows: l.Rows, columns: l.Columns, transientUntil: 3},
		ActionBudget:     2 * time.Second,
		StableFrames:     3,
		DetailStableSpan: DefaultDetailStableSpan,
		RecheckEvery:     time.Millisecond,
	}
	view := &pacedFrameView{interval: time.Millisecond}
	run := &inventoryRun{inv: inv, view: view, geo: 1, position: 1}

	out, err := run.awaitDetail(context.Background(), platform.Frame{}, 0, true)
	if err != nil {
		t.Fatalf("awaitDetail: %v", err)
	}
	if out.class != awaitStable || !out.stable {
		t.Fatalf("detail wait %+v with verdict %s, want a stable observation", out, out.match.Evidence)
	}
	if out.frame.ID <= 3 {
		t.Fatalf("settled on transient frame %d; the wall-clock span must outlast a %d-frame run",
			out.frame.ID, inv.StableFrames)
	}
}
