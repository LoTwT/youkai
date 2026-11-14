//go:build darwin

package macos

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// Run the irreversible native gate in child processes so the test runner
// and later native tests keep their normal main-queue behavior.
func TestSystemTerminationUnblocksNativeMainQueueWork(t *testing.T) {
	probe := filepath.Join(t.TempDir(), "termination-probe")
	cmd := exec.Command("clang", "-fobjc-arc", "-I.", "-Itestdata", "testdata/termination_probe.m", "hotkey_darwin.m", "quartz_darwin.m", "-framework", "Foundation", "-framework", "Carbon", "-framework", "AppKit", "-framework", "ApplicationServices", "-o", probe)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build native probe: %v\n%s", err, out)
	}
	for _, mode := range []string{"unregister", "register", "late-register", "foreground", "activation"} {
		t.Run(mode, func(t *testing.T) {
			out, err := exec.Command(probe, mode).CombinedOutput()
			t.Logf("%s", out)
			if err != nil {
				t.Fatalf("native main queue dependency: %v", err)
			}
		})
	}
}
