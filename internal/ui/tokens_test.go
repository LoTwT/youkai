package ui

import (
	"math"
	"testing"

	mgui "github.com/egoist/mygo/ui"
	"github.com/lobehub/lobe-mygo/pkg/theme"
)

func TestTokensForLightAndDark(t *testing.T) {
	light := tokensFor(false)
	dark := tokensFor(true)

	cases := []struct {
		name string
		got  mgui.Color
		want mgui.Color
	}{
		{"light window", light.SurfaceWindow, mgui.Hex("#F3F5FA")},
		{"light card", light.SurfaceCard, mgui.Hex("#FFFFFF")},
		{"light header", light.SurfaceHeader, mgui.Hex("#FFFFFF")},
		{"light control", light.SurfaceControl, mgui.Hex("#FFFFFF")},
		{"light text", light.TextPrimary, mgui.Hex("#202B40")},
		{"light muted", light.TextMuted, mgui.Hex("#637187")},
		{"light accent fill", light.AccentFill, mgui.Hex("#315FE8")},
		{"dark window", dark.SurfaceWindow, mgui.Hex("#080808")},
		{"dark card", dark.SurfaceCard, mgui.Hex("#141414")},
		{"dark header", dark.SurfaceHeader, mgui.Hex("#141414")},
		{"dark control", dark.SurfaceControl, mgui.Hex("#1B1B1B")},
		{"dark text", dark.TextPrimary, mgui.Hex("#EEEEEE")},
		{"dark muted", dark.TextMuted, mgui.Hex("#A0A0A0")},
		{"dark accent fg", dark.AccentFG, mgui.Hex("#8BABFF")},
		{"dark accent fill", dark.AccentFill, mgui.Hex("#8BABFF")},
		{"dark evidence", dark.SurfaceEvidence, mgui.Hex("#1B1B1B")},
	}
	for _, tc := range cases {
		if !sameColor(tc.got, tc.want) {
			t.Errorf("%s = %v, want %v", tc.name, tc.got, tc.want)
		}
	}
}

func TestThemeLifecyclePreferencesKeepRegisteredPointer(t *testing.T) {
	s := newThemeState()
	config := s.config
	for _, dark := range []bool{false, true} {
		base := mgui.LightTheme()
		if dark {
			base = mgui.DarkTheme()
		}
		base.Font = "system-fixture"
		pointer := config.Theme(dark)
		for _, scale := range []float32{1, 1.5, 1.5, 1.25, 1} {
			for _, high := range []bool{false, true} {
				target, tok := s.effective(base, mgui.Preferences{TextScale: scale, HighContrast: high, Accent: mgui.Hex("#FF0000")})
				if target != pointer || s.config != config {
					t.Fatal("theme/config identity changed")
				}
				if target.FontSize != 14*scale || target.Font != base.Font {
					t.Fatalf("font accumulated or changed: %+v", target)
				}
				if high && (tok.Focus.A != 255 || sameColor(tok.BorderSubtle, tokensFor(dark).BorderSubtle)) {
					t.Fatal("high contrast was not applied to application surfaces")
				}
				var primary mgui.Color
				tt := mgui.NewTester(func(c *mgui.Context) { c.SetTheme(target); primary = theme.TokensOf(c).Primary }, 32, 32)
				tt.Frame()
				if !sameColor(primary, tokensFor(dark).AccentFill) {
					t.Fatal("Lobe lost the registered brand tokens")
				}
			}
		}
	}
}

func TestThemeWithTokensPreservesRuntimeMetrics(t *testing.T) {
	base := mgui.LightTheme()
	base.FontSize = 15
	base.Spacing = 5
	base.Radius = 8

	themed, tok := themeWithTokens(base, mgui.Preferences{TextScale: 1})

	if themed.Dark != base.Dark {
		t.Errorf("Dark = %v, want %v", themed.Dark, base.Dark)
	}
	if themed.FontSize != base.FontSize || themed.Spacing != base.Spacing || themed.Radius != base.Radius {
		t.Fatalf("runtime metrics changed: got font=%v spacing=%v radius=%v, want %v/%v/%v",
			themed.FontSize, themed.Spacing, themed.Radius, base.FontSize, base.Spacing, base.Radius)
	}
	if !sameColor(themed.Background, tok.SurfaceContent) {
		t.Errorf("Background = %v, want %v", themed.Background, tok.SurfaceContent)
	}
	if !sameColor(themed.Surface, tok.SurfaceControl) {
		t.Errorf("Surface = %v, want %v", themed.Surface, tok.SurfaceControl)
	}
	if !sameColor(themed.Accent, tok.AccentFill) || !sameColor(themed.AccentText, tok.AccentOnFill) {
		t.Errorf("accent mapping = %v/%v, want %v/%v", themed.Accent, themed.AccentText, tok.AccentFill, tok.AccentOnFill)
	}
}

func TestHighContrastTokens(t *testing.T) {
	base := mgui.DarkTheme()
	normalTheme, normal := themeWithTokens(base, mgui.Preferences{})
	highTheme, high := themeWithTokens(base, mgui.Preferences{HighContrast: true})

	if sameColor(high.BorderDefault, normal.BorderDefault) {
		t.Error("high contrast did not strengthen BorderDefault")
	}
	if sameColor(high.TextMuted, normal.TextMuted) {
		t.Error("high contrast did not strengthen TextMuted")
	}
	if high.Focus.A != 255 {
		t.Errorf("high contrast Focus alpha = %d, want 255", high.Focus.A)
	}
	if !sameColor(highTheme.Border, high.BorderDefault) || !sameColor(highTheme.TextMuted, high.TextMuted) {
		t.Error("high contrast tokens were not applied to the runtime theme")
	}
	if highTheme.FontSize != normalTheme.FontSize {
		t.Errorf("high contrast changed FontSize: %v vs %v", highTheme.FontSize, normalTheme.FontSize)
	}
}

func TestTokenContrastPairs(t *testing.T) {
	for _, dark := range []bool{false, true} {
		tok := tokensFor(dark)
		pairs := []struct {
			name string
			fg   mgui.Color
			bg   mgui.Color
			min  float64
		}{
			{"primary/card", tok.TextPrimary, tok.SurfaceCard, 12},
			{"secondary/card", tok.TextSecondary, tok.SurfaceCard, 4.5},
			{"muted/card", tok.TextMuted, tok.SurfaceCard, 4.5},
			{"muted/control", tok.TextMuted, tok.SurfaceControl, 4.5},
			{"disabled/control", tok.TextDisabled, tok.SurfaceControl, 3},
			{"onFill/AccentFill", tok.AccentOnFill, tok.AccentFill, 4.5},
			{"danger/soft", tok.DangerFG, tok.DangerSoft, 4.5},
			{"warning/soft", tok.WarningFG, tok.WarningSoft, 4.5},
			{"success/soft", tok.SuccessFG, tok.SuccessSoft, 4.5},
		}
		for _, p := range pairs {
			got := contrastRatio(p.fg, p.bg)
			if got+0.01 < p.min {
				t.Errorf("dark=%v %s contrast = %.2f, want >= %.1f", dark, p.name, got, p.min)
			}
		}
	}
}

func sameColor(a, b mgui.Color) bool {
	return a.R == b.R && a.G == b.G && a.B == b.B && a.A == b.A
}

func contrastRatio(a, b mgui.Color) float64 {
	l1, l2 := relativeLuminance(a), relativeLuminance(b)
	if l1 < l2 {
		l1, l2 = l2, l1
	}
	return (l1 + 0.05) / (l2 + 0.05)
}

func relativeLuminance(c mgui.Color) float64 {
	channel := func(v uint8) float64 {
		x := float64(v) / 255
		if x <= 0.04045 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(c.R) + 0.7152*channel(c.G) + 0.0722*channel(c.B)
}
