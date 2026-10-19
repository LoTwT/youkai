package record

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestRetentionLeavesForeignEntriesAlone pins the deletion boundary of the
// only removal path in the store: pruning checkpoint generations deletes
// canonical generation files inside the record's own checkpoints directory
// and nothing else. Foreign files, directories and symlinks below that
// directory — and anything outside it — stay untouched, so a cleanup can
// never cross the target directory.
func TestRetentionLeavesForeignEntriesAlone(t *testing.T) {
	st, root := newTestStore(t)
	id := newRecord(t, st)
	for range 3 {
		saveCheckpoint(t, st, id, Checkpoint{Session: testShell()})
	}
	checkpoints := filepath.Join(root, "sessions", id, "checkpoints")

	foreignFile := filepath.Join(checkpoints, "keep.txt")
	if err := os.WriteFile(foreignFile, []byte("not a generation"), 0o644); err != nil {
		t.Fatal(err)
	}
	foreignDir := filepath.Join(checkpoints, "nested")
	if err := os.Mkdir(foreignDir, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "outside.txt")
	if err := os.WriteFile(outside, []byte("outside the record"), 0o644); err != nil {
		t.Fatal(err)
	}

	// A symlink carrying a canonical-looking generation name must not be
	// followed or removed. Some Windows configurations forbid symlinks
	// without extra privileges; the rest of the test still applies there.
	symlink := filepath.Join(checkpoints, "99.json")
	symlinked := os.Symlink(outside, symlink) == nil

	// The fourth save prunes generation 2 and keeps 3 and 4.
	if rev := saveCheckpoint(t, st, id, Checkpoint{Session: testShell()}); rev != 4 {
		t.Fatalf("fourth save revision = %d, want 4", rev)
	}
	gens, err := st.generations(id)
	if err != nil {
		t.Fatalf("generations: %v", err)
	}
	var revisions []int64
	for _, g := range gens {
		revisions = append(revisions, g.revision)
	}
	if !slices.Equal(revisions, []int64{4, 3}) {
		t.Fatalf("canonical generations = %v, want [4 3]", revisions)
	}
	if _, err := os.Stat(filepath.Join(checkpoints, "2.json")); !os.IsNotExist(err) {
		t.Fatalf("pruned generation 2 still exists: %v", err)
	}

	got, err := os.ReadFile(foreignFile)
	if err != nil || !bytes.Equal(got, []byte("not a generation")) {
		t.Fatalf("foreign file after prune = %q (err %v)", got, err)
	}
	if info, err := os.Stat(foreignDir); err != nil || !info.IsDir() {
		t.Fatalf("foreign directory after prune: info %+v err %v", info, err)
	}
	got, err = os.ReadFile(outside)
	if err != nil || !bytes.Equal(got, []byte("outside the record")) {
		t.Fatalf("file outside the record after prune = %q (err %v)", got, err)
	}
	if symlinked {
		if _, err := os.Lstat(symlink); err != nil {
			t.Fatalf("foreign symlink after prune: %v", err)
		}
		got, err = os.ReadFile(outside)
		if err != nil || !bytes.Equal(got, []byte("outside the record")) {
			t.Fatalf("symlink target after prune = %q (err %v)", got, err)
		}
	}
}
