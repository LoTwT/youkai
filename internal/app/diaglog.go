package app

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"
)

// diagnosticLogFileName is the active diagnostic log file under
// Paths.Logs; its numbered backups youkai.log.N (1 the most recent) hold
// what rotation retired. One well-known active file keeps "send us your
// log" answerable with a single path, which matters for installed users
// above all: the Windows EXE runs on the GUI subsystem and its stderr is
// invisible without a console redirection (the M1 Windows test report's
// observation of missing on-disk logs).
const diagnosticLogFileName = "youkai.log"

// Rotation limits of the diagnostic log. The architecture fixes rotation
// by size — logs are diagnostic data, never user history, so no
// time-based cleanup applies to them and the size bound is what keeps the
// directory finite. The numbers are starting values to be tuned on
// real-machine evidence: 1 MiB holds a long session's lines many times
// over (a full 42-item development scan logs a few tens of KiB), and
// three backups bound the whole directory at four files.
const (
	diagnosticLogMaxSize int64 = 1 << 20
	diagnosticLogBackups       = 3
)

// DiagnosticLog is this process's diagnostic log: the io.Writer the
// standard library logger sends its output to. Every line is written to
// the log file under Paths.Logs first and mirrored to stderr afterwards,
// best effort in both directions: an invalid stderr handle — a Windows
// GUI-subsystem process has no console — must not cost the line its file
// copy (io.MultiWriter would stop at the first failing writer and keep
// every line out of the file), and a failed file must not cost the line
// its stderr copy. The file appends across runs — the lines of earlier
// sessions stay retrievable until rotation retires them, so a restart
// never erases the evidence of the run before it — and rotates by size,
// keeping a bounded number of backups.
//
// Failures never propagate to the logger's callers: the sink is a
// diagnostic best effort, so when the file cannot be written it degrades
// to the stderr mirror alone after one notice, and the mirror's own
// result is always discarded.
type DiagnosticLog struct {
	mu   sync.Mutex
	path string
	max  int64
	keep int
	// file is nil once the sink is closed or degraded; Write then leaves
	// the line to the stderr mirror alone.
	file io.WriteCloser
	// size is the number of bytes in the active file, from its stat at
	// open and every write since, so rotation needs no stat per line.
	size int64
	// rotateWarned suppresses the rotation notice to one per streak of
	// failed rotations; it resets when a rotation succeeds again.
	rotateWarned bool
}

// OpenDiagnosticLog opens the process's diagnostic log under dir — the
// resolved Paths.Logs — creating the directory when missing. It is called
// by cmd/youkai only after the data-root occupancy is held: a second
// instance fails that acquire and exits before touching the log
// directory, so two processes never interleave or truncate each other's
// lines. A failed open returns an error and holds nothing; the caller
// keeps logging to stderr and reports the reason once.
func OpenDiagnosticLog(dir string) (*DiagnosticLog, error) {
	return openDiagnosticLog(dir, diagnosticLogMaxSize, diagnosticLogBackups)
}

// openDiagnosticLog parameterizes the rotation limits for the tests,
// which must not write megabytes to exercise the boundaries.
func openDiagnosticLog(dir string, maxSize int64, keep int) (*DiagnosticLog, error) {
	if dir == "" || !filepath.IsAbs(dir) {
		return nil, fmt.Errorf("app: open diagnostic log: directory %q is not an absolute path", dir)
	}
	if maxSize <= 0 || keep < 1 {
		return nil, fmt.Errorf("app: open diagnostic log in %q: invalid limits max=%d keep=%d", dir, maxSize, keep)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("app: open diagnostic log in %q: create directory: %w", dir, err)
	}
	path := filepath.Join(dir, diagnosticLogFileName)
	file, size, err := openLogAppend(path)
	if err != nil {
		return nil, fmt.Errorf("app: open diagnostic log: %w", err)
	}
	return &DiagnosticLog{path: path, max: maxSize, keep: keep, file: file, size: size}, nil
}

// Write records p in the log file and mirrors it to stderr, always
// claiming success: the standard logger's callers cannot act on a
// diagnostic failure. The file comes first — a failing stderr (the
// Windows GUI subsystem without a console, a closed redirection) loses
// nothing from the file — and the mirror's result is discarded — a
// closed or degraded file loses nothing from stderr. The standard logger
// serializes its calls, but the sink guards itself anyway: Close may
// race a slow writer.
func (l *DiagnosticLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.writeFile(p)
	_, _ = diagnosticLogStderr.Write(p)
	return len(p), nil
}

// writeFile appends p to the active log file, rotating it first when p
// would grow the file past the size limit. It reports nothing: the file
// is one best-effort half of Write, whose callers never see a failure.
// When the file stops being writable the sink degrades — a nil file,
// after one notifyLogProblem notice — and the stderr mirror carries the
// lines on.
func (l *DiagnosticLog) writeFile(p []byte) {
	if l.file == nil {
		// Closed or degraded: the line's stderr copy is all there is.
		return
	}
	if l.size+int64(len(p)) > l.max {
		l.rotateLocked()
		if l.file == nil {
			return
		}
	}
	n, err := l.file.Write(p)
	if err != nil {
		l.file.Close()
		l.file = nil
		notifyLogProblem("diagnostic log file no longer writable, continuing on stderr only: %v", err)
		return
	}
	l.size += int64(n)
}

// rotateLocked retires the active file into the backup chain and opens a
// fresh one; Write holds the lock. The active file is closed before the
// renames so rotation also works on Windows, where an open file cannot be
// renamed. The chain shifts from the oldest end, each rename replacing
// its target (Go's os.Rename replaces existing files on every platform),
// which is what retires youkai.log.<keep>. Failures are best effort: a
// backup another process holds — an antivirus or a backup tool, both
// routine on Windows — leaves the chain as it is and is noticed once per
// streak; the active file keeps every line and rotation retries on the
// next one, so the size bound gives way before a diagnostic line does.
// When the fresh file cannot be opened, the sink stays degraded (a nil
// file) and Write becomes a no-op.
func (l *DiagnosticLog) rotateLocked() {
	var shiftErr error
	if err := l.file.Close(); err != nil {
		shiftErr = err
	}
	l.file = nil
	for i := l.keep; i >= 2; i-- {
		if err := os.Rename(l.backupPath(i-1), l.backupPath(i)); err != nil && !errors.Is(err, fs.ErrNotExist) && shiftErr == nil {
			// A missing source is the normal case while the chain is still
			// filling up; anything else blocks this step of the shift.
			shiftErr = err
		}
	}
	if err := os.Rename(l.path, l.backupPath(1)); err != nil && !errors.Is(err, fs.ErrNotExist) && shiftErr == nil {
		shiftErr = err
	}
	file, size, err := openLogAppend(l.path)
	if err != nil {
		notifyLogProblem("diagnostic log cannot be reopened after rotation, continuing on stderr only: %v", err)
		return
	}
	l.file, l.size = file, size
	if shiftErr != nil {
		if !l.rotateWarned {
			notifyLogProblem("diagnostic log rotation incomplete (a backup may be held by another process), retrying on the next line: %v", shiftErr)
			l.rotateWarned = true
		}
	} else {
		l.rotateWarned = false
	}
}

// Close closes the active file. It is safe to call more than once; after
// it returns, writes still mirror to stderr but no longer reach the
// file. No flush is needed: the sink writes through to the operating
// system on every line, so a crash or a log.Fatal exit never loses lines
// already logged.
func (l *DiagnosticLog) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
}

// backupPath returns the path of the i-th rotated backup; 1 is the most
// recent, keep the oldest retained.
func (l *DiagnosticLog) backupPath(i int) string {
	return fmt.Sprintf("%s.%d", l.path, i)
}

// openLogAppend opens the log file name for appending, creating it when
// missing, and reports its current size. It is a package-level variable
// so the tests can hand the sink a failing file: a full disk or another
// process's exclusive open cannot be reproduced portably (Windows ignores
// the directory read-only bit and refuses to delete or replace open
// files), while the failing write itself is what must be exercised.
var openLogAppend = func(name string) (io.WriteCloser, int64, error) {
	f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return nil, 0, err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	return f, fi.Size(), nil
}

// diagnosticLogStderr is the mirror every line is written to after the
// file. The mirror's result is discarded because its failure modes are
// exactly the ones the file must survive: an invalid handle on the
// Windows GUI subsystem without a console, or a closed redirection —
// io.MultiWriter with stderr first would stop at such a failure and keep
// every line out of the file. A package-level variable so the tests can
// inject a failing or recording writer instead of the process's real
// stderr.
var diagnosticLogStderr io.Writer = os.Stderr

// notifyLogProblem reports a diagnostic-log problem on stderr, prefixed
// like the application's log lines. It bypasses the standard logger —
// called from inside Write, it would deadlock on the logger's own mutex —
// and is a package-level variable so the tests can record the notices
// instead of asserting on the process's real stderr.
var notifyLogProblem = func(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "youkai: "+format+"\n", args...)
}

// BuildCommit reports the VCS revision the binary was built from, as far
// as the embedded build information carries it: the first 12 hex
// characters of the commit, with "-modified" appended when the working
// tree had local changes at build time, so a log from a dirty build is
// never mistaken for the clean commit. It returns "" when the binary was
// built without VCS stamping — test binaries, and builds outside a
// repository — and callers then state the commit as unknown instead of
// silently dropping the field.
func BuildCommit() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	return buildCommitFrom(info)
}

// buildCommitFrom extracts the commit marker from a build description;
// split out so the extraction is testable with a synthesized one, since
// test binaries themselves are never VCS-stamped.
func buildCommitFrom(info *debug.BuildInfo) string {
	revision, modified := "", false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	if revision == "" {
		return ""
	}
	const shortLen = 12
	if len(revision) > shortLen {
		revision = revision[:shortLen]
	}
	if modified {
		revision += "-modified"
	}
	return revision
}
