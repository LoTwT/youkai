package macos

import (
	"fmt"
	"math"
	"math/bits"
	"time"
)

// Mach clock conversion (T2 §4.1). ScreenCaptureKit's displayTime is a
// tick count in the mach time domain — the domain of
// mach_absolute_time, which does not include time the system spent
// asleep. It must never be compared against a Go wall clock, and raw
// ticks are meaningless without the timebase. The Go runtime's own
// monotonic clock on darwin is mach_absolute_time scaled by the same
// timebase (nanoseconds = ticks × numer / denom), so an anchored
// conversion produces values directly comparable with time.Now-derived
// monotonic times while the system is awake; divergence across system
// sleep is not bounded by this package and is treated as unknown
// (verified in 实测轮 ①; a locked or sleeping screen stops the scan
// anyway).

// MachTimebase is the mach_timebase_info fraction: one tick equals
// numer/denom nanoseconds.
type MachTimebase struct {
	Numer, Denom uint32
}

// Identity is a test-convenience fake (one tick equals one nanosecond)
// and describes no real host: Apple Silicon hosts report 125/3 (as
// measured on the 实测轮 ① machine; see
// docs/reports/2026-10-18-m2-macos-field-round1.md), and the darwin
// bridge always reads the live fraction from mach_timebase_info.
var MachTimebaseIdentity = MachTimebase{Numer: 1, Denom: 1}

// MachClockSource supplies the raw native clock facts. The darwin
// bridge implements it with mach_absolute_time and mach_timebase_info;
// tests inject deterministic fakes.
type MachClockSource interface {
	// NowTicks returns the current mach tick count.
	NowTicks() uint64
	// Timebase returns the host timebase.
	Timebase() MachTimebase
}

// MachClock converts mach ticks to comparable Go monotonic time. It
// anchors one (ticks, time.Time) pair at construction and converts
// every later tick count relative to that anchor, which keeps frame
// times, arrival times and stop times in a single comparable domain.
type MachClock struct {
	timebase    MachTimebase
	anchorTicks uint64
	anchorTime  time.Time
}

// NewMachClock captures the anchor from source: it records the native
// tick count and a Go time (which carries a monotonic reading when it
// comes from time.Now) in one breath.
func NewMachClock(source MachClockSource) *MachClock {
	return NewMachClockFromAnchor(source, time.Now())
}

// NewMachClockFromAnchor pairs the source's current tick count with an
// explicitly supplied anchor time. It exists for tests and for callers
// that must keep one anchor across several clocks.
func NewMachClockFromAnchor(source MachClockSource, anchor time.Time) *MachClock {
	return &MachClock{
		timebase:    source.Timebase(),
		anchorTicks: source.NowTicks(),
		anchorTime:  anchor,
	}
}

// Timebase returns the host timebase the clock converts with.
func (c *MachClock) Timebase() MachTimebase { return c.timebase }

// AnchorTicks returns the anchor's raw tick count.
func (c *MachClock) AnchorTicks() uint64 { return c.anchorTicks }

// AnchorTime returns the anchor's Go time.
func (c *MachClock) AnchorTime() time.Time { return c.anchorTime }

// NanosBetween converts the tick difference from→to into a duration
// (negative when to is before from). The computation is overflow-safe
// for any realistic tick counts and saturates at the duration bounds
// instead of wrapping.
func (tb MachTimebase) NanosBetween(from, to uint64) time.Duration {
	if from == to {
		return 0
	}
	if to > from {
		return saturateDuration(tb.ticksToNanos(to - from))
	}
	return -saturateDuration(tb.ticksToNanos(from - to))
}

// ticksToNanos converts a non-negative tick delta to nanoseconds using
// 128-bit intermediate math, saturating at math.MaxInt64.
func (tb MachTimebase) ticksToNanos(ticks uint64) int64 {
	if ticks == 0 {
		return 0
	}
	if tb.Denom == 0 {
		// A zero denominator is impossible from mach_timebase_info; treat
		// the timebase as identity rather than dividing by zero.
		return saturateInt64(ticks)
	}
	hi, lo := bits.Mul64(ticks, uint64(tb.Numer))
	denom := uint64(tb.Denom)
	if hi >= denom {
		// The quotient would not fit a uint64; saturate.
		return math.MaxInt64
	}
	q, rem := bits.Div64(hi, lo, denom)
	// Round to nearest, preferring away from zero on ties.
	if rem != 0 {
		if rem >= (denom+1)/2 {
			if q == math.MaxInt64 {
				return math.MaxInt64
			}
			q++
		}
	}
	return saturateInt64(q)
}

func saturateInt64(q uint64) int64 {
	if q > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(q)
}

func saturateDuration(n int64) time.Duration {
	if n > int64(math.MaxInt64) {
		return time.Duration(math.MaxInt64)
	}
	if n < int64(math.MinInt64) {
		return time.Duration(math.MinInt64)
	}
	return time.Duration(n)
}

// ToTime converts a mach tick count to a Go time comparable with other
// times derived from this clock (and, while the system stayed awake,
// with time.Now). Ticks before the anchor extrapolate backwards; the
// result is still internally consistent. A zero tick count is a
// meaningful value to mach, so absence must be handled by the caller
// before converting.
func (c *MachClock) ToTime(machTicks uint64) time.Time {
	delta := c.timebase.NanosBetween(c.anchorTicks, machTicks)
	return c.anchorTime.Add(delta)
}

// FormatTicks is a small diagnostic helper describing a tick count in
// its raw and converted form.
func (c *MachClock) FormatTicks(machTicks uint64) string {
	return fmt.Sprintf("%d ticks (timebase %d/%d)", machTicks, c.timebase.Numer, c.timebase.Denom)
}
