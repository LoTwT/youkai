package platform

import (
	"context"
	"errors"
	"image"
	"testing"

	"github.com/LoTwT/youkai/internal/platform/macos"
)

// M-precheck: the macOS cloud client's pre-start diagnostics (T2 §3.2).
// A window that straddles or lies outside every display is targetOffscreen
// (not the generic targetNotFound), and a window whose point size is not
// the accepted configuration is windowSizeUnsupported.

func TestMacTargetOffscreen(t *testing.T) {
	w := newMacWorld(t)
	off := targetWindow()
	off.Frame = macos.PointRect{X: 1800, Y: 100, W: 320, H: 240} // extends past 1920
	w.enum.push(enumSnapshot{
		windows:  []macos.NativeWindow{off, bystanderWindow()},
		displays: testDisplays(),
	})
	_, err := w.adapter.Open(context.Background(), w.config())
	var f *Fault
	if !errors.As(err, &f) || f.Code != CodeTargetOffscreen || f.Class != ClassSetup {
		t.Fatalf("want targetOffscreen, got %v", err)
	}
	if f.Params["count"] != "1" || f.Params["window"] == "" {
		t.Fatalf("offscreen params: %v", f.Params)
	}
	if _, unreg := w.hotkey.counts(); unreg != 1 {
		t.Fatalf("hotkey rollback: %d", unreg)
	}
}

func TestMacTargetNotFoundStillDistinct(t *testing.T) {
	w := newMacWorld(t)
	w.enum.push(enumSnapshot{
		windows:  []macos.NativeWindow{bystanderWindow()},
		displays: testDisplays(),
	})
	_, err := w.adapter.Open(context.Background(), w.config())
	var f *Fault
	if !errors.As(err, &f) || f.Code != CodeTargetNotFound {
		t.Fatalf("want targetNotFound, got %v", err)
	}
}

func TestMacWindowSizePrecheck(t *testing.T) {
	t.Run("wrong size is refused before capture", func(t *testing.T) {
		w := newMacWorld(t)
		w.adapter.RequiredWindowSize = image.Pt(1164, 762)
		_, err := w.adapter.Open(context.Background(), w.config())
		var f *Fault
		if !errors.As(err, &f) || f.Code != CodeWindowSizeUnsupported || f.Class != ClassSetup {
			t.Fatalf("want windowSizeUnsupported, got %v", err)
		}
		if f.Params["expected"] != "1164x762" || f.Params["current"] != "320.0x240.0" {
			t.Fatalf("size params: %v", f.Params)
		}
		if starts, _, _, _ := w.runner.counts(); starts != 0 {
			t.Fatalf("no stream may start on a wrong window size, started %d", starts)
		}
	})
	t.Run("accepted size passes", func(t *testing.T) {
		w := newMacWorld(t)
		w.adapter.RequiredWindowSize = image.Pt(320, 240)
		frame := smallFrame(smallGeo(), 0)
		w.runner.mu.Lock()
		w.runner.autoFrame = &frame
		w.runner.mu.Unlock()
		sess, err := w.adapter.Open(context.Background(), w.config())
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		sess.Close()
	})
}
