//go:build darwin

package platform

import (
	"github.com/LoTwT/youkai/internal/platform/macos"
)

// Host returns the platform's Port on macOS: the macos adapter wired to
// the real native surfaces (SCShareableContent enumeration, SCStream,
// Quartz events, NSWorkspace foreground, permission preflights, Carbon
// hotkey, mach clock).
func Host() (Port, error) {
	adapter, err := NewMacAdapter(MacNatives{
		Enum:   macos.NewWindowEnumerator(),
		Runner: macos.NewStreamRunner(),
		Poster: macos.NewInputPoster(),
		FG:     macos.NewForegroundProbe(),
		Perms:  macos.NewPermissionProbe(),
		Hotkey: macos.NewHotKeyRegistrar(),
		Clock:  macos.NewMachClockSource(),
	})
	if err != nil {
		return nil, err
	}
	return adapter, nil
}
