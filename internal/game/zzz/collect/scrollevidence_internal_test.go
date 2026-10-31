package collect

import (
	"math"
	"testing"
)

// The competition's tie semantics: a whole-row candidate must beat the row the
// verdict is weighed against by more than nccTieEpsilon to count as a
// contradiction. These tests pin the boundary directly, including the
// round-off-sized differences on which the previous exact comparison made the
// verdict depend.

func TestNCCCompetitionTieBand(t *testing.T) {
	cases := []struct {
		name          string
		chosen, rival float64
		beaten        bool
	}{
		{"exact tie", 0.930, 0.930, false},
		{"round-off tie", 0.930, 0.930 + 1e-12, false},
		{"inside the band", 0.930, 0.934, false},
		{"outside the band", 0.930, 0.936, true},
		{"significant", 0.930, 0.960, true},
		{"rival below", 0.930, 0.910, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			comp := nccCompetition{chosen: tc.chosen, rivalRow: 3, rival: tc.rival, gap: tc.chosen - tc.rival}
			if got := comp.significantlyBeaten(); got != tc.beaten {
				t.Fatalf("rival %.6f vs chosen %.6f: significantlyBeaten = %v, want %v (epsilon %.3f)",
					tc.rival, tc.chosen, got, tc.beaten, nccTieEpsilon)
			}
		})
	}
}

func TestNCCCompetitionGapAndRival(t *testing.T) {
	cands := map[int]nccCandidate{
		0:  {ncc: 0.10},
		1:  {ncc: 0.92},
		3:  {ncc: 0.92},
		2:  {ncc: 0.80},
		-4: {ncc: 0.95},
		4:  {ncc: 0.90},
	}
	comp, ok := nccCompetitionAt(cands, 4)
	if !ok {
		t.Fatal("the chosen row was measured but nccCompetitionAt reported no competition")
	}
	if comp.rivalRow != -4 || comp.rival != 0.95 {
		t.Fatalf("competition %+v, want the strongest rival -4 at 0.950", comp)
	}
	if math.Abs(comp.gap-(-0.05)) > 1e-12 {
		t.Fatalf("gap %.6f, want the chosen row minus the rival (-0.05)", comp.gap)
	}
	if !comp.significantlyBeaten() {
		t.Fatal("a rival 0.05 above the chosen row must count as significantly better")
	}

	// With the strongest rival removed, a tie between +1 and +3 must name the
	// smaller row so the recorded evidence does not depend on map order.
	cands[-4] = nccCandidate{ncc: 0.10}
	comp, ok = nccCompetitionAt(cands, 4)
	if !ok || comp.rivalRow != 1 || comp.rival != 0.92 {
		t.Fatalf("competition %+v, want the smallest tied rival +1 at 0.920", comp)
	}
	if !comp.significantlyBeaten() {
		t.Fatal("a rival 0.02 above the chosen row is outside the tie epsilon")
	}

	// A rival less than the epsilon above the chosen row is a tie, not a
	// contradiction.
	cands[1] = nccCandidate{ncc: 0.902}
	cands[3] = nccCandidate{ncc: 0.902}
	comp, _ = nccCompetitionAt(cands, 4)
	if comp.significantlyBeaten() {
		t.Fatalf("competition %+v: a rival 0.002 above the chosen row is inside the tie epsilon", comp)
	}
}

func TestNCCCompetitionWithoutRival(t *testing.T) {
	comp, ok := nccCompetitionAt(map[int]nccCandidate{4: {ncc: 0.94}}, 4)
	if !ok {
		t.Fatal("a measured chosen row must report a competition")
	}
	if !math.IsInf(comp.gap, 1) || comp.significantlyBeaten() {
		t.Fatalf("competition %+v, want an infinite gap and no contradiction without rivals", comp)
	}
	if _, ok := nccCompetitionAt(map[int]nccCandidate{1: {ncc: 0.94}}, 4); ok {
		t.Fatal("an unmeasured chosen row must report ok=false")
	}
}

// TestNCCVerdictGapMatchesCompetition pins the diagnostics helper the log and
// manifest margins read: it must report the same gap the competition used.
func TestNCCVerdictGapMatchesCompetition(t *testing.T) {
	cands := map[int]nccCandidate{0: {ncc: 0.5}, 1: {ncc: 0.91}, 4: {ncc: 0.93}}
	comp, _ := nccCompetitionAt(cands, 4)
	best, gap, ok := nccVerdict(cands, 4)
	if !ok || best != comp.chosen || gap != comp.gap {
		t.Fatalf("nccVerdict = (%.3f, %.3f, %v), want the competition's (%.3f, %.3f)",
			best, gap, ok, comp.chosen, comp.gap)
	}
}
