package windows

import (
	"strings"
	"testing"
)

func TestConvertBGRA(t *testing.T) {
	// Two pixels of top-down BGRA: the undefined GDI alpha is replaced
	// with 0xFF and B/R swap.
	src := []byte{
		0x11, 0x22, 0x33, 0x00, // B=0x11 G=0x22 R=0x33 A=garbage
		0xAA, 0xBB, 0xCC, 0x00,
	}
	img, err := ConvertBGRA(src, 2, 1)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if img.Width != 2 || img.Height != 1 || img.Stride != 8 || len(img.Pixels) != 8 {
		t.Fatalf("image shape: %d×%d stride %d len %d", img.Width, img.Height, img.Stride, len(img.Pixels))
	}
	if r, g, b, a := img.At(0, 0); r != 0x33 || g != 0x22 || b != 0x11 || a != 0xFF {
		t.Fatalf("pixel 0: rgba %#02x %#02x %#02x %#02x", r, g, b, a)
	}
	if r, g, b, a := img.At(1, 0); r != 0xCC || g != 0xBB || b != 0xAA || a != 0xFF {
		t.Fatalf("pixel 1: rgba %#02x %#02x %#02x %#02x", r, g, b, a)
	}

	if _, err := ConvertBGRA(src, 0, 1); err == nil {
		t.Fatal("zero width must be rejected")
	}
	if _, err := ConvertBGRA(src[:3], 1, 1); err == nil {
		t.Fatal("short buffer must be rejected")
	}
}

func TestCaptureRectHappyPath(t *testing.T) {
	gdi := newFakeGDI()
	rect := PixelRect{PixelPoint{100, 50}, PixelPoint{300, 250}} // 200×200 at a positive origin

	img, meta, err := CaptureRect(gdi, rect)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if img.Width != 200 || img.Height != 200 {
		t.Fatalf("image size: %d×%d", img.Width, img.Height)
	}
	// The BitBlt source is the rect origin, recorded by the fake.
	if len(gdi.bltSrc) != 1 || gdi.bltSrc[0] != (PixelPoint{100, 50}) {
		t.Fatalf("bitblt source origin: %+v", gdi.bltSrc)
	}
	// The fake fill converts through BGRA: pixel (10, 20) is
	// B=10, G=20, R=30, alpha garbage → RGBA 30,20,10,255.
	if r, g, b, a := img.At(10, 20); r != 30 || g != 20 || b != 10 || a != 255 {
		t.Fatalf("converted pixel: rgba %d %d %d %d", r, g, b, a)
	}
	// Everything acquired is released.
	if gdi.remaining != 0 {
		t.Fatalf("handles leaked: %d outstanding", gdi.remaining)
	}
	if meta.RequestedAt.IsZero() || meta.CompletedAt.Before(meta.RequestedAt) {
		t.Fatalf("capture timing not recorded: %+v", meta)
	}
}

func TestCaptureRectNegativeOrigin(t *testing.T) {
	// A secondary display left of the primary gives negative screen
	// coordinates; BitBlt takes them as-is (the fake records the signed
	// origin).
	gdi := newFakeGDI()
	rect := PixelRect{PixelPoint{-1920, -1080}, PixelPoint{-1720, -880}}
	if _, _, err := CaptureRect(gdi, rect); err != nil {
		t.Fatalf("capture with negative origin: %v", err)
	}
	if len(gdi.bltSrc) != 1 || gdi.bltSrc[0] != (PixelPoint{-1920, -1080}) {
		t.Fatalf("negative bitblt origin: %+v", gdi.bltSrc)
	}
	if gdi.remaining != 0 {
		t.Fatalf("handles leaked: %d outstanding", gdi.remaining)
	}
}

func TestCaptureRectCleanupOnFailures(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*fakeGDI)
		want  string
	}{
		{"screen DC fails", func(g *fakeGDI) { g.screenDCFail = true }, "get screen DC"},
		{"compatible DC fails", func(g *fakeGDI) { g.createDCFail = true }, "create compatible DC"},
		{"bitmap creation fails", func(g *fakeGDI) { g.createBmpFail = true }, "create compatible bitmap"},
		{"select fails", func(g *fakeGDI) { g.selectFail = true }, "select bitmap"},
		{"bitblt fails", func(g *fakeGDI) { g.bltFail = true }, "bitblt"},
		{"getdibits fails", func(g *fakeGDI) { g.dibitsFail = true }, "getdibits"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gdi := newFakeGDI()
			tc.setup(gdi)
			img, _, err := CaptureRect(gdi, PixelRect{PixelPoint{0, 0}, PixelPoint{10, 10}})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got %v", tc.want, err)
			}
			if img != nil {
				t.Fatal("no image on failure")
			}
			// Everything acquired before the failure is released anyway.
			if gdi.remaining != 0 {
				t.Fatalf("handles leaked: %d outstanding", gdi.remaining)
			}
		})
	}
}

func TestCaptureRectReleaseFailureReportedWithImage(t *testing.T) {
	// The capture succeeds but ReleaseDC fails: the image is valid, the
	// error says a handle leaked, and the fake accounting keeps the DC
	// outstanding — exactly the honest outcome.
	gdi := newFakeGDI()
	gdi.releaseFail = true
	img, _, err := CaptureRect(gdi, PixelRect{PixelPoint{0, 0}, PixelPoint{10, 10}})
	if img == nil {
		t.Fatal("the pixels are valid and must be returned")
	}
	if err == nil || !strings.Contains(err.Error(), "ReleaseDC") {
		t.Fatalf("the cleanup failure must be reported: %v", err)
	}
	if gdi.remaining != 1 {
		t.Fatalf("the failed release leaves one handle outstanding, got %d", gdi.remaining)
	}
}

func TestCaptureRectRefusesBadRectsBeforeAnyCall(t *testing.T) {
	gdi := newFakeGDI()
	if _, _, err := CaptureRect(gdi, PixelRect{PixelPoint{0, 0}, PixelPoint{0, 100}}); err == nil {
		t.Fatal("empty rect must be refused")
	}
	if _, _, err := CaptureRect(gdi, PixelRect{PixelPoint{0, 0}, PixelPoint{MaxCaptureDimension + 1, 100}}); err == nil {
		t.Fatal("oversized rect must be refused")
	}
	if _, _, err := CaptureRect(gdi, PixelRect{PixelPoint{0, 0}, PixelPoint{6000, 3000}}); err == nil {
		t.Fatal("rect above the byte budget must be refused")
	}
	if len(gdi.bltSrc) != 0 {
		t.Fatal("no GDI work happens for a refused rect")
	}
	if gdi.remaining != 0 {
		t.Fatal("nothing was acquired, nothing leaks")
	}
}

func TestCaptureFrameBindsGeometryVersion(t *testing.T) {
	geo := ClientGeometry{
		Version: 3,
		Client:  PixelRect{PixelPoint{10, 20}, PixelPoint{1930, 1100}},
	}
	obs, fault := CaptureFrame(newFakeGDI(), geo, "frame-7")
	if fault != nil {
		t.Fatalf("capture frame: %v", fault)
	}
	if obs.GeometryVersion != 3 || obs.FrameRef != "frame-7" {
		t.Fatalf("observation must carry the geometry version and frame ref: %+v", obs)
	}
	if obs.Image == nil || obs.Image.Width != 1920 {
		t.Fatalf("image not captured: %+v", obs.Image)
	}
	if obs.RequestedAt.IsZero() || obs.CompletedAt.Before(obs.RequestedAt) {
		t.Fatalf("timing not recorded: %+v", obs)
	}
}

func TestCaptureFrameRefusesUnverifiedGeometry(t *testing.T) {
	obs, fault := CaptureFrame(newFakeGDI(), ClientGeometry{}, "f")
	if fault == nil || fault.Code != CodeCaptureFailed {
		t.Fatalf("unverified geometry must be a capture fault, got %v", fault)
	}
	if obs.Image != nil {
		t.Fatal("no image on fault")
	}
}

func TestCaptureFrameReportsCaptureFailureWithLastGeometry(t *testing.T) {
	gdi := newFakeGDI()
	gdi.bltFail = true
	geo := ClientGeometry{Version: 5, Client: PixelRect{PixelPoint{0, 0}, PixelPoint{100, 100}}}
	obs, fault := CaptureFrame(gdi, geo, "f")
	if fault == nil || fault.Code != CodeCaptureFailed || fault.LastGeometryVersion != 5 {
		t.Fatalf("capture failure must carry the last geometry version, got %v", fault)
	}
	if obs.Image != nil {
		t.Fatal("no image on fault")
	}
	if fault.Phase != PhaseCapture {
		t.Fatalf("capture failures report the capture phase, got %s", fault.Phase)
	}
	if gdi.remaining != 0 {
		t.Fatalf("handles leaked: %d outstanding", gdi.remaining)
	}
}
