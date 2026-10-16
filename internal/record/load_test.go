package record

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

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
