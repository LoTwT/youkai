package app

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/LoTwT/youkai/internal/game/zzz"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/LoTwT/youkai/internal/scan"
)

// detailLines returns the seven recognized lines of the first fixture
// disc in observation order.
func detailLines() []RecognizedLine {
	return []RecognizedLine{
		{Role: "name", Text: "河豚电音[1]", Score: 0.99},
		{Role: "level", Text: "等级15/15", Score: 0.98},
		{Role: "main", Text: "生命值 2200", Score: 0.97},
		{Role: "sub1", Text: "暴击伤害 +3 19.2%", Score: 0.96},
		{Role: "sub2", Text: "穿透值 9", Score: 0.95},
		{Role: "sub3", Text: "攻击力 +1 6%", Score: 0.94},
		{Role: "sub4", Text: "暴击率 2.4%", Score: 0.93},
	}
}

// TestStructuredDiscFromRecognizedLines checks the adapter's role
// assembly: the structured payload carries the numeric set id and parsed
// values, while every raw line keeps its exact role, text and score.
func TestStructuredDiscFromRecognizedLines(t *testing.T) {
	lines := detailLines()
	result := RecognitionResult{
		Lines:      lines,
		Structured: structuredDisc(Observation{Rarity: zzz.RarityS}, lines),
	}
	if result.Structured == nil {
		t.Fatal("recognized disc has no structured payload")
	}
	data := result.Structured.Data
	if data.SetID == nil || *data.SetID != 31100 {
		t.Fatalf("setId = %v, want 31100", data.SetID)
	}
	if data.Rarity == nil || *data.Rarity != zzz.RarityS {
		t.Fatalf("rarity = %v, want S", data.Rarity)
	}
	if data.Slot == nil || *data.Slot != 1 || data.Level == nil || *data.Level != 15 {
		t.Fatalf("slot/level = %v/%v, want 1/15", data.Slot, data.Level)
	}
	if len(data.Substats) != 4 {
		t.Fatalf("substats = %d, want 4", len(data.Substats))
	}
	if data.Substats[0].EnhancementRolls == nil || *data.Substats[0].EnhancementRolls != 3 {
		t.Fatalf("substats[0].enhancementRolls = %v, want 3", data.Substats[0].EnhancementRolls)
	}
	if result.Structured.Validation.Status != zzz.ValidationUnavailable {
		t.Fatalf("validation = %+v, want unavailable", result.Structured.Validation)
	}
	if !reflect.DeepEqual(result.Lines, lines) {
		t.Fatalf("raw lines changed: %+v", result.Lines)
	}

	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("result is not a JSON object: %v", err)
	}
	if _, ok := doc["lines"]; !ok {
		t.Fatalf("result lost the raw lines: %s", raw)
	}
	if _, ok := doc["structured"]; !ok {
		t.Fatalf("result lost the structured payload: %s", raw)
	}
}

// TestStructuredDiscRarityFromCapture checks that rarity is never inferred
// from text: without a verified capture verdict it stays null with an
// error, even when the set and level lines are perfect.
func TestStructuredDiscRarityFromCapture(t *testing.T) {
	lines := detailLines()
	structured := structuredDisc(Observation{}, lines)
	if structured == nil {
		t.Fatal("recognized disc has no structured payload")
	}
	if structured.Data.Rarity != nil {
		t.Fatalf("rarity = %v, want null without capture evidence", structured.Data.Rarity)
	}
	if structured.Validation.Status != zzz.ValidationFailed {
		t.Fatalf("validation = %+v, want failed", structured.Validation)
	}
}

// TestStructuredDiscCarriesCaptureState checks the adapter's non-text
// fields: management state and equipment presence come from the capture,
// the equipped object keeps agentId null until PR 6b, and validation stays
// unavailable instead of auto-accepting the record.
func TestStructuredDiscCarriesCaptureState(t *testing.T) {
	lines := detailLines()
	structured := structuredDisc(Observation{
		Rarity:             zzz.RarityS,
		ManagementState:    zzz.ManagementLocked,
		EquippedState:      zzz.EquippedStateEquipped,
		ManagementEvidence: "list badge locked",
		EquippedEvidence:   "detail and list avatar present",
	}, lines)
	if structured == nil {
		t.Fatal("recognized disc has no structured payload")
	}
	if structured.Data.ManagementState == nil || *structured.Data.ManagementState != zzz.ManagementLocked {
		t.Fatalf("managementState = %v, want locked", structured.Data.ManagementState)
	}
	if structured.Data.Equipped == nil || structured.Data.Equipped.State != zzz.EquippedStateEquipped {
		t.Fatalf("equipped = %+v, want equipped", structured.Data.Equipped)
	}
	if structured.Data.Equipped.AgentID != nil {
		t.Fatalf("equipped.agentId = %v, want null in this revision", structured.Data.Equipped.AgentID)
	}
	if structured.Validation.Status != zzz.ValidationUnavailable {
		t.Fatalf("validation = %+v, want unavailable while agentId is unresolved", structured.Validation)
	}

	unequipped := structuredDisc(Observation{
		Rarity:          zzz.RarityS,
		ManagementState: zzz.ManagementDiscarded,
		EquippedState:   zzz.EquippedStateUnequipped,
	}, lines)
	if unequipped == nil || unequipped.Data.Equipped == nil ||
		unequipped.Data.Equipped.State != zzz.EquippedStateUnequipped ||
		unequipped.Data.Equipped.AgentID != nil {
		t.Fatalf("unequipped = %+v, want state unequipped with agentId null", unequipped)
	}
	if unequipped.Data.ManagementState == nil || *unequipped.Data.ManagementState != zzz.ManagementDiscarded {
		t.Fatalf("managementState = %v, want discarded", unequipped.Data.ManagementState)
	}
	if unequipped.Validation.Status != zzz.ValidationUnavailable {
		t.Fatalf("validation = %+v, want unavailable", unequipped.Validation)
	}

	legacy := structuredDisc(Observation{Rarity: zzz.RarityS}, lines)
	if legacy == nil || legacy.Data.ManagementState != nil || legacy.Data.Equipped != nil {
		t.Fatalf("legacy observation = %+v, want null management and equipped", legacy)
	}
}

// TestStructuredDiscWithoutDetailRoles checks that a non-disc observation
// gets no fabricated structured payload.
func TestStructuredDiscWithoutDetailRoles(t *testing.T) {
	if got := structuredDisc(Observation{}, []RecognizedLine{{Role: "qualityBadge", Text: "S"}}); got != nil {
		t.Fatalf("structured payload = %+v, want nil", got)
	}
}

// TestDelivererPersistsRawAndStructured drives the terminal update and
// checks that a produced item keeps the raw lines and the structured
// payload in one data document, is marked needsReview even with confirmed
// management/equipment presence, and never becomes auto-accepted while the
// equipper identity is unresolved.
func TestDelivererPersistsRawAndStructured(t *testing.T) {
	lines := detailLines()
	result := RecognitionResult{
		Lines: lines,
		Structured: structuredDisc(Observation{
			Rarity:          zzz.RarityS,
			ManagementState: zzz.ManagementLocked,
			EquippedState:   zzz.EquippedStateEquipped,
		}, lines),
	}
	item := record.ItemRecord{
		Sequence: 1,
		Status:   record.StatusRunning,
		Quality:  record.QualityNotEvaluated,
	}
	if err := applyTerminalLocked(&item, scan.Record{State: scan.StateProduced, Result: result}); err != nil {
		t.Fatalf("apply terminal: %v", err)
	}
	if item.Status != record.StatusProduced {
		t.Fatalf("status = %s, want produced", item.Status)
	}
	if item.Quality != record.QualityNeedsReview {
		t.Fatalf("quality = %s, want needsReview", item.Quality)
	}
	var doc struct {
		Lines      []RecognizedLine    `json:"lines"`
		Structured *zzz.StructuredDisc `json:"structured"`
	}
	if err := json.Unmarshal(item.Data, &doc); err != nil {
		t.Fatalf("data: %v", err)
	}
	if !reflect.DeepEqual(doc.Lines, lines) {
		t.Fatalf("raw lines = %+v, want %+v", doc.Lines, lines)
	}
	if doc.Structured == nil || doc.Structured.Data.SetID == nil || *doc.Structured.Data.SetID != 31100 {
		t.Fatalf("structured payload = %+v", doc.Structured)
	}
	if doc.Structured.Data.ManagementState == nil || *doc.Structured.Data.ManagementState != zzz.ManagementLocked {
		t.Fatalf("persisted managementState = %v, want locked", doc.Structured.Data.ManagementState)
	}
	if doc.Structured.Data.Equipped == nil || doc.Structured.Data.Equipped.State != zzz.EquippedStateEquipped ||
		doc.Structured.Data.Equipped.AgentID != nil {
		t.Fatalf("persisted equipped = %+v, want equipped/null", doc.Structured.Data.Equipped)
	}

	failed := record.ItemRecord{Sequence: 2, Status: record.StatusRunning, Quality: record.QualityNotEvaluated}
	if err := applyTerminalLocked(&failed, scan.Record{State: scan.StateFailed, Reason: "no text"}); err != nil {
		t.Fatalf("apply failed terminal: %v", err)
	}
	if len(failed.Data) != 0 || failed.Quality != record.QualityNotEvaluated {
		t.Fatalf("failed item data/quality = %s/%s, want empty/notEvaluated", failed.Data, failed.Quality)
	}
}

// TestBackfillStructuredFromLegacyLines rebuilds the structured payload of
// an old data document from its raw lines and capture rarity, preserving
// every raw line and making the operation idempotent.
func TestBackfillStructuredFromLegacyLines(t *testing.T) {
	lines := detailLines()
	legacy, err := json.Marshal(struct {
		Lines []RecognizedLine `json:"lines"`
	}{Lines: lines})
	if err != nil {
		t.Fatalf("marshal legacy data: %v", err)
	}
	observed := json.RawMessage(`{"lines":[{"role":"name","source":"frame 1"}],"capture":{"quality":"S"}}`)
	out, changed, err := BackfillStructured(legacy, observed)
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if !changed {
		t.Fatal("legacy data was not backfilled")
	}
	var doc struct {
		Lines      []RecognizedLine    `json:"lines"`
		Structured *zzz.StructuredDisc `json:"structured"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("backfilled data: %v", err)
	}
	if !reflect.DeepEqual(doc.Lines, lines) {
		t.Fatalf("raw lines after backfill = %+v, want %+v", doc.Lines, lines)
	}
	if doc.Structured == nil || doc.Structured.Data.SetID == nil || *doc.Structured.Data.SetID != 31100 {
		t.Fatalf("structured payload = %+v", doc.Structured)
	}
	if doc.Structured.Data.Rarity == nil || *doc.Structured.Data.Rarity != zzz.RarityS {
		t.Fatalf("rarity = %v, want S from the capture", doc.Structured.Data.Rarity)
	}
	if doc.Structured.Data.ManagementState != nil || doc.Structured.Data.Equipped != nil {
		t.Fatalf("legacy backfill invented state: %+v", doc.Structured.Data)
	}

	again, changed, err := BackfillStructured(out, observed)
	if err != nil {
		t.Fatalf("second backfill: %v", err)
	}
	if changed || string(again) != string(out) {
		t.Fatalf("second backfill changed the payload: %s", again)
	}

	other := json.RawMessage(`{"lines":[{"role":"context","text":"x","score":1}]}`)
	if _, changed, err := BackfillStructured(other, observed); err != nil || changed {
		t.Fatalf("non-disc payload changed=%v err=%v, want unchanged", changed, err)
	}
	if _, _, err := BackfillStructured(json.RawMessage(`{"lines":`), observed); err == nil {
		t.Fatal("malformed legacy payload was accepted")
	}
}
