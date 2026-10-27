package record

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strconv"
	"strings"
)

// generation is one checkpoint file of a record on disk.
type generation struct {
	revision int64
	name     string
}

// generations lists a record's checkpoint generations, newest first. Only
// canonical names — the decimal revision plus ".json", no padding, no
// other suffix — on regular files count; anything else in the directory
// (a stray temporary, a directory, a symlink) is invisible to reading and
// retention alike, so the store neither trusts nor deletes what it did
// not write.
func (s *Store) generations(recordID string) ([]generation, error) {
	entries, err := s.fs.ReadDir(s.checkpointsDir(recordID))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("record: list checkpoints of %q: %w", recordID, err)
	}
	var gens []generation
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		rev, ok := generationNumber(e.Name())
		if !ok {
			continue
		}
		gens = append(gens, generation{revision: rev, name: e.Name()})
	}
	slices.SortFunc(gens, func(a, b generation) int { return cmp.Compare(b.revision, a.revision) })
	return gens, nil
}

// generationNumber parses canonical generation file names. The canonical
// spelling check also rejects padded names like "07.json", which would
// let two file names denote the same revision.
func generationNumber(name string) (int64, bool) {
	const suffix = ".json"
	if !strings.HasSuffix(name, suffix) {
		return 0, false
	}
	prefix := name[:len(name)-len(suffix)]
	n, err := strconv.ParseInt(prefix, 10, 64)
	if err != nil || n < 0 {
		return 0, false
	}
	if strconv.FormatInt(n, 10) != prefix {
		return 0, false
	}
	return n, true
}

// SaveCheckpoint persists cp as a new checkpoint generation and returns
// the revision it was saved under. The store stamps the envelope (format,
// format version, saved revision — the caller's values, if any, are
// ignored), validates the document, requires every indexed evidence file
// to be on disk and syncs any pending cited evidence before writing.
//
// Success is reported only after the generation file is completely
// written, synced and closed. A failure at any step — including an
// evidence sync — writes no generation, leaves every existing generation
// untouched and returns the error, so the record keeps its last valid state
// and the caller can retry; evidence that failed to sync stays pending.
// Retention then keeps this generation and the previous valid one (see
// prune).
func (s *Store) SaveCheckpoint(recordID string, cp Checkpoint) (int64, error) {
	if err := checkID("record", recordID); err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureRecord(recordID); err != nil {
		return 0, err
	}
	state := s.state(recordID)
	doc := cloneCheckpoint(&cp)
	gen, err := s.writeGeneration(recordID, state, doc)
	if err != nil {
		return 0, err
	}
	state.checkpoint = doc
	state.valid[gen] = true
	s.prune(recordID, state, gen)
	return gen, nil
}

// CheckpointBytes reports the on-disk size of one saved generation. It is
// a diagnostic read: callers log an unavailable size rather than treat it
// as a failed save, so a missing or unreadable generation is reported as
// an error here and nowhere else.
func (s *Store) CheckpointBytes(recordID string, revision int64) (int64, error) {
	if err := checkID("record", recordID); err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	info, err := s.fs.Stat(s.generationPath(recordID, revision))
	if err != nil {
		return 0, fmt.Errorf("record: stat checkpoint generation %d of %q: %w", revision, recordID, err)
	}
	return info.Size(), nil
}

// writeGeneration stamps, validates and writes doc as the next
// generation, whose revision is one above the highest generation file on
// disk. Evidence the document cites is checked on disk and any pending
// files it names are synced before the generation file is opened; a failure
// at either step writes nothing and leaves the evidence pending for a
// retry. Writing goes directly to the new file — os.Rename is not atomic
// on Windows, so nothing is renamed over an existing generation and no
// valid file is ever truncated in place; a retry after a failed write
// simply produces the next revision. Callers hold mu.
func (s *Store) writeGeneration(recordID string, state *recordState, doc *Checkpoint) (int64, error) {
	gens, err := s.generations(recordID)
	if err != nil {
		return 0, err
	}
	next := int64(1)
	if len(gens) > 0 {
		next = gens[0].revision + 1
	}
	doc.Format = FormatName
	doc.FormatVersion = FormatVersion
	doc.SavedRevision = next
	if err := doc.validate(); err != nil {
		return 0, fmt.Errorf("record: save checkpoint of %q: %w", recordID, err)
	}
	if err := s.checkEvidenceOnDisk(recordID, doc.Evidence); err != nil {
		return 0, fmt.Errorf("record: save checkpoint of %q: %w", recordID, err)
	}
	if err := s.syncEvidenceLocked(recordID, state, doc.Evidence); err != nil {
		return 0, fmt.Errorf("record: save checkpoint of %q: %w", recordID, err)
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return 0, fmt.Errorf("record: save checkpoint of %q: encode: %w", recordID, err)
	}
	data = append(data, '\n')
	path := s.generationPath(recordID, next)
	file, err := s.fs.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return 0, fmt.Errorf("record: save checkpoint of %q: create generation %d: %w", recordID, next, err)
	}
	if err := writeAll(file, data); err != nil {
		err = s.discard(file, path, err)
		return 0, fmt.Errorf("record: save checkpoint of %q: write generation %d: %w", recordID, next, err)
	}
	if err := file.Sync(); err != nil {
		err = s.discard(file, path, err)
		return 0, fmt.Errorf("record: save checkpoint of %q: sync generation %d: %w", recordID, next, err)
	}
	if err := file.Close(); err != nil {
		// os.File.Close releases the handle even when it errors, so
		// removePartial can delete the file; if that also fails, the
		// survivor is named in the returned error.
		err = s.removePartial(path, err)
		return 0, fmt.Errorf("record: save checkpoint of %q: close generation %d: %w", recordID, next, err)
	}
	return next, nil
}

// checkEvidenceOnDisk verifies that every indexed evidence file exists
// with its declared size. Digests were computed when the store itself
// wrote the bytes, so a save trusts them; recovery re-reads and re-hashes,
// which is where corruption is caught — a same-size rewrite between the
// two is outside the single-writer model and can only ever surface as a
// recovery failure, never as a silent one.
func (s *Store) checkEvidenceOnDisk(recordID string, index []EvidenceFile) error {
	for _, ev := range index {
		info, err := s.fs.Stat(s.evidencePath(recordID, ev.ID))
		if err != nil {
			return fmt.Errorf("evidence %q is not on disk: %w", ev.ID, err)
		}
		if info.Size() != ev.ByteLength {
			return fmt.Errorf("evidence %q is %d bytes on disk, not the declared %d", ev.ID, info.Size(), ev.ByteLength)
		}
	}
	return nil
}

// prune enforces generation retention: keep the current generation and
// the newest older generation this process knows to be valid, delete the
// rest.
//
// Validity is only claimed for generations this process wrote completely
// or loaded with full verification. Generations below the current one
// that no verdict exists for are never deleted: after a restart the store
// cannot know which older file is the good one, so it keeps them all
// until a load or a save of its own establishes validity. Deleting more
// than required is therefore impossible; keeping more is allowed and
// temporary. Removal errors are ignored — an extra generation is
// harmless, and failing a completed save over housekeeping would trade a
// saved result for tidiness.
func (s *Store) prune(recordID string, state *recordState, current int64) {
	gens, err := s.generations(recordID)
	if err != nil {
		return
	}
	keep := current
	for _, g := range gens {
		if g.revision < current && state.valid[g.revision] {
			keep = g.revision
			break
		}
	}
	if keep == current {
		// No verified older generation: nothing below may be deleted.
		return
	}
	for _, g := range gens {
		if g.revision < current && g.revision != keep {
			_ = s.fs.Remove(s.generationPath(recordID, g.revision))
		}
	}
}
