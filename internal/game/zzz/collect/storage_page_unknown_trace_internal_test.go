//go:build page_unknown_trace

package collect

import (
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/platform"
)

type pageUnknownTestView struct {
	frames []platform.Frame
	next   int
}

func (v *pageUnknownTestView) Observe() (platform.Observation, error) {
	return platform.Observation{Ready: true, Geometry: 1, At: time.Now()}, nil
}

func (v *pageUnknownTestView) Frame(context.Context, platform.FrameRequest) (platform.Frame, error) {
	if v.next >= len(v.frames) {
		return platform.Frame{}, context.DeadlineExceeded
	}
	f := v.frames[v.next]
	v.next++
	return f, nil
}

func (v *pageUnknownTestView) Dispatch(platform.InputOp) platform.InputResult {
	return platform.InputResult{Status: platform.InputPosted}
}

type pageUnknownTestMatcher struct {
	known       PageMatch
	unknown     PageMatch
	unknownFrom uint64
	unknownIDs  map[uint64]bool
	sameKnown   bool
	target      int
}

func (m pageUnknownTestMatcher) Match(f platform.Frame) PageMatch {
	if m.unknownIDs[f.ID] || (m.unknownFrom != 0 && f.ID >= m.unknownFrom) {
		return m.unknown
	}
	p := m.known
	if m.target > 0 && p.List != nil && m.target < len(p.List.Positions) {
		list := *p.List
		list.Selected = m.target
		p.List = &list
		if p.Detail != nil {
			detail := *p.Detail
			detail.Position = PositionVerdict{Position: list.Positions[m.target].Position}
			p.Detail = &detail
		}
	}
	return p
}
func (pageUnknownTestMatcher) MatchScroll(platform.Frame, platform.Frame) ScrollMatch {
	return ScrollMatch{}
}
func (m pageUnknownTestMatcher) CompareDetail(a, b platform.Frame) DetailComparison {
	if m.sameKnown && a.ID != 1 && b.ID != 1 {
		return DetailSame
	}
	return DetailDifferent
}

func pageUnknownTestRun(t *testing.T, frames []platform.Frame, unknownFrom uint64, dir string) *inventoryRun {
	t.Helper()
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
	known := PageMatch{
		Page: PageDriveStorage,
		List: &ListEvidence{Occupied: occupied, Positions: listPositions, Selected: target},
		Detail: &DetailEvidence{
			Quality: QualityS, Substats: 4,
			Position: PositionVerdict{Position: positions[target]},
		},
		Filter:   &FilterEvidence{State: FilterActive},
		Evidence: "known synthetic storage frame",
	}
	matcher := pageUnknownTestMatcher{
		known:       known,
		unknown:     PageMatch{Page: PageUnknown, Evidence: "synthetic page unknown"},
		unknownFrom: unknownFrom,
	}
	return &inventoryRun{
		inv: &Inventory{
			Layout: layout, Matcher: matcher, DiagnosticDir: dir,
			ActionBudget: 500 * time.Millisecond, StableFrames: 3, DetailStableSpan: -1,
		},
		view: &pageUnknownTestView{frames: frames}, geo: 1,
		position: positions[target], viewport: 0,
		filterActive: true, rangeProof: positionRange{Verified: true},
		ledger: newSlotLedgerWithPositions(layout.Columns, occupied, positions, 1),
	}
}

func pageUnknownTestRunUnknownIDs(t *testing.T, frames []platform.Frame, ids map[uint64]bool, dir string) *inventoryRun {
	t.Helper()
	run := pageUnknownTestRun(t, frames, 0, dir)
	matcher := run.inv.Matcher.(pageUnknownTestMatcher)
	matcher.unknownIDs = ids
	run.inv.Matcher = matcher
	return run
}

func pageUnknownTestUseTarget(run *inventoryRun, target int, sameKnown bool) {
	matcher := run.inv.Matcher.(pageUnknownTestMatcher)
	matcher.target = target
	matcher.sameKnown = sameKnown
	run.inv.Matcher = matcher
	run.position = target%6 + 1
}

func pageUnknownTestFrames(t *testing.T) []platform.Frame {
	t.Helper()
	layout := StorageLayout()
	img := pageUnknownTestImage(layout.Content)
	frames := make([]platform.Frame, 0, 3)
	for i := 0; i < 3; i++ {
		frames = append(frames, platform.Frame{
			ID: uint64(i + 2), Geometry: 1, Image: img, Content: layout.Content,
			ArrivedAt: time.Now().Add(time.Duration(i) * time.Millisecond),
		})
	}
	return frames
}

func pageUnknownTestImage(r image.Rectangle) *image.RGBA {
	img := image.NewRGBA(r)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			img.SetRGBA(x, y, color.RGBA{uint8(x), uint8(y), uint8(x ^ y), 255})
		}
	}
	return img
}

func pageUnknownTestBaseline() platform.Frame {
	layout := StorageLayout()
	return platform.Frame{ID: 1, Geometry: 1, Image: pageUnknownTestImage(layout.Content), Content: layout.Content}
}

func pageUnknownTestManifests(t *testing.T, dir string) []pageUnknownTraceManifest {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	manifests := make([]pageUnknownTraceManifest, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name(), "manifest.json"))
		if err != nil {
			t.Fatal(err)
		}
		var manifest pageUnknownTraceManifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			t.Fatal(err)
		}
		manifests = append(manifests, manifest)
	}
	return manifests
}

func TestPageUnknownTraceWritesOriginalRing(t *testing.T) {
	t.Setenv(pageUnknownTraceEnv, "1")
	dir := t.TempDir()
	run := pageUnknownTestRun(t, pageUnknownTestFrames(t), 4, dir)
	tracePageUnknownInput(run, 19, 1, platform.InputResult{
		Status: platform.InputPosted, EventsPlanned: 3, EventsPosted: 3,
		StartedAt: time.Now(), FinishedAt: time.Now(),
	})
	_, err := run.awaitDetailFrames(context.Background(), pageUnknownTestBaseline(),
		&DetailEvidence{Position: PositionVerdict{Position: 2}}, 19, false, nil, true)
	if err == nil {
		t.Fatal("synthetic PageUnknown was accepted")
	}
	entries, readErr := os.ReadDir(dir)
	if readErr != nil || len(entries) != 1 {
		t.Fatalf("diagnostic dirs = %d err=%v, want one", len(entries), readErr)
	}
	sub := filepath.Join(dir, entries[0].Name())
	data, err := os.ReadFile(filepath.Join(sub, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest pageUnknownTraceManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.FailureFrame != 4 || manifest.Page != PageUnknown || manifest.Capture != "original-ring" ||
		manifest.PostCapture != 0 || manifest.Input == nil || manifest.Input.Target != 19 {
		t.Fatalf("manifest = %+v", manifest)
	}
	if len(manifest.Frames) < 2 || manifest.CropCount == 0 {
		t.Fatalf("frames=%d crops=%d", len(manifest.Frames), manifest.CropCount)
	}
	for _, fr := range manifest.Frames {
		if fr.ID == 0 || fr.Evidence == "" {
			t.Fatalf("frame trace incomplete: %+v", fr)
		}
		for _, c := range fr.Crops {
			if c.Rect[2] >= fr.Image[0] && c.Rect[3] >= fr.Image[1] {
				t.Fatalf("crop %s is full frame: %+v", c.Role, c.Rect)
			}
		}
	}
}

func pageUnknownTestFrameWithID(t *testing.T, id uint64) platform.Frame {
	t.Helper()
	layout := StorageLayout()
	return platform.Frame{ID: id, Geometry: 1, Image: pageUnknownTestImage(layout.Content), Content: layout.Content, ArrivedAt: time.Now()}
}

func TestPageUnknownTraceSecondActionStartsWithFreshRing(t *testing.T) {
	t.Setenv(pageUnknownTraceEnv, "1")
	dir := t.TempDir()
	frames := []platform.Frame{
		pageUnknownTestFrameWithID(t, 2), pageUnknownTestFrameWithID(t, 3), pageUnknownTestFrameWithID(t, 4),
		pageUnknownTestFrameWithID(t, 6), pageUnknownTestFrameWithID(t, 7),
	}
	run := pageUnknownTestRunUnknownIDs(t, frames, map[uint64]bool{7: true}, dir)
	run.inv.StableFrames = 3
	pageUnknownTestUseTarget(run, 18, true)

	tracePageUnknownInput(run, 18, 1, platform.InputResult{Status: platform.InputPosted, EventsPlanned: 1, EventsPosted: 1})
	if out, err := run.awaitDetailFrames(context.Background(), pageUnknownTestBaseline(),
		&DetailEvidence{Position: PositionVerdict{Position: 1}}, 18, false, nil, true); err != nil || out.class != awaitStable {
		t.Fatalf("first target action class=%v err=%v", out.class, err)
	}
	pageUnknownTestUseTarget(run, 19, true)
	tracePageUnknownInput(run, 19, 5, platform.InputResult{Status: platform.InputPosted, EventsPlanned: 1, EventsPosted: 1})
	if _, err := run.awaitDetailFrames(context.Background(), pageUnknownTestBaseline(),
		&DetailEvidence{Position: PositionVerdict{Position: 2}}, 19, false, nil, true); err == nil {
		t.Fatal("second target action's PageUnknown was accepted")
	}
	manifests := pageUnknownTestManifests(t, dir)
	if len(manifests) != 1 {
		t.Fatalf("manifests = %d, want one", len(manifests))
	}
	m := manifests[0]
	if m.FailureFrame != 7 || m.Target != 19 || m.Input == nil || m.Input.Target != 19 || m.Input.FrameID != 5 {
		t.Fatalf("manifest leaks another action: %+v", m)
	}
	if len(m.Frames) != 2 || m.Frames[0].ID != 6 || m.Frames[1].ID != 7 {
		t.Fatalf("ring = %+v, want only the second target action's frames 6/7, not prior frame 4", m.Frames)
	}
}

func TestPageUnknownTraceDoubleSelectionCandidatesWithoutList(t *testing.T) {
	l := StorageLayout()
	matcher := NewPixelMatcher(l, StorageCalibration())
	run := &inventoryRun{inv: &Inventory{Layout: l, Matcher: matcher}}
	frame := storageSelectedPatternFrame(l, 1, 0, -1, 300, 323)
	accent := image.NewUniform(color.RGBA{R: 224, G: 208, B: 67, A: 255})
	draw.Draw(frame.Image, l.BorderRect(19), accent, image.Point{}, draw.Src)
	draw.Draw(frame.Image, l.BorderRect(20), accent, image.Point{}, draw.Src)

	match := matcher.Match(frame)
	if match.Page != PageUnknown || !strings.Contains(match.Evidence, "slots 19 and 20") {
		t.Fatalf("double selection verdict = %+v, want PageUnknown naming slots 19/20", match)
	}
	if match.List != nil {
		t.Fatalf("double selection should have no List evidence: %+v", match.List)
	}
	candidates := tracePageUnknownSelectionCandidates(run, frame)
	seen := map[int]pageUnknownTraceCandidate{}
	for _, c := range candidates {
		seen[c.Slot] = c
	}
	for _, slot := range []int{19, 20} {
		c, ok := seen[slot]
		if !ok || c.Reason != "selected-border" || c.Hits < StorageCalibration().SelectionHitMin {
			t.Fatalf("selection candidate %d = %+v (all %+v)", slot, c, candidates)
		}
	}
	crops := tracePageUnknownCrops(run, frame, match, candidates)
	found := 0
	for _, c := range crops {
		if c.Role == "selection-border-19" || c.Role == "selection-border-20" {
			found++
			if c.Reason != "selected-border" || c.Slot != 19 && c.Slot != 20 || c.Hits <= 0 {
				t.Fatalf("candidate crop = %+v", c)
			}
		}
	}
	if found != 2 {
		t.Fatalf("candidate border crops = %d, want 19 and 20", found)
	}
}

func entryNameForTest(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("diagnostic entries = %d err=%v", len(entries), err)
	}
	return entries[0].Name()
}

func TestPageUnknownTraceWrongFrameSizeKeepsOriginalFault(t *testing.T) {
	t.Setenv(pageUnknownTraceEnv, "1")
	dir := t.TempDir()
	small := image.NewRGBA(image.Rect(0, 0, 16, 16))
	frames := []platform.Frame{
		pageUnknownTestFrameWithID(t, 2), pageUnknownTestFrameWithID(t, 3),
		{ID: 4, Geometry: 1, Image: small, Content: small.Bounds(), ArrivedAt: time.Now()},
	}
	run := pageUnknownTestRunUnknownIDs(t, frames, map[uint64]bool{4: true}, dir)
	tracePageUnknownInput(run, 19, 1, platform.InputResult{Status: platform.InputPosted})
	_, err := run.awaitDetailFrames(context.Background(), pageUnknownTestBaseline(),
		&DetailEvidence{Position: PositionVerdict{Position: 2}}, 19, false, nil, true)
	if err == nil {
		t.Fatal("wrong-size PageUnknown was accepted")
	}
	manifests := pageUnknownTestManifests(t, dir)
	if len(manifests) != 1 {
		t.Fatalf("manifests = %d, want one", len(manifests))
	}
	m := manifests[0]
	if m.FailureFrame != 4 || m.Error != err.Error() {
		t.Fatalf("wrong-size manifest = %+v, original error = %v", m, err)
	}
	for _, fr := range m.Frames {
		if fr.ID == 4 && fr.Image != [2]int{16, 16} {
			t.Fatalf("wrong-size frame dimensions = %v, want 16x16", fr.Image)
		}
		if fr.ID == 4 {
			for _, c := range fr.Crops {
				if c.Role == "selection-border-20" {
					t.Fatalf("small frame unexpectedly produced a border crop: %+v", c)
				}
			}
		}
	}
}

func TestPageUnknownTraceCopyHonorsNonZeroBounds(t *testing.T) {
	src := image.NewRGBA(image.Rect(10, 20, 30, 40))
	for y := 20; y < 40; y++ {
		for x := 10; x < 30; x++ {
			src.SetRGBA(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: uint8(x ^ y), A: 255})
		}
	}
	for _, r := range []image.Rectangle{
		image.Rect(10, 20, 30, 40),
		image.Rect(15, 25, 25, 35),
	} {
		crop := tracePageUnknownCopy(src, r)
		if crop == nil || crop.Bounds() != image.Rect(0, 0, r.Dx(), r.Dy()) {
			t.Fatalf("crop bounds for %v = %v", r, crop)
		}
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				if got, want := crop.RGBAAt(x-r.Min.X, y-r.Min.Y), src.RGBAAt(x, y); got != want {
					t.Fatalf("crop pixel (%d,%d) = %v, want %v", x, y, got, want)
				}
			}
		}
	}
}

func TestPageUnknownTraceSelectionROIBoundsAreChecked(t *testing.T) {
	l := StorageLayout()
	frame := platform.Frame{ID: 1, Geometry: 1, Image: image.NewRGBA(image.Rect(0, 0, 16, 16))}
	run := &inventoryRun{inv: &Inventory{Layout: l, Matcher: NewPixelMatcher(l, StorageCalibration())}}
	candidates := tracePageUnknownSelectionCandidates(run, frame)
	if len(candidates) != len(l.Slots) {
		t.Fatalf("candidates = %d, want every slot marked unavailable", len(candidates))
	}
	for _, c := range candidates {
		if c.Reason != "border-roi-unavailable" {
			t.Fatalf("candidate outside bounds = %+v", c)
		}
	}
	_ = tracePageUnknownCrops(run, frame, PageMatch{Page: PageUnknown}, candidates)
}

func TestPageUnknownTraceClearMaxIsIncluded(t *testing.T) {
	t.Setenv(pageUnknownTraceEnv, "1")
	l := StorageLayout()
	matcher := NewPixelMatcher(l, StorageCalibration())
	frame := storageSelectedPatternFrame(l, 1, 0, -1, 300, 323)
	accent := color.RGBA{R: 224, G: 208, B: 67, A: 255}
	r := l.BorderRect(20)
	painted := 0
	for y := r.Min.Y; y < r.Max.Y && painted < matcher.C.SelectionClearMax; y++ {
		for x := r.Min.X; x < r.Max.X && painted < matcher.C.SelectionClearMax; x++ {
			frame.Image.SetRGBA(x, y, accent)
			painted++
		}
	}
	match := matcher.Match(frame)
	if match.Page != PageUnknown || match.List != nil {
		t.Fatalf("clear-boundary frame = %+v, want PageUnknown List=nil", match)
	}
	dir := t.TempDir()
	run := &inventoryRun{inv: &Inventory{Layout: l, Matcher: matcher, DiagnosticDir: dir}}
	tracePageUnknownInput(run, 19, 1, platform.InputResult{Status: platform.InputPosted})
	tracePageUnknownObserve(run, frame, match, 19, detailOutcome{})
	tracePageUnknownReject(run, frame, match, 19, errors.New("synthetic terminal"))
	manifests := pageUnknownTestManifests(t, dir)
	if len(manifests) != 1 || len(manifests[0].Frames) == 0 {
		t.Fatalf("clear-boundary manifest = %+v", manifests)
	}
	var found *pageUnknownTraceCrop
	for i := range manifests[0].Frames[0].Crops {
		c := &manifests[0].Frames[0].Crops[i]
		if c.Role == "selection-border-20" {
			found = c
			break
		}
	}
	if found == nil || found.Hits != matcher.C.SelectionClearMax || found.Reason != "border-between-tiers" {
		t.Fatalf("clear-boundary candidate = %+v, want hits=%d between-tier crop", found, matcher.C.SelectionClearMax)
	}
	if found.File == "" {
		t.Fatal("clear-boundary candidate has no written PNG")
	}
	if _, err := os.Stat(filepath.Join(dir, entryNameForTest(t, dir), found.File)); err != nil {
		t.Fatalf("clear-boundary PNG missing: %v", err)
	}
}

func TestPageUnknownTraceDisabledDoesNotWrite(t *testing.T) {
	t.Setenv(pageUnknownTraceEnv, "")
	dir := t.TempDir()
	run := pageUnknownTestRun(t, pageUnknownTestFrames(t), 4, dir)
	if _, err := run.awaitDetailFrames(context.Background(), pageUnknownTestBaseline(),
		&DetailEvidence{Position: PositionVerdict{Position: 2}}, 19, false, nil, true); err == nil {
		t.Fatal("synthetic PageUnknown was accepted")
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Fatalf("disabled trace wrote %d entries err=%v", len(entries), err)
	}
}

func TestPageUnknownTraceFirstUnknownRecoversThenTerminalWritesOnce(t *testing.T) {
	t.Setenv(pageUnknownTraceEnv, "1")
	dir := t.TempDir()
	run := pageUnknownTestRunUnknownIDs(t, pageUnknownTestFrames(t), map[uint64]bool{2: true, 4: true}, dir)
	tracePageUnknownInput(run, 19, 1, platform.InputResult{
		Status: platform.InputPosted, EventsPlanned: 3, EventsPosted: 3,
		StartedAt: time.Now(), FinishedAt: time.Now(),
	})
	if _, err := run.awaitDetailFrames(context.Background(), pageUnknownTestBaseline(),
		&DetailEvidence{Position: PositionVerdict{Position: 2}}, 19, false, nil, true); err == nil {
		t.Fatal("terminal synthetic PageUnknown was accepted")
	}
	manifests := pageUnknownTestManifests(t, dir)
	if len(manifests) != 1 {
		t.Fatalf("manifests = %d, want one terminal write", len(manifests))
	}
	m := manifests[0]
	if m.FailureFrame != 4 || m.Cause != "storageBadgeDisposition-page-unknown" ||
		m.Error == "" || m.Input == nil || m.Input.Target != 19 || m.PostCapture != 0 {
		t.Fatalf("manifest = %+v", m)
	}
	if m.Input.StartedAt.IsZero() || m.Input.FinishedAt.IsZero() ||
		m.Frames[0].ArrivedAt.IsZero() || m.Frames[1].ArrivedAt.IsZero() || m.Frames[2].ArrivedAt.IsZero() {
		t.Fatalf("missing frame/input times: %+v", m)
	}
	if len(m.Frames) != 3 || m.Frames[0].ID != 2 || m.Frames[1].ID != 3 || m.Frames[2].ID != 4 {
		t.Fatalf("ring frames = %+v", m.Frames)
	}
	if m.Frames[0].Page != PageUnknown || m.Frames[1].Page != PageDriveStorage || m.Frames[2].Page != PageUnknown {
		t.Fatalf("ring pages = %s/%s/%s", m.Frames[0].Page, m.Frames[1].Page, m.Frames[2].Page)
	}
}

func TestPageUnknownTraceFirstUnknownRecoversAndDoesNotWrite(t *testing.T) {
	t.Setenv(pageUnknownTraceEnv, "1")
	dir := t.TempDir()
	run := pageUnknownTestRunUnknownIDs(t, pageUnknownTestFrames(t), map[uint64]bool{2: true}, dir)
	run.inv.StableFrames = 2
	matcher := run.inv.Matcher.(pageUnknownTestMatcher)
	matcher.sameKnown = true
	run.inv.Matcher = matcher
	tracePageUnknownInput(run, 19, 1, platform.InputResult{Status: platform.InputPosted, EventsPlanned: 1, EventsPosted: 1})
	out, err := run.awaitDetailFrames(context.Background(), pageUnknownTestBaseline(),
		&DetailEvidence{Position: PositionVerdict{Position: 2}}, 19, false, nil, true)
	if err != nil || out.class != awaitStable {
		t.Fatalf("recovered wait class=%v err=%v frame=%d page=%s saw=%v sawDetail=%v selected=%d", out.class, err, out.frame.ID, out.match.Page, out.sawTarget, out.sawTargetDetail, out.lastSelected)
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Fatalf("recovered trace wrote %d entries err=%v", len(entries), err)
	}
}

func TestPageUnknownTraceKnownFramesDoNotWrite(t *testing.T) {
	t.Setenv(pageUnknownTraceEnv, "1")
	dir := t.TempDir()
	run := pageUnknownTestRun(t, pageUnknownTestFrames(t), 0, dir)
	if _, err := run.awaitDetailFrames(context.Background(), pageUnknownTestBaseline(),
		&DetailEvidence{Position: PositionVerdict{Position: 2}}, 19, false, nil, true); err != nil {
		t.Fatalf("known frames returned error: %v", err)
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Fatalf("known frames wrote %d entries err=%v", len(entries), err)
	}
}

func TestPageUnknownTraceRetainsBadgeWindowsAndThumbFromOriginalFrame(t *testing.T) {
	t.Setenv(pageUnknownTraceEnv, "1")
	l := StorageLayout()
	f := storageSelectedPatternFrame(l, 19, 0, 27, 300, 323)
	matcher := NewPixelMatcher(l, StorageCalibration())
	run := &inventoryRun{inv: &Inventory{Layout: l, Matcher: matcher, DiagnosticDir: t.TempDir()}}
	tracePageUnknownObserve(run, f, matcher.Match(f), 27, detailOutcome{
		lastVerifiedFrame: platform.Frame{ID: 17}, sawTarget: true,
	})
	entry := run.pageUnknownTrace.frames[0]
	if entry.AnchorFrame != 17 || !entry.SawTarget || entry.SawTargetDetail {
		t.Fatalf("unexpected pre-frame wait state: anchor=%d strong=%v detail=%v", entry.AnchorFrame, entry.SawTarget, entry.SawTargetDetail)
	}
	badges := make(map[int]bool)
	hasThumb := false
	for _, c := range entry.Crops {
		switch {
		case strings.HasPrefix(c.Role, "card-position-"):
			if c.Rect != pageUnknownRect(l.CardPositionRect(c.Slot)) || badges[c.Slot] {
				t.Fatalf("invalid or duplicate card badge crop: %+v", c)
			}
			badges[c.Slot] = true
		case c.Role == "scroll-thumb-column":
			if c.Rect != pageUnknownRect(l.ScrollTrack) || hasThumb {
				t.Fatalf("invalid or duplicate thumb crop: %+v", c)
			}
			hasThumb = true
		}
	}
	if len(badges) != len(l.Slots) || !hasThumb {
		t.Fatalf("badge crops=%d thumb=%v", len(badges), hasThumb)
	}
}
