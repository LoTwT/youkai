package ui

import (
	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/egoist/mygo/ui"
)

// resultSlotKind names the two results destinations. The live session and
// one reopened history record may both stay loaded; the active one is the
// content the results area renders.
type resultSlotKind int

const (
	resultSlotSession resultSlotKind = iota
	resultSlotHistory
)

// resultFilter is the results table's chosen segment. W2 stores it per
// slot; the filtering interactivity arrives with W5.
type resultFilter int

const (
	resultFilterPending resultFilter = iota
	resultFilterAll
	resultFilterUnexportable
)

// resultSlotState is everything one results destination remembers between
// visits. Model embeds the active state, so the existing rendering code
// keeps reading one record while the inactive slot holds its own copy.
type resultSlotState struct {
	// recLoad versions the slot's content: every load applies only while
	// its version is current, so a late response cannot replace a newer
	// record in the same slot.
	recLoad int64
	// rec is the slot's view, recData the checkpoint it was built from.
	// Exports freeze their revision from recData.
	rec     *RecordView
	recData *record.Recovery
	recErr  string
	// liveSavedShown is the live record's saved count the session slot last
	// reloaded from disk.
	liveSavedShown int

	// The slot's browser state: segment, search text, set and slot
	// filters, the chosen row and its stable capture sequence, and the
	// first visible row.
	filter           resultFilter
	search           string
	setFilter        string
	slotFilter       string
	selected         int
	selectedSequence int64
	scroll           int

	// The slot's frozen export snapshot and its outcome.
	exportData     *app.UsableExport
	exportBuildErr string
	exportNotice   string
	exportError    string
}

// resultSlot is one results destination: its identity and remembered
// state. A history slot names the opened record even when its load failed;
// the session slot's identity comes from the service snapshot.
type resultSlot struct {
	kind     resultSlotKind
	recordID string
	state    resultSlotState
}

// newResultSlotState is a slot's empty state: no record, no selection and
// the pending-review segment the design starts on.
func newResultSlotState() resultSlotState {
	return resultSlotState{
		filter:   resultFilterPending,
		selected: -1,
	}
}

// resultSlotFor returns the slot of one destination.
func (m *Model) resultSlotFor(kind resultSlotKind) *resultSlot {
	if kind == resultSlotHistory {
		return &m.historySlot
	}
	return &m.sessionSlot
}

// activeResultSlot returns the slot the results area currently renders.
func (m *Model) activeResultSlot() *resultSlot {
	return m.resultSlotFor(m.activeSlot)
}

// captureActiveResult stores the rendered state back into its slot. It runs
// before a switch so the destination keeps filters, selection, scroll and
// the export snapshot for its next visit.
func (m *Model) captureActiveResult() {
	if first, last := m.list.Visible(); last >= first {
		m.scroll = first
	}
	m.selectedSequence = m.selectedSeq()
	slot := m.activeResultSlot()
	slot.state = m.resultSlotState
}

// activateResultSlot makes kind the rendered destination. Navigation calls
// it only after the editor settled, so the previous slot never holds a
// draft while inactive.
func (m *Model) activateResultSlot(kind resultSlotKind) {
	if m.activeSlot != kind {
		m.captureActiveResult()
		m.activeSlot = kind
	}
	m.resultSlotState = m.resultSlotFor(kind).state
	m.resetResultList()
	m.edit = nil
	m.navPrompt = nil
	m.reviewErr = ""
	m.reviewNotice = ""
	m.lastConfirm = false
}

// resetResultList clears the table's per-destination scroll and selection
// internals before the slot's own place is restored.
func (m *Model) resetResultList() {
	m.list = ui.ListState{Selected: &m.selected}
	if m.rec == nil {
		m.selected = -1
		m.scroll = 0
		return
	}
	rows := len(m.rec.Rows)
	switch {
	case rows == 0:
		m.selected = -1
		m.scroll = 0
	case m.selected < 0 || m.selected >= rows:
		if index := m.rec.RowOf(m.selectedSequence); index >= 0 {
			m.selected = index
		} else {
			m.selected = 0
		}
	}
	if m.scroll < 0 {
		m.scroll = 0
	}
	if m.scroll >= rows && rows > 0 {
		m.scroll = rows - 1
	}
	if m.scroll > 0 {
		m.list.ScrollTo(m.scroll, ui.Start)
	}
}

// resetResultSlot replaces one slot's content with an empty view of id,
// invalidating any load still in flight for the slot.
func (m *Model) resetResultSlot(slot *resultSlot, id string, live bool) {
	version := slot.state.recLoad + 1
	slot.recordID = id
	slot.state = newResultSlotState()
	slot.state.recLoad = version
	slot.state.rec = &RecordView{ID: id, Live: live}
}

// showLiveSession makes the session slot the rendered destination with an
// empty view, ready for its saved generations to load.
func (m *Model) showLiveSession(id string) {
	if m.activeSlot != resultSlotSession {
		m.activateResultSlot(resultSlotSession)
	}
	m.resetResultSlot(&m.sessionSlot, id, true)
	m.resultSlotState = m.sessionSlot.state
	m.resetResultList()
	m.edit = nil
	m.navPrompt = nil
	m.reviewErr = ""
	m.reviewNotice = ""
	m.lastConfirm = false
}

// reloadSessionRecord reloads the session slot even while history is the
// rendered destination: background scan growth keeps the session slot ready
// without taking the page away.
func (m *Model) reloadSessionRecord(id string) {
	m.reloadResultRecord(&m.sessionSlot, id, true)
}

// reloadResultRecord loads one record into its own slot and applies the
// result only while that slot still expects this version. A response that
// belongs to another slot or a superseded load is discarded.
func (m *Model) reloadResultRecord(slot *resultSlot, id string, live bool) {
	slot.state.recLoad++
	version := slot.state.recLoad
	if m.activeSlot == slot.kind {
		m.recLoad = version
	}
	m.goAsync(func() {
		rec, err := m.cfg.Service.LoadRecord(id)
		m.update(func() {
			if version != slot.state.recLoad {
				return // a newer load owns this slot
			}
			if m.activeSlot == slot.kind && version != m.recLoad {
				return // an action invalidated the active view's load
			}
			if err != nil {
				slot.state.recErr = err.Error()
				if m.activeSlot == slot.kind {
					m.recErr = slot.state.recErr
				}
				return
			}
			m.applyLoadedRecord(slot, id, rec, live)
		})
	})
}

// applyLoadedRecord installs one slot's freshly loaded record. The chosen
// row follows its capture sequence across the reload; a vanished selection
// falls back to the same position, then to the first row.
func (m *Model) applyLoadedRecord(slot *resultSlot, id string, rec *record.Recovery, live bool) {
	previous := slot.state.selected
	sequence := slot.state.selectedSequence
	if m.activeSlot == slot.kind {
		previous = m.selected
		sequence = m.selectedSeq()
	}
	view := BuildRecordView(id, rec, live)
	selected := view.RowOf(sequence)
	if selected < 0 && previous >= 0 && previous < len(view.Rows) {
		selected = previous
	}
	if selected < 0 && len(view.Rows) > 0 {
		selected = 0
	}

	export, buildErr := resultExport(rec)
	slot.state.rec = view
	slot.state.recData = rec
	slot.state.recErr = ""
	slot.state.exportData = export
	slot.state.exportBuildErr = buildErr
	slot.state.selected = selected
	slot.state.selectedSequence = 0
	if selected >= 0 && selected < len(view.Rows) {
		slot.state.selectedSequence = view.Rows[selected].Seq
	}
	if slot.state.scroll >= len(view.Rows) {
		slot.state.scroll = 0
		if len(view.Rows) > 0 {
			slot.state.scroll = len(view.Rows) - 1
		}
	}

	if m.activeSlot != slot.kind {
		return // the slot is ready for its next visit without touching the page
	}
	m.rec = view
	m.recData = rec
	m.recErr = ""
	m.exportData = export
	m.exportBuildErr = buildErr
	m.selected = selected
	m.selectedSequence = slot.state.selectedSequence
	m.scroll = slot.state.scroll
	if m.scroll > 0 {
		m.list.ScrollTo(m.scroll, ui.Start)
	}
}

// resultExport builds the frozen usable-data snapshot of one checkpoint.
// It is the same snapshot export.go rebuilds for the active record; the
// slot-local copy keeps an inactive destination's export stable.
func resultExport(rec *record.Recovery) (*app.UsableExport, string) {
	if rec == nil {
		return nil, ""
	}
	export, err := app.BuildUsableExport(rec)
	if err != nil {
		return nil, err.Error()
	}
	return export, ""
}

// sessionRecord returns the session slot's current record, preferring the
// active projection so a cleared active view cannot be resurrected from a
// stale stored pointer.
func (m *Model) sessionRecord() *RecordView {
	if m.activeSlot == resultSlotSession {
		return m.rec
	}
	return m.sessionSlot.state.rec
}

// sessionLiveSavedShown reports the saved count the session slot last
// loaded, whether or not that slot is active.
func (m *Model) sessionLiveSavedShown() int {
	if m.activeSlot == resultSlotSession {
		return m.liveSavedShown
	}
	return m.sessionSlot.state.liveSavedShown
}

// setSessionLiveSavedShown records a saved count for the session slot,
// keeping the active projection and the stored slot in step.
func (m *Model) setSessionLiveSavedShown(count int) {
	m.sessionSlot.state.liveSavedShown = count
	if m.activeSlot == resultSlotSession {
		m.liveSavedShown = count
	}
}
