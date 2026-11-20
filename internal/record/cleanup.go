package record

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
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
// Generation is the newest checkpoint generation the removal wrote, or 0
// when no write was needed.
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
// evidence, coverage evidence and the index — and makes sure the pair
// retention keeps is reference-free and durable. Only after that does it
// delete the files, so a failed write or sync loses nothing and the
// screenshots stay until both recoverable states are confirmed. A failed
// file removal leaves an unreferenced leftover: the record still loads
// and a retry removes the remaining files. Callers must not delete the
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
	cp, err := s.loadCheckpointForCleanupLocked(recordID, state)
	if err != nil {
		return removal, fmt.Errorf("record: delete screenshots of %q: %w", recordID, err)
	}
	plan, err := s.planEvidenceRemovalLocked(recordID)
	if err != nil {
		return removal, fmt.Errorf("record: delete screenshots of %q: %w", recordID, err)
	}
	removal.EvidenceFiles = len(plan.files)
	for _, f := range plan.files {
		removal.EvidenceBytes += f.size
	}

	doc := cloneCheckpoint(cp)
	doc.Session.CoverageEvidenceIDs = nil
	for i := range doc.Items {
		doc.Items[i].EvidenceIDs = nil
	}
	for i := range doc.SlotMerges {
		for j := range doc.SlotMerges[i].Replaced {
			doc.SlotMerges[i].Replaced[j].EvidenceIDs = nil
		}
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

	gen, err := s.persistEvidenceRemovalLocked(recordID, state, cp, doc, plan)
	// Report the newest generation that did land, even when the pair could
	// not be completed: the partial state is what a retry starts from.
	removal.Generation = gen
	if err != nil {
		return removal, fmt.Errorf("record: delete screenshots of %q: %w", recordID, err)
	}

	removed, removedBytes, leftovers, err := s.removePlannedEvidenceLocked(recordID, plan)
	removal.RemovedFiles = removed
	removal.RemovedBytes = removedBytes
	removal.Leftovers = leftovers
	if err != nil {
		return removal, fmt.Errorf("record: delete screenshots of %q: %w", recordID, err)
	}
	// Every file the pending set could describe is gone, and the newest
	// checkpoint references none of them. Keeping the ids would make a
	// later save try to sync files that no longer exist.
	clear(state.pendingEvidence)
	// The measured total is the bytes actually reclaimed; when files
	// vanished between the load and the scan they are simply not
	// counted.
	return removal, nil
}

// persistEvidenceRemovalLocked persists the states that must survive the
// evidence files before the caller may delete them, and returns the newest
// generation it wrote (0 when no write was needed).
//
// The trigger is what the plan would actually destroy: a currently
// verifiable checkpoint that cites at least one file in the removal plan.
// Only then does retention need two reference-free generations, and both
// must carry the current result, so the stripped document is written
// twice. Writing both copies first — each with a complete write, sync and
// close — and pruning only afterwards means a failure at any step leaves
// the evidence-citing generation and its files in place for the next
// retry. Records with no removable files, and backups citing only
// protected entries such as symbolic links, keep the established no-op
// behavior. Callers hold mu.
func (s *Store) persistEvidenceRemovalLocked(recordID string, state *recordState, current, stripped *Checkpoint, plan evidenceRemovalPlan) (int64, error) {
	if len(plan.files) == 0 {
		return 0, nil
	}
	invalidates, err := s.evidenceRemovalInvalidatesBackupLocked(recordID, current)
	if err != nil {
		return 0, err
	}
	if !invalidates {
		return 0, nil
	}

	// Both retained generations must contain the current result. Writing
	// two identical reference-free copies avoids inferring equivalence
	// from generation numbers, adjacency or the absence of references.
	first, err := s.writeStrippedGenerationLocked(recordID, state, stripped)
	if err != nil {
		return 0, err
	}
	newest, err := s.writeStrippedGenerationLocked(recordID, state, stripped)
	if err != nil {
		return first, err
	}
	// Pruning and the caller's file removal happen only after both
	// generations completed their write, sync and close.
	s.prune(recordID, state, newest)
	return newest, nil
}

// evidenceRemovalInvalidatesBackupLocked reports whether removing the
// plan's regular evidence entries would make a currently verifiable
// checkpoint unreadable. The current checkpoint was already fully
// verified by loadCheckpointForCleanupLocked and is reused here, so a
// second open cannot turn a verified backup into a false no-op. Other
// generations are read from disk; only a definite defect lets the scan
// skip one. An undecided I/O failure is returned and blocks the cleanup.
// Callers hold mu.
func (s *Store) evidenceRemovalInvalidatesBackupLocked(recordID string, current *Checkpoint) (bool, error) {
	breaks, err := s.checkpointEvidenceBreaksLocked(recordID, current)
	if err != nil {
		return false, err
	}
	if breaks {
		return true, nil
	}
	gens, err := s.generations(recordID)
	if err != nil {
		return false, err
	}
	for _, g := range gens {
		if g.revision == current.SavedRevision {
			// Already verified; do not read the same generation again.
			continue
		}
		backup, err := s.readGeneration(recordID, g)
		if err != nil {
			if errors.Is(err, errGenerationUnusable) || errors.Is(err, fs.ErrNotExist) {
				// Definitively not a recoverable backup.
				continue
			}
			return false, fmt.Errorf("verify checkpoint generation %d: %w", g.revision, err)
		}
		breaks, err := s.checkpointEvidenceBreaksLocked(recordID, backup)
		if err != nil {
			return false, fmt.Errorf("checkpoint generation %d: %w", g.revision, err)
		}
		if breaks {
			return true, nil
		}
	}
	return false, nil
}

// checkpointEvidenceBreaksLocked reports whether the removal plan would
// make any evidence path cited by cp unreadable. Callers hold mu.
func (s *Store) checkpointEvidenceBreaksLocked(recordID string, cp *Checkpoint) (bool, error) {
	for _, ev := range cp.Evidence {
		breaks, err := s.evidenceRemovalBreaksPathLocked(recordID, ev.ID)
		if err != nil {
			return false, fmt.Errorf("evidence %q: %w", ev.ID, err)
		}
		if breaks {
			return true, nil
		}
	}
	return false, nil
}

// evidenceRemovalBreaksPathLocked asks the filesystem whether the removal
// plan makes one cited evidence path unreadable. It is only called for
// evidence of a checkpoint that was just verified as a readable regular
// file, so a missing entry, an unresolvable link, a different file type or
// any other failure to confirm that identity is undecided: the caller must
// stop before writing, pruning or deleting. A regular entry is itself in
// the plan. A symbolic link stays in place, so only a target that is a
// regular entry of the same evidence directory is removed; a link to a
// path outside the directory — including another name for the same file,
// such as a hard link — survives. Identity comes from resolving the path,
// never from trimming or case-folding its name. Callers hold mu.
func (s *Store) evidenceRemovalBreaksPathLocked(recordID, evidenceID string) (bool, error) {
	path := s.evidencePath(recordID, evidenceID)
	info, err := s.fs.Lstat(path)
	if err != nil {
		// The checkpoint was verified moments ago; a missing or unreadable
		// path now cannot prove that the removal is safe.
		return false, fmt.Errorf("stat evidence path: %w", err)
	}
	if info.Mode().IsRegular() {
		// Every regular entry in the evidence directory is removed, so
		// the cited path itself disappears.
		return true, nil
	}
	if info.Mode()&fs.ModeSymlink == 0 {
		return false, fmt.Errorf("evidence path changed to a %s, not the verified regular file", info.Mode().Type())
	}
	resolved, err := s.fs.EvalSymlinks(path)
	if err != nil {
		return false, fmt.Errorf("resolve evidence path: %w", err)
	}
	target, err := s.fs.Stat(resolved)
	if err != nil {
		return false, fmt.Errorf("stat resolved evidence: %w", err)
	}
	if !target.Mode().IsRegular() {
		return false, fmt.Errorf("resolved evidence changed to a %s, not a regular file", target.Mode().Type())
	}
	parent, err := s.fs.Stat(filepath.Dir(resolved))
	if err != nil {
		return false, fmt.Errorf("stat evidence parent directory: %w", err)
	}
	dir, err := s.fs.Stat(s.evidenceDir(recordID))
	if err != nil {
		return false, fmt.Errorf("stat evidence directory: %w", err)
	}
	return os.SameFile(parent, dir), nil
}

// writeStrippedGenerationLocked persists one copy of the reference-free
// document and records it as the current valid state. Callers hold mu.
func (s *Store) writeStrippedGenerationLocked(recordID string, state *recordState, stripped *Checkpoint) (int64, error) {
	// Each attempt gets its own copy: writeGeneration stamps the revision,
	// and a failed write must not leave the memo claiming a revision that
	// never landed.
	doc := cloneCheckpoint(stripped)
	gen, err := s.writeGeneration(recordID, state, doc)
	if err != nil {
		return 0, err
	}
	state.checkpoint = doc
	state.valid[gen] = true
	return gen, nil
}

// evidenceRemovalFile is one regular evidence file a cleanup will delete.
type evidenceRemovalFile struct {
	name string
	size int64
}

// evidenceRemovalPlan is the snapshot of what one cleanup will actually
// delete. Symbolic links, directories and other entries are left in place
// and keep the evidence directory.
type evidenceRemovalPlan struct {
	files   []evidenceRemovalFile
	unknown bool
}

// planEvidenceRemovalLocked lists the regular files a cleanup will delete
// and applies the same directory, path and symbolic-link protections as
// removal. The trigger and the deletion consume this one list, so a
// protected entry a backup cites is never counted as removed. A missing
// directory is an empty plan; an undecided listing or status failure is
// returned so nothing is deleted on incomplete information. Callers hold
// mu.
func (s *Store) planEvidenceRemovalLocked(recordID string) (evidenceRemovalPlan, error) {
	dir := s.evidenceDir(recordID)
	info, err := s.fs.Lstat(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return evidenceRemovalPlan{}, nil
		}
		return evidenceRemovalPlan{}, fmt.Errorf("stat evidence directory: %w", err)
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return evidenceRemovalPlan{}, errors.New("the evidence directory is a symbolic link; refusing to follow it")
	}
	if !info.IsDir() {
		return evidenceRemovalPlan{}, errors.New("the evidence path is not a directory")
	}
	entries, err := s.fs.ReadDir(dir)
	if err != nil {
		// Without the listing nothing can be deleted, so report the real
		// cause instead of a later "not empty".
		return evidenceRemovalPlan{}, fmt.Errorf("read evidence directory: %w", err)
	}
	var plan evidenceRemovalPlan
	for _, e := range entries {
		if e.Type()&fs.ModeSymlink != 0 || e.IsDir() || !e.Type().IsRegular() {
			// Not a file the store wrote (or one that would need
			// following to inspect): leave it and keep the directory.
			plan.unknown = true
			continue
		}
		info, err := e.Info()
		if err != nil {
			// Without a reliable status nothing is deleted: the file may
			// belong to a backup that must stay recoverable.
			return evidenceRemovalPlan{}, fmt.Errorf("stat evidence %q: %w", e.Name(), err)
		}
		plan.files = append(plan.files, evidenceRemovalFile{name: e.Name(), size: info.Size()})
	}
	return plan, nil
}

// removeEvidenceFilesLocked plans and removes one record's removable
// evidence files. Cleanup itself plans once and consumes that same list;
// this entry keeps the retry helper the tests use. Callers hold mu.
func (s *Store) removeEvidenceFilesLocked(recordID string) (removed int, removedBytes int64, leftovers []string, err error) {
	plan, err := s.planEvidenceRemovalLocked(recordID)
	if err != nil {
		return 0, 0, nil, err
	}
	return s.removePlannedEvidenceLocked(recordID, plan)
}

// removePlannedEvidenceLocked deletes the regular files of one plan and
// removes the directory when no protected entry remains. It reports the
// surviving file ids for a retry. Callers hold mu.
func (s *Store) removePlannedEvidenceLocked(recordID string, plan evidenceRemovalPlan) (removed int, removedBytes int64, leftovers []string, err error) {
	dir := s.evidenceDir(recordID)
	var errs []error
	for _, f := range plan.files {
		path := filepath.Join(dir, f.name)
		if rmErr := s.fs.Remove(path); rmErr != nil {
			if errors.Is(rmErr, fs.ErrNotExist) {
				continue
			}
			errs = append(errs, fmt.Errorf("remove evidence %q: %w", f.name, rmErr))
			leftovers = append(leftovers, strings.TrimSuffix(f.name, ".png"))
			continue
		}
		removed++
		removedBytes += f.size
	}
	if len(errs) == 0 && !plan.unknown {
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
