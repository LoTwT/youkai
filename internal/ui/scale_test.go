package ui

import (
	"encoding/json"
	"fmt"
	"image/color"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/game/zzz"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/egoist/mygo/ui"
)

// scaleReviewPayload is one complete structured item payload shared by the
// scale fixtures. Sharing the bytes keeps 10,000 rows cheap to build: the
// row view still parses each item independently, exactly as a real
// checkpoint would.
func scaleReviewPayload(t *testing.T) []byte {
	t.Helper()
	structured := zzz.StructuredDisc{
		Data:       completeReviewData(12),
		Issues:     []zzz.Issue{},
		Validation: zzz.Validation{Status: zzz.ValidationPassed, Issues: []zzz.Issue{}},
	}
	raw, err := json.Marshal(structured)
	if err != nil {
		t.Fatalf("marshal scale structured payload: %v", err)
	}
	payload, err := json.Marshal(map[string]json.RawMessage{
		"lines":      json.RawMessage("[]"),
		"structured": raw,
	})
	if err != nil {
		t.Fatalf("marshal scale item payload: %v", err)
	}
	return payload
}

// scaleItems builds n produced items with one evidence id each. Items
// whose zero-based index is divisible by settledEvery are auto-accepted
// (settled); the rest stay pending review.
func scaleItems(t *testing.T, n, settledEvery int) []record.ItemRecord {
	t.Helper()
	payload := scaleReviewPayload(t)
	items := make([]record.ItemRecord, n)
	for i := range items {
		quality := record.QualityNeedsReview
		if settledEvery > 0 && i%settledEvery == 0 {
			quality = record.QualityAutoAccepted
		}
		seq := int64(i + 1)
		items[i] = record.ItemRecord{
			Sequence:    seq,
			Status:      record.StatusProduced,
			Quality:     quality,
			Data:        payload,
			Observed:    []byte(`{"lines":[]}`),
			EvidenceIDs: []string{fmt.Sprintf("%06d-line0", seq)},
		}
	}
	return items
}

// TestScaleBuildRecordViewOneAndTenThousandRows checks the data layer at
// both M3 sizes: every item keeps its row and capture order, the pending
// count counts only unsettled produced items, and nextPendingAfter walks
// forward without wrapping.
func TestScaleBuildRecordViewOneAndTenThousandRows(t *testing.T) {
	for _, n := range []int{1000, 10000} {
		t.Run(fmt.Sprintf("%d_rows", n), func(t *testing.T) {
			items := scaleItems(t, n, 10)
			start := time.Now()
			view := BuildRecordView("scale", &record.Recovery{
				Checkpoint: record.Checkpoint{Items: items},
			}, false)
			build := time.Since(start)

			if len(view.Rows) != n {
				t.Fatalf("rows = %d, want %d", len(view.Rows), n)
			}
			if view.Rows[0].Seq != 1 || view.Rows[n-1].Seq != int64(n) {
				t.Fatalf("row sequences = %d..%d, want 1..%d",
					view.Rows[0].Seq, view.Rows[n-1].Seq, n)
			}
			if view.Rows[n/2].Seq != int64(n/2+1) {
				t.Fatalf("middle row sequence = %d, want %d", view.Rows[n/2].Seq, n/2+1)
			}

			m := &Model{rec: view}
			wantPending := n - n/10
			if got := m.pendingCount(); got != wantPending {
				t.Fatalf("pending count = %d, want %d", got, wantPending)
			}
			// Index 0 is settled (auto-accepted); the next pending row is
			// index 1. Index 8 (seq 9) is pending, index 9 (seq 10) is
			// settled, so the walk skips it and stops at index 10.
			if got := m.nextPendingAfter(1); got != 1 {
				t.Fatalf("next pending after seq 1 = %d, want 1", got)
			}
			if got := m.nextPendingAfter(10); got != 11 {
				t.Fatalf("next pending after seq 10 = %d, want the settled seq 11 skipped", got)
			}
			// The last row is settled (index n-1 divisible by 10); the
			// walk wraps to the first pending row, index 1.
			if got := m.nextPendingAfter(int64(n)); got != 1 {
				t.Fatalf("next pending after the last row = %d, want wrap to 1", got)
			}
			t.Logf("%d rows: BuildRecordView %s, pending %d", n, build, wantPending)
		})
	}
}

// TestScaleResultsBrowserRendersThousandRows checks the first M3 size
// through the real results view. The 10,000-row case stays at the data
// layer; rendering it in the test window is not evidence of a product UI
// benchmark and would only measure the tester.
func TestScaleResultsBrowserRendersThousandRows(t *testing.T) {
	items := scaleItems(t, 1000, 0)
	start := time.Now()
	m, _, tt, _ := openReviewRecord(t, items...)
	elapsed := time.Since(start)

	if !tt.HasText("结果：历史记录 r1（1000 条）") {
		t.Fatalf("the 1000-row record title does not show: %q", tt.Texts()[:min(8, len(tt.Texts()))])
	}
	if !tt.HasText("待校对 1000 件") {
		t.Fatalf("the pending count does not show: %q", tt.Texts()[:min(8, len(tt.Texts()))])
	}
	if !tt.HasText("河豚电音") {
		t.Fatalf("the structured first rows do not show: %q", tt.Texts()[:min(8, len(tt.Texts()))])
	}
	if len(m.rec.Rows) != 1000 {
		t.Fatalf("the rendered model holds %d rows", len(m.rec.Rows))
	}
	t.Logf("1000-row results view opened and rendered in %s", elapsed)
}

// TestScaleContinuousReviewAdvancesThroughPendingRows drives the model's
// confirm-and-next path for a bounded number of rounds. Each round persists
// one revision through the fake service, so the test proves the navigation
// and count stay correct at 1,000 rows without writing 1,000 checkpoints.
func TestScaleContinuousReviewAdvancesThroughPendingRows(t *testing.T) {
	const total = 1000
	const rounds = 100
	items := scaleItems(t, total, 0)
	rec := recovery(items...)
	svc := &fakeService{loads: map[string]*record.Recovery{"r1": rec}}
	installReviewSave(svc)
	m, q := newTestModel(t, Config{Service: svc})
	m.rec = BuildRecordView("r1", rec, false)
	m.selected = 0

	start := time.Now()
	for round := range rounds {
		if got := m.pendingCount(); got != total-round {
			t.Fatalf("round %d: pending = %d, want %d", round, got, total-round)
		}
		if m.selected != round {
			t.Fatalf("round %d: selected = %d, want %d", round, m.selected, round)
		}
		m.confirmCurrent()
		drainAll(t, m, q)
		if m.reviewErr != "" {
			t.Fatalf("round %d: review error = %q", round, m.reviewErr)
		}
		if m.rec.Rows[round].Quality != record.QualityHumanConfirmed {
			t.Fatalf("round %d: row quality = %q", round, m.rec.Rows[round].Quality)
		}
	}
	elapsed := time.Since(start)

	if got := m.pendingCount(); got != total-rounds {
		t.Fatalf("pending after %d rounds = %d, want %d", rounds, got, total-rounds)
	}
	if m.selected != rounds {
		t.Fatalf("selection after %d rounds = %d, want %d", rounds, m.selected, rounds)
	}
	if want := fmt.Sprintf("已确认并定位下一项（序号 %d）。", rounds+1); m.reviewNotice != want {
		t.Fatalf("the next-item notice = %q, want %q", m.reviewNotice, want)
	}
	if got := svc.saveCount(); got != rounds {
		t.Fatalf("saved revisions = %d, want %d", got, rounds)
	}
	t.Logf("%d confirmations over %d rows in %s", rounds, total, elapsed)
}

// waitCached drains queued updates until key reaches the cache. It does
// not wait for every background load, so a test can settle one response
// while another load is deliberately still blocked.
func waitCached(t *testing.T, m *Model, q *queueUpdater, key evidenceKey) *evidenceEntry {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, fn := range q.take() {
			fn()
		}
		if entry := m.cache.get(key); entry != nil {
			return entry
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("evidence %+v did not reach the cache", key)
	return nil
}

// gatedEvidence blocks each load until its release channel closes, so a
// late completion can be ordered explicitly in a test.
type gatedEvidence struct {
	started chan string
	release map[string]chan struct{}
	images  map[string]*ui.Bitmap
}

func (g *gatedEvidence) Evidence(recordID, evidenceID string) (*ui.Bitmap, error) {
	key := recordID + "/" + evidenceID
	g.started <- key
	<-g.release[key]
	return g.images[key], nil
}

// TestScaleEvidenceCacheStaysBoundedAcrossItems walks 100 rows of a
// 1,000-row record, loading two images per row through the real cache. The
// decoded-image cache never exceeds its configured bound, and every cached
// key keeps the bitmap of its own evidence.
func TestScaleEvidenceCacheStaysBoundedAcrossItems(t *testing.T) {
	const rows = 1000
	const visited = 100
	const limit = 8
	items := scaleItems(t, rows, 0)
	for i := range items {
		items[i].EvidenceIDs = []string{
			fmt.Sprintf("%06d-line0", i+1),
			fmt.Sprintf("%06d-line1", i+1),
		}
	}
	ev := &fakeEvidence{images: map[string]*ui.Bitmap{}}
	for i := range items {
		for line := range 2 {
			ev.images[fmt.Sprintf("r1/%06d-line%d", i+1, line)] =
				solidBitmap(color.RGBA{R: uint8(20 + (i+line)%200), G: 180, B: 90, A: 255})
		}
	}
	m, q := newTestModel(t, Config{Service: &fakeService{}, Evidence: ev, EvidenceCacheLimit: limit})
	m.rec = BuildRecordView("r1", recovery(items...), false)

	for row := range visited {
		m.selected = row
		for _, id := range m.rec.Rows[row].EvidenceIDs {
			m.evidenceFor("r1", id)
		}
		drainAll(t, m, q)
		if got := len(m.cache.entries); got > limit {
			t.Fatalf("row %d: cache holds %d entries above the limit %d", row, got, limit)
		}
		for _, id := range m.rec.Rows[row].EvidenceIDs {
			key := evidenceKey{record: "r1", id: id}
			entry := m.cache.get(key)
			if entry == nil || entry.bitmap == nil {
				t.Fatalf("row %d: evidence %q is not cached", row, id)
			}
			if entry.bitmap != ev.images["r1/"+id] {
				t.Fatalf("row %d: evidence %q is attached to another image", row, id)
			}
		}
	}
	if got := ev.callsCount(); got != visited*2 {
		t.Fatalf("evidence loads = %d, want %d", got, visited*2)
	}
	t.Logf("cache stayed at %d entries over %d rows", limit, visited)
}

// TestScaleLateEvidenceCompletionStaysKeyed orders two overlapping loads so
// the first row's response lands after the selection moved to the second
// row. The late bitmap stays under the first evidence key and cannot
// replace the second row's image.
func TestScaleLateEvidenceCompletionStaysKeyed(t *testing.T) {
	first := solidBitmap(color.RGBA{R: 200, G: 10, B: 20, A: 255})
	second := solidBitmap(color.RGBA{R: 10, G: 200, B: 30, A: 255})
	ev := &gatedEvidence{
		started: make(chan string, 2),
		release: map[string]chan struct{}{
			"r1/000001-line0": make(chan struct{}),
			"r1/000002-line0": make(chan struct{}),
		},
		images: map[string]*ui.Bitmap{
			"r1/000001-line0": first,
			"r1/000002-line0": second,
		},
	}
	items := scaleItems(t, 2, 0)
	m, q := newTestModel(t, Config{Service: &fakeService{}, Evidence: ev, EvidenceCacheLimit: 4})
	m.rec = BuildRecordView("r1", recovery(items...), false)

	m.selected = 0
	m.evidenceFor("r1", "000001-line0")
	if key := <-ev.started; key != "r1/000001-line0" {
		t.Fatalf("first load = %q", key)
	}
	// Move the selection before the first response lands, then load the
	// second row and let it finish first.
	m.selected = 1
	m.evidenceFor("r1", "000002-line0")
	if key := <-ev.started; key != "r1/000002-line0" {
		t.Fatalf("second load = %q", key)
	}
	close(ev.release["r1/000002-line0"])
	if entry := waitCached(t, m, q, evidenceKey{record: "r1", id: "000002-line0"}); entry.bitmap != second {
		t.Fatalf("the selected row's completed load is not its own bitmap: %+v", entry)
	}

	close(ev.release["r1/000001-line0"])
	late := waitCached(t, m, q, evidenceKey{record: "r1", id: "000001-line0"})
	m.Idle()
	if late.bitmap != first {
		t.Fatalf("the late first-row load landed under the wrong key: %+v", late)
	}
	shown := m.evidenceFor("r1", "000002-line0")
	if shown == nil || shown.bitmap != second {
		t.Fatalf("the second row's displayed evidence changed after the late load: %+v", shown)
	}
	if got := ev.started; len(got) != 0 {
		t.Fatalf("an unexpected extra evidence load was scheduled: %d started", len(got))
	}
}

// TestScalePendingNavigationHandlesTenThousandRows exercises the pending
// count and wrapping forward walk at 10,000 rows without any persistence.
func TestScalePendingNavigationHandlesTenThousandRows(t *testing.T) {
	const n = 10000
	view := BuildRecordView("scale", &record.Recovery{
		Checkpoint: record.Checkpoint{Items: scaleItems(t, n, 10)},
	}, false)
	m := &Model{rec: view}
	if got := m.pendingCount(); got != n-n/10 {
		t.Fatalf("pending count = %d, want %d", got, n-n/10)
	}
	if got := m.nextPendingAfter(8); got != 8 {
		t.Fatalf("next pending after seq 8 = %d, want the same pending row", got)
	}
	if got := m.nextPendingAfter(10); got != 11 {
		t.Fatalf("next pending after seq 10 = %d, want the settled row skipped", got)
	}
	if got := m.nextPendingAfter(n); got != 1 {
		t.Fatalf("next pending after the last row = %d, want wrap to 1", got)
	}
}
