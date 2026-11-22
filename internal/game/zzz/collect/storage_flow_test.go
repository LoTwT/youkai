package collect_test

import (
	"context"
	"encoding/json"
	"image"
	"image/color"
	"sync"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/game/zzz/collect"
	"github.com/LoTwT/youkai/internal/platform"
	"github.com/LoTwT/youkai/internal/platform/platformfake"
	"github.com/LoTwT/youkai/internal/scan"
)

// storageFlowGame is a deterministic state-machine fake for the drive
// storage page. It does not render pixels: the real collection state
// machine and scan kernel run, while this fake supplies page/detail/scroll
// evidence and records every input. The separate pixel and matcher tests
// cover the calibrated page evidence.
type storageFlowGame struct {
	mu sync.Mutex

	layout collect.Layout
	rows   int
	total  int
	maxTop int

	page     int // 0 list, 1 panel
	filter   collect.FilterState
	viewport int
	selected int
	detail   int

	ops              []platform.InputOp
	clickedPositions []int
	frames           map[uint64]storageFlowSnapshot
	nextID           uint64
	images           map[int]*image.RGBA
}

type storageFlowSnapshot struct {
	page, viewport, selected, detail int
	filter                           collect.FilterState
}

func newStorageFlowGame() *storageFlowGame {
	l := collect.StorageLayout()
	totalRows := 6
	g := &storageFlowGame{
		layout:   l,
		rows:     4,
		total:    totalRows,
		maxTop:   totalRows - 4,
		filter:   collect.FilterClear,
		selected: 0,
		detail:   0,
		frames:   map[uint64]storageFlowSnapshot{},
		images:   map[int]*image.RGBA{},
	}
	return g
}

func (g *storageFlowGame) cardSlot(global int) int { return global%6 + 1 }

func (g *storageFlowGame) globalAt(row, col int) int {
	return (g.viewport+row)*g.layout.Columns + col
}

func (g *storageFlowGame) slotAt(p image.Point) int {
	for i, r := range g.layout.Slots {
		if p.In(r) {
			return i
		}
	}
	return -1
}

func (g *storageFlowGame) observe() (platform.Observation, error) {
	return platform.Observation{At: time.Now(), Ready: true, Geometry: 1}, nil
}

func (g *storageFlowGame) frame(context.Context, platform.FrameRequest) (platform.Frame, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.nextID++
	snap := storageFlowSnapshot{
		page: g.page, viewport: g.viewport, selected: g.selected,
		detail: g.detail, filter: g.filter,
	}
	g.frames[g.nextID] = snap
	img := g.images[g.viewport]
	if img == nil {
		img = image.NewRGBA(image.Rect(0, 0, 1920, 1080))
		img.SetRGBA(0, 0, color.RGBA{R: uint8(g.viewport + 1), G: uint8(g.viewport + 1), B: uint8(g.viewport + 1), A: 255})
		g.images[g.viewport] = img
	}
	return platform.Frame{
		ID: g.nextID, Geometry: 1, Image: img, Content: g.layout.Content,
		ArrivedAt: time.Now(),
	}, nil
}

func (g *storageFlowGame) dispatch(op platform.InputOp) platform.InputResult {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.ops = append(g.ops, op)
	now := time.Now()
	switch op.Kind {
	case platform.InputClick:
		if g.page == 1 {
			switch {
			case op.At == g.layout.PanelReset:
				g.filter = collect.FilterClear
			case op.At == g.layout.PanelS:
				g.filter = collect.FilterActive
			case op.At == g.layout.PanelClose:
				g.page = 0
			}
		} else {
			switch {
			case op.At == g.layout.FilterButton:
				g.page = 1
			default:
				if slot := g.slotAt(op.At); slot >= 0 {
					global := g.globalAt(slot/g.layout.Columns, slot%g.layout.Columns)
					g.selected = global
					g.detail = global
					g.clickedPositions = append(g.clickedPositions, g.cardSlot(global))
				}
			}
		}
	case platform.InputScroll:
		if g.page == 0 && op.At == g.layout.ScrollFrom {
			switch {
			case op.WheelSteps < 0:
				g.viewport += -op.WheelSteps
			case op.WheelSteps > 0:
				g.viewport -= op.WheelSteps
			}
			if g.viewport < 0 {
				g.viewport = 0
			}
			if g.viewport > g.maxTop {
				g.viewport = g.maxTop
			}
		}
	}
	return platform.InputResult{Status: platform.InputPosted, StartedAt: now, FinishedAt: now}
}

func (g *storageFlowGame) match(frame platform.Frame) collect.PageMatch {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.page == 1 {
		return collect.PageMatch{
			Page: collect.PageFilterPanel,
			Panel: &collect.PanelEvidence{
				Range: &collect.PanelRangeEvidence{
					ResetVerified: g.filter == collect.FilterClear,
					RarityS:       g.filter,
				},
				SortLabel:    "稀有度",
				SortVerified: true,
			},
			Evidence: "scripted drive-storage panel",
		}
	}
	l := g.layout
	occupied := make([]bool, len(l.Slots))
	positions := make([]collect.PositionVerdict, len(l.Slots))
	management := make([]collect.ManagementState, len(l.Slots))
	avatars := make([]collect.AvatarState, len(l.Slots))
	for i := range occupied {
		global := g.viewport*l.Columns + i
		if global >= g.total*l.Columns {
			continue
		}
		occupied[i] = true
		positions[i].Position = g.cardSlot(global)
	}
	selected := -1
	if g.selected >= g.viewport*l.Columns && g.selected < (g.viewport+g.rows)*l.Columns {
		selected = g.selected - g.viewport*l.Columns
	}
	var detail *collect.DetailEvidence
	if g.detail >= 0 {
		detail = &collect.DetailEvidence{
			Position: collect.PositionVerdict{Position: g.cardSlot(g.detail)},
			Quality:  collect.QualityS,
			Substats: 4,
		}
	}
	return collect.PageMatch{
		Page: collect.PageDriveStorage,
		List: &collect.ListEvidence{
			Occupied: occupied, Positions: positions, Selected: selected,
			Management: management, Avatars: avatars,
			Top: g.viewport == 0, Bottom: g.viewport >= g.maxTop,
		},
		Detail:   detail,
		Filter:   &collect.FilterEvidence{State: g.filter},
		Evidence: "scripted drive-storage list",
	}
}

func (g *storageFlowGame) matchScroll(before, after platform.Frame) collect.ScrollMatch {
	g.mu.Lock()
	defer g.mu.Unlock()
	b, bok := g.frames[before.ID]
	a, aok := g.frames[after.ID]
	if !bok || !aok || b.page != 0 || a.page != 0 || b.viewport == a.viewport {
		return collect.ScrollMatch{Stationary: true, Evidence: "scripted stationary"}
	}
	delta := a.viewport - b.viewport
	rows := delta
	if rows < 0 {
		rows = -rows
	}
	matches := make([]collect.PositionPair, 0)
	for r := 0; r < g.rows-rows; r++ {
		for c := 0; c < g.layout.Columns; c++ {
			if delta > 0 {
				matches = append(matches, collect.PositionPair{
					FromRow: r + rows, FromColumn: c, ToRow: r, ToColumn: c,
				})
			} else {
				matches = append(matches, collect.PositionPair{
					FromRow: r, FromColumn: c, ToRow: r + rows, ToColumn: c,
				})
			}
		}
	}
	return collect.ScrollMatch{Displaced: true, Rows: rows, Matches: matches, Evidence: "scripted displacement"}
}

func (g *storageFlowGame) compareDetail(before, after platform.Frame) collect.DetailComparison {
	g.mu.Lock()
	defer g.mu.Unlock()
	b, bok := g.frames[before.ID]
	a, aok := g.frames[after.ID]
	if !bok || !aok || b.detail == a.detail {
		return collect.DetailSame
	}
	return collect.DetailDifferent
}

type storageFlowMatcher struct{ game *storageFlowGame }

func (m storageFlowMatcher) Match(f platform.Frame) collect.PageMatch { return m.game.match(f) }
func (m storageFlowMatcher) MatchScroll(b, a platform.Frame) collect.ScrollMatch {
	return m.game.matchScroll(b, a)
}
func (m storageFlowMatcher) CompareDetail(b, a platform.Frame) collect.DetailComparison {
	return m.game.compareDetail(b, a)
}

func (g *storageFlowGame) opsSnapshot() []platform.InputOp {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]platform.InputOp(nil), g.ops...)
}

func (g *storageFlowGame) clickedSnapshot() []int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]int(nil), g.clickedPositions...)
}

type storageFlowScope struct {
	Sort      string `json:"sort"`
	Coverage  string `json:"coverage"`
	Positions []struct {
		Position  int  `json:"position"`
		Viewports int  `json:"viewports"`
		Visits    int  `json:"visits"`
		Top       bool `json:"topVerified"`
		Bottom    bool `json:"bottomVerified"`
		Complete  bool `json:"complete"`
		Range     struct {
			Verified bool `json:"verified"`
		} `json:"range"`
		Ledger *struct {
			Slots []struct {
				AbsoluteRow int    `json:"absoluteRow"`
				Column      int    `json:"column"`
				Position    int    `json:"position"`
				State       string `json:"state"`
				Sequence    int    `json:"sequence"`
			} `json:"slots"`
		} `json:"slotLedger"`
	} `json:"positions"`
}

func runStorageFlow(t *testing.T, positions []int) (*storageFlowGame, []collect.Observation, storageFlowScope) {
	t.Helper()
	game := newStorageFlowGame()
	profile, err := collect.WindowsStorageProfileFor(image.Pt(1920, 1080))
	if err != nil {
		t.Fatal(err)
	}
	inv := profile.Inventory(false)
	inv.Matcher = storageFlowMatcher{game: game}
	inv.ActionBudget = 300 * time.Millisecond
	inv.ScrollBudget = 300 * time.Millisecond
	inv.ScrollTiming = &collect.ScrollTiming{
		MotionStartBudget:  40 * time.Millisecond,
		SettleBudget:       100 * time.Millisecond,
		SettleObservations: 2,
	}
	inv.StableFrames = 3
	inv.DetailStableSpan = -1
	inv.MaxRetries = 2
	inv.RecheckEvery = time.Millisecond
	if len(positions) > 0 {
		if err := inv.SetScanPositions(positions); err != nil {
			t.Fatal(err)
		}
	}
	sess := &platformfake.Session{
		ObserveFn:  game.observe,
		FrameFn:    game.frame,
		DispatchFn: game.dispatch,
	}
	port := &platformfake.Port{NewSession: func() *platformfake.Session { return sess }}
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
		ID:         scan.ID("storage-flow-" + time.Now().Format("150405.000000000")),
		Capacity:   2,
		Workers:    1,
		Controller: ctrl,
		Recognizer: fastRecognizer{},
		Deliverer:  sink,
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-kernel.Done():
	case <-time.After(60 * time.Second):
		t.Fatalf("storage flow timed out; ops=%d", len(game.opsSnapshot()))
	}
	if snap := kernel.Snapshot(); snap.End != scan.EndCompleted {
		t.Fatalf("storage flow end = %s trigger=%+v", snap.End, snap.Trigger)
	}
	observations := make([]collect.Observation, 0, sink.len())
	for _, rec := range sink.recordsSnapshot() {
		obs, ok := rec.Observation.(collect.Observation)
		if !ok {
			t.Fatalf("record observation = %T", rec.Observation)
		}
		observations = append(observations, obs)
	}
	var scope storageFlowScope
	if err := json.Unmarshal(inv.ObservedScope(), &scope); err != nil {
		t.Fatalf("decode observed scope: %v", err)
	}
	return game, observations, scope
}

func TestStorageNormalEntryActionSequence(t *testing.T) {
	game, observations, scope := runStorageFlow(t, nil)
	if len(observations) != 54 || len(game.clickedSnapshot()) != 53 {
		t.Fatalf("full flow admitted %d items with %d card clicks, want 54/53",
			len(observations), len(game.clickedSnapshot()))
	}
	ops := game.opsSnapshot()
	wantPrefix := []image.Point{
		game.layout.FilterButton,
		game.layout.PanelReset,
		game.layout.PanelS,
		game.layout.PanelClose,
	}
	if len(ops) < 5 {
		t.Fatalf("full flow dispatched only %d ops", len(ops))
	}
	for i, want := range wantPrefix {
		if ops[i].Kind != platform.InputClick || ops[i].At != want {
			t.Fatalf("prepare op %d = %s at %v, want click at %v", i, ops[i].Kind, ops[i].At, want)
		}
	}
	if ops[4].Kind != platform.InputScroll || ops[4].WheelSteps <= 0 {
		t.Fatalf("first post-prepare op = %+v, want a wheel toward the top", ops[4])
	}
	scrolls := 0
	for _, op := range ops {
		if op.Kind == platform.InputDrag {
			t.Fatalf("drive-storage flow dispatched a drag: %+v", op)
		}
		if op.Kind == platform.InputScroll && op.WheelSteps < 0 {
			scrolls++
		}
	}
	if scrolls < 2 {
		t.Fatalf("full flow dispatched %d downward wheels, want at least two", scrolls)
	}
	seenSlots := map[int]bool{}
	for _, position := range game.clickedSnapshot() {
		seenSlots[position] = true
	}
	for slot := 1; slot <= 6; slot++ {
		if !seenSlots[slot] {
			t.Fatalf("full flow never clicked slot %d: %v", slot, seenSlots)
		}
	}
	for _, obs := range observations {
		global := obs.Viewport*game.layout.Columns + obs.Row*game.layout.Columns + obs.Column
		if obs.Page != collect.PageDriveStorage {
			t.Fatalf("observation page = %q, want drive storage", obs.Page)
		}
		if obs.Position != game.cardSlot(global) {
			t.Fatalf("observation at viewport %d row %d col %d says slot %d, want %d",
				obs.Viewport, obs.Row, obs.Column, obs.Position, game.cardSlot(global))
		}
		if obs.RarityFilter != "S" || obs.Quality != collect.QualityS {
			t.Fatalf("observation rarity = %q/%q, want S/S", obs.RarityFilter, obs.Quality)
		}
	}
	if scope.Sort != "verified" || scope.Coverage != "complete" {
		t.Fatalf("full scope sort/coverage = %q/%q, want verified/complete", scope.Sort, scope.Coverage)
	}
	if len(scope.Positions) != 6 {
		t.Fatalf("full scope has %d positions, want 6", len(scope.Positions))
	}
	for i, pos := range scope.Positions {
		if pos.Position != i+1 || !pos.Complete || !pos.Top || !pos.Bottom || !pos.Range.Verified || pos.Visits != 9 {
			t.Fatalf("position %d = %+v, want complete/top/bottom/range with 9 visits", i+1, pos)
		}
		if pos.Ledger == nil || len(pos.Ledger.Slots) != 9 {
			t.Fatalf("position %d ledger = %+v, want 9 item cells", i+1, pos.Ledger)
		}
	}
}

func TestStorageScopedRescanActionSequence(t *testing.T) {
	game, observations, scope := runStorageFlow(t, []int{3})
	if len(observations) != 9 || len(game.clickedSnapshot()) != 9 {
		t.Fatalf("scoped flow admitted %d items with %d card clicks, want 9/9",
			len(observations), len(game.clickedSnapshot()))
	}
	for _, obs := range observations {
		if obs.Position != 3 {
			t.Fatalf("scoped observation slot = %d, want 3", obs.Position)
		}
	}
	for _, slot := range game.clickedSnapshot() {
		if slot != 3 {
			t.Fatalf("scoped flow clicked out-of-scope slot %d", slot)
		}
	}
	if scope.Coverage != "partial" || scope.Sort != "verified" || len(scope.Positions) != 1 ||
		scope.Positions[0].Position != 3 || !scope.Positions[0].Complete {
		t.Fatalf("scoped scope = %+v, want one complete position and partial coverage", scope)
	}
	ledger := scope.Positions[0].Ledger
	if ledger == nil || len(ledger.Slots) != 54 {
		t.Fatalf("scoped ledger = %+v, want the full 9x6 observed grid", ledger)
	}
	out, empty, admitted := 0, 0, 0
	for _, cell := range ledger.Slots {
		switch cell.State {
		case "outOfScope":
			out++
			if cell.Position == 3 {
				t.Fatalf("in-scope cell marked outOfScope: %+v", cell)
			}
		case "empty":
			empty++
		case "admitted":
			admitted++
			if cell.Position != 3 {
				t.Fatalf("admitted out-of-scope cell: %+v", cell)
			}
		}
	}
	if out != 45 || empty != 0 || admitted != 9 {
		t.Fatalf("scoped ledger counts out/empty/admitted = %d/%d/%d, want 45/0/9", out, empty, admitted)
	}
}
