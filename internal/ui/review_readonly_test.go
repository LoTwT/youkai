package ui

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/game/zzz"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/egoist/mygo/ui"
)

// errTestSave is the modify flow's injected save failure.
var errTestSave = errors.New("磁盘已满")

// settledReviewItem is a complete item already settled by the scan or a
// person, so it opens read-only (08 §3.4).
func settledReviewItem(t *testing.T, seq int64, quality record.Quality) record.ItemRecord {
	t.Helper()
	item := reviewRowItem(t, seq, 15)
	item.Quality = quality
	return item
}

// equippedWarningReviewItem is the common exportable warning from 08 §3.3:
// the disc is equipped but the agent was not recognized, so the item is
// auto accepted, exportable and carries the optional equipped warning.
func equippedWarningReviewItem(t *testing.T, seq int64) record.ItemRecord {
	t.Helper()
	item := withStructured(t, reviewRowItem(t, seq, 15), func(s *zzz.StructuredDisc) {
		s.Data.Equipped = &zzz.Equipped{State: zzz.EquippedStateEquipped}
		s.Issues = []zzz.Issue{{
			Field: "equipped", Code: zzz.CodeEquippedStateUnavailable,
			Severity: zzz.SeverityWarning, Message: "装备者未识别",
		}}
		s.Validation = zzz.Validation{Status: zzz.ValidationPassed, Issues: []zzz.Issue{}}
	})
	item.Quality = record.QualityAutoAccepted
	return item
}

// roledSettledItem is a passed item whose evidence carries the observed
// line roles, so the read-only form's field highlight can be checked.
func roledSettledItem(t *testing.T, seq int64) record.ItemRecord {
	t.Helper()
	item := settledReviewItem(t, seq, record.QualityAutoAccepted)
	roles := []string{"name", "level", "main", "sub1", "sub2", "sub3", "sub4"}
	texts := []string{"河豚电音", "等级 15/15", "暴击率 19.2%", "暴击伤害 9.6%", "攻击力 3%", "穿透值 19", "生命值 112"}
	var observedLines, resultLines []map[string]any
	for i, role := range roles {
		observedLines = append(observedLines, map[string]any{"role": role})
		resultLines = append(resultLines, map[string]any{"role": role, "text": texts[i]})
		item.EvidenceIDs = append(item.EvidenceIDs, fmt.Sprintf("%06d-line%d", seq, i))
	}
	item.EvidenceIDs = append(item.EvidenceIDs, fmt.Sprintf("%06d-context0", seq))
	observed := map[string]any{
		"lines":   observedLines,
		"context": []map[string]any{{"role": "detailAvatar"}},
	}
	rawObserved, err := json.Marshal(observed)
	if err != nil {
		t.Fatal(err)
	}
	item.Observed = rawObserved
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(item.Data, &doc); err != nil {
		t.Fatal(err)
	}
	rawLines, err := json.Marshal(resultLines)
	if err != nil {
		t.Fatal(err)
	}
	doc["lines"] = rawLines
	item.Data, err = json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return item
}

// TestReviewReadOnlyPassedForm checks the first two rows of the 08 §3.4
// table: a passed item opens as text with the 修改 action at the top right,
// and its status tag names auto acceptance or human confirmation.
func TestReviewReadOnlyPassedForm(t *testing.T) {
	m, _, tt, _ := openReviewableResults(t, recovery(
		settledReviewItem(t, 1, record.QualityAutoAccepted),
		settledReviewItem(t, 2, record.QualityHumanConfirmed),
	), 1180, 760)
	tt.Frame()
	m.setResultFilter(resultFilterAll)
	tt.Frame()

	if m.selectedSeq() != 1 || m.edit != nil {
		t.Fatalf("a passed item opened an editor: %s", probeReviewState(m))
	}
	for _, want := range []string{
		"已通过 · 自动通过", "修改", "‹ 上一件", "下一件 ›",
	} {
		if !tt.HasText(want) {
			t.Fatalf("the read-only passed form does not show %q: %q", want, tt.Texts())
		}
	}
	for _, unwanted := range []string{
		"需确认", "确认并下一项", "保存修改", "确认修改", "取消修改",
	} {
		if tt.HasText(unwanted) {
			t.Fatalf("the read-only passed form shows %q: %q", unwanted, tt.Texts())
		}
	}

	if err := tt.Click("下一件 ›"); err != nil {
		t.Fatalf("browse to the confirmed item: %v", err)
	}
	tt.Frame()
	if m.selectedSeq() != 2 {
		t.Fatalf("下一件 selected %d, want 2", m.selectedSeq())
	}
	if !tt.HasText("已通过 · 已人工确认") || tt.HasText("已通过 · 自动通过") {
		t.Fatalf("the human-confirmed status tag is wrong: %q", tt.Texts())
	}
}

// TestReviewReadOnlyEntryFocus checks the keyboard rows of 08 §3.4: the
// form starts on 修改, and a warning item also highlights its warning
// field's crop on entry (08 §3.3).
func TestReviewReadOnlyEntryFocus(t *testing.T) {
	m, _, tt, _ := openReviewableResults(t, recovery(settledReviewItem(t, 1, record.QualityAutoAccepted)), 1180, 760)
	tt.Frame()
	m.openSummaryClass(app.ItemClassPassed)
	tt.Frame()
	if m.recordTab != recordTabItems {
		t.Fatal("the passed bucket did not open the item view")
	}
	if !tt.Focused("修改") {
		t.Fatalf("the read-only form did not start on 修改: %q", tt.Texts())
	}

	wm, _, wtt, _ := openReviewableResults(t, recovery(equippedWarningReviewItem(t, 1)), 960, 640)
	wtt.Frame()
	wm.openSummaryClass(app.ItemClassWarning)
	wtt.Frame()
	if !wtt.Focused("修改") {
		t.Fatalf("the warning item did not start on 修改: %q", wtt.Texts())
	}
	if wm.reviewField != FormFieldEquipped {
		t.Fatalf("the warning entry highlighted %q, want the equipped field", wm.reviewField)
	}
}

// TestReviewReadOnlyShortcutsDoNotWrite checks the write guard of the
// read-only state: Ctrl+Enter and Ctrl+S must not reach the service, and
// the status bar must not advertise them.
func TestReviewReadOnlyShortcutsDoNotWrite(t *testing.T) {
	m, q, tt, svc := openReviewableResults(t, recovery(settledReviewItem(t, 1, record.QualityAutoAccepted)), 1180, 760)
	tt.Frame()
	m.setResultFilter(resultFilterAll)
	tt.Frame()

	status := m.resultShortcutText()
	if strings.Contains(status, shortcutConfirmNext) || strings.Contains(status, shortcutSave) {
		t.Fatalf("the read-only status bar names a write shortcut: %q", status)
	}
	if !strings.Contains(status, shortcutZoomIn) {
		t.Fatalf("the read-only status bar lost the zoom keys: %q", status)
	}

	tt.Key(ui.Cmd, ui.KeyEnter)
	settle(t, m, q, tt)
	tt.Key(ui.Cmd, ui.KeyS)
	settle(t, m, q, tt)
	if got := svc.saveCount(); got != 0 {
		t.Fatalf("read-only shortcuts wrote %d revisions", got)
	}
	if m.edit != nil || m.selectedSeq() != 1 {
		t.Fatalf("a read-only shortcut changed the workspace: %s", probeReviewState(m))
	}
}

// TestReviewReadOnlyBrowsing checks the third row of 08 §3.4: only
// 上一件 / 下一件, and they stay inside the segment without changing it.
func TestReviewReadOnlyBrowsing(t *testing.T) {
	m, _, tt, _ := openReviewableResults(t, recovery(
		settledReviewItem(t, 1, record.QualityAutoAccepted),
		settledReviewItem(t, 2, record.QualityHumanConfirmed),
		equippedWarningReviewItem(t, 3),
	), 1180, 760)
	tt.Frame()
	m.setResultFilter(resultFilterAll)
	tt.Frame()

	// 上一件 at the first row is a no-op, not a wrap-around.
	if err := tt.Click("‹ 上一件"); err != nil {
		t.Fatalf("click 上一件: %v", err)
	}
	tt.Frame()
	if m.selectedSeq() != 1 {
		t.Fatalf("上一件 wrapped past the first row: selected %d", m.selectedSeq())
	}
	if err := tt.Click("下一件 ›"); err != nil {
		t.Fatalf("click 下一件: %v", err)
	}
	tt.Frame()
	if m.selectedSeq() != 2 || m.filter != resultFilterAll {
		t.Fatalf("下一件 moved to %d in filter %v", m.selectedSeq(), m.filter)
	}
	if err := tt.Click("下一件 ›"); err != nil {
		t.Fatalf("click 下一件: %v", err)
	}
	tt.Frame()
	if m.selectedSeq() != 3 {
		t.Fatalf("下一件 reached %d, want the warning item 3", m.selectedSeq())
	}
	if err := tt.Click("下一件 ›"); err != nil {
		t.Fatalf("click 下一件: %v", err)
	}
	tt.Frame()
	if m.selectedSeq() != 3 {
		t.Fatalf("下一件 wrapped past the last row: selected %d", m.selectedSeq())
	}
}

// TestReviewReadOnlyCropListAndFieldHighlight checks the fifth row of
// 08 §3.4: the read-only item keeps the same row-crop list, and choosing a
// field highlights its crop.
func TestReviewReadOnlyCropListAndFieldHighlight(t *testing.T) {
	m, _, tt := openResponsiveResults(t, recovery(roledSettledItem(t, 1)), 1180, 760)
	tt.Frame()
	m.setResultFilter(resultFilterAll)
	tt.Frame()
	if m.edit != nil {
		t.Fatalf("the passed item opened an editor: %s", probeReviewState(m))
	}
	for _, want := range []string{
		"套装", "等级", "主词条", "副词条 1", "副词条 2", "副词条 3", "副词条 4",
		"装备者头像",
	} {
		if !tt.HasText(want) {
			t.Fatalf("the read-only crop list is missing %q: %q", want, tt.Texts())
		}
	}

	before := countColor(tt.Image(), rgba(tokensFor(false).AccentSoft))
	if err := tt.Click("河豚电音（31100）"); err != nil {
		t.Fatalf("select the set field: %v", err)
	}
	tt.Frame()
	tt.Frame()
	if m.reviewField != FormFieldSetID {
		t.Fatalf("the clicked field = %q, want the set field", m.reviewField)
	}
	after := countColor(tt.Image(), rgba(tokensFor(false).AccentSoft))
	if after <= before {
		t.Fatalf("the set field did not highlight its crop: accent pixels %d → %d", before, after)
	}
}

// TestReviewReadOnlyCtrlJStillReachesPending checks the acceptance rule:
// browsing read-only items keeps Ctrl+J aimed at the next pending item.
func TestReviewReadOnlyCtrlJStillReachesPending(t *testing.T) {
	m, q, tt, _ := openReviewableResults(t, recovery(
		settledReviewItem(t, 1, record.QualityAutoAccepted),
		reviewRowItem(t, 2, 12),
		settledReviewItem(t, 3, record.QualityHumanConfirmed),
	), 1180, 760)
	tt.Frame()
	m.setResultFilter(resultFilterAll)
	tt.Frame()
	m.requestNavigation(navigationRequest{kind: navSelect, seq: 1})
	tt.Frame()
	if m.edit != nil || m.selectedSeq() != 1 {
		t.Fatalf("the passed item is not read-only: %s", probeReviewState(m))
	}
	tt.Key(ui.Cmd, ui.KeyJ)
	settle(t, m, q, tt)
	if m.selectedSeq() != 2 {
		t.Fatalf("Ctrl+J from a read-only item reached %d, want the pending item 2", m.selectedSeq())
	}
	if m.edit == nil || m.edit.seq != 2 {
		t.Fatalf("the pending item did not open its editor: %s", probeReviewState(m))
	}
}

// TestReviewModifyFlow checks the fourth row of 08 §3.4: 修改 switches the
// bottom bar to 确认修改 / 取消修改, focuses the first field, and 确认修改
// re-confirms the item in place. Ctrl+S is not a write key in this flow.
func TestReviewModifyFlow(t *testing.T) {
	m, q, tt, svc := openReviewableResults(t, recovery(settledReviewItem(t, 1, record.QualityAutoAccepted)), 1180, 760)
	tt.Frame()
	m.setResultFilter(resultFilterAll)
	tt.Frame()

	if err := tt.Click("修改"); err != nil {
		t.Fatalf("click 修改: %v", err)
	}
	tt.Frame()
	if m.edit == nil || !m.edit.modify {
		t.Fatalf("修改 did not open the modify editor: %s", probeReviewState(m))
	}
	for _, want := range []string{"确认修改", "取消修改"} {
		if !tt.HasText(want) {
			t.Fatalf("the modify bar is missing %q: %q", want, tt.Texts())
		}
	}
	for _, unwanted := range []string{"‹ 上一件", "下一件 ›", "确认并下一项", "保存修改"} {
		if tt.HasText(unwanted) {
			t.Fatalf("the modify bar still shows %q: %q", unwanted, tt.Texts())
		}
	}
	if m.focusedReviewField != FormFieldSetID {
		t.Fatalf("修改 did not focus the first field: %q", m.focusedReviewField)
	}
	if status := m.resultShortcutText(); !strings.Contains(status, shortcutConfirmNext+" 确认修改") ||
		strings.Contains(status, shortcutSave) {
		t.Fatalf("the modify status bar names the wrong keys: %q", status)
	}

	m.edit.draft.Level = "13"
	m.edit.refresh()
	tt.Frame()
	// Ctrl+S would save an unconfirmed revision, which F-20 forbids in the
	// modify flow: it is not bound.
	tt.Key(ui.Cmd, ui.KeyS)
	settle(t, m, q, tt)
	if svc.saveCount() != 0 || m.edit == nil || m.edit.draft.Level != "13" {
		t.Fatalf("Ctrl+S in the modify flow wrote or closed the draft: %s", probeReviewState(m))
	}

	tt.Key(ui.Cmd, ui.KeyEnter)
	settle(t, m, q, tt)
	tt.Frame()
	if svc.saveCount() != 1 || !svc.saves[0].edit.Confirmed {
		t.Fatalf("Ctrl+Enter in the modify flow = %+v, want one confirmed save", svc.saves)
	}
	if m.edit != nil {
		t.Fatalf("确认修改 kept the editor open: %s", probeReviewState(m))
	}
	if m.rec.Rows[0].Quality != record.QualityHumanConfirmed ||
		m.rec.Rows[0].ItemClass != app.ItemClassPassed {
		t.Fatalf("the modified item = quality %q class %q",
			m.rec.Rows[0].Quality, m.rec.Rows[0].ItemClass)
	}
	if !tt.HasText("已通过 · 已人工确认") || !tt.HasText("已确认修改（序号 1）。") {
		t.Fatalf("the confirmed modify state is missing: %q", tt.Texts())
	}
	if !tt.Focused("修改") {
		t.Fatalf("确认修改 did not return the keyboard to 修改: %q", tt.Texts())
	}
}

// TestReviewModifyCancelDiscards checks 取消修改: no revision is written
// and the item returns to its read-only text with the old values.
func TestReviewModifyCancelDiscards(t *testing.T) {
	m, q, tt, svc := openReviewableResults(t, recovery(settledReviewItem(t, 1, record.QualityAutoAccepted)), 1180, 760)
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
	if err := tt.Click("取消修改"); err != nil {
		t.Fatalf("click 取消修改: %v", err)
	}
	settle(t, m, q, tt)
	tt.Frame()
	if svc.saveCount() != 0 {
		t.Fatalf("取消修改 wrote %d revisions", svc.saveCount())
	}
	if m.edit != nil || m.rec.Rows[0].LevelText() != "15 级" {
		t.Fatalf("取消修改 kept the draft: %s", probeReviewState(m))
	}
	if !tt.HasText("已通过 · 自动通过") || !tt.Focused("修改") {
		t.Fatalf("取消修改 did not return to the read-only form: %q", tt.Texts())
	}
}

// TestReviewModifySaveFailureKeepsInput covers F-20: a failed save keeps
// the item, the draft byte-for-byte and the retry action in place.
func TestReviewModifySaveFailureKeepsInput(t *testing.T) {
	m, q, tt, svc := openReviewableResults(t, recovery(settledReviewItem(t, 1, record.QualityAutoAccepted)), 1180, 760)
	tt.Frame()
	m.setResultFilter(resultFilterAll)
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
	if err := tt.Click("确认修改"); err != nil {
		t.Fatalf("click 确认修改: %v", err)
	}
	drainAll(t, m, q)
	tt.Frame()

	if m.edit == nil || !m.edit.modify || m.edit.draft.Level != "13" {
		t.Fatalf("the failed modify save dropped the input: %s", probeReviewState(m))
	}
	if !m.reviewSaveFailed || !strings.Contains(m.reviewErr, errTestSave.Error()) {
		t.Fatalf("the failed modify save lost its reason: %s", probeReviewState(m))
	}
	if !tt.HasText("重试确认修改") || !tt.HasText("没有保存：") {
		t.Fatalf("the failed modify save has no retry: %q", tt.Texts())
	}
	if _, _, saved := m.workspaceStatusTexts(); saved != "保存失败" {
		t.Fatalf("the failed modify save title = %q", saved)
	}

	svc.saveFn = good
	if err := tt.Click("重试确认修改"); err != nil {
		t.Fatalf("retry the modify save: %v", err)
	}
	settle(t, m, q, tt)
	tt.Frame()
	if m.edit != nil || m.rec.Rows[0].LevelText() != "13 级" {
		t.Fatalf("the successful retry did not settle: %s", probeReviewState(m))
	}
	if !tt.HasText("已通过 · 已人工确认") {
		t.Fatalf("the retried item lost its confirmed status: %q", tt.Texts())
	}
}

// TestReviewModifyKeepsOriginalOutcome checks the JSON/CSV v2 §3 invariant:
// a backfilled item keeps its original processing outcome, while the
// classes and the export snapshot follow the re-confirmed revision through
// recordClassCounts.
func TestReviewModifyKeepsOriginalOutcome(t *testing.T) {
	backfilled := settledReviewItem(t, 1, record.QualityHumanConfirmed)
	backfilled.Status = record.StatusFailed
	backfilled.Failure = "识别超时"
	backfilled.Revision = 1
	m, q, tt, _ := openReviewableResults(t, recovery(backfilled), 1180, 760)
	tt.Frame()
	m.setResultFilter(resultFilterAll)
	tt.Frame()
	if m.edit != nil {
		t.Fatalf("a settled backfill opened an editor: %s", probeReviewState(m))
	}
	if !tt.HasText("已通过 · 已人工确认") {
		t.Fatalf("the backfilled status tag is missing: %q", tt.Texts())
	}
	if err := tt.Click("修改"); err != nil {
		t.Fatalf("click 修改: %v", err)
	}
	tt.Frame()
	m.edit.draft.Level = "14"
	m.edit.refresh()
	tt.Frame()
	if err := tt.Click("确认修改"); err != nil {
		t.Fatalf("click 确认修改: %v", err)
	}
	settle(t, m, q, tt)
	tt.Frame()

	row := &m.rec.Rows[0]
	if row.Status != record.StatusFailed || row.Failure != "识别超时" || !row.Backfilled {
		t.Fatalf("the original outcome was rewritten: status %q failure %q backfilled %v",
			row.Status, row.Failure, row.Backfilled)
	}
	if row.Quality != record.QualityHumanConfirmed || row.ItemClass != app.ItemClassPassed {
		t.Fatalf("the re-confirmed row = quality %q class %q", row.Quality, row.ItemClass)
	}
	counts := m.recordClassCounts()
	if counts.Passed != 1 || counts.Pending() != 0 {
		t.Fatalf("class counts after the modify = %+v", counts)
	}
	if m.exportData == nil || m.exportData.Summary.Counts != counts {
		t.Fatalf("export counts %+v do not follow recordClassCounts %+v",
			m.exportData, counts)
	}
}

// TestReviewWarningReadOnly checks the warning item's presentation from
// 08 §3.3: the warning field leads with its "!" marker and the optional
// value, and the notice band above the form is not an error style.
func TestReviewWarningReadOnly(t *testing.T) {
	m, _, tt, _ := openReviewableResults(t, recovery(equippedWarningReviewItem(t, 1)), 1180, 760)
	tt.Frame()
	m.setResultFilter(resultFilterWarning)
	tt.Frame()

	if m.edit != nil || m.rec.Rows[0].ItemClass != app.ItemClassWarning {
		t.Fatalf("the warning item is not read-only: %s", probeReviewState(m))
	}
	notice := "已纳入导出 · 警告：装备者未识别，导出时留空。"
	for _, want := range []string{notice, "!", "未识别（可留空导出）", "修改", "‹ 上一件", "下一件 ›"} {
		if !tt.HasText(want) {
			t.Fatalf("the warning form does not show %q: %q", want, tt.Texts())
		}
	}
	for _, unwanted := range []string{"确认并下一项", "保存修改", "确认修改", "取消修改", "需确认"} {
		if tt.HasText(unwanted) {
			t.Fatalf("the warning read-only form shows %q: %q", unwanted, tt.Texts())
		}
	}

	equipped, ok := tt.Find("未识别（可留空导出）")
	if !ok {
		t.Fatalf("the equipped value is missing: %q", tt.Texts())
	}
	set, ok := tt.Find("河豚电音（31100）")
	if !ok {
		t.Fatalf("the set value is missing: %q", tt.Texts())
	}
	if equipped.Y >= set.Y {
		t.Fatalf("the warning field is not first: equipped y=%v set y=%v", equipped.Y, set.Y)
	}

	// The notice band uses the control surface, not the danger surface:
	// a warning is not an error (08 §3.3, 09 §4.3).
	text, ok := tt.Find(notice)
	if !ok {
		t.Fatalf("the notice text has no rect: %q", tt.Texts())
	}
	x, y := int(text.X+text.W/2), int(text.Y)-3
	got := tt.Image().RGBAAt(x, y)
	if got != rgba(tokensFor(false).SurfaceControl) {
		t.Fatalf("the notice band background = %v, want %v",
			got, rgba(tokensFor(false).SurfaceControl))
	}
	if got == rgba(tokensFor(false).DangerSoft) {
		t.Fatal("the notice band uses the danger surface")
	}
}

// TestReviewWarningModifyFixesWarning checks the optional warning flow:
// filling the agent and confirming turns the item into a passed, human
// confirmed one and removes the warning from the counts.
func TestReviewWarningModifyFixesWarning(t *testing.T) {
	m, q, tt, _ := openReviewableResults(t, recovery(equippedWarningReviewItem(t, 1)), 1180, 760)
	tt.Frame()
	m.setResultFilter(resultFilterWarning)
	tt.Frame()
	if err := tt.Click("修改"); err != nil {
		t.Fatalf("click 修改: %v", err)
	}
	tt.Frame()
	if m.edit == nil || !m.edit.modify {
		t.Fatalf("the warning item did not enter the modify flow: %s", probeReviewState(m))
	}
	m.edit.draft.EquippedState = describeEquipped(string(zzz.EquippedStateEquipped))
	m.edit.draft.AgentID = describeAgent(1311)
	m.edit.refresh()
	if !m.edit.canConfirm() {
		t.Fatalf("the filled warning is not confirmable: %v %v", m.edit.fieldErrors, m.edit.check)
	}
	tt.Frame()
	if err := tt.Click("确认修改"); err != nil {
		t.Fatalf("click 确认修改: %v", err)
	}
	settle(t, m, q, tt)
	tt.Frame()

	row := &m.rec.Rows[0]
	if row.ItemClass != app.ItemClassPassed || row.Quality != record.QualityHumanConfirmed {
		t.Fatalf("the fixed warning row = class %q quality %q", row.ItemClass, row.Quality)
	}
	counts := m.recordClassCounts()
	if counts.Warning != 0 || counts.Passed != 1 {
		t.Fatalf("class counts after fixing the warning = %+v", counts)
	}
	if m.exportData == nil || m.exportData.Summary.Counts != counts {
		t.Fatalf("export counts do not follow recordClassCounts: %+v vs %+v",
			m.exportData, counts)
	}

	m.setResultFilter(resultFilterAll)
	tt.Frame()
	if !tt.HasText("已通过 · 已人工确认") || tt.HasText("已纳入导出 · 警告") {
		t.Fatalf("the fixed warning still shows its band or lost its tag: %q", tt.Texts())
	}
}
