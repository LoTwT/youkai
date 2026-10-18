package windows

import (
	"errors"
	"testing"
)

func singleScreenSpace(w, h int) CoordinateSpace {
	return CoordinateSpace{
		Primary: PixelRect{PixelPoint{0, 0}, PixelPoint{w, h}},
		Virtual: PixelRect{PixelPoint{0, 0}, PixelPoint{w, h}},
	}
}

func dualScreenSpace() CoordinateSpace {
	// Primary 1920×1080 at the origin; a second display of the same
	// size to the left and above, so the virtual desktop starts at
	// (-1920,-1080).
	return CoordinateSpace{
		Primary: PixelRect{PixelPoint{0, 0}, PixelPoint{1920, 1080}},
		Virtual: PixelRect{PixelPoint{-1920, -1080}, PixelPoint{1920, 1080}},
	}
}

func TestPointToMouseAbsolutePrimaryCorners(t *testing.T) {
	space := singleScreenSpace(1920, 1080)

	abs, virtualDesk, err := PointToMouseAbsolute(PixelPoint{0, 0}, space)
	if err != nil || virtualDesk || abs.DX != 0 || abs.DY != 0 {
		t.Fatalf("first pixel should map to 0,0 without VIRTUALDESK: %+v %v %v", abs, virtualDesk, err)
	}
	abs, virtualDesk, err = PointToMouseAbsolute(PixelPoint{1919, 1079}, space)
	if err != nil || virtualDesk || abs.DX != MaxAbsoluteAxis || abs.DY != MaxAbsoluteAxis {
		t.Fatalf("last pixel should map to 65535,65535: %+v %v %v", abs, virtualDesk, err)
	}
	// A point on the exclusive far edge of the primary is outside the
	// virtual desktop too (single-screen space).
	_, _, err = PointToMouseAbsolute(PixelPoint{1920, 0}, space)
	if !errors.Is(err, ErrPointOutOfRange) {
		t.Fatalf("point past the primary edge must be out of range, got %v", err)
	}
}

func TestPointToMouseAbsoluteKnownSmallValues(t *testing.T) {
	space := singleScreenSpace(2, 2)
	abs, _, err := PointToMouseAbsolute(PixelPoint{0, 0}, space)
	if err != nil || abs.DX != 0 || abs.DY != 0 {
		t.Fatalf("2×2 origin: %+v %v", abs, err)
	}
	abs, _, err = PointToMouseAbsolute(PixelPoint{1, 1}, space)
	if err != nil || abs.DX != MaxAbsoluteAxis || abs.DY != MaxAbsoluteAxis {
		t.Fatalf("2×2 last pixel: %+v %v", abs, err)
	}

	// 3-wide axis: the middle pixel rounds up to half the domain.
	space = singleScreenSpace(3, 2)
	abs, _, err = PointToMouseAbsolute(PixelPoint{1, 0}, space)
	if err != nil || abs.DX != 32768 {
		t.Fatalf("3-wide middle: %+v %v", abs, err)
	}
}

func TestPointToMouseAbsoluteRoundTripExact(t *testing.T) {
	// Under the documented system model (decode floor(v*(span-1)/65535))
	// every pixel must map back onto itself; ceiling rounding on the
	// way out is what guarantees this.
	for _, size := range [][2]int{
		{1, 1},
		{3, 2},
		{1920, 1080},
		{2560, 1440},
		{1366, 768},
	} {
		w, h := size[0], size[1]
		space := singleScreenSpace(w, h)
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				p := PixelPoint{x, y}
				abs, virtualDesk, err := PointToMouseAbsolute(p, space)
				if err != nil {
					t.Fatalf("%d×%d %v: %v", w, h, p, err)
				}
				if virtualDesk {
					t.Fatalf("%d×%d %v: single screen must not use VIRTUALDESK", w, h, p)
				}
				if abs.DX < 0 || abs.DX > MaxAbsoluteAxis || abs.DY < 0 || abs.DY > MaxAbsoluteAxis {
					t.Fatalf("%d×%d %v: out of domain: %+v", w, h, p, abs)
				}
				back := MouseAbsoluteToPoint(abs, virtualDesk, space)
				if back != p {
					t.Fatalf("%d×%d %v: round trip landed on %v (abs %+v)", w, h, p, back, abs)
				}
			}
		}
	}
}

func TestPointToMouseAbsoluteVirtualDesk(t *testing.T) {
	space := dualScreenSpace()

	// Points on the primary use the default mapping.
	abs, virtualDesk, err := PointToMouseAbsolute(PixelPoint{0, 0}, space)
	if err != nil || virtualDesk || abs.DX != 0 || abs.DY != 0 {
		t.Fatalf("primary origin must map without VIRTUALDESK: %+v %v %v", abs, virtualDesk, err)
	}

	// Negative coordinates on the secondary display require the
	// virtual-desktop mapping and must survive the round trip.
	virtual := space.Virtual
	for _, p := range []PixelPoint{
		{-1920, -1080}, // virtual origin
		{-1, -1},       // pixel adjacent to the primary origin
		{-960, -540},   // secondary center
		{-1920, 0},     // left edge, primary top
	} {
		abs, virtualDesk, err := PointToMouseAbsolute(p, space)
		if err != nil {
			t.Fatalf("%v: %v", p, err)
		}
		if !virtualDesk {
			t.Fatalf("%v: outside the primary must require VIRTUALDESK", p)
		}
		back := MouseAbsoluteToPoint(abs, virtualDesk, space)
		if back != p {
			t.Fatalf("%v: round trip landed on %v (abs %+v)", p, back, abs)
		}
	}

	// The far corner of the virtual desktop maps to the domain end.
	abs, _, err = PointToMouseAbsolute(PixelPoint{virtual.Max.X - 1, virtual.Max.Y - 1}, space)
	if err != nil || abs.DX != MaxAbsoluteAxis || abs.DY != MaxAbsoluteAxis {
		t.Fatalf("last virtual pixel should map to 65535,65535: %+v %v", abs, err)
	}

	// Outside the virtual desktop is refused, including the exclusive
	// far edge.
	for _, p := range []PixelPoint{{-1921, 0}, {0, -1081}, {virtual.Max.X, 0}, {100, virtual.Max.Y}} {
		if _, _, err := PointToMouseAbsolute(p, space); !errors.Is(err, ErrPointOutOfRange) {
			t.Fatalf("%v must be out of range, got %v", p, err)
		}
	}
}

func TestPointToMouseAbsoluteMalformedSpace(t *testing.T) {
	bad := []CoordinateSpace{
		{Primary: PixelRect{PixelPoint{0, 0}, PixelPoint{0, 100}}, Virtual: PixelRect{PixelPoint{0, 0}, PixelPoint{100, 100}}},     // empty primary
		{Primary: PixelRect{PixelPoint{0, 0}, PixelPoint{100, 100}}, Virtual: PixelRect{PixelPoint{0, 0}, PixelPoint{0, 100}}},     // empty virtual
		{Primary: PixelRect{PixelPoint{0, 0}, PixelPoint{200, 100}}, Virtual: PixelRect{PixelPoint{50, 0}, PixelPoint{100, 100}}},  // primary outside virtual
		{Primary: PixelRect{PixelPoint{0, 0}, PixelPoint{100, 100}}, Virtual: PixelRect{PixelPoint{10, 10}, PixelPoint{110, 110}}}, // virtual origin positive
	}
	for i, space := range bad {
		if _, _, err := PointToMouseAbsolute(PixelPoint{50, 50}, space); !errors.Is(err, ErrMalformedCoordinateSpace) {
			t.Fatalf("space %d should be malformed, got %v", i, err)
		}
	}
}

func TestPointToMouseAbsoluteDegenerateAxis(t *testing.T) {
	// A 1-pixel axis has only one pixel; both "corners" are it.
	space := singleScreenSpace(1, 100)
	abs, _, err := PointToMouseAbsolute(PixelPoint{0, 99}, space)
	if err != nil || abs.DX != 0 {
		t.Fatalf("1px axis should map to 0: %+v %v", abs, err)
	}
}
