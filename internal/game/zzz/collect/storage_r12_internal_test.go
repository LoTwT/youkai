package collect

import (
	"image"
	"image/color"
	"testing"
)

// TestStorageSubstatProbeShortFourthValue pins the 2026-11-23 field failure:
// a four-row detail's short fourth value measured 174 ink pixels in the value
// column, while the three-row control measured 0 and a normal four-row control
// measured 572. The middle band must stay unknown; only the four-row tier
// moves below the short value.
func TestStorageSubstatProbeShortFourthValue(t *testing.T) {
	l := StorageLayout()
	m := NewPixelMatcher(l, StorageCalibration())
	for _, tc := range []struct {
		name   string
		ink    int
		left   int
		want   int
		wantQS bool
	}{
		{name: "three rows with left set-effect label", ink: 0, left: 914, want: 3, wantQS: true},
		{name: "three rows empty probe", ink: 0, want: 3, wantQS: true},
		{name: "short four-row value 9", ink: 174, want: 4, wantQS: true},
		{name: "normal four-row control", ink: 572, want: 4, wantQS: true},
		{name: "three-row tier boundary", ink: 60, want: 3, wantQS: true},
		{name: "unknown animation band", ink: 80, want: 0, wantQS: true},
		{name: "unknown band remains below four", ink: 99, want: 0, wantQS: true},
		{name: "four-row tier boundary", ink: 100, want: 4, wantQS: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			img := image.NewRGBA(image.Rectangle{Max: l.Image})
			fillTestInk(img, m.detailLine(LineName), 2000, color.RGBA{R: 255, G: 255, B: 255, A: 255})
			fillTestInk(img, l.QualityBadge, 250, color.RGBA{R: 255, G: 200, B: 40, A: 255})
			if tc.left > 0 {
				left := m.detailLine(LineSub4)
				left.Max.X = l.Substat4Probe.Min.X - 1
				fillTestInk(img, left, tc.left, color.RGBA{R: 255, G: 255, B: 255, A: 255})
			}
			fillTestInk(img, l.Substat4Probe, tc.ink, color.RGBA{R: 255, G: 255, B: 255, A: 255})
			ev := m.detailEvidence(img)
			if ev == nil {
				t.Fatal("detail evidence is nil")
			}
			if got := ev.Quality == QualityS; got != tc.wantQS {
				t.Fatalf("quality S = %t, want %t", got, tc.wantQS)
			}
			if ev.Substats != tc.want {
				t.Fatalf("substats = %d, want %d for probe ink %d (left %d)", ev.Substats, tc.want, tc.ink, tc.left)
			}
		})
	}
}

func fillTestInk(img *image.RGBA, r image.Rectangle, n int, c color.RGBA) {
	for y := r.Min.Y; y < r.Max.Y && n > 0; y++ {
		for x := r.Min.X; x < r.Max.X && n > 0; x++ {
			img.SetRGBA(x, y, c)
			n--
		}
	}
}
