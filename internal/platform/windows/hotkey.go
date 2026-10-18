package windows

import (
	"errors"
	"fmt"
	"sync"
)

// HotkeyModifiers are the RegisterHotKey MOD_* combination flags.
type HotkeyModifiers uint32

const (
	ModAlt     HotkeyModifiers = 0x0001
	ModControl HotkeyModifiers = 0x0002
	ModShift   HotkeyModifiers = 0x0004
	ModWin     HotkeyModifiers = 0x0008
	// ModNoRepeat suppresses auto-repeat while the key is held.
	ModNoRepeat HotkeyModifiers = 0x4000
)

// HotkeyState is the registration state of the stop hotkey.
type HotkeyState string

const (
	// HotkeyIdle: not registered (before start or after close).
	HotkeyIdle HotkeyState = "idle"
	// HotkeyRegistered: registered and delivering triggers.
	HotkeyRegistered HotkeyState = "registered"
	// HotkeyClosed: unregistered after use; triggers are no longer
	// delivered.
	HotkeyClosed HotkeyState = "closed"
)

// HotkeyAPI is the RegisterHotKey surface. The Win32 implementation is
// thread-bound: RegisterHotKey with a NULL hwnd associates the hotkey
// with the registering thread, so the real wiring registers and
// unregisters on one dedicated thread (see hotkey_windows.go).
type HotkeyAPI interface {
	RegisterHotKey(id int, mods HotkeyModifiers, vk VirtualKey) error
	UnregisterHotKey(id int) error
}

// HotkeyManager keeps the global stop hotkey: registration before the
// session starts, WM_HOTKEY deliveries to the unified stop entry, and
// clean unregistration. The manager is the pure state machine; the
// message-loop plumbing lives in the windows-only files. Delivering a
// trigger never activates a window — RegisterHotKey posts to the
// registering thread's queue and takes no focus, which is what keeps
// the stop entry usable while the app is hidden.
type HotkeyManager struct {
	mu        sync.Mutex
	api       HotkeyAPI
	id        int
	mods      HotkeyModifiers
	vk        VirtualKey
	onTrigger func()
	state     HotkeyState
	triggers  int
	late      int
	lastErr   error
}

// NewHotkeyManager builds a manager in the idle state. onTrigger is the
// unified stop entry; it runs on the hotkey thread and must return
// quickly — it records the stop request, it does not run the wrap-up,
// and it must not call the loop's Stop (that would wait on the very
// thread executing the callback).
func NewHotkeyManager(api HotkeyAPI, id int, mods HotkeyModifiers, vk VirtualKey, onTrigger func()) (*HotkeyManager, error) {
	if vk == 0 {
		return nil, errors.New("stop hotkey has no virtual-key code")
	}
	hasModifier := mods&(ModAlt|ModControl|ModShift|ModWin) != 0
	isFunctionKey := vk >= 0x70 && vk <= 0x87 // VK_F1..VK_F24
	if !hasModifier && !isFunctionKey {
		return nil, fmt.Errorf("stop hotkey vk 0x%02x has no modifier; a bare key would swallow normal typing", vk)
	}
	return &HotkeyManager{
		api:       api,
		id:        id,
		mods:      mods,
		vk:        vk,
		onTrigger: onTrigger,
		state:     HotkeyIdle,
	}, nil
}

// Register registers the hotkey. A failure — including a conflict with
// an existing registration elsewhere in the system — surfaces here,
// before any session starts, instead of being discovered mid-scan.
func (m *HotkeyManager) Register() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state != HotkeyIdle {
		return fmt.Errorf("hotkey state %s cannot register", m.state)
	}
	if err := m.api.RegisterHotKey(m.id, m.mods, m.vk); err != nil {
		m.lastErr = err
		return fmt.Errorf("register stop hotkey id %d: %w", m.id, err)
	}
	m.state = HotkeyRegistered
	return nil
}

// DeliverTrigger reports one WM_HOTKEY arrival. While registered it
// invokes the stop entry and counts the delivery; after close it only
// counts the late arrival — a stop entry must be idempotent anyway, but
// the platform does not forward system traffic it is no longer
// registered for.
func (m *HotkeyManager) DeliverTrigger() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state != HotkeyRegistered {
		m.late++
		return false
	}
	m.triggers++
	if m.onTrigger != nil {
		m.onTrigger()
	}
	return true
}

// Unregister unregisters the hotkey. It is idempotent; a failure is
// recorded and returned, reported honestly instead of assumed clean.
// In the real wiring the loop thread unregisters before it exits; a
// later call is a no-op.
func (m *HotkeyManager) Unregister() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state != HotkeyRegistered {
		return nil
	}
	if err := m.api.UnregisterHotKey(m.id); err != nil {
		m.lastErr = err
		m.state = HotkeyClosed
		return fmt.Errorf("unregister stop hotkey id %d: %w", m.id, err)
	}
	m.state = HotkeyClosed
	return nil
}

// State reports the current registration state.
func (m *HotkeyManager) State() HotkeyState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}

// TriggerCount reports how many triggers were delivered to the stop
// entry while registered.
func (m *HotkeyManager) TriggerCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.triggers
}

// LateCount reports trigger arrivals after close (expected zero; a
// nonzero value is a real-machine observation worth reporting).
func (m *HotkeyManager) LateCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.late
}

// LastError reports the last registration or unregistration error, nil
// when none occurred.
func (m *HotkeyManager) LastError() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastErr
}
