package record

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ErrNoValidCheckpoint is reported when a record holds no generation that
// verifies. An interrupted session with nothing recoverable is a clear
// outcome, not a guess: the caller shows that recovery is impossible
// instead of a fabricated partial state.
var ErrNoValidCheckpoint = errors.New("no verifiable checkpoint")

// Recovery is the outcome of loading a record's newest verifiable
// checkpoint.
type Recovery struct {
	// Checkpoint is the loaded snapshot with interruption marks applied:
	// entries the crash left queued or running come back as unprocessed
	// with reason interrupted. The marks live in memory until the next
	// save persists them; a load never rewrites anything on disk.
	Checkpoint Checkpoint
	// Interrupted lists, in sequence order, the capture sequences the
	// recovery marked interrupted.
	Interrupted []int64
	// CoverageDowngraded reports that the stored checkpoint claimed
	// complete coverage but the recovery could not uphold it and
	// downgraded it to partial. Recovery never upgrades a conclusion.
	CoverageDowngraded bool
}

// Load returns the newest checkpoint generation of a record that
// verifies: the document decodes strictly (unknown fields and trailing
// content rejected), passes the same structural validation a save
// applies, carries the revision its file name promises, and every
// indexed evidence file exists with the recorded size and digest.
// Generations that fail any of this are skipped in favour of the next
// older one.
func (s *Store) Load(recordID string) (*Recovery, error) {
	if err := checkID("record", recordID); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureRecord(recordID); err != nil {
		return nil, err
	}
	state := s.state(recordID)
	rec, err := s.loadLocked(recordID, state)
	if err != nil {
		return nil, err
	}
	// The memo gets its own copy: the caller may keep and mutate the
	// returned recovery, the store's base for later revisions stays
	// untouched.
	state.checkpoint = cloneCheckpoint(&rec.Checkpoint)
	return rec, nil
}

// loadLocked is Load's body without the memo bookkeeping, so the
// revision path can reuse it. Callers hold mu.
func (s *Store) loadLocked(recordID string, state *recordState) (*Recovery, error) {
	cp, err := s.loadCheckpointLocked(recordID, state)
	if err != nil {
		return nil, err
	}
	return applyInterruptionMarks(cp), nil
}

// loadCheckpointLocked returns the newest verifiable checkpoint without
// the recovery view's interruption marks, so operations that rewrite the
// stored document (screenshot removal, for one) preserve the states as
// they were persisted. Callers hold mu.
func (s *Store) loadCheckpointLocked(recordID string, state *recordState) (*Checkpoint, error) {
	gens, err := s.generations(recordID)
	if err != nil {
		return nil, err
	}
	for _, g := range gens {
		cp, err := s.readGeneration(recordID, g)
		if err != nil {
			// Unusable: fall back to the previous generation. The
			// reason is not surfaced per generation — the outcome the
			// caller branches on is which generation, if any,
			// verified.
			continue
		}
		state.valid[g.revision] = true
		return cp, nil
	}
	return nil, fmt.Errorf("record: load %q: %w after %d generation(s)",
		recordID, ErrNoValidCheckpoint, len(gens))
}

// Peek returns the newest structurally valid checkpoint without verifying
// its indexed evidence. It is the history list's metadata path: reading
// every evidence digest just to show a date, client and count would make
// the list as expensive as opening the record. Recovery still uses Load
// and full evidence verification.
func (s *Store) Peek(recordID string) (*Checkpoint, error) {
	if err := checkID("record", recordID); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureRecord(recordID); err != nil {
		return nil, err
	}
	gens, err := s.generations(recordID)
	if err != nil {
		return nil, err
	}
	for _, g := range gens {
		cp, err := s.readGenerationDocument(recordID, g)
		if err != nil {
			continue
		}
		return cp, nil
	}
	return nil, fmt.Errorf("record: peek %q: %w after %d generation(s)",
		recordID, ErrNoValidCheckpoint, len(gens))
}

// readGeneration reads and fully verifies one generation file. Any
// failure — unreadable file, trailing bytes, unknown fields, failed
// validation, a revision that does not match its file name, missing or
// mismatched evidence — makes the generation unusable and the caller
// falls back to an older one.
func (s *Store) readGeneration(recordID string, g generation) (*Checkpoint, error) {
	cp, err := s.readGenerationDocument(recordID, g)
	if err != nil {
		return nil, err
	}
	if err := s.verifyEvidence(recordID, cp.Evidence); err != nil {
		return nil, err
	}
	return cp, nil
}

// readGenerationDocument decodes, validates and revision-checks one
// generation without touching its evidence files.
func (s *Store) readGenerationDocument(recordID string, g generation) (*Checkpoint, error) {
	path := filepath.Join(s.checkpointsDir(recordID), g.name)
	file, err := s.fs.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(file)
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	// Strict decode: unknown fields and a second value after the
	// document both disqualify the file. Go's decoder matches keys
	// case-insensitively and tolerates duplicates; for this internal
	// format, only ever written by the store itself, that residual
	// looseness is corruption tolerance, not a contract — the public
	// scan.json parser will need the stricter machinery.
	var cp Checkpoint
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cp); err != nil {
		return nil, err
	}
	var trailing json.RawMessage
	if err := dec.Decode(&trailing); err != io.EOF {
		return nil, errors.New("trailing content after the checkpoint document")
	}
	if err := cp.validate(); err != nil {
		return nil, err
	}
	if cp.SavedRevision != g.revision {
		return nil, fmt.Errorf("saved revision %d does not match file name %d", cp.SavedRevision, g.revision)
	}
	return &cp, nil
}

// verifyEvidence re-reads every indexed evidence file and checks both
// size and digest. Recovery is where corruption is detected: the size
// catches truncation cheaply, the digest catches a same-size rewrite.
// The cost is one pass over the evidence per load; measured sessions of
// M3 scale will tell whether digest caching is warranted.
func (s *Store) verifyEvidence(recordID string, index []EvidenceFile) error {
	for _, ev := range index {
		file, err := s.fs.OpenFile(s.evidencePath(recordID, ev.ID), os.O_RDONLY, 0)
		if err != nil {
			return fmt.Errorf("evidence %q: %w", ev.ID, err)
		}
		hasher := sha256.New()
		size, err := io.Copy(hasher, file)
		closeErr := file.Close()
		if err != nil {
			return fmt.Errorf("evidence %q: read: %w", ev.ID, err)
		}
		if closeErr != nil {
			return fmt.Errorf("evidence %q: close: %w", ev.ID, closeErr)
		}
		if size != ev.ByteLength {
			return fmt.Errorf("evidence %q: %d bytes on disk, want %d", ev.ID, size, ev.ByteLength)
		}
		if hex.EncodeToString(hasher.Sum(nil)) != ev.SHA256 {
			return fmt.Errorf("evidence %q: digest mismatch", ev.ID)
		}
	}
	return nil
}

// applyInterruptionMarks turns the crash's unfinished bookkeeping into
// the recovery view. Entries the checkpoint left queued or running — the
// work that died with the process — become unprocessed with reason
// interrupted. A complete coverage claim cannot survive an interrupted
// session: neither unfinished entries nor a missing termination can
// uphold it, so it is downgraded to partial. Everything else is restored
// exactly as saved, and nothing is ever upgraded.
func applyInterruptionMarks(cp *Checkpoint) *Recovery {
	rec := &Recovery{Checkpoint: *cp}
	finished := cp.Session.Termination != nil
	for i := range rec.Checkpoint.Items {
		it := &rec.Checkpoint.Items[i]
		switch it.Status {
		case StatusQueued, StatusRunning:
			it.Status = StatusUnprocessed
			it.Reason = ReasonInterrupted
			rec.Interrupted = append(rec.Interrupted, it.Sequence)
		}
	}
	if !finished || len(rec.Interrupted) > 0 {
		if rec.Checkpoint.Session.Coverage.Status == CoverageComplete {
			rec.Checkpoint.Session.Coverage.Status = CoveragePartial
			rec.CoverageDowngraded = true
		}
	}
	return rec
}
