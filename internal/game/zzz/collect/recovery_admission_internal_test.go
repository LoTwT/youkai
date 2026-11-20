package collect

import (
	"context"
	"fmt"
	"testing"

	"github.com/LoTwT/youkai/internal/platform"
)

// Replay only the admission boundary. The caller supplies a second proven
// observation of the same cell; locating that cell belongs to traversal.
func TestRecoveryReplayUnconfirmedDetailLeavesFirstSequenceAvailable(t *testing.T) {
	layout := MacCloudLayout()
	occupied := make([]bool, len(layout.Slots))
	occupied[0] = true
	run := &inventoryRun{
		inv: &Inventory{Layout: layout, Matcher: Uncalibrated{}},
		geo: 1, position: 1, ledger: newSlotLedger(layout.Columns, occupied, 1),
	}
	err := runScriptedSession(t, run, func(ctx context.Context, run *inventoryRun) error {
		out := detailOutcome{
			frame: platform.Frame{ID: 2, Geometry: 1},
			match: PageMatch{Page: PageWarehouse,
				Wheel:  &WheelEvidence{Position: 1},
				List:   &ListEvidence{Occupied: occupied, Selected: 0},
				Detail: &DetailEvidence{Quality: QualityS},
			},
		}
		admit := func(timing *visitTiming) error {
			slot := run.h.Acquire(ctx)
			if slot == nil {
				return fmt.Errorf("could not reserve the capture slot")
			}
			return run.admit(slot, out, platform.Frame{}, PageMatch{}, 0, 0, 0,
				AssociationTransition, out.frame.ID-1, nil, 1, "first cell", timing)
		}
		if err := admit(&visitTiming{}); faultDiagnosticCode(err) != CodeDetailUnresolved {
			return fmt.Errorf("undecidable substat count returned %v", err)
		}
		out.frame.ID = 4
		out.match.Detail.Substats = 3
		var timing visitTiming
		if err := admit(&timing); err != nil {
			return err
		}
		if !timing.admitted || timing.seq != 1 || timing.visitIndex != 1 || run.lastDetailFrame.ID != 4 {
			return fmt.Errorf("revisit admitted=%t seq=%d visit=%d frame=%d", timing.admitted,
				timing.seq, timing.visitIndex, run.lastDetailFrame.ID)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
