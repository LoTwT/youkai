//go:build page_unknown_trace && !windows

package collect

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/LoTwT/youkai/internal/platform"
)

func TestPageUnknownTraceFilesArePrivateUnderUmask022(t *testing.T) {
	old := syscall.Umask(0o022)
	defer syscall.Umask(old)
	t.Setenv(pageUnknownTraceEnv, "1")
	dir := t.TempDir()
	run := pageUnknownTestRun(t, pageUnknownTestFrames(t), 4, dir)
	tracePageUnknownInput(run, 19, 1, platform.InputResult{Status: platform.InputPosted})
	if _, err := run.awaitDetailFrames(context.Background(), pageUnknownTestBaseline(),
		&DetailEvidence{Position: PositionVerdict{Position: 2}}, 19, false, nil, true); err == nil {
		t.Fatal("synthetic terminal PageUnknown was accepted")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("diagnostic dirs = %d err=%v", len(entries), err)
	}
	sub := filepath.Join(dir, entries[0].Name())
	if info, err := os.Stat(sub); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("diagnostic directory mode = %v err=%v, want 0700", info, err)
	}
	files, err := os.ReadDir(sub)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range files {
		info, err := entry.Info()
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode = %v, want 0600", entry.Name(), info.Mode().Perm())
		}
	}
}
