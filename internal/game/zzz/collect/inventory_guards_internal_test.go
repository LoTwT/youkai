package collect

import "testing"

func TestSelectionMovedAfterVisit(t *testing.T) {
	run := &inventoryRun{
		selectionTracked: true,
		listMatch:        PageMatch{List: &ListEvidence{Selected: 3}},
	}
	if run.selectionMoved(PageMatch{List: &ListEvidence{Selected: 3}}) {
		t.Fatal("an unchanged selection was treated as a moved list")
	}
	if !run.selectionMoved(PageMatch{List: &ListEvidence{Selected: 2}}) {
		t.Fatal("a changed selection was not detected")
	}
	if !run.selectionMoved(PageMatch{List: &ListEvidence{Selected: -1}}) {
		t.Fatal("a vanished selection was not detected")
	}
}

func TestSelectionMovedNeedsTrackedTargetAndPreviousSelection(t *testing.T) {
	run := &inventoryRun{listMatch: PageMatch{List: &ListEvidence{Selected: 3}}}
	if run.selectionMoved(PageMatch{List: &ListEvidence{Selected: 2}}) {
		t.Fatal("the first target was guarded without a prior visit")
	}

	run.selectionTracked = true
	run.listMatch.List.Selected = -1
	if run.selectionMoved(PageMatch{List: &ListEvidence{Selected: 2}}) {
		t.Fatal("an unknown previous selection was treated as a shift")
	}
}

func TestPositionListVerdictRequiresThePreparedWheel(t *testing.T) {
	run := &inventoryRun{inv: &Inventory{Layout: MacCloudLayout()}, position: 2}
	m := PageMatch{
		Page:  PageWarehouse,
		List:  &ListEvidence{Occupied: make([]bool, len(MacCloudLayout().Slots))},
		Wheel: &WheelEvidence{Position: 2},
	}
	if !run.positionListVerdict(m) {
		t.Fatal("the prepared wheel was rejected")
	}
	m.Wheel.Position = 3
	if run.positionListVerdict(m) {
		t.Fatal("a different wheel was accepted")
	}
	m.Wheel = nil
	if run.positionListVerdict(m) {
		t.Fatal("a missing wheel was accepted")
	}
}
