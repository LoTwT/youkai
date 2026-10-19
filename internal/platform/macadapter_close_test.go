package platform

import (
	"context"
	"errors"
	"image"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/platform/macos"
)

// M9: the watcher reports each fault code exactly once and stops
// reporting after Close.
func TestMacWatcherReportsEachCodeOnce(t *testing.T) {
	t.Run("focusLost once", func(t *testing.T) {
		w := newMacWorld(t)
		sess := w.openReady(t)
		defer sess.Close()
		w.fg.set(otherPID, nil)
		waitFor(t, 2*time.Second, "focusLost", func() bool { return w.faultCount(CodeFocusLost) > 0 })
		// Several more ticks pass; the code is not delivered again.
		time.Sleep(50 * time.Millisecond)
		if n := w.faultCount(CodeFocusLost); n != 1 {
			t.Fatalf("focusLost delivered %d times, want 1", n)
		}
	})

	t.Run("geometryChanged once", func(t *testing.T) {
		w := newMacWorld(t)
		sess := w.openReady(t)
		defer sess.Close()
		moved := smallGeo()
		moved.WindowFrame = macos.PointRect{X: 300, Y: 50, W: 320, H: 240}
		w.runner.frame(smallFrame(moved, 0))
		waitFor(t, 2*time.Second, "geometryChanged", func() bool { return w.faultCount(CodeGeometryChanged) > 0 })
		time.Sleep(50 * time.Millisecond)
		if n := w.faultCount(CodeGeometryChanged); n != 1 {
			t.Fatalf("geometryChanged delivered %d times, want 1", n)
		}
	})

	t.Run("permissionRevoked once", func(t *testing.T) {
		w := newMacWorld(t)
		sess := w.openReady(t)
		defer sess.Close()
		w.perms.set(false, true)
		waitFor(t, 2*time.Second, "permissionRevoked", func() bool { return w.faultCount(CodePermissionRevoked) > 0 })
		w.perms.set(true, false)
		time.Sleep(50 * time.Millisecond)
		// Both probes share the permissionRevoked code: still once.
		if n := w.faultCount(CodePermissionRevoked); n != 1 {
			t.Fatalf("permissionRevoked delivered %d times, want 1", n)
		}
	})

	t.Run("captureStopped once", func(t *testing.T) {
		w := newMacWorld(t)
		sess := w.openReady(t)
		defer sess.Close()
		w.runner.event(macos.NativeStreamEvent{Kind: macos.NativeEventStopped, Err: errors.New("stream died")})
		waitFor(t, 2*time.Second, "captureStopped", func() bool { return w.faultCount(CodeCaptureStopped) > 0 })
		time.Sleep(50 * time.Millisecond)
		if n := w.faultCount(CodeCaptureStopped); n != 1 {
			t.Fatalf("captureStopped delivered %d times, want 1", n)
		}
	})

	t.Run("observationFailed once", func(t *testing.T) {
		w := newMacWorld(t)
		sess := w.openReady(t)
		defer sess.Close()
		w.enum.push(enumSnapshot{err: errors.New("enumeration timed out")})
		waitFor(t, 2*time.Second, "observationFailed", func() bool { return w.faultCount(CodeObservationFailed) > 0 })
		time.Sleep(50 * time.Millisecond)
		if n := w.faultCount(CodeObservationFailed); n != 1 {
			t.Fatalf("observationFailed delivered %d times, want 1", n)
		}
	})

	t.Run("nothing is delivered after Close", func(t *testing.T) {
		w := newMacWorld(t)
		sess := w.openReady(t)
		sess.Close()
		before := len(w.delivered())
		w.fg.set(otherPID, nil)
		w.perms.set(false, false)
		w.enum.push(enumSnapshot{err: errors.New("boom")})
		time.Sleep(50 * time.Millisecond)
		if got := w.delivered(); len(got) != before {
			t.Fatalf("faults delivered after close: %v", got)
		}
	})

	t.Run("pre-baseline stream faults surface in Observe, not OnFault", func(t *testing.T) {
		w := newMacWorld(t)
		frame := smallFrame(smallGeo(), 0)
		w.runner.mu.Lock()
		w.runner.autoFrame = &frame
		w.runner.mu.Unlock()
		sess, err := w.adapter.Open(context.Background(), w.config())
		if err != nil {
			t.Fatal(err)
		}
		defer sess.Close()
		// A stream fault before the baseline: held for Observe.
		w.runner.event(macos.NativeStreamEvent{Kind: macos.NativeEventStopped, Err: errors.New("died")})
		obs, err := sess.Observe()
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, p := range obs.Problems {
			if p.Code == CodeCaptureStopped {
				found = true
			}
		}
		if !found {
			t.Fatalf("the pre-baseline stream fault must surface in Observe: %+v", obs.Problems)
		}
		if n := w.faultCount(CodeCaptureStopped); n != 0 {
			t.Fatalf("pre-baseline faults must not reach OnFault, got %d", n)
		}
	})
}

// M10: close order, idempotence, dwell and the teardown-timeout error.
func TestMacClose(t *testing.T) {
	t.Run("full teardown and idempotence", func(t *testing.T) {
		w := newMacWorld(t)
		sess := w.openReady(t)
		rep := sess.Close()
		if !rep.CaptureStopped || !rep.Drained || !rep.Released {
			t.Fatalf("teardown facts: %+v", rep)
		}
		if rep.HotkeyErr != nil || rep.Err != nil {
			t.Fatalf("clean close errors: %+v", rep)
		}
		if starts, stops, drains, releases := w.runner.counts(); starts != 1 || stops != 1 || drains != 1 || releases != 1 {
			t.Fatalf("runner counts start=%d stop=%d drain=%d release=%d", starts, stops, drains, releases)
		}
		if _, unreg := w.hotkey.counts(); unreg != 1 {
			t.Fatalf("hotkey unregistered %d times", unreg)
		}
		// The second close returns the same report and does nothing.
		rep2 := sess.Close()
		if rep2.CaptureStopped != rep.CaptureStopped || rep2.Dwell != rep.Dwell {
			t.Fatalf("close must be idempotent: %+v vs %+v", rep2, rep)
		}
		if _, stops, drains, releases := w.runner.counts(); stops != 1 || drains != 1 || releases != 1 {
			t.Fatalf("a second close must not tear down again")
		}
	})

	t.Run("dwell after a recent post, none without posts", func(t *testing.T) {
		w := newMacWorld(t)
		sess := w.openReady(t)
		rep := sess.Close()
		if rep.Dwell != 0 {
			t.Fatalf("a session that never posted must not dwell, dwelled %s", rep.Dwell)
		}

		w2 := newMacWorld(t)
		sess2 := w2.openReady(t)
		if res := sess2.Dispatch(InputOp{Kind: InputClick, Geometry: 1, At: image.Point{10, 10}}); res.Status != InputPosted {
			t.Fatalf("setup click: %+v", res)
		}
		rep2 := sess2.Close()
		if rep2.Dwell < 40*time.Millisecond {
			t.Fatalf("a recent post must dwell ~50ms, dwelled %s", rep2.Dwell)
		}
	})

	t.Run("teardown timeout sets Err and keeps the objects", func(t *testing.T) {
		w := newMacWorld(t)
		w.runner.mu.Lock()
		w.runner.noStopEvt = true
		w.runner.mu.Unlock()
		sess := w.openReady(t)
		rep := sess.Close()
		if rep.Err == nil {
			t.Fatalf("a stop timeout must set Err: %+v", rep)
		}
		if rep.Released {
			t.Fatal("the native objects must not be released on a stop timeout")
		}
	})
}

// M11: during preparation the game not being frontmost is a not-ready
// observation, not an error.
func TestMacPreparePhaseFocusIsAProblemNotAnError(t *testing.T) {
	w := newMacWorld(t)
	frame := smallFrame(smallGeo(), 0)
	w.runner.mu.Lock()
	w.runner.autoFrame = &frame
	w.runner.mu.Unlock()
	sess, err := w.adapter.Open(context.Background(), w.config())
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	w.fg.set(otherPID, nil)
	obs, err := sess.Observe()
	if err != nil {
		t.Fatalf("focus loss is not an observation error: %v", err)
	}
	if obs.Ready {
		t.Fatal("the observation must not be ready without focus")
	}
	if len(obs.Problems) != 1 || obs.Problems[0].Code != CodeFocusLost || obs.Problems[0].Class != ClassPremise {
		t.Fatalf("problems: %+v", obs.Problems)
	}
}
