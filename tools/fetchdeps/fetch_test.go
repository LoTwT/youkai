package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// digestOf returns the SHA-256 of b as lowercase hex.
func digestOf(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// newTestTool returns a tool working below a throwaway root, downloading
// through srv. Tests never touch the network: every URL points at srv.
func newTestTool(t *testing.T, srv *httptest.Server) *tool {
	t.Helper()
	tt := newTool(t.TempDir())
	tt.client = srv.Client()
	return tt
}

// assertNoLeftovers fails when any temporary file of the tool survived
// below root: ".fetchdeps-*" next to destinations, ".tmp-*" in the cache.
func assertNoLeftovers(t *testing.T, root string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".fetchdeps-") || strings.HasPrefix(d.Name(), ".tmp-") {
			t.Errorf("leftover temporary file: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestEnsureFileDownloadSkipAndCacheReuse(t *testing.T) {
	content := []byte("model-bytes")
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if _, err := w.Write(content); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer srv.Close()
	tt := newTestTool(t, srv)
	dep := dependency{
		name:   "model.onnx",
		url:    srv.URL + "/model.onnx",
		sha256: digestOf(content),
		kind:   kindFile,
		dest:   "models/model.onnx",
	}
	ctx := context.Background()

	if err := tt.ensure(ctx, dep); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if got, err := os.ReadFile(tt.destPath(dep.dest)); err != nil || !bytes.Equal(got, content) {
		t.Fatalf("destination content = %q, %v", got, err)
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("downloads = %d, want 1", n)
	}

	// Everything in place: skip without a request.
	if err := tt.ensure(ctx, dep); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("downloads after skip = %d, want 1", n)
	}

	// A missing destination is restored from the cache, still offline.
	if err := os.Remove(tt.destPath(dep.dest)); err != nil {
		t.Fatal(err)
	}
	if err := tt.ensure(ctx, dep); err != nil {
		t.Fatalf("restore from cache: %v", err)
	}
	if got, err := os.ReadFile(tt.destPath(dep.dest)); err != nil || !bytes.Equal(got, content) {
		t.Fatalf("restored content = %q, %v", got, err)
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("downloads after restore = %d, want 1", n)
	}

	// Even without the cache, an intact destination means no download.
	if err := os.Remove(filepath.Join(tt.root, cacheDir, dep.name)); err != nil {
		t.Fatal(err)
	}
	if err := tt.ensure(ctx, dep); err != nil {
		t.Fatalf("run without cache: %v", err)
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("downloads after cache removal = %d, want 1", n)
	}
}

func TestEnsureDownloadDigestMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write([]byte("tampered-bytes")); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer srv.Close()
	tt := newTestTool(t, srv)
	dep := dependency{
		name:   "model.onnx",
		url:    srv.URL + "/model.onnx",
		sha256: digestOf([]byte("good-bytes")),
		kind:   kindFile,
		dest:   "models/model.onnx",
	}

	err := tt.ensure(context.Background(), dep)
	if err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("want a digest error, got %v", err)
	}
	if _, err := os.Stat(tt.destPath(dep.dest)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("destination exists after failure: %v", err)
	}
	assertNoLeftovers(t, tt.root)
}

func TestEnsureDownloadInterrupted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write([]byte("partial-")); err != nil {
			t.Errorf("write response: %v", err)
		}
		// Drop the connection mid-body.
		panic(http.ErrAbortHandler)
	}))
	defer srv.Close()
	tt := newTestTool(t, srv)
	dep := dependency{
		name:   "model.onnx",
		url:    srv.URL + "/model.onnx",
		sha256: digestOf([]byte("good-bytes")),
		kind:   kindFile,
		dest:   "models/model.onnx",
	}

	if err := tt.ensure(context.Background(), dep); err == nil {
		t.Fatal("want an error from an interrupted download")
	}
	if _, err := os.Stat(tt.destPath(dep.dest)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("destination exists after failure: %v", err)
	}
	assertNoLeftovers(t, tt.root)
}

func TestEnsureDownloadStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()
	tt := newTestTool(t, srv)
	dep := dependency{
		name:   "model.onnx",
		url:    srv.URL + "/model.onnx",
		sha256: digestOf([]byte("good-bytes")),
		kind:   kindFile,
		dest:   "models/model.onnx",
	}

	err := tt.ensure(context.Background(), dep)
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("want a status error, got %v", err)
	}
	assertNoLeftovers(t, tt.root)
}

func TestEnsureRejectsModifiedDestination(t *testing.T) {
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
	}))
	defer srv.Close()
	tt := newTestTool(t, srv)
	dep := dependency{
		name:   "model.onnx",
		url:    srv.URL + "/model.onnx",
		sha256: digestOf([]byte("good-bytes")),
		kind:   kindFile,
		dest:   "models/model.onnx",
	}
	dst := tt.destPath(dep.dest)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("hand-edited"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := tt.ensure(context.Background(), dep)
	if err == nil || !strings.Contains(err.Error(), "exists with digest") {
		t.Fatalf("want an exists-with-digest error, got %v", err)
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("downloads = %d, want 0", n)
	}
	if got, err := os.ReadFile(dst); err != nil || string(got) != "hand-edited" {
		t.Fatalf("destination was overwritten: %q, %v", got, err)
	}
}

func TestEnsureReplacesStaleCache(t *testing.T) {
	content := []byte("good-bytes")
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if _, err := w.Write(content); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer srv.Close()
	tt := newTestTool(t, srv)
	dep := dependency{
		name:   "model.onnx",
		url:    srv.URL + "/model.onnx",
		sha256: digestOf(content),
		kind:   kindFile,
		dest:   "models/model.onnx",
	}
	if err := os.MkdirAll(filepath.Join(tt.root, cacheDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tt.root, cacheDir, dep.name), []byte("stale-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := tt.ensure(context.Background(), dep); err != nil {
		t.Fatalf("stale cache: %v", err)
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("downloads = %d, want 1", n)
	}
	got, err := fileDigest(filepath.Join(tt.root, cacheDir, dep.name))
	if err != nil || got != dep.sha256 {
		t.Fatalf("cache digest after refresh = %q, %v", got, err)
	}
	assertNoLeftovers(t, tt.root)
}

func TestEnsureZipEndToEnd(t *testing.T) {
	files := map[string]string{
		"top/lib/onnxruntime.dll":   "dll-bytes",
		"top/lib/unused.dll":        "not wanted",
		"top/LICENSE":               "license-bytes",
		"top/ThirdPartyNotices.txt": "notices-bytes",
	}
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range files {
		fw, err := w.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	archive := buf.Bytes()

	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if _, err := w.Write(archive); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer srv.Close()
	tt := newTestTool(t, srv)
	dep := dependency{
		name:   "onnxruntime-test.zip",
		url:    srv.URL + "/onnxruntime-test.zip",
		sha256: digestOf(archive),
		kind:   kindZip,
		members: []member{
			{name: "top/lib/onnxruntime.dll", dest: "windows-amd64/onnxruntime/onnxruntime.dll", sha256: digestOf([]byte("dll-bytes"))},
			{name: "top/LICENSE", dest: "windows-amd64/onnxruntime/LICENSE", sha256: digestOf([]byte("license-bytes"))},
			{name: "top/ThirdPartyNotices.txt", dest: "windows-amd64/onnxruntime/ThirdPartyNotices.txt", sha256: digestOf([]byte("notices-bytes"))},
		},
	}
	ctx := context.Background()

	if err := tt.ensure(ctx, dep); err != nil {
		t.Fatalf("first run: %v", err)
	}
	for name, want := range files {
		if name == "top/lib/unused.dll" {
			continue
		}
		member, ok := memberDest(dep, name)
		if !ok {
			t.Fatalf("no member for %s", name)
		}
		got, err := os.ReadFile(tt.destPath(member))
		if err != nil {
			t.Fatalf("%s: %v", member, err)
		}
		if string(got) != want {
			t.Errorf("%s content = %q, want %q", member, got, want)
		}
	}
	if _, err := os.Stat(tt.destPath("windows-amd64/onnxruntime/unused.dll")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("unused member was unpacked: %v", err)
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("downloads = %d, want 1", n)
	}

	if err := tt.ensure(ctx, dep); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("downloads after skip = %d, want 1", n)
	}
}

func TestEnsureTgzEndToEnd(t *testing.T) {
	// The ORT macOS archive prefixes member names with "./" and ships a
	// same-sized duplicate dylib besides the versioned one; both are
	// exercised here.
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range []struct {
		name     string
		typeflag byte
		mode     int64
		content  string
	}{
		{name: "./top/lib/libonnxruntime.1.29.0.dylib", typeflag: tar.TypeReg, mode: 0o755, content: "dylib-bytes"},
		{name: "./top/lib/libonnxruntime.dylib", typeflag: tar.TypeReg, mode: 0o755, content: "duplicate-dylib-bytes"},
		{name: "./top/LICENSE", typeflag: tar.TypeReg, mode: 0o644, content: "license-bytes"},
	} {
		hdr := &tar.Header{Name: e.name, Typeflag: e.typeflag, Mode: e.mode, Size: int64(len(e.content))}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(e.content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	archive := buf.Bytes()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write(archive); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer srv.Close()
	tt := newTestTool(t, srv)
	dep := dependency{
		name:   "onnxruntime-test.tgz",
		url:    srv.URL + "/onnxruntime-test.tgz",
		sha256: digestOf(archive),
		kind:   kindTgz,
		members: []member{
			{name: "top/lib/libonnxruntime.1.29.0.dylib", dest: "darwin-arm64/onnxruntime/libonnxruntime.1.29.0.dylib", sha256: digestOf([]byte("dylib-bytes"))},
			{name: "top/LICENSE", dest: "darwin-arm64/onnxruntime/LICENSE", sha256: digestOf([]byte("license-bytes"))},
		},
	}

	if err := tt.ensure(context.Background(), dep); err != nil {
		t.Fatalf("first run: %v", err)
	}
	dst := tt.destPath("darwin-arm64/onnxruntime/libonnxruntime.1.29.0.dylib")
	got, err := os.ReadFile(dst)
	if err != nil || string(got) != "dylib-bytes" {
		t.Fatalf("dylib content = %q, %v", got, err)
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o755 {
		t.Errorf("dylib mode = %o, want 755", perm)
	}
	if _, err := os.Stat(tt.destPath("darwin-arm64/onnxruntime/libonnxruntime.dylib")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the duplicate dylib was unpacked: %v", err)
	}
}

func TestEnsureMemberDigestMismatchLeavesNothing(t *testing.T) {
	// Two members: the first unpacks fine, the second does not match its
	// pinned digest. The failure must leave no partial file for the second
	// member and no temporary files anywhere; files of the same dependency
	// that were already completed stay in place.
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range map[string]string{
		"top/good.dll": "good-bytes",
		"top/bad.dll":  "actual-bytes",
	} {
		fw, err := w.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	archive := buf.Bytes()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write(archive); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer srv.Close()
	tt := newTestTool(t, srv)
	dep := dependency{
		name:   "test.zip",
		url:    srv.URL + "/test.zip",
		sha256: digestOf(archive),
		kind:   kindZip,
		members: []member{
			{name: "top/good.dll", dest: "windows-amd64/onnxruntime/good.dll", sha256: digestOf([]byte("good-bytes"))},
			{name: "top/bad.dll", dest: "windows-amd64/onnxruntime/bad.dll", sha256: digestOf([]byte("pinned-but-wrong"))},
		},
	}

	err := tt.ensure(context.Background(), dep)
	if err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("want a digest mismatch error, got %v", err)
	}
	if _, err := os.Stat(tt.destPath("windows-amd64/onnxruntime/bad.dll")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("bad.dll exists after failure: %v", err)
	}
	if got, err := os.ReadFile(tt.destPath("windows-amd64/onnxruntime/good.dll")); err != nil || string(got) != "good-bytes" {
		t.Errorf("good.dll should remain complete, got %q, %v", got, err)
	}
	assertNoLeftovers(t, tt.root)
}

// memberDest returns the destination of dep's member name.
func memberDest(dep dependency, name string) (string, bool) {
	for _, m := range dep.members {
		if m.name == name {
			return m.dest, true
		}
	}
	return "", false
}
