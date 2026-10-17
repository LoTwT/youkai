package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// ErrLockHeld reports that another process already holds the occupancy
// the caller asked for. Callers must refuse the operation on this error:
// neither the lock file's existence nor any timeout is a license to
// preempt, the holder releases through process exit alone, and this
// package never deletes a lock file another process may hold.
var ErrLockHeld = errors.New("lock held by another process")

// dataRootLockFileName names the data-root occupancy's lock file inside
// the data root itself. Placing it inside the directory is what folds
// every path alias of the same actual directory — a symlink, a `..`
// traversal, a case variant on a case-insensitive filesystem — into one
// occupancy domain: all of them resolve to this same file.
const dataRootLockFileName = "youkai.lock"

// The scan occupancy's lock lives under a directory named after youkai's
// base application identifier, placed in the per-user application-data
// base directory of the OS: ~/Library/Application Support on macOS,
// %LOCALAPPDATA% on Windows. The identifier is the fixed base form —
// without the .dev suffix the development build adds — and deliberately
// not the display name the data roots are keyed on, so two properties
// hold at once: development ("Youkai Dev") and installed ("Youkai")
// builds of one OS user contend on the same lock file, and the directory
// name never collides with the app data roots even on case-insensitive
// filesystems. Staying in the per-user application-data base also keeps
// the lock out of the caches and temporary locations, whose routine
// clearing could delete a held lock file and silently split the
// occupancy domain, and out of the user's home directory, where a
// hidden directory is not the convention on Windows.
const (
	// baseAppIdentifier is youkai's fixed base application identifier —
	// the "identifier" of mygo.json, without the .dev suffix the
	// development build adds. The scan lock directory and the NSIS
	// uninstall entry (uninstall_windows.go) are named after it.
	baseAppIdentifier = "com.lotwt.youkai"
	scanLockFileName  = "scan.lock"
)

// DataRootLock is this process's exclusive occupancy of a data root. It
// is taken once during startup, before any settings, checkpoint, history
// or cleanup touches the data root, and held until Close or the end of
// the process; after a crash the OS primitive releases it.
type DataRootLock struct {
	mu       sync.Mutex
	file     lockFile
	dir      string
	released bool
}

// AcquireDataRootLock takes the exclusive occupancy of dataDir for the
// calling process, creating dataDir and the lock file if needed. Every
// other process — including other youkai builds — is refused on the same
// actual directory under any path alias. A failed or indeterminate
// acquire returns an error and holds nothing; the caller must refuse to
// operate on the data root.
func AcquireDataRootLock(dataDir string) (*DataRootLock, error) {
	if dataDir == "" || !filepath.IsAbs(dataDir) {
		return nil, fmt.Errorf("app: acquire data-root lock: data directory %q is not an absolute path", dataDir)
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("app: acquire data-root lock on %q: create data root: %w", dataDir, err)
	}
	file, err := acquireLockFile(filepath.Join(dataDir, dataRootLockFileName))
	if err != nil {
		return nil, fmt.Errorf("app: acquire data-root lock on %q: %w", dataDir, err)
	}
	return &DataRootLock{file: file, dir: dataDir}, nil
}

// AcquireScanLock takes the scan occupancy: the right to be the single
// youkai process of this OS user that operates the game. It is available
// only on a held DataRootLock because the architecture fixes the order —
// data-root occupancy first, scan occupancy second — so a process that
// cannot even own its data root never reaches the game. The method form
// makes that order explicit at the API level.
//
// The scan lock file location is per-user and shared by all builds; see
// the baseAppIdentifier constants for why. It is taken during scan
// preparation and released when the scan wraps up; wiring that flow is
// the application service's job.
func (l *DataRootLock) AcquireScanLock() (*ScanLock, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.released {
		return nil, errors.New("app: acquire scan lock: data-root lock already released")
	}
	lockPath, err := scanLockPath()
	if err != nil {
		return nil, fmt.Errorf("app: acquire scan lock: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		return nil, fmt.Errorf("app: acquire scan lock on %q: create lock directory: %w", lockPath, err)
	}
	file, err := acquireLockFile(lockPath)
	if err != nil {
		return nil, fmt.Errorf("app: acquire scan lock on %q: %w", lockPath, err)
	}
	return &ScanLock{file: file}, nil
}

// Close releases the data-root occupancy. It is safe to call more than
// once; after it returns, the data root may be taken over by another
// process, so the caller must have finished all of its own file
// operations under the root first.
func (l *DataRootLock) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.released {
		return nil
	}
	l.released = true
	err := l.file.release()
	l.file = lockFile{}
	if err != nil {
		return fmt.Errorf("app: release data-root lock on %q: %w", l.dir, err)
	}
	return nil
}

// ScanLock is this process's occupancy of the single per-user scan slot:
// while it is held, no other youkai process of the same OS user —
// including a differently named build on another data root — may operate
// the game. It is released on Close or by the OS after a crash.
type ScanLock struct {
	mu       sync.Mutex
	file     lockFile
	released bool
}

// Close releases the scan occupancy. It is safe to call more than once.
func (s *ScanLock) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.released {
		return nil
	}
	s.released = true
	err := s.file.release()
	s.file = lockFile{}
	if err != nil {
		return fmt.Errorf("app: release scan lock: %w", err)
	}
	return nil
}

// scanLockBase resolves the per-user application-data base directory
// the scan lock file lives under. It is a variable solely so the
// cross-process helper tests can point their child processes at a
// temporary base through the helper environment — the OS lookups
// (UserConfigDir, the Known Folder FOLDERID_LocalAppData) cannot be
// redirected per process. Production code always runs the platform
// default.
var scanLockBase = userAppDataBase

// scanLockPath resolves the scan occupancy's lock file under the current
// user's application-data base; see the constants above for why that
// anchor is independent of the application name and the data root, and
// shared by every build of one user.
func scanLockPath() (string, error) {
	base, err := scanLockBase()
	if err != nil {
		return "", fmt.Errorf("resolve per-user application data base: %w", err)
	}
	return filepath.Join(base, baseAppIdentifier, scanLockFileName), nil
}
