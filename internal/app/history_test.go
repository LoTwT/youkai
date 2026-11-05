package app

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/game/zzz"
	"github.com/LoTwT/youkai/internal/record"
)

// exportableHistoryItem builds one settled, valid S-rarity item the export
// accepts, so the history counts can be checked against a real usable row.
func exportableHistoryItem(t *testing.T, seq int64) record.ItemRecord {
	t.Helper()
	data := reviewData()
	rarity := "S"
	data.Rarity = &rarity
	item := reviewItem(t, seq, data)
	item.Quality = record.QualityAutoAccepted
	structured := zzz.StructuredDisc{
		Data:       data,
		Issues:     []zzz.Issue{},
		Validation: zzz.Validation{Status: zzz.ValidationPassed, Issues: []zzz.Issue{}},
	}
	raw, err := json.Marshal(structured)
	if err != nil {
		t.Fatalf("marshal structured: %v", err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(item.Data, &doc); err != nil {
		t.Fatalf("unmarshal item payload: %v", err)
	}
	doc["structured"] = raw
	item.Data, err = json.Marshal(doc)
	if err != nil {
		t.Fatalf("remarshal item payload: %v", err)
	}
	return item
}

// TestHistoryItemTotalsSplitsCapturedItems checks the current history
// table's buckets and the separate four-class partition on the same
// items: exportable (warning or passed), failed, unprocessed, the review
// editor's quality queue, and the queued/running remainder.
func TestHistoryItemTotalsSplitsCapturedItems(t *testing.T) {
	exportable := exportableHistoryItem(t, 1)
	pending := reviewItem(t, 2, reviewData())
	other := record.ItemRecord{Sequence: 3, Status: record.StatusQueued, Quality: record.QualityNotEvaluated}
	failed := record.ItemRecord{Sequence: 4, Status: record.StatusFailed, Failure: "识别超时", Quality: record.QualityNotEvaluated}
	unprocessed := record.ItemRecord{Sequence: 5, Status: record.StatusUnprocessed, Reason: record.ReasonCancelled, Quality: record.QualityNotEvaluated}

	items := []record.ItemRecord{exportable, pending, other, failed, unprocessed}
	totals := historyItemTotalsOf(items)
	if totals.Exportable != 1 || totals.Failed != 1 || totals.Unprocessed != 1 ||
		totals.PendingReview != 1 || totals.Other != 1 {
		t.Fatalf("legacy buckets = exportable %d, failed %d, unprocessed %d, pending %d, other %d; want 1/1/1/1/1",
			totals.Exportable, totals.Failed, totals.Unprocessed, totals.PendingReview, totals.Other)
	}
	if totals.Exportable+totals.Failed+totals.Unprocessed+totals.PendingReview+totals.Other != int64(len(items)) {
		t.Fatalf("legacy buckets = %+v, do not add up to %d", totals, len(items))
	}
	counts := totals.Counts
	if counts.Total() != int64(len(items)) {
		t.Fatalf("class total = %d, want %d", counts.Total(), len(items))
	}
	want := ItemClassCounts{PendingBackfill: 3, PendingReview: 1, Passed: 1}
	if counts != want {
		t.Fatalf("four-class counts = %+v, want %+v", counts, want)
	}
}

// TestHistoryLegacyBucketsKeepOldSemantics pins the split between the two
// views: a produced item the export cannot use stays in the table's
// "other" bucket while the four-class view calls it pending review, so the
// existing history numbers do not change before the UI switches.
func TestHistoryLegacyBucketsKeepOldSemantics(t *testing.T) {
	invalid := exportStructuredItem(t, 1, record.QualityAutoAccepted,
		testDisc(31300, 2, "attack", "abc", zzz.UnitFlat),
		zzz.CaptureState{ManagementState: zzz.ManagementNone, EquippedState: zzz.EquippedStateUnequipped})
	unstructured := unstructuredItem(2)
	totals := historyItemTotalsOf([]record.ItemRecord{invalid, unstructured})
	if totals.Other != 2 || totals.PendingReview != 0 || totals.Exportable != 0 {
		t.Fatalf("legacy buckets = %+v, want two other items", totals)
	}
	if totals.Counts != (ItemClassCounts{PendingReview: 2}) {
		t.Fatalf("counts = %+v, want two pending-review items", totals.Counts)
	}
}

// previousHistoryItemCounts is the pre-classification history breakdown
// from 5ae58ce, kept as an independent oracle for the legacy buckets.
func previousHistoryItemCounts(items []record.ItemRecord) (exportable, failed, unprocessed, other int64) {
	for _, item := range items {
		switch {
		case item.Status == record.StatusFailed:
			failed++
		case item.Status == record.StatusUnprocessed:
			unprocessed++
		case item.Status != record.StatusProduced:
			other++
		case !hasStructured(item):
			other++
		case itemPendingReview(item):
		default:
			if _, ok := usableItem(&item); ok {
				exportable++
			} else {
				other++
			}
		}
	}
	return exportable, failed, unprocessed, other
}

// itemClassificationCorpus is the export fixture plus every item shape the
// history buckets distinguish, with each produced item repeated under all
// four qualities and revisions 0 and 1. The expansion pins the two rules
// the classifier relies on without re-decoding a usable payload.
func itemClassificationCorpus(t *testing.T) []record.ItemRecord {
	t.Helper()
	none := zzz.CaptureState{ManagementState: zzz.ManagementNone, EquippedState: zzz.EquippedStateUnequipped}
	items := append([]record.ItemRecord(nil), exportFixtureRecord(t).Checkpoint.Items...)
	items = append(items,
		legacyEquippedOnlyItem(t, 101),
		warningExportableItem(t, 102),
		unstructuredItem(103),
		exportableHistoryItem(t, 104),
		reviewItem(t, 105, reviewData()),
		record.ItemRecord{Sequence: 106, Status: record.StatusQueued, Quality: record.QualityNotEvaluated},
		record.ItemRecord{Sequence: 107, Status: record.StatusRunning, Quality: record.QualityNotEvaluated},
		exportStructuredItem(t, 108, record.QualityAutoAccepted,
			testDisc(31300, 2, "attack", "abc", zzz.UnitFlat), none),
		record.ItemRecord{Sequence: 109, Status: record.StatusFailed, Failure: "识别失败", Quality: record.QualityNotEvaluated},
		record.ItemRecord{Sequence: 110, Status: record.StatusUnprocessed, Reason: record.ReasonInterrupted, Quality: record.QualityNotEvaluated},
	)
	base := append([]record.ItemRecord(nil), items...)
	qualities := []record.Quality{
		record.QualityAutoAccepted,
		record.QualityHumanConfirmed,
		record.QualityNeedsReview,
		record.QualityNotEvaluated,
	}
	seq := int64(1000)
	for _, item := range base {
		if item.Status != record.StatusProduced {
			continue
		}
		for _, quality := range qualities {
			for _, revision := range []int64{0, 1} {
				candidate := item
				candidate.Quality = quality
				candidate.Revision = revision
				seq++
				candidate.Sequence = seq
				items = append(items, candidate)
			}
		}
	}
	return items
}

// TestUsableItemIsNeverPendingReview pins the quality rule shared by
// exportQuality and itemPendingReview: the classifier may decide pending
// review from the export refusal alone only while a usable item is never
// also pending review.
func TestUsableItemIsNeverPendingReview(t *testing.T) {
	corpus := itemClassificationCorpus(t)
	for _, item := range corpus {
		if _, usable := usableItem(&item); usable && itemPendingReview(item) {
			t.Errorf("item %d (%s/%s rev %d) is usable and pending review",
				item.Sequence, item.Status, item.Quality, item.Revision)
		}
	}
	t.Logf("checked %d items", len(corpus))
}

// TestHistoryLegacyBucketsMatchPreviousRules compares the legacy buckets
// item by item with the pre-classification implementation, so the existing
// history table, results page and cleanup preview keep their numbers.
func TestHistoryLegacyBucketsMatchPreviousRules(t *testing.T) {
	corpus := itemClassificationCorpus(t)
	for i := range corpus {
		one := corpus[i : i+1]
		oldExportable, oldFailed, oldUnprocessed, oldOther := previousHistoryItemCounts(one)
		oldPending := int64(pendingReviewCount(one))
		got := historyItemTotalsOf(one)
		if got.Exportable != oldExportable || got.Failed != oldFailed ||
			got.Unprocessed != oldUnprocessed || got.Other != oldOther ||
			got.PendingReview != oldPending {
			t.Errorf("item %d (%s/%s rev %d): new %+v, old e%d f%d u%d o%d p%d",
				corpus[i].Sequence, corpus[i].Status, corpus[i].Quality, corpus[i].Revision,
				got, oldExportable, oldFailed, oldUnprocessed, oldOther, oldPending)
		}
	}
	t.Logf("checked %d items", len(corpus))
}

// TestServiceHistoryRecordsIncludesExportBreakdown checks that the history
// metadata carries the same counts and coverage denominator the table
// renders, without loading the record.
func TestServiceHistoryRecordsIncludesExportBreakdown(t *testing.T) {
	store, paths := cleanupStore(t)
	const id = "history-breakdown"
	if err := store.CreateRecord(id); err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}
	exportable := exportableHistoryItem(t, 1)
	pending := reviewItem(t, 2, reviewData())
	failed := record.ItemRecord{Sequence: 3, Status: record.StatusFailed, Failure: "识别超时", Quality: record.QualityNotEvaluated}
	unprocessed := record.ItemRecord{Sequence: 4, Status: record.StatusUnprocessed, Reason: record.ReasonCancelled, Quality: record.QualityNotEvaluated}
	other := record.ItemRecord{Sequence: 5, Status: record.StatusQueued, Quality: record.QualityNotEvaluated}
	expected := int64(6)
	started := time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)
	termination := &record.Termination{Kind: record.TerminationFailed, Code: "navigationMismatch", Phase: record.PhaseCapturing}
	cp := record.Checkpoint{
		Session: record.SessionShell{
			SessionID:     id,
			Game:          "zzz",
			Entity:        "driveDisc",
			Client:        record.ClientWindowsLocal,
			Locale:        "zh-CN",
			Scope:         json.RawMessage(`{"rarity":"S"}`),
			StartedAt:     started,
			Termination:   termination,
			Coverage:      record.Coverage{Status: record.CoveragePartial, ExpectedCount: &expected},
			CapturedCount: 5,
		},
		Items: []record.ItemRecord{exportable, pending, failed, unprocessed, other},
	}
	if _, err := store.SaveCheckpoint(id, cp); err != nil {
		t.Fatalf("SaveCheckpoint: %v", err)
	}
	svc := newServiceWithStore(t, paths, store, nil, func() (Recognizer, error) { return newFakeRecognizer(), nil })

	records, err := svc.HistoryRecords()
	if err != nil {
		t.Fatalf("HistoryRecords: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %+v, want one", records)
	}
	rec := records[0]
	if rec.CapturedCount != 5 || rec.CoverageExpected != 6 || rec.PendingReview != 1 {
		t.Fatalf("captured/expected/pending = %d/%d/%d, want 5/6/1",
			rec.CapturedCount, rec.CoverageExpected, rec.PendingReview)
	}
	if rec.ExportableCount != 1 || rec.FailedCount != 1 || rec.UnprocessedCount != 1 || rec.OtherCount != 1 {
		t.Fatalf("counts = exportable %d, failed %d, unprocessed %d, other %d; want 1/1/1/1",
			rec.ExportableCount, rec.FailedCount, rec.UnprocessedCount, rec.OtherCount)
	}
	if rec.ExportableCount+int64(rec.PendingReview)+rec.FailedCount+rec.UnprocessedCount+rec.OtherCount != rec.CapturedCount {
		t.Fatal("the history breakdown does not add up to the captured count")
	}
	wantCounts := ItemClassCounts{PendingBackfill: 3, PendingReview: 1, Passed: 1}
	if rec.Counts != wantCounts {
		t.Fatalf("four-class counts = %+v, want %+v", rec.Counts, wantCounts)
	}
	if rec.StartedAt != started {
		t.Fatalf("started at = %s, want %s", rec.StartedAt, started)
	}
	if rec.EndedAt != nil {
		t.Fatalf("ended at = %s, want unknown", rec.EndedAt)
	}
	if _, ok := rec.Duration(); ok {
		t.Fatal("duration is known without an end time")
	}
	if rec.Interrupted {
		t.Fatal("a failed session with a termination is marked interrupted")
	}
	if rec.IncompletePositions != 0 || rec.IncompletePositionsKnown {
		t.Fatalf("incomplete positions = %d known %v, want 0/unknown without position data",
			rec.IncompletePositions, rec.IncompletePositionsKnown)
	}
	if rec.Termination == nil || *rec.Termination != *termination {
		t.Fatalf("termination = %+v, want %+v", rec.Termination, termination)
	}
}

// TestHistoryRecordsPositionConclusions checks the summary-facing
// position conclusions across the checkpoint shapes the summary and
// notification must describe: a new typed ledger, a legacy checkpoint
// whose conclusions live only in the observed scope, a complete scan, a
// scan with six complete positions but an unverified sort, a stop at
// position 3, and a record with no position data (cannot list).
func TestHistoryRecordsPositionConclusions(t *testing.T) {
	store, paths := cleanupStore(t)
	started := time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)
	scope := json.RawMessage(`{"kind":"zzzInventory","sort":"verified","coverage":"partial","positions":[
		{"position":1,"complete":true},
		{"position":2,"complete":true},
		{"position":3,"complete":false,"stage":"filter","reasonCode":"navigationMismatch"},
		{"position":4,"complete":true},
		{"position":5,"complete":false,"stage":"top","reasonCode":"navigationMismatch"},
		{"position":6,"complete":true}
	]}`)
	allComplete := func(sort record.SortVerification) *record.PositionLedger {
		ledger := &record.PositionLedger{Sort: sort}
		for i := 1; i <= record.PositionCount; i++ {
			ledger.Positions = append(ledger.Positions,
				record.PositionConclusion{Position: i, Complete: true})
		}
		return ledger
	}
	degradedLedger := &record.PositionLedger{
		Sort: record.SortVerificationVerified,
		Positions: []record.PositionConclusion{
			{Position: 1, Complete: true},
			{Position: 2, Complete: true},
			{Position: 3, Stage: "filter", ReasonCode: "navigationMismatch"},
			{Position: 4, Complete: true},
			{Position: 5, Stage: "top", ReasonCode: "navigationMismatch"},
			{Position: 6, Complete: true},
		},
	}
	states := func(states ...PositionOutcomeState) []PositionOutcomeState { return states }
	allCompleteStates := states(
		PositionOutcomeComplete, PositionOutcomeComplete, PositionOutcomeComplete,
		PositionOutcomeComplete, PositionOutcomeComplete, PositionOutcomeComplete,
	)
	degradedStates := states(
		PositionOutcomeComplete, PositionOutcomeComplete, PositionOutcomeDegraded,
		PositionOutcomeComplete, PositionOutcomeDegraded, PositionOutcomeComplete,
	)
	degradedCauses := map[int][2]string{
		3: {"filter", "navigationMismatch"},
		5: {"top", "navigationMismatch"},
	}
	cases := []struct {
		id           string
		shell        record.SessionShell
		coverage     record.CoverageStatus
		known        bool
		incomplete   int
		sortVerified bool
		sortKnown    bool
		states       []PositionOutcomeState
		degraded     map[int][2]string
	}{
		{
			id: "positions-new",
			shell: record.SessionShell{
				PositionLedger: degradedLedger,
				// The typed ledger wins over a conflicting legacy scope.
				ObservedScope: json.RawMessage(`{"positions":[{"position":6,"complete":false,"stage":"scroll"}]}`),
			},
			coverage:     record.CoveragePartial,
			known:        true,
			incomplete:   2,
			sortVerified: true,
			sortKnown:    true,
			states:       degradedStates,
			degraded:     degradedCauses,
		},
		{
			id:           "positions-legacy",
			shell:        record.SessionShell{ObservedScope: scope},
			coverage:     record.CoveragePartial,
			known:        true,
			incomplete:   2,
			sortVerified: true,
			sortKnown:    true,
			states:       degradedStates,
			degraded:     degradedCauses,
		},
		{
			id:           "positions-complete",
			shell:        record.SessionShell{PositionLedger: allComplete(record.SortVerificationVerified)},
			coverage:     record.CoverageComplete,
			known:        true,
			sortVerified: true,
			sortKnown:    true,
			states:       allCompleteStates,
		},
		{
			id:           "positions-unverified-sort",
			shell:        record.SessionShell{PositionLedger: allComplete(record.SortVerificationUnverified)},
			coverage:     record.CoveragePartial,
			known:        true,
			sortVerified: false,
			sortKnown:    true,
			states:       allCompleteStates,
		},
		{
			// An explicitly unknown sort in a saved ledger is not "known
			// unverified"; the summary must not claim the sort was checked.
			id: "positions-unknown-sort",
			shell: record.SessionShell{PositionLedger: &record.PositionLedger{
				Sort:      record.SortVerificationUnknown,
				Positions: []record.PositionConclusion{{Position: 1, Complete: true}},
			}},
			coverage: record.CoveragePartial,
			known:    true,
			states: states(
				PositionOutcomeComplete, PositionOutcomeNotReached, PositionOutcomeNotReached,
				PositionOutcomeNotReached, PositionOutcomeNotReached, PositionOutcomeNotReached,
			),
		},
		{
			// Stopping at position 3 leaves 1 and 2 complete, 3
			// truncated and 4–6 never reached; the summary can say the
			// scan covered two positions.
			id: "positions-cancelled-at-3",
			shell: record.SessionShell{PositionLedger: &record.PositionLedger{
				Sort: record.SortVerificationVerified,
				Positions: []record.PositionConclusion{
					{Position: 1, Complete: true},
					{Position: 2, Complete: true},
					{Position: 3},
				},
			}},
			coverage:     record.CoveragePartial,
			known:        true,
			sortVerified: true,
			sortKnown:    true,
			states: states(
				PositionOutcomeComplete, PositionOutcomeComplete, PositionOutcomeTruncated,
				PositionOutcomeNotReached, PositionOutcomeNotReached, PositionOutcomeNotReached,
			),
		},
		{
			// A legacy scope without a sort member still lists positions,
			// but the sort state stays unknown rather than unverified.
			id:       "positions-legacy-unsorted",
			shell:    record.SessionShell{ObservedScope: json.RawMessage(`{"positions":[{"position":1,"complete":true}]}`)},
			coverage: record.CoveragePartial,
			known:    true,
			states: states(
				PositionOutcomeComplete, PositionOutcomeNotReached, PositionOutcomeNotReached,
				PositionOutcomeNotReached, PositionOutcomeNotReached, PositionOutcomeNotReached,
			),
		},
		{
			// The sort verdict is independent of the position list: a
			// legacy scope can state it before any position is reached.
			id:           "positions-sort-only",
			shell:        record.SessionShell{ObservedScope: json.RawMessage(`{"sort":"verified"}`)},
			coverage:     record.CoveragePartial,
			sortVerified: true,
			sortKnown:    true,
		},
		{id: "positions-none", coverage: record.CoveragePartial},
	}
	for _, tc := range cases {
		if err := store.CreateRecord(tc.id); err != nil {
			t.Fatalf("CreateRecord %s: %v", tc.id, err)
		}
		shell := tc.shell
		shell.SessionID = tc.id
		shell.Game = "zzz"
		shell.Entity = "driveDisc"
		shell.Client = record.ClientWindowsLocal
		shell.Locale = "zh-CN"
		shell.Scope = json.RawMessage(`{"rarity":"S"}`)
		shell.StartedAt = started
		shell.Coverage = record.Coverage{Status: tc.coverage}
		if _, err := store.SaveCheckpoint(tc.id, record.Checkpoint{Session: shell}); err != nil {
			t.Fatalf("SaveCheckpoint %s: %v", tc.id, err)
		}
	}
	svc := newServiceWithStore(t, paths, store, nil, func() (Recognizer, error) { return newFakeRecognizer(), nil })
	records, err := svc.HistoryRecords()
	if err != nil {
		t.Fatalf("HistoryRecords: %v", err)
	}
	byID := make(map[string]HistoryRecord, len(records))
	for _, rec := range records {
		byID[rec.ID] = rec
	}
	for _, tc := range cases {
		rec := byID[tc.id]
		if rec.IncompletePositionsKnown != tc.known {
			t.Fatalf("%s: known = %v, want %v", tc.id, rec.IncompletePositionsKnown, tc.known)
		}
		if rec.IncompletePositions != tc.incomplete {
			t.Fatalf("%s: incomplete = %d, want %d", tc.id, rec.IncompletePositions, tc.incomplete)
		}
		if rec.SortVerified != tc.sortVerified || rec.SortVerifiedKnown != tc.sortKnown {
			t.Fatalf("%s: sort = %v/%v, want %v/%v", tc.id,
				rec.SortVerified, rec.SortVerifiedKnown, tc.sortVerified, tc.sortKnown)
		}
		if len(rec.PositionOutcomes) != len(tc.states) {
			t.Fatalf("%s: outcomes = %+v, want %d states", tc.id, rec.PositionOutcomes, len(tc.states))
		}
		for i, want := range tc.states {
			got := rec.PositionOutcomes[i]
			if got.Position != i+1 || got.State != want {
				t.Fatalf("%s: outcome %d = %+v, want position %d state %q", tc.id, i, got, i+1, want)
			}
		}
		for position, want := range tc.degraded {
			got := rec.PositionOutcomes[position-1]
			if got.Stage != want[0] || got.ReasonCode != want[1] {
				t.Fatalf("%s: position %d cause = %q/%q, want %q/%q",
					tc.id, position, got.Stage, got.ReasonCode, want[0], want[1])
			}
		}
	}
}

// TestHistoryRecordsSummaryAndInterruption checks the summary metadata on
// a completed record and the interruption marker on a record whose
// checkpoint never recorded a termination.
func TestHistoryRecordsSummaryAndInterruption(t *testing.T) {
	store, paths := cleanupStore(t)
	const completedID = "history-completed"
	const interruptedID = "history-interrupted"
	const endedInterruptedID = "history-interrupted-ended"
	started := time.Date(2026, 11, 5, 8, 0, 0, 0, time.UTC)
	ended := started.Add(14 * time.Minute)
	for _, id := range []string{completedID, interruptedID, endedInterruptedID} {
		if err := store.CreateRecord(id); err != nil {
			t.Fatalf("CreateRecord %s: %v", id, err)
		}
	}
	completed := record.Checkpoint{
		Session: record.SessionShell{
			SessionID: completedID, Game: "zzz", Entity: "driveDisc",
			Client: record.ClientWindowsLocal, Locale: "zh-CN",
			Scope:     json.RawMessage(`{"rarity":"S"}`),
			StartedAt: started, EndedAt: &ended,
			Termination:   &record.Termination{Kind: record.TerminationCompleted, Code: "verifiedEnd", Phase: record.PhaseCapturing},
			Coverage:      record.Coverage{Status: record.CoverageComplete},
			CapturedCount: 1,
		},
		Items: []record.ItemRecord{warningExportableItem(t, 1)},
	}
	if _, err := store.SaveCheckpoint(completedID, completed); err != nil {
		t.Fatalf("SaveCheckpoint completed: %v", err)
	}
	interrupted := record.Checkpoint{
		Session: record.SessionShell{
			SessionID: interruptedID, Game: "zzz", Entity: "driveDisc",
			Client: record.ClientWindowsLocal, Locale: "zh-CN",
			Scope:         json.RawMessage(`{"rarity":"S"}`),
			StartedAt:     started.Add(time.Hour),
			Coverage:      record.Coverage{Status: record.CoverageUnknown},
			CapturedCount: 1,
		},
		Items: []record.ItemRecord{{Sequence: 1, Status: record.StatusFailed, Failure: "识别失败", Quality: record.QualityNotEvaluated}},
	}
	if _, err := store.SaveCheckpoint(interruptedID, interrupted); err != nil {
		t.Fatalf("SaveCheckpoint interrupted: %v", err)
	}
	endedInterrupted := record.Checkpoint{
		Session: record.SessionShell{
			SessionID: endedInterruptedID, Game: "zzz", Entity: "driveDisc",
			Client: record.ClientWindowsLocal, Locale: "zh-CN",
			Scope:     json.RawMessage(`{"rarity":"S"}`),
			StartedAt: started.Add(2 * time.Hour),
			Termination: &record.Termination{
				Kind: record.TerminationInterrupted, Code: "interrupted", Phase: record.PhaseCapturing,
			},
			Coverage: record.Coverage{Status: record.CoverageUnknown},
		},
	}
	if _, err := store.SaveCheckpoint(endedInterruptedID, endedInterrupted); err != nil {
		t.Fatalf("SaveCheckpoint ended interrupted: %v", err)
	}
	svc := newServiceWithStore(t, paths, store, nil, func() (Recognizer, error) { return newFakeRecognizer(), nil })

	records, err := svc.HistoryRecords()
	if err != nil {
		t.Fatalf("HistoryRecords: %v", err)
	}
	byID := make(map[string]HistoryRecord, len(records))
	for _, rec := range records {
		byID[rec.ID] = rec
	}
	done := byID[completedID]
	if !done.StartedAt.Equal(started) || done.EndedAt == nil || !done.EndedAt.Equal(ended) {
		t.Fatalf("completed times = %s..%v, want %s..%s", done.StartedAt, done.EndedAt, started, ended)
	}
	if duration, ok := done.Duration(); !ok || duration != 14*time.Minute {
		t.Fatalf("duration = %s/%v, want 14m/true", duration, ok)
	}
	if done.Interrupted {
		t.Fatal("completed record is marked interrupted")
	}
	if done.Counts != (ItemClassCounts{Warning: 1}) {
		t.Fatalf("completed counts = %+v, want one warning", done.Counts)
	}
	broken := byID[interruptedID]
	if !broken.Interrupted {
		t.Fatal("record without a termination is not marked interrupted")
	}
	if broken.EndedAt != nil {
		t.Fatalf("interrupted ended at = %s, want unknown", broken.EndedAt)
	}
	if _, ok := broken.Duration(); ok {
		t.Fatal("interrupted record reports a duration")
	}
	if broken.Counts != (ItemClassCounts{PendingBackfill: 1}) {
		t.Fatalf("interrupted counts = %+v, want one pending backfill", broken.Counts)
	}
	endedBroken := byID[endedInterruptedID]
	if !endedBroken.Interrupted {
		t.Fatal("an explicit interrupted termination is not marked interrupted")
	}

	estimate, err := svc.ScanDurationEstimate()
	if err != nil {
		t.Fatalf("ScanDurationEstimate: %v", err)
	}
	if !estimate.Known() || estimate.Samples != 1 || estimate.Median != 14*time.Minute {
		t.Fatalf("estimate = %+v, want one 14m sample", estimate)
	}

	// A running scan also has no termination in its newest checkpoint; the
	// active record must not be labeled as an interrupted recovery.
	svc.active = &activeSession{id: interruptedID}
	records, err = svc.HistoryRecords()
	if err != nil {
		t.Fatalf("HistoryRecords with active session: %v", err)
	}
	for _, rec := range records {
		if rec.ID == interruptedID && rec.Interrupted {
			t.Fatal("the active record is labeled interrupted")
		}
	}

	// A wrapped session has finished even though the slot keeps it until
	// the next start. Its stored interrupted termination must be reported,
	// not suppressed as if the scan were still running.
	svc.active = &activeSession{id: endedInterruptedID, wrapped: true}
	records, err = svc.HistoryRecords()
	if err != nil {
		t.Fatalf("HistoryRecords with wrapped session: %v", err)
	}
	for _, rec := range records {
		if rec.ID == endedInterruptedID && !rec.Interrupted {
			t.Fatal("a wrapped record with an interrupted termination is not marked interrupted")
		}
	}
}

// completedHistory is one normally completed full scan for the estimator
// table.
func completedHistory(t *testing.T, start time.Time, duration time.Duration) HistoryRecord {
	t.Helper()
	ended := start.Add(duration)
	return HistoryRecord{
		ID:        start.Format(time.RFC3339),
		StartedAt: start,
		EndedAt:   &ended,
		Coverage:  record.CoverageComplete,
		Termination: &record.Termination{
			Kind: record.TerminationCompleted, Code: "verifiedEnd", Phase: record.PhaseCapturing,
		},
	}
}

// TestEstimateScanDuration covers the median and sample count at 0, 1, 2,
// 3 and 5 histories, and checks that a manual stop, a failure, partial
// coverage or a missing end never becomes a zero-length sample.
func TestEstimateScanDuration(t *testing.T) {
	base := time.Date(2026, 11, 5, 8, 0, 0, 0, time.UTC)
	at := func(hours int) time.Time { return base.Add(time.Duration(hours) * time.Hour) }

	t.Run("no history", func(t *testing.T) {
		estimate := EstimateScanDuration(nil)
		if estimate.Known() || estimate.Samples != 0 || estimate.Median != 0 {
			t.Fatalf("estimate = %+v, want unknown", estimate)
		}
	})
	t.Run("one history", func(t *testing.T) {
		estimate := EstimateScanDuration([]HistoryRecord{completedHistory(t, at(0), 10*time.Minute)})
		if estimate.Samples != 1 || estimate.Median != 10*time.Minute {
			t.Fatalf("estimate = %+v, want one 10m sample", estimate)
		}
	})
	t.Run("two histories use the mean", func(t *testing.T) {
		estimate := EstimateScanDuration([]HistoryRecord{
			completedHistory(t, at(0), 10*time.Minute),
			completedHistory(t, at(1), 20*time.Minute),
		})
		if estimate.Samples != 2 || estimate.Median != 15*time.Minute {
			t.Fatalf("estimate = %+v, want two samples with a 15m median", estimate)
		}
	})
	t.Run("three histories use the middle", func(t *testing.T) {
		estimate := EstimateScanDuration([]HistoryRecord{
			completedHistory(t, at(0), 30*time.Minute),
			completedHistory(t, at(1), 10*time.Minute),
			completedHistory(t, at(2), 20*time.Minute),
		})
		if estimate.Samples != 3 || estimate.Median != 20*time.Minute {
			t.Fatalf("estimate = %+v, want three samples with a 20m median", estimate)
		}
	})
	t.Run("five histories use the newest three", func(t *testing.T) {
		estimate := EstimateScanDuration([]HistoryRecord{
			completedHistory(t, at(4), 14*time.Minute), // newest
			completedHistory(t, at(0), 100*time.Minute),
			completedHistory(t, at(3), 18*time.Minute),
			completedHistory(t, at(1), 50*time.Minute),
			completedHistory(t, at(2), 12*time.Minute),
		})
		if estimate.Samples != 3 || estimate.Median != 14*time.Minute {
			t.Fatalf("estimate = %+v, want three newest samples with a 14m median", estimate)
		}
	})
	t.Run("ineligible histories are unknown", func(t *testing.T) {
		cancelled := completedHistory(t, at(0), 10*time.Minute)
		cancelled.Termination = &record.Termination{Kind: record.TerminationCancelled, Code: "hotkey", Phase: record.PhaseCapturing}
		failed := completedHistory(t, at(1), 10*time.Minute)
		failed.Termination = &record.Termination{Kind: record.TerminationFailed, Code: "targetLost", Phase: record.PhaseCapturing}
		partial := completedHistory(t, at(2), 10*time.Minute)
		partial.Coverage = record.CoveragePartial
		unknownCoverage := completedHistory(t, at(3), 10*time.Minute)
		unknownCoverage.Coverage = record.CoverageUnknown
		missingEnd := completedHistory(t, at(4), 10*time.Minute)
		missingEnd.EndedAt = nil
		zeroLength := completedHistory(t, at(5), 0)
		unreadable := completedHistory(t, at(6), 10*time.Minute)
		unreadable.Unreadable = true
		interrupted := completedHistory(t, at(7), 10*time.Minute)
		interrupted.Interrupted = true

		estimate := EstimateScanDuration([]HistoryRecord{
			cancelled, failed, partial, unknownCoverage, missingEnd, zeroLength, unreadable, interrupted,
		})
		if estimate.Known() || estimate.Samples != 0 {
			t.Fatalf("estimate = %+v, want unknown from ineligible histories", estimate)
		}
	})
	t.Run("ineligible histories do not change an eligible median", func(t *testing.T) {
		cancelled := completedHistory(t, at(3), 5*time.Minute)
		cancelled.Termination = &record.Termination{Kind: record.TerminationCancelled, Code: "hotkey", Phase: record.PhaseCapturing}
		missingEnd := completedHistory(t, at(4), 5*time.Minute)
		missingEnd.EndedAt = nil
		estimate := EstimateScanDuration([]HistoryRecord{
			cancelled,
			completedHistory(t, at(0), 10*time.Minute),
			missingEnd,
			completedHistory(t, at(1), 20*time.Minute),
			completedHistory(t, at(2), 30*time.Minute),
		})
		if estimate.Samples != 3 || estimate.Median != 20*time.Minute {
			t.Fatalf("estimate = %+v, want three samples with a 20m median", estimate)
		}
	})
}
