package ui

import (
	"strings"
	"testing"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/LoTwT/youkai/internal/scan"
)

func notificationModel(t *testing.T) (*Model, *fakeService, *[]Notification) {
	t.Helper()
	svc := &fakeService{}
	var got []Notification
	m, _ := newTestModel(t, Config{
		Service: svc,
		Notify:  func(n Notification) { got = append(got, n) },
	})
	return m, svc, &got
}

func openCapturing(session string, rev uint64, captured int) app.Snapshot {
	s := snap(session, rev, true, scan.PhaseCapturing,
		scan.Counts{Captured: captured, Produced: captured}, captured, true, "")
	s.Scan.InputOpened = true
	s.Scan.InputPermitted = true
	return s
}

// TestNotificationsFollowInputStopAndWrapUp checks the two-stage order and
// the session-derived IDs.
func TestNotificationsFollowInputStopAndWrapUp(t *testing.T) {
	m, _, got := notificationModel(t)
	m.Apply(openCapturing("s1", 1, 3))
	if len(*got) != 0 {
		t.Fatalf("a running scan notified: %+v", *got)
	}

	stopping := openCapturing("s1", 2, 3)
	stopping.Scan.Phase = scan.PhaseFinalizing
	stopping.Scan.InputPermitted = false
	m.Apply(stopping)
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

	// The open record supplies the four-class counts and coverage.
	m.rec = &RecordView{
		ID:      "s1",
		Live:    true,
		Classes: app.ItemClassCounts{Passed: 3},
		Shell: record.SessionShell{
			Coverage: record.Coverage{Status: record.CoverageComplete},
		},
	}
	m.recData = &record.Recovery{}
	ended := openCapturing("s1", 3, 3)
	ended.Busy = false
	ended.SavePending = false
	ended.Scan.Phase = scan.PhaseEnded
	ended.Scan.End = scan.EndCompleted
	ended.Scan.InputPermitted = false
	m.Apply(ended)
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
	if second.Title != "扫描完成：扫全了" || second.Body != "3 件可以导出。点击查看结果。" {
		t.Fatalf("second notification text = %q / %q", second.Title, second.Body)
	}
}

// TestPrepareTimeoutSendsOnlyTheResult checks that a session that never
// opened input has no input-stop segment.
func TestPrepareTimeoutSendsOnlyTheResult(t *testing.T) {
	m, _, got := notificationModel(t)
	preparing := snap("s1", 1, true, scan.PhasePreparing, scan.Counts{}, 0, false, "")
	m.Apply(preparing)
	timeout := snap("s1", 2, false, scan.PhaseEnded, scan.Counts{}, 0, false, "")
	timeout.Scan.End = scan.EndFailed
	timeout.Scan.Trigger = &scan.Trigger{Kind: scan.TriggerFault, Code: "prepareTimeout", Phase: scan.PhasePreparing}
	m.Apply(timeout)
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

// TestSaveFailureNotifiesOncePerWrapUp checks the first final-save failure
// is announced once; later failures and a successful retry stay silent.
func TestSaveFailureNotifiesOncePerWrapUp(t *testing.T) {
	m, _, got := notificationModel(t)
	m.Apply(openCapturing("s1", 1, 3))
	stopping := openCapturing("s1", 2, 3)
	stopping.Scan.Phase = scan.PhaseFinalizing
	stopping.Scan.InputPermitted = false
	m.Apply(stopping)

	failed := stopping
	failed.Revision = 3
	failed.Scan.Phase = scan.PhaseEnded
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
	if len(*got) != 2 {
		t.Fatalf("a retry notified again: %+v", *got)
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
	m, svc, _ := notificationModel(t)
	m.Apply(openCapturing("s1", 1, 2))
	m.OpenNotification("s1")
	if m.displayPage() != pageScan {
		t.Fatalf("a click during the wrap-up opened %v, want the scan page", m.displayPage())
	}

	ended := openCapturing("s1", 2, 2)
	ended.Busy = false
	ended.Scan.Phase = scan.PhaseEnded
	ended.Scan.End = scan.EndCompleted
	ended.Scan.InputPermitted = false
	m.Apply(ended)
	m.OpenNotification("s1")
	if m.displayPage() != pageResults || m.recordTab != recordTabSummary {
		t.Fatalf("a finished click opened page %v tab %v, want the summary", m.displayPage(), m.recordTab)
	}
	if len(svc.starts) != 0 {
		t.Fatalf("a notification click started a scan: %v", svc.starts)
	}
}

// TestNotificationIDRoundTrip checks the session-derived identity and its
// rejection of other applications' IDs.
func TestNotificationIDRoundTrip(t *testing.T) {
	for _, segment := range []NotificationSegment{NotificationInputStopped, NotificationResult} {
		id := NotificationID("abc-def", segment)
		recordID, got, ok := ParseNotificationID(id)
		if !ok || recordID != "abc-def" || got != segment {
			t.Fatalf("parse %q = %q/%v/%v", id, recordID, got, ok)
		}
	}
	for _, id := range []string{"", "other", "youkai.scan.", "youkai.scan.s1", "youkai.scan.s1.other"} {
		if _, _, ok := ParseNotificationID(id); ok {
			t.Errorf("ParseNotificationID(%q) accepted a foreign ID", id)
		}
	}
}
