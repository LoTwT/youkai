package collect

import (
	"image"
	"image/draw"
	"time"

	"github.com/LoTwT/youkai/internal/platform"
)

// Bounds of one scroll wait's frame trace. A failed wait writes the whole
// retained sequence, so the limits bound both the walk's memory and the
// dump's size. The first frame and the newest frames survive: the middle
// of a long animation is the least useful evidence, while the pre-drag
// baseline and the frames near the deadline are what the settle decision
// rests on.
const (
	maxScrollTraceFrames = 64
	maxScrollTraceBytes  = 64 << 20
)

// scrollTrace retains the frames one awaitScroll wait received, cropped to
// the region the scroll verdict reads. Recording never influences a
// decision; it only exists so a failed settle can be replayed from the
// field.
type scrollTrace struct {
	region    image.Rectangle
	maxFrames int
	maxBytes  int
	frames    []tracedFrame
	bytes     int
	// dropped and droppedBytes describe what the caps removed, so the
	// manifest can state which part of the sequence is missing.
	dropped      int
	droppedBytes int
}

// tracedFrame is one retained observation. Comparisons are the matcher
// results the wait itself produced for this frame; they are attached at
// the moment awaitScroll evaluates the pair, so the manifest carries the
// exact numbers the traversal acted on instead of an approximation.
type tracedFrame struct {
	id        uint64
	arrivedAt time.Time
	reused    bool
	// rect is the crop's region in the full frame; image is that crop.
	rect  image.Rectangle
	image *image.RGBA
	// vsBefore is the latest comparison with the pre-drag frame; nil when
	// the wait never evaluated this frame against it.
	vsBefore *ScrollMatch
	// vsCandidate is the latest comparison with the candidate current at
	// the time; candidateID names that candidate.
	vsCandidate *ScrollMatch
	candidateID uint64
}

// newScrollTrace starts a trace with the pre-drag baseline frame, which
// the failure dump must always keep even when the wait receives no further
// frame.
func newScrollTrace(layout Layout, before platform.Frame) *scrollTrace {
	t := &scrollTrace{
		region:    layout.ListViewport.Union(layout.ScrollTrack),
		maxFrames: maxScrollTraceFrames,
		maxBytes:  maxScrollTraceBytes,
	}
	t.record(before)
	return t
}

// record appends one received frame's crop and enforces the caps.
func (t *scrollTrace) record(f platform.Frame) {
	if t == nil {
		return
	}
	crop, rect := cropTraceRegion(f.Image, t.region)
	t.frames = append(t.frames, tracedFrame{
		id:        f.ID,
		arrivedAt: f.ArrivedAt,
		reused:    f.Reused,
		rect:      rect,
		image:     crop,
	})
	t.bytes += 4 * rect.Dx() * rect.Dy()
	t.trim()
}

// trim drops the oldest frame after the baseline until both caps hold. The
// baseline at index 0 and the newest frame are never dropped; with a cap
// of at least two frames there is always something to remove.
func (t *scrollTrace) trim() {
	for len(t.frames) > 2 && (len(t.frames) > t.maxFrames || t.bytes > t.maxBytes) {
		gone := t.frames[1]
		t.frames = append(t.frames[:1], t.frames[2:]...)
		t.bytes -= 4 * gone.rect.Dx() * gone.rect.Dy()
		t.dropped++
		t.droppedBytes += 4 * gone.rect.Dx() * gone.rect.Dy()
	}
}

// ensure appends f when the sequence does not already carry its ID. The
// failure dump uses it for the before/after pair, which must appear even
// when the wait observed no such frame.
func (t *scrollTrace) ensure(f platform.Frame) {
	if t == nil || f.ID == 0 {
		return
	}
	for i := range t.frames {
		if t.frames[i].id == f.ID {
			return
		}
	}
	t.record(f)
}

// noteBefore attaches a total comparison (relative to the pre-drag frame)
// to the newest entry carrying f's ID.
func (t *scrollTrace) noteBefore(f platform.Frame, sm ScrollMatch) {
	if entry := t.entry(f.ID); entry != nil {
		entry.vsBefore = &sm
	}
}

// noteCandidate attaches a candidate-relative comparison. The candidate is
// the frame the settle rule compared against at that moment; it is not
// necessarily the wait's final candidate, so the entry records its ID.
func (t *scrollTrace) noteCandidate(candidate, f platform.Frame, sm ScrollMatch) {
	if entry := t.entry(f.ID); entry != nil {
		entry.vsCandidate = &sm
		entry.candidateID = candidate.ID
	}
}

// entry returns the newest recorded frame with id.
func (t *scrollTrace) entry(id uint64) *tracedFrame {
	for i := len(t.frames) - 1; i >= 0; i-- {
		if t.frames[i].id == id {
			return &t.frames[i]
		}
	}
	return nil
}

// cropTraceRegion copies the region out of img. A nil image or an empty
// intersection yields no crop; the frame's metadata is still traced.
func cropTraceRegion(img *image.RGBA, region image.Rectangle) (*image.RGBA, image.Rectangle) {
	if img == nil {
		return nil, image.Rectangle{}
	}
	r := region.Intersect(img.Bounds())
	if r.Empty() {
		return nil, image.Rectangle{}
	}
	crop := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	draw.Draw(crop, crop.Bounds(), img, r.Min, draw.Src)
	return crop, r
}

// retentionPolicy describes the caps in the failure manifest so a reader
// knows which frames the sequence is missing and why.
func (t *scrollTrace) retentionPolicy() string {
	return "keep the pre-drag baseline and the newest frames; when a limit is " +
		"exceeded, drop the oldest frame after the baseline"
}
