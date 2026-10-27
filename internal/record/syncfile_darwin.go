//go:build darwin

package record

import (
	"errors"
	"os"
	"syscall"
)

// syncFileData delivers the file contents to the storage device with
// fsync(2). It deliberately avoids os.File.Sync on darwin: Go implements
// that as fcntl(F_FULLFSYNC), which also flushes the drive's write cache on
// every call. A batch pays for one such barrier in syncDir instead. The
// Apple fsync(2) manual page records the distinction: fsync moves data to
// the drive, while F_FULLFSYNC asks the drive to flush its buffered data.
func syncFileData(path string) error {
	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	return errors.Join(fsync(file), file.Close())
}

// fsync retries EINTR, matching os.File.Sync's handling of interrupted
// flushes.
func fsync(file *os.File) error {
	for {
		err := syscall.Fsync(int(file.Fd()))
		if !errors.Is(err, syscall.EINTR) {
			return err
		}
	}
}
