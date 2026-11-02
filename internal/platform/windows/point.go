package windows

// point is the Win32 POINT layout.
type point struct{ X, Y int32 }

// packPoint packs a Win32 POINT into the single argument slot the
// Windows x64 calling convention uses for an 8-byte structure passed by
// value: X in the low 32 bits, Y in the high 32 bits. The uint32
// conversions keep the bit pattern of negative coordinates, which occur
// on displays left of or above the primary monitor.
//
// The uint64 intermediate keeps 32-bit builds type-check and vet clean;
// the packed value itself is only meaningful where uintptr is 64 bits.
// This package pins its Win32 structures to windows/amd64 (T2 §2.1), so
// the caller never runs on a 32-bit platform.
func packPoint(p point) uintptr {
	return uintptr(uint64(uint32(p.X)) | uint64(uint32(p.Y))<<32)
}
