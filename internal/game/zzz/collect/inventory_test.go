package collect_test

import (
	"context"
	"fmt"
	"image"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/game/zzz/collect"
	"github.com/LoTwT/youkai/internal/platform"
	"github.com/LoTwT/youkai/internal/platform/platformfake"
	"github.com/LoTwT/youkai/internal/scan"
)

// The traversal tests (T-series): the Inventory state machine driven
// end to end through the real scan kernel, the collect controller and a
// scripted fake warehouse. The fake serves frames whose marker pixels
// encode the current scene; the pixel matcher decodes them — a honest
// pipeline in which the machine's every verdict comes from frame
// pixels, exactly as it will from 4b's calibrated matcher.

// ---- the scripted warehouse ----

// scriptedGame is a scriptable fake of the ZZZ drive-disc warehouse: a
// grid model (rows×cols slots per viewport, stride positions per scroll
// step, a global occupancy list), a scene state (page, selection) and
// scripting knobs for the failure modes T5's offline acceptance list
// demands. Dispatched clicks and drags take effect after a scriptable
// frame lag; every operation is recorded.
type scriptedGame struct {
	mu sync.Mutex

	// the grid model; rows, cols and stride are immutable after
	// construction.
	rows, cols, stride int
	items              []bool // global occupancy, position order

	// the scene
	viewport int
	page     collect.PageKind
	selected int // slot index within the current viewport; -1 none

	// scripting knobs (see the tests that set them)
	effectLag         int  // frame calls until an action's effect shows
	ignoreClicks      int  // clicks that silently do nothing
	clickOpensSlot    int  // -1: the clicked slot; else this slot's detail
	clickOpensUnknown bool // the click turns the page undecidable
	closeIgnored      bool // the close click does nothing
	scrollStuck       bool // the drag does not move the list
	detailFlicker     bool // the detail's selection alternates, never settling
	geometryAfter     int  // 0: never; else serve geometry 2 from this frame call on
	idleWhenDetail    bool // serve idle (Reused) frames while the detail shows
	abortNextDrag     bool // the next drag returns aborted (the gate closed)
	denyNextDispatch  bool // the next dispatch returns notDispatched

	// records
	ops        []platform.InputOp
	frameCalls int
	pending    []pendingEffect
	lastFrame  platform.Frame
	nextID     uint64
	flickerSel int
}

// pendingEffect is one scripted state change that shows up after dueAt
// frame calls.
type pendingEffect struct {
	dueAt int
	apply func(*scriptedGame)
}

// newScriptedGame builds a warehouse whose occupancy list is items.
func newScriptedGame(items ...bool) *scriptedGame {
	return &scriptedGame{
		rows: 3, cols: 2, stride: 2,
		items:          items,
		page:           collect.PageWarehouse,
		selected:       -1,
		clickOpensSlot: -1,
	}
}

// frame serves one frame: apply the effects that came due, then encode
// the scene in marker pixels — page at (0,10), selection at (1,10),
// viewport at (2,10), top/bottom flags at (3,10), per-slot occupancy at
// (x,8). An effect that just came due always shows fresh; otherwise the
// idleWhenDetail knob may serve the previous frame back as Reused
// (unchanged content is still current content).
func (g *scriptedGame) frame(ctx context.Context, req platform.FrameRequest) (platform.Frame, error) {
	// Like a real platform session, the fake serves frames only while
	// the caller's context lives — the traversal's deadline-bounded
	// waits depend on it.
	if err := ctx.Err(); err != nil {
		return platform.Frame{}, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.frameCalls++
	applied := false
	for len(g.pending) > 0 && g.pending[0].dueAt <= g.frameCalls {
		e := g.pending[0]
		g.pending = g.pending[1:]
		e.apply(g)
		applied = true
	}
	geo := platform.GeometryVersion(1)
	if g.geometryAfter > 0 && g.frameCalls >= g.geometryAfter {
		geo = 2
	}
	if g.detailFlicker && g.page == collect.PageDiscDetail {
		if g.frameCalls%2 == 0 {
			g.flickerSel = g.selected
			g.selected = -1
		} else {
			g.selected = g.flickerSel
		}
	}
	if !applied && g.idleWhenDetail && g.page == collect.PageDiscDetail && g.lastFrame.Image != nil {
		f := g.lastFrame
		f.Reused = true
		return f, nil
	}
	img := image.NewRGBA(image.Rect(0, 0, 40, 12))
	set := func(x, y int, v uint8) {
		pos := img.PixOffset(x, y)
		img.Pix[pos] = v
		img.Pix[pos+3] = 255
	}
	code := uint8(0)
	if g.page == collect.PageWarehouse {
		code = 1
	} else if g.page == collect.PageDiscDetail {
		code = 2
	}
	set(0, 10, code)
	set(1, 10, uint8(g.selected+1))
	set(2, 10, uint8(g.viewport+1))
	var flags uint8
	if g.viewport == 0 {
		flags |= 1
	}
	if g.viewport*g.stride+g.rows*g.cols >= len(g.items) {
		flags |= 2
	}
	set(3, 10, flags)
	for i := 0; i < g.rows*g.cols; i++ {
		v := uint8(0)
		if g.occupiedSlot(i) {
			v = 1
		}
		set(i, 8, v)
	}
	g.nextID++
	f := platform.Frame{
		ID:        g.nextID,
		Geometry:  geo,
		Image:     img,
		Content:   image.Rect(0, 0, 40, 12),
		ArrivedAt: time.Now(),
	}
	g.lastFrame = f
	return f, nil
}

// observe serves the platform observation: ready under the current
// geometry.
func (g *scriptedGame) observe() (platform.Observation, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	geo := platform.GeometryVersion(1)
	if g.geometryAfter > 0 && g.frameCalls >= g.geometryAfter {
		geo = 2
	}
	return platform.Observation{At: time.Now(), Ready: true, Geometry: geo}, nil
}

// dispatch applies one input operation to the script and returns its
// result. Callers hold no lock.
func (g *scriptedGame) dispatch(op platform.InputOp) platform.InputResult {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.ops = append(g.ops, op)
	now := time.Now()
	if g.abortNextDrag && op.Kind == platform.InputDrag {
		g.abortNextDrag = false
		return platform.InputResult{Status: platform.InputPartial, Aborted: true,
			EventsPlanned: 12, EventsPosted: 5, StartedAt: now, FinishedAt: now}
	}
	if g.denyNextDispatch {
		g.denyNextDispatch = false
		return platform.InputResult{Status: platform.InputNotDispatched,
			Denial: platform.NewFault(platform.CodeFocusLost, "focus lost in the script")}
	}
	lag := g.frameCalls + g.effectLag
	switch op.Kind {
	case platform.InputClick:
		if g.page == collect.PageDiscDetail && op.At.In(closeAnchor(g.rows, g.cols)) {
			if !g.closeIgnored {
				g.pending = append(g.pending, pendingEffect{dueAt: lag, apply: func(g *scriptedGame) {
					g.page = collect.PageWarehouse
					g.selected = -1
				}})
			}
		} else if slot, ok := slotAt(g.rows, g.cols, op.At); ok {
			g.clickSlot(slot, lag)
		}
	case platform.InputDrag:
		if op.At.Eq(scrollFrom(g.rows, g.cols)) && op.To.Eq(scrollTo(g.rows, g.cols)) && !g.scrollStuck {
			g.pending = append(g.pending, pendingEffect{dueAt: lag, apply: func(g *scriptedGame) {
				g.viewport++
			}})
		}
	}
	return platform.InputResult{Status: platform.InputPosted, EventsPlanned: 2, EventsPosted: 2,
		StartedAt: now, FinishedAt: now}
}

// clickSlot scripts one slot click per the knobs.
func (g *scriptedGame) clickSlot(slot, lag int) {
	if g.ignoreClicks > 0 {
		g.ignoreClicks--
		return
	}
	if g.clickOpensUnknown {
		g.pending = append(g.pending, pendingEffect{dueAt: lag, apply: func(g *scriptedGame) {
			g.page = collect.PageUnknown
		}})
		return
	}
	target := slot
	if g.clickOpensSlot >= 0 {
		target = g.clickOpensSlot
	}
	g.pending = append(g.pending, pendingEffect{dueAt: lag, apply: func(g *scriptedGame) {
		g.page = collect.PageDiscDetail
		g.selected = target
	}})
}

// occupiedSlot reports whether the current viewport's slot i holds an
// item. Callers hold the lock.
func (g *scriptedGame) occupiedSlot(i int) bool {
	p := g.viewport*g.stride + i
	return p >= 0 && p < len(g.items) && g.items[p]
}

// opsSnapshot returns the recorded operations.
func (g *scriptedGame) opsSnapshot() []platform.InputOp {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]platform.InputOp(nil), g.ops...)
}

// layout is the Inventory's anchor set for this grid. It reads only the
// immutable grid shape, so it needs no lock.
func (g *scriptedGame) layout() collect.Layout {
	return gridLayout(g.rows, g.cols)
}

// gridLayout builds the shared anchor geometry: 6×3 slot cells in a
// 40×12 frame, the close control top right, one drag from the bottom
// rail upwards.
func gridLayout(rows, cols int) collect.Layout {
	slots := make([]image.Rectangle, 0, rows*cols)
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			slots = append(slots, image.Rect(c*6, r*3, c*6+6, r*3+3))
		}
	}
	return collect.Layout{
		Rows:       rows,
		Columns:    cols,
		Slots:      slots,
		Close:      closeAnchor(rows, cols),
		ScrollFrom: scrollFrom(rows, cols),
		ScrollTo:   scrollTo(rows, cols),
	}
}

func closeAnchor(rows, cols int) image.Rectangle {
	return image.Rect(cols*6, 0, cols*6+6, 3)
}

func scrollFrom(rows, cols int) image.Point {
	return image.Pt(cols*6+4, rows*3+1)
}

func scrollTo(rows, cols int) image.Point {
	return image.Pt(cols*6+4, 2)
}

// slotAt maps a click point to its slot index.
func slotAt(rows, cols int, p image.Point) (int, bool) {
	for i, r := range gridLayout(rows, cols).Slots {
		if p.In(r) {
			return i, true
		}
	}
	return 0, false
}

// ---- the pixel matcher ----

// pixelMatcher decodes the scripted game's marker pixels — the test
// stand-in for 4b's calibrated matcher, consuming real frames.
type pixelMatcher struct {
	rows, cols, stride int

	// scripting knobs
	scrollAmbiguous bool // displacement without correspondences
	scrollRagged    bool // correspondences that miss one overlap position
}

func (m *pixelMatcher) slots() int { return m.rows * m.cols }

// Match implements collect.Matcher: decode the page markers.
func (m *pixelMatcher) Match(f platform.Frame) collect.PageMatch {
	if f.Image == nil || f.Image.Bounds().Dx() < 4 {
		return collect.PageMatch{Page: collect.PageUnknown,
			Evidence: fmt.Sprintf("frame %d has no decodable pixels", f.ID)}
	}
	get := func(x, y int) int { return int(f.Image.Pix[f.Image.PixOffset(x, y)]) }
	switch get(0, 10) {
	case 1:
		occupied := make([]bool, m.slots())
		for i := range occupied {
			occupied[i] = get(i, 8) > 0
		}
		flags := get(3, 10)
		return collect.PageMatch{
			Page: collect.PageWarehouse,
			List: &collect.ListEvidence{
				Occupied: occupied,
				Top:      flags&1 > 0,
				Bottom:   flags&2 > 0,
			},
			Evidence: fmt.Sprintf("decoded frame %d as the warehouse list (viewport %d, top=%v, bottom=%v)",
				f.ID, get(2, 10)-1, flags&1 > 0, flags&2 > 0),
		}
	case 2:
		sel := get(1, 10) - 1
		var selected *int
		if sel >= 0 {
			selected = &sel
		}
		return collect.PageMatch{
			Page:   collect.PageDiscDetail,
			Detail: &collect.DetailEvidence{Selected: selected},
			Evidence: fmt.Sprintf("decoded frame %d as the disc detail (selection slot %d)",
				f.ID, sel),
		}
	default:
		return collect.PageMatch{Page: collect.PageUnknown,
			Evidence: fmt.Sprintf("frame %d carries no decidable page marker", f.ID)}
	}
}

// MatchScroll implements collect.Matcher: derive the overlap
// correspondences between two warehouse frames from their viewport
// markers.
func (m *pixelMatcher) MatchScroll(before, after platform.Frame) collect.ScrollMatch {
	vb := m.viewportOf(before) - 1
	va := m.viewportOf(after) - 1
	if vb < 0 || va < 0 {
		return collect.ScrollMatch{Evidence: "a frame is not a decodable warehouse list"}
	}
	if va == vb {
		return collect.ScrollMatch{Displaced: false,
			Evidence: fmt.Sprintf("frames %d → %d: still viewport %d, the list did not move", before.ID, after.ID, vb)}
	}
	if m.scrollAmbiguous {
		return collect.ScrollMatch{Displaced: true,
			Evidence: fmt.Sprintf("frames %d → %d: viewport %d → %d but the overlap admits no unique correspondences (scripted)",
				before.ID, after.ID, vb, va)}
	}
	var matches []collect.PositionPair
	for p := va * m.stride; p < vb*m.stride+m.slots(); p++ {
		from, to := p-vb*m.stride, p-va*m.stride
		if from < 0 || to < 0 || from >= m.slots() || to >= m.slots() {
			continue
		}
		matches = append(matches, collect.PositionPair{
			FromRow: from / m.cols, FromColumn: from % m.cols,
			ToRow: to / m.cols, ToColumn: to % m.cols,
		})
	}
	if m.scrollRagged && len(matches) > 0 {
		matches = matches[:len(matches)-1]
	}
	return collect.ScrollMatch{Displaced: true, Matches: matches,
		Evidence: fmt.Sprintf("frames %d → %d: viewport %d → %d with %d overlap correspondences",
			before.ID, after.ID, vb, va, len(matches))}
}

func (m *pixelMatcher) viewportOf(f platform.Frame) int {
	if f.Image == nil {
		return 0
	}
	return int(f.Image.Pix[f.Image.PixOffset(2, 10)])
}

// ---- the harness ----

// fastRecognizer resolves every task immediately.
type fastRecognizer struct{}

func (fastRecognizer) Recognize(ctx context.Context, task scan.Task, submit scan.Submit) error {
	return submit(task.SessionID, task.Seq, scan.Recognition{Result: task.Observation})
}

// traversalHarness drives one Inventory through the real kernel, the
// collect controller and the scripted fake session.
type traversalHarness struct {
	t       *testing.T
	game    *scriptedGame
	matcher *pixelMatcher
	session *platformfake.Session
	inv     *collect.Inventory
	kernel  *scan.Session
	sink    *recordSink
	stops   int

	// Recognizer and Capacity override the defaults before start.
	Recognizer scan.Recognizer
	Capacity   int
}

func newTraversalHarness(t *testing.T, game *scriptedGame) *traversalHarness {
	t.Helper()
	matcher := &pixelMatcher{rows: game.rows, cols: game.cols, stride: game.stride}
	session := &platformfake.Session{}
	session.ObserveFn = game.observe
	session.FrameFn = game.frame
	session.DispatchFn = game.dispatch
	h := &traversalHarness{
		t:       t,
		game:    game,
		matcher: matcher,
		session: session,
		inv: &collect.Inventory{
			Layout:       game.layout(),
			Matcher:      matcher,
			ActionBudget: 150 * time.Millisecond,
			StableFrames: 3,
			MaxRetries:   2,
			RecheckEvery: time.Millisecond,
		},
	}
	h.Recognizer = fastRecognizer{}
	h.Capacity = 2
	return h
}

func (h *traversalHarness) start() {
	h.t.Helper()
	port := &platformfake.Port{NewSession: func() *platformfake.Session { return h.session }}
	inv := h.inv
	sink := &recordSink{}
	ctrl := &collect.Controller{
		Port:         port,
		Target:       platform.TargetSpec{BundleID: "com.example.game"},
		Hotkey:       platform.HotkeySpec{Key: platform.KeyS, Ctrl: true, Alt: true},
		Traversal:    func() collect.Traversal { return inv },
		Stop:         func() { h.stops++ },
		ObserveEvery: 2 * time.Millisecond,
	}
	kernel, err := scan.Start(scan.Config{
		ID:         scan.ID("traversal-test"),
		Capacity:   h.Capacity,
		Workers:    1,
		Controller: ctrl,
		Recognizer: h.Recognizer,
		Deliverer:  sink,
	})
	if err != nil {
		h.t.Fatalf("scan.Start: %v", err)
	}
	h.kernel = kernel
	h.sink = sink
}

// end waits for the session to seal and returns the snapshot.
func (h *traversalHarness) end() scan.Snapshot {
	h.t.Helper()
	select {
	case <-h.kernel.Done():
	case <-time.After(5 * time.Second):
		h.t.Fatalf("timed out waiting for the session to end; ops so far: %d", len(h.game.opsSnapshot()))
	}
	return h.kernel.Snapshot()
}

// endFailed asserts the session failed with the given trigger code and
// returns the snapshot.
func (h *traversalHarness) endFailed(code string) scan.Snapshot {
	h.t.Helper()
	snap := h.end()
	if snap.End != scan.EndFailed {
		h.t.Fatalf("end %s, want failed", snap.End)
	}
	if snap.Trigger == nil || snap.Trigger.Code != code {
		h.t.Fatalf("trigger %+v, want code %s", snap.Trigger, code)
	}
	return snap
}

// observations returns the admitted observations in sequence order.
func (h *traversalHarness) observations() []collect.Observation {
	h.t.Helper()
	out := make([]collect.Observation, 0, h.sink.len())
	for _, rec := range h.sink.recordsSnapshot() {
		obs, ok := rec.Observation.(collect.Observation)
		if !ok {
			h.t.Fatalf("record %d carries a %T, want collect.Observation", rec.Seq, rec.Observation)
		}
		out = append(out, obs)
	}
	return out
}

// positions renders the observations' ledger coordinates.
func positions(obs []collect.Observation) [][3]int {
	out := make([][3]int, 0, len(obs))
	for _, o := range obs {
		out = append(out, [3]int{o.Viewport, o.Row, o.Column})
	}
	return out
}

// ---- the tests ----

// T1 (normal sequence): two viewports of scrolling over nine positions
// with one empty slot — the machine visits every occupied position in
// reading order, admits each with its evidence, scrolls twice on
// verified correspondences and completes on the bottom evidence.
//
// The fake's details differ only in the selection marker, so this
// sequence doubles as T5 §4's 相邻相同详情 case: every admission rests on
// the selection transition, never on a content change.
func TestTraversalNormalSequenceWithScrolls(t *testing.T) {
	game := newScriptedGame(true, true, true, false, true, true, true, true, true)
	h := newTraversalHarness(t, game)
	h.start()
	snap := h.end()

	if snap.End != scan.EndCompleted {
		t.Fatalf("end %s (trigger %+v), want completed", snap.End, snap.Trigger)
	}
	if snap.Trigger != nil && snap.Trigger.Code != scan.CodeVerifiedEnd {
		t.Fatalf("a completed walk reports verifiedEnd, got %+v", snap.Trigger)
	}
	obs := h.observations()
	want := [][3]int{
		{0, 0, 0}, {0, 0, 1}, {0, 1, 0}, // positions 0,1,2 (position 3 is empty)
		{0, 2, 0}, {0, 2, 1}, // positions 4,5
		{1, 2, 0}, {1, 2, 1}, // viewport 1: only the new positions 6,7
		{2, 2, 0}, // viewport 2: only position 8 (position 9 is beyond the items)
	}
	if len(obs) != len(want) {
		t.Fatalf("admitted %d observations, want %d: %+v", len(obs), len(want), positions(obs))
	}
	for k, o := range obs {
		got := [3]int{o.Viewport, o.Row, o.Column}
		if got != want[k] {
			t.Fatalf("observation %d at %v, want %v (all: %v)", k+1, got, want[k], positions(obs))
		}
		if o.VisitIndex != k+1 {
			t.Fatalf("observation %d carries visitIndex %d", k+1, o.VisitIndex)
		}
		if o.Association != collect.AssociationTransition {
			t.Fatalf("observation %d association %s", k+1, o.Association)
		}
		if o.Geometry != 1 || o.FrameID == 0 || o.PreClickFrameID == 0 || o.PreClickFrameID == o.FrameID {
			t.Fatalf("observation %d provenance broken: %+v", k+1, o)
		}
		if o.Image == nil || o.Evidence == "" {
			t.Fatalf("observation %d lacks its capture or evidence", k+1)
		}
	}

	// The operations: one click and one close per admitted item, two
	// drags for the two transitions, nothing else.
	ops := game.opsSnapshot()
	clicks, closes, drags := 0, 0, 0
	for _, op := range ops {
		switch op.Kind {
		case platform.InputClick:
			if op.At.In(closeAnchor(game.rows, game.cols)) {
				closes++
			} else {
				clicks++
			}
			if op.Geometry != 1 {
				t.Fatalf("click with geometry %d, want the bound 1", op.Geometry)
			}
			if op.FrameID == 0 {
				t.Fatalf("click without pre-action frame provenance")
			}
		case platform.InputDrag:
			drags++
			if op.Geometry != 1 || op.FrameID == 0 {
				t.Fatalf("drag without the bound geometry or provenance: %+v", op)
			}
		default:
			t.Fatalf("unexpected operation %+v", op)
		}
	}
	if clicks != len(want) || closes != len(want) || drags != 2 {
		t.Fatalf("operations: %d clicks, %d closes, %d drags; want %d/%d/2", clicks, closes, drags, len(want), len(want))
	}

	// The observed-scope report carries what precheck established.
	scope := string(h.inv.ObservedScope())
	for _, want := range []string{`"kind":"zzzInventory"`, `"page":"warehouse"`, `"geometry":1`, `"slots":6`, `"top":true`} {
		if !strings.Contains(scope, want) {
			t.Fatalf("observed scope %s lacks %s", scope, want)
		}
	}
}

// T2 (end without scrolling): a list that fits one viewport completes
// on the bottom evidence with no drag at all.
func TestTraversalEndEvidenceCompletesWithoutScroll(t *testing.T) {
	game := newScriptedGame(true, true, true, true)
	h := newTraversalHarness(t, game)
	h.start()
	snap := h.end()
	if snap.End != scan.EndCompleted {
		t.Fatalf("end %s, want completed", snap.End)
	}
	if got := len(h.observations()); got != 4 {
		t.Fatalf("admitted %d, want 4", got)
	}
	for _, op := range game.opsSnapshot() {
		if op.Kind == platform.InputDrag {
			t.Fatalf("a one-viewport list must not be dragged: %+v", op)
		}
	}
}

// T3 (precheck): an empty layout, an uncalibrated matcher, an
// undecidable page and a mid-list start each refuse to begin, with
// their own codes.
func TestTraversalPrecheckRefusals(t *testing.T) {
	cases := []struct {
		name string
		set  func(*scriptedGame, *collect.Inventory)
		code string
	}{
		{"empty layout", func(g *scriptedGame, inv *collect.Inventory) {
			inv.Layout = collect.Layout{}
		}, collect.CodeAnchorsMissing},
		{"uncalibrated matcher", func(g *scriptedGame, inv *collect.Inventory) {
			inv.Matcher = nil
		}, collect.CodePageUnverified},
		{"undecidable page", func(g *scriptedGame, inv *collect.Inventory) {
			g.page = collect.PageUnknown
		}, collect.CodePageUnverified},
		{"mid-list start without top evidence", func(g *scriptedGame, inv *collect.Inventory) {
			g.viewport = 1
		}, collect.CodePageUnverified},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			game := newScriptedGame(true, true, true, true, true, true)
			h := newTraversalHarness(t, game)
			tc.set(game, h.inv)
			h.start()
			snap := h.endFailed(tc.code)
			if snap.Counts.Captured != 0 {
				t.Fatalf("a refused precheck admits nothing: %+v", snap.Counts)
			}
			if ops := game.opsSnapshot(); len(ops) != 0 {
				t.Fatalf("a refused precheck dispatches nothing: %+v", ops)
			}
		})
	}
}

// T4 (effect lag): a click whose effect shows after a few animation
// frames still verifies — the machine waits within the budget instead
// of re-clicking.
func TestTraversalClickEffectLagStillAdmits(t *testing.T) {
	game := newScriptedGame(true, true)
	game.effectLag = 4
	h := newTraversalHarness(t, game)
	h.start()
	snap := h.end()
	if snap.End != scan.EndCompleted || len(h.observations()) != 2 {
		t.Fatalf("end %s, admitted %d, want completed with 2", snap.End, len(h.observations()))
	}
	clicks := 0
	for _, op := range game.opsSnapshot() {
		if op.Kind == platform.InputClick && !op.At.In(closeAnchor(game.rows, game.cols)) {
			clicks++
		}
	}
	if clicks != 2 {
		t.Fatalf("a lagging effect must not cause re-clicks: %d clicks for 2 items", clicks)
	}
}

// T5 (idle stability): once the detail shows, an idle stream's Reused
// confirmations count as stability evidence — unchanged content is
// still current content.
func TestTraversalIdleConfirmationCountsAsStability(t *testing.T) {
	game := newScriptedGame(true)
	game.idleWhenDetail = true
	h := newTraversalHarness(t, game)
	h.start()
	snap := h.end()
	if snap.End != scan.EndCompleted || len(h.observations()) != 1 {
		t.Fatalf("end %s, admitted %d, want completed with 1", snap.End, len(h.observations()))
	}
}

// T6 (retry then success): clicks that verifiably do nothing are
// re-dispatched within the retry bound; the third attempt admits.
func TestTraversalClickRetriesThenSucceeds(t *testing.T) {
	game := newScriptedGame(true, true)
	game.ignoreClicks = 2
	h := newTraversalHarness(t, game)
	h.start()
	snap := h.end()
	if snap.End != scan.EndCompleted || len(h.observations()) != 2 {
		t.Fatalf("end %s, admitted %d, want completed with 2", snap.End, len(h.observations()))
	}
	clicks, closes := 0, 0
	for _, op := range game.opsSnapshot() {
		if op.Kind != platform.InputClick {
			continue
		}
		if op.At.In(closeAnchor(game.rows, game.cols)) {
			closes++
		} else if op.At.Eq(slotCenterOf(h.inv.Layout, 0)) {
			clicks++ // the retried slot
		}
	}
	if clicks != 3 {
		t.Fatalf("the ignored clicks must be retried to the bound: %d clicks on slot 0", clicks)
	}
	if closes != 2 {
		t.Fatalf("2 closes expected, got %d", closes)
	}
}

// T7 (retry exhausted): a click that never takes effect stops the walk
// with navigationMismatch — nothing admitted, the failure kept.
func TestTraversalClickNeverTakesStops(t *testing.T) {
	game := newScriptedGame(true, true)
	game.ignoreClicks = 99
	h := newTraversalHarness(t, game)
	h.start()
	snap := h.endFailed(collect.CodeNavigationMismatch)
	if snap.Counts.Captured != 0 {
		t.Fatalf("an ineffective click admits nothing: %+v", snap.Counts)
	}
	clicks := 0
	for _, op := range game.opsSnapshot() {
		if op.Kind == platform.InputClick {
			clicks++
		}
	}
	if clicks != 3 { // one attempt plus two retries
		t.Fatalf("clicks %d, want 3 (1 + 2 retries)", clicks)
	}
}

// T8 (unknown page, no blind retry): a click that turns the page
// undecidable stops at once — re-clicking an unverifiable page would be
// blind (T5: 不因到期而盲目补点).
func TestTraversalUnknownPageStopsWithoutRetry(t *testing.T) {
	game := newScriptedGame(true, true)
	game.clickOpensUnknown = true
	h := newTraversalHarness(t, game)
	h.start()
	h.endFailed(collect.CodeNavigationMismatch)
	clicks := 0
	for _, op := range game.opsSnapshot() {
		if op.Kind == platform.InputClick {
			clicks++
		}
	}
	if clicks != 1 {
		t.Fatalf("an undecidable page must not be re-clicked: %d clicks", clicks)
	}
}

// T9 (wrong selection): a stable detail whose selection evidence names
// another slot is not attributable — stop unresolved, admit nothing.
func TestTraversalWrongSelectionStopsUnattributed(t *testing.T) {
	game := newScriptedGame(true, true)
	game.clickOpensSlot = 1 // clicking slot 0 opens slot 1's detail
	h := newTraversalHarness(t, game)
	h.start()
	snap := h.endFailed(collect.CodeDetailUnattributed)
	if snap.Counts.Captured != 0 {
		t.Fatalf("an unattributed detail admits nothing: %+v", snap.Counts)
	}
}

// T10 (never settling): a detail whose selection flickers never settles
// — the action had its effect, so no re-click; the position stays
// unresolved.
func TestTraversalDetailNeverSettlesStopsUnattributed(t *testing.T) {
	game := newScriptedGame(true)
	game.detailFlicker = true
	h := newTraversalHarness(t, game)
	h.start()
	snap := h.endFailed(collect.CodeDetailUnattributed)
	if snap.Counts.Captured != 0 {
		t.Fatalf("an unsettled detail admits nothing: %+v", snap.Counts)
	}
	clicks := 0
	for _, op := range game.opsSnapshot() {
		if op.Kind == platform.InputClick {
			clicks++
		}
	}
	if clicks != 1 {
		t.Fatalf("an effect that appeared must not be re-clicked: %d clicks", clicks)
	}
}

// T11 (close never takes): the admission stands, the close retries to
// the bound and the walk stops with what it kept.
func TestTraversalCloseNeverTakesStops(t *testing.T) {
	game := newScriptedGame(true, true)
	game.closeIgnored = true
	h := newTraversalHarness(t, game)
	h.start()
	snap := h.endFailed(collect.CodeNavigationMismatch)
	if snap.Counts.Captured != 1 {
		t.Fatalf("the first admission must stand: %+v", snap.Counts)
	}
}

// T12 (geometry change): a frame under a different geometry version
// stops the walk with geometryChanged; the admitted past stays.
func TestTraversalGeometryChangeStops(t *testing.T) {
	game := newScriptedGame(true, true)
	game.geometryAfter = 8 // during the first detail wait
	h := newTraversalHarness(t, game)
	h.start()
	snap := h.endFailed(platform.CodeGeometryChanged)
	if snap.Counts.Captured > 1 {
		t.Fatalf("the walk must stop at the change: %+v", snap.Counts)
	}
}

// T13 (scroll ambiguity): a displaced list with no unique
// correspondences is positionLost — no guessing how far it went, no
// further drags.
func TestTraversalScrollAmbiguityStopsPositionLost(t *testing.T) {
	game := newScriptedGame(true, true, true, true, true, true, true, true)
	h := newTraversalHarness(t, game)
	h.matcher.scrollAmbiguous = true
	h.start()
	snap := h.endFailed(collect.CodePositionLost)
	if snap.Counts.Captured != 6 {
		t.Fatalf("the first viewport's admissions must stand: %+v", snap.Counts)
	}
	drags := 0
	for _, op := range game.opsSnapshot() {
		if op.Kind == platform.InputDrag {
			drags++
		}
	}
	if drags != 1 {
		t.Fatalf("a position at risk must not be dragged again: %d drags", drags)
	}
}

// T14 (ragged correspondence): correspondences that miss an overlap
// position are rejected — an unaccounted overlap position could hold an
// old or a new item.
func TestTraversalRaggedCorrespondenceStops(t *testing.T) {
	game := newScriptedGame(true, true, true, true, true, true, true, true)
	h := newTraversalHarness(t, game)
	h.matcher.scrollRagged = true
	h.start()
	h.endFailed(collect.CodePositionLost)
}

// T15 (stuck scroll is not an end): a drag that verifiably does not
// move the list is retried to the bound and then stops — no
// displacement is not end evidence (T5 §5: 滚动后无变化不能单独作结束
// 条件).
func TestTraversalStuckScrollIsNotAnEnd(t *testing.T) {
	game := newScriptedGame(true, true, true, true, true, true, true, true)
	game.scrollStuck = true
	h := newTraversalHarness(t, game)
	h.start()
	snap := h.endFailed(collect.CodeNavigationMismatch)
	if snap.Counts.Captured != 6 {
		t.Fatalf("the first viewport's admissions must stand: %+v", snap.Counts)
	}
	drags := 0
	for _, op := range game.opsSnapshot() {
		if op.Kind == platform.InputDrag {
			drags++
		}
	}
	if drags != 3 { // one attempt plus two retries
		t.Fatalf("drags %d, want 3 (1 + 2 retries)", drags)
	}
}

// T16 (stop path): a user stop while the machine waits for pipeline
// capacity unwinds promptly, ends cancelled and dispatches nothing
// further — the stop path exercised at a deterministic waiting point.
func TestTraversalStopCancelsPromptly(t *testing.T) {
	game := newScriptedGame(true, true, true, true, true, true)
	rec := &gatedRecognizer{arrived: make(chan scan.Seq, 4), release: make(chan struct{})}
	h := newTraversalHarness(t, game)
	h.Recognizer = rec
	h.Capacity = 1
	h.start()

	select {
	case <-rec.arrived: // the first item was admitted and picked up; the machine now waits for capacity
	case <-time.After(3 * time.Second):
		t.Fatal("the first item never reached the recognizer")
	}
	clicksBefore := countClicks(game.opsSnapshot())
	if clicksBefore != 1 {
		t.Fatalf("clicks before the stop: %d, want exactly the first item's", clicksBefore)
	}
	started := time.Now()
	h.kernel.Cancel()
	// Release the held recognition so the stop path's finalizer can
	// drain it and seal; the machine itself was already waiting for
	// capacity and can only unwind.
	close(rec.release)
	snap := h.end()
	if snap.End != scan.EndCancelled {
		t.Fatalf("end %s, want cancelled", snap.End)
	}
	if d := time.Since(started); d > 2*time.Second {
		t.Fatalf("the stop must unwind promptly, took %s", d)
	}
	time.Sleep(20 * time.Millisecond)
	if clicksAfter := countClicks(game.opsSnapshot()); clicksAfter != clicksBefore {
		t.Fatalf("the stopped machine dispatched further: %d → %d", clicksBefore, clicksAfter)
	}
}

// T17 (aborted drag): a drag the platform aborted (the input gate
// closed mid-operation) unwinds without dispatching anything further —
// nothing stays pressed on the machine's account.
func TestTraversalAbortedDragUnwinds(t *testing.T) {
	game := newScriptedGame(true, true, true, true, true, true, true, true)
	game.abortNextDrag = true
	h := newTraversalHarness(t, game)
	h.start()
	snap := h.endFailed(platform.CodePermitClosed)
	if snap.Counts.Captured != 6 {
		t.Fatalf("the first viewport's admissions must stand: %+v", snap.Counts)
	}
	ops := game.opsSnapshot()
	if len(ops) == 0 || ops[len(ops)-1].Kind != platform.InputDrag {
		t.Fatalf("the last operation must be the aborted drag: %+v", ops)
	}
	time.Sleep(20 * time.Millisecond)
	if after := len(game.opsSnapshot()); after != len(ops) {
		t.Fatalf("the aborted machine dispatched further: %d → %d", len(ops), after)
	}
}

// T18 (denied dispatch): a platform refusal carries its own code into
// the session's trigger.
func TestTraversalDeniedDispatchStopsWithCode(t *testing.T) {
	game := newScriptedGame(true, true)
	game.denyNextDispatch = true
	h := newTraversalHarness(t, game)
	h.start()
	h.endFailed(platform.CodeFocusLost)
}

// T19 (pipeline backpressure): a full pipeline stops the next click —
// the machine waits for capacity instead of clicking ahead.
type gatedRecognizer struct {
	arrived chan scan.Seq
	release chan struct{}
}

func (r *gatedRecognizer) Recognize(ctx context.Context, task scan.Task, submit scan.Submit) error {
	r.arrived <- task.Seq
	<-r.release
	return submit(task.SessionID, task.Seq, scan.Recognition{Result: task.Observation})
}

func TestTraversalWaitsForPipelineCapacity(t *testing.T) {
	game := newScriptedGame(true, true)
	rec := &gatedRecognizer{arrived: make(chan scan.Seq, 4), release: make(chan struct{})}
	h := newTraversalHarness(t, game)
	h.Recognizer = rec
	h.Capacity = 1
	h.start()

	select {
	case <-rec.arrived: // the first item was admitted and picked up
	case <-time.After(3 * time.Second):
		t.Fatal("the first item never reached the recognizer")
	}
	// The pipeline is full: no second click may leave.
	deadline := time.Now().Add(150 * time.Millisecond)
	for time.Now().Before(deadline) {
		if clicks := countClicks(game.opsSnapshot()); clicks > 1 {
			t.Fatalf("clicked ahead of a full pipeline: %d clicks", clicks)
		}
		time.Sleep(2 * time.Millisecond)
	}
	close(rec.release)
	snap := h.end()
	if snap.End != scan.EndCompleted || len(h.observations()) != 2 {
		t.Fatalf("end %s, admitted %d, want completed with 2", snap.End, len(h.observations()))
	}
}

// slotCenterOf computes one slot's center the way the traversal does.
func slotCenterOf(l collect.Layout, i int) image.Point {
	r := l.Slots[i]
	return r.Min.Add(r.Size().Div(2))
}

func countClicks(ops []platform.InputOp) int {
	n := 0
	for _, op := range ops {
		if op.Kind == platform.InputClick && !op.At.In(closeAnchor(3, 2)) {
			n++
		}
	}
	return n
}
