package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/game/zzz/collect"
	"github.com/LoTwT/youkai/internal/platform"
	"github.com/LoTwT/youkai/internal/platform/platformfake"
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

// T46: no environment variable keeps the production default: no scan
// source at all.
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

// T48: traverse has a calibrated layout only on darwin; every other
// platform is refused before the host or the Windows target env is read.
func TestScanSourceTraverseRejectedOutsideDarwin(t *testing.T) {
	for _, goos := range []string{"windows", "linux"} {
		source, _, err := scanSourceFrom(envOf(map[string]string{
			"YOUKAI_GAME_SOURCE": "traverse",
		}), goos, noHost(t))
		if err == nil || !strings.Contains(err.Error(), "calibrated layout") {
			t.Fatalf("%s: err = %v, want the calibrated-layout refusal", goos, err)
		}
		if source != nil {
			t.Fatalf("%s: source = %T, want none", goos, source)
		}
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
	if inv.IndependentClear {
		t.Fatal("the default traverse must keep the daily one-visit filter preparation")
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
}

// T49b: the acceptance-only independent Clear switch is wired into every
// traverse session's Inventory, and any non-empty value enables it.
func TestScanSourceTraverseWiresIndependentClear(t *testing.T) {
	host := func() (platform.Port, error) { return &platformfake.Port{}, nil }
	source, _, err := scanSourceFrom(envOf(map[string]string{
		"YOUKAI_GAME_SOURCE":       "traverse",
		"YOUKAI_INDEPENDENT_CLEAR": "1",
	}), "darwin", host)
	if err != nil {
		t.Fatalf("scanSourceFrom: %v", err)
	}
	game := source.(*app.GameScanSource)
	inv, ok := game.Traversal().(*collect.Inventory)
	if !ok {
		t.Fatalf("traversal = %T, want *collect.Inventory", game.Traversal())
	}
	if !inv.IndependentClear {
		t.Fatal("YOUKAI_INDEPENDENT_CLEAR did not enable the independent Clear check")
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
