package app

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/LoTwT/youkai/internal/record"
)

var (
	ErrSlotMergeUnavailable = errors.New("app: no completed slot can be merged")
	ErrSlotMergeBusy        = errors.New("app: records are busy")
	ErrSlotMergeUnassigned  = errors.New("app: an observation has no verified capture position")
)

// SlotMergeChoice shows the impact of replacing one proven slot, including
// an empty but fully traversed slot.
type SlotMergeChoice struct {
	Slot             int
	PreviousCount    int
	SupplementCount  int
	PreviousReviewed int
}

type SlotMergePreview struct {
	RecordID           string
	RecordRevision     int64
	RecordStartedAt    time.Time
	SupplementID       string
	SupplementRevision int64
	Choices            []SlotMergeChoice
}

type SlotMergeRequest struct {
	RecordID           string
	RecordRevision     int64
	SupplementID       string
	SupplementRevision int64
	Slots              []int
}

// PreviewSlotMerge measures a possible merge against the latest readable
// generations. The confirmation must send back these exact revisions.
func (s *Service) PreviewSlotMerge(supplementID string) (SlotMergePreview, error) {
	s.mu.Lock()
	busy := s.slotMergeBusyLocked()
	s.mu.Unlock()
	if busy {
		return SlotMergePreview{}, ErrSlotMergeBusy
	}
	inputs, err := s.loadSlotMerge(supplementID, nil)
	if err != nil {
		return SlotMergePreview{}, err
	}
	return inputs.preview, nil
}

// MergeSlots writes a new original-record revision. A stale preview or
// failed copy cannot replace any of the two previously saved generations.
func (s *Service) MergeSlots(req SlotMergeRequest) (int64, error) {
	if err := validateScanPositions(req.Slots); err != nil {
		return 0, fmt.Errorf("%w: %v", ErrSlotMergeUnavailable, err)
	}
	// Like record cleanup, this holds the admission lock until the store
	// settles, so Start, update admission and quit cannot interleave.
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.slotMergeBusyLocked() {
		return 0, ErrSlotMergeBusy
	}
	inputs, err := s.loadSlotMerge(req.SupplementID, &req)
	if err != nil {
		return 0, err
	}
	merge, err := inputs.prepareCommit(req.Slots)
	if err != nil {
		return 0, err
	}
	revision, err := s.store.CommitSlotMerge(merge)
	if err != nil {
		return 0, err
	}
	s.notifyLocked()
	return revision, nil
}

func (s *Service) slotMergeBusyLocked() bool {
	return s.writesClosed || s.quitting || s.updateInstalling ||
		(s.active != nil && (!s.active.wrapped || s.active.saveErr != nil))
}

type slotMergeInputs struct {
	preview           SlotMergePreview
	base, supplement  *record.Checkpoint
	basePositions     []int
	incomingPositions []int
}

func (s *Service) loadSlotMerge(supplementID string, expected *SlotMergeRequest) (slotMergeInputs, error) {
	supplementRec, err := s.store.LoadLatest(context.Background(), supplementID)
	if err != nil {
		return slotMergeInputs{}, fmt.Errorf("read supplement: %w", err)
	}
	supplement := &supplementRec.Checkpoint
	plan, ok := DeclaredScanPlan(supplement.Session.Scope)
	if !ok || plan.Full() || plan.RescanOf == supplementID || supplement.Session.Termination == nil {
		return slotMergeInputs{}, ErrSlotMergeUnavailable
	}
	if expected != nil && (expected.RecordID != plan.RescanOf || expected.SupplementRevision != supplement.SavedRevision) {
		return slotMergeInputs{}, record.ErrRevisionConflict
	}
	baseRec, err := s.store.LoadLatest(context.Background(), plan.RescanOf)
	if err != nil {
		return slotMergeInputs{}, fmt.Errorf("read original record: %w", err)
	}
	base := &baseRec.Checkpoint
	if expected != nil && expected.RecordRevision != base.SavedRevision {
		return slotMergeInputs{}, record.ErrRevisionConflict
	}
	a, b := base.Session, supplement.Session
	if a.Client != record.ClientWindowsLocal || a.Game != "zzz" || a.Entity != "driveDisc" ||
		a.Game != b.Game || a.Entity != b.Entity || a.Client != b.Client || a.Locale != b.Locale {
		return slotMergeInputs{}, ErrSlotMergeUnavailable
	}
	candidates, known := RescanCandidatesOf(a)
	ledger, ledgerKnown := b.PositionConclusions()
	if !known || !ledgerKnown {
		return slotMergeInputs{}, ErrSlotMergeUnavailable
	}
	incomplete := [record.PositionCount + 1]bool{}
	for _, candidate := range candidates {
		incomplete[candidate.Position] = true
	}
	preview := SlotMergePreview{
		RecordID: a.SessionID, RecordRevision: base.SavedRevision, RecordStartedAt: a.StartedAt,
		SupplementID: b.SessionID, SupplementRevision: supplement.SavedRevision,
	}
	for _, position := range ledger.Positions {
		if !plan.Includes(position.Position) {
			return slotMergeInputs{}, ErrSlotMergeUnavailable
		}
		if position.Complete && incomplete[position.Position] {
			preview.Choices = append(preview.Choices, SlotMergeChoice{Slot: position.Position})
		}
	}
	if len(preview.Choices) == 0 {
		return slotMergeInputs{}, ErrSlotMergeUnavailable
	}
	basePlan, _ := DeclaredScanPlan(a.Scope)
	originalPositions, err := mergeItemPositions(base, basePlan)
	if err != nil {
		return slotMergeInputs{}, err
	}
	incomingPositions, err := mergeItemPositions(supplement, plan)
	if err != nil {
		return slotMergeInputs{}, err
	}
	var oldCount, reviewed, incomingCount [record.PositionCount + 1]int
	for i, slot := range originalPositions {
		oldCount[slot]++
		if base.Items[i].Revision > 0 {
			reviewed[slot]++
		}
	}
	for _, slot := range incomingPositions {
		incomingCount[slot]++
	}
	for i := range preview.Choices {
		choice := &preview.Choices[i]
		choice.PreviousCount = oldCount[choice.Slot]
		choice.PreviousReviewed = reviewed[choice.Slot]
		choice.SupplementCount = incomingCount[choice.Slot]
	}
	return slotMergeInputs{preview, base, supplement, originalPositions, incomingPositions}, nil
}

// mergeItemPositions uses the recorded wheel position, which also exists
// for failed OCR and unprocessed observations. The editable item slot
// cannot establish which traversal should replace an observation.
func mergeItemPositions(cp *record.Checkpoint, plan ScanPlan) ([]int, error) {
	out := make([]int, len(cp.Items))
	for i, item := range cp.Items {
		var observed struct {
			Capture struct {
				Position int `json:"position"`
			} `json:"capture"`
		}
		if json.Unmarshal(item.Observed, &observed) != nil ||
			observed.Capture.Position < 1 || observed.Capture.Position > record.PositionCount ||
			!plan.Includes(observed.Capture.Position) {
			return nil, fmt.Errorf("%w: %s #%d", ErrSlotMergeUnassigned, cp.Session.SessionID, item.Sequence)
		}
		out[i] = observed.Capture.Position
	}
	return out, nil
}

func (in slotMergeInputs) prepareCommit(slots []int) (record.SlotMergeCommit, error) {
	var eligible, selected [record.PositionCount + 1]bool
	for _, choice := range in.preview.Choices {
		eligible[choice.Slot] = true
	}
	for _, slot := range slots {
		if !eligible[slot] {
			return record.SlotMergeCommit{}, ErrSlotMergeUnavailable
		}
		selected[slot] = true
	}
	doc := *in.base
	doc.Items = make([]record.ItemRecord, 0, len(in.base.Items))
	doc.Evidence = append([]record.EvidenceFile(nil), in.base.Evidence...)
	archive := record.SlotMerge{
		BaseRevision: in.base.SavedRevision, SupplementID: in.supplement.Session.SessionID,
		SupplementRevision: in.supplement.SavedRevision,
	}
	for _, src := range in.supplement.SlotSources() {
		if selected[src.Slot] {
			archive.Sources = append(archive.Sources, src)
		}
	}
	var lastSequence int64
	for _, oldMerge := range in.base.SlotMerges {
		for _, item := range oldMerge.Replaced {
			lastSequence = max(lastSequence, item.Sequence)
		}
	}
	for i, item := range in.base.Items {
		lastSequence = max(lastSequence, item.Sequence)
		if selected[in.basePositions[i]] {
			archive.Replaced = append(archive.Replaced, item)
		} else {
			doc.Items = append(doc.Items, item)
		}
	}
	doc.SlotMerges = append(append([]record.SlotMerge(nil), in.base.SlotMerges...), archive)
	copies := make(map[string]string)
	index := make(map[string]record.EvidenceFile, len(in.supplement.Evidence))
	for _, ev := range in.supplement.Evidence {
		index[ev.ID] = ev
	}
	prefix := "merge-" + strings.ToLower(rand.Text()) + "-"
	for i, item := range in.supplement.Items {
		if !selected[in.incomingPositions[i]] {
			continue
		}
		if lastSequence == math.MaxInt64 {
			return record.SlotMergeCommit{}, errors.New("item sequence is exhausted")
		}
		lastSequence++
		oldSequence := item.Sequence
		if item.Origin == nil {
			item.Origin = &record.ItemOrigin{SessionID: in.supplement.Session.SessionID, Sequence: oldSequence}
		}
		item.Sequence = lastSequence
		item.EvidenceIDs = append([]string(nil), item.EvidenceIDs...)
		for j, oldID := range item.EvidenceIDs {
			crop, ok := strings.CutPrefix(MergeEvidenceCropName(oldID), fmt.Sprintf("%06d-", oldSequence))
			newID := fmt.Sprintf("%s%06d-%s", prefix, item.Sequence, crop)
			if !ok || len(newID) > 64 || !(strings.HasPrefix(crop, "line") || strings.HasPrefix(crop, "context")) {
				newID = fmt.Sprintf("%s%06d-other%d", prefix, item.Sequence, j)
			}
			ev := index[oldID]
			ev.ID = newID
			copies[newID] = oldID
			item.EvidenceIDs[j] = newID
			doc.Evidence = append(doc.Evidence, ev)
		}
		doc.Items = append(doc.Items, item)
	}
	doc.Session.CapturedCount = int64(len(doc.Items))
	doc.Session.Coverage = record.Coverage{Status: record.CoveragePartial}
	doc.Session.PositionLedger = mergePositionLedger(in.base.Session, selected)
	return record.SlotMergeCommit{
		RecordID: in.preview.RecordID, RecordRevision: in.preview.RecordRevision,
		SupplementID: in.preview.SupplementID, SupplementRevision: in.preview.SupplementRevision,
		Checkpoint: doc, EvidenceCopies: copies,
	}, nil
}

func mergePositionLedger(shell record.SessionShell, selected [record.PositionCount + 1]bool) *record.PositionLedger {
	before, _ := shell.PositionConclusions()
	out := &record.PositionLedger{Sort: record.SortVerificationUnknown}
	var slots [record.PositionCount + 1]record.PositionConclusion
	for _, p := range before.Positions {
		slots[p.Position] = p
	}
	for i := 1; i <= record.PositionCount; i++ {
		if selected[i] {
			slots[i] = record.PositionConclusion{Position: i, Complete: true}
		}
		if slots[i].Position != 0 {
			out.Positions = append(out.Positions, slots[i])
		}
	}
	return out
}

// MergeEvidenceCropName removes the exclusive-copy prefix while keeping
// the crop's sequence and role available to the record viewer.
func MergeEvidenceCropName(id string) string {
	if suffix, ok := strings.CutPrefix(id, "merge-"); ok {
		if _, name, ok := strings.Cut(suffix, "-"); ok {
			return name
		}
	}
	return id
}
