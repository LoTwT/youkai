package macos_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	macos "github.com/LoTwT/youkai/internal/platform/macos"
)

// Capture session tests (T2 §4.2, §4.3): the session drives a fake
// native runner through start, frames, stop, drain and release. The
// interesting semantics are the ones the architecture demands: the
// stop sequence is asynchronous and honest (stop requested, native
// stopped and callbacks drained are reported separately; a timeout
// keeps the native objects alive instead of releasing them under
// in-flight callbacks), the intake closes the moment a stop begins so
// late callbacks are counted and rejected, frames from an old stream
// never reach a new session, and frame geometry is versioned.

// fakeRunner is a scriptable NativeStreamRunner. Lifecycle events are
// emitted synchronously from the runner calls, which keeps the tests
// deterministic: every event the session waits for is already in its
// buffered channel by the time the wait runs.
type fakeRunner struct {
	// startErr, when set, fails StartStream synchronously.
	startErr error
	// startEventErr, when set, is delivered as the started event.
	startEventErr error
	// noStartEvent suppresses the started event entirely (timeout path).
	noStartEvent bool
	// hangStop suppresses the stopped event (stop timeout path).
	hangStop bool
	// stopErr is delivered as the stopped event's error.
	stopErr error
	// noDrain suppresses the drained event.
	noDrain bool

	sink  macos.NativeStreamSink
	calls []string
	mu    sync.Mutex
}

// record appends a call under the runner lock; the teardown runs on
// its own goroutine while the test polls the calls, so the fake must
// be safe for concurrent use (the real runner's concurrency lives in
// the native layer).
func (r *fakeRunner) record(call string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, call)
}

func (r *fakeRunner) recorded() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...)
}

func (r *fakeRunner) StartStream(windowID uint32, sink macos.NativeStreamSink) (macos.NativeStreamHandle, error) {
	r.record("start")
	if r.startErr != nil {
		return nil, r.startErr
	}
	r.mu.Lock()
	r.sink = sink
	r.mu.Unlock()
	handle := r
	if !r.noStartEvent {
		sink.OnNativeStreamEvent(macos.NativeStreamEvent{Kind: macos.NativeEventStarted, Err: r.startEventErr})
	}
	return handle, nil
}

func (r *fakeRunner) RequestStop(handle macos.NativeStreamHandle) error {
	r.record("requestStop")
	r.mu.Lock()
	sink := r.sink
	r.mu.Unlock()
	if !r.hangStop {
		sink.OnNativeStreamEvent(macos.NativeStreamEvent{Kind: macos.NativeEventStopped, Err: r.stopErr})
	}
	return nil
}

func (r *fakeRunner) Drain(handle macos.NativeStreamHandle) error {
	r.record("drain")
	r.mu.Lock()
	sink := r.sink
	r.mu.Unlock()
	if !r.noDrain {
		sink.OnNativeStreamEvent(macos.NativeStreamEvent{Kind: macos.NativeEventDrained})
	}
	return nil
}

func (r *fakeRunner) Release(handle macos.NativeStreamHandle) {
	r.record("release")
}

func (r *fakeRunner) hasCall(call string) bool {
	for _, c := range r.recorded() {
		if c == call {
			return true
		}
	}
	return false
}

// waitDone waits for the teardown report.
func waitDone(t *testing.T, s *macos.CaptureSession) macos.TeardownReport {
	t.Helper()
	select {
	case rep := <-s.Done():
		return rep
	case <-time.After(5 * time.Second):
		t.Fatal("teardown report never arrived")
		return macos.TeardownReport{}
	}
}

func waitStats(t *testing.T, s *macos.CaptureSession, want func(macos.CaptureStats) bool) {
	t.Helper()
	for i := 0; i < 100_000; i++ {
		if want(s.Stats()) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("stats condition never reached: %+v", s.Stats())
}

func testSelector() macos.WindowSelector {
	return macos.WindowSelector{BundleID: "com.example.game", PID: 200, WindowID: 42}
}

// completeFrame builds a consistent complete frame for a 1280×720
// point window at 2× scale.
func completeFrame(id uint64, window macos.PointRect) macos.NativeFrame {
	return macos.NativeFrame{
		Status:          macos.NativeFrameComplete,
		DisplayTimeMach: 10_000 + id,
		HasDisplayTime:  true,
		SurfaceWidth:    2560,
		SurfaceHeight:   1440,
		BytesPerRow:     10240,
		ContentRect:     macos.PointRect{X: 0, Y: 0, W: 1280, H: 720},
		HasContentRect:  true,
		ContentScale:    1,
		HasContentScale: true,
		ScaleFactor:     2,
		HasScaleFactor:  true,
		ScreenRect:      window,
		HasScreenRect:   true,
		Pixels:          make([]byte, 10240*1440),
	}
}

func captureConfig(runner macos.NativeStreamRunner) macos.CaptureConfig {
	return macos.CaptureConfig{
		Runner:      runner,
		Selector:    testSelector(),
		WindowFrame: macos.PointRect{X: 100, Y: 50, W: 1280, H: 720},
	}
}

func TestCaptureLifecycleFramesAndGeometryVersioning(t *testing.T) {
	runner := &fakeRunner{}
	clockSource := &fakeClockSource{ticks: 1000, tb: macos.MachTimebaseIdentity}
	anchor := time.Now()
	s, err := macos.StartCapture(macos.CaptureConfig{
		Runner:      runner,
		Selector:    testSelector(),
		WindowFrame: macos.PointRect{X: 100, Y: 50, W: 1280, H: 720},
		Clock:       macos.NewMachClockFromAnchor(clockSource, anchor),
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if s.Token() == 0 {
		t.Fatal("session token zero")
	}
	if s.Geometry() != 0 {
		t.Fatalf("geometry before frames %d", s.Geometry())
	}

	// One complete frame: cached, geometry version 1, display time
	// converted relative to the anchor (1000 ticks anchor + 10001
	// ticks frame → +9001ns).
	frame := completeFrame(1, macos.PointRect{X: 100, Y: 50, W: 1280, H: 720})
	runner.sink.OnNativeFrame(frame)
	waitStats(t, s, func(st macos.CaptureStats) bool { return st.FramesComplete == 1 })
	obs, ok := s.LatestFrame()
	if !ok || obs.FrameID != 1 {
		t.Fatalf("latest frame=%d status=%s ok=%v", obs.FrameID, obs.Status, ok)
	}
	if !obs.GeometryValid || obs.Geometry != 1 {
		t.Fatalf("observation geometry valid=%v version=%d", obs.GeometryValid, obs.Geometry)
	}
	if obs.ValidRect == nil || obs.ValidRect.W != 2560 {
		t.Fatalf("valid rect %+v", obs.ValidRect)
	}
	if obs.DisplayTime == nil {
		t.Fatal("display time not converted")
	}
	if d := obs.DisplayTime.Sub(anchor); d != 9001*time.Nanosecond {
		t.Fatalf("display time delta %v", d)
	}
	if obs.DisplayTimeMach != 10001 || obs.PixelFormat != "BGRA" {
		t.Fatalf("observation displayTimeMach=%d pixelFormat=%s", obs.DisplayTimeMach, obs.PixelFormat)
	}

	// An idle frame updates the status observation but not the cache.
	runner.sink.OnNativeFrame(macos.NativeFrame{Status: macos.NativeFrameIdle})
	waitStats(t, s, func(st macos.CaptureStats) bool { return st.LastStatus == macos.FrameIdle })
	if _, ok := s.LatestFrame(); !ok {
		t.Fatal("idle frame evicted the cached image")
	}
	st := s.Stats()
	if st.FramesComplete != 1 {
		t.Fatalf("idle frame counted as complete: %+v", st)
	}

	// The window moved: the next frame bumps the geometry version; the
	// old version becomes stale. The idle callback consumed a frame id
	// without being cached, so the second complete frame is id 3.
	runner.sink.OnNativeFrame(completeFrame(3, macos.PointRect{X: 300, Y: 150, W: 1280, H: 720}))
	waitStats(t, s, func(st macos.CaptureStats) bool { return st.FramesComplete == 2 })
	if s.Geometry() != 2 {
		t.Fatalf("window move did not bump geometry: %d", s.Geometry())
	}
	obs, _ = s.LatestFrame()
	if obs.Geometry != 2 || obs.FrameID != 3 {
		t.Fatalf("observation after move frame=%d geometry=%d", obs.FrameID, obs.Geometry)
	}

	// Stop: the report separates requested, stopped, drained and
	// released, and the runner saw the whole sequence.
	s.Stop()
	rep := waitDone(t, s)
	if rep.StopRequestedAt.IsZero() || rep.IntakeClosedAt.IsZero() {
		t.Fatalf("report timestamps %+v", rep)
	}
	if !rep.NativeStopRequested || rep.NativeStoppedAt.IsZero() {
		t.Fatalf("native stop not reported: %+v", rep)
	}
	if !rep.Drained || rep.ReleasedAt.IsZero() {
		t.Fatalf("drain/release not reported: %+v", rep)
	}
	if rep.FramesReceived != 3 || rep.FramesComplete != 2 {
		t.Fatalf("report counts %+v", rep)
	}
	if rep.FinalGeometry != 2 || rep.FinalFrame != 3 {
		t.Fatalf("report identity %+v", rep)
	}
	if rep.FirstCompleteFrameID != 1 {
		t.Fatalf("first complete frame %d", rep.FirstCompleteFrameID)
	}
	if !runner.hasCall("requestStop") || !runner.hasCall("drain") || !runner.hasCall("release") {
		t.Fatalf("runner calls %v", runner.recorded())
	}
}

func TestCaptureLateFramesRejectedAfterStop(t *testing.T) {
	runner := &fakeRunner{}
	s, err := macos.StartCapture(captureConfig(runner))
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	s.Stop()
	// The intake closed with the stop: a frame arriving after it is
	// counted and rejected, never cached.
	runner.sink.OnNativeFrame(completeFrame(1, macos.PointRect{X: 100, Y: 50, W: 1280, H: 720}))
	waitStats(t, s, func(st macos.CaptureStats) bool { return st.FramesRejectedLate == 1 })
	if _, ok := s.LatestFrame(); ok {
		t.Fatal("late frame reached the cache")
	}
	rep := waitDone(t, s)
	if rep.FramesReceived != 0 || rep.FramesComplete != 0 {
		t.Fatalf("late frame counted as received: %+v", rep)
	}
}

func TestCaptureOldStreamFramesNeverReachNewSession(t *testing.T) {
	runnerA := &fakeRunner{}
	a, err := macos.StartCapture(captureConfig(runnerA))
	if err != nil {
		t.Fatalf("start a: %v", err)
	}
	runnerB := &fakeRunner{}
	b, err := macos.StartCapture(captureConfig(runnerB))
	if err != nil {
		t.Fatalf("start b: %v", err)
	}
	if a.Token() == b.Token() {
		t.Fatal("sessions share a token")
	}
	// The old stream's sink delivers after the new session exists: the
	// new session sees nothing.
	runnerA.sink.OnNativeFrame(completeFrame(1, macos.PointRect{X: 100, Y: 50, W: 1280, H: 720}))
	if st := b.Stats(); st.FramesReceived != 0 {
		t.Fatalf("new session received the old stream's frame: %+v", st)
	}
	if _, ok := b.LatestFrame(); ok {
		t.Fatal("old stream frame cached in the new session")
	}
	a.Stop()
	b.Stop()
	waitDone(t, a)
	waitDone(t, b)
}

func TestCaptureStartFailureStillTearsDown(t *testing.T) {
	runner := &fakeRunner{startEventErr: errors.New("not authorized")}
	_, err := macos.StartCapture(captureConfig(runner))
	if err == nil {
		t.Fatal("async start failure not returned")
	}
	// The session object is gone with the error; the teardown still
	// ran on the runner.
	for i := 0; i < 100_000 && !runner.hasCall("release"); i++ {
		time.Sleep(time.Millisecond)
	}
	if !runner.hasCall("requestStop") || !runner.hasCall("drain") || !runner.hasCall("release") {
		t.Fatalf("teardown sequence incomplete: %v", runner.recorded())
	}
}

func TestCaptureSyncCreationFailureHasNothingToTearDown(t *testing.T) {
	runner := &fakeRunner{startErr: errors.New("window gone")}
	if _, err := macos.StartCapture(captureConfig(runner)); err == nil {
		t.Fatal("sync start failure not returned")
	}
	if len(runner.recorded()) != 1 || runner.calls[0] != "start" {
		t.Fatalf("unexpected runner calls %v", runner.recorded())
	}
}

func TestCaptureStartTimeoutFails(t *testing.T) {
	runner := &fakeRunner{noStartEvent: true}
	cfg := captureConfig(runner)
	cfg.StartTimeout = 20 * time.Millisecond
	if _, err := macos.StartCapture(cfg); err == nil {
		t.Fatal("start timeout not returned")
	}
	for i := 0; i < 100_000 && !runner.hasCall("release"); i++ {
		time.Sleep(time.Millisecond)
	}
	if !runner.hasCall("release") {
		t.Fatalf("teardown after start timeout incomplete: %v", runner.recorded())
	}
}

func TestCaptureUnexpectedStopIsFaultAndTeardown(t *testing.T) {
	runner := &fakeRunner{}
	faults := make(chan macos.PlatformFault, 4)
	cfg := captureConfig(runner)
	cfg.OnEvent = func(f macos.PlatformFault) {
		select {
		case faults <- f:
		default:
		}
	}
	s, err := macos.StartCapture(cfg)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	// The stream stops on its own (didStopWithError): the session
	// faults and tears down without a user stop.
	runner.sink.OnNativeStreamEvent(macos.NativeStreamEvent{Kind: macos.NativeEventStopped, Err: errors.New("user stopped sharing")})
	rep := waitDone(t, s)
	if rep.StopRequestedAt != (time.Time{}) {
		t.Fatalf("unexpected stop recorded as user stop: %+v", rep)
	}
	if rep.UnexpectedStopErr == nil {
		t.Fatal("unexpected stop error not reported")
	}
	if !rep.ReleasedAt.IsZero() == false && !runner.hasCall("release") {
		t.Fatalf("teardown incomplete: %v", runner.recorded())
	}
	select {
	case f := <-faults:
		if f.Category != macos.FaultStream || f.Stage != "streamStopped" {
			t.Fatalf("fault %+v", f)
		}
		if f.Stream != s.Token() {
			t.Fatalf("fault stream %d", f.Stream)
		}
	default:
		t.Fatal("unexpected stop not reported to OnEvent")
	}
}

func TestCaptureStopTimeoutKeepsNativeObjects(t *testing.T) {
	runner := &fakeRunner{hangStop: true}
	cfg := captureConfig(runner)
	// A short stop bound so the test does not wait the default.
	cfg.StopTimeout = 20 * time.Millisecond
	s, err := macos.StartCapture(cfg)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	s.Stop()
	rep := waitDone(t, s)
	if !rep.NativeStopTimedOut {
		t.Fatalf("stop timeout not reported: %+v", rep)
	}
	if !rep.NativeStoppedAt.IsZero() {
		t.Fatalf("stop completion invented: %+v", rep)
	}
	// Releasing while callbacks may still be in flight is unsafe: the
	// objects stay alive and late frames keep being rejected.
	if !rep.ReleasedAt.IsZero() || runner.hasCall("release") {
		t.Fatalf("release after stop timeout: %+v calls=%v", rep, runner.recorded())
	}
	runner.sink.OnNativeFrame(completeFrame(1, macos.PointRect{X: 100, Y: 50, W: 1280, H: 720}))
	waitStats(t, s, func(st macos.CaptureStats) bool { return st.FramesRejectedLate == 1 })
}

func TestCaptureInvalidGeometryFrameIsFaultNotInput(t *testing.T) {
	runner := &fakeRunner{}
	faults := make(chan macos.PlatformFault, 4)
	cfg := captureConfig(runner)
	cfg.OnEvent = func(f macos.PlatformFault) {
		select {
		case faults <- f:
		default:
		}
	}
	s, err := macos.StartCapture(cfg)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	// Complete frame whose attachments contradict each other: the
	// pixels stay evidence, but the geometry is invalid and a fault is
	// reported; coordinates must not be derived from it.
	bad := completeFrame(1, macos.PointRect{X: 100, Y: 50, W: 1280, H: 720})
	bad.ScaleFactor = 0
	bad.HasScaleFactor = true
	runner.sink.OnNativeFrame(bad)
	waitStats(t, s, func(st macos.CaptureStats) bool { return st.FramesGeometryInvalid == 1 })
	obs, ok := s.LatestFrame()
	if !ok || obs.GeometryValid {
		t.Fatalf("invalid geometry observation cached as valid: frame=%d geometryValid=%v", obs.FrameID, obs.GeometryValid)
	}
	if obs.ValidRect != nil {
		t.Fatalf("invalid geometry produced a valid rect: %+v", obs.ValidRect)
	}
	select {
	case f := <-faults:
		if f.Category != macos.FaultGeometry || f.LastFrame != 1 {
			t.Fatalf("fault %+v", f)
		}
	default:
		t.Fatal("geometry fault not reported")
	}
	s.Stop()
	waitDone(t, s)
}
