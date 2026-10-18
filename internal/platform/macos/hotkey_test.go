package macos_test

import (
	"errors"
	"testing"

	macos "github.com/LoTwT/youkai/internal/platform/macos"
)

// Global stop hotkey tests (T2 §5): the spec validates, registration
// failures surface instead of being swallowed, the trigger fires the
// stop callback, and the native lifecycle (register/unregister) is
// driven exactly once. The real native registration (Carbon, main run
// loop) is a real-device path (实测轮 ①); these tests use fakes.

// fakeRegistrar is a scriptable NativeHotKeyRegistrar.
type fakeRegistrar struct {
	registerErr   error
	unregisterErr error
	triggers      map[int64]func()
	nextID        int64
	refs          []int64
	calls         []string
}

func (r *fakeRegistrar) Register(spec macos.HotKeySpec, onTrigger func()) (macos.NativeHotKeyHandle, error) {
	r.calls = append(r.calls, "register")
	if r.registerErr != nil {
		return nil, r.registerErr
	}
	r.nextID++
	if r.triggers == nil {
		r.triggers = make(map[int64]func())
	}
	r.triggers[r.nextID] = onTrigger
	r.refs = append(r.refs, r.nextID)
	return r.nextID, nil
}

func (r *fakeRegistrar) Unregister(handle macos.NativeHotKeyHandle) error {
	r.calls = append(r.calls, "unregister")
	if r.unregisterErr != nil {
		return r.unregisterErr
	}
	id, ok := handle.(int64)
	if !ok {
		return errors.New("unknown handle")
	}
	delete(r.triggers, id)
	return nil
}

// trigger fires every currently registered hotkey.
func (r *fakeRegistrar) trigger() {
	for _, fn := range r.triggers {
		fn()
	}
}

func TestHotKeySpecValidation(t *testing.T) {
	if err := (macos.HotKeySpec{KeyCode: 0, Modifiers: macos.HotKeyCommand}).Validate(); err == nil {
		t.Fatal("zero key code accepted")
	}
	// A bare function key without modifiers is legitimate.
	if err := (macos.HotKeySpec{KeyCode: 96}).Validate(); err != nil {
		t.Fatalf("bare key rejected: %v", err)
	}
	if s := (macos.HotKeySpec{KeyCode: 96, Modifiers: macos.HotKeyCommand | macos.HotKeyShift}).String(); s != "cmd+shift+key(96)" {
		t.Fatalf("spec string %q", s)
	}
}

func TestRegisterStopHotKeyLifecycle(t *testing.T) {
	reg := &fakeRegistrar{}
	stopped := make(chan struct{}, 4)
	hk, err := macos.RegisterStopHotKey(reg, macos.HotKeySpec{KeyCode: 96, Modifiers: macos.HotKeyCommand}, func() {
		stopped <- struct{}{}
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if hk.Spec().KeyCode != 96 {
		t.Fatalf("spec %+v", hk.Spec())
	}
	// The trigger reaches the stop callback; nothing else in the test
	// observes focus, and the native contract (no focus stealing) is
	// the registrar implementation's to keep.
	reg.trigger()
	select {
	case <-stopped:
	default:
		t.Fatal("trigger did not reach the stop callback")
	}
	// Unregister once; repeated Close is idempotent.
	if err := hk.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := hk.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
	if len(reg.calls) != 2 || reg.calls[0] != "register" || reg.calls[1] != "unregister" {
		t.Fatalf("calls %v", reg.calls)
	}
	// After unregistration the trigger no longer fires the callback.
	reg.trigger()
	select {
	case <-stopped:
		t.Fatal("unregistered hotkey still fires")
	default:
	}
}

func TestRegisterStopHotKeyFailureSurfaces(t *testing.T) {
	// A registration failure (conflict with an existing hotkey, no run
	// loop, …) must surface to the caller instead of being swallowed,
	// because the stop entry is a start precondition.
	reg := &fakeRegistrar{registerErr: errors.New("eventHotKeyExistsErr")}
	if _, err := macos.RegisterStopHotKey(reg, macos.HotKeySpec{KeyCode: 96}, func() {}); err == nil {
		t.Fatal("registration failure swallowed")
	}
	// Invalid specs and missing wiring fail before reaching the
	// registrar.
	if _, err := macos.RegisterStopHotKey(reg, macos.HotKeySpec{KeyCode: 0}, func() {}); err == nil {
		t.Fatal("invalid spec accepted")
	}
	if _, err := macos.RegisterStopHotKey(nil, macos.HotKeySpec{KeyCode: 96}, func() {}); err == nil {
		t.Fatal("nil registrar accepted")
	}
	if _, err := macos.RegisterStopHotKey(reg, macos.HotKeySpec{KeyCode: 96}, nil); err == nil {
		t.Fatal("nil stop callback accepted")
	}
}
