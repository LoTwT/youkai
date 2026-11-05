//go:build windows

package app

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// currentSystemInfo reads the real OS build and main-display metrics.
// The process is per-monitor-v2 DPI aware (MyGo sets the awareness
// context before creating windows), so GetSystemMetrics reports physical
// pixels rather than a virtualized rectangle.
func currentSystemInfo() SystemInfo {
	info := SystemInfo{OS: "Windows"}
	v := windows.RtlGetVersion()
	info.OSVersion = fmt.Sprintf("%d.%d.%d", v.MajorVersion, v.MinorVersion, v.BuildNumber)
	info.DisplayWidth, info.DisplayHeight, info.DisplayScale = currentDisplay()
	return info
}

var (
	diagnosticsUser32    = windows.NewLazySystemDLL("user32.dll")
	procGetSystemMetrics = diagnosticsUser32.NewProc("GetSystemMetrics")
	procGetDpiForSystem  = diagnosticsUser32.NewProc("GetDpiForSystem")
)

func currentDisplay() (width, height int, scale float64) {
	const (
		smCXScreen = 0
		smCYScreen = 1
	)
	w, _, _ := procGetSystemMetrics.Call(smCXScreen)
	h, _, _ := procGetSystemMetrics.Call(smCYScreen)
	width, height = int(w), int(h)

	// GetDpiForSystem exists from Windows 10 1607; an older system still
	// gets the resolution and an unknown scale instead of a fake 100%.
	if err := procGetDpiForSystem.Find(); err != nil {
		return width, height, 0
	}
	if dpi, _, _ := procGetDpiForSystem.Call(); dpi > 0 {
		scale = float64(dpi) / 96
	}
	return width, height, scale
}
