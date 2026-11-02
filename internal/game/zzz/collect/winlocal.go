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
// Timing, drag durations and the scaled pixel-count thresholds are
// unverified on Windows; W3's sample collection, parameter finalization
// and full 1920×1080 field acceptance replace them. The profile has no
// sort-label template, so the traversal's sort fact stays unverified and
// coverage can be at most partial.
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
// session log. Tier, scale, profile name and the uncalibrated timing
// provenance are the facts a later field report needs to attribute its
// results to this wiring.
func (p WindowsLocalProfile) Diagnostic() string {
	return fmt.Sprintf("profile=%s tier=%s client=%dx%d scale=%.4f layout=%s sort=unverified timing=%s",
		p.Name, p.Tier.Name, p.Tier.Client.X, p.Tier.Client.Y, p.Transform.Scale, p.Layout.Name, WindowsLocalTimingProvenance)
}

// Inventory builds one session's traversal from the profile: the scaled
// layout, the profile's calibration, the scroll settle profile and the
// drag durations. legacy selects the pre-stage-2 grayscale-MAD scroll
// judgment, the same field fallback the macOS wiring keeps.
//
// The layout carries no sort-label template, so the traversal reports the
// sort as unverified and coverage can be at most partial (fail-closed; a
// template is fabricated nowhere).
func (p WindowsLocalProfile) Inventory(legacy bool) *Inventory {
	pixel := NewPixelMatcher(p.Layout, p.Calibration)
	var matcher Matcher = pixel
	if !legacy {
		matcher = NewScrollEvidenceMatcher(pixel)
	}
	timing := p.ScrollTiming
	return &Inventory{
		Layout:       p.Layout,
		Matcher:      matcher,
		ScrollTiming: &timing,
	}
}
