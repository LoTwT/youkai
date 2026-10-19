package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"sync"
	"time"

	"github.com/LoTwT/youkai/internal/recognition"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/LoTwT/youkai/internal/scan"
)

// ScanSpec is what one accepted start needs beyond the request itself:
// the collection controller that drives this session, and the
// provenance fields the record shell persists about the scan. The scope
// is opaque here — the game adapter defines what a scan set out to
// cover; M1's development source describes its directory.
type ScanSpec struct {
	// Controller drives the collection side of exactly one session; the
	// scan kernel calls its Prepare and Collect once.
	Controller scan.Controller
	// Game, Entity, Client and Locale populate the record shell and must
	// be non-empty (the client one of record's known clients).
	Game   string
	Entity string
	Client record.Client
	Locale string
	// Scope is the scan's own description of what it set out to cover,
	// persisted as-is; it must be a JSON document, not null.
	Scope json.RawMessage
}

// validate fails fast on a spec the record could only reject at its
// first save, mid-scan.
func (spec ScanSpec) validate() error {
	if spec.Controller == nil {
		return errors.New("controller is nil")
	}
	if spec.Game == "" || spec.Entity == "" || spec.Locale == "" {
		return fmt.Errorf("game %q, entity %q or locale %q is empty", spec.Game, spec.Entity, spec.Locale)
	}
	switch spec.Client {
	case record.ClientWindowsLocal, record.ClientWindowsCloud, record.ClientMacOSCloud:
	default:
		return fmt.Errorf("client %q is not a known record client", spec.Client)
	}
	if len(spec.Scope) == 0 || !json.Valid(spec.Scope) {
		return errors.New("scope is not a JSON document")
	}
	if bytes.Equal(bytes.TrimSpace(spec.Scope), []byte("null")) {
		return errors.New("scope is null")
	}
	return nil
}

// ScanSource supplies a fresh ScanSpec for every accepted start. M1's
// only wired implementation is the development data source; the game
// adapters of M2 take the place without the service changing.
type ScanSource interface {
	NewScan() (ScanSpec, error)
}

// Recognizer is the process-lifetime recognizer the service hands to
// every session: the scan kernel's Recognizer plus the Close the
// explicit-exit wrap-up calls before destroying the OCR environment.
// Sessions share one instance; Close belongs to the application's exit,
// never to a single scan.
type Recognizer interface {
	scan.Recognizer
	Close() error
}

// Config assembles the application service.
type Config struct {
	// Paths are the app's resolved directories. Data is the record
	// store's root and Resources the OCR artifacts' root; the service
	// validates that Data is absolute.
	Paths Paths
	// DataLock is the data-root occupancy this process holds; the
	// service takes the scan occupancy through it on every start and
	// releases the data root itself in the explicit-exit wrap-up.
	DataLock *DataRootLock
	// Source supplies the scans the service can start; nil is the M1
	// production state — starts are refused with a clear error until
	// the game adapters of M2 arrive.
	Source ScanSource
	// Store persists records below Paths.Data. Nil opens a
	// *record.Store; tests inject a failing wrapper through this seam.
	Store recordStore
	// NewRecognizer creates the process-lifetime recognizer, called on
	// the first accepted start — never at construction, never in a
	// ready callback — and reused afterwards. Nil loads the bundled OCR
	// runtime and model from Paths.Resources.
	NewRecognizer func() (Recognizer, error)
}

// Service is youkai's application service: the owner of the data-root
// occupancy, the record store, the process-lifetime recognizer and the
// single active scan session. Its lifecycle belongs to the process, not
// to a window or a button callback: the UI only calls Start, Stop,
// Snapshot, Subscribe, RetrySave, the record listing, SaveItemRevision
// and Quit. A window hiding or rebuilding never starts or ends a session.
//
// One start request occupies the single active slot. The slot frees when
// the session ended and its final save succeeded; a failed save keeps it
// occupied — results not yet saved forbid a new session — until a retry
// persists them. An explicit quit does not accept the loss: it reports
// the unsaved result and leaves the process running.
type Service struct {
	cfg           Config
	store         recordStore
	newRecognizer func() (Recognizer, error)

	mu         sync.Mutex
	revision   uint64
	active     *activeSession
	subs       []*Subscription
	quitting   bool
	recognizer Recognizer

	// quitMu serializes Quit attempts; the teardown below it runs once.
	quitMu           sync.Mutex
	quitTeardownDone bool
	quitTeardownErr  error
}

// activeSession is the one session occupying the active slot, from
// acceptance until its wrap-up (and, after a failed final save, until a
// retry settles it). A session accepted but not yet committed has no
// kernel: its slow preparation runs without the service lock.
type activeSession struct {
	id        scan.ID
	requestID string
	startedAt time.Time
	kernel    *scan.Session
	deliverer *recordDeliverer
	scanLock  *ScanLock
	// stopRequested: a stop arrived before the kernel existed; it is
	// applied at commit, so no stop is lost to the preparation window.
	stopRequested bool
	// wrapped: the final checkpoint is on disk and the slot is free.
	wrapped bool
	// saveErr: the wrap-up's save failure, kept while a retry is due.
	saveErr error
	// wrapDone closes when the wrap-up goroutine finished its attempt —
	// or when a refused start rolled the session back.
	wrapDone  chan struct{}
	kernelSub *scan.Subscription
}

// StartRequest identifies one start attempt.
type StartRequest struct {
	// ID is the process-unique start request identifier the UI mints.
	// The same ID reaching the service again returns the session it
	// created — even after that session ended — instead of starting
	// another one; a different ID while the slot is occupied returns the
	// active session with a busy marker. Nothing persists request IDs:
	// a restarted process never replays a start.
	ID string
}

// StartOutcome is what Start returns: the session the request produced
// or found, and why.
type StartOutcome struct {
	// SessionID identifies that session.
	SessionID scan.ID
	// Replayed: the request ID had already been served; SessionID is
	// the original session (which may have ended long ago).
	Replayed bool
	// Busy: the active slot is occupied right now — by this request's
	// own session or by another's — so no new session starts.
	Busy bool
}

// NewService assembles the application service. It takes over the
// data-root occupancy for the explicit-exit wrap-up; callers keep their
// own release as a crash fallback (Close is idempotent).
func NewService(cfg Config) (*Service, error) {
	if cfg.DataLock == nil {
		return nil, errors.New("app: new service: data-root lock is nil")
	}
	if cfg.Paths.Data == "" || !filepath.IsAbs(cfg.Paths.Data) {
		return nil, fmt.Errorf("app: new service: data directory %q is not an absolute path", cfg.Paths.Data)
	}
	store := cfg.Store
	if store == nil {
		var err error
		store, err = record.Open(cfg.Paths.Data)
		if err != nil {
			return nil, fmt.Errorf("app: new service: %w", err)
		}
	}
	newRecognizer := cfg.NewRecognizer
	if newRecognizer == nil {
		newRecognizer = func() (Recognizer, error) {
			return newOCRRecognizer(cfg.Paths.Resources)
		}
	}
	svc := &Service{cfg: cfg, store: store, newRecognizer: newRecognizer}
	// Sources that own a stop entry (the game scan source, whose
	// sessions register a global stop hotkey) receive the service's Stop
	// here, so a hotkey-driven stop walks the same idempotent entry as
	// the UI — including the stopRequested path while a start is still
	// committing.
	if binder, ok := cfg.Source.(interface{ bindStop(func()) }); ok {
		binder.bindStop(svc.Stop)
	}
	return svc, nil
}

// Start accepts a start request into the single active slot: it takes
// the scan occupancy, creates the session's record, creates the
// process-lifetime recognizer if this is the first start, and starts the
// session kernel. The busy and replay outcomes are protocol answers, not
// errors; errors mean the start was refused (another process holds the
// scan occupancy, no source is wired, the recognizer's resources are
// unusable).
//
// Acceptance occupies the slot at once — a placeholder session with the
// new identity — but the slow preparation (scan occupancy, recognizer
// creation, record creation) runs without the service lock, so
// snapshots, stops and subscriptions keep working while the first model
// load takes its time. The commit re-checks the state under the lock and
// rolls the placeholder back on any refusal.
func (s *Service) Start(req StartRequest) (StartOutcome, error) {
	if req.ID == "" {
		return StartOutcome{}, errors.New("app: start: request id is empty")
	}
	recordID, err := record.NewRecordID()
	if err != nil {
		return StartOutcome{}, fmt.Errorf("app: start: %w", err)
	}
	s.mu.Lock()
	if s.quitting {
		s.mu.Unlock()
		return StartOutcome{}, errors.New("app: start: the service is quitting")
	}
	var prev *activeSession
	if a := s.active; a != nil {
		if a.requestID == req.ID {
			// The same request again: its session, whatever its state.
			out := s.outcomeLocked(a, true)
			s.mu.Unlock()
			return out, nil
		}
		if !a.wrapped {
			// Another request's session holds the slot.
			out := s.outcomeLocked(a, false)
			s.mu.Unlock()
			return out, nil
		}
		// The previous session wrapped cleanly; this request replaces it.
		prev = a
	}
	a := &activeSession{
		id:        scan.ID(recordID),
		requestID: req.ID,
		startedAt: nowUTC(),
		wrapDone:  make(chan struct{}),
	}
	s.active = a
	s.notifyLocked()
	s.mu.Unlock()

	// abort rolls the placeholder back and restores prev. Only the
	// goroutine that placed the placeholder calls it, exactly once.
	abort := func(cause error) (StartOutcome, error) {
		s.mu.Lock()
		if s.active == a {
			s.active = prev
		}
		close(a.wrapDone) // a concurrent Quit may be waiting on it
		s.notifyLocked()
		s.mu.Unlock()
		log.Printf("youkai: scan %s refused (request %q): %v", recordID, req.ID, cause)
		return StartOutcome{}, fmt.Errorf("app: start: %w", cause)
	}

	if s.cfg.Source == nil {
		return abort(errors.New("no scan source is wired"))
	}
	spec, err := s.cfg.Source.NewScan()
	if err != nil {
		return abort(err)
	}
	if err := spec.validate(); err != nil {
		return abort(err)
	}
	// The scan occupancy comes first: another youkai process of this OS
	// user must refuse here, before any work begins. The placeholder
	// already owns the in-process slot, so no other start can race for
	// it while this one holds the lock file.
	scanLock, err := s.cfg.DataLock.AcquireScanLock()
	if err != nil {
		return abort(err)
	}
	rollback := func(cause error) (StartOutcome, error) {
		if err := scanLock.Close(); err != nil {
			log.Printf("youkai: release scan occupancy after a refused start: %v", err)
		}
		return abort(cause)
	}
	// The recognizer is created on the first start and reused for the
	// process lifetime; a failed creation leaves it unset so the next
	// start retries it.
	s.mu.Lock()
	recognizer := s.recognizer
	s.mu.Unlock()
	if recognizer == nil {
		rec, err := s.newRecognizer()
		if err != nil {
			return rollback(fmt.Errorf("create recognizer: %w", err))
		}
		if rec == nil {
			return rollback(errors.New("create recognizer: factory returned nil"))
		}
		recognizer = rec
		s.mu.Lock()
		if s.recognizer == nil {
			s.recognizer = rec
		}
		s.mu.Unlock()
	}
	if err := s.store.CreateRecord(recordID); err != nil {
		return rollback(err)
	}
	shell := record.SessionShell{
		SessionID: recordID,
		Game:      spec.Game,
		Entity:    spec.Entity,
		Client:    spec.Client,
		Locale:    spec.Locale,
		Scope:     append(json.RawMessage(nil), spec.Scope...),
		StartedAt: a.startedAt,
		// The development source verifies no traversal, so no coverage
		// conclusion is claimed; the game adapter of M2 owns coverage
		// evidence, and recovery never upgrades a conclusion.
		Coverage: record.Coverage{Status: record.CoverageUnknown},
	}
	deliverer := newRecordDeliverer(s.store, shell, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.notifyLocked()
	})
	// A controller whose traversal reports the range it observed during
	// preparation (T5 §3) gets the deliverer's sink wired in before the
	// session starts, so the observed range reaches the record.
	if relay, ok := spec.Controller.(scopeRelay); ok {
		relay.SetScopeSink(deliverer.observeScope)
	}
	kernel, err := scan.Start(scan.Config{
		ID:         scan.ID(recordID),
		Capacity:   scanPipelineCapacity,
		Workers:    1, // the first version runs one recognition worker
		Controller: spec.Controller,
		Recognizer: recognizer,
		Deliverer:  deliverer,
		// The admission hook persists every admitted observation —
		// evidence first, then its queued item — before its task reaches
		// the workers, so a crash never loses an admitted record.
		OnAdmit: deliverer.Admitted,
	})
	if err != nil {
		return rollback(err)
	}

	// Commit under the lock: a quit that began during the preparation
	// refuses the session instead of racing it.
	s.mu.Lock()
	if s.quitting || s.active != a {
		s.mu.Unlock()
		kernel.Cancel()
		<-kernel.Done()
		return rollback(errors.New("the service began quitting while preparing"))
	}
	a.kernel = kernel
	a.deliverer = deliverer
	a.scanLock = scanLock
	if prev != nil {
		prev.kernelSub.Close()
	}
	a.kernelSub = kernel.Subscribe()
	if a.stopRequested {
		// A stop arrived while the session was still preparing; it
		// applies to this session, whose input never opened.
		kernel.Cancel()
	}
	go s.forwardKernel(a)
	go s.wrapUp(a)
	s.notifyLocked()
	s.mu.Unlock()
	log.Printf("youkai: scan %s accepted (request %q)", recordID, req.ID)
	return StartOutcome{SessionID: a.id}, nil
}

// outcomeLocked builds the replay or busy answer for a's session.
// Callers hold mu.
func (s *Service) outcomeLocked(a *activeSession, replayed bool) StartOutcome {
	return StartOutcome{SessionID: a.id, Replayed: replayed, Busy: !a.wrapped}
}

// Stop requests a user stop of the active session: idempotent, and a
// pure forward to the kernel's Cancel — the stop's effect (closing input
// permission, marking queued work unprocessed, terminating in-flight
// recognition) belongs to the kernel, and saving is never cancelled by
// it.
func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.active
	if a == nil {
		return
	}
	if a.kernel != nil {
		a.kernel.Cancel()
		return
	}
	// The session is still preparing: remember the stop and apply it at
	// commit, so no stop is lost to the preparation window.
	a.stopRequested = true
}

// Snapshot is the authoritative state of the scan side at one service
// revision: the active (or last) session's kernel snapshot plus the
// saving progress. Notifications only mean "something changed";
// subscribers re-fetch this and discard what they have already seen.
func (s *Service) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshotLocked()
}

func (s *Service) snapshotLocked() Snapshot {
	snap := Snapshot{Revision: s.revision}
	if a := s.active; a != nil {
		snap.SessionID = a.id
		snap.Busy = !a.wrapped
		if a.kernel != nil {
			snap.Scan = a.kernel.Snapshot()
		} else {
			// Accepted but not yet started: the prechecks (scan
			// occupancy, recognizer resources, the record) run here.
			snap.Scan = scan.Snapshot{SessionID: a.id, Revision: 1, Phase: scan.PhasePreparing}
		}
		if a.deliverer != nil {
			st := a.deliverer.status()
			snap.SavedCount = st.saved
			snap.SavePending = st.pending
			if st.err != nil {
				snap.SaveError = st.err.Error()
			}
		}
	}
	return snap
}

// Snapshot is the service's authoritative state. Scan carries the
// kernel's snapshot, zero when no session ever started.
type Snapshot struct {
	// SessionID identifies the session this snapshot describes; empty
	// when the service has never started one.
	SessionID scan.ID
	// Revision increases on every change — kernel events, save state,
	// slot transitions. The pair (SessionID, Revision) is what a
	// subscriber compares to discard stale reads.
	Revision uint64
	// Busy: the active slot is occupied; a start would not begin a new
	// session now.
	Busy bool
	// SavedCount is the number of terminal records the newest saved
	// checkpoint generation contains.
	SavedCount int
	// SavePending: terminal content exists that no saved generation
	// covers — a batch in flight, or a failed save awaiting retry.
	SavePending bool
	// SaveError is the unresolved save failure's text, empty while
	// clean.
	SaveError string
	// Scan is the kernel's authoritative snapshot of the session.
	Scan scan.Snapshot
}

// Notification tells a subscriber that the service's state changed; it
// carries no state of its own. Notifications coalesce: a slow subscriber
// misses intermediate ones and still sees the latest revision on the
// next snapshot.
type Notification struct {
	SessionID scan.ID
	Revision  uint64
}

// Subscription is one registered change listener of the service.
type Subscription struct {
	Initial Snapshot
	Changes <-chan Notification

	s      *Service
	ch     chan Notification
	closed bool
}

// Subscribe registers a change listener and returns the authoritative
// snapshot at subscription time. Close removes it; reading Changes
// afterwards yields the buffered remainder and then the closed channel.
func (s *Service) Subscribe() *Subscription {
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

// notifyLocked bumps the revision and notifies every subscriber without
// blocking. Callers hold mu.
func (s *Service) notifyLocked() {
	s.revision++
	n := Notification{Revision: s.revision}
	if s.active != nil {
		n.SessionID = s.active.id
	}
	for _, sub := range s.subs {
		select {
		case sub.ch <- n:
		default:
		}
	}
}

// forwardKernel forwards the kernel's coalesced notifications into the
// service's own stream for as long as a's subscription lives.
func (s *Service) forwardKernel(a *activeSession) {
	for range a.kernelSub.Changes {
		s.mu.Lock()
		if s.active != a {
			s.mu.Unlock()
			return
		}
		s.notifyLocked()
		s.mu.Unlock()
	}
}

// wrapUp waits for the session to end, persists the final checkpoint and
// releases the scan occupancy: the process stopped operating the game
// the moment the kernel sealed, so the occupancy frees now rather than
// at process exit. The slot itself stays occupied until the final save
// succeeded — unsaved results forbid a new session.
func (s *Service) wrapUp(a *activeSession) {
	<-a.kernel.Done()
	final := a.kernel.Snapshot()
	err := a.deliverer.finish(final)
	if lockErr := a.scanLock.Close(); lockErr != nil {
		// The OS releases the occupancy at process exit anyway; the
		// failure is diagnosable, but it must not gate the slot.
		log.Printf("youkai: scan %s: release scan occupancy: %v", a.id, lockErr)
	}
	s.mu.Lock()
	if err != nil && a.wrapped {
		// A retry persisted the final generation while this attempt was
		// failing. The newer success is the outcome: do not revive the
		// stale failure, which would keep the slot occupied and make Quit
		// refuse an exit whose save is already complete.
		a.saveErr = nil
		log.Printf("youkai: scan %s ended (%s): final save completed on a concurrent retry",
			a.id, final.End)
	} else if err != nil {
		a.saveErr = err
		log.Printf("youkai: scan %s ended (%s): final save failed: %v (retry available)",
			a.id, final.End, err)
	} else {
		a.saveErr = nil
		if !a.wrapped {
			a.wrapped = true
			log.Printf("youkai: scan %s ended (%s): %d captured, %d produced, %d unprocessed; checkpoint %s",
				a.id, final.End, final.Counts.Captured, final.Counts.Produced, final.Counts.Unprocessed,
				filepath.Join(s.cfg.Paths.Data, "sessions", string(a.id), "checkpoints",
					fmt.Sprintf("%d.json", a.deliverer.generation())))
		}
	}
	close(a.wrapDone)
	a.kernelSub.Close()
	s.notifyLocked()
	s.mu.Unlock()
}

// RetrySave re-attempts the pending persistence of the active session
// after a save failure: buffered evidence is written again, the document
// extends and a new generation is saved. It is a no-op while nothing
// failed. A successful retry of a failed final save frees the active
// slot.
func (s *Service) RetrySave() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.active
	if a == nil || a.deliverer == nil || !a.deliverer.failed() {
		return nil
	}
	if err := a.deliverer.retry(); err != nil {
		s.notifyLocked()
		return err
	}
	if a.deliverer.settled() && !a.wrapped {
		// The failed final save is now complete: the wrap-up's own
		// attempt failed, this retry wrote the final generation.
		a.wrapped = true
		a.saveErr = nil
		log.Printf("youkai: scan %s: final checkpoint saved on retry (generation %d)",
			a.id, a.deliverer.generation())
	}
	s.notifyLocked()
	return nil
}

// ListRecords lists the saved records below the data root, newest save
// first, for the reopen flow of the UI.
func (s *Service) ListRecords() ([]record.RecordInfo, error) {
	return s.store.ListRecords()
}

// LoadRecord loads one saved record: the newest checkpoint generation
// that verifies, with the record package's recovery semantics applied —
// entries the crash left queued or running come back interrupted, and a
// coverage conclusion is never upgraded.
func (s *Service) LoadRecord(recordID string) (*record.Recovery, error) {
	return s.store.Load(recordID)
}

// Quit runs the explicit-exit wrap-up: it stops the active session,
// waits for the session's finalization and saving, closes the recognizer,
// destroys the OCR environment and releases the data-root occupancy — in
// that order. It blocks until the wrap-up is done, so callers invoke it
// from their own goroutine (a quit callback must not wait on finalization
// or disk writes); repeated and concurrent calls join the same sequence.
//
// A failed final save is not a completed exit: Quit returns the error
// with the teardown deliberately not run, so the process can retry the
// save and quit again — the last valid generation stays as the durable
// result either way. Quit after a successful teardown returns its
// outcome unchanged.
func (s *Service) Quit() error {
	s.quitMu.Lock()
	defer s.quitMu.Unlock()
	if s.quitTeardownDone {
		return s.quitTeardownErr
	}
	s.mu.Lock()
	s.quitting = true
	a := s.active
	s.mu.Unlock()
	if a != nil {
		if a.kernel != nil {
			a.kernel.Cancel()
		}
		<-a.wrapDone
		s.mu.Lock()
		saveErr := a.saveErr
		s.mu.Unlock()
		if saveErr != nil {
			return fmt.Errorf("app: quit: scan %s still has an unsaved result: %w (retry the save, then quit again)", a.id, saveErr)
		}
	}
	err := s.teardown()
	s.quitTeardownDone = true
	s.quitTeardownErr = err
	return err
}

// teardown runs the exit sequence exactly once: recognizer close, OCR
// environment shutdown, data-root release. The recognizer closes only
// after every session ended — its Recognize calls run on session
// workers, which the wrap-up already joined — and the data-root
// occupancy goes last, after which nothing below the root may be written
// again.
func (s *Service) teardown() error {
	s.mu.Lock()
	rec := s.recognizer
	s.recognizer = nil
	s.mu.Unlock()
	var errs []error
	if rec != nil {
		if err := rec.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close recognizer: %w", err))
		}
	}
	if err := recognition.Shutdown(); err != nil {
		errs = append(errs, fmt.Errorf("shut down the OCR environment: %w", err))
	}
	if err := s.cfg.DataLock.Close(); err != nil {
		errs = append(errs, fmt.Errorf("release the data-root occupancy: %w", err))
	}
	err := errors.Join(errs...)
	if err != nil {
		log.Printf("youkai: exit wrap-up: %v", err)
	} else {
		log.Printf("youkai: exit wrap-up complete")
	}
	return err
}
