package ui

import (
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
// 上一件 / 下一件 sit at the editor bar's write buttons' position. A second
// click at the same point, or Space / Enter right after the move, must not
// confirm or save the item the user has only just reached. After the short
// guard the deliberate action works.
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

	// The guard only delays the action; a deliberate click after it still
	// confirms the item.
	tt.Advance(reviewBrowseGuardFor + 100*time.Millisecond)
	tt.Frame()
	if err := tt.Click("确认并下一项"); err != nil {
		t.Fatalf("click 确认并下一项 after the guard: %v", err)
	}
	settle(t, m, q, tt)
	if got := svc.saveCount(); got != 1 || !svc.saves[0].edit.Confirmed {
		t.Fatalf("the guarded confirm after expiry = %+v, want one confirmed save", svc.saves)
	}
}

// TestReviewBrowseNextCannotSaveDirtyDraft covers the second half of H1:
// 下一件 lands on the editor's 保存修改. Even with a dirty draft, a click at
// the old point (or the save button itself) is ignored until the guard
// clears; afterward the save works.
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

	// The old 下一件 point now sits on the editor bar. Whether it lands on
	// 确认并下一项 or 保存修改, the guard must ignore it.
	tt.ClickAt(x, y)
	settle(t, m, q, tt)
	if got := svc.saveCount(); got != 0 {
		t.Fatalf("the same-point click saved a dirty draft %d times", got)
	}
	if err := tt.Click("保存修改"); err != nil {
		t.Fatalf("click 保存修改 while guarded: %v", err)
	}
	settle(t, m, q, tt)
	if got := svc.saveCount(); got != 0 {
		t.Fatalf("the guarded save button wrote %d revisions", got)
	}
	if m.edit == nil || m.edit.draft.Level != "13" {
		t.Fatalf("the guard dropped the dirty draft: %s", probeReviewState(m))
	}

	tt.Advance(reviewBrowseGuardFor + 100*time.Millisecond)
	tt.Frame()
	if err := tt.Click("保存修改"); err != nil {
		t.Fatalf("click 保存修改 after the guard: %v", err)
	}
	settle(t, m, q, tt)
	if got := svc.saveCount(); got != 1 || svc.saves[0].edit.Confirmed {
		t.Fatalf("the guarded save after expiry = %+v, want one unconfirmed save", svc.saves)
	}
}

// TestReviewModifySaveAndContinueExplainsQueue covers M1: a modify draft
// saved by the generic "save and continue" leaves the confirmed set, so the
// dialog says it and the destination repeats it.
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
	// Filling the gap moves the field back with the rest; the keyboard
	// follows it there too.
	tt.Frame()
	if !tt.Focused("等级") {
		t.Fatalf("the filled field lost the keyboard on its way back: %q", tt.Texts())
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
