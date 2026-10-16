package recognition

import (
	"image"
	"image/color"
	"math"
	"path/filepath"
	"testing"
)

// The unit tests below run without any native ONNX Runtime artifacts: they
// only exercise preprocessing, charset assembly, and decoding. The tests
// loading the real library and model live in integration_test.go and skip
// when the fetchdeps resources are absent (as on CI).

// solidImage returns a w×h NRGBA image filled with c.
func solidImage(w, h int, c color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

func TestPreprocessShapeAndPadding(t *testing.T) {
	// A 136×28 line (the set-name crop size): ratio 136/28 ≈ 4.857 < 320/48,
	// so the padded width stays at the 320 baseline and the line itself is
	// resized to ceil(48×136/28) = 233 pixels wide.
	data, width, err := preprocess(solidImage(136, 28, color.NRGBA{R: 255, G: 255, B: 255, A: 255}))
	if err != nil {
		t.Fatalf("preprocess: %v", err)
	}
	if width != baseWidth {
		t.Fatalf("padded width = %d, want %d", width, baseWidth)
	}
	if len(data) != 3*inputHeight*width {
		t.Fatalf("tensor size = %d, want %d", len(data), 3*inputHeight*width)
	}
	resizedW := int(math.Ceil(float64(inputHeight) * 136.0 / 28.0))
	if resizedW != 234 {
		t.Fatalf("resized width = %d, want 234", resizedW)
	}

	plane := inputHeight * width
	for c := 0; c < 3; c++ {
		planeData := data[c*plane : (c+1)*plane]
		for y := 0; y < inputHeight; y++ {
			for x := 0; x < width; x++ {
				got := planeData[y*width+x]
				if x < resizedW {
					// White normalizes to exactly 1 in every channel.
					if got != 1 {
						t.Fatalf("plane %d at (x=%d,y=%d) = %v, want 1", c, x, y, got)
					}
				} else if got != 0 {
					// The right padding is the normalized 0, not the
					// normalized black (-1).
					t.Fatalf("plane %d padding at (x=%d,y=%d) = %v, want 0", c, x, y, got)
				}
			}
		}
	}
}

func TestPreprocessWidthRules(t *testing.T) {
	cases := []struct {
		name             string
		w, h             int
		wantWidth        int // padded batch width
		wantResizedWidth int // content width after resize
	}{
		{
			// A 234×22 line (the stat-line crop size): ratio 234/22; the
			// upstream rule truncates 48×ratio to 510 while ceil gives 511,
			// so the content is clamped back to the padded width.
			name:             "wide line truncates and clamps",
			w:                234,
			h:                22,
			wantWidth:        510,
			wantResizedWidth: 510,
		},
		{
			name:             "below baseline pads to 320",
			w:                47,
			h:                10,
			wantWidth:        320,
			wantResizedWidth: 226,
		},
		{
			name:             "nearly square line",
			w:                10,
			h:                48,
			wantWidth:        320,
			wantResizedWidth: 10,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, width, err := preprocess(solidImage(tc.w, tc.h, color.NRGBA{A: 255}))
			if err != nil {
				t.Fatalf("preprocess: %v", err)
			}
			if width != tc.wantWidth {
				t.Fatalf("padded width = %d, want %d", width, tc.wantWidth)
			}
			if len(data) != 3*inputHeight*width {
				t.Fatalf("tensor size = %d, want %d", len(data), 3*inputHeight*width)
			}
			// Solid black normalizes to exactly -1 everywhere, so the content
			// width is where the -1 region ends in the middle row.
			plane := inputHeight * width
			mid := data[plane+inputHeight/2*width : plane+(inputHeight/2+1)*width]
			content := 0
			for content < width && mid[content] == -1 {
				content++
			}
			if content != tc.wantResizedWidth {
				t.Fatalf("content width = %d, want %d", content, tc.wantResizedWidth)
			}
		})
	}
}

func TestPreprocessValues(t *testing.T) {
	// 2×1 image: left pixel pure blue, right pixel white. Height scaling is
	// trivial (single source row); horizontal blending is observable.
	img := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	img.SetNRGBA(0, 0, color.NRGBA{B: 255, A: 255})
	img.SetNRGBA(1, 0, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	data, width, err := preprocess(img)
	if err != nil {
		t.Fatalf("preprocess: %v", err)
	}
	if width != baseWidth {
		t.Fatalf("padded width = %d, want %d", width, baseWidth)
	}
	plane := inputHeight * width
	b := func(x, y int) float32 { return data[0*plane+y*width+x] }
	g := func(x, y int) float32 { return data[1*plane+y*width+x] }
	r := func(x, y int) float32 { return data[2*plane+y*width+x] }

	// BGR channel order: the blue source pixel maps to B=1, G=-1, R=-1.
	if b(0, 0) != 1 || g(0, 0) != -1 || r(0, 0) != -1 {
		t.Fatalf("channel order wrong at left edge: B=%v G=%v R=%v", b(0, 0), g(0, 0), r(0, 0))
	}
	// The right edge of the content region replicates the white pixel exactly
	// (no partial blend); the padded columns beyond it stay 0.
	resizedW := int(math.Ceil(float64(inputHeight) * 2.0))
	if b(resizedW-1, 0) != 1 || g(resizedW-1, 0) != 1 || r(resizedW-1, 0) != 1 {
		t.Fatalf("content right edge not pure white: B=%v G=%v R=%v", b(resizedW-1, 0), g(resizedW-1, 0), r(resizedW-1, 0))
	}
	if b(width-1, 0) != 0 || g(width-1, 0) != 0 || r(width-1, 0) != 0 {
		t.Fatalf("padded right edge not zero: B=%v G=%v R=%v", b(width-1, 0), g(width-1, 0), r(width-1, 0))
	}
	// Column 47 of 96 content pixels blends blue→white with fx = 47.5/48-0.5:
	// G and R interpolate to 124.84 → rounds to 125 → (125/255-0.5)/0.5 =
	// -5/255; B stays 255.
	wantBlend := float32(-5.0 / 255.0)
	for _, v := range []float32{g(47, 0), r(47, 0)} {
		if math.Abs(float64(v-wantBlend)) > 1e-6 {
			t.Fatalf("blend value = %v, want %v", v, wantBlend)
		}
	}
	if b(47, 0) != 1 {
		t.Fatalf("blend B = %v, want 1", b(47, 0))
	}

	// Vertical interpolation on a 1×2 image: top black, bottom white. At
	// output row 24, fy = 24.5/24-0.5 = 0.5208…, so the value is 255×0.5208 =
	// 132.9… → rounds to 133 → 2×133/255-1 = 11/255.
	img2 := image.NewNRGBA(image.Rect(0, 0, 1, 2))
	img2.SetNRGBA(0, 0, color.NRGBA{A: 255})
	img2.SetNRGBA(0, 1, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	data2, width2, err := preprocess(img2)
	if err != nil {
		t.Fatalf("preprocess: %v", err)
	}
	plane2 := inputHeight * width2
	if data2[plane2+0*width2] != -1 {
		t.Fatalf("row 0 not black: %v", data2[plane2+0*width2])
	}
	if data2[plane2+47*width2] != 1 {
		t.Fatalf("row 47 not white: %v", data2[plane2+47*width2])
	}
	wantRow := float32(11.0 / 255.0)
	if got := data2[plane2+24*width2]; math.Abs(float64(got-wantRow)) > 1e-6 {
		t.Fatalf("row 24 = %v, want %v", got, wantRow)
	}
}

func TestPreprocessInvalidImage(t *testing.T) {
	if _, _, err := preprocess(nil); err == nil {
		t.Fatal("preprocess(nil) succeeded, want error")
	}
	// An image with empty bounds cannot be constructed through the standard
	// library, so the nil case covers the guard.
}

func TestBuildCharset(t *testing.T) {
	cases := []struct {
		name string
		meta string
		want []string
	}{
		{name: "plain", meta: "A\nB\nC", want: []string{blankToken, "A", "B", "C", " "}},
		{name: "trailing newline", meta: "A\nB\nC\n", want: []string{blankToken, "A", "B", "C", " "}},
		{name: "crlf", meta: "A\r\nB", want: []string{blankToken, "A", "B", " "}},
		{name: "interior empty line kept", meta: "A\n\nB", want: []string{blankToken, "A", "", "B", " "}},
		{name: "empty metadata", meta: "", want: []string{blankToken, " "}},
		{name: "single char", meta: "中", want: []string{blankToken, "中", " "}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := buildCharset(tc.meta)
			if len(got) != len(tc.want) {
				t.Fatalf("table = %q (len %d), want %q (len %d)", got, len(got), tc.want, len(tc.want))
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("table[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestDecodeCTC(t *testing.T) {
	// classes = 5: 0 blank, 1..3 characters, 4 unused.
	charset := []string{blankToken, "A", "B", "C", " "}
	prob := func(steps []int, probs []float32) []float32 {
		out := make([]float32, len(steps)*5)
		for i, c := range steps {
			out[i*5+c] = probs[i]
		}
		return out
	}

	t.Run("adjacent duplicates collapse", func(t *testing.T) {
		// A A B → AB; the mean score only includes the kept steps. The probs
		// are float32, so compute the expectation from the float32 values.
		data := prob([]int{1, 1, 2}, []float32{0.9, 0.8, 0.7})
		text, score := decodeCTC(data, 5, charset)
		if text != "AB" {
			t.Fatalf("text = %q, want AB", text)
		}
		want := (float64(float32(0.9)) + float64(float32(0.7))) / 2
		if math.Abs(score-want) > 1e-9 {
			t.Fatalf("score = %v, want %v", score, want)
		}
	})
	t.Run("blank separated duplicates survive", func(t *testing.T) {
		// A A blank A → AA: the first run collapses, the post-blank A is kept.
		data := prob([]int{1, 1, 0, 1}, []float32{0.9, 0.9, 0.99, 0.6})
		text, score := decodeCTC(data, 5, charset)
		if text != "AA" {
			t.Fatalf("text = %q, want AA", text)
		}
		if math.Abs(score-0.75) > 1e-9 {
			t.Fatalf("score = %v, want 0.75", score)
		}
	})
	t.Run("blank runs collapse before removal", func(t *testing.T) {
		data := prob([]int{0, 0, 1}, []float32{0.9, 0.9, 0.5})
		text, score := decodeCTC(data, 5, charset)
		if text != "A" || math.Abs(score-0.5) > 1e-9 {
			t.Fatalf("text = %q score = %v, want A 0.5", text, score)
		}
	})
	t.Run("all blank is empty with zero score", func(t *testing.T) {
		data := prob([]int{0, 0, 0}, []float32{0.9, 0.9, 0.9})
		text, score := decodeCTC(data, 5, charset)
		if text != "" || score != 0 {
			t.Fatalf("text = %q score = %v, want empty 0", text, score)
		}
	})
	t.Run("empty probs", func(t *testing.T) {
		text, score := decodeCTC(nil, 5, charset)
		if text != "" || score != 0 {
			t.Fatalf("text = %q score = %v, want empty 0", text, score)
		}
	})
	t.Run("space is a normal class", func(t *testing.T) {
		// The appended space (last class) is decoded like any character.
		data := prob([]int{1, 4, 2}, []float32{0.9, 0.9, 0.9})
		text, score := decodeCTC(data, 5, charset)
		if text != "A B" {
			t.Fatalf("text = %q, want \"A B\"", text)
		}
		want := float64(float32(0.9))
		if math.Abs(score-want) > 1e-9 {
			t.Fatalf("score = %v, want %v", score, want)
		}
	})
}

func TestRequireAbsPath(t *testing.T) {
	if _, err := requireAbsPath("", "x"); err == nil {
		t.Fatal("empty path accepted")
	}
	if _, err := requireAbsPath("relative/onnx.so", "x"); err == nil {
		t.Fatal("relative path accepted")
	}
	// Absolute on every supported platform: on Windows a path needs a drive
	// letter, so a Unix-style "/abs/..." literal would be rejected there.
	// t.TempDir always returns a platform-absolute path.
	abs := filepath.Join(t.TempDir(), "onnx.so")
	if _, err := requireAbsPath(abs, "x"); err != nil {
		t.Fatalf("absolute path %q rejected: %v", abs, err)
	}
}
