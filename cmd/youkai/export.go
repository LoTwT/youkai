package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/egoist/mygo"
)

// fileExportSaver is the production ExportSaver: the system save dialog
// chooses the target, then the bytes are written to a temporary file in
// the same directory, synced, closed and renamed. A failed write therefore
// never truncates an existing export, and success is reported only after
// the final close succeeded. dataRoot is the application-managed root;
// exports inside it are refused because cleanup must never own them.
type fileExportSaver struct {
	dataRoot string
}

// Save implements ui.ExportSaver.
func (s fileExportSaver) Save(format app.ExportFormat, defaultName string, data []byte) (string, error) {
	title := "导出可用数据 JSON"
	filterName := "Youkai JSON"
	if format == app.ExportCSV {
		title = "导出可用数据 CSV"
		filterName = "Youkai CSV"
	}
	defaultPath := defaultName
	if dir, err := mygo.App.Path(mygo.PathDownloads); err == nil && dir != "" {
		defaultPath = filepath.Join(dir, defaultName)
	}
	path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
		Parent:      mygo.FocusedWindow(),
		Title:       title,
		DefaultPath: defaultPath,
		Filters: []mygo.FileFilter{{
			Name:       filterName,
			Extensions: []string{format.Extension()},
		}},
	})
	if err != nil {
		return "", fmt.Errorf("选择导出位置：%w", err)
	}
	if path == "" {
		return "", nil // cancelled: the internal record stays untouched
	}
	if err := refuseManagedTarget(s.dataRoot, path); err != nil {
		return "", err
	}
	if err := writeExportFile(path, data); err != nil {
		return "", fmt.Errorf("写入 %q：%w", path, err)
	}
	return path, nil
}

// refuseManagedTarget rejects a destination inside the application's own
// data root: exported files are independent copies and cleanup must never
// be able to delete them by accident.
func refuseManagedTarget(root, path string) error {
	if root == "" {
		return nil
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("解析数据目录：%w", err)
	}
	absRoot = resolveExisting(absRoot)
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("解析导出路径：%w", err)
	}
	// Resolve the existing part of the target so a symlink inside the data
	// root is still recognised as managed. The file itself, or even its
	// immediate directory, may not exist yet.
	absPath = resolveExisting(absPath)
	rel, err := filepath.Rel(absRoot, absPath)
	if err != nil {
		return nil // different volumes cannot be inside the data root
	}
	if rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))) {
		return errors.New("导出位置不能位于应用管理的数据目录内，请选择其他目录")
	}
	return nil
}

// resolveExisting resolves every symlink in the deepest existing ancestor
// of path and reattaches the components that do not exist yet. A path
// whose parent is already created and a nested future path therefore
// compare against the same real root.
func resolveExisting(path string) string {
	cur := path
	rest := ""
	for {
		if resolved, err := filepath.EvalSymlinks(cur); err == nil {
			if rest == "" {
				return resolved
			}
			return filepath.Join(resolved, rest)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return path
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}

// exportFile is the slice of *os.File the atomic export writer uses. It is
// an interface so tests can inject ENOSPC and delayed write errors without
// needing a real full volume.
type exportFile interface {
	io.Writer
	Name() string
	Sync() error
	Close() error
}

// exportFileOps are the filesystem operations the atomic export writer
// needs. Production uses os; tests replace individual steps to exercise
// failure paths deterministically.
type exportFileOps struct {
	createTemp func(dir, pattern string) (exportFile, error)
	chmod      func(name string, mode os.FileMode) error
	rename     func(oldpath, newpath string) error
	remove     func(name string) error
}

var defaultExportFileOps = exportFileOps{
	createTemp: func(dir, pattern string) (exportFile, error) {
		return os.CreateTemp(dir, pattern)
	},
	chmod:  os.Chmod,
	rename: os.Rename,
	remove: os.Remove,
}

// writeExportFile writes data beside the target, syncs and closes it, then
// renames it into place. An error removes the temporary file and leaves
// any existing target unchanged.
func writeExportFile(path string, data []byte) error {
	return writeExportFileWith(path, data, defaultExportFileOps)
}

func writeExportFileWith(path string, data []byte, ops exportFileOps) (err error) {
	dir := filepath.Dir(path)
	tmp, err := ops.createTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		if err == nil {
			return
		}
		if rmErr := ops.remove(tmpName); rmErr != nil {
			err = fmt.Errorf("%w; 临时文件 %q 未删除: %v", err, tmpName, rmErr)
		}
	}()
	n, err := tmp.Write(data)
	if err != nil {
		_ = tmp.Close()
		return err
	}
	if n != len(data) {
		_ = tmp.Close()
		return io.ErrShortWrite
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	// CreateTemp uses 0600; exports are ordinary user files.
	if err = ops.chmod(tmpName, 0o644); err != nil {
		return err
	}
	if err = ops.rename(tmpName, path); err != nil {
		return err
	}
	return nil
}
