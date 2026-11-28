package collect_test

import (
	"testing"

	"github.com/LoTwT/youkai/internal/game/zzz/collect"
	"github.com/LoTwT/youkai/internal/scan"
)

// TestStorageBadgeTransientWaitsForFreshFrame reproduces the field sequence:
// a normal target click, two non-target badge conflicts while the target and
// every prior ledger cell remain unchanged, then a fully readable frame.
// The detail wait must skip the transient frames and admit the later frame.
func TestStorageBadgeTransientWaitsForFreshFrame(t *testing.T) {
	game := newStorageFlowGame()
	game.badgeTransientAfterClick = 6
	_, observations, scope, snap := runStorageFlowInternal(t, game, nil)
	if snap.End != scan.EndCompleted {
		t.Fatalf("storage flow end = %s trigger=%+v", snap.End, snap.Trigger)
	}
	if len(observations) < 2 || !scope.Positions[0].Range.Verified {
		t.Fatalf("observations=%d range=%+v", len(observations), scope.Positions[0].Range)
	}
	transient := map[uint64]bool{}
	for _, id := range game.badgeTransientIDs {
		transient[id] = true
	}
	for _, obs := range observations {
		if transient[obs.FrameID] {
			t.Fatalf("observation was admitted from transient badge frame %d", obs.FrameID)
		}
		if obs.Quality != collect.QualityS || len(obs.Lines) == 0 {
			t.Fatalf("observation detail = quality %q lines %d, want the admitted S detail frame", obs.Quality, len(obs.Lines))
		}
	}
}

// TestStorageBadgeTransientTimeoutDoesNotRetry pins the safe stop when the
// transient never clears: no extra card click, no reference round trip.
func TestStorageBadgeTransientTimeoutDoesNotRetry(t *testing.T) {
	game := newStorageFlowGame()
	game.badgeTransientAfterClick = 1 << 30
	_, observations, _, snap := runStorageFlowInternal(t, game, nil)
	if snap.End != scan.EndFailed || snap.Trigger == nil {
		t.Fatalf("storage flow end = %s trigger=%+v", snap.End, snap.Trigger)
	}
	if len(observations) != 1 {
		t.Fatalf("captured %d observations before the transient cleared, want the one initial admission", len(observations))
	}
	clicks := game.clickedGlobalsSnapshot()
	counts := map[int]int{}
	for _, global := range clicks {
		counts[global]++
	}
	for global, n := range counts {
		if n > 1 {
			t.Fatalf("target %d was clicked %d times after the transient", global, n)
		}
	}
}

// TestStorageBadgeTransientReusedFrameCannotRecover ensures a reused frame
// after the transient cannot stand in for the fresh re-read.
func TestStorageBadgeTransientReusedFrameCannotRecover(t *testing.T) {
	game := newStorageFlowGame()
	game.badgeTransientAfterClick = 1 << 30
	game.badgeReuseAfterTransient = true
	_, observations, _, snap := runStorageFlowInternal(t, game, nil)
	if snap.End != scan.EndFailed {
		t.Fatalf("storage flow end = %s trigger=%+v", snap.End, snap.Trigger)
	}
	if len(observations) != 1 {
		t.Fatalf("captured %d observations, want only the initial admission", len(observations))
	}
}

// TestStorageBadgeRecoveryRejectsKnownWrongBadge ensures a known wrong
// non-target badge after a transient is an original fault, not another
// frame to count toward detail stability.
func TestStorageBadgeRecoveryRejectsKnownWrongBadge(t *testing.T) {
	game := newStorageFlowGame()
	game.badgeTransientAfterClick = 2
	game.badgeWrongAfterTransient = 2
	_, observations, _, snap := runStorageFlowInternal(t, game, nil)
	if snap.End != scan.EndFailed {
		t.Fatalf("storage flow end = %s trigger=%+v", snap.End, snap.Trigger)
	}
	if len(observations) != 1 {
		t.Fatalf("captured %d observations, want only the initial admission", len(observations))
	}
}

// TestStorageBadgeTransientAllowsSameDetailReference ensures a recovered,
// fully readable frame still reaches the existing reference round trip when
// the target detail is the same as its baseline.
func TestStorageBadgeTransientAllowsSameDetailReference(t *testing.T) {
	game := newStorageFlowGame()
	game.detailGroups = make([]int, game.total*game.layout.Columns)
	for i := range game.detailGroups {
		game.detailGroups[i] = i + 1
	}
	game.detailGroups[0], game.detailGroups[1] = 1, 1
	game.badgeTransientAfterClick = 2
	_, observations, _, snap := runStorageFlowInternal(t, game, nil)
	if snap.End != scan.EndCompleted || len(observations) < 2 {
		t.Fatalf("storage flow end = %s observations=%d trigger=%+v", snap.End, len(observations), snap.Trigger)
	}
	if len(game.clickedGlobalsSnapshot()) < 3 {
		t.Fatalf("same-detail flow dispatched %d card clicks, want target + reference + return", len(game.clickedGlobalsSnapshot()))
	}
}

// TestStorageBadgeTransientReferenceClickDoesNotRecover ensures the new
// wait is opted in only by the normal target click, not by the reference or
// return click.
func TestStorageBadgeTransientReferenceClickDoesNotRecover(t *testing.T) {
	game := newStorageFlowGame()
	game.detailGroups = make([]int, game.total*game.layout.Columns)
	for i := range game.detailGroups {
		game.detailGroups[i] = i + 1
	}
	game.detailGroups[0], game.detailGroups[1] = 1, 1
	game.badgeTransientAfterClick = 1 << 30
	game.badgeTransientClickAt = 2
	_, observations, _, snap := runStorageFlowInternal(t, game, nil)
	if snap.End != scan.EndFailed {
		t.Fatalf("storage flow end = %s observations=%d trigger=%+v", snap.End, len(observations), snap.Trigger)
	}
	if len(game.clickedGlobalsSnapshot()) != 2 {
		t.Fatalf("reference path dispatched %d card clicks, want target + reference only", len(game.clickedGlobalsSnapshot()))
	}
}

// TestStorageBadgeRecoveryStillChecksEveryLaterFrame covers a correct frame
// between the transient and a known wrong frame. One recovered frame must
// not disable validation for the rest of the detail wait.
func TestStorageBadgeRecoveryStillChecksEveryLaterFrame(t *testing.T) {
	game := newStorageFlowGame()
	game.badgeTransientAfterClick = 2
	game.badgeWrongAfterTransient = 2
	game.badgeWrongDelay = 2
	_, observations, _, snap := runStorageFlowInternal(t, game, nil)
	if snap.End != scan.EndFailed {
		t.Fatalf("storage flow end = %s trigger=%+v", snap.End, snap.Trigger)
	}
	if len(observations) != 1 {
		t.Fatalf("captured %d observations, want only the initial admission", len(observations))
	}
}

// TestStorageScopedRescanBadgeTransientKeepsStop ensures the narrow wait is
// only enabled for the full independent inventory, not a scoped rescan.
func TestStorageScopedRescanBadgeTransientKeepsStop(t *testing.T) {
	game := newStorageFlowGame()
	game.badgeTransientAfterClick = 6
	_, observations, _, snap := runStorageFlowInternal(t, game, []int{1})
	if snap.End != scan.EndFailed {
		t.Fatalf("storage flow end = %s trigger=%+v", snap.End, snap.Trigger)
	}
	if len(observations) != 1 {
		t.Fatalf("captured %d observations, want only the initial admission", len(observations))
	}
}

// TestStorageBadgeWrongBeforeTransientStops preserves an explicit ledger
// contradiction seen before the first transient. It must not be erased by a
// later recoverable frame sequence.
func TestStorageBadgeWrongBeforeTransientStops(t *testing.T) {
	game := newStorageFlowGame()
	game.badgeWrongOnClick = 2
	_, observations, _, snap := runStorageFlowInternal(t, game, nil)
	if snap.End != scan.EndFailed {
		t.Fatalf("storage flow end = %s trigger=%+v", snap.End, snap.Trigger)
	}
	if len(observations) != 1 {
		t.Fatalf("captured %d observations, want only the initial admission", len(observations))
	}
}
