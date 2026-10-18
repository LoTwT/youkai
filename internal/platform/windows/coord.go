package windows

import (
	"errors"
	"fmt"
)

// MaxAbsoluteAxis is the inclusive upper bound of the MOUSEINPUT
// normalized absolute coordinate domain: 0..65535.
const MaxAbsoluteAxis = 65535

// MouseAbsolute is a point in the MOUSEEVENTF_ABSOLUTE normalized
// coordinate domain 0..65535.
type MouseAbsolute struct {
	DX, DY int
}

// CoordinateSpace is the desktop geometry the normalized domain maps
// onto. Primary is the primary screen, always at origin (0,0); Virtual
// is the whole virtual desktop, which extends into negative coordinates
// when displays sit left of or above the primary. Without the
// MOUSEEVENTF_VIRTUALDESK flag, 0..65535 maps the primary screen only —
// the platform decides per point which space applies and never feeds a
// raw screen pixel into the normalized domain.
type CoordinateSpace struct {
	Primary PixelRect
	Virtual PixelRect
}

var (
	// ErrPointOutOfRange: the point lies outside the virtual desktop.
	ErrPointOutOfRange = errors.New("screen point outside the virtual desktop")
	// ErrMalformedCoordinateSpace: the measured desktop geometry is
	// inconsistent (empty areas, or the primary screen outside the
	// virtual desktop).
	ErrMalformedCoordinateSpace = errors.New("malformed coordinate space")
)

// PointToMouseAbsolute maps a screen physical pixel p into the
// normalized absolute domain and reports whether the mapping requires
// the MOUSEEVENTF_VIRTUALDESK flag.
//
// Rules:
//
//   - Points inside the primary screen use the default primary mapping
//     (virtualDesk=false); points outside the primary but inside the
//     virtual desktop use the whole-desktop mapping (virtualDesk=true);
//     points outside the virtual desktop are refused — a raw screen
//     pixel or a negative coordinate is never passed through as-is.
//   - With the mapped area A and the pixel offset o = p - A.Min (valid
//     range 0..A.Dx()-1), the normalized value is
//     ceil(o * 65535 / (A.Dx()-1)): the first pixel of the area maps to
//     0 and the last pixel to 65535. The ceiling (not nearest) rounding
//     is what makes the mapping pixel-exact under the documented system
//     model below; a nearest-rounded value can decode one pixel short.
//   - The documented system model decodes v back to the pixel
//     floor(v * (A.Dx()-1) / 65535) (MouseAbsoluteToPoint). Under that
//     model the round trip is exact for every pixel of areas up to
//     65536 pixels per axis. The model matches the documented endpoints
//     (0,0 → top-left corner, 65535,65535 → bottom-right corner); the
//     actual system behavior is confirmed on real hardware in the field
//     test round.
//   - A one-pixel axis degenerates to 0, because both corners of the
//     area are that single pixel.
//
// Relative mouse motion is never used for positioning: it is affected by
// pointer speed and acceleration settings, which makes it unrepeatable.
func PointToMouseAbsolute(p PixelPoint, space CoordinateSpace) (MouseAbsolute, bool, error) {
	if space.Primary.Empty() || space.Virtual.Empty() ||
		!space.Virtual.ContainsRect(space.Primary) ||
		space.Virtual.Min.X > 0 || space.Virtual.Min.Y > 0 {
		return MouseAbsolute{}, false, fmt.Errorf("%w: primary %v, virtual %v", ErrMalformedCoordinateSpace, space.Primary, space.Virtual)
	}
	area := space.Primary
	virtualDesk := false
	if !space.Primary.Contains(p) {
		if !space.Virtual.Contains(p) {
			return MouseAbsolute{}, false, fmt.Errorf("%w: point %v, virtual %v", ErrPointOutOfRange, p, space.Virtual)
		}
		area = space.Virtual
		virtualDesk = true
	}
	return MouseAbsolute{
		DX: mapAxisToAbsolute(p.X-area.Min.X, area.Dx()),
		DY: mapAxisToAbsolute(p.Y-area.Min.Y, area.Dy()),
	}, virtualDesk, nil
}

// mapAxisToAbsolute maps a non-negative pixel offset within an axis of
// span pixels (valid offsets 0..span-1) to the normalized domain, with
// ceiling rounding.
func mapAxisToAbsolute(offset, span int) int {
	if span <= 1 {
		return 0
	}
	last := span - 1
	return (offset*MaxAbsoluteAxis + last - 1) / last
}

// mapAxisFromAbsolute is the documented system model: the normalized
// value decodes onto the pixel offset floor(v*(span-1)/65535).
func mapAxisFromAbsolute(v, span int) int {
	if span <= 1 {
		return 0
	}
	return v * (span - 1) / MaxAbsoluteAxis
}

// MouseAbsoluteToPoint is the documented inverse of
// PointToMouseAbsolute. The dispatch path never uses it; tests use it to
// pin the round-trip behavior, and it documents what the system is
// expected to do with the values the platform sends.
func MouseAbsoluteToPoint(a MouseAbsolute, virtualDesk bool, space CoordinateSpace) PixelPoint {
	area := space.Primary
	if virtualDesk {
		area = space.Virtual
	}
	return PixelPoint{
		X: area.Min.X + mapAxisFromAbsolute(a.DX, area.Dx()),
		Y: area.Min.Y + mapAxisFromAbsolute(a.DY, area.Dy()),
	}
}
