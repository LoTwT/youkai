package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/record"
	"github.com/LoTwT/youkai/internal/scan"
)

// cleanupFixture creates one saved record with a review-pending item.
// With evidence it writes one item crop and one coverage image and culls
// references to them; without, the record stays an unsaved directory.
func cleanupFixture(t *testing.T, store *record.Store, id string, withEvidence bool) {
	t.Helper()
	if err := store.CreateRecord(id); err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}
	if !withEvidence {
		return
	}
	detail, err := store.WriteEvidence(id, "detail-1", []byte("detail evidence"))
	if err != nil {
		t.Fatalf("WriteEvidence detail: %v", err)
	}
	coverage, err := store.WriteEvidence(id, "coverage-end", []byte("coverage evidence"))
	if err != nil {
		t.Fatalf("WriteEvidence coverage: %v", err)
	}
	item := reviewItem(t, 1, reviewData())
	item.EvidenceIDs = []string{"detail-1"}
	cp := record.Checkpoint{
		Session: record.SessionShell{
			SessionID:           id,
			Game:                "zzz",
			Entity:              "driveDisc",
			Client:              record.ClientWindowsLocal,
			Locale:              "zh-CN",
			Scope:               json.RawMessage(`{"rarity":"S"}`),
			StartedAt:           time.Now().UTC(),
			Coverage:            record.Coverage{Status: record.CoveragePartial},
			CapturedCount:       1,
			CoverageEvidenceIDs: []string{"coverage-end"},
		},
		Items:    []record.ItemRecord{item},
		Evidence: []record.EvidenceFile{detail, coverage},
	}
	if _, err := store.SaveCheckpoint(id, cp); err != nil {
		t.Fatalf("SaveCheckpoint: %v", err)
	}
}

func cleanupStore(t *testing.T) (*record.Store, Paths) {
	t.Helper()
	paths := newTestPaths(t)
	store, err := record.Open(paths.Data)
	if err != nil {
		t.Fatalf("record.Open: %v", err)
	}
	return store, paths
}

func TestServiceDeleteScreenshotsKeepsRecordData(t *testing.T) {
	store, paths := cleanupStore(t)
	const id = "cleanup-1"
	cleanupFixture(t, store, id, true)
	svc := newServiceWithStore(t, paths, store, nil, func() (Recognizer, error) { return newFakeRecognizer(), nil })

	preview, err := svc.PreviewRecordScreenshots(id)
	if err != nil {
		t.Fatalf("PreviewRecordScreenshots: %v", err)
	}
	if preview.Records != 1 || preview.EvidenceFiles != 2 || preview.EvidenceBytes == 0 {
		t.Errorf("preview = %+v, want one record with two measured screenshots", preview)
	}
	if preview.PendingReview != 1 {
		t.Errorf("pending review = %d, want 1", preview.PendingReview)
	}

	before := svc.Snapshot().Revision
	removal, err := svc.DeleteRecordScreenshots(id)
	if err != nil {
		t.Fatalf("DeleteRecordScreenshots: %v", err)
	}
	if removal.Generation != 2 || removal.RemovedFiles != 2 {
		t.Errorf("removal = %+v, want generation 2 and two files", removal)
	}
	if got := svc.Snapshot().Revision; got <= before {
		t.Errorf("snapshot revision after cleanup = %d, want above %d", got, before)
	}

	rec, err := svc.LoadRecord(id)
	if err != nil {
		t.Fatalf("LoadRecord after cleanup: %v", err)
	}
	if len(rec.Checkpoint.Evidence) != 0 || len(rec.Checkpoint.Session.CoverageEvidenceIDs) != 0 {
		t.Errorf("evidence references survived: %+v / %v",
			rec.Checkpoint.Evidence, rec.Checkpoint.Session.CoverageEvidenceIDs)
	}
	item := rec.Checkpoint.Items[0]
	if len(item.EvidenceIDs) != 0 {
		t.Errorf("item evidence = %v, want none", item.EvidenceIDs)
	}
	if item.Quality != record.QualityNeedsReview || item.Revision != 0 || len(item.Data) == 0 {
		t.Errorf("item data/quality/revision changed: %+v", item)
	}
}

func TestServiceDeleteRecordAndAllRecords(t *testing.T) {
	store, paths := cleanupStore(t)
	cleanupFixture(t, store, "cleanup-1", true)
	cleanupFixture(t, store, "cleanup-2", true)
	svc := newServiceWithStore(t, paths, store, nil, func() (Recognizer, error) { return newFakeRecognizer(), nil })

	preview, err := svc.PreviewAllRecords()
	if err != nil {
		t.Fatalf("PreviewAllRecords: %v", err)
	}
	if preview.Records != 2 || preview.PendingReview != 2 || preview.EvidenceFiles != 4 {
		t.Errorf("preview = %+v, want two records, two pending and four screenshots", preview)
	}

	if _, err := svc.DeleteRecord("cleanup-1"); err != nil {
		t.Fatalf("DeleteRecord: %v", err)
	}
	infos, err := svc.ListRecords()
	if err != nil || len(infos) != 1 || infos[0].ID != "cleanup-2" {
		t.Fatalf("records after single delete = %+v err %v", infos, err)
	}
	if _, err := svc.LoadRecord("cleanup-1"); !errors.Is(err, record.ErrRecordNotFound) {
		t.Errorf("LoadRecord(deleted) = %v, want ErrRecordNotFound", err)
	}

	batch, err := svc.DeleteAllRecords()
	if err != nil {
		t.Fatalf("DeleteAllRecords: %v", err)
	}
	if batch.Records != 1 || len(batch.Failed) != 0 {
		t.Errorf("batch = %+v, want one deleted", batch)
	}
	if infos, err := svc.ListRecords(); err != nil || len(infos) != 0 {
		t.Errorf("records after delete-all = %+v err %v", infos, err)
	}
}

func TestServiceCleanupBlockedWhileScanRuns(t *testing.T) {
	store, paths := cleanupStore(t)
	cleanupFixture(t, store, "cleanup-1", true)
	gate := newGatedController()
	svc := newServiceWithStore(t, paths, store, staticSource{spec: specOf(gate)},
		func() (Recognizer, error) { return newFakeRecognizer(), nil })

	if _, err := svc.Start(StartRequest{ID: "cleanup-busy"}); err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := svc.DeleteRecord("cleanup-1"); !errors.Is(err, ErrCleanupBlocked) {
		t.Fatalf("DeleteRecord during scan = %v, want ErrCleanupBlocked", err)
	}
	if _, err := svc.DeleteAllScreenshots(); !errors.Is(err, ErrCleanupBlocked) {
		t.Fatalf("DeleteAllScreenshots during scan = %v, want ErrCleanupBlocked", err)
	}
	if _, err := svc.DeleteAllRecords(); !errors.Is(err, ErrCleanupBlocked) {
		t.Fatalf("DeleteAllRecords during scan = %v, want ErrCleanupBlocked", err)
	}

	gate.release()
	waitNotBusy(t, svc)
	if _, err := svc.DeleteRecord("cleanup-1"); err != nil {
		t.Fatalf("DeleteRecord after the session wrapped: %v", err)
	}
}

func TestServiceCleanupBlockedWhileFinalSaveFails(t *testing.T) {
	paths := newTestPaths(t)
	store := newFlakyStore(t, paths.Data)
	cleanupFixture(t, store.Store, "cleanup-1", true)
	source := devFixtureSource(t, 2*checkpointBatchSize, time.Millisecond)
	store.failOn = func(call int) bool { return call >= 1 }
	svc := newServiceWithStore(t, paths, store, source,
		func() (Recognizer, error) { return newFakeRecognizer(), nil })

	if _, err := svc.Start(StartRequest{ID: "cleanup-save-failure"}); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitFor(t, 30*time.Second, "the ended session with its failed save", func() bool {
		snap := svc.Snapshot()
		return snap.Scan.Phase == scan.PhaseEnded && snap.Busy &&
			snap.SavePending && snap.SaveError != ""
	})
	if _, err := svc.DeleteRecordScreenshots("cleanup-1"); !errors.Is(err, ErrCleanupBlocked) {
		t.Fatalf("DeleteRecordScreenshots with a failed save = %v, want ErrCleanupBlocked", err)
	}
	store.heal()
	if err := svc.RetrySave(); err != nil {
		t.Fatalf("RetrySave: %v", err)
	}
	waitNotBusy(t, svc)
	if _, err := svc.DeleteRecordScreenshots("cleanup-1"); err != nil {
		t.Fatalf("DeleteRecordScreenshots after the retry: %v", err)
	}
}

// TestPendingReviewCountBackfillsLegacyLines mirrors the results view:
// a legacy produced item with only raw drive-disc lines is still a row the
// user will see as pending after the in-memory backfill, so cleanup
// previews must not report zero.
func TestPendingReviewCountBackfillsLegacyLines(t *testing.T) {
	legacy := record.ItemRecord{
		Status:  record.StatusProduced,
		Quality: record.QualityNotEvaluated,
		Data: json.RawMessage(`{"lines":[
			{"role":"name","text":"河豚电音[1]"},
			{"role":"level","text":"等级15/15"},
			{"role":"main","text":"生命值 2200"},
			{"role":"sub1","text":"暴击伤害 +3 19.2%"},
			{"role":"sub2","text":"穿透值 9"},
			{"role":"sub3","text":"攻击力 +1 6%"}
		]}`),
		Observed: json.RawMessage(`{"capture":{"quality":"S"}}`),
	}
	if got := pendingReviewCount([]record.ItemRecord{legacy}); got != 1 {
		t.Fatalf("legacy pending count = %d, want 1", got)
	}
	empty := legacy
	empty.Data = json.RawMessage(`{"lines":[{"role":"qualityBadge","text":"S"}]}`)
	if got := pendingReviewCount([]record.ItemRecord{empty}); got != 0 {
		t.Fatalf("non-disc pending count = %d, want 0", got)
	}
}

func TestServiceCleanupPreviewsUnreadableRecord(t *testing.T) {
	store, paths := cleanupStore(t)
	cleanupFixture(t, store, "cleanup-1", true)
	if err := store.CreateRecord("cleanup-2"); err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}
	svc := newServiceWithStore(t, paths, store, nil, func() (Recognizer, error) { return newFakeRecognizer(), nil })

	preview, err := svc.PreviewAllRecords()
	if err != nil {
		t.Fatalf("PreviewAllRecords: %v", err)
	}
	if preview.Records != 2 || preview.Unreadable != 1 || preview.PendingReview != 1 {
		t.Errorf("preview = %+v, want 2 records, 1 unreadable, 1 pending", preview)
	}
	one, err := svc.PreviewRecordDeletion("cleanup-2")
	if err != nil {
		t.Fatalf("PreviewRecordDeletion(unreadable): %v", err)
	}
	if one.Records != 1 || one.Unreadable != 1 {
		t.Errorf("unreadable preview = %+v", one)
	}
	if _, err := svc.PreviewRecordScreenshots("cleanup-2"); !errors.Is(err, record.ErrNoValidCheckpoint) {
		t.Errorf("PreviewRecordScreenshots(unreadable) = %v, want ErrNoValidCheckpoint", err)
	}
}

func TestServiceStorageUsageMeasuresManagedRoots(t *testing.T) {
	store, paths := cleanupStore(t)
	cleanupFixture(t, store, "cleanup-1", true)
	if err := os.MkdirAll(paths.Cache, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.Cache, "thumb.bin"), []byte("12345"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(paths.Logs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.Logs, "youkai.log"), []byte("log line"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := newServiceWithStore(t, paths, store, nil, func() (Recognizer, error) { return newFakeRecognizer(), nil })

	usage, err := svc.StorageUsage()
	if err != nil {
		t.Fatalf("StorageUsage: %v", err)
	}
	if usage.Records != 1 || usage.EvidenceFiles != 2 || usage.EvidenceBytes == 0 {
		t.Errorf("usage = %+v, want one record with two measured screenshots", usage)
	}
	if usage.RecordBytes < usage.EvidenceBytes {
		t.Errorf("record bytes %d do not include evidence %d", usage.RecordBytes, usage.EvidenceBytes)
	}
	if usage.CacheBytes != 5 {
		t.Errorf("cache bytes = %d, want 5", usage.CacheBytes)
	}
	if usage.LogBytes != int64(len("log line")) {
		t.Errorf("log bytes = %d, want %d", usage.LogBytes, len("log line"))
	}
	if usage.DiskSpaceKnown && usage.DiskTotalBytes <= 0 {
		t.Errorf("disk space known but total = %d", usage.DiskTotalBytes)
	}
}

func TestCleanupBlockedErrorExplainsReason(t *testing.T) {
	err := &CleanupBlockedError{Reason: "扫描会话进行中，结束后才能清理。"}
	if !errors.Is(err, ErrCleanupBlocked) {
		t.Fatal("CleanupBlockedError does not unwrap to ErrCleanupBlocked")
	}
	if !strings.Contains(err.Error(), "扫描会话进行中") {
		t.Fatalf("error = %q, want its reason", err)
	}
}
