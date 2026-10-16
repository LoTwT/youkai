package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
)

const (
	// resourcesDir is MyGo's directory whose contents ship with the app;
	// downloads install into it, below ignored subdirectories.
	resourcesDir = "resources"
	// cacheDir holds the raw downloads, whole and digest-verified, so
	// re-running after a cleanup of resources/ needs no new network access.
	cacheDir = ".fetchdeps-cache"
	// userAgent identifies the tool to hosts that dislike empty agents.
	userAgent = "youkai-fetchdeps (+https://github.com/LoTwT/youkai)"
)

// tool installs dependencies below root, the repository directory.
type tool struct {
	root   string
	client *http.Client
}

func newTool(root string) *tool {
	return &tool{root: root, client: &http.Client{}}
}

// destPath is the absolute path of dest, a slash-separated path under the
// resources directory.
func (t *tool) destPath(dest string) string {
	return filepath.Join(t.root, resourcesDir, filepath.FromSlash(dest))
}

// ensure makes every file of dep exist under resources/ with the pinned
// digest. Files that already match are skipped. A file that exists with a
// different digest fails instead of being overwritten: it was modified by
// hand or came from somewhere else, and replacing it silently would hide
// that. Each file is written to a temporary sibling and renamed into place
// only after its digest is verified, so a failure never leaves a partial
// file behind.
func (t *tool) ensure(ctx context.Context, dep dependency) error {
	var pending []target
	for _, tgt := range dep.targets() {
		dst := t.destPath(tgt.dest)
		got, err := fileDigest(dst)
		switch {
		case err == nil && got == tgt.sha256:
			continue // already installed and intact
		case err == nil:
			return fmt.Errorf("%s exists with digest %s, want %s; delete the file and run fetchdeps again", dst, got, tgt.sha256)
		case !errors.Is(err, fs.ErrNotExist):
			return err
		}
		pending = append(pending, tgt)
	}
	if len(pending) == 0 {
		log.Printf("%s: up to date", dep.name)
		return nil
	}
	archive, err := t.source(ctx, dep)
	if err != nil {
		return err
	}
	for _, tgt := range pending {
		if err := t.install(dep, archive, tgt); err != nil {
			return err
		}
	}
	return nil
}

// install places one target of dep, reading the verified download at
// archive.
func (t *tool) install(dep dependency, archive string, tgt target) error {
	var (
		r       io.Reader
		mode    fs.FileMode = 0o644
		closers []io.Closer
	)
	switch dep.kind {
	case kindFile:
		f, err := os.Open(archive)
		if err != nil {
			return err
		}
		r, closers = f, append(closers, f)
	case kindZip, kindTgz:
		m, err := openArchiveMember(dep.kind, archive, tgt.member)
		if err != nil {
			return err
		}
		// Archives may carry no permission bits (some zips do not); keep
		// their permissions when present, as MyGo preserves them when
		// packaging resources.
		if perm := m.mode.Perm(); perm != 0 {
			mode = perm
		}
		r, closers = m, append(closers, m)
	}
	defer func() {
		for i := len(closers) - 1; i >= 0; i-- {
			closers[i].Close()
		}
	}()
	size, digest, err := writeFile(r, t.destPath(tgt.dest), tgt.sha256, mode)
	if err != nil {
		return err
	}
	log.Printf("installed %s/%s (%d bytes, sha256 %s)", resourcesDir, tgt.dest, size, digest)
	return nil
}

// source returns the path of dep's downloaded bytes in the cache, fetching
// them first when the cache holds no file with the pinned digest. The
// download goes to a temporary file and is renamed into place, so the cache
// only ever holds complete, digest-verified downloads; a cache file with a
// different digest is simply fetched again.
func (t *tool) source(ctx context.Context, dep dependency) (string, error) {
	cache := filepath.Join(t.root, cacheDir, dep.name)
	got, err := fileDigest(cache)
	switch {
	case err == nil && got == dep.sha256:
		return cache, nil
	case err == nil:
		log.Printf("%s: cached copy has digest %s, want %s; downloading again", dep.name, got, dep.sha256)
	case errors.Is(err, fs.ErrNotExist):
	default:
		return "", err
	}
	dir := filepath.Dir(cache)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return "", err
	}
	name := tmp.Name()
	digest, size, err := t.download(ctx, dep.url, tmp)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil && digest != dep.sha256 {
		err = fmt.Errorf("downloaded bytes have digest %s, want %s pinned for this file (from %s)", digest, dep.sha256, dep.url)
	}
	if err != nil {
		os.Remove(name)
		return "", err
	}
	if err := os.Rename(name, cache); err != nil {
		os.Remove(name)
		return "", err
	}
	log.Printf("downloaded %s (%d bytes)", dep.name, size)
	return cache, nil
}

// download streams url to w, following redirects, and returns the SHA-256
// and the size of the bytes written.
func (t *tool) download(ctx context.Context, url string, w io.Writer) (digest string, size int64, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := t.client.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("GET %s: unexpected status %s", url, resp.Status)
	}
	h := sha256.New()
	size, err = io.Copy(io.MultiWriter(w, h), resp.Body)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), size, nil
}

// writeFile streams r into a temporary file next to dst, checks the SHA-256
// of what was written against want, applies mode and renames the file into
// place. On any error the temporary file is removed and dst is untouched.
func writeFile(r io.Reader, dst, want string, mode fs.FileMode) (size int64, digest string, err error) {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return 0, "", err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".fetchdeps-*")
	if err != nil {
		return 0, "", err
	}
	name := tmp.Name()
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(name)
		}
	}()
	h := sha256.New()
	size, err = io.Copy(io.MultiWriter(tmp, h), r)
	if err != nil {
		return 0, "", err
	}
	if err = tmp.Close(); err != nil {
		return 0, "", err
	}
	digest = hex.EncodeToString(h.Sum(nil))
	if digest != want {
		err = fmt.Errorf("%s: digest mismatch, got %s, want %s", dst, digest, want)
		return 0, digest, err
	}
	if err = os.Chmod(name, mode); err != nil {
		return 0, "", err
	}
	if err = os.Rename(name, dst); err != nil {
		return 0, "", err
	}
	return size, digest, nil
}

// fileDigest returns the SHA-256 of the file at path.
func fileDigest(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
