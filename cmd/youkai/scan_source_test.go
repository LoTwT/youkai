package main

import (
	"encoding/json"
	"image"
	"reflect"
	"strings"
	"testing"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/game/zzz/collect"
	"github.com/LoTwT/youkai/internal/platform"
	"github.com/LoTwT/youkai/internal/platform/platformfake"
	"github.com/LoTwT/youkai/internal/record"
)

// envOf builds a getenv function over a fixed map.
func envOf(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

// noHost fails the test if the wiring asks for a platform it should have
// refused before touching the host.
func noHost(t *testing.T) func() (platform.Port, error) {
	t.Helper()
	return func() (platform.Port, error) {
		t.Fatal("the wiring resolved the platform host for a refused source")
		return nil, nil
	}
}

// T46: on macOS no environment variable keeps the production default: no
// scan source at all (the installed Windows default is tested below).
func TestScanSourceUnsetHasNoSource(t *testing.T) {
	source, label, err := scanSourceFrom(envOf(nil), "darwin", noHost(t))
	if err != nil {
		t.Fatalf("scanSourceFrom: %v", err)
	}
	if source != nil {
		t.Fatalf("source = %T, want none", source)
	}
	if label != "开发数据源" {
		t.Fatalf("label = %q", label)
	}
}

// T47: both source switches at once is a wiring error.
func TestScanSourceRejectsBothSwitches(t *testing.T) {
	_, _, err := scanSourceFrom(envOf(map[string]string{
		"YOUKAI_DEV_SOURCE":  "/tmp/fixture",
		"YOUKAI_GAME_SOURCE": "observe",
	}), "darwin", noHost(t))
	if err == nil || !strings.Contains(err.Error(), "pick one scan source") {
		t.Fatalf("err = %v, want the both-switches refusal", err)
	}
}

// The installed Windows default (D-01, P-01): with no environment variable
// the local client is wired without a fixed target, so the window lists the
// discovered candidates and a start must carry a confirmed one.
func TestScanSourceWindowsDefaultDiscoversTarget(t *testing.T) {
	host := func() (platform.Port, error) { return &platformfake.Port{}, nil }
	source, label, err := scanSourceFrom(envOf(nil), "windows", host)
	if err != nil {
		t.Fatalf("scanSourceFrom: %v", err)
	}
	game, ok := source.(*app.GameScanSource)
	if !ok {
		t.Fatalf("source = %T, want *app.GameScanSource", source)
	}
	if !game.RequiresTargetConfirmation() {
		t.Fatal("the installed source must require a confirmed target")
	}
	if game.Target.ExecutablePath != "" || game.Target.BundleID != "" {
		t.Fatalf("the installed source has a fixed target: %+v", game.Target)
	}
	if game.Mode != "traverse" || game.Client != record.ClientWindowsLocal {
		t.Fatalf("mode=%q client=%q, want traverse/windowsLocal", game.Mode, game.Client)
	}
	if game.TraversalForClient == nil {
		t.Fatal("the installed source has no client-area traversal factory")
	}
	if !strings.Contains(label, "自动遍历") {
		t.Fatalf("label = %q", label)
	}
}
func TestScanSourceTraverseRejectedOutsideDarwinAndWindows(t *testing.T) {
	source, _, err := scanSourceFrom(envOf(map[string]string{
		"YOUKAI_GAME_SOURCE": "traverse",
	}), "linux", noHost(t))
	if err == nil || !strings.Contains(err.Error(), "calibrated layout") {
		t.Fatalf("err = %v, want the calibrated-layout refusal", err)
	}
	if source != nil {
		t.Fatalf("source = %T, want none", source)
	}
}

// T49: traverse on darwin wires the calibrated inventory with the app
// projection, and the declared scope names the mode.
func TestScanSourceTraverseWiresOnDarwin(t *testing.T) {
	host := func() (platform.Port, error) { return &platformfake.Port{}, nil }
	source, label, err := scanSourceFrom(envOf(map[string]string{
		"YOUKAI_GAME_SOURCE": "traverse",
	}), "darwin", host)
	if err != nil {
		t.Fatalf("scanSourceFrom: %v", err)
	}
	game, ok := source.(*app.GameScanSource)
	if !ok {
		t.Fatalf("source = %T, want *app.GameScanSource", source)
	}
	if game.Mode != "traverse" {
		t.Fatalf("game source mode %q, want traverse", game.Mode)
	}
	if game.Traversal == nil {
		t.Fatal("the traverse source has no traversal factory")
	}
	if !strings.Contains(label, "自动遍历") {
		t.Fatalf("label = %q", label)
	}
	inv, ok := game.Traversal().(*collect.Inventory)
	if !ok {
		t.Fatalf("traversal = %T, want *collect.Inventory", game.Traversal())
	}
	if inv.Project == nil {
		t.Fatal("the inventory has no projection")
	}
	if inv.Matcher == nil {
		t.Fatal("the inventory has no matcher")
	}
	// The macOS construction must stay exactly the pre-W2c wiring: the
	// fixed cloud layout and calibration, the stage-2 matcher and the
	// default (nil) scroll timing, not a Windows clientRect profile.
	if !reflect.DeepEqual(inv.Layout, collect.MacCloudLayout()) {
		t.Fatal("the macOS traversal no longer uses MacCloudLayout()")
	}
	if inv.ScrollTiming != nil {
		t.Fatalf("the macOS traversal carries scroll timing %+v, want the default profile", *inv.ScrollTiming)
	}
	got, ok := inv.Matcher.(*collect.ScrollEvidenceMatcher)
	if !ok {
		t.Fatalf("matcher = %T, want *collect.ScrollEvidenceMatcher", inv.Matcher)
	}
	if !reflect.DeepEqual(got.C, collect.MacCloudCalibration()) {
		t.Fatal("the macOS traversal no longer uses MacCloudCalibration()")
	}
	spec, err := game.NewScan()
	if err != nil {
		t.Fatalf("NewScan: %v", err)
	}
	var scope struct {
		Kind string `json:"kind"`
		Mode string `json:"mode"`
	}
	if err := json.Unmarshal(spec.Scope, &scope); err != nil {
		t.Fatalf("scope: %v", err)
	}
	if scope.Kind != "zzzInventory" || scope.Mode != "traverse" {
		t.Fatalf("scope = %+v, want zzzInventory/traverse", scope)
	}
	if spec.Client != record.ClientMacOSCloud {
		t.Fatalf("client = %q, want macosCloud", spec.Client)
	}
}

// The macOS traverse keeps the legacy grayscale-MAD matcher as a field
// fallback; W2c must not change that choice.
func TestScanSourceDarwinKeepsLegacyMatcher(t *testing.T) {
	host := func() (platform.Port, error) { return &platformfake.Port{}, nil }
	source, _, err := scanSourceFrom(envOf(map[string]string{
		"YOUKAI_GAME_SOURCE":    "traverse",
		"YOUKAI_SCROLL_MATCHER": "legacy",
	}), "darwin", host)
	if err != nil {
		t.Fatalf("scanSourceFrom: %v", err)
	}
	inv := source.(*app.GameScanSource).Traversal().(*collect.Inventory)
	if _, ok := inv.Matcher.(*collect.PixelMatcher); !ok {
		t.Fatalf("matcher = %T, want *collect.PixelMatcher", inv.Matcher)
	}
}

// W2c-1: Windows local + traverse wires a client-area traversal factory
// instead of a pre-built layout: the profile is selected after the
// platform precheck measured the client area, so the source itself must
// not construct an Inventory.
func TestScanSourceTraverseWiresWindowsLocal(t *testing.T) {
	host := func() (platform.Port, error) { return &platformfake.Port{}, nil }
	env := envOf(map[string]string{
		"YOUKAI_GAME_SOURCE": "traverse",
		"YOUKAI_GAME_CLIENT": "local",
		"YOUKAI_GAME_EXE":    `C:\Games\ZenlessZoneZero.exe`,
	})
	source, label, err := scanSourceFrom(env, "windows", host)
	if err != nil {
		t.Fatalf("scanSourceFrom: %v", err)
	}
	game, ok := source.(*app.GameScanSource)
	if !ok {
		t.Fatalf("source = %T, want *app.GameScanSource", source)
	}
	if game.Mode != "traverse" {
		t.Fatalf("game source mode %q, want traverse", game.Mode)
	}
	if game.Client != record.ClientWindowsLocal {
		t.Fatalf("client = %q, want windowsLocal", game.Client)
	}
	if game.Traversal != nil {
		t.Fatal("the Windows local source must select its layout from the measured client area, not build one eagerly")
	}
	if game.TraversalForClient == nil {
		t.Fatal("the Windows local source has no client-area traversal factory")
	}
	if !strings.Contains(label, "自动遍历") {
		t.Fatalf("label = %q", label)
	}

	// The measured 1920×1080 tier builds the scaled reference profile,
	// with the reference layout's scale and the profile's timing.
	trav, err := game.TraversalForClient(image.Pt(1920, 1080))
	if err != nil {
		t.Fatalf("TraversalForClient: %v", err)
	}
	inv, ok := trav.(*collect.Inventory)
	if !ok {
		t.Fatalf("traversal = %T, want *collect.Inventory", trav)
	}
	transform, err := collect.ClientRectTransform(image.Pt(1920, 1080))
	if err != nil {
		t.Fatal(err)
	}
	wantLayout, err := collect.ScaledLayout(collect.ReferenceLayout(), transform)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(inv.Layout, wantLayout) {
		t.Fatal("the Windows traversal does not use ScaledLayout(ReferenceLayout(), 1920x1080)")
	}
	if inv.Project == nil {
		t.Fatal("the Windows inventory has no projection")
	}
	if _, ok := inv.Matcher.(*collect.ScrollEvidenceMatcher); !ok {
		t.Fatalf("matcher = %T, want *collect.ScrollEvidenceMatcher", inv.Matcher)
	}
	if inv.ScrollTiming == nil {
		t.Fatal("the Windows inventory has no scroll timing profile")
	}

	// NewScan must carry the factory into the controller; dropping it
	// there would silently fall back to the observe-only traversal.
	spec, err := game.NewScan()
	if err != nil {
		t.Fatalf("NewScan: %v", err)
	}
	ctrl, ok := spec.Controller.(*collect.Controller)
	if !ok {
		t.Fatalf("controller = %T, want *collect.Controller", spec.Controller)
	}
	if ctrl.TraversalForClient == nil {
		t.Fatal("NewScan dropped the Windows client-area traversal factory")
	}
	if spec.Client != record.ClientWindowsLocal {
		t.Fatalf("spec client = %q, want windowsLocal", spec.Client)
	}
}

// W2c-2: the Windows cloud client's traverse is refused with its reason:
// its video rectangle would need the measuredVideoRect transform, which
// exists only as an interface.
func TestScanSourceTraverseRejectsWindowsCloud(t *testing.T) {
	host := func() (platform.Port, error) { return &platformfake.Port{}, nil }
	source, _, err := scanSourceFrom(envOf(map[string]string{
		"YOUKAI_GAME_SOURCE": "traverse",
		"YOUKAI_GAME_CLIENT": "cloud",
		"YOUKAI_GAME_EXE":    `C:\Games\CloudZZZ.exe`,
	}), "windows", host)
	if err == nil {
		t.Fatal("cloud traverse was accepted")
	}
	for _, want := range []string{"local client", "measuredVideoRect", "implemented"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("err = %v, want %q in the refusal", err, want)
		}
	}
	if source != nil {
		t.Fatalf("source = %T, want none", source)
	}
}

// W2c-3: the Windows target identity stays mandatory; without the exe the
// traverse cannot be wired.
func TestScanSourceTraverseRequiresWindowsExe(t *testing.T) {
	host := func() (platform.Port, error) { return &platformfake.Port{}, nil }
	source, _, err := scanSourceFrom(envOf(map[string]string{
		"YOUKAI_GAME_SOURCE": "traverse",
		"YOUKAI_GAME_CLIENT": "local",
	}), "windows", host)
	if err == nil || !strings.Contains(err.Error(), "YOUKAI_GAME_EXE") {
		t.Fatalf("err = %v, want the missing-exe refusal", err)
	}
	if source != nil {
		t.Fatalf("source = %T, want none", source)
	}
}

// W2c-4: observe on Windows keeps the no-input rehearsal, including the
// cloud client, and does not gain a traversal.
func TestScanSourceWindowsObserveStaysObserveOnly(t *testing.T) {
	for _, kind := range []string{"local", "cloud"} {
		host := func() (platform.Port, error) { return &platformfake.Port{}, nil }
		source, label, err := scanSourceFrom(envOf(map[string]string{
			"YOUKAI_GAME_SOURCE": "observe",
			"YOUKAI_GAME_CLIENT": kind,
			"YOUKAI_GAME_EXE":    `C:\Games\game.exe`,
		}), "windows", host)
		if err != nil {
			t.Fatalf("%s: scanSourceFrom: %v", kind, err)
		}
		game := source.(*app.GameScanSource)
		if game.Mode != "observeOnly" || game.Traversal != nil || game.TraversalForClient != nil {
			t.Fatalf("%s: observe source was changed: mode=%q traversal=%v clientTraversal=%v",
				kind, game.Mode, game.Traversal != nil, game.TraversalForClient != nil)
		}
		if !strings.Contains(label, "仅观测") {
			t.Fatalf("%s: label = %q", kind, label)
		}
	}
}

// W2c-5: the failure-crop directory reaches a Windows traversal built
// later from the measured client area, not only the eager macOS one.
func TestWithDiagnosticDirWiresWindowsClientTraversals(t *testing.T) {
	host := func() (platform.Port, error) { return &platformfake.Port{}, nil }
	source, _, err := scanSourceFrom(envOf(map[string]string{
		"YOUKAI_GAME_SOURCE": "traverse",
		"YOUKAI_GAME_CLIENT": "local",
		"YOUKAI_GAME_EXE":    `C:\Games\ZenlessZoneZero.exe`,
	}), "windows", host)
	if err != nil {
		t.Fatalf("scanSourceFrom: %v", err)
	}
	dir := t.TempDir()
	withDiagnosticDir(source, dir)
	game := source.(*app.GameScanSource)
	for i := 0; i < 2; i++ {
		trav, err := game.TraversalForClient(image.Pt(1920, 1080))
		if err != nil {
			t.Fatalf("TraversalForClient %d: %v", i+1, err)
		}
		inv, ok := trav.(*collect.Inventory)
		if !ok {
			t.Fatalf("traversal %d = %T, want *collect.Inventory", i+1, trav)
		}
		if inv.DiagnosticDir != dir {
			t.Fatalf("traversal %d DiagnosticDir = %q, want %q", i+1, inv.DiagnosticDir, dir)
		}
	}
}

// T50: observe keeps the no-input rehearsal and the historical scope mode.
func TestScanSourceObserveStaysObserveOnly(t *testing.T) {
	host := func() (platform.Port, error) { return &platformfake.Port{}, nil }
	source, label, err := scanSourceFrom(envOf(map[string]string{
		"YOUKAI_GAME_SOURCE": "observe",
	}), "darwin", host)
	if err != nil {
		t.Fatalf("scanSourceFrom: %v", err)
	}
	game := source.(*app.GameScanSource)
	if game.Mode != "observeOnly" {
		t.Fatalf("game source mode %q, want observeOnly", game.Mode)
	}
	if game.Traversal != nil {
		t.Fatal("the observe source must not have a traversal factory")
	}
	if !strings.Contains(label, "仅观测") {
		t.Fatalf("label = %q", label)
	}
}

// T51: an unknown game mode is refused with the supported set.
func TestScanSourceUnknownGameMode(t *testing.T) {
	_, _, err := scanSourceFrom(envOf(map[string]string{
		"YOUKAI_GAME_SOURCE": "watch",
	}), "darwin", noHost(t))
	if err == nil || !strings.Contains(err.Error(), "observe") || !strings.Contains(err.Error(), "traverse") {
		t.Fatalf("err = %v, want the supported-mode refusal", err)
	}
}

// withDiagnosticDir points every traversal the traverse source builds at
// the failure-crop directory, including later sessions that call the
// factory again.
func TestWithDiagnosticDirWiresTraverseInventories(t *testing.T) {
	host := func() (platform.Port, error) { return &platformfake.Port{}, nil }
	source, _, err := scanSourceFrom(envOf(map[string]string{
		"YOUKAI_GAME_SOURCE": "traverse",
	}), "darwin", host)
	if err != nil {
		t.Fatalf("scanSourceFrom: %v", err)
	}
	dir := t.TempDir()
	withDiagnosticDir(source, dir)
	game := source.(*app.GameScanSource)
	for i := 0; i < 2; i++ {
		trav := game.Traversal()
		inv, ok := trav.(*collect.Inventory)
		if !ok {
			t.Fatalf("traversal %d = %T, want *collect.Inventory", i+1, trav)
		}
		if inv.DiagnosticDir != dir {
			t.Fatalf("traversal %d DiagnosticDir = %q, want %q", i+1, inv.DiagnosticDir, dir)
		}
	}
}

// An observe-only source has no traversal to configure, and an empty
// directory keeps the traverse source on its default (dumps disabled).
func TestWithDiagnosticDirLeavesObserveAndEmptyUnchanged(t *testing.T) {
	host := func() (platform.Port, error) { return &platformfake.Port{}, nil }

	observe, _, err := scanSourceFrom(envOf(map[string]string{
		"YOUKAI_GAME_SOURCE": "observe",
	}), "darwin", host)
	if err != nil {
		t.Fatalf("scanSourceFrom observe: %v", err)
	}
	withDiagnosticDir(observe, t.TempDir())
	if game := observe.(*app.GameScanSource); game.Traversal != nil {
		t.Fatal("withDiagnosticDir gave the observe source a traversal factory")
	}

	traverse, _, err := scanSourceFrom(envOf(map[string]string{
		"YOUKAI_GAME_SOURCE": "traverse",
	}), "darwin", host)
	if err != nil {
		t.Fatalf("scanSourceFrom traverse: %v", err)
	}
	withDiagnosticDir(traverse, "")
	inv := traverse.(*app.GameScanSource).Traversal().(*collect.Inventory)
	if inv.DiagnosticDir != "" {
		t.Fatalf("DiagnosticDir = %q, want dumps left disabled", inv.DiagnosticDir)
	}
}
