package collect

import (
	"github.com/LoTwT/youkai/internal/platform"
	"image"
	"image/draw"
	_ "image/png"
	"os"
	"testing"
	"time"
)

func offlineLoadedFrame(tb testing.TB, path string, id uint64, l Layout) platform.Frame {
	tb.Helper()
	f, err := os.Open(path)
	if err != nil {
		tb.Fatal(err)
	}
	defer f.Close()
	im, _, err := image.Decode(f)
	if err != nil {
		tb.Fatal(err)
	}
	if im.Bounds().Dx() != l.Image.X || im.Bounds().Dy() != l.Image.Y {
		tb.Fatalf("unexpected input size %v", im.Bounds())
	}
	out := image.NewRGBA(im.Bounds())
	draw.Draw(out, out.Bounds(), im, im.Bounds().Min, draw.Src)
	return platform.Frame{ID: id, Geometry: 1, Image: out, Content: l.Content, ArrivedAt: time.Unix(0, int64(id)*1e8)}
}
func offlineWeak(l Layout) platform.Frame {
	f := storageReviewPixelFrame(l, 4010, 10, 300, 323)
	weakSelectionReviewBorder(l, f, 9, 70)
	return f
}
