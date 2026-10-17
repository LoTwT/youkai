package app

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
)

// The diagnostic log sink is tested on the real filesystem of whatever OS
// runs the tests: the failures it must survive are produced by path shape
// (a file where a directory belongs, a directory where the log file
// belongs), never by permissions, which Windows does not carry the same
// way (its read-only bit does not stop file creation in a directory) —
// the CI runs these tests on both platforms. Only the two states that no
// filesystem shape can produce portably — a file that fails every write,
// and the stderr notices — use the package's injection points.

// TestOpenDiagnosticLogCreatesDirectoryAndAppends covers the two promises
// that make the log retrievable at all: the logs directory appears without
// any other writer having to create it (the M1 report found it never
// existed on installed builds), and a second run appends instead of
// truncating the first run's lines.
func TestOpenDiagnosticLogCreatesDirectoryAndAppends(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs", "youkai") // does not exist yet
	first, err := openDiagnosticLog(dir, 1024, 3)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := first.Write([]byte("first run line\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	// Close is idempotent; main's deferred close may follow a degraded one.
	if err := first.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}

	second, err := openDiagnosticLog(dir, 1024, 3)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { second.Close() })
	if _, err := second.Write([]byte("second run line\n")); err != nil {
		t.Fatalf("write after reopen: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, diagnosticLogFileName))
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	want := "first run line\nsecond run line\n"
	if string(got) != want {
		t.Errorf("log file = %q, want %q", got, want)
	}
}

// TestDiagnosticLogRotationAndRetention pins the bounded-directory
// contract: writes beyond the size limit rotate the active file into a
// numbered backup chain, the chain never grows past keep backups, and
// what falls out of the chain is the oldest run of lines — never a middle
// one, and never the active tail. With a limit of two lines per file and
// two backups, ten 50-byte lines leave the last six: two per file,
// newest in the active file.
func TestDiagnosticLogRotationAndRetention(t *testing.T) {
	dir := t.TempDir()
	log, err := openDiagnosticLog(dir, 120, 2)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { log.Close() })

	const lineLen = 50
	lines := make([]string, 10)
	for i := range lines {
		lines[i] = fmt.Sprintf("l%02d%s\n", i+1, strings.Repeat("x", lineLen-4))
		if len(lines[i]) != lineLen {
			t.Fatalf("test line %d is %d bytes, want %d", i+1, len(lines[i]), lineLen)
		}
		if _, err := log.Write([]byte(lines[i])); err != nil {
			t.Fatalf("write line %d: %v", i+1, err)
		}
	}

	active := filepath.Join(dir, diagnosticLogFileName)
	for name, want := range map[string]string{
		active:        lines[8] + lines[9], // the active file holds the newest pair
		active + ".1": lines[6] + lines[7],
		active + ".2": lines[4] + lines[5],
		active + ".3": "", // beyond the retention limit
	} {
		got, err := os.ReadFile(name)
		if os.IsNotExist(err) {
			if want != "" {
				t.Errorf("%s: missing, want content %q", filepath.Base(name), want)
			}
			continue
		}
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if string(got) != want {
			t.Errorf("%s = %q, want %q", filepath.Base(name), got, want)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read log directory: %v", err)
	}
	if len(entries) != 3 { // active + keep backups, nothing more
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("log directory holds %d files (%v), want 3", len(entries), names)
	}
}

// TestDiagnosticLogRotationRetriesPastObstruction covers the failure the
// rotation must survive on Windows: another process holding a backup file
// (antivirus, backup tool). Holding the file open cannot be reproduced
// portably — Windows refuses to replace open files, Unix renames them
// happily — so the obstruction is a directory in the backup's name, which
// no platform lets a file rename replace. Rotation then must not lose or
// duplicate lines: it keeps appending to the active file and retries on
// the next line, noticing the problem exactly once per streak of failed
// rotations, and succeeds as soon as the obstruction is gone.
func TestDiagnosticLogRotationRetriesPastObstruction(t *testing.T) {
	notices := setLogNotices(t)
	dir := t.TempDir()
	log, err := openDiagnosticLog(dir, 40, 1)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { log.Close() })

	backup := filepath.Join(dir, diagnosticLogFileName+".1")
	if err := os.Mkdir(backup, 0o700); err != nil {
		t.Fatalf("place obstruction: %v", err)
	}
	// 20-byte lines against a 40-byte limit: every second line wants to
	// rotate, and every rotation fails on the obstructed backup name.
	for i := 1; i <= 4; i++ {
		line := fmt.Sprintf("l%02d%s\n", i, strings.Repeat("x", 16))
		if _, err := log.Write([]byte(line)); err != nil {
			t.Fatalf("write line %d: %v", i, err)
		}
	}
	got, err := os.ReadFile(filepath.Join(dir, diagnosticLogFileName))
	if err != nil {
		t.Fatalf("read active file: %v", err)
	}
	var want strings.Builder
	for i := 1; i <= 4; i++ {
		fmt.Fprintf(&want, "l%02d%s\n", i, strings.Repeat("x", 16))
	}
	if string(got) != want.String() {
		t.Errorf("active file during obstruction = %q, want %q (no loss, no truncation)", got, want.String())
	}
	if len(*notices) != 1 {
		t.Errorf("rotation notices = %d (%v), want exactly one per streak", len(*notices), *notices)
	}

	// The obstruction clears: the next over-limit write retires the active
	// file into the backup intact, and a later streak of failures is
	// noticed again — the warning is per streak, not per process.
	if err := os.RemoveAll(backup); err != nil {
		t.Fatalf("clear obstruction: %v", err)
	}
	if _, err := log.Write([]byte("l05xxxxxxxxxxxxxxxx\n")); err != nil {
		t.Fatalf("write after clearing: %v", err)
	}
	retired, err := os.ReadFile(backup)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(retired) != want.String() {
		t.Errorf("backup after recovery = %q, want the retired content %q", retired, want.String())
	}
	// The backup is a file now; clear it before blocking its name again.
	if err := os.Remove(backup); err != nil {
		t.Fatalf("remove retired backup: %v", err)
	}
	if err := os.Mkdir(backup, 0o700); err != nil {
		t.Fatalf("re-place obstruction: %v", err)
	}
	for i := 1; i <= 2; i++ {
		if _, err := log.Write([]byte("l06xxxxxxxxxxxxxxxx\n")); err != nil {
			t.Fatalf("write during second obstruction %d: %v", i, err)
		}
	}
	if len(*notices) != 2 {
		t.Errorf("rotation notices after a second streak = %d (%v), want a fresh one", len(*notices), *notices)
	}
}

// TestOpenDiagnosticLogSurfacesOpenFailures pins the startup fallback:
// when the log cannot be opened at all, the open returns an error — the
// caller then keeps logging to stderr and reports the reason once — and
// the app still starts. Both failures come from path shape rather than
// permissions so they hold on every CI platform: a regular file where the
// logs directory should be, and a directory where the log file should be.
func TestOpenDiagnosticLogSurfacesOpenFailures(t *testing.T) {
	base := t.TempDir()
	dirAsFile := filepath.Join(base, "logs-as-file")
	if err := os.WriteFile(dirAsFile, []byte("occupied"), 0o600); err != nil {
		t.Fatalf("occupy directory path with a file: %v", err)
	}
	if _, err := OpenDiagnosticLog(dirAsFile); err == nil {
		t.Error("open with a file in the directory's place: err = nil, want an error")
	}

	dir := filepath.Join(base, "logs")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatalf("create logs directory: %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, diagnosticLogFileName), 0o700); err != nil {
		t.Fatalf("occupy log file path with a directory: %v", err)
	}
	if _, err := OpenDiagnosticLog(dir); err == nil {
		t.Error("open with a directory in the file's place: err = nil, want an error")
	}
}

// TestOpenDiagnosticLogRejectsInvalidLimits keeps the parameterized
// constructor honest: it exists for the tests, and a zero or negative
// limit there is a broken test, not a silently different rotation
// behavior. The relative-path guard mirrors AcquireDataRootLock's.
func TestOpenDiagnosticLogRejectsInvalidLimits(t *testing.T) {
	for _, tc := range []struct {
		max  int64
		keep int
	}{
		{0, 3}, {-1, 3}, {1024, 0}, {1024, -1},
	} {
		if _, err := openDiagnosticLog(t.TempDir(), tc.max, tc.keep); err == nil {
			t.Errorf("open(max=%d, keep=%d): err = nil, want an error", tc.max, tc.keep)
		}
	}
	if _, err := openDiagnosticLog(filepath.Join("relative", "logs"), 1024, 3); err == nil {
		t.Error("open with a relative directory: err = nil, want an error")
	}
}

// TestDiagnosticLogWriteFailureDegradesToStderr pins the mid-run
// contract: when the file stops accepting writes — a full disk, or an
// exclusive opener on Windows — the sink must claim success to the logger
// (diagnostics never fail the application's own operations), say so on
// stderr exactly once, and become a no-op so every later line still
// reaches stderr through the MultiWriter. A real failing file cannot be
// produced portably, so the injected opener hands the sink one that fails
// every write.
func TestDiagnosticLogWriteFailureDegradesToStderr(t *testing.T) {
	notices := setLogNotices(t)
	setOpenLogAppend(t, func(string) (io.WriteCloser, int64, error) {
		return failingLogWriteCloser{}, 0, nil
	})
	log, err := openDiagnosticLog(filepath.Join(t.TempDir(), "logs"), 1024, 3)
	if err != nil {
		t.Fatalf("open with the injected file: %v", err)
	}
	const line = "line\n"
	for i := 1; i <= 3; i++ {
		if n, err := log.Write([]byte(line)); err != nil || n != len(line) {
			t.Fatalf("write %d: n=%d err=%v, want the sink to claim success", i, n, err)
		}
	}
	if err := log.Close(); err != nil {
		t.Fatalf("close after degradation: %v", err)
	}
	if len(*notices) != 1 {
		t.Errorf("notices = %d (%v), want exactly one", len(*notices), *notices)
	}
}

// failingLogWriteCloser is the injected log file of the degradation test:
// every write fails, as a full disk or an exclusive opener would make the
// real one fail.
type failingLogWriteCloser struct{}

func (failingLogWriteCloser) Write([]byte) (int, error) { return 0, errors.New("disk full") }
func (failingLogWriteCloser) Close() error              { return nil }

// TestBuildCommitFrom pins the header's commit marker: a short revision,
// the modified marker for dirty builds, and "" when the build carries no
// VCS stamping. Test binaries are never stamped, which is why the
// extraction is split out and fed synthesized build information here.
func TestBuildCommitFrom(t *testing.T) {
	for _, tc := range []struct {
		name     string
		settings []debug.BuildSetting
		want     string
	}{
		{
			name: "clean",
			settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "93b3cdc507ddec6b5f68e4c7a7580e25e3dd7f91"},
				{Key: "vcs.modified", Value: "false"},
			},
			want: "93b3cdc507dd",
		},
		{
			name: "modified working tree",
			settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "93b3cdc507ddec6b5f68e4c7a7580e25e3dd7f91"},
				{Key: "vcs.modified", Value: "true"},
			},
			want: "93b3cdc507dd-modified",
		},
		{
			name:     "unstamped build",
			settings: []debug.BuildSetting{{Key: "GOOS", Value: "darwin"}},
			want:     "",
		},
		{
			name:     "short revision kept whole",
			settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "abc"}, {Key: "vcs.modified", Value: "false"}},
			want:     "abc",
		},
	} {
		if got := buildCommitFrom(&debug.BuildInfo{Settings: tc.settings}); got != tc.want {
			t.Errorf("%s: buildCommitFrom = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// setLogNotices replaces notifyLogProblem with a recorder for this test
// and returns the recorded notices; the sink's problems must not depend
// on the process's real stderr to be observable.
func setLogNotices(t *testing.T) *[]string {
	t.Helper()
	previous := notifyLogProblem
	var got []string
	notifyLogProblem = func(format string, args ...any) {
		got = append(got, fmt.Sprintf(format, args...))
	}
	t.Cleanup(func() { notifyLogProblem = previous })
	return &got
}

// setOpenLogAppend swaps the log-file opener for this test and restores
// it afterwards; the package-level indirection exists for exactly these
// injections (see openLogAppend).
func setOpenLogAppend(t *testing.T, open func(name string) (io.WriteCloser, int64, error)) {
	t.Helper()
	previous := openLogAppend
	openLogAppend = open
	t.Cleanup(func() { openLogAppend = previous })
}
