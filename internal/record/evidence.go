package record

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
)

var (
	// ErrEvidenceExists is reported when evidence with the same id was
	// already written: evidence files are written once and never
	// truncated in place, so the only copy cannot be damaged by a
	// duplicate write.
	ErrEvidenceExists = errors.New("evidence already exists")
	// ErrResourceExists is the same rule for recheck resources.
	ErrResourceExists = errors.New("resource already exists")
)

// WriteEvidence persists one evidence image as evidence/<evidence-id>.png
// and returns the descriptor a checkpoint must cite for it. The bytes are
// completely written and closed before the descriptor is returned; the
// device flush is deferred until the next checkpoint that cites the file
// (see SaveCheckpoint), so capture pays one batch of syncs instead of one
// sync per image. Any write or close failure along the way removes the
// partial file and reports the error. The store persists the bytes as
// given — the .png layout contract belongs to the capture stack that hands
// them over.
//
// An existing evidence file is never overwritten: ids are unique per
// record, and a duplicate write fails with ErrEvidenceExists instead of
// truncating the only copy. A duplicate is still marked for syncing
// before it may be cited, because a survivor left by an earlier failed
// attempt or a previous process has no proof of durability in this one.
func (s *Store) WriteEvidence(recordID, evidenceID string, image []byte) (EvidenceFile, error) {
	if err := checkID("record", recordID); err != nil {
		return EvidenceFile{}, err
	}
	if err := checkID("evidence", evidenceID); err != nil {
		return EvidenceFile{}, err
	}
	if len(image) == 0 {
		return EvidenceFile{}, fmt.Errorf("evidence %q of %q: image is empty", evidenceID, recordID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureRecord(recordID); err != nil {
		return EvidenceFile{}, err
	}
	state := s.state(recordID)
	path := s.evidencePath(recordID, evidenceID)
	file, err := s.fs.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			state.pendingEvidence[evidenceID] = struct{}{}
			return EvidenceFile{}, fmt.Errorf("evidence %q of %q: %w", evidenceID, recordID, ErrEvidenceExists)
		}
		return EvidenceFile{}, fmt.Errorf("record: write evidence %q of %q: %w", evidenceID, recordID, err)
	}
	if err := writeAll(file, image); err != nil {
		err = s.discard(file, path, err)
		return EvidenceFile{}, fmt.Errorf("record: write evidence %q of %q: %w", evidenceID, recordID, err)
	}
	if err := file.Close(); err != nil {
		// os.File.Close releases the handle even when it errors, so
		// removePartial can delete the file; if that also fails, the
		// survivor is named in the returned error.
		err = s.removePartial(path, err)
		return EvidenceFile{}, fmt.Errorf("record: write evidence %q of %q: close: %w", evidenceID, recordID, err)
	}
	state.pendingEvidence[evidenceID] = struct{}{}
	sum := sha256.Sum256(image)
	return EvidenceFile{
		ID:         evidenceID,
		ByteLength: int64(len(image)),
		SHA256:     hex.EncodeToString(sum[:]),
	}, nil
}

// syncEvidenceLocked flushes the pending evidence cited by index before a
// checkpoint generation may reference it. File contents are synced first,
// then the evidence directory entry through SyncDir (a no-op where the
// platform exposes no reliable directory flush), so a crash cannot leave a
// checkpoint whose files are absent or incomplete. Only the cited pending
// files are synced; the rest stay pending for a later checkpoint. A failure
// leaves every cited file pending, so the caller's retry syncs them again
// instead of assuming the failed attempt made them durable. Callers hold mu.
func (s *Store) syncEvidenceLocked(recordID string, state *recordState, index []EvidenceFile) error {
	var synced []string
	for _, ev := range index {
		if _, ok := state.pendingEvidence[ev.ID]; !ok {
			continue
		}
		if err := s.syncEvidenceFile(recordID, ev.ID); err != nil {
			return fmt.Errorf("sync evidence %q: %w", ev.ID, err)
		}
		synced = append(synced, ev.ID)
	}
	if len(synced) == 0 {
		return nil
	}
	if err := s.fs.SyncDir(s.evidenceDir(recordID)); err != nil {
		return fmt.Errorf("sync evidence directory: %w", err)
	}
	for _, id := range synced {
		delete(state.pendingEvidence, id)
	}
	return nil
}

// syncEvidenceFile flushes one completed evidence file. It is reopened with
// write access because Windows' FlushFileBuffers does not accept a
// read-only handle; opening without O_TRUNC or O_CREATE leaves the bytes
// and the write-once rule untouched. A close failure is reported as well:
// the file is not confirmed durable until both steps succeed.
func (s *Store) syncEvidenceFile(recordID, evidenceID string) error {
	file, err := s.fs.OpenFile(s.evidencePath(recordID, evidenceID), os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	return errors.Join(file.Sync(), file.Close())
}

// WriteResource persists one recheck resource as
// resources/<resource-id>.json with the same write-once discipline as
// evidence: complete write and close before success, partial file removed
// on failure, existing file never truncated. Because resources are not
// cited by checkpoints, there is no later checkpoint save to defer to, so
// the bytes are synced here. Resources hold the session's recheck material
// (the capture profile and friends); they are not referenced by checkpoints
// and not verified at recovery.
func (s *Store) WriteResource(recordID, resourceID string, content []byte) error {
	if err := checkID("record", recordID); err != nil {
		return err
	}
	if err := checkID("resource", resourceID); err != nil {
		return err
	}
	if len(content) == 0 || !json.Valid(content) {
		return fmt.Errorf("resource %q of %q: content is not a JSON document", resourceID, recordID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureRecord(recordID); err != nil {
		return err
	}
	path := s.resourcePath(recordID, resourceID)
	file, err := s.fs.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("resource %q of %q: %w", resourceID, recordID, ErrResourceExists)
		}
		return fmt.Errorf("record: write resource %q of %q: %w", resourceID, recordID, err)
	}
	if err := writeAll(file, content); err != nil {
		err = s.discard(file, path, err)
		return fmt.Errorf("record: write resource %q of %q: %w", resourceID, recordID, err)
	}
	if err := file.Sync(); err != nil {
		err = s.discard(file, path, err)
		return fmt.Errorf("record: write resource %q of %q: sync: %w", resourceID, recordID, err)
	}
	if err := file.Close(); err != nil {
		err = s.removePartial(path, err)
		return fmt.Errorf("record: write resource %q of %q: close: %w", resourceID, recordID, err)
	}
	return nil
}
