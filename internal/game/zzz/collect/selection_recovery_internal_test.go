package collect

import (
	"context"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/platform"
)

type selectionSequenceView struct {
	frames []platform.Frame
	calls  int
}

func (v *selectionSequenceView) Observe() (platform.Observation, error) {
	return platform.Observation{Ready: true, Geometry: 1}, nil
}

func (v *selectionSequenceView) Dispatch(op platform.InputOp) platform.InputResult {
	return platform.InputResult{Status: platform.InputPosted}
}

func (v *selectionSequenceView) Frame(ctx context.Context, req platform.FrameRequest) (platform.Frame, error) {
	if err := ctx.Err(); err != nil {
		return platform.Frame{}, err
	}
	if v.calls >= len(v.frames) {
		return platform.Frame{}, context.DeadlineExceeded
	}
	f := v.frames[v.calls]
	v.calls++
	return f, nil
}

type selectionByIDMatcher struct {
	selected map[uint64]int
	slots    int
}

func (m *selectionByIDMatcher) Match(f platform.Frame) PageMatch {
	occupied := make([]bool, m.slots)
	for i := range occupied {
		occupied[i] = true
	}
	return PageMatch{
		Page:   PageWarehouse,
		List:   &ListEvidence{Occupied: occupied, Selected: m.selected[f.ID]},
		Wheel:  &WheelEvidence{Position: 1},
		Filter: &FilterEvidence{State: FilterActive},
	}
}

func (m *selectionByIDMatcher) MatchScroll(before, after platform.Frame) ScrollMatch {
	return ScrollMatch{}
}

func (m *selectionByIDMatcher) CompareDetail(a, b platform.Frame) DetailComparison {
	return DetailUndecided
}

func selectionRun(t *testing.T, view *selectionSequenceView, selected map[uint64]int) *inventoryRun {
	t.Helper()
	layout := MacCloudLayout()
	return &inventoryRun{
		inv: &Inventory{
			Layout:       layout,
			Matcher:      &selectionByIDMatcher{selected: selected, slots: len(layout.Slots)},
			ActionBudget: 200 * time.Millisecond,
			StableFrames: 3,
			RecheckEvery: time.Millisecond,
		},
		view:     view,
		geo:      1,
		position: 1,
	}
}

// A transient missing selection border must not stop the walk: one fresh
// frame without the border, followed by three frames with it, is enough.
func TestAwaitSelectionReturnedWaitsOutTransientLoss(t *testing.T) {
	view := &selectionSequenceView{frames: []platform.Frame{
		{ID: 1, Geometry: 1, ArrivedAt: time.Now()},
		{ID: 2, Geometry: 1, ArrivedAt: time.Now().Add(time.Millisecond)},
		{ID: 3, Geometry: 1, ArrivedAt: time.Now().Add(2 * time.Millisecond)},
		{ID: 4, Geometry: 1, ArrivedAt: time.Now().Add(3 * time.Millisecond)},
	}}
	run := selectionRun(t, view, map[uint64]int{1: -1, 2: 1, 3: 1, 4: 1})
	out := run.awaitSelectionReturned(context.Background(), 2, 1)
	if out.class != awaitStable {
		t.Fatalf("outcome %v, want stable after the selection returned", out.class)
	}
	if out.frame.ID != 4 || out.match.List.Selected != 1 {
		t.Fatalf("stable outcome = frame %d selected %d, want frame 4 selected 1", out.frame.ID, out.match.List.Selected)
	}
}

// A selection that stays missing or different reaches the bounded deadline
// and stays a position loss.
func TestAwaitSelectionReturnedPersistentLossStops(t *testing.T) {
	view := &selectionSequenceView{frames: []platform.Frame{
		{ID: 1, Geometry: 1, ArrivedAt: time.Now()},
		{ID: 2, Geometry: 1, ArrivedAt: time.Now().Add(time.Millisecond)},
		{ID: 3, Geometry: 1, ArrivedAt: time.Now().Add(2 * time.Millisecond)},
	}}
	run := selectionRun(t, view, map[uint64]int{1: -1, 2: -1, 3: -1})
	out := run.awaitSelectionReturned(context.Background(), 2, 1)
	if out.class != awaitDeadline {
		t.Fatalf("outcome %v, want deadline for a persistent loss", out.class)
	}
}
