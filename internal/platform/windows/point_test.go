package windows

import (
	"math/bits"
	"testing"
)

// TestPackPoint pins the x64 layout of a by-value POINT argument: X in
// the low 32 bits and the int32 bit pattern of Y in the high 32 bits.
// Negative coordinates are real screen coordinates on secondary
// displays placed left of or above the primary monitor.
func TestPackPoint(t *testing.T) {
	if bits.UintSize != 64 {
		t.Skip("packPoint is defined for 64-bit uintptr targets; the package targets windows/amd64")
	}

	cases := []struct {
		name string
		p    point
		want uint64
	}{
		{"origin", point{X: 0, Y: 0}, 0x0000000000000000},
		{"field point", point{X: 836, Y: 386}, 0x0000018200000344},
		{"negative display origin", point{X: -100, Y: -50}, 0xffffffceffffff9c},
		{"negative y only", point{X: 0, Y: -1}, 0xffffffff00000000},
		{"negative x only", point{X: -1, Y: 0}, 0x00000000ffffffff},
		{"mixed signs", point{X: 1, Y: -2}, 0xfffffffe00000001},
		{"int32 minimum both axes", point{X: -1 << 31, Y: -1 << 31}, 0x8000000080000000},
		{"int32 maximum both axes", point{X: 1<<31 - 1, Y: 1<<31 - 1}, 0x7fffffff7fffffff},
		{"x minimum y maximum", point{X: -1 << 31, Y: 1<<31 - 1}, 0x7fffffff80000000},
		{"x maximum y minimum", point{X: 1<<31 - 1, Y: -1 << 31}, 0x800000007fffffff},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := uint64(packPoint(tc.p)); got != tc.want {
				t.Fatalf("packPoint(%+v) = %#016x, want %#016x", tc.p, got, tc.want)
			}
		})
	}
}
