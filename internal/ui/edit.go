package ui

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/game/zzz"
	"github.com/LoTwT/youkai/internal/record"
)

// fieldProblem is one syntax or catalog error shown beside the form.
type fieldProblem struct {
	Field   string
	Message string
}

// substatDraft is the editable text of one substat line.
type substatDraft struct {
	Attribute string
	Value     string
	Unit      string
	Rolls     string
}

// editDraft is the complete text the editor shows. Every value is kept as
// the user sees it — a decimal string or a picker label — so a failed
// save can keep the input byte-for-byte.
type editDraft struct {
	Rarity          string
	SetID           string
	Slot            string
	Level           string
	MainAttribute   string
	MainValue       string
	MainUnit        string
	SubstatsKnown   bool
	Substats        []substatDraft
	ManagementState string
	EquippedState   string
	AgentID         string
}

// editState is one item's open editor. Its identity (recordID, seq,
// baseRevision) is stable for the whole edit, so an asynchronous save can
// never land on a later selection.
type editState struct {
	recordID     string
	seq          int64
	baseRevision int64
	original     editDraft
	draft        editDraft
	// backfill marks an editor opened on a failed or unprocessed item (or
	// a produced payload without an interpretation): saving completes the
	// item instead of revising a produced one.
	backfill bool
	// modify marks an editor opened by the read-only form's 修改 action on
	// a settled item (08 §3.4). Its only settling actions are 确认修改 and
	// 取消修改: a plain save would drop the item's confirmation even though
	// F-20 requires a re-confirmation of every changed, confirmed item.
	modify bool

	data        zzz.DiscData
	fieldErrors []fieldProblem
	check       app.DataCheck
	// firstFields is the problem-first field order the edit opened with.
	// It stays fixed for the whole edit session: a validation change while
	// the user types must not move the edited field out from under the
	// keyboard or into the folded group (M5).
	firstFields []FormField
}

// reviewAction is one save/confirmation the results page can request. A
// confirmation may stay on the current item (the secondary action) or move
// to the next pending one (the primary action); a plain save never changes
// the item's review quality.
type reviewAction int

const (
	reviewSave reviewAction = iota
	reviewConfirmStay
	reviewConfirmNext
	// reviewConfirmModify re-confirms a settled item edited through the
	// read-only form's 修改 action (08 §3.4). It confirms like any other
	// confirmation and leaves the item read-only in place.
	reviewConfirmModify
)

// confirmed reports whether the action settles the item's review.
func (a reviewAction) confirmed() bool {
	return a == reviewConfirmStay || a == reviewConfirmNext || a == reviewConfirmModify
}

// navKind is a pending navigation the editor must resolve first.
type navKind int

const (
	navSelect navKind = iota
	navOpenRecord
	navPage
	navExit
	navStartScan
	navResultFilter
	navSummaryClass
	navRecordTab
	navExport
	// navPending switches to the pending segment, clears facets and selects
	// one pending row. Ctrl+J uses it when the current segment shows no
	// pending item but the record still has one (08 §3.4).
	navPending
)

// navigationRequest is a navigation deferred until unsaved input is saved
// or discarded.
type navigationRequest struct {
	kind     navKind
	seq      int64
	recordID string
	// page is the destination of a navPage request.
	page Page
	// The filter fields belong to navResultFilter / navSummaryClass; the
	// summary class is also used only by navSummaryClass.
	filter       resultFilter
	setFilter    string
	slotFilter   string
	summaryClass app.ItemClass
	recordTab    recordTab
	// exportAfterLoad opens the export dialog once a navOpenRecord request
	// has loaded its record: the records list's 导出… means "open this
	// record, then export it", not "export whatever is on screen".
	exportAfterLoad bool
}

// cloneDraft copies a draft including its substat slice.
func cloneDraft(d editDraft) editDraft {
	out := d
	if d.Substats != nil {
		// Preserve an explicitly confirmed empty list: a nil slice would
		// mean "count unknown" in the saved data and would also make an
		// untouched draft compare as dirty (PR-19b round 2).
		out.Substats = make([]substatDraft, len(d.Substats))
		copy(out.Substats, d.Substats)
	}
	return out
}

// newEditDraft turns one row's structured data into editor text.
func newEditDraft(data zzz.DiscData) editDraft {
	draft := editDraft{
		Rarity:          unreadOption,
		SetID:           unreadOption,
		MainAttribute:   unreadOption,
		MainUnit:        unreadOption,
		ManagementState: unreadOption,
		EquippedState:   unreadOption,
		AgentID:         unreadOption,
	}
	if data.Rarity != nil {
		draft.Rarity = *data.Rarity
	}
	if data.SetID != nil {
		draft.SetID = describeSet(*data.SetID)
	}
	draft.Slot = intOption(data.Slot)
	draft.Level = intOption(data.Level)
	if data.MainStat != nil {
		if data.MainStat.Attribute != nil && *data.MainStat.Attribute != "" {
			draft.MainAttribute = describeAttribute(*data.MainStat.Attribute)
		}
		if data.MainStat.Value != nil {
			draft.MainValue = *data.MainStat.Value
		}
		if data.MainStat.Unit != nil && *data.MainStat.Unit != "" {
			draft.MainUnit = describeUnit(*data.MainStat.Unit)
		}
	}
	if data.Substats != nil {
		draft.SubstatsKnown = true
		draft.Substats = make([]substatDraft, len(data.Substats))
		for i, sub := range data.Substats {
			if sub.Attribute != nil && *sub.Attribute != "" {
				draft.Substats[i].Attribute = describeAttribute(*sub.Attribute)
			} else {
				draft.Substats[i].Attribute = unreadOption
			}
			if sub.Value != nil {
				draft.Substats[i].Value = *sub.Value
			}
			if sub.Unit != nil && *sub.Unit != "" {
				draft.Substats[i].Unit = describeUnit(*sub.Unit)
			} else {
				draft.Substats[i].Unit = unreadOption
			}
			if sub.EnhancementRolls != nil {
				draft.Substats[i].Rolls = strconv.Itoa(*sub.EnhancementRolls)
			}
		}
	}
	if data.ManagementState != nil && *data.ManagementState != "" {
		draft.ManagementState = describeManagement(*data.ManagementState)
	}
	if data.Equipped != nil {
		if data.Equipped.State != "" {
			draft.EquippedState = describeEquipped(data.Equipped.State)
		}
		if data.Equipped.AgentID != nil {
			draft.AgentID = describeAgent(*data.Equipped.AgentID)
		}
	}
	return draft
}

// toData parses the editor text into the data object it represents. A
// field problem means the save is refused until the user fixes it; an
// empty part stays an explicit null.
func (d editDraft) toData(_ ...*string) (zzz.DiscData, []fieldProblem) {
	data := zzz.DiscData{}
	var problems []fieldProblem
	add := func(field, message string) {
		problems = append(problems, fieldProblem{Field: field, Message: message})
	}

	if value := optionValue(d.Rarity); value != "" {
		if value != zzz.RarityS {
			add("rarity", fmt.Sprintf("稀有度 %q 不是本修订可确认的 S", value))
		} else {
			data.Rarity = stringPtr(value)
		}
	}

	if value := optionValue(d.SetID); value != "" {
		id, ok := parseDecimalInt(value)
		if !ok {
			add("setId", "套装 ID 必须是整数")
		} else if _, found := zzz.LookupSetByID(id); !found {
			add("setId", fmt.Sprintf("套装 ID %d 不在资料表中", id))
		} else {
			data.SetID = &id
		}
	}

	if value, problem := parseOptionalInt(d.Slot, "slot", "槽位", 1, 6); problem != nil {
		add(problem.Field, problem.Message)
	} else {
		data.Slot = value
	}
	if value, problem := parseOptionalInt(d.Level, "level", "等级", 0, 15); problem != nil {
		add(problem.Field, problem.Message)
	} else {
		data.Level = value
	}

	mainAttribute := optionValue(d.MainAttribute)
	mainValue := strings.TrimSpace(d.MainValue)
	mainUnit := optionValue(d.MainUnit)
	switch {
	case mainAttribute == "" && mainValue == "" && mainUnit == "":
		// Explicitly unread.
	default:
		valid := true
		if mainAttribute != "" {
			if _, ok := zzz.LookupAttributeName(zzz.Attribute(mainAttribute)); !ok {
				add("mainStat", fmt.Sprintf("词条类型 %q 不在资料表中", mainAttribute))
				valid = false
			}
		}
		if mainUnit != "" && mainUnit != string(zzz.UnitFlat) && mainUnit != string(zzz.UnitPercent) {
			add("mainStat", fmt.Sprintf("单位 %q 不是 flat 或 percent", mainUnit))
			valid = false
		}
		if mainValue != "" && !app.ValidDecimal(mainValue) {
			add("mainStat", "数值需为规范的非负十进制（如 19.2）")
			valid = false
		}
		if valid {
			data.MainStat = &zzz.StatData{}
			if mainAttribute != "" {
				data.MainStat.Attribute = stringPtr(mainAttribute)
			}
			if mainValue != "" {
				data.MainStat.Value = stringPtr(mainValue)
			}
			if mainUnit != "" {
				data.MainStat.Unit = stringPtr(mainUnit)
			}
		}
	}

	if d.SubstatsKnown {
		data.Substats = make([]zzz.SubstatData, 0, len(d.Substats))
		for i, sub := range d.Substats {
			field := fmt.Sprintf("substats[%d]", i)
			attribute := optionValue(sub.Attribute)
			value := strings.TrimSpace(sub.Value)
			unit := optionValue(sub.Unit)
			rolls := strings.TrimSpace(sub.Rolls)
			if attribute == "" && value == "" && unit == "" && rolls == "" {
				// A line exists but nothing on it was read: keep the
				// object with explicit nulls instead of dropping the row.
				data.Substats = append(data.Substats, zzz.SubstatData{})
				continue
			}
			valid := true
			if attribute != "" {
				if _, ok := zzz.LookupAttributeName(zzz.Attribute(attribute)); !ok {
					add(field, fmt.Sprintf("词条类型 %q 不在资料表中", attribute))
					valid = false
				}
			}
			if unit != "" && unit != string(zzz.UnitFlat) && unit != string(zzz.UnitPercent) {
				add(field, fmt.Sprintf("单位 %q 不是 flat 或 percent", unit))
				valid = false
			}
			if value != "" && !app.ValidDecimal(value) {
				add(field, "数值需为规范的非负十进制（如 9.6）")
				valid = false
			}
			var rollValue *int
			if rolls != "" {
				n, ok := parseDecimalInt(rolls)
				if !ok || n < 0 || n > 5 {
					add(field, "强化档位需为 0–5 的整数，留空表示未读取")
					valid = false
				} else {
					rollValue = &n
				}
			}
			if !valid {
				continue
			}
			line := zzz.SubstatData{EnhancementRolls: rollValue}
			if attribute != "" {
				line.Attribute = stringPtr(attribute)
			}
			if value != "" {
				line.Value = stringPtr(value)
			}
			if unit != "" {
				line.Unit = stringPtr(unit)
			}
			data.Substats = append(data.Substats, line)
		}
	}

	if value := optionValue(d.ManagementState); value != "" {
		switch value {
		case zzz.ManagementLocked, zzz.ManagementDiscarded, zzz.ManagementNone:
			data.ManagementState = stringPtr(value)
		default:
			add("managementState", fmt.Sprintf("管理状态 %q 不是 locked、discarded 或 none", value))
		}
	}

	equippedState := optionValue(d.EquippedState)
	agentLabel := optionValue(d.AgentID)
	switch equippedState {
	case "":
		if agentLabel != "" {
			add("equipped", "装备状态未读取时不能填写代理人")
		}
	case string(zzz.EquippedStateUnequipped):
		if agentLabel != "" {
			add("equipped", "未装备时不能填写代理人")
		}
		data.Equipped = &zzz.Equipped{State: zzz.EquippedStateUnequipped}
	case string(zzz.EquippedStateEquipped):
		var agentID *int
		if agentLabel != "" {
			id, ok := parseDecimalInt(agentLabel)
			if !ok {
				add("equipped", "代理人 ID 必须是整数")
			} else if _, found := zzz.LookupAgentByID(id); !found {
				add("equipped", fmt.Sprintf("代理人 ID %d 不在资料表中", id))
			} else {
				agentID = &id
			}
		}
		data.Equipped = &zzz.Equipped{State: zzz.EquippedStateEquipped, AgentID: agentID}
	default:
		add("equipped", fmt.Sprintf("装备状态 %q 不是 equipped 或 unequipped", equippedState))
	}

	return data, problems
}

// refresh recomputes the parsed data and its validation for the current
// text. It runs every frame while the editor is open.
func (e *editState) refresh() {
	data, problems := e.draft.toData()
	e.data = data
	e.fieldErrors = problems
	if len(problems) > 0 {
		e.check = app.DataCheck{Validation: zzz.Validation{Status: "", Issues: []zzz.Issue{}}}
		return
	}
	e.check = app.CheckDiscData(data)
}

func (e *editState) dirty() bool {
	// Compare the data the revision would persist, not the text: "015",
	// " 15" and "19.2 " are the same values as their canonical forms and
	// must not manufacture a zero-change revision (08 §3.4).
	current, problems := e.draft.toData()
	original, originalProblems := e.original.toData()
	if len(problems) > 0 || len(originalProblems) > 0 {
		// An invalid value has no data representation. Compare the texts
		// instead, but only while an invalid value exists: a saved invalid
		// value that the user did not touch is still unchanged (L-B).
		return !reflect.DeepEqual(e.draft, e.original)
	}
	return !reflect.DeepEqual(current, original)
}

func (e *editState) canSave() bool {
	return e.dirty() && e.valid()
}

func (e *editState) valid() bool {
	return len(e.fieldErrors) == 0 && len(e.check.Blocking) == 0
}

func (e *editState) canConfirm() bool {
	return e.valid() && e.check.Confirmable()
}

// parseOptionalInt parses a nullable integer text within [min, max].
func parseOptionalInt(text, field, what string, min, max int) (*int, *fieldProblem) {
	value := strings.TrimSpace(text)
	if value == "" {
		return nil, nil
	}
	number, ok := parseDecimalInt(value)
	if !ok {
		return nil, &fieldProblem{Field: field, Message: what + "必须是整数"}
	}
	if number < min || number > max {
		return nil, &fieldProblem{Field: field, Message: fmt.Sprintf("%s %d 越界（合法 %d–%d）", what, number, min, max)}
	}
	return &number, nil
}

// parseDecimalInt parses a non-negative ASCII decimal.
func parseDecimalInt(text string) (int, bool) {
	if text == "" {
		return 0, false
	}
	for i := 0; i < len(text); i++ {
		if text[i] < '0' || text[i] > '9' {
			return 0, false
		}
	}
	value, err := strconv.Atoi(text)
	if err != nil {
		return 0, false
	}
	return value, true
}

func stringPtr(value string) *string { return &value }

func cloneStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	out := *value
	return &out
}

// editingDirty reports whether the current editor holds unsaved input.
func (m *Model) editingDirty() bool {
	return m.edit != nil && m.edit.dirty() && !m.reviewSaving
}

// startEdit opens the editable form on the selected item. A produced item
// with an interpretation is revised; a failed, unprocessed or unparsed
// item starts as a backfill with every field explicitly unread.
func (m *Model) startEdit() {
	if m.reviewActionsBlocked() || m.rec == nil || m.selected < 0 || m.selected >= len(m.rec.Rows) {
		return
	}
	m.startEditRow(&m.rec.Rows[m.selected])
}

// startEditRow opens the editor for one row. The caller supplies the row
// so the view can ensure an editor exists for the item it just selected.
func (m *Model) startEditRow(row *Row) {
	if m.reviewActionsBlocked() || m.rec == nil || row == nil {
		return
	}
	if row.Status == record.StatusQueued || row.Status == record.StatusRunning {
		return
	}
	backfill := row.Structured == nil
	var data zzz.DiscData
	if !backfill {
		data = row.Structured.Data
	}
	draft := newEditDraft(data)
	m.edit = &editState{
		recordID:     m.rec.ID,
		seq:          row.Seq,
		baseRevision: row.Revision,
		original:     cloneDraft(draft),
		draft:        draft,
		backfill:     backfill,
	}
	m.edit.refresh()
	if first, _ := reviewFormFields(m.reviewProblems(row)); first != nil {
		m.edit.firstFields = first
	}
	m.abandonNavigationPrompt()
	m.reviewErr = ""
	m.reviewSaveFailed = false
	m.lastAction = reviewSave
}

// startModify opens the editor from the read-only form's 修改 action. The
// action may only target the row the workspace currently shows and selects,
// so an editor can never name an item other than the displayed one.
func (m *Model) startModify(row *Row) {
	if row == nil || m.rec == nil || m.reviewActionsBlocked() || !m.canModifyRow(row) {
		return
	}
	if m.selectedRow() != row || !m.resultRowMatches(row) {
		return
	}
	m.startEditRow(row)
	if m.edit == nil || m.edit.seq != row.Seq {
		return
	}
	m.edit.modify = true
	m.lastAction = reviewConfirmModify
	// 08 §3.4: 修改 moves the keyboard to the first field.
	m.requestReviewFieldFocus(m.firstReviewField())
}

// cancelEdit closes the 修改 flow without saving and returns the keyboard
// to the read-only form's 修改 action.
func (m *Model) cancelEdit() {
	if m.reviewSaving || m.edit == nil || !m.edit.modify {
		return
	}
	m.discardEdit()
	m.requestModifyFocus()
}

// canModifyRow reports whether the read-only form offers 修改 for a row.
// A queued or running item has no settled revision to revise.
func (m *Model) canModifyRow(row *Row) bool {
	if row == nil {
		return false
	}
	return row.Status != record.StatusQueued && row.Status != record.StatusRunning
}

// ensureReviewEdit keeps the selected item's editor open while its row is
// pending or needs backfill. A dirty editor is never replaced here: any
// selection or filter change that would leave it must pass through
// requestNavigation first.
func (m *Model) ensureReviewEdit(row *Row) {
	if m.rec == nil || row == nil {
		return
	}
	if m.loading || m.recErr != "" {
		// The visible row is a stale snapshot or unreadable: opening an
		// editor on it would let a save target it.
		return
	}
	if !m.reviewRowEditable(row) {
		return
	}
	if m.edit != nil && m.edit.recordID == m.rec.ID && m.edit.seq == row.Seq {
		return
	}
	if m.editingDirty() {
		// A selection change must pass through requestNavigation first; never
		// replace a dirty editor from the filter/render path.
		return
	}
	m.startEditRow(row)
}

// reviewRowEditable reports whether the item's form opens as an editor
// without a separate 修改 step. Every item a person still has to handle is
// editable at once; a settled item — passed, or exportable with a warning —
// stays read-only until 修改 (08 §3.4).
func (m *Model) reviewRowEditable(row *Row) bool {
	if row == nil {
		return false
	}
	if row.Status == record.StatusQueued || row.Status == record.StatusRunning {
		return false
	}
	switch row.ItemClass {
	case app.ItemClassPendingReview, app.ItemClassPendingBackfill:
		return true
	case app.ItemClassWarning, app.ItemClassPassed:
		return false
	}
	// Rows built without a class (some tests, and a not-yet-classified
	// payload) keep the old structural rule.
	if row.Status != record.StatusProduced || row.Structured == nil {
		return true
	}
	return row.PendingReview()
}

// clearEdit closes the editor without saving.
func (m *Model) clearEdit() {
	m.edit = nil
}

// discardEdit drops the unsaved input and its navigation guard.
func (m *Model) discardEdit() {
	m.clearEdit()
	m.navPrompt = nil
	m.reviewErr = ""
	m.reviewSaveFailed = false
	m.clearReviewNotice()
	m.lastAction = reviewSave
	m.catchUpLive()
}

// setReviewNotice records a result of the current page. Leaving the page
// clears it, so a save confirmation cannot reappear on settings later (L-D).
func (m *Model) setReviewNotice(text string) {
	m.reviewNotice = text
	m.reviewNoticeAcrossPages = false
	m.reviewNoticeKeep = false
}

// setReviewNoticeAcrossPages records the modify flow's "saved and back to
// review" consequence: every destination must keep explaining the queue
// (08 §3.4, 09 §4.1).
func (m *Model) setReviewNoticeAcrossPages(text string) {
	m.reviewNotice = text
	m.reviewNoticeAcrossPages = true
	m.reviewNoticeKeep = false
}

// clearReviewNotice drops the current result.
func (m *Model) clearReviewNotice() {
	m.reviewNotice = ""
	m.reviewNoticeAcrossPages = false
	m.reviewNoticeKeep = false
}

// requestNavigation runs a navigation at once, or holds it behind a
// prompt while the editor has unsaved input.
func (m *Model) requestNavigation(req navigationRequest) {
	if m.exitState == exitRunning || m.exitState == exitDone {
		return
	}
	if m.exitIntent != exitIntentNone {
		// The quit owns the next transition; another navigation must not
		// replace its prompt or run before the exit it asked for.
		return
	}
	if m.navPrompt != nil {
		// The open prompt owns the decision; a render-time selection or a
		// second navigation must not replace its target.
		return
	}
	if m.reviewSaving {
		m.reviewErr = "正在保存，完成前不能切换。"
		return
	}
	if m.editingDirty() {
		m.navPrompt = &req
		m.compactListOpen = false
		m.compactListFocusRequested = false
		return
	}
	m.clearEdit()
	m.runNavigation(req)
}

// runNavigation performs a navigation whose editor, if any, is settled.
func (m *Model) runNavigation(req navigationRequest) {
	m.navPrompt = nil
	if req.kind != navOpenRecord {
		// A row export waits for only the record it asked for. Leaving the
		// workspace (or opening the title bar's dialog) must not let that
		// load pop the dialog over another page.
		m.exportAfterLoadID = ""
	}
	switch req.kind {
	case navSelect:
		if m.rec == nil {
			return
		}
		if index := m.rec.RowOf(req.seq); index >= 0 {
			m.selected = index
			m.reviewErr = ""
			m.reviewSaveFailed = false
			m.clearReviewNotice()
			m.pointFirstReviewField()
		}
	case navResultFilter:
		m.applyResultFilterState(req.filter, req.setFilter, req.slotFilter)
	case navPending:
		m.applyResultFilterState(resultFilterPending, "", "")
		if m.rec == nil {
			return
		}
		if index := m.rec.RowOf(req.seq); index >= 0 {
			m.selected = index
			m.selectedSequence = m.rec.Rows[index].Seq
			if display := resultDisplayIndex(m.visibleResultRows(), index); display >= 0 {
				m.tableSelected = display
				m.list.ScrollIntoView(display)
			}
			m.reviewErr = ""
			m.reviewSaveFailed = false
			m.clearReviewNotice()
			m.pointFirstReviewField()
			m.focusBrowsedItem()
		}
	case navSummaryClass:
		m.openSummaryClassState(req.filter, req.summaryClass)
	case navRecordTab:
		if req.recordTab != m.recordTab && !m.reviewNoticeAcrossPages {
			// 摘要 and 逐件 are separate screens (08 §2, §3): an ordinary
			// action result from the item browser does not follow the user
			// into the summary (L-D).
			m.clearReviewNotice()
		}
		m.recordTab = req.recordTab
		if m.recordTab == recordTabItems {
			m.enterReviewView()
		}
	case navOpenRecord:
		m.page = pageResults
		m.performOpenRecord(req.recordID, req.exportAfterLoad)
	case navPage:
		if m.displayPage() == pageResults {
			// Leaving the results page stores the list's actual place, not
			// just the last captured one: the user may have scrolled the
			// ListState without any explicit capture.
			m.captureResultPlace()
			if req.page != pageResults && !m.reviewNoticeAcrossPages {
				// An ordinary action result belongs to the page that
				// produced it; only the modify flow's "back to review"
				// consequence follows the user (L-D, 08 §3.4).
				m.clearReviewNotice()
			}
		}
		m.page = req.page
	case navExit:
		m.continueExit()
	case navStartScan:
		m.Start()
	case navExport:
		m.openExportDialog()
	}
}

// applyTableSelection settles a selection the table already wrote. A
// different row with unsaved input is reverted and prompts; otherwise the
// move is allowed (or refused while a save is in flight).
func (m *Model) applyTableSelection(previous int) {
	if m.rec == nil || m.selected == previous || m.selected < 0 || m.selected >= len(m.rec.Rows) {
		m.selected = previous
		return
	}
	if m.navPrompt != nil {
		// A pending save/discard decision owns the selection.
		m.selected = previous
		m.tableSelected = resultDisplayIndex(m.visibleResultRows(), previous)
		return
	}
	seq := m.rec.Rows[m.selected].Seq
	m.selected = previous
	m.requestNavigation(navigationRequest{kind: navSelect, seq: seq})
}

// failedConfirmationAction reports a confirmation whose write failed while
// the draft itself has no unsaved changes. The save-and-continue prompt must
// retry this action, not treat the absence of edits as success and run the
// navigation. Only row confirmations qualify: a failed modify remains dirty
// and a plain save has no confirmation semantics to replay.
func (m *Model) failedConfirmationAction() (reviewAction, bool) {
	if !m.reviewSaveFailed || m.edit == nil || m.edit.dirty() {
		return reviewSave, false
	}
	switch m.lastAction {
	case reviewConfirmNext, reviewConfirmStay:
		return m.lastAction, true
	}
	return reviewSave, false
}

// promptSaveAndContinue saves the editor, then runs the held navigation.
// A clean editor may still hold a failed confirmation: retry that exact
// action so 保存并继续 cannot skip the confirmation and continue as if it
// succeeded. A modify draft saves without confirmation, so the item drops
// back into the review queue; the dialog stated that before the choice, and
// the destination notice repeats it when the workspace stays on the record.
func (m *Model) promptSaveAndContinue() {
	if m.navPrompt == nil || m.reviewSaving {
		return
	}
	req := *m.navPrompt
	if m.edit == nil || !m.edit.dirty() {
		if action, ok := m.failedConfirmationAction(); ok {
			m.saveEdit(action, func() { m.runNavigation(req) })
			return
		}
		m.navPrompt = nil
		m.runNavigation(req)
		return
	}
	modify := m.edit.modify
	seq := m.edit.seq
	m.saveEdit(reviewSave, func() {
		m.runNavigation(req)
		if !modify {
			return
		}
		switch req.kind {
		case navExit, navStartScan, navOpenRecord:
			// The workspace or application leaves this record; the dialog
			// is the only place the consequence can be shown.
		default:
			m.setReviewNoticeAcrossPages(fmt.Sprintf("已保存修改（序号 %d）；这件退回待校对，确认前不会导出。", seq))
		}
	})
}

// promptDiscardAndContinue drops the editor, then runs the held
// navigation.
func (m *Model) promptDiscardAndContinue() {
	if m.navPrompt == nil || m.reviewSaving {
		return
	}
	req := *m.navPrompt
	m.navPrompt = nil
	m.discardEdit()
	m.runNavigation(req)
}

// promptStay closes the prompt and keeps the editor open. Staying cancels
// a pending exit; it never leaves the intent to fire after a later save.
func (m *Model) promptStay() {
	if m.reviewSaving {
		return
	}
	if m.navPrompt != nil && m.navPrompt.kind == navExit {
		m.exitIntent = exitIntentNone
	}
	m.navPrompt = nil
}

// abandonNavigationPrompt closes a prompt whose question can no longer be
// answered, such as when its record is replaced or fails to load. The exit
// intent lives outside the prompt so an in-flight save can continue it;
// once the prompt is gone the intent must go too, or every navigation is
// refused and a later save exits without asking.
func (m *Model) abandonNavigationPrompt() {
	m.navPrompt = nil
	m.exitIntent = exitIntentNone
}

// editMatchesSelection reports the binding invariant: an editor may only
// target the row the workspace currently shows and has selected.
func (m *Model) editMatchesSelection() bool {
	row := m.selectedRow()
	return m.edit != nil && m.rec != nil && row != nil &&
		m.edit.recordID == m.rec.ID && m.edit.seq == row.Seq
}

// saveEdit validates and submits the open editor. after runs only after a
// successful save (and after the editor settled), never on failure.
func (m *Model) saveEdit(action reviewAction, after func()) {
	if m.edit == nil || m.reviewSaving {
		return
	}
	if !m.editMatchesSelection() {
		m.reviewErr = "编辑内容与当前查看的条目不一致，已取消保存。"
		return
	}
	if m.recErr != "" {
		m.reviewErr = "记录读取失败，不能保存或确认旧快照。"
		return
	}
	if m.loading {
		m.reviewErr = "正在加载最新检查点，加载完成前不能保存或确认。"
		return
	}
	if m.exportBusy() {
		m.reviewErr = "正在导出，完成前不能保存或确认。"
		return
	}
	if m.cleanupRunning() {
		m.reviewErr = "正在清理，完成前不能保存或确认。"
		return
	}
	m.lastAction = action
	m.edit.refresh()
	if len(m.edit.fieldErrors) > 0 || len(m.edit.check.Blocking) > 0 {
		m.reviewErr = "请先修正标出的字段错误，未保存。"
		return
	}
	if action.confirmed() && !m.edit.check.Confirmable() {
		m.reviewErr = "必需字段仍未读取或校验未通过，不能确认。"
		return
	}
	if action == reviewConfirmModify && !m.edit.dirty() {
		// 没有修改时“确认修改”不制造一个零变化的新修订（L1）。
		m.reviewErr = ""
		m.reviewSaveFailed = false
		m.setReviewNotice("没有修改需要确认。")
		return
	}
	if action == reviewSave && !m.edit.dirty() {
		m.reviewErr = ""
		m.reviewSaveFailed = false
		m.setReviewNotice("没有修改需要保存。")
		return
	}
	m.submitReview(m.edit.recordID, m.edit.seq, m.edit.baseRevision, m.edit.data, action, after)
}

// retryAction resolves the action the error band retries. A modify draft
// whose plain save failed retries as 确认修改, so the band can never write
// an unconfirmed revision behind the modify flow's back (F-20).
func (m *Model) retryAction() reviewAction {
	if m.edit != nil && m.edit.modify && m.lastAction == reviewSave {
		return reviewConfirmModify
	}
	return m.lastAction
}

// confirmCurrent confirms the selected row as it stands and moves to the
// next pending item when one follows.
func (m *Model) confirmCurrent() {
	m.confirmCurrentAction(reviewConfirmNext)
}

// confirmCurrentStay confirms the selected row without moving away from
// it. A non-final row stays in the pending segment, marked as confirmed,
// until the page is left or the record is reloaded.
func (m *Model) confirmCurrentStay() {
	m.confirmCurrentAction(reviewConfirmStay)
}

// confirmCurrentAction confirms the selected row as it stands, without
// opening the editor.
func (m *Model) confirmCurrentAction(action reviewAction) {
	if m.reviewSaving {
		return
	}
	if m.recErr != "" {
		// The displayed record is only the last good snapshot; a failed
		// load must not let a confirmation write it over the record on
		// disk.
		m.reviewErr = "记录读取失败，不能确认旧快照。"
		return
	}
	if m.loading {
		// The visible record is a stale snapshot until the latest
		// checkpoint lands. Confirming it would also clear the loading
		// guard and drop that checkpoint, so the action waits.
		m.reviewErr = "正在加载最新检查点，加载完成前不能确认。"
		return
	}
	if m.rec == nil || m.selected < 0 || m.selected >= len(m.rec.Rows) {
		return
	}
	if m.exportBusy() {
		m.reviewErr = "正在导出，完成前不能编辑或确认。"
		return
	}
	if m.cleanupRunning() {
		m.reviewErr = "正在清理，完成前不能编辑或确认。"
		return
	}
	m.lastAction = action
	if m.edit != nil && !m.editMatchesSelection() {
		m.reviewErr = "编辑内容与当前查看的条目不一致，已取消确认。"
		return
	}
	row := &m.rec.Rows[m.selected]
	if row.Status == record.StatusQueued || row.Status == record.StatusRunning ||
		row.Structured == nil || !row.PendingReview() {
		return
	}
	check := app.CheckDiscData(row.Structured.Data)
	if !check.Confirmable() {
		m.reviewErr = "当前修订还有未读取或未通过的字段，不能确认。"
		return
	}
	m.submitReview(m.rec.ID, row.Seq, row.Revision, row.Structured.Data, action, nil)
}

// submitReview schedules one save/confirmation. The captured identity and
// serial mean a late response can only update the item it was sent for.
func (m *Model) submitReview(recordID string, seq, expectedRevision int64, data zzz.DiscData, action reviewAction, after func()) {
	if m.rec == nil || m.rec.ID != recordID || m.selectedSeq() != seq {
		m.reviewErr = "编辑内容与当前查看的条目不一致，已取消写入。"
		return
	}
	m.reviewSerial++
	serial := m.reviewSerial
	m.reviewSaving = true
	m.reviewSaveFailed = false
	m.reviewErr = ""
	m.clearReviewNotice()
	m.lastAction = action
	m.goAsync(func() {
		result, err := m.cfg.Service.SaveItemRevision(recordID, seq, expectedRevision, app.ItemRevisionEdit{
			Data:      data,
			Confirmed: action.confirmed(),
		})
		m.update(func() {
			if serial != m.reviewSerial {
				return // a newer review action owns the state
			}
			m.reviewSaving = false
			if err != nil {
				m.handleReviewError(recordID, err)
				return
			}
			if m.rec == nil || m.rec.ID != recordID {
				// The record was replaced while the save ran; the saved
				// revision is durable but belongs to another view now.
				if m.edit != nil && m.edit.recordID == recordID && m.edit.seq == seq {
					m.edit = nil
				}
				if m.continueExitAfterSave() {
					return
				}
				if after != nil {
					after()
				}
				return
			}
			m.applySavedItem(result.Item, result.Generation)
			// Invalidate an older load started before the save: this
			// save's applied revision is the newest view. No current load
			// remains, so the loading guard must not outlive it.
			m.recLoad++
			m.loading = false
			if m.edit != nil && m.edit.recordID == recordID && m.edit.seq == seq {
				m.edit = nil
			}
			m.navPrompt = nil
			m.reviewErr = ""
			m.reviewSaveFailed = false
			switch action {
			case reviewConfirmNext:
				// The confirmed item stays in the pending list until the
				// user switches filters (08 §3.1): it is marked 已确认 and
				// the selection moves to the next row a person still has
				// to handle.
				m.markRecentlyConfirmed(seq)
				next := m.nextPendingAfter(seq)
				if next >= 0 {
					m.selected = next
					m.selectedSequence = m.rec.Rows[next].Seq
					// Keep the list's chosen row and scroll position on
					// the same visible row the guard chose.
					if display := resultDisplayIndex(m.visibleResultRows(), next); display >= 0 {
						m.tableSelected = display
						m.list.ScrollIntoView(display)
					}
					m.focusFirstReviewField()
				} else if m.pendingItemsCount() > 0 {
					// Pending work remains but none of it is in the
					// current segment. Drop a confirmed row that filtered
					// itself out instead of leaving a hidden selection.
					m.settleResultSelection()
					if display := resultDisplayIndex(m.visibleResultRows(), m.selected); display >= 0 {
						m.tableSelected = display
						m.list.ScrollIntoView(display)
					}
				}
				switch {
				case m.pendingItemsCount() == 0:
					// The empty pending form already states the completion;
					// an extra "待校对 0 · 全部可导出" line below it repeated
					// the same information (L-D).
					m.clearReviewNotice()
				case next < 0:
					m.setReviewNotice("后面没有待处理项。")
				default:
					m.setReviewNotice(fmt.Sprintf("已确认并定位下一项（序号 %d）。", m.rec.Rows[next].Seq))
				}
			case reviewConfirmStay:
				m.markRecentlyConfirmed(seq)
				if m.pendingItemsCount() == 0 {
					// The last pending item completes the review even
					// though the action asked to stay.
					m.clearReviewNotice()
					break
				}
				m.setReviewNotice(fmt.Sprintf("已确认（序号 %d），仍停留在当前项。", seq))
			case reviewConfirmModify:
				// The settled item stays in place, read-only again with its
				// re-confirmed revision. Its status tag names the new
				// quality without leaving the item. The visit marker keeps
				// a fixed warning row in the 警告 segment until the filter
				// changes (08 §3.1, §3.4).
				m.markRecentlyConfirmed(seq)
				m.setReviewNotice(fmt.Sprintf("已确认修改（序号 %d）。", seq))
				m.requestModifyFocus()
			default:
				m.forgetRecentlyConfirmed(seq)
				m.setReviewNotice(fmt.Sprintf("已保存修订 %d。", result.Item.Revision))
			}
			m.catchUpLive()
			if m.continueExitAfterSave() {
				return
			}
			if after != nil {
				after()
			}
		})
	})
}

// handleReviewError keeps the input on a save failure and discards it on
// a revision conflict, reloading the current revision instead.
func (m *Model) handleReviewError(recordID string, err error) {
	m.reviewSaveFailed = true
	if errors.Is(err, record.ErrRevisionConflict) {
		m.edit = nil
		m.navPrompt = nil
		m.exitIntent = exitIntentNone
		m.reviewErr = ""
		m.reviewSaveFailed = false
		m.setReviewNotice("记录已被更新，正在重新加载当前修订；旧输入未保存。")
		m.reviewNoticeKeep = true
		if m.rec != nil && m.rec.ID == recordID {
			m.reloadRecord(recordID, m.rec.Live)
		}
		return
	}
	if errors.Is(err, app.ErrRecordActive) {
		m.reviewErr = "扫描仍在写入该记录，扫描结束后再保存。输入已保留。"
		return
	}
	m.reviewErr = "保存失败：" + err.Error() + "。输入已保留，可重试。"
}

// settleConflictNotice replaces the in-progress conflict explanation once
// the reload it announced has settled. The notice stays pinned for the next
// navigation, but it must not keep claiming a load is still running (PR-19b
// review).
func (m *Model) settleConflictNotice(reloaded bool) {
	if !m.reviewNoticeKeep ||
		!strings.HasPrefix(m.reviewNotice, "记录已被更新，正在重新加载当前修订") {
		return
	}
	if reloaded {
		m.reviewNotice = "记录已被更新，已重新加载当前修订；旧输入未保存。"
		return
	}
	m.reviewNotice = "记录已被更新，重新加载失败；旧输入未保存。"
}

// applySavedItem replaces the saved item's row in place, locating it by
// its capture sequence rather than by the current selection. The frozen
// export snapshot advances with the same revision, so a following export
// uses exactly what the save persisted.
func (m *Model) applySavedItem(item record.ItemRecord, generation int64) {
	if m.rec == nil {
		return
	}
	index := m.rec.RowOf(item.Sequence)
	if index < 0 {
		return
	}
	m.rec.Rows[index] = buildRow(&item)
	if m.recData == nil || m.recData.Checkpoint.Session.SessionID != m.rec.ID {
		return
	}
	for i := range m.recData.Checkpoint.Items {
		if m.recData.Checkpoint.Items[i].Sequence == item.Sequence {
			m.recData.Checkpoint.Items[i] = item
			break
		}
	}
	m.recData.Checkpoint.SavedRevision = generation
	m.rebuildExport()
}

// nextPendingAfter finds the next pending row after the given capture
// sequence in the current filtered view. It walks the visible rows only,
// in record order, and wraps around at the end so a confirmed row can
// never move the selection outside the segment the user is looking at.
func (m *Model) nextPendingAfter(seq int64) int {
	if m.rec == nil {
		return -1
	}
	start := m.rec.RowOf(seq)
	if start < 0 {
		return -1
	}
	visible := m.visibleResultRows()
	if len(visible) == 0 {
		return -1
	}
	next := func(index int) int {
		if index == start {
			return -1
		}
		switch m.rec.Rows[index].ItemClass {
		case app.ItemClassPendingBackfill, app.ItemClassPendingReview:
			return index
		}
		return -1
	}
	for _, index := range visible {
		if index > start {
			if found := next(index); found >= 0 {
				return found
			}
		}
	}
	for _, index := range visible {
		if index <= start {
			if found := next(index); found >= 0 {
				return found
			}
		}
	}
	return -1
}

// catchUpLive reloads the live record when a scan saved new content while
// an edit kept the results area from reloading.
func (m *Model) catchUpLive() {
	if m.rec == nil || !m.rec.Live || m.rec.ID == "" || m.rec.ID != string(m.appliedSession) {
		return
	}
	if m.snap.SavedCount > m.liveSavedShown {
		m.liveSavedShown = m.snap.SavedCount
		m.reloadRecord(m.rec.ID, true)
	}
}

// validationStatusText renders a validation status.
func validationStatusText(status string) string {
	switch status {
	case zzz.ValidationPassed:
		return "通过"
	case zzz.ValidationFailed:
		return "失败"
	case zzz.ValidationUnavailable:
		return "不可用"
	case "":
		return "未读取"
	default:
		return fmt.Sprintf("未知（%s）", status)
	}
}
