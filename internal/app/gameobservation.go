package app

import (
	"encoding/json"
	"fmt"
	"image"
	"image/draw"

	"github.com/LoTwT/youkai/internal/game/zzz/collect"
)

// ProjectDiscObservation turns one inventory capture into the application's
// observation: every declared line and context region is copied into its own
// RGBA, so the 8 MB frame is not retained, and the traversal's capture
// record travels along as provenance. The projection is injected into
// collect.Inventory because collect cannot import app (that would cycle).
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
	capture, err := json.Marshal(obs)
	if err != nil {
		return nil, fmt.Errorf("app: project disc observation: marshal capture: %w", err)
	}
	return Observation{Lines: lines, Context: context, Capture: capture}, nil
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
