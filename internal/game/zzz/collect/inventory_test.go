package collect_test

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/game/zzz/collect"
	"github.com/LoTwT/youkai/internal/platform"
	"github.com/LoTwT/youkai/internal/platform/platformfake"
	"github.com/LoTwT/youkai/internal/scan"
)

// The traversal tests (T-series): the single-page Inventory state machine
// driven end to end through the real scan kernel, the collect controller
// and a scripted macOS cloud game. The fake renders real 1920×1080 frames
// in the calibrated regions; the production pixel matcher decodes them, so
// every verdict the machine acts on comes from frame pixels.

type pageKind int

const (
	pageWarehouse pageKind = iota
	pagePanel
	pageUnknown
)

// scriptedGame is a scriptable fake of the ZZZ drive-disc warehouse as
// the macOS cloud client shows it: one frame carries the list, the detail
// pane and the wheel. items is the global occupancy list in reading order;
// the viewport is the first visible global row.
type scriptedGame struct {
	mu sync.Mutex

	layout     collect.Layout
	rows, cols int
	items      []bool
	globalRows int

	// the scene
	page         pageKind
	wheel        int
	filter       string // clear, active, unknown
	viewport     int
	selected     int // global position index; -1 none
	detailGlobal int // the detail pane's content key; -1 unpopulated

	// scripting knobs (see the tests that set them)
	frameInterval     time.Duration
	reuseIdle         bool // serve Reused frames when nothing changed
	effectLag         int  // frame calls until a click's selection shows
	detailLagExtra    int  // extra frame calls until the detail shows
	ignoreClicks      int  // clicks that silently do nothing
	clickOpensSlot    int  // >=0: clicking any slot selects this visible slot
	clickOpensUnknown bool // the click makes the page undecidable
	scrollRows        int  // rows per advance drag; 0 means 4
	scrollStuck       bool // the advance drag does not move the list
	scrollJump        int  // non-zero: the advance drag jumps this many rows
	sameDetail        bool // a new selection keeps the old detail content
	detailFlicker     bool // the detail content alternates every frame
	qualityUnknown    bool // no gold badge (A/B/unknown rarity)
	substatsUnknown   bool // the fourth row's value column is between tiers
	substats4         bool // four substat rows (default true)
	forceBottom       bool // the scrollbar claims the end while mid-list
	geometryAfter     int  // 0: never; else serve geometry 2 from this frame call on
	abortScrollDrag   bool // the next advance drag reports Aborted
	denyNextDispatch  bool // the next dispatch is refused
	content           image.Rectangle

	// records
	ops           []platform.InputOp
	frameCalls    int
	pending       []pendingEffect
	lastFrame     platform.Frame
	lastSignature string
	nextID        uint64
}

type pendingEffect struct {
	dueAt int
	apply func(*scriptedGame)
}

func newScriptedGame(items ...bool) *scriptedGame {
	l := collect.MacCloudLayout()
	if len(items) == 0 {
		items = make([]bool, l.Rows*l.Columns)
		for i := range items {
			items[i] = true
		}
	}
	g := &scriptedGame{
		layout:         l,
		rows:           l.Rows,
		cols:           l.Columns,
		items:          append([]bool(nil), items...),
		page:           pageWarehouse,
		wheel:          1,
		filter:         "clear",
		selected:       -1,
		detailGlobal:   -1,
		clickOpensSlot: -1,
		substats4:      true,
		frameInterval:  time.Millisecond,
		content:        l.Content,
	}
	g.globalRows = (len(g.items) + g.cols - 1) / g.cols
	if g.globalRows < g.rows {
		g.globalRows = g.rows
	}
	return g
}

func (g *scriptedGame) selectInitially(global int) {
	g.selected = global
	g.detailGlobal = global
}

func (g *scriptedGame) maxTop() int {
	if g.globalRows <= g.rows {
		return 0
	}
	return g.globalRows - g.rows
}

func (g *scriptedGame) globalForSlot(slot int) int {
	return (g.viewport+slot/g.cols)*g.cols + slot%g.cols
}

func (g *scriptedGame) occupied(global int) bool {
	return global >= 0 && global < len(g.items) && g.items[global]
}

func (g *scriptedGame) observe() (platform.Observation, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	geo := platform.GeometryVersion(1)
	if g.geometryAfter > 0 && g.frameCalls >= g.geometryAfter {
		geo = 2
	}
	return platform.Observation{At: time.Now(), Ready: true, Geometry: geo}, nil
}

func (g *scriptedGame) frame(ctx context.Context, req platform.FrameRequest) (platform.Frame, error) {
	if err := ctx.Err(); err != nil {
		return platform.Frame{}, err
	}
	if g.frameInterval > 0 {
		timer := time.NewTimer(g.frameInterval)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return platform.Frame{}, ctx.Err()
		case <-timer.C:
		}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.frameCalls++
	for len(g.pending) > 0 && g.pending[0].dueAt <= g.frameCalls {
		effect := g.pending[0]
		g.pending = g.pending[1:]
		effect.apply(g)
	}
	geo := platform.GeometryVersion(1)
	if g.geometryAfter > 0 && g.frameCalls >= g.geometryAfter {
		geo = 2
	}
	sig := g.signature(geo)
	if g.lastFrame.Image != nil && sig == g.lastSignature {
		// The content is unchanged: a fresh frame may share the immutable
		// image. This keeps the deadline-bound waits (and their 1920×1080
		// allocations) bounded under the race detector; the admitted
		// observations own distinct images because each item's content
		// changes the signature.
		g.nextID++
		f := g.lastFrame
		f.ID = g.nextID
		f.ArrivedAt = time.Now()
		f.Reused = g.reuseIdle
		g.lastFrame = f
		return f, nil
	}
	img := g.render()
	g.nextID++
	f := platform.Frame{
		ID:        g.nextID,
		Geometry:  geo,
		Image:     img,
		Content:   g.content,
		ArrivedAt: time.Now(),
	}
	g.lastFrame = f
	g.lastSignature = sig
	return f, nil
}

func (g *scriptedGame) signature(geo platform.GeometryVersion) string {
	flicker := 0
	if g.detailFlicker && g.frameCalls%2 == 1 {
		flicker = 1
	}
	return fmt.Sprintf("%d|%d|%s|%d|%d|%d|%d|%d|%v|%v|%v|%v|%v|%v",
		g.page, g.wheel, g.filter, g.viewport, g.selected, g.detailGlobal, flicker, geo,
		g.qualityUnknown, g.substatsUnknown, g.substats4, g.forceBottom, g.content, g.globalRows)
}

func (g *scriptedGame) render() *image.RGBA {
	l := g.layout
	img := image.NewRGBA(image.Rectangle{Max: l.Image})
	fill := func(r image.Rectangle, c color.RGBA) {
		r = r.Intersect(img.Bounds())
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				img.SetRGBA(x, y, c)
			}
		}
	}
	// The filter panel's probes decide the page.
	if g.page == pagePanel {
		for _, probe := range l.PanelOpen {
			fill(probe.At, probe.Colors["open"])
		}
	} else if g.page == pageWarehouse {
		for _, probe := range l.PanelOpen {
			fill(probe.At, probe.Colors["closed"])
		}
	} else {
		fill(l.PanelOpen[0].At, color.RGBA{R: 100, G: 100, B: 100, A: 255})
		fill(l.PanelOpen[1].At, l.PanelOpen[1].Colors["closed"])
	}
	// The funnel is read on warehouse frames only, but a panel frame may
	// still be ambiguous there; render the scripted state.
	switch g.filter {
	case "clear":
		fill(l.Funnel.At, l.Funnel.Colors["clear"])
	case "active":
		fill(l.Funnel.At, l.Funnel.Colors["active"])
	default:
		fill(l.Funnel.At, color.RGBA{R: 100, G: 100, B: 100, A: 255})
	}
	if g.page != pageWarehouse {
		return img
	}
	// The wheel: exactly one position carries the selection color in its
	// four arc probes.
	if g.wheel >= 1 && g.wheel <= 6 {
		for _, arc := range l.WheelArcs {
			fill(arc.Add(l.Wheel[g.wheel-1]), selectionColor)
		}
	}
	// The grid: per-cell occupancy bars, the selected cell's border, and a
	// per-item pattern inside the viewport so the scroll matcher has
	// content that moves with the list.
	for slot := 0; slot < g.rows*g.cols; slot++ {
		global := g.globalForSlot(slot)
		if g.occupied(global) {
			fill(l.BarRect(slot), barColor)
			center := slotCenter(l, slot)
			v := uint8(40 + (global*37)%160)
			fill(image.Rect(center.X-20, center.Y-20, center.X+20, center.Y+20), color.RGBA{R: v, G: v, B: v, A: 255})
		}
		if g.selected == global {
			fill(l.BorderRect(slot), selectionColor)
		}
	}
	g.renderScrollbar(img)
	g.renderDetail(img)
	return img
}

func (g *scriptedGame) renderScrollbar(img *image.RGBA) {
	track := g.layout.ScrollTrack
	x := track.Min.X
	start, end := track.Min.Y, track.Max.Y-1
	if !g.forceBottom && g.maxTop() > 0 {
		length := track.Dy() * g.rows / g.globalRows
		if length < 40 {
			length = 40
		}
		start += (track.Dy() - length) * g.viewport / g.maxTop()
		end = start + length - 1
		if end > track.Max.Y-1 {
			end = track.Max.Y - 1
		}
	}
	thumb := color.RGBA{R: 200, G: 200, B: 200, A: 255}
	for y := start; y <= end; y++ {
		img.SetRGBA(x, y, thumb)
	}
}

func (g *scriptedGame) renderDetail(img *image.RGBA) {
	if g.detailGlobal < 0 {
		return
	}
	fill := func(r image.Rectangle, c color.RGBA) {
		r = r.Intersect(img.Bounds())
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				img.SetRGBA(x, y, c)
			}
		}
	}
	id := g.detailGlobal
	if g.detailFlicker && g.frameCalls%2 == 1 {
		id ^= 0x7F
	}
	// Seven horizontal bands encode the content key. Every band is ink
	// (>60), so the name line is populated for every id, and any two
	// different keys differ by at least one 120-value band — well above the
	// 4.0 Different threshold.
	for _, line := range g.layout.DetailLines {
		h := line.Rect.Dy()
		for b := 0; b < 7; b++ {
			y0 := line.Rect.Min.Y + b*h/7
			y1 := line.Rect.Min.Y + (b+1)*h/7
			v := uint8(80)
			if id&(1<<b) != 0 {
				v = 200
			}
			fill(image.Rect(line.Rect.Min.X, y0, line.Rect.Max.X, y1), color.RGBA{R: v, G: v, B: v, A: 255})
		}
	}
	if !g.qualityUnknown {
		fill(g.layout.QualityBadge, goldColor)
	}
	switch {
	case g.substatsUnknown:
		fill(g.layout.Substat4Probe, color.RGBA{A: 255})
		r := g.layout.Substat4Probe
		n := 0
		for y := r.Min.Y; y < r.Max.Y && n < 60; y++ {
			for x := r.Min.X; x < r.Max.X && n < 60; x++ {
				img.SetRGBA(x, y, barColor)
				n++
			}
		}
	case g.substats4:
		fill(g.layout.Substat4Probe, barColor)
	default:
		fill(g.layout.Substat4Probe, color.RGBA{A: 255})
	}
}

func (g *scriptedGame) dispatch(op platform.InputOp) platform.InputResult {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.ops = append(g.ops, op)
	now := time.Now()
	if g.denyNextDispatch {
		g.denyNextDispatch = false
		return platform.InputResult{Status: platform.InputNotDispatched,
			Denial: platform.NewFault(platform.CodeFocusLost, "focus lost in the script")}
	}
	if g.abortScrollDrag && op.Kind == platform.InputDrag &&
		op.At.Eq(g.layout.ScrollFrom) && op.To.Eq(g.layout.ScrollTo) {
		g.abortScrollDrag = false
		return platform.InputResult{Status: platform.InputPartial, Aborted: true,
			EventsPlanned: 12, EventsPosted: 5, StartedAt: now, FinishedAt: now}
	}
	lag := g.frameCalls + g.effectLag
	switch op.Kind {
	case platform.InputClick:
		if g.page != pageWarehouse {
			return posted(now)
		}
		slot, ok := g.slotAt(op.At)
		if !ok {
			return posted(now)
		}
		if g.clickOpensUnknown {
			g.pending = append(g.pending, pendingEffect{dueAt: lag, apply: func(g *scriptedGame) {
				g.page = pageUnknown
			}})
			return posted(now)
		}
		if g.ignoreClicks > 0 {
			g.ignoreClicks--
			return posted(now)
		}
		target := g.globalForSlot(slot)
		if g.clickOpensSlot >= 0 {
			target = g.globalForSlot(g.clickOpensSlot)
		}
		previousDetail := g.detailGlobal
		g.pending = append(g.pending, pendingEffect{dueAt: lag, apply: func(g *scriptedGame) {
			g.selected = target
		}})
		if g.sameDetail && previousDetail >= 0 {
			return posted(now)
		}
		due := lag
		if g.detailLagExtra > 0 {
			due += g.detailLagExtra
		}
		g.pending = append(g.pending, pendingEffect{dueAt: due, apply: func(g *scriptedGame) {
			g.detailGlobal = target
		}})
	case platform.InputDrag:
		switch {
		case op.At.Eq(g.layout.ScrollFrom) && op.To.Eq(g.layout.ScrollTo):
			if g.scrollStuck {
				return posted(now)
			}
			step := g.scrollRows
			if step == 0 {
				step = 4
			}
			if g.scrollJump != 0 {
				step = g.scrollJump
			}
			target := g.viewport + step
			if target > g.maxTop() {
				target = g.maxTop()
			}
			if target != g.viewport {
				g.pending = append(g.pending, pendingEffect{dueAt: lag, apply: func(g *scriptedGame) {
					g.viewport = target
				}})
			}
		case op.At.Eq(g.layout.TopFrom) && op.To.Eq(g.layout.TopTo):
			if g.viewport != 0 {
				g.pending = append(g.pending, pendingEffect{dueAt: lag, apply: func(g *scriptedGame) {
					g.viewport = 0
				}})
			}
		}
	}
	return posted(now)
}

func posted(now time.Time) platform.InputResult {
	return platform.InputResult{Status: platform.InputPosted, EventsPlanned: 2, EventsPosted: 2,
		StartedAt: now, FinishedAt: now}
}

func (g *scriptedGame) slotAt(p image.Point) (int, bool) {
	for i, r := range g.layout.Slots {
		if p.In(r) {
			return i, true
		}
	}
	return 0, false
}

func (g *scriptedGame) opsSnapshot() []platform.InputOp {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]platform.InputOp(nil), g.ops...)
}

var (
	selectionColor = color.RGBA{R: 224, G: 208, B: 67, A: 255}
	barColor       = color.RGBA{R: 240, G: 240, B: 240, A: 255}
	goldColor      = color.RGBA{R: 255, G: 200, B: 0, A: 255}
)

// ---- the harness ----

type fastRecognizer struct{}

func (fastRecognizer) Recognize(ctx context.Context, task scan.Task, submit scan.Submit) error {
	return submit(task.SessionID, task.Seq, scan.Recognition{Result: task.Observation})
}

type gatedRecognizer struct {
	arrived chan scan.Seq
	release chan struct{}
}

func (r *gatedRecognizer) Recognize(ctx context.Context, task scan.Task, submit scan.Submit) error {
	r.arrived <- task.Seq
	<-r.release
	return submit(task.SessionID, task.Seq, scan.Recognition{Result: task.Observation})
}

type traversalHarness struct {
	t       *testing.T
	game    *scriptedGame
	session *platformfake.Session
	inv     *collect.Inventory
	kernel  *scan.Session
	sink    *recordSink

	Recognizer scan.Recognizer
	Capacity   int
}

func newTraversalHarness(t *testing.T, game *scriptedGame) *traversalHarness {
	t.Helper()
	l := collect.MacCloudLayout()
	session := &platformfake.Session{}
	session.ObserveFn = game.observe
	session.FrameFn = game.frame
	session.DispatchFn = game.dispatch
	h := &traversalHarness{
		t:       t,
		game:    game,
		session: session,
		inv: &collect.Inventory{
			Layout:           l,
			Matcher:          collect.NewPixelMatcher(l, collect.MacCloudCalibration()),
			ActionBudget:     300 * time.Millisecond,
			StableFrames:     3,
			DetailStableSpan: -1,
			MaxRetries:       2,
			RecheckEvery:     time.Millisecond,
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
		Stop:         func() {},
		ObserveEvery: 2 * time.Millisecond,
	}
	kernel, err := scan.Start(scan.Config{
		ID:         scan.ID("single-page-traversal-test"),
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

func (h *traversalHarness) end() scan.Snapshot {
	h.t.Helper()
	select {
	case <-h.kernel.Done():
	case <-time.After(15 * time.Second):
		h.t.Fatalf("timed out waiting for the session to end; ops so far: %d", len(h.game.opsSnapshot()))
	}
	return h.kernel.Snapshot()
}

func (h *traversalHarness) endCompleted() scan.Snapshot {
	h.t.Helper()
	snap := h.end()
	if snap.End != scan.EndCompleted {
		h.t.Fatalf("end %s (trigger %+v), want completed", snap.End, snap.Trigger)
	}
	if snap.Trigger != nil && snap.Trigger.Code != scan.CodeVerifiedEnd {
		h.t.Fatalf("a completed walk reports verifiedEnd, got %+v", snap.Trigger)
	}
	return snap
}

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

func positions(obs []collect.Observation) [][3]int {
	out := make([][3]int, 0, len(obs))
	for _, o := range obs {
		out = append(out, [3]int{o.Viewport, o.Row, o.Column})
	}
	return out
}

func slotCenter(l collect.Layout, i int) image.Point {
	r := l.Slots[i]
	return r.Min.Add(r.Size().Div(2))
}

func countOps(ops []platform.InputOp, kind platform.InputKind, at, to image.Point) int {
	n := 0
	for _, op := range ops {
		if op.Kind != kind {
			continue
		}
		if at != (image.Point{}) && !op.At.Eq(at) {
			continue
		}
		if to != (image.Point{}) && !op.To.Eq(to) {
			continue
		}
		n++
	}
	return n
}

// ---- the tests ----

// T1: one full viewport completes on the scrollbar's bottom evidence,
// paired with a stationary advance drag. Every occupied slot is admitted
// through the real pixel matcher; no close step exists.
func TestSinglePageCompletesOneViewport(t *testing.T) {
	game := newScriptedGame(true, true, true, true, true, true, true, true, true, true, true, true, true, true, true, true, true, true, true, true)
	h := newTraversalHarness(t, game)
	h.start()
	snap := h.endCompleted()

	obs := h.observations()
	if len(obs) != 20 {
		t.Fatalf("admitted %d, want 20: %v", len(obs), positions(obs))
	}
	for i, o := range obs {
		if o.Viewport != 0 || o.Row != i/4 || o.Column != i%4 {
			t.Fatalf("observation %d at %v, want viewport 0 slot %d", i+1, [3]int{o.Viewport, o.Row, o.Column}, i)
		}
		if o.Association != collect.AssociationTransition {
			t.Fatalf("observation %d association %s, want transition", i+1, o.Association)
		}
		if o.Geometry != 1 || o.FrameID == 0 || o.PreClickFrameID == 0 || o.PreClickFrameID >= o.FrameID {
			t.Fatalf("observation %d provenance broken: %+v", i+1, o)
		}
		if o.Image == nil || o.Evidence == "" || !strings.Contains(o.Evidence, "matcher:") {
			t.Fatalf("observation %d lacks its capture or evidence", i+1)
		}
	}
	if snap.Counts.Captured != 20 {
		t.Fatalf("captured count %+v", snap.Counts)
	}

	ops := game.opsSnapshot()
	if clicks := countOps(ops, platform.InputClick, image.Point{}, image.Point{}); clicks != 20 {
		t.Fatalf("%d clicks, want 20", clicks)
	}
	if drags := countOps(ops, platform.InputDrag, game.layout.ScrollFrom, game.layout.ScrollTo); drags != 1 {
		t.Fatalf("%d advance drags, want the bottom confirmation only", drags)
	}
	if drags := countOps(ops, platform.InputDrag, game.layout.TopFrom, game.layout.TopTo); drags != 1 {
		t.Fatalf("%d top drags, want 1", drags)
	}
	// The observed-scope report carries what the precheck established.
	scope := string(h.inv.ObservedScope())
	for _, want := range []string{`"kind":"zzzInventory"`, `"page":"warehouse"`, `"layout":"maccloud-1920x1080"`, `"geometry":1`, `"slots":20`, `"startPosition":1`, `"filterAtStart":"clear"`, `"sort":"unverified"`} {
		if !strings.Contains(scope, want) {
			t.Fatalf("observed scope %s lacks %s", scope, want)
		}
	}
}

// T2: a short list (three discs) verifies the top and the bottom on the
// same scrollbar state and admits only the occupied slots; the bottom is
// still paired with a stationary advance drag.
func TestSinglePageShortList(t *testing.T) {
	game := newScriptedGame(true, true, true)
	h := newTraversalHarness(t, game)
	h.start()
	h.endCompleted()
	obs := h.observations()
	want := [][3]int{{0, 0, 0}, {0, 0, 1}, {0, 0, 2}}
	if fmt.Sprint(positions(obs)) != fmt.Sprint(want) {
		t.Fatalf("positions %v, want %v", positions(obs), want)
	}
	if drags := countOps(game.opsSnapshot(), platform.InputDrag, game.layout.ScrollFrom, game.layout.ScrollTo); drags != 1 {
		t.Fatalf("%d advance drags, want the bottom confirmation", drags)
	}
}

// T3: a list with an empty slot completes with no item record for it.
func TestSinglePageSparseList(t *testing.T) {
	items := make([]bool, 20)
	for i := range items {
		items[i] = true
	}
	items[3] = false
	items[11] = false
	game := newScriptedGame(items...)
	h := newTraversalHarness(t, game)
	h.start()
	h.endCompleted()
	obs := h.observations()
	if len(obs) != 18 {
		t.Fatalf("admitted %d, want 18: %v", len(obs), positions(obs))
	}
	for _, o := range obs {
		if o.Viewport == 0 && ((o.Row == 0 && o.Column == 3) || (o.Row == 2 && o.Column == 3)) {
			t.Fatalf("an empty slot was admitted: %+v", o)
		}
	}
}

// T4: an all-empty list admits nothing and still verifies its end.
func TestSinglePageEmptyList(t *testing.T) {
	game := newScriptedGame(make([]bool, 20)...)
	h := newTraversalHarness(t, game)
	h.start()
	h.endCompleted()
	if got := len(h.observations()); got != 0 {
		t.Fatalf("admitted %d, want 0", got)
	}
	if clicks := countOps(game.opsSnapshot(), platform.InputClick, image.Point{}, image.Point{}); clicks != 0 {
		t.Fatalf("%d clicks on an empty list", clicks)
	}
}

// T5: a four-row scroll over nine rows visits the new rows only; the
// overlap row's visit state is carried across the verified
// correspondences, and the walk completes at the bottom.
func TestSinglePageScrollCarriesOverlap(t *testing.T) {
	items := make([]bool, 36)
	for i := range items {
		items[i] = true
	}
	items[3] = false
	game := newScriptedGame(items...)
	h := newTraversalHarness(t, game)
	h.start()
	h.endCompleted()

	obs := h.observations()
	if len(obs) != 35 {
		t.Fatalf("admitted %d, want 35: %v", len(obs), positions(obs))
	}
	for i, o := range obs[:19] {
		want := [3]int{0, i / 4, i % 4}
		if i >= 3 {
			want = [3]int{0, (i + 1) / 4, (i + 1) % 4}
		}
		if got := [3]int{o.Viewport, o.Row, o.Column}; got != want {
			t.Fatalf("observation %d at %v, want %v", i+1, got, want)
		}
	}
	for i, o := range obs[19:] {
		if o.Viewport != 1 || o.Row < 1 || o.Row > 4 {
			t.Fatalf("observation %d at %v, want viewport 1's new rows 1..4: %v", i+20, [3]int{o.Viewport, o.Row, o.Column}, positions(obs[19:]))
		}
	}
	if !strings.Contains(obs[19].Evidence, "pre-click list frame") {
		t.Fatalf("the first post-scroll admission lacks its pre-click evidence: %s", obs[19].Evidence)
	}

	ops := game.opsSnapshot()
	if drags := countOps(ops, platform.InputDrag, game.layout.ScrollFrom, game.layout.ScrollTo); drags != 2 {
		t.Fatalf("%d advance drags, want one scroll + one bottom confirmation", drags)
	}
	if drags := countOps(ops, platform.InputDrag, game.layout.TopFrom, game.layout.TopTo); drags != 1 {
		t.Fatalf("%d top drags, want 1", drags)
	}
}

// T6: a three-row displacement is accepted as the matcher measured it;
// the after-viewport overlap covers two rows.
func TestSinglePageThreeRowScroll(t *testing.T) {
	items := make([]bool, 32) // eight rows
	for i := range items {
		items[i] = true
	}
	game := newScriptedGame(items...)
	game.scrollRows = 3
	h := newTraversalHarness(t, game)
	h.start()
	h.endCompleted()
	obs := h.observations()
	if len(obs) != 32 {
		t.Fatalf("admitted %d, want 32: %v", len(obs), positions(obs))
	}
	seen := make(map[[3]int]bool, len(obs))
	for _, o := range obs {
		key := [3]int{o.Viewport, o.Row, o.Column}
		if seen[key] {
			t.Fatalf("duplicate position %v", key)
		}
		seen[key] = true
	}
}

// T7: an already-selected target is admitted through the initial-read
// path, with no click and no pre-click frame; the rest are transitions.
func TestSinglePageInitialRead(t *testing.T) {
	game := newScriptedGame(true, true, true, true)
	game.selectInitially(0)
	h := newTraversalHarness(t, game)
	h.start()
	h.endCompleted()
	obs := h.observations()
	if len(obs) != 4 {
		t.Fatalf("admitted %d, want 4", len(obs))
	}
	if obs[0].Association != collect.AssociationInitial || obs[0].PreClickFrameID != 0 {
		t.Fatalf("first admission %+v, want an initial read", obs[0])
	}
	for _, o := range obs[1:] {
		if o.Association != collect.AssociationTransition || o.PreClickFrameID == 0 {
			t.Fatalf("later admission %+v, want a verified transition", o)
		}
	}
	if clicks := countOps(game.opsSnapshot(), platform.InputClick, image.Point{}, image.Point{}); clicks != 3 {
		t.Fatalf("%d clicks, want 3 after the initial read", clicks)
	}
}

// T8: a click whose selection and detail arrive after an animation lag is
// still attributed; the machine waits within the budget instead of
// re-clicking.
func TestSinglePageClickEffectLag(t *testing.T) {
	game := newScriptedGame(true, true, true)
	game.effectLag = 4
	game.detailLagExtra = 2
	h := newTraversalHarness(t, game)
	h.start()
	h.endCompleted()
	if got := len(h.observations()); got != 3 {
		t.Fatalf("admitted %d, want 3", got)
	}
	if clicks := countOps(game.opsSnapshot(), platform.InputClick, image.Point{}, image.Point{}); clicks != 3 {
		t.Fatalf("%d clicks, want one per item", clicks)
	}
}

// T9: an idle stream's Reused confirmations count as stability evidence,
// and a stationary drag is verified through the same reused content.
func TestSinglePageIdleConfirmation(t *testing.T) {
	game := newScriptedGame(true)
	game.reuseIdle = true
	h := newTraversalHarness(t, game)
	h.start()
	h.endCompleted()
	if got := len(h.observations()); got != 1 {
		t.Fatalf("admitted %d, want 1", got)
	}
}

// T10: clicks that verifiably do nothing are re-dispatched within the
// retry bound.
func TestSinglePageClickRetriesThenSucceeds(t *testing.T) {
	game := newScriptedGame(true, true)
	game.ignoreClicks = 2
	h := newTraversalHarness(t, game)
	h.start()
	h.endCompleted()
	if got := len(h.observations()); got != 2 {
		t.Fatalf("admitted %d, want 2", got)
	}
	clicks := 0
	for _, op := range game.opsSnapshot() {
		if op.Kind == platform.InputClick && op.At.Eq(slotCenter(game.layout, 0)) {
			clicks++
		}
	}
	if clicks != 3 { // one attempt plus two retries on slot 0
		t.Fatalf("%d clicks on slot 0, want 3 (1 + 2 retries)", clicks)
	}
}

// T11: a click that never takes effect stops with navigationMismatch.
func TestSinglePageClickNeverTakesStops(t *testing.T) {
	game := newScriptedGame(true, true)
	game.ignoreClicks = 99
	h := newTraversalHarness(t, game)
	h.start()
	snap := h.endFailed(collect.CodeNavigationMismatch)
	if snap.Counts.Captured != 0 {
		t.Fatalf("an ineffective click admits nothing: %+v", snap.Counts)
	}
	if clicks := countOps(game.opsSnapshot(), platform.InputClick, image.Point{}, image.Point{}); clicks != 3 {
		t.Fatalf("%d clicks, want 3 (1 + 2 retries)", clicks)
	}
}

// T12: a click that turns the page undecidable stops at once — no blind
// re-click.
func TestSinglePageUnknownPageStopsWithoutRetry(t *testing.T) {
	game := newScriptedGame(true, true)
	game.clickOpensUnknown = true
	h := newTraversalHarness(t, game)
	h.start()
	h.endFailed(collect.CodeNavigationMismatch)
	if clicks := countOps(game.opsSnapshot(), platform.InputClick, image.Point{}, image.Point{}); clicks != 1 {
		t.Fatalf("%d clicks, want 1", clicks)
	}
}

// T13: a detail whose selection names another slot is not attributable.
func TestSinglePageWrongSelectionStopsUnattributed(t *testing.T) {
	game := newScriptedGame(true, true)
	game.clickOpensSlot = 1
	h := newTraversalHarness(t, game)
	h.start()
	snap := h.endFailed(collect.CodeDetailUnattributed)
	if snap.Counts.Captured != 0 {
		t.Fatalf("an unattributed detail admits nothing: %+v", snap.Counts)
	}
}

// T14: a selection transition whose detail content stays the same is not
// attributable — the same-detail neighbor needs the 4b-2 round trip.
func TestSinglePageSameDetailStopsUnattributed(t *testing.T) {
	game := newScriptedGame(true, true)
	game.sameDetail = true
	h := newTraversalHarness(t, game)
	h.start()
	snap := h.endFailed(collect.CodeDetailUnattributed)
	if snap.Counts.Captured != 1 {
		t.Fatalf("the first admission must stand: %+v", snap.Counts)
	}
}

// T15: a flickering detail never settles; the effect appeared, so the
// machine must not re-click.
func TestSinglePageDetailFlickerStopsUnattributed(t *testing.T) {
	game := newScriptedGame(true)
	game.detailFlicker = true
	h := newTraversalHarness(t, game)
	h.start()
	snap := h.endFailed(collect.CodeDetailUnattributed)
	if snap.Counts.Captured != 0 {
		t.Fatalf("an unsettled detail admits nothing: %+v", snap.Counts)
	}
	if clicks := countOps(game.opsSnapshot(), platform.InputClick, image.Point{}, image.Point{}); clicks != 1 {
		t.Fatalf("%d clicks, want 1", clicks)
	}
}

// T16: an undecidable rarity stops unresolved and keeps the position
// unresolved; nothing is admitted.
func TestSinglePageQualityUnknownStopsUnresolved(t *testing.T) {
	game := newScriptedGame(true, true)
	game.qualityUnknown = true
	h := newTraversalHarness(t, game)
	h.start()
	snap := h.endFailed(collect.CodeDetailUnresolved)
	if snap.Counts.Captured != 0 {
		t.Fatalf("an unknown rarity admits nothing: %+v", snap.Counts)
	}
}

// T17: an undecidable fourth-substat row count stops unresolved.
func TestSinglePageSubstatsUnknownStopsUnresolved(t *testing.T) {
	game := newScriptedGame(true, true)
	game.substatsUnknown = true
	h := newTraversalHarness(t, game)
	h.start()
	snap := h.endFailed(collect.CodeDetailUnresolved)
	if snap.Counts.Captured != 0 {
		t.Fatalf("an unknown substat count admits nothing: %+v", snap.Counts)
	}
}

// notSMatcher flips a settled S detail to the calibrated NotS verdict,
// the skip path the 4b-2 rarity classifier will produce. The pixel
// matcher itself only knows S and unknown.
type notSMatcher struct {
	collect.Matcher
}

func (m notSMatcher) Match(f platform.Frame) collect.PageMatch {
	pm := m.Matcher.Match(f)
	if pm.Detail != nil && pm.Detail.Quality == collect.QualityS {
		pm.Detail.Quality = collect.QualityNotS
	}
	return pm
}

// T18: a calibrated NotS disc is marked visited and discarded without a
// record; the walk continues and completes with zero admissions.
func TestSinglePageNotSSkipsWithoutAdmission(t *testing.T) {
	game := newScriptedGame(true, true, true)
	h := newTraversalHarness(t, game)
	h.inv.Matcher = notSMatcher{Matcher: h.inv.Matcher}
	h.start()
	h.endCompleted()
	if got := len(h.observations()); got != 0 {
		t.Fatalf("a NotS disc admitted a record: %d", got)
	}
	if clicks := countOps(game.opsSnapshot(), platform.InputClick, image.Point{}, image.Point{}); clicks != 3 {
		t.Fatalf("%d clicks, want 3", clicks)
	}
}

// T19: precheck refusals start with the right code and dispatch nothing.
func TestSinglePagePrecheckRefusals(t *testing.T) {
	cases := []struct {
		name string
		set  func(*scriptedGame, *collect.Inventory)
		code string
	}{
		{"empty layout", func(g *scriptedGame, inv *collect.Inventory) {
			inv.Layout = collect.Layout{}
		}, collect.CodeAnchorsMissing},
		{"wrong content region", func(g *scriptedGame, inv *collect.Inventory) {
			g.content = image.Rect(0, 0, 1600, 1080)
		}, collect.CodeAnchorsMissing},
		{"uncalibrated matcher", func(g *scriptedGame, inv *collect.Inventory) {
			inv.Matcher = nil
		}, collect.CodePageUnverified},
		{"undecidable page", func(g *scriptedGame, inv *collect.Inventory) {
			g.page = pageUnknown
		}, collect.CodePageUnverified},
		{"filter panel open", func(g *scriptedGame, inv *collect.Inventory) {
			g.page = pagePanel
		}, collect.CodePageUnverified},
		{"filter active", func(g *scriptedGame, inv *collect.Inventory) {
			g.filter = "active"
		}, collect.CodePageUnverified},
		{"undecidable funnel", func(g *scriptedGame, inv *collect.Inventory) {
			g.filter = "unknown"
		}, collect.CodePageUnverified},
		{"mid-list start", func(g *scriptedGame, inv *collect.Inventory) {
			g.globalRows = 9
			g.viewport = 1
		}, collect.CodePageUnverified},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			game := newScriptedGame(make([]bool, 20)...)
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

// T20: the observed scope reports what precheck actually established.
func TestSinglePageObservedScope(t *testing.T) {
	game := newScriptedGame(true)
	h := newTraversalHarness(t, game)
	h.start()
	h.endCompleted()
	scope := string(h.inv.ObservedScope())
	for _, want := range []string{`"kind":"zzzInventory"`, `"startPosition":1`, `"filterAtStart":"clear"`, `"sort":"unverified"`} {
		if !strings.Contains(scope, want) {
			t.Fatalf("observed scope %s lacks %s", scope, want)
		}
	}
}

// T21: a geometry change under the anchors stops the walk; the admitted
// past stays.
func TestSinglePageGeometryChangeStops(t *testing.T) {
	game := newScriptedGame(true, true)
	game.geometryAfter = 4 // during the first list wait
	h := newTraversalHarness(t, game)
	h.start()
	snap := h.endFailed(platform.CodeGeometryChanged)
	if snap.Counts.Captured != 0 {
		t.Fatalf("the change arrived before any admission: %+v", snap.Counts)
	}
}

// T22: a false bottom claim is caught by the stationary confirmation
// drag; the machine does not complete on the scrollbar alone.
func TestSinglePageBottomConfirmationMustBeStationary(t *testing.T) {
	items := make([]bool, 36)
	for i := range items {
		items[i] = true
	}
	game := newScriptedGame(items...)
	game.forceBottom = true
	h := newTraversalHarness(t, game)
	h.start()
	snap := h.endFailed(collect.CodeNavigationMismatch)
	if snap.Counts.Captured != 20 {
		t.Fatalf("the first viewport's admissions must stand: %+v", snap.Counts)
	}
	if drags := countOps(game.opsSnapshot(), platform.InputDrag, game.layout.ScrollFrom, game.layout.ScrollTo); drags != 1 {
		t.Fatalf("%d advance drags, want the failed bottom confirmation", drags)
	}
}

// T23: a list that jumps by ten rows has no verifiable overlap within the
// search radius; the position is lost, not guessed.
func TestSinglePageScrollLostStops(t *testing.T) {
	items := make([]bool, 120) // thirty rows
	for i := range items {
		items[i] = true
	}
	game := newScriptedGame(items...)
	game.scrollJump = 10
	h := newTraversalHarness(t, game)
	h.start()
	snap := h.endFailed(collect.CodePositionLost)
	if snap.Counts.Captured != 20 {
		t.Fatalf("the first viewport's admissions must stand: %+v", snap.Counts)
	}
}

// ambiguousMatcher injects a displacement without correspondences.
type ambiguousMatcher struct {
	collect.Matcher
}

func (m ambiguousMatcher) MatchScroll(before, after platform.Frame) collect.ScrollMatch {
	sm := m.Matcher.MatchScroll(before, after)
	if sm.Displaced {
		sm.Matches = nil
	}
	return sm
}

// raggedMatcher drops one correspondence from a verified overlap.
type raggedMatcher struct {
	collect.Matcher
}

func (m raggedMatcher) MatchScroll(before, after platform.Frame) collect.ScrollMatch {
	sm := m.Matcher.MatchScroll(before, after)
	if sm.Displaced && len(sm.Matches) > 1 {
		sm.Matches = sm.Matches[:len(sm.Matches)-1]
	}
	return sm
}

func TestSinglePageScrollAmbiguityStopsPositionLost(t *testing.T) {
	items := make([]bool, 36)
	for i := range items {
		items[i] = true
	}
	game := newScriptedGame(items...)
	h := newTraversalHarness(t, game)
	h.inv.Matcher = ambiguousMatcher{Matcher: h.inv.Matcher}
	h.start()
	h.endFailed(collect.CodePositionLost)
}

func TestSinglePageRaggedCorrespondenceStops(t *testing.T) {
	items := make([]bool, 36)
	for i := range items {
		items[i] = true
	}
	game := newScriptedGame(items...)
	h := newTraversalHarness(t, game)
	h.inv.Matcher = raggedMatcher{Matcher: h.inv.Matcher}
	h.start()
	h.endFailed(collect.CodePositionLost)
}

// T24: a stuck advance drag is not an end; it is retried to the bound and
// stops with navigationMismatch.
func TestSinglePageStuckScrollIsNotAnEnd(t *testing.T) {
	items := make([]bool, 36)
	for i := range items {
		items[i] = true
	}
	game := newScriptedGame(items...)
	game.scrollStuck = true
	h := newTraversalHarness(t, game)
	h.start()
	snap := h.endFailed(collect.CodeNavigationMismatch)
	if snap.Counts.Captured != 20 {
		t.Fatalf("the first viewport's admissions must stand: %+v", snap.Counts)
	}
	if drags := countOps(game.opsSnapshot(), platform.InputDrag, game.layout.ScrollFrom, game.layout.ScrollTo); drags != 3 {
		t.Fatalf("%d advance drags, want 3 (1 + 2 retries)", drags)
	}
}

// T25: an aborted advance drag unwinds; the admitted past stays.
func TestSinglePageAbortedScrollDragUnwinds(t *testing.T) {
	items := make([]bool, 36)
	for i := range items {
		items[i] = true
	}
	game := newScriptedGame(items...)
	game.abortScrollDrag = true
	h := newTraversalHarness(t, game)
	h.start()
	snap := h.endFailed(platform.CodePermitClosed)
	if snap.Counts.Captured != 20 {
		t.Fatalf("the first viewport's admissions must stand: %+v", snap.Counts)
	}
	ops := game.opsSnapshot()
	if len(ops) == 0 || ops[len(ops)-1].Kind != platform.InputDrag {
		t.Fatalf("the last operation must be the aborted drag: %+v", ops)
	}
}

// T26: a platform refusal carries its own code into the session trigger.
func TestSinglePageDeniedDispatchStopsWithCode(t *testing.T) {
	game := newScriptedGame(true, true)
	game.denyNextDispatch = true
	h := newTraversalHarness(t, game)
	h.start()
	h.endFailed(platform.CodeFocusLost)
}

// T27: a user stop while the machine waits for pipeline capacity unwinds
// promptly and dispatches nothing further.
func TestSinglePageStopCancelsPromptly(t *testing.T) {
	game := newScriptedGame(true, true, true)
	rec := &gatedRecognizer{arrived: make(chan scan.Seq, 4), release: make(chan struct{})}
	h := newTraversalHarness(t, game)
	h.Recognizer = rec
	h.Capacity = 1
	h.start()

	select {
	case <-rec.arrived:
	case <-time.After(5 * time.Second):
		t.Fatal("the first item never reached the recognizer")
	}
	clicksBefore := countOps(game.opsSnapshot(), platform.InputClick, image.Point{}, image.Point{})
	if clicksBefore != 1 {
		t.Fatalf("clicks before the stop: %d, want 1", clicksBefore)
	}
	started := time.Now()
	h.kernel.Cancel()
	close(rec.release)
	snap := h.end()
	if snap.End != scan.EndCancelled {
		t.Fatalf("end %s, want cancelled", snap.End)
	}
	if d := time.Since(started); d > 3*time.Second {
		t.Fatalf("the stop must unwind promptly, took %s", d)
	}
	time.Sleep(20 * time.Millisecond)
	if after := countOps(game.opsSnapshot(), platform.InputClick, image.Point{}, image.Point{}); after != clicksBefore {
		t.Fatalf("the stopped machine dispatched further: %d → %d", clicksBefore, after)
	}
}

// T28: a full pipeline stops the next click; the machine waits for
// capacity instead of clicking ahead.
func TestSinglePageWaitsForPipelineCapacity(t *testing.T) {
	game := newScriptedGame(true, true)
	rec := &gatedRecognizer{arrived: make(chan scan.Seq, 4), release: make(chan struct{})}
	h := newTraversalHarness(t, game)
	h.Recognizer = rec
	h.Capacity = 1
	h.start()

	select {
	case <-rec.arrived:
	case <-time.After(5 * time.Second):
		t.Fatal("the first item never reached the recognizer")
	}
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if clicks := countOps(game.opsSnapshot(), platform.InputClick, image.Point{}, image.Point{}); clicks > 1 {
			t.Fatalf("clicked ahead of a full pipeline: %d clicks", clicks)
		}
		time.Sleep(2 * time.Millisecond)
	}
	close(rec.release)
	snap := h.endCompleted()
	if snap.Counts.Captured != 2 || len(h.observations()) != 2 {
		t.Fatalf("end %s, captured %+v, observations %d; want completed with 2", snap.End, snap.Counts, len(h.observations()))
	}
}
