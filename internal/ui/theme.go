package ui

import (
	"github.com/egoist/mygo/ui"
	"github.com/lobehub/lobe-mygo/pkg/theme"
)

// themeState owns exactly one Lobe Config for this model's lifetime. Lobe
// resolves tokens by Theme pointer, so preferences update the registered
// objects in place, always from unscaled base values.
type themeState struct {
	config      *theme.Config
	light, dark ui.Theme
	tokens      Tokens
}

func newThemeState() *themeState {
	light, dark := *theme.Light(), *theme.Dark()
	for _, pair := range []struct {
		lobe *theme.Tokens
		app  *Tokens
	}{{&light, tokensFor(false)}, {&dark, tokensFor(true)}} {
		l, a := pair.lobe, pair.app
		l.Primary, l.PrimaryHover, l.PrimaryActive, l.PrimaryForeground = a.AccentFill, a.AccentFillHover, a.AccentFillPressed, a.AccentOnFill
		l.PrimaryText, l.PrimaryBg, l.PrimaryBgHover, l.PrimaryBorder = a.AccentFG, a.AccentSoft, a.AccentSoftHover, a.AccentBorder
		l.BgLayout, l.BgContainer, l.BgContainerSecondary, l.BgElevated = a.SurfaceContent, a.SurfaceCard, a.SurfaceSunken, a.SurfaceOverlay
		l.Text, l.TextHeading, l.TextSecondary, l.TextTertiary = a.TextPrimary, a.TextPrimary, a.TextSecondary, a.TextMuted
		l.Border, l.BorderSecondary = a.BorderDefault, a.BorderSubtle
		l.BorderRadius, l.BorderRadiusSM, l.BorderRadiusLG = controlRadius, 4, panelRadius
		l.FontSize = 14
	}
	config := theme.NewConfig(light, dark)
	return &themeState{config: config, light: *config.Theme(false), dark: *config.Theme(true)}
}

func (s *themeState) effective(runtime *ui.Theme, prefs ui.Preferences) (*ui.Theme, *Tokens) {
	base := s.light
	if runtime.Dark {
		base = s.dark
	}
	base.Font = runtime.Font
	// Brand color is fixed by v5; all other desktop preferences are retained.
	prefs.Accent = ui.Transparent
	effective := base.WithPreferences(prefs)
	effective, tok := themeWithTokens(effective, prefs)
	target := s.config.Theme(runtime.Dark)
	*target = *effective
	s.tokens = *tok
	return target, &s.tokens
}

func (m *Model) applyTheme(c *ui.Context) *Tokens {
	if m.theme == nil {
		m.theme = newThemeState()
	}
	target, tok := m.theme.effective(c.Theme(), c.Preferences())
	c.SetTheme(target)
	return tok
}

func (m *Model) effectiveTokens() *Tokens { return &m.theme.tokens }
