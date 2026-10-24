package platform

import (
	"context"
	"errors"
	"fmt"
	"image"
	"log"
	"os"
	"sync"
	"time"

	"github.com/LoTwT/youkai/internal/platform/macos"
)

// The macOS adapter of the unified port (PR 4a). It composes the macos
// base's native surfaces — enumeration, single-window stream, Quartz
// poster, foreground probe, permission probe, hotkey registrar, mach
// clock — into the Port/Session contract, without build tags and without
// touching the cgo bridge: both platforms compile it, and fake-driven
// tests run everywhere.
//
// Field-round facts this adapter encodes (实测轮 ①):
//
//   - The target resolves per session by filtering the enumeration
//     (bundle, layer 0, on screen, fully inside one display) down to
//     exactly one window; position drifts between launches, so nothing
//     is cached across sessions.
//   - AX focused-window queries fail on this machine even when
//     accessibility is trusted (AXError -25205), so the adapter never
//     queries AX: readiness uses NSWorkspace.frontmostApplication and
//     demands the window be fully inside a display (visible fraction 1).
//   - Screen recording is judged by what actually happened — the
//     enumeration listing other processes' windows, the stream starting,
//     the first complete frame — never by a preflight claim.
//   - The exit dwell after the last event post exists because
//     CGEventPost has no confirmation semantics.

// MacNatives are the macos base's native surfaces one adapter runs on.
// Every field is required; Host (on darwin) wires the real
// implementations, tests inject fakes.
type MacNatives struct {
	Enum     macos.WindowEnumerator
	Runner   macos.NativeStreamRunner
	Poster   macos.InputPoster
	FG       macos.ForegroundProbe
	Activate macos.AppActivator
	Perms    macos.PermissionProbe
	Hotkey   macos.NativeHotKeyRegistrar
	Clock    macos.MachClockSource
	// OwnPID partitions the screen-recording evidence (windows of other
	// processes are the meaningful signal); zero means os.Getpid().
	OwnPID int32
}

// MacAdapter is the macOS Port. Zero-value tunables take the documented
// defaults.
type MacAdapter struct {
	natives MacNatives
	// FirstFrameWait bounds Open's wait for the first complete frame.
	FirstFrameWait time.Duration // default 5 s
	// WatchEvery is the watcher's observation interval.
	WatchEvery time.Duration // default 250 ms
	// FramePoll is Frame's poll interval on the frame cache.
	FramePoll time.Duration // default 5 ms
	// DwellAfterPost is the close dwell after the last event post.
	DwellAfterPost time.Duration // default 50 ms
	// Sleep backs the dispatcher's hold and drag intervals and the close
	// dwell; tests inject a recorder.
	Sleep func(time.Duration)
	// Now supplies timestamps; tests inject a fake clock.
	Now func() time.Time
	// Log receives diagnostics (candidate windows, close reports); nil
	// uses the standard logger.
	Log *log.Logger
}

// NewMacAdapter validates the natives and returns the adapter.
func NewMacAdapter(n MacNatives) (*MacAdapter, error) {
	if n.Enum == nil || n.Runner == nil || n.Poster == nil || n.FG == nil ||
		n.Perms == nil || n.Hotkey == nil || n.Clock == nil {
		return nil, errors.New("platform: mac adapter requires every native surface")
	}
	if n.OwnPID == 0 {
		n.OwnPID = int32(os.Getpid())
	}
	return &MacAdapter{
		natives:        n,
		FirstFrameWait: 5 * time.Second,
		WatchEvery:     250 * time.Millisecond,
		FramePoll:      5 * time.Millisecond,
		DwellAfterPost: 50 * time.Millisecond,
		Sleep:          time.Sleep,
		Now:            time.Now,
	}, nil
}

func (a *MacAdapter) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func (a *MacAdapter) sleep(d time.Duration) {
	if a.Sleep != nil {
		a.Sleep(d)
		return
	}
	time.Sleep(d)
}

func (a *MacAdapter) logf(format string, args ...any) {
	msg := fmt.Sprintf("platform mac: "+format, args...)
	if a.Log != nil {
		a.Log.Print(msg)
	} else {
		log.Print(msg)
	}
}

// Preflight implements Port: accessibility trust and event synthesis,
// queried without prompting. Screen recording is deliberately absent
// here — Open judges it by the actual capture evidence.
func (a *MacAdapter) Preflight() Preflight {
	trusted, trustErr := a.natives.Perms.AccessibilityTrusted()
	allowed, postErr := a.natives.Perms.EventPostAllowed()
	return Preflight{Checks: []CapabilityCheck{
		{Name: "accessibilityTrust", OK: trustErr == nil && trusted, Err: trustErr},
		{Name: "eventSynthesis", OK: postErr == nil && allowed, Err: postErr},
	}}
}

// Open implements Port. Sequence: stop hotkey, fresh target resolution,
// capture stream with the first complete frame (the screen-recording
// evidence), gated dispatcher. Failures roll back in reverse.
func (a *MacAdapter) Open(ctx context.Context, cfg SessionConfig) (Session, error) {
	if err := cfg.validate(); err != nil {
		return nil, NewFault(CodeInvalidOperation, err.Error())
	}
	hkSpec, err := MacHotKeySpec(cfg.Hotkey)
	if err != nil {
		return nil, NewFault(CodeInvalidOperation, err.Error())
	}
	hotkey, err := macos.RegisterStopHotKey(a.natives.Hotkey, hkSpec, cfg.OnStop)
	if err != nil {
		return nil, NewFault(CodeHotkeyUnavailable, err.Error())
	}
	unregisterHotkey := func() {
		if err := hotkey.Close(); err != nil {
			a.logf("open rollback: unregister stop hotkey: %v", err)
		}
	}

	windows, displays, err := a.natives.Enum.EnumerateWindows()
	if err != nil {
		unregisterHotkey()
		return nil, NewFault(CodeObservationFailed, "enumeration failed: "+err.Error())
	}
	candidates := macos.FilterTargetCandidates(cfg.Target.BundleID, windows, displays)
	if len(candidates) == 0 {
		unregisterHotkey()
		return nil, NewFault(CodeTargetNotFound, fmt.Sprintf(
			"the game is not running or has no eligible window (bundle %q, normal layer, on screen, fully inside one display); open the game first", cfg.Target.BundleID))
	}
	if len(candidates) > 1 {
		// The candidate list is the field-round diagnostic: 实测核对 is
		// only possible when the ambiguous set is on the record.
		a.logf("target resolution: %d candidate windows of %s:", len(candidates), cfg.Target.BundleID)
		for _, c := range candidates {
			a.logf("  window %d pid %d frame(%.1f,%.1f %.1fx%.1f) title %q",
				c.WindowID, c.PID, c.Frame.X, c.Frame.Y, c.Frame.W, c.Frame.H, c.Title)
		}
		unregisterHotkey()
		return nil, NewFault(CodeTargetAmbiguous, fmt.Sprintf(
			"%d windows of %q passed the target filter; close the extra windows and retry", len(candidates), cfg.Target.BundleID))
	}
	target := candidates[0]
	selector := macos.WindowSelector{BundleID: cfg.Target.BundleID, PID: target.PID, WindowID: target.WindowID}

	s := &macSession{
		adapter:      a,
		cfg:          cfg,
		selector:     selector,
		hotkey:       hotkey,
		streamEvents: make(chan macos.PlatformFault, 64),
		reported:     make(map[string]bool),
		watchStop:    make(chan struct{}),
		watchDone:    make(chan struct{}),
		targetInfo: TargetInfo{
			BundleID: selector.BundleID,
			PID:      selector.PID,
			WindowID: selector.WindowID,
		},
	}
	capSess, err := macos.StartCapture(macos.CaptureConfig{
		Runner:        a.natives.Runner,
		Selector:      selector,
		WindowFrame:   target.Frame,
		Clock:         macos.NewMachClock(a.natives.Clock),
		CacheCapacity: 1,
		OnEvent:       func(f macos.PlatformFault) { s.onStreamEvent(f) },
	})
	if err != nil {
		unregisterHotkey()
		return nil, NewFault(CodeScreenRecordingUnavailable, "starting the capture stream failed: "+err.Error())
	}
	s.cap = capSess
	rollback := func() {
		capSess.Stop()
		<-capSess.Done()
		unregisterHotkey()
	}

	// The enumeration part of the screen-recording evidence fails fast:
	// an enumeration that lists no windows of other processes means the
	// capture permission did not demonstrate the capability the scan
	// needs, and no first frame will change that verdict.
	early := macos.ScreenRecordingEvidenceFrom(nil, windows, a.natives.OwnPID, nil, true, capSess)
	if assess := early.Assessment(); assess.State == macos.ScreenRecordingUnavailable {
		rollback()
		msg := "screen recording unavailable"
		if assess.Err != nil {
			msg = "screen recording unavailable at " + string(assess.FailedStage) + ": " + assess.Err.Error()
		}
		return nil, NewFault(CodeScreenRecordingUnavailable, msg)
	}

	// The first complete frame completes the evidence.
	wait := a.FirstFrameWait
	deadline := a.now().Add(wait)
	for capSess.FirstCompleteFrameID() == 0 {
		if err := ctx.Err(); err != nil {
			rollback()
			return nil, err
		}
		if a.now().After(deadline) {
			rollback()
			return nil, NewFault(CodeScreenRecordingUnavailable,
				fmt.Sprintf("no complete frame within %s; the capture permission or the target is not cooperating", wait))
		}
		a.sleep(a.FramePoll)
	}
	evidence := macos.ScreenRecordingEvidenceFrom(nil, windows, a.natives.OwnPID, nil, true, capSess)
	if assess := evidence.Assessment(); assess.State != macos.ScreenRecordingAvailable {
		rollback()
		msg := "screen recording unavailable"
		if assess.Err != nil {
			msg = "screen recording unavailable at " + string(assess.FailedStage) + ": " + assess.Err.Error()
		}
		return nil, NewFault(CodeScreenRecordingUnavailable, msg)
	}

	disp := macos.NewInputDispatcher(a.natives.Poster, a.now)
	disp.SetSleep(a.sleep)
	disp.SetStepGate(func() bool { return cfg.Gate.InputPermitted() })
	disp.SetReadiness(func(req macos.ShortInputRequest) error { return s.readiness(req) })
	if err := disp.OpenFor(capSess.Token()); err != nil {
		rollback()
		return nil, NewFault(CodeInvalidOperation, err.Error())
	}
	s.disp = disp
	return s, nil
}

// macSession is one macOS session of the port: locked target, capture
// stream, gated dispatcher, stop hotkey and (after LockBaseline) the
// watcher.
type macSession struct {
	adapter      *MacAdapter
	cfg          SessionConfig
	selector     macos.WindowSelector
	targetInfo   TargetInfo
	hotkey       *macos.StopHotKey
	cap          *macos.CaptureSession
	disp         *macos.InputDispatcher
	streamEvents chan macos.PlatformFault

	mu            sync.Mutex
	closed        bool
	streamStopped bool
	report        *CloseReport
	baselined     bool
	baseline      macos.GeometryVersion
	transform     *macos.GeometryTransform
	watchStarted  bool
	watchStop     chan struct{}
	watchDone     chan struct{}
	reported      map[string]bool
}

var (
	_ Session   = (*macSession)(nil)
	_ Activator = (*macSession)(nil)
)

func (s *macSession) Target() TargetInfo { return s.targetInfo }

// Activate brings the locked target application to the foreground. The
// controller calls it once for a user-started scan; every input still
// re-checks readiness, and losing focus during scanning stops the walk.
func (s *macSession) Activate() error {
	if s.adapter.natives.Activate == nil {
		return errors.New("macos: target activation is not configured")
	}
	return s.adapter.natives.Activate.ActivatePID(s.selector.PID)
}

// onStreamEvent runs on the capture stream's callback thread: it must
// never block. Events queue for the watcher (after the baseline) and for
// Observe's problems (before it); an overflowing queue drops with a log
// line rather than stalling the native callback. A stream stop also
// latches the session-level flag Frame waits on, so a frame request
// cannot poll forever against a dead stream.
func (s *macSession) onStreamEvent(f macos.PlatformFault) {
	if f.Category == macos.FaultStream {
		s.mu.Lock()
		s.streamStopped = true
		s.mu.Unlock()
	}
	select {
	case s.streamEvents <- f:
	default:
		s.adapter.logf("stream event dropped (queue full): %v", f)
	}
}

// mapStreamFault translates a macos base stream fault into the port's
// vocabulary: a stream stop is captureStopped (the platform does not
// infer a revoked permission from it), a geometry fault is
// geometryInvalid.
func mapStreamFault(f macos.PlatformFault, at time.Time) *Fault {
	switch f.Category {
	case macos.FaultStream:
		msg := "the capture stream stopped"
		if f.Err != nil {
			msg += ": " + f.Err.Error()
		}
		return &Fault{Code: CodeCaptureStopped, Class: ClassBroken, Message: msg, At: at}
	case macos.FaultGeometry:
		msg := "a frame's geometry attachments were missing or inconsistent"
		if f.Err != nil {
			msg += ": " + f.Err.Error()
		}
		return &Fault{Code: CodeGeometryInvalid, Class: ClassBroken, Message: msg, At: at}
	default:
		msg := fmt.Sprintf("unexpected platform fault %s at %s", f.Category, f.Stage)
		if f.Err != nil {
			msg += ": " + f.Err.Error()
		}
		return &Fault{Code: CodeObservationFailed, Class: ClassObservation, Message: msg, At: at}
	}
}

// takeStreamEvents drains the queued stream faults.
func (s *macSession) takeStreamEvents() []macos.PlatformFault {
	var out []macos.PlatformFault
	for {
		select {
		case f := <-s.streamEvents:
			out = append(out, f)
		default:
			return out
		}
	}
}

// Observe implements Session: one fresh observation of the target.
func (s *macSession) Observe() (Observation, error) {
	now := s.adapter.now()
	windows, displays, err := s.adapter.natives.Enum.EnumerateWindows()
	if err != nil {
		f := &Fault{Code: CodeObservationFailed, Class: ClassObservation,
			Message: "enumeration failed: " + err.Error(), At: now}
		return Observation{At: now, Problems: []*Fault{f}}, f
	}
	problems := s.assess(windows, displays, now)
	for _, f := range s.takeStreamEvents() {
		problems = append(problems, mapStreamFault(f, now))
	}
	return Observation{
		At:       now,
		Ready:    len(problems) == 0,
		Problems: problems,
		Geometry: GeometryVersion(s.cap.Geometry()),
	}, nil
}

// assess runs the observation checks over one enumeration snapshot. It
// deliberately does not use macos.AssessTarget's status directly: the
// ambiguity rule is the candidate filter's (predicate-passing siblings
// only, target excluded), per 实测轮 ①.
func (s *macSession) assess(windows []macos.NativeWindow, displays []macos.NativeDisplay, now time.Time) []*Fault {
	var problems []*Fault
	var target *macos.NativeWindow
	for i := range windows {
		if s.selector.Matches(windows[i]) {
			target = &windows[i]
			break
		}
	}
	if target == nil {
		return append(problems, &Fault{Code: CodeTargetLost, Class: ClassBroken,
			Message: "the selected window is gone from the enumeration (process exit or window replacement)", At: now})
	}
	if siblings := macos.FilterAmbiguitySiblings(s.selector, windows, displays); len(siblings) > 0 {
		msg := fmt.Sprintf("%d sibling window(s) of the target pass the candidate filter", len(siblings))
		for i, w := range siblings {
			if i >= 3 {
				msg += fmt.Sprintf(" and %d more", len(siblings)-i)
				break
			}
			msg += fmt.Sprintf("; window %d frame(%.1f,%.1f %.1fx%.1f)", w.WindowID, w.Frame.X, w.Frame.Y, w.Frame.W, w.Frame.H)
		}
		problems = append(problems, &Fault{Code: CodeTargetAmbiguous, Class: ClassPremise, Message: msg, At: now})
	}
	if !target.OnScreen {
		problems = append(problems, &Fault{Code: CodeTargetHidden, Class: ClassPremise,
			Message: "the target window is not on screen (minimized or fully off screen)", At: now})
	} else if !macos.WindowFullyInsideDisplays(target.Frame, displays) {
		problems = append(problems, &Fault{Code: CodeTargetHidden, Class: ClassPremise,
			Message: "the target window is not fully inside one display", At: now})
	}
	if pid, err := s.adapter.natives.FG.FrontmostPID(); err != nil {
		problems = append(problems, &Fault{Code: CodeObservationFailed, Class: ClassObservation,
			Message: "the frontmost query failed: " + err.Error(), At: now})
	} else if pid != s.selector.PID {
		problems = append(problems, &Fault{Code: CodeFocusLost, Class: ClassPremise,
			Message: fmt.Sprintf("the frontmost application (pid %d) is not the target (pid %d)", pid, s.selector.PID), At: now})
	}
	if geo := s.cap.Geometry(); s.baselineLocked() && geo != s.baseline {
		problems = append(problems, &Fault{Code: CodeGeometryChanged, Class: ClassBroken,
			Message: fmt.Sprintf("the geometry version moved from the baseline %d to %d", s.baseline, geo), At: now})
	}
	if latest, ok := s.cap.LatestFrame(); ok && !latest.GeometryValid {
		problems = append(problems, &Fault{Code: CodeGeometryInvalid, Class: ClassBroken,
			Message: "the latest complete frame carries missing or inconsistent geometry attachments", At: now})
	}
	return problems
}

func (s *macSession) baselineLocked() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.baselined
}

// LockBaseline implements Session.
func (s *macSession) LockBaseline() (GeometryVersion, error) {
	obs, err := s.Observe()
	if err != nil {
		return 0, err
	}
	if !obs.Ready {
		return 0, mostSevere(obs.Problems)
	}
	geo, version, ok := s.cap.CurrentGeometry()
	if !ok || version == 0 {
		return 0, &Fault{Code: CodeGeometryInvalid, Class: ClassBroken,
			Message: "no frame with valid geometry yet; the baseline cannot freeze", At: s.adapter.now()}
	}
	transform, err := macos.NewGeometryTransform(geo)
	if err != nil {
		return 0, &Fault{Code: CodeGeometryInvalid, Class: ClassBroken,
			Message: "the current geometry is not consistent enough for a transform: " + err.Error(), At: s.adapter.now()}
	}
	s.mu.Lock()
	s.baselined = true
	s.baseline = version
	s.transform = transform
	s.mu.Unlock()
	s.disp.SetGeometry(version)
	s.startWatcher()
	return GeometryVersion(version), nil
}

// mostSevere picks the problem a LockBaseline error should carry: a
// Setup or Broken problem first (they fail the session), otherwise the
// first problem (a premise the caller waits out).
func mostSevere(problems []*Fault) *Fault {
	for _, p := range problems {
		if p.Class == ClassSetup || p.Class == ClassBroken {
			return p
		}
	}
	if len(problems) > 0 {
		return problems[0]
	}
	return NewFault(CodeObservationFailed, "the observation was not ready")
}

// startWatcher launches the observation watcher exactly once; it only
// runs after the baseline locked.
func (s *macSession) startWatcher() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.watchStarted || s.closed {
		return
	}
	s.watchStarted = true
	go s.watch()
}

// watch is the port's observation thread: every interval it takes one
// observation, checks the two preflight-able permissions and drains the
// stream events, reporting each fault code at most once through OnFault.
// It stops when Close closes watchStop, and after Close no fault is
// reported at all.
func (s *macSession) watch() {
	defer close(s.watchDone)
	interval := s.adapter.WatchEvery
	if interval <= 0 {
		interval = 250 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-s.watchStop:
			return
		case <-ticker.C:
		}
		if s.isClosed() {
			return
		}
		obs, err := s.Observe()
		if err != nil {
			var f *Fault
			if errors.As(err, &f) {
				s.reportOnce(f)
			} else {
				s.reportOnce(&Fault{Code: CodeObservationFailed, Class: ClassObservation, Message: err.Error(), At: s.adapter.now()})
			}
		} else {
			for _, p := range obs.Problems {
				s.reportOnce(p)
			}
		}
		if trusted, err := s.adapter.natives.Perms.AccessibilityTrusted(); err != nil || !trusted {
			detail := "the accessibility trust is gone"
			if err != nil {
				detail = "the accessibility trust query failed: " + err.Error()
			}
			s.reportOnce(&Fault{Code: CodePermissionRevoked, Class: ClassBroken, Message: detail, At: s.adapter.now()})
		}
		if allowed, err := s.adapter.natives.Perms.EventPostAllowed(); err != nil || !allowed {
			detail := "event synthesis is no longer allowed"
			if err != nil {
				detail = "the event synthesis query failed: " + err.Error()
			}
			s.reportOnce(&Fault{Code: CodePermissionRevoked, Class: ClassBroken, Message: detail, At: s.adapter.now()})
		}
		now := s.adapter.now()
		for _, f := range s.takeStreamEvents() {
			s.reportOnce(mapStreamFault(f, now))
		}
	}
}

// reportOnce delivers a fault through OnFault the first time its code
// appears; later occurrences of the same code are silent, and after
// Close nothing is delivered at all.
func (s *macSession) reportOnce(f *Fault) {
	if f == nil {
		return
	}
	s.mu.Lock()
	if s.closed || s.reported[f.Code] {
		s.mu.Unlock()
		return
	}
	s.reported[f.Code] = true
	onFault := s.cfg.OnFault
	s.mu.Unlock()
	if onFault != nil {
		onFault(f)
	}
}

func (s *macSession) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func (s *macSession) streamStoppedLocked() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.streamStopped
}

// Frame implements Session: one complete frame with valid geometry
// satisfying req.After.
func (s *macSession) Frame(ctx context.Context, req FrameRequest) (Frame, error) {
	poll := s.adapter.FramePoll
	if poll <= 0 {
		poll = 5 * time.Millisecond
	}
	for {
		// A continuously producing stream must not bypass the caller's
		// deadline: check the context before serving a cached frame.
		if err := ctx.Err(); err != nil {
			return Frame{}, err
		}
		if s.isClosed() {
			return Frame{}, &Fault{Code: CodeCaptureStopped, Class: ClassBroken,
				Message: "the session is closed", At: s.adapter.now()}
		}
		if latest, ok := s.cap.LatestFrame(); ok && latest.GeometryValid && latest.ArrivedAt.After(req.After) {
			return s.convertFrame(latest), nil
		}
		if s.streamStoppedLocked() {
			return Frame{}, &Fault{Code: CodeCaptureStopped, Class: ClassBroken,
				Message: "the capture stream stopped before a frame satisfying the request arrived", At: s.adapter.now()}
		}
		if latest, ok := s.cap.LatestFrame(); ok && latest.GeometryValid {
			if status, statusAt := s.cap.LastStatus(); status == macos.FrameIdle && statusAt.After(req.After) {
				// The content has not changed since the latest complete
				// frame: unchanged content is still current content.
				frame := s.convertFrame(latest)
				frame.Reused = true
				return frame, nil
			}
		}
		select {
		case <-ctx.Done():
			return Frame{}, ctx.Err()
		case <-time.After(poll):
		}
	}
}

// convertFrame copies one BGRA observation into a Go-owned RGBA frame;
// the cache's bytes are never aliased.
func (s *macSession) convertFrame(obs macos.ImageObservation) Frame {
	img := image.NewRGBA(image.Rect(0, 0, obs.Width, obs.Height))
	for y := 0; y < obs.Height; y++ {
		src := obs.Pixels[y*obs.BytesPerRow : y*obs.BytesPerRow+obs.Width*4]
		dst := img.Pix[y*img.Stride : y*img.Stride+obs.Width*4]
		for x := 0; x < obs.Width; x++ {
			s4 := src[4*x:]
			d4 := dst[4*x:]
			d4[0] = s4[2] // R ← B swap of BGRA
			d4[1] = s4[1] // G
			d4[2] = s4[0] // B
			d4[3] = 0xFF  // the stream's alpha is not meaningful
		}
	}
	content := image.Rect(0, 0, obs.Width, obs.Height)
	if obs.ValidRect != nil {
		content = image.Rect(obs.ValidRect.X, obs.ValidRect.Y, obs.ValidRect.MaxX(), obs.ValidRect.MaxY())
	}
	return Frame{
		ID:          obs.FrameID,
		Geometry:    GeometryVersion(obs.Geometry),
		Image:       img,
		Content:     content,
		ArrivedAt:   obs.ArrivedAt,
		DisplayTime: obs.DisplayTime,
	}
}

// readiness is the dispatcher's pre-post hook: the kernel's gate first,
// then the fresh readiness verdict (identity, ambiguity, on-screen,
// in-display, foreground, position inside the target and the displays),
// then the event-synthesis permission. AX is never queried — the field
// round proved the system-wide focus query unusable on this machine.
func (s *macSession) readiness(req macos.ShortInputRequest) error {
	if !s.cfg.Gate.InputPermitted() {
		return &denial{fault: &Fault{Code: CodePermitClosed, Class: ClassPremise,
			Message: "the kernel's input permission is closed", At: s.adapter.now()}}
	}
	now := s.adapter.now()
	windows, displays, err := s.adapter.natives.Enum.EnumerateWindows()
	if err != nil {
		return &denial{fault: &Fault{Code: CodeObservationFailed, Class: ClassObservation,
			Message: "enumeration failed: " + err.Error(), At: now}}
	}
	assessment := macos.AssessTarget(s.selector, windows, now)
	if assessment.Observation != nil {
		// Replace AssessTarget's any-sibling ambiguity rule with the
		// candidate filter's, keeping its observation construction.
		status := assessment.Status
		if status == macos.TargetAmbiguous || status == macos.TargetUnique {
			if len(macos.FilterAmbiguitySiblings(s.selector, windows, displays)) == 0 {
				status = macos.TargetUnique
			} else {
				status = macos.TargetAmbiguous
			}
		}
		assessment.Status = status
	}
	pid, fgErr := s.adapter.natives.FG.FrontmostPID()
	foreground := macos.ForegroundState{FrontmostPID: pid, ObservedAt: now}
	if fgErr != nil {
		foreground.Err = fgErr
	}
	displayFrames := make([]macos.PointRect, len(displays))
	for i, d := range displays {
		displayFrames[i] = d.Frame
	}
	pos := macos.Point{X: req.Op.Position.X, Y: req.Op.Position.Y}
	verdict := macos.AssessInputReadiness(assessment, foreground, nil, displayFrames, &pos,
		macos.ReadinessOptions{MinVisibleFraction: 1}, now)
	if !verdict.Ready {
		return &denial{fault: readinessFault(verdict, now)}
	}
	if allowed, err := s.adapter.natives.Perms.EventPostAllowed(); err != nil || !allowed {
		detail := "event synthesis is no longer allowed"
		if err != nil {
			detail = "the event synthesis query failed: " + err.Error()
		}
		return &denial{fault: &Fault{Code: CodePermissionRevoked, Class: ClassBroken, Message: detail, At: now}}
	}
	return nil
}

// readinessFault folds a readiness verdict's denials into one port
// fault, mapped through the diagnostic vocabulary.
func readinessFault(verdict macos.ReadinessVerdict, at time.Time) *Fault {
	code := CodeObservationFailed
	class := ClassObservation
	detail := ""
	if len(verdict.Denials) > 0 {
		d := verdict.Denials[0]
		switch d.Code {
		case macos.DenialTargetMissing, macos.DenialTargetReplaced:
			code, class = CodeTargetLost, ClassBroken
		case macos.DenialTargetAmbiguous:
			code, class = CodeTargetAmbiguous, ClassPremise
		case macos.DenialWindowOffScreen, macos.DenialWindowNotVisibleEnough:
			code, class = CodeTargetHidden, ClassPremise
		case macos.DenialNotFrontmost:
			code, class = CodeFocusLost, ClassPremise
		case macos.DenialPositionOutsideTarget, macos.DenialPositionOffScreen:
			code, class = CodePositionOutside, ClassPremise
		default:
			// Display bounds unknown, frontmost query failed, the AX
			// codes (unreachable here): the observation, not the target.
			code, class = CodeObservationFailed, ClassObservation
		}
		for _, dd := range verdict.Denials {
			if detail != "" {
				detail += "; "
			}
			detail += string(dd.Code) + ": " + dd.Detail
		}
	}
	return &Fault{Code: code, Class: class, Message: detail, At: at}
}

// denial carries a port fault through the macos dispatcher's readiness
// hook without flattening it to message text.
type denial struct {
	fault *Fault
}

func (d *denial) Error() string { return d.fault.Error() }

// Dispatch implements Session.
func (s *macSession) Dispatch(op InputOp) InputResult {
	started := s.adapter.now()
	deny := func(f *Fault) InputResult {
		return InputResult{Status: InputNotDispatched, Denial: f, StartedAt: started, FinishedAt: s.adapter.now()}
	}
	if s.isClosed() {
		return deny(&Fault{Code: CodeCaptureStopped, Class: ClassBroken, Message: "the session is closed", At: started})
	}
	if !s.cfg.Gate.InputPermitted() {
		return deny(&Fault{Code: CodePermitClosed, Class: ClassPremise, Message: "the kernel's input permission is closed", At: started})
	}
	s.mu.Lock()
	baselined, transform := s.baselined, s.transform
	s.mu.Unlock()
	if !baselined {
		return deny(&Fault{Code: CodeStaleGeometry, Class: ClassPremise, Message: "no geometry baseline is locked", At: started})
	}
	if current := GeometryVersion(s.cap.Geometry()); op.Geometry != current {
		return deny(&Fault{Code: CodeStaleGeometry, Class: ClassPremise,
			Message: fmt.Sprintf("the request's geometry version %d is not the current %d", op.Geometry, current), At: started})
	}
	// Positions live in image pixels; they must lie inside the frame's
	// valid content region before mapping through the baseline
	// transform.
	content := image.Rect(0, 0, 0, 0)
	if transform != nil {
		v := transform.ValidRect()
		content = image.Rect(v.X, v.Y, v.MaxX(), v.MaxY())
	}
	if !pointInRect(op.At, content) {
		return deny(&Fault{Code: CodePositionOutside, Class: ClassPremise,
			Message: fmt.Sprintf("the operation point %v lies outside the frame's content region %v", op.At, content), At: started})
	}
	if op.Kind == InputDrag && !pointInRect(op.To, content) {
		return deny(&Fault{Code: CodePositionOutside, Class: ClassPremise,
			Message: fmt.Sprintf("the drag destination %v lies outside the frame's content region %v", op.To, content), At: started})
	}

	var mop macos.InputOperation
	switch op.Kind {
	case InputClick:
		mop = macos.InputOperation{
			Kind:     macos.OpMouseClick,
			Position: transform.PixelToScreen(macos.PixelPoint{X: op.At.X, Y: op.At.Y}),
			Button:   macos.MouseButtonLeft,
			Hold:     op.Hold,
		}
	case InputKeyPress:
		code, err := MacKeyCode(op.Key)
		if err != nil {
			return deny(&Fault{Code: CodeInvalidOperation, Class: ClassPremise, Message: err.Error(), At: started})
		}
		mop = macos.InputOperation{Kind: macos.OpKeyPress, KeyCode: code}
	case InputDrag:
		mop = macos.InputOperation{
			Kind:     macos.OpDrag,
			Position: transform.PixelToScreen(macos.PixelPoint{X: op.At.X, Y: op.At.Y}),
			To:       transform.PixelToScreen(macos.PixelPoint{X: op.To.X, Y: op.To.Y}),
			Button:   macos.MouseButtonLeft,
			Steps:    op.Steps,
			Duration: op.Duration,
		}
	default:
		return deny(&Fault{Code: CodeInvalidOperation, Class: ClassPremise,
			Message: fmt.Sprintf("unknown input kind %q", op.Kind), At: started})
	}
	res := s.disp.Dispatch(macos.ShortInputRequest{
		Stream:   s.cap.Token(),
		Geometry: macos.GeometryVersion(op.Geometry),
		Frame:    op.FrameID,
		Op:       mop,
	})
	return mapMacResult(res, started)
}

func pointInRect(p image.Point, r image.Rectangle) bool {
	return p.X >= r.Min.X && p.X < r.Max.X && p.Y >= r.Min.Y && p.Y < r.Max.Y
}

// mapMacResult translates the macos dispatcher's result into the port's.
func mapMacResult(res macos.InputResult, started time.Time) InputResult {
	out := InputResult{
		Status:        InputPosted,
		Aborted:       res.Aborted,
		EventsPlanned: res.EventsPosted + res.EventsFailed,
		EventsPosted:  res.EventsPosted,
	}
	if !res.StartedAt.IsZero() {
		out.StartedAt = res.StartedAt
	} else {
		out.StartedAt = started
	}
	out.FinishedAt = res.At
	switch res.Status {
	case macos.DispatchPosted:
	case macos.DispatchPartial:
		out.Status = InputPartial
	case macos.DispatchBlocked:
		out.Status = InputNotDispatched
		out.Denial = macBlockedFault(res)
	}
	return out
}

func macBlockedFault(res macos.InputResult) *Fault {
	switch res.BlockedCode {
	case macos.BlockedClosed, macos.BlockedNotOpen, macos.BlockedWrongSession:
		return NewFault(CodePermitClosed, res.BlockedReason)
	case macos.BlockedStaleGeometry:
		return NewFault(CodeStaleGeometry, res.BlockedReason)
	case macos.BlockedReadiness:
		var d *denial
		if errors.As(res.ReadinessErr, &d) {
			return d.fault
		}
		return NewFault(CodeObservationFailed, res.BlockedReason)
	default:
		return NewFault(CodeInvalidOperation, res.BlockedReason)
	}
}

// Close implements Session: stop the watcher, close the dispatcher
// (releasing its own presses), stop and drain the capture, unregister
// the hotkey, dwell after the last post. Idempotent.
func (s *macSession) Close() CloseReport {
	s.mu.Lock()
	if s.closed {
		rep := CloseReport{}
		if s.report != nil {
			rep = *s.report
		}
		s.mu.Unlock()
		return rep
	}
	s.closed = true
	watchStarted := s.watchStarted
	s.mu.Unlock()

	// 1. Stop the watcher and wait for it, so no OnFault races the
	// teardown.
	if watchStarted {
		close(s.watchStop)
		<-s.watchDone
	}
	// 2. Close the dispatcher: its own presses release, nothing further
	// is ever posted.
	releases := s.disp.Close("session closed")
	// 3. Stop the capture and await the full teardown — stop, drain,
	// release — bounded by the capture session's internal timeouts.
	s.cap.Stop()
	teardown := <-s.cap.Done()
	// 4. Unregister the stop hotkey.
	hotkeyErr := s.hotkey.Close()
	// 5. Dwell after the last post: CGEventPost has no confirmation
	// semantics, and a process exiting immediately can lose the final
	// release event (实测轮 ①, driver-side reproduction).
	var dwell time.Duration
	if last := s.disp.LastPostAt(); !last.IsZero() {
		dwellAfter := s.adapter.DwellAfterPost
		if dwellAfter <= 0 {
			dwellAfter = 50 * time.Millisecond
		}
		if d := dwellAfter - s.adapter.now().Sub(last); d > 0 {
			s.adapter.sleep(d)
			dwell = d
		}
	}

	rep := CloseReport{
		CaptureStopped: !teardown.NativeStoppedAt.IsZero(),
		Drained:        teardown.Drained,
		Released:       !teardown.ReleasedAt.IsZero(),
		HotkeyErr:      hotkeyErr,
		Dwell:          dwell,
	}
	for _, o := range releases {
		name := "input"
		switch o.Input.Kind {
		case "key":
			name = fmt.Sprintf("key %d", o.Input.KeyCode)
		case "button":
			name = fmt.Sprintf("button %d", o.Input.Button)
		}
		rep.Releases = append(rep.Releases, ReleaseOutcome{Input: name, Released: o.Released, Err: o.Err})
	}
	var errs []error
	if hotkeyErr != nil {
		errs = append(errs, fmt.Errorf("unregister stop hotkey: %w", hotkeyErr))
	}
	if teardown.NativeStopTimedOut {
		errs = append(errs, fmt.Errorf("capture stop did not complete within %s; the native objects were deliberately not released", teardownStopBound))
	} else if teardown.NativeStopErr != nil {
		errs = append(errs, fmt.Errorf("capture stop: %w", teardown.NativeStopErr))
	}
	if teardown.UnexpectedStopErr != nil {
		errs = append(errs, fmt.Errorf("capture stopped unexpectedly: %w", teardown.UnexpectedStopErr))
	}
	if !teardown.Drained {
		errs = append(errs, errors.New("the capture frame queue did not drain within the bound"))
	}
	for _, o := range rep.Releases {
		if !o.Released {
			errs = append(errs, fmt.Errorf("release of %s did not post", o.Input))
		}
	}
	rep.Err = errors.Join(errs...)
	s.adapter.logf("session close: capture stopped=%v drained=%v released=%v hotkeyErr=%v dwell=%s err=%v",
		rep.CaptureStopped, rep.Drained, rep.Released, rep.HotkeyErr, rep.Dwell, rep.Err)
	s.mu.Lock()
	cached := rep
	s.report = &cached
	s.mu.Unlock()
	return rep
}

// teardownStopBound documents the capture teardown's internal bound in
// close reports (stop + drain ≈ 8 s worst case).
const teardownStopBound = "about 8 s"
