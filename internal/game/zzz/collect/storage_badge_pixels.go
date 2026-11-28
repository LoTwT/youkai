package collect

import (
	"image"
	"math/bits"
	"slices"
)

// cardBadgePixels holds thresholded pixels for one badge in one frame.
// Bit x in row y is bright by the same RGB-average rule as glyphMask.f1At.
type cardBadgePixels struct {
	image      *image.RGBA
	window     image.Rectangle
	rows       [64]uint64
	brightness int
	ready      bool
}

func (p *cardBadgePixels) prepare(brightness int) {
	if p.ready && p.brightness == brightness {
		return
	}
	for y := range p.window.Dy() {
		offset := p.image.PixOffset(p.window.Min.X, p.window.Min.Y+y)
		var row uint64
		for x := range p.window.Dx() {
			i := offset + x*4
			rgb := p.image.Pix[i : i+3 : i+3]
			if (int(rgb[0])+int(rgb[1])+int(rgb[2]))/3 >= brightness {
				row |= uint64(1) << x
			}
		}
		p.rows[y] = row
	}
	p.brightness, p.ready = brightness, true
}

type packedBadgeMask struct {
	rows  [64]uint64
	ink   int
	width uint64
}

func packBadgeMask(mask glyphMask) packedBadgeMask {
	var packed packedBadgeMask
	packed.width = ^uint64(0) >> (64 - mask.size().X)
	for y, row := range mask.rows {
		for x := range len(row) {
			if row[x] == '#' {
				packed.rows[y] |= uint64(1) << x
				packed.ink++
			}
		}
	}
	return packed
}

func (m *packedBadgeMask) score(rows []uint64, x int) float64 {
	matched, observed := 0, 0
	for y, row := range rows {
		visible := (row >> x) & m.width
		matched += bits.OnesCount64(visible & m.rows[y])
		observed += bits.OnesCount64(visible)
	}
	// 2*TP+FP+FN equals the combined foreground counts.
	return 2 * float64(matched) / float64(observed+m.ink)
}

// best searches in the original mask and offset order. If requested, it
// collects every equally scoring placement for the existing conflict check.
func (p *cardBadgePixels) best(set glyphSet, offsets *[]image.Point) (float64, bool) {
	p.prepare(set.minBrightness)
	best := -1.0
	for _, mask := range set.masks {
		size := mask.size()
		if size.X > p.window.Dx() || size.Y > p.window.Dy() {
			continue
		}
		packed := packBadgeMask(mask)
		for y := 0; y <= p.window.Dy()-size.Y; y++ {
			for x := 0; x <= p.window.Dx()-size.X; x++ {
				score := packed.score(p.rows[y:y+size.Y], x)
				if score > best {
					best = score
					if offsets != nil {
						*offsets = (*offsets)[:0]
					}
				}
				if offsets != nil && score == best {
					at := p.window.Min.Add(image.Pt(x, y))
					if !slices.Contains(*offsets, at) {
						*offsets = append(*offsets, at)
					}
				}
			}
		}
	}
	return best, best >= 0
}

func (p *cardBadgePixels) bestAt(set glyphSet, at image.Point) (float64, bool) {
	p.prepare(set.minBrightness)
	best := -1.0
	for _, mask := range set.masks {
		size := mask.size()
		area := image.Rectangle{Min: at, Max: at.Add(size)}
		if !area.In(p.image.Bounds()) {
			continue
		}
		var score float64
		if area.In(p.window) {
			packed := packBadgeMask(mask)
			offset := at.Sub(p.window.Min)
			score = packed.score(p.rows[offset.Y:offset.Y+size.Y], offset.X)
		} else {
			// The original fixed-offset check permits a differently sized
			// rival mask to extend past the badge window, within the image.
			score = mask.f1At(p.image, at, set.minBrightness)
		}
		if score > best {
			best = score
		}
	}
	return best, best >= 0
}
