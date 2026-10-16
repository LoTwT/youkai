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
	localAppData, err := windows.KnownFolderPath(windows.FOLDERID_LocalAppData, windows.KF_FLAG_DEFAULT)
	if err != nil {
		return Paths{}, fmt.Errorf("app: resolve paths: query FOLDERID_LocalAppData: %w", err)
	}
	return windowsRoots(localAppData, appName), nil
}
