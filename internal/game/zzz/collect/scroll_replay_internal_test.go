package collect

import (
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/platform"
	"github.com/LoTwT/youkai/internal/scan"
)

// The field replays are opt-in, like the M2 sample replay: the recorded
// frames contain account information and unrelated UI, so they live outside
// the repository. Set YOUKAI_SCROLL_REPLAY to the "Youkai Dev" log root
// (for example ~/Library/Logs/Youkai Dev) to replay the 2026-10-28/30 scroll
// failure evidence through the stage 2 judgment; the checked-in masked
// fixtures always cover the recorded M2 scrolls (see the collect_test
// replay).

// scrollReplayRoot returns the configured field-evidence root, skipping the
// replay when it is not set.
func scrollReplayRoot(t *testing.T) string {
	t.Helper()
	root := os.Getenv("YOUKAI_SCROLL_REPLAY")
	if root == "" {
		t.Skip("YOUKAI_SCROLL_REPLAY is not set; the field scroll replay needs the recorded diagnostics")
	}
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		t.Fatalf("YOUKAI_SCROLL_REPLAY=%q is not a directory", root)
	}
	return root
}

// replayedWaitSequence is one diagnostic manifest directory's frames: the
// decoded manifest plus each frame's reconstructed full-frame image.
type replayedWaitSequence struct {
	manifest diagnosticManifest
	dir      string
	frames   []platform.Frame
}

// loadReplayedSequence reads one dump directory: every frame's crop is
// pasted back onto a full-frame canvas at the manifest's region offset and
// the warehouse probes the crop does not carry (funnel, panel marks, wheel
// arcs) are painted from the manifest's recorded facts, so the real pixel
// matcher reads a verifiable warehouse list whose list viewport, selection
// borders and scrollbar are the field pixels. The painted probes sit
// outside the scroll matcher's regions; they exist so the traversal's page
// verdicts hold, not to influence the scroll judgment.
func loadReplayedSequence(t *testing.T, dir string) *replayedWaitSequence {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest diagnosticManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if len(manifest.Frames) == 0 {
		t.Fatalf("manifest %s has no frames", dir)
	}
	layout := MacCloudLayout()
	region := image.Rect(manifest.Region.X, manifest.Region.Y,
		manifest.Region.X+manifest.Region.Width, manifest.Region.Y+manifest.Region.Height)
	origin := time.Now().Add(-layout.ScrollDuration)
	seq := &replayedWaitSequence{manifest: manifest, dir: dir}
	for i := range manifest.Frames {
		entry := &manifest.Frames[i]
		if entry.File == "" {
			// A frame the retention dropped: the sequence replays what the
			// wait retained, exactly as the failure dump did.
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.File))
		if err != nil {
			t.Fatalf("read frame crop %s: %v", entry.File, err)
		}
		crop, err := png.Decode(strings.NewReader(string(raw)))
		if err != nil {
			t.Fatalf("decode frame crop %s: %v", entry.File, err)
		}
		img := image.NewRGBA(image.Rectangle{Max: layout.Image})
		draw.Draw(img, region, crop, crop.Bounds().Min, draw.Src)
		paintWarehouseProbes(img, layout, manifest.Position)
		arrival := origin
		if entry.ArrivedMs != 0 || entry.Frame == manifest.BeforeFrame {
			arrival = origin.Add(time.Duration(entry.ArrivedMs * float64(time.Millisecond)))
		}
		seq.frames = append(seq.frames, platform.Frame{
			ID:        entry.Frame,
			Geometry:  1,
			ArrivedAt: arrival,
			Reused:    entry.Reused,
			Image:     img,
			Content:   layout.Content,
		})
	}
	if len(seq.frames) == 0 {
		t.Fatalf("manifest %s retained no frame crops", dir)
	}
	return seq
}

// paintWarehouseProbes paints the warehouse-page probes a scroll-trace crop
// does not carry: the panel marks closed, the funnel active (the recorded
// scans ran under the S filter) and the wheel selection on the recorded
// position. They are all outside the list viewport and the scrollbar track,
// so MatchScroll's evidence still comes from the field pixels alone.
func paintWarehouseProbes(img *image.RGBA, layout Layout, position int) {
	fill := func(r image.Rectangle, c color.RGBA) {
		r = r.Intersect(img.Bounds())
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				img.SetRGBA(x, y, c)
			}
		}
	}
	for _, probe := range layout.PanelOpen {
		fill(probe.At, probe.Colors["closed"])
	}
	fill(layout.Funnel.At, layout.Funnel.Colors["active"])
	for i, center := range layout.Wheel {
		for _, arc := range layout.WheelArcs {
			if i+1 == position {
				fill(arc.Add(center), color.RGBA{R: 224, G: 208, B: 67, A: 255})
			} else {
				fill(arc.Add(center), color.RGBA{A: 255})
			}
		}
	}
}

// replaySession serves a recorded wait's frames in order under a synthetic
// clock anchored before the drag, then blocks like a stalled stream until
// the request's deadline. The frame contents and order are the field's; the
// wall clock is compressed (frames are served on request), which only
// shortens the wait's idle spans — every comparison, verdict and streak the
// stage 2 judgment makes runs on the real pixels and the recorded arrival
// offsets.
type replaySession struct {
	seq     *replayedWaitSequence
	next    int
	observe int
	drags   int
}

func (s *replaySession) Observe() (platform.Observation, error) {
	s.observe++
	return platform.Observation{Ready: true, Geometry: 1}, nil
}

func (s *replaySession) Dispatch(op platform.InputOp) platform.InputResult {
	s.drags++
	now := time.Now()
	return platform.InputResult{Status: platform.InputPosted,
		StartedAt: now, FinishedAt: now}
}

func (s *replaySession) Frame(ctx context.Context, req platform.FrameRequest) (platform.Frame, error) {
	if err := ctx.Err(); err != nil {
		return platform.Frame{}, err
	}
	for s.next < len(s.seq.frames) {
		f := s.seq.frames[s.next]
		if !f.ArrivedAt.After(req.After) {
			s.next++
			continue
		}
		s.next++
		return f, nil
	}
	<-ctx.Done()
	return platform.Frame{}, ctx.Err()
}

// newReplayScrollRun builds one scroll action over the recorded sequence.
func newReplayScrollRun(t *testing.T, seq *replayedWaitSequence, m Matcher, dir string) (*inventoryRun, *replaySession) {
	t.Helper()
	layout := MacCloudLayout()
	session := &replaySession{seq: seq}
	run := &inventoryRun{
		inv: &Inventory{
			Layout:        layout,
			Matcher:       m,
			DiagnosticDir: dir,
		},
		view:     session,
		geo:      1,
		position: seq.manifest.Position,
		viewport: seq.manifest.Viewport,
		visited:  make([]bool, len(layout.Slots)),
	}
	return run, session
}

// fieldSequenceDirs are the 2026-10-30 stage 1 retest's two field failure
// sequences (25 frames each, with per-frame verdicts and margins): the
// tuning and regression corpus the stage 2 judgment must turn into a
// correct displacement plus settle, or fail closed on.
var fieldSequenceDirs = []string{
	"probes/20261030-stage1-retest/attempt1/20261030-123950734-pos1-vp4-scroll-lost-attempt1",
	"probes/20261030-stage1-retest/attempt2/20261030-125302638-pos5-vp11-scroll-lost-attempt1",
}

// TestScrollReplayFieldSequencesSettleUnderStage2 replays both 2026-10-30
// field failure sequences through the real scroll action with the stage 2
// matcher: the recorded four-row displacement must verify (selection box
// r4→r0 plus the normalized feature) and the settle must confirm on the
// candidate-relative normalized correlation — the exact waits that ended as
// positionLost in the field because the cloud noise kept the zero-shift MAD
// above the stationary threshold. A committed scroll also writes its
// frame-time dump.
func TestScrollReplayFieldSequencesSettleUnderStage2(t *testing.T) {
	root := scrollReplayRoot(t)
	for _, rel := range fieldSequenceDirs {
		t.Run(filepath.Base(rel), func(t *testing.T) {
			seq := loadReplayedSequence(t, filepath.Join(root, rel))
			dir := t.TempDir()
			m := NewScrollEvidenceMatcher(NewPixelMatcher(MacCloudLayout(), MacCloudCalibration()))
			run, session := newReplayScrollRun(t, seq, m, dir)

			if err := runScriptedScroll(t, run); err != nil {
				t.Fatalf("the stage 2 judgment must verify and settle the recorded scroll: %v", err)
			}
			if run.lastScrollRows != 4 {
				t.Fatalf("committed rows %d, want the recorded four-row displacement", run.lastScrollRows)
			}
			if session.drags != 1 {
				t.Fatalf("%d drags, want the single recorded drag", session.drags)
			}
			dumps := diagnosticDumpDirs(t, dir)
			if len(dumps) != 1 || !strings.Contains(filepath.Base(dumps[0]), "-scroll-ok") {
				t.Fatalf("the committed scroll wrote %v, want one scroll-ok frame-time dump", dumps)
			}
			manifest := readDumpManifest(t, dumps[0])
			if manifest.SettledFrame == 0 || manifest.MotionFrame == 0 || manifest.SettleNeeded < 2 {
				t.Fatalf("manifest motion=%d settled=%d of %d, want the timing records for calibration",
					manifest.MotionFrame, manifest.SettledFrame, manifest.SettleNeeded)
			}
		})
	}
}

// TestScrollReplayFieldSequencesStillFailUnderLegacy is the sequence-level
// ablation: the same frames through the pre-stage-2 matcher (the
// YOUKAI_SCROLL_MATCHER=legacy judgment) must reproduce the recorded field
// failure — the four-row displacement candidates but no settle confirmation
// within the budget, ending as the unsettled positionLost that stopped the
// 2026-10-30 runs. The stage 2 evidence, not some incidental change, is
// what turns the outcome around.
func TestScrollReplayFieldSequencesStillFailUnderLegacy(t *testing.T) {
	root := scrollReplayRoot(t)
	for _, rel := range fieldSequenceDirs {
		t.Run(filepath.Base(rel), func(t *testing.T) {
			seq := loadReplayedSequence(t, filepath.Join(root, rel))
			m := NewPixelMatcher(MacCloudLayout(), MacCloudCalibration())
			run, _ := newReplayScrollRun(t, seq, m, "")

			// The wait itself runs the whole scroll budget before the
			// unsettled stop, so the scripted-step bound must exceed it.
			err := runScriptedSessionTimeout(t, run, func(ctx context.Context, run *inventoryRun) error {
				return run.scroll(ctx)
			}, 20*time.Second)
			var fault *scan.FaultError
			if !errors.As(err, &fault) || fault.Code != CodePositionLost {
				t.Fatalf("legacy scroll error %v, want the recorded unsettled positionLost", err)
			}
			if !strings.Contains(err.Error(), "did not settle") {
				t.Fatalf("legacy scroll error %v, want the unsettled-settle stop", err)
			}
		})
	}
}

// fieldAnchorPairs are the 2026-10-28 original failures' before/after crop
// pairs (the F5 four-row anchor evidence): the pre-drag frame selects r4c3
// and the unsettled candidate frame r0c3.
var fieldAnchorPairs = [][2]string{
	{"diagnostics/20261028-110803218-pos1-vp4-scroll-lost-before-f5065.png", "diagnostics/20261028-110803218-pos1-vp4-scroll-lost-after-f5166.png"},
	{"diagnostics/20261028-111755048-pos1-vp4-scroll-lost-before-f5387.png", "diagnostics/20261028-111755048-pos1-vp4-scroll-lost-after-f5503.png"},
}

// loadReplayedPair loads one before/after crop pair as full frames with the
// warehouse probes painted, for a single MatchScroll comparison.
func loadReplayedPair(t *testing.T, root string, before, after string) (platform.Frame, platform.Frame) {
	t.Helper()
	layout := MacCloudLayout()
	region := layout.ListViewport.Union(layout.ScrollTrack)
	load := func(name string, id uint64) platform.Frame {
		raw, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatalf("read anchor crop %s: %v", name, err)
		}
		crop, err := png.Decode(strings.NewReader(string(raw)))
		if err != nil {
			t.Fatalf("decode anchor crop %s: %v", name, err)
		}
		img := image.NewRGBA(image.Rectangle{Max: layout.Image})
		draw.Draw(img, region, crop, crop.Bounds().Min, draw.Src)
		paintWarehouseProbes(img, layout, 1)
		return platform.Frame{ID: id, Geometry: 1, Image: img, Content: layout.Content}
	}
	return load(before, 1), load(after, 2)
}

// TestScrollReplayFieldAnchorPairsVerifyDisplacement replays the 2026-10-28
// before/after pairs through both matchers: the pre-stage-2 matcher already
// reported the four-row displacement (the field log's rows=4 matches=4) but
// never settled; the stage 2 matcher verifies the same four rows with the
// selection box as the primary evidence and the normalized feature at an
// error level the legacy absolute cap rejected.
func TestScrollReplayFieldAnchorPairsVerifyDisplacement(t *testing.T) {
	root := scrollReplayRoot(t)
	layout := MacCloudLayout()
	stage2 := NewScrollEvidenceMatcher(NewPixelMatcher(layout, MacCloudCalibration()))
	legacy := NewPixelMatcher(layout, MacCloudCalibration())
	for _, pair := range fieldAnchorPairs {
		t.Run(filepath.Base(pair[0]), func(t *testing.T) {
			before, after := loadReplayedPair(t, root, pair[0], pair[1])
			sm := stage2.MatchScroll(before, after)
			if !sm.Displaced || sm.Rows != 4 || len(sm.Matches) != (layout.Rows-4)*layout.Columns {
				t.Fatalf("stage 2 verdict %+v, want the four-row displacement (%s)", sm, sm.Evidence)
			}
			if sm.Metrics == nil || sm.Metrics.SelectionBefore != layout.Rows*layout.Columns-1 ||
				sm.Metrics.SelectionAfter != layout.Columns-1 {
				t.Fatalf("stage 2 metrics %+v, want the r4c3 → r0c3 selection anchors", sm.Metrics)
			}
			old := legacy.MatchScroll(before, after)
			if !old.Displaced || old.Rows != 4 {
				t.Fatalf("legacy verdict %+v, want the field log's rows=4 matches=4 displacement", old)
			}
		})
	}
}

// TestScrollTimingDefaultAndFallback pins the Inventory's timing plumbing:
// nil takes the macOS cloud profile and an invalid override falls back to
// it instead of guessing.
func TestScrollTimingDefaultAndFallback(t *testing.T) {
	want := MacCloudScrollTiming()
	if got := (&Inventory{}).scrollTiming(); got != want {
		t.Fatalf("nil timing = %+v, want the macOS cloud profile", got)
	}
	bad := ScrollTiming{MotionStartBudget: -1}
	if got := (&Inventory{ScrollTiming: &bad}).scrollTiming(); got != want {
		t.Fatalf("invalid override = %+v, want the fallback to the macOS cloud profile", got)
	}
	inv := &Inventory{ScrollTiming: &ScrollTiming{MotionStartBudget: time.Second, SettleBudget: 2 * time.Second, SettleObservations: 3}}
	if got := inv.scrollTiming(); got.SettleObservations != 3 || got.SettleBudget != 2*time.Second {
		t.Fatalf("valid override = %+v, want the configured profile", got)
	}
}
