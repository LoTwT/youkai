package collect

import (
	"image"
	"image/color"
	"testing"
)

func TestReferenceCalibrationValidates(t *testing.T) {
	if err := ReferenceCalibration().validate(); err != nil {
		t.Fatalf("the reference calibration must be structurally valid: %v", err)
	}
}

// TestReferenceScrollbarEndsAreSafe pins the scrollbar end thresholds to the
// measured reference track. The old mac-frame values, scaled as lengths,
// admitted a thumb 70 reference px down as top and a thumb ending 30 px early
// as bottom; both must now be rejected.
func TestReferenceScrollbarEndsAreSafe(t *testing.T) {
	const (
		referenceTop    = 148
		referenceBottom = 931
		// One row of scrollbar travel, not the list row pitch: the mac
		// calibration moves the thumb about 28 px per row (112 px for 4 rows);
		// at the reference scale that is about 33 px.
		referenceRowTravel = 33
	)
	for _, size := range []image.Point{image.Pt(1920, 1080), image.Pt(2560, 1440), image.Pt(1280, 720)} {
		size := size
		t.Run(size.String(), func(t *testing.T) {
			tr, err := ClientRectTransform(size)
			if err != nil {
				t.Fatal(err)
			}
			layout, err := ScaledLayout(ReferenceLayout(), tr)
			if err != nil {
				t.Fatal(err)
			}
			cal, err := ScaledCalibration(ReferenceCalibration(), tr.Scale)
			if err != nil {
				t.Fatal(err)
			}
			m := NewPixelMatcher(layout, cal)
			if !m.valid {
				t.Fatal("the scaled matcher is not structurally valid")
			}
			x := layout.ScrollTrack.Min.X
			top := int(roundFloat(float64(referenceTop) * tr.Scale))
			bottom := int(roundFloat(float64(referenceBottom) * tr.Scale))
			rowTravel := int(roundFloat(float64(referenceRowTravel) * tr.Scale))
			probe70 := int(roundFloat(70 * tr.Scale))
			probe30 := int(roundFloat(30 * tr.Scale))

			if gotTop, gotBottom := m.scrollEnds(thumbFrame(layout.Image, x, top, bottom)); !gotTop || !gotBottom {
				t.Fatalf("resting top/bottom thumb classified top=%v bottom=%v, want both true", gotTop, gotBottom)
			}
			if gotTop, gotBottom := m.scrollEnds(thumbFrame(layout.Image, x, top+rowTravel, bottom)); gotTop || !gotBottom {
				t.Errorf("thumb one row down classified top=%v bottom=%v, want top=false bottom=true", gotTop, gotBottom)
			}
			if gotTop, gotBottom := m.scrollEnds(thumbFrame(layout.Image, x, top, bottom-rowTravel)); !gotTop || gotBottom {
				t.Errorf("thumb ending one row early classified top=%v bottom=%v, want top=true bottom=false", gotTop, gotBottom)
			}
			if gotTop, _ := m.scrollEnds(thumbFrame(layout.Image, x, top+probe70, bottom)); gotTop {
				t.Errorf("thumb 70 reference px down still classified top")
			}
			if _, gotBottom := m.scrollEnds(thumbFrame(layout.Image, x, top, bottom-probe30)); gotBottom {
				t.Errorf("thumb ending 30 reference px early still classified bottom")
			}
		})
	}
}

func thumbFrame(size image.Point, x, y0, y1 int) *image.RGBA {
	img := image.NewRGBA(image.Rectangle{Max: size})
	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	if y0 < 0 {
		y0 = 0
	}
	if y1 >= size.Y {
		y1 = size.Y - 1
	}
	for y := y0; y <= y1; y++ {
		img.SetRGBA(x, y, white)
	}
	return img
}
