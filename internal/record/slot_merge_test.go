package record

import (
	"context"
	"errors"
	"os"
	"testing"
)

func TestSlotMergeFailureLeavesBothRevisionsAndCanRetry(t *testing.T) {
	for _, tc := range []struct {
		name    string
		cleaned bool
	}{
		{name: "copy"},
		{name: "checkpoint"},
		{name: "directory sync after cleanup", cleaned: true},
		{name: "checkpoint after cleanup", cleaned: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
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
			if tc.cleaned {
				removal, err := st.DeleteEvidence(baseID)
				if err != nil {
					t.Fatal(err)
				}
				baseRev = removal.Generation
				base.Items[0].EvidenceIDs = nil
				base.Evidence = nil
			}
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

			switch tc.name {
			case "copy":
				fs.injectShortWrite(st.evidencePath(baseID, copyID), 3, errDiskFull)
			case "directory sync after cleanup":
				fs.inject(faultSyncDir, st.recordDir(baseID), errDiskFull)
			default:
				fs.injectShortWrite(st.generationPath(baseID, baseRev+1), 3, errDiskFull)
			}
			if _, err := st.CommitSlotMerge(commit); !errors.Is(err, errDiskFull) {
				t.Fatalf("injected failure = %v", err)
			}
			if _, err := os.Stat(st.evidencePath(baseID, copyID)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("failed merge left copied evidence: %v", err)
			}
			if tc.cleaned {
				if _, err := os.Lstat(st.evidenceDir(baseID)); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("failed merge left a new evidence directory: %v", err)
				}
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
