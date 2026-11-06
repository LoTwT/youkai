package ui

import "github.com/egoist/mygo/ui"

// registerPageShortcuts wires the keys the page shell owns across
// destinations. Escape keeps the design's order: MyGo's overlays (menus,
// Select popovers) take it first, then a held navigation is cancelled, then
// the focus returns to the page's table. Ctrl+R exists only while the scan
// prepare page can actually start a scan; it is not a global key. Ctrl+E
// opens the record's export dialog, and F6 / Shift+F6 move between the
// review's item list, screenshots and form (other pages keep the rail and
// the page's content).
func (m *Model) registerPageShortcuts(c *ui.Context) {
	if m.exportDialog.open {
		// The modal owns the keyboard while it is open: Esc belongs to it
		// (and is ignored during an export), and page keys must not move
		// the focus behind the backdrop.
		return
	}
	c.OnShortcut(0, ui.KeyEscape, func() { m.handleEscapeShortcut(c) })
	if m.scanStartReady() {
		c.OnShortcut(ui.Cmd, ui.KeyR, func() { m.requestScanStart() })
	}
	if m.displayPage() == pageResults && !m.exportEntryBlocked() {
		c.OnShortcut(ui.Cmd, ui.KeyE, func() { m.requestExport() })
	}
	c.OnShortcut(0, ui.KeyF6, func() { m.focusPageContent(c, 1) })
	c.OnShortcut(ui.Shift, ui.KeyF6, func() { m.focusPageContent(c, -1) })
}

// focusPageContent moves the keyboard between the page's large regions.
// The review view has three (item list, screenshots, form); elsewhere F6
// enters the page's main region and Shift+F6 returns to the rail.
func (m *Model) focusPageContent(c *ui.Context, direction int) {
	switch m.displayPage() {
	case pageResults:
		if m.recordTab == recordTabItems {
			m.focusReviewRegion(c, direction)
			return
		}
		if direction < 0 {
			m.focusNavRail(c)
			return
		}
		m.workspaceTabsRef.Focus(c)
	case pageHistory:
		if direction < 0 {
			m.focusNavRail(c)
			return
		}
		m.historyList.Focus(c)
	case pageScan:
		if direction < 0 {
			m.focusNavRail(c)
			return
		}
		m.scanActionRef.Focus(c)
	case pageSettings:
		if direction < 0 {
			m.focusNavRail(c)
			return
		}
		m.settingsFirstRef.Focus(c)
	}
}

// focusReviewRegion cycles the review's three regions. The form step also
// focuses the first problem field of the chosen item.
func (m *Model) focusReviewRegion(c *ui.Context, direction int) {
	next := (int(m.reviewRegion) + direction + int(reviewRegionCount)) % int(reviewRegionCount)
	m.reviewRegion = reviewRegion(next)
	switch m.reviewRegion {
	case reviewRegionList:
		if m.reviewStandard() {
			// The list is built after the page shortcuts this frame;
			// the request lands when its rows are built.
			m.focusListRequested = true
		} else {
			// The compact profile's list is the strip's popover.
			m.reviewCompactRef.Focus(c)
		}
	case reviewRegionCrops:
		m.reviewCropsRef.Focus(c)
	case reviewRegionForm:
		m.focusReviewForm()
	}
}

// focusNavRail returns the keyboard to the rail's active destination.
func (m *Model) focusNavRail(c *ui.Context) {
	m.railActiveRef.Focus(c)
}

// scanStartReady reports whether the prepare page may start a scan now:
// the prepare destination is showing, the start guards pass, and no review
// save or export is in flight. The start button, Ctrl+R and the status bar
// all use this answer, so they cannot disagree.
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
// table. Other destinations have no table, so the key is a no-op there.
func (m *Model) focusPageTable(c *ui.Context) {
	switch m.displayPage() {
	case pageResults:
		if m.recordTab == recordTabItems {
			if m.reviewStandard() {
				m.focusListRequested = true
			} else {
				m.reviewCompactRef.Focus(c)
			}
		}
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
