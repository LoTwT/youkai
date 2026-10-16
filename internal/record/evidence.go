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
// completely written, synced and closed before the descriptor is
// returned, so a checkpoint can only reference evidence that is fully on
// disk; any failure along the way removes the partial file and reports
// the error. The store persists the bytes as given — the .png layout
// contract belongs to the capture stack that hands them over.
//
// An existing evidence file is never overwritten: ids are unique per
// record, and a duplicate write fails with ErrEvidenceExists instead of
// truncating the only copy.
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
	path := s.evidencePath(recordID, evidenceID)
	file, err := s.fs.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return EvidenceFile{}, fmt.Errorf("evidence %q of %q: %w", evidenceID, recordID, ErrEvidenceExists)
		}
		return EvidenceFile{}, fmt.Errorf("record: write evidence %q of %q: %w", evidenceID, recordID, err)
	}
	if err := writeAll(file, image); err != nil {
		err = s.discard(file, path, err)
		return EvidenceFile{}, fmt.Errorf("record: write evidence %q of %q: %w", evidenceID, recordID, err)
	}
	if err := file.Sync(); err != nil {
		err = s.discard(file, path, err)
		return EvidenceFile{}, fmt.Errorf("record: write evidence %q of %q: sync: %w", evidenceID, recordID, err)
	}
	if err := file.Close(); err != nil {
		// os.File.Close releases the handle even when it errors, so
		// removePartial can delete the file; if that also fails, the
		// survivor is named in the returned error.
		err = s.removePartial(path, err)
		return EvidenceFile{}, fmt.Errorf("record: write evidence %q of %q: close: %w", evidenceID, recordID, err)
	}
	sum := sha256.Sum256(image)
	return EvidenceFile{
		ID:         evidenceID,
		ByteLength: int64(len(image)),
		SHA256:     hex.EncodeToString(sum[:]),
	}, nil
}

// WriteResource persists one recheck resource as
// resources/<resource-id>.json with the same write-once discipline as
// evidence: complete write, sync and close before success, partial file
// removed on failure, existing file never truncated. Resources hold the
// session's recheck material (the capture profile and friends); they are
// not referenced by checkpoints and not verified at recovery.
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
