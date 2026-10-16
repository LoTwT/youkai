package record

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// validDoc builds a structurally valid checkpoint: one produced item with
// an original observation, evidence and no manual edits, one unprocessed
// item with a cancel reason, and a matching evidence index.
func validDoc() *Checkpoint {
	started := time.Date(2026, 10, 16, 8, 0, 0, 0, time.UTC)
	return &Checkpoint{
		Format:        FormatName,
		FormatVersion: FormatVersion,
		SavedRevision: 1,
		Session: SessionShell{
			SessionID: "0f0e0d0c-1a2b-3c4d-5e6f-7a8b9c0d1e2f",
			Game:      "zzz",
			Entity:    "driveDisc",
			Client:    ClientWindowsLocal,
			Locale:    "zh-CN",
			Scope:     json.RawMessage(`{"rarity":"S"}`),
			StartedAt: started,
			Termination: &Termination{
				Kind:  TerminationCancelled,
				Code:  "userCancelled",
				Phase: PhaseCapturing,
			},
			Coverage:      Coverage{Status: CoveragePartial},
			CapturedCount: 2,
			Resources: []ResourceRef{{
				Role:    ResourceModel,
				ID:      "pp-ocrv6-rec-small",
				Version: "1",
				SHA256:  strings.Repeat("ab", 32),
			}},
			CoverageEvidenceIDs: []string{"coverage-end"},
		},
		Items: []ItemRecord{
			{
				Sequence:    1,
				Status:      StatusProduced,
				Quality:     QualityAutoAccepted,
				Observed:    json.RawMessage(`{"slot":6}`),
				Data:        json.RawMessage(`{"slot":6}`),
				EvidenceIDs: []string{"detail-1", "quality-1"},
			},
			{
				Sequence:    2,
				Status:      StatusUnprocessed,
				Reason:      ReasonCancelled,
				Quality:     QualityNotEvaluated,
				EvidenceIDs: []string{"nav-fail-2"},
			},
		},
		Evidence: []EvidenceFile{
			{ID: "detail-1", ByteLength: 120, SHA256: strings.Repeat("01", 32)},
			{ID: "quality-1", ByteLength: 90, SHA256: strings.Repeat("02", 32)},
			{ID: "nav-fail-2", ByteLength: 60, SHA256: strings.Repeat("03", 32)},
			{ID: "coverage-end", ByteLength: 200, SHA256: strings.Repeat("04", 32)},
		},
	}
}

func TestValidateAcceptsValidDoc(t *testing.T) {
	if err := validDoc().validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestValidateShell(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*Checkpoint)
	}{
		{"wrong format name", func(cp *Checkpoint) { cp.Format = "youkai.scan" }},
		{"unknown format version", func(cp *Checkpoint) { cp.FormatVersion = 2 }},
		{"saved revision zero", func(cp *Checkpoint) { cp.SavedRevision = 0 }},
		{"empty session id", func(cp *Checkpoint) { cp.Session.SessionID = "" }},
		{"empty game", func(cp *Checkpoint) { cp.Session.Game = "" }},
		{"empty entity", func(cp *Checkpoint) { cp.Session.Entity = "" }},
		{"unknown client", func(cp *Checkpoint) { cp.Session.Client = "web" }},
		{"empty locale", func(cp *Checkpoint) { cp.Session.Locale = "" }},
		{"nil scope", func(cp *Checkpoint) { cp.Session.Scope = nil }},
		{"broken scope JSON", func(cp *Checkpoint) { cp.Session.Scope = json.RawMessage(`{"rarity":`) }},
		{"null scope", func(cp *Checkpoint) { cp.Session.Scope = json.RawMessage(`null`) }},
		{"zero startedAt", func(cp *Checkpoint) { cp.Session.StartedAt = time.Time{} }},
		{"zero endedAt", func(cp *Checkpoint) {
			cp.Session.EndedAt = &time.Time{}
		}},
		{"termination without code", func(cp *Checkpoint) {
			cp.Session.Termination = &Termination{Kind: TerminationFailed, Phase: PhaseFinalizing}
		}},
		{"termination with bad phase", func(cp *Checkpoint) {
			cp.Session.Termination.Code = "storageFailure"
			cp.Session.Termination.Phase = "waiting"
		}},
		{"unknown coverage status", func(cp *Checkpoint) { cp.Session.Coverage.Status = "total" }},
		{"negative expected count", func(cp *Checkpoint) {
			n := int64(-1)
			cp.Session.Coverage.ExpectedCount = &n
		}},
		{"captured count above items", func(cp *Checkpoint) { cp.Session.CapturedCount = 3 }},
		{"resource with bad role", func(cp *Checkpoint) { cp.Session.Resources[0].Role = "gpu" }},
		{"resource with empty version", func(cp *Checkpoint) { cp.Session.Resources[0].Version = "" }},
		{"resource with short digest", func(cp *Checkpoint) { cp.Session.Resources[0].SHA256 = "ab" }},
		{"resource with uppercase digest", func(cp *Checkpoint) {
			cp.Session.Resources[0].SHA256 = strings.Repeat("AB", 32)
		}},
		{"duplicate coverage evidence", func(cp *Checkpoint) {
			cp.Session.CoverageEvidenceIDs = []string{"coverage-end", "coverage-end"}
		}},
		{"escaping coverage evidence id", func(cp *Checkpoint) {
			cp.Session.CoverageEvidenceIDs = []string{"../escape"}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cp := validDoc()
			tc.mut(cp)
			if err := cp.validate(); err == nil {
				t.Fatal("validate succeeded, want error")
			}
		})
	}
}

func TestValidateItems(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*Checkpoint)
	}{
		{"unknown status", func(cp *Checkpoint) { cp.Items[0].Status = "waiting" }},
		{"reason on produced", func(cp *Checkpoint) { cp.Items[0].Reason = ReasonCancelled }},
		{"unprocessed without reason", func(cp *Checkpoint) { cp.Items[1].Reason = "" }},
		{"unprocessed with invented reason", func(cp *Checkpoint) { cp.Items[1].Reason = "bored" }},
		{"unknown quality", func(cp *Checkpoint) { cp.Items[0].Quality = "perfect" }},
		{"negative revision", func(cp *Checkpoint) { cp.Items[0].Revision = -1 }},
		{"human confirmed without revisions", func(cp *Checkpoint) { cp.Items[0].Quality = QualityHumanConfirmed }},
		{"null observed payload", func(cp *Checkpoint) { cp.Items[0].Observed = json.RawMessage(`null`) }},
		{"broken data payload", func(cp *Checkpoint) { cp.Items[0].Data = json.RawMessage(`{"slot":`) }},
		{"escaping evidence id", func(cp *Checkpoint) { cp.Items[0].EvidenceIDs = []string{"../escape"} }},
		{"duplicate evidence id", func(cp *Checkpoint) {
			cp.Items[0].EvidenceIDs = []string{"detail-1", "detail-1"}
		}},
		{"sequence out of order", func(cp *Checkpoint) {
			cp.Items[0].Sequence, cp.Items[1].Sequence = 2, 1
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cp := validDoc()
			tc.mut(cp)
			if err := cp.validate(); err == nil {
				t.Fatal("validate succeeded, want error")
			}
		})
	}
}

func TestValidateRevisionHistory(t *testing.T) {
	// One saved edit: revision 1, needs review, changes recorded.
	edited := func() *Checkpoint {
		cp := validDoc()
		cp.Items[0].Revision = 1
		cp.Items[0].Quality = QualityNeedsReview
		cp.Items[0].Revisions = []ItemRevision{{
			Revision: 1,
			SavedAt:  time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC),
			Changes: []FieldChange{{
				Field:  "slot",
				Before: json.RawMessage(`6`),
				After:  json.RawMessage(`5`),
			}},
		}}
		return cp
	}
	if err := edited().validate(); err != nil {
		t.Fatalf("edited item does not validate: %v", err)
	}
	confirmed := edited()
	confirmed.Items[0].Quality = QualityHumanConfirmed
	confirmed.Items[0].Revisions[0].Confirmed = true
	if err := confirmed.validate(); err != nil {
		t.Fatalf("confirmed item does not validate: %v", err)
	}

	cases := []struct {
		name string
		mut  func(*Checkpoint)
	}{
		{"revision number mismatch", func(cp *Checkpoint) { cp.Items[0].Revisions[0].Revision = 2 }},
		{"zero savedAt", func(cp *Checkpoint) { cp.Items[0].Revisions[0].SavedAt = time.Time{} }},
		{"empty change field", func(cp *Checkpoint) {
			cp.Items[0].Revisions[0].Changes = []FieldChange{{Field: ""}}
		}},
		{"duplicate change field", func(cp *Checkpoint) {
			cp.Items[0].Revisions[0].Changes = []FieldChange{
				{Field: "slot", After: json.RawMessage(`5`)},
				{Field: "slot", After: json.RawMessage(`4`)},
			}
		}},
		{"broken change value", func(cp *Checkpoint) {
			cp.Items[0].Revisions[0].Changes[0].After = json.RawMessage(`{`)
		}},
		{"confirmed but quality disagrees", func(cp *Checkpoint) {
			cp.Items[0].Revisions[0].Confirmed = true
		}},
		{"quality kept after edit", func(cp *Checkpoint) {
			cp.Items[0].Quality = QualityAutoAccepted
		}},
		{"revision without history entry", func(cp *Checkpoint) {
			cp.Items[0].Revisions = nil
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cp := edited()
			tc.mut(cp)
			if err := cp.validate(); err == nil {
				t.Fatal("validate succeeded, want error")
			}
		})
	}
}

func TestValidateEvidenceIndex(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*Checkpoint)
	}{
		{"referenced but not indexed", func(cp *Checkpoint) {
			cp.Evidence = cp.Evidence[:3]
		}},
		{"indexed but unreferenced", func(cp *Checkpoint) {
			cp.Items[1].EvidenceIDs = nil
		}},
		{"duplicate index entry", func(cp *Checkpoint) {
			cp.Evidence = append(cp.Evidence, cp.Evidence[0])
		}},
		{"zero byte length", func(cp *Checkpoint) { cp.Evidence[0].ByteLength = 0 }},
		{"malformed digest", func(cp *Checkpoint) { cp.Evidence[0].SHA256 = "digest" }},
		{"escaping index id", func(cp *Checkpoint) {
			cp.Evidence[0].ID = `..\escape`
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cp := validDoc()
			tc.mut(cp)
			if err := cp.validate(); err == nil {
				t.Fatal("validate succeeded, want error")
			}
		})
	}
}
