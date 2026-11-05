package ui

import (
	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/egoist/mygo/ui"
)

// resultFilter is the results table's chosen segment.
type resultFilter int

const (
	resultFilterPending resultFilter = iota
	resultFilterAll
	resultFilterUnexportable
)

// recordWorkspaceState is everything the record workspace remembers while
// one record is open: the loaded checkpoint, the browser state and the
// frozen export snapshot. The record of the just-finished scan and the same
// record reopened from the records list share this one state, so the item
// selection, filters and the unsaved editor never have two copies. Opening
// another record replaces the state.
type recordWorkspaceState struct {
	// recLoad versions the open record's content: every load applies only
	// while its version is current, so a late response cannot replace a
	// record opened afterwards.
	recLoad int64
	// loading is true from the moment a load starts until that load's
	// current version lands. The old record stays visible meanwhile, but
	// it is a stale snapshot: completion and export must wait.
	loading bool
	// rec is the open record's view, recData the checkpoint it was built
	// from. Exports freeze their revision from recData.
	rec     *RecordView
	recData *record.Recovery
	recErr  string
	// liveSavedShown is the live record's saved count the workspace last
	// loaded from disk.
	liveSavedShown int

	// The browser state: segment, search text, set and slot filters, the
	// chosen row and its stable capture sequence, and the first visible
	// row.
	filter           resultFilter
	search           string
	setFilter        string
	slotFilter       string
	selected         int
	selectedSequence int64
	scroll           int

	// recentlyConfirmed holds items confirmed with the "confirm and stay"
	// action. They remain visible in the pending segment, marked as
	// confirmed, until the page is left or the record is reloaded.
	recentlyConfirmed map[int64]bool

	// The frozen export snapshot and its build error.
	exportData     *app.UsableExport
	exportBuildErr string
}

// newRecordWorkspaceState is the empty workspace: no record, no selection
// and the pending-review segment the design starts on.
func newRecordWorkspaceState() recordWorkspaceState {
	return recordWorkspaceState{
		filter:   resultFilterPending,
		selected: -1,
	}
}

// recordIsOpen reports whether id is the record the workspace shows. A
// record whose load is pending or failed still counts: its placeholder
// view carries the id.
func (m *Model) recordIsOpen(id string) bool {
	return id != "" && m.rec != nil && m.rec.ID == id
}

// recordIsLive reports whether id names the session the service currently
// reports (or last reported).
func (m *Model) recordIsLive(id string) bool {
	return id != "" && m.appliedSession != "" && id == string(m.appliedSession)
}

// liveRecordOpen reports whether the workspace shows the applied session's
// record.
func (m *Model) liveRecordOpen() bool {
	return m.rec != nil && m.rec.Live && m.rec.ID == string(m.appliedSession)
}

// markRecentlyConfirmed keeps one confirmed item visible in the pending
// segment for the rest of this visit.
func (m *Model) markRecentlyConfirmed(seq int64) {
	if m.recentlyConfirmed == nil {
		m.recentlyConfirmed = make(map[int64]bool)
	}
	m.recentlyConfirmed[seq] = true
}

// forgetRecentlyConfirmed drops one item's temporary pending marker.
func (m *Model) forgetRecentlyConfirmed(seq int64) {
	delete(m.recentlyConfirmed, seq)
}

// clearRecentlyConfirmed drops every temporary marker on leaving the
// results page or reloading its record.
func (m *Model) clearRecentlyConfirmed() {
	m.recentlyConfirmed = nil
}

// recentlyConfirmedNow reports whether row is the current visit's
// just-confirmed item, visible in the pending segment only.
func (m *Model) recentlyConfirmedNow(row *Row) bool {
	return m.filter == resultFilterPending && m.recentlyConfirmed != nil &&
		m.recentlyConfirmed[row.Seq]
}

// captureResultPlace records the list's actual scroll position before the
// workspace is left, so returning to the same record restores its place.
func (m *Model) captureResultPlace() {
	if first, last := m.list.Visible(); last >= first {
		m.scroll = first
	}
	m.selectedSequence = m.selectedSeq()
}

// showRecord replaces the workspace with an empty view of id. The previous
// record's content, filters, export snapshot and editor are dropped; the
// caller settles any draft before opening another record. The bumped load
// version discards responses still in flight for the previous record.
func (m *Model) showRecord(id string, live bool) {
	if m.exportAfterLoadID != id {
		m.exportAfterLoadID = ""
	}
	version := m.recLoad + 1
	m.recordWorkspaceState = newRecordWorkspaceState()
	m.recLoad = version
	m.rec = &RecordView{ID: id, Live: live}
	m.resetResultList()
	m.resultsDetail = false
	m.edit = nil
	m.navPrompt = nil
	m.reviewErr = ""
	m.reviewNotice = ""
	m.lastAction = reviewSave
}

// resetResultList clears the table's scroll and selection internals before
// the record's own place is restored.
func (m *Model) resetResultList() {
	m.list = ui.ListState{Selected: &m.tableSelected}
	m.resetResultsScroll(0)
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
		m.resetResultsScroll(m.scroll)
		m.list.ScrollTo(m.scroll, ui.Start)
	}
}

// reloadRecord loads the open record and applies the result only while it
// is still current. A response that belongs to another record or to a
// superseded load is discarded.
func (m *Model) reloadRecord(id string, live bool) {
	m.recLoad++
	version := m.recLoad
	m.loading = true
	m.goAsync(func() {
		rec, err := m.cfg.Service.LoadRecord(id)
		m.update(func() {
			if version != m.recLoad || !m.recordIsOpen(id) {
				return // a newer record or load owns the workspace
			}
			if err != nil {
				m.loading = false
				m.recErr = err.Error()
				if m.exportAfterLoadID == id {
					m.exportAfterLoadID = ""
				}
				return
			}
			m.applyLoadedRecord(id, rec, live)
		})
	})
}

// applyLoadedRecord installs the open record's freshly loaded checkpoint.
// The chosen row follows its capture sequence across a reload; a vanished
// selection falls back to the same position, then to the first row.
func (m *Model) applyLoadedRecord(id string, rec *record.Recovery, live bool) {
	previous := m.selected
	sequence := m.selectedSeq()
	view := BuildRecordView(id, rec, live)
	selected := view.RowOf(sequence)
	if selected < 0 && previous >= 0 && previous < len(view.Rows) {
		selected = previous
	}
	if selected < 0 && len(view.Rows) > 0 {
		selected = 0
	}

	export, buildErr := resultExport(rec)
	m.rec = view
	m.recData = rec
	m.recErr = ""
	m.loading = false
	m.exportData = export
	m.exportBuildErr = buildErr
	// A reload is a refresh: confirmed rows no longer linger in the
	// pending segment.
	m.recentlyConfirmed = nil
	m.selected = selected
	m.selectedSequence = 0
	if selected >= 0 && selected < len(view.Rows) {
		m.selectedSequence = view.Rows[selected].Seq
	}
	if m.scroll >= len(view.Rows) {
		m.scroll = 0
		if len(view.Rows) > 0 {
			m.scroll = len(view.Rows) - 1
		}
	}
	m.settleResultSelection()
	m.clampResultScroll()
	m.selectedSequence = m.selectedSeq()
	if m.scroll > 0 {
		m.list.ScrollTo(m.scroll, ui.Start)
	}
	if m.exportAfterLoadID == id {
		m.exportAfterLoadID = ""
		if m.displayPage() == pageResults {
			m.openExportDialog()
		}
	}
}

// resultExport builds the frozen usable-data snapshot of one checkpoint.
// Export renders exactly this snapshot, so a later save or reload cannot
// mix revisions inside the written file.
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
