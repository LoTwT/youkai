//go:build windows

package record

// syncDir is a no-op on Windows: FlushFileBuffers targets file handles,
// not directory entries, and the platform exposes no portable directory
// flush. File contents are still synced before a checkpoint cites them;
// NTFS journals the metadata change that creates the entry.
func syncDir(dir string) error {
	return nil
}
