package zzz

import (
	"encoding/json"
	"testing"
)

// TestToStructuredJSONShape pins the public structured shape for a
// complete four-substat disc: numeric fairy set id, S rarity from the
// capture, numeric slot/level, string decimal values, rolls in display
// order, explicit null management/equipper and an unavailable validation.
func TestToStructuredJSONShape(t *testing.T) {
	rec, issues := ParseDetail(DetailLines{
		Set:   "河豚电音[1]",
		Level: "等级15/15",
		Main:  "生命值 2200",
		Subs:  []string{"暴击伤害 +3 19.2%", "穿透值 9", "攻击力 +1 6%", "暴击率 2.4%"},
	})
	if len(issues) != 0 {
		t.Fatalf("issues = %+v, want none", issues)
	}
	structured := ToStructured(rec, RarityS, issues)
	if structured.Data.SetID == nil || *structured.Data.SetID != 31100 {
		t.Fatalf("setId = %v, want 31100", structured.Data.SetID)
	}
	if structured.Data.Rarity == nil || *structured.Data.Rarity != RarityS {
		t.Fatalf("rarity = %v, want S", structured.Data.Rarity)
	}
	if structured.Data.ManagementState != nil || structured.Data.Equipped != nil {
		t.Fatalf("management/equipped = %v/%v, want both null",
			structured.Data.ManagementState, structured.Data.Equipped)
	}
	if structured.Validation.Status != ValidationUnavailable {
		t.Fatalf("validation = %+v, want unavailable", structured.Validation)
	}
	if len(structured.Issues) != 0 {
		t.Fatalf("structured issues = %+v, want empty", structured.Issues)
	}
	for _, issue := range structured.Validation.Issues {
		if issue.Severity != SeverityError {
			t.Fatalf("validation issue %+v is not an error", issue)
		}
	}

	raw, err := json.Marshal(structured)
	if err != nil {
		t.Fatalf("marshal structured: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("structured is not a JSON object: %v", err)
	}
	data, ok := doc["data"].(map[string]any)
	if !ok {
		t.Fatalf("data = %v, want an object", doc["data"])
	}
	if data["setId"].(float64) != 31100 {
		t.Fatalf("data.setId = %v, want the numeric fairy id 31100", data["setId"])
	}
	if data["rarity"] != RarityS {
		t.Fatalf("data.rarity = %v, want S", data["rarity"])
	}
	if data["slot"].(float64) != 1 || data["level"].(float64) != 15 {
		t.Fatalf("slot/level = %v/%v, want 1/15", data["slot"], data["level"])
	}
	main, ok := data["mainStat"].(map[string]any)
	if !ok || main["attribute"] != "health" || main["value"] != "2200" || main["unit"] != "flat" {
		t.Fatalf("data.mainStat = %v", data["mainStat"])
	}
	subs, ok := data["substats"].([]any)
	if !ok || len(subs) != 4 {
		t.Fatalf("data.substats = %v, want four entries", data["substats"])
	}
	first := subs[0].(map[string]any)
	if first["attribute"] != "critDamage" || first["value"] != "19.2" ||
		first["unit"] != "percent" || first["enhancementRolls"].(float64) != 3 {
		t.Fatalf("data.substats[0] = %v", first)
	}
	second := subs[1].(map[string]any)
	if second["enhancementRolls"].(float64) != 0 {
		t.Fatalf("data.substats[1] = %v, want no-roll 0", second)
	}
	if data["managementState"] != nil || data["equipped"] != nil {
		t.Fatalf("data.managementState/equipped = %v/%v, want null",
			data["managementState"], data["equipped"])
	}
	if _, ok := doc["issues"].([]any); !ok {
		t.Fatalf("issues = %v, want an array", doc["issues"])
	}
	validation := doc["validation"].(map[string]any)
	if validation["status"] != ValidationUnavailable {
		t.Fatalf("validation = %v, want unavailable", validation)
	}
	if vissues := validation["issues"].([]any); len(vissues) != 2 {
		t.Fatalf("validation issues = %v, want the two data gaps", vissues)
	}
}

// TestToStructuredLevelZeroThreeSubstats covers the real level-0 shape:
// level 0 is a known value (not null), and three visible substats stay
// three entries in screen order.
func TestToStructuredLevelZeroThreeSubstats(t *testing.T) {
	rec, issues := ParseDetail(DetailLines{
		Set:   "云岿如我[6]",
		Level: "等级0/15",
		Main:  "异常掌控 7.5%",
		Subs:  []string{"暴击伤害 4.8%", "攻击力 3%", "暴击率 2.4%"},
	})
	if len(issues) != 0 {
		t.Fatalf("issues = %+v, want none", issues)
	}
	structured := ToStructured(rec, RarityS, issues)
	if structured.Data.Level == nil || *structured.Data.Level != 0 {
		t.Fatalf("level = %v, want known 0", structured.Data.Level)
	}
	if len(structured.Data.Substats) != 3 {
		t.Fatalf("substats = %d, want 3", len(structured.Data.Substats))
	}
	if structured.Data.SetID == nil || *structured.Data.SetID != 33100 {
		t.Fatalf("setId = %v, want 33100", structured.Data.SetID)
	}
	for i, want := range []Attribute{AttrCritDamage, AttrAttack, AttrCritRate} {
		if got := structured.Data.Substats[i].Attribute; got == nil || *got != string(want) {
			t.Fatalf("substats[%d].attribute = %v, want %s", i, got, want)
		}
	}
}

// TestToStructuredUnknownPartsStayNull covers unknown set, unread
// substat line and unread main stat: each stays an explicit null (or an
// object of nulls for a present line) with error-severity issues, and
// validation fails instead of pretending to pass.
func TestToStructuredUnknownPartsStayNull(t *testing.T) {
	rec, issues := ParseDetail(DetailLines{
		Set:   "无名的套装[1]",
		Level: "等级15/15",
		Main:  "元素精通 92",
		Subs:  []string{"", "攻击力 9%"},
	})
	structured := ToStructured(rec, RarityS, issues)
	if structured.Data.SetID != nil {
		t.Fatalf("setId = %v, want null", structured.Data.SetID)
	}
	if structured.Data.MainStat != nil {
		t.Fatalf("mainStat = %+v, want null", structured.Data.MainStat)
	}
	if len(structured.Data.Substats) != 2 {
		t.Fatalf("substats = %d, want the two visible lines kept", len(structured.Data.Substats))
	}
	unread := structured.Data.Substats[0]
	if unread.Attribute != nil || unread.Value != nil || unread.Unit != nil || unread.EnhancementRolls != nil {
		t.Fatalf("unread substat = %+v, want all nulls", unread)
	}
	if structured.Data.Substats[1].Attribute == nil || *structured.Data.Substats[1].Attribute != "attack" {
		t.Fatalf("readable substat = %+v", structured.Data.Substats[1])
	}
	if !hasIssue(structured.Issues, "setId", CodeUnknownSet) ||
		!hasIssue(structured.Issues, "mainStat", CodeAffixNameUnknown) ||
		!hasIssue(structured.Issues, "substats[0]", CodeLineEmpty) {
		t.Fatalf("issues = %+v, want the unknown-set/main/unread-substat errors", structured.Issues)
	}
	for _, issue := range structured.Issues {
		if issue.Severity != SeverityError {
			t.Fatalf("issue %+v is not an error", issue)
		}
	}
	if structured.Validation.Status != ValidationFailed {
		t.Fatalf("validation = %+v, want failed", structured.Validation)
	}
	if len(structured.Validation.Issues) == 0 {
		t.Fatal("failed validation carries no issues")
	}
}

// TestToStructuredSubstatCountUnknownVsEmpty pins the difference between
// an unknown line count (null) and a confirmed empty list ([]).
func TestToStructuredSubstatCountUnknownVsEmpty(t *testing.T) {
	unknown, _ := ParseDetail(DetailLines{
		Set: "河豚电音[1]", Level: "等级15/15", Main: "生命值 2200",
	})
	if unknown.Substats != nil {
		t.Fatalf("unknown substat count = %#v, want nil", unknown.Substats)
	}
	unknownData := ToStructured(unknown, RarityS, nil)
	raw, err := json.Marshal(unknownData.Data)
	if err != nil {
		t.Fatalf("marshal unknown data: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unknown data is not JSON: %v", err)
	}
	if doc["substats"] != nil {
		t.Fatalf("unknown substats = %v, want null", doc["substats"])
	}

	empty, _ := ParseDetail(DetailLines{
		Set: "河豚电音[1]", Level: "等级15/15", Main: "生命值 2200", Subs: []string{},
	})
	if empty.Substats == nil || len(empty.Substats) != 0 {
		t.Fatalf("confirmed-empty substats = %#v, want an empty non-nil slice", empty.Substats)
	}
	emptyData := ToStructured(empty, RarityS, nil)
	raw, err = json.Marshal(emptyData.Data)
	if err != nil {
		t.Fatalf("marshal empty data: %v", err)
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("empty data is not JSON: %v", err)
	}
	if subs, ok := doc["substats"].([]any); !ok || len(subs) != 0 {
		t.Fatalf("confirmed-empty substats = %v, want []", doc["substats"])
	}
}

// TestToStructuredRarityUnverified keeps a missing capture verdict as
// null and records an error instead of guessing from the set name.
func TestToStructuredRarityUnverified(t *testing.T) {
	rec, issues := ParseDetail(DetailLines{
		Set:   "河豚电音[1]",
		Level: "等级15/15",
		Main:  "生命值 2200",
		Subs:  []string{"攻击力 9%"},
	})
	structured := ToStructured(rec, "", issues)
	if structured.Data.Rarity != nil {
		t.Fatalf("rarity = %v, want null", structured.Data.Rarity)
	}
	if !hasIssue(structured.Issues, "rarity", CodeRarityUnverified) {
		t.Fatalf("issues = %+v, want rarityUnverified", structured.Issues)
	}
	if structured.Validation.Status != ValidationFailed {
		t.Fatalf("validation = %+v, want failed", structured.Validation)
	}
}

// TestToStructuredWithCaptureState pins the new non-text fields: confirmed
// management state and equipment presence are written, agentId stays null,
// equipped records remain unavailable until the equipper identity exists,
// and unequipped records can pass once their management state is known.
func TestToStructuredWithCaptureState(t *testing.T) {
	rec, issues := ParseDetail(DetailLines{
		Set:   "河豚电音[1]",
		Level: "等级15/15",
		Main:  "生命值 2200",
		Subs:  []string{"暴击伤害 +3 19.2%", "穿透值 9", "攻击力 +1 6%", "暴击率 2.4%"},
	})
	structured := ToStructuredWithState(rec, RarityS, CaptureState{
		ManagementState: ManagementLocked,
		EquippedState:   EquippedStateEquipped,
	}, issues)
	if structured.Data.ManagementState == nil || *structured.Data.ManagementState != ManagementLocked {
		t.Fatalf("managementState = %v, want locked", structured.Data.ManagementState)
	}
	if structured.Data.Equipped == nil || structured.Data.Equipped.State != EquippedStateEquipped {
		t.Fatalf("equipped = %+v, want equipped", structured.Data.Equipped)
	}
	if structured.Data.Equipped.AgentID != nil {
		t.Fatalf("equipped.agentId = %v, want null", structured.Data.Equipped.AgentID)
	}
	if structured.Validation.Status != ValidationUnavailable {
		t.Fatalf("validation = %+v, want unavailable", structured.Validation)
	}
	if len(structured.Validation.Issues) != 1 ||
		structured.Validation.Issues[0].Field != "equipped" {
		t.Fatalf("validation issues = %+v, want only the equipper gap", structured.Validation.Issues)
	}

	unequipped := ToStructuredWithState(rec, RarityS, CaptureState{
		ManagementState: ManagementDiscarded,
		EquippedState:   EquippedStateUnequipped,
	}, issues)
	if unequipped.Data.ManagementState == nil || *unequipped.Data.ManagementState != ManagementDiscarded {
		t.Fatalf("managementState = %v, want discarded", unequipped.Data.ManagementState)
	}
	if unequipped.Data.Equipped == nil || unequipped.Data.Equipped.State != EquippedStateUnequipped ||
		unequipped.Data.Equipped.AgentID != nil {
		t.Fatalf("equipped = %+v, want unequipped with agentId null", unequipped.Data.Equipped)
	}
	if unequipped.Validation.Status != ValidationPassed {
		t.Fatalf("validation = %+v, want passed", unequipped.Validation)
	}
	if len(unequipped.Validation.Issues) != 0 {
		t.Fatalf("unequipped validation issues = %+v, want none", unequipped.Validation.Issues)
	}

	raw, err := json.Marshal(unequipped.Data)
	if err != nil {
		t.Fatalf("marshal data: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("data is not JSON: %v", err)
	}
	equipped := doc["equipped"].(map[string]any)
	if equipped["state"] != EquippedStateUnequipped || equipped["agentId"] != nil {
		t.Fatalf("data.equipped = %v, want unequipped/null", equipped)
	}

	legacy := ToStructured(rec, RarityS, issues)
	if legacy.Data.ManagementState != nil || legacy.Data.Equipped != nil {
		t.Fatalf("legacy structured data = %+v, want null fields", legacy.Data)
	}
	if len(legacy.Validation.Issues) != 2 {
		t.Fatalf("legacy validation issues = %+v, want the two data gaps", legacy.Validation.Issues)
	}

	invalid := ToStructuredWithState(rec, RarityS, CaptureState{
		ManagementState: "unknown-value",
		EquippedState:   "maybe",
	}, issues)
	if invalid.Data.ManagementState != nil || invalid.Data.Equipped != nil {
		t.Fatalf("invalid state = %+v, want null fields", invalid.Data)
	}
	if invalid.Validation.Status != ValidationUnavailable {
		t.Fatalf("validation = %+v, want unavailable", invalid.Validation)
	}
}

// TestAgentIdentityGatesValidation pins the identity rules: a reliable,
// catalog-resolvable agent id completes an equipped record's validation; a
// missing or stale id keeps agentId null and validation unavailable; an
// unequipped record never carries an id, even if one is supplied.
func TestAgentIdentityGatesValidation(t *testing.T) {
	rec, issues := ParseDetail(DetailLines{
		Set:   "河豚电音[1]",
		Level: "等级15/15",
		Main:  "生命值 2200",
		Subs:  []string{"暴击伤害 +3 19.2%", "穿透值 9", "攻击力 +1 6%", "暴击率 2.4%"},
	})
	if len(issues) != 0 {
		t.Fatalf("issues = %+v, want none", issues)
	}
	cases := []struct {
		name       string
		state      CaptureState
		wantID     *int
		wantStatus string
	}{
		{
			name:       "equipped with a catalog id passes",
			state:      CaptureState{ManagementState: ManagementNone, EquippedState: EquippedStateEquipped, AgentID: 1311},
			wantID:     intPtr(1311),
			wantStatus: ValidationPassed,
		},
		{
			name:       "equipped without an id stays unavailable",
			state:      CaptureState{ManagementState: ManagementNone, EquippedState: EquippedStateEquipped},
			wantStatus: ValidationUnavailable,
		},
		{
			name:       "equipped with an unknown id stays unavailable",
			state:      CaptureState{ManagementState: ManagementNone, EquippedState: EquippedStateEquipped, AgentID: 9999},
			wantStatus: ValidationUnavailable,
		},
		{
			name:       "unequipped ignores a supplied id",
			state:      CaptureState{ManagementState: ManagementDiscarded, EquippedState: EquippedStateUnequipped, AgentID: 1311},
			wantStatus: ValidationPassed,
		},
		{
			name:       "a known id without management evidence stays unavailable",
			state:      CaptureState{EquippedState: EquippedStateEquipped, AgentID: 1311},
			wantID:     intPtr(1311),
			wantStatus: ValidationUnavailable,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			structured := ToStructuredWithState(rec, RarityS, tc.state, issues)
			got := structured.Data.Equipped
			if got == nil {
				t.Fatalf("equipped = nil, want a state object")
			}
			if tc.wantID == nil {
				if got.AgentID != nil {
					t.Fatalf("agentId = %d, want null", *got.AgentID)
				}
			} else if got.AgentID == nil || *got.AgentID != *tc.wantID {
				t.Fatalf("agentId = %v, want %d", got.AgentID, *tc.wantID)
			}
			if structured.Validation.Status != tc.wantStatus {
				t.Fatalf("validation = %+v, want %s", structured.Validation, tc.wantStatus)
			}
			if tc.wantStatus == ValidationPassed && len(structured.Validation.Issues) != 0 {
				t.Fatalf("passed validation carries issues: %+v", structured.Validation.Issues)
			}
		})
	}

	// The public shape keeps the id numeric and null when unknown.
	raw, err := json.Marshal(ToStructuredWithState(rec, RarityS,
		CaptureState{ManagementState: ManagementNone, EquippedState: EquippedStateEquipped, AgentID: 1311}, issues))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var doc struct {
		Data struct {
			Equipped struct {
				AgentID *int `json:"agentId"`
			} `json:"equipped"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if doc.Data.Equipped.AgentID == nil || *doc.Data.Equipped.AgentID != 1311 {
		t.Fatalf("persisted agentId = %v, want 1311", doc.Data.Equipped.AgentID)
	}
}

func intPtr(v int) *int { return &v }
