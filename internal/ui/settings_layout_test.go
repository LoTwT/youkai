package ui

import (
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/egoist/mygo/ui"
)

// Metrics distribute the available width and switch to fewer columns before
// complete measurements or counts stop fitting at the user's text scale.
func TestSettingsStorageGroupsStayReadable(t *testing.T) {
	capacities := []string{"92.8 MB", "127.7 MB", "0 B", "35.8 MB"}
	for _, size := range []struct{ width, height, columns int }{{1440, 900, 4}, {946, 634, 2}, {600, 900, 1}} {
		t.Run(strconv.Itoa(size.width), func(t *testing.T) {
			m, q, svc := settingsTestModel(t)
			m.SetContentSize(size.width, size.height)
			svc.storage = app.StorageUsage{Records: 10, RecordBytes: 231211008, EvidenceBytes: 133903155,
				EvidenceFiles: 40938, CacheBytes: 0, LogBytes: 37539021,
				DiskFreeBytes: 393419004314, DiskTotalBytes: 998365134029, DiskSpaceKnown: true}
			unwrapped := map[string]ui.Rect{}
			tt := ui.NewTesterWith(func(c *ui.Context) {
				m.View(c)
				for _, value := range append(append([]string{}, capacities...), "10 条", "40938 张", "磁盘可用 366.4 GB / 929.8 GB") {
					fontSize := c.Theme().FontSize
					if value == "10 条" || value == "40938 张" || strings.HasPrefix(value, "磁盘") {
						fontSize *= .9
					}
					w, h := c.MeasureText(0, ui.Span{Text: value, Size: fontSize})
					unwrapped[value] = ui.Rect{W: w, H: h}
				}
			}, size.width, size.height, ui.TesterOptions{Preferences: ui.Preferences{TextScale: 1.2, ReduceMotion: true}})
			settle(t, m, q, tt)
			openSettings(t, m, q, tt)
			m.settingsScrollTarget = settingsSectionStorage
			tt.Frame()
			tt.Frame()
			for value, want := range unwrapped {
				bounds, ok := tt.Find(value)
				if !ok || bounds.W+.5 < want.W || bounds.H > want.H+.5 || bounds.X < 0 || bounds.X+bounds.W > float32(size.width) {
					t.Errorf("measurement %q is missing, wrapped or clipped: bounds=%+v unwrapped=%+v", value, bounds, want)
				}
			}
			first, ok := tt.Find(capacities[0])
			if !ok {
				t.Fatal("missing record capacity")
			}
			right := float32(size.width) - m.pagePadding() - 25
			step := (right - first.X + 24) / float32(size.columns)
			for i, capacity := range capacities {
				bounds, _ := tt.Find(capacity)
				wantX := first.X + float32(i%size.columns)*step
				if math.Abs(float64(bounds.X-wantX)) > 1 || (i < size.columns && math.Abs(float64(bounds.Y-first.Y)) > .5) || (i >= size.columns && bounds.Y <= first.Y) {
					t.Errorf("capacity %q is not in the expected %d-column grid: %+v, first=%+v wantX=%v", capacity, size.columns, bounds, first, wantX)
				}
			}
			disk, _ := tt.Find("磁盘可用 366.4 GB / 929.8 GB")
			last, _ := tt.Find(capacities[3])
			if disk.X != first.X || disk.Y <= last.Y+last.H {
				t.Errorf("disk must occupy its own row below all four metrics: disk=%+v last=%+v", disk, last)
			}
			for _, label := range []string{"记录", "截图", "缓存", "日志"} {
				if !tt.HasText(label) {
					t.Errorf("missing metric label %q", label)
				}
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
