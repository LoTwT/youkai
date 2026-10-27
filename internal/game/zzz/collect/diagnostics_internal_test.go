package collect

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LoTwT/youkai/internal/platform"
)

// TestScrollFailureDumpWritesBoundedCrops pins the field diagnostics: a
// failed scroll writes the before/after crops of the list and scrollbar
// region, a walk writes at most maxDiagnosticDumpsPerRun pairs, and an
// empty directory setting writes nothing.
func TestScrollFailureDumpWritesBoundedCrops(t *testing.T) {
	layout := MacCloudLayout()
	frame := func(id uint64) platform.Frame {
		return platform.Frame{ID: id, Image: image.NewRGBA(image.Rect(0, 0, 1280, 1000))}
	}
	dir := t.TempDir()
	run := &inventoryRun{inv: &Inventory{Layout: layout, DiagnosticDir: dir}, position: 1, viewport: 4}

	run.dumpScrollFrames("scroll-lost", frame(10), frame(11))
	files, err := filepath.Glob(filepath.Join(dir, "*.png"))
	if err != nil || len(files) != 2 {
		t.Fatalf("one dump wrote %v (err %v), want a before and an after crop", files, err)
	}
	want := layout.ListViewport.Union(layout.ScrollTrack)
	for _, path := range files {
		if !strings.Contains(filepath.Base(path), "-pos1-vp4-scroll-lost-") {
			t.Fatalf("crop name %q does not name the position, viewport and outcome", path)
		}
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		cfg, err := png.DecodeConfig(f)
		_ = f.Close()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Width != want.Dx() || cfg.Height != want.Dy() {
			t.Fatalf("crop is %dx%d, want the %v region", cfg.Width, cfg.Height, want)
		}
	}

	for i := 0; i < maxDiagnosticDumpsPerRun+3; i++ {
		run.dumpScrollFrames("scroll-lost", frame(uint64(100+2*i)), frame(uint64(101+2*i)))
	}
	files, _ = filepath.Glob(filepath.Join(dir, "*.png"))
	if len(files) != 2*maxDiagnosticDumpsPerRun {
		t.Fatalf("a walk wrote %d crops, want the per-run bound of %d pairs", len(files), maxDiagnosticDumpsPerRun)
	}

	off := &inventoryRun{inv: &Inventory{Layout: layout}}
	off.dumpScrollFrames("scroll-lost", frame(1), frame(2))
}

// TestScrollFailureDumpSkipsNilImages pins the typed-nil guard: frames
// without an image are reported and skipped instead of panicking in
// Bounds, and no file is written.
func TestScrollFailureDumpSkipsNilImages(t *testing.T) {
	dir := t.TempDir()
	run := &inventoryRun{inv: &Inventory{Layout: MacCloudLayout(), DiagnosticDir: dir}, position: 1, viewport: 4}
	run.dumpScrollFrames("scroll-lost", platform.Frame{ID: 10}, platform.Frame{ID: 11})
	if files, err := filepath.Glob(filepath.Join(dir, "*.png")); err != nil || len(files) != 0 {
		t.Fatalf("nil images wrote %v (err %v), want no files", files, err)
	}
}

// TestPruneDiagnosticsKeepsNewest pins the directory bound: only the
// newest files by their timestamped names survive.
func TestPruneDiagnosticsKeepsNewest(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 5; i++ {
		name := filepath.Join(dir, fmt.Sprintf("2026102%d-000000000-x.png", i))
		if err := os.WriteFile(name, []byte("png"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	pruneDiagnostics(dir, 2)
	files, _ := filepath.Glob(filepath.Join(dir, "*.png"))
	if len(files) != 2 || !strings.Contains(files[0], "20261023") || !strings.Contains(files[1], "20261024") {
		t.Fatalf("prune kept %v, want the two newest", files)
	}
}
