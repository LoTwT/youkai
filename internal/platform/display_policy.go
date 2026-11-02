package platform

import (
	"fmt"
	"image"
)

// WindowDisplayPolicy is the game adapter's display-configuration contract
// for the Windows local client (T2 §3.2, decision D-2): the closed set of
// 16:9 client-area tiers the renderer is defined for, the subset whose
// profiles have field acceptance and may therefore be started, and the
// client-space sample points the precheck hit-tests once the target is
// foreground.
//
// The policy is built by the game adapter from its reference canvas
// (ReferenceClientTiers, the open-tier whitelist and the scaled reference
// layout). The platform enforces it; it never scales or accepts a tier on
// its own.
type WindowDisplayPolicy struct {
	KnownTiers []ClientTier
	OpenTiers  []ClientTier
	// Probes maps an open tier's exact client size to the client-space
	// points the occlusion check tests. A size without an entry cannot
	// be started even when it is listed as open.
	Probes map[image.Point]WindowProbes
}

// ClientTier is one named client-area size. The name is a diagnostic
// label ("1920x1080"); the size is the exact physical-pixel client size.
type ClientTier struct {
	Name   string
	Client image.Point
}

// WindowProbes are one open tier's precheck samples in client-area
// pixels (origin 0,0).
type WindowProbes struct {
	// Occlusion points: a point whose WindowFromPoint root is not the
	// target means the target is covered at that point.
	Occlusion []ProbePoint
	// Content, when non-empty, is a region that must contain at least one
	// non-black pixel in the captured frame. It is optional because the
	// reference canvas has no unmasked bottom-bar sample yet.
	Content image.Rectangle
	// EdgeMargins is the measured legitimate all-black margin (in image
	// pixels) from each client edge: the game itself draws a black strip
	// at the top of the local client, so an edge is only treated as
	// screen-outside black when content does not start within the
	// calibrated margin. Zero margins mean the first line must contain
	// content.
	EdgeMargins EdgeMargins
}

// EdgeMargins are the allowed all-black margins of the four client edges.
type EdgeMargins struct {
	Top, Right, Bottom, Left int
}

// ProbePoint is one named occlusion sample.
type ProbePoint struct {
	Name string
	At   image.Point
}

// validate refuses a policy the platform could only interpret by
// guessing: an open tier outside the known set, duplicate tiers, a
// missing probe table or probes outside their tier.
func (p WindowDisplayPolicy) validate() error {
	if len(p.KnownTiers) == 0 {
		return fmt.Errorf("display policy has no known tiers")
	}
	known := make(map[image.Point]string, len(p.KnownTiers))
	for _, tier := range p.KnownTiers {
		if tier.Name == "" || tier.Client.X <= 0 || tier.Client.Y <= 0 {
			return fmt.Errorf("display policy has an invalid known tier %+v", tier)
		}
		if prev, dup := known[tier.Client]; dup {
			return fmt.Errorf("display policy lists %v twice (%s and %s)", tier.Client, prev, tier.Name)
		}
		known[tier.Client] = tier.Name
	}
	open := make(map[image.Point]bool, len(p.OpenTiers))
	for _, tier := range p.OpenTiers {
		if name, ok := known[tier.Client]; !ok {
			return fmt.Errorf("display policy opens %s (%v), which is not a known tier", tier.Name, tier.Client)
		} else if name != tier.Name {
			return fmt.Errorf("display policy opens %v as %s but knows it as %s", tier.Client, tier.Name, name)
		}
		open[tier.Client] = true
	}
	for size := range open {
		probes, ok := p.Probes[size]
		if !ok {
			return fmt.Errorf("display policy opens %v without occlusion probes", size)
		}
		names := make(map[string]bool, len(probes.Occlusion))
		for _, point := range probes.Occlusion {
			if point.Name == "" {
				return fmt.Errorf("display policy has an unnamed occlusion probe at %v", point.At)
			}
			if names[point.Name] {
				return fmt.Errorf("display policy has duplicate occlusion probe %q", point.Name)
			}
			names[point.Name] = true
			if !point.At.In(image.Rectangle{Max: size}) {
				return fmt.Errorf("occlusion probe %q at %v is outside the %v client", point.Name, point.At, size)
			}
		}
		if len(probes.Occlusion) == 0 {
			return fmt.Errorf("display policy opens %v without any occlusion probes", size)
		}
		if !probes.Content.Empty() && !probes.Content.In(image.Rectangle{Max: size}) {
			return fmt.Errorf("display policy content probe %v is outside the %v client", probes.Content, size)
		}
		m := probes.EdgeMargins
		if m.Top < 0 || m.Right < 0 || m.Bottom < 0 || m.Left < 0 {
			return fmt.Errorf("display policy edge margins must not be negative: %+v", m)
		}
		if m.Top >= size.Y || m.Bottom >= size.Y || m.Left >= size.X || m.Right >= size.X {
			return fmt.Errorf("display policy edge margins %+v leave no content on the %v client", m, size)
		}
	}
	return nil
}

// tier classifies an exact client size: known and open, known but not
// opened, or unknown.
func (p WindowDisplayPolicy) tier(client image.Point) (ClientTier, bool, bool) {
	var knownTier ClientTier
	known := false
	for _, tier := range p.KnownTiers {
		if tier.Client == client {
			knownTier = tier
			known = true
			break
		}
	}
	if !known {
		return ClientTier{}, false, false
	}
	for _, tier := range p.OpenTiers {
		if tier.Client == client {
			return knownTier, true, true
		}
	}
	return knownTier, true, false
}

func (p WindowDisplayPolicy) probesFor(client image.Point) (WindowProbes, bool) {
	probes, ok := p.Probes[client]
	return probes, ok
}
