package ui

import (
	"errors"
	"fmt"
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
	// rarity is not editable in this revision; it is carried from the
	// row so saving preserves the verified value.
	rarity *string

	data        zzz.DiscData
	fieldErrors []fieldProblem
	check       app.DataCheck
}

// navKind is a pending navigation the editor must resolve first.
type navKind int

const (
	navSelect navKind = iota
	navOpenHistory
	navShowLive
	navExit
)

// navigationRequest is a navigation deferred until unsaved input is saved
// or discarded.
type navigationRequest struct {
	kind     navKind
	seq      int64
	recordID string
}

// cloneDraft copies a draft including its substat slice.
func cloneDraft(d editDraft) editDraft {
	out := d
	out.Substats = append([]substatDraft(nil), d.Substats...)
	return out
}

// equal compares two drafts by value.
func (d editDraft) equal(other editDraft) bool {
	if d.SetID != other.SetID || d.Slot != other.Slot || d.Level != other.Level ||
		d.MainAttribute != other.MainAttribute || d.MainValue != other.MainValue ||
		d.MainUnit != other.MainUnit || d.SubstatsKnown != other.SubstatsKnown ||
		d.ManagementState != other.ManagementState || d.EquippedState != other.EquippedState ||
		d.AgentID != other.AgentID || len(d.Substats) != len(other.Substats) {
		return false
	}
	for i := range d.Substats {
		if d.Substats[i] != other.Substats[i] {
			return false
		}
	}
	return true
}

// newEditDraft turns one row's structured data into editor text.
func newEditDraft(data zzz.DiscData) editDraft {
	draft := editDraft{
		SetID:           unreadOption,
		MainAttribute:   unreadOption,
		MainUnit:        unreadOption,
		ManagementState: unreadOption,
		EquippedState:   unreadOption,
		AgentID:         unreadOption,
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
func (d editDraft) toData(rarity *string) (zzz.DiscData, []fieldProblem) {
	data := zzz.DiscData{Rarity: cloneStringPtr(rarity)}
	var problems []fieldProblem
	add := func(field, message string) {
		problems = append(problems, fieldProblem{Field: field, Message: message})
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
	data, problems := e.draft.toData(e.rarity)
	e.data = data
	e.fieldErrors = problems
	if len(problems) > 0 {
		e.check = app.DataCheck{Validation: zzz.Validation{Status: "", Issues: []zzz.Issue{}}}
		return
	}
	e.check = app.CheckDiscData(data)
}

func (e *editState) dirty() bool {
	return !e.draft.equal(e.original)
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

// startEdit opens the editable form on the selected produced row.
func (m *Model) startEdit() {
	if m.reviewSaving || m.rec == nil || m.selected < 0 || m.selected >= len(m.rec.Rows) {
		return
	}
	row := &m.rec.Rows[m.selected]
	if row.Status != record.StatusProduced || row.Structured == nil {
		return
	}
	draft := newEditDraft(row.Structured.Data)
	m.edit = &editState{
		recordID:     m.rec.ID,
		seq:          row.Seq,
		baseRevision: row.Revision,
		original:     cloneDraft(draft),
		draft:        draft,
		rarity:       cloneStringPtr(row.Structured.Data.Rarity),
	}
	m.edit.refresh()
	m.navPrompt = nil
	m.reviewErr = ""
	m.reviewNotice = ""
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
	m.reviewNotice = ""
	m.catchUpLive()
}

// requestNavigation runs a navigation at once, or holds it behind a
// prompt while the editor has unsaved input.
func (m *Model) requestNavigation(req navigationRequest) {
	if m.exitState == exitRunning || m.exitState == exitDone {
		return
	}
	if m.reviewSaving {
		m.reviewErr = "正在保存，完成前不能切换。"
		return
	}
	if m.editingDirty() {
		m.navPrompt = &req
		return
	}
	m.clearEdit()
	m.runNavigation(req)
}

// runNavigation performs a navigation whose editor, if any, is settled.
func (m *Model) runNavigation(req navigationRequest) {
	m.navPrompt = nil
	switch req.kind {
	case navSelect:
		if m.rec == nil {
			return
		}
		if index := m.rec.RowOf(req.seq); index >= 0 {
			m.selected = index
			m.reviewErr = ""
			m.reviewNotice = ""
		}
	case navOpenHistory:
		m.performOpenHistory(req.recordID)
	case navShowLive:
		m.performShowLive()
	case navExit:
		m.beginExit()
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
	seq := m.rec.Rows[m.selected].Seq
	m.selected = previous
	m.requestNavigation(navigationRequest{kind: navSelect, seq: seq})
}

// promptSaveAndContinue saves the editor, then runs the held navigation.
func (m *Model) promptSaveAndContinue() {
	if m.navPrompt == nil || m.reviewSaving {
		return
	}
	req := *m.navPrompt
	if m.edit == nil || !m.edit.dirty() {
		m.navPrompt = nil
		m.runNavigation(req)
		return
	}
	m.saveEdit(false, func() { m.runNavigation(req) })
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

// promptStay closes the prompt and keeps the editor open.
func (m *Model) promptStay() {
	if m.reviewSaving {
		return
	}
	m.navPrompt = nil
	m.reviewErr = ""
}

// saveEdit validates and submits the open editor. after runs only after a
// successful save (and after the editor settled), never on failure.
func (m *Model) saveEdit(confirm bool, after func()) {
	if m.edit == nil || m.reviewSaving {
		return
	}
	m.edit.refresh()
	if len(m.edit.fieldErrors) > 0 || len(m.edit.check.Blocking) > 0 {
		m.reviewErr = "请先修正标出的字段错误，未保存。"
		return
	}
	if confirm && !m.edit.check.Confirmable() {
		m.reviewErr = "必需字段仍未读取或校验未通过，不能确认。"
		return
	}
	if !confirm && !m.edit.dirty() {
		m.reviewNotice = "没有修改需要保存。"
		return
	}
	m.submitReview(m.edit.recordID, m.edit.seq, m.edit.baseRevision, m.edit.data, confirm, after)
}

// confirmCurrent confirms the selected row as it stands, without opening
// the editor.
func (m *Model) confirmCurrent() {
	if m.reviewSaving || m.rec == nil || m.selected < 0 || m.selected >= len(m.rec.Rows) {
		return
	}
	row := &m.rec.Rows[m.selected]
	if row.Status != record.StatusProduced || row.Structured == nil {
		return
	}
	check := app.CheckDiscData(row.Structured.Data)
	if !check.Confirmable() {
		m.reviewErr = "当前修订还有未读取或未通过的字段，不能确认。"
		return
	}
	m.submitReview(m.rec.ID, row.Seq, row.Revision, row.Structured.Data, true, nil)
}

// submitReview schedules one save/confirmation. The captured identity and
// serial mean a late response can only update the item it was sent for.
func (m *Model) submitReview(recordID string, seq, expectedRevision int64, data zzz.DiscData, confirm bool, after func()) {
	m.reviewSerial++
	serial := m.reviewSerial
	m.reviewSaving = true
	m.reviewErr = ""
	m.reviewNotice = ""
	m.lastConfirm = confirm
	m.goAsync(func() {
		result, err := m.cfg.Service.SaveItemRevision(recordID, seq, expectedRevision, app.ItemRevisionEdit{
			Data:      data,
			Confirmed: confirm,
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
				if after != nil {
					after()
				}
				return
			}
			m.applySavedItem(result.Item)
			m.recLoad++ // invalidate an older load started before the save
			if m.edit != nil && m.edit.recordID == recordID && m.edit.seq == seq {
				m.edit = nil
			}
			m.navPrompt = nil
			m.reviewErr = ""
			if confirm {
				next := m.nextPendingAfter(seq)
				if next >= 0 {
					m.selected = next
				}
				switch {
				case m.pendingCount() == 0:
					m.reviewNotice = "待校对为空"
				case next < 0:
					m.reviewNotice = "后面没有待校对项。"
				default:
					m.reviewNotice = fmt.Sprintf("已确认并定位下一项（序号 %d）。", m.rec.Rows[next].Seq)
				}
			} else {
				m.reviewNotice = fmt.Sprintf("已保存修订 %d。", result.Item.Revision)
			}
			m.catchUpLive()
			if after != nil {
				after()
			}
		})
	})
}

// handleReviewError keeps the input on a save failure and discards it on
// a revision conflict, reloading the current revision instead.
func (m *Model) handleReviewError(recordID string, err error) {
	if errors.Is(err, record.ErrRevisionConflict) {
		m.edit = nil
		m.navPrompt = nil
		m.reviewErr = ""
		m.reviewNotice = "记录已被更新，正在重新加载当前修订；旧输入未保存。"
		if m.rec != nil && m.rec.ID == recordID {
			m.reloadRecord(recordID, m.rec.Live)
		}
		return
	}
	m.reviewErr = "保存失败：" + err.Error() + "。输入已保留，可重试。"
}

// applySavedItem replaces the saved item's row in place, locating it by
// its capture sequence rather than by the current selection.
func (m *Model) applySavedItem(item record.ItemRecord) {
	if m.rec == nil {
		return
	}
	index := m.rec.RowOf(item.Sequence)
	if index < 0 {
		return
	}
	m.rec.Rows[index] = buildRow(&item)
}

// pendingCount counts the produced structured rows whose review is not
// finished. Auto-accepted and human-confirmed rows are settled.
func (m *Model) pendingCount() int {
	if m.rec == nil {
		return 0
	}
	count := 0
	for _, row := range m.rec.Rows {
		if row.PendingReview() {
			count++
		}
	}
	return count
}

// pendingText is the results area's review summary.
func (m *Model) pendingText() string {
	if count := m.pendingCount(); count > 0 {
		return fmt.Sprintf("待校对 %d 件", count)
	}
	return "待校对为空"
}

// nextPendingAfter finds the next pending row after the given capture
// sequence, in the record's capture order. It never wraps around.
func (m *Model) nextPendingAfter(seq int64) int {
	if m.rec == nil {
		return -1
	}
	start := m.rec.RowOf(seq)
	if start < 0 {
		return -1
	}
	for i := start + 1; i < len(m.rec.Rows); i++ {
		if m.rec.Rows[i].PendingReview() {
			return i
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
