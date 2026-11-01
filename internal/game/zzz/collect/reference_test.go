package collect_test

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/game/zzz/collect"
)

func rectDiff(a, b image.Rectangle) int {
	d := absDiffInt(a.Min.X, b.Min.X)
	for _, v := range []int{
		absDiffInt(a.Min.Y, b.Min.Y),
		absDiffInt(a.Max.X, b.Max.X),
		absDiffInt(a.Max.Y, b.Max.Y),
	} {
		if v > d {
			d = v
		}
	}
	return d
}

func pointDiff(a, b image.Point) int {
	d := absDiffInt(a.X, b.X)
	if v := absDiffInt(a.Y, b.Y); v > d {
		d = v
	}
	return d
}

func absDiffInt(a, b int) int {
	if a > b {
		return a - b
	}
	return b - a
}

func TestReferenceLayoutValidatesAndFitsContent(t *testing.T) {
	l := collect.ReferenceLayout()
	if err := l.Validate(); err != nil {
		t.Fatalf("the reference layout must validate: %v", err)
	}
	if !l.FitsContent(l.Content) {
		t.Fatal("every reference anchor must fit the reference content region")
	}
	if l.Image != image.Pt(collect.ReferenceCanvasWidth, collect.ReferenceCanvasHeight) {
		t.Fatalf("image size %v, want the 1920x1080 reference canvas", l.Image)
	}
	if l.Content != image.Rect(0, 0, collect.ReferenceCanvasWidth, collect.ReferenceCanvasHeight) {
		t.Fatalf("content region %v, want the full reference canvas", l.Content)
	}
	if !l.MatchesFrame(image.Pt(1920, 1080), image.Rect(0, 0, 1920, 1080)) {
		t.Fatal("the reference canvas must identify its own frame exactly")
	}
	if l.Rows != 5 || l.Columns != 4 || len(l.Slots) != 20 {
		t.Fatalf("grid %d×%d with %d slots, want 5×4 with 20", l.Rows, l.Columns, len(l.Slots))
	}
	if l.RowPitch != 171 {
		t.Fatalf("row pitch %d, want the measured 171 frame pixels", l.RowPitch)
	}
	if l.ScrollDuration != 1200*time.Millisecond || l.TopDuration != 150*time.Millisecond {
		t.Fatalf("drag durations are provisional client-profile values and must stay explicit: %s/%s", l.ScrollDuration, l.TopDuration)
	}
}

func TestClientRectTransformAcceptsOnlyListedTiers(t *testing.T) {
	tiers := collect.ReferenceClientTiers()
	want := []image.Point{
		image.Pt(3840, 2160), image.Pt(2560, 1440), image.Pt(2276, 1280),
		image.Pt(1920, 1080), image.Pt(1600, 900), image.Pt(1280, 720),
	}
	if len(tiers) != len(want) {
		t.Fatalf("%d tiers, want %d", len(tiers), len(want))
	}
	for i, size := range want {
		if tiers[i].Client != size || tiers[i].Name == "" {
			t.Fatalf("tier %d is %+v, want a named %v", i, tiers[i], size)
		}
		tr, err := collect.ClientRectTransform(size)
		if err != nil {
			t.Fatalf("%v must be accepted: %v", size, err)
		}
		if tr.Source != collect.TransformClientRect || tr.Offset != (image.Point{}) {
			t.Fatalf("%v transform is %+v, want a clientRect transform at the origin", size, tr)
		}
		wantScale := float64(size.X) / float64(collect.ReferenceCanvasWidth)
		if tr.Scale != wantScale {
			t.Fatalf("%v scale is %v, want %v", size, tr.Scale, wantScale)
		}
	}
	for _, size := range []image.Point{
		{1920, 1081}, {1280, 800}, {1600, 901}, {2276, 1279},
		{2560, 1441}, {3840, 2161}, {1, 1}, {},
	} {
		if _, err := collect.ClientRectTransform(size); err == nil {
			t.Fatalf("an unlisted client area %v must be refused", size)
		}
	}
	first := tiers[0]
	tiers[0].Client = image.Pt(1, 1)
	if collect.ReferenceClientTiers()[0].Client != first.Client {
		t.Fatal("ReferenceClientTiers must return a copy; the accepted table was mutated")
	}
}

func TestScaledLayoutMatchesClientArea(t *testing.T) {
	ref := collect.ReferenceLayout()
	for _, tier := range collect.ReferenceClientTiers() {
		tier := tier
		t.Run(tier.Name, func(t *testing.T) {
			tr, err := collect.ClientRectTransform(tier.Client)
			if err != nil {
				t.Fatal(err)
			}
			got, err := collect.ScaledLayout(ref, tr)
			if err != nil {
				t.Fatal(err)
			}
			wantContent := image.Rect(0, 0, tier.Client.X, tier.Client.Y)
			if got.Image != tier.Client {
				t.Fatalf("image %v, want the client area %v", got.Image, tier.Client)
			}
			if got.Content != wantContent {
				t.Fatalf("content %v, want %v", got.Content, wantContent)
			}
			if !got.MatchesFrame(tier.Client, wantContent) {
				t.Fatal("a clientRect layout must match its client area exactly")
			}
			if err := got.Validate(); err != nil {
				t.Fatalf("the scaled layout must validate: %v", err)
			}
			if !got.FitsContent(got.Content) {
				t.Fatal("every scaled anchor must fit the client area")
			}
			if got.Rows != ref.Rows || got.Columns != ref.Columns || len(got.Slots) != len(ref.Slots) {
				t.Fatalf("grid changed: %d×%d with %d slots", got.Rows, got.Columns, len(got.Slots))
			}
			wantPitch := int(math.Round(float64(ref.RowPitch) * tr.Scale))
			if got.RowPitch != wantPitch {
				t.Fatalf("row pitch %d, want %d", got.RowPitch, wantPitch)
			}
			if tier.Client == image.Pt(1920, 1080) {
				if got.Name != ref.Name {
					t.Fatalf("identity name %q, want %q", got.Name, ref.Name)
				}
			} else if want := fmt.Sprintf("%s@%dx%d", ref.Name, tier.Client.X, tier.Client.Y); got.Name != want {
				t.Fatalf("name %q, want %q", got.Name, want)
			}
			if wantProbe := int(math.Max(1, math.Round(tr.Scale))); got.Funnel.At.Dx() != wantProbe || got.Funnel.At.Dy() != wantProbe {
				t.Fatalf("the 1x1 funnel probe became %v, want a %dx%d probe", got.Funnel.At, wantProbe, wantProbe)
			}
			again, err := collect.ScaledLayout(ref, tr)
			if err != nil {
				t.Fatal(err)
			}
			if fmt.Sprint(again) != fmt.Sprint(got) {
				t.Fatal("ScaledLayout must be deterministic")
			}
		})
	}
}

func TestScaledLayoutRasterizesSmallProbesFromTheirCenter(t *testing.T) {
	// A 1x1 probe whose scaled corners round to the same pixel would collapse
	// to an empty rectangle under corner scaling. Center rasterization keeps
	// one pixel at the rounded center.
	cases := []struct {
		name   string
		rect   image.Rectangle
		scale  float64
		offset image.Point
		want   image.Rectangle
	}{
		{
			name:  "1x1 at (2,2), scale 0.4",
			rect:  image.Rect(2, 2, 3, 3),
			scale: 0.4,
			want:  image.Rect(1, 1, 2, 2),
		},
		{
			name:  "1x1 at (0,0), scale 0.4",
			rect:  image.Rect(0, 0, 1, 1),
			scale: 0.4,
			want:  image.Rect(0, 0, 1, 1),
		},
		{
			name:  "1x1 at (5,5), scale 0.5",
			rect:  image.Rect(5, 5, 6, 6),
			scale: 0.5,
			want:  image.Rect(3, 3, 4, 4),
		},
		{
			name:   "1x1 with offset",
			rect:   image.Rect(10, 10, 11, 11),
			scale:  0.5,
			offset: image.Pt(7, 9),
			want:   image.Rect(12, 14, 13, 15),
		},
		{
			name:  "1x1 at (1,1), scale 1.4",
			rect:  image.Rect(1, 1, 2, 2),
			scale: 1.4,
			want:  image.Rect(1, 1, 2, 2),
		},
		{
			name:  "5x3 at scale 1.5",
			rect:  image.Rect(10, 20, 15, 23),
			scale: 1.5,
			want:  image.Rect(14, 30, 22, 35),
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			ref := collect.Layout{
				Funnel: collect.ColorProbe{At: tc.rect, Colors: map[string]color.RGBA{"x": {}}},
			}
			tr := collect.CanvasTransform{Scale: tc.scale, Offset: tc.offset, Source: collect.TransformMeasuredVideoRect}
			got, err := collect.ScaledLayout(ref, tr)
			if err != nil {
				t.Fatal(err)
			}
			if got.Funnel.At != tc.want {
				t.Fatalf("probe %v scaled to %v, want %v", tc.rect, got.Funnel.At, tc.want)
			}
		})
	}
}

func TestScaledLayoutRefusesInvalidTransforms(t *testing.T) {
	ref := collect.ReferenceLayout()
	cases := []struct {
		name string
		tr   collect.CanvasTransform
	}{
		{"zero scale", collect.CanvasTransform{Scale: 0, Source: collect.TransformClientRect}},
		{"negative scale", collect.CanvasTransform{Scale: -1, Source: collect.TransformMeasuredVideoRect}},
		{"unknown source", collect.CanvasTransform{Scale: 1, Source: "somewhere"}},
		{"clientRect with an offset", collect.CanvasTransform{Scale: 1, Offset: image.Pt(0, 96), Source: collect.TransformClientRect}},
		{"clientRect with an unlisted scale", collect.CanvasTransform{Scale: 1.01, Source: collect.TransformClientRect}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := collect.ScaledLayout(ref, tc.tr); err == nil {
				t.Fatal("the invalid transform must be refused")
			}
		})
	}
	valid := collect.CanvasTransform{Scale: 1650.0 / 1920.0, Offset: image.Pt(0, 96), Source: collect.TransformMeasuredVideoRect}
	if _, err := collect.ScaledLayout(ref, valid); err != nil {
		t.Fatalf("a measured video-rect transform must be accepted by the model: %v", err)
	}
}

func TestScaledLayoutDropsSortTemplateWhenScaling(t *testing.T) {
	mac := collect.MacCloudLayout()
	if mac.SortLabel.Empty() || mac.SortTemplate == nil {
		t.Fatal("the mac layout is expected to carry a sort template for this test")
	}
	tr, err := collect.ClientRectTransform(image.Pt(2560, 1440))
	if err != nil {
		t.Fatal(err)
	}
	got, err := collect.ScaledLayout(mac, tr)
	if err != nil {
		t.Fatal(err)
	}
	if !got.SortLabel.Empty() || got.SortTemplate != nil {
		t.Fatalf("a scaled layout must not shrink the sort template: label=%v template=%v", got.SortLabel, got.SortTemplate)
	}
}

func TestScaledCalibrationReferenceCategories(t *testing.T) {
	base := collect.ReferenceCalibration()
	cases := []struct {
		name                                                          string
		s                                                             float64
		top, bottom, selection, wheel, gold, minSamples, rowTol, move int
	}{
		{"scale 2", 2, 302, 1858, 216, 812, 812, base.ScrollMinSamples, 6, 4},
		{"scale 0.5", 0.5, 76, 465, 14, 51, 51, base.ScrollMinSamples, 2, 2},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, err := collect.ScaledCalibration(base, tc.s)
			if err != nil {
				t.Fatal(err)
			}
			if got.ScrollbarTopStartMax != tc.top || got.ScrollbarBottomEndMin != tc.bottom {
				t.Fatalf("scrollbar ends %d/%d, want %d/%d", got.ScrollbarTopStartMax, got.ScrollbarBottomEndMin, tc.top, tc.bottom)
			}
			if got.SelectionHitMin != tc.selection {
				t.Fatalf("selection hit floor %d, want %d", got.SelectionHitMin, tc.selection)
			}
			if got.WheelSelectedMin != tc.wheel {
				t.Fatalf("wheel selection floor %d, want %d", got.WheelSelectedMin, tc.wheel)
			}
			if got.QualityGoldMin != tc.gold {
				t.Fatalf("gold floor %d, want %d", got.QualityGoldMin, tc.gold)
			}
			if got.ScrollMinSamples != tc.minSamples {
				t.Fatalf("sample floor %d, want the unchanged reference value %d", got.ScrollMinSamples, tc.minSamples)
			}
			if got.ScrollRowTolerance != tc.rowTol || got.ScrollbarMoveMin != tc.move {
				t.Fatalf("pixel tolerances %d/%d, want %d/%d", got.ScrollRowTolerance, got.ScrollbarMoveMin, tc.rowTol, tc.move)
			}
			if got.BarEmptyMax != base.BarEmptyMax || got.DetailSameMax != base.DetailSameMax ||
				got.ScrollStationaryNCC != base.ScrollStationaryNCC || got.ScrollbarBrightMin != base.ScrollbarBrightMin {
				t.Fatal("normalized/intensity thresholds must stay unchanged")
			}
		})
	}
	if astate := base.SelectionHitMin; astate != 54 {
		t.Fatalf("reference selection floor %d, want 54", astate)
	}
	if base.ScrollRowTolerance != 3 {
		t.Fatal("ScaledCalibration mutated the reference base")
	}
	if _, err := collect.ScaledCalibration(base, 0); err == nil {
		t.Fatal("a non-positive scale must be refused")
	}
	if _, err := collect.ScaledCalibration(base, -1); err == nil {
		t.Fatal("a negative scale must be refused")
	}
}

func TestScaledCalibrationFloorAndOverrides(t *testing.T) {
	base := collect.ReferenceCalibration()
	small, err := collect.ScaledCalibration(base, 0.1)
	if err != nil {
		t.Fatal(err)
	}
	if small.ScrollRowTolerance != 2 || small.ScrollbarMoveMin != 2 {
		t.Fatalf("pixel tolerances must have a 2-pixel floor: row=%d move=%d", small.ScrollRowTolerance, small.ScrollbarMoveMin)
	}
	overridden := small.WithOverrides(func(c *collect.Calibration) {
		c.SelectionHitMin = 7
		c.QualityGoldMin = 11
	})
	if overridden.SelectionHitMin != 7 || overridden.QualityGoldMin != 11 {
		t.Fatalf("overrides were not applied: %+v", overridden)
	}
	if small.SelectionHitMin == 7 || small.QualityGoldMin == 11 {
		t.Fatal("WithOverrides must not mutate the scaled value it was called on")
	}
	if same := small.WithOverrides(nil); same != small {
		t.Fatal("a nil override must return the unchanged threshold set")
	}
}

func TestReferenceLayoutMatchesScaledMacDerivation(t *testing.T) {
	// The architecture's derived cloud transform: the game canvas is scaled
	// by 1650/1920 and placed 96 frame pixels below the top (40-pixel title
	// bar plus the centered 928-pixel canvas).
	macTransform := collect.CanvasTransform{
		Scale:  1650.0 / 1920.0,
		Offset: image.Pt(0, 96),
		Source: collect.TransformMeasuredVideoRect,
	}
	mac := collect.MacCloudLayout()
	got, err := collect.ScaledLayout(collect.ReferenceLayout(), macTransform)
	if err != nil {
		t.Fatal(err)
	}
	const tolerance = 3
	checkRect := func(field string, got, want image.Rectangle, limit int, reason string) {
		t.Helper()
		d := rectDiff(got, want)
		if d > limit {
			t.Errorf("%s: %v vs %v differs by %d px (limit %d; %s)", field, got, want, d, limit, reason)
		}
	}
	checkPoint := func(field string, got, want image.Point, limit int, reason string) {
		t.Helper()
		d := pointDiff(got, want)
		if d > limit {
			t.Errorf("%s: %v vs %v differs by %d px (limit %d; %s)", field, got, want, d, limit, reason)
		}
	}

	if got.Rows != mac.Rows || got.Columns != mac.Columns {
		t.Fatalf("grid %d×%d, want %d×%d", got.Rows, got.Columns, mac.Rows, mac.Columns)
	}
	for i := range mac.Slots {
		checkRect(fmt.Sprintf("slot %d", i), got.Slots[i], mac.Slots[i], tolerance,
			"the local client's first rows sit 1–4 px higher; the cloud list's first-row center is slightly lower")
	}
	checkRect("cell border", got.Cell.Border, mac.Cell.Border, tolerance, "relative probe")
	checkRect("cell bar", got.Cell.Bar, mac.Cell.Bar, tolerance, "relative probe at the 3-px boundary")
	checkRect("cell management", got.Cell.Management, mac.Cell.Management, tolerance, "relative probe")
	checkRect("cell avatar", got.Cell.Avatar, mac.Cell.Avatar, tolerance, "relative probe")
	if d := absDiffInt(got.RowPitch, mac.RowPitch); d > tolerance {
		t.Errorf("row pitch: %d vs %d differs by %d px (limit %d; measured 171 × 0.859 = 147)",
			got.RowPitch, mac.RowPitch, d, tolerance)
	}
	// The list panel is the one field whose measured local bounds are not a
	// scale of the cloud panel: the local detail pane starts at canvas x≈582
	// while the cloud pane starts at x≈612, and the local panel ends at
	// y≈935 while the cloud panel maps to y≈971. The scaled bounds therefore
	// differ by 30 px at the bottom edge. This is a bounded, measured client
	// difference, not a loosened tolerance.
	checkRect("list viewport", got.ListViewport, mac.ListViewport, tolerance,
		"the measured textured panel x 70–596 / y 115–971 matches the cloud-derived viewport")
	checkRect("scroll track", got.ScrollTrack, mac.ScrollTrack, tolerance, "1-px track column")
	checkPoint("scroll from", got.ScrollFrom, mac.ScrollFrom, tolerance, "provisional drag endpoint")
	checkPoint("scroll to", got.ScrollTo, mac.ScrollTo, tolerance, "provisional drag endpoint")
	checkPoint("top from", got.TopFrom, mac.TopFrom, tolerance, "provisional drag endpoint")
	checkPoint("top to", got.TopTo, mac.TopTo, tolerance, "provisional drag endpoint")
	if got.ScrollDuration != mac.ScrollDuration || got.TopDuration != mac.TopDuration {
		t.Fatalf("durations are client-profile timing and must not scale: %s/%s vs %s/%s",
			got.ScrollDuration, got.TopDuration, mac.ScrollDuration, mac.TopDuration)
	}
	checkPoint("filter button", got.FilterButton, mac.FilterButton, tolerance, "toolbar control center")
	checkRect("funnel probe", got.Funnel.At, mac.Funnel.At, tolerance, "1x1 icon fill pixel")
	for i := range mac.PanelOpen {
		checkRect(fmt.Sprintf("panel probe %d", i), got.PanelOpen[i].At, mac.PanelOpen[i].At, tolerance, "filter-panel probe")
	}
	checkPoint("panel reset", got.PanelReset, mac.PanelReset, tolerance, "filter-panel control")
	checkPoint("panel close", got.PanelClose, mac.PanelClose, tolerance, "filter-panel control")
	checkPoint("panel S", got.PanelS, mac.PanelS, tolerance, "filter-panel control")
	for i := range mac.Wheel {
		checkPoint(fmt.Sprintf("wheel %d", i+1), got.Wheel[i], mac.Wheel[i], tolerance, "wheel center")
	}
	for i := range mac.WheelArcs {
		checkRect(fmt.Sprintf("wheel arc %d", i), got.WheelArcs[i], mac.WheelArcs[i], tolerance, "relative arc block")
	}
	for i := range mac.DetailLines {
		checkRect(fmt.Sprintf("detail line %s", mac.DetailLines[i].Role), got.DetailLines[i].Rect, mac.DetailLines[i].Rect, tolerance,
			"the measured local line band is 2–3 px from the cloud band")
	}
	checkRect("quality badge", got.QualityBadge, mac.QualityBadge, tolerance, "gold badge")
	checkRect("detail avatar", got.DetailAvatar, mac.DetailAvatar, tolerance, "avatar crop")
	checkRect("top management", got.TopManagement, mac.TopManagement, tolerance, "toolbar indicator crop")
	checkRect("substat-4 probe", got.Substat4Probe, mac.Substat4Probe, tolerance, "value column")

	// Frame identity is a client-profile field, not canvas geometry: the
	// cloud frame is 1920x1080 with a 1650x1080 content region, while the
	// canvas transform produces the canvas's 1650x1024 bounding box and its
	// (0,96)-(1650,1024) placement. A cloud profile supplies the full frame
	// and content when it wires the layout.
	if got.Image != image.Pt(1650, 1024) || got.Content != image.Rect(0, 96, 1650, 1024) {
		t.Fatalf("canvas extent is image=%v content=%v, want 1650x1024 at (0,96)", got.Image, got.Content)
	}
	if got.Name == mac.Name {
		t.Fatal("a scaled layout must name its own display configuration")
	}
	// No Windows filter-panel sample exists, so the reference layout has no
	// sort template; the mac layout's template is reference-canvas pixels and
	// is never shrunk by ScaledLayout.
	if !got.SortLabel.Empty() || got.SortTemplate != nil {
		t.Fatalf("scaled sort fields must be dropped: %v / %v", got.SortLabel, got.SortTemplate)
	}
	if mac.SortLabel.Empty() || mac.SortTemplate == nil {
		t.Fatal("the mac layout is expected to carry a sort template")
	}
	// The funnel active tier is the measured Windows fill, not the mac video
	// color. The geometry is compared above; the color tier is profile data.
	if tier := got.Funnel.Colors["active"]; tier.R != 53 || tier.G != 140 || tier.B != 252 {
		t.Fatalf("funnel active tier %+v, want the measured Windows fill", tier)
	}
	if macTier := mac.Funnel.Colors["active"]; macTier == got.Funnel.Colors["active"] {
		t.Fatal("the cloud video's funnel color is expected to differ from the native Windows fill")
	}
}
