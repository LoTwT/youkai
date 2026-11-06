package ui

import (
	"fmt"
	"strings"

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
	if m.appliedSession == scan.ID(recordID) && m.snap.Busy {
		// A dirty editor keeps its own prompt; the notification must not
		// discard the draft just to reveal the wrap-up.
		m.navigatePage(pageScan)
		return
	}
	m.openRecord(recordID)
}

// notificationFacts is the record data the notification wording reads. The
// live record supplies the four-class counts and position ledger; the
// service snapshot supplies the ending and, when the record has not loaded
// yet, the captured and saved counts.
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

// notifySnapshot derives the two notification edges from accepted service
// snapshots. The first segment only follows an observed input permission
// closing; a session that never opened input (prepare timeout, precheck
// failure) produces the second segment alone. A wrap-up's first save
// failure replaces the result notification and is sent once.
func (m *Model) notifySnapshot(previous app.Snapshot, hadPrevious bool, snap app.Snapshot) {
	if snap.SessionID == "" {
		return
	}
	if snap.SessionID != m.notifySession {
		m.notifySession = snap.SessionID
		m.notifyInputStopped = false
		m.notifyFinished = false
		m.notifySaveFailure = false
		m.notifySeenFailures = 0
	}
	failureEdge := snap.FinalSaveFailures > m.notifySeenFailures
	m.notifySeenFailures = snap.FinalSaveFailures

	sameSession := hadPrevious && previous.SessionID == snap.SessionID
	if sameSession && previous.Scan.InputPermitted && !snap.Scan.InputPermitted &&
		snap.Scan.InputOpened && !m.notifyInputStopped {
		m.notifyInputStopped = true
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
	if failureEdge && snap.SaveError != "" {
		if !m.notifyFinished {
			m.notifySaveFailure = true
			m.notifyFinished = true
			m.emitNotification(m.resultNotification(snap, true))
		}
		return
	}
	if finishEdge && !m.notifyFinished && !m.notifySaveFailure {
		m.notifyFinished = true
		m.emitNotification(m.resultNotification(snap, false))
	}
}

func (m *Model) emitNotification(n Notification) {
	if m.cfg.Notify != nil {
		m.cfg.Notify(n)
	}
}

// resultNotification assembles the second segment. saveFailed selects the
// save-failure wording; otherwise the ending, coverage and four-class counts
// decide the title and body.
func (m *Model) resultNotification(snap app.Snapshot, saveFailed bool) Notification {
	facts := m.notificationFactsFor(snap)
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

func (m *Model) notificationFactsFor(snap app.Snapshot) notificationFacts {
	facts := notificationFacts{
		captured:    snap.Scan.Counts.Captured,
		saved:       snap.SavedCount,
		inputOpened: snap.Scan.InputOpened,
		end:         snap.Scan.End,
		trigger:     snap.Scan.Trigger,
	}
	if m.rec != nil && m.rec.ID == string(snap.SessionID) && m.recData != nil {
		facts.counts = m.rec.Classes
		facts.countsKnown = true
		facts.coverage = m.rec.Shell.Coverage.Status
		facts.outcomes, facts.outcomesKnown = app.PositionOutcomesOf(m.rec.Shell)
		facts.failureParams = m.rec.Shell.FailureParams
		if len(m.rec.Rows) > facts.captured {
			facts.captured = len(m.rec.Rows)
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
