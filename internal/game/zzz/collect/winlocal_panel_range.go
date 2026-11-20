package collect

import (
	"image"
	"image/color"
)

// panelRangeProbes describe the Windows native 1920×1080 panel. The open
// panel dims the funnel; its colors are distinct from the warehouse probe.
type panelRangeProbes struct {
	funnel     ColorProbe
	sFill      ColorProbe
	otherFills [2]ColorProbe
	sLabel     image.Rectangle
}

func (p panelRangeProbes) regions() []image.Rectangle {
	return []image.Rectangle{p.funnel.At, p.sFill.At, p.otherFills[0].At, p.otherFills[1].At, p.sLabel}
}

// windowsPanelSStencil is the red-channel mask (R >= 218) of the S glyph
// at (1412,593)-(1438,615) on the 2026-11-20 position 3 reset frame. The
// S-only frame differs by one pixel and the residual-set frame by 184.
// The absolute label check rules out reading the fill of a different chip
// if selected sets pushed the S control down.
var windowsPanelSStencil = [...]string{
	"..........................",
	"..........................",
	"..........................",
	"..........................",
	"..........######..........",
	".........#########........",
	"........##########........",
	"........####...####.......",
	"........####..............",
	"........#######...........",
	".........########.........",
	"...........########.......",
	"..............#####.......",
	"........###....####.......",
	"........####...####.......",
	"........###########.......",
	".........#########........",
	"...........#####..........",
	"..........................",
	"..........................",
	"..........................",
	"..........................",
}

func windowsPanelSLabelMatches(img *image.RGBA, r image.Rectangle) bool {
	mismatches := 0
	for y, row := range windowsPanelSStencil {
		for x := range len(row) {
			white := img.RGBAAt(r.Min.X+x, r.Min.Y+y).R >= 218
			if white != (row[x] == '#') {
				mismatches++
				if mismatches > 2 {
					return false
				}
			}
		}
	}
	return true
}

func (m *PixelMatcher) panelRangeEvidence(img *image.RGBA) *PanelRangeEvidence {
	p := m.L.panelRange
	if p == nil {
		return nil
	}
	evidence := &PanelRangeEvidence{}
	if !windowsPanelSLabelMatches(img, p.sLabel) {
		return evidence
	}
	funnel := FilterState(m.classify(img, p.funnel))
	switch FilterState(m.classify(img, p.sFill)) {
	case FilterClear:
		evidence.RarityS = FilterClear
		evidence.ResetVerified = funnel == FilterClear
	case FilterActive:
		// Verify the S fill independently of the funnel and require the
		// observed neutral fills of the neighboring rarities.
		if funnel == FilterActive && m.classify(img, p.otherFills[0]) == "clear" && m.classify(img, p.otherFills[1]) == "clear" {
			evidence.RarityS = FilterActive
		}
	}
	return evidence
}

// windowsPanelRangeCalibration is measured in native reference pixels.
// ScaledLayout drops these probes for any non-identity transform.
func windowsPanelRangeCalibration() *panelRangeProbes {
	neutral := map[string]color.RGBA{"clear": {R: 11, G: 11, B: 11, A: 255}}
	return &panelRangeProbes{
		funnel: ColorProbe{
			At: image.Rect(189, 53, 192, 57), Tol: 6,
			Colors: map[string]color.RGBA{
				"clear": {R: 8, G: 8, B: 8, A: 255}, "active": {R: 34, G: 89, B: 163, A: 255},
			},
		},
		sFill: ColorProbe{
			At: image.Rect(1356, 599, 1372, 609), Tol: 6,
			Colors: map[string]color.RGBA{
				"clear": {R: 28, G: 28, B: 28, A: 255}, "active": {R: 0, G: 139, B: 255, A: 255},
			},
		},
		otherFills: [2]ColorProbe{
			{At: image.Rect(1630, 599, 1646, 609), Colors: neutral, Tol: 6},
			{At: image.Rect(1356, 657, 1372, 667), Colors: neutral, Tol: 6},
		},
		sLabel: image.Rect(1412, 593, 1438, 615),
	}
}
