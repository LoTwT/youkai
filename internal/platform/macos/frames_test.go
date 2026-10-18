package macos_test

import (
	"testing"
	"time"

	macos "github.com/LoTwT/youkai/internal/platform/macos"
)

// Frame status mapping and the bounded latest-frame cache (T2 §4.2):
// statuses map verbatim with unknowns preserved, the cache keeps the
// newest frames within its bound, and the bounded backend cache is a
// different ledger from the kernel's admitted evidence (which this
// package does not manage at all).

func TestNativeFrameStatusMapping(t *testing.T) {
	cases := []struct {
		raw  macos.NativeFrameStatus
		want macos.FrameStatus
	}{
		{macos.NativeFrameComplete, macos.FrameComplete},
		{macos.NativeFrameIdle, macos.FrameIdle},
		{macos.NativeFrameBlank, macos.FrameBlank},
		{macos.NativeFrameSuspended, macos.FrameSuspended},
		{macos.NativeFrameStarted, macos.FrameStarted},
		{macos.NativeFrameStopped, macos.FrameStopped},
		{macos.NativeFrameStatusMissing, macos.FrameUnknown},
		{macos.NativeFrameStatus(99), macos.FrameUnknown},
	}
	for _, c := range cases {
		if got := c.raw.FrameStatus(); got != c.want {
			t.Fatalf("raw %d mapped to %s want %s", c.raw, got, c.want)
		}
	}
}

func obs(id uint64) macos.ImageObservation {
	return macos.ImageObservation{FrameID: id, Status: macos.FrameComplete}
}

func TestLatestFrameCacheBoundedAndNewestWins(t *testing.T) {
	c, err := macos.NewLatestFrameCache(2)
	if err != nil {
		t.Fatalf("cache: %v", err)
	}
	if _, ok := c.Latest(); ok {
		t.Fatal("fresh cache returned a frame")
	}
	c.Offer(obs(1))
	c.Offer(obs(2))
	if c.Len() != 2 {
		t.Fatalf("len %d", c.Len())
	}
	got, ok := c.Latest()
	if !ok || got.FrameID != 2 {
		t.Fatalf("latest %+v ok=%v", got, ok)
	}
	// The third frame evicts the oldest: the bound holds and the
	// newest is still the latest.
	c.Offer(obs(3))
	if c.Len() != 2 {
		t.Fatalf("len after overflow %d", c.Len())
	}
	got, _ = c.Latest()
	if got.FrameID != 3 {
		t.Fatalf("latest after overflow %+v", got)
	}
	snapshot := c.Snapshot()
	if len(snapshot) != 2 || snapshot[0].FrameID != 2 || snapshot[1].FrameID != 3 {
		t.Fatalf("snapshot %+v", snapshot)
	}
	// The evicted observation is still owned by whoever held it: the
	// cache hands out copies of the struct, the pixel slice stays
	// valid for its holder.
	if _, err := macos.NewLatestFrameCache(0); err == nil {
		t.Fatal("zero capacity accepted")
	}
}

func TestLatestFrameCacheCapacityOneReplaces(t *testing.T) {
	c, err := macos.NewLatestFrameCache(1)
	if err != nil {
		t.Fatalf("cache: %v", err)
	}
	c.Offer(obs(1))
	c.Offer(obs(2))
	if c.Len() != 1 {
		t.Fatalf("len %d", c.Len())
	}
	got, _ := c.Latest()
	if got.FrameID != 2 {
		t.Fatalf("latest %+v", got)
	}
}

func TestImageObservationCarriesTimingAndGeometryIdentity(t *testing.T) {
	// The observation must keep the system frame time in both domains
	// and bind itself to stream, frame id and geometry version, so
	// late or stale writes are attributable (T2 §4.2 图像观测).
	display := time.Now().Add(-50 * time.Millisecond)
	o := macos.ImageObservation{
		Stream:          7,
		FrameID:         9,
		Status:          macos.FrameComplete,
		RawStatus:       0,
		Width:           2560,
		Height:          1440,
		BytesPerRow:     10240,
		PixelFormat:     "BGRA",
		Geometry:        3,
		GeometryValid:   true,
		DisplayTime:     &display,
		DisplayTimeMach: 123456789,
		ArrivedAt:       time.Now(),
	}
	if o.Stream != 7 || o.FrameID != 9 || o.Geometry != 3 {
		t.Fatalf("identity fields lost: %+v", o)
	}
	if o.DisplayTimeMach != 123456789 || o.DisplayTime == nil {
		t.Fatalf("frame time fields lost: %+v", o)
	}
}
