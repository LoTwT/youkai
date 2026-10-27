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

// diagnosticNow supplies the diagnostic file timestamps and the bottom
// confirmation's decision timing. Tests replace it to pin that crops are
// written after bottom_ms is accumulated without sleeping on the wall
// clock; production uses time.Now.
var diagnosticNow = time.Now

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
	stamp := diagnosticNow().Format("20060102-150405.000")
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

// writeRegionPNG encodes the region of img as a PNG file. The parameter is
// the concrete *image.RGBA of platform.Frame.Image: a typed nil assigned to
// image.Image would not compare equal to nil and would panic in Bounds. A
// nil image or an empty intersection is reported rather than written.
func writeRegionPNG(path string, img *image.RGBA, region image.Rectangle) error {
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
// names start with a sortable timestamp) and removes the rest. Read and
// removal failures are logged and swallowed: diagnostics must never
// change the walk.
func pruneDiagnostics(dir string, keep int) {
	pruneDiagnosticsWith(dir, keep, os.ReadDir, os.Remove)
}

// pruneDiagnosticsWith is pruneDiagnostics with its filesystem calls
// injected, so the failure paths are testable on every platform.
func pruneDiagnosticsWith(dir string, keep int, readDir func(string) ([]os.DirEntry, error), remove func(string) error) {
	entries, err := readDir(dir)
	if err != nil {
		log.Printf("youkai: collect: diagnostics: read %s: %v", dir, err)
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
		path := filepath.Join(dir, name)
		if err := remove(path); err != nil {
			log.Printf("youkai: collect: diagnostics: remove %s: %v", path, err)
		}
	}
}
