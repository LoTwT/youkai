// Package app holds youkai's application-level foundations and the
// application service that composes them: directory resolution, the
// diagnostic log sink (a rotating file mirrored to stderr, file first),
// the cross-process occupancy locks guarding a data root and the per-user
// scan slot, the scan payloads and the recognizer adapter bridging the
// session kernel to the OCR runtime, the development-only scan data
// source, and the service that owns sessions, saving, records and the
// explicit-exit wrap-up. MyGo views never touch the kernels directly;
// they call the service.
package app

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// Paths are youkai's well-known directories, resolved once while the
// process starts up and handed to the components that need them.
type Paths struct {
	// Data is the persistent per-user data root: settings, scan records,
	// checkpoints and evidence live under it. It is the identity the
	// data-root lock of a later milestone keys on.
	Data string
	// Cache is the root of rebuildable files only, such as thumbnails and
	// indexes. Clearing it must never touch Data: on Windows the cache is
	// a "cache" subdirectory of Data, on macOS it is the system cache
	// location beside it.
	Cache string
	// Logs is the directory of diagnostic logs.
	Logs string
	// Resources is the absolute root of the files the app ships with:
	// models, dictionaries and native libraries. Components receive it
	// explicitly instead of looking at the working directory; tests pass
	// their own root.
	Resources string
}

// Options configure Resolve.
type Options struct {
	// Name is the application name the per-user directories are named
	// after: "Youkai" for installed builds and "Youkai Dev" for the
	// development app of `go tool mygo dev`. The framework already names
	// the two differently, which is what keeps their data, cache and log
	// roots apart; a plain `go run` falls back to the executable's name.
	Name string
	// Resources is the shipped-resources root as the host resolved it
	// from the framework's PathResources. It must be absolute.
	Resources string
}

// Resolve computes the app's directories for the current user on the
// current OS. It only resolves paths; creating the directories is left to
// whoever writes first.
func Resolve(opts Options) (Paths, error) {
	if err := validateName(opts.Name); err != nil {
		return Paths{}, err
	}
	paths, err := currentOSRoots(opts.Name)
	if err != nil {
		return Paths{}, err
	}
	paths.Resources = opts.Resources
	if err := paths.validate(); err != nil {
		return Paths{}, err
	}
	return paths, nil
}

// validateName rejects names that could not name a single directory under
// the OS bases: empty names, and names carrying separators or standing
// for a parent, which would nest, escape or misplace the roots.
func validateName(name string) error {
	if name == "" {
		return errors.New("app: resolve paths: application name is empty")
	}
	if strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
		return fmt.Errorf("app: resolve paths: application name %q is not a single directory name", name)
	}
	return nil
}

// validate requires every root to be a non-empty absolute path, so that a
// broken environment lookup fails here instead of the app writing to a
// path relative to whatever the working directory happens to be.
func (p Paths) validate() error {
	dirs := []struct{ name, dir string }{
		{"data", p.Data},
		{"cache", p.Cache},
		{"logs", p.Logs},
		{"resources", p.Resources},
	}
	for _, d := range dirs {
		if d.dir == "" {
			return fmt.Errorf("app: resolve paths: %s directory is empty", d.name)
		}
		if !filepath.IsAbs(d.dir) {
			return fmt.Errorf("app: resolve paths: %s directory %q is not absolute", d.name, d.dir)
		}
	}
	return nil
}

// windowsRoots returns the Windows layout of the app's directories under
// localAppData, the path the Known Folder FOLDERID_LocalAppData resolves
// to: persistent data with its "cache" and "logs" subdirectories, all
// under %LOCALAPPDATA%\<app>\. Nesting keeps cache cleanup inside the
// cache subtree. Name and base come from the caller, which makes the rule
// testable on any OS.
func windowsRoots(localAppData, appName string) Paths {
	data := filepath.Join(localAppData, appName)
	return Paths{
		Data:  data,
		Cache: filepath.Join(data, "cache"),
		Logs:  filepath.Join(data, "logs"),
	}
}

// appleRoots returns the macOS layout: the app name under each of the
// three standard per-user bases the system defines — appSupport
// (~/Library/Application Support), caches (~/Library/Caches) and logs
// (~/Library/Logs). Name and bases come from the caller, which makes the
// rule testable on any OS.
func appleRoots(appSupport, caches, logs, appName string) Paths {
	return Paths{
		Data:  filepath.Join(appSupport, appName),
		Cache: filepath.Join(caches, appName),
		Logs:  filepath.Join(logs, appName),
	}
}
