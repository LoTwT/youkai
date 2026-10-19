package app

import (
	"encoding/json"
	"fmt"

	"github.com/LoTwT/youkai/internal/game/zzz/collect"
	"github.com/LoTwT/youkai/internal/platform"
	"github.com/LoTwT/youkai/internal/record"
)

// scopeRelay is the controller capability that receives the traversal's
// observed-scope relay; collect.Controller implements it (see its
// ScopeReporter). The service wires the record deliverer's sink into it
// before the session starts, so the range the precheck actually
// observed reaches the session's record (T5 §3).
type scopeRelay interface {
	SetScopeSink(func(json.RawMessage))
}

// GameScanSource supplies scans that run against a real game window
// through the unified platform port. It serves two modes: the observe-only
// rehearsal (the session opens the platform, resolves and observes the
// target and holds until a stop — no input is dispatched and nothing is
// admitted) and the calibrated inventory traversal on darwin (the
// traversal seam carries collect.Inventory). The source itself changes
// nothing between modes except the scope label and the traversal factory.
type GameScanSource struct {
	// Port is the platform the sessions run on (platform.Host()).
	Port platform.Port
	// Target selects the game window.
	Target platform.TargetSpec
	// Hotkey is the global stop hotkey the sessions register.
	Hotkey platform.HotkeySpec
	// Client is the record shell's client identity (one of record's
	// known clients).
	Client record.Client
	// Traversal builds each session's traversal; nil means the PR 4a
	// observe-only traversal.
	Traversal func() collect.Traversal
	// Mode is the scope's mode label: "observeOnly" or "traverse". Empty
	// keeps the historical observe-only label.
	Mode string

	// stop is the application's idempotent stop entry, bound by
	// NewService so the registered hotkey reaches the same stop as the
	// stop button and the quit path.
	stop func()
}

// NewScan implements ScanSource: a fresh collect.Controller per start,
// described by the source's mode (observe-only or traverse).
func (g *GameScanSource) NewScan() (ScanSpec, error) {
	if g.Port == nil {
		return ScanSpec{}, fmt.Errorf("app: game scan source: no port")
	}
	scope, err := json.Marshal(struct {
		Kind string `json:"kind"`
		Mode string `json:"mode"`
	}{Kind: "zzzInventory", Mode: g.scopeMode()})
	if err != nil {
		return ScanSpec{}, fmt.Errorf("app: game scan source: marshal scope: %w", err)
	}
	return ScanSpec{
		Game:   "zzz",
		Entity: "driveDisc",
		Client: g.Client,
		Locale: "zh-CN",
		Scope:  scope,
		Controller: &collect.Controller{
			Port:      g.Port,
			Target:    g.Target,
			Hotkey:    g.Hotkey,
			Traversal: g.Traversal,
			Stop:      g.stop,
		},
	}, nil
}

// scopeMode is the declared scan mode: the configured one, or the
// historical observe-only label when the source was built without one.
func (g *GameScanSource) scopeMode() string {
	if g.Mode != "" {
		return g.Mode
	}
	return "observeOnly"
}

// bindStop receives the service's stop entry; NewService calls it on
// every source implementing this unexported hook. The bound stop is
// what the registered hotkey triggers, so a hotkey-driven stop walks
// the exact same idempotent entry as the UI's stop button — including
// the stopRequested path when the kernel has not committed yet.
func (g *GameScanSource) bindStop(stop func()) { g.stop = stop }
