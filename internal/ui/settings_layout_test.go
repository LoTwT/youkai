package ui

import (
	"math"
	"strconv"
	"testing"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/egoist/mygo/ui"
)

// The static storage table preserves its three aligned columns and all
// measurements at both the standard and the reported narrow window size.
func TestSettingsStorageGroupsStayReadable(t *testing.T) {
	for _, size := range []struct{ width, height int }{{1440, 900}, {946, 634}} {
		t.Run(strconv.Itoa(size.width), func(t *testing.T) {
			m, q, svc := settingsTestModel(t)
			m.SetContentSize(size.width, size.height)
			svc.storage = app.StorageUsage{Records: 10, RecordBytes: 231211008, EvidenceBytes: 133903155,
				EvidenceFiles: 40938, CacheBytes: 0, LogBytes: 37539021,
				DiskFreeBytes: 393419004314, DiskTotalBytes: 998365134029, DiskSpaceKnown: true}
			measurements := map[string]ui.Rect{}
			tt := ui.NewTesterWith(func(c *ui.Context) {
				m.View(c)
				for _, value := range []string{"92.8 MB", "127.7 MB", "0 B", "35.8 MB", "10条", "40938张", "—", "磁盘可用 366.4 GB / 929.8 GB"} {
					fontSize := c.Theme().FontSize
					if value == "磁盘可用 366.4 GB / 929.8 GB" {
						fontSize *= .9
					}
					w, h := c.MeasureText(0, ui.Span{Text: value, Size: fontSize})
					measurements[value] = ui.Rect{W: w, H: h}
				}
			}, size.width, size.height, ui.TesterOptions{Preferences: ui.Preferences{TextScale: 1.2, ReduceMotion: true}})
			settle(t, m, q, tt)
			openSettings(t, m, q, tt)
			// Read the reference viewport while visible; offscreen clipped
			// accessibility bounds may have zero width after scrolling away.
			m.settingsScrollTarget = settingsSectionShortcuts
			tt.Frame()
			tt.Frame()
			shortcuts, shortcutsOK := tt.Find("快捷键列表")
			if !shortcutsOK || shortcuts.W == 0 {
				t.Fatal("missing visible shortcut width reference")
			}
			m.settingsScrollTarget = settingsSectionStorage
			tt.Frame()
			tt.Frame()
			capacityHeader, ok := tt.Find("占用")
			if !ok {
				t.Fatal("missing storage table header")
			}
			countHeader, ok := tt.Find("数量")
			if !ok || countHeader.X <= capacityHeader.X+capacityHeader.W {
				t.Fatal("numeric columns overlap")
			}
			categoryHeader, ok := tt.Find("类别")
			if !ok || categoryHeader.X+categoryHeader.W >= capacityHeader.X {
				t.Fatal("category column overlaps capacity")
			}
			if math.Abs(float64(categoryHeader.X-shortcuts.X)) > .5 || math.Abs(float64(countHeader.X+countHeader.W-shortcuts.X-shortcuts.W)) > .5 {
				t.Errorf("storage table does not share shortcut list edges: category=%+v count=%+v shortcuts=%+v", categoryHeader, countHeader, shortcuts)
			}
			t.Logf("storage edges %.1f..%.1f; shortcut edges %.1f..%.1f", categoryHeader.X, countHeader.X+countHeader.W, shortcuts.X, shortcuts.X+shortcuts.W)
			previousY := capacityHeader.Y
			for i, value := range []string{"92.8 MB", "127.7 MB", "0 B", "35.8 MB"} {
				bounds, ok := tt.Find(value)
				if !ok || math.Abs(float64(bounds.X-capacityHeader.X)) > .5 || math.Abs(float64(bounds.W-capacityHeader.W)) > .5 || bounds.Y <= previousY {
					t.Errorf("row %d capacity %q is not in its aligned column: %+v", i, value, bounds)
				}
				label := []string{"记录", "截图", "缓存", "日志"}[i]
				count := []string{"10条", "40938张", "—", "—"}[i]
				labelFound, countFound := false, false
				for _, node := range tt.AccessAll() {
					if math.Abs(float64(node.Bounds.Y-bounds.Y)) > .5 {
						continue
					}
					if node.Name == label && math.Abs(float64(node.Bounds.X-categoryHeader.X)) < .5 {
						labelFound = true
					}
					if node.Name == count && math.Abs(float64(node.Bounds.X-countHeader.X)) < .5 && math.Abs(float64(node.Bounds.W-countHeader.W)) < .5 {
						countFound = true
					}
				}
				if !labelFound || !countFound {
					t.Errorf("row %q missing aligned category or count %q", label, count)
				}
				previousY = bounds.Y
			}
			for value, measured := range measurements {
				bounds, ok := tt.Find(value)
				if !ok || bounds.W+.5 < measured.W || bounds.H > measured.H+.5 || bounds.X < 0 || bounds.X+bounds.W > float32(size.width) {
					t.Errorf("measurement %q is missing, wrapped or clipped: %+v measured=%+v", value, bounds, measured)
				}
			}
			disk, _ := tt.Find("磁盘可用 366.4 GB / 929.8 GB")
			if disk.X != categoryHeader.X || disk.Y <= previousY {
				t.Errorf("disk is not a separate row: %+v", disk)
			}
		})
	}
}

// Check the actual key bounds: filling only the horizontal viewport leaves
// its unconstrained content at its intrinsic width.
func TestSettingsShortcutsFillAvailableWidth(t *testing.T) {
	m, q, _ := settingsTestModel(t)
	tt := ui.NewTester(m.View, 1440, 900)
	settle(t, m, q, tt)
	openSettings(t, m, q, tt)
	m.settingsScrollTarget = settingsSectionShortcuts
	tt.Frame()
	tt.Frame()
	list, ok := tt.Find("快捷键列表")
	if !ok {
		t.Fatal("missing shortcut viewport")
	}
	// The card uses the width freed by removing the in-page directory.
	wantRight := float32(1440) - m.pagePadding() - 25
	wantLeft := m.railWidth() + m.pageLeftPadding() + 25
	if math.Abs(float64(list.X-wantLeft)) > 1 || math.Abs(float64(list.X+list.W-wantRight)) > 1 {
		t.Fatalf("shortcut viewport does not fill the card: %+v, want edges %v..%v", list, wantLeft, wantRight)
	}
	// Every row ends at the same right edge, including short bindings.
	ends := map[string]bool{"S": false, "R": false, "Enter": false, "J": false, "E": false, "=": false, "-": false, "0": false, "F6": false, "Esc": false}
	for _, node := range tt.AccessAll() {
		if _, key := ends[node.Name]; !key || node.Bounds.Y < list.Y || node.Bounds.X < list.X || node.Bounds.W == 0 {
			continue
		}
		ends[node.Name] = true
		if math.Abs(float64(node.Bounds.X+node.Bounds.W-wantRight)) > 1 {
			t.Errorf("%s key ends at %v, want %v", node.Name, node.Bounds.X+node.Bounds.W, wantRight)
		}
	}
	for key, seen := range ends {
		if !seen {
			t.Errorf("missing visible row-ending key %s", key)
		}
	}
	before, _ := tt.Find("停止扫描（全局 · 开始扫描时注册）")
	if err := tt.Click("快捷键列表"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyRight)
	after, _ := tt.Find("停止扫描（全局 · 开始扫描时注册）")
	if after != before {
		t.Fatalf("normal width has unnecessary horizontal scroll: %+v -> %+v", before, after)
	}
}

func TestSettingsAppearanceButtonsAndRetry(t *testing.T) {
	m, q, _ := settingsTestModel(t)
	tt := ui.NewTester(m.View, 1440, 900)
	settle(t, m, q, tt)
	if tt.HasText("主题") {
		t.Fatal("theme entry remains outside Settings")
	}
	openSettings(t, m, q, tt)
	for _, label := range []string{"跟随系统", "浅色", "深色"} {
		node, ok := tt.AccessByRole(ui.RoleRadio, label)
		if !ok || node.HasPopup != ui.PopupNone || node.Bounds.H < 40 || node.Bounds.X < m.railWidth() {
			t.Fatalf("theme is not a visible regular-height radio button: %s %+v", label, node)
		}
	}
	fail, calls := true, 0
	m.cfg.SetAppearance = func(mode app.Appearance) error {
		calls++
		if mode != app.AppearanceDark {
			t.Fatalf("unexpected save %s", mode)
		}
		if fail {
			return errString("设置文件只读")
		}
		return nil
	}
	if err := tt.Click("深色"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("保存主题失败：设置文件只读") || m.appearance != app.AppearanceDark {
		t.Fatal("theme save failure lost its reason or applied mode")
	}
	fail = false
	if err := tt.Click("重试主题保存"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if calls != 2 || m.appearanceErr != "" {
		t.Fatal("retry did not save the existing mode")
	}
	node, _ := tt.AccessByRole(ui.RoleRadio, "深色")
	if !node.Checked {
		t.Fatal("dark mode is not visibly selected")
	}
	m.SetTrayUnavailable("test")
	m.settingsFocusPending = true
	tt.Frame()
	if !tt.Focused("跟随系统") {
		t.Fatal("missing tray stole entry focus from the live theme button")
	}
}
