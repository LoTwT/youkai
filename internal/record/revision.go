package record

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var (
	// ErrRevisionConflict rejects a manual edit whose expected item
	// revision does not match the item's current revision, so a stale
	// editor can never overwrite a newer one.
	ErrRevisionConflict = errors.New("item revision conflict")
	// ErrItemNotFound rejects an edit aimed at a sequence the record
	// never captured.
	ErrItemNotFound = errors.New("item not found")
)

// ItemEdit is one manual revision of a saved item. The store appends it
// to the item's history and moves its current payload forward; the
// original observation — status, reason, observed payload, evidence — is
// never modified by it.
type ItemEdit struct {
	// Changes records what the revision edited, for the audit history.
	// Field names and values belong to the item payload and stay opaque
	// here; M2's internal/game/zzz owns them and the before-matches-
	// previous check that needs real types.
	Changes []FieldChange
	// Data is the item's next full payload; nil keeps the current one
	// (a bare whole-item confirmation).
	Data json.RawMessage
	// Confirmed marks the revision as a whole-item human confirmation.
	Confirmed bool
}

// SaveRevision appends one manual revision to an item of the record's
// latest checkpoint and persists the result as a new generation. It
// returns the updated item and the generation it was saved under.
//
// The edit carries the item's sequence and the revision the caller
// believes the item is at; a mismatch rejects the edit with
// ErrRevisionConflict before anything is written. Only items with a
// terminal processing outcome (produced, failed, unprocessed) can be
// revised — queued and running work belongs to the scan, and manual
// completion of a failed or unprocessed record is exactly what the
// history is for. The new revision derives the quality the contract
// requires: a whole-item confirmation becomes humanConfirmed, any other
// edit needsReview.
//
// A write failure leaves the previous generation and the in-memory base
// untouched, so the caller keeps its last valid result and the same
// expected revision still applies on retry.
func (s *Store) SaveRevision(recordID string, sequence, expectedRevision int64, edit ItemEdit) (ItemRecord, int64, error) {
	if err := checkID("record", recordID); err != nil {
		return ItemRecord{}, 0, err
	}
	if err := validateEdit(&edit); err != nil {
		return ItemRecord{}, 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureRecord(recordID); err != nil {
		return ItemRecord{}, 0, err
	}
	state := s.state(recordID)
	if state.checkpoint == nil {
		// No save happened in this process: the base is whatever the
		// disk can verify. With nothing verifiable there is nothing to
		// revise.
		rec, err := s.loadLocked(recordID, state)
		if err != nil {
			return ItemRecord{}, 0, fmt.Errorf("record: save revision of %q item %d: %w", recordID, sequence, err)
		}
		state.checkpoint = cloneCheckpoint(&rec.Checkpoint)
	}

	doc := cloneCheckpoint(state.checkpoint)
	idx := -1
	for i := range doc.Items {
		if doc.Items[i].Sequence == sequence {
			idx = i
			break
		}
	}
	if idx < 0 {
		return ItemRecord{}, 0, fmt.Errorf("item %d of %q: %w", sequence, recordID, ErrItemNotFound)
	}
	item := &doc.Items[idx]
	switch item.Status {
	case StatusQueued, StatusRunning:
		return ItemRecord{}, 0, fmt.Errorf("item %d of %q is still %s; only finished items can be revised",
			sequence, recordID, item.Status)
	}
	if item.Revision != expectedRevision {
		return ItemRecord{}, 0, fmt.Errorf("item %d of %q is at revision %d, not %d: %w",
			sequence, recordID, item.Revision, expectedRevision, ErrRevisionConflict)
	}

	entry := ItemRevision{
		Revision:  item.Revision + 1,
		SavedAt:   time.Now().UTC(),
		Changes:   append([]FieldChange(nil), edit.Changes...),
		Confirmed: edit.Confirmed,
	}
	item.Revisions = append(item.Revisions, entry)
	item.Revision = entry.Revision
	if edit.Data != nil {
		item.Data = append(json.RawMessage(nil), edit.Data...)
	}
	if edit.Confirmed {
		item.Quality = QualityHumanConfirmed
	} else {
		item.Quality = QualityNeedsReview
	}

	gen, err := s.writeGeneration(recordID, state, doc)
	if err != nil {
		return ItemRecord{}, 0, err
	}
	state.checkpoint = doc
	state.valid[gen] = true
	s.prune(recordID, state, gen)
	return cloneItem(item), gen, nil
}

// validateEdit checks the edit's own shape before anything is read or
// written: payload and change values must be JSON, and a revision may
// touch each field at most once.
func validateEdit(edit *ItemEdit) error {
	seen := make(map[string]struct{}, len(edit.Changes))
	for _, ch := range edit.Changes {
		if ch.Field == "" {
			return errors.New("record: revision change has an empty field name")
		}
		if _, dup := seen[ch.Field]; dup {
			return fmt.Errorf("record: revision change field %q appears twice", ch.Field)
		}
		seen[ch.Field] = struct{}{}
		if err := checkJSONValue("revision change before", ch.Before); err != nil {
			return err
		}
		if err := checkJSONValue("revision change after", ch.After); err != nil {
			return err
		}
	}
	if err := checkPayload("revision data", edit.Data); err != nil {
		return err
	}
	return nil
}
