package collect

import (
	"context"
	"image"
	"image/color"
	"image/draw"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/platform"
)

// storageDirectionFrame paints the list rows with the repeating card badge
// pattern so a card mapping can move while the scrollbar thumb moves in the
// opposite direction. It is a boundary test: the two independent witnesses
// must agree, and a single moving thumb edge must not prove boundary
// stillness.
func storageDirectionFrame(l Layout, id uint64, first, thumbStart, thumbEnd int) platform.Frame {
	f := storageReviewPixelFrame(l, id, 10, thumbStart, thumbEnd)
	blank := image.NewUniform(color.RGBA{R: 10, G: 10, B: 10, A: 255})
	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	for i := range l.Slots {
		r := l.CardPositionRect(i)
		draw.Draw(f.Image, r, blank, image.Point{}, draw.Src)
		position := (first + i/l.Columns) % len(l.CardPositions)
		for y, row := range l.CardPositions[position].masks[0].rows {
			for x := range len(row) {
				if row[x] == '#' {
					f.Image.SetRGBA(r.Min.X+x, r.Min.Y+y, white)
				}
			}
		}
	}
	return f
}

func storageSelectedPatternFrame(l Layout, id uint64, first, selected, thumbStart, thumbEnd int) platform.Frame {
	f := storageDirectionFrame(l, id, first, thumbStart, thumbEnd)
	colors := [6]uint8{33, 170, 64, 240, 92, 205}
	for row := range l.Rows {
		y := l.Slots[row*l.Columns].Min.Y + 56
		for block := 0; block < 12; block++ {
			x := 100 + block*100
			v := colors[(block*block+block*(row+first)+(row+first)*(row+first))%len(colors)]
			pixel := image.NewUniform(color.RGBA{R: v, G: v, B: v, A: 255})
			draw.Draw(f.Image, image.Rect(x, y, x+95, y+46), pixel, image.Point{}, draw.Src)
		}
	}
	if selected >= 0 {
		accent := image.NewUniform(color.RGBA{R: 224, G: 208, B: 67, A: 255})
		draw.Draw(f.Image, l.BorderRect(selected), accent, image.Point{}, draw.Src)
	}
	return f
}

func storageRepeatPositionColumns(l Layout, f platform.Frame) {
	blank := image.NewUniform(color.RGBA{R: 10, G: 10, B: 10, A: 255})
	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	for i := range l.Slots {
		r := l.CardPositionRect(i)
		draw.Draw(f.Image, r, blank, image.Point{}, draw.Src)
		position := i % l.Columns % len(l.CardPositions)
		for y, row := range l.CardPositions[position].masks[0].rows {
			for x := range len(row) {
				if row[x] == '#' {
					f.Image.SetRGBA(r.Min.X+x, r.Min.Y+y, white)
				}
			}
		}
	}
}

func TestStorageSelectedRepeatedColumnsStillAdvances(t *testing.T) {
	l := StorageLayout()
	m := NewScrollEvidenceMatcher(NewPixelMatcher(l, StorageCalibration()))
	before := storageSelectedPatternFrame(l, 1, 0, 27, 300, 323)
	after := storageSelectedPatternFrame(l, 2, 1, 18, 306, 329)
	if got := m.MatchScroll(before, after); !got.Displaced || got.Rows != 1 {
		t.Fatalf("invalid changing-badge positive control: %s", got.Evidence)
	}
	for i, f := range []platform.Frame{before, after} {
		storageRepeatPositionColumns(l, f)
		p := m.Match(f)
		if p.Page != PageDriveStorage || p.List.Selected != []int{27, 18}[i] {
			t.Fatalf("invalid repeated-column fixture: %s", p.Evidence)
		}
		for cell, position := range p.List.Positions {
			if !position.known() || position.Position != cell%l.Columns%6+1 {
				t.Fatalf("invalid repeated badge at %d: %+v", cell, position)
			}
		}
	}
	if got := m.MatchScroll(before, after); !got.Displaced || got.Rows != 1 {
		t.Fatalf("selection and thumb agree, but repeated badges blocked the row: %s", got.Evidence)
	}
}

func TestStorageSelectedCrossColumnConflictStops(t *testing.T) {
	l := StorageLayout()
	m := NewScrollEvidenceMatcher(NewPixelMatcher(l, StorageCalibration()))
	before := storageSelectedPatternFrame(l, 1, 0, 27, 300, 323)
	after := storageSelectedPatternFrame(l, 2, 1, 19, 306, 329)
	for i, f := range []platform.Frame{before, after} {
		p := m.Match(f)
		if p.Page != PageDriveStorage || p.List.Selected != []int{27, 19}[i] {
			t.Fatalf("invalid cross-column fixture: %s", p.Evidence)
		}
	}
	if got := m.MatchScroll(before, after); got.Displaced {
		t.Fatalf("accepted a column scroll while both anchors were readable and changed column: %s", got.Evidence)
	}
}

func TestStorageSelectedScrollDirectionMustMatchThumb(t *testing.T) {
	l := StorageLayout()
	m := NewScrollEvidenceMatcher(NewPixelMatcher(l, StorageCalibration()))
	before := storageSelectedPatternFrame(l, 1, 0, 27, 300, 323)
	agrees := storageSelectedPatternFrame(l, 2, 1, 18, 306, 329)
	opposes := storageSelectedPatternFrame(l, 3, 1, 18, 294, 317)
	if got := m.MatchScroll(before, agrees); !got.Displaced || got.Rows != 1 {
		t.Fatalf("positive selected control failed: %s", got.Evidence)
	}
	if got := m.MatchScroll(before, opposes); got.Displaced {
		t.Fatalf("selected path accepted a row advance against backward thumb travel: %s", got.Evidence)
	}
}

func TestStorageScrollDirectionMustMatchThumb(t *testing.T) {
	l := StorageLayout()
	m := NewScrollEvidenceMatcher(NewPixelMatcher(l, StorageCalibration()))
	before := storageDirectionFrame(l, 1, 0, 300, 323)
	agrees := storageDirectionFrame(l, 2, 1, 306, 329)
	opposes := storageDirectionFrame(l, 3, 1, 294, 317)
	if got := m.MatchScroll(before, agrees); !got.Displaced || got.Rows != 1 {
		t.Fatalf("positive control failed: %+v", got)
	}
	if got := m.MatchScroll(before, opposes); got.Displaced {
		t.Fatalf("a forward card mapping with backward scrollbar travel was accepted: %s", got.Evidence)
	}
}

func TestStorageTopRequiresStationaryThumbFromTopBase(t *testing.T) {
	l := StorageLayout()
	m := NewScrollEvidenceMatcher(NewPixelMatcher(l, StorageCalibration()))
	before := storageDirectionFrame(l, 1, 0, 244, 261)
	after := storageDirectionFrame(l, 2, 0, 238, 261)
	matched := m.Match(after)
	scroll := m.MatchScroll(before, after)
	if matched.Page != PageDriveStorage || !matched.List.Top || !scroll.Stationary {
		t.Fatalf("fixture did not reach the weak stationary path: %s; %s", matched.Evidence, scroll.Evidence)
	}
	if topConfirmed(scrollOutcome{afterMatch: matched, stationary: true, beforeAtTop: false, thumbStill: true, listStill: true}, true, true, 238, 238) {
		t.Fatal("top confirmed although the before frame was not at the top")
	}
	if topConfirmed(scrollOutcome{afterMatch: matched, stationary: true, beforeAtTop: true, thumbStill: false, listStill: true}, true, true, 244, 238) {
		t.Fatal("top confirmed although a thumb edge moved during the wait")
	}
	if !topConfirmed(scrollOutcome{afterMatch: matched, stationary: true, beforeAtTop: true, thumbStill: true, listStill: true}, true, true, 238, 238) {
		t.Fatal("a stationary thumb and list at an already proven top was rejected")
	}
}

func TestStorageTopNeedsListStillness(t *testing.T) {
	l := StorageLayout()
	m := NewScrollEvidenceMatcher(NewPixelMatcher(l, StorageCalibration()))
	before := storageSelectedPatternFrame(l, 1, 0, -1, 238, 261)
	after := storageSelectedPatternFrame(l, 2, 1, -1, 238, 261)
	page := m.Match(after)
	result := m.MatchScroll(before, after)
	if page.Page != PageDriveStorage || !page.List.Top || !result.Undecided {
		t.Fatalf("invalid fixture: %s; %s", page.Evidence, result.Evidence)
	}
	if ok, _ := m.zeroDisplacementProven(before, after); ok {
		t.Fatal("invalid fixture: the list must lack zero-displacement proof")
	}
	out := scrollOutcome{afterMatch: page, beforeAtTop: true, thumbStill: true, match: result}
	if topConfirmed(out, true, true, 238, 238) {
		t.Fatalf("stationary thumb alone confirmed the top over an unproven list: %s", result.Evidence)
	}
}

func TestStorageTopRequiresIndependentListStillness(t *testing.T) {
	page := PageMatch{Page: PageDriveStorage, List: &ListEvidence{Top: true}}
	if topConfirmed(scrollOutcome{afterMatch: page, stationary: true, beforeAtTop: true, thumbStill: true, listStill: false}, true, true, 238, 238) {
		t.Fatal("top confirmed from a stationary thumb without independent list stillness")
	}
	if !topConfirmed(scrollOutcome{afterMatch: page, stationary: true, beforeAtTop: true, thumbStill: true, listStill: true}, true, true, 238, 238) {
		t.Fatal("top with stationary list and thumb was rejected")
	}
}

func TestStorageBottomStillnessRequiresStationaryThumb(t *testing.T) {
	l := StorageLayout()
	m := NewScrollEvidenceMatcher(NewPixelMatcher(l, StorageCalibration()))
	before := storageDirectionFrame(l, 1, 0, 842, 865)
	after := storageDirectionFrame(l, 2, 0, 848, 865)
	for _, f := range []platform.Frame{before, after} {
		accent := image.NewUniform(color.RGBA{R: 224, G: 208, B: 67, A: 255})
		draw.Draw(f.Image, l.BorderRect(27), accent, image.Point{}, draw.Src)
		page := m.Match(f)
		if page.Page != PageDriveStorage || !page.List.Bottom || page.List.Selected != 27 {
			t.Fatalf("fixture did not produce a selected bottom frame: %s", page.Evidence)
		}
	}
	scroll := m.MatchScroll(before, after)
	if proven, why := m.zeroDisplacementProven(before, after); scroll.Stationary && proven {
		t.Fatalf("bottom accepts stillness despite a 6 px lead-edge move: %s", why)
	}
}

func TestStorageReopenedPanelWithClearRangeStops(t *testing.T) {
	page := PageMatch{Page: PageFilterPanel, Panel: &PanelEvidence{
		SortLabel: sortLabelRarity, SortVerified: true,
		Range: &PanelRangeEvidence{RarityS: FilterClear},
	}}
	run := inventoryRun{
		inv:          &Inventory{Layout: StorageLayout(), Matcher: storageReviewMatcher{page: page}},
		sortLabel:    sortLabelRarity,
		filterActive: true,
		rangeProof:   positionRange{Verified: true},
	}
	if _, err := run.matchFrame(platform.Frame{ID: 50}); err == nil {
		t.Fatal("a panel reopened after preparation with S visibly clear did not invalidate the scan")
	}
}

func storageSettleRun(l Layout, m *ScrollEvidenceMatcher, before platform.Frame, frames ...platform.Frame) *inventoryRun {
	now := time.Now()
	before.ArrivedAt = now
	for i := range frames {
		frames[i].ArrivedAt = now.Add(time.Duration(i+1) * time.Millisecond)
	}
	run := &inventoryRun{
		geo: 1,
		inv: &Inventory{
			Layout: l, Matcher: m, ScrollBudget: 2 * time.Second,
			ScrollTiming: &ScrollTiming{MotionStartBudget: time.Second, SettleBudget: time.Second, SettleObservations: 2},
		},
		view: &storageReviewFrames{frames: frames},
	}
	run.ledger = run.newStorageLedger(m.Match(before), before.ID)
	return run
}

func TestStorageCandidateReplacementKeepsReliableAnchor(t *testing.T) {
	l := StorageLayout()
	m := NewScrollEvidenceMatcher(NewPixelMatcher(l, StorageCalibration()))
	before := storageSelectedPatternFrame(l, 1, 0, -1, 300, 323)
	first := storageSelectedPatternFrame(l, 2, 1, 27, 306, 329)
	replacement := storageSelectedPatternFrame(l, 3, 2, -1, 312, 335)
	conflict1 := storageSelectedPatternFrame(l, 4, 2, 19, 312, 335)
	conflict2 := storageSelectedPatternFrame(l, 5, 2, 19, 312, 335)
	run := storageSettleRun(l, m, before, first, replacement, conflict1, conflict2)
	out, err := run.awaitScroll(context.Background(), before)
	if err != nil {
		t.Fatal(err)
	}
	if out.settled || run.viewport != 0 {
		t.Fatalf("a replacement frame erased the reliable candidate anchor: settled=%t viewport=%d", out.settled, run.viewport)
	}
	if !out.match.SelectionConflict {
		t.Fatalf("candidate-anchor conflict was not retained: %s", out.match.Evidence)
	}
}

func TestStorageCandidateReplacementSameColumnRecovers(t *testing.T) {
	l := StorageLayout()
	m := NewScrollEvidenceMatcher(NewPixelMatcher(l, StorageCalibration()))
	before := storageSelectedPatternFrame(l, 1, 0, -1, 300, 323)
	first := storageSelectedPatternFrame(l, 2, 1, 27, 306, 329)
	replacement := storageSelectedPatternFrame(l, 3, 2, -1, 312, 335)
	confirm1 := storageSelectedPatternFrame(l, 4, 2, 18, 312, 335)
	confirm2 := storageSelectedPatternFrame(l, 5, 2, 18, 312, 335)
	run := storageSettleRun(l, m, before, first, replacement, confirm1, confirm2)
	out, err := run.awaitScroll(context.Background(), before)
	if err != nil {
		t.Fatal(err)
	}
	if !out.displaced || !out.settled {
		t.Fatalf("same-column replacement did not settle: displaced=%t settled=%t", out.displaced, out.settled)
	}
	if err := run.applyTransition(before, out.after, m.Match(before), out.afterMatch, out.match); err != nil {
		t.Fatalf("same-column replacement did not commit: %v", err)
	}
	if run.viewport != 1 {
		t.Fatalf("same-column replacement committed viewport %d, want 1", run.viewport)
	}
}

func storageWaitAndCommit(t *testing.T, l Layout, m *ScrollEvidenceMatcher, before platform.Frame, frames ...platform.Frame) (scrollOutcome, int) {
	t.Helper()
	run := storageSettleRun(l, m, before, frames...)
	out, err := run.awaitScroll(context.Background(), before)
	if err != nil {
		t.Fatal(err)
	}
	if out.displaced {
		if err := run.latestAdvanceFault("the scroll", out); err != nil {
			t.Fatalf("unexpected last-page fault: %v", err)
		}
		if err := run.applyTransition(before, out.after, m.Match(before), out.afterMatch, out.match); err != nil {
			t.Fatalf("unexpected ledger failure: %v", err)
		}
	}
	return out, run.viewport
}

func paintStorageBadge(l Layout, f platform.Frame, cell, position int) {
	r := l.CardPositionRect(cell)
	draw.Draw(f.Image, r, image.NewUniform(color.RGBA{R: 10, G: 10, B: 10, A: 255}), image.Point{}, draw.Src)
	for y, row := range l.CardPositions[position-1].masks[0].rows {
		for x := range len(row) {
			if row[x] == '#' {
				f.Image.SetRGBA(r.Min.X+x, r.Min.Y+y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
			}
		}
	}
}

func TestStorageZeroDisplacementAnchorRetainsRow(t *testing.T) {
	l := StorageLayout()
	m := NewScrollEvidenceMatcher(NewPixelMatcher(l, StorageCalibration()))
	before := storageSelectedPatternFrame(l, 1, 0, -1, 300, 323)
	anchored := storageSelectedPatternFrame(l, 2, 0, 27, 300, 323)
	candidate := storageSelectedPatternFrame(l, 3, 1, -1, 306, 329)
	if !m.MatchScroll(before, anchored).Stationary {
		t.Fatal("invalid stationary anchor fixture")
	}
	if ok, _ := m.zeroDisplacementProven(before, anchored); !ok {
		t.Fatal("fixture did not prove zero displacement")
	}
	if got := m.MatchScroll(before, candidate); !got.Displaced || got.Rows != 1 {
		t.Fatalf("invalid one-row candidate: %s", got.Evidence)
	}
	t.Run("aligned", func(t *testing.T) {
		confirm1 := storageSelectedPatternFrame(l, 4, 1, 18, 306, 329)
		confirm2 := storageSelectedPatternFrame(l, 5, 1, 18, 306, 329)
		out, viewport := storageWaitAndCommit(t, l, m, before, anchored, candidate, confirm1, confirm2)
		if !out.settled || viewport != 1 {
			t.Fatalf("aligned zero-anchor control did not commit: settled=%t viewport=%d", out.settled, viewport)
		}
	})
	t.Run("row conflict", func(t *testing.T) {
		confirm1 := storageSelectedPatternFrame(l, 4, 1, 9, 306, 329)
		confirm2 := storageSelectedPatternFrame(l, 5, 1, 9, 306, 329)
		out, viewport := storageWaitAndCommit(t, l, m, before, anchored, candidate, confirm1, confirm2)
		if out.settled || viewport != 0 {
			t.Fatalf("zero-anchor row conflict committed frame %d to viewport %d", out.after.ID, viewport)
		}
	})
}

func TestStorageReplacementRetainsNewRowBadge(t *testing.T) {
	l := StorageLayout()
	m := NewScrollEvidenceMatcher(NewPixelMatcher(l, StorageCalibration()))
	before := storageSelectedPatternFrame(l, 1, 0, -1, 300, 323)
	candidate := storageSelectedPatternFrame(l, 2, 1, -1, 306, 329)
	if got := m.MatchScroll(before, candidate); !got.Displaced || got.Rows != 1 {
		t.Fatalf("invalid one-row candidate: %s", got.Evidence)
	}
	original := m.Match(candidate).List.Positions[27]
	if !original.known() || original.Position != 5 {
		t.Fatalf("fixture did not establish absolute row 4 col 0 badge 5: %+v", original)
	}
	for _, badge := range []int{5, 2} {
		name := "same badge"
		if badge != 5 {
			name = "known badge conflict"
		}
		t.Run(name, func(t *testing.T) {
			replacement := storageSelectedPatternFrame(l, 3, 2, -1, 312, 335)
			confirm1 := storageSelectedPatternFrame(l, 4, 2, -1, 312, 335)
			confirm2 := storageSelectedPatternFrame(l, 5, 2, -1, 312, 335)
			for _, f := range []platform.Frame{replacement, confirm1, confirm2} {
				paintStorageBadge(l, f, 18, badge)
			}
			out, viewport := storageWaitAndCommit(t, l, m, before, candidate, replacement, confirm1, confirm2)
			if badge == 5 {
				if !out.settled || viewport != 1 {
					t.Fatalf("same-badge replacement did not commit: settled=%t viewport=%d", out.settled, viewport)
				}
				return
			}
			if out.settled || viewport != 0 {
				t.Fatalf("replacement overwrote absolute row 4 badge 5 with 2 and committed frame %d to viewport %d", out.after.ID, viewport)
			}
		})
	}
}

func clearStorageBadge(l Layout, f platform.Frame, cell int) {
	r := l.CardPositionRect(cell)
	draw.Draw(f.Image, r, image.NewUniform(color.RGBA{R: 10, G: 10, B: 10, A: 255}), image.Point{}, draw.Src)
}

func TestStorageKnownBadgeSurvivesMissingAndReread(t *testing.T) {
	l := StorageLayout()
	m := NewScrollEvidenceMatcher(NewPixelMatcher(l, StorageCalibration()))
	before := storageSelectedPatternFrame(l, 1, 0, -1, 300, 323)
	candidate := storageSelectedPatternFrame(l, 2, 1, -1, 306, 329)
	if got := m.MatchScroll(before, candidate); !got.Displaced || got.Rows != 1 {
		t.Fatalf("invalid one-row candidate: %s", got.Evidence)
	}
	for _, badge := range []int{5, 2} {
		name := "same readable badge"
		if badge != 5 {
			name = "conflicting readable badge"
		}
		t.Run(name, func(t *testing.T) {
			replacement := storageSelectedPatternFrame(l, 3, 2, -1, 312, 335)
			clearStorageBadge(l, replacement, 18)
			confirm1 := storageSelectedPatternFrame(l, 4, 2, -1, 312, 335)
			confirm2 := storageSelectedPatternFrame(l, 5, 2, -1, 312, 335)
			paintStorageBadge(l, confirm1, 18, badge)
			paintStorageBadge(l, confirm2, 18, badge)
			out, viewport := storageWaitAndCommit(t, l, m, before, candidate, replacement, confirm1, confirm2)
			if badge == 5 {
				if !out.settled || viewport != 1 {
					t.Fatalf("same badge after an unreadable frame did not commit: settled=%t viewport=%d", out.settled, viewport)
				}
				return
			}
			if out.settled || viewport != 0 {
				t.Fatalf("conflicting badge after an unreadable frame committed frame %d to viewport %d", out.after.ID, viewport)
			}
		})
	}
}

func TestStorageReplacementRetainsAnchorRow(t *testing.T) {
	l := StorageLayout()
	m := NewScrollEvidenceMatcher(NewPixelMatcher(l, StorageCalibration()))
	before := storageSelectedPatternFrame(l, 1, 0, -1, 300, 323)
	anchored := storageSelectedPatternFrame(l, 2, 1, 27, 306, 329)
	replacement := storageSelectedPatternFrame(l, 3, 2, -1, 312, 335)
	confirm1 := storageSelectedPatternFrame(l, 4, 2, 9, 312, 335)
	confirm2 := storageSelectedPatternFrame(l, 5, 2, 9, 312, 335)
	run := storageSettleRun(l, m, before, anchored, replacement, confirm1, confirm2)
	out, err := run.awaitScroll(context.Background(), before)
	if err != nil {
		t.Fatal(err)
	}
	if out.settled || run.viewport != 0 {
		t.Fatalf("a replacement frame lost the reliable anchor row: settled=%t viewport=%d", out.settled, run.viewport)
	}
	if !out.match.SelectionConflict {
		t.Fatalf("anchor-row conflict was not retained: %s", out.match.Evidence)
	}
}

func TestStorageConfirmationRetainsKnownBadges(t *testing.T) {
	l := StorageLayout()
	m := NewScrollEvidenceMatcher(NewPixelMatcher(l, StorageCalibration()))
	before := storageSelectedPatternFrame(l, 1, 0, 27, 300, 323)
	candidate := storageSelectedPatternFrame(l, 2, 1, 18, 306, 329)
	confirm1 := storageSelectedPatternFrame(l, 3, 1, 18, 306, 329)
	confirm2 := storageSelectedPatternFrame(l, 4, 1, 18, 306, 329)
	for _, f := range []*platform.Frame{&confirm1, &confirm2} {
		r := l.CardPositionRect(0)
		draw.Draw(f.Image, r, image.NewUniform(color.RGBA{R: 10, G: 10, B: 10, A: 255}), image.Point{}, draw.Src)
		for y, row := range l.CardPositions[4].masks[0].rows {
			for x := range len(row) {
				if row[x] == '#' {
					f.Image.SetRGBA(r.Min.X+x, r.Min.Y+y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
				}
			}
		}
	}
	run := storageSettleRun(l, m, before, candidate, confirm1, confirm2)
	out, err := run.awaitScroll(context.Background(), before)
	if err != nil {
		t.Fatal(err)
	}
	if out.settled || run.viewport != 0 {
		t.Fatalf("a known badge change was accepted as stillness: settled=%t viewport=%d", out.settled, run.viewport)
	}
	if !out.match.SelectionConflict {
		t.Fatalf("known badge conflict was not retained: %s", out.match.Evidence)
	}
}

func TestStorageConflictBeforeCandidateInvalidatesAction(t *testing.T) {
	l := StorageLayout()
	m := NewScrollEvidenceMatcher(NewPixelMatcher(l, StorageCalibration()))
	before := storageSelectedPatternFrame(l, 1, 0, 27, 300, 323)
	conflict := storageSelectedPatternFrame(l, 2, 1, 9, 306, 329)
	candidate := storageSelectedPatternFrame(l, 3, 1, 18, 306, 329)
	confirm1 := storageSelectedPatternFrame(l, 4, 1, 18, 306, 329)
	confirm2 := storageSelectedPatternFrame(l, 5, 1, 18, 306, 329)
	run := storageSettleRun(l, m, before, conflict, candidate, confirm1, confirm2)
	out, err := run.awaitScroll(context.Background(), before)
	if err != nil {
		t.Fatal(err)
	}
	if out.settled || run.viewport != 0 {
		t.Fatalf("a conflict before the first candidate was forgotten: settled=%t viewport=%d", out.settled, run.viewport)
	}
}

func TestStorageMissingCandidateSameColumnRowConflict(t *testing.T) {
	l := StorageLayout()
	m := NewScrollEvidenceMatcher(NewPixelMatcher(l, StorageCalibration()))
	before := storageSelectedPatternFrame(l, 1, 0, 27, 300, 323)
	candidate := storageSelectedPatternFrame(l, 2, 1, -1, 306, 329)
	conflict1 := storageSelectedPatternFrame(l, 3, 1, 9, 306, 329)
	conflict2 := storageSelectedPatternFrame(l, 4, 1, 9, 306, 329)
	run := storageSettleRun(l, m, before, candidate, conflict1, conflict2)
	out, err := run.awaitScroll(context.Background(), before)
	if err != nil {
		t.Fatal(err)
	}
	if out.settled || run.viewport != 0 {
		t.Fatalf("a same-column row conflict was hidden by the missing candidate anchor: settled=%t viewport=%d", out.settled, run.viewport)
	}
}

func TestStorageCrossColumnConfirmationCannotSettleLedger(t *testing.T) {
	l := StorageLayout()
	m := NewScrollEvidenceMatcher(NewPixelMatcher(l, StorageCalibration()))
	before := storageSelectedPatternFrame(l, 1, 0, 27, 300, 323)
	candidate := storageSelectedPatternFrame(l, 2, 1, 18, 306, 329)
	conflict1 := storageSelectedPatternFrame(l, 3, 1, 19, 306, 329)
	conflict2 := storageSelectedPatternFrame(l, 4, 1, 19, 306, 329)
	run := storageSettleRun(l, m, before, candidate, conflict1, conflict2)
	out, err := run.awaitScroll(context.Background(), before)
	if err != nil {
		t.Fatal(err)
	}
	if out.settled {
		if err := run.applyTransition(before, out.after, m.Match(before), out.afterMatch, out.match); err != nil {
			t.Fatalf("fixture unexpectedly stopped the commit: %v", err)
		}
		t.Fatalf("cross-column confirmation settled ledger viewport %d from candidate %d; latest anchor %d",
			run.viewport, out.after.ID, out.lastMatch.List.Selected)
	}
	if run.viewport != 0 {
		t.Fatalf("ledger viewport advanced to %d despite a cross-column confirmation", run.viewport)
	}
}

func TestStorageMissingCandidateCrossColumnNoCommit(t *testing.T) {
	l := StorageLayout()
	m := NewScrollEvidenceMatcher(NewPixelMatcher(l, StorageCalibration()))
	before := storageSelectedPatternFrame(l, 1, 0, 27, 300, 323)
	candidate := storageSelectedPatternFrame(l, 2, 1, -1, 306, 329)
	conflict1 := storageSelectedPatternFrame(l, 3, 1, 19, 306, 329)
	conflict2 := storageSelectedPatternFrame(l, 4, 1, 19, 306, 329)
	run := storageSettleRun(l, m, before, candidate, conflict1, conflict2)
	out, err := run.awaitScroll(context.Background(), before)
	if err != nil {
		t.Fatal(err)
	}
	if out.settled || run.viewport != 0 {
		t.Fatalf("missing candidate followed by cross-column anchors committed viewport %d from candidate %d; latest %d",
			run.viewport, out.after.ID, out.lastMatch.List.Selected)
	}
	if !out.match.SelectionConflict {
		t.Fatalf("missing-anchor cross-column conflict was not retained: %s", out.match.Evidence)
	}
}

func TestStorageTotalConflictInvalidatesCandidate(t *testing.T) {
	l := StorageLayout()
	m := NewScrollEvidenceMatcher(NewPixelMatcher(l, StorageCalibration()))
	before := storageSelectedPatternFrame(l, 1, 0, 27, 300, 323)
	candidate := storageSelectedPatternFrame(l, 2, 1, -1, 306, 329)
	conflict := storageSelectedPatternFrame(l, 3, 2, 10, 312, 335)
	restored1 := storageSelectedPatternFrame(l, 4, 1, -1, 306, 329)
	restored2 := storageSelectedPatternFrame(l, 5, 1, -1, 306, 329)
	run := storageSettleRun(l, m, before, candidate, conflict, restored1, restored2)
	out, err := run.awaitScroll(context.Background(), before)
	if err != nil {
		t.Fatal(err)
	}
	if out.settled || run.viewport != 0 {
		t.Fatalf("a total-rejudgment conflict left the old candidate committable: settled=%t viewport=%d", out.settled, run.viewport)
	}
	if !out.match.SelectionConflict {
		t.Fatalf("total-selection conflict was not retained: %s", out.match.Evidence)
	}
}

func TestStorageReusedConflictDoesNotSurviveDeadline(t *testing.T) {
	l := StorageLayout()
	m := NewScrollEvidenceMatcher(NewPixelMatcher(l, StorageCalibration()))
	before := storageSelectedPatternFrame(l, 1, 0, 27, 300, 323)
	candidate := storageSelectedPatternFrame(l, 2, 1, 18, 306, 329)
	conflict := storageSelectedPatternFrame(l, 3, 1, 19, 306, 329)
	conflict.Reused = true
	run := storageSettleRun(l, m, before, candidate, conflict)
	out, err := run.awaitScroll(context.Background(), before)
	if err != nil {
		t.Fatal(err)
	}
	if out.displaced || out.settled || out.hasCandidate || run.viewport != 0 || !out.match.SelectionConflict {
		t.Fatalf("reused conflict survived the deadline: displaced=%t settled=%t candidate=%t viewport=%d conflict=%t",
			out.displaced, out.settled, out.hasCandidate, run.viewport, out.match.SelectionConflict)
	}
}

func TestStorageMissingCandidateAnchorCanRecover(t *testing.T) {
	l := StorageLayout()
	m := NewScrollEvidenceMatcher(NewPixelMatcher(l, StorageCalibration()))
	before := storageSelectedPatternFrame(l, 1, 0, 27, 300, 323)
	candidate := storageSelectedPatternFrame(l, 2, 1, -1, 306, 329)
	confirm1 := storageSelectedPatternFrame(l, 3, 1, 18, 306, 329)
	confirm2 := storageSelectedPatternFrame(l, 4, 1, 18, 306, 329)
	run := storageSettleRun(l, m, before, candidate, confirm1, confirm2)
	out, err := run.awaitScroll(context.Background(), before)
	if err != nil {
		t.Fatal(err)
	}
	if !out.displaced || !out.settled {
		t.Fatalf("a legal same-column recovery after a missing candidate anchor did not settle: displaced=%t settled=%t", out.displaced, out.settled)
	}
	if err := run.applyTransition(before, out.after, m.Match(before), out.afterMatch, out.match); err != nil {
		t.Fatalf("legal missing-anchor recovery did not commit: %v", err)
	}
	if run.viewport != 1 {
		t.Fatalf("legal missing-anchor recovery committed viewport %d, want 1", run.viewport)
	}
}

func TestStorageAlignedConfirmationSettlesLedger(t *testing.T) {
	l := StorageLayout()
	m := NewScrollEvidenceMatcher(NewPixelMatcher(l, StorageCalibration()))
	before := storageSelectedPatternFrame(l, 1, 0, 27, 300, 323)
	candidate := storageSelectedPatternFrame(l, 2, 1, 18, 306, 329)
	confirm1 := storageSelectedPatternFrame(l, 3, 1, 18, 306, 329)
	confirm2 := storageSelectedPatternFrame(l, 4, 1, 18, 306, 329)
	run := storageSettleRun(l, m, before, candidate, confirm1, confirm2)
	out, err := run.awaitScroll(context.Background(), before)
	if err != nil {
		t.Fatal(err)
	}
	if !out.displaced || !out.settled {
		t.Fatalf("aligned confirmation did not settle: displaced=%t settled=%t", out.displaced, out.settled)
	}
	if err := run.applyTransition(before, out.after, m.Match(before), out.afterMatch, out.match); err != nil {
		t.Fatalf("aligned confirmation did not commit: %v", err)
	}
	if run.viewport != 1 {
		t.Fatalf("aligned confirmation committed viewport %d, want 1", run.viewport)
	}
}
