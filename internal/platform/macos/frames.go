package macos

import (
	"errors"
	"fmt"
	"sync"
)

// Frame status mapping and the bounded latest-frame cache (T2 §4.2):
// a continuous stream may keep a bounded cache of the latest available
// frames, but the evidence tasks the kernel already admitted are a
// different ledger and must never be overwritten by later frames —
// that separation is the kernel's, and this cache is only the
// backend-side latest-frame buffer.

// NativeFrameStatus is the raw SCFrameStatus value of one frame
// callback, preserved verbatim. The numeric values are the enum's
// (complete=0 … stopped=5, verified against the SDK headers);
// NativeFrameStatusMissing marks a callback without a status
// attachment.
type NativeFrameStatus int32

const (
	NativeFrameStatusMissing NativeFrameStatus = -1
	NativeFrameComplete      NativeFrameStatus = 0
	NativeFrameIdle          NativeFrameStatus = 1
	NativeFrameBlank         NativeFrameStatus = 2
	NativeFrameSuspended     NativeFrameStatus = 3
	NativeFrameStarted       NativeFrameStatus = 4
	NativeFrameStopped       NativeFrameStatus = 5
)

// FrameStatus maps the raw value to the semantic status; unrecognized
// values map to FrameUnknown and the raw value stays available to the
// caller.
func (s NativeFrameStatus) FrameStatus() FrameStatus {
	switch s {
	case NativeFrameComplete:
		return FrameComplete
	case NativeFrameIdle:
		return FrameIdle
	case NativeFrameBlank:
		return FrameBlank
	case NativeFrameSuspended:
		return FrameSuspended
	case NativeFrameStarted:
		return FrameStarted
	case NativeFrameStopped:
		return FrameStopped
	default:
		return FrameUnknown
	}
}

// NativeFrame is one frame callback as it crosses the native boundary,
// before any session bookkeeping. Presence flags distinguish "absent
// attachment" from a legitimately zero value. Pixels are a Go-owned
// copy (BGRA, BytesPerRow stride); the native buffer is not referenced
// after the callback returns.
type NativeFrame struct {
	// Status is the raw frame status; Missing when the attachment was
	// absent.
	Status NativeFrameStatus
	// DisplayTimeMach is the raw mach tick count of the system frame
	// time; HasDisplayTime distinguishes absence.
	DisplayTimeMach uint64
	HasDisplayTime  bool
	// SurfaceWidth, SurfaceHeight and BytesPerRow describe the output
	// surface; zero when the sample carried no pixel buffer.
	SurfaceWidth, SurfaceHeight, BytesPerRow int
	// ContentRect is the content rect in points within the surface.
	ContentRect    PointRect
	HasContentRect bool
	// ContentScale is the original-content-to-surface scale.
	ContentScale    float64
	HasContentScale bool
	// ScaleFactor is pixels per point within the surface.
	ScaleFactor    float64
	HasScaleFactor bool
	// ScreenRect is the captured content's on-screen rect in points
	// (macOS 13.1+ attachment); for a window stream it is the window's
	// current screen rect and is preferred over the enumeration-time
	// frame.
	ScreenRect    PointRect
	HasScreenRect bool
	// Pixels is the copied image data; nil when the sample carried no
	// pixel buffer (idle, blank, suspended and stopped frames commonly
	// do not).
	Pixels []byte
}

// LatestFrameCache is the bounded buffer of the latest available
// frames. Capacity is fixed at construction; offering a frame to a
// full cache evicts the oldest entry, so the cache never grows and the
// newest frame is always available. It is a plain buffer: session
// tokening, intake closing and late-frame rejection live in the
// capture session, not here.
type LatestFrameCache struct {
	mu     sync.Mutex
	cap    int
	frames []ImageObservation
}

// NewLatestFrameCache builds a cache holding at most capacity frames;
// capacity below one is an error because a zero-capacity cache could
// never return what it accepted.
func NewLatestFrameCache(capacity int) (*LatestFrameCache, error) {
	if capacity < 1 {
		return nil, fmt.Errorf("macos: latest-frame cache capacity %d below one", capacity)
	}
	return &LatestFrameCache{cap: capacity}, nil
}

// Offer appends one frame observation, evicting the oldest when full,
// and reports whether the frame is now cached (always true).
func (c *LatestFrameCache) Offer(obs ImageObservation) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.frames) == c.cap {
		copy(c.frames, c.frames[1:])
		c.frames[len(c.frames)-1] = obs
		return true
	}
	c.frames = append(c.frames, obs)
	return true
}

// Latest returns the newest cached observation.
func (c *LatestFrameCache) Latest() (ImageObservation, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.frames) == 0 {
		return ImageObservation{}, false
	}
	return c.frames[len(c.frames)-1], true
}

// Len returns the number of cached frames.
func (c *LatestFrameCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.frames)
}

// Snapshot returns the cached frames oldest-first.
func (c *LatestFrameCache) Snapshot() []ImageObservation {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]ImageObservation, len(c.frames))
	copy(out, c.frames)
	return out
}

// ErrStreamClosed reports that a capture session no longer accepts
// frame callbacks; late callbacks are rejected with it.
var ErrStreamClosed = errors.New("macos: capture session intake closed")
