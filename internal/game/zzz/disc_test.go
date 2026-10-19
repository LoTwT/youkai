package zzz

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The positive tests drive the parser with the six-disc fixture of
// internal/recognition/testdata: its lines.tsv carries the human-
// transcribed expected text of every crop (file, source sha256, crop
// rectangle, expected text), which is exactly the seven-line detail page
// input this parser consumes. The macOS cloud-client group
// (internal/recognition/testdata/macos-cloud, 27 discs from the
// 2026-10-19 sample round) replays the same pipeline with real cloud-video
// screen text, including the verified slot-5/6 main-stat names, level-0
// discs and 3-substat detail pages.

// fixtureLine is one row of the recognition fixture.
type fixtureLine struct {
	file     string
	expected string
}

// readFixtureManifest parses one recognition fixture manifest (file,
// source sha256, x0, y0, x1, y1, expected text) and keeps the file name
// and expected text per row.
func readFixtureManifest(t *testing.T, path string) []fixtureLine {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading recognition fixture %s: %v", path, err)
	}
	var lines []fixtureLine
	for i, row := range strings.Split(string(raw), "\n") {
		row = strings.TrimSuffix(row, "\r")
		if row == "" || strings.HasPrefix(row, "#") {
			continue
		}
		fields := strings.Split(row, "\t")
		if len(fields) != 7 {
			t.Fatalf("%s row %d has %d fields, want 7", path, i+1, len(fields))
		}
		lines = append(lines, fixtureLine{file: fields[0], expected: fields[6]})
	}
	return lines
}

// loadFixtureLines reads ../recognition/testdata/lines.tsv and keeps the
// file name and expected text per row.
func loadFixtureLines(t *testing.T) []fixtureLine {
	t.Helper()
	lines := readFixtureManifest(t, filepath.Join("..", "..", "recognition", "testdata", "lines.tsv"))
	if len(lines) != 42 {
		t.Fatalf("lines.tsv has %d rows, want 42 (6 discs × 7 lines)", len(lines))
	}
	return lines
}

// fixtureDiscs groups the fixture rows into detail pages, keyed by the
// disc prefix of the file names, preserving the manifest's row order
// within each disc.
func fixtureDiscs(t *testing.T) map[string]DetailLines {
	t.Helper()
	discs := make(map[string]DetailLines)
	for _, line := range loadFixtureLines(t) {
		disc, role, ok := strings.Cut(strings.TrimSuffix(line.file, ".png"), "_")
		if !ok {
			t.Fatalf("fixture file %q is not <disc>_<role>.png", line.file)
		}
		page := discs[disc]
		switch role {
		case "name":
			page.Set = line.expected
		case "level":
			page.Level = line.expected
		case "main":
			page.Main = line.expected
		case "sub1", "sub2", "sub3", "sub4":
			page.Subs = append(page.Subs, line.expected)
		default:
			t.Fatalf("fixture file %q has unknown role %q", line.file, role)
		}
		discs[disc] = page
	}
	if len(discs) != 6 {
		t.Fatalf("fixture has %d discs, want 6", len(discs))
	}
	for disc, page := range discs {
		if page.Set == "" || page.Level == "" || page.Main == "" || len(page.Subs) != 4 {
			t.Fatalf("disc %s is incomplete: %+v", disc, page)
		}
	}
	return discs
}

// rolls builds an expected EnhancementRolls value.
func rolls(n int) *int { return &n }

// setID resolves an internal catalog key to the numeric fairy id expected
// in records and structured data.
func setID(key string) *int {
	// Read the table directly: test-package variables such as sampleWant
	// initialize before the package's init builds the lookup maps.
	for _, set := range sets {
		if set.Key == key {
			id := set.ID
			return &id
		}
	}
	panic("unknown set key in test table: " + key)
}

// wantDisc describes one expected complete record.
func wantDisc(set string, slot int, main Stat, subs []Substat) DiscRecord {
	level := 15
	return DiscRecord{
		SetID:    setID(set),
		Slot:     slot,
		Level:    &level,
		MainStat: main,
		Substats: subs,
	}
}

// sampleWant is the expected record of each fixture disc, transcribed
// from testdata/README.md and lines.tsv.
var sampleWant = map[string]DiscRecord{
	"disc1": wantDisc("pufferElectro", 1,
		Stat{AttrHealth, "2200", UnitFlat},
		[]Substat{
			{Stat{AttrCritDamage, "19.2", UnitPercent}, rolls(3)},
			{Stat{AttrPenetration, "9", UnitFlat}, rolls(0)},
			{Stat{AttrAttack, "6", UnitPercent}, rolls(1)},
			{Stat{AttrCritRate, "2.4", UnitPercent}, rolls(0)},
		}),
	"disc2": wantDisc("shadowHarmony", 2,
		Stat{AttrAttack, "316", UnitFlat},
		[]Substat{
			{Stat{AttrCritDamage, "14.4", UnitPercent}, rolls(2)},
			{Stat{AttrAttack, "9", UnitPercent}, rolls(2)},
			{Stat{AttrPenetration, "9", UnitFlat}, rolls(0)},
			{Stat{AttrCritRate, "4.8", UnitPercent}, rolls(1)},
		}),
	"disc3": wantDisc("shadowHarmony", 3,
		Stat{AttrDefense, "184", UnitFlat},
		[]Substat{
			{Stat{AttrCritDamage, "14.4", UnitPercent}, rolls(2)},
			{Stat{AttrHealth, "3", UnitPercent}, rolls(0)},
			{Stat{AttrAttack, "6", UnitPercent}, rolls(1)},
			{Stat{AttrCritRate, "7.2", UnitPercent}, rolls(2)},
		}),
	"disc4": wantDisc("shadowHarmony", 4,
		Stat{AttrCritRate, "24", UnitPercent},
		[]Substat{
			{Stat{AttrDefense, "30", UnitFlat}, rolls(1)},
			{Stat{AttrHealth, "112", UnitFlat}, rolls(0)},
			{Stat{AttrAttack, "9", UnitPercent}, rolls(2)},
			{Stat{AttrCritDamage, "14.4", UnitPercent}, rolls(2)},
		}),
	"disc5": wantDisc("shadowHarmony", 5,
		Stat{AttrPenetrationRatio, "24", UnitPercent},
		[]Substat{
			{Stat{AttrCritRate, "2.4", UnitPercent}, rolls(0)},
			{Stat{AttrAttack, "3", UnitPercent}, rolls(0)},
			{Stat{AttrCritDamage, "19.2", UnitPercent}, rolls(3)},
			{Stat{AttrPenetration, "18", UnitFlat}, rolls(1)},
		}),
	"disc6": wantDisc("pufferElectro", 6,
		Stat{AttrAttack, "30", UnitPercent},
		[]Substat{
			{Stat{AttrAnomalyProficiency, "9", UnitFlat}, rolls(0)},
			{Stat{AttrCritRate, "7.2", UnitPercent}, rolls(2)},
			{Stat{AttrCritDamage, "14.4", UnitPercent}, rolls(2)},
			{Stat{AttrHealth, "3", UnitPercent}, rolls(0)},
		}),
}

func TestParseDetailFixtureDiscs(t *testing.T) {
	discs := fixtureDiscs(t)
	for disc := range sampleWant {
		t.Run(disc, func(t *testing.T) {
			rec, issues := ParseDetail(discs[disc])
			if len(issues) != 0 {
				t.Fatalf("fixture disc %s produced issues, want none: %+v", disc, issues)
			}
			if !reflect.DeepEqual(rec, sampleWant[disc]) {
				t.Fatalf("record = %+v, want %+v", rec, sampleWant[disc])
			}
		})
	}
}

// TestParseDetailSpacelessVariant feeds every fixture disc with all ASCII
// spaces removed from its lines: the recognition model emits the same
// characters without the inter-column gap, so the spaceless form must
// parse to the identical record (architecture field-parsing rules).
func TestParseDetailSpacelessVariant(t *testing.T) {
	discs := fixtureDiscs(t)
	for disc, page := range discs {
		spaceless := DetailLines{
			Set:   strings.ReplaceAll(page.Set, " ", ""),
			Level: strings.ReplaceAll(page.Level, " ", ""),
			Main:  strings.ReplaceAll(page.Main, " ", ""),
		}
		for _, sub := range page.Subs {
			spaceless.Subs = append(spaceless.Subs, strings.ReplaceAll(sub, " ", ""))
		}
		rec, issues := ParseDetail(spaceless)
		if len(issues) != 0 {
			t.Fatalf("spaceless %s produced issues, want none: %+v", disc, issues)
		}
		if !reflect.DeepEqual(rec, sampleWant[disc]) {
			t.Fatalf("spaceless %s record = %+v, want %+v", disc, rec, sampleWant[disc])
		}
	}

	// The documented fused form of "暴击伤害 +3 19.2%": the roll digit is
	// exactly one character, so "+319.2%" still splits as roll 3 and the
	// value 19.2%.
	rec, issues := ParseDetail(DetailLines{
		Set:   "河豚电音[1]",
		Level: "等级15/15",
		Main:  "生命值2200",
		Subs:  []string{"暴击伤害+319.2%", "穿透值9", "攻击力+16%", "暴击率2.4%"},
	})
	if len(issues) != 0 {
		t.Fatalf("fused substat lines produced issues, want none: %+v", issues)
	}
	want := sampleWant["disc1"]
	want.Substats[2] = Substat{Stat{AttrAttack, "6", UnitPercent}, rolls(1)}
	if !reflect.DeepEqual(rec, want) {
		t.Fatalf("fused record = %+v, want %+v", rec, want)
	}
}

func TestCatalog(t *testing.T) {
	if len(sets) != 30 {
		t.Fatalf("catalog has %d sets, want 30", len(sets))
	}
	if len(agents) != 60 {
		t.Fatalf("catalog has %d agents, want 60", len(agents))
	}
	seenSets := map[int]bool{}
	seenSetKeys := map[string]bool{}
	seenAgents := map[int]bool{}
	seenAgentKeys := map[string]bool{}
	for _, s := range sets {
		if s.ID == 0 {
			t.Fatalf("set %q has no numeric fairy id", s.Key)
		}
		if seenSets[s.ID] {
			t.Fatalf("duplicate set id %d", s.ID)
		}
		seenSets[s.ID] = true
		if seenSetKeys[s.Key] {
			t.Fatalf("duplicate set key %q", s.Key)
		}
		seenSetKeys[s.Key] = true
		if got, ok := LookupSetByID(s.ID); !ok || got != s {
			t.Fatalf("set id %d resolves to %+v, want %+v", s.ID, got, s)
		}
		if got, ok := LookupSetByKey(s.Key); !ok || got != s {
			t.Fatalf("set key %q resolves to %+v, want %+v", s.Key, got, s)
		}
		got, ok := LookupSetByName(s.Name)
		if !ok || got != s {
			t.Fatalf("set name %q resolves to %+v", s.Name, got)
		}
	}
	for _, a := range agents {
		if a.ID == 0 {
			t.Fatalf("agent %q has no numeric fairy id", a.Key)
		}
		if seenAgents[a.ID] {
			t.Fatalf("duplicate agent id %d", a.ID)
		}
		seenAgents[a.ID] = true
		if seenAgentKeys[a.Key] {
			t.Fatalf("duplicate agent key %q", a.Key)
		}
		seenAgentKeys[a.Key] = true
		if got, ok := LookupAgentByID(a.ID); !ok || got != a {
			t.Fatalf("agent id %d resolves to %+v, want %+v", a.ID, got, a)
		}
		if got, ok := LookupAgentByKey(a.Key); !ok || got != a {
			t.Fatalf("agent key %q resolves to %+v, want %+v", a.Key, got, a)
		}
		if got, ok := LookupAgentByName(a.Name); !ok || got != a {
			t.Fatalf("agent name %q resolves to %+v", a.Name, got)
		}
	}

	// Spot checks against the sources: the fixture sets and the doc's
	// example identifier. The numeric fairy id is the public identity; the
	// readable key is an internal alias.
	if s, _ := LookupSetByName("河豚电音"); s.ID != 31100 || s.Key != "pufferElectro" {
		t.Fatalf("河豚电音 = %+v", s)
	}
	if s, _ := LookupSetByName("如影相随"); s.ID != 32900 || s.Key != "shadowHarmony" {
		t.Fatalf("如影相随 = %+v", s)
	}
	if s, _ := LookupSetByID(31000); s.Key != "woodpeckerElectro" || s.Name != "啄木鸟电音" {
		t.Fatalf("31000 = %+v", s)
	}
	if a, _ := LookupAgentByName("耀嘉音"); a.ID != 1311 || a.Key != "astraYao" {
		t.Fatalf("耀嘉音 = %+v", a)
	}
	if a, _ := LookupAgentByID(1311); a.Name != "耀嘉音" {
		t.Fatalf("agent 1311 = %+v", a)
	}
	if _, ok := LookupSetByName("不存在的套装"); ok {
		t.Fatal("unknown set name resolved")
	}
	if _, ok := LookupSetByID(99999); ok {
		t.Fatal("unknown set id resolved")
	}
	if _, ok := LookupAgentByID(9999); ok {
		t.Fatal("unknown agent id resolved")
	}
}

// mainAndSubs builds a minimal valid detail page around a main stat, for
// the validation-focused cases.
func mainAndSubs(main string, subs ...string) DetailLines {
	return DetailLines{Set: "河豚电音[1]", Level: "等级15/15", Main: main, Subs: subs}
}

// hasIssue reports whether the issues contain the field and code.
func hasIssue(issues []Issue, field, code string) bool {
	for _, issue := range issues {
		if issue.Field == field && issue.Code == code {
			return true
		}
	}
	return false
}

func TestParseDetailNegativeCases(t *testing.T) {
	cases := []struct {
		name   string
		lines  DetailLines
		fields []string // field, code pairs the issues must contain
	}{
		{
			name:  "roll out of range +6",
			lines: mainAndSubs("生命值 2200", "暴击率 +6 4.8%", "攻击力 9%", "防御力 30", "穿透值 9"),
			fields: []string{
				"substats[0]", CodeRollOutOfRange,
			},
		},
		{
			name:  "roll out of range +0",
			lines: mainAndSubs("生命值 2200", "攻击力 +0 3%", "攻击力 9%", "防御力 30", "穿透值 9"),
			fields: []string{
				"substats[0]", CodeRollOutOfRange,
			},
		},
		{
			name:  "unknown affix name",
			lines: mainAndSubs("生命值 2200", "元素伤害加成 30%", "攻击力 9%", "防御力 30", "穿透值 9"),
			fields: []string{
				"substats[0]", CodeAffixNameUnknown,
			},
		},
		{
			name:  "substat unit does not match type",
			lines: mainAndSubs("生命值 2200", "暴击率 24", "攻击力 9%", "防御力 30", "穿透值 9"),
			fields: []string{
				"substats[0]", CodeSubstatNotAllowed,
			},
		},
		{
			name:  "substat percent-only attribute read flat",
			lines: mainAndSubs("生命值 2200", "穿透值 9%", "攻击力 9%", "防御力 30", "暴击率 2.4%"),
			fields: []string{
				"substats[0]", CodeSubstatNotAllowed,
			},
		},
		{
			name:  "level above 15",
			lines: DetailLines{Set: "河豚电音[1]", Level: "等级16/15", Main: "生命值 2200", Subs: []string{"攻击力 9%"}},
			fields: []string{
				"level", CodeLevelOutOfRange,
			},
		},
		{
			name:  "level denominator not 15",
			lines: DetailLines{Set: "河豚电音[1]", Level: "等级15/20", Main: "生命值 2200", Subs: []string{"攻击力 9%"}},
			fields: []string{
				"level", CodeLevelDenominatorInvalid,
			},
		},
		{
			name:  "slot main combination illegal",
			lines: DetailLines{Set: "河豚电音[2]", Level: "等级15/15", Main: "生命值 2200", Subs: []string{"攻击力 9%"}},
			fields: []string{
				"mainStat", CodeMainStatNotAllowed,
			},
		},
		{
			name:  "slot out of range",
			lines: DetailLines{Set: "河豚电音[7]", Level: "等级15/15", Main: "生命值 2200", Subs: []string{"攻击力 9%"}},
			fields: []string{
				"slot", CodeSlotOutOfRange,
			},
		},
		{
			name:  "duplicate substat",
			lines: mainAndSubs("生命值 2200", "暴击率 2.4%", "暴击率 4.8%", "防御力 30", "穿透值 9"),
			fields: []string{
				"substats[1]", CodeSubstatDuplicated,
			},
		},
		{
			name:  "more than four substats",
			lines: mainAndSubs("生命值 2200", "暴击率 2.4%", "攻击力 9%", "防御力 30", "穿透值 9", "异常精通 9"),
			fields: []string{
				"substats", CodeTooManySubstats,
			},
		},
		{
			name:  "main stat must not carry a roll",
			lines: mainAndSubs("生命值 +2 2200", "攻击力 9%", "防御力 30", "穿透值 9"),
			fields: []string{
				"mainStat", CodeUnexpectedRoll,
			},
		},
		{
			name:  "unread substat line",
			lines: mainAndSubs("生命值 2200", "", "攻击力 9%", "防御力 30", "穿透值 9"),
			fields: []string{
				"substats[0]", CodeLineEmpty,
			},
		},
		{
			name:  "unknown set",
			lines: DetailLines{Set: "无名的套装[4]", Level: "等级15/15", Main: "暴击率 24%", Subs: []string{"攻击力 9%"}},
			fields: []string{
				"setId", CodeUnknownSet,
			},
		},
		{
			name:  "name line without slot bracket",
			lines: DetailLines{Set: "河豚电音", Level: "等级15/15", Main: "生命值 2200", Subs: []string{"攻击力 9%"}},
			fields: []string{
				"", CodeNameLineUnparsed,
			},
		},
		{
			name:  "level line malformed",
			lines: DetailLines{Set: "河豚电音[1]", Level: "等级15", Main: "生命值 2200", Subs: []string{"攻击力 9%"}},
			fields: []string{
				"level", CodeLevelLineUnparsed,
			},
		},
		{
			name:  "value not a plain decimal",
			lines: mainAndSubs("生命值 2200", "攻击力 -9%", "防御力 30", "穿透值 9", "暴击率 2.4%"),
			fields: []string{
				"substats[0]", CodeAffixValueInvalid,
			},
		},
		{
			name:  "plus without a digit",
			lines: mainAndSubs("生命值 2200", "攻击力 + 9%", "防御力 30", "穿透值 9", "暴击率 2.4%"),
			fields: []string{
				"substats[0]", CodeAffixValueInvalid,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec, issues := ParseDetail(tc.lines)
			for i := 0; i+1 < len(tc.fields); i += 2 {
				if !hasIssue(issues, tc.fields[i], tc.fields[i+1]) {
					t.Fatalf("issues %+v miss %s/%s (record %+v)", issues, tc.fields[i], tc.fields[i+1], rec)
				}
			}
			for _, issue := range issues {
				if issue.Severity != SeverityError {
					t.Fatalf("parser issue %+v is not an error", issue)
				}
			}
		})
	}
}

// TestParseDetailUnknownPartsStayUnknown checks that failed lines leave
// the record's fields unknown instead of picking a nearby legal value,
// and that an unread substat line still keeps its (unknown) entry.
func TestParseDetailUnknownPartsStayUnknown(t *testing.T) {
	rec, issues := ParseDetail(DetailLines{
		Set:   "无名的套装[1]",
		Level: "等级16/15",
		Main:  "元素精通 92",
		Subs:  []string{"暴击率 +6 4.8%", "攻击力 9%", "攻击力 9%", "防御力 30"},
	})
	for _, want := range [][2]string{
		{"setId", CodeUnknownSet},
		{"level", CodeLevelOutOfRange},
		{"mainStat", CodeAffixNameUnknown},
		{"substats[0]", CodeRollOutOfRange},
		{"substats[2]", CodeSubstatDuplicated},
	} {
		if !hasIssue(issues, want[0], want[1]) {
			t.Fatalf("issues %+v miss %s/%s", issues, want[0], want[1])
		}
	}
	if rec.SetID != nil {
		t.Fatalf("set id = %d, want nil", *rec.SetID)
	}
	if rec.Slot != 1 {
		t.Fatalf("slot = %d, want 1 (the slot itself was readable)", rec.Slot)
	}
	if rec.Level != nil {
		t.Fatalf("level = %v, want nil", *rec.Level)
	}
	if rec.MainStat != (Stat{}) {
		t.Fatalf("main stat = %+v, want unknown", rec.MainStat)
	}
	if len(rec.Substats) != 4 {
		t.Fatalf("substats = %d entries, want 4 (unread lines keep their entry)", len(rec.Substats))
	}
	if rec.Substats[0] != (Substat{}) || rec.Substats[0].EnhancementRolls != nil {
		t.Fatalf("unread substat = %+v, want an unknown entry with nil rolls", rec.Substats[0])
	}
	if rec.Substats[1].Attribute != AttrAttack || rec.Substats[1].Value != "9" ||
		rec.Substats[1].Unit != UnitPercent || rec.Substats[1].EnhancementRolls == nil || *rec.Substats[1].EnhancementRolls != 0 {
		t.Fatalf("readable substat = %+v, want attack 9 percent with roll 0", rec.Substats[1])
	}
}

// TestParseDetailSlot5Slot6MainStatNames covers the slot-5 and slot-6
// display names the Windows sample does not show: the six element damage
// bonuses and the three slot-6 attributes, first supplied by the user on
// 2026-10-18 and verified name-by-name against real client screens in the
// 2026-10-19 macOS sample round (see affixes.go); the macOS fixture below
// replays the same names as real screen text.
func TestParseDetailSlot5Slot6MainStatNames(t *testing.T) {
	cases := []struct {
		slot int
		line string
		want Stat
	}{
		{5, "物理伤害加成 30%", Stat{AttrPhysicalDamage, "30", UnitPercent}},
		{5, "火属性伤害加成 30%", Stat{AttrFireDamage, "30", UnitPercent}},
		{5, "冰属性伤害加成 30%", Stat{AttrIceDamage, "30", UnitPercent}},
		{5, "电属性伤害加成 30%", Stat{AttrElectricDamage, "30", UnitPercent}},
		{5, "以太伤害加成 30%", Stat{AttrEtherDamage, "30", UnitPercent}},
		{5, "风属性伤害加成 30%", Stat{AttrWindDamage, "30", UnitPercent}},
		{6, "异常掌控 30%", Stat{AttrAnomalyMastery, "30", UnitPercent}},
		{6, "冲击力 18%", Stat{AttrImpact, "18", UnitPercent}},
		{6, "能量自动回复 60%", Stat{AttrEnergyRegen, "60", UnitPercent}},
	}
	for _, tc := range cases {
		t.Run(tc.line, func(t *testing.T) {
			set := fmt.Sprintf("如影相随[%d]", tc.slot)
			rec, issues := ParseDetail(DetailLines{
				Set:   set,
				Level: "等级15/15",
				Main:  tc.line,
				Subs:  []string{"攻击力 9%", "防御力 30", "穿透值 9", "暴击率 2.4%"},
			})
			if len(issues) != 0 {
				t.Fatalf("issues = %+v, want none", issues)
			}
			if rec.MainStat != tc.want {
				t.Fatalf("main stat = %+v, want %+v", rec.MainStat, tc.want)
			}
		})
	}

	// An element damage bonus is not a legal slot-4 main stat, and a
	// slot-6-only attribute is not one of the ten substats.
	if _, issues := ParseDetail(DetailLines{
		Set: "如影相随[4]", Level: "等级15/15", Main: "以太伤害加成 30%",
		Subs: []string{"攻击力 9%", "防御力 30", "穿透值 9", "暴击率 2.4%"},
	}); !hasIssue(issues, "mainStat", CodeMainStatNotAllowed) {
		t.Fatalf("issues = %+v, want mainStatNotAllowed", issues)
	}
	if _, issues := ParseDetail(DetailLines{
		Set: "如影相随[4]", Level: "等级15/15", Main: "暴击率 24%",
		Subs: []string{"异常掌控 30%", "防御力 30", "穿透值 9", "暴击率 2.4%"},
	}); !hasIssue(issues, "substats[0]", CodeSubstatNotAllowed) {
		t.Fatalf("issues = %+v, want substatNotAllowed", issues)
	}
}

// TestCanonicalDecimal pins the value grammar: the canonical form of the
// scan JSON contract, with trailing fraction zeros canonicalized away.
func TestCanonicalDecimal(t *testing.T) {
	ok := map[string]string{
		"0":     "0",
		"2200":  "2200",
		"19.2":  "19.2",
		"9.60":  "9.6",
		"30.0":  "30",
		"0.50":  "0.5",
		"0.0":   "0",
		"112":   "112",
		"14.40": "14.4",
	}
	for in, want := range ok {
		got, valid := canonicalDecimal(in)
		if !valid || got != want {
			t.Fatalf("canonicalDecimal(%q) = %q,%v; want %q,true", in, got, valid, want)
		}
	}
	for _, in := range []string{"", "05", "019.2", "-3", "+3", "19.", ".5", "1e3", "1,920", "19 2", "3.4.5", "19.2%"} {
		if got, valid := canonicalDecimal(in); valid {
			t.Fatalf("canonicalDecimal(%q) = %q,true; want invalid", in, got)
		}
	}
}

// TestParseDetailLevelZero covers the level grammar's lower end: level 0
// is valid, so unknown must be represented by nil, not by zero.
func TestParseDetailLevelZero(t *testing.T) {
	rec, issues := ParseDetail(DetailLines{
		Set:   "河豚电音[4]",
		Level: "等级0/15",
		Main:  "暴击率 24%",
		Subs:  []string{"攻击力 9%"},
	})
	if len(issues) != 0 {
		t.Fatalf("issues = %+v, want none", issues)
	}
	if rec.Level == nil || *rec.Level != 0 {
		t.Fatalf("level = %v, want 0", rec.Level)
	}
}

// loadMacCloudLines reads the macOS cloud-client fixture manifest of
// internal/recognition/testdata/macos-cloud (same seven-column shape as
// the Windows group; crop rectangles refer to the 1920×1080 rawstream
// frame — see that directory's README).
func loadMacCloudLines(t *testing.T) []fixtureLine {
	t.Helper()
	lines := readFixtureManifest(t, filepath.Join("..", "..", "recognition", "testdata", "macos-cloud", "lines.tsv"))
	if len(lines) != 183 {
		t.Fatalf("macos-cloud lines.tsv has %d rows, want 183 (21 discs × 7 lines + 6 discs × 6 lines)", len(lines))
	}
	return lines
}

// macCloudDiscs groups the macOS fixture rows into detail pages. A level-0
// disc may show three or four substat lines on the real screen, so pages
// are complete with either count (the sample round caught both shapes:
// 3-substat s2-n6-AM-cell-r0c1 and s2-d1-discarded-r0c1, 4-substat
// s2-n5-WIND2-cell-r0c1 and s2-wBL-top-cell-r1c1).
func macCloudDiscs(t *testing.T) map[string]DetailLines {
	t.Helper()
	discs := make(map[string]DetailLines)
	for _, line := range loadMacCloudLines(t) {
		disc, role, ok := strings.Cut(strings.TrimSuffix(line.file, ".png"), "_")
		if !ok {
			t.Fatalf("fixture file %q is not <disc>_<role>.png", line.file)
		}
		page := discs[disc]
		switch role {
		case "name":
			page.Set = line.expected
		case "level":
			page.Level = line.expected
		case "main":
			page.Main = line.expected
		case "sub1", "sub2", "sub3", "sub4":
			page.Subs = append(page.Subs, line.expected)
		default:
			t.Fatalf("fixture file %q has unknown role %q", line.file, role)
		}
		discs[disc] = page
	}
	if len(discs) != 27 {
		t.Fatalf("macos-cloud fixture has %d discs, want 27", len(discs))
	}
	for disc, page := range discs {
		if page.Set == "" || page.Level == "" || page.Main == "" ||
			(len(page.Subs) != 3 && len(page.Subs) != 4) {
			t.Fatalf("disc %s is incomplete: %+v", disc, page)
		}
	}
	return discs
}

// discWant builds one expected record for the macOS fixture table below.
func discWant(set string, slot, level int, main Stat, subs ...Substat) DiscRecord {
	return DiscRecord{
		SetID:    setID(set),
		Slot:     slot,
		Level:    &level,
		MainStat: main,
		Substats: subs,
	}
}

// subWant builds one expected substat entry: attribute, canonical value,
// unit and the displayed enhancement roll (0 for a line without "+N").
func subWant(a Attribute, v string, u Unit, roll int) Substat {
	return Substat{Stat{a, v, u}, &roll}
}

// macCloudWant is the expected record of every macOS fixture disc,
// transcribed from testdata/macos-cloud/README.md and lines.tsv (the
// 2026-10-19 sample round; per-disc evidence directories are listed in
// that README).
var macCloudWant = map[string]DiscRecord{
	"maccloud1":  discWant("yunkuiTales", 1, 15, Stat{AttrHealth, "2200", UnitFlat}, subWant(AttrAnomalyProficiency, "9", UnitFlat, 0), subWant(AttrCritRate, "12", UnitPercent, 4), subWant(AttrDefense, "4.8", UnitPercent, 0), subWant(AttrCritDamage, "9.6", UnitPercent, 1)),
	"maccloud2":  discWant("phaethonSMelody", 1, 0, Stat{AttrHealth, "550", UnitFlat}, subWant(AttrHealth, "3", UnitPercent, 0), subWant(AttrPenetration, "9", UnitFlat, 0), subWant(AttrAttack, "3", UnitPercent, 0)),
	"maccloud3":  discWant("phaethonSMelody", 1, 0, Stat{AttrHealth, "550", UnitFlat}, subWant(AttrHealth, "3", UnitPercent, 0), subWant(AttrAttack, "19", UnitFlat, 0), subWant(AttrAnomalyProficiency, "9", UnitFlat, 0)),
	"maccloud4":  discWant("moonlightLullaby", 1, 15, Stat{AttrHealth, "2200", UnitFlat}, subWant(AttrAttack, "9", UnitPercent, 2), subWant(AttrAnomalyProficiency, "27", UnitFlat, 2), subWant(AttrCritDamage, "4.8", UnitPercent, 0), subWant(AttrAttack, "19", UnitFlat, 0)),
	"maccloud5":  discWant("chaosJazz", 1, 15, Stat{AttrHealth, "2200", UnitFlat}, subWant(AttrDefense, "30", UnitFlat, 1), subWant(AttrAnomalyProficiency, "27", UnitFlat, 2), subWant(AttrHealth, "3", UnitPercent, 0), subWant(AttrAttack, "9", UnitPercent, 2)),
	"maccloud6":  discWant("woodpeckerElectro", 1, 15, Stat{AttrHealth, "2200", UnitFlat}, subWant(AttrAnomalyProficiency, "18", UnitFlat, 1), subWant(AttrPenetration, "18", UnitFlat, 1), subWant(AttrAttack, "9", UnitPercent, 2), subWant(AttrCritRate, "4.8", UnitPercent, 1)),
	"maccloud7":  discWant("branchBladeSong", 2, 15, Stat{AttrAttack, "316", UnitFlat}, subWant(AttrCritRate, "7.2", UnitPercent, 2), subWant(AttrDefense, "4.8", UnitPercent, 0), subWant(AttrCritDamage, "19.2", UnitPercent, 3), subWant(AttrDefense, "15", UnitFlat, 0)),
	"maccloud8":  discWant("yunkuiTales", 2, 15, Stat{AttrAttack, "316", UnitFlat}, subWant(AttrPenetration, "18", UnitFlat, 1), subWant(AttrHealth, "6", UnitPercent, 1), subWant(AttrCritDamage, "9.6", UnitPercent, 1), subWant(AttrCritRate, "7.2", UnitPercent, 2)),
	"maccloud9":  discWant("yunkuiTales", 3, 15, Stat{AttrDefense, "184", UnitFlat}, subWant(AttrCritDamage, "14.4", UnitPercent, 2), subWant(AttrHealth, "12", UnitPercent, 3), subWant(AttrAnomalyProficiency, "9", UnitFlat, 0), subWant(AttrCritRate, "2.4", UnitPercent, 0)),
	"maccloud10": discWant("yunkuiTales", 3, 0, Stat{AttrDefense, "46", UnitFlat}, subWant(AttrAnomalyProficiency, "9", UnitFlat, 0), subWant(AttrHealth, "3", UnitPercent, 0), subWant(AttrDefense, "4.8", UnitPercent, 0), subWant(AttrHealth, "112", UnitFlat, 0)),
	"maccloud11": discWant("yunkuiTales", 4, 15, Stat{AttrCritDamage, "48", UnitPercent}, subWant(AttrHealth, "15", UnitPercent, 4), subWant(AttrHealth, "112", UnitFlat, 0), subWant(AttrPenetration, "9", UnitFlat, 0), subWant(AttrCritRate, "2.4", UnitPercent, 0)),
	"maccloud12": discWant("yunkuiTales", 4, 15, Stat{AttrCritRate, "24", UnitPercent}, subWant(AttrCritDamage, "9.6", UnitPercent, 1), subWant(AttrHealth, "3", UnitPercent, 0), subWant(AttrPenetration, "27", UnitFlat, 2), subWant(AttrDefense, "30", UnitFlat, 1)),
	"maccloud13": discWant("yunkuiTales", 4, 0, Stat{AttrCritDamage, "12", UnitPercent}, subWant(AttrHealth, "112", UnitFlat, 0), subWant(AttrPenetration, "9", UnitFlat, 0), subWant(AttrHealth, "3", UnitPercent, 0)),
	"maccloud14": discWant("yunkuiTales", 4, 0, Stat{AttrDefense, "12", UnitPercent}, subWant(AttrCritRate, "2.4", UnitPercent, 0), subWant(AttrCritDamage, "4.8", UnitPercent, 0), subWant(AttrHealth, "3", UnitPercent, 0)),
	"maccloud15": discWant("branchBladeSong", 5, 15, Stat{AttrEtherDamage, "30", UnitPercent}, subWant(AttrAnomalyProficiency, "9", UnitFlat, 0), subWant(AttrCritRate, "2.4", UnitPercent, 0), subWant(AttrHealth, "3", UnitPercent, 0), subWant(AttrCritDamage, "24", UnitPercent, 4)),
	"maccloud16": discWant("yunkuiTales", 5, 0, Stat{AttrAttack, "7.5", UnitPercent}, subWant(AttrCritRate, "2.4", UnitPercent, 0), subWant(AttrCritDamage, "4.8", UnitPercent, 0), subWant(AttrHealth, "3", UnitPercent, 0)),
	"maccloud17": discWant("yunkuiTales", 6, 15, Stat{AttrHealth, "30", UnitPercent}, subWant(AttrAttack, "9", UnitPercent, 2), subWant(AttrPenetration, "9", UnitFlat, 0), subWant(AttrCritRate, "9.6", UnitPercent, 3), subWant(AttrCritDamage, "4.8", UnitPercent, 0)),
	"maccloud18": discWant("yunkuiTales", 6, 15, Stat{AttrImpact, "18", UnitPercent}, subWant(AttrDefense, "45", UnitFlat, 2), subWant(AttrPenetration, "18", UnitFlat, 1), subWant(AttrAttack, "6", UnitPercent, 1), subWant(AttrHealth, "6", UnitPercent, 1)),
	"maccloud19": discWant("yunkuiTales", 5, 15, Stat{AttrPhysicalDamage, "30", UnitPercent}, subWant(AttrAnomalyProficiency, "9", UnitFlat, 0), subWant(AttrCritDamage, "4.8", UnitPercent, 0), subWant(AttrCritRate, "9.6", UnitPercent, 3), subWant(AttrAttack, "38", UnitFlat, 1)),
	"maccloud20": discWant("yunkuiTales", 5, 15, Stat{AttrFireDamage, "30", UnitPercent}, subWant(AttrCritDamage, "4.8", UnitPercent, 0), subWant(AttrHealth, "6", UnitPercent, 1), subWant(AttrCritRate, "7.2", UnitPercent, 2), subWant(AttrAttack, "6", UnitPercent, 1)),
	"maccloud21": discWant("yunkuiTales", 5, 15, Stat{AttrIceDamage, "30", UnitPercent}, subWant(AttrCritRate, "7.2", UnitPercent, 2), subWant(AttrDefense, "4.8", UnitPercent, 0), subWant(AttrCritDamage, "4.8", UnitPercent, 0), subWant(AttrHealth, "9", UnitPercent, 2)),
	"maccloud22": discWant("branchBladeSong", 5, 15, Stat{AttrElectricDamage, "30", UnitPercent}, subWant(AttrHealth, "112", UnitFlat, 0), subWant(AttrCritRate, "4.8", UnitPercent, 1), subWant(AttrCritDamage, "14.4", UnitPercent, 2), subWant(AttrAttack, "38", UnitFlat, 1)),
	"maccloud23": discWant("phaethonSMelody", 5, 15, Stat{AttrEtherDamage, "30", UnitPercent}, subWant(AttrAttack, "3", UnitPercent, 0), subWant(AttrCritRate, "7.2", UnitPercent, 2), subWant(AttrCritDamage, "9.6", UnitPercent, 1), subWant(AttrHealth, "6", UnitPercent, 1)),
	"maccloud24": discWant("yunkuiTales", 5, 0, Stat{AttrWindDamage, "7.5", UnitPercent}, subWant(AttrCritRate, "2.4", UnitPercent, 0), subWant(AttrHealth, "112", UnitFlat, 0), subWant(AttrHealth, "3", UnitPercent, 0), subWant(AttrAttack, "3", UnitPercent, 0)),
	"maccloud25": discWant("yunkuiTales", 6, 0, Stat{AttrAnomalyMastery, "7.5", UnitPercent}, subWant(AttrCritDamage, "4.8", UnitPercent, 0), subWant(AttrAttack, "3", UnitPercent, 0), subWant(AttrCritRate, "2.4", UnitPercent, 0)),
	"maccloud26": discWant("yunkuiTales", 6, 15, Stat{AttrImpact, "18", UnitPercent}, subWant(AttrCritRate, "7.2", UnitPercent, 2), subWant(AttrAttack, "6", UnitPercent, 1), subWant(AttrPenetration, "18", UnitFlat, 1), subWant(AttrCritDamage, "9.6", UnitPercent, 1)),
	"maccloud27": discWant("yunkuiTales", 6, 15, Stat{AttrEnergyRegen, "60", UnitPercent}, subWant(AttrHealth, "224", UnitFlat, 1), subWant(AttrHealth, "9", UnitPercent, 2), subWant(AttrPenetration, "18", UnitFlat, 1), subWant(AttrCritRate, "4.8", UnitPercent, 1)),
}

// TestParseDetailMacCloudFixture parses every disc of the macOS
// cloud-client fixture: real screen text at level 0 and 15, three- and
// four-substat pages, the verified slot-5/6 main-stat names (including
// 以太伤害加成 without 属性) and percent mains that share a display name
// with flat ones. The spaceless variant re-parses each page without ASCII
// spaces, as the recognition model emits the lines.
func TestParseDetailMacCloudFixture(t *testing.T) {
	discs := macCloudDiscs(t)
	if len(discs) != len(macCloudWant) {
		t.Fatalf("fixture has %d discs, expected records for %d", len(discs), len(macCloudWant))
	}
	for discName := range macCloudWant {
		if _, ok := discs[discName]; !ok {
			t.Fatalf("expected record for %s has no fixture disc", discName)
		}
	}
	for discName, page := range discs {
		t.Run(discName, func(t *testing.T) {
			rec, issues := ParseDetail(page)
			if len(issues) != 0 {
				t.Fatalf("macos disc %s produced issues, want none: %+v", discName, issues)
			}
			if !reflect.DeepEqual(rec, macCloudWant[discName]) {
				t.Fatalf("record = %+v, want %+v", rec, macCloudWant[discName])
			}
			spaceless := DetailLines{
				Set:   strings.ReplaceAll(page.Set, " ", ""),
				Level: strings.ReplaceAll(page.Level, " ", ""),
				Main:  strings.ReplaceAll(page.Main, " ", ""),
			}
			for _, s := range page.Subs {
				spaceless.Subs = append(spaceless.Subs, strings.ReplaceAll(s, " ", ""))
			}
			rec, issues = ParseDetail(spaceless)
			if len(issues) != 0 {
				t.Fatalf("spaceless %s produced issues, want none: %+v", discName, issues)
			}
			if !reflect.DeepEqual(rec, macCloudWant[discName]) {
				t.Fatalf("spaceless record = %+v, want %+v", rec, macCloudWant[discName])
			}
		})
	}
}

// TestParseDetailLevelZeroThreeSubstats pins the real level-0 shape the
// round evidenced: a detail page may show exactly three substat lines
// (s2-n6-AM-cell-r0c1, also s2-d1-discarded-r0c1/r0c2), while other
// level-0 discs show four (s2-n5-WIND2-cell-r0c1, maccloud24) — parsing
// and validation must not assume a fixed count; the JSON contract allows
// three or four.
func TestParseDetailLevelZeroThreeSubstats(t *testing.T) {
	rec, issues := ParseDetail(DetailLines{
		Set:   "云岿如我[6]",
		Level: "等级0/15",
		Main:  "异常掌控 7.5%",
		Subs:  []string{"暴击伤害 4.8%", "攻击力 3%", "暴击率 2.4%"},
	})
	if len(issues) != 0 {
		t.Fatalf("issues = %+v, want none", issues)
	}
	if rec.Level == nil || *rec.Level != 0 {
		t.Fatalf("level = %v, want 0", rec.Level)
	}
	if len(rec.Substats) != 3 {
		t.Fatalf("substats = %d entries, want 3", len(rec.Substats))
	}
	if rec.MainStat != (Stat{AttrAnomalyMastery, "7.5", UnitPercent}) {
		t.Fatalf("main stat = %+v", rec.MainStat)
	}
}
