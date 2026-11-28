package collect

import (
	"context"
	"fmt"

	"github.com/LoTwT/youkai/internal/platform"
	"github.com/LoTwT/youkai/internal/scan"
)

func decidableDisc(d *DetailEvidence) bool {
	return d != nil && (d.Quality == QualityNotS || (d.Quality == QualityS && (d.Substats == 3 || d.Substats == 4)))
}

// Both boundaries visible in a prepared top viewport anchor its rows without
// depending on a repeated row pattern or the last scroll's estimated offset.
func (run *inventoryRun) anchoredShortList(m PageMatch) bool {
	return run.positionListVerdict(m) && m.List.Top && m.List.Bottom &&
		m.Filter != nil && m.Filter.State == FilterActive && run.ledger.rowBase == 0 &&
		run.ledger.agreesWith(m.List.Occupied)
}

// Keep the original click baseline: a second observation of unchanged old
// detail can never establish a transition. No input is sent during a revisit.
func (run *inventoryRun) awaitTargetDetail(ctx context.Context, baseline platform.Frame, target int, initial bool) (detailOutcome, error) {
	out, err := run.awaitDetail(ctx, baseline, target, initial)
	if err != nil || !run.inv.EnableBoundedRecovery || run.ledger == nil || (out.class != awaitStable && out.class != awaitDeadline) {
		return out, err
	}
	if !run.positionListVerdict(out.match) || out.match.List.Selected != target {
		return out, nil
	}
	if err := run.checkLedgerViewport(out.match); err != nil {
		return out, err
	}
	code := CodeDetailUnresolved
	switch out.class {
	case awaitStable:
		if decidableDisc(out.match.Detail) {
			return out, nil
		}
	case awaitDeadline:
		// The existing same-detail reference round trip has its own stricter
		// evidence requirements. Preserve that path and its input budget.
		if run.sameDetailSelection(out, target) {
			return out, nil
		}
		if out.sawPopulated {
			code = CodeDetailUnattributed
		}
	}
	incomplete := func(reason string) error {
		return positionFault("detail", code, "collect: wheel position %d slot %d frame %d: %s", run.position, target, out.frame.ID, reason)
	}
	if !run.rangeProof.Verified || !run.anchoredShortList(run.listMatch) || !run.anchoredShortList(out.match) {
		return out, incomplete("the range and both list boundaries do not prove the cell for a detail revisit")
	}
	cell := &run.ledger.visible()[target]
	if cell.State != slotUnresolved || cell.Sequence != 0 {
		return out, scan.NewFault(CodePositionLost, fmt.Errorf("collect: revisit target %d already resolved", target))
	}
	if run.detailRevisitUsed {
		return out, incomplete("the session detail revisit budget was exhausted")
	}
	if out.frame.Reused || out.frame.ID <= run.lastFrame.ID || !out.frame.ArrivedAt.After(run.lastFrame.ArrivedAt) {
		return out, scan.NewFault(CodePositionLost, fmt.Errorf("collect: stale detail revisit origin frame %d", out.frame.ID))
	}
	if err := run.observeReady(ctx); err != nil {
		return out, err
	}
	run.detailRevisitUsed = true
	cell.RevisitFromFrameID = out.frame.ID
	cell.RevisitReason = code
	previous := out.frame
	check := func(f platform.Frame, m PageMatch) error {
		if f.Reused || f.ID <= previous.ID || !f.ArrivedAt.After(previous.ArrivedAt) {
			return scan.NewFault(CodePositionLost, fmt.Errorf("collect: detail revisit needs a fresh frame after %d", previous.ID))
		}
		if !run.anchoredShortList(m) || m.List.Selected != target {
			return scan.NewFault(CodePositionLost, fmt.Errorf("collect: frame %d no longer proves the selected revisit cell %d", f.ID, target))
		}
		previous = f
		return nil
	}
	next, err := run.awaitDetailFrames(ctx, baseline, nil, target, initial, check, false)
	if err == nil && next.class == awaitDeadline {
		return next, incomplete("the bounded detail revisit did not establish an admissible observation")
	}
	if err == nil && next.class == awaitStable {
		err = run.observeReady(ctx)
	}
	return next, err
}
