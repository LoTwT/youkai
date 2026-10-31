package collect

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"log"
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
	// nonListFrames makes Match report an unknown page for these frame
	// IDs, modelling a popup, loading page or another non-warehouse frame.
	nonListFrames map[uint64]bool
}

func (m *scrollScriptMatcher) Match(f platform.Frame) PageMatch {
	if m.nonListFrames[f.ID] {
		return PageMatch{Page: PageUnknown, Evidence: "scripted non-list page"}
	}
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
	// dispatches counts dispatched input operations, so a test can pin
	// how many drags one scroll action sent.
	dispatches int
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
	v.dispatches++
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

// scriptedScrollTiming is the scaled-down scroll timing the scripted runs
// use: the waits exercise the streak and verdict logic with millisecond
// budgets, so the profile's floors shrink with them (the production profile
// and its enforcement have their own dedicated tests).
func scriptedScrollTiming() *ScrollTiming {
	return &ScrollTiming{
		MotionStartBudget:  20 * time.Millisecond,
		SettleBudget:       50 * time.Millisecond,
		SettleObservations: 2,
	}
}

func newScrollRun(l Layout, view SessionView, m Matcher, budget time.Duration) *inventoryRun {
	return &inventoryRun{
		inv: &Inventory{
			Layout:       l,
			Matcher:      m,
			ScrollBudget: budget,
			ScrollTiming: scriptedScrollTiming(),
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

// TestAwaitScrollIdleReuseWithSameFrameIDDoesNotSettle pins the
// independent-observation rule: the platform re-serves the same idle frame
// on every recheck interval, so reused confirmations — however many — are
// one observation of one idle state and must not complete the settle
// streak. The wait ends as the unsettled Lost at the budget instead of
// committing on echoed stillness.
func TestAwaitScrollIdleReuseWithSameFrameIDDoesNotSettle(t *testing.T) {
	l := sixRowScrollLayout()
	content := image.Rect(0, 0, 100, 100)
	before := platform.Frame{ID: 1, Geometry: 1, Image: image.NewRGBA(content), Content: content}
	candidate := platform.Frame{ID: 2, Geometry: 1, Image: image.NewRGBA(content), Content: content}
	idle := func() platform.Frame {
		return platform.Frame{ID: 2, Geometry: 1, Image: image.NewRGBA(content), Content: content, Reused: true}
	}
	view := &scrollScriptView{frames: []platform.Frame{
		candidate, idle(), idle(), idle(), idle(),
	}, blockAfter: true}
	m := &scrollScriptMatcher{rows: l.Rows, columns: l.Columns, verdict: map[scrollPair]ScrollMatch{
		{1, 2}: displacedScan(l, 4),
	}}
	run := newScrollRun(l, view, m, 80*time.Millisecond)
	out, err := run.awaitScroll(context.Background(), before)
	if err != nil {
		t.Fatalf("awaitScroll: %v", err)
	}
	if out.displaced || out.stationary || !out.lost || !out.unsettled {
		t.Fatalf("outcome %+v, want the unsettled Lost: idle echoes are not settle confirmations", out)
	}
	if out.settled {
		t.Fatalf("outcome %+v: the reused confirmations must not settle the candidate", out)
	}
}

// TestAwaitScrollSettlesOnIndependentObservations is the positive
// counterpart: two independent fresh confirmations — with a neutral idle
// echo between them — complete the streak and commit the candidate on the
// second fresh frame.
func TestAwaitScrollSettlesOnIndependentObservations(t *testing.T) {
	l := sixRowScrollLayout()
	content := image.Rect(0, 0, 100, 100)
	before := platform.Frame{ID: 1, Geometry: 1, Image: image.NewRGBA(content), Content: content}
	candidate := platform.Frame{ID: 2, Geometry: 1, Image: image.NewRGBA(content), Content: content}
	idle := func() platform.Frame {
		return platform.Frame{ID: 2, Geometry: 1, Image: image.NewRGBA(content), Content: content, Reused: true}
	}
	still := func(id uint64) platform.Frame {
		return platform.Frame{ID: id, Geometry: 1, Image: image.NewRGBA(content), Content: content}
	}
	view := &scrollScriptView{frames: []platform.Frame{
		candidate, still(3), idle(), still(4),
	}, blockAfter: true}
	m := &scrollScriptMatcher{rows: l.Rows, columns: l.Columns, verdict: map[scrollPair]ScrollMatch{
		{1, 2}: displacedScan(l, 4),
		{2, 3}: {Stationary: true, Evidence: "scripted settled"},
		{2, 4}: {Stationary: true, Evidence: "scripted settled"},
	}}
	run := newScrollRun(l, view, m, 80*time.Millisecond)
	out, err := run.awaitScroll(context.Background(), before)
	if err != nil {
		t.Fatalf("awaitScroll: %v", err)
	}
	if !out.displaced || out.lost || out.after.ID != 2 {
		t.Fatalf("outcome %+v, want the candidate committed on the second fresh confirmation", out)
	}
	if !out.settled || out.settleFrameID != 4 {
		t.Fatalf("outcome %+v: the settle must close on the second fresh frame 4, not an idle echo", out)
	}
}

// TestAwaitScrollCommitsLatestSettledCandidate pins the two-stage rule:
// four rows is only a candidate, a later frame shows five rows total, and
// the commit waits until the candidate stayed stable for the profile's
// consecutive settle observations.
func TestAwaitScrollCommitsLatestSettledCandidate(t *testing.T) {
	l := sixRowScrollLayout()
	before := scrollFrame(1)
	view := &scrollScriptView{frames: []platform.Frame{
		scrollFrame(2), // four rows
		scrollFrame(3), // five rows total
		scrollFrame(4), // still five rows
		scrollFrame(5), // still five rows
	}}
	m := &scrollScriptMatcher{rows: l.Rows, columns: l.Columns, verdict: map[scrollPair]ScrollMatch{
		{1, 2}: displacedScan(l, 4),
		{1, 3}: displacedScan(l, 5),
		{3, 4}: {Stationary: true, Evidence: "scripted settled"},
		{3, 5}: {Stationary: true, Evidence: "scripted settled"},
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
// displacement followed by the profile's consecutive stationary observations
// commits the candidate.
func TestAwaitScrollCommitsStableFourRows(t *testing.T) {
	l := sixRowScrollLayout()
	before := scrollFrame(1)
	view := &scrollScriptView{frames: []platform.Frame{
		scrollFrame(2),
		scrollFrame(3),
		scrollFrame(4),
	}}
	m := &scrollScriptMatcher{rows: l.Rows, columns: l.Columns, verdict: map[scrollPair]ScrollMatch{
		{1, 2}: displacedScan(l, 4),
		{2, 3}: {Stationary: true, Evidence: "scripted settled"},
		{2, 4}: {Stationary: true, Evidence: "scripted settled"},
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

// paintMotionFrame paints one frame with exactly the regions MatchScroll
// reads — the list viewport pattern, one selection border and a scrollbar
// thumb — for the internal tests that run awaitScroll's motion bookkeeping
// against the real pixel matcher. The pattern mirrors scroll_evidence_test's
// paintScrollFrame (per-row base, per-(row, x) identity hash, fine
// y-texture); the page verdicts come from motionPageMatcher instead of
// painted page probes.
func paintMotionFrame(l Layout, id uint64, offset, selection, thumbStart, thumbEnd int, noise bool) platform.Frame {
	img := image.NewRGBA(image.Rectangle{Max: l.Image})
	for y := l.ListViewport.Min.Y; y < l.ListViewport.Max.Y; y++ {
		g := offset + (y-l.ListViewport.Min.Y)/l.RowPitch
		for x := l.ListViewport.Min.X; x < l.ListViewport.Max.X; x++ {
			v := 60 + (137*g*g+29*g)%120
			h := uint32(g)*1000003 + uint32(x)*97
			h ^= h >> 16
			h *= 0x45d9f3b
			h ^= h >> 16
			v += int(h % 120)
			if (y-l.ListViewport.Min.Y)%49 >= 24 {
				v += 12
			}
			if (y-l.ListViewport.Min.Y)%4 == 0 {
				v += 45
			}
			if noise {
				v += ((x*7 + y*13) % 21) - 10
			}
			if v < 0 {
				v = 0
			}
			if v > 255 {
				v = 255
			}
			img.SetRGBA(x, y, color.RGBA{R: uint8(v), G: uint8(v), B: uint8(v), A: 255})
		}
	}
	if selection >= 0 {
		r := l.BorderRect(selection).Intersect(img.Bounds())
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				img.SetRGBA(x, y, color.RGBA{R: 224, G: 208, B: 67, A: 255})
			}
		}
	}
	for y := thumbStart; y <= thumbEnd; y++ {
		if y >= l.ScrollTrack.Min.Y && y < l.ScrollTrack.Max.Y {
			img.SetRGBA(l.ScrollTrack.Min.X, y, color.RGBA{R: 200, G: 200, B: 200, A: 255})
		}
	}
	return platform.Frame{ID: id, Geometry: 1, Image: img, Content: l.Content}
}

// motionPageMatcher runs the real stage 2 MatchScroll and its threshold
// reporters behind a scripted warehouse page verdict, so awaitScroll's
// motion bookkeeping exercises real pixel comparisons without painting the
// page probes.
type motionPageMatcher struct {
	*ScrollEvidenceMatcher
}

func (m motionPageMatcher) Match(f platform.Frame) PageMatch {
	l := m.L
	return PageMatch{
		Page:   PageWarehouse,
		List:   &ListEvidence{Occupied: make([]bool, len(l.Slots)), Selected: 0},
		Wheel:  &WheelEvidence{Position: 1},
		Filter: &FilterEvidence{State: FilterActive},
	}
}

// TestAwaitScrollMotionIgnoresCloudNoise pins the movement-evidence rule:
// a static list behind codec noise fails the zero-shift MAD (5–10 against
// the 4.0 threshold) but proves stillness by normalized correlation, and
// the late-start window must not record motion on it. With nothing but
// noisy-but-static frames, no motion is recorded at all.
func TestAwaitScrollMotionIgnoresCloudNoise(t *testing.T) {
	l := MacCloudLayout()
	m := motionPageMatcher{NewScrollEvidenceMatcher(NewPixelMatcher(l, MacCloudCalibration()))}
	before := paintMotionFrame(l, 1, 0, l.Rows*l.Columns-1, 300, 430, false)
	view := &scrollScriptView{frames: []platform.Frame{
		paintMotionFrame(l, 2, 0, l.Rows*l.Columns-1, 300, 430, true),
		paintMotionFrame(l, 3, 0, l.Rows*l.Columns-1, 300, 430, true),
		paintMotionFrame(l, 4, 0, l.Rows*l.Columns-1, 300, 430, true),
	}, blockAfter: true}
	// The real matcher's searches are slow under the race detector; the
	// budget only bounds the failure path, and the wait returns as soon as
	// it settles, so a production-sized budget keeps the test robust.
	run := newScrollRun(l, view, m, 5*time.Second)
	out, err := run.awaitScroll(context.Background(), before)
	if err != nil {
		t.Fatalf("awaitScroll: %v", err)
	}
	if out.motionStarted || out.motionFrameID != 0 {
		t.Fatalf("motion recorded on frame %d of a noisy-but-static wait: codec noise above the MAD threshold is not movement (outcome %+v)",
			out.motionFrameID, out)
	}
	if !out.stationary {
		t.Fatalf("outcome %+v, want the stationary deadline verdict on the static noisy list", out)
	}
}

// TestAwaitScrollMotionRecordsRealDisplacement is the positive side of the
// movement-evidence rule: after the noisy late-start window, the frame that
// verifiably displaced records the motion.
func TestAwaitScrollMotionRecordsRealDisplacement(t *testing.T) {
	l := MacCloudLayout()
	m := motionPageMatcher{NewScrollEvidenceMatcher(NewPixelMatcher(l, MacCloudCalibration()))}
	before := paintMotionFrame(l, 1, 0, l.Rows*l.Columns-1, 300, 430, false)
	view := &scrollScriptView{frames: []platform.Frame{
		paintMotionFrame(l, 2, 0, l.Rows*l.Columns-1, 300, 430, true),
		paintMotionFrame(l, 3, 4, l.Columns-1, 400, 530, true),
		paintMotionFrame(l, 4, 4, l.Columns-1, 400, 530, true),
		paintMotionFrame(l, 5, 4, l.Columns-1, 400, 530, true),
	}, blockAfter: true}
	// See the noise test: a production-sized budget keeps the real
	// matcher's race-detector slowness out of the assertion path.
	run := newScrollRun(l, view, m, 5*time.Second)
	out, err := run.awaitScroll(context.Background(), before)
	if err != nil {
		t.Fatalf("awaitScroll: %v", err)
	}
	if !out.motionStarted || out.motionFrameID != 3 {
		t.Fatalf("outcome %+v: motion must be recorded at the displaced frame 3, not the noisy frame 2", out)
	}
	if !out.displaced || !out.settled {
		t.Fatalf("outcome %+v: the four-row advance must verify and settle", out)
	}
}

// TestAwaitScrollMotionStartBudgetFloorsDeadline pins the enforced
// motion-start floor: with no observed movement, the wait's deadline is at
// least MotionStartBudget after it began, whatever the configured scroll
// budget — stillness inside F7's late-start window is not evidence the drag
// failed, and a stationary verdict there must not become terminal.
func TestAwaitScrollMotionStartBudgetFloorsDeadline(t *testing.T) {
	l := sixRowScrollLayout()
	m := &scrollScriptMatcher{rows: l.Rows, columns: l.Columns, alwaysStationary: true}
	run := &inventoryRun{
		inv: &Inventory{
			Layout:       l,
			Matcher:      m,
			ScrollBudget: 40 * time.Millisecond,
			ScrollTiming: &ScrollTiming{
				MotionStartBudget:  250 * time.Millisecond,
				SettleBudget:       50 * time.Millisecond,
				SettleObservations: 2,
			},
		},
		view: &endlessFrameView{},
		geo:  1,
	}
	started := time.Now()
	out, err := run.awaitScroll(context.Background(), scrollFrame(1))
	if err != nil {
		t.Fatalf("awaitScroll: %v", err)
	}
	if !out.stationary {
		t.Fatalf("outcome %+v, want the stationary deadline verdict", out)
	}
	if elapsed := time.Since(started); elapsed < 200*time.Millisecond {
		t.Fatalf("the wait ended after %s with a 250ms motion-start floor; a stationary verdict inside the late-start window must not be terminal",
			elapsed)
	}
}

// paintUniformMotionFrame paints a near-uniform viewport under an
// independent codec-like dither (the phase differs per frame), the exact
// shape the review measured on a static cloud list: err0 above the MAD
// threshold, an ncc0 near zero and an information std far below the gate.
func paintUniformMotionFrame(l Layout, id uint64, value, phase, thumbStart, thumbEnd int) platform.Frame {
	f := paintMotionFrame(l, id, 0, -1, thumbStart, thumbEnd, false)
	for y := l.ListViewport.Min.Y; y < l.ListViewport.Max.Y; y++ {
		for x := l.ListViewport.Min.X; x < l.ListViewport.Max.X; x++ {
			v := value + ((x*7 + y*13 + phase) % 21) - 10
			if v < 0 {
				v = 0
			}
			if v > 255 {
				v = 255
			}
			f.Image.SetRGBA(x, y, color.RGBA{R: uint8(v), G: uint8(v), B: uint8(v), A: 255})
		}
	}
	return f
}

// TestAwaitScrollMotionIgnoresLowInformationNoise pins the information gate
// on the NCC-based movement evidence: a near-uniform list under codec noise
// decorrelates to an ncc0 near zero, and that must not read as movement —
// only a correlation measured on a support rich enough to mean something
// counts.
func TestAwaitScrollMotionIgnoresLowInformationNoise(t *testing.T) {
	l := MacCloudLayout()
	m := motionPageMatcher{NewScrollEvidenceMatcher(NewPixelMatcher(l, MacCloudCalibration()))}
	before := paintUniformMotionFrame(l, 1, 100, 0, 300, 430)
	first := m.MatchScroll(before, paintUniformMotionFrame(l, 2, 100, 7, 300, 430))
	if first.Stationary || first.Displaced || first.Lost || !first.Undecided {
		t.Fatalf("uniform noisy pair = %+v, want undecided (%s)", first, first.Evidence)
	}
	if first.Metrics == nil || !first.Metrics.NCCMeasured {
		t.Fatalf("uniform noisy pair carries no normalized metrics: %+v", first.Metrics)
	}
	ncc0, ok := first.Metrics.RowNCC[0]
	if !ok {
		t.Fatalf("uniform noisy pair carries no zero-shift correlation: %+v", first.Metrics)
	}
	if ncc0 >= m.C.ScrollStationaryNCC {
		t.Fatalf("uniform noisy pair ncc0 %.3f is not the low-correlation shape this test needs", ncc0)
	}
	if first.Metrics.InfoStd >= m.C.ScrollInfoStdMin {
		t.Fatalf("uniform noisy pair info std %.1f is not below the %.0f gate this test needs",
			first.Metrics.InfoStd, m.C.ScrollInfoStdMin)
	}
	view := &scrollScriptView{frames: []platform.Frame{
		paintUniformMotionFrame(l, 2, 100, 7, 300, 430),
		paintUniformMotionFrame(l, 3, 100, 3, 300, 430),
	}, blockAfter: true}
	run := newScrollRun(l, view, m, 5*time.Second)
	out, err := run.awaitScroll(context.Background(), before)
	if err != nil {
		t.Fatalf("awaitScroll: %v", err)
	}
	if out.motionStarted || out.motionFrameID != 0 {
		t.Fatalf("motion recorded on frame %d of a low-information noisy wait: a near-zero ncc0 over a uniform support is noise, not movement (outcome %+v)",
			out.motionFrameID, out)
	}
}

// TestAwaitScrollMotionRecordsRichUndecidedFrame is the positive
// counterpart: a genuinely moving list whose displacement verdict is still
// undecided (the only surviving overlap row is uniform, so no candidate
// corroborates) must record motion — the zero-shift correlation is low on
// an information-rich support.
func TestAwaitScrollMotionRecordsRichUndecidedFrame(t *testing.T) {
	l := MacCloudLayout()
	m := motionPageMatcher{NewScrollEvidenceMatcher(NewPixelMatcher(l, MacCloudCalibration()))}
	// Rich content, four rows advanced, and the single surviving overlap
	// row (global row 4) painted uniform in both frames so no displacement
	// candidate can corroborate: the verdict stays undecided while the
	// zero-shift comparison sees four misaligned rich rows.
	before := paintMotionFrame(l, 1, 0, -1, 300, 430, false)
	after := paintMotionFrame(l, 2, 4, -1, 400, 530, true)
	uniformRow := func(f platform.Frame, y0, y1 int) {
		for y := y0; y < y1; y++ {
			for x := l.ListViewport.Min.X; x < l.ListViewport.Max.X; x++ {
				f.Image.SetRGBA(x, y, color.RGBA{R: 90, G: 90, B: 90, A: 255})
			}
		}
	}
	uniformRow(before, l.ListViewport.Max.Y-l.RowPitch, l.ListViewport.Max.Y)
	uniformRow(after, l.ListViewport.Min.Y, l.ListViewport.Min.Y+l.RowPitch)
	sm := m.MatchScroll(before, after)
	if sm.Stationary || sm.Displaced || sm.Lost || !sm.Undecided {
		t.Fatalf("verdict %+v, want the undecided displacement this test needs (%s)", sm, sm.Evidence)
	}
	if sm.Metrics == nil || !sm.Metrics.NCCMeasured {
		t.Fatal("the undecided verdict lacks normalized metrics")
	}
	if ncc0 := sm.Metrics.RowNCC[0]; ncc0 >= m.C.ScrollStationaryNCC {
		t.Fatalf("ncc0 %.3f is not the low-correlation shape this test needs", ncc0)
	}
	if sm.Metrics.InfoStd < m.C.ScrollInfoStdMin {
		t.Fatalf("info std %.1f below the %.0f gate: the support must be rich for the positive case",
			sm.Metrics.InfoStd, m.C.ScrollInfoStdMin)
	}
	view := &scrollScriptView{frames: []platform.Frame{after}, blockAfter: true}
	run := newScrollRun(l, view, m, 5*time.Second)
	out, err := run.awaitScroll(context.Background(), before)
	if err != nil {
		t.Fatalf("awaitScroll: %v", err)
	}
	if !out.motionStarted || out.motionFrameID != after.ID {
		t.Fatalf("outcome %+v: the rich undecided frame must record motion", out)
	}
}

// TestScrollTimingRejectsSingleObservationSettle pins the configuration
// rule: a settle profile requiring only one observation contradicts the
// consecutive-stability settle, so it is refused and the default profile
// applies instead of being silently honored.
func TestScrollTimingRejectsSingleObservationSettle(t *testing.T) {
	want := MacCloudScrollTiming()
	single := &ScrollTiming{MotionStartBudget: time.Second, SettleBudget: 2 * time.Second, SettleObservations: 1}
	if got := (&Inventory{ScrollTiming: single}).scrollTiming(); got != want {
		t.Fatalf("single-observation override = %+v, want the fallback to the default profile (one observation is not a settle)",
			got)
	}
	if got := (&Inventory{ScrollTiming: single}).scrollTiming(); got.SettleObservations < 2 {
		t.Fatalf("resolved profile requires %d observations; the settle must never commit on one", got.SettleObservations)
	}
}

// TestAwaitScrollSingleStableObservationDoesNotSettle pins the consecutive
// settle streak: one candidate-relative Stationary observation followed by
// a stalled stream is not a settle — the cloud can deliver one still frame
// inside an ongoing animation, so the commit waits for the profile's second
// confirmation and ends as the unsettled Lost at the budget.
func TestAwaitScrollSingleStableObservationDoesNotSettle(t *testing.T) {
	l := sixRowScrollLayout()
	before := scrollFrame(1)
	view := &scrollScriptView{frames: []platform.Frame{
		scrollFrame(2), // four rows
		scrollFrame(3), // one still observation
	}, blockAfter: true}
	m := &scrollScriptMatcher{rows: l.Rows, columns: l.Columns, verdict: map[scrollPair]ScrollMatch{
		{1, 2}: displacedScan(l, 4),
		{2, 3}: {Stationary: true, Evidence: "scripted one still observation"},
	}}
	run := newScrollRun(l, view, m, 40*time.Millisecond)
	out, err := run.awaitScroll(context.Background(), before)
	if err != nil {
		t.Fatalf("awaitScroll: %v", err)
	}
	if out.displaced || out.stationary || !out.lost || !out.unsettled {
		t.Fatalf("outcome %+v, want the unsettled Lost: one observation is not a settle", out)
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
// onward, leaving the scripted frame stream untouched. blockAt makes one
// Frame call block until its request deadline, modelling a wait that ends
// on the deadline between two scripted frames.
type failingObserveView struct {
	*scrollScriptView
	observes   int
	failAt     int
	blockAt    int
	frameCalls int
}

func (v *failingObserveView) Observe() (platform.Observation, error) {
	v.observes++
	if v.observes >= v.failAt {
		return platform.Observation{}, errors.New("scripted observation failure")
	}
	return platform.Observation{Ready: true, Geometry: 1}, nil
}

func (v *failingObserveView) Frame(ctx context.Context, req platform.FrameRequest) (platform.Frame, error) {
	v.frameCalls++
	if v.frameCalls == v.blockAt {
		<-ctx.Done()
		return platform.Frame{}, ctx.Err()
	}
	return v.scrollScriptView.Frame(ctx, req)
}

// scriptedSessionController runs one scripted traversal step on the
// session's producer goroutine, so the step sees a real input-permission
// handle.
type scriptedSessionController struct {
	run    *inventoryRun
	action func(context.Context, *inventoryRun) error
	errCh  chan error
}

func (c *scriptedSessionController) Prepare(context.Context, *scan.ControllerHandle) error {
	return nil
}

func (c *scriptedSessionController) Collect(ctx context.Context, h *scan.ControllerHandle) error {
	c.run.h = h
	err := c.action(ctx, c.run)
	c.errCh <- err
	return err
}

func runScriptedSession(t *testing.T, run *inventoryRun, action func(context.Context, *inventoryRun) error) error {
	return runScriptedSessionTimeout(t, run, action, 5*time.Second)
}

// runScriptedSessionTimeout is runScriptedSession with a configurable
// outer bound, for scripted steps that legitimately run a full scroll
// budget (the field-failure replay's legacy ablation).
func runScriptedSessionTimeout(t *testing.T, run *inventoryRun, action func(context.Context, *inventoryRun) error, bound time.Duration) error {
	t.Helper()
	ctrl := &scriptedSessionController{run: run, action: action, errCh: make(chan error, 1)}
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
		case <-time.After(bound):
			t.Fatal("the scripted traversal session never sealed")
		}
		return err
	case <-time.After(bound):
		t.Fatal("the scripted traversal step never returned")
		return nil
	}
}

func runScriptedScroll(t *testing.T, run *inventoryRun) error {
	t.Helper()
	return runScriptedSession(t, run, func(ctx context.Context, run *inventoryRun) error {
		return run.scroll(ctx)
	})
}

func newScriptedScrollRun(l Layout, view SessionView, m Matcher, dir string, retries int, budget time.Duration) *inventoryRun {
	return &inventoryRun{
		inv: &Inventory{
			Layout:        l,
			Matcher:       m,
			DiagnosticDir: dir,
			MaxRetries:    retries,
			ScrollBudget:  budget,
			ScrollTiming:  scriptedScrollTiming(),
		},
		view:     view,
		geo:      1,
		position: 1,
		viewport: 3,
	}
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
	dumps := diagnosticDumpDirs(t, dir)
	if len(dumps) != 1 {
		t.Fatalf("the stopped scroll wrote %v, want one dump", dumps)
	}
	if !strings.Contains(filepath.Base(dumps[0]), "-scroll-stationary") {
		t.Fatalf("dump %q does not name the stopping verdict", dumps[0])
	}
	files := dumpPNGs(t, dumps[0])
	if len(files) != 2 {
		t.Fatalf("the stopped scroll wrote %d crops, want the before and received frame", len(files))
	}
	if ids := manifestFrameIDs(readDumpManifest(t, dumps[0])); len(ids) != 2 {
		t.Fatalf("manifest frames %v, want the before frame and the received frame", ids)
	}
}

// TestScrollRetryAfterVerdictDoesNotDumpPreviousAttempt pins the reset
// between attempts: the first try ends Stationary and retries from a fresh
// verified frame, the second stops before producing a verdict, and no frame
// from the first try may be reported as this failure's evidence.
func TestScrollRetryAfterVerdictDoesNotDumpPreviousAttempt(t *testing.T) {
	l := sixRowScrollLayout()
	frame := func(id uint64) platform.Frame {
		return platform.Frame{ID: id, Geometry: 1, Image: image.NewRGBA(image.Rect(0, 0, 1280, 1000))}
	}
	view := &failingObserveView{
		scrollScriptView: &scrollScriptView{frames: []platform.Frame{frame(1), frame(2), frame(3)}},
		failAt:           2,
		blockAt:          3,
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
	if dumps := diagnosticDumpDirs(t, dir); len(dumps) != 0 {
		t.Fatalf("a failure before the verdict wrote %v, want no dumps", dumps)
	}
}

// TestConfirmBottomFailureWritesDiagnostics drives the real bottom
// confirmation to a displaced failure through confirmBottomStep, the timed
// caller that writes the sequence after bottom_ms is accumulated. A
// controlled clock pins the order: the write's stamp is the third reading,
// so moving the write back inside the timed span would report its full
// second in bottom_ms. The write must not change the fault.
func TestConfirmBottomFailureWritesDiagnostics(t *testing.T) {
	l := sixRowScrollLayout()
	frame := func(id uint64) platform.Frame {
		return platform.Frame{ID: id, Geometry: 1, Image: image.NewRGBA(image.Rect(0, 0, 1280, 1000))}
	}
	view := &scrollScriptView{frames: []platform.Frame{frame(2), frame(3), frame(4)}}
	m := &scrollScriptMatcher{rows: l.Rows, columns: l.Columns, verdict: map[scrollPair]ScrollMatch{
		{1, 2}: displacedScan(l, 4),
		{2, 3}: {Stationary: true, Evidence: "scripted settled"},
		{2, 4}: {Stationary: true, Evidence: "scripted settled"},
	}}
	dir := t.TempDir()
	run := newScriptedScrollRun(l, view, m, dir, 0, time.Second)
	run.lastFrame = frame(1)

	// Start, elapsed, then the dump's file stamp. If the dump is timed,
	// the elapsed reading becomes the third time (one second later).
	base := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	clockTimes := []time.Time{base, base.Add(time.Millisecond), base.Add(time.Second)}
	clockCalls := 0
	previousNow := diagnosticNow
	diagnosticNow = func() time.Time {
		i := clockCalls
		clockCalls++
		if i >= len(clockTimes) {
			return clockTimes[len(clockTimes)-1]
		}
		return clockTimes[i]
	}
	t.Cleanup(func() { diagnosticNow = previousNow })

	observedBottom := time.Duration(-1)
	calls := slowDiagnosticWrites(t, 0, func() { observedBottom = run.positionBottomMs })
	err := runScriptedSession(t, run, func(ctx context.Context, run *inventoryRun) error {
		return run.confirmBottomStep(ctx)
	})
	if err == nil {
		t.Fatal("a displaced bottom confirmation must stop the walk")
	}
	if *calls == 0 {
		t.Fatal("the displaced bottom confirmation wrote no diagnostic artifact")
	}
	if observedBottom != time.Millisecond {
		t.Fatalf("the first write observed bottom_ms = %s, want the already-recorded 1ms decision span", observedBottom)
	}
	if clockCalls != 3 {
		t.Fatalf("the diagnostic clock was read %d times, want the start, elapsed and one dump stamp", clockCalls)
	}
	if run.positionBottomMs != time.Millisecond {
		t.Fatalf("bottom_ms = %s, want the 1ms decision span; a timed dump would report 1s", run.positionBottomMs)
	}
	dumps := diagnosticDumpDirs(t, dir)
	if len(dumps) != 1 {
		t.Fatalf("the displaced bottom confirmation wrote %v, want one dump", dumps)
	}
	if !strings.Contains(filepath.Base(dumps[0]), "-bottom-displaced") {
		t.Fatalf("dump %q does not name the bottom-confirmation verdict", dumps[0])
	}
	if files := dumpPNGs(t, dumps[0]); len(files) != 4 {
		t.Fatalf("the displaced bottom confirmation wrote %d crops, want the before and the three received frames", len(files))
	}
}

// TestScrollSuccessWritesTimingDiagnostics pins the stage 2 success side: a
// committed scroll lands its frame time series (decision §4.3 — precise
// frame times come from the application diagnostics), with the manifest
// carrying every retained frame's verdicts and only the key frames' PNG
// crops, and the settle streak's confirmation recorded for the timing
// calibration.
func TestScrollSuccessWritesTimingDiagnostics(t *testing.T) {
	l := sixRowScrollLayout()
	frame := func(id uint64) platform.Frame {
		return platform.Frame{ID: id, Geometry: 1, Image: image.NewRGBA(image.Rect(0, 0, 1280, 1000))}
	}
	view := &scrollScriptView{frames: []platform.Frame{frame(1), frame(2), frame(3), frame(4)}}
	m := &scrollScriptMatcher{rows: l.Rows, columns: l.Columns, verdict: map[scrollPair]ScrollMatch{
		{1, 2}: displacedScan(l, 4),
		{2, 3}: {Stationary: true, Evidence: "scripted settled"},
		{2, 4}: {Stationary: true, Evidence: "scripted settled"},
	}}
	dir := t.TempDir()
	run := newScriptedScrollRun(l, view, m, dir, 0, time.Second)
	run.visited = make([]bool, len(l.Slots))

	if err := runScriptedScroll(t, run); err != nil {
		t.Fatalf("the settled scroll must succeed: %v", err)
	}
	dumps := diagnosticDumpDirs(t, dir)
	if len(dumps) != 1 {
		t.Fatalf("a successful scroll wrote %v, want one scroll-ok dump", dumps)
	}
	if !strings.Contains(filepath.Base(dumps[0]), "-scroll-ok") {
		t.Fatalf("dump %q does not name the committed scroll", dumps[0])
	}
	manifest := readDumpManifest(t, dumps[0])
	if ids := manifestFrameIDs(manifest); fmt.Sprint(ids) != "[1 2 3 4]" {
		t.Fatalf("manifest frames %v, want the whole received sequence", ids)
	}
	if manifest.SettledFrame != 4 || manifest.SettleNeeded != run.inv.scrollTiming().SettleObservations {
		t.Fatalf("manifest settled frame %d of %d, want frame 4 closing the streak", manifest.SettledFrame, manifest.SettleNeeded)
	}
	// Key frames only: the baseline, the candidate and the settle
	// confirmation (the first moved frame is the candidate here) write
	// crops; the streak's intermediate frame stays in the manifest.
	if files := dumpPNGs(t, dumps[0]); len(files) != 3 {
		t.Fatalf("the committed scroll wrote %d crops, want the key frames only", len(files))
	}
}

// TestScrollUnsettledWritesSequenceManifest drives the real scroll path to
// the failed settle the field reports: the whole received sequence is
// written, every entry carries the matcher result the wait recorded, and
// the candidate-relative comparison names the candidate it was measured
// against.
func TestScrollUnsettledWritesSequenceManifest(t *testing.T) {
	l := sixRowScrollLayout()
	base := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	frame := func(id uint64) platform.Frame {
		return platform.Frame{ID: id, Geometry: 1, ArrivedAt: base.Add(time.Duration(id) * 40 * time.Millisecond),
			Image: image.NewRGBA(image.Rect(0, 0, 1280, 1000))}
	}
	metrics := func(err0, bestErr float64) *ScrollMetrics {
		return &ScrollMetrics{Err0: err0, Measured: true, BestDy: 147, BestErr: bestErr,
			RowErrors: map[int]float64{1: bestErr, 2: bestErr + 1}}
	}
	verdicts := map[scrollPair]ScrollMatch{
		{1, 2}: {Displaced: true, Rows: 4, Matches: scriptedDisplacement(l, 4), Evidence: "scripted four rows", Metrics: metrics(6.0, 3.0)},
		{2, 3}: {Evidence: "scripted still moving", Metrics: &ScrollMetrics{Err0: 2.5}},
		{1, 3}: {Displaced: true, Rows: 5, Matches: scriptedDisplacement(l, 5), Evidence: "scripted five rows", Metrics: metrics(7.0, 4.0)},
		{3, 4}: {Evidence: "scripted still moving", Metrics: &ScrollMetrics{Err0: 2.75}},
		{1, 4}: {Displaced: true, Rows: 4, Matches: scriptedDisplacement(l, 4), Evidence: "scripted back to four rows", Metrics: metrics(8.0, 5.0)},
	}
	view := &scrollScriptView{frames: []platform.Frame{frame(1), frame(2), frame(3), frame(4)}, blockAfter: true}
	m := &scrollScriptMatcher{rows: l.Rows, columns: l.Columns, verdict: verdicts}
	dir := t.TempDir()
	run := newScriptedScrollRun(l, view, m, dir, 0, 40*time.Millisecond)

	err := runScriptedScroll(t, run)
	if err == nil || !strings.Contains(err.Error(), "did not settle") {
		t.Fatalf("scroll error %v, want the unsettled displacement", err)
	}
	dumps := diagnosticDumpDirs(t, dir)
	if len(dumps) != 1 || !strings.Contains(filepath.Base(dumps[0]), "-scroll-lost") {
		t.Fatalf("the unsettled scroll wrote %v, want one scroll-lost dump", dumps)
	}
	manifest := readDumpManifest(t, dumps[0])
	if ids := manifestFrameIDs(manifest); fmt.Sprint(ids) != "[1 2 3 4]" {
		t.Fatalf("manifest frames %v, want the complete received sequence", ids)
	}
	if manifest.BeforeFrame != 1 || manifest.AfterFrame != 4 || manifest.CandidateFrame != 4 {
		t.Fatalf("manifest frames before=%d after=%d candidate=%d, want 1/4/4",
			manifest.BeforeFrame, manifest.AfterFrame, manifest.CandidateFrame)
	}
	for i, pair := range []scrollPair{{1, 2}, {1, 3}, {1, 4}} {
		entry := manifest.Frames[i+1]
		want := verdicts[pair]
		if entry.VsBefore == nil || entry.VsBefore.ReferenceFrame != pair.before ||
			entry.VsBefore.Verdict != scrollVerdict(want) {
			t.Fatalf("frame %d vs_before %+v, want the matcher's %+v", entry.Frame, entry.VsBefore, want)
		}
		if entry.VsBefore.Err0 == nil || *entry.VsBefore.Err0 != want.Metrics.Err0 ||
			entry.VsBefore.BestDy == nil || *entry.VsBefore.BestDy != want.Metrics.BestDy ||
			entry.VsBefore.BestErr == nil || *entry.VsBefore.BestErr != want.Metrics.BestErr {
			t.Fatalf("frame %d vs_before numbers %+v, want the matcher's %+v", entry.Frame, entry.VsBefore, want.Metrics)
		}
	}
	for i, pair := range []scrollPair{{2, 3}, {3, 4}} {
		entry := manifest.Frames[i+2]
		want := verdicts[pair]
		if entry.VsCandidate == nil || entry.VsCandidate.ReferenceFrame != pair.before ||
			entry.VsCandidate.Err0 == nil || *entry.VsCandidate.Err0 != want.Metrics.Err0 {
			t.Fatalf("frame %d vs_candidate %+v, want the matcher's %+v against frame %d",
				entry.Frame, entry.VsCandidate, want, pair.before)
		}
	}
	if entry := manifest.Frames[1]; entry.VsCandidate != nil {
		t.Fatalf("frame 2 vs_candidate %+v, want none before a candidate existed", entry.VsCandidate)
	}
}

// slowDiagnosticWrites delays the first artifact write and observes the
// timer state at that moment, so a test can prove the write happens after
// the decision timer was recorded.
func slowDiagnosticWrites(t *testing.T, delay time.Duration, observe func()) *int {
	t.Helper()
	previous := writeDiagnosticFile
	calls := 0
	writeDiagnosticFile = func(path string, data []byte) error {
		calls++
		if calls == 1 {
			if observe != nil {
				observe()
			}
			time.Sleep(delay)
		}
		return previous(path, data)
	}
	t.Cleanup(func() { writeDiagnosticFile = previous })
	return &calls
}

// TestScrollDumpStaysOutsideScrollTimer pins the timing contract: the
// failure dump's encode and write cost is not part of scroll_ms.
func TestScrollDumpStaysOutsideScrollTimer(t *testing.T) {
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
	observed := time.Duration(-1)
	calls := slowDiagnosticWrites(t, 500*time.Millisecond, func() { observed = run.positionScrollMs })

	if err := runScriptedScroll(t, run); err == nil {
		t.Fatal("a Stationary limit must stop the scroll")
	}
	if *calls == 0 {
		t.Fatal("the failure wrote no diagnostic artifact")
	}
	if observed <= 0 {
		t.Fatalf("the first write began before scroll_ms was recorded (observed %s)", observed)
	}
	if run.positionScrollMs >= 300*time.Millisecond {
		t.Fatalf("scroll_ms = %s, want the decision cost without the 500ms diagnostic write", run.positionScrollMs)
	}
}

// scriptedBlockView serves the scripted frames in order and blocks chosen
// Frame calls until their request deadline. It lets a test end one wait on
// the deadline between two frames, as a live stream without new content
// does.
type scriptedBlockView struct {
	frames         []platform.Frame
	blockAt        []int
	calls          int
	dispatches     int
	dispatchFrames []uint64
}

func (v *scriptedBlockView) Observe() (platform.Observation, error) {
	return platform.Observation{Ready: true, Geometry: 1}, nil
}

func (v *scriptedBlockView) Dispatch(op platform.InputOp) platform.InputResult {
	v.dispatches++
	v.dispatchFrames = append(v.dispatchFrames, op.FrameID)
	return platform.InputResult{Status: platform.InputPosted}
}

func (v *scriptedBlockView) Frame(ctx context.Context, req platform.FrameRequest) (platform.Frame, error) {
	v.calls++
	for _, n := range v.blockAt {
		if v.calls == n {
			<-ctx.Done()
			return platform.Frame{}, ctx.Err()
		}
	}
	if len(v.frames) == 0 {
		return platform.Frame{}, context.DeadlineExceeded
	}
	f := v.frames[0]
	v.frames = v.frames[1:]
	return f, nil
}

// TestAwaitScrollLastUndecidedSupersedesStationary pins the deadline
// aggregation: a Stationary observation followed by an Undecided one ends
// undecided, not stationary, and does not prove the drag was unmoved.
func TestAwaitScrollLastUndecidedSupersedesStationary(t *testing.T) {
	l := sixRowScrollLayout()
	view := &scrollScriptView{frames: []platform.Frame{scrollFrame(2), scrollFrame(3)}, blockAfter: true}
	m := &scrollScriptMatcher{rows: l.Rows, columns: l.Columns, verdict: map[scrollPair]ScrollMatch{
		{1, 2}: {Stationary: true, Evidence: "scripted stationary"},
		{1, 3}: {Undecided: true, Evidence: "scripted undecided"},
	}}
	run := newScrollRun(l, view, m, 40*time.Millisecond)
	out, err := run.awaitScroll(context.Background(), scrollFrame(1))
	if err != nil {
		t.Fatalf("awaitScroll: %v", err)
	}
	if out.stationary || out.displaced || out.lost {
		t.Fatalf("outcome %+v, want the last Undecided verdict", out)
	}
	if out.unmoved {
		t.Fatal("an Undecided observation must not prove the drag was unmoved")
	}
	if !out.match.Undecided {
		t.Fatalf("outcome match %+v, want the last comparison", out.match)
	}
}

// TestAwaitScrollLastUndecidedSupersedesLost pins the field failure:
// a Lost observation followed by Undecided frames must end undecided, not
// carry the earlier Lost to the deadline.
func TestAwaitScrollLastUndecidedSupersedesLost(t *testing.T) {
	l := sixRowScrollLayout()
	view := &scrollScriptView{frames: []platform.Frame{scrollFrame(2), scrollFrame(3)}, blockAfter: true}
	m := &scrollScriptMatcher{rows: l.Rows, columns: l.Columns, verdict: map[scrollPair]ScrollMatch{
		{1, 2}: {Lost: true, Evidence: "scripted lost"},
		{1, 3}: {Undecided: true, Evidence: "scripted undecided"},
	}}
	run := newScrollRun(l, view, m, 40*time.Millisecond)
	out, err := run.awaitScroll(context.Background(), scrollFrame(1))
	if err != nil {
		t.Fatalf("awaitScroll: %v", err)
	}
	if out.stationary || out.displaced || out.lost {
		t.Fatalf("outcome %+v, want the last Undecided verdict", out)
	}
	if out.unmoved {
		t.Fatalf("an earlier Lost must not be reported as unmoved: %+v", out)
	}
	if !out.match.Undecided {
		t.Fatalf("outcome match %+v, want the last comparison", out.match)
	}
}

// TestScrollStationaryAfterUndecidedDoesNotDragAgain pins the other
// unknown-effect case: the last verdict is Stationary, but an earlier
// Undecided observation means the wait never proved the drag was unmoved,
// so no retry is sent.
func TestScrollStationaryAfterUndecidedDoesNotDragAgain(t *testing.T) {
	l := sixRowScrollLayout()
	view := &scrollScriptView{frames: []platform.Frame{scrollFrame(1), scrollFrame(2), scrollFrame(3)}, blockAfter: true}
	m := &scrollScriptMatcher{rows: l.Rows, columns: l.Columns, verdict: map[scrollPair]ScrollMatch{
		{1, 2}: {Undecided: true, Evidence: "scripted undecided"},
		{1, 3}: {Stationary: true, Evidence: "scripted stationary"},
	}}
	run := newScriptedScrollRun(l, view, m, "", 2, 40*time.Millisecond)
	run.visited = make([]bool, len(l.Slots))

	err := runScriptedScroll(t, run)
	var fault *scan.FaultError
	if !errors.As(err, &fault) || fault.Code != CodeNavigationMismatch {
		t.Fatalf("scroll error %v, want navigationMismatch", err)
	}
	if view.dispatches != 1 {
		t.Fatalf("the not-provably-unmoved drag was followed by %d drags, want only the first", view.dispatches)
	}
}

// TestAwaitScrollCandidateNeedsSettleAfterUndecided pins the unchanged
// candidate rule: a displacement candidate followed by an Undecided total
// still ends as an unsettled Lost, because only a candidate-relative
// Stationary confirmation may commit it.
func TestAwaitScrollCandidateNeedsSettleAfterUndecided(t *testing.T) {
	l := sixRowScrollLayout()
	view := &scrollScriptView{frames: []platform.Frame{scrollFrame(2), scrollFrame(3)}, blockAfter: true}
	m := &scrollScriptMatcher{rows: l.Rows, columns: l.Columns, verdict: map[scrollPair]ScrollMatch{
		{1, 2}: displacedScan(l, 4),
		{1, 3}: {Undecided: true, Evidence: "scripted undecided"},
	}}
	run := newScrollRun(l, view, m, 40*time.Millisecond)
	out, err := run.awaitScroll(context.Background(), scrollFrame(1))
	if err != nil {
		t.Fatalf("awaitScroll: %v", err)
	}
	if !out.lost || !out.unsettled || out.displaced || out.stationary {
		t.Fatalf("outcome %+v, want the unsettled candidate outcome", out)
	}
	if !out.hasCandidate || out.after.ID != 2 || out.match.Rows != 4 {
		t.Fatalf("outcome %+v, want the untouched four-row candidate at frame 2", out)
	}
	if out.unmoved {
		t.Fatal("a displacement candidate must not be reported as unmoved")
	}
}

// TestScrollUnknownOutcomeDoesNotDragAgain pins the conservative stop: when
// the drag's effect stays unknown, no further drag is sent even though the
// retry budget is not exhausted.
func TestScrollUnknownOutcomeDoesNotDragAgain(t *testing.T) {
	l := sixRowScrollLayout()
	view := &scrollScriptView{frames: []platform.Frame{scrollFrame(1), scrollFrame(2), scrollFrame(3)}, blockAfter: true}
	m := &scrollScriptMatcher{rows: l.Rows, columns: l.Columns, verdict: map[scrollPair]ScrollMatch{
		{1, 2}: {Undecided: true, Evidence: "scripted undecided"},
		{1, 3}: {Undecided: true, Evidence: "scripted undecided"},
	}}
	run := newScriptedScrollRun(l, view, m, "", 2, 40*time.Millisecond)
	run.visited = make([]bool, len(l.Slots))

	err := runScriptedScroll(t, run)
	var fault *scan.FaultError
	if !errors.As(err, &fault) || fault.Code != CodeNavigationMismatch {
		t.Fatalf("scroll error %v, want navigationMismatch", err)
	}
	if view.dispatches != 1 {
		t.Fatalf("the unknown drag was followed by %d drags, want only the first", view.dispatches)
	}
}

// TestScrollRetryUnmovedUsesFreshBase pins the retry base: after a wait
// that only ever proved stillness, the retry must compare against the
// newest verified frame. The second attempt only settles against frames 4
// and 5, so a retry that kept the first attempt's baseline could not
// commit.
func TestScrollRetryUnmovedUsesFreshBase(t *testing.T) {
	l := sixRowScrollLayout()
	view := &scriptedBlockView{
		frames:  []platform.Frame{scrollFrame(1), scrollFrame(2), scrollFrame(3), scrollFrame(4), scrollFrame(5), scrollFrame(6)},
		blockAt: []int{3},
	}
	m := &scrollScriptMatcher{rows: l.Rows, columns: l.Columns, verdict: map[scrollPair]ScrollMatch{
		{1, 2}: {Stationary: true, Evidence: "scripted stationary"},
		{3, 4}: displacedScan(l, 4),
		{4, 5}: {Stationary: true, Evidence: "scripted settled"},
		{4, 6}: {Stationary: true, Evidence: "scripted settled"},
	}}
	run := newScriptedScrollRun(l, view, m, "", 1, 40*time.Millisecond)
	run.visited = make([]bool, len(l.Slots))

	if err := runScriptedScroll(t, run); err != nil {
		t.Fatalf("the unmoved retry must commit the fresh-base scroll: %v", err)
	}
	if view.dispatches != 2 {
		t.Fatalf("scroll dispatched %d drags, want the first attempt and one retry", view.dispatches)
	}
	if run.viewport != 4 || run.lastScrollRows != 4 {
		t.Fatalf("retry committed viewport %d rows %d, want the fresh-base four-row scroll", run.viewport, run.lastScrollRows)
	}
}

// TestScrollFailureKeepsEveryAttemptDiagnostics pins per-attempt evidence:
// a scroll that exhausts its retries writes one dump per attempt, each
// with its own before frame and attempt number, and the log distinguishes
// the attempts.
func TestScrollFailureKeepsEveryAttemptDiagnostics(t *testing.T) {
	l := sixRowScrollLayout()
	view := &scriptedBlockView{
		frames:  []platform.Frame{scrollFrame(1), scrollFrame(2), scrollFrame(3), scrollFrame(4)},
		blockAt: []int{3, 6},
	}
	m := &scrollScriptMatcher{rows: l.Rows, columns: l.Columns, verdict: map[scrollPair]ScrollMatch{
		{1, 2}: {Stationary: true, Evidence: "scripted stationary"},
		{3, 4}: {Stationary: true, Evidence: "scripted stationary"},
	}}
	dir := t.TempDir()
	run := newScriptedScrollRun(l, view, m, dir, 1, 40*time.Millisecond)
	run.visited = make([]bool, len(l.Slots))

	logs := &bytes.Buffer{}
	previous := log.Writer()
	log.SetOutput(logs)
	t.Cleanup(func() { log.SetOutput(previous) })

	err := runScriptedScroll(t, run)
	if err == nil || !strings.Contains(err.Error(), "never displaced") {
		t.Fatalf("scroll error %v, want the exhausted-stationary stop", err)
	}
	if view.dispatches != 2 {
		t.Fatalf("scroll dispatched %d drags, want two attempts", view.dispatches)
	}
	dumps := diagnosticDumpDirs(t, dir)
	if len(dumps) != 2 {
		t.Fatalf("the failed scroll wrote %d dumps, want one per attempt: %v", len(dumps), dumps)
	}
	byAttempt := make(map[int]diagnosticManifest, len(dumps))
	for _, dump := range dumps {
		manifest := readDumpManifest(t, dump)
		byAttempt[manifest.Attempt] = manifest
		if !strings.Contains(filepath.Base(dump), fmt.Sprintf("-attempt%d", manifest.Attempt)) {
			t.Fatalf("dump %q does not name its attempt %d", dump, manifest.Attempt)
		}
	}
	for _, want := range []struct {
		attempt       int
		before, after uint64
	}{
		{1, 1, 2},
		{2, 3, 4},
	} {
		manifest, ok := byAttempt[want.attempt]
		if !ok {
			t.Fatalf("no dump for attempt %d: %v", want.attempt, dumps)
		}
		if manifest.BeforeFrame != want.before || manifest.AfterFrame != want.after {
			t.Fatalf("attempt %d frames before=%d after=%d, want %d/%d",
				want.attempt, manifest.BeforeFrame, manifest.AfterFrame, want.before, want.after)
		}
	}
	for _, want := range []string{"attempt=1", "attempt=2", "attempts=2"} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("scroll log lacks %q: %s", want, logs.String())
		}
	}
}

// TestConfirmBottomUnknownOutcomeDoesNotDragAgain pins the same rule for
// the bottom confirmation: an undecided wait stops instead of retrying.
func TestConfirmBottomUnknownOutcomeDoesNotDragAgain(t *testing.T) {
	l := sixRowScrollLayout()
	view := &scrollScriptView{frames: []platform.Frame{scrollFrame(2), scrollFrame(3)}, blockAfter: true}
	m := &scrollScriptMatcher{rows: l.Rows, columns: l.Columns, verdict: map[scrollPair]ScrollMatch{
		{1, 2}: {Undecided: true, Evidence: "scripted undecided"},
		{1, 3}: {Undecided: true, Evidence: "scripted undecided"},
	}}
	run := newScriptedScrollRun(l, view, m, "", 2, 40*time.Millisecond)
	run.lastFrame = scrollFrame(1)

	err := runScriptedSession(t, run, func(ctx context.Context, run *inventoryRun) error {
		_, err := run.confirmBottom(ctx)
		return err
	})
	var fault *scan.FaultError
	if !errors.As(err, &fault) || fault.Code != CodeNavigationMismatch {
		t.Fatalf("bottom confirmation error %v, want navigationMismatch", err)
	}
	if view.dispatches != 1 {
		t.Fatalf("the undecided bottom confirmation sent %d drags, want one", view.dispatches)
	}
}

// TestScrollToTopUndecidedNonListStops pins the precondition of the
// top-fling exception: an undecided fling only continues while its latest
// observation is still the verifiable warehouse list. A popup or loading
// page after the list is a page-level failure, as in the Lost branch.
func TestScrollToTopUndecidedNonListStops(t *testing.T) {
	l := sixRowScrollLayout()
	view := &scrollScriptView{frames: []platform.Frame{scrollFrame(2), scrollFrame(3)}, blockAfter: true}
	m := &scrollScriptMatcher{rows: l.Rows, columns: l.Columns,
		verdict: map[scrollPair]ScrollMatch{
			{1, 2}: {Undecided: true, Evidence: "scripted undecided"},
		},
		nonListFrames: map[uint64]bool{3: true},
	}
	run := newScriptedScrollRun(l, view, m, "", 0, 40*time.Millisecond)
	run.lastFrame = scrollFrame(1)
	run.listMatch = m.Match(scrollFrame(1))

	err := runScriptedSession(t, run, func(ctx context.Context, run *inventoryRun) error {
		_, err := run.scrollToTop(ctx)
		return err
	})
	var fault *scan.FaultError
	if !errors.As(err, &fault) || fault.Code != CodeNavigationMismatch {
		t.Fatalf("top fling error %v, want navigationMismatch", err)
	}
	if !strings.Contains(err.Error(), "without a verifiable warehouse list") {
		t.Fatalf("top fling error %v, want the page-level stop", err)
	}
	if view.dispatches != 1 {
		t.Fatalf("the non-list fling sent %d drags, want only the first", view.dispatches)
	}
}

// TestScrollToTopUndecidedWithoutFramesStops pins the other precondition:
// an undecided wait that received no frame at all cannot support another
// fling.
func TestScrollToTopUndecidedWithoutFramesStops(t *testing.T) {
	l := sixRowScrollLayout()
	view := &scrollScriptView{}
	m := &scrollScriptMatcher{rows: l.Rows, columns: l.Columns}
	run := newScriptedScrollRun(l, view, m, "", 0, 40*time.Millisecond)
	run.lastFrame = scrollFrame(1)
	run.listMatch = m.Match(scrollFrame(1))

	err := runScriptedSession(t, run, func(ctx context.Context, run *inventoryRun) error {
		_, err := run.scrollToTop(ctx)
		return err
	})
	var fault *scan.FaultError
	if !errors.As(err, &fault) || fault.Code != CodeNavigationMismatch {
		t.Fatalf("top fling error %v, want navigationMismatch", err)
	}
	if !strings.Contains(err.Error(), "without a verifiable warehouse list") {
		t.Fatalf("top fling error %v, want the page-level stop", err)
	}
	if view.dispatches != 1 {
		t.Fatalf("the frameless fling sent %d drags, want only the first", view.dispatches)
	}
}

// TestScrollRetryBaseNavigationMismatchKeepsAttemptDiagnostics pins the
// retry-base classification: an unreadable current list is a verdict
// failure, so the previous attempt's evidence is still dumped. Operational
// errors at the same point write no dump.
func TestScrollRetryBaseNavigationMismatchKeepsAttemptDiagnostics(t *testing.T) {
	l := sixRowScrollLayout()
	view := &scriptedBlockView{
		frames:  []platform.Frame{scrollFrame(1), scrollFrame(2), scrollFrame(3)},
		blockAt: []int{3},
	}
	m := &scrollScriptMatcher{rows: l.Rows, columns: l.Columns,
		verdict: map[scrollPair]ScrollMatch{
			{1, 2}: {Stationary: true, Evidence: "scripted stationary"},
		},
		nonListFrames: map[uint64]bool{3: true},
	}
	dir := t.TempDir()
	run := newScriptedScrollRun(l, view, m, dir, 1, 40*time.Millisecond)
	run.visited = make([]bool, len(l.Slots))

	err := runScriptedScroll(t, run)
	var fault *scan.FaultError
	if !errors.As(err, &fault) || fault.Code != CodeNavigationMismatch {
		t.Fatalf("scroll error %v, want navigationMismatch", err)
	}
	if view.dispatches != 1 {
		t.Fatalf("scroll dispatched %d drags, want only the first attempt", view.dispatches)
	}
	dumps := diagnosticDumpDirs(t, dir)
	if len(dumps) != 1 {
		t.Fatalf("the retry-base navigation mismatch wrote %v, want the first attempt's dump", dumps)
	}
	manifest := readDumpManifest(t, dumps[0])
	if manifest.Attempt != 1 || manifest.BeforeFrame != 1 || manifest.AfterFrame != 2 {
		t.Fatalf("dump attempt=%d before=%d after=%d, want attempt 1 frames 1/2",
			manifest.Attempt, manifest.BeforeFrame, manifest.AfterFrame)
	}
}

// TestScrollToTopUndecidedReusedNonListEvidence pins the stop message: a
// reused non-list frame still counts as a received frame and its evidence
// is reported, not "no frame received" or an older list frame's evidence.
func TestScrollToTopUndecidedReusedNonListEvidence(t *testing.T) {
	l := sixRowScrollLayout()
	reused := func(id uint64) platform.Frame {
		return platform.Frame{ID: id, Geometry: 1, Reused: true}
	}
	cases := []struct {
		name    string
		frames  []platform.Frame
		verdict map[scrollPair]ScrollMatch
		nonList map[uint64]bool
	}{
		{
			name:    "only the reused non-list frame",
			frames:  []platform.Frame{reused(2)},
			nonList: map[uint64]bool{2: true},
		},
		{
			name:   "a list frame then the reused non-list frame",
			frames: []platform.Frame{scrollFrame(2), reused(3)},
			verdict: map[scrollPair]ScrollMatch{
				{1, 2}: {Undecided: true, Evidence: "scripted undecided"},
			},
			nonList: map[uint64]bool{3: true},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			view := &scrollScriptView{frames: tc.frames}
			m := &scrollScriptMatcher{rows: l.Rows, columns: l.Columns, verdict: tc.verdict, nonListFrames: tc.nonList}
			run := newScriptedScrollRun(l, view, m, "", 0, 40*time.Millisecond)
			run.lastFrame = scrollFrame(1)
			run.listMatch = m.Match(scrollFrame(1))

			err := runScriptedSession(t, run, func(ctx context.Context, run *inventoryRun) error {
				_, err := run.scrollToTop(ctx)
				return err
			})
			var fault *scan.FaultError
			if !errors.As(err, &fault) || fault.Code != CodeNavigationMismatch {
				t.Fatalf("top fling error %v, want navigationMismatch", err)
			}
			if !strings.Contains(err.Error(), "scripted non-list page") {
				t.Fatalf("top fling error %v, want the last frame's evidence", err)
			}
			if strings.Contains(err.Error(), "no frame received") {
				t.Fatalf("top fling error %v, but frames were received", err)
			}
			if view.dispatches != 1 {
				t.Fatalf("the non-list fling sent %d drags, want only the first", view.dispatches)
			}
		})
	}
}

// TestScrollRetryBaseOperationalErrorWritesNoDiagnostics pins the other
// side of the retry-base classification: a geometry change while reading
// the new base is an operational error, so no dump is written.
func TestScrollRetryBaseOperationalErrorWritesNoDiagnostics(t *testing.T) {
	l := sixRowScrollLayout()
	view := &scriptedBlockView{
		frames: []platform.Frame{
			scrollFrame(1),
			scrollFrame(2),
			{ID: 3, Geometry: 2},
		},
		blockAt: []int{3},
	}
	m := &scrollScriptMatcher{rows: l.Rows, columns: l.Columns, verdict: map[scrollPair]ScrollMatch{
		{1, 2}: {Stationary: true, Evidence: "scripted stationary"},
	}}
	dir := t.TempDir()
	run := newScriptedScrollRun(l, view, m, dir, 1, 40*time.Millisecond)
	run.visited = make([]bool, len(l.Slots))

	err := runScriptedScroll(t, run)
	var fault *scan.FaultError
	if !errors.As(err, &fault) || fault.Code != platform.CodeGeometryChanged {
		t.Fatalf("scroll error %v, want geometryChanged", err)
	}
	if view.dispatches != 1 {
		t.Fatalf("scroll dispatched %d drags, want only the first attempt", view.dispatches)
	}
	if dumps := diagnosticDumpDirs(t, dir); len(dumps) != 0 {
		t.Fatalf("an operational retry-base error wrote %v, want no diagnostics", dumps)
	}
}

// TestScrollToTopUndecidedContinuesFromLatestFrame pins the top-fling
// exception: an undecided fling is an exploratory action toward a hard
// boundary, so the loop sends the next fling from the newest verified
// frame, still stops after two consecutive flings without progress, and
// never exceeds the existing bounds.
func TestScrollToTopUndecidedContinuesFromLatestFrame(t *testing.T) {
	l := sixRowScrollLayout()
	view := &scriptedBlockView{frames: []platform.Frame{scrollFrame(2), scrollFrame(3)}, blockAt: []int{2, 4}}
	m := &scrollScriptMatcher{rows: l.Rows, columns: l.Columns, verdict: map[scrollPair]ScrollMatch{
		{1, 2}: {Undecided: true, Evidence: "scripted undecided"},
	}}
	run := newScriptedScrollRun(l, view, m, "", 0, 40*time.Millisecond)
	run.lastFrame = scrollFrame(1)
	run.listMatch = m.Match(scrollFrame(1))

	err := runScriptedSession(t, run, func(ctx context.Context, run *inventoryRun) error {
		_, err := run.scrollToTop(ctx)
		return err
	})
	var fault *scan.FaultError
	if !errors.As(err, &fault) || fault.Code != CodeNavigationMismatch {
		t.Fatalf("top fling error %v, want navigationMismatch", err)
	}
	if !strings.Contains(err.Error(), "no progress toward the top") {
		t.Fatalf("top fling error %v, want the no-progress bound", err)
	}
	if view.dispatches != 2 {
		t.Fatalf("the undecided fling sent %d drags, want one continuation and the no-progress stop", view.dispatches)
	}
	if got := fmt.Sprint(view.dispatchFrames); got != "[1 2]" {
		t.Fatalf("top flings used before frames %s, want the first frame then the newest verified frame", got)
	}
}

// TestConfirmTopUndecidedDoesNotDragAgain pins the stricter top
// confirmation rule: once the scrollbar claims the top, a wait without a
// verdict stops instead of re-dragging.
func TestConfirmTopUndecidedDoesNotDragAgain(t *testing.T) {
	l := sixRowScrollLayout()
	view := &scrollScriptView{frames: []platform.Frame{scrollFrame(2)}, blockAfter: true}
	m := &scrollScriptMatcher{rows: l.Rows, columns: l.Columns, verdict: map[scrollPair]ScrollMatch{
		{1, 2}: {Undecided: true, Evidence: "scripted undecided"},
	}}
	run := newScriptedScrollRun(l, view, m, "", 0, 40*time.Millisecond)
	run.inv.ActionBudget = 20 * time.Millisecond
	run.lastFrame = scrollFrame(1)
	run.listMatch = m.Match(scrollFrame(1))

	err := runScriptedSession(t, run, func(ctx context.Context, run *inventoryRun) error {
		_, err := run.confirmTop(ctx)
		return err
	})
	var fault *scan.FaultError
	if !errors.As(err, &fault) || fault.Code != CodeNavigationMismatch {
		t.Fatalf("top confirmation error %v, want navigationMismatch", err)
	}
	if view.dispatches != 1 {
		t.Fatalf("the undecided top confirmation sent %d drags, want one", view.dispatches)
	}
}

// TestTopDumpStaysOutsideTopTimer pins the same contract for the
// return-to-top phase: topStep records top_ms before writing the failed
// wait's sequence.
func TestTopDumpStaysOutsideTopTimer(t *testing.T) {
	l := sixRowScrollLayout()
	frame := func(id uint64) platform.Frame {
		return platform.Frame{ID: id, Geometry: 1, Image: image.NewRGBA(image.Rect(0, 0, 1280, 1000))}
	}
	view := &endlessFrameView{}
	m := &scrollScriptMatcher{rows: l.Rows, columns: l.Columns, alwaysStationary: true}
	dir := t.TempDir()
	run := newScriptedScrollRun(l, view, m, dir, 0, 30*time.Millisecond)
	run.lastFrame = frame(1)
	run.listMatch = m.Match(frame(1))

	var timing positionTiming
	observed := time.Duration(-1)
	calls := slowDiagnosticWrites(t, 500*time.Millisecond, func() { observed = timing.top })
	err := runScriptedSession(t, run, func(ctx context.Context, run *inventoryRun) error {
		return run.topStep(ctx, &timing)
	})
	if err == nil {
		t.Fatal("two flings without progress must stop the top phase")
	}
	if *calls == 0 {
		t.Fatal("the failed top phase wrote no diagnostic artifact")
	}
	if observed <= 0 {
		t.Fatalf("the first write began before top_ms was recorded (observed %s)", observed)
	}
	if timing.top >= 300*time.Millisecond {
		t.Fatalf("top_ms = %s, want the decision cost without the 500ms diagnostic write", timing.top)
	}
	dumps := diagnosticDumpDirs(t, dir)
	if len(dumps) != 1 || !strings.Contains(filepath.Base(dumps[0]), "-top-stationary") {
		t.Fatalf("the failed top phase wrote %v, want one top-stationary dump", dumps)
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
