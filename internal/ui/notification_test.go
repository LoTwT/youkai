package ui

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/LoTwT/youkai/internal/scan"
)

func notificationModel(t *testing.T) (*Model, *fakeService, *[]Notification, *queueUpdater) {
	t.Helper()
	svc := &fakeService{loads: map[string]*record.Recovery{}}
	var got []Notification
	m, q := newTestModel(t, Config{
		Service: svc,
		Notify:  func(n Notification) { got = append(got, n) },
	})
	return m, svc, &got, q
}

func openCapturing(session string, rev uint64, captured int) app.Snapshot {
	s := snap(session, rev, true, scan.PhaseCapturing,
		scan.Counts{Captured: captured, Produced: captured}, captured, true, "")
	s.Scan.InputOpened = true
	s.Scan.InputPermitted = true
	return s
}

func stoppingSession(session string, rev uint64, captured int) app.Snapshot {
	s := openCapturing(session, rev, captured)
	s.Scan.Phase = scan.PhaseFinalizing
	s.Scan.InputPermitted = false
	return s
}

func endedSession(session string, rev uint64, captured, saved int) app.Snapshot {
	s := openCapturing(session, rev, captured)
	s.Busy = false
	s.SavePending = false
	s.SavedCount = saved
	s.Scan.Phase = scan.PhaseEnded
	s.Scan.End = scan.EndCompleted
	s.Scan.InputPermitted = false
	return s
}

// failedItems makes n captured items whose recognition failed: each one is
// a pending backfill, the four-class total a completed scan with unresolved
// items reports.
func failedItems(n int) []record.ItemRecord {
	items := make([]record.ItemRecord, n)
	for i := range items {
		items[i] = record.ItemRecord{Sequence: int64(i + 1), Status: record.StatusFailed}
	}
	return items
}

func completePositions() []record.PositionConclusion {
	out := make([]record.PositionConclusion, 0, record.PositionCount)
	for position := 1; position <= record.PositionCount; position++ {
		out = append(out, record.PositionConclusion{Position: position, Complete: true})
	}
	return out
}

func notificationRecord(items []record.ItemRecord, coverage record.CoverageStatus,
	positions []record.PositionConclusion) *record.Recovery {
	rec := &record.Recovery{Checkpoint: record.Checkpoint{
		Session: record.SessionShell{Coverage: record.Coverage{Status: coverage}},
		Items:   items,
	}}
	if positions != nil {
		rec.Checkpoint.Session.PositionLedger = &record.PositionLedger{Positions: positions}
	}
	return rec
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// TestNotificationsFollowInputStopAndWrapUp checks the two-stage order, the
// session-derived IDs, and that the result reads the loaded final record.
func TestNotificationsFollowInputStopAndWrapUp(t *testing.T) {
	m, svc, got, q := notificationModel(t)
	svc.loads["s1"] = notificationRecord(failedItems(3), record.CoverageComplete, completePositions())

	m.Apply(openCapturing("s1", 1, 3))
	drainAll(t, m, q)
	if len(*got) != 0 {
		t.Fatalf("a running scan notified: %+v", *got)
	}

	m.Apply(stoppingSession("s1", 2, 3))
	if len(*got) != 1 {
		t.Fatalf("input stop notifications = %d, want 1: %+v", len(*got), *got)
	}
	first := (*got)[0]
	if first.Segment != NotificationInputStopped || first.Title != "youkai 已停止操作游戏" {
		t.Fatalf("first notification = %+v", first)
	}
	if first.ID != NotificationID("s1", NotificationInputStopped) || first.RecordID != "s1" {
		t.Fatalf("first notification identity = %+v", first)
	}
	if first.Flash {
		t.Fatal("the input-stop notification flashes the taskbar")
	}

	m.Apply(endedSession("s1", 3, 3, 3))
	drainAll(t, m, q)
	if len(*got) != 2 {
		t.Fatalf("wrap-up notifications = %d, want the second segment: %+v", len(*got), *got)
	}
	second := (*got)[1]
	if second.Segment != NotificationResult || !second.Flash {
		t.Fatalf("second notification = %+v", second)
	}
	if second.ID != NotificationID("s1", NotificationResult) {
		t.Fatalf("second notification ID = %q", second.ID)
	}
	if second.Title != "扫描完成：3 件待校对" ||
		second.Body != "覆盖全部，其余 0 件可以导出。点击查看。" {
		t.Fatalf("second notification text = %q / %q", second.Title, second.Body)
	}
}

// TestResultNotificationWaitsForTheFinalRecord covers H-1 through all three
// outcomes: a record with pending items, a partial scan, and no captured
// item. The model's earlier load returns a stale generation, and both the
// terminal reload and the notification's own load are held; the result must
// wait for the final record instead of reading the stale view.
func TestResultNotificationWaitsForTheFinalRecord(t *testing.T) {
	cases := []struct {
		name      string
		captured  int
		saved     int
		final     *record.Recovery
		wantTitle string
		wantBody  string
	}{
		{
			name:     "complete with pending items",
			captured: 5,
			saved:    5,
			final: notificationRecord(failedItems(5), record.CoverageComplete,
				completePositions()),
			wantTitle: "扫描完成：5 件待校对",
			wantBody:  "覆盖全部，其余 0 件可以导出。点击查看。",
		},
		{
			name:     "partial coverage",
			captured: 5,
			saved:    5,
			final: notificationRecord(failedItems(5), record.CoveragePartial,
				[]record.PositionConclusion{
					{Position: 1, Complete: true},
					{Position: 2, Complete: true},
					{Position: 3, Stage: "filter", ReasonCode: "navigationMismatch"},
					{Position: 4, Complete: true},
					{Position: 5, Complete: true},
					{Position: 6, Complete: true},
				}),
			wantTitle: "扫描完成，但没有扫全",
			wantBody:  "3 号位不完整，已保存 5 件。点击查看原因。",
		},
		{
			name:      "nothing captured",
			captured:  0,
			saved:     0,
			final:     notificationRecord(nil, record.CoverageComplete, completePositions()),
			wantTitle: "扫描完成：没有采集到驱动盘",
			wantBody:  "点击查看。",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gap := make(chan struct{})
			var mu sync.Mutex
			calls := 0
			terminal := false
			// Loads from the capturing phase see an early, unrelated
			// generation; the terminal read is the final record and is held
			// until the test releases it.
			stale := notificationRecord(failedItems(1), record.CoverageUnknown, nil)
			svc := &fakeService{loadFn: func(string) (*record.Recovery, error) {
				mu.Lock()
				calls++
				isTerminal := terminal
				mu.Unlock()
				if !isTerminal {
					return stale, nil
				}
				<-gap
				return tc.final, nil
			}}
			var got []Notification
			m, q := newTestModel(t, Config{
				Service: svc,
				Notify:  func(n Notification) { got = append(got, n) },
			})

			m.Apply(openCapturing("s1", 1, tc.captured))
			drainAll(t, m, q)
			m.Apply(stoppingSession("s1", 2, tc.captured))
			if len(got) != 1 {
				t.Fatalf("input-stop notifications = %d, want 1", len(got))
			}
			mu.Lock()
			before := calls
			terminal = true
			mu.Unlock()
			m.Apply(endedSession("s1", 3, tc.captured, tc.saved))

			// The terminal reload starts; it may not release the result
			// before the final record is read.
			waitUntil(t, "the final record loads to start", func() bool {
				mu.Lock()
				defer mu.Unlock()
				return calls >= before+1
			})
			time.Sleep(20 * time.Millisecond)
			if len(got) != 1 {
				t.Fatalf("the result was sent before the final record loaded: %+v", got[1:])
			}
			close(gap)
			drainAll(t, m, q)
			if len(got) != 2 {
				t.Fatalf("result notifications = %d, want 2 total: %+v", len(got), got)
			}
			second := got[1]
			if second.Segment != NotificationResult || second.Title != tc.wantTitle ||
				second.Body != tc.wantBody {
				t.Fatalf("result notification = %+v, want %q / %q", second, tc.wantTitle, tc.wantBody)
			}
		})
	}
}

// TestResultNotificationSendsWhenTheFinalRecordCannotLoad checks the
// notification is never lost to a failed read and is not repeated after it.
func TestResultNotificationSendsWhenTheFinalRecordCannotLoad(t *testing.T) {
	m, svc, got, q := notificationModel(t)
	svc.loadErr = map[string]error{"s1": errors.New("no verifiable checkpoint")}
	m.Apply(openCapturing("s1", 1, 3))
	drainAll(t, m, q)
	m.Apply(stoppingSession("s1", 2, 3))
	m.Apply(endedSession("s1", 3, 3, 3))
	drainAll(t, m, q)
	if len(*got) != 2 {
		t.Fatalf("notifications = %d, want input stop plus the conservative result: %+v", len(*got), *got)
	}
	second := (*got)[1]
	if second.Title != "扫描完成" || second.Body != "已保存 3 件。点击查看。" {
		t.Fatalf("conservative result = %q / %q", second.Title, second.Body)
	}

	// A repeated terminal snapshot and a later retry must not send it again.
	m.Apply(endedSession("s1", 4, 3, 3))
	m.Apply(openCapturing("s1", 5, 3))
	m.Apply(endedSession("s1", 6, 3, 3))
	drainAll(t, m, q)
	if len(*got) != 2 {
		t.Fatalf("the failed-load result repeated: %+v", *got)
	}
}

// TestPrepareTimeoutSendsOnlyTheResult checks that a session that never
// opened input has no input-stop segment.
func TestPrepareTimeoutSendsOnlyTheResult(t *testing.T) {
	m, _, got, q := notificationModel(t)
	preparing := snap("s1", 1, true, scan.PhasePreparing, scan.Counts{}, 0, false, "")
	m.Apply(preparing)
	timeout := snap("s1", 2, false, scan.PhaseEnded, scan.Counts{}, 0, false, "")
	timeout.Scan.End = scan.EndFailed
	timeout.Scan.Trigger = &scan.Trigger{Kind: scan.TriggerFault, Code: "prepareTimeout", Phase: scan.PhasePreparing}
	m.Apply(timeout)
	drainAll(t, m, q)
	if len(*got) != 1 {
		t.Fatalf("prepare-timeout notifications = %d, want the result only: %+v", len(*got), *got)
	}
	if n := (*got)[0]; n.Segment != NotificationResult || n.Title != "没有开始扫描" {
		t.Fatalf("prepare-timeout notification = %+v", n)
	}
	if !strings.Contains((*got)[0].Body, "30 秒内游戏没有就绪") {
		t.Fatalf("prepare-timeout body = %q", (*got)[0].Body)
	}
}

// TestUserStopDuringPreparationReportsTheStop checks L-1: a stop before
// input opened is a manual stop, not a failed precheck.
func TestUserStopDuringPreparationReportsTheStop(t *testing.T) {
	m, _, got, q := notificationModel(t)
	m.Apply(snap("s1", 1, true, scan.PhasePreparing, scan.Counts{}, 0, false, ""))
	stopped := snap("s1", 2, false, scan.PhaseEnded, scan.Counts{}, 0, false, "")
	stopped.Scan.End = scan.EndCancelled
	stopped.Scan.Trigger = &scan.Trigger{Kind: scan.TriggerUser, Code: scan.CodeUserCancelled}
	m.Apply(stopped)
	drainAll(t, m, q)
	if len(*got) != 1 {
		t.Fatalf("preparation-stop notifications = %d, want 1: %+v", len(*got), *got)
	}
	if n := (*got)[0]; n.Title != "扫描已手动停止" || n.Body != "还没有开始扫描。点击查看。" {
		t.Fatalf("preparation-stop notification = %q / %q", n.Title, n.Body)
	}
}

// TestSaveFailureNotifiesOncePerWrapUp checks the first final-save failure
// is announced once; later failures and a successful retry stay silent.
func TestSaveFailureNotifiesOncePerWrapUp(t *testing.T) {
	m, _, got, q := notificationModel(t)
	m.Apply(openCapturing("s1", 1, 3))
	m.Apply(stoppingSession("s1", 2, 3))

	failed := endedSession("s1", 3, 3, 2)
	failed.Busy = true
	failed.Scan.End = scan.EndCancelled
	failed.Scan.Trigger = &scan.Trigger{Kind: scan.TriggerUser, Code: scan.CodeUserCancelled, Phase: scan.PhaseCapturing}
	failed.FinalSaveFailures = 1
	failed.SaveError = "disk full"
	m.Apply(failed)
	if len(*got) != 2 {
		t.Fatalf("save-failure notifications = %d, want input stop plus failure: %+v", len(*got), *got)
	}
	if n := (*got)[1]; n.Title != "扫描结果还没有保存" || !n.Flash {
		t.Fatalf("save-failure notification = %+v", n)
	}

	again := failed
	again.Revision = 4
	again.FinalSaveFailures = 2
	m.Apply(again)
	retried := again
	retried.Revision = 5
	retried.Busy = false
	retried.SaveError = ""
	m.Apply(retried)
	drainAll(t, m, q)
	if len(*got) != 2 {
		t.Fatalf("a retry notified again: %+v", *got)
	}
}

// TestNotificationEdgesSurviveSessionSwitches checks M-2: each session's
// announcements are keyed by session and a rollback to an already
// announced session does not repeat them; a genuinely new session still
// announces its own two segments.
func TestNotificationEdgesSurviveSessionSwitches(t *testing.T) {
	m, svc, got, q := notificationModel(t)
	svc.loads["a"] = notificationRecord(failedItems(2), record.CoverageComplete, completePositions())
	svc.loads["b"] = notificationRecord(failedItems(1), record.CoverageComplete, completePositions())

	m.Apply(openCapturing("a", 1, 2))
	drainAll(t, m, q)
	m.Apply(stoppingSession("a", 2, 2))
	m.Apply(endedSession("a", 3, 2, 2))
	drainAll(t, m, q)
	if len(*got) != 2 {
		t.Fatalf("session A notifications = %d, want 2: %+v", len(*got), *got)
	}

	m.Apply(openCapturing("b", 1, 1))
	drainAll(t, m, q)
	m.Apply(stoppingSession("b", 2, 1))
	m.Apply(endedSession("b", 3, 1, 1))
	drainAll(t, m, q)
	if len(*got) != 4 {
		t.Fatalf("session B notifications = %d total, want 4: %+v", len(*got), *got)
	}

	// A refused start published its placeholder, then Service.abort rolled
	// the service back to A. A's segments were already sent.
	m.Apply(openCapturing("c", 1, 0))
	m.Apply(endedSession("a", 4, 2, 2))
	drainAll(t, m, q)
	if len(*got) != 4 {
		t.Fatalf("the rollback to A repeated its notifications: %+v", *got)
	}
}

// TestFirstObservedEndedSessionReportsItsResult checks the quick-failure
// path: a session first seen after it ended still produces the result.
func TestFirstObservedEndedSessionReportsItsResult(t *testing.T) {
	m, svc, got, q := notificationModel(t)
	svc.loads["quick"] = notificationRecord(failedItems(1), record.CoverageComplete, completePositions())
	m.Apply(endedSession("quick", 1, 1, 1))
	drainAll(t, m, q)
	if len(*got) != 1 {
		t.Fatalf("quick-failure notifications = %d, want one result: %+v", len(*got), *got)
	}
	if n := (*got)[0]; n.Segment != NotificationResult || n.Title != "扫描完成：1 件待校对" {
		t.Fatalf("quick-failure notification = %+v", n)
	}
}

// TestNotificationWording covers every second-segment variant.
func TestNotificationWording(t *testing.T) {
	outcomes := app.PositionOutcomes([]record.PositionConclusion{
		{Position: 1, Complete: true},
		{Position: 2, Complete: true},
		{Position: 3, Stage: "wheel", ReasonCode: "focusLost"},
		{Position: 4, Complete: true},
		{Position: 5, Stage: "scroll", ReasonCode: "positionLost"},
	})
	cases := []struct {
		name  string
		facts notificationFacts
		title string
		body  string
	}{
		{
			name: "complete and clean",
			facts: notificationFacts{
				inputOpened: true, end: scan.EndCompleted, saved: 1017,
				countsKnown: true, counts: app.ItemClassCounts{Passed: 1017},
				coverage: record.CoverageComplete,
			},
			title: "扫描完成：扫全了",
			body:  "1,017 件可以导出。点击查看结果。",
		},
		{
			name: "complete with pending",
			facts: notificationFacts{
				inputOpened: true, end: scan.EndCompleted, saved: 1017,
				countsKnown: true,
				counts:      app.ItemClassCounts{PendingReview: 1, Passed: 1016},
				coverage:    record.CoverageComplete,
			},
			title: "扫描完成：1 件待校对",
			body:  "覆盖全部，其余 1,016 件可以导出。点击查看。",
		},
		{
			name: "partial coverage",
			facts: notificationFacts{
				inputOpened: true, end: scan.EndCompleted, saved: 612,
				countsKnown: true, counts: app.ItemClassCounts{Passed: 612},
				coverage: record.CoveragePartial,
				outcomes: outcomes, outcomesKnown: true,
			},
			title: "扫描完成，但没有扫全",
			body:  "3 号位、5 号位不完整，已保存 612 件。点击查看原因。",
		},
		{
			name: "manual stop",
			facts: notificationFacts{
				inputOpened: true, end: scan.EndCancelled, saved: 431,
				trigger: &scan.Trigger{Kind: scan.TriggerUser, Code: scan.CodeUserCancelled},
			},
			title: "扫描已手动停止",
			body:  "已保存 431 件。点击查看。",
		},
		{
			name: "manual stop before input",
			facts: notificationFacts{
				inputOpened: false, end: scan.EndCancelled,
				trigger: &scan.Trigger{Kind: scan.TriggerUser, Code: scan.CodeUserCancelled},
			},
			title: "扫描已手动停止",
			body:  "还没有开始扫描。点击查看。",
		},
		{
			name: "fault",
			facts: notificationFacts{
				inputOpened: true, end: scan.EndFailed, saved: 431,
				trigger: &scan.Trigger{Kind: scan.TriggerFault, Code: "focusLost"},
			},
			title: "扫描中途停止：游戏失去了焦点",
			body:  "已保存 431 件。扫描时请不要切换应用。点击查看。",
		},
		{
			name: "prepare timeout",
			facts: notificationFacts{
				inputOpened: false, end: scan.EndFailed,
				trigger: &scan.Trigger{Kind: scan.TriggerFault, Code: "prepareTimeout"},
			},
			title: "没有开始扫描",
			body:  "30 秒内游戏没有就绪。点击查看。",
		},
		{
			name: "save failure",
			facts: notificationFacts{
				inputOpened: true, end: scan.EndCancelled, saved: 12, saveFailed: true,
			},
			title: "扫描结果还没有保存",
			body:  "保存失败，需要在 youkai 里重试。点击打开。",
		},
		{
			name: "nothing captured",
			facts: notificationFacts{
				inputOpened: true, end: scan.EndCompleted,
				countsKnown: true, coverage: record.CoverageComplete,
			},
			title: "扫描完成：没有采集到驱动盘",
			body:  "点击查看。",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			title, body := notificationWording(tc.facts)
			if title != tc.title || body != tc.body {
				t.Fatalf("wording = %q / %q, want %q / %q", title, body, tc.title, tc.body)
			}
		})
	}
}

// TestOpenNotificationTargetsTheWrapUpOrTheSummary checks the click target
// and that it never starts a scan.
func TestOpenNotificationTargetsTheWrapUpOrTheSummary(t *testing.T) {
	m, svc, _, q := notificationModel(t)
	svc.loads["s1"] = notificationRecord(failedItems(2), record.CoverageComplete, completePositions())
	m.Apply(openCapturing("s1", 1, 2))
	drainAll(t, m, q)
	m.OpenNotification("s1")
	if m.displayPage() != pageScan {
		t.Fatalf("a click during the wrap-up opened %v, want the scan page", m.displayPage())
	}

	m.Apply(endedSession("s1", 2, 2, 2))
	drainAll(t, m, q)
	// The same record is open on the item list; the click still lands on
	// the summary (L-2).
	m.recordTab = recordTabItems
	m.page = pageSettings
	m.OpenNotification("s1")
	if m.displayPage() != pageResults || m.recordTab != recordTabSummary {
		t.Fatalf("a finished click opened page %v tab %v, want the summary", m.displayPage(), m.recordTab)
	}
	if len(svc.starts) != 0 {
		t.Fatalf("a notification click started a scan: %v", svc.starts)
	}
}

// TestOpenNotificationKeepsAnUnsavedDraft checks that the click goes
// through the existing navigation guard instead of replacing an editor.
func TestOpenNotificationKeepsAnUnsavedDraft(t *testing.T) {
	m, q, tt, svc := openReviewableResults(t, testRecord(), 1180, 760)
	m.startEdit()
	if m.edit == nil {
		t.Skip("the fixture has no editable item")
	}
	m.edit.draft.Level = "13"
	if !m.editingDirty() {
		t.Fatal("the fixture draft is not dirty")
	}
	svc.mu.Lock()
	svc.loads["other"] = testRecord()
	svc.mu.Unlock()

	m.OpenNotification("other")
	settle(t, m, q, tt)
	if m.rec == nil || m.rec.ID != "rec" {
		t.Fatalf("the click replaced the record with %+v", m.rec)
	}
	if m.edit == nil {
		t.Fatal("the click discarded the unsaved draft")
	}
	if m.navPrompt == nil {
		t.Fatal("the click bypassed the unsaved-input prompt")
	}
}

// TestOpenNotificationClosesConflictingDialogs checks L-3: the hint's
// pending hide and an open export dialog do not survive the click.
func TestOpenNotificationClosesConflictingDialogs(t *testing.T) {
	hides := 0
	svc := &fakeService{loads: map[string]*record.Recovery{"rec": testRecord()}}
	m, q := newTestModel(t, Config{
		Service:    svc,
		HideToTray: func() { hides++ },
	})
	m.SetContentSize(1180, 760)
	m.showRecord("rec", true)
	m.reloadRecord("rec", true)
	drainAll(t, m, q)

	m.ShowFirstHideHint()
	if !m.trayHintOpen || !m.trayHintHideOnDone {
		t.Fatal("the first-hide hint did not open on the close path")
	}
	m.OpenNotification("rec")
	if m.trayHintOpen || m.trayHintHideOnDone {
		t.Fatalf("the notification click left the hint open=%v hideOnDone=%v",
			m.trayHintOpen, m.trayHintHideOnDone)
	}
	m.acknowledgeTrayHint()
	if hides != 0 {
		t.Fatalf("acknowledging the dismissed hint hid the window %d time(s)", hides)
	}

	f := newExportDialogFixture(t, testRecord(), false)
	f.open(t)
	f.m.OpenNotification("rec")
	if f.m.exportDialog.open {
		t.Fatal("the notification click left the export dialog open")
	}
}

// TestOpenNotificationDuringExitDoesNotSwitchPages checks L-4: the click
// restores the window but cannot move the exiting window off its page.
func TestOpenNotificationDuringExitDoesNotSwitchPages(t *testing.T) {
	m, svc, _, q := notificationModel(t)
	svc.loads["s1"] = notificationRecord(failedItems(1), record.CoverageComplete, completePositions())
	m.Apply(openCapturing("s1", 1, 1))
	m.Apply(endedSession("s1", 2, 1, 1))
	drainAll(t, m, q)
	m.exitState = exitRunning
	m.page = pageSettings
	m.OpenNotification("s1")
	if m.page != pageSettings {
		t.Fatalf("a click during the exit moved the page to %v", m.page)
	}
}

// waitQueued runs the FIFO updater until pred holds. It does not wait for
// model background work, so a deliberately held load does not block it.
func waitQueued(t *testing.T, q *queueUpdater, what string, pred func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, fn := range q.take() {
			fn()
		}
		if pred() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// TestResultNotificationReadsWhenAnotherRecordIsOpen covers
// own-read-removed: with no workspace read for the ended session, the
// notification starts its own read instead of waiting forever.
func TestResultNotificationReadsWhenAnotherRecordIsOpen(t *testing.T) {
	m, svc, got, q := notificationModel(t)
	svc.loads["a"] = notificationRecord(failedItems(2), record.CoverageComplete, completePositions())
	svc.loads["x"] = notificationRecord(failedItems(1), record.CoverageUnknown, nil)
	m.Apply(openCapturing("a", 1, 2))
	drainAll(t, m, q)
	m.Apply(stoppingSession("a", 2, 2))
	m.openRecord("x")
	drainAll(t, m, q)
	m.Apply(endedSession("a", 3, 2, 2))
	drainAll(t, m, q)

	results := resultNotifications(*got)
	if len(results) != 1 {
		t.Fatalf("results with another record open = %d, want 1: %+v", len(results), *got)
	}
	if results[0].Title != "扫描完成：2 件待校对" {
		t.Fatalf("result = %q, want the ended session's counts", results[0].Title)
	}
	if m.rec == nil || m.rec.ID != "x" {
		t.Fatalf("the open record changed to %+v", m.rec)
	}
}

// TestResultNotificationReadsWithDirtyDraft covers the other
// own-read-removed path: a dirty editor keeps the workspace on its record,
// so the notification reads the ended session itself.
func TestResultNotificationReadsWithDirtyDraft(t *testing.T) {
	m, q, tt, svc := openReviewableResults(t, testRecord(), 1180, 760)
	var got []Notification
	m.cfg.Notify = func(n Notification) { got = append(got, n) }
	m.startEdit()
	if m.edit == nil {
		t.Skip("the fixture has no editable item")
	}
	m.edit.draft.Level = "13"
	if !m.editingDirty() {
		t.Fatal("the fixture draft is not dirty")
	}
	svc.mu.Lock()
	svc.loads["a"] = notificationRecord(failedItems(2), record.CoverageComplete, completePositions())
	svc.mu.Unlock()

	m.Apply(openCapturing("a", 1, 2))
	m.Apply(stoppingSession("a", 2, 2))
	m.Apply(endedSession("a", 3, 2, 2))
	drainAll(t, m, q)
	tt.Frame()

	results := resultNotifications(got)
	if len(results) != 1 {
		t.Fatalf("results with a dirty draft = %d, want 1: %+v", len(results), got)
	}
	if results[0].Title != "扫描完成：2 件待校对" {
		t.Fatalf("result = %q, want the ended session's counts", results[0].Title)
	}
	if m.edit == nil || !m.editingDirty() {
		t.Fatal("the notification discarded the unsaved draft")
	}
	if m.rec == nil || m.rec.ID != "rec" {
		t.Fatalf("the notification replaced the open record with %+v", m.rec)
	}
}

// TestResultNotificationFollowsARestartedLoad covers the pending read
// following a newer load of the same record. The older read returns a
// stale generation; the notification must wait for the newer one and read
// the final counts.
func TestResultNotificationFollowsARestartedLoad(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	oldGate := make(chan struct{})
	newGate := make(chan struct{})
	stale := notificationRecord(nil, record.CoverageUnknown, nil)
	final := notificationRecord(failedItems(2), record.CoverageComplete, completePositions())
	svc := &fakeService{loadFn: func(string) (*record.Recovery, error) {
		mu.Lock()
		calls++
		call := calls
		mu.Unlock()
		switch call {
		case 1:
			return final, nil
		case 2:
			<-oldGate
			return stale, nil
		default:
			<-newGate
			return final, nil
		}
	}}
	var got []Notification
	m, q := newTestModel(t, Config{
		Service: svc,
		Notify:  func(n Notification) { got = append(got, n) },
	})
	m.Apply(openCapturing("a", 1, 2))
	drainAll(t, m, q)
	m.Apply(stoppingSession("a", 2, 2))
	m.Apply(endedSession("a", 3, 2, 2))
	waitQueued(t, q, "the terminal workspace read", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return calls >= 2
	})
	if pending := m.notifySessions["a"].pending; pending == nil {
		t.Fatal("no result waits for the terminal read")
	}

	// A newer load of the same record adopts the pending result.
	m.reloadRecord("a", true)
	waitQueued(t, q, "the restarted workspace read", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return calls >= 3
	})
	close(oldGate)
	time.Sleep(30 * time.Millisecond)
	for _, fn := range q.take() {
		fn()
	}
	if results := resultNotifications(got); len(results) != 0 {
		t.Fatalf("the superseded read settled the result: %+v", results)
	}
	close(newGate)
	waitQueued(t, q, "the final result", func() bool {
		return len(resultNotifications(got)) == 1
	})
	drainAll(t, m, q)
	results := resultNotifications(got)
	if len(results) != 1 || results[0].Title != "扫描完成：2 件待校对" {
		t.Fatalf("result after the restarted read = %+v", results)
	}
}

// TestResultNotificationSettlesASupersededWorkspaceRead covers
// superseded-no-settle: opening another record supersedes the read the
// result waits on, but that read still settles the result.
func TestResultNotificationSettlesASupersededWorkspaceRead(t *testing.T) {
	gate := make(chan struct{})
	var mu sync.Mutex
	block := false
	final := notificationRecord(failedItems(2), record.CoverageComplete, completePositions())
	other := notificationRecord(failedItems(1), record.CoverageUnknown, nil)
	svc := &fakeService{loadFn: func(id string) (*record.Recovery, error) {
		mu.Lock()
		held := block && id == "a"
		mu.Unlock()
		if held {
			<-gate
		}
		if id == "x" {
			return other, nil
		}
		return final, nil
	}}
	var got []Notification
	m, q := newTestModel(t, Config{
		Service: svc,
		Notify:  func(n Notification) { got = append(got, n) },
	})
	m.Apply(openCapturing("a", 1, 2))
	drainAll(t, m, q)
	m.Apply(stoppingSession("a", 2, 2))
	mu.Lock()
	block = true
	mu.Unlock()
	m.Apply(endedSession("a", 3, 2, 2))
	waitQueued(t, q, "the held workspace read", func() bool {
		return m.notifySessions["a"].pending != nil
	})

	// Another record takes the workspace; the held read is superseded but
	// still carries A's final generation.
	m.openRecord("x")
	waitQueued(t, q, "record x", func() bool {
		return m.rec != nil && m.rec.ID == "x" && !m.loading
	})
	close(gate)
	waitQueued(t, q, "the superseded result", func() bool {
		return len(resultNotifications(got)) == 1
	})
	drainAll(t, m, q)
	if results := resultNotifications(got); len(results) != 1 ||
		results[0].Title != "扫描完成：2 件待校对" {
		t.Fatalf("superseded result = %+v", results)
	}
	if m.rec == nil || m.rec.ID != "x" {
		t.Fatalf("the workspace left record x: %+v", m.rec)
	}
}

// TestOpenNotificationDoesNotMarkTheHintSeen covers
// open-hint-marks-seen: dismissing the close hint by clicking a
// notification is not an acknowledgement.
func TestOpenNotificationDoesNotMarkTheHintSeen(t *testing.T) {
	saved := 0
	svc := &fakeService{loads: map[string]*record.Recovery{"rec": testRecord()}}
	m, q := newTestModel(t, Config{
		Service:          svc,
		SaveTrayHintSeen: func(bool) error { saved++; return nil },
	})
	m.showRecord("rec", true)
	m.reloadRecord("rec", true)
	drainAll(t, m, q)
	m.ShowFirstHideHint()
	m.OpenNotification("rec")
	if !m.FirstHideHintDue() {
		t.Fatal("a notification click acknowledged the first-hide hint")
	}
	if saved != 0 {
		t.Fatalf("a notification click persisted the hint seen: %d write(s)", saved)
	}
}

// TestOpenNotificationKeepsTheItemsTabWithADirtyDraft covers
// open-tab-ignores-dirty: the click must ask before switching away from the
// open editor, and staying keeps both the tab and the draft.
func TestOpenNotificationKeepsTheItemsTabWithADirtyDraft(t *testing.T) {
	m, q, tt, _ := openReviewableResults(t, testRecord(), 1180, 760)
	m.startEdit()
	if m.edit == nil {
		t.Skip("the fixture has no editable item")
	}
	m.edit.draft.Level = "13"
	m.recordTab = recordTabItems
	m.OpenNotification("rec")
	settle(t, m, q, tt)
	if m.navPrompt == nil {
		t.Fatal("the click bypassed the unsaved-input prompt")
	}
	if m.recordTab != recordTabItems {
		t.Fatalf("the click switched the dirty editor to tab %v", m.recordTab)
	}
	if m.edit == nil || !m.editingDirty() {
		t.Fatal("the click discarded the dirty editor")
	}
	if err := tt.Click("留在这里"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if m.recordTab != recordTabItems || m.edit == nil || m.edit.draft.Level != "13" {
		t.Fatalf("staying lost the editor or switched tabs: %s", probeReviewState(m))
	}
}

// TestOpenNotificationDuringSaveDoesNotSwitchTabs covers the saving half of
// the notification guard: requestNavigation refuses the tab switch while a
// revision write is in flight.
func TestOpenNotificationDuringSaveDoesNotSwitchTabs(t *testing.T) {
	m, _, tt, _ := openReviewableResults(t, testRecord(), 1180, 760)
	tt.Frame()
	if m.edit == nil {
		t.Fatal("the pending row did not open its editor")
	}
	m.recordTab = recordTabItems
	m.reviewSaving = true
	m.OpenNotification("rec")
	tt.Frame()
	if m.navPrompt != nil || m.recordTab != recordTabItems || m.edit == nil {
		t.Fatalf("saving notification changed navigation: %s", probeReviewState(m))
	}
	if !strings.Contains(m.reviewErr, "正在保存") {
		t.Fatalf("saving notification did not report the guard: %q", m.reviewErr)
	}
}

// TestOpenNotificationDuringExportKeepsTheExportRecord covers N-2: while
// the frozen export is being written, a click for another record must not
// swap the dialog's numbers.
func TestOpenNotificationDuringExportKeepsTheExportRecord(t *testing.T) {
	f := newExportDialogFixture(t, testRecord(), false)
	other := testRecord()
	other.Checkpoint.Session.SessionID = "other"
	f.svc.mu.Lock()
	f.svc.loads["other"] = other
	f.svc.mu.Unlock()

	hold := make(chan struct{})
	f.saver.saveFn = func(app.ExportFormat, string, []byte) (string, error) {
		<-hold
		return "/tmp/export.json", nil
	}
	f.open(t)
	f.m.startExport(app.ExportJSON)
	if !f.m.exporting || !f.m.exportDialog.open {
		t.Fatalf("the export did not start: exporting=%v open=%v", f.m.exporting, f.m.exportDialog.open)
	}

	f.m.OpenNotification("other")
	if f.m.rec == nil || f.m.rec.ID != "rec" {
		t.Fatalf("the click swapped the record under the export: %+v", f.m.rec)
	}
	if !f.m.exportDialog.open || !f.m.exporting {
		t.Fatalf("the click closed the export: open=%v exporting=%v", f.m.exportDialog.open, f.m.exporting)
	}

	close(hold)
	drainAll(t, f.m, f.q)
	if f.m.exporting {
		t.Fatal("the export did not finish")
	}
}

// TestNotificationIDRoundTrip checks the session-derived identity and its
// rejection of other applications' IDs and empty record IDs.
func TestNotificationIDRoundTrip(t *testing.T) {
	for _, segment := range []NotificationSegment{NotificationInputStopped, NotificationResult} {
		id := NotificationID("abc-def", segment)
		recordID, got, ok := ParseNotificationID(id)
		if !ok || recordID != "abc-def" || got != segment {
			t.Fatalf("parse %q = %q/%v/%v", id, recordID, got, ok)
		}
	}
	for _, id := range []string{
		"", "other", "youkai.scan.", "youkai.scan..result",
		"youkai.scan..input-stopped", "youkai.scan.s1", "youkai.scan.s1.other",
	} {
		if _, _, ok := ParseNotificationID(id); ok {
			t.Errorf("ParseNotificationID(%q) accepted a foreign ID", id)
		}
	}
}
