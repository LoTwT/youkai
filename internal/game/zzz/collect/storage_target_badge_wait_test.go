//go:build page_unknown_trace

package collect

import (
	"context"
	"image"
	"slices"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/platform"
)

type targetBadgeWaitMatcher struct {
	pageUnknownTestMatcher
	verdicts map[uint64]PositionVerdict
	moved    uint64
}

func (m targetBadgeWaitMatcher) Match(f platform.Frame) PageMatch {
	page := m.pageUnknownTestMatcher.Match(f)
	if replacement, ok := m.verdicts[f.ID]; ok {
		list := *page.List
		list.Positions = slices.Clone(list.Positions)
		list.Positions[list.Selected] = replacement
		page.List = &list
	}
	return page
}

func (m targetBadgeWaitMatcher) MatchScroll(_, after platform.Frame) ScrollMatch {
	if after.ID == m.moved {
		return ScrollMatch{Displaced: true, Rows: 1}
	}
	return ScrollMatch{Stationary: true}
}

func (m targetBadgeWaitMatcher) ThumbEdges(platform.Frame) (int, int, bool) {
	return 500, 523, true
}

func TestStorageTargetBadgeWaitRequiresFreshStableRecovery(t *testing.T) {
	for _, tc := range []struct {
		name       string
		verdicts   map[uint64]PositionVerdict
		reused     bool
		moved      uint64
		wantStable bool
		wantFault  bool
	}{
		{name: "verified new frames", verdicts: map[uint64]PositionVerdict{3: {}}, wantStable: true},
		{name: "timeout", verdicts: map[uint64]PositionVerdict{3: {}, 4: {}, 5: {}, 6: {}}},
		{name: "reused recovery", verdicts: map[uint64]PositionVerdict{3: {}}, reused: true},
		{name: "conflicting target", verdicts: map[uint64]PositionVerdict{3: {Conflict: true}}, wantFault: true},
		{name: "different target badge", verdicts: map[uint64]PositionVerdict{3: {Position: 2}}, wantFault: true},
		{name: "moved after dropout", verdicts: map[uint64]PositionVerdict{3: {}}, moved: 4, wantFault: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frames := make([]platform.Frame, 5)
			layout := StorageLayout()
			start := time.Now()
			for i := range frames {
				frames[i] = platform.Frame{ID: uint64(i + 2), ArrivedAt: start.Add(time.Duration(i) * 100 * time.Millisecond), Geometry: 1, Image: image.NewRGBA(layout.Content), Content: layout.Content}
				if tc.reused && i >= 2 {
					frames[i].Reused = true
				}
			}

			run := pageUnknownTestRun(t, frames, 0, t.TempDir())
			const target = 18
			pageUnknownTestUseTarget(run, target, true)
			matcher := run.inv.Matcher.(pageUnknownTestMatcher)
			run.inv.Matcher = targetBadgeWaitMatcher{pageUnknownTestMatcher: matcher, verdicts: tc.verdicts, moved: tc.moved}
			base := pageUnknownTestBaseline()
			out, fault := run.awaitNormalTargetDetail(context.Background(), base, nil, target)
			if tc.wantFault {
				if code := faultDiagnosticCode(fault); code != "positionLost" {
					t.Fatalf("fault=%v want positionLost, out class=%v", fault, out.class)
				}
				return
			}
			if fault != nil {
				t.Fatal(fault)
			}
			if !out.sawTargetBadgeTransient {
				t.Fatal("missing bounded target badge recheck")
			}
			if tc.wantStable {
				if out.class != awaitStable || out.frame.ID != 6 || out.frame.Reused || out.match.List.Positions[target].Position != 1 {
					t.Fatalf("admission frame %d class %v", out.frame.ID, out.class)
				}
			} else if out.class != awaitDeadline {
				t.Fatalf("unproven result class=%v, want deadline", out.class)
			}
		})
	}
}
