package macos

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// The capture session (T2 §2.2, §4.2, §4.3): one SCStream-filtered
// single-window stream per session, driven through the narrow
// NativeStreamRunner interface so the pure logic — intake gating, the
// bounded cache, geometry versioning, the asynchronous stop sequence
// and late-callback rejection — is testable with fakes. The native
// layer only creates, starts, stops, drains and releases.

// NativeStreamEventKind names one native stream lifecycle event.
type NativeStreamEventKind string

const (
	// NativeEventStarted: startCapture completed; Err carries its
	// error.
	NativeEventStarted NativeStreamEventKind = "started"
	// NativeEventStopped: stopCapture completed (user-requested stop or
	// a stream error). A spontaneous stop while the session runs is
	// reported to the caller as a stream fault.
	NativeEventStopped NativeStreamEventKind = "stopped"
	// NativeEventDrained: the frame queue barrier completed; all
	// already-queued frame callbacks have run.
	NativeEventDrained NativeStreamEventKind = "drained"
	// eventWaitTimeout is the internal pseudo-kind returned when
	// awaiting an event timed out.
	eventWaitTimeout NativeStreamEventKind = "waitTimeout"
)

// NativeStreamEvent is one native stream lifecycle event.
type NativeStreamEvent struct {
	Kind NativeStreamEventKind
	Err  error
}

// NativeStreamSink receives the native stream's callbacks. Both
// methods are called from callback context (a native serial queue for
// frames): implementations must copy what they keep and return
// quickly, and must never wait for OCR, saving or UI work.
type NativeStreamSink interface {
	// OnNativeFrame delivers one frame callback with a Go-owned pixel
	// copy.
	OnNativeFrame(frame NativeFrame)
	// OnNativeStreamEvent delivers start/stop/drain lifecycle events.
	OnNativeStreamEvent(event NativeStreamEvent)
}

// NativeStreamHandle is the opaque native stream reference returned by
// the runner; the pure logic never inspects it.
type NativeStreamHandle interface{}

// NativeStreamRunner is the native stream surface. Implementations
// start one single-window SCStream (desktopIndependentWindow filter,
// image only — no audio, no microphone), deliver frames and lifecycle
// events through the sink, and stop asynchronously: RequestStop only
// requests, completion arrives as a NativeEventStopped event.
type NativeStreamRunner interface {
	// StartStream creates the stream for windowID and requests the
	// start; the asynchronous start result arrives as a
	// NativeEventStarted event on the sink. A non-nil error means the
	// stream was not created and no callbacks will follow.
	StartStream(windowID uint32, sink NativeStreamSink) (NativeStreamHandle, error)
	// RequestStop asks the stream to stop; completion arrives as a
	// NativeEventStopped event. It must not block waiting for the
	// native completion.
	RequestStop(handle NativeStreamHandle) error
	// Drain asks for a barrier on the frame queue; completion arrives
	// as a NativeEventDrained event. Called after the stop completed.
	Drain(handle NativeStreamHandle) error
	// Release frees the native objects. Only called after the stop
	// completed and the drain barrier passed.
	Release(handle NativeStreamHandle)
}

// Default capture session timeouts. They bound the waits for native
// lifecycle events; a timeout is reported honestly in the teardown
// report and the native objects are then deliberately not released
// (releasing while callbacks may still arrive would be unsafe). The
// values are engineering defaults, to be revisited with real-device
// data (实测轮 ①).
const (
	DefaultStartTimeout = 5 * time.Second
	DefaultStopTimeout  = 5 * time.Second
	DefaultDrainTimeout = 3 * time.Second
)

// CaptureConfig configures one capture session. Runner and Selector
// are required; the rest falls back to the documented defaults.
type CaptureConfig struct {
	// Runner starts and stops the native stream.
	Runner NativeStreamRunner
	// Selector identifies the target window; its WindowID selects the
	// captured window and its identity is carried in observations.
	Selector WindowSelector
	// WindowFrame is the enumeration-time window frame in screen
	// points. Frames that carry their own screenRect attachment (the
	// normal case on the supported systems) override it per frame.
	WindowFrame PointRect
	// Clock converts displayTime to comparable monotonic time; nil
	// leaves DisplayTime unset (raw ticks are still preserved).
	Clock *MachClock
	// CacheCapacity bounds the latest-frame cache; zero means one.
	CacheCapacity int
	// StartTimeout, StopTimeout and DrainTimeout bound the native
	// lifecycle waits; zero means the defaults.
	StartTimeout time.Duration
	StopTimeout  time.Duration
	DrainTimeout time.Duration
	// OnEvent reports stream faults (unexpected stop, invalid frame
	// geometry) to the caller. It is called from callback context and
	// must not block; the caller should enqueue and move on. Nil drops
	// the events (the session state still reflects them). Start
	// failures are reported through the StartCapture error, not here.
	OnEvent func(fault PlatformFault)
	// Now supplies observation timestamps; nil uses time.Now.
	Now func() time.Time
}

// CaptureSession owns one single-window capture stream from start to
// teardown. It is safe for concurrent use: frame callbacks arrive on
// native threads, Stop arrives from the caller, and Done is awaited
// anywhere. The intake closes the moment a stop begins; frames after
// that are counted as late, never delivered. The session never
// re-binds to another window or reuses its stream token.
type CaptureSession struct {
	token   StreamToken
	cfg     CaptureConfig
	tracker *GeometryTracker
	cache   *LatestFrameCache

	mu sync.Mutex
	// state is the lifecycle: starting → running → stopping → done.
	state sessionState
	// handle is the native stream reference (set once).
	handle NativeStreamHandle
	// report accumulates the teardown accounting.
	report TeardownReport
	// frameSeq assigns frame ids; every received callback gets one.
	frameSeq uint64
	// lastStatus is the newest status observation.
	lastStatus   FrameStatus
	lastStatusAt time.Time
	// firstCompleteFrameID is the first complete frame with pixels.
	firstCompleteFrameID uint64
	// pending holds lifecycle events read out of order while awaiting
	// a specific kind.
	pending []NativeStreamEvent
	// events carries native lifecycle events from the sink to the
	// waits.
	events chan NativeStreamEvent
	// done delivers the single teardown report.
	done chan TeardownReport
	// now supplies observation timestamps.
	now func() time.Time
}

type sessionState int32

const (
	stateStarting sessionState = iota
	stateRunning
	stateStopping
	stateDone
)

var streamTokens struct {
	mu sync.Mutex
	n  StreamToken
}

func nextStreamToken() StreamToken {
	streamTokens.mu.Lock()
	defer streamTokens.mu.Unlock()
	streamTokens.n++
	return streamTokens.n
}

// StartCapture creates the session, starts the native stream and waits
// for the asynchronous start result. On failure the teardown sequence
// still runs and the error is returned; the session token of the
// failed attempt is not reused.
func StartCapture(cfg CaptureConfig) (*CaptureSession, error) {
	if cfg.Runner == nil {
		return nil, errors.New("macos: capture config requires a runner")
	}
	if cfg.Selector.WindowID == 0 {
		return nil, errors.New("macos: capture config requires a window id")
	}
	capacity := cfg.CacheCapacity
	if capacity == 0 {
		capacity = 1
	}
	cache, err := NewLatestFrameCache(capacity)
	if err != nil {
		return nil, err
	}
	if cfg.StartTimeout == 0 {
		cfg.StartTimeout = DefaultStartTimeout
	}
	if cfg.StopTimeout == 0 {
		cfg.StopTimeout = DefaultStopTimeout
	}
	if cfg.DrainTimeout == 0 {
		cfg.DrainTimeout = DefaultDrainTimeout
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	s := &CaptureSession{
		token:   nextStreamToken(),
		cfg:     cfg,
		tracker: NewGeometryTracker(),
		cache:   cache,
		events:  make(chan NativeStreamEvent, 8),
		done:    make(chan TeardownReport, 1),
		now:     now,
	}
	handle, err := cfg.Runner.StartStream(cfg.Selector.WindowID, s)
	if err != nil {
		return nil, fmt.Errorf("macos: creating stream for window %d: %w", cfg.Selector.WindowID, err)
	}
	s.mu.Lock()
	s.handle = handle
	s.mu.Unlock()

	ev := s.awaitEvent(cfg.StartTimeout, NativeEventStarted)
	switch {
	case ev.Kind == eventWaitTimeout:
		s.beginStop(PlatformFault{
			Category: FaultStream,
			Stage:    "streamStart",
			Err:      fmt.Errorf("start completion not received within %s", cfg.StartTimeout),
			Stream:   s.token,
		}, false)
		return nil, fmt.Errorf("macos: starting stream for window %d: no completion within %s", cfg.Selector.WindowID, cfg.StartTimeout)
	case ev.Err != nil:
		s.beginStop(PlatformFault{
			Category: FaultStream,
			Stage:    "streamStart",
			Err:      ev.Err,
			Stream:   s.token,
		}, false)
		return nil, fmt.Errorf("macos: starting stream for window %d: %w", cfg.Selector.WindowID, ev.Err)
	}
	s.mu.Lock()
	state := s.state
	s.mu.Unlock()
	switch state {
	case stateRunning:
		return s, nil
	case stateStopping, stateDone:
		// The stream already stopped on its own while the start result
		// was in flight.
		return nil, fmt.Errorf("macos: stream for window %d stopped during start", cfg.Selector.WindowID)
	default:
		return nil, errors.New("macos: capture session state inconsistent after start")
	}
}

// awaitEvent waits for one event of the wanted kind, stashing events
// of other kinds for later waits, and returns a waitTimeout pseudo
// event when the deadline passes.
func (s *CaptureSession) awaitEvent(timeout time.Duration, want NativeStreamEventKind) NativeStreamEvent {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		s.mu.Lock()
		for i, ev := range s.pending {
			if ev.Kind == want {
				s.pending = append(s.pending[:i], s.pending[i+1:]...)
				s.mu.Unlock()
				return ev
			}
		}
		s.mu.Unlock()
		select {
		case ev := <-s.events:
			if ev.Kind == want {
				return ev
			}
			s.mu.Lock()
			s.pending = append(s.pending, ev)
			s.mu.Unlock()
			continue
		case <-deadline.C:
			return NativeStreamEvent{Kind: eventWaitTimeout}
		}
	}
}

// Token returns the session's stream token.
func (s *CaptureSession) Token() StreamToken { return s.token }

// Geometry returns the current geometry version observed from frames
// (zero before the first frame with valid geometry).
func (s *CaptureSession) Geometry() GeometryVersion { return s.tracker.Version() }

// LatestFrame returns the newest cached frame observation.
func (s *CaptureSession) LatestFrame() (ImageObservation, bool) { return s.cache.Latest() }

// LastStatus returns the newest frame status observation and when it
// arrived.
func (s *CaptureSession) LastStatus() (FrameStatus, time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastStatus, s.lastStatusAt
}

// FirstCompleteFrameID returns the id of the first complete frame with
// pixel data; zero when none arrived. It is the frame evidence behind
// the screen recording assessment.
func (s *CaptureSession) FirstCompleteFrameID() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.firstCompleteFrameID
}

// CaptureStats is the session's live accounting snapshot.
type CaptureStats struct {
	// FramesReceived counts callbacks accepted while the intake was
	// open.
	FramesReceived int
	// FramesComplete counts complete frames with pixels offered to the
	// cache.
	FramesComplete int
	// FramesRejectedLate counts callbacks rejected after the intake
	// closed.
	FramesRejectedLate int
	// FramesGeometryInvalid counts complete frames with unusable
	// geometry.
	FramesGeometryInvalid int
	// LastStatus is the newest observed status.
	LastStatus FrameStatus
	// FinalFrame is the newest frame id.
	FinalFrame uint64
	// FirstCompleteFrameID is the first complete frame's id.
	FirstCompleteFrameID uint64
}

// Stats returns the session's live accounting snapshot.
func (s *CaptureSession) Stats() CaptureStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return CaptureStats{
		FramesReceived:        s.report.FramesReceived,
		FramesComplete:        s.report.FramesComplete,
		FramesRejectedLate:    s.report.FramesRejectedLate,
		FramesGeometryInvalid: s.report.FramesGeometryInvalid,
		LastStatus:            s.lastStatus,
		FinalFrame:            s.report.FinalFrame,
		FirstCompleteFrameID:  s.firstCompleteFrameID,
	}
}

// Done returns the teardown report channel; exactly one report is
// delivered after the session's teardown sequence completes (or gives
// up after a timeout).
func (s *CaptureSession) Done() <-chan TeardownReport { return s.done }

// Stop requests the idempotent stop: the intake closes immediately,
// the native stop is requested asynchronously, and the teardown
// sequence completes on its own; await Done for the report. Calling
// Stop more than once, or after a fault already began the stop, is a
// no-op.
func (s *CaptureSession) Stop() {
	s.beginStop(PlatformFault{}, true)
}

// beginStop closes the intake and starts the asynchronous teardown.
// A non-empty fault means the stop was caused by a platform fault; a
// user stop passes an empty fault. Faults at the streamStart stage are
// returned through StartCapture's error and not forwarded to OnEvent;
// every other fault is reported.
func (s *CaptureSession) beginStop(fault PlatformFault, user bool) {
	s.mu.Lock()
	if s.state == stateStopping || s.state == stateDone {
		s.mu.Unlock()
		return
	}
	s.state = stateStopping
	now := s.now()
	s.report.IntakeClosedAt = now
	if user {
		s.report.StopRequestedAt = now
	}
	if fault.Category != "" && fault.Stage == "streamStopped" {
		s.report.UnexpectedStopErr = fault.Err
	}
	geo := s.tracker.Version()
	s.mu.Unlock()

	if fault.Category != "" && fault.Stage != "streamStart" && s.cfg.OnEvent != nil {
		if fault.At.IsZero() {
			fault.At = now
		}
		fault.Stream = s.token
		fault.LastGeometry = geo
		s.cfg.OnEvent(fault)
	}
	go s.teardown()
}

// OnNativeFrame implements NativeStreamSink; it runs on the native
// frame queue thread. It assigns the frame id, tracks status and
// geometry, writes the bounded cache and reports geometry faults
// without ever blocking on the caller.
func (s *CaptureSession) OnNativeFrame(frame NativeFrame) {
	s.mu.Lock()
	if s.state == stateStopping || s.state == stateDone {
		s.report.FramesRejectedLate++
		s.mu.Unlock()
		return
	}
	s.frameSeq++
	frameID := s.frameSeq
	s.report.FinalFrame = frameID
	s.report.FramesReceived++
	s.lastStatus = frame.Status.FrameStatus()
	s.lastStatusAt = s.now()
	s.mu.Unlock()

	status := frame.Status.FrameStatus()
	if status != FrameComplete || frame.Pixels == nil {
		return
	}

	obs := ImageObservation{
		Stream:      s.token,
		FrameID:     frameID,
		Status:      status,
		RawStatus:   int32(frame.Status),
		Width:       frame.SurfaceWidth,
		Height:      frame.SurfaceHeight,
		BytesPerRow: frame.BytesPerRow,
		PixelFormat: "BGRA",
		Pixels:      frame.Pixels,
		ArrivedAt:   s.now(),
	}
	if frame.HasDisplayTime {
		obs.DisplayTimeMach = frame.DisplayTimeMach
		if s.cfg.Clock != nil {
			t := s.cfg.Clock.ToTime(frame.DisplayTimeMach)
			obs.DisplayTime = &t
		}
	}

	geoValid := false
	if frame.HasContentRect && frame.HasContentScale && frame.HasScaleFactor {
		windowFrame := s.cfg.WindowFrame
		if frame.HasScreenRect && !frame.ScreenRect.Empty() {
			windowFrame = frame.ScreenRect
		}
		geo := FrameGeometry{
			SurfaceWidth:  frame.SurfaceWidth,
			SurfaceHeight: frame.SurfaceHeight,
			BytesPerRow:   frame.BytesPerRow,
			ContentRect:   frame.ContentRect,
			ContentScale:  frame.ContentScale,
			ScaleFactor:   frame.ScaleFactor,
			WindowFrame:   windowFrame,
		}
		transform, err := NewGeometryTransform(geo)
		if err == nil {
			valid := transform.ValidRect()
			obs.ValidRect = &valid
			obs.Geometry = s.tracker.Observe(geo)
			geoValid = true
		} else if s.cfg.OnEvent != nil {
			// Attachments missing or contradicting each other: the
			// pixels stay valid evidence, but coordinates must not be
			// derived from this frame.
			s.cfg.OnEvent(PlatformFault{
				Category:  FaultGeometry,
				Stage:     "frameCallback",
				Err:       err,
				At:        s.now(),
				Stream:    s.token,
				LastFrame: frameID,
			})
		}
	}
	if !geoValid {
		obs.Geometry = s.tracker.Version()
	}
	obs.GeometryValid = geoValid

	s.mu.Lock()
	s.report.FramesComplete++
	if s.firstCompleteFrameID == 0 {
		s.firstCompleteFrameID = frameID
		s.report.FirstCompleteFrameID = frameID
	}
	if !geoValid {
		s.report.FramesGeometryInvalid++
	}
	s.mu.Unlock()
	s.cache.Offer(obs)
}

// OnNativeStreamEvent implements NativeStreamSink. The started event
// promotes the session to running; a stopped event arriving while the
// session runs (and not from its own stop request) is an unexpected
// stop that faults the session into teardown.
func (s *CaptureSession) OnNativeStreamEvent(event NativeStreamEvent) {
	switch event.Kind {
	case NativeEventStarted:
		s.mu.Lock()
		if s.state == stateStarting {
			s.state = stateRunning
		}
		s.mu.Unlock()
		s.forward(event)
	case NativeEventStopped:
		s.mu.Lock()
		running := s.state == stateRunning
		s.mu.Unlock()
		if running {
			err := event.Err
			if err == nil {
				err = errors.New("stream stopped unexpectedly")
			}
			s.beginStop(PlatformFault{
				Category: FaultStream,
				Stage:    "streamStopped",
				Err:      err,
				At:       s.now(),
			}, false)
			return
		}
		s.forward(event)
	case NativeEventDrained:
		s.forward(event)
	}
}

// forward queues a lifecycle event for the waiting side; the buffer is
// large enough for every event a session can produce, and a full
// buffer (which would mean a bug) drops instead of blocking the native
// callback thread.
func (s *CaptureSession) forward(event NativeStreamEvent) {
	select {
	case s.events <- event:
	default:
	}
}

// teardown runs the stop → drain → release sequence and delivers the
// report. It runs on its own goroutine and never blocks the caller or
// any main thread.
func (s *CaptureSession) teardown() {
	s.mu.Lock()
	handle := s.handle
	s.mu.Unlock()

	// Request the native stop; the completion arrives as an event.
	if err := s.cfg.Runner.RequestStop(handle); err != nil {
		s.mu.Lock()
		s.report.NativeStopRequested = false
		if s.report.NativeStopErr == nil {
			s.report.NativeStopErr = err
		}
		s.mu.Unlock()
	} else {
		s.mu.Lock()
		s.report.NativeStopRequested = true
		s.mu.Unlock()
	}

	stopped := false
	ev := s.awaitEvent(s.cfg.StopTimeout, NativeEventStopped)
	if ev.Kind == NativeEventStopped {
		stopped = true
		s.mu.Lock()
		s.report.NativeStoppedAt = s.now()
		if ev.Err != nil && s.report.NativeStopErr == nil {
			s.report.NativeStopErr = ev.Err
		}
		s.mu.Unlock()
	} else {
		s.mu.Lock()
		s.report.NativeStopTimedOut = true
		s.mu.Unlock()
	}

	// Drain the frame queue so in-flight callbacks finish before any
	// release.
	drained := false
	if err := s.cfg.Runner.Drain(handle); err == nil {
		if ev := s.awaitEvent(s.cfg.DrainTimeout, NativeEventDrained); ev.Kind == NativeEventDrained {
			drained = true
			s.mu.Lock()
			s.report.Drained = true
			s.report.DrainedAt = s.now()
			s.mu.Unlock()
		}
	}

	// Release the native objects only when the stop completed and the
	// queue drained; otherwise the objects stay alive and late
	// callbacks keep being rejected (releasing while callbacks may
	// still be in flight is unsafe).
	if stopped && drained {
		s.cfg.Runner.Release(handle)
		s.mu.Lock()
		s.report.ReleasedAt = s.now()
		s.mu.Unlock()
	}

	s.mu.Lock()
	s.report.FinalGeometry = s.tracker.Version()
	report := s.report
	s.state = stateDone
	s.mu.Unlock()
	select {
	case s.done <- report:
	default:
	}
}
