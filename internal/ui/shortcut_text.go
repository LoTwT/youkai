package ui

import (
	"runtime"
	"strings"

	"github.com/LoTwT/youkai/internal/platform"
)

// ui.Cmd maps to Command on macOS and Ctrl on Windows. All visible hints
// use this table, including toolbar, state and disabled-action text.
var (
	// shortcutStartScan starts a scan from the prepare page.
	shortcutStartScan = commandKey(runtime.GOOS) + "+R"
	// shortcutStopScan is the global stop hotkey registered while a scan
	// session runs.
	shortcutStopScan = stopShortcut(runtime.GOOS)
	// shortcutConfirmNext saves the draft and moves to the next pending
	// item.
	shortcutConfirmNext = commandKey(runtime.GOOS) + "+Enter"
	// shortcutSave saves the draft without confirming it.
	shortcutSave = commandKey(runtime.GOOS) + "+S"
	// shortcutNextPending moves to the next pending item.
	shortcutNextPending = commandKey(runtime.GOOS) + "+J"
	// shortcutPreviousPending moves to the previous pending item.
	shortcutPreviousPending = commandKey(runtime.GOOS) + "+Shift+J"
	// shortcutZoomIn enlarges the screenshots.
	shortcutZoomIn = commandKey(runtime.GOOS) + "+="
	// shortcutZoomOut shrinks the screenshots.
	shortcutZoomOut = commandKey(runtime.GOOS) + "+-"
	// shortcutZoomFit fits the screenshots to the area.
	shortcutZoomFit = commandKey(runtime.GOOS) + "+0"
	// shortcutExport opens the record's export dialog from the record
	// workspace.
	shortcutExport = commandKey(runtime.GOOS) + "+E"
	// shortcutFocusRegion moves the keyboard between the page's large
	// regions.
	shortcutFocusRegion = "F6"
	// shortcutFocusRegionBack moves the keyboard back through the same
	// regions.
	shortcutFocusRegionBack = "Shift+F6"
	// shortcutOpenRecord opens the chosen record in the records list.
	shortcutOpenRecord = "Enter"
	// shortcutCloseOverlay closes an overlay or returns to the page's
	// table.
	shortcutCloseOverlay = "Esc"
)

func commandKey(goos string) string {
	if goos == "darwin" {
		return "Command"
	}
	return "Ctrl"
}

func stopShortcut(goos string) string {
	spec := platform.StopScanHotkey(goos)
	ctrl, alt := "Ctrl", "Alt"
	if goos == "darwin" {
		ctrl, alt = "Control", "Option"
	}
	var keys []string
	if spec.Ctrl {
		keys = append(keys, ctrl)
	}
	if spec.Alt {
		keys = append(keys, alt)
	}
	if spec.Shift {
		keys = append(keys, "Shift")
	}
	if spec.Meta {
		keys = append(keys, "Command")
	}
	return strings.Join(append(keys, string(spec.Key)), "+")
}
