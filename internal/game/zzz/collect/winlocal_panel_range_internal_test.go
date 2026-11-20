package collect

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/platform"
	"github.com/LoTwT/youkai/internal/platform/platformfake"
)

const windowsPanelFixtureDir = "testdata/winlocal-filter"

func loadWindowsPanelFrames(t *testing.T, layout Layout) map[string]platform.Frame {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(windowsPanelFixtureDir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		OriginalClientSize struct{ Width, Height int }
		Position           int
		Samples            []struct {
			Sample string
			Files  []struct {
				File, SHA256 string
				Bytes        int
				SourceRect   struct{ X, Y, Width, Height, RightExclusive, BottomExclusive int }
			}
		}
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	w, h := manifest.OriginalClientSize.Width, manifest.OriginalClientSize.Height
	if layout.Image != image.Pt(w, h) || manifest.Position != 3 || len(manifest.Samples) != 3 {
		t.Fatalf("unexpected fixture coverage: %dx%d, position %d, %d samples", w, h, manifest.Position, len(manifest.Samples))
	}
	frames := make(map[string]platform.Frame, 3)
	for i, sample := range manifest.Samples {
		canvas := image.NewRGBA(image.Rect(0, 0, w, h))
		for _, entry := range sample.Files {
			data, err := os.ReadFile(filepath.Join(windowsPanelFixtureDir, entry.File))
			if err != nil {
				t.Fatal(err)
			}
			if len(data) != entry.Bytes || fmt.Sprintf("%x", sha256.Sum256(data)) != entry.SHA256 {
				t.Fatalf("%s differs from the attached crop in the manifest", entry.File)
			}
			crop, err := png.Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatalf("decode %s: %v", entry.File, err)
			}
			src := entry.SourceRect
			region := image.Rect(src.X, src.Y, src.RightExclusive, src.BottomExclusive)
			if crop.Bounds().Size() != region.Size() || region.Size() != image.Pt(src.Width, src.Height) || !region.In(canvas.Bounds()) {
				t.Fatalf("%s: crop %v does not fit source rectangle %v", entry.File, crop.Bounds(), region)
			}
			draw.Draw(canvas, region, crop, crop.Bounds().Min, draw.Src)
		}
		frames[sample.Sample] = platform.Frame{ID: uint64(i + 1), Geometry: 1, Image: canvas, Content: layout.Content}
	}
	return frames
}

func windowsNativeRangeProfile(t *testing.T) WindowsLocalProfile {
	t.Helper()
	profile, err := WindowsLocalProfileFor(image.Pt(1920, 1080))
	if err != nil {
		t.Fatal(err)
	}
	return profile
}

func TestWindowsLocalPanelRangeStaticCrops(t *testing.T) {
	profile := windowsNativeRangeProfile(t)
	inv := profile.Inventory(false)
	if !inv.RequirePanelRangeProof || inv.actionBudget() != 2*time.Second || inv.stableFrames() != 3 {
		t.Fatalf("native panel gate = required %t, budget %s, frames %d", inv.RequirePanelRangeProof, inv.actionBudget(), inv.stableFrames())
	}
	frames := loadWindowsPanelFrames(t, inv.Layout)
	for _, tc := range []struct {
		name  string
		reset bool
		s     FilterState
	}{
		{"residual", false, FilterUnknown},
		{"reset", true, FilterClear},
		{"s-only", false, FilterActive},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frame := frames[tc.name]
			match := inv.Matcher.Match(frame)
			if match.Page != PageFilterPanel || match.Panel == nil || observedSortLabel(match) != sortLabelRarity ||
				match.Panel.Range == nil || match.Panel.Range.ResetVerified != tc.reset || match.Panel.Range.RarityS != tc.s {
				t.Fatalf("page=%s sort=%q panel=%+v, want panel with reset=%t and S=%q", match.Page, observedSortLabel(match), match.Panel, tc.reset, tc.s)
			}
		})
	}
	legacy := profile.Inventory(true)
	if match := legacy.Matcher.Match(frames["reset"]); !legacy.RequirePanelRangeProof || match.Panel == nil || !match.Panel.Range.resetConfirmed() {
		t.Fatalf("the legacy scroll matcher lost native range proof: %+v", match.Panel)
	}

	// The residual and S-only crops have the same active funnel. Putting
	// it alongside the clear S chip still cannot establish reset or select S.
	reset := frames["reset"]
	funnel := inv.Layout.panelRange.funnel.At
	draw.Draw(reset.Image, funnel, frames["residual"].Image, funnel.Min, draw.Src)
	if got := inv.Matcher.Match(reset).Panel.Range; got.ResetVerified || got.RarityS != FilterClear {
		t.Fatalf("gray S with an active funnel = %+v", got)
	}
	selected := frames["s-only"]
	probes := inv.Layout.panelRange
	other := probes.otherFills[0].At
	draw.Draw(selected.Image, other, selected.Image, probes.sFill.At.Min, draw.Src)
	if got := inv.Matcher.Match(selected).Panel.Range; got.raritySSelected() {
		t.Fatalf("confirmed the S-only step with another rarity selected: %+v", got)
	}
}

func TestWindowsLocalPanelRangeSingleVisitReplay(t *testing.T) {
	profile := windowsNativeRangeProfile(t)
	frames := loadWindowsPanelFrames(t, profile.Layout)
	// The existing native warehouse fixture supplies complete page pixels
	// for the close. The recorded panel crops provide the three open states.
	data, err := os.ReadFile("testdata/winlocal/win-1920x1080.png")
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	warehouse := image.NewRGBA(img.Bounds())
	draw.Draw(warehouse, warehouse.Bounds(), img, img.Bounds().Min, draw.Src)
	closed := platform.Frame{ID: 1, Geometry: 1, Image: warehouse, Content: profile.Layout.Content}
	for _, tc := range []struct {
		name, afterReset, afterS string
		staleS                   bool
		clicks                   int
		failure                  string
	}{
		{name: "verified", afterReset: "reset", afterS: "s-only", clicks: 4},
		{name: "reset had no effect", afterReset: "residual", clicks: 2, failure: "reset could not be verified"},
		{name: "S click had no effect", afterReset: "reset", afterS: "reset", clicks: 3, failure: "S selection could not be verified"},
		{name: "only stale S frames", afterReset: "reset", afterS: "s-only", staleS: true, clicks: 3, failure: "S selection could not be verified"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inv := profile.Inventory(false)
			inv.MaxRetries = -1
			steps := []platform.Frame{frames["residual"], frames[tc.afterReset], frames[tc.afterS], closed}
			points := []image.Point{image.Pt(304, 52), image.Pt(1430, 1030), image.Pt(1426, 604), image.Pt(1805, 49)}
			var ops []image.Point
			var finished time.Time
			samples, frameID := 0, closed.ID
			started := time.Now()
			view := platformfake.NewSession()
			view.DispatchFn = func(op platform.InputOp) platform.InputResult {
				ops = append(ops, op.At)
				finished = started.Add(time.Duration(len(ops)) * time.Second)
				samples = 0
				return platform.InputResult{Status: platform.InputPosted, FinishedAt: finished}
			}
			view.FrameFn = func(ctx context.Context, req platform.FrameRequest) (platform.Frame, error) {
				if len(ops) == 0 || len(ops) > tc.clicks {
					return platform.Frame{}, fmt.Errorf("unexpected replay request after %d operations", len(ops))
				}
				if samples == 3 {
					return platform.Frame{}, context.DeadlineExceeded
				}
				samples++
				frameID++
				frame := steps[len(ops)-1]
				frame.ID = frameID
				frame.ArrivedAt = finished.Add(time.Duration(samples) * time.Millisecond)
				if tc.staleS && len(ops) == 3 {
					frame.ArrivedAt = frame.ArrivedAt.Add(-10 * time.Millisecond)
				} else if !frame.ArrivedAt.After(req.After) {
					return platform.Frame{}, fmt.Errorf("replay frame predates the requested cursor")
				}
				return frame, nil
			}
			run := &inventoryRun{inv: inv, view: view, geo: 1, position: 3, lastFrame: closed, listMatch: inv.Matcher.Match(closed)}
			err := runScriptedSession(t, run, func(ctx context.Context, run *inventoryRun) error { return run.resetFilters(ctx) })
			if !slices.Equal(ops, points[:tc.clicks]) {
				t.Fatalf("panel operations %v, want %v", ops, points[:tc.clicks])
			}
			proof := run.rangeProof
			if tc.failure != "" {
				incomplete, ok := asPositionIncomplete(err)
				if !ok || incomplete.stage != "filter" || !strings.Contains(err.Error(), tc.failure) || proof.Verified || proof.ClosedFrameID != 0 {
					t.Fatalf("unproved filter preparation = %v, proof %+v", err, proof)
				}
				return
			}
			if err != nil || !proof.Verified || !proof.Reset.EffectVerified || !proof.SelectedS.EffectVerified ||
				proof.ClosedFrameID != 13 || proof.ClosedFunnel != FilterActive || run.inv.scope.SortLabel != sortLabelRarity {
				t.Fatalf("single-visit range proof = %+v, sort %q, error %v", proof, run.inv.scope.SortLabel, err)
			}
		})
	}
}
