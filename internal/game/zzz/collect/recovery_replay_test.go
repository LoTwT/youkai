package collect_test

import (
	"context"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"slices"
	"testing"

	"github.com/LoTwT/youkai/internal/game/zzz/collect"
	"github.com/LoTwT/youkai/internal/platform"
	"github.com/LoTwT/youkai/internal/scan"
)

// Cell 1 has the same detail as admitted cell 0. Cell 2 would disambiguate
// them, but platform permission to send the reference click is denied.
func TestRecoveryReplayFatalDuringAttribution(t *testing.T) {
	for _, code := range []string{platform.CodeFocusLost, platform.CodeGeometryChanged, platform.CodePermissionRevoked} {
		t.Run(code, func(t *testing.T) {
			game := newScriptedGame(true, true, true)
			game.detailKeys = map[int]int{0: 5, 1: 5, 2: 9}
			h := newTraversalHarness(t, game)
			h.inv.RequirePanelRangeProof = true
			h.inv.EnableBoundedRecovery = true
			h.inv.Matcher = rangeMatcher{Matcher: h.inv.Matcher, game: game}
			denied, later := false, 0
			h.session.DispatchFn = func(op platform.InputOp) platform.InputResult {
				if denied {
					later++
				}
				if op.Kind == platform.InputClick && op.At == slotCenter(game.layout, 2) {
					denied = true
					return platform.InputResult{Status: platform.InputNotDispatched,
						Denial: platform.NewFault(code, "reference click refused")}
				}
				return game.dispatch(op)
			}
			h.start()
			snap := h.endFailed(code)
			obs := h.observations()
			if !denied || later != 0 || snap.Counts.Captured != 1 || len(obs) != 1 ||
				obs[0].Position != 1 || obs[0].Row != 0 || obs[0].Column != 0 {
				t.Fatalf("denied=%t later dispatches=%d counts=%+v cells=%v", denied, later, snap.Counts, positions(obs))
			}
			h.requireFatallyStoppedPosition(1)
		})
	}
}

// Supply unreadable field-count evidence for exactly the initial stability
// window, then let the real pixel matcher read the next frames.
func TestRecoveryReplayPassiveDetailRevisit(t *testing.T) {
	for _, mode := range []string{"valid", "interrupted detail wait", "other cell", "truncated list", "stale frame", "unreadable", "second unresolved cell"} {
		t.Run(mode, func(t *testing.T) {
			game := newScriptedGame(true, true)
			h := newTraversalHarness(t, game)
			if err := h.inv.SetScanPositions([]int{1}); err != nil {
				t.Fatal(err)
			}
			h.inv.RequirePanelRangeProof = true
			h.inv.EnableBoundedRecovery = true
			frames := 0
			h.inv.Matcher = warehouseChangeMatcher{Matcher: rangeMatcher{Matcher: h.inv.Matcher, game: game}, change: func(m *collect.PageMatch) {
				if m.List == nil || m.Detail == nil || m.List.Selected < 0 {
					return
				}
				if m.List.Selected == 1 {
					if mode == "second unresolved cell" {
						m.Detail.Substats = 0
					}
					return
				}
				frames++
				if frames <= h.inv.StableFrames || mode == "unreadable" {
					m.Detail.Substats = 0
				} else if mode == "other cell" {
					m.List.Selected = 1
				} else if mode == "truncated list" {
					m.List.Occupied[1] = false
				}
			}}
			if mode == "stale frame" || mode == "interrupted detail wait" {
				next := h.session.FrameFn
				interrupted := false
				h.session.FrameFn = func(ctx context.Context, req platform.FrameRequest) (platform.Frame, error) {
					if mode == "interrupted detail wait" && frames == 1 && !interrupted {
						interrupted = true
						return platform.Frame{}, context.DeadlineExceeded
					}
					f, err := next(ctx, req)
					if mode == "stale frame" && frames >= h.inv.StableFrames {
						f.ArrivedAt = req.After
					}
					return f, err
				}
			}
			h.start()
			switch mode {
			case "valid", "interrupted detail wait":
				snap := h.endCompleted()
				entry := h.requireCompletePosition(1)
				if snap.Counts.Captured != 2 || len(entry.Ledger.Slots) != 2 ||
					entry.Ledger.Slots[0].Sequence != 1 || entry.Ledger.Slots[1].Sequence != 2 ||
					entry.Ledger.Slots[0].RevisitFromFrameID == 0 || entry.Ledger.Slots[1].RevisitFromFrameID != 0 {
					t.Fatalf("counts=%+v ledger=%+v", snap.Counts, entry.Ledger)
				}
				if countSlotClicks(game.opsSnapshot(), game.layout) != 2 {
					t.Fatal("passive revisit sent an extra click")
				}
			case "unreadable", "second unresolved cell":
				snap := h.endFailed(collect.CodePositionFailureLimit)
				entry := h.requireIncompletePosition(1, "detail", collect.CodeDetailUnresolved)
				want := 0
				if mode == "second unresolved cell" {
					want = 1
				}
				cell := entry.Ledger.Slots[want]
				if snap.Counts.Captured != want || cell.Sequence != 0 || cell.State != "unresolved" ||
					entry.Ledger.Slots[0].RevisitFromFrameID == 0 || entry.Ledger.Slots[1].RevisitFromFrameID != 0 {
					t.Fatalf("counts=%+v ledger=%+v", snap.Counts, entry.Ledger)
				}
			default:
				snap := h.endFailed(collect.CodePositionLost)
				ops := game.opsSnapshot()
				if snap.Counts.Captured != 0 || ops[len(ops)-1].At != slotCenter(game.layout, 0) {
					t.Fatalf("counts=%+v last input=%+v", snap.Counts, ops[len(ops)-1])
				}
			}
		})
	}
}

type failedFieldRecognizer struct{}

// Keep the real zero-displacement prover for the top comparison. Only the
// injected advance loses correspondence; the return uses the rendered pixels.
type lostFirstAdvance struct {
	*collect.ScrollEvidenceMatcher
	rangeMatch rangeMatcher
	lost       bool
}

func (m *lostFirstAdvance) Match(f platform.Frame) collect.PageMatch {
	return m.rangeMatch.Match(f)
}

func (m *lostFirstAdvance) MatchScroll(before, after platform.Frame) collect.ScrollMatch {
	if m.lost {
		return collect.ScrollMatch{Lost: true, Evidence: "first advance lost its correspondence"}
	}
	return m.ScrollEvidenceMatcher.MatchScroll(before, after)
}

// The positive replay supplies independent evidence for all non-S controls.
// The Windows reset/S probe alone has no such observation on reopening.
type replayCurrentRange struct {
	*lostFirstAdvance
	otherFilters collect.FilterState
}

func (m replayCurrentRange) CurrentNonSFilters(platform.Frame) collect.FilterState {
	return m.otherFilters
}

func TestRecoveryReplayRebuildAtVerifiedTop(t *testing.T) {
	for _, mode := range []string{"resume", "changed upper filter", "truncated top", "replaced cell", "range lost after close", "repeated loss", "later viewport", "reset and S probes only", "unreadable current range", "no current panel frame"} {
		t.Run(mode, func(t *testing.T) {
			game := newScriptedGame(listOf(36)...)
			if mode == "later viewport" {
				game.setList(1, listOf(52))
			}
			h := newTraversalHarness(t, game)
			if err := h.inv.SetScanPositions([]int{1}); err != nil {
				t.Fatal(err)
			}
			h.inv.RequirePanelRangeProof = true
			h.inv.EnableBoundedRecovery = true
			pixel := h.inv.Matcher.(*collect.PixelMatcher)
			m := &lostFirstAdvance{ScrollEvidenceMatcher: collect.NewScrollEvidenceMatcher(pixel)}
			advances, opens := 0, 0
			m.rangeMatch = rangeMatcher{Matcher: pixel, game: game}
			if mode == "no current panel frame" {
				reopenedFrames := 0
				m.rangeMatch.amend = func(_ int, _ *collect.PageMatch) {
					if opens == 2 {
						reopenedFrames++
					}
				}
				next := h.session.FrameFn
				h.session.FrameFn = func(ctx context.Context, req platform.FrameRequest) (platform.Frame, error) {
					if reopenedFrames >= h.inv.StableFrames {
						return platform.Frame{}, context.DeadlineExceeded
					}
					return next(ctx, req)
				}
			}
			current := replayCurrentRange{lostFirstAdvance: m, otherFilters: collect.FilterClear}
			h.inv.Matcher = &current
			closeAttempts, topDrags, replacedFrames := 0, 0, 0
			if mode == "replaced cell" {
				next := h.session.FrameFn
				h.session.FrameFn = func(ctx context.Context, req platform.FrameRequest) (platform.Frame, error) {
					f, err := next(ctx, req)
					if err == nil && topDrags > 0 && game.viewport == 0 {
						img := image.NewRGBA(f.Image.Bounds())
						draw.Draw(img, img.Bounds(), f.Image, f.Image.Bounds().Min, draw.Src)
						draw.Draw(img, game.layout.Slots[0].Inset(20), image.NewUniform(color.White), image.Point{}, draw.Src)
						f.Image = img
						replacedFrames++
					}
					return f, err
				}
			}
			switch mode {
			case "reset and S probes only":
				h.inv.Matcher = m
			case "changed upper filter":
				current.otherFilters = collect.FilterActive
			case "unreadable current range":
				current.otherFilters = collect.FilterUnknown
			}
			h.session.DispatchFn = func(op platform.InputOp) platform.InputResult {
				switch op.At {
				case game.layout.ScrollFrom:
					if op.Kind == platform.InputDrag {
						advances++
						m.lost = advances == 1 || mode == "repeated loss"
						if mode == "later viewport" {
							m.lost = advances == 2
						}
					}
				case game.layout.FilterButton:
					opens++
					m.lost = false
				case game.layout.TopFrom:
					if opens == 2 {
						topDrags++
					}
					if opens == 2 && mode == "truncated top" {
						game.mu.Lock()
						game.lists[0][0] = false
						game.mu.Unlock()
					}
				case game.layout.PanelClose:
					if opens == 2 && mode == "range lost after close" {
						closeAttempts++
						if closeAttempts == 1 {
							game.failCloseLeft = 1
							current.otherFilters = collect.FilterUnknown
						}
					}
				}
				return game.dispatch(op)
			}
			h.start()
			switch mode {
			case "resume":
				snap := h.endCompleted()
				if got := filterControlSequence(game.opsSnapshot(), game.layout); !slices.Equal(got, []string{"open", "reset", "s", "close", "open", "close"}) {
					t.Fatalf("ordinary preparation and read-only recheck sent %v", got)
				}
				entry := h.requireCompletePosition(1)
				if snap.Counts.Captured != 36 || entry.Rebuild.Confirmed <= entry.Rebuild.Before ||
					entry.Rebuild.Before == 0 || len(entry.Ledger.Slots) != 36 || countSlotClicks(game.opsSnapshot(), game.layout) != 36 {
					t.Fatalf("counts=%+v entry=%+v", snap.Counts, entry)
				}
				for i, cell := range entry.Ledger.Slots {
					if cell.Sequence != scan.Seq(i+1) || cell.AbsoluteRow != i/4 || cell.Column != i%4 {
						t.Fatalf("slot %d: %+v", i, cell)
					}
				}
			case "reset and S probes only", "unreadable current range", "range lost after close", "replaced cell":
				snap := h.endFailed(collect.CodePositionFailureLimit)
				stage, code, wantOpens := "scroll", collect.CodePositionLost, 1
				if mode == "unreadable current range" || mode == "range lost after close" {
					stage, code, wantOpens = "filter", collect.CodeNavigationMismatch, 2
				}
				if mode == "range lost after close" && (closeAttempts != 1 || topDrags != 0) {
					t.Fatalf("unverified panel caused %d closes and %d return drags", closeAttempts, topDrags)
				}
				if mode == "replaced cell" {
					stage, wantOpens = "rebuild", 2
					if replacedFrames == 0 {
						t.Fatal("the rebuilt top was never shown")
					}
				}
				entry := h.requireIncompletePosition(1, stage, code)
				if snap.Counts.Captured != 20 || len(entry.Ledger.Slots) != 20 || advances != 1 || opens != wantOpens || entry.Rebuild.Confirmed != 0 {
					t.Fatalf("counts=%+v advances=%d opens=%d rebuild=%+v", snap.Counts, advances, opens, entry.Rebuild)
				}
				for i, cell := range entry.Ledger.Slots {
					if cell.Sequence != scan.Seq(i+1) {
						t.Fatalf("unproven rebuild changed cell %d to %+v", i, cell)
					}
				}
			case "repeated loss", "later viewport":
				snap := h.endFailed(collect.CodePositionFailureLimit)
				h.requireIncompletePosition(1, "scroll", collect.CodePositionLost)
				wantCaptured, wantOpens := 20, 2
				if mode == "later viewport" {
					wantCaptured, wantOpens = 36, 1
				}
				if snap.Counts.Captured != wantCaptured || advances != 2 || opens != wantOpens {
					t.Fatalf("counts=%+v advances=%d panel opens=%d", snap.Counts, advances, opens)
				}
			default:
				code, last := collect.CodeNavigationMismatch, game.layout.FilterButton
				if mode == "truncated top" {
					code, last = collect.CodePositionLost, game.layout.TopFrom
				}
				snap := h.endFailed(code)
				ops := game.opsSnapshot()
				if snap.Counts.Captured != 20 || ops[len(ops)-1].At != last {
					t.Fatalf("counts=%+v last input=%+v", snap.Counts, ops[len(ops)-1])
				}
			}
		})
	}
}

func (failedFieldRecognizer) Recognize(_ context.Context, task scan.Task, submit scan.Submit) error {
	return submit(task.SessionID, task.Seq, scan.Recognition{Failed: errors.New("unreadable field")})
}

func TestRecoveryReplayAdmittedOCRFailureKeepsOneCapture(t *testing.T) {
	game := newScriptedGame(true)
	h := newTraversalHarness(t, game)
	h.inv.RequirePanelRangeProof = true
	h.inv.EnableBoundedRecovery = true
	h.inv.Matcher = rangeMatcher{Matcher: h.inv.Matcher, game: game}
	h.Recognizer = failedFieldRecognizer{}
	if err := h.inv.SetScanPositions([]int{1}); err != nil {
		t.Fatal(err)
	}
	h.start()
	snap := h.endCompleted()
	records := h.sink.recordsSnapshot()
	if snap.Counts.Captured != 1 || snap.Counts.Failed != 1 || len(records) != 1 ||
		records[0].Seq != 1 || records[0].Reason != "unreadable field" {
		t.Fatalf("counts=%+v records=%d, want one failed OCR record at sequence 1 with its reason", snap.Counts, len(records))
	}
	if got := countSlotClicks(game.opsSnapshot(), game.layout); got != 1 {
		t.Fatalf("grid clicks=%d, want one capture click", got)
	}
}
