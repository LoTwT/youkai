package collect

import (
	"image"
	"image/draw"
	"testing"

	"github.com/LoTwT/youkai/internal/platform"
)

// panelFrame builds the minimum frame the matcher needs to enter the
// filter-panel branch: the calibrated image/content size plus both panel
// probes at their open tier. The sort region is caller-controlled.
func panelFrame(t *testing.T, l Layout, sortRegion image.Image) platform.Frame {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, l.Image.X, l.Image.Y))
	for _, p := range l.PanelOpen {
		open, ok := p.Colors["open"]
		if !ok {
			t.Fatalf("panel probe %v has no open tier", p.At)
		}
		img.SetRGBA(p.At.Min.X, p.At.Min.Y, open)
	}
	if sortRegion != nil {
		draw.Draw(img, l.SortLabel, sortRegion, sortRegion.Bounds().Min, draw.Src)
	}
	return platform.Frame{ID: 1, Image: img, Content: l.Content, Geometry: 1}
}

func TestSortLabelMatchesCalibratedPanel(t *testing.T) {
	l := MacCloudLayout()
	m := NewPixelMatcher(l, MacCloudCalibration())
	frame := panelFrame(t, l, l.SortTemplate)
	match := m.Match(frame)
	if match.Page != PageFilterPanel {
		t.Fatalf("page = %s, want filterPanel: %s", match.Page, match.Evidence)
	}
	if match.Panel == nil || !match.Panel.SortVerified || match.Panel.SortLabel != sortLabelRarity {
		t.Fatalf("panel = %+v, want verified %q", match.Panel, sortLabelRarity)
	}
}

func TestSortLabelMismatchIsUnverifiedNotUnknown(t *testing.T) {
	l := MacCloudLayout()
	m := NewPixelMatcher(l, MacCloudCalibration())
	blank := image.NewRGBA(image.Rect(0, 0, l.SortLabel.Dx(), l.SortLabel.Dy()))
	frame := panelFrame(t, l, blank)
	match := m.Match(frame)
	if match.Page != PageFilterPanel {
		t.Fatalf("page = %s, want filterPanel: %s", match.Page, match.Evidence)
	}
	if match.Panel == nil || match.Panel.SortVerified || match.Panel.SortLabel != "" {
		t.Fatalf("panel = %+v, want an unverified sort", match.Panel)
	}
}

func TestLayoutRefusesIncompleteSortCalibration(t *testing.T) {
	base := MacCloudLayout()
	for _, tc := range []struct {
		name   string
		mutate func(*Layout)
	}{
		{"region without template", func(l *Layout) { l.SortTemplate = nil }},
		{"template without region", func(l *Layout) { l.SortLabel = image.Rectangle{} }},
		{"template size mismatch", func(l *Layout) {
			l.SortTemplate = image.NewRGBA(image.Rect(0, 0, l.SortLabel.Dx()-1, l.SortLabel.Dy()))
		}},
		{"region outside content", func(l *Layout) {
			l.SortLabel = image.Rect(1700, 205, 1860, 275)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := base
			tc.mutate(&l)
			if err := l.Validate(); err == nil {
				t.Fatal("Validate accepted an incomplete sort calibration")
			}
		})
	}
}
