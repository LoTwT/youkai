package ui

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/game/zzz"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/egoist/mygo/ui"
)

// browseButtonCenter returns the center of a browse button's text box.
func browseButtonCenter(t *testing.T, tt *ui.Tester, label string) (float32, float32) {
	t.Helper()
	rect, ok := tt.Find(label)
	if !ok {
		t.Fatalf("%q is missing: %q", label, tt.Texts())
	}
	return rect.X + rect.W/2, rect.Y + rect.H/2
}

// TestReviewBrowseDoubleClickCannotWrite covers H1: the read-only bar's
// 上一件 / 下一件 can sit at the editor bar's first write button. A second
// click at the same point, or Space / Enter right after the move, must not
// confirm or save the item the user has only just reached. The guard
// follows the pointer, not a clock: only moving the pointer (or pressing a
// key on a write button) makes the action available.
func TestReviewBrowseDoubleClickCannotWrite(t *testing.T) {
	m, q, tt, svc := openReviewableResults(t, recovery(
		settledReviewItem(t, 1, record.QualityAutoAccepted),
		reviewRowItem(t, 2, 12),
		settledReviewItem(t, 3, record.QualityAutoAccepted),
	), 1180, 760)
	tt.Frame()
	m.setResultFilter(resultFilterAll)
	tt.Frame()
	m.requestNavigation(navigationRequest{kind: navSelect, seq: 3})
	settle(t, m, q, tt)
	tt.Frame()

	x, y := browseButtonCenter(t, tt, "‹ 上一件")
	tt.ClickAt(x, y)
	settle(t, m, q, tt)
	tt.Frame()
	if m.selectedSeq() != 2 || m.edit == nil {
		t.Fatalf("‹ 上一件 did not reach the pending item: %s", probeReviewState(m))
	}
	if tt.Focused("确认并下一项") || tt.Focused("保存修改") {
		t.Fatalf("the browse click left the keyboard on a write button: %q", tt.Texts())
	}
	if m.focusedReviewField != FormFieldMainStat {
		t.Fatalf("the browse click did not move the keyboard into the form: %q",
			m.focusedReviewField)
	}

	// The same physical point now belongs to the editor's action bar. A
	// second click there must hit the browse guard, not confirm or save.
	tt.ClickAt(x, y)
	settle(t, m, q, tt)
	if got := svc.saveCount(); got != 0 {
		t.Fatalf("a second click at the browse point wrote %d revisions", got)
	}
	// The keyboard is in the form, so Space / Enter are typing and editing
	// shortcuts, not button activation of the guarded write button.
	tt.Key(0, ui.KeySpace)
	settle(t, m, q, tt)
	tt.Key(0, ui.KeyEnter)
	settle(t, m, q, tt)
	if got := svc.saveCount(); got != 0 {
		t.Fatalf("Space/Enter after the browse wrote %d revisions", got)
	}
	if m.edit == nil || m.edit.seq != 2 {
		t.Fatalf("the guard lost the pending editor: %s", probeReviewState(m))
	}

	// Moving the pointer away releases the guard before the deliberate
	// click on the confirm button.
	tt.Move(x+300, y-300)
	tt.Frame()
	if err := tt.Click("确认并下一项"); err != nil {
		t.Fatalf("click 确认并下一项 after moving the pointer: %v", err)
	}
	settle(t, m, q, tt)
	if got := svc.saveCount(); got != 1 || !svc.saves[0].edit.Confirmed {
		t.Fatalf("the confirmed click after the move = %+v, want one confirmed save", svc.saves)
	}
}

// TestReviewBrowseNextCannotSaveDirtyDraft covers the second half of H1:
// 下一件 lands on the editor's 保存修改. Even with a dirty draft, a click at
// the old point is ignored until the pointer moves; afterward the save
// works.
func TestReviewBrowseNextCannotSaveDirtyDraft(t *testing.T) {
	m, q, tt, svc := openReviewableResults(t, recovery(
		settledReviewItem(t, 1, record.QualityAutoAccepted),
		reviewRowItem(t, 2, 12),
	), 1180, 760)
	tt.Frame()
	m.setResultFilter(resultFilterAll)
	tt.Frame()
	m.requestNavigation(navigationRequest{kind: navSelect, seq: 1})
	tt.Frame()

	x, y := browseButtonCenter(t, tt, "下一件 ›")
	tt.ClickAt(x, y)
	settle(t, m, q, tt)
	tt.Frame()
	if m.selectedSeq() != 2 || m.edit == nil {
		t.Fatalf("下一件 did not open the pending editor: %s", probeReviewState(m))
	}
	m.edit.draft.Level = "13"
	m.edit.refresh()
	tt.Frame()
	if !m.editingDirty() {
		t.Fatal("the pending draft is not dirty")
	}

	// The old 下一件 point now sits on the editor bar. A click repeated
	// there must be ignored, whatever the wait.
	tt.ClickAt(x, y)
	settle(t, m, q, tt)
	if got := svc.saveCount(); got != 0 {
		t.Fatalf("the same-point click saved a dirty draft %d times", got)
	}
	if m.edit == nil || m.edit.draft.Level != "13" {
		t.Fatalf("the guard dropped the dirty draft: %s", probeReviewState(m))
	}

	// A pointer that stays at the old point never releases the guard, even
	// across the interval that used to expire.
	tt.Advance(5 * time.Second)
	tt.Frame()
	tt.ClickAt(x, y)
	settle(t, m, q, tt)
	if got := svc.saveCount(); got != 0 {
		t.Fatalf("the stationary pointer wrote %d revisions after 5s", got)
	}

	tt.Move(x+300, y-300)
	tt.Frame()
	if err := tt.Click("保存修改"); err != nil {
		t.Fatalf("click 保存修改 after moving the pointer: %v", err)
	}
	settle(t, m, q, tt)
	if got := svc.saveCount(); got != 1 || svc.saves[0].edit.Confirmed {
		t.Fatalf("the save after the move = %+v, want one unconfirmed save", svc.saves)
	}
}

// TestReviewBrowseGuardStationaryPointer covers H1's steady walk: repeated
// clicks at the same point must never write, at every common double-click
// pace, because the pointer has not moved between them.
func TestReviewBrowseGuardStationaryPointer(t *testing.T) {
	for _, gap := range []time.Duration{300, 450, 600, 800, 1000} {
		gap *= time.Millisecond
		m, q, tt, svc := openReviewableResults(t, recovery(
			settledReviewItem(t, 1, record.QualityAutoAccepted),
			reviewRowItem(t, 2, 12),
			settledReviewItem(t, 3, record.QualityAutoAccepted),
			settledReviewItem(t, 4, record.QualityAutoAccepted),
		), 1180, 760)
		tt.Frame()
		m.setResultFilter(resultFilterAll)
		tt.Frame()
		m.requestNavigation(navigationRequest{kind: navSelect, seq: 4})
		settle(t, m, q, tt)
		x, y := browseButtonCenter(t, tt, "‹ 上一件")
		for i := 0; i < 5; i++ {
			tt.ClickAt(x, y)
			settle(t, m, q, tt)
			tt.Advance(gap)
			tt.Frame()
		}
		if got := svc.saveCount(); got != 0 {
			t.Fatalf("gap %v: a stationary pointer wrote %d revisions", gap, got)
		}
		if m.selectedSeq() != 2 || m.rec.Rows[1].Quality != record.QualityNeedsReview {
			t.Fatalf("gap %v: the walk changed #0002: %s quality %q",
				gap, probeReviewState(m), m.rec.Rows[1].Quality)
		}
		// A press held at the same point and released later is the same
		// ignored click, not a new deliberate one.
		tt.Press(x, y)
		tt.Advance(400 * time.Millisecond)
		tt.Release(x, y)
		settle(t, m, q, tt)
		if got := svc.saveCount(); got != 0 {
			t.Fatalf("gap %v: the held press wrote %d revisions", gap, got)
		}
	}
}

// TestReviewBrowseGuardReleasesOnMove covers M6: the guard is not a timer,
// so a long pause with no input leaves it in place (the pointer is still at
// the browse point), and one pointer move releases it without any further
// event to wake a timer.
func TestReviewBrowseGuardReleasesOnMove(t *testing.T) {
	m, q, tt, _ := openReviewableResults(t, recovery(
		settledReviewItem(t, 1, record.QualityAutoAccepted),
		reviewRowItem(t, 2, 12),
		settledReviewItem(t, 3, record.QualityAutoAccepted),
	), 1180, 760)
	tt.Frame()
	m.setResultFilter(resultFilterAll)
	tt.Frame()
	m.requestNavigation(navigationRequest{kind: navSelect, seq: 3})
	settle(t, m, q, tt)
	x, y := browseButtonCenter(t, tt, "‹ 上一件")
	tt.ClickAt(x, y)
	settle(t, m, q, tt)
	tt.Advance(5 * time.Second)
	tt.Frame()
	if !tt.HasText(reviewBrowseGuardReason) {
		t.Fatalf("the stationary pointer lost the guard: %q", tt.Texts())
	}
	tt.Move(x+300, y-300)
	tt.Frame()
	if tt.HasText(reviewBrowseGuardReason) {
		t.Fatalf("moving the pointer did not release the guard: %q", tt.Texts())
	}
	if status := m.resultShortcutText(); !strings.Contains(status, shortcutConfirmNext) {
		t.Fatalf("the released bar still hides the confirm shortcut: %q", status)
	}
}

// TestReviewBrowseKeepsKeyboard covers M7: browsing read-only items with
// Space keeps the keyboard on the browse button and the next Space keeps
// browsing; reaching a pending item moves the keyboard to its first
// problem field, never to a write button.
func TestReviewBrowseKeepsKeyboard(t *testing.T) {
	m, q, tt, svc := openReviewableResults(t, recovery(
		settledReviewItem(t, 1, record.QualityAutoAccepted),
		settledReviewItem(t, 2, record.QualityHumanConfirmed),
		settledReviewItem(t, 3, record.QualityHumanConfirmed),
		reviewRowItem(t, 4, 12),
	), 1180, 760)
	tt.Frame()
	m.setResultFilter(resultFilterAll)
	tt.Frame()
	m.requestNavigation(navigationRequest{kind: navSelect, seq: 1})
	settle(t, m, q, tt)
	tt.Frame()
	for i := 0; i < 40 && !tt.Focused("下一件 ›"); i++ {
		tt.Key(0, ui.KeyTab)
		tt.Frame()
	}
	if !tt.Focused("下一件 ›") {
		t.Fatalf("the browse button did not take the keyboard: %q", tt.Texts())
	}
	for want := int64(2); want <= 3; want++ {
		tt.Key(0, ui.KeySpace)
		settle(t, m, q, tt)
		tt.Frame()
		if m.selectedSeq() != want {
			t.Fatalf("Space #%d selected %d, want %d", want, m.selectedSeq(), want)
		}
		if !tt.Focused("下一件 ›") {
			t.Fatalf("Space #%d left the browse button: %q", want, tt.Texts())
		}
		if m.edit != nil {
			t.Fatalf("Space #%d opened the modify flow: %s", want, probeReviewState(m))
		}
	}
	// The next read-only move lands on the pending item: the keyboard goes
	// to the first problem field, not to 修改 or a write button.
	tt.Key(0, ui.KeySpace)
	settle(t, m, q, tt)
	tt.Frame()
	if m.selectedSeq() != 4 || m.edit == nil {
		t.Fatalf("the pending arrival = %s", probeReviewState(m))
	}
	if tt.Focused("修改") || tt.Focused("确认并下一项") || tt.Focused("保存修改") {
		t.Fatalf("the pending arrival focused a modify/write action: %q", tt.Texts())
	}
	if m.focusedReviewField != FormFieldMainStat || !tt.Focused("主词条数值") {
		t.Fatalf("the pending arrival focused %q, want the first problem field: %q",
			m.focusedReviewField, tt.Texts())
	}
	tt.Key(0, ui.KeySpace)
	settle(t, m, q, tt)
	if got := svc.saveCount(); got != 0 {
		t.Fatalf("Space after the pending arrival wrote %d revisions", got)
	}
}

// TestReviewTypingAcrossChangingProblems covers M5's residual: with several
// missing fields, typing in a field that stops having a problem must not
// rebuild it somewhere else and swallow the next keystroke.
func TestReviewTypingAcrossChangingProblems(t *testing.T) {
	it := withStructured(t, reviewRowItem(t, 1, 12), func(s *zzz.StructuredDisc) {
		s.Data.Level = nil
		s.Data.Rarity = nil
	})
	m, q, tt, svc := openReviewableResults(t, recovery(it), 1180, 760)
	tt.Frame()
	m.requestReviewFieldFocus(FormFieldLevel)
	tt.Frame()
	tt.Frame()
	if !tt.Focused("等级") {
		t.Fatalf("the level field did not take the keyboard: %q", tt.Texts())
	}
	tt.Type("1")
	tt.Frame()
	tt.Frame()
	tt.Type("2")
	tt.Frame()
	tt.Frame()
	if m.edit == nil || m.edit.draft.Level != "12" {
		t.Fatalf("typing 12 stored %q", m.edit.draft.Level)
	}
	if !tt.Focused("等级") {
		t.Fatalf("the edited field moved and lost the keyboard: %q", tt.Texts())
	}
	m.edit.draft.Rarity = "S"
	m.edit.refresh()
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyEnter)
	settle(t, m, q, tt)
	if svc.saveCount() != 1 || svc.saves[0].edit.Data.Level == nil ||
		*svc.saves[0].edit.Data.Level != 12 {
		t.Fatalf("the saved revision = %+v, want level 12", svc.saves)
	}
}

// TestReviewNewBlockingProblemStaysVisible covers M-A: a blocking problem
// that appears after the edit opened joins the leading group instead of
// staying in the folded group, and F6 can focus its control.
func TestReviewNewBlockingProblemStaysVisible(t *testing.T) {
	it := withStructured(t, reviewRowItem(t, 1, 12), func(s *zzz.StructuredDisc) {
		s.Data.Slot = nil
	})
	m, _, tt := openResponsiveResults(t, recovery(it), 1180, 760)
	tt.Frame()
	if m.edit == nil || len(m.edit.firstFields) != 1 || m.edit.firstFields[0] != FormFieldSlot {
		t.Fatalf("the edit did not open with the missing slot leading: %s", probeReviewState(m))
	}
	if m.edit.draft.Level != "12" {
		t.Fatalf("unexpected level draft %q", m.edit.draft.Level)
	}

	m.edit.draft.Slot = "1"
	m.edit.refresh()
	tt.Frame()
	tt.Frame()
	if !tt.HasText("槽位 1 不允许主词条") {
		t.Fatalf("the new blocking problem stayed hidden: %q", tt.Texts())
	}
	if m.firstReviewField() != FormFieldMainStat {
		t.Fatalf("the first problem field = %q, want mainStat", m.firstReviewField())
	}

	m.reviewRegion = reviewRegionList
	tt.Frame()
	tt.Key(0, ui.KeyF6)
	tt.Frame()
	tt.Key(0, ui.KeyF6)
	tt.Frame()
	tt.Frame()
	if m.reviewRegion != reviewRegionForm {
		t.Fatalf("F6 ended in region %v, want form", m.reviewRegion)
	}
	if !tt.Focused("主词条数值") {
		t.Fatalf("F6 did not focus the rendered main-stat control: %q", tt.Texts())
	}
}

// TestReviewTypingIntoFoldedFieldKeepsFocus covers the M-A regression: a
// field in the folded group must keep the caret while typing an invalid
// value, and the new blocking problem must become visible without moving
// that field.
func TestReviewTypingIntoFoldedFieldKeepsFocus(t *testing.T) {
	it := withStructured(t, reviewRowItem(t, 1, 12), func(s *zzz.StructuredDisc) {
		s.Data.Slot = nil
	})
	m, _, tt := openResponsiveResults(t, recovery(it), 1180, 760)
	tt.Frame()
	if m.edit == nil || m.edit.firstFields[0] != FormFieldSlot || !tt.HasText("其余 7 个字段") {
		t.Fatalf("the edit is not in the expected folded state: %s", probeReviewState(m))
	}
	if err := tt.Click("其余 7 个字段"); err != nil {
		t.Fatalf("open the folded group: %v", err)
	}
	tt.Frame()
	m.requestReviewFieldFocus(FormFieldLevel)
	tt.Frame()
	tt.Frame()
	if !tt.Focused("等级") {
		t.Fatalf("the level field did not take the keyboard: %q", tt.Texts())
	}

	tt.Key(ui.Cmd, ui.KeyA)
	tt.Key(0, ui.KeyBackspace)
	tt.Type("129")
	tt.Frame()
	tt.Frame()
	if m.edit == nil || m.edit.draft.Level != "129" {
		t.Fatalf("typing 129 stored %q", probeReviewState(m))
	}
	if !tt.Focused("等级") {
		t.Fatalf("typing a blocking value moved the field and lost the keyboard: %q", tt.Texts())
	}
	if !tt.HasText("等级 129 越界") {
		t.Fatalf("the new blocking problem is hidden: %q", tt.Texts())
	}
	tt.Key(0, ui.KeyBackspace)
	tt.Frame()
	tt.Frame()
	if m.edit == nil || m.edit.draft.Level != "12" {
		t.Fatalf("Backspace after the blocking value stored %q", probeReviewState(m))
	}
	if !tt.Focused("等级") {
		t.Fatalf("the field lost the keyboard after Backspace: %q", tt.Texts())
	}
}

// TestReviewFocusSkipsTextOnlyRarityToNextProblem covers the L-A follow-up:
// with rarity (text only) and slot (a control) both unsolved, arriving at
// the item focuses the slot, not a field inside the folded group.
func TestReviewFocusSkipsTextOnlyRarityToNextProblem(t *testing.T) {
	it := withStructured(t, reviewRowItem(t, 2, 12), func(s *zzz.StructuredDisc) {
		s.Data.Rarity = nil
		s.Data.Slot = nil
	})
	m, q, tt, _ := openReviewableResults(t, recovery(
		settledReviewItem(t, 1, record.QualityAutoAccepted),
		it,
	), 1180, 760)
	tt.Frame()
	m.setResultFilter(resultFilterAll)
	tt.Frame()
	m.requestNavigation(navigationRequest{kind: navSelect, seq: 1})
	settle(t, m, q, tt)
	tt.Frame()
	x, y := browseButtonCenter(t, tt, "下一件 ›")
	tt.ClickAt(x, y)
	settle(t, m, q, tt)
	tt.Frame()
	if m.focusedReviewField != FormFieldSlot {
		t.Fatalf("the arrival focused %q, want the next focusable problem field (slot)",
			m.focusedReviewField)
	}
	if !tt.Focused("槽位：" + m.edit.draft.Slot) {
		t.Fatalf("the slot control did not take the keyboard: %q", tt.Texts())
	}
	if m.formExtrasOpen {
		t.Fatal("the arrival expanded the folded group unnecessarily")
	}
}

// TestReviewRarityProblemFocusLandsOnControl covers L-A: rarity is text for
// an item with structured data, so requesting the first problem field must
// not leave the keyboard on 保存修改 where Space would write.
func TestReviewRarityProblemFocusLandsOnControl(t *testing.T) {
	it := withStructured(t, reviewRowItem(t, 2, 12), func(s *zzz.StructuredDisc) {
		s.Data.Rarity = nil
	})
	m, q, tt, svc := openReviewableResults(t, recovery(
		settledReviewItem(t, 1, record.QualityAutoAccepted),
		it,
	), 1180, 760)
	tt.Frame()
	m.setResultFilter(resultFilterAll)
	tt.Frame()
	m.requestNavigation(navigationRequest{kind: navSelect, seq: 1})
	settle(t, m, q, tt)
	tt.Frame()
	x, y := browseButtonCenter(t, tt, "下一件 ›")
	tt.ClickAt(x, y)
	settle(t, m, q, tt)
	tt.Frame()
	if m.selectedSeq() != 2 || m.edit == nil {
		t.Fatalf("下一件 did not reach the rarity problem: %s", probeReviewState(m))
	}
	if m.firstReviewField() != FormFieldRarity {
		t.Fatalf("the first problem field = %q, want rarity", m.firstReviewField())
	}
	if tt.Focused("保存修改") || tt.Focused("确认并下一项") {
		t.Fatalf("the rarity problem left the keyboard on a write button: %q", tt.Texts())
	}
	if m.focusedReviewField != FormFieldMainStat || !tt.Focused("主词条数值") {
		t.Fatalf("the rarity problem did not focus a control: field %q, %q",
			m.focusedReviewField, tt.Texts())
	}
	tt.Key(0, ui.KeySpace)
	settle(t, m, q, tt)
	if got := svc.saveCount(); got != 0 {
		t.Fatalf("Space after the rarity problem wrote %d revisions", got)
	}
}

// TestReviewBrowseGuardIgnoresTab covers L-C: only Space / Enter on the
// guarded write button ends the browse shield, not Tab (or any other key).
func TestReviewBrowseGuardIgnoresTab(t *testing.T) {
	m, q, tt, _ := openReviewableResults(t, recovery(
		settledReviewItem(t, 1, record.QualityAutoAccepted),
		reviewRowItem(t, 2, 12),
	), 1180, 760)
	tt.Frame()
	m.setResultFilter(resultFilterAll)
	tt.Frame()
	m.requestNavigation(navigationRequest{kind: navSelect, seq: 1})
	settle(t, m, q, tt)
	tt.Frame()
	x, y := browseButtonCenter(t, tt, "下一件 ›")
	tt.ClickAt(x, y)
	settle(t, m, q, tt)
	tt.Frame()
	if !m.reviewBrowseGuarded() {
		t.Fatalf("the browse guard did not start: %s", probeReviewState(m))
	}
	for i := 0; i < 40 && !tt.Focused("确认并下一项"); i++ {
		tt.Key(0, ui.KeyTab)
		tt.Frame()
	}
	if !tt.Focused("确认并下一项") {
		t.Fatalf("the guarded write button did not take the keyboard: %q", tt.Texts())
	}
	tt.Key(0, ui.KeyTab)
	tt.Frame()
	if !m.reviewBrowseGuarded() {
		t.Fatal("Tab on the guarded write button released the browse shield")
	}
}

func TestReviewModifySaveAndContinueExplainsQueue(t *testing.T) {
	m, q, tt, svc := openReviewableResults(t, recovery(
		settledReviewItem(t, 1, record.QualityAutoAccepted),
		settledReviewItem(t, 2, record.QualityHumanConfirmed),
	), 1180, 760)
	tt.Frame()
	m.setResultFilter(resultFilterAll)
	tt.Frame()
	m.requestNavigation(navigationRequest{kind: navSelect, seq: 1})
	tt.Frame()
	if err := tt.Click("修改"); err != nil {
		t.Fatalf("click 修改: %v", err)
	}
	tt.Frame()
	m.edit.draft.Level = "13"
	m.edit.refresh()
	tt.Frame()
	if err := tt.Click("#0002"); err != nil {
		t.Fatalf("click the second row: %v", err)
	}
	tt.Frame()
	if m.navPrompt == nil {
		t.Fatalf("the dirty modify did not prompt: %s", probeReviewState(m))
	}
	if !tt.HasText("保存后这件退回待校对，确认前不会导出。") {
		t.Fatalf("the modify prompt does not name the consequence: %q", tt.Texts())
	}
	if err := tt.Click("保存并继续"); err != nil {
		t.Fatalf("click 保存并继续: %v", err)
	}
	settle(t, m, q, tt)
	tt.Frame()

	if got := svc.saveCount(); got != 1 || svc.saves[0].edit.Confirmed {
		t.Fatalf("保存并继续 = %+v, want one unconfirmed save", svc.saves)
	}
	row := &m.rec.Rows[0]
	if row.Quality != record.QualityNeedsReview || row.ItemClass != app.ItemClassPendingReview {
		t.Fatalf("the continued item = quality %q class %q", row.Quality, row.ItemClass)
	}
	if m.selectedSeq() != 2 {
		t.Fatalf("保存并继续 did not continue to the second item: selected %d", m.selectedSeq())
	}
	want := "已保存修改（序号 1）；这件退回待校对，确认前不会导出。"
	if m.reviewNotice != want || !tt.HasText(want) {
		t.Fatalf("the destination notice = %q, texts %q", m.reviewNotice, tt.Texts())
	}
	counts := m.recordClassCounts()
	if counts.Pending() != 1 || counts.Passed != 1 {
		t.Fatalf("class counts after 保存并继续 = %+v", counts)
	}
	if m.exportData == nil || m.exportData.Summary.Counts != counts {
		t.Fatalf("export counts do not follow recordClassCounts: %+v vs %+v",
			m.exportData, counts)
	}
}

// TestReviewModifyNoticeOnOtherDestinations covers L10: after saving a
// modify draft and leaving the item browser, the summary, the records list
// and settings still show the warning that the item went back to review.
func TestReviewModifyNoticeOnOtherDestinations(t *testing.T) {
	m, q, tt, _ := openReviewableResults(t, recovery(
		settledReviewItem(t, 1, record.QualityAutoAccepted),
		settledReviewItem(t, 2, record.QualityHumanConfirmed),
	), 1180, 760)
	tt.Frame()
	m.setResultFilter(resultFilterAll)
	tt.Frame()
	if err := tt.Click("修改"); err != nil {
		t.Fatalf("click 修改: %v", err)
	}
	tt.Frame()
	m.edit.draft.Level = "13"
	m.edit.refresh()
	tt.Frame()
	if err := tt.Click("#0002"); err != nil {
		t.Fatalf("click the second row: %v", err)
	}
	tt.Frame()
	if err := tt.Click("保存并继续"); err != nil {
		t.Fatalf("click 保存并继续: %v", err)
	}
	settle(t, m, q, tt)
	tt.Frame()
	want := "已保存修改（序号 1）；这件退回待校对，确认前不会导出。"
	if !tt.HasText(want) {
		t.Fatalf("the item tab lost the notice: %q", tt.Texts())
	}

	m.requestNavigation(navigationRequest{kind: navRecordTab, recordTab: recordTabSummary})
	tt.Frame()
	if !tt.HasText(want) {
		t.Fatalf("the summary tab lost the notice: %q", tt.Texts())
	}
	m.navigatePage(pageHistory)
	tt.Frame()
	if !tt.HasText(want) {
		t.Fatalf("the records page lost the notice: %q", tt.Texts())
	}
	m.navigatePage(pageSettings)
	tt.Frame()
	if !tt.HasText(want) {
		t.Fatalf("settings lost the notice: %q", tt.Texts())
	}
	m.navigatePage(pageResults)
	tt.Frame()
	m.requestNavigation(navigationRequest{kind: navRecordTab, recordTab: recordTabItems})
	tt.Frame()
	if !tt.HasText(want) {
		t.Fatalf("returning to the item tab lost the notice: %q", tt.Texts())
	}
}

// TestReviewActionReasonKeepsButtonsAnchoredAtLargeText covers the large
// text-size regression: a wrapping reason line must not move the action
// buttons when the browse guard clears.
func TestReviewActionReasonKeepsButtonsAnchoredAtLargeText(t *testing.T) {
	for _, scale := range []float32{1.75, 2} {
		m, q, tt, _ := openReviewableResults(t, recovery(
			settledReviewItem(t, 1, record.QualityAutoAccepted),
			reviewRowItem(t, 2, 12),
			settledReviewItem(t, 3, record.QualityAutoAccepted),
		), 1180, 760)
		tt.SetPreferences(ui.Preferences{TextScale: scale})
		settle(t, m, q, tt)
		tt.Frame()
		m.setResultFilter(resultFilterAll)
		tt.Frame()
		m.requestNavigation(navigationRequest{kind: navSelect, seq: 3})
		settle(t, m, q, tt)
		tt.Frame()
		x, y := browseButtonCenter(t, tt, "‹ 上一件")
		tt.ClickAt(x, y)
		settle(t, m, q, tt)
		tt.Frame()
		guarded, ok := tt.Find("确认并下一项")
		if !ok {
			t.Fatalf("scale %v: the guarded confirm button is missing: %q", scale, tt.Texts())
		}
		if !tt.HasText(reviewBrowseGuardReason) {
			t.Fatalf("scale %v: the reason line is missing: %q", scale, tt.Texts())
		}
		tt.Move(x+300, y-300)
		tt.Frame()
		released, ok := tt.Find("确认并下一项")
		if !ok {
			t.Fatalf("scale %v: the released confirm button is missing: %q", scale, tt.Texts())
		}
		if math.Abs(float64(guarded.Y-released.Y)) >= 0.5 {
			t.Fatalf("scale %v: the buttons moved %v -> %v when the reason cleared",
				scale, guarded.Y, released.Y)
		}
	}
}

// TestReviewCleanupBlocksWriteShortcuts covers PR-14b: a running screenshot
// cleanup disables the write actions, and their shortcuts must not bypass
// that state.
func TestReviewCleanupBlocksWriteShortcuts(t *testing.T) {
	m, q, tt, svc := openReviewableResults(t, recovery(reviewRowItem(t, 1, 12)), 1180, 760)
	tt.Frame()
	m.edit.draft.Level = "13"
	m.edit.refresh()
	tt.Frame()
	m.cleanup = &cleanupPrompt{running: true}
	tt.Frame()
	if strings.Contains(m.resultShortcutText(), shortcutConfirmNext) {
		t.Fatalf("cleanup still advertises the write shortcut: %q", m.resultShortcutText())
	}
	tt.Key(ui.Cmd, ui.KeyEnter)
	settle(t, m, q, tt)
	tt.Key(ui.Cmd, ui.KeyS)
	settle(t, m, q, tt)
	if got := svc.saveCount(); got != 0 {
		t.Fatalf("cleanup let a shortcut write %d revisions", got)
	}
}

// TestReviewModifyExitExplainsQueue covers the exit path of M1: the same
// dialog names the consequence before the application leaves.
func TestReviewModifyExitExplainsQueue(t *testing.T) {
	m, _, tt, _ := openReviewableResults(t, recovery(
		settledReviewItem(t, 1, record.QualityAutoAccepted),
	), 1180, 760)
	tt.Frame()
	m.setResultFilter(resultFilterAll)
	tt.Frame()
	if err := tt.Click("修改"); err != nil {
		t.Fatalf("click 修改: %v", err)
	}
	tt.Frame()
	m.edit.draft.Level = "13"
	m.edit.refresh()
	tt.Frame()
	m.requestNavigation(navigationRequest{kind: navExit})
	tt.Frame()
	if m.navPrompt == nil {
		t.Fatalf("the dirty modify exit did not prompt: %s", probeReviewState(m))
	}
	if !tt.HasText("保存后这件退回待校对，确认前不会导出。") {
		t.Fatalf("the exit prompt does not name the consequence: %q", tt.Texts())
	}
	if err := tt.Click("留在这里"); err != nil {
		t.Fatalf("click 留在这里: %v", err)
	}
	tt.Frame()
	if m.navPrompt != nil || m.edit == nil || m.edit.draft.Level != "13" {
		t.Fatalf("留在这里 lost the exit draft: %s", probeReviewState(m))
	}
}

// TestReviewModifyRetryConfirms covers the second half of M1: after a
// failed "save and continue", the error band retries 确认修改 instead of
// writing an unconfirmed revision.
func TestReviewModifyRetryConfirms(t *testing.T) {
	m, q, tt, svc := openReviewableResults(t, recovery(
		settledReviewItem(t, 1, record.QualityAutoAccepted),
		settledReviewItem(t, 2, record.QualityHumanConfirmed),
	), 1180, 760)
	tt.Frame()
	m.setResultFilter(resultFilterAll)
	tt.Frame()
	m.requestNavigation(navigationRequest{kind: navSelect, seq: 1})
	tt.Frame()
	if err := tt.Click("修改"); err != nil {
		t.Fatalf("click 修改: %v", err)
	}
	tt.Frame()
	good := svc.saveFn
	svc.saveFn = func(string, int64, int64, app.ItemRevisionEdit) (app.ItemRevisionResult, error) {
		return app.ItemRevisionResult{}, errTestSave
	}
	m.edit.draft.Level = "13"
	m.edit.refresh()
	tt.Frame()
	if err := tt.Click("#0002"); err != nil {
		t.Fatalf("click the second row: %v", err)
	}
	tt.Frame()
	if err := tt.Click("保存并继续"); err != nil {
		t.Fatalf("click 保存并继续: %v", err)
	}
	drainAll(t, m, q)
	tt.Frame()
	if err := tt.Click("留在这里"); err != nil {
		t.Fatalf("click 留在这里: %v", err)
	}
	tt.Frame()
	if m.lastAction != reviewSave {
		t.Fatalf("the failed prompt save left lastAction %v", m.lastAction)
	}
	if !tt.HasText("重试确认修改") {
		t.Fatalf("the modify failure band does not retry 确认修改: %q", tt.Texts())
	}

	svc.saveFn = good
	if err := tt.Click("重试确认修改"); err != nil {
		t.Fatalf("click 重试确认修改: %v", err)
	}
	settle(t, m, q, tt)
	tt.Frame()
	if svc.saveCount() != 2 {
		t.Fatalf("save calls = %d, want the failed save and the confirmed retry", svc.saveCount())
	}
	if svc.saves[0].edit.Confirmed || !svc.saves[1].edit.Confirmed {
		t.Fatalf("save confirmations = %v/%v, want false/true",
			svc.saves[0].edit.Confirmed, svc.saves[1].edit.Confirmed)
	}
	if m.edit != nil {
		t.Fatalf("the confirmed retry kept the editor: %s", probeReviewState(m))
	}
	if row := &m.rec.Rows[0]; row.Quality != record.QualityHumanConfirmed ||
		row.ItemClass != app.ItemClassPassed {
		t.Fatalf("the confirmed retry row = quality %q class %q", row.Quality, row.ItemClass)
	}
	if !tt.HasText("已确认修改（序号 1）。") {
		t.Fatalf("the confirmed retry has no notice: %q", tt.Texts())
	}
}

// TestReviewPendingShortcutCrossesFilters covers M2: Ctrl+J and
// Ctrl+Shift+J switch from 警告 (or a facet-narrowed view) to 待处理 when
// the record has a pending item, and the status bar says so.
func TestReviewPendingShortcutCrossesFilters(t *testing.T) {
	rec := recovery(
		reviewRowItem(t, 1, 12),
		settledReviewItem(t, 2, record.QualityAutoAccepted),
		equippedWarningReviewItem(t, 3),
		settledReviewItem(t, 4, record.QualityHumanConfirmed),
	)
	m, q, tt, _ := openReviewableResults(t, rec, 1180, 760)
	tt.Frame()
	m.setResultFilter(resultFilterWarning)
	tt.Frame()
	if status := m.resultShortcutText(); !strings.Contains(status, shortcutNextPending) {
		t.Fatalf("the warning status bar omits the pending walk: %q", status)
	}
	tt.Key(ui.Cmd, ui.KeyJ)
	settle(t, m, q, tt)
	tt.Frame()
	if m.filter != resultFilterPending || m.selectedSeq() != 1 {
		t.Fatalf("Ctrl+J from 警告 = filter %v selected %d: %s",
			m.filter, m.selectedSeq(), probeReviewState(m))
	}
	if m.edit == nil || m.edit.seq != 1 || m.focusedReviewField != FormFieldMainStat {
		t.Fatalf("Ctrl+J did not focus the pending editor: %s field %q",
			probeReviewState(m), m.focusedReviewField)
	}

	m.setResultFilter(resultFilterWarning)
	tt.Frame()
	tt.Key(ui.Cmd|ui.Shift, ui.KeyJ)
	settle(t, m, q, tt)
	tt.Frame()
	if m.filter != resultFilterPending || m.selectedSeq() != 1 {
		t.Fatalf("Ctrl+Shift+J from 警告 = filter %v selected %d",
			m.filter, m.selectedSeq())
	}

	// A facet that hides the pending row must not hide it from Ctrl+J.
	warning := withStructured(t, equippedWarningReviewItem(t, 3), func(s *zzz.StructuredDisc) {
		setID := 31200
		s.Data.SetID = &setID
	})
	m2, q2, tt2, _ := openReviewableResults(t, recovery(
		reviewRowItem(t, 1, 12),
		settledReviewItem(t, 2, record.QualityAutoAccepted),
		warning,
	), 1180, 760)
	tt2.Frame()
	m2.setResultFilter(resultFilterAll)
	m2.requestResultFilter(resultFilterAll, "31200", "")
	tt2.Frame()
	if m2.selectedSeq() != 3 || m2.setFilter != "31200" {
		t.Fatalf("the facet setup selected %d filter %q", m2.selectedSeq(), m2.setFilter)
	}
	tt2.Key(ui.Cmd, ui.KeyJ)
	settle(t, m2, q2, tt2)
	tt2.Frame()
	if m2.filter != resultFilterPending || m2.selectedSeq() != 1 || m2.setFilter != "" {
		t.Fatalf("Ctrl+J under a facet = filter %v selected %d set %q",
			m2.filter, m2.selectedSeq(), m2.setFilter)
	}
}

// TestReviewPendingShortcutVisibleFocus covers L3: Ctrl+J from a
// read-only row to a pending row in the same segment moves the keyboard
// into the pending editor.
func TestReviewPendingShortcutVisibleFocus(t *testing.T) {
	m, q, tt, _ := openReviewableResults(t, recovery(
		settledReviewItem(t, 1, record.QualityAutoAccepted),
		reviewRowItem(t, 2, 12),
	), 1180, 760)
	tt.Frame()
	m.setResultFilter(resultFilterAll)
	tt.Frame()
	m.requestNavigation(navigationRequest{kind: navSelect, seq: 1})
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyJ)
	settle(t, m, q, tt)
	tt.Frame()
	if m.selectedSeq() != 2 || m.edit == nil || m.edit.seq != 2 {
		t.Fatalf("Ctrl+J did not reach the pending editor: %s", probeReviewState(m))
	}
	if m.focusedReviewField != FormFieldMainStat {
		t.Fatalf("Ctrl+J left the keyboard at %q", m.focusedReviewField)
	}
}

// TestReviewPendingShortcutPromptsDirtyModify covers M2's guard: the jump
// to 待处理 must still pass through the unsaved dialog.
func TestReviewPendingShortcutPromptsDirtyModify(t *testing.T) {
	m, q, tt, _ := openReviewableResults(t, recovery(
		reviewRowItem(t, 1, 12),
		equippedWarningReviewItem(t, 2),
	), 1180, 760)
	tt.Frame()
	m.setResultFilter(resultFilterWarning)
	tt.Frame()
	if err := tt.Click("修改"); err != nil {
		t.Fatalf("click 修改: %v", err)
	}
	tt.Frame()
	m.edit.draft.Level = "13"
	m.edit.refresh()
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyJ)
	settle(t, m, q, tt)
	tt.Frame()
	if m.navPrompt == nil || m.filter != resultFilterWarning {
		t.Fatalf("Ctrl+J bypassed the dirty modify: %s", probeReviewState(m))
	}
	if err := tt.Click("留在这里"); err != nil {
		t.Fatalf("click 留在这里: %v", err)
	}
	tt.Frame()
	if m.edit == nil || m.edit.draft.Level != "13" || m.filter != resultFilterWarning {
		t.Fatalf("留在这里 lost the dirty modify: %s", probeReviewState(m))
	}
}

// TestReviewPendingShortcutStatusWithoutPending checks the other half of
// the status bar rule: no pending item in the record, no pending keys.
func TestReviewPendingShortcutStatusWithoutPending(t *testing.T) {
	m, _, tt, _ := openReviewableResults(t, recovery(
		settledReviewItem(t, 1, record.QualityAutoAccepted),
		equippedWarningReviewItem(t, 2),
	), 1180, 760)
	tt.Frame()
	m.setResultFilter(resultFilterWarning)
	tt.Frame()
	if status := m.resultShortcutText(); strings.Contains(status, shortcutNextPending) ||
		strings.Contains(status, shortcutPreviousPending) {
		t.Fatalf("a record without pending items advertises the pending walk: %q", status)
	}
}

// TestReviewModifyFocusesFirstRenderedField covers M3: the editor's first
// rendered field is 主词条, not the read-only form's leading 套装.
func TestReviewModifyFocusesFirstRenderedField(t *testing.T) {
	m, _, tt, _ := openReviewableResults(t, recovery(
		settledReviewItem(t, 1, record.QualityAutoAccepted),
	), 1180, 760)
	tt.Frame()
	m.setResultFilter(resultFilterAll)
	tt.Frame()
	if err := tt.Click("修改"); err != nil {
		t.Fatalf("click 修改: %v", err)
	}
	tt.Frame()
	tt.Frame()
	if m.focusedReviewField != FormFieldMainStat || m.reviewField != FormFieldMainStat {
		t.Fatalf("修改 focused %q (highlight %q), want 主词条",
			m.focusedReviewField, m.reviewField)
	}
	if !tt.Focused("主词条数值") {
		t.Fatalf("the main-stat value does not hold the keyboard: %q", tt.Texts())
	}
	if tt.Focused("套装") {
		t.Fatal("修改 still focused the set field")
	}
}

// TestReviewModifyKeepsWarningRow covers M4: a fixed warning row stays in
// the 警告 segment with its notice until the filter changes.
func TestReviewModifyKeepsWarningRow(t *testing.T) {
	m, q, tt, _ := openReviewableResults(t, recovery(
		equippedWarningReviewItem(t, 1),
		equippedWarningReviewItem(t, 2),
	), 1180, 760)
	tt.Frame()
	m.setResultFilter(resultFilterWarning)
	tt.Frame()
	if m.selectedSeq() != 1 {
		t.Fatalf("the warning visit starts at %d", m.selectedSeq())
	}
	if err := tt.Click("修改"); err != nil {
		t.Fatalf("click 修改: %v", err)
	}
	tt.Frame()
	m.edit.draft.EquippedState = describeEquipped(string(zzz.EquippedStateUnequipped))
	m.edit.refresh()
	tt.Frame()
	if err := tt.Click("确认修改"); err != nil {
		t.Fatalf("click 确认修改: %v", err)
	}
	settle(t, m, q, tt)
	tt.Frame()

	if m.filter != resultFilterWarning || m.selectedSeq() != 1 {
		t.Fatalf("the fixed warning left its segment: %s", probeReviewState(m))
	}
	if row := &m.rec.Rows[0]; row.ItemClass != app.ItemClassPassed {
		t.Fatalf("the fixed warning row class = %q", row.ItemClass)
	}
	if visible := m.visibleResultRows(); len(visible) != 2 || visible[0] != 0 {
		t.Fatalf("the fixed warning row is not kept in the segment: visible %v", visible)
	}
	if m.reviewNotice != "已确认修改（序号 1）。" || !tt.HasText("已确认修改（序号 1）。") {
		t.Fatalf("the keep lost the notice: %q", m.reviewNotice)
	}
	if !tt.Focused("修改") {
		t.Fatalf("the kept row did not return the keyboard to 修改: %q", tt.Texts())
	}

	if err := tt.Click("全部"); err != nil {
		t.Fatalf("click 全部: %v", err)
	}
	tt.Frame()
	if err := tt.Click("警告"); err != nil {
		t.Fatalf("click 警告: %v", err)
	}
	tt.Frame()
	visible := m.visibleResultRows()
	if len(visible) != 1 || m.rec.Rows[visible[0]].Seq != 2 {
		t.Fatalf("the visit marker survived the filter change: visible %v", visible)
	}
}

// TestReviewModifyKeepsLastWarningRow covers M4's empty-list case: the
// last fixed warning stays visible instead of leaving an empty segment.
func TestReviewModifyKeepsLastWarningRow(t *testing.T) {
	m, q, tt, _ := openReviewableResults(t, recovery(
		equippedWarningReviewItem(t, 1),
	), 1180, 760)
	tt.Frame()
	m.setResultFilter(resultFilterWarning)
	tt.Frame()
	if err := tt.Click("修改"); err != nil {
		t.Fatalf("click 修改: %v", err)
	}
	tt.Frame()
	m.edit.draft.EquippedState = describeEquipped(string(zzz.EquippedStateUnequipped))
	m.edit.refresh()
	tt.Frame()
	if err := tt.Click("确认修改"); err != nil {
		t.Fatalf("click 确认修改: %v", err)
	}
	settle(t, m, q, tt)
	tt.Frame()
	if visible := m.visibleResultRows(); len(visible) != 1 {
		t.Fatalf("the last fixed warning emptied the list: visible %v", visible)
	}
	if !tt.HasText("已确认修改（序号 1）。") {
		t.Fatalf("the last fixed warning lost its notice: %q", tt.Texts())
	}
}

// TestReviewModifyNoOpDoesNotWrite covers L1: 确认修改 without a change
// creates no revision.
func TestReviewModifyNoOpDoesNotWrite(t *testing.T) {
	m, q, tt, svc := openReviewableResults(t, recovery(
		settledReviewItem(t, 1, record.QualityAutoAccepted),
	), 1180, 760)
	tt.Frame()
	m.setResultFilter(resultFilterAll)
	tt.Frame()
	if err := tt.Click("修改"); err != nil {
		t.Fatalf("click 修改: %v", err)
	}
	tt.Frame()
	if status := m.resultShortcutText(); strings.Contains(status, shortcutConfirmNext) {
		t.Fatalf("the unchanged modify status bar advertises 确认修改: %q", status)
	}
	if err := tt.Click("确认修改"); err != nil {
		// A disabled button may reject or ignore the click; both are fine.
		t.Logf("disabled 确认修改 click: %v", err)
	}
	settle(t, m, q, tt)
	tt.Key(ui.Cmd, ui.KeyEnter)
	settle(t, m, q, tt)
	tt.Frame()
	if got := svc.saveCount(); got != 0 {
		t.Fatalf("an unchanged 确认修改 wrote %d revisions", got)
	}
	if m.rec.Rows[0].Quality != record.QualityAutoAccepted {
		t.Fatalf("an unchanged confirm changed the quality to %q", m.rec.Rows[0].Quality)
	}
	if m.reviewNotice != "没有修改需要确认。" {
		t.Fatalf("the unchanged confirm notice = %q", m.reviewNotice)
	}
	if m.edit == nil || !m.edit.modify {
		t.Fatalf("the unchanged confirm closed the modify editor: %s", probeReviewState(m))
	}
}

// TestReviewModifyNormalizedNoOpDoesNotWrite covers L1's residual: text
// that parses to the same values (leading zeros, surrounding spaces,
// trailing spaces in a decimal) is not a modification and creates no
// revision.
func TestReviewModifyNormalizedNoOpDoesNotWrite(t *testing.T) {
	for _, tc := range []struct {
		name  string
		apply func(*editState)
	}{
		{"level-leading-zero", func(e *editState) { e.draft.Level = "015" }},
		{"level-space", func(e *editState) { e.draft.Level = " 15" }},
		{"main-value-space", func(e *editState) { e.draft.MainValue = "19.2 " }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, q, tt, svc := openReviewableResults(t, recovery(
				settledReviewItem(t, 1, record.QualityAutoAccepted),
			), 1180, 760)
			tt.Frame()
			m.setResultFilter(resultFilterAll)
			tt.Frame()
			if err := tt.Click("修改"); err != nil {
				t.Fatalf("click 修改: %v", err)
			}
			tt.Frame()
			tc.apply(m.edit)
			m.edit.refresh()
			tt.Frame()
			if m.editingDirty() {
				t.Fatalf("%s counts as a modification: %s", tc.name, probeReviewState(m))
			}
			tt.Key(ui.Cmd, ui.KeyEnter)
			settle(t, m, q, tt)
			tt.Frame()
			if got := svc.saveCount(); got != 0 {
				t.Fatalf("%s wrote %d revisions", tc.name, got)
			}
			if m.rec.Rows[0].Quality != record.QualityAutoAccepted {
				t.Fatalf("%s changed the quality to %q", tc.name, m.rec.Rows[0].Quality)
			}
		})
	}
}

// TestReviewBrowseGuardAllowsShortcut covers L7: the guard is about a
// repeated click at the browse point, not the confirm shortcut. Ctrl+Enter
// keeps working while the pointer stays there, so the bar must not claim
// that confirmation needs a wait.
func TestReviewBrowseGuardAllowsShortcut(t *testing.T) {
	m, q, tt, svc := openReviewableResults(t, recovery(
		settledReviewItem(t, 1, record.QualityAutoAccepted),
		reviewRowItem(t, 2, 12),
		settledReviewItem(t, 3, record.QualityAutoAccepted),
	), 1180, 760)
	tt.Frame()
	m.setResultFilter(resultFilterAll)
	tt.Frame()
	m.requestNavigation(navigationRequest{kind: navSelect, seq: 3})
	settle(t, m, q, tt)
	x, y := browseButtonCenter(t, tt, "‹ 上一件")
	tt.ClickAt(x, y)
	settle(t, m, q, tt)
	tt.Frame()
	if !m.reviewBrowseGuarded() {
		t.Fatalf("the browse guard did not start: %s", probeReviewState(m))
	}
	if tt.HasText("请稍后再确认") {
		t.Fatalf("the guard copy promises a wait: %q", tt.Texts())
	}
	if status := m.resultShortcutText(); !strings.Contains(status, shortcutConfirmNext) {
		t.Fatalf("the guarded bar hides the confirm shortcut: %q", status)
	}
	tt.Key(ui.Cmd, ui.KeyEnter)
	settle(t, m, q, tt)
	if got := svc.saveCount(); got != 1 || !svc.saves[0].edit.Confirmed {
		t.Fatalf("Ctrl+Enter under the guard = %+v, want one confirmed save", svc.saves)
	}
}

// TestReviewBrowseBarsAlign pins the layout the guard protects: the browse
// button and the editor's first write button occupy the same row, so a
// click repeated at the browse point really lands on the write button and
// must be ignored (H1, L9).
func TestReviewBrowseBarsAlign(t *testing.T) {
	m, q, tt, _ := openReviewableResults(t, recovery(
		settledReviewItem(t, 1, record.QualityAutoAccepted),
		reviewRowItem(t, 2, 12),
		settledReviewItem(t, 3, record.QualityAutoAccepted),
	), 1180, 760)
	tt.Frame()
	m.setResultFilter(resultFilterAll)
	tt.Frame()
	m.requestNavigation(navigationRequest{kind: navSelect, seq: 3})
	settle(t, m, q, tt)
	x, y := browseButtonCenter(t, tt, "‹ 上一件")
	tt.ClickAt(x, y)
	settle(t, m, q, tt)
	tt.Frame()
	confirm, ok := tt.Find("确认并下一项")
	if !ok {
		t.Fatalf("确认并下一项 is missing: %q", tt.Texts())
	}
	if x < confirm.X || x > confirm.X+confirm.W ||
		y < confirm.Y || y > confirm.Y+confirm.H {
		t.Fatalf("the browse point (%.1f,%.1f) misses the write button %+v",
			x, y, confirm)
	}
}

// TestReviewModifyCleanReason checks L8 and L9: a clean modify disables
// 确认修改 with a visible reason, and the reason line keeps its place so
// clearing the browse guard does not move the button under the pointer.
func TestReviewModifyCleanReason(t *testing.T) {
	m, q, tt, _ := openReviewableResults(t, recovery(
		settledReviewItem(t, 1, record.QualityAutoAccepted),
		reviewRowItem(t, 2, 12),
		settledReviewItem(t, 3, record.QualityAutoAccepted),
	), 1180, 760)
	tt.Frame()
	m.setResultFilter(resultFilterAll)
	tt.Frame()
	m.requestNavigation(navigationRequest{kind: navSelect, seq: 1})
	settle(t, m, q, tt)
	tt.Frame()
	if err := tt.Click("修改"); err != nil {
		t.Fatalf("click 修改: %v", err)
	}
	tt.Frame()
	if !tt.HasText("没有修改，确认修改不可用。") {
		t.Fatalf("the clean modify has no disabled reason: %q", tt.Texts())
	}
	// The disabled button must not start the no-op action at all.
	if err := tt.Click("确认修改"); err != nil {
		t.Logf("disabled 确认修改 click: %v", err)
	}
	settle(t, m, q, tt)
	tt.Frame()
	if m.reviewNotice != "" {
		t.Fatalf("the clean 确认修改 reacted: %q", m.reviewNotice)
	}

	// Browse from a passed item to a pending one, then release the guard
	// with a pointer move: the action buttons must not jump.
	m.requestNavigation(navigationRequest{kind: navSelect, seq: 3})
	settle(t, m, q, tt)
	tt.Frame()
	x, y := browseButtonCenter(t, tt, "‹ 上一件")
	tt.ClickAt(x, y)
	settle(t, m, q, tt)
	tt.Frame()
	guarded, ok := tt.Find("确认并下一项")
	if !ok {
		t.Fatalf("the pending confirm button is missing: %q", tt.Texts())
	}
	if !tt.HasText(reviewBrowseGuardReason) {
		t.Fatalf("the guarded bar does not explain itself: %q", tt.Texts())
	}
	tt.Move(x+300, y-300)
	tt.Frame()
	released, ok := tt.Find("确认并下一项")
	if !ok {
		t.Fatalf("the released confirm button is missing: %q", tt.Texts())
	}
	// The button must not jump under the pointer: the former bug moved it
	// about 17 DIP. A sub-pixel difference (about 0.04 DIP) was observed
	// between the reason line and its placeholder, but its source was not
	// identified (changing the placeholder to a CJK glyph did not move it).
	// The user accepted that tolerance on 2026-11-07.
	if math.Abs(float64(guarded.Y-released.Y)) >= 0.5 {
		t.Fatalf("the action buttons moved %v -> %v when the guard cleared",
			guarded.Y, released.Y)
	}
}

// TestReviewQueuedRowShowsPlaceholders covers L2: a queued item has no
// settled fields to correct, so it shows the placeholder, not blank values
// or gap markers.
func TestReviewQueuedRowShowsPlaceholders(t *testing.T) {
	m, _, tt := openResponsiveResults(t, recovery(
		item(1, record.StatusQueued, "", "", "排队中"),
	), 1180, 760)
	tt.Frame()
	m.setResultFilter(resultFilterAll)
	tt.Frame()
	if m.edit != nil {
		t.Fatalf("a queued item opened an editor: %s", probeReviewState(m))
	}
	if !tt.HasText("—") {
		t.Fatalf("the queued placeholders are missing: %q", tt.Texts())
	}
	if tt.HasText("需补全") || tt.HasText("需确认") {
		t.Fatalf("the queued item carries gap markers: %q", tt.Texts())
	}
	if _, ok := tt.Find("修改"); ok {
		t.Fatalf("the queued item offers 修改: %q", tt.Texts())
	}
	if !tt.HasText("‹ 上一件") || !tt.HasText("下一件 ›") {
		t.Fatalf("the queued item lost the browse actions: %q", tt.Texts())
	}
}

// TestReviewClearedFieldKeepsFocus covers M5: clearing a required field
// moves it into the leading problem group; the keyboard stays in that
// field and later typing still lands there.
func TestReviewClearedFieldKeepsFocus(t *testing.T) {
	m, _, tt, _ := openReviewableResults(t, recovery(
		settledReviewItem(t, 1, record.QualityAutoAccepted),
	), 1180, 760)
	tt.Frame()
	m.setResultFilter(resultFilterAll)
	tt.Frame()
	if err := tt.Click("修改"); err != nil {
		t.Fatalf("click 修改: %v", err)
	}
	tt.Frame()
	m.requestReviewFieldFocus(FormFieldLevel)
	tt.Frame()
	tt.Frame()
	if !tt.Focused("等级") {
		t.Fatalf("the level field did not take the keyboard: %q", tt.Texts())
	}

	tt.Key(ui.Cmd, ui.KeyA)
	tt.Key(0, ui.KeyBackspace)
	tt.Frame()
	tt.Frame()
	if m.edit == nil || m.edit.draft.Level != "" {
		t.Fatalf("the level field did not clear: %s", probeReviewState(m))
	}
	if !tt.Focused("等级") {
		t.Fatalf("clearing the field moved it and lost the keyboard: %q", tt.Texts())
	}
	tt.Type("9")
	tt.Frame()
	if m.edit == nil || m.edit.draft.Level != "9" {
		t.Fatalf("typing after the move did not reach the field: %s", probeReviewState(m))
	}
	// The field keeps its place (the edit's field order is fixed) and the
	// keyboard stays with it.
	tt.Frame()
	if !tt.Focused("等级") {
		t.Fatalf("the filled field lost the keyboard: %q", tt.Texts())
	}
}

// TestReviewStatusTagIsInline covers L4: the passed status tag is a small
// inline tag, not a full-width bar.
func TestReviewStatusTagIsInline(t *testing.T) {
	m, _, tt, _ := openReviewableResults(t, recovery(
		settledReviewItem(t, 1, record.QualityAutoAccepted),
	), 1180, 760)
	tt.Frame()
	m.setResultFilter(resultFilterAll)
	tt.Frame()
	rect, ok := tt.Find("状态：已通过 · 自动通过")
	if !ok {
		t.Fatalf("the status tag is missing: %q", tt.Texts())
	}
	if rect.W > 200 {
		t.Fatalf("the status tag is %v DIP wide, want an inline tag", rect.W)
	}
}

// TestReviewFilterScrollsSelectionIntoView covers L5: switching segments
// keeps the chosen row inside the list viewport.
func TestReviewFilterScrollsSelectionIntoView(t *testing.T) {
	items := make([]record.ItemRecord, 0, 60)
	for seq := int64(1); seq <= 60; seq++ {
		if seq == 40 {
			items = append(items, equippedWarningReviewItem(t, seq))
			continue
		}
		items = append(items, settledReviewItem(t, seq, record.QualityAutoAccepted))
	}
	m, _, tt, _ := openReviewableResults(t, recovery(items...), 1180, 760)
	tt.Frame()
	m.setResultFilter(resultFilterWarning)
	tt.Frame()
	if m.selectedSeq() != 40 {
		t.Fatalf("the warning visit starts at %d, want #0040", m.selectedSeq())
	}
	m.setResultFilter(resultFilterAll)
	tt.Frame()
	visible := m.visibleResultRows()
	display := resultDisplayIndex(visible, m.selected)
	if display != 39 {
		t.Fatalf("the chosen warning row is display %d, want 39", display)
	}
	first, last := m.list.Visible()
	if display < first || display > last {
		t.Fatalf("the chosen row %d is outside the visible list %d..%d", display, first, last)
	}
}

// TestReviewReadOnlyF6FocusesModify covers L6's F6 gap: F6 into the form of
// a read-only item must land on 修改.
func TestReviewReadOnlyF6FocusesModify(t *testing.T) {
	m, _, tt, _ := openReviewableResults(t, recovery(
		settledReviewItem(t, 1, record.QualityAutoAccepted),
	), 1180, 760)
	tt.Frame()
	m.setResultFilter(resultFilterAll)
	tt.Frame()
	m.selectReviewRegion(reviewRegionForm)
	tt.Frame()
	if !tt.Focused("修改") {
		t.Fatalf("the read-only form does not start on 修改: %q", tt.Texts())
	}
	tt.Key(0, ui.KeyF6)
	tt.Frame()
	tt.Key(0, ui.KeyF6)
	tt.Frame()
	tt.Key(0, ui.KeyF6)
	tt.Frame()
	tt.Frame()
	if m.reviewRegion != reviewRegionForm {
		t.Fatalf("F6 ended in region %v, want form", m.reviewRegion)
	}
	if !tt.Focused("修改") {
		t.Fatalf("F6 into the read-only form did not focus 修改: %q", tt.Texts())
	}
}
