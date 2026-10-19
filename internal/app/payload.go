package app

import (
	"encoding/json"
	"fmt"
	"image"
)

// LineCrop is one text-line crop of an observation, as the collection
// controller admits it and the recognizer adapter recognizes it. The
// role names the line within its observation ("name", "level", "main",
// "sub1"…"sub4" for a drive disc); it stays opaque to the kernel, which
// carries the observation unchanged.
type LineCrop struct {
	// Role names the line within its observation.
	Role string
	// Source records where the crop came from, as the controller knows
	// it — a file name for the development data source, a frame and
	// region reference once the platform capture of M2 produces crops.
	// It is persisted with the observation as provenance.
	Source string
	// Image is the crop itself. It is never marshalled into a payload;
	// its pixels go to the evidence files and only the recognition
	// result travels in JSON.
	Image image.Image
}

// Observation is the payload of one admitted observation: the text-line
// crops of one item — one drive disc in the development data source. The
// kernel carries it opaquely; the recognizer adapter walks its lines, and
// the deliverer persists each line as an evidence image plus the line
// metadata as the observed payload. Field semantics (which line is which
// drive-disc field) belong to M2's game adapter and are deliberately not
// interpreted here.
type Observation struct {
	Lines []LineCrop
	// Context are the admission's non-recognition crops: the rarity badge
	// and the selected cell's border. The deliverer writes them as
	// evidence; the recognizer never reads them.
	Context []LineCrop
	// Capture is the traversal's own capture record, verbatim. Its pixels
	// are omitted by the collect side and live only in the evidence
	// files; the payload keeps the navigation provenance (position,
	// quality, lines, context, frame and visit identifiers).
	Capture json.RawMessage
}

// observedLine is the persisted projection of one LineCrop: everything
// except the pixels, which live in the evidence files.
type observedLine struct {
	Role   string `json:"role"`
	Source string `json:"source,omitempty"`
}

// observedPayload renders an Observation as the JSON the record's
// observed-payload field stores: the line and context metadata in
// observation order, matching the evidence ids of the item, plus the
// traversal's capture record.
func observedPayload(obs Observation) (json.RawMessage, error) {
	lines := make([]observedLine, len(obs.Lines))
	for i, crop := range obs.Lines {
		lines[i] = observedLine{Role: crop.Role, Source: crop.Source}
	}
	context := make([]observedLine, len(obs.Context))
	for i, crop := range obs.Context {
		context[i] = observedLine{Role: crop.Role, Source: crop.Source}
	}
	raw, err := json.Marshal(struct {
		Lines   []observedLine  `json:"lines"`
		Context []observedLine  `json:"context,omitempty"`
		Capture json.RawMessage `json:"capture,omitempty"`
	}{Lines: lines, Context: context, Capture: obs.Capture})
	if err != nil {
		return nil, fmt.Errorf("app: marshal observed payload: %w", err)
	}
	return raw, nil
}

// RecognizedLine is one recognized text line of an observation.
type RecognizedLine struct {
	Role  string  `json:"role"`
	Text  string  `json:"text"`
	Score float64 `json:"score"`
}

// RecognitionResult is the recognizer adapter's result payload: the
// recognized lines of one observation, in observation order. The score is
// the raw model confidence of the line (see the recognition package); it
// is not the probability that the field is correct.
type RecognitionResult struct {
	Lines []RecognizedLine `json:"lines"`
}
