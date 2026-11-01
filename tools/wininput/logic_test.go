package main

import (
	"errors"
	"flag"
	"io"
	"math"
	"strings"
	"testing"

	"github.com/LoTwT/youkai/internal/platform/windows"
)

func TestParseSlotPar(t *testing.T) {
	cases := []struct {
		in       string
		row, col int
		ok       bool
	}{
		{"1,1", 1, 1, true},
		{"5,4", 5, 4, true},
		{" 2 , 3 ", 2, 3, true},
		{"", 0, 0, false},
		{"1", 0, 0, false},
		{"1,2,3", 0, 0, false},
		{"0,1", 0, 0, false},
		{"1,0", 0, 0, false},
		{"6,1", 0, 0, false},
		{"1,5", 0, 0, false},
		{"-1,1", 0, 0, false},
		{"a,1", 0, 0, false},
		{"1,b", 0, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			row, col, err := ParseSlotPar(tc.in)
			if tc.ok {
				if err != nil || row != tc.row || col != tc.col {
					t.Fatalf("ParseSlotPar(%q) = %d,%d,%v; want %d,%d,nil", tc.in, row, col, err, tc.row, tc.col)
				}
				return
			}
			if err == nil {
				t.Fatalf("ParseSlotPar(%q) = %d,%d,nil; want an error", tc.in, row, col)
			}
		})
	}
}

func TestCanvasPoint(t *testing.T) {
	point, err := CanvasPoint(1, 1)
	if err != nil {
		t.Fatalf("CanvasPoint(1,1): %v", err)
	}
	if point != (windows.PixelPoint{X: 139, Y: 185}) {
		t.Fatalf("CanvasPoint(1,1) = %v, want (139,185)", point)
	}
	point, err = CanvasPoint(2, 3)
	if err != nil {
		t.Fatalf("CanvasPoint(2,3): %v", err)
	}
	if point != (windows.PixelPoint{X: 139 + 2*130, Y: 185 + 170}) {
		t.Fatalf("CanvasPoint(2,3) = %v, want (399,355)", point)
	}
	if _, err := CanvasPoint(0, 1); err == nil {
		t.Fatal("row 0 must be refused")
	}
	if _, err := CanvasPoint(1, 0); err == nil {
		t.Fatal("column 0 must be refused")
	}
	if _, err := CanvasPoint(6, 1); err == nil {
		t.Fatal("row 6 must be refused")
	}
	if _, err := CanvasPoint(1, 5); err == nil {
		t.Fatal("column 5 must be refused")
	}
}

func TestStandardTiers(t *testing.T) {
	if len(StandardTiers) == 0 {
		t.Fatal("no standard tiers")
	}
	for _, tier := range StandardTiers {
		if tier.Width*9 != tier.Height*16 {
			t.Fatalf("%s is not exact 16:9", tier.Name())
		}
		if math.Abs(tier.Scale-float64(tier.Width)/ReferenceCanvasWidth) > 1e-12 {
			t.Fatalf("%s scale %v is not width/1920", tier.Name(), tier.Scale)
		}
		matched, err := MatchStandardTier(tier.Width, tier.Height)
		if err != nil || matched != tier {
			t.Fatalf("MatchStandardTier(%s) = %+v,%v; want the table entry", tier.Name(), matched, err)
		}
	}
	if _, err := MatchStandardTier(1920, 1200); err == nil {
		t.Fatal("1920x1200 must be refused")
	}
	if _, err := MatchStandardTier(1366, 768); err == nil {
		t.Fatal("1366x768 is not exact 16:9 and must be refused")
	}
	if _, err := MatchStandardTier(1000, 1000); err == nil {
		t.Fatal("1000x1000 must be refused")
	}
	if _, err := MatchStandardTier(0, 1080); err == nil {
		t.Fatal("zero width must be refused")
	}
}

func TestPlanGameClick(t *testing.T) {
	t.Run("1920x1080 with origin", func(t *testing.T) {
		geo := geometryForTier(Tier{Width: 1920, Height: 1080, Scale: 1}, windows.PixelPoint{X: 100, Y: 200})
		plan, err := PlanGameClick(1, 1, geo)
		if err != nil {
			t.Fatalf("PlanGameClick: %v", err)
		}
		if plan.CanvasPoint != (windows.PixelPoint{X: 139, Y: 185}) {
			t.Fatalf("canvas point = %v", plan.CanvasPoint)
		}
		if plan.ClientPoint != (windows.PixelPoint{X: 139, Y: 185}) {
			t.Fatalf("client point = %v", plan.ClientPoint)
		}
		if plan.ScreenPoint != (windows.PixelPoint{X: 239, Y: 385}) {
			t.Fatalf("screen point = %v", plan.ScreenPoint)
		}
		if plan.Tier.Name() != "1920x1080" {
			t.Fatalf("tier = %s", plan.Tier.Name())
		}
		image := windows.PixelRect{Max: windows.PixelPoint{X: 1920, Y: 1080}}
		for name, region := range map[string]windows.PixelRect{
			"borderLeft":   plan.BorderLeft,
			"borderBottom": plan.BorderBottom,
			"nameRow":      plan.NameRow,
		} {
			if region.Empty() || !image.ContainsRect(region) {
				t.Fatalf("%s region %v is empty or outside the client image", name, region)
			}
		}
	})

	t.Run("125 percent tier with negative origin", func(t *testing.T) {
		tier, err := MatchStandardTier(2400, 1350)
		if err != nil {
			t.Fatal(err)
		}
		geo := geometryForTier(tier, windows.PixelPoint{X: -1000, Y: 50})
		plan, err := PlanGameClick(1, 1, geo)
		if err != nil {
			t.Fatalf("PlanGameClick: %v", err)
		}
		if plan.ClientPoint != (windows.PixelPoint{X: 174, Y: 231}) {
			t.Fatalf("client point = %v, want (174,231)", plan.ClientPoint)
		}
		if plan.ScreenPoint != (windows.PixelPoint{X: -826, Y: 281}) {
			t.Fatalf("screen point = %v, want (-826,281)", plan.ScreenPoint)
		}
		if !geo.Client.Contains(plan.ScreenPoint) {
			t.Fatalf("screen point %v is outside %v", plan.ScreenPoint, geo.Client)
		}
	})

	t.Run("4K last cell", func(t *testing.T) {
		tier, err := MatchStandardTier(3840, 2160)
		if err != nil {
			t.Fatal(err)
		}
		geo := geometryForTier(tier, windows.PixelPoint{})
		plan, err := PlanGameClick(5, 4, geo)
		if err != nil {
			t.Fatalf("PlanGameClick: %v", err)
		}
		if plan.CanvasPoint != (windows.PixelPoint{X: 529, Y: 865}) {
			t.Fatalf("canvas point = %v", plan.CanvasPoint)
		}
		if plan.ClientPoint != (windows.PixelPoint{X: 1058, Y: 1730}) {
			t.Fatalf("client point = %v, want (1058,1730)", plan.ClientPoint)
		}
	})

	t.Run("every tier and slot fits", func(t *testing.T) {
		for _, tier := range StandardTiers {
			geo := geometryForTier(tier, windows.PixelPoint{X: -50, Y: 900})
			image := windows.PixelRect{Max: windows.PixelPoint{X: tier.Width, Y: tier.Height}}
			for row := 1; row <= ReferenceRows; row++ {
				for column := 1; column <= ReferenceColumns; column++ {
					plan, err := PlanGameClick(row, column, geo)
					if err != nil {
						t.Fatalf("%s slot %d,%d: %v", tier.Name(), row, column, err)
					}
					if !image.Contains(plan.ClientPoint) {
						t.Fatalf("%s slot %d,%d: client point %v outside %v", tier.Name(), row, column, plan.ClientPoint, image)
					}
					for name, region := range map[string]windows.PixelRect{
						"borderLeft":   plan.BorderLeft,
						"borderBottom": plan.BorderBottom,
						"nameRow":      plan.NameRow,
					} {
						if region.Empty() || !image.ContainsRect(region) {
							t.Fatalf("%s slot %d,%d: %s region %v outside %v", tier.Name(), row, column, name, region, image)
						}
					}
				}
			}
		}
	})

	t.Run("tier and slot refusals", func(t *testing.T) {
		geo := geometryForTier(Tier{Width: 1920, Height: 1200, Scale: 1}, windows.PixelPoint{})
		if _, err := PlanGameClick(1, 1, geo); err == nil {
			t.Fatal("a non-16:9 tier must be refused")
		}
		geo = geometryForTier(Tier{Width: 1920, Height: 1080, Scale: 1}, windows.PixelPoint{})
		if _, err := PlanGameClick(6, 1, geo); err == nil {
			t.Fatal("row 6 must be refused")
		}
		if _, err := PlanGameClick(1, 5, geo); err == nil {
			t.Fatal("column 5 must be refused")
		}
	})
}

func TestPlanNotepadClick(t *testing.T) {
	geo := windows.ClientGeometry{
		Version: 3,
		Client: windows.PixelRect{
			Min: windows.PixelPoint{X: 100, Y: 200},
			Max: windows.PixelPoint{X: 900, Y: 800},
		},
	}
	plan, err := PlanNotepadClick(geo)
	if err != nil {
		t.Fatalf("PlanNotepadClick: %v", err)
	}
	if plan.EditRegion != (windows.PixelRect{Min: windows.PixelPoint{X: 16, Y: 72}, Max: windows.PixelPoint{X: 784, Y: 570}}) {
		t.Fatalf("edit region = %v", plan.EditRegion)
	}
	if plan.ClientPoint != (windows.PixelPoint{X: 400, Y: 321}) {
		t.Fatalf("client point = %v", plan.ClientPoint)
	}
	if plan.ScreenPoint != (windows.PixelPoint{X: 500, Y: 521}) {
		t.Fatalf("screen point = %v", plan.ScreenPoint)
	}
	if plan.HorizontalInsetPx != 16 || plan.TopInsetPx != 72 || plan.BottomInsetPx != 30 {
		t.Fatalf("insets = %d/%d/%d, want 16/72/30", plan.HorizontalInsetPx, plan.TopInsetPx, plan.BottomInsetPx)
	}
	if !geo.Client.Contains(plan.ScreenPoint) {
		t.Fatalf("screen point %v is outside %v", plan.ScreenPoint, geo.Client)
	}

	if _, err := PlanNotepadClick(windows.ClientGeometry{Client: windows.PixelRect{Max: windows.PixelPoint{X: 199, Y: 600}}}); err == nil {
		t.Fatal("a 199x600 client must be refused")
	}
	if _, err := PlanNotepadClick(windows.ClientGeometry{Client: windows.PixelRect{Max: windows.PixelPoint{X: 800, Y: 119}}}); err == nil {
		t.Fatal("a 800x119 client must be refused")
	}
}

func TestAnalyzeImage(t *testing.T) {
	t.Run("black", func(t *testing.T) {
		image := newImage(2, 2, 0, 0, 0)
		stats, err := AnalyzeImage(image)
		if err != nil {
			t.Fatalf("AnalyzeImage: %v", err)
		}
		if stats.Mean != 0 || stats.Variance != 0 || !stats.Black {
			t.Fatalf("stats = %+v, want black", stats)
		}
		if len(stats.PixelSHA256) != 64 {
			t.Fatalf("hash length = %d", len(stats.PixelSHA256))
		}
	})

	t.Run("gray is not black", func(t *testing.T) {
		image := newImage(1, 1, 10, 20, 30)
		stats, err := AnalyzeImage(image)
		if err != nil {
			t.Fatalf("AnalyzeImage: %v", err)
		}
		if stats.Black || math.Abs(stats.Mean-20) > 1e-9 || math.Abs(stats.Variance-200.0/3.0) > 1e-9 {
			t.Fatalf("stats = %+v, want mean 20 variance 200/3", stats)
		}
	})

	t.Run("alpha changes the hash but not statistics", func(t *testing.T) {
		opaque := newImage(1, 1, 10, 20, 30)
		clear := newImage(1, 1, 10, 20, 30)
		clear.Pixels[3] = 0
		opaqueStats, err := AnalyzeImage(opaque)
		if err != nil {
			t.Fatal(err)
		}
		clearStats, err := AnalyzeImage(clear)
		if err != nil {
			t.Fatal(err)
		}
		if opaqueStats.Mean != clearStats.Mean || opaqueStats.Variance != clearStats.Variance {
			t.Fatal("alpha changed the statistics")
		}
		if opaqueStats.PixelSHA256 == clearStats.PixelSHA256 {
			t.Fatal("alpha is part of the stored rows and must change the hash")
		}
	})

	if _, err := AnalyzeImage(nil); err == nil {
		t.Fatal("nil image must be refused")
	}
	if _, err := AnalyzeImage(&windows.CapturedImage{Width: 0, Height: 1, Stride: 4, Pixels: make([]byte, 4)}); err == nil {
		t.Fatal("zero width must be refused")
	}
	if _, err := AnalyzeImage(&windows.CapturedImage{Width: 2, Height: 1, Stride: 4, Pixels: make([]byte, 8)}); err == nil {
		t.Fatal("short buffer must be refused")
	}
}

func TestDiffRegion(t *testing.T) {
	t.Run("identical frames", func(t *testing.T) {
		before := newImage(4, 4, 10, 20, 30)
		after := copyImage(before)
		diff, err := DiffRegion(before, after, windows.PixelRect{Max: windows.PixelPoint{X: 4, Y: 4}}, 16)
		if err != nil {
			t.Fatalf("DiffRegion: %v", err)
		}
		if diff.MeanAbsDiff != 0 || diff.ChangedPixels != 0 || diff.MaxDelta != 0 || diff.LumaDelta != 0 {
			t.Fatalf("diff = %+v, want zero", diff)
		}
		if diff.Pixels != 16 || diff.ChangedFraction != 0 {
			t.Fatalf("diff = %+v", diff)
		}
	})

	t.Run("one white pixel", func(t *testing.T) {
		before := newImage(4, 4, 0, 0, 0)
		after := copyImage(before)
		setPixel(after, 1, 2, 255, 255, 255)
		diff, err := DiffRegion(before, after, windows.PixelRect{Max: windows.PixelPoint{X: 4, Y: 4}}, 16)
		if err != nil {
			t.Fatalf("DiffRegion: %v", err)
		}
		if diff.ChangedPixels != 1 || diff.MaxDelta != 255 {
			t.Fatalf("diff = %+v, want one 255-delta pixel", diff)
		}
		if math.Abs(diff.MeanAbsDiff-255.0/16.0) > 1e-9 {
			t.Fatalf("mean abs diff = %v, want %v", diff.MeanAbsDiff, 255.0/16.0)
		}
		if math.Abs(diff.LumaDelta-255.0/16.0) > 1e-9 {
			t.Fatalf("luma delta = %v, want %v", diff.LumaDelta, 255.0/16.0)
		}
		if math.Abs(diff.ChangedFraction-1.0/16.0) > 1e-9 {
			t.Fatalf("changed fraction = %v", diff.ChangedFraction)
		}
	})

	t.Run("alpha only is not a change", func(t *testing.T) {
		before := newImage(1, 1, 10, 20, 30)
		after := copyImage(before)
		after.Pixels[3] = 0
		diff, err := DiffRegion(before, after, windows.PixelRect{Max: windows.PixelPoint{X: 1, Y: 1}}, 16)
		if err != nil {
			t.Fatalf("DiffRegion: %v", err)
		}
		if diff.ChangedPixels != 0 || diff.MeanAbsDiff != 0 || diff.MaxDelta != 0 {
			t.Fatalf("alpha-only diff = %+v, want zero", diff)
		}
	})

	t.Run("refusals", func(t *testing.T) {
		before := newImage(2, 2, 0, 0, 0)
		after := copyImage(before)
		if _, err := DiffRegion(nil, after, windows.PixelRect{Max: windows.PixelPoint{X: 1, Y: 1}}, 16); err == nil {
			t.Fatal("nil before must be refused")
		}
		if _, err := DiffRegion(before, newImage(3, 2, 0, 0, 0), windows.PixelRect{Max: windows.PixelPoint{X: 1, Y: 1}}, 16); err == nil {
			t.Fatal("different sizes must be refused")
		}
		if _, err := DiffRegion(before, after, windows.PixelRect{}, 16); err == nil {
			t.Fatal("empty region must be refused")
		}
		if _, err := DiffRegion(before, after, windows.PixelRect{Max: windows.PixelPoint{X: 3, Y: 2}}, 16); err == nil {
			t.Fatal("outside region must be refused")
		}
		if _, err := DiffRegion(before, after, windows.PixelRect{Max: windows.PixelPoint{X: 1, Y: 1}}, 0); err == nil {
			t.Fatal("zero pixel delta must be refused")
		}
	})
}

func TestEvaluateNotepadFrame(t *testing.T) {
	plan, err := PlanNotepadClick(windows.ClientGeometry{
		Client: windows.PixelRect{Max: windows.PixelPoint{X: 400, Y: 300}},
	})
	if err != nil {
		t.Fatalf("PlanNotepadClick: %v", err)
	}
	before := newImage(400, 300, 10, 10, 10)

	t.Run("unchanged", func(t *testing.T) {
		effect, err := EvaluateNotepadFrame(before, copyImage(before), plan)
		if err != nil {
			t.Fatalf("EvaluateNotepadFrame: %v", err)
		}
		if effect.Effective || effect.Edit == nil || effect.Edit.ChangedPixels != 0 {
			t.Fatalf("effect = %+v, want unchanged", effect)
		}
	})

	t.Run("caret-sized change", func(t *testing.T) {
		after := copyImage(before)
		// A caret is a narrow vertical run; eight pixels is the fixed floor.
		for i := 0; i < NotepadMinChangedPixels; i++ {
			setPixel(after, plan.EditRegion.Min.X+20, plan.EditRegion.Min.Y+20+i, 255, 255, 255)
		}
		effect, err := EvaluateNotepadFrame(before, after, plan)
		if err != nil {
			t.Fatalf("EvaluateNotepadFrame: %v", err)
		}
		if !effect.Effective {
			t.Fatalf("effect = %+v, want effective", effect)
		}
	})

	t.Run("below pixel threshold", func(t *testing.T) {
		after := copyImage(before)
		for i := 0; i < NotepadMinChangedPixels-1; i++ {
			setPixel(after, plan.EditRegion.Min.X+20, plan.EditRegion.Min.Y+20+i, 255, 255, 255)
		}
		effect, err := EvaluateNotepadFrame(before, after, plan)
		if err != nil {
			t.Fatalf("EvaluateNotepadFrame: %v", err)
		}
		if effect.Effective {
			t.Fatalf("effect = %+v, want unchanged", effect)
		}
	})

	t.Run("below channel delta", func(t *testing.T) {
		after := copyImage(before)
		for i := 0; i < 40; i++ {
			setPixel(after, plan.EditRegion.Min.X+20, plan.EditRegion.Min.Y+20+i%20, 25, 25, 25)
		}
		effect, err := EvaluateNotepadFrame(before, after, plan)
		if err != nil {
			t.Fatalf("EvaluateNotepadFrame: %v", err)
		}
		if effect.Effective {
			t.Fatalf("effect = %+v, want unchanged: a 15-level delta is below the %d threshold", effect, NotepadPixelDelta)
		}
	})
}

func TestEvaluateGameFrame(t *testing.T) {
	tier, err := MatchStandardTier(1280, 720)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := PlanGameClick(1, 1, geometryForTier(tier, windows.PixelPoint{}))
	if err != nil {
		t.Fatalf("PlanGameClick: %v", err)
	}
	before := newImage(tier.Width, tier.Height, 10, 10, 10)

	fill := func(image *windows.CapturedImage, region windows.PixelRect, r, g, b uint8) {
		for y := region.Min.Y; y < region.Max.Y; y++ {
			for x := region.Min.X; x < region.Max.X; x++ {
				setPixel(image, x, y, r, g, b)
			}
		}
	}
	fillPixels := func(image *windows.CapturedImage, region windows.PixelRect, count int, r, g, b uint8) {
		remaining := count
		for y := region.Min.Y; y < region.Max.Y && remaining > 0; y++ {
			for x := region.Min.X; x < region.Max.X && remaining > 0; x++ {
				setPixel(image, x, y, r, g, b)
				remaining--
			}
		}
	}

	t.Run("unchanged", func(t *testing.T) {
		effect, err := EvaluateGameFrame(before, copyImage(before), plan)
		if err != nil {
			t.Fatalf("EvaluateGameFrame: %v", err)
		}
		if effect.Effective || effect.BorderLeft == nil || effect.BorderBottom == nil || effect.Detail == nil {
			t.Fatalf("effect = %+v, want unchanged with all metrics", effect)
		}
	})

	t.Run("border only", func(t *testing.T) {
		after := copyImage(before)
		fillPixels(after, plan.BorderLeft, BorderMinChangedPixels+10, 255, 255, 255)
		effect, err := EvaluateGameFrame(before, after, plan)
		if err != nil {
			t.Fatalf("EvaluateGameFrame: %v", err)
		}
		if effect.Effective {
			t.Fatalf("effect = %+v, want no effect without the detail change", effect)
		}
		if effect.BorderLeft.ChangedPixels < BorderMinChangedPixels {
			t.Fatalf("border metric = %+v", effect.BorderLeft)
		}
	})

	t.Run("detail only", func(t *testing.T) {
		after := copyImage(before)
		fillPixels(after, plan.NameRow, 500, 255, 255, 255)
		effect, err := EvaluateGameFrame(before, after, plan)
		if err != nil {
			t.Fatalf("EvaluateGameFrame: %v", err)
		}
		if effect.Effective {
			t.Fatalf("effect = %+v, want no effect without the border change", effect)
		}
		if effect.Detail.MeanAbsDiff < DetailMADThreshold || effect.Detail.ChangedFraction < DetailMinChangedFraction {
			t.Fatalf("detail metric = %+v", effect.Detail)
		}
	})

	t.Run("both conditions", func(t *testing.T) {
		after := copyImage(before)
		fillPixels(after, plan.BorderLeft, BorderMinChangedPixels+10, 255, 255, 255)
		fillPixels(after, plan.NameRow, 500, 255, 255, 255)
		effect, err := EvaluateGameFrame(before, after, plan)
		if err != nil {
			t.Fatalf("EvaluateGameFrame: %v", err)
		}
		if !effect.Effective {
			t.Fatalf("effect = %+v, want effective", effect)
		}
	})

	t.Run("border luminance only", func(t *testing.T) {
		after := copyImage(before)
		fill(after, plan.BorderLeft, 20, 20, 20)
		effect, err := EvaluateGameFrame(before, after, plan)
		if err != nil {
			t.Fatalf("EvaluateGameFrame: %v", err)
		}
		if effect.BorderLeft.ChangedPixels != 0 {
			t.Fatalf("a 10-level delta must stay below the pixel threshold: %+v", effect.BorderLeft)
		}
		if math.Abs(effect.BorderLeft.LumaDelta) < BorderMinLumaDelta {
			t.Fatalf("luma delta = %v, want >= %v", effect.BorderLeft.LumaDelta, BorderMinLumaDelta)
		}
		if effect.Effective {
			t.Fatalf("effect = %+v, want no effect without the detail change", effect)
		}
	})

	t.Run("detail below MAD", func(t *testing.T) {
		after := copyImage(before)
		fillPixels(after, plan.NameRow, 100, 255, 255, 255)
		effect, err := EvaluateGameFrame(before, after, plan)
		if err != nil {
			t.Fatalf("EvaluateGameFrame: %v", err)
		}
		if effect.Effective {
			t.Fatalf("effect = %+v, want no effect", effect)
		}
		if effect.Detail.MeanAbsDiff >= DetailMADThreshold {
			t.Fatalf("detail MAD = %v, want below %v", effect.Detail.MeanAbsDiff, DetailMADThreshold)
		}
	})
}

func TestClassifyConclusion(t *testing.T) {
	base := func() Outcome {
		return Outcome{
			PrecheckEvaluated: true,
			PrecheckPassed:    true,
			DispatchAttempted: true,
			DispatchState:     windows.StateDelivered,
			EventsPlanned:     3,
			EventsInserted:    3,
			BeforeCaptured:    true,
			PostFrames:        20,
		}
	}
	cases := []struct {
		name   string
		mutate func(*Outcome)
		want   Conclusion
	}{
		{"delivered and effective", func(o *Outcome) { o.AnyEffectiveFrame = true }, ConclusionDeliveredEffective},
		{"delivered no effect", func(o *Outcome) {}, ConclusionDeliveredNoEffect},
		{"preflight refusal", func(o *Outcome) { o.PreflightRefused = true; o.PreflightDetail = "targetForeground" }, ConclusionRefusedByPrecheck},
		{"preflight error", func(o *Outcome) { o.PreflightError = true; o.PreflightDetail = "enumerate" }, ConclusionError},
		{"platform precheck refusal", func(o *Outcome) { o.PrecheckPassed = false; o.PrecheckDetail = "precheck focusLost" }, ConclusionRefusedByPrecheck},
		{"platform precheck error", func(o *Outcome) { o.PrecheckError = true; o.PrecheckDetail = "boom" }, ConclusionError},
		{"no pre-click frame", func(o *Outcome) { o.BeforeCaptured = false }, ConclusionError},
		{"no dispatch", func(o *Outcome) { o.DispatchAttempted = false }, ConclusionError},
		{"blocked", func(o *Outcome) { o.DispatchState = windows.StateBlocked }, ConclusionError},
		{"failed", func(o *Outcome) { o.DispatchState = windows.StateFailed }, ConclusionError},
		{"partial", func(o *Outcome) { o.DispatchState = windows.StatePartialDelivered; o.EventsInserted = 2 }, ConclusionError},
		{"not dispatched", func(o *Outcome) { o.DispatchState = windows.StateNotDispatched }, ConclusionError},
		{"delivered but observation aborted", func(o *Outcome) { o.Aborted = true; o.AbortReason = "foreground changed" }, ConclusionError},
		{"delivered but zero frames", func(o *Outcome) { o.PostFrames = 0 }, ConclusionError},
		{"delivered but planned zero", func(o *Outcome) { o.EventsPlanned = 0 }, ConclusionError},
		{"effective despite later abort", func(o *Outcome) { o.AnyEffectiveFrame = true; o.Aborted = true; o.AbortReason = "foreground changed" }, ConclusionDeliveredEffective},
		{"dry run ready", func(o *Outcome) { o.DryRun = true; o.DispatchAttempted = false }, ConclusionDryRunReady},
		{"dry run aborted", func(o *Outcome) {
			o.DryRun = true
			o.DispatchAttempted = false
			o.Aborted = true
			o.AbortReason = "frame is black"
		}, ConclusionError},
		{"dry run without precheck", func(o *Outcome) { o.DryRun = true; o.DispatchAttempted = false; o.PrecheckEvaluated = false }, ConclusionError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			outcome := base()
			tc.mutate(&outcome)
			got, detail := ClassifyConclusion(outcome)
			if got != tc.want {
				t.Fatalf("ClassifyConclusion = %s (%s), want %s", got, detail, tc.want)
			}
			if detail == "" {
				t.Fatal("conclusion detail must not be empty")
			}
		})
	}
}

func TestConclusionLabels(t *testing.T) {
	if ConclusionDeliveredEffective != "delivered-and-effective" ||
		ConclusionDeliveredNoEffect != "delivered-no-effect" ||
		ConclusionRefusedByPrecheck != "refused-by-precheck" ||
		ConclusionError != "error" {
		t.Fatal("the four required conclusion labels changed")
	}
	if ConclusionDryRunReady != "dry-run-ready" {
		t.Fatal("the dry-run conclusion label changed")
	}
}

func TestSelectNotepad(t *testing.T) {
	notepad := windows.WindowProbe{
		HWND: 10, PID: 100, ImagePath: `C:\Windows\System32\notepad.exe`, Title: "Untitled - Notepad", Visible: true,
	}
	storeNotepad := windows.WindowProbe{
		HWND: 11, PID: 101,
		ImagePath: `C:\Program Files\WindowsApps\Microsoft.WindowsNotepad_11.0.0.0_x64__8wekyb3d8bbwe\Notepad.exe`,
		Title:     "无标题 - 记事本", Visible: true,
	}
	other := windows.WindowProbe{HWND: 12, PID: 102, ImagePath: `C:\Windows\System32\cmd.exe`, Title: "Notepad test", Visible: true}

	t.Run("executable base name", func(t *testing.T) {
		selected, err := SelectNotepad([]windows.WindowProbe{notepad, other}, "")
		if err != nil || selected.HWND != notepad.HWND {
			t.Fatalf("SelectNotepad = %+v, %v", selected, err)
		}
		selected, err = SelectNotepad([]windows.WindowProbe{storeNotepad}, "")
		if err != nil || selected.HWND != storeNotepad.HWND {
			t.Fatalf("SelectNotepad (store path) = %+v, %v", selected, err)
		}
	})

	t.Run("title hint", func(t *testing.T) {
		selected, err := SelectNotepad([]windows.WindowProbe{notepad, other}, "untitled - notepad")
		if err != nil || selected.HWND != notepad.HWND {
			t.Fatalf("SelectNotepad = %+v, %v", selected, err)
		}
		// A title hint is allowed to select a non-Notepad process; the
		// operator chose that criterion.
		selected, err = SelectNotepad([]windows.WindowProbe{notepad, other}, "Notepad test")
		if err != nil || selected.HWND != other.HWND {
			t.Fatalf("SelectNotepad (title on other process) = %+v, %v", selected, err)
		}
	})

	t.Run("no window", func(t *testing.T) {
		_, err := SelectNotepad([]windows.WindowProbe{other}, "")
		var selection *NotepadSelectionError
		if !errors.As(err, &selection) || selection.Kind != "noWindow" {
			t.Fatalf("error = %v, want noWindow", err)
		}
	})

	t.Run("ambiguous", func(t *testing.T) {
		_, err := SelectNotepad([]windows.WindowProbe{notepad, storeNotepad}, "")
		var selection *NotepadSelectionError
		if !errors.As(err, &selection) || selection.Kind != "ambiguous" || len(selection.Candidates) != 2 {
			t.Fatalf("error = %v, want ambiguous with two candidates", err)
		}
	})

	t.Run("hidden second window is still ambiguous", func(t *testing.T) {
		hidden := notepad
		hidden.HWND = 13
		hidden.PID = 103
		hidden.Visible = false
		_, err := SelectNotepad([]windows.WindowProbe{notepad, hidden}, "")
		var selection *NotepadSelectionError
		if !errors.As(err, &selection) || selection.Kind != "ambiguous" {
			t.Fatalf("error = %v, want ambiguous", err)
		}
	})
}

func TestMatchGameTargets(t *testing.T) {
	probes := []windows.WindowProbe{
		{HWND: 1, ImagePath: `C:\Games\ZenlessZoneZero.exe`, Title: "game"},
		{HWND: 2, ImagePath: `C:\Games\Other.exe`, Title: "other"},
		{HWND: 3, ImagePath: `C:\Games\ZenlessZoneZero.exe`, Title: "second"},
	}
	matches := MatchGameTargets(probes, `c:/games/zenlesszonezero.exe`)
	if len(matches) != 2 {
		t.Fatalf("matches = %d, want 2", len(matches))
	}
	if matches := MatchGameTargets(probes, `C:\Games\Missing.exe`); len(matches) != 0 {
		t.Fatalf("matches = %d, want 0", len(matches))
	}
}

func TestParseOptions(t *testing.T) {
	validGame := []string{
		"-target", "game",
		"-exe", `C:\Games\ZenlessZoneZero.exe`,
		"-slot", "2,3",
		"-out", `C:\evidence`,
		"-dry-run",
		"-countdown", "0",
	}
	opts, err := parseOptions(validGame, io.Discard)
	if err != nil {
		t.Fatalf("parseOptions(game): %v", err)
	}
	if opts.target != TargetGame || opts.slotRow != 2 || opts.slotColumn != 3 || !opts.dryRun || opts.countdown != 0 {
		t.Fatalf("opts = %+v", opts)
	}
	if opts.mode() != "dryRun" {
		t.Fatalf("mode = %s", opts.mode())
	}

	opts, err = parseOptions([]string{"-target", "notepad", "-out", "/tmp/out", "-notepad-title", "Untitled - Notepad"}, io.Discard)
	if err != nil {
		t.Fatalf("parseOptions(notepad): %v", err)
	}
	if opts.target != TargetNotepad || opts.notepadTitle != "Untitled - Notepad" || opts.mode() != "click" {
		t.Fatalf("opts = %+v", opts)
	}

	refusals := [][]string{
		{"-out", "/tmp/out"},
		{"-target", "game", "-out", "/tmp/out"},
		{"-target", "game", "-exe", "game.exe", "-slot", "1,1", "-out", "/tmp/out"},
		{"-target", "game", "-exe", `C:\game.exe`, "-out", "/tmp/out"},
		{"-target", "game", "-exe", `C:\game.exe`, "-slot", "0,1", "-out", "/tmp/out"},
		{"-target", "notepad", "-exe", `C:\game.exe`, "-out", "/tmp/out"},
		{"-target", "notepad", "-slot", "1,1", "-out", "/tmp/out"},
		{"-target", "notepad", "-out", "/tmp/out", "-notepad-title", "x", "-countdown", "-1"},
		{"-target", "notepad", "-out", "/tmp/out", "-countdown", "121"},
		{"-target", "game", "-exe", `C:\game.exe`, "-slot", "1,1", "-out", "/tmp/out", "-notepad-title", "x"},
		{"-target", "other", "-out", "/tmp/out"},
		{"-target", "notepad", "-out", "/tmp/out", "extra"},
	}
	for _, args := range refusals {
		if _, err := parseOptions(args, io.Discard); err == nil {
			t.Fatalf("parseOptions(%v) succeeded; want an error", args)
		}
	}

	if _, err := parseOptions([]string{"-h"}, io.Discard); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("-h error = %v, want flag.ErrHelp", err)
	}
}

func TestExitCode(t *testing.T) {
	if exitCode(string(ConclusionDeliveredEffective)) != 0 {
		t.Fatal("delivered-and-effective must exit 0")
	}
	if exitCode(string(ConclusionDryRunReady)) != 0 {
		t.Fatal("dry-run-ready must exit 0")
	}
	for _, conclusion := range []Conclusion{ConclusionDeliveredNoEffect, ConclusionRefusedByPrecheck, ConclusionError, "unknown"} {
		if exitCode(string(conclusion)) != 1 {
			t.Fatalf("%s must exit 1", conclusion)
		}
	}
}

func TestRenderSummaryDoesNotPanicOnEmptyReport(t *testing.T) {
	report := newRunReport(options{target: TargetGame, out: "out", dryRun: true})
	report.Conclusion = string(ConclusionError)
	summary := RenderSummary(report)
	if !strings.Contains(summary, "result: error") {
		t.Fatalf("summary = %q", summary)
	}
	if !strings.Contains(summary, "wininput summary") {
		t.Fatalf("summary = %q", summary)
	}
}

// Helpers.

func geometryForTier(tier Tier, origin windows.PixelPoint) windows.ClientGeometry {
	return windows.ClientGeometry{
		Version: 1,
		Client: windows.PixelRect{
			Min: origin,
			Max: windows.PixelPoint{X: origin.X + tier.Width, Y: origin.Y + tier.Height},
		},
	}
}

func newImage(width, height int, r, g, b uint8) *windows.CapturedImage {
	pixels := make([]byte, width*height*4)
	for i := 0; i < width*height; i++ {
		pixels[4*i] = r
		pixels[4*i+1] = g
		pixels[4*i+2] = b
		pixels[4*i+3] = 0xFF
	}
	return &windows.CapturedImage{Width: width, Height: height, Stride: width * 4, Pixels: pixels}
}

func copyImage(image *windows.CapturedImage) *windows.CapturedImage {
	return &windows.CapturedImage{
		Width:  image.Width,
		Height: image.Height,
		Stride: image.Stride,
		Pixels: append([]byte(nil), image.Pixels...),
	}
}

func setPixel(image *windows.CapturedImage, x, y int, r, g, b uint8) {
	i := y*image.Stride + 4*x
	image.Pixels[i] = r
	image.Pixels[i+1] = g
	image.Pixels[i+2] = b
	image.Pixels[i+3] = 0xFF
}
