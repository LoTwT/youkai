package platform

// StopScanHotkey is the existing global stop binding for each platform.
// Registration and visible key names share this spec; ui.Cmd alone cannot
// describe it because macOS intentionally also holds Control and Option.
func StopScanHotkey(goos string) HotkeySpec {
	if goos == "darwin" {
		return HotkeySpec{Key: KeyS, Ctrl: true, Alt: true, Meta: true}
	}
	return HotkeySpec{Key: KeyS, Ctrl: true, Alt: true, Shift: true}
}
