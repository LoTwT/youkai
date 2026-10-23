package ui

import mgui "github.com/egoist/mygo/ui"

// Tokens is Youkai's semantic color layer. MyGo's Theme is the runtime
// input and the adapter for built-in widgets; app-composed components
// should read these semantic roles instead of hard-coding hex values.
//
// The names here intentionally match docs/design/uiux-2026-10-21/
// dark-tokens.md. When a token changes, update that document and the
// design mockups together.
type Tokens struct {
	// Surfaces. Window/content/card/header/control are distinct roles;
	// sunken is for wells and bars, overlay for floating surfaces.
	SurfaceWindow   mgui.Color
	SurfaceContent  mgui.Color
	SurfaceCard     mgui.Color
	SurfaceControl  mgui.Color
	SurfaceHeader   mgui.Color
	SurfaceSunken   mgui.Color
	SurfaceOverlay  mgui.Color
	SurfaceEvidence mgui.Color

	SurfaceHover    mgui.Color
	SurfacePressed  mgui.Color
	SurfaceDisabled mgui.Color

	BorderSubtle  mgui.Color
	BorderDefault mgui.Color
	BorderStrong  mgui.Color
	BorderControl mgui.Color

	TextPrimary   mgui.Color
	TextSecondary mgui.Color
	TextMuted     mgui.Color
	TextDisabled  mgui.Color
	TextCode      mgui.Color

	// Accent is split into foreground and fill. Text/icons use AccentFG;
	// primary buttons and MyGo's selected row use AccentFill with OnFill.
	AccentFG          mgui.Color
	AccentFill        mgui.Color
	AccentFillHover   mgui.Color
	AccentFillPressed mgui.Color
	AccentOnFill      mgui.Color
	AccentSoft        mgui.Color
	AccentSoftHover   mgui.Color
	AccentBorder      mgui.Color

	DangerFG     mgui.Color
	DangerSoft   mgui.Color
	DangerBorder mgui.Color
	DangerStrong mgui.Color
	DangerSolid  mgui.Color

	WarningFG     mgui.Color
	WarningSoft   mgui.Color
	WarningBorder mgui.Color
	WarningStrong mgui.Color

	SuccessFG     mgui.Color
	SuccessSoft   mgui.Color
	SuccessBorder mgui.Color
	SuccessStrong mgui.Color

	RowHover          mgui.Color
	RowSelected       mgui.Color
	RowSelectedBorder mgui.Color
	TableHeader       mgui.Color
	TableHeaderText   mgui.Color
	TableBorder       mgui.Color
	EvidenceSurface   mgui.Color
	EvidenceBorder    mgui.Color
	Inverse           mgui.Color
	InverseText       mgui.Color
	Scrollbar         mgui.Color
	Focus             mgui.Color
	Selection         mgui.Color
	Scrim             mgui.Color
}

// ApplyTheme copies the runtime theme, applies Youkai's semantic palette,
// and installs the copy for this frame. It preserves Dark, FontSize,
// Spacing, Radius and the other runtime metrics; only colors are replaced.
//
// It must run before any child widgets are built.
func ApplyTheme(c *mgui.Context) *Tokens {
	themed, tok := themeWithTokens(c.Theme(), c.Preferences())
	c.SetTheme(themed)
	return tok
}

// themeWithTokens is the pure part of ApplyTheme, kept separate so tests
// can verify the mapping without constructing a UI context.
func themeWithTokens(base *mgui.Theme, prefs mgui.Preferences) (*mgui.Theme, *Tokens) {
	tok := tokensFor(base.Dark)
	if prefs.HighContrast {
		adjusted := highContrastTokens(*tok)
		tok = &adjusted
	}

	themed := *base
	themed.Background = tok.SurfaceContent
	themed.Surface = tok.SurfaceControl
	themed.SurfaceHover = tok.SurfaceHover
	themed.SurfacePressed = tok.SurfacePressed
	themed.Border = tok.BorderDefault
	themed.Text = tok.TextPrimary
	themed.TextMuted = tok.TextMuted
	themed.Accent = tok.AccentFill
	themed.AccentHover = tok.AccentFillHover
	themed.AccentPressed = tok.AccentFillPressed
	themed.AccentText = tok.AccentOnFill
	themed.Danger = tok.DangerFG
	themed.Warning = tok.WarningFG
	themed.Success = tok.SuccessFG
	themed.Selection = tok.Selection
	themed.Focus = tok.Focus
	themed.Inverse = tok.Inverse
	themed.InverseText = tok.InverseText
	themed.Scrollbar = tok.Scrollbar
	return &themed, tok
}

// highContrastTokens keeps MyGo's Increase Contrast behavior after we
// replace the default theme colors. It follows preferences.go's approach:
// borders move toward the text color, secondary text gets stronger, and
// the focus ring becomes opaque.
func highContrastTokens(t Tokens) Tokens {
	t.BorderSubtle = t.BorderSubtle.Mix(t.TextPrimary, 0.30)
	t.BorderDefault = t.BorderDefault.Mix(t.TextPrimary, 0.45)
	t.BorderStrong = t.BorderStrong.Mix(t.TextPrimary, 0.25)
	t.BorderControl = t.BorderControl.Mix(t.TextPrimary, 0.25)
	t.TextSecondary = t.TextSecondary.Mix(t.TextPrimary, 0.20)
	t.TextMuted = t.TextMuted.Mix(t.TextPrimary, 0.40)
	t.Focus.A = 255
	t.Scrollbar = t.Scrollbar.Alpha(1.6)
	return t
}

func tokensFor(dark bool) *Tokens {
	if dark {
		return &Tokens{
			SurfaceWindow:   hex("#101318"),
			SurfaceContent:  hex("#151920"),
			SurfaceCard:     hex("#1B2029"),
			SurfaceControl:  hex("#222833"),
			SurfaceHeader:   hex("#222833"),
			SurfaceSunken:   hex("#0F1217"),
			SurfaceOverlay:  hex("#29303C"),
			SurfaceEvidence: hex("#090C11"),

			SurfaceHover:    hex("#262D38"),
			SurfacePressed:  hex("#303845"),
			SurfaceDisabled: hex("#1C1C20"),

			BorderSubtle:  hex("#242A34"),
			BorderDefault: hex("#303845"),
			BorderStrong:  hex("#46505F"),
			BorderControl: hex("#7C8494"),

			TextPrimary:   hex("#F2F4F8"),
			TextSecondary: hex("#C3C9D3"),
			TextMuted:     hex("#98A1AE"),
			TextDisabled:  hex("#6F7886"),
			TextCode:      hex("#C9D1D9"),

			AccentFG:          hex("#60A5FA"),
			AccentFill:        hex("#2563EB"),
			AccentFillHover:   hex("#1D4ED8"),
			AccentFillPressed: hex("#1E40AF"),
			AccentOnFill:      hex("#FFFFFF"),
			AccentSoft:        hex("#1A2440"),
			AccentSoftHover:   hex("#223454"),
			AccentBorder:      hex("#35507A"),

			DangerFG:     hex("#F87171"),
			DangerSoft:   hex("#2A1618"),
			DangerBorder: hex("#6E2B30"),
			DangerStrong: hex("#FCA5A5"),
			DangerSolid:  hex("#DC2626"),

			WarningFG:     hex("#FBBF24"),
			WarningSoft:   hex("#2A2113"),
			WarningBorder: hex("#6B4E16"),
			WarningStrong: hex("#FCD34D"),

			SuccessFG:     hex("#4ADE80"),
			SuccessSoft:   hex("#12251A"),
			SuccessBorder: hex("#245C36"),
			SuccessStrong: hex("#86EFAC"),

			RowHover:          hex("#262D38"),
			RowSelected:       hex("#1A2440"),
			RowSelectedBorder: hex("#60A5FA"),
			TableHeader:       hex("#222833"),
			TableHeaderText:   hex("#98A1AE"),
			TableBorder:       hex("#242A34"),
			EvidenceSurface:   hex("#090C11"),
			EvidenceBorder:    hex("#303845"),
			Inverse:           hex("#F2F4F8"),
			InverseText:       hex("#101318"),
			Scrollbar:         mgui.RGBA(255, 255, 255, 0.28),
			Focus:             mgui.RGBA(147, 197, 253, 0.85),
			Selection:         mgui.RGBA(96, 165, 250, 0.30),
			Scrim:             mgui.RGBA(4, 7, 12, 0.64),
		}
	}

	return &Tokens{
		SurfaceWindow:   hex("#FFFFFF"),
		SurfaceContent:  hex("#FFFFFF"),
		SurfaceCard:     hex("#FFFFFF"),
		SurfaceControl:  hex("#F4F4F5"),
		SurfaceHeader:   hex("#FAFAFB"),
		SurfaceSunken:   hex("#F0F0F2"),
		SurfaceOverlay:  hex("#FFFFFF"),
		SurfaceEvidence: hex("#F4F4F5"),

		SurfaceHover:    hex("#F4F4F5"),
		SurfacePressed:  hex("#DDDDE1"),
		SurfaceDisabled: hex("#F4F4F5"),

		BorderSubtle:  hex("#ECECF0"),
		BorderDefault: hex("#D9D9DE"),
		BorderStrong:  hex("#8A8A93"),
		BorderControl: hex("#8A8A93"),

		TextPrimary:   hex("#18181B"),
		TextSecondary: hex("#3F3F46"),
		TextMuted:     hex("#6B6B73"),
		TextDisabled:  hex("#8A8A93"),
		TextCode:      hex("#3F3F46"),

		AccentFG:          hex("#2563EB"),
		AccentFill:        hex("#2563EB"),
		AccentFillHover:   hex("#1D4ED8"),
		AccentFillPressed: hex("#1E40AF"),
		AccentOnFill:      hex("#FFFFFF"),
		AccentSoft:        hex("#EFF6FF"),
		AccentSoftHover:   hex("#DBEAFE"),
		AccentBorder:      hex("#BFDBFE"),

		DangerFG:     hex("#B91C1C"),
		DangerSoft:   hex("#FEF2F2"),
		DangerBorder: hex("#FECACA"),
		DangerStrong: hex("#991B1B"),
		DangerSolid:  hex("#DC2626"),

		WarningFG:     hex("#B45309"),
		WarningSoft:   hex("#FFFBEB"),
		WarningBorder: hex("#FDE68A"),
		WarningStrong: hex("#92400E"),

		SuccessFG:     hex("#15803D"),
		SuccessSoft:   hex("#F0FDF4"),
		SuccessBorder: hex("#BBF7D0"),
		SuccessStrong: hex("#166534"),

		RowHover:          hex("#F4F4F5"),
		RowSelected:       hex("#EFF6FF"),
		RowSelectedBorder: hex("#2563EB"),
		TableHeader:       hex("#FAFAFB"),
		TableHeaderText:   hex("#6B6B73"),
		TableBorder:       hex("#ECECF0"),
		EvidenceSurface:   hex("#F4F4F5"),
		EvidenceBorder:    hex("#D9D9DE"),
		Inverse:           hex("#18181B"),
		InverseText:       hex("#FFFFFF"),
		Scrollbar:         mgui.RGBA(0, 0, 0, 0.32),
		Focus:             mgui.RGBA(37, 99, 235, 0.85),
		Selection:         mgui.RGBA(37, 99, 235, 0.25),
		Scrim:             mgui.RGBA(24, 24, 27, 0.32),
	}
}

func hex(s string) mgui.Color { return mgui.Hex(s) }
