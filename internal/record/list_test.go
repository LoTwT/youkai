package record

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// setSavedAt rewrites a generation file's modification time so the
// listing's newest-first order is testable without depending on the
// filesystem's timestamp granularity.
func setSavedAt(t *testing.T, path string, at time.Time) {
	t.Helper()
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}

func TestListRecordsEmptyRoot(t *testing.T) {
	st, root := newTestStore(t)
	infos, err := st.ListRecords()
	if err != nil {
		t.Fatalf("ListRecords: %v", err)
	}
	if len(infos) != 0 {
		t.Fatalf("ListRecords = %v, want none", infos)
	}
	if _, err := os.Stat(filepath.Join(root, "sessions")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ListRecords created the sessions directory: %v", err)
	}
}

func TestListRecordsNewestFirstWithRevisionAndTime(t *testing.T) {
	st, _ := newTestStore(t)
	first, second, third := "list-first", "list-second", "list-third"
	for _, id := range []string{first, second, third} {
		if err := st.CreateRecord(id); err != nil {
			t.Fatalf("CreateRecord %s: %v", id, err)
		}
	}

	base := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		id    string
		gen   int64
		saved time.Time
	}{
		{first, 1, base.Add(-2 * time.Hour)},
		{second, 3, base},
		{third, 2, base.Add(-1 * time.Hour)},
	} {
		cp := validDoc()
		cp.Session.SessionID = tc.id
		// validDoc's evidence must be backed by real files; replace the
		// hand-written index with the descriptors the store itself
		// produced, so sizes and digests are the ones on disk.
		index := make([]EvidenceFile, 0, len(cp.Evidence))
		for _, ev := range cp.Evidence {
			got, err := st.WriteEvidence(tc.id, ev.ID, []byte(ev.ID+" evidence"))
			if err != nil {
				t.Fatalf("write evidence %s/%s: %v", tc.id, ev.ID, err)
			}
			index = append(index, got)
		}
		cp.Evidence = index
		for gen := int64(1); gen <= tc.gen; gen++ {
			if _, err := st.SaveCheckpoint(tc.id, *cp); err != nil {
				t.Fatalf("save %s: %v", tc.id, err)
			}
		}
		setSavedAt(t, filepath.Join(st.checkpointsDir(tc.id), strconv.FormatInt(tc.gen, 10)+".json"), tc.saved)
	}

	infos, err := st.ListRecords()
	if err != nil {
		t.Fatalf("ListRecords: %v", err)
	}
	want := []string{second, third, first}
	if len(infos) != len(want) {
		t.Fatalf("ListRecords returned %d records, want %d: %+v", len(infos), len(want), infos)
	}
	for i, id := range want {
		if infos[i].ID != id {
			t.Fatalf("record %d = %q, want %q", i, infos[i].ID, id)
		}
	}
	if infos[0].LatestRevision != 3 || infos[1].LatestRevision != 2 || infos[2].LatestRevision != 1 {
		t.Fatalf("revisions = %d, %d, %d; want 3, 2, 1",
			infos[0].LatestRevision, infos[1].LatestRevision, infos[2].LatestRevision)
	}
	if !infos[0].SavedAt.Equal(base) {
		t.Fatalf("newest saved at = %v, want %v", infos[0].SavedAt, base)
	}
}

func TestListRecordsIncludesEmptyAndSkipsForeignEntries(t *testing.T) {
	st, root := newTestStore(t)
	// A record directory whose save never completed: listed, marked by
	// its absence of generations.
	empty := newRecord(t, st)
	// Foreign entries below sessions/: a file, an invalid id and a
	// reserved device name. None of them are records the store wrote.
	sessions := filepath.Join(root, "sessions")
	for _, name := range []string{"not-a-record.txt", "Upper-Case", "con"} {
		if err := os.MkdirAll(filepath.Join(sessions, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(sessions, "0f0e0d0c-1a2b-3c4d-5e6f-7a8b9c0d1e2f.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	infos, err := st.ListRecords()
	if err != nil {
		t.Fatalf("ListRecords: %v", err)
	}
	if len(infos) != 1 || infos[0].ID != empty {
		t.Fatalf("ListRecords = %+v, want only %q", infos, empty)
	}
	if infos[0].LatestRevision != 0 || !infos[0].SavedAt.IsZero() {
		t.Fatalf("empty record listed as %+v, want zero revision and time", infos[0])
	}
}
