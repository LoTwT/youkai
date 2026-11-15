package ui

import (
	"math"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/lobehub/lobe-mygo/pkg/action"
	"github.com/lobehub/lobe-mygo/pkg/theme"
)

// Application geometry follows the frozen UXM-01 v5 design in DIPs.
const (
	panelRadius       = 16
	controlRadius     = 8
	pageInset         = 32
	compactPageInset  = 16
	panelInset        = 24
	columnGap         = 24
	controlGap        = 12
	panelHeaderHeight = 40
	pageHeaderHeight  = 48
)

// pageHeader reserves the same title baseline and body start even when
// trailing controls disappear. Its minimum grows with desktop text size.
func pageHeader(c *ui.Context) ui.Element {
	return ui.Row(c).MinHeight(max(pageHeaderHeight, c.Theme().FontSize*1.5+24)).
		Shrink(0).Gap(12).AlignItems(ui.Center)
}

func pageTitle(c *ui.Context, title string) ui.Element {
	return ui.Text(c, title).FontSize(c.Theme().FontSize * 24 / 14).FontWeight(700).SingleLine()
}

func button(c *ui.Context, label string) ui.Element        { return styledButton(c, label, false) }
func primaryButton(c *ui.Context, label string) ui.Element { return styledButton(c, label, true) }

// viewSegments preserves MyGo Segmented's metrics and native radio group,
// while sharing the persistent-selection colors used by settings.
func viewSegments(c *ui.Context, selected *int, tok *Tokens, labels ...string) ui.Element {
	t := c.Theme()
	parts := ui.SegmentedBase(c, selected, len(labels))
	track := parts.Track.AlignItems(ui.Stretch).Padding(t.Space(.5)).Gap(t.Space(.5)).
		Radius(t.Radius).Background(t.Surface).Border(1, t.Border)
	track.Children(func() {
		for i, label := range labels {
			segment := parts.Segment(i).Padding(t.Space(1), t.Space(3)).Gap(t.Space(1.5)).
				Radius(max(t.Radius-t.Space(.5), 0))
			if i == *selected {
				segment.Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, .12))
			}
			// Reserve the selected outline inside the original padding.
			settingsSegment(segment, i == *selected, t.Dark, tok).
				Padding(t.Space(1)-1, t.Space(3)-1).
				Children(func() { ui.Text(c, label).SingleLine() })
		}
	})
	return track
}

// reviewTextInput scopes only the editable box. MyGo retains its editor,
// IME and paint-time invalid > focus > hover border precedence.
func (m *Model) reviewTextInput(c *ui.Context, value *string, width float32) ui.Element {
	var input ui.Element
	ui.Themed(c, m.theme.input.Theme(c.Theme().Dark)).Background(ui.Transparent).Width(width).Shrink(1).Children(func() {
		input = ui.TextInput(c, value).FillWidth()
	})
	return input
}

func selectedMuted(t *ui.Theme, tok *Tokens, selected bool) ui.Color {
	if selected && !t.Dark {
		return tok.TextSecondary
	}
	return tok.TextMuted
}

// Lobe supplies the button surface; metrics and semantic colors follow the
// effective application theme. Select, text inputs, keycaps and protected
// dialogs remain MyGo compositions because Lobe fixes their internal sizes.
func styledButton(c *ui.Context, label string, primary bool) ui.Element {
	label = strings.TrimSuffix(strings.TrimSuffix(label, "..."), "…")
	variant := action.Default
	if primary {
		variant = action.Primary
	}
	b := action.StyleButton(c, ui.ButtonBase(c), "", action.ButtonProps{Variant: variant, Size: theme.Large})
	t := c.Theme()
	b.Height(float32(math.Ceil(float64(max(40, t.FontSize*1.5+16))/4) * 4)).PaddingX(16).
		FontSize(t.FontSize).Radius(controlRadius).Shrink(0)
	bg, border, fg := t.Surface, t.Border, t.Text
	if primary {
		bg, border, fg = t.Accent, t.Accent, t.AccentText
	}
	if b.Pressed() {
		if primary {
			bg = t.AccentPressed
		} else {
			bg = t.SurfacePressed
		}
	} else if b.Hovered() {
		if primary {
			bg = t.AccentHover
		} else {
			bg = t.SurfaceHover
		}
	}
	b.Background(bg).Border(1, border).TextColor(fg)
	if c.Preferences().ReduceMotion {
		b.Transition(ui.ElementTransition{Duration: -1})
	}
	b.Children(func() { ui.Text(c, label).SingleLine() })
	return b
}

func panel(c *ui.Context, tok *Tokens) ui.Element {
	color, near, far := ui.RGB(22, 36, 62), float32(.035), float32(.035)
	if c.Theme().Dark {
		color, near, far = ui.Hex("#000000"), .16, .14
	}
	return ui.Column(c).Background(tok.SurfaceCard).Border(1, tok.BorderSubtle).Radius(panelRadius).
		Shadow(0, 2, 6, 0, color.Alpha(near)).Shadow(0, 8, 24, 0, color.Alpha(far))
}

// contentPanel contains page content, including full-bleed children such as
// the scan action bar. Clip uses the panel's inner rounded border in MyGo.
func contentPanel(c *ui.Context, tok *Tokens) ui.Element {
	return panel(c, tok).Grow(1).MinWidth(0).MinHeight(0).Clip()
}

func actionRow(c *ui.Context) ui.Element {
	return ui.Row(c).Gap(controlGap).AlignItems(ui.Center).Justify(ui.End).AlignSelf(ui.Stretch)
}

func keycaps(c *ui.Context, keys string) {
	t := c.Theme()
	ui.Row(c).Shrink(0).Gap(4).AlignItems(ui.Center).Children(func() {
		for _, sequence := range strings.Split(keys, " / ") {
			if sequence != strings.Split(keys, " / ")[0] {
				ui.Text(c, "/").TextColor(t.TextMuted)
			}
			for _, key := range strings.Split(sequence, "+") {
				ui.Text(c, key).FontSize(t.FontSize*12/14).Padding(4, 8).MinHeight(t.FontSize*1.5+4).
					Background(t.Surface).Border(1, t.Border).Radius(4).SingleLine()
			}
		}
	})
}

func fieldNumber(c *ui.Context, number string, selected bool, tok *Tokens) {
	fg, bg := tok.TextSecondary, tok.SurfaceSunken
	if selected {
		fg, bg = tok.AccentFG, tok.AccentSoft
	}
	ui.Text(c, number).FontSize(c.Theme().FontSize*12/14).TextColor(fg).Background(bg).Padding(2, 4).Radius(4)
}

// modal styles the same DialogBase used by MyGo Modal. Callers retain the
// open state and all busy/dismissal guards; this helper only owns appearance.
func (m *Model) modal(c *ui.Context, open *bool, content func()) ui.Element {
	tok := m.effectiveTokens()
	w, h := c.Size()
	return ui.DialogBase(c, open, func(backdrop, surface ui.Element) {
		backdrop.Background(tok.Scrim)
		surface.Padding(24).Gap(24).Radius(panelRadius).Background(tok.SurfaceOverlay).
			Border(1, tok.BorderSubtle).MaxWidth(w-32).MaxHeight(h-32).
			Shadow(0, 8, 24, 0, ui.RGBA(0, 0, 0, .18))
		content()
	})
}

func (m *Model) pagePadding() float32 {
	if m.contentWidth < 1120 || (m.displayPage() == pageResults && m.recordTab == recordTabItems && !m.reviewStandard()) {
		return compactPageInset
	}
	return pageInset
}
func (m *Model) railWidth() float32 {
	if m.contentWidth < 1120 || (m.displayPage() == pageResults && m.recordTab == recordTabItems && !m.reviewStandard()) {
		return 128
	}
	return navRailWidth
}
