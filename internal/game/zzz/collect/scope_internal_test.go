package collect

import (
	"encoding/json"
	"testing"
)

func scopeOf(t *testing.T, inv *Inventory) scopePayload {
	t.Helper()
	raw := inv.ObservedScope()
	if len(raw) == 0 {
		t.Fatal("observed scope is empty")
	}
	var scope scopePayload
	if err := json.Unmarshal(raw, &scope); err != nil {
		t.Fatalf("observed scope is not JSON: %v", err)
	}
	return scope
}

func completePositions(inv *Inventory) {
	for i := 1; i <= scopePositionCount; i++ {
		inv.recordPosition(positionScope{
			Position: i, Viewports: 2, Visits: 4, Top: true, Bottom: true, Complete: true,
		})
	}
}

func TestObservedScopeCompletesOnlyWithSortAndLedger(t *testing.T) {
	inv := &Inventory{}
	inv.scope = scopePayload{Sort: scopeSortUnverified, Coverage: scopeCoverageUnknown}
	inv.observeSort(&PanelEvidence{SortVerified: true, SortLabel: sortLabelRarity})
	if got := scopeOf(t, inv); got.Sort != scopeSortVerified || got.Coverage != scopeCoverageUnknown {
		t.Fatalf("after sort check = %+v, want verified/unknown", got)
	}

	completePositions(inv)
	got := scopeOf(t, inv)
	if got.Coverage != scopeCoverageComplete || len(got.Positions) != scopePositionCount {
		t.Fatalf("after six complete positions = %+v, want complete with six entries", got)
	}
	if got.Positions[5].Position != 6 || !got.Positions[5].Complete {
		t.Fatalf("last ledger entry = %+v", got.Positions[5])
	}
}

func TestObservedScopeStaysPartialWithoutVerifiedSort(t *testing.T) {
	inv := &Inventory{}
	inv.scope = scopePayload{Sort: scopeSortUnverified, Coverage: scopeCoverageUnknown}
	completePositions(inv)
	got := scopeOf(t, inv)
	if got.Coverage != scopeCoveragePartial || got.Sort != scopeSortUnverified {
		t.Fatalf("scope = %+v, want partial/unverified", got)
	}
}

func TestObservedScopeSortMismatchSticks(t *testing.T) {
	inv := &Inventory{}
	inv.scope = scopePayload{Sort: scopeSortUnverified, Coverage: scopeCoverageUnknown}
	inv.observeSort(&PanelEvidence{SortVerified: true, SortLabel: sortLabelRarity})
	inv.observeSort(&PanelEvidence{}) // a later panel did not match
	if got := scopeOf(t, inv); got.Sort != scopeSortUnverified || got.SortLabel != "" {
		t.Fatalf("scope = %+v, want the mismatch to leave the sort unverified", got)
	}
}

func TestObservedScopePartialLedger(t *testing.T) {
	inv := &Inventory{}
	inv.scope = scopePayload{Sort: scopeSortVerified, Coverage: scopeCoverageUnknown}
	inv.observeSort(&PanelEvidence{SortVerified: true, SortLabel: sortLabelRarity})
	inv.recordPosition(positionScope{Position: 1, Viewports: 1, Visits: 2, Top: true, Bottom: false})
	got := scopeOf(t, inv)
	if got.Coverage != scopeCoveragePartial || len(got.Positions) != 1 || got.Positions[0].Complete {
		t.Fatalf("scope = %+v, want one incomplete position and partial coverage", got)
	}
}
