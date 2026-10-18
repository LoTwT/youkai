package platform

import (
	"context"
	"errors"
	"fmt"
	"image"
	"sync"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/platform/windows"
)

// Windows adapter tests (W-series), all fake-driven — the Win32
// implementations live in the windows base behind interfaces, and this
// file re-creates the fake shapes here because the base's own fakes are
// test-file-private.

// ---- fakes ----

type fakeWnd struct {
	hwnd      windows.HWND
	pid       windows.ProcessID
	gone      bool
	visible   bool
	iconic    bool
	className string
	title     string
}

type fakeWinWindow struct {
	mu    sync.Mutex
	wnds  []*fakeWnd
	query int
}

func (f *fakeWinWindow) EnumTopLevel(visit func(windows.HWND)) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, w := range f.wnds {
		visit(w.hwnd)
	}
	return nil
}

func (f *fakeWinWindow) lookup(hwnd windows.HWND) *fakeWnd {
	for _, w := range f.wnds {
		if w.hwnd == hwnd {
			return w
		}
	}
	return nil
}

func (f *fakeWinWindow) ThreadProcessID(hwnd windows.HWND) (windows.ProcessID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.query++
	if w := f.lookup(hwnd); w != nil && !w.gone {
		return w.pid, nil
	}
	return 0, nil
}

func (f *fakeWinWindow) IsWindow(hwnd windows.HWND) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	w := f.lookup(hwnd)
	return w != nil && !w.gone
}

func (f *fakeWinWindow) IsWindowVisible(hwnd windows.HWND) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if w := f.lookup(hwnd); w != nil {
		return w.visible
	}
	return false
}

func (f *fakeWinWindow) IsIconic(hwnd windows.HWND) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if w := f.lookup(hwnd); w != nil {
		return w.iconic
	}
	return false
}

func (f *fakeWinWindow) Cloaked(hwnd windows.HWND) (bool, error) { return false, nil }

func (f *fakeWinWindow) ClassName(hwnd windows.HWND) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if w := f.lookup(hwnd); w != nil {
		return w.className, nil
	}
	return "", nil
}

func (f *fakeWinWindow) Title(hwnd windows.HWND) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if w := f.lookup(hwnd); w != nil {
		return w.title, nil
	}
	return "", nil
}

type fakeProcInfo struct {
	path    string
	started time.Time
}

type fakeWinProcs struct {
	mu    sync.Mutex
	byPID map[windows.ProcessID]fakeProcInfo
}

func (f *fakeWinProcs) Query(pid windows.ProcessID) (string, time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if info, ok := f.byPID[pid]; ok {
		return info.path, info.started, nil
	}
	return "", time.Time{}, fmt.Errorf("no such process %d", pid)
}

type fakeWinGeo struct {
	mu         sync.Mutex
	clientSize windows.PixelPoint
	origin     windows.PixelPoint
	monitor    windows.PixelRect
	windowDPI  uint32
	monitorDPI uint32
	self       windows.DpiAwareness
	selfErr    error
}

func (f *fakeWinGeo) ClientSize(hwnd windows.HWND) (int, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.clientSize.X, f.clientSize.Y, nil
}

func (f *fakeWinGeo) ClientPointToScreen(hwnd windows.HWND, x, y int) (windows.PixelPoint, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return windows.PixelPoint{X: f.origin.X + x, Y: f.origin.Y + y}, nil
}

func (f *fakeWinGeo) WindowDPI(hwnd windows.HWND) (uint32, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.windowDPI, nil
}

func (f *fakeWinGeo) MonitorDPI(hwnd windows.HWND) (uint32, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.monitorDPI, nil
}

func (f *fakeWinGeo) MonitorRect(hwnd windows.HWND) (windows.PixelRect, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.monitor, nil
}

func (f *fakeWinGeo) SelfDpiAwareness() (windows.DpiAwareness, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.self, f.selfErr
}

func (f *fakeWinGeo) setOrigin(x, y int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.origin = windows.PixelPoint{X: x, Y: y}
}

type fakeWinFG struct {
	mu         sync.Mutex
	foreground windows.HWND
	hitDefault windows.HWND
}

func (f *fakeWinFG) ForegroundWindow() windows.HWND {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.foreground
}

func (f *fakeWinFG) RequestForeground(hwnd windows.HWND) bool { return false }

func (f *fakeWinFG) HitWindow(p windows.PixelPoint) windows.HWND {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hitDefault
}

func (f *fakeWinFG) RootOf(hwnd windows.HWND) windows.HWND { return hwnd }

func (f *fakeWinFG) set(foreground windows.HWND) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.foreground = foreground
}

type fakeWinGDI struct {
	mu       sync.Mutex
	captures int
}

func (f *fakeWinGDI) GetScreenDC() (windows.DC, error) { return windows.DC(1), nil }
func (f *fakeWinGDI) ReleaseDC(dc windows.DC) error    { return nil }
func (f *fakeWinGDI) CreateCompatibleDC(dc windows.DC) (windows.DC, error) {
	return windows.DC(2), nil
}
func (f *fakeWinGDI) DeleteDC(dc windows.DC) error { return nil }
func (f *fakeWinGDI) CreateCompatibleBitmap(dc windows.DC, w, h int) (windows.GDIObject, error) {
	return windows.GDIObject(3), nil
}
func (f *fakeWinGDI) SelectObject(dc windows.DC, obj windows.GDIObject) (windows.GDIObject, error) {
	return windows.GDIObject(4), nil
}
func (f *fakeWinGDI) DeleteObject(obj windows.GDIObject) error { return nil }
func (f *fakeWinGDI) BitBlt(dst windows.DC, dstX, dstY, w, h int, src windows.DC, srcX, srcY int) error {
	return nil
}

// GetDIBits fills the buffer with a position-derived BGRA pattern:
// pixel (x,y) is B=x, G=y, R=(x+y)%200 — the RGBA conversion and the
// coordinate mapping are both observable through it.
func (f *fakeWinGDI) GetDIBits(dc windows.DC, bmp windows.GDIObject, buf []byte, w, h int) error {
	f.mu.Lock()
	f.captures++
	f.mu.Unlock()
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := (y*w + x) * 4
			buf[i], buf[i+1], buf[i+2], buf[i+3] = byte(x), byte(y), byte((x+y)%200), 0
		}
	}
	return nil
}

type fakeWinSender struct {
	mu      sync.Mutex
	calls   [][]windows.InputEvent
	respond func(call int, events []windows.InputEvent) (int, bool, error)
}

func (f *fakeWinSender) SendInput(events []windows.InputEvent) (int, bool, error) {
	f.mu.Lock()
	f.calls = append(f.calls, append([]windows.InputEvent(nil), events...))
	call := len(f.calls) - 1
	f.mu.Unlock()
	if f.respond != nil {
		return f.respond(call, events)
	}
	return len(events), false, nil
}

func (f *fakeWinSender) batches() [][]windows.InputEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][]windows.InputEvent(nil), f.calls...)
}

type fakeWinHotkeyAPI struct {
	mu  sync.Mutex
	reg map[int]bool
	err error
}

func (f *fakeWinHotkeyAPI) RegisterHotKey(id int, mods windows.HotkeyModifiers, vk windows.VirtualKey) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	if f.reg == nil {
		f.reg = make(map[int]bool)
	}
	f.reg[id] = true
	return nil
}

func (f *fakeWinHotkeyAPI) UnregisterHotKey(id int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.reg, id)
	return nil
}

func (f *fakeWinHotkeyAPI) registered() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.reg) > 0
}

// fakeWinLoop is the fake hotkey loop: registration happens on it, the
// trigger is delivered by the test, Stop unregisters.
type fakeWinLoop struct {
	mgr *windows.HotkeyManager
}

func (l fakeWinLoop) Stop() error { return l.mgr.Unregister() }

// ---- the test world ----

const (
	gameExe  = `C:\game\ZenlessZoneZero.exe`
	gamePID  = windows.ProcessID(42)
	gameHwnd = windows.HWND(7)
)

type winWorld struct {
	adapter *WinAdapter
	win     *fakeWinWindow
	procs   *fakeWinProcs
	geo     *fakeWinGeo
	fg      *fakeWinFG
	gdi     *fakeWinGDI
	send    *fakeWinSender
	hotkey  *fakeWinHotkeyAPI
	gate    *fakeGate
	mgr     *windows.HotkeyManager

	faultsMu sync.Mutex
	faults   []*Fault
	stops    int
}

func newWinWorld(t *testing.T) *winWorld {
	t.Helper()
	started := time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC)
	w := &winWorld{
		win: &fakeWinWindow{wnds: []*fakeWnd{{
			hwnd: gameHwnd, pid: gamePID, visible: true,
			className: "UnityWndClass", title: "绝区零",
		}}},
		procs: &fakeWinProcs{byPID: map[windows.ProcessID]fakeProcInfo{
			gamePID: {path: gameExe, started: started},
		}},
		geo: &fakeWinGeo{
			clientSize: windows.PixelPoint{X: 800, Y: 600},
			origin:     windows.PixelPoint{X: 10, Y: 20},
			monitor:    windows.PixelRect{Min: windows.PixelPoint{X: 0, Y: 0}, Max: windows.PixelPoint{X: 1920, Y: 1080}},
			windowDPI:  96, monitorDPI: 96,
			self: windows.DpiAwarenessPerMonitorV2,
		},
		fg:     &fakeWinFG{foreground: gameHwnd, hitDefault: gameHwnd},
		gdi:    &fakeWinGDI{},
		send:   &fakeWinSender{},
		hotkey: &fakeWinHotkeyAPI{},
		gate:   &fakeGate{open: true},
	}
	adapter, err := NewWinAdapter(WinNatives{
		Win: w.win, Procs: w.procs, Geo: w.geo, FG: w.fg, GDI: w.gdi,
		Send: w.send, HotkeyAPI: w.hotkey,
		StartLoop: func(mgr *windows.HotkeyManager) (WinHotkeyLoop, error) {
			if err := mgr.Register(); err != nil {
				return nil, err
			}
			return fakeWinLoop{mgr: mgr}, nil
		},
	})
	if err != nil {
		t.Fatalf("adapter: %v", err)
	}
	adapter.WatchEvery = 2 * time.Millisecond
	w.adapter = adapter
	return w
}

func (w *winWorld) config() SessionConfig {
	return SessionConfig{
		Target: TargetSpec{ExecutablePath: gameExe, ClassNameHint: "UnityWndClass"},
		Hotkey: HotkeySpec{Key: KeyS, Ctrl: true, Alt: true, Shift: true},
		OnStop: func() { w.stops++ },
		Gate:   w.gate,
		OnFault: func(f *Fault) {
			w.faultsMu.Lock()
			w.faults = append(w.faults, f)
			w.faultsMu.Unlock()
		},
	}
}

func (w *winWorld) faultCount(code string) int {
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

// openReady opens a session and drives it to a locked baseline.
func (w *winWorld) openReady(t *testing.T) Session {
	t.Helper()
	sess, err := w.adapter.Open(context.Background(), w.config())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	obs, err := sess.Observe()
	if err != nil || !obs.Ready {
		t.Fatalf("observe after open: err=%v obs=%+v", err, obs)
	}
	if _, err := sess.LockBaseline(); err != nil {
		t.Fatalf("lock baseline: %v", err)
	}
	return sess
}

// ---- tests ----

// W1: target resolution and geometry/DPI verification at open.
func TestWinOpenResolution(t *testing.T) {
	t.Run("healthy open locks the target", func(t *testing.T) {
		w := newWinWorld(t)
		sess := w.openReady(t)
		info := sess.Target()
		if info.HWND != uintptr(gameHwnd) || info.ExecutablePath != gameExe {
			t.Fatalf("target info: %+v", info)
		}
		sess.Close()
	})
	t.Run("no window reports targetNotFound", func(t *testing.T) {
		w := newWinWorld(t)
		w.win.mu.Lock()
		w.win.wnds = nil
		w.win.mu.Unlock()
		_, err := w.adapter.Open(context.Background(), w.config())
		var f *Fault
		if !errors.As(err, &f) || f.Code != CodeTargetNotFound || f.Class != ClassSetup {
			t.Fatalf("want targetNotFound, got %v", err)
		}
		if w.hotkey.registered() {
			t.Fatal("the hotkey must be unregistered on rollback")
		}
	})
	t.Run("ambiguous candidates are refused and logged", func(t *testing.T) {
		w := newWinWorld(t)
		w.win.mu.Lock()
		w.win.wnds = append(w.win.wnds, &fakeWnd{
			hwnd: 8, pid: gamePID, visible: true, className: "UnityWndClass", title: "登录",
		})
		w.win.mu.Unlock()
		_, err := w.adapter.Open(context.Background(), w.config())
		var f *Fault
		if !errors.As(err, &f) || f.Code != CodeTargetAmbiguous {
			t.Fatalf("want targetAmbiguous, got %v", err)
		}
	})
	t.Run("hidden single window reports targetHidden", func(t *testing.T) {
		w := newWinWorld(t)
		w.win.mu.Lock()
		w.win.wnds[0].visible = false
		w.win.mu.Unlock()
		_, err := w.adapter.Open(context.Background(), w.config())
		var f *Fault
		if !errors.As(err, &f) || f.Code != CodeTargetHidden {
			t.Fatalf("want targetHidden, got %v", err)
		}
	})
	t.Run("client not inside the monitor reports targetHidden", func(t *testing.T) {
		w := newWinWorld(t)
		w.geo.mu.Lock()
		w.geo.monitor = windows.PixelRect{Min: windows.PixelPoint{X: 0, Y: 0}, Max: windows.PixelPoint{X: 100, Y: 100}}
		w.geo.mu.Unlock()
		_, err := w.adapter.Open(context.Background(), w.config())
		var f *Fault
		if !errors.As(err, &f) || f.Code != CodeTargetHidden {
			t.Fatalf("want targetHidden, got %v", err)
		}
	})
	t.Run("DPI-virtualized target reports geometryInvalid", func(t *testing.T) {
		w := newWinWorld(t)
		w.geo.mu.Lock()
		w.geo.windowDPI = 96
		w.geo.monitorDPI = 144
		w.geo.mu.Unlock()
		_, err := w.adapter.Open(context.Background(), w.config())
		var f *Fault
		if !errors.As(err, &f) || f.Code != CodeGeometryInvalid || f.Class != ClassBroken {
			t.Fatalf("want geometryInvalid, got %v", err)
		}
	})
	t.Run("preflight demands per-monitor-v2", func(t *testing.T) {
		w := newWinWorld(t)
		if f := w.adapter.Preflight().Fault(); f != nil {
			t.Fatalf("healthy preflight: %+v", f)
		}
		w.geo.mu.Lock()
		w.geo.self = windows.DpiAwarenessSystem
		w.geo.mu.Unlock()
		f := w.adapter.Preflight().Fault()
		if f == nil || f.Code != CodePermissionMissing || !contains(f.Message, "perMonitorV2DpiAwareness") {
			t.Fatalf("pm2 missing: %+v", f)
		}
	})
	t.Run("hotkey registration failure reports hotkeyUnavailable", func(t *testing.T) {
		w := newWinWorld(t)
		w.hotkey.mu.Lock()
		w.hotkey.err = errors.New("RegisterHotKey: conflict")
		w.hotkey.mu.Unlock()
		_, err := w.adapter.Open(context.Background(), w.config())
		var f *Fault
		if !errors.As(err, &f) || f.Code != CodeHotkeyUnavailable {
			t.Fatalf("want hotkeyUnavailable, got %v", err)
		}
		if w.send.batches() != nil {
			t.Fatal("nothing may be sent on a failed open")
		}
	})
}

// W2: frames and dispatch refuse after a geometry change.
func TestWinGeometryChange(t *testing.T) {
	w := newWinWorld(t)
	sess := w.openReady(t)
	frame, err := sess.Frame(context.Background(), FrameRequest{})
	if err != nil {
		t.Fatalf("frame: %v", err)
	}
	// The fake GDI pattern: pixel (3,5) is BGRA (3,5,8,·) → RGBA (8,5,3).
	if c := frame.Image.RGBAAt(3, 5); c.R != 8 || c.G != 5 || c.B != 3 || c.A != 255 {
		t.Fatalf("RGBA at (3,5): %+v", c)
	}
	if frame.Content != image.Rect(0, 0, 800, 600) || frame.Geometry != 1 {
		t.Fatalf("frame: content %v geometry %d", frame.Content, frame.Geometry)
	}
	if frame.Reused {
		t.Fatal("a GDI frame is always fresh")
	}
	// A fresh capture arrives under the same geometry.
	second, err := sess.Frame(context.Background(), FrameRequest{After: frame.ArrivedAt})
	if err != nil {
		t.Fatalf("second frame: %v", err)
	}
	if second.ID <= frame.ID {
		t.Fatalf("frame ids must advance: %d then %d", frame.ID, second.ID)
	}

	// The window moves: the tracker bumps the version, and both Frame
	// and Dispatch refuse the stale baseline.
	w.geo.setOrigin(500, 20)
	_, err = sess.Frame(context.Background(), FrameRequest{})
	var f *Fault
	if !errors.As(err, &f) || f.Code != CodeGeometryChanged {
		t.Fatalf("frame after the move: %v", err)
	}
	res := sess.Dispatch(InputOp{Kind: InputClick, Geometry: 1, At: image.Point{3, 5}})
	if res.Status != InputNotDispatched || res.Denial == nil || res.Denial.Code != CodeStaleGeometry {
		t.Fatalf("dispatch after the move: %+v", res)
	}
	sess.Close()
}

// W3: dispatch re-observes, prechecks and honors the gate before any
// event is inserted.
func TestWinDispatchDenials(t *testing.T) {
	t.Run("focus lost refuses without sending", func(t *testing.T) {
		w := newWinWorld(t)
		sess := w.openReady(t)
		w.fg.set(999)
		res := sess.Dispatch(InputOp{Kind: InputClick, Geometry: 1, At: image.Point{3, 5}})
		if res.Status != InputNotDispatched || res.Denial == nil || res.Denial.Code != CodeFocusLost {
			t.Fatalf("want focusLost, got %+v", res)
		}
		if batches := w.send.batches(); len(batches) != 0 {
			t.Fatalf("a denied dispatch must not send: %+v", batches)
		}
		sess.Close()
	})
	t.Run("closed gate refuses without sending", func(t *testing.T) {
		w := newWinWorld(t)
		sess := w.openReady(t)
		w.gate.set(false)
		res := sess.Dispatch(InputOp{Kind: InputClick, Geometry: 1, At: image.Point{3, 5}})
		if res.Status != InputNotDispatched || res.Denial == nil || res.Denial.Code != CodePermitClosed {
			t.Fatalf("want permitClosed, got %+v", res)
		}
		if batches := w.send.batches(); len(batches) != 0 {
			t.Fatalf("a denied dispatch must not send: %+v", batches)
		}
		sess.Close()
	})
	t.Run("point outside the client refuses", func(t *testing.T) {
		w := newWinWorld(t)
		sess := w.openReady(t)
		res := sess.Dispatch(InputOp{Kind: InputClick, Geometry: 1, At: image.Point{7000, 5}})
		if res.Status != InputNotDispatched || res.Denial.Code != CodePositionOutside {
			t.Fatalf("want positionOutside, got %+v", res)
		}
		sess.Close()
	})
	t.Run("occluded point refuses", func(t *testing.T) {
		w := newWinWorld(t)
		sess := w.openReady(t)
		w.fg.mu.Lock()
		w.fg.hitDefault = 55
		w.fg.mu.Unlock()
		res := sess.Dispatch(InputOp{Kind: InputClick, Geometry: 1, At: image.Point{3, 5}})
		if res.Status != InputNotDispatched || res.Denial.Code != CodePositionOutside {
			t.Fatalf("want positionOutside for the occluded point, got %+v", res)
		}
		sess.Close()
	})
	t.Run("unknown key refuses", func(t *testing.T) {
		w := newWinWorld(t)
		sess := w.openReady(t)
		res := sess.Dispatch(InputOp{Kind: InputKeyPress, Geometry: 1, Key: "nonsense"})
		if res.Status != InputNotDispatched || res.Denial.Code != CodeInvalidOperation {
			t.Fatalf("want invalidOperation, got %+v", res)
		}
		sess.Close()
	})
}

// W4: the click hold and drag batches, and the UIPI block is sticky.
func TestWinDispatchDelivery(t *testing.T) {
	t.Run("click holds between two batches and maps the image point", func(t *testing.T) {
		w := newWinWorld(t)
		sess := w.openReady(t)
		res := sess.Dispatch(InputOp{Kind: InputClick, Geometry: 1, At: image.Point{3, 5}})
		if res.Status != InputPosted || res.EventsPlanned != 3 || res.EventsPosted != 3 {
			t.Fatalf("click: %+v", res)
		}
		batches := w.send.batches()
		if len(batches) != 2 || len(batches[0]) != 2 || len(batches[1]) != 1 {
			t.Fatalf("click batches: %+v", batches)
		}
		// Image (3,5) → client origin (10,20) → screen (13,25).
		if mv := batches[0][0]; mv.Kind != windows.EventMouseMove || mv.Point != (windows.PixelPoint{X: 13, Y: 25}) {
			t.Fatalf("the move must target the mapped screen point: %+v", mv)
		}
		if res.FinishedAt.Before(res.StartedAt) {
			t.Fatal("the result must bound the operation in time")
		}
		sess.Close()
	})
	t.Run("drag delivers one batch per interpolated move", func(t *testing.T) {
		w := newWinWorld(t)
		sess := w.openReady(t)
		res := sess.Dispatch(InputOp{Kind: InputDrag, Geometry: 1, At: image.Point{3, 5}, To: image.Point{3, 105}, Steps: 4, Duration: 40 * time.Millisecond})
		if res.Status != InputPosted || res.Aborted {
			t.Fatalf("drag: %+v", res)
		}
		batches := w.send.batches()
		if len(batches) != 6 { // press + 4 moves + release
			t.Fatalf("drag batches: %d: %+v", len(batches), batches)
		}
		// Image (3,5)→(13,25); image (3,105)→(13,125).
		last := batches[4][0]
		if last.Kind != windows.EventMouseMove || last.Point != (windows.PixelPoint{X: 13, Y: 125}) {
			t.Fatalf("the last step must land on the destination: %+v", last)
		}
		// The drag destination passed its own client and hit checks.
		sess.Close()
	})
	t.Run("drag with an occluded destination refuses", func(t *testing.T) {
		w := newWinWorld(t)
		sess := w.openReady(t)
		w.fg.mu.Lock()
		w.fg.hitDefault = 55
		w.fg.mu.Unlock()
		res := sess.Dispatch(InputOp{Kind: InputDrag, Geometry: 1, At: image.Point{3, 5}, To: image.Point{3, 105}})
		if res.Status != InputNotDispatched || res.Denial.Code != CodePositionOutside {
			t.Fatalf("the drag destination needs its own hit test: %+v", res)
		}
		if len(w.send.batches()) != 0 {
			t.Fatal("a denied drag must not send")
		}
		sess.Close()
	})
	t.Run("UIPI block is failed and sticky", func(t *testing.T) {
		w := newWinWorld(t)
		sess := w.openReady(t)
		w.send.respond = func(call int, events []windows.InputEvent) (int, bool, error) {
			return 0, true, nil
		}
		res := sess.Dispatch(InputOp{Kind: InputClick, Geometry: 1, At: image.Point{3, 5}})
		if res.Status != InputFailed || res.Denial == nil || res.Denial.Code != CodeInputBlocked {
			t.Fatalf("want failed inputBlocked, got %+v", res)
		}
		w.send.respond = nil
		res = sess.Dispatch(InputOp{Kind: InputClick, Geometry: 1, At: image.Point{3, 5}})
		if res.Status != InputNotDispatched || res.Denial.Code != CodeInputBlocked {
			t.Fatalf("the block must be sticky: %+v", res)
		}
		if len(w.send.batches()) != 1 {
			t.Fatalf("no second send after the block: %d", len(w.send.batches()))
		}
		sess.Close()
	})
}

// W5: the hotkey trigger reaches OnStop and close tears everything down.
func TestWinHotkeyAndClose(t *testing.T) {
	w := newWinWorld(t)
	sess := w.openReady(t)

	// The hotkey fires: the manager delivers the trigger, the stop entry
	// runs (the kernel's stop is the app's idempotent entry, wired by
	// the caller).
	w.fg.set(999) // simulate the user switching away; irrelevant here
	w.fg.set(gameHwnd)
	if mgr := sess.(*winSession); mgr != nil {
		_ = mgr
	}
	// Deliver through the manager the adapter created.
	sessWin, ok := sess.(*winSession)
	if !ok {
		t.Fatalf("session type %T", sess)
	}
	sessWin.hotkey.DeliverTrigger()
	if w.stops != 1 {
		t.Fatalf("the hotkey trigger must reach OnStop, got %d", w.stops)
	}

	rep := sess.Close()
	if !rep.CaptureStopped || !rep.Drained || !rep.Released {
		t.Fatalf("windows close facts: %+v", rep)
	}
	if rep.HotkeyErr != nil || rep.Err != nil {
		t.Fatalf("clean close errors: %+v", rep)
	}
	if w.hotkey.registered() {
		t.Fatal("the hotkey must be unregistered")
	}
	// Dispatch after close is refused and sends nothing.
	res := sess.Dispatch(InputOp{Kind: InputClick, Geometry: 1, At: image.Point{3, 5}})
	if res.Status != InputNotDispatched || res.Denial.Code != CodeCaptureStopped {
		t.Fatalf("post-close dispatch: %+v", res)
	}
	// The watcher stops with the close: later faults are not delivered.
	w.fg.set(999)
	time.Sleep(50 * time.Millisecond)
	if n := w.faultCount(CodeFocusLost); n != 0 {
		t.Fatalf("no watcher faults after close, got %d", n)
	}
	// A second close is a no-op returning the same facts.
	rep2 := sess.Close()
	if rep2.CaptureStopped != rep.CaptureStopped {
		t.Fatalf("close must be idempotent: %+v", rep2)
	}
}

// The watcher reports premise faults once during the session.
func TestWinWatcherReportsFaults(t *testing.T) {
	w := newWinWorld(t)
	sess := w.openReady(t)
	defer sess.Close()
	w.fg.set(999)
	waitFor(t, 2*time.Second, "focusLost", func() bool { return w.faultCount(CodeFocusLost) > 0 })
	time.Sleep(50 * time.Millisecond)
	if n := w.faultCount(CodeFocusLost); n != 1 {
		t.Fatalf("focusLost delivered %d times, want 1", n)
	}
}
