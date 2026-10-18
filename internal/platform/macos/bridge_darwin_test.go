//go:build darwin

package macos

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// Main-queue dispatch gate tests (实测轮 ① 报告 §12): the pure logic
// of the in-flight marker — pass-through when free, rejection while a
// dispatched block is unconfirmed, and release once the recovery probe
// ran behind the late block. The native dispatch wiring itself (cgo,
// main run loop) is a real-device path and is not exercised here.

func TestMainQueueGatePassesWhenFree(t *testing.T) {
	var g mainQueueGate
	if err := g.acquire("frontmost query"); err != nil {
		t.Fatalf("free gate rejected: %v", err)
	}
	// The dispatched block was confirmed to run; the next dispatch is
	// admitted again.
	g.release()
	if err := g.acquire("frontmost query"); err != nil {
		t.Fatalf("released gate rejected: %v", err)
	}
}

func TestMainQueueGateRejectsWhileInFlight(t *testing.T) {
	var g mainQueueGate
	if err := g.acquire("frontmost query"); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	err := g.acquire("hotkey register")
	if err == nil {
		t.Fatal("second dispatch accepted while in flight")
	}
	// The rejection names the stuck dispatch, so a field report says
	// which entry point hit the backed-up queue.
	msg := err.Error()
	if !strings.Contains(msg, "still in flight") || !strings.Contains(msg, "frontmost query") {
		t.Fatalf("rejection %q lacks the in-flight reason", msg)
	}
}

func TestMainQueueGateRecoversAfterLateBlockRan(t *testing.T) {
	var g mainQueueGate
	if err := g.acquire("hotkey unregister"); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	// The dispatch timed out: its block is still queued, so the marker
	// stays held and later calls keep failing — without queueing and
	// without waiting out their own timeout.
	if err := g.acquire("frontmost query"); err == nil {
		t.Fatal("dispatch accepted behind an unconfirmed block")
	}
	// The recovery probe waits for its own block to run; a probe whose
	// bound expires is retried. Here two probes time out before the
	// queue finally drains past the late block.
	calls := 0
	g.awaitRecovery(func() bool {
		calls++
		return calls == 3
	})
	if calls != 3 {
		t.Fatalf("probe calls %d, want 3", calls)
	}
	// The probe ran behind the late block, so the late block has
	// executed and the marker is free again — no manual reset.
	if err := g.acquire("frontmost query"); err != nil {
		t.Fatalf("gate not recovered: %v", err)
	}
}

func TestMainQueueGateAdmitsExactlyOneConcurrentDispatch(t *testing.T) {
	var g mainQueueGate
	const goroutines = 8
	var admitted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := g.acquire("frontmost query"); err == nil {
				admitted.Add(1)
			}
		}()
	}
	wg.Wait()
	// Nothing released the marker, so exactly one of the concurrent
	// dispatches was admitted; the rest were rejected without
	// queueing.
	if got := admitted.Load(); got != 1 {
		t.Fatalf("admitted %d concurrent dispatches, want 1", got)
	}
}
