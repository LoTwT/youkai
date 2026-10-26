package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/LoTwT/youkai/internal/game/zzz"
	"github.com/LoTwT/youkai/internal/record"
)

// ErrRevisionInvalid rejects a manual revision whose edited values are
// malformed or conflict with the fixed catalog. Explicitly unread fields
// are not invalid: they may be saved as a needsReview revision, but a
// whole-item confirmation requires every required field and a passed
// validation.
var ErrRevisionInvalid = errors.New("item revision is invalid")

// ErrRecordActive rejects a manual revision of a record whose scan still
// owns it. The scan deliverer rewrites the whole checkpoint from its
// in-memory item copy; letting an edit land before the final save would
// let that copy overwrite the revision. Once the scan has wrapped, the
// record is editable again.
var ErrRecordActive = errors.New("record is still being written by an active scan")

// RevisionInvalidError explains why an edit could not be saved or
// confirmed. Issues are the blocking field problems; Reason carries the
// non-field explanation of a refused confirmation.
type RevisionInvalidError struct {
	Reason string
	Issues []zzz.Issue
}

func (e *RevisionInvalidError) Error() string {
	if e.Reason != "" {
		return "app: save item revision: " + e.Reason
	}
	if len(e.Issues) == 0 {
		return "app: save item revision: the edited values are invalid"
	}
	return "app: save item revision: " + e.Issues[0].Message
}

func (e *RevisionInvalidError) Unwrap() error { return ErrRevisionInvalid }

// ItemRevisionEdit is one user-authored revision of a produced item's
// structured drive-disc payload. The application service recomputes the
// payload's issues and validation from Data, so a caller cannot assert a
// passed validation or keep a stale confirmation.
type ItemRevisionEdit struct {
	// Data is the complete edited data object. Explicit null parts keep
	// their unread meaning; they are never defaulted here.
	Data zzz.DiscData
	// Confirmed marks a whole-item human confirmation. It is accepted
	// only when CheckDiscData reports a passed validation.
	Confirmed bool
}

// ItemRevisionResult is one persisted revision: the item as it now stands
// and the checkpoint generation it was saved under.
type ItemRevisionResult struct {
	Item       record.ItemRecord
	Generation int64
}

// DataCheck is the current validation of one edited data object, split by
// whether a field blocks saving or merely leaves the item incomplete.
type DataCheck struct {
	// Blocking lists present values that are malformed or outside the
	// fixed catalog. A save with blocking issues is refused.
	Blocking []zzz.Issue
	// Gaps lists required fields still explicitly unread or incomplete.
	// They may be saved as needsReview, but not confirmed, and they keep
	// the record out of usable export.
	Gaps []zzz.Issue
	// Warnings lists optional fields that are explicitly incomplete. They
	// stay visible but do not block saving, confirmation or export.
	Warnings []zzz.Issue
	// Issues is Blocking followed by Gaps and Warnings, with exact
	// duplicates shown once. It is empty, not nil, when nothing is wrong.
	Issues []zzz.Issue
	// Validation is the record-level conclusion bound to this data:
	// failed with blocking issues, unavailable with only gaps, passed
	// when neither remains (warnings may still be attached).
	Validation zzz.Validation
}

// Confirmable reports whether the data may be saved with confirmed=true.
func (c DataCheck) Confirmable() bool {
	return len(c.Blocking) == 0 && c.Validation.Status == zzz.ValidationPassed
}

// SaveItemRevision appends one manual revision to a produced item and
// persists it through the record store. The store's revision check is
// authoritative: a stale expected revision is returned as
// record.ErrRevisionConflict before anything is written, so an old editor
// never overwrites a newer one. The payload's data is replaced while its
// raw recognized lines stay untouched; issues and validation are
// recomputed from the current data and stamped on the new revision.
//
// A successful save notifies the service's subscribers, so any UI reading
// snapshots learns that the record changed even when the saved count did
// not grow. A record whose scan has not wrapped is refused with
// ErrRecordActive: the deliverer still owns that checkpoint and would
// overwrite the revision with its in-memory copy.
func (s *Service) SaveItemRevision(recordID string, sequence, expectedRevision int64, edit ItemRevisionEdit) (ItemRevisionResult, error) {
	check := CheckDiscData(edit.Data)
	if len(check.Blocking) > 0 {
		return ItemRevisionResult{}, &RevisionInvalidError{Issues: check.Blocking}
	}
	if edit.Confirmed && !check.Confirmable() {
		return ItemRevisionResult{}, &RevisionInvalidError{
			Reason: "required fields are still unread or the current validation has not passed",
			Issues: check.Gaps,
		}
	}

	// The scan deliverer owns the complete checkpoint until its final save
	// succeeds. Saving a revision into that window would be overwritten by
	// the deliverer's next full-document write, so refuse it here, before
	// the store's revision check can report success.
	s.mu.Lock()
	active := s.active
	busy := active != nil && !active.wrapped && string(active.id) == recordID
	s.mu.Unlock()
	if busy {
		return ItemRevisionResult{}, fmt.Errorf(
			"app: save item revision of %q: the scan is still writing this record; wait for it to finish: %w",
			recordID, ErrRecordActive)
	}

	rec, err := s.store.Load(recordID)
	if err != nil {
		return ItemRevisionResult{}, fmt.Errorf("app: save item revision of %q: %w", recordID, err)
	}
	item, ok := itemBySequence(rec, sequence)
	if !ok {
		return ItemRevisionResult{}, fmt.Errorf("item %d of %q: %w", sequence, recordID, record.ErrItemNotFound)
	}
	if item.Revision != expectedRevision {
		return ItemRevisionResult{}, fmt.Errorf("item %d of %q is at revision %d, not %d: %w",
			sequence, recordID, item.Revision, expectedRevision, record.ErrRevisionConflict)
	}

	oldData, _ := structuredData(item.Data)
	changes := diffDiscData(oldData, edit.Data)
	payload, err := replaceStructured(item.Data, edit.Data, check)
	if err != nil {
		return ItemRevisionResult{}, fmt.Errorf("app: save item revision of %q item %d: %w", recordID, sequence, err)
	}
	saved, generation, err := s.store.SaveRevision(recordID, sequence, expectedRevision, record.ItemEdit{
		Changes:   changes,
		Data:      payload,
		Confirmed: edit.Confirmed,
	})
	if err != nil {
		return ItemRevisionResult{}, fmt.Errorf("app: save item revision of %q item %d: %w", recordID, sequence, err)
	}

	s.mu.Lock()
	s.notifyLocked()
	s.mu.Unlock()
	return ItemRevisionResult{Item: saved, Generation: generation}, nil
}

// itemBySequence finds one item of a recovered checkpoint.
func itemBySequence(rec *record.Recovery, sequence int64) (record.ItemRecord, bool) {
	for i := range rec.Checkpoint.Items {
		if rec.Checkpoint.Items[i].Sequence == sequence {
			return rec.Checkpoint.Items[i], true
		}
	}
	return record.ItemRecord{}, false
}

// CheckDiscData validates one current data object. Values that are
// present must be canonical and resolve in the fixed catalog; missing
// values stay explicit gaps rather than being coerced into a plausible
// value. The cross-field rules of the detail parser — slot and main-stat
// compatibility, the ten legal substats, duplicate substats — are reused
// by feeding it a synthetic page, so this validation cannot drift from
// the recognition-time rules.
func CheckDiscData(data zzz.DiscData) DataCheck {
	var blocking, gaps, warnings []zzz.Issue
	blockingSeen := make(map[string]struct{})
	gapsSeen := make(map[string]struct{})
	warningsSeen := make(map[string]struct{})

	addBlocking := func(issue zzz.Issue) {
		appendUnique(&blocking, blockingSeen, issue)
	}
	addGap := func(issue zzz.Issue) {
		appendUnique(&gaps, gapsSeen, issue)
	}
	addWarning := func(issue zzz.Issue) {
		appendUnique(&warnings, warningsSeen, issue)
	}

	setKnown := false
	if data.SetID == nil {
		addGap(newReviewIssue("setId", "setIdUnavailable", "套装 ID 未读取，不能完成校验"))
	} else if _, ok := zzz.LookupSetByID(*data.SetID); !ok {
		addBlocking(newReviewIssue("setId", "unknownSet",
			fmt.Sprintf("套装 ID %d 不在资料表中", *data.SetID)))
	} else {
		setKnown = true
	}

	if data.Rarity == nil || *data.Rarity == "" {
		addGap(newReviewIssue("rarity", zzz.CodeRarityUnverified,
			"本次捕获没有已验证的 S 稀有度，rarity 保留为 null"))
	} else if *data.Rarity != zzz.RarityS {
		addBlocking(newReviewIssue("rarity", "rarityUnsupported",
			fmt.Sprintf("稀有度 %q 不是本修订可确认的 S", *data.Rarity)))
	}

	slotKnown := false
	if data.Slot == nil {
		addGap(newReviewIssue("slot", "slotUnavailable", "槽位未读取，不能完成校验"))
	} else if *data.Slot < 1 || *data.Slot > 6 {
		addBlocking(newReviewIssue("slot", zzz.CodeSlotOutOfRange,
			fmt.Sprintf("槽位 %d 越界（合法 1–6）", *data.Slot)))
	} else {
		slotKnown = true
	}

	levelKnown := false
	if data.Level == nil {
		addGap(newReviewIssue("level", "levelUnavailable", "等级未读取，不能完成校验"))
	} else if *data.Level < 0 || *data.Level > 15 {
		addBlocking(newReviewIssue("level", zzz.CodeLevelOutOfRange,
			fmt.Sprintf("等级 %d 越界（合法 0–15）", *data.Level)))
	} else {
		levelKnown = true
	}

	// Main stat: present parts are validated individually; the parser's
	// slot/main cross-check is synthesized only when all three are known.
	mainKnown := false
	if data.MainStat == nil {
		addGap(newReviewIssue("mainStat", "mainStatUnavailable", "主词条未读取，不能完成校验"))
	} else {
		attributeOK := reviewAttribute(data.MainStat.Attribute, "mainStat", addBlocking, addGap)
		unitOK := reviewUnit(data.MainStat.Unit, "mainStat", addBlocking, addGap)
		valueOK := reviewValue(data.MainStat.Value, "mainStat", addBlocking, addGap)
		mainKnown = attributeOK && unitOK && valueOK
		if !mainKnown {
			addGap(newReviewIssue("mainStat", "mainStatUnavailable",
				"主词条未填写完整（类型、数值和单位均需已知）"))
		}
	}

	// Substats: a null list means the count is unknown; an empty list is
	// the confirmed "no visible substats". Each line must be complete.
	subsKnown := data.Substats != nil
	if !subsKnown {
		addGap(newReviewIssue("substats", "substatsUnavailable", "副词条数量未读取，不能完成校验"))
	} else if len(data.Substats) > 4 {
		addBlocking(newReviewIssue("substats", zzz.CodeTooManySubstats,
			fmt.Sprintf("读出 %d 条副词条，最多四条", len(data.Substats))))
	}
	type subLine struct {
		index int
		text  string
	}
	var subLines []subLine
	if data.Substats != nil {
		for i := range data.Substats {
			sub := &data.Substats[i]
			field := fmt.Sprintf("substats[%d]", i)
			attributeOK := reviewAttribute(sub.Attribute, field, addBlocking, addGap)
			unitOK := reviewUnit(sub.Unit, field, addBlocking, addGap)
			valueOK := reviewValue(sub.Value, field, addBlocking, addGap)
			rollOK := true
			if sub.EnhancementRolls == nil {
				rollOK = false
				addGap(newReviewIssue(field, "enhancementRollsUnavailable",
					"副词条强化档位未读取，不能完成校验"))
			} else if *sub.EnhancementRolls < 0 || *sub.EnhancementRolls > 5 {
				rollOK = false
				addBlocking(newReviewIssue(field, zzz.CodeRollOutOfRange,
					fmt.Sprintf("强化档位 %d 越界（合法 0–5）", *sub.EnhancementRolls)))
			}
			if !attributeOK || !unitOK || !valueOK || !rollOK {
				if attributeOK && unitOK && valueOK && !rollOK {
					// The roll issue above already names the gap.
					continue
				}
				addGap(newReviewIssue(field, "substatUnavailable",
					"副词条未填写完整（类型、数值、单位和强化档位均需已知）"))
				continue
			}
			name, _ := zzz.LookupAttributeName(zzz.Attribute(*sub.Attribute))
			line := name
			if *sub.EnhancementRolls > 0 {
				line += " +" + strconv.Itoa(*sub.EnhancementRolls)
			}
			line += " " + reviewValueLine(sub.Value, sub.Unit)
			subLines = append(subLines, subLine{index: i, text: line})
		}
	}

	if data.ManagementState == nil || *data.ManagementState == "" {
		addGap(newReviewIssue("managementState", zzz.CodeManagementStateUnavailable,
			"管理状态未读取，不能确认"))
	} else if !validManagementState(*data.ManagementState) {
		addBlocking(newReviewIssue("managementState", "managementStateInvalid",
			fmt.Sprintf("管理状态 %q 不是 locked、discarded 或 none", *data.ManagementState)))
	}

	if data.Equipped == nil {
		addWarning(newReviewWarning("equipped", zzz.CodeEquippedStateUnavailable,
			"装备状态未读取；装备字段按可选处理"))
	} else {
		switch data.Equipped.State {
		case zzz.EquippedStateUnequipped:
			if data.Equipped.AgentID != nil {
				addBlocking(newReviewIssue("equipped", "unequippedAgentConflict",
					"未装备的条目不能带代理人 ID"))
			}
		case zzz.EquippedStateEquipped:
			if data.Equipped.AgentID == nil {
				addWarning(newReviewWarning("equipped", zzz.CodeEquippedStateUnavailable,
					"已装备但代理人未读取；装备字段按可选处理"))
			} else if _, ok := zzz.LookupAgentByID(*data.Equipped.AgentID); !ok {
				addBlocking(newReviewIssue("equipped", "unknownAgent",
					fmt.Sprintf("代理人 ID %d 不在资料表中", *data.Equipped.AgentID)))
			}
		default:
			addBlocking(newReviewIssue("equipped", "equippedStateInvalid",
				fmt.Sprintf("装备状态 %q 不是 equipped 或 unequipped", data.Equipped.State)))
		}
	}

	// Reuse the detail parser's cross-field rules on the parts that are
	// complete. Missing parts were reported above and are deliberately not
	// synthesized, so no parser issue is invented for an unread value.
	lines := zzz.DetailLines{}
	setSynth := setKnown && slotKnown
	if setSynth {
		set, _ := zzz.LookupSetByID(*data.SetID)
		lines.Set = fmt.Sprintf("%s[%d]", set.Name, *data.Slot)
	}
	if levelKnown {
		lines.Level = fmt.Sprintf("等级%d/15", *data.Level)
	}
	if mainKnown {
		name, _ := zzz.LookupAttributeName(zzz.Attribute(*data.MainStat.Attribute))
		lines.Main = name + " " + reviewValueLine(data.MainStat.Value, data.MainStat.Unit)
	}
	origins := make([]int, 0, len(subLines))
	if data.Substats != nil {
		for _, line := range subLines {
			lines.Subs = append(lines.Subs, line.text)
			origins = append(origins, line.index)
		}
		if len(lines.Subs) == 0 {
			lines.Subs = []string{} // a confirmed empty substat list
		}
	}
	_, parsed := zzz.ParseDetail(lines)
	for _, issue := range parsed {
		remapped, keep := remapParserIssue(issue, setSynth, levelKnown, mainKnown, origins)
		if keep {
			addBlocking(remapped)
		}
	}

	status := zzz.ValidationPassed
	switch {
	case len(blocking) > 0:
		status = zzz.ValidationFailed
	case len(gaps) > 0:
		status = zzz.ValidationUnavailable
	}
	issues := make([]zzz.Issue, 0, len(blocking)+len(gaps)+len(warnings))
	combinedSeen := make(map[string]struct{}, len(blocking)+len(gaps)+len(warnings))
	for _, issue := range blocking {
		appendUnique(&issues, combinedSeen, issue)
	}
	for _, issue := range gaps {
		appendUnique(&issues, combinedSeen, issue)
	}
	for _, issue := range warnings {
		appendUnique(&issues, combinedSeen, issue)
	}
	validationIssues := make([]zzz.Issue, len(issues))
	copy(validationIssues, issues)
	return DataCheck{
		Blocking:   blocking,
		Gaps:       gaps,
		Warnings:   warnings,
		Issues:     issues,
		Validation: zzz.Validation{Status: status, Issues: validationIssues},
	}
}

// reviewAttribute checks a present attribute identity. A nil or empty
// part is a gap; an unknown identity blocks the save.
func reviewAttribute(attribute *string, field string, block, gap func(zzz.Issue)) bool {
	if attribute == nil || *attribute == "" {
		gap(newReviewIssue(field, "attributeUnavailable", "词条类型未读取，不能完成校验"))
		return false
	}
	if _, ok := zzz.LookupAttributeName(zzz.Attribute(*attribute)); !ok {
		block(newReviewIssue(field, zzz.CodeAffixNameUnknown,
			fmt.Sprintf("词条类型 %q 不在资料表中", *attribute)))
		return false
	}
	return true
}

// reviewUnit checks a present unit identity.
func reviewUnit(unit *string, field string, block, gap func(zzz.Issue)) bool {
	if unit == nil || *unit == "" {
		gap(newReviewIssue(field, "unitUnavailable", "单位未读取，不能完成校验"))
		return false
	}
	switch zzz.Unit(*unit) {
	case zzz.UnitFlat, zzz.UnitPercent:
		return true
	default:
		block(newReviewIssue(field, "unitInvalid",
			fmt.Sprintf("单位 %q 不是 flat 或 percent", *unit)))
		return false
	}
}

// reviewValue checks a present canonical decimal value.
func reviewValue(value *string, field string, block, gap func(zzz.Issue)) bool {
	if value == nil || *value == "" {
		gap(newReviewIssue(field, "valueUnavailable", "数值未读取，不能完成校验"))
		return false
	}
	if !ValidDecimal(*value) {
		block(newReviewIssue(field, zzz.CodeAffixValueInvalid,
			fmt.Sprintf("数值 %q 不是规范的非负十进制", *value)))
		return false
	}
	return true
}

// reviewValueLine renders one affix value the way the detail page shows
// it: a percent value carries its trailing percent sign.
func reviewValueLine(value, unit *string) string {
	if value == nil {
		return ""
	}
	if unit != nil && zzz.Unit(*unit) == zzz.UnitPercent {
		return *value + "%"
	}
	return *value
}

// remapParserIssue keeps a parser issue only for a line this validation
// synthesized and maps a synthetic substat index back to the data index.
func remapParserIssue(issue zzz.Issue, setSynth, levelSynth, mainSynth bool, origins []int) (zzz.Issue, bool) {
	switch {
	case issue.Field == "" || issue.Field == "setId" || issue.Field == "slot":
		return issue, setSynth
	case issue.Field == "level":
		return issue, levelSynth
	case issue.Field == "mainStat":
		return issue, mainSynth
	case strings.HasPrefix(issue.Field, "substats["):
		inside := strings.TrimSuffix(strings.TrimPrefix(issue.Field, "substats["), "]")
		index, err := strconv.Atoi(inside)
		if err != nil || index < 0 || index >= len(origins) {
			return zzz.Issue{}, false
		}
		issue.Field = fmt.Sprintf("substats[%d]", origins[index])
		return issue, true
	default:
		return issue, true
	}
}

// validManagementState accepts exactly the three real game states.
func validManagementState(state string) bool {
	switch state {
	case zzz.ManagementLocked, zzz.ManagementDiscarded, zzz.ManagementNone:
		return true
	}
	return false
}

// newReviewIssue builds one error-severity review issue.
func newReviewIssue(field, code, message string) zzz.Issue {
	return zzz.Issue{Field: field, Code: code, Severity: zzz.SeverityError, Message: message}
}

// newReviewWarning builds one non-blocking optional-field issue.
func newReviewWarning(field, code, message string) zzz.Issue {
	return zzz.Issue{Field: field, Code: code, Severity: zzz.SeverityWarning, Message: message}
}

// appendUnique appends issue unless an identical issue is already present.
func appendUnique(dst *[]zzz.Issue, seen map[string]struct{}, issue zzz.Issue) {
	key := issue.Field + "\x00" + issue.Code + "\x00" + string(issue.Severity) + "\x00" + issue.Message
	if _, ok := seen[key]; ok {
		return
	}
	seen[key] = struct{}{}
	*dst = append(*dst, issue)
}

// canonicalDecimalPattern is the scan JSON contract's canonical value
// syntax: non-negative, no leading zeros, no exponent, no trailing zero
// fraction.
var canonicalDecimalPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]*[1-9])?$`)

// ValidDecimal reports whether value is a canonical non-negative decimal
// string (the scan JSON contract's `value` syntax). It is exported so the
// editor can reject a malformed value beside its field before asking the
// service to save.
func ValidDecimal(value string) bool {
	return canonicalDecimalPattern.MatchString(value)
}

// structuredData reads the structured data object of an item payload.
// ok is false when the payload carries no structured interpretation yet.
func structuredData(payload json.RawMessage) (zzz.DiscData, bool) {
	var doc struct {
		Structured *struct {
			Data json.RawMessage `json:"data"`
		} `json:"structured"`
	}
	if len(payload) == 0 {
		return zzz.DiscData{}, false
	}
	if err := json.Unmarshal(payload, &doc); err != nil || doc.Structured == nil || len(doc.Structured.Data) == 0 {
		return zzz.DiscData{}, false
	}
	var data zzz.DiscData
	if err := json.Unmarshal(doc.Structured.Data, &data); err != nil {
		return zzz.DiscData{}, false
	}
	return data, true
}

// replaceStructured writes the edited data, issues and validation into
// the item's payload while keeping every other member — notably the raw
// recognized lines — byte-for-byte intact.
func replaceStructured(payload json.RawMessage, data zzz.DiscData, check DataCheck) (json.RawMessage, error) {
	doc := make(map[string]json.RawMessage)
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &doc); err != nil {
			return nil, fmt.Errorf("decode item payload: %w", err)
		}
		if doc == nil {
			doc = make(map[string]json.RawMessage)
		}
	}
	var structured map[string]json.RawMessage
	if raw, ok := doc["structured"]; ok && len(raw) > 0 {
		if err := json.Unmarshal(raw, &structured); err != nil {
			// A malformed structured object is replaced as a whole; the
			// rest of the payload stays untouched.
			structured = nil
		}
	}
	if structured == nil {
		structured = make(map[string]json.RawMessage)
	}
	dataRaw, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("encode structured data: %w", err)
	}
	issues := check.Issues
	if issues == nil {
		issues = []zzz.Issue{}
	}
	issuesRaw, err := json.Marshal(issues)
	if err != nil {
		return nil, fmt.Errorf("encode structured issues: %w", err)
	}
	validation := check.Validation
	if validation.Issues == nil {
		validation.Issues = []zzz.Issue{}
	}
	validationRaw, err := json.Marshal(validation)
	if err != nil {
		return nil, fmt.Errorf("encode structured validation: %w", err)
	}
	structured["data"] = dataRaw
	structured["issues"] = issuesRaw
	structured["validation"] = validationRaw
	structuredRaw, err := json.Marshal(structured)
	if err != nil {
		return nil, fmt.Errorf("encode structured payload: %w", err)
	}
	doc["structured"] = structuredRaw
	out, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("encode item payload: %w", err)
	}
	return out, nil
}

// diffDiscData lists the top-level data fields the edit changed, with
// their before and after JSON values for the revision's audit history.
func diffDiscData(before, after zzz.DiscData) []record.FieldChange {
	changes := make([]record.FieldChange, 0, 8)
	add := func(field string, oldValue, newValue any) {
		if reflect.DeepEqual(oldValue, newValue) {
			return
		}
		beforeRaw, err := json.Marshal(oldValue)
		if err != nil {
			return
		}
		afterRaw, err := json.Marshal(newValue)
		if err != nil {
			return
		}
		if bytes.Equal(beforeRaw, afterRaw) {
			return
		}
		changes = append(changes, record.FieldChange{
			Field:  field,
			Before: beforeRaw,
			After:  afterRaw,
		})
	}
	add("setId", before.SetID, after.SetID)
	add("rarity", before.Rarity, after.Rarity)
	add("slot", before.Slot, after.Slot)
	add("level", before.Level, after.Level)
	add("mainStat", before.MainStat, after.MainStat)
	add("substats", before.Substats, after.Substats)
	add("managementState", before.ManagementState, after.ManagementState)
	add("equipped", before.Equipped, after.Equipped)
	return changes
}
