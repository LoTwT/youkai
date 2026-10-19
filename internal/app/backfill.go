package app

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/LoTwT/youkai/internal/game/zzz"
)

// BackfillStructured derives the structured drive-disc payload of one
// persisted item and adds it beside the untouched raw recognition lines.
// It is the offline migration path for checkpoints written before the
// structured payload existed: data is the item's current RecognitionResult
// JSON ({"lines":[...]}), observed is the sealed observed payload whose
// capture carries the verified rarity. The function does not modify any
// file by itself; callers decide when to persist the returned payload.
//
// It is idempotent: data that already carries a non-null structured object
// is returned unchanged with changed=false. Data whose lines contain no
// drive-disc role is likewise left alone, so non-disc payloads are never
// given a fabricated empty disc. A malformed data document is an error;
// the old bytes are never partially replaced.
func BackfillStructured(data, observed json.RawMessage) (json.RawMessage, bool, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return data, false, nil
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, false, fmt.Errorf("app: backfill structured payload: decode data: %w", err)
	}
	if doc == nil {
		return data, false, nil
	}
	if existing, ok := doc["structured"]; ok {
		trimmed := bytes.TrimSpace(existing)
		if len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null")) {
			return data, false, nil
		}
	}

	var lines []RecognizedLine
	if raw, ok := doc["lines"]; ok {
		if err := json.Unmarshal(raw, &lines); err != nil {
			return nil, false, fmt.Errorf("app: backfill structured payload: decode lines: %w", err)
		}
	}
	detail, ok := discDetailLines(lines)
	if !ok {
		return data, false, nil
	}
	rec, issues := zzz.ParseDetail(detail)
	structured, err := json.Marshal(zzz.ToStructured(rec, observedRarity(observed), issues))
	if err != nil {
		return nil, false, fmt.Errorf("app: backfill structured payload: encode structured data: %w", err)
	}
	doc["structured"] = structured
	out, err := json.Marshal(doc)
	if err != nil {
		return nil, false, fmt.Errorf("app: backfill structured payload: encode data: %w", err)
	}
	return out, true, nil
}

// observedRarity reads the verified rarity out of a sealed observed
// payload's capture record. A missing or malformed observation stays
// unknown rather than becoming a guessed rarity.
func observedRarity(observed json.RawMessage) string {
	if len(bytes.TrimSpace(observed)) == 0 {
		return ""
	}
	var doc struct {
		Capture struct {
			Quality string `json:"quality"`
		} `json:"capture"`
	}
	if err := json.Unmarshal(observed, &doc); err != nil {
		return ""
	}
	return doc.Capture.Quality
}
