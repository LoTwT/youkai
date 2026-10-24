package app

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/LoTwT/youkai/internal/ocrlayout"
)

// The packaged loader and fetchdeps share internal/ocrlayout. This test
// pins the merged resource layout the app actually resolves: the runtime
// under onnxruntime/ and the shared model under models/.
func TestOCRResourcesUseSharedLayout(t *testing.T) {
	lib := ocrlayout.RuntimeLibName(runtime.GOOS)
	if lib == "" {
		t.Skipf("no pinned ONNX runtime artifact for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	root := t.TempDir()
	res, err := OCRResources(root)
	if err != nil {
		t.Fatalf("OCRResources: %v", err)
	}
	if want := filepath.Join(root, "onnxruntime", lib); res.LibraryPath != want {
		t.Fatalf("library path = %q, want %q", res.LibraryPath, want)
	}
	if want := filepath.Join(root, "models", ocrlayout.ModelName); res.ModelPath != want {
		t.Fatalf("model path = %q, want %q", res.ModelPath, want)
	}
}
