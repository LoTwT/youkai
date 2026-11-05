package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/LoTwT/youkai/internal/app"
)

// The occupancy error is the only signal the entry point has: app wraps the
// platform lock failure as ErrLockHeld, and every other acquire failure must
// keep the generic case, or a permission problem would tell the user to
// exit another youkai.
func TestClassifyDataRootFailure(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want dataRootFailureKind
	}{
		{
			name: "wrapped lock holder",
			err:  fmt.Errorf("app: acquire data-root lock on %q: %w", "/data/Youkai", app.ErrLockHeld),
			want: dataRootHeldElsewhere,
		},
		{
			name: "another error",
			err:  fmt.Errorf("app: acquire data-root lock on %q: create data root: %w", "/data/Youkai", errors.New("permission denied")),
			want: dataRootUnavailable,
		},
		{
			name: "plain error",
			err:  errors.New("disk full"),
			want: dataRootUnavailable,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyDataRootFailure(tt.err); got != tt.want {
				t.Fatalf("classifyDataRootFailure(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// A held data root and an unreadable one produce the two texts of 08 §10;
// the generic text carries the underlying error as the reason.
func TestDataRootFailureMessage(t *testing.T) {
	const heldText = "另一个 youkai 正在使用数据目录（可能是另一个版本）。请先从通知区域退出它，再重新打开。"

	if got := dataRootFailureMessage(dataRootHeldElsewhere, app.ErrLockHeld); got != heldText {
		t.Fatalf("held message = %q, want %q", got, heldText)
	}

	reason := errors.New("permission denied")
	wantUnavailable := "youkai 无法打开它的数据目录：permission denied。请检查磁盘与权限后重新打开。"
	if got := dataRootFailureMessage(dataRootUnavailable, reason); got != wantUnavailable {
		t.Fatalf("unavailable message = %q, want %q", got, wantUnavailable)
	}
}

// The classification must survive the real acquire path, not only a
// hand-wrapped sentinel: a second holder in this process is refused with
// ErrLockHeld (the platform primitive conflicts across open descriptions),
// and a path blocked by a regular file fails for another reason.
func TestDataRootFailureClassificationFromAcquire(t *testing.T) {
	t.Run("occupied by another holder", func(t *testing.T) {
		dataDir := t.TempDir()
		holder, err := app.AcquireDataRootLock(dataDir)
		if err != nil {
			t.Fatalf("hold data root: %v", err)
		}
		defer holder.Close()

		_, err = app.AcquireDataRootLock(dataDir)
		if !errors.Is(err, app.ErrLockHeld) {
			t.Fatalf("second acquire: err = %v, want ErrLockHeld", err)
		}
		if got := classifyDataRootFailure(err); got != dataRootHeldElsewhere {
			t.Fatalf("classified as %v, want dataRootHeldElsewhere", got)
		}
	})

	t.Run("unavailable root", func(t *testing.T) {
		blocked := filepath.Join(t.TempDir(), "blocked")
		if err := os.WriteFile(blocked, []byte("not a directory"), 0o600); err != nil {
			t.Fatalf("create blocking file: %v", err)
		}

		_, err := app.AcquireDataRootLock(blocked)
		if err == nil {
			t.Fatal("acquiring a data root blocked by a file succeeded")
		}
		if errors.Is(err, app.ErrLockHeld) {
			t.Fatalf("blocked root reported as held: %v", err)
		}
		if got := classifyDataRootFailure(err); got != dataRootUnavailable {
			t.Fatalf("classified as %v, want dataRootUnavailable", got)
		}
	})
}
