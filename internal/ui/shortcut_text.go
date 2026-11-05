package ui

// The single source of the key names the interface shows. The first release
// targets Windows only, so every label uses Windows notation: Ctrl, Alt and
// Shift joined by "+", never the macOS ⌘⌃⌥⇧ symbols (design 09 §4.5, D-02).
// The bindings keep using ui.Cmd, which maps to Ctrl on Windows; a macOS
// port changes only this table and the stop hotkey the entry point
// registers.
const (
	// shortcutStartScan starts a scan from the prepare page.
	shortcutStartScan = "Ctrl+R"
	// shortcutStopScan is the global stop hotkey registered while a scan
	// session runs.
	shortcutStopScan = "Ctrl+Alt+Shift+S"
	// shortcutConfirmNext saves the draft and moves to the next pending
	// item.
	shortcutConfirmNext = "Ctrl+Enter"
	// shortcutSave saves the draft without confirming it.
	shortcutSave = "Ctrl+S"
	// shortcutNextPending moves to the next pending item.
	shortcutNextPending = "Ctrl+J"
	// shortcutExport reveals the record's export controls until the export
	// dialog replaces them.
	shortcutExport = "Ctrl+E"
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
