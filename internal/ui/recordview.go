package ui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/game/zzz"
	"github.com/LoTwT/youkai/internal/record"
)

// RecordView is one scan record as the results area shows it: the record
// identity, the session's shell bookkeeping and one row per captured item,
// in capture order. The live record comes from the current (or last)
// session's saved checkpoint generations; a reopened one from the history
// list, with the recovery's interruption marks.
type RecordView struct {
	// ID is the record identifier (the session's), also the directory
	// name below sessions/ its evidence lives in.
	ID string
	// Live: the record of the session the service currently reports (or
	// last reported), as opposed to one reopened from history.
	Live bool
	// Interrupted: the recovery marked items interrupted — the process
	// died around a scan that never reached a normal end.
	Interrupted bool
	// InterruptedCount is the number of items so marked.
	InterruptedCount int
	// Shell is the session's bookkeeping (client, times, termination,
	// coverage).
	Shell record.SessionShell
	// EvidenceRemoved reports that the checkpoint cites no evidence at
	// all while it holds items: the screenshots were deleted (or are
	// missing). The field-quality and review state stay authoritative.
	EvidenceRemoved bool
	// Rows are the captured items in capture order; no item disappears
	// for its status.
	Rows []Row
	// Classes is the four-class count computed while the rows are built.
	// recordClassCounts uses it only before Rows exist.
	Classes app.ItemClassCounts
}

// Row is one captured item of a record: its processing outcome, its field
// quality, its observed lines and the evidence images backing them.
type Row struct {
	// Seq is the capture sequence number, 1..n in capture order.
	Seq int64
	// Status is the raw processing state: queued, running, produced,
	// failed or unprocessed.
	Status record.Status
	// Reason is why an unprocessed item never ran (cancelled or
	// interrupted).
	Reason record.UnprocessedReason
	// Failure is why recognition ended without a payload.
	Failure string
	// Quality is the field-quality state of the current revision.
	Quality record.Quality
	// ItemClass is the four-class display classification shared with the
	// export (app.ClassifyItem): pending backfill, pending review, warning
	// or passed. It is empty only for a row built without a record item,
	// as some unit tests do; ClassText then falls back to the status.
	ItemClass app.ItemClass
	// Revision is the persisted item revision the row shows. An edit is
	// saved against it, so a stale editor can only be rejected, never
	// overwrite a newer revision.
	Revision int64
	// Backfilled marks a failed or unprocessed item whose manual revision
	// supplied an interpretation. Its original Status, Failure and Reason
	// stay visible; the item's current class comes from app.ClassifyItem.
	Backfilled bool
	// Lines are the observed text lines, in observation order.
	Lines []Line
	// Evidence are the item's evidence images in capture order: line crops
	// in line order, then context crops. Each image is paired with its
	// observed role by the kind and index encoded in its id, never by its
	// position in the record: a crop skipped as empty or lost to a failed
	// write leaves its own slot without an image instead of shifting every
	// later image.
	Evidence []Evidence
	// Structured is the drive-disc interpretation of an item, as persisted
	// or backfilled in memory from its raw lines. It is nil for failed and
	// unprocessed items that have no manual interpretation and for payloads
	// without a drive-disc interpretation: those rows show no structured
	// fields rather than fabricated empty ones. A structured row may still
	// carry null fields (for example managementState or equipped), which
	// keep their own "not read" meaning.
	Structured *zzz.StructuredDisc
}

// EvidenceKind tells the two crop kinds apart: a recognized text line and a
// non-recognition context region.
type EvidenceKind string

const (
	// EvidenceLine is a text-line crop; its index is the observed line
	// index.
	EvidenceLine EvidenceKind = "line"
	// EvidenceContext is a context crop (rarity badge, selected cell border,
	// equipped-agent avatars); its index is the observed context index.
	EvidenceContext EvidenceKind = "context"
)

// Evidence is one evidence image of a row paired with the observation
// metadata it backs. The record names images <sequence>-line<i> and
// <sequence>-context<i>; the kind and index decoded from the id, not the
// image's position in the record, decide the pairing. A record that skips an
// empty crop or loses one to a write failure therefore keeps every later
// image on its own line.
type Evidence struct {
	// ID is the image identity within the record's evidence store.
	ID string
	// Kind is the crop kind decoded from ID; empty when ID does not carry
	// the deliverer's shape.
	Kind EvidenceKind
	// Index is the crop index within its kind.
	Index int
	// Role is the observed role ("name", "level", "main", "sub1"…"sub4",
	// "qualityBadge", "selectedCell", "detailAvatar", "listAvatar"); empty
	// when the record has no role metadata for the crop.
	Role string
	// Source is the crop's persisted provenance.
	Source string
}

// FormField identifies one field of the item form for evidence location.
// Values are the payload field paths the editor and its problem list already
// use (reviewFieldLabel), so a located field and a reported problem name the
// same field.
type FormField string

// The form fields an evidence crop can locate (R-07a). The remaining
// constants are the form's own field order.
const (
	FormFieldSetID           FormField = "setId"
	FormFieldRarity          FormField = "rarity"
	FormFieldSlot            FormField = "slot"
	FormFieldLevel           FormField = "level"
	FormFieldMainStat        FormField = "mainStat"
	FormFieldSubstats        FormField = "substats"
	FormFieldSubstat1        FormField = "substats[0]"
	FormFieldSubstat2        FormField = "substats[1]"
	FormFieldSubstat3        FormField = "substats[2]"
	FormFieldSubstat4        FormField = "substats[3]"
	FormFieldManagementState FormField = "managementState"
	FormFieldEquipped        FormField = "equipped"
)

// reviewFieldOrder is the form's reading order: the same order problems
// use, and the fallback when no problem names a field.
var reviewFieldOrder = []FormField{
	FormFieldMainStat,
	FormFieldSubstats,
	FormFieldSetID,
	FormFieldRarity,
	FormFieldSlot,
	FormFieldLevel,
	FormFieldManagementState,
	FormFieldEquipped,
}

// formFieldEvidence is the R-07a role → form field table, written
// field-first so a field without a crop stays explicit: the management state
// is confirmed from list badges and a top indicator that are not saved as
// per-field evidence, so its entry lists no role. The app layer keeps roles
// opaque (payload.go); this table is the only place that interprets them.
// Roles the form does not locate (qualityBadge, selectedCell) stay absent;
// the screenshot list still shows them.
var formFieldEvidence = []struct {
	field FormField
	roles []string
}{
	{FormFieldSetID, []string{"name"}},
	{FormFieldRarity, nil},
	{FormFieldSlot, nil},
	{FormFieldLevel, []string{"level"}},
	{FormFieldMainStat, []string{"main"}},
	{FormFieldSubstat1, []string{"sub1"}},
	{FormFieldSubstat2, []string{"sub2"}},
	{FormFieldSubstat3, []string{"sub3"}},
	{FormFieldSubstat4, []string{"sub4"}},
	// The group follows the rows so FormFieldForRole keeps returning the
	// specific row; the group's roles are for the "does this field have a
	// screenshot" check and the group click.
	{FormFieldSubstats, []string{"sub1", "sub2", "sub3", "sub4"}},
	{FormFieldEquipped, []string{"detailAvatar", "listAvatar"}},
	{FormFieldManagementState, nil},
}

// FormFieldForRole returns the form field an evidence role locates, and
// whether the table maps the role at all. A record whose roles are missing or
// unknown falls back to capture order without a highlight (R-07a).
func FormFieldForRole(role string) (FormField, bool) {
	for _, entry := range formFieldEvidence {
		for _, candidate := range entry.roles {
			if candidate == role {
				return entry.field, true
			}
		}
	}
	return "", false
}

// EvidenceRolesForField returns the evidence roles that back field, in
// screenshot order. nil means the field has no screenshot.
func EvidenceRolesForField(field FormField) []string {
	for _, entry := range formFieldEvidence {
		if entry.field == field {
			return entry.roles
		}
	}
	return nil
}

// Line is one observed text line of an item: its role and provenance from
// the sealed observed payload, and the recognized text with the raw model
// score from the result payload when the item produced one. The score is
// the model's confidence, not the probability the field is right.
type Line struct {
	Role   string
	Source string
	Text   string
	Score  float64
	Scored bool
}

// observedCrop is the persisted metadata of one observation region: its role
// and provenance. The pixels live in the evidence files.
type observedCrop struct {
	Role   string `json:"role"`
	Source string `json:"source,omitempty"`
}

// observedDoc is the persisted shape of an app.Observation: the line and
// context metadata in observation order, the pixels living in the evidence
// files. Records written before context crops existed carry only Lines.
type observedDoc struct {
	Lines   []observedCrop `json:"lines"`
	Context []observedCrop `json:"context,omitempty"`
}

// resultDoc is the persisted shape of an app.RecognitionResult: the raw
// recognized lines and, when the payload went through the drive-disc
// parser, its structured interpretation.
type resultDoc struct {
	Lines []struct {
		Role  string  `json:"role"`
		Text  string  `json:"text"`
		Score float64 `json:"score"`
	} `json:"lines"`
	Structured *zzz.StructuredDisc `json:"structured"`
}

// BuildRecordView flattens one loaded record into the results area's view.
// Payload shapes the application service authored are read defensively: a
// line that fails to parse simply keeps what did, never blocking the item
// from showing.
func BuildRecordView(id string, rec *record.Recovery, live bool) *RecordView {
	cp := &rec.Checkpoint
	view := &RecordView{
		ID:               id,
		Live:             live,
		Interrupted:      len(rec.Interrupted) > 0,
		InterruptedCount: len(rec.Interrupted),
		Shell:            cp.Session,
		EvidenceRemoved:  evidenceWasRemoved(cp),
	}
	for i := range cp.Items {
		view.Rows = append(view.Rows, buildRow(&cp.Items[i]))
	}
	view.Classes = app.ClassifyItems(cp.Items)
	return view
}

// evidenceWasRemoved reports that the checkpoint holds items but cites no
// evidence at all. A record captured without any crops (a development
// source) looks the same; both are shown as "已删除或缺失" rather than
// inventing images.
func evidenceWasRemoved(cp *record.Checkpoint) bool {
	if len(cp.Items) == 0 {
		return false
	}
	if len(cp.Evidence) > 0 || len(cp.Session.CoverageEvidenceIDs) > 0 {
		return false
	}
	for _, item := range cp.Items {
		if len(item.EvidenceIDs) > 0 {
			return false
		}
	}
	return true
}

// buildRow flattens one item into its results row. BuildRecordView and
// the save response share it, so a saved revision renders exactly like
// the same item loaded from disk.
func buildRow(it *record.ItemRecord) Row {
	row := Row{
		Seq:        it.Sequence,
		Status:     it.Status,
		Reason:     it.Reason,
		Failure:    it.Failure,
		Quality:    it.Quality,
		Revision:   it.Revision,
		Backfilled: it.Status != record.StatusProduced && app.ItemInterpreted(*it),
		ItemClass:  app.ClassifyItem(*it),
	}
	res := parseResult(it.Data)
	if it.Status == record.StatusProduced || row.Backfilled {
		row.Structured = res.Structured
		if row.Structured == nil && it.Status == record.StatusProduced {
			row.Structured = backfillStructured(it.Data, it.Observed)
		}
	}
	obs := parseObserved(it.Observed)
	row.Lines = mergeLines(obs, res)
	row.Evidence = pairEvidence(it.EvidenceIDs, it.Sequence, obs)
	return row
}

// parseObserved reads one item's observed payload defensively: a shape that
// fails to parse keeps the metadata it did, never blocking the item from
// showing.
func parseObserved(data json.RawMessage) observedDoc {
	var obs observedDoc
	if len(data) > 0 {
		_ = json.Unmarshal(data, &obs) // a bad shape leaves the regions it has
	}
	return obs
}

// pairEvidence decodes the evidence ids and attaches each crop's observed
// role. A repeated id keeps its first occurrence. The result is ordered by
// crop kind and index (lines, then context), not by the record's evidence
// order, so the screenshot list always follows the game panel's reading
// order. An id the deliverer's shape does not cover, including one that names
// another item's sequence, stays at the end without a role rather than
// disappearing.
func pairEvidence(ids []string, seq int64, obs observedDoc) []Evidence {
	evidence := make([]Evidence, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		ev := Evidence{ID: id}
		if kind, index, ok := parseEvidenceID(id, seq); ok {
			ev.Kind, ev.Index = kind, index
			regions := obs.Lines
			if kind == EvidenceContext {
				regions = obs.Context
			}
			if index < len(regions) {
				ev.Role = regions[index].Role
				ev.Source = regions[index].Source
			}
		}
		evidence = append(evidence, ev)
	}
	sort.SliceStable(evidence, func(i, j int) bool {
		a, b := evidence[i], evidence[j]
		if (a.Kind == "") != (b.Kind == "") {
			return a.Kind != ""
		}
		if a.Kind != b.Kind {
			return a.Kind == EvidenceLine
		}
		return a.Index < b.Index
	})
	return evidence
}

// parseEvidenceID decodes the crop kind and index from an evidence id of the
// deliverer's shape (<sequence>-line<i>, <sequence>-context<i>). ok is false
// for any other shape or for a sequence that names another item: the image is
// still shown, but the view does not guess where it belongs.
func parseEvidenceID(id string, seq int64) (kind EvidenceKind, index int, ok bool) {
	dash := strings.LastIndexByte(id, '-')
	if dash <= 0 || dash == len(id)-1 {
		return "", 0, false
	}
	sequence, valid := parseDecimalInt(id[:dash])
	if !valid || int64(sequence) != seq {
		return "", 0, false
	}
	suffix := id[dash+1:]
	switch {
	case strings.HasPrefix(suffix, string(EvidenceLine)):
		kind = EvidenceLine
		suffix = strings.TrimPrefix(suffix, string(EvidenceLine))
	case strings.HasPrefix(suffix, string(EvidenceContext)):
		kind = EvidenceContext
		suffix = strings.TrimPrefix(suffix, string(EvidenceContext))
	default:
		return "", 0, false
	}
	index, valid = parseDecimalInt(suffix)
	if !valid {
		return "", 0, false
	}
	return kind, index, true
}

// parseResult reads one item's result payload defensively: a shape that
// fails to parse keeps what did, never blocking the item from showing.
func parseResult(data json.RawMessage) resultDoc {
	var res resultDoc
	if len(data) > 0 {
		_ = json.Unmarshal(data, &res)
	}
	return res
}

// backfillStructured derives the structured payload of a record written
// before the structured interpretation existed. The derivation stays in
// memory: BuildRecordView never writes the checkpoint file and never
// persists the returned document. A payload without drive-disc roles or
// with a malformed shape keeps no structured data, so raw-only items are
// never given fabricated fields.
func backfillStructured(data, observed json.RawMessage) *zzz.StructuredDisc {
	backfilled, changed, err := app.BackfillStructured(data, observed)
	if err != nil || !changed {
		return nil
	}
	return parseResult(backfilled).Structured
}

// mergeLines zips an item's observed lines with its recognized ones by
// observation order.
func mergeLines(obs observedDoc, res resultDoc) []Line {
	n := len(obs.Lines)
	if len(res.Lines) > n {
		n = len(res.Lines)
	}
	if n == 0 {
		return nil
	}
	lines := make([]Line, n)
	for i := range lines {
		if i < len(obs.Lines) {
			lines[i].Role = obs.Lines[i].Role
			lines[i].Source = obs.Lines[i].Source
		}
		if i < len(res.Lines) {
			lines[i].Text = res.Lines[i].Text
			lines[i].Score = res.Lines[i].Score
			lines[i].Scored = true
		}
	}
	return lines
}

// LineEvidence returns the image backing observed text line i, or nil when
// the record has no image for it (an empty crop or a failed write). The
// pairing is decoded from the evidence id, so a missing image stays with its
// own line.
func (r Row) LineEvidence(i int) *Evidence {
	for j := range r.Evidence {
		if ev := &r.Evidence[j]; ev.Kind == EvidenceLine && ev.Index == i {
			return ev
		}
	}
	return nil
}

// RowOf returns the row of the capture sequence seq, or -1.
func (v *RecordView) RowOf(seq int64) int {
	if seq == 0 {
		return -1
	}
	for i := range v.Rows {
		if v.Rows[i].Seq == seq {
			return i
		}
	}
	return -1
}

// StatusText renders a processing state in the interface's language.
func (r Row) StatusText() string {
	if r.Backfilled {
		return "已补录（原：" + r.OriginalOutcomeLabel() + "）"
	}
	switch r.Status {
	case record.StatusProduced:
		return "已产出"
	case record.StatusFailed:
		return "失败"
	case record.StatusUnprocessed:
		switch r.Reason {
		case record.ReasonCancelled:
			return "未处理（用户取消）"
		case record.ReasonInterrupted:
			return "未处理（中断）"
		default:
			return "未处理"
		}
	case record.StatusQueued:
		return "排队"
	case record.StatusRunning:
		return "识别中"
	default:
		return string(r.Status)
	}
}

// OriginalOutcomeLabel names the preserved processing outcome without its
// detail, for a compact list label.
func (r Row) OriginalOutcomeLabel() string {
	switch r.Status {
	case record.StatusFailed:
		return "识别失败"
	case record.StatusUnprocessed:
		return "未处理"
	default:
		return r.StatusText()
	}
}

// OriginalOutcomeText includes the failure or interruption detail, for the
// backfill band and the raw/revision disclosure.
func (r Row) OriginalOutcomeText() string {
	switch r.Status {
	case record.StatusFailed:
		if r.Failure != "" {
			return "识别失败：" + r.Failure
		}
		return "识别失败"
	case record.StatusUnprocessed:
		switch r.Reason {
		case record.ReasonCancelled:
			return "未处理（用户取消）"
		case record.ReasonInterrupted:
			return "未处理（中断）"
		default:
			return "未处理"
		}
	default:
		return r.StatusText()
	}
}

// QualityText renders a field-quality state.
func (r Row) QualityText() string {
	switch r.Quality {
	case record.QualityAutoAccepted:
		return "自动接受"
	case record.QualityNeedsReview:
		if r.OptionalEquippedOnly() {
			return "自动接受（装备字段警告）"
		}
		return "待校对"
	case record.QualityHumanConfirmed:
		return "人工确认"
	case record.QualityNotEvaluated:
		return "未评价"
	default:
		return string(r.Quality)
	}
}

// ClassText is the item list's category label: the four-class
// classification the export and the summary counts use, with the
// backfill reason split the way the design requires ("识别失败" or
// "未处理"). A row built without a record item falls back to the
// processing outcome, never to an English class value.
func (r Row) ClassText() string {
	text := r.classText()
	if r.Backfilled {
		return "已补录 · " + text
	}
	return text
}

func (r Row) classText() string {
	switch r.ItemClass {
	case app.ItemClassPendingBackfill:
		switch r.Status {
		case record.StatusFailed:
			return "待补录 · 识别失败"
		case record.StatusUnprocessed:
			return "待补录 · 未处理"
		default:
			return "待补录"
		}
	case app.ItemClassPendingReview:
		return "待校对"
	case app.ItemClassWarning:
		return "有警告"
	case app.ItemClassPassed:
		return "已通过"
	}
	switch r.Status {
	case record.StatusFailed:
		return "待补录 · 识别失败"
	case record.StatusUnprocessed:
		return "待补录 · 未处理"
	}
	if r.PendingReview() {
		return "待校对"
	}
	return "已通过"
}

// lineRoleText names one observed line's role in the interface's language.
// A role the interface does not know keeps its position, never the raw
// internal identifier.
func lineRoleText(role string, index int) string {
	switch role {
	case "name":
		return "套装"
	case "level":
		return "等级"
	case "main":
		return "主词条"
	case "sub1":
		return "副词条 1"
	case "sub2":
		return "副词条 2"
	case "sub3":
		return "副词条 3"
	case "sub4":
		return "副词条 4"
	}
	return fmt.Sprintf("行 %d", index+1)
}

// contextRoleText names a non-text crop in the interface's language.
func contextRoleText(role string, index int) string {
	switch role {
	case "qualityBadge":
		return "稀有度"
	case "selectedCell":
		return "选中格"
	case "detailAvatar":
		return "装备者头像"
	case "listAvatar":
		return "列表头像"
	}
	return fmt.Sprintf("其他截图 %d", index+1)
}

// evidenceRoleText names any crop by its kind and role.
func evidenceRoleText(ev *Evidence) string {
	if ev == nil {
		return "截图"
	}
	if ev.Kind == EvidenceLine {
		return lineRoleText(ev.Role, ev.Index)
	}
	return contextRoleText(ev.Role, ev.Index)
}

// lineRoleRank is the game panel's reading order for the locatable line
// roles. ok is false for a role the list cannot place.
func lineRoleRank(role string) (int, bool) {
	switch role {
	case "name":
		return 0, true
	case "level":
		return 1, true
	case "main":
		return 2, true
	case "sub1":
		return 3, true
	case "sub2":
		return 4, true
	case "sub3":
		return 5, true
	case "sub4":
		return 6, true
	}
	return 0, false
}

// OptionalEquippedOnly reports the v1 revision-0 shape whose only gap was
// the now optional equipped field. It is presented as a settled record
// with a warning, without rewriting the checkpoint.
func (r Row) OptionalEquippedOnly() bool {
	return r.Revision == 0 && r.Structured != nil &&
		zzz.OnlyEquippedStateUnavailable(r.Structured.Validation, r.Structured.Issues)
}

// Summary is the one-line content of the results table: the first
// recognized text of a produced item, the failure of a failed one, the
// reason of an unprocessed one. Pending items show nothing rather than a
// guess.
func (r Row) Summary() string {
	switch r.Status {
	case record.StatusProduced:
		for _, l := range r.Lines {
			if l.Text != "" {
				return l.Text
			}
		}
		return ""
	case record.StatusFailed:
		return "识别失败：" + r.Failure
	case record.StatusUnprocessed:
		return r.StatusText()
	default:
		return ""
	}
}

// SetText is the item's set display name, without its numeric identity.
// It is empty when the row has no structured data; a structured row whose
// setId is null reports "未读取" rather than guessing a set.
func (r Row) SetText() string {
	if r.Structured == nil {
		return ""
	}
	setID := r.Structured.Data.SetID
	if setID == nil {
		return "未读取"
	}
	if set, ok := zzz.LookupSetByID(*setID); ok {
		return set.Name
	}
	return fmt.Sprintf("未知套装（%d）", *setID)
}

// SetIDText is the item's numeric fairy set id, or "未读取" when the
// structured payload has none. It is empty without structured data.
func (r Row) SetIDText() string {
	if r.Structured == nil {
		return ""
	}
	if r.Structured.Data.SetID == nil {
		return "未读取"
	}
	return fmt.Sprintf("%d", *r.Structured.Data.SetID)
}

// SetFormText is the read-only form's set field: the display name followed
// by the numeric identity, never the id alone as a name.
func (r Row) SetFormText() string {
	if r.Structured == nil {
		return ""
	}
	setID := r.Structured.Data.SetID
	if setID == nil {
		return "未读取"
	}
	if set, ok := zzz.LookupSetByID(*setID); ok {
		return fmt.Sprintf("%s（%d）", set.Name, *setID)
	}
	return fmt.Sprintf("未知套装（%d）", *setID)
}

// RarityText is the verified rarity, or "未读取" when the structured
// payload keeps it null. Rarity is never inferred from the set or level.
func (r Row) RarityText() string {
	if r.Structured == nil {
		return ""
	}
	if r.Structured.Data.Rarity == nil {
		return "未读取"
	}
	return *r.Structured.Data.Rarity
}

// SlotText is the item's 1–6 slot, or "未读取" when it is unknown.
func (r Row) SlotText() string {
	if r.Structured == nil {
		return ""
	}
	if r.Structured.Data.Slot == nil {
		return "未读取"
	}
	return fmt.Sprintf("%d 号位", *r.Structured.Data.Slot)
}

// LevelText is the item's 0–15 level, or "未读取" when it is unknown.
func (r Row) LevelText() string {
	if r.Structured == nil {
		return ""
	}
	if r.Structured.Data.Level == nil {
		return "未读取"
	}
	return fmt.Sprintf("%d 级", *r.Structured.Data.Level)
}

// SlotLevelText is the table's compact slot/level summary.
func (r Row) SlotLevelText() string {
	if r.Structured == nil {
		return ""
	}
	return r.SlotText() + " · " + r.LevelText()
}

// MainStatText is the item's main affix as attribute, value and unit.
func (r Row) MainStatText() string {
	if r.Structured == nil {
		return ""
	}
	return statText(r.Structured.Data.MainStat)
}

// SubstatTexts returns the substats in display order as attribute, value,
// unit and enhancement roll. known is false when the payload says the
// line count is unknown (substats null); an empty known list means the
// capture confirmed no visible substats.
func (r Row) SubstatTexts() (texts []string, known bool) {
	if r.Structured == nil || r.Structured.Data.Substats == nil {
		return nil, false
	}
	texts = make([]string, len(r.Structured.Data.Substats))
	for i, sub := range r.Structured.Data.Substats {
		texts[i] = substatText(sub)
	}
	return texts, true
}

// ManagementStateText renders the three real management states and the
// distinct not-read null: null is "未读取", never "无标记".
func (r Row) ManagementStateText() string {
	if r.Structured == nil {
		return ""
	}
	state := r.Structured.Data.ManagementState
	if state == nil {
		return "未读取"
	}
	switch *state {
	case "locked":
		return "锁定"
	case "discarded":
		return "弃置"
	case "none":
		return "无标记"
	default:
		return fmt.Sprintf("未知管理状态（%s）", *state)
	}
}

// EquippedText renders the optional equipper object. A null whole object
// is "未读取", never "未装备"; once read, unequipped and equipped stay
// distinct, and an equipped disc with no known agent says so.
func (r Row) EquippedText() string {
	if r.Structured == nil {
		return ""
	}
	equipped := r.Structured.Data.Equipped
	if equipped == nil {
		return "未读取"
	}
	switch equipped.State {
	case "unequipped":
		if equipped.AgentID != nil {
			return fmt.Sprintf("未装备（代理人 ID %d 与状态冲突）", *equipped.AgentID)
		}
		return "未装备"
	case "equipped":
		if equipped.AgentID == nil {
			return "已装备，代理人未知"
		}
		if agent, ok := zzz.LookupAgentByID(*equipped.AgentID); ok {
			return fmt.Sprintf("%s（%d）", agent.Name, *equipped.AgentID)
		}
		return fmt.Sprintf("已装备（代理人 %d 未知）", *equipped.AgentID)
	default:
		return fmt.Sprintf("未知装备状态（%s）", equipped.State)
	}
}

// ValidationText renders the record-level validation state. "未读取" is
// used only for a structured payload with an empty status.
func (r Row) ValidationText() string {
	if r.Structured == nil {
		return ""
	}
	switch r.Structured.Validation.Status {
	case zzz.ValidationPassed:
		return "通过"
	case zzz.ValidationFailed:
		return "失败"
	case zzz.ValidationUnavailable:
		if r.OptionalEquippedOnly() {
			return "通过（装备字段警告）"
		}
		return "不可用"
	case "":
		return "未读取"
	default:
		return fmt.Sprintf("未知（%s）", r.Structured.Validation.Status)
	}
}

// Issues returns the parsing and validation issues of a structured row,
// with exact duplicates shown once: validation repeats the parser issues
// when it fails, and the unavailable status adds its own explanation.
func (r Row) Issues() []zzz.Issue {
	if r.Structured == nil {
		return nil
	}
	out := append([]zzz.Issue(nil), r.Structured.Issues...)
	optionalEquipped := r.OptionalEquippedOnly()
	for _, issue := range r.Structured.Validation.Issues {
		if optionalEquipped && issue.Code == zzz.CodeEquippedStateUnavailable {
			issue.Severity = zzz.SeverityWarning
		}
		duplicate := false
		for _, have := range out {
			if have == issue {
				duplicate = true
				break
			}
		}
		if !duplicate {
			out = append(out, issue)
		}
	}
	return out
}

// IssueSummaryText is the table's compact issue count.
func (r Row) IssueSummaryText() string {
	if r.Structured == nil {
		return ""
	}
	return issueSummaryText(r.Issues())
}

// SetColumnText is the results table's set cell: "未解析" names a produced
// payload without a structured interpretation, and "—" leaves failed or
// unprocessed rows without a fabricated set.
func (r Row) SetColumnText() string {
	if r.Structured == nil {
		if r.Status == record.StatusProduced {
			return "未解析"
		}
		return "—"
	}
	return r.SetText()
}

// SlotLevelColumnText is the table's slot/level cell.
func (r Row) SlotLevelColumnText() string {
	if r.Structured == nil {
		return "—"
	}
	return r.SlotLevelText()
}

// statText renders one affix without dropping an unread part: attribute,
// value and unit each say "未读取" on their own.
func statText(stat *zzz.StatData) string {
	if stat == nil {
		return "未读取"
	}
	return attributeText(stat.Attribute) + " · " + valueText(stat.Value) + " · " + unitText(stat.Unit)
}

// substatText adds the displayed enhancement roll to a substat's affix.
func substatText(sub zzz.SubstatData) string {
	text := statText(&sub.StatData)
	switch {
	case sub.EnhancementRolls == nil:
		return text + " · 强化未读取"
	case *sub.EnhancementRolls == 0:
		return text + " · 无强化档位"
	default:
		return fmt.Sprintf("%s · 强化 +%d", text, *sub.EnhancementRolls)
	}
}

// attributeText resolves an attribute identifier to its zh-CN display
// name; an unknown identifier stays visible as an identifier rather than
// becoming a name.
func attributeText(attribute *string) string {
	if attribute == nil || *attribute == "" {
		return "未读取"
	}
	if name, ok := zzz.LookupAttributeName(zzz.Attribute(*attribute)); ok {
		return name
	}
	return fmt.Sprintf("未知属性（%s）", *attribute)
}

// valueText is the canonical displayed value, or "未读取".
func valueText(value *string) string {
	if value == nil || *value == "" {
		return "未读取"
	}
	return *value
}

// unitText renders the unit contract in the interface's language.
func unitText(unit *string) string {
	if unit == nil || *unit == "" {
		return "未读取"
	}
	switch zzz.Unit(*unit) {
	case zzz.UnitFlat:
		return "固定值"
	case zzz.UnitPercent:
		return "百分比"
	default:
		return fmt.Sprintf("未知单位（%s）", *unit)
	}
}

// MissingEvidence reports that no image backs this row. The record may
// have had its screenshots deleted; a produced row with lines but no
// images is marked as missing in the detail pane instead of showing an
// empty frame.
func (r Row) MissingEvidence() bool {
	return len(r.Evidence) == 0
}

// PendingReview reports whether the row still belongs to the review
// queue. The four-class classification already contains that decision, so
// a backfilled failed item behaves like any other pending item; the
// fallback keeps rows built without a record item working.
func (r Row) PendingReview() bool {
	if r.ItemClass != "" {
		return r.ItemClass == app.ItemClassPendingReview
	}
	if r.Status != record.StatusProduced || r.Structured == nil {
		return false
	}
	switch r.Quality {
	case record.QualityHumanConfirmed, record.QualityAutoAccepted:
		return false
	default:
		return !r.OptionalEquippedOnly()
	}
}

// Label is the results table's row text for assistive technology.
func (r Row) Label() string {
	return fmt.Sprintf("%d %s %s", r.Seq, r.StatusText(), r.Summary())
}
