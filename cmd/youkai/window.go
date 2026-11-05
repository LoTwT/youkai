package main

import "github.com/egoist/mygo"

// The main window's hard bounds (design 09 §1, R-12): 1180 × 760 DIP by
// default, 960 × 640 DIP at minimum, 92% of a smaller work area, and a
// minimum that yields when the work area cannot hold it.
const (
	windowDefaultWidth  = 1180
	windowDefaultHeight = 760
	windowMinWidth      = 960
	windowMinHeight     = 640
	windowWorkAreaScale = 0.92
)

// windowStateKey is the identity MyGo stores the window's position, size
// and maximized state under, so the next launch restores them. One main
// window means one key.
const windowStateKey = "main"

// windowPlacement is the size windowPlacementFor resolves for the main
// window.
type windowPlacement struct {
	Width, Height       int
	MinWidth, MinHeight int
}

// windowPlacementFor returns the main window's default and minimum sizes
// for a display work area in DIPs. The default size falls back to 92% of
// the work area when the work area is smaller than 1180 × 760; it never
// drops below the minimum. A work area that cannot even hold the minimum
// reduces the minimum to itself, and the content scrolls (design 09 §1).
func windowPlacementFor(workArea mygo.Rectangle) windowPlacement {
	width, height := windowDefaultWidth, windowDefaultHeight
	minWidth, minHeight := windowMinWidth, windowMinHeight
	if workArea.Width > 0 && workArea.Width < minWidth {
		minWidth = workArea.Width
	}
	if workArea.Height > 0 && workArea.Height < minHeight {
		minHeight = workArea.Height
	}
	if workArea.Width > 0 && workArea.Width < width {
		width = int(float64(workArea.Width) * windowWorkAreaScale)
	}
	if workArea.Height > 0 && workArea.Height < height {
		height = int(float64(workArea.Height) * windowWorkAreaScale)
	}
	if width < minWidth {
		width = minWidth
	}
	if height < minHeight {
		height = minHeight
	}
	return windowPlacement{Width: width, Height: height, MinWidth: minWidth, MinHeight: minHeight}
}
