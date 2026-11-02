package platform

import (
	"context"
	"errors"
	"image"
	"testing"

	"github.com/LoTwT/youkai/internal/platform/windows"
)

// W5-W9: the two-stage Windows precheck (T2 §3.2, D-1/D-2). All tests are
// fake-driven: the native surfaces are the same fakes the adapter tests
// use, and no Win32 call is made.

func policyWithKnown(extra ...ClientTier) *WindowDisplayPolicy {
	p := testDisplayPolicy()
	p.KnownTiers = append(p.KnownTiers, extra...)
	return p
}

func problemCode(t *testing.T, problems []*Fault) *Fault {
	t.Helper()
	if len(problems) != 1 {
		t.Fatalf("want one problem, got %+v", problems)
	}
	return problems[0]
}

func TestWindowDisplayPolicyValidate(t *testing.T) {
	base := testDisplayPolicy()
	if err := base.validate(); err != nil {
		t.Fatalf("base policy: %v", err)
	}
	cases := []struct {
		name string
		mut  func(*WindowDisplayPolicy)
	}{
		{"no known tiers", func(p *WindowDisplayPolicy) { p.KnownTiers = nil }},
		{"open tier unknown", func(p *WindowDisplayPolicy) {
			p.OpenTiers = []ClientTier{{Name: "2560x1440", Client: image.Pt(2560, 1440)}}
		}},
		{"open tier name mismatch", func(p *WindowDisplayPolicy) {
			p.OpenTiers[0].Name = "other"
		}},
		{"missing probes", func(p *WindowDisplayPolicy) { p.Probes = nil }},
		{"empty probes", func(p *WindowDisplayPolicy) {
			p.Probes[image.Pt(1920, 1080)] = WindowProbes{}
		}},
		{"probe outside client", func(p *WindowDisplayPolicy) {
			p.Probes[image.Pt(1920, 1080)] = WindowProbes{Occlusion: []ProbePoint{{Name: "x", At: image.Pt(1920, 10)}}}
		}},
		{"duplicate probe name", func(p *WindowDisplayPolicy) {
			p.Probes[image.Pt(1920, 1080)] = WindowProbes{Occlusion: []ProbePoint{
				{Name: "x", At: image.Pt(1, 1)}, {Name: "x", At: image.Pt(2, 2)},
			}}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := *base
			p.KnownTiers = append([]ClientTier(nil), base.KnownTiers...)
			p.OpenTiers = append([]ClientTier(nil), base.OpenTiers...)
			p.Probes = map[image.Point]WindowProbes{image.Pt(1920, 1080): {Occlusion: []ProbePoint{{Name: "x", At: image.Pt(1, 1)}}}}
			tc.mut(&p)
			if err := p.validate(); err == nil {
				t.Fatalf("policy passed validation")
			}
		})
	}
}

func TestWinDisplayTierChecks(t *testing.T) {
	t.Run("known but not opened tier", func(t *testing.T) {
		w := newWinWorld(t)
		w.geo.mu.Lock()
		w.geo.clientSize = windows.PixelPoint{X: 2560, Y: 1440}
		w.geo.origin = windows.PixelPoint{X: 0, Y: 0}
		w.geo.monitor = windows.PixelRect{Max: windows.PixelPoint{X: 2560, Y: 1440}}
		w.geo.workArea = windows.PixelRect{Max: windows.PixelPoint{X: 2560, Y: 1440}}
		w.geo.mu.Unlock()
		cfg := w.config()
		cfg.Display = policyWithKnown(ClientTier{Name: "2560x1440", Client: image.Pt(2560, 1440)})
		_, err := w.adapter.Open(context.Background(), cfg)
		var f *Fault
		if !errors.As(err, &f) || f.Code != CodeClientTierNotOpened || f.Class != ClassSetup {
			t.Fatalf("want clientTierNotOpened, got %v", err)
		}
		if f.Params["current"] != "2560x1440" || f.Params["tier"] != "2560x1440" {
			t.Fatalf("tier params: %v", f.Params)
		}
	})
	t.Run("unknown client size", func(t *testing.T) {
		w := newWinWorld(t)
		w.geo.mu.Lock()
		w.geo.clientSize = windows.PixelPoint{X: 800, Y: 600}
		w.geo.monitor = windows.PixelRect{Max: windows.PixelPoint{X: 800, Y: 600}}
		w.geo.workArea = windows.PixelRect{Max: windows.PixelPoint{X: 800, Y: 600}}
		w.geo.mu.Unlock()
		_, err := w.adapter.Open(context.Background(), w.config())
		var f *Fault
		if !errors.As(err, &f) || f.Code != CodeClientTierUnsupported {
			t.Fatalf("want clientTierUnsupported, got %v", err)
		}
		if f.Params["current"] != "800x600" {
			t.Fatalf("size params: %v", f.Params)
		}
	})
	t.Run("exact opened tier passes", func(t *testing.T) {
		w := newWinWorld(t)
		sess := w.openReady(t)
		sess.Close()
	})
}

func TestWinScreenContainment(t *testing.T) {
	t.Run("inside the work area passes", func(t *testing.T) {
		w := newWinWorld(t)
		w.geo.mu.Lock()
		w.geo.origin = windows.PixelPoint{X: 0, Y: 0}
		w.geo.monitor = windows.PixelRect{Max: windows.PixelPoint{X: 1920, Y: 1200}}
		w.geo.workArea = windows.PixelRect{Max: windows.PixelPoint{X: 1920, Y: 1080}}
		w.geo.mu.Unlock()
		sess := w.openReady(t)
		sess.Close()
	})
	t.Run("client exactly the monitor passes borderless", func(t *testing.T) {
		w := newWinWorld(t)
		w.geo.mu.Lock()
		w.geo.origin = windows.PixelPoint{X: 0, Y: 0}
		w.geo.monitor = windows.PixelRect{Max: windows.PixelPoint{X: 1920, Y: 1080}}
		w.geo.workArea = windows.PixelRect{Max: windows.PixelPoint{X: 1920, Y: 1040}}
		w.geo.mu.Unlock()
		sess := w.openReady(t)
		sess.Close()
	})
	t.Run("inside the monitor but outside the work area is refused", func(t *testing.T) {
		w := newWinWorld(t)
		w.geo.mu.Lock()
		w.geo.origin = windows.PixelPoint{X: 0, Y: 100}
		w.geo.monitor = windows.PixelRect{Max: windows.PixelPoint{X: 1920, Y: 1200}}
		w.geo.workArea = windows.PixelRect{Max: windows.PixelPoint{X: 1920, Y: 1080}}
		w.geo.mu.Unlock()
		_, err := w.adapter.Open(context.Background(), w.config())
		var f *Fault
		if !errors.As(err, &f) || f.Code != CodeWindowOutsideWorkArea {
			t.Fatalf("want windowOutsideWorkArea, got %v", err)
		}
		if f.Params["workArea"] != "0,0 1920x1080" {
			t.Fatalf("work-area params: %v", f.Params)
		}
	})
	t.Run("client straddling the monitor is refused", func(t *testing.T) {
		w := newWinWorld(t)
		w.geo.mu.Lock()
		w.geo.origin = windows.PixelPoint{X: 100, Y: 0}
		w.geo.monitor = windows.PixelRect{Max: windows.PixelPoint{X: 1920, Y: 1080}}
		w.geo.workArea = windows.PixelRect{Max: windows.PixelPoint{X: 1920, Y: 1080}}
		w.geo.mu.Unlock()
		_, err := w.adapter.Open(context.Background(), w.config())
		var f *Fault
		if !errors.As(err, &f) || f.Code != CodeWindowOutsideWorkArea {
			t.Fatalf("want windowOutsideWorkArea, got %v", err)
		}
	})
}

func TestWinIntegrityGate(t *testing.T) {
	t.Run("target higher than scanner is refused", func(t *testing.T) {
		w := newWinWorld(t)
		w.integrity.target = windows.IntegrityHigh
		w.integrity.self = windows.IntegrityMedium
		_, err := w.adapter.Open(context.Background(), w.config())
		var f *Fault
		if !errors.As(err, &f) || f.Code != CodeIntegrityMismatch || f.Class != ClassSetup {
			t.Fatalf("want integrityMismatch, got %v", err)
		}
		if f.Params["target"] != "high" || f.Params["self"] != "medium" {
			t.Fatalf("integrity params: %v", f.Params)
		}
	})
	t.Run("equal integrity passes", func(t *testing.T) {
		w := newWinWorld(t)
		sess := w.openReady(t)
		sess.Close()
	})
	t.Run("lower target passes", func(t *testing.T) {
		w := newWinWorld(t)
		w.integrity.target = windows.IntegrityLow
		w.integrity.self = windows.IntegrityMedium
		sess := w.openReady(t)
		sess.Close()
	})
	t.Run("target query failure is fail-closed", func(t *testing.T) {
		w := newWinWorld(t)
		w.integrity.targetErr = errors.New("OpenProcess: access denied")
		_, err := w.adapter.Open(context.Background(), w.config())
		var f *Fault
		if !errors.As(err, &f) || f.Code != CodeIntegrityUnverifiable || f.Class != ClassSetup {
			t.Fatalf("want integrityUnverifiable, got %v", err)
		}
		if f.Params["detail"] == "" {
			t.Fatalf("query failure detail missing: %v", f.Params)
		}
	})
	t.Run("self query failure is fail-closed", func(t *testing.T) {
		w := newWinWorld(t)
		w.integrity.selfErr = errors.New("OpenProcessToken: access denied")
		_, err := w.adapter.Open(context.Background(), w.config())
		var f *Fault
		if !errors.As(err, &f) || f.Code != CodeIntegrityUnverifiable {
			t.Fatalf("want integrityUnverifiable, got %v", err)
		}
	})
}

func TestWinOcclusionPrecheck(t *testing.T) {
	w := newWinWorld(t)
	// The target policy's detail probe is client (960,540); the fake
	// client origin is (10,20).
	at := windows.PixelPoint{X: 10 + 960, Y: 20 + 540}
	w.win.mu.Lock()
	w.win.wnds = append(w.win.wnds, &fakeWnd{
		hwnd: 99, pid: 7, visible: true, className: "Shell_TrayWnd", title: "任务栏",
	})
	w.win.mu.Unlock()
	w.fg.mu.Lock()
	w.fg.hits = map[windows.PixelPoint]windows.HWND{at: 99}
	w.fg.mu.Unlock()

	sess, err := w.adapter.Open(context.Background(), w.config())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer sess.Close()
	obs, err := sess.Observe()
	if err != nil {
		t.Fatalf("observe: %v", err)
	}
	if obs.Ready {
		t.Fatal("an occluded probe must not be ready")
	}
	f := problemCode(t, obs.Problems)
	if f.Code != CodeWindowOccluded || f.Class != ClassPremise {
		t.Fatalf("want windowOccluded premise, got %+v", f)
	}
	if f.Params["probe"] != "detail" || f.Params["hitClass"] != "Shell_TrayWnd" {
		t.Fatalf("occlusion params: %v", f.Params)
	}
	if _, err := sess.LockBaseline(); err == nil {
		t.Fatal("the baseline must not lock while a sample is covered")
	}

	w.fg.mu.Lock()
	w.fg.hits = nil
	w.fg.mu.Unlock()
	if _, err := sess.LockBaseline(); err != nil {
		t.Fatalf("after the cover cleared: %v", err)
	}
}

func TestWinFrameContentPrecheck(t *testing.T) {
	w := newWinWorld(t)
	w.gdi.mu.Lock()
	w.gdi.fill = func(int, int) (byte, byte, byte, byte) { return 0, 0, 0, 255 }
	w.gdi.mu.Unlock()
	sess, err := w.adapter.Open(context.Background(), w.config())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer sess.Close()
	obs, err := sess.Observe()
	if err != nil || !obs.Ready {
		t.Fatalf("stage 1 does not inspect the content: %v %+v", err, obs)
	}
	_, err = sess.LockBaseline()
	var f *Fault
	if !errors.As(err, &f) || f.Code != CodeFrameContentBlank || f.Class != ClassPremise {
		t.Fatalf("want frameContentBlank premise, got %v", err)
	}
	if f.Params["edge"] == "" {
		t.Fatalf("blank edge params: %v", f.Params)
	}

	w.gdi.mu.Lock()
	w.gdi.fill = nil
	w.gdi.mu.Unlock()
	if _, err := sess.LockBaseline(); err != nil {
		t.Fatalf("after content appeared: %v", err)
	}
}

func TestWinExclusiveFullscreenPrecheck(t *testing.T) {
	t.Run("exclusive foreground target is refused", func(t *testing.T) {
		w := newWinWorld(t)
		w.fullscreen.state = windows.FullscreenExclusive
		sess, err := w.adapter.Open(context.Background(), w.config())
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		defer sess.Close()
		obs, err := sess.Observe()
		if err != nil {
			t.Fatalf("observe: %v", err)
		}
		f := problemCode(t, obs.Problems)
		if f.Code != CodeExclusiveFullscreen || f.Class != ClassSetup {
			t.Fatalf("want exclusiveFullscreen setup, got %+v", f)
		}
	})
	t.Run("not checked while another window is foreground", func(t *testing.T) {
		w := newWinWorld(t)
		w.fullscreen.state = windows.FullscreenExclusive
		w.fg.set(99)
		sess, err := w.adapter.Open(context.Background(), w.config())
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		defer sess.Close()
		obs, err := sess.Observe()
		if err != nil {
			t.Fatalf("observe: %v", err)
		}
		if len(obs.Problems) != 1 || obs.Problems[0].Code != CodeFocusLost {
			t.Fatalf("only focusLost is known before the target is foreground: %+v", obs.Problems)
		}
	})
	t.Run("unknown shell state is not guessed", func(t *testing.T) {
		w := newWinWorld(t)
		w.fullscreen.state = windows.FullscreenUnknown
		sess := w.openReady(t)
		sess.Close()
	})
}

func TestWinDisplayChecksPrecedeFocus(t *testing.T) {
	w := newWinWorld(t)
	w.geo.mu.Lock()
	w.geo.clientSize = windows.PixelPoint{X: 800, Y: 600}
	w.geo.monitor = windows.PixelRect{Max: windows.PixelPoint{X: 800, Y: 600}}
	w.geo.workArea = windows.PixelRect{Max: windows.PixelPoint{X: 800, Y: 600}}
	w.geo.mu.Unlock()
	w.fg.set(99) // another window is foreground; the tier is still refused
	_, err := w.adapter.Open(context.Background(), w.config())
	var f *Fault
	if !errors.As(err, &f) || f.Code != CodeClientTierUnsupported {
		t.Fatalf("want the stage-1 refusal before focus, got %v", err)
	}
}
