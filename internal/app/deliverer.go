package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"log"
	"slices"
	"sync"
	"time"

	"github.com/LoTwT/youkai/internal/record"
	"github.com/LoTwT/youkai/internal/scan"
)

// checkpointBatchSize is the number of terminal records appended to the
// document before a mid-session checkpoint generation is written. Every
// save rewrites the whole document, so saving per record would be
// quadratic; the final checkpoint at wrap-up always covers the remainder.
// The value is a starting point — adjust it from measurements once real
// scans exist (docs/specs/scanner-implementation-plan.md M2 记录冷启动与延迟).
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
	ListRecords() ([]record.RecordInfo, error)
	Load(recordID string) (*record.Recovery, error)
}

// recordDeliverer implements scan.Deliverer for one session's record:
// every terminal record is persisted evidence-first (the images are
// completely written, synced and closed before any checkpoint may
// reference them), appended to the document under construction and
// flushed to a new checkpoint generation in bounded batches. The final
// checkpoint — with the session's termination and end time — is written
// once the kernel sealed; a save failure is reported to the kernel (a
// write failure stops collection), keeps the previous valid generation
// untouched and leaves the pending content in memory for a retry.
//
// The kernel delivers terminal records in completion order, which with
// more than one worker need not be sequence order; the document requires
// sequences 1..n contiguous, so records beyond a gap wait in buffered
// until the prefix reaches them. Their evidence is already durable by
// then; only the checkpoint lags.
type recordDeliverer struct {
	store    recordStore
	recordID string
	shell    record.SessionShell
	// onChange is invoked after every Deliver that changed the save
	// state, so subscribers learn about saved and pending counts. It is
	// called without the deliverer's lock held and only from the
	// kernel's delivery goroutines — never from finish or retry, whose
	// callers hold the service lock and would deadlock.
	onChange func()

	mu sync.Mutex
	// items is the contiguous prefix of terminal item records, 1..k.
	items []record.ItemRecord
	// evidence indexes exactly the evidence the prefix references.
	evidence []record.EvidenceFile
	// buffered holds delivered records the prefix has not reached.
	buffered map[scan.Seq]*pendingItem
	// nextSeq is the sequence the prefix extends to next, k+1.
	nextSeq scan.Seq
	// maxSeq is the highest delivered sequence.
	maxSeq scan.Seq
	// sinceSave counts prefix appends since the last successful save.
	sinceSave int
	// savedItems is the item count of the newest saved generation.
	savedItems int
	// savedGeneration is the revision of the newest saved generation.
	savedGeneration int64
	// saveErr is the unresolved save failure; nil while clean.
	saveErr error
	// finished reports that finish ran (the kernel sealed).
	finished bool
}

// pendingItem is one delivered record waiting for the prefix, or held
// back by a failure that a retry can clear.
type pendingItem struct {
	rec      scan.Record
	item     record.ItemRecord
	evidence []record.EvidenceFile
	err      error
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
		buffered: make(map[scan.Seq]*pendingItem),
		// Capture sequence numbers start at 1; the prefix extends from
		// the first delivered record on.
		nextSeq:  1,
		onChange: onChange,
	}
}

// Deliver implements scan.Deliverer.
func (d *recordDeliverer) Deliver(rec scan.Record) error {
	d.mu.Lock()
	if d.finished {
		d.mu.Unlock()
		return fmt.Errorf("app: deliver record %d of %s after finish", rec.Seq, d.recordID)
	}
	if rec.Seq < d.nextSeq {
		// Already in the document; the kernel's exactly-once delivery
		// makes this a defensive no-op, not a duplicate write.
		d.mu.Unlock()
		return nil
	}
	if rec.Seq > d.maxSeq {
		d.maxSeq = rec.Seq
	}
	p := &pendingItem{rec: rec}
	p.item, p.evidence, p.err = d.buildItemLocked(rec)
	d.buffered[rec.Seq] = p
	d.extendLocked()
	var err error
	if p.err != nil {
		// The prefix cannot reach this record; the session must know its
		// content is not being persisted.
		err = p.err
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

// extendLocked appends every buffered record the prefix now reaches.
// Callers hold mu.
func (d *recordDeliverer) extendLocked() {
	for {
		p, ok := d.buffered[d.nextSeq]
		if !ok || p.err != nil {
			return
		}
		d.items = append(d.items, p.item)
		d.evidence = append(d.evidence, p.evidence...)
		delete(d.buffered, d.nextSeq)
		d.nextSeq++
		d.sinceSave++
	}
}

// saveLocked writes the document under construction as a new checkpoint
// generation. Callers hold mu.
func (d *recordDeliverer) saveLocked() error {
	d.shell.CapturedCount = int64(len(d.items))
	doc := record.Checkpoint{
		Session:  d.shell,
		Items:    append([]record.ItemRecord(nil), d.items...),
		Evidence: append([]record.EvidenceFile(nil), d.evidence...),
	}
	gen, err := d.store.SaveCheckpoint(d.recordID, doc)
	if err != nil {
		return fmt.Errorf("app: save checkpoint of %s: %w", d.recordID, err)
	}
	d.savedGeneration = gen
	d.savedItems = len(d.items)
	d.sinceSave = 0
	log.Printf("youkai: scan %s: checkpoint generation %d saved (%d items)", d.recordID, gen, d.savedItems)
	return nil
}

// finish writes the final checkpoint after the kernel sealed: the shell
// gains its termination and end time, any still-buffered record gets one
// more persistence attempt, and the final generation is written even
// when the last batch already flushed — the termination must reach disk.
// A failure leaves the previous generation as the last valid state and
// the pending content in memory; retry clears it.
func (d *recordDeliverer) finish(final scan.Snapshot) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.finished {
		return d.saveErr
	}
	d.finished = true
	ended := nowUTC()
	d.shell.EndedAt = &ended
	term := terminationFrom(final)
	d.shell.Termination = &term
	if err := d.resolvePendingLocked(); err != nil {
		d.saveErr = err
		return err
	}
	if err := d.saveLocked(); err != nil {
		d.saveErr = err
		return err
	}
	d.saveErr = nil
	return nil
}

// retry re-attempts the unresolved save work: buffered records get their
// evidence written again, the prefix extends as far as it can and the
// document saves as a new generation. It is a no-op while clean.
func (d *recordDeliverer) retry() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.saveErr == nil {
		return nil
	}
	if err := d.resolvePendingLocked(); err != nil {
		d.saveErr = err
		return err
	}
	if err := d.saveLocked(); err != nil {
		d.saveErr = err
		return err
	}
	d.saveErr = nil
	return nil
}

// resolvePendingLocked retries the persistence work of every buffered
// record and extends the prefix. It fails when a record still cannot be
// persisted — the final document must not silently drop a delivered
// record — and reports the first stuck sequence.
func (d *recordDeliverer) resolvePendingLocked() error {
	for _, seq := range bufferedSeqs(d.buffered) {
		p := d.buffered[seq]
		if p.err == nil {
			continue
		}
		p.item, p.evidence, p.err = d.buildItemLocked(p.rec)
		if p.err != nil {
			return fmt.Errorf("app: persist record %d of %s: %w", seq, d.recordID, p.err)
		}
	}
	d.extendLocked()
	if len(d.buffered) > 0 {
		// Every delivered sequence is either in the prefix or buffered;
		// a remaining buffer means a stuck record still blocks the
		// sequence, and the document must not be finalized without it.
		return fmt.Errorf("app: record %s: %d terminal record(s) still unpersisted (first blocked sequence %d)",
			d.recordID, len(d.buffered), bufferedSeqs(d.buffered)[0])
	}
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
		pending: d.saveErr != nil || d.sinceSave > 0 || len(d.buffered) > 0,
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

// buildItemLocked persists one record's evidence and builds its item
// record. Evidence first: every image is completely written before any
// checkpoint may reference it (the store enforces the write-once, sync
// and close discipline). Callers hold mu.
func (d *recordDeliverer) buildItemLocked(rec scan.Record) (record.ItemRecord, []record.EvidenceFile, error) {
	item := record.ItemRecord{
		Sequence: int64(rec.Seq),
		// M1 persists raw observations; field quality is not evaluated
		// until M2's game adapter owns the field semantics.
		Quality: record.QualityNotEvaluated,
	}
	switch rec.State {
	case scan.StateProduced:
		item.Status = record.StatusProduced
		data, err := marshalPayload(rec.Result)
		if err != nil {
			return item, nil, fmt.Errorf("item %d: %w", rec.Seq, err)
		}
		item.Data = data
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
			return item, nil, fmt.Errorf("item %d: unknown unprocessed cause %q", rec.Seq, rec.Cause)
		}
	default:
		return item, nil, fmt.Errorf("item %d: non-terminal state %s", rec.Seq, rec.State)
	}

	observed, err := observedPayloadOf(rec)
	if err != nil {
		return item, nil, fmt.Errorf("item %d: %w", rec.Seq, err)
	}
	item.Observed = observed

	var evidence []record.EvidenceFile
	if obs, ok := rec.Observation.(Observation); ok {
		for i, crop := range obs.Lines {
			if crop.Image == nil {
				continue
			}
			var buf bytes.Buffer
			if err := png.Encode(&buf, crop.Image); err != nil {
				return item, nil, fmt.Errorf("item %d line %d: encode evidence: %w", rec.Seq, i, err)
			}
			data := buf.Bytes()
			id := evidenceID(rec.Seq, i)
			ev, err := d.store.WriteEvidence(d.recordID, id, data)
			if errors.Is(err, record.ErrEvidenceExists) {
				// A previous attempt of this same session already wrote
				// this line (evidence is write-once and the store is the
				// only writer below the data root); the bytes are
				// identical, so the descriptor is recomputed from them.
				// A mismatching leftover file fails the next save's
				// evidence check instead of passing silently.
				sum := sha256.Sum256(data)
				ev = record.EvidenceFile{
					ID:         id,
					ByteLength: int64(len(data)),
					SHA256:     hex.EncodeToString(sum[:]),
				}
			} else if err != nil {
				return item, nil, fmt.Errorf("item %d: write evidence %s: %w", rec.Seq, id, err)
			}
			item.EvidenceIDs = append(item.EvidenceIDs, id)
			evidence = append(evidence, ev)
		}
	}
	return item, evidence, nil
}

// evidenceID names one line's evidence file: the capture sequence and
// the line's index within the observation, stable across retries so a
// second attempt addresses the same file.
func evidenceID(seq scan.Seq, line int) string {
	return fmt.Sprintf("%06d-line%d", seq, line)
}

// observedPayloadOf renders the record's observation as the observed
// payload. An Observation persists its line metadata (the pixels live in
// the evidence files); any other payload — a test double, or a future
// controller's own type — is stored as its JSON encoding, if it has one.
func observedPayloadOf(rec scan.Record) (json.RawMessage, error) {
	switch obs := rec.Observation.(type) {
	case nil:
		return nil, nil
	case Observation:
		return observedPayload(obs), nil
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

// bufferedSeqs returns the buffered sequences in order.
func bufferedSeqs(buffered map[scan.Seq]*pendingItem) []scan.Seq {
	seqs := make([]scan.Seq, 0, len(buffered))
	for seq := range buffered {
		seqs = append(seqs, seq)
	}
	slices.Sort(seqs)
	return seqs
}

// nowUTC is the persistence layer's clock; timestamps are UTC throughout.
func nowUTC() time.Time {
	return time.Now().UTC()
}
