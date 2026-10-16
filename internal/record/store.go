// Package record persists scan records under the app's data root:
// evidence images, recheck resources and the checkpoint generations that
// carry a session's bookkeeping through crashes.
//
// The layout below the root is sessions/<local-record-id>/ with
// checkpoints/<saved-revision>.json, evidence/<evidence-id>.png and
// resources/<resource-id>.json. Identifiers are restricted to a
// filename-safe character set, so a record can never reach outside its
// directory.
//
// Durability rules (architecture section 5): evidence is written, synced
// and closed before a checkpoint may reference it; every save writes a new
// generation file and reports success only after a complete write, sync
// and close — os.Rename is not relied on, because it is not atomic on
// Windows; recovery loads the newest generation that verifies, falls back
// to the previous one and marks what the crash left unfinished as
// interrupted unprocessed, never upgrading the coverage conclusion. All
// writes go through one serial writer inside Store.
//
// Checkpoints are an internal, versioned format, deliberately not the
// public scan.json contract: they carry running state (queued and running
// entries) that public files never contain. Drive-disc field payloads
// stay opaque json.RawMessage here; M2's internal/game/zzz owns their
// types.
package record

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strconv"
	"sync"
)

// Store persists scan records below one data root. The caller hands in
// the root (the app resolves it to the per-user data directory); the store
// owns everything below <root>/sessions.
//
// mu is the serial writer: every file write, generation change and
// per-record state update happens while it is held, so callers may push
// saves from any goroutine without locking of their own. Operations are
// synchronous and take no context: saving has its own finalizing
// lifecycle and must not inherit a cancelled scan context (architecture
// section 5).
type Store struct {
	fs   FS
	root string

	mu      sync.Mutex
	records map[string]*recordState
}

// recordState is what the store remembers about one record while the
// process lives. It caches knowledge, never truth: everything on it is
// derivable from the record directory, and a fresh Store starts empty.
type recordState struct {
	// checkpoint memoizes the highest generation this process wrote or
	// loaded, with recovery marks applied — the base a revision edit
	// appends to. Nil until the first save or load.
	checkpoint *Checkpoint
	// valid lists the generations this process verified: its own
	// completed writes and fully loaded generations. Retention consults
	// it to decide which older generation is worth keeping.
	valid map[int64]bool
}

var (
	// ErrRecordNotFound is reported for operations on a record that was
	// never created under this root.
	ErrRecordNotFound = errors.New("record not found")
	// ErrRecordExists is reported when creating a record whose
	// directory already exists.
	ErrRecordExists = errors.New("record already exists")
)

// Open returns a store persisting below root, which must be absolute so
// that a broken directory lookup fails here instead of the app writing
// relative to whatever the working directory happens to be. The root is
// created by whoever writes first, matching the app's path conventions.
func Open(root string) (*Store, error) {
	return newStore(root, osFS{})
}

func newStore(root string, fsys FS) (*Store, error) {
	if root == "" {
		return nil, errors.New("record: open store: data root is empty")
	}
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("record: open store: data root %q is not absolute", root)
	}
	return &Store{fs: fsys, root: root, records: make(map[string]*recordState)}, nil
}

func (s *Store) recordDir(id string) string {
	return filepath.Join(s.root, "sessions", id)
}

func (s *Store) checkpointsDir(id string) string {
	return filepath.Join(s.recordDir(id), "checkpoints")
}

func (s *Store) evidenceDir(id string) string {
	return filepath.Join(s.recordDir(id), "evidence")
}

func (s *Store) resourcesDir(id string) string {
	return filepath.Join(s.recordDir(id), "resources")
}

func (s *Store) evidencePath(id, evidenceID string) string {
	return filepath.Join(s.evidenceDir(id), evidenceID+".png")
}

func (s *Store) resourcePath(id, resourceID string) string {
	return filepath.Join(s.resourcesDir(id), resourceID+".json")
}

func (s *Store) generationPath(id string, revision int64) string {
	return filepath.Join(s.checkpointsDir(id), strconv.FormatInt(revision, 10)+".json")
}

// state returns the per-record state, creating it on first use. Callers
// hold mu.
func (s *Store) state(id string) *recordState {
	st, ok := s.records[id]
	if !ok {
		st = &recordState{valid: make(map[int64]bool)}
		s.records[id] = st
	}
	return st
}

// ensureRecord requires the record directory to exist, so operations on
// an id nobody created fail fast instead of quietly creating directories.
// Callers hold mu.
func (s *Store) ensureRecord(id string) error {
	info, err := s.fs.Stat(s.recordDir(id))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("record %q: %w", id, ErrRecordNotFound)
		}
		return fmt.Errorf("record: stat record %q: %w", id, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("record: record %q is not a directory", id)
	}
	return nil
}

// CreateRecord creates the directory layout of a new local record: the
// record directory with its checkpoints, evidence and resources
// subdirectories. The id must come from NewRecordID or match its rules;
// creating an existing record is refused so a record is never silently
// reused. Nothing else is written: the first checkpoint generation is the
// caller's decision.
func (s *Store) CreateRecord(id string) error {
	if err := checkID("record", id); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch err := s.ensureRecord(id); {
	case err == nil:
		return fmt.Errorf("record %q: %w", id, ErrRecordExists)
	case errors.Is(err, ErrRecordNotFound):
		// Absent; create below.
	default:
		return err
	}
	for _, dir := range []string{s.checkpointsDir(id), s.evidenceDir(id), s.resourcesDir(id)} {
		if err := s.fs.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("record: create record %q: %w", id, err)
		}
	}
	return nil
}

// writeAll writes data fully, tolerating short writes: a filesystem may
// accept fewer bytes than asked, including the fault-injected ones. A
// write that makes no progress or returns an impossible count fails
// instead of spinning.
func writeAll(file File, data []byte) error {
	for len(data) > 0 {
		n, err := file.Write(data)
		if n < 0 || n > len(data) {
			return fmt.Errorf("wrote %d of %d remaining bytes", n, len(data))
		}
		data = data[n:]
		if err != nil {
			return err
		}
		if n == 0 {
			return errors.New("write made no progress")
		}
	}
	return nil
}

// discard aborts a write that already opened its target: the handle is
// closed and the partial file removed, so a failed attempt leaves nothing
// that a later save could mistake for a complete one. Errors here are
// dropped on purpose — the failure that caused the abort is the one worth
// reporting, and a leftover partial (only possible if the removal itself
// fails) can never pass verification.
func (s *Store) discard(file File, path string) {
	_ = file.Close()
	_ = s.fs.Remove(path)
}

// cloneCheckpoint deep-copies the slices a revision edit or a memo update
// can mutate, so the store's memo never aliases a caller's document.
// RawMessage payloads are shared: nothing rewrites them in place.
func cloneCheckpoint(cp *Checkpoint) *Checkpoint {
	out := *cp
	out.Session.Resources = append([]ResourceRef(nil), cp.Session.Resources...)
	out.Session.CoverageEvidenceIDs = append([]string(nil), cp.Session.CoverageEvidenceIDs...)
	out.Items = make([]ItemRecord, len(cp.Items))
	for i, it := range cp.Items {
		it.EvidenceIDs = append([]string(nil), it.EvidenceIDs...)
		if len(it.Revisions) > 0 {
			// Copy into a fresh slice before it.Revisions is
			// replaced: ranging over a field that was just
			// overwritten ranges over the copy's zero values.
			revisions := make([]ItemRevision, len(it.Revisions))
			for j, rev := range it.Revisions {
				rev.Changes = append([]FieldChange(nil), rev.Changes...)
				revisions[j] = rev
			}
			it.Revisions = revisions
		}
		out.Items[i] = it
	}
	out.Evidence = append([]EvidenceFile(nil), cp.Evidence...)
	return &out
}

// cloneItem returns a copy of one item record whose slices no longer
// alias the store's memo.
func cloneItem(it *ItemRecord) ItemRecord {
	out := *it
	out.EvidenceIDs = append([]string(nil), it.EvidenceIDs...)
	out.Revisions = make([]ItemRevision, len(it.Revisions))
	for j, rev := range it.Revisions {
		rev.Changes = append([]FieldChange(nil), rev.Changes...)
		out.Revisions[j] = rev
	}
	return out
}
