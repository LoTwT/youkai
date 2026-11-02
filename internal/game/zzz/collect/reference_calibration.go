package collect

import "image"

// The reference canvas's scrollbar end thresholds come from the 1920×1080
// Windows captures at the ScrollTrack probe column (x=55, grayscale >
// ScrollbarBrightMin). The W3a long-list sample (2026-11-02, about 28 items
// and 1.4 screens) verified both premises the earlier short-list captures
// could not: a resting thumb reaches the measured ends and the track groove
// stays dark. At the top frame the thumb is y 148–711, at the bottom frame
// y 368–931, and mid-drag y 361–924; the thumb is 564 pixels long, its
// darkest pixel is 116, the groove is 0, and one anti-aliased edge pixel is
// 92–93. The thresholds 148+3 = 151 and 931−2 = 929 therefore keep the mac
// calibration's 2–3 px resting-thumb slack. The separate y 941–945 run is the
// bottom cap, not the track.
//
// Limits: the darkest thumb pixel is only 6 levels above ScrollbarBrightMin
// (110), and the sample covers one list length (about 1.4 screens). A much
// longer list, a different theme, or a different DPI could change the groove
// or thumb brightness; those remain untested.
const (
	referenceScrollTrackTop       = 148
	referenceScrollTrackBottom    = 931
	referenceScrollbarTopSlack    = 3
	referenceScrollbarBottomSlack = 2
)

// ReferenceCalibration returns the macOS cloud threshold set expressed on the
// 1920×1080 reference canvas. It is the base ScaledCalibration expects for a
// Windows clientRect profile.
//
// The conversion is categorical:
//   - mac-frame geometry is converted by the inverse canvas scale
//     m = 1920/1650, except the scrollbar end thresholds. Those are absolute
//     mac-frame y positions, so they are re-measured against the reference
//     track's measured ends and the mac calibration's 2–3 px resting-thumb
//     slack instead of being multiplied by m (which would put the bottom
//     threshold past the measured bottom);
//   - pixel-count thresholds are converted by the area ratio m². They remain
//     unverified estimates until the Windows profile measures them per tier;
//   - ScrollMinSamples is recalibrated from the reference viewport's actual
//     sample count at the reference sampling steps, preserving the mac
//     calibration's samples-to-threshold ratio. It is not scaled by s²:
//     the steps already scale, so the sample count stays roughly constant
//     across tiers;
//   - normalized and intensity thresholds are unchanged.
func ReferenceCalibration() Calibration {
	macLayout := MacCloudLayout()
	base := MacCloudCalibration()
	// m is reference pixels per mac frame pixel: the mac canvas is the
	// 1650-wide content region, the reference canvas is 1920 wide.
	m := float64(ReferenceCanvasWidth) / float64(macLayout.Content.Dx())
	area := m * m
	count := func(v int) int { return maxInt(1, int(roundFloat(float64(v)*area))) }

	out := base

	// Geometry: mac-frame lengths and positions converted to reference pixels.
	out.ScrollStepX = maxInt(1, int(roundFloat(float64(base.ScrollStepX)*m)))
	out.ScrollStepY = maxInt(1, int(roundFloat(float64(base.ScrollStepY)*m)))
	out.ScrollSearchRadius = maxInt(1, int(roundFloat(float64(base.ScrollSearchRadius)*m)))
	// The scrollbar ends are absolute frame positions. Re-measure them on the
	// reference track rather than scaling the mac frame's positions.
	out.ScrollbarTopStartMax = referenceScrollTrackTop + referenceScrollbarTopSlack
	out.ScrollbarBottomEndMin = referenceScrollTrackBottom - referenceScrollbarBottomSlack

	// Pixel counts: area-ratio estimates, unverified on the reference canvas.
	out.SelectionHitMin = count(base.SelectionHitMin)
	out.SelectionClearMax = count(base.SelectionClearMax)
	out.WheelSelectedMin = count(base.WheelSelectedMin)
	out.WheelOtherMax = count(base.WheelOtherMax)
	out.Substat4InkMin = count(base.Substat4InkMin)
	out.Substat3InkMax = count(base.Substat3InkMax)
	out.DetailPopulatedInk = count(base.DetailPopulatedInk)
	out.ManagementLockRingMin = count(base.ManagementLockRingMin)
	out.ManagementLockCoreMax = count(base.ManagementLockCoreMax)
	out.ManagementDiscardRedMin = count(base.ManagementDiscardRedMin)
	out.ManagementClearRingMax = count(base.ManagementClearRingMax)
	out.ManagementClearRedMax = count(base.ManagementClearRedMax)
	out.AvatarRingMin = count(base.AvatarRingMin)
	out.AvatarAbsentRingMax = count(base.AvatarAbsentRingMax)
	out.TopLockedDimMin = count(base.TopLockedDimMin)
	out.TopUnlockedBrightMin = count(base.TopUnlockedBrightMin)
	out.TopUnlockedDimMax = count(base.TopUnlockedDimMax)
	out.QualityGoldMin = count(base.QualityGoldMin)

	// Sample-count threshold: preserve the mac ratio between the viewport's
	// actual sample count and the minimum. ScrollStepX/Y are already scaled
	// above, so this value is not multiplied by s² again.
	refSamples := scrollSampleCount(ReferenceLayout().ListViewport, out.ScrollStepX, out.ScrollStepY)
	macSamples := scrollSampleCount(macLayout.ListViewport, base.ScrollStepX, base.ScrollStepY)
	out.ScrollMinSamples = maxInt(1, int(roundFloat(
		float64(base.ScrollMinSamples)*float64(refSamples)/float64(macSamples))))

	return out
}

// scrollSampleCount is the number of samples zeroShiftError and bestShift
// read from a viewport at the given steps.
func scrollSampleCount(r image.Rectangle, stepX, stepY int) int {
	if r.Empty() || stepX <= 0 || stepY <= 0 {
		return 0
	}
	n := 0
	for y := r.Min.Y; y < r.Max.Y; y += stepY {
		for x := r.Min.X; x < r.Max.X; x += stepX {
			n++
		}
	}
	return n
}
