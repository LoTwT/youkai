//go:build !windows && !darwin

package app

import "runtime"

// currentSystemInfo names the unsupported platform; version and display
// are reported as unknown because no platform-specific reader exists.
func currentSystemInfo() SystemInfo {
	return SystemInfo{OS: runtime.GOOS}
}
