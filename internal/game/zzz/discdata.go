package zzz

// The structured drive-disc payload mirrors the scan JSON contract's
// `data` object plus the record-level `issues` and `validation`. It is
// the bridge between the detail-line parser and persistence: every field
// the parser could not resolve stays an explicit JSON null, and the raw
// recognized lines remain in the enclosing recognition payload for
// evidence and audit (see internal/app.RecognitionResult).
//
// Management state and equipment presence now come from the collector's
// calibrated badge/avatar evidence. The equipper's identity is deliberately
// still not recognized: this revision writes agentId=null and keeps
// validation unavailable, so no record can be auto-accepted yet. A later
// revision fills the identity and may then allow auto-acceptance.

// ManagementState and equipment-presence values of an observation, as the
// collector confirmed them. Empty means the collector could not read the
// field or its evidence conflicted; it is never guessed here.
const (
	ManagementLocked    = "locked"
	ManagementDiscarded = "discarded"
	ManagementNone      = "none"

	EquippedStateEquipped   = "equipped"
	EquippedStateUnequipped = "unequipped"
)

// CaptureState is the non-text evidence the collector confirmed for one
// observation. Empty fields stay JSON null. This revision fills presence
// only; Equipped.AgentID remains null until PR 6b.
type CaptureState struct {
	ManagementState string
	EquippedState   string
}

// RarityS is the only rarity this revision can assert, because it comes
// from the calibrated S badge of a verified capture rather than from the
// set name or level.
const RarityS = "S"

// Additional issue codes of the structured conversion.
const (
	// CodeRarityUnverified: the observation carried no verified S
	// capture, so rarity is unknown rather than guessed.
	CodeRarityUnverified = "rarityUnverified"
	// CodeManagementStateUnavailable: the observation carried no
	// confirmed management state.
	CodeManagementStateUnavailable = "managementStateUnavailable"
	// CodeEquippedStateUnavailable: the observation carried no confirmed
	// equipment-presence state, or its equipper identity is not recognized
	// yet (agentId=null).
	CodeEquippedStateUnavailable = "equippedStateUnavailable"
)

// StatData is one affix value in the public JSON shape. Every part is a
// pointer so an unread attribute, value or unit stays an explicit null.
type StatData struct {
	Attribute *string `json:"attribute"`
	Value     *string `json:"value"`
	Unit      *string `json:"unit"`
}

// SubstatData is one substat: a StatData plus the displayed enhancement
// roll. EnhancementRolls is 0 when the line shows no "+N", 1–5 when it
// does and null when the line exists but could not be read.
type SubstatData struct {
	StatData
	EnhancementRolls *int `json:"enhancementRolls"`
}

// Equipped is the public equipper shape. This revision fills State with
// the confirmed presence ("equipped" or "unequipped") and always leaves
// AgentID null; PR 6b fills the identity without reshaping persisted
// records.
type Equipped struct {
	State   string `json:"state"`
	AgentID *int   `json:"agentId"`
}

// DiscData is the structured `data` object of one drive disc. Unknown
// fields are null; Substats is null when the line count is unknown, and
// an empty non-nil slice only when there are confirmed no visible
// substats.
type DiscData struct {
	SetID           *int          `json:"setId"`
	Rarity          *string       `json:"rarity"`
	Slot            *int          `json:"slot"`
	Level           *int          `json:"level"`
	MainStat        *StatData     `json:"mainStat"`
	Substats        []SubstatData `json:"substats"`
	ManagementState *string       `json:"managementState"`
	Equipped        *Equipped     `json:"equipped"`
}

// Validation is the record-level revalidation result. This revision does
// not claim passed: parsing errors make it failed, and otherwise an
// unconfirmed management/equipment state or the still-missing equipper
// identity keeps it unavailable.
type Validation struct {
	Status string  `json:"status"`
	Issues []Issue `json:"issues"`
}

// Validation statuses of the scan JSON contract.
const (
	ValidationPassed      = "passed"
	ValidationFailed      = "failed"
	ValidationUnavailable = "unavailable"
)

// StructuredDisc is the persisted structured payload of one produced
// item: the data object and the parsing issues, plus the validation that
// explains why this revision is not auto-accepted.
type StructuredDisc struct {
	Data       DiscData   `json:"data"`
	Issues     []Issue    `json:"issues"`
	Validation Validation `json:"validation"`
}

// ToStructured converts a parsed detail record into the public structured
// payload without capture-state evidence. rarity is the verified capture
// rarity ("S") or "" when the observation carried none; it is never
// inferred from the set or level. Management state and equipment presence
// stay null. The returned issue and validation arrays are always present
// (empty arrays rather than null).
func ToStructured(rec DiscRecord, rarity string, issues []Issue) StructuredDisc {
	return ToStructuredWithState(rec, rarity, CaptureState{}, issues)
}

// ToStructuredWithState is ToStructured plus the collector's confirmed
// management state and equipment presence. Unrecognized values stay null
// and are reported; they are never coerced into a legal state.
func ToStructuredWithState(rec DiscRecord, rarity string, state CaptureState, issues []Issue) StructuredDisc {
	allIssues := append([]Issue(nil), issues...)
	if rarity != RarityS {
		allIssues = append(allIssues, newIssue("rarity", CodeRarityUnverified,
			"本次捕获没有已验证的 S 稀有度证据，rarity 保留为 null"))
	}
	return StructuredDisc{
		Data:       ToDataWithState(rec, rarity, state),
		Issues:     normalizeIssues(allIssues),
		Validation: validate(allIssues, state),
	}
}

// ToData converts a parsed detail record into the public data object
// without capture-state evidence. rarity is the verified capture rarity
// ("S") or ""; only a verified S becomes non-null. Management state and
// equipment presence stay null.
func ToData(rec DiscRecord, rarity string) DiscData {
	return ToDataWithState(rec, rarity, CaptureState{})
}

// ToDataWithState converts a parsed detail record plus confirmed capture
// state into the public data object. Empty or unrecognized state remains
// null. An equipped/unequipped object always carries agentId=null in this
// revision.
func ToDataWithState(rec DiscRecord, rarity string, state CaptureState) DiscData {
	var rarityValue *string
	if rarity == RarityS {
		value := RarityS
		rarityValue = &value
	}

	var setID *int
	if rec.SetID != nil {
		id := *rec.SetID
		setID = &id
	}
	var slot *int
	if rec.Slot != 0 {
		value := rec.Slot
		slot = &value
	}
	var level *int
	if rec.Level != nil {
		value := *rec.Level
		level = &value
	}

	var substats []SubstatData
	if rec.Substats != nil {
		substats = make([]SubstatData, len(rec.Substats))
		for i, sub := range rec.Substats {
			substats[i] = substatData(sub)
		}
	}

	var managementState *string
	if validManagementState(state.ManagementState) {
		value := state.ManagementState
		managementState = &value
	}
	var equipped *Equipped
	switch state.EquippedState {
	case EquippedStateEquipped, EquippedStateUnequipped:
		equipped = &Equipped{State: state.EquippedState}
	}

	return DiscData{
		SetID:           setID,
		Rarity:          rarityValue,
		Slot:            slot,
		Level:           level,
		MainStat:        mainStatData(rec.MainStat),
		Substats:        substats,
		ManagementState: managementState,
		Equipped:        equipped,
	}
}

// substatData converts one substat, keeping an unread line as an object
// with null fields instead of dropping it.
func substatData(sub Substat) SubstatData {
	var out SubstatData
	if sub.Attribute != "" {
		attribute := string(sub.Attribute)
		out.Attribute = &attribute
	}
	if sub.Value != "" {
		value := sub.Value
		out.Value = &value
	}
	if sub.Unit != "" {
		unit := string(sub.Unit)
		out.Unit = &unit
	}
	if sub.EnhancementRolls != nil {
		rolls := *sub.EnhancementRolls
		out.EnhancementRolls = &rolls
	}
	return out
}

// mainStatData converts one main stat; a wholly unread main stat is null
// rather than an object of nulls.
func mainStatData(stat Stat) *StatData {
	if stat.Attribute == "" || stat.Value == "" || stat.Unit == "" {
		return nil
	}
	attribute := string(stat.Attribute)
	value := stat.Value
	unit := string(stat.Unit)
	return &StatData{Attribute: &attribute, Value: &value, Unit: &unit}
}

// validate derives the current validation status. Parsing errors mean
// failed. Without them, a missing management state is a data gap, and an
// equipped record is still incomplete while the equipper identity
// (agentId) is not recognized. An explicitly unequipped record has no
// equipper to identify, so a confirmed management state is enough for
// validation to pass; the record still needs human confirmation because
// this revision never marks a scan auto-accepted.
func validate(issues []Issue, state CaptureState) Validation {
	if hasError(issues) {
		return Validation{Status: ValidationFailed, Issues: normalizeIssues(issues)}
	}
	gaps := make([]Issue, 0, 2)
	if !validManagementState(state.ManagementState) {
		gaps = append(gaps, newIssue("managementState", CodeManagementStateUnavailable,
			"本观测没有可确认的管理状态证据，无法完成校验"))
	}
	switch state.EquippedState {
	case EquippedStateEquipped:
		gaps = append(gaps, newIssue("equipped", CodeEquippedStateUnavailable,
			"本修订已确认装备存在性，但尚未识别装备者身份（agentId 为 null），无法完成校验"))
	case EquippedStateUnequipped:
		// No equipper to identify; the state itself is complete.
	case "":
		gaps = append(gaps, newIssue("equipped", CodeEquippedStateUnavailable,
			"本观测没有可确认的装备存在性证据，无法完成校验"))
	default:
		// The collector produced an unrecognized value; treat it like a
		// missing observation instead of accepting an unknown state.
		gaps = append(gaps, newIssue("equipped", CodeEquippedStateUnavailable,
			"本观测的装备存在性状态无法识别，无法完成校验"))
	}
	if len(gaps) == 0 {
		return Validation{Status: ValidationPassed, Issues: []Issue{}}
	}
	return Validation{Status: ValidationUnavailable, Issues: normalizeIssues(gaps)}
}

func validManagementState(state string) bool {
	switch state {
	case ManagementLocked, ManagementDiscarded, ManagementNone:
		return true
	default:
		return false
	}
}

// hasError reports whether any issue has error severity.
func hasError(issues []Issue) bool {
	for _, issue := range issues {
		if issue.Severity == SeverityError {
			return true
		}
	}
	return false
}

// normalizeIssues turns a nil issue list into an empty JSON array.
func normalizeIssues(issues []Issue) []Issue {
	if issues == nil {
		return []Issue{}
	}
	return issues
}
