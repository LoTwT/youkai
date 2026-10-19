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
// input this parser consumes.

// fixtureLine is one row of the recognition fixture.
type fixtureLine struct {
	file     string
	expected string
}

// loadFixtureLines reads ../recognition/testdata/lines.tsv and keeps the
// file name and expected text per row.
func loadFixtureLines(t *testing.T) []fixtureLine {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "recognition", "testdata", "lines.tsv"))
	if err != nil {
		t.Fatalf("reading recognition fixture: %v", err)
	}
	var lines []fixtureLine
	for i, row := range strings.Split(string(raw), "\n") {
		row = strings.TrimSuffix(row, "\r")
		if row == "" || strings.HasPrefix(row, "#") {
			continue
		}
		fields := strings.Split(row, "\t")
		if len(fields) != 7 {
			t.Fatalf("lines.tsv row %d has %d fields, want 7", i+1, len(fields))
		}
		lines = append(lines, fixtureLine{file: fields[0], expected: fields[6]})
	}
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

// wantDisc describes one expected complete record.
func wantDisc(set string, slot int, main Stat, subs []Substat) DiscRecord {
	level := 15
	return DiscRecord{
		SetID:    set,
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
	seenSets := map[string]bool{}
	seenAgents := map[string]bool{}
	for _, s := range sets {
		if seenSets[s.ID] {
			t.Fatalf("duplicate set id %q", s.ID)
		}
		seenSets[s.ID] = true
		if _, ok := LookupSetByID(s.ID); !ok {
			t.Fatalf("set id %q does not resolve", s.ID)
		}
		got, ok := LookupSetByName(s.Name)
		if !ok || got.ID != s.ID || got.FairyID != s.FairyID {
			t.Fatalf("set name %q resolves to %+v", s.Name, got)
		}
	}
	for _, a := range agents {
		if seenAgents[a.ID] {
			t.Fatalf("duplicate agent id %q", a.ID)
		}
		seenAgents[a.ID] = true
		if got, ok := LookupAgentByName(a.Name); !ok || got.ID != a.ID {
			t.Fatalf("agent name %q resolves to %+v", a.Name, got)
		}
	}

	// Spot checks against the sources: the fixture sets and the doc's
	// example identifier.
	if s, _ := LookupSetByName("河豚电音"); s.ID != "pufferElectro" || s.FairyID != "31100" {
		t.Fatalf("河豚电音 = %+v", s)
	}
	if s, _ := LookupSetByName("如影相随"); s.ID != "shadowHarmony" || s.FairyID != "32900" {
		t.Fatalf("如影相随 = %+v", s)
	}
	if s, _ := LookupSetByID("woodpeckerElectro"); s.FairyID != "31000" || s.Name != "啄木鸟电音" {
		t.Fatalf("woodpeckerElectro = %+v", s)
	}
	if a, _ := LookupAgentByName("耀嘉音"); a.ID != "astraYao" || a.FairyID != "1311" {
		t.Fatalf("耀嘉音 = %+v", a)
	}
	if _, ok := LookupSetByName("不存在的套装"); ok {
		t.Fatal("unknown set name resolved")
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
	if rec.SetID != "" {
		t.Fatalf("set id = %q, want empty", rec.SetID)
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

// TestParseDetailUserSuppliedNames covers the slot-5 and slot-6 display
// names the Windows sample does not show: the six element damage bonuses
// and the three slot-6 attributes, first supplied by the user on
// 2026-10-18 and verified name-by-name against real client screens in the
// 2026-10-19 macOS sample round (see affixes.go).
func TestParseDetailUserSuppliedNames(t *testing.T) {
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
