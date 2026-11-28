package app

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/LoTwT/youkai/internal/record"
)

func TestActiveSnapshotOnlyPublishesSuccessfulSaves(t *testing.T) {
	root := t.TempDir()
	store, err := record.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	const id = "active-snapshot"
	if err := store.CreateRecord(id); err != nil {
		t.Fatal(err)
	}
	shell := record.SessionShell{
		SessionID: id, Game: "zzz", Entity: "driveDisc", Client: record.ClientWindowsLocal,
		Locale: "zh-CN", Scope: []byte(`{"kind":"test"}`), StartedAt: nowUTC(),
		Coverage: record.Coverage{Status: record.CoverageUnknown},
	}
	d := newRecordDeliverer(store, shell, nil)
	d.cacheLiveSnapshot = true
	file, err := store.WriteEvidence(id, "crop", []byte("evidence"))
	if err != nil {
		t.Fatal(err)
	}
	d.evidence = append(d.evidence, file)
	d.items = append(d.items, record.ItemRecord{
		Sequence: 1, Status: record.StatusQueued, Quality: record.QualityNotEvaluated,
		EvidenceIDs: []string{"crop"}, Observed: []byte(`{"a":1}`),
	})
	if err := d.saveLocked(); err != nil {
		t.Fatal(err)
	}
	memo := d.savedLiveSnapshot.Load()
	want, err := store.Load(id)
	if err != nil {
		t.Fatal(err)
	}
	view := record.ActiveCheckpointRecovery(memo)
	if !sameActiveSnapshot(t, view, want) {
		t.Fatalf("snapshot differs from verified disk view: revision %d, disk %d", view.Checkpoint.SavedRevision, want.Checkpoint.SavedRevision)
	}
	view.Checkpoint.Items[0].Observed[2] = 'b'
	view.Checkpoint.Items[0].EvidenceIDs[0] = "changed"
	d.items[0].Status = record.StatusProduced
	unchanged := record.ActiveCheckpointRecovery(memo)
	if !sameActiveSnapshot(t, unchanged, want) {
		t.Fatal("UI mutation or newly delivered state leaked into the saved snapshot")
	}
	if err := os.Remove(record.EvidencePath(root, id, "crop")); err != nil {
		t.Fatal(err)
	}
	if err := d.saveLocked(); err == nil || d.savedLiveSnapshot.Load() != memo {
		t.Fatal("failed save replaced the published snapshot")
	}
	if _, err := store.Load(id); err == nil {
		t.Fatal("ordinary disk recovery accepted missing evidence")
	}
}

func sameActiveSnapshot(t *testing.T, a, b *record.Recovery) bool {
	t.Helper()
	view, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	disk, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	if string(view) != string(disk) {
		t.Logf("snapshot=%s disk=%s", view, disk)
	}
	return string(view) == string(disk)
}
