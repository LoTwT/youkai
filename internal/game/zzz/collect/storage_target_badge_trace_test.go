//go:build page_unknown_trace

package collect

import (
	"reflect"
	"testing"

	"github.com/LoTwT/youkai/internal/platform"
)

func TestTargetBadgeFaultRetainsExactFrame(t *testing.T) {
	t.Setenv(pageUnknownTraceEnv, "1")
	dir := t.TempDir()
	frames := pageUnknownTestFrames(t)
	run := pageUnknownTestRun(t, frames, 0, dir)
	m := run.inv.Matcher.Match(frames[0])
	target := m.List.Selected
	position := m.List.Positions[target].Position
	tracePageUnknownInput(run, target, 1, platform.InputResult{Status: platform.InputPosted})
	for _, f := range frames[:2] {
		tracePageUnknownObserve(run, f, m, target, detailOutcome{})
	}
	last := frames[2]
	m.List.Positions[target] = PositionVerdict{Conflict: true}
	_, _, _, fault := run.storageBadgeDisposition(last, platform.Frame{}, nil, m, target, true, true, platform.Frame{})
	if code := faultDiagnosticCode(fault); code != string(CodePositionLost) {
		t.Fatalf("fault code = %q, want positionLost", code)
	}
	result := pageUnknownTestManifests(t, dir)
	if len(result) != 1 || result[0].Kind != "target-badge-terminal" ||
		result[0].FailureFrame != last.ID || len(result[0].Frames) != 3 {
		t.Fatal("missing exact target badge trace")
	}
	want := &pageUnknownTraceBadge{ExpectedPosition: position, Conflict: true}
	for i, got := range result[0].Frames {
		if got.ID != frames[i].ID {
			t.Fatalf("trace frame %d has ID %d", i, got.ID)
		}
	}
	if got := result[0].Frames[2].TargetBadge; !reflect.DeepEqual(got, want) {
		t.Fatalf("terminal badge = %+v, want %+v", got, want)
	}
}
