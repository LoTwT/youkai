// Command fetchdeps installs youkai's pinned third-party resources: the
// ONNX Runtime 1.29.0 native libraries for both release platforms and the
// PP-OCRv6 recognition model. Sources and SHA-256 digests are fixed in
// deps.go; downloads land in an ignored cache and in the MyGo resources
// tree that ships with the app. No API key is needed.
//
// Usage, from the repository root:
//
//	go run ./tools/fetchdeps                    # every platform
//	go run ./tools/fetchdeps -platform current  # only this machine's platform
//	go run ./tools/fetchdeps -platform windows/amd64
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
)

func main() {
	log.SetFlags(0)
	platform := flag.String("platform", "all", "dependencies to fetch: all (default), current (this machine's goos/goarch), or a goos/goarch such as windows/amd64")
	flag.Parse()
	if flag.NArg() != 0 {
		log.Fatalf("fetchdeps takes no arguments, got %q", flag.Args())
	}
	root, err := repoRoot(".")
	if err != nil {
		log.Fatal(err)
	}
	// Interrupts cancel in-flight downloads; nothing is left half-written
	// either way, because every file is renamed into place only after its
	// digest is verified.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, newTool(root), *platform, runtime.GOOS+"/"+runtime.GOARCH); err != nil {
		log.Fatal(err)
	}
}

// run fetches the dependencies that want selects, stopping at the first
// failure so a digest problem never goes unnoticed.
func run(ctx context.Context, t *tool, want, host string) error {
	selected, err := selectDeps(deps, want, host)
	if err != nil {
		return err
	}
	for _, dep := range selected {
		if err := t.ensure(ctx, dep); err != nil {
			return fmt.Errorf("%s: %w", dep.name, err)
		}
	}
	return nil
}

// repoRoot walks up from dir to the nearest directory holding a go.mod: the
// repository root, below which resources/ and the download cache live.
func repoRoot(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		if info, err := os.Stat(filepath.Join(abs, "go.mod")); err == nil && !info.IsDir() {
			return abs, nil
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return "", fmt.Errorf("no go.mod found at or above %s; run fetchdeps from inside the repository", dir)
		}
		abs = parent
	}
}
