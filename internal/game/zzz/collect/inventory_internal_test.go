package collect

import (
	"context"
	"errors"
	"image"
	"path/filepath"
	"strings"
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
	// alwaysStationary makes every pair Stationary, for deadline tests
	// whose frame source keeps producing fresh content.
	alwaysStationary bool
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
	if m.alwaysStationary {
		return ScrollMatch{Stationary: true, Evidence: "scripted stationary"}
	}
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

// endlessFrameView keeps producing fresh frames. It models the real live
// stream: Frame can always return current content, so a wait that only
// relies on Frame returning DeadlineExceeded would spin forever.
type endlessFrameView struct {
	next uint64
}

func (v *endlessFrameView) Observe() (platform.Observation, error) {
	return platform.Observation{Ready: true, Geometry: 1}, nil
}

func (v *endlessFrameView) Dispatch(op platform.InputOp) platform.InputResult {
	return platform.InputResult{Status: platform.InputPosted}
}

func (v *endlessFrameView) Frame(ctx context.Context, req platform.FrameRequest) (platform.Frame, error) {
	if err := ctx.Err(); err != nil {
		return platform.Frame{}, err
	}
	v.next++
	return platform.Frame{ID: v.next, Geometry: 1, ArrivedAt: time.Now()}, nil
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

// A cloud-client activation can restart the video stream: one warehouse
// sample followed by a stalled stream must not end the whole scan. The
// retry gets a fresh action budget and settles once frames resume.
func TestEstablishListRetriesStalledStart(t *testing.T) {
	inv := &Inventory{
		Layout:       MacCloudLayout(),
		Matcher:      &scrollScriptMatcher{rows: 4, columns: 5},
		ActionBudget: 60 * time.Millisecond,
		StableFrames: 3,
	}
	view := &stalledListStartView{}
	run := &inventoryRun{inv: inv, view: view, geo: 1, position: 1}

	if err := run.establishList(context.Background()); err != nil {
		t.Fatalf("establishList after a stalled start: %v", err)
	}
	if !view.blocked {
		t.Fatal("the scripted stream never stalled, so the retry was not exercised")
	}
	if run.listMatch.List == nil || len(run.listMatch.List.Occupied) != len(inv.Layout.Slots) {
		t.Fatalf("settled list = %+v, want the full grid", run.listMatch.List)
	}
	if run.lastFrame.ID == 0 {
		t.Fatal("the retry did not keep the settled frame")
	}
}

type stalledListStartView struct {
	frames  int
	blocked bool
}

func (v *stalledListStartView) Observe() (platform.Observation, error) {
	return platform.Observation{Ready: true, Geometry: 1}, nil
}

func (v *stalledListStartView) Dispatch(op platform.InputOp) platform.InputResult {
	return platform.InputResult{Status: platform.InputPosted}
}

func (v *stalledListStartView) Frame(ctx context.Context, req platform.FrameRequest) (platform.Frame, error) {
	if v.frames == 0 {
		v.frames++
		return platform.Frame{ID: 1, Geometry: 1, ArrivedAt: time.Now()}, nil
	}
	if !v.blocked {
		v.blocked = true
		<-ctx.Done()
		return platform.Frame{}, ctx.Err()
	}
	v.frames++
	return platform.Frame{ID: uint64(v.frames + 1), Geometry: 1, ArrivedAt: time.Now()}, nil
}

// The first wait can also end without any decidable frame at all: the
// precheck already verified the page, but activation can leave the stream
// silent until the client redraws. The retry must not depend on having
// seen one warehouse verdict in the failed wait.
func TestEstablishListRetriesSilentStart(t *testing.T) {
	inv := &Inventory{
		Layout:       MacCloudLayout(),
		Matcher:      &scrollScriptMatcher{rows: 4, columns: 5},
		ActionBudget: 60 * time.Millisecond,
		StableFrames: 3,
	}
	view := &silentListStartView{}
	run := &inventoryRun{inv: inv, view: view, geo: 1, position: 1}

	if err := run.establishList(context.Background()); err != nil {
		t.Fatalf("establishList after a silent start: %v", err)
	}
	if !view.blocked {
		t.Fatal("the scripted stream never blocked, so the retry was not exercised")
	}
	if run.listMatch.List == nil || len(run.listMatch.List.Occupied) != len(inv.Layout.Slots) {
		t.Fatalf("settled list = %+v, want the full grid", run.listMatch.List)
	}
}

type silentListStartView struct {
	frames  int
	blocked bool
}

func (v *silentListStartView) Observe() (platform.Observation, error) {
	return platform.Observation{Ready: true, Geometry: 1}, nil
}

func (v *silentListStartView) Dispatch(op platform.InputOp) platform.InputResult {
	return platform.InputResult{Status: platform.InputPosted}
}

func (v *silentListStartView) Frame(ctx context.Context, req platform.FrameRequest) (platform.Frame, error) {
	if !v.blocked {
		v.blocked = true
		<-ctx.Done()
		return platform.Frame{}, ctx.Err()
	}
	v.frames++
	return platform.Frame{ID: uint64(v.frames), Geometry: 1, ArrivedAt: time.Now()}, nil
}

// A prior stable observation counts toward the settle window without
// replacing the fresh confirmations: with StableFrames=3, a seed plus two
// fresh frames must settle even though two alone would not.
func TestAwaitSettledSeededCountsPriorObservation(t *testing.T) {
	inv := &Inventory{
		Layout:       MacCloudLayout(),
		Matcher:      &scrollScriptMatcher{rows: 5, columns: 4},
		StableFrames: 3,
		ActionBudget: time.Second,
		RecheckEvery: time.Millisecond,
	}
	view := &scrollScriptView{frames: []platform.Frame{scrollFrame(2), scrollFrame(3)}}
	run := &inventoryRun{inv: inv, view: view, geo: 1}
	expected := func(m PageMatch) bool { return m.Page == PageWarehouse && m.List != nil }
	seed := &awaitOutcome{
		frame:    scrollFrame(1),
		match:    inv.matcher().Match(scrollFrame(0)),
		expected: true,
	}
	out := run.awaitSettledSeeded(context.Background(), expected, seed)
	if out.class != awaitStable {
		t.Fatalf("seeded settle = %v, want stable", out.class)
	}
	if view.calls != 2 {
		t.Fatalf("seeded settle observed %d fresh frames, want 2", view.calls)
	}
	if out.frame.ID != 3 {
		t.Fatalf("seeded settle ended on frame %d, want 3", out.frame.ID)
	}
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

// TestTopConfirmedRejectsMovedClaims pins that a genuine Lost or a
// displaced top claim cannot start traversal even when the scrollbar thumb
// looks unchanged. An Undecided comparison is different: it carries no
// movement evidence, so a stable thumb at the top may support the claim.
func TestTopConfirmedRejectsMovedClaims(t *testing.T) {
	top := PageMatch{Page: PageWarehouse, List: &ListEvidence{Top: true}}
	notTop := PageMatch{Page: PageWarehouse, List: &ListEvidence{Top: false}}
	cases := []struct {
		name     string
		out      scrollOutcome
		beforeOK bool
		afterOK  bool
		want     bool
	}{
		{"stationary top", scrollOutcome{stationary: true, afterMatch: top}, true, true, true},
		{"undecided stable thumb", scrollOutcome{afterMatch: top}, true, true, true},
		{"lost with stable thumb", scrollOutcome{lost: true, afterMatch: top}, true, true, false},
		{"lost undecided stable thumb", scrollOutcome{lost: true, afterMatch: top, match: ScrollMatch{Undecided: true}}, true, true, true},
		{"lost undecided moved thumb", scrollOutcome{lost: true, afterMatch: top, match: ScrollMatch{Undecided: true}}, true, true, false},
		{"displaced with stable thumb", scrollOutcome{displaced: true, afterMatch: top}, true, true, false},
		{"stationary but not top", scrollOutcome{stationary: true, afterMatch: notTop}, true, true, false},
		{"thumb moved", scrollOutcome{afterMatch: top}, true, true, false},
		{"thumb unreadable", scrollOutcome{afterMatch: top}, false, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			beforeStart, afterStart := 226, 226
			if tc.name == "thumb moved" || tc.name == "lost undecided moved thumb" {
				afterStart = 240
			}
			if got := topConfirmed(tc.out, tc.beforeOK, tc.afterOK, beforeStart, afterStart); got != tc.want {
				t.Fatalf("topConfirmed = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestAwaitScrollDoesNotCommitRepeatedTotalWhileMoving pins the unsafe
// case that a repeated row count alone cannot settle: A→B is undecided,
// the total before→B still reports four rows, but B→C later moves another
// row. The traversal must stay uncommitted and end as Lost, not accept B.
func TestAwaitScrollDoesNotCommitRepeatedTotalWhileMoving(t *testing.T) {
	l := sixRowScrollLayout()
	content := image.Rect(0, 0, 100, 100)
	before := platform.Frame{ID: 1, Geometry: 1, Image: image.NewRGBA(content), Content: content}
	first := platform.Frame{ID: 2, Geometry: 1, Image: image.NewRGBA(content), Content: content}
	stillMoving := platform.Frame{ID: 3, Geometry: 1, Image: image.NewRGBA(content), Content: content}
	later := platform.Frame{ID: 4, Geometry: 1, Image: image.NewRGBA(content), Content: content}
	view := &scrollScriptView{frames: []platform.Frame{first, stillMoving, later}, blockAfter: true}
	m := &scrollScriptMatcher{rows: l.Rows, columns: l.Columns, verdict: map[scrollPair]ScrollMatch{
		{1, 2}: displacedScan(l, 4),
		{2, 3}: {Evidence: "scripted relative frame still moving"},
		{1, 3}: displacedScan(l, 4),
		{3, 4}: displacedScan(l, 1),
		{1, 4}: {Lost: true, Evidence: "scripted total overlap lost after another row"},
	}}
	run := newScrollRun(l, view, m, 80*time.Millisecond)
	out, err := run.awaitScroll(context.Background(), before)
	if err != nil {
		t.Fatalf("awaitScroll: %v", err)
	}
	if out.displaced || !out.lost || !out.unsettled {
		t.Fatalf("outcome %+v, want an uncommitted Lost result", out)
	}
}

// TestAwaitScrollSettlesIdleReuseWithSameFrameID covers production idle
// confirmations: the platform copies the latest complete frame into a new
// RGBA buffer, so the image pointer changes while Frame.ID and content stay
// the same. That must settle the candidate rather than wait out the budget.
func TestAwaitScrollSettlesIdleReuseWithSameFrameID(t *testing.T) {
	l := sixRowScrollLayout()
	content := image.Rect(0, 0, 100, 100)
	before := platform.Frame{ID: 1, Geometry: 1, Image: image.NewRGBA(content), Content: content}
	candidate := platform.Frame{ID: 2, Geometry: 1, Image: image.NewRGBA(content), Content: content}
	idle := platform.Frame{ID: 2, Geometry: 1, Image: image.NewRGBA(content), Content: content, Reused: true}
	view := &scrollScriptView{frames: []platform.Frame{candidate, idle}, blockAfter: true}
	m := &scrollScriptMatcher{rows: l.Rows, columns: l.Columns, verdict: map[scrollPair]ScrollMatch{
		{1, 2}: displacedScan(l, 4),
	}}
	run := newScrollRun(l, view, m, 80*time.Millisecond)
	out, err := run.awaitScroll(context.Background(), before)
	if err != nil {
		t.Fatalf("awaitScroll: %v", err)
	}
	if !out.displaced || out.lost || out.after.ID != 2 {
		t.Fatalf("outcome %+v, want the idle reuse to settle frame 2", out)
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
	if !out.unsettled {
		t.Fatalf("outcome %+v, want the lost displacement marked as unsettled", out)
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

// TestAwaitScrollContinuousStreamRespectsBudget pins the live-stream
// case: fresh frames keep arriving, so Frame never returns a deadline
// error on its own. The wait must still end at its budget.
func TestAwaitScrollContinuousStreamRespectsBudget(t *testing.T) {
	l := sixRowScrollLayout()
	before := scrollFrame(1)
	m := &scrollScriptMatcher{rows: l.Rows, columns: l.Columns, alwaysStationary: true}
	budget := 40 * time.Millisecond
	run := newScrollRun(l, &endlessFrameView{}, m, budget)
	started := time.Now()
	out, err := run.awaitScroll(context.Background(), before)
	if err != nil {
		t.Fatalf("awaitScroll: %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("awaitScroll ran for %s with a %s budget", elapsed, budget)
	}
	if !out.stationary || out.displaced || out.lost {
		t.Fatalf("outcome %+v, want Stationary at the budget", out)
	}
}

// failingObserveView fails the readiness check from the configured call
// onward, leaving the scripted frame stream untouched.
type failingObserveView struct {
	*scrollScriptView
	observes int
	failAt   int
}

func (v *failingObserveView) Observe() (platform.Observation, error) {
	v.observes++
	if v.observes >= v.failAt {
		return platform.Observation{}, errors.New("scripted observation failure")
	}
	return platform.Observation{Ready: true, Geometry: 1}, nil
}

// scrollSessionController runs one scripted scroll on the session's
// producer goroutine, so scroll sees a real input-permission handle.
type scrollSessionController struct {
	run   *inventoryRun
	errCh chan error
}

func (c *scrollSessionController) Prepare(context.Context, *scan.ControllerHandle) error { return nil }

func (c *scrollSessionController) Collect(ctx context.Context, h *scan.ControllerHandle) error {
	c.run.h = h
	err := c.run.scroll(ctx)
	c.errCh <- err
	return err
}

func runScriptedScroll(t *testing.T, run *inventoryRun) error {
	t.Helper()
	ctrl := &scrollSessionController{run: run, errCh: make(chan error, 1)}
	sess, err := scan.Start(scan.Config{
		ID:         scan.ID("scripted-scroll-diagnostics"),
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
		select {
		case <-sess.Done():
		case <-time.After(5 * time.Second):
			t.Fatal("the scripted scroll session never sealed")
		}
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("the scripted scroll never returned")
		return nil
	}
}

func newScriptedScrollRun(l Layout, view SessionView, m Matcher, dir string, retries int, budget time.Duration) *inventoryRun {
	return &inventoryRun{
		inv: &Inventory{
			Layout:        l,
			Matcher:       m,
			DiagnosticDir: dir,
			MaxRetries:    retries,
			ScrollBudget:  budget,
		},
		view:     view,
		geo:      1,
		position: 1,
		viewport: 3,
	}
}

func diagnosticPNGs(t *testing.T, dir string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.png"))
	if err != nil {
		t.Fatalf("glob diagnostics: %v", err)
	}
	return files
}

// TestScrollVerdictFailureWritesDiagnostics drives the real scroll path to
// its Stationary limit: the verdict stopped the walk, so one before/after
// pair is written before the fault returns.
func TestScrollVerdictFailureWritesDiagnostics(t *testing.T) {
	l := sixRowScrollLayout()
	frame := func(id uint64) platform.Frame {
		return platform.Frame{ID: id, Geometry: 1, Image: image.NewRGBA(image.Rect(0, 0, 1280, 1000))}
	}
	view := &scrollScriptView{frames: []platform.Frame{frame(1), frame(2)}, blockAfter: true}
	m := &scrollScriptMatcher{rows: l.Rows, columns: l.Columns, verdict: map[scrollPair]ScrollMatch{
		{1, 2}: {Stationary: true, Evidence: "scripted stationary"},
	}}
	dir := t.TempDir()
	run := newScriptedScrollRun(l, view, m, dir, -1, 40*time.Millisecond)

	if err := runScriptedScroll(t, run); err == nil {
		t.Fatal("a Stationary limit must stop the scroll")
	}
	files := diagnosticPNGs(t, dir)
	if len(files) != 2 {
		t.Fatalf("the stopped scroll wrote %d crops, want a before/after pair", len(files))
	}
	for _, path := range files {
		if !strings.Contains(filepath.Base(path), "-scroll-stationary-") {
			t.Fatalf("crop %q does not name the stopping verdict", path)
		}
	}
}

// TestScrollRetryAfterVerdictDoesNotDumpPreviousAttempt pins the reset
// between attempts: the first try ends Stationary and retries, the second
// stops before producing a verdict, and no frame from the first try may be
// reported as this failure's evidence.
func TestScrollRetryAfterVerdictDoesNotDumpPreviousAttempt(t *testing.T) {
	l := sixRowScrollLayout()
	frame := func(id uint64) platform.Frame {
		return platform.Frame{ID: id, Geometry: 1, Image: image.NewRGBA(image.Rect(0, 0, 1280, 1000))}
	}
	view := &failingObserveView{
		scrollScriptView: &scrollScriptView{frames: []platform.Frame{frame(1), frame(2)}, blockAfter: true},
		failAt:           2,
	}
	m := &scrollScriptMatcher{rows: l.Rows, columns: l.Columns, verdict: map[scrollPair]ScrollMatch{
		{1, 2}: {Stationary: true, Evidence: "scripted stationary"},
	}}
	dir := t.TempDir()
	run := newScriptedScrollRun(l, view, m, dir, 1, 40*time.Millisecond)

	err := runScriptedScroll(t, run)
	if err == nil || !strings.Contains(err.Error(), "scripted observation failure") {
		t.Fatalf("scroll error = %v, want the second attempt's readiness failure", err)
	}
	if files := diagnosticPNGs(t, dir); len(files) != 0 {
		t.Fatalf("a failure before the verdict wrote %v, want no crops", files)
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
	err := run.admit(slot, out, platform.Frame{}, PageMatch{}, 0, 0, 0, AssociationTransition, 0, nil, 1, "scripted slot", nil)
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

// TestDetailBaselinePrefersLastAdmittedDetail pins the cloud-video lag
// guard: a freshly taken pre-click list frame may still show the previous
// item's stale detail, so a later same-detail click must compare against
// the last admitted detail frame instead.
func TestDetailBaselinePrefersLastAdmittedDetail(t *testing.T) {
	run := &inventoryRun{}
	pre := platform.Frame{ID: 20}
	if got := run.detailBaseline(pre); got.ID != pre.ID {
		t.Fatalf("baseline before any admission = %d, want the pre-click frame %d", got.ID, pre.ID)
	}
	run.lastDetailFrame = platform.Frame{ID: 10}
	if got := run.detailBaseline(pre); got.ID != 10 {
		t.Fatalf("baseline after admission = %d, want the last admitted detail 10", got.ID)
	}
}
