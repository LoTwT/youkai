package main

import (
	"context"
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

// fileExportSaver is the production DirectExportSaver: the page shell writes
// into the system downloads directory without a save dialog.
type fileExportSaver struct{}

// SaveDirect implements ui.DirectExportSaver. An existing name is never
// replaced; the writer picks the next numbered name instead.
func (s fileExportSaver) SaveDirect(format app.ExportFormat, defaultName string, data []byte) (string, error) {
	return s.SaveDirectContext(context.Background(), format, defaultName, data)
}

// SaveDirectContext implements ui.CancelableDirectExportSaver: a canceled
// export leaves neither the reserved name nor a temporary file behind.
func (s fileExportSaver) SaveDirectContext(ctx context.Context, format app.ExportFormat, defaultName string, data []byte) (string, error) {
	dir, err := mygo.App.Path(mygo.PathDownloads)
	if err != nil {
		return "", &ui.ExportDirectoryError{Err: err}
	}
	if dir == "" {
		return "", &ui.ExportDirectoryError{Err: errors.New("系统未提供下载目录")}
	}
	return writeDirectExportContext(ctx, dir, defaultName, data)
}

// directExportNameAttempts bounds the numbered-name search. Reaching it
// means the directory holds an implausible number of same-named files.
const directExportNameAttempts = 1000

// writeDirectExport writes data under dir without overwriting an existing
// file. It reserves the first free name with an exclusive create, then
// atomically replaces that reservation with the synced file. A failed write
// removes the reservation and leaves every existing export untouched.
func writeDirectExport(dir, defaultName string, data []byte) (string, error) {
	return writeDirectExportContext(context.Background(), dir, defaultName, data)
}

// writeDirectExportContext is writeDirectExport with the export dialog's
// cancellation. A canceled save removes its reservation and temporary
// file, so the dialog's "已取消，没有写入文件" is true. If cleanup fails,
// ExportCleanupError keeps the dialog from claiming it was not written.
func writeDirectExportContext(ctx context.Context, dir, defaultName string, data []byte) (string, error) {
	return writeDirectExportContextWith(ctx, dir, defaultName, data, defaultExportFileOps)
}

// writeDirectExportContextWith is writeDirectExportContext with injectable
// file operations, so a test can cancel after the name is reserved and
// while the temporary file is written.
func writeDirectExportContextWith(ctx context.Context, dir, defaultName string, data []byte, ops exportFileOps) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	path, err := reserveDirectExportPath(dir, defaultName)
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		if removeErr := ops.remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return "", cleanupExportError(err,
				fmt.Sprintf("预留文件 %q 未删除: %v", path, removeErr))
		}
		return "", err
	}
	if err := writeExportFileWithCancel(path, data, ops, ctx.Err); err != nil {
		removeErr := ops.remove(path)
		if ctxErr := ctx.Err(); ctxErr != nil {
			// Preserve any cleanup detail the writer added: the dialog's
			// cancellation promise is only true if no file remains. Return
			// that cleanup error itself so the cancellation text never
			// reaches the user; a real write cause stays beside it.
			var cleanupErr *ui.ExportCleanupError
			if errors.As(err, &cleanupErr) {
				if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
					err = cleanupExportError(err,
						fmt.Sprintf("预留文件 %q 未删除: %v", path, removeErr))
				}
				return "", err
			}
			if err == ctxErr {
				err = nil
			} else if !errors.Is(err, ctxErr) {
				err = errors.Join(ctxErr, err)
			}
			if err == nil {
				err = ctxErr
			}
			if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				err = cleanupExportError(err,
					fmt.Sprintf("预留文件 %q 未删除: %v", path, removeErr))
			}
			return "", err
		}
		if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			err = cleanupExportError(err,
				fmt.Sprintf("预留文件 %q 未删除: %v", path, removeErr))
		}
		if errors.Is(err, os.ErrPermission) || errors.Is(err, os.ErrNotExist) {
			return "", &ui.ExportDirectoryError{Directory: dir, Err: err}
		}
		return "", fmt.Errorf("写入 %q：%w", path, err)
	}
	return path, nil
}

// cleanupExportError adds a user-facing cleanup detail without exposing
// the cancellation error text. A cleanup error the writer already reported
// keeps its detail and remains the unwrapped cause.
func cleanupExportError(err error, detail string) *ui.ExportCleanupError {
	var existing *ui.ExportCleanupError
	if errors.As(err, &existing) {
		if existing.Detail != "" {
			detail = existing.Detail + "\n" + detail
		}
		err = existing.Err
	}
	return &ui.ExportCleanupError{Detail: detail, Err: err}
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

func writeExportFileWith(path string, data []byte, ops exportFileOps) error {
	return writeExportFileWithCancel(path, data, ops, nil)
}

// writeExportFileWithCancel is writeExportFile with cancellation checks.
// The check before the rename is the last point where canceling can still
// promise that no file appeared.
func writeExportFileWithCancel(path string, data []byte, ops exportFileOps, canceled func() error) (err error) {
	if canceled != nil {
		if err := canceled(); err != nil {
			return err
		}
	}
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
		if rmErr := ops.remove(tmpName); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
			err = &ui.ExportCleanupError{
				Detail: fmt.Sprintf("临时文件 %q 未删除: %v", tmpName, rmErr),
				Err:    err,
			}
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
	if canceled != nil {
		if err := canceled(); err != nil {
			_ = tmp.Close()
			return err
		}
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
	if canceled != nil {
		if err := canceled(); err != nil {
			return err
		}
	}
	if err = ops.rename(tmpName, path); err != nil {
		return err
	}
	return nil
}
