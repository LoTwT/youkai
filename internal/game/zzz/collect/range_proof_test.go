package collect_test

import (
	"context"
	"image"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/game/zzz/collect"
	"github.com/LoTwT/youkai/internal/platform"
)

// rangeMatcher supplies synthetic control verdicts from the scripted scene.
// Its embedded matcher still verifies the page, grid, scroll and detail.
type rangeMatcher struct {
	collect.Matcher
	game  *scriptedGame
	amend func(int, *collect.PageMatch)
}

func (m rangeMatcher) Match(f platform.Frame) collect.PageMatch {
	match := m.Matcher.Match(f)
	if match.Page != collect.PageFilterPanel {
		return match
	}
	m.game.mu.Lock()
	defer m.game.mu.Unlock()
	match.SortLabel = "稀有度"
	s := collect.FilterClear
	if m.game.sSelected {
		s = collect.FilterActive
	}
	match.Panel.Range = &collect.PanelRangeEvidence{
		ResetVerified: m.game.filter == "clear", RarityS: s,
	}
	if m.amend != nil {
		m.amend(m.game.wheel, &match)
	}
	return match
}

func TestTraverseProvesRangeInSinglePanelVisit(t *testing.T) {
	game := newScriptedGame(true)
	game.filter = "active" // An existing filter leaves the S chip off.
	h := newTraversalHarness(t, game)
	h.inv.RequirePanelRangeProof = true
	// The first panel renders a recognizable sort label only after its
	// page anchors have been visible for enough frames to settle.
	var panelFrames int
	h.inv.Matcher = rangeMatcher{Matcher: h.inv.Matcher, game: game, amend: func(pos int, m *collect.PageMatch) {
		if pos == 1 && panelFrames < h.inv.StableFrames {
			panelFrames++
			m.SortLabel = ""
		}
	}}
	h.start()
	h.endCompleted()
	scope := h.scope()
	if scope.Coverage != "complete" || scope.Sort != "verified" || scope.CountReconciled == nil || *scope.CountReconciled || len(scope.Positions) != 6 {
		t.Fatalf("scope = %+v, want complete coverage without count reconciliation", scope)
	}
	for _, entry := range scope.Positions {
		r := entry.Range
		if !r.Verified || r.Opened.FrameID == 0 || r.Opened.FrameID >= r.Reset.FrameID || r.Reset.FrameID >= r.SelectedS.FrameID ||
			r.SelectedS.FrameID >= r.ClosedFrameID || r.ClosedFunnel != collect.FilterActive ||
			r.Opened.Controls == nil || r.Opened.Controls.ResetVerified ||
			!r.Reset.EffectVerified || r.Reset.Controls == nil || !r.Reset.Controls.ResetVerified || r.Reset.Controls.RarityS != collect.FilterClear ||
			!r.SelectedS.EffectVerified || r.SelectedS.Controls == nil || r.SelectedS.Controls.RarityS != collect.FilterActive {
			t.Fatalf("position %d range proof = %+v", entry.Position, r)
		}
	}
	ops := game.opsSnapshot()
	if got := filterControlSequence(ops, game.layout); !slices.Equal(got, slices.Repeat([]string{"open", "reset", "s", "close"}, 6)) {
		t.Fatalf("filter operations = %v, want one ordered visit per position", got)
	}
	if got := countOps(ops, platform.InputDrag, image.Point{}, image.Point{}); got != 12 {
		t.Fatalf("got %d drags, want six warehouse top drags and six warehouse bottom drags", got)
	}
	if got := h.observations(); len(got) != 1 || got[0].RarityFilter != "S" {
		t.Fatalf("admitted %d observations, want one with confirmed S filter", len(got))
	}
}

func TestTraverseUnprovedRangeDegradesOnlyItsPosition(t *testing.T) {
	for _, tc := range []struct {
		name        string
		setup       func(*traversalHarness)
		amend       func(*collect.PageMatch)
		wantSClicks int
	}{
		{name: "unreadable sort after a verified position", amend: func(m *collect.PageMatch) { m.SortLabel = "" }, wantSClicks: 2},
		{name: "posted reset leaves old filter active", setup: func(h *traversalHarness) { h.game.resetDoesNothingAt = 2 }, wantSClicks: 2},
		{name: "funnel can activate while S is unreadable", amend: func(m *collect.PageMatch) {
			if m.Panel.Range.RarityS == collect.FilterActive {
				m.Panel.Range.RarityS = collect.FilterUnknown
			}
		}, wantSClicks: 3},
		{name: "posted S click leaves chip clear", setup: func(h *traversalHarness) { h.game.sClickDoesNothingAt = 2 }, wantSClicks: 3},
		{name: "pre-action S frame cannot prove selection", setup: staleRarityFrameAfterClick, wantSClicks: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			game := newScriptedGame(true)
			game.setList(2, listOf(1))
			game.setList(3, listOf(1))
			h := newTraversalHarness(t, game)
			if err := h.inv.SetScanPositions([]int{1, 2, 3}); err != nil {
				t.Fatal(err)
			}
			h.inv.RequirePanelRangeProof = true
			if tc.setup != nil {
				tc.setup(h)
			}
			h.inv.Matcher = rangeMatcher{Matcher: h.inv.Matcher, game: game, amend: func(pos int, m *collect.PageMatch) {
				if pos == 2 && tc.amend != nil {
					tc.amend(m)
				}
			}}
			h.start()
			h.endCompleted()
			entry := h.requireIncompletePosition(2, "filter", collect.CodeNavigationMismatch)
			if entry.Top || entry.Range.Verified || entry.Range.SelectedS.EffectVerified || entry.Ledger != nil {
				t.Fatalf("position 2 acquired a ledger without range proof: %+v", entry)
			}
			h.requireCompletePosition(3)
			if got := h.observations(); len(got) != 2 || got[0].Position != 1 || got[1].Position != 3 {
				t.Fatalf("admitted %d observations, want one each from positions 1 and 3", len(got))
			}
			ops := game.opsSnapshot()
			if got := countOps(ops, platform.InputClick, game.layout.FilterButton, image.Point{}); got != 3 {
				t.Fatalf("opened the panel %d times, want three", got)
			}
			if got := countOps(ops, platform.InputClick, game.layout.PanelS, image.Point{}); got != tc.wantSClicks {
				t.Fatalf("sent %d S clicks, want %d without retrying the unproved toggle", got, tc.wantSClicks)
			}
		})
	}
}

func staleRarityFrameAfterClick(h *traversalHarness) {
	h.session.FrameFn = func(ctx context.Context, req platform.FrameRequest) (platform.Frame, error) {
		f, err := h.game.frame(ctx, req)
		if err != nil {
			return f, err
		}
		h.game.mu.Lock()
		defer h.game.mu.Unlock()
		if h.game.wheel == 2 && h.game.page == pagePanel && h.game.ops[len(h.game.ops)-1].At == h.game.layout.PanelS {
			// Even matching pixels with a newer ID cannot prove the effect
			// when the capture predates the action's completion.
			f.ArrivedAt = h.game.actionFinishedAt.Add(-time.Millisecond)
		}
		return f, nil
	}
}

func TestTraverseConfirmedSRequiresActiveFunnelAfterClose(t *testing.T) {
	game := newScriptedGame(true)
	h := newTraversalHarness(t, game)
	if err := h.inv.SetScanPositions([]int{1}); err != nil {
		t.Fatal(err)
	}
	h.inv.RequirePanelRangeProof = true
	h.inv.Matcher = warehouseChangeMatcher{
		Matcher: rangeMatcher{Matcher: h.inv.Matcher, game: game},
		change:  func(m *collect.PageMatch) { m.Filter.State = collect.FilterClear },
	}
	h.start()
	snap := h.endCompleted()
	entry := h.requireIncompletePosition(1, "filter", collect.CodeNavigationMismatch)
	if !entry.Range.SelectedS.EffectVerified || entry.Range.ClosedFunnel != collect.FilterClear ||
		entry.Range.Verified || entry.Top || snap.Counts.Captured != 0 {
		t.Fatalf("clear funnel after confirmed S: range=%+v top=%t captured=%d", entry.Range, entry.Top, snap.Counts.Captured)
	}
}

type warehouseChangeMatcher struct {
	collect.Matcher
	change func(*collect.PageMatch)
}

func (m warehouseChangeMatcher) Match(f platform.Frame) collect.PageMatch {
	match := m.Matcher.Match(f)
	if match.Page == collect.PageWarehouse {
		m.change(&match)
	}
	return match
}

func TestTraverseStopsOnObservedSortOrFilterChange(t *testing.T) {
	for _, tc := range []struct {
		name, reason, sort string
		change             func(*collect.PageMatch)
	}{
		{"different sort", "observed sort label changed", "unverified", func(m *collect.PageMatch) { m.SortLabel = "fake second label" }},
		{"filter clear", "active filter changed to clear", "verified", func(m *collect.PageMatch) { m.Filter.State = collect.FilterClear }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			game := newScriptedGame(true, true)
			h := newTraversalHarness(t, game)
			changed := false
			h.inv.Matcher = warehouseChangeMatcher{Matcher: sortVerifiedMatcher{Matcher: h.inv.Matcher}, change: func(m *collect.PageMatch) {
				if changed {
					tc.change(m)
				}
			}}
			h.inv.Project = func(obs collect.Observation) (any, error) {
				changed = true
				return obs, nil
			}
			h.start()
			snap := h.endFailed(collect.CodeNavigationMismatch)
			entry := h.requireFatallyStoppedPosition(1)
			if snap.Counts.Captured != 1 || snap.Trigger == nil || !strings.Contains(snap.Trigger.Message, tc.reason) ||
				entry.Ledger == nil || len(entry.Ledger.Slots) != 2 || entry.Ledger.Slots[0].State != "admitted" || entry.Ledger.Slots[1].State != "unresolved" {
				t.Fatalf("change result = %+v ledger=%+v", snap, entry.Ledger)
			}
			if scope := h.scope(); scope.Sort != tc.sort || scope.Coverage != "partial" {
				t.Fatalf("sort=%s coverage=%s", scope.Sort, scope.Coverage)
			}
			if got := countSlotClicks(game.opsSnapshot(), game.layout); got != 1 {
				t.Fatalf("continued clicking after the change: %d target clicks", got)
			}
		})
	}
}
