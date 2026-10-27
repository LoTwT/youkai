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
	// synced counts the flush attempts each path saw, so tests can tell a
	// deferred sync from one that never happened.
	synced map[string]int
}

type faultOp uint8

const (
	faultCreate faultOp = iota + 1
	faultWrite
	faultSync
	faultSyncDir
	faultClose
	faultRemove
	faultRemoveAll
	faultReadDir
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

func (f *faultFS) Lstat(path string) (fs.FileInfo, error) { return f.inner.Lstat(path) }

func (f *faultFS) ReadDir(dir string) ([]fs.DirEntry, error) {
	if r := f.rule(faultReadDir, dir); r != nil {
		return nil, r.err
	}
	return f.inner.ReadDir(dir)
}

func (f *faultFS) Remove(path string) error {
	if r := f.rule(faultRemove, path); r != nil {
		return r.err
	}
	return f.inner.Remove(path)
}

func (f *faultFS) RemoveAll(path string) error {
	if r := f.rule(faultRemoveAll, path); r != nil {
		return r.err
	}
	return f.inner.RemoveAll(path)
}

func (f *faultFS) SyncDir(dir string) error {
	f.recordSync(dir)
	if r := f.rule(faultSyncDir, dir); r != nil {
		return r.err
	}
	return f.inner.SyncDir(dir)
}

func (f *faultFS) recordSync(path string) {
	if f.synced == nil {
		f.synced = make(map[string]int)
	}
	f.synced[path]++
}

// syncCount reports how many flushes matched one path suffix.
func (f *faultFS) syncCount(suffix string) int {
	n := 0
	for path, count := range f.synced {
		if strings.HasSuffix(path, suffix) {
			n += count
		}
	}
	return n
}

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
	f.fs.recordSync(f.path)
	if r := f.fs.rule(faultSync, f.path); r != nil {
		return r.err
	}
	return f.inner.Sync()
}

func (f *faultFile) Close() error {
	if r := f.fs.rule(faultClose, f.path); r != nil {
		// A real close failure still releases the handle — os.File
		// closes the descriptor and reports the flush result — so the
		// injected failure must release it too. Keeping the handle
		// open would make the store's cleanup Remove fail on Windows
		// (open files cannot be deleted), which no longer resembles
		// the failure being simulated.
		_ = f.inner.Close()
		return r.err
	}
	return f.inner.Close()
}

// The injected failures the tests distinguish by errors.Is.
var (
	errFaultCreate  = errors.New("fault: create refused")
	errFaultWrite   = errors.New("fault: write failed")
	errDiskFull     = errors.New("fault: no space left on device")
	errFaultSync    = errors.New("fault: sync failed")
	errFaultSyncDir = errors.New("fault: directory sync failed")
	errFaultClose   = errors.New("fault: close failed")
	errFaultRemove  = errors.New("fault: remove refused")
	errFaultReadDir = errors.New("fault: read directory refused")
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

// checkpointWithEvidence returns a valid one-item checkpoint that cites
// exactly the given evidence files.
func checkpointWithEvidence(evs ...EvidenceFile) Checkpoint {
	cp := Checkpoint{Session: testShell()}
	cp.Session.CapturedCount = 1
	ids := make([]string, 0, len(evs))
	for _, ev := range evs {
		ids = append(ids, ev.ID)
	}
	cp.Items = []ItemRecord{{
		Sequence:    1,
		Status:      StatusProduced,
		Quality:     QualityNotEvaluated,
		EvidenceIDs: ids,
	}}
	cp.Evidence = evs
	return cp
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

// Evidence writes do not flush per image: the sync is deferred and done
// once for the batch the next checkpoint cites, including the evidence
// directory entry. A file already synced is not flushed again.
func TestEvidenceSyncIsDeferredToCheckpoint(t *testing.T) {
	st, ffs, _ := newFaultStore(t)
	id := newRecord(t, st)
	first := writeEvidence(t, st, id, "shot-1", []byte("first"))
	second := writeEvidence(t, st, id, "shot-2", []byte("second"))
	if got := ffs.syncCount(".png"); got != 0 {
		t.Fatalf("WriteEvidence performed %d evidence sync(s), want none", got)
	}

	cp := checkpointWithEvidence(first, second)
	if rev := saveCheckpoint(t, st, id, cp); rev != 1 {
		t.Fatalf("first save revision = %d, want 1", rev)
	}
	if got := ffs.syncCount(".png"); got != 2 {
		t.Errorf("evidence syncs = %d, want one per cited file (2)", got)
	}
	evidenceDir := filepath.Join("sessions", id, "evidence")
	if got := ffs.syncCount(evidenceDir); got != 1 {
		t.Errorf("evidence directory syncs = %d, want 1", got)
	}

	// The same files are already durable; citing them again must not pay
	// for another flush.
	if _, err := st.SaveCheckpoint(id, cp); err != nil {
		t.Fatalf("second save: %v", err)
	}
	if got := ffs.syncCount(".png"); got != 2 {
		t.Errorf("evidence syncs after resaving the same files = %d, want 2", got)
	}
}

// A failed evidence sync fails the checkpoint before any generation is
// written and keeps the file pending: every retry while the fault lasts
// attempts the sync again, and the save succeeds normally once it clears.
func TestEvidenceSyncFailureKeepsPendingForRetry(t *testing.T) {
	cases := []struct {
		name   string
		inject func(*faultFS, string)
		target func(string) string
		want   error
	}{
		{
			name:   "file sync",
			inject: func(f *faultFS, _ string) { f.inject(faultSync, "shot-1.png", errFaultSync) },
			target: func(string) string { return "shot-1.png" },
			want:   errFaultSync,
		},
		{
			name: "directory sync",
			inject: func(f *faultFS, id string) {
				f.inject(faultSyncDir, filepath.Join("sessions", id, "evidence"), errFaultSyncDir)
			},
			target: func(id string) string { return filepath.Join("sessions", id, "evidence") },
			want:   errFaultSyncDir,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st, ffs, root := newFaultStore(t)
			id := newRecord(t, st)
			base := Checkpoint{Session: testShell()}
			base.Session.Coverage.Status = CoverageUnknown
			saveCheckpoint(t, st, id, base)

			ev := writeEvidence(t, st, id, "shot-1", []byte("image"))
			cp := checkpointWithEvidence(ev)
			tc.inject(ffs, id)
			for attempt := 1; attempt <= 2; attempt++ {
				if _, err := st.SaveCheckpoint(id, cp); !errors.Is(err, tc.want) {
					t.Fatalf("save attempt %d = %v, want %v", attempt, err, tc.want)
				}
				if files := generationFiles(t, root, id); len(files) != 1 || files[0] != "1.json" {
					t.Fatalf("attempt %d generations = %v, want the prior [1.json]", attempt, files)
				}
			}

			ffs.clear()
			rev, err := st.SaveCheckpoint(id, cp)
			if err != nil {
				t.Fatalf("retry after clearing the fault: %v", err)
			}
			if rev != 2 {
				t.Errorf("retry revision = %d, want 2", rev)
			}
			if got := ffs.syncCount(tc.target(id)); got != 3 {
				t.Errorf("sync attempts = %d, want 3 (two failures and the retry)", got)
			}
			if files := generationFiles(t, root, id); len(files) != 2 || files[0] != "1.json" || files[1] != "2.json" {
				t.Errorf("generations after retry = %v, want [1.json 2.json]", files)
			}
			if rec := mustLoad(t, st, id); rec.Checkpoint.SavedRevision != 2 {
				t.Errorf("loaded revision = %d, want 2", rec.Checkpoint.SavedRevision)
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

// A close failure whose cleanup removal also fails must still be
// reported as a failure — never as success — and must say that a file
// survived. The survivor is complete (write and sync finished before the
// close error), so a load can verify it, which preserves the work; the
// retry then takes the next free revision and retention promotes the
// survivor to the previous valid generation, since the load verified it.
func TestCloseFailureWithUnremovableGeneration(t *testing.T) {
	st, ffs, root := newFaultStore(t)
	id := newRecord(t, st)
	saveCheckpoint(t, st, id, Checkpoint{Session: testShell()})

	ffs.inject(faultClose, "2.json", errFaultClose)
	ffs.inject(faultRemove, "2.json", errFaultRemove)
	_, err := st.SaveCheckpoint(id, Checkpoint{Session: testShell()})
	if !errors.Is(err, errFaultClose) {
		t.Fatalf("SaveCheckpoint = %v, want the close failure", err)
	}
	if !errors.Is(err, errFaultRemove) || !strings.Contains(err.Error(), "left behind") {
		t.Errorf("error = %v, want the leftover file reported alongside the cause", err)
	}
	// The survivor is on disk and never counted as saved.
	if _, statErr := os.Stat(filepath.Join(root, "sessions", id, "checkpoints", "2.json")); statErr != nil {
		t.Fatalf("the unremoved generation is gone: %v", statErr)
	}

	// The survivor is complete, so a load verifies it — the bytes are
	// real, and dropping them to honour the failed close would lose
	// work. It becomes the memo base and a known-valid generation.
	ffs.clear()
	if rec := mustLoad(t, st, id); rec.Checkpoint.SavedRevision != 2 {
		t.Errorf("loaded revision = %d, want the complete survivor 2", rec.Checkpoint.SavedRevision)
	}

	// The retry takes the next free revision (the survivor keeps 2
	// busy), and retention keeps the verified survivor as the previous
	// valid generation instead of the older base.
	rev, err := st.SaveCheckpoint(id, Checkpoint{Session: testShell()})
	if err != nil {
		t.Fatalf("retry SaveCheckpoint: %v", err)
	}
	if rev != 3 {
		t.Errorf("retry revision = %d, want 3", rev)
	}
	files := generationFiles(t, root, id)
	if len(files) != 2 || files[0] != "2.json" || files[1] != "3.json" {
		t.Errorf("generations after retry = %v, want [2.json 3.json]", files)
	}
	if rec := mustLoad(t, st, id); rec.Checkpoint.SavedRevision != 3 {
		t.Errorf("loaded revision = %d, want 3", rec.Checkpoint.SavedRevision)
	}
}

// A write failure whose cleanup removal also fails leaves a truncated
// generation behind. It must never load, the retry must skip past it to
// the next free revision, and retention must delete it once the new
// valid generation establishes a fallback below it.
func TestPartialGenerationWithFailedRemovalFallsBack(t *testing.T) {
	st, ffs, root := newFaultStore(t)
	id := newRecord(t, st)
	base := Checkpoint{Session: testShell()}
	base.Session.Coverage.Status = CoverageUnknown
	saveCheckpoint(t, st, id, base)

	ffs.injectShortWrite("2.json", 40, errFaultWrite)
	ffs.inject(faultRemove, "2.json", errFaultRemove)
	_, err := st.SaveCheckpoint(id, Checkpoint{Session: testShell()})
	if !errors.Is(err, errFaultWrite) || !errors.Is(err, errFaultRemove) {
		t.Fatalf("SaveCheckpoint = %v, want the write and removal failures", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "sessions", id, "checkpoints", "2.json")); statErr != nil {
		t.Fatalf("the unremoved partial is gone: %v", statErr)
	}
	// The truncated survivor is invalid: the load falls back to the
	// base generation and never hands out the partial content.
	if rec := mustLoad(t, st, id); rec.Checkpoint.SavedRevision != 1 {
		t.Errorf("loaded revision = %d, want fallback to 1 past the partial", rec.Checkpoint.SavedRevision)
	}

	ffs.clear()
	rev, err := st.SaveCheckpoint(id, Checkpoint{Session: testShell()})
	if err != nil {
		t.Fatalf("retry SaveCheckpoint: %v", err)
	}
	if rev != 3 {
		t.Errorf("retry revision = %d, want 3 (the partial keeps 2 busy)", rev)
	}
	files := generationFiles(t, root, id)
	if len(files) != 2 || files[0] != "1.json" || files[1] != "3.json" {
		t.Errorf("generations after retry = %v, want [1.json 3.json]", files)
	}
}

// An evidence write whose close fails and whose cleanup removal also
// fails reports both and leaves the file. The write-once rule then keeps
// the id occupied: a retry with the same id is refused instead of
// truncating the survivor, and a fresh id works.
func TestEvidenceCloseFailureWithUnremovableFile(t *testing.T) {
	st, ffs, root := newFaultStore(t)
	id := newRecord(t, st)

	ffs.inject(faultClose, "shot-1.png", errFaultClose)
	ffs.inject(faultRemove, "shot-1.png", errFaultRemove)
	if _, err := st.WriteEvidence(id, "shot-1", []byte("image")); !errors.Is(err, errFaultClose) {
		t.Fatalf("WriteEvidence = %v, want the close failure", err)
	} else if !errors.Is(err, errFaultRemove) || !strings.Contains(err.Error(), "left behind") {
		t.Errorf("error = %v, want the leftover file reported alongside the cause", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "sessions", id, "evidence", "shot-1.png")); statErr != nil {
		t.Fatalf("the unremoved evidence file is gone: %v", statErr)
	}
	// The survivor keeps its id: the retry is refused before any write
	// (the exclusive create sees the file), never truncating it.
	if _, err := st.WriteEvidence(id, "shot-1", []byte("image")); !errors.Is(err, ErrEvidenceExists) {
		t.Errorf("retry with the same id = %v, want ErrEvidenceExists", err)
	}
	if _, err := st.WriteEvidence(id, "shot-2", []byte("image")); err != nil {
		t.Errorf("retry with a fresh id: %v", err)
	}
}
