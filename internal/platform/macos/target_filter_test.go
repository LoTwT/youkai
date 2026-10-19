package macos_test

import (
	"fmt"
	"testing"

	macos "github.com/LoTwT/youkai/internal/platform/macos"
)

// Target candidate filtering tests (PR 4a, M1). The window set mirrors
// the cloud client as 实测轮 ① recorded it: one main window of 1164×762
// points, four 33 pt full-width strips and one 500×500 untitled overlay,
// all of the same bundle and all in layer 0. The scenarios vary only
// which windows are on screen and where they sit; the data tests the
// predicate, not the real machine.

const cloudBundle = "com.miHoYo.cloudgames.Nap"

func fieldDisplays() []macos.NativeDisplay {
	return []macos.NativeDisplay{
		{DisplayID: 1, Width: 1920, Height: 1080, Frame: macos.PointRect{X: 0, Y: 0, W: 1920, H: 1080}},
		// A secondary display left of the main one, negative origin.
		{DisplayID: 2, Width: 1440, Height: 900, Frame: macos.PointRect{X: -1440, Y: 0, W: 1440, H: 900}},
	}
}

// cloudWindows builds the field-round window set. onScreen controls the
// main window; overlayOnScreen and overlayFrame place the 500×500
// overlay; parked marks the four strips as parked (off screen).
func cloudWindows(mainOnScreen, overlayOnScreen bool, overlayFrame macos.PointRect) []macos.NativeWindow {
	ws := []macos.NativeWindow{
		{WindowID: 100, PID: 5001, BundleID: cloudBundle, AppName: "云·绝区零", Title: "云·绝区零",
			Frame:    macos.PointRect{X: 200, Y: 150, W: 1164, H: 762},
			OnScreen: mainOnScreen,
			Layer:    0},
		{WindowID: 105, PID: 5001, BundleID: cloudBundle, Title: "",
			Frame:    overlayFrame,
			OnScreen: overlayOnScreen,
			Layer:    0},
	}
	// Four 33 pt full-width strips of the same process, parked off screen.
	for i := 0; i < 4; i++ {
		ws = append(ws, macos.NativeWindow{
			WindowID: uint32(110 + i), PID: 5001, BundleID: cloudBundle,
			Frame: macos.PointRect{X: 200, Y: float64(150 + 762 + 10 + i*40), W: 1164, H: 33},
			Layer: 0,
		})
	}
	return ws
}

func TestFilterTargetCandidates(t *testing.T) {
	displays := fieldDisplays()

	t.Run("unique main window", func(t *testing.T) {
		ws := cloudWindows(true, false, macos.PointRect{X: 300, Y: 300, W: 500, H: 500})
		got := macos.FilterTargetCandidates(cloudBundle, ws, displays)
		if len(got) != 1 || got[0].WindowID != 100 {
			t.Fatalf("want exactly the main window, got %+v", got)
		}
	})

	t.Run("overlay passing the predicate makes it ambiguous", func(t *testing.T) {
		ws := cloudWindows(true, true, macos.PointRect{X: 300, Y: 300, W: 500, H: 500})
		got := macos.FilterTargetCandidates(cloudBundle, ws, displays)
		if len(got) != 2 {
			t.Fatalf("the on-screen overlay must survive the filter: %+v", got)
		}
	})

	t.Run("tiny status chip is not a candidate", func(t *testing.T) {
		// Field round 2 saw a transient 66x20 same-process chip at the
		// game window's top-left while the client was frontmost. It
		// cannot be the game surface and must not resolve as a target.
		ws := cloudWindows(true, true, macos.PointRect{X: 206, Y: 156, W: 66, H: 20})
		got := macos.FilterTargetCandidates(cloudBundle, ws, displays)
		if len(got) != 1 || got[0].WindowID != 100 {
			t.Fatalf("the status chip must not enter the candidate set, got %+v", got)
		}
	})

	t.Run("no candidate when the main window is off screen", func(t *testing.T) {
		ws := cloudWindows(false, false, macos.PointRect{X: 300, Y: 300, W: 500, H: 500})
		if got := macos.FilterTargetCandidates(cloudBundle, ws, displays); len(got) != 0 {
			t.Fatalf("want zero candidates, got %+v", got)
		}
	})

	t.Run("partially off-display main window fails the predicate", func(t *testing.T) {
		ws := cloudWindows(true, false, macos.PointRect{X: 300, Y: 300, W: 500, H: 500})
		// Dragged beyond the right edge of the main display: 1200+1164
		// exceeds 1920, and it does not fit the secondary display either.
		ws[0].Frame = macos.PointRect{X: 1200, Y: 150, W: 1164, H: 762}
		if got := macos.FilterTargetCandidates(cloudBundle, ws, displays); len(got) != 0 {
			t.Fatalf("a partially off-display window must not resolve, got %+v", got)
		}
	})

	t.Run("other bundles and layers never qualify", func(t *testing.T) {
		ws := cloudWindows(true, false, macos.PointRect{X: 300, Y: 300, W: 500, H: 500})
		ws = append(ws,
			macos.NativeWindow{WindowID: 200, PID: 6001, BundleID: "com.example.other", Frame: macos.PointRect{X: 100, Y: 100, W: 400, H: 300}, OnScreen: true, Layer: 0},
			macos.NativeWindow{WindowID: 201, PID: 5001, BundleID: cloudBundle, Frame: macos.PointRect{X: 100, Y: 100, W: 400, H: 300}, OnScreen: true, Layer: 25},
		)
		got := macos.FilterTargetCandidates(cloudBundle, ws, displays)
		if len(got) != 1 || got[0].WindowID != 100 {
			t.Fatalf("other bundles and non-zero layers must be excluded, got %+v", got)
		}
	})

	t.Run("flush edges and the secondary display qualify", func(t *testing.T) {
		ws := cloudWindows(true, false, macos.PointRect{X: 300, Y: 300, W: 500, H: 500})
		// Flush with the main display's edges on every side.
		ws[0].Frame = macos.PointRect{X: 0, Y: 0, W: 1920, H: 1080}
		if got := macos.FilterTargetCandidates(cloudBundle, ws, displays); len(got) != 1 {
			t.Fatalf("a flush window must resolve, got %+v", got)
		}
		// Fully inside the secondary display (negative coordinates).
		ws[0].Frame = macos.PointRect{X: -1440, Y: 0, W: 1440, H: 900}
		if got := macos.FilterTargetCandidates(cloudBundle, ws, displays); len(got) != 1 {
			t.Fatalf("a window on the secondary display must resolve, got %+v", got)
		}
		// Straddling the two displays fails: it is inside neither.
		ws[0].Frame = macos.PointRect{X: -100, Y: 100, W: 1164, H: 762}
		if got := macos.FilterTargetCandidates(cloudBundle, ws, displays); len(got) != 0 {
			t.Fatalf("a straddling window must not resolve, got %+v", got)
		}
	})
}

func TestFilterAmbiguitySiblings(t *testing.T) {
	displays := fieldDisplays()
	selector := macos.WindowSelector{BundleID: cloudBundle, PID: 5001, WindowID: 100}

	t.Run("only predicate-passing siblings count", func(t *testing.T) {
		// The strips are parked and the overlay is a different bundle's
		// window: neither counts, even though AssessTarget would call
		// every same-process window a sibling.
		ws := cloudWindows(true, false, macos.PointRect{X: 300, Y: 300, W: 500, H: 500})
		ws = append(ws, macos.NativeWindow{WindowID: 300, PID: 5001, BundleID: "com.example.other", Frame: macos.PointRect{X: 100, Y: 100, W: 400, H: 300}, OnScreen: true})
		if got := macos.FilterAmbiguitySiblings(selector, ws, displays); len(got) != 0 {
			t.Fatalf("parked strips and foreign-bundle windows must not count, got %+v", got)
		}
	})

	t.Run("an on-screen overlay counts as ambiguity", func(t *testing.T) {
		ws := cloudWindows(true, true, macos.PointRect{X: 300, Y: 300, W: 500, H: 500})
		got := macos.FilterAmbiguitySiblings(selector, ws, displays)
		if len(got) != 1 || got[0].WindowID != 105 {
			t.Fatalf("the qualifying overlay must be the only ambiguity source, got %+v", got)
		}
	})

	t.Run("tiny status chip is not ambiguity", func(t *testing.T) {
		ws := cloudWindows(true, true, macos.PointRect{X: 206, Y: 156, W: 66, H: 20})
		if got := macos.FilterAmbiguitySiblings(selector, ws, displays); len(got) != 0 {
			t.Fatalf("a status chip must not block input to the locked window, got %+v", got)
		}
	})

	t.Run("the selected window itself never counts", func(t *testing.T) {
		// Even when the target itself passes the predicate, it is not its
		// own ambiguity source; and when it fails the predicate (half off
		// the display) that is its own state, not ambiguity.
		ws := cloudWindows(true, true, macos.PointRect{X: 300, Y: 300, W: 500, H: 500})
		if got := macos.FilterAmbiguitySiblings(selector, ws, displays); len(got) != 1 {
			t.Fatalf("target exclusion broken, got %+v", got)
		}
		ws[0].Frame = macos.PointRect{X: 1200, Y: 150, W: 1164, H: 762}
		if got := macos.FilterAmbiguitySiblings(selector, ws, displays); len(got) != 1 {
			t.Fatalf("a target failing the predicate is still excluded, not ambiguity, got %+v", got)
		}
	})

	t.Run("sibling of a restarted process does not count", func(t *testing.T) {
		// A same-bundle window of another process instance is not a
		// sibling of the locked one: the process instance is part of the
		// identity.
		ws := cloudWindows(true, false, macos.PointRect{X: 300, Y: 300, W: 500, H: 500})
		ws = append(ws, macos.NativeWindow{WindowID: 400, PID: 5999, BundleID: cloudBundle, Frame: macos.PointRect{X: 100, Y: 100, W: 400, H: 300}, OnScreen: true})
		if got := macos.FilterAmbiguitySiblings(selector, ws, displays); len(got) != 0 {
			t.Fatalf("another process instance's window must not count, got %+v", got)
		}
	})
}

func TestWindowFullyInsideDisplays(t *testing.T) {
	displays := fieldDisplays()
	cases := []struct {
		name  string
		frame macos.PointRect
		want  bool
	}{
		{"inside main", macos.PointRect{X: 10, Y: 10, W: 100, H: 100}, true},
		{"flush with main edges", macos.PointRect{X: 0, Y: 0, W: 1920, H: 1080}, true},
		{"epsilon over the edge", macos.PointRect{X: 0, Y: 0, W: 1920.005, H: 1080.005}, true},
		{"clearly over the edge", macos.PointRect{X: 0, Y: 0, W: 1920.5, H: 1080}, false},
		{"negative origin outside all", macos.PointRect{X: -10, Y: 0, W: 100, H: 100}, false},
		{"inside secondary", macos.PointRect{X: -1400, Y: 10, W: 200, H: 200}, true},
		{"straddling the gap", macos.PointRect{X: -100, Y: 10, W: 200, H: 200}, false},
		{"zero size", macos.PointRect{X: 10, Y: 10, W: 0, H: 0}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := macos.WindowFullyInsideDisplays(tc.frame, displays); got != tc.want {
				t.Fatalf("WindowFullyInsideDisplays(%v) = %v, want %v", tc.frame, got, tc.want)
			}
		})
	}
	// No displays means nothing can be verified.
	if macos.WindowFullyInsideDisplays(macos.PointRect{X: 0, Y: 0, W: 10, H: 10}, nil) {
		t.Fatal("without displays nothing is fully inside")
	}
}

// TestCandidatePredicateMatchesFieldGeometry pins the predicate against
// the exact numbers 实测轮 ① recorded: a 1164×762 point window at a
// typical position on a 1512×982 display resolves, and the surface
// geometry (1920×1080 surface, scaleFactor 2, contentScale ≈0.7087) is
// consistent with that window size — the resolution and the frame
// geometry must agree about the same window.
func TestCandidatePredicateMatchesFieldGeometry(t *testing.T) {
	displays := []macos.NativeDisplay{
		{DisplayID: 1, Width: 1512, Height: 982, Frame: macos.PointRect{X: 0, Y: 0, W: 1512, H: 982}},
	}
	main := macos.NativeWindow{
		WindowID: 100, PID: 5001, BundleID: cloudBundle,
		Frame:    macos.PointRect{X: 174, Y: 110, W: 1164, H: 762},
		OnScreen: true, Layer: 0,
	}
	if !macos.IsTargetCandidate(cloudBundle, main, displays) {
		t.Fatal("the field-round main window must resolve")
	}
	// The frame geometry the stream reported for that window (contentRect
	// in points, contentScale back to the original content) must be
	// constructible: the two views of the window agree.
	geo := macos.FrameGeometry{
		SurfaceWidth: 1920, SurfaceHeight: 1080, BytesPerRow: 7680,
		ContentRect:  macos.PointRect{X: 0, Y: 0, W: 824.88, H: 540},
		ContentScale: 0.7086614, ScaleFactor: 2,
		WindowFrame: main.Frame,
	}
	if _, err := macos.NewGeometryTransform(geo); err != nil {
		t.Fatalf("the field geometry must produce a transform: %v", err)
	}
	// And the pixel rect it yields is the documented content area.
	tr, _ := macos.NewGeometryTransform(geo)
	valid := tr.ValidRect()
	if valid.X != 0 || valid.Y != 0 || valid.W != 1650 || valid.H != 1080 {
		t.Fatalf("valid rect %v, want 0,0,1650,1080", valid)
	}
	// Image pixels map into screen points through the baseline transform.
	p := tr.PixelToScreen(macos.PixelPoint{X: 824, Y: 539})
	want := macos.Point{X: 174 + 824.0/2/0.7086614, Y: 110 + 539.0/2/0.7086614}
	if fmt.Sprintf("%.3f", p.X) != fmt.Sprintf("%.3f", want.X) || fmt.Sprintf("%.3f", p.Y) != fmt.Sprintf("%.3f", want.Y) {
		t.Fatalf("pixel→screen %v, want %v", p, want)
	}
}
