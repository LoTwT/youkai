package windows

import "testing"

func frameImage(w, h int, black func(x, y int) bool) *CapturedImage {
	im := &CapturedImage{Width: w, Height: h, Stride: 4 * w, Pixels: make([]byte, 4*w*h)}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*im.Stride + 4*x
			if black(x, y) {
				continue
			}
			im.Pixels[i], im.Pixels[i+1], im.Pixels[i+2], im.Pixels[i+3] = 200, 100, 50, 255
		}
	}
	return im
}

func TestCheckFrameContent(t *testing.T) {
	t.Run("content reaches every edge", func(t *testing.T) {
		if f := CheckFrameContent(frameImage(64, 48, func(int, int) bool { return false }), PixelRect{}, EdgeMargins{}); f != nil {
			t.Fatalf("want pass, got %v", f)
		}
	})
	t.Run("entire frame black", func(t *testing.T) {
		f := CheckFrameContent(frameImage(64, 48, func(int, int) bool { return true }), PixelRect{}, EdgeMargins{})
		if f == nil || f.Code != CodeFrameContentBlank {
			t.Fatalf("want frameContentBlank, got %v", f)
		}
		if f.Params["reason"] != "edge" || f.Params["edge"] != "top" {
			t.Fatalf("the first blank edge must be named: %v", f.Params)
		}
	})
	t.Run("right edge strip black", func(t *testing.T) {
		f := CheckFrameContent(frameImage(64, 48, func(x, _ int) bool { return x >= 60 }), PixelRect{}, EdgeMargins{})
		if f == nil || f.Code != CodeFrameContentBlank || f.Params["edge"] != "right" {
			t.Fatalf("want right edge, got %v", f)
		}
	})
	t.Run("bottom edge strip black", func(t *testing.T) {
		f := CheckFrameContent(frameImage(64, 48, func(_, y int) bool { return y >= 44 }), PixelRect{}, EdgeMargins{})
		if f == nil || f.Code != CodeFrameContentBlank || f.Params["edge"] != "bottom" {
			t.Fatalf("want bottom edge, got %v", f)
		}
	})
	t.Run("optional content region must not be black", func(t *testing.T) {
		im := frameImage(64, 48, func(x, y int) bool { return y >= 40 && x >= 10 && x < 50 })
		f := CheckFrameContent(im, PixelRect{PixelPoint{10, 40}, PixelPoint{50, 48}}, EdgeMargins{})
		if f == nil || f.Code != CodeFrameContentBlank || f.Params["reason"] != "content" {
			t.Fatalf("want content region verdict, got %v", f)
		}
		// The same frame passes when the optional probe is not configured.
		if f := CheckFrameContent(im, PixelRect{}, EdgeMargins{}); f != nil {
			t.Fatalf("without a content probe the frame must pass, got %v", f)
		}
	})
}

func TestCheckFrameContentCalibratedTopMargin(t *testing.T) {
	// The real 1920×1080 local frame has 22 all-black rows at the top
	// before game content starts. The calibrated margin admits that and
	// still rejects one extra off-screen row.
	im := frameImage(64, 48, func(_, y int) bool { return y < 10 })
	if f := CheckFrameContent(im, PixelRect{}, EdgeMargins{Top: 10}); f != nil {
		t.Fatalf("the calibrated top margin must pass, got %v", f)
	}
	if f := CheckFrameContent(im, PixelRect{}, EdgeMargins{Top: 0}); f == nil || f.Params["edge"] != "top" {
		t.Fatalf("without the margin the black top must be refused, got %v", f)
	}
	// Content starting at row 11 (one extra black row) is outside a
	// 10-row margin.
	shifted := frameImage(64, 48, func(_, y int) bool { return y < 11 })
	if f := CheckFrameContent(shifted, PixelRect{}, EdgeMargins{Top: 10}); f == nil || f.Params["edge"] != "top" {
		t.Fatalf("one extra black row must be refused, got %v", f)
	}
}
