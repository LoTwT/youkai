package platform

import (
	"fmt"

	"github.com/LoTwT/youkai/internal/platform/macos"
	"github.com/LoTwT/youkai/internal/platform/windows"
)

// Neutral key mapping: the port's Key vocabulary translated to the two
// platforms' native codes. The macOS table holds the HIToolbox Events.h
// kVK_ANSI_* values of the shared Carbon/CGEvent numbering; the Windows
// table holds the VK_* values. Keys outside the vocabulary are refused
// by both mappings — the port never guesses a code.

// macKeyCodes maps the vocabulary to macOS virtual key codes.
var macKeyCodes = map[Key]macos.KeyCode{
	KeyA: 0x00, KeyB: 0x0B, KeyC: 0x08, KeyD: 0x02, KeyE: 0x0E,
	KeyF: 0x03, KeyG: 0x05, KeyH: 0x04, KeyI: 0x22, KeyJ: 0x26,
	KeyK: 0x28, KeyL: 0x25, KeyM: 0x2E, KeyN: 0x2D, KeyO: 0x1F,
	KeyP: 0x23, KeyQ: 0x0C, KeyR: 0x0F, KeyS: 0x01, KeyT: 0x11,
	KeyU: 0x20, KeyV: 0x09, KeyW: 0x0D, KeyX: 0x07, KeyY: 0x10,
	KeyZ: 0x06,

	KeyF1: 0x7A, KeyF2: 0x78, KeyF3: 0x63, KeyF4: 0x76, KeyF5: 0x60,
	KeyF6: 0x61, KeyF7: 0x62, KeyF8: 0x64, KeyF9: 0x65, KeyF10: 0x6D,
	KeyF11: 0x67, KeyF12: 0x6F,

	KeyEscape: 0x35,
}

// winKeyCodes maps the vocabulary to Windows virtual-key codes.
var winKeyCodes = map[Key]windows.VirtualKey{
	KeyA: 0x41, KeyB: 0x42, KeyC: 0x43, KeyD: 0x44, KeyE: 0x45,
	KeyF: 0x46, KeyG: 0x47, KeyH: 0x48, KeyI: 0x49, KeyJ: 0x4A,
	KeyK: 0x4B, KeyL: 0x4C, KeyM: 0x4D, KeyN: 0x4E, KeyO: 0x4F,
	KeyP: 0x50, KeyQ: 0x51, KeyR: 0x52, KeyS: 0x53, KeyT: 0x54,
	KeyU: 0x55, KeyV: 0x56, KeyW: 0x57, KeyX: 0x58, KeyY: 0x59,
	KeyZ: 0x5A,

	KeyF1: 0x70, KeyF2: 0x71, KeyF3: 0x72, KeyF4: 0x73, KeyF5: 0x74,
	KeyF6: 0x75, KeyF7: 0x76, KeyF8: 0x77, KeyF9: 0x78, KeyF10: 0x79,
	KeyF11: 0x7A, KeyF12: 0x7B,

	KeyEscape: 0x1B,
}

// keyKnown reports whether k is inside the port's key vocabulary.
func keyKnown(k Key) bool {
	_, ok := macKeyCodes[k]
	return ok
}

// MacKeyCode translates a vocabulary key to its macOS virtual key code.
func MacKeyCode(k Key) (macos.KeyCode, error) {
	code, ok := macKeyCodes[k]
	if !ok {
		return 0, fmt.Errorf("platform: key %q is outside the key vocabulary", k)
	}
	return code, nil
}

// MacHotkeyModifiers translates a hotkey's modifiers to the Carbon bit
// values the native registration receives verbatim.
func MacHotkeyModifiers(s HotkeySpec) macos.HotKeyModifiers {
	var m macos.HotKeyModifiers
	if s.Ctrl {
		m |= macos.HotKeyControl
	}
	if s.Alt {
		m |= macos.HotKeyOption
	}
	if s.Shift {
		m |= macos.HotKeyShift
	}
	if s.Meta {
		m |= macos.HotKeyCommand
	}
	return m
}

// MacHotKeySpec translates a port hotkey to the macOS base's spec.
func MacHotKeySpec(s HotkeySpec) (macos.HotKeySpec, error) {
	code, err := MacKeyCode(s.Key)
	if err != nil {
		return macos.HotKeySpec{}, err
	}
	return macos.HotKeySpec{KeyCode: code, Modifiers: MacHotkeyModifiers(s)}, nil
}

// WindowsVK translates a vocabulary key to its Windows virtual-key code.
func WindowsVK(k Key) (windows.VirtualKey, error) {
	vk, ok := winKeyCodes[k]
	if !ok {
		return 0, fmt.Errorf("platform: key %q is outside the key vocabulary", k)
	}
	return vk, nil
}

// WindowsHotkeyMods translates a hotkey's modifiers to the RegisterHotKey
// MOD_* flags. NoRepeat is always added: the stop hotkey must not fire
// again and again while the key is held.
func WindowsHotkeyMods(s HotkeySpec) windows.HotkeyModifiers {
	var m windows.HotkeyModifiers = windows.ModNoRepeat
	if s.Ctrl {
		m |= windows.ModControl
	}
	if s.Alt {
		m |= windows.ModAlt
	}
	if s.Shift {
		m |= windows.ModShift
	}
	if s.Meta {
		m |= windows.ModWin
	}
	return m
}
