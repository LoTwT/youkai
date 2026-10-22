package ui

import "github.com/egoist/mygo/ui"

// registerPageShortcuts wires the keys the page shell owns across
// destinations. Escape keeps the design's order: MyGo's overlays (menus,
// Select popovers) take it first, then a held navigation is cancelled, then
// the focus returns to the page's table. ⌘R exists only while the scan
// prepare page can actually start a scan; it is not a global key.
func (m *Model) registerPageShortcuts(c *ui.Context) {
	c.OnShortcut(0, ui.KeyEscape, func() { m.handleEscapeShortcut(c) })
	if m.scanStartReady() {
		c.OnShortcut(ui.Cmd, ui.KeyR, func() { m.requestScanStart() })
	}
}

// scanStartReady reports whether the prepare page may start a scan now:
// the prepare destination is showing, the start guards pass, and no review
// save or export is in flight. The start button, ⌘R and the status bar all
// use this answer, so they cannot disagree.
func (m *Model) scanStartReady() bool {
	return m.displayPage() == pageScan && m.canStart() && !m.reviewSaving && !m.exportBusy()
}

// requestScanStart starts a scan from the prepare page. Unsaved editor
// input holds the start behind the ordinary save/discard/stay prompt; the
// scan only begins once the draft is settled.
func (m *Model) requestScanStart() {
	if !m.scanStartReady() {
		return
	}
	m.requestNavigation(navigationRequest{kind: navStartScan})
}

// handleEscapeShortcut runs after MyGo's overlays have had Escape. The row
// menu normally closes in its PopoverBase before this runs; the explicit
// check keeps the state honest if its overlay was already gone.
func (m *Model) handleEscapeShortcut(c *ui.Context) {
	if m.historyMenuOpen {
		m.closeHistoryMenu()
		return
	}
	if m.navPrompt != nil {
		m.promptStay()
		return
	}
	if m.textInputFocused {
		// MyGo text inputs do not register Escape themselves. Leaving the
		// key unhandled here keeps a focused field first, as the design
		// requires, instead of pulling its focus away.
		return
	}
	m.focusPageTable(c)
}

// focusPageTable returns the keyboard to the visible results or history
// table. Other destinations have no table, and a narrow results detail
// page has none either, so the key is a no-op there.
func (m *Model) focusPageTable(c *ui.Context) {
	switch m.displayPage() {
	case pageResults:
		if m.narrowResults() && m.resultsDetail {
			return
		}
		m.list.Focus(c)
	case pageHistory:
		m.historyList.Focus(c)
	}
}

// noteTextInputFocus records, while the frame builds, that a text field
// holds the keyboard. FocusWithin covers compound fields such as
// SearchField, where the focused element is the inner input.
func (m *Model) noteTextInputFocus(input ui.Element) ui.Element {
	if input.FocusWithin() {
		m.textInputFocused = true
	}
	return input
}
