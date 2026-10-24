package app

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/record"
	"github.com/LoTwT/youkai/internal/scan"
)

// The service tests run without any native library: the recognizer is a
// fake, the data root is a temporary directory, and the scan lock base is
// redirected to a temporary directory so nothing touches the real
// per-user lock (the same injection the occupancy-lock tests use).

// waitFor polls cond until it holds or the timeout passes; the timeout is
// a hang guard, not an ordering device.
func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// newTestPaths builds valid service paths around a fresh data root.
func newTestPaths(t *testing.T) Paths {
	t.Helper()
	root := t.TempDir()
	return Paths{
		Data:      root,
		Cache:     filepath.Join(root, "cache"),
		Logs:      filepath.Join(root, "logs"),
		Resources: filepath.Join(root, "resources"),
	}
}

// newTestService assembles a service over a fresh data root with the
// scan-lock base redirected and the given source and recognizer factory.
// An optional shutdown callback replaces the real OCR-environment
// teardown, so a test can observe its position in the exit order.
// It returns the service and the data-root lock it will release at quit.
func newTestService(t *testing.T, paths Paths, source ScanSource, newRecognizer func() (Recognizer, error), shutdownOCR ...func() error) (*Service, *DataRootLock) {
	t.Helper()
	setScanLockBase(t, t.TempDir())
	dataLock, err := AcquireDataRootLock(paths.Data)
	if err != nil {
		t.Fatalf("acquire data-root lock: %v", err)
	}
	t.Cleanup(func() { dataLock.Close() })
	var shutdown func() error
	if len(shutdownOCR) > 0 {
		shutdown = shutdownOCR[0]
	}
	svc, err := NewService(Config{
		Paths:         paths,
		DataLock:      dataLock,
		Source:        source,
		NewRecognizer: newRecognizer,
		ShutdownOCR:   shutdown,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return svc, dataLock
}

// newFakeRecognizer returns a recognizer whose Recognize echoes the
// observation back as the result, honoring cancellation the way the real
// adapter does.
func newFakeRecognizer() Recognizer {
	return &fakeRecognizer{}
}

type fakeRecognizer struct {
	closeCount int
}

func (r *fakeRecognizer) Recognize(ctx context.Context, task scan.Task, submit scan.Submit) error {
	select {
	case <-ctx.Done():
		return nil // terminated; the entry ends unprocessed
	default:
	}
	return submit(task.SessionID, task.Seq, scan.Recognition{Result: task.Observation})
}

func (r *fakeRecognizer) Close() error { r.closeCount++; return nil }

// scriptedRecognizer delegates each recognition to a function the test
// provides; it implements Recognizer with a no-op Close.
type scriptedRecognizer struct {
	recognize func(ctx context.Context, task scan.Task, submit scan.Submit) error
}

func (r *scriptedRecognizer) Recognize(ctx context.Context, task scan.Task, submit scan.Submit) error {
	return r.recognize(ctx, task, submit)
}

func (r *scriptedRecognizer) Close() error { return nil }

// waitNotBusy waits until the active session wrapped (the final save
// done) and returns the snapshot.
func waitNotBusy(t *testing.T, svc *Service) Snapshot {
	t.Helper()
	var snap Snapshot
	waitFor(t, 30*time.Second, "the session to wrap up", func() bool {
		snap = svc.Snapshot()
		return snap.SessionID != "" && !snap.Busy && !snap.SavePending
	})
	return snap
}

// devFixtureSource writes a development data source fixture with discs
// observation groups of one crop each, and returns it with the given
// pace. Tiny generated PNGs stand in for the real crops: the fake
// recognizer never decodes them, and the deliverer only re-encodes the
// pixels.
func devFixtureSource(t *testing.T, discs int, pace time.Duration) *DevScanSource {
	t.Helper()
	dir := t.TempDir()
	img := image.NewNRGBA(image.Rect(0, 0, 4, 2))
	for x := 0; x < 4; x++ {
		img.Set(x, 0, color.NRGBA{R: 0x40, G: uint8(x), B: 0x80, A: 0xff})
	}
	manifest := ""
	for i := 1; i <= discs; i++ {
		name := filepath.Join(dir, fmt.Sprintf("disc%d_main.png", i))
		f, err := os.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		manifest += fmt.Sprintf("disc%d_main.png\t%s\t0\t0\t4\t2\tfixture\n", i, "00"+strings.Repeat("ab", 62))
	}
	if err := os.WriteFile(filepath.Join(dir, "lines.tsv"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	src, err := LoadDevScanSource(dir)
	if err != nil {
		t.Fatalf("LoadDevScanSource: %v", err)
	}
	src.Pace = pace
	return src
}

// staticSource serves one fixed spec for every start.
type staticSource struct {
	spec ScanSpec
	err  error
}

func (s staticSource) NewScan() (ScanSpec, error) { return s.spec, s.err }

// specOf wraps a controller in a valid scan spec with test provenance.
func specOf(controller scan.Controller) ScanSpec {
	return ScanSpec{
		Controller: controller,
		Game:       "zzz",
		Entity:     "driveDisc",
		Client:     record.ClientWindowsLocal,
		Locale:     "zh-CN",
		Scope:      []byte(`{"kind":"test"}`),
	}
}

// gatedController prepares and then blocks in Collect until the gate
// closes or the context ends, holding the session open for as long as
// the test needs.
type gatedController struct {
	gate chan struct{}
}

func newGatedController() *gatedController {
	return &gatedController{gate: make(chan struct{})}
}

func (c *gatedController) Prepare(ctx context.Context, h *scan.ControllerHandle) error {
	return nil
}

func (c *gatedController) Collect(ctx context.Context, h *scan.ControllerHandle) error {
	select {
	case <-c.gate:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *gatedController) release() { close(c.gate) }

// flakyStore wraps a real store and injects failures: the failOn
// predicate sees each SaveCheckpoint's 1-based call index, failEvidence
// each WriteEvidence's; heal disables both from then on.
type flakyStore struct {
	*record.Store

	mu           sync.Mutex
	calls        int
	evidence     int
	failOn       func(call int) bool
	failEvidence func(call int) bool
	healed       bool
}

func (f *flakyStore) SaveCheckpoint(recordID string, cp record.Checkpoint) (int64, error) {
	f.mu.Lock()
	f.calls++
	fail := !f.healed && f.failOn != nil && f.failOn(f.calls)
	f.mu.Unlock()
	if fail {
		return 0, errors.New("flaky store: injected save failure")
	}
	return f.Store.SaveCheckpoint(recordID, cp)
}

func (f *flakyStore) WriteEvidence(recordID, evidenceID string, image []byte) (record.EvidenceFile, error) {
	f.mu.Lock()
	f.evidence++
	fail := !f.healed && f.failEvidence != nil && f.failEvidence(f.evidence)
	f.mu.Unlock()
	if fail {
		return record.EvidenceFile{}, errors.New("flaky store: injected evidence failure")
	}
	return f.Store.WriteEvidence(recordID, evidenceID, image)
}

func (f *flakyStore) heal() {
	f.mu.Lock()
	f.healed = true
	f.mu.Unlock()
}

// recordingRecognizer tracks in-flight Recognize calls and, when wired to
// a service via wire, records the session's phase at Close — the quit
// ordering assertions read both.
type recordingRecognizer struct {
	delegate Recognizer

	mu            sync.Mutex
	inFlight      int
	inFlightAtCls int
	closeCount    int
	phaseAtClose  string
	svc           *Service
}

func (r *recordingRecognizer) Recognize(ctx context.Context, task scan.Task, submit scan.Submit) error {
	r.mu.Lock()
	r.inFlight++
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.inFlight--
		r.mu.Unlock()
	}()
	return r.delegate.Recognize(ctx, task, submit)
}

func (r *recordingRecognizer) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closeCount++
	r.inFlightAtCls = r.inFlight
	r.phaseAtClose = "no-service"
	if r.svc != nil {
		if snap := r.svc.Snapshot(); snap.SessionID != "" {
			r.phaseAtClose = string(snap.Scan.Phase)
		} else {
			r.phaseAtClose = "no-session"
		}
	}
	return r.delegate.Close()
}

// wire lets Close observe the service's session phase.
func (r *recordingRecognizer) wire(svc *Service) { r.svc = svc }
