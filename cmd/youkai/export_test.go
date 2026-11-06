package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/LoTwT/youkai/internal/ui"
)

type failingExportFile struct {
	name     string
	data     []byte
	writeErr error
	writeN   int
	syncErr  error
	closeErr error
	short    bool
	closed   bool
}

func (f *failingExportFile) Name() string { return f.name }

func (f *failingExportFile) Write(p []byte) (int, error) {
	if f.writeErr != nil {
		n := f.writeN
		if n > len(p) {
			n = len(p)
		}
		f.data = append(f.data, p[:n]...)
		return n, f.writeErr
	}
	if f.short {
		n := len(p) / 2
		f.data = append(f.data, p[:n]...)
		return n, nil
	}
	f.data = append(f.data, p...)
	return len(p), nil
}

func (f *failingExportFile) Sync() error {
	if f.syncErr != nil {
		return f.syncErr
	}
	return nil
}

func (f *failingExportFile) Close() error {
	f.closed = true
	return f.closeErr
}

func TestWriteExportFileFailuresPreserveTarget(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.youkai.json")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	closeErr := errors.New("close failed")
	chmodErr := errors.New("chmod failed")
	renameErr := errors.New("rename failed")
	cases := []struct {
		name      string
		file      *failingExportFile
		chmodErr  error
		renameErr error
		wantErr   error
	}{
		{name: "write ENOSPC", file: &failingExportFile{writeErr: syscall.ENOSPC}, wantErr: syscall.ENOSPC},
		{name: "partial write ENOSPC", file: &failingExportFile{writeErr: syscall.ENOSPC, writeN: 2}, wantErr: syscall.ENOSPC},
		{name: "short write", file: &failingExportFile{short: true}, wantErr: io.ErrShortWrite},
		{name: "sync ENOSPC", file: &failingExportFile{syncErr: syscall.ENOSPC}, wantErr: syscall.ENOSPC},
		{name: "close error", file: &failingExportFile{closeErr: closeErr}, wantErr: closeErr},
		{name: "chmod error", file: &failingExportFile{}, chmodErr: chmodErr, wantErr: chmodErr},
		{name: "rename error", file: &failingExportFile{}, renameErr: renameErr, wantErr: renameErr},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.file.name = filepath.Join(dir, ".out.youkai.json.tmp-test")
			removed := ""
			renamed := false
			ops := exportFileOps{
				createTemp: func(string, string) (exportFile, error) { return tc.file, nil },
				chmod: func(string, os.FileMode) error {
					return tc.chmodErr
				},
				rename: func(string, string) error {
					renamed = true
					return tc.renameErr
				},
				remove: func(name string) error {
					removed = name
					return nil
				},
			}
			err := writeExportFileWith(path, []byte("new"), ops)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want %v", err, tc.wantErr)
			}
			if !tc.file.closed {
				t.Fatal("the temporary file was not closed")
			}
			if removed != tc.file.name {
				t.Fatalf("removed %q, want the temporary file %q", removed, tc.file.name)
			}
			if renamed != (tc.renameErr != nil) {
				t.Fatalf("rename called = %t, want %t", renamed, tc.renameErr != nil)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != "old" {
				t.Fatalf("a failed write changed the existing export: %q", got)
			}
		})
	}
}

func TestWriteExportFileReportsResidualTempFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.youkai.json")
	tmpName := filepath.Join(dir, ".out.youkai.json.tmp-test")
	file := &failingExportFile{name: tmpName, writeErr: syscall.ENOSPC}
	removeErr := errors.New("temporary file is busy")
	ops := exportFileOps{
		createTemp: func(string, string) (exportFile, error) { return file, nil },
		chmod:      func(string, os.FileMode) error { return nil },
		rename:     func(string, string) error { return nil },
		remove:     func(string) error { return removeErr },
	}
	err := writeExportFileWith(path, []byte("new"), ops)
	if !errors.Is(err, syscall.ENOSPC) {
		t.Fatalf("error = %v, want ENOSPC", err)
	}
	if !strings.Contains(err.Error(), filepath.Base(tmpName)) || !strings.Contains(err.Error(), removeErr.Error()) {
		t.Fatalf("error %q does not report the residual temporary file", err)
	}
}

func TestWriteExportFileCreateFailureLeavesTarget(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.youkai.json")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	want := syscall.ENOSPC
	ops := exportFileOps{
		createTemp: func(string, string) (exportFile, error) { return nil, want },
		chmod:      os.Chmod,
		rename:     os.Rename,
		remove:     os.Remove,
	}
	if err := writeExportFileWith(path, []byte("new"), ops); !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old" {
		t.Fatalf("a failed create changed the existing export: %q", got)
	}
}

func TestWriteDirectExportNumbersExistingNames(t *testing.T) {
	dir := t.TempDir()
	const name = "youkai-2026-10-22-abc.youkai.csv"
	first, err := writeDirectExport(dir, name, []byte("first"))
	if err != nil {
		t.Fatalf("first direct export: %v", err)
	}
	if first != filepath.Join(dir, name) {
		t.Fatalf("first path = %q", first)
	}
	second, err := writeDirectExport(dir, name, []byte("second"))
	if err != nil {
		t.Fatalf("second direct export: %v", err)
	}
	if want := filepath.Join(dir, "youkai-2026-10-22-abc.youkai-1.csv"); second != want {
		t.Fatalf("second path = %q, want %q", second, want)
	}
	third, err := writeDirectExport(dir, name, []byte("third"))
	if err != nil {
		t.Fatalf("third direct export: %v", err)
	}
	if want := filepath.Join(dir, "youkai-2026-10-22-abc.youkai-2.csv"); third != want {
		t.Fatalf("third path = %q, want %q", third, want)
	}
	for path, want := range map[string]string{first: "first", second: "second", third: "third"} {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Fatalf("%s = %q, want %q", filepath.Base(path), got, want)
		}
	}
}

func TestWriteDirectExportReportsUnusableDirectory(t *testing.T) {
	// A regular file cannot act as the export directory, which makes the
	// failure deterministic on every platform.
	file := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	path, err := writeDirectExport(file, "out.youkai.csv", []byte("data"))
	if err == nil || path != "" {
		t.Fatalf("unusable directory returned path %q err %v", path, err)
	}
	var dirErr *ui.ExportDirectoryError
	if !errors.As(err, &dirErr) {
		t.Fatalf("error %T is not an ExportDirectoryError: %v", err, err)
	}
	if dirErr.Directory != file {
		t.Fatalf("directory = %q, want %q", dirErr.Directory, file)
	}
}

func TestWriteDirectExportContextCanceledWritesNothing(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	path, err := writeDirectExportContext(ctx, dir, "out.youkai.json", []byte("data"))
	if path != "" || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled export returned path %q err %v", path, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("a canceled export left %d files", len(entries))
	}
}

// cancelAfterChecksContext reports success for the first checks and
// cancellation on cancelAt: it tests the cleanup between reserving a name
// and writing the file deterministically.
type cancelAfterChecksContext struct {
	context.Context
	checks   int
	cancelAt int
}

func (c *cancelAfterChecksContext) Err() error {
	c.checks++
	if c.checks >= c.cancelAt {
		return context.Canceled
	}
	return nil
}

func TestWriteDirectExportContextCancelAfterReserveRemovesName(t *testing.T) {
	dir := t.TempDir()
	ctx := &cancelAfterChecksContext{Context: context.Background(), cancelAt: 2}
	path, err := writeDirectExportContext(ctx, dir, "out.youkai.json", []byte("data"))
	if path != "" || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled export returned path %q err %v", path, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("a canceled export kept its reserved name: %v", entries)
	}
}

func TestWriteDirectExportContextCancelDuringWriteLeavesNothing(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	ops := defaultExportFileOps
	ops.createTemp = func(dir, pattern string) (exportFile, error) {
		f, err := os.CreateTemp(dir, pattern)
		if err != nil {
			return nil, err
		}
		cancel()
		return f, nil
	}
	path, err := writeDirectExportContextWith(ctx, dir, "out.youkai.json", []byte("data"), ops)
	if path != "" || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled export returned path %q err %v", path, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("a canceled export left files: %v", entries)
	}
}

func TestWriteDirectExportContextCancelKeepsCleanupError(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	ops := defaultExportFileOps
	ops.createTemp = func(dir, pattern string) (exportFile, error) {
		f, err := os.CreateTemp(dir, pattern)
		if err != nil {
			return nil, err
		}
		cancel()
		return f, nil
	}
	ops.remove = func(string) error { return errors.New("temporary file locked") }
	path, err := writeDirectExportContextWith(ctx, dir, "out.youkai.json", []byte("data"), ops)
	if path != "" || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled export returned path %q err %v", path, err)
	}
	if !strings.Contains(err.Error(), "temporary file locked") ||
		strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("cleanup text = %q", err)
	}
	var cleanupErr *ui.ExportCleanupError
	if !errors.As(err, &cleanupErr) {
		t.Fatalf("the cleanup failure is not classified: %v", err)
	}
}

func TestWriteDirectExportContextReserveCleanupFailureIsClassified(t *testing.T) {
	dir := t.TempDir()
	ctx := &cancelAfterChecksContext{Context: context.Background(), cancelAt: 2}
	ops := defaultExportFileOps
	ops.remove = func(string) error { return errors.New("reservation locked") }
	path, err := writeDirectExportContextWith(ctx, dir, "out.youkai.json", []byte("data"), ops)
	if path != "" || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled export returned path %q err %v", path, err)
	}
	var cleanupErr *ui.ExportCleanupError
	if !errors.As(err, &cleanupErr) {
		t.Fatalf("the reservation cleanup failure is not classified: %v", err)
	}
	if strings.Contains(err.Error(), "context canceled") ||
		!strings.Contains(err.Error(), "reservation locked") {
		t.Fatalf("cleanup text = %q", err)
	}
}

func TestWriteDirectExportContextWriteCleanupFailureIsClassified(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	ops := defaultExportFileOps
	ops.createTemp = func(dir, pattern string) (exportFile, error) {
		f, err := os.CreateTemp(dir, pattern)
		if err != nil {
			return nil, err
		}
		cancel()
		return f, nil
	}
	ops.remove = func(name string) error {
		if filepath.Base(name) == "out.youkai.json" {
			return errors.New("reservation locked")
		}
		return os.Remove(name)
	}
	path, err := writeDirectExportContextWith(ctx, dir, "out.youkai.json", []byte("data"), ops)
	if path != "" || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled export returned path %q err %v", path, err)
	}
	var cleanupErr *ui.ExportCleanupError
	if !errors.As(err, &cleanupErr) {
		t.Fatalf("the reservation cleanup failure is not classified: %v", err)
	}
	if strings.Contains(err.Error(), "context canceled") ||
		!strings.Contains(err.Error(), "reservation locked") {
		t.Fatalf("cleanup text = %q", err)
	}
}

func TestWriteDirectExportContextReserveRemoveNotExist(t *testing.T) {
	dir := t.TempDir()
	ctx := &cancelAfterChecksContext{Context: context.Background(), cancelAt: 2}
	ops := defaultExportFileOps
	ops.remove = func(string) error {
		return &os.PathError{Op: "remove", Path: "x", Err: syscall.ENOENT}
	}
	path, err := writeDirectExportContextWith(ctx, dir, "out.youkai.json", []byte("data"), ops)
	if path != "" || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled export returned path %q err %v", path, err)
	}
	var cleanupErr *ui.ExportCleanupError
	if errors.As(err, &cleanupErr) {
		t.Fatalf("ENOENT was called a cleanup failure: %v", err)
	}
}

func TestWriteDirectExportContextPlainFailureReservationNotExist(t *testing.T) {
	dir := t.TempDir()
	ops := defaultExportFileOps
	ops.createTemp = func(dir, _ string) (exportFile, error) {
		return &failingExportFile{name: filepath.Join(dir, ".tmp-x"), writeErr: syscall.ENOSPC}, nil
	}
	ops.remove = func(name string) error {
		if filepath.Base(name) == "out.youkai.json" {
			return &os.PathError{Op: "remove", Path: name, Err: syscall.ENOENT}
		}
		return nil
	}
	path, err := writeDirectExportContextWith(context.Background(), dir, "out.youkai.json", []byte("data"), ops)
	if path != "" || !errors.Is(err, syscall.ENOSPC) {
		t.Fatalf("write failure returned path %q err %v", path, err)
	}
	var cleanupErr *ui.ExportCleanupError
	if errors.As(err, &cleanupErr) {
		t.Fatalf("an already removed reservation was called a cleanup failure: %v", err)
	}
}

func TestWriteDirectExportContextPlainFailureTempNotExist(t *testing.T) {
	dir := t.TempDir()
	ops := defaultExportFileOps
	ops.createTemp = func(dir, _ string) (exportFile, error) {
		return &failingExportFile{name: filepath.Join(dir, ".tmp-x"), writeErr: syscall.ENOSPC}, nil
	}
	ops.remove = func(name string) error {
		if strings.Contains(name, ".tmp-x") {
			return &os.PathError{Op: "remove", Path: name, Err: syscall.ENOENT}
		}
		return os.Remove(name)
	}
	path, err := writeDirectExportContextWith(context.Background(), dir, "out.youkai.json", []byte("data"), ops)
	if path != "" || !errors.Is(err, syscall.ENOSPC) {
		t.Fatalf("write failure returned path %q err %v", path, err)
	}
	var cleanupErr *ui.ExportCleanupError
	if errors.As(err, &cleanupErr) {
		t.Fatalf("an already removed temporary file was called a cleanup failure: %v", err)
	}
}

// cancelOnWriteFile cancels the export as its data write begins, so the
// cancellation and the write failure race.
type cancelOnWriteFile struct {
	*failingExportFile
	cancel context.CancelFunc
}

func (f *cancelOnWriteFile) Write(p []byte) (int, error) {
	f.cancel()
	return f.failingExportFile.Write(p)
}

func TestWriteDirectExportContextWriteFailureTempCleanupKeepsCause(t *testing.T) {
	dir := t.TempDir()
	ops := defaultExportFileOps
	ops.createTemp = func(dir, _ string) (exportFile, error) {
		return &failingExportFile{name: filepath.Join(dir, ".tmp-x"), writeErr: syscall.ENOSPC}, nil
	}
	ops.remove = func(name string) error {
		if strings.Contains(name, ".tmp-x") {
			return errors.New("busy")
		}
		return os.Remove(name)
	}
	_, err := writeDirectExportContextWith(context.Background(), dir, "out.youkai.json", []byte("data"), ops)
	if !errors.Is(err, syscall.ENOSPC) {
		t.Fatalf("write cause lost: %v", err)
	}
	if !strings.Contains(err.Error(), syscall.ENOSPC.Error()) || !strings.Contains(err.Error(), "busy") {
		t.Fatalf("visible text lost a cause: %q", err)
	}
	var cleanupErr *ui.ExportCleanupError
	if !errors.As(err, &cleanupErr) {
		t.Fatalf("the cleanup failure is not classified: %v", err)
	}
}

func TestWriteDirectExportContextWriteFailureReservationCleanupKeepsCause(t *testing.T) {
	dir := t.TempDir()
	ops := defaultExportFileOps
	ops.createTemp = func(dir, _ string) (exportFile, error) {
		return &failingExportFile{name: filepath.Join(dir, ".tmp-x"), writeErr: syscall.ENOSPC}, nil
	}
	ops.remove = func(name string) error {
		if filepath.Base(name) == "out.youkai.json" {
			return errors.New("locked")
		}
		return nil
	}
	_, err := writeDirectExportContextWith(context.Background(), dir, "out.youkai.json", []byte("data"), ops)
	if !strings.Contains(err.Error(), syscall.ENOSPC.Error()) || !strings.Contains(err.Error(), "locked") {
		t.Fatalf("visible text lost a cause: %q", err)
	}
	var cleanupErr *ui.ExportCleanupError
	if !errors.As(err, &cleanupErr) {
		t.Fatalf("the cleanup failure is not classified: %v", err)
	}
}

func TestWriteDirectExportContextCancelRacingWriteFailureLeaksNoEnglish(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ops := defaultExportFileOps
	ops.createTemp = func(dir, _ string) (exportFile, error) {
		return &cancelOnWriteFile{
			failingExportFile: &failingExportFile{name: filepath.Join(dir, ".tmp-x"), writeErr: syscall.ENOSPC},
			cancel:            cancel,
		}, nil
	}
	ops.remove = func(name string) error {
		if strings.Contains(name, ".tmp-x") {
			return errors.New("busy")
		}
		return os.Remove(name)
	}
	_, err := writeDirectExportContextWith(ctx, dir, "out.youkai.json", []byte("data"), ops)
	var cleanupErr *ui.ExportCleanupError
	if !errors.As(err, &cleanupErr) {
		t.Fatalf("the cleanup failure is not classified: %v", err)
	}
	if strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("the English cancellation text leaks: %q", err)
	}
	if !strings.Contains(err.Error(), syscall.ENOSPC.Error()) {
		t.Fatalf("the write cause is hidden: %q", err)
	}
}

func TestWriteDirectExportContextCancelReservationAlreadyGone(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	ops := defaultExportFileOps
	ops.createTemp = func(dir, pattern string) (exportFile, error) {
		f, err := os.CreateTemp(dir, pattern)
		if err != nil {
			return nil, err
		}
		if err := os.Remove(filepath.Join(dir, "out.youkai.json")); err != nil {
			t.Fatal(err)
		}
		cancel()
		return f, nil
	}
	path, err := writeDirectExportContextWith(ctx, dir, "out.youkai.json", []byte("data"), ops)
	if path != "" || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled export returned path %q err %v", path, err)
	}
	var cleanupErr *ui.ExportCleanupError
	if errors.As(err, &cleanupErr) {
		t.Fatalf("an already removed reservation was called a cleanup failure: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("a canceled export left files: %v", entries)
	}
}

func TestWriteExportFileCancelBeforeRenameLeavesNoTarget(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.youkai.json")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	canceled := errors.New("canceled")
	renamed := false
	removed := ""
	ops := exportFileOps{
		createTemp: func(string, string) (exportFile, error) {
			return &failingExportFile{name: filepath.Join(dir, ".tmp")}, nil
		},
		chmod: func(string, os.FileMode) error { return nil },
		rename: func(string, string) error {
			renamed = true
			return nil
		},
		remove: func(name string) error {
			removed = name
			return nil
		},
	}
	checks := 0
	err := writeExportFileWithCancel(path, []byte("new"), ops, func() error {
		checks++
		if checks >= 3 { // the check after close and chmod, before the rename
			return canceled
		}
		return nil
	})
	if !errors.Is(err, canceled) {
		t.Fatalf("error = %v, want canceled", err)
	}
	if renamed {
		t.Fatal("a canceled export was renamed into place")
	}
	if removed == "" {
		t.Fatal("a canceled export left its temporary file")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old" {
		t.Fatalf("a canceled export changed the existing file: %q", got)
	}
}

func TestNumberedExportNameKeepsTheExtension(t *testing.T) {
	cases := map[string]string{
		"out.youkai.json":   "out.youkai-1.json",
		"out.csv":           "out-1.csv",
		"noextension":       "noextension-1",
		".hidden":           ".hidden-1",
		"two.dots.youkai.x": "two.dots.youkai-1.x",
	}
	for name, want := range cases {
		if got := numberedExportName(name, 1); got != want {
			t.Fatalf("numberedExportName(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestWriteExportFileReplacesOnlyOnSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.youkai.json")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeExportFile(path, []byte("new")); err != nil {
		t.Fatalf("writeExportFile: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Fatalf("content = %q, want new", got)
	}
	// A missing directory fails before touching the previous file.
	missing := filepath.Join(dir, "missing", "out.youkai.json")
	if err := writeExportFile(missing, []byte("x")); err == nil {
		t.Fatal("writing into a missing directory succeeded")
	}
	got, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Fatalf("a failed write changed the previous file: %q", got)
	}
}
