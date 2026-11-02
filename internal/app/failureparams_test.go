package app

import (
	"testing"

	"github.com/LoTwT/youkai/internal/scan"
)

// W2b: the first trigger's structured parameters reach the checkpoint
// shell (and therefore the UI) without entering the public scan JSON.
func TestFailureParamsFromTrigger(t *testing.T) {
	snap := scan.Snapshot{Trigger: &scan.Trigger{
		Kind:   scan.TriggerFault,
		Code:   "integrityMismatch",
		Params: map[string]string{"target": "high", "self": "medium"},
	}}
	got := failureParamsFrom(snap)
	if got["target"] != "high" || got["self"] != "medium" {
		t.Fatalf("params = %v", got)
	}
	// The copy is independent of the snapshot.
	got["target"] = "low"
	if snap.Trigger.Params["target"] != "high" {
		t.Fatal("failureParamsFrom must copy")
	}
	if failureParamsFrom(scan.Snapshot{}) != nil {
		t.Fatal("a session without a trigger has no failure params")
	}
}
