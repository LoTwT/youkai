package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"log"
	"sync"
	"time"

	"github.com/LoTwT/youkai/internal/game/zzz"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/LoTwT/youkai/internal/scan"
)

// checkpointBatchSize is the number of document changes — one per
// admission and one per terminal outcome — accumulated before a
// mid-session checkpoint generation is written. Every save rewrites the
// whole document, so saving per change would be quadratic; the final
// checkpoint at wrap-up always covers the remainder. The value is a
// starting point — adjust it from measurements once real scans exist
// (docs/specs/scanner-implementation-plan.md M2 记录冷启动与延迟).
const checkpointBatchSize = 16

// scanPipelineCapacity is the bounded pipeline's capacity: the number of
// observations admitted but not yet terminal. The first version keeps it
// small, per the architecture's bounded-queue constraint; the value is a
// starting point to tune from M2 measurements.
const scanPipelineCapacity = 4

// recordStore is the persistence surface the application service needs.
// Production is *record.Store; the seam exists so service tests can
// inject save failures without touching the data root's permissions,
// which is not portable between platforms.
type recordStore interface {
	CreateRecord(id string) error
	WriteEvidence(recordID, evidenceID string, image []byte) (record.EvidenceFile, error)
	SaveCheckpoint(recordID string, cp record.Checkpoint) (int64, error)
	// CheckpointBytes is the saved generation's on-disk size for the
	// checkpoint diagnostic line. A failure only leaves the size unknown;
	// it never fails the save.
	CheckpointBytes(recordID string, revision int64) (int64, error)
	// SaveRevision appends one manual revision to a finished item of the
	// newest saved checkpoint. The review flow reaches it through the
	// service, never through the scan deliverer.
	SaveRevision(recordID string, sequence, expectedRevision int64, edit record.ItemEdit) (record.ItemRecord, int64, error)
	ListRecords() ([]record.RecordInfo, error)
	Load(recordID string) (*record.Recovery, error)
	Peek(recordID string) (*record.Checkpoint, error)
	// Evidence and record cleanup: screenshots are removed by rewriting
	// the checkpoint first, records by removing their directory. The
	// service exposes both with its own activity guards.
	Usage(recordID string) (record.RecordUsage, error)
	StoreUsage() (record.StoreUsage, error)
	DeleteEvidence(recordID string) (record.EvidenceRemoval, error)
	DeleteAllEvidence() (record.EvidenceBatch, error)
	DeleteRecord(recordID string) (record.RecordDeletion, error)
	DeleteAllRecords() (record.RecordDeletionBatch, error)
}

// recordDeliverer persists one session's record with admission-time
// recording: the kernel's admission hook (scan.Config.OnAdmit) writes the
// observation's evidence first and appends its item to the document in
// the queued state, so every admitted observation is on a checkpoint the
// moment it exists — a crash loses no admitted record, and recovery
// marks the entries it left queued or running as interrupted unprocessed
// instead of dropping them. The terminal records the kernel delivers
// afterwards only update their item's state and payload.
//
// Both entry points count towards the bounded batch that writes a new
// checkpoint generation; the final generation, carrying the session's
// termination and end time, is written once the kernel sealed. A save
// failure is reported to the kernel (a write failure stops collection),
// keeps the previous valid generation untouched and leaves the pending
// changes in memory for a retry.
type recordDeliverer struct {
	store    recordStore
	recordID string
	shell    record.SessionShell
	// onChange is invoked after every admission or terminal update that
	// changed the save state, so subscribers learn about saved and
	// pending counts. It is called without the deliverer's lock held and
	// only from the kernel's producer and worker goroutines — never from
	// finish or retry, whose callers hold the service lock and would
	// deadlock.
	onChange func()

	mu sync.Mutex
	// items holds one item per admitted sequence, in admission order —
	// which is sequence order, the producer being single. Items start
	// queued and are updated in place when their terminal record
	// arrives.
	items []record.ItemRecord
	// evidence indexes exactly the evidence the document references.
	evidence []record.EvidenceFile
	// sinceSave counts document changes since the last successful save.
	sinceSave int
	// savedItems is the item count of the newest saved generation.
	savedItems int
	// savedGeneration is the revision of the newest saved generation.
	savedGeneration int64
	// saveErr is the unresolved save failure; nil while clean.
	saveErr error
	// finished reports that finish ran (the kernel sealed).
	finished bool
	// finalRefused reports that finish refused the final generation
	// because an admitted item was still queued or running. A retry must
	// settle those entries instead of persisting them as the final state.
	finalRefused bool
}

// newRecordDeliverer builds the deliverer of one session's record. The
// shell carries the identity and provenance the service resolved at
// acceptance; the volatile parts (captured count, termination, end time)
// are maintained here.
func newRecordDeliverer(store recordStore, shell record.SessionShell, onChange func()) *recordDeliverer {
	return &recordDeliverer{
		store:    store,
		recordID: shell.SessionID,
		shell:    shell,
		onChange: onChange,
	}
}

// Admitted persists one admitted observation: its evidence images are
// written completely and closed first, then its item joins the document in
// the queued state, then a batched checkpoint saves when due. The store
// syncs the evidence cited by that checkpoint before writing it, so capture
// no longer pays one device flush per image. It implements the kernel's
// admission hook and runs on the controller's producer goroutine.
//
// An evidence failure does not withdraw the admission — the kernel
// already assigned the sequence number, and the captured count must
// match the admitted observations — so the item is recorded with
// whatever evidence survived and the error is returned for the kernel to
// report as a storage fault.
func (d *recordDeliverer) Admitted(seq scan.Seq, observation any) error {
	d.mu.Lock()
	if d.finished {
		d.mu.Unlock()
		return fmt.Errorf("app: record %s: admission %d after finish", d.recordID, seq)
	}
	// The single producer assigns the sequence numbers in order, so the
	// hook sees them in order; anything else is a contract violation
	// upstream, not a gap to paper over.
	if seq != scan.Seq(len(d.items)+1) {
		d.mu.Unlock()
		return fmt.Errorf("app: record %s: admission %d out of order, want %d", d.recordID, seq, len(d.items)+1)
	}
	item, evidence, evErr := d.buildAdmittedLocked(seq, observation)
	d.items = append(d.items, item)
	d.evidence = append(d.evidence, evidence...)
	d.sinceSave++
	var err error
	if evErr != nil {
		err = evErr
	} else if d.sinceSave >= checkpointBatchSize {
		err = d.saveLocked()
	}
	if err != nil {
		d.saveErr = err
	}
	d.mu.Unlock()
	if d.onChange != nil {
		d.onChange()
	}
	return err
}

// Deliver implements scan.Deliverer: the terminal record of an admitted
// observation updates its item's state and payload in place. The
// observation itself — evidence, observed payload, sequence — was sealed
// at admission and is never rewritten.
func (d *recordDeliverer) Deliver(rec scan.Record) error {
	d.mu.Lock()
	if d.finished {
		d.mu.Unlock()
		return fmt.Errorf("app: record %s: delivery of %d after finish", d.recordID, rec.Seq)
	}
	idx := int(rec.Seq) - 1
	if idx < 0 || idx >= len(d.items) || d.items[idx].Sequence != int64(rec.Seq) {
		d.mu.Unlock()
		return fmt.Errorf("app: record %s: terminal record for sequence %d, which was never admitted", d.recordID, rec.Seq)
	}
	item := &d.items[idx]
	if item.Status != record.StatusQueued && item.Status != record.StatusRunning {
		// Already terminal: the kernel's exactly-once delivery makes
		// this a defensive no-op, not a duplicate write.
		d.mu.Unlock()
		return nil
	}
	if err := applyTerminalLocked(item, rec); err != nil {
		d.mu.Unlock()
		return fmt.Errorf("app: record %s: item %d: %w", d.recordID, rec.Seq, err)
	}
	d.sinceSave++
	var err error
	if d.sinceSave >= checkpointBatchSize {
		err = d.saveLocked()
	}
	if err != nil {
		d.saveErr = err
	}
	d.mu.Unlock()
	if d.onChange != nil {
		d.onChange()
	}
	return err
}

// buildAdmittedLocked writes one observation's evidence and builds its
// queued item. Evidence first: every image is completely written and
// closed before any checkpoint may reference it; the store enforces the
// write-once discipline and syncs the cited files when that checkpoint is
// saved. A failing line does not stop the others — the evidence that
// survives is what the record keeps — and the first error is returned.
// Callers hold mu.
func (d *recordDeliverer) buildAdmittedLocked(seq scan.Seq, observation any) (record.ItemRecord, []record.EvidenceFile, error) {
	item := record.ItemRecord{
		Sequence: int64(seq),
		Status:   record.StatusQueued,
		// Admission itself carries no field verdict; the terminal
		// produced update decides needsReview versus autoAccepted from
		// the structured validation.
		Quality: record.QualityNotEvaluated,
	}
	observed, err := observedPayloadOf(observation)
	if err != nil {
		return item, nil, err
	}
	item.Observed = observed

	var evidence []record.EvidenceFile
	var evErr error
	if obs, ok := observation.(Observation); ok {
		write := func(id string, img image.Image) {
			if img == nil {
				return
			}
			ev, err := d.writeEvidence(id, img)
			if err != nil {
				if evErr == nil {
					evErr = err
				}
				return
			}
			item.EvidenceIDs = append(item.EvidenceIDs, id)
			evidence = append(evidence, ev)
		}
		for i, crop := range obs.Lines {
			write(evidenceID(seq, i), crop.Image)
		}
		for i, crop := range obs.Context {
			write(contextEvidenceID(seq, i), crop.Image)
		}
	}
	return item, evidence, evErr
}

// writeEvidence persists one crop as a PNG evidence file and returns its
// descriptor.
func (d *recordDeliverer) writeEvidence(id string, img image.Image) (record.EvidenceFile, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return record.EvidenceFile{}, fmt.Errorf("encode evidence: %w", err)
	}
	data := buf.Bytes()
	ev, err := d.store.WriteEvidence(d.recordID, id, data)
	if errors.Is(err, record.ErrEvidenceExists) {
		// A previous attempt already wrote this crop (evidence is
		// write-once and the store is the only writer below the data
		// root); the bytes are identical, so the descriptor is
		// recomputed from them. A mismatching leftover file fails the
		// next save's evidence check instead of passing silently.
		sum := sha256.Sum256(data)
		ev = record.EvidenceFile{
			ID:         id,
			ByteLength: int64(len(data)),
			SHA256:     hex.EncodeToString(sum[:]),
		}
	} else if err != nil {
		return record.EvidenceFile{}, fmt.Errorf("write evidence %s: %w", id, err)
	}
	return ev, nil
}

// autoAcceptable reports whether a recognized disc passed every current
// required-field check without an error issue. It deliberately does not
// use the raw OCR score as a correctness probability: T6's confirmed
// policy is to accept records whose current validation passed and to
// leave incomplete or conflicting records for review.
func autoAcceptable(structured *zzz.StructuredDisc) bool {
	return structured != nil &&
		structured.Validation.Status == zzz.ValidationPassed &&
		!hasIssueError(structured.Issues) &&
		!hasIssueError(structured.Validation.Issues)
}

// applyTerminalLocked updates one item with its terminal record. The
// state mapping mirrors the kernel's: produced carries the result
// payload, failed the failure text, unprocessed the interrupt cause.
func applyTerminalLocked(item *record.ItemRecord, rec scan.Record) error {
	switch rec.State {
	case scan.StateProduced:
		item.Status = record.StatusProduced
		data, err := marshalPayload(rec.Result)
		if err != nil {
			return err
		}
		item.Data = data
		// A fully validated disc is auto-accepted; any missing required
		// field, parse error or unavailable validation stays pending.
		// Auto-accept means the current checks found no problem, not that
		// the OCR reading is guaranteed correct.
		if result, ok := rec.Result.(RecognitionResult); ok {
			item.Quality = record.QualityNeedsReview
			if autoAcceptable(result.Structured) {
				item.Quality = record.QualityAutoAccepted
			}
		}
	case scan.StateFailed:
		item.Status = record.StatusFailed
		item.Failure = rec.Reason
	case scan.StateUnprocessed:
		item.Status = record.StatusUnprocessed
		switch rec.Cause {
		case scan.CauseCancelled:
			item.Reason = record.ReasonCancelled
		case scan.CauseInterrupted:
			item.Reason = record.ReasonInterrupted
		default:
			return fmt.Errorf("unknown unprocessed cause %q", rec.Cause)
		}
	default:
		return fmt.Errorf("non-terminal state %s", rec.State)
	}
	return nil
}

// observeScope records the range the traversal's precheck actually
// observed (T5 §3): it amends the shell's ObservedScope — the declared
// Scope stays untouched — and persists with the very next checkpoint,
// so the observed range survives a crash right after the precheck. A
// report that is not a JSON document is refused, not silently dropped:
// the record only keeps what verifies.
func (d *recordDeliverer) observeScope(raw json.RawMessage) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.finished {
		return
	}
	if len(raw) == 0 || !json.Valid(raw) || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		d.saveErr = fmt.Errorf("app: record %s: observed scope is not a JSON document: %q", d.recordID, raw)
		return
	}
	d.shell.ObservedScope = append(json.RawMessage(nil), raw...)
	if status, ok := observedCoverage(raw); ok {
		d.shell.Coverage.Status = status
	}
	d.sinceSave++
	if d.saveErr == nil {
		d.saveErr = d.saveLocked()
	}
	if d.saveErr == nil {
		d.sinceSave = 0
	}
}

// observedCoverage reads the coverage conclusion the traversal wrote into
// its observed-scope report. An absent or unknown field leaves the shell's
// existing conclusion alone; only the three valid record statuses are
// accepted, so a malformed scope cannot invent a conclusion.
func observedCoverage(raw json.RawMessage) (record.CoverageStatus, bool) {
	var scope struct {
		Coverage string `json:"coverage"`
	}
	if err := json.Unmarshal(raw, &scope); err != nil {
		return "", false
	}
	switch status := record.CoverageStatus(scope.Coverage); status {
	case record.CoverageUnknown, record.CoveragePartial, record.CoverageComplete:
		return status, true
	default:
		return "", false
	}
}

// saveLocked writes the document under construction as a new checkpoint
// generation. The captured count is the admitted count — every admitted
// observation is in the document, whatever its state. Callers hold mu.
func (d *recordDeliverer) saveLocked() error {
	d.shell.CapturedCount = int64(len(d.items))
	doc := record.Checkpoint{
		Session:  d.shell,
		Items:    append([]record.ItemRecord(nil), d.items...),
		Evidence: append([]record.EvidenceFile(nil), d.evidence...),
	}
	started := time.Now()
	gen, err := d.store.SaveCheckpoint(d.recordID, doc)
	elapsed := time.Since(started)
	if err != nil {
		return fmt.Errorf("app: save checkpoint of %s: %w", d.recordID, err)
	}
	size, sizeErr := d.store.CheckpointBytes(d.recordID, gen)
	if sizeErr != nil {
		size = -1
	}
	d.savedGeneration = gen
	d.savedItems = len(d.items)
	d.sinceSave = 0
	log.Printf("youkai: scan %s: checkpoint generation %d saved (%d items, %d bytes, %d ms)",
		d.recordID, gen, d.savedItems, size, elapsed.Milliseconds())
	return nil
}

// finish writes the final checkpoint after the kernel sealed: the shell
// gains its termination and end time and the final generation is written
// even when the last batch already flushed — the termination must reach
// disk. Every admitted sequence is terminal by then; a non-terminal item
// would mean the seal happened without a delivery, which is refused
// rather than persisted as a lie. A failure leaves the previous
// generation as the last valid state and the pending changes in memory;
// retry clears it.
func (d *recordDeliverer) finish(final scan.Snapshot) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.finished {
		return d.saveErr
	}
	d.finished = true
	for i := range d.items {
		switch d.items[i].Status {
		case record.StatusQueued, record.StatusRunning:
			// The kernel seals only after every admitted sequence reached
			// a terminal outcome, so this is a broken invariant. Refuse to
			// record the kernel's end while an item is still mid-flight;
			// the failure stays visible, and a retry settles the entries as
			// interrupted instead of persisting a completed session with
			// unfinished rows.
			d.saveErr = fmt.Errorf("app: record %s: item %d is still %s at the end of the session",
				d.recordID, d.items[i].Sequence, d.items[i].Status)
			d.finalRefused = true
			return d.saveErr
		}
	}
	ended := nowUTC()
	d.shell.EndedAt = &ended
	term := terminationFrom(final)
	d.shell.Termination = &term
	if err := d.saveLocked(); err != nil {
		d.saveErr = err
		return err
	}
	d.saveErr = nil
	return nil
}

// retry re-attempts the failed save as a new generation. It is a no-op
// while clean. When the final generation was refused because an item
// never reached a terminal outcome, the retry cannot invent that outcome:
// it settles the record the way recovery reads an interrupted session —
// the unfinished entries become unprocessed/interrupted, a complete
// coverage claim is downgraded, and the termination says interrupted.
// Without that settlement a successful retry would persist a mid-flight
// row as the final result.
func (d *recordDeliverer) retry() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.saveErr == nil {
		return nil
	}
	if d.finalRefused {
		for i := range d.items {
			switch d.items[i].Status {
			case record.StatusQueued, record.StatusRunning:
				d.items[i].Status = record.StatusUnprocessed
				d.items[i].Reason = record.ReasonInterrupted
			}
		}
		if d.shell.Coverage.Status == record.CoverageComplete {
			// Missing terminal outcomes cannot uphold a complete scan.
			d.shell.Coverage.Status = record.CoveragePartial
		}
		ended := nowUTC()
		d.shell.EndedAt = &ended
		d.shell.Termination = &record.Termination{
			Kind:  record.TerminationInterrupted,
			Code:  "interrupted",
			Phase: record.PhaseCapturing,
		}
	}
	if err := d.saveLocked(); err != nil {
		d.saveErr = err
		return err
	}
	d.saveErr = nil
	d.finalRefused = false
	return nil
}

// saveStatus is the deliverer's contribution to the service snapshot.
type saveStatus struct {
	saved   int
	pending bool
	err     error
}

// status reports the save state for the snapshot. Callers hold no locks
// of the deliverer.
func (d *recordDeliverer) status() saveStatus {
	d.mu.Lock()
	defer d.mu.Unlock()
	return saveStatus{
		saved:   d.savedItems,
		pending: d.saveErr != nil || d.sinceSave > 0,
		err:     d.saveErr,
	}
}

// failed reports an unresolved save failure.
func (d *recordDeliverer) failed() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.saveErr != nil
}

// settled reports that the final checkpoint — the one carrying the
// termination — is on disk.
func (d *recordDeliverer) settled() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.finished && d.saveErr == nil
}

// generation returns the newest saved generation's revision.
func (d *recordDeliverer) generation() int64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.savedGeneration
}

// observedPayloadOf renders an observation as the observed payload. An
// Observation persists its line metadata (the pixels live in the
// evidence files); any other payload — a test double, or a future
// controller's own type — is stored as its JSON encoding, if it has one.
func observedPayloadOf(observation any) (json.RawMessage, error) {
	switch obs := observation.(type) {
	case nil:
		return nil, nil
	case Observation:
		return observedPayload(obs)
	default:
		return marshalPayload(obs)
	}
}

// marshalPayload encodes a payload as JSON, treating a literal null as
// absence: payloads use omission for "no result yet" and must not fake
// one with null (the same distinction the record package keeps).
func marshalPayload(v any) (json.RawMessage, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("encode payload: %w", err)
	}
	if string(raw) == "null" {
		return nil, nil
	}
	return raw, nil
}

// evidenceID names one line's evidence file: the capture sequence and
// the line's index within the observation, stable across retries so a
// second attempt addresses the same file.
func evidenceID(seq scan.Seq, line int) string {
	return fmt.Sprintf("%06d-line%d", seq, line)
}

// contextEvidenceID names one non-recognition context crop's evidence
// file: the capture sequence and the context index. Context evidence is
// written beside the line crops and never recognized.
func contextEvidenceID(seq scan.Seq, context int) string {
	return fmt.Sprintf("%06d-context%d", seq, context)
}

// terminationFrom maps the kernel's final snapshot onto the record's
// termination: the end kind from the first trigger (the kernel derives
// it), the trigger's code and phase, and the verified-end code for a
// normal completion. A completed session has no trigger; its phase is
// the capturing the range completed from.
func terminationFrom(final scan.Snapshot) record.Termination {
	t := record.Termination{Phase: record.PhaseCapturing}
	if final.Trigger != nil {
		t.Phase = record.Phase(final.Trigger.Phase)
		t.Code = final.Trigger.Code
	}
	switch final.End {
	case scan.EndCompleted:
		t.Kind = record.TerminationCompleted
		if t.Code == "" {
			t.Code = scan.CodeVerifiedEnd
		}
	case scan.EndCancelled:
		t.Kind = record.TerminationCancelled
		if t.Code == "" {
			t.Code = scan.CodeUserCancelled
		}
	case scan.EndFailed:
		t.Kind = record.TerminationFailed
		if t.Code == "" {
			t.Code = scan.CodeControllerFailed
		}
	default:
		// The kernel sets the end kind at sealing; anything else is a
		// wiring fault, recorded as such rather than guessed at.
		t.Kind = record.TerminationFailed
		t.Code = "unknownEnd"
	}
	return t
}

// nowUTC is the persistence layer's clock; timestamps are UTC throughout.
func nowUTC() time.Time {
	return time.Now().UTC()
}
