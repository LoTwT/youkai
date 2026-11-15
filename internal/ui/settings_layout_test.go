package ui

import (
	"math"
	"testing"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/egoist/mygo/ui"
)

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
