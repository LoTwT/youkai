package ui

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"sync"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/game/zzz"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/LoTwT/youkai/internal/scan"
	"github.com/egoist/mygo/ui"
)

// queueUpdater collects Update closures instead of running them; the test
// drains them from its own goroutine, so the model's state stays confined
// and the test decides when an asynchronous result lands.
type queueUpdater struct {
	mu  sync.Mutex
	fns []func()
	n   int // total closures ever queued
}

func (q *queueUpdater) Update(fn func()) {
	q.mu.Lock()
	q.fns = append(q.fns, fn)
	q.n++
	q.mu.Unlock()
}

// take returns the queued closures and clears the queue.
func (q *queueUpdater) take() []func() {
	q.mu.Lock()
	defer q.mu.Unlock()
	fns := q.fns
	q.fns = nil
	return fns
}

// empty reports whether nothing is queued.
func (q *queueUpdater) empty() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.fns) == 0
}

// count returns the total number of closures ever queued.
func (q *queueUpdater) count() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.n
}

// drainAll runs the queued updates until the model's background work is
// idle and nothing more is queued, and reports whether it ran anything.
func drainAll(t *testing.T, m *Model, q *queueUpdater) bool {
	for i := 0; i < 200; i++ {
		m.Idle()
		fns := q.take()
		if len(fns) == 0 {
			return i > 0
		}
		for _, fn := range fns {
			fn()
		}
	}
	t.Fatalf("queued updates did not settle")
	return false
}

// settle drains the model's asynchronous work and redraws, repeating
// until a frame schedules nothing new, so the last frame shows everything
// that landed.
func settle(t *testing.T, m *Model, q *queueUpdater, tt *ui.Tester) {
	for i := 0; i < 10; i++ {
		drainAll(t, m, q)
		tt.Frame()
		m.Idle()
		if q.empty() {
			return
		}
	}
	t.Fatalf("frames kept scheduling work")
}

// fakeService is the application service double: every call is recorded,
// every answer canned. The model's tests drive states through Apply and
// the fakes' answers; no native window is involved.
type fakeService struct {
	mu      sync.Mutex
	starts  []app.StartRequest
	startFn func(req app.StartRequest) (app.StartOutcome, error)
	stops   int
	retries int
	quits   int
	quitFn  func() error

	list    []record.RecordInfo
	listErr error

	loads   map[string]*record.Recovery
	loadErr map[string]error
	loadLog []string

	saves  []saveCall
	saveFn func(recordID string, sequence, expectedRevision int64, edit app.ItemRevisionEdit) (app.ItemRevisionResult, error)
}

// saveCall is one recorded SaveItemRevision request.
type saveCall struct {
	recordID         string
	sequence         int64
	expectedRevision int64
	edit             app.ItemRevisionEdit
}

func (f *fakeService) Start(req app.StartRequest) (app.StartOutcome, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.starts = append(f.starts, req)
	if f.startFn != nil {
		return f.startFn(req)
	}
	return app.StartOutcome{SessionID: scan.ID("scan-1")}, nil
}

func (f *fakeService) Stop() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stops++
}

func (f *fakeService) Snapshot() app.Snapshot {
	return app.Snapshot{}
}

func (f *fakeService) RetrySave() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.retries++
	return nil
}

func (f *fakeService) ListRecords() ([]record.RecordInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.list, f.listErr
}

func (f *fakeService) LoadRecord(recordID string) (*record.Recovery, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loadLog = append(f.loadLog, recordID)
	if err, ok := f.loadErr[recordID]; ok {
		return nil, err
	}
	rec, ok := f.loads[recordID]
	if !ok {
		return nil, fmt.Errorf("no verifiable checkpoint after 1 generation(s)")
	}
	return rec, nil
}

func (f *fakeService) SaveItemRevision(recordID string, sequence, expectedRevision int64, edit app.ItemRevisionEdit) (app.ItemRevisionResult, error) {
	f.mu.Lock()
	f.saves = append(f.saves, saveCall{
		recordID:         recordID,
		sequence:         sequence,
		expectedRevision: expectedRevision,
		edit:             edit,
	})
	fn := f.saveFn
	f.mu.Unlock()
	if fn != nil {
		return fn(recordID, sequence, expectedRevision, edit)
	}
	return app.ItemRevisionResult{}, fmt.Errorf("fake service: no save handler")
}

func (f *fakeService) saveCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.saves)
}

func (f *fakeService) Quit() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.quits++
	if f.quitFn != nil {
		return f.quitFn()
	}
	return nil
}

// fakeEvidence is the evidence double: every load is recorded and answers
// from a table, so tests assert which images the view asked for.
type fakeEvidence struct {
	mu     sync.Mutex
	calls  []string
	images map[string]*ui.Bitmap
}

func (f *fakeEvidence) Evidence(recordID, evidenceID string) (*ui.Bitmap, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, recordID+"/"+evidenceID)
	bm, ok := f.images[recordID+"/"+evidenceID]
	if !ok {
		return nil, fmt.Errorf("missing evidence %s", evidenceID)
	}
	return bm, nil
}

func (f *fakeEvidence) callsCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// solidBitmap makes a small bitmap of one flat color, distinct per test
// item so a rendered frame tells the images apart by their pixels.
func solidBitmap(c color.RGBA) *ui.Bitmap {
	img := image.NewRGBA(image.Rect(0, 0, 24, 10))
	for y := img.Bounds().Dy() - 1; y >= 0; y-- {
		for x := img.Bounds().Dx() - 1; x >= 0; x-- {
			img.SetRGBA(x, y, c)
		}
	}
	return ui.NewBitmap(img)
}

// snap builds a service snapshot for a session at one revision.
func snap(session string, rev uint64, busy bool, phase scan.Phase, counts scan.Counts,
	saved int, pending bool, saveErr string) app.Snapshot {
	return app.Snapshot{
		SessionID:   scan.ID(session),
		Revision:    rev,
		Busy:        busy,
		SavedCount:  saved,
		SavePending: pending,
		SaveError:   saveErr,
		Scan: scan.Snapshot{
			SessionID: scan.ID(session),
			Revision:  rev,
			Phase:     phase,
			Counts:    counts,
		},
	}
}

// recovery builds a loaded record with the items; items the crash left
// interrupted are reported as the recovery marks them. The shell carries
// the fields the export module needs, so export tests can use the same
// helper.
func recovery(items ...record.ItemRecord) *record.Recovery {
	var interrupted []int64
	for _, it := range items {
		if it.Status == record.StatusUnprocessed && it.Reason == record.ReasonInterrupted {
			interrupted = append(interrupted, it.Sequence)
		}
	}
	return &record.Recovery{
		Checkpoint: record.Checkpoint{
			Format:        record.FormatName,
			FormatVersion: record.FormatVersion,
			SavedRevision: 1,
			Session: record.SessionShell{
				SessionID:     "rec",
				Game:          "zzz",
				Entity:        "driveDisc",
				Client:        record.ClientWindowsLocal,
				Locale:        "zh-CN",
				Scope:         []byte(`{"kind":"zzzInventory","mode":"traverse"}`),
				StartedAt:     time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC),
				Termination:   &record.Termination{Kind: record.TerminationCompleted, Code: "verifiedEnd", Phase: record.PhaseCapturing},
				Coverage:      record.Coverage{Status: record.CoverageUnknown},
				CapturedCount: int64(len(items)),
			},
			Items: items,
		},
		Interrupted: interrupted,
	}
}

// item builds one item record with lines texts: a produced item carries
// the observed and result payloads, a failed one its failure text, an
// unprocessed one its reason. Evidence ids follow the line count.
func item(seq int64, status record.Status, reason record.UnprocessedReason, failure string,
	texts ...string) record.ItemRecord {
	var observed, data string
	for i, text := range texts {
		if i > 0 {
			observed += ","
			data += ","
		}
		role := fmt.Sprintf("line%d", i)
		observed += fmt.Sprintf(`{"role":%q,"source":"d%d_l%d.png"}`, role, seq, i)
		data += fmt.Sprintf(`{"role":%q,"text":%q,"score":0.9%d}`, role, text, i)
	}
	observed = `{"lines":[` + observed + `]}`
	data = `{"lines":[` + data + `]}`
	it := record.ItemRecord{
		Sequence: seq,
		Status:   status,
		Reason:   reason,
		Failure:  failure,
		Quality:  record.QualityNotEvaluated,
		Observed: []byte(observed),
	}
	if status == record.StatusProduced {
		it.Data = []byte(data)
	}
	for i := range texts {
		it.EvidenceIDs = append(it.EvidenceIDs, fmt.Sprintf("%06d-line%d", seq, i))
	}
	return it
}

// structuredItem builds a produced item with raw recognized lines and a
// persisted structured payload, as a current record carries both.
func structuredItem(seq int64, disc zzz.DiscRecord, texts ...string) record.ItemRecord {
	it := item(seq, record.StatusProduced, "", "", texts...)
	structured := zzz.ToStructured(disc, zzz.RarityS, nil)
	raw, err := json.Marshal(structured)
	if err != nil {
		panic(err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(it.Data, &doc); err != nil {
		panic(err)
	}
	doc["structured"] = raw
	it.Data, err = json.Marshal(doc)
	if err != nil {
		panic(err)
	}
	return it
}

// intp returns a pointer to v for the structured payload's nullable
// numeric fields.
func intp(v int) *int { return &v }
