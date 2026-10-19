package platform

import (
	"context"
	"fmt"
)

// InputGate is the kernel's input permission. *scan.ControllerHandle
// satisfies it, so the port reads the permission live from the kernel
// instead of keeping a truth of its own: the platform dispatchers'
// latches close for good at Session.Close and nothing else.
type InputGate interface {
	InputPermitted() bool
}

// Port is one platform's session factory: preflight and open. A Port is
// safe for concurrent use and outlives any single session.
type Port interface {
	// Preflight checks process capabilities without a target and never
	// prompts: macOS accessibility trust + event synthesis; Windows
	// PMv2 DPI awareness. Screen recording stays unknown here — Open
	// judges it by the real enumeration, stream start and first frame.
	Preflight() Preflight
	// Open, in order: registers the stop hotkey, resolves the target
	// window afresh (never cached across sessions), starts capture and
	// waits for the first complete frame (macOS) or measures and
	// probe-captures (Windows), then builds the gated dispatcher. On
	// failure it rolls back in reverse — the capture stops and drains,
	// the hotkey unregisters — and returns a *Fault. The stop hotkey is
	// registered here, on purpose, and stays until Close: neither
	// platform offers a side-effect-free registrability probe, a
	// register-unregister-register sequence would leave a window where
	// another process could take the combination, and showing the stop
	// means while preparing is part of the session architecture.
	Open(ctx context.Context, cfg SessionConfig) (Session, error)
}

// SessionConfig configures one session of a Port.
type SessionConfig struct {
	// Target selects the game window.
	Target TargetSpec
	// Hotkey is the global stop hotkey, registered for the whole
	// session.
	Hotkey HotkeySpec
	// OnStop runs when the hotkey fires: the app's idempotent stop
	// entry. It must not block; it runs on the platform's callback
	// thread.
	OnStop func()
	// Gate is the kernel's input permission, read live before and
	// during every dispatch and between a drag's steps. Required.
	Gate InputGate
	// OnFault reports watcher and stream faults after the baseline was
	// locked. It may be called from any goroutine and must not block.
	// Faults before the baseline surface in Observe's Problems instead.
	OnFault func(*Fault)
}

// validate fails fast on a config the port could only reject mid-open.
func (c SessionConfig) validate() error {
	if c.Target.BundleID == "" && c.Target.ExecutablePath == "" {
		return fmt.Errorf("platform: session config has no target (bundle id or executable path)")
	}
	if c.Hotkey.Key == "" {
		return fmt.Errorf("platform: session config has no stop hotkey key")
	}
	if !keyKnown(c.Hotkey.Key) {
		return fmt.Errorf("platform: stop hotkey key %q is outside the key vocabulary", c.Hotkey.Key)
	}
	if c.Gate == nil {
		return fmt.Errorf("platform: session config has no input gate")
	}
	if c.OnStop == nil {
		return fmt.Errorf("platform: session config has no stop callback")
	}
	return nil
}

// Session is one opened platform session: the locked target, the
// capture, the gated dispatcher and the stop hotkey, from Open to
// Close. Every method is safe for concurrent use; Observe, Frame and
// Dispatch re-verify the target before acting on it.
type Session interface {
	// Target returns the locked identity, for diagnostics only.
	Target() TargetInfo
	// Observe takes one fresh observation: identity, uniqueness,
	// foreground, visibility/in-display, geometry, capture health. A
	// non-nil error means the observation itself failed (Class
	// observation), never "the target is fine".
	Observe() (Observation, error)
	// LockBaseline freezes the geometry baseline when the current
	// observation is Ready; afterwards any geometry change is a
	// geometryChanged problem. It also starts the port's watcher, whose
	// faults go to OnFault. A non-nil error carries the observation's
	// problems (or the observation failure) so the caller can decide to
	// wait or fail by class.
	LockBaseline() (GeometryVersion, error)
	// Frame returns one complete frame satisfying req. It returns only
	// frames with valid geometry; a caller holding the frame owns its
	// pixels.
	Frame(ctx context.Context, req FrameRequest) (Frame, error)
	// Dispatch delivers one bounded input operation. It re-checks,
	// inside the platform's serial section right before posting: the
	// gate, geometry == the current version, identity, foreground,
	// in-display, the point inside the content region. The result never
	// claims a game action succeeded.
	Dispatch(op InputOp) InputResult
	// Close tears the session down — stop the watcher, close and release
	// the dispatcher's own presses, stop and drain the capture,
	// unregister the hotkey, dwell — and reports each stage. Idempotent:
	// later calls return the first report.
	Close() CloseReport
}

// Activator is an optional Session capability: bring the locked target
// application to the foreground. A user-initiated scan calls it once
// before waiting for readiness, so the user does not have to click back
// to the game. It is never used during scanning: losing focus there is a
// stop condition, not something to repair.
type Activator interface {
	Activate() error
}
