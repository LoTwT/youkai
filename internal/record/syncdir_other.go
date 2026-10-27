//go:build !darwin && !windows

package record

import (
	"errors"
	"os"
)

// syncDir flushes the directory entries to stable storage. Unix exposes a
// directory as a readable handle whose fsync flushes the entries; the error
// is returned because losing a new entry would make a checkpoint that cites
// the file unrecoverable after a crash.
func syncDir(dir string) error {
	file, err := os.Open(dir)
	if err != nil {
		return err
	}
	return errors.Join(file.Sync(), file.Close())
}
