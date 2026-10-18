//go:build windows

package platform

import (
	"github.com/LoTwT/youkai/internal/platform/windows"
)

// Host returns the platform's Port on Windows: the windows adapter
// wired to the real Win32 surfaces (user32 window and process queries,
// client geometry with DPI, the GDI capture path, SendInput, and the
// stop hotkey on its own message-loop thread).
func Host() (Port, error) {
	adapter, err := NewWinAdapter(WinNatives{
		Win:       windows.RealWindowAPI(),
		Procs:     windows.RealProcessAPI(),
		Geo:       windows.RealGeometryAPI(),
		FG:        windows.RealForegroundAPI(),
		GDI:       windows.RealGDI(),
		Send:      windows.RealSender(),
		HotkeyAPI: windows.RealHotkeyAPI(),
		StartLoop: func(mgr *windows.HotkeyManager) (WinHotkeyLoop, error) {
			loop, err := windows.StartHotkeyLoop(mgr)
			if err != nil {
				return nil, err
			}
			return loop, nil
		},
	})
	if err != nil {
		return nil, err
	}
	return adapter, nil
}
