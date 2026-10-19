package collect_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/game/zzz/collect"
	"github.com/LoTwT/youkai/internal/platform"
	"github.com/LoTwT/youkai/internal/platform/platformfake"
)

// The field replays are opt-in: the recorded raw-stream frames contain
// account information and unrelated UI, so they live outside the
// repository. Set YOUKAI_M2_SAMPLES to the sample round's directory to run
// the same matcher on the unmasked sources; the checked-in fixtures are
// the reduced form that always runs.

func sampleRoot(t *testing.T) string {
	t.Helper()
	root := os.Getenv("YOUKAI_M2_SAMPLES")
	if root == "" {
		t.Skip("YOUKAI_M2_SAMPLES is not set; the field-frame replay needs the recorded samples")
	}
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		t.Fatalf("YOUKAI_M2_SAMPLES=%q is not a directory", root)
	}
	return root
}

func loadSourcePNG(t *testing.T, p string) platform.Frame {
	t.Helper()
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read source frame %s: %v", p, err)
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("decode source frame %s: %v", p, err)
	}
	b := img.Bounds()
	rgba := image.NewRGBA(b)
	draw.Draw(rgba, b, img, b.Min, draw.Src)
	return platform.Frame{
		Content: collect.MacCloudLayout().Content,
		Image:   rgba,
	}
}

func loadSourceFrame(t *testing.T, root, dir string) platform.Frame {
	t.Helper()
	f := loadSourcePNG(t, filepath.Join(root, dir, "frame-0001.png"))
	f.ID = 1
	f.Geometry = 1
	return f
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// TestReplayFieldSamples runs the calibrated matcher over every recorded
// source frame in the fixture table and the measured scroll/detail pairs.
// The fixtures prove the same verdicts on the reduced pixels; this replay
// proves the reduction did not change them.
func TestReplayFieldSamples(t *testing.T) {
	root := sampleRoot(t)
	m := macCloudMatcher()
	for _, row := range loadManifest(t) {
		row := row
		t.Run(row.name, func(t *testing.T) {
			p := filepath.Join(root, filepath.FromSlash(row.source))
			raw, err := os.ReadFile(p)
			if err != nil {
				t.Fatalf("read source frame %s: %v", p, err)
			}
			sum := sha256.Sum256(raw)
			if got := hex.EncodeToString(sum[:]); got != row.sha256 {
				t.Fatalf("source frame %s has SHA-256 %s, want %s", p, got, row.sha256)
			}
			assertMatchExpectations(t, m, loadSourceFrame(t, root, row.name), row.expect)
		})
	}

	for _, tc := range scrollCases {
		t.Run("scroll/"+tc.from+"->"+tc.to, func(t *testing.T) {
			before := loadSourceFrame(t, root, tc.from)
			after := loadSourceFrame(t, root, tc.to)
			assertScrollCase(t, m, before, after, tc)
		})
	}
	for _, tc := range detailCases {
		t.Run("detail/"+tc.name, func(t *testing.T) {
			got := m.CompareDetail(loadSourceFrame(t, root, tc.a), loadSourceFrame(t, root, tc.b))
			if got != tc.want {
				t.Fatalf("comparison %v, want %v", got, tc.want)
			}
		})
	}

	replayMADDistribution(t, root, m)
}

// replayMADDistribution measures the detail MAD distribution the 4b-1
// thresholds were chosen from. The same-content pairs are each evidence
// directory's frame-0000/frame-0001 (both captured after the click); the
// adjacent pairs are the orthogonal neighbors inside the six complete
// 5×4 viewports, using each cell's settled frame-0001. The key safety
// invariant is that no same-content pair crosses DetailSameMax; known
// low-MAD adjacent near-duplicates are reported, never asserted away.
func replayMADDistribution(t *testing.T, root string, m *collect.PixelMatcher) {
	t.Helper()
	same := sameContentMADs(t, root, m)
	if len(same) != 236 {
		t.Fatalf("same-content pairs: %d, want 236", len(same))
	}
	unsafe := 0
	for _, v := range same {
		if v > m.C.DetailSameMax {
			unsafe++
		}
	}
	if unsafe != 0 {
		t.Fatalf("%d same-content pairs exceed DetailSameMax %.1f; the Same band would falsely admit them",
			unsafe, m.C.DetailSameMax)
	}
	logMADStats(t, "same-content frame-0000/0001", same, m.C.DetailSameMax, m.C.DetailDifferentMin)

	adjacent, low, band := adjacentViewportMADs(t, root, m)
	if len(adjacent) != 186 {
		t.Fatalf("adjacent pairs: %d, want 186", len(adjacent))
	}
	logMADStats(t, "orthogonally adjacent cells in the six viewports", adjacent, m.C.DetailSameMax, m.C.DetailDifferentMin)
	if len(low) > 0 {
		t.Logf("adjacent pairs below DetailSameMax (safe stops, not failures): %s", strings.Join(low, "; "))
	}
	if len(band) > 0 {
		t.Logf("adjacent pairs between the bands (safe stops, not failures): %s", strings.Join(band, "; "))
	}
}

func sameContentMADs(t *testing.T, root string, m *collect.PixelMatcher) []float64 {
	t.Helper()
	dirs, err := filepath.Glob(filepath.Join(root, "s2-*"))
	if err != nil {
		t.Fatalf("list sample directories: %v", err)
	}
	sort.Strings(dirs)
	out := make([]float64, 0, 236)
	for _, dir := range dirs {
		a := filepath.Join(dir, "frame-0000.png")
		b := filepath.Join(dir, "frame-0001.png")
		if !fileExists(a) || !fileExists(b) {
			continue
		}
		value, ok := m.DetailMAD(loadSourcePNG(t, a), loadSourcePNG(t, b))
		if !ok {
			t.Fatalf("%s: DetailMAD is not comparable", dir)
		}
		out = append(out, value)
	}
	return out
}

func adjacentViewportMADs(t *testing.T, root string, m *collect.PixelMatcher) ([]float64, []string, []string) {
	t.Helper()
	groups := []string{
		"s2-s1-bot-cell",
		"s2-wL-top-cell",
		"s2-wBL-top-cell",
		"s2-wBR-top-cell",
		"s2-s5-top-cell",
		"s2-s6-top-cell",
	}
	out := make([]float64, 0, 186)
	var low, band []string
	for _, group := range groups {
		frames := make([]platform.Frame, 20)
		for r := 0; r < 5; r++ {
			for c := 0; c < 4; c++ {
				frames[r*4+c] = loadSourceFrame(t, root, fmt.Sprintf("%s-r%dc%d", group, r, c))
			}
		}
		add := func(i, j int, label string) {
			value, ok := m.DetailMAD(frames[i], frames[j])
			if !ok {
				t.Fatalf("%s: DetailMAD is not comparable", label)
			}
			out = append(out, value)
			switch {
			case value < m.C.DetailSameMax:
				low = append(low, fmt.Sprintf("%s %.3f", label, value))
			case value <= m.C.DetailDifferentMin:
				band = append(band, fmt.Sprintf("%s %.3f", label, value))
			}
		}
		for r := 0; r < 5; r++ {
			for c := 0; c < 4; c++ {
				i := r*4 + c
				if c < 3 {
					add(i, i+1, fmt.Sprintf("%s r%dc%d→r%dc%d", group, r, c, r, c+1))
				}
				if r < 4 {
					add(i, i+4, fmt.Sprintf("%s r%dc%d→r%dc%d", group, r, c, r+1, c))
				}
			}
		}
	}
	return out, low, band
}

func logMADStats(t *testing.T, label string, values []float64, sameMax, differentMin float64) {
	t.Helper()
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	var above, below, band int
	aboveMin := math.Inf(1)
	for _, v := range sorted {
		switch {
		case v > differentMin:
			above++
			if v < aboveMin {
				aboveMin = v
			}
		case v < sameMax:
			below++
		default:
			band++
		}
	}
	aboveMinText := "n/a"
	if above > 0 {
		aboveMinText = fmt.Sprintf("%.3f", aboveMin)
	}
	aboveSame := above + band
	t.Logf("%s: n=%d min=%.3f p50=%.3f p95=%.3f max=%.3f; >%.1f=%d (min %s) >%.1f=%d <%.1f=%d in-band=%d",
		label, len(sorted), sorted[0], quantile(sorted, 0.50), quantile(sorted, 0.95), sorted[len(sorted)-1],
		differentMin, above, aboveMinText, sameMax, aboveSame, sameMax, below, band)
}

func quantile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	// Nearest lower rank over n-1 intervals; this is the convention the
	// 4b-1 calibration note reports (p95 0.88 on the same pairs).
	index := int(p * float64(len(sorted)-1))
	return sorted[index]
}

// fieldReplay serves the recorded wheel-2 frames as the game's frames: the
// initial list until a click, then the clicked cell's recorded frame. It
// is the navigation-sequence replay T5 §6 asks for, with the pixel matcher
// judging every frame.
type fieldReplay struct {
	mu sync.Mutex

	layout  collect.Layout
	initial platform.Frame
	cells   []platform.Frame // slot i is cells[i]
	current int              // 0: the initial list; i+1: cells[i]
	nextID  uint64
	ops     []platform.InputOp
}

func newFieldReplay(t *testing.T, root string) *fieldReplay {
	t.Helper()
	l := collect.MacCloudLayout()
	r := &fieldReplay{
		layout:  l,
		initial: loadSourceFrame(t, root, "s2-62-wheelL-list"),
	}
	for slot := 0; slot < l.Rows*l.Columns; slot++ {
		name := fmt.Sprintf("s2-wL-top-cell-r%dc%d", slot/l.Columns, slot%l.Columns)
		r.cells = append(r.cells, loadSourceFrame(t, root, name))
	}
	return r
}

func (r *fieldReplay) observe() (platform.Observation, error) {
	return platform.Observation{At: time.Now(), Ready: true, Geometry: 1}, nil
}

func (r *fieldReplay) frame(ctx context.Context, req platform.FrameRequest) (platform.Frame, error) {
	if err := ctx.Err(); err != nil {
		return platform.Frame{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	base := r.initial
	if r.current > 0 {
		base = r.cells[r.current-1]
	}
	r.nextID++
	f := base
	f.ID = r.nextID
	f.ArrivedAt = time.Now()
	return f, nil
}

func (r *fieldReplay) dispatch(op platform.InputOp) platform.InputResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ops = append(r.ops, op)
	now := time.Now()
	if op.Kind == platform.InputClick {
		for slot, rect := range r.layout.Slots {
			if op.At.In(rect) {
				r.current = slot + 1
				break
			}
		}
	}
	return platform.InputResult{Status: platform.InputPosted, EventsPlanned: 2, EventsPosted: 2,
		StartedAt: now, FinishedAt: now}
}

func (r *fieldReplay) opsSnapshot() []platform.InputOp {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]platform.InputOp(nil), r.ops...)
}

// bottomMatcher fixes the end evidence the recorded cell frames do not
// carry: the 20 cells are all top-view frames, while the navigation under
// test ends at the last position. The rest of every verdict is the real
// matcher's.
type bottomMatcher struct {
	collect.Matcher
}

func (m bottomMatcher) Match(f platform.Frame) collect.PageMatch {
	pm := m.Matcher.Match(f)
	if pm.List != nil {
		pm.List.Bottom = true
	}
	return pm
}

// TestReplayViewportFromFieldFrames walks the recorded wheel-2 viewport:
// the initial list plus the 20 clicked cell frames, in reading order. The
// initial selection is admitted as an initial read; each later slot is a
// verified transition; the walk completes on the forced end evidence.
func TestReplayViewportFromFieldFrames(t *testing.T) {
	root := sampleRoot(t)
	replay := newFieldReplay(t, root)
	l := collect.MacCloudLayout()
	session := &platformfake.Session{}
	session.ObserveFn = replay.observe
	session.FrameFn = replay.frame
	session.DispatchFn = replay.dispatch
	h := &traversalHarness{
		t:       t,
		game:    &scriptedGame{layout: l},
		session: session,
		inv: &collect.Inventory{
			Layout:           l,
			Matcher:          bottomMatcher{Matcher: collect.NewPixelMatcher(l, collect.MacCloudCalibration())},
			ActionBudget:     120 * time.Millisecond,
			StableFrames:     3,
			DetailStableSpan: -1,
			MaxRetries:       2,
			RecheckEvery:     time.Millisecond,
		},
	}
	h.Recognizer = fastRecognizer{}
	h.Capacity = 4
	h.start()
	h.endCompleted()

	obs := h.observations()
	if len(obs) != 20 {
		t.Fatalf("admitted %d, want 20: %v", len(obs), positions(obs))
	}
	for i, o := range obs {
		if o.Viewport != 0 || o.Row != i/4 || o.Column != i%4 {
			t.Fatalf("observation %d at %v, want slot %d", i+1, [3]int{o.Viewport, o.Row, o.Column}, i)
		}
		wantAssoc := collect.AssociationTransition
		if i == 0 {
			wantAssoc = collect.AssociationInitial
		}
		if o.Association != wantAssoc {
			t.Fatalf("observation %d association %s, want %s", i+1, o.Association, wantAssoc)
		}
		if o.Image == nil || o.Evidence == "" {
			t.Fatalf("observation %d lacks its capture or evidence", i+1)
		}
	}
	ops := replay.opsSnapshot()
	if clicks := countOps(ops, platform.InputClick, image.Point{}, image.Point{}); clicks != 19 {
		t.Fatalf("%d clicks, want 19 (slot 0 is the initial read)", clicks)
	}
	if drags := countOps(ops, platform.InputDrag, l.ScrollFrom, l.ScrollTo); drags != 1 {
		t.Fatalf("%d advance drags, want the bottom confirmation", drags)
	}
	if drags := countOps(ops, platform.InputDrag, l.TopFrom, l.TopTo); drags != 1 {
		t.Fatalf("%d top drags, want 1", drags)
	}
}
