package collect

import (
	"encoding/json"
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// TestStorageFrame347BadgeRegression replays the de-identified candidate
// frames from the Windows R14 run. Frame 347 is the actual candidate frame
// that stopped with positionLost after its 1 px vertical shift made five
// row-2 slot-2 badges ambiguous with slot 3; frames 338 and 340 are the two
// preceding candidate frames. The private evidence is not in the repository;
// set YOUKAI_FRAME347_EVIDENCE to the extracted handoff directory.
func TestStorageFrame347BadgeRegression(t *testing.T) {
	dir := os.Getenv("YOUKAI_FRAME347_EVIDENCE")
	if dir == "" {
		t.Skip("YOUKAI_FRAME347_EVIDENCE is not set; private frame347 evidence is unavailable")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Frames []struct {
			Role  string `json:"role"`
			Frame uint64 `json:"frame"`
			Cells []struct {
				Index        int    `json:"index"`
				ExactRect    [4]int `json:"exact_rect"`
				ObservedRect [4]int `json:"observed_rect"`
				ROIFile      string `json:"roi_file"`
			} `json:"cells"`
		} `json:"frames"`
		Ledger struct {
			Cells []struct {
				State    string `json:"state"`
				Position int    `json:"position"`
			} `json:"cells"`
		} `json:"ledger"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Frames) != 3 {
		t.Fatalf("candidate frames = %d, want 3", len(manifest.Frames))
	}
	layout := StorageLayout()
	matcher := &PixelMatcher{}
	for _, frame := range manifest.Frames {
		if len(frame.Cells) != len(layout.Slots) {
			t.Fatalf("%s frame %d cells = %d, want %d", frame.Role, frame.Frame, len(frame.Cells), len(layout.Slots))
		}
		known, wrong := 0, 0
		for _, cell := range frame.Cells {
			img := frame347ROI(t, filepath.Join(dir, cell.ROIFile), cell.ExactRect, cell.ObservedRect)
			got := matcher.positionVerdict(img, image.Rect(0, 0, cell.ExactRect[2], cell.ExactRect[3]), layout.CardPositions)
			if got.known() {
				known++
			}
			want := manifest.Ledger.Cells[cell.Index].Position
			if got.known() && got.Position != want {
				wrong++
			}
			if frame.Role == "failure" && cell.Index == 16 {
				if !got.known() || got.Position != 3 {
					t.Fatalf("failure R2C8 = %+v, want known position 3", got)
				}
			}
		}
		if known != len(layout.Slots) || wrong != 0 {
			t.Fatalf("%s frame %d: known=%d/%d wrong=%d", frame.Role, frame.Frame, known, len(layout.Slots), wrong)
		}
	}
}

// TestStorageFrame347AmbiguousBlendStaysUnknown keeps the 2/3 conflict gate:
// a synthetic half-blend of the real failing 2 cell and the real known 3 cell
// must not acquire a guessed slot merely because the calibration grew.
func TestStorageFrame347AmbiguousBlendStaysUnknown(t *testing.T) {
	dir := os.Getenv("YOUKAI_FRAME347_EVIDENCE")
	if dir == "" {
		t.Skip("YOUKAI_FRAME347_EVIDENCE is not set; private frame347 evidence is unavailable")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Frames []struct {
			Cells []struct {
				Index        int    `json:"index"`
				ExactRect    [4]int `json:"exact_rect"`
				ObservedRect [4]int `json:"observed_rect"`
				ROIFile      string `json:"roi_file"`
			} `json:"cells"`
		} `json:"frames"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	failure := manifest.Frames[len(manifest.Frames)-1]
	two := frame347ROI(t, filepath.Join(dir, failure.Cells[9].ROIFile), failure.Cells[9].ExactRect, failure.Cells[9].ObservedRect)
	three := frame347ROI(t, filepath.Join(dir, failure.Cells[16].ROIFile), failure.Cells[16].ExactRect, failure.Cells[16].ObservedRect)
	blend := image.NewRGBA(image.Rect(0, 0, two.Bounds().Dx(), two.Bounds().Dy()))
	for y := 0; y < two.Bounds().Dy(); y++ {
		for x := 0; x < two.Bounds().Dx(); x++ {
			i := blend.PixOffset(x, y)
			j := two.PixOffset(x, y)
			k := three.PixOffset(x, y)
			for c := 0; c < 4; c++ {
				blend.Pix[i+c] = uint8((int(two.Pix[j+c]) + int(three.Pix[k+c])) / 2)
			}
		}
	}
	got := (&PixelMatcher{}).positionVerdict(blend, image.Rect(0, 0, 34, 26), StorageLayout().CardPositions)
	if got.known() {
		t.Fatalf("half-blended 2/3 badge = %+v, want unknown/conflict", got)
	}
}

func frame347ROI(t *testing.T, path string, exact, observed [4]int) *image.RGBA {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(f)
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	src := image.NewRGBA(decoded.Bounds())
	draw.Draw(src, src.Bounds(), decoded, decoded.Bounds().Min, draw.Src)
	ox, oy := exact[0]-observed[0], exact[1]-observed[1]
	out := image.NewRGBA(image.Rect(0, 0, exact[2], exact[3]))
	draw.Draw(out, out.Bounds(), src, image.Pt(ox, oy), draw.Src)
	return out
}
