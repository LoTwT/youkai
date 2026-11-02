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
// top-level window of its own, so a desktop that merely lacks other
// application windows still has a guaranteed hit target. Passing a
// pointer where Win32 expects the 8-byte structure makes the call return
// zero for every point, which this test reports.
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
		t.Fatalf("HitWindow(%v) returned 0; the test's own window %#x is visible at that point", center, hwnd)
	}
	t.Logf("HitWindow(%v) = %#x for the test's own window %#x", center, hit, hwnd)
}
