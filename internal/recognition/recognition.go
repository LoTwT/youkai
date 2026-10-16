// Package recognition runs the bundled PP-OCRv6 text-line recognition model
// on local CPU ONNX Runtime.
//
// The package covers the recognizer's own responsibilities only: native
// library and model lifecycle, preprocessing, inference, and CTC decoding
// (see docs/specs/scanner-architecture.md §3). It does not decide whether a
// decoded line is a plausible drive-disc field; field semantics belong to the
// game adapter, and the scan kernel owns cancellation accounting.
//
// Model facts (input/output names, shapes, character table, preprocessing and
// decoding rules) are fixed by docs/specs/t3-ocr-runtime-model-samples.md
// §固定 small 制品的输入与解码; the code below cites it where the rules are
// applied. The pinned artifacts come from tools/fetchdeps.
//
// This package requires cgo (CGO_ENABLED=1) to compile the ONNX Runtime Go
// bindings. Nothing is loaded at package initialization: the native library
// is dlopen'd/LoadLibrary'd only when [New] is first called.
//
// Lifecycle: the process-wide ONNX Runtime environment belongs to the
// application's lifetime (docs/specs/scanner-architecture.md §4). It is
// initialized by the first [New] and kept loaded across recognizer
// open/close cycles — the application creates a recognizer per scan session
// without repeatedly unloading the native library. The environment is
// destroyed only by an explicit [Shutdown], typically once shortly before
// process exit; [Shutdown] also covers the deliberate unload-and-reload case
// by allowing later [New] calls to re-initialize the environment.
package recognition

import (
	"context"
	"errors"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
)

// Errors returned by this package. They are wrapped with %w and always carry
// the offending path or value, so callers can both classify them with
// errors.Is and locate the cause in the message.
var (
	// ErrLibraryUnavailable reports a failure to load the ONNX Runtime
	// shared library: the file is missing, or the platform loader rejected
	// it (for example an architecture mismatch; the loader's own message is
	// included).
	ErrLibraryUnavailable = errors.New("onnx runtime library unavailable")
	// ErrLibraryInUse reports that the process-wide ONNX Runtime environment
	// was already initialized from a different library path. Only one
	// library can be loaded per process.
	ErrLibraryInUse = errors.New("onnx runtime environment already loaded from a different library path")
	// ErrEnvironmentInUse reports that [Shutdown] was called while
	// recognizers are still open; the environment cannot be destroyed
	// underneath them.
	ErrEnvironmentInUse = errors.New("onnx runtime environment still in use")
	// ErrModelUnavailable reports that the model file is missing, empty, or
	// unreadable.
	ErrModelUnavailable = errors.New("recognition model unavailable")
	// ErrModelInvalid reports that ONNX Runtime rejected the model file or
	// that its input/output layout does not match what this package
	// supports (corrupt file, wrong model, unexpected shapes).
	ErrModelInvalid = errors.New("recognition model is invalid")
	// ErrCharsetMismatch reports that the character table assembled from the
	// model's metadata does not match the model's output class count; the
	// model and its table are not a matching pair.
	ErrCharsetMismatch = errors.New("character table does not match the model")
	// ErrInvalidImage reports an input image that cannot be preprocessed.
	ErrInvalidImage = errors.New("invalid input image")
	// ErrClosed reports use of a [Recognizer] after [Recognizer.Close].
	ErrClosed = errors.New("recognizer is closed")
)

// ONNX Runtime thread pool sizes. These limit the threads ORT spawns inside
// inference (separate from the number of Go workers), because the scanner
// runs alongside the game and idle ORT spin threads cost CPU and power
// (docs/specs/t3-ocr-runtime-model-samples.md §CPU 与停止). The values are a
// deliberately conservative starting point, to be tuned from M2 measurements
// with the real workload; they are intentionally not caller-configurable
// (docs/specs/scanner-architecture.md §3: 首版不提供用户可调的并发参数).
const (
	ortIntraOpThreads = 2
	ortInterOpThreads = 1
)

// Resources names the native artifacts a [Recognizer] runs on. Both paths
// must be absolute: the scanner loads exactly the bundled copies resolved
// from the application resource root and must not depend on the working
// directory or the system library search path
// (docs/specs/scanner-architecture.md §4).
type Resources struct {
	// LibraryPath is the absolute path of the ONNX Runtime shared library
	// (onnxruntime.dll on Windows, the versioned libonnxruntime dylib on
	// macOS).
	LibraryPath string
	// ModelPath is the absolute path of the PP-OCRv6 recognition model
	// (resources/models/PP-OCRv6_rec_small.onnx).
	ModelPath string
}

// Result is the outcome of recognizing one text-line crop.
type Result struct {
	// Text is the decoded character sequence.
	Text string
	// Score is the mean model confidence over the decoded characters. It is
	// a raw model score, not the probability that the field is correct, and
	// must not be used as an acceptance threshold without independent
	// validation (docs/specs/t3-ocr-runtime-model-samples.md §模型配套资料).
	Score float64
}

// ortEnv holds the process-wide ONNX Runtime environment. The environment
// belongs to the application's lifetime (docs/specs/scanner-architecture.md
// §4): the first [New] initializes it, [Recognizer.Close] only releases that
// recognizer's session, and only [Shutdown] destroys the environment. The
// refcount tracks open recognizers so Shutdown can refuse to run while
// sessions are still alive.
var ortEnv struct {
	mu          sync.Mutex
	initialized bool   // environment is loaded and not yet shut down
	libraryPath string // absolute path the environment was loaded from
	refs        int    // number of open recognizers
}

// Recognizer runs the recognition model on text-line crops. A Recognizer owns
// one reused model session; create it with [New] and release it with
// [Recognizer.Close]. The process-wide ONNX Runtime environment outlives
// individual recognizers and is released by [Shutdown].
//
// Inference is serialized: the first release uses a single recognition worker
// (docs/specs/scanner-architecture.md §3), so [Recognizer.Recognize] takes the
// recognizer lock for the whole call and [Recognizer.Close] waits for any
// in-flight call. Cancel in-flight calls through their context before closing
// if that wait must be bounded.
type Recognizer struct {
	mu         sync.Mutex
	closed     bool
	session    *ort.DynamicAdvancedSession
	inputName  string
	outputName string
	classes    int
	charset    []string
}

// New loads the ONNX Runtime library (if this is the first recognizer in the
// process) and creates a reused model session.
//
// The environment initialized here stays loaded until an explicit
// [Shutdown]; recognizer sessions are released by [Recognizer.Close] and do
// not unload the native library. After [Shutdown], New re-initializes the
// environment on demand.
func New(res Resources) (*Recognizer, error) {
	libPath, err := requireAbsPath(res.LibraryPath, "ONNX Runtime library")
	if err != nil {
		return nil, err
	}
	modelPath, err := requireAbsPath(res.ModelPath, "recognition model")
	if err != nil {
		return nil, err
	}
	if fi, err := os.Stat(modelPath); err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrModelUnavailable, modelPath, err)
	} else if fi.Size() == 0 {
		return nil, fmt.Errorf("%w: %s: file is empty", ErrModelUnavailable, modelPath)
	}
	// Fail with a locatable message when the library file is absent before
	// the platform loader reports it, so 缺库 is distinguishable from 错架构
	// (the latter surfaces via the loader's own message below). Checked
	// regardless of the environment state so the error is stable.
	if _, err := os.Stat(libPath); err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrLibraryUnavailable, libPath, err)
	}

	ortEnv.mu.Lock()
	defer ortEnv.mu.Unlock()

	if ortEnv.initialized {
		if ortEnv.libraryPath != libPath {
			return nil, fmt.Errorf("%w: environment uses %s, refusing to load %s",
				ErrLibraryInUse, ortEnv.libraryPath, libPath)
		}
	} else {
		ort.SetSharedLibraryPath(libPath)
		if err := ort.InitializeEnvironment(); err != nil {
			return nil, fmt.Errorf("%w: loading %s: %w", ErrLibraryUnavailable, libPath, err)
		}
		ortEnv.initialized = true
		ortEnv.libraryPath = libPath
	}

	rec, err := newRecognizerSession(modelPath)
	if err != nil {
		// The environment stays loaded for the application lifetime; a failed
		// session load does not tear it down.
		return nil, err
	}
	ortEnv.refs++
	return rec, nil
}

// newRecognizerSession loads the model and assembles the charset. It runs
// with ortEnv.mu held, so the environment cannot be torn down underneath it.
func newRecognizerSession(modelPath string) (*Recognizer, error) {
	opts, err := ort.NewSessionOptions()
	if err != nil {
		return nil, fmt.Errorf("creating ONNX runtime session options: %w", err)
	}
	defer opts.Destroy()
	if err := opts.SetIntraOpNumThreads(ortIntraOpThreads); err != nil {
		return nil, fmt.Errorf("setting intra-op threads: %w", err)
	}
	if err := opts.SetInterOpNumThreads(ortInterOpThreads); err != nil {
		return nil, fmt.Errorf("setting inter-op threads: %w", err)
	}

	// Inspecting the model loads it into a temporary session inside the
	// binding, so the model file is parsed twice at startup. That cost is
	// accepted to validate the fixed layout (one float32 [N,3,48,W] input,
	// one [N,T,C] output with a static class dimension) and to learn the
	// input/output names instead of hard-coding them.
	inputs, outputs, err := ort.GetInputOutputInfoWithOptions(modelPath, opts)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrModelInvalid, modelPath, err)
	}
	if len(inputs) != 1 || len(outputs) != 1 {
		return nil, fmt.Errorf("%w: %s: expected 1 input and 1 output, got %d and %d",
			ErrModelInvalid, modelPath, len(inputs), len(outputs))
	}
	in, out := inputs[0], outputs[0]
	if in.OrtValueType != ort.ONNXTypeTensor || out.OrtValueType != ort.ONNXTypeTensor {
		return nil, fmt.Errorf("%w: %s: input/output is not a tensor", ErrModelInvalid, modelPath)
	}
	if in.DataType != ort.TensorElementDataTypeFloat || out.DataType != ort.TensorElementDataTypeFloat {
		return nil, fmt.Errorf("%w: %s: input/output is not float32 (input %s, output %s)",
			ErrModelInvalid, modelPath, in.DataType, out.DataType)
	}
	// Input must be [N, 3, 48, W] with N and W dynamic (T3: 输入 x, float32,
	// [N, 3, 48, W]). Accept static or dynamic N/W, but the channel count and
	// height are fixed by the model.
	if len(in.Dimensions) != 4 || in.Dimensions[1] != 3 || in.Dimensions[2] != inputHeight {
		return nil, fmt.Errorf("%w: %s: input shape is %s, want [N,3,%d,W]",
			ErrModelInvalid, modelPath, in.Dimensions, inputHeight)
	}
	// Output must be [N, T, C] with a static class dimension C; C backs the
	// character-table length check below.
	if len(out.Dimensions) != 3 || out.Dimensions[2] <= 0 {
		return nil, fmt.Errorf("%w: %s: output shape is %s, want [N,T,C] with static C",
			ErrModelInvalid, modelPath, out.Dimensions)
	}

	session, err := ort.NewDynamicAdvancedSession(modelPath,
		[]string{in.Name}, []string{out.Name}, opts)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrModelInvalid, modelPath, err)
	}

	meta, err := session.GetModelMetadata()
	if err != nil {
		session.Destroy()
		return nil, fmt.Errorf("%w: %s: reading model metadata: %w", ErrModelInvalid, modelPath, err)
	}
	character, ok, err := meta.LookupCustomMetadataMap("character")
	meta.Destroy()
	if err != nil {
		session.Destroy()
		return nil, fmt.Errorf("%w: %s: reading character metadata: %w", ErrModelInvalid, modelPath, err)
	}
	if !ok {
		session.Destroy()
		return nil, fmt.Errorf("%w: %s: model has no \"character\" metadata",
			ErrModelInvalid, modelPath)
	}

	charset := buildCharset(character)
	classes := int(out.Dimensions[2])
	// The table must cover exactly the model's output classes: index 0 is the
	// CTC blank, the metadata entries follow unchanged, and a single space is
	// appended at the end (T3: 字符顺序). For the pinned PP-OCRv6 small model
	// both sides are 18,710.
	if len(charset) != classes {
		session.Destroy()
		return nil, fmt.Errorf("%w: table has %d entries (blank + %d metadata lines + space) but the model outputs %d classes",
			ErrCharsetMismatch, len(charset), len(charset)-2, classes)
	}

	return &Recognizer{
		session:    session,
		inputName:  in.Name,
		outputName: out.Name,
		classes:    classes,
		charset:    charset,
	}, nil
}

// Close releases this recognizer's model session. It waits for in-flight
// [Recognizer.Recognize] calls, so cancel them first if the wait must be
// bounded. The process-wide ONNX Runtime environment is not affected: it
// stays loaded for the application lifetime and is destroyed only by
// [Shutdown]. Close is idempotent.
func (r *Recognizer) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	err := r.session.Destroy()

	ortEnv.mu.Lock()
	defer ortEnv.mu.Unlock()
	ortEnv.refs--
	return err
}

// Shutdown destroys the process-wide ONNX Runtime environment and unloads
// the native library. The environment belongs to the application's lifetime
// (docs/specs/scanner-architecture.md §4): keep recognizers' open/close
// cycles cheap by leaving the environment loaded between scans, and call
// Shutdown once when recognition is permanently done, typically shortly
// before process exit.
//
// Shutdown returns an error wrapping [ErrEnvironmentInUse] while recognizers
// are still open; close them first. It is a no-op returning nil when the
// environment is not initialized. After Shutdown, [New] re-initializes the
// environment on demand (reloading the native library); repeated
// Shutdown/New cycles are supported but each cycle pays the full library
// load cost, so applications should not shutdown per scan.
func Shutdown() error {
	ortEnv.mu.Lock()
	defer ortEnv.mu.Unlock()
	if !ortEnv.initialized {
		return nil
	}
	if ortEnv.refs > 0 {
		return fmt.Errorf("%w: %d recognizer(s) still open", ErrEnvironmentInUse, ortEnv.refs)
	}
	ortEnv.initialized = false
	ortEnv.libraryPath = ""
	if err := ort.DestroyEnvironment(); err != nil {
		return fmt.Errorf("destroying ONNX runtime environment: %w", err)
	}
	return nil
}

// Recognize preprocesses and recognizes a single text-line crop.
//
// The input is a Go image of one text line (produced upstream by the game
// adapter from a screenshot region); arbitrary page screenshots are not
// valid input. Cancellation: each call uses its own RunOptions; when ctx is
// canceled the run is terminated via the ORT terminate flag, and this method
// returns only after the inference call has actually returned and the run's
// tensors and options have been released — no goroutine is abandoned to
// pretend the work stopped (docs/specs/t3-ocr-runtime-model-samples.md
// §绑定与运行库的实际交接).
func (r *Recognizer) Recognize(ctx context.Context, img image.Image) (Result, error) {
	if img == nil {
		return Result{}, fmt.Errorf("%w: image is nil", ErrInvalidImage)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return Result{}, ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return Result{}, fmt.Errorf("recognition: canceled before inference: %w", err)
	}

	input, width, err := preprocess(img)
	if err != nil {
		return Result{}, err
	}
	inputTensor, err := ort.NewTensor(ort.Shape{1, 3, inputHeight, int64(width)}, input)
	if err != nil {
		return Result{}, fmt.Errorf("recognition: creating input tensor: %w", err)
	}
	runOpts, err := ort.NewRunOptions()
	if err != nil {
		inputTensor.Destroy()
		return Result{}, fmt.Errorf("recognition: creating run options: %w", err)
	}

	// Watch for cancellation while the (blocking) inference call runs. When
	// ctx fires, set the terminate flag on this run's options; the inference
	// call below then fails promptly. The watcher exits deterministically and
	// is joined before any release, so Terminate can never race with
	// RunOptions.Destroy. A fresh RunOptions per call keeps the sticky
	// terminate flag from leaking into later runs.
	runDone := make(chan struct{})
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		select {
		case <-ctx.Done():
			_ = runOpts.Terminate()
		case <-runDone:
		}
	}()

	outputs := []ort.Value{nil} // let ORT allocate the dynamic-width output
	runErr := r.session.RunWithOptions([]ort.Value{inputTensor}, outputs, runOpts)
	close(runDone)
	<-watchDone

	if runErr != nil {
		inputTensor.Destroy()
		runOpts.Destroy()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return Result{}, fmt.Errorf("recognition: inference canceled: %w (onnx runtime: %v)", ctxErr, runErr)
		}
		return Result{}, fmt.Errorf("recognition: inference failed: %w", runErr)
	}

	out, ok := outputs[0].(*ort.Tensor[float32])
	if !ok {
		inputTensor.Destroy()
		runOpts.Destroy()
		return Result{}, fmt.Errorf("recognition: unexpected output value type %T", outputs[0])
	}
	shape := out.GetShape()
	// The output is [N, T, C] with N=1 and C fixed at load time; T varies with
	// the input width, so allocate and read by the actual shape (T3: 输出).
	if len(shape) != 3 || shape[0] != 1 || int(shape[2]) != r.classes || shape[1] <= 0 {
		out.Destroy()
		inputTensor.Destroy()
		runOpts.Destroy()
		return Result{}, fmt.Errorf("recognition: unexpected output shape %v, want [1,T,%d]", shape, r.classes)
	}
	text, score := decodeCTC(out.GetData(), r.classes, r.charset)

	// Release in the fixed order after the inference call returned: output
	// tensor, input tensor, run options.
	releaseErr := out.Destroy()
	releaseErr = errors.Join(releaseErr, inputTensor.Destroy())
	releaseErr = errors.Join(releaseErr, runOpts.Destroy())
	if releaseErr != nil {
		return Result{}, fmt.Errorf("recognition: releasing run resources: %w", releaseErr)
	}
	return Result{Text: text, Score: score}, nil
}

// requireAbsPath validates that path is absolute and usable, keeping library
// and model loading independent of the working directory.
func requireAbsPath(path, what string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("recognition: %s path is empty", what)
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("recognition: %s path %q must be absolute", what, path)
	}
	return path, nil
}
