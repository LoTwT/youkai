package collect

import (
	"context"
	"fmt"

	"github.com/LoTwT/youkai/internal/platform"
	"github.com/LoTwT/youkai/internal/scan"
)

// PanelRangeEvidence reports the visible reset result and S selection.
// ResetVerified requires calibrated evidence that the filters are reset.
// RarityS comes from the S chip's fill and stays unknown when unreadable.
// Neither verdict can be inferred from input delivery.
type PanelRangeEvidence struct {
	ResetVerified bool        `json:"resetVerified"`
	RarityS       FilterState `json:"rarityS,omitempty"`
}

func (p *PanelRangeEvidence) resetConfirmed() bool {
	return p != nil && p.ResetVerified && p.RarityS == FilterClear
}

func (p *PanelRangeEvidence) raritySSelected() bool {
	return p != nil && p.RarityS == FilterActive
}

type panelRangeSample struct {
	FrameID        uint64              `json:"frameId"`
	SortLabel      string              `json:"sortLabel,omitempty"`
	Controls       *PanelRangeEvidence `json:"controls,omitempty"`
	EffectVerified bool                `json:"effectVerified,omitempty"`
}

type positionRange struct {
	Verified      bool              `json:"verified"`
	Opened        *panelRangeSample `json:"opened,omitempty"`
	Reset         *panelRangeSample `json:"reset,omitempty"`
	SelectedS     *panelRangeSample `json:"selectedS,omitempty"`
	ClosedFrameID uint64            `json:"closedFrameId,omitempty"`
	ClosedFunnel  FilterState       `json:"closedFunnel,omitempty"`
}

func panelRangeObservation(frame platform.Frame, match PageMatch) *panelRangeSample {
	if match.Page != PageFilterPanel || match.Panel == nil {
		return nil
	}
	sample := &panelRangeSample{FrameID: frame.ID, SortLabel: observedSortLabel(match)}
	if match.Panel.Range != nil {
		controls := *match.Panel.Range
		sample.Controls = &controls
	}
	return sample
}

func (run *inventoryRun) panelEffectObservation(out awaitOutcome) *panelRangeSample {
	sample := panelRangeObservation(out.frame, out.match)
	if sample != nil {
		sample.EffectVerified = run.inv.RequirePanelRangeProof && out.class == awaitStable
	}
	return sample
}

func observedSortLabel(m PageMatch) string {
	if (m.Page == PageWarehouse || m.Page == PageFilterPanel) && m.SortLabel != "" {
		return m.SortLabel
	}
	if m.Page == PageFilterPanel && m.Panel != nil && m.Panel.SortVerified {
		return m.Panel.SortLabel
	}
	return ""
}

// matchFrame applies the scan-wide gates to every observed frame, including
// the next position's preparation. A template miss or a hidden label is
// absence of proof. Only a directly identified different label is a change.
func (run *inventoryRun) matchFrame(frame platform.Frame) (PageMatch, error) {
	m := run.inv.matcher().Match(frame)
	if run.inv.EnableBoundedRecovery && run.rangeProof.Verified {
		fresh := !frame.Reused && frame.ID > run.newestFrameID && frame.ArrivedAt.After(run.lastArrived)
		idle := frame.Reused && frame.ID == run.newestFrameID && frame.ArrivedAt.Equal(run.lastArrived)
		if !fresh && !idle {
			return m, scan.NewFault(CodePositionLost, fmt.Errorf("collect: stale frame %d at the recovery-enabled position", frame.ID))
		}
	}
	if run.rebuildGuard != nil {
		if err := run.checkRebuildFrame(frame, m); err != nil {
			return m, err
		}
	}
	run.newestFrameID = max(run.newestFrameID, frame.ID)
	label := observedSortLabel(m)
	if run.sortLabel == "" {
		run.sortLabel = label
	}
	if label != "" && label != run.sortLabel {
		run.inv.scope.Sort = scopeSortUnverified
		run.inv.scope.SortLabel = ""
		run.inv.updateCoverage()
		_ = run.inv.writeScope()
		return m, scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
			"collect: wheel position %d frame %d: the observed sort label changed from %q to %q", run.position, frame.ID, run.sortLabel, label))
	}
	if run.filterActive && m.Page == PageWarehouse && m.Filter != nil && m.Filter.State == FilterClear {
		return m, scan.NewFault(CodeNavigationMismatch, fmt.Errorf(
			"collect: wheel position %d frame %d: the active filter changed to clear", run.position, frame.ID))
	}
	return m, nil
}

func (run *inventoryRun) rangeFailure(reason string) error {
	run.pendingPanelClose = true
	return positionFault("filter", CodeNavigationMismatch, "collect: wheel position %d: %s", run.position, reason)
}

func (run *inventoryRun) awaitPanelEffect(ctx context.Context, controlsMatch func(*PanelRangeEvidence) bool) awaitOutcome {
	if !run.inv.RequirePanelRangeProof {
		return run.awaitSettled(ctx, run.panelVerdict)
	}
	return run.awaitRangeStep(ctx, func(_ platform.Frame, m PageMatch) bool {
		return run.panelVerdict(m) && controlsMatch(m.Panel.Range)
	}, false)
}

// awaitRangeStep requires the requested effect and rejects a stable
// contradiction within the same panel visit. An unreadable frame or an
// isolated animation verdict does not confirm either result.
func (run *inventoryRun) awaitRangeStep(ctx context.Context, expect func(platform.Frame, PageMatch) bool, closing bool) awaitOutcome {
	current := run.afterInput(func(platform.Frame, PageMatch) bool { return true })
	contradictions := 0
	return run.awaitCheckedFrame(ctx, func(f platform.Frame, m PageMatch) (bool, error) {
		if !current(f, m) {
			contradictions = 0
			return false, nil
		}
		if m.Page == PageWarehouse && !run.positionListVerdict(m) {
			return false, run.advanceStateFault("filter preparation", m)
		}
		earlyClose := !closing && run.listVerdict(m)
		clearedS := closing && run.panelVerdict(m) && m.Panel.Range != nil && m.Panel.Range.RarityS == FilterClear
		if !earlyClose && !clearedS {
			contradictions = 0
			return expect(f, m), nil
		}
		contradictions++
		if contradictions < run.inv.stableFrames() {
			return false, nil
		}
		if earlyClose {
			return false, positionFault("filter", CodeNavigationMismatch,
				"collect: wheel position %d: the filter panel closed before its effect was verified (frame %d)", run.position, f.ID)
		}
		return false, run.rangeFailure(fmt.Sprintf("S was cleared before the filter panel closed (frame %d)", f.ID))
	}, nil)
}

// afterInput requires a fresh post-action frame before an idle confirmation
// may contribute effect evidence for that action.
func (run *inventoryRun) afterInput(expect func(platform.Frame, PageMatch) bool) func(platform.Frame, PageMatch) bool {
	beforeID, finished := run.lastFrame.ID, run.lastArrived
	var verifiedID uint64
	return func(f platform.Frame, m PageMatch) bool {
		if f.Reused {
			return verifiedID != 0 && f.ID == verifiedID && expect(f, m)
		}
		if f.ID <= beforeID || !f.ArrivedAt.After(finished) {
			return false
		}
		verifiedID = f.ID
		return expect(f, m)
	}
}
