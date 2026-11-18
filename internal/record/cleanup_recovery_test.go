package record

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// cleanupResultCheckpoint is a checkpoint whose item carries a manual
// revision, whose coverage cites evidence and whose session terminated
// normally, so a fallback can be checked for everything cleanup must keep.
func cleanupResultCheckpoint(detail, coverage EvidenceFile) Checkpoint {
	savedAt := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	cp := Checkpoint{
		Session: testShell(),
		Items: []ItemRecord{{
			Sequence:    1,
			Status:      StatusProduced,
			Quality:     QualityHumanConfirmed,
			Revision:    2,
			Observed:    []byte(`{"lines":[{"role":"name"}]}`),
			Data:        []byte(`{"lines":[],"structured":{"setId":31100,"level":15}}`),
			EvidenceIDs: []string{detail.ID},
			Revisions: []ItemRevision{
				{Revision: 1, SavedAt: savedAt},
				{Revision: 2, SavedAt: savedAt.Add(time.Minute), Confirmed: true},
			},
		}},
		Evidence: []EvidenceFile{detail, coverage},
	}
	cp.Session.CoverageEvidenceIDs = []string{coverage.ID}
	cp.Session.CapturedCount = 1
	cp.Session.Coverage.Status = CoveragePartial
	cp.Session.Termination = &Termination{Kind: TerminationCompleted, Code: "scan-complete", Phase: PhaseFinalizing}
	return cp
}

// revisedEvidenceCheckpoint returns a checkpoint whose single item has one
// confirmation revision, partial coverage and a completed termination,
// citing ev.
func revisedEvidenceCheckpoint(ev EvidenceFile) Checkpoint {
	savedAt := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	cp := checkpointWithEvidence(ev)
	cp.Items[0].Quality = QualityHumanConfirmed
	cp.Items[0].Revision = 2
	cp.Items[0].Observed = []byte(`{"lines":[{"role":"name"}]}`)
	cp.Items[0].Data = []byte(`{"lines":[],"structured":{"setId":31100,"level":15}}`)
	cp.Items[0].Revisions = []ItemRevision{
		{Revision: 1, SavedAt: savedAt},
		{Revision: 2, SavedAt: savedAt, Confirmed: true},
	}
	cp.Session.Coverage.Status = CoveragePartial
	cp.Session.Termination = &Termination{Kind: TerminationCompleted, Code: "scan-complete", Phase: PhaseFinalizing}
	return cp
}

// Deleting screenshots must leave two generations that verify without
// them: retention keeps current and previous, and recovery falls back to
// the previous one when the newest file is damaged. Before the fix only
// the stripped generation survived, and the retained old generation cited
// files that were already deleted.
func TestDeleteEvidenceKeepsTwoRecoverableGenerations(t *testing.T) {
	st, root := newTestStore(t)
	id := "rec-1"
	if err := st.CreateRecord(id); err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}
	detail := writeEvidence(t, st, id, "detail-1", bytes.Repeat([]byte("d"), 120))
	coverage := writeEvidence(t, st, id, "coverage-end", bytes.Repeat([]byte("c"), 80))
	cp := cleanupResultCheckpoint(detail, coverage)
	saveCheckpoint(t, st, id, cp)

	if removal, err := st.DeleteEvidence(id); err != nil {
		t.Fatalf("DeleteEvidence: %v", err)
	} else if removal.Generation != 3 {
		t.Errorf("generation = %d, want 3", removal.Generation)
	}

	// Retention keeps the two reference-free generations and removes the
	// pre-cleanup one.
	files := generationFiles(t, root, id)
	if !slices.Equal(files, []string{"2.json", "3.json"}) {
		t.Fatalf("generations after cleanup = %v, want [2.json 3.json]", files)
	}
	for _, name := range files {
		rev, ok := generationNumber(name)
		if !ok {
			t.Fatalf("%q is not a generation name", name)
		}
		doc, err := st.readGenerationDocument(id, generation{revision: rev, name: name})
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if len(doc.Evidence) != 0 || len(doc.Session.CoverageEvidenceIDs) != 0 || len(doc.Items[0].EvidenceIDs) != 0 {
			t.Errorf("%s still cites screenshots: %+v", name, doc)
		}
	}

	// Damage the newest generation: the previous one must restore the
	// results, revisions, coverage and termination without the files.
	if err := os.Remove(filepath.Join(root, "sessions", id, "checkpoints", "3.json")); err != nil {
		t.Fatalf("remove newest generation: %v", err)
	}
	got := mustLoad(t, st, id).Checkpoint
	if got.SavedRevision != 2 {
		t.Errorf("fallback revision = %d, want 2", got.SavedRevision)
	}
	item := got.Items[0]
	if item.Revision != 2 || item.Quality != QualityHumanConfirmed || len(item.Revisions) != 2 {
		t.Errorf("fallback item = revision %d, quality %q, %d revisions; want 2, humanConfirmed, 2",
			item.Revision, item.Quality, len(item.Revisions))
	}
	if !sameJSON(t, item.Data, cp.Items[0].Data) || !sameJSON(t, item.Observed, cp.Items[0].Observed) {
		t.Errorf("fallback item payload differs: data %s != %s, observed %s != %s",
			item.Data, cp.Items[0].Data, item.Observed, cp.Items[0].Observed)
	}
	if got.Session.Coverage.Status != CoveragePartial {
		t.Errorf("fallback coverage = %q, want partial", got.Session.Coverage.Status)
	}
	if got.Session.Termination == nil || *got.Session.Termination != *cp.Session.Termination {
		t.Errorf("fallback termination = %+v, want %+v", got.Session.Termination, cp.Session.Termination)
	}
	if len(got.Evidence) != 0 || len(got.Session.CoverageEvidenceIDs) != 0 || len(item.EvidenceIDs) != 0 {
		t.Error("fallback generation cites deleted screenshots")
	}
	for _, name := range []string{"detail-1.png", "coverage-end.png"} {
		if _, err := os.Stat(filepath.Join(root, "sessions", id, "evidence", name)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s survived cleanup (err %v)", name, err)
		}
	}
}

// A retry in the same store must not let a stale validity verdict keep a
// generation that no longer verifies: the damaged newest generation is
// dropped from retention instead of replacing the only usable state.
func TestDeleteEvidenceRetryDoesNotKeepDamagedGeneration(t *testing.T) {
	st, ffs, root := newFaultStore(t)
	id := "rec-1"
	cleanupRecord(t, st, id)

	ffs.inject(faultRemove, filepath.Join(id, "evidence", "detail-1.png"), errFaultRemove)
	if _, err := st.DeleteEvidence(id); !errors.Is(err, errFaultRemove) {
		t.Fatalf("DeleteEvidence = %v, want the remove failure", err)
	}
	ffs.clear()

	// The successful first cleanup left generations 2 and 3; damage the
	// newest one before the retry.
	newest := filepath.Join(root, "sessions", id, "checkpoints", "3.json")
	if err := os.WriteFile(newest, []byte("{ not a checkpoint"), 0o644); err != nil {
		t.Fatalf("damage newest generation: %v", err)
	}

	retry, err := st.DeleteEvidence(id)
	if err != nil {
		t.Fatalf("retry DeleteEvidence: %v", err)
	}
	if retry.Generation != 0 || retry.RemovedFiles != 1 {
		t.Errorf("retry = generation %d, %d removed; want 0 and 1", retry.Generation, retry.RemovedFiles)
	}
	if _, err := os.Stat(filepath.Join(root, "sessions", id, "checkpoints", "2.json")); err != nil {
		t.Fatalf("usable generation 2 disappeared during the retry: %v", err)
	}

	// A later save prunes with the invalidated verdict: the damaged 3
	// must go and the usable 2 must stay as the fallback.
	saveCheckpoint(t, st, id, mustLoad(t, st, id).Checkpoint)
	files := generationFiles(t, root, id)
	if !slices.Equal(files, []string{"2.json", "4.json"}) {
		t.Fatalf("generations after the next save = %v, want [2.json 4.json]", files)
	}
	if err := os.Remove(filepath.Join(root, "sessions", id, "checkpoints", "4.json")); err != nil {
		t.Fatalf("remove newest generation: %v", err)
	}
	if rec := mustLoad(t, st, id); rec.Checkpoint.SavedRevision != 2 {
		t.Errorf("fallback revision = %d, want 2", rec.Checkpoint.SavedRevision)
	}
}

// After the second generation write failed, a cold retry must not count an
// older empty generation as the fallback: the pair has to carry the
// current result, or damaging the newest file would restore an empty
// record and lose items, revisions, coverage and termination.
func TestDeleteEvidenceColdRetryKeepsCurrentResult(t *testing.T) {
	st, ffs, root := newFaultStore(t)
	id := "rec-1"
	if err := st.CreateRecord(id); err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}
	// Generation 1 is the scan-start checkpoint with no results.
	saveCheckpoint(t, st, id, Checkpoint{Session: testShell()})
	detail := writeEvidence(t, st, id, "detail-1", bytes.Repeat([]byte("d"), 120))
	coverage := writeEvidence(t, st, id, "coverage-end", bytes.Repeat([]byte("c"), 80))
	cp := cleanupResultCheckpoint(detail, coverage)
	saveCheckpoint(t, st, id, cp) // generation 2

	ffs.inject(faultCreate, "4.json", errFaultCreate)
	if _, err := st.DeleteEvidence(id); !errors.Is(err, errFaultCreate) {
		t.Fatalf("DeleteEvidence = %v, want the second generation write failure", err)
	}
	ffs.clear()
	if rec := mustLoad(t, st, id); rec.Checkpoint.SavedRevision != 3 {
		t.Fatalf("revision after the failed pair = %d, want the stripped 3", rec.Checkpoint.SavedRevision)
	}

	second, err := newStore(root, osFS{})
	if err != nil {
		t.Fatalf("newStore (cold restart): %v", err)
	}
	retry, err := second.DeleteEvidence(id)
	if err != nil {
		t.Fatalf("cold retry DeleteEvidence: %v", err)
	}
	if retry.Generation != 5 {
		t.Errorf("cold retry generation = %d, want 5", retry.Generation)
	}
	files := generationFiles(t, root, id)
	if !slices.Equal(files, []string{"4.json", "5.json"}) {
		t.Fatalf("generations after the cold retry = %v, want [4.json 5.json]", files)
	}
	if err := os.Remove(filepath.Join(root, "sessions", id, "checkpoints", "5.json")); err != nil {
		t.Fatalf("remove newest generation: %v", err)
	}
	got := mustLoad(t, second, id).Checkpoint
	if got.SavedRevision != 4 {
		t.Errorf("fallback revision = %d, want 4", got.SavedRevision)
	}
	if len(got.Items) != 1 {
		t.Fatalf("fallback items = %d, want the one current result", len(got.Items))
	}
	item := got.Items[0]
	if item.Revision != 2 || len(item.Revisions) != 2 || !sameJSON(t, item.Data, cp.Items[0].Data) {
		t.Errorf("fallback item = %+v, want the current result", item)
	}
	if got.Session.Coverage.Status != CoveragePartial || got.Session.Termination == nil {
		t.Errorf("fallback coverage/termination = %q/%+v, want partial and completed",
			got.Session.Coverage.Status, got.Session.Termination)
	}
}

// Adjacency and the absence of references do not prove that an older
// generation carries the current result. A pair whose previous state is an
// empty checkpoint must be replaced by two copies of the current result
// before the evidence is removed.
func TestDeleteEvidencePairCarriesCurrentResultNotEmptyPredecessor(t *testing.T) {
	st, root := newTestStore(t)
	id := "rec-1"
	if err := st.CreateRecord(id); err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}
	detail := writeEvidence(t, st, id, "detail-1", bytes.Repeat([]byte("d"), 120))
	coverage := writeEvidence(t, st, id, "coverage-end", bytes.Repeat([]byte("c"), 80))
	cp := cleanupResultCheckpoint(detail, coverage)
	saveCheckpoint(t, st, id, cp) // 1: the full result with evidence

	// Restart before each save so retention keeps the unverified older
	// generations instead of pruning them.
	second, err := newStore(root, osFS{})
	if err != nil {
		t.Fatalf("newStore: %v", err)
	}
	saveCheckpoint(t, second, id, Checkpoint{Session: testShell()}) // 2: no-reference empty state
	third, err := newStore(root, osFS{})
	if err != nil {
		t.Fatalf("newStore: %v", err)
	}
	stripped := cloneCheckpoint(&cp)
	stripped.Session.CoverageEvidenceIDs = nil
	stripped.Items[0].EvidenceIDs = nil
	stripped.Evidence = nil
	saveCheckpoint(t, third, id, *stripped) // 3: current result without references
	if files := generationFiles(t, root, id); !slices.Equal(files, []string{"1.json", "2.json", "3.json"}) {
		t.Fatalf("generations before cleanup = %v, want [1.json 2.json 3.json]", files)
	}

	removal, err := third.DeleteEvidence(id)
	if err != nil {
		t.Fatalf("DeleteEvidence: %v", err)
	}
	if removal.Generation != 5 || removal.RemovedFiles != 2 {
		t.Errorf("removal = generation %d, %d removed; want 5 and 2", removal.Generation, removal.RemovedFiles)
	}
	files := generationFiles(t, root, id)
	if !slices.Equal(files, []string{"4.json", "5.json"}) {
		t.Fatalf("generations after cleanup = %v, want [4.json 5.json]", files)
	}
	if err := os.Remove(filepath.Join(root, "sessions", id, "checkpoints", "5.json")); err != nil {
		t.Fatalf("remove newest generation: %v", err)
	}
	got := mustLoad(t, third, id).Checkpoint
	if got.SavedRevision != 4 || len(got.Evidence) != 0 {
		t.Errorf("fallback = revision %d, %d evidence; want 4 and 0", got.SavedRevision, len(got.Evidence))
	}
	if len(got.Items) != 1 || got.Items[0].Revision != 2 || !sameJSON(t, got.Items[0].Data, cp.Items[0].Data) {
		t.Errorf("fallback items = %+v, want the current result", got.Items)
	}
	if got.Session.Coverage.Status != CoveragePartial || got.Session.Termination == nil {
		t.Errorf("fallback coverage/termination = %q/%+v, want partial and completed",
			got.Session.Coverage.Status, got.Session.Termination)
	}
}

// A manual revision saved between two cleanup rounds belongs to the
// current result. The retry must persist that revision into both cleanup
// generations instead of trusting the adjacent previous revision.
func TestDeleteEvidenceRetryKeepsRevisionSavedBetweenRounds(t *testing.T) {
	st, ffs, root := newFaultStore(t)
	id := "rec-1"
	cleanupRecord(t, st, id) // generation 1, item revision 1, with evidence

	ffs.inject(faultCreate, "3.json", errFaultCreate)
	if _, err := st.DeleteEvidence(id); !errors.Is(err, errFaultCreate) {
		t.Fatalf("DeleteEvidence = %v, want the second copy failure", err)
	}
	ffs.clear()

	// Keep the evidence-citing generation on disk while a later save
	// prunes, so the next cleanup round has a live backup to protect.
	ffs.inject(faultRemove, "1.json", errFaultRemove)
	edited := []byte(`{"lines":[],"structured":{"setId":31100,"level":15}}`)
	_, gen, err := st.SaveRevision(id, 1, 1, ItemEdit{Data: edited})
	if err != nil {
		t.Fatalf("SaveRevision: %v", err)
	}
	if gen != 3 {
		t.Fatalf("revision generation = %d, want 3", gen)
	}
	ffs.clear()
	if files := generationFiles(t, root, id); !slices.Equal(files, []string{"1.json", "2.json", "3.json"}) {
		t.Fatalf("generations before the retry = %v, want [1.json 2.json 3.json]", files)
	}

	removal, err := st.DeleteEvidence(id)
	if err != nil {
		t.Fatalf("retry DeleteEvidence: %v", err)
	}
	if removal.Generation != 5 || removal.RemovedFiles != 2 {
		t.Errorf("retry = generation %d, %d removed; want 5 and 2", removal.Generation, removal.RemovedFiles)
	}
	files := generationFiles(t, root, id)
	if !slices.Equal(files, []string{"4.json", "5.json"}) {
		t.Fatalf("generations after the retry = %v, want [4.json 5.json]", files)
	}
	if err := os.Remove(filepath.Join(root, "sessions", id, "checkpoints", "5.json")); err != nil {
		t.Fatalf("remove newest generation: %v", err)
	}
	rec := mustLoad(t, st, id)
	item := rec.Checkpoint.Items[0]
	if item.Revision != 2 || len(item.Revisions) != 2 || !sameJSON(t, item.Data, edited) {
		t.Errorf("fallback item = revision %d, %d revisions, data %s; want revision 2 with %s",
			item.Revision, len(item.Revisions), item.Data, edited)
	}
}

// A generation that parses may be the survivor of a failed sync whose
// removal also failed. A retry must not treat it as a durable pair member:
// it writes two fresh copies, and a failure in that preparation leaves the
// evidence-citing generation and its files untouched for the next attempt.
func TestDeleteEvidenceRetriesUntilPairIsDurable(t *testing.T) {
	st, ffs, root := newFaultStore(t)
	id := "rec-1"
	cleanupRecord(t, st, id)

	// The first attempt's second generation is complete on disk but its
	// sync failed and its removal was refused: a parseable survivor.
	ffs.inject(faultSync, "3.json", errFaultSync)
	ffs.inject(faultRemove, "3.json", errFaultRemove)
	if _, err := st.DeleteEvidence(id); !errors.Is(err, errFaultSync) || !errors.Is(err, errFaultRemove) {
		t.Fatalf("DeleteEvidence = %v, want the sync and removal failures", err)
	}
	ffs.clear()
	if _, err := os.Stat(filepath.Join(root, "sessions", id, "checkpoints", "3.json")); err != nil {
		t.Fatalf("the unremoved generation is gone: %v", err)
	}

	// The next cold retry fails while writing the second fresh copy. It
	// must not prune the evidence-citing generation or remove a file.
	failing := &faultFS{inner: osFS{}}
	failing.inject(faultCreate, "5.json", errFaultCreate)
	second, err := newStore(root, failing)
	if err != nil {
		t.Fatalf("newStore: %v", err)
	}
	if _, err := second.DeleteEvidence(id); !errors.Is(err, errFaultCreate) {
		t.Fatalf("cold retry = %v, want the second copy write failure", err)
	}
	assertBackupPreserved(t, root, id, []string{"1.json", "2.json", "3.json", "4.json"})

	// A second consecutive failure, after another cold start, must not
	// skip the check or prune the backup either.
	failingAgain := &faultFS{inner: osFS{}}
	failingAgain.inject(faultCreate, "6.json", errFaultCreate)
	third, err := newStore(root, failingAgain)
	if err != nil {
		t.Fatalf("newStore: %v", err)
	}
	if _, err := third.DeleteEvidence(id); !errors.Is(err, errFaultCreate) {
		t.Fatalf("second cold retry = %v, want the second copy write failure", err)
	}
	assertBackupPreserved(t, root, id, []string{"1.json", "2.json", "3.json", "4.json", "5.json"})

	// A further cold retry persists two fresh copies, then prunes and
	// removes the evidence.
	fourth, err := newStore(root, osFS{})
	if err != nil {
		t.Fatalf("newStore: %v", err)
	}
	retry, err := fourth.DeleteEvidence(id)
	if err != nil {
		t.Fatalf("retry DeleteEvidence: %v", err)
	}
	if retry.Generation != 7 || retry.RemovedFiles != 2 {
		t.Errorf("retry = generation %d, %d removed; want 7 and 2", retry.Generation, retry.RemovedFiles)
	}
	files := generationFiles(t, root, id)
	if !slices.Equal(files, []string{"6.json", "7.json"}) {
		t.Fatalf("generations after the retry = %v, want [6.json 7.json]", files)
	}
	if err := os.Remove(filepath.Join(root, "sessions", id, "checkpoints", "7.json")); err != nil {
		t.Fatalf("remove newest generation: %v", err)
	}
	rec := mustLoad(t, fourth, id)
	if rec.Checkpoint.SavedRevision != 6 || len(rec.Checkpoint.Items) != 1 || len(rec.Checkpoint.Evidence) != 0 {
		t.Errorf("fallback = revision %d, %d items, %d evidence; want 6, 1 and 0",
			rec.Checkpoint.SavedRevision, len(rec.Checkpoint.Items), len(rec.Checkpoint.Evidence))
	}
}

// assertBackupPreserved checks that a failed cleanup preparation left the
// evidence-citing generation, its screenshots and the listed generations
// untouched.
func assertBackupPreserved(t *testing.T, root, id string, wantGenerations []string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(root, "sessions", id, "checkpoints", "1.json")); err != nil {
		t.Errorf("the evidence-citing generation was pruned during a failed preparation: %v", err)
	}
	for _, name := range []string{"detail-1.png", "coverage-end.png"} {
		if _, err := os.Stat(filepath.Join(root, "sessions", id, "evidence", name)); err != nil {
			t.Errorf("%s was removed during a failed preparation: %v", name, err)
		}
	}
	if files := generationFiles(t, root, id); !slices.Equal(files, wantGenerations) {
		t.Fatalf("generations after the failed preparation = %v, want %v", files, wantGenerations)
	}
}

// A failure writing the second reference-free generation leaves the
// screenshots in place. The retry adds the missing state before deleting
// them, so a transient failure cannot leave only one recoverable
// generation.
func TestDeleteEvidenceSecondGenerationFailureKeepsFilesAndRetries(t *testing.T) {
	st, ffs, root := newFaultStore(t)
	id := "rec-1"
	cleanupRecord(t, st, id)

	ffs.inject(faultCreate, "3.json", errFaultCreate)
	removal, err := st.DeleteEvidence(id)
	if !errors.Is(err, errFaultCreate) {
		t.Fatalf("DeleteEvidence = %v, want the second generation write failure", err)
	}
	if removal.Generation != 2 || removal.EvidenceFiles != 2 || removal.RemovedFiles != 0 {
		t.Errorf("removal = generation %d, %d measured, %d removed; want 2, 2 and 0",
			removal.Generation, removal.EvidenceFiles, removal.RemovedFiles)
	}
	for _, name := range []string{"detail-1.png", "coverage-end.png"} {
		if _, statErr := os.Stat(filepath.Join(root, "sessions", id, "evidence", name)); statErr != nil {
			t.Errorf("%s was deleted despite the failed pair: %v", name, statErr)
		}
	}
	// The first new generation is valid and references no screenshots.
	if rec := mustLoad(t, st, id); rec.Checkpoint.SavedRevision != 2 || len(rec.Checkpoint.Evidence) != 0 {
		t.Errorf("record after the failed pair = revision %d, %d evidence",
			rec.Checkpoint.SavedRevision, len(rec.Checkpoint.Evidence))
	}

	ffs.clear()
	retry, err := st.DeleteEvidence(id)
	if err != nil {
		t.Fatalf("retry DeleteEvidence: %v", err)
	}
	if retry.Generation != 4 {
		t.Errorf("retry generation = %d, want 4", retry.Generation)
	}
	if retry.RemovedFiles != 2 || len(retry.Leftovers) != 0 {
		t.Errorf("retry removed %d files, leftovers %v; want 2 and none", retry.RemovedFiles, retry.Leftovers)
	}
	files := generationFiles(t, root, id)
	if !slices.Equal(files, []string{"3.json", "4.json"}) {
		t.Fatalf("generations after retry = %v, want [3.json 4.json]", files)
	}
	if err := os.Remove(filepath.Join(root, "sessions", id, "checkpoints", "4.json")); err != nil {
		t.Fatalf("remove newest generation: %v", err)
	}
	if rec := mustLoad(t, st, id); rec.Checkpoint.SavedRevision != 3 {
		t.Errorf("fallback revision = %d, want 3", rec.Checkpoint.SavedRevision)
	}
}

// A record that never cited screenshots keeps its no-op cleanup: no
// checkpoint is written and no generation is returned.
func TestDeleteEvidenceWithoutReferencesDoesNotWrite(t *testing.T) {
	st, ffs, root := newFaultStore(t)
	id := "rec-1"
	if err := st.CreateRecord(id); err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}
	saveCheckpoint(t, st, id, Checkpoint{Session: testShell()})
	flushes := ffs.syncCount(".json")

	removal, err := st.DeleteEvidence(id)
	if err != nil {
		t.Fatalf("DeleteEvidence: %v", err)
	}
	if removal.Generation != 0 || removal.RemovedFiles != 0 {
		t.Errorf("removal = generation %d, %d removed; want 0 and 0", removal.Generation, removal.RemovedFiles)
	}
	if files := generationFiles(t, root, id); !slices.Equal(files, []string{"1.json"}) {
		t.Fatalf("generations after cleanup = %v, want [1.json]", files)
	}
	if got := ffs.syncCount(".json") - flushes; got != 0 {
		t.Errorf("generation flushes = %d, want none", got)
	}
}

// Orphan screenshots that no generation cites are removed without a
// checkpoint write: the record already holds all of its result state.
func TestDeleteEvidenceOrphanEvidenceDoesNotWrite(t *testing.T) {
	st, ffs, root := newFaultStore(t)
	id := "rec-1"
	if err := st.CreateRecord(id); err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}
	// Two reference-free generations: retention would normally keep both
	// as the pair, but no generation ever cited the orphan file.
	saveCheckpoint(t, st, id, Checkpoint{Session: testShell()})
	saveCheckpoint(t, st, id, Checkpoint{Session: testShell()})
	writeEvidence(t, st, id, "orphan-1", []byte("never cited"))
	flushes := ffs.syncCount(".json")

	removal, err := st.DeleteEvidence(id)
	if err != nil {
		t.Fatalf("DeleteEvidence: %v", err)
	}
	if removal.Generation != 0 || removal.RemovedFiles != 1 {
		t.Errorf("removal = generation %d, %d removed; want 0 and 1", removal.Generation, removal.RemovedFiles)
	}
	if files := generationFiles(t, root, id); !slices.Equal(files, []string{"1.json", "2.json"}) {
		t.Fatalf("generations after cleanup = %v, want [1.json 2.json]", files)
	}
	if got := ffs.syncCount(".json") - flushes; got != 0 {
		t.Errorf("generation flushes = %d, want none for an orphan-only cleanup", got)
	}
	if _, err := os.Stat(filepath.Join(root, "sessions", id, "evidence", "orphan-1.png")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("orphan screenshot survived (err %v)", err)
	}
}

// A record cleaned by the previous implementation has one usable
// reference-free generation and an older generation whose screenshots are
// already gone. The later cleanup keeps the established no-write
// behavior instead of manufacturing or bumping a result revision.
func TestDeleteEvidenceLegacyCleanupDoesNotWrite(t *testing.T) {
	st, root := newTestStore(t)
	id := "rec-1"
	cleanupRecord(t, st, id)
	cp := mustLoad(t, st, id).Checkpoint

	stripped := cloneCheckpoint(&cp)
	stripped.Session.CoverageEvidenceIDs = nil
	stripped.Items[0].EvidenceIDs = nil
	stripped.Evidence = nil
	saveCheckpoint(t, st, id, *stripped) // generation 2, as the old cleanup wrote
	if err := os.RemoveAll(filepath.Join(root, "sessions", id, "evidence")); err != nil {
		t.Fatalf("remove evidence: %v", err)
	}

	removal, err := st.DeleteEvidence(id)
	if err != nil {
		t.Fatalf("DeleteEvidence: %v", err)
	}
	if removal.Generation != 0 || removal.RemovedFiles != 0 {
		t.Errorf("removal = generation %d, %d removed; want 0 and 0", removal.Generation, removal.RemovedFiles)
	}
	if files := generationFiles(t, root, id); !slices.Equal(files, []string{"1.json", "2.json"}) {
		t.Fatalf("generations after cleanup = %v, want [1.json 2.json]", files)
	}
	if rec := mustLoad(t, st, id); rec.Checkpoint.SavedRevision != 2 {
		t.Errorf("loaded revision = %d, want 2", rec.Checkpoint.SavedRevision)
	}
}

// The current checkpoint is loaded and verified once. Cleanup must reuse
// that verified result instead of opening the same generation again: a
// transient error on a second open must not turn it into a false "no
// backup" and let the screenshots be deleted without a fallback.
func TestDeleteEvidenceReusesVerifiedCurrentCheckpoint(t *testing.T) {
	st, ffs, root := newFaultStore(t)
	id := "rec-1"
	cleanupRecord(t, st, id)

	// The first read of 1.json (the load) succeeds; any later read fails.
	// Reuse means there is no later read at all.
	ffs.injectAfter(faultOpen, "1.json", 1, errFaultOpen)
	removal, err := st.DeleteEvidence(id)
	if err != nil {
		t.Fatalf("DeleteEvidence: %v", err)
	}
	if removal.Generation != 3 || removal.RemovedFiles != 2 {
		t.Errorf("removal = generation %d, %d removed; want 3 and 2",
			removal.Generation, removal.RemovedFiles)
	}
	rec := mustLoad(t, st, id)
	if rec.Checkpoint.SavedRevision != 3 || len(rec.Checkpoint.Evidence) != 0 {
		t.Errorf("record = revision %d, %d evidence; want 3 and 0",
			rec.Checkpoint.SavedRevision, len(rec.Checkpoint.Evidence))
	}
	for _, name := range []string{"detail-1.png", "coverage-end.png"} {
		if _, err := os.Stat(filepath.Join(root, "sessions", id, "evidence", name)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s survived cleanup (err %v)", name, err)
		}
	}
}

// On a cold start the newest generation verifies, but an older backup can
// fail with an undecided I/O error. Cleanup must report it and keep the
// screenshots that backup depends on instead of trusting the failure as
// "not a backup".
func TestDeleteEvidenceColdUnreadableBackupKeepsEvidence(t *testing.T) {
	st, ffs, root := newFaultStore(t)
	id := "rec-1"
	cleanupRecord(t, st, id)

	// Leave generations [1 evidence, 2 stripped] with the screenshots
	// intact by failing the second pair copy.
	ffs.inject(faultCreate, "3.json", errFaultCreate)
	if _, err := st.DeleteEvidence(id); !errors.Is(err, errFaultCreate) {
		t.Fatalf("DeleteEvidence = %v, want the second copy failure", err)
	}
	ffs.clear()

	failing := &faultFS{inner: osFS{}}
	failing.inject(faultOpen, "1.json", errFaultOpen)
	second, err := newStore(root, failing)
	if err != nil {
		t.Fatalf("newStore: %v", err)
	}
	removal, err := second.DeleteEvidence(id)
	if !errors.Is(err, errFaultOpen) {
		t.Fatalf("DeleteEvidence = %v, want the undecided read failure", err)
	}
	if removal.Generation != 0 || removal.RemovedFiles != 0 {
		t.Errorf("removal = generation %d, %d removed; want 0 and 0",
			removal.Generation, removal.RemovedFiles)
	}
	for _, name := range []string{"detail-1.png", "coverage-end.png"} {
		if _, statErr := os.Stat(filepath.Join(root, "sessions", id, "evidence", name)); statErr != nil {
			t.Errorf("%s was deleted despite the undecided read error: %v", name, statErr)
		}
	}
	if files := generationFiles(t, root, id); !slices.Equal(files, []string{"1.json", "2.json"}) {
		t.Fatalf("generations after the refused cleanup = %v, want [1.json 2.json]", files)
	}
}

// A backup that cites only a protected symbolic link is not affected by
// deleting an unrelated orphan regular file. The deletion plan is the
// same list used for both the decision and the removal, so no checkpoint
// pair is written.
func TestDeleteEvidenceProtectedSymlinkDoesNotTriggerPair(t *testing.T) {
	st, root := newTestStore(t)
	id := "rec-1"
	if err := st.CreateRecord(id); err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}
	linked := writeEvidence(t, st, id, "linked-1", []byte("linked evidence"))
	evidencePath := filepath.Join(root, "sessions", id, "evidence", "linked-1.png")
	target := filepath.Join(t.TempDir(), "linked-target.png")
	if err := os.WriteFile(target, []byte("linked evidence"), 0o644); err != nil {
		t.Fatalf("write symlink target: %v", err)
	}
	if err := os.Remove(evidencePath); err != nil {
		t.Fatalf("remove regular evidence: %v", err)
	}
	if err := os.Symlink(target, evidencePath); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	saveCheckpoint(t, st, id, checkpointWithEvidence(linked))
	// The only removable entry is never cited.
	writeEvidence(t, st, id, "orphan-1", []byte("orphan"))

	removal, err := st.DeleteEvidence(id)
	if err != nil {
		t.Fatalf("DeleteEvidence: %v", err)
	}
	if removal.Generation != 0 || removal.EvidenceFiles != 1 || removal.RemovedFiles != 1 {
		t.Errorf("removal = generation %d, %d measured, %d removed; want 0, 1 and 1",
			removal.Generation, removal.EvidenceFiles, removal.RemovedFiles)
	}
	if files := generationFiles(t, root, id); !slices.Equal(files, []string{"1.json"}) {
		t.Fatalf("generations after cleanup = %v, want [1.json]", files)
	}
	if info, err := os.Lstat(evidencePath); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("protected evidence symlink changed: info %+v err %v", info, err)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "linked evidence" {
		t.Errorf("symlink target changed: data %q err %v", data, err)
	}
	if rec := mustLoad(t, st, id); rec.Checkpoint.SavedRevision != 1 || len(rec.Checkpoint.Evidence) != 1 {
		t.Errorf("record = revision %d, %d evidence; want 1 and 1",
			rec.Checkpoint.SavedRevision, len(rec.Checkpoint.Evidence))
	}
}

// A cited evidence path that is a symbolic link into the same evidence
// directory loses its target when the plan removes that target entry. The
// impact check resolves the path instead of comparing evidence ids, so the
// pair is persisted before the target disappears.
func TestDeleteEvidenceSymlinkTargetInEvidenceDirectory(t *testing.T) {
	st, root := newTestStore(t)
	id := "rec-1"
	if err := st.CreateRecord(id); err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}
	content := bytes.Repeat([]byte("t"), 64)
	writeEvidence(t, st, id, "different-name", content)
	cited := writeEvidence(t, st, id, "detail-1", content)
	evidenceDir := filepath.Join(root, "sessions", id, "evidence")
	if err := os.Remove(filepath.Join(evidenceDir, "detail-1.png")); err != nil {
		t.Fatalf("remove cited regular file: %v", err)
	}
	if err := os.Symlink("different-name.png", filepath.Join(evidenceDir, "detail-1.png")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	saveCheckpoint(t, st, id, checkpointWithEvidence(cited))

	removal, err := st.DeleteEvidence(id)
	if err != nil {
		t.Fatalf("DeleteEvidence: %v", err)
	}
	if removal.Generation != 3 || removal.EvidenceFiles != 1 || removal.RemovedFiles != 1 {
		t.Errorf("removal = generation %d, %d measured, %d removed; want 3, 1 and 1",
			removal.Generation, removal.EvidenceFiles, removal.RemovedFiles)
	}
	if files := generationFiles(t, root, id); !slices.Equal(files, []string{"2.json", "3.json"}) {
		t.Fatalf("generations after cleanup = %v, want [2.json 3.json]", files)
	}
	if _, err := os.Stat(filepath.Join(evidenceDir, "different-name.png")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("symlink target survived (err %v)", err)
	}
	if info, err := os.Lstat(filepath.Join(evidenceDir, "detail-1.png")); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("protected symlink changed: info %+v err %v", info, err)
	}
	if rec := mustLoad(t, st, id); rec.Checkpoint.SavedRevision != 3 || len(rec.Checkpoint.Evidence) != 0 {
		t.Errorf("record = revision %d, %d evidence; want 3 and 0",
			rec.Checkpoint.SavedRevision, len(rec.Checkpoint.Evidence))
	}
}

// On a case-insensitive volume the actual entry can differ in case from
// the cited evidence id. The cited path still resolves to the regular
// entry the plan removes, so the pair must be written first.
func TestDeleteEvidenceCaseInsensitiveEvidenceName(t *testing.T) {
	st, root := newTestStore(t)
	id := "rec-1"
	if err := st.CreateRecord(id); err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}
	content := []byte("case-insensitive evidence")
	ev := writeEvidence(t, st, id, "detail-1", content)
	evidenceDir := filepath.Join(root, "sessions", id, "evidence")
	lower := filepath.Join(evidenceDir, "detail-1.png")
	upper := filepath.Join(evidenceDir, "DETAIL-1.PNG")
	if err := os.Rename(lower, upper); err != nil {
		t.Skipf("case-only rename unavailable: %v", err)
	}
	if _, err := os.Stat(lower); err != nil {
		t.Skipf("filesystem is case-sensitive; the cited path no longer resolves: %v", err)
	}
	saveCheckpoint(t, st, id, checkpointWithEvidence(ev))

	removal, err := st.DeleteEvidence(id)
	if err != nil {
		t.Fatalf("DeleteEvidence: %v", err)
	}
	if removal.Generation != 3 || removal.EvidenceFiles != 1 || removal.RemovedFiles != 1 {
		t.Errorf("removal = generation %d, %d measured, %d removed; want 3, 1 and 1",
			removal.Generation, removal.EvidenceFiles, removal.RemovedFiles)
	}
	if files := generationFiles(t, root, id); !slices.Equal(files, []string{"2.json", "3.json"}) {
		t.Fatalf("generations after cleanup = %v, want [2.json 3.json]", files)
	}
	if _, err := os.Stat(upper); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("actual evidence entry survived (err %v)", err)
	}
	if rec := mustLoad(t, st, id); rec.Checkpoint.SavedRevision != 3 || len(rec.Checkpoint.Evidence) != 0 {
		t.Errorf("record = revision %d, %d evidence; want 3 and 0",
			rec.Checkpoint.SavedRevision, len(rec.Checkpoint.Evidence))
	}
}

// An extensionless orphan named like an evidence id is not that evidence
// file. The cited path is a protected symlink to an outside target, so
// cleanup must remove the orphan without trying to persist a pair. A
// write fault makes a false positive fail instead of silently succeeding.
func TestDeleteEvidenceExtensionlessOrphanDoesNotMatchEvidence(t *testing.T) {
	st, ffs, root := newFaultStore(t)
	id := "rec-1"
	if err := st.CreateRecord(id); err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}
	content := []byte("linked evidence")
	ev := writeEvidence(t, st, id, "detail-1", content)
	evidenceDir := filepath.Join(root, "sessions", id, "evidence")
	target := filepath.Join(t.TempDir(), "target.png")
	if err := os.WriteFile(target, content, 0o644); err != nil {
		t.Fatalf("write symlink target: %v", err)
	}
	if err := os.Remove(filepath.Join(evidenceDir, "detail-1.png")); err != nil {
		t.Fatalf("remove cited regular file: %v", err)
	}
	if err := os.Symlink(target, filepath.Join(evidenceDir, "detail-1.png")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	saveCheckpoint(t, st, id, checkpointWithEvidence(ev))
	if err := os.WriteFile(filepath.Join(evidenceDir, "detail-1"), []byte("orphan"), 0o644); err != nil {
		t.Fatalf("write extensionless orphan: %v", err)
	}

	ffs.inject(faultCreate, "2.json", errFaultCreate)
	removal, err := st.DeleteEvidence(id)
	if err != nil {
		t.Fatalf("DeleteEvidence = %v; a false pair write must not be attempted", err)
	}
	if removal.Generation != 0 || removal.EvidenceFiles != 1 || removal.RemovedFiles != 1 {
		t.Errorf("removal = generation %d, %d measured, %d removed; want 0, 1 and 1",
			removal.Generation, removal.EvidenceFiles, removal.RemovedFiles)
	}
	if files := generationFiles(t, root, id); !slices.Equal(files, []string{"1.json"}) {
		t.Fatalf("generations after cleanup = %v, want [1.json]", files)
	}
	if _, err := os.Stat(filepath.Join(evidenceDir, "detail-1")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("extensionless orphan survived (err %v)", err)
	}
	if data, err := os.ReadFile(target); err != nil || !bytes.Equal(data, content) {
		t.Errorf("symlink target changed: data %q err %v", data, err)
	}
	if rec := mustLoad(t, st, id); rec.Checkpoint.SavedRevision != 1 || len(rec.Checkpoint.Evidence) != 1 {
		t.Errorf("record = revision %d, %d evidence; want 1 and 1",
			rec.Checkpoint.SavedRevision, len(rec.Checkpoint.Evidence))
	}
}

// An external hard-link alias keeps the cited symlink readable even when
// another name for the same file inside the evidence directory is removed.
// Impact follows the resolved path, not the file identity alone.
func TestDeleteEvidenceExternalHardLinkAliasSurvives(t *testing.T) {
	st, root := newTestStore(t)
	id := "rec-1"
	if err := st.CreateRecord(id); err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}
	content := []byte("hard link evidence")
	ev := writeEvidence(t, st, id, "detail-1", content)
	evidenceDir := filepath.Join(root, "sessions", id, "evidence")
	external := filepath.Join(t.TempDir(), "external.png")
	if err := os.WriteFile(external, content, 0o644); err != nil {
		t.Fatalf("write external target: %v", err)
	}
	if err := os.Remove(filepath.Join(evidenceDir, "detail-1.png")); err != nil {
		t.Fatalf("remove cited regular file: %v", err)
	}
	if err := os.Symlink(external, filepath.Join(evidenceDir, "detail-1.png")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Link(external, filepath.Join(evidenceDir, "linked-inside.png")); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	saveCheckpoint(t, st, id, checkpointWithEvidence(ev))

	removal, err := st.DeleteEvidence(id)
	if err != nil {
		t.Fatalf("DeleteEvidence: %v", err)
	}
	if removal.Generation != 0 || removal.EvidenceFiles != 1 || removal.RemovedFiles != 1 {
		t.Errorf("removal = generation %d, %d measured, %d removed; want 0, 1 and 1",
			removal.Generation, removal.EvidenceFiles, removal.RemovedFiles)
	}
	if files := generationFiles(t, root, id); !slices.Equal(files, []string{"1.json"}) {
		t.Fatalf("generations after cleanup = %v, want [1.json]", files)
	}
	if _, err := os.Stat(filepath.Join(evidenceDir, "linked-inside.png")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("inside hard link survived (err %v)", err)
	}
	if data, err := os.ReadFile(external); err != nil || !bytes.Equal(data, content) {
		t.Errorf("external hard link changed: data %q err %v", data, err)
	}
	if rec := mustLoad(t, st, id); rec.Checkpoint.SavedRevision != 1 || len(rec.Checkpoint.Evidence) != 1 {
		t.Errorf("record = revision %d, %d evidence; want 1 and 1",
			rec.Checkpoint.SavedRevision, len(rec.Checkpoint.Evidence))
	}
}

// A transient read error on the full newest generation must abort the
// cleanup, not fall back to an older empty generation that a later read
// would then overwrite. After the fault clears the retry keeps the full
// result and both cleanup generations.
func TestDeleteEvidenceTransientReadErrorDoesNotFallBack(t *testing.T) {
	st, ffs, root := newFaultStore(t)
	id := "rec-1"
	if err := st.CreateRecord(id); err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}
	saveCheckpoint(t, st, id, Checkpoint{Session: testShell()}) // 1: empty
	detail := writeEvidence(t, st, id, "detail-1", bytes.Repeat([]byte("d"), 120))
	coverage := writeEvidence(t, st, id, "coverage-end", bytes.Repeat([]byte("c"), 80))
	cp := cleanupResultCheckpoint(detail, coverage)
	saveCheckpoint(t, st, id, cp) // 2: full result with evidence

	// The first open of the full generation fails; a second would work.
	ffs.injectFirst(faultOpen, "2.json", 1, errFaultOpen)
	removal, err := st.DeleteEvidence(id)
	if !errors.Is(err, errFaultOpen) {
		t.Fatalf("DeleteEvidence = %v, want the transient read failure", err)
	}
	if removal.Generation != 0 || removal.RemovedFiles != 0 {
		t.Errorf("removal = generation %d, %d removed; want 0 and 0",
			removal.Generation, removal.RemovedFiles)
	}
	if files := generationFiles(t, root, id); !slices.Equal(files, []string{"1.json", "2.json"}) {
		t.Fatalf("generations after the refused cleanup = %v, want [1.json 2.json]", files)
	}
	for _, name := range []string{"detail-1.png", "coverage-end.png"} {
		if _, err := os.Stat(filepath.Join(root, "sessions", id, "evidence", name)); err != nil {
			t.Errorf("%s was removed despite the read failure: %v", name, err)
		}
	}

	ffs.clear()
	before := mustLoad(t, st, id).Checkpoint
	if before.SavedRevision != 2 || len(before.Items) != 1 || before.Items[0].Revision != 2 {
		t.Fatalf("full checkpoint before retry = revision %d, %d items", before.SavedRevision, len(before.Items))
	}
	retry, err := st.DeleteEvidence(id)
	if err != nil {
		t.Fatalf("retry DeleteEvidence: %v", err)
	}
	if retry.Generation != 4 || retry.RemovedFiles != 2 {
		t.Errorf("retry = generation %d, %d removed; want 4 and 2", retry.Generation, retry.RemovedFiles)
	}
	if files := generationFiles(t, root, id); !slices.Equal(files, []string{"3.json", "4.json"}) {
		t.Fatalf("generations after the retry = %v, want [3.json 4.json]", files)
	}
	if err := os.Remove(filepath.Join(root, "sessions", id, "checkpoints", "4.json")); err != nil {
		t.Fatalf("remove newest generation: %v", err)
	}
	got := mustLoad(t, st, id).Checkpoint
	if got.SavedRevision != 3 || len(got.Items) != 1 || got.Items[0].Revision != 2 {
		t.Errorf("fallback = revision %d, %d items, item revision %d; want 3, 1 and 2",
			got.SavedRevision, len(got.Items), got.Items[0].Revision)
	}
	if !sameJSON(t, got.Items[0].Data, cp.Items[0].Data) {
		t.Errorf("fallback item data = %s, want %s", got.Items[0].Data, cp.Items[0].Data)
	}
	if got.Session.Coverage.Status != CoveragePartial || got.Session.Termination == nil {
		t.Errorf("fallback coverage/termination = %q/%+v, want partial and completed",
			got.Session.Coverage.Status, got.Session.Termination)
	}
}

// A generation citing an evidence path that is now a directory is
// definitively unusable. Cleanup must skip it, delete the unrelated orphan
// screenshot and keep the directory without writing a checkpoint.
func TestDeleteEvidenceDirectoryEvidenceKeepsOrphanRemoval(t *testing.T) {
	st, root := newTestStore(t)
	id := "rec-1"
	if err := st.CreateRecord(id); err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}
	ev := writeEvidence(t, st, id, "detail-1", []byte("replaced by a directory"))
	saveCheckpoint(t, st, id, checkpointWithEvidence(ev)) // 1 cites detail-1
	evidenceDir := filepath.Join(root, "sessions", id, "evidence")
	evidencePath := filepath.Join(evidenceDir, "detail-1.png")
	if err := os.Remove(evidencePath); err != nil {
		t.Fatalf("remove evidence file: %v", err)
	}
	if err := os.Mkdir(evidencePath, 0o755); err != nil {
		t.Fatalf("replace evidence with directory: %v", err)
	}
	// The newest checkpoint has no screenshot references.
	saveCheckpoint(t, st, id, Checkpoint{Session: testShell()}) // 2
	writeEvidence(t, st, id, "orphan-1", []byte("orphan"))      // removable, cited by nobody

	removal, err := st.DeleteEvidence(id)
	if err != nil {
		t.Fatalf("DeleteEvidence: %v", err)
	}
	if removal.Generation != 0 || removal.EvidenceFiles != 1 || removal.RemovedFiles != 1 {
		t.Errorf("removal = generation %d, %d measured, %d removed; want 0, 1 and 1",
			removal.Generation, removal.EvidenceFiles, removal.RemovedFiles)
	}
	if files := generationFiles(t, root, id); !slices.Equal(files, []string{"1.json", "2.json"}) {
		t.Fatalf("generations after cleanup = %v, want [1.json 2.json]", files)
	}
	if info, err := os.Lstat(evidencePath); err != nil || !info.IsDir() {
		t.Errorf("protected directory changed: info %+v err %v", info, err)
	}
	if _, err := os.Stat(filepath.Join(evidenceDir, "orphan-1.png")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("orphan screenshot survived (err %v)", err)
	}
	if rec := mustLoad(t, st, id); rec.Checkpoint.SavedRevision != 2 || len(rec.Checkpoint.Evidence) != 0 {
		t.Errorf("record = revision %d, %d evidence; want 2 and 0",
			rec.Checkpoint.SavedRevision, len(rec.Checkpoint.Evidence))
	}
}

// A generation citing a self-referential evidence symlink is definitively
// unusable as well. The loop is detected by walking links, not by one
// platform's error value, so the orphan screenshot is still removed.
func TestDeleteEvidenceSymlinkLoopKeepsOrphanRemoval(t *testing.T) {
	st, root := newTestStore(t)
	id := "rec-1"
	if err := st.CreateRecord(id); err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}
	ev := writeEvidence(t, st, id, "detail-1", []byte("replaced by a loop"))
	saveCheckpoint(t, st, id, checkpointWithEvidence(ev)) // 1 cites detail-1
	evidenceDir := filepath.Join(root, "sessions", id, "evidence")
	evidencePath := filepath.Join(evidenceDir, "detail-1.png")
	if err := os.Remove(evidencePath); err != nil {
		t.Fatalf("remove evidence file: %v", err)
	}
	if err := os.Symlink("detail-1.png", evidencePath); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	// The newest checkpoint has no screenshot references.
	saveCheckpoint(t, st, id, Checkpoint{Session: testShell()}) // 2
	writeEvidence(t, st, id, "orphan-1", []byte("orphan"))      // removable, cited by nobody

	removal, err := st.DeleteEvidence(id)
	if err != nil {
		t.Fatalf("DeleteEvidence: %v", err)
	}
	if removal.Generation != 0 || removal.EvidenceFiles != 1 || removal.RemovedFiles != 1 {
		t.Errorf("removal = generation %d, %d measured, %d removed; want 0, 1 and 1",
			removal.Generation, removal.EvidenceFiles, removal.RemovedFiles)
	}
	if files := generationFiles(t, root, id); !slices.Equal(files, []string{"1.json", "2.json"}) {
		t.Fatalf("generations after cleanup = %v, want [1.json 2.json]", files)
	}
	if info, err := os.Lstat(evidencePath); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("protected symlink changed: info %+v err %v", info, err)
	}
	if _, err := os.Stat(filepath.Join(evidenceDir, "orphan-1.png")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("orphan screenshot survived (err %v)", err)
	}
	if rec := mustLoad(t, st, id); rec.Checkpoint.SavedRevision != 2 || len(rec.Checkpoint.Evidence) != 0 {
		t.Errorf("record = revision %d, %d evidence; want 2 and 0",
			rec.Checkpoint.SavedRevision, len(rec.Checkpoint.Evidence))
	}
}

// verifyHopFixture proves that path reaches want through the platform's
// native path resolution. Windows cannot open this POSIX symlink-then-..
// shape, so skip only that specific fixture there; a missing link, wrong
// bytes or any error other than Windows' file-not-found still fails.
func verifyHopFixture(t *testing.T, path string, want []byte) {
	t.Helper()
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("fixture link %s: %v", path, err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		if runtime.GOOS == "windows" && errors.Is(err, fs.ErrNotExist) {
			t.Skipf("fixture link %s requires symlink-then-.. resolution, which Windows cannot open: %v", path, err)
		}
		t.Fatalf("fixture link %s is unreadable: %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("fixture link %s = %q, want %q", path, got, want)
	}
}

// A link target such as holder/hop/../target.png must resolve exactly as
// the system resolves it: hop is followed first, so the parent is the
// evidence directory, not holder. Lexical cleaning would look for
// holder/target.png. The test exercises a relative and an absolute link
// with the real filesystem and walks the full cleanup sequence: load the
// newest result, persist the pair before removal, remove the evidence,
// then fall back after damaging the newest copy.
func TestDeleteEvidenceSymlinkHopParentResolution(t *testing.T) {
	cases := []struct {
		name     string
		absolute bool
	}{
		{name: "relative"},
		{name: "absolute", absolute: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st, ffs, root := newFaultStore(t)
			id := "rec-1"
			if err := st.CreateRecord(id); err != nil {
				t.Fatalf("CreateRecord: %v", err)
			}
			saveCheckpoint(t, st, id, Checkpoint{Session: testShell()}) // 1: empty

			evidenceDir := filepath.Join(root, "sessions", id, "evidence")
			if err := os.MkdirAll(filepath.Join(evidenceDir, "holder"), 0o755); err != nil {
				t.Fatalf("create holder: %v", err)
			}
			if err := os.MkdirAll(filepath.Join(evidenceDir, "sub"), 0o755); err != nil {
				t.Fatalf("create sub: %v", err)
			}
			content := []byte("hop parent resolution evidence")
			targetPath := filepath.Join(evidenceDir, "target.png")
			if err := os.WriteFile(targetPath, content, 0o644); err != nil {
				t.Fatalf("write target: %v", err)
			}
			if err := os.Symlink("../sub", filepath.Join(evidenceDir, "holder", "hop")); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			linkTarget := "holder/hop/../target.png"
			if tc.absolute {
				// Keep "hop/.." in the string; filepath.Join would clean it.
				linkTarget = filepath.FromSlash(evidenceDir + "/holder/hop/../target.png")
			}
			if err := os.Symlink(linkTarget, filepath.Join(evidenceDir, "detail-1.png")); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			verifyHopFixture(t, filepath.Join(evidenceDir, "detail-1.png"), content)

			sum := sha256.Sum256(content)
			ev := EvidenceFile{
				ID:         "detail-1",
				ByteLength: int64(len(content)),
				SHA256:     hex.EncodeToString(sum[:]),
			}
			cp := revisedEvidenceCheckpoint(ev)
			saveCheckpoint(t, st, id, cp) // 2: full result

			// Normal loading must already read through the system's
			// resolution and keep the full newest result.
			before := mustLoad(t, st, id).Checkpoint
			if before.SavedRevision != 2 || len(before.Items) != 1 || before.Items[0].Revision != 2 {
				t.Fatalf("load = revision %d, %d items; want 2 and the revised item",
					before.SavedRevision, len(before.Items))
			}

			// Fail the actual removal: the pair must already be persisted
			// while target.png is still present.
			ffs.inject(faultRemove, "target.png", errFaultRemove)
			if _, err := st.DeleteEvidence(id); !errors.Is(err, errFaultRemove) {
				t.Fatalf("DeleteEvidence = %v, want the removal failure", err)
			}
			ffs.clear()
			if _, err := os.Stat(targetPath); err != nil {
				t.Fatalf("target removed before the pair was confirmed: %v", err)
			}
			if files := generationFiles(t, root, id); !slices.Equal(files, []string{"3.json", "4.json"}) {
				t.Fatalf("generations before removal = %v, want [3.json 4.json]", files)
			}
			mid := mustLoad(t, st, id).Checkpoint
			if mid.SavedRevision != 4 || len(mid.Items) != 1 || mid.Items[0].Revision != 2 {
				t.Fatalf("pair = revision %d, %d items; want 4 and the revised item",
					mid.SavedRevision, len(mid.Items))
			}

			// The retry removes the evidence and keeps both full results.
			retry, err := st.DeleteEvidence(id)
			if err != nil {
				t.Fatalf("retry DeleteEvidence: %v", err)
			}
			if retry.Generation != 0 || retry.RemovedFiles != 1 {
				t.Errorf("retry = generation %d, %d removed; want 0 and 1",
					retry.Generation, retry.RemovedFiles)
			}
			if _, err := os.Stat(targetPath); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("target survived the retry (err %v)", err)
			}
			after := mustLoad(t, st, id).Checkpoint
			if after.SavedRevision != 4 || len(after.Items) != 1 || after.Items[0].Revision != 2 {
				t.Fatalf("after removal = revision %d, %d items; want 4 and the revised item",
					after.SavedRevision, len(after.Items))
			}

			// Damage the newest copy: the other copy carries the same full
			// result.
			if err := os.Remove(filepath.Join(root, "sessions", id, "checkpoints", "4.json")); err != nil {
				t.Fatalf("remove newest generation: %v", err)
			}
			got := mustLoad(t, st, id).Checkpoint
			if got.SavedRevision != 3 || len(got.Items) != 1 || got.Items[0].Revision != 2 {
				t.Fatalf("fallback = revision %d, %d items, item revision %d; want 3, 1 and 2",
					got.SavedRevision, len(got.Items), got.Items[0].Revision)
			}
			if !sameJSON(t, got.Items[0].Data, cp.Items[0].Data) {
				t.Errorf("fallback data = %s, want %s", got.Items[0].Data, cp.Items[0].Data)
			}
			if got.Session.Coverage.Status != CoveragePartial || got.Session.Termination == nil {
				t.Errorf("fallback coverage/termination = %q/%+v, want partial and completed",
					got.Session.Coverage.Status, got.Session.Termination)
			}
		})
	}
}

// The removal-impact check runs only for evidence of an already verified
// checkpoint. A missing entry, missing link target, missing parent or
// directory, or any other failure to confirm that identity is undecided:
// the cleanup must stop before writing, pruning or deleting, and a retry
// after the transient fault clears must complete.
func TestDeleteEvidenceUnresolvedRemovalImpactStopsCleanup(t *testing.T) {
	cases := []struct {
		name   string
		inject func(*faultFS, string)
	}{
		{"lstat missing", func(f *faultFS, _ string) {
			f.injectFirst(faultLstat, "detail-1.png", 1, fs.ErrNotExist)
		}},
		{"eval symlinks missing", func(f *faultFS, _ string) {
			f.injectFirst(faultEvalSymlinks, "detail-1.png", 1, fs.ErrNotExist)
		}},
		{"resolved target missing", func(f *faultFS, _ string) {
			f.injectFirst(faultStat, "target.png", 1, fs.ErrNotExist)
		}},
		{"parent stat missing", func(f *faultFS, id string) {
			f.injectFirst(faultStat, filepath.Join(id, "evidence"), 1, fs.ErrNotExist)
		}},
		{"evidence dir stat missing", func(f *faultFS, id string) {
			f.injectAfter(faultStat, filepath.Join(id, "evidence"), 1, fs.ErrNotExist)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st, ffs, root := newFaultStore(t)
			id := "rec-1"
			if err := st.CreateRecord(id); err != nil {
				t.Fatalf("CreateRecord: %v", err)
			}
			saveCheckpoint(t, st, id, Checkpoint{Session: testShell()}) // 1: empty
			evidenceDir := filepath.Join(root, "sessions", id, "evidence")
			content := []byte("unresolved impact evidence")
			targetPath := filepath.Join(evidenceDir, "target.png")
			if err := os.WriteFile(targetPath, content, 0o644); err != nil {
				t.Fatalf("write target: %v", err)
			}
			citedPath := filepath.Join(evidenceDir, "detail-1.png")
			if err := os.Symlink("target.png", citedPath); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			sum := sha256.Sum256(content)
			ev := EvidenceFile{
				ID:         "detail-1",
				ByteLength: int64(len(content)),
				SHA256:     hex.EncodeToString(sum[:]),
			}
			cp := revisedEvidenceCheckpoint(ev)
			saveCheckpoint(t, st, id, cp) // 2: full result
			if rec := mustLoad(t, st, id); rec.Checkpoint.SavedRevision != 2 {
				t.Fatalf("baseline load revision = %d, want 2", rec.Checkpoint.SavedRevision)
			}

			tc.inject(ffs, id)
			removal, err := st.DeleteEvidence(id)
			if !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("DeleteEvidence = %v, want the injected missing-path error", err)
			}
			if !strings.Contains(err.Error(), `evidence "detail-1"`) {
				t.Errorf("diagnostic = %v, want it to name the evidence id", err)
			}
			if removal.Generation != 0 || removal.EvidenceFiles != 1 || removal.RemovedFiles != 0 {
				t.Errorf("removal = generation %d, %d measured, %d removed; want 0, 1 and 0",
					removal.Generation, removal.EvidenceFiles, removal.RemovedFiles)
			}
			if files := generationFiles(t, root, id); !slices.Equal(files, []string{"1.json", "2.json"}) {
				t.Fatalf("generations after the stopped cleanup = %v, want [1.json 2.json]", files)
			}
			if _, err := os.Stat(targetPath); err != nil {
				t.Errorf("target was removed despite the undecided impact: %v", err)
			}
			if info, err := os.Lstat(citedPath); err != nil || info.Mode()&os.ModeSymlink == 0 {
				t.Errorf("protected symlink changed: info %+v err %v", info, err)
			}
			if rec := mustLoad(t, st, id); rec.Checkpoint.SavedRevision != 2 || rec.Checkpoint.Items[0].Revision != 2 {
				t.Errorf("record after the stopped cleanup = revision %d, item revision %d; want 2 and 2",
					rec.Checkpoint.SavedRevision, rec.Checkpoint.Items[0].Revision)
			}

			ffs.clear()
			retry, err := st.DeleteEvidence(id)
			if err != nil {
				t.Fatalf("retry DeleteEvidence: %v", err)
			}
			if retry.Generation != 4 || retry.RemovedFiles != 1 {
				t.Errorf("retry = generation %d, %d removed; want 4 and 1",
					retry.Generation, retry.RemovedFiles)
			}
			if _, err := os.Stat(targetPath); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("target survived the retry (err %v)", err)
			}
			if files := generationFiles(t, root, id); !slices.Equal(files, []string{"3.json", "4.json"}) {
				t.Fatalf("generations after the retry = %v, want [3.json 4.json]", files)
			}
			if err := os.Remove(filepath.Join(root, "sessions", id, "checkpoints", "4.json")); err != nil {
				t.Fatalf("remove newest generation: %v", err)
			}
			got := mustLoad(t, st, id).Checkpoint
			if got.SavedRevision != 3 || got.Items[0].Revision != 2 || got.Session.Termination == nil {
				t.Errorf("fallback = revision %d, item revision %d, termination %+v; want 3, 2 and completed",
					got.SavedRevision, got.Items[0].Revision, got.Session.Termination)
			}
		})
	}
}

// A later read that sees a different file type than the verified regular
// file is a contradiction, not proof that the removal is harmless. Both
// the cited entry and a link target are checked.
func TestEvidenceRemovalBreaksPathRejectsChangedType(t *testing.T) {
	st, root := newTestStore(t)
	id := "rec-1"
	if err := st.CreateRecord(id); err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}
	evidenceDir := filepath.Join(root, "sessions", id, "evidence")
	targetPath := filepath.Join(evidenceDir, "target.png")
	if err := os.WriteFile(targetPath, []byte("type evidence"), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}
	citedPath := filepath.Join(evidenceDir, "detail-1.png")
	if err := os.Symlink("target.png", citedPath); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	st.mu.Lock()
	breaks, err := st.evidenceRemovalBreaksPathLocked(id, "detail-1")
	st.mu.Unlock()
	if err != nil || !breaks {
		t.Fatalf("baseline = affected %v, err %v; want affected", breaks, err)
	}

	if err := os.Remove(targetPath); err != nil {
		t.Fatalf("remove target: %v", err)
	}
	if err := os.Mkdir(targetPath, 0o755); err != nil {
		t.Fatalf("replace target with directory: %v", err)
	}
	st.mu.Lock()
	_, err = st.evidenceRemovalBreaksPathLocked(id, "detail-1")
	st.mu.Unlock()
	if err == nil {
		t.Fatal("directory target accepted, want an undecided type error")
	}

	if err := os.RemoveAll(targetPath); err != nil {
		t.Fatalf("remove target directory: %v", err)
	}
	if err := os.Remove(citedPath); err != nil {
		t.Fatalf("remove cited symlink: %v", err)
	}
	if err := os.Mkdir(citedPath, 0o755); err != nil {
		t.Fatalf("replace cited path with directory: %v", err)
	}
	st.mu.Lock()
	_, err = st.evidenceRemovalBreaksPathLocked(id, "detail-1")
	st.mu.Unlock()
	if err == nil {
		t.Fatal("directory evidence path accepted, want an undecided type error")
	}
}

// sameJSON compares two JSON payloads independent of the indentation the
// checkpoint writer applies to embedded raw messages.
func sameJSON(t *testing.T, got, want []byte) bool {
	t.Helper()
	var compactGot, compactWant bytes.Buffer
	if err := json.Compact(&compactGot, got); err != nil {
		t.Fatalf("compact payload %s: %v", got, err)
	}
	if err := json.Compact(&compactWant, want); err != nil {
		t.Fatalf("compact expected payload %s: %v", want, err)
	}
	return bytes.Equal(compactGot.Bytes(), compactWant.Bytes())
}
