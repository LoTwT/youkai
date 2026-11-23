package collect

import (
	"image"
	"testing"
)

// TestStorageLevelLineExcludesEquippedEmpty pins the 2026-11-23 field level
// OCR failure. The accepted detail crop's equipped-slot "EMPTY" label begins
// at client x=1700; the level line must end at 1645 so the level crop cannot
// concatenate "等级15/15EMPTY" while still containing the level text.
func TestStorageLevelLineExcludesEquippedEmpty(t *testing.T) {
	l := StorageLayout()
	var level image.Rectangle
	for _, line := range l.DetailLines {
		if line.Role == LineLevel {
			level = line.Rect
			break
		}
	}
	if level.Empty() {
		t.Fatal("drive-storage level line is missing")
	}
	if level.Max.X > 1645 {
		t.Fatalf("level line right edge = %d, want <= 1645 so it excludes the equipped EMPTY label at x=1700", level.Max.X)
	}
	if level.Max.X < 1632 {
		t.Fatalf("level line right edge = %d, want >= 1632 so the accepted level text is not cut", level.Max.X)
	}
	if level.Min.X != 1448 {
		t.Fatalf("level line left edge = %d, want the calibrated 1448", level.Min.X)
	}
}
