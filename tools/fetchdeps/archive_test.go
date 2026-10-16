package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// zipEntry is one entry of a test zip archive. mode's type bits decide the
// entry kind; content is written as the entry body (a symlink's target for
// symlink entries).
type zipEntry struct {
	name    string
	mode    fs.FileMode
	content []byte
}

// writeTestZip writes entries to a zip archive at path.
func writeTestZip(t *testing.T, path string, entries []zipEntry) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := zip.NewWriter(f)
	for _, e := range entries {
		hdr := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		hdr.SetMode(e.mode)
		fw, err := w.CreateHeader(hdr)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write(e.content); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

// tarEntry is one entry of a test tar archive.
type tarEntry struct {
	name     string
	typeflag byte
	mode     int64
	content  []byte
	linkname string
}

// writeTestTgz writes entries to a gzipped tar archive at path.
func writeTestTgz(t *testing.T, path string, entries []tarEntry) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	w := tar.NewWriter(gz)
	for _, e := range entries {
		hdr := &tar.Header{
			Name:     e.name,
			Typeflag: e.typeflag,
			Mode:     e.mode,
			Size:     int64(len(e.content)),
			Linkname: e.linkname,
		}
		if err := w.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(e.content); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestSafeRelPath(t *testing.T) {
	for _, p := range []string{"file.txt", "dir/file.txt", "deep/dir/file", "onnxruntime.dll"} {
		if err := safeRelPath(p); err != nil {
			t.Errorf("safeRelPath(%q) = %v, want nil", p, err)
		}
	}
	for _, p := range []string{
		"", ".", "..", "/etc/passwd", "../evil", "dir/../evil", "dir/../../evil",
		"dir//file", "./file", "dir/.", "dir/", `C:\evil`, `dir\file`, "C:evil",
	} {
		if err := safeRelPath(p); err == nil {
			t.Errorf("safeRelPath(%q) = nil, want an error", p)
		}
	}
}

func TestOpenArchiveMemberRejectsUnsafeNames(t *testing.T) {
	// The guard fires before the archive is opened, so the path does not
	// even have to exist.
	for _, name := range []string{"", "..", "../evil.txt", "/etc/passwd", `C:\evil`, "dir/../../evil"} {
		if _, err := openArchiveMember(kindZip, "unused.zip", name); err == nil {
			t.Errorf("openArchiveMember(kindZip, %q) = nil, want an error", name)
		}
		if _, err := openArchiveMember(kindTgz, "unused.tgz", name); err == nil {
			t.Errorf("openArchiveMember(kindTgz, %q) = nil, want an error", name)
		}
	}
}

func TestOpenZipMember(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.zip")
	writeTestZip(t, path, []zipEntry{
		{name: "dir/", mode: fs.ModeDir | 0o755},
		{name: "dir/file.txt", mode: 0o644, content: []byte("hello")},
		{name: "./other.txt", mode: 0o644, content: []byte("dot-prefixed")},
	})

	m, err := openZipMember(path, "dir/file.txt")
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(m)
	m.Close()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Errorf("member content = %q, want %q", got, "hello")
	}

	// Entry names are matched after cleaning, so a "./" prefix is fine.
	m, err = openZipMember(path, "other.txt")
	if err != nil {
		t.Fatal(err)
	}
	got, err = io.ReadAll(m)
	m.Close()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "dot-prefixed" {
		t.Errorf("member content = %q, want %q", got, "dot-prefixed")
	}

	if _, err := openZipMember(path, "missing.txt"); err == nil {
		t.Error("missing member: want an error")
	}
	if _, err := openZipMember(path, "dir"); err == nil {
		t.Error("directory entry: want an error")
	}
}

func TestOpenZipMemberRejectsSymlink(t *testing.T) {
	path := filepath.Join(t.TempDir(), "link.zip")
	writeTestZip(t, path, []zipEntry{
		{name: "link", mode: fs.ModeSymlink | 0o777, content: []byte("../outside-target")},
		{name: "file", mode: 0o644, content: []byte("ok")},
	})
	if _, err := openZipMember(path, "link"); err == nil {
		t.Error("symlink member: want an error")
	}
}

func TestOpenTgzMember(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.tgz")
	writeTestTgz(t, path, []tarEntry{
		{name: "./top/", typeflag: tar.TypeDir, mode: 0o755},
		{name: "./top/lib/x.dylib", typeflag: tar.TypeReg, mode: 0o755, content: []byte("dylib-bytes")},
		{name: "./top/lib/link", typeflag: tar.TypeSymlink, mode: 0o777, linkname: "x.dylib"},
		{name: "./top/lib/hard", typeflag: tar.TypeLink, mode: 0o755, linkname: "x.dylib"},
	})

	// The manifest-style name has no "./" prefix; the archive's does.
	m, err := openTgzMember(path, "top/lib/x.dylib")
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(m)
	m.Close()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "dylib-bytes" {
		t.Errorf("member content = %q, want %q", got, "dylib-bytes")
	}
	if perm := m.mode.Perm(); perm != 0o755 { // mode stays readable after Close
		t.Errorf("member mode = %o, want 755", perm)
	}

	for _, name := range []string{"top/lib/link", "top/lib/hard"} {
		if _, err := openTgzMember(path, name); err == nil {
			t.Errorf("%s: want an error for a non-regular member", name)
		}
	}
	if _, err := openTgzMember(path, "top/missing"); err == nil {
		t.Error("missing member: want an error")
	}
}
