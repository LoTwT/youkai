package app

import (
	"strings"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/game/zzz/collect"
	"github.com/LoTwT/youkai/internal/platform"
	"github.com/LoTwT/youkai/internal/platform/platformfake"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/LoTwT/youkai/internal/scan"
)

// discoveringSource wires a GameScanSource without a fixed target over a
// fake port that discovers candidates — the installed Windows source's
// shape (P-01).
func discoveringSource(candidates []platform.TargetCandidate) (*GameScanSource, *platformfake.Port) {
	port := &platformfake.Port{DiscoverFn: func() ([]platform.TargetCandidate, error) {
		return candidates, nil
	}}
	return &GameScanSource{
		Port:   port,
		Hotkey: platform.HotkeySpec{Key: platform.KeyS},
		Client: record.ClientWindowsLocal,
	}, port
}

// P-01: a source without a fixed target never starts on its own; the user
// must confirm one discovered window, even when only one is running.
func TestGameScanSourceRequiresConfirmedTarget(t *testing.T) {
	started := time.Date(2026, 11, 2, 8, 0, 0, 0, time.UTC)
	candidates := []platform.TargetCandidate{{
		Handle: 0xB0ABE, PID: 42, ExecutablePath: `C:\Games\ZZZ\ZenlessZoneZero.exe`,
		ProcessStartedAt: started, ClassName: "UnityWndClass",
	}}
	source, _ := discoveringSource(candidates)
	if !source.RequiresTargetConfirmation() {
		t.Fatal("a source without a fixed target must require confirmation")
	}
	if _, err := source.NewScan(); err == nil || !strings.Contains(err.Error(), "confirmed") {
		t.Fatalf("NewScan without a confirmed target: %v", err)
	}
	got, err := source.DiscoverTargets()
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(got) != 1 || got[0].Handle != candidates[0].Handle {
		t.Fatalf("discovered %+v, want the fake candidate", got)
	}
}

// The confirmed candidate becomes the fixed path plus the handle hint the
// platform locks; a candidate that vanished or was replaced refuses the
// start instead of locking another window.
func TestGameScanSourceConfirmedTargetLocksPathAndHandle(t *testing.T) {
	started := time.Date(2026, 11, 2, 8, 0, 0, 0, time.UTC)
	first := platform.TargetCandidate{
		Handle: 1, PID: 42, ExecutablePath: `C:\Games\ZZZ\ZenlessZoneZero.exe`,
		ProcessStartedAt: started, ClassName: "UnityWndClass",
	}
	second := platform.TargetCandidate{
		Handle: 2, PID: 77, ExecutablePath: `D:\Other\ZenlessZoneZero.exe`,
		ProcessStartedAt: started, ClassName: "UnityWndClass",
	}
	source, _ := discoveringSource([]platform.TargetCandidate{first, second})
	spec, err := source.NewScanForTarget(second)
	if err != nil {
		t.Fatalf("NewScanForTarget: %v", err)
	}
	ctrl, ok := spec.Controller.(*collect.Controller)
	if !ok {
		t.Fatalf("controller = %T", spec.Controller)
	}
	if ctrl.Target.ExecutablePath != second.ExecutablePath || ctrl.Target.HandleHint != second.Handle {
		t.Fatalf("controller target = %+v, want the confirmed path and handle", ctrl.Target)
	}

	source, _ = discoveringSource(nil)
	if _, err := source.NewScanForTarget(first); err == nil ||
		!strings.Contains(err.Error(), "gone or was replaced") {
		t.Fatalf("a vanished candidate must refuse the start, got %v", err)
	}

	// The same handle but a restarted process is not the confirmed window.
	restarted := first
	restarted.ProcessStartedAt = started.Add(time.Hour)
	source, _ = discoveringSource([]platform.TargetCandidate{restarted})
	if _, err := source.NewScanForTarget(first); err == nil {
		t.Fatal("a restarted process reusing the handle must refuse the start")
	}
}

// The confirmed candidate reaches the session's target through the service
// and its start request.
func TestGameScanSourceConfirmedTargetReachesTheSession(t *testing.T) {
	started := time.Date(2026, 11, 2, 8, 0, 0, 0, time.UTC)
	cand := platform.TargetCandidate{
		Handle: 0xB0ABE, PID: 42, ExecutablePath: `C:\Games\ZZZ\ZenlessZoneZero.exe`,
		ProcessStartedAt: started, ClassName: "UnityWndClass",
	}
	sess := platformfake.NewSession()
	port := &platformfake.Port{
		NewSession: func() *platformfake.Session { return sess },
		DiscoverFn: func() ([]platform.TargetCandidate, error) {
			return []platform.TargetCandidate{cand}, nil
		},
	}
	source := &GameScanSource{
		Port:   port,
		Hotkey: platform.HotkeySpec{Key: platform.KeyS},
		Client: record.ClientWindowsLocal,
	}
	svc, _ := newTestService(t, newTestPaths(t), source, func() (Recognizer, error) {
		return newFakeRecognizer(), nil
	})

	if _, err := svc.Start(StartRequest{ID: "no-target"}); err == nil ||
		!strings.Contains(err.Error(), "confirmed") {
		t.Fatalf("a discovery source must refuse a start without a target, got %v", err)
	}
	if port.DiscoveryCount() != 0 {
		t.Fatalf("a refused start enumerated %d times", port.DiscoveryCount())
	}

	out, err := svc.Start(StartRequest{ID: "confirmed-target", Target: &cand})
	if err != nil {
		t.Fatalf("start with a confirmed target: %v", err)
	}
	if out.SessionID == "" {
		t.Fatal("the start returned no session")
	}
	waitSnapshot(t, svc, scan.PhaseCapturing, "the confirmed-target session to capture")
	cfg := port.LastConfig()
	if cfg == nil || cfg.Target.ExecutablePath != cand.ExecutablePath || cfg.Target.HandleHint != cand.Handle {
		t.Fatalf("the session target = %+v, want the confirmed path and handle", cfg)
	}
	if port.DiscoveryCount() != 1 {
		t.Fatalf("discovery runs = %d, want one re-check", port.DiscoveryCount())
	}
	sess.TriggerHotkey()
	waitWrapped(t, svc)
	if final := svc.Snapshot().Scan; final.End != scan.EndCancelled {
		t.Fatalf("end %s, want cancelled", final.End)
	}
}
