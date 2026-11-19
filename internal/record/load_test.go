package record

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestPeekReadsMetadataWithoutVerifyingEvidence covers the history list's
// read path: a structurally valid checkpoint stays readable for dates,
// clients and counts even when opening the record would fail because an
// evidence file is missing.
func TestPeekReadsMetadataWithoutVerifyingEvidence(t *testing.T) {
	st, root := newTestStore(t)
	id := "rec-1"
	detail, _ := cleanupRecord(t, st, id)
	if err := os.Remove(filepath.Join(root, "sessions", id, "evidence", detail.ID+".png")); err != nil {
		t.Fatalf("remove evidence: %v", err)
	}
	cp, err := st.Peek(id)
	if err != nil {
		t.Fatalf("Peek: %v", err)
	}
	if cp.Session.Client != ClientMacOSCloud || cp.Session.CapturedCount != 1 ||
		cp.Session.Coverage.Status != CoveragePartial || len(cp.Items) != 1 {
		t.Fatalf("peek metadata = %+v", cp.Session)
	}
	if _, err := st.Load(id); !errors.Is(err, ErrNoValidCheckpoint) {
		t.Fatalf("Load after evidence removal = %v, want ErrNoValidCheckpoint", err)
	}
}

func mustLoad(t *testing.T, st *Store, id string) *Recovery {
	t.Helper()
	rec, err := st.Load(id)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return rec
}

func TestLoadReportsMissingState(t *testing.T) {
	st, _ := newTestStore(t)
	// Unknown record: nothing was ever created under this id.
	if _, err := st.Load("no-such-record"); !errors.Is(err, ErrRecordNotFound) {
		t.Errorf("unknown record = %v, want ErrRecordNotFound", err)
	}
	// Created but never saved: no generation exists to verify.
	id := newRecord(t, st)
	if _, err := st.Load(id); !errors.Is(err, ErrNoValidCheckpoint) {
		t.Errorf("unsaved record = %v, want ErrNoValidCheckpoint", err)
	}
	if _, err := st.Load("../escape"); !errors.Is(err, ErrInvalidID) {
		t.Errorf("escaping id = %v, want ErrInvalidID", err)
	}
}

func TestLoadReturnsNewestGeneration(t *testing.T) {
	st, _ := newTestStore(t)
	id := newRecord(t, st)
	for _, status := range []CoverageStatus{CoverageUnknown, CoveragePartial, CoveragePartial} {
		cp := Checkpoint{Session: testShell()}
		cp.Session.Coverage.Status = status
		saveCheckpoint(t, st, id, cp)
	}
	rec := mustLoad(t, st, id)
	if rec.Checkpoint.SavedRevision != 3 {
		t.Errorf("loaded revision = %d, want 3", rec.Checkpoint.SavedRevision)
	}
	if rec.Checkpoint.Session.Coverage.Status != CoveragePartial {
		t.Errorf("coverage = %q, want partial as saved", rec.Checkpoint.Session.Coverage.Status)
	}
	if len(rec.Interrupted) != 0 || rec.CoverageDowngraded {
		t.Errorf("recovery = %+v, want no marks on a finished snapshot", rec)
	}
}

func TestLoadFallsBackOverCorruption(t *testing.T) {
	cases := []struct {
		name   string
		damage func(t *testing.T, root, id string)
	}{
		{"truncated generation", func(t *testing.T, root, id string) {
			path := filepath.Join(root, "sessions", id, "checkpoints", "2.json")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, data[:len(data)/2], 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{"deleted generation", func(t *testing.T, root, id string) {
			if err := os.Remove(filepath.Join(root, "sessions", id, "checkpoints", "2.json")); err != nil {
				t.Fatal(err)
			}
		}},
		{"empty generation", func(t *testing.T, root, id string) {
			if err := os.WriteFile(filepath.Join(root, "sessions", id, "checkpoints", "2.json"), nil, 0o644); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st, root := newTestStore(t)
			id := newRecord(t, st)
			// Generation 1: an early snapshot with nothing captured.
			first := Checkpoint{Session: testShell()}
			first.Session.Coverage.Status = CoverageUnknown
			saveCheckpoint(t, st, id, first)
			// Generation 2: the one that will be damaged.
			second := Checkpoint{Session: testShell()}
			second.Session.Coverage.Status = CoveragePartial
			saveCheckpoint(t, st, id, second)

			tc.damage(t, root, id)
			rec := mustLoad(t, st, id)
			if rec.Checkpoint.SavedRevision != 1 {
				t.Errorf("loaded revision = %d, want fallback to 1", rec.Checkpoint.SavedRevision)
			}
			if rec.Checkpoint.Session.Coverage.Status != CoverageUnknown {
				t.Errorf("loaded coverage = %q, want generation 1's", rec.Checkpoint.Session.Coverage.Status)
			}
		})
	}
}

func TestLoadFallsBackOverTamperedDocuments(t *testing.T) {
	// Document-level tampering: the generation file is decoded, damaged
	// and written back.
	docCases := []struct {
		name string
		mut  func(doc *Checkpoint)
	}{
		{"unknown format version", func(doc *Checkpoint) { doc.FormatVersion = FormatVersion + 1 }},
		{"revision does not match file name", func(doc *Checkpoint) { doc.SavedRevision = 99 }},
	}
	for _, tc := range docCases {
		t.Run(tc.name, func(t *testing.T) {
			st, root := newTestStore(t)
			id := newRecord(t, st)
			first := Checkpoint{Session: testShell()}
			first.Session.Coverage.Status = CoverageUnknown
			saveCheckpoint(t, st, id, first)
			second := Checkpoint{Session: testShell()}
			second.Session.Coverage.Status = CoveragePartial
			saveCheckpoint(t, st, id, second)

			path := filepath.Join(root, "sessions", id, "checkpoints", "2.json")
			var doc Checkpoint
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(data, &doc); err != nil {
				t.Fatal(err)
			}
			tc.mut(&doc)
			encoded, err := json.MarshalIndent(&doc, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, append(encoded, '\n'), 0o644); err != nil {
				t.Fatal(err)
			}
			rec := mustLoad(t, st, id)
			if rec.Checkpoint.SavedRevision != 1 {
				t.Errorf("loaded revision = %d, want fallback to 1", rec.Checkpoint.SavedRevision)
			}
		})
	}

	// Evidence-level damage: the document is intact, but the bytes it
	// cites are gone or do not match the recorded digest.
	evidenceCases := []struct {
		name   string
		damage func(t *testing.T, path string)
	}{
		{"replaced evidence bytes", func(t *testing.T, path string) {
			// Same length, different bytes: only the digest catches it.
			if err := os.WriteFile(path, bytes.Repeat([]byte("x"), 5), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{"truncated evidence", func(t *testing.T, path string) {
			if err := os.WriteFile(path, []byte("abc"), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{"missing evidence file", func(t *testing.T, path string) {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range evidenceCases {
		t.Run(tc.name, func(t *testing.T) {
			st, root := newTestStore(t)
			id := newRecord(t, st)
			// Generation 1 cites no evidence, so it survives every
			// damage below; generation 2 cites one file.
			first := Checkpoint{Session: testShell()}
			first.Session.Coverage.Status = CoverageUnknown
			saveCheckpoint(t, st, id, first)
			ev := writeEvidence(t, st, id, "shot-1", []byte("abcde"))
			second := Checkpoint{
				Session: testShell(),
				Items: []ItemRecord{{
					Sequence:    1,
					Status:      StatusProduced,
					Quality:     QualityNotEvaluated,
					EvidenceIDs: []string{"shot-1"},
				}},
				Evidence: []EvidenceFile{ev},
			}
			second.Session.CapturedCount = 1
			second.Session.Coverage.Status = CoveragePartial
			saveCheckpoint(t, st, id, second)

			tc.damage(t, filepath.Join(root, "sessions", id, "evidence", "shot-1.png"))
			rec := mustLoad(t, st, id)
			if rec.Checkpoint.SavedRevision != 1 {
				t.Errorf("loaded revision = %d, want fallback to 1", rec.Checkpoint.SavedRevision)
			}
		})
	}
}

func TestLoadRejectsNonCanonicalDocuments(t *testing.T) {
	st, root := newTestStore(t)
	id := newRecord(t, st)
	first := Checkpoint{Session: testShell()}
	saveCheckpoint(t, st, id, first)
	saveCheckpoint(t, st, id, Checkpoint{Session: testShell()})

	gen2 := filepath.Join(root, "sessions", id, "checkpoints", "2.json")
	data, err := os.ReadFile(gen2)
	if err != nil {
		t.Fatal(err)
	}
	// An unknown field inside the document must disqualify it.
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	doc["mystery"] = json.RawMessage(`1`)
	tampered, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gen2, tampered, 0o644); err != nil {
		t.Fatal(err)
	}
	rec := mustLoad(t, st, id)
	if rec.Checkpoint.SavedRevision != 1 {
		t.Errorf("unknown-field document accepted, loaded revision %d, want 1", rec.Checkpoint.SavedRevision)
	}

	// A second JSON value after the document must disqualify it too.
	if err := os.WriteFile(gen2, append(data, '\n', '{', '}', '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	rec = mustLoad(t, st, id)
	if rec.Checkpoint.SavedRevision != 1 {
		t.Errorf("trailing-content document accepted, loaded revision %d, want 1", rec.Checkpoint.SavedRevision)
	}
}

func TestLoadWithoutAnyValidGeneration(t *testing.T) {
	st, root := newTestStore(t)
	id := newRecord(t, st)
	saveCheckpoint(t, st, id, Checkpoint{Session: testShell()})
	// Both existing generations are destroyed: nothing can be verified.
	if err := os.WriteFile(filepath.Join(root, "sessions", id, "checkpoints", "1.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Load(id); !errors.Is(err, ErrNoValidCheckpoint) {
		t.Errorf("all-corrupt record = %v, want ErrNoValidCheckpoint", err)
	}
}

func TestLoadLatestNeverFallsBackToAnOlderGeneration(t *testing.T) {
	st, root := newTestStore(t)
	id := newRecord(t, st)
	shell := testShell()
	shell.SessionID = id
	saveCheckpoint(t, st, id, Checkpoint{Session: shell})
	saveCheckpoint(t, st, id, Checkpoint{Session: shell})
	if err := os.WriteFile(filepath.Join(root, "sessions", id, "checkpoints", "2.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Load keeps recovery working by falling back; LoadLatest must not let
	// a caller prove the source record from the older generation.
	if cp, err := st.Peek(id); err != nil || cp.SavedRevision != 1 {
		t.Fatalf("Peek = revision %d, %v; want the older generation", cp.SavedRevision, err)
	}
	if rec, err := st.Load(id); err != nil || rec.Checkpoint.SavedRevision != 1 {
		t.Fatalf("Load = %v, %v; want the older generation", rec, err)
	}
	if _, err := st.LoadLatest(context.Background(), id); err == nil {
		t.Fatal("LoadLatest accepted a broken newest generation")
	}
}

func TestLoadLatestVerifiesNewestEvidenceWithoutFallback(t *testing.T) {
	cases := []struct {
		name   string
		damage func(t *testing.T, path string)
	}{
		{name: "missing", damage: func(t *testing.T, path string) {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "bad digest", damage: func(t *testing.T, path string) {
			if err := os.WriteFile(path, bytes.Repeat([]byte("x"), 32), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st, root := newTestStore(t)
			id := newRecord(t, st)
			shell := testShell()
			shell.SessionID = id
			saveCheckpoint(t, st, id, Checkpoint{Session: shell})
			ev := writeEvidence(t, st, id, "coverage-end", bytes.Repeat([]byte("c"), 32))
			shell.CoverageEvidenceIDs = []string{ev.ID}
			saveCheckpoint(t, st, id, Checkpoint{Session: shell, Evidence: []EvidenceFile{ev}})
			tc.damage(t, filepath.Join(root, "sessions", id, "evidence", ev.ID+".png"))

			if rec, err := st.Load(id); err != nil || rec.Checkpoint.SavedRevision != 1 {
				t.Fatalf("Load = %v, %v; want the older evidence-free generation", rec, err)
			}
			if _, err := st.LoadLatest(context.Background(), id); err == nil {
				t.Fatal("LoadLatest accepted the newest generation with damaged evidence")
			}
		})
	}
}

func TestLoadLatestRejectsACrossDirectoryIdentity(t *testing.T) {
	st, _ := newTestStore(t)
	id := newRecord(t, st)
	shell := testShell()
	shell.SessionID = "a93e8b22-5c0d-4a7e-8f1b-52a2c11d7f10"
	saveCheckpoint(t, st, id, Checkpoint{Session: shell})
	if _, err := st.Load(id); err != nil {
		t.Fatalf("Load should keep its historical behavior: %v", err)
	}
	if _, err := st.LoadLatest(context.Background(), id); err == nil {
		t.Fatal("LoadLatest accepted a checkpoint whose session identity names another record")
	}
}

func TestLoadLatestReportsNoGeneration(t *testing.T) {
	st, _ := newTestStore(t)
	id := newRecord(t, st)
	if _, err := st.LoadLatest(context.Background(), id); !errors.Is(err, ErrNoValidCheckpoint) {
		t.Fatalf("LoadLatest without a generation = %v, want ErrNoValidCheckpoint", err)
	}
}

func TestLoadMarksInterruptions(t *testing.T) {
	st, _ := newTestStore(t)
	id := newRecord(t, st)
	cp := Checkpoint{Session: testShell()} // still running: no termination
	cp.Items = []ItemRecord{
		{Sequence: 1, Status: StatusProduced, Quality: QualityAutoAccepted},
		{Sequence: 2, Status: StatusQueued, Quality: QualityNotEvaluated},
		{Sequence: 3, Status: StatusRunning, Quality: QualityNotEvaluated},
		{Sequence: 4, Status: StatusUnprocessed, Reason: ReasonCancelled, Quality: QualityNotEvaluated},
		{Sequence: 5, Status: StatusFailed, Failure: "recognizer error", Quality: QualityNotEvaluated},
	}
	cp.Session.CapturedCount = 5
	cp.Session.Coverage.Status = CoverageUnknown
	saveCheckpoint(t, st, id, cp)

	rec := mustLoad(t, st, id)
	want := []int64{2, 3}
	if len(rec.Interrupted) != len(want) || rec.Interrupted[0] != 2 || rec.Interrupted[1] != 3 {
		t.Errorf("interrupted = %v, want %v", rec.Interrupted, want)
	}
	items := rec.Checkpoint.Items
	if items[1].Status != StatusUnprocessed || items[1].Reason != ReasonInterrupted {
		t.Errorf("queued item recovered as %s/%q, want unprocessed/interrupted", items[1].Status, items[1].Reason)
	}
	if items[2].Status != StatusUnprocessed || items[2].Reason != ReasonInterrupted {
		t.Errorf("running item recovered as %s/%q, want unprocessed/interrupted", items[2].Status, items[2].Reason)
	}
	// Terminal outcomes are restored exactly as saved, including an
	// earlier cancel: its reason must not be rewritten to interrupted.
	if items[0].Status != StatusProduced || items[3].Status != StatusUnprocessed || items[3].Reason != ReasonCancelled || items[4].Status != StatusFailed {
		t.Errorf("terminal items disturbed: %s, %s/%q, %s", items[0].Status, items[3].Status, items[3].Reason, items[4].Status)
	}
	// Unknown coverage stays unknown — never upgraded, no need to
	// downgrade.
	if rec.Checkpoint.Session.Coverage.Status != CoverageUnknown || rec.CoverageDowngraded {
		t.Errorf("coverage = %q downgraded=%v, want unknown untouched", rec.Checkpoint.Session.Coverage.Status, rec.CoverageDowngraded)
	}
}

func TestLoadDoesNotUpholdCompleteCoverage(t *testing.T) {
	finished := &Termination{Kind: TerminationCompleted, Code: "verifiedEnd", Phase: PhaseCapturing}
	endedAt := testShell().StartedAt.Add(10 * time.Minute)
	cases := []struct {
		name string
		cp   func() Checkpoint
		// wantStatus is the coverage the recovery must report, and
		// wantDowngraded whether it had to clamp a complete claim.
		wantStatus    CoverageStatus
		wantDowngrade bool
	}{
		{
			name: "queued items cannot be complete",
			cp: func() Checkpoint {
				cp := Checkpoint{Session: testShell()}
				cp.Items = []ItemRecord{{Sequence: 1, Status: StatusQueued, Quality: QualityNotEvaluated}}
				cp.Session.CapturedCount = 1
				cp.Session.Coverage.Status = CoverageComplete
				return cp
			},
			wantStatus:    CoveragePartial,
			wantDowngrade: true,
		},
		{
			name: "unfinished session cannot be complete",
			cp: func() Checkpoint {
				cp := Checkpoint{Session: testShell()} // no termination
				cp.Items = []ItemRecord{{Sequence: 1, Status: StatusProduced, Quality: QualityNotEvaluated}}
				cp.Session.CapturedCount = 1
				cp.Session.Coverage.Status = CoverageComplete
				return cp
			},
			wantStatus:    CoveragePartial,
			wantDowngrade: true,
		},
		{
			name: "cleanly finished session keeps complete",
			cp: func() Checkpoint {
				cp := Checkpoint{Session: testShell()}
				cp.Session.Termination = finished
				cp.Session.EndedAt = &endedAt
				cp.Items = []ItemRecord{{Sequence: 1, Status: StatusProduced, Quality: QualityNotEvaluated}}
				cp.Session.CapturedCount = 1
				cp.Session.Coverage.Status = CoverageComplete
				return cp
			},
			wantStatus:    CoverageComplete,
			wantDowngrade: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st, _ := newTestStore(t)
			id := newRecord(t, st)
			saveCheckpoint(t, st, id, tc.cp())
			rec := mustLoad(t, st, id)
			if rec.Checkpoint.Session.Coverage.Status != tc.wantStatus {
				t.Errorf("coverage = %q, want %q", rec.Checkpoint.Session.Coverage.Status, tc.wantStatus)
			}
			if rec.CoverageDowngraded != tc.wantDowngrade {
				t.Errorf("downgraded = %v, want %v", rec.CoverageDowngraded, tc.wantDowngrade)
			}
		})
	}
}

func TestRecoveredMarksRoundTrip(t *testing.T) {
	st, _ := newTestStore(t)
	id := newRecord(t, st)
	cp := Checkpoint{Session: testShell()}
	cp.Items = []ItemRecord{
		{Sequence: 1, Status: StatusProduced, Quality: QualityNotEvaluated},
		{Sequence: 2, Status: StatusRunning, Quality: QualityNotEvaluated},
	}
	cp.Session.CapturedCount = 2
	cp.Session.Coverage.Status = CoverageComplete
	saveCheckpoint(t, st, id, cp)

	rec := mustLoad(t, st, id)
	if len(rec.Interrupted) != 1 || !rec.CoverageDowngraded {
		t.Fatalf("recovery = %+v, want item 2 interrupted and coverage clamped", rec)
	}
	// The app layer persists the recovered view as the next generation.
	saveCheckpoint(t, st, id, rec.Checkpoint)

	rec2 := mustLoad(t, st, id)
	if rec2.Checkpoint.SavedRevision != 2 {
		t.Errorf("re-saved revision = %d, want 2", rec2.Checkpoint.SavedRevision)
	}
	item := rec2.Checkpoint.Items[1]
	if item.Status != StatusUnprocessed || item.Reason != ReasonInterrupted {
		t.Errorf("interrupted mark did not persist: %s/%q", item.Status, item.Reason)
	}
	if len(rec2.Interrupted) != 0 {
		t.Errorf("already-terminal item marked again: %v", rec2.Interrupted)
	}
	if rec2.Checkpoint.Session.Coverage.Status != CoveragePartial || rec2.CoverageDowngraded {
		t.Errorf("coverage = %q downgraded=%v, want partial once and stable", rec2.Checkpoint.Session.Coverage.Status, rec2.CoverageDowngraded)
	}
}

func TestRetentionKeepsVerifiedGenerationAfterRestart(t *testing.T) {
	// A new Store instance simulates a process restart: it has verified
	// nothing, so its first save must not delete the older generations
	// it cannot judge.
	root := t.TempDir()
	first, err := newStore(root, osFS{})
	if err != nil {
		t.Fatal(err)
	}
	id := "rec-1"
	if err := first.CreateRecord(id); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		saveCheckpoint(t, first, id, Checkpoint{Session: testShell()})
	}
	if files := generationFiles(t, root, id); len(files) != 2 {
		t.Fatalf("generations before restart = %v, want two", files)
	}

	second, err := newStore(root, osFS{})
	if err != nil {
		t.Fatal(err)
	}
	saveCheckpoint(t, second, id, Checkpoint{Session: testShell()})
	files := generationFiles(t, root, id)
	if len(files) != 3 {
		t.Errorf("generations after unverified-restart save = %v, want all three kept", files)
	}
	// Once the restarted store loads (and thereby verifies) a
	// generation, the next save prunes back to current + previous.
	if _, err := second.Load(id); err != nil {
		t.Fatal(err)
	}
	saveCheckpoint(t, second, id, Checkpoint{Session: testShell()})
	files = generationFiles(t, root, id)
	if len(files) != 2 || files[0] != "4.json" || files[1] != "5.json" {
		t.Errorf("generations after verified save = %v, want [4.json 5.json]", files)
	}
}

// M2: failureParams is an optional v2 addition. A v2 checkpoint written
// before it existed (the field omitted) must still load under the strict
// decoder.
func TestLoadV2CheckpointWithoutFailureParams(t *testing.T) {
	st, _ := newTestStore(t)
	id := newRecord(t, st)
	cp := Checkpoint{Session: testShell()}
	saveCheckpoint(t, st, id, cp)
	rec := mustLoad(t, st, id)
	if rec.Checkpoint.Session.FailureParams != nil {
		t.Fatalf("failureParams = %v, want nil for an old v2 record", rec.Checkpoint.Session.FailureParams)
	}
}

type blockingReadFile struct {
	File
	started   chan struct{}
	release   chan struct{}
	closed    chan struct{}
	once      sync.Once
	closeOnce sync.Once
}

func (f *blockingReadFile) Read(p []byte) (int, error) {
	if f.started != nil {
		f.once.Do(func() { close(f.started) })
	}
	<-f.release
	return f.File.Read(p)
}

// blockingCheckpointFS blocks the first checkpoint read, so Load holds the
// store mutex until released.
type blockingCheckpointFS struct {
	FS
	path    string
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (f *blockingCheckpointFS) OpenFile(path string, flag int, perm fs.FileMode) (File, error) {
	file, err := f.FS.OpenFile(path, flag, perm)
	if err != nil || path != f.path {
		return file, err
	}
	f.once.Do(func() { close(f.started) })
	return &blockingReadFile{File: file, release: f.release}, nil
}

// blockingStatFS blocks one Stat call and records whether the adjacent
// ReadDir/OpenFile calls happened before cancellation was observed.
type blockingStatFS struct {
	FS
	path     string
	started  chan struct{}
	release  chan struct{}
	once     sync.Once
	readDirs int
	opens    int
}

func (f *blockingStatFS) Stat(path string) (fs.FileInfo, error) {
	info, err := f.FS.Stat(path)
	if path == f.path && err == nil {
		f.once.Do(func() { close(f.started) })
		<-f.release
	}
	return info, err
}

func (f *blockingStatFS) ReadDir(dir string) ([]fs.DirEntry, error) {
	f.readDirs++
	return f.FS.ReadDir(dir)
}

func (f *blockingStatFS) OpenFile(path string, flag int, perm fs.FileMode) (File, error) {
	if path == f.path {
		f.opens++
	}
	return f.FS.OpenFile(path, flag, perm)
}

func (f *blockingReadFile) Close() error {
	if f.closed != nil {
		f.closeOnce.Do(func() { close(f.closed) })
	}
	return f.File.Close()
}

type blockingEvidenceFS struct {
	FS
	path    string
	started chan struct{}
	release chan struct{}
	closed  chan struct{}
}

func (f *blockingEvidenceFS) OpenFile(path string, flag int, perm fs.FileMode) (File, error) {
	file, err := f.FS.OpenFile(path, flag, perm)
	if err != nil || path != f.path {
		return file, err
	}
	return &blockingReadFile{File: file, started: f.started, release: f.release, closed: f.closed}, nil
}

// A native read already inside the OS cannot be interrupted, but LoadLatest
// must reject it at the next boundary and never return a partially verified
// newest generation.
func TestLoadLatestCancelsWhileEvidenceReadBlocks(t *testing.T) {
	st, root := newTestStore(t)
	id := newRecord(t, st)
	shell := testShell()
	shell.SessionID = id
	saveCheckpoint(t, st, id, Checkpoint{Session: shell})
	ev := writeEvidence(t, st, id, "coverage-end", bytes.Repeat([]byte("c"), 32))
	shell.CoverageEvidenceIDs = []string{ev.ID}
	saveCheckpoint(t, st, id, Checkpoint{Session: shell, Evidence: []EvidenceFile{ev}})

	path := filepath.Join(root, "sessions", id, "evidence", ev.ID+".png")
	fsys := &blockingEvidenceFS{
		FS:      osFS{},
		path:    path,
		started: make(chan struct{}),
		release: make(chan struct{}),
		closed:  make(chan struct{}),
	}
	blocked, err := newStore(root, fsys)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := blocked.LoadLatest(ctx, id)
		done <- err
	}()
	select {
	case <-fsys.started:
	case <-time.After(5 * time.Second):
		t.Fatal("evidence read did not start")
	}
	cancel()
	close(fsys.release)
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("LoadLatest after cancellation = %v, want context.Canceled", err)
		}
		select {
		case <-fsys.closed:
		default:
			t.Fatal("the blocked evidence handle was not closed")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("LoadLatest did not return after the blocked read was released")
	}
}

func TestLoadLatestCancelsWhileLoadHoldsStoreLock(t *testing.T) {
	st, root := newTestStore(t)
	id := newRecord(t, st)
	shell := testShell()
	shell.SessionID = id
	saveCheckpoint(t, st, id, Checkpoint{Session: shell})
	fsys := &blockingCheckpointFS{
		FS:      osFS{},
		path:    filepath.Join(root, "sessions", id, "checkpoints", "1.json"),
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	blocked, err := newStore(root, fsys)
	if err != nil {
		t.Fatal(err)
	}
	loadDone := make(chan error, 1)
	go func() {
		_, err := blocked.Load(id)
		loadDone <- err
	}()
	select {
	case <-fsys.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the ordinary load did not hold the store lock")
	}

	ctx, cancel := context.WithCancel(context.Background())
	latestDone := make(chan error, 1)
	go func() {
		_, err := blocked.LoadLatest(ctx, id)
		latestDone <- err
	}()
	cancel()
	select {
	case err := <-latestDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("LoadLatest while the lock is held = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("LoadLatest waited for the ordinary load's store lock")
	}
	close(fsys.release)
	select {
	case err := <-loadDone:
		if err != nil {
			t.Fatalf("ordinary Load after release: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ordinary Load did not finish after release")
	}
}

func TestLoadLatestWaitsForStoreLockAndContinues(t *testing.T) {
	st, root := newTestStore(t)
	id := newRecord(t, st)
	shell := testShell()
	shell.SessionID = id
	saveCheckpoint(t, st, id, Checkpoint{Session: shell})
	fsys := &blockingCheckpointFS{
		FS:      osFS{},
		path:    filepath.Join(root, "sessions", id, "checkpoints", "1.json"),
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	blocked, err := newStore(root, fsys)
	if err != nil {
		t.Fatal(err)
	}
	loadDone := make(chan error, 1)
	go func() {
		_, err := blocked.Load(id)
		loadDone <- err
	}()
	select {
	case <-fsys.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the ordinary load did not start")
	}
	latestDone := make(chan error, 1)
	go func() {
		_, err := blocked.LoadLatest(context.Background(), id)
		latestDone <- err
	}()
	time.Sleep(5 * time.Millisecond) // let LoadLatest wait on the held lock
	close(fsys.release)
	select {
	case err := <-loadDone:
		if err != nil {
			t.Fatalf("ordinary Load: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ordinary Load did not finish")
	}
	select {
	case err := <-latestDone:
		if err != nil {
			t.Fatalf("LoadLatest after a healthy lock wait: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("LoadLatest did not continue after the lock was released")
	}
}

func TestLoadLatestCancelsBeforeReadDirAfterDirectoryStat(t *testing.T) {
	st, root := newTestStore(t)
	id := newRecord(t, st)
	saveCheckpoint(t, st, id, Checkpoint{Session: testShell()})
	fsys := &blockingStatFS{
		FS:      osFS{},
		path:    filepath.Join(root, "sessions", id),
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	blocked, err := newStore(root, fsys)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := blocked.LoadLatest(ctx, id)
		done <- err
	}()
	select {
	case <-fsys.started:
	case <-time.After(5 * time.Second):
		t.Fatal("directory Stat did not block")
	}
	cancel()
	close(fsys.release)
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("LoadLatest after directory Stat cancellation = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("LoadLatest did not return after directory Stat cancellation")
	}
	if fsys.readDirs != 0 || fsys.opens != 0 {
		t.Fatalf("cancelled directory read still called ReadDir/OpenFile: %d/%d", fsys.readDirs, fsys.opens)
	}
}

func TestLoadLatestCancelsBeforeOpenAfterEvidenceStat(t *testing.T) {
	st, root := newTestStore(t)
	id := newRecord(t, st)
	shell := testShell()
	shell.SessionID = id
	saveCheckpoint(t, st, id, Checkpoint{Session: shell})
	ev := writeEvidence(t, st, id, "coverage-end", bytes.Repeat([]byte("c"), 32))
	shell.CoverageEvidenceIDs = []string{ev.ID}
	saveCheckpoint(t, st, id, Checkpoint{Session: shell, Evidence: []EvidenceFile{ev}})

	path := filepath.Join(root, "sessions", id, "evidence", ev.ID+".png")
	fsys := &blockingStatFS{
		FS:      osFS{},
		path:    path,
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	blocked, err := newStore(root, fsys)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := blocked.LoadLatest(ctx, id)
		done <- err
	}()
	select {
	case <-fsys.started:
	case <-time.After(5 * time.Second):
		t.Fatal("evidence Stat did not block")
	}
	cancel()
	close(fsys.release)
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("LoadLatest after evidence Stat cancellation = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("LoadLatest did not return after evidence Stat cancellation")
	}
	if fsys.opens != 0 {
		t.Fatalf("cancelled evidence Stat still called OpenFile %d time(s)", fsys.opens)
	}
}
