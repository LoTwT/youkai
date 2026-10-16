package app

import (
	"path/filepath"
	"strings"
	"testing"
)

// The layout composers are pure: they take the OS base directories as
// inputs, so their rules are tested with fixed bases on any OS. Expected
// values join with filepath like the composers do, which is exact on each
// OS and still checks names and nesting everywhere.

// inside reports whether dir is a strict subdirectory of base.
func inside(base, dir string) bool {
	rel, err := filepath.Rel(base, dir)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, "../")
}

func TestWindowsRoots(t *testing.T) {
	// What FOLDERID_LocalAppData resolves to; the space keeps the layout
	// honest about quoting.
	const localAppData = `C:\Users\youkai user\AppData\Local`
	p := windowsRoots(localAppData, "Youkai")
	if p.Data != filepath.Join(localAppData, "Youkai") {
		t.Errorf("data = %q, want %q", p.Data, filepath.Join(localAppData, "Youkai"))
	}
	if p.Cache != filepath.Join(localAppData, "Youkai", "cache") {
		t.Errorf("cache = %q, want %q", p.Cache, filepath.Join(localAppData, "Youkai", "cache"))
	}
	if p.Logs != filepath.Join(localAppData, "Youkai", "logs") {
		t.Errorf("logs = %q, want %q", p.Logs, filepath.Join(localAppData, "Youkai", "logs"))
	}
	// Cache and logs nest under the data root, so clearing the cache
	// subtree can never touch anything else the app keeps.
	if rel, err := filepath.Rel(p.Data, p.Cache); err != nil || rel != "cache" {
		t.Errorf("cache %q is not %q below data %q (rel %q, err %v)", p.Cache, "cache", p.Data, rel, err)
	}
	if rel, err := filepath.Rel(p.Data, p.Logs); err != nil || rel != "logs" {
		t.Errorf("logs %q is not %q below data %q (rel %q, err %v)", p.Logs, "logs", p.Data, rel, err)
	}
	if p.Resources != "" {
		t.Errorf("resources = %q, want empty: the layout does not resolve resources", p.Resources)
	}
}

func TestAppleRoots(t *testing.T) {
	const (
		appSupport = "/Users/youkai user/Library/Application Support"
		caches     = "/Users/youkai user/Library/Caches"
		logs       = "/Users/youkai user/Library/Logs"
	)
	p := appleRoots(appSupport, caches, logs, "Youkai")
	if p.Data != filepath.Join(appSupport, "Youkai") {
		t.Errorf("data = %q, want %q", p.Data, filepath.Join(appSupport, "Youkai"))
	}
	if p.Cache != filepath.Join(caches, "Youkai") {
		t.Errorf("cache = %q, want %q", p.Cache, filepath.Join(caches, "Youkai"))
	}
	if p.Logs != filepath.Join(logs, "Youkai") {
		t.Errorf("logs = %q, want %q", p.Logs, filepath.Join(logs, "Youkai"))
	}
	// The macOS cache lives in the system cache location beside the data
	// root, not inside it.
	if inside(p.Data, p.Cache) {
		t.Errorf("cache %q must not live inside data %q", p.Cache, p.Data)
	}
	if p.Resources != "" {
		t.Errorf("resources = %q, want empty: the layout does not resolve resources", p.Resources)
	}
}

// The development app ("Youkai Dev", the name `go tool mygo dev` builds)
// and the installed app ("Youkai") must not share any root, on either OS
// layout.
func TestDevRootsDifferFromRelease(t *testing.T) {
	const release, dev = "Youkai", "Youkai Dev"
	pairs := []struct{ release, dev Paths }{
		{
			windowsRoots(`C:\Users\youkai user\AppData\Local`, release),
			windowsRoots(`C:\Users\youkai user\AppData\Local`, dev),
		},
		{
			appleRoots("/Users/youkai user/Library/Application Support", "/Users/youkai user/Library/Caches", "/Users/youkai user/Library/Logs", release),
			appleRoots("/Users/youkai user/Library/Application Support", "/Users/youkai user/Library/Caches", "/Users/youkai user/Library/Logs", dev),
		},
	}
	for _, pair := range pairs {
		if pair.release.Data == pair.dev.Data {
			t.Errorf("data roots collide: %q", pair.release.Data)
		}
		if pair.release.Cache == pair.dev.Cache {
			t.Errorf("cache roots collide: %q", pair.release.Cache)
		}
		if pair.release.Logs == pair.dev.Logs {
			t.Errorf("log roots collide: %q", pair.release.Logs)
		}
	}
}

func TestResolveRejectsBadOptions(t *testing.T) {
	cases := []struct {
		name string
		opts Options
	}{
		{"empty name", Options{Resources: t.TempDir()}},
		{"name with separator", Options{Name: "You/kai", Resources: t.TempDir()}},
		{"name with windows separator", Options{Name: `You\kai`, Resources: t.TempDir()}},
		{"parent name", Options{Name: "..", Resources: t.TempDir()}},
		{"empty resources", Options{Name: "Youkai"}},
		{"relative resources", Options{Name: "Youkai", Resources: "resources"}},
	}
	for _, tc := range cases {
		if _, err := Resolve(tc.opts); err == nil {
			t.Errorf("%s: Resolve(%+v) succeeded, want error", tc.name, tc.opts)
		}
	}
}

// Resolve against the real environment of the host OS. The exact per-OS
// layout is covered by the composer tests; this exercises the real lookup
// path and requires distinct roots (validate already proved them absolute
// and non-empty).
func TestResolveCurrentOS(t *testing.T) {
	paths, err := Resolve(Options{Name: "Youkai", Resources: t.TempDir()})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if paths.Data == paths.Cache || paths.Data == paths.Logs || paths.Cache == paths.Logs {
		t.Errorf("roots collide: %+v", paths)
	}
	if paths.Resources == "" {
		t.Error("resources root is empty")
	}
}
