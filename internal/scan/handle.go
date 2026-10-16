package scan

import (
	"context"
	"errors"
	"fmt"
)

// ControllerHandle is the kernel side of the controller contract: the
// query and admission points the collection loop uses. The kernel hands
// one handle to the controller's Prepare and Collect calls; its methods
// must be called only from the controller's own goroutine (the producer),
// because admission and capacity belong to that single producer.
type ControllerHandle struct {
	s *Session
}

// InputPermitted reports whether the collection side may dispatch short
// game operations. It is false during preparing, opens exactly when
// capturing begins and, once a stop closed it, never reopens. The
// controller checks it before every short operation.
func (h *ControllerHandle) InputPermitted() bool {
	h.s.mu.Lock()
	defer h.s.mu.Unlock()
	return h.s.permit
}

// Acquire waits for one processing slot of the bounded pipeline. The
// controller calls it before selecting the next item, so a full pipeline
// stops further game input instead of dropping tasks or caching without
// bound. It returns nil once ctx or the session's scan context is done —
// the controller must not capture further observations then — and the
// returned slot must be consumed by exactly one Admit or Discard.
func (h *ControllerHandle) Acquire(ctx context.Context) *Slot {
	s := h.s
	select {
	case <-s.capCh:
		return &Slot{s: s}
	case <-ctx.Done():
		return nil
	case <-s.scanCtx.Done():
		return nil
	}
}

// Fail reports a game-side fault that stops the session, with the phase
// the controller is in. Wrap the cause in a FaultError to carry a
// diagnostic code (focusLost, geometryChanged, …); a plain error gets
// controllerFailed. Faults reported after the session's outcome is already
// decided are recorded as additional faults and change nothing.
func (h *ControllerHandle) Fail(err error) {
	if err == nil {
		return
	}
	h.s.mu.Lock()
	defer h.s.mu.Unlock()
	h.s.faultLocked(CodeControllerFailed, err, h.s.phase, true)
}

// Slot is one acquired processing slot of the bounded pipeline, held
// between Acquire and the admission decision for the next item. Exactly
// one of Admit or Discard consumes it.
type Slot struct {
	s *Session
	// used is guarded by s.mu and makes the slot single-use.
	used bool
}

// Admit consumes the slot and admits one observation: the kernel assigns
// it the next session-monotonic sequence number, creates its pending
// record (state queued) and queues the task for the recognition workers.
// Attempts that are not admitted never consume a sequence number. Admit
// fails once the session stopped or sealed; the controller then discards
// the slot and returns.
//
// When the session has an OnAdmit hook, it runs here — after the state
// lock is released and before the task is queued — so the persistence
// layer records the observation (its evidence and its queued item)
// before any terminal outcome for it can exist. A hook error is a
// storage fault that cuts collection; the task is still queued below,
// and the stop the fault triggers drains or discards it like any stray.
func (c *Slot) Admit(observation any) (Seq, error) {
	s := c.s
	s.mu.Lock()
	if c.used {
		s.mu.Unlock()
		return 0, errors.New("scan: slot already used")
	}
	if s.stopping || s.sealed || s.phase != PhaseCapturing {
		phase := s.phase
		s.mu.Unlock()
		return 0, fmt.Errorf("scan: session %s is not admitting in phase %s", s.id, phase)
	}
	c.used = true
	seq := s.nextSeq
	s.nextSeq++
	s.entries[seq] = &entry{state: StateQueued, obs: observation}
	s.counts.Queued++
	s.counts.Captured++
	s.notifyLocked()
	s.mu.Unlock()
	// The admission hook runs outside the state lock, before the queue
	// receives the task: the persistence layer's record of the admission
	// strictly precedes any terminal outcome. It happens on this
	// goroutine — the controller's producer — by design: admission
	// bookkeeping belongs to the admission step, and its cost (evidence
	// writes and a batched checkpoint at most) is part of pacing the
	// collection loop.
	if s.cfg.OnAdmit != nil {
		if err := s.cfg.OnAdmit(seq, observation); err != nil {
			s.mu.Lock()
			s.faultLocked(CodeStorageFailure, err, s.phase, false)
			s.mu.Unlock()
		}
	}
	// The queue holds at most the outstanding tasks, which the capacity
	// bounds, so this buffered send never blocks in a well-behaved
	// session. It happens outside the lock on purpose: even a contract
	// violation that piled stale tasks into the queue could then only
	// delay this send, never deadlock the kernel — workers keep draining
	// and discard strays via beginRun. A stop accepted in between marks
	// the entry terminal first, and this stray task is discarded the same
	// way.
	s.taskCh <- Task{SessionID: s.id, Seq: seq, Observation: observation}
	return seq, nil
}

// Discard consumes the slot without admitting: the attempt was not
// accepted as an observation (untrusted detail, a reference screenshot,
// a non-target item) and therefore consumes no sequence number.
func (c *Slot) Discard() {
	s := c.s
	s.mu.Lock()
	defer s.mu.Unlock()
	if c.used {
		return
	}
	c.used = true
	s.releaseSlotLocked()
}
