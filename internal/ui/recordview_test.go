package ui

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/game/zzz"
	"github.com/LoTwT/youkai/internal/record"
)

// strp returns a pointer to v for the structured payload's nullable
// string fields.
func strp(v string) *string { return &v }

// mustJSON marshals v or fails the test.
func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal test payload: %v", err)
	}
	return raw
}

// producedItem builds one produced item from a result payload and its
// observed payload.
func producedItem(seq int64, data, observed []byte) record.ItemRecord {
	return record.ItemRecord{
		Sequence: seq,
		Status:   record.StatusProduced,
		Quality:  record.QualityNotEvaluated,
		Data:     data,
		Observed: observed,
	}
}

// buildView flattens one record of the items.
func buildView(items ...record.ItemRecord) *RecordView {
	return BuildRecordView("r1", &record.Recovery{
		Checkpoint: record.Checkpoint{Items: items},
	}, false)
}

// fullStructuredDisc is a complete structured payload with every display
// field populated.
func fullStructuredDisc() *zzz.StructuredDisc {
	return &zzz.StructuredDisc{
		Data: zzz.DiscData{
			SetID:  intp(31100),
			Rarity: strp(zzz.RarityS),
			Slot:   intp(4),
			Level:  intp(12),
			MainStat: &zzz.StatData{
				Attribute: strp(string(zzz.AttrCritRate)),
				Value:     strp("19.2"),
				Unit:      strp(string(zzz.UnitPercent)),
			},
			Substats: []zzz.SubstatData{
				{
					StatData: zzz.StatData{
						Attribute: strp(string(zzz.AttrCritDamage)),
						Value:     strp("19.2"),
						Unit:      strp(string(zzz.UnitPercent)),
					},
					EnhancementRolls: intp(3),
				},
				{
					StatData: zzz.StatData{
						Attribute: strp(string(zzz.AttrPenetration)),
						Value:     strp("9"),
						Unit:      strp(string(zzz.UnitFlat)),
					},
					EnhancementRolls: intp(0),
				},
				{
					StatData: zzz.StatData{
						Attribute: strp(string(zzz.AttrAttack)),
						Value:     strp("6"),
						Unit:      strp(string(zzz.UnitPercent)),
					},
				},
			},
			ManagementState: strp("none"),
			Equipped: &zzz.Equipped{
				State:   "equipped",
				AgentID: intp(1311),
			},
		},
		Issues: []zzz.Issue{{
			Field: "substats[2]", Code: "rollOutOfRange", Severity: zzz.SeverityWarning,
			Message: "强化档位无法读取",
		}},
		Validation: zzz.Validation{
			Status: zzz.ValidationUnavailable,
			Issues: []zzz.Issue{{
				Field: "managementState", Code: zzz.CodeManagementStateUnavailable,
				Severity: zzz.SeverityError, Message: "管理状态未识别",
			}},
		},
	}
}

// TestBuildRecordViewParsesStructured checks a current record's persisted
// structured payload: names resolve from the catalog, the numeric id stays
// visible, and every field renders through the display methods.
func TestBuildRecordViewParsesStructured(t *testing.T) {
	structured := fullStructuredDisc()
	data := mustJSON(t, app.RecognitionResult{
		Lines: []app.RecognizedLine{{
			Role: "name", Text: "河豚电音[4]", Score: 0.99,
		}},
		Structured: structured,
	})
	view := buildView(producedItem(1, data, []byte(`{"lines":[{"role":"name","source":"frame 1"}]}`)))
	row := view.Rows[0]

	if row.Structured == nil {
		t.Fatal("the persisted structured payload was not parsed")
	}
	if got := row.SetText(); got != "河豚电音" {
		t.Errorf("set name = %q, want 河豚电音", got)
	}
	if got := row.SetIDText(); got != "31100" {
		t.Errorf("set id = %q, want 31100", got)
	}
	if got := row.SetFormText(); got != "河豚电音（31100）" {
		t.Errorf("form set = %q, want 河豚电音（31100）", got)
	}
	if got := row.RarityText(); got != "S" {
		t.Errorf("rarity = %q, want S", got)
	}
	if got := row.SlotLevelText(); got != "4 号位 · 12 级" {
		t.Errorf("slot/level = %q, want 4 号位 · 12 级", got)
	}
	if got := row.MainStatText(); got != "暴击率 · 19.2 · 百分比" {
		t.Errorf("main stat = %q", got)
	}
	subs, known := row.SubstatTexts()
	if !known {
		t.Fatal("the substat list was reported unknown")
	}
	want := []string{
		"暴击伤害 · 19.2 · 百分比 · 强化 +3",
		"穿透值 · 9 · 固定值 · 无强化档位",
		"攻击力 · 6 · 百分比 · 强化未读取",
	}
	if strings.Join(subs, "\n") != strings.Join(want, "\n") {
		t.Errorf("substats = %q, want %q", subs, want)
	}
	if got := row.ManagementStateText(); got != "无标记" {
		t.Errorf("management state = %q, want 无标记", got)
	}
	if got := row.EquippedText(); got != "耀嘉音（1311）" {
		t.Errorf("equipped = %q, want 耀嘉音（1311）", got)
	}
	if got := row.ValidationText(); got != "不可用" {
		t.Errorf("validation = %q, want 不可用", got)
	}
	if issues := row.Issues(); len(issues) != 2 {
		t.Errorf("issues = %d, want the parser issue and the validation issue", len(issues))
	}
	if got := row.IssueSummaryText(); got != "2 个问题（1 个错误）" {
		t.Errorf("issue summary = %q", got)
	}
	if got := row.ResultColumnText(); got != "未评价 · 校验不可用 · 2 个问题（1 个错误）" {
		t.Errorf("table result = %q", got)
	}
}

// TestBuildRecordViewBackfillsLegacyLines checks the 995 old-checkpoint
// path: a record with only data.lines is interpreted in memory at read
// time, showing set, slot, level, main stat and substats while the unread
// management state and equipper stay explicitly not read. The checkpoint
// payload itself is never rewritten.
func TestBuildRecordViewBackfillsLegacyLines(t *testing.T) {
	observed := mustJSON(t, map[string]any{
		"lines":   []map[string]string{{"role": "name", "source": "frame 1"}},
		"capture": map[string]string{"quality": zzz.RarityS},
	})
	data := mustJSON(t, map[string]any{
		"lines": []app.RecognizedLine{
			{Role: "name", Text: "河豚电音[4]", Score: 0.99},
			{Role: "level", Text: "等级15/15", Score: 0.98},
			{Role: "main", Text: "生命值 2200", Score: 0.97},
			{Role: "sub1", Text: "暴击伤害 +3 19.2%", Score: 0.96},
			{Role: "sub2", Text: "穿透值 9", Score: 0.95},
		},
	})
	original := append([]byte(nil), data...)
	row := buildView(producedItem(7, data, observed)).Rows[0]

	if row.Structured == nil {
		t.Fatal("the legacy raw lines were not backfilled in memory")
	}
	if got := row.SetFormText(); got != "河豚电音（31100）" {
		t.Errorf("set = %q", got)
	}
	if got := row.SlotLevelText(); got != "4 号位 · 15 级" {
		t.Errorf("slot/level = %q", got)
	}
	if got := row.MainStatText(); got != "生命值 · 2200 · 固定值" {
		t.Errorf("main stat = %q", got)
	}
	subs, known := row.SubstatTexts()
	if !known || len(subs) != 2 {
		t.Fatalf("substats = %q known=%v", subs, known)
	}
	if subs[0] != "暴击伤害 · 19.2 · 百分比 · 强化 +3" {
		t.Errorf("first substat = %q", subs[0])
	}
	if got := row.RarityText(); got != "S" {
		t.Errorf("rarity = %q, want the verified capture rarity", got)
	}
	if got := row.ManagementStateText(); got != "未读取" {
		t.Errorf("management state = %q, want 未读取", got)
	}
	if got := row.EquippedText(); got != "未读取" {
		t.Errorf("equipped = %q, want 未读取", got)
	}
	if !bytes.Equal(data, original) {
		t.Errorf("BuildRecordView rewrote the checkpoint payload: %s", data)
	}
	if bytes.Contains(data, []byte(`"structured"`)) {
		t.Error("the legacy payload gained a persisted structured field")
	}
}

// TestUnreadStatesStayDistinct checks that null managementState and null
// equipped never render as none/unequipped, while the read states keep
// their exact meaning.
func TestUnreadStatesStayDistinct(t *testing.T) {
	base := zzz.DiscData{
		SetID:  intp(31100),
		Rarity: strp(zzz.RarityS),
		Slot:   intp(1),
		Level:  intp(15),
	}
	tests := []struct {
		name       string
		state      *string
		want       string
		wantColumn string
	}{
		{"null", nil, "未读取", "未读取"},
		{"none", strp("none"), "无标记", "无标记"},
		{"locked", strp("locked"), "锁定", "锁定"},
		{"discarded", strp("discarded"), "弃置", "弃置"},
	}
	for _, tt := range tests {
		t.Run("management/"+tt.name, func(t *testing.T) {
			data := base
			data.ManagementState = tt.state
			structured := &zzz.StructuredDisc{Data: data}
			row := buildView(producedItem(1, mustJSON(t, app.RecognitionResult{
				Structured: structured,
			}), nil)).Rows[0]
			if got := row.ManagementStateText(); got != tt.want {
				t.Errorf("management = %q, want %q", got, tt.want)
			}
			if got := row.ManagementColumnText(); got != tt.wantColumn {
				t.Errorf("table management = %q, want %q", got, tt.wantColumn)
			}
		})
	}

	equippedTests := []struct {
		name     string
		equipped *zzz.Equipped
		want     string
	}{
		{"null", nil, "未读取"},
		{"unequipped", &zzz.Equipped{State: "unequipped"}, "未装备"},
		{"equipped-null-agent", &zzz.Equipped{State: "equipped"}, "已装备，代理人未知"},
		{"equipped-agent", &zzz.Equipped{State: "equipped", AgentID: intp(1311)}, "耀嘉音（1311）"},
		{"equipped-unknown-agent", &zzz.Equipped{State: "equipped", AgentID: intp(9999)}, "已装备（代理人 9999 未知）"},
	}
	for _, tt := range equippedTests {
		t.Run("equipped/"+tt.name, func(t *testing.T) {
			data := base
			data.Equipped = tt.equipped
			structured := &zzz.StructuredDisc{Data: data}
			row := buildView(producedItem(1, mustJSON(t, app.RecognitionResult{
				Structured: structured,
			}), nil)).Rows[0]
			if got := row.EquippedText(); got != tt.want {
				t.Errorf("equipped = %q, want %q", got, tt.want)
			}
			if got := row.EquippedColumnText(); got != tt.want {
				t.Errorf("table equipped = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestStructuredAbsentFieldsStayExplicit checks null ids, unknown catalog
// entries and empty structured field groups without names becoming ids.
func TestStructuredAbsentFieldsStayExplicit(t *testing.T) {
	structured := &zzz.StructuredDisc{
		Data: zzz.DiscData{
			SetID:    intp(99999),
			Rarity:   nil,
			Slot:     nil,
			Level:    nil,
			MainStat: nil,
			Substats: []zzz.SubstatData{},
		},
		Validation: zzz.Validation{Status: zzz.ValidationPassed, Issues: []zzz.Issue{}},
	}
	row := buildView(producedItem(1, mustJSON(t, app.RecognitionResult{
		Structured: structured,
	}), nil)).Rows[0]

	if got := row.SetText(); got != "未知套装（99999）" {
		t.Errorf("unknown set = %q", got)
	}
	if got := row.SetFormText(); got != "未知套装（99999）" {
		t.Errorf("unknown set form = %q", got)
	}
	if got := row.SetIDText(); got != "99999" {
		t.Errorf("unknown set id = %q", got)
	}
	if got := row.RarityText(); got != "未读取" {
		t.Errorf("rarity = %q, want 未读取", got)
	}
	if got := row.SlotLevelText(); got != "未读取 · 未读取" {
		t.Errorf("slot/level = %q", got)
	}
	if got := row.MainStatText(); got != "未读取" {
		t.Errorf("main stat = %q", got)
	}
	subs, known := row.SubstatTexts()
	if !known || len(subs) != 0 {
		t.Errorf("confirmed empty substats = %q known=%v", subs, known)
	}

	noSet := &zzz.StructuredDisc{Data: zzz.DiscData{}}
	row = buildView(producedItem(1, mustJSON(t, app.RecognitionResult{
		Structured: noSet,
	}), nil)).Rows[0]
	if got := row.SetFormText(); got != "未读取" {
		t.Errorf("null set form = %q, want 未读取", got)
	}
	if _, known := row.SubstatTexts(); known {
		t.Error("null substats were reported as a known empty list")
	}
}

// TestFailedAndUnprocessedDoNotFabricateStructured checks that a stale or
// impossible data payload on a failed/unprocessed item never becomes a
// structured form, and that a produced payload without drive-disc roles
// stays raw-only. Their failure or unprocessed reason still shows.
func TestFailedAndUnprocessedDoNotFabricateStructured(t *testing.T) {
	structured := fullStructuredDisc()
	data := mustJSON(t, app.RecognitionResult{Structured: structured})
	observed := []byte(`{"lines":[{"role":"name","source":"frame 1"}]}`)

	failed := record.ItemRecord{
		Sequence: 1, Status: record.StatusFailed, Failure: "识别超时",
		Quality: record.QualityNotEvaluated, Data: data, Observed: observed,
	}
	unprocessed := record.ItemRecord{
		Sequence: 2, Status: record.StatusUnprocessed, Reason: record.ReasonInterrupted,
		Quality: record.QualityNotEvaluated, Data: data, Observed: observed,
	}
	rawOnly := producedItem(3, mustJSON(t, app.RecognitionResult{
		Lines: []app.RecognizedLine{{Role: "context", Text: "x", Score: 1}},
	}), nil)

	view := buildView(failed, unprocessed, rawOnly)
	if view.Rows[0].Structured != nil {
		t.Error("the failed item showed a structured payload")
	}
	if got := view.Rows[0].SetColumnText(); got != "—" {
		t.Errorf("failed set cell = %q, want —", got)
	}
	if got := view.Rows[0].ResultColumnText(); got != "识别失败：识别超时" {
		t.Errorf("failed result = %q", got)
	}
	if got := view.Rows[0].Summary(); got != "识别失败：识别超时" {
		t.Errorf("failed summary = %q", got)
	}

	if view.Rows[1].Structured != nil {
		t.Error("the unprocessed item showed a structured payload")
	}
	if got := view.Rows[1].ResultColumnText(); got != "未处理（中断）" {
		t.Errorf("unprocessed result = %q", got)
	}
	if got := view.Rows[1].Summary(); got != "未处理（中断）" {
		t.Errorf("unprocessed summary = %q", got)
	}

	if view.Rows[2].Structured != nil {
		t.Error("the non-disc produced payload gained structured fields")
	}
	if got := view.Rows[2].SetColumnText(); got != "未解析" {
		t.Errorf("raw-only set cell = %q, want 未解析", got)
	}
	if got := view.Rows[2].ResultColumnText(); got != "未评价" {
		t.Errorf("raw-only result = %q, want only the quality", got)
	}
}

// TestIssueSummaryDeduplicatesValidationIssues checks that validation
// repeating a parser issue does not double the count.
func TestIssueSummaryDeduplicatesValidationIssues(t *testing.T) {
	issue := zzz.Issue{
		Field: "mainStat", Code: "affixValueInvalid",
		Severity: zzz.SeverityError, Message: "数值无效",
	}
	structured := &zzz.StructuredDisc{
		Issues: []zzz.Issue{issue},
		Validation: zzz.Validation{
			Status: zzz.ValidationFailed,
			Issues: []zzz.Issue{issue},
		},
	}
	row := buildView(producedItem(1, mustJSON(t, app.RecognitionResult{
		Structured: structured,
	}), nil)).Rows[0]

	if issues := row.Issues(); len(issues) != 1 {
		t.Errorf("issues = %d, want the duplicated issue once", len(issues))
	}
	if got := row.IssueSummaryText(); got != "1 个问题（1 个错误）" {
		t.Errorf("issue summary = %q", got)
	}
	if got := row.ValidationText(); got != "失败" {
		t.Errorf("validation = %q, want 失败", got)
	}
}
