package collect

import "testing"

func TestStorageBottomJitterRetainsKnownBadge(t *testing.T) {
	l := StorageLayout()
	m := NewScrollEvidenceMatcher(NewPixelMatcher(l, StorageCalibration()))
	before := storageSelectedPatternFrame(l, 1, 0, -1, 833, 856)
	candidate := storageSelectedPatternFrame(l, 2, 1, -1, 839, 862)
	if got := m.MatchScroll(before, candidate); !got.Displaced || got.Rows != 1 {
		t.Fatalf("candidate must prove one row: %s", got.Evidence)
	}
	wantBadge := m.Match(candidate).List.Positions[27]
	if !wantBadge.known() || wantBadge.Position != 5 {
		t.Fatalf("candidate must establish badge 5 in the new row: %+v", wantBadge)
	}
	for _, tc := range []struct {
		name  string
		badge int
		want  int
	}{{"aligned", 5, 1}, {"conflict", 2, 0}} {
		t.Run(tc.name, func(t *testing.T) {
			jitter := storageSelectedPatternFrame(l, 3, 1, -1, 839, 863)
			clearStorageBadge(l, jitter, 0)
			paintStorageBadge(l, jitter, 27, tc.badge)
			c, j := m.Match(candidate).List, m.Match(jitter).List
			if c.Bottom || !j.Bottom {
				t.Fatalf("one-pixel trailing-edge jitter should cross the bottom threshold: candidate=%t jitter=%t", c.Bottom, j.Bottom)
			}
			if got := j.Positions[27]; !got.known() || got.Position != tc.badge {
				t.Fatalf("jitter frame must freshly read the chosen badge: %+v", got)
			}
			if got := m.MatchScroll(before, jitter); !got.Undecided || got.SelectionConflict {
				t.Fatalf("a missing overlap badge must leave the direct displacement unproven: %s", got.Evidence)
			}
			if ok, why := m.zeroDisplacementProven(candidate, jitter); !ok {
				t.Fatalf("the candidate-relative comparison must independently prove zero: %s", why)
			}
			one := storageSelectedPatternFrame(l, 4, 1, -1, 839, 862)
			two := storageSelectedPatternFrame(l, 5, 1, -1, 839, 862)
			out, viewport := storageWaitAndCommit(t, l, m, before, candidate, jitter, one, two)
			if viewport != tc.want || out.settled != (tc.want == 1) {
				t.Fatalf("badge 5 → %d on the proven stationary frame with Bottom=true → 5 twice: settled=%t candidate=%d confirmation=%d viewport=%d want=%d",
					tc.badge, out.settled, out.after.ID, out.settleFrameID, viewport, tc.want)
			}
		})
	}
}

func TestStorageRelativeReturnChecksExposedBadge(t *testing.T) {
	l := StorageLayout()
	m := NewScrollEvidenceMatcher(NewPixelMatcher(l, StorageCalibration()))
	before := storageSelectedPatternFrame(l, 1, 0, -1, 300, 323)
	candidate := storageSelectedPatternFrame(l, 2, 2, -1, 312, 335)
	if got := m.MatchScroll(before, candidate); !got.Displaced || got.Rows != 2 {
		t.Fatalf("candidate must prove two rows: %s", got.Evidence)
	}
	if p := m.Match(before).List.Positions[9]; !p.known() || p.Position != 2 {
		t.Fatalf("start must establish badge 2 at absolute row 1: %+v", p)
	}
	for _, tc := range []struct {
		name  string
		badge int
		want  int
	}{{"aligned", 2, 1}, {"conflict", 5, 0}} {
		t.Run(tc.name, func(t *testing.T) {
			back := storageSelectedPatternFrame(l, 3, 1, -1, 306, 329)
			paintStorageBadge(l, back, 0, tc.badge)
			if p := m.Match(back).List.Positions[0]; !p.known() || p.Position != tc.badge {
				t.Fatalf("fixture must read the chosen badge at absolute row 1: %+v", p)
			}
			if rows, ok := scrollSignedRows(m.MatchScroll(candidate, back)); !ok || rows != -1 {
				t.Fatalf("the candidate-relative comparison must independently prove -1 row: %s", m.MatchScroll(candidate, back).Evidence)
			}
			direct := m.MatchScroll(before, back)
			if (tc.want == 0 && !direct.Undecided) ||
				(tc.want == 1 && (!direct.Displaced || direct.Rows != 1)) {
				t.Fatalf("direct comparison must distinguish contradictory badge: %s", direct.Evidence)
			}
			one := storageSelectedPatternFrame(l, 4, 2, -1, 312, 335)
			two := storageSelectedPatternFrame(l, 5, 2, -1, 312, 335)
			three := storageSelectedPatternFrame(l, 6, 2, -1, 312, 335)
			out, viewport := storageWaitAndCommit(t, l, m, before, candidate, back, one, two, three)
			if viewport != tc.want || out.settled != (tc.want == 1) {
				t.Fatalf("badge 2 → offscreen at +2 → badge %d at proven +1 → offscreen at +2: settled=%t candidate=%d confirmation=%d viewport=%d want=%d",
					tc.badge, out.settled, out.after.ID, out.settleFrameID, viewport, tc.want)
			}
		})
	}
}
