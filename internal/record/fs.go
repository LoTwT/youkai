package record

import (
	"io"
	"io/fs"
	"os"
)

// FS is the filesystem boundary the store works through. It covers only
// the operations the store actually performs, so tests can inject a
// failure at each of them; the production implementation goes through the
// os package. The store is the only writer below the data root, and every
// path it builds comes from a validated root plus validated identifiers,
// so implementations need no path sandboxing of their own.
type FS interface {
	// MkdirAll creates dir and its missing parents, like os.MkdirAll.
	MkdirAll(dir string, perm fs.FileMode) error
	// Stat reports the file info of path, like os.Stat.
	Stat(path string) (fs.FileInfo, error)
	// Lstat is Stat without following a final symbolic link, like
	// os.Lstat; deletion uses it to refuse a record directory that is a
	// link instead of removing its target.
	Lstat(path string) (fs.FileInfo, error)
	// ReadDir lists dir, like os.ReadDir.
	ReadDir(dir string) ([]fs.DirEntry, error)
	// OpenFile opens path with os flag semantics, like os.OpenFile.
	// Reads use os.O_RDONLY; writes use os.O_WRONLY|os.O_CREATE, with
	// os.O_EXCL where an existing file must not be touched.
	OpenFile(path string, flag int, perm fs.FileMode) (File, error)
	// Remove deletes path, like os.Remove.
	Remove(path string) error
	// RemoveAll deletes path and everything below it, like os.RemoveAll.
	// It removes symbolic links themselves instead of following them.
	RemoveAll(path string) error
}

// File is the handle the store reads and writes through. Besides the io
// interfaces it carries the durability steps a save must complete before
// it may report success: Sync pushes the written bytes to the storage
// device, Close releases the handle, and an error from either one fails
// the save that is in flight.
type File interface {
	io.Reader
	io.Writer
	Sync() error
	Close() error
}

// osFS implements FS with the os package.
type osFS struct{}

func (osFS) MkdirAll(dir string, perm fs.FileMode) error { return os.MkdirAll(dir, perm) }

func (osFS) Stat(path string) (fs.FileInfo, error) { return os.Stat(path) }

func (osFS) Lstat(path string) (fs.FileInfo, error) { return os.Lstat(path) }

func (osFS) ReadDir(dir string) ([]fs.DirEntry, error) { return os.ReadDir(dir) }

func (osFS) OpenFile(path string, flag int, perm fs.FileMode) (File, error) {
	return os.OpenFile(path, flag, perm)
}

func (osFS) Remove(path string) error { return os.Remove(path) }

func (osFS) RemoveAll(path string) error { return os.RemoveAll(path) }
