package platformfake_test

import (
	"testing"

	"github.com/LoTwT/youkai/internal/platform"
	"github.com/LoTwT/youkai/internal/platform/platformfake"
)

type staticGate bool

func (g staticGate) InputPermitted() bool { return bool(g) }

func clickOp() platform.InputOp { return platform.InputOp{Kind: platform.InputClick} }

// The fake must model the real adapters' dispatch order: session closed
// first, then the kernel gate, and only then the scripted DispatchFn.
func TestDispatchHonorsGateAndClose(t *testing.T) {
	t.Run("closed gate refuses before the scripted game", func(t *testing.T) {
		called := false
		s := platformfake.NewSession()
		s.Configure(platform.SessionConfig{Gate: staticGate(false)})
		s.DispatchFn = func(platform.InputOp) platform.InputResult {
			called = true
			return platform.InputResult{Status: platform.InputPosted}
		}

		res := s.Dispatch(clickOp())
		if res.Status != platform.InputNotDispatched || res.Denial == nil || res.Denial.Code != platform.CodePermitClosed {
			t.Fatalf("want notDispatched permitClosed, got %+v", res)
		}
		if called {
			t.Fatal("the scripted game must not receive input after the gate closed")
		}
		if _, _, _, attempts, _ := s.Counters(); attempts != 1 {
			t.Fatalf("dispatch attempts = %d, want 1 (the attempt is evidence)", attempts)
		}
	})

	t.Run("open gate reaches the scripted game", func(t *testing.T) {
		called := false
		s := platformfake.NewSession()
		s.Configure(platform.SessionConfig{Gate: staticGate(true)})
		s.DispatchFn = func(platform.InputOp) platform.InputResult {
			called = true
			return platform.InputResult{Status: platform.InputPosted, EventsPlanned: 1, EventsPosted: 1}
		}

		res := s.Dispatch(clickOp())
		if res.Status != platform.InputPosted || !called {
			t.Fatalf("want posted through the script, got %+v (called=%v)", res, called)
		}
	})

	t.Run("close wins over an open gate", func(t *testing.T) {
		called := false
		s := platformfake.NewSession()
		s.Configure(platform.SessionConfig{Gate: staticGate(true)})
		s.DispatchFn = func(platform.InputOp) platform.InputResult {
			called = true
			return platform.InputResult{Status: platform.InputPosted}
		}
		s.Close()

		res := s.Dispatch(clickOp())
		if res.Status != platform.InputNotDispatched || res.Denial == nil || res.Denial.Code != platform.CodeCaptureStopped {
			t.Fatalf("want notDispatched captureStopped, got %+v", res)
		}
		if called {
			t.Fatal("the scripted game must not receive input after Close")
		}
	})
}
