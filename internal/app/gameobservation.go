package app

import (
	"encoding/json"
	"fmt"
	"image"
	"image/draw"

	"github.com/LoTwT/youkai/internal/game/zzz/agentavatars"
	"github.com/LoTwT/youkai/internal/game/zzz/collect"
)

// ProjectDiscObservation turns one inventory capture into the application's
// observation: every declared line and context region is copied into its own
// RGBA, so the 8 MB frame is not retained, and the traversal's capture
// record travels along as provenance. The projection is injected into
// collect.Inventory because collect cannot import app (that would cycle).
//
// The context includes the detail and list equipped-agent avatars. When
// the traversal confirmed an equipped state, the projection matches those
// two crops against the embedded fairy avatars and carries the accepted
// fairy numeric id plus the matcher's evidence. A missing, ambiguous or
// low-confidence match leaves the id at zero, so the structured payload
// writes agentId=null instead of guessing.
//
// Every region must lie inside the capture's content region. A region that
// does not is a calibration or wiring error, not a disc to skip: the error
// names the role and rectangle and fails the admission.
func ProjectDiscObservation(obs collect.Observation) (any, error) {
	if obs.Image == nil {
		return nil, fmt.Errorf("app: project disc observation: the capture has no frame image")
	}
	lines, err := projectRegions(obs, obs.Lines, "line")
	if err != nil {
		return nil, err
	}
	context, err := projectRegions(obs, obs.Context, "context")
	if err != nil {
		return nil, err
	}
	agentID, agentEvidence := matchEquippedAgent(obs, context)
	capture, err := json.Marshal(obs)
	if err != nil {
		return nil, fmt.Errorf("app: project disc observation: marshal capture: %w", err)
	}
	// Rarity is the traversal's calibrated quality verdict. It is copied
	// explicitly so the recognizer can attach it without parsing the
	// capture JSON or inferring a rarity from the recognized text.
	return Observation{
		Lines:              lines,
		Context:            context,
		Rarity:             string(obs.Quality),
		ManagementState:    obs.ManagementState,
		ManagementEvidence: obs.ManagementEvidence,
		EquippedState:      obs.EquippedState,
		EquippedEvidence:   obs.EquippedEvidence,
		AgentID:            agentID,
		AgentIDEvidence:    agentEvidence,
		Capture:            capture,
	}, nil
}

// matchEquippedAgent runs the avatar matcher only when the traversal
// confirmed equipment presence. Identity is deliberately not inferred for
// unequipped or unresolved states, and a missing avatar crop is reported as
// unavailable rather than matched against whatever pixels happen to be in
// the context.
func matchEquippedAgent(obs collect.Observation, context []LineCrop) (int, string) {
	if obs.EquippedState != collect.EquippedStateEquipped {
		return 0, "agent avatar match not evaluated: the equipment state is not equipped"
	}
	detail, ok := contextCrop(context, collect.LineDetailAvatar)
	if !ok {
		return 0, "agent avatar match unavailable: the capture has no detail avatar crop"
	}
	list, ok := contextCrop(context, collect.LineListAvatar)
	if !ok {
		return 0, "agent avatar match unavailable: the capture has no list avatar crop"
	}
	res := agentavatars.Match(detail.Image, list.Image)
	return res.AgentID, res.Evidence
}

// contextCrop finds one non-recognition context crop by role.
func contextCrop(crops []LineCrop, role string) (LineCrop, bool) {
	for _, crop := range crops {
		if crop.Role == role {
			return crop, crop.Image != nil
		}
	}
	return LineCrop{}, false
}

// projectRegions copies each region out of the captured frame. The source
// description preserves the frame identity and the region in frame pixels,
// so every evidence file is traceable to the capture it came from.
func projectRegions(obs collect.Observation, regions []collect.LineRegion, kind string) ([]LineCrop, error) {
	crops := make([]LineCrop, 0, len(regions))
	for _, region := range regions {
		if region.Role == "" {
			return nil, fmt.Errorf("app: project disc observation: %s region has no role", kind)
		}
		r := region.Rect
		if r.Empty() {
			return nil, fmt.Errorf("app: project disc observation: %s %q has an empty region", kind, region.Role)
		}
		if !r.In(obs.Content) {
			return nil, fmt.Errorf("app: project disc observation: %s %q region %v is outside the content region %v",
				kind, region.Role, r, obs.Content)
		}
		crop := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
		draw.Draw(crop, crop.Bounds(), obs.Image, r.Min, draw.Src)
		crops = append(crops, LineCrop{
			Role:   region.Role,
			Source: fmt.Sprintf("frame %d rect %d,%d,%d,%d", obs.FrameID, r.Min.X, r.Min.Y, r.Max.X, r.Max.Y),
			Image:  crop,
		})
	}
	return crops, nil
}
