// Command iconassets exports the selected Youkai artwork for the app and tray.
// Run from the repository root; the original artwork is never overwritten.
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"

	"golang.org/x/image/draw"
)

func main() {
	if err := generate(); err != nil {
		fmt.Fprintln(os.Stderr, "iconassets:", err)
		os.Exit(1)
	}
}

func generate() error {
	f, err := os.Open("docs/design/icon-2026-11-17/a-vortex-eye.png")
	if err != nil {
		return err
	}
	src, err := png.Decode(f)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	b := src.Bounds()
	if b.Dx() != b.Dy() {
		return fmt.Errorf("source must be square, got %v", b.Size())
	}
	app := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(app, app.Bounds(), src, b.Min, draw.Src)
	// Only remove light pixels connected to the canvas edge. The dark tile
	// separates these white exterior corners from the ivory eye inside it.
	exterior := make([]bool, b.Dx()*b.Dy())
	var queue []image.Point
	visit := func(p image.Point) {
		if !p.In(app.Bounds()) {
			return
		}
		i := p.Y*b.Dx() + p.X
		c := app.NRGBAAt(p.X, p.Y)
		if exterior[i] || max(c.R, c.G, c.B) < 80 {
			return
		}
		exterior[i] = true
		queue = append(queue, p)
	}
	for x := range b.Dx() {
		visit(image.Pt(x, 0))
		visit(image.Pt(x, b.Dy()-1))
	}
	for y := range b.Dy() {
		visit(image.Pt(0, y))
		visit(image.Pt(b.Dx()-1, y))
	}
	for i := 0; i < len(queue); i++ {
		p := queue[i]
		visit(p.Add(image.Pt(-1, 0)))
		visit(p.Add(image.Pt(1, 0)))
		visit(p.Add(image.Pt(0, -1)))
		visit(p.Add(image.Pt(0, 1)))
	}
	for _, p := range queue {
		app.SetNRGBA(p.X, p.Y, color.NRGBA{})
	}
	// The macOS template uses only the alpha of the eye and its lower sweep.
	// Both ivory and magenta are well above the near-black tile; soften the
	// threshold before downsampling to retain the original anti-aliased edges.
	mask := image.NewNRGBA(app.Bounds())
	markBounds := image.Rectangle{}
	for y := range b.Dy() {
		for x := range b.Dx() {
			c := app.NRGBAAt(x, y)
			if c.A == 0 {
				continue
			}
			a := uint8(min(255, max(0, (int(max(c.R, c.G, c.B))-64)*255/64)))
			if a == 0 {
				continue
			}
			mask.SetNRGBA(x, y, color.NRGBA{A: a})
			markBounds = markBounds.Union(image.Rect(x, y, x+1, y+1))
		}
	}
	if markBounds.Empty() {
		return fmt.Errorf("source has no foreground mark")
	}
	template := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	w, h := markBounds.Dx(), markBounds.Dy()
	scale := 28.0 / float64(max(w, h))
	w, h = int(float64(w)*scale+0.5), int(float64(h)*scale+0.5)
	target := image.Rect((32-w)/2, (32-h)/2, (32-w)/2+w, (32-h)/2+h)
	draw.CatmullRom.Scale(template, target, mask, markBounds, draw.Src, nil)
	for _, output := range []struct {
		path string
		img  image.Image
	}{
		{"cmd/youkai/appicon.png", resize(app, 1024)},
		{"cmd/youkai/trayicon.png", resize(app, 32)},
		{"cmd/youkai/trayicon-template.png", template},
	} {
		if err := writePNG(output.path, output.img); err != nil {
			return err
		}
		fmt.Println(output.path)
	}
	return nil
}

func resize(src image.Image, size int) image.Image {
	dst := image.NewNRGBA(image.Rect(0, 0, size, size))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Src, nil)
	return dst
}

func writePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	err = png.Encode(f, img)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}
