package record

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// faultFS wraps an FS and injects failures at each operation the store
// uses, matching by operation and path suffix. It exists so the write
// discipline can be tested against real failures instead of assumed ones.
// It is not safe for concurrent use: the store serializes its callers, so
// fault tests run single-goroutine by construction.
type faultFS struct {
	inner FS
	rules []faultRule
	// written counts the bytes each path received, for write budgets.
	written map[string]int64
}

type faultOp uint8

const (
	faultCreate faultOp = iota + 1
	faultWrite
	faultSync
	faultClose
)

type faultRule struct {
	op   faultOp
	path string // matches when the target path ends with this
	err  error
	// budget caps the total bytes writes to the matching path may emit
	// before failing; only faultWrite uses it. Negative means no bytes
	// at all — the first write fails outright, like a full disk at the
	// moment of the first block.
	budget int64
}

// inject fails every matching operation with err.
func (f *faultFS) inject(op faultOp, path string, err error) {
	f.rules = append(f.rules, faultRule{op: op, path: path, err: err, budget: -1})
}

// injectShortWrite lets a matching file receive at most budget bytes in
// total, then fails with err — a file truncated mid-write.
func (f *faultFS) injectShortWrite(path string, budget int64, err error) {
	f.rules = append(f.rules, faultRule{op: faultWrite, path: path, err: err, budget: budget})
}

// clear removes all rules, simulating the transient fault going away.
func (f *faultFS) clear() {
	f.rules = nil
}

func (f *faultFS) rule(op faultOp, path string) *faultRule {
	for i := range f.rules {
		if f.rules[i].op == op && strings.HasSuffix(path, f.rules[i].path) {
			return &f.rules[i]
		}
	}
	return nil
}

func (f *faultFS) MkdirAll(dir string, perm fs.FileMode) error { return f.inner.MkdirAll(dir, perm) }

func (f *faultFS) Stat(path string) (fs.FileInfo, error) { return f.inner.Stat(path) }

func (f *faultFS) ReadDir(dir string) ([]fs.DirEntry, error) { return f.inner.ReadDir(dir) }

func (f *faultFS) Remove(path string) error { return f.inner.Remove(path) }

func (f *faultFS) OpenFile(path string, flag int, perm fs.FileMode) (File, error) {
	if flag&os.O_CREATE != 0 {
		if r := f.rule(faultCreate, path); r != nil {
			return nil, r.err
		}
	}
	file, err := f.inner.OpenFile(path, flag, perm)
	if err != nil {
		return nil, err
	}
	return &faultFile{fs: f, inner: file, path: path}, nil
}

type faultFile struct {
	fs    *faultFS
	inner File
	path  string
}

func (f *faultFile) Read(p []byte) (int, error) { return f.inner.Read(p) }

func (f *faultFile) Write(p []byte) (int, error) {
	r := f.fs.rule(faultWrite, f.path)
	if r == nil {
		return f.inner.Write(p)
	}
	if f.fs.written == nil {
		f.fs.written = make(map[string]int64)
	}
	allowed := r.budget - f.fs.written[f.path]
	if allowed <= 0 {
		return 0, r.err
	}
	n := min(int64(len(p)), allowed)
	nw, err := f.inner.Write(p[:int(n)])
	f.fs.written[f.path] += int64(nw)
	if err != nil {
		return nw, err
	}
	if nw == len(p) {
		return nw, nil
	}
	return nw, r.err
}

func (f *faultFile) Sync() error {
	if r := f.fs.rule(faultSync, f.path); r != nil {
		return r.err
	}
	return f.inner.Sync()
}

func (f *faultFile) Close() error {
	if r := f.fs.rule(faultClose, f.path); r != nil {
		return r.err
	}
	return f.inner.Close()
}

// The injected failures the tests distinguish by errors.Is.
var (
	errFaultCreate = errors.New("fault: create refused")
	errFaultWrite  = errors.New("fault: write failed")
	errDiskFull    = errors.New("fault: no space left on device")
	errFaultSync   = errors.New("fault: sync failed")
	errFaultClose  = errors.New("fault: close failed")
)

func newFaultStore(t *testing.T) (*Store, *faultFS, string) {
	t.Helper()
	root := t.TempDir()
	ffs := &faultFS{inner: osFS{}}
	st, err := newStore(root, ffs)
	if err != nil {
		t.Fatalf("newStore: %v", err)
	}
	return st, ffs, root
}

func readEvidenceFile(t *testing.T, root, id, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "sessions", id, "evidence", name))
	if err != nil {
		t.Fatalf("ReadFile %s: %v", name, err)
	}
	return data
}

func TestEvidenceWriteFailures(t *testing.T) {
	cases := []struct {
		name   string
		inject func(*faultFS)
		want   error
	}{
		{"create refused", func(f *faultFS) { f.inject(faultCreate, "shot-1.png", errFaultCreate) }, errFaultCreate},
		{"write truncated", func(f *faultFS) { f.injectShortWrite("shot-1.png", 2, errFaultWrite) }, errFaultWrite},
		{"disk full at first block", func(f *faultFS) { f.injectShortWrite("shot-1.png", 0, errDiskFull) }, errDiskFull},
		{"sync failed", func(f *faultFS) { f.inject(faultSync, "shot-1.png", errFaultSync) }, errFaultSync},
		{"close failed", func(f *faultFS) { f.inject(faultClose, "shot-1.png", errFaultClose) }, errFaultClose},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st, ffs, root := newFaultStore(t)
			id := newRecord(t, st)
			// An earlier evidence file must survive every failure below.
			writeEvidence(t, st, id, "shot-0", []byte("earlier evidence"))
			before := readEvidenceFile(t, root, id, "shot-0.png")

			tc.inject(ffs)
			if _, err := st.WriteEvidence(id, "shot-1", []byte("evidence bytes")); !errors.Is(err, tc.want) {
				t.Fatalf("WriteEvidence = %v, want %v", err, tc.want)
			}
			if _, err := os.Stat(filepath.Join(root, "sessions", id, "evidence", "shot-1.png")); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("failed write left a file behind (stat err %v), want none", err)
			}
			if after := readEvidenceFile(t, root, id, "shot-0.png"); !bytes.Equal(before, after) {
				t.Error("earlier evidence file was damaged by the failed write")
			}
		})
	}
}

func TestCheckpointWriteFailuresKeepPreviousGeneration(t *testing.T) {
	cases := []struct {
		name   string
		inject func(*faultFS)
		want   error
	}{
		{"create refused", func(f *faultFS) { f.inject(faultCreate, "2.json", errFaultCreate) }, errFaultCreate},
		{"truncated mid-file", func(f *faultFS) { f.injectShortWrite("2.json", 40, errFaultWrite) }, errFaultWrite},
		{"disk full", func(f *faultFS) { f.injectShortWrite("2.json", 0, errDiskFull) }, errDiskFull},
		{"sync failed", func(f *faultFS) { f.inject(faultSync, "2.json", errFaultSync) }, errFaultSync},
		{"close failed", func(f *faultFS) { f.inject(faultClose, "2.json", errFaultClose) }, errFaultClose},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st, ffs, root := newFaultStore(t)
			id := newRecord(t, st)

			first := Checkpoint{Session: testShell()}
			first.Session.Coverage.Status = CoverageUnknown
			if rev := saveCheckpoint(t, st, id, first); rev != 1 {
				t.Fatalf("first save revision = %d, want 1", rev)
			}
			gen1Path := filepath.Join(root, "sessions", id, "checkpoints", "1.json")
			gen1, err := os.ReadFile(gen1Path)
			if err != nil {
				t.Fatalf("ReadFile: %v", err)
			}

			second := Checkpoint{Session: testShell()}
			second.Session.Coverage.Status = CoveragePartial
			tc.inject(ffs)
			if _, err := st.SaveCheckpoint(id, second); !errors.Is(err, tc.want) {
				t.Fatalf("SaveCheckpoint = %v, want %v", err, tc.want)
			}
			// The failed generation left nothing behind, and the
			// previous valid generation is byte-identical.
			if _, err := os.Stat(filepath.Join(root, "sessions", id, "checkpoints", "2.json")); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("failed save left a generation behind (stat err %v), want none", err)
			}
			if after, err := os.ReadFile(gen1Path); err != nil || !bytes.Equal(gen1, after) {
				t.Errorf("previous generation changed (err %v)", err)
			}

			// Once the fault clears, the retry saves fresh content as
			// the same next revision — the failed attempt was never
			// counted as saved.
			ffs.clear()
			rev, err := st.SaveCheckpoint(id, second)
			if err != nil {
				t.Fatalf("retry SaveCheckpoint: %v", err)
			}
			if rev != 2 {
				t.Errorf("retry revision = %d, want 2", rev)
			}
			files := generationFiles(t, root, id)
			if len(files) != 2 || files[0] != "1.json" || files[1] != "2.json" {
				t.Errorf("generations after retry = %v, want [1.json 2.json]", files)
			}
		})
	}
}

func TestCheckpointCannotReferenceFailedEvidence(t *testing.T) {
	st, ffs, root := newFaultStore(t)
	id := newRecord(t, st)

	ffs.injectShortWrite("shot-1.png", 0, errDiskFull)
	if _, err := st.WriteEvidence(id, "shot-1", []byte("image")); !errors.Is(err, errDiskFull) {
		t.Fatalf("WriteEvidence = %v, want %v", err, errDiskFull)
	}
	// The caller cannot know the digest of bytes that never landed, but
	// even a well-formed index entry cannot pass: the file is not on
	// disk, so the save is refused before anything is written.
	sum := sha256.Sum256([]byte("image"))
	cp := Checkpoint{
		Session: testShell(),
		Items: []ItemRecord{{
			Sequence:    1,
			Status:      StatusProduced,
			Quality:     QualityNotEvaluated,
			EvidenceIDs: []string{"shot-1"},
		}},
		Evidence: []EvidenceFile{{
			ID:         "shot-1",
			ByteLength: 5,
			SHA256:     hex.EncodeToString(sum[:]),
		}},
	}
	cp.Session.CapturedCount = 1
	if _, err := st.SaveCheckpoint(id, cp); err == nil {
		t.Fatal("save succeeded with evidence that never landed, want error")
	}
	if files := generationFiles(t, root, id); len(files) != 0 {
		t.Errorf("rejected save left generations %v, want none", files)
	}
	// The evidence directory holds no partial file either.
	entries, err := os.ReadDir(filepath.Join(root, "sessions", id, "evidence"))
	if err != nil {
		t.Fatalf("ReadDir evidence: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("evidence dir holds %d file(s), want none", len(entries))
	}
}

// The write budget really does produce partial files: without the
// remove-on-failure discipline a short write would leave a truncated
// generation on disk. This test pins that discipline by re-injecting the
// fault and checking the leftover-free guarantee holds for a save that
// fails after some bytes were accepted.
func TestShortWriteLeavesNothingBehind(t *testing.T) {
	st, ffs, root := newFaultStore(t)
	id := newRecord(t, st)
	saveCheckpoint(t, st, id, Checkpoint{Session: testShell()})

	ffs.injectShortWrite("2.json", 40, errFaultWrite)
	if _, err := st.SaveCheckpoint(id, Checkpoint{Session: testShell()}); err == nil {
		t.Fatal("save succeeded, want failure")
	}
	entries, err := os.ReadDir(filepath.Join(root, "sessions", id, "checkpoints"))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "1.json" {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("checkpoints after failed save = %v, want [1.json]", names)
	}
	// Sanity: the injected budget is really below the document size, so
	// the failure was a mid-file truncation, not a refusal.
	data, err := os.ReadFile(filepath.Join(root, "sessions", id, "checkpoints", "1.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) <= 40 {
		t.Fatalf("test setup is meaningless: document is %d bytes, budget 40", len(data))
	}
}
