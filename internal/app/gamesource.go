package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"strings"

	"github.com/LoTwT/youkai/internal/game/zzz/collect"
	"github.com/LoTwT/youkai/internal/platform"
	"github.com/LoTwT/youkai/internal/record"
)

// TargetCandidate is one running game window the user can confirm as the
// scan target (P-01). It is the platform's candidate shape, re-exported so
// the window and the service share one vocabulary.
type TargetCandidate = platform.TargetCandidate

// TargetDiscoverer is the optional capability of a ScanSource that can
// enumerate the game windows a user may confirm. Sources with a fixed
// target (macOS, the environment overrides, development fixtures) do not
// implement it.
type TargetDiscoverer interface {
	DiscoverTargets() ([]TargetCandidate, error)
}

// TargetScanSource is the optional capability of a ScanSource that starts
// one user-confirmed window. StartRequest.Target reaches it instead of
// NewScan, so a source that discovers targets never starts without the
// user's confirmation.
type TargetScanSource interface {
	NewScanForTarget(TargetCandidate) (ScanSpec, error)
}

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
// admitted) and the calibrated inventory traversal (the traversal seam
// carries collect.Inventory). The source itself changes nothing between
// modes except the scope label and the traversal factory. A source wired
// without a fixed target discovers the running game windows and starts
// only the candidate the user confirmed (P-01).
type GameScanSource struct {
	// Port is the platform the sessions run on (platform.Host()).
	Port platform.Port
	// Target selects the game window. Empty means the user must confirm
	// one of the windows DiscoverTargets returns; the source then refuses
	// NewScan.
	Target platform.TargetSpec
	// Hotkey is the global stop hotkey the sessions register.
	Hotkey platform.HotkeySpec
	// Client is the record shell's client identity (one of record's
	// known clients).
	Client record.Client
	// Traversal builds each session's traversal; nil means the PR 4a
	// observe-only traversal.
	Traversal func() collect.Traversal
	// TraversalForClient builds each session's traversal from the measured
	// client area the platform precheck accepted (the Windows local
	// client, W2c). It takes precedence over Traversal.
	TraversalForClient func(image.Point) (collect.Traversal, error)
	// Mode is the scope's mode label: "observeOnly" or "traverse". Empty
	// keeps the historical observe-only label.
	Mode string

	// stop is the application's idempotent stop entry, bound by
	// NewService so the registered hotkey reaches the same stop as the
	// stop button and the quit path.
	stop func()
}

// NewScan implements ScanSource: a fresh collect.Controller per start,
// described by the source's mode (observe-only or traverse). A source
// without a fixed target refuses here: the user must confirm one of the
// discovered windows first, even when only one is running (P-01).
func (g *GameScanSource) NewScan() (ScanSpec, error) {
	if g.RequiresTargetConfirmation() {
		return ScanSpec{}, errors.New("app: game scan source: the game window must be confirmed before starting")
	}
	return g.newScan(g.Target)
}

// RequiresTargetConfirmation reports whether a start must carry a
// user-confirmed candidate. It is true for the installed Windows source,
// which is wired without a target and discovers the running client.
func (g *GameScanSource) RequiresTargetConfirmation() bool {
	return g.Target.BundleID == "" && g.Target.ExecutablePath == ""
}

// DiscoverTargets implements TargetDiscoverer through the platform port.
func (g *GameScanSource) DiscoverTargets() ([]TargetCandidate, error) {
	if g.Port == nil {
		return nil, errors.New("app: game scan source: no port")
	}
	discovery, ok := g.Port.(platform.TargetDiscovery)
	if !ok {
		return nil, errors.New("app: game scan source: the wired platform cannot discover target windows")
	}
	candidates, err := discovery.DiscoverTargets()
	if err != nil {
		return nil, fmt.Errorf("app: game scan source: enumerate game windows: %w", err)
	}
	return candidates, nil
}

// NewScanForTarget implements TargetScanSource: it re-enumerates the game
// windows and requires the confirmed window to still be present with the
// same process instance. A window that vanished or was replaced refuses the
// start instead of locking another window of the same executable.
func (g *GameScanSource) NewScanForTarget(confirmed TargetCandidate) (ScanSpec, error) {
	if confirmed.Handle == 0 {
		return ScanSpec{}, errors.New("app: game scan source: the confirmed target has no window handle")
	}
	current, err := g.DiscoverTargets()
	if err != nil {
		return ScanSpec{}, err
	}
	for _, c := range current {
		if sameTargetInstance(c, confirmed) {
			return g.newScan(platform.TargetSpec{
				ExecutablePath: c.ExecutablePath,
				HandleHint:     c.Handle,
			})
		}
	}
	return ScanSpec{}, errors.New("app: game scan source: the confirmed game window is gone or was replaced; check the target again")
}

// sameTargetInstance reports whether two candidates describe the same
// window of the same process instance. Paths compare case-insensitively
// like Windows compares them.
func sameTargetInstance(a, b TargetCandidate) bool {
	return a.Handle == b.Handle && a.PID == b.PID &&
		a.ProcessStartedAt.Equal(b.ProcessStartedAt) &&
		strings.EqualFold(a.ExecutablePath, b.ExecutablePath)
}

// newScan builds the accepted spec for one target.
func (g *GameScanSource) newScan(target platform.TargetSpec) (ScanSpec, error) {
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
			Port:               g.Port,
			Target:             target,
			Hotkey:             g.Hotkey,
			Traversal:          g.Traversal,
			TraversalForClient: g.TraversalForClient,
			Stop:               g.stop,
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
