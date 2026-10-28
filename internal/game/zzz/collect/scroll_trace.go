package collect

import (
	"fmt"
	"image"
	"image/draw"
	"time"

	"github.com/LoTwT/youkai/internal/platform"
)

// Bounds of one scroll wait's frame trace. A failed wait writes the whole
// retained sequence, so the limits bound both the walk's memory and the
// dump's size. A 5s wait can deliver roughly 280 frames, far more than the
// byte cap holds, so retention is head plus tail: the first frames of the
// drag's effect give the initial displacement context, the newest frames
// show the settling or the deadline, and the first non-stationary frame
// and the latest candidate are pinned regardless of where they fall.
const (
	maxScrollTraceFrames = 64
	maxScrollTraceBytes  = 64 << 20
	// maxScrollTraceHeadFrames counts the pre-drag baseline, so the trace
	// keeps the baseline and the first 15 received frames.
	maxScrollTraceHeadFrames = 16
)

// scrollTrace retains the frames one awaitScroll wait received, cropped to
// the region the scroll verdict reads. Recording never influences a
// decision; it only exists so a failed settle can be replayed from the
// field.
type scrollTrace struct {
	region     image.Rectangle
	maxFrames  int
	maxBytes   int
	headFrames int
	frames     []tracedFrame
	bytes      int
	// nextSeq gives every recorded frame a stable identity even when a
	// reused frame repeats its ID.
	nextSeq uint64
	// firstMovedSeq is the first frame whose total comparison was not
	// Stationary; candidateSeq is the latest candidate. Both stay in the
	// sequence when the caps trim the middle.
	firstMovedSeq uint64
	candidateSeq  uint64
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
	seq       uint64
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
		region:     layout.ListViewport.Union(layout.ScrollTrack),
		maxFrames:  maxScrollTraceFrames,
		maxBytes:   maxScrollTraceBytes,
		headFrames: maxScrollTraceHeadFrames,
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
	t.nextSeq++
	t.frames = append(t.frames, tracedFrame{
		seq:       t.nextSeq,
		id:        f.ID,
		arrivedAt: f.ArrivedAt,
		reused:    f.Reused,
		rect:      rect,
		image:     crop,
	})
	t.bytes += 4 * rect.Dx() * rect.Dy()
	t.trim()
}

// trim enforces the caps by dropping the oldest unpinned frame from the
// middle, which leaves the head segment and the newest frames intact. Only
// a pathological cap that cannot fit the head, the pins and the newest
// frame sacrifices head frames; the baseline, the pins and the newest frame
// are never dropped.
func (t *scrollTrace) trim() {
	for len(t.frames) > 2 && (len(t.frames) > t.maxFrames || t.bytes > t.maxBytes) {
		victim := t.droppableFrom(t.headFrames)
		if victim < 0 {
			victim = t.droppableFrom(1)
		}
		if victim < 0 {
			return
		}
		t.drop(victim)
	}
}

// droppableFrom returns the oldest frame at or after start that is neither
// pinned nor the newest frame, or -1 when none exists.
func (t *scrollTrace) droppableFrom(start int) int {
	if start < 1 {
		start = 1
	}
	for i := start; i < len(t.frames)-1; i++ {
		seq := t.frames[i].seq
		if seq == t.firstMovedSeq || seq == t.candidateSeq {
			continue
		}
		return i
	}
	return -1
}

// drop removes one retained frame and updates the byte accounting.
func (t *scrollTrace) drop(i int) {
	gone := t.frames[i]
	t.frames = append(t.frames[:i], t.frames[i+1:]...)
	size := 4 * gone.rect.Dx() * gone.rect.Dy()
	t.bytes -= size
	t.dropped++
	t.droppedBytes += size
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
// to the newest entry carrying f's ID. The first frame that is not
// Stationary against the baseline is pinned: it is the earliest evidence
// of the drag's effect, even when a long animation leaves it in the middle
// of the sequence.
func (t *scrollTrace) noteBefore(f platform.Frame, sm ScrollMatch) {
	entry := t.entry(f.ID)
	if entry == nil {
		return
	}
	entry.vsBefore = &sm
	if !sm.Stationary && t.firstMovedSeq == 0 {
		t.firstMovedSeq = entry.seq
	}
}

// pinCandidate keeps the candidate frame through later trims. Replacing the
// candidate replaces the pin, so the wait's final candidate is always in
// the dump without accumulating one frame per observed candidate.
func (t *scrollTrace) pinCandidate(f platform.Frame) {
	if t == nil {
		return
	}
	if entry := t.entry(f.ID); entry != nil {
		t.candidateSeq = entry.seq
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
	t.pinCandidate(candidate)
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
	head := t.headFrames - 1
	if head < 0 {
		head = 0
	}
	return fmt.Sprintf("keep the pre-drag baseline, the next %d frames, the first "+
		"frame that was not stationary against the baseline, the current "+
		"candidate and the newest frames; when a limit is exceeded, drop the "+
		"oldest unpinned frame from the middle, then from the head as a last resort", head)
}
