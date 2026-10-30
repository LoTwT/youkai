package collect

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/LoTwT/youkai/internal/platform"
)

// Bounds of the scroll failure dumps: a walk stops on its first failed
// scroll, so a handful per walk covers retries, the top confirmation and
// the bottom confirmation; the directory keeps the newest dumps across
// walks so repeated field runs never grow it without bound.
const (
	maxDiagnosticDumpsPerRun = 6
	maxDiagnosticDumps       = 12
)

// diagnosticNow supplies the diagnostic file timestamps and the bottom
// confirmation's decision timing. Tests replace it to pin that dumps are
// written after bottom_ms is accumulated without sleeping on the wall
// clock; production uses time.Now.
var diagnosticNow = time.Now

// writeDiagnosticFile writes one diagnostic artifact's bytes, creating it
// exclusively. Tests replace it to exercise write failures and to pin that
// the write cost stays outside the decision timers; production uses an
// exclusive 0600 file.
var writeDiagnosticFile = func(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

// diagnosticManifest is the failure dump's index: the wait it belongs to,
// the retention limits, and one entry per retained frame with the matcher
// numbers the wait itself produced.
type diagnosticManifest struct {
	Kind           string            `json:"kind"`
	Position       int               `json:"position"`
	Viewport       int               `json:"viewport"`
	Attempt        int               `json:"attempt,omitempty"`
	BeforeFrame    uint64            `json:"before_frame"`
	AfterFrame     uint64            `json:"after_frame"`
	CandidateFrame uint64            `json:"candidate_frame,omitempty"`
	Region         diagnosticRegion  `json:"region"`
	FrameLimit     int               `json:"frame_limit"`
	ByteLimit      int               `json:"byte_limit"`
	Retention      string            `json:"retention"`
	DroppedFrames  int               `json:"dropped_frames"`
	DroppedBytes   int               `json:"dropped_bytes"`
	MatchNote      string            `json:"match_note"`
	Frames         []diagnosticFrame `json:"frames"`
}

// diagnosticRegion is the cropped region in full-frame pixels.
type diagnosticRegion struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

// diagnosticFrame is one retained frame's manifest entry.
type diagnosticFrame struct {
	File        string           `json:"file,omitempty"`
	Frame       uint64           `json:"frame"`
	ArrivedAt   string           `json:"arrived_at,omitempty"`
	ArrivedMs   float64          `json:"arrived_ms"`
	Reused      bool             `json:"reused"`
	VsBefore    *diagnosticMatch `json:"vs_before"`
	VsCandidate *diagnosticMatch `json:"vs_candidate"`
}

// diagnosticMatch is one comparison's verdict and numbers.
type diagnosticMatch struct {
	ReferenceFrame uint64             `json:"reference_frame"`
	Verdict        string             `json:"verdict"`
	Err0           *float64           `json:"err0"`
	BestDy         *int               `json:"best_dy"`
	BestErr        *float64           `json:"best_err"`
	RowErrors      map[string]float64 `json:"row_errors"`
	Evidence       string             `json:"evidence,omitempty"`
}

// diagnosticMatchNote states why some entries carry no comparison: the
// wait only computes a MatchScroll for frames it must judge, and the
// manifest must not imply numbers that were never measured.
const diagnosticMatchNote = "comparisons are the MatchScroll results the wait produced; null means the wait did not evaluate that pair"

// dumpScrollTrace writes one failed wait's retained frame sequence, its
// manifest and the pre-drag/after frames as PNG crops to a subdirectory of
// the diagnostic directory. attempt numbers a scroll action's drag attempt
// (0 when the wait belongs to a top or bottom confirmation); every attempt
// gets its own subdirectory so a retried scroll keeps all of them. Every
// failure here is logged and swallowed: diagnostics must never change the
// walk.
func (run *inventoryRun) dumpScrollTrace(kind string, attempt int, before, after platform.Frame, trace *scrollTrace, candidate platform.Frame, hasCandidate bool) {
	dir := run.inv.DiagnosticDir
	if dir == "" || run.diagnosticDumps >= maxDiagnosticDumpsPerRun {
		return
	}
	run.diagnosticDumps++
	if trace == nil {
		trace = newScrollTrace(run.inv.Layout, before)
	}
	trace.ensure(before)
	trace.ensure(after)

	stamp := diagnosticNow().Format("20060102-150405.000")
	name := fmt.Sprintf("%s-pos%d-vp%d-%s", strings.ReplaceAll(stamp, ".", ""), run.position, run.viewport, kind)
	if attempt > 0 {
		name = fmt.Sprintf("%s-attempt%d", name, attempt)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		log.Printf("youkai: collect: diagnostics: create %s: %v", dir, err)
		return
	}
	subdir, err := createDumpDir(dir, name)
	if err != nil {
		log.Printf("youkai: collect: diagnostics: create %s: %v", filepath.Join(dir, name), err)
		return
	}

	manifest := diagnosticManifest{
		Kind:           kind,
		Position:       run.position,
		Viewport:       run.viewport,
		Attempt:        attempt,
		BeforeFrame:    before.ID,
		AfterFrame:     after.ID,
		CandidateFrame: candidate.ID,
		Region: diagnosticRegion{
			X: trace.region.Min.X, Y: trace.region.Min.Y,
			Width: trace.region.Dx(), Height: trace.region.Dy(),
		},
		FrameLimit:    trace.maxFrames,
		ByteLimit:     trace.maxBytes,
		Retention:     trace.retentionPolicy(),
		DroppedFrames: trace.dropped,
		DroppedBytes:  trace.droppedBytes,
		MatchNote:     diagnosticMatchNote,
	}
	if !hasCandidate {
		manifest.CandidateFrame = 0
	}
	written := 0
	for i := range trace.frames {
		frame := &trace.frames[i]
		entry := diagnosticFrame{
			Frame:     frame.id,
			ArrivedAt: formatArrival(frame.arrivedAt),
			ArrivedMs: arrivalOffsetMs(before, frame.arrivedAt),
			Reused:    frame.reused,
			VsBefore:  diagnosticComparison(frame.vsBefore, before.ID),
		}
		if frame.vsCandidate != nil {
			entry.VsCandidate = diagnosticComparison(frame.vsCandidate, frame.candidateID)
		}
		if frame.image != nil {
			// The crop is already exactly the region; writeDiagnosticPNG
			// keeps its nil guard and failure handling.
			entry.File = fmt.Sprintf("%04d-f%d.png", i, frame.id)
			path := filepath.Join(subdir, entry.File)
			if err := writeDiagnosticPNG(path, frame.image); err != nil {
				log.Printf("youkai: collect: diagnostics: write %s: %v", path, err)
				entry.File = ""
			} else {
				written++
			}
		}
		manifest.Frames = append(manifest.Frames, entry)
	}

	path := filepath.Join(subdir, "manifest.json")
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err == nil {
		if err := writeDiagnosticFile(path, append(data, '\n')); err != nil {
			log.Printf("youkai: collect: diagnostics: write %s: %v", path, err)
		}
	} else {
		log.Printf("youkai: collect: diagnostics: marshal manifest: %v", err)
	}
	log.Printf("youkai: collect: diagnostics: %s: %d frames, %d dropped (%d bytes) -> %s",
		name, written, trace.dropped, trace.droppedBytes, subdir)
	pruneDiagnostics(dir, maxDiagnosticDumps)
}

// createDumpDir creates the dump directory exclusively. A second failure
// in the same millisecond gets a numeric suffix instead of silently
// reusing the first dump's files and losing its evidence.
func createDumpDir(parent, name string) (string, error) {
	for i := 0; i < 10; i++ {
		candidate := name
		if i > 0 {
			candidate = fmt.Sprintf("%s-%d", name, i+1)
		}
		path := filepath.Join(parent, candidate)
		err := os.Mkdir(path, 0o700)
		if err == nil {
			return path, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return "", err
		}
	}
	return "", fmt.Errorf("10 timestamp colliding dumps already exist")
}

// diagnosticComparison converts one recorded matcher verdict into the
// manifest's shape. Nil means the wait never evaluated the pair; the
// numbers are copied unchanged from the matcher.
func diagnosticComparison(sm *ScrollMatch, reference uint64) *diagnosticMatch {
	if sm == nil {
		return nil
	}
	out := &diagnosticMatch{
		ReferenceFrame: reference,
		Verdict:        scrollVerdict(*sm),
		Evidence:       sm.Evidence,
	}
	if m := sm.Metrics; m != nil {
		err0 := m.Err0
		out.Err0 = &err0
		if m.Measured {
			dy, best := m.BestDy, m.BestErr
			out.BestDy, out.BestErr = &dy, &best
			if len(m.RowErrors) > 0 {
				out.RowErrors = make(map[string]float64, len(m.RowErrors))
				for row, err := range m.RowErrors {
					out.RowErrors[fmt.Sprintf("%+d", row)] = err
				}
			}
		}
	}
	return out
}

// scrollVerdict names a ScrollMatch's outcome for diagnostics.
func scrollVerdict(sm ScrollMatch) string {
	switch {
	case sm.Stationary:
		return "Stationary"
	case sm.Displaced:
		return "Displaced"
	case sm.Lost:
		return "Lost"
	case sm.Undecided:
		return "Undecided"
	default:
		return "None"
	}
}

// formatArrival renders a frame's arrival time for the manifest.
func formatArrival(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339Nano)
}

// arrivalOffsetMs is the frame's arrival relative to the pre-drag frame,
// which is how the settle timeline reads in the field.
func arrivalOffsetMs(before platform.Frame, arrived time.Time) float64 {
	if before.ArrivedAt.IsZero() || arrived.IsZero() {
		return 0
	}
	return float64(arrived.Sub(before.ArrivedAt)) / float64(time.Millisecond)
}

// scrollThresholdReporter is the matcher's optional capability reporting
// the thresholds its MatchScroll applies, so the diagnostic logs can print
// the distance to each limit. A matcher without it still logs the measured
// numbers, just not the margins.
type scrollThresholdReporter interface {
	ScrollThresholds() (stationaryMax, displacedMax float64)
}

// scrollMarginFields renders one wait's decision margins for the success
// logs: the zero-shift error and its distance below
// ScrollStationaryMax, the best displacement error and its distance below
// ScrollDisplacedMax, the gap to the next whole-row candidate, and the
// candidate-relative zero-shift error of the settling confirmation. A
// positive margin means the value is inside its threshold. "none" marks a
// number the matcher did not measure.
func scrollMarginFields(matcher Matcher, total, settle ScrollMatch) string {
	err0, stationary, bestErr, displaced, rival, settleErr := "none", "none", "none", "none", "none", "none"
	stationaryMax, displacedMax, haveThresholds := 0.0, 0.0, false
	if reporter, ok := matcher.(scrollThresholdReporter); ok {
		stationaryMax, displacedMax = reporter.ScrollThresholds()
		haveThresholds = true
	}
	if m := total.Metrics; m != nil {
		err0 = fmt.Sprintf("%.2f", m.Err0)
		if haveThresholds {
			stationary = fmt.Sprintf("%.2f", stationaryMax-m.Err0)
		}
		if m.Measured {
			bestErr = fmt.Sprintf("%.2f", m.BestErr)
			if haveThresholds {
				displaced = fmt.Sprintf("%.2f", displacedMax-m.BestErr)
			}
			if gap, ok := rivalGap(m.RowErrors); ok {
				rival = fmt.Sprintf("%.2f", gap)
			}
		}
	}
	if m := settle.Metrics; m != nil {
		settleErr = fmt.Sprintf("%.2f", m.Err0)
	}
	return fmt.Sprintf("err0=%s stationary_margin=%s best_err=%s displaced_margin=%s rival_gap=%s settle_err0=%s",
		err0, stationary, bestErr, displaced, rival, settleErr)
}

// rivalGap is the distance between the best and second-best whole-row
// candidate errors. Fewer than two candidates means there is nothing to
// distinguish.
func rivalGap(rowErrors map[int]float64) (float64, bool) {
	if len(rowErrors) < 2 {
		return 0, false
	}
	errs := make([]float64, 0, len(rowErrors))
	for _, err := range rowErrors {
		errs = append(errs, err)
	}
	sort.Float64s(errs)
	return errs[1] - errs[0], true
}

// writeDiagnosticPNG encodes the cropped image as a PNG and writes it
// through writeDiagnosticFile. The parameter is the concrete *image.RGBA
// so a typed nil cannot panic in Bounds; a nil image is reported rather
// than written.
func writeDiagnosticPNG(path string, img *image.RGBA) error {
	if img == nil {
		return fmt.Errorf("no image")
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return err
	}
	return writeDiagnosticFile(path, buf.Bytes())
}

// pruneDiagnostics keeps the newest keep dumps of dir by name (the names
// start with a sortable timestamp) and removes the rest. Regular PNG files
// are the pre-sequence dumps of older builds; they share the bound so an
// upgraded install does not keep them forever. Read and removal failures
// are logged and swallowed: diagnostics must never change the walk.
func pruneDiagnostics(dir string, keep int) {
	pruneDiagnosticsWith(dir, keep, os.ReadDir, os.RemoveAll)
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
		if e.IsDir() || (e.Type().IsRegular() && strings.HasSuffix(e.Name(), ".png")) {
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
