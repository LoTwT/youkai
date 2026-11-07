package ui

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/game/zzz"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/LoTwT/youkai/internal/scan"
	"github.com/egoist/mygo/ui"
)

// reviewDisc is a complete S-rank drive disc for the review fixtures.
func reviewDisc(set, slot, level int) zzz.DiscRecord {
	return zzz.DiscRecord{
		SetID: setIDPtr(set), Slot: slot, Level: intPtr(level),
		MainStat: zzz.Stat{Attribute: zzz.AttrCritRate, Value: "19.2", Unit: zzz.UnitPercent},
		Substats: []zzz.Substat{{
			Stat:             zzz.Stat{Attribute: zzz.AttrCritDamage, Value: "9.6", Unit: zzz.UnitPercent},
			EnhancementRolls: intPtr(3),
		}},
	}
}

func setIDPtr(v int) *int { return &v }
func intPtr(v int) *int   { return &v }

func rgba(c ui.Color) color.RGBA { return color.RGBA{R: c.R, G: c.G, B: c.B, A: c.A} }

// pendingReviewRecord builds a record with one item per class: pending
// review, pending backfill (failed and unprocessed), warning and passed.
func pendingReviewRecord(t *testing.T) *record.Recovery {
	t.Helper()
	pending := reviewRowItem(t, 1, 12)
	failed := item(2, record.StatusFailed, "", "识别超时", "失败一")
	unprocessed := item(3, record.StatusUnprocessed, record.ReasonCancelled, "", "未处理")
	warning := withStructured(t, reviewRowItem(t, 4, 15), func(s *zzz.StructuredDisc) {
		s.Issues = []zzz.Issue{{
			Field: "equipped", Code: zzz.CodeEquippedStateUnavailable,
			Severity: zzz.SeverityWarning, Message: "装备者未识别",
		}}
	})
	warning.Quality = record.QualityAutoAccepted
	passed := reviewRowItem(t, 5, 15)
	passed.Quality = record.QualityAutoAccepted
	return recovery(pending, failed, unprocessed, warning, passed)
}

// manyPendingRecord builds n pending-review items.
func manyPendingRecord(t *testing.T, n int) *record.Recovery {
	t.Helper()
	items := make([]record.ItemRecord, 0, n)
	for seq := 1; seq <= n; seq++ {
		items = append(items, reviewRowItem(t, int64(seq), 15))
	}
	return recovery(items...)
}

// responsiveResultRecord builds a record with two pending items and one
// failed and one unprocessed item, so every segment has a distinct
// population.
func responsiveResultRecord(t *testing.T) *record.Recovery {
	t.Helper()
	first := zzz.DiscRecord{
		SetID: intPtr(31100), Slot: 1, Level: intPtr(15),
		MainStat: zzz.Stat{Attribute: zzz.AttrHealth, Value: "2200", Unit: zzz.UnitFlat},
		Substats: []zzz.Substat{{
			Stat:             zzz.Stat{Attribute: zzz.AttrCritDamage, Value: "19.2", Unit: zzz.UnitPercent},
			EnhancementRolls: intPtr(3),
		}},
	}
	second := first
	second.SetID = intPtr(31200)
	second.Slot = 2
	return recovery(
		structuredItem(1, first, "待一"),
		structuredItem(2, second, "待二"),
		item(3, record.StatusFailed, "", "识别超时", "失败"),
		item(4, record.StatusUnprocessed, record.ReasonCancelled, "", "未处理"),
	)
}

// openResponsiveResults opens one saved record on the review view at the
// requested window size.
func openResponsiveResults(t *testing.T, rec *record.Recovery, width, height int) (*Model, *queueUpdater, *ui.Tester) {
	t.Helper()
	m, q, tt, _ := openResponsiveResultsService(t, rec, width, height, false)
	return m, q, tt
}

// openReviewableResults is openResponsiveResults with the revision save
// the review actions need.
func openReviewableResults(t *testing.T, rec *record.Recovery, width, height int) (*Model, *queueUpdater, *ui.Tester, *fakeService) {
	t.Helper()
	return openResponsiveResultsService(t, rec, width, height, true)
}

func openResponsiveResultsService(t *testing.T, rec *record.Recovery, width, height int, save bool) (*Model, *queueUpdater, *ui.Tester, *fakeService) {
	t.Helper()
	svc := &fakeService{
		list: []record.RecordInfo{{
			ID: "rec", LatestRevision: 1,
			SavedAt: time.Date(2026, 10, 21, 14, 2, 0, 0, time.UTC),
		}},
		loads: map[string]*record.Recovery{"rec": rec},
	}
	if save {
		installReviewSave(svc)
	}
	m, q := newTestModel(t, Config{Service: svc})
	m.SetContentSize(width, height)
	tt := ui.NewTester(m.View, width, height)
	settle(t, m, q, tt)
	m.histChoice = historyLabel(t, m, "rec")
	m.OpenHistory()
	settle(t, m, q, tt)
	showRecordItems(m)
	tt.Frame()
	showRecordItems(m)
	tt.Frame()
	return m, q, tt, svc
}

// roledItem builds one produced item with real observed roles and the
// matching evidence ids, so the screenshot list can be checked against
// R-07a instead of the helper's synthetic "line0" roles.
func roledItem(t *testing.T, seq int64, roles, contexts []string, texts []string) record.ItemRecord {
	t.Helper()
	item := record.ItemRecord{
		Sequence: seq, Status: record.StatusProduced,
		Quality: record.QualityNeedsReview,
	}
	var observedLines, resultLines []map[string]any
	for i, role := range roles {
		observedLines = append(observedLines, map[string]any{"role": role, "source": fmt.Sprintf("d%d_l%d.png", seq, i)})
		text := role
		if i < len(texts) {
			text = texts[i]
		}
		resultLines = append(resultLines, map[string]any{"role": role, "text": text, "score": 0.9})
		item.EvidenceIDs = append(item.EvidenceIDs, fmt.Sprintf("%06d-line%d", seq, i))
	}
	observed := map[string]any{"lines": observedLines}
	if len(contexts) > 0 {
		var contextRows []map[string]any
		for i, role := range contexts {
			contextRows = append(contextRows, map[string]any{"role": role, "source": fmt.Sprintf("d%d_c%d.png", seq, i)})
			item.EvidenceIDs = append(item.EvidenceIDs, fmt.Sprintf("%06d-context%d", seq, i))
		}
		observed["context"] = contextRows
	}
	rawObserved, err := json.Marshal(observed)
	if err != nil {
		t.Fatal(err)
	}
	rawResult, err := json.Marshal(map[string]any{"lines": resultLines})
	if err != nil {
		t.Fatal(err)
	}
	item.Observed = rawObserved
	item.Data = rawResult
	return item
}

// TestReviewLayoutProfiles checks the three width profiles: standard has
// the 240-DIP list, compact folds it into the strip, and every profile
// keeps screenshots (≥ 360) beside the 400-DIP form.
func TestReviewLayoutProfiles(t *testing.T) {
	cases := []struct {
		name          string
		width, height int
		standard      bool
	}{
		{"standard-1180", 1180, 760, true},
		{"standard-1120", 1120, 700, true},
		{"compact-1119", 1119, 700, false},
		{"compact-1000", 1000, 700, false},
		{"compact-short-960", 960, 640, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, _, tt := openResponsiveResults(t, manyPendingRecord(t, 4), tc.width, tc.height)
			layout := m.reviewLayout
			if layout.Standard != tc.standard {
				t.Fatalf("standard = %v, want %v", layout.Standard, tc.standard)
			}
			if layout.Crops < reviewMinCropsWidth-0.5 {
				t.Fatalf("screenshot width = %v, want ≥ %v", layout.Crops, reviewMinCropsWidth)
			}
			if layout.Form < reviewFormWidth-0.5 || layout.Form > reviewFormWidth+0.5 {
				t.Fatalf("form width = %v, want %v", layout.Form, reviewFormWidth)
			}
			if tc.standard {
				if layout.List < reviewListWidth-0.5 || layout.List > reviewListWidth+0.5 {
					t.Fatalf("list width = %v, want %v", layout.List, reviewListWidth)
				}
				if !tt.HasText("待处理") {
					t.Fatal("the standard list filter is missing")
				}
			} else {
				if layout.List != 0 {
					t.Fatalf("compact profile still shows a list column: %v", layout.List)
				}
				if !tt.HasText("全部件 ▾") {
					t.Fatalf("the compact strip is missing: %q", tt.Texts())
				}
			}
			_ = tt.Image()
		})
	}
}

// TestReviewCompactStripKeepsEvidenceBesideForm checks the 960 × 640
// acceptance evidence: screenshots and the form are both present, and the
// item list is in the strip.
func TestReviewCompactStripAtMinimumWindow(t *testing.T) {
	m, q, tt := openResponsiveResults(t, manyPendingRecord(t, 3), 960, 640)
	if m.reviewLayout.Crops < reviewMinCropsWidth || m.reviewLayout.Form < reviewFormWidth {
		t.Fatalf("960×640 layout = %+v", m.reviewLayout)
	}
	if !tt.HasText("截图（逐行）") || !tt.HasText("确认并下一项") {
		t.Fatalf("the screenshot or form is missing at 960×640: %q", tt.Texts())
	}
	if !tt.HasText("待处理 1 / 3") {
		t.Fatalf("the compact position is missing: %q", tt.Texts())
	}
	tt.SetSize(1000, 700)
	m.SetContentSize(1000, 700)
	settle(t, m, q, tt)
	if m.reviewLayout.Crops < reviewMinCropsWidth || m.reviewLayout.Form < reviewFormWidth {
		t.Fatalf("1000-wide layout = %+v", m.reviewLayout)
	}
}

// TestReviewFilterSegments checks the three segments and the category
// labels, including the two backfill reasons.
func TestReviewFilterSegments(t *testing.T) {
	m, _, tt := openResponsiveResults(t, pendingReviewRecord(t), 1180, 760)
	if m.filter != resultFilterPending {
		t.Fatalf("the view starts on %v, want pending", m.filter)
	}
	if got := m.resultFilterCount(resultFilterPending); got != 3 {
		t.Fatalf("pending count = %d, want 3 (review + failed + unprocessed)", got)
	}
	if got := m.resultFilterCount(resultFilterWarning); got != 1 {
		t.Fatalf("warning count = %d, want 1", got)
	}
	if got := m.resultFilterCount(resultFilterAll); got != 5 {
		t.Fatalf("all count = %d, want 5", got)
	}
	if !tt.HasText("待补录 · 识别失败") || !tt.HasText("待补录 · 未处理") || !tt.HasText("待校对") {
		t.Fatalf("the pending labels are missing: %q", tt.Texts())
	}
	m.setResultFilter(resultFilterWarning)
	tt.Frame()
	if !tt.HasText("有警告") {
		t.Fatalf("the warning label is missing: %q", tt.Texts())
	}
	m.setResultFilter(resultFilterAll)
	tt.Frame()
	if !tt.HasText("已通过") {
		t.Fatalf("the passed label is missing: %q", tt.Texts())
	}
}

// TestReviewConfirmKeepsRowAndMovesFocus checks that a confirmed row stays
// in the pending list for the visit, the selection moves on, and the next
// item's first problem field takes the focus.
func TestReviewConfirmKeepsRowAndMovesFocus(t *testing.T) {
	first := reviewRowItem(t, 1, 15)
	second := withStructured(t, reviewRowItem(t, 2, 15), func(s *zzz.StructuredDisc) {
		s.Data.ManagementState = nil
		s.Validation = zzz.Validation{Status: zzz.ValidationUnavailable, Issues: []zzz.Issue{}}
	})
	m, q, tt, _ := openReviewableResults(t, recovery(first, second), 1180, 760)
	if m.selected != 0 {
		t.Fatalf("initial selection = %d", m.selected)
	}
	m.saveEdit(reviewConfirmNext, nil)
	drainAll(t, m, q)
	tt.Frame()
	tt.Frame()
	if m.selected != 1 {
		t.Fatalf("selection after confirm = %d, want 1", m.selected)
	}
	if !m.recentlyConfirmed[first.Sequence] {
		t.Fatal("the confirmed item left the pending list")
	}
	visible := m.visibleResultRows()
	if resultDisplayIndex(visible, 0) < 0 {
		t.Fatalf("the confirmed row is hidden: visible %v", visible)
	}
	if !tt.HasText("已确认") {
		t.Fatalf("the confirmed row is not marked: %q", tt.Texts())
	}
	if !tt.Focused("管理状态") {
		t.Fatalf("focus did not move to the next item's first problem field; texts %q", tt.Texts())
	}
	m.setResultFilter(resultFilterAll)
	if m.recentlyConfirmed != nil {
		t.Fatal("switching segments did not clear the just-confirmed marker")
	}
}

// TestReviewSaveFailureKeepsInput checks that a failed save stays on the
// item with the draft intact and offers a retry.
func TestReviewSaveFailureKeepsInput(t *testing.T) {
	rec := recovery(reviewRowItem(t, 1, 15), reviewRowItem(t, 2, 15))
	svc := &fakeService{
		list:  []record.RecordInfo{{ID: "rec", LatestRevision: 1, SavedAt: time.Now()}},
		loads: map[string]*record.Recovery{"rec": rec},
	}
	installReviewSave(svc)
	svc.saveFn = func(string, int64, int64, app.ItemRevisionEdit) (app.ItemRevisionResult, error) {
		return app.ItemRevisionResult{}, fmt.Errorf("磁盘空间不足")
	}
	m, q := newTestModel(t, Config{Service: svc})
	m.SetContentSize(1180, 760)
	tt := ui.NewTester(m.View, 1180, 760)
	settle(t, m, q, tt)
	m.histChoice = historyLabel(t, m, "rec")
	m.OpenHistory()
	settle(t, m, q, tt)
	showRecordItems(m)
	tt.Frame()
	showRecordItems(m)
	tt.Frame()
	seq := m.selectedSeq()
	m.edit.draft.Level = "11"
	m.saveEdit(reviewSave, nil)
	drainAll(t, m, q)
	tt.Frame()
	if m.selectedSeq() != seq {
		t.Fatalf("selection moved from %d to %d after a failed save", seq, m.selectedSeq())
	}
	if m.edit == nil || m.edit.draft.Level != "11" {
		t.Fatal("the failed save dropped the input")
	}
	if !tt.HasText("输入已保留，可重试。") {
		t.Fatalf("the failure band is missing: %q", tt.Texts())
	}
	if !tt.HasText("保存失败") {
		t.Fatalf("the title bar does not show the save failure: %q", tt.Texts())
	}
	if got := strings.Count(strings.Join(tt.Texts(), "\n"), "输入已保留"); got != 1 {
		t.Fatalf("the failure band repeats the preserved-input note %d times: %q", got, tt.Texts())
	}
}

// TestReviewFieldClickHighlightsCrop checks that selecting a form field
// highlights and scrolls to its row crop, and that a record without line
// roles falls back to capture order without a highlight.
func TestReviewFieldClickHighlightsCrop(t *testing.T) {
	roles := []string{"name", "level", "main", "sub1", "sub2", "sub3", "sub4"}
	items := []record.ItemRecord{
		roledItem(t, 1, roles, []string{"detailAvatar", "qualityBadge"}, []string{"河豚电音", "15", "19.2%", "9.6%", "3%", "19", "112"}),
	}
	rec := recovery(items...)
	m, _, tt := openResponsiveResults(t, rec, 1180, 760)
	// The roles are real but the payload has no structured interpretation,
	// so the item is still a pending backfill.
	if !tt.HasText("套装") || !tt.HasText("副词条 4") || !tt.HasText("装备者头像") || !tt.HasText("稀有度") {
		t.Fatalf("the Chinese role labels are missing: %q", tt.Texts())
	}
	if tt.HasText("detailAvatar") || tt.HasText("qualityBadge") {
		t.Fatalf("an English role id leaked into the UI: %q", tt.Texts())
	}
	before := countColor(tt.Image(), rgba(tokensFor(false).AccentSoft))
	m.selectReviewField(FormFieldLevel)
	tt.Frame()
	tt.Frame()
	after := countColor(tt.Image(), rgba(tokensFor(false).AccentSoft))
	if after <= before {
		t.Fatalf("the level field did not highlight its crop: accent pixels %d → %d", before, after)
	}
	if m.reviewField != FormFieldLevel {
		t.Fatalf("selected field = %q", m.reviewField)
	}

	// Remove the roles: capture order, no highlight, and a note.
	noRoles := roledItem(t, 1, nil, nil, nil)
	noRoles.Observed = []byte(`{"lines":[{},{"},{"}]}`)
	noRoles.Data = []byte(`{"lines":[{},{},{}]}`)
	noRoles.EvidenceIDs = []string{"000001-line0", "000001-line1", "000001-line2"}
	m2, _, tt2 := openResponsiveResults(t, recovery(noRoles), 1180, 760)
	if !tt2.HasText("这件缺少行角色，截图按采集顺序列出。") {
		t.Fatalf("the fallback note is missing: %q", tt2.Texts())
	}
	m2.selectReviewField(FormFieldLevel)
	tt2.Frame()
	if m2.scrollToCrop {
		t.Fatal("the fallback still tried to highlight a crop")
	}
}

// TestReviewMissingImagePerRow checks the per-row missing-image note; a
// missing crop never borrows the previous item's image.
func TestReviewMissingImagePerRow(t *testing.T) {
	first := roledItem(t, 1, []string{"name", "level"}, nil, []string{"套装", "15"})
	second := roledItem(t, 2, []string{"name", "level"}, nil, []string{"套装", "15"})
	second.EvidenceIDs = []string{"000002-line0"} // the level crop is missing
	m, _, tt := openResponsiveResults(t, recovery(first, second), 1180, 760)
	m.requestNavigation(navigationRequest{kind: navSelect, seq: 2})
	tt.Frame()
	tt.Frame()
	if !tt.HasText("这一行缺少截图。") {
		t.Fatalf("the missing-image note is missing: %q", tt.Texts())
	}
	if !tt.HasText("套装") || !tt.HasText("等级") {
		t.Fatalf("the line labels are missing: %q", tt.Texts())
	}
}

// TestReviewBackfillKeepsOriginalOutcome drives the failed item's form to a
// confirmed completion and checks it joins the export.
func TestReviewBackfillKeepsOriginalOutcome(t *testing.T) {
	failed := item(1, record.StatusFailed, "", "识别超时", "失败一")
	m, q, tt, svc := openReviewableResults(t, recovery(failed), 1180, 760)
	saves := 0
	inner := svc.saveFn
	svc.saveFn = func(recordID string, sequence, expectedRevision int64, edit app.ItemRevisionEdit) (app.ItemRevisionResult, error) {
		saves++
		return inner(recordID, sequence, expectedRevision, edit)
	}
	if m.edit == nil || !m.edit.backfill {
		t.Fatal("the failed item did not open a backfill editor")
	}
	if !tt.HasText("补录：这件识别失败，补全必填字段后才能导出。") {
		t.Fatalf("the backfill notice is missing: %q", tt.Texts())
	}
	draft := &m.edit.draft
	draft.Rarity = zzz.RarityS
	draft.SetID = describeSet(31100)
	draft.Slot = "4"
	draft.Level = "15"
	draft.MainAttribute = describeAttribute(string(zzz.AttrCritRate))
	draft.MainValue = "19.2"
	draft.MainUnit = describeUnit(string(zzz.UnitPercent))
	draft.SubstatsKnown = true
	draft.Substats = []substatDraft{{
		Attribute: describeAttribute(string(zzz.AttrCritDamage)),
		Value:     "9.6",
		Unit:      describeUnit(string(zzz.UnitPercent)),
		Rolls:     "3",
	}}
	draft.ManagementState = describeManagement(zzz.ManagementNone)
	draft.EquippedState = describeEquipped(string(zzz.EquippedStateUnequipped))
	m.edit.refresh()
	if !m.edit.canConfirm() {
		t.Fatalf("the filled backfill cannot be confirmed: %v %v", m.edit.fieldErrors, m.edit.check)
	}
	m.saveEdit(reviewConfirmNext, nil)
	drainAll(t, m, q)
	tt.Frame()
	tt.Frame()
	if saves == 0 {
		t.Fatal("the backfill was not saved")
	}
	row := &m.rec.Rows[0]
	if row.Status != record.StatusFailed || row.Failure != "识别超时" ||
		row.Quality != record.QualityHumanConfirmed {
		t.Fatalf("the backfilled row = status %q failure %q quality %q",
			row.Status, row.Failure, row.Quality)
	}
	if !row.Backfilled || row.ItemClass != app.ItemClassPassed {
		t.Fatalf("the backfilled row = backfilled %v class %q, want true/passed",
			row.Backfilled, row.ItemClass)
	}
	if m.pendingItemsCount() != 0 {
		t.Fatalf("pending after completion = %d", m.pendingItemsCount())
	}
	if m.exportData == nil || m.recordClassCounts() != m.exportData.Summary.Counts {
		t.Fatalf("class counts %+v do not match export summary %+v",
			m.recordClassCounts(), m.exportData)
	}
}

// TestReviewShortcutsZoomAndWalk checks Ctrl+J / Ctrl+Shift+J and the
// screenshot zoom keys.
func TestReviewShortcutsZoomAndWalk(t *testing.T) {
	m, q, tt := openResponsiveResults(t, manyPendingRecord(t, 3), 1180, 760)
	tt.Key(ui.Cmd, ui.KeyJ)
	drainAll(t, m, q)
	if m.selected != 1 {
		t.Fatalf("Ctrl+J selected %d, want 1", m.selected)
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyJ)
	drainAll(t, m, q)
	if m.selected != 0 {
		t.Fatalf("Ctrl+Shift+J selected %d, want 0", m.selected)
	}
	tt.Key(ui.Cmd, ui.KeyEqual)
	tt.Frame()
	if m.cropZoom <= 0 {
		t.Fatal("Ctrl+= did not zoom the screenshots")
	}
	zoomed := m.cropZoom
	tt.Key(ui.Cmd, ui.KeyMinus)
	tt.Frame()
	if m.cropZoom >= zoomed && m.cropZoom != 0 {
		t.Fatalf("Ctrl+- did not shrink: %v → %v", zoomed, m.cropZoom)
	}
	tt.Key(ui.Cmd, ui.KeyEqual)
	tt.Frame()
	tt.Key(ui.Cmd, ui.Key0)
	tt.Frame()
	if m.cropZoom != 0 {
		t.Fatalf("Ctrl+0 left zoom %v", m.cropZoom)
	}
}

// TestReviewUnsavedDialog checks the unsaved-changes dialog's title,
// buttons and default focus.
func TestReviewUnsavedDialog(t *testing.T) {
	m, q, tt := openResponsiveResults(t, manyPendingRecord(t, 2), 1180, 760)
	m.edit.draft.Level = "11"
	tt.Frame()
	m.requestNavigation(navigationRequest{kind: navSelect, seq: 2})
	tt.Frame()
	if m.navPrompt == nil {
		t.Fatal("switching items with a dirty draft did not open the prompt")
	}
	if !tt.HasText("第 1 件有未保存的修改") {
		t.Fatalf("the dialog title is missing: %q", tt.Texts())
	}
	for _, label := range []string{"保存并继续", "放弃修改", "留在这里"} {
		if !tt.HasText(label) {
			t.Fatalf("the dialog button %q is missing: %q", label, tt.Texts())
		}
	}
	if !tt.Focused("留在这里") {
		t.Fatal("the dialog did not focus 留在这里")
	}
	tt.Key(0, ui.KeyEscape)
	settle(t, m, q, tt)
	if m.navPrompt != nil {
		t.Fatal("Escape did not close the dialog")
	}
	if m.selected != 0 || m.edit == nil || m.edit.draft.Level != "11" {
		t.Fatal("Escape did not keep the item and its input")
	}
}

// TestReviewEvidenceCacheStaysBounded cycles through many items and checks
// the decoded-image cache never grows past its bound.
func TestReviewEvidenceCacheStaysBounded(t *testing.T) {
	const total = 120
	items := make([]record.ItemRecord, 0, total)
	images := make(map[string]*ui.Bitmap, total)
	for seq := 1; seq <= total; seq++ {
		it := roledItem(t, int64(seq), []string{"name"}, nil, []string{"套装"})
		items = append(items, it)
		images[fmt.Sprintf("rec/%06d-line0", seq)] = solidBitmap(color.RGBA{R: byte(seq), G: 20, B: 40, A: 255})
	}
	rec := recovery(items...)
	svc := &fakeService{
		list:  []record.RecordInfo{{ID: "rec", LatestRevision: 1, SavedAt: time.Now()}},
		loads: map[string]*record.Recovery{"rec": rec},
	}
	evidence := &fakeEvidence{images: images}
	m, q := newTestModel(t, Config{Service: svc, Evidence: evidence, EvidenceCacheLimit: 8})
	m.SetContentSize(1180, 760)
	tt := ui.NewTester(m.View, 1180, 760)
	settle(t, m, q, tt)
	m.histChoice = historyLabel(t, m, "rec")
	m.OpenHistory()
	settle(t, m, q, tt)
	showRecordItems(m)
	tt.Frame()
	m.setResultFilter(resultFilterAll)
	for seq := int64(1); seq <= total; seq++ {
		m.requestNavigation(navigationRequest{kind: navSelect, seq: seq})
		tt.Frame()
		tt.Frame()
		if len(m.cache.entries) > 8+1 {
			t.Fatalf("evidence cache holds %d entries after seq %d", len(m.cache.entries), seq)
		}
	}
}

// TestReviewF6CyclesRegions checks F6 between the item list, the
// screenshots and the form.
func TestReviewF6CyclesRegions(t *testing.T) {
	second := withStructured(t, reviewRowItem(t, 2, 15), func(s *zzz.StructuredDisc) {
		s.Data.ManagementState = nil
		s.Validation = zzz.Validation{Status: zzz.ValidationUnavailable, Issues: []zzz.Issue{}}
	})
	m, _, tt := openResponsiveResults(t, recovery(reviewRowItem(t, 1, 15), second), 1180, 760)
	// The view starts in the form; F6 wraps to the list.
	tt.Key(0, ui.KeyF6)
	tt.Frame()
	if m.reviewRegion != reviewRegionList {
		t.Fatalf("F6 left the review in region %v", m.reviewRegion)
	}
	before := m.tableSelected
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	if m.tableSelected != before+1 {
		t.Fatalf("F6 did not focus the item list: row %d → %d (region %v focused %v request %v field %q)", before, m.tableSelected, m.reviewRegion, m.reviewLayout.ListFocused, m.focusListRequested, m.reviewField)
	}
	if m.reviewField != FormFieldManagementState || m.reviewRegion != reviewRegionList {
		t.Fatalf("choosing a row should point at its first problem field while the list keeps focus: region %v field %q", m.reviewRegion, m.reviewField)
	}
	// The second item is now chosen; its first problem field focuses
	// through the form step.
	tt.Key(0, ui.KeyF6)
	tt.Frame()
	tt.Key(0, ui.KeyF6)
	tt.Frame()
	tt.Frame()
	if !tt.Focused("管理状态") {
		t.Fatalf("F6 did not land on the first problem field: %q", tt.Texts())
	}
}

// TestReviewFieldScrollsToCrop checks that a field whose crop lies below
// the screenshot viewport scrolls it into view.
func TestReviewFieldScrollsToCrop(t *testing.T) {
	roles := []string{"name", "level", "main", "sub1", "sub2", "sub3", "sub4"}
	item := roledItem(t, 1, roles, nil, []string{"套装", "等级", "主词条", "一", "二", "三", "四"})
	images := map[string]*ui.Bitmap{}
	for i := range roles {
		images[fmt.Sprintf("rec/%06d-line%d", 1, i)] = tallBitmap(i)
	}
	rec := recovery(item)
	svc := &fakeService{
		list:  []record.RecordInfo{{ID: "rec", LatestRevision: 1, SavedAt: time.Now()}},
		loads: map[string]*record.Recovery{"rec": rec},
	}
	m, q := newTestModel(t, Config{Service: svc, Evidence: &fakeEvidence{images: images}})
	m.SetContentSize(1180, 760)
	tt := ui.NewTester(m.View, 1180, 760)
	settle(t, m, q, tt)
	m.histChoice = historyLabel(t, m, "rec")
	m.OpenHistory()
	settle(t, m, q, tt)
	showRecordItems(m)
	settle(t, m, q, tt)

	before, ok := tt.Find("副词条 4")
	if !ok {
		t.Fatalf("the last crop is missing: %q", tt.Texts())
	}
	if before.Y+before.H < 760-80 {
		t.Fatalf("the crop list did not overflow: last crop ends at %v", before.Y+before.H)
	}
	m.selectReviewField(FormFieldSubstat4)
	tt.Frame()
	tt.Frame()
	after, ok := tt.Find("副词条 4")
	if !ok {
		t.Fatalf("the last crop disappeared after scrolling: %q", tt.Texts())
	}
	if after.Y+after.H > 760-60 {
		t.Fatalf("the selected crop is still below the viewport: %+v", after)
	}
}

// tallBitmap is a 100×400 crop, so seven crops overflow the screenshot
// pane in a 760-DIP window.
func tallBitmap(seed int) *ui.Bitmap {
	img := image.NewRGBA(image.Rect(0, 0, 100, 400))
	for y := 0; y < 400; y++ {
		for x := 0; x < 100; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(30 + seed*20), G: 80, B: 160, A: 255})
		}
	}
	return ui.NewBitmap(img)
}

// TestReviewConfirmNextReachesBackfill checks that the keyboard and the
// confirm button use the same pending queue: a backfill item is a valid
// next item even though it is not a "pending review" quality row.
func TestReviewConfirmNextReachesBackfill(t *testing.T) {
	first := reviewRowItem(t, 1, 15)
	failed := item(2, record.StatusFailed, "", "识别超时", "失败")
	m, q, tt, _ := openReviewableResults(t, recovery(first, failed), 1180, 760)
	m.saveEdit(reviewConfirmNext, nil)
	drainAll(t, m, q)
	tt.Frame()
	tt.Frame()
	if m.selected != 1 {
		t.Fatalf("confirm-and-next selected %d, want the backfill item 1 (notice %q)", m.selected, m.reviewNotice)
	}
	if m.edit == nil || !m.edit.backfill {
		t.Fatalf("the next backfill item did not open its editor: %+v", m.edit)
	}
	if m.reviewNotice == "后面没有待校对项。" {
		t.Fatal("confirm-and-next claimed no next item while one pending item remains")
	}
}

// TestReviewSubstatGroupHasCrops checks that selecting the substat group
// highlights all four row crops and never says the field has no screenshot.
func TestReviewSubstatGroupHasCrops(t *testing.T) {
	roles := []string{"name", "level", "main", "sub1", "sub2", "sub3", "sub4"}
	item := roledItem(t, 1, roles, nil, []string{"a", "b", "c", "d", "e", "f", "g"})
	m, _, tt := openResponsiveResults(t, recovery(item), 1180, 760)
	if got := EvidenceRolesForField(FormFieldSubstats); len(got) != 4 {
		t.Fatalf("substat group roles = %v, want sub1..sub4", got)
	}
	m.selectReviewField(FormFieldSubstats)
	tt.Frame()
	tt.Frame()
	if tt.HasText("这个字段没有截图。") {
		t.Fatalf("the substat group still claims no screenshot: %q", tt.Texts())
	}
	highlight := m.highlightedCropRoles(m.selectedRow(), true)
	for _, role := range []string{"sub1", "sub2", "sub3", "sub4"} {
		if !highlight[role] {
			t.Fatalf("the substat group does not highlight %s: %v", role, highlight)
		}
	}
}

// TestReviewSubstatRowFocus checks that a per-row problem focuses that row
// and highlights only its crop.
func TestReviewSubstatRowFocus(t *testing.T) {
	item := withStructured(t, reviewRowItem(t, 1, 15), func(s *zzz.StructuredDisc) {
		second := s.Data.Substats[0]
		second.Value = stringPtr("")
		s.Data.Substats = append(s.Data.Substats, second)
	})
	m, _, tt := openResponsiveResults(t, recovery(item), 1180, 760)
	m.requestReviewFieldFocus(FormFieldSubstat2)
	tt.Frame()
	tt.Frame()
	if m.reviewField != FormFieldSubstat2 {
		t.Fatalf("substat row focus selected %q, want %q", m.reviewField, FormFieldSubstat2)
	}
	highlight := m.highlightedCropRoles(m.selectedRow(), true)
	if !highlight["sub2"] || highlight["sub1"] {
		t.Fatalf("substat row highlight = %v, want only sub2", highlight)
	}
}

// TestReviewFacetFilters checks the set and slot facets narrow the visible
// rows without changing the record-wide segment counts.
func TestReviewFacetFilters(t *testing.T) {
	m, _, tt := openResponsiveResults(t, responsiveResultRecord(t), 1180, 760)
	if !tt.HasText("全部套装") || !tt.HasText("全部槽位") {
		t.Fatalf("the facet controls are missing: %q", tt.Texts())
	}
	pending := m.resultFilterCount(resultFilterPending)
	m.setFilter = "31100"
	m.afterResultFilterChange()
	tt.Frame()
	rows := m.visibleResultRows()
	if len(rows) == 0 {
		t.Fatal("the set facet hid every pending row")
	}
	for _, index := range rows {
		row := &m.rec.Rows[index]
		if row.Structured == nil || row.Structured.Data.SetID == nil ||
			*row.Structured.Data.SetID != 31100 {
			t.Fatalf("set facet kept row %d with set %+v", row.Seq, row.Structured)
		}
	}
	if got := m.resultFilterCount(resultFilterPending); got != pending {
		t.Fatalf("the facet changed the segment count from %d to %d", pending, got)
	}
	m.slotFilter = "2"
	m.afterResultFilterChange()
	tt.Frame()
	for _, index := range m.visibleResultRows() {
		row := &m.rec.Rows[index]
		if row.Structured == nil || row.Structured.Data.Slot == nil ||
			*row.Structured.Data.Slot != 2 {
			t.Fatalf("slot facet kept row %d with slot %+v", row.Seq, row.Structured)
		}
	}
	m.setFilter = "99999"
	m.afterResultFilterChange()
	tt.Frame()
	if len(m.visibleResultRows()) != 0 {
		t.Fatal("an impossible set facet kept rows visible")
	}
	if !tt.HasText("没有符合筛选条件的条目。") {
		t.Fatalf("the facet empty explanation is missing: %q", tt.Texts())
	}
	if err := tt.Click("清除筛选"); err != nil {
		t.Fatalf("click clear facets: %v", err)
	}
	tt.Frame()
	if m.setFilter != "" || m.slotFilter != "" {
		t.Fatalf("clear facets left set %q slot %q", m.setFilter, m.slotFilter)
	}
}

// TestReviewCompactPopoverVirtualizesRows checks the compact list is a
// virtualized List: 10,000 rows must not build a button per row.
func TestReviewCompactPopoverVirtualizesRows(t *testing.T) {
	m, _, tt := openResponsiveResults(t, manyPendingRecord(t, 10000), 960, 640)
	m.compactListOpen = true
	m.compactListFocusRequested = true
	tt.Frame()
	start := time.Now()
	tt.Frame()
	elapsed := time.Since(start)
	if elapsed > 100*time.Millisecond {
		t.Fatalf("the 10k compact popover frame took %v", elapsed)
	}
	if !tt.HasText("全部套装") || !tt.HasText("全部槽位") {
		t.Fatalf("the compact popover lacks the set/slot facets: %q", tt.Texts())
	}
	first, last := m.list.Visible()
	if first < 0 || last < first {
		t.Fatalf("the compact list shows no rows: %d..%d", first, last)
	}
	rows := 0
	for _, text := range tt.Texts() {
		if strings.HasPrefix(text, "#") {
			rows++
		}
	}
	if rows == 0 || rows > 100 {
		t.Fatalf("the compact popover built %d row texts, want a virtualized window", rows)
	}
}

// TestReviewRowsFollowTextScale checks the list row height and category
// text grow with the desktop text scale instead of clipping the row.
func TestReviewRowsFollowTextScale(t *testing.T) {
	open := func(scale float32) (*Model, *ui.Tester) {
		m, q, tt := openResponsiveResults(t, manyPendingRecord(t, 20), 1180, 760)
		if scale != 1 {
			tt.SetPreferences(ui.Preferences{TextScale: scale})
			settle(t, m, q, tt)
		}
		tt.Frame()
		return m, tt
	}
	_, baseTT := open(1)
	baseBadge, ok := baseTT.Find("待校对")
	if !ok {
		t.Fatalf("the 100%% badge is missing: %q", baseTT.Texts())
	}

	m, tt := open(2)
	if m.textScale != 2 {
		t.Fatalf("text scale = %v, want 2", m.textScale)
	}
	first, ok := tt.Find("#0001")
	if !ok {
		t.Fatalf("the first scaled row is missing: %q", tt.Texts())
	}
	second, ok := tt.Find("#0002")
	if !ok {
		t.Fatalf("the second scaled row is missing: %q", tt.Texts())
	}
	if gap := second.Y - first.Y; gap < reviewListRowHeight*1.8 {
		t.Fatalf("the scaled row pitch = %.1f, want about %.1f", gap, float32(reviewListRowHeight*2))
	}
	badge, ok := tt.Find("待校对")
	if !ok {
		t.Fatalf("the scaled badge is missing: %q", tt.Texts())
	}
	if badge.H <= baseBadge.H {
		t.Fatalf("the scaled badge height %v is not above the 100%% height %v", badge.H, baseBadge.H)
	}
	if badge.Y < first.Y || badge.Y+badge.H > first.Y+reviewListRowHeight*2 {
		t.Fatalf("the scaled badge is clipped by its row: %+v in row at %+v", badge, first)
	}
}

// TestReviewRarityProblemsDedupeByCode checks an old persisted message is
// replaced by the current rarity text and shown once, not beside the
// current validation's gap for the same code.
func TestReviewRarityProblemsDedupeByCode(t *testing.T) {
	item := withStructured(t, reviewRowItem(t, 1, 15), func(s *zzz.StructuredDisc) {
		s.Data.Rarity = nil
		s.Issues = []zzz.Issue{{
			Field: "rarity", Code: zzz.CodeRarityUnverified,
			Severity: zzz.SeverityWarning, Message: "本次捕获没有已验证的 S 稀有度证据，rarity 保留为 null",
		}}
	})
	_, _, tt := openResponsiveResults(t, recovery(item), 1180, 760)
	texts := strings.Join(tt.Texts(), "\n")
	if strings.Count(texts, "稀有度保持未读取") != 1 {
		t.Fatalf("the rarity problem is not shown once: %q", texts)
	}
	if strings.Contains(texts, "rarity 保留为 null") || strings.Contains(texts, "null") {
		t.Fatalf("an old rarity message leaked into the form: %q", texts)
	}
}

// TestReviewMappedFieldWithoutCropExplains checks a field whose evidence
// role exists but this item has no such crop gets a note instead of
// silently doing nothing.
func TestReviewMappedFieldWithoutCropExplains(t *testing.T) {
	roles := []string{"name", "level", "main", "sub1", "sub2", "sub3", "sub4"}
	item := roledItem(t, 1, roles, nil, []string{"a", "b", "c", "d", "e", "f", "g"})
	m, _, tt := openResponsiveResults(t, recovery(item), 1180, 760)
	m.selectReviewField(FormFieldEquipped)
	tt.Frame()
	tt.Frame()
	if !tt.HasText("这个字段的截图缺失。") {
		t.Fatalf("the missing mapped crop is not explained: %q", tt.Texts())
	}
}

// TestReviewFilterStateBelongsToOpenRecord checks that opening another
// record starts with the pending segment and no facets, while reopening
// the record already on screen keeps the user's segment and facets.
func TestReviewFilterStateBelongsToOpenRecord(t *testing.T) {
	svc := &fakeService{
		list: []record.RecordInfo{{
			ID: "old", LatestRevision: 1,
			SavedAt: time.Date(2026, 10, 20, 22, 18, 0, 0, time.UTC),
		}},
		loads: map[string]*record.Recovery{
			"old": responsiveResultRecord(t),
			"s1":  responsiveResultRecord(t),
		},
	}
	m, q := newTestModel(t, Config{Service: svc})
	m.SetContentSize(1180, 760)
	tt := ui.NewTester(m.View, 1180, 760)
	settle(t, m, q, tt)
	m.Apply(snap("s1", 1, false, scan.PhaseEnded,
		scan.Counts{Captured: 4, Produced: 2}, 4, false, ""))
	settle(t, m, q, tt)

	m.filter = resultFilterAll
	m.setFilter = "31100"
	m.slotFilter = "2"
	m.selected = 1
	m.selectedSequence = 2
	m.histChoice = historyLabel(t, m, "old")
	m.OpenHistory()
	settle(t, m, q, tt)
	showRecordItems(m)
	tt.Frame()

	if m.rec == nil || m.rec.ID != "old" || m.rec.Live {
		t.Fatalf("the chosen history record did not open: %+v", m.rec)
	}
	if m.filter != resultFilterPending || m.setFilter != "" || m.slotFilter != "" {
		t.Fatalf("the new record kept the previous filters: filter %v set %q slot %q",
			m.filter, m.setFilter, m.slotFilter)
	}

	m.filter = resultFilterAll
	m.setFilter = "31200"
	m.slotFilter = "2"
	m.selected = 1
	m.selectedSequence = m.rec.Rows[1].Seq
	m.OpenHistory()
	settle(t, m, q, tt)
	showRecordItems(m)
	tt.Frame()
	if m.filter != resultFilterAll || m.setFilter != "31200" || m.slotFilter != "2" {
		t.Fatalf("reopening reset the open record's filters: filter %v set %q slot %q",
			m.filter, m.setFilter, m.slotFilter)
	}
	if m.selectedSeq() != m.rec.Rows[1].Seq {
		t.Fatalf("the reopen moved the selection to seq %d", m.selectedSeq())
	}
}

// TestZoomModeDropsDecodedCache checks a zoom change does not reuse a
// bitmap whose pixelated style belongs to the previous mode.
func TestZoomModeDropsDecodedCache(t *testing.T) {
	item := roledItem(t, 1, []string{"name", "level"}, nil, []string{"河豚电音", "15"})
	images := map[string]*ui.Bitmap{
		"rec/000001-line0": tallBitmap(1),
		"rec/000001-line1": tallBitmap(2),
	}
	svc := &fakeService{
		list: []record.RecordInfo{{
			ID: "rec", LatestRevision: 1,
			SavedAt: time.Date(2026, 10, 21, 14, 2, 0, 0, time.UTC),
		}},
		loads: map[string]*record.Recovery{"rec": recovery(item)},
	}
	m, q := newTestModel(t, Config{Service: svc, Evidence: &fakeEvidence{images: images}})
	m.SetContentSize(1180, 760)
	tt := ui.NewTester(m.View, 1180, 760)
	settle(t, m, q, tt)
	m.histChoice = historyLabel(t, m, "rec")
	m.OpenHistory()
	settle(t, m, q, tt)
	showRecordItems(m)
	settle(t, m, q, tt)
	tt.Frame()
	if len(m.cache.entries) == 0 {
		t.Fatal("the initial evidence was not cached")
	}
	m.zoomCrop(reviewZoomStep)
	tt.Frame()
	settle(t, m, q, tt)
	if len(m.cache.entries) == 0 {
		t.Fatal("the zoomed evidence was not reloaded into the cache")
	}
	m.zoomCrop(0)
	tt.Frame()
	if len(m.cache.entries) != 0 {
		t.Fatalf("fit mode reused %d pixelated cache entries", len(m.cache.entries))
	}
}

// TestRecordClassCountsMatchExportSummary pins the one four-class source:
// built rows and the export snapshot must agree once both are ready.
func TestRecordClassCountsMatchExportSummary(t *testing.T) {
	m, _, tt := openResponsiveResults(t, responsiveResultRecord(t), 1180, 760)
	tt.Frame()
	if m.exportData == nil {
		t.Fatal("the export snapshot is missing")
	}
	if got, want := m.recordClassCounts(), m.exportData.Summary.Counts; got != want {
		t.Fatalf("recordClassCounts = %+v, export summary = %+v", got, want)
	}
}

// TestRecordClassCountsFallsBackToClasses checks the pre-row fallback.
func TestRecordClassCountsFallsBackToClasses(t *testing.T) {
	want := app.ItemClassCounts{PendingBackfill: 2, PendingReview: 1, Warning: 3, Passed: 4}
	m := &Model{rec: &RecordView{Classes: want}}
	if got := m.recordClassCounts(); got != want {
		t.Fatalf("recordClassCounts = %+v, want %+v", got, want)
	}
}

// TestReviewCountsFollowBackfillState checks the status line and export
// summary do not call an interpreted failed item failed.
func TestReviewCountsFollowBackfillState(t *testing.T) {
	backfill := func(quality record.Quality) record.ItemRecord {
		item := reviewRowItem(t, 1, 15)
		item.Status = record.StatusFailed
		item.Failure = "识别超时"
		item.Quality = quality
		item.Revision = 1
		return item
	}

	m, _, tt := openResponsiveResults(t, recovery(backfill(record.QualityNeedsReview)), 1180, 760)
	tt.Frame()
	if counts := m.recordCounts(); counts.Failed != 0 || counts.Pending != 1 || counts.Exportable != 0 {
		t.Fatalf("saved backfill counts = %+v, want pending review only", counts)
	}
	if got := m.pendingText(); got != "待校对 1 件" {
		t.Fatalf("saved backfill pending text = %q", got)
	}

	m, _, tt = openResponsiveResults(t, recovery(backfill(record.QualityHumanConfirmed)), 1180, 760)
	tt.Frame()
	if counts := m.recordCounts(); counts.Failed != 0 || counts.Pending != 0 || counts.Exportable != 1 {
		t.Fatalf("confirmed backfill counts = %+v, want one exportable", counts)
	}
	if got := m.pendingText(); got != "待校对 0 · 全部可导出" {
		t.Fatalf("confirmed backfill pending text = %q", got)
	}
}

// TestWorkspaceSavedStateIncludesReviewSave checks the title bar follows a
// revision save in flight and a failed revision save.
func TestWorkspaceSavedStateIncludesReviewSave(t *testing.T) {
	m, _, _ := openResponsiveResults(t, recovery(reviewRowItem(t, 1, 15)), 1180, 760)
	m.reviewSaving = true
	if _, _, saved := m.workspaceStatusTexts(); saved != "正在保存…" {
		t.Fatalf("saving title = %q, want 正在保存…", saved)
	}
	m.reviewSaving = false
	m.reviewSaveFailed = true
	if _, _, saved := m.workspaceStatusTexts(); saved != "保存失败" {
		t.Fatalf("failed title = %q, want 保存失败", saved)
	}
}

// TestLiveScanDoesNotShowFinishedReview checks the completion surface waits
// for the live scan to end and save.
func TestLiveScanDoesNotShowFinishedReview(t *testing.T) {
	m, _, tt := openResponsiveResults(t, recovery(confirmedReviewItem(t, 1, 12)), 1180, 760)
	m.rec.Live = true
	m.snap = snap("rec", 1, true, scan.PhaseCapturing, scan.Counts{Captured: 1, Produced: 1}, 1, false, "")
	tt.Frame()
	if tt.HasText("都处理完了") {
		t.Fatalf("a running scan shows the finished review: %q", tt.Texts())
	}
}

// TestReviewFacetFilterFitsList checks the two standard facets stack inside
// the 240-DIP list column instead of clipping the slot picker.
func TestReviewFacetFilterFitsList(t *testing.T) {
	_, _, tt := openResponsiveResults(t, pendingReviewRecord(t), 1180, 760)
	tt.Frame()
	set, ok := tt.Find("全部套装")
	if !ok {
		t.Fatalf("the set facet is missing: %q", tt.Texts())
	}
	slot, ok := tt.Find("全部槽位")
	if !ok {
		t.Fatalf("the slot facet is missing: %q", tt.Texts())
	}
	const listRight = float32(navRailWidth + reviewPagePadding + reviewListWidth)
	if set.X+set.W > listRight || slot.X+slot.W > listRight {
		t.Fatalf("a facet exceeds the list column: set %+v slot %+v right %.1f", set, slot, listRight)
	}
	if slot.Y <= set.Y {
		t.Fatalf("the facets are not stacked: set %+v slot %+v", set, slot)
	}
}

// TestReviewProblemsDedupeByCode checks issues are deduplicated by field
// and code even when their prose differs; a message-keyed mutation keeps
// both.
func TestReviewProblemsDedupeByCode(t *testing.T) {
	item := withStructured(t, reviewRowItem(t, 1, 15), func(s *zzz.StructuredDisc) {
		s.Issues = []zzz.Issue{
			{Field: "level", Code: "custom", Severity: zzz.SeverityWarning, Message: "第一条"},
			{Field: "level", Code: "custom", Severity: zzz.SeverityWarning, Message: "第二条"},
		}
	})
	row := buildRow(&item)
	m := &Model{}
	count := 0
	for _, problem := range m.reviewProblems(&row) {
		if problem.Field == "level" && problem.Code == "custom" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("same (field, code) produced %d problems, want 1", count)
	}
}

// TestReviewSelectedRowNumberReadable checks the selected row's sequence
// number keeps a dark foreground on the light selected background.
func TestReviewSelectedRowNumberReadable(t *testing.T) {
	_, _, tt := openResponsiveResults(t, manyPendingRecord(t, 5), 1180, 760)
	tt.Frame()
	rect, ok := tt.Find("#0001")
	if !ok {
		t.Fatalf("the selected row number is missing: %q", tt.Texts())
	}
	if dark := countDarkPixels(tt.Image(), rect); dark < 5 {
		t.Fatalf("the selected row number has only %d dark pixels: %+v", dark, rect)
	}
}

func countDarkPixels(img *image.RGBA, r ui.Rect) int {
	rect := image.Rect(int(r.X), int(r.Y), int(r.X+r.W), int(r.Y+r.H)).Intersect(img.Bounds())
	count := 0
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			p := img.RGBAAt(x, y)
			if int(p.R)+int(p.G)+int(p.B) < 450 {
				count++
			}
		}
	}
	return count
}

// TestReviewFormControlsFitMinimumWidth checks the main-stat widths add up
// inside the 400-DIP form at 960. The constants are the actual widths the
// row uses, so mutating them back to the old values fails here.
func TestReviewFormControlsFitMinimumWidth(t *testing.T) {
	const formInner = reviewFormWidth - 2*16 - 2*8
	total := float32(reviewMainAttributeWidth + reviewMainValueWidth + reviewMainUnitWidth + 2*8)
	if total > formInner {
		t.Fatalf("main-stat row width %.0f exceeds the form inner width %.0f", total, float32(formInner))
	}

	m, _, tt := openResponsiveResults(t, recovery(reviewRowItem(t, 1, 15)), 960, 640)
	tt.Frame()
	m.formExtrasOpen = true
	tt.Frame()
	tt.Frame()
	const formRight = 960 - reviewPagePadding
	if m.mainUnitBounds.W <= 0 || m.mainUnitBounds.X+m.mainUnitBounds.W > formRight {
		t.Fatalf("rendered main-stat unit bounds = %+v, form right %d", m.mainUnitBounds, formRight)
	}
}

// TestReviewFacetChangePromptsForDirtyDraft checks a facet change that
// would leave the edited row goes through the unsaved-changes prompt.
func TestReviewFacetChangePromptsForDirtyDraft(t *testing.T) {
	m, q, tt, _ := openReviewableResults(t, responsiveResultRecord(t), 1180, 760)
	defer assertReviewBinding(t, m)
	tt.Frame()
	seq := m.selectedSeq()
	m.edit.draft.Level = "11"
	m.edit.refresh()
	tt.Frame()
	if err := tt.Click("全部套装"); err != nil {
		t.Fatalf("open set facet: %v", err)
	}
	tt.Frame()
	if err := tt.Click(describeSet(31200)); err != nil {
		t.Fatalf("choose set: %v", err)
	}
	tt.Frame()
	drainAll(t, m, q)
	tt.Frame()
	if m.navPrompt == nil {
		t.Fatalf("a facet change dropped the dirty draft without a prompt: %s", probeReviewState(m))
	}
	if m.setFilter != "" || m.edit == nil || m.edit.seq != seq || m.edit.draft.Level != "11" {
		t.Fatalf("facet change mutated the dirty state: %s", probeReviewState(m))
	}
}

// TestReviewFacetChangePromptsAfterFailedSave checks the failure band and
// its input survive a facet change that would leave the row.
func TestReviewFacetChangePromptsAfterFailedSave(t *testing.T) {
	m, q, tt, svc := openReviewableResults(t, responsiveResultRecord(t), 1180, 760)
	defer assertReviewBinding(t, m)
	tt.Frame()
	svc.saveFn = func(string, int64, int64, app.ItemRevisionEdit) (app.ItemRevisionResult, error) {
		return app.ItemRevisionResult{}, fmt.Errorf("磁盘已满")
	}
	m.edit.draft.Level = "11"
	m.saveEdit(reviewSave, nil)
	drainAll(t, m, q)
	tt.Frame()
	if !m.reviewSaveFailed {
		t.Fatalf("failed save did not set the failure state: %s", probeReviewState(m))
	}
	if err := tt.Click("全部套装"); err != nil {
		t.Fatalf("open set facet: %v", err)
	}
	tt.Frame()
	if err := tt.Click(describeSet(31200)); err != nil {
		t.Fatalf("choose set: %v", err)
	}
	tt.Frame()
	drainAll(t, m, q)
	tt.Frame()
	if m.navPrompt == nil || m.edit == nil || m.edit.draft.Level != "11" {
		t.Fatalf("failed-save input was dropped by a facet change: %s", probeReviewState(m))
	}
	if !m.reviewSaveFailed {
		t.Fatalf("the save failure state was cleared by a filter change: %s", probeReviewState(m))
	}
	if _, _, saved := m.workspaceStatusTexts(); saved != "保存失败" {
		t.Fatalf("title after failed-save facet change = %q, want 保存失败", saved)
	}
}

// TestReviewSegmentChangePromptsFromAllToPending checks a real 全部 →
// 待处理 click with a dirty passed-row editor.
func TestReviewSegmentChangePromptsFromAllToPending(t *testing.T) {
	m, q, tt, _ := openReviewableResults(t, pendingReviewRecord(t), 1180, 760)
	defer assertReviewBinding(t, m)
	tt.Frame()
	if err := tt.Click("全部"); err != nil {
		t.Fatalf("click 全部: %v", err)
	}
	tt.Frame()
	m.requestNavigation(navigationRequest{kind: navSelect, seq: 5})
	tt.Frame()
	if err := tt.Click("修改"); err != nil {
		t.Fatalf("open passed-row editor: %v", err)
	}
	tt.Frame()
	m.edit.draft.Level = "11"
	m.edit.refresh()
	tt.Frame()
	if err := tt.Click("待处理"); err != nil {
		t.Fatalf("click 待处理: %v", err)
	}
	tt.Frame()
	drainAll(t, m, q)
	tt.Frame()
	if m.navPrompt == nil {
		t.Fatalf("全部 → 待处理 dropped the dirty draft: %s", probeReviewState(m))
	}
	if m.filter != resultFilterAll || m.edit == nil || m.edit.draft.Level != "11" {
		t.Fatalf("全部 → 待处理 mutated the dirty state: %s", probeReviewState(m))
	}
}

// TestReviewSegmentChangePromptsPendingToWarning covers the path that
// previously popped only because the frame still used the old visible list.
func TestReviewSegmentChangePromptsPendingToWarning(t *testing.T) {
	m, q, tt, _ := openReviewableResults(t, pendingReviewRecord(t), 1180, 760)
	defer assertReviewBinding(t, m)
	tt.Frame()
	m.edit.draft.Level = "11"
	m.edit.refresh()
	tt.Frame()
	if err := tt.Click("警告"); err != nil {
		t.Fatalf("click 警告: %v", err)
	}
	tt.Frame()
	drainAll(t, m, q)
	tt.Frame()
	if m.navPrompt == nil {
		t.Fatalf("待处理 → 警告 dropped the dirty draft: %s", probeReviewState(m))
	}
	if m.filter != resultFilterPending || m.edit == nil || m.edit.draft.Level != "11" {
		t.Fatalf("待处理 → 警告 mutated the dirty state: %s", probeReviewState(m))
	}
}

func probeReviewState(m *Model) string {
	seq, level := int64(-1), ""
	if m.edit != nil {
		seq, level = m.edit.seq, m.edit.draft.Level
	}
	_, _, saved := m.workspaceStatusTexts()
	return fmt.Sprintf("filter=%v set=%q slot=%q selected=%d edit=%d level=%q prompt=%v title=%q err=%q failed=%v",
		m.filter, m.setFilter, m.slotFilter, m.selectedSeq(), seq, level, m.navPrompt != nil, saved, m.reviewErr, m.reviewSaveFailed)
}

// TestWorkspaceUnsavedTitle checks the dirty editor takes the title's save
// slot, after a failed save and before the ordinary saved state.
func TestWorkspaceUnsavedTitle(t *testing.T) {
	m, _, _ := openResponsiveResults(t, recovery(reviewRowItem(t, 1, 15)), 1180, 760)
	m.edit.draft.Level = "11"
	m.edit.refresh()
	if _, _, saved := m.workspaceStatusTexts(); saved != "未保存修改" {
		t.Fatalf("dirty title = %q, want 未保存修改", saved)
	}
	m.reviewSaveFailed = true
	if _, _, saved := m.workspaceStatusTexts(); saved != "保存失败" {
		t.Fatalf("failure title lost priority: %q", saved)
	}
}

// TestReviewSaveFailureStateSetAndCleared covers the real set, retry and
// clear paths of reviewSaveFailed (L5).
func TestReviewSaveFailureStateSetAndCleared(t *testing.T) {
	m, q, tt, svc := openReviewableResults(t, manyPendingRecord(t, 2), 1180, 760)
	tt.Frame()
	good := svc.saveFn
	svc.saveFn = func(string, int64, int64, app.ItemRevisionEdit) (app.ItemRevisionResult, error) {
		return app.ItemRevisionResult{}, fmt.Errorf("磁盘已满")
	}
	m.edit.draft.Level = "11"
	m.saveEdit(reviewSave, nil)
	drainAll(t, m, q)
	tt.Frame()
	if !m.reviewSaveFailed {
		t.Fatalf("failed save did not set reviewSaveFailed: %s", probeReviewState(m))
	}
	if _, _, saved := m.workspaceStatusTexts(); saved != "保存失败" {
		t.Fatalf("failed title = %q", saved)
	}

	svc.saveFn = good
	m.saveEdit(m.lastAction, nil)
	if _, _, saved := m.workspaceStatusTexts(); saved != "正在保存…" {
		t.Fatalf("retry in-flight title = %q", saved)
	}
	drainAll(t, m, q)
	tt.Frame()
	if m.reviewSaveFailed || m.reviewErr != "" {
		t.Fatalf("successful retry left failure state: %s", probeReviewState(m))
	}
	if _, _, saved := m.workspaceStatusTexts(); saved != "已保存" {
		t.Fatalf("retry success title = %q", saved)
	}
}

// TestReviewRevertAfterFailedSaveClearsFailure covers L1: the input is put
// back to its saved value, then the retry is a no-op that still clears the
// previous failure.
func TestReviewRevertAfterFailedSaveClearsFailure(t *testing.T) {
	m, q, tt, svc := openReviewableResults(t, manyPendingRecord(t, 2), 1180, 760)
	tt.Frame()
	original := m.edit.draft.Level
	svc.saveFn = func(string, int64, int64, app.ItemRevisionEdit) (app.ItemRevisionResult, error) {
		return app.ItemRevisionResult{}, fmt.Errorf("磁盘已满")
	}
	m.edit.draft.Level = "11"
	m.saveEdit(reviewSave, nil)
	drainAll(t, m, q)
	tt.Frame()
	m.edit.draft.Level = original
	m.edit.refresh()
	m.saveEdit(m.lastAction, nil)
	if m.reviewSaveFailed || m.reviewErr != "" {
		t.Fatalf("reverted retry kept the failure: %s", probeReviewState(m))
	}
	if m.reviewNotice != "没有修改需要保存。" {
		t.Fatalf("reverted retry notice = %q", m.reviewNotice)
	}
	if _, _, saved := m.workspaceStatusTexts(); saved != "已保存" {
		t.Fatalf("reverted retry title = %q", saved)
	}
}

// TestReviewConflictClearsFailure covers L2: the conflict is resolved by a
// reload, so the failure title must not outlive it.
func TestReviewConflictClearsFailure(t *testing.T) {
	m, q, _, svc := openReviewableResults(t, manyPendingRecord(t, 1), 1180, 760)
	svc.saveFn = func(string, int64, int64, app.ItemRevisionEdit) (app.ItemRevisionResult, error) {
		return app.ItemRevisionResult{}, fmt.Errorf("stale: %w", record.ErrRevisionConflict)
	}
	m.edit.draft.Level = "11"
	m.saveEdit(reviewSave, nil)
	drainAll(t, m, q)
	if m.reviewSaveFailed {
		t.Fatalf("conflict reload kept the failure state: %s", probeReviewState(m))
	}
	if _, _, saved := m.workspaceStatusTexts(); saved == "保存失败" {
		t.Fatalf("conflict reload kept the failure title: %s", probeReviewState(m))
	}
}

// TestReviewFilterPromptSaveAppliesAfterSuccess checks the deferred
// filter request really runs after a successful save.
func TestReviewFilterPromptSaveAppliesAfterSuccess(t *testing.T) {
	m, q, tt, _ := openReviewableResults(t, responsiveResultRecord(t), 1180, 760)
	defer assertReviewBinding(t, m)
	tt.Frame()
	m.edit.draft.Level = "11"
	m.edit.refresh()
	tt.Frame()
	if err := tt.Click("全部套装"); err != nil {
		t.Fatalf("open set facet: %v", err)
	}
	tt.Frame()
	if err := tt.Click(describeSet(31200)); err != nil {
		t.Fatalf("choose set: %v", err)
	}
	tt.Frame()
	if m.navPrompt == nil {
		t.Fatal("no prompt before save")
	}
	if err := tt.Click("保存并继续"); err != nil {
		t.Fatalf("save and continue: %v", err)
	}
	drainAll(t, m, q)
	tt.Frame()
	if m.navPrompt != nil || m.setFilter != "31200" {
		t.Fatalf("deferred filter did not apply after save: %s", probeReviewState(m))
	}
}

// TestHistoryEmptyPendingTextWithDirtyEditor checks L3: a finished history
// record must not claim the scan is still running just because an editor is
// dirty.
func TestHistoryEmptyPendingTextWithDirtyEditor(t *testing.T) {
	m := &Model{
		rec:     &RecordView{Live: false},
		recData: &record.Recovery{},
		edit: &editState{
			recordID: "r", seq: 1,
			original: editDraft{Level: "15"},
			draft:    editDraft{Level: "11"},
		},
	}
	m.filter = resultFilterPending
	if got := m.filteredEmptyText(); got != "待处理 0 件，导出清单尚未就绪。" {
		t.Fatalf("history empty text = %q, want a finished-state text", got)
	}
	if !m.editingDirty() {
		t.Fatal("the test fixture is not exercising the dirty history path")
	}
}

// TestEnsureReviewEditKeepsDirtyDraft checks the render path never replaces
// a dirty editor when a row is requested directly.
func TestEnsureReviewEditKeepsDirtyDraft(t *testing.T) {
	m, _, _, _ := openReviewableResults(t, manyPendingRecord(t, 2), 1180, 760)
	m.edit.draft.Level = "11"
	m.edit.refresh()
	seq := m.edit.seq
	m.ensureReviewEdit(&m.rec.Rows[1])
	if m.edit == nil || m.edit.seq != seq || m.edit.draft.Level != "11" {
		t.Fatalf("ensureReviewEdit replaced a dirty editor: %s", probeReviewState(m))
	}
}

// assertReviewBinding is the invariant shared by every navigation path:
// an editor, when open, names the selected row and that row is visible.
func assertReviewBinding(t *testing.T, m *Model) {
	t.Helper()
	if m.edit == nil {
		return
	}
	row := m.selectedRow()
	if row == nil || m.edit.seq != row.Seq || m.edit.recordID != m.rec.ID {
		t.Fatalf("editor #%d is not bound to selected #%d (%s)", m.edit.seq, rowSeq(row), probeReviewState(m))
	}
	if !slices.Contains(m.visibleResultRows(), m.selected) {
		t.Fatalf("selected #%d is not visible for the editor: %s", m.edit.seq, probeReviewState(m))
	}
}

func rowSeq(row *Row) int64 {
	if row == nil {
		return -1
	}
	return row.Seq
}

// reviewWarningItem is an exportable warning row: usable, auto accepted
// and carrying one equipped warning.
func reviewWarningItem(t *testing.T, seq int64) record.ItemRecord {
	t.Helper()
	item := withStructured(t, reviewRowItem(t, seq, 15), func(s *zzz.StructuredDisc) {
		s.Issues = []zzz.Issue{{
			Field: "equipped", Code: zzz.CodeEquippedStateUnavailable,
			Severity: zzz.SeverityWarning, Message: "装备者未识别",
		}}
	})
	item.Quality = record.QualityAutoAccepted
	return item
}

// TestReviewReloadFailureKeepsDirtyDraft covers L11's silent-loss path: a
// failed refresh must keep the typed draft (writes stay blocked by recErr).
func TestReviewReloadFailureKeepsDirtyDraft(t *testing.T) {
	m, q, tt, svc := openReviewableResults(t, recovery(reviewRowItem(t, 1, 12)), 1180, 760)
	tt.Frame()
	if m.edit == nil {
		t.Fatal("the pending row did not open its editor")
	}
	m.edit.draft.Level = "11"
	m.edit.refresh()
	svc.mu.Lock()
	svc.loadErr = map[string]error{"rec": fmt.Errorf("读取失败")}
	svc.mu.Unlock()
	m.reloadRecord("rec", false)
	drainAll(t, m, q)
	tt.Frame()
	if m.edit == nil || m.edit.draft.Level != "11" {
		t.Fatalf("the failed reload dropped the draft: %s", probeReviewState(m))
	}
	if m.recErr == "" {
		t.Fatalf("the failed reload lost its error: %s", probeReviewState(m))
	}
}

// TestReviewFormBlockedWhileCleanupRuns covers L11: deletion reloads the
// open record, so the form must be read-only while that deletion runs.
func TestReviewFormBlockedWhileCleanupRuns(t *testing.T) {
	m, _, tt, _ := openReviewableResults(t, recovery(reviewRowItem(t, 1, 12)), 1180, 760)
	tt.Frame()
	if m.edit == nil {
		t.Fatal("the pending row did not open its editor")
	}
	m.cleanup = &cleanupPrompt{
		target:   cleanupOneScreenshots,
		recordID: "rec",
		ready:    true,
		running:  true,
	}
	tt.Frame()
	if !m.reviewActionsBlocked() {
		t.Fatal("the review form is still editable while cleanup runs")
	}
}

// TestReviewCleanEditorClearedWhenFilterHasNoRows covers L9: an empty
// segment must not leave a clean editor and its save hint behind.
func TestReviewCleanEditorClearedWhenFilterHasNoRows(t *testing.T) {
	m, _, tt, _ := openReviewableResults(t, recovery(reviewRowItem(t, 1, 12)), 1180, 760)
	tt.Frame()
	if m.edit == nil {
		m.startEdit()
		tt.Frame()
	}
	if m.edit == nil {
		t.Fatal("the pending row did not open its editor")
	}
	if err := tt.Click("警告"); err != nil {
		t.Fatalf("click 警告: %v", err)
	}
	tt.Frame()
	if m.edit != nil {
		t.Fatalf("clean editor survived an empty filter: %s", probeReviewState(m))
	}
	if m.selected != -1 {
		t.Fatalf("empty filter selected row %d: %s", m.selected, probeReviewState(m))
	}
	if strings.Contains(m.resultShortcutText(), shortcutSave+" 保存修改") {
		t.Fatalf("the empty filter still advertises save: %q", m.resultShortcutText())
	}
}

// TestReviewFilterClearsFailedRowFeedback covers L10: after a failed
// confirmation, switching to a segment with another selected row must not
// leave the old failure band and retry action on that row.
func TestReviewFilterClearsFailedRowFeedback(t *testing.T) {
	m, q, tt, svc := openReviewableResults(t, recovery(
		reviewRowItem(t, 1, 12),
		reviewWarningItem(t, 4),
	), 960, 640)
	tt.Frame()
	svc.mu.Lock()
	svc.saveFn = func(string, int64, int64, app.ItemRevisionEdit) (app.ItemRevisionResult, error) {
		return app.ItemRevisionResult{}, fmt.Errorf("磁盘已满")
	}
	svc.mu.Unlock()
	if err := tt.Click("确认并下一项"); err != nil {
		t.Fatalf("click confirm: %v", err)
	}
	drainAll(t, m, q)
	tt.Frame()
	if m.reviewErr == "" {
		t.Fatal("the failed confirmation did not leave a band")
	}
	if err := tt.Click("警告"); err != nil {
		t.Fatalf("click 警告: %v", err)
	}
	tt.Frame()
	if m.selectedSeq() != 4 {
		t.Fatalf("warning filter selected #%d: %s", m.selectedSeq(), probeReviewState(m))
	}
	if m.reviewErr != "" || m.reviewSaveFailed {
		t.Fatalf("#4 kept #1's failure: err=%q failed=%v", m.reviewErr, m.reviewSaveFailed)
	}
	if tt.HasText("重试确认并下一项") {
		t.Fatalf("#4 still offers #1's retry: %q", tt.Texts())
	}
}

// TestReviewFilterChangeClearsCleanEditor covers H2: a clean editor must
// not survive a filter change and Ctrl+Enter must never confirm the hidden
// old row.
func TestReviewFilterChangeClearsCleanEditor(t *testing.T) {
	m, q, tt, svc := openReviewableResults(t, recovery(
		reviewRowItem(t, 1, 12),
		reviewWarningItem(t, 2),
	), 1180, 760)
	tt.Frame()
	if m.edit == nil || m.edit.seq != 1 {
		t.Fatalf("the pending row did not open its editor: %+v", m.edit)
	}
	if err := tt.Click("警告"); err != nil {
		t.Fatalf("click 警告: %v", err)
	}
	tt.Frame()
	assertReviewBinding(t, m)
	if m.selectedSeq() != 2 || m.rec.Rows[m.selected].ItemClass != app.ItemClassWarning {
		t.Fatalf("warning filter selected row %d (%s): %s", m.selectedSeq(),
			m.rec.Rows[m.selected].ItemClass, probeReviewState(m))
	}
	if m.edit != nil && m.edit.seq == 1 {
		t.Fatalf("the old clean editor survived the filter: %s", probeReviewState(m))
	}
	tt.Key(ui.Cmd, ui.KeyEnter)
	drainAll(t, m, q)
	tt.Frame()
	if got := svc.saveCount(); got != 0 {
		t.Fatalf("Ctrl+Enter confirmed a hidden row: %d saves", got)
	}
	assertReviewBinding(t, m)
}

// TestReviewConfirmedDraftFacetPrompts covers M2: a confirmed item's dirty
// draft must prompt, not remain attached to a list that no longer shows it.
func TestReviewConfirmedDraftFacetPrompts(t *testing.T) {
	m, q, tt, _ := openReviewableResults(t, pendingReviewRecord(t), 1180, 760)
	defer assertReviewBinding(t, m)
	tt.Frame()
	m.confirmCurrentStay()
	drainAll(t, m, q)
	tt.Frame()
	if err := tt.Click("修改"); err != nil {
		t.Fatalf("edit confirmed row: %v", err)
	}
	tt.Frame()
	if m.edit == nil {
		t.Fatal("the confirmed row did not open its editor")
	}
	m.edit.draft.Level = "11"
	m.edit.refresh()
	tt.Frame()
	if !m.editingDirty() {
		t.Fatal("the confirmed-row draft is not dirty")
	}
	// The confirmed row is still visible through the visit's marker; the
	// facet change must still prompt because the target pending segment
	// excludes it.
	m.requestResultFilter(resultFilterPending, "31100", "")
	tt.Frame()
	if m.navPrompt == nil || m.filter != resultFilterPending || m.setFilter != "" {
		t.Fatalf("confirmed dirty facet did not prompt: %s", probeReviewState(m))
	}
	assertReviewBinding(t, m)
	if err := tt.Click("留在这里"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if m.edit == nil || m.edit.draft.Level != "11" || m.setFilter != "" {
		t.Fatalf("staying lost the confirmed draft or applied the facet: %s", probeReviewState(m))
	}
	assertReviewBinding(t, m)
}

// TestReviewEmptyFacetDoesNotStrandDraft covers M2's empty-filter trap.
func TestReviewEmptyFacetDoesNotStrandDraft(t *testing.T) {
	m, _, tt, _ := openReviewableResults(t, pendingReviewRecord(t), 1180, 760)
	defer assertReviewBinding(t, m)
	tt.Frame()
	m.edit.draft.Level = "11"
	m.edit.refresh()
	tt.Frame()
	m.requestResultFilter(m.filter, "99999", "9")
	tt.Frame()
	if m.navPrompt == nil {
		t.Fatalf("empty facet dropped the draft: %s", probeReviewState(m))
	}
	assertReviewBinding(t, m)
	if err := tt.Click("留在这里"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if m.edit == nil || m.edit.draft.Level != "11" || m.setFilter != "" || m.slotFilter != "" {
		t.Fatalf("staying on the empty facet lost the draft: %s", probeReviewState(m))
	}
	if tt.HasText("没有符合筛选条件的条目。") {
		t.Fatalf("the empty facet persisted after stay: %q", tt.Texts())
	}
	assertReviewBinding(t, m)
}

// TestReviewSummaryCountPromptsDirtyDraft checks the summary count entry
// uses the same navigation guard.
func TestReviewSummaryCountPromptsDirtyDraft(t *testing.T) {
	m, _, tt, _ := openReviewableResults(t, pendingReviewRecord(t), 1180, 760)
	defer assertReviewBinding(t, m)
	tt.Frame()
	m.edit.draft.Level = "11"
	m.edit.refresh()
	tt.Frame()
	m.openSummaryClass(app.ItemClassPendingReview)
	tt.Frame()
	if m.navPrompt == nil || m.recordTab != recordTabItems {
		t.Fatalf("summary count did not prompt: %s", probeReviewState(m))
	}
	assertReviewBinding(t, m)
}

// TestReviewCompactWalkPromptsDirtyDraft checks the compact ‹ › buttons
// use requestNavigation.
func TestReviewCompactWalkPromptsDirtyDraft(t *testing.T) {
	m, _, tt, _ := openReviewableResults(t, manyPendingRecord(t, 3), 960, 640)
	defer assertReviewBinding(t, m)
	tt.Frame()
	m.edit.draft.Level = "11"
	m.edit.refresh()
	tt.Frame()
	if err := tt.Click("›"); err != nil {
		t.Fatalf("click compact next: %v", err)
	}
	tt.Frame()
	if m.navPrompt == nil || m.selected != 0 {
		t.Fatalf("compact next did not prompt: %s", probeReviewState(m))
	}
	assertReviewBinding(t, m)
}

// TestSettleResultSelectionHonorsDirtyBinding is the direct guard check:
// a dirty selection must not move even if the filter field is changed by a
// stray caller. It catches removal of settleResultSelection's protection.
func TestSettleResultSelectionHonorsDirtyBinding(t *testing.T) {
	m, _, _, _ := openReviewableResults(t, pendingReviewRecord(t), 1180, 760)
	m.edit.draft.Level = "11"
	m.edit.refresh()
	seq := m.selectedSeq()
	m.filter = resultFilterWarning
	m.settleResultSelection()
	if m.selectedSeq() != seq || m.edit == nil || m.edit.draft.Level != "11" {
		t.Fatalf("settle moved a dirty selection: %s", probeReviewState(m))
	}
}

// TestReviewFilterRejectsWhileSaving checks the save-in-flight refusal.
func TestReviewFilterRejectsWhileSaving(t *testing.T) {
	m, _, _, _ := openReviewableResults(t, pendingReviewRecord(t), 1180, 760)
	m.reviewSaving = true
	m.requestResultFilter(resultFilterWarning, "", "")
	if m.filter != resultFilterPending || m.reviewErr == "" {
		t.Fatalf("filter change during save = filter %v err %q", m.filter, m.reviewErr)
	}
}

// TestReviewFilterSyncsTableSelected checks the table selection pointer
// follows every applied filter so a later click cannot target a stale row.
func TestReviewFilterSyncsTableSelected(t *testing.T) {
	m, _, tt, _ := openReviewableResults(t, pendingReviewRecord(t), 1180, 760)
	tt.Frame()
	// Choose a row that is not first in the target segment, so the sync is
	// observable rather than accidentally already correct.
	m.clearEdit()
	m.selected = 4
	m.selectedSequence = m.rec.Rows[4].Seq
	m.tableSelected = 0
	m.requestResultFilter(resultFilterAll, "", "")
	want := resultDisplayIndex(m.visibleResultRows(), m.selected)
	if want != 4 || m.tableSelected != want {
		t.Fatalf("filter did not sync tableSelected: got %d, want %d (selected %d)",
			m.tableSelected, want, m.selected)
	}
	assertReviewBinding(t, m)
}

// TestReviewStayKeepsFailureBand covers L7: choosing 留在这里 after a
// failed save must keep the reason and retry affordance.
func TestReviewStayKeepsFailureBand(t *testing.T) {
	m, q, tt, svc := openReviewableResults(t, pendingReviewRecord(t), 1180, 760)
	tt.Frame()
	svc.saveFn = func(string, int64, int64, app.ItemRevisionEdit) (app.ItemRevisionResult, error) {
		return app.ItemRevisionResult{}, fmt.Errorf("磁盘已满")
	}
	m.edit.draft.Level = "11"
	m.saveEdit(reviewSave, nil)
	drainAll(t, m, q)
	tt.Frame()
	m.requestResultFilter(resultFilterWarning, "", "")
	tt.Frame()
	if m.navPrompt == nil {
		t.Fatal("no prompt after failed save")
	}
	if err := tt.Click("留在这里"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !m.reviewSaveFailed || !strings.Contains(m.reviewErr, "磁盘已满") {
		t.Fatalf("stay cleared the failure state: %s", probeReviewState(m))
	}
	if !tt.HasText("重试保存") {
		t.Fatalf("stay lost the retry action: %q", tt.Texts())
	}
	assertReviewBinding(t, m)
}

// TestReviewCompactPromptClosesPopover covers L8: the compact list must
// close before the unsaved dialog, so the first dialog click is not eaten.
func TestReviewCompactPromptClosesPopover(t *testing.T) {
	m, _, tt, _ := openReviewableResults(t, manyPendingRecord(t, 3), 960, 640)
	tt.Frame()
	m.compactListOpen = true
	m.edit.draft.Level = "11"
	m.edit.refresh()
	tt.Frame()
	m.requestResultFilter(resultFilterWarning, "", "")
	tt.Frame()
	if m.navPrompt == nil {
		t.Fatal("no compact filter prompt")
	}
	if m.compactListOpen {
		t.Fatalf("the compact popover stayed behind the dialog: %s", probeReviewState(m))
	}
	if !tt.HasText("留在这里") {
		t.Fatalf("the dialog is not visible: %q", tt.Texts())
	}
	assertReviewBinding(t, m)
}

// TestReviewWriteRejectsDisconnectedEditor checks the write guards: a
// stale editor cannot save or confirm another selected row.
func TestReviewWriteRejectsDisconnectedEditor(t *testing.T) {
	m, q, tt, svc := openReviewableResults(t, manyPendingRecord(t, 2), 1180, 760)
	tt.Frame()
	m.edit.draft.Level = "11"
	m.edit.refresh()
	m.selected = 1 // direct break for the probe; the UI paths prompt instead
	m.saveEdit(reviewSave, nil)
	drainAll(t, m, q)
	if got := svc.saveCount(); got != 0 {
		t.Fatalf("a disconnected editor wrote %d saves", got)
	}
	if !strings.Contains(m.reviewErr, "不一致") {
		t.Fatalf("write guard did not explain the mismatch: %q", m.reviewErr)
	}
}

// TestReviewFilterAppliesWhenSelectedStaysVisible checks the protection is
// conditional: a filter that keeps the edited row visible applies without a
// pointless prompt.
func TestReviewFilterAppliesWhenSelectedStaysVisible(t *testing.T) {
	m, _, tt, _ := openReviewableResults(t, pendingReviewRecord(t), 1180, 760)
	tt.Frame()
	m.edit.draft.Level = "11"
	m.edit.refresh()
	tt.Frame()
	if err := tt.Click("全部"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if m.filter != resultFilterAll || m.navPrompt != nil {
		t.Fatalf("visible-row filter prompted or stayed: %s", probeReviewState(m))
	}
	if m.edit == nil || m.edit.draft.Level != "11" {
		t.Fatalf("visible-row filter lost the draft: %s", probeReviewState(m))
	}
	assertReviewBinding(t, m)
}

// TestReviewPageSwitchPromptsDirtyDraft checks destination changes use the
// same guard as filters and selection.
func TestReviewPageSwitchPromptsDirtyDraft(t *testing.T) {
	m, _, tt, _ := openReviewableResults(t, pendingReviewRecord(t), 1180, 760)
	tt.Frame()
	m.edit.draft.Level = "11"
	m.edit.refresh()
	tt.Frame()
	m.navigatePage(pageSettings)
	tt.Frame()
	if m.navPrompt == nil || m.displayPage() != pageResults {
		t.Fatalf("page switch did not prompt: %s", probeReviewState(m))
	}
	if m.edit == nil || m.edit.draft.Level != "11" {
		t.Fatalf("page switch lost the draft: %s", probeReviewState(m))
	}
	assertReviewBinding(t, m)
}
