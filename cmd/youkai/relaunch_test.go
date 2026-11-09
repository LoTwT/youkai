package main

import (
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LoTwT/youkai/internal/app"
)

// requireRootHeld fails when another process could take root, naming the
// point in the exit sequence that must still hold the occupancy.
func requireRootHeld(t *testing.T, root, when string) {
	t.Helper()
	taken, err := app.AcquireDataRootLock(root)
	if err == nil {
		taken.Close()
		t.Fatalf("data-root occupancy %s was released, want it held", when)
	}
	if !errors.Is(err, app.ErrLockHeld) {
		t.Fatalf("data-root occupancy %s: %v, want ErrLockHeld", when, err)
	}
}

// routeLogToDataRoot makes the standard logger write into a fresh
// diagnostic log under a data root and restores the previous output when
// the test ends.
func routeLogToDataRoot(t *testing.T) (logDir string) {
	t.Helper()
	logDir = t.TempDir()
	logFile, err := app.OpenDiagnosticLog(logDir)
	if err != nil {
		t.Fatalf("open the diagnostic log: %v", err)
	}
	t.Cleanup(func() { _ = logFile.Close() })
	previous := log.Writer()
	log.SetOutput(logFile)
	t.Cleanup(func() { log.SetOutput(previous) })
	return logDir
}

// readDiagnosticLog returns the data root's diagnostic log.
func readDiagnosticLog(t *testing.T, logDir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(logDir, "youkai.log"))
	if err != nil {
		t.Fatalf("read the diagnostic log: %v", err)
	}
	return string(data)
}

// TestRelaunchReleasesDataRootBeforeRestart pins the installed update's
// restart handoff: the root stays held through every point at which the
// exit can still be refused or the app keeps running, and the OnQuit
// listener — the last step before MyGo starts the new process — detaches
// the diagnostic log and then releases the root.
func TestRelaunchReleasesDataRootBeforeRestart(t *testing.T) {
	root := t.TempDir()
	held, err := app.AcquireDataRootLock(root)
	if err != nil {
		t.Fatalf("acquire the data-root occupancy: %v", err)
	}
	defer held.Close()
	logDir := routeLogToDataRoot(t)
	handoff := relaunchHandoff{
		detachLog: detachDiagnosticLog,
		release: func() error {
			if log.Writer() != os.Stderr {
				t.Error("the data-root occupancy was released before the diagnostic log was detached")
			}
			return held.Close()
		},
	}

	// A quit without the settled exit: the install's first relaunch is
	// cancelled by the exit interception, and the app keeps running. The
	// occupancy and the diagnostic log stay where they are.
	handoff.onQuit()
	requireRootHeld(t, root, "after a quit without a settled exit")
	log.Print("youkai: logged before the restart handoff")
	if got := readDiagnosticLog(t, logDir); !strings.Contains(got, "logged before the restart handoff") {
		t.Fatalf("a line logged before the restart handoff did not reach the data root: %q", got)
	}

	handoff.updateInstalled = true
	handoff.settle()
	// The settled exit re-issued the relaunch, but the quit is still on
	// its way; a listener can refuse it.
	requireRootHeld(t, root, "after the settled exit requested the relaunch")

	handoff.onQuit()
	log.Print("youkai: logged after the restart handoff")
	if got := readDiagnosticLog(t, logDir); strings.Contains(got, "logged after the restart handoff") {
		t.Fatalf("a line logged after the restart handoff reached the data root: %q", got)
	}
	if log.Writer() != os.Stderr {
		t.Fatal("the logger still writes to the diagnostic log after the restart handoff")
	}
	taken, err := app.AcquireDataRootLock(root)
	if err != nil {
		t.Fatalf("data root after the restart release: %v", err)
	}
	taken.Close()
}

// TestPlainExitKeepsDataRootUntilProcessExit pins the non-restart path: an
// explicit exit that is not the installed update's restart leaves the
// release to the entry point's deferred Close at process exit, even after
// a successful teardown, and the diagnostic log keeps receiving lines.
func TestPlainExitKeepsDataRootUntilProcessExit(t *testing.T) {
	root := t.TempDir()
	held, err := app.AcquireDataRootLock(root)
	if err != nil {
		t.Fatalf("acquire the data-root occupancy: %v", err)
	}
	logDir := routeLogToDataRoot(t)
	handoff := relaunchHandoff{detachLog: detachDiagnosticLog, release: held.Close}
	handoff.settle()
	handoff.onQuit()
	requireRootHeld(t, root, "after a plain exit's quit listener")
	log.Print("youkai: logged on the plain exit")
	if got := readDiagnosticLog(t, logDir); !strings.Contains(got, "logged on the plain exit") {
		t.Fatalf("a line logged on the plain exit did not reach the data root: %q", got)
	}

	// The entry point's deferred release is the process-exit release.
	if err := held.Close(); err != nil {
		t.Fatalf("release the data-root occupancy at exit: %v", err)
	}
	taken, err := app.AcquireDataRootLock(root)
	if err != nil {
		t.Fatalf("data root after the exit release: %v", err)
	}
	taken.Close()
}
