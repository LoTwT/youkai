package record

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteEvidenceBatchKeepsSuccessfulFiles(t *testing.T) {
	root := t.TempDir()
	files := &faultFS{inner: osFS{}}
	store, err := newStore(root, files)
	if err != nil {
		t.Fatal(err)
	}
	id := newRecord(t, store)
	writeFailed := errors.New("injected partial write")
	files.injectShortWrite("failed.png", 2, writeFailed)

	inputs := []EvidenceWrite{
		{ID: "first", PNG: []byte("original")},
		{ID: "failed", PNG: []byte("partial write")},
		{ID: "first", PNG: []byte("duplicate")},
		{ID: "empty"},
		{ID: "last", PNG: []byte("retained")},
	}
	results := store.WriteEvidenceBatch(id, inputs)
	if len(results) != len(inputs) {
		t.Fatalf("got %d results for %d inputs", len(results), len(inputs))
	}
	if !errors.Is(results[1].Err, writeFailed) || !errors.Is(results[2].Err, ErrEvidenceExists) || results[3].Err == nil {
		t.Fatalf("failed, duplicate and empty results: %+v", results)
	}
	dir := filepath.Join(root, "sessions", id, "evidence")
	if _, err := os.Stat(filepath.Join(dir, "failed.png")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial file cleanup: %v", err)
	}
	var saved []EvidenceFile
	for _, idx := range []int{0, 4} {
		result := results[idx]
		if result.Err != nil || result.File.ID != inputs[idx].ID {
			t.Fatalf("result %d: %+v", idx, result)
		}
		data, err := os.ReadFile(filepath.Join(dir, result.File.ID+".png"))
		if err != nil || string(data) != string(inputs[idx].PNG) {
			t.Fatalf("result %d file: %q, %v", idx, data, err)
		}
		saved = append(saved, result.File)
	}
	shell := testShell()
	shell.CapturedCount = 1
	saveCheckpoint(t, store, id, Checkpoint{
		Session: shell,
		Items: []ItemRecord{{
			Sequence: 1, Status: StatusProduced, Quality: QualityNotEvaluated,
			EvidenceIDs: []string{"first", "last"},
		}},
		Evidence: saved,
	})
}
