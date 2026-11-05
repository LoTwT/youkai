//go:build windows

package main

import (
	"errors"
	"syscall"

	"golang.org/x/sys/windows"
)

// isDiskFull reports an exhausted volume. Windows maps a full volume to
// ERROR_DISK_FULL, not to the invented syscall.ENOSPC that package os
// expects, so both are accepted.
func isDiskFull(err error) bool {
	return errors.Is(err, syscall.ENOSPC) || errors.Is(err, windows.ERROR_DISK_FULL)
}

// isSharingViolation reports a lock file another program opened without
// read/write sharing: youkai shares the file between its own builds, so a
// refusal to open it means a foreign program holds it.
func isSharingViolation(err error) bool {
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION)
}
