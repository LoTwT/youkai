package collect

import "fmt"

// ScaledCalibration converts a reference-canvas threshold set to scale s.
// The canonical base is ReferenceCalibration(); MacCloudCalibration() is a
// cloud-frame set whose scrollbar end thresholds are absolute mac-frame y
// positions, so scaling that set directly would carry the frame offset and
// scale into the reference coordinates. ReferenceCalibration() converts and
// re-measures the geometry first.
//
// The thresholds are not all lengths, so they do not all scale alike. The
// categories are explicit:
//
//   - geometry lengths and positions (×s): the scroll sampling steps, the
//     shift-search radius and the reference scrollbar track's resting
//     positions;
//   - pixel-count thresholds (×s², an initial estimate only): selection and
//     wheel hit counts, ink counts, management/avatar/top-indicator counts and
//     gold counts. These depend on the number of pixels in a probe, so they
//     are marked unverified and callers must replace them with measured
//     per-tier values through WithOverrides;
//   - pixel tolerances (×s, floor 2): ScrollRowTolerance and
//     ScrollbarMoveMin. The floor keeps a whole-row match from collapsing at
//     small scales;
//   - sample-count threshold (unchanged): ScrollMinSamples. The sampling steps
//     already scale, so the number of viewport samples stays roughly constant
//     across tiers; ReferenceCalibration() sets it from the reference
//     viewport's sample count instead of multiplying it by s²;
//   - normalized or intensity thresholds (unchanged): MAD/NCC bands,
//     grayscale floors, color-intensity tiers, sector counts, the relative
//     scrollbar travel tolerance and the row count;
//   - timing (unchanged): durations are a client profile, not canvas
//     geometry. ScrollTiming is not part of Calibration and is not scaled.
//
// Templates are not part of Calibration; see ScaledLayout for their
// reference-canvas resampling convention.
func ScaledCalibration(base Calibration, s float64) (Calibration, error) {
	if s <= 0 {
		return Calibration{}, fmt.Errorf("collect: calibration: scale %v is not positive", s)
	}
	count := func(v int) int { return scaledCount(v, s) }
	length := func(v int) int { return maxInt(1, int(roundFloat(float64(v)*s))) }
	tolerance := func(v int) int { return maxInt(2, int(roundFloat(float64(v)*s))) }

	out := base

	// Geometry lengths and positions (×s).
	out.ScrollStepX = length(base.ScrollStepX)
	out.ScrollStepY = length(base.ScrollStepY)
	out.ScrollSearchRadius = length(base.ScrollSearchRadius)
	out.ScrollbarTopStartMax = length(base.ScrollbarTopStartMax)
	out.ScrollbarBottomEndMin = length(base.ScrollbarBottomEndMin)

	// Pixel-count thresholds (×s², unverified estimates).
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

	// ScrollMinSamples is a sample-count threshold, not an area count: the
	// viewport area and the sampling steps both change with the tier, so the
	// reference base's value stays unchanged. See ReferenceCalibration.

	// Pixel tolerances (×s, floor 2).
	out.ScrollRowTolerance = tolerance(base.ScrollRowTolerance)
	out.ScrollbarMoveMin = tolerance(base.ScrollbarMoveMin)

	// Everything else is a normalized, intensity, count-of-a-fixed-set or
	// timing value and stays unchanged: BarEmptyMax, BarOccupiedMin,
	// DetailSameMax, DetailDifferentMin, InkThreshold, AvatarRingSectorsMin,
	// AvatarCoreMeanMin, AvatarCoreRangeMin, AvatarAbsentCoreMeanMax,
	// SortLabelMADMax, ScrollStationaryMax, ScrollDisplacedMax,
	// ScrollDisplacedMargin, ScrollShiftMargin, ScrollMaxRows, ScrollLostMin,
	// ScrollStationaryNCC, ScrollSelectionNCC, ScrollDisplacedNCC,
	// ScrollNCCMargin, ScrollInfoStdMin, ScrollbarTravelTolerance and
	// ScrollbarBrightMin.
	return out, nil
}

func scaledCount(v int, s float64) int {
	if v == 0 {
		return 0
	}
	return maxInt(1, int(roundFloat(float64(v)*s*s)))
}

// WithOverrides applies measured per-tier replacements to a scaled
// calibration. It is the entry point for the count and intensity thresholds
// whose scaled values are only initial estimates: a profile measures them on
// its own fixtures (and later on the real client) and overwrites them here.
// The receiver is a value, so the base calibration is never mutated.
func (c Calibration) WithOverrides(apply func(*Calibration)) Calibration {
	if apply != nil {
		apply(&c)
	}
	return c
}
