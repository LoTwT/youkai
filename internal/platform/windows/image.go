package windows

import (
	"fmt"
	"time"
)

// CapturedImage is the platform's own image type: 8-bit RGBA, rows
// top-down, alpha always 0xFF. GDI delivers 32bpp BGRA with an undefined
// alpha channel; the capture path converts to RGBA and marks the image
// opaque by construction, so no consumer can ever observe a transparent
// screenshot through this type.
type CapturedImage struct {
	Width  int
	Height int
	// Stride is the bytes per row; for images produced here it is
	// 4*Width.
	Stride int
	Pixels []byte // len(Pixels) == Stride*Height
}

// At returns the pixel at (x,y). The caller guarantees the coordinates
// are inside the image; Bounds-checking happens where the coordinates
// are computed.
func (im *CapturedImage) At(x, y int) (r, g, b, a uint8) {
	i := y*im.Stride + 4*x
	return im.Pixels[i], im.Pixels[i+1], im.Pixels[i+2], im.Pixels[i+3]
}

// ConvertBGRA converts one top-down 32bpp BGRA buffer — the layout
// GetDIBits produces with a negative header height — into an RGBA
// CapturedImage. The B and R channels swap; the undefined GDI alpha is
// replaced with 0xFF.
func ConvertBGRA(src []byte, width, height int) (*CapturedImage, error) {
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("invalid image size %d×%d", width, height)
	}
	if len(src) != width*height*4 {
		return nil, fmt.Errorf("buffer of %d bytes does not hold a %d×%d BGRA image", len(src), width, height)
	}
	im := &CapturedImage{
		Width:  width,
		Height: height,
		Stride: 4 * width,
		Pixels: make([]byte, width*height*4),
	}
	for i := 0; i < width*height; i++ {
		s := 4 * i
		d := 4 * i
		b := src[s]
		g := src[s+1]
		r := src[s+2]
		im.Pixels[d] = r
		im.Pixels[d+1] = g
		im.Pixels[d+2] = b
		im.Pixels[d+3] = 0xFF
	}
	return im, nil
}

// ImageObservation is the "image observation" handover category of T2
// §4.2: the captured image bound to the geometry version it was taken
// under, plus the timing of the machine-side capture call. RequestedAt
// and CompletedAt document only when this machine called into GDI and
// when it returned — a successful capture, a non-black frame or a fast
// return is NOT evidence that the game produced a new frame; GDI offers
// no presentation counter on this path and none is invented.
type ImageObservation struct {
	Image           *CapturedImage
	GeometryVersion GeometryVersion
	// FrameRef is the caller's opaque reference to this capture, carried
	// through to input requests so results correlate with the frame the
	// coordinates were derived from.
	FrameRef string
	// RequestedAt is when the capture call started; CompletedAt when it
	// returned.
	RequestedAt time.Time
	CompletedAt time.Time
}
