package app

// CurrentSystemInfo reports the operating-system and main-display
// context a diagnostic copy includes. Each supported platform supplies
// it with build tags; an unsupported platform still names its OS, and
// the diagnostic text says the rest is unknown.
func CurrentSystemInfo() SystemInfo {
	return currentSystemInfo()
}
