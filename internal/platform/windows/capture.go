package windows

import (
	"errors"
	"fmt"
	"time"
)

// DC and GDIObject are GDI handle values. They are borrowed inside one
// CaptureRect call and never outlive it.
type DC uintptr
type GDIObject uintptr

// GDI is the gdi32 surface the capture choreography runs on. The real
// implementation is the Win32 path; tests inject fakes that account for
// every acquired handle.
type GDI interface {
	// GetScreenDC returns a DC for the whole screen (GetDC of the
	// desktop).
	GetScreenDC() (DC, error)
	ReleaseDC(dc DC) error
	CreateCompatibleDC(dc DC) (DC, error)
	DeleteDC(dc DC) error
	CreateCompatibleBitmap(dc DC, w, h int) (GDIObject, error)
	// SelectObject selects obj into dc and returns the object it
	// replaced, which must be re-selected before the new object is
	// deleted.
	SelectObject(dc DC, obj GDIObject) (GDIObject, error)
	DeleteObject(obj GDIObject) error
	// BitBlt copies a physical-pixel rectangle from the screen DC into
	// the destination DC.
	BitBlt(dst DC, dstX, dstY, w, h int, src DC, srcX, srcY int) error
	// GetDIBits reads the bitmap into buf as one top-down 32bpp BGRA
	// image of w×h pixels (len(buf) must be w*h*4).
	GetDIBits(dc DC, bmp GDIObject, buf []byte, w, h int) error
}

// Defensive bounds of one capture: the target configurations are
// 2560×1440 and 1920×1080 client areas; anything beyond these bounds is
// a measurement bug or a pathological desktop, refused before
// allocating.
const (
	MaxCaptureDimension = 16384
	MaxCaptureBytes     = 64 << 20
)

// CaptureMeta records the timing of one capture call: when the machine
// called into GDI and when it returned. It says nothing about the game:
// a successful capture, a non-black frame or a fast return is not
// evidence of a new game frame, and GDI provides no presentation
// counter on this path.
type CaptureMeta struct {
	RequestedAt time.Time
	CompletedAt time.Time
}

// CaptureRect copies rect — a screen physical-pixel rectangle of the
// desktop — into an RGBA image. The path is: screen DC, private
// compatible DC and bitmap, BitBlt of the rect, GetDIBits into a BGRA
// buffer, convert to RGBA. Cleanup runs in reverse acquisition order
// (the bitmap is re-selected out before it is deleted) and always runs,
// including on failure; a cleanup failure is reported alongside the
// result. When the returned image is non-nil with a non-nil error, the
// pixels are valid but a cleanup call failed and a handle may have
// leaked — the error says which.
func CaptureRect(gdi GDI, rect PixelRect) (img *CapturedImage, meta CaptureMeta, err error) {
	w, h := rect.Dx(), rect.Dy()
	if w < 1 || h < 1 {
		return nil, meta, fmt.Errorf("capture rect %v is empty", rect)
	}
	if w > MaxCaptureDimension || h > MaxCaptureDimension || int64(w)*int64(h)*4 > MaxCaptureBytes {
		return nil, meta, fmt.Errorf("capture rect %d×%d exceeds the bound %d×%d / %d bytes", w, h, MaxCaptureDimension, MaxCaptureDimension, MaxCaptureBytes)
	}
	meta.RequestedAt = time.Now()
	defer func() { meta.CompletedAt = time.Now() }()

	// Cleanup runs in reverse acquisition order: re-select the replaced
	// object, delete the bitmap, delete the memory DC, release the
	// screen DC. Cleanup errors are joined into the returned error even
	// when the capture itself succeeded.
	var cleanup []func() error
	defer func() {
		errs := []error{err}
		for i := len(cleanup) - 1; i >= 0; i-- {
			errs = append(errs, cleanup[i]())
		}
		err = errors.Join(errs...)
	}()

	screenDC, err := gdi.GetScreenDC()
	if err != nil {
		return nil, meta, fmt.Errorf("get screen DC: %w", err)
	}
	cleanup = append(cleanup, func() error { return gdi.ReleaseDC(screenDC) })

	memDC, err := gdi.CreateCompatibleDC(screenDC)
	if err != nil {
		return nil, meta, fmt.Errorf("create compatible DC: %w", err)
	}
	cleanup = append(cleanup, func() error { return gdi.DeleteDC(memDC) })

	bmp, err := gdi.CreateCompatibleBitmap(screenDC, w, h)
	if err != nil {
		return nil, meta, fmt.Errorf("create compatible bitmap: %w", err)
	}
	cleanup = append(cleanup, func() error { return gdi.DeleteObject(bmp) })

	old, err := gdi.SelectObject(memDC, bmp)
	if err != nil {
		return nil, meta, fmt.Errorf("select bitmap: %w", err)
	}
	// Runs first in the reverse order, before DeleteObject(bmp): a
	// bitmap still selected into a DC cannot be deleted.
	cleanup = append(cleanup, func() error {
		_, err := gdi.SelectObject(memDC, old)
		return err
	})

	if err := gdi.BitBlt(memDC, 0, 0, w, h, screenDC, rect.Min.X, rect.Min.Y); err != nil {
		return nil, meta, fmt.Errorf("bitblt %d×%d from %v: %w", w, h, rect.Min, err)
	}
	buf := make([]byte, w*h*4)
	if err := gdi.GetDIBits(memDC, bmp, buf, w, h); err != nil {
		return nil, meta, fmt.Errorf("getdibits: %w", err)
	}
	img, err = ConvertBGRA(buf, w, h)
	if err != nil {
		return nil, meta, err
	}
	return img, meta, nil
}

// CaptureFrame captures the current client region of a verified
// geometry and binds the result to its geometry version — the "image
// observation" handover of T2 §4.2. The geometry must be non-empty and
// carry a version; the caller derives input coordinates only from
// observations of the version they were taken under.
func CaptureFrame(gdi GDI, geo ClientGeometry, frameRef string) (ImageObservation, *PlatformFault) {
	if geo.Version == 0 || geo.Client.Empty() {
		return ImageObservation{}, NewFault(CodeCaptureFailed, "no verified client geometry to capture", nil, PhaseCapture, time.Now(), geo.Version)
	}
	img, meta, err := CaptureRect(gdi, geo.Client)
	if err != nil {
		return ImageObservation{}, NewFault(CodeCaptureFailed, "GDI capture failed", err, PhaseCapture, time.Now(), geo.Version)
	}
	return ImageObservation{
		Image:           img,
		GeometryVersion: geo.Version,
		FrameRef:        frameRef,
		RequestedAt:     meta.RequestedAt,
		CompletedAt:     meta.CompletedAt,
	}, nil
}
