//go:build windows

package app

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// currentSystemInfo reads the real OS build and main-display metrics.
// The display values depend on the process's DPI awareness: a packaged
// build is per-monitor-v2 from process start through its manifest,
// while an unpackaged build becomes per-monitor-v2 when MyGo
// initializes. Called before that point (for example on an early
// startup failure), the values describe the virtualized desktop:
// logical pixels and 96 DPI.
func currentSystemInfo() SystemInfo {
	info := SystemInfo{OS: "Windows"}
	v := windows.RtlGetVersion()
	info.OSVersion = fmt.Sprintf("%d.%d.%d", v.MajorVersion, v.MinorVersion, v.BuildNumber)
	info.DisplayWidth, info.DisplayHeight, info.DisplayScale = currentDisplay()
	return info
}

const (
	smCXScreen = 0
	smCYScreen = 1

	monitorDefaultToPrimary = 1
	monitorDpiTypeEffective = 0
	dpiDefault              = 96

	// pointOrigin packs POINT{0,0} into the single register Win64 passes
	// a two-LONG structure in.
	pointOrigin = 0
)

var (
	diagnosticsUser32    = windows.NewLazySystemDLL("user32.dll")
	diagnosticsShcore    = windows.NewLazySystemDLL("shcore.dll")
	procGetSystemMetrics = diagnosticsUser32.NewProc("GetSystemMetrics")
	procMonitorFromPoint = diagnosticsUser32.NewProc("MonitorFromPoint")
	procGetDpiForMonitor = diagnosticsShcore.NewProc("GetDpiForMonitor")
	procGetDpiForSystem  = diagnosticsUser32.NewProc("GetDpiForSystem")
)

func currentDisplay() (width, height int, scale float64) {
	w, _, _ := procGetSystemMetrics.Call(smCXScreen)
	h, _, _ := procGetSystemMetrics.Call(smCYScreen)
	return int(w), int(h), currentDisplayScale()
}

// currentDisplayScale reads the primary monitor's effective DPI. Unlike
// GetDpiForSystem, which reports the system DPI captured at sign-in,
// GetDpiForMonitor reflects a scale change made in the current session.
// GetDpiForSystem is only a fallback; a Windows without either function
// reports an unknown scale instead of a fake 100%.
func currentDisplayScale() float64 {
	monitor, _, _ := procMonitorFromPoint.Call(pointOrigin, monitorDefaultToPrimary)
	if monitor != 0 && procGetDpiForMonitor.Find() == nil {
		var dpiX, dpiY uint32
		hr, _, _ := procGetDpiForMonitor.Call(
			monitor,
			monitorDpiTypeEffective,
			uintptr(unsafe.Pointer(&dpiX)),
			uintptr(unsafe.Pointer(&dpiY)),
		)
		if hr == 0 && dpiX > 0 {
			return float64(dpiX) / dpiDefault
		}
	}
	if procGetDpiForSystem.Find() == nil {
		if dpi, _, _ := procGetDpiForSystem.Call(); dpi > 0 {
			return float64(dpi) / dpiDefault
		}
	}
	return 0
}
