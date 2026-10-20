package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/egoist/mygo/ui"
)

func historyMetadata(r1, r2 time.Time) []app.HistoryRecord {
	return []app.HistoryRecord{
		{
			ID:            "r1",
			SavedAt:       r1,
			Client:        record.ClientMacOSCloud,
			CapturedCount: 995,
			Coverage:      record.CoveragePartial,
			PendingReview: 12,
			TotalBytes:    1024,
			EvidenceFiles: 4,
		},
		{
			ID:         "r2",
			SavedAt:    r2,
			Unreadable: true,
		},
	}
}

func TestViewHistoryShowsMetadataAndDateFilter(t *testing.T) {
	svc := &fakeService{history: historyMetadata(
		time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC),
		time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC),
	)}
	m, q := newTestModel(t, Config{Service: svc})
	tt := ui.NewTester(m.View, 1020, 1000)
	settle(t, m, q, tt)

	if !tt.HasText("macOS 云客户端 · 995 件 · 覆盖：部分 · 待校对 12 · 1.0 KB · r1") {
		t.Fatalf("history metadata does not show: %q", tt.Texts())
	}
	if !tt.HasText("件数未知 · 覆盖：无法确认 · 记录不可读") {
		t.Fatalf("unreadable history state does not show: %q", tt.Texts())
	}

	m.historyFilter = "2026-10-17"
	tt.Frame()
	if !tt.HasText("r1") {
		t.Fatalf("matching record disappeared: %q", tt.Texts())
	}
	if tt.HasText("r2") {
		t.Fatalf("date filter kept r2: %q", tt.Texts())
	}

	m.historyFilter = "2026-13-40"
	tt.Frame()
	if !tt.HasText("日期格式应为 YYYY-MM-DD。") {
		t.Fatalf("invalid date hint does not show: %q", tt.Texts())
	}

	if err := tt.Click("清除筛选"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if m.historyFilter != "" || !tt.HasText("r2") {
		t.Fatalf("clear filter did not restore all records: filter=%q texts=%q", m.historyFilter, tt.Texts())
	}
}

func TestHistoryMultiSelectDeletesSelectedRecords(t *testing.T) {
	svc := &fakeService{
		history: historyMetadata(
			time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC),
			time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC),
		),
		previewFn: func(kind, recordID string) (app.CleanupPreview, error) {
			return app.CleanupPreview{
				Records: 1, EvidenceFiles: 2, EvidenceBytes: 2048,
				TotalBytes: 4096, PendingReview: 1,
			}, nil
		},
	}
	m, q := newTestModel(t, Config{Service: svc, ExportSaver: &fakeExportSaver{}})
	tt := ui.NewTester(m.View, 1020, 1000)
	settle(t, m, q, tt)

	m.setHistorySelected("r1", true)
	m.setHistorySelected("r2", true)
	m.RequestSelectedRecordCleanup()
	settle(t, m, q, tt)
	if !tt.HasText("将永久删除选中 2 条记录（共 8.0 KB，其中截图 4 张 4.0 KB）；待校对 2 项。") {
		t.Fatalf("selected cleanup preview does not show: %q", tt.Texts())
	}
	if err := tt.Click("确认删除选中"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if len(svc.recordDels) != 2 || svc.recordDels[0] != "r1" || svc.recordDels[1] != "r2" {
		t.Fatalf("deleted records = %v, want r1/r2", svc.recordDels)
	}
	if !tt.HasText("已删除选中 2 条记录（8.0 KB）。外部导出的 JSON/CSV 不受影响。") {
		t.Fatalf("selected deletion result does not show: %q", tt.Texts())
	}
}

func TestHistoryMultiSelectDeletesSelectedScreenshots(t *testing.T) {
	svc := &fakeService{
		history: historyMetadata(
			time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC),
			time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC),
		),
		previewFn: func(kind, recordID string) (app.CleanupPreview, error) {
			return app.CleanupPreview{Records: 1, EvidenceFiles: 2, EvidenceBytes: 2048, TotalBytes: 4096, PendingReview: 1}, nil
		},
	}
	m, q := newTestModel(t, Config{Service: svc, ExportSaver: &fakeExportSaver{}})
	tt := ui.NewTester(m.View, 1020, 1000)
	settle(t, m, q, tt)

	m.setHistorySelected("r1", true)
	m.setHistorySelected("r2", true)
	m.RequestSelectedScreenshotCleanup()
	settle(t, m, q, tt)
	if err := tt.Click("确认删除选中"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if len(svc.screenshotDels) != 2 {
		t.Fatalf("screenshot deletions = %v, want two", svc.screenshotDels)
	}
	if !tt.HasText("已删除选中记录的 4 张截图（4.0 KB），结果与校对状态保留。") {
		t.Fatalf("selected screenshot result does not show: %q", tt.Texts())
	}
}

func TestHistoryBulkPartialFailureKeepsFailedSelectionForRetry(t *testing.T) {
	svc := &fakeService{
		history: historyMetadata(
			time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC),
			time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC),
		),
		previewFn: func(kind, recordID string) (app.CleanupPreview, error) {
			return app.CleanupPreview{Records: 1, TotalBytes: 4096}, nil
		},
		deleteErr: errString("磁盘忙"),
	}
	m, q := newTestModel(t, Config{Service: svc, ExportSaver: &fakeExportSaver{}})
	tt := ui.NewTester(m.View, 1020, 1000)
	settle(t, m, q, tt)

	m.setHistorySelected("r1", true)
	m.setHistorySelected("r2", true)
	m.RequestSelectedRecordCleanup()
	settle(t, m, q, tt)
	if err := tt.Click("确认删除选中"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if m.historyBulk == nil || len(m.historyBulk.ids) != 2 || m.historyBulk.err == "" {
		t.Fatalf("failed selection was not kept for retry: %+v", m.historyBulk)
	}
	if selected := m.selectedHistoryIDs(); len(selected) != 2 {
		t.Fatalf("failed selection = %v, want two", selected)
	}
	if !tt.HasText("可重试") || !tt.HasText("确认删除选中") {
		t.Fatalf("retry prompt does not show: %q", tt.Texts())
	}
}

func TestHistoryExportCancelAndFailure(t *testing.T) {
	svc := &fakeService{
		history: historyMetadata(time.Now(), time.Now().Add(-time.Hour)),
		loads: map[string]*record.Recovery{
			"r1": recovery(uiStructuredItem(t, 1, record.QualityAutoAccepted)),
		},
	}
	saver := &fakeExportSaver{}
	m, q := newTestModel(t, Config{Service: svc, ExportSaver: saver, AppVersion: "test"})
	tt := ui.NewTester(m.View, 1020, 1000)
	settle(t, m, q, tt)

	saver.saveFn = func(app.ExportFormat, string, []byte) (string, error) {
		return "", nil
	}
	m.ExportHistory("r1", app.ExportJSON)
	settle(t, m, q, tt)
	if m.historyExporting != "" || m.historyError != "" || m.historyNotice != "已取消导出。" {
		t.Fatalf("cancel state = exporting %q, error %q, notice %q", m.historyExporting, m.historyError, m.historyNotice)
	}

	saver.saveFn = func(app.ExportFormat, string, []byte) (string, error) {
		return "", errors.New("no space left on device")
	}
	m.ExportHistory("r1", app.ExportJSON)
	settle(t, m, q, tt)
	if m.historyExporting != "" || !strings.Contains(m.historyError, "no space left on device") {
		t.Fatalf("failure state = exporting %q, error %q", m.historyExporting, m.historyError)
	}
}

func TestHistoryPerRowExportUsesThatRecord(t *testing.T) {
	svc := &fakeService{
		history: historyMetadata(
			time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC),
			time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC),
		),
		loads: map[string]*record.Recovery{
			"r1": recovery(uiStructuredItem(t, 1, record.QualityAutoAccepted)),
		},
	}
	saver := &fakeExportSaver{}
	m, q := newTestModel(t, Config{Service: svc, ExportSaver: saver, AppVersion: "test"})
	tt := ui.NewTester(m.View, 1020, 1000)
	settle(t, m, q, tt)

	m.ExportHistory("r1", app.ExportJSON)
	settle(t, m, q, tt)
	calls := saver.snapshot()
	if len(calls) != 1 || calls[0].format != app.ExportJSON {
		t.Fatalf("export calls = %+v, want one JSON export", calls)
	}
	if m.historyExporting != "" || m.historyError != "" {
		t.Fatalf("export state = %q / %q", m.historyExporting, m.historyError)
	}
	if !tt.HasText("已导出 JSON：/tmp/") {
		t.Fatalf("history export result does not show: %q", tt.Texts())
	}
}
