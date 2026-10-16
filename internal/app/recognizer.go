package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"runtime"

	"github.com/LoTwT/youkai/internal/recognition"
	"github.com/LoTwT/youkai/internal/scan"
)

// The OCR artifacts this app bundles, as fetchdeps lays them out below
// the resources root. MyGo's packaging merges the per-platform resource
// directories into the root of the shipped resources (see the framework's
// resources documentation), so a packaged app — and the development app
// mygo dev stages — finds the runtime library at onnxruntime/ and the
// shared model at models/, independently of the platform directory the
// fetchdeps tool downloads into (docs/specs/t3-ocr-runtime-model-samples.md
// 固定组合与制品静态核查).
const (
	// ocrModelName is the single shared recognition model.
	ocrModelName = "PP-OCRv6_rec_small.onnx"
	// onnxRuntimeLibName returns the pinned ONNX Runtime shared library
	// file name of a supported platform; the version is part of the
	// pinned combination.
	onnxRuntimeVersion = "1.29.0"
)

// onnxRuntimeLibName returns the pinned ONNX Runtime shared library file
// name for the given GOOS, or an empty string when the platform has no
// pinned artifact.
func onnxRuntimeLibName(goos string) string {
	switch goos {
	case "darwin":
		return "libonnxruntime." + onnxRuntimeVersion + ".dylib"
	case "windows":
		return "onnxruntime.dll"
	default:
		return ""
	}
}

// ocrResources resolves the OCR runtime and model paths below a resources
// root, as the bundled layout ships them. Paths stay absolute and rooted
// at resourcesRoot so loading never depends on the working directory or
// the system library search path (docs/specs/scanner-architecture.md §4).
func ocrResources(resourcesRoot string) (recognition.Resources, error) {
	lib := onnxRuntimeLibName(runtime.GOOS)
	if lib == "" {
		return recognition.Resources{}, fmt.Errorf("app: no pinned ONNX runtime artifact for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	if resourcesRoot == "" {
		return recognition.Resources{}, errors.New("app: resources root is empty")
	}
	return recognition.Resources{
		LibraryPath: filepath.Join(resourcesRoot, "onnxruntime", lib),
		ModelPath:   filepath.Join(resourcesRoot, "models", ocrModelName),
	}, nil
}

// newOCRRecognizer creates the real recognizer from a resources root:
// one model session over the bundled ONNX Runtime environment. The
// process-wide environment outlives the session (see recognition.Shutdown)
// and the returned adapter is closed only by the explicit-exit wrap-up.
func newOCRRecognizer(resourcesRoot string) (Recognizer, error) {
	res, err := ocrResources(resourcesRoot)
	if err != nil {
		return nil, err
	}
	rec, err := recognition.New(res)
	if err != nil {
		return nil, err
	}
	return &lineRecognizer{rec: rec}, nil
}

// lineRecognizer adapts the OCR recognizer to the scan kernel's
// Recognizer: one observation — the text-line crops of one item — is
// recognized line by line, and the per-line texts and scores travel on as
// the task's result payload. Interpreting the lines (drive-disc fields,
// units) is M2's game adapter; nothing here judges a decoded line.
//
// The wrapped *recognition.Recognizer belongs to the application's
// lifetime: it is created once for the first scan and closed once by the
// explicit-exit wrap-up, never per scan (docs/specs/scanner-architecture.md
// §4 释放顺序).
type lineRecognizer struct {
	rec *recognition.Recognizer
}

// Recognize implements scan.Recognizer. A hard recognition error that
// belongs to this one observation is reported as the task's failure; a
// broken recognizer or runtime is returned as an error and stops the
// session; a stop's cancellation returns without an outcome, leaving the
// entry unprocessed to the kernel.
func (r *lineRecognizer) Recognize(ctx context.Context, task scan.Task, submit scan.Submit) error {
	obs, ok := task.Observation.(Observation)
	if !ok {
		return fmt.Errorf("app: observation of task %d is %T, not app.Observation", task.Seq, task.Observation)
	}
	if ctx.Err() != nil {
		return nil
	}
	result := RecognitionResult{Lines: make([]RecognizedLine, 0, len(obs.Lines))}
	for _, crop := range obs.Lines {
		if ctx.Err() != nil {
			return nil
		}
		line, err := r.rec.Recognize(ctx, crop.Image)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			if errors.Is(err, recognition.ErrInvalidImage) {
				// One unusable crop fails this one observation, not the
				// session.
				return submit(task.SessionID, task.Seq, scan.Recognition{Failed: err})
			}
			// Anything else — inference failure, a closed recognizer, a
			// broken runtime — stops the whole session.
			return err
		}
		result.Lines = append(result.Lines, RecognizedLine{Role: crop.Role, Text: line.Text, Score: line.Score})
	}
	log.Printf("youkai: scan %s item %d recognized: %s", task.SessionID, task.Seq, formatLines(result.Lines))
	return submit(task.SessionID, task.Seq, scan.Recognition{Result: result})
}

// Close releases the model session; the process-wide environment stays
// for recognition.Shutdown.
func (r *lineRecognizer) Close() error {
	return r.rec.Close()
}

// formatLines renders recognized lines compactly for the development
// log: role=text(score) joined by spaces.
func formatLines(lines []RecognizedLine) string {
	out := ""
	for i, line := range lines {
		if i > 0 {
			out += " "
		}
		out += fmt.Sprintf("%s=%s(%.3f)", line.Role, line.Text, line.Score)
	}
	return out
}
