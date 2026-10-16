//go:build unix

package app

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// lockFile is an open, exclusively locked file. The flock(2) lock sits on
// the open file description, so it belongs to this descriptor alone: the
// kernel releases it when the process exits or crashes, and a second
// open in the same process conflicts with it as much as one in another
// process does. flock is chosen over fcntl record locks because POSIX
// locks are dropped when *any* descriptor of the file closes, a footgun
// for a lock held next to unrelated code that may open the same path.
type lockFile struct {
	f *os.File
}

// acquireLockFile opens path, creating an empty file if needed, and
// takes a non-blocking exclusive flock on it. EINTR is retried because a
// retry is idempotent; EWOULDBLOCK means another process holds the lock.
// Any other outcome is reported as an error and holds nothing, so an
// indeterminate result never reads as success.
func acquireLockFile(path string) (lockFile, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return lockFile{}, err
	}
	for {
		err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if !errors.Is(err, unix.EINTR) {
			break
		}
	}
	if err != nil {
		f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return lockFile{}, ErrLockHeld
		}
		return lockFile{}, fmt.Errorf("flock %q: %w", path, err)
	}
	return lockFile{f: f}, nil
}

// release drops the flock and closes the file. Closing alone would also
// release the lock; the explicit unlock keeps the failure of either step
// observable.
func (l lockFile) release() error {
	if l.f == nil {
		return nil
	}
	unlockErr := unix.Flock(int(l.f.Fd()), unix.LOCK_UN)
	closeErr := l.f.Close()
	return errors.Join(unlockErr, closeErr)
}
