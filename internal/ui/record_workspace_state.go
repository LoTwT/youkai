package ui

import (
	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/egoist/mygo/ui"
)

// resultFilter is the item list's chosen segment: the items a person still
// has to handle (backfill + review), the exportable items with a warning,
// or every captured item.
type resultFilter int

const (
	resultFilterPending resultFilter = iota
	resultFilterWarning
	resultFilterAll
)

// reviewRegion is one of the three large regions of the item review:
// the item list, the screenshots and the form. F6 cycles through them.
type reviewRegion int

const (
	reviewRegionList reviewRegion = iota
	reviewRegionCrops
	reviewRegionForm
	reviewRegionCount
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

	// The browser state: segment, set/slot facets, the chosen row and its
	// stable capture sequence, and the first visible row. The item list is
	// 240 DIP; the facets narrow the chosen segment without changing the
	// record-wide segment counts.
	filter           resultFilter
	setFilter        string
	slotFilter       string
	selected         int
	selectedSequence int64
	scroll           int

	// The review layout's transient state. reviewRegion is the F6 region
	// the keyboard is in; reviewField is the form field whose crop is
	// highlighted; focusReviewField asks the next frame to focus that
	// field's control; scrollToCrop asks it to bring the highlighted crop
	// into view. cropZoom 0 means fit the screenshot area, >0 multiplies
	// the fit width. formExtrasOpen is the "其余字段" disclosure, and
	// compactListOpen is the compact strip's "全部件" popover.
	reviewRegion       reviewRegion
	reviewField        FormField
	focusReviewField   FormField
	focusedReviewField FormField
	scrollToCrop       bool
	cropZoom           float32
	formExtrasOpen     bool
	formRawOpen        bool
	compactListOpen    bool
	// compactListFocusRequested asks the compact popover's list to take the
	// keyboard once, after the anchor opens it.
	compactListFocusRequested bool
	// focusListRequested asks the next list build to take the keyboard;
	// ListState.Focus must be called while its rows are being built.
	focusListRequested bool

	// recentlyConfirmed holds items confirmed with the "confirm and stay"
	// action. They remain visible in the pending segment, marked as
	// confirmed, until the page is left or the record is reloaded.
	recentlyConfirmed map[int64]bool

	// The frozen export snapshot and its build error.
	exportData     *app.UsableExport
	exportBuildErr string

	// The summary's two disclosures and the last diagnostics-open failure.
	// Both start collapsed (09 §2, §4.6); a different record resets them with
	// the rest of the workspace, while a reload of the same record keeps the
	// user's state.
	summaryDetailsOpen     bool
	summaryDiagnosticsOpen bool
	summaryDiagnosticsErr  string
}

// newRecordWorkspaceState is the empty workspace: no record, no selection
// and the pending segment the design starts on.
func newRecordWorkspaceState() recordWorkspaceState {
	return recordWorkspaceState{
		filter:       resultFilterPending,
		selected:     -1,
		reviewRegion: reviewRegionForm,
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

// recordClassCounts is the one source of the four-class counts and the
// pending total. Built rows are freshest because applySavedItem rebuilds
// them after every save; Classes is the fallback before rows exist.
func (m *Model) recordClassCounts() app.ItemClassCounts {
	if m.rec == nil {
		return app.ItemClassCounts{}
	}
	if len(m.rec.Rows) > 0 {
		for i := range m.rec.Rows {
			if m.rec.Rows[i].ItemClass != "" {
				return rowClassCounts(m.rec.Rows)
			}
		}
	}
	return m.rec.Classes
}

// rowClassCounts sums the four classes of already built rows.
func rowClassCounts(rows []Row) app.ItemClassCounts {
	var counts app.ItemClassCounts
	for i := range rows {
		switch rows[i].ItemClass {
		case app.ItemClassPendingBackfill:
			counts.PendingBackfill++
		case app.ItemClassPendingReview:
			counts.PendingReview++
		case app.ItemClassWarning:
			counts.Warning++
		case app.ItemClassPassed:
			counts.Passed++
		}
	}
	return counts
}

// pendingItemsCount is the design's "待处理" total: backfill plus review.
func (m *Model) pendingItemsCount() int {
	return int(m.recordClassCounts().Pending())
}

// selectReviewRegion moves the keyboard to one of the review's three
// regions on the next frame.
func (m *Model) selectReviewRegion(region reviewRegion) {
	if region < 0 || region >= reviewRegionCount {
		return
	}
	m.reviewRegion = region
	if region == reviewRegionForm && m.reviewField == "" {
		m.requestReviewFieldFocus(m.firstReviewField())
	}
}

// selectReviewField highlights the screenshots that back one form field
// and asks the next frame to bring the matching crop into view.
func (m *Model) selectReviewField(field FormField) {
	if field == "" {
		return
	}
	m.reviewRegion = reviewRegionForm
	if m.reviewField != field {
		m.reviewField = field
		m.scrollToCrop = true
	}
}

// focusFirstReviewField starts a newly chosen item on its first problem
// field and clears the previous item's screenshot highlight.
func (m *Model) focusFirstReviewField() {
	m.reviewField = ""
	m.scrollToCrop = false
	m.requestReviewFieldFocus("")
}

// pointFirstReviewField highlights a newly chosen item's first problem
// field without taking keyboard focus away from the list it was chosen in.
func (m *Model) pointFirstReviewField() {
	m.reviewField = m.firstReviewField()
	m.scrollToCrop = true
}

// requestReviewFieldFocus focuses one form field's control on the next
// frame. The marker is consumed once, so later Tab moves are the user's.
func (m *Model) requestReviewFieldFocus(field FormField) {
	if field == "" {
		field = m.firstReviewField()
	}
	m.focusReviewField = field
}

// firstReviewField is the field the form starts on: the first problem
// field in form order, else the first required field.
func (m *Model) firstReviewField() FormField {
	row := m.selectedRow()
	if row == nil {
		return FormFieldSetID
	}
	problems := m.reviewProblems(row)
	for _, problem := range problems {
		if field := canonicalFormField(problem.Field); field != "" && problem.Blocks() {
			return field
		}
	}
	for _, problem := range problems {
		if field := canonicalFormField(problem.Field); field != "" {
			return field
		}
	}
	return FormFieldSetID
}

// selectedRow is the open record's chosen row, or nil.
func (m *Model) selectedRow() *Row {
	if m.rec == nil || m.selected < 0 || m.selected >= len(m.rec.Rows) {
		return nil
	}
	return &m.rec.Rows[m.selected]
}

// zoomCrop steps the screenshot zoom in or out; reset returns to fitting
// the screenshot area.
func (m *Model) zoomCrop(delta float32) {
	previous := m.cropZoom
	if delta == 0 {
		m.cropZoom = 0
		if previous != 0 && m.cache != nil {
			m.cache.reset()
		}
		return
	}
	zoom := m.cropZoom
	if zoom <= 0 {
		zoom = 1
	}
	zoom += delta
	switch {
	case zoom < 1:
		zoom = 1
	case zoom > 4:
		zoom = 4
	}
	if zoom <= 1 {
		zoom = 0
	}
	if zoom == previous {
		return
	}
	m.cropZoom = zoom
	if m.cache != nil {
		m.cache.reset()
	}
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
	m.reviewField = ""
	m.focusReviewField = ""
	m.scrollToCrop = false
	m.cropZoom = 0
	m.formExtrasOpen = false
	m.compactListOpen = false
	m.compactListFocusRequested = false
	m.edit = nil
	m.navPrompt = nil
	m.reviewErr = ""
	m.reviewSaveFailed = false
	m.reviewNotice = ""
	m.lastAction = reviewSave
}

// resetResultList clears the list's scroll and selection internals before
// the record's own place is restored.
func (m *Model) resetResultList() {
	m.list = ui.ListState{Selected: &m.tableSelected}
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
				// A failed refresh cannot prove a draft is stale. Keep dirty
				// input visible (writes stay blocked by recErr) instead of
				// silently dropping what the person typed.
				if m.edit == nil || !m.edit.dirty() {
					m.edit = nil
				}
				m.navPrompt = nil
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
