// Package platformfake provides a scriptable Port and Session of the
// unified platform port for the collect controller's and the
// application service's tests: no native surface is involved, every
// behavior is set up by the test, and every call is recorded.
//
// It is a normal package (not a _test file) because two packages test
// through it; the recordings it keeps are the assertions' raw material.
package platformfake

import (
	"context"
	"fmt"
	"image"
	"sync"
	"time"

	"github.com/LoTwT/youkai/internal/platform"
)

// Port is a scriptable platform.Port.
type Port struct {
	mu sync.Mutex
	// FailingCheck, when non-empty, makes Preflight report that
	// capability as missing.
	FailingCheck string
	// OpenFault, when set, makes Open return it without creating a
	// session.
	OpenFault *platform.Fault
	// OnOpen, when set, runs at Open's entry before anything else — the
	// seam for triggering the stop hotkey in the commit race window.
	OnOpen func(cfg platform.SessionConfig)
	// NewSession builds the session Open returns; nil builds a plain
	// ready session.
	NewSession func() *Session

	openCalls int
	lastCfg   *platform.SessionConfig
}

// Preflight implements platform.Port.
func (p *Port) Preflight() platform.Preflight {
	p.mu.Lock()
	defer p.mu.Unlock()
	return platform.Preflight{Checks: []platform.CapabilityCheck{
		{Name: "fakeCapability", OK: p.FailingCheck == ""},
	}}
}

// Open implements platform.Port.
func (p *Port) Open(ctx context.Context, cfg platform.SessionConfig) (platform.Session, error) {
	p.mu.Lock()
	p.openCalls++
	stored := cfg
	p.lastCfg = &stored
	fault := p.OpenFault
	hook := p.OnOpen
	builder := p.NewSession
	p.mu.Unlock()
	if hook != nil {
		hook(cfg)
	}
	if fault != nil {
		return nil, fault
	}
	var sess *Session
	if builder != nil {
		sess = builder()
	} else {
		sess = NewSession()
	}
	sess.Configure(cfg)
	return sess, nil
}

// OpenCount reports how often Open ran.
func (p *Port) OpenCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.openCalls
}

// LastConfig returns the most recent Open config.
func (p *Port) LastConfig() *platform.SessionConfig {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastCfg
}

// Session is a scriptable platform.Session. The zero value serves a
// ready target: Observe is ready, LockBaseline returns version 1, Frame
// returns a small image, Dispatch posts, Close succeeds. Every field a
// test sets overrides the default, and every call is counted.
type Session struct {
	mu sync.Mutex

	// ObserveFn, when set, replaces the default ready observation.
	ObserveFn func() (platform.Observation, error)
	// ObserveErr makes Observe fail with an observationFailed fault.
	ObserveErr error
	// ObserveFaults are appended to every observation's problems.
	ObserveFaults []*platform.Fault
	// LockBaselineFault makes LockBaseline fail with it.
	LockBaselineFault *platform.Fault
	// LockBaselineVer is the version LockBaseline returns.
	LockBaselineVer platform.GeometryVersion
	// FrameFn, when set, replaces the default frame.
	FrameFn func(ctx context.Context, req platform.FrameRequest) (platform.Frame, error)
	// DispatchFn, when set, replaces the default posted result.
	DispatchFn func(op platform.InputOp) platform.InputResult
	// ActivateFn, when set, replaces the default successful activation.
	ActivateFn func() error
	// CloseFault joins into the close report's Err.
	CloseFault error
	// CloseSleep runs inside Close — the dwell stand-in.
	CloseSleep time.Duration
	// CloseReport overrides the whole report when set.
	CloseReport *platform.CloseReport

	// wiring captured from the Open config
	OnStop  func()
	OnFault func(*platform.Fault)
	Gate    platform.InputGate

	// records
	observeCalls    int
	lockCalls       int
	frameCalls      int
	activateCalls   int
	dispatchOps     []platform.InputOp
	closeCalls      int
	closed          bool
	hotkeyFired     int
	hotkeyAfterStop int
}

// NewSession returns a plain ready session.
func NewSession() *Session {
	return &Session{LockBaselineVer: 1}
}

// Configure captures the wiring of one Open config.
func (s *Session) Configure(cfg platform.SessionConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.OnStop = cfg.OnStop
	s.OnFault = cfg.OnFault
	s.Gate = cfg.Gate
}

// Target implements platform.Session.
func (s *Session) Target() platform.TargetInfo {
	return platform.TargetInfo{BundleID: "fake.bundle", PID: 1, WindowID: 2}
}

// Activate implements platform.Activator: the fake counts calls and
// reports the configured outcome.
func (s *Session) Activate() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activateCalls++
	if s.ActivateFn != nil {
		return s.ActivateFn()
	}
	return nil
}

// Observe implements platform.Session.
func (s *Session) Observe() (platform.Observation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.observeCalls++
	if s.ObserveErr != nil {
		f := &platform.Fault{Code: platform.CodeObservationFailed, Class: platform.ClassObservation,
			Message: s.ObserveErr.Error(), At: time.Now()}
		return platform.Observation{At: time.Now(), Problems: []*platform.Fault{f}}, f
	}
	if s.ObserveFn != nil {
		return s.ObserveFn()
	}
	obs := platform.Observation{At: time.Now(), Ready: true, Geometry: 1}
	obs.Problems = append(obs.Problems, s.ObserveFaults...)
	obs.Ready = len(obs.Problems) == 0
	return obs, nil
}

// LockBaseline implements platform.Session.
func (s *Session) LockBaseline() (platform.GeometryVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lockCalls++
	if s.LockBaselineFault != nil {
		return 0, s.LockBaselineFault
	}
	if s.LockBaselineVer == 0 {
		return 1, nil
	}
	return s.LockBaselineVer, nil
}

// Frame implements platform.Session.
func (s *Session) Frame(ctx context.Context, req platform.FrameRequest) (platform.Frame, error) {
	s.mu.Lock()
	s.frameCalls++
	fn := s.FrameFn
	s.mu.Unlock()
	if fn != nil {
		return fn(ctx, req)
	}
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	return platform.Frame{
		ID:        uint64(s.frameCalls),
		Geometry:  1,
		Image:     img,
		Content:   image.Rect(0, 0, 4, 4),
		ArrivedAt: time.Now(),
	}, nil
}

// Dispatch implements platform.Session.
func (s *Session) Dispatch(op platform.InputOp) platform.InputResult {
	s.mu.Lock()
	s.dispatchOps = append(s.dispatchOps, op)
	fn := s.DispatchFn
	closed := s.closed
	s.mu.Unlock()
	if fn != nil {
		return fn(op)
	}
	if closed {
		return platform.InputResult{Status: platform.InputNotDispatched,
			Denial: platform.NewFault(platform.CodeCaptureStopped, "the session is closed")}
	}
	return platform.InputResult{Status: platform.InputPosted, EventsPlanned: 2, EventsPosted: 2}
}

// Close implements platform.Session.
func (s *Session) Close() platform.CloseReport {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closeCalls++
	if s.closed {
		if s.CloseReport != nil {
			return *s.CloseReport
		}
		return platform.CloseReport{}
	}
	s.closed = true
	if s.CloseSleep > 0 {
		time.Sleep(s.CloseSleep)
	}
	if s.CloseReport != nil {
		rep := *s.CloseReport
		return rep
	}
	return platform.CloseReport{CaptureStopped: true, Drained: true, Released: true, Err: s.CloseFault}
}

// TriggerHotkey fires the stop hotkey: the captured OnStop runs, unless
// the session already closed — late triggers must not leak into the
// next session.
func (s *Session) TriggerHotkey() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		s.hotkeyAfterStop++
		return
	}
	s.hotkeyFired++
	if s.OnStop != nil {
		s.OnStop()
	}
}

// TriggerFault delivers a watcher-style fault through the captured
// OnFault, unless the session already closed.
func (s *Session) TriggerFault(f *platform.Fault) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.OnFault == nil {
		return
	}
	s.OnFault(f)
}

// HotkeyCallback returns the captured OnStop wiring — the controller's
// wrapped stop entry — so a test can invoke it directly, bypassing this
// fake's own closed guard and probing the caller's late-trigger
// protection.
func (s *Session) HotkeyCallback() func() {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.OnStop
}

// Counters reports the call recordings.
func (s *Session) Counters() (observe, lock, frames, dispatch, close int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.observeCalls, s.lockCalls, s.frameCalls, len(s.dispatchOps), s.closeCalls
}

// Activations returns the number of Activate calls.
func (s *Session) Activations() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.activateCalls
}

// DispatchedOps returns the recorded dispatch operations.
func (s *Session) DispatchedOps() []platform.InputOp {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]platform.InputOp(nil), s.dispatchOps...)
}

// IsClosed reports whether Close ran.
func (s *Session) IsClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// String renders the counters for failure messages.
func (s *Session) String() string {
	o, l, f, d, c := s.Counters()
	return fmt.Sprintf("observe=%d lock=%d frame=%d dispatch=%d close=%d", o, l, f, d, c)
}
