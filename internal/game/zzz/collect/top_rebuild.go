package collect

import (
	"bytes"
	"context"
	"fmt"
	"image"

	"github.com/LoTwT/youkai/internal/platform"
	"github.com/LoTwT/youkai/internal/scan"
)

type topRebuildEvidence struct {
	Reason       string            `json:"reason"`
	Settled      uint64            `json:"settledFrameId,omitempty"`
	Closed       uint64            `json:"closedFrameId,omitempty"`
	Alignment    string            `json:"alignment,omitempty"`
	Before       uint64            `json:"beforeFrameId"`
	Panel        *panelRangeSample `json:"panel,omitempty"`
	OtherFilters FilterState       `json:"otherFilters,omitempty"`
	Confirmed    uint64            `json:"confirmedFrameId,omitempty"`
}

type rebuildFrameGuard struct {
	panelAllowed         bool
	panelRequired        bool
	closingPanel         bool
	otherFilters         FilterState
	selected             int
	before               platform.Frame
	prover               topRebuildProver
	thumbStart, thumbEnd int
}

type topRebuildProver interface {
	zeroDisplacementProver
	thumbSegment(*image.RGBA) (int, int, bool)
	thumbMoved(thumbReading, thumbReading) bool
	// CurrentNonSFilters reads all filters other than S from the current
	// panel. Missing or obscured controls must yield FilterUnknown. A
	// previous reset, rarity selection and blue funnel cannot supply this.
	CurrentNonSFilters(platform.Frame) FilterState
}

// Opening and inspecting the panel must not alter its selections. A positive
// contradiction stops immediately, including while waiting to close it.
func (run *inventoryRun) checkRebuildFrame(f platform.Frame, m PageMatch) error {
	g := run.rebuildGuard
	if f.Reused {
		return scan.NewFault(CodePositionLost, fmt.Errorf("collect: stale frame %d during top rebuild", f.ID))
	}
	if g.panelAllowed && run.panelVerdict(m) {
		p := m.Panel.Range
		g.otherFilters = g.prover.CurrentNonSFilters(f)
		if (p != nil && (p.ResetVerified || p.RarityS == FilterClear)) || g.otherFilters == FilterActive {
			return scan.NewFault(CodeNavigationMismatch, fmt.Errorf("collect: frame %d shows a changed filter during top rebuild", f.ID))
		}
		if g.closingPanel && (!p.raritySSelected() || observedSortLabel(m) == "" || g.otherFilters != FilterClear) {
			run.rebuildEvidence.Panel = panelRangeObservation(f, m)
			run.rebuildEvidence.OtherFilters = g.otherFilters
			return run.rangeFailure(fmt.Sprintf("frame %d no longer proves the range while closing the rebuild panel", f.ID))
		}
		return nil
	}
	if g.panelRequired || !run.positionListVerdict(m) || m.Filter == nil || m.Filter.State != FilterActive {
		return scan.NewFault(CodeNavigationMismatch, fmt.Errorf("collect: frame %d no longer proves the rebuild page, wheel or filter", f.ID))
	}
	s, e, readable := g.prover.thumbSegment(f.Image)
	if readable && (e-s != g.thumbEnd-g.thumbStart || (m.List.Top && (s != g.thumbStart || e != g.thumbEnd))) {
		return scan.NewFault(CodePositionLost, fmt.Errorf("collect: frame %d contradicts the original list extent", f.ID))
	}
	if m.List.Top && (!run.ledger.agreesWith(m.List.Occupied) || m.List.Selected != g.selected || m.List.Bottom) {
		return scan.NewFault(CodePositionLost, fmt.Errorf("collect: frame %d contradicts the selected cell or extent of the original top viewport", f.ID))
	}
	if m.List.Top && run.inv.matcher().CompareDetail(g.before, f) == DetailDifferent {
		return scan.NewFault(CodePositionLost, fmt.Errorf("collect: the selected top detail changed on frame %d", f.ID))
	}
	return nil
}

// Only the first advance has a retained top witness for every resolved row.
// At later failures the ledger records ordinals but cannot join the old prefix
// to the new walk. Never manufacture that join from matching occupancy.
func (run *inventoryRun) rebuildFirstViewport(ctx context.Context, cause error) error {
	failure, scoped := asPositionIncomplete(cause)
	prover, calibrated := run.inv.matcher().(topRebuildProver)
	if !scoped || failure.stage != "scroll" || !calibrated || !run.inv.EnableBoundedRecovery || !run.rangeProof.Verified ||
		run.topRebuildUsed || run.viewport != 0 || run.ledger.rowBase != 0 || !run.selectionTracked {
		return cause
	}
	before, page := run.lastFrame, run.listMatch
	start, end, ok := prover.thumbSegment(before.Image)
	if !ok {
		return cause
	}
	if !run.positionListVerdict(page) || !page.List.Top || page.List.Bottom ||
		page.List.Selected < 0 || page.List.Selected >= len(page.List.Occupied) ||
		!run.ledger.agreesWith(page.List.Occupied) || run.ledger.visible()[page.List.Selected].State != slotAdmitted {
		return cause
	}
	run.topRebuildUsed = true
	evidence := &topRebuildEvidence{Before: before.ID, Reason: cause.Error()}
	run.rebuildEvidence = evidence
	g := &rebuildFrameGuard{selected: page.List.Selected, before: before, prover: prover, thumbStart: start, thumbEnd: end}
	run.rebuildGuard = g
	originalRange := run.rangeProof
	defer func() {
		run.rangeProof = originalRange
		run.rebuildGuard = nil
	}()
	// The old frame is a comparison witness only. Establish the current
	// warehouse from fresh observations before sending even a panel click.
	run.listMatch, run.lastFrame = PageMatch{}, platform.Frame{}
	var settled platform.Frame
	out := run.awaitSettledFrame(ctx, func(f platform.Frame, m PageMatch) bool {
		s, e, ok := prover.thumbSegment(f.Image)
		if !ok || m.List.Top || s <= start || e-s != end-start ||
			!prover.thumbMoved(thumbReading{start, end, true}, thumbReading{s, e, true}) {
			settled = platform.Frame{}
			return false
		}
		if settled.ID != 0 {
			if same, _ := prover.zeroDisplacementProven(settled, f); same {
				return true
			}
		}
		settled = f
		return false
	}, nil)
	switch out.class {
	case awaitStable:
		run.listMatch, run.lastFrame = out.match, out.frame
		evidence.Settled = out.frame.ID
	case awaitFrameErr, awaitCanceled:
		return out.err
	case awaitGeometry:
		return run.geometryChanged(out.frame)
	default:
		if !run.positionListVerdict(out.match) {
			return scan.NewFault(CodeNavigationMismatch, fmt.Errorf("collect: the rebuild never obtained a current warehouse frame"))
		}
		return cause
	}
	g.panelAllowed = true
	if err := run.openFilterPanel(ctx); err != nil {
		return err
	}
	g.panelRequired = true
	out = run.awaitSettledFrame(ctx, run.afterInput(func(_ platform.Frame, m PageMatch) bool {
		return run.panelVerdict(m) && m.Panel.Range.raritySSelected() && observedSortLabel(m) == run.sortLabel &&
			g.otherFilters == FilterClear
	}), nil)
	evidence.Panel = panelRangeObservation(out.frame, out.match)
	evidence.OtherFilters = g.otherFilters
	switch out.class {
	case awaitStable:
		run.listMatch, run.lastFrame = out.match, out.frame
	case awaitCanceled, awaitFrameErr:
		return out.err
	case awaitGeometry:
		return run.geometryChanged(out.frame)
	default:
		if !run.panelVerdict(out.match) {
			return scan.NewFault(CodeNavigationMismatch, fmt.Errorf("collect: no current panel frame verified the rebuild range"))
		}
		return run.rangeFailure("the reopened panel could not prove the original S-only range and sort label")
	}
	g.panelRequired = false
	g.closingPanel = true
	if err := run.closeFilterPanel(ctx); err != nil {
		return err
	}
	evidence.Closed = run.lastFrame.ID
	g.panelAllowed = false
	frames, err := run.scrollToTop(ctx)
	run.dumpFailure(frames)
	if err != nil {
		return err
	}
	if !run.listMatch.List.Top || run.listMatch.List.Selected != g.selected {
		return cause
	}
	s, e, ok := prover.thumbSegment(run.lastFrame.Image)
	if !ok || s != start || e != end || run.inv.matcher().CompareDetail(before, run.lastFrame) != DetailSame {
		return positionFault("rebuild", CodePositionLost, "collect: rebuilt top differs in scrollbar extent or selected detail from frame %d", before.ID)
	}
	same, why := prover.zeroDisplacementProven(before, run.lastFrame)
	evidence.Alignment = why
	if !same {
		return positionFault("rebuild", CodePositionLost, "collect: original and rebuilt top frames %d and %d could not be aligned: %s", before.ID, run.lastFrame.ID, why)
	}
	for i, cell := range run.ledger.visible() {
		if cell.State != slotAdmitted && cell.State != slotNotS {
			continue
		}
		if !sameRebuiltCell(before.Image, run.lastFrame.Image, run.inv.Layout.Slots[i]) {
			return positionFault("rebuild", CodePositionLost, "collect: rebuilt top cell %d cannot be matched between frames %d and %d", i, before.ID, run.lastFrame.ID)
		}
	}
	evidence.Confirmed = run.lastFrame.ID
	// The existing ledger still describes exactly this top. nextSlot skips
	// resolved entries; only applyTransition may add subsequent rows.
	return nil
}

// Compare the calibrated cell in place after proving range and alignment.
// Until animation tolerance is calibrated per profile, even a flashing badge
// can make this recovery inconclusive. Motion outside the cells is irrelevant here.
func sameRebuiltCell(before, after *image.RGBA, cell image.Rectangle) bool {
	if cell.Empty() || !cell.In(before.Bounds()) || !cell.In(after.Bounds()) {
		return false
	}
	width := cell.Dx() * 4
	for y := cell.Min.Y; y < cell.Max.Y; y++ {
		i, j := before.PixOffset(cell.Min.X, y), after.PixOffset(cell.Min.X, y)
		if !bytes.Equal(before.Pix[i:i+width], after.Pix[j:j+width]) {
			return false
		}
	}
	return true
}
