package collect

import (
	"bufio"
	"encoding/hex"
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The 2026-11-02 W3a fixtures are small crops of the Windows local sample
// frames, not full 1920×1080 captures. Each row in manifest.tsv names the
// source frame and its SHA-256, the crop region on the reference canvas, and
// the verdict the sample records. The crop keeps its source coordinates as
// the decoded image's bounds, so the production matcher can read the same
// absolute probes without a full-size fixture.

const w3aFixtureDir = "testdata/winlocal-w3a"

type w3aFixture struct {
	name   string
	source string
	sha256 string
	region image.Rectangle
	expect map[string]string
}

func loadW3AManifest(t *testing.T) []w3aFixture {
	t.Helper()
	f, err := os.Open(filepath.Join(w3aFixtureDir, "manifest.tsv"))
	if err != nil {
		t.Fatalf("open W3a manifest: %v", err)
	}
	defer f.Close()
	var rows []w3aFixture
	sc := bufio.NewScanner(f)
	line := 0
	for sc.Scan() {
		line++
		text := sc.Text()
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		cols := strings.Split(text, "\t")
		if len(cols) != 5 {
			t.Fatalf("W3a manifest line %d %q has %d columns, want 5", line, text, len(cols))
		}
		parts := strings.Split(cols[3], ",")
		if len(parts) != 4 {
			t.Fatalf("W3a manifest line %d region %q is not x0,y0,x1,y1", line, cols[3])
		}
		var v [4]int
		for i, p := range parts {
			n, err := strconv.Atoi(p)
			if err != nil {
				t.Fatalf("W3a manifest line %d region %q: %v", line, cols[3], err)
			}
			v[i] = n
		}
		expect := make(map[string]string)
		for _, item := range strings.Split(cols[4], ";") {
			key, value, ok := strings.Cut(item, "=")
			if !ok || key == "" || value == "" {
				t.Fatalf("W3a manifest line %d expectation %q is not key=value", line, item)
			}
			expect[key] = value
		}
		rows = append(rows, w3aFixture{
			name:   cols[0],
			source: cols[1],
			sha256: cols[2],
			region: image.Rect(v[0], v[1], v[2], v[3]),
			expect: expect,
		})
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("read W3a manifest: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("the W3a manifest has no fixtures")
	}
	return rows
}

// loadW3ACrop decodes a fixture into an RGBA image whose bounds are the
// source crop region. Keeping the source coordinates is what lets the
// production matcher's absolute probes read the small crop unchanged.
func loadW3ACrop(t *testing.T, row w3aFixture) *image.RGBA {
	t.Helper()
	f, err := os.Open(filepath.Join(w3aFixtureDir, row.name+".png"))
	if err != nil {
		t.Fatalf("open W3a fixture %s: %v", row.name, err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatalf("decode W3a fixture %s: %v", row.name, err)
	}
	if got := img.Bounds().Size(); got != row.region.Size() {
		t.Fatalf("W3a fixture %s is %v, want crop size %v", row.name, got, row.region.Size())
	}
	out := image.NewRGBA(row.region)
	draw.Draw(out, row.region, img, img.Bounds().Min, draw.Src)
	return out
}

func w3aRows(t *testing.T, key string) []w3aFixture {
	t.Helper()
	var out []w3aFixture
	for _, row := range loadW3AManifest(t) {
		if _, ok := row.expect[key]; ok {
			out = append(out, row)
		}
	}
	return out
}

func w3aWindowsMatcher(t *testing.T) (*PixelMatcher, Layout) {
	t.Helper()
	profile, err := WindowsLocalProfileFor(image.Pt(1920, 1080))
	if err != nil {
		t.Fatalf("WindowsLocalProfileFor: %v", err)
	}
	return NewPixelMatcher(profile.Layout, profile.Calibration), profile.Layout
}

func w3aSortMAD(img, template *image.RGBA, region image.Rectangle) float64 {
	var sum float64
	for y := 0; y < region.Dy(); y++ {
		for x := 0; x < region.Dx(); x++ {
			d := grayAt(img, region.Min.X+x, region.Min.Y+y) -
				grayAt(template, template.Bounds().Min.X+x, template.Bounds().Min.Y+y)
			if d < 0 {
				d = -d
			}
			sum += float64(d)
		}
	}
	return sum / float64(region.Dx()*region.Dy())
}

// TestWindowsLocalW3ASortFromOnePanelOpen pins the W3a sort evidence against
// the embedded Windows template from one panel opening: frame 0000 matches
// within the calibrated MAD, and the closed-panel frame at the same
// coordinates does not. The three S2 sort crops are byte-identical, so only
// frame 0000 is a fixture; stability across separate openings and after a
// filter change is not covered here.
//
// The only negative sample is the closed panel. Production calls
// sortLabelMatches only when both probes are open, so an open panel with a
// different sort label is untested. A review-time synthetic erase of the
// template text box (1329,154)-(1396,175) to background gray 12 gives MAD
// 16.44, more than 3x the 5.0 limit; that suggests a false match is unlikely
// but is not a recorded frame. The next sample round should add a real
// mismatched-label frame.
func TestWindowsLocalW3ASortFromOnePanelOpen(t *testing.T) {
	m, layout := w3aWindowsMatcher(t)
	if layout.SortTemplate == nil || layout.SortLabel.Empty() {
		t.Fatal("the 1920×1080 layout has no Windows sort template")
	}
	for _, row := range w3aRows(t, "sort") {
		img := loadW3ACrop(t, row)
		got := m.sortLabelMatches(img)
		want := row.expect["sort"] == "match"
		mad := w3aSortMAD(img, layout.SortTemplate, layout.SortLabel)
		if got != want {
			t.Fatalf("%s: sortLabelMatches=%v, want %v (MAD %.2f, limit %.1f)",
				row.name, got, want, mad, m.C.SortLabelMADMax)
		}
		if want && mad > m.C.SortLabelMADMax {
			t.Fatalf("%s: MAD %.2f exceeds the calibrated limit %.1f", row.name, mad, m.C.SortLabelMADMax)
		}
		if !want && mad <= m.C.SortLabelMADMax {
			t.Fatalf("%s: closed-panel MAD %.2f is inside the calibrated limit %.1f", row.name, mad, m.C.SortLabelMADMax)
		}
		t.Logf("%s: MAD %.2f (limit %.1f)", row.name, mad, m.C.SortLabelMADMax)
	}
}

// TestWindowsLocalW3APanelProbes pins both panel probes on the open and
// closed sample frames. The first probe's open tier is the measured Windows
// fill; the second matches the carried cloud value.
func TestWindowsLocalW3APanelProbes(t *testing.T) {
	m, _ := w3aWindowsMatcher(t)
	for _, row := range w3aRows(t, "probe") {
		idx, err := strconv.Atoi(row.expect["probe"])
		if err != nil || idx < 1 || idx > len(m.L.PanelOpen) {
			t.Fatalf("%s: probe index %q is invalid", row.name, row.expect["probe"])
		}
		got := m.classify(loadW3ACrop(t, row), m.L.PanelOpen[idx-1])
		if want := row.expect["panel"]; got != want {
			t.Fatalf("%s: probe %d classified %q, want %q", row.name, idx, got, want)
		}
	}
}

// TestWindowsLocalW3AScrollbarEnds pins the long-list thumb samples: the
// top and bottom frames satisfy their respective end threshold, the mid-drag
// frame satisfies neither, and each run keeps the measured 564-pixel length.
func TestWindowsLocalW3AScrollbarEnds(t *testing.T) {
	m, _ := w3aWindowsMatcher(t)
	for _, row := range w3aRows(t, "thumb") {
		img := loadW3ACrop(t, row)
		start, end, ok := m.thumbSegment(img)
		if !ok {
			t.Fatalf("%s: no bright thumb run", row.name)
		}
		wantStart, wantEnd, ok := parseW3AThumb(row.expect["thumb"])
		if !ok {
			t.Fatalf("%s: invalid thumb expectation %q", row.name, row.expect["thumb"])
		}
		if start != wantStart || end != wantEnd {
			t.Fatalf("%s: thumb run %d–%d, want %d–%d", row.name, start, end, wantStart, wantEnd)
		}
		if end-start+1 != 564 {
			t.Fatalf("%s: thumb length %d, want the measured 564", row.name, end-start+1)
		}
		top, bottom := m.scrollEnds(img)
		if wantTop := row.expect["top"] == "true"; top != wantTop {
			t.Fatalf("%s: top=%v, want %v", row.name, top, wantTop)
		}
		if wantBottom := row.expect["bottom"] == "true"; bottom != wantBottom {
			t.Fatalf("%s: bottom=%v, want %v", row.name, bottom, wantBottom)
		}
	}
}

func parseW3AThumb(text string) (start, end int, ok bool) {
	parts := strings.Split(text, "-")
	if len(parts) != 2 {
		return 0, 0, false
	}
	start, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, false
	}
	end, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, false
	}
	return start, end, true
}

// TestWindowsLocalW3ASortVerifiesCoverage exercises the coverage conclusion
// with the Windows template present: one matching panel makes the observed
// sort verified, and six complete positions then make coverage complete. This
// is the logic change W3a enables; it is still not field acceptance.
func TestWindowsLocalW3ASortVerifiesCoverage(t *testing.T) {
	m, layout := w3aWindowsMatcher(t)
	match := m.Match(panelFrame(t, layout, layout.SortTemplate))
	if match.Page != PageFilterPanel || match.Panel == nil || !match.Panel.SortVerified {
		t.Fatalf("panel match = %+v, want a verified filter panel", match)
	}
	inv := &Inventory{}
	inv.scope = scopePayload{Sort: scopeSortUnverified, Coverage: scopeCoverageUnknown}
	inv.observeSort(match.Panel)
	if got := scopeOf(t, inv); got.Sort != scopeSortVerified || got.SortLabel != sortLabelRarity {
		t.Fatalf("after the Windows panel match scope = %+v, want verified %q", got, sortLabelRarity)
	}
	completePositions(inv)
	if got := scopeOf(t, inv); got.Coverage != scopeCoverageComplete {
		t.Fatalf("after six complete positions and a verified Windows sort, coverage = %q", got.Coverage)
	}
}

// TestWindowsLocalW3AScaledTierDropsSortTemplate keeps the existing rule:
// only the 1:1 1920×1080 reference layout declares the native template. A
// scaled tier drops the claim instead of shrinking the template.
func TestWindowsLocalW3AScaledTierDropsSortTemplate(t *testing.T) {
	ref := ReferenceLayout()
	if ref.SortLabel.Empty() || ref.SortTemplate == nil {
		t.Fatal("ReferenceLayout does not declare the W3a Windows sort template")
	}
	tr, err := ClientRectTransform(image.Pt(2560, 1440))
	if err != nil {
		t.Fatal(err)
	}
	got, err := ScaledLayout(ref, tr)
	if err != nil {
		t.Fatal(err)
	}
	if !got.SortLabel.Empty() || got.SortTemplate != nil {
		t.Fatalf("scaled tier kept sort fields: label=%v template=%v", got.SortLabel, got.SortTemplate)
	}
}

// TestWindowsLocalW3AManifestProvenance checks the fixture metadata: every
// row names a source frame and its full-frame SHA-256, and every crop lies
// inside the 1920×1080 reference canvas.
func TestWindowsLocalW3AManifestProvenance(t *testing.T) {
	rows := loadW3AManifest(t)
	if len(rows) != 9 {
		t.Fatalf("%d W3a fixtures, want 9", len(rows))
	}
	seen := make(map[string]bool, len(rows))
	for _, row := range rows {
		if seen[row.name] {
			t.Fatalf("duplicate W3a fixture name %s", row.name)
		}
		seen[row.name] = true
		if !strings.HasPrefix(row.source, "S1/") && !strings.HasPrefix(row.source, "S2/") && !strings.HasPrefix(row.source, "S3/") {
			t.Fatalf("%s: source %q is not an S1/S2/S3 frame", row.name, row.source)
		}
		if strings.Contains(row.source, "..") {
			t.Fatalf("%s: source %q escapes the evidence directory", row.name, row.source)
		}
		if len(row.sha256) != 64 {
			t.Fatalf("%s: SHA-256 %q is not 64 hex characters", row.name, row.sha256)
		}
		if _, err := hex.DecodeString(row.sha256); err != nil {
			t.Fatalf("%s: SHA-256 is not hex: %v", row.name, err)
		}
		if row.region.Empty() || !row.region.In(image.Rect(0, 0, ReferenceCanvasWidth, ReferenceCanvasHeight)) {
			t.Fatalf("%s: crop %v is outside the reference canvas", row.name, row.region)
		}
	}
}
