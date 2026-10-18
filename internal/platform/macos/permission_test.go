package macos_test

import (
	"errors"
	"testing"

	macos "github.com/LoTwT/youkai/internal/platform/macos"
)

// Permission mapping tests (T2 §2.2): the three capabilities stay
// separate, queries never claim more than they observed, and screen
// recording is assessed only from what the actual capture calls
// returned.

func TestAccessibilityTrustMapping(t *testing.T) {
	cases := []struct {
		trust macos.AccessibilityTrust
		want  macos.CapabilityState
	}{
		{macos.AccessibilityTrust{Trusted: true}, macos.CapabilityGranted},
		{macos.AccessibilityTrust{Trusted: false}, macos.CapabilityNotGranted},
		{macos.AccessibilityTrust{Err: errors.New("query failed")}, macos.CapabilityUnknown},
	}
	for _, c := range cases {
		if got := c.trust.State(); got != c.want {
			t.Fatalf("trust %+v mapped to %s", c.trust, got)
		}
	}
}

func TestEventPostAccessMapping(t *testing.T) {
	cases := []struct {
		acc  macos.EventPostAccess
		want macos.CapabilityState
	}{
		{macos.EventPostAccess{Allowed: true}, macos.CapabilityGranted},
		{macos.EventPostAccess{Allowed: false}, macos.CapabilityNotGranted},
		{macos.EventPostAccess{Err: errors.New("query failed")}, macos.CapabilityUnknown},
	}
	for _, c := range cases {
		if got := c.acc.State(); got != c.want {
			t.Fatalf("access %+v mapped to %s", c.acc, got)
		}
	}
}

func TestScreenRecordingEvidenceCombinations(t *testing.T) {
	// Everything worked: available.
	e := macos.ScreenRecordingEvidence{
		OtherProcessWindows: 12,
		StreamStarted:       true,
		FirstFrameComplete:  true,
		FirstFrameHasPixels: true,
	}
	if a := e.Assessment(); a.State != macos.ScreenRecordingAvailable {
		t.Fatalf("complete evidence assessed %s", a.State)
	}
	// Enumeration error: unavailable with the stage and error.
	e = macos.ScreenRecordingEvidence{EnumerationErr: errors.New("no permission")}
	a := e.Assessment()
	if a.State != macos.ScreenRecordingUnavailable || a.FailedStage != macos.StageEnumeration || a.Err == nil {
		t.Fatalf("enumeration failure assessed %+v", a)
	}
	// Enumeration fine but only own windows: unavailable (permission
	// likely missing), still not a permission claim.
	e = macos.ScreenRecordingEvidence{}
	a = e.Assessment()
	if a.State != macos.ScreenRecordingUnavailable || a.FailedStage != macos.StageEnumeration {
		t.Fatalf("own-windows-only assessed %+v", a)
	}
	// Stream start failed: unavailable at that stage.
	e = macos.ScreenRecordingEvidence{OtherProcessWindows: 12, StreamStartErr: errors.New("start failed")}
	a = e.Assessment()
	if a.State != macos.ScreenRecordingUnavailable || a.FailedStage != macos.StageStreamStart {
		t.Fatalf("stream failure assessed %+v", a)
	}
	// Enumeration fine, stream started, but no first frame yet:
	// unknown, not available and not unavailable.
	e = macos.ScreenRecordingEvidence{OtherProcessWindows: 12, StreamStarted: true}
	if a := e.Assessment(); a.State != macos.ScreenRecordingUnknown {
		t.Fatalf("incomplete evidence assessed %s", a.State)
	}
	// First frame without pixels: still unknown.
	e.FirstFrameComplete = true
	if a := e.Assessment(); a.State != macos.ScreenRecordingUnknown {
		t.Fatalf("frameless evidence assessed %s", a.State)
	}
}
