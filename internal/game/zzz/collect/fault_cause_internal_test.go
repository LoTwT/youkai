package collect

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/LoTwT/youkai/internal/platform"
	"github.com/LoTwT/youkai/internal/platform/platformfake"
	"github.com/LoTwT/youkai/internal/scan"
)

// faultCauseView serves one scripted observation and fails loudly when a
// test reaches a path it did not script.
type faultCauseView struct {
	observation platform.Observation
	err         error
}

func (v faultCauseView) Observe() (platform.Observation, error) {
	return v.observation, v.err
}

func (v faultCauseView) Frame(context.Context, platform.FrameRequest) (platform.Frame, error) {
	panic("faultCauseView: unexpected Frame call")
}

func (v faultCauseView) Dispatch(platform.InputOp) platform.InputResult {
	panic("faultCauseView: unexpected Dispatch call")
}

// requireFaultCauseOnce pins the invariant shared by every site that
// converts a platform fault into a scan fault: the cause must be the
// fault's message, not the fault itself. platform.Fault implements error,
// so passing the whole fault compiles and makes scan.FaultError repeat the
// diagnostic code ("focusLost: focusLost: …") in the stop message.
func requireFaultCauseOnce(t *testing.T, err error, fault *platform.Fault) {
	t.Helper()
	var fe *scan.FaultError
	if !errors.As(err, &fe) {
		t.Fatalf("error %v does not carry a scan fault code", err)
	}
	if fe.Code != fault.Code {
		t.Fatalf("scan fault code = %s, want %s", fe.Code, fault.Code)
	}
	if got := strings.Count(fe.Error(), fault.Code); got != 1 {
		t.Fatalf("stop message %q repeats %s %d times, want once", fe.Error(), fault.Code, got)
	}
	if !strings.Contains(fe.Error(), fault.Message) {
		t.Fatalf("stop message %q lost the cause %q", fe.Error(), fault.Message)
	}
}

// TestTraversalFaultCausesKeepCodeOnce covers every faultCause call site
// on the traversal's stop paths. A regression at any one of them has to
// fail on its own name.
func TestTraversalFaultCausesKeepCodeOnce(t *testing.T) {
	problem := platform.NewFault(platform.CodeFocusLost, "another application holds the foreground")
	cases := []struct {
		name string
		run  func(t *testing.T) error
	}{
		{
			name: "precheck observation problem",
			run: func(t *testing.T) error {
				t.Helper()
				sess := platformfake.NewSession()
				sess.ObserveFaults = []*platform.Fault{problem}
				inv := &Inventory{Layout: MacCloudLayout()}
				return inv.Precheck(context.Background(), sess)
			},
		},
		{
			name: "collection observation problem",
			run: func(t *testing.T) error {
				t.Helper()
				run := &inventoryRun{view: faultCauseView{
					observation: platform.Observation{Problems: []*platform.Fault{problem}},
				}}
				return run.observeReady(context.Background())
			},
		},
		{
			name: "dispatch denial",
			run: func(t *testing.T) error {
				t.Helper()
				run := &inventoryRun{}
				return run.dispatchOutcome(context.Background(), platform.InputResult{
					Status: platform.InputNotDispatched,
					Denial: problem,
				})
			},
		},
		{
			name: "observation error fault",
			run: func(t *testing.T) error {
				t.Helper()
				return wrapObservationErr(problem)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			requireFaultCauseOnce(t, tc.run(t), problem)
		})
	}
}
