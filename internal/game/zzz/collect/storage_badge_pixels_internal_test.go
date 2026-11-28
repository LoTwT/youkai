package collect

import (
	"image"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/platform"
)

func TestFastCardBadgeScores(t *testing.T) {
	rng := rand.New(rand.NewSource(5))
	img := image.NewRGBA(image.Rect(5, 7, 53, 45))
	window := image.Rect(8, 9, 42, 35)
	sets := StorageLayout().CardPositions
	// Also exercise several mask sizes, thresholds and a fixed-offset mask
	// that can read past the edge of the window.
	sets[0] = glyphSet{minBrightness: 85, masks: []glyphMask{
		{rows: []string{"##", ".#"}},
		{rows: []string{"#..#", ".##."}},
	}}
	for sample := range 12 {
		if sample > 0 {
			_, _ = rng.Read(img.Pix)
		}
		pixels := cardBadgePixels{image: img, window: window}
		for index, set := range sets {
			var offsets []image.Point
			got, ok := pixels.best(set, &offsets)
			want, _, wantOK := set.bestMatch(img, window)
			wantOffsets, _ := set.bestOffsets(img, window)
			if ok != wantOK || got != want || !reflect.DeepEqual(offsets, wantOffsets) {
				t.Fatalf("sample %d set %d score/offsets differ: %v versus %v", sample, index+1, got, want)
			}
			for _, point := range append(offsets, window.Max.Sub(image.Pt(2, 2))) {
				got, ok := pixels.bestAt(set, point)
				want, wantOK := set.bestAt(img, point)
				if ok != wantOK || got != want {
					t.Fatalf("sample %d set %d score at %v: %v versus %v", sample, index+1, point, got, want)
				}
			}
		}
	}
}

func TestFastCardBadgeStorageReplay(t *testing.T) {
	root := os.Getenv("YOUKAI_TIMING_ROSTER")
	if root == "" {
		t.Skip("private roster is not configured")
	}
	paths, err := filepath.Glob(filepath.Join(root, "view-????.png"))
	if err != nil || len(paths) < 112 {
		t.Fatalf("roster paths=%d error=%v", len(paths), err)
	}
	rosterCount := len(paths)
	if post := os.Getenv("YOUKAI_TIMING_POST"); post != "" {
		controls, err := filepath.Glob(filepath.Join(post, "frame-????.png"))
		if err != nil || len(controls) == 0 {
			t.Fatalf("scoped controls=%d error=%v", len(controls), err)
		}
		paths = append(paths, controls...)
	}
	l := StorageLayout()
	a, b := NewPixelMatcher(l, StorageCalibration()), NewPixelMatcher(l, StorageCalibration())
	a.fastCardBadges, b.fastCardBadges = false, true
	check := func(f platform.Frame) {
		t.Helper()
		want, got := a.storageMatchDetailed(f), b.storageMatchDetailed(f)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("detailed match differs on frame %d: %s versus %s", f.ID, got.Evidence, want.Evidence)
		}
		want, got = a.publicStorageMatch(f, want), b.publicStorageMatch(f, got)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("public match differs on frame %d: %s versus %s", f.ID, got.Evidence, want.Evidence)
		}
	}
	for i, path := range paths {
		a.partitionOneBadge, b.partitionOneBadge = i >= rosterCount, i >= rosterCount
		check(offlineLoadedFrame(t, path, uint64(i+1), l))
	}
	check(offlineWeak(l))
	t.Logf("%d retained full frames plus a synthetic weak-selection frame matched exactly", len(paths))
}

func BenchmarkFastCardBadgePageMatch(b *testing.B) {
	root := os.Getenv("YOUKAI_TIMING_ROSTER")
	if root == "" {
		b.Skip("private roster is not configured")
	}
	l := StorageLayout()
	f := offlineLoadedFrame(b, filepath.Join(root, "view-0000.png"), 1, l)
	for _, arm := range []struct {
		name string
		fast bool
	}{{"Original", false}, {"Packed", true}} {
		b.Run(arm.name, func(b *testing.B) {
			m := NewPixelMatcher(l, StorageCalibration())
			m.fastCardBadges = arm.fast
			b.ReportAllocs()
			for b.Loop() {
				if m.Match(f).Page != PageDriveStorage {
					b.Fatal("expected drive storage")
				}
			}
		})
	}
}

func TestFastBeforeClickOnlyUsesPrivateCopy(t *testing.T) {
	l := StorageLayout()
	base := NewPixelMatcher(l, StorageCalibration())
	base.fastCardBadges, base.fastBeforeClick = false, true
	run := &inventoryRun{
		inv:      &Inventory{Layout: l, Matcher: NewScrollEvidenceMatcher(base)},
		abTiming: &storageABTiming{started: time.Now(), stage: "pre_click", frames: make([]storageABFrameTiming, 0, 2)},
	}
	f := storageReviewPixelFrame(l, 1, 10, 300, 323)
	f.ArrivedAt = time.Now()
	want := base.Match(f)
	got, err := run.preClickFrameMatcher()(f)
	if err != nil || !reflect.DeepEqual(got, want) || base.fastCardBadges {
		t.Fatalf("pre-click fast path changed its verdict or the session matcher: %v", err)
	}
	f.ID, f.ArrivedAt = 2, f.ArrivedAt.Add(time.Millisecond)
	run.abTiming.stage = "normal_detail"
	got, err = run.matchFrame(f)
	if err != nil || got.Page != want.Page || base.fastCardBadges {
		t.Fatalf("normal frame did not keep the original path: %v", err)
	}
	samples := run.abTiming.frames
	if len(samples) != 2 || !samples[0].FastBeforeClick || samples[1].FastBeforeClick {
		t.Fatal("expected fast matching on just the pre-click observation")
	}
}
func TestPacedStorageDetailRequestPreservesFreshnessFloor(t *testing.T) {
	after := time.Unix(7, 0)
	if got := pacedStorageDetailRequest(after, time.Time{}).After; !got.Equal(after) {
		t.Fatalf("the first request should use the original floor, got %v", got)
	}
	previous := after.Add(time.Millisecond)
	want := previous.Add(storageDetailCaptureHold)
	if got := pacedStorageDetailRequest(after, previous).After; !got.Equal(want) {
		t.Fatalf("subsequent request floor = %v, want %v", got, want)
	}
	later := want.Add(time.Millisecond)
	if got := pacedStorageDetailRequest(later, previous).After; !got.Equal(later) {
		t.Fatalf("a stricter freshness floor was lost: %v", got)
	}
}
