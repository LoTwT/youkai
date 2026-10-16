package recognition

import (
	"fmt"
	"image"
	"math"
)

// Fixed input geometry and the width strategy, mirroring the pinned upstream
// preprocessing exactly (docs/specs/t3-ocr-runtime-model-samples.md §固定 small
// 制品的输入与解码, which follows RapidOCR's ch_ppocr_rec at the pinned
// commit): BGR color order, height 48, linear-interpolation resize, then
// float32 CHW normalization (px/255-0.5)/0.5, and right padding with the
// normalized value 0 — not black pixels.
const (
	// inputHeight is the model's fixed input height.
	inputHeight = 48
	// baseWidth is the upstream baseline width (rec_img_shape [3,48,320]): a
	// batch's padded width is int(48 * max(320/48, batch max w/h ratio)), so a
	// single line is never padded narrower than 320 and lines wider than the
	// baseline set their own width. Upstream batches several lines and widens
	// to the batch maximum; this package recognizes one line per call, which
	// is that rule with a batch of one. Batched small-batch inference is a
	// later optimization to compare with samples (T3 §效率判断与边界).
	baseWidth = 320
)

// preprocess converts a text-line crop into the model's normalized float32
// CHW input and returns it together with the padded width.
//
// Steps, in upstream order: convert to BGR, resize to height 48 with the
// aspect-derived width using linear interpolation (with cv2-style uint8
// rounding), normalize, and right-pad with normalized 0. The returned slice
// has length 3*48*width laid out as [C][H][W] with B first.
func preprocess(img image.Image) ([]float32, int, error) {
	if img == nil {
		return nil, 0, fmt.Errorf("%w: image is nil", ErrInvalidImage)
	}
	b := img.Bounds()
	srcW, srcH := b.Dx(), b.Dy()
	if srcW <= 0 || srcH <= 0 {
		return nil, 0, fmt.Errorf("%w: empty bounds %v", ErrInvalidImage, b)
	}

	bgr := imageToBGR(img, srcW, srcH)

	ratio := float64(srcW) / float64(srcH)
	// Padded batch width: int(48 * max(320/48, ratio)); Python's int()
	// truncates, and so does the float64→int conversion below.
	width := int(float64(inputHeight) * math.Max(float64(baseWidth)/float64(inputHeight), ratio))
	// The image itself keeps its aspect ratio: resize to ceil(48 * ratio)
	// wide, clamped to the batch width (upstream resize_norm_img).
	resizedW := int(math.Ceil(float64(inputHeight) * ratio))
	if resizedW > width {
		resizedW = width
	}
	if resizedW < 1 {
		resizedW = 1
	}

	// Zero-initialized: the pad region must stay exactly 0 in normalized
	// space (equivalent to mid-gray 127.5 before normalization), which
	// differs from padding black pixels before normalizing (they would
	// become -1).
	data := make([]float32, 3*inputHeight*width)
	plane := inputHeight * width
	for y := 0; y < inputHeight; y++ {
		// Center-aligned mapping like cv2.resize: src = (dst+0.5)*scale-0.5,
		// clamped to the source so edges replicate (cv2's border handling for
		// INTER_LINEAR resize).
		sy := (float64(y)+0.5)*float64(srcH)/float64(inputHeight) - 0.5
		if sy < 0 {
			sy = 0
		} else if sy > float64(srcH-1) {
			sy = float64(srcH - 1)
		}
		y0 := int(sy)
		y1 := y0 + 1
		if y1 > srcH-1 {
			y1 = srcH - 1
		}
		fy := sy - float64(y0)
		for x := 0; x < resizedW; x++ {
			sx := (float64(x)+0.5)*float64(srcW)/float64(resizedW) - 0.5
			if sx < 0 {
				sx = 0
			} else if sx > float64(srcW-1) {
				sx = float64(srcW - 1)
			}
			x0 := int(sx)
			x1 := x0 + 1
			if x1 > srcW-1 {
				x1 = srcW - 1
			}
			fx := sx - float64(x0)

			// Destination position inside the padded [width]-wide rows.
			dst := y*width + x
			for c := 0; c < 3; c++ {
				top := float64(bgr[(y0*srcW+x0)*3+c])*(1-fx) + float64(bgr[(y0*srcW+x1)*3+c])*fx
				bottom := float64(bgr[(y1*srcW+x0)*3+c])*(1-fx) + float64(bgr[(y1*srcW+x1)*3+c])*fx
				v := top*(1-fy) + bottom*fy
				// Round to uint8 before normalizing, matching cv2.resize's
				// uint8 output; upstream normalizes the rounded result.
				if v < 0 {
					v = 0
				} else if v > 255 {
					v = 255
				}
				p := uint8(v + 0.5)
				// Normalize in float32, step by step, like the upstream
				// float32 pipeline: /255, -0.5, /0.5.
				f := float32(p) / 255
				f -= 0.5
				f /= 0.5
				data[c*plane+dst] = f
			}
		}
	}
	return data, width, nil
}

// imageToBGR decodes img into tightly packed BGR rows. Any image.Image is
// accepted via At(); the scanner's crops are small opaque PNG regions, so the
// generic path is sufficient here. Pixels are read through Go's
// alpha-premultiplied RGBA(), which equals the plain channels for the opaque
// crops this package receives; alpha blending against a background is not
// applied (T2 owns capture-time color/alpha conventions).
func imageToBGR(img image.Image, w, h int) []byte {
	b := img.Bounds()
	out := make([]byte, w*h*3)
	i := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			out[i] = byte(bl >> 8)
			out[i+1] = byte(g >> 8)
			out[i+2] = byte(r >> 8)
			i += 3
		}
	}
	return out
}
