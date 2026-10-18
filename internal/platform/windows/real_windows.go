//go:build windows

package windows

import (
	"errors"
	"fmt"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Real Win32 implementations of the injected API interfaces. They are
// thin: each method is one API call plus error mapping, so the policy —
// identity rules, geometry versioning, dispatch accounting — stays in
// the pure-logic files and stays testable everywhere.

type winWindowAPI struct{}

// RealWindowAPI returns the user32-backed WindowAPI.
func RealWindowAPI() WindowAPI { return winWindowAPI{} }

func (winWindowAPI) EnumTopLevel(visit func(HWND)) error {
	enumMu.Lock()
	defer enumMu.Unlock()
	enumList = enumList[:0]
	if err := windows.EnumWindows(enumWindowsProc, nil); err != nil {
		return fmt.Errorf("EnumWindows: %w", err)
	}
	for _, h := range enumList {
		visit(HWND(h))
	}
	return nil
}

func (winWindowAPI) ThreadProcessID(hwnd HWND) (ProcessID, error) {
	// GetWindowThreadProcessId never fails on a queried handle; a zero
	// PID is the "no owner" answer for a window that vanished.
	var pid uint32
	procGetWindowThreadProcessId.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&pid)))
	return ProcessID(pid), nil
}

func (winWindowAPI) IsWindow(hwnd HWND) bool {
	return windows.IsWindow(windows.HWND(hwnd))
}

func (winWindowAPI) IsWindowVisible(hwnd HWND) bool {
	return windows.IsWindowVisible(windows.HWND(hwnd))
}

func (winWindowAPI) IsIconic(hwnd HWND) bool {
	ret, _, _ := procIsIconic.Call(uintptr(hwnd))
	return ret != 0
}

func (winWindowAPI) Cloaked(hwnd HWND) (bool, error) {
	var cloaked uint32
	err := windows.DwmGetWindowAttribute(windows.HWND(hwnd), windows.DWMWA_CLOAKED, unsafe.Pointer(&cloaked), uint32(unsafe.Sizeof(cloaked)))
	if err != nil {
		return false, fmt.Errorf("DwmGetWindowAttribute(DWMWA_CLOAKED): %w", err)
	}
	return cloaked != 0, nil
}

func (winWindowAPI) ClassName(hwnd HWND) (string, error) {
	var buf [512]uint16
	n, err := windows.GetClassName(windows.HWND(hwnd), &buf[0], int32(len(buf)))
	if err != nil {
		return "", fmt.Errorf("GetClassName: %w", err)
	}
	return windows.UTF16ToString(buf[:n]), nil
}

func (winWindowAPI) Title(hwnd HWND) (string, error) {
	// Reading the caption of another process's top-level window uses the
	// system's cached text and sends the target no message, so a hung
	// game cannot hang the reader.
	var buf [512]uint16
	ret, _, _ := procGetWindowTextW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return windows.UTF16ToString(buf[:ret]), nil
}

type winProcessAPI struct{}

// RealProcessAPI returns the kernel32-backed ProcessAPI. The process
// handle is opened and closed within one Query call — nothing keeps a
// handle to the game process.
func RealProcessAPI() ProcessAPI { return winProcessAPI{} }

func (winProcessAPI) Query(pid ProcessID) (string, time.Time, error) {
	h, err := windows.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("open process %d: %w", pid, err)
	}
	defer windows.CloseHandle(h)

	var buf [1024]uint16
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return "", time.Time{}, fmt.Errorf("query image name of pid %d: %w", pid, err)
	}
	var created, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &created, &exit, &kernel, &user); err != nil {
		return "", time.Time{}, fmt.Errorf("query times of pid %d: %w", pid, err)
	}
	// Filetime.Nanoseconds already converts to nanoseconds since the
	// Unix epoch.
	return windows.UTF16ToString(buf[:size]), time.Unix(0, created.Nanoseconds()).UTC(), nil
}

type winGeometryAPI struct{}

// RealGeometryAPI returns the user32/shcore-backed GeometryAPI. It only
// measures; the process DPI awareness is never changed here.
func RealGeometryAPI() GeometryAPI { return winGeometryAPI{} }

func (winGeometryAPI) ClientSize(hwnd HWND) (int, int, error) {
	var r windows.Rect
	ret, _, err := procGetClientRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&r)))
	if ret == 0 {
		return 0, 0, fmt.Errorf("GetClientRect: %w", callErrno(err))
	}
	return int(r.Right - r.Left), int(r.Bottom - r.Top), nil
}

func (winGeometryAPI) ClientPointToScreen(hwnd HWND, x, y int) (PixelPoint, error) {
	pt := point{X: int32(x), Y: int32(y)}
	ret, _, err := procClientToScreen.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&pt)))
	if ret == 0 {
		return PixelPoint{}, fmt.Errorf("ClientToScreen(%d,%d): %w", x, y, callErrno(err))
	}
	return PixelPoint{X: int(pt.X), Y: int(pt.Y)}, nil
}

func (winGeometryAPI) WindowDPI(hwnd HWND) (uint32, error) {
	if err := procGetDpiForWindow.Find(); err != nil {
		return 0, fmt.Errorf("GetDpiForWindow unavailable (needs Windows 10 1703+): %w", err)
	}
	ret, _, err := procGetDpiForWindow.Call(uintptr(hwnd))
	if ret == 0 {
		return 0, fmt.Errorf("GetDpiForWindow: %w", callErrno(err))
	}
	return uint32(ret), nil
}

func (winGeometryAPI) MonitorDPI(hwnd HWND) (uint32, error) {
	if err := procGetDpiForMonitor.Find(); err != nil {
		return 0, fmt.Errorf("GetDpiForMonitor unavailable (needs shcore): %w", err)
	}
	hmon, _, _ := procMonitorFromWindow.Call(uintptr(hwnd), monitorDefaultToNearest)
	if hmon == 0 {
		return 0, errors.New("MonitorFromWindow: no monitor for the window")
	}
	var dpiX, dpiY uint32
	hr, _, _ := procGetDpiForMonitor.Call(hmon, mdtEffectiveDPI, uintptr(unsafe.Pointer(&dpiX)), uintptr(unsafe.Pointer(&dpiY)))
	if hr != 0 || dpiX == 0 {
		return 0, fmt.Errorf("GetDpiForMonitor: HRESULT 0x%08x", int32(hr))
	}
	return dpiX, nil
}

func (winGeometryAPI) MonitorRect(hwnd HWND) (PixelRect, error) {
	hmon, _, _ := procMonitorFromWindow.Call(uintptr(hwnd), monitorDefaultToNearest)
	if hmon == 0 {
		return PixelRect{}, errors.New("MonitorFromWindow: no monitor for the window")
	}
	var mi monitorInfo
	mi.cbSize = uint32(unsafe.Sizeof(mi))
	ret, _, err := procGetMonitorInfoW.Call(hmon, uintptr(unsafe.Pointer(&mi)))
	if ret == 0 {
		return PixelRect{}, fmt.Errorf("GetMonitorInfoW: %w", callErrno(err))
	}
	return PixelRect{
		Min: PixelPoint{X: int(mi.rcMonitor.Left), Y: int(mi.rcMonitor.Top)},
		Max: PixelPoint{X: int(mi.rcMonitor.Right), Y: int(mi.rcMonitor.Bottom)},
	}, nil
}

func (winGeometryAPI) SelfDpiAwareness() (DpiAwareness, error) {
	if err := procGetCurrentDpiCtx.Find(); err != nil {
		return 0, fmt.Errorf("GetCurrentDpiAwarenessContext unavailable (needs Windows 10 1703+): %w", err)
	}
	ctx, _, _ := procGetCurrentDpiCtx.Call()
	switch ctx {
	case dpiCtxUnaware:
		return DpiAwarenessUnaware, nil
	case dpiCtxSystem:
		return DpiAwarenessSystem, nil
	case dpiCtxPerMonitor:
		return DpiAwarenessPerMonitor, nil
	case dpiCtxPerMonitorV2:
		return DpiAwarenessPerMonitorV2, nil
	default:
		return 0, fmt.Errorf("GetCurrentDpiAwarenessContext returned unknown context %#x", ctx)
	}
}

type winForegroundAPI struct{}

// RealForegroundAPI returns the user32-backed ForegroundAPI.
func RealForegroundAPI() ForegroundAPI { return winForegroundAPI{} }

func (winForegroundAPI) ForegroundWindow() HWND {
	return HWND(windows.GetForegroundWindow())
}

func (winForegroundAPI) RequestForeground(hwnd HWND) bool {
	// SetForegroundWindow's BOOL is not trusted by itself: the system can
	// refuse the request or the focus can bounce elsewhere. The observed
	// foreground after the call is the answer.
	procSetForegroundWindow.Call(uintptr(hwnd))
	return HWND(windows.GetForegroundWindow()) == hwnd
}

func (winForegroundAPI) HitWindow(p PixelPoint) HWND {
	pt := point{X: int32(p.X), Y: int32(p.Y)}
	ret, _, _ := procWindowFromPoint.Call(uintptr(unsafe.Pointer(&pt)))
	return HWND(ret)
}

func (winForegroundAPI) RootOf(hwnd HWND) HWND {
	ret, _, _ := procGetAncestor.Call(uintptr(hwnd), gaRoot)
	return HWND(ret)
}

type winGDIAPI struct{}

// RealGDI returns the gdi32-backed GDI surface. Every method returns an
// error instead of a bare boolean so cleanup failures surface.
func RealGDI() GDI { return winGDIAPI{} }

func (winGDIAPI) GetScreenDC() (DC, error) {
	ret, _, err := procGetDC.Call(0)
	if ret == 0 {
		return 0, fmt.Errorf("GetDC: %w", callErrno(err))
	}
	return DC(ret), nil
}

func (winGDIAPI) ReleaseDC(dc DC) error {
	ret, _, err := procReleaseDC.Call(0, uintptr(dc))
	if ret == 0 {
		return fmt.Errorf("ReleaseDC: %w", callErrno(err))
	}
	return nil
}

func (winGDIAPI) CreateCompatibleDC(dc DC) (DC, error) {
	ret, _, err := procCreateCompatibleDC.Call(uintptr(dc))
	if ret == 0 {
		return 0, fmt.Errorf("CreateCompatibleDC: %w", callErrno(err))
	}
	return DC(ret), nil
}

func (winGDIAPI) DeleteDC(dc DC) error {
	ret, _, err := procDeleteDC.Call(uintptr(dc))
	if ret == 0 {
		return fmt.Errorf("DeleteDC: %w", callErrno(err))
	}
	return nil
}

func (winGDIAPI) CreateCompatibleBitmap(dc DC, w, h int) (GDIObject, error) {
	ret, _, err := procCreateCompatibleBitmap.Call(uintptr(dc), uintptr(w), uintptr(h))
	if ret == 0 {
		return 0, fmt.Errorf("CreateCompatibleBitmap(%d×%d): %w", w, h, callErrno(err))
	}
	return GDIObject(ret), nil
}

func (winGDIAPI) SelectObject(dc DC, obj GDIObject) (GDIObject, error) {
	ret, _, err := procSelectObject.Call(uintptr(dc), uintptr(obj))
	if ret == 0 || ret == ^uintptr(0) {
		return 0, fmt.Errorf("SelectObject: %w", callErrno(err))
	}
	return GDIObject(ret), nil
}

func (winGDIAPI) DeleteObject(obj GDIObject) error {
	ret, _, err := procDeleteObject.Call(uintptr(obj))
	if ret == 0 {
		return fmt.Errorf("DeleteObject: %w", callErrno(err))
	}
	return nil
}

func (winGDIAPI) BitBlt(dst DC, dstX, dstY, w, h int, src DC, srcX, srcY int) error {
	ret, _, err := procBitBlt.Call(
		uintptr(dst), uintptr(dstX), uintptr(dstY), uintptr(w), uintptr(h),
		uintptr(src), uintptr(srcX), uintptr(srcY), srccopy)
	if ret == 0 {
		return fmt.Errorf("BitBlt: %w", callErrno(err))
	}
	return nil
}

func (winGDIAPI) GetDIBits(dc DC, bmp GDIObject, buf []byte, w, h int) error {
	var bi bitmapInfoHeader
	bi.biSize = uint32(unsafe.Sizeof(bi))
	bi.biWidth = int32(w)
	bi.biHeight = int32(-h) // negative: top-down rows
	bi.biPlanes = 1
	bi.biBitCount = 32
	ret, _, err := procGetDIBits.Call(
		uintptr(dc), uintptr(bmp), 0, uintptr(h),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&bi)), dibRGBColors)
	if ret == 0 {
		return fmt.Errorf("GetDIBits: %w", callErrno(err))
	}
	return nil
}

type winSender struct{}

// RealSender returns the user32-backed Sender.
func RealSender() Sender { return winSender{} }

// SendInput marshals the events into one SendInput call. The desktop
// coordinate space is measured fresh at dispatch time: a display change
// between observation and dispatch makes the conversion fail (point no
// longer inside the measured desktop) instead of misclicking.
func (winSender) SendInput(events []InputEvent) (int, bool, error) {
	if len(events) == 0 {
		return 0, false, errors.New("SendInput: empty event list")
	}
	if len(events) > senderBatchLimit {
		return 0, false, fmt.Errorf("SendInput: %d events exceeds the bounded batch of %d", len(events), senderBatchLimit)
	}
	space, err := currentCoordinateSpace()
	if err != nil {
		return 0, false, err
	}
	ins := make([]inputStruct, len(events))
	for i, ev := range events {
		e := &ins[i]
		switch ev.Kind {
		case EventMouseMove:
			abs, virtualDesk, err := PointToMouseAbsolute(ev.Point, space)
			if err != nil {
				return 0, false, fmt.Errorf("convert %v: %w", ev.Point, err)
			}
			flags := uint32(mouseEventfMove | mouseEventfAbsolute)
			if virtualDesk {
				flags |= mouseEventfVirtualDesk
			}
			e.typ = inputMouse
			e.mi = mouseInput{dx: int32(abs.DX), dy: int32(abs.DY), dwFlags: flags}
		case EventMouseDown, EventMouseUp:
			var down, up uint32
			switch ev.Button {
			case MouseButtonLeft:
				down, up = mouseEventfLeftDown, mouseEventfLeftUp
			case MouseButtonRight:
				down, up = mouseEventfRightDown, mouseEventfRightUp
			case MouseButtonMiddle:
				down, up = mouseEventfMiddleDown, mouseEventfMiddleUp
			default:
				return 0, false, fmt.Errorf("SendInput: invalid mouse button %d", ev.Button)
			}
			e.typ = inputMouse
			if ev.Kind == EventMouseDown {
				e.mi = mouseInput{dwFlags: down}
			} else {
				e.mi = mouseInput{dwFlags: up}
			}
		case EventWheel:
			e.typ = inputMouse
			// One detent per event; the countable unit keeps partial
			// deliveries meaningful.
			e.mi = mouseInput{mouseData: uint32(int32(wheelDelta * ev.WheelSteps)), dwFlags: mouseEventfWheel}
		case EventKeyDown, EventKeyUp:
			flags := uint32(0)
			if ev.Kind == EventKeyUp {
				flags = keyEventfKeyUp
			}
			e.typ = inputKeyboard
			// keybdInput overlays the union member at the same offset.
			*(*keybdInput)(unsafe.Pointer(&e.mi)) = keybdInput{wVk: uint16(ev.Key), dwFlags: flags}
		default:
			return 0, false, fmt.Errorf("SendInput: unknown event kind %d", ev.Kind)
		}
	}
	ret, _, err := procSendInput.Call(uintptr(len(ins)), uintptr(unsafe.Pointer(&ins[0])), unsafe.Sizeof(ins[0]))
	accepted := int(ret)
	if accepted == len(ins) {
		return accepted, false, nil
	}
	if accepted == 0 {
		if errno := callErrno(err); errno != nil {
			if errors.Is(errno, windows.ERROR_ACCESS_DENIED) {
				// SendInput's error text does not name UIPI; zero delivery
				// with access denial is the shape a privilege mismatch
				// takes, and this classification is reported as such.
				return 0, true, nil
			}
			return 0, false, fmt.Errorf("SendInput: %w", errno)
		}
		return 0, false, errors.New("SendInput inserted no events and reported no error; input may be blocked by UIPI or another thread")
	}
	// Partial delivery: the count is the fact; the last error of a
	// partial call is not reliably fresh, so it is not attached.
	return accepted, false, nil
}

// currentCoordinateSpace measures the primary and virtual-desktop
// extents as physical pixels of this per-monitor-v2-aware thread.
func currentCoordinateSpace() (CoordinateSpace, error) {
	pw := systemMetric(smCXScreen)
	ph := systemMetric(smCYScreen)
	vx := systemMetric(smXVirtualScreen)
	vy := systemMetric(smYVirtualScreen)
	vw := systemMetric(smCXVirtualScreen)
	vh := systemMetric(smCYVirtualScreen)
	if pw <= 0 || ph <= 0 || vw <= 0 || vh <= 0 {
		return CoordinateSpace{}, fmt.Errorf("GetSystemMetrics returned non-positive desktop extents: primary %d×%d, virtual %d×%d at (%d,%d)", pw, ph, vw, vh, vx, vy)
	}
	return CoordinateSpace{
		Primary: PixelRect{Min: PixelPoint{0, 0}, Max: PixelPoint{pw, ph}},
		Virtual: PixelRect{Min: PixelPoint{vx, vy}, Max: PixelPoint{vx + vw, vy + vh}},
	}, nil
}

func systemMetric(index int) int {
	ret, _, _ := procGetSystemMetrics.Call(uintptr(index))
	return int(int32(ret))
}

type winHotkeyAPI struct{}

// RealHotkeyAPI returns the user32-backed HotkeyAPI.
func RealHotkeyAPI() HotkeyAPI { return winHotkeyAPI{} }

func (winHotkeyAPI) RegisterHotKey(id int, mods HotkeyModifiers, vk VirtualKey) error {
	ret, _, err := procRegisterHotKey.Call(0, uintptr(id), uintptr(mods), uintptr(vk))
	if ret == 0 {
		return fmt.Errorf("RegisterHotKey id %d: %w", id, callErrno(err))
	}
	return nil
}

func (winHotkeyAPI) UnregisterHotKey(id int) error {
	ret, _, err := procUnregisterHotKey.Call(0, uintptr(id))
	if ret == 0 {
		return fmt.Errorf("UnregisterHotKey id %d: %w", id, callErrno(err))
	}
	return nil
}
