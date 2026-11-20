package record

import (
	"context"
	"errors"
	"os"
	"testing"
)

func TestSlotMergeFailureLeavesBothRevisionsAndCanRetry(t *testing.T) {
	for _, boundary := range []string{"copy", "checkpoint"} {
		t.Run(boundary, func(t *testing.T) {
			st, fs, root := newFaultStore(t)
			const baseID, supplementID, copyID = "original", "supplement", "merge-copy"
			for _, id := range []string{baseID, supplementID} {
				if err := st.CreateRecord(id); err != nil {
					t.Fatal(err)
				}
			}
			oldEvidence, err := st.WriteEvidence(baseID, "old", []byte("original image"))
			if err != nil {
				t.Fatal(err)
			}
			newEvidence, err := st.WriteEvidence(supplementID, "new", []byte("supplement image"))
			if err != nil {
				t.Fatal(err)
			}
			base := checkpointWithEvidence(oldEvidence)
			base.Session.SessionID = baseID
			supplement := checkpointWithEvidence(newEvidence)
			supplement.Session.SessionID = supplementID
			baseRev := saveCheckpoint(t, st, baseID, base)
			supplementRev := saveCheckpoint(t, st, supplementID, supplement)
			merged := *cloneCheckpoint(&base)
			merged.Session.Coverage = Coverage{Status: CoveragePartial}
			merged.Session.PositionLedger = &PositionLedger{
				Sort: SortVerificationUnknown, Positions: []PositionConclusion{{Position: 4, Complete: true}},
			}
			merged.SlotMerges = []SlotMerge{{
				BaseRevision: baseRev, SupplementID: supplementID, SupplementRevision: supplementRev,
				Sources:  []SlotSource{{Slot: 4, SessionID: supplementID, StartedAt: supplement.Session.StartedAt, SavedRevision: supplementRev}},
				Replaced: base.Items,
			}}
			copied := newEvidence
			copied.ID = copyID
			merged.Evidence = append(merged.Evidence, copied)
			merged.Items[0].Sequence = 2
			merged.Items[0].Origin = &ItemOrigin{SessionID: supplementID, Sequence: 1}
			merged.Items[0].EvidenceIDs = []string{copyID}
			commit := SlotMergeCommit{
				RecordID: baseID, RecordRevision: baseRev, SupplementID: supplementID, SupplementRevision: supplementRev,
				Checkpoint: merged, EvidenceCopies: map[string]string{copyID: newEvidence.ID},
			}

			path := st.generationPath(baseID, baseRev+1)
			if boundary == "copy" {
				path = st.evidencePath(baseID, copyID)
			}
			fs.injectShortWrite(path, 3, errDiskFull)
			if _, err := st.CommitSlotMerge(commit); !errors.Is(err, errDiskFull) {
				t.Fatalf("injected failure = %v", err)
			}
			if _, err := os.Stat(st.evidencePath(baseID, copyID)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("failed merge left copied evidence: %v", err)
			}
			for id, want := range map[string]int64{baseID: baseRev, supplementID: supplementRev} {
				rec, err := st.LoadLatest(context.Background(), id)
				if err != nil || rec.Checkpoint.SavedRevision != want {
					t.Fatalf("failed merge changed %s: %v, %v", id, rec, err)
				}
			}
			fs.clear()
			if _, err := st.CommitSlotMerge(commit); err != nil {
				t.Fatal(err)
			}
			if got := string(readEvidenceFile(t, root, baseID, copyID+".png")); got != "supplement image" {
				t.Fatalf("retry copied %q", got)
			}
			if _, err := st.CommitSlotMerge(commit); !errors.Is(err, ErrRevisionConflict) {
				t.Fatalf("stale store commit = %v", err)
			}
		})
	}
}
