package ui

import (
	"testing"

	"github.com/LoTwT/youkai/internal/app"
	"github.com/LoTwT/youkai/internal/platform"
	"github.com/egoist/mygo/ui"
)

func TestPageTitleBoundsStayAligned(t *testing.T) {
	m, q, _ := settingsTestModel(t)
	tt := ui.NewTester(m.View, 1440, 900)
	settle(t, m, q, tt)
	var want ui.Rect
	for i, page := range []Page{pageScan, pageHistory, pageSettings, pageHistory, pageScan} {
		m.page = page
		if i == 3 {
			m.history = nil
		}
		if i == 4 {
			m.snap.Busy = true
		}
		tt.Frame()
		title := map[Page]string{pageScan: "扫描", pageHistory: "记录", pageSettings: "设置"}[page]
		found := false
		for _, node := range tt.AccessAll() {
			if node.Name != title || node.Bounds.X < navRailWidth {
				continue
			}
			got := node.Bounds
			if want.W == 0 {
				want = got
			}
			if got.X != want.X || got.Y != want.Y || got.H != want.H {
				t.Fatalf("%s title = %+v, want x/y/height of %+v", title, got, want)
			}
			found = true
			break
		}
		if !found {
			t.Fatalf("missing %s page title", title)
		}
	}
}

func TestAppearancePickerKeepsDraftAndThemeIdentity(t *testing.T) {
	m, _, tt, _ := openReviewableResults(t, pendingReviewRecord(t), 1440, 900)
	if m.edit == nil {
		t.Fatal("fixture has no editable draft")
	}
	m.edit.draft.Level = "13"
	draft, page, config := m.edit, m.page, m.theme.config
	light, dark := config.Theme(false), config.Theme(true)
	var saved app.Appearance
	m.cfg.SetAppearance = func(mode app.Appearance) error { saved = mode; return nil }
	tt.SetPreferences(ui.Preferences{TextScale: 1.5, HighContrast: true})
	tt.SetDark(true)
	for _, mode := range []app.Appearance{app.AppearanceDark, app.AppearanceLight, app.AppearanceSystem} {
		if err := tt.Click("主题"); err != nil {
			t.Fatal(err)
		}
		if err := tt.Click(appearanceLabel(mode)); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if saved != mode || m.appearance != mode {
			t.Fatalf("mode = %s saved %s, want %s", m.appearance, saved, mode)
		}
		if m.edit != draft || m.edit.draft.Level != "13" || !m.editingDirty() || m.page != page || m.navPrompt != nil {
			t.Fatal("theme picker changed navigation or draft")
		}
		if m.theme.config != config || config.Theme(false) != light || config.Theme(true) != dark {
			t.Fatal("theme identity changed")
		}
		expectDark := mode != app.AppearanceLight
		active := config.Theme(expectDark)
		if !sameColor(m.theme.tokens.SurfaceContent, tokensFor(expectDark).SurfaceContent) {
			t.Fatal("appearance did not take effect")
		}
		if active.FontSize != 21 || active.Focus.A != 255 {
			t.Fatalf("desktop preferences lost: %+v", active)
		}
	}
	tt.SetDark(false)
	if !sameColor(m.theme.tokens.SurfaceContent, tokensFor(false).SurfaceContent) {
		t.Fatal("system mode did not follow desktop change")
	}
}

func TestPlatformShortcutNamesMatchStopSpec(t *testing.T) {
	for _, tc := range []struct {
		goos, command, stop string
		spec                platform.HotkeySpec
	}{
		{"darwin", "Command", "Control+Option+Command+S", platform.HotkeySpec{Key: platform.KeyS, Ctrl: true, Alt: true, Meta: true}},
		{"windows", "Ctrl", "Ctrl+Alt+Shift+S", platform.HotkeySpec{Key: platform.KeyS, Ctrl: true, Alt: true, Shift: true}},
	} {
		if commandKey(tc.goos) != tc.command || stopShortcut(tc.goos) != tc.stop || platform.StopScanHotkey(tc.goos) != tc.spec {
			t.Fatalf("%s binding or visible names changed", tc.goos)
		}
	}
}
