package ui

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
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
	if !tt.HasText("输入已保留。") {
		t.Fatalf("the failure band is missing: %q", tt.Texts())
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
