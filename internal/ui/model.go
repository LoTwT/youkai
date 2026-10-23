// Package ui builds youkai's main window on MyGo's native UI. It owns no
// session, result or save state of its own: the model renders the
// application service's snapshots and record loads, and every interaction
// only schedules asynchronous work against the service — a UI callback
// never waits on a scan, recognition or a save.
//
// The model's state is confined to the UI thread: view callbacks mutate it
// during a frame, background work mutates it through Updater (the window's
// Update), and nothing else touches it. View tests without a window drive
// the same paths through a queued Updater.
package ui

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/LoTwT/youkai/internal/scan"
	"github.com/egoist/mygo/ui"
)

// Service is the application-service surface the main window drives. It is
// the application service's own exported API, so *app.Service satisfies it
// directly; the entry point also pumps the service's subscription into
// Apply (the model itself stays free of native window types).
type Service interface {
	// Start accepts a start request into the single active slot; the
	// same request ID returns the session it created.
	Start(req app.StartRequest) (app.StartOutcome, error)
	// Stop requests an idempotent user stop of the active session.
	Stop()
	// Snapshot is the authoritative state at one service revision.
	Snapshot() app.Snapshot
	// RetrySave re-attempts a failed persistence of the active session.
	RetrySave() error
	// HistoryRecords lists saved records with the metadata the history
	// area shows.
	HistoryRecords() ([]app.HistoryRecord, error)
	// LoadRecord loads one saved record with recovery semantics applied.
	LoadRecord(recordID string) (*record.Recovery, error)
	// SaveItemRevision appends one manual revision to a produced item and
	// returns the updated item.
	SaveItemRevision(recordID string, sequence, expectedRevision int64, edit app.ItemRevisionEdit) (app.ItemRevisionResult, error)
	// DeleteRecordScreenshots deletes one record's screenshots while
	// keeping its data, revisions and review state.
	DeleteRecordScreenshots(recordID string) (record.EvidenceRemoval, error)
	// DeleteAllScreenshots deletes every record's screenshots.
	DeleteAllScreenshots() (record.EvidenceBatch, error)
	// DeleteRecord permanently deletes one record and its screenshots.
	DeleteRecord(recordID string) (record.RecordDeletion, error)
	// DeleteAllRecords permanently deletes every record.
	DeleteAllRecords() (record.RecordDeletionBatch, error)
	// PreviewRecordScreenshots measures one record's screenshot deletion.
	PreviewRecordScreenshots(recordID string) (app.CleanupPreview, error)
	// PreviewAllScreenshots measures deleting every record's screenshots.
	PreviewAllScreenshots() (app.CleanupPreview, error)
	// PreviewRecordDeletion measures deleting one record.
	PreviewRecordDeletion(recordID string) (app.CleanupPreview, error)
	// PreviewAllRecords measures deleting every record.
	PreviewAllRecords() (app.CleanupPreview, error)
	// StorageUsage measures records, screenshots, cache, logs and the
	// free space of the data volume.
	StorageUsage() (app.StorageUsage, error)
	// PreviewCacheClear measures the rebuildable cache.
	PreviewCacheClear() (app.StorageClearPreview, error)
	// ClearCache removes the rebuildable cache only.
	ClearCache() (app.StorageClearPreview, error)
	// PreviewOldLogClear measures the rotated diagnostic-log backups.
	PreviewOldLogClear() (app.StorageClearPreview, error)
	// ClearOldLogs removes the rotated diagnostic-log backups only.
	ClearOldLogs() (app.StorageClearPreview, error)
	// Quit runs the explicit-exit wrap-up; it blocks until it is done.
	Quit() error
}

// Updater runs fn on the UI thread and redraws the window. *mygo.Window
// implements it; view tests queue the closures and run them from the test
// goroutine.
type Updater interface {
	Update(fn func())
}

// ScanSourceInfo says whether a scan can start and why. The entry point
// knows whether it wired a source (M1: only the development data source);
// the service refuses starts without one, and the button explains it.
type ScanSourceInfo struct {
	// Available is whether the service has a source to start from.
	Available bool
	// Label names the wired source for the prepare area (empty to hide).
	Label string
	// Reason explains why starts are impossible when Available is false.
	Reason string
}

// Config assembles the main-window model.
type Config struct {
	// Service is the application service the window drives. Required.
	Service Service
	// PageLayout selects the four-page shell. The legacy single-column
	// layout remains for the interaction-test migration.
	PageLayout bool
	// ScanSource describes the start availability shown in the prepare
	// area.
	ScanSource ScanSourceInfo
	// Evidence loads evidence images by record and evidence identity;
	// nil disables images (the details show their texts only).
	Evidence EvidenceSource
	// ExportSaver chooses and writes the usable-data JSON/CSV files through
	// the system save dialog. The legacy layout keeps this behavior.
	ExportSaver ExportSaver
	// DirectExportSaver writes the usable-data files straight into the
	// system downloads directory. The page shell prefers it; nil falls back
	// to ExportSaver.
	DirectExportSaver DirectExportSaver
	// OpenDownloadsDirectory reveals the direct export directory with the
	// system file manager; nil hides the "open directory" action.
	OpenDownloadsDirectory func() error
	// OpenDataDirectory opens the managed data root with the system file
	// manager; nil hides the action.
	OpenDataDirectory func() error
	// EvidenceCacheLimit bounds the decoded evidence bitmaps held at
	// once; 0 uses evidenceCacheLimit.
	EvidenceCacheLimit int
	// Updates checks for and installs application updates (see
	// update.go); nil hides the update area entirely.
	Updates UpdateSource
	// AppVersion is the running application's version, shown in the update
	// area as "当前版本 …" so the window itself names the build it runs;
	// the entry point passes mygo.App.Version(). Empty hides the line.
	AppVersion string
	// DevAutoInstall installs an update a check found on its own, without
	// the install button. It is a development switch (the entry point sets
	// it from YOUKAI_DEV_AUTOUPDATE) for driving the real install and
	// restart flow in the update rehearsal (docs/development.md), never a
	// user setting; InstallUpdate's guards still apply.
	DevAutoInstall bool
	// UpdateCheckWait waits for the next periodic update check of
	// StartUpdateChecks; nil waits real time. View tests inject a
	// controllable wait to drive the schedule without sleeping.
	UpdateCheckWait func(interval time.Duration) <-chan time.Time
	// OnUpdateInstalled runs on the UI thread after an update installed
	// successfully — the entry point relaunches the application in it,
	// which runs the normal quit sequence (a session whose save keeps
	// failing refuses the restart, and the window stays).
	OnUpdateInstalled func()
	// OnExitSettled runs on the UI thread after an explicit exit
	// succeeded — the entry point quits the application in it. A failed
	// wrap-up (an unsaved result) keeps the window instead.
	OnExitSettled func()
	// OnExitFailed runs on the UI thread after an explicit exit failed and
	// the window stays. The entry point restores the window here: an exit
	// requested from the tray while the window was hidden must not leave
	// the failure invisible.
	OnExitFailed func()
	// CloseBehavior is the persisted preference loaded at startup; empty
	// means CloseBehaviorHide.
	CloseBehavior app.CloseBehavior
	// SaveCloseBehavior persists a preference change from the window. Nil
	// keeps changes in memory only (tests and builds without a data root).
	SaveCloseBehavior func(app.CloseBehavior) error
}

// LifecycleStatus is the state the tray/menu bar shows: a short label and
// whether an active session can still be stopped. It is read on the UI
// thread.
type LifecycleStatus struct {
	// Text is one of 就绪, 扫描中, 收尾中 or 保存失败.
	Text string
	// Busy is whether a scan session is still active and stoppable.
	Busy bool
}

// exitState is where the explicit-exit flow stands.
type exitState int

const (
	exitIdle exitState = iota
	exitRunning
	exitFailed
	exitDone
)

// Model is the state of youkai's main window: what the prepare, progress
// and results areas show. It renders the service's snapshots and loaded
// records; its state is confined to the UI thread (see the package
// comment).
type Model struct {
	cfg Config
	upd Updater

	// wg tracks the model-spawned background work; view tests wait on it
	// to know a click's asynchronous work has settled.
	wg sync.WaitGroup

	source ScanSourceInfo

	// The last applied service snapshot and its identity, for discarding
	// stale updates by session and revision.
	snap            app.Snapshot
	appliedSession  scan.ID
	appliedRevision uint64
	prevBusy        bool

	// Results destinations. The embedded state is the active slot's
	// projection, so the view keeps reading one record; the two slots keep
	// their own record, filters, selection, scroll and export snapshot.
	// Only the active slot may hold an unsaved editor.
	resultSlotState
	activeSlot  resultSlotKind
	sessionSlot resultSlot
	historySlot resultSlot

	// Start requests: one request ID per attempt, reused until the
	// service answered, so a double click cannot start two sessions.
	startSerial int
	startReqID  string
	startErr    string

	history          []HistoryEntry
	histChoice       string
	histErr          string
	historyFilter    string
	historySelected  map[string]bool
	historyNotice    string
	historyError     string
	historyExporting string
	historyBulk      *historyBulkPrompt

	// The history table's choice, place and row menu. historyRow indexes
	// the visible (date-filtered) entries; the ListState keeps the scroll
	// and the keyboard choice. Only one row's popover menu is open at once.
	historyList     ui.ListState
	historyRow      int
	historyScroll   ui.ScrollState
	historyMenuID   string
	historyMenuOpen bool
	// The composite table snaps precise scrolls to whole 52-DIP rows.
	// historyScrollSeen is the offset the last frame observed, and the two
	// times debounce the rest snap after the user stops moving.
	historyScrollSeen    float32
	historyScrollMovedAt time.Time
	historyScrollSnapAt  time.Time
	// The 860 composite's measured frame: the panel, its header and its
	// footer give the whole-row list viewport once laid out.
	historyPanel  ui.Ref
	historyHead   ui.Ref
	historyFooter ui.Ref

	// Screenshot/history cleanup. savePrompt is the post-save three-way
	// choice; promptedSessions remembers the sessions it was settled for,
	// so "稍后处理" never reopens it. cleanup is the preview awaiting
	// confirmation, and storage is the storeroom view's last successful
	// measurement (nil after a failure — old numbers are never shown as
	// exact).
	savePrompt       *savePrompt
	promptedSessions map[string]bool
	cleanup          *cleanupPrompt
	cleanupNotice    string
	storage          *app.StorageUsage
	storageErr       string
	storageLoading   bool
	storagePrompt    *storagePrompt

	// exporting is the shared export slot: one export runs at a time. Its
	// frozen snapshot and outcome live in the active result slot;
	// exportOutcome carries the direct export's global status strip message
	// so switching destinations cannot hide it.
	exporting     bool
	exportOutcome *exportOutcome

	// page is the four-page shell's explicit choice; pageAuto follows
	// the loaded record/history/update state.
	page Page

	// contentWidth/contentHeight track the page area so the shell can
	// switch the results split orientation at narrow widths.
	contentWidth  int
	contentHeight int

	// The results table's place and its chosen row live in the active
	// result slot's state; list keeps the toolkit's per-pass list state.
	// tableSelected is the chosen display row of the filtered table, which
	// maps back to the slot's full-row selected index. textInputFocused is
	// rebuilt every frame: MyGo's text inputs leave Escape to shortcuts, so
	// the shell keeps the key with a focused field instead of taking it.
	list             ui.ListState
	tableSelected    int
	textInputFocused bool
	split            float32
	// The results composite takes a whole number of rows, so a partial row
	// cannot sit at the bottom. Its slot fills the pane whatever the
	// table's capped height, giving each frame the room to measure. The
	// last whole-row height is kept for a page that returns before its
	// slot has a committed frame.
	resultsTableSlot   ui.Ref
	resultsTableHeight float32
	// The results composite tracks its list's vertical offset. A precise
	// scroll may stop between 32-DIP rows; resultsScrollSeen and the two
	// times debounce the rest snap that aligns it to a row.
	resultsScroll        ui.ScrollState
	resultsScrollSeen    float32
	resultsScrollMovedAt time.Time
	resultsScrollSnapAt  time.Time
	// historyRowProbe measures the native history table's tallest cell, so
	// its whole-row cap uses the platform's real line metrics.
	historyRowProbe ui.Ref
	// resultsDetail opens a narrow results page from its list to the
	// chosen item's detail; it has no effect in the side-by-side split.
	resultsDetail bool
	// showEvidence reveals the detail pane's evidence area — screenshots,
	// raw OCR lines, their provenance and model scores. It is off by
	// default so the structured form stays the main view.
	showEvidence bool

	// The review editor: one item's unsaved draft, and the save currently
	// in flight. reviewSerial invalidates a late response when a newer
	// action owns the editor. navPrompt holds a navigation that waits for
	// the dirty draft to be saved or discarded.
	edit         *editState
	reviewSerial int64
	reviewSaving bool
	reviewErr    string
	reviewNotice string
	lastAction   reviewAction
	navPrompt    *navigationRequest

	exitState exitState
	exitErr   string

	// The close-window preference and the last persistence failure, plus a
	// notice the entry point sets when the tray/menu bar entry is
	// unavailable — the close-to-tray default must not hide the only way
	// back into the app.
	closeBehavior    app.CloseBehavior
	closeBehaviorErr string
	windowNotice     string

	// The auto-update corner's state (update.go); updatesEnabled is cached
	// from Updates at construction — Enabled probes the install location,
	// too costly to repeat every frame, and the answer cannot change
	// within a run. appVersion and devAutoInstall come from the config.
	updates           UpdateSource
	updatesEnabled    bool
	updatesState      updateState
	updatesVersion    string
	updatesNotes      string
	updatesHandle     UpdateHandle
	updatesCheckErr   string
	updatesInstallErr string
	updatesProgress   float64
	appVersion        string
	devAutoInstall    bool

	cache *evidenceCache
}

// New creates the main-window model. It starts no work: Attach connects
// the updater and loads the record history.
func New(cfg Config) *Model {
	if cfg.Service == nil {
		panic("ui: new model: service is nil")
	}
	limit := cfg.EvidenceCacheLimit
	if limit <= 0 {
		limit = evidenceCacheLimit
	}
	closeBehavior := cfg.CloseBehavior
	if closeBehavior != app.CloseBehaviorExit {
		closeBehavior = app.CloseBehaviorHide
	}
	m := &Model{
		cfg:              cfg,
		source:           cfg.ScanSource,
		split:            420,
		contentWidth:     1020,
		contentHeight:    700,
		cache:            newEvidenceCache(cfg.Evidence, limit),
		promptedSessions: make(map[string]bool),
		historySelected:  make(map[string]bool),
		historyRow:       -1,
		closeBehavior:    closeBehavior,
	}
	if cfg.Updates != nil {
		m.updates = cfg.Updates
		m.updatesEnabled = cfg.Updates.Enabled()
	}
	m.appVersion = cfg.AppVersion
	m.devAutoInstall = cfg.DevAutoInstall
	m.sessionSlot = resultSlot{kind: resultSlotSession, state: newResultSlotState()}
	m.historySlot = resultSlot{kind: resultSlotHistory, state: newResultSlotState()}
	m.resultSlotState = m.sessionSlot.state
	m.list.Selected = &m.tableSelected
	return m
}

// SetContentSize records the page area for responsive layout. The entry
// point calls it once at ready and again from Window.OnResize.
func (m *Model) SetContentSize(width, height int) {
	if width <= 0 || height <= 0 {
		return
	}
	if m.contentWidth == width && m.contentHeight == height {
		return
	}
	sideBySide := resultsSideBySide(m.contentWidth)
	m.contentWidth, m.contentHeight = width, height
	m.split = resultsSplitTableWidth(width)
	if sideBySide != resultsSideBySide(width) && m.rec != nil {
		// Crossing the split breakpoint changes the table pane and its
		// column widths. Re-enter on the first visible row so the new
		// layout starts from the same row and a mid-input offset cannot
		// leave half rows behind.
		if first, last := m.list.Visible(); last >= first && first >= 0 {
			m.resetResultsScroll(first)
			m.list.ScrollTo(first, ui.Start)
		}
	}
	if m.upd != nil {
		m.upd.Update(func() {})
	}
}

// Attach connects the updater every background result flows through and
// loads the record history. It runs before the first frame that reacts to
// input, from the entry point's ready callback.
func (m *Model) Attach(upd Updater) {
	if upd == nil {
		panic("ui: attach: updater is nil")
	}
	m.upd = upd
	m.loadHistory()
	m.refreshStorage()
}

// update applies fn on the UI thread: in the window through Updater, which
// also redraws. Fail fast when nothing is attached — a model that reacts
// must be attached first.
func (m *Model) update(fn func()) {
	if m.upd == nil {
		panic("ui: background work without an attached updater")
	}
	m.upd.Update(fn)
}

// goAsync runs fn as the model's own background work; tests wait for it
// through Idle. fn itself must not touch model state — it publishes
// through update.
func (m *Model) goAsync(fn func()) {
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		fn()
	}()
}

// Idle waits until the model's background work settled. View tests use it
// before draining the queued updater.
func (m *Model) Idle() { m.wg.Wait() }

// Apply renders one service snapshot, discarding stale updates: a
// snapshot of the session already shown applies only at a higher
// revision; one of another session (including the empty idle identity)
// always applies. The entry point's subscription pump calls it through
// the window's Update.
func (m *Model) Apply(snap app.Snapshot) {
	if snap.SessionID == m.appliedSession && snap.Revision <= m.appliedRevision {
		return
	}
	prevSession, prevBusy := m.appliedSession, m.prevBusy
	m.appliedSession, m.appliedRevision = snap.SessionID, snap.Revision
	m.snap = snap
	m.prevBusy = snap.Busy

	// A new session becomes the session slot's record: the live one, an
	// empty view until its saved generations load (a refused start rolls
	// the service back to the previous session, whose saved record reloads
	// through the growth branch below). Unsaved review input is never
	// thrown away by a scan starting behind the user's back.
	if snap.SessionID != "" && snap.SessionID != prevSession {
		m.beginSession(string(snap.SessionID))
	}
	// The live record's saved count grew: a newer generation is on disk,
	// so reload the session slot (the previous rows stay until the load
	// lands). A dirty active session keeps its snapshot; catchUpLive
	// reloads once it settles. History stays on screen meanwhile.
	reloadedLive := false
	if rec := m.sessionRecord(); rec != nil && rec.Live && snap.SessionID != "" &&
		rec.ID == string(snap.SessionID) && snap.SavedCount > m.sessionLiveSavedShown() {
		dirtyActive := m.activeSlot == resultSlotSession && (m.editingDirty() || m.reviewSaving)
		if !dirtyActive {
			m.setSessionLiveSavedShown(snap.SavedCount)
			m.reloadSessionRecord(rec.ID)
			reloadedLive = true
		}
	}
	// A session that just wrapped left a durable final record: refresh the
	// history list and, when the saved count did not grow (for example a
	// session with no captured items), reload the live record so its
	// termination and saved revision are the final ones. The idle snapshot
	// at startup loads history once via Attach.
	if prevBusy && !snap.Busy && snap.SessionID != "" {
		if snap.SaveError == "" && snap.Scan.Phase == scan.PhaseEnded {
			m.requestSavePrompt(string(snap.SessionID))
			if m.cfg.PageLayout && m.activeSlot == resultSlotSession &&
				!m.editingDirty() && !m.reviewSaving {
				m.page = pageResults
			}
		}
		m.loadHistory()
		m.refreshStorage()
		if !reloadedLive {
			if rec := m.sessionRecord(); rec != nil && rec.Live && rec.ID == string(snap.SessionID) {
				dirtyActive := m.activeSlot == resultSlotSession && (m.editingDirty() || m.reviewSaving)
				if !dirtyActive {
					m.setSessionLiveSavedShown(snap.SavedCount)
					m.reloadSessionRecord(rec.ID)
				}
			}
		}
	}
}

// beginSession routes a newly started scan into the session slot. The live
// destination becomes active unless unsaved input on the current record
// keeps the user where they are; a background scan must not discard input.
func (m *Model) beginSession(id string) {
	busyEditing := m.reviewSaving || m.editingDirty()
	if m.savePrompt != nil && m.savePrompt.recordID != id {
		// A new scan replaces the results view; the old session's prompt is
		// settled as postponed instead of floating over unrelated content.
		m.promptedSessions[m.savePrompt.recordID] = true
		m.savePrompt = nil
	}
	if busyEditing && m.rec != nil && m.rec.ID != id {
		if m.activeSlot == resultSlotSession {
			m.rec.Live = false // the previous session's record is history now
			m.reviewNotice = "新的扫描已开始；保存或放弃当前编辑后可查看本次扫描。"
			return
		}
		m.reviewNotice = "新的扫描已开始；保存或放弃当前编辑后可查看本次扫描。"
		m.resetBackgroundSession(id)
		return
	}
	m.showLiveSession(id)
}

// resetBackgroundSession points the inactive or about-to-be-shown session
// slot at a new session without touching the active history destination.
func (m *Model) resetBackgroundSession(id string) {
	version := m.sessionSlot.state.recLoad + 1
	m.sessionSlot = resultSlot{kind: resultSlotSession, recordID: id, state: newResultSlotState()}
	m.sessionSlot.state.recLoad = version
	m.sessionSlot.state.rec = &RecordView{ID: id, Live: true}
	if m.activeSlot == resultSlotSession {
		m.resultSlotState = m.sessionSlot.state
		m.resetResultList()
	}
}

// ShowLive switches the results area back to the current (or last) scan's
// record, after a reopened history record replaced it. The session slot
// keeps its loaded view, so returning does not reload or reset it.
func (m *Model) ShowLive() {
	if m.appliedSession == "" || m.exitState == exitRunning || m.exitState == exitDone {
		return
	}
	if m.activeSlot == resultSlotSession && m.sessionSlot.recordID == string(m.appliedSession) &&
		m.rec != nil && m.rec.Live && m.rec.ID == string(m.appliedSession) {
		// The session slot is already the live destination; returning from
		// settings or the history list still starts a new visit. Revalidate
		// the same slot so a confirmed-and-stayed row leaves the pending
		// segment and the detail follows the rows the filter shows.
		m.page = pageResults
		m.activateResultSlot(resultSlotSession)
		return
	}
	m.page = pageResults
	m.requestNavigation(navigationRequest{kind: navShowLive})
}

// performShowLive is the navigation action behind ShowLive.
func (m *Model) performShowLive() {
	id := string(m.appliedSession)
	if session := &m.sessionSlot; session.recordID == id && session.state.rec != nil &&
		session.state.recErr == "" {
		m.activateResultSlot(resultSlotSession)
		return
	}
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
	m.lastAction = reviewSave
}

// HistoryEntry is one saved record of the history list, as the reopen flow
// shows it.
type HistoryEntry struct {
	// ID is the record identifier to reopen.
	ID string
	// Label is the short picker label (date and record id).
	Label string

	SavedAt       time.Time
	Client        record.Client
	CapturedCount int64
	Coverage      record.CoverageStatus
	// CoverageExpected is 0 when the record does not know the scope's
	// expected count; the table then shows the status word instead of a
	// percentage.
	CoverageExpected int64
	PendingReview    int
	ExportableCount  int64
	FailedCount      int64
	UnprocessedCount int64
	OtherCount       int64
	TotalBytes       int64
	Unreadable       bool
}

// loadHistory refreshes the history list from the service.
func (m *Model) loadHistory() {
	m.goAsync(func() {
		records, err := m.cfg.Service.HistoryRecords()
		m.update(func() {
			if err != nil {
				m.histErr = err.Error()
				return
			}
			m.histErr = ""
			entries := make([]HistoryEntry, 0, len(records))
			for _, info := range records {
				label := info.ID
				if !info.SavedAt.IsZero() {
					label = info.SavedAt.Format("2006-01-02 15:04") + " · " + info.ID
				}
				entries = append(entries, HistoryEntry{
					ID:               info.ID,
					Label:            label,
					SavedAt:          info.SavedAt,
					Client:           info.Client,
					CapturedCount:    info.CapturedCount,
					Coverage:         info.Coverage,
					CoverageExpected: info.CoverageExpected,
					PendingReview:    info.PendingReview,
					ExportableCount:  info.ExportableCount,
					FailedCount:      info.FailedCount,
					UnprocessedCount: info.UnprocessedCount,
					OtherCount:       info.OtherCount,
					TotalBytes:       info.TotalBytes,
					Unreadable:       info.Unreadable,
				})
			}
			m.history = entries
			for id := range m.historySelected {
				if !m.hasHistoryID(id) {
					delete(m.historySelected, id)
				}
			}
			labels := m.HistoryLabels()
			if m.histChoice == "" && len(labels) > 0 {
				m.histChoice = labels[0]
			}
			if m.histChoice != "" && !m.hasHistoryLabel(m.histChoice) {
				m.histChoice = ""
			}
		})
	})
}

func (m *Model) hasHistoryID(id string) bool {
	for _, e := range m.history {
		if e.ID == id {
			return true
		}
	}
	return false
}

func (m *Model) hasHistoryLabel(label string) bool {
	for _, e := range m.history {
		if e.Label == label && m.historyVisible(e) {
			return true
		}
	}
	return false
}

// historyFilterTime returns the parsed date filter. Empty means no
// filter; ok is false when the text is not a valid YYYY-MM-DD date.
func (m *Model) historyFilterTime() (time.Time, bool) {
	text := strings.TrimSpace(m.historyFilter)
	if text == "" {
		return time.Time{}, true
	}
	date, err := time.ParseInLocation("2006-01-02", text, time.Local)
	if err != nil {
		return time.Time{}, false
	}
	return date, true
}

func (m *Model) historyVisible(entry HistoryEntry) bool {
	filter, ok := m.historyFilterTime()
	if !ok {
		return false
	}
	if filter.IsZero() {
		return true
	}
	saved := entry.SavedAt.In(time.Local)
	return saved.Year() == filter.Year() && saved.Month() == filter.Month() && saved.Day() == filter.Day()
}

func (m *Model) visibleHistory() []HistoryEntry {
	out := make([]HistoryEntry, 0, len(m.history))
	for _, entry := range m.history {
		if m.historyVisible(entry) {
			out = append(out, entry)
		}
	}
	return out
}

// HistoryLabels returns the visible history labels for the picker.
func (m *Model) HistoryLabels() []string {
	entries := m.visibleHistory()
	labels := make([]string, len(entries))
	for i, e := range entries {
		labels[i] = e.Label
	}
	return labels
}

func (m *Model) historyFilterInvalid() bool {
	_, ok := m.historyFilterTime()
	return !ok
}

func (m *Model) historyLabel(id string) string {
	for _, e := range m.history {
		if e.ID == id {
			return e.Label
		}
	}
	return ""
}

func (m *Model) setHistorySelected(id string, selected bool) {
	if selected {
		m.historySelected[id] = true
		return
	}
	delete(m.historySelected, id)
}

func (m *Model) selectedHistoryIDs() []string {
	ids := make([]string, 0, len(m.historySelected))
	for _, entry := range m.visibleHistory() {
		if m.historySelected[entry.ID] {
			ids = append(ids, entry.ID)
		}
	}
	return ids
}

func (m *Model) historyClientText(client record.Client) string {
	switch client {
	case record.ClientWindowsLocal:
		return "Windows 完整客户端"
	case record.ClientWindowsCloud:
		return "Windows 云客户端"
	case record.ClientMacOSCloud:
		return "macOS 云客户端"
	default:
		return "客户端未知"
	}
}

func (m *Model) historyRowText(entry HistoryEntry) string {
	date := "日期未知"
	if !entry.SavedAt.IsZero() {
		date = entry.SavedAt.In(time.Local).Format("2006-01-02 15:04")
	}
	review := "已校对"
	count := fmt.Sprintf("%d 件", entry.CapturedCount)
	coverage := coverageText(entry.Coverage)
	switch {
	case entry.Unreadable:
		review = "记录不可读"
		count = "件数未知"
		coverage = "无法确认"
	case entry.PendingReview > 0:
		review = fmt.Sprintf("待校对 %d", entry.PendingReview)
	}
	return fmt.Sprintf("%s · %s · %s · 覆盖：%s · %s · %s · %s",
		date, m.historyClientText(entry.Client), count,
		coverage, review, formatBytes(entry.TotalBytes), entry.ID)
}

// OpenHistory opens the chosen history record in the history slot. The
// live session slot stays untouched; the "本次" record routes back to it
// through ShowLive so one record never has two editable copies.
func (m *Model) OpenHistory() {
	id := m.historyID(m.histChoice)
	if id == "" || m.exitState == exitRunning || m.exitState == exitDone {
		return
	}
	if id == string(m.appliedSession) && m.appliedSession != "" {
		m.ShowLive()
		return
	}
	if m.activeSlot == resultSlotHistory && m.historySlot.recordID == id {
		m.page = pageResults
		if m.editingDirty() || m.reviewSaving {
			// An explicit reopen while the editor is unsettled still walks
			// the save/discard/stay prompt; the settled action reloads.
			m.requestNavigation(navigationRequest{kind: navOpenHistory, recordID: id, reload: true})
			return
		}
		// Entering from the list is a new visit: the previous visit's
		// confirmed-and-stayed rows leave the pending segment, so the
		// selection is checked against the rows the filters still show.
		// Everything else — filter, search, set/slot choices, a still
		// visible selection and scroll — stays where the user left it.
		m.clearRecentlyConfirmedFor(resultSlotHistory)
		m.settleResultSelection()
		return // the destination is already active: no reset, no reload
	}
	m.page = pageResults
	m.requestNavigation(navigationRequest{kind: navOpenHistory, recordID: id})
}

// performOpenHistory is the navigation action behind OpenHistory. A revisit
// with no forced reload restores the stored view; otherwise a different
// record (or an explicit reload) replaces the history slot and invalidates
// its in-flight load.
func (m *Model) performOpenHistory(id string, reload bool) {
	if id == "" {
		return
	}
	if m.appliedSession != "" && id == string(m.appliedSession) {
		m.performShowLive()
		return
	}
	history := &m.historySlot
	if !reload && history.recordID == id && history.state.rec != nil && history.state.recErr == "" {
		m.activateResultSlot(resultSlotHistory)
		return
	}
	if m.activeSlot != resultSlotHistory {
		m.activateResultSlot(resultSlotHistory)
	} else {
		// The old history content still needs to be stored before the slot
		// is repointed at another record.
		m.captureActiveResult()
	}
	m.resetResultSlot(history, id, false)
	m.resultSlotState = history.state
	m.resetResultList()
	m.settleResultSelection()
	m.resultsDetail = false
	m.edit = nil
	m.navPrompt = nil
	m.reviewErr = ""
	m.reviewNotice = ""
	m.lastAction = reviewSave
	m.reloadResultRecord(history, id, false)
}

func (m *Model) historyID(label string) string {
	for _, e := range m.visibleHistory() {
		if e.Label == label {
			return e.ID
		}
	}
	return ""
}

// reloadRecord loads one record into the active result slot. The load
// result lands through reloadResultRecord's slot + version guard.
func (m *Model) reloadRecord(id string, live bool) {
	m.reloadResultRecord(m.activeResultSlot(), id, live)
}

// selectedSeq returns the selected row's capture sequence, 0 for none.
func (m *Model) selectedSeq() int64 {
	if m.rec == nil || m.selected < 0 || m.selected >= len(m.rec.Rows) {
		return 0
	}
	return m.rec.Rows[m.selected].Seq
}

// Start begins a scan: a fresh request identity per attempt, reused until
// the service answered, so repeated clicks cannot start two sessions. The
// click only schedules the call; the outcome arrives through update and
// the service's snapshots.
func (m *Model) Start() {
	if !m.canStart() {
		return
	}
	m.page = pageScan
	if m.startReqID == "" {
		m.startSerial++
		m.startReqID = "ui-" + strconv.Itoa(m.startSerial)
	}
	m.startErr = ""
	reqID := m.startReqID
	m.goAsync(func() {
		_, err := m.cfg.Service.Start(app.StartRequest{ID: reqID})
		m.update(func() {
			m.startReqID = ""
			if err != nil {
				m.startErr = err.Error()
			}
		})
	})
}

// canStart says whether the start button is usable: a source must be
// wired, no start may be in flight, no session may hold the active slot,
// the process may not be exiting, and no update may be installing or done
// and about to restart — a scan just begun would be cut short by it.
func (m *Model) canStart() bool {
	return m.source.Available && m.startReqID == "" && !m.snap.Busy &&
		m.exitState == exitIdle && !m.updateRestartImminent()
}

// updateRestartImminent reports whether an update is downloading or has
// installed and the restart is due: the model stops offering scans for
// the rest of the run.
func (m *Model) updateRestartImminent() bool {
	return m.updatesState == updateInstalling || m.updatesState == updateInstalled
}

// Stop requests the idempotent user stop of the active session. A start
// request already in flight has no Busy snapshot yet, but the service has
// already placed its placeholder session; stop it too so the request
// cannot begin collecting behind a disabled button.
func (m *Model) Stop() {
	if !m.snap.Busy && m.startReqID == "" {
		return
	}
	m.goAsync(func() { m.cfg.Service.Stop() })
}

// RetrySave re-attempts the failed persistence the progress area shows.
func (m *Model) RetrySave() {
	if m.snap.SaveError == "" {
		return
	}
	m.goAsync(func() { _ = m.cfg.Service.RetrySave() })
}

// CloseBehavior returns the current close-window preference. The entry
// point reads it on the UI thread to decide between hiding the window and
// running the explicit-exit wrap-up.
func (m *Model) CloseBehavior() app.CloseBehavior {
	if m.closeBehavior == app.CloseBehaviorExit {
		return app.CloseBehaviorExit
	}
	return app.CloseBehaviorHide
}

// SetCloseBehavior applies and persists the close-window preference. The
// change takes effect immediately even when persistence fails; the failure
// is shown next to the setting so the user knows it will not survive a
// restart. The save is a small file write, deliberately synchronous: it
// finishes before the user can quit, so no settings write can outlive the
// data-root occupancy the exit wrap-up releases.
func (m *Model) SetCloseBehavior(behavior app.CloseBehavior) {
	if behavior != app.CloseBehaviorExit {
		behavior = app.CloseBehaviorHide
	}
	if behavior == m.closeBehavior {
		return
	}
	m.closeBehavior = behavior
	m.closeBehaviorErr = ""
	if m.cfg.SaveCloseBehavior == nil {
		return
	}
	if err := m.cfg.SaveCloseBehavior(behavior); err != nil {
		m.closeBehaviorErr = "保存设置失败：" + err.Error()
	}
}

// Exiting reports whether an explicit exit is running or already settled.
// The window's close handler must not turn that exit into a hide.
func (m *Model) Exiting() bool {
	return m.exitState == exitRunning || m.exitState == exitDone
}

// PrepareRestore is the restore entry every route uses (tray, Dock, second
// instance, deep links): an active session is stopped before the window
// becomes interactive again. It never starts a scan. The stop is the same
// idempotent request as the window's stop button; saving continues after
// it.
//
// The request is issued synchronously, unlike the stop button's background
// call: the entry point shows the window right after this returns, and
// Service.Stop only takes the service lock and closes input permission — it
// does not wait for collection, recognition or a save. An accepted start
// still in preparation has no Busy snapshot yet, so the in-flight request
// is stopped too (the service records it and applies it at commit).
func (m *Model) PrepareRestore() {
	if !m.snap.Busy && m.startReqID == "" {
		return
	}
	m.cfg.Service.Stop()
}

// LifecycleStatus renders the tray/menu bar state from the authoritative
// snapshot and the exit flow.
func (m *Model) LifecycleStatus() LifecycleStatus {
	switch {
	case m.exitState == exitRunning:
		return LifecycleStatus{Text: "收尾中", Busy: false}
	case m.exitState == exitFailed || m.snap.SaveError != "":
		return LifecycleStatus{Text: "保存失败", Busy: m.snap.Busy && !m.Exiting()}
	case m.snap.Busy || m.startReqID != "":
		return LifecycleStatus{Text: "扫描中", Busy: true}
	default:
		return LifecycleStatus{Text: "就绪", Busy: false}
	}
}

// SetWindowNotice sets a persistent notice about the window lifecycle, for
// example that the tray entry is unavailable and closing will therefore
// quit instead of hiding. Empty clears it.
func (m *Model) SetWindowNotice(notice string) {
	m.windowNotice = notice
}

// AllowClose reports whether the window may close now. The first ask
// starts the explicit-exit wrap-up asynchronously and refuses the close;
// the wrap-up shows its progress in the window. A failed wrap-up (an
// unsaved result) keeps the window with the save failure and its retry;
// once the save settled, the next ask runs the wrap-up again. A completed
// wrap-up allows every later ask.
//
// This is the explicit-exit path, used by the window close when the user
// chose close-to-exit, and by every quit event regardless of the close
// preference. The close-to-tray default must not call it.
func (m *Model) AllowClose() bool {
	switch m.exitState {
	case exitDone:
		return true
	case exitRunning:
		return false
	}
	if m.reviewSaving {
		m.navPrompt = &navigationRequest{kind: navExit}
		m.reviewErr = "正在保存，完成前不能退出。"
		return false
	}
	if m.editingDirty() {
		// The close is refused until the editor's input is saved or
		// discarded; the prompt offers both and a way to stay.
		m.navPrompt = &navigationRequest{kind: navExit}
		return false
	}
	m.beginExit()
	return false
}

// beginExit starts the explicit-exit wrap-up. It runs once the editor is
// settled (or absent).
func (m *Model) beginExit() {
	if m.exitState == exitRunning || m.exitState == exitDone {
		return
	}
	m.edit = nil
	m.navPrompt = nil
	m.exitState = exitRunning
	m.exitErr = ""
	m.goAsync(func() {
		err := m.cfg.Service.Quit()
		m.update(func() {
			if err != nil {
				m.exitState = exitFailed
				m.exitErr = err.Error()
				if m.cfg.OnExitFailed != nil {
					m.cfg.OnExitFailed()
				}
				return
			}
			m.exitState = exitDone
			if m.cfg.OnExitSettled != nil {
				m.cfg.OnExitSettled()
			}
		})
	})
}
