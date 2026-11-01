package collect_test

import (
	"math"
	"reflect"
	"sort"
	"testing"

	"github.com/LoTwT/youkai/internal/game/zzz/collect"
)

// Calibration field categories. Every field of collect.Calibration must appear
// here; TestCalibrationFieldCategories fails when a field is added or removed
// without updating the table, so a new field cannot silently stay unscaled.
const (
	calibrationGeometry    = "geometry"
	calibrationCount       = "count"
	calibrationTolerance   = "tolerance"
	calibrationSampleCount = "sampleCount"
	calibrationUnchanged   = "unchanged"
)

var calibrationFieldCategories = map[string]string{
	// Geometry lengths and positions (×s).
	"ScrollStepX":           calibrationGeometry,
	"ScrollStepY":           calibrationGeometry,
	"ScrollSearchRadius":    calibrationGeometry,
	"ScrollbarTopStartMax":  calibrationGeometry,
	"ScrollbarBottomEndMin": calibrationGeometry,
	// Pixel counts (×s², unverified estimates).
	"SelectionHitMin":         calibrationCount,
	"SelectionClearMax":       calibrationCount,
	"WheelSelectedMin":        calibrationCount,
	"WheelOtherMax":           calibrationCount,
	"Substat4InkMin":          calibrationCount,
	"Substat3InkMax":          calibrationCount,
	"DetailPopulatedInk":      calibrationCount,
	"ManagementLockRingMin":   calibrationCount,
	"ManagementLockCoreMax":   calibrationCount,
	"ManagementDiscardRedMin": calibrationCount,
	"ManagementClearRingMax":  calibrationCount,
	"ManagementClearRedMax":   calibrationCount,
	"AvatarRingMin":           calibrationCount,
	"AvatarAbsentRingMax":     calibrationCount,
	"TopLockedDimMin":         calibrationCount,
	"TopUnlockedBrightMin":    calibrationCount,
	"TopUnlockedDimMax":       calibrationCount,
	"QualityGoldMin":          calibrationCount,
	// Pixel tolerances (×s, floor 2).
	"ScrollRowTolerance": calibrationTolerance,
	"ScrollbarMoveMin":   calibrationTolerance,
	// Sample-count threshold (reference-calibrated, unchanged by s).
	"ScrollMinSamples": calibrationSampleCount,
	// Normalized/intensity thresholds and row/sector counts (unchanged).
	"BarEmptyMax":              calibrationUnchanged,
	"BarOccupiedMin":           calibrationUnchanged,
	"DetailSameMax":            calibrationUnchanged,
	"DetailDifferentMin":       calibrationUnchanged,
	"InkThreshold":             calibrationUnchanged,
	"AvatarRingSectorsMin":     calibrationUnchanged,
	"AvatarCoreMeanMin":        calibrationUnchanged,
	"AvatarCoreRangeMin":       calibrationUnchanged,
	"AvatarAbsentCoreMeanMax":  calibrationUnchanged,
	"SortLabelMADMax":          calibrationUnchanged,
	"ScrollStationaryMax":      calibrationUnchanged,
	"ScrollDisplacedMax":       calibrationUnchanged,
	"ScrollDisplacedMargin":    calibrationUnchanged,
	"ScrollShiftMargin":        calibrationUnchanged,
	"ScrollMaxRows":            calibrationUnchanged,
	"ScrollLostMin":            calibrationUnchanged,
	"ScrollStationaryNCC":      calibrationUnchanged,
	"ScrollSelectionNCC":       calibrationUnchanged,
	"ScrollDisplacedNCC":       calibrationUnchanged,
	"ScrollNCCMargin":          calibrationUnchanged,
	"ScrollInfoStdMin":         calibrationUnchanged,
	"ScrollbarTravelTolerance": calibrationUnchanged,
	"ScrollbarBrightMin":       calibrationUnchanged,
}

func TestCalibrationFieldCategories(t *testing.T) {
	typ := reflect.TypeOf(collect.Calibration{})
	fields := make(map[string]bool, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		fields[f.Name] = true
		if _, ok := calibrationFieldCategories[f.Name]; !ok {
			t.Errorf("Calibration field %s has no category; add it to calibrationFieldCategories", f.Name)
		}
		if f.Type.Kind() != reflect.Int && f.Type.Kind() != reflect.Float64 {
			t.Errorf("Calibration field %s has unexpected kind %s", f.Name, f.Type.Kind())
		}
	}
	var extra []string
	for name := range calibrationFieldCategories {
		if !fields[name] {
			extra = append(extra, name)
		}
	}
	if len(extra) > 0 {
		sort.Strings(extra)
		t.Errorf("calibrationFieldCategories names unknown fields: %v", extra)
	}
	if typ.NumField() != len(calibrationFieldCategories) {
		t.Fatalf("Calibration has %d fields but the category table has %d entries", typ.NumField(), len(calibrationFieldCategories))
	}

	base := collect.ReferenceCalibration()
	baseValue := reflect.ValueOf(base)
	for _, s := range []float64{2, 0.5} {
		got, err := collect.ScaledCalibration(base, s)
		if err != nil {
			t.Fatal(err)
		}
		gotValue := reflect.ValueOf(got)
		for i := 0; i < typ.NumField(); i++ {
			name := typ.Field(i).Name
			category := calibrationFieldCategories[name]
			switch baseValue.Field(i).Kind() {
			case reflect.Int:
				b := baseValue.Field(i).Int()
				g := gotValue.Field(i).Int()
				var want int64
				switch category {
				case calibrationGeometry:
					want = int64(math.Max(1, math.Round(float64(b)*s)))
				case calibrationCount:
					want = int64(math.Max(1, math.Round(float64(b)*s*s)))
				case calibrationTolerance:
					want = int64(math.Max(2, math.Round(float64(b)*s)))
				case calibrationSampleCount, calibrationUnchanged:
					want = b
				default:
					t.Fatalf("%s has unknown category %q", name, category)
				}
				if g != want {
					t.Errorf("s=%v %s (%s) = %d, want %d", s, name, category, g, want)
				}
			case reflect.Float64:
				if gotValue.Field(i).Float() != baseValue.Field(i).Float() {
					t.Errorf("s=%v %s (%s) changed from %v to %v; float fields must be unchanged",
						s, name, category, baseValue.Field(i).Float(), gotValue.Field(i).Float())
				}
			}
		}
	}
}

// TestCalibrationCategoryTableIsComplete guards the table against an empty or
// truncated edit; the field loop above reports the concrete missing fields.
func TestCalibrationCategoryTableIsComplete(t *testing.T) {
	if len(calibrationFieldCategories) != reflect.TypeOf(collect.Calibration{}).NumField() {
		t.Fatalf("category table has %d entries for %d fields", len(calibrationFieldCategories), reflect.TypeOf(collect.Calibration{}).NumField())
	}
	for name, category := range calibrationFieldCategories {
		switch category {
		case calibrationGeometry, calibrationCount, calibrationTolerance, calibrationSampleCount, calibrationUnchanged:
		default:
			t.Fatalf("%s has unknown category %q", name, category)
		}
	}
}
