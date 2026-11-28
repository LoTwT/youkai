package collect

import (
	"image"
	"math"
)

// cardGlyphLDA is the derived two-class 5/6 shape model used as a second
// opinion for one card badge. The weights are generated from independently
// labelled 21x16 glyph cores by tools/storageglyphgen; no image, ROI, ledger
// or private record is embedded in the product.
type cardGlyphLDA struct {
	Version  string
	Weights  []float64
	Bias     float64
	MinScore float64
}

const (
	cardGlyphCoreWidth  = 21
	cardGlyphCoreHeight = 16
	cardGlyphFeatureLen = cardGlyphCoreWidth*cardGlyphCoreHeight +
		cardGlyphCoreHeight + cardGlyphCoreWidth
)

func (m cardGlyphLDA) valid() bool {
	if m.Version == "" || m.Version == "unset" || len(m.Weights) != cardGlyphFeatureLen || m.MinScore <= 0 ||
		math.IsNaN(m.MinScore) || math.IsInf(m.MinScore, 0) || math.IsNaN(m.Bias) || math.IsInf(m.Bias, 0) {
		return false
	}
	for _, w := range m.Weights {
		if math.IsNaN(w) || math.IsInf(w, 0) {
			return false
		}
	}
	return true
}

// confirmCardGlyph is deliberately independent of the F1 winner offset. It
// locates one near-neutral bright component in the current 34x26 window,
// anchors a fixed 21x16 core at that component's bbox centre, and compares
// the derived shape score with the C1 winner. It reads no ledger, no detail
// pane and no pre frame.
func (m *PixelMatcher) confirmCardGlyph(img *image.RGBA, window image.Rectangle, want int) bool {
	if m == nil || img == nil || window.Empty() || !window.In(img.Bounds()) ||
		window.Dx() != 34 || window.Dy() != 26 || len(storageCardGlyphLDA.Weights) == 0 {
		return false
	}
	if !storageCardGlyphLDA.valid() || want < 1 || want > 6 {
		return false
	}
	crop := image.NewRGBA(image.Rect(0, 0, 34, 26))
	for y := 0; y < 26; y++ {
		for x := 0; x < 34; x++ {
			crop.Set(x, y, img.At(window.Min.X+x, window.Min.Y+y))
		}
	}
	feat, ok := cardGlyphFeatures(crop)
	if !ok {
		return false
	}
	score := storageCardGlyphLDA.Bias
	norm := 0.0
	for i, w := range storageCardGlyphLDA.Weights {
		score += w * feat[i]
		norm += w * w
		if math.IsNaN(score) || math.IsInf(score, 0) || math.IsNaN(norm) || math.IsInf(norm, 0) {
			return false
		}
	}
	norm = math.Sqrt(norm)
	if norm < 1e-9 || math.IsInf(norm, 0) {
		return false
	}
	margin := math.Abs(score) / norm
	if math.IsNaN(margin) || math.IsInf(margin, 0) || margin < storageCardGlyphLDA.MinScore {
		return false
	}
	if score > 0 {
		return want == 5
	}
	return want == 6
}

func cardGlyphLuma(r, g, b int) float64 {
	return float64((r + g + b) / 3)
}

// cardGlyphFeatures reproduces the BadgeShape core exactly: locate one
// unambiguous near-neutral bright component, exclude the yellow selection
// border, center a fixed 21x16 core on the component bbox, z-normalize its
// luma, then append 16 row means and 21 column means. The order is part of
// the model contract.
func cardGlyphFeatures(im *image.RGBA) ([]float64, bool) {
	if im == nil || im.Bounds().Dx() != 34 || im.Bounds().Dy() != 26 {
		return nil, false
	}
	component, area, count, ok := cardGlyphComponent(im)
	if !ok || count != 1 || area == 0 {
		return nil, false
	}
	core := image.Rect(6, 5, 6+cardGlyphCoreWidth, 5+cardGlyphCoreHeight)
	center := component.Min.Add(component.Size().Div(2))
	core = core.Add(center.Sub(image.Pt(cardGlyphCoreWidth/2, cardGlyphCoreHeight/2)))
	if core.Min.X < 0 {
		core = core.Add(image.Pt(-core.Min.X, 0))
	}
	if core.Min.Y < 0 {
		core = core.Add(image.Pt(0, -core.Min.Y))
	}
	if core.Max.X > 34 {
		core = core.Add(image.Pt(34-core.Max.X, 0))
	}
	if core.Max.Y > 26 {
		core = core.Add(image.Pt(0, 26-core.Max.Y))
	}
	vals := make([]float64, 0, cardGlyphCoreWidth*cardGlyphCoreHeight)
	for y := core.Min.Y; y < core.Max.Y; y++ {
		for x := core.Min.X; x < core.Max.X; x++ {
			i := im.PixOffset(x, y)
			// Match the exporter's integer RGB average exactly; changing this
			// to a floating division changes every normalized feature.
			vals = append(vals, cardGlyphLuma(int(im.Pix[i]), int(im.Pix[i+1]), int(im.Pix[i+2])))
		}
	}
	mean := 0.0
	for _, v := range vals {
		mean += v
	}
	mean /= float64(len(vals))
	sd := 0.0
	for _, v := range vals {
		d := v - mean
		sd += d * d
	}
	sd = math.Sqrt(sd / float64(len(vals)))
	if sd < 1e-9 {
		return nil, false
	}
	feat := make([]float64, 0, cardGlyphFeatureLen)
	for _, v := range vals {
		feat = append(feat, (v-mean)/sd)
	}
	for y := 0; y < cardGlyphCoreHeight; y++ {
		sum := 0.0
		for x := 0; x < cardGlyphCoreWidth; x++ {
			sum += feat[y*cardGlyphCoreWidth+x]
		}
		feat = append(feat, sum/float64(cardGlyphCoreWidth))
	}
	for x := 0; x < cardGlyphCoreWidth; x++ {
		sum := 0.0
		for y := 0; y < cardGlyphCoreHeight; y++ {
			sum += feat[y*cardGlyphCoreWidth+x]
		}
		feat = append(feat, sum/float64(cardGlyphCoreHeight))
	}
	return feat, len(feat) == cardGlyphFeatureLen
}

// cardGlyphComponent returns the bbox and area of the one glyph-like
// component. A second component at >=60% of the largest area is ambiguous.
func cardGlyphComponent(im *image.RGBA) (image.Rectangle, int, int, bool) {
	on := [34][26]bool{}
	seen := [34][26]bool{}
	for y := 0; y < 26; y++ {
		for x := 0; x < 34; x++ {
			i := im.PixOffset(x, y)
			r, g, b := int(im.Pix[i]), int(im.Pix[i+1]), int(im.Pix[i+2])
			yValue := (r + g + b) / 3
			chroma := maxChannel(r, g, b) - minChannel(r, g, b)
			selected := g > 170 && b < 100 && r > 130
			on[x][y] = !selected && yValue >= 200 && chroma <= 24
		}
	}
	type found struct {
		bbox image.Rectangle
		area int
	}
	var comps []found
	for y := 0; y < 26; y++ {
		for x := 0; x < 34; x++ {
			if !on[x][y] || seen[x][y] {
				continue
			}
			stack := []image.Point{{x, y}}
			seen[x][y] = true
			minX, minY, maxX, maxY := x, y, x, y
			area := 0
			for len(stack) > 0 {
				p := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				area++
				if p.X < minX {
					minX = p.X
				}
				if p.Y < minY {
					minY = p.Y
				}
				if p.X > maxX {
					maxX = p.X
				}
				if p.Y > maxY {
					maxY = p.Y
				}
				for dy := -1; dy <= 1; dy++ {
					for dx := -1; dx <= 1; dx++ {
						nx, ny := p.X+dx, p.Y+dy
						if nx < 0 || nx >= 34 || ny < 0 || ny >= 26 || (dx == 0 && dy == 0) || seen[nx][ny] || !on[nx][ny] {
							continue
						}
						seen[nx][ny] = true
						stack = append(stack, image.Point{nx, ny})
					}
				}
			}
			bb := image.Rect(minX, minY, maxX+1, maxY+1)
			if area < 20 || area > 450 || bb.Dx() < 6 || bb.Dx() > 28 || bb.Dy() < 6 || bb.Dy() > 22 {
				continue
			}
			comps = append(comps, found{bbox: bb, area: area})
		}
	}
	if len(comps) == 0 {
		return image.Rectangle{}, 0, 0, false
	}
	// Stable selection by area; an equal-size second component is ambiguous.
	best := comps[0]
	for _, c := range comps[1:] {
		if c.area > best.area {
			best = c
		}
	}
	if len(comps) > 1 {
		for _, c := range comps {
			if c != best && c.area >= best.area*6/10 {
				return image.Rectangle{}, 0, len(comps), false
			}
		}
	}
	return best.bbox, best.area, len(comps), true
}

func maxChannel(r, g, b int) int {
	if r < g {
		r = g
	}
	if r < b {
		r = b
	}
	return r
}

func minChannel(r, g, b int) int {
	if r > g {
		r = g
	}
	if r > b {
		r = b
	}
	return r
}
