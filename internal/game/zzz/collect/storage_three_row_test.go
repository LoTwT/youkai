package collect_test

import (
	"testing"

	"github.com/LoTwT/youkai/internal/platform"
	"github.com/LoTwT/youkai/internal/scan"
)

func storageThreeRowGame(rows, cards int) *storageFlowGame {
	g := newStorageFlowGame()
	g.total = rows
	g.maxTop = rows - g.rows
	g.cards = cards
	return g
}

func runStorageThreeRowFlow(t *testing.T, game *storageFlowGame) (int, storageFlowScope) {
	t.Helper()
	result, observations, scope, snap := runStorageFlowInternal(t, game, nil)
	if snap.End != scan.EndCompleted {
		t.Fatalf("storage flow end = %s trigger=%+v", snap.End, snap.Trigger)
	}
	if scope.Coverage != "complete" {
		t.Fatalf("coverage = %q, want complete", scope.Coverage)
	}
	for _, pos := range scope.Positions {
		if !pos.Complete || !pos.Top || !pos.Bottom || !pos.Range.Verified {
			t.Fatalf("position %d is not complete: %+v", pos.Position, pos)
		}
	}
	checkStorageIdentities(t, result, observations, len(game.clickedSnapshot()))
	return len(observations), scope
}

func TestStorageThreeRowFullStepThenOneDetentBottom(t *testing.T) {
	game := storageThreeRowGame(7, 7*9)
	observations, scope := runStorageThreeRowFlow(t, game)
	if observations != 63 {
		t.Fatalf("admitted %d cards, want 63", observations)
	}
	checkStorageActions(t, game, game.opsSnapshot(), 62, 1, 1)
	if len(scope.Positions) != 6 {
		t.Fatalf("positions = %d, want 6", len(scope.Positions))
	}
}

func TestStorageThreeRowInitialBottomUsesOneDetentRecheck(t *testing.T) {
	game := storageThreeRowGame(4, 4*9)
	observations, scope := runStorageThreeRowFlow(t, game)
	if observations != 36 {
		t.Fatalf("admitted %d cards, want 36", observations)
	}
	checkStorageActions(t, game, game.opsSnapshot(), 35, 0, 1)
	if len(scope.Positions) != 6 {
		t.Fatalf("positions = %d, want 6", len(scope.Positions))
	}
}

func TestStorageThreeRowShortFinalStepStillUsesBottomRecheck(t *testing.T) {
	for _, tc := range []struct {
		name  string
		rows  int
		cards int
		want  int
	}{
		{name: "one row", rows: 5, cards: 45, want: 44},
		{name: "two rows", rows: 6, cards: 54, want: 53},
	} {
		t.Run(tc.name, func(t *testing.T) {
			game := storageThreeRowGame(tc.rows, tc.cards)
			observations, _ := runStorageThreeRowFlow(t, game)
			if observations != tc.cards {
				t.Fatalf("admitted %d cards, want %d", observations, tc.cards)
			}
			checkStorageActions(t, game, game.opsSnapshot(), tc.want, 1, 1)
		})
	}
}

func TestStorageThreeRowShortFinalStepWithExtraCell(t *testing.T) {
	// Six grid rows, but the last row has one occupied card: the final
	// three-row input actually moves two rows and must still be accepted as the
	// short displacement, followed by the independent one-detent end recheck.
	game := storageThreeRowGame(6, 46)
	observations, scope := runStorageThreeRowFlow(t, game)
	if observations != 46 {
		t.Fatalf("admitted %d cards, want 46", observations)
	}
	checkStorageActions(t, game, game.opsSnapshot(), 45, 1, 1)
	if scope.Positions[0].Ledger == nil {
		t.Fatal("extra-cell flow has no persisted ledger")
	}
	empty := 0
	for _, cell := range scope.Positions[0].Ledger.Slots {
		if cell.State == "empty" {
			empty++
		}
	}
	if empty != 8 {
		t.Fatalf("extra-cell ledger empty cells = %d, want 8", empty)
	}
}

func TestStorageThreeRowWithoutSelectionAnchor(t *testing.T) {
	game := storageThreeRowGame(6, 54)
	game.clearSelectionOnScroll = true
	observations, _ := runStorageThreeRowFlow(t, game)
	if observations != 54 {
		t.Fatalf("admitted %d cards without a selection anchor, want 54", observations)
	}
	checkStorageActions(t, game, game.opsSnapshot(), 53, 1, 1)
}

func runStorageThreeRowFailure(t *testing.T, game *storageFlowGame) (storageFlowScope, []platform.InputOp) {
	t.Helper()
	_, _, scope, snap := runStorageFlowInternal(t, game, nil)
	if snap.End == scan.EndCompleted {
		t.Fatalf("failure fixture unexpectedly completed: %+v", snap.Trigger)
	}
	return scope, game.opsSnapshot()
}

func TestStorageThreeRowShortStepMovingRecheckDoesNotComplete(t *testing.T) {
	game := storageThreeRowGame(6, 54)
	game.bottomRecheckShift = 1
	scope, ops := runStorageThreeRowFailure(t, game)
	downs := 0
	for _, op := range ops {
		if op.Kind == platform.InputScroll && op.WheelSteps < 0 {
			downs++
		}
	}
	if downs != 2 {
		t.Fatalf("wheel sequence = %+v, want one -3 advance and one -1 recheck", ops)
	}
	for _, pos := range scope.Positions {
		if pos.Complete || pos.Stage != "bottom" {
			t.Fatalf("position %d = %+v, want incomplete bottom after moving recheck", pos.Position, pos)
		}
	}
}

func TestStorageThreeRowShortStepLostRecheckDoesNotComplete(t *testing.T) {
	game := storageThreeRowGame(6, 54)
	game.bottomRecheckLost = true
	scope, ops := runStorageThreeRowFailure(t, game)
	downs := 0
	for _, op := range ops {
		if op.Kind == platform.InputScroll && op.WheelSteps < 0 {
			downs++
		}
	}
	if downs != 2 {
		t.Fatalf("wheel sequence = %+v, want one -3 advance and one -1 recheck", ops)
	}
	for _, pos := range scope.Positions {
		if pos.Complete || pos.Stage != "bottom" {
			t.Fatalf("position %d = %+v, want incomplete bottom after lost recheck", pos.Position, pos)
		}
	}
}
