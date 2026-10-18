//go:build windows

package windows

import (
	"errors"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Raw Win32 declarations. x/sys/windows covers the kernel32 and part of
// the user32 surface; the rest — window geometry, DPI, SendInput, hotkey
// and message loop, and all of gdi32 — are declared here as procedures
// of the system DLLs and called through golang.org/x/sys/windows'
// LazyDLL machinery. No cgo is involved.
//
// The INPUT/MSG structure layouts below are pinned to windows/amd64
// (T2 §2.1: fixed x64 structure layout); the package does not support
// 32-bit Windows.
var (
	modUser32 = windows.NewLazySystemDLL("user32.dll")
	modGdi32  = windows.NewLazySystemDLL("gdi32.dll")
	modShcore = windows.NewLazySystemDLL("shcore.dll")

	procGetWindowThreadProcessId = modUser32.NewProc("GetWindowThreadProcessId")
	procGetWindowTextW           = modUser32.NewProc("GetWindowTextW")
	procIsIconic                 = modUser32.NewProc("IsIconic")
	procGetClientRect            = modUser32.NewProc("GetClientRect")
	procClientToScreen           = modUser32.NewProc("ClientToScreen")
	procGetDpiForWindow          = modUser32.NewProc("GetDpiForWindow")
	procMonitorFromWindow        = modUser32.NewProc("MonitorFromWindow")
	procGetMonitorInfoW          = modUser32.NewProc("GetMonitorInfoW")
	procWindowFromPoint          = modUser32.NewProc("WindowFromPoint")
	procGetAncestor              = modUser32.NewProc("GetAncestor")
	procSetForegroundWindow      = modUser32.NewProc("SetForegroundWindow")
	procGetSystemMetrics         = modUser32.NewProc("GetSystemMetrics")
	procSendInput                = modUser32.NewProc("SendInput")
	procRegisterHotKey           = modUser32.NewProc("RegisterHotKey")
	procUnregisterHotKey         = modUser32.NewProc("UnregisterHotKey")
	procGetMessageW              = modUser32.NewProc("GetMessageW")
	procPostThreadMessageW       = modUser32.NewProc("PostThreadMessageW")
	procGetCurrentDpiCtx         = modUser32.NewProc("GetCurrentDpiAwarenessContext")

	procGetDC                  = modGdi32.NewProc("GetDC")
	procReleaseDC              = modGdi32.NewProc("ReleaseDC")
	procCreateCompatibleDC     = modGdi32.NewProc("CreateCompatibleDC")
	procDeleteDC               = modGdi32.NewProc("DeleteDC")
	procCreateCompatibleBitmap = modGdi32.NewProc("CreateCompatibleBitmap")
	procSelectObject           = modGdi32.NewProc("SelectObject")
	procDeleteObject           = modGdi32.NewProc("DeleteObject")
	procBitBlt                 = modGdi32.NewProc("BitBlt")
	procGetDIBits              = modGdi32.NewProc("GetDIBits")

	procGetDpiForMonitor = modShcore.NewProc("GetDpiForMonitor")
)

// Win32 constants used by the declarations below.
const (
	wmQuit   = 0x0012
	wmHotkey = 0x0312

	gaRoot                  = 2 // GetAncestor: root window
	monitorDefaultToNearest = 2
	mdtEffectiveDPI         = 0

	srccopy       = 0x00CC0020
	dibRGBColors  = 0
	biRGB         = 0
	wheelDelta    = 120
	inputMouse    = 0
	inputKeyboard = 1

	mouseEventfMove        = 0x0001
	mouseEventfLeftDown    = 0x0002
	mouseEventfLeftUp      = 0x0004
	mouseEventfRightDown   = 0x0008
	mouseEventfRightUp     = 0x0010
	mouseEventfMiddleDown  = 0x0020
	mouseEventfMiddleUp    = 0x0040
	mouseEventfWheel       = 0x0800
	mouseEventfVirtualDesk = 0x4000
	mouseEventfAbsolute    = 0x8000

	keyEventfKeyUp = 0x0002

	smCXScreen        = 0
	smCYScreen        = 1
	smXVirtualScreen  = 76
	smYVirtualScreen  = 77
	smCXVirtualScreen = 78
	smCYVirtualScreen = 79

	processQueryLimitedInformation = 0x1000

	// DPI awareness context handle values (negative HANDLEs as uintptr).
	dpiCtxUnaware      = ^uintptr(0) // -1
	dpiCtxSystem       = ^uintptr(1) // -2
	dpiCtxPerMonitor   = ^uintptr(2) // -3
	dpiCtxPerMonitorV2 = ^uintptr(3) // -4
)

// point is the Win32 POINT layout.
type point struct{ X, Y int32 }

// monitorInfo is the Win32 MONITORINFO layout.
type monitorInfo struct {
	cbSize    uint32
	rcMonitor windows.Rect
	rcWork    windows.Rect
	dwFlags   uint32
}

// bitmapInfoHeader is the Win32 BITMAPINFOHEADER layout. A negative
// biHeight makes GetDIBits produce top-down rows.
type bitmapInfoHeader struct {
	biSize          uint32
	biWidth         int32
	biHeight        int32
	biPlanes        uint16
	biBitCount      uint16
	biCompression   uint32
	biSizeImage     uint32
	biXPelsPerMeter int32
	biYPelsPerMeter int32
	biClrUsed       uint32
	biClrImportant  uint32
}

// mouseInput is the Win32 MOUSEINPUT layout (amd64).
type mouseInput struct {
	dx          int32
	dy          int32
	mouseData   uint32
	dwFlags     uint32
	time        uint32
	_           [4]byte // padding to ULONG_PTR alignment
	dwExtraInfo uintptr
}

// keybdInput is the Win32 KEYBDINPUT layout (amd64). SendInput events
// overlay it on the union member of inputStruct.
type keybdInput struct {
	wVk         uint16
	wScan       uint16
	dwFlags     uint32
	time        uint32
	_           [4]byte
	dwExtraInfo uintptr
}

// inputStruct is the Win32 INPUT layout (amd64): a type tag followed by
// a union; mouseInput is the largest member and keyboard events overlay
// keybdInput at the same offset.
type inputStruct struct {
	typ uint32
	_   [4]byte // union alignment
	mi  mouseInput
}

// msg is the Win32 MSG layout (amd64). Only the message field is read.
type msg struct {
	hwnd    uintptr
	message uint32
	_       [4]byte
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      point
}

// Compile-time pins of the amd64 layouts this package depends on.
var (
	_ = [unsafe.Sizeof(inputStruct{}) - 40]struct{}{}
	_ = [40 - unsafe.Sizeof(inputStruct{})]struct{}{}
	_ = [unsafe.Sizeof(keybdInput{}) - 24]struct{}{}
	_ = [24 - unsafe.Sizeof(keybdInput{})]struct{}{}
	_ = [unsafe.Sizeof(mouseInput{}) - 32]struct{}{}
	_ = [32 - unsafe.Sizeof(mouseInput{})]struct{}{}
	_ = [unsafe.Sizeof(msg{}) - 48]struct{}{}
	_ = [48 - unsafe.Sizeof(msg{})]struct{}{}
)

// enumMu guards enumList: EnumWindows runs its callback synchronously
// on the calling thread, so the slice is safe while the lock is held.
var (
	enumMu   sync.Mutex
	enumList []windows.Handle
)

// enumWindowsProc is the single ENUMWNDPROC callback of the package.
// The collected handles live in a lock-guarded package variable instead
// of an unsafe pointer round-trip through LPARAM. Returning 1 keeps the
// enumeration going.
var enumWindowsProc = syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
	enumList = append(enumList, windows.Handle(hwnd))
	return 1
})

// callErrno extracts the fresh system error of a failed call. LazyProc
// calls always return a non-nil error; it is meaningful only when the
// call's own return value signals failure (a stale last error on a
// successful call is normal).
func callErrno(err error) error {
	var errno syscall.Errno
	if errors.As(err, &errno) && errno != 0 {
		return errno
	}
	return nil
}
