package ui

import (
	"testing"

	"github.com/LoTwT/youkai/internal/record"
	"github.com/egoist/mygo/ui"
)

// A queued UI commit must not expose a placeholder, even after disk I/O
// has finished. The first detail frame already owns the real record.
func TestHistoryOpenWaitsForPreparedCommit(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	svc := &fakeService{history: historyTableRecords(2), loadFn: func(string) (*record.Recovery, error) {
		close(started)
		<-release
		return testRecord(), nil
	}}
	m, q := newTestModel(t, Config{Service: svc})
	drainAll(t, m, q)
	m.page = pageHistory
	tt := ui.NewTester(m.View, 1180, 760)
	tt.Key(0, ui.KeyEnter)
	<-started
	// Always release the worker, including when the old behavior fails.
	close(release)
	m.Idle()
	t.Logf("before UI commit: page=%v rec=%v queued=%d", m.displayPage(), m.rec, q.count())
	if m.displayPage() != pageHistory || m.rec != nil {
		t.Fatalf("unprepared record escaped the list: page=%v rec=%+v", m.displayPage(), m.rec)
	}
	tt.Frame()
	if !tt.HasText("正在读取记录…") {
		t.Fatalf("the list does not show its loading layer: %q", tt.Texts())
	}
	drainAll(t, m, q)
	tt.Frame()
	if m.displayPage() != pageResults || m.rec == nil || m.rec.ID != "r01" || len(m.rec.Rows) != 4 || m.recData == nil {
		t.Fatalf("the prepared commit did not enter the real record: page=%v rec=%+v", m.displayPage(), m.rec)
	}
}
