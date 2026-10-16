package record

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// TestConcurrentWritesGoThroughTheSerialWriter races evidence writes,
// checkpoint saves and loads from many goroutines. The race detector
// watches the shared state; the assertions watch the invariants: every
// evidence file lands, the record always loads a complete generation,
// and retention keeps the generation count bounded.
func TestConcurrentWritesGoThroughTheSerialWriter(t *testing.T) {
	st, root := newTestStore(t)
	id := newRecord(t, st)

	const workers = 8
	const rounds = 5
	var wg sync.WaitGroup
	errCh := make(chan error, workers*rounds*2)

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for r := 0; r < rounds; r++ {
				evID := fmt.Sprintf("ev-%d-%d", w, r)
				image := []byte{0x89, 'P', byte(w), byte(r), 0, 1, 2, 3}
				ev, err := st.WriteEvidence(id, evID, image)
				if err != nil {
					errCh <- fmt.Errorf("worker %d: WriteEvidence: %w", w, err)
					continue
				}
				cp := Checkpoint{Session: testShell()}
				cp.Session.CapturedCount = 1
				cp.Items = []ItemRecord{{
					Sequence:    1,
					Status:      StatusProduced,
					Quality:     QualityAutoAccepted,
					EvidenceIDs: []string{ev.ID},
				}}
				cp.Evidence = []EvidenceFile{ev}
				if _, err := st.SaveCheckpoint(id, cp); err != nil {
					errCh <- fmt.Errorf("worker %d: SaveCheckpoint: %w", w, err)
				}
			}
		}(w)
	}

	// A concurrent reader must only ever observe complete states; a
	// load before the first save finds nothing yet, which is a valid
	// observation, not an error.
	stop := make(chan struct{})
	var readers sync.WaitGroup
	for i := 0; i < 2; i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				if _, err := st.Load(id); err != nil && !errors.Is(err, ErrNoValidCheckpoint) {
					errCh <- fmt.Errorf("reader: Load: %w", err)
					return
				}
			}
		}()
	}

	wg.Wait()
	close(stop)
	readers.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}

	entries, err := os.ReadDir(filepath.Join(root, "sessions", id, "evidence"))
	if err != nil {
		t.Fatalf("ReadDir evidence: %v", err)
	}
	if len(entries) != workers*rounds {
		t.Errorf("evidence files = %d, want %d", len(entries), workers*rounds)
	}
	mustLoad(t, st, id)
	gens, err := os.ReadDir(filepath.Join(root, "sessions", id, "checkpoints"))
	if err != nil {
		t.Fatalf("ReadDir checkpoints: %v", err)
	}
	// Retention keeps the current and the previous valid generation;
	// anything above three would mean pruning is not keeping up.
	if len(gens) > 3 {
		t.Errorf("generations = %d, want at most current + previous (+ one in flight)", len(gens))
	}
}

// Competing edits of the same item with the same expectation: exactly
// one wins, everyone else gets a conflict, and the item ends up with
// one revision — the serial writer plus optimistic concurrency is what
// makes a stale editor harmless.
func TestConcurrentRevisionsOnlyOneWins(t *testing.T) {
	st, _ := newTestStore(t)
	id := revisionRecord(t, st)

	const n = 4
	var wg sync.WaitGroup
	results := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := st.SaveRevision(id, 1, 0, ItemEdit{
				Changes: []FieldChange{{
					Field:  "slot",
					Before: json.RawMessage(`6`),
					After:  json.RawMessage(`5`),
				}},
				Data: json.RawMessage(`{"slot":5}`),
			})
			results <- err
		}()
	}
	wg.Wait()
	close(results)

	wins, conflicts := 0, 0
	for err := range results {
		switch {
		case err == nil:
			wins++
		case errors.Is(err, ErrRevisionConflict):
			conflicts++
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}
	if wins != 1 || conflicts != n-1 {
		t.Fatalf("results = %d wins, %d conflicts; want 1 win, %d conflicts", wins, conflicts, n-1)
	}
	rec := mustLoad(t, st, id)
	item := rec.Checkpoint.Items[0]
	if item.Revision != 1 || len(item.Revisions) != 1 || item.Quality != QualityNeedsReview {
		t.Errorf("item = revision %d, %d history entries, quality %q; want 1, 1, needsReview",
			item.Revision, len(item.Revisions), item.Quality)
	}
}
