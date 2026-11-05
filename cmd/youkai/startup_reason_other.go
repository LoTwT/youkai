//go:build !windows

package main

import (
	"errors"
	"syscall"
)

// isDiskFull reports an exhausted volume. Unix returns ENOSPC; Windows has
// a separate code and its own implementation.
func isDiskFull(err error) bool {
	return errors.Is(err, syscall.ENOSPC)
}

// isSharingViolation is Windows-only: POSIX flock refuses a second holder
// with EWOULDBLOCK, which the app package already reports as ErrLockHeld.
func isSharingViolation(error) bool {
	return false
}
