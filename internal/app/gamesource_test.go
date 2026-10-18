package app

import (
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/game/zzz/collect"
	"github.com/LoTwT/youkai/internal/platform"
	"github.com/LoTwT/youkai/internal/platform/platformfake"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/LoTwT/youkai/internal/scan"
)

// Game scan source tests (A-series): the observe-only rehearsal wired
// end to end through the real application service and scan kernel, with
// the scriptable fake port standing in for the platform.

// gameService wires a GameScanSource over a fake port and returns the
// service, its data-root lock (for scan-occupancy checks) and the port.
func gameService(t *testing.T, sess *platformfake.Session) (*Service, *DataRootLock, *platformfake.Port) {
	t.Helper()
	port := &platformfake.Port{NewSession: func() *platformfake.Session { return sess }}
	source := &GameScanSource{
		Port:   port,
		Target: collect.MacCloudTarget,
		Hotkey: platform.HotkeySpec{Key: platform.KeyS, Ctrl: true, Alt: true, Meta: true},
		Client: record.ClientMacOSCloud,
	}
	svc, dataLock := newTestService(t, newTestPaths(t), source, func() (Recognizer, error) {
		return newFakeRecognizer(), nil
	})
	return svc, dataLock, port
}

// waitSnapshot polls for the session to reach the wanted phase and
// returns the kernel snapshot at that point.
func waitSnapshot(t *testing.T, svc *Service, want scan.Phase, what string) scan.Snapshot {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		kernelSnap := svc.Snapshot().Scan
		if kernelSnap.Phase == want {
			return kernelSnap
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s (phase %s)", what, kernelSnap.Phase)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// waitWrapped polls until the active slot freed.
func waitWrapped(t *testing.T, svc *Service) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for svc.Snapshot().Busy {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for the session to wrap")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// A1: the source's spec validates, and one without a client is refused
// at validation time rather than mid-scan.
func TestGameScanSourceSpecValidates(t *testing.T) {
	source := &GameScanSource{
		Port:   &platformfake.Port{},
		Target: collect.MacCloudTarget,
		Hotkey: platform.HotkeySpec{Key: platform.KeyS},
		Client: record.ClientMacOSCloud,
	}
	spec, err := source.NewScan()
	if err != nil {
		t.Fatalf("NewScan: %v", err)
	}
	if err := spec.validate(); err != nil {
		t.Fatalf("spec must validate: %v", err)
	}

	source.Client = ""
	spec, err = source.NewScan()
	if err != nil {
		t.Fatal(err)
	}
	if err := spec.validate(); err == nil {
		t.Fatal("a spec without a client must be rejected")
	}
}

// A2: the observe-only rehearsal end to end — start, capture, hotkey
// stop, cancelled end, released scan occupancy, and the platform
// session closed before the wrap reported completion.
func TestGameScanSourceObserveOnlyEndToEnd(t *testing.T) {
	sess := platformfake.NewSession()
	svc, dataLock, port := gameService(t, sess)

	if _, err := svc.Start(StartRequest{ID: "game-1"}); err != nil {
		t.Fatalf("start: %v", err)
	}
	snap := waitSnapshot(t, svc, scan.PhaseCapturing, "the observe-only session to capture")
	if !snap.InputPermitted {
		t.Fatalf("input permission must be open in capturing: %+v", snap)
	}
	if ops := sess.DispatchedOps(); len(ops) != 0 {
		t.Fatalf("the observe-only rehearsal dispatches nothing: %+v", ops)
	}

	// The stop hotkey fires: the service's stop runs and the session
	// unwinds.
	sess.TriggerHotkey()
	waitWrapped(t, svc)

	final := svc.Snapshot().Scan
	if final.End != scan.EndCancelled {
		t.Fatalf("end %s, want cancelled", final.End)
	}
	if final.Trigger == nil || final.Trigger.Kind != scan.TriggerUser {
		t.Fatalf("the hotkey is a user stop: %+v", final.Trigger)
	}
	// The controller's release ran before the wrap completed: the
	// platform session is closed and the port opened exactly once.
	if !sess.IsClosed() {
		t.Fatal("the platform session must close before the wrap completes")
	}
	if port.OpenCount() != 1 {
		t.Fatalf("open calls: %d", port.OpenCount())
	}
	// The scan occupancy is free again.
	scanLock, err := dataLock.AcquireScanLock()
	if err != nil {
		t.Fatalf("the scan occupancy must release with the wrap: %v", err)
	}
	if err := scanLock.Close(); err != nil {
		t.Fatal(err)
	}
}

// A3: a watcher fault during the rehearsal fails the session with the
// fault's code as the trigger.
func TestGameScanSourceObservationFaultFailsSession(t *testing.T) {
	sess := platformfake.NewSession()
	svc, _, _ := gameService(t, sess)

	if _, err := svc.Start(StartRequest{ID: "game-2"}); err != nil {
		t.Fatal(err)
	}
	waitSnapshot(t, svc, scan.PhaseCapturing, "capturing")
	sess.TriggerFault(platform.NewFault(platform.CodeFocusLost, "the user switched away"))
	waitWrapped(t, svc)

	final := svc.Snapshot().Scan
	if final.End != scan.EndFailed || final.Trigger == nil || final.Trigger.Code != "focusLost" {
		t.Fatalf("want failed focusLost, got %+v", final)
	}
	if !sess.IsClosed() {
		t.Fatal("the fault path must release the platform session too")
	}
}

// A4: a failing preflight fails the session without opening the port or
// dispatching anything, and the slot frees.
func TestGameScanSourcePreflightFailure(t *testing.T) {
	sess := platformfake.NewSession()
	port := &platformfake.Port{
		FailingCheck: "fakeCapability",
		NewSession:   func() *platformfake.Session { return sess },
	}
	source := &GameScanSource{
		Port:   port,
		Target: collect.MacCloudTarget,
		Hotkey: platform.HotkeySpec{Key: platform.KeyS},
		Client: record.ClientMacOSCloud,
	}
	svc, _ := newTestService(t, newTestPaths(t), source, func() (Recognizer, error) {
		return newFakeRecognizer(), nil
	})

	if _, err := svc.Start(StartRequest{ID: "game-3"}); err != nil {
		t.Fatal(err)
	}
	waitWrapped(t, svc)
	final := svc.Snapshot().Scan
	if final.End != scan.EndFailed || final.Trigger == nil || final.Trigger.Code != platform.CodePermissionMissing {
		t.Fatalf("want failed permissionMissing, got %+v", final)
	}
	if port.OpenCount() != 0 {
		t.Fatalf("a failed preflight must not open the port, opened %d", port.OpenCount())
	}
	if ops := sess.DispatchedOps(); len(ops) != 0 {
		t.Fatalf("no input may be dispatched: %+v", ops)
	}
	if svc.Snapshot().Busy {
		t.Fatal("the slot must free after the failed session")
	}
}

// A5: Quit waits for the platform session's close — the dwell included
// — before it returns.
func TestGameScanSourceQuitWaitsForClose(t *testing.T) {
	sess := platformfake.NewSession()
	sess.CloseSleep = 150 * time.Millisecond
	svc, _, _ := gameService(t, sess)

	if _, err := svc.Start(StartRequest{ID: "game-4"}); err != nil {
		t.Fatal(err)
	}
	waitSnapshot(t, svc, scan.PhaseCapturing, "capturing")
	sess.TriggerHotkey()

	started := time.Now()
	if err := svc.Quit(); err != nil {
		t.Fatalf("quit: %v", err)
	}
	if d := time.Since(started); d < 150*time.Millisecond {
		t.Fatalf("quit returned after %s, before the platform close (150ms) settled", d)
	}
	if !sess.IsClosed() {
		t.Fatal("the platform session must be closed by quit")
	}
}

// A6: a stop arriving before the kernel committed walks the
// stopRequested path and is applied at commit. The stop entry the hotkey
// is bound to is the service's Stop, so no stop is lost to the
// preparation window.
func TestGameScanSourceStopBeforeCommitIsKept(t *testing.T) {
	sess := platformfake.NewSession()
	port := &platformfake.Port{NewSession: func() *platformfake.Session { return sess }}
	source := &GameScanSource{
		Port:   port,
		Target: collect.MacCloudTarget,
		Hotkey: platform.HotkeySpec{Key: platform.KeyS},
		Client: record.ClientMacOSCloud,
	}
	// The recognizer factory is the seam that holds the start between
	// the accepted placeholder and the kernel: while it blocks, the
	// kernel does not exist, and the stop entry must record the request.
	entered := make(chan struct{})
	block := make(chan struct{})
	svc, _ := newTestService(t, newTestPaths(t), source, func() (Recognizer, error) {
		close(entered)
		<-block
		return newFakeRecognizer(), nil
	})

	go func() {
		if _, err := svc.Start(StartRequest{ID: "game-5"}); err != nil {
			t.Errorf("start: %v", err)
		}
	}()
	<-entered
	// The start is parked inside the factory: the placeholder exists,
	// the kernel does not. This is the stop the hotkey would deliver.
	svc.Stop()
	close(block)

	waitWrapped(t, svc)
	final := svc.Snapshot().Scan
	if final.End != scan.EndCancelled {
		t.Fatalf("the early stop must be applied at commit, got end %s", final.End)
	}
	if !sess.IsClosed() {
		t.Fatal("the platform session must close through the release")
	}
}

// The bindStop seam stays invisible to sources without it (A7
// companion: the development source's tests keep passing untouched).
func TestGameScanSourceBindStopSeam(t *testing.T) {
	src := &GameScanSource{
		Port:   &platformfake.Port{},
		Target: collect.MacCloudTarget,
		Hotkey: platform.HotkeySpec{Key: platform.KeyS},
		Client: record.ClientMacOSCloud,
	}
	var _ ScanSource = src
	if _, ok := interface{}(src).(interface{ bindStop(func()) }); !ok {
		t.Fatal("the game source must expose the bindStop hook")
	}
	if _, ok := interface{}(staticSource{}).(interface{ bindStop(func()) }); ok {
		t.Fatal("ordinary sources must not grow the hook")
	}
}
