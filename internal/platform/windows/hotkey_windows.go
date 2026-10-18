//go:build windows

package windows

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// HotkeyLoop runs the stop hotkey on one locked OS thread.
// RegisterHotKey with a NULL hwnd binds the hotkey to the registering
// thread — its WM_HOTKEY messages are posted to that thread's queue —
// so registration, the message loop and the unregistration all happen
// on the same thread. The loop is the only owner of that thread; when
// it exits, the hotkey is unregistered first, so nothing leaks.
//
// The loop keeps running while the app is hidden: it depends on no
// window and takes no focus, which is what keeps the stop entry
// reachable after the UI disappears. A trigger executes the stop
// callback on the loop thread; the callback must return quickly (it
// requests the stop, it does not run the wrap-up).
type HotkeyLoop struct {
	mgr      *HotkeyManager
	threadID atomic.Uint32
	regErr   chan error
	done     chan struct{}
	stopOnce sync.Once
	loopErr  atomic.Pointer[error]
}

// StartHotkeyLoop starts the hotkey thread and waits for the
// registration outcome: a failure — including a conflict with a hotkey
// already registered elsewhere in the system — is returned here, before
// any session starts, instead of being discovered mid-scan. The error is
// a CodeHotkeyUnavailable PlatformFault so the caller can fail the
// precheck with the diagnostic code intact.
func StartHotkeyLoop(mgr *HotkeyManager, api HotkeyAPI) (*HotkeyLoop, error) {
	l := &HotkeyLoop{
		mgr:    mgr,
		regErr: make(chan error, 1),
		done:   make(chan struct{}),
	}
	go l.run()
	if err := <-l.regErr; err != nil {
		// The thread exits without a loop on a failed registration.
		<-l.done
		return nil, NewFault(CodeHotkeyUnavailable, "stop hotkey registration failed", err, PhasePrecheck, time.Now(), 0)
	}
	return l, nil
}

func (l *HotkeyLoop) run() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	l.threadID.Store(windows.GetCurrentThreadId())

	if err := l.mgr.Register(); err != nil {
		l.regErr <- err
		close(l.done)
		return
	}
	l.regErr <- nil

	var loopErr error
	for {
		var m msg
		ret, _, callErr := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(ret) == -1 {
			loopErr = fmt.Errorf("GetMessage: %w", callErrno(callErr))
			break
		}
		if ret == 0 { // WM_QUIT
			break
		}
		if m.message == wmHotkey {
			l.mgr.DeliverTrigger()
		}
		// Any other message is ignored.
	}
	// Unregister on the thread that registered, before it exits.
	l.mgr.Unregister()
	l.loopErr.Store(&loopErr)
	close(l.done)
}

// Stop asks the loop thread to quit and waits until the thread has
// unregistered the hotkey and exited. It returns the loop error when
// the message loop itself failed; the unregistration outcome is kept by
// the manager's report. Stop is idempotent.
func (l *HotkeyLoop) Stop() error {
	l.stopOnce.Do(func() {
		if id := l.threadID.Load(); id != 0 {
			// A failing post (thread already gone) is harmless: the done
			// channel still closes when the loop thread exits.
			procPostThreadMessageW.Call(uintptr(id), wmQuit, 0, 0)
		}
	})
	<-l.done
	if p := l.loopErr.Load(); p != nil && *p != nil {
		return *p
	}
	return nil
}
