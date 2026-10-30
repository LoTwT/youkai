package collect

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/platform"
)

// diagnosticDumpDirs lists the per-failure dump directories.
func diagnosticDumpDirs(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		t.Fatalf("read diagnostics: %v", err)
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, filepath.Join(dir, e.Name()))
		}
	}
	return dirs
}

// dumpPNGs lists one dump's frame crops.
func dumpPNGs(t *testing.T, dump string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dump, "*.png"))
	if err != nil {
		t.Fatalf("glob dump crops: %v", err)
	}
	return files
}

// readDumpManifest decodes one dump's manifest.
func readDumpManifest(t *testing.T, dump string) diagnosticManifest {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dump, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest diagnosticManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	return manifest
}

func manifestFrameIDs(manifest diagnosticManifest) []uint64 {
	ids := make([]uint64, 0, len(manifest.Frames))
	for _, frame := range manifest.Frames {
		ids = append(ids, frame.Frame)
	}
	return ids
}

// TestScrollFailureDumpWritesBoundedSequence pins the field diagnostics: a
// failed wait writes its whole retained sequence and manifest into one
// timestamped subdirectory, a walk writes at most
// maxDiagnosticDumpsPerRun dumps, and an empty directory setting writes
// nothing.
func TestScrollFailureDumpWritesBoundedSequence(t *testing.T) {
	layout := MacCloudLayout()
	frame := func(id uint64) platform.Frame {
		return platform.Frame{ID: id, Geometry: 1, Image: image.NewRGBA(image.Rect(0, 0, 1280, 1000))}
	}
	dir := t.TempDir()
	run := &inventoryRun{inv: &Inventory{Layout: layout, DiagnosticDir: dir}, position: 1, viewport: 4}

	trace := newScrollTrace(layout, frame(10))
	for i := 0; i < 5; i++ {
		trace.record(frame(uint64(11 + i)))
	}
	run.dumpScrollTrace("scroll-lost", 0, frame(10), frame(15), trace, platform.Frame{}, false)

	dumps := diagnosticDumpDirs(t, dir)
	if len(dumps) != 1 {
		t.Fatalf("one dump wrote %v, want one directory", dumps)
	}
	if !strings.Contains(filepath.Base(dumps[0]), "-pos1-vp4-scroll-lost") {
		t.Fatalf("dump directory %q does not name the position, viewport and outcome", dumps[0])
	}
	manifest := readDumpManifest(t, dumps[0])
	if manifest.Kind != "scroll-lost" || manifest.Position != 1 || manifest.Viewport != 4 {
		t.Fatalf("manifest header %+v, want scroll-lost at position 1 viewport 4", manifest)
	}
	if ids := manifestFrameIDs(manifest); fmt.Sprint(ids) != "[10 11 12 13 14 15]" {
		t.Fatalf("manifest frames %v, want the complete received sequence", ids)
	}
	want := layout.ListViewport.Union(layout.ScrollTrack)
	if manifest.Region.Width != want.Dx() || manifest.Region.Height != want.Dy() {
		t.Fatalf("manifest region %+v, want the %v list and scrollbar crop", manifest.Region, want)
	}
	files := dumpPNGs(t, dumps[0])
	if len(files) != len(manifest.Frames) {
		t.Fatalf("%d crops for %d frames, want one crop per frame", len(files), len(manifest.Frames))
	}
	for _, path := range files {
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

	for i := 0; i < maxDiagnosticDumpsPerRun+2; i++ {
		id := uint64(100 + 2*i)
		fresh := newScrollTrace(layout, frame(id))
		fresh.record(frame(id + 1))
		run.dumpScrollTrace("scroll-lost", 0, frame(id), frame(id+1), fresh, platform.Frame{}, false)
	}
	if dumps := diagnosticDumpDirs(t, dir); len(dumps) != maxDiagnosticDumpsPerRun {
		t.Fatalf("a walk wrote %d dumps, want the per-run bound of %d", len(dumps), maxDiagnosticDumpsPerRun)
	}

	off := &inventoryRun{inv: &Inventory{Layout: layout}}
	off.dumpScrollTrace("scroll-lost", 0, frame(1), frame(2), nil, platform.Frame{}, false)
}

// TestScrollFailureDumpKeepsSameMillisecondDumps pins the exclusive dump
// directory: two failures stamped in the same millisecond keep separate
// sequences instead of overwriting each other's evidence.
func TestScrollFailureDumpKeepsSameMillisecondDumps(t *testing.T) {
	layout := MacCloudLayout()
	frame := func(id uint64) platform.Frame {
		return platform.Frame{ID: id, Geometry: 1, Image: image.NewRGBA(image.Rect(0, 0, 1280, 1000))}
	}
	pinned := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	previous := diagnosticNow
	diagnosticNow = func() time.Time { return pinned }
	t.Cleanup(func() { diagnosticNow = previous })

	dir := t.TempDir()
	run := &inventoryRun{inv: &Inventory{Layout: layout, DiagnosticDir: dir}, position: 1, viewport: 4}
	for i := 0; i < 2; i++ {
		id := uint64(10 + 2*i)
		trace := newScrollTrace(layout, frame(id))
		run.dumpScrollTrace("scroll-lost", 0, frame(id), frame(id+1), trace, platform.Frame{}, false)
	}
	dumps := diagnosticDumpDirs(t, dir)
	if len(dumps) != 2 {
		t.Fatalf("same-millisecond dumps wrote %v, want two directories", dumps)
	}
	for _, dump := range dumps {
		if len(dumpPNGs(t, dump)) != 2 {
			t.Fatalf("dump %q lost a crop to the name collision", dump)
		}
		if manifest := readDumpManifest(t, dump); len(manifest.Frames) != 2 {
			t.Fatalf("dump %q manifest frames %d, want both frame entries", dump, len(manifest.Frames))
		}
	}
}

// traceFrameIDs returns a trace's retained frame IDs in order.
func traceFrameIDs(trace *scrollTrace) []uint64 {
	ids := make([]uint64, 0, len(trace.frames))
	for _, frame := range trace.frames {
		ids = append(ids, frame.id)
	}
	return ids
}

// TestScrollTraceRetainsHeadAndTail pins the bounded retention: a long
// wait keeps the leading frames and the newest frames while the middle is
// dropped, under both the frame and byte limits.
func TestScrollTraceRetainsHeadAndTail(t *testing.T) {
	layout := Layout{
		ListViewport: image.Rect(0, 0, 4, 4),
		ScrollTrack:  image.Rect(4, 0, 5, 4),
	}
	frame := func(id uint64) platform.Frame {
		return platform.Frame{ID: id, Geometry: 1, Image: image.NewRGBA(image.Rect(0, 0, 5, 4))}
	}
	frameBytes := 4 * 5 * 4

	trace := newScrollTrace(layout, frame(1))
	trace.maxFrames, trace.maxBytes, trace.headFrames = 8, 1<<20, 3
	for i := uint64(2); i <= 10; i++ {
		trace.record(frame(i))
	}
	if ids := traceFrameIDs(trace); fmt.Sprint(ids) != "[1 2 3 6 7 8 9 10]" {
		t.Fatalf("retained frames %v, want the head and the newest frames", ids)
	}
	if trace.dropped != 2 || trace.droppedBytes != 2*frameBytes {
		t.Fatalf("dropped %d frames / %d bytes, want the two middle frames", trace.dropped, trace.droppedBytes)
	}
	if !strings.Contains(trace.retentionPolicy(), "baseline") {
		t.Fatalf("retention policy %q does not explain the kept baseline", trace.retentionPolicy())
	}

	byBytes := newScrollTrace(layout, frame(1))
	byBytes.maxFrames, byBytes.maxBytes, byBytes.headFrames = 100, 5*frameBytes, 2
	for i := uint64(2); i <= 10; i++ {
		byBytes.record(frame(i))
	}
	if ids := traceFrameIDs(byBytes); fmt.Sprint(ids) != "[1 2 8 9 10]" {
		t.Fatalf("byte-capped trace kept %v, want the head and the newest frames", ids)
	}
	if byBytes.bytes > byBytes.maxBytes {
		t.Fatalf("byte-capped trace holds %d bytes, limit %d", byBytes.bytes, byBytes.maxBytes)
	}
}

// TestScrollTraceKeepsFirstMovedAndCandidate pins the pinned frames across a
// long wait: the first comparison that was not Stationary and the latest
// candidate survive even when both fall in the dropped middle, and a newer
// candidate replaces the previous pin.
func TestScrollTraceKeepsFirstMovedAndCandidate(t *testing.T) {
	layout := Layout{
		ListViewport: image.Rect(0, 0, 4, 4),
		ScrollTrack:  image.Rect(4, 0, 5, 4),
	}
	frame := func(id uint64) platform.Frame {
		return platform.Frame{ID: id, Geometry: 1, Image: image.NewRGBA(image.Rect(0, 0, 5, 4))}
	}
	trace := newScrollTrace(layout, frame(1))
	trace.maxFrames, trace.maxBytes, trace.headFrames = 10, 1<<20, 4
	for i := uint64(2); i <= 100; i++ {
		trace.record(frame(i))
		switch i {
		case 5:
			// The drag's first visible effect, far behind the tail soon.
			trace.noteBefore(frame(i), ScrollMatch{Lost: true, Evidence: "first movement"})
		case 40:
			trace.pinCandidate(frame(i))
		case 80:
			// A later candidate replaces the frame-40 pin.
			trace.pinCandidate(frame(i))
		}
	}
	if ids := traceFrameIDs(trace); fmt.Sprint(ids) != "[1 2 3 4 5 80 97 98 99 100]" {
		t.Fatalf("long wait retained %v, want the head, first movement, final candidate and tail", ids)
	}
	if trace.bytes > trace.maxBytes || len(trace.frames) > trace.maxFrames {
		t.Fatalf("long wait holds %d frames / %d bytes, want the caps to hold", len(trace.frames), trace.bytes)
	}
	if policy := trace.retentionPolicy(); !strings.Contains(policy, "not stationary") || !strings.Contains(policy, "candidate") {
		t.Fatalf("retention policy %q does not explain the pinned frames", policy)
	}
}

// TestScrollFailureManifestCarriesMatcherNumbers pins the manifest's
// numerical fidelity: each entry carries the ScrollMatch the wait recorded,
// including err0, best dy, best err and every whole-row candidate, without
// rounding or recomputation.
func TestScrollFailureManifestCarriesMatcherNumbers(t *testing.T) {
	layout := MacCloudLayout()
	frame := func(id uint64, at time.Time) platform.Frame {
		return platform.Frame{ID: id, Geometry: 1, ArrivedAt: at,
			Image: image.NewRGBA(image.Rect(0, 0, 1280, 1000))}
	}
	base := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	before := frame(10, base)
	moving := frame(11, base.Add(40*time.Millisecond))
	settled := frame(12, base.Add(90*time.Millisecond))
	trace := newScrollTrace(layout, before)
	trace.record(moving)
	trace.record(settled)

	total := ScrollMatch{Displaced: true, Rows: 1, Evidence: "scripted total", Metrics: &ScrollMetrics{
		Err0: 5.25, Measured: true, BestDy: 147, BestErr: 2.5,
		RowErrors: map[int]float64{1: 2.5, 2: 6.75, -1: 9.5},
	}}
	rel := ScrollMatch{Stationary: true, Evidence: "scripted settle", Metrics: &ScrollMetrics{Err0: 1.125}}
	trace.noteBefore(moving, total)
	trace.noteCandidate(moving, settled, rel)

	dir := t.TempDir()
	run := &inventoryRun{inv: &Inventory{Layout: layout, DiagnosticDir: dir}, position: 1, viewport: 4}
	run.dumpScrollTrace("scroll-lost", 0, before, moving, trace, moving, true)

	dumps := diagnosticDumpDirs(t, dir)
	if len(dumps) != 1 {
		t.Fatalf("one dump wrote %v, want one directory", dumps)
	}
	manifest := readDumpManifest(t, dumps[0])
	if manifest.CandidateFrame != moving.ID {
		t.Fatalf("manifest candidate %d, want %d", manifest.CandidateFrame, moving.ID)
	}
	if len(manifest.Frames) != 3 {
		t.Fatalf("manifest frames %d, want the baseline, moving and settled frames", len(manifest.Frames))
	}
	if manifest.Frames[0].VsBefore != nil || manifest.Frames[0].VsCandidate != nil {
		t.Fatalf("baseline entry %+v, want no comparisons", manifest.Frames[0])
	}

	entry := manifest.Frames[1]
	if entry.Frame != moving.ID || entry.ArrivedMs != 40 {
		t.Fatalf("moving entry %+v, want frame %d at +40ms", entry, moving.ID)
	}
	if entry.VsBefore == nil || entry.VsBefore.Verdict != "Displaced" || entry.VsBefore.ReferenceFrame != before.ID {
		t.Fatalf("moving vs_before %+v, want the recorded total displacement against frame %d", entry.VsBefore, before.ID)
	}
	if entry.VsBefore.Err0 == nil || *entry.VsBefore.Err0 != 5.25 ||
		entry.VsBefore.BestDy == nil || *entry.VsBefore.BestDy != 147 ||
		entry.VsBefore.BestErr == nil || *entry.VsBefore.BestErr != 2.5 {
		t.Fatalf("moving vs_before numbers %+v, want the matcher's 5.25/147/2.5", entry.VsBefore)
	}
	wantRows := map[string]float64{"+1": 2.5, "+2": 6.75, "-1": 9.5}
	if fmt.Sprint(entry.VsBefore.RowErrors) != fmt.Sprint(wantRows) {
		t.Fatalf("row candidates %v, want %v", entry.VsBefore.RowErrors, wantRows)
	}

	settledEntry := manifest.Frames[2]
	if settledEntry.VsCandidate == nil || settledEntry.VsCandidate.Verdict != "Stationary" ||
		settledEntry.VsCandidate.ReferenceFrame != moving.ID {
		t.Fatalf("settled vs_candidate %+v, want the stationary comparison against frame %d", settledEntry.VsCandidate, moving.ID)
	}
	if settledEntry.VsCandidate.Err0 == nil || *settledEntry.VsCandidate.Err0 != 1.125 {
		t.Fatalf("settled vs_candidate err0 %v, want 1.125", settledEntry.VsCandidate.Err0)
	}
	if settledEntry.VsCandidate.BestDy != nil || settledEntry.VsCandidate.RowErrors != nil {
		t.Fatalf("settled vs_candidate %+v has numbers the stationary matcher never computed", settledEntry.VsCandidate)
	}
	if manifest.DroppedFrames != 0 || !strings.Contains(manifest.Retention, "baseline") {
		t.Fatalf("manifest retention %+v, want the stated drop policy and no drops", manifest)
	}
}

// TestScrollFailureDumpSkipsNilImages pins the nil-image guard: frames
// without pixels still appear in the manifest but write no crop, and the
// dump itself still succeeds.
func TestScrollFailureDumpSkipsNilImages(t *testing.T) {
	layout := MacCloudLayout()
	dir := t.TempDir()
	run := &inventoryRun{inv: &Inventory{Layout: layout, DiagnosticDir: dir}, position: 1, viewport: 4}
	trace := newScrollTrace(layout, platform.Frame{ID: 10})
	trace.record(platform.Frame{ID: 11})
	run.dumpScrollTrace("scroll-lost", 0, platform.Frame{ID: 10}, platform.Frame{ID: 11}, trace, platform.Frame{}, false)

	dumps := diagnosticDumpDirs(t, dir)
	if len(dumps) != 1 {
		t.Fatalf("nil images wrote %v, want one manifest-only dump", dumps)
	}
	if files := dumpPNGs(t, dumps[0]); len(files) != 0 {
		t.Fatalf("nil images wrote %v, want no crops", files)
	}
	if manifest := readDumpManifest(t, dumps[0]); len(manifest.Frames) != 2 {
		t.Fatalf("manifest frames %d, want both frame entries", len(manifest.Frames))
	}
}

// TestPruneDiagnosticsLogsFailures pins the best-effort cleanup's
// observability: a directory read failure and every removal failure are
// logged with the path and error, and pruning still returns no error to
// the walk.
func TestPruneDiagnosticsLogsFailures(t *testing.T) {
	dir := t.TempDir()
	logs := &bytes.Buffer{}
	previous := log.Writer()
	log.SetOutput(logs)
	t.Cleanup(func() { log.SetOutput(previous) })

	readErr := errors.New("scripted read failure")
	pruneDiagnosticsWith(dir, 2, func(string) ([]os.DirEntry, error) {
		return nil, readErr
	}, func(string) error {
		t.Fatal("prune removed a file after a failed read")
		return nil
	})
	if got := logs.String(); !strings.Contains(got, "read "+dir) || !strings.Contains(got, readErr.Error()) {
		t.Fatalf("read failure log = %q, want the directory %q and %v", got, dir, readErr)
	}

	for i := 0; i < 3; i++ {
		path := filepath.Join(dir, fmt.Sprintf("2026102%d-000000000-x", i))
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	logs.Reset()
	removeErr := errors.New("scripted remove failure")
	pruneDiagnosticsWith(dir, 1, os.ReadDir, func(string) error { return removeErr })
	if got := logs.String(); strings.Count(got, removeErr.Error()) != 2 {
		t.Fatalf("removal failure log = %q, want one line for each of the two oldest dumps", got)
	}
	for _, name := range []string{"20261020-000000000-x", "20261021-000000000-x"} {
		if path := filepath.Join(dir, name); !strings.Contains(logs.String(), "remove "+path) {
			t.Fatalf("removal failure log = %q, want the path %q", logs.String(), path)
		}
	}
}

// TestPruneDiagnosticsKeepsNewest pins the directory bound: only the
// newest dumps survive, including legacy top-level crop files.
func TestPruneDiagnosticsKeepsNewest(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 4; i++ {
		name := filepath.Join(dir, fmt.Sprintf("2026102%d-000000000-x", i))
		if err := os.Mkdir(name, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	legacy := filepath.Join(dir, "20261024-000000000-legacy.png")
	if err := os.WriteFile(legacy, []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}
	pruneDiagnostics(dir, 2)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if fmt.Sprint(names) != "[20261023-000000000-x 20261024-000000000-legacy.png]" {
		t.Fatalf("prune kept %v, want the two newest dumps", names)
	}
}
