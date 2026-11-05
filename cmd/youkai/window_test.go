package main

import (
	"testing"

	"github.com/egoist/mygo"
)

// TestWindowPlacementFor checks the design's window bounds: 1180 × 760 by
// default, 92% of a smaller work area, a 960 × 640 minimum that yields to
// an even smaller work area (design 09 §1).
func TestWindowPlacementFor(t *testing.T) {
	for _, tc := range []struct {
		name     string
		workArea mygo.Rectangle
		want     windowPlacement
	}{
		{
			name:     "4K at 125%",
			workArea: mygo.Rectangle{Width: 3072, Height: 1680},
			want:     windowPlacement{Width: 1180, Height: 760, MinWidth: 960, MinHeight: 640},
		},
		{
			name:     "1080p at 125%",
			workArea: mygo.Rectangle{Width: 1536, Height: 816},
			want:     windowPlacement{Width: 1180, Height: 760, MinWidth: 960, MinHeight: 640},
		},
		{
			name:     "1080p at 100%",
			workArea: mygo.Rectangle{Width: 1920, Height: 1032},
			want:     windowPlacement{Width: 1180, Height: 760, MinWidth: 960, MinHeight: 640},
		},
		{
			name:     "1366x768 at 100%",
			workArea: mygo.Rectangle{Width: 1366, Height: 728},
			want:     windowPlacement{Width: 1180, Height: 669, MinWidth: 960, MinHeight: 640},
		},
		{
			name:     "1366x768 at 125%",
			workArea: mygo.Rectangle{Width: 1093, Height: 614},
			want:     windowPlacement{Width: 1005, Height: 614, MinWidth: 960, MinHeight: 614},
		},
		{
			name:     "smaller than the minimum",
			workArea: mygo.Rectangle{Width: 800, Height: 500},
			want:     windowPlacement{Width: 800, Height: 500, MinWidth: 800, MinHeight: 500},
		},
		{
			name: "unknown work area",
			want: windowPlacement{Width: 1180, Height: 760, MinWidth: 960, MinHeight: 640},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := windowPlacementFor(tc.workArea); got != tc.want {
				t.Fatalf("windowPlacementFor(%+v) = %+v, want %+v", tc.workArea, got, tc.want)
			}
		})
	}
}
