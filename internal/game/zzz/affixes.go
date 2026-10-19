package zzz

// Affix tables, copied once from fairy commit 726c9116 (see catalog.go
// for the provenance block): the legal main-stat identities per slot and
// the ten legal substat identities, plus the zh-CN attribute display
// names the detail-line parser splits affix lines against.
//
// The attribute identifiers match the scan JSON contract's encoding
// (scan-json-format.md section 4); fairy's identifiers map as noted per
// constant (criticalRate→critRate, criticalDamage→critDamage,
// penetrationValue→penetration, damageBonus+element→<element>Damage; the
// rest share names).
//
// Display-name sources: 生命值、攻击力、防御力、暴击率、暴击伤害、穿透值、
// 穿透率、异常精通 are evidenced by the six-disc sample of
// internal/recognition/testdata; the six slot-5 damage bonuses (物理伤
// 害加成 uses 物理; 火/冰/电/风 follow the uniform X属性伤害加成 pattern,
// while 以太伤害加成 has no 属性 — asymmetric with the elements, matching
// 物理; the user's 2026-10-18 supply had assumed the uniform pattern) and
// the three slot-6 attributes (异常掌控、冲击力、能量自动回复) were
// verified against real client screens in the 2026-10-19 macOS sample
// round (detail pages and filter-panel options, evidence directories
// s2-n5-* and s2-n6-*; see docs/reports/2026-10-19-m2-macos-sample-round.md
// and the 27-disc fixture in internal/recognition/testdata/macos-cloud).

// Unit distinguishes the two ways a stat value is displayed: fixed points
// or a percentage.
type Unit string

const (
	// UnitFlat is a fixed-point value ("2200").
	UnitFlat Unit = "flat"
	// UnitPercent is a displayed percentage: the value string keeps the
	// visible precision, so "19.2" + percent is 19.2%, not the ratio 0.192.
	UnitPercent Unit = "percent"
)

// Attribute identifies one stat kind. The constants match the JSON
// contract's attribute encoding exactly.
type Attribute string

const (
	AttrHealth             Attribute = "health"             // 生命值
	AttrAttack             Attribute = "attack"             // 攻击力
	AttrDefense            Attribute = "defense"            // 防御力
	AttrCritRate           Attribute = "critRate"           // 暴击率
	AttrCritDamage         Attribute = "critDamage"         // 暴击伤害
	AttrPenetration        Attribute = "penetration"        // 穿透值
	AttrPenetrationRatio   Attribute = "penetrationRatio"   // 穿透率
	AttrAnomalyProficiency Attribute = "anomalyProficiency" // 异常精通
	AttrAnomalyMastery     Attribute = "anomalyMastery"     // 异常掌控
	AttrImpact             Attribute = "impact"             // 冲击力
	AttrEnergyRegen        Attribute = "energyRegen"        // 能量自动回复
	AttrPhysicalDamage     Attribute = "physicalDamage"     // 物理伤害加成
	AttrFireDamage         Attribute = "fireDamage"         // 火属性伤害加成
	AttrIceDamage          Attribute = "iceDamage"          // 冰属性伤害加成
	AttrElectricDamage     Attribute = "electricDamage"     // 电属性伤害加成
	AttrEtherDamage        Attribute = "etherDamage"        // 以太伤害加成
	AttrWindDamage         Attribute = "windDamage"         // 风属性伤害加成
)

// LookupAttributeName resolves an attribute identifier to the zh-CN
// display name the detail page shows. Unknown identifiers report false;
// callers keep the identifier itself rather than inventing a name.
func LookupAttributeName(attribute Attribute) (string, bool) {
	for i := range attributeTable {
		if attributeTable[i].attribute == attribute {
			return attributeTable[i].name, true
		}
	}
	return "", false
}

// Affix is the identity of one stat kind in one unit — what a main-stat
// slot may carry and what a substat line may show. health, attack and
// defense exist in both units; every other attribute has exactly one.
type Affix struct {
	Attribute Attribute
	Unit      Unit
}

// attributeDef describes one attribute as the detail page can show it.
type attributeDef struct {
	attribute Attribute
	// name is the zh-CN display name; the affix-line split matches
	// against these names (longest prefix).
	name string
	// units lists the units the catalog allows for the attribute.
	units []Unit
}

// attributeTable lists every attribute an affix line can name, in
// catalog order. No name is a prefix of another name today; the parser
// still does a longest-prefix match so a future longer name that starts
// with a shorter one resolves deterministically.
var attributeTable = []attributeDef{
	{attribute: AttrHealth, name: "生命值", units: []Unit{UnitFlat, UnitPercent}},
	{attribute: AttrAttack, name: "攻击力", units: []Unit{UnitFlat, UnitPercent}},
	{attribute: AttrDefense, name: "防御力", units: []Unit{UnitFlat, UnitPercent}},
	{attribute: AttrCritRate, name: "暴击率", units: []Unit{UnitPercent}},
	{attribute: AttrCritDamage, name: "暴击伤害", units: []Unit{UnitPercent}},
	{attribute: AttrPenetration, name: "穿透值", units: []Unit{UnitFlat}},
	{attribute: AttrPenetrationRatio, name: "穿透率", units: []Unit{UnitPercent}},
	{attribute: AttrAnomalyProficiency, name: "异常精通", units: []Unit{UnitFlat}},
	{attribute: AttrAnomalyMastery, name: "异常掌控", units: []Unit{UnitPercent}},
	{attribute: AttrImpact, name: "冲击力", units: []Unit{UnitPercent}},
	{attribute: AttrEnergyRegen, name: "能量自动回复", units: []Unit{UnitPercent}},
	{attribute: AttrPhysicalDamage, name: "物理伤害加成", units: []Unit{UnitPercent}},
	{attribute: AttrFireDamage, name: "火属性伤害加成", units: []Unit{UnitPercent}},
	{attribute: AttrIceDamage, name: "冰属性伤害加成", units: []Unit{UnitPercent}},
	{attribute: AttrElectricDamage, name: "电属性伤害加成", units: []Unit{UnitPercent}},
	{attribute: AttrEtherDamage, name: "以太伤害加成", units: []Unit{UnitPercent}},
	{attribute: AttrWindDamage, name: "风属性伤害加成", units: []Unit{UnitPercent}},
}

// mainAffixesBySlot lists the legal main-stat identities per slot (index
// 1–6), in fairy's order: slots 1–3 are fixed; slots 4–6 list every legal
// option. Slot 5's six element damage bonuses come from fairy's
// damageBonus entries distinguished by their element field.
var mainAffixesBySlot = [7][]Affix{
	1: {{AttrHealth, UnitFlat}},
	2: {{AttrAttack, UnitFlat}},
	3: {{AttrDefense, UnitFlat}},
	4: {
		{AttrCritDamage, UnitPercent},
		{AttrCritRate, UnitPercent},
		{AttrAttack, UnitPercent},
		{AttrHealth, UnitPercent},
		{AttrAnomalyProficiency, UnitFlat},
		{AttrDefense, UnitPercent},
	},
	5: {
		{AttrAttack, UnitPercent},
		{AttrHealth, UnitPercent},
		{AttrDefense, UnitPercent},
		{AttrPenetrationRatio, UnitPercent},
		{AttrPhysicalDamage, UnitPercent},
		{AttrFireDamage, UnitPercent},
		{AttrIceDamage, UnitPercent},
		{AttrElectricDamage, UnitPercent},
		{AttrEtherDamage, UnitPercent},
		{AttrWindDamage, UnitPercent},
	},
	6: {
		{AttrAttack, UnitPercent},
		{AttrHealth, UnitPercent},
		{AttrDefense, UnitPercent},
		{AttrAnomalyMastery, UnitPercent},
		{AttrImpact, UnitPercent},
		{AttrEnergyRegen, UnitPercent},
	},
}

// subAffixes lists the ten legal substat identities, in fairy's order.
// penetration here is the flat 穿透值; penetrationRatio (穿透率) is a
// slot-5 main stat only, and neither may be mixed up with the other, just
// like anomalyProficiency (异常精通) versus anomalyMastery (异常掌控).
var subAffixes = []Affix{
	{AttrHealth, UnitFlat},
	{AttrHealth, UnitPercent},
	{AttrAttack, UnitFlat},
	{AttrAttack, UnitPercent},
	{AttrDefense, UnitFlat},
	{AttrDefense, UnitPercent},
	{AttrPenetration, UnitFlat},
	{AttrCritRate, UnitPercent},
	{AttrCritDamage, UnitPercent},
	{AttrAnomalyProficiency, UnitFlat},
}

// mainAffixAllowed reports whether slot (1–6) may carry the affix.
func mainAffixAllowed(slot int, affix Affix) bool {
	if slot < 1 || slot > 6 {
		return false
	}
	for _, candidate := range mainAffixesBySlot[slot] {
		if candidate == affix {
			return true
		}
	}
	return false
}

// subAffixAllowed reports whether the affix is one of the ten substats.
func subAffixAllowed(affix Affix) bool {
	for _, candidate := range subAffixes {
		if candidate == affix {
			return true
		}
	}
	return false
}
