package collect_test

import (
	"image"
	"testing"

	"github.com/LoTwT/youkai/internal/game/zzz/collect"
)

// W2b: the Windows local display policy is the single place that decides
// which reference tiers may start and where the precheck samples. D-2
// opens 1920×1080 only.
func TestWindowsLocalDisplayPolicy(t *testing.T) {
	policy, err := collect.WindowsLocalDisplayPolicy()
	if err != nil {
		t.Fatalf("policy: %v", err)
	}
	if len(policy.KnownTiers) != len(collect.ReferenceClientTiers()) {
		t.Fatalf("known tiers = %d, want every ReferenceClientTier (%d)",
			len(policy.KnownTiers), len(collect.ReferenceClientTiers()))
	}
	if len(policy.OpenTiers) != 1 || policy.OpenTiers[0].Name != "1920x1080" {
		t.Fatalf("open tiers = %+v, want only 1920x1080", policy.OpenTiers)
	}
	probes, ok := policy.Probes[image.Pt(1920, 1080)]
	if !ok {
		t.Fatal("the open tier has no probe set")
	}
	if len(probes.Occlusion) != 18 {
		t.Fatalf("occlusion probes = %d, want 18 (4 corners + bottom bar + 2 scrollbar + detail + 6 wheels + 4 viewport corners)",
			len(probes.Occlusion))
	}
	names := map[string]bool{}
	for _, probe := range probes.Occlusion {
		if probe.At.X < 0 || probe.At.Y < 0 || probe.At.X >= 1920 || probe.At.Y >= 1080 {
			t.Fatalf("probe %q at %v is outside 1920×1080", probe.Name, probe.At)
		}
		if names[probe.Name] {
			t.Fatalf("duplicate probe name %q", probe.Name)
		}
		names[probe.Name] = true
	}
	for _, want := range []string{"corner-top-left", "corner-bottom-right", "bottom-button-bar",
		"scrollbar-top", "scrollbar-bottom", "detail-pane", "wheel-1", "wheel-6",
		"list-top-left", "list-bottom-right"} {
		if !names[want] {
			t.Fatalf("probe %q missing from %v", want, names)
		}
	}
	// A tier that is listed but not opened must not claim a probe table:
	// the tier check refuses it before any sample is meaningful.
	if _, ok := policy.Probes[image.Pt(2560, 1440)]; ok {
		t.Fatal("an unopened tier must not carry precheck probes")
	}
	if probes.EdgeMargins.Top != 24 || probes.EdgeMargins.Left != 0 ||
		probes.EdgeMargins.Right != 0 || probes.EdgeMargins.Bottom != 0 {
		t.Fatalf("edge margins = %+v, want a 24-px top margin from the measured 22-px black strip and no margin on the other edges",
			probes.EdgeMargins)
	}
}
