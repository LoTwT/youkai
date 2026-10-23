package ui

// resultsMetrics is the results table's geometry at one text scale. The
// 100% design is a 22-DIP badge in a 32-DIP row; the badge and the row
// grow by the same factor so a scaled label and glyph stay inside the pill
// and the row keeps the pill's full height visible.
type resultsMetrics struct {
	RowHeight      float32
	BadgeHeight    float32
	BadgePaddingX  float32
	BadgeGap       float32
	BadgeFontSize  float32
	BadgeGlyphSize float32
}

// resultsMetricsFor returns the design metrics scaled by the desktop's text
// size. A zero or negative value means the preference was never read and
// keeps the 100% design.
func resultsMetricsFor(textScale float32) resultsMetrics {
	scale := textScale
	if scale <= 0 {
		scale = 1
	}
	return resultsMetrics{
		RowHeight:      resultsRowHeight * scale,
		BadgeHeight:    resultBadgeHeight * scale,
		BadgePaddingX:  resultBadgePaddingX * scale,
		BadgeGap:       resultBadgeGap * scale,
		BadgeFontSize:  resultBadgeFontSize * scale,
		BadgeGlyphSize: resultBadgeGlyphSize * scale,
	}
}
