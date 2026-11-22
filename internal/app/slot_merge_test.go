package app

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/record"
)

func TestSlotMergePreservesRevisionsEvidenceAndExportIdentity(t *testing.T) {
	svc, store := slotMergeService(t)
	base := saveSlotMergeFixture(t, store, "original", "", nil, []record.PositionConclusion{
		{Position: 1, Complete: true}, {Position: 2, Complete: true}, {Position: 3, Complete: true},
		{Position: 4}, {Position: 5}, {Position: 6, Complete: true},
	}, []int{4, 1, 5})
	saveSlotMergeFixture(t, store, "supplement", base, []int{4, 5}, []record.PositionConclusion{
		{Position: 4, Complete: true}, {Position: 5},
	}, []int{4, 4, 5})
	for _, id := range []string{base, "supplement"} {
		if _, err := svc.SaveItemRevision(id, 1, 0, ItemRevisionEdit{Data: reviewData(), Confirmed: true}); err != nil {
			t.Fatal(err)
		}
	}
	before := latestMergeFixture(t, store, base)
	supplement := latestMergeFixture(t, store, "supplement")
	preview, err := svc.PreviewSlotMerge("supplement")
	if err != nil {
		t.Fatal(err)
	}
	want := []SlotMergeChoice{{Slot: 4, PreviousCount: 1, SupplementCount: 2, PreviousReviewed: 1}}
	if !reflect.DeepEqual(preview.Choices, want) {
		t.Fatalf("preview = %+v, want %+v", preview.Choices, want)
	}
	req := mergeFixtureRequest(preview, 4)
	if _, err := svc.MergeSlots(req); err != nil {
		t.Fatal(err)
	}
	merged := latestMergeFixture(t, store, base)
	cp := &merged.Checkpoint
	if cp.SavedRevision <= before.Checkpoint.SavedRevision || cp.Session.CapturedCount != 4 ||
		cp.Session.Coverage.Status != record.CoveragePartial || cp.Session.Coverage.ExpectedCount != nil {
		t.Fatalf("merged session: %+v", cp.Session)
	}
	if got := cp.Items; !reflect.DeepEqual(got[:2], before.Checkpoint.Items[1:]) ||
		got[2].Sequence != 4 || got[3].Sequence != 5 || got[2].Revision != 1 ||
		got[2].Origin == nil || got[2].Origin.SessionID != "supplement" || got[2].Origin.Sequence != 1 {
		t.Fatalf("retained and imported identities are incorrect: %+v", got)
	}
	if len(cp.SlotMerges) != 1 || len(cp.SlotMerges[0].Replaced) != 1 {
		t.Fatalf("merge archive count is wrong: %+v", cp.SlotMerges)
	}
	// The enclosing archive changes indentation of RawMessage payloads.
	archived, archiveErr := json.Marshal(cp.SlotMerges[0].Replaced[0])
	original, originalErr := json.Marshal(before.Checkpoint.Items[0])
	if archiveErr != nil || originalErr != nil || !bytes.Equal(archived, original) {
		t.Fatalf("archive changed the observation: %s, want %s (%v, %v)", archived, original, archiveErr, originalErr)
	}
	if len(cp.Evidence) != 5 || !strings.HasSuffix(MergeEvidenceCropName(cp.Items[2].EvidenceIDs[0]), "000004-line0") {
		t.Fatalf("wrong evidence count or crop role: %d, %v", len(cp.Evidence), cp.Items[2].EvidenceIDs)
	}
	if !reflect.DeepEqual(supplement, latestMergeFixture(t, store, "supplement")) {
		t.Fatal("merge changed the supplement")
	}
	if _, err := svc.MergeSlots(req); !errors.Is(err, record.ErrRevisionConflict) {
		t.Fatalf("double confirmation = %v", err)
	}
	if _, err := svc.SaveItemRevision(base, 1, 1, ItemRevisionEdit{Data: reviewData()}); !errors.Is(err, record.ErrItemNotFound) {
		t.Fatalf("stale edit of a retired sequence = %v", err)
	}

	export, err := BuildUsableExport(merged)
	if err != nil {
		t.Fatal(err)
	}
	data, err := export.JSON(exportTestTime(), "test")
	if err != nil {
		t.Fatal(err)
	}
	var doc exportDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	sources := doc.Session.SlotSources
	if doc.FormatVersion != 2 || doc.Session.Coverage.Status != record.CoveragePartial || len(doc.Session.Scope.Slots) != 6 ||
		doc.Session.CapturedCount != int64(len(doc.Items)+doc.ExcludedCount) || len(doc.Items) != 1 ||
		doc.Items[0].Origin == nil || doc.Items[0].Sequence != 4 || len(sources) != 6 ||
		sources[3].Slot != 4 || sources[3].SessionID != "supplement" ||
		sources[3].SavedRevision != supplement.Checkpoint.SavedRevision ||
		sources[3].StartedAt != formatExportTime(supplement.Checkpoint.Session.StartedAt) {
		t.Fatalf("unexpected merged export: %+v", doc)
	}
	data, err = export.CSV()
	if err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(strings.NewReader(string(data))).ReadAll()
	if err != nil || len(rows) != len(doc.Items)+1 || rows[1][0] != "4" || rows[1][1] != base {
		t.Fatalf("CSV does not describe the same items: %v, %v", rows, err)
	}

	// Consumed slots can no longer be replaced. A different completed and
	// empty slot can be merged later without recycling the retired sequence.
	saveSlotMergeFixture(t, store, "empty-supplement", base, []int{5}, []record.PositionConclusion{
		{Position: 5, Complete: true},
	}, nil)
	empty, err := svc.PreviewSlotMerge("empty-supplement")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.MergeSlots(mergeFixtureRequest(empty, 5)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DeleteRecord("supplement"); err != nil {
		t.Fatal(err)
	}
	updated := latestMergeFixture(t, store, base)
	if len(updated.Checkpoint.SlotMerges) != 2 || len(updated.Checkpoint.Items) != 3 ||
		updated.Checkpoint.Items[1].Origin.SessionID != "supplement" {
		t.Fatal("the composed record depends on the supplement directory")
	}
	candidates, known := RescanCandidatesOf(updated.Checkpoint.Session)
	if !known || len(candidates) != 0 || updated.Checkpoint.Session.Coverage.Status != record.CoveragePartial {
		t.Fatalf("all six are individually complete but coverage must stay partial: %v, %t", candidates, known)
	}
	if _, err := svc.DeleteRecordScreenshots(base); err != nil {
		t.Fatal(err)
	}
	clean := latestMergeFixture(t, store, base)
	if len(clean.Checkpoint.Evidence) != 0 || len(clean.Checkpoint.SlotMerges) != 2 ||
		clean.Checkpoint.SlotMerges[0].Replaced[0].Revision != 1 ||
		len(clean.Checkpoint.SlotMerges[0].Replaced[0].EvidenceIDs) != 0 {
		t.Fatal("screenshot cleanup did not preserve the archived revision")
	}
}

func TestSlotMergeRefusesUnassignedItemsAndStaleRevisions(t *testing.T) {
	svc, store := slotMergeService(t)
	base := saveSlotMergeFixture(t, store, "original", "", nil, []record.PositionConclusion{
		{Position: 4},
	}, []int{4})
	proof := []record.PositionConclusion{{Position: 4, Complete: true}}
	saveSlotMergeFixture(t, store, "unknown-position", base, []int{4}, proof, []int{0})
	if _, err := svc.PreviewSlotMerge("unknown-position"); !errors.Is(err, ErrSlotMergeUnassigned) {
		t.Fatalf("preview accepted an item without capture attribution: %v", err)
	}
	saveSlotMergeFixture(t, store, "supplement", base, []int{4}, proof, []int{4})
	preview, err := svc.PreviewSlotMerge("supplement")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.MergeSlots(mergeFixtureRequest(preview, 1)); !errors.Is(err, ErrSlotMergeUnavailable) {
		t.Fatalf("confirm accepted an unoffered slot: %v", err)
	}
	if _, err := svc.SaveItemRevision("supplement", 1, 0, ItemRevisionEdit{Data: reviewData(), Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.MergeSlots(mergeFixtureRequest(preview, 4)); !errors.Is(err, record.ErrRevisionConflict) {
		t.Fatalf("merge accepted a stale preview: %v", err)
	}
	if got := latestMergeFixture(t, store, base).Checkpoint; got.SavedRevision != 1 || len(got.SlotMerges) != 0 {
		t.Fatalf("rejected merge wrote a generation: %+v", got)
	}
}

func TestSlotMergeAfterOriginalScreenshotsAreDeleted(t *testing.T) {
	paths := newTestPaths(t)
	store, err := record.Open(paths.Data)
	if err != nil {
		t.Fatal(err)
	}
	svc := newServiceWithStore(t, paths, store, nil, nil)
	base := saveSlotMergeFixture(t, store, "original", "", nil, []record.PositionConclusion{
		{Position: 1, Complete: true}, {Position: 4},
	}, []int{1, 4})
	supplement := saveSlotMergeFixture(t, store, "supplement", base, []int{4}, []record.PositionConclusion{
		{Position: 4, Complete: true},
	}, []int{4})
	if _, err := svc.DeleteRecordScreenshots(base); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(record.EvidencePath(paths.Data, base, "000001-line0"))
	if _, err := os.Lstat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cleanup left an evidence directory: %v", err)
	}
	if err := svc.Quit(); err != nil {
		t.Fatal(err)
	}
	store, err = record.Open(paths.Data)
	if err != nil {
		t.Fatal(err)
	}
	svc = newServiceWithStore(t, paths, store, nil, nil)
	preview, err := svc.PreviewSlotMerge(supplement)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.MergeSlots(mergeFixtureRequest(preview, 4)); err != nil {
		t.Fatal(err)
	}
	cp := latestMergeFixture(t, store, base).Checkpoint
	if len(cp.Items) != 2 || len(cp.SlotMerges) != 1 || len(cp.SlotMerges[0].Replaced) != 1 ||
		len(cp.Items[0].EvidenceIDs) != 0 || len(cp.SlotMerges[0].Replaced[0].EvidenceIDs) != 0 ||
		len(cp.Evidence) != 1 || !slices.Equal(cp.Items[1].EvidenceIDs, []string{cp.Evidence[0].ID}) {
		t.Fatalf("merge did not preserve screenshot cleanup or import the new screenshot: %+v", cp)
	}
	got, err := os.ReadFile(record.EvidencePath(paths.Data, base, cp.Evidence[0].ID))
	if err != nil || string(got) != "supplement item 1" {
		t.Fatalf("copied screenshot = %q, %v", got, err)
	}
}

func TestSlotMergeConcurrentConfirmationsRequireNewPreview(t *testing.T) {
	svc, store := slotMergeService(t)
	base := saveSlotMergeFixture(t, store, "original", "", nil, []record.PositionConclusion{
		{Position: 4}, {Position: 5},
	}, []int{4, 5})
	saveSlotMergeFixture(t, store, "supplement", base, []int{4, 5}, []record.PositionConclusion{
		{Position: 4, Complete: true}, {Position: 5, Complete: true},
	}, []int{4, 5})
	preview, err := svc.PreviewSlotMerge("supplement")
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	for _, slot := range []int{4, 5} {
		go func() {
			_, err := svc.MergeSlots(mergeFixtureRequest(preview, slot))
			results <- err
		}()
	}
	first, second := <-results, <-results
	if first != nil {
		first, second = second, first
	}
	if first != nil || !errors.Is(second, record.ErrRevisionConflict) {
		t.Fatalf("concurrent confirmations = %v, %v", first, second)
	}
	latest, err := svc.PreviewSlotMerge("supplement")
	if err != nil {
		t.Fatal(err)
	}
	if len(latest.Choices) != 1 {
		t.Fatalf("next preview did not exclude the committed slot: %+v", latest)
	}
	if _, err := svc.MergeSlots(mergeFixtureRequest(latest, latest.Choices[0].Slot)); err != nil {
		t.Fatal(err)
	}
	got := latestMergeFixture(t, store, base).Checkpoint
	if len(got.SlotMerges) != 2 || len(got.Items) != 2 || got.Items[0].Sequence != 3 || got.Items[1].Sequence != 4 {
		t.Fatalf("confirmed revisions left the wrong composition: %+v", got)
	}
}

func slotMergeService(t *testing.T) (*Service, *record.Store) {
	t.Helper()
	paths := newTestPaths(t)
	store, err := record.Open(paths.Data)
	if err != nil {
		t.Fatal(err)
	}
	return newServiceWithStore(t, paths, store, nil, nil), store
}

func saveSlotMergeFixture(t *testing.T, store *record.Store, id, parent string, planned []int, ledger []record.PositionConclusion, positions []int) string {
	t.Helper()
	shell := exportTestShell(len(positions))
	shell.SessionID = id
	shell.Client = record.ClientWindowsLocal
	shell.Coverage.Status = record.CoveragePartial
	shell.PositionLedger = &record.PositionLedger{Sort: record.SortVerificationVerified, Positions: ledger}
	if parent != "" {
		shell.Scope = scopedScope(parent, planned...)
		shell.StartedAt = shell.StartedAt.Add(time.Hour)
		ended := shell.EndedAt.Add(time.Hour)
		shell.EndedAt = &ended
	}
	cp := record.Checkpoint{Session: shell}
	if err := store.CreateRecord(id); err != nil {
		t.Fatal(err)
	}
	for i, position := range positions {
		sequence := int64(i + 1)
		item := reviewItem(t, sequence, reviewData())
		item.Observed = []byte(fmt.Sprintf(`{"lines":[{"role":"name"}],"capture":{"position":%d}}`, position))
		ev, err := store.WriteEvidence(id, fmt.Sprintf("%06d-line0", sequence), []byte(fmt.Sprintf("%s item %d", id, sequence)))
		if err != nil {
			t.Fatal(err)
		}
		item.EvidenceIDs = []string{ev.ID}
		cp.Items = append(cp.Items, item)
		cp.Evidence = append(cp.Evidence, ev)
	}
	if _, err := store.SaveCheckpoint(id, cp); err != nil {
		t.Fatal(err)
	}
	return id
}

func latestMergeFixture(t *testing.T, store *record.Store, id string) *record.Recovery {
	t.Helper()
	got, err := store.LoadLatest(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func mergeFixtureRequest(preview SlotMergePreview, slots ...int) SlotMergeRequest {
	return SlotMergeRequest{
		RecordID: preview.RecordID, RecordRevision: preview.RecordRevision,
		SupplementID: preview.SupplementID, SupplementRevision: preview.SupplementRevision,
		Slots: slices.Clone(slots),
	}
}
