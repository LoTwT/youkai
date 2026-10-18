package windows

import (
	"errors"
	"sort"
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
