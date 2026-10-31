package collect

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/platform"
	"github.com/LoTwT/youkai/internal/scan"
)

// panelStartPage is a scripted page state for establishList: it stays a
// verified filter panel until the close control is dispatched, then becomes
// the warehouse list.
type panelStartPage struct {
	layout      Layout
	open        bool
	frames      uint64
	dispatches  int
	closeClicks int
}

func (p *panelStartPage) Observe() (platform.Observation, error) {
	return platform.Observation{Ready: true, Geometry: 1}, nil
}

func (p *panelStartPage) Dispatch(op platform.InputOp) platform.InputResult {
	p.dispatches++
	if p.open && op.At.Eq(p.layout.PanelClose) {
		p.closeClicks++
		p.open = false
	}
	return platform.InputResult{Status: platform.InputPosted}
}

func (p *panelStartPage) Frame(ctx context.Context, req platform.FrameRequest) (platform.Frame, error) {
	if err := ctx.Err(); err != nil {
		return platform.Frame{}, err
	}
	p.frames++
	return platform.Frame{ID: p.frames, Geometry: 1, ArrivedAt: time.Now()}, nil
}

type panelStartMatcher struct {
	page *panelStartPage
}

func (m *panelStartMatcher) Match(f platform.Frame) PageMatch {
	if m.page.open {
		return PageMatch{Page: PageFilterPanel,
			Panel:    &PanelEvidence{SortVerified: true, SortLabel: sortLabelRarity},
			Evidence: "scripted verified panel"}
	}
	occupied := make([]bool, len(m.page.layout.Slots))
	return PageMatch{Page: PageWarehouse,
		List:     &ListEvidence{Occupied: occupied, Selected: -1},
		Wheel:    &WheelEvidence{Position: 1},
		Filter:   &FilterEvidence{State: FilterActive},
		Evidence: "scripted warehouse list"}
}

func (m *panelStartMatcher) MatchScroll(before, after platform.Frame) ScrollMatch {
	return ScrollMatch{Stationary: true, Evidence: "scripted stationary"}
}

func (m *panelStartMatcher) CompareDetail(a, b platform.Frame) DetailComparison {
	return DetailUndecided
}

// TestEstablishListClosesPanelOnlyWithDegradedMarker pins the panel-recovery
// boundary: a verified panel left open by a degraded previous position may
// be closed (the marker), while the same panel without that marker stays a
// session-fatal page failure and dispatches nothing.
func TestEstablishListClosesPanelOnlyWithDegradedMarker(t *testing.T) {
	l := MacCloudLayout()
	cases := []struct {
		name   string
		marker bool
	}{
		{"degraded previous position", true},
		{"no degraded previous position", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			page := &panelStartPage{layout: l, open: true}
			run := &inventoryRun{
				inv: &Inventory{
					Layout:       l,
					Matcher:      &panelStartMatcher{page: page},
					ActionBudget: 30 * time.Millisecond,
					StableFrames: 3,
					RecheckEvery: time.Millisecond,
				},
				view:              page,
				geo:               1,
				position:          1,
				pendingPanelClose: tc.marker,
			}
			err := runScriptedSession(t, run, func(ctx context.Context, run *inventoryRun) error {
				return run.establishList(ctx)
			})
			if tc.marker {
				if err != nil {
					t.Fatalf("establishList with the degraded marker: %v", err)
				}
				if page.closeClicks != 1 || page.dispatches != 1 {
					t.Fatalf("close clicks %d dispatches %d, want exactly one verified close", page.closeClicks, page.dispatches)
				}
				if run.listMatch.Page != PageWarehouse || run.pendingPanelClose {
					t.Fatalf("list match %s pending marker %t, want the warehouse and a cleared marker", run.listMatch.Page, run.pendingPanelClose)
				}
				return
			}
			var fault *scan.FaultError
			if !errors.As(err, &fault) || fault.Code != CodeNavigationMismatch {
				t.Fatalf("establishList without the marker: %v, want a navigationMismatch session fault", err)
			}
			var incomplete *positionIncompleteError
			if errors.As(err, &incomplete) {
				t.Fatalf("an unexpected panel was degraded as a position failure: %v", err)
			}
			if page.dispatches != 0 || page.closeClicks != 0 {
				t.Fatalf("an unexpected panel dispatched %d closes (%d dispatches), want none", page.closeClicks, page.dispatches)
			}
		})
	}
}

// TestAdvanceStateFaultDistinguishesWheelChangeAndUnreadableGrid pins the
// session-fatal messages for an advance action that observed a changed
// wheel or a warehouse frame without a readable grid. Neither is a
// position-level proof failure.
func TestAdvanceStateFaultDistinguishesWheelChangeAndUnreadableGrid(t *testing.T) {
	l := MacCloudLayout()
	run := &inventoryRun{inv: &Inventory{Layout: l}, position: 1}
	occupied := make([]bool, len(l.Slots))

	gridless := PageMatch{Page: PageWarehouse,
		Wheel:    &WheelEvidence{Position: 1},
		Evidence: "scripted grid unreadable"}
	err := run.advanceStateFault("the scroll", gridless)
	if !strings.Contains(err.Error(), "grid could not be verified") {
		t.Fatalf("gridless fault = %v, want the unreadable-grid cause", err)
	}
	var incomplete *positionIncompleteError
	if errors.As(err, &incomplete) {
		t.Fatalf("a gridless warehouse page was degraded: %v", err)
	}

	moved := PageMatch{Page: PageWarehouse,
		List:     &ListEvidence{Occupied: occupied, Selected: -1},
		Wheel:    &WheelEvidence{Position: 2},
		Evidence: "scripted wheel change"}
	err = run.advanceStateFault("the scroll", moved)
	if !strings.Contains(err.Error(), "changed the wheel position to 2") {
		t.Fatalf("wheel-change fault = %v, want the changed-wheel cause", err)
	}
	if errors.As(err, &incomplete) {
		t.Fatalf("a wheel change was degraded: %v", err)
	}

	unreadableWheel := PageMatch{Page: PageWarehouse,
		List:     &ListEvidence{Occupied: occupied, Selected: -1},
		Evidence: "scripted wheel unreadable"}
	err = run.advanceStateFault("the scroll", unreadableWheel)
	if !strings.Contains(err.Error(), "wheel position unreadable") {
		t.Fatalf("unreadable-wheel fault = %v, want the unreadable-wheel cause", err)
	}
	if errors.As(err, &incomplete) {
		t.Fatalf("an unreadable wheel was degraded: %v", err)
	}
}
