package record

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// revisionRecord prepares a record with one produced item that carries
// an original observation and one queued item, saved as a checkpoint.
func revisionRecord(t *testing.T, st *Store) string {
	t.Helper()
	id := newRecord(t, st)
	cp := Checkpoint{Session: testShell()}
	cp.Items = []ItemRecord{
		{
			Sequence: 1,
			Status:   StatusProduced,
			Quality:  QualityAutoAccepted,
			Observed: json.RawMessage(`{"slot":6,"level":0}`),
			Data:     json.RawMessage(`{"slot":6,"level":0}`),
		},
		{
			Sequence: 2,
			Status:   StatusQueued,
			Quality:  QualityNotEvaluated,
		},
	}
	cp.Session.CapturedCount = 2
	saveCheckpoint(t, st, id, cp)
	return id
}

func TestSaveRevisionAppendsHistory(t *testing.T) {
	st, root := newTestStore(t)
	id := revisionRecord(t, st)

	item, gen, err := st.SaveRevision(id, 1, 0, ItemEdit{
		Changes: []FieldChange{{
			Field:  "slot",
			Before: json.RawMessage(`6`),
			After:  json.RawMessage(`5`),
		}},
		Data: json.RawMessage(`{"slot":5,"level":0}`),
	})
	if err != nil {
		t.Fatalf("SaveRevision: %v", err)
	}
	if item.Revision != 1 || item.Quality != QualityNeedsReview {
		t.Errorf("item after edit = revision %d quality %q, want 1/needsReview", item.Revision, item.Quality)
	}
	if string(item.Data) != `{"slot":5,"level":0}` {
		t.Errorf("current payload = %s, want the edit's payload", item.Data)
	}
	// The original observation is untouched by the manual edit.
	if string(item.Observed) != `{"slot":6,"level":0}` || item.Status != StatusProduced || item.Quality == QualityHumanConfirmed {
		t.Errorf("original observation disturbed: %+v", item)
	}
	if len(item.Revisions) != 1 {
		t.Fatalf("history = %d entries, want 1", len(item.Revisions))
	}
	entry := item.Revisions[0]
	if entry.Revision != 1 || entry.SavedAt.IsZero() || entry.Confirmed {
		t.Errorf("history entry = %+v, want revision 1, timestamped, unconfirmed", entry)
	}
	if len(entry.Changes) != 1 || entry.Changes[0].Field != "slot" {
		t.Errorf("changes = %+v, want the recorded slot edit", entry.Changes)
	}
	if gen != 2 {
		t.Errorf("generation = %d, want 2 (one more than the base)", gen)
	}
	if files := generationFiles(t, root, id); len(files) != 2 {
		t.Errorf("generations = %v, want the base and the revision save", files)
	}

	// The edit is visible to a fresh load, and the memo advanced: the
	// next edit must use the new revision.
	rec := mustLoad(t, st, id)
	if got := rec.Checkpoint.Items[0]; got.Revision != 1 || got.Quality != QualityNeedsReview {
		t.Errorf("loaded item = %+v, want revision 1 needsReview", got)
	}
	if _, _, err := st.SaveRevision(id, 1, 1, ItemEdit{Confirmed: true}); err != nil {
		t.Fatalf("follow-up edit with the advanced revision: %v", err)
	}
}

func TestSaveRevisionConfirmDerivesQuality(t *testing.T) {
	st, _ := newTestStore(t)
	id := revisionRecord(t, st)

	// A bare whole-item confirmation: no changes, payload kept.
	item, _, err := st.SaveRevision(id, 1, 0, ItemEdit{Confirmed: true})
	if err != nil {
		t.Fatalf("SaveRevision: %v", err)
	}
	if item.Revision != 1 || item.Quality != QualityHumanConfirmed {
		t.Errorf("item = revision %d quality %q, want 1/humanConfirmed", item.Revision, item.Quality)
	}
	if string(item.Data) != `{"slot":6,"level":0}` {
		t.Errorf("payload changed by a bare confirmation: %s", item.Data)
	}

	// Confirming again — now against revision 1 — re-confirms and
	// requires review once more if the next edit is a plain change.
	item, _, err = st.SaveRevision(id, 1, 1, ItemEdit{
		Changes: []FieldChange{{Field: "level", After: json.RawMessage(`3`)}},
	})
	if err != nil {
		t.Fatalf("SaveRevision after confirmation: %v", err)
	}
	if item.Revision != 2 || item.Quality != QualityNeedsReview {
		t.Errorf("item after re-edit = revision %d quality %q, want 2/needsReview", item.Revision, item.Quality)
	}
}

func TestSaveRevisionConflicts(t *testing.T) {
	st, root := newTestStore(t)
	id := revisionRecord(t, st)
	if _, _, err := st.SaveRevision(id, 1, 0, ItemEdit{Data: json.RawMessage(`{"slot":5}`)}); err != nil {
		t.Fatalf("first edit: %v", err)
	}
	before := generationFiles(t, root, id)

	for _, expected := range []int64{0, 2, -1} {
		if _, _, err := st.SaveRevision(id, 1, expected, ItemEdit{Data: json.RawMessage(`{"slot":4}`)}); !errors.Is(err, ErrRevisionConflict) {
			t.Errorf("expected revision %d = %v, want ErrRevisionConflict", expected, err)
		}
	}
	// A rejected edit writes nothing.
	if after := generationFiles(t, root, id); len(after) != len(before) {
		t.Errorf("generations after conflicts = %v, want unchanged %v", after, before)
	}
	// The item stays at revision 1.
	rec := mustLoad(t, st, id)
	if item := rec.Checkpoint.Items[0]; item.Revision != 1 || len(item.Revisions) != 1 {
		t.Errorf("item after conflicts = revision %d with %d entries, want 1/1", item.Revision, len(item.Revisions))
	}
}

func TestSaveRevisionRejectsImpossibleTargets(t *testing.T) {
	st, root := newTestStore(t)
	id := revisionRecord(t, st)

	// Unknown sequence.
	if _, _, err := st.SaveRevision(id, 99, 0, ItemEdit{Confirmed: true}); !errors.Is(err, ErrItemNotFound) {
		t.Errorf("unknown sequence = %v, want ErrItemNotFound", err)
	}
	// In-progress items belong to the scan, not to manual editing.
	if _, _, err := st.SaveRevision(id, 2, 0, ItemEdit{Confirmed: true}); err == nil {
		t.Error("queued item accepted, want error")
	}
	// No record, no checkpoint.
	if _, _, err := st.SaveRevision("no-such-record", 1, 0, ItemEdit{}); !errors.Is(err, ErrRecordNotFound) {
		t.Errorf("unknown record = %v, want ErrRecordNotFound", err)
	}
	if err := st.CreateRecord("rec-empty"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.SaveRevision("rec-empty", 1, 0, ItemEdit{}); !errors.Is(err, ErrNoValidCheckpoint) {
		t.Errorf("record without checkpoint = %v, want ErrNoValidCheckpoint", err)
	}
	if files := generationFiles(t, root, id); len(files) != 1 {
		t.Errorf("generations = %v, want only the base", files)
	}
}

func TestSaveRevisionValidatesEdits(t *testing.T) {
	st, _ := newTestStore(t)
	id := revisionRecord(t, st)
	cases := []struct {
		name string
		edit ItemEdit
	}{
		{"broken payload", ItemEdit{Data: json.RawMessage(`{"slot":`)}},
		{"null payload", ItemEdit{Data: json.RawMessage(`null`)}},
		{"empty change field", ItemEdit{Changes: []FieldChange{{Field: ""}}}},
		{"duplicate change field", ItemEdit{Changes: []FieldChange{
			{Field: "slot", After: json.RawMessage(`5`)},
			{Field: "slot", After: json.RawMessage(`4`)},
		}}},
		{"broken change value", ItemEdit{Changes: []FieldChange{{Field: "slot", After: json.RawMessage(`{`)}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := st.SaveRevision(id, 1, 0, tc.edit); err == nil {
				t.Fatal("SaveRevision accepted an invalid edit, want error")
			}
		})
	}
	// Nothing was written and the item is unchanged.
	rec := mustLoad(t, st, id)
	if item := rec.Checkpoint.Items[0]; item.Revision != 0 || item.Quality != QualityAutoAccepted {
		t.Errorf("item = %+v, want untouched base", item)
	}
}

func TestSaveRevisionFailureKeepsPreviousResult(t *testing.T) {
	st, ffs, root := newFaultStore(t)
	id := revisionRecord(t, st)

	// The revision save's generation write fails on close.
	ffs.inject(faultClose, "2.json", errFaultClose)
	_, _, err := st.SaveRevision(id, 1, 0, ItemEdit{
		Changes: []FieldChange{{Field: "slot", Before: json.RawMessage(`6`), After: json.RawMessage(`5`)}},
		Data:    json.RawMessage(`{"slot":5}`),
	})
	if !errors.Is(err, errFaultClose) {
		t.Fatalf("SaveRevision = %v, want %v", err, errFaultClose)
	}
	// The base generation is intact and nothing partial remains.
	gen1, readErr := os.ReadFile(filepath.Join(root, "sessions", id, "checkpoints", "1.json"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if _, statErr := os.Stat(filepath.Join(root, "sessions", id, "checkpoints", "2.json")); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("failed revision save left a generation (stat err %v)", statErr)
	}
	// The in-memory base did not advance: the same expected revision
	// still applies once the fault clears.
	ffs.clear()
	item, _, err := st.SaveRevision(id, 1, 0, ItemEdit{
		Changes: []FieldChange{{Field: "slot", Before: json.RawMessage(`6`), After: json.RawMessage(`5`)}},
		Data:    json.RawMessage(`{"slot":5}`),
	})
	if err != nil {
		t.Fatalf("retry SaveRevision: %v", err)
	}
	if item.Revision != 1 {
		t.Errorf("retry item revision = %d, want 1", item.Revision)
	}
	after, err := os.ReadFile(filepath.Join(root, "sessions", id, "checkpoints", "1.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(gen1) != string(after) {
		t.Error("the base generation was rewritten by the failed save's retry")
	}
}

func TestSaveRevisionAfterRestartUsesRecoveredBase(t *testing.T) {
	// A record whose last checkpoint holds a queued item. The new store
	// instance has no memo, so the first revision edit must load (and
	// recover) first — the recovered item is unprocessed/interrupted
	// and can be completed manually.
	st, root := newTestStore(t)
	id := newRecord(t, st)
	cp := Checkpoint{Session: testShell()}
	cp.Items = []ItemRecord{{Sequence: 1, Status: StatusQueued, Quality: QualityNotEvaluated}}
	cp.Session.CapturedCount = 1
	saveCheckpoint(t, st, id, cp)

	restarted, err := newStore(root, osFS{})
	if err != nil {
		t.Fatal(err)
	}
	item, _, err := restarted.SaveRevision(id, 1, 0, ItemEdit{
		Data:      json.RawMessage(`{"slot":5}`),
		Confirmed: true,
	})
	if err != nil {
		t.Fatalf("SaveRevision after restart: %v", err)
	}
	if item.Status != StatusUnprocessed || item.Reason != ReasonInterrupted {
		t.Errorf("recovered status = %s/%q, want unprocessed/interrupted preserved by the edit", item.Status, item.Reason)
	}
	if item.Revision != 1 || item.Quality != QualityHumanConfirmed {
		t.Errorf("item = revision %d quality %q, want 1/humanConfirmed", item.Revision, item.Quality)
	}
	rec := mustLoad(t, restarted, id)
	if got := rec.Checkpoint.Items[0]; got.Reason != ReasonInterrupted || got.Revision != 1 {
		t.Errorf("loaded item = %+v, want interrupted reason kept and revision 1", got)
	}
}

func TestSaveRevisionStampedAtSaveTime(t *testing.T) {
	st, _ := newTestStore(t)
	id := revisionRecord(t, st)
	before := time.Now().UTC().Add(-time.Minute)
	item, _, err := st.SaveRevision(id, 1, 0, ItemEdit{Confirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	if item.Revisions[0].SavedAt.Before(before) {
		t.Errorf("savedAt = %v, want the store's save time, not a caller-supplied one", item.Revisions[0].SavedAt)
	}
}
