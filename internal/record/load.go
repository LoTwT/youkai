package record

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"time"
)

// ErrNoValidCheckpoint is reported when a record holds no generation that
// verifies. An interrupted session with nothing recoverable is a clear
// outcome, not a guess: the caller shows that recovery is impossible
// instead of a fabricated partial state.
var ErrNoValidCheckpoint = errors.New("no verifiable checkpoint")

// errGenerationUnusable marks a generation that is definitively not a
// recoverable backup: the file or one of its evidence files is gone, or
// its content is malformed or fails verification. An error without this
// marker is an undecided I/O failure; callers that decide whether evidence
// may be deleted must not treat it as "no backup".
var errGenerationUnusable = errors.New("checkpoint generation is unusable")

// unusableGeneration wraps one definite failure so callers can tell it
// apart from an undecided I/O error without losing the cause.
func unusableGeneration(err error) error {
	return fmt.Errorf("%w: %w", errGenerationUnusable, err)
}

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
			// verified. A generation this process once recorded as
			// valid can fail verification later (a lost unsynced write,
			// external damage); drop the stale verdict so retention
			// never keeps it in preference to one that still verifies.
			delete(state.valid, g.revision)
			continue
		}
		state.valid[g.revision] = true
		return cp, nil
	}
	return nil, fmt.Errorf("record: load %q: %w after %d generation(s)",
		recordID, ErrNoValidCheckpoint, len(gens))
}

// loadCheckpointForCleanupLocked returns the newest checkpoint that is
// safe to use as the base of an evidence removal. Unlike
// loadCheckpointLocked, an undecided I/O error on a newer generation
// aborts instead of falling back: a later successful read of that
// generation must not leave the cleanup writing the older result it
// already chose. Definitively unusable generations are still skipped, so
// the normal fallback contract and old-record compatibility are
// unchanged. Callers hold mu.
func (s *Store) loadCheckpointForCleanupLocked(recordID string, state *recordState) (*Checkpoint, error) {
	gens, err := s.generations(recordID)
	if err != nil {
		return nil, err
	}
	for _, g := range gens {
		cp, err := s.readGeneration(recordID, g)
		if err != nil {
			if errors.Is(err, errGenerationUnusable) {
				// Definitively not recoverable; fall back as usual.
				delete(state.valid, g.revision)
				continue
			}
			// Undecided: selecting an older result now could overwrite a
			// newer one that proves readable a moment later.
			return nil, fmt.Errorf("read checkpoint generation %d: %w", g.revision, err)
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

// LoadLatest returns only the newest generation after full verification:
// structure, revision, the session identity matching the record directory,
// and every evidence file's existence, size and digest. Unlike Load it
// never falls back to an older generation, so a caller that must prove the
// latest saved facts (the partial-rescan source check) cannot silently act
// on stale data. Load and Peek keep their historical fallback for the
// history list and recovery. The context is checked between file reads and
// between evidence entries; a single native filesystem read that has
// already entered the OS cannot be interrupted.
func (s *Store) LoadLatest(ctx context.Context, recordID string) (*Recovery, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := checkID("record", recordID); err != nil {
		return nil, err
	}
	if err := lockLoadLatest(ctx, &s.mu); err != nil {
		return nil, err
	}
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.ensureRecord(recordID); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, err
	}
	// The directory Stat above can be the last filesystem operation before
	// ReadDir. A cancellation that arrived while it was blocked must win
	// before the next call.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	gens, err := s.generations(recordID)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(gens) == 0 {
		return nil, fmt.Errorf("record: load latest %q: %w", recordID, ErrNoValidCheckpoint)
	}
	cp, err := s.readGenerationContext(ctx, recordID, gens[0])
	if err != nil {
		return nil, fmt.Errorf("record: load latest %q generation %d: %w", recordID, gens[0].revision, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if cp.Session.SessionID != recordID {
		return nil, fmt.Errorf("record: load latest %q: checkpoint session %q does not match the record directory",
			recordID, cp.Session.SessionID)
	}
	return applyInterruptionMarks(cp), nil
}

// lockLoadLatest waits for the store's ordinary mutex with a cancellation
// path. The read still happens under that same lock; only its wait is
// cancellable. TryLock avoids a detached goroutine, and the post-acquire
// check rejects a lock/cancel race. No global timeout or lock rewrite is
// involved, and Load/Peek keep their blocking lock semantics.
func lockLoadLatest(ctx context.Context, mu *sync.Mutex) error {
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if mu.TryLock() {
			if err := ctx.Err(); err != nil {
				mu.Unlock()
				return err
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// readGeneration reads and fully verifies one generation file. Any
// failure — unreadable file, trailing bytes, unknown fields, failed
// validation, a revision that does not match its file name, missing or
// mismatched evidence — makes the generation unusable and the caller
// falls back to an older one.
func (s *Store) readGeneration(recordID string, g generation) (*Checkpoint, error) {
	return s.readGenerationContext(context.Background(), recordID, g)
}

func (s *Store) readGenerationContext(ctx context.Context, recordID string, g generation) (*Checkpoint, error) {
	cp, err := s.readGenerationDocumentContext(ctx, recordID, g)
	if err != nil {
		return nil, err
	}
	if err := s.verifyEvidenceContext(ctx, recordID, cp.Evidence); err != nil {
		return nil, err
	}
	return cp, nil
}

// readGenerationDocument decodes, validates and revision-checks one
// generation without touching its evidence files.
func (s *Store) readGenerationDocument(recordID string, g generation) (*Checkpoint, error) {
	return s.readGenerationDocumentContext(context.Background(), recordID, g)
}

func (s *Store) readGenerationDocumentContext(ctx context.Context, recordID string, g generation) (*Checkpoint, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path := filepath.Join(s.checkpointsDir(recordID), g.name)
	file, err := s.fs.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, unusableGeneration(err)
		}
		// Any other open failure is undecided: the generation may still
		// be a recoverable backup once the I/O fault clears.
		return nil, err
	}
	data, err := readAllContext(ctx, file)
	if err != nil {
		_ = file.Close()
		// A read failure or cancellation is undecided for the same reason.
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Strict decode: unknown fields and a second value after the
	// document both disqualify the file. Go's decoder matches keys
	// case-insensitively and tolerates duplicates; for this internal
	// format, only ever written by the store itself, that residual
	// looseness is corruption tolerance, not a contract — the public
	// scan.json parser will need the stricter machinery.
	// Everything below reads only the already-captured bytes, so a
	// decoding or validation failure is a definite content defect rather
	// than an undecided I/O error.
	var cp Checkpoint
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cp); err != nil {
		return nil, unusableGeneration(err)
	}
	var trailing json.RawMessage
	if err := dec.Decode(&trailing); err != io.EOF {
		return nil, unusableGeneration(errors.New("trailing content after the checkpoint document"))
	}
	if err := cp.validate(); err != nil {
		return nil, unusableGeneration(err)
	}
	if cp.SavedRevision != g.revision {
		return nil, unusableGeneration(fmt.Errorf("saved revision %d does not match file name %d", cp.SavedRevision, g.revision))
	}
	return &cp, nil
}

// evidencePathUnreadableLocked reports whether path is definitively not a
// readable regular file: missing, a directory or another special file, or
// an unresolvable symbolic-link loop. The query goes through the same
// system stat call that the later open uses, so symlinks followed through
// components such as "hop/.." resolve exactly as the reader will see them;
// lexical joining or cleaning could fold a component before its link was
// followed. Permission and transient I/O failures remain undecided errors.
// Callers hold mu.
func (s *Store) evidencePathUnreadableLocked(path string) (bool, error) {
	info, err := s.fs.Stat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return true, nil
		}
		if isUnresolvableLinkError(err) {
			return true, nil
		}
		return false, err
	}
	return !info.Mode().IsRegular(), nil
}

// isUnresolvableLinkError reports an error that proves the filesystem
// cannot resolve a symbolic-link loop. Windows reports ERROR_CANT_RESOLVE_FILENAME
// (1921) or, on some filesystems, ERROR_TOO_MANY_LINKS (1142); the syscall
// package does not export either Win32 code, so they are named here. The
// runtime guard keeps the numeric values from matching an unrelated Unix
// error.
func isUnresolvableLinkError(err error) bool {
	if errors.Is(err, syscall.ELOOP) {
		return true
	}
	if runtime.GOOS != "windows" {
		return false
	}
	const (
		errorCantResolveFilename = syscall.Errno(1921)
		errorTooManyLinks        = syscall.Errno(1142)
	)
	return errors.Is(err, errorCantResolveFilename) || errors.Is(err, errorTooManyLinks)
}

// verifyEvidence re-reads every indexed evidence file and checks both
// size and digest. Recovery is where corruption is detected: the size
// catches truncation cheaply, the digest catches a same-size rewrite.
// The cost is one pass over the evidence per load; measured sessions of
// M3 scale will tell whether digest caching is warranted.
func (s *Store) verifyEvidence(recordID string, index []EvidenceFile) error {
	return s.verifyEvidenceContext(context.Background(), recordID, index)
}

func (s *Store) verifyEvidenceContext(ctx context.Context, recordID string, index []EvidenceFile) error {
	for _, ev := range index {
		if err := ctx.Err(); err != nil {
			return err
		}
		path := s.evidencePath(recordID, ev.ID)
		unreadable, statErr := s.evidencePathUnreadableLocked(path)
		if err := ctx.Err(); err != nil {
			// The Stat above can be the last call before OpenFile; a
			// cancellation that arrived while it was blocked must win.
			return err
		}
		if statErr != nil {
			// Undecided: the file may become readable again.
			return fmt.Errorf("evidence %q: %w", ev.ID, statErr)
		}
		if unreadable {
			return unusableGeneration(fmt.Errorf("evidence %q is not a readable regular file", ev.ID))
		}
		file, err := s.fs.OpenFile(path, os.O_RDONLY, 0)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return unusableGeneration(fmt.Errorf("evidence %q: %w", ev.ID, err))
			}
			// Undecided: the file may become readable again.
			return fmt.Errorf("evidence %q: %w", ev.ID, err)
		}
		hasher := sha256.New()
		size, err := copyContext(ctx, hasher, file)
		closeErr := file.Close()
		if err != nil {
			// A read failure or cancellation is undecided, not corruption.
			return fmt.Errorf("evidence %q: read: %w", ev.ID, err)
		}
		if closeErr != nil {
			return fmt.Errorf("evidence %q: close: %w", ev.ID, closeErr)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if size != ev.ByteLength {
			return unusableGeneration(fmt.Errorf("evidence %q: %d bytes on disk, want %d", ev.ID, size, ev.ByteLength))
		}
		if hex.EncodeToString(hasher.Sum(nil)) != ev.SHA256 {
			return unusableGeneration(fmt.Errorf("evidence %q: digest mismatch", ev.ID))
		}
	}
	return nil
}

// copyContext hashes in chunks and observes cancellation between reads.
// A single Read already inside the OS may not be interruptible; callers
// still reject the result at the next boundary.
func copyContext(ctx context.Context, dst io.Writer, src io.Reader) (int64, error) {
	buf := make([]byte, 64*1024)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		n, readErr := src.Read(buf)
		if n > 0 {
			written, writeErr := dst.Write(buf[:n])
			total += int64(written)
			if writeErr != nil {
				return total, writeErr
			}
			if written != n {
				return total, io.ErrShortWrite
			}
		}
		if readErr == io.EOF {
			if err := ctx.Err(); err != nil {
				return total, err
			}
			return total, nil
		}
		if readErr != nil {
			return total, readErr
		}
	}
}

func readAllContext(ctx context.Context, src io.Reader) ([]byte, error) {
	var buf bytes.Buffer
	if _, err := copyContext(ctx, &buf, src); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
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
