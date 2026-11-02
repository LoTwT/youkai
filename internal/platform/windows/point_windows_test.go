//go:build windows

package windows

import (
	"runtime"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// TestHitWindowFindsOwnedWindow pins the by-value POINT argument of
// WindowFromPoint against the real API. The test creates a visible
// top-level STATIC window as a fixture, then checks a real on-screen
// point with the platform's hit test. A STATIC control without
// SS_NOTIFY is transparent to hit testing, so the hit is not required to
// be the fixture window. The regression this guards is the opposite:
// passing the POINT address where Win32 expects the 8-byte value makes
// Windows read the address as out-of-screen coordinates and return zero
// for every point.
func TestHitWindowFindsOwnedWindow(t *testing.T) {
	// The window belongs to the creating thread; destroy it on the same
	// thread after the hit test.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	var (
		procCreateWindowExW = modUser32.NewProc("CreateWindowExW")
		procDestroyWindow   = modUser32.NewProc("DestroyWindow")
		procGetWindowRect   = modUser32.NewProc("GetWindowRect")
	)

	class, err := windows.UTF16PtrFromString("STATIC")
	if err != nil {
		t.Fatalf("encode window class: %v", err)
	}
	title, err := windows.UTF16PtrFromString("youkai hit-test fixture")
	if err != nil {
		t.Fatalf("encode window title: %v", err)
	}

	const (
		wsPopup   = 0x80000000
		wsVisible = 0x10000000
	)
	hwnd, _, callErr := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(class)),
		uintptr(unsafe.Pointer(title)),
		wsPopup|wsVisible,
		100, 100, 400, 300,
		0, 0, 0, 0,
	)
	if hwnd == 0 {
		t.Fatalf("CreateWindowExW: %v", callErr)
	}
	defer procDestroyWindow.Call(hwnd)

	var rect windows.Rect
	if ret, _, callErr := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rect))); ret == 0 {
		t.Fatalf("GetWindowRect: %v", callErr)
	}
	center := PixelPoint{
		X: int(rect.Left + (rect.Right-rect.Left)/2),
		Y: int(rect.Top + (rect.Bottom-rect.Top)/2),
	}

	hit := RealForegroundAPI().HitWindow(center)
	if hit == 0 {
		t.Fatalf("HitWindow(%v) returned 0; a real on-screen point must hit some non-zero window (the fixture %#x is transparent to hit testing), while passing the POINT address as coordinates misses the screen", center, hwnd)
	}
	t.Logf("HitWindow(%v) = %#x; fixture window %#x", center, hit, hwnd)
}
