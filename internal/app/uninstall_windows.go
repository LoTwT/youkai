//go:build windows

package app

import (
	"errors"
	"fmt"

	"golang.org/x/sys/windows/registry"
)

// uninstallKeyPath is the per-user uninstall entry the NSIS installer
// writes (cmd/mygo's NSIS template, under mygo.json's identifier):
// "应用和功能" — Windows' Apps & Features — lists the app from it.
const uninstallKeyPath = `Software\Microsoft\Windows\CurrentVersion\Uninstall\` + baseAppIdentifier

// RefreshUninstallDisplayVersion keeps the uninstall entry's
// DisplayVersion at the running version: the installer writes it once, on
// the day it ran, and the in-place updater never touches the registry
// (MyGo replaces the app's files only), so without this "应用和功能" would
// keep showing the version the user installed, not the one they updated
// to. The NSIS uninstaller never reads the value — its Uninstall section
// deletes the whole key — so refreshing it cannot disturb an uninstall.
//
// A missing key is not an error: nothing was installed by NSIS (a
// portable build, or the entry was removed), and updated stays false. The
// caller is expected to be the production build that owns the entry (the
// entry point guards on the app name and IsDev).
func RefreshUninstallDisplayVersion(version string) (updated bool, err error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, uninstallKeyPath,
		registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("app: open the uninstall entry: %w", err)
	}
	defer k.Close()
	current, _, err := k.GetStringValue("DisplayVersion")
	if err != nil && !errors.Is(err, registry.ErrNotExist) {
		return false, fmt.Errorf("app: read the uninstall entry's DisplayVersion: %w", err)
	}
	if current == version {
		return false, nil
	}
	if err := k.SetStringValue("DisplayVersion", version); err != nil {
		return false, fmt.Errorf("app: set the uninstall entry's DisplayVersion: %w", err)
	}
	return true, nil
}
