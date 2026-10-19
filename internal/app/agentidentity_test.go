package app

import (
	"image"
	"image/color"
	"image/draw"
	"strings"
	"testing"

	xdraw "golang.org/x/image/draw"

	"github.com/LoTwT/youkai/internal/game/zzz"
	"github.com/LoTwT/youkai/internal/game/zzz/agentavatars"
	"github.com/LoTwT/youkai/internal/game/zzz/collect"
)

// TestProjectDiscObservationMatchesAgentAvatar drives the pixel plumbing
// end to end: the layout's detail and list avatar regions are cropped into
// context evidence, the matcher accepts the synthetic embedded icon, and
// the structured payload carries the fairy numeric id with passed
// validation even though the record still needs review.
func TestProjectDiscObservationMatchesAgentAvatar(t *testing.T) {
	layout := collect.MacCloudLayout()
	asset, ok := agentavatars.AvatarImage(1311)
	if !ok {
		t.Fatal("AvatarImage(1311) failed")
	}
	frame := syntheticFrame(layout, asset)

	obs := collect.Observation{
		Image:              frame,
		Content:            image.Rect(0, 0, 900, 400),
		FrameID:            7,
		Quality:            collect.QualityS,
		ManagementState:    string(collect.ManagementNone),
		EquippedState:      collect.EquippedStateEquipped,
		ManagementEvidence: "list badge none",
		EquippedEvidence:   "detail and list avatars present",
		Context:            avatarContext(layout, 0),
	}
	projected, err := ProjectDiscObservation(obs)
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	appObs, ok := projected.(Observation)
	if !ok {
		t.Fatalf("projection is %T, want app.Observation", projected)
	}
	if appObs.AgentID != 1311 {
		t.Fatalf("agentId = %d, want 1311 (%s)", appObs.AgentID, appObs.AgentIDEvidence)
	}
	if !strings.Contains(appObs.AgentIDEvidence, "1311") {
		t.Fatalf("identity evidence does not name the agent: %s", appObs.AgentIDEvidence)
	}
	if len(appObs.Context) != 4 {
		t.Fatalf("context crops = %d, want qualityBadge/selectedCell/detailAvatar/listAvatar", len(appObs.Context))
	}
	detail, ok := contextCrop(appObs.Context, collect.LineDetailAvatar)
	if !ok || detail.Image == nil || detail.Image.Bounds().Size() != image.Pt(44, 44) {
		t.Fatalf("detail avatar evidence = %+v", detail)
	}
	list, ok := contextCrop(appObs.Context, collect.LineListAvatar)
	if !ok || list.Image == nil || list.Image.Bounds().Size() != image.Pt(32, 32) {
		t.Fatalf("list avatar evidence = %+v", list)
	}

	structured := structuredDisc(appObs, detailLines())
	if structured == nil || structured.Data.Equipped == nil ||
		structured.Data.Equipped.AgentID == nil || *structured.Data.Equipped.AgentID != 1311 {
		t.Fatalf("structured equipped = %+v, want agentId 1311", structured)
	}
	if structured.Validation.Status != zzz.ValidationPassed {
		t.Fatalf("validation = %+v, want passed", structured.Validation)
	}
}

// TestProjectDiscObservationKeepsUnreliableIdentityNull covers the
// conservative paths: a detail/list mismatch, a missing avatar crop and an
// unequipped record all leave the id at zero.
func TestProjectDiscObservationKeepsUnreliableIdentityNull(t *testing.T) {
	layout := collect.MacCloudLayout()
	astra, ok := agentavatars.AvatarImage(1311)
	if !ok {
		t.Fatal("AvatarImage(1311) failed")
	}
	ellen, ok := agentavatars.AvatarImage(1191)
	if !ok {
		t.Fatal("AvatarImage(1191) failed")
	}
	frame := syntheticFrame(layout, astra)
	drawAvatar(frame, layout.AvatarRect(0), ellen)
	base := collect.Observation{
		Image:           frame,
		Content:         image.Rect(0, 0, 900, 400),
		FrameID:         8,
		Quality:         collect.QualityS,
		ManagementState: string(collect.ManagementNone),
		EquippedState:   collect.EquippedStateEquipped,
		Context:         avatarContext(layout, 0),
	}
	projected, err := ProjectDiscObservation(base)
	if err != nil {
		t.Fatalf("project mismatch: %v", err)
	}
	mismatch := projected.(Observation)
	if mismatch.AgentID != 0 {
		t.Fatalf("mixed avatars produced agentId %d: %s", mismatch.AgentID, mismatch.AgentIDEvidence)
	}
	structured := structuredDisc(mismatch, detailLines())
	if structured.Data.Equipped.AgentID != nil || structured.Validation.Status != zzz.ValidationUnavailable {
		t.Fatalf("mixed avatars = %+v/%s, want null/unavailable",
			structured.Data.Equipped, structured.Validation.Status)
	}

	missing := base
	missing.Context = avatarContext(layout, 0)[:2]
	projected, err = ProjectDiscObservation(missing)
	if err != nil {
		t.Fatalf("project missing crop: %v", err)
	}
	if got := projected.(Observation); got.AgentID != 0 ||
		!strings.Contains(got.AgentIDEvidence, "no detail avatar crop") {
		t.Fatalf("missing crop = %d/%s, want unknown with the missing-crop reason",
			got.AgentID, got.AgentIDEvidence)
	}

	unequipped := base
	unequipped.EquippedState = collect.EquippedStateUnequipped
	projected, err = ProjectDiscObservation(unequipped)
	if err != nil {
		t.Fatalf("project unequipped: %v", err)
	}
	got := projected.(Observation)
	if got.AgentID != 0 || !strings.Contains(got.AgentIDEvidence, "not evaluated") {
		t.Fatalf("unequipped = %d/%s, want zero and not evaluated", got.AgentID, got.AgentIDEvidence)
	}
	structured = structuredDisc(got, detailLines())
	if structured.Data.Equipped == nil || structured.Data.Equipped.State != zzz.EquippedStateUnequipped ||
		structured.Data.Equipped.AgentID != nil || structured.Validation.Status != zzz.ValidationPassed {
		t.Fatalf("unequipped structured = %+v/%s, want unequipped/null/passed",
			structured.Data.Equipped, structured.Validation.Status)
	}
}

// avatarContext is the admission's non-recognition region list in the
// order collect.Layout.contextLines produces.
func avatarContext(layout collect.Layout, slot int) []collect.LineRegion {
	return []collect.LineRegion{
		{Role: collect.LineQualityBadge, Rect: layout.QualityBadge},
		{Role: collect.LineSelectedCell, Rect: layout.BorderRect(slot)},
		{Role: collect.LineDetailAvatar, Rect: layout.DetailAvatar},
		{Role: collect.LineListAvatar, Rect: layout.AvatarRect(slot)},
	}
}

// syntheticFrame builds a small frame containing the real layout's avatar
// regions with one embedded icon drawn into both. The frame is only large
// enough for the context regions; the matcher never needs the full frame.
func syntheticFrame(layout collect.Layout, detailAsset image.Image) *image.RGBA {
	frame := image.NewRGBA(image.Rect(0, 0, 900, 400))
	draw.Draw(frame, frame.Bounds(), &image.Uniform{C: color.RGBA{14, 13, 16, 255}}, image.Point{}, draw.Src)
	drawAvatar(frame, layout.DetailAvatar, detailAsset)
	drawAvatar(frame, layout.AvatarRect(0), detailAsset)
	return frame
}

// drawAvatar draws an embedded icon into a calibration region using the
// observed real-frame inner geometry: the detail circle is 33x33 at (+3,
// +4) inside its 44x44 probe; the list circle fills the 32x32 probe.
func drawAvatar(frame *image.RGBA, region image.Rectangle, asset image.Image) {
	if region.Dx() == 44 && region.Dy() == 44 {
		xdraw.CatmullRom.Scale(frame, image.Rect(region.Min.X+3, region.Min.Y+4, region.Min.X+36, region.Min.Y+37),
			asset, asset.Bounds(), xdraw.Over, nil)
		return
	}
	xdraw.CatmullRom.Scale(frame, region.Inset(1), asset, asset.Bounds(), xdraw.Over, nil)
}
