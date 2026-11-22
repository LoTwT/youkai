package collect

import (
	"context"
	"fmt"
	"log"

	"github.com/LoTwT/youkai/internal/platform"
	"github.com/LoTwT/youkai/internal/scan"
)

// storageWheelStep is the normal advance input: one wheel detent. Jane's
// 2026-11-22 sample measured one detent = one row with three rows of
// overlap, and the extra detent at the list's end left both the last row
// and the slider stationary. The traversal still verifies every step's
// displacement; the detent count is an input hypothesis, not evidence.
const storageWheelStep = -1

// storageTopStep is the return-to-top input. Three detents reduce the
// number of bounded actions on a long list; the shared top loop still
// proves the thumb reached and stayed at the top.
const storageTopStep = 3

func (run *inventoryRun) scrollInputOp(before platform.Frame) platform.InputOp {
	if run.inv.storagePage() {
		return platform.InputOp{
			Kind:       platform.InputScroll,
			Geometry:   run.geo,
			FrameID:    before.ID,
			At:         run.inv.Layout.ScrollFrom,
			WheelSteps: storageWheelStep,
		}
	}
	return platform.InputOp{
		Kind:     platform.InputDrag,
		Geometry: run.geo,
		FrameID:  before.ID,
		At:       run.inv.Layout.ScrollFrom,
		To:       run.inv.Layout.ScrollTo,
		Steps:    10,
		Duration: run.inv.Layout.ScrollDuration,
	}
}

func (run *inventoryRun) topInputOp(before platform.Frame) platform.InputOp {
	if run.inv.storagePage() {
		return platform.InputOp{
			Kind:       platform.InputScroll,
			Geometry:   run.geo,
			FrameID:    before.ID,
			At:         run.inv.Layout.ScrollFrom,
			WheelSteps: storageTopStep,
		}
	}
	return platform.InputOp{
		Kind:     platform.InputDrag,
		Geometry: run.geo,
		FrameID:  before.ID,
		At:       run.inv.Layout.TopFrom,
		To:       run.inv.Layout.TopTo,
		Steps:    10,
		Duration: run.inv.Layout.TopDuration,
	}
}

func (run *inventoryRun) requireCardPosition(m PageMatch, slot int, target string) error {
	if !run.inv.storagePage() {
		return nil
	}
	if m.List == nil || slot < 0 || slot >= len(m.List.Positions) {
		return scan.NewFault(CodePositionUnreadable, fmt.Errorf(
			"collect: %s: the drive-storage list has no card-slot evidence for the target", target))
	}
	verdict := m.List.Positions[slot]
	switch {
	case verdict.Conflict:
		return scan.NewFault(CodePositionConflict, fmt.Errorf(
			"collect: %s: the card's slot badge matches more than one calibrated digit: %s", target, m.Evidence))
	case !verdict.known():
		return scan.NewFault(CodePositionUnreadable, fmt.Errorf(
			"collect: %s: the card's slot badge is unreadable: %s", target, m.Evidence))
	case verdict.Position != run.position:
		return scan.NewFault(CodePositionConflict, fmt.Errorf(
			"collect: %s: the card badge says slot %d while the traversal selected slot %d: %s",
			target, verdict.Position, run.position, m.Evidence))
	}
	return nil
}

func (run *inventoryRun) requireDetailPosition(m PageMatch, slot int, target string) error {
	if !run.inv.storagePage() {
		return nil
	}
	if m.List == nil || slot < 0 || slot >= len(m.List.Positions) || m.Detail == nil {
		return scan.NewFault(CodeDetailUnresolved, fmt.Errorf(
			"collect: %s: the settled frame has no detail-side slot evidence", target))
	}
	card := m.List.Positions[slot]
	switch {
	case card.Conflict || m.Detail.Position.Conflict:
		return scan.NewFault(CodePositionConflict, fmt.Errorf(
			"collect: %s: the card or detail slot badge is ambiguous: %s", target, m.Evidence))
	case !card.known() || !m.Detail.Position.known():
		return scan.NewFault(CodePositionUnreadable, fmt.Errorf(
			"collect: %s: the card or detail slot badge is unreadable: %s", target, m.Evidence))
	case card.Position != m.Detail.Position.Position:
		return scan.NewFault(CodePositionConflict, fmt.Errorf(
			"collect: %s: the card badge says slot %d while the detail badge says slot %d: %s",
			target, card.Position, m.Detail.Position.Position, m.Evidence))
	case m.Detail.Position.Position != run.position:
		return scan.NewFault(CodePositionConflict, fmt.Errorf(
			"collect: %s: the detail badge says slot %d while the traversal selected slot %d: %s",
			target, m.Detail.Position.Position, run.position, m.Evidence))
	}
	return nil
}

// runStorage is the drive-storage walk: one global preparation and one
// top-to-bottom pass over the mixed 9×4 list. Every observed card keeps
// its slot badge in the ledger; a scoped rescan skips cards outside its
// declared slots but still traverses the whole list so the selected
// slots' coverage can be proven. A global failure stops the pass and
// records every declared position incomplete, with the already admitted
// items kept.
func (run *inventoryRun) runStorage(ctx context.Context) error {
	err := run.prepareStorage(ctx)
	if err == nil {
		err = run.collectStorageList(ctx)
	}
	if err == nil {
		run.storageComplete = true
		run.storageBottomVerified = true
	}
	run.recordStoragePositions(err)
	return err
}

func (run *inventoryRun) prepareStorage(ctx context.Context) error {
	run.inv.reportPosition(0)
	run.position = 0
	run.viewport = 0
	run.ledger = nil
	run.rangeProof = positionRange{}
	run.filterActive = false
	run.selectionTracked = false
	run.allowInitial = true
	run.lastScrollRows = 0
	run.lastScrollEvidence = ""
	run.lastDetailFrame = platform.Frame{}
	run.positionVisitMs = 0
	run.positionScrollMs = 0
	run.positionBottomMs = 0
	run.positionScrolls = 0

	if err := run.establishList(ctx); err != nil {
		return positionIncomplete("list", err)
	}
	if err := run.resetFilters(ctx); err != nil {
		return positionIncomplete("filter", err)
	}
	var timing positionTiming
	if err := run.topStep(ctx, &timing); err != nil {
		return positionIncomplete("top", err)
	}
	run.storageTopVerified = true
	run.ledger = run.newStorageLedger(run.listMatch, run.lastFrame.ID)
	if err := run.checkLedgerViewport(run.listMatch); err != nil {
		return positionIncomplete("top", err)
	}
	log.Printf("youkai: collect: drive storage prepared: S filter active, sort=%s, top confirmed, %d visible card(s)",
		run.inv.scope.Sort, countOccupied(run.listMatch.List.Occupied))
	return nil
}

func (run *inventoryRun) newStorageLedger(m PageMatch, frameID uint64) *slotLedger {
	if m.List == nil {
		return nil
	}
	positions := make([]int, len(m.List.Positions))
	for i, p := range m.List.Positions {
		if p.known() {
			positions[i] = p.Position
		}
	}
	return newSlotLedgerWithPositions(run.inv.Layout.Columns, m.List.Occupied, positions, frameID)
}

func (run *inventoryRun) collectStorageList(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !run.h.InputPermitted() {
			return ctx.Err()
		}
		if !run.positionListVerdict(run.listMatch) {
			return scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
				"collect: drive storage: the list no longer shows the calibrated 9x4 page: %s",
				run.listMatch.Evidence))
		}
		i := run.nextSlot()
		if i < 0 {
			if run.listMatch.List.Bottom {
				// The handoff's end evidence is the extra detent that leaves
				// both the last row and the slider stationary; the short-step
				// rule is for the old four-row drag and is not accepted here.
				if err := run.confirmBottomStep(ctx); err != nil {
					return positionIncomplete("bottom", err)
				}
				return nil
			}
			if err := run.scroll(ctx); err != nil {
				return positionIncomplete("scroll", err)
			}
			// After a scroll the selection may be an old card that has left
			// the viewport while its detail stays on screen. No later
			// initial read is allowed to inherit that detail.
			run.allowInitial = false
			continue
		}
		verdict := run.listMatch.List.Positions[i]
		switch {
		case verdict.Conflict:
			return positionIncomplete("position", scan.NewFault(CodePositionConflict, fmt.Errorf(
				"collect: drive storage: slot %d's card badge matches more than one digit; the mixed list has no verifiable slot mapping: %s",
				i, run.listMatch.Evidence)))
		case !verdict.known():
			return positionIncomplete("position", scan.NewFault(CodePositionUnreadable, fmt.Errorf(
				"collect: drive storage: slot %d's card badge is unreadable; refusing to click a card whose slot is unknown: %s",
				i, run.listMatch.Evidence)))
		}
		if len(run.inv.positions) > 0 && !containsPosition(run.inv.positions, verdict.Position) {
			run.ledger.visible()[i].State = slotOutOfScope
			continue
		}
		run.position = verdict.Position
		if err := run.visitSlot(ctx, i); err != nil {
			return positionIncomplete("detail", err)
		}
	}
}

func (run *inventoryRun) topFlingBound() int {
	if run.inv.storagePage() {
		// A long mixed list can be more than a hundred rows; three detents
		// per bounded action and a 64-action cap still leaves margin.
		return 64
	}
	return maxTopFlings
}

func containsPosition(positions []int, want int) bool {
	for _, p := range positions {
		if p == want {
			return true
		}
	}
	return false
}

// recordStoragePositions maps the one storage pass to the existing
// per-slot ledger. Only a globally complete pass proves every slot: the
// mixed list is ordered by appearance, so stopping early leaves an unknown
// remainder for every slot. A scoped pass records only its declared slots.
func (run *inventoryRun) recordStoragePositions(err error) {
	order, orderErr := run.inv.positionOrder()
	if orderErr != nil {
		return
	}
	incomplete, degraded := asPositionIncomplete(err)
	stage, reasonCode, reason := "", "", ""
	if degraded {
		stage = incomplete.stage
		reasonCode = faultDiagnosticCode(incomplete.err)
		reason = incomplete.err.Error()
	} else if err != nil {
		stage = "storage"
		reasonCode = faultDiagnosticCode(err)
		reason = err.Error()
	}
	for _, pos := range order {
		entry := positionScope{
			Position:   pos,
			Top:        run.storageTopVerified,
			Bottom:     run.storageBottomVerified,
			Range:      run.rangeProof,
			Stage:      stage,
			ReasonCode: reasonCode,
			Reason:     reason,
		}
		if run.ledger != nil {
			entry.Viewports = run.viewport + 1
		}
		entry.SlotLedger, entry.Visits = run.storagePositionLedger(pos)
		// A per-slot ledger is complete only when the one pass also proved
		// the filter range and both list ends. A scoped rescan can complete
		// its declared positions this way, but updateCoverage still keeps the
		// overall session partial because fullScope is false.
		if run.storageComplete && run.storageTopVerified && run.storageBottomVerified &&
			run.rangeProof.Verified && entry.SlotLedger != nil {
			entry.Complete = true
		}
		if entry.Complete {
			entry.Stage, entry.ReasonCode, entry.Reason = "", "", ""
		}
		run.inv.recordPosition(entry)
	}
	_ = run.inv.writeScope()
}

func (run *inventoryRun) storagePositionLedger(pos int) (*slotLedgerReport, int) {
	if run.ledger == nil {
		return nil, 0
	}
	report := &slotLedgerReport{Slots: []slotEntry{}}
	visits := 0
	for _, cell := range run.ledger.cells {
		if cell.State == "" || cell.Position != pos {
			continue
		}
		report.Slots = append(report.Slots, cell)
		if cell.State == slotAdmitted || cell.State == slotNotS {
			visits++
		}
	}
	return report, visits
}
