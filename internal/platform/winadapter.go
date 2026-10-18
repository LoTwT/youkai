package platform

import (
	"context"
	"errors"
	"fmt"
	"image"
	"log"
	"sync"
	"time"

	"github.com/LoTwT/youkai/internal/platform/windows"
)

// The Windows adapter of the unified port (PR 4a). It composes the
// windows base's pure logic — probe collection, target selection and
// identity locking, client geometry with DPI verification, the GDI
// capture choreography, the serial input actor, the hotkey manager —
// into the Port/Session contract, without build tags: the Win32
// implementations live behind the base's interfaces and this adapter
// runs (and is tested) on every platform.
//
// Windows specifics the adapter encodes:
//
//   - Every Frame and every Dispatch re-run ObserveTarget first: the
//     existing InputPrecheck does not re-measure geometry, and a moved
//     window would otherwise be clicked at stale coordinates.
//   - There is no capture stream: each Frame is a fresh GDI capture of
//     the client rect, so After is honored by construction (the capture
//     happens now) and there is no idle/reuse state to report.
//   - The stop hotkey's RegisterHotKey binds to the registering thread,
//     so registration and the message loop live on one locked thread
//     behind the StartLoop seam; the windows-only host wires the real
//     loop, tests a fake.
type WinNatives struct {
	Win   windows.WindowAPI
	Procs windows.ProcessAPI
	Geo   windows.GeometryAPI
	FG    windows.ForegroundAPI
	GDI   windows.GDI
	Send  windows.Sender
	// HotkeyAPI backs the hotkey manager.
	HotkeyAPI windows.HotkeyAPI
	// StartLoop registers the stop hotkey on a thread that owns its
	// message loop and returns the loop's stopper. RegisterHotKey with a
	// NULL hwnd binds to the registering thread, so the real wiring
	// (host_windows.go) runs registration, the loop and the
	// unregistration on one locked OS thread; tests inject a fake of the
	// same shape.
	StartLoop func(mgr *windows.HotkeyManager) (WinHotkeyLoop, error)
	// HotkeyID is the RegisterHotKey id; zero means 1.
	HotkeyID int
}

// WinHotkeyLoop stops the hotkey's message loop; Stop unregisters the
// hotkey on the thread that registered it and waits for the thread.
type WinHotkeyLoop interface {
	Stop() error
}

// WinAdapter is the Windows Port. Zero-value tunables take the
// documented defaults.
type WinAdapter struct {
	natives WinNatives
	// WatchEvery is the watcher's observation interval.
	WatchEvery time.Duration // default 250 ms
	// Sleep backs the actor's hold and drag intervals; tests inject a
	// recorder.
	Sleep func(time.Duration)
	// Now supplies timestamps; tests inject a fake clock.
	Now func() time.Time
	// Log receives diagnostics; nil uses the standard logger.
	Log *log.Logger
}

// NewWinAdapter validates the natives and returns the adapter.
func NewWinAdapter(n WinNatives) (*WinAdapter, error) {
	if n.Win == nil || n.Procs == nil || n.Geo == nil || n.FG == nil ||
		n.GDI == nil || n.Send == nil || n.HotkeyAPI == nil || n.StartLoop == nil {
		return nil, errors.New("platform: windows adapter requires every native surface")
	}
	if n.HotkeyID == 0 {
		n.HotkeyID = 1
	}
	return &WinAdapter{
		natives:    n,
		WatchEvery: 250 * time.Millisecond,
		Sleep:      time.Sleep,
		Now:        time.Now,
	}, nil
}

func (a *WinAdapter) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func (a *WinAdapter) sleep(d time.Duration) {
	if a.Sleep != nil {
		a.Sleep(d)
		return
	}
	time.Sleep(d)
}

func (a *WinAdapter) logf(format string, args ...any) {
	msg := fmt.Sprintf("platform windows: "+format, args...)
	if a.Log != nil {
		a.Log.Print(msg)
	} else {
		log.Print(msg)
	}
}

// Preflight implements Port: the process must be per-monitor-v2 DPI
// aware, or its screen coordinates are virtualized and the whole
// physical-pixel premise collapses. MyGo configures the awareness before
// creating windows; this check reports the process capability, it never
// changes it.
func (a *WinAdapter) Preflight() Preflight {
	self, err := a.natives.Geo.SelfDpiAwareness()
	return Preflight{Checks: []CapabilityCheck{{
		Name: "perMonitorV2DpiAwareness",
		OK:   err == nil && self == windows.DpiAwarenessPerMonitorV2,
		Err:  err,
	}}}
}

// Open implements Port. Sequence: stop hotkey on its message-loop
// thread, probe collection and target selection, identity lock, client
// measurement with DPI verification, a probe capture, the gated actor.
// Failures roll back in reverse.
func (a *WinAdapter) Open(ctx context.Context, cfg SessionConfig) (Session, error) {
	if err := cfg.validate(); err != nil {
		return nil, NewFault(CodeInvalidOperation, err.Error())
	}
	if cfg.Target.ExecutablePath == "" {
		return nil, NewFault(CodeInvalidOperation, "the windows target requires an executable path")
	}
	vk, err := WindowsVK(cfg.Hotkey.Key)
	if err != nil {
		return nil, NewFault(CodeInvalidOperation, err.Error())
	}
	mgr, err := windows.NewHotkeyManager(a.natives.HotkeyAPI, a.natives.HotkeyID,
		WindowsHotkeyMods(cfg.Hotkey), vk, cfg.OnStop)
	if err != nil {
		return nil, NewFault(CodeHotkeyUnavailable, err.Error())
	}
	loop, err := a.natives.StartLoop(mgr)
	if err != nil {
		return nil, NewFault(CodeHotkeyUnavailable, err.Error())
	}
	stopHotkey := func() {
		if err := loop.Stop(); err != nil {
			a.logf("open rollback: stop hotkey loop: %v", err)
		}
	}

	probes, err := windows.CollectProbes(a.natives.Win, a.natives.Procs)
	if err != nil {
		stopHotkey()
		return nil, NewFault(CodeObservationFailed, "window enumeration failed: "+err.Error())
	}
	spec := windows.TargetSpec{
		ExecutablePath: cfg.Target.ExecutablePath,
		ClassNameHint:  cfg.Target.ClassNameHint,
		TitleHint:      cfg.Target.TitleHint,
		RequireVisible: true,
	}
	probe, err := windows.SelectTarget(probes, spec)
	if err != nil {
		stopHotkey()
		var sel *windows.SelectionError
		if errors.As(err, &sel) {
			switch sel.Kind {
			case windows.SelectionNoWindow:
				return nil, NewFault(CodeTargetNotFound,
					fmt.Sprintf("no top-level window of %q; the game must be running", sel.Executable))
			case windows.SelectionNotReady:
				return nil, NewFault(CodeTargetHidden,
					fmt.Sprintf("the target window of %q is %s; make it visible and retry", sel.Executable, sel.HintDetail))
			case windows.SelectionAmbiguous:
				a.logf("target resolution: %d candidate windows of %s:", len(sel.Candidates), sel.Executable)
				for _, c := range sel.Candidates {
					a.logf("  hwnd %d class %q title %q visible=%v minimized=%v",
						c.HWND, c.ClassName, c.Title, c.Visible, c.Minimized)
				}
				return nil, NewFault(CodeTargetAmbiguous,
					fmt.Sprintf("%d windows of %q remain candidates; hints must disambiguate the launcher, login and same-name windows", len(sel.Candidates), sel.Executable))
			}
		}
		return nil, NewFault(CodeObservationFailed, err.Error())
	}
	locked := windows.LockIdentity(probe)

	s := &winSession{
		adapter:   a,
		cfg:       cfg,
		locked:    locked,
		tracker:   &windows.GeometryTracker{},
		hotkey:    mgr,
		loop:      loop,
		watchStop: make(chan struct{}),
		watchDone: make(chan struct{}),
		reported:  make(map[string]bool),
		targetInfo: TargetInfo{
			HWND:           uintptr(locked.HWND),
			ExecutablePath: locked.ImagePath,
			ClassName:      locked.ClassName,
			Title:          locked.Title,
		},
	}
	s.precheck = windows.InputPrecheck(a.natives.Win, a.natives.FG, s.tracker, locked)

	// The first observation measures and verifies the client geometry
	// (identity, DPI consistency) — its problems are Open failures here.
	obs := s.observe()
	if obs.fault != nil {
		stopHotkey()
		return nil, mapWinFault(obs.fault)
	}
	if !obs.obs.Matched {
		stopHotkey()
		return nil, NewFault(CodeTargetLost, "the freshly locked target failed its first re-verification")
	}
	geo := obs.obs.Geometry
	if !geo.Monitor.ContainsRect(geo.Client) {
		stopHotkey()
		return nil, NewFault(CodeTargetHidden, "the client area is not fully inside its monitor")
	}
	// The probe capture proves the GDI path works on this target before
	// the session starts.
	capture, fault := windows.CaptureFrame(a.natives.GDI, geo, "open-probe")
	if fault != nil {
		stopHotkey()
		return nil, mapWinFault(fault)
	}
	s.frameSeq++
	s.imageW = capture.Image.Width
	s.imageH = capture.Image.Height

	actor := windows.NewInputActor(a.natives.Send, s.precheck)
	actor.SetSleep(a.sleep)
	actor.SetStepGate(func() bool { return cfg.Gate.InputPermitted() })
	s.actor = actor
	return s, nil
}

// winObservation is the adapter-internal observation: the windows base's
// TargetObservation plus the mapped port fault when its premises broke.
type winObservation struct {
	obs   windows.TargetObservation
	fault *windows.PlatformFault
}

// mapWinFault translates the windows base's fault codes into the port's
// vocabulary: identity loss is targetLost; measurement and DPI-target
// inconsistencies void the geometry (geometryInvalid); the process's own
// DPI awareness is a process capability (permissionMissing).
func mapWinFault(f *windows.PlatformFault) *Fault {
	switch f.Code {
	case windows.CodeTargetLost:
		return &Fault{Code: CodeTargetLost, Class: ClassBroken, Message: f.Message + ": " + f.RawErr, At: f.At}
	case windows.CodeDpiAwarenessMismatch:
		return &Fault{Code: CodePermissionMissing, Class: ClassSetup, Message: f.Message + ": " + f.RawErr, At: f.At}
	default:
		// CodeGeometryUnavailable, CodeDpiVirtualized,
		// CodeDpiCheckUnavailable, CodeCaptureFailed: the geometry or
		// capture path cannot be trusted.
		code := CodeGeometryInvalid
		if f.Code == windows.CodeCaptureFailed {
			code = CodeCaptureStopped
		}
		return &Fault{Code: code, Class: ClassBroken, Message: f.Message + ": " + f.RawErr, At: f.At}
	}
}

// winSession is one Windows session of the port.
type winSession struct {
	adapter    *WinAdapter
	cfg        SessionConfig
	locked     windows.WindowIdentity
	tracker    *windows.GeometryTracker
	precheck   windows.PrecheckFunc
	actor      *windows.InputActor
	hotkey     *windows.HotkeyManager
	loop       WinHotkeyLoop
	targetInfo TargetInfo

	mu           sync.Mutex
	closed       bool
	report       *CloseReport
	baselined    bool
	baseline     windows.GeometryVersion
	watchStarted bool
	watchStop    chan struct{}
	watchDone    chan struct{}
	reported     map[string]bool
	frameSeq     uint64
	imageW       int
	imageH       int
}

func (s *winSession) Target() TargetInfo { return s.targetInfo }

// observe runs one ObserveTarget pass and maps its premises.
func (s *winSession) observe() winObservation {
	obs := windows.ObserveTarget(s.adapter.natives.Win, s.adapter.natives.Procs,
		s.adapter.natives.Geo, s.adapter.natives.FG, s.locked, s.tracker, s.adapter.now())
	if obs.Fault != nil {
		return winObservation{obs: obs, fault: obs.Fault}
	}
	return winObservation{obs: obs}
}

// Observe implements Session.
func (s *winSession) Observe() (Observation, error) {
	now := s.adapter.now()
	o := s.observe()
	var problems []*Fault
	if o.fault != nil {
		problems = append(problems, mapWinFault(o.fault))
	} else {
		if !o.obs.Matched {
			problems = append(problems, &Fault{Code: CodeTargetLost, Class: ClassBroken,
				Message: "the locked target no longer matches its identity", At: now})
		}
		if !o.obs.Foreground {
			problems = append(problems, &Fault{Code: CodeFocusLost, Class: ClassPremise,
				Message: "another window is the actual foreground window", At: now})
		}
		if o.obs.Minimized {
			problems = append(problems, &Fault{Code: CodeTargetHidden, Class: ClassPremise,
				Message: "the target window is minimized", At: now})
		} else if !o.obs.Visible {
			problems = append(problems, &Fault{Code: CodeTargetHidden, Class: ClassPremise,
				Message: "the target window is not visible", At: now})
		}
		if s.baselineLocked() && o.obs.GeometryChanged {
			problems = append(problems, &Fault{Code: CodeGeometryChanged, Class: ClassBroken,
				Message: fmt.Sprintf("the client geometry changed after the baseline (version %d)", o.obs.Geometry.Version), At: now})
		}
	}
	return Observation{
		At:       now,
		Ready:    len(problems) == 0,
		Problems: problems,
		Geometry: GeometryVersion(o.obs.Geometry.Version),
	}, nil
}

func (s *winSession) baselineLocked() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.baselined
}

// LockBaseline implements Session.
func (s *winSession) LockBaseline() (GeometryVersion, error) {
	obs, err := s.Observe()
	if err != nil {
		return 0, err
	}
	if !obs.Ready {
		return 0, mostSevere(obs.Problems)
	}
	if obs.Geometry == 0 {
		return 0, &Fault{Code: CodeGeometryInvalid, Class: ClassBroken,
			Message: "no verified client geometry; the baseline cannot freeze", At: s.adapter.now()}
	}
	s.mu.Lock()
	s.baselined = true
	s.baseline = windows.GeometryVersion(obs.Geometry)
	s.mu.Unlock()
	s.actor.Permit()
	s.startWatcher()
	return obs.Geometry, nil
}

func (s *winSession) startWatcher() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.watchStarted || s.closed {
		return
	}
	s.watchStarted = true
	go s.watch()
}

// watch is the port's observation thread; each fault code is reported at
// most once and nothing is reported after Close.
func (s *winSession) watch() {
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
			continue // unreachable today: observe never errors
		}
		for _, p := range obs.Problems {
			s.reportOnce(p)
		}
	}
}

func (s *winSession) reportOnce(f *Fault) {
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

func (s *winSession) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// Frame implements Session: a fresh GDI capture of the re-measured
// client region. There is no stream, so every frame is fresh by
// construction — Reused is always false — and an After in the future
// simply delays the capture until then.
func (s *winSession) Frame(ctx context.Context, req FrameRequest) (Frame, error) {
	if s.isClosed() {
		return Frame{}, &Fault{Code: CodeCaptureStopped, Class: ClassBroken,
			Message: "the session is closed", At: s.adapter.now()}
	}
	if d := time.Until(req.After); d > 0 {
		select {
		case <-ctx.Done():
			return Frame{}, ctx.Err()
		case <-time.After(d):
		}
	}
	o := s.observe()
	if o.fault != nil {
		return Frame{}, mapWinFault(o.fault)
	}
	if s.baselineLocked() && o.obs.GeometryChanged {
		return Frame{}, &Fault{Code: CodeGeometryChanged, Class: ClassBroken,
			Message: "the client geometry changed after the baseline; the frame cannot be captured under the old coordinates", At: s.adapter.now()}
	}
	geo := o.obs.Geometry
	s.mu.Lock()
	s.frameSeq++
	seq := s.frameSeq
	s.mu.Unlock()
	capture, fault := windows.CaptureFrame(s.adapter.natives.GDI, geo, fmt.Sprintf("frame-%d", seq))
	if fault != nil {
		return Frame{}, mapWinFault(fault)
	}
	s.mu.Lock()
	s.imageW = capture.Image.Width
	s.imageH = capture.Image.Height
	s.mu.Unlock()
	// The capture path allocates fresh pixels per capture; the RGBA image
	// wraps them and the frame owns them.
	img := &image.RGBA{
		Pix:    capture.Image.Pixels,
		Stride: capture.Image.Stride,
		Rect:   image.Rect(0, 0, capture.Image.Width, capture.Image.Height),
	}
	return Frame{
		ID:        seq,
		Geometry:  GeometryVersion(capture.GeometryVersion),
		Image:     img,
		Content:   image.Rect(0, 0, capture.Image.Width, capture.Image.Height),
		ArrivedAt: capture.CompletedAt,
	}, nil
}

// Dispatch implements Session: observe (re-measure), precheck, gate,
// act. The actor re-runs the precheck inside its serial section as
// defense in depth.
func (s *winSession) Dispatch(op InputOp) InputResult {
	started := s.adapter.now()
	deny := func(f *Fault) InputResult {
		return InputResult{Status: InputNotDispatched, Denial: f, StartedAt: started, FinishedAt: s.adapter.now()}
	}
	if s.isClosed() {
		return deny(&Fault{Code: CodeCaptureStopped, Class: ClassBroken, Message: "the session is closed", At: started})
	}

	// 1. Re-measure: the precheck alone would click stale coordinates
	// after a window move.
	o := s.observe()
	if o.fault != nil {
		return deny(mapWinFault(o.fault))
	}
	if s.baselineLocked() && o.obs.GeometryChanged {
		return deny(&Fault{Code: CodeStaleGeometry, Class: ClassPremise,
			Message: fmt.Sprintf("the client geometry changed after the baseline (version %d); re-derive the coordinates", o.obs.Geometry.Version), At: started})
	}
	geo := o.obs.Geometry
	if !s.baselineLocked() || op.Geometry != GeometryVersion(geo.Version) {
		return deny(&Fault{Code: CodeStaleGeometry, Class: ClassPremise,
			Message: fmt.Sprintf("the request's geometry version %d is not the current %d", op.Geometry, geo.Version), At: started})
	}

	// 2. Image pixels → screen pixels through the measured geometry.
	s.mu.Lock()
	imageW, imageH := s.imageW, s.imageH
	s.mu.Unlock()
	if imageW == 0 || imageH == 0 {
		return deny(&Fault{Code: CodePositionOutside, Class: ClassPremise,
			Message: "no frame captured yet; input coordinates cannot be mapped", At: started})
	}
	at, err := windows.ImagePointToScreen(geo, imageW, imageH, windows.PixelPoint{X: op.At.X, Y: op.At.Y})
	if err != nil {
		return deny(&Fault{Code: CodePositionOutside, Class: ClassPremise, Message: err.Error(), At: started})
	}

	var wop windows.InputOp
	switch op.Kind {
	case InputClick:
		wop = windows.InputOp{Kind: windows.OpClick, Point: at, Button: windows.MouseButtonLeft, Hold: op.Hold}
	case InputKeyPress:
		vk, err := WindowsVK(op.Key)
		if err != nil {
			return deny(&Fault{Code: CodeInvalidOperation, Class: ClassPremise, Message: err.Error(), At: started})
		}
		wop = windows.InputOp{Kind: windows.OpKeyPress, Key: vk}
	case InputDrag:
		to, err := windows.ImagePointToScreen(geo, imageW, imageH, windows.PixelPoint{X: op.To.X, Y: op.To.Y})
		if err != nil {
			return deny(&Fault{Code: CodePositionOutside, Class: ClassPremise, Message: err.Error(), At: started})
		}
		wop = windows.InputOp{Kind: windows.OpDrag, Point: at, To: to, Button: windows.MouseButtonLeft,
			Steps: op.Steps, Duration: op.Duration}
	default:
		return deny(&Fault{Code: CodeInvalidOperation, Class: ClassPremise,
			Message: fmt.Sprintf("unknown input kind %q", op.Kind), At: started})
	}
	wreq := windows.InputRequest{
		GeometryVersion: windows.GeometryVersion(op.Geometry),
		FrameRef:        fmt.Sprintf("frame-%d", op.FrameID),
		Op:              wop,
	}

	// 3. Precheck, then the kernel's gate (the design order for the
	// Windows side); the actor re-runs the precheck inside its serial
	// section.
	if err := s.precheck(wreq); err != nil {
		return deny(precheckFault(err, started))
	}
	if !s.cfg.Gate.InputPermitted() {
		return deny(&Fault{Code: CodePermitClosed, Class: ClassPremise,
			Message: "the kernel's input permission is closed", At: started})
	}
	// A drag's destination needs its own client-membership and hit test:
	// the precheck only checks the op's primary point.
	if op.Kind == InputDrag {
		if !geo.Client.Contains(wop.To) {
			return deny(&Fault{Code: CodePositionOutside, Class: ClassPremise,
				Message: fmt.Sprintf("the drag destination %v lies outside the client rect %v", wop.To, geo.Client), At: started})
		}
		if hit := s.adapter.natives.FG.HitWindow(wop.To); hit == 0 || s.adapter.natives.FG.RootOf(hit) != s.locked.HWND {
			return deny(&Fault{Code: CodePositionOutside, Class: ClassPremise,
				Message: fmt.Sprintf("the drag destination %v is covered or unresolvable", wop.To), At: started})
		}
	}

	res := s.actor.Dispatch(wreq)
	return mapWinResult(res, started, s.adapter.now())
}

// precheckFault maps a PrecheckFunc refusal to the port's vocabulary.
func precheckFault(err error, at time.Time) *Fault {
	var pe *windows.PrecheckError
	if errors.As(err, &pe) {
		switch pe.Code {
		case windows.PrecheckNoGeometry:
			return &Fault{Code: CodeGeometryInvalid, Class: ClassBroken, Message: pe.Detail, At: at}
		case windows.PrecheckStaleGeometry:
			return &Fault{Code: CodeStaleGeometry, Class: ClassPremise, Message: pe.Detail, At: at}
		case windows.PrecheckTargetGone, windows.PrecheckTargetReplaced:
			return &Fault{Code: CodeTargetLost, Class: ClassBroken, Message: pe.Detail, At: at}
		case windows.PrecheckFocusLost:
			return &Fault{Code: CodeFocusLost, Class: ClassPremise, Message: pe.Detail, At: at}
		case windows.PrecheckMinimized:
			return &Fault{Code: CodeTargetHidden, Class: ClassPremise, Message: pe.Detail, At: at}
		case windows.PrecheckPointOutside, windows.PrecheckOccluded:
			return &Fault{Code: CodePositionOutside, Class: ClassPremise, Message: pe.Detail, At: at}
		}
		return &Fault{Code: CodeObservationFailed, Class: ClassObservation, Message: pe.Detail, At: at}
	}
	var pf *windows.PlatformFault
	if errors.As(err, &pf) {
		return mapWinFault(pf)
	}
	return &Fault{Code: CodeInvalidOperation, Class: ClassPremise, Message: err.Error(), At: at}
}

// mapWinResult translates the actor's result into the port's.
func mapWinResult(res windows.InputResult, started, finished time.Time) InputResult {
	out := InputResult{
		EventsPlanned: res.EventsPlanned,
		EventsPosted:  res.EventsInserted,
		Aborted:       res.Aborted,
		StartedAt:     started,
		FinishedAt:    finished,
	}
	switch res.State {
	case windows.StateDelivered:
		out.Status = InputPosted
	case windows.StatePartialDelivered:
		out.Status = InputPartial
		if res.Err != nil {
			out.Denial = precheckResultFault(res.Err)
		}
	case windows.StateBlocked:
		out.Status = InputFailed
		out.Denial = NewFault(CodeInputBlocked, faultMessage(res.Err))
	case windows.StateFailed:
		out.Status = InputFailed
		out.Denial = NewFault(CodeInputFailed, faultMessage(res.Err))
	default: // StateNotDispatched
		out.Status = InputNotDispatched
		out.Denial = precheckResultFault(res.Err)
	}
	return out
}

// precheckResultFault maps the actor's refusal error; a closed
// permission and the sticky UIPI refusal have their own codes.
func precheckResultFault(err error) *Fault {
	if err == nil {
		return NewFault(CodeInvalidOperation, "the operation was refused")
	}
	msg := err.Error()
	if len(msg) >= 24 && msg[:24] == "input permission closed:" {
		return NewFault(CodePermitClosed, msg)
	}
	if len(msg) >= 22 && msg[:22] == "input refused after an" {
		return NewFault(CodeInputBlocked, msg)
	}
	return precheckFault(err, time.Now())
}

func faultMessage(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// Close implements Session: stop the watcher, close the actor's
// permission and release its own presses, stop the hotkey loop (which
// unregisters on the thread that registered). There is no capture
// stream and SendInput is synchronous, so there is no drain to wait for
// and no dwell to sleep.
func (s *winSession) Close() CloseReport {
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

	if watchStarted {
		close(s.watchStop)
		<-s.watchDone
	}
	releases := s.actor.ReleaseHeld()
	s.actor.ClosePermission("session closed")
	hotkeyErr := s.loop.Stop()

	rep := CloseReport{
		// Windows has no capture stream; nothing was ever left running.
		CaptureStopped: true,
		Drained:        true,
		Released:       true,
		HotkeyErr:      hotkeyErr,
	}
	for _, r := range releases {
		rep.Releases = append(rep.Releases, ReleaseOutcome{
			Input:    r.Key.String(),
			Released: r.Released,
			Err:      r.Err,
		})
	}
	var errs []error
	if hotkeyErr != nil {
		errs = append(errs, fmt.Errorf("stop hotkey loop: %w", hotkeyErr))
	}
	for _, r := range rep.Releases {
		if !r.Released {
			errs = append(errs, fmt.Errorf("release of %s was not inserted", r.Input))
		}
	}
	rep.Err = errors.Join(errs...)
	s.adapter.logf("session close: releases=%d hotkeyErr=%v err=%v", len(rep.Releases), rep.HotkeyErr, rep.Err)
	s.mu.Lock()
	cached := rep
	s.report = &cached
	s.mu.Unlock()
	return rep
}
