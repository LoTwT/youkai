package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/game/zzz"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/LoTwT/youkai/internal/scan"
	"github.com/egoist/mygo/ui"
)

func cleanupPreview() app.CleanupPreview {
	return app.CleanupPreview{
		Records:       1,
		EvidenceFiles: 2,
		EvidenceBytes: 2048,
		TotalBytes:    4096,
		PendingReview: 1,
	}
}

func cleanupPreviewService(list []record.RecordInfo, loads map[string]*record.Recovery) *fakeService {
	return &fakeService{
		list:  list,
		loads: loads,
		previewFn: func(kind, recordID string) (app.CleanupPreview, error) {
			return cleanupPreview(), nil
		},
	}
}

func TestViewSavePromptOffersKeepDeleteLater(t *testing.T) {
	live := recovery(structuredItem(1, zzz.DiscRecord{SetID: intp(31100), Slot: 1, Level: intp(15)}, "静听嘉音"))
	second := recovery(structuredItem(1, zzz.DiscRecord{SetID: intp(31200), Slot: 2, Level: intp(15)}, "啄木鸟电音"))
	svc := cleanupPreviewService(
		[]record.RecordInfo{{ID: "live", LatestRevision: 1, SavedAt: time.Now()}},
		map[string]*record.Recovery{"live": live, "live2": second},
	)
	m, q := newTestModel(t, Config{Service: svc, ExportSaver: &fakeExportSaver{}})
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)

	applyAndSettle(t, m, q, tt, snap("live", 1, true, scan.PhaseCapturing,
		scan.Counts{Captured: 1, Produced: 1}, 1, false, ""))
	applyAndSettle(t, m, q, tt, snap("live", 2, false, scan.PhaseEnded,
		scan.Counts{Captured: 1, Produced: 1}, 1, false, ""))

	want := "本次扫描已保存：截图 2 张 · 2.0 KB · 待校对 1 项。"
	if !tt.HasText(want) {
		t.Fatalf("the save-completion prompt does not show %q: %q", want, tt.Texts())
	}
	for _, label := range []string{"保留截图", "删除截图", "稍后处理"} {
		if !tt.HasText(label) {
			t.Errorf("the prompt does not offer %q", label)
		}
	}
	if err := tt.Click("保留截图"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if m.savePrompt != nil {
		t.Fatal("保留截图 did not dismiss the prompt")
	}
	if screenshots, all, records, allRecords := svc.cleanupCalls(); len(screenshots) != 0 || all != 0 || len(records) != 0 || allRecords != 0 {
		t.Fatalf("保留截图 deleted something: %v %d %v %d", screenshots, all, records, allRecords)
	}

	// A second session offers the choice again; deleting its screenshots
	// calls the service and reports the measured result.
	applyAndSettle(t, m, q, tt, snap("live2", 3, true, scan.PhaseCapturing,
		scan.Counts{Captured: 1, Produced: 1}, 1, false, ""))
	applyAndSettle(t, m, q, tt, snap("live2", 4, false, scan.PhaseEnded,
		scan.Counts{Captured: 1, Produced: 1}, 1, false, ""))
	if !tt.HasText(want) {
		t.Fatalf("the second save prompt does not show: %q", tt.Texts())
	}
	if err := tt.Click("删除截图"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	screenshots, _, _, _ := svc.cleanupCalls()
	if len(screenshots) != 1 || screenshots[0] != "live2" {
		t.Fatalf("screenshot deletions = %v, want live2", screenshots)
	}
	if m.savePrompt != nil {
		t.Fatal("删除截图 did not dismiss the prompt")
	}
	if !tt.HasText("已删除本次扫描的 2 张截图（2.0 KB），结果与校对状态保留。") {
		t.Fatalf("the screenshot deletion result does not show: %q", tt.Texts())
	}
}

func TestViewSavePromptNotShownAfterFailedSave(t *testing.T) {
	svc := cleanupPreviewService(nil, map[string]*record.Recovery{})
	m, q := newTestModel(t, Config{Service: svc, ExportSaver: &fakeExportSaver{}})
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)

	applyAndSettle(t, m, q, tt, snap("live", 1, true, scan.PhaseCapturing,
		scan.Counts{Captured: 1}, 0, false, ""))
	applyAndSettle(t, m, q, tt, snap("live", 2, false, scan.PhaseEnded,
		scan.Counts{Captured: 1}, 0, true, "磁盘已满"))
	if m.savePrompt != nil || tt.HasText("保留截图") {
		t.Fatalf("a failed save opened the cleanup prompt: %q", tt.Texts())
	}
}

func TestViewHistoryScreenshotCleanupPreviewsCancelAndFails(t *testing.T) {
	svc := cleanupPreviewService(
		[]record.RecordInfo{{ID: "r1", LatestRevision: 2, SavedAt: time.Now()}},
		map[string]*record.Recovery{"r1": testRecord()},
	)
	m, q := newTestModel(t, Config{Service: svc, ExportSaver: &fakeExportSaver{}})
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)
	m.OpenHistory()
	settle(t, m, q, tt)

	if err := tt.Click("删除截图"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	prompt := "将删除记录 r1 的 2 张截图（2.0 KB），保留结果、修订与校对状态；待校对 1 项。"
	if !tt.HasText(prompt) {
		t.Fatalf("the preview does not show %q: %q", prompt, tt.Texts())
	}
	if err := tt.Click("取消"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if m.cleanup != nil {
		t.Fatal("取消 did not close the cleanup preview")
	}
	if screenshots, _, _, _ := svc.cleanupCalls(); len(screenshots) != 0 {
		t.Fatalf("cancel deleted screenshots: %v", screenshots)
	}

	// A failed deletion keeps the measured entry and its retry.
	svc.deleteErr = errString("磁盘忙")
	if err := tt.Click("删除截图"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if err := tt.Click("确认删除"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if m.cleanup == nil || m.cleanup.err == "" {
		t.Fatalf("failed deletion did not keep its prompt: %+v", m.cleanup)
	}
	if !tt.HasText("删除截图失败：磁盘忙") {
		t.Fatalf("the deletion failure does not show: %q", tt.Texts())
	}
	if !tt.HasText("确认删除") {
		t.Fatal("the failed entry offers no retry")
	}

	svc.deleteErr = nil
	if err := tt.Click("确认删除"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if m.cleanup != nil {
		t.Fatal("successful retry did not close the prompt")
	}
	screenshots, _, _, _ := svc.cleanupCalls()
	if len(screenshots) != 2 || screenshots[0] != "r1" || screenshots[1] != "r1" {
		t.Fatalf("screenshot deletion calls = %v, want two attempts for r1", screenshots)
	}
	if !tt.HasText("已删除 2 张截图（2.0 KB），结果与校对状态保留。") {
		t.Fatalf("the deletion result does not show: %q", tt.Texts())
	}
}

func TestViewStorageBulkCleanupPreviewsAndIndependentEntries(t *testing.T) {
	svc := &fakeService{
		list: []record.RecordInfo{
			{ID: "r1", LatestRevision: 1, SavedAt: time.Now()},
			{ID: "r2", LatestRevision: 1, SavedAt: time.Now()},
		},
		storage: app.StorageUsage{
			Records: 2, RecordBytes: 10000, EvidenceBytes: 4096, EvidenceFiles: 4,
			CacheBytes: 1024, LogBytes: 2048,
			DiskFreeBytes: 5 << 30, DiskTotalBytes: 10 << 30, DiskSpaceKnown: true,
		},
	}
	svc.previewFn = func(kind, recordID string) (app.CleanupPreview, error) {
		preview := app.CleanupPreview{
			Records: 2, EvidenceFiles: 4, EvidenceBytes: 4096,
			TotalBytes: 10000, PendingReview: 1,
		}
		if kind == "all-records" {
			preview.Unreadable = 1
		}
		return preview, nil
	}
	m, q := newTestModel(t, Config{Service: svc, ExportSaver: &fakeExportSaver{}})
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)

	if !tt.HasText("历史 2 条 · 记录数据 5.8 KB · 截图 4 张 4.0 KB · 缓存 1.0 KB · 日志 2.0 KB · 磁盘可用 5.0 GB / 10.0 GB") {
		t.Fatalf("the storeroom summary does not show: %q", tt.Texts())
	}

	if err := tt.Click("删除全部截图"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if !tt.HasText("将删除 2 条记录的截图（4 张，4.0 KB），保留结果、修订与校对状态；待校对 1 项。") {
		t.Fatalf("the all-screenshots preview does not show: %q", tt.Texts())
	}
	if err := tt.Click("取消"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if _, all, _, _ := svc.cleanupCalls(); all != 0 {
		t.Fatal("cancel still deleted all screenshots")
	}

	// The screenshot entry fails on its own without touching the record
	// entries, and keeps a retry.
	svc.deleteErr = errString("截图被占用")
	if err := tt.Click("删除全部截图"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if err := tt.Click("确认删除"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if !tt.HasText("删除全部截图失败：截图被占用") || m.cleanup == nil {
		t.Fatalf("the all-screenshots failure does not keep its retry: %q", tt.Texts())
	}
	if _, _, records, allRecords := svc.cleanupCalls(); len(records) != 0 || allRecords != 0 {
		t.Fatal("a failed screenshot cleanup deleted records")
	}
	if err := tt.Click("取消"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)

	svc.deleteErr = nil
	if err := tt.Click("删除全部记录"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if !tt.HasText("将永久删除 2 条记录（共 9.8 KB，其中截图 4 张 4.0 KB）；待校对 1 项。外部导出的 JSON/CSV 不受影响。") {
		t.Fatalf("the all-records preview does not show: %q", tt.Texts())
	}
	if !tt.HasText("其中 1 条记录无法读取，待校对数量不完整。") {
		t.Fatalf("the unreadable mark does not show: %q", tt.Texts())
	}

	svc.deleteErr = errString("权限不足")
	if err := tt.Click("确认删除"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if !tt.HasText("删除全部记录失败：权限不足") {
		t.Fatalf("the bulk failure does not show: %q", tt.Texts())
	}
	if m.cleanup == nil {
		t.Fatal("a failed bulk deletion discarded its retry entry")
	}

	svc.deleteErr = nil
	if err := tt.Click("确认删除"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if m.cleanup != nil {
		t.Fatal("successful bulk retry did not close the prompt")
	}
	if _, _, _, allRecords := svc.cleanupCalls(); allRecords != 2 {
		t.Fatalf("all-records attempts = %d, want 2", allRecords)
	}
	if !tt.HasText("已删除 2 条记录（9.8 KB）。外部导出的 JSON/CSV 不受影响。") {
		t.Fatalf("the bulk result does not show: %q", tt.Texts())
	}
}

func TestViewCleanupDisabledReasons(t *testing.T) {
	// No export saver: both bulk entries explain that cleanup is
	// unavailable in this build.
	svc := &fakeService{}
	m, q := newTestModel(t, Config{Service: svc})
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)
	if !tt.HasText("当前构建未启用导出，历史清理不可用。") {
		t.Fatalf("the missing export saver does not disable cleanup: %q", tt.Texts())
	}

	// A running scan blocks cleanup with its reason.
	svc2 := &fakeService{}
	m2, q2 := newTestModel(t, Config{Service: svc2, ExportSaver: &fakeExportSaver{}})
	tt2 := ui.NewTester(m2.View, 1020, 700)
	settle(t, m2, q2, tt2)
	applyAndSettle(t, m2, q2, tt2, snap("s1", 1, true, scan.PhaseCapturing,
		scan.Counts{Captured: 2}, 0, true, ""))
	if !tt2.HasText("扫描会话进行中，结束后才能清理。") {
		t.Fatalf("the active scan does not block cleanup: %q", tt2.Texts())
	}
	if err := tt2.Click("删除全部记录"); err != nil {
		t.Fatal(err)
	}
	drainAll(t, m2, q2)
	if _, _, _, allRecords := svc2.cleanupCalls(); allRecords != 0 {
		t.Fatalf("a disabled bulk entry still deleted: %d", allRecords)
	}
}

func TestViewHistoryRecordDeletionClosesResults(t *testing.T) {
	svc := cleanupPreviewService(
		[]record.RecordInfo{{ID: "r1", LatestRevision: 1, SavedAt: time.Now()}},
		map[string]*record.Recovery{"r1": testRecord()},
	)
	m, q := newTestModel(t, Config{Service: svc, ExportSaver: &fakeExportSaver{}})
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)
	m.OpenHistory()
	settle(t, m, q, tt)

	if err := tt.Click("删除当前记录"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if !tt.HasText("将永久删除记录 r1（共 4.0 KB，其中截图 2 张 2.0 KB）；待校对 1 项。外部导出的 JSON/CSV 不受影响。") {
		t.Fatalf("the record deletion preview does not show: %q", tt.Texts())
	}
	if err := tt.Click("确认删除"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if m.cleanup != nil {
		t.Fatal("successful record deletion kept the prompt")
	}
	if m.rec != nil {
		t.Fatalf("the deleted record stayed open: %+v", m.rec)
	}
	if !tt.HasText("还没有扫描结果。开始一次扫描，或从历史记录打开一条已保存的记录。") {
		t.Fatalf("the results area did not return to its empty state: %q", tt.Texts())
	}
	if _, _, records, _ := svc.cleanupCalls(); len(records) != 1 || records[0] != "r1" {
		t.Fatalf("record deletions = %v, want r1", records)
	}
}

func TestViewCleanupBlockedByExportAndUnsavedEdit(t *testing.T) {
	svc := &fakeService{
		list:  []record.RecordInfo{{ID: "r1", LatestRevision: 1, SavedAt: time.Now()}},
		loads: map[string]*record.Recovery{"r1": testRecord()},
	}
	m, q := newTestModel(t, Config{Service: svc, ExportSaver: &fakeExportSaver{}})
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)
	m.OpenHistory()
	settle(t, m, q, tt)

	m.exporting = true
	tt.Frame()
	if !tt.HasText("正在导出，完成前不能清理。") {
		t.Fatalf("exporting does not block cleanup: %q", tt.Texts())
	}
	if err := tt.Click("删除截图"); err != nil {
		t.Fatal(err)
	}
	drainAll(t, m, q)
	if screenshots, _, _, _ := svc.cleanupCalls(); len(screenshots) != 0 {
		t.Fatalf("cleanup ran during an export: %v", screenshots)
	}
	m.exporting = false

	m.startEdit()
	if m.edit == nil {
		t.Fatal("the editor did not open")
	}
	m.edit.draft.Level = "14"
	tt.Frame()
	if !tt.HasText("当前记录有未保存的修改，保存或放弃后才能清理。") {
		t.Fatalf("an unsaved edit does not block cleanup: %q", tt.Texts())
	}
	if err := tt.Click("删除当前记录"); err != nil {
		t.Fatal(err)
	}
	drainAll(t, m, q)
	if _, _, records, _ := svc.cleanupCalls(); len(records) != 0 {
		t.Fatalf("cleanup ran with an unsaved edit: %v", records)
	}
}

func TestViewStorageUsageFailureDoesNotShowStaleNumbers(t *testing.T) {
	svc := &fakeService{storageErr: errString("权限不足")}
	m, q := newTestModel(t, Config{Service: svc, ExportSaver: &fakeExportSaver{}})
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)
	if !tt.HasText("占用读取失败：权限不足") {
		t.Fatalf("the storage failure does not show: %q", tt.Texts())
	}
	if tt.HasText("历史 0 条") {
		t.Fatalf("a failed measurement still shows numbers: %q", tt.Texts())
	}
}

func TestViewMissingEvidenceAfterCleanup(t *testing.T) {
	it := structuredItem(1, zzz.DiscRecord{SetID: intp(31100), Slot: 1, Level: intp(15)}, "静听嘉音")
	it.EvidenceIDs = nil
	svc := &fakeService{
		list:  []record.RecordInfo{{ID: "r1", LatestRevision: 2, SavedAt: time.Now()}},
		loads: map[string]*record.Recovery{"r1": recovery(it)},
	}
	m, q := newTestModel(t, Config{Service: svc, ExportSaver: &fakeExportSaver{}})
	tt := ui.NewTester(m.View, 1020, 700)
	settle(t, m, q, tt)
	m.OpenHistory()
	settle(t, m, q, tt)

	if !strings.Contains(m.resultsTitle(), "截图已删除或缺失") {
		t.Fatalf("the record title does not mark missing screenshots: %q", m.resultsTitle())
	}
	if !tt.HasText("序号 1 · 已产出 · 质量：未评价") || !tt.HasText("截图已删除或缺失") {
		t.Fatalf("the missing-evidence mark does not show: %q", tt.Texts())
	}
	if err := tt.Click("显示证据"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if !tt.HasText("该记录的截图已删除或缺失，无法显示图像；识别文本与字段数据仍保留。") {
		t.Fatalf("the evidence area does not explain the missing screenshots: %q", tt.Texts())
	}
	if !tt.HasText("静听嘉音") {
		t.Fatalf("the recognized text disappeared with the screenshots: %q", tt.Texts())
	}
}
