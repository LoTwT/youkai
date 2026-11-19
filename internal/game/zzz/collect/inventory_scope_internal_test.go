package collect

import (
	"reflect"
	"testing"
)

func TestSetScanPositionsValidatesAndCopies(t *testing.T) {
	inv := &Inventory{}
	if err := inv.SetScanPositions([]int{2, 4}); err != nil {
		t.Fatalf("SetScanPositions: %v", err)
	}
	if want := []int{2, 4}; !reflect.DeepEqual(inv.positions, want) {
		t.Fatalf("positions = %v, want %v", inv.positions, want)
	}
	// The traversal owns its copy: a caller's later mutation cannot change
	// the session's range.
	positions := []int{2, 4}
	other := &Inventory{}
	if err := other.SetScanPositions(positions); err != nil {
		t.Fatal(err)
	}
	positions[0] = 6
	if want := []int{2, 4}; !reflect.DeepEqual(other.positions, want) {
		t.Fatalf("positions changed through the caller's slice: %v", other.positions)
	}

	for _, bad := range [][]int{nil, {}, {4, 2}, {2, 2}, {0}, {7}} {
		if err := (&Inventory{}).SetScanPositions(bad); err == nil {
			t.Fatalf("SetScanPositions(%v) accepted an invalid range", bad)
		}
	}
}

func TestInventoryPositionOrderDefaultsToAllSix(t *testing.T) {
	order, err := (&Inventory{}).positionOrder()
	if err != nil {
		t.Fatalf("positionOrder: %v", err)
	}
	if want := []int{1, 2, 3, 4, 5, 6}; !reflect.DeepEqual(order, want) {
		t.Fatalf("default order = %v, want %v", order, want)
	}
}
