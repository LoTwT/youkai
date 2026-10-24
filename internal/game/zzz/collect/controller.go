// Package collect is the ZZZ game adapter's session controller: the
// scan.Controller that drives one scan through the unified platform
// port. The controller owns the lifecycle — preflight, open, wait for a
// ready target, lock the geometry baseline, hand over to the traversal
// (see traversal.go) — while the traversal owns T5's navigation and
// admission rules. PR 4a wired the lifecycle with the observe-only
// traversal; Inventory is the calibrated single-page traversal beside it.
//
// The pure ZZZ field semantics (catalog, disc validation) stay in the
// parent game/zzz package, which must not grow this package's platform
// dependency.
package collect

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/LoTwT/youkai/internal/platform"
	"github.com/LoTwT/youkai/internal/scan"
)

// MacCloudTarget is the App Store cloud client's target on macOS, as
// field round ① identified it (bundle com.miHoYo.cloudgames.Nap).
var MacCloudTarget = platform.TargetSpec{BundleID: "com.miHoYo.cloudgames.Nap"}

// CodePrepareTimeout is the controller's own diagnostic code: the
// target did not become ready within the preparation budget.
const CodePrepareTimeout = "prepareTimeout"

// Preparation pacing defaults: the user gets 30 seconds to switch back
// to the game and satisfy every premise; the controller re-observes
// every 200 ms while waiting.
const (
	DefaultPrepareBudget = 30 * time.Second
	DefaultObserveEvery  = 200 * time.Millisecond
)

// Controller is the ZZZ scan.Controller over the unified platform port.
// One Controller serves one session; the application's scan source
// builds a fresh one per start.
type Controller struct {
	// Port is the platform the session runs on.
	Port platform.Port
	// Target and Hotkey configure the session; the hotkey registers in
	// Prepare and stays until Release.
	Target platform.TargetSpec
	Hotkey platform.HotkeySpec
	// Traversal builds the traversal of this session; PR 4a wires
	// func() collect.Traversal { return collect.ObserveOnly{} }.
	Traversal func() Traversal
	// Stop is the application's idempotent stop entry (the service's
	// Stop). The registered hotkey triggers it; a late trigger after
	// Release is ignored so it cannot cut the next session.
	Stop func()
	// OnScopeObserved, when set, receives the observed-scope payload of
	// the session's traversal (the ScopeReporter capability) after its
	// precheck succeeded and again after Collect — the application's
	// record wiring persists it as the session's observed range (T5 §3).
	// It runs on the producer goroutine; a traversal without the
	// capability or with an empty report never triggers it. Wire it via
	// SetScopeSink before the session starts.
	OnScopeObserved func(scope json.RawMessage)

	// PrepareBudget bounds the whole preparation wait; zero takes the
	// default. ObserveEvery paces the wait loop; zero takes the default.
	PrepareBudget time.Duration
	ObserveEvery  time.Duration

	// sess is the open session, set by Prepare and closed by Release.
	// It is written on the producer goroutine (Prepare) and read there
	// (Collect) and by Release (also on the producer, after the
	// controller returned); released guards the cross-phase state.
	mu       sync.Mutex
	released bool
	sess     platform.Session
	trav     Traversal
}

// The kernel releases the platform session through this optional
// capability; a rename must fail at compile time, not only in a test.
var _ scan.Releaser = (*Controller)(nil)

func (c *Controller) budget() time.Duration {
	if c.PrepareBudget > 0 {
		return c.PrepareBudget
	}
	return DefaultPrepareBudget
}

func (c *Controller) every() time.Duration {
	if c.ObserveEvery > 0 {
		return c.ObserveEvery
	}
	return DefaultObserveEvery
}

func (c *Controller) isReleased() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.released
}

// onHotkey is the registered hotkey's trigger: the application's stop
// entry, guarded so a trigger racing the release cannot fall into the
// next session.
func (c *Controller) onHotkey() {
	if c.isReleased() {
		return
	}
	if c.Stop != nil {
		c.Stop()
	}
}

// onFault reports a watcher fault to the kernel, guarded the same way.
func (c *Controller) onFault(h *scan.ControllerHandle, f *platform.Fault) {
	if f == nil || c.isReleased() {
		return
	}
	h.Fail(scan.NewFault(f.Code, f))
}

// Prepare implements scan.Controller: preflight, open, wait for a ready
// target within the budget, lock the baseline, run the traversal's
// precheck. Setup and broken problems fail immediately; premise
// problems (focus lost, target hidden, ambiguity) and observation
// failures are waited out within the budget — the user is switching
// back to the game. No input is dispatched anywhere in Prepare.
func (c *Controller) Prepare(ctx context.Context, h *scan.ControllerHandle) error {
	if f := c.Port.Preflight().Fault(); f != nil {
		return scan.NewFault(f.Code, f)
	}
	sess, err := c.Port.Open(ctx, platform.SessionConfig{
		Target:  c.Target,
		Hotkey:  c.Hotkey,
		OnStop:  c.onHotkey,
		Gate:    h,
		OnFault: func(f *platform.Fault) { c.onFault(h, f) },
	})
	if err != nil {
		var f *platform.Fault
		if errors.As(err, &f) {
			return scan.NewFault(f.Code, f)
		}
		return err
	}
	c.mu.Lock()
	c.sess = sess
	c.mu.Unlock()

	// Starting a scan is a user-intent action: bring the game forward so
	// the user does not have to click back to it. A platform without the
	// capability (or a failed activation) falls through to the readiness
	// wait, where the user can still focus the target manually.
	if activator, ok := sess.(platform.Activator); ok {
		if err := activator.Activate(); err != nil {
			log.Printf("youkai: collect: target activation failed: %v; waiting for the user to focus the game", err)
		}
	}

	deadline := time.Now().Add(c.budget())
	every := c.every()
	var lastProblems []*platform.Fault
	var lastObsErr error
	// The wait loop: observe until ready, then lock the baseline; a
	// premise or observation failure at the baseline re-enters the wait
	// (paced), and the budget spans the whole preparation.
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		obs, err := sess.Observe()
		switch {
		case err != nil:
			lastObsErr = err
			lastProblems = nil
			if fault, ok := portFault(err); ok && (fault.Class == platform.ClassSetup || fault.Class == platform.ClassBroken) {
				return scan.NewFault(fault.Code, fault)
			}
			// An observation failure is waited out within the budget;
			// the last error survives into the timeout.
		case obs.Ready:
			lastProblems = nil
			lastObsErr = nil
			// Lock the baseline while the observation holds.
			_, err := sess.LockBaseline()
			if err == nil {
				c.mu.Lock()
				c.trav = c.newTraversal()
				trav := c.trav
				c.mu.Unlock()
				if err := trav.Precheck(ctx, sess); err != nil {
					return err
				}
				c.relayScope(trav)
				return nil
			}
			if fault, ok := portFault(err); ok {
				switch fault.Class {
				case platform.ClassSetup, platform.ClassBroken:
					return scan.NewFault(fault.Code, fault)
				}
			}
			// A premise or observation failure at the baseline: back to
			// the wait loop, paced.
			lastProblems = []*platform.Fault{faultOrNil(err)}
		default:
			for _, p := range obs.Problems {
				if p.Class == platform.ClassSetup || p.Class == platform.ClassBroken {
					return scan.NewFault(p.Code, p)
				}
			}
			lastProblems = obs.Problems
			lastObsErr = nil
		}
		if time.Now().After(deadline) {
			return scan.NewFault(CodePrepareTimeout, timeoutDetail(lastProblems, lastObsErr))
		}
		if err := sleepCtx(ctx, every); err != nil {
			return err
		}
	}
}

// SetScopeSink wires OnScopeObserved; the application calls it between
// building the controller and starting the session (the service does it
// right after the scan source handed over the spec). The field is
// written before the kernel starts the producer goroutine and only read
// there, so no synchronization is needed.
func (c *Controller) SetScopeSink(sink func(json.RawMessage)) { c.OnScopeObserved = sink }

// newTraversal builds this session's traversal.
func (c *Controller) newTraversal() Traversal {
	if c.Traversal != nil {
		return c.Traversal()
	}
	return ObserveOnly{}
}

// relayScope hands the traversal's observed-scope report (T5 §3) to the
// application's sink. It runs after a successful precheck and again after
// Collect, on the producer goroutine; the later report carries the sort
// and coverage facts the traversal established while running.
func (c *Controller) relayScope(trav Traversal) {
	if c.OnScopeObserved == nil {
		return
	}
	r, ok := trav.(ScopeReporter)
	if !ok {
		return
	}
	if scope := r.ObservedScope(); len(scope) > 0 {
		c.OnScopeObserved(scope)
	}
}

// Collect implements scan.Controller: the traversal runs the collection
// loop on the open session. The observed scope is relayed again after the
// traversal returns — including a stopped traversal — because the sort
// and coverage facts are only known once panels and positions have been
// observed; the deliverer overwrites the precheck report with this final
// one.
func (c *Controller) Collect(ctx context.Context, h *scan.ControllerHandle) error {
	c.mu.Lock()
	sess, trav := c.sess, c.trav
	c.mu.Unlock()
	if sess == nil || trav == nil {
		return errors.New("collect: collect called without a prepared session")
	}
	err := trav.Collect(ctx, sess, h)
	c.relayScope(trav)
	return err
}

// Release implements scan.Releaser: close the platform session exactly
// once, after the controller returned, and report its teardown.
func (c *Controller) Release() error {
	c.mu.Lock()
	if c.released {
		c.mu.Unlock()
		return nil
	}
	c.released = true
	sess := c.sess
	c.sess = nil
	c.mu.Unlock()
	if sess == nil {
		return nil
	}
	rep := sess.Close()
	log.Printf("youkai: collect: platform session closed: captureStopped=%v drained=%v released=%v hotkeyErr=%v dwell=%s err=%v",
		rep.CaptureStopped, rep.Drained, rep.Released, rep.HotkeyErr, rep.Dwell, rep.Err)
	return rep.Err
}

// portFault extracts a port fault from an error.
func portFault(err error) (*platform.Fault, bool) {
	var f *platform.Fault
	if errors.As(err, &f) {
		return f, true
	}
	return nil, false
}

// faultOrNil wraps a non-fault error as a plain fault-less problem
// holder; the timeout detail falls back to the error text.
func faultOrNil(err error) *platform.Fault {
	if f, ok := portFault(err); ok {
		return f
	}
	return &platform.Fault{Code: platform.CodeObservationFailed,
		Class: platform.ClassObservation, Message: err.Error(), At: time.Now()}
}

// timeoutDetail assembles the prepareTimeout message from what the wait
// observed last.
func timeoutDetail(problems []*platform.Fault, obsErr error) error {
	var parts []string
	if obsErr != nil {
		parts = append(parts, "last observation error: "+obsErr.Error())
	}
	for _, p := range problems {
		if p != nil {
			parts = append(parts, p.Code+": "+p.Message)
		}
	}
	if len(parts) == 0 {
		return errors.New("the target did not become ready within the preparation budget")
	}
	return fmt.Errorf("the target did not become ready within the preparation budget (%s)", joinParts(parts))
}

func joinParts(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += "; "
		}
		out += p
	}
	return out
}

// sleepCtx waits d or until ctx ends.
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
