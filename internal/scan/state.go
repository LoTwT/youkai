package scan

import (
	"errors"
	"fmt"
	"sort"
	"time"
)

// entry is the pending or terminal record of one admitted sequence
// number, guarded by Session.mu.
type entry struct {
	state  State
	obs    any
	result any
	reason string
	cause  UnprocessedCause
}

// faultLocked records a fault and, unless the outcome is already decided,
// cuts the session's active work as the first stop trigger. cutsFinalizing
// says whether the fault also cuts a normal drain in the finalizing phase:
// a recognizer runtime failure does (the remaining queue cannot run), a
// delivery failure does not (saving state is the deliverer's concern and
// must not cancel remaining recognition).
func (s *Session) faultLocked(defaultCode string, err error, where Phase, cutsFinalizing bool) {
	code := faultCode(err, defaultCode)
	if !s.stopping && (s.phase != PhaseFinalizing || cutsFinalizing) {
		s.beginStopLocked(Trigger{
			Kind:    TriggerFault,
			Code:    code,
			Message: err.Error(),
			Phase:   where,
			At:      time.Now(),
		})
		return
	}
	s.faults = append(s.faults, Fault{Code: code, Message: err.Error(), Phase: where, At: time.Now()})
	s.notifyLocked()
}

// beginStopLocked is the single stop entry: it accepts the first trigger,
// closes input permission, moves to finalizing, cancels the scan context
// (the terminate request for the producer and in-flight recognition),
// marks every queued entry unprocessed with the trigger's cause, drains
// stray queued tasks and starts the finalizer. Callers must verify that
// the trigger is admissible (not already stopping, and user stops are
// absorbed during finalizing).
func (s *Session) beginStopLocked(t Trigger) {
	if s.stopping || s.phase == PhaseEnded {
		return
	}
	s.stopping = true
	s.firstTrigger = &t
	s.permit = false
	s.phase = PhaseFinalizing
	s.cancelScan()
	cause := CauseInterrupted
	if t.Kind == TriggerUser {
		cause = CauseCancelled
	}
	for seq, e := range s.entries {
		if e.state != StateQueued {
			continue
		}
		e.state = StateUnprocessed
		e.cause = cause
		e.reason = t.Code
		s.counts.Queued--
		s.counts.Unprocessed++
		s.releaseSlotLocked()
		s.pendingDelivery = append(s.pendingDelivery, s.recordLocked(seq, e))
	}
	// Discard tasks still sitting in the queue; their entries are terminal
	// now, so workers that race onto them discard them via beginRun.
	drained := false
	for !drained {
		select {
		case _, ok := <-s.taskCh:
			if !ok {
				drained = true
			}
		default:
			drained = true
		}
	}
	if !s.finalizerStarted {
		s.finalizerStarted = true
		go s.finalize(false)
	}
	s.notifyLocked()
}

// beginRun moves a queued entry to running. It reports false when the
// entry is already terminal — a stray task from the stop race — so the
// worker discards it without recognition.
func (s *Session) beginRun(seq Seq) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[seq]
	if !ok || e.state != StateQueued {
		return false
	}
	e.state = StateRunning
	s.counts.Queued--
	s.counts.Running++
	s.notifyLocked()
	return true
}

// finishRun resolves a task whose Recognize call returned: if the
// recognizer submitted no outcome, the entry ends unprocessed when the
// session is stopping (cancelled by a user stop or interrupted by the
// fault that is being recorded here) and failed otherwise. A returned
// runtime error is recorded as a fault on top. The goroutine that resolves
// the entry delivers its record right after, outside the lock.
func (s *Session) finishRun(seq Seq, recErr error) {
	var rec Record
	var mustDeliver bool
	s.mu.Lock()
	if recErr != nil {
		s.faultLocked(CodeRuntimeFailure, recErr, s.phase, true)
	}
	if e, ok := s.entries[seq]; ok && e.state == StateRunning {
		if s.stopping {
			e.state = StateUnprocessed
			e.cause = CauseInterrupted
			if s.firstTrigger != nil && s.firstTrigger.Kind == TriggerUser {
				e.cause = CauseCancelled
			}
			if recErr != nil {
				e.reason = recErr.Error()
			} else if s.firstTrigger != nil {
				e.reason = s.firstTrigger.Code
			}
		} else {
			e.state = StateFailed
			e.reason = "recognizer returned without an outcome"
		}
		s.counts.Running--
		s.countStateLocked(e.state)
		s.releaseSlotLocked()
		rec = s.recordLocked(seq, e)
		mustDeliver = true
		s.notifyLocked()
	}
	s.mu.Unlock()
	if mustDeliver {
		s.deliver(rec)
	}
}

// submitOutcome is the Submit closure handed to the recognizer. The
// goroutine whose submission is accepted delivers the record right after,
// outside the lock.
func (s *Session) submitOutcome(sessionID ID, seq Seq, recognition Recognition) error {
	var rec Record
	var accepted bool
	var err error
	s.mu.Lock()
	rec, accepted, err = s.submitLocked(sessionID, seq, recognition)
	s.mu.Unlock()
	if accepted {
		s.deliver(rec)
	}
	return err
}

// submitLocked applies the acceptance rules to one outcome: same session,
// not sealed, known sequence, not terminal yet, and a well-formed
// recognition. The first outcome wins; everything else is rejected without
// changing anything.
func (s *Session) submitLocked(sessionID ID, seq Seq, recognition Recognition) (Record, bool, error) {
	if sessionID != s.id {
		return Record{}, false, fmt.Errorf("scan: outcome for session %q does not belong to session %q", sessionID, s.id)
	}
	if s.sealed {
		// The terminal-entry check below also rejects this in today's
		// kernel: the stop path marks every queued/running entry terminal
		// before sealing. Keep the explicit gate as the contract's first
		// line and as protection if the entry lifecycle changes.
		return Record{}, false, errors.New("scan: session sealed; late outcome rejected")
	}
	e, ok := s.entries[seq]
	if !ok {
		return Record{}, false, fmt.Errorf("scan: sequence %d was never admitted", seq)
	}
	old := e.state
	if old.IsTerminal() {
		return Record{}, false, fmt.Errorf("scan: sequence %d is already %s; duplicate outcome rejected", seq, old)
	}
	switch {
	case recognition.Failed != nil:
		e.state = StateFailed
		e.reason = recognition.Failed.Error()
	case recognition.Result != nil:
		e.state = StateProduced
		e.result = recognition.Result
	default:
		return Record{}, false, errors.New("scan: outcome carries neither a result nor a failure")
	}
	if old == StateRunning {
		s.counts.Running--
	} else {
		s.counts.Queued--
	}
	s.countStateLocked(e.state)
	s.releaseSlotLocked()
	s.notifyLocked()
	return s.recordLocked(seq, e), true, nil
}

// countStateLocked increments the counter of terminal state st. Callers
// have already decremented the counter of the previous state.
func (s *Session) countStateLocked(st State) {
	switch st {
	case StateProduced:
		s.counts.Produced++
	case StateFailed:
		s.counts.Failed++
	case StateUnprocessed:
		s.counts.Unprocessed++
	}
}

// releaseSlotLocked returns one processing slot to the capacity semaphore.
// A slot is only ever returned while it is held, so the buffered send can
// never block: held slots are missing from the channel by construction.
// The non-blocking send is a fail-safe: even a violated invariant could
// only shrink the capacity, never over-admit.
func (s *Session) releaseSlotLocked() {
	select {
	case s.capCh <- struct{}{}:
	default:
	}
}

// recordLocked builds the deliverable record of entry e.
func (s *Session) recordLocked(seq Seq, e *entry) Record {
	return Record{
		SessionID:   s.id,
		Seq:         seq,
		State:       e.state,
		Result:      e.result,
		Reason:      e.reason,
		Cause:       e.cause,
		Observation: e.obs,
	}
}

// takePendingDelivery hands the stop path's unprocessed marks to the
// finalizer, in sequence order, and clears them.
func (s *Session) takePendingDelivery() []Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	pending := s.pendingDelivery
	s.pendingDelivery = nil
	sort.Slice(pending, func(i, j int) bool { return pending[i].Seq < pending[j].Seq })
	return pending
}

// deliver hands one terminal record to the injected deliverer, serialized
// with all other deliveries and outside the state lock. A delivery error
// stops collection while capturing and is recorded as an additional fault
// once the session is already stopping; it never changes the record.
func (s *Session) deliver(rec Record) {
	s.deliverMu.Lock()
	err := s.cfg.Deliverer.Deliver(rec)
	s.deliverMu.Unlock()
	if err != nil {
		s.mu.Lock()
		s.faultLocked(CodeStorageFailure, err, s.phase, false)
		s.mu.Unlock()
	}
}

// notifyLocked bumps the revision and notifies every subscriber without
// blocking: notifications coalesce, so a full subscriber buffer means the
// subscriber will re-fetch a newer snapshot anyway.
func (s *Session) notifyLocked() {
	s.revision++
	n := Notification{SessionID: s.id, Revision: s.revision}
	for _, sub := range s.subs {
		select {
		case sub.ch <- n:
		default:
		}
	}
}

// snapshotLocked copies the authoritative state of the session.
func (s *Session) snapshotLocked() Snapshot {
	snap := Snapshot{
		SessionID:      s.id,
		Revision:       s.revision,
		Phase:          s.phase,
		End:            s.end,
		InputPermitted: s.permit,
		Counts:         s.counts,
	}
	if s.firstTrigger != nil {
		t := *s.firstTrigger
		snap.Trigger = &t
	}
	if len(s.faults) > 0 {
		snap.Faults = append([]Fault(nil), s.faults...)
	}
	return snap
}
