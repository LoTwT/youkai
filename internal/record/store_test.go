package record

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// newTestStore opens a store over a fresh temporary data root on the real
// filesystem. Fault-injection tests build their own store around faultFS.
func newTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	root := t.TempDir()
	st, err := newStore(root, osFS{})
	if err != nil {
		t.Fatalf("newStore: %v", err)
	}
	return st, root
}

// newRecord creates a record with a fixed id and returns it.
func newRecord(t *testing.T, st *Store) string {
	t.Helper()
	const id = "rec-1"
	if err := st.CreateRecord(id); err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}
	return id
}

// testShell builds a minimal valid session shell: a running session, no
// termination yet, unknown coverage. Tests adjust the fields they care
// about.
func testShell() SessionShell {
	return SessionShell{
		SessionID: "0f0e0d0c-1a2b-3c4d-5e6f-7a8b9c0d1e2f",
		Game:      "zzz",
		Entity:    "driveDisc",
		Client:    ClientMacOSCloud,
		Locale:    "zh-CN",
		Scope:     json.RawMessage(`{"rarity":"S","slots":[1,2,3,4,5,6]}`),
		StartedAt: time.Date(2026, 10, 16, 8, 0, 0, 0, time.UTC),
		Coverage:  Coverage{Status: CoverageUnknown},
	}
}

// saveCheckpoint saves a checkpoint and fails the test on error,
// returning the generation revision it was saved under.
func saveCheckpoint(t *testing.T, st *Store, id string, cp Checkpoint) int64 {
	t.Helper()
	rev, err := st.SaveCheckpoint(id, cp)
	if err != nil {
		t.Fatalf("SaveCheckpoint: %v", err)
	}
	return rev
}

// writeEvidence writes one evidence image and fails the test on error.
func writeEvidence(t *testing.T, st *Store, id, evidenceID string, image []byte) EvidenceFile {
	t.Helper()
	ev, err := st.WriteEvidence(id, evidenceID, image)
	if err != nil {
		t.Fatalf("WriteEvidence(%q): %v", evidenceID, err)
	}
	return ev
}

// generationFiles lists the checkpoint file names present, sorted.
func generationFiles(t *testing.T, root, id string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, "sessions", id, "checkpoints"))
	if err != nil {
		t.Fatalf("ReadDir checkpoints: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	slices.Sort(names)
	return names
}

func TestOpenRejectsBadRoots(t *testing.T) {
	for _, root := range []string{"", "relative/root"} {
		if _, err := Open(root); err == nil {
			t.Errorf("Open(%q) succeeded, want error", root)
		}
	}
}

func TestOpenAcceptsAbsoluteRoot(t *testing.T) {
	root := t.TempDir()
	if _, err := Open(root); err != nil {
		t.Fatalf("Open(%q): %v", root, err)
	}
}

func TestCreateRecordBuildsLayout(t *testing.T) {
	st, root := newTestStore(t)
	if err := st.CreateRecord("rec-1"); err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}
	for _, sub := range []string{"checkpoints", "evidence", "resources"} {
		dir := filepath.Join(root, "sessions", "rec-1", sub)
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			t.Errorf("expected directory %q (stat err %v)", dir, err)
		}
	}
	// The root and sessions parent were created on demand.
	if info, err := os.Stat(filepath.Join(root, "sessions")); err != nil || !info.IsDir() {
		t.Errorf("expected sessions root (stat err %v)", err)
	}
}

func TestCreateRecordRefusesReuseAndBadIDs(t *testing.T) {
	st, _ := newTestStore(t)
	if err := st.CreateRecord("rec-1"); err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}
	if err := st.CreateRecord("rec-1"); !errors.Is(err, ErrRecordExists) {
		t.Errorf("second CreateRecord = %v, want ErrRecordExists", err)
	}
	longID := string(bytes.Repeat([]byte("a"), idMaxLen+1))
	for _, id := range []string{"", "UPPER", "../escape", `back\slash`, "with space", "-lead", "trail-", "con", "a.b", longID} {
		if err := st.CreateRecord(id); !errors.Is(err, ErrInvalidID) {
			t.Errorf("CreateRecord(%q) = %v, want ErrInvalidID", id, err)
		}
	}
}

func TestWriteEvidenceRoundTrip(t *testing.T) {
	st, root := newTestStore(t)
	id := newRecord(t, st)
	image := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 1, 2, 3}
	ev := writeEvidence(t, st, id, "shot-1", image)

	if ev.ID != "shot-1" || ev.ByteLength != int64(len(image)) {
		t.Errorf("descriptor = %+v, want id shot-1 and %d bytes", ev, len(image))
	}
	sum := sha256.Sum256(image)
	if ev.SHA256 != hex.EncodeToString(sum[:]) {
		t.Errorf("digest = %q, want %q", ev.SHA256, hex.EncodeToString(sum[:]))
	}
	got, err := os.ReadFile(filepath.Join(root, "sessions", id, "evidence", "shot-1.png"))
	if err != nil || !bytes.Equal(got, image) {
		t.Errorf("evidence file = %q (err %v), want the exact bytes written", got, err)
	}

	// A duplicate write must not truncate the only copy.
	if _, err := st.WriteEvidence(id, "shot-1", image); !errors.Is(err, ErrEvidenceExists) {
		t.Errorf("duplicate WriteEvidence = %v, want ErrEvidenceExists", err)
	}
	if _, err := st.WriteEvidence(id, "shot-1", nil); err == nil {
		t.Error("empty image accepted, want error")
	}
	if _, err := st.WriteEvidence(id, "../escape", image); !errors.Is(err, ErrInvalidID) {
		t.Errorf("escaping evidence id = %v, want ErrInvalidID", err)
	}
	if _, err := st.WriteEvidence("no-such-record", "shot-2", image); !errors.Is(err, ErrRecordNotFound) {
		t.Errorf("unknown record = %v, want ErrRecordNotFound", err)
	}
}

func TestWriteResourceRoundTrip(t *testing.T) {
	st, root := newTestStore(t)
	id := newRecord(t, st)
	content := json.RawMessage(`{"captureProfile":{"dpi":2}}`)
	if err := st.WriteResource(id, "profile-1", content); err != nil {
		t.Fatalf("WriteResource: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(root, "sessions", id, "resources", "profile-1.json"))
	if err != nil || !bytes.Equal(got, content) {
		t.Errorf("resource file = %q (err %v), want the exact bytes written", got, err)
	}
	if err := st.WriteResource(id, "profile-1", content); !errors.Is(err, ErrResourceExists) {
		t.Errorf("duplicate WriteResource = %v, want ErrResourceExists", err)
	}
	if err := st.WriteResource(id, "profile-2", []byte("not json")); err == nil {
		t.Error("non-JSON resource accepted, want error")
	}
	if err := st.WriteResource(id, "../escape", content); !errors.Is(err, ErrInvalidID) {
		t.Errorf("escaping resource id = %v, want ErrInvalidID", err)
	}
	if err := st.WriteResource("no-such-record", "profile-3", content); !errors.Is(err, ErrRecordNotFound) {
		t.Errorf("unknown record = %v, want ErrRecordNotFound", err)
	}
}

func TestSaveCheckpointWritesStampedGeneration(t *testing.T) {
	st, root := newTestStore(t)
	id := newRecord(t, st)
	shell := testShell()
	rev, err := st.SaveCheckpoint(id, Checkpoint{Session: shell})
	if err != nil {
		t.Fatalf("SaveCheckpoint: %v", err)
	}
	if rev != 1 {
		t.Errorf("revision = %d, want 1", rev)
	}
	data, err := os.ReadFile(filepath.Join(root, "sessions", id, "checkpoints", "1.json"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var doc Checkpoint
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("generation does not decode: %v", err)
	}
	if doc.Format != FormatName || doc.FormatVersion != FormatVersion || doc.SavedRevision != 1 {
		t.Errorf("envelope = %q v%d rev %d, want %q v%d rev 1", doc.Format, doc.FormatVersion, doc.SavedRevision, FormatName, FormatVersion)
	}
	if doc.Session.SessionID != shell.SessionID || doc.Session.Client != ClientMacOSCloud {
		t.Errorf("session shell did not round-trip: %+v", doc.Session)
	}
	if len(doc.Items) != 0 || len(doc.Evidence) != 0 {
		t.Errorf("items/evidence = %d/%d, want empty", len(doc.Items), len(doc.Evidence))
	}
}

func TestSaveCheckpointRetentionKeepsPrevious(t *testing.T) {
	st, root := newTestStore(t)
	id := newRecord(t, st)
	revs := make([]int64, 0, 3)
	for i, status := range []CoverageStatus{CoverageUnknown, CoveragePartial, CoveragePartial} {
		cp := Checkpoint{Session: testShell()}
		cp.Session.Coverage.Status = status
		revs = append(revs, saveCheckpoint(t, st, id, cp))
		if want := int64(i + 1); revs[i] != want {
			t.Fatalf("save %d returned revision %d, want %d", i+1, revs[i], want)
		}
	}
	files := generationFiles(t, root, id)
	if len(files) != 2 || files[0] != "2.json" || files[1] != "3.json" {
		t.Errorf("generations = %v, want [2.json 3.json]", files)
	}
}

func TestSaveCheckpointRequiresEvidenceOnDisk(t *testing.T) {
	st, root := newTestStore(t)
	id := newRecord(t, st)

	// A well-formed index entry for evidence that was never written.
	cp := Checkpoint{
		Session: testShell(),
		Items: []ItemRecord{{
			Sequence:    1,
			Status:      StatusProduced,
			Quality:     QualityNotEvaluated,
			EvidenceIDs: []string{"ghost"},
		}},
		Evidence: []EvidenceFile{{ID: "ghost", ByteLength: 4, SHA256: string(bytes.Repeat([]byte("0"), 64))}},
	}
	cp.Session.CapturedCount = 1
	if _, err := st.SaveCheckpoint(id, cp); err == nil {
		t.Fatal("save referenced missing evidence, want error")
	}
	if files := generationFiles(t, root, id); len(files) != 0 {
		t.Errorf("rejected save left generations %v, want none", files)
	}

	// Written evidence with a mismatching declared size is equally
	// rejected — the checkpoint must describe the bytes on disk.
	writeEvidence(t, st, id, "shot-1", []byte("abcdef"))
	cp.Items[0].EvidenceIDs = []string{"shot-1"}
	cp.Evidence = []EvidenceFile{{ID: "shot-1", ByteLength: 3, SHA256: string(bytes.Repeat([]byte("0"), 64))}}
	if _, err := st.SaveCheckpoint(id, cp); err == nil {
		t.Fatal("save with wrong declared size succeeded, want error")
	}
	if files := generationFiles(t, root, id); len(files) != 0 {
		t.Errorf("rejected save left generations %v, want none", files)
	}
}

func TestSaveCheckpointRejectsInvalidDocuments(t *testing.T) {
	st, root := newTestStore(t)
	id := newRecord(t, st)
	cases := []struct {
		name string
		mut  func(*Checkpoint)
	}{
		{"unknown client", func(cp *Checkpoint) { cp.Session.Client = "webClient" }},
		{"missing scope", func(cp *Checkpoint) { cp.Session.Scope = nil }},
		{"bad termination kind", func(cp *Checkpoint) {
			cp.Session.Termination = &Termination{Kind: "vanished", Code: "x", Phase: PhaseCapturing}
		}},
		{"captured count mismatch", func(cp *Checkpoint) { cp.Session.CapturedCount = 3 }},
		{"item without reason", func(cp *Checkpoint) {
			cp.Items = []ItemRecord{{Sequence: 1, Status: StatusUnprocessed, Quality: QualityNotEvaluated}}
			cp.Session.CapturedCount = 1
		}},
		{"item sequence gap", func(cp *Checkpoint) {
			cp.Items = []ItemRecord{{Sequence: 2, Status: StatusProduced, Quality: QualityNotEvaluated}}
			cp.Session.CapturedCount = 1
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cp := Checkpoint{Session: testShell()}
			tc.mut(&cp)
			if _, err := st.SaveCheckpoint(id, cp); err == nil {
				t.Fatal("SaveCheckpoint succeeded, want error")
			}
			if files := generationFiles(t, root, id); len(files) != 0 {
				t.Errorf("rejected save left generations %v, want none", files)
			}
		})
	}
}
