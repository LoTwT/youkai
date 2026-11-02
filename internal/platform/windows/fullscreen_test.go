package windows

import "testing"

func TestFullscreenStateFromNotification(t *testing.T) {
	cases := map[uint32]FullscreenState{
		1: FullscreenNotExclusive, // QUNS_NOT_PRESENT
		2: FullscreenNotExclusive, // QUNS_BUSY: full screen, but not proof of exclusive D3D
		3: FullscreenExclusive,    // QUNS_RUNNING_D3D_FULL_SCREEN
		4: FullscreenNotExclusive, // QUNS_PRESENTATION_MODE
		5: FullscreenNotExclusive, // QUNS_ACCEPTS_NOTIFICATIONS
		6: FullscreenNotExclusive, // QUNS_QUIET_TIME
		7: FullscreenNotExclusive, // QUNS_APP
		0: FullscreenUnknown,
		9: FullscreenUnknown,
	}
	for state, want := range cases {
		if got := FullscreenStateFromNotification(state); got != want {
			t.Errorf("state %d = %q, want %q", state, got, want)
		}
	}
}
