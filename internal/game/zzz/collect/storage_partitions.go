package collect

import (
	"context"
	"fmt"
	"image"
	"image/color"

	"github.com/LoTwT/youkai/internal/platform"
	"github.com/LoTwT/youkai/internal/scan"
)

func storagePartitionChip(i int) image.Point {
	return image.Pt(1425+280*(i%2), 766+58*(i/2))
}

func (m *PixelMatcher) storagePartitionMask(img *image.RGBA) *uint8 {
	colors := map[string]color.RGBA{"clear": {R: 28, G: 28, B: 28, A: 255}, "active": {R: 0, G: 139, B: 255, A: 255}}
	var mask uint8
	for i := 0; i < 6; i++ {
		p := storagePartitionChip(i).Sub(image.Pt(95, 0))
		r := image.Rect(p.X-2, p.Y-2, p.X+3, p.Y+3)
		if !r.In(img.Bounds()) {
			return nil
		}
		switch m.classify(img, ColorProbe{At: r, Tol: 10, Colors: colors}) {
		case "clear":
		case "active":
			mask |= 1 << i
		default:
			return nil
		}
	}
	return &mask
}

func (run *inventoryRun) prepareStoragePartitions(ctx context.Context) error {
	matcher, ok := run.inv.matcher().(*ScrollEvidenceMatcher)
	if !run.inv.storagePage() || run.inv.fullScope() || !ok || matcher == nil {
		return nil
	}
	controls := run.listMatch.Panel
	if controls == nil || controls.Range == nil || controls.Range.StoragePartitions == nil ||
		*controls.Range.StoragePartitions != 0 || !controls.Range.raritySSelected() {
		return run.rangeFailure("the six reset partition chips are not independently readable")
	}
	var expected uint8
	for _, position := range run.inv.positions {
		if err := run.observeReady(ctx); err != nil {
			return err
		}
		if !run.h.InputPermitted() {
			return ctx.Err()
		}
		res := run.view.Dispatch(platform.InputOp{Kind: platform.InputClick, Geometry: run.geo, FrameID: run.lastFrame.ID, At: storagePartitionChip(position - 1)})
		if err := run.dispatchOutcome(ctx, res); err != nil {
			return err
		}
		expected |= 1 << (position - 1)
		out := run.awaitPanelEffect(ctx, func(p *PanelRangeEvidence) bool {
			return p.raritySSelected() && p.StoragePartitions != nil && *p.StoragePartitions == expected
		})
		switch out.class {
		case awaitStable:
			run.listMatch, run.lastFrame = out.match, out.frame
			run.rangeProof.SelectedPartitions = run.panelEffectObservation(out)
		case awaitCanceled, awaitFrameErr:
			return out.err
		case awaitGeometry:
			return run.geometryChanged(out.frame)
		default:
			return run.rangeFailure("the requested partition selection did not settle")
		}
	}
	return nil
}

func (run *inventoryRun) activateStoragePartitions() error {
	sample := run.rangeProof.SelectedPartitions
	if sample == nil {
		return nil
	}
	m := run.inv.matcher().(*ScrollEvidenceMatcher)
	var expected uint8
	for _, p := range run.inv.positions {
		expected |= 1 << (p - 1)
	}
	if sample.Controls == nil || sample.Controls.StoragePartitions == nil || *sample.Controls.StoragePartitions != expected || !sample.EffectVerified ||
		!storageListInPartitions(run.listMatch.List, expected, true) {
		return scan.NewFault(CodePositionLost, fmt.Errorf("collect: the closed warehouse list did not corroborate the requested partition filters"))
	}
	clone := *m
	clone.storagePartitions = expected
	pixels := *m.PixelMatcher
	pixels.partitionOneBadge = expected == 1
	clone.PixelMatcher = &pixels
	run.inv.Matcher = &clone
	return nil
}

func storageListInPartitions(list *ListEvidence, wanted uint8, requireKnown bool) bool {
	if list == nil || len(list.Occupied) != len(list.Positions) {
		return false
	}
	for i, occupied := range list.Occupied {
		if !occupied {
			continue
		}
		p := list.Positions[i]
		if p.known() {
			if wanted&(1<<(p.Position-1)) == 0 {
				return false
			}
		} else if requireKnown {
			return false
		}
	}
	return true
}

// In a filtered list badge equality is never evidence of item identity.
// Require independent scrollbar travel, intact overlapping cells, and a
// distinguishable, information-rich correlation at the same whole-row offset.
func (m *ScrollEvidenceMatcher) filteredStorageScroll(before, after platform.Frame, thumbB, thumbA thumbReading, metrics *ScrollMetrics) (ScrollMatch, bool) {
	a, b := m.PixelMatcher.Match(before), m.PixelMatcher.Match(after)
	if a.Page != PageDriveStorage || b.Page != PageDriveStorage || a.Filter == nil || b.Filter == nil || a.Filter.State != FilterActive || b.Filter.State != FilterActive ||
		!storageListInPartitions(a.List, m.storagePartitions, true) || !storageListInPartitions(b.List, m.storagePartitions, true) || metrics == nil {
		return ScrollMatch{}, false
	}
	var rows int
	for step := -m.C.ScrollMaxRows; step <= m.C.ScrollMaxRows; step++ {
		if _, ok, _ := m.thumbTravelOfStrict(thumbB, thumbA, step); ok {
			if rows != 0 {
				return ScrollMatch{}, false
			}
			rows = step
		}
	}
	score, ok := metrics.RowNCC[rows]
	if rows == 0 || !ok || score < m.C.ScrollSelectionNCC || metrics.InfoStd < m.C.ScrollInfoStdMin {
		return ScrollMatch{}, false
	}
	for other, rival := range metrics.RowNCC {
		if other != rows && score-rival < m.C.ScrollNCCMargin-nccTieEpsilon {
			return ScrollMatch{}, false
		}
	}
	matched := 0
	for from, occ := range a.List.Occupied {
		to := from - rows*m.L.Columns
		if to < 0 || to >= len(b.List.Occupied) {
			continue
		}
		if occ != b.List.Occupied[to] {
			return ScrollMatch{}, false
		}
		if occ {
			if a.List.Positions[from] != b.List.Positions[to] {
				return ScrollMatch{}, false
			}
			matched++
		}
	}
	if matched == 0 {
		return ScrollMatch{}, false
	}
	return m.displaced(before.ID, after.ID, rows, ScrollBasisStoragePositions, metrics,
		fmt.Sprintf("verified %d-row filtered-storage displacement from scrollbar travel and aligned card pixels (%d occupied correspondences)", rows, matched)), true
}

// A verified game-side partition still has the same row and item
// evidence as an unfiltered scan. Use the existing bounded detail
// wait there too. A logical-only partial scope has no such proof.
func (run *inventoryRun) storageDetailRecoveryEnabled() bool {
	if run.inv == nil || !run.inv.storagePage() {
		return false
	}
	if run.inv.fullScope() {
		return true
	}
	matcher, ok := run.inv.matcher().(*ScrollEvidenceMatcher)
	return ok && matcher.storagePartitions != 0 && run.rangeProof.Verified && run.filterActive
}
