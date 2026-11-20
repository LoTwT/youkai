package collect

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/platform"
	"github.com/LoTwT/youkai/internal/scan"
)

// The traversal-level repeated-row regression: the Windows 1920×1080
// frames go through the real stage 2 scroll matcher and the real scroll()
// state machine. A one-row move of a repeated-row list changes only the
// selection border and the thumb, so the old MAD shortcut judged it
// Stationary, retried the drag, and carried the visit bookkeeping over an
// unaccounted row (nextSlot resumed past the skipped row). These tests pin
// that the committed advance matches the scene and that an unverifiable
// move stops the position without touching the bookkeeping.

// repeatedRowsMatcher keeps the real scroll judgment but answers the page
// verdict as a stable warehouse list: the synthetic frames paint only the
// list viewport, the border probes and the track, which is all MatchScroll
// reads.
type repeatedRowsMatcher struct {
	*ScrollEvidenceMatcher
	rows, columns int
}

func (m *repeatedRowsMatcher) Match(f platform.Frame) PageMatch {
	occupied := make([]bool, m.rows*m.columns)
	for i := range occupied {
		occupied[i] = true
	}
	top, bottom := m.scrollEnds(f.Image)
	return PageMatch{
		Page:   PageWarehouse,
		List:   &ListEvidence{Occupied: occupied, Selected: 0, Top: top, Bottom: bottom},
		Wheel:  &WheelEvidence{Position: 1},
		Filter: &FilterEvidence{State: FilterActive},
	}
}

// repeatedRowsThumb returns the thumb segment for a list of total rows with
// offset rows above the viewport, matching the geometry thumbTravelOf
// derives from the observed segment.
func repeatedRowsThumb(l Layout, offset, total int) (start, end int) {
	trackLen := l.ScrollTrack.Dy()
	thumbLen := int(math.Round(float64(trackLen) * float64(l.Rows) / float64(total)))
	if thumbLen < 1 {
		thumbLen = 1
	}
	perRow := float64(trackLen-thumbLen) / float64(total-l.Rows)
	start = l.ScrollTrack.Min.Y + int(math.Round(perRow*float64(offset)))
	return start, start + thumbLen - 1
}

// paintRepeatedRowsFrame paints a list whose rows are pixel-identical: the
// only position evidence is the selection border slot and the thumb.
func paintRepeatedRowsFrame(l Layout, id uint64, offset, selection, thumbStart, thumbEnd int) platform.Frame {
	img := image.NewRGBA(image.Rectangle{Max: l.Image})
	for y := l.ListViewport.Min.Y; y < l.ListViewport.Max.Y; y++ {
		yy := (y - l.ListViewport.Min.Y) % l.RowPitch
		for x := l.ListViewport.Min.X; x < l.ListViewport.Max.X; x++ {
			h := uint32(x*97 + yy*1000003)
			h ^= h >> 16
			h *= 0x45d9f3b
			h ^= h >> 16
			v := int(40 + h%160)
			if yy >= 24 && yy < 36 {
				v += 12
			}
			if yy%4 == 0 {
				v += 45
			}
			if v > 255 {
				v = 255
			}
			i := img.PixOffset(x, y)
			img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = uint8(v), uint8(v), uint8(v), 255
		}
	}
	if selection >= 0 {
		r := l.BorderRect(selection).Intersect(img.Bounds())
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				img.SetRGBA(x, y, color.RGBA{R: 224, G: 208, B: 67, A: 255})
			}
		}
	}
	for y := thumbStart; y <= thumbEnd; y++ {
		if y >= l.ScrollTrack.Min.Y && y < l.ScrollTrack.Max.Y {
			img.SetRGBA(l.ScrollTrack.Min.X, y, color.RGBA{R: 200, G: 200, B: 200, A: 255})
		}
	}
	return platform.Frame{ID: id, Geometry: 1, Image: img, Content: l.Content}
}

// repeatedRowsView advances one scene row per dispatched drag and serves the
// current scene on every frame request with a fresh ID. It blocks after
// maxFrames so a wait that expects more observations ends on its budget
// instead of spinning over an endless identical stream.
type repeatedRowsView struct {
	layout        Layout
	offset        int
	initialOffset int
	selectedRow   int
	totalRows     int
	thumbMoves    bool
	maxFrames     int
	served        int
	nextID        uint64
	dispatches    int
}

func (v *repeatedRowsView) Observe() (platform.Observation, error) {
	return platform.Observation{Ready: true, Geometry: 1}, nil
}

func (v *repeatedRowsView) Dispatch(platform.InputOp) platform.InputResult {
	v.dispatches++
	v.offset++
	return platform.InputResult{Status: platform.InputPosted}
}

func (v *repeatedRowsView) Frame(ctx context.Context, req platform.FrameRequest) (platform.Frame, error) {
	if err := ctx.Err(); err != nil {
		return platform.Frame{}, err
	}
	if v.served >= v.maxFrames {
		<-ctx.Done()
		return platform.Frame{}, ctx.Err()
	}
	v.served++
	thumbOffset := v.offset
	if !v.thumbMoves {
		thumbOffset = v.initialOffset
	}
	thumbStart, thumbEnd := repeatedRowsThumb(v.layout, thumbOffset, v.totalRows)
	selection := -1
	if row := v.selectedRow - v.offset; row >= 0 && row < v.layout.Rows {
		selection = row * v.layout.Columns
	}
	v.nextID++
	f := paintRepeatedRowsFrame(v.layout, v.nextID, v.offset, selection, thumbStart, thumbEnd)
	f.ArrivedAt = time.Now()
	return f, nil
}

// newRepeatedRowsRun builds the Windows-profile run over the real stage 2
// matcher and the scripted views above. The scroll budget must cover the
// normalized-feature search over a full 1920×1080 viewport, which is far
// slower than the scripted verifiers the other tests use.
func newRepeatedRowsRun(t *testing.T, view SessionView, retries int, budget time.Duration) *inventoryRun {
	t.Helper()
	profile, err := WindowsLocalProfileFor(image.Pt(1920, 1080))
	if err != nil {
		t.Fatalf("WindowsLocalProfileFor: %v", err)
	}
	pixel := NewPixelMatcher(profile.Layout, profile.Calibration)
	matcher := &repeatedRowsMatcher{
		ScrollEvidenceMatcher: NewScrollEvidenceMatcher(pixel),
		rows:                  profile.Layout.Rows,
		columns:               profile.Layout.Columns,
	}
	run := newScriptedScrollRun(profile.Layout, view, matcher, "", retries, budget)
	for i := range run.ledger.cells {
		run.ledger.cells[i].State = slotAdmitted
	}
	return run
}

// TestScrollRepeatedRowsOneRowAdvanceCommitsOnce pins the ordinary case: the
// one-row move of a repeated-row list is verified from the selection box and
// the thumb, committed exactly once, and the visit bookkeeping advances by
// the same one row the scene moved. No retry drag is sent, so the traversal
// cannot skip the newly exposed row.
func TestScrollRepeatedRowsOneRowAdvanceCommitsOnce(t *testing.T) {
	view := &repeatedRowsView{
		offset:        3,
		initialOffset: 3,
		selectedRow:   7,
		totalRows:     30,
		thumbMoves:    true,
		maxFrames:     4,
	}
	run := newRepeatedRowsRun(t, view, 1, 5*time.Second)
	view.layout = run.inv.Layout

	if err := runScriptedScroll(t, run); err != nil {
		t.Fatalf("the verified one-row advance must commit: %v", err)
	}
	if view.dispatches != 1 {
		t.Fatalf("scroll dispatched %d drags, want the single verified drag", view.dispatches)
	}
	if view.offset != 4 || run.viewport != 4 || run.lastScrollRows != 1 {
		t.Fatalf("scene offset %d, viewport %d, rows %d: want the committed advance to match the scene (4/4/1)",
			view.offset, run.viewport, run.lastScrollRows)
	}
	// The viewport was exhausted before the advance (all 20 slots visited).
	// After one committed row the freshly exposed bottom row (slots 16–19)
	// must be the next visit; the rows above it inherit their visit state.
	if next := run.nextSlot(); next != 16 {
		t.Fatalf("nextSlot = %d after one committed row, want the newly exposed row 16", next)
	}
	if cells := run.ledger.visible(); cells[12].State != slotAdmitted || cells[16].State != slotUnresolved {
		t.Fatalf("row 3=%+v row 4=%+v, want the inherited row admitted and the new row unresolved", cells[12], cells[16])
	}
}

// TestScrollRepeatedRowsUnexplainedMoveFailsClosed pins the negative side:
// the selection box moved one row but the thumb did not, so the displacement
// cannot be verified (the repeated rows give the features no unique row).
// The drag's effect stays unknown: the position fails closed and neither
// viewport nor visited bookkeeping may advance, even though the scene moved.
func TestScrollRepeatedRowsUnexplainedMoveFailsClosed(t *testing.T) {
	view := &repeatedRowsView{
		offset:        3,
		initialOffset: 3,
		selectedRow:   7,
		totalRows:     30,
		thumbMoves:    false,
		maxFrames:     2,
	}
	run := newRepeatedRowsRun(t, view, 1, 80*time.Millisecond)
	view.layout = run.inv.Layout
	ledgerBefore := slices.Clone(run.ledger.cells)

	err := runScriptedScroll(t, run)
	var incomplete *positionIncompleteError
	if !errors.As(err, &incomplete) || incomplete.stage != "scroll" {
		t.Fatalf("scroll error %v, want a position-scoped scroll failure", err)
	}
	var fault *scan.FaultError
	if !errors.As(err, &fault) || fault.Code != CodeNavigationMismatch {
		t.Fatalf("scroll error %v, want navigationMismatch", err)
	}
	if view.dispatches != 1 {
		t.Fatalf("scroll dispatched %d drags, want no retry after an unverifiable move", view.dispatches)
	}
	if run.viewport != 3 || run.lastScrollRows != 0 {
		t.Fatalf("viewport %d rows %d after an unverifiable move, want the bookkeeping unchanged (3/0)",
			run.viewport, run.lastScrollRows)
	}
	if run.ledger.rowBase != 0 || !slices.Equal(run.ledger.cells, ledgerBefore) {
		t.Fatalf("ledger changed after an unverifiable move: %+v", run.ledger)
	}
}

// unprovenRebaseMatcher forces the await verdict to Stationary while denying
// every zero-displacement proof, isolating the retry gate from the matcher
// fix: even a wait that claims stillness may not rebase on an unproven
// frame.
type unprovenRebaseMatcher struct {
	*scrollScriptMatcher
}

func (m *unprovenRebaseMatcher) zeroDisplacementProven(before, after platform.Frame) (bool, string) {
	return false, fmt.Sprintf("scripted: frame %d → %d is not proven at the old position", before.ID, after.ID)
}

// TestScrollRetryRefusesUnprovenRebase pins the retry baseline gate: a wait
// that only ever proved stillness may retry, but only when the newest frame
// is proven to still sit at the old position. Here the follow-up frame
// cannot be proven, so no second drag is sent and the old visit bookkeeping
// is not carried over a possibly moved baseline.
func TestScrollRetryRefusesUnprovenRebase(t *testing.T) {
	l := sixRowScrollLayout()
	view := &scriptedBlockView{
		frames:  []platform.Frame{scrollFrame(1), scrollFrame(2), scrollFrame(3)},
		blockAt: []int{3},
	}
	scripted := &scrollScriptMatcher{rows: l.Rows, columns: l.Columns, verdict: map[scrollPair]ScrollMatch{
		{1, 2}: {Stationary: true, Evidence: "scripted stationary"},
	}}
	m := &unprovenRebaseMatcher{scrollScriptMatcher: scripted}
	run := newScriptedScrollRun(l, view, m, "", 1, 40*time.Millisecond)

	err := runScriptedScroll(t, run)
	var incomplete *positionIncompleteError
	if !errors.As(err, &incomplete) || incomplete.stage != "scroll" {
		t.Fatalf("scroll error %v, want a position-scoped scroll failure", err)
	}
	if !strings.Contains(err.Error(), "retry baseline") || !strings.Contains(err.Error(), "not proven to be the same position") {
		t.Fatalf("scroll error %q should name the unproven retry baseline", err)
	}
	if view.dispatches != 1 {
		t.Fatalf("scroll dispatched %d drags, want only the first attempt", view.dispatches)
	}
	if run.viewport != 3 {
		t.Fatalf("viewport %d after the refused rebase, want 3", run.viewport)
	}
}

// TestScrollEvidenceZeroDisplacementProof pins the proof the retry gate
// consumes: the same repeated-row scene at a new frame ID is proven by the
// selection box staying on one slot, while the one-row move whose selection
// and thumb moved is not proven.
func TestScrollEvidenceZeroDisplacementProof(t *testing.T) {
	profile, err := WindowsLocalProfileFor(image.Pt(1920, 1080))
	if err != nil {
		t.Fatalf("WindowsLocalProfileFor: %v", err)
	}
	m := NewScrollEvidenceMatcher(NewPixelMatcher(profile.Layout, profile.Calibration))
	l := profile.Layout
	stillStart, stillEnd := repeatedRowsThumb(l, 3, 30)
	movedStart, movedEnd := repeatedRowsThumb(l, 4, 30)
	before := paintRepeatedRowsFrame(l, 1, 3, 16, stillStart, stillEnd)
	still := paintRepeatedRowsFrame(l, 2, 3, 16, stillStart, stillEnd)
	moved := paintRepeatedRowsFrame(l, 3, 4, 12, movedStart, movedEnd)

	if proven, why := m.zeroDisplacementProven(before, still); !proven {
		t.Fatalf("the identical scene is not proven still: %s", why)
	}
	if proven, why := m.zeroDisplacementProven(before, moved); proven {
		t.Fatalf("the one-row move is claimed as proven stillness: %s", why)
	}
}

// TestScrollEvidenceZeroDisplacementProofNeedsAWitness pins the first review
// finding: with no readable selection box and no readable thumb, a repeated
// row list at two different offsets is pixel-identical, so the zero-shift MAD
// is exactly zero. A zero MAD is not proof — the implementation must not
// accept the rebase that would carry the old visit bookkeeping over the hidden
// one-row move. A readable same-slot selection box is the witness that
// distinguishes the same scene at one position, even without a thumb.
func TestScrollEvidenceZeroDisplacementProofNeedsAWitness(t *testing.T) {
	profile, err := WindowsLocalProfileFor(image.Pt(1920, 1080))
	if err != nil {
		t.Fatalf("WindowsLocalProfileFor: %v", err)
	}
	m := NewScrollEvidenceMatcher(NewPixelMatcher(profile.Layout, profile.Calibration))
	l := profile.Layout

	before := paintRepeatedRowsFrame(l, 1, 3, -1, 0, -1)
	moved := paintRepeatedRowsFrame(l, 2, 4, -1, 0, -1)
	sm := m.MatchScroll(before, moved)
	if !sm.Stationary || sm.Metrics == nil || sm.Metrics.Err0 != 0 || sm.Metrics.NCCMeasured {
		t.Fatalf("verdict %+v (metrics %+v): the fixture must be the MAD-only stationary shape (err0=0, no NCC)", sm, sm.Metrics)
	}
	if proven, why := m.zeroDisplacementProven(before, moved); proven {
		t.Fatalf("the anchor-less hidden move is claimed as proven stillness: %s", why)
	}

	stillA := paintRepeatedRowsFrame(l, 3, 3, 16, 0, -1)
	stillB := paintRepeatedRowsFrame(l, 4, 3, 16, 0, -1)
	if proven, why := m.zeroDisplacementProven(stillA, stillB); !proven {
		t.Fatalf("the same-slot selection box must prove the scene stayed still: %s", why)
	}
}

// TestScrollEvidenceZeroDisplacementProofAcceptsUniqueNCC pins the third
// review finding: a truly stationary frame pair with only a one-level
// grayscale lift fails neither the MAD shortcut (0.84 < 4.0) nor the
// normalized gates (ncc ≈ 1.0, runner-up gap ≈ 0.6, info std ≈ 47), but the
// MAD shortcut returns before the normalized evidence is measured. The proof
// must evaluate the normalized path itself instead of rejecting a non-zero
// MAD. A repeated-row tie leaves the zero candidate indistinguishable from a
// neighbouring row and must stay unproven.
func TestScrollEvidenceZeroDisplacementProofAcceptsUniqueNCC(t *testing.T) {
	profile, err := WindowsLocalProfileFor(image.Pt(1920, 1080))
	if err != nil {
		t.Fatalf("WindowsLocalProfileFor: %v", err)
	}
	m := NewScrollEvidenceMatcher(NewPixelMatcher(profile.Layout, profile.Calibration))
	l := profile.Layout
	thumbStart, thumbEnd := repeatedRowsThumb(l, 3, 30)

	before := paintMotionFrame(l, 1, 3, -1, thumbStart, thumbEnd, false)
	after := paintMotionFrame(l, 2, 3, -1, thumbStart, thumbEnd, false)
	for y := l.ListViewport.Min.Y; y < l.ListViewport.Max.Y; y++ {
		for x := l.ListViewport.Min.X; x < l.ListViewport.Max.X; x++ {
			r, g, b, a := after.Image.At(x, y).RGBA()
			lift := func(v uint32) uint8 {
				if v>>8 >= 255 {
					return 255
				}
				return uint8(v>>8) + 1
			}
			after.Image.SetRGBA(x, y, color.RGBA{R: lift(r), G: lift(g), B: lift(b), A: uint8(a >> 8)})
		}
	}

	sm := m.MatchScroll(before, after)
	if !sm.Stationary || sm.Metrics == nil || sm.Metrics.NCCMeasured {
		t.Fatalf("verdict %+v (metrics %+v): the fixture must hit the MAD shortcut before the NCC path", sm, sm.Metrics)
	}
	if sm.Metrics.Err0 <= 0 || sm.Metrics.Err0 >= m.C.ScrollStationaryMax {
		t.Fatalf("zero-shift error %.2f, want the small non-zero lift that the old proof rejected", sm.Metrics.Err0)
	}
	proven, why := m.zeroDisplacementProven(before, after)
	if !proven {
		t.Fatalf("the unique normalized zero displacement is not proven: %s", why)
	}
	if !strings.Contains(why, "unique zero-shift candidate") {
		t.Fatalf("proof %q should rest on the normalized zero-shift candidate", why)
	}
	cand, comp, ok := m.stationaryEvidence(m.rowCorrelations(before.Image, after.Image, nil))
	if !ok || cand.ncc < 0.99 || comp.gap < 0.5 || cand.std < m.C.ScrollInfoStdMin {
		t.Fatalf("normalized evidence ncc %.3f gap %.3f std %.1f (ok=%t), want a unique high-correlation, information-rich zero shift",
			cand.ncc, comp.gap, cand.std, ok)
	}

	tieA := paintRepeatedRowsFrame(l, 3, 3, -1, 0, -1)
	tieB := paintRepeatedRowsFrame(l, 4, 3, -1, 0, -1)
	if proven, why := m.zeroDisplacementProven(tieA, tieB); proven {
		t.Fatalf("the repeated-row tie is claimed as proven stillness: %s", why)
	}
}

// TestScrollEvidenceBottomCropLeadingEdgeIsNotMovement pins the fourth review
// finding: at the Windows bottom frame the captured thumb reads 368–931, and
// a second frame reads 370–931 — only the leading edge differs by two pixels,
// while the trailing edge and every list pixel stay identical. Both frames are
// Bottom, so this is the same position, and the single-edge difference must
// not veto stillness (the bottom confirmation would otherwise degrade).
func TestScrollEvidenceBottomCropLeadingEdgeIsNotMovement(t *testing.T) {
	profile, err := WindowsLocalProfileFor(image.Pt(1920, 1080))
	if err != nil {
		t.Fatalf("WindowsLocalProfileFor: %v", err)
	}
	m := NewScrollEvidenceMatcher(NewPixelMatcher(profile.Layout, profile.Calibration))
	l := profile.Layout

	before := paintMotionFrame(l, 1, 3, -1, 368, 931, false)
	after := paintMotionFrame(l, 2, 3, -1, 370, 931, false)
	if top, bottom := m.scrollEnds(before.Image); top || !bottom {
		t.Fatalf("before frame top=%t bottom=%t, want Bottom", top, bottom)
	}
	if top, bottom := m.scrollEnds(after.Image); top || !bottom {
		t.Fatalf("after frame top=%t bottom=%t, want Bottom", top, bottom)
	}
	beforeThumb := thumbReading{start: 368, end: 931, ok: true}
	afterThumb := thumbReading{start: 370, end: 931, ok: true}
	if travel, ok := thumbTranslation(beforeThumb, afterThumb); ok {
		t.Fatalf("single-edge change read as translation %d", travel)
	}
	if m.thumbMoved(beforeThumb, afterThumb) {
		t.Fatal("a single edge moving two pixels must not count as definite thumb movement")
	}
	sm := m.MatchScroll(before, after)
	if !sm.Stationary || sm.Displaced || sm.Lost || sm.Undecided {
		t.Fatalf("verdict %+v, want Stationary for the bottom-crop edge jitter (%s)", sm, sm.Evidence)
	}

	// A translation of both edges is still definite movement and must keep
	// vetoing stillness (the leading edge alone is what the crop perturbs).
	if !m.thumbMoved(beforeThumb, thumbReading{start: 370, end: 933, ok: true}) {
		t.Fatal("a two-edge translation by the calibrated floor must count as movement")
	}
}

// The bottom confirmation still accepts two pixels of leading-edge crop
// jitter. A stationary view without a current bottom indicator is incomplete.
func TestConfirmBottomRequiresStillnessAndBottomIndicator(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		thumbStart, thumbEnd int
		bottom               bool
	}{
		{"leading edge crop jitter", 370, 931, true},
		{"stationary with no bottom indicator", 368, 928, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view := &repeatedRowsSequenceView{idBase: 100}
			run := newRepeatedRowsRun(t, view, 0, 200*time.Millisecond)
			l := run.inv.Layout
			view.layout = l
			spec := repeatedRowsFrameSpec{selection: 16, thumbStart: tc.thumbStart, thumbEnd: tc.thumbEnd}
			view.specs = []repeatedRowsFrameSpec{spec, spec}
			before := paintRepeatedRowsFrame(l, 50, 3, 16, 368, 931)
			matcher := run.inv.matcher().(*repeatedRowsMatcher)
			if top, bottom := matcher.scrollEnds(before.Image); top || !bottom {
				t.Fatalf("pre-drag frame top=%t bottom=%t, want Bottom", top, bottom)
			}
			after := paintRepeatedRowsFrame(l, 51, 3, 16, tc.thumbStart, tc.thumbEnd)
			if m := matcher.Match(after); m.List.Bottom != tc.bottom {
				t.Fatalf("terminal bottom indicator = %t, want %t", m.List.Bottom, tc.bottom)
			}
			sm := matcher.MatchScroll(before, after)
			if proven, why := run.stillnessProven(before, after, sm); !proven {
				t.Fatalf("the terminal frame must prove stillness: %s", why)
			}
			err := runScriptedSession(t, run, func(ctx context.Context, run *inventoryRun) error {
				run.lastFrame = before
				_, err := run.confirmBottom(ctx)
				return err
			})
			if view.dispatches != 1 {
				t.Fatalf("bottom confirmation dispatched %d drags, want 1", view.dispatches)
			}
			if !tc.bottom {
				incomplete, ok := asPositionIncomplete(err)
				if !ok || incomplete.stage != "bottom" || run.lastFrame.ID != before.ID {
					t.Fatalf("bottom without an indicator: frame %d, error %v", run.lastFrame.ID, err)
				}
				return
			}
			if err != nil || run.lastFrame.ID != view.idBase+2 {
				t.Fatalf("bottom confirmation: frame %d, want %d, error %v", run.lastFrame.ID, view.idBase+2, err)
			}
		})
	}
}

// repeatedRowsFrameSpec is one synthetic frame of a scripted repeated-row
// sequence: the selection slot, thumb geometry and optional sub-row content
// shift name the scene position; reused marks an idle echo.
type repeatedRowsFrameSpec struct {
	selection            int
	shift                int
	shifted              bool
	reused               bool
	thumbStart, thumbEnd int
}

// repeatedRowsSequenceView serves a fixed frame sequence. Dispatch only counts
// the drags; the specs model the frames each drag exposes. blockAt makes the
// wait consume a deadline after serving that many frames (models a stream that
// has not delivered the next fresh observation yet), so the following frame is
// served to the caller's post-wait read.
type repeatedRowsSequenceView struct {
	layout     Layout
	specs      []repeatedRowsFrameSpec
	idBase     uint64
	served     int
	blockAt    int
	blocked    bool
	dispatches int
}

func (v *repeatedRowsSequenceView) Observe() (platform.Observation, error) {
	return platform.Observation{Ready: true, Geometry: 1}, nil
}

func (v *repeatedRowsSequenceView) Dispatch(platform.InputOp) platform.InputResult {
	v.dispatches++
	return platform.InputResult{Status: platform.InputPosted}
}

func (v *repeatedRowsSequenceView) Frame(ctx context.Context, req platform.FrameRequest) (platform.Frame, error) {
	if err := ctx.Err(); err != nil {
		return platform.Frame{}, err
	}
	if v.blockAt > 0 && v.served == v.blockAt && !v.blocked {
		v.blocked = true
		<-ctx.Done()
		return platform.Frame{}, ctx.Err()
	}
	if v.served >= len(v.specs) {
		<-ctx.Done()
		return platform.Frame{}, ctx.Err()
	}
	v.served++
	spec := v.specs[v.served-1]
	f := paintRepeatedRowsFrame(v.layout, v.idBase+uint64(v.served), 0, spec.selection, spec.thumbStart, spec.thumbEnd)
	if spec.shifted || spec.shift != 0 {
		f = paintShiftedRowFrame(v.layout, v.idBase+uint64(v.served), spec.shift, spec.selection, spec.thumbStart, spec.thumbEnd)
	}
	f.Reused = spec.reused
	f.ArrivedAt = time.Now()
	return f, nil
}

// paintShiftedRowFrame paints a repeated-row list whose content is shifted
// upward by shift pixels: viewport y shows the row pattern at y+shift. Each
// row carries one narrow horizontal band on a flat background, so an 8px
// sub-row move changes only the band edges: the zero-shift MAD stays below
// the stationary threshold while the normalized correlation drops on a
// support whose variance is still above the information floor. A whole-row
// shift reproduces the same pixels because every row shares the pattern.
func paintShiftedRowFrame(l Layout, id uint64, shift, selection, thumbStart, thumbEnd int) platform.Frame {
	const (
		background = 100
		bandValue  = 136
		bandTop    = 30
		bandHeight = 28
	)
	img := image.NewRGBA(image.Rectangle{Max: l.Image})
	for y := l.ListViewport.Min.Y; y < l.ListViewport.Max.Y; y++ {
		local := (y + shift - l.ListViewport.Min.Y) % l.RowPitch
		v := uint8(background)
		if local >= bandTop && local < bandTop+bandHeight {
			v = bandValue
		}
		for x := l.ListViewport.Min.X; x < l.ListViewport.Max.X; x++ {
			img.SetRGBA(x, y, color.RGBA{R: v, G: v, B: v, A: 255})
		}
	}
	if selection >= 0 {
		r := l.BorderRect(selection).Intersect(img.Bounds())
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				img.SetRGBA(x, y, color.RGBA{R: 224, G: 208, B: 67, A: 255})
			}
		}
	}
	for y := thumbStart; y <= thumbEnd; y++ {
		if y >= l.ScrollTrack.Min.Y && y < l.ScrollTrack.Max.Y {
			img.SetRGBA(l.ScrollTrack.Min.X, y, color.RGBA{R: 200, G: 200, B: 200, A: 255})
		}
	}
	return platform.Frame{ID: id, Geometry: 1, Image: img, Content: l.Content}
}

// TestScrollRetryRefusesAnchorlessRebaseAfterHiddenMove pins the first review
// finding end-to-end: the first drag moves one row of a repeated-row list but
// the wait sees only anchor-less, pixel-identical frames and ends Stationary.
// The newest frame before the retry sits one row further than the pre-drag
// baseline, and with no selection pair or thumb the zero-shift MAD cannot tell
// that from stillness. No second drag may be sent, so the row the hidden move
// exposed is never silently inherited as visited.
func TestScrollRetryRefusesAnchorlessRebaseAfterHiddenMove(t *testing.T) {
	view := &repeatedRowsSequenceView{}
	run := newRepeatedRowsRun(t, view, 1, 120*time.Millisecond)
	l := run.inv.Layout
	retryStart, retryEnd := repeatedRowsThumb(l, 4, 30)
	nextStart, nextEnd := repeatedRowsThumb(l, 5, 30)
	view.layout = l
	view.blockAt = 2 // the wait observes the hidden move, then the stream stalls until its deadline
	view.specs = []repeatedRowsFrameSpec{
		{selection: -1}, // pre-drag frame: no selection, no thumb
		{selection: -1}, // the hidden one-row move is pixel-identical on repeated rows
		{selection: 12, thumbStart: retryStart, thumbEnd: retryEnd}, // retry baseline
	}

	err := runScriptedScroll(t, run)
	var incomplete *positionIncompleteError
	if !errors.As(err, &incomplete) || incomplete.stage != "scroll" {
		t.Fatalf("scroll error %v, want a position-scoped scroll failure", err)
	}
	if !strings.Contains(err.Error(), "retry baseline") || !strings.Contains(err.Error(), "not proven") {
		t.Fatalf("scroll error %q should name the unproven retry baseline", err)
	}
	if view.dispatches != 1 {
		t.Fatalf("scroll dispatched %d drags, want no retry on an anchor-less moved baseline", view.dispatches)
	}
	if run.viewport != 3 || run.lastScrollRows != 0 {
		t.Fatalf("viewport %d rows %d after the refused retry, want the bookkeeping unchanged (3/0)",
			run.viewport, run.lastScrollRows)
	}

	// If the retry had been rebased, one more drag would have verified one
	// row against the already-moved baseline and committed it as if the
	// skipped row had been visited.
	hypotheticalBefore := paintRepeatedRowsFrame(l, 100, 4, 12, retryStart, retryEnd)
	hypotheticalAfter := paintRepeatedRowsFrame(l, 101, 5, 8, nextStart, nextEnd)
	hyp := run.inv.matcher().MatchScroll(hypotheticalBefore, hypotheticalAfter)
	if !hyp.Displaced || hyp.Rows != 1 {
		t.Fatalf("hypothetical second drag %+v, want the one-row displacement that would have hidden the skipped row (%s)",
			hyp, hyp.Evidence)
	}
}

// TestAwaitScrollRejectsUnprovenSettleAtStaleCandidate pins the second review
// finding: after a verified one-row candidate, two fresh repeated-row frames
// whose anchors are gone still look Stationary to the MAD, but the scene has
// moved two more rows. A settle confirmation needs proof, so the stale
// candidate must not commit; the wait ends unsettled without touching the
// bookkeeping.
func TestAwaitScrollRejectsUnprovenSettleAtStaleCandidate(t *testing.T) {
	view := &repeatedRowsSequenceView{idBase: 100}
	run := newRepeatedRowsRun(t, view, 0, 120*time.Millisecond)
	l := run.inv.Layout
	candidateStart, candidateEnd := repeatedRowsThumb(l, 4, 30)
	beforeStart, beforeEnd := repeatedRowsThumb(l, 3, 30)
	view.layout = l
	view.specs = []repeatedRowsFrameSpec{
		{selection: 12, thumbStart: candidateStart, thumbEnd: candidateEnd},
		{selection: -1}, // scene at +3 rows, anchors lost
		{selection: -1}, // still there, anchors still lost
	}
	before := paintRepeatedRowsFrame(l, 1, 3, 16, beforeStart, beforeEnd)
	candidate := paintRepeatedRowsFrame(l, 2, 4, 12, candidateStart, candidateEnd)
	if sm := run.inv.matcher().MatchScroll(before, candidate); !sm.Displaced || sm.Rows != 1 {
		t.Fatalf("fixture candidate %+v, want the verified one-row advance (%s)", sm, sm.Evidence)
	}
	moving := paintRepeatedRowsFrame(l, 3, 6, -1, 0, -1)
	rel := run.inv.matcher().MatchScroll(candidate, moving)
	if !rel.Stationary || rel.Metrics == nil || rel.Metrics.NCCMeasured {
		t.Fatalf("candidate-relative moving frame %+v (metrics %+v), want the MAD-only stationary trap", rel, rel.Metrics)
	}
	evidence, ok := run.inv.matcher().(*repeatedRowsMatcher)
	if !ok {
		t.Fatalf("matcher %T is not the repeated-rows evidence matcher", run.inv.matcher())
	}
	if proven, why := evidence.zeroDisplacementProven(candidate, moving); proven {
		t.Fatalf("the still-moving frame is claimed as proven stillness: %s", why)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := run.awaitScroll(ctx, before)
	if err != nil {
		t.Fatalf("awaitScroll: %v", err)
	}
	if out.displaced || out.stationary || !out.lost || !out.unsettled {
		t.Fatalf("outcome %+v, want the unsettled Lost instead of a stale candidate commit", out)
	}
	if out.settled || out.settleFrameID != 0 {
		t.Fatalf("outcome %+v: the unproven candidate must not settle", out)
	}
	if out.after.ID != view.idBase+1 || out.match.Rows != 1 {
		t.Fatalf("candidate frame %d rows %d, want the candidate preserved without an advance", out.after.ID, out.match.Rows)
	}
}

// TestAwaitScrollRecoversAnchorsAndCommitsTheTrueTotal is the positive half of
// the second review finding: the same stale candidate is followed by frames
// whose anchors recovered and show the true three-row total. The candidate
// must be replaced with the verified total (not committed at the old one-row
// offset) and settle on the recovered same-slot selection box.
func TestAwaitScrollRecoversAnchorsAndCommitsTheTrueTotal(t *testing.T) {
	view := &repeatedRowsSequenceView{}
	run := newRepeatedRowsRun(t, view, 0, 5*time.Second)
	l := run.inv.Layout
	baseStart, baseEnd := repeatedRowsThumb(l, 3, 30)
	candidateStart, candidateEnd := repeatedRowsThumb(l, 4, 30)
	recoveredStart, recoveredEnd := repeatedRowsThumb(l, 6, 30)
	view.layout = l
	view.specs = []repeatedRowsFrameSpec{
		{selection: 16, thumbStart: baseStart, thumbEnd: baseEnd},
		{selection: 12, thumbStart: candidateStart, thumbEnd: candidateEnd},
		{selection: 4, thumbStart: recoveredStart, thumbEnd: recoveredEnd},
		{selection: 4, thumbStart: recoveredStart, thumbEnd: recoveredEnd},
		{selection: 4, thumbStart: recoveredStart, thumbEnd: recoveredEnd},
	}

	if err := runScriptedScroll(t, run); err != nil {
		t.Fatalf("the recovered three-row total must commit: %v", err)
	}
	if view.dispatches != 1 {
		t.Fatalf("scroll dispatched %d drags, want the single drag", view.dispatches)
	}
	if run.lastScrollRows != 3 || run.viewport != 4 {
		t.Fatalf("committed rows %d, viewport %d; want the true three-row total in one scroll step (3/4)",
			run.lastScrollRows, run.viewport)
	}
	if run.ledger.visible()[l.positionSlot(l.Rows-3, 0)].State != slotUnresolved {
		t.Fatal("the first newly exposed row must stay unvisited after the three-row commit")
	}
	if run.ledger.visible()[l.positionSlot(l.Rows-4, 0)].State != slotAdmitted {
		t.Fatal("the last overlap row must inherit its visited state")
	}
}

// TestAwaitScrollRejectsInformationRichSubRowCreep pins the second review
// round's first finding: after a verified one-row candidate, fresh frames
// creep 8px and 16px forward while the selection border still reads slot 12
// and the zero-shift MAD stays below 4. The support is information-rich and
// the zero-shift correlation collapses; that is the same movement
// counter-evidence the wait already uses to start its motion clock. The
// candidate must keep waiting, then commit only when a later frame proves the
// candidate's content again (the real settled position).
func TestAwaitScrollRejectsInformationRichSubRowCreep(t *testing.T) {
	view := &repeatedRowsSequenceView{idBase: 200}
	run := newRepeatedRowsRun(t, view, 0, 3*time.Second)
	l := run.inv.Layout
	baseStart, baseEnd := repeatedRowsThumb(l, 3, 80)
	candidateStart, candidateEnd := repeatedRowsThumb(l, 4, 80)
	view.layout = l
	view.specs = []repeatedRowsFrameSpec{
		{selection: 12, shift: l.RowPitch, thumbStart: candidateStart, thumbEnd: candidateEnd},
		{selection: 12, shift: l.RowPitch + 8, thumbStart: candidateStart, thumbEnd: candidateEnd},
		{selection: 12, shift: l.RowPitch + 16, thumbStart: candidateStart, thumbEnd: candidateEnd},
		{selection: 12, shift: l.RowPitch, thumbStart: candidateStart, thumbEnd: candidateEnd},
		{selection: 12, shift: l.RowPitch, thumbStart: candidateStart, thumbEnd: candidateEnd},
	}
	before := paintShiftedRowFrame(l, 1, 0, 16, baseStart, baseEnd)
	candidate := paintShiftedRowFrame(l, 2, l.RowPitch, 12, candidateStart, candidateEnd)
	if sm := run.inv.matcher().MatchScroll(before, candidate); !sm.Displaced || sm.Rows != 1 {
		t.Fatalf("fixture candidate %+v, want the verified one-row advance (%s)", sm, sm.Evidence)
	}
	creep8 := paintShiftedRowFrame(l, 3, l.RowPitch+8, 12, candidateStart, candidateEnd)
	rel := run.inv.matcher().MatchScroll(candidate, creep8)
	if !rel.Stationary || rel.Metrics == nil || rel.Metrics.NCCMeasured ||
		rel.Metrics.Err0 <= 0 || rel.Metrics.Err0 >= run.inv.matcher().(*repeatedRowsMatcher).C.ScrollStationaryMax {
		t.Fatalf("candidate-relative 8px creep %+v (metrics %+v), want the low-MAD stationary trap", rel, rel.Metrics)
	}
	evidence, ok := run.inv.matcher().(*repeatedRowsMatcher)
	if !ok {
		t.Fatalf("matcher %T is not the repeated-rows evidence matcher", run.inv.matcher())
	}
	borderMask := []image.Rectangle{l.BorderRect(12)}
	ncc0, std, ok := evidence.correlationAt(candidate.Image, creep8.Image, 0, borderMask)
	if !ok || std < evidence.C.ScrollInfoStdMin || ncc0 >= evidence.C.ScrollStationaryNCC {
		t.Fatalf("zero-shift evidence ncc %.3f std %.1f (ok=%t), want information-rich movement counter-evidence", ncc0, std, ok)
	}
	if proven, why := evidence.zeroDisplacementProven(candidate, creep8); proven {
		t.Fatalf("the sub-row creep is claimed as proven stillness: %s", why)
	} else if !strings.Contains(why, "movement counter-evidence") {
		t.Fatalf("proof %q should name the normalized movement counter-evidence", why)
	}
	if aligned, _, ok := evidence.correlationAt(candidate.Image, creep8.Image, 8, borderMask); !ok || aligned < 0.99 {
		t.Fatalf("the 8px-aligned correlation %.3f (ok=%t) should prove the actual displacement", aligned, ok)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := run.awaitScroll(ctx, before)
	if err != nil {
		t.Fatalf("awaitScroll: %v", err)
	}
	if !out.displaced || !out.settled || out.lost || out.stationary {
		t.Fatalf("outcome %+v, want the settled candidate after the creep returns to the proven content", out)
	}
	if out.match.Rows != 1 || out.after.ID != view.idBase+1 {
		t.Fatalf("commit frame %d rows %d, want the original one-row candidate (frame %d)", out.after.ID, out.match.Rows, view.idBase+1)
	}
	if out.settleFrameID != view.idBase+5 {
		t.Fatalf("settled on frame %d, want the second recovered confirmation %d", out.settleFrameID, view.idBase+5)
	}
	if !out.motionStarted {
		t.Fatal("the information-rich creep must start the motion clock")
	}
}

// TestConfirmBottomRejectsUnprovenFinalRowAdvance pins the second review
// round's second finding: on an 80-row repeated list with one row left, the
// pre-drag thumb still reads Bottom (end 930 > 929), but the confirmation
// drag advances that last row. The fresh frames lose the anchors and the
// trailing edge no longer reads Bottom; the MAD still says Stationary. The
// position must be recorded incomplete with Bottom=false instead of being
// accepted as complete.
func TestConfirmBottomRejectsUnprovenFinalRowAdvance(t *testing.T) {
	view := &repeatedRowsSequenceView{idBase: 300}
	run := newRepeatedRowsRun(t, view, 0, 200*time.Millisecond)
	l := run.inv.Layout
	view.layout = l
	view.specs = []repeatedRowsFrameSpec{
		{selection: -1}, // final-row advance: anchors and thumb lost, Bottom=false
		{selection: -1},
	}
	before := paintRepeatedRowsFrame(l, 50, 76, -1, 881, 930)
	if !(930 >= run.inv.matcher().(*repeatedRowsMatcher).C.ScrollbarBottomEndMin) {
		t.Fatalf("fixture bottom end 930 is below the calibrated Bottom threshold %d",
			run.inv.matcher().(*repeatedRowsMatcher).C.ScrollbarBottomEndMin)
	}
	evidence := run.inv.matcher().(*repeatedRowsMatcher)
	after := paintRepeatedRowsFrame(l, 301, 77, -1, 0, -1)
	if sm := evidence.MatchScroll(before, after); !sm.Stationary || sm.Metrics == nil || sm.Metrics.Err0 != 0 {
		t.Fatalf("fixture after frame %+v (metrics %+v), want the MAD-only stationary shape", sm, sm.Metrics)
	}
	if proven, why := evidence.zeroDisplacementProven(before, after); proven {
		t.Fatalf("the final-row advance is claimed as proven stillness: %s", why)
	}

	err := runScriptedSession(t, run, func(ctx context.Context, run *inventoryRun) error {
		run.lastFrame = before
		run.listMatch = run.inv.matcher().Match(before)
		run.listMatch.List.Bottom = true
		_, err := run.confirmBottom(ctx)
		return err
	})
	var fault *scan.FaultError
	if !errors.As(err, &fault) || fault.Code != CodeNavigationMismatch {
		t.Fatalf("bottom confirmation error %v, want navigationMismatch", err)
	}
	var incomplete *positionIncompleteError
	if !errors.As(err, &incomplete) || incomplete.stage != "bottom" {
		t.Fatalf("bottom confirmation error %v, want a position-level bottom failure", err)
	}
	if view.dispatches != 1 {
		t.Fatalf("bottom confirmation dispatched %d drags, want the single confirmation drag", view.dispatches)
	}
	if finishErr := run.finishPosition(err, positionTiming{}, 0, 0); finishErr != nil {
		t.Fatalf("one incomplete position must not stop the session: %v", finishErr)
	}
	positions := run.inv.scope.Positions
	if len(positions) != 1 {
		t.Fatalf("recorded %d positions, want the incomplete position", len(positions))
	}
	entry := positions[0]
	if entry.Complete || entry.Bottom {
		t.Fatalf("recorded position %+v, want complete=false bottom=false", entry)
	}
	if entry.Stage != "bottom" || entry.ReasonCode != CodeNavigationMismatch {
		t.Fatalf("recorded position %+v, want the bottom/navigationMismatch reason", entry)
	}
}

// TestScrollRetryRejectsMotionObservedBeforeCandidate pins the third review
// finding on both frame paths: the first drag produces only an 8px sub-row
// move before any candidate exists. The same-slot border and low MAD keep the
// matcher verdict Stationary, but the information-rich zero-shift correlation
// contradicts stillness. The wait must remember that movement even after a
// later frame returns to the baseline, so `unmoved` stays false and no second
// drag is sent.
func TestScrollRetryRejectsMotionObservedBeforeCandidate(t *testing.T) {
	for _, reused := range []bool{false, true} {
		name := "fresh"
		if reused {
			name = "reused"
		}
		t.Run(name, func(t *testing.T) {
			view := &repeatedRowsSequenceView{idBase: 400}
			run := newRepeatedRowsRun(t, view, 1, 2*time.Second)
			l := run.inv.Layout
			thumbStart, thumbEnd := repeatedRowsThumb(l, 3, 80)
			view.layout = l
			view.blockAt = 3 // the wait sees the creep and the rebound, then stalls until its deadline
			view.specs = []repeatedRowsFrameSpec{
				{selection: 16, shifted: true, thumbStart: thumbStart, thumbEnd: thumbEnd},
				{selection: 16, shift: 8, shifted: true, reused: reused, thumbStart: thumbStart, thumbEnd: thumbEnd},
				{selection: 16, shifted: true, reused: reused, thumbStart: thumbStart, thumbEnd: thumbEnd},
				{selection: 16, shifted: true, thumbStart: thumbStart, thumbEnd: thumbEnd}, // would-be retry baseline
			}
			baseline := paintShiftedRowFrame(l, 1, 0, 16, thumbStart, thumbEnd)
			moved := paintShiftedRowFrame(l, 2, 8, 16, thumbStart, thumbEnd)
			rebound := paintShiftedRowFrame(l, 3, 0, 16, thumbStart, thumbEnd)
			evidence := run.inv.matcher().(*repeatedRowsMatcher)
			sm := evidence.MatchScroll(baseline, moved)
			if !sm.Stationary || sm.Metrics == nil || sm.Metrics.NCCMeasured ||
				sm.Metrics.Err0 <= 0 || sm.Metrics.Err0 >= evidence.C.ScrollStationaryMax {
				t.Fatalf("baseline → 8px creep %+v (metrics %+v), want the low-MAD stationary shape", sm, sm.Metrics)
			}
			if proven, why := evidence.zeroDisplacementProven(baseline, moved); proven {
				t.Fatalf("the 8px creep is claimed as proven stillness: %s", why)
			}
			if counter, why := evidence.zeroShiftMovement(baseline, moved); !counter {
				t.Fatalf("the 8px creep is not reported as movement counter-evidence: %s", why)
			}
			if proven, why := evidence.zeroDisplacementProven(baseline, rebound); !proven {
				t.Fatalf("the rebound baseline should be a provable retry base: %s", why)
			}

			ledgerBefore := slices.Clone(run.ledger.cells)
			err := runScriptedScroll(t, run)
			var incomplete *positionIncompleteError
			if !errors.As(err, &incomplete) || incomplete.stage != "scroll" {
				t.Fatalf("scroll error %v, want a position-scoped scroll failure", err)
			}
			var fault *scan.FaultError
			if !errors.As(err, &fault) || fault.Code != CodeNavigationMismatch {
				t.Fatalf("scroll error %v, want navigationMismatch", err)
			}
			if !strings.Contains(err.Error(), "observed movement or an undecided comparison") {
				t.Fatalf("scroll error %q should name the observed movement that vetoed the retry", err)
			}
			if view.dispatches != 1 {
				t.Fatalf("scroll dispatched %d drags, want no retry after observed sub-row movement", view.dispatches)
			}
			if run.viewport != 3 || run.lastScrollRows != 0 {
				t.Fatalf("viewport %d rows %d after the refused retry, want the bookkeeping unchanged (3/0)",
					run.viewport, run.lastScrollRows)
			}
			if run.ledger.rowBase != 0 || !slices.Equal(run.ledger.cells, ledgerBefore) {
				t.Fatalf("ledger changed after the refused retry: %+v", run.ledger)
			}
		})
	}
}
