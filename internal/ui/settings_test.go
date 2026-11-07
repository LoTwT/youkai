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

// settingsTestModel is the settings page with measurements, a confirmable
// bulk preview and the diagnostic surfaces wired, so the test drives the
// real controls.
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
		cachePreview: app.StorageClearPreview{Files: 2, Bytes: 2048},
		logPreview:   app.StorageClearPreview{Files: 2, Bytes: 3072},
	}
	svc.previewFn = func(kind, recordID string) (app.CleanupPreview, error) {
		return app.CleanupPreview{
			Records: 2, EvidenceFiles: 4, EvidenceBytes: 4096,
			TotalBytes: 10000, PendingReview: 1,
		}, nil
	}
	m, q := newTestModel(t, Config{
		Service:                  svc,
		DirectExportSaver:        &fakeDirectExportSaver{},
		Updates:                  &fakeUpdates{enabled: false},
		AppVersion:               "0.6.0 (build 1021)",
		BuildCommit:              "83ba00c",
		OpenDataDirectory:        func() error { return nil },
		SystemInfo:               func() app.SystemInfo { return app.SystemInfo{OS: "Windows"} },
		OpenDiagnosticsDirectory: func() error { return nil },
		OpenLicenses:             func() error { return nil },
	})
	m.SetContentSize(1440, 1000)
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

// TestSettingsPageSectionsAndDirectory checks the single-page structure:
// the in-page directory before the sections, the six sections in reading
// order, and the keyboard entry on the directory (08 §6, 09 §2).
func TestSettingsPageSectionsAndDirectory(t *testing.T) {
	m, q, _ := settingsTestModel(t)
	tt := ui.NewTester(m.View, 1440, 1000)
	settle(t, m, q, tt)
	openSettings(t, m, q, tt)

	settings, ok := tt.Find("设置")
	if !ok {
		t.Fatal("the settings rail entry is missing")
	}
	if settings.X+settings.W > navRailWidth {
		t.Errorf("the settings entry at %+v leaves the rail", settings)
	}
	if settings.Y+settings.H < 1000*0.8 {
		t.Errorf("the settings entry at %+v is not pinned to the bottom", settings)
	}

	for _, section := range []string{"常规", "快捷键", "存储与清理", "诊断", "更新", "关于"} {
		if !tt.HasText(section) {
			t.Errorf("the settings page does not show the %q section: %q", section, tt.Texts())
		}
	}
	// The directory repeats the section titles, so each title appears twice
	// in the standard width band; the directory is left of the content.
	if got := countText(tt.Texts(), "常规"); got != 2 {
		t.Fatalf("常规 appears %d times, want the directory entry and its title", got)
	}
	directory, ok := tt.Find("常规")
	if !ok {
		t.Fatal("the in-page directory entry is missing")
	}
	content := float32(0)
	for _, title := range []string{"点击窗口的 ✕ 时", "停止扫描", "打开数据目录", "复制诊断信息"} {
		rect, ok := tt.Find(title)
		if !ok {
			t.Fatalf("%q is missing: %q", title, tt.Texts())
		}
		if rect.X > content {
			content = rect.X
		}
	}
	if directory.X >= content {
		t.Errorf("the directory at %v is not left of the section content at %v", directory, content)
	}
	if !tt.Focused("常规") {
		t.Fatalf("entering settings does not put the keyboard on the directory: %q", tt.Texts())
	}
}

// TestSettingsPageCompactHidesDirectory checks the compact band: no
// in-page directory, and the keyboard lands on the first regular control
// instead (09 §2).
func TestSettingsPageCompactHidesDirectory(t *testing.T) {
	m, q, _ := settingsTestModel(t)
	m.SetContentSize(1020, 900)
	tt := ui.NewTester(m.View, 1020, 900)
	settle(t, m, q, tt)
	openSettings(t, m, q, tt)

	if m.settingsTableOfContentsVisible() {
		t.Fatal("the compact width still claims room for the directory")
	}
	if got := countText(tt.Texts(), "常规"); got != 1 {
		t.Fatalf("常规 appears %d times in the compact band, want only the section title", got)
	}
	if !tt.Focused("隐藏到通知区域（默认）") {
		t.Fatalf("the compact entry does not focus the first regular control: %q", tt.Texts())
	}
}

// TestSettingsPageShortcuts checks the read-only table: only registered
// keys, the design's order, Windows notation, and the stop key state.
func TestSettingsPageShortcuts(t *testing.T) {
	m, q, _ := settingsTestModel(t)
	tt := ui.NewTester(m.View, 1440, 1000)
	settle(t, m, q, tt)
	openSettings(t, m, q, tt)

	rows := m.settingsShortcutRows()
	if len(rows) != 11 {
		t.Fatalf("the shortcut list has %d rows, want 11: %+v", len(rows), rows)
	}
	if got, want := rows[0], (settingsShortcut{action: "停止扫描", context: "开始扫描时注册", keys: shortcutStopScan}); got != want {
		t.Errorf("the first shortcut row = %+v, want %+v", got, want)
	}
	if got, want := rows[10], (settingsShortcut{action: "关闭浮层/返回表格", context: "任意", keys: shortcutCloseOverlay}); got != want {
		t.Errorf("the last shortcut row = %+v, want %+v", got, want)
	}
	for _, registered := range []string{
		"停止扫描", "开始扫描时注册", shortcutStopScan,
		"开始扫描", "扫描准备", shortcutStartScan,
		"确认并下一项", shortcutConfirmNext,
		"保存修改", shortcutSave,
		"下一个待处理件", shortcutNextPending,
		"上一个待处理件", shortcutPreviousPending,
		"导出…", "记录工作区", shortcutExport,
		"截图放大 / 缩小 / 适合窗口", shortcutZoomIn, shortcutZoomOut, shortcutZoomFit,
		"在大区域间跳转", shortcutFocusRegion, shortcutFocusRegionBack,
		"打开选中的记录", shortcutOpenRecord,
		"关闭浮层/返回表格", shortcutCloseOverlay,
	} {
		if !tt.HasText(registered) {
			t.Errorf("the shortcut list does not show %q: %q", registered, tt.Texts())
		}
	}
	for _, row := range rows {
		if strings.ContainsAny(row.keys, "⌘⌃⌥⇧↵") {
			t.Errorf("the shortcut row %+v uses macOS notation; the interface shows Windows keys", row)
		}
	}
}

// TestSettingsStopHotkeyState checks the stop row against the actual
// registration lifetime: Open succeeds before PrepareDeadline is reported;
// the controller marks finalizing before releasing the port, and recognition
// draining follows that release, so finalizing cannot prove the hotkey is
// held. A failed registration wins over the phase because it is the reason
// the session ended.
func TestSettingsStopHotkeyState(t *testing.T) {
	m, q, _ := settingsTestModel(t)
	tt := ui.NewTester(m.View, 1440, 1000)
	settle(t, m, q, tt)
	openSettings(t, m, q, tt)

	if got := m.stopHotkeyStateText(); got != "开始扫描时注册" {
		t.Fatalf("the idle stop key = %q, want the start promise", got)
	}
	if !tt.HasText("开始扫描时注册") {
		t.Fatalf("the idle stop key is not shown: %q", tt.Texts())
	}

	cases := []struct {
		name     string
		snapshot func() app.Snapshot
		want     string
	}{
		{
			name: "preparing before Open returns",
			snapshot: func() app.Snapshot {
				return snap("s1", 1, true, scan.PhasePreparing, scan.Counts{}, 0, true, "")
			},
			want: "正在注册",
		},
		{
			name: "preparing after Open returns",
			snapshot: func() app.Snapshot {
				s := snap("s1", 2, true, scan.PhasePreparing, scan.Counts{}, 0, true, "")
				s.Scan.PrepareDeadline = time.Now().Add(30 * time.Second)
				return s
			},
			want: "已注册",
		},
		{
			name: "capturing",
			snapshot: func() app.Snapshot {
				return snap("s1", 3, true, scan.PhaseCapturing, scan.Counts{Captured: 2}, 0, true, "")
			},
			want: "已注册",
		},
		{
			name: "finalizing does not prove the port is held",
			snapshot: func() app.Snapshot {
				return snap("s1", 4, true, scan.PhaseFinalizing, scan.Counts{Captured: 2}, 0, true, "")
			},
			want: "扫描收尾中",
		},
		{
			name: "finalizing with a failed registration",
			snapshot: func() app.Snapshot {
				s := snap("s1", 5, true, scan.PhaseFinalizing, scan.Counts{}, 0, true, "")
				s.Scan.Trigger = &scan.Trigger{
					Kind: scan.TriggerFault, Code: "hotkeyUnavailable", Phase: scan.PhasePreparing,
				}
				return s
			},
			want: "上次注册失败：被其他程序占用",
		},
		{
			name: "ended with a failed registration",
			snapshot: func() app.Snapshot {
				s := snap("s1", 6, false, scan.PhaseEnded, scan.Counts{}, 0, false, "")
				s.Scan.End = scan.EndFailed
				s.Scan.Trigger = &scan.Trigger{
					Kind: scan.TriggerFault, Code: "hotkeyUnavailable", Phase: scan.PhasePreparing,
				}
				return s
			},
			want: "上次注册失败：被其他程序占用",
		},
		{
			name: "ended with another fault",
			snapshot: func() app.Snapshot {
				s := snap("s1", 7, false, scan.PhaseEnded, scan.Counts{}, 0, false, "")
				s.Scan.End = scan.EndFailed
				s.Scan.Trigger = &scan.Trigger{
					Kind: scan.TriggerFault, Code: "focusLost", Phase: scan.PhaseCapturing,
				}
				return s
			},
			want: "开始扫描时注册",
		},
		{
			name: "completed normally",
			snapshot: func() app.Snapshot {
				s := snap("s1", 8, false, scan.PhaseEnded, scan.Counts{}, 0, false, "")
				s.Scan.End = scan.EndCompleted
				return s
			},
			want: "开始扫描时注册",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			applyAndSettle(t, m, q, tt, tc.snapshot())
			m.page = pageSettings
			tt.Frame()
			if got := m.stopHotkeyStateText(); got != tc.want {
				t.Fatalf("the stop hotkey state = %q, want %q", got, tc.want)
			}
			if !tt.HasText(tc.want) {
				t.Fatalf("the stop hotkey state %q is not shown: %q", tc.want, tt.Texts())
			}
		})
	}
}

// TestSettingsDirectoryScrollsToSection checks the directory actually
// moves the content: a section below the fold comes into view when its
// entry is used.
func TestSettingsDirectoryScrollsToSection(t *testing.T) {
	m, q, _ := settingsTestModel(t)
	m.SetContentSize(1180, 600)
	tt := ui.NewTester(m.View, 1180, 600)
	settle(t, m, q, tt)
	openSettings(t, m, q, tt)

	if rect, ok := tt.Find("开源许可"); ok && rect.H > 0 && rect.Y >= 0 && rect.Y+rect.H <= 600 {
		t.Fatalf("the about section already fits; the jump cannot be observed: %+v", rect)
	}
	if err := tt.Click("关于"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	rect, ok := tt.Find("开源许可")
	if !ok || rect.H == 0 || rect.Y < 0 || rect.Y+rect.H > 600 {
		t.Fatalf("the about section did not come into view: %+v", rect)
	}
}

// TestSettingsGeneralClosePreference checks the regular section: both close
// choices, the persisted change, the tray-hint placeholder and the tray
// notice.
func TestSettingsGeneralClosePreference(t *testing.T) {
	var saved []app.CloseBehavior
	m, q, _ := settingsTestModel(t)
	m.cfg.SaveCloseBehavior = func(behavior app.CloseBehavior) error {
		saved = append(saved, behavior)
		return nil
	}
	tt := ui.NewTester(m.View, 1440, 1000)
	settle(t, m, q, tt)
	openSettings(t, m, q, tt)

	if !tt.HasText("点击窗口的 ✕ 时") {
		t.Fatal("the close preference does not name the interaction")
	}
	if !tt.HasText("隐藏到通知区域（默认）") || !tt.HasText("停止扫描并退出") {
		t.Fatalf("the close choices are missing: %q", tt.Texts())
	}
	if !tt.HasText("再看一次托盘图标位置提示") {
		t.Fatal("the tray-hint placeholder is missing")
	}

	if err := tt.Click("停止扫描并退出"); err != nil {
		t.Fatal(err)
	}
	drainAll(t, m, q)
	if m.CloseBehavior() != app.CloseBehaviorExit {
		t.Fatalf("close behavior = %q after the click", m.CloseBehavior())
	}
	if len(saved) != 1 || saved[0] != app.CloseBehaviorExit {
		t.Fatalf("saved preferences = %v", saved)
	}

	tt.Frame()
	if err := tt.Click("隐藏到通知区域（默认）"); err != nil {
		t.Fatal(err)
	}
	drainAll(t, m, q)
	if m.CloseBehavior() != app.CloseBehaviorHide {
		t.Fatalf("close behavior = %q after the second click", m.CloseBehavior())
	}
	if len(saved) != 2 || saved[1] != app.CloseBehaviorHide {
		t.Fatalf("saved preferences = %v", saved)
	}

	m.SetTrayUnavailable("test")
	tt.Frame()
	if !tt.HasText("通知区域图标不可用") {
		t.Fatal("the tray-unavailable reason does not show")
	}
	if tt.HasText("隐藏到通知区域（默认）") {
		t.Fatal("the hide option stays visible without a tray")
	}
}

// TestSettingsGeneralSaveFailureOffersRetry checks the failure exit: the
// reason stays in the section and the retry persists the already applied
// preference again.
func TestSettingsGeneralSaveFailureOffersRetry(t *testing.T) {
	var saved []app.CloseBehavior
	fail := true
	m, q, _ := settingsTestModel(t)
	m.cfg.SaveCloseBehavior = func(behavior app.CloseBehavior) error {
		saved = append(saved, behavior)
		if fail {
			return errString("设置文件只读")
		}
		return nil
	}
	tt := ui.NewTester(m.View, 1440, 1000)
	settle(t, m, q, tt)
	openSettings(t, m, q, tt)

	if err := tt.Click("停止扫描并退出"); err != nil {
		t.Fatal(err)
	}
	drainAll(t, m, q)
	if !tt.HasText("保存设置失败：设置文件只读") {
		t.Fatalf("the save failure does not show: %q", tt.Texts())
	}
	fail = false
	if err := tt.Click("重试"); err != nil {
		t.Fatal(err)
	}
	drainAll(t, m, q)
	if len(saved) != 2 || saved[1] != app.CloseBehaviorExit {
		t.Fatalf("the retry saved %v, want the exit preference again", saved)
	}
	if tt.HasText("保存设置失败") {
		t.Fatal("the save failure outlived the successful retry")
	}
}

// TestSettingsStorageCopy checks the storage section's measured summary,
// the rebuildable and destructive entries, and the removed danger zone.
func TestSettingsStorageCopy(t *testing.T) {
	m, q, _ := settingsTestModel(t)
	tt := ui.NewTester(m.View, 1440, 1000)
	settle(t, m, q, tt)
	openSettings(t, m, q, tt)

	want := "记录 5.8 KB（2 条） · 截图 4 张 4.0 KB · 缓存 1.0 KB · 日志 2.0 KB"
	if !tt.HasText(want) {
		t.Fatalf("the storeroom summary does not show: %q", tt.Texts())
	}
	if !tt.HasText("磁盘可用 5.0 GB / 10.0 GB") {
		t.Fatalf("the free disk space does not show: %q", tt.Texts())
	}
	for _, label := range []string{"打开数据目录", "清缓存", "清旧日志", "删除全部截图…", "删除全部记录…"} {
		if !tt.HasText(label) {
			t.Errorf("the storage section does not offer %q", label)
		}
	}
	if tt.HasText("危险区") {
		t.Fatal("the old danger zone is still on the page")
	}
}

// TestSettingsStorageDisabledReasons checks that a running scan disables the
// destructive entries with one consistent reason.
func TestSettingsStorageDisabledReasons(t *testing.T) {
	m2, q2, svc2 := settingsTestModel(t)
	tt2 := ui.NewTester(m2.View, 1440, 1000)
	settle(t, m2, q2, tt2)
	openSettings(t, m2, q2, tt2)
	applyAndSettle(t, m2, q2, tt2, snap("s1", 1, true, scan.PhaseCapturing,
		scan.Counts{Captured: 2}, 0, true, ""))
	if !tt2.HasText("扫描会话进行中，结束后才能清理。") {
		t.Fatalf("the active scan does not block cleanup: %q", tt2.Texts())
	}
	if err := tt2.Click("删除全部记录…"); err != nil {
		t.Fatal(err)
	}
	drainAll(t, m2, q2)
	if _, _, _, allRecords := svc2.cleanupCalls(); allRecords != 0 {
		t.Fatalf("a disabled bulk entry still deleted: %d", allRecords)
	}
}

// TestSettingsCleanupPromptDefaultsToCancel checks the measured
// confirmation: it names the records and bytes, opens on 取消, and Enter
// closes it without deleting. The deliberate confirm still deletes.
func TestSettingsCleanupPromptDefaultsToCancel(t *testing.T) {
	m, q, svc := settingsTestModel(t)
	tt := ui.NewTester(m.View, 1440, 1000)
	settle(t, m, q, tt)
	openSettings(t, m, q, tt)

	if err := tt.Click("删除全部记录…"); err != nil {
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
	if err := tt.Click("删除全部记录…"); err != nil {
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

// TestSettingsStorageFromPrepareCheck checks the disk-space entry: the
// page shows what completes the trip, focuses the first storage action, and
// a completed cleanup returns to the prepare check.
func TestSettingsStorageFromPrepareCheck(t *testing.T) {
	m, q, svc := settingsTestModel(t)
	m.SetContentSize(1180, 520)
	tt := ui.NewTester(m.View, 1180, 520)
	settle(t, m, q, tt)

	m.ShowStorageFromPrepareCheck()
	settle(t, m, q, tt)
	if m.displayPage() != pageSettings {
		t.Fatalf("the disk-space entry opened %v, want settings", m.displayPage())
	}
	if !tt.HasText("完成后返回准备检查") {
		t.Fatalf("the return promise does not show: %q", tt.Texts())
	}
	if !tt.Focused("打开数据目录") {
		t.Fatalf("the disk-space entry does not focus the first storage action: %q", tt.Texts())
	}
	// The focused action was scrolled into view, not left below the fold.
	if rect, ok := tt.Find("打开数据目录"); !ok || rect.Y < 0 || rect.Y+rect.H > 520 {
		t.Fatalf("the storage action is not visible: %+v", rect)
	}

	if err := tt.Click("清缓存"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if err := tt.Click("确认清理"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if cache, _ := svc.storageClearCalls(); cache != 1 {
		t.Fatalf("cache clears = %d, want 1", cache)
	}
	if m.displayPage() != pageScan {
		t.Fatalf("the completed cleanup stayed on %v, want the prepare check", m.displayPage())
	}
	if tt.HasText("完成后返回准备检查") {
		t.Fatal("the return promise outlived the return")
	}
}

// TestSettingsCleanupReturnsFromEachFinishPath checks every cleanup that
// can run from the storage section returns to the prepare check after
// success, including the single-record paths reachable from the same page's
// shared prompt.
func TestSettingsCleanupReturnsFromEachFinishPath(t *testing.T) {
	cases := []struct {
		name    string
		request func(*Model)
	}{
		{"all screenshots", func(m *Model) { m.RequestAllScreenshotCleanup() }},
		{"all records", func(m *Model) { m.RequestAllRecordCleanup() }},
		{"one record's screenshots", func(m *Model) { m.RequestScreenshotCleanup("r1") }},
		{"one record", func(m *Model) { m.RequestRecordCleanup("r1") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, q, _ := settingsTestModel(t)
			tt := ui.NewTester(m.View, 1440, 1000)
			settle(t, m, q, tt)
			m.ShowStorageFromPrepareCheck()
			settle(t, m, q, tt)

			tc.request(m)
			settle(t, m, q, tt)
			if m.cleanup == nil || !m.cleanup.ready {
				t.Fatalf("the preview did not become confirmable: %+v", m.cleanup)
			}
			if err := tt.Click("确认删除"); err != nil {
				t.Fatal(err)
			}
			settle(t, m, q, tt)
			if m.displayPage() != pageScan {
				t.Fatalf("the completed cleanup stayed on %v, want the prepare check", m.displayPage())
			}
			if m.cleanup != nil {
				t.Fatalf("the successful cleanup kept its prompt: %+v", m.cleanup)
			}
			if tt.HasText("完成后返回准备检查") {
				t.Fatal("the return promise outlived the return")
			}
		})
	}
}

// TestSettingsFailureStaysOnSettings checks every failure branch keeps the
// user on the storage section with its retry instead of claiming the return.
// The shared cache/log branch is exercised through the cache.
func TestSettingsFailureStaysOnSettings(t *testing.T) {
	cases := []struct {
		name    string
		request func(*Model)
		seed    func(*fakeService)
		confirm string
		prompt  func(*Model) string
		want    string
	}{
		{
			name:    "all screenshots",
			request: func(m *Model) { m.RequestAllScreenshotCleanup() },
			seed:    func(s *fakeService) { s.deleteErr = errString("权限不足") },
			confirm: "确认删除",
			prompt: func(m *Model) string {
				if m.cleanup == nil {
					return ""
				}
				return m.cleanup.err
			},
			want: "删除全部截图失败：权限不足",
		},
		{
			name:    "one record's screenshots",
			request: func(m *Model) { m.RequestScreenshotCleanup("r1") },
			seed:    func(s *fakeService) { s.deleteErr = errString("权限不足") },
			confirm: "确认删除",
			prompt: func(m *Model) string {
				if m.cleanup == nil {
					return ""
				}
				return m.cleanup.err
			},
			want: "删除截图失败：权限不足",
		},
		{
			name:    "one record",
			request: func(m *Model) { m.RequestRecordCleanup("r1") },
			seed:    func(s *fakeService) { s.deleteErr = errString("权限不足") },
			confirm: "确认删除",
			prompt: func(m *Model) string {
				if m.cleanup == nil {
					return ""
				}
				return m.cleanup.err
			},
			want: "删除记录失败：权限不足",
		},
		{
			name:    "all records",
			request: func(m *Model) { m.RequestAllRecordCleanup() },
			seed:    func(s *fakeService) { s.deleteErr = errString("权限不足") },
			confirm: "确认删除",
			prompt: func(m *Model) string {
				if m.cleanup == nil {
					return ""
				}
				return m.cleanup.err
			},
			want: "删除全部记录失败：权限不足",
		},
		{
			name:    "cache",
			request: func(m *Model) { m.RequestCacheClear() },
			seed:    func(s *fakeService) { s.cacheClearErr = errString("缓存忙") },
			confirm: "确认清理",
			prompt: func(m *Model) string {
				if m.storagePrompt == nil {
					return ""
				}
				return m.storagePrompt.err
			},
			want: "清理失败：缓存忙",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, q, svc := settingsTestModel(t)
			tc.seed(svc)
			tt := ui.NewTester(m.View, 1440, 1000)
			settle(t, m, q, tt)
			m.ShowStorageFromPrepareCheck()
			settle(t, m, q, tt)

			tc.request(m)
			settle(t, m, q, tt)
			if err := tt.Click(tc.confirm); err != nil {
				t.Fatal(err)
			}
			settle(t, m, q, tt)
			if m.displayPage() != pageSettings {
				t.Fatalf("a failed cleanup returned to %v, want settings", m.displayPage())
			}
			if got := tc.prompt(m); !strings.Contains(got, tc.want) {
				t.Fatalf("the failed prompt = %q, want %q", got, tc.want)
			}
			if !tt.HasText(tc.want) {
				t.Fatalf("the failed cleanup does not show %q: %q", tc.want, tt.Texts())
			}
		})
	}
}

// TestSettingsDiagnosticsActions checks both diagnostic actions: the
// copied text follows the newest session's allowlist, the build commit is
// present, window titles and user paths stay out, the directory action
// reaches the entry point, and the privacy note stays visible.
func TestSettingsDiagnosticsActions(t *testing.T) {
	const windowTitle = "Personal Bank - Google Chrome"
	const userPath = `C:\Users\alice\AppData\secret.log`
	at := time.Date(2026, 11, 5, 14, 3, 21, 0, time.Local)
	opened := 0
	m, q, _ := settingsTestModel(t)
	m.cfg.OpenDiagnosticsDirectory = func() error {
		opened++
		return nil
	}
	tt := ui.NewTester(m.View, 1440, 1000)
	settle(t, m, q, tt)
	openSettings(t, m, q, tt)

	ended := snap("s1", 1, false, scan.PhaseEnded, scan.Counts{Captured: 1}, 0, false, "")
	ended.Scan.End = scan.EndFailed
	ended.Scan.Trigger = &scan.Trigger{
		Kind:    scan.TriggerFault,
		Code:    "focusLost",
		Message: windowTitle,
		Params:  map[string]string{"window": windowTitle, "path": userPath},
		Phase:   scan.PhaseCapturing,
		At:      at,
	}
	applyAndSettle(t, m, q, tt, ended)
	m.page = pageSettings
	tt.Frame()

	if !tt.HasText("复制的内容不包含窗口标题等隐私") {
		t.Fatalf("the privacy note is missing: %q", tt.Texts())
	}
	if err := tt.Click("复制诊断信息"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	want := app.DiagnosticsText(app.DiagnosticsInput{
		Version: "0.6.0 (build 1021)",
		Commit:  "83ba00c",
		System:  app.SystemInfo{OS: "Windows"},
		Scan: &app.DiagnosticsScan{
			End: string(scan.EndFailed),
			Trigger: &scan.Trigger{
				Kind:  scan.TriggerFault,
				Code:  "focusLost",
				Phase: scan.PhaseCapturing,
				At:    at,
			},
		},
	})
	if got := tt.Clipboard(); got != want {
		t.Fatalf("the clipboard holds %q, want %q", got, want)
	}
	if !tt.HasText("已复制诊断信息。") {
		t.Fatalf("the copy result does not show: %q", tt.Texts())
	}
	if !strings.Contains(tt.Clipboard(), "（构建 83ba00c）") {
		t.Fatalf("the copied text does not name the build commit: %q", tt.Clipboard())
	}
	if !strings.Contains(tt.Clipboard(), "触发码：focusLost") {
		t.Fatalf("the copied text does not name the newest trigger: %q", tt.Clipboard())
	}
	if strings.Contains(tt.Clipboard(), windowTitle) || strings.Contains(tt.Clipboard(), userPath) {
		t.Fatalf("the copied text leaked private context: %q", tt.Clipboard())
	}

	if err := tt.Click("打开诊断目录"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if opened != 1 || !tt.HasText("已打开诊断目录。") {
		t.Fatalf("opened=%d texts=%q", opened, tt.Texts())
	}
}

// TestSettingsOpenDiagnosticsDirectoryFailure checks the directory
// action's failure exit.
func TestSettingsOpenDiagnosticsDirectoryFailure(t *testing.T) {
	m, q, _ := settingsTestModel(t)
	m.cfg.OpenDiagnosticsDirectory = func() error { return errString("没有文件管理器") }
	tt := ui.NewTester(m.View, 1440, 1000)
	settle(t, m, q, tt)
	openSettings(t, m, q, tt)

	if err := tt.Click("打开诊断目录"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if !tt.HasText("打开诊断目录失败：没有文件管理器") {
		t.Fatalf("the directory failure does not show: %q", tt.Texts())
	}
}

// TestSettingsAbout checks the version and the bundled open-source notice
// action.
func TestSettingsAbout(t *testing.T) {
	opened := 0
	m, q, _ := settingsTestModel(t)
	m.cfg.OpenLicenses = func() error {
		opened++
		return nil
	}
	tt := ui.NewTester(m.View, 1440, 1000)
	settle(t, m, q, tt)
	openSettings(t, m, q, tt)

	if !tt.HasText("youkai 0.6.0 (build 1021)") {
		t.Fatalf("the about section does not name the version: %q", tt.Texts())
	}
	if !tt.HasText("以 MIT 许可发布。") {
		t.Fatalf("the about section does not state the license: %q", tt.Texts())
	}
	if err := tt.Click("开源许可"); err != nil {
		t.Fatal(err)
	}
	settle(t, m, q, tt)
	if opened != 1 {
		t.Fatalf("the license action opened %d times, want 1", opened)
	}
}
