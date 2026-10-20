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
// an equipped object without an accepted identity keeps agentId null and a
// non-blocking warning, and explicitly unequipped records can validate
// once their management state is known (still needing human confirmation).
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
		t.Fatalf("equipped.agentId = %v, want null without an accepted identity", structured.Data.Equipped.AgentID)
	}
	if structured.Validation.Status != zzz.ValidationPassed {
		t.Fatalf("validation = %+v, want passed with an equipped warning", structured.Validation)
	}
	if len(structured.Validation.Issues) != 1 || structured.Validation.Issues[0].Severity != zzz.SeverityWarning {
		t.Fatalf("validation issues = %+v, want one equipped warning", structured.Validation.Issues)
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
	if unequipped.Validation.Status != zzz.ValidationPassed {
		t.Fatalf("validation = %+v, want passed", unequipped.Validation)
	}
	if len(unequipped.Validation.Issues) != 0 {
		t.Fatalf("unequipped validation issues = %+v, want none", unequipped.Validation.Issues)
	}

	legacy := structuredDisc(Observation{Rarity: zzz.RarityS}, lines)
	if legacy == nil || legacy.Data.ManagementState != nil || legacy.Data.Equipped != nil {
		t.Fatalf("legacy observation = %+v, want null management and equipped", legacy)
	}
}

// TestStructuredDiscCarriesAgentIdentity checks the identity rules: an
// accepted catalog id fills equipped.agentId and lets an otherwise complete
// record pass validation; zero or an unknown id stays null and unavailable;
// an unequipped record ignores a supplied id. Quality remains needsReview
// at the deliverer, covered by TestDelivererPersistsRawAndStructured.
func TestStructuredDiscCarriesAgentIdentity(t *testing.T) {
	lines := detailLines()
	base := Observation{
		Rarity:          zzz.RarityS,
		ManagementState: zzz.ManagementNone,
		EquippedState:   zzz.EquippedStateEquipped,
	}
	matched := base
	matched.AgentID = 1311
	got := structuredDisc(matched, lines)
	if got == nil || got.Data.Equipped == nil || got.Data.Equipped.AgentID == nil ||
		*got.Data.Equipped.AgentID != 1311 {
		t.Fatalf("equipped = %+v, want agentId 1311", got)
	}
	if got.Validation.Status != zzz.ValidationPassed {
		t.Fatalf("validation = %+v, want passed with a known id", got.Validation)
	}

	unmatched := base
	unmatched.AgentID = 0
	if got := structuredDisc(unmatched, lines); got.Data.Equipped.AgentID != nil ||
		got.Validation.Status != zzz.ValidationPassed {
		t.Fatalf("unmatched = %+v/%s, want agentId null and passed with a warning", got.Data.Equipped, got.Validation.Status)
	}

	stale := base
	stale.AgentID = 9999
	if got := structuredDisc(stale, lines); got.Data.Equipped.AgentID != nil ||
		got.Validation.Status != zzz.ValidationUnavailable {
		t.Fatalf("stale = %+v/%s, want agentId null and unavailable", got.Data.Equipped, got.Validation.Status)
	}

	unequipped := Observation{
		Rarity:          zzz.RarityS,
		ManagementState: zzz.ManagementDiscarded,
		EquippedState:   zzz.EquippedStateUnequipped,
		AgentID:         1311,
	}
	if got := structuredDisc(unequipped, lines); got.Data.Equipped.AgentID != nil ||
		got.Data.Equipped.State != zzz.EquippedStateUnequipped ||
		got.Validation.Status != zzz.ValidationPassed {
		t.Fatalf("unequipped = %+v/%s, want null agentId and passed", got.Data.Equipped, got.Validation.Status)
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
// payload in one data document, carries a matched agent id, and is
// auto-accepted once its current validation passes.
func TestDelivererPersistsRawAndStructured(t *testing.T) {
	lines := detailLines()
	result := RecognitionResult{
		Lines: lines,
		Structured: structuredDisc(Observation{
			Rarity:          zzz.RarityS,
			ManagementState: zzz.ManagementLocked,
			EquippedState:   zzz.EquippedStateEquipped,
			AgentID:         1311,
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
	if item.Quality != record.QualityAutoAccepted {
		t.Fatalf("quality = %s, want autoAccepted", item.Quality)
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
		doc.Structured.Data.Equipped.AgentID == nil || *doc.Structured.Data.Equipped.AgentID != 1311 {
		t.Fatalf("persisted equipped = %+v, want equipped/1311", doc.Structured.Data.Equipped)
	}
	if doc.Structured.Validation.Status != zzz.ValidationPassed {
		t.Fatalf("persisted validation = %+v, want passed", doc.Structured.Validation)
	}

	failed := record.ItemRecord{Sequence: 2, Status: record.StatusRunning, Quality: record.QualityNotEvaluated}
	if err := applyTerminalLocked(&failed, scan.Record{State: scan.StateFailed, Reason: "no text"}); err != nil {
		t.Fatalf("apply failed terminal: %v", err)
	}
	if len(failed.Data) != 0 || failed.Quality != record.QualityNotEvaluated {
		t.Fatalf("failed item data/quality = %s/%s, want empty/notEvaluated", failed.Data, failed.Quality)
	}
}

// TestDelivererAppliesCurrentOptionalFieldPolicy checks auto-accept under
// the v2 policy: a missing management state stays needsReview, while an
// equipped disc without an accepted agent id is accepted with a warning.
// A result without structured data still stays needsReview.
func TestDelivererAppliesCurrentOptionalFieldPolicy(t *testing.T) {
	lines := detailLines()
	cases := []struct {
		name string
		obs  Observation
		want record.Quality
	}{
		{
			name: "missing management state",
			obs:  Observation{Rarity: zzz.RarityS, EquippedState: zzz.EquippedStateUnequipped},
			want: record.QualityNeedsReview,
		},
		{
			name: "equipped without agent identity",
			obs: Observation{
				Rarity:          zzz.RarityS,
				ManagementState: zzz.ManagementLocked,
				EquippedState:   zzz.EquippedStateEquipped,
			},
			want: record.QualityAutoAccepted,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := RecognitionResult{Lines: lines, Structured: structuredDisc(tc.obs, lines)}
			item := record.ItemRecord{Sequence: 1, Status: record.StatusRunning, Quality: record.QualityNotEvaluated}
			if err := applyTerminalLocked(&item, scan.Record{State: scan.StateProduced, Result: result}); err != nil {
				t.Fatalf("apply terminal: %v", err)
			}
			if item.Quality != tc.want {
				t.Fatalf("quality = %s, want %s", item.Quality, tc.want)
			}
		})
	}

	complete := RecognitionResult{
		Lines: lines,
		Structured: structuredDisc(Observation{
			Rarity:          zzz.RarityS,
			ManagementState: zzz.ManagementLocked,
			EquippedState:   zzz.EquippedStateUnequipped,
		}, lines),
	}
	item := record.ItemRecord{Sequence: 1, Status: record.StatusRunning, Quality: record.QualityNotEvaluated}
	if err := applyTerminalLocked(&item, scan.Record{State: scan.StateProduced, Result: complete}); err != nil {
		t.Fatalf("apply complete terminal: %v", err)
	}
	if item.Quality != record.QualityAutoAccepted {
		t.Fatalf("complete unequipped quality = %s, want autoAccepted", item.Quality)
	}

	item = record.ItemRecord{Sequence: 1, Status: record.StatusRunning, Quality: record.QualityNotEvaluated}
	if err := applyTerminalLocked(&item, scan.Record{
		State:  scan.StateProduced,
		Result: RecognitionResult{Lines: lines},
	}); err != nil {
		t.Fatalf("apply line-only terminal: %v", err)
	}
	if item.Quality != record.QualityNeedsReview {
		t.Fatalf("line-only quality = %s, want needsReview", item.Quality)
	}
}

// TestAutoAcceptableRequiresPassedValidationWithoutErrorIssues pins the
// acceptance predicate independently of the deliverer state machine.
func TestAutoAcceptableRequiresPassedValidationWithoutErrorIssues(t *testing.T) {
	passed := &zzz.StructuredDisc{Validation: zzz.Validation{Status: zzz.ValidationPassed}}
	errIssue := zzz.Issue{Severity: zzz.SeverityError}
	cases := []struct {
		name       string
		structured *zzz.StructuredDisc
		want       bool
	}{
		{"nil", nil, false},
		{"passed", passed, true},
		{"unavailable", &zzz.StructuredDisc{Validation: zzz.Validation{Status: zzz.ValidationUnavailable}}, false},
		{"failed", &zzz.StructuredDisc{Validation: zzz.Validation{Status: zzz.ValidationFailed}}, false},
		{"error issue", &zzz.StructuredDisc{Issues: []zzz.Issue{errIssue}, Validation: zzz.Validation{Status: zzz.ValidationPassed}}, false},
		{"error validation issue", &zzz.StructuredDisc{Validation: zzz.Validation{Status: zzz.ValidationPassed, Issues: []zzz.Issue{errIssue}}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := autoAcceptable(tc.structured); got != tc.want {
				t.Fatalf("autoAcceptable = %v, want %v", got, tc.want)
			}
		})
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
