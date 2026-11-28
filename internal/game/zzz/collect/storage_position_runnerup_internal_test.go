package collect

import (
	"image"
	"image/color"
	"testing"
)

// TestStorageRunnerUpUsesWinnerOffset pins the 5/6 boundary fixed by C1.
// The image contains the calibrated 5A shape at (3,3) plus the first 75
// foreground pixels of 6C at (4,3). Before C1 the runner-up could win at a
// different offset and make this a conflict; at the winner's offset it
// falls behind the unchanged 0.04 margin.
func TestStorageRunnerUpUsesWinnerOffset(t *testing.T) {
	layout := StorageLayout()
	five := layout.CardPositions[4].masks[0]
	six := layout.CardPositions[5].masks[2]
	img := image.NewRGBA(image.Rect(0, 0, 34, 26))
	paintMask(img, five, 3, 3)
	added := 0
	for y, row := range six.rows {
		for x, ch := range row {
			if ch != '#' {
				continue
			}
			if added == 75 {
				break
			}
			img.SetRGBA(x+4, y+3, colorRunnerUpWhite)
			added++
		}
		if added == 75 {
			break
		}
	}
	got := (&PixelMatcher{}).positionVerdict(img, img.Bounds(), layout.CardPositions)
	if !got.known() || got.Position != 5 {
		t.Fatalf("verdict = %+v, want known slot 5", got)
	}
}

func paintMask(img *image.RGBA, mask glyphMask, ox, oy int) {
	for y, row := range mask.rows {
		for x, ch := range row {
			if ch == '#' {
				img.SetRGBA(x+ox, y+oy, colorRunnerUpWhite)
			}
		}
	}
}

var colorRunnerUpWhite = color.RGBA{R: 255, G: 255, B: 255, A: 255}
