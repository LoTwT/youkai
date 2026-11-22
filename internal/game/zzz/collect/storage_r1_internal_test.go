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

func storageReviewPage() PageMatch {
	l := StorageLayout()
	return PageMatch{
		Page: PageDriveStorage,
		List: &ListEvidence{
			Occupied:  make([]bool, len(l.Slots)),
			Positions: make([]PositionVerdict, len(l.Slots)),
			Selected:  -1,
		},
		Filter: &FilterEvidence{State: FilterActive},
	}
}

type storageReviewMatcher struct {
	page PageMatch
}

func (m storageReviewMatcher) Match(platform.Frame) PageMatch { return m.page }

func (m storageReviewMatcher) MatchScroll(platform.Frame, platform.Frame) ScrollMatch {
	return ScrollMatch{}
}

func (m storageReviewMatcher) CompareDetail(before, after platform.Frame) DetailComparison {
	if before.Image == after.Image {
		return DetailSame
	}
	return DetailDifferent
}

type storageReviewFrames struct {
	platform.Session
	frames []platform.Frame
}

func (s *storageReviewFrames) Frame(context.Context, platform.FrameRequest) (platform.Frame, error) {
	if len(s.frames) == 0 {
		return platform.Frame{}, context.DeadlineExceeded
	}
	f := s.frames[0]
	s.frames = s.frames[1:]
	return f, nil
}

func TestStorageTopConfirmedAtTop(t *testing.T) {
	page := storageReviewPage()
	page.List.Top = true
	if !topConfirmed(scrollOutcome{afterMatch: page, stationary: true}, true, true, 238, 238) {
		t.Fatal("a stationary drive-storage list at the proven scrollbar top was rejected")
	}
}

func TestStorageFilterClearAfterPreparationStops(t *testing.T) {
	page := storageReviewPage()
	page.Filter.State = FilterClear
	run := inventoryRun{
		inv:          &Inventory{Layout: StorageLayout(), Matcher: storageReviewMatcher{page: page}},
		filterActive: true,
		rangeProof:   positionRange{Verified: true},
	}
	if _, err := run.matchFrame(platform.Frame{ID: 2}); err == nil {
		t.Fatal("the scan accepted a drive-storage list after the proven S filter cleared")
	}
}

func TestStoragePanelSortMismatchAfterOpenStops(t *testing.T) {
	page := storageReviewPage()
	page.Page = PageFilterPanel
	page.List = nil
	page.Panel = &PanelEvidence{SortLabel: "", SortVerified: false}
	run := inventoryRun{
		inv:       &Inventory{Layout: StorageLayout(), Matcher: storageReviewMatcher{page: page}},
		sortLabel: sortLabelRarity,
	}
	if _, err := run.matchFrame(platform.Frame{ID: 3}); err == nil {
		t.Fatal("a later filter-panel frame with no readable sort label was accepted")
	}
}

func TestStorageReferenceRespectsScope(t *testing.T) {
	page := storageReviewPage()
	page.List.Occupied[0] = true
	page.List.Occupied[1] = true
	page.List.Occupied[2] = true
	page.List.Positions[0].Position = 3
	page.List.Positions[1].Position = 3
	page.List.Positions[2].Position = 4
	run := inventoryRun{inv: &Inventory{Layout: StorageLayout(), positions: []int{3}}}
	if ref, ok := run.referenceSlot(page, 1, 0); ok {
		t.Fatalf("selected reference cell %d with out-of-scope position %d", ref, page.List.Positions[ref].Position)
	}
}

func TestStorageTransitionAcceptsProvenEmptyOverlap(t *testing.T) {
	before, after := storageReviewPage(), storageReviewPage()
	before.List.Occupied[9], before.List.Positions[9].Position = true, 3
	after.List.Occupied[0], after.List.Positions[0].Position = true, 3
	run := inventoryRun{inv: &Inventory{Layout: StorageLayout()}}
	run.ledger = run.newStorageLedger(before, 1)
	matcher := NewScrollEvidenceMatcher(NewPixelMatcher(StorageLayout(), StorageCalibration()))
	step := matcher.displaced(1, 2, 1, ScrollBasisStoragePositions, nil, "proven one-row move")
	if err := run.applyTransition(platform.Frame{ID: 1}, platform.Frame{ID: 2}, before, after, step); err != nil {
		t.Fatal(err)
	}
}

func TestStorageDetailBaselineUsesCurrentFrame(t *testing.T) {
	run := inventoryRun{
		inv:             &Inventory{Layout: StorageLayout()},
		lastDetailFrame: platform.Frame{ID: 10},
	}
	pre := platform.Frame{ID: 20}
	if got := run.detailBaseline(pre); got.ID != pre.ID {
		t.Fatalf("storage baseline = %d, want the current pre-click frame %d", got.ID, pre.ID)
	}
	if got := run.detailBaseline(platform.Frame{}); got.ID != 10 {
		t.Fatalf("storage baseline fallback = %d, want the last admitted frame 10", got.ID)
	}
}

func TestStoragePreparedDetailUsesCurrentBaseline(t *testing.T) {
	page := storageReviewPage()
	page.List.Selected = 0
	page.List.Occupied[0], page.List.Positions[0].Position = true, 3
	page.Detail = &DetailEvidence{Position: PositionVerdict{Position: 3}, Quality: QualityS, Substats: 4}
	now := time.Now()
	current := image.NewRGBA(image.Rect(0, 0, 1, 1))
	frame := func(id uint64) platform.Frame {
		return platform.Frame{ID: id, Geometry: 1, Image: current, ArrivedAt: now.Add(time.Duration(id) * time.Millisecond)}
	}
	run := inventoryRun{
		inv:             &Inventory{Layout: StorageLayout(), Matcher: storageReviewMatcher{page: page}, DetailStableSpan: -1},
		view:            &storageReviewFrames{frames: []platform.Frame{frame(6), frame(7), frame(8)}},
		geo:             1,
		position:        3,
		lastDetailFrame: platform.Frame{ID: 1, Image: image.NewRGBA(image.Rect(0, 0, 1, 1))},
	}
	pre := frame(5)
	got, err := run.awaitDetail(context.Background(), run.detailBaseline(pre), 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.class == awaitStable && run.requireDetailPosition(got.match, 0, "target") == nil {
		t.Fatal("a detail already visible before the click was accepted as a transition despite no detail change")
	}
}

func TestStorageThumbTravelRequiresBothEdges(t *testing.T) {
	m := NewScrollEvidenceMatcher(NewPixelMatcher(StorageLayout(), StorageCalibration()))
	before := thumbReading{start: 300, end: 323, ok: true}
	oneEdge := thumbReading{start: 306, end: 323, ok: true}
	if _, ok, why := m.thumbTravelOfStrict(before, oneEdge, 1); ok {
		t.Fatalf("one moving thumb edge proved a row: %s", why)
	}
	both := thumbReading{start: 306, end: 329, ok: true}
	if _, ok, why := m.thumbTravelOfStrict(before, both, 1); !ok {
		t.Fatalf("both moving thumb edges did not prove one row: %s", why)
	}
}

func TestStorageProfileAlwaysUsesEvidenceMatcher(t *testing.T) {
	profile, err := WindowsStorageProfileFor(image.Pt(1920, 1080))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := profile.Inventory(true).Matcher.(*ScrollEvidenceMatcher); !ok {
		t.Fatalf("legacy storage inventory = %T, want ScrollEvidenceMatcher", profile.Inventory(true).Matcher)
	}
}

func TestStorageSavedLedgerKeepsEmptyAndOutOfScope(t *testing.T) {
	occupied := make([]bool, storageRows*storageColumns)
	positions := make([]int, len(occupied))
	occupied[0], positions[0] = true, 3
	occupied[2], positions[2] = true, 4
	ledger := newSlotLedgerWithPositions(storageColumns, occupied, positions, 1)
	ledger.visible()[2].State = slotOutOfScope
	run := inventoryRun{
		ledger: ledger,
		inv:    &Inventory{Layout: StorageLayout(), positions: []int{3}},
	}
	report, visits := run.storagePositionLedger(3)
	if report == nil || visits != 0 {
		t.Fatalf("ledger report = %+v visits %d", report, visits)
	}
	states := map[slotState]int{}
	for _, cell := range report.Slots {
		states[cell.State]++
	}
	if states[slotEmpty] == 0 || states[slotOutOfScope] == 0 || states[slotUnresolved] == 0 {
		t.Fatalf("saved ledger states = %+v, want empty, outOfScope and unresolved cells", states)
	}
}

func storageReviewPixelFrame(l Layout, id uint64, shade uint8, thumbStart, thumbEnd int) platform.Frame {
	img := image.NewRGBA(l.Content)
	paint := func(r image.Rectangle, value color.RGBA) {
		draw.Draw(img, r, image.NewUniform(value), image.Point{}, draw.Src)
	}
	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	paint(l.ListViewport, color.RGBA{R: shade, G: shade, B: shade, A: 255})
	paint(l.Funnel.At, l.Funnel.Colors["active"])
	for _, p := range l.PanelOpen {
		paint(p.At, p.Colors["closed"])
	}
	paintMask := func(at image.Point, rows []string) {
		for y, row := range rows {
			for x := range len(row) {
				if row[x] == '#' {
					img.SetRGBA(at.X+x, at.Y+y, white)
				}
			}
		}
	}
	paintMask(l.PageTitle.Min, l.PageTitleStencil.rows)
	for i := range l.Slots {
		paint(l.BarRect(i), white)
		paintMask(l.CardPositionRect(i).Min, storageCardPosition3A.rows)
	}
	paint(image.Rect(l.ScrollTrack.Min.X, thumbStart, l.ScrollTrack.Max.X, thumbEnd+1), white)
	return platform.Frame{ID: id, Geometry: 1, Image: img, Content: l.Content}
}

func TestStorageRepeatedBadgesDoNotProveRow(t *testing.T) {
	l := StorageLayout()
	m := NewScrollEvidenceMatcher(NewPixelMatcher(l, StorageCalibration()))
	before := storageReviewPixelFrame(l, 1, 10, 300, 323)
	after := storageReviewPixelFrame(l, 2, 10, 306, 329)
	if got := m.MatchScroll(before, after); got.Displaced {
		t.Fatalf("a static same-slot grid with both thumb edges moving was accepted: %s", got.Evidence)
	}
}
