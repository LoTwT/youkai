package ui

import (
	"slices"
	"testing"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/record"
)

func TestSlotMergeSummaryPreviewsBeforeConfirmingAndReopensOriginal(t *testing.T) {
	supplement := rescanRecord()
	shell := &supplement.Checkpoint.Session
	shell.Client = record.ClientWindowsLocal
	shell.Scope = []byte(`{"positions":[4],"rescanOf":"original"}`)
	shell.PositionLedger = &record.PositionLedger{
		Sort: record.SortVerificationVerified, Positions: []record.PositionConclusion{{Position: 4, Complete: true}},
	}
	original := rescanRecord()
	original.Checkpoint.Session.SessionID = "original"
	original.Checkpoint.SlotMerges = []record.SlotMerge{{
		BaseRevision: 2, SupplementID: "rec", SupplementRevision: 3,
		Sources: []record.SlotSource{{Slot: 4, SessionID: "rec", SavedRevision: 3, StartedAt: shell.StartedAt}},
	}}
	original.Checkpoint.Session.PositionLedger.Positions = append(original.Checkpoint.Session.PositionLedger.Positions,
		record.PositionConclusion{Position: 4, Complete: true})
	m, q, tt, svc := openSummaryRecord(t, supplement, Config{DirectExportSaver: &fakeDirectExportSaver{}}, 1180, 900)
	svc.loads["original"] = original
	svc.mergePreviewFn = func(id string) (app.SlotMergePreview, error) {
		return app.SlotMergePreview{
			RecordID: "original", RecordRevision: 2, RecordStartedAt: original.Checkpoint.Session.StartedAt,
			SupplementID: id, SupplementRevision: 3,
			Choices: []app.SlotMergeChoice{{Slot: 4, PreviousCount: 1, SupplementCount: 2, PreviousReviewed: 1}},
		}, nil
	}
	var requests []app.SlotMergeRequest
	svc.mergeFn = func(req app.SlotMergeRequest) (int64, error) {
		requests = append(requests, req)
		return 3, nil
	}
	click := func(label string) {
		t.Helper()
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
		settle(t, m, q, tt)
	}
	click("合并回原记录")
	if len(requests) != 0 || !tt.HasText("4 号位：1 件 → 2 件（归档 1 件有校对修订的观测）") {
		t.Fatalf("missing read-only preview: %v, %q", requests, tt.Texts())
	}
	click("取消")
	if len(requests) != 0 || !m.recordIsOpen("rec") {
		t.Fatal("cancel submitted a merge or changed records")
	}
	click("合并回原记录")
	click("确认合并")
	if len(requests) != 1 || requests[0].RecordID != "original" || requests[0].RecordRevision != 2 ||
		requests[0].SupplementID != "rec" || requests[0].SupplementRevision != 3 ||
		!slices.Equal(requests[0].Slots, []int{4}) {
		t.Fatalf("confirm did not send the previewed revisions: %+v", requests)
	}
	if !m.recordIsOpen("original") || !tt.HasText("已合并补扫号位") || !tt.HasText("号位采集来源") {
		t.Fatalf("merged original is not open with provenance: %q", tt.Texts())
	}
}
