package ui

import (
	"encoding/json"
	"fmt"
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

// summaryPositionsLedger builds a typed position ledger for the summary
// tests: every position not listed is unreached, as the record contract
// defines it.
func summaryPositionsLedger(sort record.SortVerification, positions ...record.PositionConclusion) *record.PositionLedger {
	return &record.PositionLedger{Sort: sort, Positions: positions}
}

func summaryCompletePositions() []record.PositionConclusion {
	out := make([]record.PositionConclusion, 0, record.PositionCount)
	for i := 1; i <= record.PositionCount; i++ {
		out = append(out, record.PositionConclusion{Position: i, Complete: true})
	}
	return out
}

// summaryView assembles the part of a loaded record the conclusion reads.
func summaryView(rows int, classes app.ItemClassCounts, shell record.SessionShell) *RecordView {
	view := &RecordView{ID: "rec", Classes: classes, Shell: shell}
	for i := 1; i <= rows; i++ {
		view.Rows = append(view.Rows, Row{Seq: int64(i), Status: record.StatusProduced})
	}
	return view
}

func summaryModel(view *RecordView, snap app.Snapshot) *Model {
	m := &Model{snap: snap}
	m.recordWorkspaceState = newRecordWorkspaceState()
	m.rec = view
	return m
}

// TestRecordSummaryConclusionTable covers the six endings of 08 §2.1 plus
// the save-failure state that is promoted to primary and the two honest
// fallbacks (unverified sort only, unknown coverage).
func TestRecordSummaryConclusionTable(t *testing.T) {
	fixed := time.Date(2026, 11, 3, 21, 40, 0, 0, time.Local)
	ended := fixed.Add(14 * time.Minute)
	shell := func(term *record.Termination, coverage record.CoverageStatus, sort record.SortVerification, positions []record.PositionConclusion) record.SessionShell {
		return record.SessionShell{
			StartedAt:      fixed,
			EndedAt:        &ended,
			Termination:    term,
			Coverage:       record.Coverage{Status: coverage},
			PositionLedger: summaryPositionsLedger(sort, positions...),
		}
	}
	completed := &record.Termination{Kind: record.TerminationCompleted, Code: "verifiedEnd", Phase: record.PhaseCapturing}
	failed := &record.Termination{Kind: record.TerminationFailed, Code: "focusLost", Phase: record.PhaseCapturing}
	pageFault := &record.Termination{Kind: record.TerminationFailed, Code: "pageUnverified", Phase: record.PhasePreparing}
	cancelled := &record.Termination{Kind: record.TerminationCancelled, Code: "userCancelled", Phase: record.PhaseCapturing}
	unknownFault := &record.Termination{Kind: record.TerminationFailed, Code: "unknownFault", Phase: record.PhaseCapturing}
	truncatedAt3 := []record.PositionConclusion{
		{Position: 1, Complete: true},
		{Position: 2, Complete: true},
		{Position: 3},
	}

	cases := []struct {
		name           string
		view           *RecordView
		snap           app.Snapshot
		wantTitle      string
		wantPrimary    summaryActionKind
		wantPrimaryLbl string
		wantSupplement string
		wantSecondary  []summaryActionKind
		wantNotes      string
	}{
		{
			name: "complete and exportable",
			view: summaryView(1, app.ItemClassCounts{Passed: 1},
				shell(completed, record.CoverageComplete, record.SortVerificationVerified, summaryCompletePositions())),
			wantTitle:      "扫全了，可以直接导出",
			wantPrimary:    summaryActionExport,
			wantPrimaryLbl: "导出…",
			wantSecondary:  []summaryActionKind{summaryActionViewItems},
		},
		{
			name: "complete with one pending review",
			view: summaryView(1, app.ItemClassCounts{PendingReview: 1},
				shell(completed, record.CoverageComplete, record.SortVerificationVerified, summaryCompletePositions())),
			wantTitle:      "扫全了，还有 1 件待校对",
			wantPrimary:    summaryActionReview,
			wantPrimaryLbl: "校对 1 件",
		},
		{
			name: "partial coverage names incomplete positions and unverified sort",
			view: summaryView(1, app.ItemClassCounts{Passed: 612},
				shell(completed, record.CoveragePartial, record.SortVerificationUnverified, []record.PositionConclusion{
					{Position: 1, Complete: true},
					{Position: 2, Complete: true},
					{Position: 3, Stage: "filter", ReasonCode: "navigationMismatch"},
					{Position: 4, Complete: true},
					{Position: 5, Stage: "bottom"},
					{Position: 6, Complete: true},
				})),
			wantTitle:      "没有扫全：3 号位、5 号位不完整；排序未验证",
			wantPrimary:    summaryActionExport,
			wantPrimaryLbl: "导出…",
			wantSecondary:  []summaryActionKind{summaryActionRescan},
			wantNotes:      "其余 4 个号位完整",
		},
		{
			name: "partial coverage with unverified sort only",
			view: summaryView(1, app.ItemClassCounts{Passed: 1},
				shell(completed, record.CoveragePartial, record.SortVerificationUnverified, summaryCompletePositions())),
			wantTitle:      "没有扫全：排序未验证",
			wantPrimary:    summaryActionExport,
			wantPrimaryLbl: "导出…",
			wantSecondary:  []summaryActionKind{summaryActionRescan},
		},
		{
			name: "manual stop counts completed positions",
			view: summaryView(1, app.ItemClassCounts{Passed: 1},
				shell(cancelled, record.CoveragePartial, record.SortVerificationUnverified, truncatedAt3)),
			wantTitle:      "已手动停止，扫描了 2 个号位",
			wantPrimary:    summaryActionExport,
			wantPrimaryLbl: "导出…",
			wantSecondary:  []summaryActionKind{summaryActionRescan},
		},
		{
			name: "fault names its cause and recovery",
			view: summaryView(1, app.ItemClassCounts{Warning: 1},
				shell(failed, record.CoveragePartial, record.SortVerificationUnverified, truncatedAt3)),
			wantTitle:      "扫描中途停止：游戏失去了焦点",
			wantPrimary:    summaryActionRescan,
			wantPrimaryLbl: "修正后重新扫描",
			wantSupplement: "怎么做：扫描时请不要切换应用。",
			wantSecondary:  []summaryActionKind{summaryActionViewItems, summaryActionCopyDiagnostics},
			wantNotes:      "已保存：1 件（1–2 号位完整，3 号位部分）",
		},
		{
			name: "fault with pending review offers review as secondary",
			view: summaryView(1, app.ItemClassCounts{PendingReview: 1},
				shell(failed, record.CoveragePartial, record.SortVerificationUnverified, truncatedAt3)),
			wantTitle:      "扫描中途停止：游戏失去了焦点",
			wantPrimary:    summaryActionRescan,
			wantPrimaryLbl: "修正后重新扫描",
			wantSupplement: "怎么做：扫描时请不要切换应用。",
			wantSecondary: []summaryActionKind{
				summaryActionReview, summaryActionViewItems, summaryActionCopyDiagnostics,
			},
		},
		{
			name: "zero captured items return to the preparation check",
			view: summaryView(0, app.ItemClassCounts{},
				shell(pageFault, record.CoverageUnknown, record.SortVerificationUnverified, nil)),
			wantTitle:      "没有采集到任何驱动盘：游戏页面不是驱动盘仓库页",
			wantPrimary:    summaryActionPrepare,
			wantPrimaryLbl: "回到准备检查",
			wantSupplement: "怎么做：请打开驱动盘仓库页后重新开始扫描。",
			wantSecondary:  []summaryActionKind{summaryActionCopyDiagnostics},
		},
		{
			name: "zero captured after a normal end does not blame the page",
			view: summaryView(0, app.ItemClassCounts{},
				shell(completed, record.CoverageComplete, record.SortVerificationVerified, summaryCompletePositions())),
			wantTitle:      "没有采集到任何驱动盘",
			wantPrimary:    summaryActionPrepare,
			wantPrimaryLbl: "回到准备检查",
			wantSupplement: "回到准备检查后重新开始。",
		},
		{
			name: "zero captured with an unknown fault does not leave an empty action",
			view: summaryView(0, app.ItemClassCounts{},
				shell(unknownFault, record.CoverageUnknown, record.SortVerificationUnknown, nil)),
			wantTitle:      "没有采集到任何驱动盘",
			wantPrimary:    summaryActionPrepare,
			wantPrimaryLbl: "回到准备检查",
			wantSupplement: "回到准备检查后重新开始。",
			wantSecondary:  []summaryActionKind{summaryActionCopyDiagnostics},
		},
		{
			name: "zero captured after a manual stop",
			view: summaryView(0, app.ItemClassCounts{},
				shell(cancelled, record.CoveragePartial, record.SortVerificationUnverified, truncatedAt3)),
			wantTitle:      "已手动停止，没有采集到任何驱动盘",
			wantPrimary:    summaryActionPrepare,
			wantPrimaryLbl: "回到准备检查",
			wantSupplement: "回到准备检查后重新开始。",
		},
		{
			name: "interrupted record offers rescan",
			view: func() *RecordView {
				view := summaryView(1, app.ItemClassCounts{Passed: 1},
					shell(nil, record.CoveragePartial, record.SortVerificationUnverified, truncatedAt3))
				view.Interrupted = true
				return view
			}(),
			wantTitle:      "扫描中断，只保存了部分结果",
			wantPrimary:    summaryActionExport,
			wantPrimaryLbl: "导出…",
			wantSecondary:  []summaryActionKind{summaryActionViewItems, summaryActionRescan},
		},
		{
			name: "scan still running returns to the scan page",
			view: func() *RecordView {
				view := summaryView(0, app.ItemClassCounts{},
					shell(completed, record.CoverageComplete, record.SortVerificationVerified, summaryCompletePositions()))
				view.Live = true
				return view
			}(),
			snap:           app.Snapshot{Busy: true},
			wantTitle:      "扫描还没有结束",
			wantPrimary:    summaryActionPrepare,
			wantPrimaryLbl: "返回扫描页",
		},
		{
			name: "save failure is the primary state",
			view: func() *RecordView {
				view := summaryView(1, app.ItemClassCounts{Passed: 1},
					shell(completed, record.CoverageComplete, record.SortVerificationVerified, summaryCompletePositions()))
				view.Live = true
				return view
			}(),
			snap:           app.Snapshot{Busy: true, SaveError: "磁盘已满"},
			wantTitle:      "扫描结果还没有保存",
			wantPrimary:    summaryActionRetrySave,
			wantPrimaryLbl: "重试保存",
			wantSupplement: "保存失败，请重试",
			wantSecondary:  []summaryActionKind{summaryActionViewItems},
		},
		{
			name: "save failure wins over zero captured items",
			view: func() *RecordView {
				view := summaryView(0, app.ItemClassCounts{},
					shell(pageFault, record.CoverageUnknown, record.SortVerificationUnverified, nil))
				view.Live = true
				return view
			}(),
			snap:           app.Snapshot{Busy: true, SaveError: "磁盘已满"},
			wantTitle:      "扫描结果还没有保存",
			wantPrimary:    summaryActionRetrySave,
			wantPrimaryLbl: "重试保存",
			wantSupplement: "保存失败，请重试",
		},
		{
			name: "history record ignores the previous session save error",
			view: summaryView(1, app.ItemClassCounts{Passed: 1},
				shell(completed, record.CoverageComplete, record.SortVerificationVerified, summaryCompletePositions())),
			snap:           app.Snapshot{Busy: true, SaveError: "磁盘已满"},
			wantTitle:      "扫全了，可以直接导出",
			wantPrimary:    summaryActionExport,
			wantPrimaryLbl: "导出…",
			wantSecondary:  []summaryActionKind{summaryActionViewItems},
		},
		{
			name: "pending review and backfill both appear",
			view: summaryView(3, app.ItemClassCounts{PendingReview: 1, PendingBackfill: 1, Warning: 1},
				shell(completed, record.CoverageComplete, record.SortVerificationVerified, summaryCompletePositions())),
			wantTitle:      "扫全了，还有 1 件待校对、1 件待补录",
			wantPrimary:    summaryActionReview,
			wantPrimaryLbl: "校对 1 件",
			wantSecondary:  []summaryActionKind{summaryActionBackfill, summaryActionExport},
		},
		{
			name: "unknown coverage is not rounded to complete",
			view: summaryView(1, app.ItemClassCounts{Passed: 1},
				shell(completed, record.CoverageUnknown, record.SortVerificationUnknown, nil)),
			wantTitle:      "扫描结束，无法确认是否扫全",
			wantPrimary:    summaryActionExport,
			wantPrimaryLbl: "导出…",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := summaryModel(tc.view, tc.snap)
			m.cfg.DirectExportSaver = &fakeDirectExportSaver{}
			got := m.recordSummaryConclusion()
			if got.Title != tc.wantTitle {
				t.Errorf("title = %q, want %q", got.Title, tc.wantTitle)
			}
			if got.Primary.Kind != tc.wantPrimary || got.Primary.Label != tc.wantPrimaryLbl {
				t.Errorf("primary = %v/%q, want %v/%q",
					got.Primary.Kind, got.Primary.Label, tc.wantPrimary, tc.wantPrimaryLbl)
			}
			if !strings.Contains(got.Supplement, tc.wantSupplement) {
				t.Errorf("supplement = %q, want it to contain %q", got.Supplement, tc.wantSupplement)
			}
			if tc.wantNotes != "" && !strings.Contains(strings.Join(got.Notes, "\n"), tc.wantNotes) {
				t.Errorf("notes = %q, want them to contain %q", got.Notes, tc.wantNotes)
			}
			var gotSecondary []summaryActionKind
			for _, action := range got.Secondary {
				gotSecondary = append(gotSecondary, action.Kind)
			}
			if !slices.Equal(gotSecondary, tc.wantSecondary) {
				t.Errorf("secondary = %v, want %v", gotSecondary, tc.wantSecondary)
			}
		})
	}
}

// summaryStructuredItem builds a produced item whose capture state lets the
// classifier reach warning or passed instead of the default pending-review
// state of a record without management/equipment evidence.
func summaryStructuredItem(t *testing.T, seq int64, quality record.Quality, state zzz.CaptureState) record.ItemRecord {
	t.Helper()
	disc := zzz.DiscRecord{
		SetID: intp(31100), Slot: 4, Level: intp(15),
		MainStat: zzz.Stat{Attribute: zzz.AttrCritRate, Value: "24", Unit: zzz.UnitPercent},
		Substats: []zzz.Substat{
			{Stat: zzz.Stat{Attribute: zzz.AttrCritDamage, Value: "19.2", Unit: zzz.UnitPercent}, EnhancementRolls: intp(3)},
			{Stat: zzz.Stat{Attribute: zzz.AttrAttack, Value: "38", Unit: zzz.UnitFlat}, EnhancementRolls: intp(1)},
			{Stat: zzz.Stat{Attribute: zzz.AttrHealth, Value: "112", Unit: zzz.UnitFlat}, EnhancementRolls: intp(0)},
			{Stat: zzz.Stat{Attribute: zzz.AttrAnomalyProficiency, Value: "9", Unit: zzz.UnitFlat}, EnhancementRolls: intp(0)},
		},
	}
	it := item(seq, record.StatusProduced, "", "", fmt.Sprintf("证据 %d", seq))
	structured := zzz.ToStructuredWithState(disc, zzz.RarityS, state, nil)
	raw, err := json.Marshal(structured)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(it.Data, &doc); err != nil {
		t.Fatal(err)
	}
	doc["structured"] = raw
	it.Data, err = json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	it.Quality = quality
	return it
}

// summaryExportActionRecord is a complete, exportable record for the
// summary export-action state matrix.
func summaryExportActionRecord(t *testing.T) *record.Recovery {
	t.Helper()
	rec := recovery(summaryStructuredItem(t, 1, record.QualityAutoAccepted,
		zzz.CaptureState{ManagementState: zzz.ManagementNone, EquippedState: zzz.EquippedStateUnequipped}))
	rec.Checkpoint.Session.Coverage = record.Coverage{Status: record.CoverageComplete}
	rec.Checkpoint.Session.ObservedScope = []byte(`{"sort":"verified"}`)
	rec.Checkpoint.Session.PositionLedger = summaryPositionsLedger(
		record.SortVerificationVerified, summaryCompletePositions()...)
	return rec
}

// summaryFourClassRecord has one item of every class, so the count cells
// and their destinations can be verified together.
func summaryFourClassRecord(t *testing.T) *record.Recovery {
	t.Helper()
	none := zzz.CaptureState{ManagementState: zzz.ManagementNone, EquippedState: zzz.EquippedStateUnequipped}
	equipped := zzz.CaptureState{ManagementState: zzz.ManagementNone, EquippedState: zzz.EquippedStateEquipped}
	rec := recovery(
		item(1, record.StatusFailed, "", "识别超时", "失败"),
		summaryStructuredItem(t, 2, record.QualityNeedsReview, none),
		summaryStructuredItem(t, 3, record.QualityAutoAccepted, equipped),
		summaryStructuredItem(t, 4, record.QualityAutoAccepted, none),
	)
	rec.Checkpoint.Session.Coverage = record.Coverage{Status: record.CoverageComplete}
	rec.Checkpoint.Session.ObservedScope = []byte(`{"sort":"verified"}`)
	rec.Checkpoint.Session.PositionLedger = summaryPositionsLedger(
		record.SortVerificationVerified, summaryCompletePositions()...)
	return rec
}

// openSummaryRecord opens one saved record on its summary view (rather than
// its item browser) at the requested window size.
func openSummaryRecord(t *testing.T, rec *record.Recovery, cfg Config, width, height int) (*Model, *queueUpdater, *ui.Tester, *fakeService) {
	t.Helper()
	svc := &fakeService{
		list: []record.RecordInfo{{
			ID: "rec", LatestRevision: 1,
			SavedAt: time.Date(2026, 11, 3, 21, 40, 0, 0, time.Local),
		}},
		loads: map[string]*record.Recovery{"rec": rec},
	}
	cfg.Service = svc
	m, q := newTestModel(t, cfg)
	m.SetContentSize(width, height)
	tt := ui.NewTester(m.View, width, height)
	settle(t, m, q, tt)
	m.histChoice = historyLabel(t, m, "rec")
	m.OpenHistory()
	settle(t, m, q, tt)
	return m, q, tt, svc
}

// TestSummaryExportActionOpensTheOnlyDialog checks the summary's export
// action routes to the same export dialog as the title bar and record list,
// instead of the removed direct export controls.
func TestSummaryExportActionOpensTheOnlyDialog(t *testing.T) {
	m, _, tt, _ := openSummaryRecord(t, summaryFourClassRecord(t),
		Config{DirectExportSaver: &fakeDirectExportSaver{}}, 1180, 760)
	if err := tt.Click("先导出可导出的 2 件…"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !m.exportDialog.open {
		t.Fatalf("the summary export action did not open the dialog: %q", tt.Texts())
	}
	if !tt.HasText("导出这条记录") || !tt.Focused("导出 JSON") {
		t.Fatalf("the summary opened something other than the export dialog: %q", tt.Texts())
	}
}

// TestSummaryExportActionMatrix checks 09 §4.1: only a dirty draft keeps
// the summary's export action clickable; every other blocked state disables
// it with a reason beside it.
func TestSummaryExportActionMatrix(t *testing.T) {
	cases := []struct {
		name         string
		live         bool
		setup        func(*exportDialogFixture)
		wantDisabled bool
		wantReason   string
	}{
		{name: "ready"},
		{
			name: "dirty draft",
			setup: func(f *exportDialogFixture) {
				showRecordItems(f.m)
				f.m.selected = 0
				f.m.startEdit()
				f.m.edit.draft.Level = "14"
			},
		},
		{
			name: "build error",
			setup: func(f *exportDialogFixture) {
				f.m.exportData = nil
				f.m.exportBuildErr = "boom"
			},
			wantDisabled: true,
			wantReason:   "无法生成导出：boom",
		},
		{
			name: "empty manifest",
			setup: func(f *exportDialogFixture) {
				f.m.exportData = nil
			},
			wantDisabled: true,
			wantReason:   "导出清单尚未就绪",
		},
		{
			name: "loading",
			setup: func(f *exportDialogFixture) {
				f.m.loading = true
			},
			wantDisabled: true,
			wantReason:   "正在加载最新检查点",
		},
		{
			name: "review saving",
			setup: func(f *exportDialogFixture) {
				f.m.reviewSaving = true
			},
			wantDisabled: true,
			wantReason:   "正在保存修改",
		},
		{
			name: "exit running",
			setup: func(f *exportDialogFixture) {
				f.m.exitState = exitRunning
			},
			wantDisabled: true,
			wantReason:   "正在退出应用",
		},
		{
			name: "exporting",
			setup: func(f *exportDialogFixture) {
				f.m.exporting = true
			},
			wantDisabled: true,
			wantReason:   "正在导出",
		},
		{
			name: "no saver",
			setup: func(f *exportDialogFixture) {
				f.m.cfg.DirectExportSaver = nil
			},
			wantDisabled: true,
			wantReason:   "当前构建未启用导出",
		},
		{
			name: "live busy",
			live: true,
			setup: func(f *exportDialogFixture) {
				f.m.snap.Busy = true
			},
			wantDisabled: true,
			wantReason:   "扫描尚未收尾",
		},
		{
			name: "live save pending",
			live: true,
			setup: func(f *exportDialogFixture) {
				f.m.snap.SavePending = true
			},
			wantDisabled: true,
			wantReason:   "扫描结果尚未保存",
		},
		{
			name: "live save error",
			live: true,
			setup: func(f *exportDialogFixture) {
				f.m.snap.SaveError = "disk"
			},
			wantDisabled: true,
			wantReason:   "扫描结果尚未保存",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newExportDialogFixture(t, summaryExportActionRecord(t), tc.live)
			if tc.setup != nil {
				tc.setup(f)
			}
			action := f.m.exportAction()
			if action.Kind != summaryActionExport {
				t.Fatalf("action kind = %v, want export", action.Kind)
			}
			if got := action.disabled(); got != tc.wantDisabled {
				t.Fatalf("disabled = %t (reason %q), want %t",
					got, action.DisabledReason, tc.wantDisabled)
			}
			if tc.wantReason != "" && !strings.Contains(action.DisabledReason, tc.wantReason) {
				t.Fatalf("reason = %q, want %q", action.DisabledReason, tc.wantReason)
			}
		})
	}
}

// TestSummaryExportActionKeepsDirtyDraftClickable checks the summary uses
// the same entry guard as the title bar: a dirty draft must reach the
// dialog's save/discard prompt instead of disabling the action.
func TestSummaryExportActionKeepsDirtyDraftClickable(t *testing.T) {
	f := newExportDialogFixture(t, recovery(uiStructuredItem(t, 1, record.QualityAutoAccepted)), false)
	showRecordItems(f.m)
	f.m.selected = 0
	f.m.startEdit()
	if f.m.edit == nil {
		t.Fatal("the editor did not open")
	}
	f.m.edit.draft.Level = "14"
	if action := f.m.exportAction(); action.disabled() {
		t.Fatalf("a dirty draft disabled the summary export action: %q", action.DisabledReason)
	}
}

// TestViewSummaryCountsOpenFilteredItems checks the four count cells open
// the item browser on the clicked class and select its first item. The
// existing segments are used until PR-14b adds the four-class filters;
// warning and passed still select the first matching row in 全部.
func TestViewSummaryCountsOpenFilteredItems(t *testing.T) {
	m, q, tt, _ := openSummaryRecord(t, summaryFourClassRecord(t),
		Config{DirectExportSaver: &fakeDirectExportSaver{}}, 1180, 760)

	if !tt.HasText("扫全了，还有 1 件待校对、1 件待补录") {
		t.Fatalf("the four-class conclusion is missing: %q", tt.Texts())
	}
	if !tt.HasText("覆盖：全部") {
		t.Fatalf("the header does not use the 09 §5.2 coverage word: %q", tt.Texts())
	}
	if !tt.HasText("正常结束") {
		t.Fatalf("the normal-ending eyebrow is missing: %q", tt.Texts())
	}
	if tt.HasText("失败 0 件") {
		t.Fatalf("the summary still repeats the old export count line: %q", tt.Texts())
	}
	cases := []struct {
		cell   string
		filter resultFilter
		class  app.ItemClass
	}{
		{"待校对 ›", resultFilterPending, app.ItemClassPendingReview},
		{"有警告但可导出 ›", resultFilterWarning, app.ItemClassWarning},
		{"已通过 ›", resultFilterAll, app.ItemClassPassed},
		{"待补录 ›", resultFilterPending, app.ItemClassPendingBackfill},
	}
	for _, tc := range cases {
		t.Run(tc.cell, func(t *testing.T) {
			m.recordTab = recordTabSummary
			tt.Frame()
			if err := tt.Click(tc.cell); err != nil {
				t.Fatal(err)
			}
			settle(t, m, q, tt)
			if m.recordTab != recordTabItems {
				t.Fatalf("clicking %q left tab %v", tc.cell, m.recordTab)
			}
			if m.filter != tc.filter {
				t.Fatalf("clicking %q chose filter %v, want %v", tc.cell, m.filter, tc.filter)
			}
			if m.rec == nil || m.selected < 0 || m.selected >= len(m.rec.Rows) {
				t.Fatalf("clicking %q selected no item", tc.cell)
			}
			if got := selectedSummaryClass(t, m); got != tc.class {
				t.Fatalf("clicking %q selected class %q, want %q", tc.cell, got, tc.class)
			}
		})
	}
}

// TestSummaryDisclosuresFollowTheOpenRecord checks that the summary's
// disclosure state belongs to the record workspace: revisiting the same
// record keeps it, while another record starts with both disclosures
// collapsed and no stale diagnostics error.
func TestSummaryDisclosuresFollowTheOpenRecord(t *testing.T) {
	m := summaryModel(summaryView(1, app.ItemClassCounts{Passed: 1}, record.SessionShell{}), app.Snapshot{})
	m.summaryDetailsOpen = true
	m.summaryDiagnosticsOpen = true
	m.summaryDiagnosticsErr = "旧错误"

	m.openRecord("rec")
	if !m.summaryDetailsOpen || !m.summaryDiagnosticsOpen || m.summaryDiagnosticsErr != "旧错误" {
		t.Fatalf("revisiting the same record reset the summary: %+v", m.recordWorkspaceState)
	}

	m.showRecord("another", false)
	if m.summaryDetailsOpen || m.summaryDiagnosticsOpen || m.summaryDiagnosticsErr != "" {
		t.Fatalf("another record inherited the summary state: %+v", m.recordWorkspaceState)
	}
}

// TestViewSummaryCountClearsItemBrowserFilters checks that a count cell does
// not inherit a search or set/slot filter that would hide the class it
// promises to open.
func TestViewSummaryCountClearsItemBrowserFilters(t *testing.T) {
	m, q, tt, _ := openSummaryRecord(t, summaryFourClassRecord(t),
		Config{DirectExportSaver: &fakeDirectExportSaver{}}, 1180, 760)
	m.setFilter = "99999"
	m.slotFilter = "9"
	tt.Frame()

	if err := tt.Click("待校对 ›"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if m.setFilter != "" || m.slotFilter != "" {
		t.Fatalf("count click kept item filters: set=%q slot=%q", m.setFilter, m.slotFilter)
	}
	if m.filter != resultFilterPending || m.recordTab != recordTabItems {
		t.Fatalf("count click opened tab %v filter %v", m.recordTab, m.filter)
	}
	if got := selectedSummaryClass(t, m); got != app.ItemClassPendingReview {
		t.Fatalf("count click selected class %q", got)
	}
}

// TestSummaryAttentionCell pins the wireframe 05 text-plus-border rule: only
// the pending classes with a non-zero count get the strong border.
func TestSummaryAttentionCell(t *testing.T) {
	m := &Model{}
	if !m.summaryCellAttention(app.ItemClassPendingReview, app.ItemClassCounts{PendingReview: 1}) {
		t.Fatal("pending review with items is not marked")
	}
	if !m.summaryCellAttention(app.ItemClassPendingBackfill, app.ItemClassCounts{PendingBackfill: 1}) {
		t.Fatal("pending backfill with items is not marked")
	}
	if m.summaryCellAttention(app.ItemClassWarning, app.ItemClassCounts{Warning: 1}) {
		t.Fatal("exportable warning is marked as needing work")
	}
	if m.summaryCellAttention(app.ItemClassPassed, app.ItemClassCounts{Passed: 1}) {
		t.Fatal("passed is marked as needing work")
	}
}

// selectedSummaryClass classifies the item the item browser selected.
func selectedSummaryClass(t *testing.T, m *Model) app.ItemClass {
	t.Helper()
	if m.rec == nil || m.recData == nil || m.selected < 0 || m.selected >= len(m.rec.Rows) {
		t.Fatalf("the summary/opened record cannot name a selected item")
	}
	seq := m.rec.Rows[m.selected].Seq
	for i := range m.recData.Checkpoint.Items {
		if m.recData.Checkpoint.Items[i].Sequence == seq {
			return app.ClassifyItem(m.recData.Checkpoint.Items[i])
		}
	}
	t.Fatalf("the selected row %d has no checkpoint item", seq)
	return ""
}

// TestViewSummarySaveFailureRetries checks the save-failure state promoted to
// the conclusion: the retry reaches the service.
func TestViewSummarySaveFailureRetries(t *testing.T) {
	rec := recovery(structuredItem(1, zzz.DiscRecord{SetID: intp(31100), Slot: 1, Level: intp(15)}, "静听嘉音"))
	svc := &fakeService{
		list:  []record.RecordInfo{{ID: "s1", LatestRevision: 1, SavedAt: time.Now()}},
		loads: map[string]*record.Recovery{"s1": rec},
	}
	m, q := newTestModel(t, Config{Service: svc, DirectExportSaver: &fakeDirectExportSaver{}})
	tt := ui.NewTester(m.View, 1180, 760)
	settle(t, m, q, tt)

	applyAndSettle(t, m, q, tt, snap("s1", 1, true, scan.PhaseCapturing,
		scan.Counts{Captured: 1, Produced: 1}, 1, false, ""))
	applyAndSettle(t, m, q, tt, snap("s1", 2, true, scan.PhaseEnded,
		scan.Counts{Captured: 1, Produced: 1}, 1, false, "磁盘已满"))
	m.openRecordWorkspace()
	tt.Frame()

	if !tt.HasText("扫描结果还没有保存") || !tt.HasText("保存失败，请重试。若仍然失败，请检查磁盘空间和权限。") {
		t.Fatalf("the save failure is not the summary's conclusion: %q", tt.Texts())
	}
	if tt.HasText("磁盘已满") {
		t.Fatalf("the raw save error leaked into the page-level conclusion: %q", tt.Texts())
	}
	if err := tt.Click("重试保存"); err != nil {
		t.Fatal(err)
	}
	drainAll(t, m, q)
	if svc.retries != 1 {
		t.Fatalf("the summary retry issued %d retries", svc.retries)
	}
}

// TestViewSummaryZeroItemsReturnsToPrepare checks the zero-item ending's main
// action.
func TestViewSummaryZeroItemsReturnsToPrepare(t *testing.T) {
	rec := recovery()
	rec.Checkpoint.Session.Termination = &record.Termination{
		Kind: record.TerminationFailed, Code: "pageUnverified", Phase: record.PhasePreparing,
	}
	rec.Checkpoint.Session.Coverage = record.Coverage{Status: record.CoverageUnknown}
	m, q, tt, _ := openSummaryRecord(t, rec,
		Config{DirectExportSaver: &fakeDirectExportSaver{}}, 1180, 760)

	if !tt.HasText("没有采集到任何驱动盘：游戏页面不是驱动盘仓库页") ||
		!tt.HasText("回到准备检查") {
		t.Fatalf("the zero-item conclusion is missing: %q", tt.Texts())
	}
	if err := tt.Click("回到准备检查"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if m.displayPage() != pageScan {
		t.Fatalf("回到准备检查 stayed on page %v", m.displayPage())
	}
}

// TestViewSummaryDiagnosticsCopyAndOpen checks the fault diagnostics
// disclosure: the code appears only inside it, copy writes the
// privacy-bounded text, and the directory action reaches the configured
// opener.
func TestViewSummaryDiagnosticsCopyAndOpen(t *testing.T) {
	rec := recovery(structuredItem(1, zzz.DiscRecord{SetID: intp(31100), Slot: 1, Level: intp(15)}, "静听嘉音"))
	rec.Checkpoint.Session.Termination = &record.Termination{
		Kind: record.TerminationFailed, Code: "focusLost", Phase: record.PhaseCapturing,
	}
	rec.Checkpoint.Session.Coverage = record.Coverage{Status: record.CoveragePartial}
	rec.Checkpoint.Session.EndedAt = timePtr(time.Date(2026, 11, 3, 21, 52, 7, 0, time.Local))

	opened := 0
	m, q, tt, _ := openSummaryRecord(t, rec, Config{
		DirectExportSaver: &fakeDirectExportSaver{},
		AppVersion:        "0.4.0",
		SystemInfo:        func() app.SystemInfo { return app.SystemInfo{OS: "Windows", OSVersion: "10.0"} },
		OpenDiagnosticsDirectory: func() error {
			opened++
			return nil
		},
	}, 1180, 900)

	if tt.HasText("故障码：focusLost") {
		t.Fatalf("the fault code shows before the diagnostics section opens: %q", tt.Texts())
	}
	if err := tt.Click("详情"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	tt.Advance(300 * time.Millisecond)
	tt.Frame()
	if err := tt.Click("诊断信息"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	tt.Advance(300 * time.Millisecond)
	tt.Frame()
	if !tt.HasText("故障码：focusLost") {
		t.Fatalf("the expanded diagnostics do not name the fault code: %q", tt.Texts())
	}
	if err := tt.Click("复制诊断信息"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if got := tt.Clipboard(); !strings.Contains(got, "youkai 诊断信息") ||
		!strings.Contains(got, "focusLost") || !strings.Contains(got, "0.4.0") {
		t.Fatalf("the copied diagnostics are incomplete: %q", got)
	}

	clickRevealedSettingsControl(t, tt, 900, "打开诊断目录")
	settle(t, m, q, tt)
	if opened != 1 {
		t.Fatalf("打开诊断目录 called the opener %d times", opened)
	}
	if m.summaryDiagnosticsErr != "" {
		t.Fatalf("opening the diagnostics directory failed: %q", m.summaryDiagnosticsErr)
	}
}

// TestViewSummaryDetailsShowPositionConclusion checks the detail area names
// positions from the typed ledger, including an incomplete slot's localized
// cause and the unverified sort, without leaking the raw code.
func TestViewSummaryDetailsShowPositionConclusion(t *testing.T) {
	rec := recovery(structuredItem(1, zzz.DiscRecord{SetID: intp(31100), Slot: 1, Level: intp(15)}, "静听嘉音"))
	rec.Checkpoint.Session.Coverage = record.Coverage{Status: record.CoveragePartial}
	rec.Checkpoint.Session.PositionLedger = summaryPositionsLedger(
		record.SortVerificationUnverified,
		record.PositionConclusion{Position: 1, Complete: true},
		record.PositionConclusion{Position: 2, Stage: "filter", ReasonCode: "navigationMismatch"},
	)
	m, q, tt, _ := openSummaryRecord(t, rec,
		Config{DirectExportSaver: &fakeDirectExportSaver{}}, 1180, 900)

	if err := tt.Click("详情"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	tt.Advance(300 * time.Millisecond)
	tt.Frame()
	for _, want := range []string{
		"1 号位：完整",
		"2 号位：不完整，筛选准备失败：点击或拖拽后页面没有按预期变化",
		"3 号位：未扫描",
		"排序：未验证",
		"覆盖：部分",
	} {
		if !tt.HasText(want) {
			t.Fatalf("the detail area does not show %q: %q", want, tt.Texts())
		}
	}
	if tt.HasText("navigationMismatch") {
		t.Fatalf("the detail area leaks the raw code: %q", tt.Texts())
	}
}

func timePtr(t time.Time) *time.Time { return &t }

// TestViewSummaryEndings renders each of 08 §2.1's six endings and checks
// that the conclusion title and its main button reach the screen. The
// pure conclusion table above pins the model; this test pins the view.
func TestViewSummaryEndings(t *testing.T) {
	none := zzz.CaptureState{ManagementState: zzz.ManagementNone, EquippedState: zzz.EquippedStateUnequipped}
	completed := &record.Termination{Kind: record.TerminationCompleted, Code: "verifiedEnd", Phase: record.PhaseCapturing}
	failed := &record.Termination{Kind: record.TerminationFailed, Code: "focusLost", Phase: record.PhaseCapturing}
	pageFault := &record.Termination{Kind: record.TerminationFailed, Code: "pageUnverified", Phase: record.PhasePreparing}
	cancelled := &record.Termination{Kind: record.TerminationCancelled, Code: "userCancelled", Phase: record.PhaseCapturing}
	truncatedAt3 := []record.PositionConclusion{
		{Position: 1, Complete: true},
		{Position: 2, Complete: true},
		{Position: 3},
	}
	recordWith := func(term *record.Termination, coverage record.CoverageStatus, sort record.SortVerification, positions []record.PositionConclusion, items int) *record.Recovery {
		rec := recovery()
		for i := 1; i <= items; i++ {
			rec.Checkpoint.Items = append(rec.Checkpoint.Items, summaryStructuredItem(t, int64(i), record.QualityAutoAccepted, none))
		}
		rec.Checkpoint.Session.Termination = term
		rec.Checkpoint.Session.Coverage = record.Coverage{Status: coverage}
		rec.Checkpoint.Session.ObservedScope = []byte(fmt.Sprintf(`{"sort":%q}`, sort))
		rec.Checkpoint.Session.PositionLedger = summaryPositionsLedger(sort, positions...)
		rec.Checkpoint.Session.CapturedCount = int64(items)
		return rec
	}

	cases := []struct {
		name    string
		rec     *record.Recovery
		title   string
		primary string
	}{
		{"complete and exportable", recordWith(completed, record.CoverageComplete, record.SortVerificationVerified, summaryCompletePositions(), 1),
			"扫全了，可以直接导出", "导出…"},
		{"complete with pending", recordWith(completed, record.CoverageComplete, record.SortVerificationVerified, summaryCompletePositions(), 1),
			"扫全了，还有 1 件待校对", "校对 1 件"},
		{"partial coverage", recordWith(completed, record.CoveragePartial, record.SortVerificationUnverified, []record.PositionConclusion{
			{Position: 1, Complete: true}, {Position: 2, Complete: true},
			{Position: 3, Stage: "filter", ReasonCode: "navigationMismatch"},
			{Position: 4, Complete: true}, {Position: 5, Stage: "bottom"}, {Position: 6, Complete: true},
		}, 1), "没有扫全：3 号位、5 号位不完整；排序未验证", "导出…"},
		{"manual stop", recordWith(cancelled, record.CoveragePartial, record.SortVerificationUnverified, truncatedAt3, 1),
			"已手动停止，扫描了 2 个号位", "导出…"},
		{"fault", recordWith(failed, record.CoveragePartial, record.SortVerificationUnverified, truncatedAt3, 1),
			"扫描中途停止：游戏失去了焦点", "修正后重新扫描"},
		{"zero items", recordWith(pageFault, record.CoverageUnknown, record.SortVerificationUnverified, nil, 0),
			"没有采集到任何驱动盘：游戏页面不是驱动盘仓库页", "回到准备检查"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "complete with pending" {
				// The one produced item is auto-accepted; make it pending
				// review so the pending branch is the one under test.
				rec := tc.rec
				rec.Checkpoint.Items[0].Quality = record.QualityNeedsReview
			}
			_, _, tt, _ := openSummaryRecord(t, tc.rec,
				Config{DirectExportSaver: &fakeDirectExportSaver{}}, 1180, 900)
			if !tt.HasText(tc.title) {
				t.Fatalf("the summary title %q is missing: %q", tc.title, tt.Texts())
			}
			if !tt.HasText(tc.primary) {
				t.Fatalf("the main action %q is missing: %q", tc.primary, tt.Texts())
			}
		})
	}
}
