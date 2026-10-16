package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
)

// safeRelPath rejects paths that could point outside the tree files are
// unpacked into: absolute paths, parent-directory or dot segments,
// backslashes (Windows separators) and drive-letter colons. It is applied
// to the pinned archive member and destination names, so that not even a
// manifest typo can make an install escape resources/.
func safeRelPath(p string) error {
	switch {
	case p == "":
		return fmt.Errorf("path is empty")
	case strings.ContainsAny(p, `\:`):
		return fmt.Errorf("path %q contains a backslash or drive-letter colon", p)
	case strings.HasPrefix(p, "/"):
		return fmt.Errorf("path %q is absolute", p)
	}
	for _, part := range strings.Split(p, "/") {
		if part == "" || part == "." || part == ".." {
			return fmt.Errorf("path %q is not a clean relative path", p)
		}
	}
	return nil
}

// memberFile streams one regular-file member of an archive, with the
// permission bits to install it with.
type memberFile struct {
	r       io.Reader
	closers []io.Closer // closed in reverse order on Close
	mode    fs.FileMode
}

func (m *memberFile) Read(p []byte) (int, error) { return m.r.Read(p) }

func (m *memberFile) Close() error {
	var err error
	for i := len(m.closers) - 1; i >= 0; i-- {
		if e := m.closers[i].Close(); e != nil && err == nil {
			err = e
		}
	}
	return err
}

// openArchiveMember opens the member named name of the archive at path.
// name must be a safe relative path and must match a regular-file entry;
// directories, symlinks, hard links and other special entries are refused,
// so a crafted archive cannot plant a link that writes outside the
// resources directory. Entry names are compared after cleaning, because
// archives prefix members with "./" (the ORT macOS archive does).
func openArchiveMember(k kind, archive, name string) (*memberFile, error) {
	if err := safeRelPath(name); err != nil {
		return nil, err
	}
	if k == kindZip {
		return openZipMember(archive, name)
	}
	return openTgzMember(archive, name)
}

// openZipMember opens one member of the zip archive at path. Only regular
// files are returned; entries with type bits (directories, symlinks) are
// refused. Entries that carry no Unix attributes at all report mode 0,
// which the caller turns into default permissions.
func openZipMember(archive, name string) (*memberFile, error) {
	f, err := os.Open(archive)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	r, err := zip.NewReader(f, info.Size())
	if err != nil {
		f.Close()
		return nil, err
	}
	for _, zf := range r.File {
		if path.Clean(zf.Name) != name {
			continue
		}
		if zf.Mode().Type() != 0 {
			f.Close()
			return nil, fmt.Errorf("%s: archive member %q is not a regular file", archive, zf.Name)
		}
		rc, err := zf.Open()
		if err != nil {
			f.Close()
			return nil, err
		}
		return &memberFile{r: rc, closers: []io.Closer{f, rc}, mode: zf.Mode().Perm()}, nil
	}
	f.Close()
	return nil, fmt.Errorf("%s: no archive member %q", archive, name)
}

// openTgzMember opens one member of the gzipped tar archive at path. Only
// regular files are returned; symlinks, hard links and directories are
// refused.
func openTgzMember(archive, name string) (*memberFile, error) {
	f, err := os.Open(archive)
	if err != nil {
		return nil, err
	}
	gz, err := gzip.NewReader(f)
	if err != nil {
		f.Close()
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			f.Close()
			gz.Close()
			return nil, fmt.Errorf("%s: no archive member %q", archive, name)
		}
		if err != nil {
			f.Close()
			gz.Close()
			return nil, err
		}
		if path.Clean(hdr.Name) != name {
			continue
		}
		if hdr.Typeflag != tar.TypeReg {
			f.Close()
			gz.Close()
			return nil, fmt.Errorf("%s: archive member %q is not a regular file", archive, hdr.Name)
		}
		return &memberFile{r: tr, closers: []io.Closer{f, gz}, mode: fs.FileMode(hdr.Mode).Perm()}, nil
	}
}
