package scan

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"
)

// These tests pin the payload-retention side of the terminal delivery
// contract. The kernel must hand the observation and result to the
// deliverer exactly once and must not keep one processed payload per
// entry afterward: a real observation carries a full set of image crops,
// and a session's entry table lives as long as the service's active
// session. The entry table is internal, so the assertions are white-box.

// retentionPayload stands in for the heavy pixels of one admitted
// observation.
type retentionPayload struct {
	Seq Seq
}

// retentionController admits the given payloads in order. By default it
// then ends the collection normally; hold keeps it inside Collect until
// a stop cancels the scan context, so the stop path resolves the
// remaining entries.
type retentionController struct {
	payloads []*retentionPayload
	hold     bool
}

func (*retentionController) Prepare(context.Context, *ControllerHandle) error { return nil }

func (c *retentionController) Collect(ctx context.Context, h *ControllerHandle) error {
	for _, payload := range c.payloads {
		slot := h.Acquire(ctx)
		if slot == nil {
			return ctx.Err()
		}
		if _, err := slot.Admit(payload); err != nil {
			slot.Discard()
			return err
		}
	}
	if c.hold {
		<-ctx.Done()
		return ctx.Err()
	}
	return nil
}

// retentionRecognizer delegates each recognition to recognize.
type retentionRecognizer struct {
	recognize func(ctx context.Context, task Task, submit Submit) error
}

func (r *retentionRecognizer) Recognize(ctx context.Context, task Task, submit Submit) error {
	return r.recognize(ctx, task, submit)
}

// retentionSink keeps every delivered record so the tests can check the
// handoff contract. Retaining them here is the deliverer's own choice;
// the entry table must release its references regardless.
type retentionSink struct {
	mu      sync.Mutex
	records []Record
}

func (s *retentionSink) Deliver(rec Record) error {
	s.mu.Lock()
	s.records = append(s.records, rec)
	s.mu.Unlock()
	return nil
}

func (s *retentionSink) bySeq() map[Seq]Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[Seq]Record, len(s.records))
	for _, rec := range s.records {
		out[rec.Seq] = rec
	}
	return out
}

// entryPayloadView is the payload-retention state of one entry.
type entryPayloadView struct {
	present bool
	state   State
	obs     bool
	result  bool
}

// payloadViews copies the entry table's payload-retention state for seqs.
func payloadViews(s *Session, seqs []Seq) map[Seq]entryPayloadView {
	s.mu.Lock()
	defer s.mu.Unlock()
	views := make(map[Seq]entryPayloadView, len(seqs))
	for _, seq := range seqs {
		e, ok := s.entries[seq]
		if !ok {
			continue
		}
		views[seq] = entryPayloadView{
			present: true,
			state:   e.state,
			obs:     e.obs != nil,
			result:  e.result != nil,
		}
	}
	return views
}

// requirePayloadsReleased fails when any of seqs still references a
// payload from the entry table.
func requirePayloadsReleased(t *testing.T, s *Session, seqs []Seq, why string) {
	t.Helper()
	views := payloadViews(s, seqs)
	for _, seq := range seqs {
		view, ok := views[seq]
		if !ok {
			t.Fatalf("%s: entry %d is missing", why, seq)
		}
		if view.obs || view.result {
			t.Fatalf("%s: entry %d still retains obs=%v result=%v", why, seq, view.obs, view.result)
		}
	}
}

// waitCounts polls the authoritative snapshot until want holds.
func waitCounts(t *testing.T, s *Session, want func(Counts) bool) {
	t.Helper()
	for i := 0; i < 500_000; i++ {
		if want(s.Snapshot().Counts) {
			return
		}
		runtime.Gosched()
	}
	t.Fatalf("counts condition was never reached: %+v", s.Snapshot().Counts)
}

// TestTerminalEntriesReleasePayloads drives a normal completion of more
// items than the pipeline capacity. Every entry is terminal in the end,
// and the delivered records still carry the observations and results;
// the entries themselves must have released them, so retention does not
// grow with the number of processed items.
func TestTerminalEntriesReleasePayloads(t *testing.T) {
	const items = 64
	const gateSeq = Seq(8)

	payloads := make([]*retentionPayload, items)
	for i := range payloads {
		payloads[i] = &retentionPayload{Seq: Seq(i + 1)}
	}
	gate := make(chan struct{})
	rec := &retentionRecognizer{recognize: func(_ context.Context, task Task, submit Submit) error {
		if err := submit(task.SessionID, task.Seq, Recognition{Result: fmt.Sprintf("result-%d", task.Seq)}); err != nil {
			return err
		}
		// submit returned, so this entry is terminal and its record was
		// already delivered.
		if task.Seq == gateSeq {
			close(gate)
		}
		return nil
	}}
	sink := &retentionSink{}
	s, err := Start(Config{
		ID:         "retention-complete",
		Capacity:   4,
		Workers:    1,
		Controller: &retentionController{payloads: payloads},
		Recognizer: rec,
		Deliverer:  sink,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	select {
	case <-gate:
	case <-time.After(30 * time.Second):
		t.Fatal("recognition never reached the gate sequence")
	}
	requirePayloadsReleased(t, s, []Seq{1, 2, 3, 4, 5, 6, 7, 8}, "while later items are still in flight")

	select {
	case <-s.Done():
	case <-time.After(30 * time.Second):
		t.Fatal("session did not end")
	}

	all := make([]Seq, items)
	for i := range all {
		all[i] = Seq(i + 1)
	}
	requirePayloadsReleased(t, s, all, "after the session ended")

	records := sink.bySeq()
	if len(records) != items {
		t.Fatalf("delivered %d records, want %d", len(records), items)
	}
	for _, seq := range all {
		record, ok := records[seq]
		if !ok {
			t.Fatalf("sequence %d was never delivered", seq)
		}
		if record.State != StateProduced {
			t.Fatalf("record %d is %s, want produced", seq, record.State)
		}
		if record.Observation != payloads[seq-1] {
			t.Fatalf("record %d lost its observation payload", seq)
		}
		if record.Result != fmt.Sprintf("result-%d", seq) {
			t.Fatalf("record %d lost its result payload: %v", seq, record.Result)
		}
	}
	if snap := s.Snapshot(); snap.Counts != (Counts{Captured: items, Produced: items}) {
		t.Fatalf("counts %+v, want %d produced", snap.Counts, items)
	}
}

// TestStopDrainsTaskQueuedByLateAdmit pins the admission-versus-stop
// interleaving: the producer is inside OnAdmit when Cancel marks the entry
// unprocessed and cancels the scan context, the worker exits, and only then
// does the hook return so that Admit enqueues its task into a buffer no
// worker will ever receive from again. The finalizer must drop that buffered
// task before sealing, while the unprocessed record — the one that owns the
// observation now — is still delivered exactly once and in full.
func TestStopDrainsTaskQueuedByLateAdmit(t *testing.T) {
	payload := &retentionPayload{Seq: 1}
	hookEntered := make(chan struct{})
	releaseHook := make(chan struct{})

	sink := &retentionSink{}
	rec := &retentionRecognizer{recognize: func(context.Context, Task, Submit) error {
		t.Error("a task queued after the stop must never reach recognition")
		return nil
	}}
	s, err := Start(Config{
		ID:         "retention-late-admit",
		Capacity:   4,
		Workers:    1,
		Controller: &retentionController{payloads: []*retentionPayload{payload}},
		Recognizer: rec,
		Deliverer:  sink,
		OnAdmit: func(Seq, any) error {
			close(hookEntered)
			<-releaseHook
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	select {
	case <-hookEntered:
	case <-time.After(30 * time.Second):
		t.Fatal("admission hook was never entered")
	}

	s.Cancel()
	// Cancel marked the queued entry unprocessed and cancelled the scan
	// context. Waiting for the worker group pins "worker exited" before the
	// hook releases the admission that will enqueue the stray task.
	s.workersWG.Wait()
	close(releaseHook)

	select {
	case <-s.Done():
	case <-time.After(30 * time.Second):
		t.Fatal("session did not end")
	}

	// The closed queue must hold no leftover task: receiving from it after
	// sealing yields the zero task and ok=false only when the finalizer
	// dropped the late admission.
	if task, ok := <-s.taskCh; ok {
		t.Fatalf("task %d with observation %v survived sealing", task.Seq, task.Observation)
	}

	s.mu.Lock()
	pending := len(s.pendingDelivery)
	s.mu.Unlock()
	sink.mu.Lock()
	deliveries := append([]Record(nil), sink.records...)
	sink.mu.Unlock()
	if pending != 0 {
		t.Fatalf("%d pending deliveries survived sealing", pending)
	}
	if len(deliveries) != 1 {
		t.Fatalf("delivered %d records, want exactly one: %+v", len(deliveries), deliveries)
	}
	record := deliveries[0]
	if record.Seq != 1 || record.State != StateUnprocessed || record.Cause != CauseCancelled {
		t.Fatalf("record %+v, want the cancelled unprocessed entry", record)
	}
	if record.Observation != payload {
		t.Fatal("the unprocessed record lost its observation payload")
	}
	requirePayloadsReleased(t, s, []Seq{1}, "after the late-admission stop ended")
	if snap := s.Snapshot(); snap.Counts != (Counts{Captured: 1, Unprocessed: 1}) {
		t.Fatalf("counts %+v, want one captured unprocessed item", snap.Counts)
	}
}

// TestStopEntriesReleasePayloadsAfterHandoff stops a session with one
// produced, one failed, one running and one queued entry. Until the stop
// resolves them, the running and queued entries keep their observations —
// recognition and the stop's unprocessed record still need them — and
// after the terminal handoff every entry releases its payloads while the
// delivered records keep them.
func TestStopEntriesReleasePayloadsAfterHandoff(t *testing.T) {
	payloads := []*retentionPayload{
		{Seq: 1},
		{Seq: 2},
		{Seq: 3},
		{Seq: 4},
	}
	inFlight := make(chan struct{})
	recognized := make(chan Seq, len(payloads))
	rec := &retentionRecognizer{recognize: func(ctx context.Context, task Task, submit Submit) error {
		recognized <- task.Seq
		switch task.Seq {
		case 1:
			return submit(task.SessionID, task.Seq, Recognition{Result: "one"})
		case 2:
			return submit(task.SessionID, task.Seq, Recognition{Failed: errors.New("unreadable")})
		case 3:
			close(inFlight)
			<-ctx.Done()
			return nil // terminated: the entry ends unprocessed
		default:
			return errors.New("the queued entry must not reach recognition after the stop drain")
		}
	}}
	sink := &retentionSink{}
	s, err := Start(Config{
		ID:         "retention-stop",
		Capacity:   4,
		Workers:    1,
		Controller: &retentionController{payloads: payloads, hold: true},
		Recognizer: rec,
		Deliverer:  sink,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	select {
	case <-inFlight:
	case <-time.After(30 * time.Second):
		t.Fatal("recognition never reached the third entry")
	}
	waitCounts(t, s, func(c Counts) bool {
		return c == (Counts{Captured: 4, Queued: 1, Running: 1, Produced: 1, Failed: 1})
	})

	// The produced and failed entries handed their observations off
	// already; the running and queued ones still need theirs.
	views := payloadViews(s, []Seq{1, 2, 3, 4})
	for _, seq := range []Seq{1, 2} {
		view := views[seq]
		if view.obs || view.result {
			t.Fatalf("terminal entry %d still retains obs=%v result=%v", seq, view.obs, view.result)
		}
	}
	for _, seq := range []Seq{3, 4} {
		view := views[seq]
		if !view.obs {
			t.Fatalf("in-flight entry %d lost its observation before its terminal record", seq)
		}
	}
	if views[3].state != StateRunning || views[4].state != StateQueued {
		t.Fatalf("entry states %s/%s, want running/queued", views[3].state, views[4].state)
	}

	s.Cancel()
	select {
	case <-s.Done():
	case <-time.After(30 * time.Second):
		t.Fatal("session did not end")
	}
	requirePayloadsReleased(t, s, []Seq{1, 2, 3, 4}, "after the stopped session ended")

	snap := s.Snapshot()
	want := Counts{Captured: 4, Produced: 1, Failed: 1, Unprocessed: 2}
	if snap.Counts != want {
		t.Fatalf("counts %+v, want %+v", snap.Counts, want)
	}
	records := sink.bySeq()
	if len(records) != len(payloads) {
		t.Fatalf("delivered %d records, want %d", len(records), len(payloads))
	}
	for seq, record := range records {
		if record.Observation != payloads[seq-1] {
			t.Fatalf("record %d lost its observation payload", seq)
		}
	}
	if record := records[1]; record.State != StateProduced || record.Result != "one" {
		t.Fatalf("record 1 %+v, want produced with its result", record)
	}
	if record := records[2]; record.State != StateFailed || record.Reason != "unreadable" {
		t.Fatalf("record 2 %+v, want failed with its reason", record)
	}
	for _, seq := range []Seq{3, 4} {
		if record := records[seq]; record.State != StateUnprocessed || record.Cause != CauseCancelled {
			t.Fatalf("record %d %+v, want cancelled unprocessed", seq, record)
		}
	}
	if got := <-recognized; got != 1 {
		t.Fatalf("first recognized sequence is %d, want 1", got)
	}
	if got := <-recognized; got != 2 {
		t.Fatalf("second recognized sequence is %d, want 2", got)
	}
	if got := <-recognized; got != 3 {
		t.Fatalf("third recognized sequence is %d, want 3", got)
	}
	select {
	case got := <-recognized:
		t.Fatalf("queued sequence %d reached recognition after the stop drain", got)
	default:
	}
}
