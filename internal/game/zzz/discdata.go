package zzz

// The structured drive-disc payload mirrors the scan JSON contract's
// `data` object plus the record-level `issues` and `validation`. It is
// the bridge between the detail-line parser and persistence: every field
// the parser could not resolve stays an explicit JSON null, and the raw
// recognized lines remain in the enclosing recognition payload for
// evidence and audit (see internal/app.RecognitionResult).
//
// Management state and the equipper are deliberately not recognized yet:
// this revision always writes managementState=null and equipped=null and
// reports validation as unavailable unless parsing already failed. A
// later revision fills both and may then allow auto-acceptance.

// RarityS is the only rarity this revision can assert, because it comes
// from the calibrated S badge of a verified capture rather than from the
// set name or level.
const RarityS = "S"

// Additional issue codes of the structured conversion.
const (
	// CodeRarityUnverified: the observation carried no verified S
	// capture, so rarity is unknown rather than guessed.
	CodeRarityUnverified = "rarityUnverified"
	// CodeManagementStateUnavailable: management state recognition is not
	// implemented yet.
	CodeManagementStateUnavailable = "managementStateUnavailable"
	// CodeEquippedStateUnavailable: equipper recognition is not
	// implemented yet.
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

// Equipped is the public equipper shape. This revision never produces
// one; it is defined now so the later recognition work fills the same
// contract without reshaping persisted records.
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
// not claim passed: parsing errors make it failed, and otherwise the
// missing management/equipper data keeps it unavailable.
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
// payload. rarity is the verified capture rarity ("S") or "" when the
// observation carried none; it is never inferred from the set or level.
// The returned issue and validation arrays are always present (empty
// arrays rather than null).
func ToStructured(rec DiscRecord, rarity string, issues []Issue) StructuredDisc {
	allIssues := append([]Issue(nil), issues...)
	if rarity != RarityS {
		allIssues = append(allIssues, newIssue("rarity", CodeRarityUnverified,
			"本次捕获没有已验证的 S 稀有度证据，rarity 保留为 null"))
	}
	return StructuredDisc{
		Data:       ToData(rec, rarity),
		Issues:     normalizeIssues(allIssues),
		Validation: validate(allIssues),
	}
}

// ToData converts a parsed detail record into the public data object.
// rarity is the verified capture rarity ("S") or ""; only a verified S
// becomes non-null. Management state and the equipper are always null in
// this revision.
func ToData(rec DiscRecord, rarity string) DiscData {
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

	return DiscData{
		SetID:           setID,
		Rarity:          rarityValue,
		Slot:            slot,
		Level:           level,
		MainStat:        mainStatData(rec.MainStat),
		Substats:        substats,
		ManagementState: nil,
		Equipped:        nil,
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
// failed. Without them the record is still unavailable because management
// state and the equipper are not recognized in this revision; that gap is
// reported as an error rather than hidden behind a passed status.
func validate(issues []Issue) Validation {
	if hasError(issues) {
		return Validation{Status: ValidationFailed, Issues: normalizeIssues(issues)}
	}
	return Validation{
		Status: ValidationUnavailable,
		Issues: normalizeIssues([]Issue{
			newIssue("managementState", CodeManagementStateUnavailable,
				"本修订尚未识别管理状态（锁定/弃置），无法完成校验"),
			newIssue("equipped", CodeEquippedStateUnavailable,
				"本修订尚未识别装备者，无法完成校验"),
		}),
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
