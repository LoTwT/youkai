package collect

import (
	"image"
	"image/color"
	"math"
	"testing"
)

func TestCardGlyphConfirmationIsFailClosedWithoutModel(t *testing.T) {
	old := storageCardGlyphLDA
	storageCardGlyphLDA = cardGlyphLDA{Version: "unset"}
	defer func() { storageCardGlyphLDA = old }()
	img := image.NewRGBA(image.Rect(0, 0, 34, 26))
	img.SetRGBA(10, 10, color.RGBA{R: 255, G: 255, B: 255, A: 255})
	if (&PixelMatcher{}).confirmCardGlyph(img, img.Bounds(), 5) {
		t.Fatal("unset model must not confirm a card glyph")
	}
}

func TestCardGlyphConfirmationUsesIndependentComponent(t *testing.T) {
	old := storageCardGlyphLDA
	storageCardGlyphLDA = cardGlyphLDA{
		Version:  "test",
		Weights:  append([]float64{1}, make([]float64, cardGlyphFeatureLen-1)...),
		Bias:     1e9,
		MinScore: 0.5,
	}
	defer func() { storageCardGlyphLDA = old }()
	img := image.NewRGBA(image.Rect(0, 0, 34, 26))
	// A small glyph-like blob; the component anchor, not an F1 offset, must
	// locate it. The fake model only exercises the integration contract.
	for y := 6; y < 14; y++ {
		for x := 8; x < 20; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
		}
	}
	if !(&PixelMatcher{}).confirmCardGlyph(img, img.Bounds(), 5) {
		t.Fatal("fake model should confirm a single component for class 5")
	}
	if (&PixelMatcher{}).confirmCardGlyph(img, img.Bounds(), 6) {
		t.Fatal("fake model must not confirm the other class")
	}
}

func TestCardGlyphFeatureOrder(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 34, 26))
	for y := 6; y < 14; y++ {
		for x := 8; x < 20; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
		}
	}
	feat, ok := cardGlyphFeatures(img)
	if !ok {
		t.Fatal("synthetic component not extracted")
	}
	if len(feat) != cardGlyphFeatureLen {
		t.Fatalf("feature length = %d, want %d", len(feat), cardGlyphFeatureLen)
	}
	// The first 336 values are row-major core luma; the next 16 are row
	// means; the last 21 are column means. Check the boundary slices rather
	// than duplicating the normalisation formula.
	sumRows := 0.0
	for i := 0; i < 21; i++ {
		sumRows += feat[i]
	}
	if math.Abs(feat[336]-sumRows/21) > 1e-12 {
		t.Fatalf("first row mean = %v, want %v", feat[336], sumRows/21)
	}
	sumCols := 0.0
	for y := 0; y < 16; y++ {
		sumCols += feat[y*21]
	}
	if math.Abs(feat[352]-sumCols/16) > 1e-12 {
		t.Fatalf("first column mean = %v, want %v", feat[352], sumCols/16)
	}
}

func TestCardGlyphLumaMatchesExportIntegerAverage(t *testing.T) {
	if got := cardGlyphLuma(255, 0, 0); got != 85 {
		t.Fatalf("integer RGB average = %v, want 85", got)
	}
}

func TestCardGlyphConfirmationRejectsOverflow(t *testing.T) {
	old := storageCardGlyphLDA
	storageCardGlyphLDA = cardGlyphLDA{
		Version:  "test",
		Weights:  append([]float64{math.MaxFloat64}, make([]float64, cardGlyphFeatureLen-1)...),
		Bias:     math.MaxFloat64,
		MinScore: 0.5,
	}
	defer func() { storageCardGlyphLDA = old }()
	img := image.NewRGBA(image.Rect(0, 0, 34, 26))
	for y := 6; y < 14; y++ {
		for x := 8; x < 20; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
		}
	}
	if (&PixelMatcher{}).confirmCardGlyph(img, img.Bounds(), 5) {
		t.Fatal("overflowed model score must not confirm")
	}
}
