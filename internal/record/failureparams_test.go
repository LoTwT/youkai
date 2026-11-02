package record

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestSessionShellFailureParamsJSON(t *testing.T) {
	cp := validDoc()
	cp.Session.FailureParams = map[string]string{"target": "high", "self": "medium"}
	raw, err := json.Marshal(cp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !bytes.Contains(raw, []byte(`"failureParams":{"self":"medium","target":"high"}`)) {
		t.Fatalf("failureParams missing from the checkpoint JSON: %s", raw)
	}
	var decoded Checkpoint
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Session.FailureParams["target"] != "high" || decoded.Session.FailureParams["self"] != "medium" {
		t.Fatalf("failureParams round trip = %v", decoded.Session.FailureParams)
	}
}
