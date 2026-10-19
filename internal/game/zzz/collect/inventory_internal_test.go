package collect

import (
	"context"
	"errors"
	"image"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/platform"
	"github.com/LoTwT/youkai/internal/scan"
)

// The awaitScroll settle rules are exercised here with a scripted view and
// matcher: the end-to-end fake frames exercise the real pixel matcher, but
// these cases need exact verdict sequences (for example a five-row total in
// a six-row grid) that the calibrated five-row layout cannot produce.

type scrollPair struct {
	before, after uint64
}

type scrollScriptMatcher struct {
	rows    int
	columns int
	verdict map[scrollPair]ScrollMatch
}

func (m *scrollScriptMatcher) Match(f platform.Frame) PageMatch {
	occupied := make([]bool, m.rows*m.columns)
	for i := range occupied {
		occupied[i] = true
	}
	return PageMatch{
		Page:   PageWarehouse,
		List:   &ListEvidence{Occupied: occupied, Selected: 0},
		Wheel:  &WheelEvidence{Position: 1},
		Filter: &FilterEvidence{State: FilterActive},
	}
}

func (m *scrollScriptMatcher) MatchScroll(before, after platform.Frame) ScrollMatch {
	if sm, ok := m.verdict[scrollPair{before: before.ID, after: after.ID}]; ok {
		return sm
	}
	return ScrollMatch{Evidence: "scripted: no verdict for this pair"}
}

func (m *scrollScriptMatcher) CompareDetail(a, b platform.Frame) DetailComparison {
	return DetailUndecided
}

type scrollScriptView struct {
	frames []platform.Frame
	calls  int
	// attempts counts every Frame entry, including the calls that block
	// after the scripted frames are exhausted.
	attempts int
	// blockAfter makes Frame block until the request's deadline after the
	// scripted frames are exhausted.
	blockAfter bool
}

func (v *scrollScriptView) Observe() (platform.Observation, error) {
	return platform.Observation{Ready: true, Geometry: 1}, nil
}

func (v *scrollScriptView) Dispatch(op platform.InputOp) platform.InputResult {
	return platform.InputResult{Status: platform.InputPosted}
}

func (v *scrollScriptView) Frame(ctx context.Context, req platform.FrameRequest) (platform.Frame, error) {
	v.attempts++
	if v.calls < len(v.frames) {
		f := v.frames[v.calls]
		v.calls++
		return f, nil
	}
	if v.blockAfter {
		<-ctx.Done()
		return platform.Frame{}, ctx.Err()
	}
	return platform.Frame{}, context.DeadlineExceeded
}

// sixRowScrollLayout is the calibrated layout widened to six abstract rows
// so a five-row total displacement still retains one row of overlap. Only
// the grid dimensions matter to awaitScroll and applyTransition; no pixels
// are involved.
func sixRowScrollLayout() Layout {
	l := MacCloudLayout()
	l.Rows = 6
	for c := 0; c < l.Columns; c++ {
		cx := 119 + 112*c
		l.Slots = append(l.Slots, image.Rect(cx-50, 927, cx+50, 1047))
	}
	return l
}

func scrollFrame(id uint64) platform.Frame {
	return platform.Frame{ID: id, Geometry: 1}
}

// scriptedDisplacement builds a unique correspondence set for a total
// displacement of rows in the layout's grid.
func scriptedDisplacement(l Layout, rows int) []PositionPair {
	pairs := make([]PositionPair, 0, (l.Rows-rows)*l.Columns)
	for r := 0; r < l.Rows-rows; r++ {
		for c := 0; c < l.Columns; c++ {
			pairs = append(pairs, PositionPair{
				FromRow: r + rows, FromColumn: c,
				ToRow: r, ToColumn: c,
			})
		}
	}
	return pairs
}

func displacedScan(l Layout, rows int) ScrollMatch {
	return ScrollMatch{
		Displaced: true,
		Rows:      rows,
		Matches:   scriptedDisplacement(l, rows),
		Evidence:  "scripted displacement",
	}
}

func newScrollRun(l Layout, view SessionView, m Matcher, budget time.Duration) *inventoryRun {
	return &inventoryRun{
		inv: &Inventory{
			Layout:       l,
			Matcher:      m,
			ScrollBudget: budget,
		},
		view: view,
		geo:  1,
	}
}

// TestAwaitScrollCommitsLatestSettledCandidate pins the two-stage rule:
// four rows is only a candidate, a later frame shows five rows total, and
// the commit waits until a frame relative to the five-row candidate is
// Stationary.
func TestAwaitScrollCommitsLatestSettledCandidate(t *testing.T) {
	l := sixRowScrollLayout()
	before := scrollFrame(1)
	view := &scrollScriptView{frames: []platform.Frame{
		scrollFrame(2), // four rows
		scrollFrame(3), // five rows total
		scrollFrame(4), // still five rows
	}}
	m := &scrollScriptMatcher{rows: l.Rows, columns: l.Columns, verdict: map[scrollPair]ScrollMatch{
		{1, 2}: displacedScan(l, 4),
		{1, 3}: displacedScan(l, 5),
		{3, 4}: {Stationary: true, Evidence: "scripted settled"},
	}}
	run := newScrollRun(l, view, m, time.Second)
	out, err := run.awaitScroll(context.Background(), before)
	if err != nil {
		t.Fatalf("awaitScroll: %v", err)
	}
	if !out.displaced || out.stationary || out.lost {
		t.Fatalf("outcome %+v, want a settled displacement", out)
	}
	if out.after.ID != 3 || out.match.Rows != 5 || len(out.match.Matches) != (l.Rows-5)*l.Columns {
		t.Fatalf("committed frame %d rows %d matches %d, want frame 3 with the five-row total",
			out.after.ID, out.match.Rows, len(out.match.Matches))
	}
	run.visited = make([]bool, len(l.Slots))
	run.listMatch = m.Match(before)
	if err := run.applyTransition(before, out.after, m.Match(before), out.afterMatch, out.match); err != nil {
		t.Fatalf("the settled five-row candidate must commit in the six-row grid: %v", err)
	}
	if run.viewport != 1 {
		t.Fatalf("viewport %d, want 1 after the commit", run.viewport)
	}
}

// TestAwaitScrollCommitsStableFourRows pins the ordinary case: one
// displacement followed by a stationary observation commits immediately.
func TestAwaitScrollCommitsStableFourRows(t *testing.T) {
	l := sixRowScrollLayout()
	before := scrollFrame(1)
	view := &scrollScriptView{frames: []platform.Frame{
		scrollFrame(2),
		scrollFrame(3),
	}}
	m := &scrollScriptMatcher{rows: l.Rows, columns: l.Columns, verdict: map[scrollPair]ScrollMatch{
		{1, 2}: displacedScan(l, 4),
		{2, 3}: {Stationary: true, Evidence: "scripted settled"},
	}}
	run := newScrollRun(l, view, m, time.Second)
	out, err := run.awaitScroll(context.Background(), before)
	if err != nil {
		t.Fatalf("awaitScroll: %v", err)
	}
	if !out.displaced || out.after.ID != 2 || out.match.Rows != 4 {
		t.Fatalf("outcome %+v, want frame 2 with four rows", out)
	}
}

// TestAwaitScrollWithoutSettlingIsLost pins the safety stop: a
// displacement that never settles relative to its candidate returns Lost
// at the budget and commits no viewport.
func TestAwaitScrollWithoutSettlingIsLost(t *testing.T) {
	l := sixRowScrollLayout()
	before := scrollFrame(1)
	view := &scrollScriptView{
		frames: []platform.Frame{
			scrollFrame(2), // four rows
			scrollFrame(3), // five rows
			scrollFrame(4), // four rows again: still moving
		},
		blockAfter: true,
	}
	m := &scrollScriptMatcher{rows: l.Rows, columns: l.Columns, verdict: map[scrollPair]ScrollMatch{
		{1, 2}: displacedScan(l, 4),
		{1, 3}: displacedScan(l, 5),
		{1, 4}: displacedScan(l, 4),
		{4, 4}: {Stationary: true},
	}}
	run := newScrollRun(l, view, m, 40*time.Millisecond)
	run.visited = make([]bool, len(l.Slots))
	out, err := run.awaitScroll(context.Background(), before)
	if err != nil {
		t.Fatalf("awaitScroll: %v", err)
	}
	if !out.lost || out.displaced || out.stationary {
		t.Fatalf("outcome %+v, want Lost and no commit", out)
	}
	if run.viewport != 0 {
		t.Fatalf("viewport advanced to %d without a settling confirmation", run.viewport)
	}
}

// TestAwaitScrollStationaryWaitsForBudget pins the no-displacement case:
// Stationary is a deadline verdict, not an early return.
func TestAwaitScrollStationaryWaitsForBudget(t *testing.T) {
	l := sixRowScrollLayout()
	before := scrollFrame(1)
	view := &scrollScriptView{
		frames:     []platform.Frame{scrollFrame(2)},
		blockAfter: true,
	}
	m := &scrollScriptMatcher{rows: l.Rows, columns: l.Columns, verdict: map[scrollPair]ScrollMatch{
		{1, 2}: {Stationary: true, Evidence: "scripted stationary"},
	}}
	budget := 40 * time.Millisecond
	run := newScrollRun(l, view, m, budget)
	started := time.Now()
	out, err := run.awaitScroll(context.Background(), before)
	if err != nil {
		t.Fatalf("awaitScroll: %v", err)
	}
	if !out.stationary || out.displaced || out.lost {
		t.Fatalf("outcome %+v, want Stationary", out)
	}
	if elapsed := time.Since(started); elapsed < budget*3/4 {
		t.Fatalf("Stationary returned after %s, before the %s budget", elapsed, budget)
	}
	if view.attempts < 2 {
		t.Fatalf("only %d frame request(s); a stationary verdict must be rechecked to the budget", view.attempts)
	}
}

// noopRecognizer completes every task unchanged; the admission-failure
// test never reaches recognition.
type noopRecognizer struct{}

func (noopRecognizer) Recognize(ctx context.Context, task scan.Task, submit scan.Submit) error {
	return submit(task.SessionID, task.Seq, scan.Recognition{Result: task.Observation})
}

// noopDeliverer accepts terminal records; the scripted stop leaves no
// admitted entries to deliver.
type noopDeliverer struct{}

func (noopDeliverer) Deliver(rec scan.Record) error { return nil }

// admitFailureController acquires a slot, stops the session (so admission
// must be refused) and calls the traversal's admission path directly.
type admitFailureController struct {
	errCh chan error
}

func (c *admitFailureController) Prepare(ctx context.Context, h *scan.ControllerHandle) error {
	return nil
}

func (c *admitFailureController) Collect(ctx context.Context, h *scan.ControllerHandle) error {
	slot := h.Acquire(ctx)
	if slot == nil {
		c.errCh <- errors.New("scripted controller: no slot")
		return nil
	}
	// A stop accepted after Acquire closes admission; the traversal must
	// classify the refused slot as an evidence failure, not as a generic
	// controller error.
	h.Fail(scan.NewFault("scriptedStop", errors.New("scripted stop")))
	inv := &Inventory{Layout: MacCloudLayout(), Matcher: Uncalibrated{}}
	run := &inventoryRun{inv: inv, h: h, geo: 1, visited: make([]bool, len(inv.Layout.Slots))}
	out := detailOutcome{
		class: awaitStable,
		frame: platform.Frame{ID: 1, Geometry: 1},
		match: PageMatch{
			Page:   PageWarehouse,
			List:   &ListEvidence{Occupied: make([]bool, len(inv.Layout.Slots)), Selected: 0},
			Detail: &DetailEvidence{Quality: QualityS, Substats: 3},
		},
	}
	err := run.admit(slot, out, platform.Frame{}, 0, 0, 0, AssociationTransition, 0, 1, "scripted slot")
	c.errCh <- err
	return err
}

// TestAdmitFailureCarriesEvidenceFailed pins the second evidenceFailed
// path: a slot the kernel refuses after the session stopped.
func TestAdmitFailureCarriesEvidenceFailed(t *testing.T) {
	ctrl := &admitFailureController{errCh: make(chan error, 1)}
	sess, err := scan.Start(scan.Config{
		ID:         scan.ID("admit-failure"),
		Capacity:   1,
		Workers:    1,
		Controller: ctrl,
		Recognizer: noopRecognizer{},
		Deliverer:  noopDeliverer{},
	})
	if err != nil {
		t.Fatalf("scan.Start: %v", err)
	}
	select {
	case err := <-ctrl.errCh:
		var fault *scan.FaultError
		if !errors.As(err, &fault) || fault.Code != CodeEvidenceFailed {
			t.Fatalf("admission error %v, want evidenceFailed", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the scripted controller never returned")
	}
	select {
	case <-sess.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the session never sealed")
	}
}
