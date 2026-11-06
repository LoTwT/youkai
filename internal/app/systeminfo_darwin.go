//go:build darwin

package app

import "golang.org/x/sys/unix"

// currentSystemInfo reads the macOS product version and main-display
// metrics. kern.osproductversion is the marketing version (14.6); older
// kernels fall back to the Darwin release so the field is never invented.
func currentSystemInfo() SystemInfo {
	info := SystemInfo{OS: "macOS"}
	if version, err := unix.Sysctl("kern.osproductversion"); err == nil {
		info.OSVersion = version
	} else if release, err := unix.Sysctl("kern.osrelease"); err == nil {
		info.OSVersion = "Darwin " + release
	}
	info.DisplayWidth, info.DisplayHeight, info.DisplayScale = currentDisplay()
	return info
}
