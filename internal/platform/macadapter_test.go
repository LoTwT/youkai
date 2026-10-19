package platform

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"sync"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/platform/macos"
)

// macOS adapter tests (M-series), all fake-driven: no native screen,
// stream, Quartz or Carbon call is ever made. The fakes mirror the
// macos base's native interfaces; the AX focus probe has no surface
// here at all — the adapter structurally never queries AX (实测轮 ①:
// the system-wide focus query is unusable on the target machine).

// ---- fakes ----

type enumSnapshot struct {
	windows  []macos.NativeWindow
	displays []macos.NativeDisplay
	err      error
}

type fakeMacEnum struct {
	mu        sync.Mutex
	snapshots []enumSnapshot
	calls     int
}

func (f *fakeMacEnum) push(s enumSnapshot) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.snapshots = append(f.snapshots, s)
}

func (f *fakeMacEnum) replaceLast(s enumSnapshot) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.snapshots) > 0 {
		f.snapshots[len(f.snapshots)-1] = s
	}
}

func (f *fakeMacEnum) EnumerateWindows() ([]macos.NativeWindow, []macos.NativeDisplay, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if len(f.snapshots) == 0 {
		return nil, nil, errors.New("enumeration: no snapshot scripted")
	}
	s := f.snapshots[len(f.snapshots)-1]
	return s.windows, s.displays, s.err
}

type fakeMacRunner struct {
	mu        sync.Mutex
	sink      macos.NativeStreamSink
	starts    int
	stops     int
	drains    int
	releases  int
	startErr  error
	noStopEvt bool
	windowID  uint32
	// autoFrame, when set, is pushed as the first frame callback during
	// StartStream, so Open's first-complete-frame wait sees one without
	// the test racing it.
	autoFrame *macos.NativeFrame
}

func (r *fakeMacRunner) StartStream(windowID uint32, sink macos.NativeStreamSink) (macos.NativeStreamHandle, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.starts++
	r.windowID = windowID
	if r.startErr != nil {
		return nil, r.startErr
	}
	r.sink = sink
	// The start completion arrives before StartCapture's wait begins.
	sink.OnNativeStreamEvent(macos.NativeStreamEvent{Kind: macos.NativeEventStarted})
	if r.autoFrame != nil {
		sink.OnNativeFrame(*r.autoFrame)
	}
	return "fake-stream", nil
}

func (r *fakeMacRunner) RequestStop(h macos.NativeStreamHandle) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stops++
	if !r.noStopEvt && r.sink != nil {
		r.sink.OnNativeStreamEvent(macos.NativeStreamEvent{Kind: macos.NativeEventStopped})
	}
	return nil
}

func (r *fakeMacRunner) Drain(h macos.NativeStreamHandle) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.drains++
	if r.sink != nil {
		r.sink.OnNativeStreamEvent(macos.NativeStreamEvent{Kind: macos.NativeEventDrained})
	}
	return nil
}

func (r *fakeMacRunner) Release(h macos.NativeStreamHandle) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.releases++
	r.sink = nil
}

func (r *fakeMacRunner) counts() (starts, stops, drains, releases int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.starts, r.stops, r.drains, r.releases
}

func (r *fakeMacRunner) frame(nf macos.NativeFrame) {
	r.mu.Lock()
	sink := r.sink
	r.mu.Unlock()
	if sink != nil {
		sink.OnNativeFrame(nf)
	}
}

func (r *fakeMacRunner) event(ev macos.NativeStreamEvent) {
	r.mu.Lock()
	sink := r.sink
	r.mu.Unlock()
	if sink != nil {
		sink.OnNativeStreamEvent(ev)
	}
}

type fakeMacPoster struct {
	mu      sync.Mutex
	events  []string
	failOne map[string]int
}

func (p *fakeMacPoster) post(name string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.failOne[name] > 0 {
		p.failOne[name]--
		return errors.New("creation failed: " + name)
	}
	p.events = append(p.events, name)
	return nil
}

func (p *fakeMacPoster) PostMouseMoved(pt macos.Point) error {
	return p.post(fmt.Sprintf("move(%.2f,%.2f)", pt.X, pt.Y))
}

func (p *fakeMacPoster) PostMouseButton(b macos.MouseButton, down bool, at macos.Point) error {
	return p.post(fmt.Sprintf("%s(%.2f,%.2f)", buttonName(down), at.X, at.Y))
}

func (p *fakeMacPoster) PostKey(c macos.KeyCode, down bool) error {
	return p.post(fmt.Sprintf("key%d-%v", c, down))
}

func (p *fakeMacPoster) PostScroll(dy float64) error { return p.post("scroll") }

func (p *fakeMacPoster) posted() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.events...)
}

func buttonName(down bool) string {
	if down {
		return "button-down"
	}
	return "button-up"
}

type fakeMacFG struct {
	mu  sync.Mutex
	pid int32
	err error
}

type fakeMacActivator struct {
	mu    sync.Mutex
	pid   int32
	calls int
	err   error
}

func (a *fakeMacActivator) setErr(err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.err = err
}

func (a *fakeMacActivator) counts() (pid int32, calls int, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.pid, a.calls, a.err
}

func (a *fakeMacActivator) ActivatePID(pid int32) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.pid, a.calls = pid, a.calls+1
	return a.err
}

func (f *fakeMacFG) set(pid int32, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pid, f.err = pid, err
}

func (f *fakeMacFG) FrontmostPID() (int32, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pid, f.err
}

type fakeMacPerms struct {
	mu         sync.Mutex
	trusted    bool
	allowed    bool
	trustErr   error
	allowedErr error
}

func (f *fakeMacPerms) set(trusted, allowed bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.trusted, f.allowed = trusted, allowed
}

func (f *fakeMacPerms) AccessibilityTrusted() (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.trusted, f.trustErr
}

func (f *fakeMacPerms) EventPostAllowed() (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.allowed, f.allowedErr
}

type fakeMacHotkey struct {
	mu         sync.Mutex
	registered int
	unreg      int
	err        error
	unregErr   error
}

func (f *fakeMacHotkey) Register(spec macos.HotKeySpec, onTrigger func()) (macos.NativeHotKeyHandle, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.registered++
	if f.err != nil {
		return nil, f.err
	}
	return "fake-hotkey", nil
}

func (f *fakeMacHotkey) Unregister(h macos.NativeHotKeyHandle) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unreg++
	return f.unregErr
}

func (f *fakeMacHotkey) counts() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.registered, f.unreg
}

// sleepRecorder records every sleep without waiting.
type sleepRecorder struct {
	mu    sync.Mutex
	spans []time.Duration
}

func (r *sleepRecorder) sleep(d time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.spans = append(r.spans, d)
}

func (r *sleepRecorder) recorded() []time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]time.Duration(nil), r.spans...)
}

// fakeMachClock supplies deterministic ticks to the mach clock anchor.
type fakeMachClock struct {
	ticks uint64
}

func (c *fakeMachClock) NowTicks() uint64 { return c.ticks }
func (c *fakeMachClock) Timebase() macos.MachTimebase {
	return macos.MachTimebaseIdentity
}

// fakeGate is an open gate until failAfter checks have passed (when
// failAfter > 0), closed from the next check on; set flips it manually.
type fakeGate struct {
	mu        sync.Mutex
	open      bool
	failAfter int
	checks    int
}

func (g *fakeGate) InputPermitted() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.checks++
	if g.failAfter > 0 && g.checks > g.failAfter {
		return false
	}
	return g.open
}

func (g *fakeGate) set(open bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.open = open
}

// ---- the test world ----

const (
	testBundle = "com.miHoYo.cloudgames.Nap"
	testPID    = int32(5001)
	testWinID  = uint32(100)
	otherPID   = int32(7777)
)

func testDisplays() []macos.NativeDisplay {
	return []macos.NativeDisplay{
		{DisplayID: 1, Width: 1920, Height: 1080, Frame: macos.PointRect{X: 0, Y: 0, W: 1920, H: 1080}},
	}
}

func mainWindowFrame() macos.PointRect {
	return macos.PointRect{X: 100, Y: 50, W: 320, H: 240}
}

func targetWindow() macos.NativeWindow {
	return macos.NativeWindow{
		WindowID: testWinID, PID: testPID, BundleID: testBundle, Title: "云·绝区零",
		Frame: mainWindowFrame(), OnScreen: true, Layer: 0,
	}
}

func bystanderWindow() macos.NativeWindow {
	return macos.NativeWindow{
		WindowID: 900, PID: otherPID, BundleID: "com.example.finder", Title: "Finder",
		Frame: macos.PointRect{X: 0, Y: 0, W: 300, H: 200}, OnScreen: true, Layer: 0,
	}
}

type macWorld struct {
	adapter  *MacAdapter
	enum     *fakeMacEnum
	runner   *fakeMacRunner
	poster   *fakeMacPoster
	fg       *fakeMacFG
	perms    *fakeMacPerms
	hotkey   *fakeMacHotkey
	gate     *fakeGate
	sleeps   *sleepRecorder
	faultsMu sync.Mutex
	faults   []*Fault
	stops    int
	activate *fakeMacActivator
}

func newMacWorld(t *testing.T) *macWorld {
	t.Helper()
	w := &macWorld{
		enum:     &fakeMacEnum{},
		runner:   &fakeMacRunner{},
		poster:   &fakeMacPoster{},
		fg:       &fakeMacFG{pid: testPID},
		perms:    &fakeMacPerms{trusted: true, allowed: true},
		hotkey:   &fakeMacHotkey{},
		gate:     &fakeGate{open: true},
		sleeps:   &sleepRecorder{},
		activate: &fakeMacActivator{},
	}
	w.enum.push(enumSnapshot{
		windows:  []macos.NativeWindow{targetWindow(), bystanderWindow()},
		displays: testDisplays(),
	})
	adapter, err := NewMacAdapter(MacNatives{
		Enum:     w.enum,
		Runner:   w.runner,
		Poster:   w.poster,
		FG:       w.fg,
		Activate: w.activate,
		Perms:    w.perms,
		Hotkey:   w.hotkey,
		Clock:    &fakeMachClock{ticks: 12345},
	})
	if err != nil {
		t.Fatalf("adapter: %v", err)
	}
	adapter.WatchEvery = 2 * time.Millisecond
	adapter.FramePoll = time.Millisecond
	adapter.Sleep = w.sleeps.sleep
	w.adapter = adapter
	return w
}

func (w *macWorld) config() SessionConfig {
	return SessionConfig{
		Target: TargetSpec{BundleID: testBundle},
		Hotkey: HotkeySpec{Key: KeyS, Ctrl: true, Alt: true, Meta: true},
		OnStop: func() { w.stops++ },
		Gate:   w.gate,
		OnFault: func(f *Fault) {
			w.faultsMu.Lock()
			w.faults = append(w.faults, f)
			w.faultsMu.Unlock()
		},
	}
}

func (w *macWorld) delivered() []string {
	w.faultsMu.Lock()
	defer w.faultsMu.Unlock()
	out := make([]string, len(w.faults))
	for i, f := range w.faults {
		out[i] = f.Code
	}
	return out
}

func (w *macWorld) faultCount(code string) int {
	w.faultsMu.Lock()
	defer w.faultsMu.Unlock()
	n := 0
	for _, f := range w.faults {
		if f.Code == code {
			n++
		}
	}
	return n
}

// smallGeo is a consistent frame geometry: surface 320×240 px at scale
// 1, content the whole surface, window at (100,50) 320×240 pt. The size
// is the smallest target candidate the platform accepts.
func smallGeo() macos.FrameGeometry {
	return macos.FrameGeometry{
		SurfaceWidth: 320, SurfaceHeight: 240, BytesPerRow: 1280,
		ContentRect:  macos.PointRect{X: 0, Y: 0, W: 320, H: 240},
		ContentScale: 1, ScaleFactor: 1,
		WindowFrame: mainWindowFrame(),
	}
}

// smallFrame builds one complete frame for geo with a recognizable
// pixel pattern: (0,0) is BGRA (10,20,30,·), every other pixel (1,2,3,·).
func smallFrame(geo macos.FrameGeometry, displayMach uint64) macos.NativeFrame {
	nf := macos.NativeFrame{
		Status:          macos.NativeFrameComplete,
		DisplayTimeMach: displayMach,
		HasDisplayTime:  displayMach > 0,
		SurfaceWidth:    geo.SurfaceWidth, SurfaceHeight: geo.SurfaceHeight, BytesPerRow: geo.BytesPerRow,
		ContentRect: geo.ContentRect, HasContentRect: true,
		ContentScale: geo.ContentScale, HasContentScale: true,
		ScaleFactor: geo.ScaleFactor, HasScaleFactor: true,
		ScreenRect: geo.WindowFrame, HasScreenRect: true,
		Pixels: make([]byte, geo.BytesPerRow*geo.SurfaceHeight),
	}
	for i := range nf.Pixels {
		nf.Pixels[i] = byte(i%7) + 1
	}
	// (0,0): B=10, G=20, R=30.
	nf.Pixels[0], nf.Pixels[1], nf.Pixels[2], nf.Pixels[3] = 10, 20, 30, 0
	return nf
}

// openReady opens a session and drives it to a locked baseline with one
// complete frame, the common starting point of the M-tests.
func (w *macWorld) openReady(t *testing.T) Session {
	t.Helper()
	frame := smallFrame(smallGeo(), 0)
	w.runner.mu.Lock()
	w.runner.autoFrame = &frame
	w.runner.mu.Unlock()
	sess, err := w.adapter.Open(context.Background(), w.config())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	waitFor(t, 2*time.Second, "ready observation", func() bool {
		obs, err := sess.Observe()
		return err == nil && obs.Ready
	})
	if _, err := sess.LockBaseline(); err != nil {
		t.Fatalf("lock baseline: %v", err)
	}
	return sess
}

// waitFor polls cond until it holds or the timeout passes.
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

// ---- tests ----

// M2: the preflight names the missing capability.
func TestMacPreflight(t *testing.T) {
	w := newMacWorld(t)
	pf := w.adapter.Preflight()
	if f := pf.Fault(); f != nil {
		t.Fatalf("healthy preflight: %+v", f)
	}
	w.perms.set(false, true)
	f := w.adapter.Preflight().Fault()
	if f == nil || f.Code != CodePermissionMissing || f.Class != ClassSetup {
		t.Fatalf("missing accessibility: %+v", f)
	}
	if !contains(f.Message, "accessibilityTrust") {
		t.Fatalf("the check that failed must be named: %q", f.Message)
	}
	w.perms.set(true, false)
	f = w.adapter.Preflight().Fault()
	if f == nil || !contains(f.Message, "eventSynthesis") {
		t.Fatalf("missing event synthesis: %+v", f)
	}
	w.perms.mu.Lock()
	w.perms.trustErr = errors.New("query failed")
	w.perms.allowedErr = errors.New("query failed")
	w.perms.mu.Unlock()
	f = w.adapter.Preflight().Fault()
	if f == nil || !contains(f.Message, "unverifiable") {
		t.Fatalf("failed queries count as missing: %+v", f)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || index(s, sub) >= 0)
}

func index(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// The macOS session exposes the optional Activator capability: a
// user-started scan activates the locked target's process once, and a
// refused activation surfaces as an error the controller can log.
func TestMacSessionActivate(t *testing.T) {
	w := newMacWorld(t)
	sess := w.openReady(t)
	defer sess.Close()
	activator, ok := sess.(Activator)
	if !ok {
		t.Fatal("the mac session must implement Activator")
	}
	if err := activator.Activate(); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	pid, calls, _ := w.activate.counts()
	if pid != testPID || calls != 1 {
		t.Fatalf("activation pid=%d calls=%d, want pid=%d calls=1", pid, calls, testPID)
	}
	w.activate.setErr(errors.New("activation refused"))
	if err := activator.Activate(); err == nil {
		t.Fatal("a refused activation must surface")
	}
}

// A cached frame must not be served to a request whose context already
// ended: the traversal's bounded waits rely on the context, not on the
// stream briefly running dry.
func TestMacFrameHonorsCanceledContext(t *testing.T) {
	w := newMacWorld(t)
	sess := w.openReady(t)
	defer sess.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := sess.Frame(ctx, FrameRequest{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Frame error = %v, want context.Canceled", err)
	}
}

// M3/M4/M1-adapter: open outcomes and rollback.
func TestMacOpenOutcomesAndRollback(t *testing.T) {
	t.Run("hotkey failure leaves everything else untouched", func(t *testing.T) {
		w := newMacWorld(t)
		w.hotkey.mu.Lock()
		w.hotkey.err = errors.New("RegisterEventHotKey: conflict")
		w.hotkey.mu.Unlock()
		_, err := w.adapter.Open(context.Background(), w.config())
		var f *Fault
		if !errors.As(err, &f) || f.Code != CodeHotkeyUnavailable || f.Class != ClassSetup {
			t.Fatalf("want hotkeyUnavailable, got %v", err)
		}
		if starts, _, _, _ := w.runner.counts(); starts != 0 {
			t.Fatalf("the stream must never start after a hotkey failure, started %d", starts)
		}
	})

	t.Run("stream start failure unregisters the hotkey", func(t *testing.T) {
		w := newMacWorld(t)
		w.runner.mu.Lock()
		w.runner.startErr = errors.New("stream creation failed")
		w.runner.mu.Unlock()
		_, err := w.adapter.Open(context.Background(), w.config())
		var f *Fault
		if !errors.As(err, &f) || f.Code != CodeScreenRecordingUnavailable {
			t.Fatalf("want screenRecordingUnavailable, got %v", err)
		}
		if _, unreg := w.hotkey.counts(); unreg != 1 {
			t.Fatalf("the hotkey must be unregistered on rollback, got %d", unreg)
		}
	})

	t.Run("no other-process windows report screen recording", func(t *testing.T) {
		// OwnPID is scripted to the target's pid and the enumeration
		// lists nothing besides "our own" windows: the shape a missing
		// screen-recording permission produces. The enumeration evidence
		// fails fast — before any first-frame wait.
		w := newMacWorld(t)
		w.enum.push(enumSnapshot{
			windows:  []macos.NativeWindow{targetWindow()},
			displays: testDisplays(),
		})
		adapter, err := NewMacAdapter(MacNatives{
			Enum: w.enum, Runner: w.runner, Poster: w.poster, FG: w.fg,
			Perms: w.perms, Hotkey: w.hotkey, Clock: &fakeMachClock{},
			OwnPID: testPID,
		})
		if err != nil {
			t.Fatal(err)
		}
		adapter.Sleep = func(time.Duration) {}
		adapter.FirstFrameWait = time.Hour // the evidence must not need it
		started := time.Now()
		_, err = adapter.Open(context.Background(), w.config())
		var f *Fault
		if !errors.As(err, &f) || f.Code != CodeScreenRecordingUnavailable {
			t.Fatalf("want screenRecordingUnavailable, got %v", err)
		}
		if !contains(f.Message, "enumeration") {
			t.Fatalf("the enumeration stage must be named: %q", f.Message)
		}
		if time.Since(started) > time.Second {
			t.Fatalf("the enumeration evidence must fail fast, took %s", time.Since(started))
		}
		// The rollback ran the full capture teardown and unregistered the
		// hotkey.
		waitFor(t, time.Second, "teardown", func() bool {
			starts, _, drains, releases := w.runner.counts()
			_, unreg := w.hotkey.counts()
			return starts == 1 && drains == 1 && releases == 1 && unreg == 1
		})
	})

	t.Run("no candidate window reports targetNotFound", func(t *testing.T) {
		w := newMacWorld(t)
		w.enum.push(enumSnapshot{
			windows:  []macos.NativeWindow{bystanderWindow()},
			displays: testDisplays(),
		})
		_, err := w.adapter.Open(context.Background(), w.config())
		var f *Fault
		if !errors.As(err, &f) || f.Code != CodeTargetNotFound || f.Class != ClassSetup {
			t.Fatalf("want targetNotFound, got %v", err)
		}
		if _, unreg := w.hotkey.counts(); unreg != 1 {
			t.Fatalf("hotkey rollback: %d", unreg)
		}
	})

	t.Run("ambiguous candidates are refused and logged", func(t *testing.T) {
		w := newMacWorld(t)
		overlay := macos.NativeWindow{
			WindowID: 105, PID: testPID, BundleID: testBundle,
			Frame: macos.PointRect{X: 300, Y: 300, W: 500, H: 500}, OnScreen: true, Layer: 0,
		}
		w.enum.push(enumSnapshot{
			windows:  []macos.NativeWindow{targetWindow(), overlay, bystanderWindow()},
			displays: testDisplays(),
		})
		_, err := w.adapter.Open(context.Background(), w.config())
		var f *Fault
		if !errors.As(err, &f) || f.Code != CodeTargetAmbiguous {
			t.Fatalf("want targetAmbiguous, got %v", err)
		}
		if _, unreg := w.hotkey.counts(); unreg != 1 {
			t.Fatalf("hotkey rollback: %d", unreg)
		}
		if starts, _, _, _ := w.runner.counts(); starts != 0 {
			t.Fatalf("no stream may start on ambiguity, started %d", starts)
		}
	})

	t.Run("invalid config is refused before any native call", func(t *testing.T) {
		w := newMacWorld(t)
		_, err := w.adapter.Open(context.Background(), SessionConfig{Target: TargetSpec{BundleID: testBundle}})
		var f *Fault
		if !errors.As(err, &f) || f.Code != CodeInvalidOperation {
			t.Fatalf("want invalidOperation, got %v", err)
		}
		if reg, _ := w.hotkey.counts(); reg != 0 {
			t.Fatalf("nothing may register on an invalid config, registered %d", reg)
		}
	})
}

// M5: frame conversion, content rect, reuse and waiting.
func TestMacFrame(t *testing.T) {
	t.Run("converts BGRA to a private RGBA image", func(t *testing.T) {
		w := newMacWorld(t)
		sess := w.openReady(t)
		frame, err := sess.Frame(context.Background(), FrameRequest{})
		if err != nil {
			t.Fatalf("frame: %v", err)
		}
		if frame.Image == nil || frame.Image.Bounds().Dx() != 320 || frame.Image.Bounds().Dy() != 240 {
			t.Fatalf("image bounds: %v", frame.Image.Bounds())
		}
		c := frame.Image.RGBAAt(0, 0)
		if c.R != 30 || c.G != 20 || c.B != 10 || c.A != 255 {
			t.Fatalf("BGRA→RGBA at (0,0): got (%d,%d,%d,%d)", c.R, c.G, c.B, c.A)
		}
		if frame.Content != image.Rect(0, 0, 320, 240) {
			t.Fatalf("content: %v", frame.Content)
		}
		// Mutating the returned image must not touch the cache.
		frame.Image.Set(0, 0, image.NewUniform(color.RGBA{255, 0, 0, 255}))
		again, err := sess.Frame(context.Background(), FrameRequest{})
		if err != nil {
			t.Fatal(err)
		}
		if c := again.Image.RGBAAt(0, 0); c.R != 30 {
			t.Fatalf("the returned image must be private: R=%d", c.R)
		}
		sess.Close()
	})

	t.Run("skips geometry-invalid frames", func(t *testing.T) {
		w := newMacWorld(t)
		frame := smallFrame(smallGeo(), 0)
		w.runner.mu.Lock()
		w.runner.autoFrame = &frame
		w.runner.mu.Unlock()
		sess, err := w.adapter.Open(context.Background(), w.config())
		if err != nil {
			t.Fatal(err)
		}
		waitFor(t, time.Second, "first frame", func() bool {
			_, err := sess.Frame(ctxQuick(50*time.Millisecond), FrameRequest{})
			return err == nil
		})
		bad := smallFrame(smallGeo(), 0)
		bad.HasContentScale = false
		w.runner.frame(bad)
		// The invalid frame never satisfies a request; a context bound
		// proves the wait, then a good frame resolves it.
		if _, err := sess.Frame(ctxQuick(30*time.Millisecond), FrameRequest{}); err == nil {
			t.Fatal("an invalid-geometry frame must not be returned")
		}
		w.runner.frame(smallFrame(smallGeo(), 0))
		if _, err := sess.Frame(ctxQuick(time.Second), FrameRequest{}); err != nil {
			t.Fatalf("the next valid frame must satisfy: %v", err)
		}
		sess.Close()
	})

	t.Run("After waits for a fresher frame or an idle observation", func(t *testing.T) {
		w := newMacWorld(t)
		sess := w.openReady(t)
		first, err := sess.Frame(context.Background(), FrameRequest{})
		if err != nil {
			t.Fatal(err)
		}
		after := first.ArrivedAt.Add(50 * time.Millisecond)
		// Nothing new yet: a bounded context expires.
		if _, err := sess.Frame(ctxQuick(20*time.Millisecond), FrameRequest{After: after}); err == nil {
			t.Fatal("a request after the future must wait")
		}
		// An idle observation after the deadline satisfies it with the
		// latest complete frame, marked reused. The deadline has to pass
		// first — the idle status must be observed after it.
		if d := time.Until(after); d > 0 {
			time.Sleep(d)
		}
		w.runner.frame(macos.NativeFrame{Status: macos.NativeFrameIdle})
		frame, err := sess.Frame(ctxQuick(time.Second), FrameRequest{After: after})
		if err != nil {
			t.Fatalf("idle reuse: %v", err)
		}
		if !frame.Reused || frame.ID != first.ID {
			t.Fatalf("idle reuse must return the latest complete frame reused: %+v", frame)
		}
		// A fresh complete frame satisfies it without reuse.
		freshAfter := time.Now()
		time.Sleep(2 * time.Millisecond)
		w.runner.frame(smallFrame(smallGeo(), 0))
		frame, err = sess.Frame(ctxQuick(time.Second), FrameRequest{After: freshAfter})
		if err != nil {
			t.Fatal(err)
		}
		if frame.Reused {
			t.Fatal("a fresh complete frame must not be marked reused")
		}
		sess.Close()
	})

	t.Run("a stopped stream fails the wait honestly", func(t *testing.T) {
		w := newMacWorld(t)
		sess := w.openReady(t)
		w.runner.event(macos.NativeStreamEvent{Kind: macos.NativeEventStopped, Err: errors.New("stream died")})
		_, err := sess.Frame(ctxQuick(500*time.Millisecond), FrameRequest{After: time.Now().Add(time.Hour)})
		var f *Fault
		if !errors.As(err, &f) || f.Code != CodeCaptureStopped {
			t.Fatalf("want captureStopped, got %v", err)
		}
		sess.Close()
	})

	t.Run("a closed session refuses frames", func(t *testing.T) {
		w := newMacWorld(t)
		sess := w.openReady(t)
		sess.Close()
		_, err := sess.Frame(context.Background(), FrameRequest{})
		var f *Fault
		if !errors.As(err, &f) || f.Code != CodeCaptureStopped {
			t.Fatalf("want captureStopped after close, got %v", err)
		}
	})
}

func ctxQuick(d time.Duration) (ctx context.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), d)
	// The tests are short; leaking the cancel for their lifetime is the
	// simpler and honest option here.
	_ = cancel
	return ctx
}

// M6: dispatch denial paths; the poster is never called on a denial.
func TestMacDispatchDenials(t *testing.T) {
	denied := func(t *testing.T, res InputResult, code string) {
		t.Helper()
		if res.Status != InputNotDispatched || res.Denial == nil || res.Denial.Code != code {
			t.Fatalf("want notDispatched %s, got %+v", code, res)
		}
	}
	clickOp := func() InputOp {
		return InputOp{Kind: InputClick, Geometry: 1, At: image.Point{10, 10}}
	}

	t.Run("closed gate", func(t *testing.T) {
		w := newMacWorld(t)
		sess := w.openReady(t)
		w.gate.set(false)
		denied(t, sess.Dispatch(clickOp()), CodePermitClosed)
		if len(w.poster.posted()) != 0 {
			t.Fatal("a denied dispatch must not post")
		}
		sess.Close()
	})
	t.Run("stale geometry, including zero", func(t *testing.T) {
		w := newMacWorld(t)
		sess := w.openReady(t)
		op := clickOp()
		op.Geometry = 0
		denied(t, sess.Dispatch(op), CodeStaleGeometry)
		op.Geometry = 99
		denied(t, sess.Dispatch(op), CodeStaleGeometry)
		if len(w.poster.posted()) != 0 {
			t.Fatal("a denied dispatch must not post")
		}
		sess.Close()
	})
	t.Run("point outside the content region", func(t *testing.T) {
		w := newMacWorld(t)
		sess := w.openReady(t)
		op := clickOp()
		op.At = image.Point{1995, 10}
		denied(t, sess.Dispatch(op), CodePositionOutside)
		op.At = image.Point{10, 10}
		op.Kind = InputDrag
		op.To = image.Point{5000, 10}
		denied(t, sess.Dispatch(op), CodePositionOutside)
		sess.Close()
	})
	t.Run("focus lost", func(t *testing.T) {
		w := newMacWorld(t)
		sess := w.openReady(t)
		w.fg.set(otherPID, nil)
		denied(t, sess.Dispatch(clickOp()), CodeFocusLost)
		if len(w.poster.posted()) != 0 {
			t.Fatal("a denied dispatch must not post")
		}
		sess.Close()
	})
	t.Run("target hidden", func(t *testing.T) {
		w := newMacWorld(t)
		sess := w.openReady(t)
		hidden := targetWindow()
		hidden.OnScreen = false
		w.enum.push(enumSnapshot{windows: []macos.NativeWindow{hidden, bystanderWindow()}, displays: testDisplays()})
		denied(t, sess.Dispatch(clickOp()), CodeTargetHidden)
		sess.Close()
	})
	t.Run("target ambiguous through the sibling filter", func(t *testing.T) {
		w := newMacWorld(t)
		sess := w.openReady(t)
		overlay := macos.NativeWindow{
			WindowID: 105, PID: testPID, BundleID: testBundle,
			Frame: macos.PointRect{X: 300, Y: 300, W: 500, H: 500}, OnScreen: true, Layer: 0,
		}
		w.enum.push(enumSnapshot{
			windows:  []macos.NativeWindow{targetWindow(), overlay, bystanderWindow()},
			displays: testDisplays(),
		})
		denied(t, sess.Dispatch(clickOp()), CodeTargetAmbiguous)
		sess.Close()
	})
	t.Run("target lost", func(t *testing.T) {
		w := newMacWorld(t)
		sess := w.openReady(t)
		w.enum.push(enumSnapshot{windows: []macos.NativeWindow{bystanderWindow()}, displays: testDisplays()})
		denied(t, sess.Dispatch(clickOp()), CodeTargetLost)
		sess.Close()
	})
	t.Run("enumeration failure during readiness", func(t *testing.T) {
		w := newMacWorld(t)
		sess := w.openReady(t)
		w.enum.push(enumSnapshot{err: errors.New("enumeration timed out")})
		denied(t, sess.Dispatch(clickOp()), CodeObservationFailed)
		sess.Close()
	})
	t.Run("frontmost query failure during readiness", func(t *testing.T) {
		w := newMacWorld(t)
		sess := w.openReady(t)
		w.fg.set(0, errors.New("main run loop unavailable"))
		denied(t, sess.Dispatch(clickOp()), CodeObservationFailed)
		sess.Close()
	})
	t.Run("unknown key and kind", func(t *testing.T) {
		w := newMacWorld(t)
		sess := w.openReady(t)
		op := InputOp{Kind: InputKeyPress, Geometry: 1, Key: "nonsense"}
		denied(t, sess.Dispatch(op), CodeInvalidOperation)
		op = InputOp{Kind: InputKind("wiggle"), Geometry: 1}
		denied(t, sess.Dispatch(op), CodeInvalidOperation)
		sess.Close()
	})
	t.Run("event synthesis revoked denies before posting", func(t *testing.T) {
		w := newMacWorld(t)
		sess := w.openReady(t)
		w.perms.set(true, false)
		denied(t, sess.Dispatch(clickOp()), CodePermissionRevoked)
		if len(w.poster.posted()) != 0 {
			t.Fatal("a denied dispatch must not post")
		}
		sess.Close()
	})
}

// M7 + P2: click hold and drag behavior through the port.
func TestMacDispatchDelivery(t *testing.T) {
	t.Run("click holds the press for the default", func(t *testing.T) {
		w := newMacWorld(t)
		sess := w.openReady(t)
		res := sess.Dispatch(InputOp{Kind: InputClick, Geometry: 1, At: image.Point{10, 10}})
		if res.Status != InputPosted || res.EventsPlanned != 2 || res.EventsPosted != 2 {
			t.Fatalf("click result: %+v", res)
		}
		got := w.poster.posted()
		if len(got) != 2 {
			t.Fatalf("posted %v", got)
		}
		if got[0] != "button-down(110.00,60.00)" || got[1] != "button-up(110.00,60.00)" {
			t.Fatalf("the image point (10,10) maps to the screen (110,60): %v", got)
		}
		if !hasSpan(w.sleeps, macos.DefaultHold) {
			t.Fatalf("default hold not slept: %v", w.sleeps.spans)
		}
		sess.Close()
	})
	t.Run("an over-bound hold is refused", func(t *testing.T) {
		w := newMacWorld(t)
		sess := w.openReady(t)
		res := sess.Dispatch(InputOp{Kind: InputClick, Geometry: 1, At: image.Point{10, 10}, Hold: macos.MaxHold + time.Millisecond})
		if res.Status != InputNotDispatched || res.Denial == nil || res.Denial.Code != CodeInvalidOperation {
			t.Fatalf("over-bound hold: %+v", res)
		}
		if len(w.poster.posted()) != 0 {
			t.Fatal("nothing may post")
		}
		sess.Close()
	})
	t.Run("drag posts the interpolation and aborts at a closed gate", func(t *testing.T) {
		w := newMacWorld(t)
		sess := w.openReady(t)
		// The gate stays open for the adapter's pre-check and the first
		// step-gate check, then closes: one move posts, the abort
		// releases.
		w.gate.mu.Lock()
		w.gate.failAfter = 3
		w.gate.mu.Unlock()
		res := sess.Dispatch(InputOp{Kind: InputDrag, Geometry: 1, At: image.Point{10, 10}, To: image.Point{10, 90}, Steps: 4, Duration: 40 * time.Millisecond})
		if !res.Aborted || res.Status != InputPartial {
			t.Fatalf("aborted drag: %+v", res)
		}
		got := w.poster.posted()
		if len(got) != 3 {
			t.Fatalf("abort sequence %v", got)
		}
		if got[0] != "button-down(110.00,60.00)" || got[1] != "move(110.00,80.00)" || got[2] != "button-up(110.00,80.00)" {
			t.Fatalf("the drag aborts after the first interpolated move and releases there: %v", got)
		}
		sess.Close()
	})

	t.Run("a delivered drag reaches the destination", func(t *testing.T) {
		w := newMacWorld(t)
		sess := w.openReady(t)
		res := sess.Dispatch(InputOp{Kind: InputDrag, Geometry: 1, At: image.Point{10, 10}, To: image.Point{10, 90}, Steps: 2, Duration: 20 * time.Millisecond})
		if res.Status != InputPosted || res.Aborted {
			t.Fatalf("drag: %+v", res)
		}
		got := w.poster.posted()
		if len(got) != 4 {
			t.Fatalf("drag sequence %v", got)
		}
		if got[0] != "button-down(110.00,60.00)" || got[3] != "button-up(110.00,140.00)" {
			t.Fatalf("the drag presses at the image point and releases at the destination: %v", got)
		}
		sess.Close()
	})
}

func hasSpan(r *sleepRecorder, want time.Duration) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.spans {
		if s == want {
			return true
		}
	}
	return false
}

// M8: image→screen mapping through the field-round geometry.
func TestMacDispatchMapsFieldGeometry(t *testing.T) {
	w := newMacWorld(t)
	// The exact numbers 实测轮 ① recorded: 1164×762 pt window at
	// (174,110) on a 1512×982 display, surface 1920×1080 px, scaleFactor
	// 2, contentScale 0.7086614, contentRect (0,0,824.88,540) pt.
	w.enum.push(enumSnapshot{
		windows: []macos.NativeWindow{{
			WindowID: testWinID, PID: testPID, BundleID: testBundle,
			Frame: macos.PointRect{X: 174, Y: 110, W: 1164, H: 762}, OnScreen: true, Layer: 0,
		}, bystanderWindow()},
		displays: []macos.NativeDisplay{{DisplayID: 1, Width: 1512, Height: 982, Frame: macos.PointRect{X: 0, Y: 0, W: 1512, H: 982}}},
	})
	geo := macos.FrameGeometry{
		SurfaceWidth: 1920, SurfaceHeight: 1080, BytesPerRow: 7680,
		ContentRect:  macos.PointRect{X: 0, Y: 0, W: 824.88, H: 540},
		ContentScale: 0.7086614, ScaleFactor: 2,
		WindowFrame: macos.PointRect{X: 174, Y: 110, W: 1164, H: 762},
	}
	fieldFrame := smallFrame(geo, 0)
	w.runner.mu.Lock()
	w.runner.autoFrame = &fieldFrame
	w.runner.mu.Unlock()
	sess, err := w.adapter.Open(context.Background(), w.config())
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, 2*time.Second, "ready", func() bool {
		obs, err := sess.Observe()
		return err == nil && obs.Ready
	})
	baseline, err := sess.LockBaseline()
	if err != nil {
		t.Fatal(err)
	}
	frame, err := sess.Frame(context.Background(), FrameRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if frame.Content != image.Rect(0, 0, 1650, 1080) {
		t.Fatalf("content %v, want (0,0)-(1650,1080)", frame.Content)
	}
	// A click at the image pixel (824,539) maps to the screen point
	// computed by the documented chain: /scaleFactor, back through
	// contentScale, plus the window origin.
	res := sess.Dispatch(InputOp{Kind: InputClick, Geometry: baseline, At: image.Point{824, 539}})
	if res.Status != InputPosted {
		t.Fatalf("click: %+v", res)
	}
	want := fmt.Sprintf("button-down(%.2f,%.2f)", 174+824.0/2/0.7086614, 110+539.0/2/0.7086614)
	if got := w.poster.posted()[0]; got != want {
		t.Fatalf("field-geometry click position %q, want %q", got, want)
	}
	// A point inside the surface but outside the content region is
	// refused: the right ~270 px are stream padding, not window content.
	if res := sess.Dispatch(InputOp{Kind: InputClick, Geometry: baseline, At: image.Point{1700, 500}}); res.Status != InputNotDispatched || res.Denial.Code != CodePositionOutside {
		t.Fatalf("padding click: %+v", res)
	}
	sess.Close()
}
