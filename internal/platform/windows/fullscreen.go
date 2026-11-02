package windows

// FullscreenState is the exclusive-fullscreen verdict of the shell's
// user-notification state.
//
// Windows offers no reliable per-window "is exclusive full screen" API.
// The documented signal is SHQueryUserNotificationState: QUNS_RUNNING_D3D_FULL_SCREEN
// means an exclusive-mode Direct3D application is running. QUNS_BUSY only
// says a full-screen application (or presentation mode) is running and
// cannot distinguish an exclusive game from a borderless one, so it is
// deliberately not treated as exclusive. Any other answer, or a failed
// query, is unknown: the checker reports what it knows and does not guess.
type FullscreenState string

const (
	// FullscreenNotExclusive: the shell reports no exclusive Direct3D
	// application.
	FullscreenNotExclusive FullscreenState = "notExclusive"
	// FullscreenExclusive: QUNS_RUNNING_D3D_FULL_SCREEN.
	FullscreenExclusive FullscreenState = "exclusive"
	// FullscreenUnknown: the state could not be read or was not one of
	// the documented values.
	FullscreenUnknown FullscreenState = "unknown"
)

// FullscreenAPI exposes the shell's exclusive-fullscreen state. It is a
// global signal; the adapter only attributes it to the target while the
// target is the actual foreground window.
type FullscreenAPI interface {
	FullscreenState() (FullscreenState, error)
}

// CodeExclusiveFullscreen: the foreground target is an exclusive-mode
// Direct3D application; GDI captures and SendInput are not a supported
// path there.
const CodeExclusiveFullscreen = "exclusiveFullscreen"

// FullscreenStateFromNotification maps the documented
// QUERY_USER_NOTIFICATION_STATE values to the conservative verdict.
func FullscreenStateFromNotification(state uint32) FullscreenState {
	switch state {
	case 3: // QUNS_RUNNING_D3D_FULL_SCREEN
		return FullscreenExclusive
	case 1, 2, 4, 5, 6, 7: // not present, busy, presentation, accepts, quiet, app
		return FullscreenNotExclusive
	default:
		return FullscreenUnknown
	}
}
