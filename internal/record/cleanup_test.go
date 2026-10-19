package record

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// cleanupRecord writes a record whose single produced item cites one
// piece of evidence and whose coverage cites another, then saves it as
// generation 1.
func cleanupRecord(t *testing.T, st *Store, id string) (EvidenceFile, EvidenceFile) {
	t.Helper()
	if err := st.CreateRecord(id); err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}
	detail := writeEvidence(t, st, id, "detail-1", bytes.Repeat([]byte("d"), 120))
	coverage := writeEvidence(t, st, id, "coverage-end", bytes.Repeat([]byte("c"), 80))
	cp := Checkpoint{
		Session: testShell(),
		Items: []ItemRecord{{
			Sequence:    1,
			Status:      StatusProduced,
			Quality:     QualityNeedsReview,
			Revision:    1,
			Observed:    []byte(`{"lines":[{"role":"name"}]}`),
			Data:        []byte(`{"lines":[],"structured":{"setId":31100}}`),
			EvidenceIDs: []string{"detail-1"},
			Revisions: []ItemRevision{{
				Revision:  1,
				SavedAt:   time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC),
				Confirmed: false,
			}},
		}},
		Evidence: []EvidenceFile{detail, coverage},
	}
	cp.Session.CoverageEvidenceIDs = []string{"coverage-end"}
	cp.Session.CapturedCount = 1
	cp.Session.Coverage.Status = CoveragePartial
	saveCheckpoint(t, st, id, cp)
	return detail, coverage
}

func TestDeleteEvidenceWritesNewGenerationAndKeepsReviewState(t *testing.T) {
	st, root := newTestStore(t)
	id := "rec-1"
	detail, coverage := cleanupRecord(t, st, id)

	before := mustLoad(t, st, id)
	beforeItem := before.Checkpoint.Items[0]

	removal, err := st.DeleteEvidence(id)
	if err != nil {
		t.Fatalf("DeleteEvidence: %v", err)
	}
	if removal.Generation != 2 {
		t.Errorf("generation = %d, want 2", removal.Generation)
	}
	if removal.RemovedFiles != 2 || removal.EvidenceFiles != 2 {
		t.Errorf("files = %d/%d removed, want 2/2", removal.RemovedFiles, removal.EvidenceFiles)
	}
	if removal.RemovedBytes != detail.ByteLength+coverage.ByteLength {
		t.Errorf("removed bytes = %d, want %d", removal.RemovedBytes, detail.ByteLength+coverage.ByteLength)
	}
	if len(removal.Leftovers) != 0 {
		t.Errorf("leftovers = %v, want none", removal.Leftovers)
	}

	rec := mustLoad(t, st, id)
	if rec.Checkpoint.SavedRevision != 2 {
		t.Errorf("saved revision = %d, want 2", rec.Checkpoint.SavedRevision)
	}
	if len(rec.Checkpoint.Evidence) != 0 {
		t.Errorf("evidence index = %v, want empty", rec.Checkpoint.Evidence)
	}
	if len(rec.Checkpoint.Session.CoverageEvidenceIDs) != 0 {
		t.Errorf("coverage evidence = %v, want empty", rec.Checkpoint.Session.CoverageEvidenceIDs)
	}
	item := rec.Checkpoint.Items[0]
	if len(item.EvidenceIDs) != 0 {
		t.Errorf("item evidence = %v, want empty", item.EvidenceIDs)
	}
	if item.Status != beforeItem.Status || item.Quality != beforeItem.Quality ||
		item.Revision != beforeItem.Revision || len(item.Revisions) != len(beforeItem.Revisions) {
		t.Errorf("item state changed: %+v, want %+v", item, beforeItem)
	}
	if !bytes.Equal(item.Data, beforeItem.Data) || !bytes.Equal(item.Observed, beforeItem.Observed) {
		t.Error("item payload changed by screenshot deletion")
	}
	if rec.Checkpoint.Session.Coverage.Status != CoveragePartial {
		t.Errorf("coverage status = %q, want partial preserved", rec.Checkpoint.Session.Coverage.Status)
	}
	for _, name := range []string{"detail-1.png", "coverage-end.png"} {
		if _, err := os.Stat(filepath.Join(root, "sessions", id, "evidence", name)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s still on disk (err %v)", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "sessions", id, "evidence")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("evidence directory still exists (err %v)", err)
	}

	// The record remains a normal editable record: a later manual revision
	// still appends to the screenshot-free generation.
	_, gen, err := st.SaveRevision(id, 1, 1, ItemEdit{
		Data:      []byte(`{"lines":[],"structured":{"setId":31100,"level":15}}`),
		Confirmed: false,
	})
	if err != nil {
		t.Fatalf("SaveRevision after evidence deletion: %v", err)
	}
	if gen != 3 {
		t.Errorf("revision generation = %d, want 3", gen)
	}
	after := mustLoad(t, st, id)
	if got := after.Checkpoint.Items[0]; got.Revision != 2 || got.Quality != QualityNeedsReview {
		t.Errorf("item after revision = %+v, want revision 2 needsReview", got)
	}
}

func TestDeleteEvidenceCheckpointFailureKeepsFiles(t *testing.T) {
	st, ffs, root := newFaultStore(t)
	id := "rec-1"
	cleanupRecord(t, st, id)

	ffs.inject(faultCreate, "2.json", errFaultCreate)
	if _, err := st.DeleteEvidence(id); !errors.Is(err, errFaultCreate) {
		t.Fatalf("DeleteEvidence = %v, want the checkpoint write failure", err)
	}
	for _, name := range []string{"detail-1.png", "coverage-end.png"} {
		if _, err := os.Stat(filepath.Join(root, "sessions", id, "evidence", name)); err != nil {
			t.Errorf("%s was deleted despite the failed checkpoint: %v", name, err)
		}
	}
	rec := mustLoad(t, st, id)
	if rec.Checkpoint.SavedRevision != 1 || len(rec.Checkpoint.Evidence) != 2 {
		t.Errorf("record = revision %d with %d evidence, want the untouched generation 1",
			rec.Checkpoint.SavedRevision, len(rec.Checkpoint.Evidence))
	}

	// Retry after the fault clears removes the screenshots and advances
	// the revision.
	ffs.clear()
	removal, err := st.DeleteEvidence(id)
	if err != nil {
		t.Fatalf("retry DeleteEvidence: %v", err)
	}
	if removal.Generation != 2 || removal.RemovedFiles != 2 {
		t.Errorf("retry = generation %d, %d files; want 2 and 2", removal.Generation, removal.RemovedFiles)
	}
}

func TestDeleteEvidenceFileFailureLeavesLoadableRecordAndRetry(t *testing.T) {
	st, ffs, root := newFaultStore(t)
	id := "rec-1"
	cleanupRecord(t, st, id)

	ffs.inject(faultRemove, "detail-1.png", errFaultRemove)
	removal, err := st.DeleteEvidence(id)
	if !errors.Is(err, errFaultRemove) {
		t.Fatalf("DeleteEvidence = %v, want the remove failure", err)
	}
	if removal.Generation != 2 {
		t.Errorf("generation = %d, want the checkpoint saved before the file failed", removal.Generation)
	}
	if len(removal.Leftovers) != 1 || removal.Leftovers[0] != "detail-1" {
		t.Errorf("leftovers = %v, want [detail-1]", removal.Leftovers)
	}
	if _, err := os.Stat(filepath.Join(root, "sessions", id, "evidence", "detail-1.png")); err != nil {
		t.Fatalf("leftover file is gone: %v", err)
	}
	// The record is loadable because the saved generation no longer
	// references the surviving file.
	if rec := mustLoad(t, st, id); rec.Checkpoint.SavedRevision != 2 || len(rec.Checkpoint.Evidence) != 0 {
		t.Errorf("record after partial deletion = revision %d, %d evidence",
			rec.Checkpoint.SavedRevision, len(rec.Checkpoint.Evidence))
	}

	ffs.clear()
	retry, err := st.DeleteEvidence(id)
	if err != nil {
		t.Fatalf("retry DeleteEvidence: %v", err)
	}
	if retry.Generation != 0 {
		t.Errorf("retry generation = %d, want 0 (no reference change)", retry.Generation)
	}
	if retry.RemovedFiles != 1 || len(retry.Leftovers) != 0 {
		t.Errorf("retry removed %d files, leftovers %v; want 1 and none", retry.RemovedFiles, retry.Leftovers)
	}
	if _, err := os.Stat(filepath.Join(root, "sessions", id, "evidence")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("evidence directory survived the retry (err %v)", err)
	}
}

func TestDeleteEvidenceDoesNotFollowEvidenceSymlinks(t *testing.T) {
	st, root := newTestStore(t)
	id := "rec-1"
	cleanupRecord(t, st, id)
	outside := filepath.Join(t.TempDir(), "outside.png")
	if err := os.WriteFile(outside, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "sessions", id, "evidence", "link.png")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := st.DeleteEvidence(id); err != nil {
		t.Fatalf("DeleteEvidence: %v", err)
	}
	if data, err := os.ReadFile(outside); err != nil || string(data) != "outside" {
		t.Fatalf("symlink target was touched (data %q, err %v)", data, err)
	}
	if _, err := os.Lstat(link); err != nil {
		t.Errorf("evidence symlink was removed: %v", err)
	}
	// The record still loads: only the indexed evidence mattered.
	if rec := mustLoad(t, st, id); rec.Checkpoint.SavedRevision != 2 {
		t.Errorf("saved revision = %d, want 2", rec.Checkpoint.SavedRevision)
	}
}

func TestDeleteEvidenceRefusesSymlinkedEvidenceDirectory(t *testing.T) {
	st, root := newTestStore(t)
	id := "rec-1"
	cleanupRecord(t, st, id)
	evidenceDir := filepath.Join(root, "sessions", id, "evidence")
	outsideDir := filepath.Join(t.TempDir(), "evidence")
	if err := os.Rename(evidenceDir, outsideDir); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideDir, evidenceDir); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	before := readEvidenceFile(t, root, id, "detail-1.png")

	if _, err := st.DeleteEvidence(id); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("DeleteEvidence = %v, want a symbolic-link refusal", err)
	}
	if after := readEvidenceFile(t, root, id, "detail-1.png"); !bytes.Equal(before, after) {
		t.Error("evidence behind the symlink was changed or deleted")
	}
	// The checkpoint written before the refusal no longer cites the
	// screenshots, so the record still loads; only the external files
	// were left alone.
	if rec := mustLoad(t, st, id); rec.Checkpoint.SavedRevision != 2 || len(rec.Checkpoint.Evidence) != 0 {
		t.Errorf("record after refused file deletion = revision %d, %d evidence",
			rec.Checkpoint.SavedRevision, len(rec.Checkpoint.Evidence))
	}
	if usage, err := st.Usage(id); err != nil {
		t.Fatalf("Usage: %v", err)
	} else if usage.EvidenceFiles != 0 {
		t.Errorf("usage counted files behind the symlink: %+v", usage)
	}
}

func TestDeleteRecordRejectsUnsafeTargets(t *testing.T) {
	st, root := newTestStore(t)
	id := newRecord(t, st)
	saveCheckpoint(t, st, id, Checkpoint{Session: testShell()})

	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "kept.txt")
	if err := os.WriteFile(outsideFile, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "sessions", "link-1")
	if err := os.Symlink(outsideDir, link); err != nil {
		t.Logf("symlink test skipped: %v", err)
	} else {
		if _, err := st.DeleteRecord("link-1"); err == nil || !strings.Contains(err.Error(), "symbolic link") {
			t.Errorf("DeleteRecord(symlink) = %v, want a symbolic-link refusal", err)
		}
		if data, err := os.ReadFile(outsideFile); err != nil || string(data) != "keep" {
			t.Errorf("symlink target was touched (data %q, err %v)", data, err)
		}
	}

	fileEntry := filepath.Join(root, "sessions", "file-1")
	if err := os.WriteFile(fileEntry, []byte("not a record"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DeleteRecord("file-1"); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("DeleteRecord(file) = %v, want a not-a-directory refusal", err)
	}
	if _, err := st.DeleteRecord("file-1"); err == nil {
		t.Errorf("file entry was deleted")
	}

	for _, bad := range []string{"", "../escape", "Upper-Case", "with space", "a/b", "con"} {
		if _, err := st.DeleteRecord(bad); !errors.Is(err, ErrInvalidID) {
			t.Errorf("DeleteRecord(%q) = %v, want ErrInvalidID", bad, err)
		}
	}
	if data, err := os.ReadFile(outsideFile); err != nil || string(data) != "keep" {
		t.Errorf("outside file changed (data %q, err %v)", data, err)
	}
	if _, err := os.Stat(filepath.Join(root, "sessions", id)); err != nil {
		t.Errorf("valid record disappeared after refused deletes: %v", err)
	}

	if _, err := st.DeleteRecord(id); err != nil {
		t.Fatalf("DeleteRecord: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "sessions", id)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("record directory survived deletion (err %v)", err)
	}
	if _, err := st.DeleteRecord(id); !errors.Is(err, ErrRecordNotFound) {
		t.Errorf("second DeleteRecord = %v, want ErrRecordNotFound", err)
	}
}

func TestDeleteAllRecordsContinuesAfterFailures(t *testing.T) {
	st, ffs, root := newFaultStore(t)
	for _, id := range []string{"rec-1", "rec-2"} {
		if err := st.CreateRecord(id); err != nil {
			t.Fatalf("CreateRecord %s: %v", id, err)
		}
		saveCheckpoint(t, st, id, Checkpoint{Session: testShell()})
	}
	ffs.inject(faultRemoveAll, "rec-1", errFaultRemove)

	batch, err := st.DeleteAllRecords()
	if !errors.Is(err, errFaultRemove) {
		t.Fatalf("DeleteAllRecords = %v, want the remove-all failure", err)
	}
	if batch.Records != 1 || len(batch.Failed) != 1 || batch.Failed[0].RecordID != "rec-1" {
		t.Errorf("batch = %+v, want one deleted and rec-1 failed", batch)
	}
	if _, err := os.Stat(filepath.Join(root, "sessions", "rec-1")); err != nil {
		t.Errorf("failed record was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "sessions", "rec-2")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("rec-2 survived (err %v)", err)
	}
	if rec := mustLoad(t, st, "rec-1"); rec.Checkpoint.SavedRevision != 1 {
		t.Errorf("failed record no longer loads")
	}

	ffs.clear()
	retry, err := st.DeleteAllRecords()
	if err != nil {
		t.Fatalf("retry DeleteAllRecords: %v", err)
	}
	if retry.Records != 1 || len(retry.Failed) != 0 {
		t.Errorf("retry = %+v, want rec-1 removed", retry)
	}
}

func TestDeleteAllEvidenceContinuesAfterFailures(t *testing.T) {
	st, ffs, root := newFaultStore(t)
	cleanupRecord(t, st, "rec-1")
	cleanupRecord(t, st, "rec-2")
	ffs.inject(faultRemove, filepath.Join("rec-1", "evidence", "detail-1.png"), errFaultRemove)

	batch, err := st.DeleteAllEvidence()
	if !errors.Is(err, errFaultRemove) {
		t.Fatalf("DeleteAllEvidence = %v, want the remove failure", err)
	}
	if len(batch.Failed) != 1 || batch.Failed[0].RecordID != "rec-1" {
		t.Errorf("failed = %v, want rec-1", batch.Failed)
	}
	if batch.EvidenceFiles != 4 || batch.RemovedFiles != 3 {
		t.Errorf("batch files = %d/%d removed, want 4/3", batch.RemovedFiles, batch.EvidenceFiles)
	}
	for _, id := range []string{"rec-1", "rec-2"} {
		rec := mustLoad(t, st, id)
		if len(rec.Checkpoint.Evidence) != 0 {
			t.Errorf("%s still cites evidence after the batch", id)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "sessions", "rec-1", "evidence", "detail-1.png")); err != nil {
		t.Errorf("rec-1's failed file is gone: %v", err)
	}

	ffs.clear()
	retry, err := st.DeleteAllEvidence()
	if err != nil {
		t.Fatalf("retry DeleteAllEvidence: %v", err)
	}
	if retry.RemovedFiles != 1 || len(retry.Failed) != 0 {
		t.Errorf("retry removed %d files with failures %v, want 1 and none",
			retry.RemovedFiles, retry.Failed)
	}
}

func TestUsageMeasuresRecordsAndListingReportsSizes(t *testing.T) {
	st, _ := newTestStore(t)
	cleanupRecord(t, st, "rec-1")

	usage, err := st.Usage("rec-1")
	if err != nil {
		t.Fatalf("Usage: %v", err)
	}
	if usage.EvidenceFiles != 2 || usage.EvidenceBytes != 200 {
		t.Errorf("usage = %+v, want 2 evidence files and 200 bytes", usage)
	}
	if usage.TotalBytes <= usage.EvidenceBytes {
		t.Errorf("total %d does not include the checkpoint beyond evidence %d",
			usage.TotalBytes, usage.EvidenceBytes)
	}

	infos, err := st.ListRecords()
	if err != nil || len(infos) != 1 {
		t.Fatalf("ListRecords = %+v err %v", infos, err)
	}
	if infos[0].TotalBytes != usage.TotalBytes || infos[0].EvidenceBytes != usage.EvidenceBytes ||
		infos[0].EvidenceFiles != usage.EvidenceFiles {
		t.Errorf("listing sizes = %+v, want %+v", infos[0], usage)
	}

	storeUsage, err := st.StoreUsage()
	if err != nil {
		t.Fatalf("StoreUsage: %v", err)
	}
	if storeUsage.Records != 1 || storeUsage.TotalBytes != usage.TotalBytes ||
		storeUsage.EvidenceBytes != usage.EvidenceBytes {
		t.Errorf("store usage = %+v, want one record matching %+v", storeUsage, usage)
	}
}
