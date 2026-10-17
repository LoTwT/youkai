package ui

import (
	"os"

	"github.com/LoTwT/youkai/internal/record"
	"github.com/egoist/mygo/ui"
)

// EvidenceSource loads one evidence image by its stable record and
// evidence identity, as a loaded checkpoint names them. Implementations
// resolve the identity below the data root themselves — the interface
// never takes a path.
type EvidenceSource interface {
	Evidence(recordID, evidenceID string) (*ui.Bitmap, error)
}

// FileEvidence loads evidence images from a data root's sessions layout —
// the same layout the record store writes. Evidence identities come from
// loaded checkpoints, which verified the files; a read or decode failure
// is shown as that image being unavailable, never hidden.
type FileEvidence struct {
	// Root is the app's data root (record.Paths.Data).
	Root string
}

// Evidence implements EvidenceSource.
func (f FileEvidence) Evidence(recordID, evidenceID string) (*ui.Bitmap, error) {
	data, err := os.ReadFile(record.EvidencePath(f.Root, recordID, evidenceID))
	if err != nil {
		return nil, err
	}
	return ui.DecodeBitmap(data)
}

// evidenceCacheLimit is the default number of decoded evidence bitmaps
// held at once. One item shows a handful of line crops; the bound keeps a
// long browsing session's memory flat.
const evidenceCacheLimit = 32

type evidenceKey struct {
	record string
	id     string
}

// evidenceEntry is one evidence image's cached outcome: the decoded
// bitmap, or the error that keeps it unavailable. Errors are cached too,
// so a missing image does not reload on every frame.
type evidenceEntry struct {
	bitmap *ui.Bitmap
	err    error
}

// evidenceCache keeps the details view's decoded evidence bitmaps, most
// recently used last, bounded by evicting the least recently used. Like
// the model, it is confined to the UI thread: gets happen while the view
// builds, puts inside the load's update.
type evidenceCache struct {
	load    EvidenceSource
	limit   int
	entries map[evidenceKey]*evidenceEntry
	order   []evidenceKey
	loading map[evidenceKey]bool
}

func newEvidenceCache(load EvidenceSource, limit int) *evidenceCache {
	return &evidenceCache{
		load:    load,
		limit:   limit,
		entries: make(map[evidenceKey]*evidenceEntry),
		loading: make(map[evidenceKey]bool),
	}
}

// get returns the cached entry for key, or nil; a hit keeps the entry
// fresh.
func (c *evidenceCache) get(key evidenceKey) *evidenceEntry {
	if e, ok := c.entries[key]; ok {
		c.touch(key)
		return e
	}
	return nil
}

// begin marks key as loading and reports whether the caller should start
// the load; a load already on its way is not started twice.
func (c *evidenceCache) begin(key evidenceKey) bool {
	if c.loading[key] {
		return false
	}
	c.loading[key] = true
	return true
}

// put stores the load's outcome for key and clears its loading mark. An
// error is an outcome like any other. Trimming to the bound happens in
// evict, which the caller gives the displayed keys to keep.
func (c *evidenceCache) put(key evidenceKey, entry *evidenceEntry) {
	delete(c.loading, key)
	if _, ok := c.entries[key]; !ok {
		c.order = append(c.order, key)
	}
	c.entries[key] = entry
	c.touch(key)
}

// evict trims the cache to its limit, keeping the keys protected holds as
// displayed — evicting the images on screen would only churn reloads, so
// while one item's evidence shows the cache may hold a few entries more
// than its limit (bounded by that one item's lines).
func (c *evidenceCache) evict(protected map[evidenceKey]struct{}) {
	for len(c.order) > c.limit {
		i := 0
		for i < len(c.order) {
			if _, keep := protected[c.order[i]]; !keep {
				break
			}
			i++
		}
		if i == len(c.order) {
			return // everything held is on screen
		}
		delete(c.entries, c.order[i])
		c.order = append(c.order[:i], c.order[i+1:]...)
	}
}

// touch moves key to the most-recently-used end.
func (c *evidenceCache) touch(key evidenceKey) {
	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			c.order = append(c.order, key)
			return
		}
	}
}

// evidenceFor returns the cached outcome of one evidence image of record
// recordID, scheduling its load when absent: the view shows a placeholder
// until the load lands through update, keyed by identity so a completed
// load can never attach to another item. The load's arrival trims the
// cache around what is on screen.
func (m *Model) evidenceFor(recordID, id string) *evidenceEntry {
	if m.cache.load == nil || id == "" {
		return nil
	}
	key := evidenceKey{record: recordID, id: id}
	if entry := m.cache.get(key); entry != nil {
		return entry
	}
	if !m.cache.begin(key) {
		return nil // already loading; the placeholder stays
	}
	m.goAsync(func() {
		entry := &evidenceEntry{}
		entry.bitmap, entry.err = m.cache.load.Evidence(recordID, id)
		m.update(func() {
			m.cache.put(key, entry)
			m.cache.evict(m.protectedEvidence())
		})
	})
	return nil
}

// protectedEvidence returns the evidence identities the details pane shows
// right now — the chosen row's — which eviction keeps.
func (m *Model) protectedEvidence() map[evidenceKey]struct{} {
	if m.rec == nil || m.selected < 0 || m.selected >= len(m.rec.Rows) {
		return nil
	}
	ids := m.rec.Rows[m.selected].EvidenceIDs
	set := make(map[evidenceKey]struct{}, len(ids))
	for _, id := range ids {
		set[evidenceKey{record: m.rec.ID, id: id}] = struct{}{}
	}
	return set
}
