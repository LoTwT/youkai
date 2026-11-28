package collect_test

import (
	"testing"

	"github.com/LoTwT/youkai/internal/scan"
)

// TestStorageCloseToleratesSortMissBeforePanelLeaves reproduces the Windows
// trace: the close input is posted, both storage panel probes still read
// open, and the rare-sort template misses for two frames while the panel is
// leaving. The close must wait for the page change; it must not abort on
// those unverified-but-open frames.
func TestStorageCloseToleratesSortMissBeforePanelLeaves(t *testing.T) {
	game := newStorageFlowGame()
	game.panelCloseLag = 3
	_, observations, scope, snap := runStorageFlowInternal(t, game, []int{1})
	if snap.End != scan.EndCompleted {
		t.Fatalf("storage flow end = %s trigger=%+v", snap.End, snap.Trigger)
	}
	if len(observations) == 0 || observations[0].RarityFilter != "S" || !scope.Positions[0].Range.Verified {
		t.Fatalf("observations=%d range=%+v", len(observations), scope.Positions[0].Range)
	}
}
