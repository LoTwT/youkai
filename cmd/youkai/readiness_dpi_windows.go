//go:build windows

package main

import "golang.org/x/sys/windows"

// prepareReadinessDPI sets the process to per-monitor-v2 DPI awareness for
// the headless readiness run. MyGo does this in its backend init, which a
// headless run never reaches; without it the read-only DPI row would report
// a false dpiAwarenessMismatch. A packaged manifest may have set it already,
// in which case the call fails and the check reports the actual awareness.
func prepareReadinessDPI() error {
	proc := windows.NewLazySystemDLL("user32.dll").NewProc("SetProcessDpiAwarenessContext")
	if err := proc.Find(); err != nil {
		return err
	}
	const perMonitorV2 = ^uintptr(0) - 3 // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 (-4)
	ret, _, err := proc.Call(perMonitorV2)
	if ret == 0 {
		return err
	}
	return nil
}
