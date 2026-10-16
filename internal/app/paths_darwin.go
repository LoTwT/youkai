//go:build darwin

package app

import (
	"fmt"
	"os"
	"path/filepath"
)

// currentOSRoots resolves the macOS layout from the standard per-user
// directories of the Go runtime interfaces, which on macOS are the
// system-defined ~/Library locations; the framework itself resolves its
// defaults the same way. Logs have no dedicated interface, so they join
// the standard ~/Library/Logs base, as the framework's default does.
func currentOSRoots(appName string) (Paths, error) {
	appSupport, err := os.UserConfigDir()
	if err != nil {
		return Paths{}, fmt.Errorf("app: resolve paths: resolve application support: %w", err)
	}
	caches, err := os.UserCacheDir()
	if err != nil {
		return Paths{}, fmt.Errorf("app: resolve paths: resolve caches: %w", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, fmt.Errorf("app: resolve paths: resolve home: %w", err)
	}
	return appleRoots(appSupport, caches, filepath.Join(home, "Library", "Logs"), appName), nil
}
