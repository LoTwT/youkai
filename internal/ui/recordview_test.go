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

// evidenceRow builds one produced row of sequence 1 from the observed
// payload, the recognized lines and the evidence ids the record kept.
func evidenceRow(t *testing.T, observed []byte, lines []app.RecognizedLine, ids ...string) Row {
	t.Helper()
	it := producedItem(1, mustJSON(t, app.RecognitionResult{Lines: lines}), observed)
	it.EvidenceIDs = ids
	return buildView(it).Rows[0]
}

// evidenceIDs lists the row's evidence ids in display order.
func evidenceIDs(row Row) []string {
	ids := make([]string, len(row.Evidence))
	for i, ev := range row.Evidence {
		ids[i] = ev.ID
	}
	return ids
}

// TestEvidencePairsByIDNotPosition checks R-07a's pairing: the kind and index
// decoded from an evidence id decide which observed line or context region an
// image backs, never the image's position in the record. A skipped empty crop
// and a failed write each leave their own slot without an image, and the ids
// may be stored in any order.
func TestEvidencePairsByIDNotPosition(t *testing.T) {
	observed := mustJSON(t, map[string]any{
		"lines": []map[string]string{
			{"role": "name"}, {"role": "level"}, {"role": "main"}, {"role": "sub1"},
		},
		"context": []map[string]string{
			{"role": "qualityBadge"}, {"role": "selectedCell"},
			{"role": "detailAvatar"}, {"role": "listAvatar"},
		},
	})
	// line1 (empty crop) and context0 (failed write) were never written;
	// the remaining ids are deliberately out of order.
	row := evidenceRow(t, observed, []app.RecognizedLine{
		{Role: "name", Text: "河豚电音[4]", Score: 0.99},
		{Role: "level", Text: "等级15/15", Score: 0.98},
		{Role: "main", Text: "生命值 2200", Score: 0.97},
		{Role: "sub1", Text: "暴击伤害 +3 19.2%", Score: 0.96},
	},
		"000001-line3", "000001-context2", "000001-line0",
		"000001-context1", "000001-line2", "000001-context3")

	wantIDs := []string{
		"000001-line0", "000001-line2", "000001-line3",
		"000001-context1", "000001-context2", "000001-context3",
	}
	if got := strings.Join(evidenceIDs(row), ","); got != strings.Join(wantIDs, ",") {
		t.Fatalf("evidence order = %v, want capture order %v", evidenceIDs(row), wantIDs)
	}

	line0 := row.LineEvidence(0)
	if line0 == nil || line0.ID != "000001-line0" || line0.Role != "name" {
		t.Fatalf("line 0 evidence = %+v, want its own image with role name", line0)
	}
	if line1 := row.LineEvidence(1); line1 != nil {
		t.Fatalf("line 1 evidence = %+v, want none after the skipped crop", line1)
	}
	line2 := row.LineEvidence(2)
	if line2 == nil || line2.ID != "000001-line2" || line2.Role != "main" {
		t.Fatalf("line 2 evidence = %+v, want its own image with role main", line2)
	}
	line3 := row.LineEvidence(3)
	if line3 == nil || line3.ID != "000001-line3" || line3.Role != "sub1" {
		t.Fatalf("line 3 evidence = %+v, want its own image with role sub1", line3)
	}

	contextRoles := map[int]string{1: "selectedCell", 2: "detailAvatar", 3: "listAvatar"}
	for _, ev := range row.Evidence {
		if ev.Kind != EvidenceContext {
			continue
		}
		if want, ok := contextRoles[ev.Index]; !ok || ev.Role != want {
			t.Errorf("context %d evidence = %+v, want role %q", ev.Index, ev, want)
		}
		delete(contextRoles, ev.Index)
	}
	if len(contextRoles) != 0 {
		t.Errorf("context evidence missing for indexes %v", contextRoles)
	}
}

// TestEvidenceWithoutRolesStillPairs checks the R-07a fallback for a record
// whose observed metadata carries no roles: images still pair by the id index
// and stay in capture order, and no form field can be located from them.
func TestEvidenceWithoutRolesStillPairs(t *testing.T) {
	row := evidenceRow(t, []byte(`{"lines":[{},{"role":""}]}`),
		[]app.RecognizedLine{{Text: "河豚电音[4]"}, {Text: "等级15/15"}},
		"000001-line1", "000001-line0")

	for i, want := range []string{"000001-line0", "000001-line1"} {
		ev := row.LineEvidence(i)
		if ev == nil || ev.ID != want {
			t.Fatalf("line %d evidence = %+v, want %q", i, ev, want)
		}
		if ev.Role != "" {
			t.Errorf("line %d role = %q, want an empty role", i, ev.Role)
		}
		if _, ok := FormFieldForRole(ev.Role); ok {
			t.Errorf("line %d empty role located a form field", i)
		}
	}
}

// TestLegacyEvidencePairsLinesOnly checks a record written before context
// crops existed and one without any observed metadata: line ids still pair
// with their own lines, and no context evidence is invented.
func TestLegacyEvidencePairsLinesOnly(t *testing.T) {
	lines := []app.RecognizedLine{
		{Role: "name", Text: "河豚电音[4]", Score: 0.99},
		{Role: "level", Text: "等级15/15", Score: 0.98},
	}
	t.Run("observed lines only", func(t *testing.T) {
		row := evidenceRow(t, []byte(`{"lines":[{"role":"name"},{"role":"level"}]}`),
			lines, "000001-line1", "000001-line0")
		if got := strings.Join(evidenceIDs(row), ","); got != "000001-line0,000001-line1" {
			t.Fatalf("evidence = %v, want only the two line crops in order", evidenceIDs(row))
		}
		if ev := row.LineEvidence(0); ev == nil || ev.Role != "name" {
			t.Errorf("line 0 evidence = %+v, want role name", ev)
		}
		if ev := row.LineEvidence(1); ev == nil || ev.Role != "level" {
			t.Errorf("line 1 evidence = %+v, want role level", ev)
		}
	})
	t.Run("no observed payload", func(t *testing.T) {
		row := evidenceRow(t, nil, lines, "000001-line1", "000001-line0")
		if got := strings.Join(evidenceIDs(row), ","); got != "000001-line0,000001-line1" {
			t.Fatalf("evidence = %v, want only the two line crops in order", evidenceIDs(row))
		}
		for i := range lines {
			ev := row.LineEvidence(i)
			if ev == nil || ev.Role != "" {
				t.Errorf("line %d evidence = %+v, want an image without a role", i, ev)
			}
		}
	})
}

// TestEvidenceRejectsForeignAndMalformedIDs checks that an id with another
// item's sequence, a malformed suffix or a non-decimal sequence pairs with
// nothing: it stays visible at the end without a role.
func TestEvidenceRejectsForeignAndMalformedIDs(t *testing.T) {
	row := evidenceRow(t, []byte(`{"lines":[{"role":"name"}]}`),
		[]app.RecognizedLine{{Role: "name", Text: "河豚电音[4]", Score: 0.99}},
		"000001-line0", "foo", "000001-line", "000001-linex", "000002-line0", "+1-line0", "-line0")

	if got := len(row.Evidence); got != 7 {
		t.Fatalf("evidence = %d, want the valid id and the six foreign ones", got)
	}
	first := row.Evidence[0]
	if first.ID != "000001-line0" || first.Kind != EvidenceLine || first.Role != "name" {
		t.Fatalf("first evidence = %+v, want the item's own line 0", first)
	}
	foreign := []string{"foo", "000001-line", "000001-linex", "000002-line0", "+1-line0", "-line0"}
	for i, want := range foreign {
		ev := row.Evidence[i+1]
		if ev.ID != want {
			t.Errorf("evidence %d = %q, want %q", i+1, ev.ID, want)
		}
		if ev.Kind != "" || ev.Role != "" {
			t.Errorf("%q = %+v, want an unrecognized image without a role", want, ev)
		}
	}
	if ev := row.LineEvidence(0); ev == nil || ev.ID != "000001-line0" {
		t.Errorf("line 0 evidence = %+v, want the valid id", ev)
	}
}

// TestEvidenceDeduplicatesRepeatedIDs checks that a repeated id keeps its
// first occurrence, so one image cannot render twice.
func TestEvidenceDeduplicatesRepeatedIDs(t *testing.T) {
	row := evidenceRow(t, []byte(`{"lines":[{"role":"name"},{"role":"level"}]}`),
		[]app.RecognizedLine{{Role: "name", Text: "河豚电音[4]"}, {Role: "level", Text: "等级15/15"}},
		"000001-line1", "000001-line0", "000001-line1", "000001-line0")

	if got := strings.Join(evidenceIDs(row), ","); got != "000001-line0,000001-line1" {
		t.Fatalf("evidence = %q, want each id once in capture order", got)
	}
}

// TestFormFieldEvidenceMapping locks the R-07a role table: every recognized
// line role locates its form field, both equipped-agent avatar crops locate
// the equipper, and the management state has no screenshot.
func TestFormFieldEvidenceMapping(t *testing.T) {
	roles := []struct {
		role  string
		field FormField
		ok    bool
	}{
		{"name", FormFieldSetID, true},
		{"level", FormFieldLevel, true},
		{"main", FormFieldMainStat, true},
		{"sub1", FormFieldSubstat1, true},
		{"sub2", FormFieldSubstat2, true},
		{"sub3", FormFieldSubstat3, true},
		{"sub4", FormFieldSubstat4, true},
		{"detailAvatar", FormFieldEquipped, true},
		{"listAvatar", FormFieldEquipped, true},
		{"qualityBadge", "", false},
		{"selectedCell", "", false},
		{"", "", false},
	}
	for _, tc := range roles {
		field, ok := FormFieldForRole(tc.role)
		if field != tc.field || ok != tc.ok {
			t.Errorf("FormFieldForRole(%q) = (%q, %v), want (%q, %v)",
				tc.role, field, ok, tc.field, tc.ok)
		}
	}

	fields := []struct {
		field FormField
		roles string
	}{
		{FormFieldSetID, "name"},
		{FormFieldLevel, "level"},
		{FormFieldMainStat, "main"},
		{FormFieldSubstat1, "sub1"},
		{FormFieldSubstat2, "sub2"},
		{FormFieldSubstat3, "sub3"},
		{FormFieldSubstat4, "sub4"},
		{FormFieldEquipped, "detailAvatar,listAvatar"},
		{FormFieldManagementState, ""},
	}
	for _, tc := range fields {
		got := strings.Join(EvidenceRolesForField(tc.field), ",")
		if got != tc.roles {
			t.Errorf("EvidenceRolesForField(%q) = %q, want %q", tc.field, got, tc.roles)
		}
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
	if got := row.IssueSummaryText(); got != "2 个问题（1 个错误，1 个警告）" {
		t.Errorf("issue summary = %q", got)
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
		name  string
		state *string
		want  string
	}{
		{"null", nil, "未读取"},
		{"none", strp("none"), "无标记"},
		{"locked", strp("locked"), "锁定"},
		{"discarded", strp("discarded"), "弃置"},
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
	if got := view.Rows[0].Summary(); got != "识别失败：识别超时" {
		t.Errorf("failed summary = %q", got)
	}

	if view.Rows[1].Structured != nil {
		t.Error("the unprocessed item showed a structured payload")
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
