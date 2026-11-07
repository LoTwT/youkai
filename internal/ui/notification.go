package ui

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/LoTwT/youkai/internal/scan"
)

// NotificationSegment tells the two scan notifications apart. The input
// stop announces the wrap-up; the result carries the outcome or the first
// save failure.
type NotificationSegment int

const (
	// NotificationInputStopped is the first segment: input closed and the
	// session entered its wrap-up.
	NotificationInputStopped NotificationSegment = iota
	// NotificationResult is the second segment: the wrap-up settled, or its
	// final save failed for the first time.
	NotificationResult
)

const (
	notificationIDPrefix         = "youkai.scan."
	notificationIDInputStopped   = "input-stopped"
	notificationIDResult         = "result"
	notificationCompleteNoIssues = "扫描完成：扫全了"
)

// Notification is one native notification the entry point sends. The text
// is assembled here from the application service's counts and the shared
// fault table; the entry point only owns the native notification, its
// click, and the taskbar flash.
type Notification struct {
	// ID is derived from the record (session) and the segment, so a click
	// can find the record even after this run's Notification object is
	// gone.
	ID string
	// Segment selects the two-stage behavior: the result replaces the
	// input-stop notification.
	Segment NotificationSegment
	// RecordID is the session's record.
	RecordID string
	Title    string
	Body     string
	// Flash asks the entry point to flash the window's taskbar button.
	Flash bool
}

// NotificationID derives a notification ID from the record and segment.
func NotificationID(recordID string, segment NotificationSegment) string {
	suffix := notificationIDResult
	if segment == NotificationInputStopped {
		suffix = notificationIDInputStopped
	}
	return notificationIDPrefix + recordID + "." + suffix
}

// ParseNotificationID recovers the record and segment from one of this
// application's notification IDs. Other applications' IDs are rejected.
func ParseNotificationID(id string) (string, NotificationSegment, bool) {
	if !strings.HasPrefix(id, notificationIDPrefix) {
		return "", 0, false
	}
	rest := strings.TrimPrefix(id, notificationIDPrefix)
	switch {
	case strings.HasSuffix(rest, "."+notificationIDInputStopped):
		recordID := strings.TrimSuffix(rest, "."+notificationIDInputStopped)
		return recordID, NotificationInputStopped, recordID != ""
	case strings.HasSuffix(rest, "."+notificationIDResult):
		recordID := strings.TrimSuffix(rest, "."+notificationIDResult)
		return recordID, NotificationResult, recordID != ""
	default:
		return "", 0, false
	}
}

// OpenNotification brings the window to the record a notification names.
// A session whose wrap-up is still incomplete opens the scan page, where
// the wrap-up and save failure states live; a finished one opens its record
// summary. It never starts a scan.
func (m *Model) OpenNotification(recordID string) {
	if recordID == "" {
		return
	}
	// The click is a navigation of its own: dialogs left from an earlier
	// action must not keep ownership of the window (a pending first-hide
	// hint would otherwise hide the just-restored window on acknowledgement).
	m.trayHintOpen = false
	m.trayHintHideOnDone = false
	if m.exporting || (m.exportDialog.open && m.exportDialog.attempted) {
		// The export dialog shows the frozen snapshot of the record being
		// written. Switching records underneath it would pair the dialog with
		// another record's counts or outcome. Keep only the latest click and
		// replay it after the user closes the dialog; a settled success,
		// canceled or failure dialog is still the user's to dismiss
		// (PR-19b, decisions 3 and 4).
		m.pendingNotificationRecordID = recordID
		return
	}
	m.pendingNotificationRecordID = ""
	m.closeExportDialog()
	if m.exitState == exitRunning || m.exitState == exitDone {
		return
	}
	if m.appliedSession == scan.ID(recordID) && m.snap.Busy {
		// A dirty editor keeps its own prompt; the notification must not
		// discard the draft just to reveal the wrap-up.
		m.navigatePage(pageScan)
		return
	}
	if m.recordIsOpen(recordID) && m.recErr == "" {
		// A notification click is a navigation even when the record is
		// already open: requestNavigation owns the dirty/save prompt and the
		// record-tab switch, so it cannot discard an editor that the click
		// should have asked about.
		m.page = pageResults
		m.requestNavigation(navigationRequest{kind: navRecordTab, recordTab: recordTabSummary})
		return
	}
	m.openRecord(recordID)
}

// notificationFacts is the record data the notification wording reads. The
// final record supplies the four-class counts and position ledger; the
// service snapshot supplies the ending and, when the record could not be
// loaded, the captured and saved counts.
type notificationFacts struct {
	captured      int
	saved         int
	counts        app.ItemClassCounts
	countsKnown   bool
	coverage      record.CoverageStatus
	outcomes      []app.PositionOutcome
	outcomesKnown bool
	inputOpened   bool
	end           scan.EndKind
	trigger       *scan.Trigger
	failureParams map[string]string
	saveFailed    bool
}

// sessionNotifications is one session's announced segments. It is keyed by
// session and never replaced on a session switch: Service.abort can roll a
// refused start back to the previous session, whose result must not repeat.
type sessionNotifications struct {
	inputStopped bool
	resultQueued bool
	seenFailures int
	pending      *pendingResult
}

// pendingResult is a result notification waiting for the wrap-up's final
// record. Its wording must read the same counts and coverage as the summary,
// which only exist after that load; a failed load still sends the
// conservative snapshot wording.
type pendingResult struct {
	snap app.Snapshot
	// loadVersion is the workspace load already reading the final record.
	// Zero means no such load is running and the notification starts its
	// own read.
	loadVersion int64
	// load is the read this segment waits for. Its result is also read by
	// the exit flush when the queued settle update would be dropped.
	load *recordLoad
}

// recordLoad is one background record read. The goroutine stores its result
// under the mutex and closes done; the UI thread reads the result only after
// done is closed, so both the normal settle update and the exit flush can
// use it without touching the model from another goroutine.
type recordLoad struct {
	mu        sync.Mutex
	rec       *record.Recovery
	err       error
	done      chan struct{}
	closeOnce sync.Once
}

func newRecordLoad() *recordLoad {
	return &recordLoad{done: make(chan struct{})}
}

func (l *recordLoad) finish(rec *record.Recovery, err error) {
	l.mu.Lock()
	l.rec, l.err = rec, err
	l.mu.Unlock()
	l.closeOnce.Do(func() { close(l.done) })
}

// finished reports whether the read returned, including with an error.
func (l *recordLoad) finished() bool {
	select {
	case <-l.done:
		return true
	default:
		return false
	}
}

// result returns the stored read outcome. Before finish it returns the zero
// outcome, which the wording treats as the conservative fallback.
func (l *recordLoad) result() (*record.Recovery, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.rec, l.err
}

// sessionNotificationState returns one session's announcement record,
// creating it on first sight.
func (m *Model) sessionNotificationState(id scan.ID) *sessionNotifications {
	if m.notifySessions == nil {
		m.notifySessions = make(map[scan.ID]*sessionNotifications)
	}
	state := m.notifySessions[id]
	if state == nil {
		state = &sessionNotifications{}
		m.notifySessions[id] = state
	}
	return state
}

// notifySnapshot derives the two notification edges from accepted service
// snapshots. The first segment only follows an observed input permission
// closing; a session that never opened input (prepare timeout, precheck
// failure) produces the second segment alone. A wrap-up's first save
// failure replaces the result notification and is sent once.
func (m *Model) notifySnapshot(previous app.Snapshot, hadPrevious bool, snap app.Snapshot, finalLoadVersion int64) {
	if snap.SessionID == "" {
		return
	}
	state := m.sessionNotificationState(snap.SessionID)
	if snap.FinalSaveFailures > state.seenFailures {
		state.seenFailures = snap.FinalSaveFailures
		if snap.SaveError != "" && !state.resultQueued {
			state.resultQueued = true
			m.emitNotification(m.resultNotification(snap, true, nil))
		}
	}

	sameSession := hadPrevious && previous.SessionID == snap.SessionID
	if sameSession && previous.Scan.InputPermitted && !snap.Scan.InputPermitted &&
		snap.Scan.InputOpened && !state.inputStopped {
		state.inputStopped = true
		m.emitNotification(Notification{
			ID:       NotificationID(string(snap.SessionID), NotificationInputStopped),
			Segment:  NotificationInputStopped,
			RecordID: string(snap.SessionID),
			Title:    "youkai 已停止操作游戏",
			Body:     "正在识别和保存结果，完成后会再通知你。现在可以正常使用电脑。",
		})
	}

	finishEdge := false
	if sameSession {
		finishEdge = previous.Busy && !snap.Busy
	} else {
		// A session first observed after it already ended (a quick
		// preparation failure) still reports its result.
		finishEdge = !snap.Busy && snap.Scan.Phase == scan.PhaseEnded
	}
	if !finishEdge || state.resultQueued {
		return
	}
	state.resultQueued = true
	m.queueResultNotification(snap, finalLoadVersion)
}

// queueResultNotification defers the result segment until the session's
// final record loaded. The wrap-up's save completed before the ended
// snapshot, so both this read and the workspace reload named by
// finalLoadVersion see the generation the summary shows. The segment is
// still sent once if that load fails, with the snapshot's conservative
// wording.
func (m *Model) queueResultNotification(snap app.Snapshot, finalLoadVersion int64) {
	id := snap.SessionID
	state := m.sessionNotificationState(id)
	if load := m.recordReads[finalLoadVersion]; finalLoadVersion != 0 && load != nil {
		state.pending = &pendingResult{
			snap:        snap,
			loadVersion: finalLoadVersion,
			load:        load,
		}
		return
	}
	load := newRecordLoad()
	state.pending = &pendingResult{snap: snap, load: load}
	m.goAsync(func() {
		rec, err := m.cfg.Service.LoadRecord(string(id))
		load.finish(rec, err)
		m.update(func() { m.settleResultNotification(id) })
	})
}

// settleResultNotification sends the deferred result exactly once after
// the notification's own load. A failed load falls back to the snapshot
// facts.
func (m *Model) settleResultNotification(id scan.ID) {
	m.settlePendingResult(id, 0)
}

// settleResultFromRecord settles a pending result from a workspace reload.
// The version keeps a stale read from standing in for the load the result
// actually waits for.
func (m *Model) settleResultFromRecord(id scan.ID, version int64) {
	m.settlePendingResult(id, version)
}

func (m *Model) settlePendingResult(id scan.ID, version int64) {
	state := m.notifySessions[id]
	if state == nil || state.pending == nil || state.pending.loadVersion != version {
		return
	}
	pending := state.pending
	state.pending = nil
	var rec *record.Recovery
	var err error
	if pending.load != nil {
		rec, err = pending.load.result()
	}
	if err != nil {
		rec = nil
	}
	m.emitNotification(m.resultNotification(pending.snap, false, rec))
}

// resultNotificationExitWait bounds how long the explicit exit waits for a
// pending result read before sending the conservative snapshot wording. The
// record is local; a normal read takes tens of milliseconds. One second
// tolerates a loaded machine without keeping the user in a closing window,
// and the exit is never blocked longer than this.
const resultNotificationExitWait = time.Second

// flushResultsBeforeExit settles every pending result before the entry
// point quits the application, whose event loop stops delivering the queued
// settle updates. A read still running is given the shared budget; a timeout
// or failure sends the conservative wording through the same once-only path.
func (m *Model) flushResultsBeforeExit() {
	deadline := time.Now().Add(resultNotificationExitWait)
	for id, state := range m.notifySessions {
		pending := state.pending
		if pending == nil {
			continue
		}
		if pending.load != nil {
			if remaining := time.Until(deadline); remaining > 0 && !pending.load.finished() {
				timer := time.NewTimer(remaining)
				select {
				case <-pending.load.done:
				case <-timer.C:
				}
				timer.Stop()
			}
		}
		m.settlePendingResult(id, pending.loadVersion)
	}
}

func (m *Model) emitNotification(n Notification) {
	if m.cfg.Notify != nil {
		m.cfg.Notify(n)
	}
}

// resultNotification assembles the second segment. saveFailed selects the
// save-failure wording; otherwise the final record's coverage and four-class
// counts decide the title and body, falling back to the snapshot when the
// record could not be read.
func (m *Model) resultNotification(snap app.Snapshot, saveFailed bool, rec *record.Recovery) Notification {
	facts := notificationFactsFor(snap, rec)
	facts.saveFailed = saveFailed
	title, body := notificationWording(facts)
	return Notification{
		ID:       NotificationID(string(snap.SessionID), NotificationResult),
		Segment:  NotificationResult,
		RecordID: string(snap.SessionID),
		Title:    title,
		Body:     body,
		Flash:    true,
	}
}

// snapshotHasSavedResults reports whether the session has a result to call
// saved. It reuses the notification facts, including the saved-to-captured
// fallback for a snapshot whose saved counter lags the final record, so the
// exit failure copy cannot claim a result the result notification says never
// started.
func snapshotHasSavedResults(snap app.Snapshot, rec *record.Recovery) bool {
	return notificationFactsFor(snap, rec).saved > 0
}

// notificationFactsFor reads the conclusion from the final record when it
// loaded. The summary builds its four-class cells and coverage from the same
// recovery, so both surfaces always agree.
func notificationFactsFor(snap app.Snapshot, rec *record.Recovery) notificationFacts {
	facts := notificationFacts{
		captured:    snap.Scan.Counts.Captured,
		saved:       snap.SavedCount,
		inputOpened: snap.Scan.InputOpened,
		end:         snap.Scan.End,
		trigger:     snap.Scan.Trigger,
	}
	if rec != nil {
		view := BuildRecordView(string(snap.SessionID), rec, false)
		facts.counts = view.Classes
		facts.countsKnown = true
		facts.coverage = view.Shell.Coverage.Status
		facts.outcomes, facts.outcomesKnown = app.PositionOutcomesOf(view.Shell)
		facts.failureParams = view.Shell.FailureParams
		if len(view.Rows) > facts.captured {
			facts.captured = len(view.Rows)
		}
	}
	if facts.saved == 0 && facts.captured > 0 {
		facts.saved = facts.captured
	}
	return facts
}

// notificationWording is the single copy table for the second segment. It
// uses the shared fault causes and actions (PR-09) and never prints a raw
// diagnostic code.
func notificationWording(facts notificationFacts) (string, string) {
	if facts.saveFailed {
		return "扫描结果还没有保存", "保存失败，需要在 youkai 里重试。点击打开。"
	}
	if !facts.inputOpened {
		// A user stop during preparation never opened input, but the
		// precheck did not fail: report the stop, not a failed check.
		if facts.trigger != nil && facts.trigger.Kind == scan.TriggerUser {
			return "扫描已手动停止", "还没有开始扫描。点击查看。"
		}
		return "没有开始扫描", preparationNotificationBody(facts)
	}
	if facts.end == scan.EndFailed ||
		(facts.trigger != nil && facts.trigger.Kind == scan.TriggerFault) {
		return faultNotificationWording(facts)
	}
	if facts.end == scan.EndCancelled ||
		(facts.trigger != nil && facts.trigger.Kind == scan.TriggerUser) {
		return "扫描已手动停止", fmt.Sprintf("已保存 %s 件。点击查看。", formatCount(int64(facts.saved)))
	}
	return completedNotificationWording(facts)
}

func preparationNotificationBody(facts notificationFacts) string {
	if facts.trigger != nil {
		if cause := faultCauseText(facts.trigger.Code, notificationParams(facts)); cause != "" {
			return cause + "。点击查看。"
		}
	}
	return "准备检查没有通过。点击查看。"
}

func faultNotificationWording(facts notificationFacts) (string, string) {
	code := ""
	params := notificationParams(facts)
	if facts.trigger != nil {
		code = facts.trigger.Code
	}
	cause := faultCauseText(code, params)
	if cause == "" {
		cause = "出现故障"
	}
	body := fmt.Sprintf("已保存 %s 件。", formatCount(int64(facts.saved)))
	if guidance, ok := faultGuidanceFor(code, params); ok {
		body += guidance.action
	}
	body += "点击查看。"
	return "扫描中途停止：" + cause, body
}

func notificationParams(facts notificationFacts) map[string]string {
	if facts.trigger != nil && len(facts.trigger.Params) > 0 {
		return facts.trigger.Params
	}
	return facts.failureParams
}

func completedNotificationWording(facts notificationFacts) (string, string) {
	if facts.countsKnown && facts.counts.Total() == 0 {
		return "扫描完成：没有采集到驱动盘", "点击查看。"
	}
	if !facts.countsKnown {
		return "扫描完成", fmt.Sprintf("已保存 %s 件。点击查看。", formatCount(int64(facts.saved)))
	}
	switch facts.coverage {
	case record.CoverageComplete:
		pending := facts.counts.Pending()
		exportable := facts.counts.Exportable()
		if pending == 0 {
			return notificationCompleteNoIssues,
				fmt.Sprintf("%s 件可以导出。点击查看结果。", formatCount(exportable))
		}
		return fmt.Sprintf("扫描完成：%s 件待校对", formatCount(pending)),
			fmt.Sprintf("覆盖全部，其余 %s 件可以导出。点击查看。", formatCount(exportable))
	case record.CoveragePartial:
		return "扫描完成，但没有扫全",
			fmt.Sprintf("%s，已保存 %s 件。点击查看原因。", incompletePositions(facts), formatCount(int64(facts.saved)))
	default:
		return "扫描完成", fmt.Sprintf("已保存 %s 件。点击查看。", formatCount(int64(facts.saved)))
	}
}

// incompletePositions names the degraded positions for a partial scan. A
// record without a usable ledger says only that coverage is partial rather
// than inventing position numbers.
func incompletePositions(facts notificationFacts) string {
	if !facts.outcomesKnown {
		return "没有扫全"
	}
	var positions []string
	for _, outcome := range facts.outcomes {
		if outcome.State == app.PositionOutcomeDegraded {
			positions = append(positions, fmt.Sprintf("%d 号位", outcome.Position))
		}
	}
	if len(positions) == 0 {
		return "没有扫全"
	}
	return strings.Join(positions, "、") + "不完整"
}
