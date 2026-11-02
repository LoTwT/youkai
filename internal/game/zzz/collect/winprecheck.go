package collect

import (
	"fmt"
	"image"

	"github.com/LoTwT/youkai/internal/platform"
)

// WindowsLocalDisplayPolicy builds the Windows local client's precheck
// contract (T2 §3.2): the reference tiers, the opened whitelist and the
// occlusion samples derived from ReferenceLayout at each opened tier's
// clientRect transform. The platform enforces it; this function owns the
// game-specific geometry.
//
// Only opened tiers get probes. A known-but-not-opened tier is refused by
// the tier check before any sample is used, so the policy does not pretend
// to know its sampling positions.
func WindowsLocalDisplayPolicy() (platform.WindowDisplayPolicy, error) {
	var policy platform.WindowDisplayPolicy
	for _, tier := range ReferenceClientTiers() {
		policy.KnownTiers = append(policy.KnownTiers, platform.ClientTier{Name: tier.Name, Client: tier.Client})
	}
	policy.Probes = make(map[image.Point]platform.WindowProbes, len(openClientTiers))
	for _, tier := range OpenClientTiers() {
		transform, err := ClientRectTransform(tier.Client)
		if err != nil {
			return platform.WindowDisplayPolicy{}, fmt.Errorf("collect: display policy: %w", err)
		}
		layout, err := ScaledLayout(ReferenceLayout(), transform)
		if err != nil {
			return platform.WindowDisplayPolicy{}, fmt.Errorf("collect: display policy: scale reference layout to %s: %w", tier.Name, err)
		}
		probes, err := localClientProbes(layout)
		if err != nil {
			return platform.WindowDisplayPolicy{}, fmt.Errorf("collect: display policy: %s: %w", tier.Name, err)
		}
		policy.OpenTiers = append(policy.OpenTiers, platform.ClientTier{Name: tier.Name, Client: tier.Client})
		policy.Probes[tier.Client] = probes
	}
	return policy, nil
}

// localClientProbes derives the fixed occlusion sample set from a scaled
// reference layout (T2 §3.2):
//
//   - the four client corners, inset from the edges;
//   - one point in the bottom button bar;
//   - the scrollbar track's top and bottom;
//   - the detail pane (the first calibrated detail line's center);
//   - the six wheel centers;
//   - the four corners of the list viewport, inset.
//
// The samples are positions the game itself paints; hit-testing them
// finds a taskbar, dialog or other root window covering the target. The
// optional content probe is deliberately left empty until an unmasked
// bottom-bar frame is measured: the reference fixtures mask everything
// outside their calibrated probes, so guessing a content region would
// reject valid frames.
func localClientProbes(layout Layout) (platform.WindowProbes, error) {
	w, h := layout.Image.X, layout.Image.Y
	if w <= 0 || h <= 0 {
		return platform.WindowProbes{}, fmt.Errorf("scaled layout has image size %dx%d", w, h)
	}
	inside := image.Rectangle{Max: layout.Image}
	cornerX := maxInt(1, w/80) // 24 at 1920
	cornerY := maxInt(1, h/45) // 24 at 1080
	viewX := maxInt(1, w/480)  // 4 at 1920
	viewY := maxInt(1, h/270)  // 4 at 1080
	barY := h - maxInt(1, h*52/1080)

	var probes []platform.ProbePoint
	add := func(name string, at image.Point) error {
		if !at.In(inside) {
			return fmt.Errorf("probe %q at %v is outside the %dx%d client", name, at, w, h)
		}
		probes = append(probes, platform.ProbePoint{Name: name, At: at})
		return nil
	}
	points := []struct {
		name string
		at   image.Point
	}{
		{"corner-top-left", image.Pt(cornerX, cornerY)},
		{"corner-top-right", image.Pt(w-1-cornerX, cornerY)},
		{"corner-bottom-left", image.Pt(cornerX, h-1-cornerY)},
		{"corner-bottom-right", image.Pt(w-1-cornerX, h-1-cornerY)},
		{"bottom-button-bar", image.Pt(w/2, barY)},
		{"scrollbar-top", image.Pt(layout.ScrollTrack.Min.X+layout.ScrollTrack.Dx()/2, layout.ScrollTrack.Min.Y+1)},
		{"scrollbar-bottom", image.Pt(layout.ScrollTrack.Min.X+layout.ScrollTrack.Dx()/2, layout.ScrollTrack.Max.Y-2)},
		{"detail-pane", rectCenter(layout.DetailLines[0].Rect)},
		{"list-top-left", image.Pt(layout.ListViewport.Min.X+viewX, layout.ListViewport.Min.Y+viewY)},
		{"list-top-right", image.Pt(layout.ListViewport.Max.X-1-viewX, layout.ListViewport.Min.Y+viewY)},
		{"list-bottom-left", image.Pt(layout.ListViewport.Min.X+viewX, layout.ListViewport.Max.Y-1-viewY)},
		{"list-bottom-right", image.Pt(layout.ListViewport.Max.X-1-viewX, layout.ListViewport.Max.Y-1-viewY)},
	}
	for _, p := range points {
		if err := add(p.name, p.at); err != nil {
			return platform.WindowProbes{}, err
		}
	}
	for i, center := range layout.Wheel {
		if err := add(fmt.Sprintf("wheel-%d", i+1), center); err != nil {
			return platform.WindowProbes{}, err
		}
	}
	// The game draws a legitimate all-black strip at the top of the local
	// client: 22 px at 1920×1080 in the 2026-11-01 native capture. The
	// margin keeps 2 px of slack; the other three measured edges reach the
	// client border with content, so their margins stay zero.
	topMargin := maxInt(1, h*24/1080)
	return platform.WindowProbes{
		Occlusion: probes,
		EdgeMargins: platform.EdgeMargins{
			Top: topMargin,
		},
	}, nil
}

func pointString(p image.Point) string {
	return fmt.Sprintf("%dx%d", p.X, p.Y)
}

func rectString(r image.Rectangle) string {
	return fmt.Sprintf("%d,%d %dx%d", r.Min.X, r.Min.Y, r.Dx(), r.Dy())
}
