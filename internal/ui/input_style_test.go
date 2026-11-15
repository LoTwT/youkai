package ui

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// The scoped input must leave paint-time validation and focus ahead of
// its stronger baseline border, including when validation changes in place.
func TestReviewInputBorderStatePrecedence(t *testing.T) {
	for _, dark := range []bool{false, true} {
		m := &Model{theme: newThemeState()}
		base := ui.LightTheme()
		if dark {
			base = ui.DarkTheme()
		}
		theme, tok := m.theme.effective(base, ui.Preferences{ReduceMotion: true})
		value, invalid := "1.20", false
		var bounds ui.Rect
		tt := ui.NewTester(func(c *ui.Context) {
			c.SetTheme(theme)
			ui.Column(c).Padding(12).Children(func() {
				input := m.reviewTextInput(c, &value, 110).Label("数值").Invalid(invalid)
				bounds = input.Bounds()
			})
		}, 200, 100)
		tt.SetPreferences(ui.Preferences{ReduceMotion: true})
		tt.Frame()
		tt.Frame()
		check := func(want ui.Color) {
			t.Helper()
			got := tt.Image().RGBAAt(int(bounds.X), int(bounds.Y+bounds.H/2))
			if got.R != want.R || got.G != want.G || got.B != want.B {
				t.Fatalf("dark=%v input border %v, want %v", dark, got, want)
			}
		}
		check(tok.BorderControl)
		if err := tt.Click("数值"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		check(tok.AccentFG)
		invalid = true
		tt.Frame()
		check(tok.DangerFG)
		invalid = false
		tt.Frame()
		check(tok.AccentFG)
		if value != "1.20" {
			t.Fatal("styling normalized the string draft")
		}
	}
}
