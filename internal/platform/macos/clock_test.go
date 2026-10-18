package macos_test

import (
	"math"
	"testing"
	"time"

	macos "github.com/LoTwT/youkai/internal/platform/macos"
)

// Mach clock conversion tests (T2 §4.1): displayTime lives in the mach
// tick domain, must never be compared against a wall clock, and the
// anchored conversion must be overflow-safe, direction-aware and
// consistent with the runtime's monotonic domain assumptions.

type fakeClockSource struct {
	ticks uint64
	tb    macos.MachTimebase
}

func (s *fakeClockSource) NowTicks() uint64             { return s.ticks }
func (s *fakeClockSource) Timebase() macos.MachTimebase { return s.tb }

func TestTimebaseIdentityConvertsTicksToNanos(t *testing.T) {
	tb := macos.MachTimebaseIdentity
	if got := tb.NanosBetween(0, 1_000_000_000); got != time.Second {
		t.Fatalf("identity timebase: %v", got)
	}
	if got := tb.NanosBetween(5, 5); got != 0 {
		t.Fatalf("same ticks: %v", got)
	}
	if got := tb.NanosBetween(10, 5); got != -5*time.Nanosecond {
		t.Fatalf("backwards: %v", got)
	}
}

func TestTimebaseFractionalConvertsWithRounding(t *testing.T) {
	// The classic Intel fraction: 125/3 nanoseconds per tick.
	tb := macos.MachTimebase{Numer: 125, Denom: 3}
	if got := tb.NanosBetween(0, 3); got != 125*time.Nanosecond {
		t.Fatalf("3 ticks: %v", got)
	}
	if got := tb.NanosBetween(0, 1); got != 42*time.Nanosecond {
		t.Fatalf("1 tick (125/3, nearest): %v", got)
	}
	// Large tick counts stay exact via 128-bit math, rounded to
	// nearest: 2^40·125/3 = 45812984490666.67 → …967.
	got := tb.NanosBetween(0, 1<<40)
	if int64(got) != 45812984490667 {
		t.Fatalf("2^40 ticks: %d", int64(got))
	}
}

func TestTimebaseSaturatesInsteadOfWrapping(t *testing.T) {
	tb := macos.MachTimebase{Numer: 1 << 20, Denom: 1}
	got := tb.NanosBetween(0, math.MaxUint64)
	if int64(got) != math.MaxInt64 {
		t.Fatalf("overflow did not saturate: %d", int64(got))
	}
}

func TestMachClockAnchorsAndConverts(t *testing.T) {
	src := &fakeClockSource{ticks: 10_000, tb: macos.MachTimebaseIdentity}
	anchor := time.Now()
	clock := macos.NewMachClockFromAnchor(src, anchor)
	if clock.AnchorTicks() != 10_000 {
		t.Fatalf("anchor ticks %d", clock.AnchorTicks())
	}
	// Forward conversion adds the tick delta to the anchor.
	got := clock.ToTime(10_000 + 500_000_000)
	if d := got.Sub(anchor); d != 500*time.Millisecond {
		t.Fatalf("forward delta %v", d)
	}
	// Backward conversion extrapolates before the anchor and stays
	// comparable.
	got = clock.ToTime(9_000)
	if d := got.Sub(anchor); d != -1*time.Microsecond {
		t.Fatalf("backward delta %v", d)
	}
	// A fractional timebase anchors equally well.
	src2 := &fakeClockSource{ticks: 600, tb: macos.MachTimebase{Numer: 125, Denom: 3}}
	clock2 := macos.NewMachClockFromAnchor(src2, anchor)
	if d := clock2.ToTime(600 + 3000).Sub(anchor); d != 125*time.Microsecond {
		t.Fatalf("fractional delta %v", d)
	}
}
