package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/ui"
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

// SaveDirect implements ui.DirectExportSaver: the page shell writes into
// the system downloads directory without a save dialog. An existing name is
// never replaced; the writer picks the next numbered name instead.
func (s fileExportSaver) SaveDirect(format app.ExportFormat, defaultName string, data []byte) (string, error) {
	dir, err := mygo.App.Path(mygo.PathDownloads)
	if err != nil {
		return "", &ui.ExportDirectoryError{Err: err}
	}
	if dir == "" {
		return "", &ui.ExportDirectoryError{Err: errors.New("系统未提供下载目录")}
	}
	path, err := writeDirectExport(dir, defaultName, data)
	if err != nil {
		return "", err
	}
	return path, nil
}

// directExportNameAttempts bounds the numbered-name search. Reaching it
// means the directory holds an implausible number of same-named files.
const directExportNameAttempts = 1000

// writeDirectExport writes data under dir without overwriting an existing
// file. It reserves the first free name with an exclusive create, then
// atomically replaces that reservation with the synced file. A failed write
// removes the reservation and leaves every existing export untouched.
func writeDirectExport(dir, defaultName string, data []byte) (string, error) {
	path, err := reserveDirectExportPath(dir, defaultName)
	if err != nil {
		return "", err
	}
	if err := writeExportFile(path, data); err != nil {
		_ = os.Remove(path)
		if errors.Is(err, os.ErrPermission) || errors.Is(err, os.ErrNotExist) {
			return "", &ui.ExportDirectoryError{Directory: dir, Err: err}
		}
		return "", fmt.Errorf("写入 %q：%w", path, err)
	}
	return path, nil
}

// reserveDirectExportPath claims the first free numbered name. The
// exclusive create closes the check-then-write race, so two exports never
// choose the same file.
func reserveDirectExportPath(dir, defaultName string) (string, error) {
	name := filepath.Base(defaultName)
	if name == "" || name == "." || name == string(filepath.Separator) {
		return "", &ui.ExportDirectoryError{Directory: dir, Err: errors.New("导出文件名无效")}
	}
	for attempt := 0; attempt < directExportNameAttempts; attempt++ {
		candidate := filepath.Join(dir, numberedExportName(name, attempt))
		file, err := os.OpenFile(candidate, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", &ui.ExportDirectoryError{Directory: dir, Err: err}
		}
		if err := file.Close(); err != nil {
			_ = os.Remove(candidate)
			return "", &ui.ExportDirectoryError{Directory: dir, Err: err}
		}
		return candidate, nil
	}
	return "", &ui.ExportDirectoryError{
		Directory: dir,
		Err:       errors.New("同名导出文件过多，无法选择新文件名"),
	}
}

// numberedExportName inserts the conflict counter before the extension:
// name.youkai.json, name.youkai-1.json, name.youkai-2.json.
func numberedExportName(name string, attempt int) string {
	if attempt == 0 {
		return name
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	if stem == "" {
		// A leading-dot name is a file name, not an extension.
		stem, ext = name, ""
	}
	return fmt.Sprintf("%s-%d%s", stem, attempt, ext)
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
