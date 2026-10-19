package collect

import "testing"

// The lock badge flashes; one frame may lose its annulus. These tests pin
// the union rules the admission applies across the target's pre-click and
// final frames: a positive badge survives a missed frame, while genuinely
// contradictory evidence stays unknown (JSON null), never none.
func TestMergeManagementAcrossFrames(t *testing.T) {
	cases := []struct {
		name   string
		states []ManagementState
		want   ManagementState
	}{
		{"single lock", []ManagementState{ManagementLocked}, ManagementLocked},
		{"lock survives a dark frame", []ManagementState{ManagementNone, ManagementLocked}, ManagementLocked},
		{"lock survives an unreadable frame", []ManagementState{ManagementUnknown, ManagementLocked}, ManagementLocked},
		{"trash survives a missed frame", []ManagementState{ManagementNone, ManagementDiscarded}, ManagementDiscarded},
		{"all none", []ManagementState{ManagementNone, ManagementNone}, ManagementNone},
		{"missed and unreadable stay unknown", []ManagementState{ManagementNone, ManagementUnknown}, ManagementUnknown},
		{"lock and trash conflict", []ManagementState{ManagementLocked, ManagementDiscarded}, ManagementUnknown},
		{"no evidence", nil, ManagementUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mergeManagement(tc.states...); got != tc.want {
				t.Fatalf("mergeManagement(%v) = %q, want %q", tc.states, got, tc.want)
			}
		})
	}
}

func TestManagementVerdictRequiresTopConfirmation(t *testing.T) {
	match := func(list ManagementState, top TopManagementState) PageMatch {
		return PageMatch{List: &ListEvidence{
			Management:    []ManagementState{list},
			TopManagement: top,
		}}
	}
	cases := []struct {
		name string
		pre  PageMatch
		out  PageMatch
		want string
	}{
		{"locked confirmed", match(ManagementLocked, TopManagementLocked), match(ManagementLocked, TopManagementLocked), "locked"},
		{"none confirmed", match(ManagementNone, TopManagementUnlocked), match(ManagementNone, TopManagementUnlocked), "none"},
		{"discarded confirmed", match(ManagementDiscarded, TopManagementUnlocked), match(ManagementDiscarded, TopManagementUnlocked), "discarded"},
		{"lock recovered from pre-click frame", match(ManagementLocked, TopManagementLocked), match(ManagementNone, TopManagementLocked), "locked"},
		{"discard recovered from pre-click frame", match(ManagementDiscarded, TopManagementUnlocked), match(ManagementNone, TopManagementUnlocked), "discarded"},
		{"top indicator disagrees with lock", match(ManagementLocked, TopManagementUnlocked), match(ManagementLocked, TopManagementUnlocked), ""},
		{"top indicator disagrees with none", match(ManagementNone, TopManagementLocked), match(ManagementNone, TopManagementLocked), ""},
		{"unreadable list badge stays null", match(ManagementUnknown, TopManagementLocked), match(ManagementUnknown, TopManagementLocked), ""},
		{"unreadable top indicator stays null", match(ManagementLocked, TopManagementUnknown), match(ManagementLocked, TopManagementUnknown), ""},
		{"lock and trash conflict stays null", match(ManagementLocked, TopManagementLocked), match(ManagementDiscarded, TopManagementLocked), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, evidence := managementVerdict(tc.pre, tc.out, 0)
			if got != tc.want {
				t.Fatalf("managementVerdict = %q, want %q (%s)", got, tc.want, evidence)
			}
			if evidence == "" {
				t.Fatal("managementVerdict carries no evidence")
			}
		})
	}
}

func TestEquippedVerdictRequiresBothAvatars(t *testing.T) {
	match := func(list AvatarState, detail AvatarState) PageMatch {
		return PageMatch{
			List:   &ListEvidence{Avatars: []AvatarState{list}},
			Detail: &DetailEvidence{Avatar: detail},
		}
	}
	cases := []struct {
		name string
		pre  PageMatch
		out  PageMatch
		want string
	}{
		{"present and present", match(AvatarPresent, AvatarPresent), match(AvatarPresent, AvatarPresent), EquippedStateEquipped},
		{"absent and absent", match(AvatarAbsent, AvatarAbsent), match(AvatarAbsent, AvatarAbsent), EquippedStateUnequipped},
		{"detail present, list absent", match(AvatarAbsent, AvatarPresent), match(AvatarAbsent, AvatarPresent), ""},
		{"detail absent, list present", match(AvatarPresent, AvatarAbsent), match(AvatarPresent, AvatarAbsent), ""},
		{"unreadable list stays null", match(AvatarUnknown, AvatarPresent), match(AvatarUnknown, AvatarPresent), ""},
		{"unreadable detail stays null", match(AvatarPresent, AvatarUnknown), match(AvatarPresent, AvatarUnknown), ""},
		{"per-frame conflict stays null", match(AvatarPresent, AvatarAbsent), match(AvatarAbsent, AvatarAbsent), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, evidence := equippedVerdict(tc.pre, tc.out, 0)
			if got != tc.want {
				t.Fatalf("equippedVerdict = %q, want %q (%s)", got, tc.want, evidence)
			}
			if evidence == "" {
				t.Fatal("equippedVerdict carries no evidence")
			}
		})
	}
}
