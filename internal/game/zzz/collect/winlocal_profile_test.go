package collect_test

import (
	"image"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/game/zzz/collect"
	"github.com/LoTwT/youkai/internal/platform"
	"github.com/LoTwT/youkai/internal/platform/platformfake"
	"github.com/LoTwT/youkai/internal/scan"
)

// W2c profile tests: the Windows local traversal's layout and thresholds
// are generated from the measured client-area tier after the platform
// precheck accepted it, and the profile keeps every uncalibrated timing
// value in one place.

// TestWindowsLocalProfileBuildsScaledReference pins the layout selection:
// the 1920×1080 tier yields exactly ScaledLayout(ReferenceLayout(), t) and
// ScaledCalibration(ReferenceCalibration(), s), with the macOS timing as
// the documented temporary initial value.
func TestWindowsLocalProfileBuildsScaledReference(t *testing.T) {
	client := image.Pt(1920, 1080)
	profile, err := collect.WindowsLocalProfileFor(client)
	if err != nil {
		t.Fatalf("WindowsLocalProfileFor: %v", err)
	}
	if profile.Tier.Client != client {
		t.Fatalf("tier = %v, want %v", profile.Tier.Client, client)
	}
	transform, err := collect.ClientRectTransform(client)
	if err != nil {
		t.Fatal(err)
	}
	if profile.Transform != transform {
		t.Fatalf("transform = %+v, want %+v", profile.Transform, transform)
	}
	wantLayout, err := collect.ScaledLayout(collect.ReferenceLayout(), transform)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(profile.Layout, wantLayout) {
		t.Fatal("the profile layout is not ScaledLayout(ReferenceLayout(), clientRect transform)")
	}
	wantCalibration, err := collect.ScaledCalibration(collect.ReferenceCalibration(), transform.Scale)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(profile.Calibration, wantCalibration) {
		t.Fatal("the profile calibration is not ScaledCalibration(ReferenceCalibration(), scale)")
	}
	mac := collect.MacCloudLayout()
	if profile.ScrollTiming != collect.MacCloudScrollTiming() {
		t.Fatalf("scroll timing = %+v, want the uncalibrated macOS profile", profile.ScrollTiming)
	}
	if profile.ScrollDuration != mac.ScrollDuration || profile.TopDuration != mac.TopDuration {
		t.Fatalf("drag durations = %s/%s, want the uncalibrated macOS values %s/%s",
			profile.ScrollDuration, profile.TopDuration, mac.ScrollDuration, mac.TopDuration)
	}
	// The profile has no sort-label template: coverage must stay partial.
	if !profile.Layout.SortLabel.Empty() || profile.Layout.SortTemplate != nil {
		t.Fatal("the Windows profile claims a sort template it does not have")
	}
	diag := profile.Diagnostic()
	for _, want := range []string{"tier=1920x1080", "scale=1.0000", "profile=", "uncalibrated"} {
		if !strings.Contains(diag, want) {
			t.Fatalf("diagnostic %q does not contain %q", diag, want)
		}
	}
}

// TestWindowsLocalProfileRefusesUnopenedTiers: a known-but-unopened tier
// and an unknown size are both refused, and the refusal names the open
// tier. Listing a tier in the reference table does not open it (D-2).
func TestWindowsLocalProfileRefusesUnopenedTiers(t *testing.T) {
	known, err := collect.WindowsLocalProfileFor(image.Pt(1280, 720))
	if err == nil {
		t.Fatalf("the known-but-unopened 1280x720 tier produced profile %+v", known)
	}
	if !strings.Contains(err.Error(), "known 16:9 tier") || !strings.Contains(err.Error(), "1920x1080") {
		t.Fatalf("err = %v, want the known-but-unopened refusal naming 1920x1080", err)
	}
	unknown, err := collect.WindowsLocalProfileFor(image.Pt(1000, 1000))
	if err == nil {
		t.Fatalf("the unknown 1000x1000 size produced profile %+v", unknown)
	}
	if !strings.Contains(err.Error(), "not a known 16:9 tier") || !strings.Contains(err.Error(), "1920x1080") {
		t.Fatalf("err = %v, want the unknown-size refusal naming 1920x1080", err)
	}
}

// TestWindowsLocalProfileMatcherOn1920Fixture is the W2c smoke test: the
// matcher the wiring builds on the 1920×1080 tier reads the W2a Windows
// fixture with the expected page, occupancy, selection, wheel and funnel
// verdicts. It proves the wiring, not field acceptance.
func TestWindowsLocalProfileMatcherOn1920Fixture(t *testing.T) {
	frame := loadWinMaskedFrame(t, "win-1920x1080")
	profile, err := collect.WindowsLocalProfileFor(image.Pt(1920, 1080))
	if err != nil {
		t.Fatalf("WindowsLocalProfileFor: %v", err)
	}
	inv := profile.Inventory(false)
	if !inv.Layout.MatchesFrame(frame.Image.Bounds().Size(), frame.Content) {
		t.Fatalf("the wired layout does not identify the 1920x1080 fixture: image=%v content=%v layout=%dx%d/%v",
			frame.Image.Bounds().Size(), frame.Content, inv.Layout.Image.X, inv.Layout.Image.Y, inv.Layout.Content)
	}
	pm := inv.Matcher.Match(frame)
	if pm.Page != collect.PageWarehouse {
		t.Fatalf("page %s, want warehouse (%s)", pm.Page, pm.Evidence)
	}
	if pm.List == nil || pm.Wheel == nil || pm.Filter == nil {
		t.Fatalf("warehouse frame lacks list/wheel/filter evidence: %+v", pm)
	}
	if len(pm.List.Occupied) != 20 {
		t.Fatalf("%d occupancy slots, want 20", len(pm.List.Occupied))
	}
	for i, occupied := range pm.List.Occupied {
		if want := i < 13; occupied != want {
			t.Fatalf("slot %d occupied=%v, want %v", i, occupied, want)
		}
	}
	if pm.List.Selected != 0 {
		t.Fatalf("selected %d, want the first visible item (slot 0)", pm.List.Selected)
	}
	if pm.Wheel.Position != 3 {
		t.Fatalf("wheel %d, want position 3", pm.Wheel.Position)
	}
	if pm.Filter.State != collect.FilterActive {
		t.Fatalf("filter %s, want active", pm.Filter.State)
	}
}

// TestWindowsTraversalSelectedAfterBaselineAndBeforePrecheck drives the
// real controller and scan kernel with a fake platform session: the
// client-area factory must run exactly once, after LockBaseline accepted
// the tier, and must build an Inventory from that measured size.
func TestWindowsTraversalSelectedAfterBaselineAndBeforePrecheck(t *testing.T) {
	sess := platformfake.NewSession()
	sess.ClientAreaSize = image.Pt(1920, 1080)
	port := &platformfake.Port{NewSession: func() *platformfake.Session { return sess }}

	var (
		mu          sync.Mutex
		calls       int
		clientSize  image.Point
		locksAtCall int
		built       *collect.Inventory
	)
	ctrl := &collect.Controller{
		Port:   port,
		Target: platform.TargetSpec{ExecutablePath: `C:\Games\ZenlessZoneZero.exe`},
		Hotkey: platform.HotkeySpec{Key: platform.KeyS, Ctrl: true, Alt: true, Shift: true},
		TraversalForClient: func(client image.Point) (collect.Traversal, error) {
			_, locks, _, _, _ := sess.Counters()
			profile, err := collect.WindowsLocalProfileFor(client)
			if err != nil {
				return nil, err
			}
			inv := profile.Inventory(false)
			mu.Lock()
			calls++
			clientSize, locksAtCall, built = client, locks, inv
			mu.Unlock()
			return inv, nil
		},
		Stop:         func() {},
		ObserveEvery: time.Millisecond,
	}
	kernel, err := scan.Start(scan.Config{
		ID:         "windows-profile-flow",
		Capacity:   1,
		Workers:    1,
		Controller: ctrl,
		Recognizer: waitRecognizer{},
		Deliverer:  &recordSink{},
	})
	if err != nil {
		t.Fatalf("scan.Start: %v", err)
	}
	select {
	case <-kernel.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the session to end")
	}

	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Fatalf("the client-area factory ran %d times, want once", calls)
	}
	if locksAtCall != 1 {
		t.Fatalf("the factory ran with %d LockBaseline calls, want the precheck's 1", locksAtCall)
	}
	if clientSize != image.Pt(1920, 1080) {
		t.Fatalf("the factory received %v, want the measured 1920x1080 client area", clientSize)
	}
	if built == nil {
		t.Fatal("the factory did not build an Inventory")
	}
	if built.Layout.Image != image.Pt(1920, 1080) {
		t.Fatalf("the Inventory layout image = %v, want the measured 1920x1080", built.Layout.Image)
	}
	if built.Matcher == nil {
		t.Fatal("the Inventory has no matcher")
	}
	// The fake's 4×4 frame cannot pass the page precheck; that failure is
	// after the profile was selected and proves Precheck ran on the
	// Inventory rather than being skipped.
	if end := kernel.Snapshot().End; end != scan.EndFailed {
		t.Fatalf("session end = %s, want failed on the fake frame", end)
	}
}

// TestWindowsTraversalRefusesUnopenedTierBeforeInventory: a measured
// 1280×720 tier reaches the factory (the fake session bypasses the
// platform precheck) and must be refused there; no Inventory is built.
func TestWindowsTraversalRefusesUnopenedTierBeforeInventory(t *testing.T) {
	sess := platformfake.NewSession()
	sess.ClientAreaSize = image.Pt(1280, 720)
	port := &platformfake.Port{NewSession: func() *platformfake.Session { return sess }}

	var (
		mu         sync.Mutex
		calls      int
		built      int
		factoryErr error
	)
	ctrl := &collect.Controller{
		Port:   port,
		Target: platform.TargetSpec{ExecutablePath: `C:\Games\ZenlessZoneZero.exe`},
		Hotkey: platform.HotkeySpec{Key: platform.KeyS, Ctrl: true, Alt: true, Shift: true},
		TraversalForClient: func(client image.Point) (collect.Traversal, error) {
			profile, err := collect.WindowsLocalProfileFor(client)
			mu.Lock()
			calls++
			if err != nil {
				factoryErr = err
			} else {
				built++
			}
			mu.Unlock()
			if err != nil {
				return nil, err
			}
			return profile.Inventory(false), nil
		},
		Stop:         func() {},
		ObserveEvery: time.Millisecond,
	}
	kernel, err := scan.Start(scan.Config{
		ID:         "windows-profile-unopened",
		Capacity:   1,
		Workers:    1,
		Controller: ctrl,
		Recognizer: waitRecognizer{},
		Deliverer:  &recordSink{},
	})
	if err != nil {
		t.Fatalf("scan.Start: %v", err)
	}
	select {
	case <-kernel.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the session to end")
	}

	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Fatalf("the client-area factory ran %d times, want once", calls)
	}
	if built != 0 {
		t.Fatalf("an Inventory was built for an unopened tier (%d times)", built)
	}
	if factoryErr == nil {
		t.Fatal("the unopened tier was not refused")
	}
	if !strings.Contains(factoryErr.Error(), "known 16:9 tier") {
		t.Fatalf("err = %v, want the known-but-unopened refusal", factoryErr)
	}
	if end := kernel.Snapshot().End; end != scan.EndFailed {
		t.Fatalf("session end = %s, want failed", end)
	}
}
