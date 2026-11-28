package collect

import (
	"image"
	"testing"

	"github.com/LoTwT/youkai/internal/platform"
)

func storageBadgeTransientFixture() (*inventoryRun, platform.Frame, PageMatch, int) {
	layout := StorageLayout()
	occupied := make([]bool, len(layout.Slots))
	positions := make([]int, len(layout.Slots))
	listPositions := make([]PositionVerdict, len(layout.Slots))
	for i := range occupied {
		occupied[i] = true
		positions[i] = i%6 + 1
		listPositions[i] = PositionVerdict{Position: positions[i]}
	}
	target := 19
	run := &inventoryRun{
		inv:          &Inventory{Layout: layout},
		geo:          1,
		position:     positions[target],
		filterActive: true,
		rangeProof:   positionRange{Verified: true},
		ledger:       newSlotLedgerWithPositions(layout.Columns, occupied, positions, 1),
	}
	frame := platform.Frame{ID: 2, Geometry: 1, Content: layout.Content, Image: image.NewRGBA(layout.Content)}
	match := PageMatch{
		Page: PageDriveStorage,
		List: &ListEvidence{
			Occupied:  occupied,
			Positions: listPositions,
			Selected:  target,
		},
		Detail: &DetailEvidence{Position: PositionVerdict{Position: positions[target]}},
		Filter: &FilterEvidence{State: FilterActive},
	}
	return run, frame, match, target
}

func TestStorageBadgeTransientRequiresExactScope(t *testing.T) {
	run, frame, match, target := storageBadgeTransientFixture()
	match.List.Positions[20] = PositionVerdict{Conflict: true}
	if !run.storageBadgeTransientFrame(frame, match, target) {
		t.Fatal("non-target conflict inside the proven target action was not recognized")
	}

	for _, tc := range []struct {
		name string
		edit func(*inventoryRun, *platform.Frame, *PageMatch)
	}{
		{"target conflict", func(_ *inventoryRun, _ *platform.Frame, m *PageMatch) {
			m.List.Positions[target] = PositionVerdict{Conflict: true}
		}},
		{"known wrong non-target", func(_ *inventoryRun, _ *platform.Frame, m *PageMatch) {
			m.List.Positions[20] = PositionVerdict{Position: 5}
		}},
		{"occupancy changed", func(_ *inventoryRun, _ *platform.Frame, m *PageMatch) {
			m.List.Occupied[20] = false
		}},
		{"filter changed", func(_ *inventoryRun, _ *platform.Frame, m *PageMatch) {
			m.Filter.State = FilterClear
		}},
		{"page changed", func(_ *inventoryRun, _ *platform.Frame, m *PageMatch) {
			m.Page = PageWarehouse
		}},
		{"selection changed", func(_ *inventoryRun, _ *platform.Frame, m *PageMatch) {
			m.List.Selected = 18
		}},
		{"client changed", func(_ *inventoryRun, f *platform.Frame, _ *PageMatch) {
			f.Content = f.Content.Inset(1)
		}},
		{"all target badges known", func(_ *inventoryRun, _ *platform.Frame, m *PageMatch) {
			m.List.Positions[20] = PositionVerdict{Position: positionsForTest(20)}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run, frame, match, target := storageBadgeTransientFixture()
			match.List.Positions[20] = PositionVerdict{Conflict: true}
			tc.edit(run, &frame, &match)
			if run.storageBadgeTransientFrame(frame, match, target) {
				t.Fatal("scope violation was accepted as a transient badge frame")
			}
		})
	}
}

func positionsForTest(i int) int { return i%6 + 1 }

func TestStorageBadgeDispositionRejectsBeforeTargetSelection(t *testing.T) {
	run, frame, match, target := storageBadgeTransientFixture()
	match.List.Selected = target - 1
	match.List.Positions[20] = PositionVerdict{Position: 4}
	if unconfirmed, ready, _, err := run.storageBadgeDisposition(frame, platform.Frame{}, nil, match, target, false, false, platform.Frame{}); err == nil {
		t.Fatalf("known wrong badge before target selection was not fatal: unconfirmed=%t ready=%t", unconfirmed, ready)
	}
	match.List.Positions[20] = PositionVerdict{Conflict: true}
	if unconfirmed, _, _, err := run.storageBadgeDisposition(frame, platform.Frame{}, nil, match, target, false, false, platform.Frame{}); err != nil || unconfirmed {
		t.Fatalf("pre-selection conflict = unconfirmed %t err %v, want a plain non-target frame", unconfirmed, err)
	}
}

func TestStorageBadgeDispositionRejectsDetailOrSelectionDrift(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*PageMatch)
	}{
		{"selection returned", func(m *PageMatch) { m.List.Selected = 18 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run, frame, match, target := storageBadgeTransientFixture()
			match.List.Positions[20] = PositionVerdict{Conflict: true}
			tc.edit(&match)
			if _, _, _, err := run.storageBadgeDisposition(frame, platform.Frame{}, nil, match, target, true, false, platform.Frame{}); err == nil {
				t.Fatal("drift after target selection was not fatal")
			}
		})
	}
}

func TestStorageBadgeDispositionRejectsNewWrongDetail(t *testing.T) {
	run, frame, match, target := storageBadgeTransientFixture()
	match.List.Positions[20] = PositionVerdict{Conflict: true}
	match.Detail.Position = PositionVerdict{Position: 6}
	baselineDetail := &DetailEvidence{Position: PositionVerdict{Position: 1}}
	if _, _, _, err := run.storageBadgeDisposition(frame, platform.Frame{ID: 1}, baselineDetail, match, target, true, true, platform.Frame{}); err == nil {
		t.Fatal("new wrong detail slot was not fatal")
	}
}

func TestStorageBadgeDispositionRejectsPageDriftAfterTarget(t *testing.T) {
	run, frame, match, target := storageBadgeTransientFixture()
	match.Page = PageWarehouse
	match.List.Positions[20] = PositionVerdict{Conflict: true}
	if _, _, _, err := run.storageBadgeDisposition(frame, platform.Frame{}, nil, match, target, true, false, platform.Frame{}); err == nil {
		t.Fatal("page drift after target selection was not fatal")
	}
}

type badgeTransientSameDetailMatcher struct{ Uncalibrated }

func (badgeTransientSameDetailMatcher) CompareDetail(platform.Frame, platform.Frame) DetailComparison {
	return DetailSame
}

func TestStorageBadgeDispositionAllowsOldDetailDelay(t *testing.T) {
	run, frame, match, target := storageBadgeTransientFixture()
	run.inv.Matcher = badgeTransientSameDetailMatcher{}
	match.List.Positions[20] = PositionVerdict{Conflict: true}
	match.Detail.Position = PositionVerdict{Position: 6}
	if unconfirmed, ready, deferred, err := run.storageBadgeDisposition(frame, platform.Frame{ID: 1}, nil, match, target, true, false, platform.Frame{}); err != nil || unconfirmed || ready || !deferred {
		t.Fatalf("old-detail delay = unconfirmed %t ready %t err %v, want ordinary wait", unconfirmed, ready, err)
	}
}

func TestStorageBadgeDispositionRejectsConfirmedPageChangeBeforeTarget(t *testing.T) {
	run, frame, match, target := storageBadgeTransientFixture()
	match.Page = PageFilterPanel
	match.List = nil
	if _, _, _, err := run.storageBadgeDisposition(frame, platform.Frame{}, nil, match, target, false, false, platform.Frame{}); err == nil {
		t.Fatal("confirmed filter-panel page before target selection was not fatal")
	}
}

func TestStorageBadgeDispositionRejectsBaselineEqualAfterTargetDetail(t *testing.T) {
	run, frame, match, target := storageBadgeTransientFixture()
	match.List.Positions[20] = PositionVerdict{Conflict: true}
	match.Detail.Position = PositionVerdict{Position: 1}
	baselineDetail := &DetailEvidence{Position: PositionVerdict{Position: 1}}
	if _, _, _, err := run.storageBadgeDisposition(frame, platform.Frame{ID: 1}, baselineDetail, match, target, true, true, platform.Frame{}); err == nil {
		t.Fatal("different detail after a confirmed target detail was not fatal")
	}
}

type badgeTransientDifferentDetailMatcher struct{ Uncalibrated }

func (badgeTransientDifferentDetailMatcher) CompareDetail(platform.Frame, platform.Frame) DetailComparison {
	return DetailDifferent
}

func TestStorageBadgeDispositionWaitsWithoutBaselineBadge(t *testing.T) {
	run, frame, match, target := storageBadgeTransientFixture()
	run.inv.Matcher = badgeTransientDifferentDetailMatcher{}
	match.List.Positions[20] = PositionVerdict{Conflict: true}
	match.Detail.Position = PositionVerdict{Position: 6}
	if unconfirmed, ready, deferred, err := run.storageBadgeDisposition(frame, platform.Frame{ID: 1}, nil, match, target, true, false, platform.Frame{}); err != nil || unconfirmed || ready || !deferred {
		t.Fatalf("missing baseline detail = unconfirmed %t ready %t deferred %t err %v, want wait", unconfirmed, ready, deferred, err)
	}
}

func TestStoragePartitionWaitRequiresVerifiedScope(t *testing.T) {
	run, frame, match, target := storageBadgeTransientFixture()
	run.inv.positions = []int{1}
	run.position = 1
	slots := make([]int, len(match.List.Occupied))
	for i := range slots {
		slots[i] = 1
		match.List.Positions[i] = PositionVerdict{Position: 1}
	}
	run.ledger = newSlotLedgerWithPositions(run.inv.Layout.Columns, match.List.Occupied, slots, 1)
	match.Detail.Position = PositionVerdict{Position: 1}
	match.List.Positions[20] = PositionVerdict{Conflict: true}
	m := NewScrollEvidenceMatcher(NewPixelMatcher(StorageLayout(), StorageCalibration()))
	run.inv.Matcher = m
	if run.storageBadgeTransientFrame(frame, match, target) {
		t.Fatal("unverified partition entered the wait")
	}
	m.storagePartitions = 1
	if !run.storageBadgeTransientFrame(frame, match, target) {
		t.Fatal("verified partition did not defer the unreadable non-target badge")
	}
	if run.checkLedgerViewport(match) == nil {
		t.Fatal("unreadable frame satisfied the admission ledger")
	}
	match.List.Positions[20] = PositionVerdict{Position: 2}
	if _, _, _, err := run.storageBadgeDisposition(frame, platform.Frame{}, nil, match, target, true, true, platform.Frame{}); err == nil {
		t.Fatal("known wrong badge was deferred")
	}
	match.List.Positions[20] = PositionVerdict{Position: 1}
	if err := run.checkLedgerViewport(match); err != nil {
		t.Fatal(err)
	}
}
