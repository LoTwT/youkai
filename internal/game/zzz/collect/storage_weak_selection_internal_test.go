package collect

import (
	"context"
	"image"
	"image/color"
	"image/draw"
	"strings"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/platform"
)

type weakSelectionView struct {
	frames []platform.Frame
	next   int
}

func (v *weakSelectionView) Observe() (platform.Observation, error) {
	return platform.Observation{Ready: true, Geometry: 1, At: time.Now()}, nil
}

func (v *weakSelectionView) Frame(context.Context, platform.FrameRequest) (platform.Frame, error) {
	if v.next >= len(v.frames) {
		return platform.Frame{}, context.DeadlineExceeded
	}
	f := v.frames[v.next]
	v.next++
	return f, nil
}

func (v *weakSelectionView) Dispatch(platform.InputOp) platform.InputResult {
	return platform.InputResult{Status: platform.InputPosted}
}

type weakSelectionMatcher struct {
	pages  map[uint64]PageMatch
	detail map[uint64]PageMatch
	weak   map[uint64]*weakSelectionEvidence
	thumbs map[uint64][2]int
}

func (m weakSelectionMatcher) Match(f platform.Frame) PageMatch {
	if p, ok := m.pages[f.ID]; ok {
		return p
	}
	return PageMatch{Page: PageUnknown, Evidence: "unscripted frame"}
}
func (m weakSelectionMatcher) weakSelectionEvidence(f platform.Frame) (PageMatch, *weakSelectionEvidence) {
	p, ok := m.detail[f.ID]
	if !ok {
		return PageMatch{}, nil
	}
	return p, m.weak[f.ID]
}
func (m weakSelectionMatcher) MatchScroll(platform.Frame, platform.Frame) ScrollMatch {
	return ScrollMatch{Stationary: true, Evidence: "scripted same viewport"}
}
func (m weakSelectionMatcher) ThumbEdges(f platform.Frame) (int, int, bool) {
	if t, ok := m.thumbs[f.ID]; ok {
		return t[0], t[1], true
	}
	return 300, 323, true
}
func (weakSelectionMatcher) CompareDetail(a, b platform.Frame) DetailComparison {
	if a.ID == 1 {
		return DetailDifferent
	}
	return DetailSame
}

func weakSelectionFixture(target int) (Layout, []bool, []int, PageMatch) {
	l := StorageLayout()
	occupied := make([]bool, len(l.Slots))
	positions := make([]int, len(l.Slots))
	for i := range occupied {
		occupied[i] = true
		positions[i] = i%6 + 1
	}
	p := PageMatch{
		Page: PageDriveStorage,
		List: &ListEvidence{
			Occupied: occupied, Selected: target, Positions: make([]PositionVerdict, len(l.Slots)),
		},
		Detail: &DetailEvidence{Quality: QualityS, Substats: 4, Position: PositionVerdict{Position: positions[target]}},
		Filter: &FilterEvidence{State: FilterActive}, Evidence: "known target frame",
	}
	for i := range positions {
		p.List.Positions[i] = PositionVerdict{Position: positions[i]}
	}
	return l, occupied, positions, p
}

func weakSelectionPage(base PageMatch, weakSlot, hits int) PageMatch {
	p := base
	list := *base.List
	list.Selected = -1
	list.weakSelection = &weakSelectionEvidence{Slot: weakSlot, Hits: hits}
	p.List = &list
	p.Evidence = "between-tier target border"
	return p
}

func weakSelectionRun(t *testing.T, l Layout, occupied []bool, positions []int, pages map[uint64]PageMatch, frames []platform.Frame, target int) *inventoryRun {
	t.Helper()
	matcher := weakSelectionMatcher{
		pages: map[uint64]PageMatch{}, detail: map[uint64]PageMatch{},
		weak: map[uint64]*weakSelectionEvidence{},
	}
	for id, page := range pages {
		if page.List != nil && page.List.weakSelection != nil {
			matcher.pages[id] = PageMatch{Page: PageUnknown, Evidence: "scripted weak public verdict"}
			matcher.detail[id] = page
			matcher.weak[id] = page.List.weakSelection
			continue
		}
		matcher.pages[id] = page
	}
	return weakSelectionRunWithMatcher(t, l, occupied, positions, matcher, frames, target)
}

func weakSelectionRunWithMatcher(t *testing.T, l Layout, occupied []bool, positions []int, matcher weakSelectionMatcher, frames []platform.Frame, target int) *inventoryRun {
	t.Helper()
	return &inventoryRun{
		inv: &Inventory{
			Layout: l, Matcher: matcher,
			ActionBudget: 200 * time.Millisecond, StableFrames: 2, DetailStableSpan: -1,
		},
		view: &weakSelectionView{frames: frames}, geo: 1,
		position: positions[target], filterActive: true,
		rangeProof: positionRange{Verified: true},
		ledger:     newSlotLedgerWithPositions(l.Columns, occupied, positions, 1),
	}
}

func weakSelectionMatcherFor(base, weak PageMatch, weakIDs ...uint64) weakSelectionMatcher {
	m := weakSelectionMatcher{
		pages: map[uint64]PageMatch{}, detail: map[uint64]PageMatch{},
		weak: map[uint64]*weakSelectionEvidence{}, thumbs: map[uint64][2]int{},
	}
	for _, id := range weakIDs {
		m.pages[id] = PageMatch{Page: PageUnknown, Evidence: "scripted weak public verdict"}
		m.detail[id] = weak
		m.weak[id] = weak.List.weakSelection
	}
	return m
}

func (m weakSelectionMatcher) withPage(id uint64, p PageMatch) weakSelectionMatcher {
	m.pages[id] = p
	return m
}

func weakSelectionFrame(id uint64, l Layout) platform.Frame {
	return platform.Frame{ID: id, Geometry: 1, Image: image.NewRGBA(l.Content), Content: l.Content, ArrivedAt: time.Now()}
}

func weakSelectionReviewBorder(l Layout, f platform.Frame, slot, hits int) {
	r := l.BorderRect(slot)
	gold := color.RGBA{R: 224, G: 208, B: 67, A: 255}
	for i := 0; i < hits; i++ {
		f.Image.SetRGBA(r.Min.X+i%r.Dx(), r.Min.Y+i/r.Dx(), gold)
	}
}

func weakSelectionReviewDetail(m *PixelMatcher, f platform.Frame, position int) {
	white := image.NewUniform(color.RGBA{R: 255, G: 255, B: 255, A: 255})
	gold := image.NewUniform(color.RGBA{R: 224, G: 208, B: 67, A: 255})
	for _, r := range m.L.DetailLines {
		draw.Draw(f.Image, r.Rect, white, image.Point{}, draw.Src)
	}
	draw.Draw(f.Image, m.L.QualityBadge, gold, image.Point{}, draw.Src)
	for y, row := range m.L.DetailPositions[position-1].masks[0].rows {
		for x := range len(row) {
			if row[x] == '#' {
				f.Image.SetRGBA(m.L.DetailPositionBadge.Min.X+x, m.L.DetailPositionBadge.Min.Y+y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
			}
		}
	}
}

func TestWeakSelectionMatcherKeepsStructureAtHit70(t *testing.T) {
	l := StorageLayout()
	m := NewPixelMatcher(l, StorageCalibration())
	f := storageReviewPixelFrame(l, 1, 10, 300, 323)
	r := l.BorderRect(9)
	for i := 0; i < 70; i++ {
		f.Image.SetRGBA(r.Min.X+i%r.Dx(), r.Min.Y+i/r.Dx(), color.RGBA{R: 224, G: 208, B: 67, A: 255})
	}
	if got := countSelectionPixels(f.Image, r); got != 70 {
		t.Fatalf("fixture hits = %d, want 70", got)
	}
	public := m.Match(f)
	if public.Page != PageUnknown || public.List != nil ||
		!strings.Contains(public.Evidence, "slot 9's selection border has 70 hits") {
		t.Fatalf("public weak verdict = %+v, want the original PageUnknown evidence", public)
	}
	detailed, weak := m.weakSelectionEvidence(f)
	if weak == nil || weak.Slot != 9 || weak.Hits != 70 ||
		detailed.Page != PageDriveStorage || detailed.List == nil || detailed.List.Selected != -1 {
		t.Fatalf("private weak evidence = page %s list %+v weak %+v", detailed.Page, detailed.List, weak)
	}
}

func TestWeakSelectionMatcherRejectsTwoWeakCandidates(t *testing.T) {
	l := StorageLayout()
	m := NewPixelMatcher(l, StorageCalibration())
	f := storageReviewPixelFrame(l, 1, 10, 300, 323)
	for _, slot := range []int{9, 10} {
		r := l.BorderRect(slot)
		for i := 0; i < 70; i++ {
			f.Image.SetRGBA(r.Min.X+i%r.Dx(), r.Min.Y+i/r.Dx(), color.RGBA{R: 224, G: 208, B: 67, A: 255})
		}
	}
	got := m.Match(f)
	if got.Page != PageUnknown || got.List != nil {
		t.Fatalf("two weak selections = %+v, want PageUnknown", got)
	}
}

func TestWeakSelectionMatcherRejectsSecondCandidate(t *testing.T) {
	l := StorageLayout()
	m := NewPixelMatcher(l, StorageCalibration())
	f := storageReviewPixelFrame(l, 1, 10, 300, 323)
	accent := image.NewUniform(color.RGBA{R: 224, G: 208, B: 67, A: 255})
	draw.Draw(f.Image, l.BorderRect(9), accent, image.Point{}, draw.Src)
	r := l.BorderRect(10)
	for i := 0; i < 70; i++ {
		f.Image.SetRGBA(r.Min.X+i%r.Dx(), r.Min.Y+i/r.Dx(), color.RGBA{R: 224, G: 208, B: 67, A: 255})
	}
	got := m.Match(f)
	if got.Page != PageUnknown || got.List != nil {
		t.Fatalf("strong+weak selection = %+v, want PageUnknown", got)
	}
}

func TestWeakSelectionListVerdictRejectsWithoutNormalWait(t *testing.T) {
	l, occupied, positions, base := weakSelectionFixture(9)
	weak := weakSelectionPage(base, 9, 70)
	run := weakSelectionRun(t, l, occupied, positions, nil, nil, 9)
	if run.listVerdict(weak) {
		t.Fatal("weak selection was accepted as a normal list verdict")
	}
}

func TestWeakSelectionPrivateHintRejectsInvalidFrames(t *testing.T) {
	l := StorageLayout()
	m := NewPixelMatcher(l, StorageCalibration())
	base := storageReviewPixelFrame(l, 1, 10, 300, 323)
	valid := base.Image
	badBounds := image.NewRGBA(image.Rect(5, 5, 5+l.Image.X, 5+l.Image.Y))
	cases := []struct {
		name  string
		frame platform.Frame
		m     *PixelMatcher
	}{
		{name: "nil-image", frame: platform.Frame{ID: 2, Geometry: 1, Content: l.Content}, m: m},
		{name: "small-image", frame: platform.Frame{ID: 2, Geometry: 1, Image: image.NewRGBA(image.Rect(0, 0, 64, 64)), Content: l.Content}, m: m},
		{name: "non-zero-bounds", frame: platform.Frame{ID: 2, Geometry: 1, Image: badBounds, Content: l.Content}, m: m},
		{name: "wrong-content", frame: platform.Frame{ID: 2, Geometry: 1, Image: valid, Content: image.Rect(0, 0, 1, 1)}, m: m},
		{name: "nil-matcher", frame: platform.Frame{ID: 2, Geometry: 1, Image: valid, Content: l.Content}, m: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			page, weak := tc.m.weakSelectionEvidence(tc.frame)
			if page.Page != PageUnknown || page.List != nil || weak != nil {
				t.Fatalf("invalid frame was accepted as weak evidence: page=%s list=%+v weak=%+v", page.Page, page.List, weak)
			}
		})
	}
}

func TestWeakSelectionReusedStrongCannotCompleteRecovery(t *testing.T) {
	l, occupied, positions, base := weakSelectionFixture(9)
	weak := weakSelectionPage(base, 9, 70)
	third := weakSelectionFrame(4, l)
	fourth := weakSelectionFrame(4, l)
	fourth.Reused = true
	frames := []platform.Frame{
		weakSelectionFrame(2, l), weakSelectionFrame(3, l), third, fourth,
	}
	run := weakSelectionRun(t, l, occupied, positions,
		map[uint64]PageMatch{2: base, 3: weak, 4: base}, frames, 9)
	out, err := run.awaitDetailFrames(context.Background(), weakSelectionFrame(1, l), base.Detail, 9, false, nil, true)
	if err == nil && out.class == awaitStable {
		t.Fatalf("reused strong frame completed weak recovery: class=%v frame=%d err=%v", out.class, out.frame.ID, err)
	}
}

func TestWeakSelectionTargetWaitRecoversOnFreshSelectedFrame(t *testing.T) {
	l, occupied, positions, base := weakSelectionFixture(9)
	weak := weakSelectionPage(base, 9, 70)
	pages := map[uint64]PageMatch{2: base, 3: weak, 4: base, 5: base}
	frames := []platform.Frame{
		weakSelectionFrame(2, l), weakSelectionFrame(3, l),
		weakSelectionFrame(4, l), weakSelectionFrame(5, l),
	}
	run := weakSelectionRun(t, l, occupied, positions, pages, frames, 9)
	baseline := weakSelectionFrame(1, l)
	out, err := run.awaitDetailFrames(context.Background(), baseline, base.Detail, 9, false, nil, true)
	if err != nil || out.class != awaitStable {
		t.Fatalf("weak→fresh target wait class=%v err=%v", out.class, err)
	}
	if out.frame.ID != 5 || !out.sawTarget {
		t.Fatalf("admitted frame=%d sawTarget=%t, want fresh frame 5", out.frame.ID, out.sawTarget)
	}
}

func TestWeakSelectionDetailViewportUsesIndependentThumb(t *testing.T) {
	l := StorageLayout()
	m := NewScrollEvidenceMatcher(NewPixelMatcher(l, StorageCalibration()))
	before := storageReviewPixelFrame(l, 1, 10, 300, 323)
	accent := image.NewUniform(color.RGBA{R: 224, G: 208, B: 67, A: 255})
	draw.Draw(before.Image, l.BorderRect(9), accent, image.Point{}, draw.Src)
	weak := storageReviewPixelFrame(l, 2, 10, 300, 323)
	r := l.BorderRect(9)
	for i := 0; i < 70; i++ {
		weak.Image.SetRGBA(r.Min.X+i%r.Dx(), r.Min.Y+i/r.Dx(), color.RGBA{R: 224, G: 208, B: 67, A: 255})
	}
	run := &inventoryRun{inv: &Inventory{Layout: l, Matcher: m}}
	if ok, why := run.detailViewportSame(before, weak); !ok {
		t.Fatalf("same viewport was not proven: %s", why)
	}
	moved := storageReviewPixelFrame(l, 3, 10, 330, 353)
	if ok, _ := run.detailViewportSame(before, moved); ok {
		t.Fatal("moved scrollbar thumb was accepted as the same viewport")
	}
}

func TestWeakSelectionActualMatcherFreshAdmission(t *testing.T) {
	testWeakSelectionActualMatcherFreshAdmission(t, false)
}

func TestWeakSelectionBeforeDetailBadgeSettles(t *testing.T) {
	testWeakSelectionActualMatcherFreshAdmission(t, true)
}

func testWeakSelectionActualMatcherFreshAdmission(t *testing.T, deferFirstDetail bool) {
	l := StorageLayout()
	m := NewScrollEvidenceMatcher(NewPixelMatcher(l, StorageCalibration()))
	makeFrame := func(id uint64, selected int, weak bool) platform.Frame {
		f := storageReviewPixelFrame(l, id, 10, 300, 323)
		if selected >= 0 {
			if weak {
				weakSelectionReviewBorder(l, f, selected, 70)
			} else {
				draw.Draw(f.Image, l.BorderRect(selected), image.NewUniform(color.RGBA{R: 224, G: 208, B: 67, A: 255}), image.Point{}, draw.Src)
			}
		}
		weakSelectionReviewDetail(m.PixelMatcher, f, 3)
		return f
	}
	pre := storageReviewPixelFrame(l, 1, 10, 300, 323)
	first := makeFrame(2, 9, false)
	if deferFirstDetail {
		// Frames 1389 and 1390 showed a strong target with an unreadable
		// detail badge, followed by a weak border on the same target.
		draw.Draw(first.Image, l.DetailPositionBadge, image.Black, image.Point{}, draw.Src)
		draw.Draw(first.Image, l.QualityBadge, image.Black, image.Point{}, draw.Src)
		fr := m.Match(first)
		if fr.Page != PageDriveStorage || fr.List == nil || fr.List.Selected != 9 ||
			fr.Detail == nil || fr.Detail.Position.known() {
			t.Fatalf("fixture does not represent a strong target with pending detail: %s", fr.Evidence)
		}
	}
	weak := makeFrame(3, 9, true)
	last1 := makeFrame(4, 9, false)
	last2 := makeFrame(5, 9, false)
	preMatch := m.Match(pre)
	if preMatch.Page != PageDriveStorage || preMatch.List == nil {
		t.Fatalf("pre frame is not storage: %s", preMatch.Evidence)
	}
	// This exercises the real pixel matcher, whose CPU time is amplified
	// by the race detector. The assertion concerns frame admission.
	run := &inventoryRun{
		inv:  &Inventory{Layout: l, Matcher: m, ActionBudget: 10 * time.Second, StableFrames: 2, DetailStableSpan: -1},
		view: &weakSelectionView{frames: []platform.Frame{first, weak, last1, last2}}, geo: 1,
		position: preMatch.List.Positions[9].Position, filterActive: true,
		rangeProof: positionRange{Verified: true},
		ledger:     runNewLedger(l, preMatch),
	}
	run.abTiming = startStorageABTiming()
	defer func() { run.abTiming.finish(nil) }()
	out, err := run.awaitDetailFrames(context.Background(), pre, preMatch.Detail, 9, false, nil, true)
	if err != nil || out.class != awaitStable {
		t.Fatalf("actual matcher weak recovery class=%v err=%v match=%s", out.class, err, out.match.Evidence)
	}
	if out.frame.ID != 5 || out.frame.Image == nil {
		t.Fatalf("actual recovery saved frame=%d imageNil=%t, want fresh frame 5", out.frame.ID, out.frame.Image == nil)
	}
}

func runNewLedger(l Layout, m PageMatch) *slotLedger {
	positions := make([]int, len(m.List.Positions))
	for i, p := range m.List.Positions {
		if p.known() {
			positions[i] = p.Position
		}
	}
	return newSlotLedgerWithPositions(l.Columns, m.List.Occupied, positions, 1)
}

func TestWeakSelectionSecondWeakCandidateFails(t *testing.T) {
	l, occupied, positions, base := weakSelectionFixture(9)
	weak := weakSelectionPage(base, 9, 70)
	matcher := weakSelectionMatcherFor(base, weak, 3, 4).withPage(2, base)
	frames := []platform.Frame{
		weakSelectionFrame(2, l), weakSelectionFrame(3, l),
		weakSelectionFrame(4, l), weakSelectionFrame(5, l),
	}
	run := weakSelectionRunWithMatcher(t, l, occupied, positions, matcher, frames, 9)
	out, err := run.awaitDetailFrames(context.Background(), weakSelectionFrame(1, l), base.Detail, 9, false, nil, true)
	if err == nil || out.frame.ID != 4 {
		t.Fatalf("second weak candidate was not rejected immediately: frame=%d err=%v", out.frame.ID, err)
	}
}

func TestWeakSelectionMovedThumbStopsAtWeakFrame(t *testing.T) {
	l, occupied, positions, base := weakSelectionFixture(9)
	weak := weakSelectionPage(base, 9, 70)
	matcher := weakSelectionMatcherFor(base, weak, 3).withPage(2, base)
	matcher.thumbs = map[uint64][2]int{3: {330, 353}}
	frames := []platform.Frame{
		weakSelectionFrame(2, l), weakSelectionFrame(3, l),
		weakSelectionFrame(4, l), weakSelectionFrame(5, l),
	}
	run := weakSelectionRunWithMatcher(t, l, occupied, positions, matcher, frames, 9)
	out, err := run.awaitDetailFrames(context.Background(), weakSelectionFrame(1, l), base.Detail, 9, false, nil, true)
	if err == nil || out.frame.ID != 3 {
		t.Fatalf("moved weak frame was not stopped immediately: frame=%d err=%v", out.frame.ID, err)
	}
}

func TestWeakSelectionPersistentWeakDoesNotAdmit(t *testing.T) {
	l, occupied, positions, base := weakSelectionFixture(9)
	weak := weakSelectionPage(base, 9, 70)
	pages := map[uint64]PageMatch{2: base, 3: weak, 4: weak, 5: weak}
	frames := []platform.Frame{
		weakSelectionFrame(2, l), weakSelectionFrame(3, l),
		weakSelectionFrame(4, l), weakSelectionFrame(5, l),
	}
	run := weakSelectionRun(t, l, occupied, positions, pages, frames, 9)
	out, err := run.awaitDetailFrames(context.Background(), weakSelectionFrame(1, l), base.Detail, 9, false, nil, true)
	if err == nil && out.class == awaitStable {
		t.Fatalf("persistent weak wait class=%v err=%v admitted the weak target", out.class, err)
	}
}

func TestWeakSelectionKnownWrongBadgeDoesNotDefer(t *testing.T) {
	l, occupied, positions, base := weakSelectionFixture(9)
	weak := weakSelectionPage(base, 9, 70)
	list := *weak.List
	list.Positions = append([]PositionVerdict(nil), weak.List.Positions...)
	list.Positions[9] = PositionVerdict{Position: 5}
	weak.List = &list
	frames := []platform.Frame{weakSelectionFrame(2, l), weakSelectionFrame(3, l)}
	run := weakSelectionRun(t, l, occupied, positions, map[uint64]PageMatch{2: base, 3: weak}, frames, 9)
	_, err := run.awaitDetailFrames(context.Background(), weakSelectionFrame(1, l), base.Detail, 9, false, nil, true)
	if err == nil || run.positionListVerdict(weak) {
		t.Fatalf("known wrong badge did not fail closed: err=%v", err)
	}
}

func TestWeakSelectionChangedGeometryFails(t *testing.T) {
	l, occupied, positions, base := weakSelectionFixture(9)
	weak := weakSelectionPage(base, 9, 70)
	frames := []platform.Frame{weakSelectionFrame(2, l), weakSelectionFrame(3, l), weakSelectionFrame(4, l)}
	frames[1].Geometry = 2
	run := weakSelectionRun(t, l, occupied, positions, map[uint64]PageMatch{2: base, 3: weak, 4: weak}, frames, 9)
	out, err := run.awaitDetailFrames(context.Background(), weakSelectionFrame(1, l), base.Detail, 9, false, nil, true)
	if err == nil && out.class == awaitStable {
		t.Fatal("weak selection with changed geometry was admitted")
	}
}

func TestWeakSelectionChangedFilterFails(t *testing.T) {
	l, occupied, positions, base := weakSelectionFixture(9)
	weak := weakSelectionPage(base, 9, 70)
	weak.Filter = &FilterEvidence{State: FilterClear}
	frames := []platform.Frame{weakSelectionFrame(2, l), weakSelectionFrame(3, l), weakSelectionFrame(4, l)}
	run := weakSelectionRun(t, l, occupied, positions, map[uint64]PageMatch{2: base, 3: weak, 4: weak}, frames, 9)
	if _, err := run.awaitDetailFrames(context.Background(), weakSelectionFrame(1, l), base.Detail, 9, false, nil, true); err == nil {
		t.Fatal("weak selection with a changed filter did not fail closed")
	}
}

func TestWeakSelectionRequiresAccountableLedger(t *testing.T) {
	l, occupied, positions, base := weakSelectionFixture(9)
	weak := weakSelectionPage(base, 9, 70)
	frames := []platform.Frame{weakSelectionFrame(2, l), weakSelectionFrame(3, l), weakSelectionFrame(4, l)}
	run := weakSelectionRun(t, l, occupied, positions, map[uint64]PageMatch{2: base, 3: weak, 4: weak}, frames, 9)
	run.ledger = nil
	out, err := run.awaitDetailFrames(context.Background(), weakSelectionFrame(1, l), base.Detail, 9, false, nil, true)
	if err == nil && out.class == awaitStable {
		t.Fatal("weak selection without a ledger was admitted")
	}
}

func TestWeakSelectionReusedFrameCannotRecover(t *testing.T) {
	l, occupied, positions, base := weakSelectionFixture(9)
	weak := weakSelectionPage(base, 9, 70)
	frames := []platform.Frame{weakSelectionFrame(2, l), weakSelectionFrame(3, l)}
	frames[1].Reused = true
	run := weakSelectionRun(t, l, occupied, positions, map[uint64]PageMatch{2: base, 3: weak}, frames, 9)
	_, err := run.awaitDetailFrames(context.Background(), weakSelectionFrame(1, l), base.Detail, 9, false, nil, true)
	if err == nil || run.positionListVerdict(weak) {
		t.Fatalf("reused weak frame did not fail closed: err=%v", err)
	}
}

func TestWeakSelectionWrongTargetDoesNotDefer(t *testing.T) {
	l, occupied, positions, base := weakSelectionFixture(9)
	weak := weakSelectionPage(base, 8, 70)
	frames := []platform.Frame{weakSelectionFrame(2, l), weakSelectionFrame(3, l)}
	run := weakSelectionRun(t, l, occupied, positions, map[uint64]PageMatch{2: base, 3: weak}, frames, 9)
	_, err := run.awaitDetailFrames(context.Background(), weakSelectionFrame(1, l), base.Detail, 9, false, nil, true)
	if err == nil || run.positionListVerdict(weak) {
		t.Fatalf("wrong-target weak frame did not fail closed: err=%v", err)
	}
}

func TestWeakSelectionWaitsForUnreadableNonTargetBadge(t *testing.T) {
	const target, other = 27, 26
	for _, tc := range []struct {
		name                 string
		weakBadge, nextBadge PositionVerdict
		wantStable           bool
	}{
		{"recovers on fresh strong frames", PositionVerdict{Conflict: true}, PositionVerdict{Position: 3}, true},
		{"persistent unreadable badge does not admit", PositionVerdict{Conflict: true}, PositionVerdict{Conflict: true}, false},
		{"known wrong badge at weak frame stops", PositionVerdict{Position: 2}, PositionVerdict{Position: 3}, false},
		{"known wrong badge after weak frame stops", PositionVerdict{Conflict: true}, PositionVerdict{Position: 2}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l, occupied, positions, strong := weakSelectionFixture(target)
			withOtherBadge := func(p PageMatch, badge PositionVerdict) PageMatch {
				list := *p.List
				list.Positions = append([]PositionVerdict(nil), p.List.Positions...)
				list.Positions[other] = badge
				p.List = &list
				return p
			}
			weak := withOtherBadge(weakSelectionPage(strong, target, 70), tc.weakBadge)
			next := withOtherBadge(strong, tc.nextBadge)
			frames := []platform.Frame{
				weakSelectionFrame(2, l), weakSelectionFrame(3, l),
				weakSelectionFrame(4, l), weakSelectionFrame(5, l),
			}
			run := weakSelectionRun(t, l, occupied, positions, map[uint64]PageMatch{2: strong, 3: weak, 4: next, 5: next}, frames, target)
			out, err := run.awaitDetailFrames(context.Background(), weakSelectionFrame(1, l), strong.Detail, target, false, nil, true)
			if tc.wantStable && (err != nil || out.class != awaitStable || out.frame.ID != 5 || !out.sawWeakSelection) {
				t.Fatalf("expected fresh complete recovery after weak frame: class=%v frame=%d weak=%v err=%v", out.class, out.frame.ID, out.sawWeakSelection, err)
			}
			if !tc.wantStable && err == nil && out.class == awaitStable {
				t.Fatalf("unexpected acceptance of incomplete or wrong badge: frame=%d", out.frame.ID)
			}
		})
	}
}

type weakSelectionDeadlineView struct {
	weakSelectionView
	recoveryDelay time.Duration
}

func (v *weakSelectionDeadlineView) Frame(ctx context.Context, req platform.FrameRequest) (platform.Frame, error) {
	var delay time.Duration
	switch v.next {
	case 2:
		deadline, _ := ctx.Deadline()
		delay = time.Until(deadline) - 70*time.Millisecond
	case 3, 4:
		delay = 50 * time.Millisecond
		if v.recoveryDelay > 0 {
			delay = v.recoveryDelay
		}
	}
	if delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return platform.Frame{}, ctx.Err()
		case <-timer.C:
		}
	}
	return v.weakSelectionView.Frame(ctx, req)
}

func TestWeakSelectionLateRecoveryCanSettleSameDetail(t *testing.T) {
	const target = 18
	l, occupied, positions, strong := weakSelectionFixture(target)
	weak := weakSelectionPage(strong, target, 70)
	frames := []platform.Frame{
		weakSelectionFrame(2, l), weakSelectionFrame(3, l), weakSelectionFrame(4, l),
		weakSelectionFrame(5, l), weakSelectionFrame(6, l),
	}
	run := weakSelectionRun(t, l, occupied, positions, map[uint64]PageMatch{2: strong, 3: weak, 4: strong, 5: strong, 6: strong}, frames, target)
	run.inv.ActionBudget = 300 * time.Millisecond
	run.inv.StableFrames = 3
	run.inv.DetailStableSpan = 80 * time.Millisecond
	run.view = &weakSelectionDeadlineView{weakSelectionView: weakSelectionView{frames: frames}}
	// The complete late frames still compare equal to the pre-click
	// pane. They can permit a reference round trip, never direct admission.
	out, err := run.awaitDetailFrames(context.Background(), weakSelectionFrame(12, l), strong.Detail, target, false, nil, true)
	if err != nil || out.class != awaitDeadline || out.frame.ID != 6 || !run.sameDetailSelection(out, target) {
		t.Fatalf("late strong frames did not establish eligibility for reference: class=%v frame=%d stableSame=%v err=%v",
			out.class, out.frame.ID, out.settledNotDifferent, err)
	}
}

type weakPrecheckSession struct {
	platform.Session
	view *weakSelectionView
}

func (s weakPrecheckSession) Observe() (platform.Observation, error) { return s.view.Observe() }
func (s weakPrecheckSession) Frame(ctx context.Context, req platform.FrameRequest) (platform.Frame, error) {
	return s.view.Frame(ctx, req)
}

func TestWeakSelectionPrecheckWaitsForFreshListWithoutInput(t *testing.T) {
	l, occupied, positions, strong := weakSelectionFixture(18)
	weak := weakSelectionPage(strong, 18, 70)
	for _, tc := range []struct {
		name      string
		final     PageMatch
		wantReady bool
	}{
		{"strong follows weak", strong, true},
		{"weak never resolves", weak, false},
		{"real page change stops", PageMatch{Page: PageUnknown}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frames := []platform.Frame{weakSelectionFrame(1, l), weakSelectionFrame(2, l)}
			run := weakSelectionRun(t, l, occupied, positions, map[uint64]PageMatch{1: weak, 2: tc.final}, frames, 18)
			sess := weakPrecheckSession{view: run.view.(*weakSelectionView)}
			err := run.inv.Precheck(context.Background(), sess)
			if (err == nil) != tc.wantReady || sess.view.next != len(frames) {
				t.Fatalf("precheck ready=%v received=%d err=%v", err == nil, sess.view.next, err)
			}
		})
	}
}

func TestWeakSelectionCompleteGridCanVerifyIncompleteStrongAnchor(t *testing.T) {
	const target, other = 27, 19
	l, occupied, positions, strong := weakSelectionFixture(target)
	pending := strong
	pending.Detail = &DetailEvidence{Substats: 4}
	list := *strong.List
	list.Positions = append([]PositionVerdict(nil), strong.List.Positions...)
	list.Positions[other] = PositionVerdict{}
	pending.List = &list
	for _, tc := range []struct {
		name                       string
		incompleteWeak, movedThumb bool
		wantStable                 bool
	}{
		{"complete weak frame followed by fresh recovery", false, false, true},
		{"two incomplete views cannot prove the list", true, false, false},
		{"changed scrollbar cannot prove the list", false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			weak := weakSelectionPage(strong, target, 70)
			if tc.incompleteWeak {
				wl := *weak.List
				wl.Positions = list.Positions
				weak.List = &wl
			}
			matcher := weakSelectionMatcherFor(strong, weak, 3).withPage(2, pending).withPage(4, strong).withPage(5, strong)
			if tc.movedThumb {
				matcher.thumbs[3] = [2]int{311, 334}
			}
			frames := []platform.Frame{weakSelectionFrame(2, l), weakSelectionFrame(3, l), weakSelectionFrame(4, l), weakSelectionFrame(5, l)}
			run := weakSelectionRunWithMatcher(t, l, occupied, positions, matcher, frames, target)
			out, err := run.awaitDetailFrames(context.Background(), weakSelectionFrame(1, l), strong.Detail, target, false, nil, true)
			stable := err == nil && out.class == awaitStable && out.frame.ID == 5 && out.sawWeakSelection
			if stable != tc.wantStable {
				t.Fatalf("fresh complete recovery=%v want=%v frame=%d err=%v", stable, tc.wantStable, out.frame.ID, err)
			}
		})
	}
}

func TestWeakSelectionSeparatedGapsRequireFreshStrongStability(t *testing.T) {
	const target = 18
	l, occupied, positions, strong := weakSelectionFixture(target)
	weak := weakSelectionPage(strong, target, 70)
	// As with the observed frames at R105C1, the target oscillates
	// between two verified strong reads and another single weak sample.
	for _, last := range []int{6, 7} {
		frames := make([]platform.Frame, 0, last-1)
		pages := make(map[uint64]PageMatch)
		for id := 2; id <= last; id++ {
			frames = append(frames, weakSelectionFrame(uint64(id), l))
			pages[uint64(id)] = strong
		}
		pages[3], pages[6] = weak, weak
		run := weakSelectionRun(t, l, occupied, positions, pages, frames, target)
		run.inv.StableFrames = 3
		out, err := run.awaitDetailFrames(context.Background(), weakSelectionFrame(12, l), strong.Detail, target, false, nil, true)
		if last == 6 {
			if err == nil && run.sameDetailSelection(out, target) {
				t.Fatal("weak frame counted as the third strong sample")
			}
			continue
		}
		if err != nil || out.frame.ID != 7 || out.class != awaitDeadline || !run.sameDetailSelection(out, target) {
			t.Fatalf("strong sample after separated weak gap did not prove stability for reference: class=%v frame=%d err=%v", out.class, out.frame.ID, err)
		}
	}
}

func TestWeakSelectionSamePositionReferenceUsesVerifiedWait(t *testing.T) {
	const target = 9
	l, occupied, positions, strong := weakSelectionFixture(target)
	weak := weakSelectionPage(strong, target, 70)
	frames := []platform.Frame{
		weakSelectionFrame(2, l), weakSelectionFrame(3, l),
		weakSelectionFrame(4, l), weakSelectionFrame(5, l),
		weakSelectionFrame(6, l), weakSelectionFrame(7, l),
	}
	pages := map[uint64]PageMatch{2: strong, 3: weak, 4: strong, 5: strong, 6: weak, 7: strong}
	run := weakSelectionRun(t, l, occupied, positions, pages, frames, target)
	run.inv.StableFrames = 3
	baseline := weakSelectionFrame(1, l)
	// For a reference click, a genuinely different pane can be accepted
	// only after the same three fresh strong samples.
	out, err := run.awaitReferenceDetail(context.Background(), baseline, strong.Detail, target)
	if err != nil || out.class != awaitStable || out.frame.ID != 7 || !out.sawWeakSelection {
		t.Fatalf("reference returned without complete fresh stability: class=%v frame=%d weak=%v err=%v",
			out.class, out.frame.ID, out.sawWeakSelection, err)
	}
}

func TestStorageReferencePrefersDifferentVerifiedPosition(t *testing.T) {
	const target, nearest, different = 18, 9, 21
	l, occupied, positions, strong := weakSelectionFixture(target)
	for i := range strong.List.Positions {
		strong.List.Positions[i] = PositionVerdict{Position: 4}
		positions[i] = 4
	}
	strong.List.Positions[different] = PositionVerdict{Position: 5}
	positions[different] = 5
	run := weakSelectionRun(t, l, occupied, positions, nil, nil, target)
	if got, ok := run.referenceSlot(strong, target, 17); !ok || got != different {
		t.Fatalf("reference=%d ok=%v, want a different position (%d, same-position nearest=%d)", got, ok, different, nearest)
	}
}

func TestStorageDetailPositionCanProveDifferentPanes(t *testing.T) {
	l := StorageLayout()
	m := NewPixelMatcher(l, StorageCalibration())
	target := storageReviewPixelFrame(l, 1, 10, 300, 323)
	ref := storageReviewPixelFrame(l, 2, 10, 300, 323)
	weakSelectionReviewDetail(m, target, 4)
	weakSelectionReviewDetail(m, ref, 5)
	if mean, ok := m.DetailMAD(target, ref); !ok || mean >= m.C.DetailSameMax {
		t.Fatalf("fixture does not have similar text regions: mad=%f ok=%v", mean, ok)
	}
	run := &inventoryRun{inv: &Inventory{Layout: l, Matcher: m}}
	if got := run.compareDetailBaseline(target, ref, m.detailEvidence(target.Image), m.detailEvidence(ref.Image)); got != DetailDifferent {
		t.Fatalf("independent detail badges 4 and 5 should prove different panes: %v", got)
	}
}

func TestWeakSelectionAtDeadlineAllowsFreshRecovery(t *testing.T) {
	const target = 27
	l, occupied, positions, strong := weakSelectionFixture(target)
	weak := weakSelectionPage(strong, target, 70)
	frames := []platform.Frame{
		weakSelectionFrame(2, l), weakSelectionFrame(3, l), weakSelectionFrame(4, l),
		weakSelectionFrame(5, l), weakSelectionFrame(6, l),
	}
	run := weakSelectionRun(t, l, occupied, positions, map[uint64]PageMatch{
		2: strong, 3: strong, 4: weak, 5: strong, 6: strong,
	}, frames, target)
	run.inv.ActionBudget = 300 * time.Millisecond
	run.inv.StableFrames = 2
	run.inv.DetailStableSpan = 40 * time.Millisecond
	run.view = &weakSelectionDeadlineView{weakSelectionView: weakSelectionView{frames: frames}, recoveryDelay: 100 * time.Millisecond}
	out, err := run.awaitDetailFrames(context.Background(), weakSelectionFrame(12, l), strong.Detail, target, false, nil, true)
	if err != nil || out.frame.ID != 6 || !run.sameDetailSelection(out, target) || !out.sawWeakSelection {
		t.Fatalf("late weak frame stranded recovery: class=%v frame=%d stableSame=%v err=%v",
			out.class, out.frame.ID, out.settledNotDifferent, err)
	}
}
func TestWeakSelectionWithUnreadableDetailAndOneNonTargetBadge(t *testing.T) {
	const target, other = 27, 19
	for _, name := range []string{"recovers", "persistent", "reused", "known wrong", "two unknown", "conflicting badge", "changed thumb"} {
		t.Run(name, func(t *testing.T) {
			l, occupied, positions, strong := weakSelectionFixture(target)
			weak := weakSelectionPage(strong, target, 70)
			weak.List.Positions = append([]PositionVerdict(nil), weak.List.Positions...)
			weak.List.Positions[other] = PositionVerdict{}
			weak.Detail = &DetailEvidence{Substats: 4}
			switch name {
			case "known wrong":
				weak.List.Positions[other] = PositionVerdict{Position: 6}
			case "two unknown":
				weak.List.Positions[other+1] = PositionVerdict{}
			case "conflicting badge":
				weak.List.Positions[other] = PositionVerdict{Conflict: true}
			}
			next := strong
			if name == "persistent" {
				next = weak
			}
			frames := []platform.Frame{
				weakSelectionFrame(2, l), weakSelectionFrame(3, l),
				weakSelectionFrame(4, l), weakSelectionFrame(5, l),
			}
			// These captures model distinct successive frames. Give them
			// explicit times rather than relying on host clock resolution.
			firstArrival := time.Now()
			for i := range frames {
				frames[i].ArrivedAt = firstArrival.Add(time.Duration(i) * time.Millisecond)
			}
			if name == "reused" {
				frames[2].Reused, frames[3].Reused = true, true
			}
			anchor := strong
			anchor.Detail = nil
			run := weakSelectionRun(t, l, occupied, positions, map[uint64]PageMatch{2: anchor, 3: weak, 4: next, 5: next}, frames, target)
			if name == "changed thumb" {
				m := run.inv.Matcher.(weakSelectionMatcher)
				m.thumbs = map[uint64][2]int{3: {301, 324}}
				run.inv.Matcher = m
			}
			out, err := run.awaitDetailFrames(context.Background(), weakSelectionFrame(1, l), strong.Detail, target, false, nil, true)
			if name == "recovers" {
				if err != nil || out.class != awaitStable || out.frame.ID != 5 || !out.sawWeakSelection {
					t.Fatalf("expected stable fresh frame 5, got frame %d class=%v weak=%v err=%v", out.frame.ID, out.class, out.sawWeakSelection, err)
				}
			} else if err == nil && out.class == awaitStable {
				t.Fatalf("unsafe recovery on frame %d", out.frame.ID)
			}
		})
	}
}
func TestWeakSelectionWithVerifiedPreClickAnchor(t *testing.T) {
	const target, other = 27, 19
	tests := []struct {
		name                        string
		weakFrames                  int
		badge                       PositionVerdict
		noAnchor, drift, wantStable bool
	}{
		{name: "1900 recovers", weakFrames: 1, badge: PositionVerdict{Position: 2}, wantStable: true},
		{name: "1024 waits on two conflicts", weakFrames: 1, badge: PositionVerdict{Conflict: true}, wantStable: true},
		{name: "later weak badge dropout is observed", weakFrames: 2, wantStable: true},
		{name: "no verified pre-click frame", weakFrames: 1, noAnchor: true},
		{name: "moved since pre-click", weakFrames: 1, drift: true},
		{name: "known wrong stays rejected", weakFrames: 1, badge: PositionVerdict{Position: 6}},
		{name: "competing class without verified anchor stays rejected", weakFrames: 1, badge: PositionVerdict{Conflict: true}, noAnchor: true},
		{name: "bounded continuation", weakFrames: 5},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			l, occupied, positions, strong := weakSelectionFixture(target)
			incomplete := strong
			incomplete.Detail = nil
			list := *strong.List
			list.Positions = append([]PositionVerdict(nil), strong.List.Positions...)
			list.Positions[34], list.Positions[35] = PositionVerdict{Conflict: true}, PositionVerdict{Conflict: true}
			incomplete.List = &list
			weak := weakSelectionPage(strong, target, 70)
			weak.Detail = nil
			weak.List.Positions = append([]PositionVerdict(nil), weak.List.Positions...)
			weak.List.Positions[other] = tc.badge
			if tc.name == "1024 waits on two conflicts" {
				incomplete.List = strong.List
				weak.List.Positions[other+1] = tc.badge
			}
			pre := weakSelectionFrame(1, l)
			frames := []platform.Frame{weakSelectionFrame(2, l)}
			pages := map[uint64]PageMatch{2: incomplete}
			for i := range tc.weakFrames {
				id := uint64(3 + i)
				frames = append(frames, weakSelectionFrame(id, l))
				pages[id] = weak
			}
			last := uint64(tc.weakFrames + 4)
			for _, id := range []uint64{last - 1, last} {
				frames = append(frames, weakSelectionFrame(id, l))
				pages[id] = strong
			}
			for i := range frames {
				frames[i].ArrivedAt = pre.ArrivedAt.Add(time.Duration(frames[i].ID-1) * time.Millisecond)
			}
			run := weakSelectionRun(t, l, occupied, positions, pages, frames, target)
			if tc.drift {
				m := run.inv.Matcher.(weakSelectionMatcher)
				m.thumbs = map[uint64][2]int{1: {301, 324}}
				run.inv.Matcher = m
			}
			anchor := pre
			if tc.noAnchor {
				anchor = platform.Frame{}
			}
			out, err := run.awaitDetailFramesAnchored(context.Background(), pre, strong.Detail, target, false, nil, true, anchor)
			if tc.wantStable {
				if err != nil || out.class != awaitStable || out.frame.ID != last || out.tentativeWeakFrames != tc.weakFrames {
					t.Fatalf("wanted fresh recovered frame %d, got frame %d class=%v tentative=%d err=%v", last, out.frame.ID, out.class, out.tentativeWeakFrames, err)
				}
			} else if err == nil && out.class == awaitStable {
				t.Fatalf("unexpectedly admitted on frame %d", out.frame.ID)
			}
		})
	}
}
func TestWeakSelectionVerifiedRecoveryAllowsOnlyTrailingThumbEdge(t *testing.T) {
	l := StorageLayout()
	m := NewScrollEvidenceMatcher(NewPixelMatcher(l, StorageCalibration()))
	before := storageReviewPixelFrame(l, 1, 10, 406, 428)
	run := &inventoryRun{inv: &Inventory{Layout: l, Matcher: m}}

	edge := storageReviewPixelFrame(l, 2, 10, 406, 429)
	if ok, _ := run.detailViewportSame(before, edge); ok {
		t.Fatal("ordinary viewport proof must keep its exact-edge rule")
	}
	if ok, why := run.detailViewportSameWithSingleEdge(before, edge, true); !ok {
		t.Fatalf("still list with a one-pixel trailing edge was rejected: %s", why)
	}
	leading := storageReviewPixelFrame(l, 6, 10, 407, 428)
	if ok, why := run.detailViewportSameWithSingleEdge(before, leading, true); !ok {
		t.Fatalf("still list with a one-pixel leading edge was rejected: %s", why)
	}
	shift := storageReviewPixelFrame(l, 3, 10, 407, 429)
	if ok, _ := run.detailViewportSameWithSingleEdge(before, shift, true); ok {
		t.Fatal("both thumb edges translated")
	}
	elastic := storageReviewPixelFrame(l, 4, 10, 406, 430)
	if ok, _ := run.detailViewportSameWithSingleEdge(before, elastic, true); ok {
		t.Fatal("more than one pixel of edge change accepted")
	}
	moved := storageReviewPixelFrame(l, 5, 11, 406, 429)
	weakSelectionReviewBorder(l, before, 9, 350)
	weakSelectionReviewBorder(l, moved, 18, 350)
	if ok, _ := run.detailViewportSameWithSingleEdge(before, moved, true); ok {
		t.Fatal("edge tolerance accepted separately detectable list movement")
	}
}

type weakDetailUndecidedMatcher struct{ weakSelectionMatcher }

func (m weakDetailUndecidedMatcher) CompareDetail(a, b platform.Frame) DetailComparison {
	if b.ID == 4 {
		return DetailUndecided
	}
	return m.weakSelectionMatcher.CompareDetail(a, b)
}

func TestWeakSelectionTwoFreshObservationsRequireNewStrongProof(t *testing.T) {
	const target = 9
	for _, name := range []string{"recovers", "weak detail compare undecided", "non-target ambiguity during repeat", "known wrong during repeat", "keeps weak frame cap", "reused cannot recover", "drift stops", "different detail stops"} {
		t.Run(name, func(t *testing.T) {
			l, occupied, positions, strong := weakSelectionFixture(target)
			pre := weakSelectionFrame(1, l)
			weak := weakSelectionPage(strong, target, 70)
			second := weak
			if name == "non-target ambiguity during repeat" || name == "known wrong during repeat" {
				list := *weak.List
				list.Positions = append([]PositionVerdict(nil), weak.List.Positions...)
				list.Positions[18], list.Positions[19], list.Positions[20] = PositionVerdict{Conflict: true}, PositionVerdict{}, PositionVerdict{Conflict: true}
				if name == "known wrong during repeat" {
					list.Positions[18] = PositionVerdict{Position: 6}
				}
				second.List = &list
			}
			if name == "different detail stops" {
				second.Detail = &DetailEvidence{Quality: QualityS, Substats: 4, Position: PositionVerdict{Position: 2}}
			}
			pages := map[uint64]PageMatch{2: strong, 3: weak, 4: second, 5: strong, 6: strong}
			if name == "keeps weak frame cap" {
				pages[5], pages[6], pages[7], pages[8] = weak, weak, strong, strong
			}
			frames := []platform.Frame{}
			for i := 2; i <= len(pages)+1; i++ {
				frames = append(frames, weakSelectionFrame(uint64(i), l))
			}
			if name == "reused cannot recover" {
				frames[len(frames)-1].Reused = true
			}
			for i := range frames {
				frames[i].ArrivedAt = pre.ArrivedAt.Add(time.Duration(frames[i].ID-1) * time.Millisecond)
			}
			run := weakSelectionRun(t, l, occupied, positions, pages, frames, target)
			if name == "drift stops" {
				m := run.inv.Matcher.(weakSelectionMatcher)
				m.thumbs = map[uint64][2]int{4: {307, 330}}
				run.inv.Matcher = m
			}
			if name == "weak detail compare undecided" {
				run.inv.Matcher = weakDetailUndecidedMatcher{run.inv.Matcher.(weakSelectionMatcher)}
			}
			out, err := run.awaitDetailFramesAnchored(context.Background(), pre, strong.Detail, target, false, nil, true, pre)
			if name == "recovers" || name == "weak detail compare undecided" || name == "non-target ambiguity during repeat" {
				if err != nil || out.class != awaitStable || out.frame.ID != 6 || out.verifiedWeakRepeats != 1 {
					t.Fatalf("wanted fresh strong frame 6 after the weak pair, got frame=%d class=%v repeats=%d err=%v", out.frame.ID, out.class, out.verifiedWeakRepeats, err)
				}
			} else if err == nil && out.class == awaitStable {
				t.Fatalf("unexpected admission on frame %d", out.frame.ID)
			}
		})
	}
}

type weakSelectionStillnessMatcher struct {
	weakSelectionMatcher
	moved bool
}

func (m weakSelectionStillnessMatcher) zeroShiftMovement(platform.Frame, platform.Frame) (bool, string) {
	return m.moved, "independent movement counter-evidence"
}

func TestWeakSelectionTransitionVerifiesTrailingEdgeOnWeakFrame(t *testing.T) {
	const target = 27
	for _, name := range []string{"recovers", "translated", "movement detector", "no pre-click proof"} {
		t.Run(name, func(t *testing.T) {
			l, occupied, positions, strong := weakSelectionFixture(target)
			pre := weakSelectionFrame(1, l)
			transitionalStrong := strong
			transitionalStrong.Detail = nil
			weak := weakSelectionPage(strong, target, 70)
			weak.Detail = nil
			frames := []platform.Frame{}
			for i := 2; i <= 5; i++ {
				f := weakSelectionFrame(uint64(i), l)
				f.ArrivedAt = pre.ArrivedAt.Add(time.Duration(i-1) * time.Millisecond)
				frames = append(frames, f)
			}
			run := weakSelectionRun(t, l, occupied, positions, map[uint64]PageMatch{2: transitionalStrong, 3: weak, 4: strong, 5: strong}, frames, target)
			m := run.inv.Matcher.(weakSelectionMatcher)
			m.thumbs = map[uint64][2]int{1: {406, 428}, 2: {406, 428}, 3: {406, 429}, 4: {406, 429}, 5: {406, 429}}
			if name == "translated" {
				m.thumbs[3] = [2]int{407, 429}
			}
			run.inv.Matcher = weakSelectionStillnessMatcher{weakSelectionMatcher: m, moved: name == "movement detector"}
			anchor := pre
			if name == "no pre-click proof" {
				anchor = platform.Frame{}
			}
			out, err := run.awaitDetailFramesAnchored(context.Background(), pre, strong.Detail, target, false, nil, true, anchor)
			if name == "recovers" {
				if err != nil || out.class != awaitStable || out.frame.ID != 5 {
					t.Fatalf("expected fresh strong frame 5, got frame=%d class=%v err=%v", out.frame.ID, out.class, err)
				}
			} else if err == nil && out.class == awaitStable {
				t.Fatalf("unexpected acceptance on frame %d", out.frame.ID)
			}
		})
	}
}
func TestWeakSelectionDuringTargetBadgeRecovery(t *testing.T) {
	const target = 0
	for _, name := range []string{"recover", "weak with unreadable target", "weak with competing target", "unknown without target evidence", "wrong target"} {
		t.Run(name, func(t *testing.T) {
			l, occupied, positions, strong := weakSelectionFixture(target)
			targetUnreadable := strong
			tl := *strong.List
			tl.Positions = append([]PositionVerdict(nil), tl.Positions...)
			tl.Positions[target] = PositionVerdict{}
			targetUnreadable.List = &tl
			weak := weakSelectionPage(strong, target, 70)
			weak.List.Positions = append([]PositionVerdict(nil), weak.List.Positions...)
			weak.List.Positions[1] = PositionVerdict{}
			switch name {
			case "weak with unreadable target":
				weak.List.Positions[target], weak.List.Positions[1] = PositionVerdict{}, strong.List.Positions[1]
			case "weak with competing target":
				weak.List.Positions[target], weak.List.Positions[1] = PositionVerdict{Conflict: true}, strong.List.Positions[1]
			case "unknown without target evidence":
				weak = PageMatch{Page: PageUnknown}
			case "wrong target":
				weak.List.weakSelection = &weakSelectionEvidence{Slot: 1, Hits: 70}
			}
			pre := weakSelectionFrame(1, l)
			frames := []platform.Frame{}
			for i := 2; i <= 6; i++ {
				f := weakSelectionFrame(uint64(i), l)
				f.ArrivedAt = pre.ArrivedAt.Add(time.Duration(i-1) * time.Millisecond)
				frames = append(frames, f)
			}
			run := weakSelectionRun(t, l, occupied, positions, map[uint64]PageMatch{2: strong, 3: targetUnreadable, 4: weak, 5: strong, 6: strong}, frames, target)
			out, err := run.awaitDetailFramesAnchored(context.Background(), pre, strong.Detail, target, false, nil, true, pre)
			if name == "recover" || name == "weak with unreadable target" {
				if err != nil || out.class != awaitStable || out.frame.ID != 6 || !out.sawTargetBadgeTransient || !out.sawWeakSelection {
					t.Fatalf("expected strong recovery, got frame=%d class=%v target-wait=%v weak=%v err=%v", out.frame.ID, out.class, out.sawTargetBadgeTransient, out.sawWeakSelection, err)
				}
			} else if err == nil && out.class == awaitStable {
				t.Fatalf("admitted an unverified target on frame %d", out.frame.ID)
			}
		})
	}
}
