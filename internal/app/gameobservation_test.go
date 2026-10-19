package app

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/LoTwT/youkai/internal/game/zzz/collect"
	"github.com/LoTwT/youkai/internal/platform"
	"github.com/LoTwT/youkai/internal/platform/platformfake"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/LoTwT/youkai/internal/scan"
)

const (
	maccloudFixturesDir   = "../game/zzz/collect/testdata/maccloud"
	recognitionFixtures   = "../recognition/testdata/macos-cloud"
	maccloudFixtureDetail = 16 // tools/zzzframemask's detail quantization step
)

// recognitionCrop is one expected text line from the recognition fixture.
type recognitionCrop struct {
	file string
	rect image.Rectangle
}

// loadRecognitionCrops maps a source frame's SHA-256 to its recognized
// line crops by role, using the recognition fixture's lines.tsv.
func loadRecognitionCrops(t *testing.T) map[string]map[string]recognitionCrop {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(recognitionFixtures, "lines.tsv"))
	if err != nil {
		t.Fatalf("read recognition lines: %v", err)
	}
	out := make(map[string]map[string]recognitionCrop)
	for i, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 7 {
			t.Fatalf("recognition lines row %d has %d fields, want 7", i+1, len(fields))
		}
		nums := make([]int, 4)
		for j := range nums {
			nums[j], err = strconv.Atoi(fields[2+j])
			if err != nil {
				t.Fatalf("recognition lines row %d: %v", i+1, err)
			}
		}
		base := strings.TrimSuffix(fields[0], ".png")
		cut := strings.LastIndex(base, "_")
		if cut < 0 {
			t.Fatalf("recognition file %q has no role suffix", fields[0])
		}
		role := base[cut+1:]
		if out[fields[1]] == nil {
			out[fields[1]] = make(map[string]recognitionCrop)
		}
		out[fields[1]][role] = recognitionCrop{
			file: fields[0],
			rect: image.Rect(nums[0], nums[1], nums[2], nums[3]),
		}
	}
	if len(out) == 0 {
		t.Fatal("recognition lines.tsv has no rows")
	}
	return out
}

// loadFixtureFrame decodes one masked fixture frame.
func loadFixtureFrame(t *testing.T, name string) *image.RGBA {
	t.Helper()
	f, err := os.Open(filepath.Join(maccloudFixturesDir, name+".png"))
	if err != nil {
		t.Fatalf("open fixture %s: %v", name, err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatalf("decode fixture %s: %v", name, err)
	}
	rgba, ok := img.(*image.RGBA)
	if !ok {
		t.Fatalf("fixture %s decodes to %T, want *image.RGBA", name, img)
	}
	return rgba
}

// manifestRow is one masked-fixture record.
type manifestRow struct {
	name     string
	sha      string
	substats int
}

// loadFixtureManifest reads the fixture manifest and returns only frames
// that also appear in the recognition fixture set.
func loadFixtureManifest(t *testing.T, known map[string]map[string]recognitionCrop) []manifestRow {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(maccloudFixturesDir, "manifest.tsv"))
	if err != nil {
		t.Fatalf("read fixture manifest: %v", err)
	}
	var out []manifestRow
	for i, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 5 {
			t.Fatalf("manifest row %d has %d fields, want 5", i+1, len(fields))
		}
		if known[fields[2]] == nil {
			continue
		}
		substats := 0
		for _, token := range strings.Split(fields[4], ";") {
			if strings.HasPrefix(token, "substats=") {
				substats, err = strconv.Atoi(strings.TrimPrefix(token, "substats="))
				if err != nil {
					t.Fatalf("manifest row %d: %v", i+1, err)
				}
			}
		}
		if substats != 3 && substats != 4 {
			t.Fatalf("manifest row %d has substats %d, want 3 or 4", i+1, substats)
		}
		out = append(out, manifestRow{name: fields[0], sha: fields[2], substats: substats})
	}
	if len(out) != 27 {
		t.Fatalf("%d masked fixtures match the recognition set, want 27", len(out))
	}
	return out
}

// T41: the 27 masked inventory frames project onto exactly the same line
// regions as the recognition fixture. The masked frames are quantized (the
// repository fixture's documented 16-step detail quantization), so the
// comparison is pixel-exact against the quantized recognition crop; that
// still catches any off-by-one region, wrong pixel or wrong line count.
func TestProjectDiscObservationMatchesFixtureCrops(t *testing.T) {
	known := loadRecognitionCrops(t)
	rows := loadFixtureManifest(t, known)
	layout := collect.MacCloudLayout()

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			img := loadFixtureFrame(t, row.name)
			obs := collect.Observation{
				Image:    img,
				Content:  layout.Content,
				FrameID:  7,
				Position: 1,
				Quality:  collect.QualityS,
				Lines:    layoutDetailLines(layout, row.substats),
				Context:  layoutContextLines(layout),
			}
			projected, err := ProjectDiscObservation(obs)
			if err != nil {
				t.Fatalf("project: %v", err)
			}
			got, ok := projected.(Observation)
			if !ok {
				t.Fatalf("projection returned %T, want app.Observation", projected)
			}
			wantLines := 6
			if row.substats == 4 {
				wantLines = 7
			}
			if len(got.Lines) != wantLines {
				t.Fatalf("projected %d lines, want %d", len(got.Lines), wantLines)
			}
			if len(got.Context) != 2 {
				t.Fatalf("projected %d context crops, want 2", len(got.Context))
			}
			expected := known[row.sha]
			for _, crop := range got.Lines {
				spec, ok := expected[crop.Role]
				if !ok {
					t.Fatalf("no recognition crop for role %q", crop.Role)
				}
				assertQuantizedCrop(t, layout, crop, spec)
			}
			if row.substats == 3 {
				if _, ok := expected[collect.LineSub4]; ok {
					t.Fatalf("fixture reports three substats but the recognition set has a sub4 crop")
				}
			}
			for _, crop := range got.Context {
				if crop.Image == nil || crop.Image.Bounds().Empty() {
					t.Fatalf("context crop %q is empty", crop.Role)
				}
			}
			if !json.Valid(got.Capture) {
				t.Fatalf("capture is not valid JSON: %s", got.Capture)
			}
		})
	}
}

// layoutDetailLines builds the observation's line list from the layout,
// skipping sub4 for a three-substat disc.
func layoutDetailLines(layout collect.Layout, substats int) []collect.LineRegion {
	out := make([]collect.LineRegion, 0, len(layout.DetailLines))
	for _, line := range layout.DetailLines {
		if line.Role == collect.LineSub4 && substats < 4 {
			continue
		}
		out = append(out, line)
	}
	return out
}

// layoutContextLines builds the two admission-context regions.
func layoutContextLines(layout collect.Layout) []collect.LineRegion {
	return []collect.LineRegion{
		{Role: collect.LineQualityBadge, Rect: layout.QualityBadge},
		{Role: collect.LineSelectedCell, Rect: layout.BorderRect(0)},
	}
}

// assertQuantizedCrop compares one projected crop with the recognition
// fixture under the masked fixture's documented detail quantization.
func assertQuantizedCrop(t *testing.T, layout collect.Layout, crop LineCrop, spec recognitionCrop) {
	t.Helper()
	if crop.Source != fmt.Sprintf("frame 7 rect %d,%d,%d,%d",
		spec.rect.Min.X, spec.rect.Min.Y, spec.rect.Max.X, spec.rect.Max.Y) {
		t.Fatalf("crop %q source = %q", crop.Role, crop.Source)
	}
	if crop.Image == nil {
		t.Fatalf("crop %q has no image", crop.Role)
	}
	if crop.Image.Bounds().Size() != spec.rect.Size() {
		t.Fatalf("crop %q is %v, want %v", crop.Role, crop.Image.Bounds().Size(), spec.rect.Size())
	}
	f, err := os.Open(filepath.Join(recognitionFixtures, spec.file))
	if err != nil {
		t.Fatalf("open recognition crop %s: %v", spec.file, err)
	}
	defer f.Close()
	expected, err := png.Decode(f)
	if err != nil {
		t.Fatalf("decode recognition crop %s: %v", spec.file, err)
	}
	if expected.Bounds().Size() != spec.rect.Size() {
		t.Fatalf("recognition crop %s is %v, want %v", spec.file, expected.Bounds().Size(), spec.rect.Size())
	}
	for y := 0; y < spec.rect.Dy(); y++ {
		for x := 0; x < spec.rect.Dx(); x++ {
			gr, gg, gb, _ := crop.Image.At(x, y).RGBA()
			er, eg, eb, _ := expected.At(x, y).RGBA()
			// The masked fixture quantizes each detail channel with the
			// documented step, except the exact probes the mask tool keeps
			// lossless (the fourth-substat value column lies inside the
			// sub4 line region). Compare against the fixture's own rule.
			exact := image.Pt(spec.rect.Min.X+x, spec.rect.Min.Y+y).In(layout.Substat4Probe) ||
				image.Pt(spec.rect.Min.X+x, spec.rect.Min.Y+y).In(layout.QualityBadge)
			want := [3]uint32{er >> 8, eg >> 8, eb >> 8}
			if !exact {
				want = [3]uint32{
					(er >> 8) / maccloudFixtureDetail * maccloudFixtureDetail,
					(eg >> 8) / maccloudFixtureDetail * maccloudFixtureDetail,
					(eb >> 8) / maccloudFixtureDetail * maccloudFixtureDetail,
				}
			}
			got := [3]uint32{gr >> 8, gg >> 8, gb >> 8}
			if got != want {
				t.Fatalf("crop %q pixel (%d,%d) = %v, want quantized %v (recognition %v)",
					crop.Role, x, y, got, want, [3]uint32{er >> 8, eg >> 8, eb >> 8})
			}
		}
	}
}

// T42: the capture JSON carries the traversal's fields and no pixels.
func TestProjectDiscObservationCaptureJSON(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 20, 20))
	obs := collect.Observation{
		Image:    img,
		Content:  image.Rect(0, 0, 20, 20),
		FrameID:  42,
		Position: 3,
		Quality:  collect.QualityS,
		Lines:    []collect.LineRegion{{Role: "name", Rect: image.Rect(0, 0, 4, 3)}},
		Context:  []collect.LineRegion{{Role: "qualityBadge", Rect: image.Rect(4, 4, 6, 6)}},
	}
	projected, err := ProjectDiscObservation(obs)
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	got := projected.(Observation)
	var capture map[string]any
	if err := json.Unmarshal(got.Capture, &capture); err != nil {
		t.Fatalf("capture is not a JSON object: %v", err)
	}
	for _, key := range []string{"position", "quality", "lines", "context"} {
		if _, ok := capture[key]; !ok {
			t.Fatalf("capture lacks %q: %s", key, got.Capture)
		}
	}
	if _, ok := capture["Image"]; ok {
		t.Fatalf("capture serializes the frame pixels: %s", got.Capture)
	}
	if strings.Contains(string(got.Capture), "iVBOR") || strings.Contains(string(got.Capture), "Pix") {
		t.Fatalf("capture contains pixel data: %s", got.Capture)
	}
	if capture["position"].(float64) != 3 || capture["quality"].(string) != "S" {
		t.Fatalf("capture fields = %v", capture)
	}
	if lines := capture["lines"].([]any); len(lines) != 1 {
		t.Fatalf("capture lines = %v", lines)
	}
	if context := capture["context"].([]any); len(context) != 1 {
		t.Fatalf("capture context = %v", context)
	}
}

// T43: a region outside the capture's content region is refused with a
// clear error, and the frame is not required to be a full display size.
func TestProjectDiscObservationRejectsOutsideContent(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	base := collect.Observation{
		Image:   img,
		Content: image.Rect(0, 0, 80, 100),
	}
	cases := []struct {
		name string
		line collect.LineRegion
	}{
		{"outside content", collect.LineRegion{Role: "name", Rect: image.Rect(85, 10, 95, 20)}},
		{"empty", collect.LineRegion{Role: "name", Rect: image.Rect(10, 10, 10, 20)}},
		{"no role", collect.LineRegion{Rect: image.Rect(10, 10, 20, 20)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			obs := base
			obs.Lines = []collect.LineRegion{tc.line}
			if _, err := ProjectDiscObservation(obs); err == nil {
				t.Fatal("the projection must refuse the region")
			}
		})
	}
	noFrame := collect.Observation{Content: base.Content}
	if _, err := ProjectDiscObservation(noFrame); err == nil {
		t.Fatal("the projection must refuse a capture without a frame")
	}
}

// T44: the deliverer writes context crops as evidence files, keeps them
// out of recognition, and stores the capture in the observed payload.
func TestDelivererWritesContextEvidence(t *testing.T) {
	dir := t.TempDir()
	store, err := record.Open(dir)
	if err != nil {
		t.Fatalf("record.Open: %v", err)
	}
	const id = "context-evidence"
	if err := store.CreateRecord(id); err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}
	shell := record.SessionShell{
		SessionID: id,
		Game:      "zzz",
		Entity:    "driveDisc",
		Client:    record.ClientMacOSCloud,
		Locale:    "zh-CN",
		Scope:     []byte(`{"kind":"zzzInventory","mode":"traverse"}`),
		StartedAt: nowUTC(),
		Coverage:  record.Coverage{Status: record.CoverageUnknown},
	}
	deliverer := newRecordDeliverer(store, shell, nil)
	line := image.NewRGBA(image.Rect(0, 0, 3, 2))
	contextImg := image.NewRGBA(image.Rect(0, 0, 4, 4))
	obs := Observation{
		Lines: []LineCrop{
			{Role: "name", Source: "frame 9 rect 0,0,3,2", Image: line},
		},
		Context: []LineCrop{
			{Role: "qualityBadge", Source: "frame 9 rect 0,0,4,4", Image: contextImg},
			{Role: "selectedCell", Source: "frame 9 rect 0,0,4,4", Image: contextImg},
		},
		Capture: json.RawMessage(`{"position":2,"quality":"S"}`),
	}
	if err := deliverer.Admitted(1, obs); err != nil {
		t.Fatalf("Admitted: %v", err)
	}
	item := deliverer.items[0]
	wantIDs := []string{
		evidenceID(1, 0),
		contextEvidenceID(1, 0),
		contextEvidenceID(1, 1),
	}
	if fmt.Sprint(item.EvidenceIDs) != fmt.Sprint(wantIDs) {
		t.Fatalf("evidence ids = %v, want %v", item.EvidenceIDs, wantIDs)
	}
	if len(deliverer.evidence) != 3 {
		t.Fatalf("evidence descriptors = %d, want 3", len(deliverer.evidence))
	}
	for _, ev := range deliverer.evidence {
		if ev.ByteLength <= 0 {
			t.Fatalf("evidence %s has no bytes", ev.ID)
		}
	}
	var observed map[string]any
	if err := json.Unmarshal(item.Observed, &observed); err != nil {
		t.Fatalf("observed payload: %v", err)
	}
	if _, ok := observed["capture"]; !ok {
		t.Fatalf("observed payload lacks the capture: %s", item.Observed)
	}
	contextMeta, ok := observed["context"].([]any)
	if !ok || len(contextMeta) != 2 {
		t.Fatalf("observed payload context = %v", observed["context"])
	}

	// The recognizer walks Lines only; a context-only observation submits
	// an empty result instead of touching the (nil) OCR session.
	recognizer := &lineRecognizer{}
	var submitted scan.Recognition
	err = recognizer.Recognize(context.Background(), scan.Task{
		SessionID:   scan.ID(id),
		Seq:         1,
		Observation: Observation{Context: []LineCrop{{Role: "qualityBadge", Image: colorImage()}}},
	}, func(_ scan.ID, _ scan.Seq, rec scan.Recognition) error {
		submitted = rec
		return nil
	})
	if err != nil {
		t.Fatalf("recognize a context-only observation: %v", err)
	}
	result, ok := submitted.Result.(RecognitionResult)
	if !ok || len(result.Lines) != 0 {
		t.Fatalf("recognition result = %+v, want no lines from context", submitted.Result)
	}
}

// colorImage returns a tiny opaque image for context crops.
func colorImage() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.SetRGBA(0, 0, color.RGBA{R: 10, G: 20, B: 30, A: 255})
	return img
}

// T45: GameScanSource.Mode reaches the declared scope; an empty mode keeps
// the historical observe-only label.
func TestGameScanSourceModeInScope(t *testing.T) {
	for _, tc := range []struct {
		mode string
		want string
	}{
		{"", "observeOnly"},
		{"observeOnly", "observeOnly"},
		{"traverse", "traverse"},
	} {
		source := &GameScanSource{
			Port:   &platformfake.Port{},
			Target: collect.MacCloudTarget,
			Hotkey: platform.HotkeySpec{Key: platform.KeyS},
			Client: record.ClientMacOSCloud,
			Mode:   tc.mode,
		}
		spec, err := source.NewScan()
		if err != nil {
			t.Fatalf("NewScan: %v", err)
		}
		var scope struct {
			Kind string `json:"kind"`
			Mode string `json:"mode"`
		}
		if err := json.Unmarshal(spec.Scope, &scope); err != nil {
			t.Fatalf("scope: %v", err)
		}
		if scope.Kind != "zzzInventory" || scope.Mode != tc.want {
			t.Fatalf("scope = %+v, want zzzInventory/%s", scope, tc.want)
		}
	}
}
