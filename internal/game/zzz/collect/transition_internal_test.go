package collect

import (
	"errors"
	"fmt"
	"math/rand"
	"slices"
	"testing"

	"github.com/LoTwT/youkai/internal/platform"
	"github.com/LoTwT/youkai/internal/scan"
)

// applyTransition is the ledger's last line of defense against a matcher
// that commits a wrong displacement (T5 §5, decision §4.8's 不猜滚动行数 and
// 每个观测一个结局): a correspondence set is accepted only when it is
// one-to-one, keeps columns, agrees with the verified row count, preserves
// occupancy and leaves no overlap position unaccounted for. The tests here
// pin each guard on its own, in both directions, so removing one of them
// cannot silently go unnoticed.

// newTransitionRun builds the minimal run applyTransition reads: the layout
// under which the correspondence is validated, the per-slot visited ledger
// and the viewport index. No view, matcher or frame pipeline is involved.
func newTransitionRun(l Layout) *inventoryRun {
	return &inventoryRun{
		inv:     &Inventory{Layout: l},
		visited: make([]bool, len(l.Slots)),
	}
}

// gridOccupancy fills every slot of the layout's grid with the same
// occupancy verdict.
func gridOccupancy(l Layout, occupied bool) []bool {
	occ := make([]bool, len(l.Slots))
	for i := range occ {
		occ[i] = occupied
	}
	return occ
}

// transitionListVerdict is the list evidence a stable warehouse frame
// reports for the given occupancy. applyTransition consumes only
// List.Occupied; the rest of the page verdict stays absent.
func transitionListVerdict(l Layout, occupied []bool) PageMatch {
	return PageMatch{Page: PageWarehouse, List: &ListEvidence{Occupied: occupied}}
}

// rejectTransition applies a transition that must be refused and asserts
// both halves of the fail-closed contract: the fault code is positionLost,
// and the rejected scroll left the ledger untouched (a rejected transition
// must not advance the viewport or carry visit state across a position it
// could not prove).
func rejectTransition(t *testing.T, run *inventoryRun, before, after PageMatch, sm ScrollMatch) {
	t.Helper()
	err := run.applyTransition(platform.Frame{ID: 1}, platform.Frame{ID: 2}, before, after, sm)
	if err == nil {
		t.Fatal("a broken correspondence was accepted as a verified scroll")
	}
	var fault *scan.FaultError
	if !errors.As(err, &fault) || fault.Code != CodePositionLost {
		t.Fatalf("correspondence error %v, want the fail-closed %s", err, CodePositionLost)
	}
	if run.viewport != 0 {
		t.Fatalf("the rejected transition advanced the viewport to %d", run.viewport)
	}
	if slices.Contains(run.visited, true) {
		t.Fatalf("the rejected transition carried visit state: %v", run.visited)
	}
}

// TestApplyTransitionAcceptsCompleteCorrespondence is the positive
// counterpart: every verified whole-row displacement from one row up to the
// last row the grid allows is accepted, and the committed scroll advances
// the viewport exactly once.
func TestApplyTransitionAcceptsCompleteCorrespondence(t *testing.T) {
	l := MacCloudLayout()
	for _, rows := range []int{1, 2, l.Rows - 1} {
		t.Run(fmt.Sprintf("%d rows", rows), func(t *testing.T) {
			run := newTransitionRun(l)
			sm := ScrollMatch{
				Displaced: true,
				Rows:      rows,
				Matches:   scriptedDisplacement(l, rows),
				Evidence:  "complete correspondence",
			}
			before := transitionListVerdict(l, gridOccupancy(l, true))
			if err := run.applyTransition(platform.Frame{ID: 1}, platform.Frame{ID: 2}, before, before, sm); err != nil {
				t.Fatalf("the complete %d-row correspondence was rejected: %v", rows, err)
			}
			if run.viewport != 1 {
				t.Fatalf("viewport %d after one committed scroll, want 1", run.viewport)
			}
		})
	}
}

// TestApplyTransitionAcceptsConsistentOccupancy pins the positive side of
// the occupancy guard: the same item may be empty on both sides of its
// correspondence, and that is not a change.
func TestApplyTransitionAcceptsConsistentOccupancy(t *testing.T) {
	l := MacCloudLayout()
	before, after := gridOccupancy(l, true), gridOccupancy(l, true)
	before[l.positionSlot(4, 2)] = false
	after[l.positionSlot(3, 2)] = false
	before[l.positionSlot(2, 1)] = false
	after[l.positionSlot(1, 1)] = false
	run := newTransitionRun(l)
	sm := ScrollMatch{Displaced: true, Rows: 1, Matches: scriptedDisplacement(l, 1), Evidence: "consistent occupancy"}
	if err := run.applyTransition(platform.Frame{ID: 1}, platform.Frame{ID: 2},
		transitionListVerdict(l, before), transitionListVerdict(l, after), sm); err != nil {
		t.Fatalf("a correspondence whose corresponding slots are both empty was rejected: %v", err)
	}
}

// TestApplyTransitionCarriesVisitedState pins the bookkeeping the guards
// protect: overlap positions inherit their before-visit state in the same
// row/column, and the after-positions below the overlap start unvisited.
func TestApplyTransitionCarriesVisitedState(t *testing.T) {
	l := MacCloudLayout()
	run := newTransitionRun(l)
	run.viewport = 3
	run.visited[l.positionSlot(2, 0)] = true
	run.visited[l.positionSlot(4, 3)] = true
	before := transitionListVerdict(l, gridOccupancy(l, true))
	sm := ScrollMatch{Displaced: true, Rows: 2, Matches: scriptedDisplacement(l, 2), Evidence: "visited carry"}

	if err := run.applyTransition(platform.Frame{ID: 1}, platform.Frame{ID: 2}, before, before, sm); err != nil {
		t.Fatalf("the complete two-row correspondence was rejected: %v", err)
	}
	want := make([]bool, len(l.Slots))
	want[l.positionSlot(0, 0)] = true // (2,0) moved to (0,0)
	want[l.positionSlot(2, 3)] = true // (4,3) moved to (2,3)
	if !slices.Equal(run.visited, want) {
		t.Fatalf("visited after the scroll %v, want %v", run.visited, want)
	}
	if run.viewport != 4 {
		t.Fatalf("viewport %d after one committed scroll, want 4", run.viewport)
	}
}

// TestApplyTransitionRejectsBrokenCorrespondence pins each ledger guard on
// its own: breaking exactly one condition of an otherwise complete
// correspondence is refused as positionLost, and the bookkeeping stays put.
func TestApplyTransitionRejectsBrokenCorrespondence(t *testing.T) {
	l := MacCloudLayout()
	occupied := func() []bool { return gridOccupancy(l, true) }
	withEmpty := func(slot int) []bool {
		occ := gridOccupancy(l, true)
		occ[slot] = false
		return occ
	}
	cases := []struct {
		name    string
		rows    int
		matches func() []PositionPair
		before  func() []bool
		after   func() []bool
	}{
		{
			// Two pairs claim the same before and after position: one item
			// would be counted twice and another position left unexplained.
			name: "one-to-one: a correspondence is repeated",
			rows: 1,
			matches: func() []PositionPair {
				pairs := scriptedDisplacement(l, 1)
				return append(pairs, pairs[0])
			},
			before: occupied,
			after:  occupied,
		},
		{
			// The columns are still one-to-one and the overlap is complete,
			// but the item would be attributed to the wrong column.
			name: "column: two target columns are swapped",
			rows: 1,
			matches: func() []PositionPair {
				pairs := scriptedDisplacement(l, 1)
				pairs[0].ToColumn, pairs[1].ToColumn = pairs[1].ToColumn, pairs[0].ToColumn
				return pairs
			},
			before: occupied,
			after:  occupied,
		},
		{
			// One pair claims no movement while the scroll was verified as
			// one row: the ledger would inherit the wrong position.
			name: "row displacement: one pair keeps its row",
			rows: 1,
			matches: func() []PositionPair {
				pairs := scriptedDisplacement(l, 1)
				pairs[0] = PositionPair{FromRow: 0, FromColumn: 0, ToRow: 0, ToColumn: 0}
				return pairs
			},
			before: occupied,
			after:  occupied,
		},
		{
			// An item cannot appear or disappear across a verified scroll.
			name:    "occupancy: an occupied position becomes empty",
			rows:    1,
			matches: func() []PositionPair { return scriptedDisplacement(l, 1) },
			before:  occupied,
			after:   func() []bool { return withEmpty(l.positionSlot(0, 2)) },
		},
		{
			name:    "occupancy: an empty position becomes occupied",
			rows:    1,
			matches: func() []PositionPair { return scriptedDisplacement(l, 1) },
			before:  func() []bool { return withEmpty(l.positionSlot(1, 3)) },
			after:   occupied,
		},
		{
			// A zero-row "scroll" is not a displacement; the row count must
			// come from the verified 1..Rows-1 range.
			name:    "guard: zero row displacement",
			rows:    0,
			matches: func() []PositionPair { return scriptedDisplacement(l, 0) },
			before:  occupied,
			after:   occupied,
		},
		{
			// A whole-grid displacement leaves no overlap to verify.
			name:    "guard: whole-grid displacement",
			rows:    l.Rows,
			matches: func() []PositionPair { return scriptedDisplacement(l, l.Rows) },
			before:  occupied,
			after:   occupied,
		},
		{
			name:    "guard: no correspondences",
			rows:    1,
			matches: func() []PositionPair { return nil },
			before:  occupied,
			after:   occupied,
		},
		{
			// A pair outside the grid would make the slot bookkeeping index
			// a position that does not exist.
			name: "guard: pair outside the grid",
			rows: 1,
			matches: func() []PositionPair {
				return append(scriptedDisplacement(l, 1), PositionPair{FromRow: 1, FromColumn: -1, ToRow: 0, ToColumn: -1})
			},
			before: occupied,
			after:  occupied,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			run := newTransitionRun(l)
			sm := ScrollMatch{Displaced: true, Rows: tc.rows, Matches: tc.matches(), Evidence: tc.name}
			rejectTransition(t, run, transitionListVerdict(l, tc.before()), transitionListVerdict(l, tc.after()), sm)
		})
	}
}

// TestApplyTransitionRejectsUncoveredOverlap pins full overlap coverage
// directly: dropping any single correspondence leaves an after-overlap
// position that could hold an old or a new item, and the machine must not
// guess which (T5 §5, decision §4.8's 没有唯一对应就重建位置).
func TestApplyTransitionRejectsUncoveredOverlap(t *testing.T) {
	l := MacCloudLayout()
	cases := []struct {
		name string
		rows int
		drop PositionPair
	}{
		{"middle column of a one-row step", 1, PositionPair{FromRow: 1, FromColumn: 2, ToRow: 0, ToColumn: 2}},
		{"last column of a two-row step", 2, PositionPair{FromRow: 2, FromColumn: 3, ToRow: 0, ToColumn: 3}},
		{"the only overlap column of a full step", l.Rows - 1, PositionPair{FromRow: l.Rows - 1, FromColumn: 1, ToRow: 0, ToColumn: 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pairs := scriptedDisplacement(l, tc.rows)
			kept := make([]PositionPair, 0, len(pairs)-1)
			for _, p := range pairs {
				if p != tc.drop {
					kept = append(kept, p)
				}
			}
			if len(kept) != len(pairs)-1 {
				t.Fatalf("the case dropped %d correspondences, want 1", len(pairs)-len(kept))
			}
			run := newTransitionRun(l)
			sm := ScrollMatch{Displaced: true, Rows: tc.rows, Matches: kept, Evidence: tc.name}
			occupancy := transitionListVerdict(l, gridOccupancy(l, true))
			rejectTransition(t, run, occupancy, occupancy, sm)
		})
	}
}

// randomConsistentOccupancy randomizes the occupancy of every matched
// position while keeping both sides of each correspondence equal, so the
// base transition stays legal.
func randomConsistentOccupancy(l Layout, pairs []PositionPair, rng *rand.Rand) (before, after []bool) {
	before = make([]bool, len(l.Slots))
	after = make([]bool, len(l.Slots))
	for _, p := range pairs {
		occupied := rng.Intn(2) == 0
		before[l.positionSlot(p.FromRow, p.FromColumn)] = occupied
		after[l.positionSlot(p.ToRow, p.ToColumn)] = occupied
	}
	return before, after
}

// brokenTransition is one mutated correspondence set and the occupancy
// verdicts it is validated against.
type brokenTransition struct {
	name          string
	rows          int
	pairs         []PositionPair
	before, after []bool
}

// mutateTransition breaks one random condition of a legal correspondence
// while leaving the other conditions as they were. Every mutation is
// rejected by the production guards, so a property run asserts the
// accept/reject split without re-implementing the validation rules.
func mutateTransition(l Layout, rows int, pairs []PositionPair, before, after []bool, rng *rand.Rand) brokenTransition {
	base := brokenTransition{rows: rows, pairs: pairs, before: before, after: after}
	switch rng.Intn(5) {
	case 0:
		// Duplicate one pair: one-to-one is broken on both sides.
		i := rng.Intn(len(pairs))
		base.name = "repeated correspondence"
		base.pairs = append(append([]PositionPair{}, pairs...), pairs[i])
	case 1:
		// Swap the target columns of two pairs in the same overlap row:
		// one-to-one, coverage and the row displacement still hold, but the
		// items are attributed to the wrong columns.
		row := rng.Intn(len(pairs) / l.Columns)
		from := rng.Intn(l.Columns)
		to := (from + 1 + rng.Intn(l.Columns-1)) % l.Columns
		i, j := row*l.Columns+from, row*l.Columns+to
		swapped := append([]PositionPair{}, pairs...)
		swapped[i].ToColumn, swapped[j].ToColumn = swapped[j].ToColumn, swapped[i].ToColumn
		base.name = "swapped target columns"
		base.pairs = swapped
	case 2:
		// Move one pair off the verified displacement. The pairs whose
		// before-row equals the verified count form the first overlap row;
		// stepping one of them back stays in the grid and one-to-one but
		// disagrees with the row count.
		firstRow := make([]int, 0, l.Columns)
		for i, p := range pairs {
			if p.FromRow == rows {
				firstRow = append(firstRow, i)
			}
		}
		changed := append([]PositionPair{}, pairs...)
		changed[firstRow[rng.Intn(len(firstRow))]].FromRow--
		base.name = "row displacement disagrees"
		base.pairs = changed
	case 3:
		// Flip one target's occupancy: the same item would appear or
		// disappear across the scroll.
		i := rng.Intn(len(pairs))
		flipped := append([]bool{}, after...)
		slot := l.positionSlot(pairs[i].ToRow, pairs[i].ToColumn)
		flipped[slot] = !flipped[slot]
		base.name = "target occupancy flipped"
		base.after = flipped
	default:
		// Drop one correspondence: its overlap position is unexplained.
		i := rng.Intn(len(pairs))
		kept := append(append([]PositionPair{}, pairs[:i]...), pairs[i+1:]...)
		base.name = "overlap position uncovered"
		base.pairs = kept
	}
	return base
}

// TestApplyTransitionCorrespondenceProperty is the randomized counterpart:
// random legal correspondences are accepted, and one random mutation of
// each is rejected. The deterministic seed keeps failures reproducible.
func TestApplyTransitionCorrespondenceProperty(t *testing.T) {
	l := MacCloudLayout()
	rng := rand.New(rand.NewSource(20261031))
	for i := 0; i < 100; i++ {
		rows := 1 + rng.Intn(l.Rows-1)
		pairs := scriptedDisplacement(l, rows)
		before, after := randomConsistentOccupancy(l, pairs, rng)
		sm := ScrollMatch{Displaced: true, Rows: rows, Matches: pairs, Evidence: "property base"}
		run := newTransitionRun(l)
		if err := run.applyTransition(platform.Frame{ID: 1}, platform.Frame{ID: 2},
			transitionListVerdict(l, before), transitionListVerdict(l, after), sm); err != nil {
			t.Fatalf("iteration %d: the legal %d-row correspondence was rejected: %v", i, rows, err)
		}

		broken := mutateTransition(l, rows, pairs, before, after, rng)
		run = newTransitionRun(l)
		err := run.applyTransition(platform.Frame{ID: 1}, platform.Frame{ID: 2},
			transitionListVerdict(l, broken.before), transitionListVerdict(l, broken.after),
			ScrollMatch{Displaced: true, Rows: broken.rows, Matches: broken.pairs, Evidence: broken.name})
		if err == nil {
			t.Fatalf("iteration %d: the %q mutation was accepted as a verified scroll", i, broken.name)
		}
		var fault *scan.FaultError
		if !errors.As(err, &fault) || fault.Code != CodePositionLost {
			t.Fatalf("iteration %d: the %q mutation ended in %v, want %s", i, broken.name, err, CodePositionLost)
		}
	}
}
