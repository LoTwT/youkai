//go:build windows

package main

import (
	"testing"

	"golang.org/x/sys/windows"
)

// Windows reports a full volume and a lock file held without sharing with
// codes that have no Unix equivalent; both must still select their own
// reason instead of the generic text.
func TestDataRootFailureReasonWindowsCodes(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "disk full", err: windows.ERROR_DISK_FULL, want: "磁盘空间不足"},
		{name: "sharing violation", err: windows.ERROR_SHARING_VIOLATION, want: "锁文件被其他程序占用"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := dataRootFailureReason(tt.err); got != tt.want {
				t.Fatalf("dataRootFailureReason(%v) = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
}
