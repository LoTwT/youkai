package ui

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestResultsMetricsForTextScale(t *testing.T) {
	cases := []struct {
		scale                              float32
		rowHeight, badgeHeight             float32
		paddingX, gap, fontSize, glyphSize float32
	}{
		{0, 32, 22, 8, 5, 11, 12},
		{1, 32, 22, 8, 5, 11, 12},
		{1.25, 40, 27.5, 10, 6.25, 13.75, 15},
		{1.5, 48, 33, 12, 7.5, 16.5, 18},
		{2, 64, 44, 16, 10, 22, 24},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%g", tc.scale), func(t *testing.T) {
			got := resultsMetricsFor(tc.scale)
			checks := []struct {
				name string
				got  float32
				want float32
			}{
				{"row height", got.RowHeight, tc.rowHeight},
				{"badge height", got.BadgeHeight, tc.badgeHeight},
				{"badge padding", got.BadgePaddingX, tc.paddingX},
				{"badge gap", got.BadgeGap, tc.gap},
				{"badge font", got.BadgeFontSize, tc.fontSize},
				{"badge glyph", got.BadgeGlyphSize, tc.glyphSize},
			}
			for _, check := range checks {
				if absFloat(check.got-check.want) > 0.001 {
					t.Errorf("%s = %v, want %v", check.name, check.got, check.want)
				}
			}
		})
	}
}

// TestResultsBadgesAndRowsFollowTextScale checks the 860 list and the 1440
// split table at 100%, 125%, 150% and 200%: the badge's glyph and label grow,
// each rendered row takes the scaled height with no clipped row, and the
// first-to-last row distance follows the same height.
func TestResultsBadgesAndRowsFollowTextScale(t *testing.T) {
	layouts := []struct {
		name          string
		width, height int
	}{
		{"narrow", 860, 700},
		{"split", 1440, 900},
	}
	scales := []float32{1, 1.25, 1.5, 2}
	type rendered struct{ label, glyph float32 }
	heights := map[string]map[float32]rendered{}

	for _, layout := range layouts {
		heights[layout.name] = map[float32]rendered{}
		for _, scale := range scales {
			t.Run(fmt.Sprintf("%s-%g", layout.name, scale), func(t *testing.T) {
				m, q, tt := openResponsiveResults(t, manyPendingRecord(t, 30), layout.width, layout.height)
				if scale != 1 {
					tt.SetPreferences(ui.Preferences{TextScale: scale})
					settle(t, m, q, tt)
				}
				tt.Frame()
				if absFloat(m.textScale-scale) > 0.001 {
					t.Fatalf("the view recorded text scale %v, want %v", m.textScale, scale)
				}
				metrics := resultsMetricsFor(scale)

				header, ok := tt.Find("序号")
				if !ok {
					t.Fatalf("the table header is missing: %q", tt.Texts())
				}
				if header.H >= metrics.RowHeight {
					t.Fatalf("the header text %.2f fills the %.2f row", header.H, metrics.RowHeight)
				}

				visible := m.visibleResultRows()
				first, last := m.list.Visible()
				if first < 0 || last-first < 2 {
					t.Fatalf("only rows %d..%d are visible", first, last)
				}
				var firstY, lastY float32
				for row := first; row <= last; row++ {
					rect, ok := tt.Find(m.rec.Rows[visible[row]].Label())
					if !ok {
						t.Fatalf("row %d is not rendered", row)
					}
					if absFloat(rect.H-metrics.RowHeight) > 0.01 {
						t.Fatalf("row %d height = %.3f, want %.3f", row, rect.H, metrics.RowHeight)
					}
					if rect.Y < 0 || rect.Y+rect.H > float32(layout.height) {
						t.Fatalf("row %d is clipped by the viewport: %+v", row, rect)
					}
					if row == first {
						firstY = rect.Y
					} else if want := firstY + float32(row-first)*metrics.RowHeight; math.Abs(float64(rect.Y-want)) > 0.01 {
						t.Fatalf("row %d starts at %.2f, want %.2f", row, rect.Y, want)
					}
					lastY = rect.Y
				}
				if got, want := lastY-firstY, float32(last-first)*metrics.RowHeight; math.Abs(float64(got-want)) > 0.01 {
					t.Fatalf("first-to-last row distance = %.2f, want %.2f", got, want)
				}
				if last+1 < len(visible) {
					if rect, ok := tt.Find(m.rec.Rows[visible[last+1]].Label()); ok && rect.H > 0 {
						t.Fatalf("row %d below the viewport is clipped into view: %+v", last+1, rect)
					}
				}

				glyph, ok := tt.Find("◐")
				if !ok {
					t.Fatalf("the pending glyph is missing: %q", tt.Texts())
				}
				label, ok := tt.Find("待校对")
				if !ok {
					t.Fatalf("the pending label is missing: %q", tt.Texts())
				}
				if glyph.X >= label.X || absFloat(glyph.Y-label.Y) > 3*scale {
					t.Fatalf("the badge does not pair its glyph and label: %+v / %+v", glyph, label)
				}
				firstRow, ok := tt.Find(m.rec.Rows[visible[first]].Label())
				if !ok {
					t.Fatalf("the first row is not rendered")
				}
				for name, rect := range map[string]ui.Rect{"glyph": glyph, "label": label} {
					if rect.Y < firstRow.Y || rect.Y+rect.H > firstRow.Y+firstRow.H {
						t.Fatalf("the badge %s is clipped by its row: %+v in %+v", name, rect, firstRow)
					}
				}
				heights[layout.name][scale] = rendered{label: label.H, glyph: glyph.H}
			})
		}

		base := heights[layout.name][1]
		for _, scale := range scales[1:] {
			got := heights[layout.name][scale]
			if got.label <= base.label {
				t.Fatalf("%s: the label height %v at %g is not above the 100%% height %v",
					layout.name, got.label, scale, base.label)
			}
			if got.glyph <= base.glyph {
				t.Fatalf("%s: the glyph height %v at %g is not above the 100%% height %v",
					layout.name, got.glyph, scale, base.glyph)
			}
		}
	}
}

// TestResultsScaledScrollSnapsToRows checks the precise 17-DIP and 26-DIP
// moves at each scaled row height: the offset stays with the input until the
// rest, then lands exactly on a scaled row boundary and shows whole rows.
func TestResultsScaledScrollSnapsToRows(t *testing.T) {
	layouts := []struct {
		name          string
		width, height int
	}{
		{"narrow", 860, 700},
		{"split", 1440, 900},
	}
	for _, layout := range layouts {
		for _, scale := range []float32{1.25, 1.5, 2} {
			t.Run(fmt.Sprintf("%s-%g", layout.name, scale), func(t *testing.T) {
				m, q, tt := openResponsiveResults(t, manyPendingRecord(t, 200), layout.width, layout.height)
				tt.SetPreferences(ui.Preferences{TextScale: scale})
				settle(t, m, q, tt)
				rowHeight := resultsMetricsFor(scale).RowHeight
				rest := func() {
					tt.Advance(resultsSnapDelay + 40*time.Millisecond)
					tt.Frame()
				}

				tt.Scroll(300, 300, 0, 17)
				tt.Frame()
				if got := m.resultsScroll.Y; got != 17 {
					t.Fatalf("the 17-DIP move was snapped mid-input: offset %v", got)
				}
				rest()
				checkScaledScrollOffset(t, m, tt, "after 17 DIP", resultRowOffset(17, rowHeight), rowHeight)

				start := m.resultsScroll.Y
				tt.Scroll(300, 300, 0, 26)
				tt.Frame()
				if got := m.resultsScroll.Y; got != start+26 {
					t.Fatalf("the 26-DIP move was snapped mid-input: offset %v, want %v", got, start+26)
				}
				rest()
				checkScaledScrollOffset(t, m, tt, "after 26 DIP", resultRowOffset(start+26, rowHeight), rowHeight)
			})
		}
	}
}

func checkScaledScrollOffset(t *testing.T, m *Model, tt *ui.Tester, stage string, want, rowHeight float32) {
	t.Helper()
	got := m.resultsScroll.Y
	if math.Abs(float64(got-want)) > 0.01 {
		t.Fatalf("%s: scroll offset = %v, want %v", stage, got, want)
	}
	if math.Mod(float64(got), float64(rowHeight)) != 0 {
		t.Fatalf("%s: the offset %v is not on a %.3f row boundary", stage, got, rowHeight)
	}
	first, last := m.list.Visible()
	if first != int(math.Round(float64(got/rowHeight))) || last < first {
		t.Fatalf("%s: the list shows rows %d..%d at offset %v", stage, first, last, got)
	}
	visible := m.visibleResultRows()
	for row := first; row <= last; row++ {
		rect, ok := tt.Find(m.rec.Rows[visible[row]].Label())
		if !ok {
			t.Fatalf("%s: row %d is not rendered", stage, row)
		}
		if absFloat(rect.H-rowHeight) > 0.01 {
			t.Fatalf("%s: row %d height = %.3f, want %.3f", stage, row, rect.H, rowHeight)
		}
	}
	if last+1 < len(visible) {
		if rect, ok := tt.Find(m.rec.Rows[visible[last+1]].Label()); ok && rect.H > 0 {
			t.Fatalf("%s: row %d below the viewport is clipped into view: %+v", stage, last+1, rect)
		}
	}
}

// TestResultDetailBadgeFollowsTextScale checks the same renderer's badge in
// the 860 detail header at 200%: it still pairs its glyph and label and both
// are larger than the 100% detail header's.
func TestResultDetailBadgeFollowsTextScale(t *testing.T) {
	open := func(scale float32) (*Model, *ui.Tester) {
		m, q, tt := openResponsiveResults(t, manyPendingRecord(t, 20), 860, 700)
		if scale != 1 {
			tt.SetPreferences(ui.Preferences{TextScale: scale})
			settle(t, m, q, tt)
		}
		tt.Frame()
		return m, tt
	}
	_, baseTT := open(1)
	baseLabel, ok := baseTT.Find("待校对")
	if !ok {
		t.Fatalf("the 100%% list badge is missing: %q", baseTT.Texts())
	}

	m, tt := open(2)
	if err := tt.Click("待校对"); err != nil {
		t.Fatalf("click the scaled badge row: %v", err)
	}
	tt.Frame()
	if !m.resultsDetail {
		t.Fatal("clicking the scaled badge did not open the detail page")
	}
	if !tt.HasText("← 结果列表") {
		t.Fatalf("the detail page is missing its way back: %q", tt.Texts())
	}
	glyph, ok := tt.Find("◐")
	if !ok {
		t.Fatalf("the detail header glyph is missing: %q", tt.Texts())
	}
	label, ok := tt.Find("待校对")
	if !ok {
		t.Fatalf("the detail header label is missing: %q", tt.Texts())
	}
	if glyph.X >= label.X || absFloat(glyph.Y-label.Y) > 6 {
		t.Fatalf("the detail header badge does not pair its glyph and label: %+v / %+v", glyph, label)
	}
	if label.H <= baseLabel.H {
		t.Fatalf("the detail header label height %v is not above the 100%% height %v",
			label.H, baseLabel.H)
	}
}
