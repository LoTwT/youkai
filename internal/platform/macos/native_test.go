package macos_test

import (
	"testing"

	macos "github.com/LoTwT/youkai/internal/platform/macos"
)

// Evidence composition across the native interfaces: the helper folds
// the enumeration snapshot and the capture session's stream outcome
// into the screen recording evidence (T2 §2.2 录屏授权 row).

func TestScreenRecordingEvidenceFromSession(t *testing.T) {
	// A session that saw a complete frame contributes the first-frame
	// evidence; one that never started does not.
	windows := []macos.NativeWindow{
		{WindowID: 1, PID: 100, BundleID: "self"},
		{WindowID: 2, PID: 200, BundleID: "game"},
	}
	e := macos.ScreenRecordingEvidenceFrom(nil, windows, 100, nil, true, nil)
	if e.OtherProcessWindows != 1 || !e.StreamStarted {
		t.Fatalf("evidence %+v", e)
	}
	if a := e.Assessment(); a.State != macos.ScreenRecordingUnknown {
		t.Fatalf("no first frame assessed %s", a.State)
	}
}
