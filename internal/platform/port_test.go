package platform

import (
	"testing"

	"github.com/LoTwT/youkai/internal/platform/macos"
	"github.com/LoTwT/youkai/internal/platform/windows"
)

// Key and modifier mapping tests (P1): every vocabulary key maps to the
// documented native code on both platforms, the ⌃⌥⌘ combination takes
// the measured modifier value (6400, 实测轮 ①), and keys outside the
// vocabulary are refused instead of guessed.

func TestKeyVocabularyMapsOnBothPlatforms(t *testing.T) {
	keys := []Key{KeyA, KeyB, KeyC, KeyD, KeyE, KeyF, KeyG, KeyH, KeyI, KeyJ,
		KeyK, KeyL, KeyM, KeyN, KeyO, KeyP, KeyQ, KeyR, KeyS, KeyT,
		KeyU, KeyV, KeyW, KeyX, KeyY, KeyZ,
		KeyF1, KeyF2, KeyF3, KeyF4, KeyF5, KeyF6, KeyF7, KeyF8, KeyF9, KeyF10, KeyF11, KeyF12,
		KeyEscape}
	for _, k := range keys {
		if _, err := MacKeyCode(k); err != nil {
			t.Errorf("macOS: %v", err)
		}
		if _, err := WindowsVK(k); err != nil {
			t.Errorf("Windows: %v", err)
		}
	}
	// Spot-check documented values.
	spot := []struct {
		key Key
		mac macos.KeyCode
		win windows.VirtualKey
	}{
		{KeyA, 0x00, 0x41},
		{KeyS, 0x01, 0x53},
		{KeyZ, 0x06, 0x5A},
		{KeyF1, 0x7A, 0x70},
		{KeyF12, 0x6F, 0x7B},
		{KeyEscape, 0x35, 0x1B},
	}
	for _, s := range spot {
		if got, _ := MacKeyCode(s.key); got != s.mac {
			t.Errorf("%s: mac code %#x, want %#x", s.key, got, s.mac)
		}
		if got, _ := WindowsVK(s.key); got != s.win {
			t.Errorf("%s: VK %#x, want %#x", s.key, got, s.win)
		}
	}
}

func TestUnknownKeysAreRefused(t *testing.T) {
	for _, k := range []Key{"", "a", "KeyA", "Space", "Enter", "F13"} {
		if _, err := MacKeyCode(k); err == nil {
			t.Errorf("macOS: key %q must be refused", k)
		}
		if _, err := WindowsVK(k); err == nil {
			t.Errorf("Windows: key %q must be refused", k)
		}
		if keyKnown(k) {
			t.Errorf("key %q must be unknown", k)
		}
	}
}

func TestMacHotkeyModifiersMatchMeasuredCombination(t *testing.T) {
	// ⌃⌥⌘S is the macOS default stop hotkey; the field round measured
	// the registration's modifier value as 6400 (controlKey 1<<12 +
	// optionKey 1<<11 + cmdKey 1<<8).
	spec := HotkeySpec{Key: KeyS, Ctrl: true, Alt: true, Meta: true}
	if got := MacHotkeyModifiers(spec); got != 6400 {
		t.Fatalf("⌃⌥⌘ modifiers = %d, want 6400", got)
	}
	macSpec, err := MacHotKeySpec(spec)
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	if macSpec.KeyCode != 0x01 || macSpec.Modifiers != 6400 {
		t.Fatalf("mac spec: %+v", macSpec)
	}
	if err := macSpec.Validate(); err != nil {
		t.Fatalf("the translated spec must be registrable: %v", err)
	}
}

func TestWindowsHotkeyMods(t *testing.T) {
	// Ctrl+Alt+Shift+S plus NoRepeat (the default avoids the Win key).
	spec := HotkeySpec{Key: KeyS, Ctrl: true, Alt: true, Shift: true}
	want := windows.ModControl | windows.ModAlt | windows.ModShift | windows.ModNoRepeat
	if got := WindowsHotkeyMods(spec); got != want {
		t.Fatalf("mods = %#x, want %#x", got, want)
	}
	// Meta maps to the Win modifier.
	if got := WindowsHotkeyMods(HotkeySpec{Key: KeyF1, Meta: true}); got != windows.ModWin|windows.ModNoRepeat {
		t.Fatalf("meta mods = %#x", got)
	}
}

func TestSessionConfigValidation(t *testing.T) {
	gate := staticGate(true)
	base := SessionConfig{
		Target: TargetSpec{BundleID: "com.example.game"},
		Hotkey: HotkeySpec{Key: KeyS},
		OnStop: func() {},
		Gate:   gate,
	}
	if err := base.validate(); err != nil {
		t.Fatalf("a complete config must validate: %v", err)
	}
	broken := []SessionConfig{
		{},                                  // no target
		{Target: TargetSpec{BundleID: "x"}}, // no hotkey key
		{Target: TargetSpec{BundleID: "x"}, Hotkey: HotkeySpec{Key: "nonsense"}, OnStop: func() {}, Gate: gate},
		{Target: TargetSpec{BundleID: "x"}, Hotkey: HotkeySpec{Key: KeyS}, Gate: gate},        // no OnStop
		{Target: TargetSpec{BundleID: "x"}, Hotkey: HotkeySpec{Key: KeyS}, OnStop: func() {}}, // no gate
		{Target: TargetSpec{ExecutablePath: `C:\game.exe`}, Hotkey: HotkeySpec{Key: KeyS}, OnStop: func() {}, Gate: gate},
	}
	// The last one is valid (Windows-style target); drop it.
	broken = broken[:len(broken)-1]
	for i, cfg := range broken {
		if err := cfg.validate(); err == nil {
			t.Errorf("config %d must be rejected", i)
		}
	}
	if err := (SessionConfig{Target: TargetSpec{ExecutablePath: `C:\game.exe`}, Hotkey: HotkeySpec{Key: KeyS}, OnStop: func() {}, Gate: gate}).validate(); err != nil {
		t.Fatalf("a Windows-style target must validate: %v", err)
	}
}

// staticGate is a fixed InputGate for tests.
type staticGate bool

func (g staticGate) InputPermitted() bool { return bool(g) }

func TestFaultClassTable(t *testing.T) {
	cases := map[string]FaultClass{
		CodePermissionMissing:          ClassSetup,
		CodeHotkeyUnavailable:          ClassSetup,
		CodeTargetNotFound:             ClassSetup,
		CodeScreenRecordingUnavailable: ClassSetup,
		CodeUnsupportedPlatform:        ClassSetup,
		CodeFocusLost:                  ClassPremise,
		CodeTargetHidden:               ClassPremise,
		CodeTargetAmbiguous:            ClassPremise,
		CodePositionOutside:            ClassPremise,
		CodeStaleGeometry:              ClassPremise,
		CodeTargetLost:                 ClassBroken,
		CodeGeometryChanged:            ClassBroken,
		CodeGeometryInvalid:            ClassBroken,
		CodeCaptureStopped:             ClassBroken,
		CodePermissionRevoked:          ClassBroken,
		CodeInputBlocked:               ClassBroken,
		CodeInputFailed:                ClassBroken,
		CodeObservationFailed:          ClassObservation,
	}
	for code, class := range cases {
		if got := ClassOf(code); got != class {
			t.Errorf("ClassOf(%s) = %s, want %s", code, got, class)
		}
		f := NewFault(code, "detail")
		if f.Class != class {
			t.Errorf("NewFault(%s).Class = %s, want %s", code, f.Class, class)
		}
		if f.Error() != code+": detail" {
			t.Errorf("Error() = %q", f.Error())
		}
	}
	if got := ClassOf("somethingNew"); got != ClassPremise {
		t.Fatalf("unknown codes default to premise, got %s", got)
	}
	if f := NewFault(CodeFocusLost, ""); f.Error() != CodeFocusLost {
		t.Fatalf("messageless fault: %q", f.Error())
	}
}

func TestPreflightFaultNamesTheMissingCapability(t *testing.T) {
	if f := (Preflight{Checks: []CapabilityCheck{{Name: "axTrust", OK: true}, {Name: "eventPost", OK: true}}}).Fault(); f != nil {
		t.Fatalf("all passing: %+v", f)
	}
	f := (Preflight{Checks: []CapabilityCheck{{Name: "axTrust", OK: false}, {Name: "eventPost", OK: true}}}).Fault()
	if f == nil || f.Code != CodePermissionMissing || f.Class != ClassSetup {
		t.Fatalf("missing capability: %+v", f)
	}
	if want := "capability axTrust missing"; f.Message != want {
		t.Fatalf("message %q, want %q", f.Message, want)
	}
	// A failed query also counts as missing and names the error.
	f = (Preflight{Checks: []CapabilityCheck{{Name: "dpiAwareness", OK: false, Err: errString("query refused")}}}).Fault()
	if f == nil || f.Message != "capability dpiAwareness unverifiable: query refused" {
		t.Fatalf("unverifiable capability: %+v", f)
	}
}

type errString string

func (e errString) Error() string { return string(e) }
