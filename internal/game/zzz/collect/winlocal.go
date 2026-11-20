package collect

import (
	"fmt"
	"image"
	"time"
)

// WindowsLocalTimingProvenance names where the Windows local profile's
// timing comes from. W2c has no Windows timing measurements, so the scroll
// settle profile and both drag durations are the macOS cloud client's
// values. Diagnostics quote this label so a field failure can be attributed
// to a known initial value instead of a Windows measurement; W3 replaces
// the values and this label with the measured profile.
const WindowsLocalTimingProvenance = "macOS cloud profile (uncalibrated; W3 finalizes)"

// WindowsLocalProfile is the Windows local client's traversal profile for
// one opened client-area tier (W2c). The layout and thresholds are derived
// from the measured client area through the W2a reference canvas; the
// timing and drag values are temporary initial values copied from the
// macOS cloud profile.
//
// Calibration state: W2c wires the profile, it does not calibrate it.
// Scroll timing, drag durations and scaled pixel-count thresholds are
// unverified on Windows. The native 1920×1080 reference carries the W3a
// sort-label template and the T5 range probes exercised by the six-position
// panel replay. Non-identity transforms drop both native panel probes.
type WindowsLocalProfile struct {
	// Name identifies the profile in diagnostics.
	Name string
	// Tier is the opened client-area tier the profile was built for.
	Tier DisplayTier
	// Transform maps the 1920×1080 reference canvas onto Tier.
	Transform CanvasTransform
	// Layout is ScaledLayout(ReferenceLayout(), Transform) with this
	// profile's drag durations.
	Layout Layout
	// Calibration is ScaledCalibration(ReferenceCalibration(), scale).
	Calibration Calibration
	// ScrollTiming is the stage-2 settle profile. Uncalibrated: it is
	// MacCloudScrollTiming, not a Windows measurement; W3 finalizes it.
	ScrollTiming ScrollTiming
	// ScrollDuration and TopDuration are the drags' press-to-release
	// lengths stored in Layout. Uncalibrated: they are the macOS cloud
	// profile's values (1200 ms and 150 ms); W3 finalizes them.
	ScrollDuration time.Duration
	TopDuration    time.Duration
}

// WindowsLocalProfileFor builds the traversal profile for one measured
// client area. The caller must pass a size the platform precheck accepted
// and froze (the controller does so after LockBaseline): a layout selected
// from an unverified client area would act on the wrong pixels.
//
// Only tiers in the D-2 whitelist (openClientTiers, currently 1920×1080)
// are opened. A known-but-unopened tier and an unknown size are both
// refused, with the allowed tiers named: the platform precheck refuses
// them first, and this second gate keeps a caller that bypasses the
// precheck from starting an unaccepted tier.
func WindowsLocalProfileFor(client image.Point) (WindowsLocalProfile, error) {
	tier, err := openClientTier(client)
	if err != nil {
		return WindowsLocalProfile{}, err
	}
	return windowsLocalProfileForTier(tier)
}

// windowsLocalProfileForTier builds the profile for a known tier. The public
// entry point gates which tiers may start; this split also lets tests check
// the duration invariant on every reference tier, including the scaled tiers
// that D-2 has not opened yet.
func windowsLocalProfileForTier(tier DisplayTier) (WindowsLocalProfile, error) {
	transform, err := ClientRectTransform(tier.Client)
	if err != nil {
		return WindowsLocalProfile{}, fmt.Errorf("collect: windows local: %s: %w", tier.Name, err)
	}
	layout, err := ScaledLayout(ReferenceLayout(), transform)
	if err != nil {
		return WindowsLocalProfile{}, fmt.Errorf("collect: windows local: %s: scale layout: %w", tier.Name, err)
	}
	calibration, err := ScaledCalibration(ReferenceCalibration(), transform.Scale)
	if err != nil {
		return WindowsLocalProfile{}, fmt.Errorf("collect: windows local: %s: scale calibration: %w", tier.Name, err)
	}
	// The drag geometry comes from the scaled reference layout; the drag
	// durations are client timing and stay the macOS cloud profile's
	// uncalibrated initial values until W3.
	mac := MacCloudLayout()
	layout.ScrollDuration = mac.ScrollDuration
	layout.TopDuration = mac.TopDuration
	return WindowsLocalProfile{
		Name:           "windowsLocal-" + tier.Name,
		Tier:           tier,
		Transform:      transform,
		Layout:         layout,
		Calibration:    calibration,
		ScrollTiming:   MacCloudScrollTiming(),
		ScrollDuration: layout.ScrollDuration,
		TopDuration:    layout.TopDuration,
	}, nil
}

// openClientTier returns the opened tier whose client area is exactly
// client. Known-but-closed and unknown sizes are different refusals: the
// first can be fixed by switching the game's display setting, the second
// needs a supported configuration.
func openClientTier(client image.Point) (DisplayTier, error) {
	for _, tier := range openClientTiers {
		if tier.Client == client {
			return tier, nil
		}
	}
	for _, tier := range referenceClientTiers {
		if tier.Client == client {
			return DisplayTier{}, fmt.Errorf(
				"collect: windows local: client area %s is a known 16:9 tier but no profile for it is open; the open tiers are %s (D-2: each tier needs its own field acceptance)",
				tier.Name, openTierNames())
		}
	}
	return DisplayTier{}, fmt.Errorf(
		"collect: windows local: client area %dx%d is not a known 16:9 tier (%s); the open tiers are %s",
		client.X, client.Y, tierNames(), openTierNames())
}

// Diagnostic renders the profile's identity and calibration state for the
// session log, including native sort and range probe availability and the
// provenance of the scroll timing.
func (p WindowsLocalProfile) Diagnostic() string {
	return fmt.Sprintf("profile=%s tier=%s client=%dx%d scale=%.4f layout=%s sort=%s panel_range_required=%t timing=%s",
		p.Name, p.Tier.Name, p.Tier.Client.X, p.Tier.Client.Y, p.Transform.Scale, p.Layout.Name, p.sortState(), p.Layout.panelRange != nil, WindowsLocalTimingProvenance)
}

// sortState names whether this profile can read the panel's sort order. The
// 1920×1080 tier is 1:1 with the reference canvas and therefore carries the
// Windows native template; a scaled tier drops it (see ScaledLayout) and
// stays unverified rather than comparing a resampled template.
func (p WindowsLocalProfile) sortState() string {
	if !p.Layout.SortLabel.Empty() && p.Layout.SortTemplate != nil {
		return "template"
	}
	return "unverified"
}

// Inventory builds one session's traversal from the profile: the scaled
// layout, the profile's calibration, the scroll settle profile and the
// drag durations. legacy selects the pre-stage-2 grayscale-MAD scroll
// judgment, the same field fallback the macOS wiring keeps.
//
// The native layout requires calibrated reset and S evidence. The scroll
// matcher fallback uses the same panel probes and preparation gate.
func (p WindowsLocalProfile) Inventory(legacy bool) *Inventory {
	pixel := NewPixelMatcher(p.Layout, p.Calibration)
	var matcher Matcher = pixel
	if !legacy {
		matcher = NewScrollEvidenceMatcher(pixel)
	}
	timing := p.ScrollTiming
	return &Inventory{
		Layout:                 p.Layout,
		Matcher:                matcher,
		RequirePanelRangeProof: p.Layout.panelRange != nil,
		ScrollTiming:           &timing,
	}
}
