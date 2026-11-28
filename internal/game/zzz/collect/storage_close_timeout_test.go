package collect_test

import (
	"testing"

	"github.com/LoTwT/youkai/internal/game/zzz/collect"
	"github.com/LoTwT/youkai/internal/scan"
)

// TestStorageCloseTimeoutOnUnreadablePanelDoesNotRetry pins the timeout
// boundary: after the close has already been posted and only unreadable
// panel frames remain, a retry can toggle the shared funnel control and
// reopen the panel instead of proving a close.
func TestStorageCloseTimeoutOnUnreadablePanelDoesNotRetry(t *testing.T) {
	game := newStorageFlowGame()
	game.panelCloseLag = 1 << 30
	_, observations, _, snap := runStorageFlowInternal(t, game, []int{1})
	if snap.End != scan.EndFailed {
		t.Fatalf("storage flow end = %s trigger=%+v", snap.End, snap.Trigger)
	}
	if snap.Trigger == nil || snap.Trigger.Code != collect.CodeNavigationMismatch {
		t.Fatalf("trigger = %+v, want navigationMismatch", snap.Trigger)
	}
	if len(observations) != 0 {
		t.Fatalf("captured %d observations while the panel never closed", len(observations))
	}
	var opened, closes int
	for _, op := range game.opsSnapshot() {
		if op.At != game.layout.FilterButton {
			continue
		}
		if opened == 0 {
			opened = 1
			continue
		}
		closes++
	}
	if closes != 1 {
		t.Fatalf("dispatched %d close clicks after opening the panel, want 1", closes)
	}
}
