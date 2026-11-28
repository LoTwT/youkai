package collect

import (
	"errors"
	"testing"

	"github.com/LoTwT/youkai/internal/platform"
	"github.com/LoTwT/youkai/internal/scan"
)

func TestStorageThreeRowWheelInputSeparation(t *testing.T) {
	run := inventoryRun{inv: &Inventory{Layout: StorageLayout()}, geo: 1}
	before := platform.Frame{ID: 7}
	if got := run.scrollInputOp(before); got.Kind != platform.InputScroll || got.WheelSteps != -3 || got.At != StorageLayout().ScrollFrom {
		t.Fatalf("normal storage advance = %+v, want a -3 wheel at ScrollFrom", got)
	}
	if got := run.bottomInputOp(before); got.Kind != platform.InputScroll || got.WheelSteps != -1 || got.At != StorageLayout().ScrollFrom {
		t.Fatalf("storage bottom recheck = %+v, want a -1 wheel at ScrollFrom", got)
	}
	if got := run.topInputOp(before); got.Kind != platform.InputScroll || got.WheelSteps != 3 || got.At != StorageLayout().ScrollFrom {
		t.Fatalf("storage top input = %+v, want the unchanged +3 wheel", got)
	}

	legacy := inventoryRun{inv: &Inventory{Layout: MacCloudLayout()}, geo: 1}
	if got := legacy.scrollInputOp(before); got.Kind != platform.InputDrag {
		t.Fatalf("legacy advance = %+v, want the unchanged drag", got)
	}
	if got := legacy.bottomInputOp(before); got.Kind != platform.InputDrag {
		t.Fatalf("legacy bottom = %+v, want the unchanged drag", got)
	}
	if got := legacy.topInputOp(before); got.Kind != platform.InputDrag {
		t.Fatalf("legacy top = %+v, want the unchanged drag", got)
	}
}

func TestStorageThreeRowMatcherWithAndWithoutSelectionAnchor(t *testing.T) {
	l := StorageLayout()
	m := NewScrollEvidenceMatcher(NewPixelMatcher(l, StorageCalibration()))
	for _, tc := range []struct {
		name     string
		selected int
		after    int
	}{
		{name: "with selection anchor", selected: 27, after: 0},
		{name: "without selection anchor", selected: -1, after: -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := storageSelectedPatternFrame(l, 1, 0, tc.selected, 300, 323)
			after := storageSelectedPatternFrame(l, 2, 3, tc.after, 318, 341)
			got := m.MatchScroll(before, after)
			if !got.Displaced || got.Rows != 3 || len(got.Matches) != l.Columns {
				t.Fatalf("three-row match = %+v, want displaced 3 rows with one %d-cell overlap", got, l.Columns)
			}
		})
	}
}

func storageTransitionVerdict(l Layout, occupied []bool) PageMatch {
	positions := make([]PositionVerdict, len(occupied))
	for i := range positions {
		positions[i] = PositionVerdict{Position: 1}
	}
	return PageMatch{Page: PageDriveStorage, List: &ListEvidence{Occupied: occupied, Positions: positions}}
}

func TestStorageThreeRowTransitionKeepsOneRowOverlap(t *testing.T) {
	l := StorageLayout()
	run := newTransitionRun(l)
	before := storageTransitionVerdict(l, gridOccupancy(l, true))
	run.ledger = newSlotLedgerWithPositions(l.Columns, before.List.Occupied, make([]int, len(before.List.Occupied)), 1)
	sm := ScrollMatch{
		Displaced: true, Rows: 3,
		Matches:  scriptedDisplacement(l, 3),
		Evidence: "three rows with exactly one row of overlap",
	}
	if len(sm.Matches) != l.Columns {
		t.Fatalf("overlap pairs = %d, want one row of %d cells", len(sm.Matches), l.Columns)
	}
	if err := run.applyTransition(platform.Frame{ID: 1}, platform.Frame{ID: 2}, before, before, sm); err != nil {
		t.Fatalf("the three-row overlap was rejected: %v", err)
	}
	if run.viewport != 1 {
		t.Fatalf("viewport = %d, want one committed advance", run.viewport)
	}
}

func rejectStorageTransition(t *testing.T, run *inventoryRun, before PageMatch, sm ScrollMatch, why string) {
	t.Helper()
	run.ledger = newSlotLedgerWithPositions(run.inv.Layout.Columns, before.List.Occupied, make([]int, len(before.List.Occupied)), 1)
	beforeViewport := run.viewport
	err := run.applyTransition(platform.Frame{ID: 1}, platform.Frame{ID: 2}, before, before, sm)
	if err == nil {
		t.Fatalf("%s was accepted", why)
	}
	var fault *scan.FaultError
	if !errors.As(err, &fault) || fault.Code != CodePositionLost {
		t.Fatalf("%s returned %v, want positionLost", why, err)
	}
	if run.viewport != beforeViewport {
		t.Fatalf("%s advanced the viewport", why)
	}
}

func TestStorageThreeRowTransitionRejectsUnsafeCorrespondence(t *testing.T) {
	l := StorageLayout()
	before := storageTransitionVerdict(l, gridOccupancy(l, true))
	for _, tc := range []struct {
		name string
		rows int
		make func() []PositionPair
	}{
		{
			name: "over four rows",
			rows: 4,
			make: func() []PositionPair { return scriptedDisplacement(l, 4) },
		},
		{
			name: "duplicate correspondence",
			rows: 2,
			make: func() []PositionPair {
				m := scriptedDisplacement(l, 2)
				return append(m, m[len(m)-1])
			},
		},
		{
			name: "missing overlap cell",
			rows: 2,
			make: func() []PositionPair {
				m := scriptedDisplacement(l, 2)
				return m[1:]
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rejectStorageTransition(t, newTransitionRun(l), before, ScrollMatch{
				Displaced: true, Rows: tc.rows, Matches: tc.make(), Evidence: tc.name,
			}, tc.name)
		})
	}
}
