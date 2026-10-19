package collect_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/game/zzz/collect"
	"github.com/LoTwT/youkai/internal/platform"
	"github.com/LoTwT/youkai/internal/platform/platformfake"
	"github.com/LoTwT/youkai/internal/scan"
)

// Collect controller tests (C-series) over the scriptable fake port
// and the real scan kernel: the lifecycle rules PR 4a adds — preflight
// gating, open failure codes, the preparation wait budget, the
// observe-only collection, the hotkey-to-stop wiring and the release.

// harness is one kernel session around a collect.Controller.
type harness struct {
	t       *testing.T
	port    *platformfake.Port
	session *platformfake.Session
	stops   int
	sink    *recordSink
	kernel  *scan.Session
}

func newHarness(t *testing.T, sess *platformfake.Session) *harness {
	t.Helper()
	h := &harness{t: t, port: &platformfake.Port{}, session: sess, sink: &recordSink{}}
	h.port.NewSession = func() *platformfake.Session { return sess }
	return h
}

func (h *harness) start() {
	h.t.Helper()
	ctrl := &collect.Controller{
		Port:         h.port,
		Target:       platform.TargetSpec{BundleID: "com.example.game"},
		Hotkey:       platform.HotkeySpec{Key: platform.KeyS, Ctrl: true, Alt: true, Meta: true},
		Traversal:    func() collect.Traversal { return collect.ObserveOnly{} },
		Stop:         func() { h.stops++ },
		ObserveEvery: 5 * time.Millisecond,
	}
	kernel, err := scan.Start(scan.Config{
		ID:         scan.ID("collect-test"),
		Capacity:   2,
		Workers:    1,
		Controller: ctrl,
		Recognizer: waitRecognizer{},
		Deliverer:  h.sink,
	})
	if err != nil {
		h.t.Fatalf("scan.Start: %v", err)
	}
	h.kernel = kernel
}

// end waits for the session to seal and returns the snapshot.
func (h *harness) end() scan.Snapshot {
	h.t.Helper()
	select {
	case <-h.kernel.Done():
	case <-time.After(5 * time.Second):
		h.t.Fatal("timed out waiting for the session to end")
	}
	return h.kernel.Snapshot()
}

// waitUntil polls cond with a timeout.
func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// waitRecognizer never resolves on its own; the observe-only session
// has nothing to recognize anyway.
type waitRecognizer struct{}

func (waitRecognizer) Recognize(ctx context.Context, task scan.Task, submit scan.Submit) error {
	<-ctx.Done()
	return nil
}

type recordSink struct {
	mu      sync.Mutex
	records []scan.Record
}

func (s *recordSink) Deliver(rec scan.Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, rec)
	return nil
}

func (s *recordSink) len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.records)
}

// recordsSnapshot returns the terminal records collected so far.
func (s *recordSink) recordsSnapshot() []scan.Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]scan.Record(nil), s.records...)
}

// readySession is the standard ready fake session.
func readySession() *platformfake.Session { return platformfake.NewSession() }

// notReady returns an observation carrying the given premise fault.
func notReady(f *platform.Fault) (platform.Observation, error) {
	return platform.Observation{At: time.Now(), Problems: []*platform.Fault{f}}, nil
}

// C1: the preparation order runs Preflight → Open → Observe… →
// LockBaseline → Precheck, and none of it dispatches input.
func TestPrepareOrderAndNoDispatch(t *testing.T) {
	sess := readySession()
	h := newHarness(t, sess)
	h.start()
	// The observe-only traversal holds the session in capturing until a
	// stop; the preparation completed by then.
	waitUntil(t, "capturing", func() bool {
		return h.kernel.Snapshot().Phase == scan.PhaseCapturing
	})
	if ops := sess.DispatchedOps(); len(ops) != 0 {
		t.Fatalf("Prepare must never dispatch: %+v", ops)
	}
	h.kernel.Cancel()
	snap := h.end()
	if snap.Phase != scan.PhaseEnded {
		t.Fatalf("phase %s", snap.Phase)
	}
	if h.port.OpenCount() != 1 {
		t.Fatalf("Open calls: %d", h.port.OpenCount())
	}
	observe, lock, _, _, _ := sess.Counters()
	if observe == 0 || lock != 1 {
		t.Fatalf("prepare counters: %s", sess)
	}
	if ops := sess.DispatchedOps(); len(ops) != 0 {
		t.Fatalf("Prepare must never dispatch: %+v", ops)
	}
	// The traversal precheck ran after the baseline: the session's lock
	// happened before Collect began — proven by the kernel reaching the
	// capturing phase.
	if snap.Counts.Captured != 0 {
		t.Fatalf("the observe-only traversal admits nothing: %+v", snap.Counts)
	}
	if h.sink.len() != 0 {
		t.Fatalf("no records expected: %d", h.sink.len())
	}
}

// C1b: a user-started scan brings the target forward exactly once before
// waiting for readiness; a failed activation is only logged and the
// readiness wait still gives the user the preparation budget.
func TestPrepareActivatesTargetOnce(t *testing.T) {
	sess := readySession()
	h := newHarness(t, sess)
	h.start()
	waitUntil(t, "capturing", func() bool {
		return h.kernel.Snapshot().Phase == scan.PhaseCapturing
	})
	if got := sess.Activations(); got != 1 {
		t.Fatalf("Activate calls: %d, want 1", got)
	}
	h.kernel.Cancel()
	h.end()
}

func TestPrepareActivationFailureFallsBackToReadinessWait(t *testing.T) {
	sess := readySession()
	sess.ActivateFn = func() error { return errors.New("activation refused") }
	h := newHarness(t, sess)
	h.start()
	waitUntil(t, "capturing", func() bool {
		return h.kernel.Snapshot().Phase == scan.PhaseCapturing
	})
	if got := sess.Activations(); got != 1 {
		t.Fatalf("Activate calls: %d, want 1", got)
	}
	h.kernel.Cancel()
	h.end()
}

// C2: a failing preflight never opens the port.
func TestPreflightFailureSkipsOpen(t *testing.T) {
	sess := readySession()
	h := newHarness(t, sess)
	h.port.FailingCheck = "fakeCapability"
	h.start()
	snap := h.end()

	if snap.End != scan.EndFailed || snap.Trigger == nil || snap.Trigger.Code != platform.CodePermissionMissing {
		t.Fatalf("want failed permissionMissing, got %+v", snap)
	}
	if h.port.OpenCount() != 0 {
		t.Fatalf("Open must not run after a failed preflight, ran %d", h.port.OpenCount())
	}
	if _, _, _, _, close := sess.Counters(); close != 0 {
		t.Fatalf("no session exists to close, closed %d", close)
	}
}

// C3: an Open failure's diagnostic code survives into the session.
func TestOpenFailureCarriesCode(t *testing.T) {
	sess := readySession()
	h := newHarness(t, sess)
	h.port.OpenFault = platform.NewFault(platform.CodeTargetNotFound, "no window")
	h.start()
	snap := h.end()

	if snap.End != scan.EndFailed || snap.Trigger == nil || snap.Trigger.Code != platform.CodeTargetNotFound {
		t.Fatalf("want failed targetNotFound, got %+v", snap)
	}
	// The controller released: nothing to close, but Release ran.
	if !sess.IsClosed() && h.port.OpenCount() != 1 {
		t.Fatalf("open calls %d", h.port.OpenCount())
	}
}

// kernelSession starts a real kernel around ctrl and waits for the end.
func kernelSession(t *testing.T, ctrl *collect.Controller) scan.Snapshot {
	t.Helper()
	kernel, err := scan.Start(scan.Config{
		ID:         scan.ID("collect-kernel"),
		Capacity:   2,
		Workers:    1,
		Controller: ctrl,
		Recognizer: waitRecognizer{},
		Deliverer:  &recordSink{},
	})
	if err != nil {
		t.Fatalf("scan.Start: %v", err)
	}
	select {
	case <-kernel.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the session to end")
	}
	return kernel.Snapshot()
}

// C4 (real): the budget expires with prepareTimeout and the session
// closes on release.
func TestPrepareTimeoutRealKernel(t *testing.T) {
	sess := readySession()
	sess.ObserveFn = func() (platform.Observation, error) {
		return notReady(platform.NewFault(platform.CodeFocusLost, "another app is frontmost"))
	}
	port := &platformfake.Port{NewSession: func() *platformfake.Session { return sess }}
	ctrl := &collect.Controller{
		Port:          port,
		Target:        platform.TargetSpec{BundleID: "com.example.game"},
		Hotkey:        platform.HotkeySpec{Key: platform.KeyS},
		PrepareBudget: 60 * time.Millisecond,
		ObserveEvery:  5 * time.Millisecond,
	}
	snap := kernelSession(t, ctrl)

	if snap.End != scan.EndFailed || snap.Trigger == nil || snap.Trigger.Code != collect.CodePrepareTimeout {
		t.Fatalf("want failed prepareTimeout, got %+v", snap)
	}
	if !strings.Contains(snap.Trigger.Message, "focusLost") {
		t.Fatalf("the timeout must carry the last observed problem: %q", snap.Trigger.Message)
	}
	waitUntil(t, "session closed", sess.IsClosed)
}

// C5: a broken problem during the wait fails immediately.
func TestTargetLostFailsPrepare(t *testing.T) {
	sess := readySession()
	sess.ObserveFn = func() (platform.Observation, error) {
		return notReady(platform.NewFault(platform.CodeTargetLost, "window gone"))
	}
	port := &platformfake.Port{NewSession: func() *platformfake.Session { return sess }}
	ctrl := &collect.Controller{
		Port: port, Target: platform.TargetSpec{BundleID: "com.example.game"},
		Hotkey: platform.HotkeySpec{Key: platform.KeyS},
	}
	snap := kernelSession(t, ctrl)
	if snap.End != scan.EndFailed || snap.Trigger == nil || snap.Trigger.Code != platform.CodeTargetLost {
		t.Fatalf("want failed targetLost, got %+v", snap)
	}
	waitUntil(t, "session closed", sess.IsClosed)
}

// C6: a cancel during the wait returns promptly and the session ends
// cancelled.
func TestCancelDuringPrepareReturnsPromptly(t *testing.T) {
	sess := readySession()
	sess.ObserveFn = func() (platform.Observation, error) {
		return notReady(platform.NewFault(platform.CodeFocusLost, "waiting for focus"))
	}
	port := &platformfake.Port{NewSession: func() *platformfake.Session { return sess }}
	ctrl := &collect.Controller{
		Port: port, Target: platform.TargetSpec{BundleID: "com.example.game"},
		Hotkey: platform.HotkeySpec{Key: platform.KeyS},
	}
	kernel, err := scan.Start(scan.Config{
		ID: scan.ID("collect-cancel"), Capacity: 2, Workers: 1,
		Controller: ctrl, Recognizer: waitRecognizer{}, Deliverer: &recordSink{},
	})
	if err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "observations flowing", func() bool {
		observe, _, _, _, _ := sess.Counters()
		return observe >= 2
	})
	started := time.Now()
	kernel.Cancel()
	select {
	case <-kernel.Done():
	case <-time.After(time.Second):
		t.Fatal("the cancel must unwind promptly")
	}
	if d := time.Since(started); d > 500*time.Millisecond {
		t.Fatalf("the cancel took %s", d)
	}
	snap := kernel.Snapshot()
	if snap.End != scan.EndCancelled {
		t.Fatalf("end %s, want cancelled", snap.End)
	}
	waitUntil(t, "session closed", sess.IsClosed)
}

// C7: against the real kernel, a watcher fault during the observe-only
// collection fails the session with the fault's code.
func TestWatcherFaultFailsObserveOnlySession(t *testing.T) {
	sess := readySession()
	port := &platformfake.Port{NewSession: func() *platformfake.Session { return sess }}
	var stops int
	ctrl := &collect.Controller{
		Port: port, Target: platform.TargetSpec{BundleID: "com.example.game"},
		Hotkey:    platform.HotkeySpec{Key: platform.KeyS},
		Traversal: func() collect.Traversal { return collect.ObserveOnly{} },
		Stop:      func() { stops++ },
	}
	kernel, err := scan.Start(scan.Config{
		ID: scan.ID("collect-fault"), Capacity: 2, Workers: 1,
		Controller: ctrl, Recognizer: waitRecognizer{}, Deliverer: &recordSink{},
	})
	if err != nil {
		t.Fatal(err)
	}
	// The session reaches the capturing phase (ObserveOnly waits).
	waitUntil(t, "capturing", func() bool {
		return kernel.Snapshot().Phase == scan.PhaseCapturing
	})
	sess.TriggerFault(platform.NewFault(platform.CodeFocusLost, "the user switched away"))
	select {
	case <-kernel.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("the fault must end the session")
	}
	snap := kernel.Snapshot()
	if snap.End != scan.EndFailed || snap.Trigger == nil || snap.Trigger.Code != platform.CodeFocusLost {
		t.Fatalf("want failed focusLost, got %+v", snap)
	}
	if stops != 0 {
		t.Fatalf("a fault is not a hotkey trigger, stops=%d", stops)
	}
	waitUntil(t, "session closed", sess.IsClosed)
}

// C8: the hotkey triggers the stop entry while the session lives, and a
// late trigger after the release is ignored.
func TestHotkeyTriggersStopLateIgnored(t *testing.T) {
	sess := readySession()
	port := &platformfake.Port{NewSession: func() *platformfake.Session { return sess }}
	var mu sync.Mutex
	stops := 0
	var kernel *scan.Session
	ctrl := &collect.Controller{
		Port: port, Target: platform.TargetSpec{BundleID: "com.example.game"},
		Hotkey:    platform.HotkeySpec{Key: platform.KeyS},
		Traversal: func() collect.Traversal { return collect.ObserveOnly{} },
		// The stop entry is the service's Stop in production; here it
		// records and cancels the kernel, which is what the real entry
		// does.
		Stop: func() {
			mu.Lock()
			stops++
			k := kernel
			mu.Unlock()
			if k != nil {
				k.Cancel()
			}
		},
	}
	started, err := scan.Start(scan.Config{
		ID: scan.ID("collect-hotkey"), Capacity: 2, Workers: 1,
		Controller: ctrl, Recognizer: waitRecognizer{}, Deliverer: &recordSink{},
	})
	if err != nil {
		t.Fatal(err)
	}
	kernel = started
	waitUntil(t, "capturing", func() bool {
		return kernel.Snapshot().Phase == scan.PhaseCapturing
	})
	sess.TriggerHotkey()
	waitUntil(t, "stop entered", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return stops == 1
	})
	select {
	case <-kernel.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("the stop must end the session")
	}
	if snap := kernel.Snapshot(); snap.End != scan.EndCancelled {
		t.Fatalf("end %s, want cancelled", snap.End)
	}
	waitUntil(t, "session closed", sess.IsClosed)
	// A late hotkey trigger — after the release — must not reach the
	// stop entry, or it would cut the next session.
	if cb := sess.HotkeyCallback(); cb != nil {
		cb()
	}
	mu.Lock()
	defer mu.Unlock()
	if stops != 1 {
		t.Fatalf("a late hotkey trigger reached the stop entry, stops=%d", stops)
	}
}

// C9: Release runs exactly once and passes the close report's error
// through.
func TestReleaseOnceAndErrorPassthrough(t *testing.T) {
	sess := readySession()
	sess.CloseFault = errors.New("teardown timeout")
	sess.ObserveFn = func() (platform.Observation, error) {
		return notReady(platform.NewFault(platform.CodeTargetLost, "gone"))
	}
	port := &platformfake.Port{NewSession: func() *platformfake.Session { return sess }}
	ctrl := &collect.Controller{
		Port: port, Target: platform.TargetSpec{BundleID: "com.example.game"},
		Hotkey: platform.HotkeySpec{Key: platform.KeyS},
	}
	snap := kernelSession(t, ctrl)
	if snap.End != scan.EndFailed || snap.Trigger == nil || snap.Trigger.Code != platform.CodeTargetLost {
		t.Fatalf("the release error must not mask the original fault: %+v", snap)
	}
	// The kernel recorded the release failure as a fault.
	found := false
	for _, f := range snap.Faults {
		if f.Code == scan.CodeReleaseFailed {
			found = true
		}
	}
	if !found {
		t.Fatalf("releaseFailed fault missing: %+v", snap.Faults)
	}
	if _, _, _, _, closeCalls := sess.Counters(); closeCalls != 1 {
		t.Fatalf("close calls: %d, want 1", closeCalls)
	}
}

// C10: the controller relays the traversal's observed scope (T5 §3)
// once — after a successful precheck, before Collect — and only when
// the traversal both implements the capability and has a report; a
// failed precheck never relays.
type scopeReportingTraversal struct {
	scope       json.RawMessage
	precheckErr error
	events      []string
	mu          sync.Mutex
}

func (t *scopeReportingTraversal) Precheck(ctx context.Context, sess platform.Session) error {
	t.mu.Lock()
	t.events = append(t.events, "precheck")
	err := t.precheckErr
	t.mu.Unlock()
	return err
}

func (t *scopeReportingTraversal) Collect(ctx context.Context, sess platform.Session, h *scan.ControllerHandle) error {
	t.mu.Lock()
	t.events = append(t.events, "collect")
	t.mu.Unlock()
	return nil
}

func (t *scopeReportingTraversal) ObservedScope() json.RawMessage {
	t.mu.Lock()
	t.events = append(t.events, "observedScope")
	s := t.scope
	t.mu.Unlock()
	return s
}

func (t *scopeReportingTraversal) snapshot() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.events...)
}

func TestScopeRelayAfterPrecheck(t *testing.T) {
	payload := json.RawMessage(`{"kind":"zzzInventory","page":"warehouse"}`)
	trav := &scopeReportingTraversal{scope: payload}
	sess := readySession()
	port := &platformfake.Port{NewSession: func() *platformfake.Session { return sess }}
	var mu sync.Mutex
	var relayed []json.RawMessage
	ctrl := &collect.Controller{
		Port: port, Target: platform.TargetSpec{BundleID: "com.example.game"},
		Hotkey:    platform.HotkeySpec{Key: platform.KeyS},
		Traversal: func() collect.Traversal { return trav },
	}
	ctrl.SetScopeSink(func(scope json.RawMessage) {
		mu.Lock()
		defer mu.Unlock()
		relayed = append(relayed, append(json.RawMessage(nil), scope...))
	})
	snap := kernelSession(t, ctrl)
	if snap.End != scan.EndCompleted {
		t.Fatalf("end %s, want completed", snap.End)
	}
	if len(relayed) != 1 || string(relayed[0]) != string(payload) {
		t.Fatalf("relayed %q, want exactly the traversal's report", relayed)
	}
	// The relay sits between the precheck and the collection, on the
	// producer goroutine.
	if got, want := trav.snapshot(), []string{"precheck", "observedScope", "collect"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("event order %v, want %v", got, want)
	}
}

func TestScopeRelayNegativePaths(t *testing.T) {
	// A traversal without the ScopeReporter capability never fires the
	// sink; the observe-only session holds until cancelled.
	t.Run("no capability", func(t *testing.T) {
		sess := readySession()
		port := &platformfake.Port{NewSession: func() *platformfake.Session { return sess }}
		var mu sync.Mutex
		relayed := 0
		ctrl := &collect.Controller{
			Port: port, Target: platform.TargetSpec{BundleID: "com.example.game"},
			Hotkey:    platform.HotkeySpec{Key: platform.KeyS},
			Traversal: func() collect.Traversal { return collect.ObserveOnly{} },
		}
		ctrl.SetScopeSink(func(json.RawMessage) {
			mu.Lock()
			defer mu.Unlock()
			relayed++
		})
		kernel, err := scan.Start(scan.Config{
			ID: scan.ID("collect-scope-none"), Capacity: 2, Workers: 1,
			Controller: ctrl, Recognizer: waitRecognizer{}, Deliverer: &recordSink{},
		})
		if err != nil {
			t.Fatal(err)
		}
		waitUntil(t, "capturing", func() bool {
			return kernel.Snapshot().Phase == scan.PhaseCapturing
		})
		kernel.Cancel()
		select {
		case <-kernel.Done():
		case <-time.After(3 * time.Second):
			t.Fatal("the cancel must end the session")
		}
		mu.Lock()
		defer mu.Unlock()
		if relayed != 0 {
			t.Fatalf("a traversal without the capability must not relay, fired %d times", relayed)
		}
	})

	// A failed precheck never relays, and an empty report is skipped.
	t.Run("failed precheck and empty report", func(t *testing.T) {
		for _, tc := range []struct {
			name  string
			trav  *scopeReportingTraversal
			fails bool
		}{
			{"failed precheck", &scopeReportingTraversal{
				scope:       json.RawMessage(`{"kind":"zzzInventory"}`),
				precheckErr: errors.New("the page does not verify"),
			}, true},
			{"empty report", &scopeReportingTraversal{}, false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				port := &platformfake.Port{NewSession: func() *platformfake.Session { return readySession() }}
				var mu sync.Mutex
				relayed := 0
				ctrl := &collect.Controller{
					Port: port, Target: platform.TargetSpec{BundleID: "com.example.game"},
					Hotkey:    platform.HotkeySpec{Key: platform.KeyS},
					Traversal: func() collect.Traversal { return tc.trav },
				}
				ctrl.SetScopeSink(func(json.RawMessage) {
					mu.Lock()
					defer mu.Unlock()
					relayed++
				})
				snap := kernelSession(t, ctrl)
				if tc.fails && snap.End != scan.EndFailed {
					t.Fatalf("end %s, want failed", snap.End)
				}
				mu.Lock()
				defer mu.Unlock()
				if relayed != 0 {
					t.Fatalf("the sink must not fire, fired %d times", relayed)
				}
			})
		}
	})
}
