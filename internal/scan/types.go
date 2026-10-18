package scan

import (
	"context"
	"errors"
	"time"
)

// ID identifies one scan session within the process. The application
// service generates it when it accepts a start request and hands it to
// Start; the kernel only compares identities, so outcomes and callbacks
// naming another session are rejected.
type ID string

// Seq is the session-scoped, monotonically increasing capture sequence
// number of one admitted observation. The first admitted observation of a
// session is 1; attempts that are not admitted never consume a number.
type Seq int64

// Phase is the coarse lifecycle phase of a session: preparing → capturing
// → finalizing → ended. The names match the termination phases of the scan
// JSON contract.
type Phase string

const (
	// PhasePreparing covers the precheck work the injected controller runs
	// before any input is permitted.
	PhasePreparing Phase = "preparing"
	// PhaseCapturing is the active collection loop: input is permitted and
	// observations are admitted.
	PhaseCapturing Phase = "capturing"
	// PhaseFinalizing drains or terminates the remaining work, waits for
	// the producer and the recognition workers, and delivers terminal
	// records.
	PhaseFinalizing Phase = "finalizing"
	// PhaseEnded is the sealed terminal state: every captured sequence has
	// exactly one outcome and late writes are rejected.
	PhaseEnded Phase = "ended"
)

// State is the raw processing state of one admitted observation. The first
// version of the public JSON contract serializes only the three terminal
// states; queued and running exist while the session runs.
type State string

const (
	// StateQueued: admitted, waiting for a recognition worker.
	StateQueued State = "queued"
	// StateRunning: pulled by a worker, recognition in flight.
	StateRunning State = "running"
	// StateProduced: recognition returned a payload.
	StateProduced State = "produced"
	// StateFailed: recognition ended without a payload (per-task failure).
	StateFailed State = "failed"
	// StateUnprocessed: the entry never got a chance to produce a result;
	// Cause and Reason say why.
	StateUnprocessed State = "unprocessed"
)

// IsTerminal reports whether state is one of the three terminal states.
func (st State) IsTerminal() bool {
	switch st {
	case StateProduced, StateFailed, StateUnprocessed:
		return true
	default:
		return false
	}
}

// EndKind is how a session ended. It is derived from the first stop
// trigger when there was one, and completed otherwise. The JSON contract's
// interrupted kind only exists for sessions restored after a crash, so the
// live kernel never produces it.
type EndKind string

const (
	// EndCompleted: the controller reported the range complete and every
	// captured sequence drained to a produced or failed outcome.
	EndCompleted EndKind = "completed"
	// EndCancelled: a user stop was the first trigger that cut the work.
	EndCancelled EndKind = "cancelled"
	// EndFailed: a fault was the first trigger that cut the work.
	EndFailed EndKind = "failed"
)

// TriggerKind classifies a stop trigger by who caused it.
type TriggerKind string

const (
	// TriggerUser: the owner requested a stop (stop button, hotkey,
	// restored window, quit path).
	TriggerUser TriggerKind = "user"
	// TriggerFault: a controller, recognizer, deliverer or owner fault.
	TriggerFault TriggerKind = "fault"
)

// UnprocessedCause says why a captured entry ended unprocessed: the user
// stopped the session, or a fault interrupted it.
type UnprocessedCause string

const (
	// CauseCancelled: a user stop cut the work.
	CauseCancelled UnprocessedCause = "cancelled"
	// CauseInterrupted: a fault cut the work.
	CauseInterrupted UnprocessedCause = "interrupted"
)

// Diagnostic codes of kernel-synthesized triggers and faults. They follow
// the suggestions of the scan JSON contract; controllers can carry their
// own codes (focusLost, permissionRevoked, …) by reporting FaultError
// values.
const (
	// CodeVerifiedEnd marks a normal completion: the controller verified
	// the end of the range.
	CodeVerifiedEnd = "verifiedEnd"
	// CodeUserCancelled marks a user-requested stop.
	CodeUserCancelled = "userCancelled"
	// CodeControllerFailed is the fallback code for controller errors that
	// carry no FaultError code.
	CodeControllerFailed = "controllerFailed"
	// CodeRuntimeFailure marks a recognizer failure that stops the whole
	// session (a per-task failure is not a fault).
	CodeRuntimeFailure = "runtimeFailure"
	// CodeStorageFailure marks a delivery (persistence) failure.
	CodeStorageFailure = "storageFailure"
	// CodeReleaseFailed marks a fault recorded when a controller's
	// Release (see Releaser) returned an error after the session's outcome
	// was already decided. It never changes the outcome: the resources are
	// gone either way, and the release failure is the durable diagnosis.
	CodeReleaseFailed = "releaseFailed"
)

// FaultError pairs a diagnostic code with the error that caused a session
// fault, so a controller's or recognizer's trigger code survives the trip
// through the kernel. A plain error gets a source-specific default code
// instead.
type FaultError struct {
	Code string
	Err  error
}

// NewFault wraps err with a diagnostic code for reporting to the session.
// code should be a stable, non-empty diagnostic code such as focusLost; err
// may be nil when the code alone says everything.
func NewFault(code string, err error) *FaultError {
	return &FaultError{Code: code, Err: err}
}

func (e *FaultError) Error() string {
	if e.Err == nil {
		return e.Code
	}
	return e.Code + ": " + e.Err.Error()
}

func (e *FaultError) Unwrap() error { return e.Err }

// faultCode returns err's FaultError code, or fallback when err carries
// none.
func faultCode(err error, fallback string) string {
	var fe *FaultError
	if errors.As(err, &fe) && fe.Code != "" {
		return fe.Code
	}
	return fallback
}

// Trigger is the first stop trigger accepted by a session. The kernel
// keeps it unchanged even when later faults follow, so a user cancel
// cannot mask them: they pile up in Snapshot.Faults instead.
type Trigger struct {
	Kind    TriggerKind
	Code    string
	Message string
	Phase   Phase // the phase the trigger was accepted in, not the phase at sealing
	At      time.Time
}

// Fault is a fault recorded after the session's outcome was already
// decided — either after the first trigger, or during finalizing where it
// does not cut the remaining work.
type Fault struct {
	Code    string
	Message string
	Phase   Phase
	At      time.Time
}

// Counts is the raw processing accounting of a session. Captured is the
// number of admitted observations and always equals the sum of the five
// states; the invariant holds at every instant.
type Counts struct {
	Captured    int
	Queued      int
	Running     int
	Produced    int
	Failed      int
	Unprocessed int
}

// Sum returns the sum of the five processing states.
func (c Counts) Sum() int {
	return c.Queued + c.Running + c.Produced + c.Failed + c.Unprocessed
}

// Snapshot is the authoritative state of one session at one revision. The
// UI re-fetches snapshots on notification and compares SessionID and
// Revision to decide what to render; nothing else is a source of truth.
type Snapshot struct {
	SessionID ID
	Revision  uint64
	Phase     Phase
	// End is set once Phase is Ended; until then a fault during
	// finalizing can still change how the session ends.
	End EndKind
	// InputPermitted reports whether the collection side may dispatch
	// short game operations. It is false while preparing, open only
	// during capturing, and once closed it never reopens.
	InputPermitted bool
	// Trigger is the first stop trigger, or nil when the session ran to a
	// normal completion without one.
	Trigger *Trigger
	// Faults lists faults recorded after the outcome was decided.
	Faults []Fault
	Counts Counts
}

// Notification tells a subscriber that the session changed. It carries no
// state: the subscriber re-fetches a snapshot. Notifications are coalesced
// — a slow subscriber misses intermediate ones and still sees the latest
// revision on its next read.
type Notification struct {
	SessionID ID
	Revision  uint64
}

// Task is one admitted observation handed to the recognizer. SessionID and
// Seq identify the entry; the recognizer echoes them when reporting the
// outcome so stale callbacks naming another session are rejected.
type Task struct {
	SessionID   ID
	Seq         Seq
	Observation any
}

// Recognition is the outcome one recognizer reports for a task: exactly
// one of Result and Failed is non-nil — the produced payload, or the error
// that failed this one task. A failed task does not stop the session;
// recognizer runtime failures are reported by returning an error from
// Recognize instead.
type Recognition struct {
	Result any
	Failed error
}

// Submit reports one recognition outcome to the session that admitted the
// task. sessionID must be that session's ID; callbacks naming another
// session are rejected. The first outcome per sequence number wins, so
// duplicates, outcomes for unknown or already-terminal sequences, and
// anything submitted after the session sealed are rejected with an error
// and change nothing.
type Submit func(sessionID ID, seq Seq, rec Recognition) error

// Record is the terminal outcome of one captured sequence, handed to the
// Deliverer for persistence exactly once per sequence. The kernel carries
// the observation payload through unchanged so the persistence layer can
// store the evidence with the outcome.
type Record struct {
	SessionID ID
	Seq       Seq
	// State is one of produced, failed or unprocessed.
	State State
	// Result is the recognizer payload when State is produced.
	Result any
	// Reason is the failure text for failed entries and the interrupt
	// detail for interrupted unprocessed entries; it is empty for
	// produced entries and for plain cancelled entries.
	Reason string
	// Cause says why an unprocessed entry never produced a result.
	Cause UnprocessedCause
	// Observation is the admitted payload, carried through unchanged.
	Observation any
}

// Controller drives the game side of one session; the kernel stays
// game-independent by injecting it. Its two methods run one after another
// on the session's producer goroutine: preparation first, collection once
// preparation passed. Input permission opens exactly when the kernel calls
// Collect.
type Controller interface {
	// Prepare runs the prechecks of the preparing phase: exclusivity,
	// save location, permissions, resources, stop means, target window and
	// range. It must return when ctx is cancelled. A non-nil error fails
	// the session before any input is permitted; a FaultError carries the
	// diagnostic code.
	Prepare(ctx context.Context, h *ControllerHandle) error
	// Collect runs the collection loop of the capturing phase. Before
	// every short game operation it checks h.InputPermitted; before
	// selecting the next item it acquires processing capacity with
	// h.Acquire and waits there while the pipeline is full; captured
	// observations are admitted with Slot.Admit and discarded attempts
	// with Slot.Discard. It returns nil when the range is complete
	// (normal completion) or a non-nil error to fail the session. It must
	// return promptly once ctx is cancelled.
	Collect(ctx context.Context, h *ControllerHandle) error
}

// Releaser is implemented by controllers that hold session resources
// across Prepare and Collect — a platform session with its capture stream,
// dispatcher and stop hotkey, for example. The kernel calls Release
// exactly once on the producer goroutine after the controller returned,
// with input permission already closed and before the finalizer waits for
// the workers and seals, so an ended session never leaves platform
// resources running. Without this hook a controller whose Prepare
// succeeded but whose session was stopped before entering Collect would
// never be asked to clean up, because Collect is the only method that
// could do it.
//
// Release must be idempotent in effect for defensive purposes but is
// called at most once per session. Its error is recorded as a
// releaseFailed fault and never changes the session's outcome.
type Releaser interface {
	Release() error
}

// Recognizer is the injected recognition worker. The kernel calls
// Recognize once per admitted sequence number, on its worker goroutines.
type Recognizer interface {
	// Recognize processes task and reports the outcome through submit:
	// per-task results as Recognition{Result: …} and per-task failures as
	// Recognition{Failed: …}. When ctx is cancelled — the terminate
	// request of the stop path — it must return without submitting, and
	// the task ends unprocessed. A returned non-nil error is a recognizer
	// runtime failure that stops the whole session, not a per-task
	// failure.
	Recognize(ctx context.Context, task Task, submit Submit) error
}

// Deliverer receives the terminal record of every captured sequence for
// persistence, exactly once per sequence and never concurrently with
// itself. It is not called with the cancelled scan context: finalization
// owns its own lifecycle, and a stop never cancels saving. A non-nil error
// stops collection while capturing (a write failure stops the scan) and is
// recorded as an additional fault once the session is already stopping; it
// never changes an already-terminal record. Implementations must not call
// back into the session.
type Deliverer interface {
	Deliver(rec Record) error
}
