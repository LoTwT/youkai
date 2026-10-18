package macos

import (
	"errors"
	"fmt"
	"sync"
)

// The global stop hotkey (T2 §5): a native background global
// registration — Carbon RegisterEventHotKey on the darwin bridge — so
// the stop entry works while the scanner is hidden and the game holds
// focus. This is deliberately not a MyGo in-window key handler or a
// menu shortcut: those only fire while the scanner itself has focus
// and are never a global stop. Triggering the hotkey does not steal
// focus; registration failures are reported, not swallowed; the final
// key combination is chosen during M2 real-device testing and is a
// caller decision, not a default baked in here.

// HotKeyModifiers is the modifier set of a hotkey, using the Carbon
// bit values (HIToolbox Events.h) so the native registration receives
// them verbatim: cmdKey 1<<8, shiftKey 1<<9, optionKey 1<<11,
// controlKey 1<<12.
type HotKeyModifiers uint32

const (
	HotKeyCommand HotKeyModifiers = 1 << 8
	HotKeyShift   HotKeyModifiers = 1 << 9
	HotKeyOption  HotKeyModifiers = 1 << 11
	HotKeyControl HotKeyModifiers = 1 << 12
)

// HotKeySpec is one global hotkey: a virtual key code (the shared
// Carbon/CGEvent kVK_* numbering) plus modifiers. A bare key without
// modifiers is allowed (plain function-key stops are legitimate) but
// is usually a poor default for printable keys; callers pick the
// combination.
type HotKeySpec struct {
	KeyCode   KeyCode
	Modifiers HotKeyModifiers
}

// Validate reports whether the spec is registrable: the key code must
// be non-zero. Everything else — including conflicts with existing
// system or app hotkeys — is reported by the native registration
// itself, because only the system knows.
func (s HotKeySpec) Validate() error {
	if s.KeyCode == 0 {
		return errors.New("macos: hotkey requires a non-zero key code")
	}
	return nil
}

// String renders the spec for diagnostics.
func (s HotKeySpec) String() string {
	m := ""
	if s.Modifiers&HotKeyCommand != 0 {
		m += "cmd+"
	}
	if s.Modifiers&HotKeyControl != 0 {
		m += "ctrl+"
	}
	if s.Modifiers&HotKeyOption != 0 {
		m += "opt+"
	}
	if s.Modifiers&HotKeyShift != 0 {
		m += "shift+"
	}
	return fmt.Sprintf("%skey(%d)", m, uint32(s.KeyCode))
}

// NativeHotKeyRegistrar is the native registration surface. Register
// must report system rejections (a conflict with an existing hotkey,
// for example) as errors instead of silently failing, and delivering
// onTrigger must not activate the registering process or steal focus.
type NativeHotKeyRegistrar interface {
	// Register registers the hotkey globally and returns an opaque
	// handle. onTrigger fires on the hotkey from then on until
	// Unregister; it must not block.
	Register(spec HotKeySpec, onTrigger func()) (NativeHotKeyHandle, error)
	// Unregister removes the registration; it is only called with
	// handles Register returned.
	Unregister(handle NativeHotKeyHandle) error
}

// NativeHotKeyHandle is the opaque native hotkey reference.
type NativeHotKeyHandle interface{}

// StopHotKey owns one registered global stop hotkey. onStop runs when
// the hotkey fires — typically a request into the same idempotent stop
// entry as the stop button and the quit path — and Close unregisters
// exactly once.
type StopHotKey struct {
	registrar NativeHotKeyRegistrar
	handle    NativeHotKeyHandle
	spec      HotKeySpec

	mu       sync.Mutex
	closed   bool
	closeErr error
}

// RegisterStopHotKey validates spec, registers it natively and returns
// the owner. A registration error is returned to the caller verbatim:
// the start flow must treat a missing stop hotkey as a failed
// precondition (T2 §5: 注册失败/冲突要在开始前处理).
func RegisterStopHotKey(registrar NativeHotKeyRegistrar, spec HotKeySpec, onStop func()) (*StopHotKey, error) {
	if registrar == nil {
		return nil, errors.New("macos: stop hotkey requires a registrar")
	}
	if onStop == nil {
		return nil, errors.New("macos: stop hotkey requires a stop callback")
	}
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	handle, err := registrar.Register(spec, onStop)
	if err != nil {
		return nil, fmt.Errorf("macos: registering stop hotkey %s: %w", spec, err)
	}
	return &StopHotKey{registrar: registrar, handle: handle, spec: spec}, nil
}

// Spec returns the registered hotkey spec.
func (h *StopHotKey) Spec() HotKeySpec { return h.spec }

// Close unregisters the hotkey. It is idempotent and returns the first
// result; unregistering is not required at process exit (the system
// cleans up), so a failed late Close is reportable but not fatal.
func (h *StopHotKey) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return h.closeErr
	}
	h.closed = true
	h.closeErr = h.registrar.Unregister(h.handle)
	return h.closeErr
}
