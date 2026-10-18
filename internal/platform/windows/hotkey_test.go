package windows

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestNewHotkeyManagerValidation(t *testing.T) {
	if _, err := NewHotkeyManager(newFakeHotkey(), 1, ModAlt, 0, nil); err == nil {
		t.Fatal("a hotkey without a key must be rejected")
	}
	// A bare non-function key would swallow normal typing.
	if _, err := NewHotkeyManager(newFakeHotkey(), 1, 0, 0x53, nil); err == nil {
		t.Fatal("an unmodified plain key must be rejected")
	}
	// A function key alone is a legitimate global hotkey.
	if _, err := NewHotkeyManager(newFakeHotkey(), 1, 0, 0x74, nil); err != nil {
		t.Fatalf("bare F5 should be accepted: %v", err)
	}
	if _, err := NewHotkeyManager(newFakeHotkey(), 1, ModControl|ModAlt, 0x53, nil); err != nil {
		t.Fatalf("ctrl+alt+S should be accepted: %v", err)
	}
}

func TestHotkeyManagerRegisterConflictSurfacesBeforeStart(t *testing.T) {
	api := newFakeHotkey()
	api.regErr = errors.New("RegisterHotKey: hotkey already registered")
	mgr, err := NewHotkeyManager(api, 1, ModControl|ModAlt, 0x53, func() {})
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	// A registration conflict is returned by Register, before any
	// session starts.
	if err := mgr.Register(); err == nil {
		t.Fatal("the conflict must surface")
	}
	if mgr.State() != HotkeyIdle {
		t.Fatalf("a failed registration leaves the manager idle: %s", mgr.State())
	}
	if api.registered[1] {
		t.Fatal("the fake must not hold a registration")
	}
}

func TestHotkeyManagerLifecycle(t *testing.T) {
	api := newFakeHotkey()
	var mu sync.Mutex
	fired := 0
	mgr, err := NewHotkeyManager(api, 2, ModAlt|ModNoRepeat, 0x53, func() {
		mu.Lock()
		fired++
		mu.Unlock()
	})
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	if err := mgr.Register(); err != nil {
		t.Fatalf("register: %v", err)
	}
	if !api.registered[2] || mgr.State() != HotkeyRegistered {
		t.Fatalf("registration state: registered=%v state=%s", api.registered[2], mgr.State())
	}

	// Two triggers deliver to the stop entry twice.
	if !mgr.DeliverTrigger() || !mgr.DeliverTrigger() {
		t.Fatal("registered deliveries must be forwarded")
	}
	mu.Lock()
	firedNow := fired
	mu.Unlock()
	if firedNow != 2 || mgr.TriggerCount() != 2 {
		t.Fatalf("two deliveries, got fired=%d count=%d", firedNow, mgr.TriggerCount())
	}

	if err := mgr.Unregister(); err != nil {
		t.Fatalf("unregister: %v", err)
	}
	if api.registered[2] {
		t.Fatal("the fake must be unregistered")
	}
	if mgr.State() != HotkeyClosed {
		t.Fatalf("state after unregister: %s", mgr.State())
	}

	// A late delivery is counted, never forwarded.
	if mgr.DeliverTrigger() {
		t.Fatal("a closed manager must not forward triggers")
	}
	mu.Lock()
	firedNow = fired
	mu.Unlock()
	if firedNow != 2 || mgr.LateCount() != 1 {
		t.Fatalf("late delivery must not reach the stop entry: fired=%d late=%d", firedNow, mgr.LateCount())
	}

	// Unregister is idempotent.
	if err := mgr.Unregister(); err != nil {
		t.Fatalf("second unregister: %v", err)
	}
}

func TestHotkeyManagerUnregisterFailureRecorded(t *testing.T) {
	api := newFakeHotkey()
	api.unregErr = errors.New("UnregisterHotKey: failed")
	mgr, _ := NewHotkeyManager(api, 3, ModAlt, 0x70, func() {})
	if err := mgr.Register(); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := mgr.Unregister(); err == nil {
		t.Fatal("the unregistration failure must surface")
	}
	if mgr.State() != HotkeyClosed {
		t.Fatalf("state: %s", mgr.State())
	}
	if mgr.LastError() == nil {
		t.Fatal("the failure must be kept for the report")
	}
}

func TestShutdownAssemblesHonestReport(t *testing.T) {
	// A partially delivered click leaves the button held; the hotkey is
	// registered and fired twice. Shutdown closes permission, releases
	// the button, unregisters the hotkey, and reports each fact.
	api := newFakeHotkey()
	sender := newFakeSender()
	sender.respond = func(call int, events []InputEvent) (int, bool, error) {
		if call == 0 {
			return 2, false, nil // click: move + down, up missing
		}
		return len(events), false, nil
	}
	actor := NewInputActor(sender, nil)
	actor.Permit()
	if res := actor.Dispatch(clickRequest(1, PixelPoint{0, 0})); res.State != StatePartialDelivered {
		t.Fatalf("setup: want partial delivery, got %s", res.State)
	}

	mgr, err := NewHotkeyManager(api, 4, ModAlt, 0x53, func() {})
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	if err := mgr.Register(); err != nil {
		t.Fatalf("register: %v", err)
	}
	mgr.DeliverTrigger()
	mgr.DeliverTrigger()

	at := time.Date(2026, 10, 18, 13, 0, 0, 0, time.UTC)
	report := Shutdown(actor, mgr, "user stop", at)

	if !report.PermissionClosed || report.PermissionCloseReason != "user stop" {
		t.Fatalf("permission close must be reported: %+v", report)
	}
	if !report.StopRequestedAt.Equal(at) {
		t.Fatalf("stop time: %+v", report)
	}
	if len(report.HeldAtStop) != 1 {
		t.Fatalf("the held button must be listed: %+v", report.HeldAtStop)
	}
	if len(report.Releases) != 1 || !report.Releases[0].Released {
		t.Fatalf("the release succeeded: %+v", report.Releases)
	}
	if report.HotkeyState != HotkeyClosed || report.HotkeyTriggers != 2 || report.HotkeyUnregisterErr != "" {
		t.Fatalf("hotkey wrap-up: %+v", report)
	}
	if api.registered[4] {
		t.Fatal("the hotkey must be unregistered")
	}

	// After the shutdown, dispatch is refused and the ledger is empty.
	if res := actor.Dispatch(clickRequest(1, PixelPoint{0, 0})); res.State != StateNotDispatched {
		t.Fatalf("post-shutdown dispatch must be refused, got %s", res.State)
	}
	if held := actor.Held(); len(held) != 0 {
		t.Fatalf("ledger should be empty after the successful release: %+v", held)
	}
}

func TestShutdownRecordsReleaseAndUnregisterFailures(t *testing.T) {
	// Everything fails: the release is blocked, the unregistration fails.
	// The report must say so instead of claiming a clean wrap-up.
	api := newFakeHotkey()
	api.unregErr = errors.New("UnregisterHotKey: failed")
	sender := newFakeSender()
	sender.respond = func(call int, events []InputEvent) (int, bool, error) {
		if call == 0 {
			return 2, false, nil
		}
		return 0, true, nil // release blocked
	}
	actor := NewInputActor(sender, nil)
	actor.Permit()
	actor.Dispatch(clickRequest(1, PixelPoint{0, 0}))
	mgr, _ := NewHotkeyManager(api, 5, ModAlt, 0x53, func() {})
	mgr.Register()

	report := Shutdown(actor, mgr, "fault stop", time.Now())
	if len(report.Releases) != 1 || report.Releases[0].Released {
		t.Fatalf("the blocked release must be reported unreleased: %+v", report.Releases)
	}
	if report.HotkeyUnregisterErr == "" {
		t.Fatal("the unregistration failure must be in the report")
	}
	if report.HotkeyState != HotkeyClosed {
		t.Fatalf("state after a failed unregistration is still closed: %s", report.HotkeyState)
	}
	// The key may still be down; the report keeps it listed.
	if len(report.HeldAtStop) != 1 {
		t.Fatalf("held keys at stop must be listed: %+v", report.HeldAtStop)
	}
}

func TestShutdownWithNilComponents(t *testing.T) {
	report := Shutdown(nil, nil, "user stop", time.Now())
	if report.PermissionClosed || report.HotkeyState != "" || report.HotkeyUnregisterErr != "" {
		t.Fatalf("nil components report no facts claimed: %+v", report)
	}
}
