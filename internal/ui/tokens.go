package ui

import mgui "github.com/egoist/mygo/ui"

// Tokens is Youkai's semantic color layer. MyGo's Theme is the runtime
// input and the adapter for built-in widgets; app-composed components
// should read these semantic roles instead of hard-coding hex values.
//
// Values follow the accepted UXM-01 v5 semantic palette. Runtime desktop
// preferences are applied once by themeState, before any children build.
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

// themeWithTokens maps the effective v5 palette onto MyGo widgets.
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
			SurfaceWindow:   hex("#080808"),
			SurfaceContent:  hex("#080808"),
			SurfaceCard:     hex("#141414"),
			SurfaceControl:  hex("#1B1B1B"),
			SurfaceHeader:   hex("#141414"),
			SurfaceSunken:   hex("#1B1B1B"),
			SurfaceOverlay:  hex("#222222"),
			SurfaceEvidence: hex("#1B1B1B"),

			SurfaceHover:    hex("#292929"),
			SurfacePressed:  hex("#333333"),
			SurfaceDisabled: hex("#1C1C20"),

			BorderSubtle:  hex("#2A2A2A"),
			BorderDefault: hex("#363636"),
			BorderStrong:  hex("#666666"),
			BorderControl: hex("#7C8494"),

			TextPrimary:   hex("#EEEEEE"),
			TextSecondary: hex("#B0B0B0"),
			TextMuted:     hex("#A0A0A0"),
			TextDisabled:  hex("#8D8D8D"),
			TextCode:      hex("#C9D1D9"),

			AccentFG:          hex("#8BABFF"),
			AccentFill:        hex("#8BABFF"),
			AccentFillHover:   hex("#A5BEFF"),
			AccentFillPressed: hex("#759BFA"),
			AccentOnFill:      hex("#101010"),
			AccentSoft:        hex("#202B42"),
			AccentSoftHover:   hex("#2A3854"),
			AccentBorder:      hex("#425579"),

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

			RowHover:          hex("#292929"),
			RowSelected:       hex("#202B42"),
			RowSelectedBorder: hex("#8BABFF"),
			TableHeader:       hex("#1B1B1B"),
			TableHeaderText:   hex("#98A1AE"),
			TableBorder:       hex("#2A2A2A"),
			EvidenceSurface:   hex("#1B1B1B"),
			EvidenceBorder:    hex("#363636"),
			Inverse:           hex("#F2F4F8"),
			InverseText:       hex("#101318"),
			Scrollbar:         mgui.RGBA(255, 255, 255, 0.28),
			Focus:             mgui.RGBA(147, 197, 253, 0.85),
			Selection:         mgui.RGBA(96, 165, 250, 0.30),
			Scrim:             mgui.RGBA(4, 7, 12, 0.64),
		}
	}

	return &Tokens{
		SurfaceWindow:   hex("#F3F5FA"),
		SurfaceContent:  hex("#F3F5FA"),
		SurfaceCard:     hex("#FFFFFF"),
		SurfaceControl:  hex("#FFFFFF"),
		SurfaceHeader:   hex("#FFFFFF"),
		SurfaceSunken:   hex("#F7F9FD"),
		SurfaceOverlay:  hex("#FFFFFF"),
		SurfaceEvidence: hex("#F7F9FD"),

		SurfaceHover:    hex("#EAF0FF"),
		SurfacePressed:  hex("#E2EAF8"),
		SurfaceDisabled: hex("#F4F4F5"),

		BorderSubtle:  hex("#E6EBF3"),
		BorderDefault: hex("#DCE3EE"),
		BorderStrong:  hex("#8A8A93"),
		BorderControl: hex("#8A8A93"),

		TextPrimary:   hex("#202B40"),
		TextSecondary: hex("#536177"),
		TextMuted:     hex("#637187"),
		TextDisabled:  hex("#7D899C"),
		TextCode:      hex("#3F3F46"),

		AccentFG:          hex("#315FE8"),
		AccentFill:        hex("#315FE8"),
		AccentFillHover:   hex("#244FD1"),
		AccentFillPressed: hex("#2046B8"),
		AccentOnFill:      hex("#FFFFFF"),
		AccentSoft:        hex("#EDF2FF"),
		AccentSoftHover:   hex("#E2EAFF"),
		AccentBorder:      hex("#CCD9FF"),

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

		RowHover:          hex("#F2F6FF"),
		RowSelected:       hex("#EDF2FF"),
		RowSelectedBorder: hex("#315FE8"),
		TableHeader:       hex("#F7F9FD"),
		TableHeaderText:   hex("#637187"),
		TableBorder:       hex("#E6EBF3"),
		EvidenceSurface:   hex("#F7F9FD"),
		EvidenceBorder:    hex("#DCE3EE"),
		Inverse:           hex("#18181B"),
		InverseText:       hex("#FFFFFF"),
		Scrollbar:         mgui.RGBA(0, 0, 0, 0.32),
		Focus:             mgui.RGBA(37, 99, 235, 0.85),
		Selection:         mgui.RGBA(37, 99, 235, 0.25),
		Scrim:             mgui.RGBA(24, 24, 27, 0.32),
	}
}

func hex(s string) mgui.Color { return mgui.Hex(s) }
