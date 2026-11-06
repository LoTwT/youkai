package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/LoTwT/youkai/internal/scan"
)

// fifoUpdater is the entry point's UI thread in these tests: posted tasks
// run in FIFO order and the test stops stepping once the exit settled.
type fifoUpdater struct {
	mu sync.Mutex
	q  []func()
}

func (u *fifoUpdater) Update(fn func()) {
	u.mu.Lock()
	u.q = append(u.q, fn)
	u.mu.Unlock()
}

func (u *fifoUpdater) step() bool {
	u.mu.Lock()
	if len(u.q) == 0 {
		u.mu.Unlock()
		return false
	}
	fn := u.q[0]
	u.q = u.q[1:]
	u.mu.Unlock()
	fn()
	return true
}

// exitController keeps admitting items until the session stops, so a quit
// can happen while the scan is in progress.
type exitController struct {
	admitted atomic.Int64
}

func (c *exitController) Prepare(context.Context, *scan.ControllerHandle) error { return nil }

func (c *exitController) Collect(ctx context.Context, h *scan.ControllerHandle) error {
	for i := 0; ; i++ {
		slot := h.Acquire(ctx)
		if slot == nil {
			return ctx.Err()
		}
		observation := app.Observation{Lines: []app.LineCrop{{
			Role:   "name",
			Source: fmt.Sprintf("exit-%d", i),
			Image:  image.NewRGBA(image.Rect(0, 0, 4, 4)),
		}}}
		if _, err := slot.Admit(observation); err != nil {
			slot.Discard()
			return err
		}
		c.admitted.Add(1)
		time.Sleep(5 * time.Millisecond)
	}
}

type exitSource struct{ controller *exitController }

func (s exitSource) NewScan() (app.ScanSpec, error) {
	return app.ScanSpec{
		Controller: s.controller,
		Game:       "zzz",
		Entity:     "driveDisc",
		Client:     record.ClientWindowsLocal,
		Locale:     "zh-CN",
		Scope:      json.RawMessage(`{"kind":"test"}`),
	}, nil
}

// slowLoadService delays the reads the model starts; the real service and
// its record store remain underneath.
type slowLoadService struct {
	*app.Service
	delay time.Duration
}

func (s *slowLoadService) LoadRecord(id string) (*record.Recovery, error) {
	time.Sleep(s.delay)
	return s.Service.LoadRecord(id)
}

func resultNotifications(got []Notification) []Notification {
	var out []Notification
	for _, n := range got {
		if n.Segment == NotificationResult {
			out = append(out, n)
		}
	}
	return out
}

// TestExitFlushesPendingResult covers N-1: a quit while the result record
// is still being read must still send exactly one result notification
// before the entry point stops the UI loop in OnExitSettled.
func TestExitFlushesPendingResult(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := t.TempDir()
	paths := app.Paths{Data: filepath.Join(root, "data"), Resources: filepath.Join(root, "res")}
	lock, err := app.AcquireDataRootLock(paths.Data)
	if err != nil {
		t.Fatal(err)
	}
	controller := &exitController{}
	svc, err := app.NewService(app.Config{
		Paths:    paths,
		DataLock: lock,
		Source:   exitSource{controller: controller},
		NewRecognizer: func() (app.Recognizer, error) {
			return notificationServiceRecognizer{}, nil
		},
		ShutdownOCR: func() error { return nil },
	})
	if err != nil {
		lock.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Quit() })

	delayed := &slowLoadService{Service: svc, delay: 120 * time.Millisecond}
	var got []Notification
	settled := make(chan struct{})
	var settleOnce sync.Once
	m := New(Config{
		Service:       delayed,
		ScanSource:    availableSource(),
		Notify:        func(n Notification) { got = append(got, n) },
		OnExitSettled: func() { settleOnce.Do(func() { close(settled) }) },
	})
	u := &fifoUpdater{}
	m.Attach(u)
	sub := svc.Subscribe()
	u.Update(func() { m.Apply(sub.Initial) })
	go func() {
		for range sub.Changes {
			snap := svc.Snapshot()
			u.Update(func() { m.Apply(snap) })
		}
	}()
	if _, err := svc.Start(app.StartRequest{ID: "exit"}); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(15 * time.Second)
	for !(m.snap.Scan.InputPermitted && controller.admitted.Load() >= 5) {
		if time.Now().After(deadline) {
			t.Fatal("the scan did not start")
		}
		if !u.step() {
			time.Sleep(time.Millisecond)
		}
	}
	u.Update(func() { m.AllowClose() })

	for {
		select {
		case <-settled:
		case <-time.After(time.Until(deadline)):
			t.Fatal("the exit did not settle")
		default:
			if !u.step() {
				time.Sleep(200 * time.Microsecond)
			}
			continue
		}
		break
	}

	results := resultNotifications(got)
	if len(results) != 1 {
		t.Fatalf("result notifications when the UI loop stopped = %d, want 1: %+v", len(results), got)
	}
	// The late settle update may still be queued; running it after the exit
	// must not produce a second result.
	m.Idle()
	for u.step() {
	}
	if after := resultNotifications(got); len(after) != 1 {
		t.Fatalf("the result repeated after the exit: %+v", after)
	}
}

// TestExitBoundsAStalledResultRead covers N-1's upper bound: a read that
// never returns is abandoned after resultNotificationExitWait and the
// conservative wording is sent once before the exit settles.
func TestExitBoundsAStalledResultRead(t *testing.T) {
	gate := make(chan struct{})
	var mu sync.Mutex
	block := false
	recordRead := notificationRecord(failedItems(2), record.CoverageComplete, completePositions())
	svc := &fakeService{loadFn: func(string) (*record.Recovery, error) {
		mu.Lock()
		held := block
		mu.Unlock()
		if held {
			<-gate
		}
		return recordRead, nil
	}}
	var got []Notification
	settled := make(chan struct{})
	var settleOnce sync.Once
	m := New(Config{
		Service:       svc,
		ScanSource:    availableSource(),
		Notify:        func(n Notification) { got = append(got, n) },
		OnExitSettled: func() { settleOnce.Do(func() { close(settled) }) },
	})
	u := &fifoUpdater{}
	m.Attach(u)
	m.Apply(openCapturing("s1", 1, 2))
	for i := 0; i < 4; i++ {
		m.Idle()
		for u.step() {
		}
	}

	mu.Lock()
	block = true
	mu.Unlock()
	m.Apply(stoppingSession("s1", 2, 2))
	m.Apply(endedSession("s1", 3, 2, 2))
	u.Update(func() { m.AllowClose() })

	start := time.Now()
	deadline := start.Add(5 * time.Second)
	for {
		select {
		case <-settled:
		case <-time.After(time.Until(deadline)):
			t.Fatal("the exit did not settle")
		default:
			if !u.step() {
				time.Sleep(200 * time.Microsecond)
			}
			continue
		}
		break
	}
	elapsed := time.Since(start)
	if elapsed < resultNotificationExitWait/2 {
		t.Fatalf("the exit did not wait for the read: %v", elapsed)
	}
	if elapsed > 3*resultNotificationExitWait {
		t.Fatalf("the exit waited too long: %v", elapsed)
	}
	results := resultNotifications(got)
	if len(results) != 1 {
		t.Fatalf("timed-out result notifications = %d, want 1: %+v", len(results), got)
	}
	if results[0].Title != "扫描完成" || strings.Contains(results[0].Body, "/") {
		t.Fatalf("timed-out result is not the conservative wording: %+v", results[0])
	}

	close(gate)
	m.Idle()
	for u.step() {
	}
	if after := resultNotifications(got); len(after) != 1 {
		t.Fatalf("the timed-out result repeated: %+v", after)
	}
}
