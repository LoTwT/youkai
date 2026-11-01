package collect_test

import (
	"bufio"
	"encoding/hex"
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/LoTwT/youkai/internal/game/zzz/collect"
	"github.com/LoTwT/youkai/internal/platform"
)

// The Windows local fixtures are the 2026-11-01 native client captures
// reduced to the pixels the scaled reference layout declares. They pin the
// five verdicts this model must preserve across the three measured scales:
// warehouse page, per-slot occupancy (including the seven empty cells),
// selection border, wheel position and funnel state. The management badge,
// avatar and top-management probes are masked too, but they have no labelled
// Windows sample and are deliberately not asserted here.

const winFixtureDir = "testdata/winlocal"

func loadWinManifest(t *testing.T) []fixtureRow {
	t.Helper()
	f, err := os.Open(filepath.Join(winFixtureDir, "manifest.tsv"))
	if err != nil {
		t.Fatalf("open windows manifest: %v", err)
	}
	defer f.Close()
	var rows []fixtureRow
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) != 5 {
			t.Fatalf("windows manifest row %q has %d columns, want 5", line, len(cols))
		}
		rows = append(rows, fixtureRow{
			name:   cols[0],
			source: cols[1],
			sha256: cols[2],
			mode:   cols[3],
			expect: parseExpect(t, cols[4]),
		})
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("read windows manifest: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("the windows manifest has no fixtures")
	}
	return rows
}

func loadWinMaskedFrame(t *testing.T, name string) platform.Frame {
	t.Helper()
	f, err := os.Open(filepath.Join(winFixtureDir, name+".png"))
	if err != nil {
		t.Fatalf("open windows fixture %s: %v", name, err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatalf("decode windows fixture %s: %v", name, err)
	}
	b := img.Bounds()
	rgba := image.NewRGBA(b)
	draw.Draw(rgba, b, img, b.Min, draw.Src)
	return platform.Frame{
		ID:       1,
		Geometry: 1,
		Image:    rgba,
		Content:  image.Rect(0, 0, b.Dx(), b.Dy()),
	}
}

func windowsMatcher(t *testing.T, frame platform.Frame) (*collect.PixelMatcher, collect.Layout) {
	t.Helper()
	tr, err := collect.ClientRectTransform(frame.Image.Bounds().Size())
	if err != nil {
		t.Fatal(err)
	}
	layout, err := collect.ScaledLayout(collect.ReferenceLayout(), tr)
	if err != nil {
		t.Fatal(err)
	}
	calibration, err := collect.ScaledCalibration(collect.ReferenceCalibration(), tr.Scale)
	if err != nil {
		t.Fatal(err)
	}
	return collect.NewPixelMatcher(layout, calibration), layout
}

func TestWindowsFixturesAgreeWithRecordedExpectations(t *testing.T) {
	for _, row := range loadWinManifest(t) {
		row := row
		t.Run(row.name, func(t *testing.T) {
			frame := loadWinMaskedFrame(t, row.name)
			m, layout := windowsMatcher(t, frame)
			if !layout.MatchesFrame(frame.Image.Bounds().Size(), frame.Content) {
				t.Fatalf("the scaled layout does not identify the fixture's frame: image=%v content=%v layout=%dx%d/%v",
					frame.Image.Bounds().Size(), frame.Content, layout.Image.X, layout.Image.Y, layout.Content)
			}
			pm := m.Match(frame)
			if pm.Page != collect.PageWarehouse {
				t.Fatalf("page %s, want warehouse (%s)", pm.Page, pm.Evidence)
			}
			if pm.List == nil || pm.Wheel == nil || pm.Filter == nil {
				t.Fatalf("warehouse frame lacks list/wheel/filter evidence: %+v", pm)
			}
			if len(pm.List.Occupied) != 20 {
				t.Fatalf("%d occupancy slots, want 20", len(pm.List.Occupied))
			}
			// The measured frame shows 13 items in reading order and seven
			// empty cells (slots 13–19).
			for i, occupied := range pm.List.Occupied {
				if want := i < 13; occupied != want {
					t.Fatalf("slot %d occupied=%v, want %v", i, occupied, want)
				}
			}
			if pm.List.Selected != 0 {
				t.Fatalf("selected %d, want the first visible item (slot 0)", pm.List.Selected)
			}
			if pm.Wheel.Position != 3 {
				t.Fatalf("wheel %d, want position 3", pm.Wheel.Position)
			}
			if pm.Filter.State != collect.FilterActive {
				t.Fatalf("filter %s, want active", pm.Filter.State)
			}
			if pm.Detail == nil || pm.Detail.Quality != collect.QualityS || pm.Detail.Substats != 4 {
				t.Fatalf("detail %+v, want S with 4 substats", pm.Detail)
			}
			// The manifest is the fixture's provenance and expectation record;
			// keep the hard assertions above and the recorded values in sync.
			if want := row.expect["page"]; want != "warehouse" {
				t.Fatalf("manifest page %q, want warehouse", want)
			}
			if want := row.expect["selected"]; want != "0" {
				t.Fatalf("manifest selected %q, want 0", want)
			}
			if want := row.expect["wheel"]; want != "3" {
				t.Fatalf("manifest wheel %q, want 3", want)
			}
			if want := row.expect["filter"]; want != "active" {
				t.Fatalf("manifest filter %q, want active", want)
			}
			if want := row.expect["occupied"]; want != "13" {
				t.Fatalf("manifest occupied %q, want 13", want)
			}
		})
	}
}

// TestWindowsScaledCalibrationSeparatesTheFixtureTiers records the measured
// margins behind the verdict test. Every required count clears its scaled
// threshold by at least 1.5×; the clear cells stay at zero. These margins are
// measured from the fixtures and are not a field acceptance.
func TestWindowsScaledCalibrationSeparatesTheFixtureTiers(t *testing.T) {
	for _, row := range loadWinManifest(t) {
		row := row
		t.Run(row.name, func(t *testing.T) {
			frame := loadWinMaskedFrame(t, row.name)
			m, layout := windowsMatcher(t, frame)
			c := m.C
			img := frame.Image
			selectedHits := selectionHits(img, layout.BorderRect(0))
			for i := 1; i < len(layout.Slots); i++ {
				if hits := selectionHits(img, layout.BorderRect(i)); hits != 0 {
					t.Fatalf("unselected slot %d carries %d selection hits", i, hits)
				}
			}
			if selectedHits < 3*c.SelectionHitMin/2 {
				t.Fatalf("selected border has %d hits, want at least 1.5× the scaled floor %d", selectedHits, c.SelectionHitMin)
			}
			wheelSelected := 0
			for i, center := range layout.Wheel {
				hits := 0
				for _, arc := range layout.WheelArcs {
					hits += selectionHits(img, arc.Add(center))
				}
				if i == 2 {
					wheelSelected = hits
				} else if hits != 0 {
					t.Fatalf("unselected wheel %d carries %d selection hits", i+1, hits)
				}
			}
			if wheelSelected < 3*c.WheelSelectedMin/2 {
				t.Fatalf("selected wheel has %d hits, want at least 1.5× the scaled floor %d", wheelSelected, c.WheelSelectedMin)
			}
			gold := goldPixels(img, layout.QualityBadge)
			if gold < 3*c.QualityGoldMin/2 {
				t.Fatalf("quality badge has %d gold pixels, want at least 1.5× the scaled floor %d", gold, c.QualityGoldMin)
			}
			ink := inkPixels(img, layout.DetailLines[0].Rect, c.InkThreshold)
			if ink < 3*c.DetailPopulatedInk/2 {
				t.Fatalf("name line has %d ink pixels, want at least 1.5× the scaled floor %d", ink, c.DetailPopulatedInk)
			}
			sub4 := inkPixels(img, layout.Substat4Probe, c.InkThreshold)
			if sub4 < 3*c.Substat4InkMin/2 {
				t.Fatalf("fourth substat has %d ink pixels, want at least 1.5× the scaled floor %d", sub4, c.Substat4InkMin)
			}
			for i := 0; i < 13; i++ {
				if v := barMaxChannel(img, layout.BarRect(i)); v < c.BarOccupiedMin {
					t.Fatalf("occupied slot %d bar max %d, want ≥ %d", i, v, c.BarOccupiedMin)
				}
			}
			for i := 13; i < 20; i++ {
				if v := barMaxChannel(img, layout.BarRect(i)); v > c.BarEmptyMax {
					t.Fatalf("empty slot %d bar max %d, want ≤ %d", i, v, c.BarEmptyMax)
				}
			}
			t.Logf("selection=%d/%d wheel=%d/%d gold=%d/%d nameInk=%d/%d sub4Ink=%d/%d",
				selectedHits, c.SelectionHitMin, wheelSelected, c.WheelSelectedMin,
				gold, c.QualityGoldMin, ink, c.DetailPopulatedInk, sub4, c.Substat4InkMin)
		})
	}
}

// TestWindowsPixelMatcherReadsOnlyDeclaredRegions is the structural check
// behind the masked fixtures: randomizing every pixel outside the scaled
// layout's declared regions must not change the verdict.
func TestWindowsPixelMatcherReadsOnlyDeclaredRegions(t *testing.T) {
	for _, row := range loadWinManifest(t) {
		row := row
		t.Run(row.name, func(t *testing.T) {
			frame := loadWinMaskedFrame(t, row.name)
			m, layout := windowsMatcher(t, frame)
			want := m.Match(frame)
			randomizeOutside(frame.Image, layout.ReadRegions())
			got := m.Match(frame)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("Match changed after randomizing outside the declared regions:\n got %+v\nwant %+v", got, want)
			}
		})
	}
}

func TestWindowsManifestProvenance(t *testing.T) {
	rows := loadWinManifest(t)
	if len(rows) != 3 {
		t.Fatalf("%d fixtures, want the 1920, 2560 and 1280 probes", len(rows))
	}
	seen := make(map[string]bool, len(rows))
	roles := make(map[string]int)
	sizes := make(map[image.Point]bool)
	for _, row := range rows {
		if seen[row.name] {
			t.Fatalf("duplicate fixture name %s", row.name)
		}
		seen[row.name] = true
		if row.source == "" || strings.Contains(row.source, "..") {
			t.Fatalf("fixture %s has source %q", row.name, row.source)
		}
		if len(row.sha256) != 64 {
			t.Fatalf("fixture %s has SHA-256 %q", row.name, row.sha256)
		}
		if _, err := hex.DecodeString(row.sha256); err != nil {
			t.Fatalf("fixture %s has a non-hex SHA-256: %v", row.name, err)
		}
		if row.mode != "item" {
			t.Fatalf("fixture %s has mode %q, want item", row.name, row.mode)
		}
		roles[row.expect["role"]]++
		frame := loadWinMaskedFrame(t, row.name)
		sizes[frame.Content.Size()] = true
	}
	if roles["cell"] != 1 || roles["scale"] != 2 {
		t.Fatalf("fixture roles %v, want one reference cell and two scale fixtures", roles)
	}
	for _, size := range []image.Point{image.Pt(1920, 1080), image.Pt(2560, 1440), image.Pt(1280, 720)} {
		if !sizes[size] {
			t.Fatalf("no fixture covers the %v client area", size)
		}
	}
}

func selectionHits(img *image.RGBA, r image.Rectangle) int {
	n := 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			i := img.PixOffset(x, y)
			red, green, blue := int(img.Pix[i]), int(img.Pix[i+1]), int(img.Pix[i+2])
			if green > 170 && blue < 100 && red > 130 {
				n++
			}
		}
	}
	return n
}

func goldPixels(img *image.RGBA, r image.Rectangle) int {
	n := 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			i := img.PixOffset(x, y)
			red, green, blue := int(img.Pix[i]), int(img.Pix[i+1]), int(img.Pix[i+2])
			if red > 150 && green > 110 && blue < 110 && red > blue+60 {
				n++
			}
		}
	}
	return n
}

func inkPixels(img *image.RGBA, r image.Rectangle, threshold int) int {
	n := 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			i := img.PixOffset(x, y)
			if (int(img.Pix[i])+int(img.Pix[i+1])+int(img.Pix[i+2]))/3 > threshold {
				n++
			}
		}
	}
	return n
}

func barMaxChannel(img *image.RGBA, r image.Rectangle) int {
	var sumR, sumG, sumB, n int
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			i := img.PixOffset(x, y)
			sumR += int(img.Pix[i])
			sumG += int(img.Pix[i+1])
			sumB += int(img.Pix[i+2])
			n++
		}
	}
	if n == 0 {
		return 0
	}
	max := sumR / n
	if v := sumG / n; v > max {
		max = v
	}
	if v := sumB / n; v > max {
		max = v
	}
	return max
}

func TestWindowsFixtureNames(t *testing.T) {
	for _, row := range loadWinManifest(t) {
		if !strings.HasPrefix(row.name, "win-") {
			t.Fatalf("fixture %q is not named win-*", row.name)
		}
	}
}
