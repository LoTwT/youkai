package recognition

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The integration tests load the real ONNX Runtime library and the pinned
// PP-OCRv6 model from the fetchdeps-managed resources directory. That
// directory is deliberately absent on CI (PR checks do not download OCR
// resources), so every test here skips with a reason when the artifacts are
// missing. Run `go run ./tools/fetchdeps -platform current` first.

// resourcePaths resolves the absolute library and model paths for the current
// platform and reports whether both exist.
func resourcePaths(t *testing.T) (libPath, modelPath string, ok bool) {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("resolving repository root: %v", err)
	}
	var libRel string
	switch runtime.GOOS {
	case "darwin":
		// Load the versioned file by its exact name; do not depend on an
		// unversioned symlink being present (T3 §实际原生文件与打包成本).
		libRel = filepath.Join("resources", "darwin-arm64", "onnxruntime", "libonnxruntime.1.29.0.dylib")
	case "windows":
		libRel = filepath.Join("resources", "windows-amd64", "onnxruntime", "onnxruntime.dll")
	default:
		t.Skipf("no pinned ONNX runtime artifact for %s/%s in this repository", runtime.GOOS, runtime.GOARCH)
	}
	libPath = filepath.Join(root, libRel)
	modelPath = filepath.Join(root, "resources", "models", "PP-OCRv6_rec_small.onnx")
	if _, err := os.Stat(libPath); err != nil {
		t.Skipf("ONNX runtime library missing (%s); run go run ./tools/fetchdeps -platform current: %v", libPath, err)
	}
	if _, err := os.Stat(modelPath); err != nil {
		t.Skipf("recognition model missing (%s); run go run ./tools/fetchdeps: %v", modelPath, err)
	}
	return libPath, modelPath, true
}

// TestIntegration drives the recognizer against the real library and model.
// The failure-path cases do not depend on execution order: the missing-file
// checks run before any environment state is consulted.
func TestIntegration(t *testing.T) {
	libPath, modelPath, ok := resourcePaths(t)
	if !ok {
		t.Fatal("unreachable")
	}

	t.Run("MissingLibrary", func(t *testing.T) {
		_, err := New(Resources{
			LibraryPath: filepath.Join(filepath.Dir(libPath), "does-not-exist.dylib"),
			ModelPath:   modelPath,
		})
		if !errors.Is(err, ErrLibraryUnavailable) {
			t.Fatalf("New error = %v, want ErrLibraryUnavailable", err)
		}
		if !strings.Contains(err.Error(), "does-not-exist") {
			t.Fatalf("error does not name the missing library: %v", err)
		}
	})

	t.Run("MissingModel", func(t *testing.T) {
		_, err := New(Resources{
			LibraryPath: libPath,
			ModelPath:   filepath.Join(filepath.Dir(modelPath), "does-not-exist.onnx"),
		})
		if !errors.Is(err, ErrModelUnavailable) {
			t.Fatalf("New error = %v, want ErrModelUnavailable", err)
		}
	})

	t.Run("CorruptModel", func(t *testing.T) {
		corrupt := filepath.Join(t.TempDir(), "corrupt.onnx")
		// Not an ONNX file: readable, non-empty, rejected by the runtime.
		if err := os.WriteFile(corrupt, []byte("this is not an onnx model, but it is long enough to look like a file"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := New(Resources{LibraryPath: libPath, ModelPath: corrupt})
		if !errors.Is(err, ErrModelInvalid) {
			t.Fatalf("New error = %v, want ErrModelInvalid", err)
		}
		if !strings.Contains(err.Error(), corrupt) {
			t.Fatalf("error does not name the model path: %v", err)
		}
	})

	t.Run("LoadAndRecognize", func(t *testing.T) {
		rec, err := New(Resources{LibraryPath: libPath, ModelPath: modelPath})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		defer rec.Close()

		// Fixed-model facts from T3 (输入/输出 and 字符顺序).
		if rec.inputName != "x" || rec.outputName != "fetch_name_0" {
			t.Logf("note: model io names are %q/%q (T3 documents x/fetch_name_0)", rec.inputName, rec.outputName)
		}
		if rec.classes != 18710 {
			t.Fatalf("output classes = %d, want 18710 (T3)", rec.classes)
		}
		if len(rec.charset) != rec.classes {
			t.Fatalf("charset length %d != classes %d", len(rec.charset), rec.classes)
		}

		recognizeTestdata(t, rec)

		t.Run("RepeatedRuns", func(t *testing.T) {
			img := loadTestImage(t, "disc1_name.png")
			var first Result
			for i := 0; i < 10; i++ {
				got, err := rec.Recognize(context.Background(), img)
				if err != nil {
					t.Fatalf("run %d: %v", i, err)
				}
				if i == 0 {
					first = got
					continue
				}
				if got != first {
					t.Fatalf("run %d result %+v differs from first %+v", i, got, first)
				}
			}
		})

		t.Run("CancelDuringRun", func(t *testing.T) {
			// A very wide line makes one inference take long enough for the
			// cancellation to land mid-run. The exact width is not important;
			// it only has to produce a slow run on this machine.
			for _, w := range []int{800, 1600, 3200} {
				img := image.NewNRGBA(image.Rect(0, 0, w, 8))
				ctx, cancel := context.WithCancel(context.Background())
				time.AfterFunc(20*time.Millisecond, cancel)
				res, err := rec.Recognize(ctx, img)
				if errors.Is(err, context.Canceled) {
					// Cancellation landed mid-run and the call returned. The
					// session must remain usable and uncontaminated for later
					// runs (fresh RunOptions per call).
					_ = res
					probe := loadTestImage(t, "disc1_level.png")
					got, perr := rec.Recognize(context.Background(), probe)
					if perr != nil {
						t.Fatalf("recognize after cancel: %v", perr)
					}
					if got.Text == "" {
						t.Fatalf("recognize after cancel returned empty text")
					}
					return
				}
				if err == nil {
					// The run finished before the 20 ms cancel fired; retry
					// with a wider line.
					continue
				}
				t.Fatalf("unexpected error at width %d: %v", w, err)
			}
			t.Fatal("could not exercise mid-run cancellation: every trial completed within 20 ms")
		})

		t.Run("CancelBeforeRun", func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			_, err := rec.Recognize(ctx, loadTestImage(t, "disc1_name.png"))
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("error = %v, want context.Canceled", err)
			}
		})
	})

	t.Run("ClosedRecognizer", func(t *testing.T) {
		rec, err := New(Resources{LibraryPath: libPath, ModelPath: modelPath})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		if err := rec.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		if err := rec.Close(); err != nil {
			t.Fatalf("second Close: %v (want idempotent nil)", err)
		}
		if _, err := rec.Recognize(context.Background(), loadTestImage(t, "disc1_name.png")); !errors.Is(err, ErrClosed) {
			t.Fatalf("Recognize after Close error = %v, want ErrClosed", err)
		}
	})

	t.Run("ReopenAfterClose", func(t *testing.T) {
		// Recognizer close/reopen cycles are the per-scan pattern: Close
		// releases only the session, and a fresh New reuses the still-loaded
		// environment (the "重开" lifecycle from M1's pass criteria).
		first, err := New(Resources{LibraryPath: libPath, ModelPath: modelPath})
		if err != nil {
			t.Fatalf("first New: %v", err)
		}
		img := loadTestImage(t, "disc1_name.png")
		want, err := first.Recognize(context.Background(), img)
		if err != nil {
			t.Fatalf("recognize before close: %v", err)
		}
		if err := first.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}

		second, err := New(Resources{LibraryPath: libPath, ModelPath: modelPath})
		if err != nil {
			t.Fatalf("reopen after close: %v", err)
		}
		defer second.Close()
		got, err := second.Recognize(context.Background(), img)
		if err != nil {
			t.Fatalf("recognize after reopen: %v", err)
		}
		if got != want {
			t.Fatalf("result after reopen = %+v, want %+v", got, want)
		}
	})

	t.Run("ShutdownLifecycle", func(t *testing.T) {
		// The environment belongs to the application lifetime: Shutdown is
		// explicit, refuses while recognizers are open, is idempotent, and a
		// later New may re-initialize the environment.
		rec, err := New(Resources{LibraryPath: libPath, ModelPath: modelPath})
		if err != nil {
			t.Fatalf("New: %v", err)
		}

		if err := Shutdown(); !errors.Is(err, ErrEnvironmentInUse) {
			t.Fatalf("Shutdown with open recognizer error = %v, want ErrEnvironmentInUse", err)
		}

		img := loadTestImage(t, "disc1_name.png")
		want, err := rec.Recognize(context.Background(), img)
		if err != nil {
			t.Fatalf("recognize: %v", err)
		}
		if err := rec.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}

		if err := Shutdown(); err != nil {
			t.Fatalf("Shutdown after Close: %v", err)
		}
		if err := Shutdown(); err != nil {
			t.Fatalf("second Shutdown (idempotent): %v", err)
		}

		// New after Shutdown re-initializes the environment on demand.
		reopened, err := New(Resources{LibraryPath: libPath, ModelPath: modelPath})
		if err != nil {
			t.Fatalf("New after Shutdown: %v", err)
		}
		got, err := reopened.Recognize(context.Background(), img)
		if err != nil {
			t.Fatalf("recognize after re-init: %v", err)
		}
		if got != want {
			t.Fatalf("result after re-init = %+v, want %+v", got, want)
		}
		if err := reopened.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		if err := Shutdown(); err != nil {
			t.Fatalf("final Shutdown: %v", err)
		}
	})
}

// TestSelfCheck runs the install-time self-check against the real artifacts,
// exactly as the entry point's YOUKAI_SELFTEST mode does on an installed app.
func TestSelfCheck(t *testing.T) {
	libPath, modelPath, ok := resourcePaths(t)
	if !ok {
		t.Fatal("unreachable")
	}
	res := Resources{LibraryPath: libPath, ModelPath: modelPath}
	got, err := SelfCheck(res)
	if err != nil {
		t.Fatalf("SelfCheck: %v", err)
	}
	if got.Text != selfCheckWant {
		t.Fatalf("SelfCheck text = %q, want %q", got.Text, selfCheckWant)
	}
	if got.Score <= 0 {
		t.Fatalf("SelfCheck score = %v, want > 0", got.Score)
	}
	// The self-check leaves the environment loaded, as the application's
	// first scan does; the entry point's self-test mode calls Shutdown after
	// it, and so does this test to leave the process clean.
	if err := Shutdown(); err != nil {
		t.Fatalf("Shutdown after SelfCheck: %v", err)
	}
}

// testLine is one row of a recognition fixture manifest.
type testLine struct {
	file     string
	source   string // sha256 of the source screenshot
	x0, y0   int
	x1, y1   int
	expected string
}

// fixtureGroup is one client's crop set: the manifest file and the
// directory holding the crops, both relative to testdata/.
type fixtureGroup struct {
	name     string
	manifest string
	dir      string
}

// fixtureGroups are the crop sets the integration test recognizes: the
// Windows full-client sample (rects in the 1280×739 uploaded screenshot)
// and the macOS cloud-client sample (rects in the 1920×1080 rawstream
// frame); provenance per group in the group's README.
var fixtureGroups = []fixtureGroup{
	{name: "windows-local", manifest: "lines.tsv", dir: "."},
	{name: "macos-cloud", manifest: filepath.Join("macos-cloud", "lines.tsv"), dir: "macos-cloud"},
}

// loadTestLines parses one fixture manifest (file, source sha256, x0, y0,
// x1, y1, expected text).
func loadTestLines(t *testing.T, manifest string) []testLine {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", manifest))
	if err != nil {
		t.Fatalf("reading testdata/%s: %v", manifest, err)
	}
	var lines []testLine
	for i, row := range strings.Split(string(raw), "\n") {
		if row == "" || strings.HasPrefix(row, "#") {
			continue
		}
		f := strings.Split(row, "\t")
		if len(f) != 7 {
			t.Fatalf("%s row %d has %d fields, want 7: %q", manifest, i+1, len(f), row)
		}
		var tl testLine
		tl.file, tl.source, tl.expected = f[0], f[1], f[6]
		if _, err := fmt.Sscanf(f[2], "%d", &tl.x0); err != nil {
			t.Fatalf("%s row %d: bad x0: %v", manifest, i+1, err)
		}
		if _, err := fmt.Sscanf(f[3], "%d", &tl.y0); err != nil {
			t.Fatalf("%s row %d: bad y0: %v", manifest, i+1, err)
		}
		if _, err := fmt.Sscanf(f[4], "%d", &tl.x1); err != nil {
			t.Fatalf("%s row %d: bad x1: %v", manifest, i+1, err)
		}
		if _, err := fmt.Sscanf(f[5], "%d", &tl.y1); err != nil {
			t.Fatalf("%s row %d: bad y1: %v", manifest, i+1, err)
		}
		lines = append(lines, tl)
	}
	if len(lines) == 0 {
		t.Fatalf("testdata/%s has no rows", manifest)
	}
	return lines
}

// loadGroupImage opens one crop of a fixture group.
func loadGroupImage(t *testing.T, group fixtureGroup, name string) image.Image {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", group.dir, name))
	if err != nil {
		t.Fatalf("opening %s/%s: %v", group.dir, name, err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatalf("decoding %s/%s: %v", group.dir, name, err)
	}
	return img
}

func loadTestImage(t *testing.T, name string) image.Image {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("opening %s: %v", name, err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatalf("decoding %s: %v", name, err)
	}
	return img
}

// exactAsserted reports whether a crop is asserted to decode to the
// expected text exactly: the set-name and level lines of both fixture
// groups, which decode cleanly on the development machine (verified with
// the pinned model for all 33 discs — 6 Windows + 27 macOS). The stat
// lines decode the same characters but without the space the two-column
// layout shows to a human reader (the model does not emit the wide
// inter-column gap as a space), so they are reported with a
// space-insensitive comparison instead of asserted. This milestone proves
// the pipeline (model + preprocessing + decoding) runs and yields
// readable text on both platforms — not an accuracy gate (that is T3 §4).
func exactAsserted(file string) bool {
	_, role, _ := strings.Cut(file, "_")
	role = strings.TrimSuffix(role, ".png")
	return role == "name" || role == "level"
}

// recognizeTestdata recognizes every crop of every fixture group and
// reports the full expected/actual/score table; the macOS cloud-client
// crops come from compressed cloud video and their stat lines are
// reported, not asserted.
func recognizeTestdata(t *testing.T, rec *Recognizer) {
	t.Helper()
	for _, group := range fixtureGroups {
		lines := loadTestLines(t, group.manifest)
		t.Logf("group %s: %d lines", group.name, len(lines))
		for _, tl := range lines {
			res, err := rec.Recognize(context.Background(), loadGroupImage(t, group, tl.file))
			if err != nil {
				t.Errorf("%s/%s: recognize: %v", group.dir, tl.file, err)
				continue
			}
			exact := res.Text == tl.expected
			noSpace := strings.ReplaceAll(res.Text, " ", "") == strings.ReplaceAll(tl.expected, " ", "")
			t.Logf("%-18s %-24s %-24s %6.4f  %-7t %t", tl.file, tl.expected, res.Text, res.Score, exact, noSpace)
			if exactAsserted(tl.file) && !exact {
				t.Errorf("%s: decoded %q, want %q", tl.file, res.Text, tl.expected)
			}
			if !noSpace {
				t.Errorf("%s: decoded %q, want %q (space-insensitive mismatch)", tl.file, res.Text, tl.expected)
			}
		}
	}
}
