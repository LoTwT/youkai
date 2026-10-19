package ui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/LoTwT/youkai/internal/game/zzz"
)

// unreadOption is the explicit "not read" choice of every catalog picker.
// It maps to a JSON null, never to a plausible default.
const unreadOption = "（未读取）"

// reviewAttributes is the fixed attribute candidate order of the scan
// JSON contract. Labels resolve through zzz.LookupAttributeName, so the
// display names stay owned by the catalog package.
var reviewAttributes = []zzz.Attribute{
	zzz.AttrHealth,
	zzz.AttrAttack,
	zzz.AttrDefense,
	zzz.AttrCritRate,
	zzz.AttrCritDamage,
	zzz.AttrPenetration,
	zzz.AttrPenetrationRatio,
	zzz.AttrAnomalyProficiency,
	zzz.AttrAnomalyMastery,
	zzz.AttrImpact,
	zzz.AttrEnergyRegen,
	zzz.AttrPhysicalDamage,
	zzz.AttrFireDamage,
	zzz.AttrIceDamage,
	zzz.AttrElectricDamage,
	zzz.AttrEtherDamage,
	zzz.AttrWindDamage,
}

// reviewCatalogLimit bounds the enumeration below. The contract's set and
// agent ids are five-digit fairy entity ids; this revision's catalog
// package exposes lookup-by-id only, and the PR's file boundary keeps it
// untouched, so the picker enumerates the fixed catalog through the
// public lookup once per process instead of duplicating the tables.
const reviewCatalogLimit = 100000

var (
	catalogOnce        sync.Once
	catalogSetLabels   []string
	catalogAgentLabels []string
)

func enumerateCatalogOnce() {
	catalogOnce.Do(func() {
		for id := 0; id < reviewCatalogLimit; id++ {
			if set, ok := zzz.LookupSetByID(id); ok {
				catalogSetLabels = append(catalogSetLabels, fmt.Sprintf("%d %s", set.ID, set.Name))
			}
			if agent, ok := zzz.LookupAgentByID(id); ok {
				catalogAgentLabels = append(catalogAgentLabels, fmt.Sprintf("%d %s", agent.ID, agent.Name))
			}
		}
	})
}

// setChoices lists the fixed set candidates, with an unknown current id
// kept as its own option so a legacy value is never silently replaced.
func setChoices(current *int) []string {
	enumerateCatalogOnce()
	options := append([]string{unreadOption}, catalogSetLabels...)
	if current != nil {
		label := describeSet(*current)
		if !slices.Contains(options, label) {
			options = append(options, label)
		}
	}
	return options
}

// agentChoices lists the fixed agent candidates, keeping an unknown
// current id visible for correction.
func agentChoices(current *int) []string {
	enumerateCatalogOnce()
	options := append([]string{unreadOption}, catalogAgentLabels...)
	if current != nil {
		label := describeAgent(*current)
		if !slices.Contains(options, label) {
			options = append(options, label)
		}
	}
	return options
}

// attributeChoices lists the fixed affix-type candidates.
func attributeChoices(current *string) []string {
	options := []string{unreadOption}
	for _, attribute := range reviewAttributes {
		options = append(options, describeAttribute(string(attribute)))
	}
	if current != nil && *current != "" {
		label := describeAttribute(*current)
		if !slices.Contains(options, label) {
			options = append(options, label)
		}
	}
	return options
}

// unitChoices lists the units the affix type can carry. Both units are
// offered while the type is unread; the authoritative slot/main and
// substat rules still come from the detail parser at save time.
func unitChoices(attribute, current *string) []string {
	allowed := []zzz.Unit{zzz.UnitFlat, zzz.UnitPercent}
	if attribute != nil {
		switch zzz.Attribute(*attribute) {
		case zzz.AttrHealth, zzz.AttrAttack, zzz.AttrDefense:
			// Both units are legal for these three.
		case zzz.AttrPenetration, zzz.AttrAnomalyProficiency:
			allowed = []zzz.Unit{zzz.UnitFlat}
		default:
			allowed = []zzz.Unit{zzz.UnitPercent}
		}
	}
	options := []string{unreadOption}
	for _, unit := range allowed {
		options = append(options, describeUnit(string(unit)))
	}
	if current != nil && *current != "" {
		label := describeUnit(*current)
		if !slices.Contains(options, label) {
			options = append(options, label)
		}
	}
	return options
}

// slotChoices lists the six slots.
func slotChoices(current *int) []string {
	options := []string{unreadOption}
	for slot := 1; slot <= 6; slot++ {
		options = append(options, strconv.Itoa(slot))
	}
	if current != nil {
		value := strconv.Itoa(*current)
		if !slices.Contains(options, value) {
			options = append(options, value)
		}
	}
	return options
}

func managementChoices(current *string) []string {
	options := []string{
		unreadOption,
		"locked 锁定",
		"discarded 弃置",
		"none 无标记",
	}
	if current != nil && *current != "" {
		label := describeManagement(*current)
		if !slices.Contains(options, label) {
			options = append(options, label)
		}
	}
	return options
}

func equippedChoices(current string) []string {
	options := []string{
		unreadOption,
		"equipped 已装备",
		"unequipped 未装备",
	}
	if current != "" {
		label := describeEquipped(current)
		if !slices.Contains(options, label) {
			options = append(options, label)
		}
	}
	return options
}

// describeSet is the fixed label of a set id.
func describeSet(id int) string {
	if set, ok := zzz.LookupSetByID(id); ok {
		return fmt.Sprintf("%d %s", set.ID, set.Name)
	}
	return fmt.Sprintf("%d 未知套装", id)
}

func describeAgent(id int) string {
	if agent, ok := zzz.LookupAgentByID(id); ok {
		return fmt.Sprintf("%d %s", agent.ID, agent.Name)
	}
	return fmt.Sprintf("%d 未知代理人", id)
}

func describeAttribute(attribute string) string {
	if name, ok := zzz.LookupAttributeName(zzz.Attribute(attribute)); ok {
		return attribute + " " + name
	}
	return attribute + " 未知词条"
}

func describeUnit(unit string) string {
	switch zzz.Unit(unit) {
	case zzz.UnitFlat:
		return "flat 固定值"
	case zzz.UnitPercent:
		return "percent 百分比"
	default:
		return unit + " 未知单位"
	}
}

func describeManagement(state string) string {
	switch state {
	case zzz.ManagementLocked:
		return "locked 锁定"
	case zzz.ManagementDiscarded:
		return "discarded 弃置"
	case zzz.ManagementNone:
		return "none 无标记"
	default:
		return state + " 未知状态"
	}
}

func describeEquipped(state string) string {
	switch state {
	case zzz.EquippedStateEquipped:
		return "equipped 已装备"
	case zzz.EquippedStateUnequipped:
		return "unequipped 未装备"
	default:
		return state + " 未知状态"
	}
}

// optionValue returns the machine value of a picker label, empty for the
// explicit unread choice. Labels are "<value> <display name>".
func optionValue(label string) string {
	value, _, _ := strings.Cut(label, " ")
	if value == unreadOption {
		return ""
	}
	return value
}

// intOption renders a nullable integer as a text box value / picker
// label, empty for null.
func intOption(value *int) string {
	if value == nil {
		return ""
	}
	return strconv.Itoa(*value)
}
