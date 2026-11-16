package ui

import (
	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/LoTwT/youkai/internal/scan"
)

// historyOpen is navigation ownership, separate from the workspace's
// recLoad content version. Cancelling it leaves the old workspace intact.
type historyOpen struct {
	token           int64
	id              string
	exportAfterLoad bool
}

// preparedRecord contains only data. Building it never touches Model,
// ListState or a UI Context; the UI installs it in one queued update.
type preparedRecord struct {
	recovery  *record.Recovery
	view      *RecordView
	export    *app.UsableExport
	exportErr string
}

func prepareRecord(id string, rec *record.Recovery, live bool) preparedRecord {
	export, exportErr := resultExport(rec)
	return preparedRecord{recovery: rec, view: BuildRecordView(id, rec, live), export: export, exportErr: exportErr}
}

func (m *Model) recordIsReady(id string) bool {
	return m.recordIsOpen(id) && m.recData != nil && !m.loading && m.recErr == ""
}

func (m *Model) cancelHistoryOpen() {
	if m.historyOpen != nil {
		m.historyOpen = nil
		m.historyFocusPending = true
	}
}

func (m *Model) beginHistoryOpen(id string, live, exportAfterLoad bool) {
	if m.historyOpen != nil {
		return // Same-target repeats coalesce; other rows are blocked.
	}
	m.historyOpenSerial++
	intent := historyOpen{token: m.historyOpenSerial, id: id, exportAfterLoad: exportAfterLoad}
	m.historyOpen = &intent
	m.historyOpenError = ""
	m.historyOpenFailed = nil
	m.closeHistoryMenu()
	m.historyMenuID = ""
	version := m.recLoad
	savedShown := m.snap.SavedCount
	// Workspace versions are positive. Negative tokens let notifications
	// follow this read without invalidating a refresh of the old workspace.
	readVersion := -intent.token
	load := newRecordLoad()
	m.trackRecordRead(id, readVersion, load)
	service, update := m.cfg.Service, m.upd.Update
	m.goAsync(func() {
		rec, err := service.LoadRecord(id)
		// done means I/O finished, including on a cancelled navigation.
		// Exit's bounded notification flush never waits for UI preparation.
		load.finish(rec, err)
		var prepared preparedRecord
		if err == nil {
			prepared = prepareRecord(id, rec, live)
		}
		update(func() {
			defer func() {
				m.settleResultFromRecord(scan.ID(id), readVersion)
				delete(m.recordReads, readVersion)
			}()
			if m.historyOpen == nil || m.historyOpen.token != intent.token {
				return
			}
			m.cancelHistoryOpen()
			if m.displayPage() != pageHistory || m.recLoad != version ||
				m.exitIntent != exitIntentNone || m.exitState == exitRunning || m.exitState == exitDone {
				return
			}
			if err != nil {
				m.historyOpenError = err.Error()
				m.historyOpenFailed = &intent
				return
			}
			m.showRecord(id, live)
			m.recordTab = recordTabSummary
			if live {
				// Growth while this read was pending still needs a refresh;
				// this generation cannot claim a later snapshot's saved count.
				m.liveSavedShown = savedShown
			}
			m.installPreparedRecord(prepared)
			m.page = pageResults
			if intent.exportAfterLoad {
				m.openExportDialog()
			}
		})
	})
}

// trackRecordRead preserves the final-generation notification contract
// for both an open workspace refresh and a list's pending navigation.
func (m *Model) trackRecordRead(id string, version int64, load *recordLoad) {
	if m.recordReads == nil {
		m.recordReads = make(map[int64]*recordLoad)
	}
	m.recordReads[version] = load
	if state := m.notifySessions[scan.ID(id)]; state != nil && state.pending != nil &&
		state.pending.loadVersion != 0 {
		state.pending.loadVersion = version
		state.pending.load = load
	}
}
