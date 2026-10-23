package ui

import (
	"github.com/LoTwT/youkai/internal/record"
	"github.com/egoist/mygo/ui"
)

// resultBadgeKind is the design's status shape: the pending review, the
// settled confirmation, the automatic pass, the failure, the unprocessed
// item and the neutral fallback.
type resultBadgeKind uint8

const (
	resultBadgeNeutral resultBadgeKind = iota
	resultBadgePending
	resultBadgeConfirmed
	resultBadgeAuto
	resultBadgeFailed
	resultBadgeUnprocessed
)

// The badges' 100% design metrics: a 22-DIP pill with an 8-DIP side
// padding, a 5-DIP icon gap, an 11px/600 label and a 12px shape.
// resultsMetricsFor scales all of them with the desktop's text size.
const (
	resultBadgeHeight    = 22
	resultBadgePaddingX  = 8
	resultBadgeGap       = 5
	resultBadgeFontSize  = 11
	resultBadgeGlyphSize = 12
)

// resultBadgeKindFor maps a row to its status shape. The recently
// confirmed flag is the model's temporary pending-segment marker, so the
// pure function can be tested without a model.
func resultBadgeKindFor(row *Row, recentlyConfirmed bool) resultBadgeKind {
	switch {
	case recentlyConfirmed:
		return resultBadgeConfirmed
	case row.Status == record.StatusFailed:
		return resultBadgeFailed
	case row.Status == record.StatusUnprocessed:
		return resultBadgeUnprocessed
	case row.PendingReview():
		return resultBadgePending
	case row.OptionalEquippedOnly(), row.Quality == record.QualityAutoAccepted:
		return resultBadgeAuto
	case row.Quality == record.QualityHumanConfirmed:
		return resultBadgeConfirmed
	default:
		return resultBadgeNeutral
	}
}

// resultBadgeStyle is one badge's resolved colors and shape. A selected
// table row overrides the background, border and text as the design does;
// the other columns keep the table's own selected style.
type resultBadgeStyle struct {
	Glyph       string
	Background  ui.Color
	Border      ui.Color
	BorderWidth float32
	Text        ui.Color
}

// resultBadgeStyleFor resolves a kind against the semantic tokens. The
// selected text comes from the runtime theme because Theme.AccentText is
// the only selected-row foreground the design uses.
func resultBadgeStyleFor(kind resultBadgeKind, tok *Tokens, theme *ui.Theme, selected bool) resultBadgeStyle {
	style := resultBadgeStyle{BorderWidth: 1}
	switch kind {
	case resultBadgePending:
		style.Glyph = "◐"
		style.Background = tok.WarningSoft
		style.Border = tok.WarningBorder
		style.Text = tok.WarningFG
	case resultBadgeConfirmed:
		style.Glyph = "✓"
		style.Background = tok.SuccessSoft
		style.Border = tok.SuccessBorder
		style.Text = tok.SuccessFG
	case resultBadgeAuto:
		style.Glyph = "○"
		style.Background = ui.Transparent
		style.Border = tok.BorderDefault
		style.Text = tok.TextSecondary
	case resultBadgeFailed:
		style.Glyph = "✕"
		style.Background = tok.DangerSoft
		style.Border = tok.DangerBorder
		style.Text = tok.DangerFG
	case resultBadgeUnprocessed:
		style.Glyph = "⊘"
		style.Background = ui.Transparent
		style.Border = tok.BorderDefault
		style.Text = tok.TextMuted
	default:
		style.Glyph = "⧉"
		style.Background = tok.SurfaceControl
		style.Border = tok.BorderDefault
		style.Text = tok.TextSecondary
	}
	if selected {
		style.Background = ui.Transparent
		style.Border = theme.AccentText.Alpha(0.75)
		style.Text = theme.AccentText
	}
	return style
}

// resultStatusBadge renders one row's status as the design's shape plus
// label badge. tableRow keeps the table's row height: the cell's 6-DIP
// vertical padding leaves less than the badge at 100%, so the badge cancels
// the extra DIPs with a negative margin instead of making every row taller.
// The metrics follow the text scale, so the badge grows beside the table's
// text and the row grows with it. The narrow list's row click still opens
// the detail page: the badge carries the same action as the table's other
// cells.
func (m *Model) resultStatusBadge(c *ui.Context, t *ui.Theme, tok *Tokens, row *Row, tableRow, selected bool) {
	metrics := resultsMetricsFor(m.textScale)
	style := resultBadgeStyleFor(resultBadgeKindFor(row, m.recentlyConfirmedNow(row)), tok, t, selected)
	badge := ui.Row(c).Height(metrics.BadgeHeight).Padding(0, metrics.BadgePaddingX).
		Gap(metrics.BadgeGap).AlignItems(ui.Center).Shrink(0).
		Background(style.Background).Border(style.BorderWidth, style.Border).
		Radius(metrics.BadgeHeight / 2)
	if tableRow {
		// The table's row height less its cells' vertical padding.
		inner := metrics.RowHeight - 2*t.Space(1.5)
		if overflow := metrics.BadgeHeight - inner; overflow > 0 {
			badge.MarginY(-overflow / 2)
		}
	}
	var onClick func()
	if tableRow && m.resultsRowOpensDetail() {
		full := m.rec.RowOf(row.Seq)
		onClick = func() { m.openResultDetailAt(full) }
		badge.OnClick(onClick)
	}
	badge.Children(func() {
		glyph := ui.Text(c, style.Glyph).FontSize(metrics.BadgeGlyphSize).FontWeight(600).
			TextColor(style.Text).SingleLine()
		label := ui.Text(c, m.resultStatusText(row)).FontSize(metrics.BadgeFontSize).
			FontWeight(600).TextColor(style.Text).SingleLine()
		if onClick != nil {
			// The press lands on the text above the badge's own box, so
			// both carry the narrow list's row action.
			glyph.OnClick(onClick)
			label.OnClick(onClick)
		}
	})
}
