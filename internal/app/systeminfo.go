package app

// CurrentSystemInfo reports the operating-system and main-display
// context a diagnostic copy includes. Each supported platform supplies
// it with build tags; an unsupported platform still names its OS, and
// the diagnostic text says the rest is unknown.
//
// On Windows the display figures depend on the process's DPI awareness:
// a packaged build is per-monitor-v2 from process start through its
// manifest, while an unpackaged build becomes per-monitor-v2 when MyGo
// initializes. Calling this before that point — for example on an early
// startup failure — reports the virtualized desktop: logical pixels and
// 96 DPI.
func CurrentSystemInfo() SystemInfo {
	return currentSystemInfo()
}
