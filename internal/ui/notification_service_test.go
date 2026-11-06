package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/LoTwT/youkai/internal/scan"
)

// notificationServiceController drives a real app.Service through a short
// collection: every admitted item fails recognition, which leaves it in the
// pending-backfill class, and the final scope report supplies coverage and
// the position ledger.
type notificationServiceController struct {
	items int
	scope json.RawMessage
	sink  func(json.RawMessage)
}

func (c *notificationServiceController) SetScopeSink(sink func(json.RawMessage)) {
	c.sink = sink
}

func (c *notificationServiceController) Prepare(context.Context, *scan.ControllerHandle) error {
	return nil
}

func (c *notificationServiceController) Collect(ctx context.Context, h *scan.ControllerHandle) error {
	for i := 0; i < c.items; i++ {
		slot := h.Acquire(ctx)
		if slot == nil {
			return ctx.Err()
		}
		observation := app.Observation{Lines: []app.LineCrop{{
			Role:   "name",
			Source: fmt.Sprintf("probe-%d", i),
			Image:  image.NewRGBA(image.Rect(0, 0, 4, 4)),
		}}}
		if _, err := slot.Admit(observation); err != nil {
			slot.Discard()
			return err
		}
		time.Sleep(10 * time.Millisecond)
	}
	if c.sink != nil && c.scope != nil {
		c.sink(c.scope)
	}
	return nil
}

// notificationServiceRecognizer fails every item: the resulting record has
// captured items but no usable data, the four-class "pending" outcome.
type notificationServiceRecognizer struct{}

func (notificationServiceRecognizer) Recognize(_ context.Context, task scan.Task, submit scan.Submit) error {
	return submit(task.SessionID, task.Seq, scan.Recognition{Failed: errors.New("ocr failed")})
}

func (notificationServiceRecognizer) Close() error { return nil }

// notificationServiceSource hands the service one controller and can refuse
// a later start after a delay, so the refused start's placeholder session is
// observable before the service rolls back.
type notificationServiceSource struct {
	controller *notificationServiceController
	mu         sync.Mutex
	refuse     bool
	delay      time.Duration
}

func (s *notificationServiceSource) NewScan() (app.ScanSpec, error) {
	s.mu.Lock()
	controller, refuse, delay := s.controller, s.refuse, s.delay
	s.mu.Unlock()
	if delay > 0 {
		time.Sleep(delay)
	}
	if refuse {
		return app.ScanSpec{}, errors.New("source refused")
	}
	return app.ScanSpec{
		Controller: controller,
		Game:       "zzz",
		Entity:     "driveDisc",
		Client:     record.ClientWindowsLocal,
		Locale:     "zh-CN",
		Scope:      json.RawMessage(`{"kind":"test"}`),
	}, nil
}

func (s *notificationServiceSource) refuseNext(delay time.Duration) {
	s.mu.Lock()
	s.refuse, s.delay = true, delay
	s.mu.Unlock()
}

func completeScanScope() json.RawMessage {
	return json.RawMessage(`{"kind":"zzzInventory","sort":"verified","coverage":"complete","positions":[
		{"position":1,"complete":true},{"position":2,"complete":true},{"position":3,"complete":true},
		{"position":4,"complete":true},{"position":5,"complete":true},{"position":6,"complete":true}]}`)
}

func partialScanScope() json.RawMessage {
	return json.RawMessage(`{"kind":"zzzInventory","sort":"verified","coverage":"partial","positions":[
		{"position":1,"complete":true},{"position":2,"complete":true},
		{"position":3,"complete":false,"stage":"filter","reasonCode":"navigationMismatch","reason":"x"},
		{"position":4,"complete":true},{"position":5,"complete":true},{"position":6,"complete":true}]}`)
}

// notificationServiceRig joins a real service, the model and its queued
// updater. The subscription pump publishes into the queue; only the test
// goroutine runs model code.
type notificationServiceRig struct {
	t   *testing.T
	svc *app.Service
	m   *Model
	q   *queueUpdater
	got *[]Notification
}

func newNotificationServiceRig(t *testing.T, source app.ScanSource) *notificationServiceRig {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	paths := app.Paths{
		Data:      filepath.Join(t.TempDir(), "data"),
		Resources: filepath.Join(t.TempDir(), "resources"),
	}
	lock, err := app.AcquireDataRootLock(paths.Data)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := app.NewService(app.Config{
		Paths:    paths,
		DataLock: lock,
		Source:   source,
		NewRecognizer: func() (app.Recognizer, error) {
			return notificationServiceRecognizer{}, nil
		},
		ShutdownOCR: func() error { return nil },
	})
	if err != nil {
		lock.Close()
		t.Fatal(err)
	}
	var got []Notification
	m, q := newTestModel(t, Config{
		Service: svc,
		Notify:  func(n Notification) { got = append(got, n) },
	})
	sub := svc.Subscribe()
	initial := sub.Initial
	q.Update(func() { m.Apply(initial) })
	go func() {
		for range sub.Changes {
			snap := svc.Snapshot()
			q.Update(func() { m.Apply(snap) })
		}
	}()
	t.Cleanup(func() {
		if err := svc.Quit(); err != nil {
			t.Logf("quit after the test: %v", err)
		}
	})
	return &notificationServiceRig{t: t, svc: svc, m: m, q: q, got: &got}
}

// until drains queued snapshots until pred holds, then settles the model's
// background work and drains the results once more.
func (r *notificationServiceRig) until(what string, pred func() bool) {
	r.t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		drainAll(r.t, r.m, r.q)
		if pred() {
			drainAll(r.t, r.m, r.q)
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	r.t.Fatalf("timed out waiting for %s", what)
}

func (r *notificationServiceRig) waitNotifications(n int) []Notification {
	r.until(fmt.Sprintf("%d notification(s)", n), func() bool { return len(*r.got) >= n })
	return *r.got
}

// TestResultNotificationMatchesTheFinalRecord drives the real service
// through the three conclusions H-1 names. The result segment must read the
// record the summary reads, not the generation loaded while capturing.
func TestResultNotificationMatchesTheFinalRecord(t *testing.T) {
	cases := []struct {
		name      string
		items     int
		scope     json.RawMessage
		wantTitle string
		wantBody  string
	}{
		{
			name:      "complete with pending items",
			items:     5,
			scope:     completeScanScope(),
			wantTitle: "扫描完成：5 件待校对",
			wantBody:  "覆盖全部，其余 0 件可以导出。点击查看。",
		},
		{
			name:      "partial coverage",
			items:     5,
			scope:     partialScanScope(),
			wantTitle: "扫描完成，但没有扫全",
			wantBody:  "3 号位不完整，已保存 5 件。点击查看原因。",
		},
		{
			name:      "nothing captured",
			items:     0,
			scope:     completeScanScope(),
			wantTitle: "扫描完成：没有采集到驱动盘",
			wantBody:  "点击查看。",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source := &notificationServiceSource{controller: &notificationServiceController{
				items: tc.items,
				scope: tc.scope,
			}}
			r := newNotificationServiceRig(t, source)
			out, err := r.svc.Start(app.StartRequest{ID: "final-record"})
			if err != nil {
				t.Fatal(err)
			}
			got := r.waitNotifications(2)
			if got[0].Segment != NotificationInputStopped || got[1].Segment != NotificationResult {
				t.Fatalf("segments = %v / %v, want input stop then result", got[0].Segment, got[1].Segment)
			}
			if got[1].ID != NotificationID(string(out.SessionID), NotificationResult) {
				t.Fatalf("result ID = %q, want the session-derived result ID", got[1].ID)
			}
			if got[1].Title != tc.wantTitle || got[1].Body != tc.wantBody {
				t.Fatalf("result = %q / %q, want %q / %q",
					got[1].Title, got[1].Body, tc.wantTitle, tc.wantBody)
			}
			if !got[1].Flash {
				t.Fatal("the result notification does not flash the taskbar")
			}
		})
	}
}

// TestRefusedStartRollbackDoesNotRepeatTheResult covers M-2 on the real
// service: the refused start's placeholder is observed, Service.abort rolls
// back to the finished session, and that session must not notify again.
func TestRefusedStartRollbackDoesNotRepeatTheResult(t *testing.T) {
	source := &notificationServiceSource{controller: &notificationServiceController{
		items: 2,
		scope: completeScanScope(),
	}}
	r := newNotificationServiceRig(t, source)
	out, err := r.svc.Start(app.StartRequest{ID: "first"})
	if err != nil {
		t.Fatal(err)
	}
	first := r.waitNotifications(2)
	if len(first) != 2 {
		t.Fatalf("first scan notifications = %d, want 2", len(first))
	}

	source.refuseNext(60 * time.Millisecond)
	done := make(chan error, 1)
	go func() { _, err := r.svc.Start(app.StartRequest{ID: "refused"}); done <- err }()
	// Drain while the refused start's placeholder is published, so the
	// model observes the other session before the rollback.
	startErr := error(nil)
	startDone := false
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && !startDone {
		drainAll(t, r.m, r.q)
		select {
		case startErr = <-done:
			startDone = true
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if !startDone {
		t.Fatal("timed out waiting for the refused start")
	}
	if startErr == nil {
		t.Fatal("the refused start succeeded")
	}
	r.until("the rollback to the first session", func() bool {
		return r.m.appliedSession == out.SessionID && !r.m.snap.Busy
	})
	time.Sleep(30 * time.Millisecond)
	drainAll(t, r.m, r.q)
	if got := *r.got; len(got) != 2 {
		t.Fatalf("notifications after the rollback = %d, want 2: %+v", len(got), got)
	}
	if last := (*r.got)[len(*r.got)-1]; last.Segment != NotificationResult ||
		last.ID != NotificationID(string(out.SessionID), NotificationResult) {
		t.Fatalf("unexpected last notification: %+v", last)
	}
}
