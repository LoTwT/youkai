package record

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
)

// RecordUsage is one record's disk footprint below sessions/.
type RecordUsage struct {
	// TotalBytes is every regular file in the record directory.
	TotalBytes int64
	// EvidenceBytes and EvidenceFiles describe the evidence directory,
	// the part deleting the screenshots reclaims.
	EvidenceBytes int64
	EvidenceFiles int
}

// StoreUsage aggregates the records below one data root.
type StoreUsage struct {
	Records       int
	TotalBytes    int64
	EvidenceBytes int64
	EvidenceFiles int
}

// RecordFailure is one cleanup target that could not be fully handled.
// The failed entry survives on disk for a retry; BulkResults report it
// alongside the entries that succeeded.
type RecordFailure struct {
	RecordID string
	Err      error
}

func (f RecordFailure) Error() string {
	return fmt.Sprintf("record %q: %v", f.RecordID, f.Err)
}

func (f RecordFailure) Unwrap() error { return f.Err }

// EvidenceRemoval is the outcome of deleting one record's screenshots.
// Generation is the new checkpoint generation that no longer references
// them, or 0 when the record already held no references and only
// leftover files were removed.
type EvidenceRemoval struct {
	RecordID      string
	Generation    int64
	EvidenceFiles int
	EvidenceBytes int64
	RemovedFiles  int
	RemovedBytes  int64
	// Leftovers lists the evidence ids whose files could not be removed.
	// The record itself is still loadable: the saved generation no
	// longer references them, so a later retry only has to delete the
	// files.
	Leftovers []string
}

// EvidenceBatch is the outcome of deleting screenshots of every record.
type EvidenceBatch struct {
	Records       int
	EvidenceFiles int
	EvidenceBytes int64
	RemovedFiles  int
	RemovedBytes  int64
	Results       []EvidenceRemoval
	Failed        []RecordFailure
}

// RecordDeletion is the outcome of deleting one record directory.
type RecordDeletion struct {
	RecordID string
	// Bytes is the record's measured footprint when it could be read.
	Bytes int64
}

// RecordDeletionBatch is the outcome of deleting every record.
type RecordDeletionBatch struct {
	Records int
	Bytes   int64
	Deleted []RecordDeletion
	Failed  []RecordFailure
}

// Usage measures one record's footprint. It does not verify checkpoint
// contents; listing and previews use it to estimate what cleanup
// reclaims.
func (s *Store) Usage(recordID string) (RecordUsage, error) {
	if err := checkID("record", recordID); err != nil {
		return RecordUsage{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.realRecordDir(recordID); err != nil {
		return RecordUsage{}, err
	}
	return s.usageLocked(recordID)
}

// StoreUsage aggregates every valid record directory's footprint.
func (s *Store) StoreUsage() (StoreUsage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	infos, err := s.listRecordsLocked()
	if err != nil {
		return StoreUsage{}, err
	}
	var usage StoreUsage
	usage.Records = len(infos)
	for _, info := range infos {
		usage.TotalBytes += info.TotalBytes
		usage.EvidenceBytes += info.EvidenceBytes
		usage.EvidenceFiles += info.EvidenceFiles
	}
	return usage, nil
}

// usageLocked measures one record. Callers hold mu.
func (s *Store) usageLocked(recordID string) (RecordUsage, error) {
	total, _, err := s.dirFootprint(s.recordDir(recordID))
	if err != nil {
		return RecordUsage{}, err
	}
	var evidence int64
	var files int
	evidenceDir := s.evidenceDir(recordID)
	info, lerr := s.fs.Lstat(evidenceDir)
	switch {
	case lerr == nil && info.Mode()&fs.ModeSymlink != 0:
		// The evidence directory is a link: never follow it to measure
		// or reclaim anything outside the record.
	case lerr == nil:
		evidence, files, err = s.dirFootprint(evidenceDir)
		if err != nil {
			return RecordUsage{}, err
		}
	case errors.Is(lerr, fs.ErrNotExist):
		// Already cleaned.
	default:
		return RecordUsage{}, lerr
	}
	return RecordUsage{TotalBytes: total, EvidenceBytes: evidence, EvidenceFiles: files}, nil
}

// dirFootprint sums the regular files below dir without following
// symbolic links. A missing directory is an empty one: records created
// before the layout existed and already-cleaned evidence directories are
// both legitimate states.
func (s *Store) dirFootprint(dir string) (bytes int64, files int, err error) {
	entries, err := s.fs.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, 0, nil
		}
		return 0, 0, err
	}
	for _, e := range entries {
		if e.Type()&fs.ModeSymlink != 0 {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return 0, 0, err
		}
		if info.IsDir() {
			subBytes, subFiles, err := s.dirFootprint(filepath.Join(dir, e.Name()))
			if err != nil {
				return 0, 0, err
			}
			bytes += subBytes
			files += subFiles
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		bytes += info.Size()
		files++
	}
	return bytes, files, nil
}

// DeleteEvidence removes one record's screenshots while keeping its
// results, revisions and review state. It first loads the newest
// verifiable checkpoint, strips every evidence reference — item
// evidence, coverage evidence and the index — and writes that document as
// a new generation. Only after that save succeeds does it delete the
// files, so a failed checkpoint write loses nothing. A failed file
// removal leaves an unreferenced leftover: the record still loads and a
// retry removes the remaining files. Callers must not delete the
// screenshots of an active session; the application service enforces
// that.
func (s *Store) DeleteEvidence(recordID string) (EvidenceRemoval, error) {
	if err := checkID("record", recordID); err != nil {
		return EvidenceRemoval{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureRecord(recordID); err != nil {
		return EvidenceRemoval{}, err
	}
	return s.deleteEvidenceLocked(recordID)
}

// DeleteAllEvidence removes the screenshots of every record. One failed
// record never stops the others: the batch reports each failure and the
// entries that could not be cleaned stay available for a retry.
func (s *Store) DeleteAllEvidence() (EvidenceBatch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	infos, err := s.listRecordsLocked()
	if err != nil {
		return EvidenceBatch{}, err
	}
	batch := EvidenceBatch{Records: len(infos)}
	for _, info := range infos {
		removal, err := s.deleteEvidenceLocked(info.ID)
		batch.Results = append(batch.Results, removal)
		batch.EvidenceFiles += removal.EvidenceFiles
		batch.EvidenceBytes += removal.EvidenceBytes
		batch.RemovedFiles += removal.RemovedFiles
		batch.RemovedBytes += removal.RemovedBytes
		if err != nil {
			batch.Failed = append(batch.Failed, RecordFailure{RecordID: info.ID, Err: err})
		}
	}
	return batch, failuresError(batch.Failed)
}

// deleteEvidenceLocked is DeleteEvidence's body. Callers hold mu.
func (s *Store) deleteEvidenceLocked(recordID string) (EvidenceRemoval, error) {
	removal := EvidenceRemoval{RecordID: recordID}
	if _, err := s.realRecordDir(recordID); err != nil {
		return removal, err
	}
	state := s.state(recordID)
	cp, err := s.loadCheckpointLocked(recordID, state)
	if err != nil {
		return removal, fmt.Errorf("record: delete screenshots of %q: %w", recordID, err)
	}
	usage, err := s.usageLocked(recordID)
	if err != nil {
		return removal, fmt.Errorf("record: delete screenshots of %q: measure evidence: %w", recordID, err)
	}
	removal.EvidenceFiles = usage.EvidenceFiles
	removal.EvidenceBytes = usage.EvidenceBytes

	if len(cp.Evidence) > 0 {
		doc := cloneCheckpoint(cp)
		doc.Session.CoverageEvidenceIDs = nil
		for i := range doc.Items {
			doc.Items[i].EvidenceIDs = nil
		}
		// The index must describe exactly the referenced set; with every
		// reference gone it is empty. Filtering (instead of assigning
		// nil) keeps the invariant explicit and would preserve any
		// future reference source.
		referenced := doc.referencedEvidence()
		index := make([]EvidenceFile, 0, len(referenced))
		for _, ev := range doc.Evidence {
			if _, ok := referenced[ev.ID]; ok {
				index = append(index, ev)
			}
		}
		doc.Evidence = index
		gen, err := s.writeGeneration(recordID, state, doc)
		if err != nil {
			return removal, fmt.Errorf("record: delete screenshots of %q: save checkpoint: %w", recordID, err)
		}
		state.checkpoint = doc
		state.valid[gen] = true
		removal.Generation = gen
		s.prune(recordID, state, gen)
	}

	removed, removedBytes, leftovers, err := s.removeEvidenceFilesLocked(recordID)
	removal.RemovedFiles = removed
	removal.RemovedBytes = removedBytes
	removal.Leftovers = leftovers
	if err != nil {
		return removal, fmt.Errorf("record: delete screenshots of %q: %w", recordID, err)
	}
	// The measured total is the bytes actually reclaimed; when files
	// vanished between the load and the scan they are simply not
	// counted.
	return removal, nil
}

// removeEvidenceFilesLocked deletes every regular file below the
// record's evidence directory without following symbolic links, then
// removes the empty directory. It reports the surviving file ids for a
// retry. Callers hold mu.
func (s *Store) removeEvidenceFilesLocked(recordID string) (removed int, removedBytes int64, leftovers []string, err error) {
	dir := s.evidenceDir(recordID)
	info, err := s.fs.Lstat(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, 0, nil, nil
		}
		return 0, 0, nil, fmt.Errorf("stat evidence directory: %w", err)
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return 0, 0, nil, errors.New("the evidence directory is a symbolic link; refusing to follow it")
	}
	if !info.IsDir() {
		return 0, 0, nil, errors.New("the evidence path is not a directory")
	}
	entries, err := s.fs.ReadDir(dir)
	var errs []error
	unknown := false
	for _, e := range entries {
		if e.Type()&fs.ModeSymlink != 0 || e.IsDir() {
			// Not a file the store wrote (or one that would need
			// following to inspect): leave it and keep the directory.
			unknown = true
			continue
		}
		if !e.Type().IsRegular() {
			unknown = true
			continue
		}
		info, infoErr := e.Info()
		if infoErr != nil {
			errs = append(errs, fmt.Errorf("stat evidence %q: %w", e.Name(), infoErr))
			leftovers = append(leftovers, strings.TrimSuffix(e.Name(), ".png"))
			continue
		}
		path := filepath.Join(dir, e.Name())
		if rmErr := s.fs.Remove(path); rmErr != nil {
			if errors.Is(rmErr, fs.ErrNotExist) {
				continue
			}
			errs = append(errs, fmt.Errorf("remove evidence %q: %w", e.Name(), rmErr))
			leftovers = append(leftovers, strings.TrimSuffix(e.Name(), ".png"))
			continue
		}
		removed++
		removedBytes += info.Size()
	}
	if len(errs) == 0 && !unknown {
		if rmErr := s.fs.Remove(dir); rmErr != nil && !errors.Is(rmErr, fs.ErrNotExist) {
			errs = append(errs, fmt.Errorf("remove evidence directory: %w", rmErr))
		}
	}
	return removed, removedBytes, leftovers, errors.Join(errs...)
}

// DeleteRecord permanently removes one record directory. The id must be
// a canonical record id, the resolved directory must stay directly below
// sessions/ and a symbolic link is refused instead of followed. A
// failure leaves the entry in place for a retry.
func (s *Store) DeleteRecord(recordID string) (RecordDeletion, error) {
	if err := checkID("record", recordID); err != nil {
		return RecordDeletion{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	dir, err := s.realRecordDir(recordID)
	if err != nil {
		return RecordDeletion{}, err
	}
	usage, _ := s.usageLocked(recordID)
	if err := s.fs.RemoveAll(dir); err != nil {
		return RecordDeletion{RecordID: recordID, Bytes: usage.TotalBytes}, fmt.Errorf("record: delete %q: %w", recordID, err)
	}
	delete(s.records, recordID)
	// A footprint that could not be measured is reported as zero bytes,
	// not as a failure: the directory is gone, so a retry would only
	// report "not found". The measured total is an estimate refreshed by
	// the caller's next usage read.
	return RecordDeletion{RecordID: recordID, Bytes: usage.TotalBytes}, nil
}

// DeleteAllRecords permanently removes every valid record directory. It
// reports failures per record and keeps deleting the others; a failed
// directory stays in place for a retry. Entries that are not canonical
// record directories — files, foreign names, symbolic links — are never
// touched.
func (s *Store) DeleteAllRecords() (RecordDeletionBatch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.fs.ReadDir(s.recordDirParent())
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return RecordDeletionBatch{}, nil
		}
		return RecordDeletionBatch{}, fmt.Errorf("record: delete all: list records: %w", err)
	}
	var batch RecordDeletionBatch
	for _, e := range entries {
		if !validID(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			batch.Failed = append(batch.Failed, RecordFailure{RecordID: e.Name(), Err: err})
			continue
		}
		if info.Mode()&fs.ModeSymlink != 0 || !info.IsDir() {
			// Not a record the store wrote; never follow or remove it.
			continue
		}
		dir := filepath.Join(s.recordDirParent(), e.Name())
		usage, _ := s.usageLocked(e.Name())
		if err := s.fs.RemoveAll(dir); err != nil {
			batch.Failed = append(batch.Failed, RecordFailure{RecordID: e.Name(), Err: err})
			continue
		}
		delete(s.records, e.Name())
		batch.Records++
		batch.Bytes += usage.TotalBytes
		batch.Deleted = append(batch.Deleted, RecordDeletion{RecordID: e.Name(), Bytes: usage.TotalBytes})
	}
	return batch, failuresError(batch.Failed)
}

// realRecordDir resolves a record id and requires the directory itself to
// be a real directory, not a symbolic link. Cleanup writes and deletes
// only below such a directory. Callers hold mu.
func (s *Store) realRecordDir(recordID string) (string, error) {
	dir, err := s.checkedRecordDir(recordID)
	if err != nil {
		return "", err
	}
	info, err := s.fs.Lstat(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("record %q: %w", recordID, ErrRecordNotFound)
		}
		return "", fmt.Errorf("record %q: stat: %w", recordID, err)
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return "", fmt.Errorf("record %q: the record directory is a symbolic link; refusing to follow it", recordID)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("record %q: the record path is not a directory", recordID)
	}
	return dir, nil
}

// checkedRecordDir resolves a record id to its directory and proves the
// result stays directly below sessions/. Ids are already restricted to
// safe components; this second check keeps the deletion path safe even
// if that rule ever changes.
func (s *Store) checkedRecordDir(recordID string) (string, error) {
	parent := s.recordDirParent()
	dir := filepath.Join(parent, recordID)
	rel, err := filepath.Rel(parent, dir)
	if err != nil || rel != recordID || filepath.IsAbs(rel) || rel == "." ||
		rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("record id %q does not resolve inside %q", recordID, parent)
	}
	return dir, nil
}

// failuresError joins per-record failures into one error for callers that
// only need to know whether anything failed. The batch itself carries the
// per-record detail.
func failuresError(failures []RecordFailure) error {
	if len(failures) == 0 {
		return nil
	}
	errs := make([]error, len(failures))
	for i := range failures {
		errs[i] = failures[i]
	}
	return errors.Join(errs...)
}
