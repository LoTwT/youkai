package collect

import "testing"

func TestStorageProvenZeroAfterCandidateBindsAnchor(t *testing.T) {
	l := StorageLayout()
	m := NewScrollEvidenceMatcher(NewPixelMatcher(l, StorageCalibration()))
	before := storageSelectedPatternFrame(l, 1, 0, -1, 300, 323)
	first := storageSelectedPatternFrame(l, 2, 1, -1, 306, 329)
	bounce := storageSelectedPatternFrame(l, 3, 0, 27, 300, 323)
	replacement := storageSelectedPatternFrame(l, 4, 1, -1, 306, 329)
	if !m.MatchScroll(before, first).Displaced {
		t.Fatal("first candidate did not prove one row")
	}
	if !m.MatchScroll(before, bounce).Stationary {
		t.Fatal("bounce was not stationary from the action start")
	}
	if ok, why := m.zeroDisplacementProven(before, bounce); !ok {
		t.Fatalf("bounce did not prove zero displacement: %s", why)
	}
	for _, tc := range []struct {
		name string
		slot int
		want int
	}{{"aligned", 18, 1}, {"conflict", 9, 0}} {
		t.Run(tc.name, func(t *testing.T) {
			one := storageSelectedPatternFrame(l, 5, 1, tc.slot, 306, 329)
			two := storageSelectedPatternFrame(l, 6, 1, tc.slot, 306, 329)
			out, viewport := storageWaitAndCommit(t, l, m, before, first, bounce, replacement, one, two)
			if viewport != tc.want || out.settled != (tc.want == 1) {
				t.Fatalf("proven return to zero did not bind the anchor: settled=%t candidate=%d confirmation=%d viewport=%d want=%d",
					out.settled, out.after.ID, out.settleFrameID, viewport, tc.want)
			}
		})
	}
}

func TestStorageLaterProvenAnchorUpgradesColumnOnly(t *testing.T) {
	l := StorageLayout()
	m := NewScrollEvidenceMatcher(NewPixelMatcher(l, StorageCalibration()))
	before := storageSelectedPatternFrame(l, 1, 0, -1, 300, 323)
	unproven := storageSelectedPatternFrame(l, 2, 0, 27, 0, 0)
	anchored := storageSelectedPatternFrame(l, 3, 0, 27, 300, 323)
	replacement := storageSelectedPatternFrame(l, 4, 1, -1, 306, 329)
	if ok, why := m.zeroDisplacementProven(before, unproven); ok {
		t.Fatalf("unproven frame passed zero proof: %s", why)
	}
	if ok, why := m.zeroDisplacementProven(before, anchored); !ok {
		t.Fatalf("later frame did not prove zero: %s", why)
	}
	for _, tc := range []struct {
		name string
		slot int
		want int
	}{{"aligned", 18, 1}, {"conflict", 9, 0}} {
		t.Run(tc.name, func(t *testing.T) {
			one := storageSelectedPatternFrame(l, 5, 1, tc.slot, 306, 329)
			two := storageSelectedPatternFrame(l, 6, 1, tc.slot, 306, 329)
			out, viewport := storageWaitAndCommit(t, l, m, before, unproven, anchored, replacement, one, two)
			if viewport != tc.want || out.settled != (tc.want == 1) {
				t.Fatalf("column-only anchor was not upgraded: settled=%t candidate=%d confirmation=%d viewport=%d want=%d",
					out.settled, out.after.ID, out.settleFrameID, viewport, tc.want)
			}
		})
	}
}

func TestStorageProvenZeroAfterCandidateChecksKnownBadge(t *testing.T) {
	l := StorageLayout()
	m := NewScrollEvidenceMatcher(NewPixelMatcher(l, StorageCalibration()))
	before := storageSelectedPatternFrame(l, 1, 0, -1, 300, 323)
	first := storageSelectedPatternFrame(l, 2, 2, -1, 312, 335)
	if got := m.MatchScroll(before, first); !got.Displaced || got.Rows != 2 {
		t.Fatalf("first candidate did not prove two rows: %s", got.Evidence)
	}
	for _, tc := range []struct {
		name  string
		badge int
		want  int
	}{{"aligned", 2, 1}, {"conflict", 5, 0}} {
		t.Run(tc.name, func(t *testing.T) {
			bounce := storageSelectedPatternFrame(l, 3, 0, -1, 300, 323)
			paintStorageBadge(l, bounce, 9, tc.badge)
			if ok, why := m.zeroDisplacementProven(before, bounce); !ok {
				t.Fatalf("bounce did not prove zero: %s", why)
			}
			replacement := storageSelectedPatternFrame(l, 4, 2, -1, 312, 335)
			one := storageSelectedPatternFrame(l, 5, 2, -1, 312, 335)
			two := storageSelectedPatternFrame(l, 6, 2, -1, 312, 335)
			out, viewport := storageWaitAndCommit(t, l, m, before, first, bounce, replacement, one, two)
			if viewport != tc.want || out.settled != (tc.want == 1) {
				t.Fatalf("proven zero badge was not reconciled: settled=%t candidate=%d confirmation=%d viewport=%d want=%d",
					out.settled, out.after.ID, out.settleFrameID, viewport, tc.want)
			}
		})
	}
}

func TestStorageProvenCandidateStillnessAnchorsConfirmation(t *testing.T) {
	l := StorageLayout()
	m := NewScrollEvidenceMatcher(NewPixelMatcher(l, StorageCalibration()))
	before := storageSelectedPatternFrame(l, 1, 0, 27, 300, 323)
	candidate := storageSelectedPatternFrame(l, 2, 1, -1, 306, 329)
	if got := m.MatchScroll(before, candidate); !got.Displaced || got.Rows != 1 {
		t.Fatalf("candidate did not prove one row: %s", got.Evidence)
	}
	for _, tc := range []struct {
		name string
		slot int
		want int
	}{{"aligned", 18, 1}, {"conflict", 9, 0}} {
		t.Run(tc.name, func(t *testing.T) {
			confirm1 := storageSelectedPatternFrame(l, 3, 1, tc.slot, 306, 329)
			confirm2 := storageSelectedPatternFrame(l, 4, 1, tc.slot, 306, 329)
			clearStorageBadge(l, confirm1, 0)
			clearStorageBadge(l, confirm2, 0)
			if got := m.MatchScroll(before, confirm1); !got.Undecided || got.SelectionConflict {
				t.Fatalf("direct comparison should be undecided without conflict: %s", got.Evidence)
			}
			if ok, why := m.zeroDisplacementProven(candidate, confirm1); !ok {
				t.Fatalf("confirmation did not prove candidate stillness: %s", why)
			}
			out, viewport := storageWaitAndCommit(t, l, m, before, candidate, confirm1, confirm2)
			if viewport != tc.want || out.settled != (tc.want == 1) {
				t.Fatalf("candidate stillness did not anchor confirmation: settled=%t candidate=%d confirmation=%d viewport=%d want=%d",
					out.settled, out.after.ID, out.settleFrameID, viewport, tc.want)
			}
		})
	}
}
