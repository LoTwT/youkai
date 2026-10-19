package collect_test

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/LoTwT/youkai/internal/game/zzz/collect"
	"github.com/LoTwT/youkai/internal/platform"
)

// The masked fixtures are the recorded macOS cloud sample frames reduced
// to the pixels the matcher declares it reads. They lock the calibrated
// verdicts, the scroll correspondences and the detail comparison bands to
// the field evidence; the full-frame replay in TestReplayFieldSamples
// checks the same matcher on the unmasked sources when the sample
// directory is present.

const fixtureDir = "testdata/maccloud"

type fixtureRow struct {
	name   string
	source string
	sha256 string
	mode   string
	expect map[string]string
}

func loadManifest(t *testing.T) []fixtureRow {
	t.Helper()
	f, err := os.Open(filepath.Join(fixtureDir, "manifest.tsv"))
	if err != nil {
		t.Fatalf("open manifest: %v", err)
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
			t.Fatalf("manifest row %q has %d columns, want 5", line, len(cols))
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
		t.Fatalf("read manifest: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("the manifest has no fixtures")
	}
	return rows
}

func parseExpect(t *testing.T, s string) map[string]string {
	t.Helper()
	out := make(map[string]string)
	for _, field := range strings.Split(s, ";") {
		if field == "" {
			continue
		}
		k, v, ok := strings.Cut(field, "=")
		if !ok {
			t.Fatalf("expectation %q is not key=value", field)
		}
		out[k] = v
	}
	return out
}

func loadMaskedFrame(t *testing.T, name string) platform.Frame {
	t.Helper()
	f, err := os.Open(filepath.Join(fixtureDir, name+".png"))
	if err != nil {
		t.Fatalf("open fixture %s: %v", name, err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatalf("decode fixture %s: %v", name, err)
	}
	b := img.Bounds()
	rgba := image.NewRGBA(b)
	draw.Draw(rgba, b, img, b.Min, draw.Src)
	return platform.Frame{
		ID:       1,
		Geometry: 1,
		Image:    rgba,
		Content:  collect.MacCloudLayout().Content,
	}
}

func macCloudMatcher() *collect.PixelMatcher {
	return collect.NewPixelMatcher(collect.MacCloudLayout(), collect.MacCloudCalibration())
}

func TestMacCloudFixturesAgreeWithRecordedExpectations(t *testing.T) {
	m := macCloudMatcher()
	for _, row := range loadManifest(t) {
		row := row
		t.Run(row.name, func(t *testing.T) {
			assertMatchExpectations(t, m, loadMaskedFrame(t, row.name), row.expect)
		})
	}
}

// assertMatchExpectations checks one frame's page evidence against the
// fixture table's recorded expectations.
func assertMatchExpectations(t *testing.T, m *collect.PixelMatcher, f platform.Frame, expect map[string]string) {
	t.Helper()
	pm := m.Match(f)
	page := PageName(pm.Page)
	if want := expect["page"]; want != "" && page != want {
		t.Fatalf("page %s, want %s (%s)", page, want, pm.Evidence)
	}
	if pm.Page == collect.PageFilterPanel {
		if pm.List != nil {
			t.Fatalf("a panel frame carries list evidence: %+v", pm.List)
		}
		return
	}
	if pm.Page != collect.PageWarehouse {
		t.Fatalf("page %s, want warehouse (%s)", pm.Page, pm.Evidence)
	}
	if pm.List == nil || pm.Wheel == nil || pm.Filter == nil {
		t.Fatalf("warehouse frame lacks list/wheel/filter evidence: %+v", pm)
	}
	if got, want := len(pm.List.Occupied), len(collect.MacCloudLayout().Slots); got != want {
		t.Fatalf("occupancy has %d slots, want %d", got, want)
	}
	if want, ok := expect["selected"]; ok {
		if got := fmt.Sprint(pm.List.Selected); got != want {
			t.Fatalf("selected %s, want %s (%s)", got, want, pm.Evidence)
		}
	}
	if want, ok := expect["wheel"]; ok {
		if got := fmt.Sprint(pm.Wheel.Position); got != want {
			t.Fatalf("wheel %s, want %s (%s)", got, want, pm.Evidence)
		}
	}
	if want, ok := expect["filter"]; ok {
		if got := FilterName(pm.Filter.State); got != want {
			t.Fatalf("filter %s, want %s (%s)", got, want, pm.Evidence)
		}
	}
	if want, ok := expect["top"]; ok {
		if got := fmt.Sprint(pm.List.Top); got != want {
			t.Fatalf("top %s, want %s (%s)", got, want, pm.Evidence)
		}
	}
	if want, ok := expect["bottom"]; ok {
		if got := fmt.Sprint(pm.List.Bottom); got != want {
			t.Fatalf("bottom %s, want %s (%s)", got, want, pm.Evidence)
		}
	}
	if want, ok := expect["occupied"]; ok {
		n := 0
		for _, occupied := range pm.List.Occupied {
			if occupied {
				n++
			}
		}
		if got := fmt.Sprint(n); got != want {
			t.Fatalf("occupied %s, want %s (%s)", got, want, pm.Evidence)
		}
	}
	if want, ok := expect["quality"]; ok {
		if pm.Detail == nil {
			t.Fatalf("quality %s expected but the detail is not populated", want)
		}
		if got := QualityName(pm.Detail.Quality); got != want {
			t.Fatalf("quality %s, want %s (%s)", got, want, pm.Evidence)
		}
	}
	if want, ok := expect["substats"]; ok {
		if pm.Detail == nil {
			t.Fatalf("substats %s expected but the detail is not populated", want)
		}
		if got := fmt.Sprint(pm.Detail.Substats); got != want {
			t.Fatalf("substats %s, want %s (%s)", got, want, pm.Evidence)
		}
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func PageName(p collect.PageKind) string {
	switch p {
	case collect.PageWarehouse:
		return "warehouse"
	case collect.PageFilterPanel:
		return "filterPanel"
	default:
		return "unknown"
	}
}

func FilterName(s collect.FilterState) string {
	switch s {
	case collect.FilterClear:
		return "clear"
	case collect.FilterActive:
		return "active"
	default:
		return "unknown"
	}
}

func QualityName(q collect.Quality) string {
	switch q {
	case collect.QualityS:
		return "S"
	case collect.QualityNotS:
		return "notS"
	default:
		return "unknown"
	}
}

type scrollCase struct {
	from, to string
	kind     string // stationary, displaced, lost
	rows     int
}

var scrollCases = []scrollCase{
	{"s2-21-totop-3", "s2-21-totop-4", "stationary", 0},
	{"s2-21-totop-2", "s2-21-totop-3", "displaced", 3},
	{"s2-22-s1-step-1", "s2-22-s1-step-2", "displaced", 4},
	{"s2-22-s1-step-2", "s2-22-s1-step-3", "displaced", 4},
	{"s2-22-s1-step-3", "s2-22-s1-step-4", "displaced", 4},
	{"s2-22-s1-step-4", "s2-22-s1-step-5", "displaced", 3},
	{"s2-22-s1-step-5", "s2-22-s1-step-6", "stationary", 0},
	// One recorded pair is off-pitch (the two frames differ at zero
	// displacement and no whole-row shift explains it); it must be Lost,
	// never rounded into a displacement.
	{"s2-21-totop-1", "s2-21-totop-2", "lost", 0},
}

func TestMacCloudScrollDisplacements(t *testing.T) {
	m := macCloudMatcher()
	for _, tc := range scrollCases {
		tc := tc
		t.Run(tc.from+"->"+tc.to, func(t *testing.T) {
			assertScrollCase(t, m, loadMaskedFrame(t, tc.from), loadMaskedFrame(t, tc.to), tc)
		})
	}
	if sm := m.MatchScroll(loadMaskedFrame(t, "s2-22-s1-step-1"), loadMaskedFrame(t, "s2-22-s1-step-1")); !sm.Stationary {
		t.Fatalf("a frame compared with itself must be stationary: %+v", sm)
	}
}

func assertScrollCase(t *testing.T, m *collect.PixelMatcher, before, after platform.Frame, tc scrollCase) {
	t.Helper()
	sm := m.MatchScroll(before, after)
	switch tc.kind {
	case "stationary":
		if !sm.Stationary || sm.Displaced || sm.Lost {
			t.Fatalf("verdict %+v, want stationary (%s)", sm, sm.Evidence)
		}
	case "lost":
		if !sm.Lost || sm.Displaced || sm.Stationary {
			t.Fatalf("verdict %+v, want lost (%s)", sm, sm.Evidence)
		}
	case "displaced":
		if !sm.Displaced || sm.Rows != tc.rows || sm.Stationary || sm.Lost {
			t.Fatalf("verdict %+v, want displaced %d rows (%s)", sm, tc.rows, sm.Evidence)
		}
		wantMatches := (collect.MacCloudLayout().Rows - tc.rows) * collect.MacCloudLayout().Columns
		if len(sm.Matches) != wantMatches {
			t.Fatalf("%d correspondences, want %d (%s)", len(sm.Matches), wantMatches, sm.Evidence)
		}
		for _, p := range sm.Matches {
			if p.FromColumn != p.ToColumn || abs(p.FromRow-p.ToRow) != tc.rows {
				t.Fatalf("correspondence %+v does not preserve the column or the %d-row displacement", p, tc.rows)
			}
		}
	}
}

type detailCase struct {
	name string
	a, b string
	want collect.DetailComparison
}

var detailCases = []detailCase{
	// Same content under a different navigation step (s1 bottom cell vs
	// d1 discarded) — measured same-content pairs top out at 1.01.
	{"same content", "s2-d1-discarded-r0c1", "s2-s1-bot-cell-r4c2", collect.DetailSame},
	// Adjacent discs with different fields — 183 of the 186 measured
	// adjacent pairs are above 4.0; two known near-duplicates are below
	// 2.0 (0.42, 0.62) and one is in the band (2.38).
	{"different content", "s2-d1-discarded-r0c1", "s2-d1-discarded-r0c2", collect.DetailDifferent},
	{"adjacent wheel rows", "s2-wL-top-cell-r0c0", "s2-wL-top-cell-r0c1", collect.DetailDifferent},
	// The known near-duplicate pair (0.62): Same is the honest answer at
	// pixel resolution, and the transition rule still needs another piece
	// of evidence (a unique 1:1 neighbor round trip, not yet implemented).
	{"near duplicate", "s2-s6-top-cell-r4c0", "s2-s6-top-cell-r4c1", collect.DetailSame},
	// The known 3.72 pair is diagonal (non-adjacent) and sits between the
	// bands: undecided, not rounded into either answer.
	{"between the bands", "s2-s6-top-cell-r3c3", "s2-s6-top-cell-r4c0", collect.DetailUndecided},
}

func TestMacCloudDetailComparison(t *testing.T) {
	m := macCloudMatcher()
	for _, tc := range detailCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := m.CompareDetail(loadMaskedFrame(t, tc.a), loadMaskedFrame(t, tc.b))
			if got != tc.want {
				t.Fatalf("comparison %v, want %v", got, tc.want)
			}
		})
	}
	if got := m.CompareDetail(loadMaskedFrame(t, "s2-d1-discarded-r0c1"), loadMaskedFrame(t, "s2-d1-discarded-r0c1")); got != collect.DetailSame {
		t.Fatalf("a frame compared with itself is %v, want Same", got)
	}
}

// TestPixelMatcherReadsOnlyDeclaredRegions randomizes every pixel outside
// ReadRegions and requires identical verdicts. It is the structural check
// behind the masked fixtures: a probe that reads an undeclared pixel would
// change its answer here.
func TestPixelMatcherReadsOnlyDeclaredRegions(t *testing.T) {
	l := collect.MacCloudLayout()
	m := collect.NewPixelMatcher(l, collect.MacCloudCalibration())
	regions := l.ReadRegions()

	a := loadMaskedFrame(t, "s2-d1-discarded-r0c1")
	b := loadMaskedFrame(t, "s2-d1-discarded-r0c2")
	wantA := m.Match(a)
	wantDetail := m.CompareDetail(a, b)

	scrollBefore := loadMaskedFrame(t, "s2-22-s1-step-1")
	scrollAfter := loadMaskedFrame(t, "s2-22-s1-step-2")
	wantScroll := m.MatchScroll(scrollBefore, scrollAfter)

	for _, f := range []platform.Frame{a, b, scrollBefore, scrollAfter} {
		randomizeOutside(f.Image, regions)
	}

	if got := m.Match(a); !reflect.DeepEqual(got, wantA) {
		t.Fatalf("Match changed after randomizing outside the declared regions:\n got %+v\nwant %+v", got, wantA)
	}
	if got := m.CompareDetail(a, b); got != wantDetail {
		t.Fatalf("CompareDetail %v after randomizing outside the declared regions, want %v", got, wantDetail)
	}
	if got := m.MatchScroll(scrollBefore, scrollAfter); !reflect.DeepEqual(got, wantScroll) {
		t.Fatalf("MatchScroll changed after randomizing outside the declared regions:\n got %+v\nwant %+v", got, wantScroll)
	}
}

func randomizeOutside(img *image.RGBA, regions []image.Rectangle) {
	if img == nil {
		return
	}
	b := img.Bounds()
	masked := make([]bool, b.Dx()*b.Dy())
	for _, r := range regions {
		r = r.Intersect(b)
		for y := r.Min.Y; y < r.Max.Y; y++ {
			row := (y - b.Min.Y) * b.Dx()
			for x := r.Min.X; x < r.Max.X; x++ {
				masked[row+x-b.Min.X] = true
			}
		}
	}
	rng := rand.New(rand.NewSource(20261019))
	for y := b.Min.Y; y < b.Max.Y; y++ {
		row := (y - b.Min.Y) * b.Dx()
		for x := b.Min.X; x < b.Max.X; x++ {
			if masked[row+x-b.Min.X] {
				continue
			}
			i := img.PixOffset(x, y)
			img.Pix[i] = uint8(rng.Intn(256))
			img.Pix[i+1] = uint8(rng.Intn(256))
			img.Pix[i+2] = uint8(rng.Intn(256))
			img.Pix[i+3] = 0xFF
		}
	}
}

func TestMacCloudManifestProvenance(t *testing.T) {
	rows := loadManifest(t)
	seen := make(map[string]bool, len(rows))
	roles := make(map[string]int)
	wheels := make(map[string]bool)
	for _, row := range rows {
		if seen[row.name] {
			t.Fatalf("duplicate fixture name %s", row.name)
		}
		seen[row.name] = true
		if row.source == "" || !strings.HasPrefix(row.source, row.name+"/") {
			t.Fatalf("fixture %s has source %q", row.name, row.source)
		}
		if len(row.sha256) != 64 {
			t.Fatalf("fixture %s has SHA-256 %q", row.name, row.sha256)
		}
		if _, err := hex.DecodeString(row.sha256); err != nil {
			t.Fatalf("fixture %s has a non-hex SHA-256: %v", row.name, err)
		}
		if row.mode != "item" && row.mode != "scroll" {
			t.Fatalf("fixture %s has mode %q", row.name, row.mode)
		}
		roles[row.expect["role"]]++
		if w := row.expect["wheel"]; w != "" {
			wheels[w] = true
		}
		if len(row.expect) == 0 {
			t.Fatalf("fixture %s has no expectations", row.name)
		}
	}
	if len(rows) != 69 {
		t.Fatalf("%d fixtures, want 69 (27 items + panels/filters + wheel + scroll + detail extras + wheel-L cells)", len(rows))
	}
	if roles["item"] != 31 || roles["cell"] != 18 || roles["filter"] != 2 || roles["panel"] != 3 || roles["wheel"] != 5 || roles["scroll"] != 10 {
		t.Fatalf("fixture roles %v do not cover the planned sample groups", roles)
	}
	for pos := 1; pos <= 6; pos++ {
		if !wheels[fmt.Sprint(pos)] {
			t.Fatalf("wheel position %d is not covered by any fixture", pos)
		}
	}
}
