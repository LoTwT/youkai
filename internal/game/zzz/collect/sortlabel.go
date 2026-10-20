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

const sortLabelRarity = "稀有度"

var maccloudSortLabelTemplate = decodeSortLabelTemplate(sortLabelRarityPNG)

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
