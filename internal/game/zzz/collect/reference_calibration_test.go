package collect_test

import (
	"image"
	"math"
	"testing"

	"github.com/LoTwT/youkai/internal/game/zzz/collect"
)

func TestReferenceCalibration(t *testing.T) {
	c := collect.ReferenceCalibration()
	ints := []struct {
		name string
		got  int
		want int
	}{
		// Geometry converted to reference pixels, plus the measured scrollbar
		// ends with the mac calibration's 2–3 px slack.
		{"ScrollStepX", c.ScrollStepX, 3},
		{"ScrollStepY", c.ScrollStepY, 2},
		{"ScrollSearchRadius", c.ScrollSearchRadius, 838},
		{"ScrollbarTopStartMax", c.ScrollbarTopStartMax, 151},
		{"ScrollbarBottomEndMin", c.ScrollbarBottomEndMin, 929},
		// Pixel counts converted by the area ratio (unverified estimates).
		{"SelectionHitMin", c.SelectionHitMin, 54},
		{"SelectionClearMax", c.SelectionClearMax, 14},
		{"WheelSelectedMin", c.WheelSelectedMin, 203},
		{"WheelOtherMax", c.WheelOtherMax, 81},
		{"Substat4InkMin", c.Substat4InkMin, 108},
		{"Substat3InkMax", c.Substat3InkMax, 68},
		{"DetailPopulatedInk", c.DetailPopulatedInk, 677},
		{"ManagementLockRingMin", c.ManagementLockRingMin, 162},
		{"ManagementLockCoreMax", c.ManagementLockCoreMax, 95},
		{"ManagementDiscardRedMin", c.ManagementDiscardRedMin, 135},
		{"ManagementClearRingMax", c.ManagementClearRingMax, 81},
		{"ManagementClearRedMax", c.ManagementClearRedMax, 41},
		{"AvatarRingMin", c.AvatarRingMin, 54},
		{"AvatarAbsentRingMax", c.AvatarAbsentRingMax, 162},
		{"TopLockedDimMin", c.TopLockedDimMin, 880},
		{"TopUnlockedBrightMin", c.TopUnlockedBrightMin, 271},
		{"TopUnlockedDimMax", c.TopUnlockedDimMax, 609},
		{"QualityGoldMin", c.QualityGoldMin, 203},
		// Sample-count threshold, calibrated from the reference viewport.
		{"ScrollMinSamples", c.ScrollMinSamples, 2711},
		// Tolerances are unchanged at the reference scale.
		{"ScrollRowTolerance", c.ScrollRowTolerance, 3},
		{"ScrollbarMoveMin", c.ScrollbarMoveMin, 2},
		// Normalized/intensity fields stay unchanged.
		{"BarEmptyMax", c.BarEmptyMax, 40},
		{"BarOccupiedMin", c.BarOccupiedMin, 120},
		{"InkThreshold", c.InkThreshold, 60},
		{"AvatarRingSectorsMin", c.AvatarRingSectorsMin, 7},
		{"AvatarCoreMeanMin", c.AvatarCoreMeanMin, 60},
		{"AvatarCoreRangeMin", c.AvatarCoreRangeMin, 40},
		{"AvatarAbsentCoreMeanMax", c.AvatarAbsentCoreMeanMax, 45},
		{"ScrollMaxRows", c.ScrollMaxRows, 4},
		{"ScrollbarBrightMin", c.ScrollbarBrightMin, 110},
	}
	for _, tc := range ints {
		if tc.got != tc.want {
			t.Errorf("%s = %d, want %d", tc.name, tc.got, tc.want)
		}
	}
	floats := []struct {
		name string
		got  float64
		want float64
	}{
		{"DetailSameMax", c.DetailSameMax, 2.0},
		{"DetailDifferentMin", c.DetailDifferentMin, 4.0},
		{"SortLabelMADMax", c.SortLabelMADMax, 5.0},
		{"ScrollStationaryMax", c.ScrollStationaryMax, 4.0},
		{"ScrollDisplacedMax", c.ScrollDisplacedMax, 10.0},
		{"ScrollDisplacedMargin", c.ScrollDisplacedMargin, 3.0},
		{"ScrollShiftMargin", c.ScrollShiftMargin, 2.0},
		{"ScrollLostMin", c.ScrollLostMin, 10.0},
		{"ScrollStationaryNCC", c.ScrollStationaryNCC, 0.92},
		{"ScrollSelectionNCC", c.ScrollSelectionNCC, 0.90},
		{"ScrollDisplacedNCC", c.ScrollDisplacedNCC, 0.93},
		{"ScrollNCCMargin", c.ScrollNCCMargin, 0.05},
		{"ScrollInfoStdMin", c.ScrollInfoStdMin, 12.0},
		{"ScrollbarTravelTolerance", c.ScrollbarTravelTolerance, 0.20},
	}
	for _, tc := range floats {
		if tc.got != tc.want {
			t.Errorf("%s = %v, want %v", tc.name, tc.got, tc.want)
		}
	}
}

func TestReferenceCalibrationSampleRatios(t *testing.T) {
	macLayout := collect.MacCloudLayout()
	macCal := collect.MacCloudCalibration()
	macRatio := float64(sampleCount(macLayout.ListViewport, macCal.ScrollStepX, macCal.ScrollStepY)) / float64(macCal.ScrollMinSamples)
	if math.Abs(macRatio-27.78) > 0.05 {
		t.Fatalf("mac sample ratio %.3f, want about 27.78", macRatio)
	}
	for _, size := range []image.Point{image.Pt(1920, 1080), image.Pt(2560, 1440), image.Pt(1280, 720)} {
		tr, err := collect.ClientRectTransform(size)
		if err != nil {
			t.Fatal(err)
		}
		layout, err := collect.ScaledLayout(collect.ReferenceLayout(), tr)
		if err != nil {
			t.Fatal(err)
		}
		cal, err := collect.ScaledCalibration(collect.ReferenceCalibration(), tr.Scale)
		if err != nil {
			t.Fatal(err)
		}
		samples := sampleCount(layout.ListViewport, cal.ScrollStepX, cal.ScrollStepY)
		ratio := float64(samples) / float64(cal.ScrollMinSamples)
		if ratio < macRatio*0.7 || ratio > macRatio*1.5 {
			t.Errorf("%v sample ratio %.2f outside [0.7, 1.5]× the mac ratio %.2f (samples %d, floor %d)",
				size, ratio, macRatio, samples, cal.ScrollMinSamples)
		}
		t.Logf("%v: samples=%d floor=%d ratio=%.2f (mac %.2f)", size, samples, cal.ScrollMinSamples, ratio, macRatio)
	}
}

func sampleCount(r image.Rectangle, stepX, stepY int) int {
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

func TestClientRectTransformRequiresExactTierScale(t *testing.T) {
	exact := collect.CanvasTransform{Scale: 1, Source: collect.TransformClientRect}
	if err := exact.Validate(); err != nil {
		t.Fatalf("the exact 1920×1080 tier scale must validate: %v", err)
	}
	for _, s := range []float64{math.Nextafter(1, 2), 1 + 1e-9, 1 - 1e-9} {
		tr := collect.CanvasTransform{Scale: s, Source: collect.TransformClientRect}
		if err := tr.Validate(); err == nil {
			t.Fatalf("scale %v is not the exact 1920×1080 tier ratio but validated", s)
		}
	}
	tr, err := collect.ClientRectTransform(image.Pt(1280, 720))
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Validate(); err != nil {
		t.Fatalf("the exact 1280×720 tier scale must validate: %v", err)
	}
	near := tr
	near.Scale = math.Nextafter(tr.Scale, 2)
	if err := near.Validate(); err == nil {
		t.Fatalf("a one-ulp offset from the 1280×720 ratio validated: %v", near.Scale)
	}
}
