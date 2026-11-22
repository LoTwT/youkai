package collect

import (
	"image"
	"image/color"
	"image/draw"
	"testing"

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
	if topConfirmed(scrollOutcome{afterMatch: matched, stationary: true, beforeAtTop: false, thumbStill: true}, true, true, 238, 238) {
		t.Fatal("top confirmed although the before frame was not at the top")
	}
	if topConfirmed(scrollOutcome{afterMatch: matched, stationary: true, beforeAtTop: true, thumbStill: false}, true, true, 244, 238) {
		t.Fatal("top confirmed although a thumb edge moved during the wait")
	}
	if !topConfirmed(scrollOutcome{afterMatch: matched, stationary: true, beforeAtTop: true, thumbStill: true}, true, true, 238, 238) {
		t.Fatal("a stationary thumb at an already proven top was rejected")
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
