package agentavatars

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	xdraw "golang.org/x/image/draw"

	"github.com/LoTwT/youkai/internal/game/zzz"
)

// TestEmbeddedAvatarCatalog pins the embedded set to the fairy catalog:
// 57 decodable 142x142 circular icons, the three documented missing ids,
// and one file per catalog association with no orphans.
func TestEmbeddedAvatarCatalog(t *testing.T) {
	agents := zzz.Agents()
	if len(agents) != 60 {
		t.Fatalf("catalog has %d agents, want 60", len(agents))
	}
	missing := map[int]bool{1381: true, 1531: true, 1551: true}
	withAvatar := 0
	for _, agent := range agents {
		if missing[agent.ID] {
			if agent.Avatar != "" {
				t.Fatalf("agent %d has avatar %q, want none in fairy", agent.ID, agent.Avatar)
			}
			if HasAvatar(agent.ID) {
				t.Fatalf("HasAvatar(%d) = true for a missing asset", agent.ID)
			}
			if _, ok := AvatarImage(agent.ID); ok {
				t.Fatalf("AvatarImage(%d) decoded a missing asset", agent.ID)
			}
			continue
		}
		if agent.Avatar == "" {
			t.Fatalf("agent %d (%s) has no embedded avatar", agent.ID, agent.Name)
		}
		withAvatar++
		img, ok := AvatarImage(agent.ID)
		if !ok {
			t.Fatalf("AvatarImage(%d) failed", agent.ID)
		}
		if got := img.Bounds().Size(); got != image.Pt(assetSize, assetSize) {
			t.Fatalf("agent %d avatar is %v, want %dx%d", agent.ID, got, assetSize, assetSize)
		}
	}
	if withAvatar != 57 {
		t.Fatalf("%d agents carry an avatar, want 57", withAvatar)
	}
	if got := EmbeddedAssetCount(); got != 57 {
		t.Fatalf("embedded files = %d, want 57", got)
	}

	catalogFiles := make(map[string]bool)
	for _, agent := range agents {
		if agent.Avatar != "" {
			catalogFiles[agent.Avatar+".webp"] = true
		}
	}
	entries, err := embedded.ReadDir(assetDir)
	if err != nil {
		t.Fatalf("read embedded assets: %v", err)
	}
	var total int
	for _, entry := range entries {
		if !catalogFiles[entry.Name()] {
			t.Fatalf("embedded file %s has no catalog entry", entry.Name())
		}
		data, err := embedded.ReadFile(assetDir + "/" + entry.Name())
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		total += len(data)
	}
	if total >= 2<<20 {
		t.Fatalf("embedded avatars are %d bytes, want < 2 MiB", total)
	}
}

// TestSyntheticMatchesEveryAvailableAgent scales each embedded icon into
// the real detail/list crop geometry, adds deterministic ±3 noise, and
// requires the matcher to return exactly that id. The upstream duplicate
// 1581/1621 is the one expected ambiguity: it must return 0.
func TestSyntheticMatchesEveryAvailableAgent(t *testing.T) {
	m, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	minScore, minID := 1.0, 0
	for _, agent := range zzz.Agents() {
		if agent.Avatar == "" {
			continue
		}
		asset, ok := AvatarImage(agent.ID)
		if !ok {
			t.Fatalf("AvatarImage(%d) failed", agent.ID)
		}
		detail, list := syntheticCrops(asset)
		res := m.Match(detail, list)
		if agent.ID == 1581 || agent.ID == 1621 {
			if res.AgentID != 0 {
				t.Fatalf("agent %d: duplicate avatar matched %d, want unknown (%s)", agent.ID, res.AgentID, res.Evidence)
			}
			if !strings.Contains(res.Evidence, "identical") {
				t.Fatalf("agent %d: evidence does not name the duplicate ambiguity: %s", agent.ID, res.Evidence)
			}
			continue
		}
		if res.AgentID != agent.ID {
			t.Fatalf("agent %d matched %d (score %.3f, second %.3f, margin %.3f): %s",
				agent.ID, res.AgentID, res.Score, res.Second, res.Margin, res.Evidence)
		}
		if res.Score < MinScore || res.Margin < MinMargin {
			t.Fatalf("agent %d synthetic score %.3f margin %.3f is below the accepted floor: %s",
				agent.ID, res.Score, res.Margin, res.Evidence)
		}
		if res.Score < minScore {
			minScore, minID = res.Score, agent.ID
		}
	}
	t.Logf("minimum synthetic match score %.3f (agent %d)", minScore, minID)
}

// TestSyntheticAmbiguityStaysUnknown covers the two ways a guess could be
// tempting: two different avatars in the detail/list pair, and a flat
// portrait-like disc with no agent art.
func TestSyntheticAmbiguityStaysUnknown(t *testing.T) {
	m, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a, ok := AvatarImage(1011)
	if !ok {
		t.Fatal("AvatarImage(1011) failed")
	}
	b, ok := AvatarImage(1191)
	if !ok {
		t.Fatal("AvatarImage(1191) failed")
	}
	detail, _ := syntheticCrops(a)
	_, list := syntheticCrops(b)
	if res := m.Match(detail, list); res.AgentID != 0 {
		t.Fatalf("mixed avatars matched %d, want unknown: %s", res.AgentID, res.Evidence)
	}

	flatDetail := image.NewRGBA(image.Rect(0, 0, 44, 44))
	flatList := image.NewRGBA(image.Rect(0, 0, 32, 32))
	draw.Draw(flatDetail, flatDetail.Bounds(), &image.Uniform{C: color.RGBA{14, 13, 16, 255}}, image.Point{}, draw.Src)
	draw.Draw(flatList, flatList.Bounds(), &image.Uniform{C: color.RGBA{14, 13, 16, 255}}, image.Point{}, draw.Src)
	if res := m.Match(flatDetail, flatList); res.AgentID != 0 {
		t.Fatalf("flat crops matched %d, want unknown: %s", res.AgentID, res.Evidence)
	}
}

// TestRealFrameIdentityStaysUnknown pins the feasibility result: the one
// frame whose equipper is independently known (Yixuan 1371) does not
// match any fairy icon closely enough. The crops are checked in under
// testdata and come from s2-70-unlocked-equipped/frame-0001.png.
func TestRealFrameIdentityStaysUnknown(t *testing.T) {
	detail := loadPNG(t, filepath.Join("testdata", "s2-70-detail-avatar.png"))
	list := loadPNG(t, filepath.Join("testdata", "s2-70-list-avatar.png"))
	if detail.Bounds().Size() != image.Pt(44, 44) || list.Bounds().Size() != image.Pt(32, 32) {
		t.Fatalf("real crops are %v/%v, want 44x44 and 32x32", detail.Bounds().Size(), list.Bounds().Size())
	}
	res := Match(detail, list)
	if res.AgentID != 0 {
		t.Fatalf("real frame matched %d, want unknown: %s", res.AgentID, res.Evidence)
	}
	if res.Score >= MinScore {
		t.Fatalf("real frame score %.3f reached the acceptance floor %.3f: %s", res.Score, MinScore, res.Evidence)
	}
	if res.Score >= 0.6 {
		t.Fatalf("real frame score %.3f is unexpectedly high; re-check the feasibility conclusion: %s", res.Score, res.Evidence)
	}
	if !strings.Contains(res.Evidence, "unavailable") {
		t.Fatalf("real frame evidence does not report the unknown: %s", res.Evidence)
	}
}

// syntheticCrops builds the real detail/list crop geometry around one
// embedded avatar and degrades it deterministically. The detail crop is
// 44x44 with the avatar at (3,4)-(36,37); the list crop is 32x32 with the
// avatar at (1,1)-(31,31).
func syntheticCrops(asset image.Image) (*image.RGBA, *image.RGBA) {
	detail := image.NewRGBA(image.Rect(0, 0, 44, 44))
	list := image.NewRGBA(image.Rect(0, 0, 32, 32))
	draw.Draw(detail, detail.Bounds(), &image.Uniform{C: color.RGBA{14, 13, 16, 255}}, image.Point{}, draw.Src)
	draw.Draw(list, list.Bounds(), &image.Uniform{C: color.RGBA{14, 13, 16, 255}}, image.Point{}, draw.Src)
	xdraw.CatmullRom.Scale(detail, image.Rect(3, 4, 36, 37), asset, asset.Bounds(), xdraw.Over, nil)
	xdraw.CatmullRom.Scale(list, image.Rect(1, 1, 31, 31), asset, asset.Bounds(), xdraw.Over, nil)
	degrade(detail)
	degrade(list)
	return detail, list
}

// degrade adds deterministic ±3 RGB noise so the synthetic pair resembles
// a quantized stream frame rather than a lossless copy.
func degrade(img *image.RGBA) {
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			i := img.PixOffset(x, y)
			n := (x*31+y*17+7)%7 - 3
			for c := 0; c < 3; c++ {
				img.Pix[i+c] = uint8(clamp(int(img.Pix[i+c]) + n))
			}
		}
	}
}

func loadPNG(t *testing.T, path string) image.Image {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return img
}

func clamp(v int) int {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return v
}
