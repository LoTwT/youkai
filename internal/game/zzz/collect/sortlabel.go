package collect

import (
	"bytes"
	_ "embed"
	"fmt"
	"image"
	"image/draw"
	"image/png"
)

// The filter panel's sort label is a fixed text region, not a live
// value: the panel dims the warehouse behind it and the label renders
// identically across the recorded panel frames. Comparing the region
// against this reference therefore verifies both the label's identity
// and its position; it is not general OCR and deliberately fails closed
// when the player changes the sort order.
//
// The reference is the crop (1100,205)-(1260,275) of the M2 panel frame
// s2-01-panel-slot1-before/frame-0001.png, whose "稀有度" label occupies
// (1138,231,61,22) at 1920x1080. All visible label pixels are inside the
// crop; the surrounding panel chrome pins the alignment.
//
//go:embed assets/sort-label-rarity.png
var sortLabelRarityPNG []byte

// The Windows local client renders the same label at native pixels, so it
// needs its own template. This is the crop (1285,124)-(1467,201) of the
// 2026-11-02 W3a evidence frame S2/frame-0000.png (SHA-256
// 5632e847f40597363600f74d2056fa6e79fb9ab74f3f24643d744c629f60aac6),
// 182×77 pixels. The crop is the mac template's text margin converted to
// the reference canvas (x 1/0.859, y (v-96)/0.859): the measured Windows
// text box is (1329,154)-(1396,175). It contains the whole label and only
// panel pixels, and stops before the dropdown arrow. Native means 1:1 on
// the 1920×1080 reference canvas; scaled tiers drop the claim instead of
// resampling this template (see ScaledLayout).
//
//go:embed assets/sort-label-rarity-winlocal.png
var sortLabelRarityWindowsLocalPNG []byte

const sortLabelRarity = "稀有度"

var (
	maccloudSortLabelTemplate     = decodeSortLabelTemplate(sortLabelRarityPNG)
	windowsLocalSortLabelTemplate = decodeSortLabelTemplate(sortLabelRarityWindowsLocalPNG)
)

func decodeSortLabelTemplate(raw []byte) *image.RGBA {
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		panic(fmt.Sprintf("collect: decode embedded sort-label template: %v", err))
	}
	b := img.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(out, out.Bounds(), img, b.Min, draw.Src)
	return out
}
