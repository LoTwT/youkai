package ui

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/game/zzz"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/LoTwT/youkai/internal/scan"
	"github.com/egoist/mygo/ui"
)

// reviewPageHeight is the page shell's 1024 split window height. The reveal
// helper needs the same height to keep its fallback pane bottom inside the
// real window.
const reviewPageHeight = 700

// revealDetailControl returns a control's laid-out rect. The review's
// action bar is fixed at the bottom of the form pane, so a bottom action
// is always on screen; form fields scroll inside their own pane and are
// clicked directly by the tests that exercise them.
func revealDetailControl(t *testing.T, tt *ui.Tester, windowHeight float32, label string) ui.Rect {
	t.Helper()
	rect, ok := tt.Find(label)
	if !ok {
		t.Fatalf("%q is missing: %q", label, tt.Texts())
	}
	_ = windowHeight
	return rect
}

// clickRevealedDetailControl reveals an editor action before clicking it.
// Click still validates the final point against the window bounds.
func clickRevealedDetailControl(t *testing.T, tt *ui.Tester, windowHeight float32, label string) {
	t.Helper()
	revealDetailControl(t, tt, windowHeight, label)
	if err := tt.Click(label); err != nil {
		t.Fatal(err)
	}
}

// completeReviewData is a complete, confirmable drive-disc data object.
func completeReviewData(level int) zzz.DiscData {
	setID, rarity, slot := 31100, zzz.RarityS, 4
	mainAttribute, mainValue, mainUnit := string(zzz.AttrCritRate), "19.2", string(zzz.UnitPercent)
	subAttribute, subValue, subUnit, roll := string(zzz.AttrCritDamage), "19.2", string(zzz.UnitPercent), 3
	management := zzz.ManagementNone
	return zzz.DiscData{
		SetID:  &setID,
		Rarity: &rarity,
		Slot:   &slot,
		Level:  &level,
		MainStat: &zzz.StatData{
			Attribute: &mainAttribute,
			Value:     &mainValue,
			Unit:      &mainUnit,
		},
		Substats: []zzz.SubstatData{{
			StatData:         zzz.StatData{Attribute: &subAttribute, Value: &subValue, Unit: &subUnit},
			EnhancementRolls: &roll,
		}},
		ManagementState: &management,
		Equipped:        &zzz.Equipped{State: zzz.EquippedStateUnequipped},
	}
}

// reviewRowItem builds one produced item carrying complete structured
// data and raw recognized lines.
func reviewRowItem(t *testing.T, seq int64, level int) record.ItemRecord {
	t.Helper()
	it := item(seq, record.StatusProduced, "", "", "河豚电音", "等级"+fmt.Sprint(level))
	structured := zzz.StructuredDisc{
		Data:       completeReviewData(level),
		Issues:     []zzz.Issue{},
		Validation: zzz.Validation{Status: zzz.ValidationPassed, Issues: []zzz.Issue{}},
	}
	raw, err := json.Marshal(structured)
	if err != nil {
		t.Fatalf("marshal structured data: %v", err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(it.Data, &doc); err != nil {
		t.Fatalf("decode item data: %v", err)
	}
	doc["structured"] = raw
	it.Data, err = json.Marshal(doc)
	if err != nil {
		t.Fatalf("encode item data: %v", err)
	}
	it.Quality = record.QualityNeedsReview
	return it
}

// withStructured rewrites one review item's persisted structured payload
// through mutate, so tests can build an export-excluded revision.
func withStructured(t *testing.T, it record.ItemRecord, mutate func(*zzz.StructuredDisc)) record.ItemRecord {
	t.Helper()
	structured := parseResult(it.Data).Structured
	if structured == nil {
		t.Fatal("the item has no structured payload")
	}
	mutate(structured)
	raw, err := json.Marshal(structured)
	if err != nil {
		t.Fatalf("marshal structured data: %v", err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(it.Data, &doc); err != nil {
		t.Fatalf("decode item data: %v", err)
	}
	doc["structured"] = raw
	it.Data, err = json.Marshal(doc)
	if err != nil {
		t.Fatalf("encode item data: %v", err)
	}
	return it
}

// installReviewSave makes the fake service persist SaveItemRevision like
// the real service: it advances the item, recomputes validation, keeps
// the raw lines and rejects a stale expected revision.
func installReviewSave(svc *fakeService) {
	svc.saveFn = func(recordID string, sequence, expectedRevision int64, edit app.ItemRevisionEdit) (app.ItemRevisionResult, error) {
		svc.mu.Lock()
		defer svc.mu.Unlock()
		rec := svc.loads[recordID]
		if rec == nil {
			return app.ItemRevisionResult{}, fmt.Errorf("no record %s", recordID)
		}
		for i := range rec.Checkpoint.Items {
			current := &rec.Checkpoint.Items[i]
			if current.Sequence != sequence {
				continue
			}
			if current.Revision != expectedRevision {
				return app.ItemRevisionResult{}, fmt.Errorf("item %d is at revision %d: %w",
					sequence, current.Revision, record.ErrRevisionConflict)
			}
			updated := *current
			updated.Revision = expectedRevision + 1
			// Keep the original processing outcome: a backfill adds the
			// interpretation and quality, never rewrites Status/Failure/Reason.
			if edit.Confirmed {
				updated.Quality = record.QualityHumanConfirmed
			} else {
				updated.Quality = record.QualityNeedsReview
			}
			check := app.CheckDiscData(edit.Data)
			structured := zzz.StructuredDisc{
				Data:       edit.Data,
				Issues:     check.Issues,
				Validation: check.Validation,
			}
			raw, err := json.Marshal(structured)
			if err != nil {
				return app.ItemRevisionResult{}, err
			}
			doc := make(map[string]json.RawMessage)
			if len(current.Data) > 0 {
				if err := json.Unmarshal(current.Data, &doc); err != nil {
					return app.ItemRevisionResult{}, err
				}
			}
			doc["structured"] = raw
			updated.Data, err = json.Marshal(doc)
			if err != nil {
				return app.ItemRevisionResult{}, err
			}
			*current = updated
			return app.ItemRevisionResult{Item: updated, Generation: 1}, nil
		}
		return app.ItemRevisionResult{}, fmt.Errorf("item %d: %w", sequence, record.ErrItemNotFound)
	}
}

// openReviewRecordPage opens one history record on the four-page results
// destination, where the review actions and completion state live.
func openReviewRecordPage(t *testing.T, items ...record.ItemRecord) (*Model, *queueUpdater, *ui.Tester, *fakeService) {
	t.Helper()
	svc := &fakeService{
		list: []record.RecordInfo{{
			ID:             "r1",
			SavedAt:        time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC),
			LatestRevision: 1,
		}},
	}
	rec := recovery(items...)
	rec.Checkpoint.Session.SessionID = "r1"
	svc.loads = map[string]*record.Recovery{"r1": rec}
	installReviewSave(svc)
	m, q := newTestModel(t, Config{Service: svc})
	m.SetContentSize(1024, reviewPageHeight)
	tt := ui.NewTester(m.View, 1024, reviewPageHeight)
	settle(t, m, q, tt)
	m.histChoice = historyLabel(t, m, "r1")
	m.OpenHistory()
	settle(t, m, q, tt)
	showRecordItems(m)
	tt.Frame()
	// These tests drive the item browser; the shell's own tests cover the
	// summary landing.
	m.recordTab = recordTabItems
	tt.Frame()
	return m, q, tt, svc
}

// TestReviewResultNoticeStaysOnItsPage covers L-D: an ordinary save result
// is not repeated on settings after leaving the item browser.
func TestReviewResultNoticeStaysOnItsPage(t *testing.T) {
	m, q, tt, _ := openReviewRecordPage(t, reviewRowItem(t, 1, 12))
	tt.Frame()
	m.edit.draft.Level = "13"
	tt.Frame()
	if err := tt.Click("保存修改"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	const notice = "已保存修订 1。"
	if !tt.HasText(notice) {
		t.Fatalf("the save result is missing on its own page: %q", tt.Texts())
	}
	m.navigatePage(pageSettings)
	tt.Frame()
	if m.reviewNotice != "" {
		t.Fatalf("the save result survived the page change: %q", m.reviewNotice)
	}
	if tt.HasText(notice) {
		t.Fatalf("settings repeats the item browser's save result: %q", tt.Texts())
	}
}

// TestReviewResultNoticeDoesNotFollowTheTab covers L-D's remaining path:
// 逐件 and 摘要 are separate screens, so an ordinary save result does not
// appear after switching to the summary tab.
func TestReviewResultNoticeDoesNotFollowTheTab(t *testing.T) {
	m, q, tt, _ := openReviewRecordPage(t, reviewRowItem(t, 1, 12))
	tt.Frame()
	m.edit.draft.Level = "13"
	tt.Frame()
	if err := tt.Click("保存修改"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	const notice = "已保存修订 1。"
	if !tt.HasText(notice) {
		t.Fatalf("the save result is missing on the item tab: %q", tt.Texts())
	}
	m.requestNavigation(navigationRequest{kind: navRecordTab, recordTab: recordTabSummary})
	tt.Frame()
	if m.reviewNotice != "" {
		t.Fatalf("the item result survived the tab switch: %q", m.reviewNotice)
	}
	if tt.HasText(notice) {
		t.Fatalf("the summary repeats the item tab's save result: %q", tt.Texts())
	}
}

// TestRecordWorkspaceSummaryDropsOrdinaryReviewNotice covers the other
// direct 摘要 switch: when a finished scan lands on the open record's
// summary, an ordinary result from the item browser must not come along.
func TestRecordWorkspaceSummaryDropsOrdinaryReviewNotice(t *testing.T) {
	m, _, tt, _ := openReviewRecordPage(t, reviewRowItem(t, 1, 12))
	tt.Frame()
	const notice = "已确认并定位下一项（序号 1）。"
	m.setReviewNotice(notice)
	m.openRecordWorkspace()
	tt.Frame()
	if m.recordTab != recordTabSummary {
		t.Fatalf("the workspace opened tab %v", m.recordTab)
	}
	if m.reviewNotice != "" || tt.HasText(notice) {
		t.Fatalf("the item result followed the summary switch: %q", tt.Texts())
	}
}

// TestInvalidInputIsStillUnsaved covers L-B: an invalid value with no data
// representation is a real change, so leaving the item prompts instead of
// silently discarding it.
func TestInvalidInputIsStillUnsaved(t *testing.T) {
	m, _, tt, _ := openReviewRecordPage(t, reviewRowItem(t, 1, 12))
	tt.Frame()
	m.edit.draft.Level = "99"
	m.edit.refresh()
	tt.Frame()
	if !m.edit.dirty() {
		t.Fatal("the out-of-range level was treated as unchanged")
	}
	if m.edit.canSave() {
		t.Fatal("the invalid level was offered for saving")
	}
	m.requestNavigation(navigationRequest{kind: navPage, page: pageSettings})
	tt.Frame()
	if m.navPrompt == nil {
		t.Fatal("leaving with the invalid input did not prompt")
	}
	if !tt.HasText("有未保存的修改") {
		t.Fatalf("the unsaved prompt does not show: %q", tt.Texts())
	}
	m.promptStay()
	tt.Frame()
	if m.edit == nil || m.edit.draft.Level != "99" {
		t.Fatalf("staying lost the invalid input: %s", probeReviewState(m))
	}
}

// TestSavedInvalidValueIsNotDirtyUntilEdited covers the L-B regression: an
// invalid persisted value is unchanged until the user actually edits it.
func TestSavedInvalidValueIsNotDirtyUntilEdited(t *testing.T) {
	it := withStructured(t, reviewRowItem(t, 1, 12), func(s *zzz.StructuredDisc) {
		level := 99
		s.Data.Level = &level
		s.Data.Substats = []zzz.SubstatData{}
	})
	m, _, tt, _ := openReviewableResults(t, recovery(it), 1180, 760)
	tt.Frame()
	if m.edit == nil || m.edit.draft.Level != "99" {
		t.Fatalf("the saved invalid level did not open: %s", probeReviewState(m))
	}
	if m.edit.original.Substats == nil || m.edit.draft.Substats == nil {
		t.Fatalf("the confirmed empty substat list lost its shape: original=%v draft=%v",
			m.edit.original.Substats, m.edit.draft.Substats)
	}
	data, problems := m.edit.draft.toData()
	if len(problems) == 0 || data.Substats == nil || len(data.Substats) != 0 {
		t.Fatalf("toData = %+v, problems %+v; want the confirmed empty list", data, problems)
	}
	if m.editingDirty() {
		t.Fatal("the untouched saved invalid value is dirty")
	}
	m.requestNavigation(navigationRequest{kind: navPage, page: pageSettings})
	tt.Frame()
	if m.navPrompt != nil {
		t.Fatalf("leaving with an untouched invalid value prompted: %s", probeReviewState(m))
	}
	if m.displayPage() != pageSettings {
		t.Fatalf("the navigation did not run: %s", probeReviewState(m))
	}

	// A real edit to the invalid value is still unsaved input.
	m.navigatePage(pageResults)
	tt.Frame()
	tt.Frame()
	if m.edit == nil {
		t.Fatalf("the editor did not reopen: %s", probeReviewState(m))
	}
	m.edit.draft.Level = "98"
	m.edit.refresh()
	tt.Frame()
	if !m.editingDirty() {
		t.Fatal("editing the invalid value did not mark it dirty")
	}
}

func TestDraftKeepsExplicitUnknownSubstats(t *testing.T) {
	draft := editDraft{
		SetID:           describeSet(31100),
		MainAttribute:   unreadOption,
		MainUnit:        unreadOption,
		ManagementState: unreadOption,
		EquippedState:   unreadOption,
		AgentID:         unreadOption,
		SubstatsKnown:   true,
		Substats: []substatDraft{
			{},             // the line exists but nothing was read
			{Value: "9.6"}, // a present value with unread identity
			{Attribute: unreadOption, Unit: unreadOption, Rolls: "3"},
		},
	}
	data, problems := draft.toData(nil)
	if len(problems) != 0 {
		t.Fatalf("explicit unknowns reported field errors: %+v", problems)
	}
	if len(data.Substats) != 3 {
		t.Fatalf("substats = %+v, want all three lines kept", data.Substats)
	}
	if data.Substats[0].Attribute != nil || data.Substats[0].Value != nil ||
		data.Substats[0].Unit != nil || data.Substats[0].EnhancementRolls != nil {
		t.Fatalf("a wholly unread line gained values: %+v", data.Substats[0])
	}
	if data.Substats[1].Value == nil || *data.Substats[1].Value != "9.6" ||
		data.Substats[1].Attribute != nil {
		t.Fatalf("the partially read line was not preserved: %+v", data.Substats[1])
	}
	if data.Substats[2].EnhancementRolls == nil || *data.Substats[2].EnhancementRolls != 3 {
		t.Fatalf("the roll was not preserved: %+v", data.Substats[2])
	}
	check := app.CheckDiscData(data)
	if len(check.Blocking) != 0 || check.Confirmable() {
		t.Fatalf("unknown substats: blocking %+v confirmable %v", check.Blocking, check.Confirmable())
	}
}

func TestDraftRejectsInvalidPresentValues(t *testing.T) {
	base := editDraft{
		SetID:           describeSet(31100),
		Slot:            "4",
		Level:           "12",
		MainAttribute:   describeAttribute(string(zzz.AttrCritRate)),
		MainValue:       "19.2",
		MainUnit:        describeUnit(string(zzz.UnitPercent)),
		ManagementState: describeManagement(zzz.ManagementNone),
		EquippedState:   describeEquipped(zzz.EquippedStateUnequipped),
		AgentID:         unreadOption,
		SubstatsKnown:   true,
	}
	cases := []struct {
		name  string
		apply func(*editDraft)
		field string
	}{
		{"set", func(d *editDraft) { d.SetID = "999999 未知套装" }, "setId"},
		{"slot", func(d *editDraft) { d.Slot = "7" }, "slot"},
		{"level", func(d *editDraft) { d.Level = "16" }, "level"},
		{"value", func(d *editDraft) { d.MainValue = "9.60" }, "mainStat"},
		{"management", func(d *editDraft) { d.ManagementState = "bogus 未知状态" }, "managementState"},
		{"agent", func(d *editDraft) {
			d.EquippedState = describeEquipped(zzz.EquippedStateEquipped)
			d.AgentID = "9999 未知代理人"
		}, "equipped"},
		{"substat unit", func(d *editDraft) {
			d.Substats = []substatDraft{{
				Attribute: describeAttribute(string(zzz.AttrCritRate)),
				Value:     "9.6",
				Unit:      "bogus 未知单位",
			}}
		}, "substats[0]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			draft := cloneDraft(base)
			tc.apply(&draft)
			_, problems := draft.toData(nil)
			found := false
			for _, problem := range problems {
				if problem.Field == tc.field {
					found = true
				}
			}
			if !found {
				t.Fatalf("problems = %+v, want field %s", problems, tc.field)
			}
		})
	}
}

func TestCatalogChoicesCoverTheFixedTables(t *testing.T) {
	sets := setChoices(nil)
	if len(sets) != 31 {
		t.Fatalf("set choices = %d, want the unread option and 30 sets", len(sets))
	}
	agents := agentChoices(nil)
	if len(agents) != 61 {
		t.Fatalf("agent choices = %d, want the unread option and 60 agents", len(agents))
	}
	attributes := attributeChoices(nil)
	if len(attributes) != len(reviewAttributes)+1 {
		t.Fatalf("attribute choices = %d, want the unread option and %d attributes",
			len(attributes), len(reviewAttributes))
	}
	units := unitChoices(nil, nil)
	if len(units) != 3 {
		t.Fatalf("unit choices = %v, want unread, flat and percent", units)
	}
	crit := string(zzz.AttrCritRate)
	if got := unitChoices(&crit, nil); len(got) != 2 {
		t.Fatalf("crit-rate units = %v, want unread and percent", got)
	}
}

func TestEditSavePersistsRevision(t *testing.T) {
	m, q, tt, svc := openReviewRecordPage(t, reviewRowItem(t, 1, 12))
	if !tt.HasText("待校对 1 件") {
		t.Fatalf("the pending count does not show: %q", tt.Texts())
	}
	tt.Frame()
	if !tt.HasText("确认并下一项") || !tt.HasText("保存修改") {
		t.Fatalf("the editor does not show: %q", tt.Texts())
	}
	if _, ok := tt.Find("保存修改"); !ok {
		t.Fatalf("the editor does not offer its plain save: %q", tt.Texts())
	}
	if m.edit == nil || m.edit.draft.Level != "12" {
		t.Fatalf("the editor did not start from the row: %+v", m.edit)
	}
	if m.edit.canSave() {
		t.Fatal("an unchanged draft was offered as a modification")
	}
	if err := tt.Click("保存修改"); err != nil {
		t.Fatal(err)
	}
	drainAll(t, m, q)
	if svc.saveCount() != 0 {
		t.Fatalf("an unchanged draft saved %d revisions", svc.saveCount())
	}

	m.edit.draft.Level = "13"
	tt.Frame()
	if err := tt.Click("保存修改"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)

	if svc.saveCount() != 1 {
		t.Fatalf("save calls = %d, want 1", svc.saveCount())
	}
	call := svc.saves[0]
	if call.expectedRevision != 0 || call.edit.Confirmed {
		t.Fatalf("save call = %+v, want revision 0 and unconfirmed", call)
	}
	if call.edit.Data.Level == nil || *call.edit.Data.Level != 13 {
		t.Fatalf("saved data level = %v, want 13", call.edit.Data.Level)
	}
	if m.edit == nil || m.edit.draft.Level != "13" {
		t.Fatalf("the editable form did not reopen on the saved item: %+v", m.edit)
	}
	if m.reviewErr != "" || m.reviewNotice != "已保存修订 1。" {
		t.Fatalf("save state = err %q notice %q", m.reviewErr, m.reviewNotice)
	}
	row := m.rec.Rows[0]
	if row.Revision != 1 || row.Quality != record.QualityNeedsReview || row.LevelText() != "13 级" {
		t.Fatalf("row after save = revision %d quality %q level %q",
			row.Revision, row.Quality, row.LevelText())
	}
	if !tt.HasText("13 级") || !tt.HasText("已保存修订 1。") {
		t.Fatalf("the saved revision does not show: %q", tt.Texts())
	}
}

func TestConfirmAndNextThroughToEmpty(t *testing.T) {
	m, q, tt, svc := openReviewRecordPage(t,
		reviewRowItem(t, 1, 12),
		reviewRowItem(t, 2, 10),
	)
	if m.selected != 0 {
		t.Fatalf("default selection = %d, want the first item", m.selected)
	}
	if err := tt.Click("确认并下一项"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if svc.saveCount() != 1 || !svc.saves[0].edit.Confirmed {
		t.Fatalf("confirm call = %+v, want a confirmed save", svc.saves)
	}
	if m.rec.Rows[0].Quality != record.QualityHumanConfirmed || m.rec.Rows[0].Revision != 1 {
		t.Fatalf("first item after confirm = quality %q revision %d",
			m.rec.Rows[0].Quality, m.rec.Rows[0].Revision)
	}
	if m.selected != 1 {
		t.Fatalf("confirm did not move to the next pending item: %d", m.selected)
	}
	if !tt.HasText("已确认并定位下一项（序号 2）。") {
		t.Fatalf("the next-item notice does not show: %q", tt.Texts())
	}

	if err := tt.Click("确认并下一项"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if m.pendingItemsCount() != 0 || m.reviewNotice != "" {
		t.Fatalf("after the last item: pending %d notice %q", m.pendingItemsCount(), m.reviewNotice)
	}
	if m.selected != 1 {
		t.Fatalf("the last confirmation moved away from the last item: %d", m.selected)
	}
	if !tt.HasText("待校对 0") {
		t.Fatalf("the empty pending state does not show: %q", tt.Texts())
	}
	if tt.HasText("待校对 0 · 全部可导出") {
		t.Fatalf("the completed form repeats its own conclusion: %q", tt.Texts())
	}
}

func TestConfirmStayKeepsItemInPendingSegment(t *testing.T) {
	m, q, tt, svc := openReviewRecordPage(t,
		reviewRowItem(t, 1, 12),
		reviewRowItem(t, 2, 10),
	)
	m.confirmCurrentStay()
	settle(t, m, q, tt)
	if svc.saveCount() != 1 || !svc.saves[0].edit.Confirmed {
		t.Fatalf("confirm-and-stay call = %+v, want one confirmed save", svc.saves)
	}
	if m.selected != 0 || m.pendingItemsCount() != 1 {
		t.Fatalf("confirm-and-stay moved: selected %d pending %d", m.selected, m.pendingItemsCount())
	}
	if got := m.visibleResultRows(); len(got) != 2 || got[0] != 0 || got[1] != 1 {
		t.Fatalf("the confirmed row left the pending segment: rows %v", got)
	}
	if !m.recentlyConfirmedNow(&m.rec.Rows[0]) {
		t.Fatal("the confirmed row is not marked for the pending segment")
	}
	if !tt.HasText("已确认") {
		t.Fatalf("the confirmed row is not marked: %q", tt.Texts())
	}
	if !tt.HasText("已确认（序号 1），仍停留在当前项。") {
		t.Fatalf("the stay notice does not show: %q", tt.Texts())
	}

	// Leaving the results destination refreshes the pending segment; the
	// same confirmed row no longer lingers there on return.
	if err := tt.Click("‹ 全部记录"); err != nil {
		t.Fatalf("click 返回历史: %v", err)
	}
	tt.Frame()
	if m.recentlyConfirmed != nil {
		t.Fatal("leaving the results page kept the confirmed row")
	}
	if err := tt.Click(historyTimeText(m.history[0])); err != nil {
		t.Fatalf("select the record: %v", err)
	}
	tt.Key(0, ui.KeyEnter)
	settle(t, m, q, tt)
	if m.displayPage() != pageResults {
		t.Fatalf("returning to the scan destination shows page %v", m.displayPage())
	}
	if got := m.visibleResultRows(); len(got) != 1 || got[0] != 1 {
		t.Fatalf("the pending segment after return shows rows %v", got)
	}
	if m.rec.Rows[0].QualityText() != "人工确认" {
		t.Fatalf("the refreshed row quality = %q", m.rec.Rows[0].QualityText())
	}
}

func TestConfirmLastItemStayShowsFinishedReview(t *testing.T) {
	m, q, tt, svc := openReviewRecordPage(t, reviewRowItem(t, 1, 12))
	m.confirmCurrentStay()
	settle(t, m, q, tt)
	if svc.saveCount() != 1 || !svc.saves[0].edit.Confirmed {
		t.Fatalf("last confirm-and-stay calls = %+v", svc.saves)
	}
	if m.pendingItemsCount() != 0 || !m.completionVisible() {
		t.Fatalf("the last confirm-and-stay did not complete: pending %d visible %v",
			m.pendingItemsCount(), m.completionVisible())
	}
	if !tt.HasText("都处理完了") || !tt.HasText("导出…") {
		t.Fatalf("the finished review state is missing: %q", tt.Texts())
	}
	if tt.HasText("修改") {
		t.Fatalf("the finished review kept the item actions: %q", tt.Texts())
	}
}

func TestEditReviewActionsStayOrAdvance(t *testing.T) {
	t.Run("save and confirm stays", func(t *testing.T) {
		m, q, tt, svc := openReviewRecordPage(t,
			reviewRowItem(t, 1, 12),
			reviewRowItem(t, 2, 10),
		)
		tt.Frame()
		for _, label := range []string{"保存修改", "确认并下一项"} {
			if _, ok := tt.Find(label); !ok {
				t.Fatalf("the editor does not offer %q: %q", label, tt.Texts())
			}
		}
		m.edit.draft.Level = "13"
		m.saveEdit(reviewConfirmStay, nil)
		settle(t, m, q, tt)
		if svc.saveCount() != 1 || !svc.saves[0].edit.Confirmed {
			t.Fatalf("save-and-confirm calls = %+v", svc.saves)
		}
		if m.selected != 0 || m.pendingItemsCount() != 1 {
			t.Fatalf("save-and-confirm moved: selected %d pending %d",
				m.selected, m.pendingItemsCount())
		}
		if !tt.HasText("修改") {
			t.Fatal("the settled item lost its read-only fallback")
		}
		if !m.recentlyConfirmedNow(&m.rec.Rows[0]) {
			t.Fatal("save-and-confirm did not keep the row in the pending segment")
		}
	})

	t.Run("save and confirm advances", func(t *testing.T) {
		m, q, tt, _ := openReviewRecordPage(t,
			reviewRowItem(t, 1, 12),
			reviewRowItem(t, 2, 10),
		)
		tt.Frame()
		m.edit.draft.Level = "13"
		tt.Frame()
		clickRevealedDetailControl(t, tt, reviewPageHeight, "确认并下一项")
		settle(t, m, q, tt)
		if m.selected != 1 || m.pendingItemsCount() != 1 {
			t.Fatalf("save-and-confirm-next did not move: selected %d pending %d",
				m.selected, m.pendingItemsCount())
		}
		if m.edit == nil || m.edit.seq != 2 || m.edit.draft.Level != "10" {
			t.Fatalf("the next item's editor did not open: %+v", m.edit)
		}
		if m.reviewNotice != "已确认并定位下一项（序号 2）。" {
			t.Fatalf("advance notice = %q", m.reviewNotice)
		}
	})
}

func TestAutoAcceptedHasNoConfirmationActions(t *testing.T) {
	auto := reviewRowItem(t, 1, 12)
	auto.Quality = record.QualityAutoAccepted
	_, _, tt, _ := openReviewRecordPage(t, auto)
	if !tt.HasText("都处理完了") {
		t.Fatalf("the settled record does not show its finished queue: %q", tt.Texts())
	}
	if err := tt.Click("全部"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, ok := tt.Find("修改"); !ok {
		t.Fatalf("the auto-accepted item has no edit action: %q", tt.Texts())
	}
	if _, ok := tt.Find("确认并下一项"); ok {
		t.Fatal("the auto-accepted item offers a confirmation action")
	}
}

func TestUnexportableItemShowsItsMissingFields(t *testing.T) {
	invalid := reviewRowItem(t, 1, 12)
	invalid.Quality = record.QualityAutoAccepted
	invalid = withStructured(t, invalid, func(structured *zzz.StructuredDisc) {
		structured.Data.ManagementState = nil
	})
	_, _, tt, _ := openReviewRecordPage(t, invalid)
	if _, ok := tt.Find("修改"); ok {
		t.Fatalf("a pending item offered the settled form's 修改 action: %q", tt.Texts())
	}
	if !tt.HasText("管理状态") || !tt.HasText("需补全") || !tt.HasText("暂时不能确认") {
		t.Fatalf("the incomplete item does not explain its gap: %q", tt.Texts())
	}
	if _, ok := tt.Find("确认并下一项"); !ok {
		t.Fatalf("the incomplete item has no disabled confirmation: %q", tt.Texts())
	}
}

func TestConfirmStayFailureKeepsItemAndRetries(t *testing.T) {
	m, q, tt, svc := openReviewRecordPage(t,
		reviewRowItem(t, 1, 12),
		reviewRowItem(t, 2, 10),
	)
	svc.saveFn = func(string, int64, int64, app.ItemRevisionEdit) (app.ItemRevisionResult, error) {
		return app.ItemRevisionResult{}, errors.New("disk full")
	}
	m.confirmCurrentStay()
	settle(t, m, q, tt)
	if m.pendingItemsCount() != 2 || m.selected != 0 || m.recentlyConfirmedNow(&m.rec.Rows[0]) {
		t.Fatalf("a failed confirmation settled the row: pending %d selected %d",
			m.pendingItemsCount(), m.selected)
	}
	if !strings.Contains(m.reviewErr, "disk full") {
		t.Fatalf("the confirmation failure = %q", m.reviewErr)
	}
	if _, ok := tt.Find("重试确认"); !ok {
		t.Fatalf("the retry confirmation action is missing: %q", tt.Texts())
	}

	installReviewSave(svc)
	if err := tt.Click("重试确认"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if m.rec.Rows[0].Quality != record.QualityHumanConfirmed || !m.recentlyConfirmedNow(&m.rec.Rows[0]) {
		t.Fatalf("the retried confirmation did not stay: quality %q", m.rec.Rows[0].Quality)
	}
}

func TestSaveAndConfirmNextFailureKeepsEditor(t *testing.T) {
	m, q, tt, svc := openReviewRecordPage(t,
		reviewRowItem(t, 1, 12),
		reviewRowItem(t, 2, 10),
	)
	svc.saveFn = func(string, int64, int64, app.ItemRevisionEdit) (app.ItemRevisionResult, error) {
		return app.ItemRevisionResult{}, errors.New("disk full")
	}
	tt.Frame()
	m.edit.draft.Level = "13"
	tt.Frame()
	clickRevealedDetailControl(t, tt, reviewPageHeight, "确认并下一项")
	settle(t, m, q, tt)
	if m.edit == nil || m.edit.draft.Level != "13" || m.selected != 0 {
		t.Fatalf("a failed save-and-confirm lost the editor or moved: edit %+v selected %d",
			m.edit, m.selected)
	}
	if retry := revealDetailControl(t, tt, reviewPageHeight, "重试确认并下一项"); retry.H <= 0 {
		t.Fatalf("the retry action is not visible: %+v", retry)
	}
	if !strings.Contains(m.reviewErr, "disk full") {
		t.Fatalf("the failure is not reported: %q", m.reviewErr)
	}
}

func TestConfirmRequiresCompleteValidation(t *testing.T) {
	m, q, tt, svc := openReviewRecordPage(t, reviewRowItem(t, 1, 12))
	tt.Frame()

	m.edit.draft.ManagementState = unreadOption
	tt.Frame()
	if m.edit.canConfirm() {
		t.Fatal("an unread required field was confirmable")
	}
	if err := tt.Click("确认并下一项"); err == nil {
		// The button exists but is disabled; a click must save nothing.
		drainAll(t, m, q)
	}
	if svc.saveCount() != 0 {
		t.Fatalf("a disabled confirm saved %d revisions", svc.saveCount())
	}
	if !tt.HasText("管理状态未读取") {
		t.Fatalf("the missing required field is not explained: %q", tt.Texts())
	}

	m.edit.draft.ManagementState = describeManagement(zzz.ManagementNone)
	tt.Frame()
	if !m.edit.canConfirm() {
		t.Fatalf("complete data is not confirmable: %+v", m.edit.check)
	}
}

func TestSaveFailureKeepsInputAndRetries(t *testing.T) {
	m, q, tt, svc := openReviewRecordPage(t, reviewRowItem(t, 1, 12))
	svc.saveFn = func(string, int64, int64, app.ItemRevisionEdit) (app.ItemRevisionResult, error) {
		return app.ItemRevisionResult{}, errors.New("disk full")
	}

	tt.Frame()
	m.edit.draft.Level = "13"
	tt.Frame()
	if err := tt.Click("保存修改"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)

	if m.edit == nil || m.edit.draft.Level != "13" {
		t.Fatalf("a failed save lost the input: %+v", m.edit)
	}
	if m.selected != 0 || m.rec.Rows[0].LevelText() != "12 级" {
		t.Fatalf("a failed save changed the row: selected %d level %q", m.selected, m.rec.Rows[0].LevelText())
	}
	if !tt.HasText("保存失败：disk full") || !tt.HasText("重试保存") {
		t.Fatalf("the failure or retry does not show: %q", tt.Texts())
	}

	installReviewSave(svc)
	if err := tt.Click("重试保存"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if m.edit == nil || m.edit.draft.Level != "13" {
		t.Fatalf("the retry did not reopen the saved draft: %+v", m.edit)
	}
	if m.rec.Rows[0].LevelText() != "13 级" {
		t.Fatalf("the retry did not apply the input: %q", m.rec.Rows[0].LevelText())
	}
}

func TestRevisionConflictReloadsCurrentRevision(t *testing.T) {
	m, q, tt, svc := openReviewRecordPage(t, reviewRowItem(t, 1, 12))
	tt.Frame()
	m.edit.draft.Level = "14"

	// Another editor advanced the item before this save landed.
	svc.mu.Lock()
	newer := reviewRowItem(t, 1, 11)
	newer.Revision = 2
	svc.loads["r1"].Checkpoint.Items[0] = newer
	svc.mu.Unlock()

	tt.Frame()
	if err := tt.Click("保存修改"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)

	if m.edit == nil || m.edit.draft.Level != "11" {
		t.Fatalf("the reloaded editor did not show the newer revision: %+v", m.edit)
	}
	if !tt.HasText("记录已被更新") {
		t.Fatalf("the conflict is not explained: %q", tt.Texts())
	}
	if m.rec.Rows[0].Revision != 2 || m.rec.Rows[0].LevelText() != "11 级" {
		t.Fatalf("the reloaded row = revision %d level %q, want the newer revision",
			m.rec.Rows[0].Revision, m.rec.Rows[0].LevelText())
	}
	loaded := false
	for _, id := range svc.loadLog {
		if id == "r1" {
			loaded = true
		}
	}
	if !loaded {
		t.Fatalf("the conflict did not reload the record: %v", svc.loadLog)
	}
}

// TestRevisionConflictNoticeSurvivesLeavingFilter covers PR-14b: the
// reloaded row may move out of the pending segment, but the explanation
// for the discarded input must stay visible.
func TestRevisionConflictNoticeSurvivesLeavingFilter(t *testing.T) {
	m, q, tt, svc := openReviewableResults(t, recovery(
		reviewRowItem(t, 1, 12),
		reviewRowItem(t, 2, 10),
	), 1180, 760)
	tt.Frame()
	if m.selectedSeq() != 1 {
		t.Fatalf("the first pending item is not selected: %d", m.selectedSeq())
	}
	m.edit.draft.Level = "14"
	m.edit.refresh()

	newer := reviewRowItem(t, 1, 11)
	newer.Revision = 2
	newer.Quality = record.QualityHumanConfirmed
	svc.mu.Lock()
	svc.loads["rec"].Checkpoint.Items[0] = newer
	svc.mu.Unlock()
	svc.saveFn = func(string, int64, int64, app.ItemRevisionEdit) (app.ItemRevisionResult, error) {
		return app.ItemRevisionResult{}, fmt.Errorf("stale: %w", record.ErrRevisionConflict)
	}

	tt.Frame()
	m.saveEdit(reviewSave, nil)
	drainAll(t, m, q)
	tt.Frame()
	if m.selectedSeq() != 2 {
		t.Fatalf("the reloaded conflict did not leave the filter: selected %d", m.selectedSeq())
	}
	if !strings.Contains(m.reviewNotice, "记录已被更新，已重新加载当前修订") {
		t.Fatalf("the settled conflict notice = %q", m.reviewNotice)
	}
	if !tt.HasText("记录已被更新") {
		t.Fatalf("the conflict notice is not visible: %q", tt.Texts())
	}
}

// TestRevisionConflictNoticeSurvivesEmptyFilter covers the same conflict
// when the reloaded row was the only pending item: the empty completion
// surface must not drop the explanation on a later frame.
func TestRevisionConflictNoticeSurvivesEmptyFilter(t *testing.T) {
	m, q, tt, svc := openReviewableResults(t, recovery(reviewRowItem(t, 1, 12)), 1180, 760)
	tt.Frame()
	m.edit.draft.Level = "14"
	m.edit.refresh()

	newer := reviewRowItem(t, 1, 11)
	newer.Revision = 2
	newer.Quality = record.QualityHumanConfirmed
	svc.mu.Lock()
	svc.loads["rec"].Checkpoint.Items[0] = newer
	svc.mu.Unlock()
	svc.saveFn = func(string, int64, int64, app.ItemRevisionEdit) (app.ItemRevisionResult, error) {
		return app.ItemRevisionResult{}, fmt.Errorf("stale: %w", record.ErrRevisionConflict)
	}

	tt.Frame()
	m.saveEdit(reviewSave, nil)
	drainAll(t, m, q)
	tt.Frame()
	tt.Frame()
	if len(m.visibleResultRows()) != 0 {
		t.Fatalf("the reloaded row is still in the pending filter: %v", m.visibleResultRows())
	}
	if !strings.Contains(m.reviewNotice, "记录已被更新，已重新加载当前修订") {
		t.Fatalf("the settled conflict notice = %q", m.reviewNotice)
	}
	if !tt.HasText("记录已被更新") {
		t.Fatalf("the conflict notice is not visible: %q", tt.Texts())
	}
}

// TestRevisionConflictReloadFailureNoticeVisible covers PR-19b round 2: when
// the conflict reload fails, the item action bar never renders, so the
// explanation must appear at page level.
func TestRevisionConflictReloadFailureNoticeVisible(t *testing.T) {
	m, q, tt, svc := openReviewableResults(t, recovery(reviewRowItem(t, 1, 12)), 1180, 760)
	tt.Frame()
	m.edit.draft.Level = "14"
	m.edit.refresh()
	svc.saveFn = func(string, int64, int64, app.ItemRevisionEdit) (app.ItemRevisionResult, error) {
		return app.ItemRevisionResult{}, fmt.Errorf("stale: %w", record.ErrRevisionConflict)
	}
	svc.mu.Lock()
	svc.loadErr = map[string]error{"rec": errors.New("disk gone")}
	svc.mu.Unlock()

	tt.Frame()
	m.saveEdit(reviewSave, nil)
	drainAll(t, m, q)
	tt.Frame()
	tt.Frame()
	if !strings.Contains(m.recErr, "disk gone") {
		t.Fatalf("the reload did not fail: %s", probeReviewState(m))
	}
	want := "记录已被更新，重新加载失败；旧输入未保存。"
	if m.reviewNotice != want {
		t.Fatalf("the failed-reload notice = %q, want %q", m.reviewNotice, want)
	}
	if !tt.HasText(want) {
		t.Fatalf("the failed-reload notice is not visible: %q", tt.Texts())
	}
	if !tt.HasText("记录读取失败：disk gone") {
		t.Fatalf("the record error is not visible: %q", tt.Texts())
	}
}

// TestRevisionConflictReloadSuccessClearsFailedNotice covers the second
// independent fault after a conflict: once a later load of the same record
// succeeds, the pinned explanation must stop claiming the reload failed.
func TestRevisionConflictReloadSuccessClearsFailedNotice(t *testing.T) {
	m, q, tt, svc := openReviewableResults(t, recovery(reviewRowItem(t, 1, 12)), 1180, 760)
	tt.Frame()
	m.edit.draft.Level = "14"
	m.edit.refresh()
	svc.saveFn = func(string, int64, int64, app.ItemRevisionEdit) (app.ItemRevisionResult, error) {
		return app.ItemRevisionResult{}, fmt.Errorf("stale: %w", record.ErrRevisionConflict)
	}
	svc.mu.Lock()
	svc.loadErr = map[string]error{"rec": errors.New("disk gone")}
	svc.mu.Unlock()

	tt.Frame()
	m.saveEdit(reviewSave, nil)
	drainAll(t, m, q)
	tt.Frame()
	tt.Frame()
	failed := "记录已被更新，重新加载失败；旧输入未保存。"
	if m.reviewNotice != failed {
		t.Fatalf("the failed-reload notice = %q, want %q", m.reviewNotice, failed)
	}

	// The next successful load of the open record (for example, a live
	// record catching up with saved generations) must replace the failure.
	svc.mu.Lock()
	delete(svc.loadErr, "rec")
	svc.mu.Unlock()
	m.reloadRecord("rec", false)
	drainAll(t, m, q)
	tt.Frame()
	want := "记录已被更新，已重新加载当前修订；旧输入未保存。"
	if m.reviewNotice != want {
		t.Fatalf("the settled notice = %q, want %q", m.reviewNotice, want)
	}
	if !tt.HasText(want) || tt.HasText(failed) {
		t.Fatalf("the stale failure is still on screen: %q", tt.Texts())
	}
}

func TestSaveWhileScanWritesRecordExplainsInChinese(t *testing.T) {
	m, q, tt, svc := openReviewRecordPage(t, reviewRowItem(t, 1, 12))
	svc.saveFn = func(string, int64, int64, app.ItemRevisionEdit) (app.ItemRevisionResult, error) {
		return app.ItemRevisionResult{}, fmt.Errorf("app: save item revision: %w", app.ErrRecordActive)
	}

	tt.Frame()
	m.edit.draft.Level = "13"
	tt.Frame()
	loadsBefore := len(svc.loadLog)
	if err := tt.Click("保存修改"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)

	if m.edit == nil || m.edit.draft.Level != "13" {
		t.Fatalf("the active-record refusal lost the input: %+v", m.edit)
	}
	if !tt.HasText("扫描仍在写入该记录，扫描结束后再保存。输入已保留。") || !tt.HasText("重试保存") {
		t.Fatalf("the active-record refusal is not explained: %q", tt.Texts())
	}
	if strings.Contains(m.reviewErr, "record is still being written") {
		t.Fatalf("the internal English error reached the UI: %q", m.reviewErr)
	}
	if len(svc.loadLog) != loadsBefore {
		t.Fatalf("the active-record refusal reloaded the record: %v", svc.loadLog)
	}
}

func TestUnsavedEditSwitchPrompt(t *testing.T) {
	m, q, tt, svc := openReviewRecordPage(t,
		reviewRowItem(t, 1, 12),
		reviewRowItem(t, 2, 10),
	)
	// Normal browsing with nothing to lose does not prompt.
	m.selected = 1
	m.applyTableSelection(0)
	if m.navPrompt != nil || m.selected != 1 {
		t.Fatalf("normal browsing prompted: selected %d prompt %v", m.selected, m.navPrompt)
	}
	tt.Frame()
	m.edit.draft.Level = "13"
	tt.Frame()

	loadsBefore := len(svc.loadLog)
	svc.loads["r2"] = recovery(reviewRowItem(t, 1, 12))
	m.openRecord("r2")
	settle(t, m, q, tt)
	showRecordItems(m)
	tt.Frame()
	if m.navPrompt == nil {
		t.Fatal("switching records with unsaved input did not prompt")
	}
	if len(svc.loadLog) != loadsBefore {
		t.Fatalf("the record switched before the editor settled: %v", svc.loadLog)
	}
	if !tt.HasText("第 2 件有未保存的修改") || !tt.HasText("留在这里") {
		t.Fatalf("the unsaved-edit prompt does not show: %q", tt.Texts())
	}

	if err := tt.Click("留在这里"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if m.navPrompt != nil || m.edit == nil || m.edit.draft.Level != "13" {
		t.Fatalf("staying lost the editor: prompt %v edit %+v", m.navPrompt, m.edit)
	}

	// Save-and-continue performs the save first, then the navigation.
	m.openRecord("r2")
	settle(t, m, q, tt)
	showRecordItems(m)
	tt.Frame()
	if err := tt.Click("保存并继续"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if svc.saveCount() != 1 {
		t.Fatalf("save-and-continue issued %d saves", svc.saveCount())
	}
	if m.navPrompt != nil || m.rec == nil || m.rec.ID != "r2" {
		t.Fatalf("save-and-continue did not settle: rec %+v prompt %v", m.rec, m.navPrompt)
	}
	if len(svc.loadLog) <= loadsBefore {
		t.Fatalf("save-and-continue did not perform the navigation: %v", svc.loadLog)
	}
}

func TestDiscardUnsavedEditContinues(t *testing.T) {
	m, q, tt, svc := openReviewRecordPage(t,
		reviewRowItem(t, 1, 12),
		reviewRowItem(t, 2, 10),
	)
	tt.Frame()
	m.edit.draft.Level = "13"
	tt.Frame()
	loadsBefore := len(svc.loadLog)
	svc.loads["r2"] = recovery(reviewRowItem(t, 1, 12))
	m.openRecord("r2")
	settle(t, m, q, tt)
	showRecordItems(m)
	tt.Frame()

	if err := tt.Click("放弃修改"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if m.edit != nil || m.navPrompt != nil {
		t.Fatalf("discarding did not settle the editor: edit %v prompt %v", m.edit, m.navPrompt)
	}
	if svc.saveCount() != 0 {
		t.Fatalf("discarding saved %d revisions", svc.saveCount())
	}
	if len(svc.loadLog) <= loadsBefore {
		t.Fatalf("discarding did not continue the navigation: %v", svc.loadLog)
	}
}

func TestLateSaveCannotCrossItems(t *testing.T) {
	m, q, tt, svc := openReviewRecordPage(t,
		reviewRowItem(t, 1, 12),
		reviewRowItem(t, 2, 10),
	)
	started := make(chan struct{})
	release := make(chan struct{})
	var once bool
	inner := svc.saveFn
	svc.saveFn = func(recordID string, sequence, expected int64, edit app.ItemRevisionEdit) (app.ItemRevisionResult, error) {
		if !once {
			once = true
			close(started)
			<-release
		}
		return inner(recordID, sequence, expected, edit)
	}

	tt.Frame()
	m.edit.draft.Level = "13"
	tt.Frame()
	if err := tt.Click("保存修改"); err != nil {
		t.Fatal(err)
	}
	<-started

	// The save is in flight: a selection change is refused.
	m.selected = 1
	m.applyTableSelection(0)
	if m.selected != 0 {
		t.Fatalf("a selection moved while saving: %d", m.selected)
	}
	if m.reviewErr == "" {
		t.Fatal("the refused selection is not explained")
	}

	// Even if the selection changed behind the guard, the response is
	// bound to the saved item's sequence, never the selected row.
	m.selected = 1
	close(release)
	settle(t, m, q, tt)

	if m.rec.Rows[0].LevelText() != "13 级" || m.rec.Rows[0].Revision != 1 {
		t.Fatalf("the saved item 1 = revision %d level %q", m.rec.Rows[0].Revision, m.rec.Rows[0].LevelText())
	}
	if m.rec.Rows[1].LevelText() != "10 级" || m.rec.Rows[1].Revision != 0 {
		t.Fatalf("the late response crossed into item 2 = revision %d level %q",
			m.rec.Rows[1].Revision, m.rec.Rows[1].LevelText())
	}
}

func TestSaveKeepsExplicitUnknownsPending(t *testing.T) {
	m, q, tt, svc := openReviewRecordPage(t, reviewRowItem(t, 1, 12))
	tt.Frame()

	// Explicit unknowns are a valid draft, but they can never be
	// confirmed until they are filled.
	m.edit.draft.Level = ""
	m.edit.draft.ManagementState = unreadOption
	tt.Frame()
	if !m.edit.canSave() || m.edit.canConfirm() {
		t.Fatalf("explicit unknowns: canSave=%v canConfirm=%v", m.edit.canSave(), m.edit.canConfirm())
	}
	clickRevealedDetailControl(t, tt, reviewPageHeight, "保存修改")
	settle(t, m, q, tt)

	if svc.saveCount() != 1 || svc.saves[0].edit.Confirmed {
		t.Fatalf("unknowns save = %+v, want one unconfirmed save", svc.saves)
	}
	row := m.rec.Rows[0]
	if row.Quality != record.QualityNeedsReview || m.pendingItemsCount() != 1 {
		t.Fatalf("unknowns row = quality %q pending %d", row.Quality, m.pendingItemsCount())
	}
	if row.LevelText() != "未读取" || row.ManagementStateText() != "未读取" {
		t.Fatalf("unknowns were not preserved: level %q management %q",
			row.LevelText(), row.ManagementStateText())
	}
	if !tt.HasText("需补全") || !tt.HasText("暂时不能确认") {
		t.Fatalf("the incomplete revision does not show its gaps: %q", tt.Texts())
	}
}

func TestNewSessionKeepsDirtyEditor(t *testing.T) {
	m, q, tt, svc := openReviewRecordPage(t, reviewRowItem(t, 1, 12))
	svc.loads["s2"] = recovery(reviewRowItem(t, 1, 12))
	tt.Frame()
	m.edit.draft.Level = "13"
	tt.Frame()

	m.Apply(snap("s2", 1, true, scan.PhaseCapturing,
		scan.Counts{Captured: 1, Produced: 1}, 1, true, ""))
	settle(t, m, q, tt)

	if m.rec == nil || m.rec.ID != "r1" {
		t.Fatalf("a new session replaced the dirty record: %+v", m.rec)
	}
	if m.rec.Live {
		t.Fatal("the previous session's record still calls itself live")
	}
	if m.edit == nil || m.edit.draft.Level != "13" {
		t.Fatalf("the new session discarded the editor: %+v", m.edit)
	}
	if !tt.HasText("新的扫描已开始") || !tt.HasText("查看本次扫描") {
		t.Fatalf("the deferred live view is not explained: %q", tt.Texts())
	}

	m.discardEdit()
	settle(t, m, q, tt)
	if m.edit == nil || m.edit.draft.Level != "12" || m.navPrompt != nil {
		t.Fatalf("discarding did not restore the saved draft: edit=%+v nav=%+v", m.edit, m.navPrompt)
	}
	if err := tt.Click("查看本次扫描"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if m.rec == nil || m.rec.ID != "s2" || !m.rec.Live {
		t.Fatalf("the live session is not reachable after settling: rec=%+v applied=%q edit=%+v nav=%+v exit=%v",
			m.rec, m.appliedSession, m.edit, m.navPrompt, m.exitState)
	}
}

func TestExitRefusesWithUnsavedEdit(t *testing.T) {
	m, q, tt, svc := openReviewRecordPage(t, reviewRowItem(t, 1, 12))
	tt.Frame()
	m.edit.draft.Level = "13"
	tt.Frame()

	if m.AllowClose() {
		t.Fatal("the close was allowed with unsaved input")
	}
	settle(t, m, q, tt)
	if svc.quits != 0 {
		t.Fatalf("the quit started before the editor settled: %d", svc.quits)
	}
	if !tt.HasText("第 1 件有未保存的修改") || !tt.HasText("保存并继续") {
		t.Fatalf("the exit prompt does not show: %q", tt.Texts())
	}

	if err := tt.Click("留在这里"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if m.edit == nil {
		t.Fatal("staying closed the editor")
	}

	// The next close prompts again; saving and exiting runs the save
	// first and the wrap-up only after it succeeded.
	if m.AllowClose() {
		t.Fatal("the close was allowed with the editor still dirty")
	}
	settle(t, m, q, tt)
	if svc.quits != 0 {
		t.Fatalf("the wrap-up started before the save: %d", svc.quits)
	}
	if err := tt.Click("保存并继续"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if svc.saveCount() != 1 {
		t.Fatalf("save-and-exit issued %d saves", svc.saveCount())
	}
	if svc.quits != 1 || m.exitState != exitDone {
		t.Fatalf("save-and-exit did not finish: quits %d state %d", svc.quits, m.exitState)
	}
}

// TestConfirmNextStaysWithinVisibleFilter checks that confirmation moves
// only inside the current segment. Rows hidden by the pending or
// unexportable filter are never selected behind the visible list.
func TestConfirmNextStaysWithinVisibleFilter(t *testing.T) {
	t.Run("pending skips hidden rows and completes", func(t *testing.T) {
		m, q, tt, svc := openReviewRecordPage(t,
			reviewRowItem(t, 1, 12),
			confirmedReviewItem(t, 2, 10),
			reviewRowItem(t, 3, 14),
		)
		m.filter = resultFilterPending
		m.selected = 0
		tt.Frame()
		if visible := m.visibleResultRows(); len(visible) != 2 || visible[0] != 0 || visible[1] != 2 {
			t.Fatalf("the pending segment shows rows %v, want [0 2]", visible)
		}

		m.confirmCurrent()
		settle(t, m, q, tt)
		if svc.saveCount() != 1 {
			t.Fatalf("confirmation issued %d saves", svc.saveCount())
		}
		if m.selected != 2 {
			t.Fatalf("confirmation selected row %d, want the visible seq 3 (index 2)", m.selected)
		}
		visible := m.visibleResultRows()
		if len(visible) != 2 || visible[0] != 0 || visible[1] != 2 || resultDisplayIndex(visible, m.selected) < 0 {
			t.Fatalf("the confirmed row did not stay in the pending rows: selected %d visible %v",
				m.selected, visible)
		}

		m.confirmCurrent()
		settle(t, m, q, tt)
		if m.pendingItemsCount() != 0 || !m.completionVisible() {
			t.Fatalf("the last confirmation did not complete: pending %d completion %v",
				m.pendingItemsCount(), m.completionVisible())
		}
	})

	t.Run("all wraps to the first pending row", func(t *testing.T) {
		m, q, tt, _ := openReviewRecordPage(t,
			reviewRowItem(t, 1, 12),
			reviewRowItem(t, 2, 10),
			reviewRowItem(t, 3, 14),
		)
		m.filter = resultFilterAll
		m.selected = 2
		tt.Frame()
		m.confirmCurrent()
		settle(t, m, q, tt)
		if m.selected != 0 {
			t.Fatalf("the last pending row did not wrap: selected %d", m.selected)
		}
		if !tt.HasText("已确认并定位下一项（序号 1）。") {
			t.Fatalf("the wrapped next-item notice is missing: %q", tt.Texts())
		}
		if visible := m.visibleResultRows(); resultDisplayIndex(visible, m.selected) < 0 {
			t.Fatalf("the wrapped selection is outside the visible rows: selected %d visible %v",
				m.selected, visible)
		}
	})

}
