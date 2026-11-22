package collect_test

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/LoTwT/youkai/internal/game/zzz/collect"
	"github.com/LoTwT/youkai/internal/platform"
)

// TestStoragePrivateHandoff replays Jane's 2026-11-22 private 1920×1080
// drive-storage handoff through the calibrated matcher. The samples stay
// outside the repository; set YOUKAI_STORAGE_SAMPLES to the extracted
// handoff-1080p directory. It is a calibration/replay check, not a
// committed fixture and not a substitute for the field acceptance.
func TestStoragePrivateHandoff(t *testing.T) {
	dir := os.Getenv("YOUKAI_STORAGE_SAMPLES")
	if dir == "" {
		t.Skip("YOUKAI_STORAGE_SAMPLES is not set; private drive-storage handoff not available")
	}
	layout := collect.StorageLayout()
	pixel := collect.NewPixelMatcher(layout, collect.StorageCalibration())
	matcher := collect.Matcher(collect.NewScrollEvidenceMatcher(pixel))
	base := mustStorageFrame(t, filepath.Join(dir, "final-state.png"), 55, 105)
	overlay := func(path string, at image.Point) *image.RGBA {
		frame := image.NewRGBA(base.Bounds())
		draw.Draw(frame, frame.Bounds(), base, image.Point{}, draw.Src)
		mustDraw(t, frame, filepath.Join(dir, path), at)
		return frame
	}
	frame := func(frame *image.RGBA) platform.Frame {
		return platform.Frame{ID: 1, Image: frame, Content: image.Rect(0, 0, 1920, 1080)}
	}

	final := frame(overlay("final-state.png", image.Pt(55, 105)))
	m := matcher.Match(final)
	if m.Page != collect.PageDriveStorage || m.List == nil {
		t.Fatalf("final state page = %s (%s), want drive storage", m.Page, m.Evidence)
	}
	if m.List.Selected != 0 {
		t.Fatalf("final state selected = %d, want first card 0", m.List.Selected)
	}
	if m.List.Management[0] != collect.ManagementLocked || m.List.TopManagement != collect.TopManagementLocked {
		t.Fatalf("final state management = %q/%q, want locked/locked", m.List.Management[0], m.List.TopManagement)
	}
	if got := m.List.Positions[0].Position; got != 3 {
		t.Fatalf("first card slot = %d, want 3", got)
	}
	if got := m.Detail.Position.Position; got != 3 {
		t.Fatalf("detail slot = %d, want 3", got)
	}

	top := frame(overlay("scroll-top.png", image.Pt(55, 184)))
	tm := matcher.Match(top)
	if tm.Page != collect.PageDriveStorage || tm.List == nil {
		t.Fatalf("scroll-top page = %s (%s), want drive storage", tm.Page, tm.Evidence)
	}
	if tm.List.Selected != 0 || tm.List.Positions[0].Position != 3 {
		t.Fatalf("scroll-top selected %d slot %d, want 0/3 (%s)", tm.List.Selected, tm.List.Positions[0].Position, tm.Evidence)
	}
	for i, p := range tm.List.Positions {
		if p.Position == 0 || p.Conflict {
			t.Fatalf("scroll-top card %d has no unique slot: %+v", i, p)
		}
	}
	oneRow := frame(overlay("scroll-one-row.png", image.Pt(55, 184)))
	om := matcher.Match(oneRow)
	if om.Page != collect.PageDriveStorage || om.List.Selected != -1 {
		t.Fatalf("one-row selection = %d, want none (the selected card left the viewport)", om.List.Selected)
	}
	for i, p := range om.List.Positions {
		if p.Position == 0 || p.Conflict {
			t.Fatalf("one-row card %d has no unique slot: %+v", i, p)
		}
	}
	if sm := matcher.MatchScroll(top, oneRow); !sm.Displaced || sm.Rows != 1 || len(sm.Matches) == 0 {
		t.Fatalf("one-row scroll = displaced %t rows %d matches %d (%s), want one row", sm.Displaced, sm.Rows, len(sm.Matches), sm.Evidence)
	}
	// With the badge windows obscured, the repeated-grid feature must not
	// silently choose a row after the storage-specific witness disappears.
	obscured := image.NewRGBA(oneRow.Image.Bounds())
	draw.Draw(obscured, obscured.Bounds(), oneRow.Image, image.Point{}, draw.Src)
	for i := range layout.Slots {
		draw.Draw(obscured, layout.CardPositionRect(i), image.NewUniform(color.RGBA{A: 255}), image.Point{}, draw.Src)
	}
	if sm := matcher.MatchScroll(top, frame(obscured)); sm.Displaced || !sm.Undecided {
		t.Fatalf("badge-less scroll = displaced %t undecided %t rows %d (%s), want fail-closed",
			sm.Displaced, sm.Undecided, sm.Rows, sm.Evidence)
	}
	bottom := frame(overlay("scroll-bottom.png", image.Pt(55, 184)))
	bm := matcher.Match(bottom)
	if bm.Page != collect.PageDriveStorage || bm.List == nil || !bm.List.Bottom {
		t.Fatalf("bottom page = %s bottom %v, want drive storage with bottom evidence", bm.Page, bm.List != nil && bm.List.Bottom)
	}
	// The bottom viewport still shows the three full rows above the final
	// one; only the last row has the handoff's single card plus eight empty
	// cells.
	lastRowOccupied := 0
	for i := 3 * 9; i < len(bm.List.Occupied); i++ {
		if bm.List.Occupied[i] {
			lastRowOccupied++
		}
	}
	if lastRowOccupied != 1 {
		t.Fatalf("bottom row occupied = %d, want the handoff's single last card", lastRowOccupied)
	}
	if bm.List.Positions[3*9].Position != 6 {
		t.Fatalf("last card slot = %d, want 6", bm.List.Positions[3*9].Position)
	}
	recheck := frame(overlay("scroll-bottom-recheck.png", image.Pt(55, 184)))
	if sm := matcher.MatchScroll(bottom, recheck); !sm.Stationary {
		t.Fatalf("bottom recheck = displaced %t stationary %t (%s), want stationary", sm.Displaced, sm.Stationary, sm.Evidence)
	}

	// Detail-side slots from the six named crops. The base list/detail
	// background is irrelevant; the calibrated detail regions come from the
	// crop.
	for position, name := range map[int]string{
		1: "slot-1-detail.png", 2: "slot-2-detail.png", 3: "slot-3-detail.png",
		4: "slot-4-detail.png", 5: "slot-5-detail.png", 6: "slot-6-detail.png",
	} {
		f := frame(overlay(name, image.Pt(1401, 255)))
		m := matcher.Match(f)
		if m.Detail == nil || m.Detail.Position.Position != position {
			t.Fatalf("%s detail slot = %+v, want %d (%s)", name, m.Detail, position, m.Evidence)
		}
	}

	panel := func(name string, funnelName string) platform.Frame {
		f := overlay(name, image.Pt(1236, 100))
		mustDraw(t, f, filepath.Join(dir, funnelName), image.Pt(125, 900))
		return frame(f)
	}
	open := matcher.Match(panel("panel-open.png", "funnel-open.png"))
	if open.Page != collect.PageFilterPanel || open.Panel == nil || !open.Panel.SortVerified {
		t.Fatalf("panel-open page = %s sort %v (%s), want verified filter panel", open.Page, open.Panel != nil && open.Panel.SortVerified, open.Evidence)
	}
	reset := matcher.Match(panel("panel-reset.png", "funnel-reset.png"))
	if reset.Page != collect.PageFilterPanel || reset.Panel == nil || reset.Panel.Range == nil ||
		!reset.Panel.Range.ResetVerified || reset.Panel.Range.RarityS != collect.FilterClear {
		t.Fatalf("panel-reset range = %+v (%s), want reset verified and S clear", reset.Panel, reset.Evidence)
	}
	onlyS := matcher.Match(panel("panel-only-s.png", "funnel-only-s.png"))
	if onlyS.Page != collect.PageFilterPanel || onlyS.Panel == nil || onlyS.Panel.Range == nil ||
		onlyS.Panel.Range.RarityS != collect.FilterActive {
		t.Fatalf("panel-only-s range = %+v (%s), want S active", onlyS.Panel, onlyS.Evidence)
	}
	rarity := matcher.Match(panel("panel-only-s-rarity.png", "funnel-only-s-rarity.png"))
	if rarity.Page != collect.PageFilterPanel || rarity.Panel == nil || !rarity.Panel.SortVerified ||
		rarity.Panel.Range == nil || rarity.Panel.Range.RarityS != collect.FilterActive {
		t.Fatalf("panel-s-rarity = %+v (%s), want verified rarity and S active", rarity.Panel, rarity.Evidence)
	}
}

// TestStoragePrivateSelectionInsideOverlap edits only the three selection
// probes on the private one-row pair. The selected card moves in the same
// direction as the unchanged list and scrollbar evidence; the repeated-grid
// zero-shift correlation must not override that checked displacement.
func TestStoragePrivateSelectionInsideOverlap(t *testing.T) {
	dir := os.Getenv("YOUKAI_STORAGE_SAMPLES")
	if dir == "" {
		t.Skip("YOUKAI_STORAGE_SAMPLES is not set; private drive-storage handoff not available")
	}
	layout := collect.StorageLayout()
	m := collect.NewScrollEvidenceMatcher(collect.NewPixelMatcher(layout, collect.StorageCalibration()))
	frame := func(name string, id uint64) platform.Frame {
		f := mustStorageFrame(t, filepath.Join(dir, "final-state.png"), 55, 105)
		mustDraw(t, f, filepath.Join(dir, name), image.Pt(55, 184))
		return platform.Frame{ID: id, Geometry: 1, Image: f, Content: layout.Content}
	}
	before := frame("scroll-top.png", 1)
	after := frame("scroll-one-row.png", 2)
	if s := m.MatchScroll(before, after); !s.Displaced || s.Rows != 1 {
		t.Fatalf("original sample control: %s", s.Evidence)
	}
	yellow := image.NewUniform(color.RGBA{R: 224, G: 208, B: 67, A: 255})
	dim := image.NewUniform(color.RGBA{R: 16, G: 16, B: 16, A: 255})
	draw.Draw(before.Image, layout.BorderRect(0), dim, image.Point{}, draw.Src)
	draw.Draw(before.Image, layout.BorderRect(35), yellow, image.Point{}, draw.Src)
	draw.Draw(after.Image, layout.BorderRect(26), yellow, image.Point{}, draw.Src)
	if b, a := m.Match(before), m.Match(after); b.List == nil || a.List == nil ||
		b.List.Selected != 35 || a.List.Selected != 26 {
		t.Fatalf("edited selection probes did not read as 35 -> 26: %s; %s", b.Evidence, a.Evidence)
	}
	if s := m.MatchScroll(before, after); !s.Displaced || s.Rows != 1 {
		t.Fatalf("in-view selection agrees with the same one-row movement but rejects it: %s", s.Evidence)
	}
}

func mustStorageFrame(t *testing.T, path string, x, y int) *image.RGBA {
	t.Helper()
	frame := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	draw.Draw(frame, frame.Bounds(), image.NewUniform(color.RGBA{0, 0, 0, 255}), image.Point{}, draw.Src)
	mustDraw(t, frame, path, image.Pt(x, y))
	return frame
}

func mustDraw(t *testing.T, dst *image.RGBA, path string, at image.Point) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	draw.Draw(dst, img.Bounds().Add(at), img, image.Point{}, draw.Src)
}
