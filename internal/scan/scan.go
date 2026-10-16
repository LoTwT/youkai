// Package scan implements the game-independent session kernel of a scan.
//
// The kernel owns the lifecycle of one session — preparing, capturing,
// finalizing, ended — and the machinery between the injected collection
// controller and the injected recognition workers:
//
//   - an input permission that starts closed, opens only for the
//     capturing phase and, once closed by a stop, never reopens;
//   - a bounded pipeline: the controller acquires a processing slot
//     before selecting the next item, waits while the pipeline is full and
//     still observes cancellation while waiting;
//   - admission that assigns session-monotonic capture sequence numbers
//     and pending records, with unadmitted attempts consuming no number;
//   - five-state accounting (queued, running, produced, failed,
//     unprocessed) where the captured count always equals the sum of the
//     five states and every sequence number terminates exactly once —
//     duplicates, stale-session callbacks and outcomes after sealing are
//     rejected;
//   - a stop path that first closes input permission, marks queued
//     entries unprocessed, requests in-flight recognition to terminate
//     and truly waits for the producer and the workers to return;
//   - authoritative snapshots with a session identity and an increasing
//     revision, plus coalescible change notifications that never block on
//     slow subscribers.
//
// The kernel carries opaque observation and result payloads without
// inspecting them; it performs no recognition, persistence or UI work and
// writes nothing to disk. Its state critical sections do only fast
// bookkeeping — no screenshots, inference, disk writes or UI calls. The
// first stop trigger and its phase are kept even when later faults follow,
// so a user cancel cannot mask them. Start-request idempotency and
// cross-process occupation live in the application service, not here.
package scan

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Config configures one session. All fields except Workers are required.
type Config struct {
	// ID identifies the session within the process. The application
	// service generates it when accepting a start request; it must be
	// non-empty.
	ID ID
	// Capacity is the bound of the processing pipeline: the number of
	// observations that may be admitted but not yet terminal (queued plus
	// running) at any time. The controller waits in Acquire when the
	// pipeline is full, so no task is dropped and nothing is cached
	// without bound.
	Capacity int
	// Workers is the number of recognition worker goroutines. Zero
	// defaults to one; the first version uses a single worker.
	Workers int
	// Controller drives the game side of the session.
	Controller Controller
	// Recognizer processes admitted observations.
	Recognizer Recognizer
	// Deliverer receives terminal records for persistence.
	Deliverer Deliverer
}

// Session is one scan session kernel. It is safe for concurrent use by the
// owner, the UI (snapshots and subscriptions) and the stop entries.
type Session struct {
	cfg     Config
	id      ID
	workers int

	// mu guards every field below. It is held only for fast bookkeeping;
	// recognition, delivery and any other slow work happen without it.
	mu sync.Mutex
	// phase is the current lifecycle phase.
	phase Phase
	// permit is the input permission: open only during capturing, and
	// once closed never reopened.
	permit bool
	// stopping is set when a trigger cut the work (user stop or fault);
	// normal completion leaves it false and goes through draining.
	stopping bool
	// firstTrigger is the first accepted stop trigger, kept even when
	// later faults follow.
	firstTrigger *Trigger
	// faults are faults recorded after the outcome was decided.
	faults []Fault
	// end is the end kind, set at sealing.
	end EndKind
	// sealed rejects late outcomes; it becomes true together with Ended.
	sealed bool
	// revision increases on every state change.
	revision uint64
	// entries is the pending-or-terminal record of every admitted
	// sequence number.
	entries map[Seq]*entry
	// nextSeq is the sequence number the next admission will take.
	nextSeq Seq
	counts  Counts
	// pendingDelivery are terminal records (the unprocessed marks of a
	// stop) that the finalizer delivers after the workers returned.
	pendingDelivery []Record
	// finalizerStarted keeps exactly one finalizer goroutine per session.
	finalizerStarted bool
	// subs are the active change subscriptions.
	subs []*Subscription

	// scanCtx is the capturing context: cancelled by a stop to terminate
	// the producer and in-flight recognition. Finalization does not
	// inherit it — the finalizer runs on its own, uncancelled lifecycle.
	scanCtx    context.Context
	cancelScan context.CancelFunc
	// taskCh is the bounded task queue. Producers send under mu (never
	// blocking, because capacity bounds outstanding tasks); workers
	// receive; the finalizer closes it once the producer returned.
	taskCh chan Task
	// capCh is the capacity semaphore, filled with Capacity tokens at
	// start. Acquire takes a token, a terminal transition or Discard
	// returns it.
	capCh chan struct{}
	// deliverMu serializes Deliverer calls so implementations can be
	// single-writer. It is never held together with mu.
	deliverMu sync.Mutex

	// producerDone closes when the producer goroutine returned.
	producerDone chan struct{}
	// workersWG tracks the recognition workers; empty until capturing.
	workersWG sync.WaitGroup
	// done closes once the session sealed and reached Ended.
	done chan struct{}
}

// Start validates cfg and starts one session in the preparing phase. The
// producer goroutine begins running the controller's Prepare immediately;
// the returned session is ready for snapshots, subscriptions and Cancel
// right away.
func Start(cfg Config) (*Session, error) {
	if cfg.ID == "" {
		return nil, errors.New("scan: start: id is empty")
	}
	if cfg.Capacity < 1 {
		return nil, fmt.Errorf("scan: start: capacity %d must be at least 1", cfg.Capacity)
	}
	if cfg.Workers < 0 {
		return nil, fmt.Errorf("scan: start: workers %d must not be negative", cfg.Workers)
	}
	if cfg.Controller == nil {
		return nil, errors.New("scan: start: controller is nil")
	}
	if cfg.Recognizer == nil {
		return nil, errors.New("scan: start: recognizer is nil")
	}
	if cfg.Deliverer == nil {
		return nil, errors.New("scan: start: deliverer is nil")
	}
	workers := cfg.Workers
	if workers == 0 {
		workers = 1
	}
	s := &Session{
		cfg:          cfg,
		id:           cfg.ID,
		workers:      workers,
		phase:        PhasePreparing,
		revision:     1,
		entries:      make(map[Seq]*entry),
		nextSeq:      1,
		taskCh:       make(chan Task, cfg.Capacity),
		capCh:        make(chan struct{}, cfg.Capacity),
		producerDone: make(chan struct{}),
		done:         make(chan struct{}),
	}
	s.scanCtx, s.cancelScan = context.WithCancel(context.Background())
	for i := 0; i < cfg.Capacity; i++ {
		s.capCh <- struct{}{}
	}
	go s.runProducer()
	return s, nil
}

// ID returns the session identity.
func (s *Session) ID() ID { return s.id }

// Snapshot returns the authoritative state of the session at its current
// revision. It never blocks on recognition, delivery or subscribers.
func (s *Session) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshotLocked()
}

// Done returns a channel that closes once the session sealed and reached
// the ended phase. Waiting on it is how the owner observes that the
// producer and all workers have actually returned.
func (s *Session) Done() <-chan struct{} { return s.done }

// Subscription is one registered change listener of a session. Initial is
// the snapshot at subscription time; Changes delivers coalesced
// notifications from then on. Close removes the listener; reading Changes
// after Close yields the buffered remainder and then the closed channel.
type Subscription struct {
	Initial Snapshot
	Changes <-chan Notification

	s      *Session
	ch     chan Notification
	closed bool
}

// Subscribe registers a change listener and returns the authoritative
// snapshot at subscription time. Notifications only mean "something
// changed": the subscriber re-fetches a snapshot, matches SessionID and
// discards revisions it has already seen. The kernel never blocks on a
// slow subscriber — buffered notifications coalesce.
func (s *Session) Subscribe() *Subscription {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch := make(chan Notification, 1)
	sub := &Subscription{
		Initial: s.snapshotLocked(),
		Changes: ch,
		s:       s,
		ch:      ch,
	}
	s.subs = append(s.subs, sub)
	return sub
}

// Close removes the subscription. It is safe to call more than once.
func (sub *Subscription) Close() {
	s := sub.s
	s.mu.Lock()
	defer s.mu.Unlock()
	if sub.closed {
		return
	}
	sub.closed = true
	for i, cur := range s.subs {
		if cur == sub {
			s.subs = append(s.subs[:i], s.subs[i+1:]...)
			break
		}
	}
	close(sub.ch)
}

// Cancel requests a user stop. It is idempotent, safe to call from any
// goroutine, and returns after the fast part only: the trigger is
// recorded, input permission is closed, queued entries are marked
// unprocessed and the scan context is cancelled. The remaining work —
// waiting for the producer, the in-flight recognition and the deliveries —
// happens on the finalizer; watch Done for it. Once the session is
// finalizing, a user stop is absorbed: it changes nothing, because the
// outcome is already decided and draining must not be cancelled.
func (s *Session) Cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping || s.phase == PhaseFinalizing || s.phase == PhaseEnded {
		return
	}
	s.beginStopLocked(Trigger{
		Kind:  TriggerUser,
		Code:  CodeUserCancelled,
		Phase: s.phase,
		At:    time.Now(),
	})
}

// runProducer runs the controller on the session's producer goroutine:
// Prepare, then Collect once preparation passed, then either the normal
// finalization (range complete) or nothing (a stop already started the
// finalizer).
func (s *Session) runProducer() {
	defer close(s.producerDone)
	h := &ControllerHandle{s: s}
	if err := s.cfg.Controller.Prepare(s.scanCtx, h); err != nil {
		// A cancelled context after a stop is the controller unwinding,
		// not a new fault; anything else is recorded.
		s.controllerFault(PhasePreparing, err)
		return
	}
	if !s.enterCapturing() {
		return // a stop was accepted while preparing
	}
	if err := s.cfg.Controller.Collect(s.scanCtx, h); err != nil {
		s.controllerFault(PhaseCapturing, err)
		return
	}
	s.rangeCompleted()
}

// controllerFault records a controller error from phase where, or absorbs
// it when it is just the unwind of an already-triggered stop.
func (s *Session) controllerFault(where Phase, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping && errors.Is(err, context.Canceled) {
		return
	}
	s.faultLocked(CodeControllerFailed, err, where, true)
}

// enterCapturing moves the session from preparing to capturing: input
// permission opens for the first and only time, and the recognition
// workers start. It reports false when the session is already stopping.
func (s *Session) enterCapturing() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping || s.phase != PhasePreparing {
		return false
	}
	s.phase = PhaseCapturing
	s.permit = true
	s.workersWG.Add(s.workers)
	for i := 0; i < s.workers; i++ {
		go s.runWorker()
	}
	s.notifyLocked()
	return true
}

// runWorker is one recognition worker. It pulls tasks until the queue is
// closed and drained (normal completion) or the scan context is cancelled
// (stop). Tasks whose entry is already terminal — strays from the stop
// race — are discarded without recognition.
func (s *Session) runWorker() {
	defer s.workersWG.Done()
	for {
		select {
		case task, ok := <-s.taskCh:
			if !ok {
				return
			}
			if !s.beginRun(task.Seq) {
				continue
			}
			err := s.cfg.Recognizer.Recognize(s.scanCtx, task, s.submitOutcome)
			s.finishRun(task.Seq, err)
		case <-s.scanCtx.Done():
			return
		}
	}
}

// rangeCompleted is the normal completion path: the controller reported
// the range complete. The producer goroutine itself becomes the finalizer
// and drains the queue instead of cutting it.
func (s *Session) rangeCompleted() {
	s.mu.Lock()
	if s.stopping || s.phase != PhaseCapturing {
		s.mu.Unlock()
		return
	}
	s.permit = false
	s.phase = PhaseFinalizing
	s.finalizerStarted = true
	s.notifyLocked()
	s.mu.Unlock()
	s.finalize(true)
}

// finalize drains or terminates the remaining work and seals the session.
// It runs exactly once per session, either on the producer goroutine
// (normal completion, fromProducer) or on its own goroutine started by a
// stop. It deliberately uses no context: finalization must not inherit the
// cancelled scan context, and a stop never cancels saving.
func (s *Session) finalize(fromProducer bool) {
	if !fromProducer {
		// The stop path: wait until the producer has really returned.
		<-s.producerDone
	}
	// The producer has returned, so no task can be sent anymore.
	close(s.taskCh)
	// Wait for the workers: in-flight recognition returns (terminated by
	// the cancelled context after a stop, or finished normally during a
	// drain), and every worker delivers its terminal record before
	// exiting.
	s.workersWG.Wait()
	// Recognition is over; release the scan context.
	s.cancelScan()
	for _, rec := range s.takePendingDelivery() {
		s.deliver(rec)
	}
	s.seal()
}

// seal ends the session: sealed, ended, notified, done. The end kind
// follows the first trigger when there was one and completion otherwise.
func (s *Session) seal() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sealed {
		return
	}
	s.sealed = true
	s.phase = PhaseEnded
	s.permit = false
	s.end = EndCompleted
	if s.firstTrigger != nil {
		switch s.firstTrigger.Kind {
		case TriggerUser:
			s.end = EndCancelled
		case TriggerFault:
			s.end = EndFailed
		}
	}
	s.notifyLocked()
	close(s.done)
}
