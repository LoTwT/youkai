package collect_test

import (
	"strings"
	"testing"

	"github.com/LoTwT/youkai/internal/game/zzz/collect"
	"github.com/LoTwT/youkai/internal/platform"
)

// stateMatcher overrides the production matcher's management/avatar
// verdicts so the admission's combination rules can be driven without
// synthesizing every badge in the scripted game's frames.
type stateMatcher struct {
	collect.Matcher
	target       int
	beforeList   collect.ManagementState
	afterList    collect.ManagementState
	top          collect.TopManagementState
	listAvatar   collect.AvatarState
	detailAvatar collect.AvatarState
}

func (m *stateMatcher) Match(f platform.Frame) collect.PageMatch {
	pm := m.Matcher.Match(f)
	if pm.List != nil {
		state := m.afterList
		if pm.List.Selected != m.target {
			state = m.beforeList
		}
		for i := range pm.List.Management {
			pm.List.Management[i] = state
		}
		pm.List.TopManagement = m.top
		for i := range pm.List.Avatars {
			pm.List.Avatars[i] = m.listAvatar
		}
	}
	if pm.Detail != nil {
		pm.Detail.Avatar = m.detailAvatar
	}
	return pm
}

// TestTraverseMergesLockFlashAndCarriesPresence checks that a lock missed
// on the pre-click frame is recovered from the final frame, that the
// confirmed management/equipment presence reaches the Observation, and
// that the evidence strings name both sources.
func TestTraverseMergesLockFlashAndCarriesPresence(t *testing.T) {
	game := newScriptedGame(true)
	h := newTraversalHarness(t, game)
	h.inv.Matcher = &stateMatcher{
		Matcher:      h.inv.Matcher,
		target:       0,
		beforeList:   collect.ManagementNone,
		afterList:    collect.ManagementLocked,
		top:          collect.TopManagementLocked,
		listAvatar:   collect.AvatarPresent,
		detailAvatar: collect.AvatarPresent,
	}
	h.start()
	h.endCompleted()
	obs := h.observations()
	if len(obs) != 1 {
		t.Fatalf("admitted %d, want 1", len(obs))
	}
	if obs[0].ManagementState != string(collect.ManagementLocked) {
		t.Fatalf("managementState = %q, want locked", obs[0].ManagementState)
	}
	if !strings.Contains(obs[0].ManagementEvidence, "list badge locked") ||
		!strings.Contains(obs[0].ManagementEvidence, "top indicator locked") {
		t.Fatalf("management evidence = %q, want both the badge and top indicator", obs[0].ManagementEvidence)
	}
	if obs[0].EquippedState != collect.EquippedStateEquipped {
		t.Fatalf("equippedState = %q, want equipped", obs[0].EquippedState)
	}
	if !strings.Contains(obs[0].EquippedEvidence, "detail avatar present") ||
		!strings.Contains(obs[0].EquippedEvidence, "list avatar present") {
		t.Fatalf("equipped evidence = %q, want detail and list avatar evidence", obs[0].EquippedEvidence)
	}
}

// TestTraverseConflictingStateEvidenceStaysNull checks that a lock the top
// indicator does not confirm, and an avatar conflict, both stay empty
// (JSON null). Neither is collapsed into none or unequipped.
func TestTraverseConflictingStateEvidenceStaysNull(t *testing.T) {
	game := newScriptedGame(true)
	h := newTraversalHarness(t, game)
	h.inv.Matcher = &stateMatcher{
		Matcher:      h.inv.Matcher,
		target:       0,
		beforeList:   collect.ManagementLocked,
		afterList:    collect.ManagementLocked,
		top:          collect.TopManagementUnlocked,
		listAvatar:   collect.AvatarAbsent,
		detailAvatar: collect.AvatarPresent,
	}
	h.start()
	h.endCompleted()
	obs := h.observations()
	if len(obs) != 1 {
		t.Fatalf("admitted %d, want 1", len(obs))
	}
	if obs[0].ManagementState != "" {
		t.Fatalf("managementState = %q, want null on a top-indicator conflict", obs[0].ManagementState)
	}
	if obs[0].EquippedState != "" {
		t.Fatalf("equippedState = %q, want null on an avatar conflict", obs[0].EquippedState)
	}
	if !strings.Contains(obs[0].ManagementEvidence, "does not confirm") {
		t.Fatalf("management evidence = %q, want the conflict reason", obs[0].ManagementEvidence)
	}
	if !strings.Contains(obs[0].EquippedEvidence, "do not agree") {
		t.Fatalf("equipped evidence = %q, want the conflict reason", obs[0].EquippedEvidence)
	}
}
