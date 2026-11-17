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

// Saved item growth and a completed wrap-up both publish checkpoints.
// A final checkpoint can change termination/revision without adding items.
// This stamp is captured and compared only on the UI thread; ordinary
// progress snapshots do not cause rereads.
type historyLiveCheckpoint struct {
	savedCount int
	final      bool
}

func historyLiveCheckpointFor(snap app.Snapshot) historyLiveCheckpoint {
	return historyLiveCheckpoint{snap.SavedCount,
		!snap.Busy && snap.Scan.Phase == scan.PhaseEnded && !snap.SavePending && snap.SaveError == ""}
}

func prepareRecord(id string, rec *record.Recovery, live bool) preparedRecord {
	export, exportErr := resultExport(rec)
	return preparedRecord{recovery: rec, view: BuildRecordView(id, rec, live), export: export, exportErr: exportErr}
}

func (m *Model) recordIsReady(id string) bool {
	return m.recordIsOpen(id) && m.recData != nil && !m.loading && m.recErr == "" && !m.liveWorkspaceNeedsRefresh()
}

func (m *Model) liveWorkspaceNeedsRefresh() bool {
	return m.liveRecordOpen() && m.liveCheckpoint != historyLiveCheckpointFor(m.snap)
}

func (m *Model) cancelHistoryOpen() {
	if m.historyOpen != nil {
		m.historyOpen = nil
		m.historyFocusPending = true
		// Cancelling transfers no navigation rights to a read. The retained
		// workspace still owns any accepted checkpoint it has not installed.
		m.catchUpLive()
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
	checkpoint := historyLiveCheckpointFor(m.snap)
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
			if m.displayPage() != pageHistory || m.recLoad != version ||
				m.exitIntent != exitIntentNone || m.exitState == exitRunning || m.exitState == exitDone {
				m.cancelHistoryOpen()
				return
			}
			if err != nil {
				m.cancelHistoryOpen()
				m.historyOpenError = err.Error()
				m.historyOpenFailed = &intent
				return
			}
			if live && historyLiveCheckpointFor(m.snap) != checkpoint {
				// The latest accepted service event may be the last one.
				// Keep waiting in the list and read after that checkpoint;
				// never install/export the older prepared generation first.
				// Only a changed checkpoint stamp schedules another async
				// read, so stable progress cannot create a retry loop.
				m.historyOpen = nil
				m.beginHistoryOpen(id, live, intent.exportAfterLoad)
				return
			}
			// This prepared installation takes over the workspace's data
			// responsibility; do not start a redundant refresh of its old view.
			m.historyOpen = nil
			m.showRecord(id, live)
			m.recordTab = recordTabSummary
			if live {
				m.liveSavedShown = checkpoint.savedCount
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
