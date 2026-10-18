package zzz

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// DetailLines is the fixed text of one drive-disc detail page as the
// recognition stage reads it: the set-name line ("河豚电音[1]"), the level
// line ("等级15/15"), the main-stat line ("生命值 2200") and the substat
// lines from top to bottom ("暴击伤害 +3 19.2%"). A detail page shows at
// most four substat lines. Whether a space separates the name and value
// columns does not matter — the recognition model emits the same
// characters without the inter-column gap (see internal/recognition's
// integration test).
type DetailLines struct {
	Set   string
	Level string
	Main  string
	Subs  []string
}

// Stat is one parsed affix: the attribute identity, the displayed value
// as a canonical decimal string and the unit the value is expressed in.
// An empty attribute, value or unit means that part could not be read;
// the issue list carries the raw text that could not be used.
type Stat struct {
	Attribute Attribute
	Value     string
	Unit      Unit
}

// Substat is one substat line: a Stat plus the enhancement-roll
// observation. EnhancementRolls is the raw observation of the screen's
// "+N": the displayed N (1–5) when the line shows one, 0 when it does
// not, and nil when the line exists but could not be read. No value
// arithmetic is ever derived from it. Main stats have no such field.
type Substat struct {
	Stat
	EnhancementRolls *int
}

// DiscRecord is the structured result of parsing one detail page's
// lines: the resolved set identity, the slot, the level, the main stat
// and the substats in display order. Unknown parts stay zero (empty
// set id, slot 0, nil level, empty attribute); rarity, management
// states and the equipper are not part of these lines and belong to the
// rest of the game adapter.
type DiscRecord struct {
	// SetID is the youkai set key, or "" when the set name did not
	// resolve in the catalog.
	SetID string
	// Slot is 1–6, or 0 when unknown or out of range.
	Slot int
	// Level is 0–15, or nil when unknown.
	Level    *int
	MainStat Stat
	Substats []Substat
}

// Issue reports one parsing or validation problem of a detail page.
// Field names the record field — "" for the whole record, "substats[2]"
// for one substat line — using the scan JSON contract's data field
// names. Code is a stable diagnostic code; Message is a human-readable
// explanation that quotes the raw line text, so a failed line never
// loses its original observation. All parser issues are errors in the
// contract's terms: they keep the record out of auto-accept, and no
// issue ever substitutes a nearby legal value.
type Issue struct {
	Field   string
	Code    string
	Message string
}

// Issue codes of the detail-line parser.
const (
	// CodeNameLineUnparsed: the set-name line has no 套装名[N] shape.
	CodeNameLineUnparsed = "nameLineUnparsed"
	// CodeSlotOutOfRange: the slot read from the name line is not 1–6.
	CodeSlotOutOfRange = "slotOutOfRange"
	// CodeUnknownSet: the set name is not in the catalog.
	CodeUnknownSet = "unknownSet"
	// CodeLevelLineUnparsed: the level line has no 等级N/15 shape.
	CodeLevelLineUnparsed = "levelLineUnparsed"
	// CodeLevelDenominatorInvalid: the level denominator is not 15.
	CodeLevelDenominatorInvalid = "levelDenominatorInvalid"
	// CodeLevelOutOfRange: the level is not 0–15.
	CodeLevelOutOfRange = "levelOutOfRange"
	// CodeLineEmpty: a line exists but no text was read.
	CodeLineEmpty = "lineEmpty"
	// CodeAffixNameUnknown: no catalog display name matches the line.
	CodeAffixNameUnknown = "affixNameUnknown"
	// CodeAffixValueInvalid: the value part is not a non-negative decimal.
	CodeAffixValueInvalid = "affixValueInvalid"
	// CodeRollOutOfRange: the "+N" enhancement roll is outside 1–5.
	CodeRollOutOfRange = "rollOutOfRange"
	// CodeUnexpectedRoll: a main-stat line shows an enhancement roll.
	CodeUnexpectedRoll = "unexpectedRoll"
	// CodeMainStatNotAllowed: the slot may not carry this main affix.
	CodeMainStatNotAllowed = "mainStatNotAllowed"
	// CodeSubstatNotAllowed: the affix is not one of the ten substats.
	CodeSubstatNotAllowed = "substatNotAllowed"
	// CodeSubstatDuplicated: an earlier substat has the same attribute+unit.
	CodeSubstatDuplicated = "substatDuplicated"
	// CodeTooManySubstats: more than four substat lines were read.
	CodeTooManySubstats = "tooManySubstats"
)

// ParseDetail parses the fixed lines of one drive-disc detail page into a
// structured record plus the issues found. Parsing is table-driven with
// the auxiliary rules of the architecture's field-parsing section: the
// affix name comes from a longest-prefix match against the catalog's
// display names, a "+N" after the name with N in 1–5 is the enhancement
// roll (out-of-range rolls such as +0 or +6 are recognition anomalies,
// kept as raw text with an issue), the rest is the value and a trailing
// "%" selects the percent unit. Every cross-check — slot range, set
// resolution, level 0–15 with denominator 15, the slot×main combination,
// the ten substat kinds, at most four substats without a repeated
// attribute+unit — fails loudly with an issue instead of guessing.
func ParseDetail(lines DetailLines) (DiscRecord, []Issue) {
	var rec DiscRecord
	var issues []Issue

	// Set and slot: "河豚电音[1]". The name line feeds both fields, so a
	// structural failure of the line itself is reported as a whole-record
	// issue rather than pinned to one of them.
	name, slot, ok := splitNameLine(lines.Set)
	switch {
	case !ok && strings.TrimSpace(lines.Set) == "":
		issues = append(issues, Issue{"", CodeLineEmpty,
			"套装名行没有读出文本"})
	case !ok:
		issues = append(issues, Issue{"", CodeNameLineUnparsed,
			fmt.Sprintf("套装名行 %q 无法按 套装名[N] 拆出套装名与槽位", lines.Set)})
	default:
		if slot < 1 || slot > 6 {
			issues = append(issues, Issue{"slot", CodeSlotOutOfRange,
				fmt.Sprintf("槽位 %d 越界（合法 1–6）：%q", slot, lines.Set)})
		} else {
			rec.Slot = slot
		}
		if set, found := LookupSetByName(name); found {
			rec.SetID = set.ID
		} else {
			issues = append(issues, Issue{"setId", CodeUnknownSet,
				fmt.Sprintf("套装名 %q 不在资料表中", name)})
		}
	}

	// Level: "等级15/15".
	level, denominator, ok := splitLevelLine(lines.Level)
	switch {
	case !ok && strings.TrimSpace(lines.Level) == "":
		issues = append(issues, Issue{"level", CodeLineEmpty,
			"等级行没有读出文本"})
	case !ok:
		issues = append(issues, Issue{"level", CodeLevelLineUnparsed,
			fmt.Sprintf("等级行 %q 无法按 等级N/15 解析", lines.Level)})
	default:
		if denominator != 15 {
			issues = append(issues, Issue{"level", CodeLevelDenominatorInvalid,
				fmt.Sprintf("等级分母为 %d，应为 15：%q", denominator, lines.Level)})
		}
		if level > 15 {
			issues = append(issues, Issue{"level", CodeLevelOutOfRange,
				fmt.Sprintf("等级 %d 越界（合法 0–15）：%q", level, lines.Level)})
		}
		if denominator == 15 && level <= 15 {
			rec.Level = &level
		}
	}

	// Main stat: no enhancement-roll field exists for main stats.
	split, problem := splitAffixLine(lines.Main)
	switch {
	case problem != nil:
		issues = append(issues, Issue{"mainStat", problem.code, problem.message})
	default:
		if split.roll != nil {
			issues = append(issues, Issue{"mainStat", CodeUnexpectedRoll,
				fmt.Sprintf("主词条行不应有强化档位：%q", lines.Main)})
		}
		if rec.Slot != 0 && !mainAffixAllowed(rec.Slot, Affix{split.attribute, split.unit}) {
			issues = append(issues, Issue{"mainStat", CodeMainStatNotAllowed,
				fmt.Sprintf("槽位 %d 不允许主词条 %s+%s：%q", rec.Slot, split.attribute, split.unit, lines.Main)})
		}
		rec.MainStat = Stat{Attribute: split.attribute, Value: split.value, Unit: split.unit}
	}

	// Substats: at most four lines, each with its raw roll observation.
	subs := lines.Subs
	if len(subs) > 4 {
		issues = append(issues, Issue{"substats", CodeTooManySubstats,
			fmt.Sprintf("读出 %d 条副词条，最多四条；多余行原文：%q", len(subs), strings.Join(subs[4:], "、"))})
		subs = subs[:4]
	}
	seen := make(map[Affix]bool, len(subs))
	for i, text := range subs {
		field := fmt.Sprintf("substats[%d]", i)
		split, problem := splitAffixLine(text)
		if problem != nil {
			rec.Substats = append(rec.Substats, Substat{})
			issues = append(issues, Issue{field, problem.code, problem.message})
			continue
		}
		identity := Affix{split.attribute, split.unit}
		if !subAffixAllowed(identity) {
			issues = append(issues, Issue{field, CodeSubstatNotAllowed,
				fmt.Sprintf("副词条 %s+%s 不在十种合法类型内：%q", split.attribute, split.unit, text)})
		}
		if seen[identity] {
			issues = append(issues, Issue{field, CodeSubstatDuplicated,
				fmt.Sprintf("副词条 %s+%s 与前面的词条重复：%q", split.attribute, split.unit, text)})
		}
		seen[identity] = true
		rolls := 0
		if split.roll != nil {
			rolls = *split.roll
		}
		rec.Substats = append(rec.Substats, Substat{
			Stat:             Stat{Attribute: split.attribute, Value: split.value, Unit: split.unit},
			EnhancementRolls: &rolls,
		})
	}

	return rec, issues
}

// affixSplit is a successful split of one affix line.
type affixSplit struct {
	attribute Attribute
	// roll is the displayed enhancement roll, or nil when the line shows
	// no "+N".
	roll  *int
	value string
	unit  Unit
}

// affixProblem is a failed split: a stable code and a message quoting the
// raw line.
type affixProblem struct {
	code    string
	message string
}

// splitAffixLine splits one affix line into the attribute, the optional
// enhancement roll, the canonical value and the unit. Spaces between the
// name and the value are optional; the roll is exactly one digit in 1–5
// (the screen shows at most one digit, and the model fuses "+3" and
// "19.2%" into "+319.2%", which still resolves); anything that does not
// split or validate is reported as a problem with the raw text kept.
func splitAffixLine(text string) (affixSplit, *affixProblem) {
	line := strings.TrimSpace(text)
	if line == "" {
		return affixSplit{}, &affixProblem{CodeLineEmpty, "词条行没有读出文本"}
	}

	// Longest-prefix match against the catalog's display names.
	var def *attributeDef
	for i := range attributeTable {
		if strings.HasPrefix(line, attributeTable[i].name) &&
			(def == nil || len(attributeTable[i].name) > len(def.name)) {
			def = &attributeTable[i]
		}
	}
	if def == nil {
		return affixSplit{}, &affixProblem{CodeAffixNameUnknown,
			fmt.Sprintf("词条名无法在资料表中匹配：%q", line)}
	}

	rest := strings.TrimLeftFunc(line[len(def.name):], unicode.IsSpace)
	roll := (*int)(nil)
	if strings.HasPrefix(rest, "+") && len(rest) >= 2 && isDigit(rest[1]) {
		n := int(rest[1] - '0')
		if n < 1 || n > 5 {
			return affixSplit{}, &affixProblem{CodeRollOutOfRange,
				fmt.Sprintf("强化档位 +%d 越界（合法 1–5），保留原文不猜：%q", n, line)}
		}
		roll = &n
		rest = rest[2:]
	}
	rest = strings.TrimLeftFunc(rest, unicode.IsSpace)

	unit := UnitFlat
	if strings.HasSuffix(rest, "%") {
		unit = UnitPercent
		rest = rest[:len(rest)-1]
	}
	value, ok := canonicalDecimal(strings.TrimSpace(rest))
	if !ok {
		return affixSplit{}, &affixProblem{CodeAffixValueInvalid,
			fmt.Sprintf("数值部分不是合法的非负十进制：%q", line)}
	}
	return affixSplit{attribute: def.attribute, roll: roll, value: value, unit: unit}, nil
}

// splitNameLine splits the set-name line ("河豚电音[1]", half-width
// brackets, no leading zero in the slot) into the set name and the slot.
func splitNameLine(text string) (name string, slot int, ok bool) {
	line := strings.TrimSpace(text)
	open := strings.LastIndexByte(line, '[')
	if open < 0 || !strings.HasSuffix(line, "]") {
		return "", 0, false
	}
	digits := line[open+1 : len(line)-1]
	if !isDigits(digits) || (len(digits) > 1 && digits[0] == '0') {
		return "", 0, false
	}
	slot, err := strconv.Atoi(digits)
	if err != nil {
		return "", 0, false
	}
	name = strings.TrimSpace(line[:open])
	if name == "" {
		return "", 0, false
	}
	return name, slot, true
}

// splitLevelLine splits the level line ("等级15/15") into the level and
// the denominator, both non-negative decimals without leading zeros.
func splitLevelLine(text string) (level, denominator int, ok bool) {
	line := strings.TrimSpace(text)
	rest, found := strings.CutPrefix(line, "等级")
	if !found {
		return 0, 0, false
	}
	num, denom, found := strings.Cut(rest, "/")
	if !found || !isDigits(num) || !isDigits(denom) ||
		(len(num) > 1 && num[0] == '0') || (len(denom) > 1 && denom[0] == '0') {
		return 0, 0, false
	}
	level, err1 := strconv.Atoi(num)
	denominator, err2 := strconv.Atoi(denom)
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return level, denominator, true
}

// canonicalDecimal validates s as a decimal the screen can show and
// returns its canonical form: digits with an optional fraction, no sign,
// no exponent, no leading zero ("0" itself is fine) and no trailing
// fraction zero ("9.60" canonically is "9.6", as the scan JSON contract
// notes). Internal spaces or any other character fails.
func canonicalDecimal(s string) (string, bool) {
	intPart, fracPart, hasFrac := strings.Cut(s, ".")
	if intPart == "" || !isDigits(intPart) || (len(intPart) > 1 && intPart[0] == '0') {
		return "", false
	}
	if !hasFrac {
		return intPart, true
	}
	if fracPart == "" || !isDigits(fracPart) {
		return "", false
	}
	fracPart = strings.TrimRight(fracPart, "0")
	if fracPart == "" {
		return intPart, true
	}
	return intPart + "." + fracPart, true
}

// isDigit reports whether b is an ASCII digit.
func isDigit(b byte) bool { return b >= '0' && b <= '9' }

// isDigits reports whether s is one or more ASCII digits.
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isDigit(s[i]) {
			return false
		}
	}
	return true
}
