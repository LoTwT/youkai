//go:build darwin

package record

import (
	"errors"
	"os"
	"syscall"
)

// syncDir flushes the directory entries and then acts as the batch's single
// device-cache barrier. A directory fsync first moves the new entries to the
// drive; one fcntl(F_FULLFSYNC) then asks the drive to flush everything
// buffered so far, including the file contents delivered by the fsync calls
// before it. The Apple fsync(2) manual page recommends F_FULLFSYNC for
// strict ordering because plain fsync does not flush the drive cache. If the
// filesystem rejects F_FULLFSYNC (ENOTSUP, as on some SMB mounts), the
// completed fsync is the best available guarantee, matching os.File.Sync's
// fallback.
func syncDir(dir string) error {
	file, err := os.Open(dir)
	if err != nil {
		return err
	}
	if err := fsync(file); err != nil {
		_ = file.Close()
		return err
	}
	return errors.Join(fullSync(file), file.Close())
}

func fullSync(file *os.File) error {
	for {
		_, _, errno := syscall.Syscall(syscall.SYS_FCNTL, file.Fd(), uintptr(syscall.F_FULLFSYNC), 0)
		switch errno {
		case 0:
			return nil
		case syscall.EINTR:
			continue
		case syscall.ENOTSUP:
			// fsync already ran; there is no stronger barrier available.
			return nil
		default:
			return errno
		}
	}
}
