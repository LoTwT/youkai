//go:build windows

package app

import (
	"errors"
	"fmt"

	"golang.org/x/sys/windows"
)

// lockFile is an open, exclusively locked file. The LockFileEx byte-range
// lock is bound to this handle, so the kernel releases it when the
// process exits or crashes, and a second handle — even in the same
// process — conflicts with it as much as one in another process does.
type lockFile struct {
	h windows.Handle
}

// acquireLockFile opens path, creating an empty file if needed, and
// takes an exclusive, fail-immediately LockFileEx lock on its first
// byte. The file is opened with read/write sharing so another youkai
// process can open it and observe the lock, but deliberately without
// FILE_SHARE_DELETE: nothing may delete or rename the lock file out from
// under a holder. ERROR_LOCK_VIOLATION means another process holds the
// lock; any other outcome is an error and holds nothing, so an
// indeterminate result never reads as success. Paths come from the
// per-user roots and stay well below MAX_PATH.
func acquireLockFile(path string) (lockFile, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return lockFile{}, err
	}
	h, err := windows.CreateFile(p,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_ALWAYS,
		windows.FILE_ATTRIBUTE_NORMAL,
		0)
	if err != nil {
		return lockFile{}, fmt.Errorf("open %q: %w", path, err)
	}
	var overlapped windows.Overlapped
	err = windows.LockFileEx(h,
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, 1, 0, &overlapped)
	if err != nil {
		windows.CloseHandle(h)
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return lockFile{}, ErrLockHeld
		}
		return lockFile{}, fmt.Errorf("lock %q: %w", path, err)
	}
	return lockFile{h: h}, nil
}

// release drops the byte-range lock and closes the handle. Closing alone
// would also release the lock; the explicit unlock keeps the failure of
// either step observable.
func (l lockFile) release() error {
	if l.h == 0 || l.h == windows.InvalidHandle {
		return nil
	}
	var overlapped windows.Overlapped
	unlockErr := windows.UnlockFileEx(l.h, 0, 1, 0, &overlapped)
	closeErr := windows.CloseHandle(l.h)
	return errors.Join(unlockErr, closeErr)
}
