//go:build !windows

package app

// RefreshUninstallDisplayVersion keeps the NSIS uninstall entry's
// DisplayVersion at the running version on Windows; other platforms
// install no such entry (macOS apps are bundles, there is no uninstall
// list), so there is nothing to refresh.
func RefreshUninstallDisplayVersion(string) (updated bool, err error) {
	return false, nil
}
