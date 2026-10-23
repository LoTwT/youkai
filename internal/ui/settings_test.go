package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/record"
	"github.com/LoTwT/youkai/internal/scan"
	"github.com/egoist/mygo/ui"
)

// settingsTestModel is the settings page with measurements and a
// confirmable bulk preview, so the test drives the real controls.
func settingsTestModel(t *testing.T) (*Model, *queueUpdater, *fakeService) {
	t.Helper()
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
		return app.CleanupPreview{
			Records: 2, EvidenceFiles: 4, EvidenceBytes: 4096,
			TotalBytes: 10000, PendingReview: 1,
		}, nil
	}
	m, q := newTestModel(t, Config{
		Service:           svc,
		DirectExportSaver: &fakeDirectExportSaver{},
		Updates:           &fakeUpdates{enabled: false},
		AppVersion:        "0.6.0 (build 1021)",
	})
	m.SetContentSize(1440, 900)
	return m, q, svc
}

// openSettings selects the settings destination after the shell settled.
func openSettings(t *testing.T, m *Model, q *queueUpdater, tt *ui.Tester) {
	t.Helper()
	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if m.displayPage() != pageSettings {
		t.Fatalf("the page is %v after clicking settings, want settings", m.displayPage())
	}
}

// TestSettingsPageKeepsTheWideRail checks the settings destination uses
// the 168-DIP rail and keeps its entry pinned below the other items.
func TestSettingsPageKeepsTheWideRail(t *testing.T) {
	m, q, _ := settingsTestModel(t)
	tt := ui.NewTester(m.View, 1440, 900)
	settle(t, m, q, tt)
	openSettings(t, m, q, tt)

	if got := m.sidebarWidth(); got != sidebarWideWidth {
		t.Fatalf("the settings page rail is %v DIP wide, want %v", got, sidebarWideWidth)
	}
	settings, ok := tt.Find("设置")
	if !ok {
		t.Fatal("the settings rail entry is missing")
	}
	if settings.Y+settings.H < 900*0.8 {
		t.Errorf("the settings entry at %+v is not pinned to the bottom", settings)
	}
}

// TestSettingsPageGroupsVersionAndShortcuts checks the round-3 grid: the
// four titled groups, one version line, and only the keys this build
// registers.
func TestSettingsPageGroupsVersionAndShortcuts(t *testing.T) {
	m, q, _ := settingsTestModel(t)
	tt := ui.NewTester(m.View, 1440, 900)
	settle(t, m, q, tt)
	openSettings(t, m, q, tt)

	for _, group := range []string{"窗口与启动", "软件更新", "存储与清理", "快捷键", "危险区"} {
		if !tt.HasText(group) {
			t.Errorf("the settings page does not show the %q group: %q", group, tt.Texts())
		}
	}

	// The two columns sit side by side at 1440: window/update on the left,
	// storage/shortcuts on the right.
	left, ok := tt.Find("窗口与启动")
	if !ok {
		t.Fatal("the window group is missing")
	}
	right, ok := tt.Find("存储与清理")
	if !ok {
		t.Fatal("the storage group is missing")
	}
	if left.X >= right.X {
		t.Errorf("the settings groups are not in two columns: window at %v, storage at %v", left, right)
	}

	// The version is named once, in 软件更新.
	versions := 0
	for _, text := range tt.Texts() {
		if strings.Contains(text, "当前版本") {
			versions++
		}
	}
	if versions != 1 {
		t.Fatalf("the settings page shows the version %d times, want 1: %q", versions, tt.Texts())
	}

	// Every row names a key this build registers, in the design's order.
	rows := settingsShortcutRows()
	if len(rows) != 7 {
		t.Fatalf("the shortcut list has %d rows, want 7: %+v", len(rows), rows)
	}
	for _, registered := range []string{
		"开始扫描", "⌘R", "扫描准备",
		"停止扫描", "扫描中 · 全局", "确认并下一项", "下一个待校对", "⌘↵", "⌘J",
		"保存修改", "⌘S", "打开记录", "关闭浮层/返回表格", "Esc",
	} {
		if !tt.HasText(registered) {
			t.Errorf("the shortcut list does not show %q: %q", registered, tt.Texts())
		}
	}
	if got, want := rows[0], (settingsShortcut{action: "开始扫描", context: "扫描准备", keys: "⌘R"}); got != want {
		t.Errorf("the first shortcut row = %+v, want %+v", got, want)
	}
	if got, want := rows[6], (settingsShortcut{action: "关闭浮层/返回表格", context: "任意", keys: "Esc"}); got != want {
		t.Errorf("the last shortcut row = %+v, want %+v", got, want)
	}
}

// TestSettingsPageStacksBelowTheSplit checks the narrow reading order.
func TestSettingsPageStacksBelowTheSplit(t *testing.T) {
	m, q, _ := settingsTestModel(t)
	m.SetContentSize(1020, 900)
	tt := ui.NewTester(m.View, 1020, 900)
	settle(t, m, q, tt)
	openSettings(t, m, q, tt)

	window, ok := tt.Find("窗口与启动")
	if !ok {
		t.Fatal("the window group is missing")
	}
	storage, ok := tt.Find("存储与清理")
	if !ok {
		t.Fatal("the storage group is missing")
	}
	if storage.Y <= window.Y {
		t.Errorf("the narrow settings groups are not stacked: window at %v, storage at %v", window, storage)
	}
}

// TestSettingsDangerZoneCopy checks the destructive entries' wording and
// the description of what they affect.
func TestSettingsDangerZoneCopy(t *testing.T) {
	m, q, _ := settingsTestModel(t)
	tt := ui.NewTester(m.View, 1440, 900)
	settle(t, m, q, tt)
	openSettings(t, m, q, tt)

	description := "删除后无法恢复。确认前会列出影响的记录数与占用空间，默认焦点在“取消”；扫描进行中不可用。"
	if !tt.HasText(description) {
		t.Fatalf("the danger zone does not explain the scope: %q", tt.Texts())
	}
	for _, label := range []string{"删除全部截图", "删除全部记录"} {
		if !tt.HasText(label) {
			t.Errorf("the danger zone does not offer %q", label)
		}
	}
	for _, text := range tt.Texts() {
		if strings.HasPrefix(text, "删除全部") && strings.Contains(text, "…") {
			t.Errorf("the danger entry %q still carries an ellipsis", text)
		}
	}
}

// TestSettingsCleanupPromptDefaultsToCancel checks the measured
// confirmation: it names the records and bytes, opens on 取消, and Enter
// closes it without deleting. The confirm action still deletes.
func TestSettingsCleanupPromptDefaultsToCancel(t *testing.T) {
	m, q, svc := settingsTestModel(t)
	tt := ui.NewTester(m.View, 1440, 900)
	settle(t, m, q, tt)
	openSettings(t, m, q, tt)

	if err := tt.Click("删除全部记录"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	want := "将永久删除 2 条记录（共 9.8 KB，其中截图 4 张 4.0 KB）；待校对 1 项。外部导出的 JSON/CSV 不受影响。"
	if !tt.HasText(want) {
		t.Fatalf("the confirmation does not name the records and bytes: %q", tt.Texts())
	}
	if !tt.Focused("取消") {
		t.Fatalf("the confirmation does not open on 取消: %q", tt.Texts())
	}

	tt.Key(0, ui.KeyEnter)
	settle(t, m, q, tt)
	if m.cleanup != nil {
		t.Fatal("Enter on the default focus did not close the confirmation")
	}
	if _, _, _, allRecords := svc.cleanupCalls(); allRecords != 0 {
		t.Fatalf("Enter on the default focus deleted %d record batches", allRecords)
	}

	// The deliberate confirm still runs the previewed deletion.
	if err := tt.Click("删除全部记录"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if !tt.Focused("取消") {
		t.Fatal("the reopened confirmation does not take the safe focus")
	}
	if err := tt.Click("确认删除"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if m.cleanup != nil {
		t.Fatal("the confirmed deletion did not close the prompt")
	}
	if _, _, _, allRecords := svc.cleanupCalls(); allRecords != 1 {
		t.Fatalf("record deletion batches = %d, want 1", allRecords)
	}
	if !tt.HasText("已删除 2 条记录（9.8 KB）。外部导出的 JSON/CSV 不受影响。") {
		t.Fatalf("the deletion result does not show: %q", tt.Texts())
	}
}

// TestSettingsDangerZoneDisabled checks the disabled state and its reason
// during a scan.
func TestSettingsDangerZoneDisabled(t *testing.T) {
	m, q, svc := settingsTestModel(t)
	tt := ui.NewTester(m.View, 1440, 900)
	settle(t, m, q, tt)
	openSettings(t, m, q, tt)

	applyAndSettle(t, m, q, tt, snap("s1", 1, true, scan.PhaseCapturing,
		scan.Counts{Captured: 2}, 0, true, ""))
	if !tt.HasText("扫描会话进行中，结束后才能清理。") {
		t.Fatalf("the danger zone does not explain the disabled entries: %q", tt.Texts())
	}
	if err := tt.Click("删除全部记录"); err != nil {
		t.Fatal(err)
	}
	drainAll(t, m, q)
	if m.cleanup != nil {
		t.Fatal("a disabled danger entry opened its confirmation")
	}
	if _, _, _, allRecords := svc.cleanupCalls(); allRecords != 0 {
		t.Fatalf("a disabled danger entry deleted %d record batches", allRecords)
	}
}
