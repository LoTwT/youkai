package collect

import (
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/platform/windows"
)

// TestWindowsLocalDragDurationsFitWindowsInputBound guards the 2026-11-02
// field failure: the Windows local profile inherits the macOS 1200ms scroll
// drag, and the Windows actor rejected it before SendInput against its former
// 1s bound. Both profile durations must stay inside the actor's bound for
// every reference tier, including the scaled tiers that D-2 has not opened
// yet.
func TestWindowsLocalDragDurationsFitWindowsInputBound(t *testing.T) {
	check := func(name string, scroll, top time.Duration) {
		t.Helper()
		if scroll <= 0 || scroll > windows.MaxDragDuration {
			t.Errorf("%s: scroll duration %s outside (0, %s]", name, scroll, windows.MaxDragDuration)
		}
		if top <= 0 || top > windows.MaxDragDuration {
			t.Errorf("%s: top duration %s outside (0, %s]", name, top, windows.MaxDragDuration)
		}
	}

	for _, tier := range openClientTiers {
		profile, err := WindowsLocalProfileFor(tier.Client)
		if err != nil {
			t.Fatalf("%s: %v", tier.Name, err)
		}
		check("open "+tier.Name, profile.ScrollDuration, profile.TopDuration)
		check("open "+tier.Name+" layout", profile.Layout.ScrollDuration, profile.Layout.TopDuration)
	}

	// A scaled tier reuses the same client timing when a later field
	// acceptance opens it; check the production builder directly rather than
	// duplicating the duration selection here.
	for _, tier := range referenceClientTiers {
		profile, err := windowsLocalProfileForTier(tier)
		if err != nil {
			t.Fatalf("%s: %v", tier.Name, err)
		}
		check("reference "+tier.Name, profile.ScrollDuration, profile.TopDuration)
		check("reference "+tier.Name+" layout", profile.Layout.ScrollDuration, profile.Layout.TopDuration)
	}
}
