package collect

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/platform"
	"github.com/LoTwT/youkai/internal/platform/platformfake"
)

func TestWindowsPanelRangeRejectsStableContradiction(t *testing.T) {
	p := windowsNativeRangeProfile(t)
	frames := loadWindowsPanelFrames(t, p.Layout)
	warehouse := loadWindowsWarehouseFrame(t, p.Layout)
	for _, tc := range []struct {
		name              string
		insertAfterAction int
		intervening       platform.Frame
		frames            int
		failure           string
		panelLeftOpen     bool
	}{
		{"closed warehouse before S proof", 3, warehouse, 3, "closed before its effect was verified", false},
		{"reset after S proof", 4, frames["reset"], 3, "S was cleared", true},
		{"one clear frame during closure", 4, frames["reset"], 1, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inv := p.Inventory(false)
			steps := []platform.Frame{frames["residual"], frames["reset"], frames["s-only"], warehouse}
			var actions, samples int
			nextID := warehouse.ID
			start := time.Now()
			var finished time.Time
			view := platformfake.NewSession()
			view.DispatchFn = func(platform.InputOp) platform.InputResult {
				actions++
				samples = 0
				finished = start.Add(time.Duration(actions) * time.Second)
				return platform.InputResult{Status: platform.InputPosted, FinishedAt: finished}
			}
			view.FrameFn = func(_ context.Context, req platform.FrameRequest) (platform.Frame, error) {
				if actions < 1 || actions > len(steps) {
					return platform.Frame{}, fmt.Errorf("unexpected action %d", actions)
				}
				samples++
				nextID++
				f := steps[actions-1]
				if actions == tc.insertAfterAction && samples <= tc.frames {
					f = tc.intervening
				}
				f.ID = nextID
				f.ArrivedAt = finished.Add(time.Duration(samples) * time.Millisecond)
				if !f.ArrivedAt.After(req.After) {
					return platform.Frame{}, fmt.Errorf("probe produced a stale frame")
				}
				return f, nil
			}
			run := &inventoryRun{inv: inv, view: view, geo: 1, position: 3, lastFrame: warehouse, listMatch: inv.Matcher.Match(warehouse)}
			err := runScriptedSession(t, run, func(ctx context.Context, run *inventoryRun) error { return run.resetFilters(ctx) })
			if tc.failure == "" {
				if err != nil || !run.rangeProof.Verified || actions != 4 {
					t.Fatalf("ordinary closure after a transient frame: actions=%d proof=%+v err=%v", actions, run.rangeProof, err)
				}
				return
			}
			incomplete, scoped := asPositionIncomplete(err)
			if !scoped || incomplete.stage != "filter" || !strings.Contains(err.Error(), tc.failure) ||
				run.rangeProof.Verified || run.rangeProof.ClosedFrameID != 0 || run.pendingPanelClose != tc.panelLeftOpen ||
				actions != tc.insertAfterAction || samples != tc.frames {
				t.Fatalf("stable contradiction: actions=%d samples=%d proof=%+v pending close=%t err=%v",
					actions, samples, run.rangeProof, run.pendingPanelClose, err)
			}
		})
	}
}

type topBaseWheelChangeMatcher struct {
	*scrollScriptMatcher
}

func (m topBaseWheelChangeMatcher) Match(f platform.Frame) PageMatch {
	page := m.scrollScriptMatcher.Match(f)
	page.List.Top = f.ID > 1
	if f.ID == 5 {
		page.Wheel.Position = 2
	}
	return page
}

func TestTopBaseWheelChangeStopsBeforeConfirmationDrag(t *testing.T) {
	l := sixRowScrollLayout()
	m := topBaseWheelChangeMatcher{&scrollScriptMatcher{rows: l.Rows, columns: l.Columns, verdict: map[scrollPair]ScrollMatch{
		{1, 2}: displacedScan(l, 2), {2, 3}: {Stationary: true}, {2, 4}: {Stationary: true},
	}}}
	before := scrollFrame(1)
	view := &scrollScriptView{frames: []platform.Frame{scrollFrame(2), scrollFrame(3), scrollFrame(4), scrollFrame(5), scrollFrame(6)}, blockAfter: true}
	run := newScriptedScrollRun(l, view, m, "", 0, 30*time.Millisecond)
	run.inv.ActionBudget = 15 * time.Millisecond
	run.lastFrame, run.listMatch = before, m.Match(before)
	err := runScriptedSession(t, run, func(ctx context.Context, run *inventoryRun) error {
		_, err := run.scrollToTop(ctx)
		return err
	})
	if _, scoped := asPositionIncomplete(err); scoped || view.dispatches != 1 || view.calls != 4 ||
		faultDiagnosticCode(err) != CodeNavigationMismatch || !strings.Contains(err.Error(), "changed the wheel position to 2") {
		t.Fatalf("wrong wheel after a settled top fling: drags=%d reads=%d err=%v; want one drag and immediate session fault", view.dispatches, view.calls, err)
	}
}

type bottomStepMatcher struct {
	*ScrollEvidenceMatcher
}

func (m bottomStepMatcher) Match(f platform.Frame) PageMatch {
	page := m.ScrollEvidenceMatcher.Match(f)
	if f.ID == 1 && page.List != nil {
		copy(page.List.Occupied[m.L.Columns:], page.List.Occupied)
		page.List.Bottom = false
	}
	return page
}

func (m bottomStepMatcher) MatchScroll(before, after platform.Frame) ScrollMatch {
	if before.ID == 1 && after.ID == 2 {
		return displacedScan(m.L, 1)
	}
	return m.ScrollEvidenceMatcher.MatchScroll(before, after)
}

func TestShortBottomStepRejectsChangedOccupancyAtSettle(t *testing.T) {
	p := windowsNativeRangeProfile(t)
	m := bottomStepMatcher{p.Inventory(false).Matcher.(*ScrollEvidenceMatcher)}
	l := p.Layout
	candidate := loadWindowsWarehouseFrame(t, l)
	candidate.ID = 2
	draw.Draw(candidate.Image, l.ScrollTrack, image.NewUniform(color.Gray{Y: 220}), image.Point{}, draw.Src)
	changed := candidate
	changed.ID = 3
	changed.Image = image.NewRGBA(candidate.Image.Bounds())
	draw.Draw(changed.Image, changed.Image.Bounds(), candidate.Image, image.Point{}, draw.Src)
	newSlot := len(l.Slots) - 1
	draw.Draw(changed.Image, l.BarRect(newSlot), image.NewUniform(color.RGBA{R: 255, G: 180, B: 30, A: 255}), image.Point{}, draw.Src)
	candidateMatch, changedMatch := m.Match(candidate), m.Match(changed)
	if candidateMatch.Page != PageWarehouse || !candidateMatch.List.Bottom || candidateMatch.List.Occupied[newSlot] ||
		changedMatch.Page != PageWarehouse || !changedMatch.List.Bottom || !changedMatch.List.Occupied[newSlot] {
		t.Fatal("probe needs a valid terminal frame and a matching frame with one new occupied slot")
	}
	if ok, reason := m.zeroDisplacementProven(candidate, changed); !ok {
		t.Fatalf("production stillness matcher rejected the modified occupancy: %s", reason)
	}
	before, last := candidate, changed
	before.ID, last.ID = 1, 4
	before.Image = image.NewRGBA(candidate.Image.Bounds())
	draw.Draw(before.Image, before.Image.Bounds(), candidate.Image, image.Point{}, draw.Src)
	view := &scrollScriptView{frames: []platform.Frame{before, candidate, changed, last}, blockAfter: true}
	run := newScriptedScrollRun(l, view, m, "", 0, 60*time.Millisecond)
	run.position, run.filterActive, run.rangeProof.Verified = 3, true, true
	run.lastFrame, run.listMatch = before, m.Match(before)
	run.ledger = newSlotLedger(l.Columns, run.listMatch.List.Occupied, before.ID)
	for i, entry := range run.ledger.visible() {
		if entry.State == slotUnresolved {
			run.ledger.visible()[i].State = slotAdmitted
		}
	}
	originalLedger, _ := run.ledger.report()
	err := runScriptedSession(t, run, func(ctx context.Context, run *inventoryRun) error { return run.collectPosition(ctx) })
	run.inv.scope.Sort = scopeSortVerified
	for pos := 1; pos <= 6; pos++ {
		if pos == run.position {
			_ = run.finishPosition(err, positionTiming{}, 0, 0)
		} else {
			run.inv.recordPosition(positionScope{
				Position: pos, Top: true, Bottom: true, Complete: true, Range: positionRange{Verified: true},
			})
		}
	}
	ledger, _ := run.ledger.report()
	incomplete, scoped := asPositionIncomplete(err)
	if !scoped || incomplete.stage != "scroll" || faultDiagnosticCode(incomplete.err) != CodePositionLost ||
		run.inv.scope.Coverage != scopeCoveragePartial || run.lastFrame.ID != before.ID || view.dispatches != 1 ||
		!reflect.DeepEqual(ledger, originalLedger) {
		t.Fatalf("changed terminal occupancy: coverage=%s frame=%d drags=%d rowBase=%d err=%v",
			run.inv.scope.Coverage, run.lastFrame.ID, view.dispatches, run.ledger.rowBase, err)
	}
}
