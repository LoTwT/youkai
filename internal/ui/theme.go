package ui

import (
	"github.com/LoTwT/youkai/internal/app"
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
	runtime := *c.Theme()
	switch m.appearance {
	case app.AppearanceLight:
		runtime.Dark = false
	case app.AppearanceDark:
		runtime.Dark = true
	}
	target, tok := m.theme.effective(&runtime, c.Preferences())
	c.SetTheme(target)
	return tok
}

func (m *Model) effectiveTokens() *Tokens { return &m.theme.tokens }

func appearanceLabel(mode app.Appearance) string {
	switch mode {
	case app.AppearanceLight:
		return "浅色"
	case app.AppearanceDark:
		return "深色"
	default:
		return "跟随系统"
	}
}

func (m *Model) setAppearance(mode app.Appearance) {
	m.appearance = mode.Normalized()
	m.appearanceErr = ""
	if m.cfg.SetAppearance != nil {
		if err := m.cfg.SetAppearance(m.appearance); err != nil {
			m.appearanceErr = "保存主题失败：" + err.Error()
		}
	}
}

// settingsAppearance is the sole theme entry. SegmentedBase owns native
// single-selection, keyboard and focus semantics without changing pages.
func (m *Model) settingsAppearance(c *ui.Context, t *ui.Theme, tok *Tokens) {
	ui.Text(c, "主题").TextColor(tok.TextSecondary)
	modes := []app.Appearance{app.AppearanceSystem, app.AppearanceLight, app.AppearanceDark}
	selected := 0
	for i, mode := range modes {
		if mode == m.appearance {
			selected = i
		}
	}
	parts := ui.SegmentedBase(c, &selected, len(modes))
	control := parts.Track.Label("主题").AlignSelf(ui.Start).Padding(4).Gap(4).
		Radius(controlRadius).Background(tok.SurfaceSunken).Border(1, tok.BorderDefault)
	control.Children(func() {
		for i, mode := range modes {
			segment := parts.Segment(i).MinHeight(max(40, t.FontSize*1.5+16)).Padding(8, 16).Radius(4)
			if i == 0 {
				segment = segment.Bind(&m.settingsFirstRef)
				if m.settingsFocusPending && !m.settingsFocusStorage {
					segment = segment.Focus()
					m.settingsFocusPending = false
				}
			}
			bg := ui.Transparent
			if i == selected {
				bg = tok.SurfaceControl
			}
			if segment.Hovered() {
				bg = tok.SurfaceHover
			}
			if segment.Pressed() {
				bg = tok.SurfacePressed
			}
			segment.Background(bg).Children(func() { ui.Text(c, appearanceLabel(mode)).SingleLine() })
		}
	})
	if modes[selected] != m.appearance {
		m.setAppearance(modes[selected])
	}
	if m.appearanceErr != "" {
		ui.Text(c, m.appearanceErr).TextColor(c.Theme().Danger)
		if button(c, "重试主题保存").Clicked() {
			m.setAppearance(m.appearance)
		}
	}
}
