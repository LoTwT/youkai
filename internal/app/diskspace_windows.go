//go:build windows

package app

import (
	"path/filepath"

	"golang.org/x/sys/windows"
)

// diskSpace reports the free and total bytes of the volume holding path.
// It walks up to the nearest existing ancestor, because the data root
// itself is created by whoever writes first. The second result is false
// when no ancestor can be queried; the interface then says the disk space
// is unavailable instead of showing zero as exact.
func diskSpace(path string) (free, total int64, ok bool) {
	for path != "" {
		ptr, err := windows.UTF16PtrFromString(path)
		if err == nil {
			var freeBytes, totalBytes, totalFree uint64
			if err := windows.GetDiskFreeSpaceEx(ptr, &freeBytes, &totalBytes, &totalFree); err == nil {
				return int64(freeBytes), int64(totalBytes), true
			}
		}
		parent := filepath.Dir(path)
		if parent == path {
			return 0, 0, false
		}
		path = parent
	}
	return 0, 0, false
}
