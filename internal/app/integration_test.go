package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/ocrlayout"
	"github.com/LoTwT/youkai/internal/recognition"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/LoTwT/youkai/internal/scan"
)

// The integration test runs the real recognizer over the real
// development data source — the same fixed combination the recognition
// package verifies — through the whole application service: start, save,
// reopen and quit. It skips when the fetchdeps artifacts are missing,
// exactly like the recognition package's own integration tests; run
// `go run ./tools/fetchdeps -platform current` first.

// integrationResourcePaths resolves the OCR artifacts in their raw
// repository layout (resources/<os>-<arch>/…, as fetchdeps writes them),
// mirroring the recognition package's helper. The application itself
// resolves the merged layout mygo dev and packaging produce; this test
// builds its recognizer factory directly so it runs from a plain
// `go test` without staging resources.
func integrationResourcePaths(t *testing.T) (libPath, modelPath string, ok bool) {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("resolving repository root: %v", err)
	}
	libRel := ocrlayout.RuntimeDest(runtime.GOOS, runtime.GOARCH)
	if libRel == "" {
		t.Skipf("no pinned ONNX runtime artifact for %s/%s in this repository", runtime.GOOS, runtime.GOARCH)
	}
	libPath = filepath.Join(root, "resources", filepath.FromSlash(libRel))
	modelPath = filepath.Join(root, "resources", filepath.FromSlash(ocrlayout.ModelDest))
	if _, err := os.Stat(libPath); err != nil {
		t.Skipf("ONNX runtime library missing (%s); run go run ./tools/fetchdeps -platform current: %v", libPath, err)
	}
	if _, err := os.Stat(modelPath); err != nil {
		t.Skipf("recognition model missing (%s); run go run ./tools/fetchdeps: %v", modelPath, err)
	}
	return libPath, modelPath, true
}

// integrationRecognizerFactory lazily creates the real recognizer over
// the raw repository artifacts; the service calls it on the first
// accepted start.
func integrationRecognizerFactory(t *testing.T, libPath, modelPath string) func() (Recognizer, error) {
	t.Helper()
	return func() (Recognizer, error) {
		rec, err := recognition.New(recognition.Resources{LibraryPath: libPath, ModelPath: modelPath})
		if err != nil {
			return nil, err
		}
		return &lineRecognizer{rec: rec}, nil
	}
}

func TestIntegrationApplicationScan(t *testing.T) {
	libPath, modelPath, ok := integrationResourcePaths(t)
	if !ok {
		t.Fatal("unreachable")
	}

	t.Run("FullScanSavesAndReloadsConsistently", func(t *testing.T) {
		src, err := LoadDevScanSource(recognitionFixtureDir)
		if err != nil {
			t.Fatalf("LoadDevScanSource: %v", err)
		}
		src.Pace = 0
		paths := newTestPaths(t)
		svc, _ := newTestService(t, paths, src, integrationRecognizerFactory(t, libPath, modelPath))

		out, err := svc.Start(StartRequest{ID: "integration"})
		if err != nil {
			t.Fatalf("start: %v", err)
		}
		snap := waitNotBusy(t, svc)
		if snap.Scan.End != scan.EndCompleted || snap.Scan.Counts.Captured != 6 || snap.Scan.Counts.Produced != 6 {
			t.Fatalf("snapshot = end %s counts %+v, want completed with 6 produced discs", snap.Scan.End, snap.Scan.Counts)
		}

		rec, err := svc.LoadRecord(string(out.SessionID))
		if err != nil {
			t.Fatalf("LoadRecord: %v", err)
		}
		if len(rec.Checkpoint.Items) != 6 {
			t.Fatalf("record has %d items, want 6", len(rec.Checkpoint.Items))
		}
		if len(rec.Checkpoint.Evidence) != 42 {
			t.Fatalf("record indexes %d evidence files, want the 42 line crops", len(rec.Checkpoint.Evidence))
		}
		for i, item := range rec.Checkpoint.Items {
			if item.Status != record.StatusProduced {
				t.Fatalf("item %d = %s, want produced", i+1, item.Status)
			}
			var result RecognitionResult
			if err := json.Unmarshal(item.Data, &result); err != nil {
				t.Fatalf("item %d data: %v", i+1, err)
			}
			if len(result.Lines) != 7 {
				t.Fatalf("item %d has %d recognized lines, want 7", i+1, len(result.Lines))
			}
			if result.Lines[0].Role != "name" || result.Lines[0].Text == "" {
				t.Fatalf("item %d name line = %+v, want non-empty recognized text", i+1, result.Lines[0])
			}
		}
		if rec.Checkpoint.Session.Termination == nil ||
			rec.Checkpoint.Session.Termination.Kind != record.TerminationCompleted {
			t.Fatalf("termination = %+v, want completed", rec.Checkpoint.Session.Termination)
		}

		// A fresh store on the same root reloads the same result, the
		// way a restarted process reads the record back.
		reopened, err := record.Open(paths.Data)
		if err != nil {
			t.Fatalf("reopen store: %v", err)
		}
		again, err := reopened.Load(string(out.SessionID))
		if err != nil {
			t.Fatalf("reload: %v", err)
		}
		if !reflect.DeepEqual(rec.Checkpoint.Items, again.Checkpoint.Items) {
			t.Fatalf("reloaded items differ from the first load")
		}

		if err := svc.Quit(); err != nil {
			t.Fatalf("quit: %v", err)
		}
	})

	t.Run("StopMidwayKeepsPartialResults", func(t *testing.T) {
		src, err := LoadDevScanSource(recognitionFixtureDir)
		if err != nil {
			t.Fatalf("LoadDevScanSource: %v", err)
		}
		// The pace leaves a wide, quiet gap after the first disc: the
		// stop that follows the first captured item lands before the
		// second one admits.
		src.Pace = 5 * time.Second
		paths := newTestPaths(t)
		svc, _ := newTestService(t, paths, src, integrationRecognizerFactory(t, libPath, modelPath))

		if _, err := svc.Start(StartRequest{ID: "integration-stop"}); err != nil {
			t.Fatalf("start: %v", err)
		}
		waitFor(t, 30*time.Second, "the first captured disc", func() bool {
			return svc.Snapshot().Scan.Counts.Captured >= 1
		})
		svc.Stop()
		snap := waitNotBusy(t, svc)
		if snap.Scan.End != scan.EndCancelled {
			t.Fatalf("end = %s, want cancelled", snap.Scan.End)
		}
		if snap.Scan.Counts.Captured != 1 {
			t.Fatalf("captured %d discs before the stop took effect, want 1 (5s pace)", snap.Scan.Counts.Captured)
		}
		c := snap.Scan.Counts
		if c.Sum() != c.Captured {
			t.Fatalf("counts do not reconcile: %+v", c)
		}
		if c.Produced+c.Unprocessed != c.Captured {
			t.Fatalf("captured discs split into %d produced and %d unprocessed of %d captured",
				c.Produced, c.Unprocessed, c.Captured)
		}

		records, err := svc.ListRecords()
		if err != nil || len(records) != 1 {
			t.Fatalf("records = %+v err %v, want the one session", records, err)
		}
		rec, err := svc.LoadRecord(records[0].ID)
		if err != nil {
			t.Fatalf("LoadRecord: %v", err)
		}
		if len(rec.Checkpoint.Items) != c.Captured {
			t.Fatalf("record has %d items, kernel captured %d", len(rec.Checkpoint.Items), c.Captured)
		}
		item := rec.Checkpoint.Items[0]
		switch item.Status {
		case record.StatusProduced:
			// The result payload is alongside the evidence.
		case record.StatusUnprocessed:
			// The stop terminated the in-flight recognition; the
			// observation is accounted for as unprocessed.
		default:
			t.Fatalf("item status = %s, want produced or unprocessed", item.Status)
		}
		// Evidence is written at admission, so it is on disk whatever
		// the terminal state turned out to be.
		if len(item.EvidenceIDs) != 7 {
			t.Fatalf("item carries %d evidence files, want its 7 crops persisted at admission", len(item.EvidenceIDs))
		}
		if rec.Checkpoint.Session.Termination == nil ||
			rec.Checkpoint.Session.Termination.Kind != record.TerminationCancelled {
			t.Fatalf("termination = %+v, want cancelled", rec.Checkpoint.Session.Termination)
		}

		if err := svc.Quit(); err != nil {
			t.Fatalf("quit: %v", err)
		}
	})
}
