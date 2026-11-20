package record

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

// ItemOrigin identifies an observation brought in from another session.
// Sequence on ItemRecord belongs to the destination; this sequence never
// changes when the item is copied into another record.
type ItemOrigin struct {
	SessionID string `json:"sessionId"`
	Sequence  int64  `json:"sequence"`
}

// SlotSource names the saved acquisition and its session time window.
type SlotSource struct {
	Slot          int        `json:"slot"`
	SessionID     string     `json:"sessionId"`
	SavedRevision int64      `json:"savedRevision"`
	StartedAt     time.Time  `json:"startedAt"`
	EndedAt       *time.Time `json:"endedAt,omitempty"`
}

// SlotMerge keeps the displaced observations, revisions and evidence in
// the same checkpoint that publishes the replacement. Ordinary checkpoint
// retention therefore cannot discard this archive.
type SlotMerge struct {
	BaseRevision       int64        `json:"baseRevision"`
	SupplementID       string       `json:"supplementId"`
	SupplementRevision int64        `json:"supplementRevision"`
	Sources            []SlotSource `json:"sources"`
	Replaced           []ItemRecord `json:"replaced,omitempty"`
}

// SlotSources gives each reached slot its latest acquisition source.
// Unreached slots have no acquisition time. The original session's
// revision is frozen at the first merge.
func (cp *Checkpoint) SlotSources() []SlotSource {
	ledger, ok := cp.Session.PositionConclusions()
	if !ok {
		return nil
	}
	baseRevision := cp.SavedRevision
	var sources [PositionCount + 1]SlotSource
	if len(cp.SlotMerges) != 0 {
		baseRevision = cp.SlotMerges[0].BaseRevision
		for _, merge := range cp.SlotMerges {
			for _, src := range merge.Sources {
				sources[src.Slot] = src
			}
		}
	}
	out := make([]SlotSource, 0, len(ledger.Positions))
	for _, p := range ledger.Positions {
		src := sources[p.Position]
		if src.SessionID == "" {
			src = SlotSource{
				Slot: p.Position, SessionID: cp.Session.SessionID, SavedRevision: baseRevision,
				StartedAt: cp.Session.StartedAt, EndedAt: cp.Session.EndedAt,
			}
		}
		out = append(out, src)
	}
	return out
}

func (cp *Checkpoint) validateSlotMerges() error {
	seen := make(map[int64]struct{})
	var priorSequence int64
	checkItem := func(item ItemRecord) error {
		if _, exists := seen[item.Sequence]; exists || item.Sequence < 1 {
			return fmt.Errorf("invalid or reused item sequence %d", item.Sequence)
		}
		seen[item.Sequence] = struct{}{}
		if item.Origin != nil && (!validID(item.Origin.SessionID) ||
			item.Origin.SessionID == cp.Session.SessionID || item.Origin.Sequence < 1) {
			return fmt.Errorf("invalid origin of item %d", item.Sequence)
		}
		return nil
	}
	for _, item := range cp.Items {
		if item.Sequence <= priorSequence {
			return fmt.Errorf("item sequence %d is out of order", item.Sequence)
		}
		priorSequence = item.Sequence
		if err := checkItem(item); err != nil {
			return err
		}
	}
	var priorRevision int64
	slots := make(map[int]struct{})
	for _, merge := range cp.SlotMerges {
		if merge.BaseRevision <= priorRevision || merge.BaseRevision >= cp.SavedRevision ||
			!validID(merge.SupplementID) || merge.SupplementID == cp.Session.SessionID ||
			merge.SupplementRevision < 1 || len(merge.Sources) == 0 {
			return errors.New("invalid slot merge revision or supplement")
		}
		priorRevision = merge.BaseRevision
		var priorSlot int
		for _, src := range merge.Sources {
			if _, exists := slots[src.Slot]; exists {
				return fmt.Errorf("slot %d was replaced more than once", src.Slot)
			}
			if src.Slot <= priorSlot || src.Slot > PositionCount || !validID(src.SessionID) ||
				src.SessionID == cp.Session.SessionID || src.SavedRevision < 1 || src.StartedAt.IsZero() ||
				(src.EndedAt != nil && src.EndedAt.IsZero()) {
				return fmt.Errorf("invalid source for slot %d", src.Slot)
			}
			priorSlot = src.Slot
			slots[src.Slot] = struct{}{}
		}
		for _, it := range merge.Replaced {
			if err := it.validate(); err != nil {
				return fmt.Errorf("archived item %d: %w", it.Sequence, err)
			}
			if err := checkItem(it); err != nil {
				return err
			}
		}
	}
	if len(slots) != 0 {
		if cp.Session.Coverage.Status != CoveragePartial || cp.Session.Coverage.ExpectedCount != nil ||
			cp.Session.PositionLedger == nil || cp.Session.PositionLedger.Sort != SortVerificationUnknown {
			return errors.New("merged record has unsupported inventory-wide coverage")
		}
		for _, slot := range cp.Session.PositionLedger.Positions {
			if slot.Complete {
				delete(slots, slot.Position)
			}
		}
		if len(slots) != 0 {
			return errors.New("merged slots are missing from the completed position ledger")
		}
	}
	return nil
}

// SlotMergeCommit binds the composed checkpoint to the two revisions the
// user previewed. EvidenceCopies maps new destination ids to supplement ids.
type SlotMergeCommit struct {
	RecordID           string
	RecordRevision     int64
	SupplementID       string
	SupplementRevision int64
	Checkpoint         Checkpoint
	EvidenceCopies     map[string]string
}

// CommitSlotMerge publishes one new destination generation. Under the
// store lock it checks both generation numbers, stages verified evidence,
// and commits the checkpoint. It never edits the supplement.
func (s *Store) CommitSlotMerge(merge SlotMergeCommit) (revision int64, err error) {
	if err := checkID("record", merge.RecordID); err != nil {
		return 0, err
	}
	if err := checkID("supplement", merge.SupplementID); err != nil {
		return 0, err
	}
	if merge.RecordID == merge.SupplementID || merge.Checkpoint.Session.SessionID != merge.RecordID {
		return 0, errors.New("record: invalid slot merge destination")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, pair := range []struct {
		id       string
		revision int64
	}{{merge.RecordID, merge.RecordRevision}, {merge.SupplementID, merge.SupplementRevision}} {
		if _, err := s.realRecordDir(pair.id); err != nil {
			return 0, err
		}
		gens, err := s.generations(pair.id)
		if err != nil {
			return 0, err
		}
		if len(gens) == 0 || gens[0].revision != pair.revision {
			return 0, fmt.Errorf("record: %q changed after slot merge preview: %w", pair.id, ErrRevisionConflict)
		}
	}
	dirs := []string{s.checkpointsDir(merge.RecordID)}
	if len(merge.EvidenceCopies) != 0 {
		dirs = append(dirs, s.evidenceDir(merge.RecordID))
	}
	for _, dir := range dirs {
		info, err := s.fs.Lstat(dir)
		if err != nil {
			return 0, err
		}
		if !info.IsDir() {
			return 0, fmt.Errorf("record: slot merge destination %q is not a real directory", dir)
		}
	}

	state := s.state(merge.RecordID)
	var created []string
	defer func() {
		if err == nil {
			return
		}
		for _, id := range created {
			err = s.removePartial(s.evidencePath(merge.RecordID, id), err)
			delete(state.pendingEvidence, id)
		}
	}()
	doc := cloneCheckpoint(&merge.Checkpoint)
	for _, ev := range doc.Evidence {
		sourceID, ok := merge.EvidenceCopies[ev.ID]
		if !ok {
			continue
		}
		if err := s.copyMergeEvidence(merge.RecordID, merge.SupplementID, sourceID, ev); err != nil {
			return 0, err
		}
		created = append(created, ev.ID)
		state.pendingEvidence[ev.ID] = struct{}{}
	}
	revision, err = s.writeGeneration(merge.RecordID, state, doc)
	if err != nil {
		return 0, err
	}
	state.checkpoint = doc
	state.valid[merge.RecordRevision] = true
	state.valid[revision] = true
	s.prune(merge.RecordID, state, revision)
	return revision, nil
}

// copyMergeEvidence writes a fresh, exclusive file and verifies the bytes
// while streaming. The normal checkpoint save flushes it before commit.
func (s *Store) copyMergeEvidence(recordID, sourceRecord, sourceID string, ev EvidenceFile) error {
	if !validID(sourceID) || !validID(ev.ID) {
		return ErrInvalidID
	}
	path := s.evidencePath(sourceRecord, sourceID)
	unreadable, err := s.evidencePathUnreadableLocked(path)
	if err != nil {
		return err
	}
	if unreadable {
		return fmt.Errorf("record: merge evidence %q is not readable", sourceID)
	}
	src, err := s.fs.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	dest := s.evidencePath(recordID, ev.ID)
	dst, err := s.fs.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return errors.Join(err, src.Close())
	}
	hash := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(dst, hash), io.LimitReader(src, ev.ByteLength+1))
	err = errors.Join(copyErr, src.Close(), dst.Close())
	if err == nil && (n != ev.ByteLength || hex.EncodeToString(hash.Sum(nil)) != ev.SHA256) {
		err = fmt.Errorf("record: merge evidence %q changed", sourceID)
	}
	if err != nil {
		return s.removePartial(dest, err)
	}
	return nil
}
