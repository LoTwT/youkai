//go:build darwin

// Command fieldprobe is the development driver for the M2 macOS field
// rounds (实测轮 ①). It drives the real native layer of
// internal/platform/macos — window enumeration, permission preflights,
// foreground and AX probes, the capture session, the raw stream bridge,
// Quartz input posting and the global stop hotkey — against the real
// machine and cloud client. It takes no part in release builds; the
// darwin constraint keeps the Windows build green, and the macOS CI
// builds and vets it. Field results are recorded in
// docs/reports/2026-10-18-m2-macos-field-round1.md.
package main

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/LoTwT/youkai/internal/platform/macos"
)

// The main goroutine must stay on the process's main thread for the
// tool's whole lifetime. Without the lock the goroutine migrates between
// OS threads: the AppKit run loop ends up on a non-main thread (so the
// GCD main queue is never drained and frontmost queries time out
// permanently), and Carbon hotkey events can never be delivered. Both
// failure modes were reproduced without the game during the 2026-10-18
// review round; locking the thread fixed them.
func init() {
	runtime.LockOSThread()
}

const usageText = `fieldprobe — M2 macOS field-test driver (dev tool, not shipped)

modes:
  windows [substr]       enumerate windows + displays as JSON lines; a substring marks matches
  perms                  accessibility trust + event synthesis preflight (JSON)
  frontmost [n]          n samples (1/s) of frontmost PID + AX focused window
  cursorpos              current global cursor position (screen points, top-left origin)
  rawstream <winID> <secs> <outdir>  raw bridge stream: record every frame's attachments,
                                     save a PNG every 500ms, full stop/drain/release lifecycle
  stream <winID> <secs>  capture-session stream: live stats, teardown report
  move <x> <y>           post one mouse move (screen points)
  click <x> <y>          post one left click (down then up) at a screen point
  key <code>             post one key press (Carbon kVK code)
  scroll <dy>            post one scroll (pixel delta; positive scrolls up)
  hotkey <code> <mods> <secs>  register a hotkey and count triggers
                          mods bits: cmd=256 shift=512 opt=2048 ctrl=4096 (combinable by sum)
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usageText)
		os.Exit(2)
	}
	// SCStream creation aborts (CGS_REQUIRE_INIT) in a plain
	// command-line process; initialize AppKit/CGS on the main thread
	// before any native call that needs it.
	initGUI()
	go func() {
		if err := run(os.Args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, "fieldprobe:", err)
			os.Exit(1)
		}
		// CGEventPost delivery to the WindowServer is asynchronous with
		// respect to the caller: a process that exits immediately after
		// posting can cancel the in-flight event (verified on-device:
		// the same NULL-source HID-tap post moves the cursor when the
		// process stays alive and is lost when it exits at once). The
		// real youkai app is long-lived, so this is a driver-side
		// requirement only.
		time.Sleep(300 * time.Millisecond)
		os.Exit(0)
	}()
	// The native layer marshals NSWorkspace reads and Carbon hotkey
	// registration through the main dispatch queue, and hotkey delivery
	// needs the AppKit/Carbon event loop; the main run loop serves both,
	// mirroring the host-app run loop MyGo provides.
	serveMainLoop()
}

func run(args []string) error {
	mode := args[0]
	rest := args[1:]
	switch mode {
	case "windows":
		return runWindows(rest)
	case "perms":
		return runPerms()
	case "frontmost":
		n := 3
		if len(rest) > 0 {
			if v, err := atoi(rest[0]); err == nil {
				n = v
			}
		}
		return runFrontmost(n)
	case "cursorpos":
		return runCursorPos()
	case "rawstream":
		if len(rest) < 3 {
			return fmt.Errorf("rawstream needs <winID> <secs> <outdir>")
		}
		id, err1 := atoi(rest[0])
		secs, err2 := atoi(rest[1])
		if err1 != nil || err2 != nil {
			return fmt.Errorf("bad rawstream args")
		}
		return runRawStream(uint32(id), secs, rest[2])
	case "stream":
		if len(rest) < 2 {
			return fmt.Errorf("stream needs <winID> <secs>")
		}
		id, err1 := atoi(rest[0])
		secs, err2 := atoi(rest[1])
		if err1 != nil || err2 != nil {
			return fmt.Errorf("bad stream args")
		}
		return runSessionStream(uint32(id), secs)
	case "move":
		x, y, err := twoFloats(rest)
		if err != nil {
			return err
		}
		return runMove(x, y)
	case "click":
		x, y, err := twoFloats(rest)
		if err != nil {
			return err
		}
		// Optional ms between press and release: a 0 ms click is two
		// events back to back, which per-frame games (and the iOS
		// compatibility layer translating clicks to taps) may drop;
		// the field test compares both variants.
		ms := 0
		if len(rest) > 2 {
			if v, aerr := atoi(rest[2]); aerr == nil {
				ms = v
			}
		}
		p := macos.NewInputPoster()
		if err := p.PostMouseButton(macos.MouseButtonLeft, true, macos.Point{X: x, Y: y}); err != nil {
			return err
		}
		if ms > 0 {
			time.Sleep(time.Duration(ms) * time.Millisecond)
		}
		return p.PostMouseButton(macos.MouseButtonLeft, false, macos.Point{X: x, Y: y})
	case "key":
		if len(rest) < 1 {
			return fmt.Errorf("key needs <code>")
		}
		code, err := atoi(rest[0])
		if err != nil {
			return err
		}
		p := macos.NewInputPoster()
		if err := p.PostKey(macos.KeyCode(code), true); err != nil {
			return err
		}
		return p.PostKey(macos.KeyCode(code), false)
	case "scroll":
		if len(rest) < 1 {
			return fmt.Errorf("scroll needs <dy>")
		}
		var dy float64
		if _, err := fmt.Sscanf(rest[0], "%g", &dy); err != nil {
			return err
		}
		return macos.NewInputPoster().PostScroll(dy)
	case "drag":
		// drag <x1> <y1> <x2> <y2> [msTotal] — press, interpolated moves,
		// release, all inside one process. Synthesized wheel events are
		// ignored by the iOS compatibility layer (measured 2026-10-18),
		// so list scrolling must go through a touch-style drag, which
		// rides the proven press/move/release path.
		if len(rest) < 4 {
			return fmt.Errorf("drag needs <x1> <y1> <x2> <y2> [msTotal]")
		}
		x1, y1, err1 := twoFloats(rest[0:2])
		x2, y2, err2 := twoFloats(rest[2:4])
		if err1 != nil || err2 != nil {
			return fmt.Errorf("bad drag args")
		}
		msTotal := 400
		if len(rest) > 4 {
			if v, aerr := atoi(rest[4]); aerr == nil {
				msTotal = v
			}
		}
		return runDrag(x1, y1, x2, y2, msTotal)
	case "hotkey":
		if len(rest) < 3 {
			return fmt.Errorf("hotkey needs <code> <mods> <secs>")
		}
		code, err1 := atoi(rest[0])
		mods, err2 := atoi(rest[1])
		secs, err3 := atoi(rest[2])
		if err1 != nil || err2 != nil || err3 != nil {
			return fmt.Errorf("bad hotkey args")
		}
		return runHotkey(uint32(code), uint32(mods), secs)
	default:
		return fmt.Errorf("unknown mode %q\n%s", mode, usageText)
	}
}

func atoi(s string) (int, error) {
	var v int
	if _, err := fmt.Sscanf(s, "%d", &v); err != nil {
		return 0, err
	}
	return v, nil
}

func twoFloats(rest []string) (float64, float64, error) {
	if len(rest) < 2 {
		return 0, 0, fmt.Errorf("need <x> <y>")
	}
	var x, y float64
	if _, err := fmt.Sscanf(rest[0], "%g", &x); err != nil {
		return 0, 0, err
	}
	if _, err := fmt.Sscanf(rest[1], "%g", &y); err != nil {
		return 0, 0, err
	}
	return x, y, nil
}

func printJSON(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		fmt.Println("{\"marshalError\":", err, "}")
		return
	}
	fmt.Println(string(b))
}

// ---- windows ----

type windowOut struct {
	PID      int             `json:"pid"`
	WindowID uint32          `json:"windowID"`
	BundleID string          `json:"bundle"`
	AppName  string          `json:"app"`
	Title    string          `json:"title"`
	Frame    macos.PointRect `json:"frame"`
	OnScreen bool            `json:"onScreen"`
	Active   bool            `json:"active"`
	Layer    int             `json:"layer"`
	Match    bool            `json:"match,omitempty"`
}

type displayOut struct {
	DisplayID uint32          `json:"displayID"`
	Width     int             `json:"width"`
	Height    int             `json:"height"`
	Frame     macos.PointRect `json:"frame"`
}

func runWindows(rest []string) error {
	substr := ""
	if len(rest) > 0 {
		substr = rest[0]
	}
	enum := macos.NewWindowEnumerator()
	wins, displays, err := enum.EnumerateWindows()
	printJSON(map[string]any{
		"at":  time.Now(),
		"pid": os.Getpid(),
		"enumerationErr": func() string {
			if err != nil {
				return err.Error()
			}
			return ""
		}(),
		"displays": func() []displayOut {
			out := make([]displayOut, 0, len(displays))
			for _, d := range displays {
				out = append(out, displayOut{DisplayID: d.DisplayID, Width: d.Width, Height: d.Height, Frame: d.Frame})
			}
			return out
		}(),
		"windowCount": len(wins),
	})
	if err != nil {
		return nil // the error itself is the field observation
	}
	for _, w := range wins {
		o := windowOut{
			PID: int(w.PID), WindowID: w.WindowID, BundleID: w.BundleID, AppName: w.AppName,
			Title: w.Title, Frame: w.Frame, OnScreen: w.OnScreen, Active: w.Active, Layer: w.Layer,
		}
		if substr != "" && (contains(w.BundleID, substr) || contains(w.Title, substr) || contains(w.AppName, substr)) {
			o.Match = true
		}
		printJSON(o)
	}
	return nil
}

func contains(s, sub string) bool {
	if sub == "" {
		return false
	}
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

// ---- perms ----

func runPerms() error {
	p := macos.NewPermissionProbe()
	ax, axErr := p.AccessibilityTrusted()
	ep, epErr := p.EventPostAllowed()
	printJSON(map[string]any{
		"at":               time.Now(),
		"pid":              os.Getpid(),
		"argv0":            os.Args[0],
		"accessibility":    ax,
		"accessibilityErr": errStr(axErr),
		"eventPost":        ep,
		"eventPostErr":     errStr(epErr),
	})
	return nil
}

func errStr(err error) string {
	if err != nil {
		return err.Error()
	}
	return ""
}

// ---- frontmost ----

func runFrontmost(n int) error {
	fg := macos.NewForegroundProbe()
	axp := macos.NewAXProbe()
	for i := 0; i < n; i++ {
		pid, fgErr := fg.FrontmostPID()
		rep, axErr := axp.FocusedWindow(time.Second)
		sample := map[string]any{
			"at":        time.Now(),
			"probe":     i,
			"frontmost": map[string]any{"pid": pid, "err": errStr(fgErr)},
			"axFocused": map[string]any{
				"pid":      rep.PID,
				"role":     rep.Role,
				"frame":    rep.Frame,
				"timedOut": rep.TimedOut,
				"err":      errStr(axErr),
			},
		}
		printJSON(sample)
		if i < n-1 {
			time.Sleep(time.Second)
		}
	}
	return nil
}

// ---- cursor position ----

// runCursorPos reports the global cursor position in the same top-left
// screen-point space as window frames and Quartz mouse events. It is a
// read-only diagnostic used to verify that a posted mouse move actually
// moved the cursor (the game hides the pointer, so visual confirmation
// is unreliable).
func runCursorPos() error {
	x, y, ok := cursorPos(2 * time.Second)
	printJSON(map[string]any{
		"phase": "cursorpos",
		"x":     x,
		"y":     y,
		"ok":    ok,
		"at":    time.Now(),
	})
	return nil
}

// runMove posts one absolute mouse move and then, inside the same
// process, samples the cursor position every 10 ms for a second. This
// yields the landing latency and the post-landing trajectory in one
// invocation — separate cursorpos processes cost ~0.37 s each and miss
// the first 0.4 s, which is exactly where a game repositioning the
// pointer would show.
func runMove(x, y float64) error {
	poster := macos.NewInputPoster()
	postAt := time.Now()
	if err := poster.PostMouseMoved(macos.Point{X: x, Y: y}); err != nil {
		return err
	}
	type sample struct {
		Ms int64   `json:"msAfterPost"`
		X  float64 `json:"x"`
		Y  float64 `json:"y"`
		OK bool    `json:"ok"`
	}
	samples := make([]sample, 0, 101)
	var landedMs *int64
	deadline := postAt.Add(time.Second)
	for time.Now().Before(deadline) {
		px, py, ok := cursorPos(500 * time.Millisecond)
		s := sample{Ms: time.Since(postAt).Milliseconds(), X: px, Y: py, OK: ok}
		samples = append(samples, s)
		if landedMs == nil && ok && math.Abs(px-x) < 0.5 && math.Abs(py-y) < 0.5 {
			ms := s.Ms
			landedMs = &ms
		}
		time.Sleep(10 * time.Millisecond)
	}
	out := map[string]any{
		"phase": "move", "targetX": x, "targetY": y,
		"postAt": postAt, "samples": samples, "at": time.Now(),
	}
	if landedMs != nil {
		out["landedMs"] = *landedMs
	}
	printJSON(out)
	return nil
}

// ---- raw stream ----

type frameMeta struct {
	Seq             int             `json:"seq"`
	At              time.Time       `json:"at"`
	Status          int32           `json:"status"`
	HasDisplayTime  bool            `json:"hasDisplayTime"`
	DisplayTimeMach uint64          `json:"displayTimeMach"`
	SurfaceWidth    int             `json:"w"`
	SurfaceHeight   int             `json:"h"`
	BytesPerRow     int             `json:"bpr"`
	HasContentRect  bool            `json:"hasContentRect"`
	ContentRect     macos.PointRect `json:"contentRect"`
	HasContentScale bool            `json:"hasContentScale"`
	ContentScale    float64         `json:"contentScale"`
	HasScaleFactor  bool            `json:"hasScaleFactor"`
	ScaleFactor     float64         `json:"scaleFactor"`
	HasScreenRect   bool            `json:"hasScreenRect"`
	ScreenRect      macos.PointRect `json:"screenRect"`
	HasPixels       bool            `json:"hasPixels"`
}

type eventRec struct {
	At   time.Time `json:"at"`
	Kind string    `json:"kind"`
	Err  string    `json:"err,omitempty"`
}

type recordingSink struct {
	mu     sync.Mutex
	seq    int
	frames []frameMeta
	events []eventRec
	latest *macos.NativeFrame
	notify chan macos.NativeStreamEventKind
}

func newRecordingSink() *recordingSink {
	return &recordingSink{notify: make(chan macos.NativeStreamEventKind, 32)}
}

func (s *recordingSink) OnNativeFrame(f macos.NativeFrame) {
	s.mu.Lock()
	s.seq++
	m := frameMeta{
		Seq: s.seq, At: time.Now(), Status: int32(f.Status),
		HasDisplayTime: f.HasDisplayTime, DisplayTimeMach: f.DisplayTimeMach,
		SurfaceWidth: f.SurfaceWidth, SurfaceHeight: f.SurfaceHeight, BytesPerRow: f.BytesPerRow,
		HasContentRect: f.HasContentRect, ContentRect: f.ContentRect,
		HasContentScale: f.HasContentScale, ContentScale: f.ContentScale,
		HasScaleFactor: f.HasScaleFactor, ScaleFactor: f.ScaleFactor,
		HasScreenRect: f.HasScreenRect, ScreenRect: f.ScreenRect,
		HasPixels: f.Pixels != nil,
	}
	s.frames = append(s.frames, m)
	if f.Status == macos.NativeFrameComplete && f.Pixels != nil {
		cp := f
		s.latest = &cp
	}
	s.mu.Unlock()
}

func (s *recordingSink) OnNativeStreamEvent(e macos.NativeStreamEvent) {
	rec := eventRec{At: time.Now(), Kind: string(e.Kind), Err: errStr(e.Err)}
	s.mu.Lock()
	s.events = append(s.events, rec)
	s.mu.Unlock()
	select {
	case s.notify <- e.Kind:
	default:
	}
}

func (s *recordingSink) snapshotLatest() (macos.NativeFrame, int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.latest == nil {
		return macos.NativeFrame{}, 0, false
	}
	return *s.latest, s.seq, true
}

func (s *recordingSink) summary() (frames []frameMeta, events []eventRec) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.frames, s.events
}

func waitForEvent(ch chan macos.NativeStreamEventKind, want macos.NativeStreamEventKind, timeout time.Duration) (bool, macos.NativeStreamEvent) {
	deadline := time.After(timeout)
	for {
		select {
		case k := <-ch:
			if k == want {
				return true, macos.NativeStreamEvent{Kind: k}
			}
		case <-deadline:
			return false, macos.NativeStreamEvent{}
		}
	}
}

func runRawStream(windowID uint32, secs int, outdir string) error {
	if err := os.MkdirAll(outdir, 0o755); err != nil {
		return err
	}
	runner := macos.NewStreamRunner()
	sink := newRecordingSink()
	started := time.Now()
	handle, err := runner.StartStream(windowID, sink)
	if err != nil {
		printJSON(map[string]any{"phase": "startStream", "err": err.Error(), "at": time.Now()})
		return nil
	}
	ok, _ := waitForEvent(sink.notify, macos.NativeEventStarted, 5*time.Second)
	printJSON(map[string]any{"phase": "started", "ok": ok, "at": time.Now()})

	saved := 0
	lastSeq := -1
	deadline := time.Now().Add(time.Duration(secs) * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)
		f, seq, has := sink.snapshotLatest()
		if has && seq != lastSeq {
			lastSeq = seq
			path := filepath.Join(outdir, fmt.Sprintf("frame-%04d.png", saved))
			if err := saveFramePNG(path, &f); err != nil {
				printJSON(map[string]any{"phase": "savePNG", "err": err.Error()})
			} else {
				saved++
			}
		}
	}

	printJSON(map[string]any{"phase": "requestStop", "at": time.Now()})
	stopErr := runner.RequestStop(handle)
	stopped, _ := waitForEvent(sink.notify, macos.NativeEventStopped, 5*time.Second)
	printJSON(map[string]any{"phase": "stopped", "requestErr": errStr(stopErr), "got": stopped, "at": time.Now()})
	drainErr := runner.Drain(handle)
	drained, _ := waitForEvent(sink.notify, macos.NativeEventDrained, 3*time.Second)
	printJSON(map[string]any{"phase": "drained", "requestErr": errStr(drainErr), "got": drained, "at": time.Now()})
	if stopped && drained {
		runner.Release(handle)
		printJSON(map[string]any{"phase": "released", "at": time.Now()})
	} else {
		printJSON(map[string]any{"phase": "releaseSkipped", "reason": "stop or drain did not complete; native objects kept alive", "at": time.Now()})
	}

	frames, events := sink.summary()
	byStatus := map[int32]int{}
	for _, f := range frames {
		byStatus[f.Status]++
	}
	metaPath := filepath.Join(outdir, "frames.jsonl")
	if file, err := os.Create(metaPath); err == nil {
		for _, f := range frames {
			b, _ := json.Marshal(f)
			file.Write(append(b, '\n'))
		}
		file.Close()
	}
	printJSON(map[string]any{
		"phase":     "summary",
		"windowID":  windowID,
		"startedAt": started,
		"duration":  time.Since(started).String(),
		"frames":    len(frames),
		"byStatus":  byStatus,
		"savedPNG":  saved,
		"events":    events,
		"outdir":    outdir,
	})
	return nil
}

func saveFramePNG(path string, f *macos.NativeFrame) error {
	if f.Pixels == nil || f.SurfaceWidth <= 0 || f.SurfaceHeight <= 0 {
		return fmt.Errorf("frame has no pixels")
	}
	img := image.NewRGBA(image.Rect(0, 0, f.SurfaceWidth, f.SurfaceHeight))
	for y := 0; y < f.SurfaceHeight; y++ {
		row := f.Pixels[y*f.BytesPerRow:]
		for x := 0; x < f.SurfaceWidth; x++ {
			s := row[x*4 : x*4+4]
			img.SetRGBA(x, y, color.RGBA{R: s[2], G: s[1], B: s[0], A: 255})
		}
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return png.Encode(file, img)
}

// ---- capture session ----

func runSessionStream(windowID uint32, secs int) error {
	sess, err := macos.StartCapture(macos.CaptureConfig{
		Runner:   macos.NewStreamRunner(),
		Selector: macos.WindowSelector{WindowID: windowID},
		Clock:    macos.NewMachClock(macos.NewMachClockSource()),
	})
	if err != nil {
		printJSON(map[string]any{"phase": "sessionStart", "err": err.Error(), "at": time.Now()})
		return nil
	}
	deadline := time.Now().Add(time.Duration(secs) * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(250 * time.Millisecond)
		stats := sess.Stats()
		obs, ok := sess.LatestFrame()
		line := map[string]any{
			"phase": "live", "stats": stats,
		}
		if ok {
			line["obs"] = map[string]any{
				"frameID": obs.FrameID, "status": string(obs.Status), "rawStatus": obs.RawStatus,
				"w": obs.Width, "h": obs.Height, "geometryValid": obs.GeometryValid,
				"geometry": obs.Geometry, "validRect": obs.ValidRect,
				"displayTime": obs.DisplayTime, "arrivedAt": obs.ArrivedAt,
			}
		}
		printJSON(line)
	}
	sess.Stop()
	report := <-sess.Done()
	printJSON(map[string]any{
		"phase": "sessionTeardown", "report": report, "stats": sess.Stats(),
	})
	return nil
}

// ---- hotkey ----

func runHotkey(code, mods uint32, secs int) error {
	reg := macos.NewHotKeyRegistrar()
	var mu sync.Mutex
	var triggers []time.Time
	hk, err := macos.RegisterStopHotKey(reg, macos.HotKeySpec{
		KeyCode:   macos.KeyCode(code),
		Modifiers: macos.HotKeyModifiers(mods),
	}, func() {
		mu.Lock()
		triggers = append(triggers, time.Now())
		mu.Unlock()
	})
	if err != nil {
		printJSON(map[string]any{"phase": "register", "err": err.Error(), "at": time.Now()})
		return nil
	}
	// The user is in the game and cannot see this process's output, so
	// arming and trigger confirmation are announced audibly instead.
	// Glass on arm, Ping on each recorded trigger.
	go playSound("/System/Library/Sounds/Glass.aiff")
	fmt.Fprintf(os.Stderr, "hotkey armed (code=%d mods=%d) — press it within %ds\n", code, mods, secs)
	deadline := time.Now().Add(time.Duration(secs) * time.Second)
	seen := 0
	for time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		mu.Lock()
		n := len(triggers)
		mu.Unlock()
		if n > seen {
			seen = n
			playSound("/System/Library/Sounds/Ping.aiff")
		}
	}
	mu.Lock()
	got := append([]time.Time(nil), triggers...)
	mu.Unlock()
	closeErr := hk.Close()
	printJSON(map[string]any{
		"phase": "hotkeyResult", "code": code, "mods": mods,
		"triggers": got, "unregisterErr": errStr(closeErr), "at": time.Now(),
	})
	return nil
}

// playSound plays a system sound synchronously; best effort only.
func playSound(path string) {
	_ = exec.Command("afplay", path).Run()
}

// runDrag posts a touch-style drag: press at the start, interpolated
// moves, release at the end. If any intermediate move fails to create,
// the button is still released best-effort so no synthetic press is
// left dangling.
func runDrag(x1, y1, x2, y2 float64, msTotal int) error {
	p := macos.NewInputPoster()
	if err := p.PostMouseButton(macos.MouseButtonLeft, true, macos.Point{X: x1, Y: y1}); err != nil {
		return err
	}
	const steps = 10
	for i := 1; i <= steps; i++ {
		f := float64(i) / steps
		x := x1 + (x2-x1)*f
		y := y1 + (y2-y1)*f
		if err := p.PostMouseMoved(macos.Point{X: x, Y: y}); err != nil {
			_ = p.PostMouseButton(macos.MouseButtonLeft, false, macos.Point{X: x, Y: y})
			return err
		}
		if i < steps {
			time.Sleep(time.Duration(msTotal/steps) * time.Millisecond)
		}
	}
	return p.PostMouseButton(macos.MouseButtonLeft, false, macos.Point{X: x2, Y: y2})
}
