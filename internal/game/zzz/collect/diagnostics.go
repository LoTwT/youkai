package collect

import (
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/LoTwT/youkai/internal/platform"
)

// Bounds of the scroll failure dumps: a walk stops on its first failed
// scroll, so a handful per walk covers retries and the bottom
// confirmation; the directory keeps the newest files across walks so
// repeated field runs never grow it without bound.
const (
	maxDiagnosticDumpsPerRun = 6
	maxDiagnosticFiles       = 48
)

// dumpScrollFrames writes the list region of the frames before and after
// a failed scroll to the diagnostic directory and logs where they went.
// The region spans the scrollbar track and the list viewport, which is
// what the scroll verdict and the thumb evidence read. Every failure here
// is logged and swallowed: diagnostics must never change the walk.
func (run *inventoryRun) dumpScrollFrames(kind string, before, after platform.Frame) {
	dir := run.inv.DiagnosticDir
	if dir == "" || run.diagnosticDumps >= maxDiagnosticDumpsPerRun {
		return
	}
	run.diagnosticDumps++
	if err := os.MkdirAll(dir, 0o700); err != nil {
		log.Printf("youkai: collect: diagnostics: create %s: %v", dir, err)
		return
	}
	region := run.inv.Layout.ListViewport.Union(run.inv.Layout.ScrollTrack)
	stamp := time.Now().Format("20060102-150405.000")
	base := fmt.Sprintf("%s-pos%d-vp%d-%s", strings.ReplaceAll(stamp, ".", ""), run.position, run.viewport, kind)
	for _, part := range []struct {
		name  string
		frame platform.Frame
	}{{"before", before}, {"after", after}} {
		path := filepath.Join(dir, fmt.Sprintf("%s-%s-f%d.png", base, part.name, part.frame.ID))
		if err := writeRegionPNG(path, part.frame.Image, region); err != nil {
			log.Printf("youkai: collect: diagnostics: write %s: %v", path, err)
			continue
		}
		log.Printf("youkai: collect: diagnostics: %s frame %d -> %s", part.name, part.frame.ID, path)
	}
	pruneDiagnostics(dir, maxDiagnosticFiles)
}

// writeRegionPNG encodes the region of img as a PNG file. A nil image or
// an empty intersection is reported rather than written.
func writeRegionPNG(path string, img image.Image, region image.Rectangle) error {
	if img == nil {
		return fmt.Errorf("no image")
	}
	r := region.Intersect(img.Bounds())
	if r.Empty() {
		return fmt.Errorf("region %v is outside the image %v", region, img.Bounds())
	}
	crop := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	draw.Draw(crop, crop.Bounds(), img, r.Min, draw.Src)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if err := png.Encode(file, crop); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

// pruneDiagnostics keeps the newest keep PNG files of dir by name (the
// names start with a sortable timestamp) and removes the rest.
func pruneDiagnostics(dir string, keep int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var names []string
	for _, e := range entries {
		if e.Type().IsRegular() && strings.HasSuffix(e.Name(), ".png") {
			names = append(names, e.Name())
		}
	}
	if len(names) <= keep {
		return
	}
	sort.Strings(names)
	for _, name := range names[:len(names)-keep] {
		_ = os.Remove(filepath.Join(dir, name))
	}
}
