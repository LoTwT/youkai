package windows

import (
	"errors"
	"sort"
	"sync"
	"time"
)

// API fakes for the pure-logic tests. They run on any platform; the
// real Win32 implementations live behind //go:build windows files and
// are exercised on real machines in the field test round.

// Sentinel errors the fakes use to model system failures.
var (
	errProcessGone   = errors.New("open process: access denied or gone")
	errDpiApiMissing = errors.New("dpi api missing")
	errMeasureBroken = errors.New("measure broken")
)

// fakeWnd is one fake top-level window.
type fakeWnd struct {
	pid     ProcessID
	visible bool
	iconic  bool
	cloaked bool
	class   string
	title   string
	// gone: the handle no longer names a window.
	gone   bool
	pidErr error
}

// fakeWindows implements WindowAPI over a handle table.
type fakeWindows struct {
	byHandle map[HWND]*fakeWnd
	enumErr  error
}

func newFakeWindows() *fakeWindows {
	return &fakeWindows{byHandle: map[HWND]*fakeWnd{}}
}

func (f *fakeWindows) add(h HWND, w *fakeWnd) *fakeWnd {
	f.byHandle[h] = w
	return w
}

func (f *fakeWindows) EnumTopLevel(visit func(HWND)) error {
	if f.enumErr != nil {
		return f.enumErr
	}
	var handles []HWND
	for h, w := range f.byHandle {
		if !w.gone {
			handles = append(handles, h)
		}
	}
	sort.Slice(handles, func(i, j int) bool { return handles[i] < handles[j] })
	for _, h := range handles {
		visit(h)
	}
	return nil
}

func (f *fakeWindows) ThreadProcessID(hwnd HWND) (ProcessID, error) {
	w := f.byHandle[hwnd]
	if w == nil || w.gone {
		return 0, nil
	}
	if w.pidErr != nil {
		return 0, w.pidErr
	}
	return w.pid, nil
}

func (f *fakeWindows) IsWindow(hwnd HWND) bool {
	w := f.byHandle[hwnd]
	return w != nil && !w.gone
}

func (f *fakeWindows) IsWindowVisible(hwnd HWND) bool {
	w := f.byHandle[hwnd]
	return w != nil && w.visible
}

func (f *fakeWindows) IsIconic(hwnd HWND) bool {
	w := f.byHandle[hwnd]
	return w != nil && w.iconic
}

func (f *fakeWindows) Cloaked(hwnd HWND) (bool, error) {
	w := f.byHandle[hwnd]
	if w == nil {
		return false, errors.New("no such window")
	}
	return w.cloaked, nil
}

func (f *fakeWindows) ClassName(hwnd HWND) (string, error) {
	w := f.byHandle[hwnd]
	if w == nil {
		return "", errors.New("no such window")
	}
	return w.class, nil
}

func (f *fakeWindows) Title(hwnd HWND) (string, error) {
	w := f.byHandle[hwnd]
	if w == nil {
		return "", errors.New("no such window")
	}
	return w.title, nil
}

// fakeProc is one fake process instance.
type fakeProc struct {
	path    string
	started time.Time
	err     error
}

// fakeProcesses implements ProcessAPI over a PID table.
type fakeProcesses struct {
	byPID map[ProcessID]fakeProc
}

func newFakeProcesses() *fakeProcesses {
	return &fakeProcesses{byPID: map[ProcessID]fakeProc{}}
}

func (f *fakeProcesses) Query(pid ProcessID) (string, time.Time, error) {
	p, ok := f.byPID[pid]
	if !ok || p.err != nil {
		err := p.err
		if err == nil {
			err = errors.New("no such process")
		}
		return "", time.Time{}, err
	}
	return p.path, p.started, nil
}

// fakeGeometry implements GeometryAPI with settable behaviors; the
// defaults are a consistent 1920×1080 client at the origin with equal
// 96 DPIs and a PMv2-aware caller.
type fakeGeometry struct {
	clientSize  func() (int, int, error)
	toScreen    func(x, y int) (PixelPoint, error)
	windowDPI   func() (uint32, error)
	monitorDPI  func() (uint32, error)
	monitorRect func() (PixelRect, error)
	self        func() (DpiAwareness, error)
}

func newFakeGeometry() *fakeGeometry {
	return &fakeGeometry{
		clientSize:  func() (int, int, error) { return 1920, 1080, nil },
		toScreen:    func(x, y int) (PixelPoint, error) { return PixelPoint{x, y}, nil },
		windowDPI:   func() (uint32, error) { return 96, nil },
		monitorDPI:  func() (uint32, error) { return 96, nil },
		monitorRect: func() (PixelRect, error) { return PixelRect{PixelPoint{0, 0}, PixelPoint{1920, 1080}}, nil },
		self:        func() (DpiAwareness, error) { return DpiAwarenessPerMonitorV2, nil },
	}
}

func (f *fakeGeometry) ClientSize(hwnd HWND) (int, int, error) {
	return f.clientSize()
}

func (f *fakeGeometry) ClientPointToScreen(hwnd HWND, x, y int) (PixelPoint, error) {
	return f.toScreen(x, y)
}

func (f *fakeGeometry) WindowDPI(hwnd HWND) (uint32, error) {
	return f.windowDPI()
}

func (f *fakeGeometry) MonitorDPI(hwnd HWND) (uint32, error) {
	return f.monitorDPI()
}

func (f *fakeGeometry) MonitorRect(hwnd HWND) (PixelRect, error) {
	return f.monitorRect()
}

func (f *fakeGeometry) SelfDpiAwareness() (DpiAwareness, error) {
	return f.self()
}

// fakeForeground implements ForegroundAPI over settable fields.
type fakeForeground struct {
	foreground HWND
	// hits maps checked points to hit windows; hitDefault covers the
	// rest.
	hits       map[PixelPoint]HWND
	hitDefault HWND
	// roots maps windows to their root window; unlisted windows are
	// their own root.
	roots map[HWND]HWND
	// setFgOK: RequestForeground actually moves the foreground.
	setFgOK bool
}

func (f *fakeForeground) ForegroundWindow() HWND { return f.foreground }

func (f *fakeForeground) RequestForeground(hwnd HWND) bool {
	if f.setFgOK {
		f.foreground = hwnd
	}
	return f.foreground == hwnd
}

func (f *fakeForeground) HitWindow(p PixelPoint) HWND {
	if h, ok := f.hits[p]; ok {
		return h
	}
	return f.hitDefault
}

func (f *fakeForeground) RootOf(hwnd HWND) HWND {
	if r, ok := f.roots[hwnd]; ok {
		return r
	}
	return hwnd
}

// fakeSender implements Sender with a scripted response per call. gate,
// when non-nil, blocks every call until it is closed; onEnter, when set,
// runs after the call is recorded and before the gate (for the
// close-vs-dispatch race test).
type fakeSender struct {
	mu      sync.Mutex
	calls   [][]InputEvent
	respond func(call int, events []InputEvent) (int, bool, error)
	gate    chan struct{}
	onEnter func()
}

func newFakeSender() *fakeSender {
	return &fakeSender{
		respond: func(call int, events []InputEvent) (int, bool, error) {
			return len(events), false, nil
		},
	}
}

func (f *fakeSender) SendInput(events []InputEvent) (int, bool, error) {
	f.mu.Lock()
	f.calls = append(f.calls, append([]InputEvent(nil), events...))
	call := len(f.calls) - 1
	f.mu.Unlock()
	if f.onEnter != nil {
		f.onEnter()
	}
	if f.gate != nil {
		<-f.gate
	}
	return f.respond(call, events)
}

func (f *fakeSender) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// fakeGDI implements GDI and accounts for every acquired handle so the
// tests can prove the capture choreography releases everything, in the
// right order: deleting a bitmap that is still selected into a DC is an
// error, exactly like the real API.
type fakeGDI struct {
	next      uintptr
	dcs       map[DC]bool
	bmps      map[GDIObject]bool
	selected  map[DC]GDIObject
	remaining int // handles acquired but not yet released
	bltSrc    []PixelPoint

	screenDCFail  bool
	createDCFail  bool
	createBmpFail bool
	selectFail    bool
	bltFail       bool
	dibitsFail    bool
	deleteFail    bool
	releaseFail   bool

	// fill paints one pixel of the BGRA source; b,g,r,a per pixel.
	fill func(x, y int) (b, g, r, a byte)
}

func newFakeGDI() *fakeGDI {
	return &fakeGDI{
		dcs:      map[DC]bool{},
		bmps:     map[GDIObject]bool{},
		selected: map[DC]GDIObject{},
		fill: func(x, y int) (b, g, r, a byte) {
			return byte(x), byte(y), byte(x + y), 0 // GDI alpha is undefined garbage
		},
	}
}

func (f *fakeGDI) alloc() uintptr {
	f.next++
	f.remaining++
	return f.next
}

func (f *fakeGDI) GetScreenDC() (DC, error) {
	if f.screenDCFail {
		return 0, errors.New("GetDC failed")
	}
	h := DC(f.alloc())
	f.dcs[h] = true
	return h, nil
}

func (f *fakeGDI) ReleaseDC(dc DC) error {
	if f.releaseFail {
		return errors.New("ReleaseDC failed")
	}
	if !f.dcs[dc] {
		return errors.New("ReleaseDC of an unknown DC")
	}
	delete(f.dcs, dc)
	f.remaining--
	return nil
}

func (f *fakeGDI) CreateCompatibleDC(dc DC) (DC, error) {
	if f.createDCFail {
		return 0, errors.New("CreateCompatibleDC failed")
	}
	h := DC(f.alloc())
	f.dcs[h] = true
	return h, nil
}

func (f *fakeGDI) DeleteDC(dc DC) error {
	if f.deleteFail {
		return errors.New("DeleteDC failed")
	}
	if !f.dcs[dc] {
		return errors.New("DeleteDC of an unknown DC")
	}
	delete(f.dcs, dc)
	delete(f.selected, dc)
	f.remaining--
	return nil
}

func (f *fakeGDI) CreateCompatibleBitmap(dc DC, w, h int) (GDIObject, error) {
	if f.createBmpFail {
		return 0, errors.New("CreateCompatibleBitmap failed")
	}
	obj := GDIObject(f.alloc())
	f.bmps[obj] = true
	return obj, nil
}

func (f *fakeGDI) SelectObject(dc DC, obj GDIObject) (GDIObject, error) {
	if f.selectFail {
		return 0, errors.New("SelectObject failed")
	}
	old := f.selected[dc]
	f.selected[dc] = obj
	return old, nil
}

func (f *fakeGDI) DeleteObject(obj GDIObject) error {
	if f.deleteFail {
		return errors.New("DeleteObject failed")
	}
	if !f.bmps[obj] {
		return errors.New("DeleteObject of an unknown bitmap")
	}
	for _, sel := range f.selected {
		if sel == obj {
			return errors.New("DeleteObject of a bitmap still selected into a DC")
		}
	}
	delete(f.bmps, obj)
	f.remaining--
	return nil
}

func (f *fakeGDI) BitBlt(dst DC, dstX, dstY, w, h int, src DC, srcX, srcY int) error {
	if f.bltFail {
		return errors.New("BitBlt failed")
	}
	f.bltSrc = append(f.bltSrc, PixelPoint{srcX, srcY})
	return nil
}

func (f *fakeGDI) GetDIBits(dc DC, bmp GDIObject, buf []byte, w, h int) error {
	if f.dibitsFail {
		return errors.New("GetDIBits failed")
	}
	if !f.bmps[bmp] {
		return errors.New("GetDIBits of an unknown bitmap")
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			b, g, r, a := f.fill(x, y)
			i := 4 * (y*w + x)
			buf[i], buf[i+1], buf[i+2], buf[i+3] = b, g, r, a
		}
	}
	return nil
}

// fakeHotkey implements HotkeyAPI.
type fakeHotkey struct {
	registered map[int]bool
	regErr     error
	unregErr   error
}

func newFakeHotkey() *fakeHotkey {
	return &fakeHotkey{registered: map[int]bool{}}
}

func (f *fakeHotkey) RegisterHotKey(id int, mods HotkeyModifiers, vk VirtualKey) error {
	if f.regErr != nil {
		return f.regErr
	}
	f.registered[id] = true
	return nil
}

func (f *fakeHotkey) UnregisterHotKey(id int) error {
	if f.unregErr != nil {
		return f.unregErr
	}
	delete(f.registered, id)
	return nil
}
