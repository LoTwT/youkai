package collect_test

import (
	"image"
	"testing"

	"github.com/LoTwT/youkai/internal/game/zzz/collect"
	"github.com/LoTwT/youkai/internal/platform"
)

// Layout validation and the Uncalibrated matcher's empty-value
// semantics — the PR 4c contract: an empty or inconsistent layout is
// refused, never guessed around, and an uncalibrated matcher reports
// every frame undecidable.

// testLayout builds a structurally valid 2×2 layout inside a 32×16
// content region.
func testLayout() collect.Layout {
	slot := func(x, y int) image.Rectangle {
		return image.Rect(x, y, x+8, y+6)
	}
	return collect.Layout{
		Rows:       2,
		Columns:    2,
		Slots:      []image.Rectangle{slot(0, 0), slot(8, 0), slot(0, 6), slot(8, 6)},
		Close:      image.Rect(24, 0, 32, 6),
		ScrollFrom: image.Pt(4, 14),
		ScrollTo:   image.Pt(4, 2),
	}
}

func TestLayoutValidateAcceptsCalibratedShape(t *testing.T) {
	if err := testLayout().Validate(); err != nil {
		t.Fatalf("a calibrated layout must validate: %v", err)
	}
}

func TestLayoutValidateRefusesEmptyAndBroken(t *testing.T) {
	zero := collect.Layout{}
	if err := zero.Validate(); err == nil {
		t.Fatalf("the zero-value layout must be refused: without calibrated anchors there are no honest coordinates")
	}
	cases := []struct {
		name   string
		mutate func(*collect.Layout)
	}{
		{"no grid", func(l *collect.Layout) { l.Rows, l.Columns = 0, 0 }},
		{"slots do not add up", func(l *collect.Layout) { l.Slots = l.Slots[:3] }},
		{"empty slot region", func(l *collect.Layout) { l.Slots[1] = image.Rect(8, 0, 8, 0) }},
		{"empty close region", func(l *collect.Layout) { l.Close = image.Rectangle{} }},
		{"drag goes nowhere", func(l *collect.Layout) { l.ScrollTo = l.ScrollFrom }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := testLayout()
			tc.mutate(&l)
			if err := l.Validate(); err == nil {
				t.Fatalf("the mutated layout must be refused")
			}
		})
	}
}

func TestLayoutFitsContent(t *testing.T) {
	content := image.Rect(0, 0, 32, 16)
	if !testLayout().FitsContent(content) {
		t.Fatalf("a layout inside the content region must fit")
	}
	outside := testLayout()
	outside.Close = image.Rect(24, 0, 40, 6)
	if outside.FitsContent(content) {
		t.Fatalf("a close anchor outside the content region must not fit")
	}
	off := testLayout()
	off.ScrollTo = image.Pt(4, 18)
	if off.FitsContent(content) {
		t.Fatalf("a scroll endpoint outside the content region must not fit")
	}
}

func TestUncalibratedMatcherReportsUndecidable(t *testing.T) {
	m := collect.Uncalibrated{}
	frame := platform.Frame{ID: 7, Geometry: 1}
	pm := m.Match(frame)
	if pm.Page != collect.PageUnknown || pm.List != nil || pm.Detail != nil {
		t.Fatalf("an uncalibrated match must be unknown without evidence: %+v", pm)
	}
	sm := m.MatchScroll(frame, platform.Frame{ID: 8, Geometry: 1})
	if sm.Displaced || len(sm.Matches) != 0 {
		t.Fatalf("an uncalibrated scroll verdict must claim nothing: %+v", sm)
	}
}
