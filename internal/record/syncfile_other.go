//go:build !darwin

package record

import (
	"errors"
	"os"
)

// syncFileData flushes the file contents with the platform's ordinary
// durability operation. These platforms do not have darwin's separate
// device-cache flush, so File.Sync is the right primitive.
func syncFileData(path string) error {
	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	return errors.Join(file.Sync(), file.Close())
}
