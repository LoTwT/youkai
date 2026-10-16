//go:build windows

package app

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// currentOSRoots resolves the Windows layout from the Known Folder
// FOLDERID_LocalAppData, which follows the folder the user configured:
// no user-name concatenation, no assumption about the system drive. The
// framework's own defaults put user data in %APPDATA% instead, which is
// why youkai overrides them.
func currentOSRoots(appName string) (Paths, error) {
	localAppData, err := userAppDataBase()
	if err != nil {
		return Paths{}, fmt.Errorf("app: resolve paths: query FOLDERID_LocalAppData: %w", err)
	}
	return windowsRoots(localAppData, appName), nil
}

// userAppDataBase returns the per-user base directory for application
// data, the Known Folder FOLDERID_LocalAppData — the same base
// currentOSRoots resolves the app roots from. State that must stay
// independent of the application name — the scan lock of the occupancy
// locks — anchors in a fixed subdirectory of this base instead of under
// an app name.
func userAppDataBase() (string, error) {
	return windows.KnownFolderPath(windows.FOLDERID_LocalAppData, windows.KF_FLAG_DEFAULT)
}
