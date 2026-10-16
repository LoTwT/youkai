package app

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// The occupancy locks are cross-process by definition, so the tests
// prove them with real independent processes: the test binary re-execs
// itself as a helper that owns locks on command and reports the outcome
// over a pipe. The parent drives the helpers and asserts the protocol;
// every wait is on a pipe message or a process exit, never a fixed
// sleep.

// lockHelperEnv switches the re-executed test binary into helper mode.
const lockHelperEnv = "YOUKAI_LOCK_TEST_HELPER"

// lockHelperScanBaseEnv points a helper at a temporary scan-lock base
// directory. The base is injected rather than redirected through the
// environment the OS lookups read: the Known Folder lookup of Windows
// cannot be redirected per process at all.
const lockHelperScanBaseEnv = "YOUKAI_LOCK_TEST_SCAN_BASE"

func TestMain(m *testing.M) {
	if os.Getenv(lockHelperEnv) == "1" {
		if base := os.Getenv(lockHelperScanBaseEnv); base != "" {
			scanLockBase = func() (string, error) { return base, nil }
		}
		os.Exit(runLockHelper())
	}
	os.Exit(m.Run())
}

// runLockHelper serves the lock protocol on stdin/stdout: `data <dir>`
// and `scan` acquire the occupancy, `release-data` and `release-scan`
// drop it, `quit` exits. One response line per command keeps the parent
// synchronized without any timing assumption.
func runLockHelper() int {
	in := bufio.NewScanner(os.Stdin)
	out := bufio.NewWriter(os.Stdout)
	respond := func(format string, args ...any) {
		fmt.Fprintf(out, format+"\n", args...)
		out.Flush()
	}
	var data *DataRootLock
	var scan *ScanLock
	for in.Scan() {
		cmd, arg, _ := strings.Cut(in.Text(), " ")
		switch cmd {
		case "data":
			l, err := AcquireDataRootLock(arg)
			switch {
			case err == nil:
				data = l
				respond("acquired data")
			case errors.Is(err, ErrLockHeld):
				respond("held data")
			default:
				respond("error data %v", err)
			}
		case "scan":
			if data == nil {
				respond("error scan no data-root lock held")
				continue
			}
			s, err := data.AcquireScanLock()
			switch {
			case err == nil:
				scan = s
				respond("acquired scan")
			case errors.Is(err, ErrLockHeld):
				respond("held scan")
			default:
				respond("error scan %v", err)
			}
		case "release-data":
			if data == nil {
				respond("error release-data nothing held")
				continue
			}
			err := data.Close()
			data = nil
			if err != nil {
				respond("error release-data %v", err)
				continue
			}
			respond("released data")
		case "release-scan":
			if scan == nil {
				respond("error release-scan nothing held")
				continue
			}
			err := scan.Close()
			scan = nil
			if err != nil {
				respond("error release-scan %v", err)
				continue
			}
			respond("released scan")
		case "quit":
			if scan != nil {
				scan.Close()
			}
			if data != nil {
				data.Close()
			}
			respond("quit")
			return 0
		default:
			respond("error unknown-command %q", cmd)
			return 1
		}
	}
	// Stdin closed without quit: release what is held and exit cleanly.
	if scan != nil {
		scan.Close()
	}
	if data != nil {
		data.Close()
	}
	return 0
}

// lockHelper drives one helper process. Lines travel parent→child as
// commands and child→parent as outcomes; stderr is captured so failures
// name the misbehaving process.
type lockHelper struct {
	t       *testing.T
	cmd     *exec.Cmd
	in      io.WriteCloser
	out     *bufio.Scanner
	stderr  *syncBuffer
	waited  bool
	stopped bool
}

// syncBuffer is a mutex-guarded buffer: exec copies the child's stderr
// on its own goroutine, so the test may read it while the child lives.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// startLockHelper starts one helper process. A non-empty scanBase makes
// the helper resolve the scan lock under that directory (see
// lockHelperScanBaseEnv); data-root locks always use the directory given
// per command.
func startLockHelper(t *testing.T, scanBase string) *lockHelper {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("locate test binary: %v", err)
	}
	cmd := exec.Command(exe)
	env := []string{lockHelperEnv + "=1"}
	if scanBase != "" {
		env = append(env, lockHelperScanBaseEnv+"="+scanBase)
	}
	cmd.Env = append(os.Environ(), env...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("helper stdin pipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("helper stdout pipe: %v", err)
	}
	h := &lockHelper{t: t, cmd: cmd, in: stdin, out: bufio.NewScanner(stdout), stderr: &syncBuffer{}}
	cmd.Stderr = h.stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}
	t.Cleanup(h.stop)
	return h
}

// send writes one command line to the helper.
func (h *lockHelper) send(format string, args ...any) {
	h.t.Helper()
	if _, err := fmt.Fprintf(h.in, format+"\n", args...); err != nil {
		h.t.Fatalf("write to helper: %v (stderr: %q)", err, h.stderr.String())
	}
}

// expect reads the helper's next response line and requires the exact
// outcome, so protocol corruption fails loudly instead of miscounting.
func (h *lockHelper) expect(want string) {
	h.t.Helper()
	if !h.out.Scan() {
		h.t.Fatalf("helper output ended, want %q (stderr: %q)", want, h.stderr.String())
	}
	if got := h.out.Text(); got != want {
		h.t.Fatalf("helper responded %q, want %q (stderr: %q)", got, want, h.stderr.String())
	}
}

// stop shuts the helper down for good. It is best effort by design —
// cleanup must not mask the failure that led here: a quit line, or
// alone the closed stdin, makes the helper release its locks and exit.
func (h *lockHelper) stop() {
	if h.stopped {
		return
	}
	h.stopped = true
	if h.waited {
		return
	}
	h.waited = true
	io.WriteString(h.in, "quit\n")
	h.in.Close()
	if err := h.cmd.Wait(); err != nil {
		h.t.Logf("helper exited: %v (stderr: %q)", err, h.stderr.String())
	}
}

// kill terminates the helper without its clean shutdown, the way a
// crashed process dies: no Close, no unlock, only the OS reclaiming the
// process. Waiting for the exit guarantees the kernel has finished
// releasing the process's descriptors before the caller continues.
func (h *lockHelper) kill() {
	h.t.Helper()
	if err := h.cmd.Process.Kill(); err != nil {
		h.t.Fatalf("kill helper: %v", err)
	}
	h.waited = true
	if err := h.cmd.Wait(); err == nil {
		h.t.Fatal("killed helper exited cleanly, want a crash exit")
	}
}

// TestDataRootLockExclusiveAcrossProcesses checks the single-holder rule
// with three independent processes: the first helper holds a data root,
// a second helper and the test process itself are refused, and after the
// holder releases, the refused process takes over.
func TestDataRootLockExclusiveAcrossProcesses(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")

	holder := startLockHelper(t, "")
	holder.send("data %s", dataDir)
	holder.expect("acquired data")

	rival := startLockHelper(t, "")
	rival.send("data %s", dataDir)
	rival.expect("held data")

	// The test process is a third independent process and is refused too.
	if _, err := AcquireDataRootLock(dataDir); !errors.Is(err, ErrLockHeld) {
		t.Fatalf("acquire in test process: err = %v, want ErrLockHeld", err)
	}

	holder.send("release-data")
	holder.expect("released data")
	rival.send("data %s", dataDir)
	rival.expect("acquired data")
}

// dataDirAliases returns path strings that reach dataDir's directory
// through other routes. Each alias kind that the host supports is
// exercised; the ones the OS or filesystem refuses (unprivileged
// symlinks on Windows, case-sensitive filesystems) are skipped with a
// logged reason instead of assumed.
func dataDirAliases(t *testing.T, base, dataDir string) []string {
	t.Helper()
	var aliases []string

	// A `..` traversal: the OS resolves the intermediate component, so
	// the same lock file is reached through a different string.
	anchor := filepath.Join(base, "anchor")
	if err := os.Mkdir(anchor, 0o700); err != nil {
		t.Fatalf("create anchor directory: %v", err)
	}
	sep := string(os.PathSeparator)
	aliases = append(aliases, base+sep+"anchor"+sep+".."+sep+"data")

	// A symlink to the data root; creation may need privileges on Windows.
	link := filepath.Join(base, "link")
	if err := os.Symlink(dataDir, link); err != nil {
		t.Logf("skip symlink alias, host refused creation: %v", err)
	} else {
		aliases = append(aliases, link)
	}

	// A case variant of the directory name, meaningful only where the
	// filesystem is case-insensitive; probe it instead of assuming.
	variant := filepath.Join(base, strings.ToUpper(filepath.Base(dataDir)))
	vst, verr := os.Stat(variant)
	ost, oerr := os.Stat(dataDir)
	switch {
	case verr != nil:
		t.Logf("skip case alias, %q does not resolve to the data root: %v", variant, verr)
	case oerr != nil:
		t.Fatalf("stat data root: %v", oerr)
	case os.SameFile(vst, ost):
		aliases = append(aliases, variant)
	default:
		t.Logf("skip case alias, filesystem treats %q as a distinct directory", variant)
	}

	if len(aliases) == 0 {
		t.Fatal("no alias form is applicable on this host")
	}
	return aliases
}

// TestDataRootLockPathAliasesConflict proves that path aliases of one
// actual data root fall into the same occupancy domain: a helper holding
// the root by its real path refuses another helper that reaches it
// through a `..` traversal, a symlink, or a case variant.
func TestDataRootLockPathAliasesConflict(t *testing.T) {
	base := t.TempDir()
	dataDir := filepath.Join(base, "data")

	holder := startLockHelper(t, "")
	holder.send("data %s", dataDir)
	holder.expect("acquired data")

	rival := startLockHelper(t, "")
	for _, alias := range dataDirAliases(t, base, dataDir) {
		rival.send("data %s", alias)
		rival.expect("held data")
	}
}

// TestDataRootLockReleasedWhenHolderKilled proves the crash story: after
// the holder dies without any cleanup, the OS releases the occupancy and
// another process can take the same data root.
func TestDataRootLockReleasedWhenHolderKilled(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")

	crashed := startLockHelper(t, "")
	crashed.send("data %s", dataDir)
	crashed.expect("acquired data")
	crashed.kill()

	successor := startLockHelper(t, "")
	successor.send("data %s", dataDir)
	successor.expect("acquired data")
}

// TestScanLockExclusiveAcrossDataRoots proves the two occupancy domains
// coexist and contend correctly: processes on different data roots each
// hold their own data-root occupancy, but only one of them can hold the
// per-user scan occupancy — and releasing it hands the slot over.
func TestScanLockExclusiveAcrossDataRoots(t *testing.T) {
	// The scan lock lives under the per-user application-data base of the
	// OS; point it at a temporary directory — in this process directly,
	// in the helpers through their environment — so the test never
	// touches the real one nor collides with a running youkai.
	scanBase := t.TempDir()
	setScanLockBase(t, scanBase)

	firstDir := filepath.Join(t.TempDir(), "first")
	secondDir := filepath.Join(t.TempDir(), "second")

	first := startLockHelper(t, scanBase)
	first.send("data %s", firstDir)
	first.expect("acquired data")
	second := startLockHelper(t, scanBase)
	second.send("data %s", secondDir)
	second.expect("acquired data")

	first.send("scan")
	first.expect("acquired scan")

	second.send("scan")
	second.expect("held scan")

	// The test process holds a third data root and is refused too.
	thirdDir := filepath.Join(t.TempDir(), "third")
	third, err := AcquireDataRootLock(thirdDir)
	if err != nil {
		t.Fatalf("acquire third data root: %v", err)
	}
	defer third.Close()
	if _, err := third.AcquireScanLock(); !errors.Is(err, ErrLockHeld) {
		t.Fatalf("acquire scan lock in test process: err = %v, want ErrLockHeld", err)
	}

	first.send("release-scan")
	first.expect("released scan")
	second.send("scan")
	second.expect("acquired scan")
}

// TestScanLockPathIsPerUserAndBuildIndependent pins the scan lock's
// location contract: a fixed base-identifier directory under the
// per-user application-data base — derived from neither the application
// name nor the data root — so every build of youkai contends on the same
// file.
func TestScanLockPathIsPerUserAndBuildIndependent(t *testing.T) {
	base := setScanLockBase(t, t.TempDir())
	got, err := scanLockPath()
	if err != nil {
		t.Fatalf("scanLockPath: %v", err)
	}
	if want := filepath.Join(base, "com.lotwt.youkai", "scan.lock"); got != want {
		t.Fatalf("scan lock path = %q, want %q", got, want)
	}
}

// TestDataRootLockSecondAcquireRefusedInProcess checks the primitive's
// conflict semantics without processes: a second acquire through a fresh
// descriptor of the same lock file is refused even inside the same
// process, and after a release the next acquire succeeds.
func TestDataRootLockSecondAcquireRefusedInProcess(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	first, err := AcquireDataRootLock(dataDir)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	defer first.Close()
	if _, err := AcquireDataRootLock(dataDir); !errors.Is(err, ErrLockHeld) {
		t.Fatalf("second acquire in process: err = %v, want ErrLockHeld", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("release: %v", err)
	}
	again, err := AcquireDataRootLock(dataDir)
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	again.Close()
}

// TestScanLockSecondAcquireRefusedInProcess is the in-process
// counterpart for the scan occupancy.
func TestScanLockSecondAcquireRefusedInProcess(t *testing.T) {
	setScanLockBase(t, t.TempDir())
	data, err := AcquireDataRootLock(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("acquire data root: %v", err)
	}
	defer data.Close()
	first, err := data.AcquireScanLock()
	if err != nil {
		t.Fatalf("first scan acquire: %v", err)
	}
	if _, err := data.AcquireScanLock(); !errors.Is(err, ErrLockHeld) {
		t.Fatalf("second scan acquire in process: err = %v, want ErrLockHeld", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("release scan: %v", err)
	}
	second, err := data.AcquireScanLock()
	if err != nil {
		t.Fatalf("scan acquire after release: %v", err)
	}
	second.Close()
}

// TestDataRootLockCloseGatesScanLock checks the release semantics of the
// API: Close is idempotent, and once the data-root occupancy is released
// the same holder can no longer ask for the scan occupancy — the refusal
// is a usage error, not a lock conflict.
func TestDataRootLockCloseGatesScanLock(t *testing.T) {
	setScanLockBase(t, t.TempDir())
	data, err := AcquireDataRootLock(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("acquire data root: %v", err)
	}
	scan, err := data.AcquireScanLock()
	if err != nil {
		t.Fatalf("acquire scan lock: %v", err)
	}
	if err := scan.Close(); err != nil {
		t.Fatalf("release scan lock: %v", err)
	}
	if err := data.Close(); err != nil {
		t.Fatalf("release data-root lock: %v", err)
	}
	if err := data.Close(); err != nil {
		t.Fatalf("second Close: err = %v, want nil", err)
	}
	_, err = data.AcquireScanLock()
	if err == nil || errors.Is(err, ErrLockHeld) {
		t.Fatalf("scan acquire after data-root release: err = %v, want a released-usage error", err)
	}
}

// TestAcquireDataRootLockRejectsRelativePath keeps the failure local and
// loud: a relative data root would lock whatever the working directory
// happens to be.
func TestAcquireDataRootLockRejectsRelativePath(t *testing.T) {
	if _, err := AcquireDataRootLock(filepath.Join("relative", "data")); err == nil || errors.Is(err, ErrLockHeld) {
		t.Fatalf("relative data root: err = %v, want an absolute-path error", err)
	}
}

// setScanLockBase points the scan-lock base resolution at dir for this
// test process and returns dir. Helper children receive the same base
// through their environment instead — see lockHelperScanBaseEnv.
func setScanLockBase(t *testing.T, dir string) string {
	t.Helper()
	previous := scanLockBase
	scanLockBase = func() (string, error) { return dir, nil }
	t.Cleanup(func() { scanLockBase = previous })
	return dir
}
