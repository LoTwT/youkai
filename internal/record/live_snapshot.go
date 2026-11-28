package record

import (
	"bytes"
	"maps"
)

// ActiveCheckpointRecovery makes a detached display view of a checkpoint
// whose save already succeeded. Use Store.Load to verify records on disk.
func ActiveCheckpointRecovery(saved *Checkpoint) *Recovery {
	doc := cloneCheckpoint(saved)
	doc.Session.Scope = bytes.Clone(doc.Session.Scope)
	doc.Session.ObservedScope = bytes.Clone(doc.Session.ObservedScope)
	doc.Session.FailureParams = maps.Clone(doc.Session.FailureParams)
	if doc.Session.EndedAt != nil {
		ended := *doc.Session.EndedAt
		doc.Session.EndedAt = &ended
	}
	if doc.Session.Termination != nil {
		termination := *doc.Session.Termination
		doc.Session.Termination = &termination
	}
	for i := range doc.Items {
		copySnapshotPayloads(&doc.Items[i])
	}
	for i := range doc.SlotMerges {
		for j := range doc.SlotMerges[i].Replaced {
			copySnapshotPayloads(&doc.SlotMerges[i].Replaced[j])
		}
	}
	return applyInterruptionMarks(doc)
}

func copySnapshotPayloads(item *ItemRecord) {
	item.Observed = bytes.Clone(item.Observed)
	item.Data = bytes.Clone(item.Data)
	for i := range item.Revisions {
		for j := range item.Revisions[i].Changes {
			change := &item.Revisions[i].Changes[j]
			change.Before = bytes.Clone(change.Before)
			change.After = bytes.Clone(change.After)
		}
	}
}
