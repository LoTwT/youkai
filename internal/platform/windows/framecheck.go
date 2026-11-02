package windows

import (
	"fmt"
	"time"
)

// CodeFrameContentBlank: a sampled frame edge (or the optional content
// probe) is entirely black. A window that extends past the screen edge is
// captured as black there by GDI; the check is a fallback for cases the
// geometry checks cannot see (a window that still lies inside the monitor
// rect but whose content was never painted, an exclusive-fullscreen
// capture, or a display edge the work-area test cannot attribute).
const CodeFrameContentBlank = "frameContentBlank"

// frameBlackMax is the per-channel ceiling of a pixel counted as black.
// GDI's screen-outside pixels are exactly zero; the small tolerance keeps
// compression-free rendering artifacts from masking the verdict.
const frameBlackMax = 8

// EdgeMargins is the measured legitimate all-black margin from each
// client edge, in image pixels.
type EdgeMargins struct {
	Top, Right, Bottom, Left int
}

// CheckFrameContent inspects a captured client frame:
//
//   - from each edge, content must appear within the calibrated all-black
//     margin. The Windows local client draws a legitimate black strip at
//     the top (measured 22 px at 1920×1080 on 2026-11-01), so a literal
//     "first row must not be black" test would reject every valid frame.
//     The margin is per edge and per tier; the other three measured edges
//     have no black margin. A margin is the number of whole black lines
//     the game may draw, and at least one line within it must contain a
//     non-black pixel;
//   - when content is non-empty, that region must contain at least one
//     non-black pixel (the optional bottom-bar presence check).
//
// It is a fallback only: passing proves the pixels are not uniformly black,
// never that the frame shows a current game page.
func CheckFrameContent(im *CapturedImage, content PixelRect, margins EdgeMargins) *PlatformFault {
	if im == nil || im.Width <= 0 || im.Height <= 0 || len(im.Pixels) < im.Stride*im.Height {
		return NewFault(CodeFrameContentBlank, "the captured frame has no usable pixels", nil, PhasePrecheck, time.Now(), 0).
			WithParams(map[string]string{"reason": "invalidImage"})
	}
	w, h := im.Width, im.Height
	m := margins
	if m.Top < 0 {
		m.Top = 0
	}
	if m.Bottom < 0 {
		m.Bottom = 0
	}
	if m.Left < 0 {
		m.Left = 0
	}
	if m.Right < 0 {
		m.Right = 0
	}
	if m.Top >= h || m.Bottom >= h || m.Left >= w || m.Right >= w {
		return NewFault(CodeFrameContentBlank, "the calibrated edge margins leave no content to sample", nil, PhasePrecheck, time.Now(), 0).
			WithParams(map[string]string{"reason": "invalidMargins"})
	}
	blackAt := func(x, y int) bool {
		i := y*im.Stride + 4*x
		return im.Pixels[i] <= frameBlackMax && im.Pixels[i+1] <= frameBlackMax && im.Pixels[i+2] <= frameBlackMax
	}
	lineHasContent := func(fixed int, horizontal bool) bool {
		if horizontal {
			for x := 0; x < w; x++ {
				if !blackAt(x, fixed) {
					return true
				}
			}
			return false
		}
		for y := 0; y < h; y++ {
			if !blackAt(fixed, y) {
				return true
			}
		}
		return false
	}
	edgeHasContent := func(first, last int, horizontal bool) bool {
		for line := first; line <= last; line++ {
			if lineHasContent(line, horizontal) {
				return true
			}
		}
		return false
	}
	if !edgeHasContent(0, m.Top, true) {
		return blankEdge("top", m.Top)
	}
	if !edgeHasContent(h-1-m.Bottom, h-1, true) {
		return blankEdge("bottom", h-1-m.Bottom)
	}
	if !edgeHasContent(0, m.Left, false) {
		return blankEdge("left", m.Left)
	}
	if !edgeHasContent(w-1-m.Right, w-1, false) {
		return blankEdge("right", w-1-m.Right)
	}
	if !content.Empty() && (PixelRect{Max: PixelPoint{X: w, Y: h}}).ContainsRect(content) {
		found := false
		for y := content.Min.Y; y < content.Max.Y && !found; y++ {
			for x := content.Min.X; x < content.Max.X; x++ {
				if !blackAt(x, y) {
					found = true
					break
				}
			}
		}
		if !found {
			return NewFault(CodeFrameContentBlank, "the bottom content region is entirely black", nil, PhasePrecheck, time.Now(), 0).
				WithParams(map[string]string{
					"reason": "content",
					"region": frameRectString(content),
				})
		}
	}
	return nil
}

func blankEdge(edge string, within int) *PlatformFault {
	return NewFault(CodeFrameContentBlank, fmt.Sprintf("the %s frame edge is entirely black", edge), nil, PhasePrecheck, time.Now(), 0).
		WithParams(map[string]string{
			"reason": "edge",
			"edge":   edge,
			"line":   fmt.Sprintf("%d", within),
		})
}

func frameRectString(r PixelRect) string {
	return fmt.Sprintf("%d,%d %dx%d", r.Min.X, r.Min.Y, r.Dx(), r.Dy())
}
